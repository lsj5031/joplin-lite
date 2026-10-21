package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/service"
)

func TestUIMetricsRecordingAndSlowLogs(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	m := &uiMetrics{enabled: true}

	// Fast samples under the 16ms ui.frame threshold should not emit warnings.
	m.recordDuration(spanUIFrame, 2*time.Millisecond)
	m.recordDuration(spanUIFrame, 4*time.Millisecond)
	m.recordDuration(spanUIFrame, 6*time.Millisecond)
	if strings.Contains(buf.String(), "ui slow operation") {
		t.Fatalf("unexpected slow warning for fast frames: %s", buf.String())
	}

	// A slow reload_notes call exceeding 16ms should increment slowCount and warn.
	m.recordDuration(spanReloadNotes, 28*time.Millisecond,
		slog.Int("rows", 100),
		slog.Int("query_len", 5),
	)
	if !strings.Contains(buf.String(), "ui slow operation") || !strings.Contains(buf.String(), "span=op.reload_notes") {
		t.Fatalf("missing slow warning for op.reload_notes: %s", buf.String())
	}

	snaps := m.snapshot()
	byName := make(map[string]spanSnapshot, len(snaps))
	for _, s := range snaps {
		byName[s.Span] = s
	}

	frameSnap, ok := byName["ui.frame"]
	if !ok {
		t.Fatalf("missing ui.frame snapshot in %+v", snaps)
	}
	if frameSnap.Count != 3 || frameSnap.SlowCount != 0 {
		t.Fatalf("ui.frame counts = (%d, %d), want (3, 0)", frameSnap.Count, frameSnap.SlowCount)
	}
	if frameSnap.P50 != 4*time.Millisecond || frameSnap.Max != 6*time.Millisecond {
		t.Fatalf("ui.frame p50/max = (%v, %v), want (4ms, 6ms)", frameSnap.P50, frameSnap.Max)
	}

	reloadSnap, ok := byName["op.reload_notes"]
	if !ok {
		t.Fatalf("missing op.reload_notes snapshot in %+v", snaps)
	}
	if reloadSnap.Count != 1 || reloadSnap.SlowCount != 1 {
		t.Fatalf("op.reload_notes counts = (%d, %d), want (1, 1)", reloadSnap.Count, reloadSnap.SlowCount)
	}

	buf.Reset()
	m.logSummary("test")
	if !strings.Contains(buf.String(), "ui metrics summary") || !strings.Contains(buf.String(), "reason=test") {
		t.Fatalf("missing summary log output: %s", buf.String())
	}
}

func TestNativeWorkspaceMetricsPoints(t *testing.T) {
	_, db := openNativeTestApp(t)
	svc := service.New(db)

	folder := &models.Folder{Title: "Field Notes"}
	if err := svc.CreateNotebook(folder); err != nil {
		t.Fatalf("CreateNotebook: %v", err)
	}
	n1 := addTestNote(t, svc, folder.ID, "First Note", "# Heading\n\nSome markdown body text.")
	n2 := addTestNote(t, svc, folder.ID, "Second Note", "Another note with words.")

	app := newNativeApp(db)
	app.railOpen = true

	tt := ui.NewTester(app.view, 1280, 800)
	tt.Frame()

	// Exercise note switching, editing (markDirty), saving (flush), search
	// (reloadNotes), command palette (ui.command_palette), and sync refresh.
	app.showNote(n2.ID)
	app.showNote(n1.ID)
	app.body = "# Updated\n\nBody with more words for metrics."
	app.markDirty()
	app.flush()
	app.query = "Updated"
	app.reloadNotes()
	app.query = ""
	app.reloadNotes()
	app.openPalette()
	tt.Frame()
	app.paletteOpen = false
	app.refreshAfterSync()
	tt.Frame()

	snaps := app.metrics.snapshot()
	recorded := make(map[string]uint64, len(snaps))
	for _, s := range snaps {
		recorded[s.Span] = s.Count
	}

	wantSpans := []string{
		"op.boot",
		"ui.frame",
		"ui.rail_pane",
		"ui.notes_pane",
		"ui.editor_pane",
		"ui.word_count",
		"ui.markdown_preview",
		"ui.command_palette",
		"op.reload_notes",
		"op.show_note",
		"op.mark_dirty",
		"op.flush_save",
		"op.reload_notebooks",
		"op.reload_tags",
		"op.refresh_after_sync",
	}
	for _, name := range wantSpans {
		if recorded[name] == 0 {
			t.Errorf("expected metric span %q to be recorded, got count=0 (recorded=%v)", name, recorded)
		}
	}
}
