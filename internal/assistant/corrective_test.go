package assistant

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lsj5031/joplin-lite/internal/models"
)

func admittedTestRun(m *Manager) *Run {
	run := testRun(Scope{Kind: "vault"})
	run.ConversationID = m.CurrentConversationID()
	run.ctx, run.cancel = context.WithCancel(context.Background())
	run.done = make(chan struct{})
	m.mu.Lock()
	m.active = run
	m.conversation.Running = true
	m.mu.Unlock()
	return run
}

func TestCancellationCannotBeOverwrittenByAdmissionWrites(t *testing.T) {
	db := openTestVault(t)
	m, err := NewManager(db, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(context.Background())
	settings := DefaultSettings()
	settings.BaseURL, settings.ModelID = "http://127.0.0.1:1/v1", "fixture"
	if err := m.ApplySettings(settings); err != nil {
		t.Fatal(err)
	}
	tx, err := m.asst.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	started := make(chan error, 1)
	go func() { _, err := m.Start(AskRequest{Text: "question", Scope: Scope{Kind: "vault"}}); started <- err }()
	var run *Run
	deadline := time.Now().Add(time.Second)
	for run == nil {
		m.mu.Lock()
		run = m.active
		m.mu.Unlock()
		if time.Now().After(deadline) {
			t.Fatal("run was not admitted")
		}
		time.Sleep(time.Millisecond)
	}
	stopped := make(chan struct{})
	go func() { _ = m.Cancel(run.ID); close(stopped) }()
	for {
		m.mu.Lock()
		claimed := run.terminal
		m.mu.Unlock()
		if claimed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cancellation was not claimed")
		}
		time.Sleep(time.Millisecond)
	}
	if _, active := m.ActiveRun(); !active {
		t.Fatal("admission released while initial writes were pending")
	}
	_ = tx.Rollback()
	select {
	case err := <-started:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Start did not finish persistence")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop did not finish persistence")
	}
	var status string
	if err := m.asst.db.QueryRow(`SELECT status FROM jl_ask_runs WHERE id = ?`, run.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "cancelled" {
		t.Fatalf("initial persistence overwrote terminal status: %s", status)
	}
	if got := m.Snapshot(""); got.Running || len(got.Messages) != 2 {
		t.Fatalf("inconsistent cancelled transcript: %+v", got)
	}
}

func TestTerminalCallbackSeesFinalizedPersistedOutcome(t *testing.T) {
	for _, terminal := range []string{"run.completed", "run.cancelled", "run.failed"} {
		t.Run(terminal, func(t *testing.T) {
			db := openTestVault(t)
			observed := make(chan error, 4)
			delivered := make(chan struct{})
			var m *Manager
			var err error
			m, err = NewManager(db, t.TempDir(), func(event Event) {
				if event.Type == "test.barrier" {
					close(delivered)
					return
				}
				if event.Type != EventRunCompleted && event.Type != EventRunCancelled && event.Type != EventRunFailed {
					return
				}
				snapshot := m.Snapshot("")
				messages, loadErr := m.asst.loadMessages(snapshot.ConversationID)
				if loadErr != nil || snapshot.Running || len(snapshot.Messages) != 1 || len(messages) != 1 {
					observed <- fmt.Errorf("premature terminal: snapshot=%+v saved=%+v err=%v", snapshot, messages, loadErr)
					return
				}
				if snapshot.Messages[0].Text != messages[0].Text {
					observed <- fmt.Errorf("snapshot and persisted message differ")
					return
				}
				if _, active := m.ActiveRun(); active {
					observed <- fmt.Errorf("admission still occupied in terminal callback")
					return
				}
				observed <- nil
			})
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close(context.Background())
			run := admittedTestRun(m)
			m.settings.BaseURL = "http://127.0.0.1:1/v1"
			m.settings.ModelID = "fixture"
			run.answer = "final answer"
			rt := &agentRuntime{run: run}

			// Occupy the sole connection to prove admission stays occupied until
			// persistence finishes, not merely until the outcome is claimed.
			tx, err := m.asst.db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			finished := make(chan struct{})
			go func() {
				m.handleAgentEvent(rt, map[string]any{"type": terminal, "text": "final answer", "message": "failure"})
				close(finished)
			}()
			deadline := time.After(time.Second)
			for {
				m.mu.Lock()
				claimed := run.terminal
				m.mu.Unlock()
				if claimed {
					break
				}
				select {
				case <-deadline:
					t.Fatal("terminal outcome was not claimed")
				default:
					time.Sleep(time.Millisecond)
				}
			}
			if _, active := m.ActiveRun(); !active {
				t.Fatal("admission released before persistence")
			}
			if _, err := m.Start(AskRequest{ConversationID: run.ConversationID, Text: "next", Scope: run.Scope}); !errors.Is(err, ErrBusy) {
				t.Fatalf("next admission bypassed persistence: %v", err)
			}
			_ = tx.Rollback()
			select {
			case <-finished:
			case <-time.After(time.Second):
				t.Fatal("terminalization blocked")
			}
			select {
			case err := <-observed:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("no terminal event")
			}
			var duplicates sync.WaitGroup
			for i := 0; i < 3; i++ {
				duplicates.Add(1)
				go func() {
					defer duplicates.Done()
					m.completeRun(run, "late")
					m.forceCancelled(run, "late")
					m.failRun(run, "late", "late")
				}()
			}
			duplicates.Wait()
			m.publish(nil, Event{Type: "test.barrier"})
			select {
			case <-delivered:
			case <-time.After(time.Second):
				t.Fatal("event delivery did not drain")
			}
			if len(observed) != 0 || len(m.Snapshot("").Messages) != 1 {
				t.Fatal("duplicate terminal outcome")
			}
		})
	}
}

func TestEventQueuePreservesPublicationOrder(t *testing.T) {
	db := openTestVault(t)
	events := make(chan Event, 200)
	m, err := NewManager(db, t.TempDir(), func(event Event) { events <- event })
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(context.Background())
	for i := 0; i < 200; i++ {
		m.publish(nil, Event{Type: EventActivity, Text: fmt.Sprint(i)})
	}
	for i := 0; i < 200; i++ {
		select {
		case event := <-events:
			if event.Sequence != uint64(i+1) || event.Text != fmt.Sprint(i) {
				t.Fatalf("event delivery reordered: %+v at %d", event, i)
			}
		case <-time.After(time.Second):
			t.Fatal("event dispatcher stopped")
		}
	}
}

func TestBlockedUISinkDoesNotBlockCancelOrClose(t *testing.T) {
	db := openTestVault(t)
	entered, release := make(chan struct{}), make(chan struct{})
	events := make(chan Event, 4)
	m, err := NewManager(db, t.TempDir(), func(event Event) {
		if event.Type == EventRunStarted {
			close(entered)
			<-release // Simulates synchronous RunOnMain waiting for the UI.
		}
		events <- event
	})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(context.Background())
	defer close(release)
	run := admittedTestRun(m)
	m.publish(run, Event{Type: EventRunStarted})
	<-entered
	stopped := make(chan struct{})
	go func() { _ = m.Cancel(run.ID); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("UI Stop deadlocked with event delivery")
	}
	closed := make(chan struct{})
	go func() { _ = m.Close(context.Background()); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("shutdown waited on the UI callback")
	}
}

func TestSearchContinuationRevalidatesCandidates(t *testing.T) {
	for _, mutation := range []string{"moved", "deleted", "encrypted", "conflict", "updated"} {
		t.Run(mutation, func(t *testing.T) {
			m, folder, elsewhere := noteTestManager(t)
			for i := 0; i < 3; i++ {
				note := &models.Note{ParentID: folder.ID, Title: fmt.Sprintf("Camera %d", i), Body: "camera original"}
				if err := m.vault.CreateNote(note); err != nil {
					t.Fatal(err)
				}
			}
			run := testRun(Scope{Kind: "notebook", NotebookID: folder.ID})
			first, err := m.searchNotes(context.Background(), run, SearchInput{Query: "camera", Limit: 1})
			if err != nil || first.NextCursor == "" {
				t.Fatalf("first page: %+v %v", first, err)
			}
			hits := run.search[cursorFor("camera", "")]
			target := hits[1].NoteID
			switch mutation {
			case "moved":
				_, err = m.vault.Exec(`UPDATE notes SET parent_id = ? WHERE id = ?`, elsewhere.ID, target)
			case "deleted":
				_, err = m.vault.Exec(`UPDATE notes SET deleted_time = 1 WHERE id = ?`, target)
			case "encrypted":
				_, err = m.vault.Exec(`UPDATE notes SET encryption_applied = 1 WHERE id = ?`, target)
			case "conflict":
				_, err = m.vault.Exec(`UPDATE notes SET is_conflict = 1 WHERE id = ?`, target)
			case "updated":
				_, err = m.vault.Exec(`UPDATE notes SET title = 'Camera current', body = 'camera current permitted snapshot' WHERE id = ?`, target)
			}
			if err != nil {
				t.Fatal(err)
			}
			page, err := m.searchNotes(context.Background(), run, SearchInput{Query: "camera", Cursor: first.NextCursor, Limit: 1})
			if err != nil {
				t.Fatal(err)
			}
			if mutation == "updated" {
				if len(page.Results) != 1 || page.Results[0].Title != "Camera current" || !strings.Contains(page.Results[0].Snippet, "current permitted") {
					t.Fatalf("stale search content: %+v", page)
				}
			} else if len(page.Results) != 0 {
				t.Fatalf("ineligible cached candidate escaped: %+v", page)
			}
		})
	}
}

func TestHistoricalCitationKeepsFullReadPassage(t *testing.T) {
	m, folder, _ := noteTestManager(t)
	body := strings.Repeat("界", 1250) + " Historical fact: approval was on Tuesday."
	note := &models.Note{ParentID: folder.ID, Title: "Evidence", Body: body}
	if err := m.vault.CreateNote(note); err != nil {
		t.Fatal(err)
	}
	run := testRun(Scope{Kind: "note", NoteID: note.ID})
	if _, err := m.readNote(context.Background(), run, ReadInput{NoteID: note.ID}); err != nil {
		t.Fatal(err)
	}
	citations := run.evidence.cite("Approval was on Tuesday. [E1]")
	if err := m.asst.appendMessageWithCitations(m.CurrentConversationID(), "assistant", "Approval was on Tuesday. [E1]", citations); err != nil {
		t.Fatal(err)
	}
	if _, err := m.vault.Exec(`UPDATE notes SET body = 'replaced' WHERE id = ?`, note.ID); err != nil {
		t.Fatal(err)
	}
	messages, err := m.asst.loadMessages(m.CurrentConversationID())
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || len(messages[0].Citations) != 1 || messages[0].Citations[0].Excerpt != body {
		t.Fatalf("historical passage was truncated: %+v", messages)
	}
	citation := messages[0].Citations[0]
	if citation.StartByte != 0 || citation.EndByte != len(body) {
		t.Fatalf("historical passage lost its byte offsets: %+v", citation)
	}
	current, err := m.CitationCurrent(messages[0].Citations[0])
	if err != nil || current {
		t.Fatalf("updated note still current: %v %v", current, err)
	}
}
