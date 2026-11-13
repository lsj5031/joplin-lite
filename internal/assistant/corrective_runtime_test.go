package assistant

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lsj5031/joplin-lite/internal/models"
)

func waitForChatAnswer(t *testing.T, m *Manager, events <-chan Event, question string, scope Scope) Event {
	t.Helper()
	handle, err := m.Start(AskRequest{ConversationID: m.CurrentConversationID(), Text: question, Scope: scope})
	if err != nil {
		t.Fatal(err)
	}
	timeout := time.After(30 * time.Second)
	for {
		select {
		case event := <-events:
			if event.RunID != handle.ID {
				continue
			}
			if event.Type == EventRunFailed || event.Type == EventRunCancelled {
				t.Fatalf("run failed: %+v", event)
			}
			if event.Type == EventRunCompleted {
				if snapshot := m.Snapshot(""); snapshot.Running || len(snapshot.Messages) < 2 || snapshot.Messages[len(snapshot.Messages)-1].Text != event.Text {
					t.Fatalf("terminal preceded finalized snapshot: %+v", snapshot)
				}
				return event
			}
		case <-timeout:
			t.Fatal("local fixture did not complete")
		}
	}
}

func TestConversationScopeSurvivesSwitchAndRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded runtime")
	}
	for _, restart := range []bool{false, true} {
		t.Run(map[bool]string{false: "switch", true: "restart"}[restart], func(t *testing.T) {
			db := openTestVault(t)
			note := &models.Note{Title: "Camera", Body: "camera repair approved"}
			if err := db.CreateNote(note); err != nil {
				t.Fatal(err)
			}
			fixture := &sseServer{noteID: note.ID}
			server := httptest.NewServer(http.HandlerFunc(fixture.handler))
			defer server.Close()
			root := t.TempDir()
			events := make(chan Event, 128)
			sink := func(event Event) { events <- event }
			m, err := NewManager(db, root, sink)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = m.Close(context.Background()) }()
			settings := DefaultSettings()
			settings.BaseURL, settings.ModelID = server.URL+"/v1", "test-model"
			if err := m.ApplySettings(settings); err != nil {
				t.Fatal(err)
			}
			a := m.CurrentConversationID()
			waitForChatAnswer(t, m, events, "WIDER_SECRET_A camera repair", Scope{Kind: "vault"})
			var tables int
			if err := m.asst.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'pientries'`).Scan(&tables); err != nil || tables != 1 {
				t.Fatalf("real Pi storage was not initialized: %d %v", tables, err)
			}
			if _, err := m.NewConversation(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := m.asst.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name GLOB 'pi*'`).Scan(&tables); err != nil || tables != 0 {
				t.Fatalf("switch retained real Pi tables: %d %v", tables, err)
			}
			narrow := Scope{Kind: "note", NoteID: note.ID}
			waitForChatAnswer(t, m, events, "B camera repair", narrow)
			if restart {
				if err := m.Close(context.Background()); err != nil {
					t.Fatal(err)
				}
				m, err = NewManager(db, root, sink)
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := m.SelectConversation(context.Background(), a); err != nil {
				t.Fatal(err)
			}
			fixture.mu.Lock()
			before := len(fixture.requests)
			fixture.mu.Unlock()
			waitForChatAnswer(t, m, events, "A narrow camera repair", narrow)
			fixture.mu.Lock()
			outgoing, err := json.Marshal(fixture.requests[before:])
			fixture.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(outgoing), "WIDER_SECRET_A") {
				t.Fatal("wider A transcript escaped into a narrow A request")
			}
		})
	}
}

func TestLegacyGlobalScopeFailsClosed(t *testing.T) {
	db := openTestVault(t)
	root := t.TempDir()
	m, err := NewManager(db, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.asst.appendMessage(m.CurrentConversationID(), "user", "legacy wider context"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.asst.db.Exec(`INSERT INTO jl_ask_settings (key, value) VALUES ('scope', '{"kind":"vault"}')`); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	m, err = NewManager(db, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(context.Background())
	if m.scopeKnown {
		t.Fatal("legacy global scope authorized a conversation recap")
	}
}

func TestForcedCancellationReleasesRuntimeTransactionBeforePersistence(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded runtime")
	}
	db := openTestVault(t)
	note := &models.Note{Title: "Camera", Body: "camera repair approved"}
	if err := db.CreateNote(note); err != nil {
		t.Fatal(err)
	}
	fixture := &sseServer{noteID: note.ID}
	server := httptest.NewServer(http.HandlerFunc(fixture.handler))
	defer server.Close()
	events := make(chan Event, 128)
	m, err := NewManager(db, t.TempDir(), func(event Event) { events <- event })
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(context.Background())
	settings := DefaultSettings()
	settings.BaseURL, settings.ModelID = server.URL+"/v1", "test-model"
	if err := m.ApplySettings(settings); err != nil {
		t.Fatal(err)
	}
	run := admittedTestRun(m)
	rt, err := m.ensureRuntime(run, settings)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.asst.db.Exec(`CREATE TABLE orphan_probe (value TEXT)`); err != nil {
		t.Fatal(err)
	}
	begun, err := m.asst.execute(rt.storageCtx, dbRequest{Op: "begin"})
	if err != nil {
		t.Fatal(err)
	}
	txID := begun["tx"].(string)
	if _, err := m.asst.execute(rt.storageCtx, dbRequest{Op: "exec", Tx: txID, SQL: `INSERT INTO orphan_probe VALUES ('must roll back')`}); err != nil {
		t.Fatal(err)
	}
	stopped := make(chan struct{})
	go func() { m.forceCancelled(run, "grace period expired"); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("orphan transaction blocked lifecycle persistence")
	}
	var count int
	if err := m.asst.db.QueryRow(`SELECT COUNT(*) FROM orphan_probe`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("orphan transaction was not rolled back: %d %v", count, err)
	}
	m.asst.txMu.Lock()
	remaining := len(m.asst.txs)
	m.asst.txMu.Unlock()
	if remaining != 0 {
		t.Fatal("disposed runtime retained transaction handles")
	}
	if _, err := m.asst.execute(rt.storageCtx, dbRequest{Op: "begin"}); err == nil {
		t.Fatal("disposed generation admitted storage work")
	}
	waitForChatAnswer(t, m, events, "camera repair after cancellation", Scope{Kind: "note", NoteID: note.ID})
	closed := make(chan struct{})
	go func() { _ = m.Close(context.Background()); close(closed) }()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown blocked after transaction disposal")
	}
}

func TestTerminalCallbackCanAdmitNextRun(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded runtime")
	}
	db := openTestVault(t)
	note := &models.Note{Title: "Camera", Body: "camera repair approved"}
	if err := db.CreateNote(note); err != nil {
		t.Fatal(err)
	}
	fixture := &sseServer{noteID: note.ID}
	server := httptest.NewServer(http.HandlerFunc(fixture.handler))
	defer server.Close()
	events := make(chan Event, 128)
	admitted := make(chan error, 1)
	var m *Manager
	var err error
	startedNext := false // Owned solely by the ordered callback dispatcher.
	m, err = NewManager(db, t.TempDir(), func(event Event) {
		events <- event
		if event.Type == EventRunCompleted && !startedNext {
			startedNext = true
			_, err := m.Start(AskRequest{ConversationID: m.CurrentConversationID(), Text: "next camera repair", Scope: Scope{Kind: "note", NoteID: note.ID}})
			admitted <- err
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(context.Background())
	settings := DefaultSettings()
	settings.BaseURL, settings.ModelID = server.URL+"/v1", "test-model"
	if err := m.ApplySettings(settings); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start(AskRequest{Text: "first camera repair", Scope: Scope{Kind: "note", NoteID: note.ID}}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-admitted:
		if err != nil {
			t.Fatalf("callback could not admit next run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("terminal callback deadlocked on next admission")
	}
	completed := 0
	timeout := time.After(10 * time.Second)
	for completed < 2 {
		select {
		case event := <-events:
			if event.Type == EventRunFailed {
				t.Fatalf("next run failed: %+v", event)
			}
			if event.Type == EventRunCompleted {
				completed++
			}
		case <-timeout:
			t.Fatal("next run did not complete")
		}
	}
	if got := m.Snapshot(""); got.Running || len(got.Messages) != 4 {
		t.Fatalf("next transcript was not finalized: %+v", got)
	}
}

func TestEndpointChangeHonorsExplicitEmptyKey(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded runtime")
	}
	db := openTestVault(t)
	note := &models.Note{Title: "Camera", Body: "camera repair approved"}
	if err := db.CreateNote(note); err != nil {
		t.Fatal(err)
	}
	fixture := &sseServer{noteID: note.ID}
	testAuth := make(chan string, 1)
	serverA := httptest.NewServer(http.NotFoundHandler())
	defer serverA.Close()
	serverB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// TestConnection uses a non-streaming response; actual inference uses
		// the existing deterministic tool/read/answer streaming fixture.
		body := make(map[string]any)
		raw := json.NewDecoder(r.Body)
		if err := raw.Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if body["stream"] != true {
			testAuth <- r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"OK"}}]}`))
			return
		}
		encoded, _ := json.Marshal(body)
		r.Body = io.NopCloser(strings.NewReader(string(encoded)))
		fixture.handler(w, r)
	}))
	defer serverB.Close()
	events := make(chan Event, 128)
	m, err := NewManager(db, t.TempDir(), func(event Event) { events <- event })
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(context.Background())
	secrets := &fakeSecretStore{}
	m.SetSecretStore(secrets)
	settings := DefaultSettings()
	settings.BaseURL, settings.ModelID, settings.APIKey, settings.RememberKey = serverA.URL+"/v1", "test-model", "endpoint-a-only", true
	if err := m.ApplySettings(settings); err != nil {
		t.Fatal(err)
	}
	settings.BaseURL, settings.APIKey = serverB.URL+"/v1", ""
	if err := m.TestConnection(context.Background(), settings); err != nil {
		t.Fatal(err)
	}
	if auth := <-testAuth; auth != "" {
		t.Fatal("TestConnection sent endpoint A's key to endpoint B")
	}
	if err := m.ApplySettings(settings); err != nil {
		t.Fatal(err)
	}
	if secrets.value != "" || m.Settings().APIKey != "" {
		t.Fatal("empty key did not clear remembered and in-memory credentials")
	}
	waitForChatAnswer(t, m, events, "camera repair", Scope{Kind: "note", NoteID: note.ID})
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if len(fixture.auth) == 0 {
		t.Fatal("no inference requests")
	}
	for _, auth := range fixture.auth {
		if auth != "" {
			t.Fatal("inference sent Authorization to keyless endpoint B")
		}
	}
}
