package main

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/lsj5031/joplin-lite/internal/assistant"
	"github.com/lsj5031/joplin-lite/internal/config"
	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/service"
)

func TestNativeAskMenuTogglesPanel(t *testing.T) {
	app, _ := openNativeTestApp(t)
	var ask *mygo.MenuItem
	for _, menu := range app.appMenu().Items() {
		if menu.Label != "View" {
			continue
		}
		for _, item := range menu.Submenu {
			if item.Label == "Ask Notes…" {
				ask = item
			}
		}
	}
	if ask == nil {
		t.Fatal("Ask Notes… is missing from the View menu")
	}
	if ask.Accelerator != "CmdOrCtrl+Shift+A" {
		t.Fatalf("Ask shortcut = %q", ask.Accelerator)
	}
	ask.Click(ask, nil)
	if !app.ask.open {
		t.Fatal("Ask Notes… did not open the panel")
	}
	app.ask.settingsOpen = true
	if !app.modalOpen() {
		t.Fatal("Ask settings should count as a modal for native menu actions")
	}
}

// askDemo records a screenshot sequence and a cursor trail for the live Ask
// walkthrough. It is only used by TestCaptureAskDemo.
type askDemo struct {
	t        *testing.T
	app      *nativeApp
	tt       *ui.Tester
	dir      string
	events   <-chan assistant.Event
	n        int
	x, y     float32
	down     bool
	terminal bool
	failed   string
}

const askDemoFPS = 30

func (d *askDemo) frame() {
	for {
		select {
		case event := <-d.events:
			if event.Type == assistant.EventRunFailed {
				d.failed = event.Message
				d.terminal = true
			}
			if event.Type == assistant.EventRunCompleted {
				d.terminal = true
			}
			d.app.applyAskEvent(event)
		default:
			goto drained
		}
	}
drained:
	d.tt.Advance(time.Second / askDemoFPS)
	d.tt.Frame()
	d.n++
	raw := d.tt.Image()
	img := image.NewRGBA(raw.Bounds())
	copy(img.Pix, raw.Pix)
	drawAskCursor(img, d.x, d.y, d.down)
	path := filepath.Join(d.dir, fmt.Sprintf("%04d.png", d.n))
	f, err := os.Create(path)
	if err != nil {
		d.t.Fatalf("create Ask demo frame: %v", err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		d.t.Fatalf("encode Ask demo frame: %v", err)
	}
}

func drawAskCursor(img *image.RGBA, x, y float32, down bool) {
	// A small high-contrast marker is easier to read than a desktop pointer
	// once the frame sequence is encoded at 30 fps.
	cx, cy := float64(x), float64(y)
	if down {
		cy += 2
	}
	for yy := int(cy) - 8; yy <= int(cy)+8; yy++ {
		for xx := int(cx) - 8; xx <= int(cx)+8; xx++ {
			if !image.Pt(xx, yy).In(img.Bounds()) {
				continue
			}
			dist := math.Hypot(float64(xx)+0.5-cx, float64(yy)+0.5-cy)
			switch {
			case dist <= 7:
				img.SetRGBA(xx, yy, color.RGBA{255, 253, 249, 235})
			case dist <= 5.4:
				img.SetRGBA(xx, yy, color.RGBA{30, 52, 43, 255})
			}
		}
	}
}

func (d *askDemo) hold(seconds float64) {
	for i := int(seconds * askDemoFPS); i > 0; i-- {
		d.frame()
	}
}

func (d *askDemo) center(label string) (float32, float32) {
	r, ok := d.tt.Find(label)
	if !ok {
		d.t.Fatalf("Ask demo: no element %q", label)
	}
	return r.X + r.W/2, r.Y + r.H/2
}

func (d *askDemo) glideTo(tx, ty float32, seconds float64) {
	sx, sy := d.x, d.y
	n := int(seconds * askDemoFPS)
	if n < 1 {
		n = 1
	}
	for i := 1; i <= n; i++ {
		u := float64(i) / float64(n)
		e := u * u * u * (u*(u*6-15) + 10)
		d.x = sx + float32(float64(tx-sx)*e)
		d.y = sy + float32(float64(ty-sy)*e)
		d.tt.Move(d.x, d.y)
		d.frame()
	}
	d.x, d.y = tx, ty
}

func (d *askDemo) glide(label string, seconds float64) {
	tx, ty := d.center(label)
	d.glideTo(tx, ty, seconds)
}

func (d *askDemo) tap(label string) {
	d.glide(label, 0.35)
	d.hold(0.12)
	d.down = true
	d.hold(0.04)
	if err := d.tt.Click(label); err != nil {
		d.t.Fatalf("Ask demo click %s: %v", label, err)
	}
	d.down = false
	d.hold(0.18)
}

func (d *askDemo) typeHuman(s string) {
	for _, r := range s {
		d.tt.Type(string(r))
		d.hold(0.02)
		if r == ' ' || r == ',' {
			d.hold(0.06)
		}
	}
}

func TestCaptureAskDemo(t *testing.T) {
	baseURL := os.Getenv("ASK_LIVE_BASE_URL")
	if baseURL == "" {
		t.Skip("set ASK_LIVE_BASE_URL and CAPTURE_DIR to record the Ask demo")
	}
	modelID := os.Getenv("ASK_LIVE_MODEL")
	if modelID == "" {
		modelID = "main"
	}
	apiKey := os.Getenv("ASK_LIVE_API_KEY")

	root := captureDir(t)
	frames := filepath.Join(root, "ask-demo")
	if err := os.RemoveAll(frames); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(frames, 0o755); err != nil {
		t.Fatal(err)
	}
	registerFonts()

	app, db := openNativeTestApp(t)
	svc := service.New(db)
	everyday := &models.Folder{Title: "Everyday"}
	if err := svc.CreateNotebook(everyday); err != nil {
		t.Fatal(err)
	}
	weekly := &models.Note{
		ParentID: everyday.ID,
		Title:    "Weekly notes",
		Body:     "Keep the useful things. Leave a little room for next week.",
	}
	camera := &models.Note{
		ParentID: everyday.ID,
		Title:    "Camera repair",
		Body:     "The camera repair was approved at the shop. Follow up about the lens.",
	}
	for _, note := range []*models.Note{weekly, camera} {
		if err := svc.CreateNote(note); err != nil {
			t.Fatal(err)
		}
	}

	events := make(chan assistant.Event, 512)
	mgr, err := assistant.NewManager(db, t.TempDir(), func(event assistant.Event) {
		events <- event
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = mgr.Close(ctx)
	})
	settings := assistant.DefaultSettings()
	settings.BaseURL = baseURL
	settings.ModelID = modelID
	settings.APIKey = apiKey
	settings.ContextWindow = 262144
	settings.MaxOutput = 4096
	settings.TimeoutMS = 300000
	if err := mgr.ApplySettings(settings); err != nil {
		t.Fatal(err)
	}

	st := &syncStatus{last: time.Now().Add(-time.Minute)}
	app.be = &backend{ask: mgr, status: st, cfg: &config.Config{SyncTarget: 7}}
	app.reloadNotebooks()
	app.reloadTags()
	app.selectedNotebook = everyday.ID
	app.selectedID = weekly.ID
	app.reloadNotes()
	app.showNote(weekly.ID)
	app.ask.scope = 2 // All notes: the cited camera note is in this notebook.

	tt := ui.NewTester(app.view, 1280, 800)
	tt.SetSize(1280, 800)
	tt.SetTitleBar(ui.TitleBar{Height: 46, Left: 76})
	tt.Frame()
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	d := &askDemo{t: t, app: app, tt: tt, dir: frames, events: events, x: 760, y: 410}

	d.hold(1.4)
	d.tap("Ask")
	d.mark("ask-open")
	d.hold(0.8)
	d.tap("Model…")
	d.hold(1.5)
	d.tap("Save connection")
	d.mark("connection-saved")
	d.hold(0.8)
	d.tap("Question")
	d.typeHuman("What did I decide about the camera repair, and what follow-up is outstanding?")
	d.hold(0.4)
	d.tap("Send")
	d.glideTo(880, 540, 0.4)

	deadline := time.Now().Add(4 * time.Minute)
	for !d.terminal {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the live Ask answer")
		}
		d.frame()
		time.Sleep(time.Second / askDemoFPS)
	}
	if d.failed != "" {
		t.Fatalf("live Ask run failed: %s", d.failed)
	}
	d.mark("answer-complete")
	d.hold(2.2)
	// Open the inline citation itself; the source chip remains a fallback for
	// older providers that emit a token the renderer has not registered.
	if _, ok := d.tt.Find("[E1]"); ok {
		d.tap("[E1]")
	}
	// The inline link and the source chip share the same verified citation. If
	// the link hit box was clipped by the answer scroll, use the chip so the
	// walkthrough still demonstrates source navigation.
	if d.app.note == nil || d.app.note.Title != "Camera repair" {
		if _, ok := d.tt.Find("[E1] Camera repair"); ok {
			d.tap("[E1] Camera repair")
		}
	}
	d.mark("citation-opened")
	d.glideTo(880, 620, 0.4)
	d.hold(1.8)
	d.tap("History")
	d.mark("history-open")
	d.hold(3.8)
	d.hold(0.8)

	t.Logf("Ask demo: %d frames (%.1fs at %d fps) in %s",
		d.n, float64(d.n)/askDemoFPS, askDemoFPS, frames)
}

func (d *askDemo) mark(name string) {
	d.t.Logf("ASK_DEMO_MARK %s frame=%d seconds=%.3f", name, d.n, float64(d.n)/askDemoFPS)
}
