// Package assistant hosts the embedded Ask agent: it owns the moejs runtime,
// validates every note and database request, and projects agent events to the
// native UI. The agent itself is bundled with esbuild and embedded in the app;
// Node is a build tool only.
package assistant

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const APIVersion = 1

var (
	ErrBusy           = errors.New("an Ask run is already active")
	ErrUnconfigured   = errors.New("Ask has no model connection configured")
	ErrClosed         = errors.New("Ask is closed")
	ErrSecretNotFound = errors.New("saved Ask credential not found")
)

// ToolError is a structured, model-visible failure. It deliberately contains
// no note body or credential.
type ToolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *ToolError) Error() string { return e.Message }

func toolErr(code, format string, args ...any) error {
	return &ToolError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Scope is the immutable note boundary chosen by the user for one run.
type Scope struct {
	Kind               string `json:"kind"`
	NoteID             string `json:"noteId,omitempty"`
	NotebookID         string `json:"notebookId,omitempty"`
	IncludeDescendants bool   `json:"includeDescendants,omitempty"`
}

func (s Scope) Validate() error {
	switch s.Kind {
	case "vault":
		return nil
	case "note":
		if len(s.NoteID) != 32 {
			return errors.New("invalid note scope")
		}
		return nil
	case "notebook":
		if len(s.NotebookID) != 32 {
			return errors.New("invalid notebook scope")
		}
		return nil
	case "":
		return errors.New("a note scope is required")
	default:
		return errors.New("unknown scope")
	}
}

// Limits are whole-run engineering defaults. They bound tool work and emitted
// evidence in Go even if the engine resets its own memory accounting.
type Limits struct {
	ModelTurns       int `json:"modelTurns"`
	NoteToolCalls    int `json:"noteToolCalls"`
	EvidenceBytes    int `json:"evidenceBytes"`
	ReadBytes        int `json:"readBytes"`
	SearchLimit      int `json:"searchLimit"`
	SearchCandidates int `json:"searchCandidates"`
}

func DefaultLimits() Limits {
	return Limits{
		ModelTurns:       12,
		NoteToolCalls:    40,
		EvidenceBytes:    120 << 10,
		ReadBytes:        12 << 10,
		SearchLimit:      20,
		SearchCandidates: 50,
	}
}

// Settings are the non-secret connection fields plus the session key. The key
// itself is never written to assistant.sqlite; RememberKey only records whether
// the shell may keep it in the operating system's credential store.
type Settings struct {
	Provider      string `json:"provider"`
	BaseURL       string `json:"baseUrl"`
	ModelID       string `json:"modelId"`
	ContextWindow int    `json:"contextWindow"`
	MaxOutput     int    `json:"maxOutput"`
	TimeoutMS     int    `json:"timeoutMs"`
	RememberKey   bool   `json:"rememberKey,omitempty"`
	APIKey        string `json:"-"`
}

func DefaultSettings() Settings {
	return Settings{
		Provider:      "openai-completions",
		ContextWindow: 128000,
		MaxOutput:     4096,
		TimeoutMS:     120000,
	}
}

func (s *Settings) Normalize() {
	if s.Provider == "" {
		s.Provider = "openai-completions"
	}
	if s.ContextWindow <= 0 {
		s.ContextWindow = 128000
	}
	if s.MaxOutput <= 0 {
		s.MaxOutput = 4096
	}
	if s.TimeoutMS <= 0 {
		s.TimeoutMS = 120000
	}
	s.BaseURL = strings.TrimRight(strings.TrimSpace(s.BaseURL), "/")
	s.ModelID = strings.TrimSpace(s.ModelID)
	s.APIKey = strings.TrimSpace(s.APIKey)
}

func (s Settings) Validate() error {
	if s.Provider != "" && s.Provider != "openai-completions" {
		return errors.New("only the OpenAI-compatible Chat Completions format is supported in v1")
	}
	if s.BaseURL == "" {
		return errors.New("the model base URL is required")
	}
	u, err := url.Parse(s.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return errors.New("the model base URL must be an http or https URL without credentials")
	}
	if strings.TrimSpace(s.ModelID) == "" {
		return errors.New("the model ID is required")
	}
	if s.ContextWindow < 1024 || s.ContextWindow > 4_000_000 {
		return errors.New("the context window must be between 1,024 and 4,000,000 tokens")
	}
	if s.MaxOutput < 1 || s.MaxOutput > 200_000 {
		return errors.New("the maximum output must be between 1 and 200,000 tokens")
	}
	return nil
}

// Configured allows keyless local inference endpoints. A key is optional; when
// present it is still held only in memory and injected by the Go transport.
func (s Settings) Configured() bool {
	return s.BaseURL != "" && s.ModelID != ""
}

// AskRequest is one accepted Ask submission. The Go host owns its identity and
// scope; the model cannot widen either.
type AskRequest struct {
	ConversationID string `json:"conversationId"`
	Text           string `json:"text"`
	Scope          Scope  `json:"scope"`
}

// RunHandle is returned once a run is admitted.
type RunHandle struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversationId"`
}

// Event is one versioned, sequenced update. Terminal events occur once.
type Event struct {
	APIVersion     int        `json:"apiVersion"`
	Sequence       uint64     `json:"seq"`
	RunID          string     `json:"runId,omitempty"`
	ConversationID string     `json:"conversationId,omitempty"`
	Type           string     `json:"type"`
	Text           string     `json:"text,omitempty"`
	Activity       *Activity  `json:"activity,omitempty"`
	Citation       *Citation  `json:"citation,omitempty"`
	Citations      []Citation `json:"citations,omitempty"`
	Code           string     `json:"code,omitempty"`
	Message        string     `json:"message,omitempty"`
	Status         string     `json:"status,omitempty"`
}

type Activity struct {
	Kind   string `json:"kind,omitempty"`
	Tool   string `json:"tool,omitempty"`
	Label  string `json:"label,omitempty"`
	Status string `json:"status,omitempty"`
	Active int    `json:"active"`
}

// Citation is a verified passage read through read_note during this run.
type Citation struct {
	Token     string `json:"token"`
	NoteID    string `json:"noteId"`
	Title     string `json:"title"`
	Excerpt   string `json:"excerpt"`
	StartByte int    `json:"startByte,omitempty"`
	EndByte   int    `json:"endByte,omitempty"`
	Version   string `json:"version"`
	Verified  bool   `json:"verified"`
}

// ConversationSnapshot is the read-only projection the panel renders.
type ConversationSnapshot struct {
	ConversationID string    `json:"conversationId"`
	Title          string    `json:"title,omitempty"`
	Messages       []Message `json:"messages"`
	Running        bool      `json:"running"`
	RunID          string    `json:"runId,omitempty"`
}

// ConversationSummary is the metadata the history menu needs without loading
// every transcript.
type ConversationSummary struct {
	ConversationID string `json:"conversationId"`
	Title          string `json:"title,omitempty"`
	Preview        string `json:"preview,omitempty"`
	MessageCount   int    `json:"messageCount"`
	UpdatedTime    int64  `json:"updatedTime"`
}

type Message struct {
	Role      string     `json:"role"`
	Text      string     `json:"text"`
	Citations []Citation `json:"citations,omitempty"`
}

// SecretStore is the shell-owned operating-system keychain. Implementations
// must never write the key into assistant.sqlite or a log.
type SecretStore interface {
	Load() (string, error)
	Save(string) error
	Delete() error
}

// SearchInput is validated at the host boundary before it reaches SQL.
type SearchInput struct {
	Query  string `json:"query"`
	Limit  int    `json:"limit"`
	TagID  string `json:"tagId"`
	Cursor string `json:"cursor"`
}

type SearchPage struct {
	Query      string         `json:"query"`
	Results    []SearchResult `json:"results"`
	NextCursor string         `json:"nextCursor,omitempty"`
	Evidence   string         `json:"evidence"`
}

type SearchResult struct {
	NoteID      string  `json:"noteId"`
	Title       string  `json:"title"`
	Notebook    string  `json:"notebook"`
	UpdatedTime int64   `json:"updatedTime"`
	Snippet     string  `json:"snippet"`
	Score       float64 `json:"score"`
}

type ReadInput struct {
	NoteID          string `json:"noteId"`
	ExpectedVersion string `json:"expectedVersion"`
	StartByte       int    `json:"startByte"`
	MaxBytes        int    `json:"maxBytes"`
}

type NotePassage struct {
	NoteID      string `json:"noteId"`
	Title       string `json:"title"`
	Notebook    string `json:"notebook"`
	Version     string `json:"version"`
	UpdatedTime int64  `json:"updatedTime"`
	StartByte   int    `json:"startByte"`
	EndByte     int    `json:"endByte"`
	NextByte    int    `json:"nextByte,omitempty"`
	LineStart   int    `json:"lineStart"`
	LineEnd     int    `json:"lineEnd"`
	Text        string `json:"text"`
	EvidenceID  string `json:"evidenceId"`
	Truncated   bool   `json:"truncated"`
}

// EventType constants keep the JS projection and the Go reducer in step.
const (
	EventRunStarted     = "run.started"
	EventActivity       = "activity.updated"
	EventThinking       = "thinking.delta"
	EventAnswerDelta    = "answer.delta"
	EventAnswerSnapshot = "answer.snapshot"
	EventEvidenceRead   = "evidence.read"
	EventCitation       = "citation.resolved"
	EventRunCompleted   = "run.completed"
	EventRunCancelled   = "run.cancelled"
	EventRunFailed      = "run.failed"
)

// deadline is the whole-run wall clock, separate from the streaming timeout of
// one provider request.
func (s Settings) deadline() time.Duration {
	if s.TimeoutMS <= 0 {
		return 120 * time.Second
	}
	return time.Duration(s.TimeoutMS) * time.Millisecond
}
