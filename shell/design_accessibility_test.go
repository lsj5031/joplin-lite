package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
	"github.com/lsj5031/joplin-lite/service"
)

func TestDeleteConfirmationStartsWithCancel(t *testing.T) {
	app, db := openNativeTestApp(t)
	svc := service.New(db)
	note := addTestNote(t, svc, "", "Disposable safety note", "Keep this body")
	app = newNativeApp(db)
	tt := ui.NewTester(app.view, 1200, 800)
	open := func() {
		t.Helper()
		click(t, tt, "Note actions")
		click(t, tt, "Delete note…")
		if !tt.Focused("Cancel") {
			t.Fatal("Delete confirmation must initially focus Cancel")
		}
	}
	for _, key := range []ui.Key{ui.KeyEnter, ui.KeyEscape} {
		open()
		tt.Key(0, key)
		if app.confirmDelete {
			t.Fatalf("%v did not close confirmation", key)
		}
		if got, err := svc.GetNote(note.ID); err != nil || got == nil || got.Body != "Keep this body" {
			t.Fatalf("%v changed note: note=%+v err=%v", key, got, err)
		}
	}
	open()
	tt.Key(0, ui.KeyTab)
	if !tt.Focused("Delete") {
		t.Fatal("Tab from Cancel must reach explicit Delete")
	}
	tt.Key(0, ui.KeyEnter)
	if got, err := svc.GetNote(note.ID); err != nil || got != nil {
		t.Fatalf("explicit Delete did not delete: note=%+v err=%v", got, err)
	}
}

func TestSharedSegmentedPickerKeyboard(t *testing.T) {
	selected := 1
	changes := 0
	tt := ui.NewTester(func(c *ui.Context) {
		applyPalette(false)
		applyDesignPreferences(c)
		c.SetTheme(designTheme())
		if segmentedControl(c, "Appearance", &selected, "Light", "Dark", "System") {
			changes++
		}
	}, 450, 120)
	tt.Key(0, ui.KeyTab)
	if !tt.Focused("Dark") {
		t.Fatal("group Tab entry must focus selected segment")
	}
	tt.Key(0, ui.KeyRight)
	if selected != 2 || changes != 1 || !tt.Focused("System") {
		t.Fatalf("Right: selection=%d changes=%d", selected, changes)
	}
	tt.Key(0, ui.KeyLeft)
	if selected != 1 || changes != 2 {
		t.Fatalf("Left: selection=%d changes=%d", selected, changes)
	}
	click(t, tt, "Light")
	if selected != 0 || changes != 3 {
		t.Fatalf("click: selection=%d changes=%d", selected, changes)
	}
}

func TestFieldNotesThemeFollowsPreferences(t *testing.T) {
	for _, dark := range []bool{false, true} {
		t.Run(map[bool]string{false: "light", true: "dark"}[dark], func(t *testing.T) {
			var theme ui.Theme
			tt := ui.NewTester(func(c *ui.Context) {
				applyPalette(dark)
				applyDesignPreferences(c)
				c.SetTheme(designTheme())
				theme = *c.Theme()
				ui.Text(c, "Reading sample").FontSize(sizeBody)
			}, 600, 160)
			before, _ := tt.Find("Reading sample")
			normal := theme
			tt.SetPreferences(ui.Preferences{TextScale: 2, HighContrast: true})
			after, _ := tt.Find("Reading sample")
			if after.H < before.H*1.8 || theme.FontSize != normal.FontSize*2 {
				t.Fatalf("text did not enlarge: %v -> %v; theme %g -> %g", before, after, normal.FontSize, theme.FontSize)
			}
			if theme.Border == normal.Border || theme.TextMuted == normal.TextMuted || theme.Focus.A != 255 {
				t.Fatal("high contrast did not strengthen borders, secondary text and focus")
			}
			if theme.Background != normal.Background || theme.Accent != normal.Accent {
				t.Fatal("preferences discarded Field Notes palette")
			}
			tt.Frame()
			if theme.FontSize != normal.FontSize*2 {
				t.Fatal("text scale compounded between frames")
			}
			tt.SetPreferences(ui.Preferences{TextScale: 1})
			if theme != normal {
				t.Fatal("turning preferences off did not restore theme")
			}
		})
	}
}

func TestGlassAccessibilityFallbacks(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for name, material := range map[string]ui.Material{
			"glass": glass.Glass{}, "clear": glass.Glass{Style: glass.Clear},
			"interactive": glass.Glass{Interactive: true, Tint: ui.RGB(180, 30, 20)},
			"blur":        glass.Blur{Radius: 6},
			"soft edge":   glass.ScrollEdge{}, "hard edge": glass.ScrollEdge{Hard: true},
		} {
			t.Run(name+map[bool]string{false: "/light", true: "/dark"}[dark], func(t *testing.T) {
				back := ui.RGB(200, 30, 60)
				var theme *ui.Theme
				tt := ui.NewTester(func(c *ui.Context) {
					applyPalette(dark)
					applyDesignPreferences(c)
					theme = designTheme()
					// Distinct tokens catch a swapped Surface/Background fallback.
					theme.Surface = ui.RGB(71, 82, 65)
					c.SetTheme(theme)
					ui.Box(c).Fill().Background(back).Children(func() {
						ui.Box(c).Absolute().Left(20).Top(20).Size(120, 80).Material(material)
					})
				}, 180, 120)
				red := tt.Image().RGBAAt(80, 30)
				back = ui.RGB(10, 110, 220)
				tt.Frame()
				if blue := tt.Image().RGBAAt(80, 30); blue == red {
					t.Fatal("control: normal material must sample backdrop")
				}
				tt.SetPreferences(ui.Preferences{ReduceTransparency: true, HighContrast: true})
				// MyGO v0.9.0 falls back to Background for Clear glass and
				// keeps an interactive tint on the opaque surface.
				want := theme.Background
				if name == "glass" {
					want = theme.Surface
				}
				if name == "interactive" {
					want = theme.Surface.Mix(ui.RGB(180, 30, 20), 0.88)
				}
				if name == "glass" || name == "clear" || name == "interactive" {
					border := tt.Image().RGBAAt(20, 50)
					if border.R != theme.Border.R || border.G != theme.Border.G || border.B != theme.Border.B || border.A != 255 {
						t.Fatalf("opaque contrast border=%v want %v", border, theme.Border)
					}
				}
				got := tt.Image().RGBAAt(80, 30)
				if got.R != want.R || got.G != want.G || got.B != want.B || got.A != 255 {
					t.Fatalf("opaque center=%v want %v", got, want)
				}
				back = ui.RGB(200, 30, 60)
				tt.Frame()
				if after := tt.Image().RGBAAt(80, 30); after != got {
					t.Fatalf("reduced transparency still samples backdrop: %v -> %v", got, after)
				}
			})
		}
	}
}

func TestWorkspaceEnlargedText(t *testing.T) {
	originalChrome := windowsChrome
	t.Cleanup(func() { windowsChrome = originalChrome })
	for _, windows := range []bool{false, true} {
		t.Run(map[bool]string{false: "standard-toolbar", true: "windows-toolbar"}[windows], func(t *testing.T) {
			windowsChrome = windows
			for name, width := range map[string]float32{"default": listWidth, "minimum": listMinWidth} {
				t.Run(name, func(t *testing.T) { testWorkspaceEnlargedText(t, width) })
			}
		})
	}
}

func testWorkspaceEnlargedText(t *testing.T, width float32) {
	t.Helper()
	app, db := openNativeTestApp(t)
	addTestNote(t, service.New(db), "", "Enlarged reading", "Readable body at the OS text size.")
	app = newNativeApp(db)
	app.listW = width
	tt := ui.NewTester(app.view, 1280, 800)
	click(t, tt, "Settings")
	before, ok := tt.Find("Appearance")
	if !ok {
		t.Fatal("Appearance heading is missing")
	}
	tt.SetPreferences(ui.Preferences{TextScale: 2, HighContrast: true, ReduceTransparency: true})
	after, ok := tt.Find("Appearance")
	if !ok || after.H < before.H*1.8 {
		t.Fatalf("Appearance text did not enlarge: %v -> %v", before, after)
	}
	click(t, tt, "Cancel settings")
	if app.settingsOpen {
		t.Fatal("Cancel did not close Settings")
	}
	if !windowsChrome {
		button, ok := tt.Find("Settings")
		if !ok || button.X < 0 || button.X+button.W > width {
			t.Fatalf("Settings extends outside note list toolbar: %v, pane width %g", button, width)
		}
	}
	click(t, tt, "Settings")
	if !tt.HasText("Appearance") {
		t.Fatal("Settings did not open with enlarged text")
	}
	if dir := os.Getenv("ACCESSIBILITY_CAPTURE_DIR"); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(filepath.Join(dir, "settings-enlarged-contrast-opaque.png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}
}
