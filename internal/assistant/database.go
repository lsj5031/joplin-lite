package assistant

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// assistantDB owns assistant.sqlite. The Pi durable tables live in their own
// `pi` prefix and are migrated by Pi itself; Joplin Lite lifecycle data uses the
// separate jl_ask_ namespace.
type assistantDB struct {
	db   *sql.DB
	path string

	txMu     sync.Mutex
	txs      map[string]*sql.Tx
	txOwners map[string]context.Context
}

func openAssistantDB(dataDir string) (*assistantDB, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create assistant data directory: %w", err)
	}
	path := filepath.Join(dataDir, "assistant.sqlite")
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open assistant database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect assistant database: %w", err)
	}
	_ = os.Chmod(path, 0o600)
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS jl_ask_settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	); CREATE TABLE IF NOT EXISTS jl_ask_conversations (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL DEFAULT '',
		created_time INTEGER NOT NULL,
		updated_time INTEGER NOT NULL
	); CREATE TABLE IF NOT EXISTS jl_ask_runs (
		id TEXT PRIMARY KEY,
		conversation_id TEXT NOT NULL,
		request_id TEXT NOT NULL,
		scope TEXT NOT NULL,
		status TEXT NOT NULL,
		answer TEXT NOT NULL DEFAULT '',
		created_time INTEGER NOT NULL,
		finished_time INTEGER NOT NULL DEFAULT 0
	); CREATE TABLE IF NOT EXISTS jl_ask_messages (
		conversation_id TEXT NOT NULL,
		seq INTEGER NOT NULL,
		role TEXT NOT NULL,
		text TEXT NOT NULL,
		citations_json TEXT NOT NULL DEFAULT '[]',
		PRIMARY KEY (conversation_id, seq)
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create assistant lifecycle schema: %w", err)
	}
	if err := ensureColumn(db, "jl_ask_messages", "citations_json", "TEXT NOT NULL DEFAULT '[]'"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate Ask messages: %w", err)
	}
	now := time.Now().UnixMilli()
	// Keep the pre-history installation's one thread visible after upgrading.
	if _, err := db.Exec(`INSERT OR IGNORE INTO jl_ask_conversations (id, title, created_time, updated_time)
		SELECT conversation_id, '', ?, ? FROM jl_ask_messages GROUP BY conversation_id`, now, now); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate Ask conversations: %w", err)
	}
	if _, err := db.Exec(`INSERT INTO jl_ask_conversations (id, title, created_time, updated_time)
		SELECT ?, '', ?, ? WHERE NOT EXISTS (SELECT 1 FROM jl_ask_conversations)`, defaultConversationID, now, now); err != nil {
		db.Close()
		return nil, fmt.Errorf("create the default Ask conversation: %w", err)
	}
	// A run that was admitted before the process ended has no live worker now.
	// Never replay it; the user can ask again explicitly.
	if _, err := db.Exec(`UPDATE jl_ask_runs SET status = 'interrupted',
		finished_time = CASE WHEN finished_time = 0 THEN created_time ELSE finished_time END
		WHERE status = 'running'`); err != nil {
		db.Close()
		return nil, fmt.Errorf("reconcile interrupted Ask runs: %w", err)
	}
	return &assistantDB{db: db, path: path, txs: make(map[string]*sql.Tx), txOwners: make(map[string]context.Context)}, nil
}

// ensureColumn applies additive schema changes to databases created by an
// earlier build without requiring a migration framework.
func ensureColumn(db *sql.DB, table, column, definition string) error {
	rows, err := db.Query(`PRAGMA table_info("` + table + `")`)
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, kind string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return err
		}
		if name == column {
			found = true
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = db.Exec(`ALTER TABLE "` + table + `" ADD COLUMN "` + column + `" ` + definition)
	return err
}

func (a *assistantDB) loadSettings() (Settings, error) {
	s := DefaultSettings()
	var raw string
	err := a.db.QueryRow(`SELECT value FROM jl_ask_settings WHERE key = 'model'`).Scan(&raw)
	if err == sql.ErrNoRows {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return DefaultSettings(), nil
	}
	s.Normalize()
	return s, nil
}

func (a *assistantDB) saveSettings(s Settings) error {
	raw, err := json.Marshal(map[string]any{
		"provider":      s.Provider,
		"baseUrl":       s.BaseURL,
		"modelId":       s.ModelID,
		"contextWindow": s.ContextWindow,
		"maxOutput":     s.MaxOutput,
		"timeoutMs":     s.TimeoutMS,
		"rememberKey":   s.RememberKey,
	})
	if err != nil {
		return err
	}
	_, err = a.db.Exec(`INSERT OR REPLACE INTO jl_ask_settings (key, value) VALUES ('model', ?)`, string(raw))
	return err
}

func (a *assistantDB) saveRun(runID, conversationID, requestID string, scope Scope, status, answer string) error {
	scopeJSON, _ := json.Marshal(scope)
	now := time.Now().UnixMilli()
	finished := now
	if status == "running" {
		finished = 0
	}
	_, err := a.db.Exec(`INSERT OR REPLACE INTO jl_ask_runs
		(id, conversation_id, request_id, scope, status, answer, created_time, finished_time)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, runID, conversationID, requestID, string(scopeJSON), status, answer, now, finished)
	return err
}

func (a *assistantDB) saveScope(conversationID string, scope Scope) error {
	raw, err := json.Marshal(scope)
	if err != nil {
		return err
	}
	_, err = a.db.Exec(`INSERT OR REPLACE INTO jl_ask_settings (key, value) VALUES (?, ?)`, "scope:"+conversationID, string(raw))
	return err
}

func (a *assistantDB) loadScope(conversationID string) (Scope, bool, error) {
	var raw string
	// The legacy global scope cannot establish any particular chat's boundary.
	err := a.db.QueryRow(`SELECT value FROM jl_ask_settings WHERE key = ?`, "scope:"+conversationID).Scan(&raw)
	if err == sql.ErrNoRows {
		return Scope{}, false, nil
	}
	if err != nil {
		return Scope{}, false, err
	}
	var scope Scope
	if err := json.Unmarshal([]byte(raw), &scope); err != nil || scope.Validate() != nil {
		return Scope{}, false, nil
	}
	return scope, true, nil
}

func (a *assistantDB) loadMessages(conversationID string) ([]Message, error) {
	rows, err := a.db.Query(`SELECT role, text, citations_json FROM jl_ask_messages WHERE conversation_id = ? ORDER BY seq ASC`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var message Message
		var citations string
		if err := rows.Scan(&message.Role, &message.Text, &citations); err != nil {
			return nil, err
		}
		if citations != "" {
			_ = json.Unmarshal([]byte(citations), &message.Citations)
		}
		out = append(out, message)
	}
	return out, rows.Err()
}

func (a *assistantDB) clearMessages(conversationID string) error {
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM jl_ask_messages WHERE conversation_id = ?`, conversationID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE jl_ask_conversations SET updated_time = ? WHERE id = ?`, time.Now().UnixMilli(), conversationID); err != nil {
		return err
	}
	return tx.Commit()
}

func (a *assistantDB) ensureConversation(conversationID string) error {
	now := time.Now().UnixMilli()
	_, err := a.db.Exec(`INSERT OR IGNORE INTO jl_ask_conversations (id, title, created_time, updated_time) VALUES (?, '', ?, ?)`, conversationID, now, now)
	return err
}

func (a *assistantDB) conversationExists(conversationID string) (bool, error) {
	var one int
	err := a.db.QueryRow(`SELECT 1 FROM jl_ask_conversations WHERE id = ?`, conversationID).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

func (a *assistantDB) latestConversationID() (string, error) {
	var id string
	err := a.db.QueryRow(`SELECT id FROM jl_ask_conversations ORDER BY updated_time DESC, rowid DESC LIMIT 1`).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id, err
}

func (a *assistantDB) listConversations() ([]ConversationSummary, error) {
	rows, err := a.db.Query(`SELECT c.id,
		COALESCE(NULLIF(c.title, ''), (SELECT text FROM jl_ask_messages m WHERE m.conversation_id = c.id AND m.role = 'user' ORDER BY m.seq ASC LIMIT 1), ''),
		c.created_time, c.updated_time,
		COALESCE((SELECT text FROM jl_ask_messages m WHERE m.conversation_id = c.id ORDER BY m.seq DESC LIMIT 1), ''),
		(SELECT COUNT(*) FROM jl_ask_messages m WHERE m.conversation_id = c.id)
		FROM jl_ask_conversations c ORDER BY c.updated_time DESC, c.rowid DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ConversationSummary
	for rows.Next() {
		var summary ConversationSummary
		var created int64
		if err := rows.Scan(&summary.ConversationID, &summary.Title, &created, &summary.UpdatedTime, &summary.Preview, &summary.MessageCount); err != nil {
			return nil, err
		}
		out = append(out, summary)
	}
	return out, rows.Err()
}

func (a *assistantDB) renameConversation(conversationID, title string) error {
	_, err := a.db.Exec(`UPDATE jl_ask_conversations SET title = ?, updated_time = ? WHERE id = ?`, title, time.Now().UnixMilli(), conversationID)
	return err
}

func (a *assistantDB) deleteConversation(conversationID string) error {
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, statement := range []string{
		`DELETE FROM jl_ask_messages WHERE conversation_id = ?`,
		`DELETE FROM jl_ask_runs WHERE conversation_id = ?`,
		`DELETE FROM jl_ask_conversations WHERE id = ?`,
	} {
		if _, err := tx.Exec(statement, conversationID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// clearAgentState drops only Pi's own tables. Settings and Joplin Lite
// lifecycle records survive a conversation reset.
func (a *assistantDB) clearAgentState() error {
	rows, err := a.db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name GLOB 'pi*'`)
	if err != nil {
		return err
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		names = append(names, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(names) == 0 {
		return nil
	}
	if _, err := a.db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	defer a.db.Exec(`PRAGMA foreign_keys = ON`)
	for _, name := range names {
		if _, err := a.db.Exec(`DROP TABLE IF EXISTS "` + strings.ReplaceAll(name, `"`, `""`) + `"`); err != nil {
			return err
		}
	}
	return nil
}

func (a *assistantDB) appendMessage(conversationID, role, text string) error {
	return a.appendMessageWithCitations(conversationID, role, text, nil)
}

func (a *assistantDB) appendMessageWithCitations(conversationID, role, text string, citations []Citation) error {
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UnixMilli()
	if _, err := tx.Exec(`INSERT OR IGNORE INTO jl_ask_conversations (id, title, created_time, updated_time) VALUES (?, '', ?, ?)`, conversationID, now, now); err != nil {
		return err
	}
	var seq int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(seq), 0) + 1 FROM jl_ask_messages WHERE conversation_id = ?`, conversationID).Scan(&seq); err != nil {
		return err
	}
	rawCitations, err := json.Marshal(citations)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO jl_ask_messages (conversation_id, seq, role, text, citations_json) VALUES (?, ?, ?, ?, ?)`, conversationID, seq, role, text, string(rawCitations)); err != nil {
		return err
	}
	if role == "user" {
		if _, err := tx.Exec(`UPDATE jl_ask_conversations SET title = CASE WHEN title = '' THEN ? ELSE title END, updated_time = ? WHERE id = ?`, conversationTitle(text), now, conversationID); err != nil {
			return err
		}
	} else if _, err := tx.Exec(`UPDATE jl_ask_conversations SET updated_time = ? WHERE id = ?`, now, conversationID); err != nil {
		return err
	}
	return tx.Commit()
}

func conversationTitle(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) > 56 {
		return string(runes[:55]) + "…"
	}
	return text
}

func (a *assistantDB) close() error {
	a.txMu.Lock()
	for id, tx := range a.txs {
		_ = tx.Rollback()
		delete(a.txs, id)
	}
	a.txMu.Unlock()
	return a.db.Close()
}

type dbRequest struct {
	Op     string            `json:"op"`
	SQL    string            `json:"sql"`
	Params []json.RawMessage `json:"params"`
	Tx     string            `json:"tx"`
}

type taggedValue struct {
	T string          `json:"t"`
	V json.RawMessage `json:"v"`
}

func decodeDBParams(params []json.RawMessage) ([]any, error) {
	out := make([]any, 0, len(params))
	for _, raw := range params {
		var value taggedValue
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		switch value.T {
		case "n", "":
			out = append(out, nil)
		case "s":
			var s string
			if err := json.Unmarshal(value.V, &s); err != nil {
				return nil, err
			}
			out = append(out, s)
		case "f":
			var f float64
			if err := json.Unmarshal(value.V, &f); err != nil {
				return nil, err
			}
			out = append(out, f)
		case "i":
			var text string
			if err := json.Unmarshal(value.V, &text); err != nil {
				return nil, err
			}
			n, err := strconv.ParseInt(text, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("integer parameter is out of range")
			}
			out = append(out, n)
		case "b":
			var text string
			if err := json.Unmarshal(value.V, &text); err != nil {
				return nil, err
			}
			b, err := base64.StdEncoding.DecodeString(text)
			if err != nil {
				return nil, err
			}
			out = append(out, b)
		default:
			return nil, fmt.Errorf("unknown SQLite parameter tag %q", value.T)
		}
	}
	return out, nil
}

func encodeDBValue(value any) taggedValue {
	switch v := value.(type) {
	case nil:
		return taggedValue{T: "n"}
	case int64:
		return taggedValue{T: "i", V: json.RawMessage(strconv.FormatInt(v, 10))}
	case int:
		return taggedValue{T: "i", V: json.RawMessage(strconv.Itoa(v))}
	case float64:
		return taggedValue{T: "f", V: json.RawMessage(strconv.FormatFloat(v, 'g', -1, 64))}
	case bool:
		if v {
			return taggedValue{T: "i", V: json.RawMessage("1")}
		}
		return taggedValue{T: "i", V: json.RawMessage("0")}
	case []byte:
		return taggedValue{T: "b", V: mustJSON(base64.StdEncoding.EncodeToString(v))}
	case time.Time:
		return taggedValue{T: "s", V: mustJSON(v.UTC().Format(time.RFC3339Nano))}
	case string:
		return taggedValue{T: "s", V: mustJSON(v)}
	default:
		return taggedValue{T: "s", V: mustJSON(fmt.Sprint(v))}
	}
}

func mustJSON(value any) json.RawMessage {
	raw, _ := json.Marshal(value)
	return raw
}

func newHandle() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// execute runs one portable SQLite request. JavaScript serializes operations,
// so a transaction and unrelated statements are never interleaved; the single
// Go connection also enforces that at the driver boundary.
func (a *assistantDB) execute(ctx context.Context, req dbRequest) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch req.Op {
	case "begin":
		tx, err := a.db.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		id := newHandle()
		a.txMu.Lock()
		if err := ctx.Err(); err != nil {
			a.txMu.Unlock()
			_ = tx.Rollback()
			return nil, err
		}
		a.txs[id] = tx
		a.txOwners[id] = ctx
		a.txMu.Unlock()
		return map[string]any{"tx": id}, nil
	case "commit", "rollback":
		a.txMu.Lock()
		tx := a.txs[req.Tx]
		delete(a.txs, req.Tx)
		delete(a.txOwners, req.Tx)
		a.txMu.Unlock()
		if tx == nil {
			return nil, errors.New("unknown or finished transaction")
		}
		if req.Op == "commit" {
			return map[string]any{}, tx.Commit()
		}
		return map[string]any{}, tx.Rollback()
	}
	if req.SQL == "" {
		return nil, errors.New("empty SQL")
	}
	params, err := decodeDBParams(req.Params)
	if err != nil {
		return nil, err
	}
	var tx *sql.Tx
	if req.Tx != "" {
		a.txMu.Lock()
		tx = a.txs[req.Tx]
		a.txMu.Unlock()
		if tx == nil {
			return nil, errors.New("unknown or finished transaction")
		}
	}
	switch req.Op {
	case "exec":
		if tx != nil {
			_, err = tx.ExecContext(ctx, req.SQL)
		} else {
			_, err = a.db.ExecContext(ctx, req.SQL)
		}
		return map[string]any{}, err
	case "run":
		var result sql.Result
		if tx != nil {
			result, err = tx.ExecContext(ctx, req.SQL, params...)
		} else {
			result, err = a.db.ExecContext(ctx, req.SQL, params...)
		}
		if err != nil {
			return nil, err
		}
		id, _ := result.LastInsertId()
		n, _ := result.RowsAffected()
		return map[string]any{"lastInsertId": id, "rowsAffected": n}, nil
	case "get", "all":
		var rows *sql.Rows
		if tx != nil {
			rows, err = tx.QueryContext(ctx, req.SQL, params...)
		} else {
			rows, err = a.db.QueryContext(ctx, req.SQL, params...)
		}
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		cols, err := rows.Columns()
		if err != nil {
			return nil, err
		}
		out := make([]map[string]taggedValue, 0, 8)
		for rows.Next() {
			values := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range values {
				ptrs[i] = &values[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				return nil, err
			}
			row := make(map[string]taggedValue, len(cols))
			for i, col := range cols {
				row[col] = encodeDBValue(values[i])
			}
			out = append(out, row)
			if req.Op == "get" {
				break
			}
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		if req.Op == "get" {
			if len(out) == 0 {
				return map[string]any{}, nil
			}
			return map[string]any{"row": out[0]}, nil
		}
		return map[string]any{"rows": out}, nil
	default:
		return nil, fmt.Errorf("unsupported database operation %q", req.Op)
	}
}

// releaseTransactions runs after the owning runtime context is cancelled.
// A BEGIN racing disposal either registers before this lock or sees cancellation.
func (a *assistantDB) releaseTransactions(ctx context.Context) {
	a.txMu.Lock()
	defer a.txMu.Unlock()
	for id, owner := range a.txOwners {
		if owner == ctx {
			_ = a.txs[id].Rollback()
			delete(a.txs, id)
			delete(a.txOwners, id)
		}
	}
}

func (a *assistantDB) status() string {
	if a == nil {
		return ""
	}
	return strings.TrimSpace(a.path)
}
