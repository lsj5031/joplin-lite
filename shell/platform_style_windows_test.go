package main

import (
	"fmt"
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestWindowsHeaderBrandAndActions(t *testing.T) {
	registerFonts()
	for _, size := range []struct{ width, scale int }{{900, 1}, {1024, 1}, {1280, 1}, {900, 2}, {1280, 2}} {
		width := size.width
		t.Run(fmt.Sprintf("%d-text-%dx", width, size.scale), func(t *testing.T) {
			app, _ := openNativeTestApp(t)
			tt := ui.NewTester(app.view, width, 800)
			setCaptureTitleBar(tt)
			tt.Frame()
			normalBrand, _ := tt.Find("Joplin Lite")
			tt.SetPreferences(ui.Preferences{TextScale: float32(size.scale)})
			tt.Frame()
			brand, ok := tt.Find("Joplin Lite")
			if !ok || brand.X > 50 || brand.Y >= 48 {
				t.Fatalf("brand must lead the header beside its existing mark: %+v, found=%v", brand, ok)
			}
			if size.scale == 2 && brand.H < normalBrand.H*1.8 {
				t.Fatalf("branding ignored doubled OS text size: normal=%+v scaled=%+v", normalBrand, brand)
			}
			toggleLabel := "Open notebooks"
			if app.railOpen && !app.narrow {
				toggleLabel = "Close notebooks"
			}
			toggle, ok := tt.Find(toggleLabel)
			if !ok || toggle.X < brand.X+brand.W {
				t.Fatalf("notebook action must follow the brand: %+v, found=%v", toggle, ok)
			}
			for _, label := range []string{toggleLabel, "Search notes", "New note", "Ask", "Settings"} {
				r, found := tt.Find(label)
				if !found || r.W <= 0 || r.X < 12 || r.X+r.W > float32(width-138) || r.Y+r.H > 48 {
					t.Fatalf("%s must fit before native caption buttons: %+v, found=%v", label, r, found)
				}
			}
			if dir := os.Getenv("CAPTURE_DIR"); dir != "" {
				capture(t, dir, fmt.Sprintf("windows-header-%d-text-%dx", width, size.scale), tt)
			}
			wasOpen := app.railOpen
			click(t, tt, toggleLabel)
			if app.railOpen == wasOpen {
				t.Fatal("separate notebook action did not toggle the rail")
			}
			if app.narrow {
				screen(tt)
				tt.Key(0, ui.KeyEscape)
				screen(tt)
				if app.railOpen {
					t.Fatal("Escape did not dismiss the notebook drawer")
				}
			}
			// The action remains independently keyboard reachable and operable.
			// Leave the pointer over the toolbar while switching to the keyboard:
			// its hover tooltip must not prevent dismissal of the focused tooltip.
			toggleLabel = "Open notebooks"
			if app.railOpen && !app.narrow {
				toggleLabel = "Close notebooks"
			}
			for i := 0; i < 60 && !tt.Focused(toggleLabel); i++ {
				tt.Key(0, ui.KeyTab)
				tt.Frame()
			}
			if !tt.Focused(toggleLabel) {
				t.Fatal("notebook action is not keyboard reachable")
			}
			beforeKeyboard := app.railOpen
			tt.Key(0, ui.KeySpace)
			tt.Frame()
			if app.railOpen == beforeKeyboard {
				t.Fatal("Space did not activate the notebook action")
			}
			if app.narrow {
				screen(tt)
				// Keyboard opening focuses New notebook and shows its tooltip.
				// Escape dismisses that top overlay before dismissing the drawer.
				tt.Key(0, ui.KeyEscape)
				screen(tt)
				if !app.railOpen {
					t.Fatal("first Escape must dismiss the focus tooltip before the drawer")
				}
				tt.Key(0, ui.KeyEscape)
				screen(tt)
				if app.railOpen {
					t.Fatal("second Escape did not dismiss the keyboard-opened drawer")
				}
			}
			click(t, tt, "Search notes")
			tt.Type("header search")
			tt.Frame()
			if app.query != "header search" {
				t.Fatalf("search did not receive input: %q; focused=%v rail=%v texts=%v", app.query, tt.Focused("Search notes"), app.railOpen, tt.Texts())
			}
			click(t, tt, "New note")
			if app.note == nil {
				t.Fatal("New note did not create a note")
			}
			click(t, tt, "Ask")
			if !app.ask.open {
				t.Fatal("Ask did not open")
			}
			click(t, tt, "Settings")
			if !app.settingsOpen {
				t.Fatal("Settings did not open")
			}
		})
	}
}
