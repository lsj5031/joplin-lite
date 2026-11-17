package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestNativeShortcutsQuestionMark opens the reference with a plain "?", the
// way GitHub and Slack do, and keeps the character typing when a text field
// is focused instead.
func TestNativeShortcutsQuestionMark(t *testing.T) {
	app, _ := seedShortcutApp(t)
	tt := ui.NewTester(app.view, 1280, 800)
	tt.Frame()

	tt.Key(ui.Shift, ui.KeySlash)
	tt.Frame()
	if !app.shortcutsOpen {
		t.Fatal("? did not open the keyboard reference")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if app.shortcutsOpen {
		t.Fatal("Escape did not close the keyboard reference")
	}

	// With the search field focused the key types there; the reference must
	// stay closed even though the window registered the same chord.
	click(t, tt, "Search notes")
	tt.Type("query ")
	tt.Frame()
	tt.Key(ui.Shift, ui.KeySlash)
	tt.Frame()
	if app.shortcutsOpen {
		t.Fatal("? opened the keyboard reference while the search field was focused")
	}
}
