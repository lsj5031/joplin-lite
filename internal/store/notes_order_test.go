package store

import (
	"testing"

	"github.com/lsj5031/joplin-lite/internal/models"
)

// TestNoteFractionalOrder is a regression guard for real Joplin profiles: the
// `notes.order` column holds a NUMERIC that Joplin sometimes writes as a
// fraction (conflict ordering). Scanning it into an int64 failed the whole note
// list with "converting driver.Value type float64 ... to a int64". Order is a
// float64 and must survive both the write and the read path.
func TestNoteFractionalOrder(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	const fractional = 12615.228260740638
	note := &models.Note{ID: "frac-note", Title: "fractional order", Order: fractional}
	if err := db.CreateNote(note); err != nil {
		t.Fatalf("CreateNote: %v", err)
	}

	got, err := db.GetNote("frac-note")
	if err != nil {
		t.Fatalf("GetNote: %v", err)
	}
	if got == nil || got.Order != fractional {
		t.Fatalf("GetNote order = %v, want %v", got, fractional)
	}

	list, _, err := db.ListNotes("order", "ASC", true, 10, 0)
	if err != nil {
		t.Fatalf("ListNotes: %v", err)
	}
	if len(list) != 1 || list[0].Order != fractional {
		t.Fatalf("ListNotes order = %+v, want the fractional value", list)
	}
}
