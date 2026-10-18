package sync

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/internal/store"
)

// Conflict handling — the never-lose-data pass, ported from Joplin's
// packages/lib (Synchronizer.ts conflict actions, Note.createConflictNote /
// Note.mustHandleConflict, Resource.createConflictResourceNote /
// duplicateResource, Folder.markNotesAsConflict):
//
//   - both sides changed: the local version is kept as a conflict note (new
//     id, is_conflict=1, conflict_original_id=<original>, everything else
//     inherited — title and parent_id unchanged) and the remote version
//     becomes canonical under the original id;
//   - remote deleted while local has changes: same, except the copy gets an
//     empty conflict_original_id (the original is about to disappear);
//   - resources: the local resource is duplicated (new id, blob copied) and an
//     "Attachment conflict: …" note is filed in the "Conflicts (attachments)"
//     folder pointing at the duplicate;
//   - a folder deleted remotely while still holding notes marks them
//     is_conflict instead of orphaning them;
//   - folders/tags/note-tags have no conflict copy (Joplin's ItemConflict):
//     the remote version wins.
//
// Our engine is pull-first where Joplin uploads first, so Joplin's upload-side
// decision ("both remote and local have changes") is taken on the pull path
// before the remote item is applied.

// pullDecision is how a pulled item relates to the local copy.
type pullDecision int

const (
	// pullApply: the remote item is the current revision — apply it.
	pullApply pullDecision = iota
	// pullStale: the remote item is not a newer revision than what we already
	// have or have pushed — keep local and leave the sync point alone, so the
	// pending local change still uploads.
	pullStale
	// pullConflict: both sides changed since the last sync.
	pullConflict
)

// remoteDecision ports Joplin's upload-side conflict rules to the pull-first
// engine. "dirty" is Joplin's itemsThatNeedSync: modified after the last sync
// for this target. A remote item counts as changed after the sync point
// (Joplin: remoteContent.updated_time > local.sync_time).
func remoteDecision(si *models.SyncItem, localUpdated, remoteUpdated int64, localIsConflictNote bool) pullDecision {
	if si == nil {
		// Never synced to this target: the pull applies the remote copy
		// (pinned by delta_test). The double-edit conflict rule needs a sync
		// point to tell "changed since" apart, and a same-ID local copy is an
		// ID collision that generated IDs cannot produce.
		return pullApply
	}
	if localUpdated <= si.SyncTime {
		// Local is clean — the remote revision is authoritative. Never apply a
		// revision older than the local one though (a rolled-back target file
		// must not silently regress synced content).
		if remoteUpdated < localUpdated {
			return pullStale
		}
		return pullApply
	}
	// Local has unsynced changes.
	if remoteUpdated <= si.SyncTime {
		return pullStale // not a newer revision (redelivery/echo); our push wins
	}
	if localIsConflictNote && localUpdated > remoteUpdated {
		// Joplin: "conflict note has conflicting remote changes; local version
		// is newer" — treated as UpdateRemote.
		return pullStale
	}
	return pullConflict
}

// noteMustHandleConflict mirrors Joplin's Note.mustHandleConflict: an item with
// cipher text always conflicts (its content can't be compared here); otherwise
// only title/body differences matter — a metadata-only difference is resolved
// by taking the remote version without a conflict note.
func noteMustHandleConflict(local, remote *models.Note) bool {
	if local.EncryptionCipherText != "" || remote.EncryptionCipherText != "" {
		return true
	}
	return local.Title != remote.Title || local.Body != remote.Body
}

// keepDecryptedCopy reports whether a still-encrypted remote copy must not
// replace a decrypted local copy — at-least-once redelivery of the same or an
// older revision. A genuinely newer encrypted revision falls through to the
// normal decision and is decrypted after the pull; when the local copy has
// unsynced changes the sync point is left alone so the pending change still
// uploads.
func keepDecryptedCopy(db *store.DB, syncTarget, itemType int, itemID string, remoteApplied, localApplied int, remoteUpdated, localUpdated int64, si *models.SyncItem) (bool, error) {
	if remoteApplied != 1 || localApplied != 0 {
		return false, nil
	}
	if remoteUpdated > localUpdated {
		return false, nil // genuinely newer revision — apply and decrypt later
	}
	if si != nil && remoteUpdated > si.SyncTime {
		return false, nil // changed since the sync point — the conflict path decides
	}
	slog.Debug("skipping encrypted server copy, local is already decrypted", "id", itemID)
	if si != nil && localUpdated > si.SyncTime {
		return true, nil // pending local change still uploads; leave the sync point
	}
	return true, db.UpsertSyncItem(itemID, itemType, syncTarget)
}

// resourceMustHandleConflict mirrors Joplin's Resource.mustHandleConflict:
// only blob changes matter ("most of the resource metadata is not
// user-editable so there won't be any data loss").
func resourceMustHandleConflict(local, remote *models.Resource) bool {
	return local.BlobUpdatedTime != remote.BlobUpdatedTime
}

// conflictBytes serializes a conflict copy losslessly: the source item's raw
// text with the copy's fields merged over it, so unknown metadata keys
// (is_locked, extracted_resource_ids, …) survive verbatim in place. Falls back
// to a fresh serialization when no raw copy exists or it doesn't parse.
func conflictBytes(db *store.DB, sourceID string, fields map[string]string, fresh string) []byte {
	raw, _, ok, err := db.GetItemRaw(sourceID)
	if err != nil || !ok {
		return []byte(fresh)
	}
	pi, perr := models.ParseItem(string(raw))
	if perr != nil {
		return []byte(fresh)
	}
	return []byte(pi.SerializeWithOverrides(fields))
}

// createNoteConflictCopy keeps the local note as a conflict note and returns
// its new ID (Joplin's Note.createConflictNote: a copy of the local note with a
// new id, is_conflict=1, conflict_original_id set — empty when the remote copy
// no longer exists — and share fields cleared; everything else, including
// title, parent_id and the timestamps, is inherited).
func createNoteConflictCopy(db *store.DB, n *models.Note, remoteExists bool) (string, error) {
	c := *n
	c.ID = models.NewID()
	c.IsConflict = 1
	if remoteExists {
		c.ConflictOriginalID = n.ID
	} else {
		c.ConflictOriginalID = ""
	}
	c.IsShared = 0
	c.ShareID = ""

	plain := conflictBytes(db, n.ID, noteOverrides(&c), models.SerializeNote(&c))
	// One transaction: the conflict note and the bytes we will push for it
	// must not be able to land separately.
	if err := db.CreateNoteAndRaw(&c, plain); err != nil {
		return "", err
	}
	return c.ID, nil
}

// createResourceConflict keeps the local resource as a duplicate and files the
// pointer note Joplin creates for every attachment conflict
// (Resource.createConflictResourceNote).
func createResourceConflict(db *store.DB, r *models.Resource) error {
	dup := *r
	dup.ID = models.NewID()
	dup.IsShared = 0
	dup.ShareID = ""

	plain := conflictBytes(db, r.ID, resourceOverrides(&dup), models.SerializeResource(&dup))
	if err := db.UpsertResource(&dup); err != nil {
		return err
	}
	if err := db.SaveItemRaw(dup.ID, plain, dup.UpdatedTime); err != nil {
		return err
	}
	// Copy the blob (Joplin's duplicateResource copies it only if it exists).
	if data, err := os.ReadFile(db.GetResourceFile(r.ID)); err == nil {
		if err := db.SaveResourceFile(dup.ID, data); err != nil {
			return err
		}
	}

	folderID, err := resourceConflictFolderID(db)
	if err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	note := &models.Note{
		ID:              models.NewID(),
		ParentID:        folderID,
		Title:           fmt.Sprintf("Attachment conflict: \"%s\"", r.Title),
		Body:            fmt.Sprintf("There was a [conflict](%s) on the attachment below.\n\n%s", conflictHelpURL, resourceMarkupTag(&dup)),
		CreatedTime:     now,
		UpdatedTime:     now,
		UserCreatedTime: now,
		UserUpdatedTime: now,
		MarkupLanguage:  1,
	}
	if err := db.CreateNote(note); err != nil {
		return err
	}
	slog.Info("resource conflict: local attachment kept as duplicate", "id", r.ID, "copy_id", dup.ID)
	return nil
}

const conflictHelpURL = "https://joplinapp.org/help/apps/conflict"

// resourceConflictFolderID returns the id of the "Conflicts (attachments)"
// folder, creating it when missing (or when the same-titled folder is nested —
// Joplin keeps it top-level).
func resourceConflictFolderID(db *store.DB) (string, error) {
	const title = "Conflicts (attachments)"
	f, err := db.GetFolderByTitle(title)
	if err != nil {
		return "", err
	}
	if f == nil || f.ParentID != "" {
		now := time.Now().UnixMilli()
		nf := &models.Folder{
			ID:              models.NewID(),
			Title:           title,
			CreatedTime:     now,
			UpdatedTime:     now,
			UserCreatedTime: now,
			UserUpdatedTime: now,
		}
		if err := db.CreateFolder(nf); err != nil {
			return "", err
		}
		return nf.ID, nil
	}
	return f.ID, nil
}

// resourceMarkupTag renders Joplin's markdown resource link
// (Resource.markupTag, markdown flavor).
func resourceMarkupTag(r *models.Resource) string {
	alt := strings.NewReplacer(`\`, `\\`, `[`, `\[`, `]`, `\]`).Replace(r.Title)
	if strings.HasPrefix(r.Mime, "image/") {
		return fmt.Sprintf("![%s](:/%s)", alt, r.ID)
	}
	return fmt.Sprintf("[%s](:/%s)", alt, r.ID)
}

// applyResourceBlob writes a pulled resource blob, keeping a locally modified
// blob as a conflict copy first. The delta item's timestamp is compared
// against the sync point so at-least-once redelivery of our own last push
// cannot clobber an unpushed local blob.
func applyResourceBlob(db *store.DB, syncTarget int, resourceID string, remoteTime int64, content []byte) error {
	existing, err := db.GetResource(resourceID)
	if err != nil {
		return err
	}
	if existing == nil {
		// Metadata not known yet — store the blob; the .md item carries it.
		return savePulledBlob(db, resourceID, content)
	}

	si, err := db.GetSyncItem(resourceID, syncTarget)
	if err != nil {
		return err
	}
	if si == nil || existing.UpdatedTime > si.SyncTime {
		// The resource is modified locally and unsynced. If the remote blob is
		// not a newer revision than the sync point it is a redelivery of our
		// own last push — keep the local blob. Otherwise the remote blob wins
		// the canonical spot and ours is kept as a conflict copy first.
		if si != nil && remoteTime <= si.SyncTime {
			slog.Debug("keeping local resource blob; remote blob is not newer", "id", resourceID)
			return nil
		}
		if local, err := os.ReadFile(db.GetResourceFile(resourceID)); err == nil && bytes.Equal(local, content) {
			// Identical content — nothing to conflict about.
			return savePulledBlob(db, resourceID, content)
		}
		if err := createResourceConflict(db, existing); err != nil {
			return fmt.Errorf("cannot keep conflict copy of resource %s: %w", resourceID, err)
		}
		if err := db.UpsertSyncItem(resourceID, models.TypeResource, syncTarget); err != nil {
			return err
		}
	}
	return savePulledBlob(db, resourceID, content)
}

// fileV1Prefix marks a FileV1 (E2EE) encrypted blob, the same check the
// decrypt pass makes before calling DecryptFile.
const fileV1Prefix = "JED01"

// savePulledBlob writes a blob fetched from the target and records whether the
// bytes now on disk are a FileV1 envelope. That flag is all the decrypt pass
// looks at, and this function is the only place a pulled blob is stored: a file
// target re-lists every item on each pass, so an encrypted blob can arrive
// long after its metadata row was decrypted (flag 0). Storing it without the
// flag would leave the vault serving ciphertext for that resource forever.
func savePulledBlob(db *store.DB, resourceID string, content []byte) error {
	if err := db.SaveResourceFile(resourceID, content); err != nil {
		return err
	}
	res, err := db.GetResource(resourceID)
	if err != nil || res == nil {
		// Metadata has not arrived yet; the .md item sets the flag.
		return err
	}
	want := 0
	if bytes.HasPrefix(content, []byte(fileV1Prefix)) {
		want = 1
	}
	if res.EncryptionBlobEncrypted == want {
		return nil
	}
	res.EncryptionBlobEncrypted = want
	return db.UpsertResource(res)
}

// handleRemoteDelete applies a deletion found on the target without losing
// local changes: a note/resource modified since its sync keeps its content as
// a conflict copy first (Joplin's "remote has been deleted, but local has
// changes"), and a folder that still holds notes marks them is_conflict
// instead of orphaning them (Joplin's Folder.markNotesAsConflict).
func handleRemoteDelete(db *store.DB, syncTarget int, itemID string, itemType int) error {
	if itemType == 0 {
		var resolved int
		if err := db.QueryRow("SELECT item_type FROM sync_items WHERE item_id = ? LIMIT 1", itemID).Scan(&resolved); err == nil {
			itemType = resolved
		}
	}

	si, err := db.GetSyncItem(itemID, syncTarget)
	if err != nil {
		return err
	}
	if si == nil {
		// Never synced to this target: the local copy (if any) is a pending
		// create, and Joplin re-creates the remote item from it rather than
		// deleting local data. Let the push pass do the same.
		slog.Info("remote deleted an item never synced here; keeping it for re-upload", "id", itemID)
		return nil
	}

	switch itemType {
	case models.TypeNote:
		n, err := db.GetNote(itemID)
		if err != nil {
			return err
		}
		if n != nil && n.UpdatedTime > si.SyncTime && n.IsConflict == 0 && n.DeletedTime == 0 {
			if _, err := createNoteConflictCopy(db, n, false); err != nil {
				return fmt.Errorf("cannot keep conflict copy of deleted note %s: %w", itemID, err)
			}
			slog.Info("remote note deleted while modified locally; kept as conflict note", "id", itemID)
		}

	case models.TypeResource:
		r, err := db.GetResource(itemID)
		if err != nil {
			return err
		}
		if r != nil && r.UpdatedTime > si.SyncTime {
			if err := createResourceConflict(db, r); err != nil {
				return fmt.Errorf("cannot keep conflict copy of deleted resource %s: %w", itemID, err)
			}
			slog.Info("remote resource deleted while modified locally; kept as conflict copy", "id", itemID)
		}

	case models.TypeFolder:
		// Joplin: "a folder to be deleted still contains notes" is a conflict
		// — whatever deleted it should have deleted its content too.
		if _, err := db.Exec("UPDATE notes SET is_conflict = 1 WHERE parent_id = ?", itemID); err != nil {
			return err
		}
	}

	return db.DeleteLocalItem(itemID, itemType)
}
