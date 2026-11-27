package main

import (
	"strings"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// TestNativeUIReviewFromHelpMenu opens the embedded UI review from Help ▸ UI
// Review, checks it renders its title, and closes it with Escape.
func TestNativeUIReviewFromHelpMenu(t *testing.T) {
	app, _ := seedShortcutApp(t)

	if !strings.Contains(uiReviewMarkdown, "Feedback Check and Improvement Plan") {
		t.Fatal("the embedded UI review is missing its title")
	}

	var item *mygo.MenuItem
	for _, menu := range app.appMenu().Items() {
		if menu.Label != "Help" {
			continue
		}
		for _, sub := range menu.Submenu {
			if sub.Label == "UI Review" {
				item = sub
			}
		}
	}
	if item == nil {
		t.Fatal("UI Review is missing from the Help menu")
	}

	item.Click(item, nil)
	if !app.uiReviewOpen {
		t.Fatal("Help ▸ UI Review did not open the review")
	}

	tt := ui.NewTester(app.view, 1280, 800)
	tt.Frame()
	if !tt.HasText("Feedback Check and Improvement Plan") {
		t.Fatalf("UI review does not show its title; texts = %q", tt.Texts())
	}

	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if app.uiReviewOpen {
		t.Fatal("Escape did not close the UI review")
	}
}
