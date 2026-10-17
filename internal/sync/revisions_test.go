package sync

import (
	"testing"

	"github.com/lsj5031/joplin-lite/internal/models"
)

// testRevisionItem is a serialized revision (metadata-only block, like
// Joplin's Revision model serializes it: the historical content is in the
// diff fields).
const testRevisionItem = `id: 3c4d5e6f708192a3b4c5d6e7f8091a2b
parent_id: 0d28d893565c4b2989301f2259164273
item_id: 0d28d893565c4b2989301f2259164273
item_type: 1
item_updated_time: 1772000000000
title_diff: [{"diffs":[[1,"Old title"]],"start1":0,"start2":0,"length1":0,"length2":9}]
body_diff: []
metadata_diff: {"new":{},"deleted":[]}
created_time: 2026-03-02T10:00:00.000Z
updated_time: 2026-03-02T10:00:00.000Z
user_created_time: 2026-03-02T10:00:00.000Z
user_updated_time: 2026-03-02T10:00:00.000Z
encryption_cipher_text: 
encryption_applied: 0
master_key_id: 
user_data: 
type_: 13`

const testRevisionID = "3c4d5e6f708192a3b4c5d6e7f8091a2b"

// TestDelta_RevisionPersistsRawAndPrunes: revision items used to be recorded
// as synced and thrown away; now they are indexed for /revisions and kept
// byte-exact in item_raw (raw passthrough — there is no revision serializer).
func TestDelta_RevisionPersistsRawAndPrunes(t *testing.T) {
	db := testDB(t)
	syncTarget := 9

	item := DeltaItem{ID: testRevisionID, ItemName: testRevisionID + ".md", Type: 1, UpdatedTime: 1772000000000}
	if err := applyDeltaItem(nil, db, syncTarget, item, []byte(testRevisionItem)); err != nil {
		t.Fatalf("applyDeltaItem: %v", err)
	}

	rev, err := db.GetRevision(testRevisionID)
	if err != nil || rev == nil {
		t.Fatalf("revision not indexed (rev=%v err=%v)", rev, err)
	}
	if rev.ItemID != "0d28d893565c4b2989301f2259164273" || rev.ItemType != 1 {
		t.Fatalf("revision index = %+v", rev)
	}
	if rev.ItemUpdatedTime != 1772000000000 {
		t.Fatalf("item_updated_time = %d", rev.ItemUpdatedTime)
	}
	raw, rawTime, ok, err := db.GetItemRaw(testRevisionID)
	if err != nil || !ok {
		t.Fatalf("raw copy missing (ok=%v err=%v)", ok, err)
	}
	if string(raw) != testRevisionItem {
		t.Fatalf("raw copy is not byte-exact:\n got: %q\nwant: %q", raw, testRevisionItem)
	}
	if rawTime != rev.UpdatedTime {
		t.Fatalf("raw timestamp = %d, want %d", rawTime, rev.UpdatedTime)
	}

	// Redelivery is idempotent.
	if err := applyDeltaItem(nil, db, syncTarget, item, []byte(testRevisionItem)); err != nil {
		t.Fatalf("applyDeltaItem (redelivery): %v", err)
	}

	// Remote deletion prunes index + raw (DeleteLocalItem knows revisions).
	if err := handleRemoteDelete(db, syncTarget, testRevisionID, models.TypeRevision); err != nil {
		t.Fatalf("handleRemoteDelete: %v", err)
	}
	rev, err = db.GetRevision(testRevisionID)
	if err != nil {
		t.Fatalf("GetRevision after prune: %v", err)
	}
	if rev != nil {
		t.Fatalf("revision still indexed after remote delete: %+v", rev)
	}
	if _, _, ok, _ := db.GetItemRaw(testRevisionID); ok {
		t.Fatal("raw copy still present after remote delete")
	}
}
