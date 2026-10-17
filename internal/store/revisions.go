package store

import (
	"database/sql"

	"github.com/lsj5031/joplin-lite/internal/models"
)

// Note-history (revisions) storage: the index rows live in `revisions`, the
// authoritative item bytes in `item_raw` (raw passthrough).

// UpsertRevision stores a revision index row.
func (db *DB) UpsertRevision(r *models.Revision) error {
	_, err := db.Exec(`INSERT OR REPLACE INTO revisions
		(id, parent_id, item_id, item_type, item_updated_time, created_time, updated_time,
		encryption_applied, encryption_cipher_text)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.ParentID, r.ItemID, r.ItemType, r.ItemUpdatedTime, r.CreatedTime, r.UpdatedTime,
		r.EncryptionApplied, r.EncryptionCipherText,
	)
	return err
}

// GetRevision returns a revision index row (nil, nil when missing).
func (db *DB) GetRevision(id string) (*models.Revision, error) {
	r := &models.Revision{}
	err := db.QueryRow(`SELECT id, parent_id, item_id, item_type, item_updated_time,
		created_time, updated_time, encryption_applied, encryption_cipher_text
		FROM revisions WHERE id = ?`, id).Scan(
		&r.ID, &r.ParentID, &r.ItemID, &r.ItemType, &r.ItemUpdatedTime,
		&r.CreatedTime, &r.UpdatedTime, &r.EncryptionApplied, &r.EncryptionCipherText,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}

// ListRevisions returns revision index rows, newest first.
func (db *DB) ListRevisions(limit, offset int) ([]*models.Revision, error) {
	rows, err := db.Query(`SELECT id, parent_id, item_id, item_type, item_updated_time,
		created_time, updated_time, encryption_applied, encryption_cipher_text FROM revisions
		ORDER BY item_updated_time DESC, id ASC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.Revision
	for rows.Next() {
		r := &models.Revision{}
		if err := rows.Scan(&r.ID, &r.ParentID, &r.ItemID, &r.ItemType, &r.ItemUpdatedTime,
			&r.CreatedTime, &r.UpdatedTime, &r.EncryptionApplied, &r.EncryptionCipherText); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CountRevisions returns the total number of revision index rows.
func (db *DB) CountRevisions() (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM revisions`).Scan(&n)
	return n, err
}

// DeleteRevision removes a revision and queues the remote deletion (like the
// other typed delete paths).
func (db *DB) DeleteRevision(id string) error {
	if err := db.recordDeletion(id, models.TypeRevision); err != nil {
		return err
	}
	if _, err := db.Exec(`DELETE FROM revisions WHERE id = ?`, id); err != nil {
		return err
	}
	return db.DeleteItemRaw(id)
}
