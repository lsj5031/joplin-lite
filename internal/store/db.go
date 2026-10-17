package store

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// DB wraps a sql.DB for Joplin data storage.
type DB struct {
	*sql.DB
	dataDir string
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

	store := &DB{DB: db, dataDir: dataDir}
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
	// Check current schema version
	var version int
	row := db.QueryRow("SELECT value FROM key_values WHERE key = 'schema_version'")
	if err := row.Scan(&version); err != nil {
		// Table doesn't exist yet, create everything
		version = 0
	}

	if version >= 49 {
		return nil
	}

	slog.Info("running migrations", "from_version", version, "to_version", 49)

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

	return tx.Commit()
}

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

	// Raw passthrough store (joplin-lite sidecar, not part of the Joplin v49
	// schema): exact bytes of pulled items, pushed back unmodified.
	`CREATE TABLE IF NOT EXISTS item_raw (
		id TEXT PRIMARY KEY,
		raw BLOB NOT NULL,
		updated_time INT NOT NULL DEFAULT 0
	)`,

	// Note-history index (revisions): the full serialized item — including
	// the diff fields — stays in item_raw (raw passthrough); this table
	// indexes what listing/filtering needs.
	`CREATE TABLE IF NOT EXISTS revisions (
		id TEXT PRIMARY KEY,
		parent_id TEXT NOT NULL DEFAULT '',
		item_id TEXT NOT NULL DEFAULT '',
		item_type INT NOT NULL DEFAULT 0,
		item_updated_time INT NOT NULL DEFAULT 0,
		created_time INT NOT NULL DEFAULT 0,
		updated_time INT NOT NULL DEFAULT 0
	)`,

	// FTS for notes search. FTS5, not FTS4: the cgo-free modernc.org/sqlite
	// build ships SQLITE_ENABLE_FTS5 only (no FTS3/4). This table is a local
	// search index and is not part of the Joplin sync format.
	`CREATE VIRTUAL TABLE IF NOT EXISTS notes_fts USING fts5(
		id UNINDEXED,
		title,
		body,
		content='notes',
		content_rowid='rowid'
	)`,

	// FTS triggers (canonical external-content pattern)
	`CREATE TRIGGER IF NOT EXISTS notes_fts_before_update BEFORE UPDATE ON notes BEGIN
		INSERT INTO notes_fts(notes_fts, rowid, id, title, body) VALUES('delete', old.rowid, old.id, old.title, old.body);
	END`,

	`CREATE TRIGGER IF NOT EXISTS notes_fts_before_delete BEFORE DELETE ON notes BEGIN
		INSERT INTO notes_fts(notes_fts, rowid, id, title, body) VALUES('delete', old.rowid, old.id, old.title, old.body);
	END`,

	`CREATE TRIGGER IF NOT EXISTS notes_fts_after_update AFTER UPDATE ON notes BEGIN
		INSERT INTO notes_fts(rowid, id, title, body) VALUES(new.rowid, new.id, new.title, new.body);
	END`,

	`CREATE TRIGGER IF NOT EXISTS notes_fts_after_insert AFTER INSERT ON notes BEGIN
		INSERT INTO notes_fts(rowid, id, title, body) VALUES(new.rowid, new.id, new.title, new.body);
	END`,

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
