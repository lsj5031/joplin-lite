package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"image"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/egoist/mygo/ui/markdown/parser"
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
	savedWash string

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

	// JEX export/import. The work runs off the UI thread and reports through
	// backupBusy / backupNotice / backupErr; the pick* seams let tests avoid
	// native panels.
	backupBusy    string
	backupNotice  string
	backupErr     string
	importPath    string
	importPlan    *service.ImportResult
	confirmImport bool
	pickExportPath func() (string, error)
	pickImportPath func() (string, error)

	todo          bool
	confirmDelete bool
	noteMenuOpen  bool
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
	pendingOpen             string
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
	loadedFrom        int
	lastLoadedCount   int
	loadMoreRun       uint64
	hasPendingScroll  bool
	pendingScrollRow  int
	inlineRenameFocus bool

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

	md                  *markdown.Options
	mdTheme             *markdown.Theme
	mdThemeDark         bool
	mdThemeWash         string
	mdThemeInit         bool
	previewDoc          *parser.Document
	previewRaw          string
	previewBody         string
	previewChunks       []string
	previewChunkStarts  []int
	previewPreserveBody string
	previewList         ui.ListState
	previewReady        bool
	wordCountBody       string
	wordCountVal        int
	wordCountReady      bool
	metrics             *uiMetrics
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
	bootStart := time.Now()
	dark := loadDarkSetting(db)
	activeWash = loadBackdropSetting(db)
	app := &nativeApp{
		svc:          service.New(db),
		db:           db,
		dark:         dark,
		savedDark:    dark,
		selectedNote: -1,
		railW:        railWidth,
		listW:        listWidth,
		metrics:      newUIMetrics(),
	}
	// Screenshot attachments can decode to hundreds of MB; load one at a time.
	var imageMu sync.Mutex
	app.md = &markdown.Options{LoadImage: func(ref string) (ui.ImageSource, error) {
		imageMu.Lock()
		defer imageMu.Unlock()
		start := time.Now()
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
		defer func() {
			app.metrics.observeAttrs(spanLoadImage, start,
				slog.String("resource_id", id),
				slog.Int("bytes", len(data)),
			)
		}()
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
	app.metrics.observeAttrs(spanBoot, bootStart,
		slog.Int("notebooks", len(app.notebooks)),
		slog.Int("tags", len(app.tags)),
		slog.Int("notes", len(app.notes)),
	)
	return app
}

func (a *nativeApp) reloadNotebooks() {
	start := time.Now()
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
	a.metrics.observeAttrs(spanReloadNotebooks, start,
		slog.Int("notebooks", len(a.notebooks)),
		slog.Int("conflicts", a.conflictCount),
	)
}

func (a *nativeApp) reloadConflictCount() {
	if page, err := a.svc.ListNoteSummaries(service.NoteFilter{ConflictOnly: true, Page: service.Page{Limit: nativePageSize}}); err == nil {
		a.conflictCount = len(page.Notes)
	}
}

func (a *nativeApp) reloadTags() {
	start := time.Now()
	page, err := a.svc.ListTags(service.Page{Limit: nativeNotebookLimit})
	if err != nil {
		return
	}
	a.tags = page.Tags
	if a.selectedTag != "" && a.tagByID(a.selectedTag) == nil {
		a.selectedTag = ""
	}
	a.metrics.observeAttrs(spanReloadTags, start, slog.Int("tags", len(a.tags)))
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

// reloadNotes refreshes the loaded pages for the current filter, keeping the
// selected note when it is still in the results.
func (a *nativeApp) reloadNotes() {
	start := time.Now()
	pages := 1
	page, err := a.svc.ListNoteSummaries(a.noteFilter(""))
	if err != nil {
		a.saveErr = err.Error()
		return
	}
	// Preserve the loaded window so a palette result on a later page does not
	// disappear on the next sync refresh.
	for page.HasMore && page.NextCursor != "" && len(page.Notes) < len(a.notes) {
		next, err := a.svc.ListNoteSummaries(a.noteFilter(page.NextCursor))
		if err != nil {
			a.saveErr = err.Error()
			return
		}
		pages++
		page.Notes = append(page.Notes, next.Notes...)
		page.HasMore, page.NextCursor = next.HasMore, next.NextCursor
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
	a.metrics.observeAttrs(spanReloadNotes, start,
		slog.Int("rows", len(a.notes)),
		slog.Int("pages", pages),
		slog.Int("query_len", len(a.query)),
	)
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
	start := time.Now()
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
		a.hasPendingScroll = true
		a.loadNote()
	}
	a.metrics.observeAttrs(spanLoadMore, start,
		slog.Int("loaded", len(page.Notes)),
		slog.Int("total_rows", len(a.notes)),
	)
}

// loadNote flushes pending edits before switching, then loads the selected
// row. Selecting the note already open is a no-op, so search reloads do not
// reset the editor.
func (a *nativeApp) loadNote() {
	if a.selectedNote < 0 || a.selectedNote >= len(a.notes) {
		a.flush()
		a.note = nil
		a.selectedID = ""
		a.noteMenuOpen = false
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
	start := time.Now()
	note, err := a.svc.GetNote(id)
	if err != nil || note == nil {
		return
	}
	// Markdown caches decoded images; release the previous note's cache.
	a.md = &markdown.Options{LoadImage: a.md.LoadImage, OnLink: a.md.OnLink}
	a.previewRaw = ""
	a.previewBody = ""
	a.previewChunks = nil
	a.previewChunkStarts = nil
	a.previewPreserveBody = ""
	a.previewReady = false
	a.wordCountBody = ""
	a.wordCountReady = false
	a.note = note
	a.selectedID = id
	a.noteMenuOpen = false
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
	a.metrics.observeAttrs(spanShowNote, start,
		slog.Int("body_bytes", len(a.body)),
		slog.Int("tags", len(a.noteTags)),
	)
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
	start := time.Now()
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
	a.metrics.observeAttrs(spanMarkDirty, start, slog.Int("body_bytes", len(a.body)))
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
	start := time.Now()
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
	a.metrics.observeAttrs(spanFlushSave, start, slog.Int("body_bytes", len(n.Body)))
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

// setTodoCompleted checks or unchecks a to-do from the note list or the
// header pill, saving pending edits to the open note first.
func (a *nativeApp) setTodoCompleted(id string, done bool) {
	if id == "" {
		return
	}
	a.flush()
	n, err := a.svc.GetNote(id)
	if err != nil {
		a.saveErr = err.Error()
		return
	}
	if n == nil || n.IsTodo == 0 {
		return
	}
	if done {
		n.TodoCompleted = 1
	} else {
		n.TodoCompleted = 0
	}
	if err := a.svc.UpdateNote(n); err != nil {
		a.saveErr = err.Error()
		return
	}
	if a.note != nil && a.note.ID == id {
		a.note = n
		a.todo = n.IsTodo != 0
	}
	for _, row := range a.notes {
		if row.ID == id {
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

// openLink handles a click on a link in the rendered note. A Joplin resource
// reference (:/id) is copied out under its original filename and handed to the
// system, because the vault stores extensions such as .pdf only in metadata;
// web and mail links keep their usual behavior.
func (a *nativeApp) openLink(link string) {
	if id, ok := strings.CutPrefix(link, ":/"); ok {
		if len(id) != 32 {
			return
		}
		if _, err := hex.DecodeString(id); err != nil {
			return
		}
		resource, err := a.db.GetResource(id)
		if err != nil {
			a.saveErr = err.Error()
			return
		}
		if resource == nil {
			a.saveErr = "That attachment is no longer in this vault."
			return
		}
		if resource.EncryptionApplied != 0 || resource.EncryptionBlobEncrypted != 0 {
			a.saveErr = "That attachment is encrypted and cannot be opened."
			return
		}
		data, err := os.ReadFile(a.db.GetResourceFile(id))
		if err != nil {
			a.saveErr = err.Error()
			return
		}
		name := filepath.Base(strings.TrimSpace(resource.Title))
		if name == "" || name == "." || name == string(filepath.Separator) {
			name = "attachment"
		}
		if filepath.Ext(name) == "" && resource.FileExtension != "" &&
			!strings.ContainsAny(resource.FileExtension, `/\:`) {
			name += "." + resource.FileExtension
		}
		cache, err := os.UserCacheDir()
		if err != nil {
			cache = os.TempDir()
		}
		dir := filepath.Join(cache, "joplin-lite", "attachments", id)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			a.saveErr = err.Error()
			return
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			a.saveErr = err.Error()
			return
		}
		slash := filepath.ToSlash(path)
		if !strings.HasPrefix(slash, "/") {
			slash = "/" + slash
		}
		a.pendingOpen = (&url.URL{Scheme: "file", Path: slash}).String()
		return
	}
	switch {
	case strings.HasPrefix(link, "http://"), strings.HasPrefix(link, "https://"), strings.HasPrefix(link, "mailto:"):
		a.pendingOpen = link
	}
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
	a.noteMenuOpen = false
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
	defer a.metrics.observe(spanRefreshAfterSync, time.Now())
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
	a.savedWash = validWash(activeWash)
	if a.settings.Backdrop != "" {
		// The stored choice wins over a preview left by an earlier dialog.
		a.savedWash = validWash(a.settings.Backdrop)
	}
	a.settings.Backdrop = a.savedWash
	activeWash = a.settings.Backdrop
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
	a.settings.Backdrop = validWash(a.settings.Backdrop)
	activeWash = a.settings.Backdrop
	a.savedWash = activeWash
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
		_, height := c.Size()
		ui.Scroll(c.Key("settings-fields")).Width(420).MaxHeight(max(120, height-210)).Children(func() {
			ui.Column(c).Gap(8).Children(func() {
				// Group 1: Appearance, with each track named above it.
				ui.Column(c).Width(416).Padding(8, 10).Gap(6).
					Background(paperSunken.Alpha(0.55)).Border(1, hairline).Radius(radiusControl).Children(func() {
					ui.Text(c, "Appearance").FontSize(12).Bold()
					label(c, "Theme")
					appearancePick := 0
					if a.settings.Dark {
						appearancePick = 1
					}
					if segmentedControl(c, "Theme", &appearancePick, "Light", "Dark") {
						a.settings.Dark = appearancePick == 1
						a.dark = a.settings.Dark
					}

					label(c, "Surface wash")
					washPick := 0
					washLabels := make([]string, len(washOptions))
					for i, opt := range washOptions {
						washLabels[i] = opt.label
						if opt.id == a.settings.Backdrop {
							washPick = i
						}
					}
					if segmentedControl(c, "Surface wash", &washPick, washLabels...) {
						a.settings.Backdrop = washOptions[washPick].id
						activeWash = a.settings.Backdrop
					}
				})

				// Group 2: Synchronization on its own, so its target is not read
				// as a third appearance choice.
				ui.Column(c).Width(416).Padding(8, 10).Gap(6).
					Background(paperSunken.Alpha(0.55)).Border(1, hairline).Radius(radiusControl).Children(func() {
					ui.Text(c, "Synchronization").FontSize(12).Bold()
					label(c, "Sync target")

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

				// Encryption only matters once items leave this device; a local-only
				// vault should not be asked for a master password.
				if a.settings.Target != targetOff {
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
				}

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
				activeWash = a.savedWash
				a.settings.Backdrop = a.savedWash
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
		activeWash = a.savedWash
		a.settings.Backdrop = a.savedWash
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
		if a.note != nil && a.note.ID == id {
			a.note = n
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
	title := noteTitle(n)
	switch {
	case n.IsTodo != 0 && n.TodoCompleted != 0:
		return "☑ " + title
	case n.IsTodo != 0:
		return "☐ " + title
	}
	return title
}

// noteTitle is a summary's title without the to-do glyph, which a list row
// draws as a real checkbox instead.
func noteTitle(n *models.NoteSummary) string {
	if strings.TrimSpace(n.Title) == "" {
		return "Untitled"
	}
	return n.Title
}

// excerpt is a list-row summary: whitespace collapsed and cut short, so a
// long note cannot blow up the row layout.
func excerpt(body string) string {
	return clip(body, 120)
}

// clip collapses whitespace and cuts s to n runes without scanning or
// allocating past the first n collapsed runes of a long note body.
func clip(s string, n int) string {
	if n <= 0 || s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(min(len(s), n*4+4))
	count := 0
	inSpace := true
	pendingSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !inSpace {
				pendingSpace = true
				inSpace = true
			}
			continue
		}
		if pendingSpace {
			if count >= n {
				b.WriteString("…")
				return b.String()
			}
			b.WriteByte(' ')
			count++
			pendingSpace = false
		}
		if count >= n {
			b.WriteString("…")
			return b.String()
		}
		b.WriteRune(r)
		count++
		inSpace = false
	}
	return b.String()
}

// countWords counts whitespace-delimited words without allocating a slice.
func countWords(s string) int {
	n := 0
	inWord := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			inWord = false
		} else if !inWord {
			inWord = true
			n++
		}
	}
	return n
}

// bodyWordCount returns the cached word count for a.body, recomputing only
// when the body string changes.
func (a *nativeApp) bodyWordCount() int {
	wcStart := time.Now()
	if !a.wordCountReady || a.wordCountBody != a.body {
		a.wordCountVal = countWords(a.body)
		a.wordCountBody = a.body
		a.wordCountReady = true
	}
	a.metrics.observeAttrs(spanUIWordCount, wcStart,
		slog.Int("body_bytes", len(a.body)),
		slog.Int("words", a.wordCountVal),
	)
	return a.wordCountVal
}

// formatMarkdownPreview preserves single line breaks inside Markdown
// paragraphs (matching Joplin's default note renderer and the raw editor)
// while leaving code blocks, tables, math, headings, and inline spans intact.
func formatMarkdownPreview(src string) string {
	if !strings.Contains(src, "\n") {
		return src
	}
	return formatMarkdownPreviewWithDoc(parser.New(parser.Default), src)
}

func formatMarkdownPreviewWithDoc(d *parser.Document, src string) string {
	if !strings.Contains(src, "\n") {
		return src
	}
	d.Set(src)
	var breaks []int
	for i := 1; i < d.Len(); i++ {
		if d.Block(i).Kind != parser.Paragraph {
			continue
		}
		for _, inl := range d.Inlines(i) {
			if inl.Kind == parser.SoftBreak && inl.Start >= 0 && inl.Start <= len(src) {
				breaks = append(breaks, inl.Start)
			}
		}
	}
	if len(breaks) == 0 {
		return src
	}
	var b strings.Builder
	b.Grow(len(src) + len(breaks)*2)
	prev := 0
	for _, pos := range breaks {
		if pos < prev || pos > len(src) {
			continue
		}
		b.WriteString(src[prev:pos])
		b.WriteString("  ")
		prev = pos
	}
	b.WriteString(src[prev:])
	return b.String()
}

// Small chunks bound cold shaping work for the visible part of a long note.
// Short documents retain a single Markdown view.
const previewChunkRunes = 1000
const previewVirtualizeRunes = 12000
const previewEstimatedRowHeight = 500

// previewChunks splits src inside paragraphs longer than limit runes, at line
// ends that no inline span crosses. It returns nil when no paragraph is long
// enough. Other blocks are never split: their layouts are small, and cutting
// between blocks would drop the spacing a block gets from following another.
func previewChunks(doc *parser.Document, src string, limit int) []string {
	return previewChunksAbove(doc, src, limit, limit)
}

func previewChunksAbove(doc *parser.Document, src string, limit, threshold int) []string {
	if limit <= 0 || utf8.RuneCountInString(src) <= threshold {
		return nil
	}
	doc.Set(src)
	var cuts []int
	for i := 1; i < doc.Len(); i = doc.Block(i).Next {
		b := doc.Block(i)
		if b.Kind != parser.Paragraph || utf8.RuneCountInString(src[b.Start:b.End]) <= threshold {
			continue
		}
		cuts = append(cuts, paragraphCuts(doc, src, i, limit)...)
	}
	if len(cuts) == 0 {
		return nil
	}
	chunks := make([]string, 0, len(cuts)+1)
	prev := 0
	for _, pos := range cuts {
		chunks = append(chunks, src[prev:pos])
		prev = pos
	}
	return append(chunks, src[prev:])
}

// paragraphCuts returns the offsets in src where the block's paragraph can
// start a new Markdown view: the starts of its lines, far enough apart that
// each piece stays under limit runes, and outside every inline span so a link,
// emphasis or code span is never cut.
func paragraphCuts(doc *parser.Document, src string, block, limit int) []int {
	b := doc.Block(block)
	inlines := doc.Inlines(block)
	inSpan := func(pos int) bool {
		for _, inl := range inlines {
			switch inl.Kind {
			case parser.Code, parser.Emphasis, parser.Strong, parser.Strikethrough,
				parser.Link, parser.Image, parser.HTML, parser.Math, parser.FootnoteRef:
				if inl.Start < pos && pos < inl.End {
					return true
				}
			}
		}
		return false
	}
	var cuts []int
	runes := 0
	for pos := b.Start; pos < b.End; {
		nl := strings.IndexByte(src[pos:b.End], '\n')
		if nl < 0 {
			break
		}
		lineEnd := pos + nl
		runes += len([]rune(src[pos : lineEnd+1]))
		next := lineEnd + 1
		if runes >= limit && next < b.End && !inSpan(next) {
			cuts = append(cuts, next)
			runes = 0
		}
		pos = next
	}
	if len(cuts) > 0 {
		return cuts
	}
	// A paragraph written on one line has no line end to split at; wrap it at
	// rune boundaries that no span crosses, as a line break would.
	runes = 0
	for i := b.Start; i < b.End; {
		next := i + 1
		for next < b.End && src[next]&0xC0 == 0x80 {
			next++
		}
		runes++
		if runes >= limit && next < b.End && !inSpan(next) {
			cuts = append(cuts, next)
			runes = 0
		}
		i = next
	}
	return cuts
}

// prepareMarkdownOptions updates and returns the cached markdown.Options for
// the current note body and palette without allocating a new Theme or
// re-formatting unchanged Markdown on every frame.
func (a *nativeApp) prepareMarkdownOptions() string {
	if !a.mdThemeInit || a.mdThemeDark != a.dark || a.mdThemeWash != activeWash {
		a.mdTheme = &markdown.Theme{
			Font:           fontSans,
			FontSize:       sizeBody,
			LineHeight:     1.6,
			CodeLineHeight: 1.45,
			BlockSpacing:   0.85,
			ItemSpacing:    0.28,
			Text:           inkNavy,
			Muted:          inkSoft,
			Link:           routeBlue,
			Strong:         inkNavy,
			StrongWeight:   600,
			Rule:           hairline,
			QuoteBar:       routeBlue.Alpha(0.55),
			QuoteText:      inkSoft,
			QuoteBarWidth:  3,
			Headings: [6]markdown.HeadingStyle{
				{Size: 1.55, Weight: 700, Color: inkNavy, Top: 1.35, Bottom: 0.5},
				{Size: 1.32, Weight: 650, Color: inkNavy, Top: 1.25, Bottom: 0.45},
				{Size: 1.16, Weight: 600, Color: inkNavy, Top: 1.1, Bottom: 0.4},
				{Size: 1.05, Weight: 600, Color: inkNavy, Top: 1.0, Bottom: 0.35},
				{Size: 1.0, Weight: 600, Color: inkNavy, Top: 0.9, Bottom: 0.3},
				{Size: 0.94, Weight: 600, Color: inkSoft, Top: 0.85, Bottom: 0.3},
			},
			CodeFont:            fontMono,
			CodeSize:            0.86,
			Code:                inkNavy,
			CodeBackground:      paperSunken.Alpha(0.75),
			CodeBlockBackground: paperSunken.Alpha(0.55),
			CodeBlockBorder:     hairline,
			TableBorder:         hairline,
			TableHeader:         paperSunken.Alpha(0.65),
			TableStripe:         paperSunken.Alpha(0.28),
			Checkbox:            routeBlue,
		}
		a.mdThemeDark = a.dark
		a.mdThemeWash = activeWash
		a.mdThemeInit = true
	}
	if !a.previewReady || a.previewRaw != a.body {
		a.previewRaw = a.body
		if a.previewDoc == nil {
			a.previewDoc = parser.New(parser.Default)
		}
		a.previewBody = formatMarkdownPreviewWithDoc(a.previewDoc, a.body)
		a.previewChunks = nil
		a.previewChunks = previewChunksAbove(a.previewDoc, a.previewBody, previewChunkRunes, previewVirtualizeRunes)
		a.previewChunkStarts = a.previewChunkStarts[:0]
		start := 0
		for _, chunk := range a.previewChunks {
			a.previewChunkStarts = append(a.previewChunkStarts, start)
			start += len(chunk)
		}
		if !a.previewReady || a.previewPreserveBody != a.body {
			a.previewList = ui.ListState{EstimatedRowHeight: previewEstimatedRowHeight}
		}
		a.previewPreserveBody = ""
		a.previewReady = true
	}
	a.md.Theme = a.mdTheme
	a.md.WrapCode = true
	a.md.OnLink = nil
	if strings.Contains(a.body, "](:/") {
		a.md.OnLink = a.openLink
	}
	return a.previewBody
}

// renameSelectedRail starts inline rename for the chosen notebook or tag, the
// keyboard counterpart of a row's double click.
func (a *nativeApp) renameSelectedRail() {
	if a.inlineRenameKind != "" {
		return
	}
	switch {
	case a.selectedTag != "":
		if tag := a.tagByID(a.selectedTag); tag != nil {
			a.startInlineRename("tag", tag.ID, tag.Title)
		}
	case a.selectedNotebook != "" && a.selectedNotebook != conflictsRail:
		if nb := a.notebookByID(a.selectedNotebook); nb != nil {
			a.startInlineRename("notebook", nb.ID, nb.Title)
		}
	}
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

func (a *nativeApp) openPaletteNote(n *models.NoteSummary) {
	a.paletteOpen = false
	a.openNoteRow(n.ID, n.ParentID, n.IsConflict)
}

// openExternalNote shows the note a joplin://x-callback-url/openNote link
// names, which may sit in a notebook the list is not showing. It reports
// false, leaving a status message, when the note cannot be opened.
func (a *nativeApp) openExternalNote(id string) bool {
	note, err := a.svc.GetNote(id)
	if err != nil {
		a.saveErr = err.Error()
		return false
	}
	if note == nil {
		a.saveErr = "That note is not in this vault yet."
		return false
	}
	a.paletteOpen = false
	return a.openNoteRow(note.ID, note.ParentID, note.IsConflict)
}

// openNoteRow selects the notebook (or the Conflicts rail) of a note and its
// row, paging the list until the row is loaded. A draft that could not be
// saved keeps the editor where it is.
func (a *nativeApp) openNoteRow(id, parentID string, isConflict int) bool {
	a.flush()
	if a.dirty {
		return false
	}
	notebook := a.selectedNotebook
	if isConflict != 0 {
		notebook = conflictsRail
	} else if notebook != "" {
		notebook = parentID
	}
	a.selectedNotebook, a.selectedTag, a.query = notebook, "", ""
	a.selectedID = id
	a.restoring = true
	a.reloadNotes()
	a.restoring = false
	for a.rowOf(id) < 0 && a.hasMore && a.cursor != "" {
		page, err := a.svc.ListNoteSummaries(a.noteFilter(a.cursor))
		if err != nil {
			a.saveErr = err.Error()
			return false
		}
		a.notes = append(a.notes, page.Notes...)
		a.hasMore, a.cursor = page.HasMore, page.NextCursor
	}
	a.showNote(id)
	a.list.ScrollIntoView(a.selectedNote)
	a.remember(id)
	if a.narrow {
		a.narrowEditor = true
	}
	return a.note != nil && a.note.ID == id
}

// Native callbacks do not pass through the toolkit's modal shortcut guard.
func (a *nativeApp) modalOpen() bool {
	return a.confirmDelete || a.confirmBulkDelete || a.confirmDeleteNotebook ||
		a.confirmDeleteTag || a.notebookBlocked || a.notebookOpen || a.tagOpen ||
		a.settingsOpen || a.savedOpen || a.savedDelete != "" || a.resolveConflict || a.paletteOpen ||
		a.confirmImport
}

func (a *nativeApp) menuAction(fn func()) func(*mygo.MenuItem, *mygo.Window) {
	return func(*mygo.MenuItem, *mygo.Window) {
		if a.modalOpen() {
			return
		}
		fn()
		if a.invalidate != nil {
			a.invalidate()
		}
	}
}

func (a *nativeApp) toggleDark() {
	dark := !a.dark
	if err := saveDarkSetting(a.db, dark); err != nil {
		a.saveErr = err.Error()
		return
	}
	a.dark, a.savedDark = dark, dark
}

// appMenu builds the native OS menu bar for Joplin Lite.
func (a *nativeApp) appMenu() *mygo.Menu {
	do := a.menuAction

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
		mygo.Separator(),
		{Label: "Export All Notes (JEX)…", Accelerator: "CmdOrCtrl+Shift+E", Click: do(a.exportAll)},
		{Label: "Import JEX Archive…", Click: do(a.chooseImportArchive)},
		mygo.Separator(),
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
				{Label: "Toggle Dark Appearance", Click: do(a.toggleDark)},
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
		{Label: "New Note", Click: a.menuAction(a.newNote)},
		{Label: "Sync Now", Click: a.menuAction(a.syncNow)},
	})
}

// openNativeWindow opens the native workspace. It returns the window so the
// caller keeps the same lifecycle handling as the webview shell, and the
// workspace so an incoming joplin:// link can select a note in it.
func openNativeWindow(be *backend) (*mygo.Window, *nativeApp) {
	app := newNativeApp(be.db)
	app.be = be
	if be != nil {
		be.setMetrics(app.metrics)
	}
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
		app.metrics.logSummary("window closed")
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
	return win, app
}
