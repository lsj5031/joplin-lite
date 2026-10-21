package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"image"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"golang.org/x/image/draw"

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
	db  *store.DB

	dark      bool
	savedDark bool

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
	// selection is the multi-row choice, keyed by note ID. Selected stays the
	// row the editor follows; this is what bulk actions act on.
	selection         ui.Selection[string]
	bulkMoveOpen      bool
	confirmBulkDelete bool

	query    string
	sort     string
	sortOpen bool

	savedSearches []savedSearch
	savedMenuOpen bool
	savedOpen     bool
	savedName     string
	savedErr      string
	savedDelete   string

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
	moveFilter    string
	conflictCount int

	// settings is the dialog's draft; settingsPick indexes the target segment
	// (the labels and targets come from settingsTargets).
	settingsOpen   bool
	settings       desktopSettings
	settingsPick   int
	settingsErr    string
	settingsReveal bool
	loadSettings   func() desktopSettings
	applySettings  func(desktopSettings) error

	tagMenuOpen             bool
	tagFilter               string
	tagActions              bool
	tagOpen                 bool
	tagRename               bool
	tagAttachNote           bool
	tagTitle                string
	tagErr                  string
	confirmDeleteTag        bool
	resolveConflict         bool
	conflictOriginal        *models.Note
	pickAttachmentPaths     func() ([]string, error)
	pendingInsert           string
	pendingCaret            *int
	notebookActions         bool
	notebookOpen            bool
	notebookRename          bool
	notebookTitle           string
	notebookParent          string
	notebookCandidateParent string
	notebookErr             string
	confirmDeleteNotebook   bool
	notebookBlocked         bool

	// history is the visited note IDs, ending at histPos. Moving between
	// notes records a step; walking history itself does not.
	history   []string
	histPos   int
	restoring bool

	// be is the in-process backend; nil in headless tests. Polling stays alive
	// while configured, including between automatic passes.
	be           *backend
	syncWatch    bool
	syncPoll     *time.Timer
	syncRevision uint64

	// narrow is whether the window is below DESIGN.md's breakpoint, where the
	// rail becomes a slide-over and the list and editor swap as single views.
	// narrowEditor is which of the two shows; railOpen is the slide-over.
	narrow       bool
	narrowEditor bool
	railOpen     bool
	railW        float32
	listW        float32

	// Inline double-click / context-menu rename state for sidebar rows.
	inlineRenameKind  string
	inlineRenameID    string
	inlineRenameTitle string

	// Pagination feedback when loading additional pages in the note list.
	loadedFrom         int
	lastLoadedCount    int
	loadMoreRun        uint64
	hasPendingScroll   bool
	pendingScrollRow   int
	pendingScrollAlign ui.Align
	inlineRenameFocus  bool

	// Spotlight-style command & quick-open palette state.
	paletteOpen  bool
	paletteQuery string
	paletteIndex int
	paletteFocus bool

	// surfaces carries the entrance state of the menus and dialogs, so a panel
	// animates in once per open and never at rest.
	surfaces surfaceMotions

	// target is the sync target the status strip's line runs to, set from the
	// settings once they are known.
	target    int
	targetSet bool

	md *markdown.Options
}

// targetLabel names the sync target for the status strip: the backend's own
// configuration while one is running (a target change rebuilds the backend),
// else the settings the dialog loaded, else nothing.
func (a *nativeApp) targetLabel() string {
	if a.be != nil && a.be.cfg != nil {
		target := a.be.cfg.SyncTarget
		if a.be.cfg.NoSync {
			target = targetOff
		}
		return targetLabel(target)
	}
	if a.targetSet {
		return targetLabel(a.target)
	}
	return targetLabel(targetOff)
}

func newNativeApp(db *store.DB) *nativeApp {
	dark := loadDarkSetting(db)
	app := &nativeApp{
		svc:          service.New(db),
		db:           db,
		dark:         dark,
		savedDark:    dark,
		selectedNote: -1,
		railW:        railWidth,
		listW:        listWidth,
	}
	// Screenshot attachments can decode to hundreds of MB; load one at a time.
	var imageMu sync.Mutex
	app.md = &markdown.Options{LoadImage: func(ref string) (ui.ImageSource, error) {
		imageMu.Lock()
		defer imageMu.Unlock()
		id, ok := strings.CutPrefix(ref, ":/")
		if !ok || len(id) != 32 {
			return nil, fmt.Errorf("not a Joplin resource reference")
		}
		if _, err := hex.DecodeString(id); err != nil {
			return nil, err
		}
		resource, err := db.GetResource(id)
		if err != nil {
			return nil, err
		}
		if resource == nil || resource.EncryptionApplied != 0 || resource.EncryptionBlobEncrypted != 0 {
			return nil, fmt.Errorf("resource is missing or encrypted")
		}
		data, err := os.ReadFile(db.GetResourceFile(id))
		if err != nil {
			return nil, err
		}
		if resource.Mime == "image/svg+xml" {
			return ui.ParseSVG(data)
		}
		config, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		// The page is 700 DIPs wide. Keep 2x detail, but fit every supported
		// Windows GPU's 8192px texture limit, including very tall screenshots.
		scale := min(1.0, 1400.0/float64(config.Width), 8192.0/float64(config.Height))
		if scale == 1 {
			return ui.DecodeBitmap(data)
		}
		decoded, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		preview := image.NewRGBA(image.Rect(0, 0, max(1, int(float64(config.Width)*scale)), max(1, int(float64(config.Height)*scale))))
		draw.ApproxBiLinear.Scale(preview, preview.Bounds(), decoded, decoded.Bounds(), draw.Src, nil)
		return ui.NewBitmap(preview), nil
	}}
	app.reloadNotebooks()
	app.reloadTags()
	app.loadSavedSearches()
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
	a.reloadConflictCount()
}

func (a *nativeApp) reloadConflictCount() {
	if page, err := a.svc.ListNoteSummaries(service.NoteFilter{ConflictOnly: true, Page: service.Page{Limit: nativePageSize}}); err == nil {
		a.conflictCount = len(page.Notes)
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
	a.tagMenuOpen = false
	a.tagActions = false
	a.tagRename = false
	a.tagAttachNote = false
	a.tagTitle = ""
	a.tagErr = ""
	a.tagOpen = true
}

func (a *nativeApp) openNewTagForNote() {
	a.openNewTag()
	a.tagAttachNote = a.note != nil
}

func (a *nativeApp) openRenameTag() {
	tag := a.tagByID(a.selectedTag)
	if tag == nil {
		return
	}
	a.tagActions = false
	a.tagRename = true
	a.tagAttachNote = false
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
		tag := &models.Tag{Title: title}
		if err := a.svc.CreateTag(tag); err != nil {
			a.tagErr = err.Error()
			return
		}
		if a.tagAttachNote && a.note != nil {
			a.flush()
			if err := a.svc.AddTag(a.note.ID, tag.ID); err != nil {
				a.saveErr = err.Error()
			}
		}
	}
	a.tagOpen = false
	a.tagAttachNote = false
	a.tagErr = ""
	a.reloadTags()
	a.reloadNoteTags()
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
	a.reloadNoteTags()
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
				if primaryAction(c, action).Clicked() {
					a.submitTag()
				}
			})
		})
	}
	if a.confirmDeleteTag {
		if confirmDialog(c, dangerTheme, &a.confirmDeleteTag, "Delete tag?",
			"This removes the tag from every note on this device and queues the deletion for sync.",
			"Delete tag") == 1 {
			a.deleteTag()
		}
	}
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
		Sort:   a.sort,
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
	a.loadedFrom = 0
	a.lastLoadedCount = 0
	a.hasPendingScroll = false
	if a.selectedNotebook == conflictsRail && a.query == "" {
		a.conflictCount = len(a.notes)
	}
	a.pruneSelection()

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
	if idx == 0 {
		a.list.ScrollTo(0, ui.Start)
	} else if idx > 0 {
		a.list.ScrollIntoView(idx)
	}
	a.loadNote()
}

// pruneSelection drops chosen keys the current rows no longer show, so the
// bulk bar never counts notes that a filter or reload hid.
func (a *nativeApp) pruneSelection() {
	if a.selection.Len() == 0 {
		return
	}
	visible := make(map[string]bool, len(a.notes))
	for _, n := range a.notes {
		visible[n.ID] = true
	}
	var stale []string
	for id := range a.selection.All() {
		if !visible[id] {
			stale = append(stale, id)
		}
	}
	for _, id := range stale {
		a.selection.Remove(id)
	}
}

// loadMore appends the next keyset page, scrolls to the first newly loaded
// note, selects it, and triggers entrance feedback for the new batch.
func (a *nativeApp) loadMore() {
	if !a.hasMore || a.cursor == "" {
		return
	}
	page, err := a.svc.ListNoteSummaries(a.noteFilter(a.cursor))
	if err != nil {
		a.saveErr = err.Error()
		return
	}
	firstNew := len(a.notes)
	a.notes = append(a.notes, page.Notes...)
	a.hasMore, a.cursor = page.HasMore, page.NextCursor
	if len(page.Notes) > 0 {
		a.loadedFrom = firstNew
		a.lastLoadedCount = len(page.Notes)
		a.loadMoreRun++
		a.selectedNote = firstNew
		a.selection.Clear()
		a.pendingScrollRow = firstNew
		a.pendingScrollAlign = ui.Start
		a.hasPendingScroll = true
		a.loadNote()
	}
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
	// Markdown caches decoded images; release the previous note's cache.
	a.md = &markdown.Options{LoadImage: a.md.LoadImage}
	a.note = note
	a.selectedID = id
	a.title, a.body = note.Title, note.Body
	a.editing = false
	a.focusBody = false
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

// rowOf returns the row showing id, or -1 when the current page of rows does
// not include it.
func (a *nativeApp) rowOf(id string) int {
	for i := range a.notes {
		if a.notes[i].ID == id {
			return i
		}
	}
	return -1
}

// noteInRows reports whether id is in the current page of the list.
func (a *nativeApp) noteInRows(id string) bool { return a.rowOf(id) >= 0 }

// historyShortcuts walks the note history with the keyboard. MyGO keeps Alt
// and an arrow for a focused editor on macOS, where that is word-by-word
// caret movement in every native text field, so there the reported binding is
// the browser convention Cmd+[ / Cmd+]; Alt+Left/Right still work wherever no
// text input has the keyboard.
func (a *nativeApp) historyShortcuts(c *ui.Context) {
	if runtime.GOOS == "darwin" {
		if c.Shortcut(ui.Cmd, ui.KeyBracketLeft) {
			a.goHistory(-1)
		}
		if c.Shortcut(ui.Cmd, ui.KeyBracketRight) {
			a.goHistory(1)
		}
	}
	if c.Shortcut(ui.Alt, ui.KeyLeft) {
		a.goHistory(-1)
	}
	if c.Shortcut(ui.Alt, ui.KeyRight) {
		a.goHistory(1)
	}
}

// historyHint is the shortcut Back and Forward advertise in their tooltips:
// the key that works from the note editor, which owns Alt and an arrow on
// macOS.
func historyHint(forward bool) string {
	if runtime.GOOS == "darwin" {
		if forward {
			return "Forward (Cmd+])"
		}
		return "Back (Cmd+[)"
	}
	if forward {
		return "Forward (Alt+Right)"
	}
	return "Back (Alt+Left)"
}

// goHistory moves delta steps through the visited notes. When the target is
// hidden by the current filters, it falls back to All notes or Conflicts so
// the note can be shown again with a matching selected row.
func (a *nativeApp) goHistory(delta int) {
	pos := a.histPos + delta
	if pos < 0 || pos >= len(a.history) {
		return
	}
	a.flush()
	id := a.history[pos]
	if !a.noteInRows(id) {
		note, err := a.svc.GetNote(id)
		if err != nil {
			a.saveErr = err.Error()
			return
		}
		a.query = ""
		a.selectedNotebook = ""
		if note != nil && note.IsConflict != 0 {
			a.selectedNotebook = conflictsRail
		}
		a.selectedTag = ""
		a.restoring = true
		a.reloadNotes()
		a.restoring = false
	}
	a.histPos = pos
	if a.note == nil || a.note.ID != id {
		a.showNote(id)
	}
}

// markDirty records an edit and arms the debounce timer. The timer only asks
// for a frame: the write stays on the UI thread, as the store expects.
func (a *nativeApp) markDirty() {
	a.dirty = true
	if a.note != nil {
		for _, row := range a.notes {
			if row.ID == a.note.ID {
				row.Title = a.title
				row.Excerpt = excerpt(a.body)
				break
			}
		}
	}
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
	a.reloadNotebooks()
	a.reloadNotes()
	a.editing = true
	a.focusBody = true
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
	a.reloadNotebooks()
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
	a.notebookCandidateParent = a.notebookParent
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
	a.notebookCandidateParent = ""
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
	if a.confirmDeleteNotebook && a.notebookHasChildren(a.selectedNotebook) {
		a.confirmDeleteNotebook = false
		a.notebookBlocked = true
	}
	if a.notebookOpen {
		ui.Modal(c, &a.notebookOpen, func() {
			title, action := "New notebook", "Create"
			if a.notebookRename {
				title, action = "Rename notebook", "Save"
			}
			ui.Text(c, title).FontSize(17).Bold()
			parentChanged := false
			if !a.notebookRename {
				if nb := a.notebookByID(a.notebookCandidateParent); nb != nil {
					parentIdx := 0
					if a.notebookParent == "" {
						parentIdx = 1
					}
					seg := ui.Segmented(c, &parentIdx, "Inside “"+nb.Title+"”", "Top level").Label("Notebook location")
					if seg.Changed() {
						parentChanged = true
						if parentIdx == 0 {
							a.notebookParent = a.notebookCandidateParent
						} else {
							a.notebookParent = ""
						}
					}
				} else {
					ui.Text(c, "At top level").FontSize(12).TextColor(t.TextMuted)
				}
			}
			input := ui.TextInput(c.Key("notebook-name"), &a.notebookTitle).
				Placeholder("Notebook name").
				Label("Notebook name").
				Width(320).
				AutoFocus()
			if parentChanged {
				input.Focus()
			}
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
				if primaryAction(c, action).Clicked() {
					a.submitNotebook()
				}
			})
		})
	}
	if a.notebookBlocked {
		ui.AlertDialog(c, &a.notebookBlocked, "Notebook has child notebooks",
			"Delete its child notebooks first. Their notes will also be deleted; move any notes you want to keep to another notebook before deleting.", "OK")
	}
	if a.confirmDeleteNotebook {
		name, count := "this notebook", 0
		if nb := a.notebookByID(a.selectedNotebook); nb != nil {
			name, count = "“"+nb.Title+"”", nb.NoteCount
		}
		if confirmDialog(c, dangerTheme, &a.confirmDeleteNotebook, "Delete "+name+"?",
			fmt.Sprintf("This deletes the notebook and %d note(s) on this device, and queues the deletions for sync.", count),
			"Delete notebook") == 1 {
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

// openResolveConflict snapshots the original note (if still present) and opens
// the conflict resolution modal.
func (a *nativeApp) openResolveConflict() {
	a.conflictOriginal = nil
	if a.note != nil && a.note.ConflictOriginalID != "" {
		if orig, err := a.svc.GetNote(a.note.ConflictOriginalID); err == nil {
			a.conflictOriginal = orig
		}
	}
	a.resolveConflict = true
}

// conflictDialog shows the conflict copy alongside the original note (when
// present) so the user can compare both versions before choosing one.
func (a *nativeApp) conflictDialog(c *ui.Context, t *ui.Theme) {
	if !a.resolveConflict {
		return
	}
	ui.Modal(c, &a.resolveConflict, func() {
		ui.Text(c, "Keep this version?").FontSize(sizeBody).Bold()
		ui.Text(c, "Copies this version to the original, then removes the conflict. If the original is missing, keeps this note as a regular note. Errors leave the conflict intact.").
			MaxWidth(520).TextColor(inkSoft)

		if orig := a.conflictOriginal; orig != nil {
			ui.Row(c).Width(520).Gap(10).AlignItems(ui.Stretch).Children(func() {
				conflictCard := func(heading string, headingColor ui.Color, titleText, bodyText, key string) {
					ui.Column(c).Width(255).Shrink(0).Padding(10, 12).Gap(6).
						Background(paperSunken).Border(1, hairline).Radius(radiusControl).Children(func() {
						ui.Text(c, heading).FontSize(sizeSmall).Bold().TextColor(headingColor)
						name := strings.TrimSpace(titleText)
						if name == "" {
							name = "Untitled"
						}
						ui.Text(c, name).FontSize(sizeUI).Bold().SingleLine()
						preview := bodyText
						if preview == "" {
							preview = "(Empty note)"
						}
						ui.Scroll(c.Key(key)).MaxHeight(130).Children(func() {
							ui.Text(c, preview).Font(fontMono).FontSize(sizeMonoSm).
								LineHeight(1.45).TextColor(inkSoft).MaxWidth(225)
						})
					})
				}
				conflictCard("This conflict copy", amberInk, a.title, a.body, "conflict-copy-preview")
				conflictCard("Original note", inkSoft, orig.Title, orig.Body, "conflict-orig-preview")
			})
			ui.Text(c, conflictDiffSummary(a.body, orig.Body)).
				Font(fontMono).FontSize(sizeMonoSm).TextColor(inkSoft)
		} else {
			ui.Column(c).Width(420).Padding(10, 12).Gap(4).
				Background(paperSunken).Border(1, hairline).Radius(radiusControl).Children(func() {
				ui.Text(c, "Original note not on this device").FontSize(sizeSmall).Bold().TextColor(amberInk)
				ui.Text(c, "Keeping this version turns the conflict copy into a regular note in place.").
					FontSize(sizeSmall).TextColor(inkSoft).MaxWidth(390)
			})
		}

		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "Cancel").AutoFocus().Clicked() {
				a.resolveConflict = false
			}
			if a.conflictOriginal != nil {
				if ui.Button(c, "Keep original").Tooltip("Discard this conflict copy and keep the original note").Clicked() {
					a.resolveConflict = false
					a.keepOriginalVersion()
				}
			}
			if underTheme(c, actionTheme, func() ui.Element { return ui.PrimaryButton(c, "Keep this version") }).Clicked() {
				a.resolveConflict = false
				a.keepConflictVersion()
			}
		})
	})
}

func conflictDiffSummary(conflictBody, origBody string) string {
	cw := len(strings.Fields(conflictBody))
	ow := len(strings.Fields(origBody))
	cl := len(strings.Split(strings.TrimSpace(conflictBody), "\n"))
	if strings.TrimSpace(conflictBody) == "" {
		cl = 0
	}
	ol := len(strings.Split(strings.TrimSpace(origBody), "\n"))
	if strings.TrimSpace(origBody) == "" {
		ol = 0
	}
	return fmt.Sprintf("Conflict: %d words (%d lines)  ·  Original: %d words (%d lines)", cw, cl, ow, ol)
}

// keepOriginalVersion discards the conflict copy and keeps the original note.
func (a *nativeApp) keepOriginalVersion() {
	if a.note == nil || a.note.IsConflict == 0 {
		return
	}
	conflictID := a.note.ID
	origID := a.note.ConflictOriginalID
	if origID == "" {
		return
	}
	original, err := a.svc.GetNote(origID)
	if err != nil {
		a.saveErr = err.Error()
		return
	}
	if original == nil {
		a.saveErr = "The original note is no longer on this device."
		return
	}
	if err := a.svc.DeleteNote(conflictID); err != nil {
		a.saveErr = err.Error()
		return
	}
	a.dirty = false
	a.saveErr = ""
	a.note = nil
	a.selectedID = ""
	a.title, a.body = "", ""
	a.noteTags = nil
	a.reloadNotebooks()
	a.reloadNotes()
}

// keepConflictVersion copies the conflict's title and body onto the original
// note and deletes the conflict. If the original is gone, the conflict becomes
// a normal note in place, preserving its content, tags and ID.
func (a *nativeApp) keepConflictVersion() {
	if a.note == nil || a.note.IsConflict == 0 {
		return
	}
	a.flush()
	if a.dirty {
		return
	}
	conflict := *a.note
	var original *models.Note
	if conflict.ConflictOriginalID != "" {
		var err error
		original, err = a.svc.GetNote(conflict.ConflictOriginalID)
		if err != nil {
			a.saveErr = err.Error()
			return
		}
	}
	if original == nil {
		conflict.IsConflict = 0
		conflict.ConflictOriginalID = ""
		if err := a.svc.UpdateNote(&conflict); err != nil {
			a.saveErr = err.Error()
			return
		}
	} else {
		original.Title, original.Body = conflict.Title, conflict.Body
		if err := a.svc.UpdateNote(original); err != nil {
			a.saveErr = err.Error()
			return
		}
		if err := a.svc.DeleteNote(conflict.ID); err != nil {
			a.saveErr = err.Error()
			return
		}
	}
	a.saveErr = ""
	a.note = nil
	a.selectedID = ""
	a.title, a.body = "", ""
	a.noteTags = nil
	a.reloadNotebooks()
	a.reloadNotes()
}

// attachFiles creates Joplin resources for each local file path and appends
// their Markdown references to the open note.
func (a *nativeApp) attachFiles(paths []string) {
	if a.note == nil || len(paths) == 0 {
		return
	}
	refs := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		_, ref, err := a.svc.AttachFile(p)
		if err != nil {
			a.saveErr = err.Error()
			return
		}
		refs = append(refs, ref)
	}
	a.insertAttachmentRefs(refs)
}

func (a *nativeApp) queuePasteFiles(paths []string) bool {
	if a.note == nil || len(paths) == 0 {
		return false
	}
	refs := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		_, ref, err := a.svc.AttachFile(p)
		if err != nil {
			a.saveErr = err.Error()
			return true
		}
		refs = append(refs, ref)
	}
	if len(refs) == 0 {
		return false
	}
	a.pendingInsert = strings.Join(refs, "\n")
	return true
}

func (a *nativeApp) queuePasteBytes(filename, mimeType string, data []byte) bool {
	if a.note == nil || len(data) == 0 {
		return false
	}
	_, ref, err := a.svc.AttachBytes(filename, mimeType, data)
	if err != nil {
		a.saveErr = err.Error()
		return true
	}
	a.pendingInsert = ref
	return true
}

func (a *nativeApp) insertAttachmentRefs(refs []string) {
	if a.note == nil || len(refs) == 0 {
		return
	}
	block := strings.Join(refs, "\n")
	switch {
	case strings.TrimSpace(a.body) == "":
		a.body = block
	case strings.HasSuffix(a.body, "\n\n"):
		a.body += block
	case strings.HasSuffix(a.body, "\n"):
		a.body += "\n" + block
	default:
		a.body += "\n\n" + block
	}
	a.markDirty()
	a.flush()
}

// promptAttachFiles opens a file picker (or invokes pickAttachmentPaths in
// headless tests) and attaches the chosen files to the open note.
func (a *nativeApp) promptAttachFiles() {
	if a.note == nil {
		return
	}
	if a.pickAttachmentPaths != nil {
		paths, err := a.pickAttachmentPaths()
		if err != nil {
			a.saveErr = err.Error()
			return
		}
		a.attachFiles(paths)
		return
	}
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Title:    "Attach files",
			Multiple: true,
		})
		mygo.RunOnMain(func() {
			if err != nil {
				a.saveErr = err.Error()
			} else if len(paths) > 0 {
				a.attachFiles(paths)
			}
			if a.invalidate != nil {
				a.invalidate()
			}
		})
	}()
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
	a.reloadNotebooks()
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

// syncNotice is what the footer says about sync. Line is the state in a few
// words; Hint explains it or says when the app tries again. Separating the two
// keeps the reason from being squeezed into one truncated error line.
type syncNotice struct {
	Line string
	Hint string
	Err  bool
}

// syncNotice describes the current sync state. A pass that another device is
// locking or a lost connection is reported as paused, not as a failure: both
// clear on their own, and the loop has already armed a retry.
func (a *nativeApp) syncNotice() syncNotice {
	return syncNoticeFor(a.syncState())
}

// syncNoticeFor turns a snapshot into the footer's words.
func syncNoticeFor(st syncSnapshot) syncNotice {
	switch {
	case !st.Enabled:
		return syncNotice{Line: "Local only", Hint: "Notes stay on this computer."}
	case st.Running || st.Pending:
		line := "Sync queued…"
		if st.Phase != "" {
			line = st.Phase + "…"
		}
		hint := "Not synced yet"
		if !st.Last.IsZero() {
			hint = "last synced " + st.Last.Format("15:04")
		}
		return syncNotice{Line: line, Hint: hint}
	case st.Err == "":
		if st.Last.IsZero() {
			return syncNotice{Line: "Not synced yet"}
		}
		return syncNotice{Line: "Synced " + st.Last.Format("15:04")}
	}

	var meta []string
	if !st.Last.IsZero() {
		meta = append(meta, "last synced "+st.Last.Format("15:04"))
	}
	if !st.Retry.IsZero() {
		meta = append(meta, "retrying at "+st.Retry.Format("15:04"))
	}
	tail := strings.Join(meta, " · ")
	switch st.Kind {
	case syncFailureLock:
		if tail == "" {
			tail = "Another device is syncing this vault."
		}
		return syncNotice{Line: "Sync paused · another device is syncing", Hint: tail}
	case syncFailureOffline:
		if tail == "" {
			tail = "Changes are kept here until it syncs."
		} else {
			tail = "Changes are kept here · " + tail
		}
		return syncNotice{Line: "Offline · sync paused", Hint: tail}
	case syncFailureTarget:
		return syncNotice{Line: "Sync stopped · target needs a newer Joplin", Hint: clip(st.Err, 80), Err: true}
	}
	hint := clip(st.Err, 80)
	if tail != "" {
		hint = strings.TrimSpace(hint + " · " + tail)
	}
	return syncNotice{Line: "Sync failed", Hint: hint, Err: true}
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

// tickSync observes even passes that start and finish between two frames.
func (a *nativeApp) tickSync() {
	if a.be != nil {
		a.be.setNotify(a.pollSync)
	}
	st := a.syncState()
	finished := st.Revision != a.syncRevision
	a.syncRevision = st.Revision
	active := st.Running || st.Pending
	if active {
		a.scheduleSyncPoll()
	} else if a.syncPoll != nil {
		a.syncPoll.Stop()
	}
	if finished || (a.syncWatch && !active) {
		a.refreshAfterSync()
	}
	a.syncWatch = active
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
		a.settings = desktopSettings{Dark: a.dark}
	}
	a.savedDark = a.dark
	if a.settings.Dark {
		a.dark = true
	} else {
		a.settings.Dark = a.dark
	}
	a.settingsErr = ""
	a.settingsReveal = false
	a.settingsPick = 0
	a.target, a.targetSet = a.settings.Target, true
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
	a.dark = a.settings.Dark
	a.savedDark = a.dark
	_ = saveDarkSetting(a.db, a.dark)
	a.settingsOpen = false
	a.settingsErr = ""
	// Even when the new target is off, one poll refreshes rows after the
	// engine's last pass has stopped.
	a.syncWatch = true
	a.scheduleSyncPoll()
}

// settingsDialog is the native settings form: sync target and its fields, the
// E2EE master password, and the clipper token, organized in grouped cards.
func (a *nativeApp) settingsDialog(c *ui.Context, t *ui.Theme) {
	if !a.settingsOpen {
		return
	}
	ui.Modal(c, &a.settingsOpen, func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconSettings).Size(16, 16).TextColor(routeBlue)
			ui.Text(c, "Settings").FontSize(17).Bold()
		})
		ui.Text(c, "Sync and the web clipper, without editing a config file.").
			FontSize(12).TextColor(t.TextMuted)

		_, height := c.Size()
		ui.Scroll(c.Key("settings-fields")).Width(420).MaxHeight(max(120, height-210)).Children(func() {
			ui.Column(c).Gap(8).Children(func() {
				// Group 1: Appearance & Sync Target
				ui.Column(c).Width(416).Padding(8, 10).Gap(6).
					Background(paperSunken.Alpha(0.55)).Border(1, hairline).Radius(radiusControl).Children(func() {
					appearancePick := 0
					if a.settings.Dark {
						appearancePick = 1
					}
					if segmentedControl(c, "Appearance", &appearancePick, "Light", "Dark") {
						a.settings.Dark = appearancePick == 1
						a.dark = a.settings.Dark
					}

					washPick := 0
					washLabels := make([]string, len(washOptions))
					for i, opt := range washOptions {
						washLabels[i] = opt.label
						if opt.id == activeWash {
							washPick = i
						}
					}
					if segmentedControl(c, "Backdrop", &washPick, washLabels...) {
						activeWash = washOptions[washPick].id
					}

					labels, targets := a.settingsTargets()
					if segmentedControl(c, "Sync target", &a.settingsPick, labels...) {
						a.settings.Target = targets[a.settingsPick]
					}

					switch a.settings.Target {
					case targetFolder:
						ui.Text(c, "Sync folder").FontSize(12).Bold()
						ui.TextInput(c.Key("settings-folder"), &a.settings.LocalDir).
							Placeholder("Folder Joplin's File system target uses").
							Label("Sync folder path").Width(394)
					case targetServer:
						ui.Text(c, "Server URL").FontSize(12).Bold()
						ui.TextInput(c.Key("settings-url"), &a.settings.ServerURL).
							Placeholder("https://sync.example.com").
							Label("Joplin Server URL").Width(394)
						ui.Text(c, "Email").FontSize(12).Bold()
						ui.TextInput(c.Key("settings-user"), &a.settings.Username).
							Label("Server account email").Width(394)
						ui.Text(c, "Password").FontSize(12).Bold()
						password := ui.TextInput(c.Key("settings-pass"), &a.settings.Password).
							Label("Server account password").Width(394)
						if !a.settingsReveal {
							password.Password()
						}
					case 7, 8:
						ui.Text(c, targetLabel(a.settings.Target)+" is configured in the config file; this dialog keeps it.").
							FontSize(11).TextColor(t.TextMuted)
					case targetOff:
						ui.Text(c, "Notes stay on this device; nothing is sent anywhere.").
							FontSize(11).TextColor(t.TextMuted)
					}
				})

				// Group 2: End-to-End Encryption
				ui.Column(c).Width(416).Padding(8, 10).Gap(5).
					Background(paperSunken.Alpha(0.55)).Border(1, hairline).Radius(radiusControl).Children(func() {
					ui.Text(c, "Master password (E2EE)").FontSize(12).Bold()
					master := ui.TextInput(c.Key("settings-e2ee"), &a.settings.MasterPassword).
						Placeholder("Leave empty when E2EE is off").
						Label("E2EE master password").Width(394)
					if !a.settingsReveal {
						master.Password()
					}
					ui.Checkbox(c, &a.settingsReveal, "Show passwords")
					ui.Text(c, "Decrypts synced notes on this device. Stored locally in this vault and never sent anywhere.").
						FontSize(11).TextColor(t.TextMuted)
				})

				// Group 3: Web Clipper Token
				ui.Column(c).Width(416).Padding(8, 10).Gap(5).
					Background(paperSunken.Alpha(0.55)).Border(1, hairline).Radius(radiusControl).Children(func() {
					ui.Text(c, "Clipper token").FontSize(12).Bold()
					ui.Row(c).Width(394).Gap(6).AlignItems(ui.Center).Children(func() {
						ui.TextInput(c.Key("settings-token"), &a.settings.APIToken).
							Label("Clipper API token").Grow(1).MinWidth(0)
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
				})
			})
		})

		if a.settingsErr != "" {
			ui.Text(c, a.settingsErr).FontSize(12).TextColor(t.Danger)
		}
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "Cancel").Label("Cancel settings").Clicked() {
				a.dark = a.savedDark
				a.settings.Dark = a.savedDark
				a.settingsOpen = false
			}
			if primaryAction(c, "Save").Label("Save settings").Clicked() {
				a.submitSettings()
			}
		})
	})
	if !a.settingsOpen {
		a.dark = a.savedDark
		a.settings.Dark = a.savedDark
	}
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

// selectedIDs returns the chosen notes in list order. The choice may name
// rows that a reload has since dropped, so only visible rows are returned.
func (a *nativeApp) selectedIDs() []string {
	if a.selection.Len() == 0 {
		return nil
	}
	ids := make([]string, 0, a.selection.Len())
	for _, n := range a.notes {
		if a.selection.Has(n.ID) {
			ids = append(ids, n.ID)
		}
	}
	return ids
}

// moveSelected moves the chosen notes, saving pending edits first. The
// selection clears because those rows leave the current notebook view.
func (a *nativeApp) moveSelected(folderID string) {
	a.moveNotes(a.selectedIDs(), folderID)
}

// moveNotes moves the given notes to a notebook. Drop and the bulk menu share
// it, so a dragged note and a chosen one are handled the same way.
func (a *nativeApp) moveNotes(ids []string, folderID string) {
	if folderID == "" || len(ids) == 0 {
		return
	}
	a.flush()
	for _, id := range ids {
		n, err := a.svc.GetNote(id)
		if err != nil {
			a.saveErr = err.Error()
			return
		}
		if n == nil || n.ParentID == folderID {
			continue
		}
		n.ParentID = folderID
		if err := a.svc.UpdateNote(n); err != nil {
			a.saveErr = err.Error()
			return
		}
	}
	a.bulkMoveOpen = false
	a.selection.Clear()
	a.reloadNotebooks()
	a.reloadNotes()
}

// deleteSelected deletes every chosen note. Pending edits to a note that is
// going away are dropped instead of being written first.
func (a *nativeApp) deleteSelected() {
	ids := a.selectedIDs()
	if len(ids) == 0 {
		return
	}
	for _, id := range ids {
		if a.note != nil && a.note.ID == id {
			a.dirty = false
			a.note = nil
			a.selectedID = ""
			a.title, a.body = "", ""
			a.todo = false
			a.noteTags = nil
		}
		if err := a.svc.DeleteNote(id); err != nil {
			a.saveErr = err.Error()
			break
		}
	}
	a.selection.Clear()
	a.reloadNotebooks()
	a.reloadNotes()
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

// startInlineRename enters inline editing for a notebook or tag in the sidebar.
func (a *nativeApp) startInlineRename(kind, id, title string) {
	a.inlineRenameKind = kind
	a.inlineRenameID = id
	a.inlineRenameTitle = title
	a.inlineRenameFocus = true
}

func (a *nativeApp) cancelInlineRename() {
	a.inlineRenameKind = ""
	a.inlineRenameID = ""
	a.inlineRenameTitle = ""
	a.inlineRenameFocus = false
}

func (a *nativeApp) commitInlineRename() {
	kind, id := a.inlineRenameKind, a.inlineRenameID
	title := strings.TrimSpace(a.inlineRenameTitle)
	a.cancelInlineRename()
	if title == "" || id == "" {
		return
	}
	switch kind {
	case "notebook":
		nb := a.notebookByID(id)
		if nb == nil || nb.Title == title {
			return
		}
		updated := *nb
		updated.Title = title
		if err := a.svc.UpdateNotebook(&updated); err != nil {
			a.saveErr = err.Error()
			return
		}
		a.reloadNotebooks()
	case "tag":
		tag := a.tagByID(id)
		if tag == nil || tag.Title == title {
			return
		}
		updated := *tag
		updated.Title = title
		if err := a.svc.UpdateTag(&updated); err != nil {
			a.saveErr = err.Error()
			return
		}
		a.reloadTags()
		a.reloadNoteTags()
	}
}

// openPalette opens the Spotlight-style command and note switcher palette.
func (a *nativeApp) openPalette() {
	a.paletteQuery = ""
	a.paletteIndex = 0
	a.paletteFocus = true
	a.paletteOpen = true
}

// appMenu builds the native OS menu bar for Joplin Lite.
func (a *nativeApp) appMenu() *mygo.Menu {
	do := func(fn func()) func(*mygo.MenuItem, *mygo.Window) {
		return func(*mygo.MenuItem, *mygo.Window) {
			fn()
			if a.invalidate != nil {
				a.invalidate()
			}
		}
	}

	var items []*mygo.MenuItem
	if runtime.GOOS == "darwin" {
		items = append(items, &mygo.MenuItem{
			Label: "Joplin Lite",
			Submenu: []*mygo.MenuItem{
				{Role: mygo.RoleAbout},
				mygo.Separator(),
				{Label: "Settings…", Accelerator: "CmdOrCtrl+,", Click: do(a.openSettings)},
				mygo.Separator(),
				{Role: mygo.RoleServices},
				mygo.Separator(),
				{Role: mygo.RoleHide},
				{Role: mygo.RoleHideOthers},
				{Role: mygo.RoleUnhide},
				mygo.Separator(),
				{Role: mygo.RoleQuit},
			},
		})
	}

	fileItems := []*mygo.MenuItem{
		{Label: "New Note", Accelerator: "CmdOrCtrl+N", Click: do(a.newNote)},
		{Label: "New Notebook…", Accelerator: "CmdOrCtrl+Shift+N", Click: do(a.openNewNotebook)},
		{Label: "New Tag…", Accelerator: "CmdOrCtrl+Shift+T", Click: do(a.openNewTag)},
		mygo.Separator(),
		{Label: "Save Note", Accelerator: "CmdOrCtrl+S", Click: do(a.flush)},
		{Label: "Attach Files…", Click: do(a.promptAttachFiles)},
		{Label: "Sync Now", Accelerator: "CmdOrCtrl+R", Click: do(a.syncNow)},
		mygo.Separator(),
	}
	if runtime.GOOS != "darwin" {
		fileItems = append(fileItems,
			&mygo.MenuItem{Label: "Settings…", Accelerator: "CmdOrCtrl+,", Click: do(a.openSettings)},
			mygo.Separator(),
		)
	}
	fileItems = append(fileItems, &mygo.MenuItem{Role: mygo.RoleClose})

	items = append(items,
		&mygo.MenuItem{Label: "File", Submenu: fileItems},
		&mygo.MenuItem{
			Label: "Edit",
			Submenu: []*mygo.MenuItem{
				{Role: mygo.RoleUndo},
				{Role: mygo.RoleRedo},
				mygo.Separator(),
				{Role: mygo.RoleCut},
				{Role: mygo.RoleCopy},
				{Role: mygo.RolePaste},
				{Role: mygo.RoleSelectAll},
				mygo.Separator(),
				{Label: "Find in Notes…", Accelerator: "CmdOrCtrl+F", Click: do(func() {
					a.focusSearch = true
					a.focusBody = false
				})},
				{Label: "Command Palette…", Accelerator: "CmdOrCtrl+P", Click: do(a.openPalette)},
			},
		},
		&mygo.MenuItem{
			Label: "View",
			Submenu: []*mygo.MenuItem{
				{Label: "Toggle Edit / Preview", Accelerator: "CmdOrCtrl+E", Click: do(a.toggleEdit)},
				{Label: "Toggle Notebooks Sidebar", Accelerator: "CmdOrCtrl+\\", Click: do(func() {
					a.railOpen = !a.railOpen
				})},
				mygo.Separator(),
				{Label: "Back", Accelerator: "CmdOrCtrl+[", Click: do(func() { a.goHistory(-1) })},
				{Label: "Forward", Accelerator: "CmdOrCtrl+]", Click: do(func() { a.goHistory(1) })},
				mygo.Separator(),
				{Label: "Toggle Dark Appearance", Click: do(func() {
					a.dark = !a.dark
					a.savedDark = a.dark
					_ = saveDarkSetting(a.db, a.dark)
				})},
				mygo.Separator(),
				{Role: mygo.RoleToggleFullScreen},
			},
		},
		&mygo.MenuItem{Role: mygo.RoleWindowMenu},
	)
	return mygo.NewMenu(items)
}

// dockMenu builds the macOS Dock icon menu.
func (a *nativeApp) dockMenu() *mygo.Menu {
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: "New Note", Click: func(*mygo.MenuItem, *mygo.Window) {
			a.newNote()
			if a.invalidate != nil {
				a.invalidate()
			}
		}},
		{Label: "Sync Now", Click: func(*mygo.MenuItem, *mygo.Window) {
			a.syncNow()
			if a.invalidate != nil {
				a.invalidate()
			}
		}},
	})
}

// openNativeWindow opens the native workspace. It returns the window so the
// caller keeps the same lifecycle handling as the webview shell.
func openNativeWindow(be *backend) *mygo.Window {
	app := newNativeApp(be.db)
	app.be = be
	app.loadSettings = be.Settings
	app.applySettings = be.ApplySettings
	app.target, app.targetSet = be.Settings().Target, true
	mygo.App.SetMenu(app.appMenu())
	if runtime.GOOS == "darwin" {
		mygo.App.Dock.SetMenu(app.dockMenu())
	}
	view := app.view
	if windowsChrome {
		appearance := mygo.ThemeLight
		if app.dark {
			appearance = mygo.ThemeDark
		}
		mygo.Theme.SetSource(appearance)
		lastDark := app.dark
		view = func(c *ui.Context) {
			if app.dark != lastDark {
				lastDark = app.dark
				appearance := mygo.ThemeLight
				if app.dark {
					appearance = mygo.ThemeDark
				}
				// Applying appearance invalidates native content. Queue it
				// outside the frame rather than re-entering its renderer.
				go mygo.Theme.SetSource(appearance)
			}
			app.view(c)
		}
	}
	opts := mygo.WindowOptions{
		Title:     "Joplin Lite",
		Width:     1280,
		Height:    800,
		MinWidth:  900,
		MinHeight: 600,
		StateKey:  "native-main",
		Content:   ui.View(view),
	}
	if runtime.GOOS == "darwin" {
		opts.TitleBarStyle = mygo.TitleBarHiddenInset
		opts.TitleBarHeight = 44
		opts.TrafficLightPosition = &mygo.Point{X: 16, Y: 16}
		opts.Vibrancy = mygo.VibrancySidebar
	} else if windowsChrome {
		opts.TitleBarStyle = mygo.TitleBarHidden
		opts.TitleBarHeight = 48
		opts.Vibrancy = mygo.VibrancyMica
		opts.AutoHideMenuBar = true
	}
	win := mygo.NewWindow(opts)
	win.OnClosed(func() {
		if app.be != nil {
			app.be.setNotify(nil)
		}
		app.flush()
		if app.saveTimer != nil {
			app.saveTimer.Stop()
		}
		if app.syncPoll != nil {
			app.syncPoll.Stop()
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
