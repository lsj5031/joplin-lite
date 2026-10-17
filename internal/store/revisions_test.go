package store

import (
	"testing"

	"github.com/lsj5031/joplin-lite/internal/models"
)

// TestRevisionsTable_MigratesExistingDatabase pins the sidecar-schema fix: a
// database created at v49 *before* the revisions table existed must gain it
// on the next open (schemaStatements only run below v49).
func TestRevisionsTable_MigratesExistingDatabase(t *testing.T) {
	dir := t.TempDir()

	db, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := db.Exec(`DROP TABLE revisions`); err != nil {
		t.Fatalf("drop revisions: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = Open(dir) // already at schema version 49
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db.Close()

	rev := &models.Revision{
		ID:              "3c4d5e6f708192a3b4c5d6e7f8091a2b",
		ItemID:          "0d28d893565c4b2989301f2259164273",
		ItemType:        models.TypeNote,
		ItemUpdatedTime: 1772000000000,
	}
	if err := db.UpsertRevision(rev); err != nil {
		t.Fatalf("UpsertRevision after re-open: %v", err)
	}
	got, err := db.GetRevision(rev.ID)
	if err != nil || got == nil || got.ItemID != rev.ItemID {
		t.Fatalf("GetRevision = %+v (err=%v)", got, err)
	}
}
