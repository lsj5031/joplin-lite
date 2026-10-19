package main

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/lsj5031/joplin-lite/internal/config"
	"github.com/lsj5031/joplin-lite/internal/models"
	jsync "github.com/lsj5031/joplin-lite/internal/sync"
	"github.com/lsj5031/joplin-lite/service"
)

// The capture harness renders every screen of the native workspace through the
// real view function in MyGO's headless renderer and writes PNGs, so the
// screens can be audited frame by frame and cut into the demo video. It is
// opt-in: set CAPTURE_DIR to run it.
//
//	CAPTURE_DIR=../tmp/ui-audit go test -run TestCaptureScreens ./shell/

func captureDir(t *testing.T) string {
	dir := os.Getenv("CAPTURE_DIR")
	if dir == "" {
		t.Skip("set CAPTURE_DIR to write the screen captures")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	return dir
}

// capture renders the state three times: the first frame mounts whatever
// transient surface just opened and starts its entrance, the advance runs that
// entrance out, and the last frame is the settled screen the audit reads.
func capture(t *testing.T, dir, name string, tt *ui.Tester) {
	t.Helper()
	tt.Frame()
	tt.Advance(260 * time.Millisecond)
	tt.Frame()
	img := tt.Image()
	path := filepath.Join(dir, name+".png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode %s: %v", path, err)
	}
	// A screen is made of what it says; the list is the audit's index.
	texts := tt.Texts()
	if len(texts) > 0 {
		t.Logf("%s: %d texts: %s", name, len(texts), strings.Join(clipList(texts, 14), " | "))
	}
}

// click presses the control showing or labeled s after settling the tree, so
// the press lands on what the state change just built: a click dispatched
// against a frame whose popover is still open would land on its backdrop.
func click(t *testing.T, tt *ui.Tester, s string) {
	t.Helper()
	tt.Frame()
	if err := tt.Click(s); err != nil {
		t.Fatalf("click %s: %v", s, err)
	}
	tt.Frame()
}

func clipList(in []string, n int) []string {
	out := make([]string, 0, n)
	for _, s := range in {
		if len(out) == n {
			out = append(out, "…")
			break
		}
		out = append(out, strings.TrimSpace(s))
	}
	return out
}

// seedCaptures fills a vault with the shapes every screen has to survive: a
// notebook tree, tags, a to-do, a conflict, a long rendered note, an empty one.
func seedCaptures(t *testing.T, svc *service.Service) (work, notes *models.Folder, long *models.Note) {
	t.Helper()
	work = &models.Folder{Title: "Work"}
	notes = &models.Folder{Title: "Notes"}
	archive := &models.Folder{Title: "Archive"}
	for _, nb := range []*models.Folder{work, notes, archive} {
		if err := svc.CreateNotebook(nb); err != nil {
			t.Fatalf("CreateNotebook: %v", err)
		}
	}
	deep := &models.Folder{Title: "2026", ParentID: work.ID}
	if err := svc.CreateNotebook(deep); err != nil {
		t.Fatalf("CreateNotebook child: %v", err)
	}

	long = &models.Note{ParentID: work.ID, Title: "Release checklist", Body: sampleNoteBody}
	if err := svc.CreateNote(long); err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	for i, title := range []string{
		"Weekly review", "Reading list", "Kernel notes", "Meeting 12 Oct",
		"Ideas for the sync line", "Tax receipts", "Bike service",
	} {
		body := fmt.Sprintf("Notes for %s.\n\n- first point\n- second point\n", title)
		if i == 2 {
			body = "Long body kept short so the row stays one line. " + strings.Repeat("filler ", 20)
		}
		n := &models.Note{ParentID: notes.ID, Title: title, Body: body}
		if i == 1 {
			n.IsTodo = 1
		}
		if i == 4 {
			n.IsTodo, n.TodoCompleted = 1, 1
		}
		if err := svc.CreateNote(n); err != nil {
			t.Fatalf("CreateNote: %v", err)
		}
	}
	if err := svc.CreateNote(&models.Note{ParentID: work.ID, Title: "Empty draft"}); err != nil {
		t.Fatalf("CreateNote: %v", err)
	}

	original := &models.Note{ParentID: work.ID, Title: "Sync conflict draft", Body: "mine"}
	if err := svc.CreateNote(original); err != nil {
		t.Fatalf("CreateNote original: %v", err)
	}
	if err := svc.CreateNote(&models.Note{
		ParentID:           work.ID,
		Title:              "Sync conflict draft (conflict)",
		Body:               "theirs",
		IsConflict:         1,
		ConflictOriginalID: original.ID,
	}); err != nil {
		t.Fatalf("CreateNote conflict: %v", err)
	}

	for _, title := range []string{"work", "reading", "deep-work"} {
		tag := &models.Tag{Title: title}
		if err := svc.CreateTag(tag); err != nil {
			t.Fatalf("CreateTag: %v", err)
		}
		if title == "work" {
			if err := svc.AddTag(long.ID, tag.ID); err != nil {
				t.Fatalf("AddTag: %v", err)
			}
		}
	}
	return work, notes, long
}

// sampleNoteBody is the rendered-preview fixture: the Markdown a real note
// holds, so the preview screen is audited with headings, lists, a code block,
// a table and a link in it.
const sampleNoteBody = `## Before the beta

The native shell is the product now: one process, no Chromium, and the same
vault the clipper writes into.

- [x] Port the item round-trip
- [ ] Re-check E2EE interop
- [ ] Trim the startup path

### Numbers

| Measurement | Value |
| --- | --- |
| Processes | 1 |
| Binary | 17.5 MiB |
| Idle RSS | ~120 MB |

> A note is a record, not a document: unknown properties survive a round-trip.

` + "```go" + `
func (a *nativeApp) syncNow() {
	a.flush()
	a.be.SyncNow()
}
` + "```" + `

See the [migration notes](docs/native-ui-migration.md) for the full table.`

// TestCaptureReadme uses short, synthetic everyday notes for public screenshots.
func TestCaptureReadme(t *testing.T) {
	dir := captureDir(t)
	registerFonts()
	app, db := openNativeTestApp(t)
	svc := service.New(db)
	for _, title := range []string{"Everyday", "Reading", "Ideas"} {
		if err := svc.CreateNotebook(&models.Folder{Title: title}); err != nil {
			t.Fatal(err)
		}
	}
	var selected *models.Note
	for _, entry := range []struct{ title, body string }{
		{"Books for autumn", "A few pages before the day begins.\n\n- A book of essays\n- Something to read twice"},
		{"A small kitchen garden", "Basil by the window. Rosemary by the door."},
		{"Weekly notes", "Keep the useful things. Leave a little room for next week."},
		{"Favourite walks", "The long way home, along the water."},
		{"A slower Saturday", "## Leave the afternoon open\n\nA walk by the water, a small notebook, and no particular hurry.\n\n### Take along\n\n- [x] A thermos of coffee\n- [ ] The book from the bedside table\n- [ ] Something to write with\n\n> Make room for the things you notice."},
	} {
		selected = addTestNote(t, svc, "", entry.title, entry.body)
	}
	app.reloadNotebooks()
	app.reloadNotes()
	app.showNote(selected.ID)
	tt := ui.NewTester(app.view, 1280, 900)
	capture(t, dir, "native-workspace", tt)
	click(t, tt, "Edit")
	capture(t, dir, "native-writing", tt)
	click(t, tt, "Preview")
	app.narrowEditor = true
	tt.SetSize(900, 700)
	capture(t, dir, "native-narrow", tt)
}

// TestCaptureScreens renders every screen state and writes it to CAPTURE_DIR.
func TestCaptureScreens(t *testing.T) {
	dir := captureDir(t)
	registerFonts()

	app, db := openNativeTestApp(t)
	svc := service.New(db)
	work, notes, long := seedCaptures(t, svc)
	app.reloadNotebooks()
	app.reloadTags()
	app.reloadNotes()

	tt := ui.NewTester(app.view, 1280, 800)
	tt.SetSize(1280, 800)

	// 1. The workspace with a note open in the editor.
	app.selectedNotebook = work.ID
	app.selectedID = long.ID
	app.reloadNotes()
	app.showNote(long.ID)
	app.toggleEdit()
	capture(t, dir, "01-workspace", tt)

	// 2. The rendered preview.
	app.editing = false
	capture(t, dir, "02-preview", tt)
	app.editing = true

	// 3. The notebook rail with a nested notebook chosen.
	app.selectedNotebook = notes.ID
	app.selectedID = ""
	app.reloadNotes()
	capture(t, dir, "03-notebook", tt)

	// 4. The tag filter.
	app.reloadTags()
	if len(app.tags) > 0 {
		app.selectedTag = app.tags[0].ID
		app.selectedNotebook = ""
		app.reloadNotes()
	}
	capture(t, dir, "04-tag-filter", tt)

	// 5. Conflicts.
	app.selectedTag = ""
	app.selectedNotebook = conflictsRail
	app.reloadNotes()
	capture(t, dir, "05-conflicts", tt)

	// 6. Search with results, then a search with none.
	app.selectedNotebook = ""
	app.query = "reading"
	app.reloadNotes()
	capture(t, dir, "06-search", tt)
	app.query = "no such note anywhere"
	app.reloadNotes()
	capture(t, dir, "07-search-empty", tt)
	app.query = ""
	app.reloadNotes()

	// 8. A multi-row selection with the bulk-action bar.
	app.selection.Clear()
	for i := 0; i < 3 && i < len(app.notes); i++ {
		app.selection.Add(app.notes[i].ID)
	}
	capture(t, dir, "08-bulk-select", tt)
	app.selection.Clear()

	// 9-13. The transient surfaces.
	click(t, tt, "Sort")
	tt.Advance(220 * time.Millisecond)
	capture(t, dir, "09-sort-menu", tt)
	click(t, tt, "Sort")

	click(t, tt, "Saved searches")
	tt.Advance(220 * time.Millisecond)
	capture(t, dir, "10-saved-menu", tt)
	click(t, tt, "Saved searches")

	app.selectedNotebook = work.ID
	app.selectedID = long.ID
	app.reloadNotes()
	app.showNote(long.ID)
	click(t, tt, "Tags")
	tt.Advance(220 * time.Millisecond)
	capture(t, dir, "11-note-tags-menu", tt)
	click(t, tt, "Tags")

	click(t, tt, "Move")
	tt.Advance(220 * time.Millisecond)
	capture(t, dir, "12-move-menu", tt)
	click(t, tt, "Move")

	app.openNewNotebook()
	tt.Advance(220 * time.Millisecond)
	capture(t, dir, "13-new-notebook", tt)
	app.notebookOpen = false

	app.openSettings()
	tt.Advance(220 * time.Millisecond)
	capture(t, dir, "14-settings", tt)
	app.settingsOpen = false

	app.openNewTag()
	tt.Advance(220 * time.Millisecond)
	capture(t, dir, "15-new-tag", tt)
	app.tagOpen = false

	click(t, tt, "Delete…")
	tt.Advance(220 * time.Millisecond)
	capture(t, dir, "16-delete-note", tt)
	click(t, tt, "Cancel")

	// 17. The empty editor.
	app.selectedID = ""
	app.note = nil
	app.title, app.body = "", ""
	capture(t, dir, "17-empty-editor", tt)

	// 18. An empty vault.
	empty, db2 := openNativeTestApp(t)
	ttEmpty := ui.NewTester(empty.view, 1280, 800)
	capture(t, dir, "18-empty-vault", ttEmpty)
	_ = db2

	// 19-22. The narrow shell: list, slide-over rail, editor, stacked strip.
	app.selectedNotebook = work.ID
	app.selectedID = long.ID
	app.reloadNotes()
	app.showNote(long.ID)
	tt.SetSize(900, 640)
	app.narrowEditor = false
	capture(t, dir, "19-narrow-list", tt)
	app.railOpen = true
	tt.Advance(260 * time.Millisecond)
	capture(t, dir, "20-narrow-rail", tt)
	app.railOpen = false
	app.narrowEditor = true
	capture(t, dir, "21-narrow-editor", tt)

	// 23-26. The sync states on the status strip.
	app.narrowEditor = false
	tt.SetSize(1280, 800)
	st := &syncStatus{last: time.Date(2026, 10, 19, 15, 4, 0, 0, time.Local)}
	app.be = &backend{status: st, cfg: &config.Config{SyncTarget: 7}}
	st.finish(fmt.Errorf("sync refused: %w", jsync.ErrExclusiveLock))
	st.retryIn(time.Now().Add(time.Minute))
	tt.Frame()
	capture(t, dir, "22-sync-paused", tt)
	st.start()
	tt.Advance(120 * time.Millisecond)
	capture(t, dir, "23-syncing", tt)
	st.finish(nil)
	tt.Frame()
	capture(t, dir, "24-synced", tt)
	st.finish(fmt.Errorf("push: invalid credentials"))
	tt.Frame()
	capture(t, dir, "25-sync-failed", tt)
	st.start()
	for i, phase := range []string{"Checking remote", "Checking library", "Decrypting", "Uploading", "Finishing"} {
		st.progress(phase)
		capture(t, dir, fmt.Sprintf("%02d-sync-phase", 26+i), tt)
	}
	st.progress("Checking library")
	tt.SetSize(900, 640)
	capture(t, dir, "31-narrow-sync-phase", tt)
	app.be = nil
}
