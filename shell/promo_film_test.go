package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	xdraw "golang.org/x/image/draw"

	"github.com/egoist/mygo/plugins/render"
	"github.com/egoist/mygo/ui"
)

// The product film. TestCapturePromo renders the tour at 2x and writes the
// beat and cursor telemetry; this cuts those captures into the 1920x1080 film
// with MyGO's render plugin — the same views the app draws, encoded by the
// system's own encoder — so no browser takes part.
//
//	CAPTURE_DIR=../tmp/promo go test -run TestCapturePromo ./shell/
//	go test -run TestRenderPromoFilm ./shell/   # -> docs/promo/*.mp4, poster
//
// PROMO_DIR selects the captures and PROMO_OUT the film's directory.
const (
	filmW, filmH = 1920, 1080
	filmFPS      = 30

	// The captured window is drawn 1:1 inside a rounded card, which leaves the
	// caption column on the left, as the editorial cut had it.
	stageX, stageY = 456, 100
	stageW, stageH = 1408, 880

	captionX, captionW = 64, 344
)

// The film's colors, mixed from the Field Notes light and dark palettes as the
// cut crosses into the night scene.
type filmRGB struct{ r, g, b float64 }

func (c filmRGB) color(alpha float64) ui.Color {
	channel := func(v float64) uint8 { return uint8(math.Round(clamp01(v) * 255)) }
	return ui.RGBA(channel(c.r), channel(c.g), channel(c.b), float32(clamp01(alpha)))
}

func (c filmRGB) mix(other filmRGB, u float64) filmRGB {
	u = clamp01(u)
	return filmRGB{
		r: c.r + (other.r-c.r)*u,
		g: c.g + (other.g-c.g)*u,
		b: c.b + (other.b-c.b)*u,
	}
}

var (
	filmInk       = filmRGB{0x2E / 255.0, 0x3D / 255.0, 0x32 / 255.0}
	filmInkSoft   = filmRGB{0x5D / 255.0, 0x6C / 255.0, 0x57 / 255.0}
	filmAccent    = filmRGB{0x56 / 255.0, 0x6C / 255.0, 0x4A / 255.0}
	filmNightInk  = filmRGB{0xE8 / 255.0, 0xEC / 255.0, 0xDF / 255.0}
	filmNightSoft = filmRGB{0xAF / 255.0, 0xBA / 255.0, 0xA5 / 255.0}
	filmNightAcc  = filmRGB{0xC1 / 255.0, 0xD2 / 255.0, 0xAD / 255.0}
	filmPaperFrom = filmRGB{0xF8 / 255.0, 0xFA / 255.0, 0xF3 / 255.0}
	filmPaperTo   = filmRGB{0xE6 / 255.0, 0xEC / 255.0, 0xE0 / 255.0}
	filmNightFrom = filmRGB{0x2A / 255.0, 0x37 / 255.0, 0x2E / 255.0}
	filmNightTo   = filmRGB{0x1C / 255.0, 0x28 / 255.0, 0x21 / 255.0}
)

// The folded-ribbon J and the film's pointer, as the cut drew them.
const filmMarkSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="64" height="64" viewBox="0 0 64 64">` +
	`<path d="M22 8h28L38 20H22z" fill="#566C4A" opacity=".36"/>` +
	`<path d="M38 20 50 8v35H38z" fill="#566C4A" opacity=".74"/>` +
	`<path d="M14 43h36L38 55H26z" fill="#566C4A"/>` +
	`<path d="M26 31v12H14z" fill="#566C4A" opacity=".56"/>` +
	`<path d="M14 31h12L14 43z" fill="#566C4A" opacity=".68"/></svg>`

const filmPointerSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="23" height="36" viewBox="0 0 23 36">` +
	`<path d="M0 0 L0 31 L7.8 23.8 L13.5 35.5 L18.2 33.2 L12.5 21.6 L22.5 21.6 Z"` +
	` fill="#1B241D" stroke="#FFFFFF" stroke-width="1.8" stroke-linejoin="round"/></svg>`

const filmClickSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="48" height="48" viewBox="0 0 48 48">` +
	`<circle cx="24" cy="24" r="21" fill="#566C4A" fill-opacity=".22"` +
	` stroke="#566C4A" stroke-opacity=".55" stroke-width="2.5"/></svg>`

// filmScene is one beat of the cut: a card, a captured beat with a caption
// column, or the closing card.
type filmScene struct {
	kind   string // "hook", "app" or "end"
	beat   string
	dur    float64
	kicker string
	cap    string
	sub    string
	dark   bool
}

// film is the composition's state: the cut's clock, the captures, and the few
// frames decoded at a time. The render plugin builds every frame of a
// composition on its own, so the view reads the frame's time and keeps nothing
// from frame to frame but the decode cache, which is why it runs one worker.
type film struct {
	dir      string
	manifest promoManifest
	beats    map[string]promoBeat
	scenes   []filmScene
	starts   []float64
	total    float64
	darkFrom int

	err error
}

func (f *film) locate(t float64) (int, filmScene, float64) {
	i := len(f.scenes) - 1
	for k := range f.scenes {
		if t < f.starts[k]+f.scenes[k].dur-1e-6 {
			i = k
			break
		}
	}
	return i, f.scenes[i], math.Max(0, t-f.starts[i])
}

// frame returns the captured frame n, decoded and scaled to the card. It reads
// the frame it builds and keeps nothing, so the frames do not depend on the
// order they are built in, and holding all 660 never happens.
func (f *film) frame(n int) ui.ImageSource {
	data, err := os.ReadFile(filepath.Join(f.dir, "frames", fmt.Sprintf("%04d.png", n)))
	if err != nil {
		f.err = err
		return nil
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		f.err = err
		return nil
	}
	scaled := image.NewRGBA(image.Rect(0, 0, stageW, stageH))
	xdraw.ApproxBiLinear.Scale(scaled, scaled.Bounds(), img, img.Bounds(), xdraw.Src, nil)
	return ui.NewBitmap(scaled)
}

// view builds one frame of the film from the frame's time.
func (f *film) view(c *ui.Context, fr render.Frame) {
	t := fr.Seconds()
	i, scene, local := f.locate(t)

	night := 0.0
	if scene.dark {
		if i > 0 && f.scenes[i-1].dark {
			night = 1
		} else {
			night = filmEaseOut(local / 0.45)
		}
	}

	ui.Overlay(c, func() {
		ui.Column(c).Fill().LinearGradient(ui.LinearGradient{
			From:  filmPaperFrom.color(1),
			To:    filmPaperTo.color(1),
			Angle: 163,
		})
		if night > 0.01 {
			ui.Column(c).Absolute().Fill().LinearGradient(ui.LinearGradient{
				From:  filmNightFrom.color(1),
				To:    filmNightTo.color(1),
				Angle: 163,
			}).Opacity(float32(night))
		}

		if scene.kind == "app" {
			f.appScene(c, scene, local, night)
		}
		switch scene.kind {
		case "hook":
			f.hookCard(c, local)
		case "end":
			f.endCard(c, local)
		default:
			f.caption(c, scene, local, night)
		}
	})
}

func (f *film) appScene(c *ui.Context, scene filmScene, local, night float64) {
	beat, ok := f.beats[scene.beat]
	if !ok {
		f.err = fmt.Errorf("promo: no capture for beat %q", scene.beat)
		return
	}
	n := beat.From + int(math.Floor(local*float64(f.manifest.FPS)+1e-5))
	n = clampInt(n, beat.From, beat.To)
	src := f.frame(n)
	if src == nil {
		return
	}

	// The card enters with the first app scene and leaves with the end card.
	alpha, lift := 1.0, 0.0
	if local < 0.55 && scene.beat == f.scenes[1].beat {
		enter := filmEaseOut(local / 0.50)
		alpha, lift = enter, (1-enter)*14
	}
	if alpha <= 0.01 {
		return
	}

	shadow := ui.RGBA(0x24, 0x30, 0x24, 0.32)
	border := ui.RGBA(0x34, 0x42, 0x38, 0.14)
	if night > 0.02 {
		shadow = ui.RGBA(0, 0, 0, 0.64)
		border = ui.RGBA(0xE8, 0xEC, 0xDF, 0.12)
	}
	ui.Column(c).Absolute().Left(stageX).Top(stageY).
		Width(stageW).Height(stageH).Radius(16).
		Shadow(0, 26, 64, 0, shadow).
		Border(1, border).
		Opacity(float32(alpha)).Translate(0, float32(lift)).
		Children(func() {
			ui.Image(c, src).Width(stageW).Height(stageH).Radius(16)
		})

	pointer, _ := ui.ParseSVG([]byte(filmPointerSVG))
	ring, _ := ui.ParseSVG([]byte(filmClickSVG))
	if n-1 >= 0 && n-1 < len(f.manifest.Cursors) {
		cur := f.manifest.Cursors[n-1]
		if cur.Opacity > 0.02 {
			x := float32(stageX + cur.X*(float64(stageW)/float64(f.manifest.Pixels.W)))
			y := float32(stageY + cur.Y*(float64(stageH)/float64(f.manifest.Pixels.H)))
			if cur.Down && ring != nil {
				ui.Image(c, ring).Absolute().Left(x - 24).Top(y - 24).Opacity(float32(cur.Opacity))
			}
			if pointer != nil {
				ui.Image(c, pointer).Absolute().Left(x).Top(y).Opacity(float32(cur.Opacity))
			}
		}
	}
}

func (f *film) caption(c *ui.Context, scene filmScene, local, night float64) {
	const d = 0.08
	k := filmEaseOut((local - d) / 0.36)
	r := filmEaseOut((local - d - 0.04) / 0.44)
	l := filmEaseOut((local - d - 0.08) / 0.52)
	s := filmEaseOut((local - d - 0.14) / 0.56)
	out := 1 - filmEaseOut((local-(scene.dur-0.28))/0.22)

	ink := filmInk.mix(filmNightInk, night)
	soft := filmInkSoft.mix(filmNightSoft, night)
	accent := filmAccent.mix(filmNightAcc, night)

	ui.Column(c).Absolute().Left(captionX).Top(0).Width(captionW).Height(filmH).
		Justify(ui.Center).Gap(0).Children(func() {
		ui.Text(c, scene.kicker).Font(fontMono).FontSize(12).
			LetterSpacing(1.9).Bold().
			TextColor(soft.color(clamp01(k * out)))
		ui.Row(c).Width(34).Height(1).Margin(12, 0, 16, 0).
			Background(accent.color(clamp01(r * out)))
		ui.Text(c, scene.cap).Font(fontHeading).FontSize(40).FontWeight(600).
			LetterSpacing(-0.6).LineHeight(1.14).
			TextColor(ink.color(clamp01(l*out))).
			Translate(0, float32((1-l)*12))
		ui.Text(c, scene.sub).Font(fontSans).FontSize(16).LineHeight(1.45).
			MaxWidth(captionW).
			TextColor(soft.color(clamp01(s*out))).
			Translate(0, float32((1-s)*10))
	})
}

func (f *film) hookCard(c *ui.Context, local float64) {
	out := 1 - filmEaseOut((local-(2.1-0.28))/0.24)
	step := func(delay float64) float64 { return filmEaseOut((local - delay) / 0.52) }
	mk, rule, a, b, sub := step(0.06), step(0.18), step(0.24), step(0.34), step(0.48)

	mark, _ := ui.ParseSVG([]byte(filmMarkSVG))
	ui.Column(c).Absolute().Left(160).Top(0).Width(filmW - 320).Height(filmH).
		Justify(ui.Center).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(18).
			Opacity(float32(clamp01(mk*out))).
			Translate(0, float32((1-mk)*10)).
			Children(func() {
				if mark != nil {
					ui.Image(c, mark).Width(54).Height(54)
				}
				ui.Text(c, "joplin-lite").Font(fontHeading).FontSize(26).TextColor(filmInk.color(1))
			})
		ui.Row(c).Width(64).Height(1).Margin(34, 0, 30, 0).
			Background(filmAccent.color(clamp01(0.34 * rule * out)))
		ui.Text(c, "Joplin notes.").Font(fontHeading).FontSize(84).
			LineHeight(1.06).TextColor(filmInk.color(1)).
			Opacity(float32(clamp01(a*out))).Translate(0, float32((1-a)*20))
		ui.Text(c, "No browser inside.").Font(fontHeading).FontSize(84).
			LineHeight(1.06).TextColor(filmInk.color(1)).
			Opacity(float32(clamp01(b*out))).Translate(0, float32((1-b)*20))
		ui.Text(c, "one native process · ~120 MB RAM · your library, unchanged").
			Font(fontMono).FontSize(15).LineHeight(1.6).Margin(30, 0, 0, 0).
			TextColor(filmInkSoft.color(1)).
			Opacity(float32(clamp01(sub*out))).Translate(0, float32((1-sub)*12))
	})
}

func (f *film) endCard(c *ui.Context, local float64) {
	step := func(delay float64) float64 { return filmEaseOut((local - delay) / 0.52) }
	mk, tag, rule, meta, url, lic := step(0.18), step(0.30), step(0.40), step(0.48), step(0.56), step(0.66)

	mark, _ := ui.ParseSVG([]byte(filmMarkSVG))
	ui.Column(c).Absolute().Left(160).Top(0).Width(filmW - 320).Height(filmH).
		Justify(ui.Center).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(18).
			Opacity(float32(clamp01(mk))).
			Translate(0, float32((1-mk)*14)).
			Children(func() {
				if mark != nil {
					ui.Image(c, mark).Width(60).Height(60)
				}
				ui.Text(c, "Joplin Lite").Font(fontHeading).FontSize(40).
					TextColor(filmNightInk.color(1))
			})
		ui.Text(c, "A quiet, native home for your Joplin notes.").Font(fontSans).
			FontSize(30).LineHeight(1.35).Margin(28, 0, 0, 0).
			TextColor(filmNightSoft.color(clamp01(tag))).
			Translate(0, float32((1-tag)*16))
		ui.Row(c).Width(640).Height(1).Margin(30, 0, 26, 0).
			Background(filmNightAcc.color(clamp01(0.22 * rule)))
		ui.Text(c, "v0.2.0 · macOS & Windows").Font(fontMono).FontSize(16).LineHeight(1.9).
			TextColor(filmNightSoft.color(clamp01(meta))).
			Translate(0, float32((1-meta)*10))
		ui.Text(c, "github.com/lsj5031/joplin-lite").Font(fontMono).FontSize(16).LineHeight(1.9).
			TextColor(filmNightAcc.color(clamp01(url))).
			Translate(0, float32((1-url)*10))
		ui.Text(c, "AGPL-3.0 · independent Joplin client").Font(fontMono).FontSize(13).
			LineHeight(1.9).Margin(26, 0, 0, 0).
			TextColor(ui.RGBA(0x8A, 0x96, 0x82, 1)).
			Opacity(float32(clamp01(lic))).Translate(0, float32((1-lic)*8))
	})
}

// cut fills the scenes' durations from the capture's beat spans, so every
// beat plays 1:1 at 30 fps with no time-warping or jump cuts.
func (f *film) cut() {
	f.scenes = []filmScene{
		{kind: "hook", dur: 2.1},
		{kind: "app", beat: "library", kicker: "your library",
			cap: "Notebooks, tags, and every note.",
			sub: "Liquid Glass sidebar rail with live counts and instant filtering."},
		{kind: "app", beat: "write", kicker: "writing",
			cap: "Markdown in, a page out.",
			sub: "Write in plain Markdown and toggle into a typeset Field Notes page."},
		{kind: "app", beat: "palette", kicker: "⌘K · ⌘P",
			cap: "Every command, one keystroke.",
			sub: "Jump to any note or action from the keyboard."},
		{kind: "app", beat: "night", dark: true, kicker: "light · dark",
			cap: "Then the lights go down.",
			sub: "Warm daylight paper and deep forest night ink."},
		{kind: "end", dur: 2.5, dark: true},
	}
	t := 0.0
	f.starts = make([]float64, len(f.scenes))
	for i := range f.scenes {
		s := &f.scenes[i]
		if s.kind == "app" {
			beat, ok := f.beats[s.beat]
			if !ok {
				continue
			}
			s.dur = float64(beat.To-beat.From+1) / float64(f.manifest.FPS)
		}
		f.starts[i] = t
		t += s.dur
	}
	f.total = t
}

// TestRenderPromoFilm cuts the captured tour into the product film and its
// poster, with MyGO's render plugin.
func TestRenderPromoFilm(t *testing.T) {
	dir := os.Getenv("PROMO_DIR")
	if dir == "" {
		dir = filepath.Join("..", "tmp", "promo")
	}
	out := os.Getenv("PROMO_OUT")
	if out == "" {
		out = filepath.Join("..", "docs", "promo")
	}
	data, err := os.ReadFile(filepath.Join(dir, "promo.json"))
	if err != nil {
		t.Skipf("no capture in %s; run TestCapturePromo first: %v", dir, err)
	}
	registerFonts()

	f := &film{dir: dir, beats: map[string]promoBeat{}}
	if err := json.Unmarshal(data, &f.manifest); err != nil {
		t.Fatalf("promo.json: %v", err)
	}
	if f.manifest.FPS == 0 {
		f.manifest.FPS = promoFPS
	}
	for _, b := range f.manifest.Beats {
		f.beats[b.Name] = b
	}
	f.cut()
	if len(f.beats) < 4 {
		t.Fatalf("capture has %d beats, want the tour's four", len(f.beats))
	}

	frames := int(math.Round(f.total * filmFPS))
	comp := render.Composition{
		Width: filmW, Height: filmH, FPS: filmFPS, Frames: frames,
		View: f.view,
	}

	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", out, err)
	}
	mp4 := filepath.Join(out, "joplin-lite-promo.mp4")
	t.Logf("film: %.2fs, %d frames -> %s", f.total, frames, mp4)
	progress := func(p render.Progress) {
		if p.Frame%150 == 0 {
			t.Logf("  %d/%d frames", p.Frame, frames)
		}
	}
	err = render.Video(context.Background(), comp, mp4, render.Options{Progress: progress})
	if err != nil {
		// A headless session (a CI runner, a sandboxed test) has no system
		// media service to encode with; ffmpeg is the renderer's fallback.
		t.Logf("system encoder: %v; encoding with ffmpeg", err)
		err = render.Video(context.Background(), comp, mp4, render.Options{
			Encoder: render.FFmpeg, Progress: progress,
		})
	}
	if err != nil {
		t.Fatalf("render film: %v", err)
	}
	if f.err != nil {
		t.Fatalf("film view: %v", f.err)
	}

	// The poster is the library beat, a little after its card has entered.
	poster := f.starts[1] + f.scenes[1].dur*0.45
	img, err := render.Still(comp, int(math.Round(poster*filmFPS)), render.Options{})
	if err != nil {
		t.Fatalf("render poster: %v", err)
	}
	posterPath := filepath.Join(out, "joplin-lite-promo-poster.png")
	file, err := os.Create(posterPath)
	if err != nil {
		t.Fatalf("create poster: %v", err)
	}
	if err := png.Encode(file, img); err != nil {
		file.Close()
		t.Fatalf("encode poster: %v", err)
	}
	file.Close()
	if f.err != nil {
		t.Fatalf("poster view: %v", f.err)
	}
	t.Logf("poster: %s", posterPath)
}

func filmEaseOut(u float64) float64 {
	u = clamp01(u)
	return 1 - math.Pow(1-u, 3)
}
