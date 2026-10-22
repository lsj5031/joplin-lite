package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/lsj5031/joplin-lite/internal/models"
)

func TestPreviewTaskToggleSavesOriginalMarkdown(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, width := range []int{520, 1000} {
			t.Run(fmt.Sprintf("dark=%v/width=%d", dark, width), func(t *testing.T) {
				app, _ := openNativeTestApp(t)
				raw := "中文 intro\r\nsoft line\r\n\r\n- [ ] first task\r\n  continued\r\n  - [X] nested task\r\n\r\n> 1. [ ] quoted task\r\n\r\n```md\r\n- [ ] not a task\r\n```\r\n"
				note := addTestNote(t, app.svc, "", "Tasks", raw)
				app.showNote(note.ID)
				app.dark = dark
				applyPalette(dark)
				t.Cleanup(func() { applyPalette(false) })
				tt := ui.NewTester(func(c *ui.Context) {
					c.SetTheme(designTheme())
					app.previewContent(c)
				}, width, 600)
				click(t, tt, "Task: quoted task")
				want := strings.Replace(raw, "> 1. [ ]", "> 1. [x]", 1)
				if app.body != want || !app.dirty || app.editing {
					t.Fatalf("preview edit changed extra bytes or editing state: %q dirty=%v editing=%v", app.body, app.dirty, app.editing)
				}
				if dir := os.Getenv("CAPTURE_DIR"); dir != "" {
					capture(t, dir, fmt.Sprintf("preview-tasks-checked-%v-%d", dark, width), tt)
				}
				app.saveDue = time.Time{}
				app.tick()
				saved, err := app.svc.GetNote(note.ID)
				if err != nil || saved.Body != want || app.dirty {
					t.Fatalf("autosave did not persist toggle: note=%+v err=%v", saved, err)
				}
				tt.Key(0, ui.KeySpace)
				if app.body != raw {
					t.Fatal("keyboard toggle failed to restore original bytes")
				}
				click(t, tt, "Task: nested task")
				if app.body != strings.Replace(raw, "[X]", "[ ]", 1) {
					t.Fatal("uppercase or nested checkbox did not toggle")
				}
				if _, ok := tt.Find("Task: not a task"); ok {
					t.Fatal("code fence became interactive")
				}
				if dir := os.Getenv("CAPTURE_DIR"); dir != "" {
					capture(t, dir, fmt.Sprintf("preview-tasks-%v-%d", dark, width), tt)
				}
				other := addTestNote(t, app.svc, "", "Other", "- [ ] other task")
				app.showNote(other.ID)
				tt.Frame()
				click(t, tt, "Task: other task")
				app.flush()
				saved, err = app.svc.GetNote(note.ID)
				if err != nil || saved.Body != strings.Replace(raw, "[X]", "[ ]", 1) {
					t.Fatal("switching notes lost the previous task edit")
				}
			})
		}
	}
}

func TestPreviewTaskToggleAtVirtualizedEndKeepsScroll(t *testing.T) {
	app, _ := openNativeTestApp(t)
	raw := "- [ ] duplicate\n\n" + longPreviewFixture() + "\n\n- [ ] duplicate\n"
	note := addTestNote(t, app.svc, "", "Long tasks", raw)
	app.showNote(note.ID)
	tt := ui.NewTester(app.previewContent, 796, 600)
	app.previewList.ScrollToEnd()
	tt.Frame()
	click(t, tt, "Task: duplicate")
	tt.Frame()
	pos := strings.LastIndex(raw, "[ ]") + 1
	if want := raw[:pos] + "x" + raw[pos+1:]; app.body != want {
		t.Fatal("virtualized task changed the wrong duplicate or corrupted the source")
	}
	first, last := app.previewList.Visible()
	if first == 0 || last != len(app.previewChunks)-1 {
		t.Fatalf("task toggle lost scroll position: %d..%d", first, last)
	}
	app.flush()
	saved, err := app.svc.GetNote(note.ID)
	if err != nil || saved.Body != app.body {
		t.Fatal("virtualized toggle did not save")
	}
}

func TestPreviewTaskRejectsStaleSourceAndNote(t *testing.T) {
	raw := "- [ ] task"
	app := &nativeApp{note: &models.Note{ID: "one"}, body: raw, md: &markdown.Options{}}
	app.setPreviewTask("other", raw, raw, 3, true)
	app.setPreviewTask("one", "stale", raw, 3, true)
	app.setPreviewTask("one", raw, raw, -1, true)
	if app.body != raw || app.dirty {
		t.Fatal("stale or invalid callback changed the note")
	}
}

func TestPreviewTaskSaveFailureKeepsDraft(t *testing.T) {
	app, db := openNativeTestApp(t)
	note := addTestNote(t, app.svc, "", "Tasks", "- [ ] task")
	app.showNote(note.ID)
	tt := ui.NewTester(app.previewContent, 796, 600)
	click(t, tt, "Task: task")
	if _, err := db.Exec("CREATE TRIGGER reject_task_save BEFORE UPDATE ON notes BEGIN SELECT RAISE(FAIL, 'fixture save failure'); END"); err != nil {
		t.Fatal(err)
	}
	app.flush()
	if !app.dirty || !strings.Contains(app.saveErr, "fixture save failure") || app.body != "- [x] task" {
		t.Fatal("failed save lost the checkbox edit or hid its error")
	}
}
