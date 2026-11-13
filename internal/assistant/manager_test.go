package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/internal/store"
)

// sseServer is a deterministic OpenAI-compatible fixture. It never contacts a
// real provider and records the exact requests it receives.
type sseServer struct {
	mu       sync.Mutex
	requests []map[string]any
	auth     []string
	paths    []string
	noteID   string
}

func (s *sseServer) handler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.requests = append(s.requests, body)
	s.auth = append(s.auth, r.Header.Get("Authorization"))
	s.paths = append(s.paths, r.URL.Path)
	count := len(s.requests)
	s.mu.Unlock()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)
	chunk := func(delta map[string]any, finish any) {
		payload := map[string]any{
			"id": "chatcmpl-fixture", "object": "chat.completion.chunk",
			"created": time.Now().Unix(), "model": "test-model",
			"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}},
		}
		raw, _ := json.Marshal(payload)
		fmt.Fprintf(w, "data: %s\n\n", raw)
		if flusher != nil {
			flusher.Flush()
		}
	}
	finish := func(reason string) {
		chunk(map[string]any{}, reason)
		fmt.Fprint(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}

	phase := (count - 1) % 3
	switch phase {
	case 0:
		chunk(map[string]any{
			"role": "assistant",
			"tool_calls": []any{map[string]any{
				"index": 0, "id": "call_search", "type": "function",
				"function": map[string]any{"name": "search_notes", "arguments": `{"query":"camera repair","limit":4}`},
			}},
		}, nil)
		finish("tool_calls")
	case 1:
		chunk(map[string]any{
			"role": "assistant",
			"tool_calls": []any{map[string]any{
				"index": 0, "id": "call_read", "type": "function",
				"function": map[string]any{"name": "read_note", "arguments": fmt.Sprintf(`{"noteId":%q,"maxBytes":4096}`, s.noteID)},
			}},
		}, nil)
		finish("tool_calls")
	default:
		chunk(map[string]any{"role": "assistant", "content": "The camera repair was approved. "}, nil)
		chunk(map[string]any{"content": "Follow up with the shop about the lens. [E1]"}, nil)
		finish("stop")
	}
}

func waitForCompleted(t *testing.T, m *Manager, events <-chan Event, question string, scope Scope) Event {
	t.Helper()
	handle, err := m.Start(AskRequest{Text: question, Scope: scope})
	if err != nil {
		t.Fatal(err)
	}
	if handle.ID == "" {
		t.Fatal("no run id")
	}
	timeout := time.After(90 * time.Second)
	for {
		select {
		case event := <-events:
			t.Logf("event %s %s", event.Type, event.Message)
			if event.Type == EventRunFailed {
				t.Fatalf("run failed: %s %s", event.Code, event.Message)
			}
			if event.Type == EventRunCompleted {
				return event
			}
		case <-timeout:
			t.Fatal("timed out waiting for the embedded agent")
		}
	}
}

func TestEmbeddedAgentCompletesCitedAnswer(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded runtime")
	}
	fixture := &sseServer{}
	server := httptest.NewServer(http.HandlerFunc(fixture.handler))
	defer server.Close()

	root := t.TempDir()
	db, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	folder := &models.Folder{Title: "Camera"}
	if err := db.CreateFolder(folder); err != nil {
		t.Fatal(err)
	}
	note := &models.Note{ParentID: folder.ID, Title: "Camera repair", Body: "The camera repair was approved at the shop. Follow up about the lens."}
	if err := db.CreateNote(note); err != nil {
		t.Fatal(err)
	}
	fixture.noteID = note.ID

	events := make(chan Event, 128)
	m, err := NewManager(db, root, func(event Event) { events <- event })
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = m.Close(ctx)
	}()
	settings := DefaultSettings()
	settings.BaseURL = server.URL + "/v1"
	settings.ModelID = "test-model"
	settings.APIKey = "test-key"
	if err := m.ApplySettings(settings); err != nil {
		t.Fatal(err)
	}
	completed := waitForCompleted(t, m, events, "What did I decide about the camera repair, and what follow-up is outstanding?", Scope{Kind: "note", NoteID: note.ID})
	t.Logf("completed status=%q text=%q citations=%d", completed.Status, completed.Text, len(completed.Citations))
	if !strings.Contains(completed.Text, "[E1]") {
		t.Fatalf("answer lost its citation: %q", completed.Text)
	}
	if len(completed.Citations) != 1 || completed.Citations[0].NoteID != note.ID || !completed.Citations[0].Verified {
		t.Fatalf("citation was not verified against read_note: %+v", completed.Citations)
	}
	snapshot := m.Snapshot("")
	if len(snapshot.Messages) < 2 || len(snapshot.Messages[1].Citations) != 1 || snapshot.Messages[1].Citations[0].NoteID != note.ID {
		t.Fatalf("completed snapshot lost its citations: %+v", snapshot.Messages)
	}

	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if len(fixture.requests) < 3 {
		t.Fatalf("expected search, read and answer turns, got %d requests", len(fixture.requests))
	}
	for i, auth := range fixture.auth {
		if auth != "Bearer test-key" {
			t.Fatalf("request %d sent %q, not the configured credential", i, auth)
		}
		if strings.Contains(auth, "joplin-host-managed-key") {
			t.Fatalf("the SDK placeholder leaked to the provider")
		}
	}
	if !strings.HasSuffix(fixture.paths[0], "/chat/completions") {
		t.Fatalf("unexpected provider path %q", fixture.paths[0])
	}
}

func TestScopeChangeResetsAgentContextBeforeNextRun(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded runtime")
	}
	fixture := &sseServer{}
	server := httptest.NewServer(http.HandlerFunc(fixture.handler))
	defer server.Close()

	root := t.TempDir()
	db, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	first := &models.Note{Title: "First", Body: "The first note mentions camera repair."}
	second := &models.Note{Title: "Second", Body: "The second note mentions camera repair."}
	if err := db.CreateNote(first); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateNote(second); err != nil {
		t.Fatal(err)
	}
	fixture.noteID = first.ID

	events := make(chan Event, 256)
	m, err := NewManager(db, root, func(event Event) { events <- event })
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = m.Close(ctx)
	}()
	settings := DefaultSettings()
	settings.BaseURL = server.URL + "/v1"
	settings.ModelID = "test-model"
	settings.APIKey = "test-key"
	if err := m.ApplySettings(settings); err != nil {
		t.Fatal(err)
	}
	if event := waitForCompleted(t, m, events, "First question", Scope{Kind: "note", NoteID: first.ID}); len(event.Citations) != 1 {
		t.Fatalf("first run citations: %+v", event.Citations)
	}

	fixture.noteID = second.ID
	resetSeen := false
	done := make(chan Event, 1)
	go func() {
		for event := range events {
			if event.Type == "conversation.reset" {
				resetSeen = true
			}
			if event.Type == EventRunFailed {
				done <- event
				return
			}
			if event.Type == EventRunCompleted {
				done <- event
				return
			}
		}
	}()
	if _, err := m.Start(AskRequest{Text: "Second question", Scope: Scope{Kind: "note", NoteID: second.ID}}); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-done:
		if event.Type == EventRunFailed {
			t.Fatalf("second run failed: %s %s", event.Code, event.Message)
		}
		if len(event.Citations) != 1 || event.Citations[0].NoteID != second.ID {
			t.Fatalf("second run citation: %+v", event.Citations)
		}
	case <-time.After(90 * time.Second):
		t.Fatal("timed out waiting for the second run")
	}
	if !resetSeen {
		t.Fatal("scope change did not publish a conversation reset")
	}
	snapshot := m.Snapshot(defaultConversationID)
	if len(snapshot.Messages) != 2 || snapshot.Messages[0].Text != "Second question" {
		t.Fatalf("scope change kept old messages: %+v", snapshot.Messages)
	}
}
