package main

import (
	"bytes"
	"fmt"
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/lsj5031/joplin-lite/internal/models"
)

func TestNativeOrphanConflictPreservesVersion(t *testing.T) {
	registerFonts()
	for _, originalID := range []string{"deleted-original", ""} {
		t.Run(originalID, func(t *testing.T) {
			app, _ := openNativeTestApp(t)
			conflict := &models.Note{Title: "Recovered draft", Body: "Only remaining version", IsConflict: 1, ConflictOriginalID: originalID, IsTodo: 1}
			if err := app.svc.CreateNote(conflict); err != nil {
				t.Fatal(err)
			}
			tag := &models.Tag{Title: "Recovery"}
			if err := app.svc.CreateTag(tag); err != nil {
				t.Fatal(err)
			}
			if err := app.svc.AddTag(conflict.ID, tag.ID); err != nil {
				t.Fatal(err)
			}
			app.reloadTags()
			app.selectedNotebook = conflictsRail
			app.reloadNotes()
			tt := ui.NewTester(app.view, 1280, 800)
			click(t, tt, "Resolve conflict")
			if dir := os.Getenv("NATIVE_SAFETY_CAPTURE_DIR"); dir != "" && originalID != "" {
				capture(t, dir, "1280x800-orphan-conflict-confirm", tt)
			}
			click(t, tt, "Keep this version")
			got, err := app.svc.GetNote(conflict.ID)
			if err != nil || got == nil || got.IsConflict != 0 || got.ConflictOriginalID != "" || got.Title != conflict.Title || got.Body != conflict.Body || got.IsTodo != 1 {
				t.Fatalf("preserved version = %#v, err %v", got, err)
			}
			if len(app.notes) != 0 {
				t.Fatalf("resolved note remains in Conflicts: %#v", app.notes)
			}
			click(t, tt, "All notes")
			click(t, tt, "☐ Recovered draft")
			if app.body != conflict.Body || len(app.noteTags) != 1 || app.noteTags[0].ID != tag.ID {
				t.Fatalf("recovered note content/tags unavailable: %q %#v", app.body, app.noteTags)
			}
			if dir := os.Getenv("NATIVE_SAFETY_CAPTURE_DIR"); dir != "" && originalID != "" {
				capture(t, dir, "1280x800-orphan-conflict-preserved", tt)
			}
		})
	}
}

func TestNativeConflictFailuresKeepCopy(t *testing.T) {
	for _, failure := range []string{"read-original", "write-original", "write-orphan", "save-edits", "delete-copy"} {
		t.Run(failure, func(t *testing.T) {
			app, db := openNativeTestApp(t)
			original := addTestNote(t, app.svc, "", "Original", "Original body")
			conflict := &models.Note{Title: "Conflict", Body: "Keep this body", IsConflict: 1, ConflictOriginalID: original.ID}
			if failure == "write-orphan" {
				conflict.ConflictOriginalID = "missing"
			}
			if err := app.svc.CreateNote(conflict); err != nil {
				t.Fatal(err)
			}
			app.selectedNotebook = conflictsRail
			app.reloadNotes()
			tt := ui.NewTester(app.view, 1280, 800)
			click(t, tt, "Resolve conflict")
			if failure == "read-original" {
				// A schema read failure differs from a legitimately missing original.
				if _, err := db.Exec(`ALTER TABLE notes RENAME COLUMN body TO unavailable_body`); err != nil {
					t.Fatal(err)
				}
			} else {
				operation, id := "UPDATE", original.ID
				if failure == "write-orphan" || failure == "save-edits" {
					id = conflict.ID
				}
				if failure == "save-edits" {
					app.body = "Unsaved requested version"
					app.markDirty()
				}
				if failure == "delete-copy" {
					operation, id = "DELETE", conflict.ID
				}
				if _, err := db.Exec(fmt.Sprintf(`CREATE TRIGGER reject_resolution BEFORE %s ON notes WHEN OLD.id = '%s' BEGIN SELECT RAISE(ABORT, 'fixture write failure'); END`, operation, id)); err != nil {
					t.Fatal(err)
				}
			}
			click(t, tt, "Keep this version")
			if !tt.HasText("Could not save:") {
				t.Fatalf("resolution error was not shown: %v", tt.Texts())
			}
			if dir := os.Getenv("NATIVE_SAFETY_CAPTURE_DIR"); dir != "" && failure == "write-original" {
				capture(t, dir, "1280x800-conflict-write-error", tt)
			}
			if failure == "read-original" {
				if _, err := db.Exec(`ALTER TABLE notes RENAME COLUMN unavailable_body TO body`); err != nil {
					t.Fatal(err)
				}
			}
			got, err := app.svc.GetNote(conflict.ID)
			if err != nil || got == nil || got.Body != conflict.Body || got.IsConflict != 1 {
				t.Fatalf("failed resolution lost copy: %#v, err %v", got, err)
			}
			if failure == "save-edits" && app.body != "Unsaved requested version" {
				t.Fatal("failed autosave lost editor text")
			}
			if failure != "delete-copy" {
				got, err = app.svc.GetNote(original.ID)
				if err != nil || got == nil || got.Body != original.Body {
					t.Fatalf("failed resolution changed original: %#v, err %v", got, err)
				}
			}
		})
	}
}

func TestNativeSettingsProtectPasswordsAndKeepActionsVisible(t *testing.T) {
	registerFonts()
	app, _ := openNativeTestApp(t)
	stored := desktopSettings{Target: targetServer, Username: "fixture@example.invalid", Password: "fixture-server-secret", MasterPassword: "fixture-master-secret", APIToken: "synthetic-clipper-token"}
	app.loadSettings = func() desktopSettings { return stored }
	app.applySettings = func(s desktopSettings) error { return s.validate() }
	app.openSettings()
	tt := ui.NewTester(app.view, 1280, 800)
	tt.Advance(300 * time.Millisecond)
	var masked bytes.Buffer
	if err := png.Encode(&masked, tt.Image()); err != nil {
		t.Fatal(err)
	}
	// Equal-length secrets must produce exactly the same masked pixels.
	app.settings.Password = "changed-server-secret"
	app.settings.MasterPassword = "changed-master-secret"
	tt.Frame()
	var changed bytes.Buffer
	if err := png.Encode(&changed, tt.Image()); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(masked.Bytes(), changed.Bytes()) {
		t.Fatal("changing hidden secrets changed the rendered settings")
	}
	if dir := os.Getenv("NATIVE_SAFETY_CAPTURE_DIR"); dir != "" {
		capture(t, dir, "1280x800-settings-masked", tt)
	}
	click(t, tt, "Show passwords")
	var revealed bytes.Buffer
	if err := png.Encode(&revealed, tt.Image()); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(changed.Bytes(), revealed.Bytes()) {
		t.Fatal("Show passwords did not reveal the fields")
	}
	if dir := os.Getenv("NATIVE_SAFETY_CAPTURE_DIR"); dir != "" {
		capture(t, dir, "1280x800-settings-revealed-synthetic", tt)
	}
	click(t, tt, "Cancel")
	app.openSettings()
	tt.Frame()
	if app.settingsReveal || app.settings.Password != stored.Password || app.settings.MasterPassword != stored.MasterPassword {
		t.Fatal("reopening settings did not reset reveal and restore stored values")
	}
	tt.SetSize(900, 600)
	click(t, tt, "Save")
	if !tt.HasText("enter the Joplin Server URL") {
		t.Fatal("missing settings validation error")
	}
	for _, label := range []string{"Save settings", "Cancel settings"} {
		r, ok := tt.Find(label)
		if !ok || r.H < 20 || r.Y < 0 || r.Y+r.H > 580 {
			t.Fatalf("%s is clipped at 900x600: %#v", label, r)
		}
	}
	if dir := os.Getenv("NATIVE_SAFETY_CAPTURE_DIR"); dir != "" {
		capture(t, dir, "900x600-settings-error", tt)
	}
	// Scrolling reaches the lower fields while the footer remains available.
	tt.Scroll(600, 350, 0, 600)
	tt.Frame()
	r, ok := tt.Find("Clipper API token")
	if !ok || r.H < 20 {
		t.Fatalf("settings fields do not scroll into view: %#v", r)
	}
	if dir := os.Getenv("NATIVE_SAFETY_CAPTURE_DIR"); dir != "" {
		capture(t, dir, "900x600-settings-error-scrolled", tt)
	}
	click(t, tt, "Cancel")
	if app.settingsOpen {
		t.Fatal("Cancel was not reachable")
	}
}

func TestNativeHistoryClearsUnrelatedTag(t *testing.T) {
	app, _ := openNativeTestApp(t)
	alpha := addTestNote(t, app.svc, "", "Alpha", "untagged")
	beta := addTestNote(t, app.svc, "", "Beta", "tagged")
	tag := &models.Tag{Title: "Only beta"}
	if err := app.svc.CreateTag(tag); err != nil {
		t.Fatal(err)
	}
	if err := app.svc.AddTag(beta.ID, tag.ID); err != nil {
		t.Fatal(err)
	}
	app.reloadTags()
	app.selectedID = alpha.ID
	app.reloadNotes()
	tt := ui.NewTester(app.view, 1280, 800)
	click(t, tt, "Only beta")
	click(t, tt, "Back")
	if app.selectedTag != "" || app.selectedID != alpha.ID || app.selectedNote < 0 || !tt.HasText("Alpha") {
		t.Fatalf("history retained incompatible filter: tag %q, selected %q, row %d", app.selectedTag, app.selectedID, app.selectedNote)
	}
}

func TestNativeBulkMoveRefreshesCounts(t *testing.T) {
	app, _ := openNativeTestApp(t)
	inbox, target := &models.Folder{Title: "Inbox"}, &models.Folder{Title: "Target"}
	for _, folder := range []*models.Folder{inbox, target} {
		if err := app.svc.CreateNotebook(folder); err != nil {
			t.Fatal(err)
		}
	}
	addTestNote(t, app.svc, inbox.ID, "Alpha", "first")
	addTestNote(t, app.svc, inbox.ID, "Beta", "second")
	addTestNote(t, app.svc, inbox.ID, "Gamma", "stays")
	app.reloadNotebooks()
	app.selectedNotebook = inbox.ID
	app.reloadNotes()
	tt := ui.NewTester(app.view, 1280, 800)
	click(t, tt, "Alpha")
	if err := tt.ClickWith(ui.Cmd, "Beta"); err != nil {
		t.Fatal(err)
	}
	click(t, tt, "Move selected notes")
	click(t, tt, "Move to Target")
	if app.notebookByID(inbox.ID).NoteCount != 1 || app.notebookByID(target.ID).NoteCount != 2 || len(app.notes) != 1 || app.notes[0].Title != "Gamma" {
		t.Fatalf("bulk move left stale notebook counts or rows: Inbox %#v, Target %#v, rows %#v", app.notebookByID(inbox.ID), app.notebookByID(target.ID), app.notes)
	}
}
