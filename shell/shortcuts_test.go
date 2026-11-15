package main

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/service"
)

func seedShortcutApp(t *testing.T) (*nativeApp, *models.Note) {
	t.Helper()
	app, db := openNativeTestApp(t)
	svc := service.New(db)
	nb := &models.Folder{Title: "Shortcut notes"}
	if err := svc.CreateNotebook(nb); err != nil {
		t.Fatalf("CreateNotebook: %v", err)
	}
	note := addTestNote(t, svc, nb.ID, "Shortcut fixture", "A body")
	app.reloadNotebooks()
	app.reloadNotes()
	app.showNote(note.ID)
	if app.note == nil || app.note.ID != note.ID {
		t.Fatalf("fixture note did not open: %#v", app.note)
	}
	return app, note
}

// TestNativeShortcutsHelp opens the reference with both advertised keys,
// checks it names the current platform's chords, closes it, and reaches it
// from the command palette as well.
func TestNativeShortcutsHelp(t *testing.T) {
	app, _ := seedShortcutApp(t)
	tt := ui.NewTester(app.view, 1280, 800)
	tt.Frame()

	// The visible controls announce the same keys the dialog lists.
	if a, ok := tt.AccessByRole(ui.RoleButton, "New note"); !ok || a.KeyShortcuts != shortcutKey("N") {
		t.Fatalf("New note key shortcuts = %+v, want %q", a, shortcutKey("N"))
	}
	if a, ok := tt.AccessByRole(ui.RoleButton, "Settings"); !ok || a.KeyShortcuts != shortcutKey(",") {
		t.Fatalf("Settings key shortcuts = %+v, want %q", a, shortcutKey(","))
	}

	tt.Key(0, ui.KeyF1)
	if !app.shortcutsOpen {
		t.Fatal("F1 did not open the keyboard reference")
	}
	tt.Frame()
	for _, want := range []string{"Keyboard shortcuts", "Mark as to-do", shortcutKey("S"), shortcutDeleteKey()} {
		if !tt.HasText(want) {
			t.Fatalf("keyboard reference is missing %q; texts = %q", want, tt.Texts())
		}
	}
	if a, ok := tt.AccessByRole(ui.RoleHeading, "Keyboard shortcuts"); !ok || a.Level != 2 {
		t.Fatalf("keyboard reference heading = %+v, want level 2", a)
	}

	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if app.shortcutsOpen {
		t.Fatal("Escape did not close the keyboard reference")
	}

	tt.Key(ui.Cmd, ui.KeySlash)
	if !app.shortcutsOpen {
		t.Fatal("Cmd/Ctrl+/ did not open the keyboard reference")
	}
	tt.Frame()
	click(t, tt, "Done")
	if app.shortcutsOpen {
		t.Fatal("Done did not close the keyboard reference")
	}

	// Discoverability: the palette lists the reference alongside its key.
	tt.Key(ui.Cmd, ui.KeyP)
	tt.Frame()
	tt.Type("keyboard")
	tt.Frame()
	click(t, tt, "Keyboard Shortcuts")
	if !app.shortcutsOpen {
		t.Fatal("the command palette did not open the keyboard reference")
	}
}

// TestNativeShortcutTodoAndDelete covers the two note actions the audit added
// key paths for: to-do toggling works in the view, and delete asks first and
// yields to a field that is taking text.
func TestNativeShortcutTodoAndDelete(t *testing.T) {
	app, note := seedShortcutApp(t)
	tt := ui.NewTester(app.view, 1280, 800)
	tt.Frame()

	tt.Key(ui.Cmd|ui.Shift, ui.KeyD)
	if app.note == nil || app.note.IsTodo != 1 {
		t.Fatalf("to-do shortcut left the note as %#v", app.note)
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyD)
	if app.note == nil || app.note.IsTodo != 0 {
		t.Fatalf("second to-do shortcut left the note as %#v", app.note)
	}

	// The editor owns the key while it is taking text.
	click(t, tt, "Edit")
	click(t, tt, "Note body")
	tt.Key(ui.Cmd|ui.Shift, ui.KeyBackspace)
	if app.confirmDelete {
		t.Fatal("delete shortcut fired while the editor had the keyboard")
	}

	tt.Key(ui.Cmd, ui.KeyE)
	tt.Frame()
	tt.Key(ui.Cmd|ui.Shift, ui.KeyBackspace)
	if !app.confirmDelete {
		t.Fatal("delete shortcut did not ask from the reading view")
	}
	tt.Frame()
	if !tt.HasText("Delete note?") {
		t.Fatalf("delete confirmation is missing; texts = %q", tt.Texts())
	}
	click(t, tt, "Cancel")
	if app.note == nil || app.note.ID != note.ID {
		t.Fatal("Cancel deleted the note")
	}
}
