package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/lsj5031/joplin-lite/internal/assistant"
	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/service"
)

func TestAskCitationMarkdownOnlyLinksVerifiedTokens(t *testing.T) {
	text := "Approved [E1]. Invented [E9]."
	got := askCitationMarkdown(text, []assistant.Citation{{Token: "E1", NoteID: "a", Version: "v"}}, "0")
	if !strings.Contains(got, "[[E1]](ask-cite:0:E1)") {
		t.Fatalf("verified citation was not linked: %q", got)
	}
	if strings.Contains(got, "ask-cite:0:E9") {
		t.Fatalf("unknown citation was linked: %q", got)
	}
}

func TestAskHistoryAndInlineCitationSurface(t *testing.T) {
	app, db := openNativeTestApp(t)
	mgr, err := assistant.NewManager(db, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = mgr.Close(ctx)
	})
	app.be = &backend{ask: mgr}
	app.ask.open = true
	app.refreshAsk()
	app.ask.messages = []assistant.Message{{
		Role: "assistant",
		Text: "The repair was approved [E1].",
		Citations: []assistant.Citation{{
			Token: "E1", NoteID: strings.Repeat("a", 32), Title: "Camera repair", Version: "v", Verified: true,
		}},
	}}
	app.ask.citations = app.ask.messages[0].Citations
	tt := ui.NewTester(app.view, 1280, 800)
	tt.Frame()
	if !tt.HasText("History") || !tt.HasText("New") {
		t.Fatalf("Ask header lacks history controls: %v", tt.Texts())
	}
	if err := tt.Click("History"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("New chat") {
		t.Fatalf("history menu did not open: %v", tt.Texts())
	}
}

func TestAskInlineCitationOpensNote(t *testing.T) {
	app, db := openNativeTestApp(t)
	svc := service.New(db)
	folder := &models.Folder{Title: "Everyday"}
	if err := svc.CreateNotebook(folder); err != nil {
		t.Fatal(err)
	}
	first := addTestNote(t, svc, folder.ID, "Weekly notes", "Plan the week.")
	cited := addTestNote(t, svc, folder.ID, "Camera repair", "The camera repair was approved.")
	app.selectedID = first.ID
	app.showNote(first.ID)
	app.ask.open = true
	app.ask.messages = []assistant.Message{{
		Role: "assistant",
		Text: "The repair was approved [E1].",
		Citations: []assistant.Citation{{
			Token: "E1", NoteID: cited.ID, Title: cited.Title, Version: "version", Verified: true,
		}},
	}}
	tt := ui.NewTester(app.view, 1280, 800)
	tt.Frame()
	if err := tt.Click("[E1]"); err != nil {
		t.Fatalf("inline citation was not clickable: %v", err)
	}
	if app.note == nil || app.note.ID != cited.ID {
		t.Fatalf("inline citation opened %v, want %s", app.note, cited.ID)
	}
}
