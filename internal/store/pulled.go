package store

import (
	"fmt"

	"github.com/lsj5031/joplin-lite/internal/models"
)

// ApplyPulledItem commits one pulled item in a single transaction: the typed
// row, the sync_items point, and the raw bytes that make a later push
// lossless. On a first sync this is the difference between three commits per
// item and one, and a crash can never leave an item indexed but missing its
// raw copy.
func (db *DB) ApplyPulledItem(item any, syncTarget int, raw []byte) error {
	return db.writeTx(func(ex execer) error {
		var (
			id         string
			itemType   int
			updated    int64
			upsertErr  error
		)

		switch v := item.(type) {
		case *models.Note:
			upsertErr = UpsertNoteTx(ex, v)
			id, itemType, updated = v.ID, models.TypeNote, v.UpdatedTime
		case *models.Folder:
			upsertErr = UpsertFolderTx(ex, v)
			id, itemType, updated = v.ID, models.TypeFolder, v.UpdatedTime
		case *models.Tag:
			upsertErr = UpsertTagTx(ex, v)
			id, itemType, updated = v.ID, models.TypeTag, v.UpdatedTime
		case *models.NoteTag:
			upsertErr = UpsertNoteTagTx(ex, v)
			id, itemType, updated = v.ID, models.TypeNoteTag, v.UpdatedTime
		case *models.Resource:
			upsertErr = UpsertResourceTx(ex, v)
			id, itemType, updated = v.ID, models.TypeResource, v.UpdatedTime
		case *models.MasterKey:
			upsertErr = UpsertMasterKeyTx(ex, v)
			id, itemType, updated = v.ID, models.TypeMasterKey, v.UpdatedTime
		case *models.Revision:
			upsertErr = UpsertRevisionTx(ex, v)
			id, itemType, updated = v.ID, models.TypeRevision, v.UpdatedTime
		default:
			return fmt.Errorf("store: unsupported pulled item %T", item)
		}
		if upsertErr != nil {
			return upsertErr
		}
		if err := upsertSyncItemTx(ex, id, itemType, syncTarget, updated); err != nil {
			return err
		}
		return saveItemRawTx(ex, id, raw, updated)
	})
}
