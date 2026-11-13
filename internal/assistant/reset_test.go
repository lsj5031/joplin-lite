package assistant

import (
	"context"
	"testing"
)

func TestResetConversationDropsAgentStateAndMessages(t *testing.T) {
	db := openTestVault(t)
	m, err := NewManager(db, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(context.Background())
	m.asst.appendMessage(defaultConversationID, "user", "old question")
	if _, err := m.asst.db.Exec(`CREATE TABLE pi_fixture (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if err := m.ResetConversation(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := m.Snapshot(defaultConversationID); len(got.Messages) != 0 {
		t.Fatalf("messages survived reset: %+v", got.Messages)
	}
	var count int
	if err := m.asst.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'pi_fixture'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("Pi durable tables survived reset")
	}
}
