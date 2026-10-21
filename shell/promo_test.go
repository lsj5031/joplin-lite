package main

import (
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/lsj5031/joplin-lite/internal/config"
	"github.com/lsj5031/joplin-lite/service"
)

// TestCapturePromo renders the promotional tour, which qa/promo cuts into the
// product film. Every frame is rendered at 2x at 30 fps with real pointer
// movement, hover states, and beat/cursor telemetry in promo.json so the
// compositor plays the entire capture 1:1 without time-warping or jump cuts.
//
//	CAPTURE_DIR=../tmp/promo go test -run TestCapturePromo ./shell/
//	node qa/promo/render.mjs            # -> docs/promo/*.mp4
const promoFPS = 30

type promoPoint struct {
	X float64
	Y float64
}

type promoCursor struct {
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Down    bool    `json:"down,omitempty"`
	Opacity float64 `json:"opacity"`
}

type promoBeat struct {
	Name string `json:"name"`
	From int    `json:"from"`
	To   int    `json:"to"`
}

type promoSize struct {
	W int `json:"w"`
	H int `json:"h"`
}

type promoManifest struct {
	FPS      int           `json:"fps"`
	DIP      promoSize     `json:"dip"`
	Pixels   promoSize     `json:"pixels"`
	DarkFrom int           `json:"darkFrom"`
	Beats    []promoBeat   `json:"beats"`
	Cursors  []promoCursor `json:"cursors"`
}

// promoTour writes one PNG per frame at 30 frames a second, driving real
// pointer hover and click states through ui.Tester.
type promoTour struct {
	t        *testing.T
	app      *nativeApp
	tt       *ui.Tester
	dir      string
	n        int
	cur      promoPoint
	down     bool
	curAlpha float64
	darkFrom int
	beats    []promoBeat
	cursors  []promoCursor
	open     int
	name     string
}

func (p *promoTour) frame() {
	p.tt.Move(float32(p.cur.X), float32(p.cur.Y))
	p.tt.Advance(time.Second / promoFPS)
	p.tt.Frame()
	p.n++
	p.cursors = append(p.cursors, promoCursor{
		X:       math.Round(p.cur.X*10) / 10,
		Y:       math.Round(p.cur.Y*10) / 10,
		Down:    p.down,
		Opacity: math.Round(p.curAlpha*100) / 100,
	})
	raw := p.tt.Image()
	img := image.NewRGBA(raw.Bounds())
	copy(img.Pix, raw.Pix)
	paintTrafficLights(img, p.app != nil && p.app.dark)
	f, err := os.Create(filepath.Join(p.dir, fmt.Sprintf("%04d.png", p.n)))
	if err != nil {
		p.t.Fatalf("create frame %d: %v", p.n, err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		p.t.Fatalf("encode frame %d: %v", p.n, err)
	}
}

func (p *promoTour) holdFrames(frames int) {
	for i := 0; i < frames; i++ {
		p.frame()
	}
}

func (p *promoTour) hold(seconds float64) {
	p.holdFrames(int(math.Round(seconds * promoFPS)))
}

// glide moves the pointer along a minimum-jerk curve with a gentle arc,
// calling tt.Move on every frame so hover states animate naturally.
func (p *promoTour) glide(x, y float64, seconds float64) {
	sx, sy := p.cur.X, p.cur.Y
	dx, dy := x-sx, y-sy
	dist := math.Hypot(dx, dy)
	// Subtle perpendicular arc proportional to distance so motion feels hand-guided.
	arc := math.Min(18, dist*0.06)
	var nx, ny float64
	if dist > 1 {
		nx, ny = -dy/dist, dx/dist
		if ny > 0 {
			nx, ny = -nx, -ny
		}
	}
	n := int(math.Round(seconds * promoFPS))
	if n < 1 {
		n = 1
	}
	startAlpha := p.curAlpha
	for i := 1; i <= n; i++ {
		u := float64(i) / float64(n)
		// Minimum-jerk fifth-order polynomial: 10u^3 - 15u^4 + 6u^5
		e := u * u * u * (u*(u*6-15) + 10)
		bow := math.Sin(u*math.Pi) * arc
		p.cur.X = sx + dx*e + nx*bow
		p.cur.Y = sy + dy*e + ny*bow
		p.curAlpha = startAlpha + (1.0-startAlpha)*math.Min(1, u*2.5)
		p.frame()
	}
	p.cur.X, p.cur.Y = x, y
	p.curAlpha = 1.0
}

// center returns the middle of the element showing or labeled label.
func (p *promoTour) center(label string) (float64, float64) {
	if r, ok := p.tt.Find(label); ok {
		return float64(r.X + r.W/2), float64(r.Y + r.H/2)
	}
	p.t.Fatalf("promo: no element %q to point at", label)
	return p.cur.X, p.cur.Y
}

// tap glides to the control, lets its hover state settle, and clicks it with
// visible press feedback.
func (p *promoTour) tap(label string) {
	x, y := p.center(label)
	p.glide(x, y, 0.42)
	p.holdFrames(3)
	p.down = true
	p.holdFrames(2)
	if err := p.tt.Click(label); err != nil {
		p.t.Fatalf("click %s: %v", label, err)
	}
	p.down = false
	p.holdFrames(2)
	p.tt.Frame()
}

// typeHuman types s with natural cadence: brisk within words and a brief beat
// at word boundaries, punctuation, and line breaks.
func (p *promoTour) typeHuman(s string) {
	for _, r := range s {
		p.tt.Type(string(r))
		p.curAlpha = 0.90
		frames := 1
		switch {
		case r == '\n':
			frames = 3
		case r == '.' || r == ':':
			frames = 3
		case r == ' ':
			frames = 2
		}
		p.holdFrames(frames)
	}
}

// typeAt types s a character every perChar frames, for short filter queries.
func (p *promoTour) typeAt(s string, perChar int) {
	for _, r := range s {
		p.tt.Type(string(r))
		p.holdFrames(perChar)
	}
}

func (p *promoTour) begin(name string) {
	p.open, p.name = p.n+1, name
}

func (p *promoTour) end() {
	p.beats = append(p.beats, promoBeat{Name: p.name, From: p.open, To: p.n})
}

// TestCapturePromo renders the tour.
func TestCapturePromo(t *testing.T) {
	root := captureDir(t)
	frames := filepath.Join(root, "frames")
	if err := os.RemoveAll(frames); err != nil {
		t.Fatalf("clear promo frames: %v", err)
	}
	if err := os.MkdirAll(frames, 0o755); err != nil {
		t.Fatalf("mkdir promo: %v", err)
	}
	registerFonts()

	app, db := openNativeTestApp(t)
	svc := service.New(db)
	opened := seedReadme(t, svc)

	// Show a live, synced library in the footer strip.
	st := &syncStatus{last: time.Date(2026, 10, 19, 14, 32, 0, 0, time.Local)}
	st.finish(nil)
	app.be = &backend{status: st, cfg: &config.Config{SyncTarget: 7}}

	app.reloadNotebooks()
	app.reloadTags()
	app.selectedID = opened.ID
	app.reloadNotes()
	app.showNote(opened.ID)

	tt := ui.NewTester(app.view, 1280, 800)
	tt.SetSize(1280, 800)
	tt.SetScale(2)
	tt.SetVibrancy(true)
	tt.SetTitleBar(ui.TitleBar{Height: 46, Left: 76})
	// Settle initial entrance animations before recording frame 1.
	tt.Frame()
	tt.Advance(300 * time.Millisecond)
	tt.Frame()

	p := &promoTour{
		t:        t,
		app:      app,
		tt:       tt,
		dir:      frames,
		cur:      promoPoint{X: 680, Y: 360},
		curAlpha: 0.9,
	}

	// 1. Library: reading "A slower Saturday", opening the Liquid Glass
	// notebooks & tags rail, and filtering to the Everyday notebook.
	p.begin("library")
	p.hold(0.75)
	p.tap("Open notebooks")
	p.hold(0.85)
	p.tap("Everyday")
	p.hold(0.75)
	p.end()

	// 2. Write: create a new note from the sidebar (updating the Everyday count
	// in the rail from 3 to 4), type the title (updating the note list row live),
	// write Markdown in the body, and toggle Preview to render the typeset page.
	p.begin("write")
	p.tap("New note")
	p.hold(0.25)
	p.tap("Note title")
	p.typeHuman("Along the waterfront")
	p.hold(0.25)
	p.tap("Note body")
	p.typeHuman("## The long way home\n\nThe path follows the water past the boathouse.\n\n- [x] Pack the field notebook\n- [ ] Stop for coffee")
	p.hold(0.4)
	p.tap("Preview")
	p.hold(1.1)
	p.end()

	// 3. Palette: open the Command Palette from the search bar and filter live
	// down to "Toggle dark mode".
	p.begin("palette")
	p.tap("Command palette")
	p.hold(0.45)
	p.typeAt("dark", 4)
	p.hold(0.7)
	p.end()

	// 4. Night: run the command to switch the workspace to the Field Notes dark
	// palette in-place, then select "All notes" in the rail to show the full
	// 6-note library in dark mode.
	p.begin("night")
	p.darkFrom = p.n + 1
	tt.Key(0, ui.KeyEnter)
	p.hold(0.85)
	p.tap("All notes")
	p.glide(760, 460, 0.45)
	p.hold(1.25)
	p.end()

	t.Logf("promo: %d frames (%.2fs at %d fps), %d beats in %s",
		p.n, float64(p.n)/promoFPS, promoFPS, len(p.beats), frames)

	img := tt.Image()
	manifest := promoManifest{
		FPS:      promoFPS,
		DIP:      promoSize{W: 1280, H: 800},
		Pixels:   promoSize{W: img.Bounds().Dx(), H: img.Bounds().Dy()},
		DarkFrom: p.darkFrom,
		Beats:    p.beats,
		Cursors:  p.cursors,
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("manifest json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "promo.json"), append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write promo.json: %v", err)
	}
}
