package main

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/lsj5031/joplin-lite/internal/models"
)

func regressionNotebook(t *testing.T, app *nativeApp, title string) *models.Folder {
	t.Helper()
	nb := &models.Folder{Title: title}
	if err := app.svc.CreateNotebook(nb); err != nil {
		t.Fatal(err)
	}
	return nb
}

func TestNativeHeaderActionsCanEmptyFilter(t *testing.T) {
	for _, action := range []string{"move", "tag"} {
		t.Run(action, func(t *testing.T) {
			app, _ := openNativeTestApp(t)
			source := regressionNotebook(t, app, "Source")
			target := regressionNotebook(t, app, "Target")
			n := addTestNote(t, app.svc, source.ID, "Only note", "body")
			tag := &models.Tag{Title: "Only tag"}
			if err := app.svc.CreateTag(tag); err != nil {
				t.Fatal(err)
			}
			if err := app.svc.AddTag(n.ID, tag.ID); err != nil {
				t.Fatal(err)
			}
			app.reloadNotebooks()
			app.reloadTags()
			if action == "move" {
				app.selectedNotebook = source.ID
			} else {
				app.selectedTag = tag.ID
			}
			app.reloadNotes()
			tt := ui.NewTester(app.view, 1280, 800)
			if action == "move" {
				click(t, tt, "Move note")
				screen(tt)
				click(t, tt, "Move to "+target.Title)
			} else {
				click(t, tt, "Note tags")
				screen(tt)
				click(t, tt, "Tag: "+tag.Title)
			}
			if app.note != nil || len(app.notes) != 0 {
				t.Fatalf("filter should be empty: note=%v rows=%d", app.note, len(app.notes))
			}
			tt.Frame()
		})
	}
}

func TestNativeMenusRespectDeleteConfirmation(t *testing.T) {
	app, _ := openNativeTestApp(t)
	nb := regressionNotebook(t, app, "Notes")
	alpha := addTestNote(t, app.svc, nb.ID, "Alpha", "")
	beta := addTestNote(t, app.svc, nb.ID, "Beta", "")
	app.reloadNotebooks()
	app.reloadNotes()
	app.showNote(alpha.ID)
	app.history, app.histPos = nil, -1
	app.remember(alpha.ID)
	app.showNote(beta.ID)
	app.remember(beta.ID)
	tt := ui.NewTester(app.view, 1280, 800)
	click(t, tt, "Delete note")
	var back *mygo.MenuItem
	for _, menu := range app.appMenu().Items() {
		for _, item := range menu.Submenu {
			if item.Label == "Back" {
				back = item
			}
		}
	}
	if back == nil {
		t.Fatal("Back menu missing")
	}
	back.Click(back, nil)
	dockNew := app.dockMenu().Items()[0]
	dockNew.Click(dockNew, nil)
	if app.note.ID != beta.ID {
		t.Fatal("native menu changed the confirmation target")
	}
	click(t, tt, "Delete")
	if n, err := app.svc.GetNote(alpha.ID); err != nil || n == nil {
		t.Fatalf("unconfirmed note deleted: %v", err)
	}
	if n, err := app.svc.GetNote(beta.ID); err != nil || n != nil {
		t.Fatalf("confirmed note not deleted: %v", err)
	}
}

func TestNativeContextMoveSurvivesAutosave(t *testing.T) {
	app, _ := openNativeTestApp(t)
	source := regressionNotebook(t, app, "Source")
	target := regressionNotebook(t, app, "Target")
	n := addTestNote(t, app.svc, source.ID, "Moved note", "body")
	app.reloadNotebooks()
	app.reloadNotes()
	tt := ui.NewTester(app.view, 1280, 800)
	if err := tt.RightClick(n.Title); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Move to notebook", target.Title); err != nil {
		t.Fatal(err)
	}
	app.body = "Edited after moving"
	app.markDirty()
	app.flush()
	got, err := app.svc.GetNote(n.ID)
	if err != nil || got.ParentID != target.ID || got.Body != app.body {
		t.Fatalf("autosave reverted move: %#v, %v", got, err)
	}
}

func TestNativeHeaderActionsRemainVisible(t *testing.T) {
	registerFonts()
	for _, dark := range []bool{false, true} {
		t.Run(fmt.Sprint(dark), func(t *testing.T) {
			app, _ := openNativeTestApp(t)
			nb := regressionNotebook(t, app, strings.Repeat("Wide notebook ", 4))
			n := addTestNote(t, app.svc, nb.ID, "A readable title", "Distinctive note excerpt")
			for _, title := range []string{"First very long tag name", "Second very long tag name"} {
				tag := &models.Tag{Title: title}
				if err := app.svc.CreateTag(tag); err != nil {
					t.Fatal(err)
				}
				if err := app.svc.AddTag(n.ID, tag.ID); err != nil {
					t.Fatal(err)
				}
			}
			app.reloadNotebooks()
			app.reloadTags()
			app.reloadNotes()
			app.dark, app.railOpen = dark, true
			tt := ui.NewTester(app.view, 1024, 640)
			screen(tt)
			for _, label := range []string{"Move note", "Note tags", "Todo", "Attach file", "Edit", "Delete note"} {
				r, ok := tt.Find(label)
				if !ok || r.W < 20 || r.H < 20 || r.X < 0 || r.X+r.W > 1024 || r.Y+r.H > 640 {
					t.Errorf("%s is inaccessible: %+v found=%v", label, r, ok)
				}
			}
			r, ok := tt.Find("Distinctive note excerpt")
			if !ok || r.X >= app.railW+app.listW+cardGap || r.H == 0 {
				t.Fatal("note list lost its excerpt")
			}
			if dir := os.Getenv("CAPTURE_DIR"); dir != "" {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				capture(t, dir, fmt.Sprintf("review-header-dark-%v", dark), tt)
			}
		})
	}
}

func TestNativePaletteFindsLaterPagesAndKeepsSelection(t *testing.T) {
	app, _ := openNativeTestApp(t)
	nb := regressionNotebook(t, app, "Journal")
	var target *models.Note
	for i := 0; i < 105; i++ {
		target = addTestNote(t, app.svc, nb.ID, fmt.Sprintf("Entry %03d", i), "body")
	}
	app.reloadNotebooks()
	app.sort = "title-asc"
	app.reloadNotes()
	tt := ui.NewTester(app.view, 900, 600)
	app.selectedNotebook, app.selectedTag, app.query = conflictsRail, "unrelated", "no matches"
	app.reloadNotes()
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyP)
	tt.Type(target.Title)
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if app.note == nil || app.note.ID != target.ID || app.selectedNote < 100 ||
		!app.narrowEditor || app.selectedTag != "" || app.query != "" || app.selectedNotebook == conflictsRail {
		t.Fatalf("palette did not reveal result: selected=%d narrowEditor=%v note=%v", app.selectedNote, app.narrowEditor, app.note)
	}
	app.refreshAfterSync()
	tt.Frame()
	if app.note == nil || app.note.ID != target.ID || app.selectedNote < 100 {
		t.Fatal("refresh lost the later-page palette selection")
	}
	if _, ok := tt.Find("Note title"); !ok {
		t.Fatal("compact palette selection did not show editor")
	}
}

func TestNativeSearchArrowsScrollSelection(t *testing.T) {
	app, _ := openNativeTestApp(t)
	nb := regressionNotebook(t, app, "Journal")
	for i := 0; i < 100; i++ {
		addTestNote(t, app.svc, nb.ID, fmt.Sprintf("Entry %03d", i), "body")
	}
	app.reloadNotebooks()
	app.sort = "title-asc"
	app.reloadNotes()
	tt := ui.NewTester(app.view, 1280, 800)
	click(t, tt, "Search notes")
	for i := 0; i < 60; i++ {
		tt.Key(0, ui.KeyDown)
	}
	first, last := app.list.Visible()
	if app.selectedNote != 60 || first > 60 || last <= 60 {
		t.Fatalf("selection not visible: selected=%d visible=[%d,%d)", app.selectedNote, first, last)
	}
	for i := 0; i < 60; i++ {
		tt.Key(0, ui.KeyUp)
	}
	first, last = app.list.Visible()
	if app.selectedNote != 0 || first != 0 || last <= 0 {
		t.Fatalf("up selection not visible: selected=%d visible=[%d,%d)", app.selectedNote, first, last)
	}
}

func TestNativeContextCancelKeepsFilterConsistent(t *testing.T) {
	for _, kind := range []string{"notebook", "tag"} {
		for _, action := range []string{"Rename", "Delete"} {
			t.Run(kind+action, func(t *testing.T) {
				app, _ := openNativeTestApp(t)
				source := regressionNotebook(t, app, "Source")
				target := regressionNotebook(t, app, "Target")
				addTestNote(t, app.svc, source.ID, "Source note", "")
				targetNote := addTestNote(t, app.svc, target.ID, "Target note", "")
				label := target.Title
				if kind == "tag" {
					tag := &models.Tag{Title: "Target tag"}
					if err := app.svc.CreateTag(tag); err != nil {
						t.Fatal(err)
					}
					if err := app.svc.AddTag(targetNote.ID, tag.ID); err != nil {
						t.Fatal(err)
					}
					label = "Target tag"
				}
				app.reloadNotebooks()
				app.reloadTags()
				app.selectedNotebook, app.railOpen = source.ID, true
				app.reloadNotes()
				tt := ui.NewTester(app.view, 1280, 800)
				if err := tt.RightClick(label); err != nil {
					t.Fatal(err)
				}
				if err := tt.ChooseMenuItem(action + " " + kind + "…"); err != nil {
					t.Fatal(err)
				}
				click(t, tt, "Cancel")
				if len(app.notes) != 1 || app.notes[0].ID != targetNote.ID {
					t.Fatal("cancel left stale notes under the new filter")
				}
			})
		}
	}
}
