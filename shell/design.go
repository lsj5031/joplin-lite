package main

import (
	_ "embed"
	"fmt"
	"log/slog"
	"time"

	"github.com/egoist/mygo/ui"
)

// The native shell draws DESIGN.md. This file is the one place its tokens
// live: the palette, the type scale, the spacing rhythm, the motion and the
// theme that carries them into MyGO's widgets, so a screen never picks a
// color or a size of its own.

//go:embed fonts/Overpass.ttf
var overpassTTF []byte

//go:embed fonts/OverpassMono.ttf
var overpassMonoTTF []byte

// Field Notes: sage binding, warm paper, green ink. Semantic colors remain
// distinct so warnings and failures are never merely decorative.
var (
	darkPalette = false

	paper       = ui.Hex("#F9FAF5") // warm paper
	paperSunken = ui.Hex("#E9EEE3") // sage binding
	paperRaised = ui.Hex("#F9FAF5") // a continuous reading surface
	inkNavy     = ui.Hex("#344238") // deep green ink
	inkSoft     = ui.Hex("#606F5A") // secondary text: at least 4.5:1 on sage and paper

	hairline       = ui.Hex("#DCE3D6")
	hairlineStrong = ui.Hex("#A8B69F")

	navyDeep = paperRaised
	navyLine = hairline
	navyText = inkSoft

	routeBlue     = ui.Hex("#566C4A") // primary actions and focus
	routeBlueSoft = routeBlue
	routeBlueDeep = ui.Hex("#485C3D")
	routeBlueDown = ui.Hex("#3C4E32")

	moss       = ui.Hex("#276B43") // synced
	mossBright = moss

	signalRed     = ui.Hex("#C8322B") // conflict and disruption
	signalRedSoft = signalRed

	amberInk    = ui.Hex("#8A5A12") // degraded, locked, waiting
	amberBright = amberInk

	violet = routeBlue // selection: rows, caret, text selection

	shadowPopover = ui.RGBA(15, 29, 45, 0.22) // the one shadow vocabulary entry
)

// applyPalette switches the Field Notes tokens between light and dark mode.
func applyPalette(dark bool) {
	darkPalette = dark
	if dark {
		paper = ui.Hex("#20271F")
		paperSunken = ui.Hex("#1A2119")
		paperRaised = ui.Hex("#20271F")
		inkNavy = ui.Hex("#E8ECDF")
		inkSoft = ui.Hex("#AFBAA5")
		hairline = ui.Hex("#364130")
		hairlineStrong = ui.Hex("#67765C")
		navyDeep = paperRaised
		navyLine = hairline
		navyText = inkSoft
		routeBlue = ui.Hex("#C1D2AD")
		routeBlueSoft = routeBlue
		routeBlueDeep = ui.Hex("#D1E0C0")
		routeBlueDown = ui.Hex("#A8BC93")
		moss = ui.Hex("#6BC48A")
		mossBright = moss
		signalRed = ui.Hex("#F06A63")
		signalRedSoft = signalRed
		amberInk = ui.Hex("#E5A94C")
		amberBright = amberInk
		violet = routeBlue
		shadowPopover = ui.RGBA(0, 0, 0, 0.45)
		return
	}
	paper = ui.Hex("#F9FAF5")
	paperSunken = ui.Hex("#E9EEE3")
	paperRaised = ui.Hex("#F9FAF5")
	inkNavy = ui.Hex("#344238")
	inkSoft = ui.Hex("#606F5A")
	hairline = ui.Hex("#DCE3D6")
	hairlineStrong = ui.Hex("#A8B69F")
	navyDeep = paperRaised
	navyLine = hairline
	navyText = inkSoft
	routeBlue = ui.Hex("#566C4A")
	routeBlueSoft = routeBlue
	routeBlueDeep = ui.Hex("#485C3D")
	routeBlueDown = ui.Hex("#3C4E32")
	moss = ui.Hex("#276B43")
	mossBright = moss
	signalRed = ui.Hex("#C8322B")
	signalRedSoft = signalRed
	amberInk = ui.Hex("#8A5A12")
	amberBright = amberInk
	violet = routeBlue
	shadowPopover = ui.RGBA(15, 29, 45, 0.22)
}

// Installed Humanist families with bundled Overpass fallbacks.
var (
	fontSans    = "Overpass"
	fontHeading = "Overpass"
)

const fontMono = "Overpass Mono"

// Spacious reading text, compact controls, and quiet sentence-case labels.
const (
	sizeDisplay = float32(36)
	sizeTitle   = float32(24)
	sizeBody    = float32(17)
	sizeUI      = float32(13)
	sizeSmall   = float32(12)
	sizeLabel   = float32(11)
	sizeMono    = float32(11)
	sizeMonoSm  = float32(10)
)

// Two panes by default; notebook management opens on demand.
const (
	railWidth          = float32(300)
	listWidth          = float32(300)
	topBarHeight       = float32(68)
	statusHeight       = float32(40)
	narrowStatusHeight = float32(40)
	narrowWidth        = float32(1024)
	rowHeight          = float32(56)
	contentPad         = float32(20)
	radiusControl      = float32(6)
	radiusSmall        = float32(4)
	radiusPanel        = float32(8)
)

// Motion runs only in response to interaction and respects reduced motion.
var (
	motionFade        = ui.Animation{Duration: 160 * time.Millisecond, Ease: ui.EaseOut}
	motionMove        = ui.Animation{Duration: 200 * time.Millisecond, Ease: ui.EaseOut}
	motionHover       = ui.Animation{Duration: 110 * time.Millisecond, Ease: ui.EaseOut}
	motionEnterFrames = []ui.Keyframe{
		{At: 0, Value: 0},
		{At: 170 * time.Millisecond, Value: 1, Ease: ui.EaseOut},
	}
)

// registerFonts adds the two vendored families. Called once at startup, before
// the first window; a failure is logged rather than fatal, because the widget
// toolkit falls back to the system family and the app still works.
func registerFonts() {
	if err := ui.RegisterFont(overpassTTF, "Overpass"); err != nil {
		slog.Warn("cannot register Overpass; falling back to the system font", "error", err)
	}
	if err := ui.RegisterFont(overpassMonoTTF, fontMono); err != nil {
		slog.Warn("cannot register Overpass Mono; falling back to the system font", "error", err)
	}
	// Use installed fonts without redistributing Apple's font files. Other
	// platforms retain the bundled family rather than relying on a silent fallback.
	for _, family := range ui.FontFamilies() {
		switch family.Name {
		case "Avenir Next":
			fontSans = family.Name
		case "Optima":
			fontHeading = family.Name
		}
	}
}

// designTheme carries Field Notes into MyGo's standard widgets.
func designTheme() *ui.Theme {
	t := ui.LightTheme()
	if darkPalette {
		t = ui.DarkTheme()
	}
	t.Background = paper
	t.Surface = paperRaised
	t.SurfaceHover = paperSunken
	t.SurfacePressed = hairline
	t.Border = hairline
	t.Text = inkNavy
	t.TextMuted = inkSoft
	t.Accent = violet
	t.AccentHover = routeBlueDeep
	t.AccentPressed = routeBlueDown
	t.AccentText = paper
	if darkPalette {
		t.AccentText = paperSunken
	}
	t.Dark = darkPalette
	t.Danger = signalRed
	t.Warning = amberInk
	t.Success = moss
	t.Selection = violet.Alpha(0.25)
	t.Focus = routeBlue.Alpha(0.6)
	t.Inverse = inkNavy
	t.InverseText = paper
	t.Scrollbar = hairlineStrong
	t.ScrollbarWidth = 6
	t.Radius = radiusControl
	t.Spacing = 4
	t.FontSize = sizeUI
	t.Font = fontSans
	return t
}

// listTheme keeps selection as a sage tint with readable ink text.
func listTheme() *ui.Theme {
	t := designTheme()
	t.Accent = paper.Mix(violet, 0.10)
	t.AccentHover = paper.Mix(violet, 0.14)
	t.AccentPressed = paper.Mix(violet, 0.18)
	t.AccentText = inkNavy
	return t
}

// actionTheme carries sage through primary controls and their hover states.
func actionTheme() *ui.Theme {
	t := designTheme()
	t.Accent = routeBlue
	t.AccentHover = routeBlueDeep
	t.AccentPressed = routeBlueDown
	t.AccentText = paper
	if darkPalette {
		t.AccentText = paperSunken
	}
	return t
}

// primaryAction is the sage-accented primary button. MyGO colors primary buttons
// from the accent, so the action theme goes on for the construction only.
func primaryAction(c *ui.Context, label string) ui.Element {
	return underTheme(c, actionTheme, func() ui.Element { return ui.PrimaryButton(c, label) })
}

// underTheme builds make with t as the theme, then puts the design theme back.
func underTheme(c *ui.Context, t func() *ui.Theme, make func() ui.Element) ui.Element {
	c.SetTheme(t())
	el := make()
	c.SetTheme(designTheme())
	return el
}

// dangerTheme is designTheme with signal red on the accent, for the destructive
// button of a confirmation that deletes something: destroying work is red,
// never the sage selection or primary action.
func dangerTheme() *ui.Theme {
	t := designTheme()
	t.Accent = signalRed
	t.AccentHover = ui.Hex("#AD2A24")
	t.AccentPressed = ui.Hex("#93231E")
	t.AccentText = ui.Hex("#F9FAF5")
	return t
}

// confirmDialog focuses Cancel so Enter cannot destroy work on opening.
// It returns 1 for the explicit action, 0 for Cancel, or -1 otherwise.
func confirmDialog(c *ui.Context, theme func() *ui.Theme, open *bool, title, message, action string) int {
	chosen := -1
	ui.Modal(c, open, func() {
		ui.Text(c, title).FontSize(sizeBody).Bold()
		ui.Text(c, message).MaxWidth(320).TextColor(inkSoft)
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, "Cancel").AutoFocus().Clicked() {
				chosen = 0
				*open = false
			}
			if underTheme(c, theme, func() ui.Element { return ui.PrimaryButton(c, action) }).Clicked() {
				chosen = 1
				*open = false
			}
		})
	})
	return chosen
}

// toolbarButton keeps the toolbar chrome of a pane compact.
func toolbarButton(c *ui.Context, label, tip string) ui.Element {
	btn := ui.Button(c, label).Tooltip(tip)
	btn.Padding(4, 8).MinWidth(26)
	return btn
}

// label renders a quiet 11px sentence-case section heading (DESIGN.md).
func label(c *ui.Context, text string) ui.Element {
	return labelOn(c, text, inkSoft)
}

func labelOn(c *ui.Context, text string, color ui.Color) ui.Element {
	return ui.Text(c, text).
		FontSize(sizeLabel).
		TextColor(color)
}

// hairlineRule is the 1px separator resting surfaces use instead of a shadow
// (the Hairline Rule). MyGO's Divider is one pixel of the theme's border color.
func hairlineRule(c *ui.Context) ui.Element {
	return ui.Divider(c)
}

// --- Transient surfaces -----------------------------------------------------

// surfaceMotions remembers which transient surfaces were open in the last
// frame, so a panel's entrance plays on every open and never while it sits
// there. Run is what KeyframeOptions.Run wants.
type surfaceMotions struct {
	prev map[string]bool
	run  map[string]uint64
}

func (s *surfaceMotions) opening(key string, open bool) uint64 {
	if s.prev == nil {
		s.prev, s.run = map[string]bool{}, map[string]uint64{}
	}
	if open && !s.prev[key] {
		s.run[key]++
	}
	s.prev[key] = open
	return s.run[key]
}

// panelIn gives a transient surface its entrance: the panel fades in and rises
// 8px, once, over 170ms. It is the only motion a panel has — no loop, nothing
// while it rests. run must change on each open (surfaceMotions.opening).
func panelIn(p ui.Element, key string, run uint64) ui.Element {
	k := "panel-in:" + key
	v := p.Keyframes(k, motionEnterFrames, ui.KeyframeOptions{Run: run})
	return p.Opacity(v).Translate(0, (1-v)*8)
}

// panelStyle is the one floating surface of the depth system: paper-raised
// over the chrome, an 8px radius, a hairline edge and the single popover
// shadow (DESIGN.md's Shadow Vocabulary).
func panelStyle(p ui.Element, width float32) ui.Element {
	p = p.Background(paperRaised).
		Border(1, hairline).
		Radius(radiusPanel).
		Shadow(0, 6, 18, 0, shadowPopover).
		Gap(2)
	if width > 0 {
		p = p.Width(width)
	}
	return p
}

// menuRow is a row of a menu: 13px, 6px radius, a sunken hover fill that fades
// in over 110ms. Hovering a menu is never route blue — that color is for the
// primary action and focus (the Service Color Rule) — and the chosen row keeps
// the violet selection tint.
func menuRow(c *ui.Context, key string, chosen bool) ui.Element {
	row := ui.Row(c.Key(key)).Padding(6, 10).Radius(radiusControl).Gap(6).AlignItems(ui.Center)
	base := ui.Color{}
	if chosen {
		base = violet.Alpha(0.12)
	} else if row.Hovered() {
		base = paperSunken
	}
	bg := row.AnimateColor("bg", base, motionHover)
	row.Background(bg)
	if chosen {
		row.TextColor(violet)
	}
	return row
}

// magnifier draws the search glyph: a ring and its handle, coords given by the
// box it is drawn in.
func magnifier(p *ui.Painter, r ui.Rect, c ui.Color) {
	s := min(r.W, r.H) * 0.62
	x, y := r.X+(r.W-s)/2-1, r.Y+(r.H-s)/2-1
	p.Stroke(ui.Rect{X: x, Y: y, W: s, H: s}, c, s/2, 1.5)
	p.Line(x+s*0.78, y+s*0.78, x+s+2, y+s+2, 1.5, c)
}

// noteCountLabel counts notes the machine way, noun included, so a number is
// never bare on the stacked narrow strip.
func noteCountLabel(n int, more bool) string {
	switch {
	case more:
		return fmt.Sprintf("%d+ notes", n)
	case n == 1:
		return "1 note"
	}
	return fmt.Sprintf("%d notes", n)
}

// notebookCountLabel counts notebooks, singular when there is one.
func notebookCountLabel(n int) string {
	if n == 1 {
		return "1 notebook"
	}
	return fmt.Sprintf("%d notebooks", n)
}

// fmtCount renders a count the machine way: mono, tabular, and with a "+"
// when the list is still paged.
func fmtCount(n int, more bool) string {
	if more {
		return fmt.Sprintf("%d+", n)
	}
	return fmt.Sprintf("%d", n)
}
