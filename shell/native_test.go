package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/internal/store"
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
// types into the focused body, waits for the autosave deadline, switches to
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
	app.reloadNotebooks()
	app.reloadNotes()
	if app.note == nil || app.note.ID != seed.ID || !app.focusBody {
		t.Fatalf("seed note not focused: %#v", app.note)
	}

	tt := ui.NewTester(app.view, 1200, 800)
	if !tt.HasText("Seed") {
		t.Fatalf("list does not show the seed note; texts = %v", tt.Texts())
	}
	if !tt.HasText("Local only") {
		t.Fatalf("status does not report a backend-less app as local only; texts = %v", tt.Texts())
	}

	// The body has the keyboard on load, so typing goes straight into the
	// note and the debounce timer writes it.
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

	// Alt+Left/Right are the keyboard equivalents.
	tt.Key(ui.Alt, ui.KeyLeft)
	if app.note == nil || app.note.ID != beta.ID {
		t.Fatalf("after Alt+Left = %#v, want beta", app.note)
	}
	tt.Key(ui.Alt, ui.KeyRight)
	if app.note == nil || app.note.ID != alpha.ID {
		t.Fatalf("after Alt+Right = %#v, want alpha", app.note)
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
	if err := tt.Click("Delete notebook"); err != nil {
		t.Fatalf("confirm Delete notebook: %v", err)
	}
	if !app.notebookBlocked {
		t.Fatal("deleting a parent notebook was not blocked")
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
	st.finish(errors.New("offline"))
	if snap := st.snapshot(true); snap.Running || snap.Pending || snap.Err != "offline" || snap.Last.IsZero() {
		t.Fatalf("failed snapshot = %#v", snap)
	}
	st.request()
	st.start()
	st.finish(nil)
	if snap := st.snapshot(true); snap.Err != "" || snap.Last.IsZero() {
		t.Fatalf("successful snapshot = %#v", snap)
	}

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
	if !app.editing {
		t.Fatal("a newly opened note should be in edit mode")
	}

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
