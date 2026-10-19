package main

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/lsj5031/joplin-lite/internal/config"
	"github.com/lsj5031/joplin-lite/service"
)

// TestCaptureDemo drives the real workspace through a short product tour and
// writes every frame, so ffmpeg can cut it into the native demo clip. It is
// opt-in like TestCaptureScreens: set CAPTURE_DIR to run it.
//
//	CAPTURE_DIR=../tmp/ui-audit go test -run TestCaptureDemo ./shell/
//	ffmpeg -y -framerate 30 -i ../tmp/ui-audit/demo/%04d.png \
//	  -c:v libx264 -pix_fmt yuv420p -crf 20 -movflags +faststart \
//	  ../qa/video/native-workspace.mp4
const demoFPS = 30

// demo writes one PNG per frame at 30 frames a second, so holds and the few
// transitions a screen has read at real speed.
type demo struct {
	t   *testing.T
	tt  *ui.Tester
	dir string
	n   int
}

func (d *demo) frame() {
	d.tt.Advance(time.Second / demoFPS)
	d.tt.Frame()
	d.n++
	f, err := os.Create(filepath.Join(d.dir, fmt.Sprintf("%04d.png", d.n)))
	if err != nil {
		d.t.Fatalf("create frame %d: %v", d.n, err)
	}
	defer f.Close()
	if err := png.Encode(f, d.tt.Image()); err != nil {
		d.t.Fatalf("encode frame %d: %v", d.n, err)
	}
}

// hold keeps the current screen for seconds.
func (d *demo) hold(seconds float64) {
	for i := int(seconds * demoFPS); i > 0; i-- {
		d.frame()
	}
}

// click presses the control showing or labeled s. The tour holds the frame
// before each action, so the tree is already settled; a Frame here would clear
// the label index the finder reads.
func (d *demo) click(s string) {
	if err := d.tt.Click(s); err != nil {
		d.t.Fatalf("click %s: %v", s, err)
	}
	d.tt.Frame()
}

// TestCaptureDemo renders the tour.
func TestCaptureDemo(t *testing.T) {
	root := captureDir(t)
	frames := filepath.Join(root, "demo")
	if err := os.RemoveAll(frames); err != nil {
		t.Fatalf("clear demo frames: %v", err)
	}
	if err := os.MkdirAll(frames, 0o755); err != nil {
		t.Fatalf("mkdir demo: %v", err)
	}
	registerFonts()

	app, db := openNativeTestApp(t)
	svc := service.New(db)
	work, notes, long := seedCaptures(t, svc)
	app.reloadNotebooks()
	app.reloadTags()
	app.selectedNotebook = work.ID
	app.selectedID = long.ID
	app.reloadNotes()
	app.showNote(long.ID)

	tt := ui.NewTester(app.view, 1280, 800)
	tt.SetSize(1280, 800)
	d := &demo{t: t, tt: tt, dir: frames}
	d.hold(0.2)

	// Existing notes start in preview; show the editor and return to reading.
	d.hold(1.2)
	tt.Key(ui.Cmd, ui.KeyE)
	d.hold(2.0)
	tt.Key(ui.Cmd, ui.KeyE)
	d.hold(0.5)

	// The whole library, and another note in it.
	d.click("Open notebooks")
	d.click("All notes")
	d.click("Close notebooks")
	d.hold(0.9)
	d.click("Meeting 12 Oct")
	d.hold(1.0)

	// Search the vault and clear it.
	d.click("Search notes")
	for _, chunk := range []string{"re", "adi", "ng"} {
		tt.Type(chunk)
		d.hold(0.25)
	}
	d.hold(1.1)
	d.click("Clear search")
	d.hold(0.4)

	// Write a note: body first (the new note focuses it), then its title.
	tt.Key(ui.Cmd, ui.KeyN)
	d.hold(0.4)
	tt.Type("The native shell is the product now.")
	d.hold(0.3)
	tt.Type("\nOne process, no Chromium, and the same vault ")
	d.hold(0.5)
	tt.Type("the clipper writes into.")
	d.hold(0.5)
	d.click("Note title")
	tt.Type("Why native")
	d.hold(0.9)
	tt.Key(ui.Cmd, ui.KeyE)
	d.hold(1.6)
	tt.Key(ui.Cmd, ui.KeyE)
	d.hold(0.4)

	// The tag menu, then settings.
	d.click("Tags")
	d.hold(1.3)
	d.click("Tags")
	d.click("Settings")
	d.hold(1.9)
	d.click("Cancel")
	d.hold(0.4)

	// The narrow shell: list, drawer, editor.
	tt.SetSize(900, 640)
	app.narrowEditor = false
	d.hold(1.0)
	app.railOpen = true
	d.hold(1.5)
	d.click("Close notebooks")
	app.narrowEditor = true
	d.hold(1.5)
	tt.SetSize(1280, 800)
	app.narrowEditor = false
	d.hold(0.6)

	// The status strip's states.
	app.selectedNotebook = notes.ID
	app.selectedID = long.ID
	app.reloadNotes()
	app.showNote(long.ID)
	st := &syncStatus{}
	app.be = &backend{status: st, cfg: &config.Config{SyncTarget: 7}}
	st.finish(nil)
	d.hold(1.3)
	st.start()
	d.hold(1.4)
	st.finish(fmt.Errorf("push: invalid credentials"))
	d.hold(1.6)
	app.be = nil
	app.selectedNotebook = work.ID
	app.reloadNotes()
	d.hold(1.0)

	t.Logf("demo: %d frames in %s (%.1fs at %d fps)", d.n, frames, float64(d.n)/demoFPS, demoFPS)
}
