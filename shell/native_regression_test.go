package main

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown/parser"
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
	click(t, tt, "Note actions")
	click(t, tt, "Delete note…")
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
			// The header keeps the everyday actions in view; the structural
			// ones are one menu away rather than four 28px symbols wide.
			for _, label := range []string{"Move note", "Note tags", "Edit", "Note actions"} {
				r, ok := tt.Find(label)
				if !ok || r.W < 20 || r.H < 20 || r.X < 0 || r.X+r.W > 1024 || r.Y+r.H > 640 {
					t.Errorf("%s is inaccessible: %+v found=%v", label, r, ok)
				}
			}
			click(t, tt, "Note actions")
			for _, label := range []string{"Mark as To-do", "Attach files…", "Delete note…"} {
				if _, ok := tt.Find(label); !ok {
					t.Errorf("%s is missing from the note menu", label)
				}
			}
			tt.Key(0, ui.KeyEscape)
			r, ok := tt.Find("A readable title")
			if !ok || r.X >= app.railW+app.listW+cardGap || r.H == 0 {
				t.Fatal("note list lost its title")
			}
			if ex, ok := tt.Find("Distinctive note excerpt"); ok && ex.X < app.railW+app.listW {
				t.Fatalf("note list should not render a second excerpt line: %+v", ex)
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

// TestNativeEditorHeaderIsOneQuietRow pins the distillation: the pills hug
// their text on the same 48px spine as the title, structural actions live in
// one menu, and zero-state chrome stays out of the list.
func TestNativeEditorHeaderIsOneQuietRow(t *testing.T) {
	registerFonts()
	app, _ := openNativeTestApp(t)
	nb := regressionNotebook(t, app, "Welcome")
	note := addTestNote(t, app.svc, nb.ID, "Reading notes", "A body\n\nPreview paragraph")
	addTestNote(t, app.svc, nb.ID, "Title only", "")
	app.reloadNotebooks()
	app.reloadNotes()
	app.showNote(note.ID)
	tt := ui.NewTester(app.view, 1200, 800)
	tt.Frame()

	move, ok := tt.Find("Move note")
	if !ok {
		t.Fatal("notebook pill is missing")
	}
	title, ok := tt.Find("Note title")
	if !ok {
		t.Fatal("note title is missing")
	}
	if diff := move.X - title.X; diff > 0.5 || diff < -0.5 {
		t.Fatalf("header and title do not share a spine: pill x=%v title x=%v", move.X, title.X)
	}
	body, ok := tt.Find("Preview paragraph")
	if !ok {
		t.Fatal("preview body is missing")
	}
	if diff := body.X - title.X; diff > 0.5 || diff < -0.5 {
		t.Fatalf("preview body and title do not share a spine: body x=%v title x=%v", body.X, title.X)
	}
	if move.W > 200 {
		t.Fatalf("notebook pill stretches to %v; it should hug its text", move.W)
	}
	if tt.HasText("Empty note") {
		t.Fatal("title-only rows still print \"Empty note\"")
	}
	if _, ok := tt.Find("Conflicts"); ok {
		t.Fatal("the Conflicts row appears with zero conflicts")
	}
	if _, ok := tt.Find("Attach file"); ok {
		t.Fatal("the attach action still takes a header slot")
	}
	click(t, tt, "Note actions")
	for _, item := range []string{"Mark as To-do", "Attach files…", "Delete note…"} {
		if _, ok := tt.Find(item); !ok {
			t.Fatalf("%s is missing from the note menu", item)
		}
	}
}

func TestMarkdownPreviewSoftBreaksAndCaching(t *testing.T) {
	registerFonts()
	app, _ := openNativeTestApp(t)
	nb := regressionNotebook(t, app, "Preview examples")
	body := "### 目录示例\nhttps://example.com/index\n\n### 参考链接\nhttps://example.org/notes#section\nhttps://example.net/read?id=101-102\n\n101-102\n103\n104-105\n106-107\n108-109\n110-111\n112-113\n114-115\n\n=== 测试记录\n101 第一条示例\n102-103 第二条示例\n104 分段记录（上）\n105 分段记录（下）\n106-107 中文与 English 混合内容\n\n```text\ncode line 1\ncode line 2\n```"
	note := addTestNote(t, app.svc, nb.ID, "Multilingual preview", body)
	addTestNote(t, app.svc, nb.ID, "Another note", "Second note body that should not appear in note row")
	app.reloadNotebooks()
	app.reloadNotes()
	app.showNote(note.ID)

	tt := ui.NewTester(app.view, 1280, 800)
	tt.Frame()

	// Verify code block content has no trailing spaces injected.
	formatted := formatMarkdownPreview(body)
	if !strings.Contains(formatted, "```text\ncode line 1\ncode line 2\n```") {
		t.Fatalf("code block should not be modified: %q", formatted)
	}
	if !strings.Contains(formatted, "101-102  \n103  \n104-105") {
		t.Fatalf("paragraph soft breaks should become hard line breaks: %q", formatted)
	}

	// Verify word count and preview body are cached across frames and mode switches.
	wc := app.bodyWordCount()
	if wc == 0 || !app.wordCountReady || !app.previewReady {
		t.Fatalf("expected cached word count and preview state, got wc=%d wordCountReady=%v previewReady=%v", wc, app.wordCountReady, app.previewReady)
	}
	themePtr := app.mdTheme
	if dir := os.Getenv("CAPTURE_DIR"); dir != "" {
		writeCapture(t, dir, "multilingual-preview", screen(tt), tt)
	}
	click(t, tt, "Edit")
	tt.Frame()
	if dir := os.Getenv("CAPTURE_DIR"); dir != "" {
		writeCapture(t, dir, "multilingual-edit", screen(tt), tt)
	}
	click(t, tt, "Preview")
	tt.Frame()
	if app.mdTheme != themePtr {
		t.Fatal("markdown theme should be reused across Edit/Preview switches")
	}
}

// TestPreviewChunksLongParagraph covers the one-paragraph note whose shaped
// layout misses MyGo's cache: it is split at line ends, and the pieces rejoin
// to the same Markdown.
func TestPreviewChunksLongParagraph(t *testing.T) {
	doc := parser.New(parser.Default)
	var b strings.Builder
	for i := 0; i < 2000; i++ {
		fmt.Fprintf(&b, "%03d 中文测试记录　『日本語の表示テスト』　2026/01/08　〖8.5%%〗 mixed text\n", i)
	}
	src := formatMarkdownPreviewWithDoc(doc, b.String())
	chunks := previewChunks(doc, src, previewChunkRunes)
	if len(chunks) < 2 {
		t.Fatalf("a %d-rune paragraph was not split: %d chunks", len([]rune(src)), len(chunks))
	}
	if strings.Join(chunks, "") != src {
		t.Fatal("split chunks do not rejoin to the formatted preview")
	}
	for _, chunk := range chunks {
		if n := len([]rune(chunk)); n > previewChunkRunes*2 {
			t.Fatalf("chunk of %d runes is much larger than the %d-rune budget", n, previewChunkRunes)
		}
	}
}

// TestPreviewChunksNeverCutInline keeps a link split across source lines in
// one piece, and wraps a single-line paragraph at a rune boundary instead.
func TestPreviewChunksNeverCutInline(t *testing.T) {
	doc := parser.New(parser.Default)
	body := "[the link\nlabel](https://example.com) " + strings.Repeat("中", 200)
	src := formatMarkdownPreviewWithDoc(doc, body)
	chunks := previewChunks(doc, src, 40)
	if len(chunks) < 2 {
		t.Fatalf("a %d-rune single-line paragraph was not split", len([]rune(src)))
	}
	start := strings.Index(src, "[the link")
	end := strings.Index(src, "https://example.com)") + len("https://example.com)")
	for _, chunk := range chunks {
		if strings.Contains(chunk, "[the link") && !strings.Contains(chunk, "https://example.com)") {
			t.Fatalf("link was cut across chunks: %q", chunk)
		}
	}
	if start < 0 || end <= start {
		t.Fatal("link not found in the formatted source")
	}
}

func TestPreviewChunksShortBodyUnsplit(t *testing.T) {
	doc := parser.New(parser.Default)
	src := formatMarkdownPreviewWithDoc(doc, "short\nbody")
	if chunks := previewChunks(doc, src, previewChunkRunes); chunks != nil {
		t.Fatalf("short body was split: %q", chunks)
	}
}
