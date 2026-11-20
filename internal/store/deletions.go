package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/lsj5031/joplin-lite/internal/models"
)

// Keep a record for every target that saw the item, but bind only the copy
// for the currently selected, identified remote. An empty scope is quarantine.
func (db *DB) recordDeletion(itemID string, itemType int) error {
	return recordDeletionTx(db, itemID, itemType)
}

func recordDeletionTx(ex execer, itemID string, itemType int) error {
	_, err := ex.Exec(`INSERT INTO deleted_items (item_type, item_id, deleted_time, sync_target, jl_scope)
		SELECT DISTINCT ?, ?, ?, si.sync_target, COALESCE(t.scope, '')
		FROM sync_items si LEFT JOIN jl_sync_targets t ON t.sync_target = si.sync_target AND t.active = 1
		WHERE si.item_id = ?`,
		itemType, itemID, time.Now().UnixMilli(), itemID)
	return err
}

// SelectDeleteTarget binds future deletes to one remote. Changing selections
// quarantines outstanding rows permanently, even if the remote is selected
// again. First use after an upgrade preserves the existing cursor and sync
// points; legacy rows are already quarantined by the migration.
func (db *DB) SelectDeleteTarget(target int, scope string) (int64, error) {
	var quarantined int64
	err := db.writeTx(func(ex execer) error {
		var activeTarget int
		var activeScope string
		err := ex.QueryRow(`SELECT sync_target, scope FROM jl_sync_targets WHERE active = 1`).Scan(&activeTarget, &activeScope)
		switch {
		case err == nil:
			if activeTarget == target && activeScope == scope {
				return nil
			}
			if err := saveNoteSwitchBaselines(ex, activeTarget); err != nil {
				return err
			}
			result, err := ex.Exec(`UPDATE deleted_items SET jl_scope = '' WHERE jl_scope != ''`)
			if err != nil {
				return err
			}
			quarantined, err = result.RowsAffected()
			if err != nil {
				return err
			}
			if _, err := ex.Exec(`DELETE FROM key_values WHERE key = 'sync.cursor'`); err != nil {
				return err
			}
			if _, err := ex.Exec(`DELETE FROM jl_sync_downloads`); err != nil {
				return err
			}
		case err != sql.ErrNoRows:
			return err
		}

		// A different remote of the same provider must not use the old
		// remote's sync points to prune local items missing from its listing.
		var previousScope string
		err = ex.QueryRow(`SELECT scope FROM jl_sync_targets WHERE sync_target = ?`, target).Scan(&previousScope)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == nil && previousScope != scope {
			if activeTarget != target {
				if err := saveNoteSwitchBaselines(ex, target); err != nil {
					return err
				}
			}
			if _, err := ex.Exec(`DELETE FROM sync_items WHERE sync_target = ?`, target); err != nil {
				return err
			}
		}
		if _, err := ex.Exec(`UPDATE jl_sync_targets SET active = 0 WHERE active = 1`); err != nil {
			return err
		}
		_, err = ex.Exec(`INSERT OR REPLACE INTO jl_sync_targets (sync_target, scope, active) VALUES (?, ?, 1)`, target, scope)
		return err
	})
	return quarantined, err
}

func saveNoteSwitchBaselines(ex execer, target int) error {
	_, err := ex.Exec(`INSERT INTO jl_note_switch_baselines (note_id, sync_time)
		SELECT n.id, si.sync_time FROM sync_items si JOIN notes n ON n.id = si.item_id
		WHERE si.sync_target = ? AND si.item_type = ?
		ON CONFLICT(note_id) DO UPDATE SET sync_time = MIN(sync_time, excluded.sync_time)`,
		target, models.TypeNote)
	return err
}

// NoteNeedsTargetConflict checks for an edit made since an earlier target's
// baseline when there is no sync point for the selected remote.
func (db *DB) NoteNeedsTargetConflict(noteID string, target int) (bool, error) {
	var pending bool
	err := db.QueryRow(`SELECT EXISTS (
		SELECT 1 FROM jl_note_switch_baselines b JOIN notes n ON n.id = b.note_id
		WHERE b.note_id = ? AND n.updated_time > b.sync_time AND NOT EXISTS (
			SELECT 1 FROM sync_items WHERE item_id = b.note_id AND sync_target = ?
		))`, noteID, target).Scan(&pending)
	return pending, err
}

func clearNoteSwitchBaseline(ex execer, noteID string) error {
	_, err := ex.Exec(`DELETE FROM jl_note_switch_baselines WHERE note_id = ?`, noteID)
	return err
}

// PushableDeletedItems returns only deletes bound to the current selection.
// GetDeletedItems also exposes held rows for inspection and recovery.
func (db *DB) PushableDeletedItems(target int, scope string) ([]*models.DeletedItem, error) {
	if scope == "" {
		return nil, nil
	}
	return db.listDeletedItems(`WHERE sync_target = ? AND jl_scope = ? AND EXISTS (
		SELECT 1 FROM jl_sync_targets WHERE sync_target = ? AND scope = ? AND active = 1
	)`, target, scope, target, scope)
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
