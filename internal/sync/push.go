package sync

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"

	"github.com/lsj5031/joplin-lite/internal/e2ee"
	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/internal/store"
)

// requireEncryptionKey refuses all pushes when encryption is required but its
// active key is unavailable. Never silently fall back to plaintext.
func requireEncryptionKey(db *store.DB, svc *e2ee.Service) error {
	active, err := db.GetActiveMasterKeyID()
	if err != nil {
		return err
	}
	enabled, err := db.GetSetting("sync.e2ee.enabled")
	if err != nil {
		return err
	}
	if (enabled == "true" || active != "") && (active == "" || svc == nil || !svc.HasMasterKey(active)) {
		return fmt.Errorf("sync refused: unlock the active encryption key before uploading")
	}
	return nil
}

// PushChanges uploads local changes, encrypting them when E2EE is configured.
func PushChanges(backend SyncBackend, db *store.DB, e2eeSvc *e2ee.Service) error {
	if err := requireEncryptionKey(db, e2eeSvc); err != nil {
		return err
	}
	syncTarget := backend.SyncTarget()
	activeMasterKeyID, _ := db.GetActiveMasterKeyID()
	encryptBeforePush := e2eeSvc != nil && activeMasterKeyID != "" && e2eeSvc.HasMasterKey(activeMasterKeyID)

	// Push changed notes
	if err := pushNotes(backend, db, syncTarget, encryptBeforePush, e2eeSvc, activeMasterKeyID); err != nil {
		return err
	}

	// Push changed folders
	if err := pushFolders(backend, db, syncTarget, encryptBeforePush, e2eeSvc, activeMasterKeyID); err != nil {
		return err
	}

	// Push changed tags
	if err := pushTags(backend, db, syncTarget, encryptBeforePush, e2eeSvc, activeMasterKeyID); err != nil {
		return err
	}

	// Push changed note-tags
	if err := pushNoteTags(backend, db, syncTarget, encryptBeforePush, e2eeSvc, activeMasterKeyID); err != nil {
		return err
	}

	// Push changed resources
	if err := pushResources(backend, db, syncTarget, encryptBeforePush, e2eeSvc, activeMasterKeyID); err != nil {
		return err
	}

	// Push deletes
	if err := pushDeletes(backend, db, syncTarget); err != nil {
		return err
	}

	return nil
}

// itemPlain returns the plaintext bytes to push for an item and whether they
// are newly serialized (as opposed to the exact bytes that were pulled).
// Unchanged items are pushed back byte-for-byte from the raw store
// (losslessness by construction); changed items are re-serialized from the raw
// text so unknown metadata keys survive verbatim in their original positions.
func itemPlain(db *store.DB, id string, updatedTime int64, fields map[string]string, fresh string) ([]byte, bool) {
	raw, rawTime, ok, err := db.GetItemRaw(id)
	if err != nil || !ok {
		return []byte(fresh), true
	}
	if rawTime == updatedTime {
		return raw, false
	}
	pi, perr := models.ParseItem(string(raw))
	if perr != nil {
		return []byte(fresh), true
	}
	return []byte(pi.SerializeWithOverrides(fields)), true
}

// envelopeMeta reduces a field map to Joplin's keep-keys set for an encrypted
// envelope (BaseItem.serializeForSync) plus the encryption fields.
func envelopeMeta(fields map[string]string, cipherText, masterKeyID string) map[string]string {
	keep := map[string]bool{
		"id": true, "note_id": true, "tag_id": true, "parent_id": true,
		"share_id": true, "updated_time": true, "deleted_time": true,
		"type_": true, "master_key_id": true,
	}
	out := make(map[string]string)
	for k, v := range fields {
		if keep[k] {
			out[k] = v
		}
	}
	out["encryption_applied"] = "1"
	out["encryption_cipher_text"] = cipherText
	out["master_key_id"] = masterKeyID
	return out
}

// itemOverrides returns the formatted field map for the merge path, including
// title/body so edits to the head section survive re-serialization.
func noteOverrides(n *models.Note) map[string]string {
	f := models.NoteFields(n)
	f["title"] = n.Title
	f["body"] = n.Body
	return f
}

func folderOverrides(f *models.Folder) map[string]string {
	m := models.FolderFields(f)
	m["title"] = f.Title
	return m
}

func tagOverrides(t *models.Tag) map[string]string {
	m := models.TagFields(t)
	m["title"] = t.Title
	return m
}

func resourceOverrides(r *models.Resource) map[string]string {
	m := models.ResourceFields(r)
	m["title"] = r.Title
	return m
}

// storePushedRaw records the serialized plaintext after a successful push of
// newly serialized content, so later pushes of the unchanged item pass through
// these exact bytes.
func storePushedRaw(db *store.DB, id string, plain []byte, changed bool, updatedTime int64) {
	if !changed {
		return
	}
	if err := db.SaveItemRaw(id, plain, updatedTime); err != nil {
		slog.Error("failed to store raw item copy", "id", id, "error", err)
	}
}

func pushNotes(backend SyncBackend, db *store.DB, syncTarget int, encryptBeforePush bool, e2eeSvc *e2ee.Service, activeMasterKeyID string) error {
	// Find notes that have been modified locally but not synced
	rows, err := db.Query(`SELECT n.id FROM notes n
		LEFT JOIN sync_items si ON si.item_id = n.id AND si.sync_target = ?
		WHERE si.id IS NULL OR n.updated_time > si.sync_time`, syncTarget)
	if err != nil {
		return err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}

	return parallelEach(len(ids), pushWorkers, func(i int) error {
		id := ids[i]
		note, err := db.GetNote(id)
		if err != nil {
			return err
		}
		if note == nil {
			return nil
		}

		plain, changed := itemPlain(db, id, note.UpdatedTime, noteOverrides(note), models.SerializeNote(note))
		content := serializeNoteForSync(note, plain, changed, db, encryptBeforePush, e2eeSvc, activeMasterKeyID)
		if content == nil {
			return nil
		}
		itemName := id + ".md"
		path := fmt.Sprintf("/api/items/root:/%s:/content", url.PathEscape(itemName))

		if err := backend.Put(path, content); err != nil {
			slog.Error("failed to push note", "id", id, "error", err)
			return nil
		}

		if err := db.UpsertSyncItem(id, models.TypeNote, syncTarget); err != nil {
			return err
		}
		slog.Debug("pushed note", "id", id, "title", note.Title)
		return nil
	})
}

func serializeNoteForSync(note *models.Note, plain []byte, changed bool, db *store.DB, encrypt bool, e2eeSvc *e2ee.Service, masterKeyID string) []byte {
	if encrypt && note.EncryptionApplied == 0 && e2eeSvc != nil && masterKeyID != "" {
		cipherText, err := e2eeSvc.EncryptString(masterKeyID, string(plain))
		if err != nil {
			slog.Error("failed to encrypt note for sync", "id", note.ID, "error", err)
			return nil
		}
		storePushedRaw(db, note.ID, plain, changed, note.UpdatedTime)
		return []byte(models.SerializeEncryptedEnvelope(envelopeMeta(models.NoteFields(note), cipherText, masterKeyID)))
	}
	storePushedRaw(db, note.ID, plain, changed, note.UpdatedTime)
	return plain
}

func pushFolders(backend SyncBackend, db *store.DB, syncTarget int, encryptBeforePush bool, e2eeSvc *e2ee.Service, activeMasterKeyID string) error {
	rows, err := db.Query(`SELECT f.id FROM folders f
		LEFT JOIN sync_items si ON si.item_id = f.id AND si.sync_target = ?
		WHERE si.id IS NULL OR f.updated_time > si.sync_time`, syncTarget)
	if err != nil {
		return err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}

	return parallelEach(len(ids), pushWorkers, func(i int) error {
		id := ids[i]
		folder, err := db.GetFolder(id)
		if err != nil {
			return err
		}
		if folder == nil {
			return nil
		}

		plain, changed := itemPlain(db, id, folder.UpdatedTime, folderOverrides(folder), models.SerializeFolder(folder))
		content := serializeFolderForSync(folder, plain, changed, db, encryptBeforePush, e2eeSvc, activeMasterKeyID)
		if content == nil {
			return nil
		}
		itemName := id + ".md"
		path := fmt.Sprintf("/api/items/root:/%s:/content", url.PathEscape(itemName))

		if err := backend.Put(path, content); err != nil {
			slog.Error("failed to push folder", "id", id, "error", err)
			return nil
		}

		if err := db.UpsertSyncItem(id, models.TypeFolder, syncTarget); err != nil {
			return err
		}
		slog.Debug("pushed folder", "id", id, "title", folder.Title)
		return nil
	})
}

func serializeFolderForSync(folder *models.Folder, plain []byte, changed bool, db *store.DB, encrypt bool, e2eeSvc *e2ee.Service, masterKeyID string) []byte {
	if encrypt && folder.EncryptionApplied == 0 && e2eeSvc != nil && masterKeyID != "" {
		cipherText, err := e2eeSvc.EncryptString(masterKeyID, string(plain))
		if err != nil {
			slog.Error("failed to encrypt folder for sync", "id", folder.ID, "error", err)
			return nil
		}
		storePushedRaw(db, folder.ID, plain, changed, folder.UpdatedTime)
		return []byte(models.SerializeEncryptedEnvelope(envelopeMeta(models.FolderFields(folder), cipherText, masterKeyID)))
	}
	storePushedRaw(db, folder.ID, plain, changed, folder.UpdatedTime)
	return plain
}

func pushTags(backend SyncBackend, db *store.DB, syncTarget int, encryptBeforePush bool, e2eeSvc *e2ee.Service, activeMasterKeyID string) error {
	rows, err := db.Query(`SELECT t.id FROM tags t
		LEFT JOIN sync_items si ON si.item_id = t.id AND si.sync_target = ?
		WHERE si.id IS NULL OR t.updated_time > si.sync_time`, syncTarget)
	if err != nil {
		return err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}

	return parallelEach(len(ids), pushWorkers, func(i int) error {
		id := ids[i]
		tag, err := db.GetTag(id)
		if err != nil {
			return err
		}
		if tag == nil {
			return nil
		}

		plain, changed := itemPlain(db, id, tag.UpdatedTime, tagOverrides(tag), models.SerializeTag(tag))
		content := serializeTagForSync(tag, plain, changed, db, encryptBeforePush, e2eeSvc, activeMasterKeyID)
		if content == nil {
			return nil
		}
		itemName := id + ".md"
		path := fmt.Sprintf("/api/items/root:/%s:/content", url.PathEscape(itemName))

		if err := backend.Put(path, content); err != nil {
			slog.Error("failed to push tag", "id", id, "error", err)
			return nil
		}

		if err := db.UpsertSyncItem(id, models.TypeTag, syncTarget); err != nil {
			return err
		}
		slog.Debug("pushed tag", "id", id, "title", tag.Title)
		return nil
	})
}

func serializeTagForSync(tag *models.Tag, plain []byte, changed bool, db *store.DB, encrypt bool, e2eeSvc *e2ee.Service, masterKeyID string) []byte {
	if encrypt && tag.EncryptionApplied == 0 && e2eeSvc != nil && masterKeyID != "" {
		cipherText, err := e2eeSvc.EncryptString(masterKeyID, string(plain))
		if err != nil {
			slog.Error("failed to encrypt tag for sync", "id", tag.ID, "error", err)
			return nil
		}
		storePushedRaw(db, tag.ID, plain, changed, tag.UpdatedTime)
		return []byte(models.SerializeEncryptedEnvelope(envelopeMeta(models.TagFields(tag), cipherText, masterKeyID)))
	}
	storePushedRaw(db, tag.ID, plain, changed, tag.UpdatedTime)
	return plain
}

func pushNoteTags(backend SyncBackend, db *store.DB, syncTarget int, encryptBeforePush bool, e2eeSvc *e2ee.Service, activeMasterKeyID string) error {
	rows, err := db.Query(`SELECT nt.id, nt.note_id, nt.tag_id, nt.created_time, nt.updated_time,
		nt.user_created_time, nt.user_updated_time, nt.encryption_cipher_text,
		nt.encryption_applied, nt.is_shared
		FROM note_tags nt
		LEFT JOIN sync_items si ON si.item_id = nt.id AND si.sync_target = ?
		WHERE si.id IS NULL OR nt.updated_time > si.sync_time`, syncTarget)
	if err != nil {
		return err
	}
	defer rows.Close()

	var noteTags []*models.NoteTag
	for rows.Next() {
		nt := &models.NoteTag{}
		if err := rows.Scan(&nt.ID, &nt.NoteID, &nt.TagID, &nt.CreatedTime, &nt.UpdatedTime,
			&nt.UserCreatedTime, &nt.UserUpdatedTime, &nt.EncryptionCipherText,
			&nt.EncryptionApplied, &nt.IsShared); err != nil {
			return err
		}
		noteTags = append(noteTags, nt)
	}

	return parallelEach(len(noteTags), pushWorkers, func(i int) error {
		nt := noteTags[i]
		plain, changed := itemPlain(db, nt.ID, nt.UpdatedTime, models.NoteTagFields(nt), models.SerializeNoteTag(nt))
		content := serializeNoteTagForSync(nt, plain, changed, db, encryptBeforePush, e2eeSvc, activeMasterKeyID)
		if content == nil {
			return nil
		}
		itemName := nt.ID + ".md"
		path := fmt.Sprintf("/api/items/root:/%s:/content", url.PathEscape(itemName))

		if err := backend.Put(path, content); err != nil {
			slog.Error("failed to push note_tag", "id", nt.ID, "error", err)
			return nil
		}

		if err := db.UpsertSyncItem(nt.ID, models.TypeNoteTag, syncTarget); err != nil {
			return err
		}
		slog.Debug("pushed note_tag", "id", nt.ID)
		return nil
	})
}

func serializeResourceForSync(r *models.Resource, plain []byte, changed bool, db *store.DB, encrypt bool, e2eeSvc *e2ee.Service, masterKeyID string) []byte {
	if encrypt && r.EncryptionApplied == 0 && e2eeSvc != nil && masterKeyID != "" {
		cipherText, err := e2eeSvc.EncryptString(masterKeyID, string(plain))
		if err != nil {
			slog.Error("failed to encrypt resource for sync", "id", r.ID, "error", err)
			return nil
		}
		storePushedRaw(db, r.ID, plain, changed, r.UpdatedTime)
		return []byte(models.SerializeEncryptedEnvelope(envelopeMeta(models.ResourceFields(r), cipherText, masterKeyID)))
	}
	storePushedRaw(db, r.ID, plain, changed, r.UpdatedTime)
	return plain
}

func serializeNoteTagForSync(nt *models.NoteTag, plain []byte, changed bool, db *store.DB, encrypt bool, e2eeSvc *e2ee.Service, masterKeyID string) []byte {
	if encrypt && nt.EncryptionApplied == 0 && e2eeSvc != nil && masterKeyID != "" {
		cipherText, err := e2eeSvc.EncryptString(masterKeyID, string(plain))
		if err != nil {
			slog.Error("failed to encrypt note_tag", "id", nt.ID, "error", err)
			return nil
		}
		storePushedRaw(db, nt.ID, plain, changed, nt.UpdatedTime)
		return []byte(models.SerializeEncryptedEnvelope(envelopeMeta(models.NoteTagFields(nt), cipherText, masterKeyID)))
	}
	storePushedRaw(db, nt.ID, plain, changed, nt.UpdatedTime)
	return plain
}

func pushResources(backend SyncBackend, db *store.DB, syncTarget int, encryptBeforePush bool, e2eeSvc *e2ee.Service, activeMasterKeyID string) error {
	rows, err := db.Query(`SELECT r.id FROM resources r
		LEFT JOIN sync_items si ON si.item_id = r.id AND si.sync_target = ?
		WHERE si.id IS NULL OR r.updated_time > si.sync_time`, syncTarget)
	if err != nil {
		return err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}

	return parallelEach(len(ids), resourcePushWorkers, func(i int) error {
		id := ids[i]
		resource, err := db.GetResource(id)
		if err != nil {
			return err
		}
		if resource == nil {
			return nil
		}

		// With E2EE the blob pushed below is FileV1-encrypted, and the item
		// plaintext must say so (encryption_blob_encrypted = 1) — that flag is
		// how the pulling client knows the blob needs decrypting. The local
		// record keeps 0: the on-disk blob stays plaintext (plaintext at rest).
		encrypt := encryptBeforePush && resource.EncryptionApplied == 0 && e2eeSvc != nil && activeMasterKeyID != ""
		pushRes := resource
		if encrypt {
			r := *resource
			r.EncryptionBlobEncrypted = 1
			pushRes = &r
		}

		// Push metadata (raw-passthrough like other items)
		plain, changed := itemPlain(db, id, resource.UpdatedTime, resourceOverrides(pushRes), models.SerializeResource(pushRes))
		content := serializeResourceForSync(resource, plain, changed, db, encrypt, e2eeSvc, activeMasterKeyID)
		if content == nil {
			return nil
		}
		itemName := id + ".md"
		path := fmt.Sprintf("/api/items/root:/%s:/content", url.PathEscape(itemName))

		if err := backend.Put(path, content); err != nil {
			slog.Error("failed to push resource metadata", "id", id, "error", err)
			return nil
		}

		// Push blob if exists
		blobPath := db.GetResourceFile(id)
		if blobData, err := os.ReadFile(blobPath); err == nil {
			if encrypt {
				cipherText, err := e2eeSvc.EncryptFile(activeMasterKeyID, blobData)
				if err != nil {
					slog.Error("failed to encrypt resource blob", "id", id, "error", err)
					return nil
				}
				blobData = []byte(cipherText)
			}
			blobItemName := ".resource/" + id
			blobAPIPath := fmt.Sprintf("/api/items/root:/%s:/content", url.PathEscape(blobItemName))
			if err := backend.Put(blobAPIPath, blobData); err != nil {
				slog.Error("failed to push resource blob", "id", id, "error", err)
				return nil
			}
		}

		if err := db.UpsertSyncItem(id, models.TypeResource, syncTarget); err != nil {
			return err
		}
		slog.Debug("pushed resource", "id", id)
		return nil
	})
}

func pushDeletes(backend SyncBackend, db *store.DB, syncTarget int) error {
	deleted, err := db.GetDeletedItems(syncTarget)
	if err != nil {
		return err
	}

	for _, item := range deleted {
		// A resource lives at two paths on the target: metadata <id>.md and
		// the blob .resource/<id>. The blob delete is best-effort; the
		// metadata delete below is what the bookkeeping keys on.
		if item.ItemType == models.TypeResource {
			blobItemName := ".resource/" + item.ItemID
			blobPath := fmt.Sprintf("/api/items/root:/%s:/content", url.PathEscape(blobItemName))
			if err := backend.Delete(blobPath); err != nil {
				slog.Warn("failed to delete remote resource blob", "id", item.ItemID, "error", err)
			}
		}

		itemName := item.ItemID + ".md"
		path := fmt.Sprintf("/api/items/root:/%s:/content", url.PathEscape(itemName))

		if err := backend.Delete(path); err != nil {
			return fmt.Errorf("cannot delete remote item %s: %w", item.ItemID, err)
		}

		if err := db.RemoveDeletedItem(item.ID); err != nil {
			return err
		}
		_ = db.DeleteItemRaw(item.ItemID)
		slog.Debug("deleted remote item", "id", item.ItemID)
	}
	return nil
}
