package main

import (
	"context"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/plugins/render"
	"github.com/egoist/mygo/ui"

	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/service"
)

// TestRenderScreens draws the real workspace through the render plugin, not
// the tester, and writes a 2x still for visual review. It is opt-in so the
// ordinary suite does not need a GPU, a media encoder or a large temp file.
//
//	RENDER_DIR=../tmp/render CGO_ENABLED=0 go test -run TestRenderScreens .
func TestRenderScreens(t *testing.T) {
	dir := renderCaptureDir(t)
	registerFonts()
	for _, dark := range []bool{false, true} {
		theme := "light"
		if dark {
			theme = "dark"
		}
		t.Run(theme, func(t *testing.T) {
			app := renderWorkspace(t, dark)
			comp := render.Composition{
				Width: 2560, Height: 1600, Scale: 2, Frames: 1,
				View: func(c *ui.Context, _ render.Frame) { app.view(c) },
			}
			img, err := render.Still(comp, 0, render.Options{Renderer: render.CPU})
			if err != nil {
				t.Fatalf("render still: %v", err)
			}
			if b := img.Bounds(); b.Dx() != 2560 || b.Dy() != 1600 {
				t.Fatalf("still is %v, want 2560x1600", b)
			}
			if n := sampledColors(img); n < 16 {
				t.Fatalf("still has only %d sampled colors; the view did not draw", n)
			}
			path := filepath.Join(dir, "workspace-"+theme+"-2x.png")
			f, err := os.Create(path)
			if err != nil {
				t.Fatalf("create still: %v", err)
			}
			if err := png.Encode(f, img); err != nil {
				_ = f.Close()
				t.Fatalf("encode still: %v", err)
			}
			if err := f.Close(); err != nil {
				t.Fatalf("close still: %v", err)
			}
			t.Logf("wrote %s", path)
		})
	}
}

// TestRenderFilm draws the same workspace into a short H.264 film with an
// animated caption, proving the video path and its system encoder. It shares
// RENDER_DIR with TestRenderScreens and skips where the platform has no
// encoder.
func TestRenderFilm(t *testing.T) {
	dir := renderCaptureDir(t)
	registerFonts()
	app := renderWorkspace(t, false)
	const (
		fps    = 30
		frames = 36
	)
	comp := render.Composition{
		Width: 1280, Height: 800, FPS: fps, Frames: frames, Scale: 1,
		View: func(c *ui.Context, f render.Frame) {
			app.view(c)
			// A quiet Field Notes strip: it rises in, holds, and leaves.
			in := f.Interpolate([]float64{0, 12}, []float64{0, 1})
			out := f.Interpolate([]float64{frames - 9, frames - 1}, []float64{1, 0})
			a := float32(min(in, out)) // The film plate stays dark in both palettes, so it does not
			// follow navyDeep (paper in the light palette).
			plate := ui.RGBA(16, 23, 18, 0.97)
			filmInk := ui.Hex("#F9FAF5")
			ui.Row(c).Absolute().Left(0).Right(0).Bottom(0).Height(72).
				Background(plate.Mix(ui.Transparent, 1-a)).
				AlignItems(ui.Center).Gap(12).Padding(0, 32).
				Translate(0, float32(18*(1-a))).Children(func() {
				ui.Text(c, "Joplin Lite").Font(fontHeading).FontSize(scaledText(c, 24)).
					TextColor(filmInk).Opacity(a)
				ui.Text(c, "A quiet, native home for your Joplin notes").
					FontSize(sizeSmall).TextColor(filmInk.Alpha(0.72)).Opacity(a)
			})
		},
	}
	path := filepath.Join(dir, "workspace-film.mp4")
	err := render.Video(context.Background(), comp, path, render.Options{Renderer: render.CPU})
	if errors.Is(err, errors.ErrUnsupported) {
		t.Skipf("no H.264 encoder on this platform: %v", err)
	}
	if err != nil {
		t.Fatalf("render film: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() < 4096 {
		t.Fatalf("film was not written: info=%v err=%v", info, err)
	}
	head, err := os.ReadFile(path)
	if err != nil || len(head) < 12 || string(head[4:8]) != "ftyp" {
		t.Fatalf("film is not an MP4: err=%v head=%q", err, head[:min(len(head), 12)])
	}
	t.Logf("wrote %s (%d bytes)", path, info.Size())
}

func renderCaptureDir(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("RENDER_DIR")
	if dir == "" {
		t.Skip("set RENDER_DIR to write rendered stills and film")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir RENDER_DIR: %v", err)
	}
	return dir
}

// renderWorkspace seeds the shapes a review still needs: a notebook tree, a
// tagged note in reading view, and a second note in the list.
func renderWorkspace(t *testing.T, dark bool) *nativeApp {
	t.Helper()
	app, db := openNativeTestApp(t)
	svc := service.New(db)
	notebook := &models.Folder{Title: "Field Notes"}
	if err := svc.CreateNotebook(notebook); err != nil {
		t.Fatalf("CreateNotebook: %v", err)
	}
	tag := &models.Tag{Title: "reading"}
	if err := svc.CreateTag(tag); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	note := addTestNote(t, svc, notebook.ID, "Along the waterfront", "The tide came in slowly, and the light held.\n\n- [ ] bring the field notebook\n- [x] charge the camera\n\nWe walked until the path turned into grass.\n")
	if err := svc.AddTag(note.ID, tag.ID); err != nil {
		t.Fatalf("AddTag: %v", err)
	}
	addTestNote(t, svc, notebook.ID, "Release checklist", "Ship the native build.")
	app.reloadNotebooks()
	app.reloadTags()
	app.selectedNotebook = notebook.ID
	app.reloadNotes()
	app.showNote(note.ID)
	app.editing = false
	app.railOpen = true
	app.dark = dark
	applyPalette(dark)
	t.Cleanup(func() {
		app.dark = false
		applyPalette(false)
	})
	return app
}

// sampledColors counts distinct colors on a coarse grid, enough to tell a
// rendered workspace from an empty frame without comparing pixels.
func sampledColors(img *image.RGBA) int {
	seen := make(map[uint32]struct{})
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y += 19 {
		for x := b.Min.X; x < b.Max.X; x += 23 {
			i := img.PixOffset(x, y)
			seen[uint32(img.Pix[i])<<24|uint32(img.Pix[i+1])<<16|uint32(img.Pix[i+2])<<8|uint32(img.Pix[i+3])] = struct{}{}
		}
	}
	return len(seen)
}
