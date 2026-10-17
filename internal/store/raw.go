package store

import "database/sql"

// item_raw keeps the exact bytes of items as pulled from the sync target, so
// unmodified items are pushed back byte-for-byte (losslessness by
// construction). Modified items are re-serialized from the raw text so unknown
// metadata keys survive in their original positions.

// SaveItemRaw stores the exact bytes of an item along with the item's
// updated_time at capture time.
func (db *DB) SaveItemRaw(id string, raw []byte, updatedTime int64) error {
	return saveItemRawTx(db, id, raw, updatedTime)
}

func saveItemRawTx(ex execer, id string, raw []byte, updatedTime int64) error {
	_, err := ex.Exec(`INSERT OR REPLACE INTO item_raw (id, raw, updated_time) VALUES (?, ?, ?)`, id, raw, updatedTime)
	return err
}

// GetItemRaw returns the stored raw bytes for an item and the updated_time
// recorded at capture time. ok is false when no raw copy exists.
func (db *DB) GetItemRaw(id string) (raw []byte, updatedTime int64, ok bool, err error) {
	err = db.QueryRow(`SELECT raw, updated_time FROM item_raw WHERE id = ?`, id).Scan(&raw, &updatedTime)
	if err == sql.ErrNoRows {
		return nil, 0, false, nil
	}
	if err != nil {
		return nil, 0, false, err
	}
	return raw, updatedTime, true, nil
}

// DeleteItemRaw drops the raw copy of an item.
func (db *DB) DeleteItemRaw(id string) error {
	_, err := db.Exec(`DELETE FROM item_raw WHERE id = ?`, id)
	return err
}
