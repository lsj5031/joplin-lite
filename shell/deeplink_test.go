package main

import (
	"strings"
	"testing"

	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/service"
)

func TestJoplinNoteID(t *testing.T) {
	const id = "bca23b7573e156f6c428fb9a9f0d0fc7"
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"canonical", "joplin://x-callback-url/openNote?id=" + id, id},
		{"upper-case hex", "joplin://x-callback-url/openNote?id=" + strings.ToUpper(id), id},
		{"without the x-callback-url host", "joplin://openNote?id=" + id, id},
		{"trailing slash", "joplin://x-callback-url/openNote/?id=" + id, id},
		{"callback parameters", "joplin://x-callback-url/openNote?id=" + id + "&x-success=hook://x-callback-url/done", id},
		{"another action", "joplin://x-callback-url/openFolder?id=" + id, ""},
		{"another scheme", "https://example.com/openNote?id=" + id, ""},
		{"short id", "joplin://x-callback-url/openNote?id=abc", ""},
		{"not hex", "joplin://x-callback-url/openNote?id=" + strings.Repeat("z", 32), ""},
		{"no id", "joplin://x-callback-url/openNote", ""},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := joplinNoteID(tc.raw); got != tc.want {
				t.Fatalf("joplinNoteID(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestNativeOpenExternalNote opens the note a joplin:// link names from a
// notebook the sidebar is not showing, which is what a pasted external link
// has to do.
func TestNativeOpenExternalNote(t *testing.T) {
	app, db := openNativeTestApp(t)
	svc := service.New(db)

	work := &models.Folder{Title: "Work"}
	home := &models.Folder{Title: "Home"}
	for _, folder := range []*models.Folder{work, home} {
		if err := svc.CreateNotebook(folder); err != nil {
			t.Fatalf("CreateNotebook: %v", err)
		}
	}
	target := addTestNote(t, svc, work.ID, "Target", "body")
	other := addTestNote(t, svc, home.ID, "Other", "body")

	app.reloadNotebooks()
	app.selectedNotebook, app.selectedID = home.ID, other.ID
	app.reloadNotes()
	if app.note == nil || app.note.ID != other.ID {
		t.Fatalf("setup opened %v, want the other note", app.note)
	}

	if !app.openExternalNote(target.ID) {
		t.Fatalf("openExternalNote(%s) failed: %s", target.ID, app.saveErr)
	}
	if app.note == nil || app.note.ID != target.ID {
		t.Fatalf("opened note = %v, want %s", app.note, target.ID)
	}
	if app.selectedNotebook != work.ID {
		t.Fatalf("selectedNotebook = %q, want %q", app.selectedNotebook, work.ID)
	}
	if app.rowOf(target.ID) < 0 {
		t.Fatal("the opened note has no row in the list")
	}
	if len(app.history) == 0 || app.history[len(app.history)-1] != target.ID {
		t.Fatalf("history = %v, want the opened note last", app.history)
	}

	if app.openExternalNote(strings.Repeat("0", 32)) {
		t.Fatal("openExternalNote opened a note that does not exist")
	}
	if !strings.Contains(app.saveErr, "not in this vault") {
		t.Fatalf("saveErr = %q, want a missing-note message", app.saveErr)
	}
}
