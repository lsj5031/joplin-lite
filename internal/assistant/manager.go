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

	mu           sync.Mutex
	events       *eventQueue
	settings     Settings
	secret       SecretStore
	active       *Run
	runtime      *agentRuntime
	closed       bool
	resetting    bool
	seq          uint64
	idle         *time.Timer
	conversation ConversationSnapshot
	lastScope    Scope
	scopeKnown   bool
	// agentConversation names the transcript currently inside the durable Pi
	// context. Switching chats clears Pi state, so a resumed chat gets a compact
	// transcript recap for reference while citations still require read_note.
	agentConversation string
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
	setupDone      chan struct{}
	answer         string
	prompt         string
	citations      []Citation
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
	conversations, err := asst.listConversations()
	if err != nil {
		_ = asst.close()
		return nil, fmt.Errorf("load Ask conversations: %w", err)
	}
	latest := defaultConversationID
	if latestID, idErr := asst.latestConversationID(); idErr != nil {
		_ = asst.close()
		return nil, fmt.Errorf("load latest Ask conversation: %w", idErr)
	} else if latestID != "" {
		latest = latestID
	}
	if len(conversations) > 0 {
		latest = conversations[0].ConversationID
	} else if err := asst.ensureConversation(latest); err != nil {
		_ = asst.close()
		return nil, err
	}
	messages, err := asst.loadMessages(latest)
	if err != nil {
		_ = asst.close()
		return nil, fmt.Errorf("load Ask history: %w", err)
	}
	title := ""
	for _, conversation := range conversations {
		if conversation.ConversationID == latest {
			title = conversation.Title
			break
		}
	}
	lastScope, scopeKnown, err := asst.loadScope(latest)
	if err != nil {
		_ = asst.close()
		return nil, fmt.Errorf("load Ask scope: %w", err)
	}
	m := &Manager{
		vault:        vault,
		asst:         asst,
		settings:     settings,
		conversation: ConversationSnapshot{ConversationID: latest, Title: title, Messages: messages},
		lastScope:    lastScope,
		scopeKnown:   scopeKnown,
	}
	m.events = newEventQueue(emit)
	return m, nil
}

func (m *Manager) Settings() Settings {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.settings
}

// SetSecretStore attaches the shell's operating-system credential store. The
// manager only holds the key in memory; this store is the sole persistence path.
func (m *Manager) SetSecretStore(store SecretStore) {
	m.mu.Lock()
	m.secret = store
	m.mu.Unlock()
}

// RestoreStoredKey loads an opted-in key without writing it back to SQLite.
func (m *Manager) RestoreStoredKey() error {
	m.mu.Lock()
	store := m.secret
	remember := m.settings.RememberKey
	m.mu.Unlock()
	if !remember || store == nil {
		return nil
	}
	key, err := store.Load()
	if err != nil {
		return err
	}
	if key == "" {
		return nil
	}
	m.mu.Lock()
	m.settings.APIKey = key
	m.mu.Unlock()
	return nil
}

func (m *Manager) CurrentConversationID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.conversation.ConversationID
}

// ListConversations returns newest-first metadata for the history menu.
func (m *Manager) ListConversations() []ConversationSummary {
	conversations, err := m.asst.listConversations()
	if err == nil {
		return conversations
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conversation.ConversationID == "" {
		return nil
	}
	return []ConversationSummary{{ConversationID: m.conversation.ConversationID, Title: m.conversation.Title, MessageCount: len(m.conversation.Messages)}}
}

// SelectConversation loads a saved transcript. The Pi context is deliberately
// cleared: evidence from another chat must never become implicit context for
// this one. The next question receives a compact, non-evidence recap instead.
func (m *Manager) SelectConversation(ctx context.Context, conversationID string) (ConversationSnapshot, error) {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return ConversationSnapshot{}, errors.New("choose an Ask chat")
	}
	m.mu.Lock()
	if m.conversation.ConversationID == conversationID && m.active == nil {
		snapshot := m.snapshotLocked()
		m.mu.Unlock()
		return snapshot, nil
	}
	m.mu.Unlock()
	exists, err := m.asst.conversationExists(conversationID)
	if err != nil {
		return ConversationSnapshot{}, err
	}
	if !exists {
		return ConversationSnapshot{}, errors.New("that Ask chat no longer exists")
	}
	return m.activateConversation(ctx, conversationID)
}

func (m *Manager) NewConversation(ctx context.Context) (ConversationSnapshot, error) {
	id := uuid()
	if err := m.asst.ensureConversation(id); err != nil {
		return ConversationSnapshot{}, err
	}
	return m.activateConversation(ctx, id)
}

func (m *Manager) RenameConversation(conversationID, title string) error {
	title = strings.Join(strings.Fields(strings.TrimSpace(title)), " ")
	if title == "" {
		return errors.New("a chat title cannot be empty")
	}
	if len([]rune(title)) > 80 {
		return errors.New("a chat title is limited to 80 characters")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrClosed
	}
	if m.active != nil {
		return ErrBusy
	}
	if err := m.asst.renameConversation(conversationID, title); err != nil {
		return err
	}
	if m.conversation.ConversationID == conversationID {
		m.conversation.Title = title
	}
	return nil
}

func (m *Manager) DeleteConversation(ctx context.Context, conversationID string) (ConversationSnapshot, error) {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return ConversationSnapshot{}, errors.New("choose an Ask chat")
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ConversationSnapshot{}, ErrClosed
	}
	if m.active != nil {
		m.mu.Unlock()
		return ConversationSnapshot{}, ErrBusy
	}
	current := m.conversation.ConversationID == conversationID
	m.mu.Unlock()

	target := ""
	if current {
		conversations, err := m.asst.listConversations()
		if err != nil {
			return ConversationSnapshot{}, err
		}
		for _, conversation := range conversations {
			if conversation.ConversationID != conversationID {
				target = conversation.ConversationID
				break
			}
		}
		if target == "" {
			target = uuid()
			if err := m.asst.ensureConversation(target); err != nil {
				return ConversationSnapshot{}, err
			}
		}
	}
	if err := m.asst.deleteConversation(conversationID); err != nil {
		return ConversationSnapshot{}, err
	}
	if !current {
		return m.Snapshot(""), nil
	}
	return m.activateConversation(ctx, target)
}

func (m *Manager) activateConversation(ctx context.Context, conversationID string) (ConversationSnapshot, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ConversationSnapshot{}, ErrClosed
	}
	if m.active != nil {
		m.mu.Unlock()
		return ConversationSnapshot{}, ErrBusy
	}
	if m.resetting {
		m.mu.Unlock()
		return ConversationSnapshot{}, errors.New("Ask is already switching chats")
	}
	m.resetting = true
	rt := m.runtime
	m.runtime = nil
	m.agentConversation = ""
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
		return ConversationSnapshot{}, fmt.Errorf("clear the previous Ask context: %w", err)
	}
	messages, err := m.asst.loadMessages(conversationID)
	if err != nil {
		return ConversationSnapshot{}, err
	}
	scope, known, err := m.asst.loadScope(conversationID)
	if err != nil {
		return ConversationSnapshot{}, err
	}
	conversations, _ := m.asst.listConversations()
	title := ""
	for _, conversation := range conversations {
		if conversation.ConversationID == conversationID {
			title = conversation.Title
			break
		}
	}
	m.mu.Lock()
	m.conversation = ConversationSnapshot{ConversationID: conversationID, Title: title, Messages: messages}
	m.lastScope, m.scopeKnown = scope, known
	m.mu.Unlock()
	return m.Snapshot(""), nil
}

// resumedPrompt gives a switched-to chat enough conversational context to
// resolve "that" without turning the old transcript into note evidence.
func resumedPrompt(messages []Message, question string) string {
	start := len(messages) - 8
	if start < 0 {
		start = 0
	}
	var b strings.Builder
	b.WriteString("Earlier messages in this Ask chat, for conversational context only. Do not treat them as note evidence and do not cite their tokens; use the note tools for every claim.\n")
	wrote := false
	budget := 6000
	for _, message := range messages[start:] {
		if message.Role != "user" && message.Role != "assistant" {
			continue
		}
		text := strings.TrimSpace(message.Text)
		if text == "" {
			continue
		}
		runes := []rune(text)
		if len(runes) > 700 {
			text = string(runes[:700]) + "…"
		}
		if budget-len([]rune(text)) < 0 {
			break
		}
		budget -= len([]rune(text))
		label := "User"
		if message.Role == "assistant" {
			label = "Assistant"
		}
		b.WriteString(label + ": " + text + "\n")
		wrote = true
	}
	if !wrote {
		return question
	}
	b.WriteString("\nCurrent question: " + question)
	return b.String()
}

// ApplySettings validates, persists non-secret fields, and keeps the API key in
// memory only. Changing the endpoint discards the runtime so the next request
// uses the new transport.
func (m *Manager) ApplySettings(settings Settings) error {
	settings.Normalize()
	if err := settings.Validate(); err != nil {
		return err
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
	store := m.secret
	m.mu.Unlock()
	if settings.RememberKey {
		if store == nil {
			return errors.New("this build has no operating-system keychain available")
		}
		if settings.APIKey == "" {
			if err := store.Delete(); err != nil && !errors.Is(err, ErrSecretNotFound) {
				return fmt.Errorf("remove the saved API key: %w", err)
			}
		} else if err := store.Save(settings.APIKey); err != nil {
			return fmt.Errorf("save the API key in the keychain: %w", err)
		}
	} else if store != nil {
		if err := store.Delete(); err != nil && !errors.Is(err, ErrSecretNotFound) {
			return fmt.Errorf("remove the saved API key: %w", err)
		}
	}
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
		req.ConversationID = m.CurrentConversationID()
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
	if req.ConversationID != m.conversation.ConversationID {
		m.mu.Unlock()
		return RunHandle{}, errors.New("open that Ask chat before asking in it")
	}
	if m.idle != nil {
		m.idle.Stop()
		m.idle = nil
	}
	priorMessages := append([]Message(nil), m.conversation.Messages...)
	// A different evidence boundary starts a separate chat. Keep the previous
	// transcript and citations available in History, without recapping them
	// into a request that no longer authorizes that scope.
	scopeChanged := (m.scopeKnown && !sameScope(m.lastScope, req.Scope)) ||
		(!m.scopeKnown && len(m.conversation.Messages) > 0)
	if scopeChanged {
		priorMessages = nil
		if len(m.conversation.Messages) > 0 {
			id := uuid()
			if err := m.asst.ensureConversation(id); err != nil {
				m.mu.Unlock()
				return RunHandle{}, fmt.Errorf("create a chat for the new scope: %w", err)
			}
			req.ConversationID = id
			m.conversation = ConversationSnapshot{ConversationID: id}
		}
	}
	// A restored Pi root has no proven owner in this manager. Reset it even
	// when the visible chat is empty; only same-scope saved messages may recap.
	resetAgent := scopeChanged || m.agentConversation != req.ConversationID
	needsRecap := !scopeChanged && len(priorMessages) > 0 && resetAgent
	prompt := text
	if needsRecap {
		prompt = resumedPrompt(priorMessages, text)
	}
	ctx, cancel := context.WithTimeout(context.Background(), settings.deadline())
	run := &Run{
		ID:             uuid(),
		ConversationID: req.ConversationID,
		Text:           text,
		prompt:         prompt,
		Scope:          req.Scope,
		limits:         DefaultLimits(),
		ctx:            ctx,
		cancel:         cancel,
		evidence:       newEvidenceRegistry(),
		budget:         newRunBudget(DefaultLimits()),
		done:           make(chan struct{}),
		setupDone:      make(chan struct{}),
		resetAgent:     resetAgent,
		tools:          make(map[string]bool),
		search:         make(map[string][]store.AssistantSearchHit),
	}
	m.lastScope = req.Scope
	m.scopeKnown = true
	m.active = run
	m.conversation.Running = true
	m.conversation.RunID = run.ID
	m.conversation.Messages = append(m.conversation.Messages, Message{Role: "user", Text: text})
	m.mu.Unlock()

	setupFailure := func() string {
		if scopeChanged {
			m.publish(nil, Event{Type: "conversation.reset"})
		}
		if err := m.asst.appendMessage(run.ConversationID, "user", text); err != nil {
			return "The question could not be saved."
		}
		if err := m.asst.saveScope(run.ConversationID, run.Scope); err != nil {
			return "The Ask scope could not be saved."
		}
		if err := m.asst.saveRun(run.ID, run.ConversationID, run.ID, run.Scope, "running", ""); err != nil {
			return "The Ask run could not be saved."
		}
		return ""
	}()
	close(run.setupDone)
	if setupFailure != "" {
		m.failRun(run, "storage_failed", setupFailure)
		return RunHandle{ID: run.ID, ConversationID: run.ConversationID}, nil
	}
	m.mu.Lock()
	if m.active == run && !run.terminal {
		m.publishLocked(run, Event{Type: EventRunStarted, Activity: &Activity{Kind: "starting", Label: "Starting", Active: 1}})
	}
	m.mu.Unlock()
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
	if !m.runAlive(run) {
		return
	}
	if run.resetAgent {
		if err := runtime.resetConversation(run.ctx); err != nil {
			m.failRun(run, "runtime_failed", "The previous Ask context could not be reset: "+err.Error())
			return
		}
	}
	m.setAgentConversation(run)
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
	if m.closed || m.active != run || run.terminal {
		m.mu.Unlock()
		return nil, context.Canceled
	}
	if m.runtime != nil && !m.runtime.isClosed() {
		rt := m.runtime
		rt.bindRun(run)
		m.mu.Unlock()
		return rt, nil
	}
	m.mu.Unlock()

	rt, err := newAgentRuntime(m, settings, run.ConversationID)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	if m.closed || m.active != run || run.terminal {
		m.mu.Unlock()
		rt.close(context.Background())
		return nil, context.Canceled
	}
	rt.bindRun(run)
	m.runtime = rt
	m.mu.Unlock()
	if err := rt.open(run.ctx, settings, run.ConversationID); err != nil {
		m.mu.Lock()
		if m.runtime == rt {
			m.runtime = nil
		}
		m.mu.Unlock()
		rt.disposeStorage()
		rt.close(context.Background())
		return nil, err
	}
	m.mu.Lock()
	if m.closed || m.active != run || run.terminal {
		m.mu.Unlock()
		rt.close(context.Background())
		return nil, ErrClosed
	}
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
	return m.snapshotLocked()
}

func (m *Manager) snapshotLocked() ConversationSnapshot {
	snapshot := m.conversation
	snapshot.Messages = append([]Message(nil), m.conversation.Messages...)
	if m.scopeKnown {
		scope := m.lastScope
		snapshot.Scope = &scope
	}
	snapshot.Running = m.active != nil
	if m.active != nil {
		snapshot.RunID = m.active.ID
	}
	return snapshot
}

func (m *Manager) setAgentConversation(run *Run) {
	m.mu.Lock()
	if m.active == run && !run.terminal {
		m.agentConversation = run.ConversationID
	}
	m.mu.Unlock()
}

func (m *Manager) dropRuntime() {
	m.mu.Lock()
	rt := m.runtime
	m.runtime = nil
	m.agentConversation = ""
	if m.idle != nil {
		m.idle.Stop()
		m.idle = nil
	}
	m.mu.Unlock()
	if rt != nil {
		go rt.close(context.Background())
	}
}

// ResetConversation explicitly discards the current chat's transcript and
// durable agent context. Scope changes instead preserve history in a new chat.
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
	conversationID := m.CurrentConversationID()
	if err := m.asst.clearMessages(conversationID); err != nil {
		return fmt.Errorf("clear Ask history: %w", err)
	}
	m.mu.Lock()
	m.agentConversation = ""
	m.conversation.Messages = nil
	m.conversation.Running = false
	m.conversation.RunID = ""
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
	var timer *time.Timer
	timer = time.AfterFunc(idleRuntimeLifetime, func() { m.expireIdleRuntime(timer) })
	m.idle = timer
	m.mu.Unlock()
}

// A stopped timer may already be waiting for the manager lock. It can only
// dispose the runtime while it still owns the current idle period.
func (m *Manager) expireIdleRuntime(timer *time.Timer) {
	m.mu.Lock()
	if m.idle != timer || m.active != nil || m.closed {
		m.mu.Unlock()
		return
	}
	rt := m.runtime
	m.runtime = nil
	m.idle = nil
	m.agentConversation = ""
	m.mu.Unlock()
	if rt != nil {
		slog.Info("closing idle Ask runtime")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		rt.close(ctx)
	}
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
	m.events.close()
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
	if run != nil {
		m.forceCancelled(run, "The application is closing.")
		<-run.done
	}
	return m.asst.close()
}

func (m *Manager) loopError(rt *agentRuntime, err error) {
	run := rt.currentRun()
	if run != nil {
		m.failRun(run, "runtime_failed", "The Ask runtime stopped unexpectedly: "+trimLog(err.Error()))
	}
}

func (m *Manager) publish(run *Run, event Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if run != nil && (m.active != run || run.terminal) {
		return
	}
	m.publishLocked(run, event)
}

func (m *Manager) publishLocked(run *Run, event Event) {
	m.seq++
	event.Sequence = m.seq
	event.APIVersion = APIVersion
	if run != nil {
		event.RunID = run.ID
		event.ConversationID = run.ConversationID
	}
	m.events.enqueue(event)
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
	case "thinking.delta":
		text := stringValue(raw, "text")
		m.mu.Lock()
		if m.active != run || run.terminal {
			m.mu.Unlock()
			return
		}
		m.mu.Unlock()
		// Reasoning is transient: it streams to the UI and desktop.log but is
		// never written into the persisted transcript.
		m.publish(run, Event{Type: EventThinking, Text: text})
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
		m.finish(run, Event{Type: EventRunCancelled, Code: "cancelled", Message: reason, Status: "cancelled"})
	case "run.failed":
		code := normalizeCode(stringValue(raw, "code"))
		message := stringValue(raw, "message")
		if message == "" {
			message = "The Ask run failed."
		}
		if stringValue(raw, "phase") == "open" {
			rt.failReady(errors.New(message))
		}
		m.failRun(run, code, message)
	}
}

func (m *Manager) completeRun(run *Run, text string) {
	m.finish(run, Event{Type: EventRunCompleted, Text: text, Status: "completed"})
}

func (m *Manager) failRun(run *Run, code, message string) {
	m.finish(run, Event{Type: EventRunFailed, Code: normalizeCode(code), Message: message, Status: "failed"})
}

func (m *Manager) forceCancelled(run *Run, reason string) {
	m.finish(run, Event{Type: EventRunCancelled, Code: "cancelled", Message: reason, Status: "cancelled"})
}

// finish claims the outcome once, keeping admission occupied until storage and
// the snapshot are finalized. No consumer can observe a premature terminal.
func (m *Manager) finish(run *Run, event Event) {
	m.mu.Lock()
	if m.active != run || run.terminal {
		m.mu.Unlock()
		return
	}
	// Stop owns the outcome once its Go context is cancelled. Pi may settle
	// the aborted submission as unanswered before its cancellation event.
	if event.Type != EventRunCancelled && errors.Is(run.ctx.Err(), context.Canceled) {
		event = Event{Type: EventRunCancelled, Code: "cancelled", Message: "Stopped by the user.", Status: "cancelled"}
	}
	run.terminal = true
	if event.Type == EventRunCompleted && event.Text != "" {
		run.answer = event.Text
	}
	answer := run.answer
	citations := run.evidence.cite(answer)
	run.citations = citations
	rt := m.runtime
	discard := event.Type != EventRunCompleted
	if discard {
		m.runtime = nil
		m.agentConversation = ""
	}
	m.mu.Unlock()

	// Release runtime-owned transactions before lifecycle writes need the sole
	// connection. Closing JavaScript itself is bounded and need not block Stop.
	if rt != nil && discard {
		rt.disposeStorage()
		go rt.close(context.Background())
	}
	if rt != nil {
		rt.clearRunIf(run)
	}
	// Stop can race Start's initial writes. Never let a late "running" write
	// overwrite the terminal outcome, or release admission ahead of that write.
	if run.setupDone != nil {
		<-run.setupDone
	}
	saveErr := m.asst.saveRun(run.ID, run.ConversationID, run.ID, run.Scope, event.Status, answer)
	if saveErr != nil {
		slog.Error("save Ask run outcome", "error", saveErr)
	}
	message := Message{Role: "assistant", Text: answer, Citations: citations}
	if event.Type == EventRunFailed {
		message = Message{Role: "error", Text: event.Message}
	}
	appendErr := m.asst.appendMessageWithCitations(run.ConversationID, message.Role, message.Text, message.Citations)
	if appendErr != nil {
		slog.Error("save Ask answer", "error", appendErr)
	}
	if saveErr != nil || appendErr != nil {
		event = Event{Type: EventRunFailed, Status: "failed", Code: "storage_failed", Message: "The Ask response could not be fully saved. Copy any answer you need before closing the app."}
	}
	m.mu.Lock()
	m.conversation.Messages = append(m.conversation.Messages, message)
	m.conversation.Running = false
	m.conversation.RunID = ""
	m.active = nil
	if event.Type == EventRunCompleted {
		event.Text, event.Citations = answer, citations
		for _, citation := range citations {
			c := citation
			m.publishLocked(run, Event{Type: EventCitation, Citation: &c})
		}
	}
	m.publishLocked(run, event)
	run.cancel()
	close(run.done)
	m.mu.Unlock()
	m.scheduleIdleClose()
}

func sameScope(a, b Scope) bool {
	return a.Kind == b.Kind && a.NoteID == b.NoteID && a.NotebookID == b.NotebookID && a.IncludeDescendants == b.IncludeDescendants
}
