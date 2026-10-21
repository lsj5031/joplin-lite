package main

import (
	_ "embed"
	"fmt"
	"log/slog"
	"time"

	"github.com/egoist/mygo/plugins/glass"
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
	activeWash  = "sage"

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

// Installed Humanist families with bundled Overpass fallbacks for reading and
// brand headings, while chrome and controls use the native OS UI font.
var (
	fontUI      = ""
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
	railWidth          = float32(260)
	railMinWidth       = float32(200)
	railMaxWidth       = float32(380)
	listWidth          = float32(300)
	listMinWidth       = float32(240)
	listMaxWidth       = float32(460)
	topBarHeight       = float32(56)
	statusHeight       = float32(34)
	narrowStatusHeight = float32(38)
	narrowWidth        = float32(1024)
	rowHeight          = float32(34)
	contentPad         = float32(20)
	cardGap            = float32(8)
	radiusControl      = float32(6)
	radiusSmall        = float32(4)
	radiusPanel        = float32(9)
	radiusCard         = float32(11)
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
	if windowsChrome {
		return
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
	if windowsChrome {
		t.FontSize = 14
	}
	t.Font = fontUI
	return t
}

// listTheme keeps ui.List's outer row wrapper transparent so noteRow's own
// chosenPill and bulk-selection styling control the visual state without a
// duplicate outer selection box.
func listTheme() *ui.Theme {
	t := designTheme()
	t.SurfaceHover = ui.Transparent
	t.Accent = ui.Transparent
	t.AccentHover = ui.Transparent
	t.AccentPressed = ui.Transparent
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

// washSpec defines a Kero-style multi-stop Oklab gradient wash and its corner glow.
type washSpec struct {
	light     []string
	lightGlow ui.Color
	dark      []string
	darkGlow  ui.Color
}

// washTable holds Oklab backdrop washes. The default "sage" wash harmonizes
// with Field Notes' botanical sage mark, forest-green ink, and warm cream
// paper, transitioning from misty sage-eucalyptus at the top through soft
// celadon and herbal linen into warm sunlit parchment at the bottom.
var washTable = map[string]washSpec{
	"sage": {
		light:     []string{"#d7e6d8", "#e3ede0", "#efefe2", "#f5efd6"},
		lightGlow: ui.RGBA(206, 228, 204, 0.80),
		dark:      []string{"#141d16", "#19221a", "#1f221a"},
		darkGlow:  ui.RGBA(86, 122, 82, 0.28),
	},
	"linen": {
		light:     []string{"#e5e2d7", "#edeae0", "#f4f1e6", "#f8f4e3"},
		lightGlow: ui.RGBA(238, 232, 214, 0.80),
		dark:      []string{"#191b17", "#1d1f1a", "#22211b"},
		darkGlow:  ui.RGBA(115, 112, 92, 0.24),
	},
	"mist": {
		light:     []string{"#d4e5e3", "#e2eeeb", "#efefe5", "#f5efe0"},
		lightGlow: ui.RGBA(204, 232, 228, 0.78),
		dark:      []string{"#131c1c", "#172120", "#1c211c"},
		darkGlow:  ui.RGBA(74, 124, 118, 0.26),
	},
	"kero": {
		light:     []string{"#ded8f3", "#ebe5f0", "#f6ede3", "#f6efcf"},
		lightGlow: ui.RGBA(247, 220, 241, 0.85),
		dark:      []string{"#171622", "#1a1720", "#1c1916"},
		darkGlow:  ui.RGBA(110, 75, 115, 0.28),
	},
}

// washOptions lists the user-selectable backdrop gradient presets in order.
var washOptions = []struct {
	id    string
	label string
}{
	{id: "sage", label: "Sage"},
	{id: "linen", label: "Linen"},
	{id: "mist", label: "Mist"},
	{id: "kero", label: "Kero"},
}

// washes returns the two Oklab linear gradients of a Kero-style backdrop: the
// vertical multi-stop wash and the diagonal top-right corner glow.
func washes(id string, dark bool) (base, glow ui.LinearGradient) {
	spec, ok := washTable[id]
	if !ok {
		spec = washTable["sage"]
	}
	hexes, g := spec.light, spec.lightGlow
	if dark {
		hexes, g = spec.dark, spec.darkGlow
	}
	stops := make([]ui.GradientStop, len(hexes))
	for i, h := range hexes {
		at := float32(0)
		if len(hexes) > 1 {
			at = float32(i) / float32(len(hexes)-1)
		}
		stops[i] = ui.GradientStop{Color: ui.Hex(h), At: at}
	}
	base = ui.LinearGradient{Angle: 180, Oklab: true, Stops: stops}
	glow = ui.LinearGradient{
		Angle: 225,
		Oklab: true,
		Stops: []ui.GradientStop{
			{Color: g, At: 0},
			{Color: g.Alpha(0), At: 0.55},
		},
	}
	return base, glow
}

// backdrop paints the two-layer Oklab gradient wash on the window root and the
// workspace container, letting macOS window vibrancy breathe through when active.
func backdrop(c *ui.Context, root ui.Element) {
	id := activeWash
	if id == "" {
		id = "sage"
	}
	base, glow := washes(id, darkPalette)
	if c.Vibrancy() {
		for i := range base.Stops {
			base.Stops[i].Color = base.Stops[i].Color.Alpha(0.95)
		}
	}
	c.Root().LinearGradient(base)
	root.LinearGradient(glow)
}

// iconButton renders a quiet borderless 28×28 native icon button that reveals
// a refractive Liquid Glass lens on hover/press while keeping an accessible
// Label for tests and screen readers.
func iconButton(c *ui.Context, key string, icon *ui.SVG, label, tip string) ui.Element {
	btn := ui.ButtonBase(c.Key(key)).
		Size(28, 28).
		Radius(14).
		AlignItems(ui.Center).
		Justify(ui.Center).
		Label(label).
		Tooltip(tip)
	if btn.Pressed() || btn.Hovered() {
		tint := routeBlueSoft.Alpha(0.18)
		if btn.Pressed() {
			tint = routeBlue.Alpha(0.26)
		}
		btn.Background(tint).Material(glass.Glass{
			Style:       glass.Regular,
			Tint:        tint,
			Interactive: true,
		})
	}
	fg := inkSoft
	if btn.Hovered() || btn.Pressed() {
		fg = inkNavy
	}
	btn.Children(func() {
		ui.Icon(c, icon).Size(15, 15).TextColor(fg)
	})
	return btn
}

// toolbarIconButton renders a compact capsule button with an embedded 14×14 SVG
// icon, visible text label, and interactive Liquid Glass optics (specular rim,
// edge refraction, and press expansion).
func toolbarIconButton(c *ui.Context, key string, icon *ui.SVG, text, label, tip string) ui.Element {
	btn := ui.ButtonBase(c.Key(key)).
		Height(28).
		Padding(4, 11).
		Gap(5).
		Radius(14).
		AlignItems(ui.Center).
		Label(label).
		Tooltip(tip)
	base := routeBlueSoft.Alpha(0.16)
	if btn.Pressed() {
		base = routeBlue.Alpha(0.28)
	} else if btn.Hovered() {
		base = routeBlueSoft.Alpha(0.26)
	}
	tint := btn.AnimateColor("bg", base, motionHover)
	btn.Background(tint).Material(glass.Glass{
		Style:       glass.Regular,
		Tint:        tint,
		Interactive: true,
	})
	btn.Children(func() {
		ui.Icon(c, icon).Size(13, 13).TextColor(inkSoft)
		ui.Text(c, text).FontSize(sizeSmall).TextColor(inkNavy).SingleLine()
	})
	return btn
}

// elevateCard styles the floating primary workspace card with a subtle border
// (or top-lit linear gradient in dark mode) and soft ambient shadow, matching
// Kero's raised card treatment over the Oklab gradient backdrop.
func elevateCard(p ui.Element) ui.Element {
	fill := paperRaised.Alpha(0.92)
	if darkPalette {
		fill = paperRaised.Alpha(0.90)
	}
	if windowsChrome {
		fill = paperRaised
	}
	p = p.Background(fill).Radius(radiusCard).Clip()
	if darkPalette {
		return p.Border(1, hairline).
			BorderLinearGradient(ui.LinearGradient{
				From:  ui.Hex("#FFFFFF").Alpha(0.09),
				To:    hairline.Alpha(0.85),
				Angle: 180,
			}).
			Shadow(0, 4, 16, 0, ui.RGBA(0, 0, 0, 0.34))
	}
	return p.Border(1, ui.RGBA(15, 29, 20, 0.08)).
		Shadow(0, 1, 2, 0, ui.RGBA(15, 29, 20, 0.05)).
		Shadow(0, 4, 14, 0, ui.RGBA(15, 29, 20, 0.06))
}

// elevateSurface styles transient popovers, menus, and floating palettes with
// Liquid Glass refraction, backdrop blur, dual-layer shadows in light mode,
// and a top-lit border highlight in dark mode.
func elevateSurface(p ui.Element, r float32) ui.Element {
	tint := paperRaised.Alpha(0.28)
	if darkPalette {
		tint = paperRaised.Alpha(0.22)
	}
	p = p.Background(tint).Radius(r).Material(glass.Glass{
		Style: glass.Regular,
		Tint:  tint,
	})
	if darkPalette {
		return p.Border(1, hairline).
			BorderLinearGradient(ui.LinearGradient{
				From:  ui.Hex("#FFFFFF").Alpha(0.14),
				To:    hairline.Alpha(0.9),
				Angle: 180,
			}).
			Shadow(0, 8, 24, 0, shadowPopover)
	}
	return p.Shadow(0, 2, 5, 0, ui.RGBA(15, 29, 20, 0.06)).
		Shadow(0, 8, 22, 0, shadowPopover)
}

// chosenPill styles a sidebar row as a high-contrast rounded Liquid Glass pill
// on top of the gradient wash so the active notebook, tag, or note is unmistakable.
func chosenPill(row ui.Element, chosen, hovered bool) ui.Element {
	if chosen {
		tint := violet.Alpha(0.24)
		if darkPalette {
			tint = violet.Alpha(0.28)
		}
		row.Background(tint).
			Material(glass.Glass{
				Style: glass.Regular,
				Tint:  tint,
			}).
			Border(1, violet.Alpha(0.38))
		return row
	}
	base := ui.Color{}
	if hovered {
		if darkPalette {
			base = ui.RGBA(255, 255, 255, 0.06)
		} else {
			base = ui.RGBA(15, 29, 20, 0.055)
		}
	}
	row.Background(row.AnimateColor("bg", base, motionHover))
	return row
}

// segmentedControl renders a sunken pill track whose selected segment uses a
// high-contrast accent fill and bold text so the active option stands out clearly.
func segmentedControl(c *ui.Context, groupLabel string, selected *int, labels ...string) bool {
	changed := false
	track := ui.Row(c.Key("seg-track:"+groupLabel)).FillWidth().AlignItems(ui.Stretch).
		Padding(3).Gap(4).Radius(radiusControl+2).
		Background(paperSunken).Border(1, hairline).Label(groupLabel)
	track.Children(func() {
		for i, text := range labels {
			i, text := i, text
			active := selected != nil && i == *selected
			seg := ui.ButtonBase(c.Key("seg:"+groupLabel+":"+text)).
				Label(text).
				Grow(1).
				Justify(ui.Center).
				AlignItems(ui.Center).
				Padding(5, 10).
				Radius(radiusControl)
			if active {
				seg.Background(violet).Border(1, violet).
					Shadow(0, 1, 3, 0, ui.RGBA(0, 0, 0, 0.18))
			} else if seg.Hovered() {
				seg.Background(paperRaised.Alpha(0.65))
			}
			if seg.Clicked() && selected != nil && *selected != i {
				*selected = i
				changed = true
			}
			seg.Children(func() {
				lbl := ui.Text(c, text).FontSize(sizeSmall).SingleLine()
				if active {
					lbl.FontWeight(600).TextColor(paperRaised)
				} else if seg.Hovered() {
					lbl.TextColor(inkNavy)
				} else {
					lbl.TextColor(inkSoft)
				}
			})
		}
	})
	return changed
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
// over the chrome, a 9px radius, a hairline/top-lit edge and the popover
// shadow (DESIGN.md's Shadow Vocabulary).
func panelStyle(p ui.Element, width float32) ui.Element {
	p = elevateSurface(p, radiusPanel).Gap(2)
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
