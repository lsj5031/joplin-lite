package main

import (
	_ "embed"
	"fmt"
	"log/slog"
	"math"
	"strings"
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

// The palette. Line colors carry state and only state (the Service Color
// Rule); every state color has a paper-safe and a navy-safe value (the
// Two-Value Rule), so chrome text never uses the dark value and vice versa.
var (
	paper       = ui.Hex("#F6F4EE") // the ground everything sits on
	paperSunken = ui.Hex("#EDE8DD") // sidebar rail, hover fills, code chips
	paperRaised = ui.Hex("#FBFAF6") // the editor surface, one step brighter
	inkNavy     = ui.Hex("#16283C") // body text and the chrome bars
	inkSoft     = ui.Hex("#4A5A6E") // secondary text: meta, counts, hints

	hairline       = ui.Hex("#D9D2C4") // row rules, borders, control strokes
	hairlineStrong = ui.Hex("#BEB5A3")

	navyDeep = ui.Hex("#0F1D2D") // chrome interiors
	navyLine = ui.Hex("#33455C") // chrome borders
	navyText = ui.Hex("#A8B4C4") // chrome secondary text

	routeBlue     = ui.Hex("#1D5FA8") // primary actions, focus, syncing
	routeBlueSoft = ui.Hex("#9CC1E8") // the syncing line on navy chrome
	routeBlueDeep = ui.Hex("#1A5490")
	routeBlueDown = ui.Hex("#164878")

	moss       = ui.Hex("#276B43") // synced
	mossBright = ui.Hex("#3E9B63")

	signalRed     = ui.Hex("#C8322B") // conflict and disruption
	signalRedSoft = ui.Hex("#E05A52")

	amberInk    = ui.Hex("#8A5A12") // degraded, locked, waiting
	amberBright = ui.Hex("#C9821B")

	violet = ui.Hex("#6B4E9B") // selection: rows, caret, text selection

	shadowPopover = ui.RGBA(15, 29, 45, 0.22) // the one shadow vocabulary entry
)

// Fonts. The web build self-hosts the same two families under
// web/public/fonts; the native shell registers the TrueType originals.
const (
	fontSans = "Overpass"
	fontMono = "Overpass Mono"
)

// The type scale: display 24, title 20, body 15, UI 13, meta 11-12, label 11
// uppercase with +0.08em tracking, mono 10-11 with tabular figures.
const (
	sizeDisplay = float32(24)
	sizeTitle   = float32(20)
	sizeBody    = float32(15)
	sizeUI      = float32(13)
	sizeSmall   = float32(12)
	sizeLabel   = float32(11)
	sizeMono    = float32(11)
	sizeMonoSm  = float32(10)
)

// The layout metrics of DESIGN.md: the three-pane grid, the chrome bars, and
// the 4px spacing rhythm (4/8/12/16/24).
const (
	railWidth          = float32(248)
	listWidth          = float32(360)
	topBarHeight       = float32(56)
	statusHeight       = float32(56)
	narrowStatusHeight = float32(96)
	narrowWidth        = float32(1024)
	rowHeight          = float32(40)
	contentPad         = float32(20)
	radiusControl      = float32(6)
	radiusSmall        = float32(4)
	radiusPanel        = float32(8)
)

// Motion. Nothing moves while the app is idle: each of these is triggered by
// an interaction, and only the sync line keeps drawing frames, and only while
// a pass runs. Values follow the toolkit's reduced-motion preference.
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
	if err := ui.RegisterFont(overpassTTF, fontSans); err != nil {
		slog.Warn("cannot register Overpass; falling back to the system font", "error", err)
	}
	if err := ui.RegisterFont(overpassMonoTTF, fontMono); err != nil {
		slog.Warn("cannot register Overpass Mono; falling back to the system font", "error", err)
	}
}

// designTheme is DESIGN.md as MyGO's widgets read it. Accent is violet because
// the toolkit uses it for what DESIGN.md reserves for selection — chosen rows,
// chosen nav items, the caret and text selection — and route blue, which
// DESIGN.md allows only on primary actions and focus, is applied by
// primaryAction and Focus.
func designTheme() *ui.Theme {
	t := ui.LightTheme()
	t.Background = paper
	t.Surface = paperRaised
	t.SurfaceHover = paperSunken
	t.SurfacePressed = hairline
	t.Border = hairline
	t.Text = inkNavy
	t.TextMuted = inkSoft
	t.Accent = violet
	t.AccentHover = ui.Hex("#5D4388")
	t.AccentPressed = ui.Hex("#4F396F")
	t.AccentText = paper
	t.Dark = false
	t.Danger = signalRed
	t.Warning = amberInk
	t.Success = moss
	t.Selection = violet.Alpha(0.25)
	t.Focus = routeBlue.Alpha(0.6)
	t.Inverse = navyDeep
	t.InverseText = paper
	t.Scrollbar = hairlineStrong
	t.ScrollbarWidth = 6
	t.Radius = radiusControl
	t.Spacing = 4
	t.FontSize = sizeUI
	t.Font = fontSans
	return t
}

// listTheme is designTheme with DESIGN.md's chosen row on the accent: violet
// 10% over paper with ink text. MyGO's List paints the rows it carries in the
// accent color, and a timetable row is a tint with a violet edge, never a
// solid fill — so the note list is built with this theme and keeps its own
// row look.
func listTheme() *ui.Theme {
	t := designTheme()
	t.Accent = paper.Mix(violet, 0.10)
	t.AccentHover = paper.Mix(violet, 0.14)
	t.AccentPressed = paper.Mix(violet, 0.18)
	t.AccentText = inkNavy
	return t
}

// actionTheme is designTheme with route blue on the accent, for the one
// primary action a screen carries (New note, Create, Save, Keep this
// version): the theme is swapped around the button's construction so the
// widget keeps route blue for its hover and press as well.
func actionTheme() *ui.Theme {
	t := designTheme()
	t.Accent = routeBlue
	t.AccentHover = routeBlueDeep
	t.AccentPressed = routeBlueDown
	t.AccentText = paper
	return t
}

// primaryAction is the route-blue button. MyGO colors primary buttons from the
// accent, so the action theme goes on for the construction only.
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

// underThemeValue is underTheme for a builder that returns a value, as an
// alert dialog returns the button chosen.
func underThemeValue[T any](c *ui.Context, t func() *ui.Theme, make func() T) T {
	c.SetTheme(t())
	v := make()
	c.SetTheme(designTheme())
	return v
}

// dangerTheme is designTheme with signal red on the accent, for the default
// button of a confirmation that deletes something: destroying work is red,
// never the violet selection or the route-blue primary action.
func dangerTheme() *ui.Theme {
	t := designTheme()
	t.Accent = signalRed
	t.AccentHover = ui.Hex("#AD2A24")
	t.AccentPressed = ui.Hex("#93231E")
	t.AccentText = paper
	return t
}

// confirmDialog is the toolkit's alert with DESIGN's vocabulary on its two
// buttons: the safe choice first, the one the alert is about last, in the
// theme that choice deserves. It returns the index of the button chosen — 1
// for the action — or -1 while the alert stays open.
func confirmDialog(c *ui.Context, theme func() *ui.Theme, open *bool, title, message, action string) int {
	return underThemeValue(c, theme, func() int {
		return ui.AlertDialog(c, open, title, message, "Cancel", action)
	})
}

// sidebarButtons keeps the toolbar chrome of a nav pane compact: the arrows
// that own a pane read as one control, not as two floating buttons.
func toolbarButton(c *ui.Context, label, tip string) ui.Element {
	btn := ui.Button(c, label).Tooltip(tip)
	btn.Padding(4, 8).MinWidth(26)
	return btn
}

// label is the signpost style: 11px, semibold, uppercase, +0.08em tracking.
// It heads a section and nothing else (DESIGN.md's Label).
func label(c *ui.Context, text string) ui.Element {
	return labelOn(c, text, inkSoft)
}

func labelOn(c *ui.Context, text string, color ui.Color) ui.Element {
	return ui.Text(c, strings.ToUpper(text)).
		FontSize(sizeLabel).
		Bold().
		LetterSpacing(sizeLabel * 0.08).
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

// --- The sync line ----------------------------------------------------------

// syncLine is DESIGN.md's one signature: a Beck-grammar route diagram of the
// library's network — this device, the sync target, Joplin desktop, a 45°
// branch to mobile and a stub to the local clipper — drawn full width in the
// status strip and nowhere else. Segments and stations take service colors;
// the active segment carries a running dash while a pass is in flight; a
// disrupted sync marks the target station with a red cross. Nothing animates
// while no sync is running.
func syncLine(c *ui.Context, target string, st syncSnapshot, notice syncNotice) ui.Element {
	seg, station, dash := syncLineColors(st, notice)
	el := ui.Box(c).Width(syncLineWidth).Height(syncLineHeight).Shrink(0)
	el.Draw(func(p *ui.Painter, r ui.Rect) {
		x0, y0 := r.X, r.Y

		// The geometry: the labels above the main line at y=20, stations at
		// 12 / 132 / 256, the clipper stub down from this device and the
		// mobile branch down from the desktop peer. 90° and 45° only.
		const (
			mainY     = float32(20)
			deviceX   = float32(12)
			targetX   = float32(132)
			desktopX  = float32(256)
			branchY   = float32(38)
			branchDx  = float32(16)
			lineWidth = float32(3)
		)
		desktop := "Joplin desktop"
		if !st.Enabled {
			desktop = "not connected"
		}
		clipper := "clipper :41184"

		dot := func(x float32, c ui.Color, solid bool) {
			if solid {
				p.Fill(ui.Rect{X: x0 + x - 3.5, Y: y0 + mainY - 3.5, W: 7, H: 7}, c, 3.5)
				return
			}
			p.Fill(ui.Rect{X: x0 + x - 4, Y: y0 + mainY - 4, W: 8, H: 8}, navyDeep, 4)
			p.Stroke(ui.Rect{X: x0 + x - 4, Y: y0 + mainY - 4, W: 8, H: 8}, c, 4, 1.5)
		}

		p.Line(x0+deviceX, y0+mainY, x0+targetX, y0+mainY, lineWidth, seg)
		p.Line(x0+targetX, y0+mainY, x0+desktopX, y0+mainY, lineWidth, station)
		// The clipper stub, off this device.
		p.Line(x0+deviceX, y0+mainY, x0+deviceX+branchDx, y0+branchY, lineWidth, navyLine)
		p.Fill(ui.Rect{X: x0 + deviceX + branchDx - 2.5, Y: y0 + branchY - 2.5, W: 5, H: 5}, navyText, 2.5)
		// The mobile branch, off the desktop peer.
		p.Line(x0+desktopX, y0+mainY, x0+desktopX+branchDx, y0+branchY, lineWidth, navyLine)
		p.Fill(ui.Rect{X: x0 + desktopX + branchDx - 2.5, Y: y0 + branchY - 2.5, W: 5, H: 5}, navyText, 2.5)

		// The running dash, on the active segment only.
		if dash {
			phase := float32(math.Mod(float64(p.Now().UnixNano())/float64(90*time.Millisecond), 1))
			const dashLen, gap = float32(9), float32(9)
			for x := deviceX + phase*(dashLen+gap) - (dashLen + gap); x < targetX; x += dashLen + gap {
				from, to := max(x, deviceX), min(x+dashLen, targetX)
				if to <= from {
					continue
				}
				p.Line(x0+from, y0+mainY, x0+to, y0+mainY, lineWidth+0.5, paper)
			}
			p.AnimationFrame()
		}

		p.Text(x0+deviceX-6, y0+2, "this device", 10, navyText)
		p.Text(x0+targetX-6, y0+2, target, 10, station)
		p.Text(x0+desktopX-34, y0+2, desktop, 10, navyText)
		p.Text(x0+deviceX+branchDx+6, y0+30, clipper, 10, navyText)
		p.Text(x0+desktopX+branchDx+6, y0+30, "mobile", 10, navyText)

		dot(deviceX, navyText, true)
		dot(targetX, station, true)
		if st.Enabled {
			dot(desktopX, genColor(station, 0.75), true)
		} else {
			dot(desktopX, navyLine, false)
		}
		if notice.Err {
			// The disruption cross on the target station.
			const s = float32(7)
			cross := signalRedSoft
			p.Line(x0+targetX-s, y0+mainY-s, x0+targetX+s, y0+mainY+s, 2, cross)
			p.Line(x0+targetX-s, y0+mainY+s, x0+targetX+s, y0+mainY-s, 2, cross)
		}
	})
	return el
}

const (
	syncLineWidth  = float32(340)
	syncLineHeight = float32(46)
)

// magnifier draws the search glyph: a ring and its handle, coords given by the
// box it is drawn in.
func magnifier(p *ui.Painter, r ui.Rect, c ui.Color) {
	s := min(r.W, r.H) * 0.62
	x, y := r.X+(r.W-s)/2-1, r.Y+(r.H-s)/2-1
	p.Stroke(ui.Rect{X: x, Y: y, W: s, H: s}, c, s/2, 1.5)
	p.Line(x+s*0.78, y+s*0.78, x+s+2, y+s+2, 1.5, c)
}

// syncLineColors maps the sync state onto the service vocabulary: an active
// pass is route blue with a running dash, a finished pass is moss, a paused
// one amber, a failure red, and a vault with no target a plain navy line.
func syncLineColors(st syncSnapshot, notice syncNotice) (segment, station ui.Color, dash bool) {
	switch {
	case !st.Enabled:
		return navyLine, navyText, false
	case st.Running || st.Pending:
		return routeBlueSoft, routeBlueSoft, true
	case notice.Err:
		return signalRedSoft, signalRedSoft, false
	case notice.Line == "Offline · sync paused" || strings.HasPrefix(notice.Line, "Sync paused"):
		return amberBright, amberBright, false
	case st.Last.IsZero():
		return navyText, navyText, false
	default:
		return mossBright, mossBright, false
	}
}

// genColor dims a chrome color without leaving the palette.
func genColor(c ui.Color, a float32) ui.Color {
	return c.Mix(navyDeep, 1-a)
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
