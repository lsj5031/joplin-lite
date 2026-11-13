package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lsj5031/joplin-lite/internal/store"
)

const (
	defaultConversationID = "ask"
	idleRuntimeLifetime   = 60 * time.Second
	maxQuestionRunes      = 8000
)

// Manager owns the assistant database, the lazy runtime, and the single active
// run. An event callback must be safe to call from a background goroutine; the
// native shell posts it to the UI thread with mygo.RunOnMain.
type Manager struct {
	vault *store.DB
	asst  *assistantDB
	emit  func(Event)

	mu           sync.Mutex
	emitMu       sync.Mutex
	settings     Settings
	active       *Run
	runtime      *agentRuntime
	closed       bool
	resetting    bool
	seq          uint64
	idle         *time.Timer
	conversation ConversationSnapshot
	lastScope    Scope
	scopeKnown   bool
}

// Run is one admitted Ask request and its whole-run state.
type Run struct {
	ID             string
	ConversationID string
	Text           string
	Scope          Scope
	limits         Limits
	ctx            context.Context
	cancel         context.CancelFunc
	evidence       *evidenceRegistry
	budget         *runBudget
	done           chan struct{}
	answer         string
	terminal       bool
	resetAgent     bool
	tools          map[string]bool

	searchMu sync.Mutex
	search   map[string][]store.AssistantSearchHit
}

func NewManager(vault *store.DB, dataDir string, emit func(Event)) (*Manager, error) {
	if vault == nil {
		return nil, errors.New("ask needs the note store")
	}
	asst, err := openAssistantDB(dataDir)
	if err != nil {
		return nil, err
	}
	settings, err := asst.loadSettings()
	if err != nil {
		_ = asst.close()
		return nil, err
	}
	messages, err := asst.loadMessages(defaultConversationID)
	if err != nil {
		_ = asst.close()
		return nil, fmt.Errorf("load Ask history: %w", err)
	}
	lastScope, scopeKnown, err := asst.loadScope()
	if err != nil {
		_ = asst.close()
		return nil, fmt.Errorf("load Ask scope: %w", err)
	}
	m := &Manager{
		vault:        vault,
		asst:         asst,
		emit:         emit,
		settings:     settings,
		conversation: ConversationSnapshot{ConversationID: defaultConversationID, Messages: messages},
		lastScope:    lastScope,
		scopeKnown:   scopeKnown,
	}
	return m, nil
}

func (m *Manager) Settings() Settings {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.settings
}

// ApplySettings validates, persists non-secret fields, and keeps the API key in
// memory only. Changing the endpoint discards the runtime so the next request
// uses the new transport.
func (m *Manager) ApplySettings(settings Settings) error {
	settings.Normalize()
	if err := settings.Validate(); err != nil {
		return err
	}
	if settings.APIKey == "" {
		m.mu.Lock()
		settings.APIKey = m.settings.APIKey
		m.mu.Unlock()
	}
	m.mu.Lock()
	if m.active != nil {
		m.mu.Unlock()
		return errors.New("stop the current Ask run before changing the model connection")
	}
	m.mu.Unlock()
	if err := m.asst.saveSettings(settings); err != nil {
		return fmt.Errorf("save Ask settings: %w", err)
	}
	m.mu.Lock()
	m.settings = settings
	m.mu.Unlock()
	m.dropRuntime()
	return nil
}

func (m *Manager) Configured() bool {
	return m.Settings().Configured()
}

// TestConnection makes one small, non-streaming request through the same
// endpoint-scoped transport as a real run. A successful catalog lookup is not
// proof of inference, so this deliberately asks the model for a response.
func (m *Manager) TestConnection(ctx context.Context, settings Settings) error {
	settings.Normalize()
	if settings.APIKey == "" {
		settings.APIKey = m.Settings().APIKey
	}
	if err := settings.Validate(); err != nil {
		return err
	}
	client, err := newEndpointClient(settings)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{
		"model":      settings.ModelID,
		"messages":   []map[string]string{{"role": "user", "content": "Reply with OK."}},
		"max_tokens": 8,
		"stream":     false,
	})
	if err != nil {
		return err
	}
	endpoint := strings.TrimRight(settings.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail := strings.TrimSpace(string(body))
		if detail != "" {
			return fmt.Errorf("model endpoint returned HTTP %d: %s", resp.StatusCode, clipMessage(detail, 300))
		}
		return fmt.Errorf("model endpoint returned HTTP %d", resp.StatusCode)
	}
	if readErr != nil {
		return fmt.Errorf("read model response: %w", readErr)
	}
	var parsed struct {
		Choices []json.RawMessage `json:"choices"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return fmt.Errorf("model endpoint returned an unreadable response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return errors.New("model endpoint returned no choices")
	}
	return nil
}

func clipMessage(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}

func (m *Manager) ActiveRun() (RunHandle, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil {
		return RunHandle{}, false
	}
	return RunHandle{ID: m.active.ID, ConversationID: m.active.ConversationID}, true
}

// Start admits exactly one run application-wide. The caller may display another
// saved conversation but cannot start a competing run.
func (m *Manager) Start(req AskRequest) (RunHandle, error) {
	settings := m.Settings()
	if !settings.Configured() {
		return RunHandle{}, ErrUnconfigured
	}
	if err := req.Scope.Validate(); err != nil {
		return RunHandle{}, err
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return RunHandle{}, errors.New("Ask needs a question")
	}
	if len([]rune(text)) > maxQuestionRunes {
		return RunHandle{}, errors.New("the question is too long")
	}
	if req.ConversationID == "" {
		req.ConversationID = defaultConversationID
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return RunHandle{}, ErrClosed
	}
	if m.active != nil {
		m.mu.Unlock()
		return RunHandle{}, ErrBusy
	}
	if m.resetting {
		m.mu.Unlock()
		return RunHandle{}, errors.New("Ask is starting a fresh conversation; try again in a moment")
	}
	if m.idle != nil {
		m.idle.Stop()
		m.idle = nil
	}
	// A different note or notebook is a different evidence boundary even when
	// the segmented scope kind did not change. Reset the durable agent context
	// before admission so an earlier answer cannot carry wider-scope evidence
	// into this run.
	scopeChanged := (m.scopeKnown && !sameScope(m.lastScope, req.Scope)) ||
		(!m.scopeKnown && len(m.conversation.Messages) > 0)
	ctx, cancel := context.WithTimeout(context.Background(), settings.deadline())
	run := &Run{
		ID:             uuid(),
		ConversationID: req.ConversationID,
		Text:           text,
		Scope:          req.Scope,
		limits:         DefaultLimits(),
		ctx:            ctx,
		cancel:         cancel,
		evidence:       newEvidenceRegistry(),
		budget:         newRunBudget(DefaultLimits()),
		done:           make(chan struct{}),
		resetAgent:     scopeChanged,
		tools:          make(map[string]bool),
		search:         make(map[string][]store.AssistantSearchHit),
	}
	if scopeChanged {
		m.conversation = ConversationSnapshot{ConversationID: req.ConversationID}
	}
	m.lastScope = req.Scope
	m.scopeKnown = true
	m.active = run
	m.conversation.Running = true
	m.conversation.RunID = run.ID
	m.conversation.Messages = append(m.conversation.Messages, Message{Role: "user", Text: text})
	m.mu.Unlock()

	if scopeChanged {
		if err := m.asst.clearMessages(run.ConversationID); err != nil {
			m.failRun(run, "storage_failed", "The Ask conversation could not be reset.")
			return RunHandle{ID: run.ID, ConversationID: run.ConversationID}, nil
		}
		m.publish(nil, Event{Type: "conversation.reset"})
	}
	if err := m.asst.appendMessage(run.ConversationID, "user", text); err != nil {
		m.failRun(run, "storage_failed", "The question could not be saved.")
		return RunHandle{ID: run.ID, ConversationID: run.ConversationID}, nil
	}
	_ = m.asst.saveScope(run.Scope)
	if err := m.asst.saveRun(run.ID, run.ConversationID, run.ID, run.Scope, "running", ""); err != nil {
		m.failRun(run, "storage_failed", "The Ask run could not be saved.")
		return RunHandle{ID: run.ID, ConversationID: run.ConversationID}, nil
	}
	m.publish(run, Event{Type: EventRunStarted, Activity: &Activity{Kind: "starting", Label: "Starting", Active: 1}})
	go m.drive(run)
	return RunHandle{ID: run.ID, ConversationID: run.ConversationID}, nil
}

func (m *Manager) drive(run *Run) {
	settings := m.Settings()
	runtime, err := m.ensureRuntime(run, settings)
	if err != nil {
		m.failRun(run, "runtime_failed", "The Ask runtime could not start: "+err.Error())
		return
	}
	runtime.bindRun(run)
	if !m.runAlive(run) {
		return
	}
	if run.resetAgent {
		if err := runtime.resetConversation(run.ctx); err != nil {
			m.failRun(run, "runtime_failed", "The previous Ask context could not be reset: "+err.Error())
			return
		}
	}
	if err := runtime.submit(run); err != nil {
		m.failRun(run, "runtime_failed", "The Ask runtime could not accept the question: "+err.Error())
		return
	}
	select {
	case <-run.done:
		return
	case <-run.ctx.Done():
		reason := "The Ask run timed out."
		if errors.Is(run.ctx.Err(), context.Canceled) {
			reason = "Stopped by the user."
		}
		runtime.abort(reason)
		select {
		case <-run.done:
		case <-time.After(5 * time.Second):
			m.forceCancelled(run, reason)
		}
	}
}

func (m *Manager) runAlive(run *Run) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active == run && !run.terminal
}

func (m *Manager) ensureRuntime(run *Run, settings Settings) (*agentRuntime, error) {
	m.mu.Lock()
	if m.runtime != nil && !m.runtime.isClosed() {
		rt := m.runtime
		m.mu.Unlock()
		return rt, nil
	}
	m.mu.Unlock()

	rt, err := newAgentRuntime(m, settings, run.ConversationID)
	if err != nil {
		return nil, err
	}
	rt.bindRun(run)
	if err := rt.open(run.ctx, settings, run.ConversationID); err != nil {
		_ = err
		_ = rt
		rt.close(context.Background())
		return nil, err
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		rt.close(context.Background())
		return nil, ErrClosed
	}
	m.runtime = rt
	m.mu.Unlock()
	return rt, nil
}

// Cancel is idempotent: an unknown or already terminal run is acknowledged.
func (m *Manager) Cancel(runID string) error {
	m.mu.Lock()
	run := m.active
	rt := m.runtime
	m.mu.Unlock()
	if run == nil || run.ID != runID {
		return nil
	}
	run.cancel()
	if rt != nil {
		m.publish(run, Event{Type: EventActivity, Activity: &Activity{Kind: "stopping", Label: "Stopping", Active: 0}})
		rt.abort("Stopped by the user.")
	} else {
		m.forceCancelled(run, "Stopped by the user.")
	}
	return nil
}

func (m *Manager) Snapshot(conversationID string) ConversationSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	snapshot := m.conversation
	snapshot.Messages = append([]Message(nil), m.conversation.Messages...)
	snapshot.Running = m.active != nil
	if m.active != nil {
		snapshot.RunID = m.active.ID
	}
	return snapshot
}

func (m *Manager) ListConversations() []ConversationSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.conversation.Messages) == 0 {
		return nil
	}
	out := m.conversation
	out.Messages = append([]Message(nil), m.conversation.Messages...)
	return []ConversationSnapshot{out}
}

func (m *Manager) dropRuntime() {
	m.mu.Lock()
	rt := m.runtime
	m.runtime = nil
	if m.idle != nil {
		m.idle.Stop()
		m.idle = nil
	}
	m.mu.Unlock()
	if rt != nil {
		go rt.close(context.Background())
	}
}

// ResetConversation discards the durable agent conversation and the in-memory
// transcript. Scope changes call it because earlier turns may already contain
// evidence from a wider scope.
func (m *Manager) ResetConversation(ctx context.Context) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ErrClosed
	}
	if m.active != nil {
		m.mu.Unlock()
		return ErrBusy
	}
	m.resetting = true
	rt := m.runtime
	m.runtime = nil
	if m.idle != nil {
		m.idle.Stop()
		m.idle = nil
	}
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.resetting = false
		m.mu.Unlock()
	}()
	if rt != nil {
		rt.close(ctx)
	}
	if err := m.asst.clearAgentState(); err != nil {
		return fmt.Errorf("reset the Ask conversation: %w", err)
	}
	if err := m.asst.clearMessages(defaultConversationID); err != nil {
		return fmt.Errorf("clear Ask history: %w", err)
	}
	m.mu.Lock()
	m.conversation = ConversationSnapshot{ConversationID: defaultConversationID}
	m.mu.Unlock()
	m.publish(nil, Event{Type: "conversation.reset"})
	return nil
}

// CitationCurrent reports whether the note still matches the version the
// cited passage came from. A changed note must not highlight an unrelated
// current range.
func (m *Manager) CitationCurrent(citation Citation) (bool, error) {
	if citation.NoteID == "" || citation.Version == "" {
		return false, nil
	}
	note, err := m.vault.GetNote(citation.NoteID)
	if err != nil {
		return false, err
	}
	if note == nil || note.DeletedTime != 0 || note.EncryptionApplied != 0 || note.IsConflict != 0 {
		return false, nil
	}
	return noteVersion(note.ID, note.ParentID, note.Title, note.Body, note.UpdatedTime) == citation.Version, nil
}

func (m *Manager) scheduleIdleClose() {
	m.mu.Lock()
	if m.closed || m.active != nil {
		m.mu.Unlock()
		return
	}
	if m.idle != nil {
		m.idle.Stop()
	}
	m.idle = time.AfterFunc(idleRuntimeLifetime, func() {
		m.mu.Lock()
		rt := m.runtime
		m.runtime = nil
		m.idle = nil
		m.mu.Unlock()
		if rt != nil {
			slog.Info("closing idle Ask runtime")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			rt.close(ctx)
		}
	})
	m.mu.Unlock()
}

// Close stops admission, cancels the active run, drains the agent, then closes
// the assistant database. The backend calls it before closing the vault store.
func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	run := m.active
	rt := m.runtime
	m.runtime = nil
	if m.idle != nil {
		m.idle.Stop()
		m.idle = nil
	}
	m.mu.Unlock()

	if run != nil {
		run.cancel()
		if rt != nil {
			rt.abort("The application is closing.")
		}
		select {
		case <-run.done:
		case <-ctx.Done():
		case <-time.After(3 * time.Second):
		}
	}
	if rt != nil {
		rt.close(ctx)
	}
	return m.asst.close()
}

func (m *Manager) loopError(err error) {
	m.mu.Lock()
	run := m.active
	m.mu.Unlock()
	if run != nil {
		m.failRun(run, "runtime_failed", "The Ask runtime stopped unexpectedly: "+trimLog(err.Error()))
	}
}

func (m *Manager) publish(run *Run, event Event) {
	m.emitMu.Lock()
	defer m.emitMu.Unlock()
	m.mu.Lock()
	m.seq++
	event.Sequence = m.seq
	event.APIVersion = APIVersion
	if run != nil {
		event.RunID = run.ID
		event.ConversationID = run.ConversationID
	}
	m.mu.Unlock()
	if m.emit != nil {
		m.emit(event)
	}
}

func (m *Manager) handleAgentEvent(rt *agentRuntime, raw map[string]any) {
	eventType := stringValue(raw, "type")
	run := rt.currentRun()
	switch eventType {
	case "runtime.ready":
		rt.markReady()
		return
	case "runtime.closed":
		rt.markClosed()
		return
	case "conversation.reset":
		var err error
		if message := stringValue(raw, "error"); message != "" {
			err = errors.New(message)
		}
		rt.finishReset(err)
		return
	}
	if run == nil {
		return
	}
	if requestID := requestHost(raw); requestID != "" && requestID != run.ID {
		return
	}
	switch eventType {
	case "answer.delta":
		text := stringValue(raw, "text")
		m.mu.Lock()
		if m.active != run || run.terminal {
			m.mu.Unlock()
			return
		}
		run.answer += text
		m.mu.Unlock()
		m.publish(run, Event{Type: EventAnswerDelta, Text: text})
	case "answer.snapshot":
		text := stringValue(raw, "text")
		m.mu.Lock()
		if m.active != run || run.terminal {
			m.mu.Unlock()
			return
		}
		run.answer = text
		m.mu.Unlock()
		m.publish(run, Event{Type: EventAnswerSnapshot, Text: text})
	case "activity.updated":
		m.publish(run, Event{Type: EventActivity, Activity: activityFrom(raw)})
	case "diagnostic":
		slog.Warn("ask agent diagnostic", "message", trimLog(stringValue(raw, "message")))
	case "run.completed":
		text := stringValue(raw, "text")
		m.completeRun(run, text)
	case "run.cancelled":
		reason := stringValue(raw, "reason")
		if reason == "" {
			reason = "Stopped."
		}
		m.finish(run, "cancelled", reason, false)
	case "run.failed":
		code := normalizeCode(stringValue(raw, "code"))
		message := stringValue(raw, "message")
		if message == "" {
			message = "The Ask run failed."
		}
		if stringValue(raw, "phase") == "open" {
			rt.failReady(errors.New(message))
		}
		m.publish(run, Event{Type: EventRunFailed, Code: code, Message: message})
		m.finish(run, "failed", message, false)
	}
}

func (m *Manager) completeRun(run *Run, text string) {
	m.mu.Lock()
	if m.active != run || run.terminal {
		m.mu.Unlock()
		return
	}
	if text == "" {
		text = run.answer
	}
	run.answer = text
	m.mu.Unlock()
	citations := run.evidence.cite(text)
	for _, citation := range citations {
		c := citation
		m.publish(run, Event{Type: EventCitation, Citation: &c})
	}
	m.publish(run, Event{
		Type:      EventRunCompleted,
		Text:      text,
		Citations: citations,
		Status:    "completed",
	})
	m.finish(run, "completed", "", false)
}

func (m *Manager) failRun(run *Run, code, message string) {
	m.mu.Lock()
	if m.active != run || run.terminal {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()
	m.publish(run, Event{Type: EventRunFailed, Code: normalizeCode(code), Message: message})
	m.finish(run, "failed", message, false)
}

func (m *Manager) forceCancelled(run *Run, reason string) {
	m.mu.Lock()
	if m.active != run || run.terminal {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()
	m.publish(run, Event{Type: EventRunCancelled, Code: "cancelled", Message: reason})
	m.finish(run, "cancelled", reason, false)
	// A run that had to be forced still has JavaScript pending inside this
	// runtime. Discard it rather than reusing a possibly inconsistent runtime.
	m.mu.Lock()
	rt := m.runtime
	m.runtime = nil
	m.mu.Unlock()
	if rt != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			rt.close(ctx)
		}()
	}
}

// finish closes a run exactly once. The terminal event has already been
// published by the caller.
func (m *Manager) finish(run *Run, status, message string, failed bool) {
	m.mu.Lock()
	if run.terminal {
		m.mu.Unlock()
		return
	}
	run.terminal = true
	if m.active == run {
		m.active = nil
	}
	answer := run.answer
	if failed && answer == "" {
		answer = ""
	}
	// A late terminal event from an old run must not append to a newer run's
	// transcript or clear its running marker.
	ownsConversation := m.active == nil
	if ownsConversation {
		if status == "completed" || status == "cancelled" {
			m.conversation.Messages = append(m.conversation.Messages, Message{Role: "assistant", Text: answer})
		} else {
			m.conversation.Messages = append(m.conversation.Messages, Message{Role: "error", Text: message})
		}
		m.conversation.Running = false
		m.conversation.RunID = ""
	}
	if rt := m.runtime; rt != nil {
		rt.clearRunIf(run)
	}
	m.mu.Unlock()

	run.cancel()
	close(run.done)
	if err := m.asst.saveRun(run.ID, run.ConversationID, run.ID, run.Scope, status, answer); err != nil {
		slog.Error("save Ask run outcome", "error", err)
	}
	if ownsConversation && answer != "" {
		if err := m.asst.appendMessage(run.ConversationID, "assistant", answer); err != nil {
			slog.Error("save Ask answer", "error", err)
		}
	}
	m.scheduleIdleClose()
}

func sameScope(a, b Scope) bool {
	return a.Kind == b.Kind && a.NoteID == b.NoteID && a.NotebookID == b.NotebookID && a.IncludeDescendants == b.IncludeDescendants
}
