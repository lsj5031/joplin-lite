package store

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

// DB wraps a sql.DB for Joplin data storage. Hot queries go through
// queryStmt, which caches prepared statements: database/sql alone re-prepares
// SQL for each new connection, and the list/search paths re-parse the same
// statements on every keystroke.
type DB struct {
	*sql.DB
	dataDir string

	stmtMu sync.Mutex
	stmts  map[string]*sql.Stmt
}

// queryStmt runs a cached prepared statement.
func (db *DB) queryStmt(query string, args ...any) (*sql.Rows, error) {
	stmt, err := db.prepared(query)
	if err != nil {
		return nil, err
	}
	return stmt.Query(args...)
}

func (db *DB) prepared(query string) (*sql.Stmt, error) {
	db.stmtMu.Lock()
	defer db.stmtMu.Unlock()
	if s, ok := db.stmts[query]; ok {
		return s, nil
	}
	s, err := db.DB.Prepare(query)
	if err != nil {
		return nil, err
	}
	db.stmts[query] = s
	return s, nil
}

// Close releases cached statements, then the database.
func (db *DB) Close() error {
	db.stmtMu.Lock()
	for q, s := range db.stmts {
		_ = s.Close()
		delete(db.stmts, q)
	}
	db.stmtMu.Unlock()
	return db.DB.Close()
}

// Open opens (or creates) a Joplin-compatible SQLite database in the given directory.
func Open(dataDir string) (*DB, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("cannot create data directory: %w", err)
	}

	dbPath := filepath.Join(dataDir, "gojoplin.sqlite")
	// cgo-free driver (modernc.org/sqlite): per-connection pragmas must be passed
	// in the driver's _pragma= DSN form; the mattn-style _journal_mode params are ignored.
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("cannot open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("cannot connect to database: %w", err)
	}

	// One writer at a time is SQLite's rule; WAL still lets readers proceed.
	// Without a cap the UI and the sync workers can pile up connections and
	// fight over the write lock, so keep the pool small and bounded.
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)

	store := &DB{DB: db, dataDir: dataDir, stmts: make(map[string]*sql.Stmt)}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migration failed: %w", err)
	}

	slog.Info("database opened", "path", dbPath)
	return store, nil
}

// DataDir returns the data directory path.
func (db *DB) DataDir() string {
	return db.dataDir
}

// ResourceDir returns the path where resource files are stored.
func (db *DB) ResourceDir() string {
	dir := filepath.Join(db.dataDir, "resources")
	os.MkdirAll(dir, 0o755)
	return dir
}

func (db *DB) migrate() error {
	if db.coreTablesExist() {
		// An existing database — either a joplin-lite store or a real Joplin
		// profile. Its schema is authoritative; schemaStatements only ever run
		// on a fresh file. Just keep the sidecars in shape.
		slog.Info("existing database, ensuring sidecar schema", "version", db.schemaVersion())
	} else {
		// Fresh file: create the Joplin v49 schema.
		slog.Info("running migrations", "from_version", 0, "to_version", 49)

		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		for _, stmt := range schemaStatements {
			if _, err := tx.Exec(stmt); err != nil {
				return fmt.Errorf("migration statement failed: %w\nSQL: %s", err, stmt)
			}
		}

		// Set schema version
		if _, err := tx.Exec(`INSERT OR REPLACE INTO key_values (key, value, type, updated_time) VALUES ('schema_version', '49', 1, strftime('%s','now') * 1000)`); err != nil {
			return err
		}

		if err := tx.Commit(); err != nil {
			return err
		}
	}

	if err := db.ensureSidecarSchema(); err != nil {
		return err
	}
	return db.ensureSearchIndex()
}

// coreTablesExist reports whether the Joplin core tables are already present.
// This is how a fresh file is told apart from an existing Joplin database: a
// real Joplin profile tracks its schema in a `version` table (v2+), not in
// key_values.schema_version, so the marker alone is not a reliable signal.
func (db *DB) coreTablesExist() bool {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('key_values','notes','folders')`).Scan(&n); err != nil {
		return false
	}
	return n == 3
}

// schemaVersion reads the schema version, preferring Joplin's own `version`
// table (present in real desktop/server profiles, currently v54) and falling
// back to the joplin-lite marker.
func (db *DB) schemaVersion() int {
	var v int
	if err := db.QueryRow(`SELECT version FROM version LIMIT 1`).Scan(&v); err == nil {
		return v
	}
	if err := db.QueryRow(`SELECT value FROM key_values WHERE key = 'schema_version'`).Scan(&v); err == nil {
		return v
	}
	return 0
}

// ensureSidecarSchema creates/updates the joplin-lite sidecar tables. It runs
// on every open: schemaStatements only execute on a fresh file, so a database
// created before a sidecar table existed — or a real Joplin profile — would
// otherwise never get it. Names that could collide with a real Joplin table
// are prefixed (`jl_revisions`; Joplin's own `revisions` holds note history).
func (db *DB) ensureSidecarSchema() error {
	for _, stmt := range []string{createItemRawSQL, createRevisionsSQL} {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("sidecar schema statement failed: %w\nSQL: %s", err, stmt)
		}
	}
	// Guarded columns: databases that created `jl_revisions` before the
	// encryption columns existed.
	for _, col := range []string{
		"encryption_applied INT NOT NULL DEFAULT 0",
		"encryption_cipher_text TEXT NOT NULL DEFAULT ''",
	} {
		if _, err := db.Exec(`ALTER TABLE jl_revisions ADD COLUMN ` + col); err != nil &&
			!strings.Contains(err.Error(), "duplicate column name") {
			return err
		}
	}
	// List indexes for the UI's access paths: the note list is always
	// newest-first and usually scoped to a folder or a tag, so an index that
	// already matches the ORDER BY lets keyset paging seek instead of sort.
	for _, stmt := range []string{
		`CREATE INDEX IF NOT EXISTS idx_notes_updated_id ON notes(updated_time DESC, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_notes_parent_conflict_updated ON notes(parent_id, is_conflict, updated_time DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_notes_conflict_updated ON notes(is_conflict, updated_time DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_note_tags_tag_note ON note_tags(tag_id, note_id)`,
		`CREATE INDEX IF NOT EXISTS idx_sync_items_item_target ON sync_items(item_id, sync_target)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("index statement failed: %w\nSQL: %s", err, stmt)
		}
	}
	return nil
}

// ensureSearchIndex (re)creates the local FTS5 search index and, when it is
// missing or out of step with `notes`, rebuilds it from the content table.
// Joplin's own index is FTS4 (`notes_fts`/`items_fts`), which the cgo-free
// modernc driver cannot read, so this index is named distinctly
// (`jl_notes_fts`) and never collides with a real profile's tables.
func (db *DB) ensureSearchIndex() error {
	for _, stmt := range searchIndexStatements {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("search index statement failed: %w\nSQL: %s", err, stmt)
		}
	}

	// The triggers keep the index current from here on, so the two COUNT(*)
	// scans are needed once per database, not on every open. The marker is
	// written only after a successful (re)build; if the FTS table was dropped
	// the CREATE above puts an empty one back, and a missing marker rebuilds.
	var ready string
	err := db.QueryRow(`SELECT value FROM key_values WHERE key = 'jl.fts.ready'`).Scan(&ready)
	if err == nil && ready == "1" {
		return nil
	}
	if err != nil && err != sql.ErrNoRows {
		return err
	}

	// The FTS5 shadow `_docsize` table holds one row per indexed document, so
	// its count is the true index size (an external-content FTS table reports
	// its content table for a plain SELECT, so that cannot be used here).
	indexed := -1
	if err := db.QueryRow(`SELECT COUNT(*) FROM jl_notes_fts_docsize`).Scan(&indexed); err != nil {
		indexed = -1 // never built
	}
	var notes int
	if err := db.QueryRow(`SELECT COUNT(*) FROM notes`).Scan(&notes); err != nil {
		return err
	}
	if indexed != notes {
		slog.Info("rebuilding search index", "indexed", indexed, "notes", notes)
		if _, err := db.Exec(`INSERT INTO jl_notes_fts(jl_notes_fts) VALUES('rebuild')`); err != nil {
			return fmt.Errorf("search index rebuild failed: %w", err)
		}
	}
	_, err = db.Exec(`INSERT OR REPLACE INTO key_values (key, value, type, updated_time)
		VALUES ('jl.fts.ready', '1', 1, strftime('%s','now') * 1000)`)
	return err
}

const (
	// createItemRawSQL is the raw passthrough store (joplin-lite sidecar, not
	// part of the Joplin v49 schema): exact bytes of pulled items, pushed
	// back unmodified.
	createItemRawSQL = `CREATE TABLE IF NOT EXISTS item_raw (
		id TEXT PRIMARY KEY,
		raw BLOB NOT NULL,
		updated_time INT NOT NULL DEFAULT 0
	)`

	// createRevisionsSQL is the note-history index: the full serialized item
	// — including the diff fields — stays in item_raw (raw passthrough);
	// this table indexes what listing/filtering and the decrypt pass need.
	// Named `jl_revisions`: a real Joplin profile already has a `revisions`
	// table (its note history, with title_diff/body_diff/metadata_diff), and
	// CREATE ... IF NOT EXISTS would silently keep that incompatible shape.
	createRevisionsSQL = `CREATE TABLE IF NOT EXISTS jl_revisions (
		id TEXT PRIMARY KEY,
		parent_id TEXT NOT NULL DEFAULT '',
		item_id TEXT NOT NULL DEFAULT '',
		item_type INT NOT NULL DEFAULT 0,
		item_updated_time INT NOT NULL DEFAULT 0,
		created_time INT NOT NULL DEFAULT 0,
		updated_time INT NOT NULL DEFAULT 0,
		encryption_applied INT NOT NULL DEFAULT 0,
		encryption_cipher_text TEXT NOT NULL DEFAULT ''
	)`
)

// schemaStatements creates the Joplin v49 schema.
var schemaStatements = []string{
	// Key-value store (must be first for schema version tracking)
	`CREATE TABLE IF NOT EXISTS key_values (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		key TEXT NOT NULL UNIQUE,
		value TEXT NOT NULL,
		type INTEGER NOT NULL DEFAULT 1,
		updated_time INT NOT NULL DEFAULT 0
	)`,

	// Folders (notebooks)
	`CREATE TABLE IF NOT EXISTS folders (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL DEFAULT '',
		created_time INT NOT NULL DEFAULT 0,
		updated_time INT NOT NULL DEFAULT 0,
		user_created_time INT NOT NULL DEFAULT 0,
		user_updated_time INT NOT NULL DEFAULT 0,
		encryption_cipher_text TEXT NOT NULL DEFAULT '',
		encryption_applied INT NOT NULL DEFAULT 0,
		parent_id TEXT NOT NULL DEFAULT '',
		is_shared INT NOT NULL DEFAULT 0,
		share_id TEXT NOT NULL DEFAULT '',
		master_key_id TEXT NOT NULL DEFAULT '',
		icon TEXT NOT NULL DEFAULT '',
		user_data TEXT NOT NULL DEFAULT '',
		deleted_time INT NOT NULL DEFAULT 0
	)`,

	// Notes
	`CREATE TABLE IF NOT EXISTS notes (
		id TEXT PRIMARY KEY,
		parent_id TEXT NOT NULL DEFAULT '',
		title TEXT NOT NULL DEFAULT '',
		body TEXT NOT NULL DEFAULT '',
		created_time INT NOT NULL DEFAULT 0,
		updated_time INT NOT NULL DEFAULT 0,
		is_conflict INT NOT NULL DEFAULT 0,
		latitude NUMERIC NOT NULL DEFAULT 0,
		longitude NUMERIC NOT NULL DEFAULT 0,
		altitude NUMERIC NOT NULL DEFAULT 0,
		author TEXT NOT NULL DEFAULT '',
		source_url TEXT NOT NULL DEFAULT '',
		is_todo INT NOT NULL DEFAULT 0,
		todo_due INT NOT NULL DEFAULT 0,
		todo_completed INT NOT NULL DEFAULT 0,
		source TEXT NOT NULL DEFAULT '',
		source_application TEXT NOT NULL DEFAULT '',
		application_data TEXT NOT NULL DEFAULT '',
		"order" INT NOT NULL DEFAULT 0,
		user_created_time INT NOT NULL DEFAULT 0,
		user_updated_time INT NOT NULL DEFAULT 0,
		encryption_cipher_text TEXT NOT NULL DEFAULT '',
		encryption_applied INT NOT NULL DEFAULT 0,
		markup_language INT NOT NULL DEFAULT 1,
		is_shared INT NOT NULL DEFAULT 0,
		share_id TEXT NOT NULL DEFAULT '',
		conflict_original_id TEXT NOT NULL DEFAULT '',
		master_key_id TEXT NOT NULL DEFAULT '',
		user_data TEXT NOT NULL DEFAULT '',
		deleted_time INT NOT NULL DEFAULT 0
	)`,

	// Tags
	`CREATE TABLE IF NOT EXISTS tags (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL DEFAULT '',
		created_time INT NOT NULL DEFAULT 0,
		updated_time INT NOT NULL DEFAULT 0,
		user_created_time INT NOT NULL DEFAULT 0,
		user_updated_time INT NOT NULL DEFAULT 0,
		encryption_cipher_text TEXT NOT NULL DEFAULT '',
		encryption_applied INT NOT NULL DEFAULT 0,
		is_shared INT NOT NULL DEFAULT 0,
		parent_id TEXT NOT NULL DEFAULT '',
		user_data TEXT NOT NULL DEFAULT ''
	)`,

	// Note-Tag junction
	`CREATE TABLE IF NOT EXISTS note_tags (
		id TEXT PRIMARY KEY,
		note_id TEXT NOT NULL DEFAULT '',
		tag_id TEXT NOT NULL DEFAULT '',
		created_time INT NOT NULL DEFAULT 0,
		updated_time INT NOT NULL DEFAULT 0,
		user_created_time INT NOT NULL DEFAULT 0,
		user_updated_time INT NOT NULL DEFAULT 0,
		encryption_cipher_text TEXT NOT NULL DEFAULT '',
		encryption_applied INT NOT NULL DEFAULT 0,
		is_shared INT NOT NULL DEFAULT 0
	)`,

	// Resources
	`CREATE TABLE IF NOT EXISTS resources (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL DEFAULT '',
		mime TEXT NOT NULL DEFAULT '',
		filename TEXT NOT NULL DEFAULT '',
		created_time INT NOT NULL DEFAULT 0,
		updated_time INT NOT NULL DEFAULT 0,
		user_created_time INT NOT NULL DEFAULT 0,
		user_updated_time INT NOT NULL DEFAULT 0,
		file_extension TEXT NOT NULL DEFAULT '',
		encryption_cipher_text TEXT NOT NULL DEFAULT '',
		encryption_applied INT NOT NULL DEFAULT 0,
		encryption_blob_encrypted INT NOT NULL DEFAULT 0,
		size INT NOT NULL DEFAULT 0,
		is_shared INT NOT NULL DEFAULT 0,
		share_id TEXT NOT NULL DEFAULT '',
		master_key_id TEXT NOT NULL DEFAULT '',
		user_data TEXT NOT NULL DEFAULT '',
		blob_updated_time INT NOT NULL DEFAULT 0
	)`,

	// Master keys (E2EE)
	`CREATE TABLE IF NOT EXISTS master_keys (
		id TEXT PRIMARY KEY,
		created_time INT NOT NULL DEFAULT 0,
		updated_time INT NOT NULL DEFAULT 0,
		source_application TEXT NOT NULL DEFAULT '',
		encryption_method INT NOT NULL DEFAULT 0,
		checksum TEXT NOT NULL DEFAULT '',
		content TEXT NOT NULL DEFAULT ''
	)`,

	// Sync items
	`CREATE TABLE IF NOT EXISTS sync_items (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		sync_target INT NOT NULL,
		sync_time INT NOT NULL DEFAULT 0,
		item_type INT NOT NULL DEFAULT 0,
		item_id TEXT NOT NULL DEFAULT '',
		sync_disabled INT NOT NULL DEFAULT 0,
		sync_disabled_reason TEXT NOT NULL DEFAULT '',
		force_sync INT NOT NULL DEFAULT 0,
		item_location INT NOT NULL DEFAULT 0
	)`,

	// Deleted items
	`CREATE TABLE IF NOT EXISTS deleted_items (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		item_type INT NOT NULL,
		item_id TEXT NOT NULL,
		deleted_time INT NOT NULL DEFAULT 0,
		sync_target INT NOT NULL DEFAULT 0
	)`,

	// Item changes (for events API)
	`CREATE TABLE IF NOT EXISTS item_changes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		item_type INT NOT NULL,
		item_id TEXT NOT NULL,
		type INT NOT NULL,
		created_time INT NOT NULL DEFAULT 0
	)`,

	createItemRawSQL,

	// Indexes
	`CREATE INDEX IF NOT EXISTS idx_notes_parent_id ON notes(parent_id)`,
	`CREATE INDEX IF NOT EXISTS idx_notes_is_todo ON notes(is_todo)`,
	`CREATE INDEX IF NOT EXISTS idx_note_tags_note_id ON note_tags(note_id)`,
	`CREATE INDEX IF NOT EXISTS idx_note_tags_tag_id ON note_tags(tag_id)`,
	`CREATE INDEX IF NOT EXISTS idx_sync_items_item_id ON sync_items(item_id)`,
	`CREATE INDEX IF NOT EXISTS idx_sync_items_sync_target ON sync_items(sync_target)`,
	`CREATE INDEX IF NOT EXISTS idx_deleted_items_sync_target ON deleted_items(sync_target)`,
	`CREATE INDEX IF NOT EXISTS idx_item_changes_item_id ON item_changes(item_id)`,
	`CREATE INDEX IF NOT EXISTS idx_item_changes_created_time ON item_changes(created_time)`,
}

// searchIndexStatements is the local notes search index. FTS5, not FTS4: the
// cgo-free modernc.org/sqlite build ships SQLITE_ENABLE_FTS5 only (no FTS3/4),
// and a real Joplin profile's own `notes_fts` is FTS4 and unreadable here. The
// `jl_` prefix keeps this index from colliding with it. External-content over
// `notes` with the canonical (delete-then-insert) triggers.
var searchIndexStatements = []string{
	`CREATE VIRTUAL TABLE IF NOT EXISTS jl_notes_fts USING fts5(
		id UNINDEXED,
		title,
		body,
		content='notes',
		content_rowid='rowid'
	)`,

	`CREATE TRIGGER IF NOT EXISTS jl_notes_fts_before_update BEFORE UPDATE ON notes BEGIN
		INSERT INTO jl_notes_fts(jl_notes_fts, rowid, id, title, body) VALUES('delete', old.rowid, old.id, old.title, old.body);
	END`,

	`CREATE TRIGGER IF NOT EXISTS jl_notes_fts_before_delete BEFORE DELETE ON notes BEGIN
		INSERT INTO jl_notes_fts(jl_notes_fts, rowid, id, title, body) VALUES('delete', old.rowid, old.id, old.title, old.body);
	END`,

	`CREATE TRIGGER IF NOT EXISTS jl_notes_fts_after_update AFTER UPDATE ON notes BEGIN
		INSERT INTO jl_notes_fts(rowid, id, title, body) VALUES(new.rowid, new.id, new.title, new.body);
	END`,

	`CREATE TRIGGER IF NOT EXISTS jl_notes_fts_after_insert AFTER INSERT ON notes BEGIN
		INSERT INTO jl_notes_fts(rowid, id, title, body) VALUES(new.rowid, new.id, new.title, new.body);
	END`,
}

func (db *DB) getKV(key string) string {
	var val string
	err := db.QueryRow("SELECT value FROM key_values WHERE key = ?", key).Scan(&val)
	if err != nil {
		return ""
	}
	return val
}

func (db *DB) setKV(key, value string) error {
	_, err := db.Exec(`INSERT OR REPLACE INTO key_values (key, value, type, updated_time) VALUES (?, ?, 1, strftime('%s','now') * 1000)`, key, value)
	return err
}

// GetSetting returns a key_values entry ("" when unset). Unlike getKV it
// reports errors: this is the exported settings seam for the UI service
// layer (sync target, clipper token, master password reference).
func (db *DB) GetSetting(key string) (string, error) {
	var val string
	err := db.QueryRow("SELECT value FROM key_values WHERE key = ?", key).Scan(&val)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return val, err
}

// SetSetting stores a key_values entry.
func (db *DB) SetSetting(key, value string) error {
	return db.setKV(key, value)
}
