package store

import (
	"database/sql"
	"time"

	"github.com/lsj5031/joplin-lite/internal/models"
)

// GetSyncCursor returns the stored delta sync cursor.
func (db *DB) GetSyncCursor() (string, error) {
	var cursor string
	err := db.QueryRow("SELECT value FROM key_values WHERE key = 'sync.cursor'").Scan(&cursor)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return cursor, err
}

// SetSyncCursor persists the delta sync cursor.
func (db *DB) SetSyncCursor(cursor string) error {
	_, err := db.Exec(`INSERT OR REPLACE INTO key_values (key, value, type, updated_time) VALUES ('sync.cursor', ?, 1, strftime('%s','now') * 1000)`, cursor)
	return err
}

// DownloadCheckpoint records an applied remote revision, not a sync cursor.
// Resource bytes are checked against LocalHash before reusing the receipt.
type DownloadCheckpoint struct {
	Revision  string
	LocalHash string
}

func (db *DB) DownloadCheckpoints(scope string) (map[string]DownloadCheckpoint, error) {
	rows, err := db.Query(`SELECT item_name, revision, local_hash FROM jl_sync_downloads WHERE scope = ?`, scope)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make(map[string]DownloadCheckpoint)
	for rows.Next() {
		var name string
		var item DownloadCheckpoint
		if err := rows.Scan(&name, &item.Revision, &item.LocalHash); err != nil {
			return nil, err
		}
		items[name] = item
	}
	return items, rows.Err()
}

func (db *DB) SaveDownloadCheckpoint(scope, name string, item DownloadCheckpoint) error {
	_, err := db.Exec(`INSERT OR REPLACE INTO jl_sync_downloads (scope, item_name, revision, local_hash) VALUES (?, ?, ?, ?)`,
		scope, name, item.Revision, item.LocalHash)
	return err
}

func (db *DB) ClearDownloadCheckpoints(scope string) error {
	_, err := db.Exec(`DELETE FROM jl_sync_downloads WHERE scope = ?`, scope)
	return err
}

// GetActiveMasterKeyID returns the active E2EE master key ID from sync info (empty if not set).
func (db *DB) GetActiveMasterKeyID() (string, error) {
	var id string
	err := db.QueryRow("SELECT value FROM key_values WHERE key = 'sync.active_master_key_id'").Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id, err
}

// SetActiveMasterKeyID stores the active E2EE master key ID from info.json.
func (db *DB) SetActiveMasterKeyID(id string) error {
	_, err := db.Exec(`INSERT OR REPLACE INTO key_values (key, value, type, updated_time) VALUES ('sync.active_master_key_id', ?, 1, strftime('%s','now') * 1000)`, id)
	return err
}

// GetSyncItem returns the sync state for a specific item.
func (db *DB) GetSyncItem(itemID string, syncTarget int) (*models.SyncItem, error) {
	// Hot path: called for every pulled item, so the statement is cached.
	rows, err := db.queryStmt(`SELECT id, sync_target, sync_time, item_type, item_id,
		sync_disabled, sync_disabled_reason, force_sync, item_location
		FROM sync_items WHERE item_id = ? AND sync_target = ?`, itemID, syncTarget)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	si := &models.SyncItem{}
	if err := rows.Scan(
		&si.ID, &si.SyncTarget, &si.SyncTime, &si.ItemType, &si.ItemID,
		&si.SyncDisabled, &si.SyncDisabledReason, &si.ForceSync, &si.ItemLocation,
	); err != nil {
		return nil, err
	}
	return si, rows.Err()
}

// UpsertSyncItem records that an item has been synced, using the current
// wall-clock time as the sync point.
func (db *DB) UpsertSyncItem(itemID string, itemType, syncTarget int) error {
	return upsertSyncItemTx(db, itemID, itemType, syncTarget, time.Now().UnixMilli())
}

// UpsertSyncItemAt records that the given revision of an item has been synced.
// The sync point must be the item's updated_time, not the wall clock: push
// selects rows with updated_time > sync_time, so a wall-clock sync point that
// lands on (or after) a local edit's timestamp makes that edit invisible and
// silently drops it.
func (db *DB) UpsertSyncItemAt(itemID string, itemType, syncTarget int, syncTime int64) error {
	return upsertSyncItemTx(db, itemID, itemType, syncTarget, syncTime)
}

func upsertSyncItemTx(ex execer, itemID string, itemType, syncTarget int, syncTime int64) error {
	var one int
	err := ex.QueryRow(`SELECT 1 FROM sync_items WHERE item_id = ? AND sync_target = ?`,
		itemID, syncTarget).Scan(&one)
	switch {
	case err == nil:
		_, err = ex.Exec(`UPDATE sync_items SET sync_time = ? WHERE item_id = ? AND sync_target = ?`,
			syncTime, itemID, syncTarget)
	case err == sql.ErrNoRows:
		_, err = ex.Exec(`INSERT INTO sync_items (sync_target, sync_time, item_type, item_id, sync_disabled, sync_disabled_reason, force_sync, item_location)
			VALUES (?, ?, ?, ?, 0, '', 0, 0)`,
			syncTarget, syncTime, itemType, itemID)
	}
	return err
}

// AddDeletedItem records an item that needs to be deleted on the server.
func (db *DB) AddDeletedItem(itemID string, itemType, syncTarget int) error {
	now := time.Now().UnixMilli()
	_, err := db.Exec(`INSERT INTO deleted_items (item_type, item_id, deleted_time, sync_target)
		VALUES (?, ?, ?, ?)`, itemType, itemID, now, syncTarget)
	return err
}

// GetDeletedItems returns items that need to be deleted from the server.
func (db *DB) GetDeletedItems(syncTarget int) ([]*models.DeletedItem, error) {
	rows, err := db.Query(`SELECT id, item_type, item_id, deleted_time, sync_target
		FROM deleted_items WHERE sync_target = ?`, syncTarget)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*models.DeletedItem
	for rows.Next() {
		di := &models.DeletedItem{}
		if err := rows.Scan(&di.ID, &di.ItemType, &di.ItemID, &di.DeletedTime, &di.SyncTarget); err != nil {
			return nil, err
		}
		items = append(items, di)
	}
	return items, nil
}

// RemoveDeletedItem removes a deleted item record after it's been synced.
func (db *DB) RemoveDeletedItem(id int) error {
	_, err := db.Exec("DELETE FROM deleted_items WHERE id = ?", id)
	return err
}

// ListSyncItemIDs returns all (item_id, item_type) pairs for a given sync target.
func (db *DB) ListSyncItemIDs(syncTarget int) ([]models.SyncItem, error) {
	rows, err := db.Query("SELECT item_id, item_type FROM sync_items WHERE sync_target = ?", syncTarget)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []models.SyncItem
	for rows.Next() {
		var si models.SyncItem
		if err := rows.Scan(&si.ItemID, &si.ItemType); err != nil {
			return nil, err
		}
		items = append(items, si)
	}
	return items, nil
}

// DeleteLocalItem removes an item from the local database (called during sync pull).
// When itemType is 0 (unknown, e.g. from a delta delete where the .md filename
// doesn't encode the type), it looks up the type from the sync_items table.
func (db *DB) DeleteLocalItem(itemID string, itemType int) error {
	if itemType == 0 {
		var resolved int
		err := db.QueryRow("SELECT item_type FROM sync_items WHERE item_id = ? LIMIT 1", itemID).Scan(&resolved)
		if err == sql.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		itemType = resolved
	}

	// Clear sync_items before the typed delete: the typed paths queue remote
	// deletions based on sync_items rows, and a delete that came from the
	// server must never echo back to it.
	if _, err := db.Exec("DELETE FROM sync_items WHERE item_id = ?", itemID); err != nil {
		return err
	}

	var err error
	switch itemType {
	case models.TypeNote:
		_, err = db.Exec("DELETE FROM notes WHERE id = ?", itemID)
	case models.TypeFolder:
		_, err = db.Exec("DELETE FROM folders WHERE id = ?", itemID)
	case models.TypeTag:
		_, err = db.Exec("DELETE FROM tags WHERE id = ?", itemID)
	case models.TypeNoteTag:
		_, err = db.Exec("DELETE FROM note_tags WHERE id = ?", itemID)
	case models.TypeResource:
		err = db.DeleteResource(itemID)
	case models.TypeRevision:
		err = db.DeleteRevision(itemID)
	}
	if err != nil {
		return err
	}

	return db.DeleteItemRaw(itemID)
}
