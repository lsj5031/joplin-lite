package main

import (
	"fmt"
	"os"
	"runtime"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/lsj5031/joplin-lite/internal/models"
)

// These scenarios exercise the real view against disposable vaults. Set
// CAPTURE_DIR to also save the final states for visual review.
func TestNativeNavigation(t *testing.T) {
	registerFonts()
	for _, size := range [][2]int{{900, 600}, {1024, 640}, {1280, 800}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			app, _ := openNativeTestApp(t)
			folder := &models.Folder{Title: "A long notebook name for the release and documentation checklist"}
			if err := app.svc.CreateNotebook(folder); err != nil {
				t.Fatal(err)
			}
			alpha := addTestNote(t, app.svc, folder.ID, "Alpha", "alpha body")
			beta := addTestNote(t, app.svc, "", "Beta", "beta body")
			app.reloadNotebooks()
			app.reloadNotes()
			tt := ui.NewTester(app.view, size[0], size[1])
			shot := func(name string) {
				if dir := os.Getenv("CAPTURE_DIR"); dir != "" {
					capture(t, dir, fmt.Sprintf("%dx%d-%s", size[0], size[1], name), tt)
				}
			}
			click(t, tt, "Alpha")
			if app.narrow {
				click(t, tt, "Back to notes")
				click(t, tt, "Alpha")
				if !app.narrowEditor || app.note.ID != alpha.ID {
					t.Fatal("same row did not reopen after Back")
				}
				shot("same-row-reopened")
				click(t, tt, "Back to notes")
				if err := tt.ClickWith(ui.Cmd, "Beta"); err != nil {
					t.Fatal(err)
				}
				tt.Frame()
				if app.narrowEditor || !app.selection.Has(alpha.ID) || !app.selection.Has(beta.ID) {
					t.Fatalf("modifier selection left list or lost selection: editor=%v selection=%v", app.narrowEditor, app.selectedIDs())
				}
				shot("bulk-list")
				// Windows uses Ctrl+Shift to retain the existing choice while
				// extending a range; macOS Shift already retains other rows.
				mods := ui.Shift
				if runtime.GOOS != "darwin" {
					mods |= ui.Cmd
				}
				if err := tt.ClickWith(mods, "Alpha"); err != nil {
					t.Fatal(err)
				}
				if app.narrowEditor || app.selection.Len() != 2 {
					t.Fatalf("Shift selection must preserve list and range: editor=%v selection=%v", app.narrowEditor, app.selectedIDs())
				}
				click(t, tt, "Delete selected notes")
				tt.Key(0, ui.KeyEnter)
				if n, _ := app.svc.GetNote(alpha.ID); n == nil || app.confirmBulkDelete {
					t.Fatal("Enter must cancel bulk deletion")
				}
				click(t, tt, "Clear selection")
				click(t, tt, "Alpha")
			}

			// A shortcut must move actual focus away from the note body, even
			// after the search input has already mounted.
			for _, key := range []ui.Key{ui.KeyK, ui.KeyF} {
				if !app.editing {
					click(t, tt, "Edit")
				}
				click(t, tt, "Note body")
				tt.Key(ui.Cmd, key)
				tt.Type("Alpha")
				if app.query != "Alpha" || app.body != "alpha body" {
					t.Fatalf("shortcut %v typing: query=%q body=%q", key, app.query, app.body)
				}
				click(t, tt, "Clear search")
			}

			// Long metadata and many tags cannot displace any note action.
			for i := range 18 {
				tag := &models.Tag{Title: fmt.Sprintf("Long project tag %02d", i)}
				if err := app.svc.CreateTag(tag); err != nil {
					t.Fatal(err)
				}
				if err := app.svc.AddTag(alpha.ID, tag.ID); err != nil {
					t.Fatal(err)
				}
			}
			app.reloadNoteTags()
			tt.Frame()
			shot("many-tags")
			click(t, tt, "Note tags")
			if !app.tagMenuOpen {
				t.Fatal("Tags action unreachable")
			}
			tt.Key(0, ui.KeyEscape)
			click(t, tt, "Move note")
			if !app.moveOpen {
				t.Fatal("Move action unreachable")
			}
			tt.Key(0, ui.KeyEscape)
			click(t, tt, "Note actions")
			click(t, tt, "Delete note…")
			shot("safe-delete")
			tt.Key(0, ui.KeyEnter)
			if n, _ := app.svc.GetNote(alpha.ID); n == nil || app.confirmDelete {
				t.Fatal("Enter must cancel deletion")
			}
			click(t, tt, "Note actions")
			click(t, tt, "Delete note…")
			click(t, tt, "Delete")
			if n, _ := app.svc.GetNote(alpha.ID); n != nil {
				t.Fatal("explicit Delete did not delete")
			}
		})
	}
}

func TestNativeSavedSearchNavigation(t *testing.T) {
	registerFonts()
	app, _ := openNativeTestApp(t)
	for i := range 20 {
		app.savedSearches = append(app.savedSearches, savedSearch{Name: fmt.Sprintf("Search %02d", i), Query: fmt.Sprintf("query %02d", i)})
	}
	app.query = "replacement"
	tt := ui.NewTester(app.view, 900, 600)
	click(t, tt, "Saved searches")
	if dir := os.Getenv("CAPTURE_DIR"); dir != "" {
		capture(t, dir, "900x600-saved-searches", tt)
	}
	// Save remains outside the scroll area and can be reached immediately.
	click(t, tt, "Save current search")
	if !app.savedOpen {
		t.Fatal("Save current search unreachable")
	}
	tt.Type("search 00")
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	if app.savedSearches[0].Query != "query 00" || !app.savedOpen {
		t.Fatal("Enter silently replaced existing saved search")
	}
	if dir := os.Getenv("CAPTURE_DIR"); dir != "" {
		capture(t, dir, "900x600-replace-saved-search", tt)
	}
	click(t, tt, "Replace saved search")
	if app.savedSearches[0].Query != "replacement" {
		t.Fatal("explicit replacement failed")
	}
	click(t, tt, "Saved searches")
	r, ok := tt.Find("Apply saved search Search 00")
	if !ok {
		t.Fatal("first saved search missing")
	}
	tt.Scroll(r.X+30, r.Y+10, 0, 1200)
	tt.Frame()
	if dir := os.Getenv("CAPTURE_DIR"); dir != "" {
		capture(t, dir, "900x600-saved-searches-scrolled", tt)
	}
	click(t, tt, "Apply saved search Search 19")
	if app.query != "query 19" {
		t.Fatal("last saved search unreachable")
	}
	click(t, tt, "Saved searches")
	tt.Scroll(r.X+30, r.Y+10, 0, 1200)
	click(t, tt, "Delete saved search Search 19")
	if app.savedDelete != "Search 19" {
		t.Fatal("delete confirmation did not open")
	}
	tt.Key(0, ui.KeyEnter)
	if len(app.savedSearches) != 20 || app.savedDelete != "" {
		t.Fatal("Enter must cancel saved-search deletion")
	}
	click(t, tt, "Saved searches")
	tt.Scroll(r.X+30, r.Y+10, 0, 1200)
	click(t, tt, "Delete saved search Search 19")
	click(t, tt, "Delete saved search")
	if len(app.savedSearches) != 19 {
		t.Fatal("explicit saved-search deletion failed")
	}
}

func TestNativeConflictComparisonNavigation(t *testing.T) {
	registerFonts()
	for _, size := range [][2]int{{900, 600}, {1024, 640}, {1280, 800}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			app, _ := openNativeTestApp(t)
			original := addTestNote(t, app.svc, "", "Original", "original body")
			addTestNote(t, app.svc, "", "Unrelated newer note", "unrelated")
			conflict := &models.Note{Title: "Conflict copy", Body: "conflict body", IsConflict: 1, ConflictOriginalID: original.ID}
			if err := app.svc.CreateNote(conflict); err != nil {
				t.Fatal(err)
			}
			app.selectedNotebook = conflictsRail
			app.reloadNotes()
			tt := ui.NewTester(app.view, size[0], size[1])
			if app.narrow {
				click(t, tt, "Conflict copy")
			}
			click(t, tt, "View original")
			if app.note.ID != original.ID || app.selectedNotebook != "" {
				t.Fatal("View original did not navigate to original")
			}
			if runtime.GOOS == "darwin" {
				tt.Key(ui.Cmd, ui.KeyBracketLeft)
			} else {
				tt.Key(ui.Alt, ui.KeyLeft)
			}
			if app.note.ID != conflict.ID {
				t.Fatalf("Back returned %s instead of exact conflict", app.note.Title)
			}
			if app.selectedNotebook != conflictsRail || app.selectedNote < 0 || app.notes[app.selectedNote].ID != conflict.ID {
				t.Fatal("Back must restore Conflicts and select the conflict row")
			}
			if dir := os.Getenv("CAPTURE_DIR"); dir != "" {
				capture(t, dir, fmt.Sprintf("%dx%d-conflict-actions", size[0], size[1]), tt)
			}
			click(t, tt, "Resolve conflict")
			tt.Key(0, ui.KeyEnter)
			if n, _ := app.svc.GetNote(conflict.ID); n == nil {
				t.Fatal("Enter resolved conflict destructively")
			}
			click(t, tt, "Resolve conflict")
			click(t, tt, "Keep this version")
			if n, _ := app.svc.GetNote(original.ID); n == nil || n.Body != "conflict body" {
				t.Fatal("explicit Keep this version did not copy body")
			}
		})
	}
}

func TestNativeEmptyGuidance(t *testing.T) {
	registerFonts()
	for _, size := range [][2]int{{900, 600}, {1024, 640}, {1280, 800}} {
		app, _ := openNativeTestApp(t)
		tt := ui.NewTester(app.view, size[0], size[1])
		if !tt.HasText("Use New note above") || !tt.HasText("Local only") {
			t.Fatal("empty library lacks creation guidance or local state")
		}
		if dir := os.Getenv("CAPTURE_DIR"); dir != "" {
			capture(t, dir, fmt.Sprintf("%dx%d-empty-library", size[0], size[1]), tt)
		}
		app.selectedNotebook = conflictsRail
		app.reloadNotes()
		tt.Frame()
		if !tt.HasText("No conflicts found on this device.") || tt.HasText("agrees with the sync target") {
			t.Fatal("empty conflicts claims unverified agreement")
		}
		if dir := os.Getenv("CAPTURE_DIR"); dir != "" {
			capture(t, dir, fmt.Sprintf("%dx%d-empty-conflicts", size[0], size[1]), tt)
		}
	}
}
