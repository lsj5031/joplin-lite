package main

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"github.com/lsj5031/joplin-lite/internal/config"
	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/internal/store"
	jsync "github.com/lsj5031/joplin-lite/internal/sync"
	"github.com/lsj5031/joplin-lite/service"
)

func openNativeTestApp(t *testing.T) (*nativeApp, *store.DB) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "vault"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return newNativeApp(db), db
}

func addTestNote(t *testing.T, svc *service.Service, parent, title, body string) *models.Note {
	t.Helper()
	n := &models.Note{ParentID: parent, Title: title, Body: body}
	if err := svc.CreateNote(n); err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	return n
}

// TestNativeWorkspaceDrivesTheView runs the real native view headlessly: it
// opens an existing note in preview, edits its body, waits for autosave, switches to
// the Markdown preview, creates a note with the keyboard shortcut and types
// in the title and search fields. This covers the widget wiring that the
// direct method tests above cannot.
func TestNativeWorkspaceDrivesTheView(t *testing.T) {
	app, db := openNativeTestApp(t)
	svc := service.New(db)

	folder := &models.Folder{Title: "Work"}
	if err := svc.CreateNotebook(folder); err != nil {
		t.Fatalf("CreateNotebook: %v", err)
	}
	seed := addTestNote(t, svc, folder.ID, "Seed", "")
	app = newNativeApp(db)
	if app.note == nil || app.note.ID != seed.ID {
		t.Fatalf("seed note not opened: %#v", app.note)
	}

	tt := ui.NewTester(app.view, 1200, 800)
	if !tt.HasText("Seed") {
		t.Fatalf("list does not show the seed note; texts = %v", tt.Texts())
	}
	if !tt.HasText("Local only") {
		t.Fatalf("status does not report a backend-less app as local only; texts = %v", tt.Texts())
	}

	// Existing notes open as rendered pages; Edit focuses the body for writing.
	if _, ok := tt.Find("Note body"); ok {
		t.Fatal("an existing note opened in Write instead of Preview")
	}
	click(t, tt, "Edit")
	tt.Type("Hello native")
	time.Sleep(600 * time.Millisecond)
	tt.Frame()
	got, err := svc.GetNote(seed.ID)
	if err != nil {
		t.Fatalf("GetNote: %v", err)
	}
	if got.Body != "Hello native" {
		t.Fatalf("typed body = %q, want Hello native", got.Body)
	}

	// Ctrl+E switches to the Markdown preview and back to the editor.
	tt.Key(ui.Cmd, ui.KeyE)
	if app.editing {
		t.Fatal("Ctrl+E did not enter preview")
	}
	if !tt.HasText("Hello native") {
		t.Fatalf("preview does not render the body; texts = %v", tt.Texts())
	}
	tt.Key(ui.Cmd, ui.KeyE)
	if !app.editing {
		t.Fatal("Ctrl+E did not return to the editor")
	}

	// Ctrl+N creates a note in the same notebook and focuses its body.
	tt.Key(ui.Cmd, ui.KeyN)
	if app.note == nil || app.note.ID == seed.ID || app.note.ParentID != folder.ID {
		t.Fatalf("new note = %#v", app.note)
	}
	fresh := app.note.ID
	tt.Type("Quick capture")
	time.Sleep(600 * time.Millisecond)
	tt.Frame()
	got, err = svc.GetNote(fresh)
	if err != nil {
		t.Fatalf("GetNote: %v", err)
	}
	if got.Body != "Quick capture" {
		t.Fatalf("new note body = %q", got.Body)
	}

	// The title field is a real input; click it, type, and autosave.
	if err := tt.Click("Note title"); err != nil {
		t.Fatalf("click title: %v", err)
	}
	tt.Type("Fresh note")
	time.Sleep(600 * time.Millisecond)
	tt.Frame()
	got, err = svc.GetNote(fresh)
	if err != nil {
		t.Fatalf("GetNote: %v", err)
	}
	if got.Title != "Fresh note" {
		t.Fatalf("typed title = %q", got.Title)
	}
	if !tt.HasText("Fresh note") {
		t.Fatalf("list does not show the new title; texts = %v", tt.Texts())
	}

	// A tag can be assigned from the note footer.
	tag := &models.Tag{Title: "Alpha"}
	if err := svc.CreateTag(tag); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	app.reloadTags()
	tt.Frame()
	if err := tt.Click("Note tags"); err != nil {
		t.Fatalf("click Note tags: %v", err)
	}
	if !app.tagMenuOpen {
		t.Fatal("tag menu did not open")
	}
	if err := tt.Click("Tag: Alpha"); err != nil {
		t.Fatalf("click Tag: Alpha: %v", err)
	}
	tags, err := svc.NoteTags(fresh)
	if err != nil {
		t.Fatalf("NoteTags: %v", err)
	}
	if len(tags) != 1 || tags[0].ID != tag.ID {
		t.Fatalf("tags on fresh note = %#v", tags)
	}
	if err := tt.Click("Note tags"); err != nil {
		t.Fatalf("close Note tags: %v", err)
	}

	// The tag rail filters the note list and can be cleared.
	click(t, tt, "Open notebooks")
	if err := tt.Click("Alpha"); err != nil {
		t.Fatalf("click tag Alpha: %v", err)
	}
	if app.selectedTag != tag.ID {
		t.Fatalf("selectedTag = %q", app.selectedTag)
	}
	if len(app.notes) != 1 || app.notes[0].ID != fresh {
		t.Fatalf("tag filter = %#v", app.notes)
	}
	if err := tt.Click("Clear tag"); err != nil {
		t.Fatalf("click Clear tag: %v", err)
	}
	if app.selectedTag != "" {
		t.Fatalf("selectedTag after clear = %q", app.selectedTag)
	}

	// Search is wired to the store and narrows the list to the seed note.
	if err := tt.Click("Search notes"); err != nil {
		t.Fatalf("click search: %v", err)
	}
	tt.Type("Hello native")
	if app.query != "Hello native" {
		t.Fatalf("search field = %q", app.query)
	}
	if len(app.notes) != 1 || app.notes[0].ID != seed.ID {
		t.Fatalf("search results = %#v", app.notes)
	}
	if app.note == nil || app.note.ID != seed.ID {
		t.Fatalf("search did not select the match: %#v", app.note)
	}
	if _, ok := tt.Find("Note body"); ok {
		t.Fatal("switching to an existing note did not return to Preview")
	}
	if _, ok := tt.Find("Edit"); !ok {
		t.Fatal("the existing note has no Edit action")
	}

	// The Todo checkbox writes through immediately.
	if err := tt.Click("Todo"); err != nil {
		t.Fatalf("click Todo: %v", err)
	}
	got, err = svc.GetNote(seed.ID)
	if err != nil {
		t.Fatalf("GetNote: %v", err)
	}
	if got.IsTodo == 0 {
		t.Fatal("Todo did not set is_todo")
	}

	// Delete opens a confirmation dialog; the note is gone only after the
	// dialog's Delete is chosen.
	if err := tt.Click("Delete…"); err != nil {
		t.Fatalf("click Delete: %v", err)
	}
	if !app.confirmDelete {
		t.Fatal("Delete did not open the confirmation dialog")
	}
	if err := tt.Click("Delete"); err != nil {
		t.Fatalf("confirm Delete: %v", err)
	}
	if app.confirmDelete {
		t.Fatal("confirmation dialog stayed open")
	}
	got, err = svc.GetNote(seed.ID)
	if err != nil {
		t.Fatalf("GetNote: %v", err)
	}
	if got != nil {
		t.Fatalf("deleted note still exists: %#v", got)
	}
}

// TestNativeHistoryAndMove covers the two remaining Phase 1 controls: Back
// and Forward through visited notes, and moving the open note to another
// notebook (including when that removes it from the current notebook view).
func TestNativeHistoryAndMove(t *testing.T) {
	app, db := openNativeTestApp(t)
	svc := service.New(db)

	work := &models.Folder{Title: "Work"}
	archive := &models.Folder{Title: "Archive"}
	if err := svc.CreateNotebook(work); err != nil {
		t.Fatalf("CreateNotebook Work: %v", err)
	}
	if err := svc.CreateNotebook(archive); err != nil {
		t.Fatalf("CreateNotebook Archive: %v", err)
	}
	alpha := addTestNote(t, svc, work.ID, "Alpha", "first")
	beta := addTestNote(t, svc, work.ID, "Beta", "second")

	app.reloadNotebooks()
	app.selectedNotebook = work.ID
	app.reloadNotes()
	// Both notes may share a millisecond, so pick the starting note instead
	// of relying on the list order.
	app.selectedID = beta.ID
	app.reloadNotes()
	if app.note == nil || app.note.ID != beta.ID {
		t.Fatalf("initial note = %#v, want beta", app.note)
	}

	tt := ui.NewTester(app.view, 1200, 800)
	if err := tt.Click("Alpha"); err != nil {
		t.Fatalf("click Alpha: %v", err)
	}
	if app.note == nil || app.note.ID != alpha.ID {
		t.Fatalf("after click = %#v, want alpha", app.note)
	}

	if err := tt.Click("Back"); err != nil {
		t.Fatalf("click Back: %v", err)
	}
	if app.note == nil || app.note.ID != beta.ID {
		t.Fatalf("after Back = %#v, want beta", app.note)
	}
	if err := tt.Click("Forward"); err != nil {
		t.Fatalf("click Forward: %v", err)
	}
	if app.note == nil || app.note.ID != alpha.ID {
		t.Fatalf("after Forward = %#v, want alpha", app.note)
	}

	// The keyboard equivalents. The note editor owns Alt and an arrow on
	// macOS, where that moves the caret by words in every native text field,
	// so the binding from inside a note is Cmd+[ / Cmd+] there, as in a
	// browser; Alt+Left/Right stay the binding elsewhere.
	backMods, backKey := ui.Alt, ui.KeyLeft
	forwardMods, forwardKey := ui.Alt, ui.KeyRight
	if runtime.GOOS == "darwin" {
		backMods, backKey = ui.Cmd, ui.KeyBracketLeft
		forwardMods, forwardKey = ui.Cmd, ui.KeyBracketRight
	}
	tt.Key(backMods, backKey)
	if app.note == nil || app.note.ID != beta.ID {
		t.Fatalf("after %s = %#v, want beta", historyHint(false), app.note)
	}
	tt.Key(forwardMods, forwardKey)
	if app.note == nil || app.note.ID != alpha.ID {
		t.Fatalf("after %s = %#v, want alpha", historyHint(true), app.note)
	}
	if runtime.GOOS == "darwin" {
		// Pins the decision above: while editing, Alt and an arrow must
		// stay the editor's word movement, not a navigation.
		click(t, tt, "Edit")
		click(t, tt, "Note body")
		tt.Key(ui.Alt, ui.KeyLeft)
		if app.note == nil || app.note.ID != alpha.ID {
			t.Fatalf("Alt+Left moved the history while editing; the editor owns it on macOS")
		}
	}

	// Moving Alpha out of Work leaves Beta as the selected row.
	if err := tt.Click("Move"); err != nil {
		t.Fatalf("click Move: %v", err)
	}
	if !app.moveOpen {
		t.Fatal("Move did not open the notebook menu")
	}
	if err := tt.Click("Move to Archive"); err != nil {
		t.Fatalf("click Move to Archive: %v", err)
	}
	if app.moveOpen {
		t.Fatal("move menu stayed open")
	}
	moved, err := svc.GetNote(alpha.ID)
	if err != nil {
		t.Fatalf("GetNote: %v", err)
	}
	if moved == nil || moved.ParentID != archive.ID {
		t.Fatalf("moved note = %#v, want parent %s", moved, archive.ID)
	}
	if app.note == nil || app.note.ID != beta.ID {
		t.Fatalf("note after move = %#v, want beta", app.note)
	}

	// Back reaches the moved note, dropping the Work filter to show it.
	if err := tt.Click("Back"); err != nil {
		t.Fatalf("click Back after move: %v", err)
	}
	if app.note == nil || app.note.ID != alpha.ID {
		t.Fatalf("after Back = %#v, want alpha", app.note)
	}
	if app.selectedNotebook != "" {
		t.Fatalf("selectedNotebook = %q, want All notes", app.selectedNotebook)
	}
}

// TestNativeTagFilterAndToggle covers tag assignment and the tag-filtered
// note list, including a note leaving the list when its tag is removed.
func TestNativeTagFilterAndToggle(t *testing.T) {
	app, db := openNativeTestApp(t)
	svc := service.New(db)

	folder := &models.Folder{Title: "Work"}
	if err := svc.CreateNotebook(folder); err != nil {
		t.Fatalf("CreateNotebook: %v", err)
	}
	first := addTestNote(t, svc, folder.ID, "First", "one")
	second := addTestNote(t, svc, folder.ID, "Second", "two")
	alpha := &models.Tag{Title: "Alpha"}
	beta := &models.Tag{Title: "Beta"}
	if err := svc.CreateTag(alpha); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	if err := svc.CreateTag(beta); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}

	app.reloadNotebooks()
	app.reloadTags()
	app.selectedID = first.ID
	app.reloadNotes()
	if app.note == nil || app.note.ID != first.ID {
		t.Fatalf("open note = %#v", app.note)
	}

	app.toggleTag(alpha.ID)
	if !app.hasNoteTag(alpha.ID) {
		t.Fatal("toggleTag did not add the tag")
	}
	app.selectedTag = alpha.ID
	app.reloadNotes()
	if len(app.notes) != 1 || app.notes[0].ID != first.ID {
		t.Fatalf("alpha filter = %#v", app.notes)
	}

	// Removing the tag while filtered removes the note from the view.
	app.toggleTag(alpha.ID)
	if app.hasNoteTag(alpha.ID) {
		t.Fatal("toggleTag did not remove the tag")
	}
	if len(app.notes) != 0 || app.note != nil {
		t.Fatalf("after removing the tag: notes=%#v note=%#v", app.notes, app.note)
	}
	if tags, _ := svc.NoteTags(first.ID); len(tags) != 0 {
		t.Fatalf("stored tags = %#v", tags)
	}

	// A different tag shows the other note.
	if err := svc.AddTag(second.ID, beta.ID); err != nil {
		t.Fatalf("AddTag: %v", err)
	}
	app.selectedTag = beta.ID
	app.reloadNotes()
	if len(app.notes) != 1 || app.notes[0].ID != second.ID {
		t.Fatalf("beta filter = %#v", app.notes)
	}
}

// TestNativeConflictsResolve lists the virtual Conflicts notebook and keeps
// the conflict's version on the original note.
func TestNativeConflictsResolve(t *testing.T) {
	app, db := openNativeTestApp(t)
	svc := service.New(db)

	folder := &models.Folder{Title: "Work"}
	if err := svc.CreateNotebook(folder); err != nil {
		t.Fatalf("CreateNotebook: %v", err)
	}
	original := addTestNote(t, svc, folder.ID, "Original", "my body")
	conflict := &models.Note{
		ParentID:           folder.ID,
		Title:              "Original (conflict)",
		Body:               "their body",
		IsConflict:         1,
		ConflictOriginalID: original.ID,
	}
	if err := svc.CreateNote(conflict); err != nil {
		t.Fatalf("CreateNote conflict: %v", err)
	}

	app.reloadNotebooks()
	app.selectedNotebook = conflictsRail
	app.reloadNotes()
	if len(app.notes) != 1 || app.notes[0].ID != conflict.ID {
		t.Fatalf("conflicts list = %#v", app.notes)
	}
	if app.note == nil || app.note.ID != conflict.ID {
		t.Fatalf("open conflict = %#v", app.note)
	}

	tt := ui.NewTester(app.view, 1200, 800)
	if !tt.HasText("Conflicts") {
		t.Fatalf("conflicts view not labeled; texts = %v", tt.Texts())
	}
	if err := tt.Click("Resolve conflict"); err != nil {
		t.Fatalf("click Resolve conflict: %v", err)
	}
	if err := tt.Click("Keep this version"); err != nil {
		t.Fatalf("click Keep this version: %v", err)
	}

	got, err := svc.GetNote(original.ID)
	if err != nil {
		t.Fatalf("GetNote original: %v", err)
	}
	if got == nil || got.Title != conflict.Title || got.Body != conflict.Body {
		t.Fatalf("resolved original = %#v", got)
	}
	if gone, _ := svc.GetNote(conflict.ID); gone != nil {
		t.Fatalf("conflict still exists: %#v", gone)
	}
	if len(app.notes) != 0 || app.note != nil {
		t.Fatalf("conflicts list after resolve = %#v note %#v", app.notes, app.note)
	}
}

// TestNativeTagManagement creates, renames and deletes a tag through the tag
// rail's header and actions menu.
func TestNativeTagManagement(t *testing.T) {
	app, db := openNativeTestApp(t)
	svc := service.New(db)

	folder := &models.Folder{Title: "Work"}
	if err := svc.CreateNotebook(folder); err != nil {
		t.Fatalf("CreateNotebook: %v", err)
	}
	addTestNote(t, svc, folder.ID, "Note", "body")
	app.reloadNotebooks()
	app.reloadTags()
	app.reloadNotes()

	tt := ui.NewTester(app.view, 1200, 800)
	click(t, tt, "Open notebooks")
	if err := tt.Click("New tag"); err != nil {
		t.Fatalf("click New tag: %v", err)
	}
	tt.Type("Alpha")
	if err := tt.Click("Create"); err != nil {
		t.Fatalf("click Create: %v", err)
	}
	if len(app.tags) != 1 || app.tags[0].Title != "Alpha" {
		t.Fatalf("tags after create = %#v", app.tags)
	}

	if err := tt.Click("Alpha"); err != nil {
		t.Fatalf("select Alpha: %v", err)
	}
	if app.selectedTag != app.tags[0].ID {
		t.Fatalf("selectedTag = %q", app.selectedTag)
	}

	if err := tt.Click("Tag actions"); err != nil {
		t.Fatalf("click Tag actions: %v", err)
	}
	if err := tt.Click("Rename tag…"); err != nil {
		t.Fatalf("click Rename tag: %v", err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("Beta")
	if err := tt.Click("Save"); err != nil {
		t.Fatalf("click Save: %v", err)
	}
	if len(app.tags) != 1 || app.tags[0].Title != "Beta" {
		t.Fatalf("tags after rename = %#v", app.tags)
	}

	if err := tt.Click("Tag actions"); err != nil {
		t.Fatalf("click Tag actions: %v", err)
	}
	if err := tt.Click("Delete tag…"); err != nil {
		t.Fatalf("click Delete tag: %v", err)
	}
	if err := tt.Click("Delete tag"); err != nil {
		t.Fatalf("confirm Delete tag: %v", err)
	}
	if len(app.tags) != 0 {
		t.Fatalf("tags after delete = %#v", app.tags)
	}
	if app.selectedTag != "" {
		t.Fatalf("selectedTag after delete = %q", app.selectedTag)
	}
	if page, _ := svc.ListTags(service.Page{Limit: 10}); len(page.Tags) != 0 {
		t.Fatalf("stored tags after delete = %#v", page.Tags)
	}
}

// TestNativeNotebookManagement creates a child notebook through the modal,
// renames it, deletes it with its notes after confirmation, and checks that a
// notebook with children cannot be deleted.
func TestNativeNotebookManagement(t *testing.T) {
	app, db := openNativeTestApp(t)
	svc := service.New(db)

	work := &models.Folder{Title: "Work"}
	if err := svc.CreateNotebook(work); err != nil {
		t.Fatalf("CreateNotebook: %v", err)
	}
	addTestNote(t, svc, work.ID, "Existing", "body")
	app.reloadNotebooks()
	app.selectedNotebook = work.ID
	app.reloadNotes()

	tt := ui.NewTester(app.view, 1200, 800)
	click(t, tt, "Open notebooks")
	if err := tt.Click("New notebook"); err != nil {
		t.Fatalf("click New notebook: %v", err)
	}
	tt.Type("Child")
	if err := tt.Click("Create"); err != nil {
		t.Fatalf("click Create: %v", err)
	}
	child := app.notebookByID(app.selectedNotebook)
	if child == nil || child.Title != "Child" || child.ParentID != work.ID {
		t.Fatalf("created notebook = %#v", child)
	}
	childID := child.ID
	if app.notebookDepth[childID] != 1 {
		t.Fatalf("depth of child = %d, want 1", app.notebookDepth[childID])
	}

	if err := tt.Click("Notebook actions"); err != nil {
		t.Fatalf("click actions: %v", err)
	}
	if err := tt.Click("Rename notebook…"); err != nil {
		t.Fatalf("click Rename: %v", err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("Renamed")
	if err := tt.Click("Save"); err != nil {
		t.Fatalf("click Save: %v", err)
	}
	if got, _ := svc.GetNotebook(childID); got == nil || got.Title != "Renamed" {
		t.Fatalf("renamed notebook = %#v", got)
	}

	// Put a note in the child, then delete the notebook and that note.
	tt.Key(ui.Cmd, ui.KeyN)
	if app.note == nil || app.note.ParentID != childID {
		t.Fatalf("new note parent = %#v", app.note)
	}
	noteID := app.note.ID
	tt.Type("Note in child")
	time.Sleep(600 * time.Millisecond)
	tt.Frame()

	if err := tt.Click("Notebook actions"); err != nil {
		t.Fatalf("click actions: %v", err)
	}
	if err := tt.Click("Delete notebook…"); err != nil {
		t.Fatalf("click Delete notebook: %v", err)
	}
	if err := tt.Click("Delete notebook"); err != nil {
		t.Fatalf("confirm Delete notebook: %v", err)
	}
	if got, _ := svc.GetNotebook(childID); got != nil {
		t.Fatalf("notebook still exists: %#v", got)
	}
	if got, _ := svc.GetNote(noteID); got != nil {
		t.Fatalf("note still exists: %#v", got)
	}
	if app.selectedNotebook != "" {
		t.Fatalf("selectedNotebook = %q, want All notes", app.selectedNotebook)
	}

	// A notebook with a child refuses deletion and keeps both.
	if err := tt.Click("New notebook"); err != nil {
		t.Fatalf("click New notebook: %v", err)
	}
	tt.Type("Parent")
	if err := tt.Click("Create"); err != nil {
		t.Fatalf("click Create: %v", err)
	}
	parentID := app.selectedNotebook
	if err := tt.Click("New notebook"); err != nil {
		t.Fatalf("click New notebook: %v", err)
	}
	tt.Type("Child two")
	if err := tt.Click("Create"); err != nil {
		t.Fatalf("click Create: %v", err)
	}
	if err := tt.Click("Parent"); err != nil {
		t.Fatalf("select Parent: %v", err)
	}
	if err := tt.Click("Notebook actions"); err != nil {
		t.Fatalf("click actions: %v", err)
	}
	if err := tt.Click("Delete notebook…"); err != nil {
		t.Fatalf("click Delete notebook: %v", err)
	}
	if !tt.HasText("Notebook has child notebooks") || tt.HasText("Delete notebook") {
		t.Fatalf("parent deletion should explain the block before confirmation: %v", tt.Texts())
	}
	if err := tt.Click("OK"); err != nil {
		t.Fatalf("dismiss blocked dialog: %v", err)
	}
	if got, _ := svc.GetNotebook(parentID); got == nil {
		t.Fatal("blocked delete removed the parent notebook")
	}
}

// TestNativeSyncRefresh covers the post-sync refresh: a pulled change updates
// an idle open note, while unsaved local edits are left alone.
func TestNativeSyncRefresh(t *testing.T) {
	app, db := openNativeTestApp(t)
	svc := service.New(db)

	folder := &models.Folder{Title: "Work"}
	if err := svc.CreateNotebook(folder); err != nil {
		t.Fatalf("CreateNotebook: %v", err)
	}
	note := addTestNote(t, svc, folder.ID, "Local", "local body")
	app.reloadNotebooks()
	app.reloadNotes()
	if app.note == nil || app.note.ID != note.ID {
		t.Fatalf("open note = %#v", app.note)
	}

	remote := *note
	remote.Title = "Pulled"
	remote.Body = "pulled body"
	if err := svc.UpdateNote(&remote); err != nil {
		t.Fatalf("UpdateNote: %v", err)
	}
	app.refreshAfterSync()
	if app.title != "Pulled" || app.body != "pulled body" {
		t.Fatalf("idle note not refreshed: %q %q", app.title, app.body)
	}

	// Unsaved typing wins over a pulled change.
	app.title, app.body = "Mine", "typing"
	app.markDirty()
	remote = *app.note
	remote.Title = "Pulled again"
	remote.Body = "remote again"
	if err := svc.UpdateNote(&remote); err != nil {
		t.Fatalf("UpdateNote: %v", err)
	}
	app.refreshAfterSync()
	if app.title != "Mine" || app.body != "typing" {
		t.Fatalf("refresh overwrote local edits: %q %q", app.title, app.body)
	}
}

// TestSyncStatusTransitions covers the state machine the sync footer reads.
func TestSyncStatusTransitions(t *testing.T) {
	st := &syncStatus{}
	if snap := st.snapshot(false); snap.Enabled || snap.Running || snap.Pending {
		t.Fatalf("initial snapshot = %#v", snap)
	}
	st.request()
	if snap := st.snapshot(true); !snap.Enabled || !snap.Pending || snap.Running {
		t.Fatalf("pending snapshot = %#v", snap)
	}
	st.start()
	if snap := st.snapshot(true); snap.Pending || !snap.Running {
		t.Fatalf("running snapshot = %#v", snap)
	}
	st.progress("Uploading")
	if snap := st.snapshot(true); snap.Phase != "Uploading" {
		t.Fatalf("progress snapshot = %#v", snap)
	}
	st.finish(errors.New("offline"))
	if snap := st.snapshot(true); snap.Running || snap.Pending || snap.Err != "offline" || !snap.Last.IsZero() || snap.Phase != "" {
		t.Fatalf("failed snapshot = %#v", snap)
	}
	st.request()
	st.start()
	st.finish(nil)
	if snap := st.snapshot(true); snap.Err != "" || snap.Last.IsZero() {
		t.Fatalf("successful snapshot = %#v", snap)
	}
	last := st.snapshot(true).Last

	// A lock failure is classified, and a retry records when it will run.
	st.finish(fmt.Errorf("sync refused: %w", jsync.ErrExclusiveLock))
	if snap := st.snapshot(true); snap.Kind != syncFailureLock || !snap.Last.Equal(last) {
		t.Fatalf("lock failure snapshot = %#v", snap)
	}
	when := time.Now().Add(time.Minute)
	st.retryIn(when)
	if snap := st.snapshot(true); !snap.Retry.Equal(when) {
		t.Fatalf("retry = %v, want %v", snap.Retry, when)
	}
	st.start()
	if snap := st.snapshot(true); !snap.Retry.IsZero() {
		t.Fatalf("a new pass kept the stale retry: %v", snap.Retry)
	}
	st.finish(nil)

	// A backend without an engine offers nothing to trigger.
	if (&backend{}).SyncNow() {
		t.Fatal("SyncNow on a local-only backend reported true")
	}
}

func noteIndex(rows []*models.NoteSummary, id string) int {
	for i, n := range rows {
		if n.ID == id {
			return i
		}
	}
	return -1
}

// TestNativeAppLoadAndAutosave covers the Phase 1 editing path: the first
// note loads, edits mark the app dirty, and the debounced tick writes both
// title and body through the store.
func TestNativeAppLoadAndAutosave(t *testing.T) {
	app, db := openNativeTestApp(t)
	svc := service.New(db)

	folder := &models.Folder{Title: "Work"}
	if err := svc.CreateNotebook(folder); err != nil {
		t.Fatalf("CreateNotebook: %v", err)
	}
	note := addTestNote(t, svc, folder.ID, "First", "alpha body")
	addTestNote(t, svc, folder.ID, "Second", "beta body")

	app.reloadNotebooks()
	app.reloadNotes()
	if len(app.notebooks) != 1 || app.notebooks[0].ID != folder.ID {
		t.Fatalf("notebooks = %#v", app.notebooks)
	}
	if len(app.notes) != 2 {
		t.Fatalf("notes = %d, want 2", len(app.notes))
	}

	app.selectedNote = noteIndex(app.notes, note.ID)
	app.loadNote()
	if app.note == nil || app.note.ID != note.ID {
		t.Fatalf("loaded note = %#v, want %s", app.note, note.ID)
	}
	if app.editing {
		t.Fatal("an existing note should open in preview")
	}
	app.toggleEdit()

	app.title = "Renamed"
	app.body = "alpha with more words"
	app.markDirty()
	if !app.dirty {
		t.Fatal("markDirty did not set dirty")
	}

	// Nothing is written before the debounce deadline.
	app.saveDue = time.Now().Add(time.Minute)
	app.tick()
	got, err := svc.GetNote(note.ID)
	if err != nil {
		t.Fatalf("GetNote: %v", err)
	}
	if got.Title != "First" || got.Body != "alpha body" {
		t.Fatalf("saved before the deadline: %q %q", got.Title, got.Body)
	}

	app.saveDue = time.Now().Add(-time.Millisecond)
	app.tick()
	if app.dirty {
		t.Fatal("tick left the app dirty")
	}
	got, err = svc.GetNote(note.ID)
	if err != nil {
		t.Fatalf("GetNote: %v", err)
	}
	if got.Title != "Renamed" || got.Body != "alpha with more words" {
		t.Fatalf("saved note = %q %q", got.Title, got.Body)
	}
	if row := app.notes[noteIndex(app.notes, note.ID)]; row.Title != "Renamed" || row.Excerpt != "alpha with more words" {
		t.Fatalf("row not updated after save: %#v", row)
	}
}

// TestNativeAppSearchAndSelection covers filtering, keeping the open note
// when it is still in the results, and flushing before a note switch.
func TestNativeAppSearchAndSelection(t *testing.T) {
	app, db := openNativeTestApp(t)
	svc := service.New(db)

	folder := &models.Folder{Title: "Work"}
	if err := svc.CreateNotebook(folder); err != nil {
		t.Fatalf("CreateNotebook: %v", err)
	}
	alpha := addTestNote(t, svc, folder.ID, "Alpha", "plain text")
	beta := addTestNote(t, svc, folder.ID, "Beta", "kernel panic notes")

	app.reloadNotebooks()
	app.reloadNotes()
	app.selectedNote = noteIndex(app.notes, alpha.ID)
	app.loadNote()

	app.title = "Alpha unsaved"
	app.body = "typed but not debounced yet"
	app.markDirty()

	// A search that still contains the open note keeps the editor state.
	app.query = "alpha"
	app.reloadNotes()
	if len(app.notes) != 1 || app.notes[0].ID != alpha.ID {
		t.Fatalf("search alpha = %#v", app.notes)
	}
	if app.note == nil || app.note.ID != alpha.ID || app.body != "typed but not debounced yet" {
		t.Fatalf("search reset the open note: %#v", app.note)
	}

	// A search that excludes it flushes the old note before loading the new.
	app.query = "kernel"
	app.reloadNotes()
	if len(app.notes) != 1 || app.notes[0].ID != beta.ID {
		t.Fatalf("search kernel = %#v", app.notes)
	}
	if app.note == nil || app.note.ID != beta.ID {
		t.Fatalf("loaded note = %#v, want beta", app.note)
	}
	got, err := svc.GetNote(alpha.ID)
	if err != nil {
		t.Fatalf("GetNote: %v", err)
	}
	if got.Title != "Alpha unsaved" || got.Body != "typed but not debounced yet" {
		t.Fatalf("switch did not flush the old note: %q %q", got.Title, got.Body)
	}

	// Clearing the search restores both notes; selection survives.
	app.query = ""
	app.reloadNotes()
	if len(app.notes) != 2 {
		t.Fatalf("cleared search = %d notes, want 2", len(app.notes))
	}
	if app.selectedID != beta.ID || app.note == nil || app.note.ID != beta.ID {
		t.Fatalf("selection after reload = %q note %#v", app.selectedID, app.note)
	}
}

// TestNativeAppNewNote covers creating a note in the selected notebook and
// creating the default notebook when the vault has none.
func TestNativeAppNewNote(t *testing.T) {
	app, db := openNativeTestApp(t)
	svc := service.New(db)

	folder := &models.Folder{Title: "Work"}
	if err := svc.CreateNotebook(folder); err != nil {
		t.Fatalf("CreateNotebook: %v", err)
	}
	addTestNote(t, svc, folder.ID, "Existing", "body")
	app.reloadNotebooks()
	app.reloadNotes()
	app.selectedNotebook = folder.ID
	app.selectedID = ""
	app.reloadNotes()

	app.newNote()
	if len(app.notes) != 2 {
		t.Fatalf("notes after new = %d, want 2", len(app.notes))
	}
	if app.note == nil || app.note.ParentID != folder.ID {
		t.Fatalf("new note = %#v, want parent %s", app.note, folder.ID)
	}
	if !app.editing || !app.focusBody {
		t.Fatal("new note should open in the editor with focus")
	}
	app.title = "Fresh"
	app.body = "draft body"
	app.markDirty()
	app.flush()
	got, err := svc.GetNote(app.note.ID)
	if err != nil {
		t.Fatalf("GetNote: %v", err)
	}
	if got.Title != "Fresh" || got.Body != "draft body" {
		t.Fatalf("new note saved = %q %q", got.Title, got.Body)
	}

	// A vault with no notebooks gets a default one rather than a note with
	// an empty parent.
	empty, _ := openNativeTestApp(t)
	empty.newNote()
	if len(empty.notebooks) != 1 || empty.notebooks[0].Title != "Notes" {
		t.Fatalf("default notebooks = %#v", empty.notebooks)
	}
	if empty.note == nil || empty.note.ParentID != empty.notebooks[0].ID {
		t.Fatalf("note in default notebook = %#v", empty.note)
	}
}

// TestNativeSettingsDialog drives the real settings form: load the current
// values, switch the target, type the target's fields, save, and check that a
// rejected save stays open with the reason instead of closing.
func TestNativeSettingsDialog(t *testing.T) {
	app, _ := openNativeTestApp(t)

	var applied []desktopSettings
	app.loadSettings = func() desktopSettings {
		return desktopSettings{Target: targetOff, APIToken: "token-1"}
	}
	app.applySettings = func(s desktopSettings) error {
		if err := s.validate(); err != nil {
			return err
		}
		applied = append(applied, s)
		return nil
	}

	tt := ui.NewTester(app.view, 1200, 800)
	if err := tt.Click("Settings"); err != nil {
		t.Fatalf("click Settings: %v", err)
	}
	if !app.settingsOpen {
		t.Fatal("Settings did not open the dialog")
	}
	if !tt.HasText("Sync and the web clipper") {
		t.Fatalf("settings form not drawn; texts = %v", tt.Texts())
	}
	if app.settings.APIToken != "token-1" {
		t.Fatalf("draft token = %q, want the loaded one", app.settings.APIToken)
	}

	if err := tt.Click("File system"); err != nil {
		t.Fatalf("choose File system: %v", err)
	}
	if app.settings.Target != targetFolder {
		t.Fatalf("target after choosing File system = %d", app.settings.Target)
	}
	if err := tt.Click("Sync folder path"); err != nil {
		t.Fatalf("click the folder field: %v", err)
	}
	tt.Type("/vault/sync")
	if err := tt.Click("Save"); err != nil {
		t.Fatalf("click Save: %v", err)
	}
	if len(applied) != 1 {
		t.Fatalf("applied %d times, want 1", len(applied))
	}
	if got := applied[0]; got.Target != targetFolder || got.LocalDir != "/vault/sync" || got.APIToken != "token-1" {
		t.Fatalf("applied = %+v", got)
	}
	if app.settingsOpen {
		t.Fatal("the dialog stayed open after a successful save")
	}

	// A server target without a URL is refused in place, with the reason.
	if err := tt.Click("Settings"); err != nil {
		t.Fatalf("reopen Settings: %v", err)
	}
	if err := tt.Click("Joplin Server"); err != nil {
		t.Fatalf("choose Joplin Server: %v", err)
	}
	if app.settings.Target != targetServer {
		t.Fatalf("target after choosing Joplin Server = %d", app.settings.Target)
	}
	if err := tt.Click("Save"); err != nil {
		t.Fatalf("click Save: %v", err)
	}
	if !app.settingsOpen {
		t.Fatal("an invalid save closed the dialog")
	}
	if app.settingsErr == "" || !tt.HasText("enter the Joplin Server URL") {
		t.Fatalf("validation reason not shown: %q texts = %v", app.settingsErr, tt.Texts())
	}
	if len(applied) != 1 {
		t.Fatalf("an invalid save reached the backend: %d", len(applied))
	}

	// Fill every required server field and save.
	for label, value := range map[string]string{
		"Joplin Server URL":       "https://sync.example.com",
		"Server account email":    "me@example.com",
		"Server account password": "hunter2",
		"E2EE master password":    "master pass",
	} {
		if err := tt.Click(label); err != nil {
			t.Fatalf("click %s: %v", label, err)
		}
		tt.Type(value)
	}
	if err := tt.Click("Save"); err != nil {
		t.Fatalf("click Save: %v", err)
	}
	if app.settingsOpen {
		t.Fatalf("valid server settings did not save: %q", app.settingsErr)
	}
	if len(applied) != 2 {
		t.Fatalf("applied %d times after the fix, want 2", len(applied))
	}
	got := applied[1]
	if got.Target != targetServer || got.ServerURL != "https://sync.example.com" ||
		got.Username != "me@example.com" || got.Password != "hunter2" || got.MasterPassword != "master pass" {
		t.Fatalf("applied server settings = %+v", got)
	}
}

// TestNativeSortMenu switches the note-list order through the Sort popover.
// The order must match the store's, and the selection must stay on a note.
func TestNativeSortMenu(t *testing.T) {
	app, db := openNativeTestApp(t)
	svc := service.New(db)
	for _, title := range []string{"Delta", "alpha", "Charlie"} {
		addTestNote(t, svc, "", title, "body of "+title)
	}
	app.reloadNotes()
	if len(app.notes) != 3 {
		t.Fatalf("seeded %d notes, want 3", len(app.notes))
	}

	tt := ui.NewTester(app.view, 1200, 800)
	if err := tt.Click("Sort"); err != nil {
		t.Fatalf("click Sort: %v", err)
	}
	if !app.sortOpen {
		t.Fatal("Sort did not open its menu")
	}
	if err := tt.Click("Title A-Z"); err != nil {
		t.Fatalf("click Title A-Z: %v", err)
	}
	if app.sort != string(store.SortTitleAsc) {
		t.Fatalf("sort = %q, want %q", app.sort, store.SortTitleAsc)
	}
	if got := strings.Join(noteTitles(app.notes), ","); got != "alpha,Charlie,Delta" {
		t.Fatalf("title order = %s", got)
	}
	if app.sortOpen {
		t.Fatal("the sort menu stayed open after choosing")
	}

	if err := tt.Click("Sort"); err != nil {
		t.Fatalf("reopen Sort: %v", err)
	}
	if err := tt.Click("Title Z-A"); err != nil {
		t.Fatalf("click Title Z-A: %v", err)
	}
	if got := strings.Join(noteTitles(app.notes), ","); got != "Delta,Charlie,alpha" {
		t.Fatalf("descending title order = %s", got)
	}

	// The selected row still opens its note after the reorder.
	if err := tt.Click("Charlie"); err != nil {
		t.Fatalf("select Charlie: %v", err)
	}
	if app.note == nil || app.note.Title != "Charlie" {
		t.Fatalf("selected note = %#v", app.note)
	}
}

func noteTitles(notes []*models.NoteSummary) []string {
	out := make([]string, 0, len(notes))
	for _, n := range notes {
		out = append(out, n.Title)
	}
	return out
}

// TestNativeSavedSearches saves the active query from the search row, applies
// it from the menu, and deletes it. The list must survive a fresh app instance
// (it lives in key_values, not in memory).
func TestNativeSavedSearches(t *testing.T) {
	app, db := openNativeTestApp(t)
	svc := service.New(db)
	addTestNote(t, svc, "", "Work todo", "finish the report")
	addTestNote(t, svc, "", "Groceries", "milk")
	app.reloadNotes()

	tt := ui.NewTester(app.view, 1200, 800)
	if err := tt.Click("Search notes"); err != nil {
		t.Fatalf("click search: %v", err)
	}
	tt.Type("report")
	if len(app.notes) != 1 || app.notes[0].Title != "Work todo" {
		t.Fatalf("search results = %v", app.notes)
	}

	if err := tt.Click("Saved searches"); err != nil {
		t.Fatalf("open saved searches: %v", err)
	}
	if !app.savedMenuOpen {
		t.Fatal("the saved-search menu did not open")
	}
	if err := tt.Click("Save current search…"); err != nil {
		t.Fatalf("click Save current search: %v", err)
	}
	if !app.savedOpen {
		t.Fatal("the save dialog did not open")
	}
	tt.Type("Work")
	if err := tt.Click("Save"); err != nil {
		t.Fatalf("click Save: %v", err)
	}
	if app.savedOpen || app.savedErr != "" {
		t.Fatalf("save dialog stayed open: %q", app.savedErr)
	}
	if len(app.savedSearches) != 1 || app.savedSearches[0].Name != "Work" || app.savedSearches[0].Query != "report" {
		t.Fatalf("saved searches = %+v", app.savedSearches)
	}

	// A fresh view over the same vault loads the list.
	if fresh := newNativeApp(db); len(fresh.savedSearches) != 1 || fresh.savedSearches[0].Name != "Work" {
		t.Fatalf("reloaded saved searches = %+v", fresh.savedSearches)
	}

	// Clearing the query and applying the saved search restores it.
	app.query = ""
	app.reloadNotes()
	tt.Frame()
	if len(app.notes) != 2 {
		t.Fatalf("cleared search still filters: %v", app.notes)
	}
	if err := tt.Click("Saved searches"); err != nil {
		t.Fatalf("reopen saved searches: %v", err)
	}
	if err := tt.Click("Apply saved search Work"); err != nil {
		t.Fatalf("apply saved search: %v", err)
	}
	if app.query != "report" || len(app.notes) != 1 || app.notes[0].Title != "Work todo" {
		t.Fatalf("applied query %q, notes %v", app.query, app.notes)
	}
	if app.savedMenuOpen {
		t.Fatal("the saved-search menu stayed open after applying")
	}

	// Deleting removes it from the menu and from the vault.
	if err := tt.Click("Saved searches"); err != nil {
		t.Fatalf("reopen saved searches: %v", err)
	}
	if err := tt.Click("Delete saved search Work"); err != nil {
		t.Fatalf("delete saved search: %v", err)
	}
	tt.Frame()
	if len(app.savedSearches) != 1 {
		t.Fatal("saved search removed before confirmation")
	}
	if err := tt.Click("Delete saved search"); err != nil {
		t.Fatalf("confirm delete saved search: %v", err)
	}
	if len(app.savedSearches) != 0 {
		t.Fatalf("saved searches after delete = %+v", app.savedSearches)
	}
	if fresh := newNativeApp(db); len(fresh.savedSearches) != 0 {
		t.Fatalf("deleted search came back: %+v", fresh.savedSearches)
	}
}

// TestNativeSavedSearchReplacesByName checks that saving an existing name
// updates its query instead of duplicating the entry, and that an empty name
// is refused in the dialog.
func TestNativeSavedSearchReplacesByName(t *testing.T) {
	app, db := openNativeTestApp(t)
	svc := service.New(db)
	addTestNote(t, svc, "", "Alpha", "first")
	addTestNote(t, svc, "", "Beta", "second")
	app.reloadNotes()

	save := func(query, name string) {
		app.query = query
		app.openSaveSearch()
		app.savedName = name
		app.submitSavedSearch()
	}

	save("first", "  Work  ")
	if len(app.savedSearches) != 1 || app.savedSearches[0].Name != "Work" || app.savedSearches[0].Query != "first" {
		t.Fatalf("after first save = %+v", app.savedSearches)
	}
	save("second", "work")
	if len(app.savedSearches) != 1 || app.savedSearches[0].Query != "second" {
		t.Fatalf("after replacing = %+v", app.savedSearches)
	}

	app.savedName = "   "
	app.savedOpen = true
	app.submitSavedSearch()
	if !app.savedOpen || app.savedErr == "" {
		t.Fatalf("empty name accepted: open=%v err=%q", app.savedOpen, app.savedErr)
	}
	app.savedOpen = false
}

// TestNativeBulkMoveAndDelete drives multi-select through the real list: a
// plain click chooses one row, Cmd/Ctrl-click adds another, and the bulk bar
// moves both notes or deletes them after confirmation.
func TestNativeBulkMoveAndDelete(t *testing.T) {
	app, db := openNativeTestApp(t)
	svc := service.New(db)

	inbox := &models.Folder{Title: "Inbox"}
	target := &models.Folder{Title: "Target"}
	for _, f := range []*models.Folder{inbox, target} {
		if err := svc.CreateNotebook(f); err != nil {
			t.Fatalf("CreateNotebook: %v", err)
		}
	}
	alpha := addTestNote(t, svc, inbox.ID, "Alpha", "first")
	beta := addTestNote(t, svc, inbox.ID, "Beta", "second")
	gamma := addTestNote(t, svc, inbox.ID, "Gamma", "third")

	app.reloadNotebooks()
	app.selectedNotebook = inbox.ID
	app.reloadNotes()

	tt := ui.NewTester(app.view, 1200, 800)
	if err := tt.Click("Alpha"); err != nil {
		t.Fatalf("select Alpha: %v", err)
	}
	if err := tt.ClickWith(ui.Cmd, "Beta"); err != nil {
		t.Fatalf("cmd-click Beta: %v", err)
	}
	if app.selection.Len() != 2 {
		t.Fatalf("selection = %d rows, want 2", app.selection.Len())
	}
	if !tt.HasText("2 selected") {
		t.Fatalf("bulk bar missing; texts = %v", tt.Texts())
	}

	// Move both to Target.
	if err := tt.Click("Move selected notes"); err != nil {
		t.Fatalf("open bulk move: %v", err)
	}
	if !app.bulkMoveOpen {
		t.Fatal("bulk move menu did not open")
	}
	if err := tt.Click("Move to Target"); err != nil {
		t.Fatalf("click Target: %v", err)
	}
	for _, n := range []*models.Note{alpha, beta} {
		got, err := svc.GetNote(n.ID)
		if err != nil || got == nil {
			t.Fatalf("GetNote %s: %v", n.ID, err)
		}
		if got.ParentID != target.ID {
			t.Fatalf("%s parent = %q, want %q", n.Title, got.ParentID, target.ID)
		}
	}
	if app.selection.Len() != 0 {
		t.Fatalf("selection after move = %d rows", app.selection.Len())
	}
	if got, _ := svc.GetNote(gamma.ID); got == nil || got.ParentID != inbox.ID {
		t.Fatalf("Gamma moved unexpectedly: %#v", got)
	}

	// Select two others in All notes and delete them; Beta is one of them, so
	// the open editor must let go of it instead of saving it back.
	app.selectedNotebook = ""
	app.reloadNotes()
	tt.Frame()
	if err := tt.Click("Beta"); err != nil {
		t.Fatalf("select Beta: %v", err)
	}
	if app.note == nil || app.note.ID != beta.ID {
		t.Fatalf("open note = %#v, want Beta", app.note)
	}
	if err := tt.ClickWith(ui.Cmd, "Gamma"); err != nil {
		t.Fatalf("cmd-click Gamma: %v", err)
	}
	if err := tt.Click("Delete selected notes"); err != nil {
		t.Fatalf("open bulk delete: %v", err)
	}
	if !app.confirmBulkDelete {
		t.Fatal("bulk delete confirmation did not open")
	}
	if !tt.HasText("The 2 selected notes") {
		t.Fatalf("confirmation does not name the count; texts = %v", tt.Texts())
	}
	if err := tt.Click("Delete notes"); err != nil {
		t.Fatalf("confirm delete: %v", err)
	}
	for _, n := range []*models.Note{beta, gamma} {
		if got, _ := svc.GetNote(n.ID); got != nil {
			t.Fatalf("%s still exists after bulk delete", n.Title)
		}
	}
	if got, _ := svc.GetNote(alpha.ID); got == nil {
		t.Fatal("bulk delete removed an unselected note")
	}
	if app.saveErr != "" {
		t.Fatalf("save error after bulk delete: %s", app.saveErr)
	}
	if app.note != nil && app.note.ID == beta.ID {
		t.Fatal("the editor kept a deleted note")
	}
}

// TestNativeSelectionPrunesOnFilter checks that a selection made under one
// filter does not linger (and count) after the rows change.
func TestNativeSelectionPrunesOnFilter(t *testing.T) {
	app, db := openNativeTestApp(t)
	svc := service.New(db)
	addTestNote(t, svc, "", "Alpha", "first")
	addTestNote(t, svc, "", "Beta", "second")
	app.reloadNotes()

	tt := ui.NewTester(app.view, 1200, 800)
	if err := tt.Click("Alpha"); err != nil {
		t.Fatalf("select Alpha: %v", err)
	}
	if app.selection.Len() != 1 {
		t.Fatalf("selection = %d, want 1", app.selection.Len())
	}
	if err := tt.Click("Search notes"); err != nil {
		t.Fatalf("click search: %v", err)
	}
	tt.Type("second")
	if app.selection.Len() != 0 {
		t.Fatalf("hidden note stayed selected: %d", app.selection.Len())
	}
	if tt.HasText("selected") {
		t.Fatalf("bulk bar still shows; texts = %v", tt.Texts())
	}
}

// TestNativeDragNoteToNotebook simulates a real pointer drag: press a row, move
// past the drag threshold onto a notebook in the sidebar, release. A chosen row
// drags the whole choice.
func TestNativeDragNoteToNotebook(t *testing.T) {
	app, db := openNativeTestApp(t)
	svc := service.New(db)

	inbox := &models.Folder{Title: "Inbox"}
	target := &models.Folder{Title: "Target"}
	for _, f := range []*models.Folder{inbox, target} {
		if err := svc.CreateNotebook(f); err != nil {
			t.Fatalf("CreateNotebook: %v", err)
		}
	}
	alpha := addTestNote(t, svc, inbox.ID, "Alpha", "first")
	beta := addTestNote(t, svc, inbox.ID, "Beta", "second")
	gamma := addTestNote(t, svc, inbox.ID, "Gamma", "third")

	app.reloadNotebooks()
	app.selectedNotebook = inbox.ID
	app.reloadNotes()

	tt := ui.NewTester(app.view, 1200, 800)
	drag := func(from, to string) {
		t.Helper()
		src, ok := tt.Find("Drag note " + from)
		if !ok {
			t.Fatalf("no grip for %q", from)
		}
		dst, ok := tt.Find(to)
		if !ok {
			t.Fatalf("no notebook %q", to)
		}
		sx, sy := src.X+src.W/2, src.Y+src.H/2
		dx, dy := dst.X+dst.W/2, dst.Y+dst.H/2
		tt.Press(sx, sy)
		tt.Move(sx+6, sy+6)
		tt.Move((sx+dx)/2, (sy+dy)/2)
		tt.Move(dx, dy)
		tt.Release(dx, dy)
		tt.Frame()
		tt.Frame()
	}

	// A single, unchosen row drags itself.
	click(t, tt, "Open notebooks")
	drag("Alpha", "Target")
	if got, _ := svc.GetNote(alpha.ID); got == nil || got.ParentID != target.ID {
		t.Fatalf("Alpha parent = %#v, want Target", got)
	}
	if got, _ := svc.GetNote(beta.ID); got == nil || got.ParentID != inbox.ID {
		t.Fatalf("Beta moved with a single-row drag: %#v", got)
	}

	// A chosen row drags the whole choice.
	app.selectedNotebook = ""
	app.reloadNotes()
	tt.Frame()
	if err := tt.Click("Beta"); err != nil {
		t.Fatalf("select Beta: %v", err)
	}
	if err := tt.ClickWith(ui.Cmd, "Gamma"); err != nil {
		t.Fatalf("cmd-click Gamma: %v", err)
	}
	if app.selection.Len() != 2 {
		t.Fatalf("selection = %d, want 2", app.selection.Len())
	}
	drag("Gamma", "Target")
	for _, n := range []*models.Note{beta, gamma} {
		got, _ := svc.GetNote(n.ID)
		if got == nil || got.ParentID != target.ID {
			t.Fatalf("%s parent = %#v, want Target", n.Title, got)
		}
	}
	if app.selection.Len() != 0 {
		t.Fatalf("selection survived the drag: %d", app.selection.Len())
	}
}

// TestSyncNoticeStates pins the footer's words for each state. A lock or a
// lost connection reads as paused, and says when the loop retries; a refusal
// that needs the user keeps the reason.
func TestSyncNoticeStates(t *testing.T) {
	last := time.Date(2026, 10, 19, 15, 4, 0, 0, time.Local)
	retry := time.Date(2026, 10, 19, 15, 5, 0, 0, time.Local)

	cases := []struct {
		name string
		snap syncSnapshot
		line string
		hint string
		err  bool
	}{
		{
			name: "local only",
			snap: syncSnapshot{},
			line: "Local only",
			hint: "Notes stay on this computer.",
		},
		{
			name: "running",
			snap: syncSnapshot{Enabled: true, Running: true},
			line: "Sync queued…",
			hint: "Not synced yet",
		},
		{
			name: "pending after success",
			snap: syncSnapshot{Enabled: true, Pending: true, Last: last},
			line: "Sync queued…",
			hint: "last synced 15:04",
		},
		{
			name: "uploading after success",
			snap: syncSnapshot{Enabled: true, Running: true, Phase: "Uploading", Last: last},
			line: "Uploading…",
			hint: "last synced 15:04",
		},
		{
			name: "downloading after success",
			snap: syncSnapshot{Enabled: true, Running: true, Phase: "Downloading — 2138 of 7541", Last: last},
			line: "Downloading — 2138 of 7541…",
			hint: "last synced 15:04",
		},
		{
			name: "never synced",
			snap: syncSnapshot{Enabled: true},
			line: "Not synced yet",
		},
		{
			name: "synced",
			snap: syncSnapshot{Enabled: true, Last: last},
			line: "Synced 15:04",
		},
		{
			name: "locked",
			snap: syncSnapshot{Enabled: true, Last: last, Err: "sync refused: active exclusive lock", Kind: syncFailureLock, Retry: retry},
			line: "Sync paused · another device is syncing",
			hint: "last synced 15:04 · retrying at 15:05",
		},
		{
			name: "locked without history",
			snap: syncSnapshot{Enabled: true, Err: "lock", Kind: syncFailureLock},
			line: "Sync paused · another device is syncing",
			hint: "Another device is syncing this vault.",
		},
		{
			name: "offline",
			snap: syncSnapshot{Enabled: true, Last: last, Err: "dial tcp: no such host", Kind: syncFailureOffline, Retry: retry},
			line: "Offline · sync paused",
			hint: "Changes are kept here · last synced 15:04 · retrying at 15:05",
		},
		{
			name: "target too new",
			snap: syncSnapshot{Enabled: true, Err: "sync refused: sync target format is newer than this client supports", Kind: syncFailureTarget},
			line: "Sync stopped · target needs a newer Joplin",
			hint: "sync refused: sync target format is newer than this client supports",
			err:  true,
		},
		{
			name: "refused",
			snap: syncSnapshot{Enabled: true, Last: last, Err: "invalid credentials", Kind: syncFailureOther, Retry: retry},
			line: "Sync failed",
			hint: "invalid credentials · last synced 15:04 · retrying at 15:05",
			err:  true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := syncNoticeFor(tc.snap)
			if got.Line != tc.line || got.Hint != tc.hint || got.Err != tc.err {
				t.Fatalf("notice = %+v, want line %q hint %q err %v", got, tc.line, tc.hint, tc.err)
			}
		})
	}
}

// TestSyncRetryPolicy covers which failures the loop retries on its own: a
// lock and a lost connection clear; a rejected login does not.
func TestSyncRetryPolicy(t *testing.T) {
	offline := fmt.Errorf("list folder: %w", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")})
	cases := []struct {
		name string
		err  error
		want time.Duration
	}{
		{"none", nil, 0},
		{"lock", fmt.Errorf("sync refused: %w", jsync.ErrExclusiveLock), syncRetryLock},
		{"network", offline, syncRetryNetwork},
		{"unresolved host", errors.New("Get \"https://api.dropboxapi.com\": dial tcp: lookup api.dropboxapi.com: no such host"), syncRetryNetwork},
		{"target too new", fmt.Errorf("sync refused: %w", jsync.ErrSyncTargetUnsupported), 0},
		{"bad credentials", errors.New("invalid credentials"), 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := retryAfter(tc.err); got != tc.want {
				t.Fatalf("retryAfter(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestNativeLocalOnlyFooterOffersSetup checks the footer of a vault without a
// target: it says notes stay local and the first button opens the sync setup
// instead of a Sync now that could do nothing.
func TestNativeLocalOnlyFooterOffersSetup(t *testing.T) {
	app, _ := openNativeTestApp(t)
	app.loadSettings = func() desktopSettings { return desktopSettings{Target: targetOff} }

	tt := ui.NewTester(app.view, 1200, 800)
	if !tt.HasText("Notes stay on this computer.") {
		t.Fatalf("local-only footer lacks its explanation; texts = %v", tt.Texts())
	}
	if tt.HasText("Sync now") {
		t.Fatalf("local-only footer offers a useless Sync now; texts = %v", tt.Texts())
	}
	if err := tt.Click("Set up sync"); err != nil {
		t.Fatalf("click Set up sync: %v", err)
	}
	if !app.settingsOpen {
		t.Fatal("Set up sync did not open the settings dialog")
	}
}

// TestNativeFooterShowsPausedStates drives the real footer through a backend's
// status: a lock and a lost connection read as paused with the retry time, and
// a finished pass goes back to Synced.
func TestNativeFooterShowsPausedStates(t *testing.T) {
	app, _ := openNativeTestApp(t)
	st := &syncStatus{}
	app.be = &backend{status: st, cfg: &config.Config{SyncTarget: 2}}
	tt := ui.NewTester(app.view, 1200, 800)

	st.finish(fmt.Errorf("sync refused: %w", jsync.ErrExclusiveLock))
	st.retryIn(time.Now().Add(time.Minute))
	tt.Frame()
	if !tt.HasText("Sync paused · another device is syncing") {
		t.Fatalf("lock state not shown; texts = %v", tt.Texts())
	}
	if !tt.HasText("Sync now") {
		t.Fatalf("a configured vault lost its Sync now; texts = %v", tt.Texts())
	}

	st.finish(errors.New("Get \"https://api.dropboxapi.com\": dial tcp: lookup api.dropboxapi.com: no such host"))
	st.retryIn(time.Now().Add(time.Minute))
	tt.Frame()
	if !tt.HasText("Offline · sync paused") {
		t.Fatalf("offline state not shown; texts = %v", tt.Texts())
	}
	if tt.HasText("Sync failed") {
		t.Fatalf("offline was reported as a hard failure; texts = %v", tt.Texts())
	}

	st.start()
	tt.Frame()
	if !tt.HasText("Sync queued…") {
		t.Fatalf("running state not shown; texts = %v", tt.Texts())
	}
	for _, phase := range []string{"Checking remote", "Downloading — 0 of 7541", "Downloading — 2138 of 7541", "Downloading — 9 of 9+", "Downloading — 7541 of 7541", "Checking library", "Decrypting", "Uploading", "Finishing"} {
		st.progress(phase)
		tt.Frame()
		if !tt.HasText(phase + "…") {
			t.Fatalf("phase %q not shown; texts = %v", phase, tt.Texts())
		}
	}
	st.finish(nil)
	tt.Frame()
	if !tt.HasText("Synced " + time.Now().Format("15:04")) {
		t.Fatalf("success state not shown; texts = %v", tt.Texts())
	}
}

// Automatic passes can finish between frames, without a manual Sync now.
func TestNativeAutomaticSyncRefresh(t *testing.T) {
	app, db := openNativeTestApp(t)
	st := &syncStatus{}
	app.be = &backend{status: st, cfg: &config.Config{SyncTarget: 2}}
	wake := make(chan struct{}, 1)
	app.invalidate = func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	tt := ui.NewTester(app.view, 1200, 800)
	t.Cleanup(func() {
		if app.syncPoll != nil {
			app.syncPoll.Stop()
		}
	})
	svc := service.New(db)
	for _, result := range []error{nil, errors.New("push refused")} {
		st.start()
		addTestNote(t, svc, "", fmt.Sprintf("Pulled note %v", result), "Pulled from another device")
		st.finish(result)
		select {
		case <-wake:
		case <-time.After(2 * time.Second):
			t.Fatal("configured idle sync did not request a new frame")
		}
		tt.Frame()
		if !tt.HasText(fmt.Sprintf("Pulled note %v", result)) {
			t.Fatalf("automatic pass did not refresh notes; texts = %v", tt.Texts())
		}
	}
}

// TestNativeSearchKeepsTheKeyboard types a query one frame per character. The
// narrowed search drops the open note and opens the first match; the query
// must still reach the field, and the note that opens must keep its body.
func TestNativeSearchKeepsTheKeyboard(t *testing.T) {
	app, db := openNativeTestApp(t)
	svc := service.New(db)
	alpha := addTestNote(t, svc, "", "Alpha", "kernel report body")
	beta := addTestNote(t, svc, "", "Beta", "nothing to do with it")
	app.reloadNotes()
	app.showNote(beta.ID)

	tt := ui.NewTester(app.view, 1200, 800)
	if err := tt.Click("Search notes"); err != nil {
		t.Fatalf("click search: %v", err)
	}
	for _, r := range "report" {
		tt.Type(string(r))
		tt.Frame()
	}
	// Let a stray edit reach the disk if one was scheduled.
	time.Sleep(600 * time.Millisecond)
	tt.Frame()

	if app.query != "report" {
		t.Fatalf("query = %q, want report", app.query)
	}
	if app.note == nil || app.note.ID != alpha.ID {
		t.Fatalf("open note = %#v, want the first match Alpha", app.note)
	}
	if app.body != "kernel report body" {
		t.Fatalf("the search query leaked into the note: body = %q", app.body)
	}
	got, err := svc.GetNote(alpha.ID)
	if err != nil {
		t.Fatalf("GetNote: %v", err)
	}
	if got.Body != "kernel report body" {
		t.Fatalf("search typing changed the note: body = %q", got.Body)
	}
}

func TestNativeResourcePreview(t *testing.T) {
	registerFonts()
	app, db := openNativeTestApp(t)
	id := "cccccccccccccccccccccccccccccccc"
	if err := db.CreateResource(&models.Resource{ID: id, Mime: "image/png", FileExtension: "png"}); err != nil {
		t.Fatal(err)
	}
	// Use the official filename and an asymmetric picture wider than the page.
	picture := image.NewRGBA(image.Rect(0, 0, 1600, 1000))
	red, blue := color.RGBA{220, 40, 60, 255}, color.RGBA{30, 100, 220, 255}
	draw.Draw(picture, image.Rect(0, 0, 600, 1000), &image.Uniform{red}, image.Point{}, draw.Src)
	draw.Draw(picture, image.Rect(600, 0, 1600, 1000), &image.Uniform{blue}, image.Point{}, draw.Src)
	f, err := os.Create(filepath.Join(db.ResourceDir(), id+".png"))
	if err != nil {
		t.Fatal(err)
	}
	err = png.Encode(f, picture)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	body := "![Paper sample](:/" + id + ")"
	note := addTestNote(t, app.svc, "", "Picture note", body)
	app.reloadNotes()
	app.showNote(note.ID)
	tt := ui.NewTester(app.view, 1280, 800)
	deadline := time.Now().Add(5 * time.Second)
	for {
		tt.Frame()
		r, ok := tt.Find("Paper sample")
		if ok && r.W > 400 && r.H > 200 {
			frame := tt.Image()
			left := color.RGBAModel.Convert(frame.At(int(r.X+r.W/4), int(r.Y+20)))
			right := color.RGBAModel.Convert(frame.At(int(r.X+3*r.W/4), int(r.Y+20)))
			if left == red && right == blue {
				if r.W > 701 {
					t.Fatalf("image overflowed the reading column: %#v", r)
				}
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("resource image did not paint its original colors: rect=%#v, found=%v", r, ok)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if dir := os.Getenv("CAPTURE_DIR"); dir != "" {
		capture(t, dir, "native-resource-preview", tt)
	}
	click(t, tt, "Edit")
	if app.body != body {
		t.Fatal("rendering rewrote the original Markdown")
	}
}

func TestNativePopoverFiltersAndConflictCount(t *testing.T) {
	registerFonts()
	app, db := openNativeTestApp(t)
	svc := service.New(db)

	work := &models.Folder{Title: "Work"}
	archive := &models.Folder{Title: "Archive"}
	for _, nb := range []*models.Folder{work, archive} {
		if err := svc.CreateNotebook(nb); err != nil {
			t.Fatal(err)
		}
	}
	for _, title := range []string{"alpha", "beta", "gamma"} {
		if err := svc.CreateTag(&models.Tag{Title: title}); err != nil {
			t.Fatal(err)
		}
	}
	note := addTestNote(t, svc, work.ID, "Plan", "body")
	conflict := &models.Note{
		ParentID:           work.ID,
		Title:              "Plan (conflict)",
		Body:               "conflict body",
		IsConflict:         1,
		ConflictOriginalID: note.ID,
	}
	if err := svc.CreateNote(conflict); err != nil {
		t.Fatal(err)
	}

	app.reloadNotebooks()
	app.reloadTags()
	app.reloadNotes()
	if app.conflictCount != 1 {
		t.Fatalf("conflictCount = %d, want 1", app.conflictCount)
	}
	if got := app.notebookByID(work.ID); got == nil || got.NoteCount != 1 {
		t.Fatalf("work NoteCount = %#v, want 1 (excluding conflict copy)", got)
	}

	tt := ui.NewTester(app.view, 1280, 800)

	// Filter tags inside the note Tags popover and open New tag… from the menu.
	if err := tt.Click("Note tags"); err != nil {
		t.Fatalf("open note tags: %v", err)
	}
	if err := tt.Click("Filter tags"); err != nil {
		t.Fatalf("focus tag filter: %v", err)
	}
	tt.Type("bet")
	tt.Frame()
	if !tt.HasText("beta") || tt.HasText("gamma") {
		t.Fatalf("tag filter should narrow to beta: %v", tt.Texts())
	}
	if err := tt.Click("New tag…"); err != nil {
		t.Fatalf("click New tag from popover: %v", err)
	}
	if app.tagMenuOpen || !app.tagOpen {
		t.Fatalf("New tag… should close tagMenuOpen (%v) and open tagOpen (%v)", app.tagMenuOpen, app.tagOpen)
	}
	tt.Type("delta")
	if err := tt.Click("Create"); err != nil {
		t.Fatalf("create tag from note popover: %v", err)
	}
	if len(app.noteTags) != 1 || app.noteTags[0].Title != "delta" {
		t.Fatalf("New tag… from note popover should attach tag to open note: %#v", app.noteTags)
	}
	tt.Frame()

	// Filter notebooks inside the Move popover.
	if err := tt.Click("Move note"); err != nil {
		t.Fatalf("open move menu: %v", err)
	}
	if err := tt.Click("Filter notebooks"); err != nil {
		t.Fatalf("focus notebook filter: %v", err)
	}
	tt.Type("arch")
	tt.Frame()
	if err := tt.Click("Move to Archive"); err != nil {
		t.Fatalf("move filtered notebook: %v", err)
	}
	if app.note == nil || app.note.ParentID != archive.ID {
		t.Fatalf("note parent = %v, want %s", app.note, archive.ID)
	}
	if w, a := app.notebookByID(work.ID), app.notebookByID(archive.ID); w.NoteCount != 0 || a.NoteCount != 1 {
		t.Fatalf("counts after move: work=%d archive=%d, want 0 and 1", w.NoteCount, a.NoteCount)
	}

	// Create a top-level notebook even while Archive is selected.
	app.selectedNotebook = archive.ID
	app.openNewNotebook()
	tt.Frame()
	if err := tt.Click("Top level"); err != nil {
		t.Fatalf("select Top level parent: %v", err)
	}
	tt.Type("TopLevelBook")
	if err := tt.Click("Create"); err != nil {
		t.Fatalf("create top-level notebook: %v", err)
	}
	if created := app.notebookByID(app.selectedNotebook); created == nil || created.ParentID != "" || created.Title != "TopLevelBook" {
		t.Fatalf("created top-level notebook = %#v", created)
	}
}

func TestNativeConflictDialogComparisonAndKeepOriginal(t *testing.T) {
	registerFonts()
	app, _ := openNativeTestApp(t)
	original := addTestNote(t, app.svc, "", "Original title", "original body line 1\noriginal body line 2")
	conflict := &models.Note{
		Title:              "Conflict title",
		Body:               "conflict body line 1",
		IsConflict:         1,
		ConflictOriginalID: original.ID,
	}
	if err := app.svc.CreateNote(conflict); err != nil {
		t.Fatalf("CreateNote conflict: %v", err)
	}
	app.selectedNotebook = conflictsRail
	app.reloadNotes()

	tt := ui.NewTester(app.view, 1280, 800)
	if err := tt.Click("Resolve conflict"); err != nil {
		t.Fatalf("click Resolve conflict: %v", err)
	}
	for _, want := range []string{
		"This conflict copy",
		"Original note",
		"Original title",
		"original body line 1",
		"Conflict: 4 words (1 lines)  ·  Original: 8 words (2 lines)",
	} {
		if !tt.HasText(want) {
			t.Fatalf("conflict comparison dialog missing %q; texts = %v", want, tt.Texts())
		}
	}
	if err := tt.Click("Keep original"); err != nil {
		t.Fatalf("click Keep original: %v", err)
	}
	if got, _ := app.svc.GetNote(conflict.ID); got != nil {
		t.Fatalf("conflict copy still exists after Keep original: %#v", got)
	}
	if got, err := app.svc.GetNote(original.ID); err != nil || got == nil || got.Body != original.Body {
		t.Fatalf("original note altered after Keep original: %#v, err %v", got, err)
	}
	if app.conflictCount != 0 {
		t.Fatalf("conflictCount = %d, want 0", app.conflictCount)
	}
}

func TestNativeDarkAppearanceSetting(t *testing.T) {
	registerFonts()
	app, db := openNativeTestApp(t)
	app.loadSettings = func() desktopSettings {
		return desktopSettings{Target: targetOff, APIToken: "test-token", Dark: loadDarkSetting(db)}
	}
	app.applySettings = func(s desktopSettings) error {
		if err := s.validate(); err != nil {
			return err
		}
		return s.save(db)
	}

	tt := ui.NewTester(app.view, 1280, 800)
	if app.dark || darkPalette {
		t.Fatal("expected light mode by default")
	}

	// Preview Dark in Settings, then Cancel -> restores Light.
	app.openSettings()
	tt.Frame()
	if err := tt.Click("Dark"); err != nil {
		t.Fatalf("click Dark segment: %v", err)
	}
	tt.Frame()
	if !app.dark || !darkPalette {
		t.Fatal("clicking Dark should preview dark palette immediately")
	}
	if err := tt.Click("Cancel settings"); err != nil {
		t.Fatalf("click Cancel settings: %v", err)
	}
	tt.Frame()
	if app.dark || darkPalette {
		t.Fatal("cancelling settings should restore previous appearance")
	}

	// Reopen Settings, choose Dark, Save -> persists in key_values.
	app.openSettings()
	tt.Frame()
	if err := tt.Click("Dark"); err != nil {
		t.Fatalf("click Dark segment: %v", err)
	}
	if err := tt.Click("Save settings"); err != nil {
		t.Fatalf("click Save settings: %v", err)
	}
	tt.Frame()
	if !app.dark || !loadDarkSetting(db) {
		t.Fatal("saving Dark appearance did not persist")
	}
	if fresh := newNativeApp(db); !fresh.dark {
		t.Fatal("fresh app did not load persisted dark setting")
	}
	// Reset global palette for subsequent tests.
	applyPalette(false)
}

func TestNativeEditorAttachAndPaste(t *testing.T) {
	registerFonts()
	app, db := openNativeTestApp(t)
	note := addTestNote(t, app.svc, "", "Field Log", "Initial entry.")
	app.reloadNotes()
	app.showNote(note.ID)

	tmpDir := t.TempDir()
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.RGBA{R: 86, G: 108, B: 74, A: 255}}, image.Point{}, draw.Src)
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	imgPath := filepath.Join(tmpDir, "diagram.png")
	if err := os.WriteFile(imgPath, pngBuf.Bytes(), 0o600); err != nil {
		t.Fatalf("write png: %v", err)
	}
	docPath := filepath.Join(tmpDir, "notes.txt")
	if err := os.WriteFile(docPath, []byte("attachment text"), 0o600); err != nil {
		t.Fatalf("write txt: %v", err)
	}

	app.pickAttachmentPaths = func() ([]string, error) {
		return []string{imgPath, docPath}, nil
	}

	tt := ui.NewTester(app.view, 1280, 800)
	if err := tt.Click("Attach file"); err != nil {
		t.Fatalf("click Attach file: %v", err)
	}
	if !strings.Contains(app.body, "![diagram.png](:/") || !strings.Contains(app.body, "[notes.txt](:/") {
		t.Fatalf("expected image and file markdown links in body, got %q", app.body)
	}
	saved, err := app.svc.GetNote(note.ID)
	if err != nil || saved == nil {
		t.Fatalf("GetNote: %v", err)
	}
	linked, err := db.GetResourcesByNote(saved.ID)
	if err != nil || len(linked) != 2 {
		t.Fatalf("GetResourcesByNote = %#v, err %v (body=%q)", linked, err, saved.Body)
	}

	// Paste a PNG image from the clipboard into the focused editor.
	if err := tt.Click("Edit"); err != nil {
		t.Fatalf("click Edit: %v", err)
	}
	if !tt.Focused("Note body") {
		if err := tt.Click("Note body"); err != nil {
			t.Fatalf("focus Note body: %v", err)
		}
	}
	tt.SetClipboardData(transfer.New(transfer.NewItem(transfer.Bytes(transfer.PNG, pngBuf.Bytes()))))
	tt.Key(ui.Cmd, ui.KeyV)
	tt.Frame()
	if !strings.Contains(app.body, "![pasted-image.png](:/") {
		t.Fatalf("expected pasted image link in body, got %q", app.body)
	}
	linked, err = db.GetResourcesByNote(note.ID)
	if err != nil || len(linked) != 3 {
		t.Fatalf("GetResourcesByNote after paste = %#v, err %v", linked, err)
	}
}


