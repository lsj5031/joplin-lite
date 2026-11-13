package assistant

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func taggedInt(n string) json.RawMessage {
	return json.RawMessage(`{"t":"i","v":"` + n + `"}`)
}

func taggedText(s string) json.RawMessage {
	raw, _ := json.Marshal(map[string]string{"t": "s", "v": s})
	return raw
}

func TestAssistantDatabaseTransactionsAndValues(t *testing.T) {
	a, err := openAssistantDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.close()
	ctx := context.Background()
	if _, err := a.execute(ctx, dbRequest{Op: "exec", SQL: `CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT, b BLOB, n TEXT)`}); err != nil {
		t.Fatal(err)
	}
	begun, err := a.execute(ctx, dbRequest{Op: "begin"})
	if err != nil {
		t.Fatal(err)
	}
	tx := begun["tx"].(string)
	if _, err := a.execute(ctx, dbRequest{Op: "run", SQL: `INSERT INTO t (id, v, b, n) VALUES (?, ?, ?, ?)`, Tx: tx, Params: []json.RawMessage{
		taggedInt("1"), taggedText("value"), json.RawMessage(`{"t":"b","v":"AAEC"}`), json.RawMessage(`{"t":"n"}`),
	}}); err != nil {
		t.Fatal(err)
	}
	result, err := a.execute(ctx, dbRequest{Op: "get", SQL: `SELECT id, v, b, n FROM t`, Tx: tx})
	if err != nil {
		t.Fatal(err)
	}
	if result["row"] == nil {
		t.Fatal("row missing inside transaction")
	}
	if _, err := a.execute(ctx, dbRequest{Op: "rollback", Tx: tx}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM t`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rollback left %d rows", count)
	}

	begun, err = a.execute(ctx, dbRequest{Op: "begin"})
	if err != nil {
		t.Fatal(err)
	}
	tx = begun["tx"].(string)
	if _, err := a.execute(ctx, dbRequest{Op: "run", SQL: `INSERT INTO t (id, v) VALUES (?, ?)`, Tx: tx, Params: []json.RawMessage{taggedInt("2"), taggedText("committed")}}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.execute(ctx, dbRequest{Op: "commit", Tx: tx}); err != nil {
		t.Fatal(err)
	}
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM t`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("commit left %d rows", count)
	}
	if _, err := a.execute(ctx, dbRequest{Op: "run", SQL: `INSERT INTO t (id, v) VALUES (?, ?)`, Params: []json.RawMessage{
		json.RawMessage(`{"t":"i","v":"999999999999999999999999"}`), taggedText("too big"),
	}}); err == nil {
		t.Fatal("an unrepresentable integer must be rejected")
	}
	if a.status() != filepath.Join(a.path) {
		t.Fatalf("unexpected database status %q", a.status())
	}
}

func TestConversationHistorySurvivesManagerReopen(t *testing.T) {
	db := openTestVault(t)
	dir := t.TempDir()
	m1, err := NewManager(db, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	m1.asst.appendMessage(defaultConversationID, "user", "hello")
	m1.asst.appendMessage(defaultConversationID, "assistant", "world")
	if err := m1.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	m2, err := NewManager(db, filepath.Dir(filepath.Join(dir, "assistant.sqlite")), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m2.Close(context.Background())
	snapshot := m2.Snapshot(defaultConversationID)
	if len(snapshot.Messages) != 2 || snapshot.Messages[0].Text != "hello" || snapshot.Messages[1].Text != "world" {
		t.Fatalf("history did not survive reopen: %+v", snapshot.Messages)
	}
}
