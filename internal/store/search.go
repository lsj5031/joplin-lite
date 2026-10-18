package store

import (
	"github.com/lsj5031/joplin-lite/internal/models"
)

// SearchNotes performs a full-text search on notes using the local FTS5 index
// (jl_notes_fts — distinct from a real Joplin profile's FTS4 notes_fts).
// Conflict copies are excluded, matching ListNotes' default.
func (db *DB) SearchNotes(query string, limit, offset int) ([]*models.Note, bool, error) {
	return db.SearchNotesFiltered(query, limit, offset, false)
}

// SearchNotesFiltered is SearchNotes with the Data API's include_conflicts
// semantics: conflicts are hidden unless includeConflicts is set.
func (db *DB) SearchNotesFiltered(query string, limit, offset int, includeConflicts bool) ([]*models.Note, bool, error) {
	if limit <= 0 {
		limit = 10
	}

	fts := ftsQuery(query)
	if fts == "" {
		return []*models.Note{}, false, nil
	}

	where := `jl_notes_fts MATCH ?`
	if !includeConflicts {
		where += ` AND n.is_conflict = 0`
	}

	// External-content FTS5 rows are joined by rowid, not by the content
	// table's id: `id` is declared UNINDEXED, so `n.id = fts.id` forces a
	// full scan of the index. rowid is the join key the table is built on.
	rows, err := db.queryStmt(`SELECT n.id, n.parent_id, n.title, n.body, n.created_time, n.updated_time,
		n.is_conflict, n.latitude, n.longitude, n.altitude, n.author, n.source_url,
		n.is_todo, n.todo_due, n.todo_completed, n.source, n.source_application, n.application_data,
		n."order", n.user_created_time, n.user_updated_time, n.encryption_cipher_text,
		n.encryption_applied, n.markup_language, n.is_shared, n.share_id, n.conflict_original_id,
		n.master_key_id, n.user_data, n.deleted_time
		FROM notes n
		JOIN jl_notes_fts ON n.rowid = jl_notes_fts.rowid
		WHERE `+where+`
		ORDER BY n.updated_time DESC
		LIMIT ? OFFSET ?`, fts, limit+1, offset)
	if err != nil {
		return nil, false, err
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
			return nil, false, err
		}
		n.Type_ = models.TypeNote
		notes = append(notes, n)
	}

	hasMore := len(notes) > limit
	if hasMore {
		notes = notes[:limit]
	}
	return notes, hasMore, nil
}

// SearchFolders searches folders by title.
func (db *DB) SearchFolders(query string, limit, offset int) ([]*models.Folder, bool, error) {
	if limit <= 0 {
		limit = 10
	}

	// Prefix matching (query%) can use the title index; "%query%" cannot and
	// scanned every folder. Titles are short, so prefix is also the more
	// predictable result set for the sidebar.
	rows, err := db.queryStmt(`SELECT id, title, created_time, updated_time,
		user_created_time, user_updated_time, encryption_cipher_text, encryption_applied,
		parent_id, is_shared, share_id, master_key_id, icon, user_data, deleted_time
		FROM folders WHERE title LIKE ? COLLATE NOCASE ORDER BY title ASC LIMIT ? OFFSET ?`,
		query+"%", limit+1, offset)
	if err != nil {
		return nil, false, err
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
			return nil, false, err
		}
		f.Type_ = models.TypeFolder
		folders = append(folders, f)
	}

	hasMore := len(folders) > limit
	if hasMore {
		folders = folders[:limit]
	}
	return folders, hasMore, nil
}

// SearchTags searches tags by title.
func (db *DB) SearchTags(query string, limit, offset int) ([]*models.Tag, bool, error) {
	if limit <= 0 {
		limit = 10
	}

	// Prefix like folders: indexed and predictable.
	rows, err := db.queryStmt(`SELECT id, title, created_time, updated_time,
		user_created_time, user_updated_time, encryption_cipher_text, encryption_applied,
		is_shared, parent_id, user_data
		FROM tags WHERE title LIKE ? COLLATE NOCASE ORDER BY title ASC LIMIT ? OFFSET ?`,
		query+"%", limit+1, offset)
	if err != nil {
		return nil, false, err
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
			return nil, false, err
		}
		t.Type_ = models.TypeTag
		tags = append(tags, t)
	}

	hasMore := len(tags) > limit
	if hasMore {
		tags = tags[:limit]
	}
	return tags, hasMore, nil
}
