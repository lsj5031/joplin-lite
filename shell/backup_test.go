package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/service"
)

func TestBackupStatusPrefersErrors(t *testing.T) {
	app := &nativeApp{backupNotice: "Exported 3 notes"}
	if line, _ := app.backupStatus(); line != "Exported 3 notes" {
		t.Errorf("notice not shown: %q", line)
	}

	app.backupBusy = "Importing notes…"
	if line, _ := app.backupStatus(); line != "Importing notes…" {
		t.Errorf("busy text should win over a stale notice: %q", line)
	}

	app.backupErr = "Could not export: disk full"
	if line, _ := app.backupStatus(); line != app.backupErr {
		t.Errorf("error should win over busy text: %q", line)
	}

	app = &nativeApp{}
	if line, _ := app.backupStatus(); line != "" {
		t.Errorf("idle app should show no backup status, got %q", line)
	}
}

func TestDefaultExportPath(t *testing.T) {
	got := defaultExportPath("/Users/example/.config/joplin-desktop")
	if !strings.HasSuffix(got, ".jex") {
		t.Errorf("suggested name should end in .jex: %q", got)
	}
	base := filepath.Base(got)
	if !strings.HasPrefix(base, "joplin-desktop-") {
		t.Errorf("suggested name should use the profile name: %q", base)
	}
	if strings.HasSuffix(got, "-.jex") {
		t.Errorf("missing timestamp: %q", got)
	}
}

func TestWithJEXExtension(t *testing.T) {
	cases := map[string]string{
		"/tmp/all.jex":         "/tmp/all.jex",
		"/tmp/all.jex.jex":     "/tmp/all.jex",
		"/tmp/all.JEX":         "/tmp/all.JEX",
		"/tmp/all":             "/tmp/all.jex",
		"/tmp/all.jex.jex.jex": "/tmp/all.jex",
	}
	for in, want := range cases {
		if got := withJEXExtension(in); got != want {
			t.Errorf("withJEXExtension(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestImportDialogReturnConfirms drives the confirmation the way a keyboard
// user does: Return answers Import, so the archive lands without touching the
// mouse. Cancel stays reachable with Escape.
func TestImportDialogReturnConfirms(t *testing.T) {
	app, db := openNativeTestApp(t)
	svc := service.New(db)
	nb := &models.Folder{Title: "Inbox"}
	if err := svc.CreateNotebook(nb); err != nil {
		t.Fatalf("CreateNotebook: %v", err)
	}
	addTestNote(t, svc, nb.ID, "Alpha", "body")

	archive := filepath.Join(t.TempDir(), "backup.jex")
	if _, err := svc.ExportJEX(archive, nil); err != nil {
		t.Fatalf("ExportJEX: %v", err)
	}

	app = newNativeApp(db)
	app.importPath = archive
	app.importPlan = &service.ImportResult{Notes: 1, Folders: 1}
	app.confirmImport = true

	tt := ui.NewTester(app.view, 1280, 800)
	tt.SetTitleBar(ui.TitleBar{Height: 44, Left: 76})
	tt.Frame()
	if !tt.HasText("Import archive?") {
		t.Fatalf("import confirmation did not render; texts = %v", tt.Texts())
	}

	notes := func() int {
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM notes").Scan(&n); err != nil {
			t.Fatalf("count notes: %v", err)
		}
		return n
	}
	before := notes()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()

	if app.confirmImport {
		t.Fatal("Return left the confirmation open")
	}
	if app.importPath != "" || app.importPlan != nil {
		t.Fatal("Return did not approve the import")
	}
	deadline := time.Now().Add(10 * time.Second)
	for notes() <= before && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if got := notes(); got != before+1 {
		t.Fatalf("import wrote %d notes, want %d", got-before, 1)
	}

	// Escape answers Cancel on the same dialog and writes nothing.
	app.importPath = archive
	app.importPlan = &service.ImportResult{Notes: 1, Folders: 1}
	app.confirmImport = true
	tt.Frame()
	before = notes()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if app.confirmImport {
		t.Fatal("Escape left the confirmation open")
	}
	time.Sleep(100 * time.Millisecond)
	if got := notes(); got != before {
		t.Fatalf("Escape imported %d notes", got-before)
	}
}

// TestDeleteDialogReturnStillCancels guards the safe default: destructive
// confirmations keep Cancel focused, so Return cannot destroy work.
func TestDeleteDialogReturnStillCancels(t *testing.T) {
	app, db := openNativeTestApp(t)
	svc := service.New(db)
	nb := &models.Folder{Title: "Inbox"}
	if err := svc.CreateNotebook(nb); err != nil {
		t.Fatalf("CreateNotebook: %v", err)
	}
	note := addTestNote(t, svc, nb.ID, "Alpha", "body")

	app = newNativeApp(db)
	app.confirmDelete = true

	tt := ui.NewTester(app.view, 1280, 800)
	tt.SetTitleBar(ui.TitleBar{Height: 44, Left: 76})
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()

	if app.confirmDelete {
		t.Fatal("Return should choose Cancel in a delete confirmation")
	}
	got, err := svc.GetNote(note.ID)
	if err != nil {
		t.Fatalf("GetNote: %v", err)
	}
	if got == nil {
		t.Fatal("Return deleted the note; destructive dialogs must keep Cancel focused")
	}
}

func TestDefaultExportPathFallsBackToName(t *testing.T) {
	got := defaultExportPath("/")
	if !strings.HasSuffix(got, ".jex") {
		t.Fatalf("expected a file name, got %q", got)
	}
	if !strings.Contains(filepath.Base(got), "all_notebooks") {
		t.Errorf("expected the fallback name, got %q", filepath.Base(got))
	}
}
