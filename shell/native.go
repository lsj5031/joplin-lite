package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/internal/store"
	"github.com/lsj5031/joplin-lite/service"
)

const (
	// nativePageSize is how many note rows one request loads. Keyset paging
	// keeps the list's memory flat as the vault grows; Phase 0 loaded 500
	// rows at once to compare shells on the same snapshot.
	nativePageSize = 100
	// nativeNotebookLimit is effectively "all notebooks": there are usually
	// far fewer than this.
	nativeNotebookLimit = 500
	// nativeSaveDelay is the autosave debounce. Typing resets the timer; the
	// write happens on the UI thread once the deadline passes.
	nativeSaveDelay = 500 * time.Millisecond
	// conflictsRail is the notebook selection that shows Joplin's virtual
	// Conflicts notebook. It is not a folder ID.
	conflictsRail = "__conflicts__"
)

// nativeApp is the native workspace: notebooks, a paged note list, and the
// selected note as a Markdown editor or a rendered preview. It is a pure Go
// MyGO UI over service/, with no webview and no HTTP.
type nativeApp struct {
	svc *service.Service

	notebooks     []*models.Folder // flat, parents before children
	notebookTree  []*models.Folder // roots with Children
	notebookDepth map[string]int
	tags          []*models.Tag
	noteTags      []*models.Tag
	selectedTag   string
	notes         []*models.NoteSummary
	hasMore       bool
	cursor        string

	selectedNotebook string // "" is All notes
	selectedNote     int
	selectedID       string
	list             ui.ListState

	query       string
	editing     bool
	note        *models.Note
	title       string
	body        string
	dirty       bool
	saveDue     time.Time
	saveTimer   *time.Timer
	invalidate  func()
	savedAt     time.Time
	saveErr     string
	focusBody   bool
	focusSearch bool

	todo          bool
	confirmDelete bool
	moveOpen      bool

	// settings is the dialog's draft; settingsPick indexes the target segment
	// (the labels and targets come from settingsTargets).
	settingsOpen  bool
	settings      desktopSettings
	settingsPick  int
	settingsErr   string
	loadSettings  func() desktopSettings
	applySettings func(desktopSettings) error

	tagMenuOpen           bool
	tagActions            bool
	tagOpen               bool
	tagRename             bool
	tagTitle              string
	tagErr                string
	confirmDeleteTag      bool
	resolveConflict       bool
	notebookActions       bool
	notebookOpen          bool
	notebookRename        bool
	notebookTitle         string
	notebookParent        string
	notebookErr           string
	confirmDeleteNotebook bool
	notebookBlocked       bool

	// history is the visited note IDs, ending at histPos. Moving between
	// notes records a step; walking history itself does not.
	history   []string
	histPos   int
	restoring bool

	// be is the in-process backend; nil in headless tests. syncWatch keeps a
	// poll alive while a pass runs so the workspace can refresh when it ends.
	be        *backend
	syncWatch bool
	syncPoll  *time.Timer

	md markdown.Options
}

func newNativeApp(db *store.DB) *nativeApp {
	app := &nativeApp{svc: service.New(db), selectedNote: -1, editing: true}
	app.reloadNotebooks()
	app.reloadTags()
	app.reloadNotes()
	app.remember(app.selectedID)
	return app
}

func (a *nativeApp) reloadNotebooks() {
	tree, err := a.svc.NotebookTree()
	if err != nil {
		return
	}
	a.notebookTree = tree
	a.notebooks = nil
	a.notebookDepth = make(map[string]int)
	var walk func(nodes []*models.Folder, depth int)
	walk = func(nodes []*models.Folder, depth int) {
		for _, f := range nodes {
			a.notebookDepth[f.ID] = depth
			a.notebooks = append(a.notebooks, f)
			walk(f.Children, depth+1)
		}
	}
	walk(tree, 0)
	if a.selectedNotebook != "" && a.selectedNotebook != conflictsRail && a.notebookByID(a.selectedNotebook) == nil {
		a.selectedNotebook = ""
	}
}

func (a *nativeApp) reloadTags() {
	page, err := a.svc.ListTags(service.Page{Limit: nativeNotebookLimit})
	if err != nil {
		return
	}
	a.tags = page.Tags
	if a.selectedTag != "" && a.tagByID(a.selectedTag) == nil {
		a.selectedTag = ""
	}
}

func (a *nativeApp) tagByID(id string) *models.Tag {
	for _, tag := range a.tags {
		if tag.ID == id {
			return tag
		}
	}
	return nil
}

func (a *nativeApp) openNewTag() {
	a.tagActions = false
	a.tagRename = false
	a.tagTitle = ""
	a.tagErr = ""
	a.tagOpen = true
}

func (a *nativeApp) openRenameTag() {
	tag := a.tagByID(a.selectedTag)
	if tag == nil {
		return
	}
	a.tagActions = false
	a.tagRename = true
	a.tagTitle = tag.Title
	a.tagErr = ""
	a.tagOpen = true
}

func (a *nativeApp) submitTag() {
	title := strings.TrimSpace(a.tagTitle)
	if title == "" {
		a.tagErr = "Give the tag a name."
		return
	}
	if a.tagRename {
		tag := a.tagByID(a.selectedTag)
		if tag == nil {
			a.tagErr = "That tag no longer exists."
			return
		}
		updated := *tag
		updated.Title = title
		if err := a.svc.UpdateTag(&updated); err != nil {
			a.tagErr = err.Error()
			return
		}
	} else {
		if err := a.svc.CreateTag(&models.Tag{Title: title}); err != nil {
			a.tagErr = err.Error()
			return
		}
	}
	a.tagOpen = false
	a.tagErr = ""
	a.reloadTags()
}

func (a *nativeApp) deleteTag() {
	id := a.selectedTag
	if id == "" {
		return
	}
	if err := a.svc.DeleteTag(id); err != nil {
		a.saveErr = err.Error()
		return
	}
	a.selectedTag = ""
	a.selectedID = ""
	a.reloadTags()
	a.reloadNotes()
}

// tagDialogs draws the create/rename modal and the delete confirmation.
func (a *nativeApp) tagDialogs(c *ui.Context, t *ui.Theme) {
	if a.tagOpen {
		ui.Modal(c, &a.tagOpen, func() {
			title, action := "New tag", "Create"
			if a.tagRename {
				title, action = "Rename tag", "Save"
			}
			ui.Text(c, title).FontSize(17).Bold()
			input := ui.TextInput(c.Key("tag-name"), &a.tagTitle).
				Placeholder("Tag name").
				Label("Tag name").
				Width(280).
				AutoFocus()
			if input.Submitted() {
				a.submitTag()
			}
			if a.tagErr != "" {
				ui.Text(c, a.tagErr).FontSize(12).TextColor(t.Danger)
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "Cancel").Clicked() {
					a.tagOpen = false
				}
				if ui.PrimaryButton(c, action).Clicked() {
					a.submitTag()
				}
			})
		})
	}
	if a.confirmDeleteTag {
		if ui.AlertDialog(c, &a.confirmDeleteTag, "Delete tag?",
			"This removes the tag from every note on this device and queues the deletion for sync.",
			"Delete tag", "Cancel") == 0 {
			a.deleteTag()
		}
	}
}

// tagActionsMenu is the Rename/Delete menu for the selected tag.
func (a *nativeApp) tagActionsMenu(c *ui.Context, t *ui.Theme) {
	items := []struct {
		label string
		run   func()
	}{
		{"Rename tag…", a.openRenameTag},
		{"Delete tag…", func() {
			a.tagActions = false
			a.confirmDeleteTag = true
		}},
	}
	ui.Column(c).Width(170).Padding(4).Gap(2).Children(func() {
		for _, item := range items {
			entry := ui.Row(c.Key("tag-action:"+item.label)).Padding(6, 10).Radius(5)
			if entry.Hovered() {
				entry.Background(t.Accent).TextColor(t.AccentText)
			}
			if entry.Clicked() {
				a.tagActions = false
				item.run()
			}
			entry.Children(func() { ui.Text(c, item.label) })
		}
	})
}

func (a *nativeApp) reloadNoteTags() {
	if a.note == nil {
		a.noteTags = nil
		return
	}
	tags, err := a.svc.NoteTags(a.note.ID)
	if err != nil {
		return
	}
	a.noteTags = tags
}

func (a *nativeApp) hasNoteTag(id string) bool {
	for _, tag := range a.noteTags {
		if tag.ID == id {
			return true
		}
	}
	return false
}

// toggleTag adds or removes a tag on the open note.
func (a *nativeApp) toggleTag(id string) {
	if a.note == nil {
		return
	}
	a.flush()
	var err error
	if a.hasNoteTag(id) {
		err = a.svc.RemoveTag(a.note.ID, id)
	} else {
		err = a.svc.AddTag(a.note.ID, id)
	}
	if err != nil {
		a.saveErr = err.Error()
		return
	}
	a.reloadNoteTags()
	if a.selectedTag != "" {
		a.reloadNotes()
	}
}

func (a *nativeApp) notebookByID(id string) *models.Folder {
	for _, nb := range a.notebooks {
		if nb.ID == id {
			return nb
		}
	}
	return nil
}

func (a *nativeApp) notebookHasChildren(id string) bool {
	for _, nb := range a.notebooks {
		if nb.ParentID == id {
			return true
		}
	}
	return false
}

func (a *nativeApp) noteFilter(cursor string) service.NoteFilter {
	f := service.NoteFilter{
		Page:   service.Page{Limit: nativePageSize},
		Cursor: cursor,
		Query:  a.query,
	}
	if a.query == "" {
		switch {
		case a.selectedNotebook == conflictsRail:
			f.ConflictOnly = true
		case a.selectedTag != "":
			f.TagID = a.selectedTag
		case a.selectedNotebook != "":
			f.FolderID = a.selectedNotebook
		}
	}
	return f
}

// reloadNotes reloads the first page for the current filter, keeping the
// selected note when it is still in the results.
func (a *nativeApp) reloadNotes() {
	page, err := a.svc.ListNoteSummaries(a.noteFilter(""))
	if err != nil {
		a.saveErr = err.Error()
		return
	}
	a.notes = page.Notes
	a.hasMore, a.cursor = page.HasMore, page.NextCursor

	idx := -1
	for i, n := range a.notes {
		if n.ID == a.selectedID {
			idx = i
			break
		}
	}
	if idx < 0 && len(a.notes) > 0 {
		idx = 0
	}
	a.selectedNote = idx
	a.loadNote()
}

// loadMore appends the next keyset page without disturbing the selection.
func (a *nativeApp) loadMore() {
	if !a.hasMore || a.cursor == "" {
		return
	}
	page, err := a.svc.ListNoteSummaries(a.noteFilter(a.cursor))
	if err != nil {
		a.saveErr = err.Error()
		return
	}
	a.notes = append(a.notes, page.Notes...)
	a.hasMore, a.cursor = page.HasMore, page.NextCursor
}

// loadNote flushes pending edits before switching, then loads the selected
// row. Selecting the note already open is a no-op, so search reloads do not
// reset the editor.
func (a *nativeApp) loadNote() {
	if a.selectedNote < 0 || a.selectedNote >= len(a.notes) {
		a.flush()
		a.note = nil
		a.selectedID = ""
		a.title, a.body = "", ""
		a.dirty = false
		a.todo = false
		a.noteTags = nil
		return
	}
	a.showNote(a.notes[a.selectedNote].ID)
	if !a.restoring {
		a.remember(a.selectedID)
	}
}

// showNote loads a note by ID and selects its row when it is visible. It
// flushes pending edits first and never records history.
func (a *nativeApp) showNote(id string) {
	if id == "" {
		return
	}
	if a.note != nil && a.note.ID == id {
		a.selectedID = id
		a.selectedNote = -1
		for i, row := range a.notes {
			if row.ID == id {
				a.selectedNote = i
				break
			}
		}
		return
	}
	a.flush()
	note, err := a.svc.GetNote(id)
	if err != nil || note == nil {
		return
	}
	a.note = note
	a.selectedID = id
	a.title, a.body = note.Title, note.Body
	a.editing = true
	a.focusBody = true
	a.dirty = false
	a.saveErr = ""
	a.todo = note.IsTodo != 0
	a.reloadNoteTags()
	a.selectedNote = -1
	for i, row := range a.notes {
		if row.ID == id {
			a.selectedNote = i
			break
		}
	}
}

// remember makes id the current history step, dropping any forward steps.
func (a *nativeApp) remember(id string) {
	if id == "" {
		return
	}
	if a.histPos >= 0 && a.histPos < len(a.history) && a.history[a.histPos] == id {
		return
	}
	if a.histPos+1 < len(a.history) {
		a.history = append(a.history[:a.histPos+1], id)
	} else {
		a.history = append(a.history, id)
	}
	a.histPos = len(a.history) - 1
}

func (a *nativeApp) canBack() bool    { return a.histPos > 0 }
func (a *nativeApp) canForward() bool { return a.histPos+1 < len(a.history) }

// noteInRows reports whether id is in the current page of the list.
func (a *nativeApp) noteInRows(id string) bool {
	for _, row := range a.notes {
		if row.ID == id {
			return true
		}
	}
	return false
}

// goHistory moves delta steps through the visited notes. When the target is
// hidden by the current search or notebook, it falls back to All notes so the
// note can be shown again.
func (a *nativeApp) goHistory(delta int) {
	pos := a.histPos + delta
	if pos < 0 || pos >= len(a.history) {
		return
	}
	a.flush()
	a.histPos = pos
	id := a.history[pos]
	if !a.noteInRows(id) {
		a.query = ""
		a.selectedNotebook = ""
		a.restoring = true
		a.reloadNotes()
		a.restoring = false
	}
	if a.note == nil || a.note.ID != id {
		a.showNote(id)
	}
}

// markDirty records an edit and arms the debounce timer. The timer only asks
// for a frame: the write stays on the UI thread, as the store expects.
func (a *nativeApp) markDirty() {
	a.dirty = true
	a.saveDue = time.Now().Add(nativeSaveDelay)
	if a.invalidate == nil {
		return
	}
	if a.saveTimer == nil {
		a.saveTimer = time.AfterFunc(nativeSaveDelay, a.invalidate)
		return
	}
	a.saveTimer.Reset(nativeSaveDelay)
}

// tick saves the note when its debounce deadline has passed.
func (a *nativeApp) tick() {
	if a.dirty && !time.Now().Before(a.saveDue) {
		a.flush()
	}
}

// flush writes the editor's title and body if they changed. It is safe to
// call when there is nothing to save.
func (a *nativeApp) flush() {
	if !a.dirty || a.note == nil {
		return
	}
	n := *a.note
	n.Title = a.title
	n.Body = a.body
	if err := a.svc.UpdateNote(&n); err != nil {
		a.saveErr = err.Error()
		return
	}
	a.note = &n
	a.dirty = false
	a.savedAt = time.Now()
	a.saveErr = ""
	for _, row := range a.notes {
		if row.ID == n.ID {
			row.Title = n.Title
			row.Excerpt = excerpt(n.Body)
			row.UpdatedTime = n.UpdatedTime
			break
		}
	}
}

// newNote creates a note in the selected notebook (or the first one) and
// opens it for editing.
func (a *nativeApp) newNote() {
	a.flush()
	if a.selectedNotebook == conflictsRail {
		a.selectedNotebook = ""
	}
	parent := a.selectedNotebook
	if parent == "" {
		if len(a.notebooks) == 0 {
			folder := &models.Folder{Title: "Notes"}
			if err := a.svc.CreateNotebook(folder); err != nil {
				a.saveErr = err.Error()
				return
			}
			a.reloadNotebooks()
			parent = folder.ID
		} else {
			parent = a.notebooks[0].ID
		}
	}
	note := &models.Note{ParentID: parent}
	if err := a.svc.CreateNote(note); err != nil {
		a.saveErr = err.Error()
		return
	}
	if a.selectedTag != "" {
		if err := a.svc.AddTag(note.ID, a.selectedTag); err != nil {
			a.saveErr = err.Error()
		}
	}
	a.query = ""
	a.selectedID = note.ID
	a.note = nil
	a.dirty = false
	a.editing = true
	a.focusBody = true
	a.reloadNotes()
	a.remember(note.ID)
}

// moveTo moves the open note to another notebook. When that removes it from
// the current notebook view, the next note in the view is opened.
func (a *nativeApp) moveTo(folderID string) {
	if a.note == nil || folderID == "" || a.note.ParentID == folderID {
		return
	}
	a.flush()
	n := *a.note
	n.ParentID = folderID
	if err := a.svc.UpdateNote(&n); err != nil {
		a.saveErr = err.Error()
		return
	}
	a.note = &n
	for _, row := range a.notes {
		if row.ID == n.ID {
			row.ParentID = folderID
			row.UpdatedTime = n.UpdatedTime
			break
		}
	}
	if a.selectedNotebook != "" && a.selectedNotebook != conflictsRail && a.selectedNotebook != folderID {
		a.selectedID = ""
		a.reloadNotes()
	}
}

func (a *nativeApp) openNewNotebook() {
	a.notebookActions = false
	a.notebookRename = false
	a.notebookParent = a.selectedNotebook
	if a.notebookParent == conflictsRail {
		a.notebookParent = ""
	}
	a.notebookTitle = ""
	a.notebookErr = ""
	a.notebookOpen = true
}

func (a *nativeApp) openRenameNotebook() {
	nb := a.notebookByID(a.selectedNotebook)
	if nb == nil {
		return
	}
	a.notebookActions = false
	a.notebookRename = true
	a.notebookParent = nb.ParentID
	a.notebookTitle = nb.Title
	a.notebookErr = ""
	a.notebookOpen = true
}

// submitNotebook creates or renames the notebook whose dialog is open.
func (a *nativeApp) submitNotebook() {
	title := strings.TrimSpace(a.notebookTitle)
	if title == "" {
		a.notebookErr = "Give the notebook a name."
		return
	}
	if a.notebookRename {
		nb := a.notebookByID(a.selectedNotebook)
		if nb == nil {
			a.notebookErr = "That notebook no longer exists."
			return
		}
		updated := *nb
		updated.Title = title
		if err := a.svc.UpdateNotebook(&updated); err != nil {
			a.notebookErr = err.Error()
			return
		}
	} else {
		nb := &models.Folder{Title: title, ParentID: a.notebookParent}
		if err := a.svc.CreateNotebook(nb); err != nil {
			a.notebookErr = err.Error()
			return
		}
		a.selectedNotebook = nb.ID
		a.selectedID = ""
		a.history = nil
		a.histPos = 0
	}
	a.notebookOpen = false
	a.notebookErr = ""
	a.reloadNotebooks()
	if !a.notebookRename {
		a.reloadNotes()
	}
}

// deleteNotebook removes the selected notebook and its notes. Child
// notebooks must be moved or deleted first, so a delete cannot orphan a
// subtree.
func (a *nativeApp) deleteNotebook() {
	id := a.selectedNotebook
	if id == "" {
		return
	}
	if a.notebookHasChildren(id) {
		a.notebookBlocked = true
		return
	}
	a.dirty = false
	filter := service.NoteFilter{FolderID: id, Page: service.Page{Limit: 100}}
	for {
		page, err := a.svc.ListNoteSummaries(filter)
		if err != nil {
			a.saveErr = err.Error()
			return
		}
		for _, note := range page.Notes {
			if err := a.svc.DeleteNote(note.ID); err != nil {
				a.saveErr = err.Error()
				return
			}
		}
		if !page.HasMore || page.NextCursor == "" {
			break
		}
		filter.Cursor = page.NextCursor
	}
	if err := a.svc.DeleteNotebook(id); err != nil {
		a.saveErr = err.Error()
		return
	}
	a.selectedNotebook = ""
	a.selectedID = ""
	a.title, a.body = "", ""
	a.note = nil
	a.history = nil
	a.histPos = 0
	a.reloadNotebooks()
	a.reloadNotes()
}

// notebookDialogs draws the create/rename modal and the delete confirmations.
func (a *nativeApp) notebookDialogs(c *ui.Context, t *ui.Theme) {
	if a.notebookOpen {
		ui.Modal(c, &a.notebookOpen, func() {
			title, action := "New notebook", "Create"
			if a.notebookRename {
				title, action = "Rename notebook", "Save"
			} else {
				parent := "top level"
				if a.notebookParent != "" {
					if nb := a.notebookByID(a.notebookParent); nb != nil {
						parent = "“" + nb.Title + "”"
					}
				}
				ui.Text(c, "Inside "+parent).FontSize(12).TextColor(t.TextMuted)
			}
			ui.Text(c, title).FontSize(17).Bold()
			input := ui.TextInput(c.Key("notebook-name"), &a.notebookTitle).
				Placeholder("Notebook name").
				Label("Notebook name").
				Width(320).
				AutoFocus()
			if input.Submitted() {
				a.submitNotebook()
			}
			if a.notebookErr != "" {
				ui.Text(c, a.notebookErr).FontSize(12).TextColor(t.Danger)
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "Cancel").Clicked() {
					a.notebookOpen = false
				}
				if ui.PrimaryButton(c, action).Clicked() {
					a.submitNotebook()
				}
			})
		})
	}
	if a.notebookBlocked {
		ui.AlertDialog(c, &a.notebookBlocked, "Notebook has child notebooks",
			"Move or delete its child notebooks first.", "OK")
	}
	if a.confirmDeleteNotebook {
		name, count := "this notebook", 0
		if nb := a.notebookByID(a.selectedNotebook); nb != nil {
			name, count = "“"+nb.Title+"”", nb.NoteCount
		}
		if ui.AlertDialog(c, &a.confirmDeleteNotebook, "Delete "+name+"?",
			fmt.Sprintf("This deletes the notebook and %d note(s) on this device, and queues the deletions for sync.", count),
			"Delete notebook", "Cancel") == 0 {
			a.deleteNotebook()
		}
	}
}

// setTodo turns the open note into a todo (or back), saving pending edits
// first so the toggle cannot discard them.
func (a *nativeApp) setTodo(on bool) {
	if a.note == nil {
		return
	}
	a.flush()
	n := *a.note
	if on {
		n.IsTodo = 1
		n.TodoCompleted = 0
	} else {
		n.IsTodo = 0
		n.TodoCompleted = 0
	}
	if err := a.svc.UpdateNote(&n); err != nil {
		a.saveErr = err.Error()
		return
	}
	a.note = &n
	a.todo = n.IsTodo != 0
	for _, row := range a.notes {
		if row.ID == n.ID {
			row.IsTodo = n.IsTodo
			row.TodoCompleted = n.TodoCompleted
			break
		}
	}
}

// keepConflictVersion copies the conflict's title and body onto the original
// note and deletes the conflict. If the original is gone, the conflict is
// simply deleted.
func (a *nativeApp) keepConflictVersion() {
	if a.note == nil || a.note.IsConflict == 0 {
		return
	}
	a.flush()
	conflict := *a.note
	originalID := conflict.ConflictOriginalID
	if originalID != "" {
		original, err := a.svc.GetNote(originalID)
		if err == nil && original != nil {
			updated := *original
			updated.Title, updated.Body = conflict.Title, conflict.Body
			if err := a.svc.UpdateNote(&updated); err != nil {
				a.saveErr = err.Error()
				return
			}
		}
	}
	if err := a.svc.DeleteNote(conflict.ID); err != nil {
		a.saveErr = err.Error()
		return
	}
	a.note = nil
	a.selectedID = ""
	a.title, a.body = "", ""
	a.noteTags = nil
	a.reloadNotes()
}

// deleteNote removes the open note and loads the next one. Pending edits are
// discarded rather than written to a note that is going away.
func (a *nativeApp) deleteNote() {
	if a.note == nil {
		return
	}
	id := a.note.ID
	a.dirty = false
	if err := a.svc.DeleteNote(id); err != nil {
		a.saveErr = err.Error()
		return
	}
	a.note = nil
	a.selectedID = ""
	a.title, a.body = "", ""
	a.todo = false
	a.noteTags = nil
	a.history = nil
	a.histPos = 0
	a.reloadNotes()
}

// toggleEdit switches between editing and preview, saving first when leaving
// the editor.
func (a *nativeApp) toggleEdit() {
	if a.editing {
		a.flush()
	}
	a.editing = !a.editing
	if a.editing {
		a.focusBody = true
	}
}

func (a *nativeApp) titleOfCurrent() string {
	if a.title != "" {
		return a.title
	}
	return "Untitled"
}

// syncState reads the backend's sync state; an app without a backend (the
// headless tests, or a build with sync off) reports it disabled.
func (a *nativeApp) syncState() syncSnapshot {
	if a.be == nil {
		return syncSnapshot{}
	}
	return a.be.SyncState()
}

func (a *nativeApp) syncLine() (string, bool) {
	st := a.syncState()
	switch {
	case !st.Enabled:
		return "Local only", false
	case st.Running || st.Pending:
		return "Syncing…", false
	case st.Err != "":
		return "Sync failed: " + clip(st.Err, 40), true
	case st.Last.IsZero():
		return "Not synced yet", false
	default:
		return "Synced " + st.Last.Format("15:04"), false
	}
}

// watchSync starts polling if an initial background sync is already running
// when the first frame is built.
func (a *nativeApp) watchSync() {
	st := a.syncState()
	if st.Enabled && (st.Running || st.Pending) {
		a.syncWatch = true
		a.scheduleSyncPoll()
	}
}

func (a *nativeApp) scheduleSyncPoll() {
	if a.syncPoll == nil {
		a.syncPoll = time.AfterFunc(time.Second, a.pollSync)
		return
	}
	a.syncPoll.Reset(time.Second)
}

// pollSync only asks for a frame; the UI thread reads the status and decides
// whether to keep polling.
func (a *nativeApp) pollSync() {
	if a.invalidate != nil {
		a.invalidate()
	}
}

// tickSync keeps the poll alive during a pass and refreshes once when it ends.
func (a *nativeApp) tickSync() {
	if !a.syncWatch {
		return
	}
	st := a.syncState()
	if st.Running || st.Pending {
		a.scheduleSyncPoll()
		return
	}
	a.syncWatch = false
	a.refreshAfterSync()
}

// syncNow flushes local edits, queues a sync, and starts watching for its end.
func (a *nativeApp) syncNow() {
	if a.be == nil {
		return
	}
	a.flush()
	if !a.be.SyncNow() {
		return
	}
	a.syncWatch = true
	a.scheduleSyncPoll()
}

// refreshAfterSync reloads notebooks and rows, and updates the open note only
// when it has no unsaved edits, so a pull cannot overwrite what is being typed.
func (a *nativeApp) refreshAfterSync() {
	a.reloadNotebooks()
	a.reloadTags()
	a.reloadNotes()
	a.reloadNoteTags()
	if a.note == nil || a.dirty {
		return
	}
	fresh, err := a.svc.GetNote(a.note.ID)
	if err != nil || fresh == nil {
		return
	}
	// Compare content, not only updated_time: a pull can land in the same
	// millisecond as the read that opened the note.
	if fresh.Title == a.title && fresh.Body == a.body && fresh.IsTodo == a.note.IsTodo {
		a.note = fresh
		return
	}
	a.note = fresh
	a.title, a.body = fresh.Title, fresh.Body
	a.todo = fresh.IsTodo != 0
}

// openSettings loads the effective settings into the dialog's draft.
func (a *nativeApp) openSettings() {
	switch {
	case a.loadSettings != nil:
		a.settings = a.loadSettings()
	case a.be != nil:
		a.settings = a.be.Settings()
	default:
		a.settings = desktopSettings{}
	}
	a.settingsErr = ""
	a.settingsPick = 0
	_, targets := a.settingsTargets()
	for i, target := range targets {
		if target == a.settings.Target {
			a.settingsPick = i
		}
	}
	a.settingsOpen = true
}

// settingsTargets are the segments the dialog offers. Dropbox and S3 stay
// config-file work, but a vault already using one keeps it as an extra
// segment instead of being silently switched off.
func (a *nativeApp) settingsTargets() ([]string, []int) {
	labels := []string{"Off", "File system", "Joplin Server"}
	targets := []int{targetOff, targetFolder, targetServer}
	if a.settings.Target == 7 || a.settings.Target == 8 {
		labels = append(labels, targetLabel(a.settings.Target))
		targets = append(targets, a.settings.Target)
	}
	return labels, targets
}

// submitSettings validates and applies the draft. The backend persists it and
// restarts sync; a rejection stays in the dialog instead of closing it.
func (a *nativeApp) submitSettings() {
	apply := a.applySettings
	if apply == nil && a.be != nil {
		apply = a.be.ApplySettings
	}
	if apply == nil {
		a.settingsErr = "Settings are unavailable in this build."
		return
	}
	if err := apply(a.settings); err != nil {
		a.settingsErr = err.Error()
		return
	}
	a.settingsOpen = false
	a.settingsErr = ""
	// Even when the new target is off, one poll refreshes rows after the
	// engine's last pass has stopped.
	a.syncWatch = true
	a.scheduleSyncPoll()
}

// settingsDialog is the native settings form: sync target and its fields, the
// E2EE master password, and the clipper token.
func (a *nativeApp) settingsDialog(c *ui.Context, t *ui.Theme) {
	if !a.settingsOpen {
		return
	}
	ui.Modal(c, &a.settingsOpen, func() {
		ui.Text(c, "Settings").FontSize(17).Bold()
		ui.Text(c, "Sync and the web clipper, without editing a config file.").
			FontSize(12).TextColor(t.TextMuted)

		labels, targets := a.settingsTargets()
		seg := ui.Segmented(c, &a.settingsPick, labels...).Label("Sync target")
		if seg.Changed() {
			a.settings.Target = targets[a.settingsPick]
		}

		switch a.settings.Target {
		case targetFolder:
			ui.Text(c, "Sync folder").FontSize(12).Bold()
			ui.TextInput(c.Key("settings-folder"), &a.settings.LocalDir).
				Placeholder("Folder Joplin's File system target uses").
				Label("Sync folder path").Width(360)
		case targetServer:
			ui.Text(c, "Server URL").FontSize(12).Bold()
			ui.TextInput(c.Key("settings-url"), &a.settings.ServerURL).
				Placeholder("https://sync.example.com").
				Label("Joplin Server URL").Width(360)
			ui.Text(c, "Email").FontSize(12).Bold()
			ui.TextInput(c.Key("settings-user"), &a.settings.Username).
				Label("Server account email").Width(360)
			ui.Text(c, "Password").FontSize(12).Bold()
			ui.TextInput(c.Key("settings-pass"), &a.settings.Password).
				Label("Server account password").Width(360)
		case 7, 8:
			ui.Text(c, targetLabel(a.settings.Target)+" is configured in the config file; this dialog keeps it.").
				FontSize(11).TextColor(t.TextMuted)
		case targetOff:
			ui.Text(c, "Notes stay on this device; nothing is sent anywhere.").
				FontSize(11).TextColor(t.TextMuted)
		}

		ui.Text(c, "Master password (E2EE)").FontSize(12).Bold()
		ui.TextInput(c.Key("settings-e2ee"), &a.settings.MasterPassword).
			Placeholder("Leave empty when E2EE is off").
			Label("E2EE master password").Width(360)
		ui.Text(c, "Decrypts synced notes on this device. Stored locally and never sent anywhere.").
			FontSize(11).TextColor(t.TextMuted)

		ui.Text(c, "Clipper token").FontSize(12).Bold()
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.TextInput(c.Key("settings-token"), &a.settings.APIToken).
				Label("Clipper API token").Width(300)
			if ui.Button(c, "Regenerate").Tooltip("Replace the token with a fresh one").Clicked() {
				if token, err := randomToken(); err == nil {
					a.settings.APIToken = token
				} else {
					a.settingsErr = err.Error()
				}
			}
		})
		ui.Text(c, "Paste this into the Joplin Web Clipper. Saving signs out any old token.").
			FontSize(11).TextColor(t.TextMuted)

		if a.settingsErr != "" {
			ui.Text(c, a.settingsErr).FontSize(12).TextColor(t.Danger)
		}
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "Cancel").Clicked() {
				a.settingsOpen = false
			}
			if ui.PrimaryButton(c, "Save").Clicked() {
				a.submitSettings()
			}
		})
	})
}

func (a *nativeApp) statusText() string {
	switch {
	case a.saveErr != "":
		return "Could not save: " + a.saveErr
	case a.dirty:
		return "Saving…"
	case !a.savedAt.IsZero():
		return "Saved " + a.savedAt.Format("15:04:05")
	default:
		return ""
	}
}

func (a *nativeApp) view(c *ui.Context) {
	if a.invalidate == nil {
		a.invalidate = c.Services().Invalidate
		a.watchSync()
	}
	t := c.Theme()

	if c.Shortcut(ui.Cmd, ui.KeyN) {
		a.newNote()
	}
	if c.Shortcut(ui.Cmd, ui.KeyF) {
		a.focusSearch = true
	}
	if c.Shortcut(ui.Cmd, ui.KeyS) {
		a.flush()
	}
	if c.Shortcut(ui.Cmd, ui.KeyE) {
		a.toggleEdit()
	}
	if c.Shortcut(ui.Alt, ui.KeyLeft) {
		a.goHistory(-1)
	}
	if c.Shortcut(ui.Alt, ui.KeyRight) {
		a.goHistory(1)
	}
	a.tick()
	a.tickSync()

	a.notebookDialogs(c, t)
	a.tagDialogs(c, t)
	a.settingsDialog(c, t)

	if a.resolveConflict {
		if ui.AlertDialog(c, &a.resolveConflict, "Keep this version?",
			"Copies this conflict's title and body to the original note, then deletes the conflict.",
			"Keep this version", "Cancel") == 0 {
			a.keepConflictVersion()
		}
	}

	if a.confirmDelete {
		if ui.AlertDialog(c, &a.confirmDelete, "Delete note?",
			"The note is removed from this device and the deletion is queued for the next sync.",
			"Delete", "Cancel") == 0 {
			a.deleteNote()
		}
	}

	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		a.notebooksPane(c, t)
		a.notesPane(c, t)
		a.editorPane(c, t)
	})
}

func (a *nativeApp) notebooksPane(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Width(220).Shrink(0).Gap(2).Background(t.Surface).Children(func() {
		ui.Row(c).Padding(10, 14).Gap(2).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Notebooks").Bold().Grow(1)
			ui.Textf(c, "%d", len(a.notebooks)).FontSize(11).TextColor(t.TextMuted)
			if ui.Button(c, "+").Label("New notebook").Tooltip("New notebook").Clicked() {
				a.openNewNotebook()
			}
			if a.notebookByID(a.selectedNotebook) != nil {
				actions := ui.Button(c, "…").Label("Notebook actions").Tooltip("Rename or delete this notebook")
				if actions.Clicked() {
					a.notebookActions = !a.notebookActions
				}
				ui.Popover(c, actions, &a.notebookActions, func() { a.notebookMenu(c, t) })
			}
		})
		sidebar := ui.Sidebar(c, &a.selectedNotebook, func() {
			ui.SidebarItem(c.Key("all"), "all", nil, "All notes")
			ui.SidebarItem(c.Key("conflicts"), conflictsRail, nil, "Conflicts")
			var walk func(nodes []*models.Folder)
			walk = func(nodes []*models.Folder) {
				for _, nb := range nodes {
					label := strings.Repeat("  ", a.notebookDepth[nb.ID]) + nb.Title
					ui.SidebarItem(c.Key(nb.ID), nb.ID, nil, label).Children(func() {
						ui.Textf(c, "%d", nb.NoteCount).FontSize(10).TextColor(t.TextMuted)
					})
					walk(nb.Children)
				}
			}
			walk(a.notebookTree)
		}).Grow(1).Label("Notebooks")
		sidebar.OnChange(func() {
			a.selectedTag = ""
			a.selectedID = ""
			a.reloadNotes()
		})

		if len(a.tags) > 0 {
			ui.Divider(c)
			ui.Row(c).Padding(8, 14).Gap(2).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "Tags").Bold().FontSize(12).Grow(1)
				if ui.Button(c, "+").Label("New tag").Tooltip("New tag").Clicked() {
					a.openNewTag()
				}
				if a.selectedTag != "" {
					tagActions := ui.Button(c, "…").Label("Tag actions").Tooltip("Rename or delete this tag")
					if tagActions.Clicked() {
						a.tagActions = !a.tagActions
					}
					ui.Popover(c, tagActions, &a.tagActions, func() { a.tagActionsMenu(c, t) })
				}
				if a.selectedTag != "" {
					if ui.Button(c, "×").Label("Clear tag").Tooltip("Clear the tag filter").Clicked() {
						a.selectedTag = ""
						a.selectedID = ""
						a.reloadNotes()
					}
				}
			})
			height := 28*len(a.tags) + 8
			if height < 36 {
				height = 36
			}
			if height > 170 {
				height = 170
			}
			tagSidebar := ui.Sidebar(c, &a.selectedTag, func() {
				for _, tag := range a.tags {
					ui.SidebarItem(c.Key("tag:"+tag.ID), tag.ID, nil, tag.Title)
				}
			}).Height(float32(height)).Label("Tags")
			tagSidebar.OnChange(func() {
				a.selectedNotebook = ""
				a.selectedID = ""
				a.reloadNotes()
			})
		} else {
			ui.Divider(c)
			ui.Row(c).Padding(8, 14).Gap(2).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "Tags").Bold().FontSize(12).Grow(1)
				if ui.Button(c, "+").Label("New tag").Tooltip("New tag").Clicked() {
					a.openNewTag()
				}
			})
		}

		ui.Divider(c)
		ui.Column(c).Padding(10, 14).Gap(6).Children(func() {
			line, isErr := a.syncLine()
			text := ui.Text(c, line).FontSize(10).TextColor(t.TextMuted)
			if isErr {
				text.TextColor(t.Danger)
			}
			st := a.syncState()
			ui.Row(c).Gap(6).Children(func() {
				if ui.Button(c, "Sync now").Disabled(!st.Enabled || st.Running || st.Pending).Clicked() {
					a.syncNow()
				}
				if ui.Button(c, "Settings").Tooltip("Sync target, master password and clipper token").Clicked() {
					a.openSettings()
				}
			})
		})
	})
}

func (a *nativeApp) notesPane(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Width(330).Shrink(0).Children(func() {
		ui.Row(c).Padding(10, 14).Gap(4).AlignItems(ui.Center).Children(func() {
			if ui.Button(c, "<").Label("Back").Disabled(!a.canBack()).Tooltip("Back (Alt+Left)").Clicked() {
				a.goHistory(-1)
			}
			if ui.Button(c, ">").Label("Forward").Disabled(!a.canForward()).Tooltip("Forward (Alt+Right)").Clicked() {
				a.goHistory(1)
			}
			title := "All notes"
			switch {
			case a.selectedNotebook == conflictsRail:
				title = "Conflicts"
			case a.selectedTag != "":
				if tag := a.tagByID(a.selectedTag); tag != nil {
					title = "#" + tag.Title
				}
			case a.selectedNotebook != "":
				if nb := a.notebookByID(a.selectedNotebook); nb != nil {
					title = nb.Title
				}
			}
			ui.Text(c, title).Bold().Grow(1)
			ui.Textf(c, "%d", len(a.notes)).FontSize(11).TextColor(t.TextMuted)
			if ui.Button(c, "+").Tooltip("New note (Cmd/Ctrl+N)").Clicked() {
				a.newNote()
			}
		})
		ui.Row(c).Padding(0, 14, 8, 14).Gap(6).AlignItems(ui.Center).Children(func() {
			search := ui.TextInput(c.Key("search"), &a.query).
				Placeholder("Search notes").
				Label("Search notes").
				Grow(1)
			if a.focusSearch {
				search.AutoFocus()
				a.focusSearch = false
			}
			if search.Changed() {
				a.reloadNotes()
			}
			if a.query != "" {
				if ui.Button(c, "×").Tooltip("Clear search").Clicked() {
					a.query = ""
					a.reloadNotes()
				}
			}
		})
		ui.Divider(c)

		a.list.Selected = &a.selectedNote
		a.list.Key = func(i int) any { return a.notes[i].ID }
		a.list.Label = func(i int) string { return noteRowTitle(a.notes[i]) }
		list := ui.List(c, &a.list, len(a.notes), func(i int) {
			n := a.notes[i]
			ui.Column(c).Padding(8, 14).Gap(2).Children(func() {
				ui.Text(c, noteRowTitle(n)).FontSize(13)
				if n.Excerpt != "" {
					ui.Text(c, excerpt(n.Excerpt)).FontSize(11).TextColor(t.TextMuted)
				}
				ui.Text(c, time.UnixMilli(n.UpdatedTime).Format("02 Jan 15:04")).
					FontSize(10).TextColor(t.TextMuted)
			})
		}).Grow(1)
		if list.Changed() {
			a.loadNote()
		}
		if a.hasMore {
			ui.Row(c).Padding(6, 14).Children(func() {
				if ui.Button(c, "Load more").Clicked() {
					a.loadMore()
				}
			})
		}
	})
}

func (a *nativeApp) editorPane(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Grow(1).MinWidth(0).Children(func() {
		ui.Row(c).Padding(10, 20).Gap(8).AlignItems(ui.Center).Children(func() {
			if a.note == nil {
				ui.Text(c, "No note selected").Bold().FontSize(15).Grow(1)
				return
			}
			title := ui.TextInput(c.Key("title:"+a.note.ID), &a.title).
				Placeholder("Untitled").
				Label("Note title").
				FontSize(15).
				Grow(1)
			if title.Changed() {
				a.markDirty()
			}
			if ui.Checkbox(c, &a.todo, "Todo").Changed() {
				a.setTodo(a.todo)
			}
			if a.note.IsConflict != 0 {
				if ui.Button(c, "Resolve conflict").Tooltip("Keep this version and delete the conflict").Clicked() {
					a.resolveConflict = true
				}
			}
			tagButton := ui.Button(c, "Tags").Label("Note tags").Tooltip("Tags on this note")
			if tagButton.Clicked() {
				a.tagMenuOpen = !a.tagMenuOpen
			}
			ui.Popover(c, tagButton, &a.tagMenuOpen, func() { a.tagMenu(c, t) })
			move := ui.Button(c, "Move")
			if move.Clicked() {
				a.moveOpen = !a.moveOpen
			}
			ui.Popover(c, move, &a.moveOpen, func() { a.moveMenu(c, t) })
			if ui.Button(c, a.modeLabel()).Tooltip("Edit/Preview (Cmd/Ctrl+E)").Clicked() {
				a.toggleEdit()
			}
			if ui.Button(c, "Delete…").Tooltip("Delete note").Clicked() {
				a.confirmDelete = true
			}
		})
		ui.Divider(c)

		if a.note == nil {
			a.emptyEditor(c, t)
		} else if a.editing {
			editor := ui.TextArea(c.Key("body:"+a.note.ID), &a.body).
				Placeholder("Write in Markdown…").
				Label("Note body").
				Font("monospace").
				FillWidth().
				Grow(1)
			if a.focusBody {
				editor.AutoFocus()
				a.focusBody = false
			}
			if editor.Changed() {
				a.markDirty()
			}
		} else {
			ui.Scroll(c).Grow(1).Padding(10, 20).Children(func() {
				if a.body == "" {
					ui.Text(c, "This note is empty.").TextColor(t.TextMuted)
					return
				}
				markdown.View(c, a.body, &a.md)
			})
		}

		ui.Divider(c)
		ui.Row(c).Padding(6, 20).Gap(8).AlignItems(ui.Center).Children(func() {
			status := a.statusText()
			if status == "" && a.note != nil {
				status = a.titleOfCurrent()
			}
			text := ui.Text(c, status).FontSize(11).TextColor(t.TextMuted).Grow(1)
			if a.saveErr != "" {
				text.TextColor(t.Danger)
			}
		})
	})
}

// tagMenu toggles the tags of the open note. The menu stays open so several
// tags can be added in one go.
func (a *nativeApp) tagMenu(c *ui.Context, t *ui.Theme) {
	height := 30 + 26*len(a.tags)
	if height < 54 {
		height = 54
	}
	if height > 300 {
		height = 300
	}
	ui.Scroll(c).Width(220).Height(float32(height)).Children(func() {
		ui.Column(c).Width(220).Padding(6).Gap(2).Children(func() {
			ui.Text(c, "Tags on this note").FontSize(11).TextColor(t.TextMuted)
			if len(a.tags) == 0 {
				ui.Text(c, "No tags yet").TextColor(t.TextMuted)
				return
			}
			for _, tag := range a.tags {
				on := a.hasNoteTag(tag.ID)
				entry := ui.Row(c.Key("note-tag:"+tag.ID)).Label("Tag: "+tag.Title).
					Padding(6, 10).Radius(5).AlignItems(ui.Center)
				if on {
					entry.Background(t.Accent.Alpha(0.16))
				}
				if entry.Hovered() {
					entry.Background(t.Accent).TextColor(t.AccentText)
				}
				if entry.Clicked() {
					a.toggleTag(tag.ID)
				}
				entry.Children(func() {
					ui.Text(c, tag.Title).Grow(1)
					if on {
						ui.Text(c, "✓")
					}
				})
			}
		})
	})
}

// notebookMenu is the Rename/Delete menu for the selected notebook.
func (a *nativeApp) notebookMenu(c *ui.Context, t *ui.Theme) {
	items := []struct {
		label string
		run   func()
	}{
		{"Rename notebook…", a.openRenameNotebook},
		{"Delete notebook…", func() {
			a.notebookActions = false
			a.confirmDeleteNotebook = true
		}},
	}
	ui.Column(c).Width(190).Padding(4).Gap(2).Children(func() {
		for _, item := range items {
			entry := ui.Row(c.Key("notebook:"+item.label)).Padding(6, 10).Radius(5)
			if entry.Hovered() {
				entry.Background(t.Accent).TextColor(t.AccentText)
			}
			if entry.Clicked() {
				a.notebookActions = false
				item.run()
			}
			entry.Children(func() { ui.Text(c, item.label) })
		}
	})
}

// moveMenu lists the notebooks the open note can move to.
func (a *nativeApp) moveMenu(c *ui.Context, t *ui.Theme) {
	// Keep a long notebook list a scrollable menu instead of a popover taller
	// than the window.
	height := 28 + 30*len(a.notebooks)
	if height > 320 {
		height = 320
	}
	ui.Scroll(c).Width(240).Height(float32(height)).Children(func() {
		ui.Column(c).Width(240).Padding(6).Gap(2).Children(func() {
			ui.Text(c, "Move to notebook").FontSize(11).TextColor(t.TextMuted)
			if len(a.notebooks) == 0 {
				ui.Text(c, "No notebooks").TextColor(t.TextMuted)
				return
			}
			for _, nb := range a.notebooks {
				current := a.note != nil && a.note.ParentID == nb.ID
				entry := ui.Row(c.Key("move:"+nb.ID)).Padding(6, 10).Radius(5).
					Gap(6).AlignItems(ui.Center)
				if entry.Hovered() {
					entry.Background(t.Accent).TextColor(t.AccentText)
				}
				if entry.Clicked() {
					a.moveOpen = false
					a.moveTo(nb.ID)
				}
				entry.Children(func() {
					ui.Text(c, "Move to "+strings.Repeat("  ", a.notebookDepth[nb.ID])+nb.Title).Grow(1)
					if current {
						ui.Text(c, "✓")
					}
				})
			}
		})
	})
}

func (a *nativeApp) emptyEditor(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Grow(1).AlignItems(ui.Center).Justify(ui.Center).Gap(8).Children(func() {
		ui.Text(c, "No note selected").FontSize(15).TextColor(t.TextMuted)
		if ui.PrimaryButton(c, "New note").Clicked() {
			a.newNote()
		}
	})
}

func (a *nativeApp) modeLabel() string {
	if a.editing {
		return "Preview"
	}
	return "Edit"
}

func noteRowTitle(n *models.NoteSummary) string {
	title := n.Title
	if strings.TrimSpace(title) == "" {
		title = "Untitled"
	}
	switch {
	case n.IsTodo != 0 && n.TodoCompleted != 0:
		return "☑ " + title
	case n.IsTodo != 0:
		return "☐ " + title
	}
	return title
}

// excerpt is a list-row summary: whitespace collapsed and cut short, so a
// long note cannot blow up the row layout.
func excerpt(body string) string {
	return clip(body, 120)
}

// clip collapses whitespace and cuts s to n runes.
func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// openNativeWindow opens the native workspace. It returns the window so the
// caller keeps the same lifecycle handling as the webview shell.
func openNativeWindow(be *backend) *mygo.Window {
	app := newNativeApp(be.db)
	app.be = be
	app.loadSettings = be.Settings
	app.applySettings = be.ApplySettings
	win := mygo.NewWindow(mygo.WindowOptions{
		Title:     "Joplin Lite (native)",
		Width:     1280,
		Height:    800,
		MinWidth:  900,
		MinHeight: 600,
		StateKey:  "native-main",
		Content:   ui.View(app.view),
	})
	win.OnClosed(func() {
		app.flush()
		if app.saveTimer != nil {
			app.saveTimer.Stop()
		}
	})
	if *smoke {
		// Native content has no page to evaluate, so the smoke check captures
		// the rendered window instead and quits.
		go func() {
			time.Sleep(*smokeIn)
			if png, err := win.CapturePage(); err == nil {
				_ = os.WriteFile("tmp/native-smoke.png", png, 0o644)
			}
			mygo.App.Quit()
		}()
	}
	return win
}
