package store

import (
	"fmt"
	"time"
)

// recordDeletion queues an item's remote deletion: one deleted_items row per
// sync target the item has ever reached (a sync_items row means the server
// knows the item, whether it was pushed or pulled). Push sends these deletes
// and clears the rows. Items that never synced produce no rows — the server
// has never seen them, so there is nothing to delete.
func (db *DB) recordDeletion(itemID string, itemType int) error {
	return recordDeletionTx(db, itemID, itemType)
}

func recordDeletionTx(ex execer, itemID string, itemType int) error {
	_, err := ex.Exec(`INSERT INTO deleted_items (item_type, item_id, deleted_time, sync_target)
		SELECT DISTINCT ?, ?, ?, sync_target FROM sync_items WHERE item_id = ?`,
		itemType, itemID, time.Now().UnixMilli(), itemID)
	return err
}

// noteTagIDs returns the IDs of note-tag links for a note (column "note_id")
// or a tag (column "tag_id") so callers can queue their remote deletion
// before the rows are removed. The column argument is restricted to those two
// literals — it is never user input.
func (db *DB) noteTagIDs(column, value string) ([]string, error) {
	return noteTagIDsTx(db, column, value)
}

func noteTagIDsTx(ex execer, column, value string) ([]string, error) {
	if column != "note_id" && column != "tag_id" {
		return nil, fmt.Errorf("store: invalid note_tags column %q", column)
	}
	rows, err := ex.Query("SELECT id FROM note_tags WHERE "+column+" = ?", value)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
