package main

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/lsj5031/joplin-lite/internal/assistant"
	"github.com/lsj5031/joplin-lite/internal/config"
	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/service"
)

func tabToWorkspaceControl(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	for i := 0; i < 80; i++ {
		if tt.Focused(label) {
			return
		}
		tt.Key(0, ui.KeyTab)
	}
	t.Fatalf("Tab did not reach %q", label)
}

func TestWorkspaceKeyboardSplittersAndRail(t *testing.T) {
	for _, dark := range []bool{false, true} {
		t.Run(fmt.Sprint(dark), func(t *testing.T) {
			app, db := openNativeTestApp(t)
			nb := &models.Folder{Title: "Keyboard notebook"}
			if err := service.New(db).CreateNotebook(nb); err != nil {
				t.Fatal(err)
			}
			app.reloadNotebooks()
			app.railOpen, app.dark = true, dark
			tt := ui.NewTester(app.view, 1800, 800)
			tabToWorkspaceControl(t, tt, nb.Title)
			tt.Key(0, ui.KeyEnter)
			if app.selectedNotebook != nb.ID {
				t.Fatal("Enter did not choose the focused notebook")
			}
			tabToWorkspaceControl(t, tt, "Notebook pane width")
			before := app.railW
			tt.Key(0, ui.KeyRight)
			if app.railW != before+10 {
				t.Fatalf("Right resized rail to %v, from %v", app.railW, before)
			}
			tt.Key(0, ui.KeyHome)
			if app.railW != railWidth {
				t.Fatalf("Home did not reset rail: %v", app.railW)
			}
			app.railW = railMinWidth
			tt.Frame()
			tt.Key(0, ui.KeyLeft)
			if app.railW != railMinWidth {
				t.Fatal("rail escaped its minimum")
			}
			tabToWorkspaceControl(t, tt, "Note list width")
			before = app.listW
			tt.Key(0, ui.KeyLeft)
			if app.listW != before-10 {
				t.Fatal("Left did not resize note list")
			}
			app.ask.open = true
			tt.Frame()
			tabToWorkspaceControl(t, tt, "Ask pane width")
			before = app.ask.width
			tt.Key(0, ui.KeyRight)
			if app.ask.width != before-10 {
				t.Fatalf("right-side divider moved in the wrong direction: %v -> %v", before, app.ask.width)
			}
			tt.Key(0, ui.KeyLeft)
			if app.ask.width != before {
				t.Fatal("opposite arrow did not restore Ask width")
			}
			if dir := os.Getenv("WORKSPACE_CAPTURE_DIR"); dir != "" {
				capture(t, dir, fmt.Sprintf("workspace-keyboard-dark-%v", dark), tt)
			}
		})
	}
}

func TestWorkspaceWritingMeasureAtMinimumAndWideWidths(t *testing.T) {
	for _, width := range []int{900, 1800} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d-dark-%v", width, dark), func(t *testing.T) {
				app, db := openNativeTestApp(t)
				n := addTestNote(t, service.New(db), "", "Measure fixture", strings.Repeat("A readable writing measure. ", 60))
				app.showNote(n.ID)
				app.editing, app.narrowEditor, app.dark = true, true, dark
				tt := ui.NewTester(app.view, width, 600)
				tt.Frame()
				body, ok := tt.Find("Note body")
				if !ok || body.W < 300 || body.X < 0 || body.X+body.W > float32(width) {
					t.Fatalf("body outside usable window: %+v", body)
				}
				pad := float32(48)
				if width == 900 {
					pad = 24
				}
				if body.W-2*pad > 700.5 {
					t.Fatalf("writing measure exceeds 700 DIP: %v", body.W-2*pad)
				}
				if width == 1800 && body.W-2*pad < 699.5 {
					t.Fatalf("wide writing measure did not reach cap: %v", body.W-2*pad)
				}
				if dir := os.Getenv("WORKSPACE_CAPTURE_DIR"); dir != "" {
					capture(t, dir, fmt.Sprintf("writing-%d-dark-%v", width, dark), tt)
				}
				tt.Key(ui.Cmd, ui.KeyE)
				if app.editing || !tt.HasText(strings.TrimSpace(n.Body)) {
					t.Fatalf("preview did not render the same note: editing=%v, texts=%q", app.editing, tt.Texts())
				}
				if dir := os.Getenv("WORKSPACE_CAPTURE_DIR"); dir != "" {
					capture(t, dir, fmt.Sprintf("reading-%d-dark-%v", width, dark), tt)
				}
			})
		}
	}
}

func TestWorkspaceAnnouncesStatusWithoutStreamingNoise(t *testing.T) {
	app, db := openNativeTestApp(t)
	n := addTestNote(t, service.New(db), "", "Status fixture", "Before")
	app.showNote(n.ID)
	app.editing = true
	st := &syncStatus{}
	app.be = &backend{db: db, cfg: &config.Config{SyncTarget: targetFolder}, status: st}
	t.Cleanup(func() {
		if app.syncPoll != nil {
			app.syncPoll.Stop()
		}
	})
	tt := ui.NewTester(app.view, 1280, 800)
	tt.Announcements()
	expect := func(want string) {
		t.Helper()
		got := tt.Announcements()
		if !slices.Contains(got, want) {
			t.Fatalf("announcement %q missing from %q", want, got)
		}
		tt.Frame()
		if repeated := tt.Announcements(); len(repeated) != 0 {
			t.Fatalf("unchanged state repeated announcements: %q", repeated)
		}
	}
	click(t, tt, "Note body")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("After")
	tt.Key(ui.Cmd, ui.KeyS)
	expect("Note saved")
	app.saveErr = "Disk unavailable"
	tt.Frame()
	expect("Could not save: Disk unavailable")
	app.saveErr = ""
	tt.Frame()
	tt.Announcements()
	st.start()
	tt.Frame()
	expect("Syncing notes")
	st.progress("Downloading 1 of 99")
	tt.Frame()
	if got := tt.Announcements(); len(got) != 0 {
		t.Fatalf("sync progress was noisy: %q", got)
	}
	st.finish(errors.New("Denied"))
	tt.Frame()
	expect("Sync failed. Denied")
	st.finish(nil)
	tt.Frame()
	got := tt.Announcements()
	if len(got) != 1 || !strings.HasPrefix(got[0], "Synced ") {
		t.Fatalf("successful sync was not announced once: %q", got)
	}
	app.backupBusy = "Exporting 1 of 42"
	tt.Frame()
	expect("Import or export in progress")
	app.backupBusy = "Exporting 2 of 42"
	tt.Frame()
	if got := tt.Announcements(); len(got) != 0 {
		t.Fatalf("backup progress was noisy: %q", got)
	}
	app.backupBusy, app.backupNotice = "", "Exported 42 notes"
	tt.Frame()
	expect("Exported 42 notes")
	app.backupErr = "Could not import: invalid archive"
	tt.Frame()
	expect("Could not import: invalid archive")
	app.ask.busy, app.ask.runID = true, "run-1"
	tt.Frame()
	expect("Ask is working")
	app.applyAskEvent(assistant.Event{Type: "answer.delta", RunID: "run-1", Text: "Partial answer"})
	tt.Frame()
	if got := tt.Announcements(); len(got) != 0 {
		t.Fatalf("streaming answer was announced: %q", got)
	}
	app.applyAskEvent(assistant.Event{Type: "run.failed", RunID: "run-1", Message: "Connection lost"})
	tt.Frame()
	expect("Ask failed: Connection lost")
	for _, terminal := range []struct{ event, announcement string }{
		{"run.completed", "Ask response ready"}, {"run.cancelled", "Ask stopped"},
	} {
		app.ask.busy, app.ask.runID = true, "next-run"
		app.applyAskEvent(assistant.Event{Type: terminal.event, RunID: "next-run"})
		tt.Frame()
		expect(terminal.announcement)
	}
	// A second edit/save still announces even if it occurs in the same minute.
	click(t, tt, "Note body")
	tt.Type(" another edit")
	tt.Key(ui.Cmd, ui.KeyS)
	expect("Note saved")
}

func TestWorkspaceSavedSearchTextScale(t *testing.T) {
	app, _ := openNativeTestApp(t)
	app.query = "tag:work"
	app.openSaveSearch()
	tt := ui.NewTester(app.view, 900, 600)
	before, ok := tt.Find("Save this search")
	if !ok {
		t.Fatal("saved search dialog did not open")
	}
	tt.SetPreferences(ui.Preferences{TextScale: 2, HighContrast: true})
	after, ok := tt.Find("Save this search")
	if !ok || after.H < before.H*1.8 {
		t.Fatalf("saved-search heading did not scale: %+v -> %+v", before, after)
	}
	tt.Type("Work filter")
	click(t, tt, "Save")
	if app.savedOpen || len(app.savedSearches) != 1 || app.savedSearches[0].Name != "Work filter" {
		t.Fatal("enlarged saved search dialog could not save")
	}
	click(t, tt, "Saved searches")
	if _, ok := tt.Find("Apply saved search Work filter"); !ok {
		t.Fatal("saved search menu lost its apply action at enlarged text size")
	}
	if dir := os.Getenv("WORKSPACE_CAPTURE_DIR"); dir != "" {
		capture(t, dir, "saved-search-enlarged", tt)
	}
	tt.Key(0, ui.KeyEscape)
	app.query = ""
	tt.Frame()
}
