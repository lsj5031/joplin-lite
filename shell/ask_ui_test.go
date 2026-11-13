package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/lsj5031/joplin-lite/internal/assistant"
	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/service"
)

func TestAskOpeningOwnsTypingAndRestoresEditorFocus(t *testing.T) {
	app, db := openNativeTestApp(t)
	note := addTestNote(t, service.New(db), "", "Focus fixture", "Original body.")
	app.showNote(note.ID)
	app.editing, app.focusBody = true, true
	tt := ui.NewTester(app.view, 1280, 800)
	tt.Frame()
	if !tt.Focused("Note body") {
		t.Fatal("fixture did not focus the body")
	}
	for _, nativeMenu := range []bool{false, true} {
		if nativeMenu {
			for _, menu := range app.appMenu().Items() {
				for _, item := range menu.Submenu {
					if item.Label == "Ask Notes…" {
						item.Click(item, nil)
					}
				}
			}
			tt.Frame()
		} else {
			tt.Key(ui.Cmd|ui.Shift, ui.KeyA)
		}
		if !app.ask.open || !tt.Focused("Question") {
			t.Fatalf("Ask did not take focus (native menu %v)", nativeMenu)
		}
		tt.Type("Question text")
		if app.body != "Original body." || !strings.Contains(app.ask.input, "Question text") {
			t.Fatalf("typing reached the note: body=%q question=%q", app.body, app.ask.input)
		}
		tt.Key(0, ui.KeyEnter)
		if !strings.Contains(app.ask.input, "\n") {
			t.Fatal("Enter must retain multiline composition")
		}
		tt.Key(ui.Cmd|ui.Shift, ui.KeyA)
		if app.ask.open || !tt.Focused("Note body") {
			t.Fatal("closing Ask did not restore editor focus")
		}
	}
}

func TestAskHistoryStaysReachableAndKeyboardOpeningIsInstant(t *testing.T) {
	for _, height := range []float32{600, 800} {
		t.Run(fmt.Sprint(height), func(t *testing.T) {
			app, _ := openNativeTestApp(t)
			app.ask.open = true
			app.ask.conversationID = "chat-0"
			for i := 0; i < 24; i++ {
				app.ask.conversations = append(app.ask.conversations, assistant.ConversationSummary{
					ConversationID: fmt.Sprintf("chat-%d", i), Title: fmt.Sprintf("Camera repair discussion with a long shared prefix, camera %02d", i), Preview: "Saved answer preview",
				})
			}
			tt := ui.NewTester(app.view, 1280, int(height))
			if err := tt.Click("History"); err != nil {
				t.Fatal(err)
			}
			tt.Key(0, ui.KeyEscape)
			if !tt.Focused("History") {
				t.Fatal("History button lost keyboard focus")
			}
			tt.Key(0, ui.KeyEnter)
			if !app.ask.historyOpen {
				t.Fatal("Enter did not open History")
			}
			for _, name := range []string{"New chat", "Rename current chat…", "Delete current chat…"} {
				r, ok := tt.Find(name)
				if !ok || r.H < 10 || r.Y+r.H > height {
					t.Fatalf("unreachable %s: %+v", name, r)
				}
			}
			before := append([]byte(nil), tt.Image().Pix...)
			tt.Advance(235 * time.Millisecond)
			if !bytes.Equal(before, tt.Image().Pix) {
				t.Fatal("keyboard-opened History animated")
			}
			first, _ := tt.Find(app.ask.conversations[0].Title)
			tt.Scroll(first.X+20, first.Y+10, 0, 10000)
			last, ok := tt.Find(app.ask.conversations[23].Title)
			if !ok || last.H < 10 || last.Y+last.H > height {
				t.Fatalf("oldest full title could not be reached: %+v", last)
			}
			if err := tt.Click("Delete current chat…"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if !tt.Focused("Cancel") {
				t.Fatal("destructive confirmation did not initially focus Cancel")
			}
			if dir := os.Getenv("ASK_FIX_CAPTURE_DIR"); dir != "" {
				capture(t, dir, fmt.Sprintf("history-%d", int(height)), tt)
			}
			tt.Key(0, ui.KeyEnter)
			if app.ask.deleteID != "" {
				t.Fatal("Enter did not cancel deletion")
			}
		})
	}
}

func TestAskStreamFollowsBottomAndAllowsReadingAbove(t *testing.T) {
	app, _ := openNativeTestApp(t)
	app.ask.open, app.ask.busy = true, true
	app.ask.thinking = strings.Repeat("Reasoning detail. ", 300)
	app.ask.answer = strings.Repeat("## Section\n\nAnswer detail with a little more context.\n\n", 18)
	tt := ui.NewTester(app.view, 1280, 800)
	tt.Frame()
	if app.ask.scroll.MaxY <= 0 || app.ask.scroll.Y < app.ask.scroll.MaxY-2 {
		t.Fatalf("initial answer did not follow bottom: %+v", app.ask.scroll)
	}
	app.ask.answer += "\n\nLatest answer paragraph."
	tt.Frame()
	latest, _ := tt.Find("Latest answer paragraph.")
	if latest.H < 10 {
		t.Fatalf("new content did not follow bottom: %+v", latest)
	}
	tt.Scroll(latest.X+20, latest.Y, 0, -300)
	prior := app.ask.scroll.Y
	app.ask.answer += "\n\nMore streamed content."
	tt.Frame()
	if app.ask.scroll.Y != prior || !tt.HasText("Jump to latest") {
		t.Fatal("streaming interrupted deliberate reading above")
	}
	if err := tt.Click("Jump to latest"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.ask.scroll.Y < app.ask.scroll.MaxY-2 {
		t.Fatal("Jump to latest did not reach bottom")
	}
	// Reasoning starts collapsed, so a long trace cannot push a short answer away.
	app.ask.answer = "A short answer remains visible."
	tt.Frame()
	if !tt.HasText("Show reasoning") || tt.HasText(app.ask.thinking) {
		t.Fatal("reasoning did not start collapsed")
	}
	if err := tt.Click("Show reasoning"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	answer, _ := tt.Find("A short answer remains visible.")
	if answer.H < 10 {
		t.Fatalf("expanded reasoning displaced the answer: %+v", answer)
	}
	if dir := os.Getenv("ASK_FIX_CAPTURE_DIR"); dir != "" {
		capture(t, dir, "stream-reasoning", tt)
	}
}

func TestAskKeyboardSendAndStop(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded runtime")
	}
	app, db := openNativeTestApp(t)
	note := addTestNote(t, service.New(db), "", "Keyboard fixture", "Keep this body.")
	app.showNote(note.ID)
	requested := make(chan struct{}, 1)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested <- struct{}{}
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	events := make(chan assistant.Event, 128)
	mgr, err := assistant.NewManager(db, t.TempDir(), func(event assistant.Event) { events <- event })
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close(context.Background())
	settings := assistant.DefaultSettings()
	settings.BaseURL, settings.ModelID = server.URL+"/v1", "test-model"
	if err := mgr.ApplySettings(settings); err != nil {
		t.Fatal(err)
	}
	app.be = &backend{ask: mgr}
	app.toggleAsk()
	tt := ui.NewTester(app.view, 1280, 800)
	tt.Type("A deliberate question")
	tt.Key(0, ui.KeyEnter)
	tt.Type("with a second line")
	tt.Key(ui.Cmd, ui.KeyEnter)
	if !app.ask.busy || app.ask.input != "" {
		t.Fatalf("keyboard did not submit: %+v", app.ask)
	}
	select {
	case <-requested:
	case <-time.After(10 * time.Second):
		t.Fatal("no model request after keyboard send")
	}
	if got := mgr.Snapshot("").Messages[0].Text; got != "A deliberate question\nwith a second line" {
		t.Fatalf("sent %q", got)
	}
	tt.Key(0, ui.KeyTab)
	if !tt.Focused("Stop") {
		t.Fatal("Stop is not keyboard reachable while busy")
	}
	tt.Key(0, ui.KeyTab)
	if tt.Focused("Send") {
		t.Fatal("busy Send is still enabled in the focus order")
	}
	if dir := os.Getenv("ASK_FIX_CAPTURE_DIR"); dir != "" {
		capture(t, dir, "busy-send-disabled", tt)
	}
	if err := tt.Click("Stop"); err != nil {
		t.Fatal(err)
	}
	timeout := time.After(10 * time.Second)
	for {
		select {
		case event := <-events:
			app.applyAskEvent(event)
			if event.Type == assistant.EventRunFailed {
				t.Fatalf("unexpected failure: %+v", event)
			}
			if event.Type == assistant.EventRunCancelled {
				if app.ask.busy {
					t.Fatal("Stop left Ask busy")
				}
				return
			}
		case <-timeout:
			t.Fatal("Stop did not cancel the run")
		}
	}
}

func TestAskWidthAndVisibleConnectionLabels(t *testing.T) {
	app, db := openNativeTestApp(t)
	note := addTestNote(t, service.New(db), "", "Editor has usable space", "The editor remains readable beside Ask.")
	app.showNote(note.ID)
	app.ask.open, app.ask.width = true, askMaxWidth
	tt := ui.NewTester(app.view, 1024, 768)
	title, _ := tt.Find("Note title")
	if app.narrow || title.W < 250 {
		t.Fatalf("editor was compressed: %+v", title)
	}
	if dir := os.Getenv("ASK_FIX_CAPTURE_DIR"); dir != "" {
		capture(t, dir, "width-1024", tt)
	}
	app.railOpen = true
	tt.Frame()
	if !app.narrow {
		t.Fatal("overcrowded layout did not switch to single-pane Ask")
	}
	app.railOpen = false
	tt.SetSize(900, 600)
	if !app.narrow {
		t.Fatal("minimum window did not use single-pane Ask")
	}
	if dir := os.Getenv("ASK_FIX_CAPTURE_DIR"); dir != "" {
		capture(t, dir, "width-900", tt)
	}
	app.ask.settingsOpen = true
	tt.Frame()
	if dir := os.Getenv("ASK_FIX_CAPTURE_DIR"); dir != "" {
		capture(t, dir, "connection-labels", tt)
	}
	// Texts includes both rendered labels and accessibility-only labels; each
	// field therefore needs two occurrences to prove a persistent visible label.
	for _, name := range []string{"Base URL (OpenAI-compatible, usually ends in /v1)", "Model ID", "API key (optional for local endpoints)", "Context window (tokens)", "Max output (tokens)"} {
		count := 0
		for _, text := range tt.Texts() {
			if text == name {
				count++
			}
		}
		if count != 2 {
			t.Fatalf("%s lacks a rendered label (%d occurrences)", name, count)
		}
	}
	r, _ := tt.Find("Model ID")
	tt.Scroll(r.X+20, r.Y+10, 0, 500)
	if dir := os.Getenv("ASK_FIX_CAPTURE_DIR"); dir != "" {
		capture(t, dir, "token-labels", tt)
	}
}

func TestAskNavigationPreservesHistoryAndRestoresScope(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded runtime")
	}
	app, db := openNativeTestApp(t)
	svc := service.New(db)
	a := &models.Folder{Title: "Notebook A"}
	b := &models.Folder{Title: "Notebook B"}
	for _, folder := range []*models.Folder{a, b} {
		if err := svc.CreateNotebook(folder); err != nil {
			t.Fatal(err)
		}
	}
	first := addTestNote(t, svc, a.ID, "Note A", "First note")
	second := addTestNote(t, svc, b.ID, "Note B", "Second note")
	app = newNativeApp(db)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"id":"fixture","object":"chat.completion.chunk","model":"test-model","choices":[{"index":0,"delta":{"role":"assistant","content":"Saved answer."},"finish_reason":null}]}

data: {"id":"fixture","object":"chat.completion.chunk","model":"test-model","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`)
	}))
	defer server.Close()
	events := make(chan assistant.Event, 256)
	mgr, err := assistant.NewManager(db, t.TempDir(), func(event assistant.Event) { events <- event })
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close(context.Background())
	settings := assistant.DefaultSettings()
	settings.BaseURL, settings.ModelID = server.URL+"/v1", "test-model"
	if err := mgr.ApplySettings(settings); err != nil {
		t.Fatal(err)
	}
	app.be = &backend{ask: mgr}
	app.ask.open = true
	app.refreshAsk()
	scopes := []assistant.Scope{
		{Kind: "note", NoteID: first.ID},
		{Kind: "note", NoteID: second.ID},
		{Kind: "notebook", NotebookID: a.ID},
		{Kind: "notebook", NotebookID: b.ID},
		{Kind: "notebook", NotebookID: b.ID, IncludeDescendants: true},
		{Kind: "vault"},
	}
	ids := make([]string, 0, len(scopes))
	for i, scope := range scopes {
		switch scope.Kind {
		case "note":
			app.ask.scope = 0
			app.openExternalNote(scope.NoteID)
		case "notebook":
			app.ask.scope = 1
			app.selectedNotebook = scope.NotebookID
		case "vault":
			app.ask.scope = 2
		}
		app.ask.includeDesc = scope.IncludeDescendants
		app.ask.input = fmt.Sprintf("Question %d", i)
		app.askSend()
		if !app.ask.busy {
			t.Fatalf("question was not admitted: %s", app.ask.err)
		}
		timeout := time.After(10 * time.Second)
		completed := false
		for !completed {
			select {
			case event := <-events:
				app.applyAskEvent(event)
				if event.Type == assistant.EventRunFailed {
					t.Fatalf("run failed: %+v", event)
				}
				completed = event.Type == assistant.EventRunCompleted
			case <-timeout:
				t.Fatal("fixture timed out")
			}
		}
		ids = append(ids, mgr.CurrentConversationID())
		if i > 0 && ids[i] == ids[i-1] {
			t.Fatal("scope navigation reused the previous chat")
		}
		if i == 1 {
			tt := ui.NewTester(app.view, 1280, 800)
			tt.Frame()
			if !tt.HasText("Scope changed. Started a new chat; the previous one is in History.") {
				t.Fatalf("missing scope-change notice: %v", tt.Texts())
			}
			if dir := os.Getenv("ASK_FIX_CAPTURE_DIR"); dir != "" {
				capture(t, dir, "scope-navigation", tt)
			}
		}
	}
	for i, id := range ids {
		snapshot, err := mgr.SelectConversation(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		app.refreshAsk()
		got, err := app.askScope()
		if err != nil || got != scopes[i] {
			t.Fatalf("resumed scope = %+v, %v; want %+v", got, err, scopes[i])
		}
		if snapshot.Scope == nil || *snapshot.Scope != scopes[i] || len(snapshot.Messages) != 2 || snapshot.Messages[0].Text != fmt.Sprintf("Question %d", i) {
			t.Fatalf("lost history or scope: %+v", snapshot)
		}
	}
}

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
	if dir := os.Getenv("ASK_REVIEW_CAPTURE_DIR"); dir != "" {
		capture(t, dir, "ask-cited-answer", tt)
	}
	if err := tt.Click("History"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("New chat") {
		t.Fatalf("history menu did not open: %v", tt.Texts())
	}
	if dir := os.Getenv("ASK_REVIEW_CAPTURE_DIR"); dir != "" {
		capture(t, dir, "ask-history-open", tt)
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
