package sync

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"

	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/internal/store"
)

// DeltaResponse represents the Joplin Server delta API response.
type DeltaResponse struct {
	Items   []DeltaItem `json:"items"`
	Cursor  string      `json:"cursor"`
	HasMore bool        `json:"has_more"`
}

// DeltaItem represents a single item in a delta response.
type DeltaItem struct {
	ID          string `json:"id"`
	ItemName    string `json:"item_name"`
	Type        int    `json:"type"` // 1=put, 3=delete
	UpdatedTime int64  `json:"updated_time"`
}

// PullChanges fetches all changes from the server since the last sync cursor
// and applies them to the local database.
func PullChanges(backend SyncBackend, db *store.DB) error {
	cursor, err := db.GetSyncCursor()
	if err != nil {
		return fmt.Errorf("cannot get sync cursor: %w", err)
	}

	syncTarget := backend.SyncTarget()

	for {
		path := "/api/items/root:/:/delta"
		if cursor != "" {
			path += "?cursor=" + url.QueryEscape(cursor)
		}

		data, err := backend.Get(path)
		if err != nil {
			return fmt.Errorf("delta request failed: %w", err)
		}

		var delta DeltaResponse
		if err := json.Unmarshal(data, &delta); err != nil {
			return fmt.Errorf("cannot parse delta response: %w", err)
		}

		slog.Info("processing delta", "items", len(delta.Items), "has_more", delta.HasMore)

		typeCounts := make(map[string]int)

		// Fetch item content in bounded parallel waves, then apply the wave
		// sequentially. The network was the serial bottleneck (one blocking
		// GET per item); DB writes and conflict decisions stay ordered, and a
		// wave is capped so large resource blobs cannot pile up unbounded.
		const deltaBatch = 8
		for start := 0; start < len(delta.Items); start += deltaBatch {
			end := start + deltaBatch
			if end > len(delta.Items) {
				end = len(delta.Items)
			}
			batch := delta.Items[start:end]
			prefetched := make([][]byte, len(batch))
			fetchErrs := make([]error, len(batch))
			var mu sync.Mutex

			_ = parallelEach(len(batch), deltaWorkers, func(i int) error {
				item := batch[i]
				if item.Type == 3 {
					return nil
				}
				path := fmt.Sprintf("/api/items/root:/%s:/content", url.PathEscape(item.ItemName))
				content, err := backend.Get(path)
				if err != nil {
					mu.Lock()
					fetchErrs[i] = err
					mu.Unlock()
					return nil
				}
				prefetched[i] = content
				return nil
			})

			for i, item := range batch {
				if fetchErrs[i] != nil {
					return fmt.Errorf("cannot fetch delta item %s: %w", item.ItemName, fetchErrs[i])
				}
				if item.Type == 3 {
					typeCounts["delete"]++
				} else if isResourceBlob(item.ItemName) {
					typeCounts["resource_blob"]++
				}
				if err := applyDeltaItem(backend, db, syncTarget, item, prefetched[i]); err != nil {
					return fmt.Errorf("cannot apply delta item %s: %w", item.ItemName, err)
				}
			}
		}
		slog.Info("delta summary", "counts", typeCounts)

		cursor = delta.Cursor
		if err := db.SetSyncCursor(cursor); err != nil {
			return fmt.Errorf("cannot save sync cursor: %w", err)
		}

		if !delta.HasMore {
			break
		}
	}

	return nil
}

// applyDeltaItem processes a single delta item. If prefetched is non-nil it is
// used as the item content; otherwise the content is fetched from the server.
func applyDeltaItem(backend SyncBackend, db *store.DB, syncTarget int, item DeltaItem, prefetched []byte) error {
	// Delete type
	if item.Type == 3 {
		itemID, itemType := parseItemName(item.ItemName)
		if itemID != "" {
			return handleRemoteDelete(db, syncTarget, itemID, itemType)
		}
		return nil
	}

	// Put type - use prefetched content or fetch from server
	content := prefetched
	if content == nil {
		path := fmt.Sprintf("/api/items/root:/%s:/content", url.PathEscape(item.ItemName))
		var err error
		content, err = backend.Get(path)
		if err != nil {
			return fmt.Errorf("cannot fetch item content: %w", err)
		}
	}

	// Check if this is a resource blob (binary file)
	if isResourceBlob(item.ItemName) {
		resourceID := extractResourceID(item.ItemName)
		if resourceID != "" {
			return applyResourceBlob(db, syncTarget, resourceID, item.UpdatedTime, content)
		}
		return nil
	}

	// Parse as Joplin text format
	itemType, fields := models.Deserialize(string(content))

	slog.Debug("applying delta item", "name", item.ItemName, "parsed_type", itemType)

	switch itemType {
	case models.TypeNote:
		note := models.DeserializeNote(string(content))
		if note.ID == "" {
			return nil
		}
		existing, err := db.GetNote(note.ID)
		if err != nil {
			return err
		}
		if existing != nil {
			si, err := db.GetSyncItem(note.ID, syncTarget)
			if err != nil {
				return err
			}
			if handled, err := keepDecryptedCopy(db, syncTarget, models.TypeNote, note.ID,
				note.EncryptionApplied, existing.EncryptionApplied, note.UpdatedTime, existing.UpdatedTime, si); handled || err != nil {
				return err
			}
			switch remoteDecision(si, existing.UpdatedTime, note.UpdatedTime, existing.IsConflict == 1) {
			case pullStale:
				slog.Debug("keeping local note; remote copy is not a newer revision", "id", note.ID)
				return nil
			case pullConflict:
				if existing.IsConflict != 1 && noteMustHandleConflict(existing, note) {
					if _, err := createNoteConflictCopy(db, existing, true); err != nil {
						return fmt.Errorf("cannot keep conflict copy of note %s: %w", note.ID, err)
					}
					slog.Info("note conflict: local version kept as conflict note, remote is canonical", "id", note.ID)
				}
			}
		}
		if err := db.ApplyPulledItem(note, syncTarget, content); err != nil {
			return fmt.Errorf("cannot upsert note %s: %w", note.ID, err)
		}
		return nil

	case models.TypeFolder:
		folder := models.DeserializeFolder(string(content))
		if folder.ID == "" {
			return nil
		}
		existing, err := db.GetFolder(folder.ID)
		if err != nil {
			return err
		}
		if existing != nil {
			si, err := db.GetSyncItem(folder.ID, syncTarget)
			if err != nil {
				return err
			}
			if handled, err := keepDecryptedCopy(db, syncTarget, models.TypeFolder, folder.ID,
				folder.EncryptionApplied, existing.EncryptionApplied, folder.UpdatedTime, existing.UpdatedTime, si); handled || err != nil {
				return err
			}
			switch remoteDecision(si, existing.UpdatedTime, folder.UpdatedTime, false) {
			case pullStale:
				slog.Debug("keeping local folder; remote copy is not a newer revision", "id", folder.ID)
				return nil
			case pullConflict:
				// Joplin's ItemConflict: folder metadata has no conflict copy,
				// the remote version wins.
				slog.Info("folder conflict: taking remote version", "id", folder.ID)
			}
		}
		if err := db.ApplyPulledItem(folder, syncTarget, content); err != nil {
			return fmt.Errorf("cannot upsert folder %s: %w", folder.ID, err)
		}
		return nil

	case models.TypeTag:
		tag := models.DeserializeTag(string(content))
		if tag.ID == "" {
			return nil
		}
		existing, err := db.GetTag(tag.ID)
		if err != nil {
			return err
		}
		if existing != nil {
			si, err := db.GetSyncItem(tag.ID, syncTarget)
			if err != nil {
				return err
			}
			if handled, err := keepDecryptedCopy(db, syncTarget, models.TypeTag, tag.ID,
				tag.EncryptionApplied, existing.EncryptionApplied, tag.UpdatedTime, existing.UpdatedTime, si); handled || err != nil {
				return err
			}
			switch remoteDecision(si, existing.UpdatedTime, tag.UpdatedTime, false) {
			case pullStale:
				slog.Debug("keeping local tag; remote copy is not a newer revision", "id", tag.ID)
				return nil
			case pullConflict:
				// Joplin's ItemConflict: tag metadata has no conflict copy,
				// the remote version wins.
				slog.Info("tag conflict: taking remote version", "id", tag.ID)
			}
		}
		if err := db.ApplyPulledItem(tag, syncTarget, content); err != nil {
			return fmt.Errorf("cannot upsert tag %s: %w", tag.ID, err)
		}
		return nil

	case models.TypeNoteTag:
		nt := models.DeserializeNoteTag(string(content))
		if nt.ID == "" {
			return nil
		}
		var localUpdated int64
		if err := db.QueryRow("SELECT updated_time FROM note_tags WHERE id = ?", nt.ID).Scan(&localUpdated); err == nil {
			si, err := db.GetSyncItem(nt.ID, syncTarget)
			if err != nil {
				return err
			}
			if remoteDecision(si, localUpdated, nt.UpdatedTime, false) == pullStale {
				slog.Debug("keeping local note_tag; remote copy is not a newer revision", "id", nt.ID)
				return nil
			}
			// Joplin's ItemConflict: junction rows take the remote version.
		}
		if err := db.ApplyPulledItem(nt, syncTarget, content); err != nil {
			return fmt.Errorf("cannot upsert note_tag %s: %w", nt.ID, err)
		}
		return nil

	case models.TypeResource:
		resource := models.DeserializeResource(string(content))
		if resource.ID == "" {
			return nil
		}
		existing, err := db.GetResource(resource.ID)
		if err != nil {
			return err
		}
		if existing != nil {
			if _, present := fields["blob_updated_time"]; !present {
				resource.BlobUpdatedTime = existing.BlobUpdatedTime
			}
			si, err := db.GetSyncItem(resource.ID, syncTarget)
			if err != nil {
				return err
			}
			switch remoteDecision(si, existing.UpdatedTime, resource.UpdatedTime, false) {
			case pullStale:
				slog.Debug("keeping local resource; remote copy is not a newer revision", "id", resource.ID)
				return nil
			case pullConflict:
				if resourceMustHandleConflict(existing, resource) {
					if err := createResourceConflict(db, existing); err != nil {
						return fmt.Errorf("cannot keep conflict copy of resource %s: %w", resource.ID, err)
					}
					slog.Info("resource conflict: remote version is canonical", "id", resource.ID)
				}
			}
		}
		if err := db.ApplyPulledItem(resource, syncTarget, content); err != nil {
			return fmt.Errorf("cannot upsert resource %s: %w", resource.ID, err)
		}
		return nil

	case models.TypeMasterKey:
		mk := models.DeserializeMasterKey(string(content))
		if mk.ID == "" {
			return nil
		}
		if err := db.ApplyPulledItem(mk, syncTarget, content); err != nil {
			return fmt.Errorf("cannot upsert master_key %s: %w", mk.ID, err)
		}
		return nil

	case models.TypeRevision:
		// Revisions (note history): indexed for the /revisions API, content
		// kept raw (item_raw) and re-pushed byte-for-byte — there is no
		// revision serializer, losslessness is by construction.
		rev := models.DeserializeRevision(string(content))
		if rev == nil {
			return nil
		}
		if err := db.ApplyPulledItem(rev, syncTarget, content); err != nil {
			return fmt.Errorf("cannot upsert revision %s: %w", rev.ID, err)
		}
		return nil

	default:
		slog.Warn("skipping unknown item type", "type", itemType, "name", item.ItemName)
	}

	return nil
}

// parseItemName extracts the item ID from a Joplin Server item name.
// Item names look like: "<32-hex-char-id>.md"
// Follows the same approach as Joplin's BaseItem.pathToId / isSystemPath:
// split on ".", verify the name part is a valid 32-char hex ID and the
// extension is "md".
func parseItemName(name string) (string, int) {
	// Take the last path component (handles "folder/id.md" style paths)
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	parts := strings.SplitN(name, ".", 2)
	if len(parts) != 2 || parts[1] != "md" {
		return "", 0
	}
	id := parts[0]
	if !isValidID(id) {
		return "", 0
	}
	return id, 0 // Type is unknown from name alone
}

// isResourceBlob checks if an item name refers to a resource blob.
// Resource blobs don't end with ".md".
func isResourceBlob(name string) bool {
	return len(name) > 2 && !strings.HasSuffix(name, ".md")
}

// extractResourceID gets the resource ID from a blob item name.
func extractResourceID(name string) string {
	// Resource blobs are stored as ".resource/<id>"
	if len(name) >= 42 && name[:10] == ".resource/" {
		return name[10:]
	}
	return ""
}

func isValidID(s string) bool {
	if len(s) != 32 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
