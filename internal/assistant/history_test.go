package assistant

import (
	"context"
	"strings"
	"testing"

	"github.com/lsj5031/joplin-lite/internal/store"
)

type fakeSecretStore struct {
	value string
	saved int
}

func (f *fakeSecretStore) Load() (string, error) {
	if f.value == "" {
		return "", ErrSecretNotFound
	}
	return f.value, nil
}
func (f *fakeSecretStore) Save(value string) error { f.value, f.saved = value, f.saved+1; return nil }
func (f *fakeSecretStore) Delete() error           { f.value = ""; return nil }

func TestConversationHistoryLifecycle(t *testing.T) {
	root := t.TempDir()
	db, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := NewManager(db, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(context.Background())

	first := m.CurrentConversationID()
	if first == "" {
		t.Fatal("fresh manager has no conversation")
	}
	second, err := m.NewConversation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.ConversationID == first {
		t.Fatal("NewConversation reused the current id")
	}
	if err := m.RenameConversation(second.ConversationID, "Camera chat"); err != nil {
		t.Fatal(err)
	}
	list := m.ListConversations()
	if len(list) < 2 {
		t.Fatalf("history = %+v", list)
	}
	if list[0].ConversationID != second.ConversationID || list[0].Title != "Camera chat" {
		t.Fatalf("renamed conversation not first: %+v", list)
	}
	selected, err := m.SelectConversation(context.Background(), first)
	if err != nil || selected.ConversationID != first {
		t.Fatalf("select = %+v, %v", selected, err)
	}
	deleted, err := m.DeleteConversation(context.Background(), second.ConversationID)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.ConversationID == second.ConversationID {
		t.Fatal("deleting a conversation left it active")
	}
	if _, err := m.SelectConversation(context.Background(), second.ConversationID); err == nil {
		t.Fatal("deleted conversation still selectable")
	}
}

func TestSecretStoreOnlyWhenRemembered(t *testing.T) {
	root := t.TempDir()
	db, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := NewManager(db, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(context.Background())
	secrets := &fakeSecretStore{}
	m.SetSecretStore(secrets)
	settings := DefaultSettings()
	settings.BaseURL = "http://127.0.0.1:8080/v1"
	settings.ModelID = "local"
	settings.APIKey = "secret-key"
	settings.RememberKey = true
	if err := m.ApplySettings(settings); err != nil {
		t.Fatal(err)
	}
	if secrets.saved != 1 || secrets.value != "secret-key" {
		t.Fatalf("secret store = %+v", secrets)
	}

	m2, err := NewManager(db, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m2.Close(context.Background())
	m2.SetSecretStore(secrets)
	if err := m2.RestoreStoredKey(); err != nil {
		t.Fatal(err)
	}
	if got := m2.Settings().APIKey; got != "secret-key" {
		t.Fatalf("restored key = %q", got)
	}
	settings = m2.Settings()
	settings.RememberKey = false
	if err := m2.ApplySettings(settings); err != nil {
		t.Fatal(err)
	}
	if secrets.value != "" {
		t.Fatalf("disabling remember left a credential: %+v", secrets)
	}
}

func TestResumedPromptKeepsOnlyRecentTranscript(t *testing.T) {
	var messages []Message
	for i := 0; i < 20; i++ {
		messages = append(messages, Message{Role: "user", Text: strings.Repeat("x", 800)})
	}
	prompt := resumedPrompt(messages, "current question")
	if !strings.Contains(prompt, "Current question: current question") {
		t.Fatalf("missing current question: %.120q", prompt)
	}
	if len([]rune(prompt)) > 6500 {
		t.Fatalf("recap grew too large: %d runes", len([]rune(prompt)))
	}
	if strings.Count(prompt, "User:") > 8 {
		t.Fatalf("recap included too many turns: %d", strings.Count(prompt, "User:"))
	}
}
