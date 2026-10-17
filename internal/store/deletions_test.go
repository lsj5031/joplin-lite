package store

import (
	"testing"

	"github.com/lsj5031/joplin-lite/internal/models"
)

// Deletion propagation wiring: a local delete queues one deleted_items row
// per sync target that has seen the item (sync_items), so push can delete the
// remote copies. Never-synced items queue nothing, and deletes that came from
// the server (DeleteLocalItem) must never echo back to it.

// deletedIDs maps item ID -> item type queued for deletion on a target.
func deletedIDs(t *testing.T, db *DB, target int) map[string]int {
	t.Helper()
	items, err := db.GetDeletedItems(target)
	if err != nil {
		t.Fatalf("GetDeletedItems(%d): %v", target, err)
	}
	out := map[string]int{}
	for _, di := range items {
		out[di.ItemID] = di.ItemType
	}
	return out
}

const (
	noteID = "0d28d893565c4b2989301f2259164273"
	tagID  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa1"
	resID  = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb2"
	folID  = "ccccccccccccccccccccccccccccccc3"
)

func TestDeleteNote_QueuesRemoteDeletion(t *testing.T) {
	db := testDB(t)

	if err := db.CreateNote(&models.Note{ID: noteID, Title: "note", Body: "body"}); err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	if err := db.CreateTag(&models.Tag{ID: tagID, Title: "tag"}); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	if err := db.AddNoteTag(noteID, tagID); err != nil {
		t.Fatalf("AddNoteTag: %v", err)
	}
	var ntID string
	if err := db.QueryRow("SELECT id FROM note_tags WHERE note_id = ?", noteID).Scan(&ntID); err != nil {
		t.Fatalf("note-tag id: %v", err)
	}

	// The note reached two sync targets; the link only one.
	for _, target := range []int{7, 8} {
		if err := db.UpsertSyncItem(noteID, models.TypeNote, target); err != nil {
			t.Fatalf("UpsertSyncItem(note, %d): %v", target, err)
		}
	}
	if err := db.UpsertSyncItem(ntID, models.TypeNoteTag, 7); err != nil {
		t.Fatalf("UpsertSyncItem(note-tag): %v", err)
	}

	if err := db.DeleteNote(noteID); err != nil {
		t.Fatalf("DeleteNote: %v", err)
	}

	d7 := deletedIDs(t, db, 7)
	if d7[noteID] != models.TypeNote {
		t.Errorf("target 7: note deletion not queued (got type %d)", d7[noteID])
	}
	if d7[ntID] != models.TypeNoteTag {
		t.Errorf("target 7: note-tag link deletion not queued (got type %d)", d7[ntID])
	}
	d8 := deletedIDs(t, db, 8)
	if d8[noteID] != models.TypeNote {
		t.Errorf("target 8: note deletion not queued (got type %d)", d8[noteID])
	}
	if _, ok := d8[ntID]; ok {
		t.Error("target 8: note-tag link queued although that target never saw it")
	}
}

func TestDeleteNote_NeverSynced_QueuesNothing(t *testing.T) {
	db := testDB(t)

	if err := db.CreateNote(&models.Note{ID: noteID, Title: "local only", Body: "b"}); err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	if err := db.DeleteNote(noteID); err != nil {
		t.Fatalf("DeleteNote: %v", err)
	}
	for _, target := range []int{7, 8} {
		if got := deletedIDs(t, db, target); len(got) != 0 {
			t.Errorf("target %d: queued %v for an item the server never saw", target, got)
		}
	}
}

func TestRemoveNoteTag_QueuesLinkDeletion(t *testing.T) {
	db := testDB(t)

	if err := db.CreateNote(&models.Note{ID: noteID, Title: "n", Body: "b"}); err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	if err := db.CreateTag(&models.Tag{ID: tagID, Title: "tag"}); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	if err := db.AddNoteTag(noteID, tagID); err != nil {
		t.Fatalf("AddNoteTag: %v", err)
	}
	var ntID string
	if err := db.QueryRow("SELECT id FROM note_tags WHERE note_id = ?", noteID).Scan(&ntID); err != nil {
		t.Fatalf("note-tag id: %v", err)
	}
	if err := db.UpsertSyncItem(ntID, models.TypeNoteTag, 7); err != nil {
		t.Fatalf("UpsertSyncItem: %v", err)
	}

	if err := db.RemoveNoteTag(noteID, tagID); err != nil {
		t.Fatalf("RemoveNoteTag: %v", err)
	}
	if got := deletedIDs(t, db, 7); got[ntID] != models.TypeNoteTag {
		t.Errorf("note-tag link deletion not queued (got %v)", got)
	}
}

func TestDeleteTag_QueuesTagAndLinkDeletions(t *testing.T) {
	db := testDB(t)

	if err := db.CreateNote(&models.Note{ID: noteID, Title: "n", Body: "b"}); err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	if err := db.CreateTag(&models.Tag{ID: tagID, Title: "tag"}); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	if err := db.AddNoteTag(noteID, tagID); err != nil {
		t.Fatalf("AddNoteTag: %v", err)
	}
	var ntID string
	if err := db.QueryRow("SELECT id FROM note_tags WHERE note_id = ?", noteID).Scan(&ntID); err != nil {
		t.Fatalf("note-tag id: %v", err)
	}
	if err := db.UpsertSyncItem(tagID, models.TypeTag, 7); err != nil {
		t.Fatalf("UpsertSyncItem(tag): %v", err)
	}
	if err := db.UpsertSyncItem(ntID, models.TypeNoteTag, 7); err != nil {
		t.Fatalf("UpsertSyncItem(note-tag): %v", err)
	}

	if err := db.DeleteTag(tagID); err != nil {
		t.Fatalf("DeleteTag: %v", err)
	}
	got := deletedIDs(t, db, 7)
	if got[tagID] != models.TypeTag {
		t.Errorf("tag deletion not queued (got %v)", got)
	}
	if got[ntID] != models.TypeNoteTag {
		t.Errorf("note-tag link deletion not queued (got %v)", got)
	}
}

func TestDeleteResourceAndFolder_QueueRemoteDeletion(t *testing.T) {
	db := testDB(t)

	if err := db.CreateResource(&models.Resource{ID: resID, Title: "blob"}); err != nil {
		t.Fatalf("CreateResource: %v", err)
	}
	if err := db.CreateFolder(&models.Folder{ID: folID, Title: "folder"}); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if err := db.UpsertSyncItem(resID, models.TypeResource, 7); err != nil {
		t.Fatalf("UpsertSyncItem(resource): %v", err)
	}
	if err := db.UpsertSyncItem(folID, models.TypeFolder, 7); err != nil {
		t.Fatalf("UpsertSyncItem(folder): %v", err)
	}

	if err := db.DeleteResource(resID); err != nil {
		t.Fatalf("DeleteResource: %v", err)
	}
	if err := db.DeleteFolder(folID); err != nil {
		t.Fatalf("DeleteFolder: %v", err)
	}
	got := deletedIDs(t, db, 7)
	if got[resID] != models.TypeResource {
		t.Errorf("resource deletion not queued (got %v)", got)
	}
	if got[folID] != models.TypeFolder {
		t.Errorf("folder deletion not queued (got %v)", got)
	}
}

func TestDeleteLocalItem_DoesNotEchoDeletion(t *testing.T) {
	db := testDB(t)

	if err := db.CreateResource(&models.Resource{ID: resID, Title: "blob"}); err != nil {
		t.Fatalf("CreateResource: %v", err)
	}
	if err := db.UpsertSyncItem(resID, models.TypeResource, 7); err != nil {
		t.Fatalf("UpsertSyncItem: %v", err)
	}

	// The server told us this item is gone; applying that must not queue a
	// delete back to any target.
	if err := db.DeleteLocalItem(resID, models.TypeResource); err != nil {
		t.Fatalf("DeleteLocalItem: %v", err)
	}
	if got := deletedIDs(t, db, 7); len(got) != 0 {
		t.Errorf("server-driven delete echoed back as %v", got)
	}
}
