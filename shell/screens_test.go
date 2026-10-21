package main

import (
	"fmt"
	"image"
	"image/png"
	"math"
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

// forceMacChrome renders a capture with the macOS window chrome on every host,
// matching the TitleBarHiddenInset metrics and traffic lights the published
// screenshots and promo frames composite.
func forceMacChrome(t *testing.T) {
	t.Helper()
	previous := windowsChrome
	windowsChrome = false
	t.Cleanup(func() { windowsChrome = previous })
}

// screen renders the state three times: the first frame mounts whatever
// transient surface just opened and starts its entrance, the advance runs that
// entrance out, and the last frame is the settled screen the audit reads.
func screen(tt *ui.Tester) *image.RGBA {
	tt.Frame()
	tt.Advance(260 * time.Millisecond)
	tt.Frame()
	return tt.Image()
}

// writeCapture writes one PNG and logs its text, the audit's index.
func writeCapture(t *testing.T, dir, name string, img *image.RGBA, tt *ui.Tester) {
	t.Helper()
	path := filepath.Join(dir, name+".png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode %s: %v", path, err)
	}
	texts := tt.Texts()
	if len(texts) > 0 {
		t.Logf("%s: %d texts: %s", name, len(texts), strings.Join(clipList(texts, 14), " | "))
	}
}

// capture writes a settled screen for the audit.
func capture(t *testing.T, dir, name string, tt *ui.Tester) {
	t.Helper()
	writeCapture(t, dir, name, screen(tt), tt)
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
		if title == "work" || title == "deep-work" {
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

// seedReadme fills a vault with the short, synthetic everyday notes the public
// screenshots and the launch film share, so the README, the stills and the
// video show one library. It returns the note both open.
func seedReadme(t *testing.T, svc *service.Service) *models.Note {
	t.Helper()
	var everydayID, readingID, ideasID string
	for _, title := range []string{"Everyday", "Reading", "Ideas"} {
		nb := &models.Folder{Title: title}
		if err := svc.CreateNotebook(nb); err != nil {
			t.Fatalf("CreateNotebook: %v", err)
		}
		switch title {
		case "Everyday":
			everydayID = nb.ID
		case "Reading":
			readingID = nb.ID
		case "Ideas":
			ideasID = nb.ID
		}
	}
	// The notes are a minute apart, so the list order is the fixture's and not
	// the order a same-millisecond sort happens to return. Screenshots and the
	// launch film then show one library in one order.
	base := time.Now().Add(-time.Hour).UnixMilli()
	var selected, books *models.Note
	for i, entry := range []struct {
		parent string
		title  string
		body   string
	}{
		{readingID, "Books for autumn", "A few pages before the day begins.\n\n- A book of essays\n- Something to read twice"},
		{ideasID, "A small kitchen garden", "Basil by the window. Rosemary by the door."},
		{everydayID, "Weekly notes", "Keep the useful things. Leave a little room for next week."},
		{everydayID, "Favourite walks", "The long way home, along the water."},
		{everydayID, "A slower Saturday", "## Leave the afternoon open\n\nA walk by the water, a small notebook, and no particular hurry.\n\n### Take along\n\n- [x] A thermos of coffee\n- [ ] The book from the bedside table\n- [ ] Something to write with\n\n> Make room for the things you notice."},
	} {
		at := base + int64(i)*60_000
		n := &models.Note{ParentID: entry.parent, Title: entry.title, Body: entry.body, CreatedTime: at, UpdatedTime: at}
		if err := svc.CreateNote(n); err != nil {
			t.Fatalf("CreateNote: %v", err)
		}
		if i == 0 {
			books = n
		}
		selected = n
	}
	for _, name := range []string{"weekend", "outdoors", "reading"} {
		tag := &models.Tag{Title: name}
		if err := svc.CreateTag(tag); err != nil {
			t.Fatalf("CreateTag: %v", err)
		}
		target := selected.ID
		if name == "reading" {
			target = books.ID
		}
		if err := svc.AddTag(target, tag.ID); err != nil {
			t.Fatalf("AddTag: %v", err)
		}
	}
	if err := svc.UpdateNote(selected); err != nil {
		t.Fatalf("UpdateNote: %v", err)
	}
	return selected
}

// TestCaptureReadme uses short, synthetic everyday notes for public screenshots,
// rendered at 2x Retina with macOS TitleBarHiddenInset traffic lights, window
// vibrancy, rounded window corners, and WindowServer drop shadow.
func TestCaptureReadme(t *testing.T) {
	dir := captureDir(t)
	forceMacChrome(t)
	registerFonts()
	app, db := openNativeTestApp(t)
	svc := service.New(db)
	selected := seedReadme(t, svc)

	st := &syncStatus{last: time.Date(2026, 10, 19, 14, 32, 0, 0, time.Local)}
	st.finish(nil)
	app.be = &backend{status: st, cfg: &config.Config{SyncTarget: 7}}

	app.reloadNotebooks()
	app.reloadTags()
	app.selectedID = selected.ID
	app.reloadNotes()
	app.showNote(selected.ID)
	app.railOpen = true

	tt := ui.NewTester(app.view, 1280, 800)
	tt.SetSize(1280, 800)
	tt.SetScale(2)
	tt.SetVibrancy(true)
	tt.SetTitleBar(ui.TitleBar{Height: 46, Left: 76})

	captureMacWindow(t, dir, "native-workspace", tt, app.dark)
	app.dark = true
	captureMacWindow(t, dir, "native-dark", tt, app.dark)
	app.dark = false
	app.railOpen = false
	click(t, tt, "Edit")
	captureMacWindow(t, dir, "native-writing", tt, app.dark)
	click(t, tt, "Preview")
	app.narrowEditor = true
	tt.SetSize(900, 700)
	captureMacWindow(t, dir, "native-narrow", tt, app.dark)
}

func captureMacWindow(t *testing.T, dir, name string, tt *ui.Tester, dark bool) {
	t.Helper()
	raw := screen(tt)
	framed := macOSWindow(raw, dark)
	writeCapture(t, dir, name, framed, tt)
}

// macOSWindow composites a 2x Retina window surface with the macOS
// TitleBarHiddenInset traffic lights (at TrafficLightPosition{X: 16, Y: 16}),
// rounded window corners (16 DIP / 32 px radius), 1px rim, and WindowServer
// drop shadow on a transparent alpha border.
func macOSWindow(src *image.RGBA, dark bool) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	win := image.NewRGBA(image.Rect(0, 0, w, h))
	copy(win.Pix, src.Pix)
	paintTrafficLights(win, dark)

	const (
		padX      = 88
		padTop    = 60
		padBottom = 116
		radius    = 32.0
	)
	outW := w + padX*2
	outH := h + padTop + padBottom
	out := image.NewRGBA(image.Rect(0, 0, outW, outH))

	hw := float64(w) * 0.5
	hh := float64(h) * 0.5
	cx := float64(padX) + hw
	cy := float64(padTop) + hh

	for y := 0; y < outH; y++ {
		py := float64(y) + 0.5
		for x := 0; x < outW; x++ {
			px := float64(x) + 0.5
			sd := sdRoundRect(px-cx, py-cy, hw, hh, radius)

			// Compute WindowServer dual-layer drop shadow (contact umbra + offset penumbra).
			sdContact := sdRoundRect(px-cx, (py-4.0)-cy, hw, hh, radius)
			sdKey := sdRoundRect(px-cx, (py-22.0)-cy, hw, hh, radius)
			var shadowA float64
			if sdContact > -2.0 {
				dc := math.Max(0, sdContact)
				dk := math.Max(0, sdKey)
				umbra := 0.18 * math.Exp(-dc/9.0)
				penumbra := 0.26 * math.Exp(-(dk*dk)/(2.0*34.0*34.0))
				shadowA = math.Min(1.0, umbra+penumbra)
				// Crisp 1px outer frame hairline around the window edge.
				if sd >= -0.5 && sd <= 1.2 {
					edge := 1.0 - math.Abs(sd-0.35)/0.85
					if edge > 0 {
						rimA := 0.24 * edge
						if dark {
							rimA = 0.42 * edge
						}
						shadowA = math.Min(1.0, shadowA+rimA*(1.0-shadowA))
					}
				}
			}

			if sd >= 0.5 {
				if shadowA > 0.001 {
					a8 := uint8(math.Round(shadowA * 255.0))
					i := out.PixOffset(x, y)
					out.Pix[i+3] = a8
				}
				continue
			}

			cov := clamp01(0.5 - sd)
			sx := clampInt(x-padX, 0, w-1)
			sy := clampInt(y-padTop, 0, h-1)
			si := win.PixOffset(sx, sy)
			r := float64(win.Pix[si+0])
			g := float64(win.Pix[si+1])
			bl := float64(win.Pix[si+2])

			// Subtle 1px inner specular highlight along the window perimeter.
			if sd >= -2.2 && sd <= 0.0 {
				inner := 1.0 - math.Abs(sd+1.0)/1.2
				if inner > 0 {
					topWeight := clamp01(1.0 - float64(sy)/90.0)
					hl := 0.16 + 0.18*topWeight
					if dark {
						hl = 0.12 + 0.10*topWeight
					}
					t := inner * hl
					r = r*(1.0-t) + 255.0*t
					g = g*(1.0-t) + 255.0*t
					bl = bl*(1.0-t) + 255.0*t
				}
			}

			// Composite window pixel (with coverage cov) over the shadow pixel (0,0,0,shadowA).
			outA := cov + shadowA*(1.0-cov)
			if outA > 0 {
				i := out.PixOffset(x, y)
				out.Pix[i+0] = uint8(math.Round(clamp01((r/255.0)*cov) * 255.0))
				out.Pix[i+1] = uint8(math.Round(clamp01((g/255.0)*cov) * 255.0))
				out.Pix[i+2] = uint8(math.Round(clamp01((bl/255.0)*cov) * 255.0))
				out.Pix[i+3] = uint8(math.Round(clamp01(outA) * 255.0))
			}
		}
	}
	return out
}

// paintTrafficLights draws the three macOS window controls at 2x Retina scale
// matching TrafficLightPosition{X: 16, Y: 16} (centers at x=46, 92, 138 px,
// y=45.5 px, radius=13.5 px).
func paintTrafficLights(img *image.RGBA, dark bool) {
	type light struct {
		cx         float64
		topR, topG float64
		topB       float64
		botR, botG float64
		botB       float64
		rimR, rimG float64
		rimB       float64
	}
	lights := []light{
		{
			cx:   46.0,
			topR: 255, topG: 99, topB: 91,
			botR: 254, botG: 89, botB: 80,
			rimR: 218, rimG: 67, rimB: 60,
		},
		{
			cx:   92.0,
			topR: 255, topG: 193, topB: 52,
			botR: 253, botG: 184, botB: 42,
			rimR: 216, rimG: 156, rimB: 34,
		},
		{
			cx:   138.0,
			topR: 46, topG: 206, topB: 68,
			botR: 38, botG: 197, botB: 60,
			rimR: 28, rimG: 168, rimB: 42,
		},
	}
	const (
		cy     = 45.5
		radius = 13.5
	)
	for _, l := range lights {
		minX := int(math.Floor(l.cx - radius - 2))
		maxX := int(math.Ceil(l.cx + radius + 2))
		minY := int(math.Floor(cy - radius - 2))
		maxY := int(math.Ceil(cy + radius + 2))
		for y := minY; y <= maxY; y++ {
			for x := minX; x <= maxX; x++ {
				if !image.Pt(x, y).In(img.Bounds()) {
					continue
				}
				dx := (float64(x) + 0.5) - l.cx
				dy := (float64(y) + 0.5) - cy
				dist := math.Hypot(dx, dy)
				sd := dist - radius
				cov := clamp01(0.5 - sd)
				if cov <= 0 {
					continue
				}
				v := clamp01((dy + radius) / (2.0 * radius))
				fr := l.topR*(1.0-v) + l.botR*v
				fg := l.topG*(1.0-v) + l.botG*v
				fb := l.topB*(1.0-v) + l.botB*v

				// 1.4px inner border stroke around the disc.
				rimCov := clamp01(1.4 - math.Abs(sd+0.7))
				if dark {
					rimCov *= 0.55
				}
				cr := fr*(1.0-rimCov) + l.rimR*rimCov
				cg := fg*(1.0-rimCov) + l.rimG*rimCov
				cb := fb*(1.0-rimCov) + l.rimB*rimCov

				i := img.PixOffset(x, y)
				br := float64(img.Pix[i+0])
				bg := float64(img.Pix[i+1])
				bb := float64(img.Pix[i+2])
				img.Pix[i+0] = uint8(math.Round(br*(1.0-cov) + cr*cov))
				img.Pix[i+1] = uint8(math.Round(bg*(1.0-cov) + cg*cov))
				img.Pix[i+2] = uint8(math.Round(bb*(1.0-cov) + cb*cov))
				img.Pix[i+3] = 255
			}
		}
	}
}

func sdRoundRect(px, py, hw, hh, r float64) float64 {
	qx := math.Abs(px) - (hw - r)
	qy := math.Abs(py) - (hh - r)
	outX := math.Max(qx, 0)
	outY := math.Max(qy, 0)
	return math.Hypot(outX, outY) + math.Min(math.Max(qx, qy), 0) - r
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
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
	click(t, tt, "Note tags")
	tt.Advance(220 * time.Millisecond)
	capture(t, dir, "11-note-tags-menu", tt)
	click(t, tt, "Note tags")

	click(t, tt, "Move note")
	tt.Advance(220 * time.Millisecond)
	capture(t, dir, "12-move-menu", tt)
	click(t, tt, "Move note")

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

	click(t, tt, "Delete note")
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
	tt.SetSize(1280, 800)
	for i, phase := range []string{"Checking items — 0 of 7541", "Checking items — 2138 of 7541", "Checking items — 9 of 9+", "Checking items — 7541 of 7541"} {
		st.progress(phase)
		capture(t, dir, fmt.Sprintf("%02d-check-items-progress", 32+i), tt)
	}
	st.progress("Checking items — 2138 of 7541")
	tt.SetSize(900, 640)
	capture(t, dir, "36-narrow-check-items-progress", tt)
	app.be = nil

	// 37. Self-contained Resolve conflict side-by-side comparison modal.
	tt.SetSize(1280, 800)
	app.narrowEditor = false
	app.selectedNotebook = conflictsRail
	app.selectedID = ""
	app.reloadNotes()
	tt.Frame()
	if err := tt.Click("Resolve conflict"); err != nil {
		t.Fatalf("click Resolve conflict for screen 37: %v", err)
	}
	tt.Advance(220 * time.Millisecond)
	capture(t, dir, "37-resolve-conflict-diff", tt)
	if err := tt.Click("Cancel"); err != nil {
		t.Fatalf("cancel Resolve conflict for screen 37: %v", err)
	}

	// 38-39. Field Notes dark palette: workspace and preview.
	app.dark = true
	app.selectedNotebook = work.ID
	app.selectedID = long.ID
	app.reloadNotes()
	app.showNote(long.ID)
	app.toggleEdit()
	tt.Frame()
	capture(t, dir, "38-dark-workspace", tt)
	app.toggleEdit()
	tt.Frame()
	capture(t, dir, "39-dark-preview", tt)
	app.dark = false
	applyPalette(false)
}
