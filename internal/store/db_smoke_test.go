package store

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/lsj5031/joplin-lite/internal/models"
)

// M0 store smoke test for the cgo-free modernc.org/sqlite driver: WAL,
// FTS5 search (trigger-maintained external-content index), and resource
// blob round-trip through the new driver.

func TestOpen_WALMode(t *testing.T) {
	db := testDB(t)
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want %q", mode, "wal")
	}
}

func TestSearchNotes_FTS5RoundTrip(t *testing.T) {
	db := testDB(t)

	mk := func(id, title, body string) *models.Note {
		return &models.Note{ID: id, Title: title, Body: body}
	}
	if err := db.CreateNote(mk("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "First note", "the needle is here")); err != nil {
		t.Fatalf("CreateNote 1: %v", err)
	}
	if err := db.CreateNote(mk("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "Second note", "haystack only")); err != nil {
		t.Fatalf("CreateNote 2: %v", err)
	}

	notes, hasMore, err := db.SearchNotes("needle", 10, 0)
	if err != nil {
		t.Fatalf("SearchNotes: %v", err)
	}
	if hasMore || len(notes) != 1 || notes[0].ID != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("search 'needle' = %d notes (hasMore=%v), want exactly the first note", len(notes), hasMore)
	}

	// Update re-indexes: old terms must drop out, new terms must match.
	n := notes[0]
	n.Body = "now with a stapler"
	if err := db.UpdateNote(n); err != nil {
		t.Fatalf("UpdateNote: %v", err)
	}
	if notes, _, err := db.SearchNotes("needle", 10, 0); err != nil || len(notes) != 0 {
		t.Fatalf("after update, search 'needle' = %d notes, err=%v; want 0", len(notes), err)
	}
	if notes, _, err := db.SearchNotes("stapler", 10, 0); err != nil || len(notes) != 1 || notes[0].ID != n.ID {
		t.Fatalf("after update, search 'stapler' = %d notes, err=%v; want exactly the updated note", len(notes), err)
	}

	// Delete removes the row from the index too.
	if err := db.DeleteNote(n.ID); err != nil {
		t.Fatalf("DeleteNote: %v", err)
	}
	if notes, _, err := db.SearchNotes("stapler", 10, 0); err != nil || len(notes) != 0 {
		t.Fatalf("after delete, search 'stapler' = %d notes, err=%v; want 0", len(notes), err)
	}
}

func TestResourceBlobRoundTrip(t *testing.T) {
	for _, tc := range []struct{ name, extension, suffix string }{
		{"lite", "", ""},
		{"official", "png", ".png"},
		{"legacy-extensionless", "png", ""},
		{"invalid-extension", "../../outside", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := testDB(t)
			id := "cccccccccccccccccccccccccccccccc"
			r := &models.Resource{ID: id, Title: "photo.png", Mime: "image/png", FileExtension: tc.extension}
			if err := db.CreateResource(r); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(db.ResourceDir(), id+tc.suffix)
			if err := os.WriteFile(path, []byte("old content"), 0o600); err != nil {
				t.Fatal(err)
			}
			if got, err := os.ReadFile(db.GetResourceFile(id)); err != nil || string(got) != "old content" {
				t.Fatalf("existing resource read = %q, %v", got, err)
			}
			if tc.name == "invalid-extension" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			blob := []byte{0x89, 'P', 'N', 'G', 0, 1, 2, 3, 0xff}
			if err := db.SaveResourceFile(id, blob); err != nil {
				t.Fatal(err)
			}
			if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, blob) {
				t.Fatalf("resource not updated in place: %v, %v", got, err)
			}
			if err := db.DeleteResource(id); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("blob file still exists after DeleteResource (stat err=%v)", err)
			}
		})
	}
}
