package store

import (
	"database/sql"

	"github.com/lsj5031/joplin-lite/internal/models"
)

// The All* readers below load a whole table for export. They deliberately skip
// pagination: an export has to see every item, and the caller streams them out
// rather than holding the library in memory.

// AllNotes returns every live note, including conflict copies.
func (db *DB) AllNotes() ([]*models.Note, error) {
	rows, err := db.Query(`SELECT id, parent_id, title, body, created_time, updated_time,
		is_conflict, latitude, longitude, altitude, author, source_url,
		is_todo, todo_due, todo_completed, source, source_application, application_data,
		"order", user_created_time, user_updated_time, encryption_cipher_text,
		encryption_applied, markup_language, is_shared, share_id, conflict_original_id,
		master_key_id, user_data, deleted_time
		FROM notes WHERE deleted_time = 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var notes []*models.Note
	for rows.Next() {
		n := &models.Note{}
		if err := rows.Scan(
			&n.ID, &n.ParentID, &n.Title, &n.Body, &n.CreatedTime, &n.UpdatedTime,
			&n.IsConflict, &n.Latitude, &n.Longitude, &n.Altitude, &n.Author, &n.SourceURL,
			&n.IsTodo, &n.TodoDue, &n.TodoCompleted, &n.Source, &n.SourceApplication, &n.ApplicationData,
			&n.Order, &n.UserCreatedTime, &n.UserUpdatedTime, &n.EncryptionCipherText,
			&n.EncryptionApplied, &n.MarkupLanguage, &n.IsShared, &n.ShareID, &n.ConflictOriginalID,
			&n.MasterKeyID, &n.UserData, &n.DeletedTime,
		); err != nil {
			return nil, err
		}
		n.Type_ = models.TypeNote
		notes = append(notes, n)
	}
	return notes, rows.Err()
}

// AllFolders returns every live notebook.
func (db *DB) AllFolders() ([]*models.Folder, error) {
	rows, err := db.Query(`SELECT id, title, created_time, updated_time,
		user_created_time, user_updated_time, encryption_cipher_text, encryption_applied,
		parent_id, is_shared, share_id, master_key_id, icon, user_data, deleted_time
		FROM folders WHERE deleted_time = 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var folders []*models.Folder
	for rows.Next() {
		f := &models.Folder{}
		if err := rows.Scan(
			&f.ID, &f.Title, &f.CreatedTime, &f.UpdatedTime,
			&f.UserCreatedTime, &f.UserUpdatedTime, &f.EncryptionCipherText, &f.EncryptionApplied,
			&f.ParentID, &f.IsShared, &f.ShareID, &f.MasterKeyID, &f.Icon, &f.UserData, &f.DeletedTime,
		); err != nil {
			return nil, err
		}
		f.Type_ = models.TypeFolder
		folders = append(folders, f)
	}
	return folders, rows.Err()
}

// AllResources returns every attachment's metadata (not its bytes).
func (db *DB) AllResources() ([]*models.Resource, error) {
	rows, err := db.Query(`SELECT id, title, mime, filename, created_time, updated_time,
		user_created_time, user_updated_time, file_extension, encryption_cipher_text,
		encryption_applied, encryption_blob_encrypted, size, is_shared, share_id,
		master_key_id, user_data, blob_updated_time
		FROM resources`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var resources []*models.Resource
	for rows.Next() {
		r := &models.Resource{}
		if err := rows.Scan(
			&r.ID, &r.Title, &r.Mime, &r.Filename, &r.CreatedTime, &r.UpdatedTime,
			&r.UserCreatedTime, &r.UserUpdatedTime, &r.FileExtension, &r.EncryptionCipherText,
			&r.EncryptionApplied, &r.EncryptionBlobEncrypted, &r.Size, &r.IsShared, &r.ShareID,
			&r.MasterKeyID, &r.UserData, &r.BlobUpdatedTime,
		); err != nil {
			return nil, err
		}
		r.Type_ = models.TypeResource
		resources = append(resources, r)
	}
	return resources, rows.Err()
}

// AllTags returns every tag.
func (db *DB) AllTags() ([]*models.Tag, error) {
	rows, err := db.Query(`SELECT id, title, created_time, updated_time,
		user_created_time, user_updated_time, encryption_cipher_text, encryption_applied,
		is_shared, parent_id, user_data
		FROM tags`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []*models.Tag
	for rows.Next() {
		t := &models.Tag{}
		if err := rows.Scan(
			&t.ID, &t.Title, &t.CreatedTime, &t.UpdatedTime,
			&t.UserCreatedTime, &t.UserUpdatedTime, &t.EncryptionCipherText, &t.EncryptionApplied,
			&t.IsShared, &t.ParentID, &t.UserData,
		); err != nil {
			return nil, err
		}
		t.Type_ = models.TypeTag
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// AllNoteTags returns every note/tag link.
func (db *DB) AllNoteTags() ([]*models.NoteTag, error) {
	rows, err := db.Query(`SELECT id, note_id, tag_id, created_time, updated_time,
		user_created_time, user_updated_time, encryption_cipher_text, encryption_applied, is_shared
		FROM note_tags`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var links []*models.NoteTag
	for rows.Next() {
		nt := &models.NoteTag{}
		if err := rows.Scan(
			&nt.ID, &nt.NoteID, &nt.TagID, &nt.CreatedTime, &nt.UpdatedTime,
			&nt.UserCreatedTime, &nt.UserUpdatedTime, &nt.EncryptionCipherText, &nt.EncryptionApplied, &nt.IsShared,
		); err != nil {
			return nil, err
		}
		nt.Type_ = models.TypeNoteTag
		links = append(links, nt)
	}
	return links, rows.Err()
}

// ImportBatch is a set of new items to insert. The import path resolves every
// id and title beforehand, so the write is a single transaction that either
// lands completely or not at all.
type ImportBatch struct {
	Folders   []*models.Folder
	Notes     []*models.Note
	Resources []*models.Resource
	Tags      []*models.Tag
	NoteTags  []*models.NoteTag
}

// ImportBatch inserts the batch atomically, recording each item as a change so
// the UI refreshes and the next sync uploads it.
func (db *DB) ImportBatch(b ImportBatch) error {
	return db.writeTx(func(ex execer) error {
		for _, f := range b.Folders {
			if err := UpsertFolderTx(ex, f); err != nil {
				return err
			}
			if err := recordItemChangeTx(ex, models.TypeFolder, f.ID, 1); err != nil {
				return err
			}
		}
		for _, n := range b.Notes {
			if err := UpsertNoteTx(ex, n); err != nil {
				return err
			}
			if err := recordItemChangeTx(ex, models.TypeNote, n.ID, 1); err != nil {
				return err
			}
		}
		for _, r := range b.Resources {
			if err := UpsertResourceTx(ex, r); err != nil {
				return err
			}
			if err := recordItemChangeTx(ex, models.TypeResource, r.ID, 1); err != nil {
				return err
			}
		}
		for _, t := range b.Tags {
			if err := UpsertTagTx(ex, t); err != nil {
				return err
			}
			if err := recordItemChangeTx(ex, models.TypeTag, t.ID, 1); err != nil {
				return err
			}
		}
		for _, nt := range b.NoteTags {
			if err := UpsertNoteTagTx(ex, nt); err != nil {
				return err
			}
			if err := recordItemChangeTx(ex, models.TypeNoteTag, nt.ID, 1); err != nil {
				return err
			}
		}
		return nil
	})
}

// FolderTitleTaken reports whether a live notebook with this title already
// exists under parentID. Imports use it to keep notebook names unique.
func (db *DB) FolderTitleTaken(title, parentID string) (bool, error) {
	var id string
	err := db.QueryRow(`SELECT id FROM folders WHERE title = ? AND parent_id = ? AND deleted_time = 0 LIMIT 1`,
		title, parentID).Scan(&id)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
