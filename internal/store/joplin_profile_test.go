package store

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/lsj5031/joplin-lite/internal/models"
)

// TestOpen_RealJoplinProfileLookalike pins the real-profile compatibility fix:
// a database shaped like a Joplin desktop profile — schema tracked in its own
// `version` table (v54), no key_values.schema_version marker, its OWN FTS4
// `notes_fts`, and its OWN `revisions` note-history table with diff columns —
// must open cleanly, keep its tables untouched, and get working local
// sidecars (jl_revisions + jl_notes_fts) next to them.
//
// The FTS4 module is not available in the cgo-free build, so Joplin's
// notes_fts is simulated as a plain table with the same name: the point is
// that we never read or replace it, not that it is really FTS4.
func TestOpen_RealJoplinProfileLookalike(t *testing.T) {
	dir := t.TempDir()

	// 1. Start from a joplin-lite store so the core `notes` schema exists.
	seed, err := Open(dir)
	if err != nil {
		t.Fatalf("seed Open: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}

	// 2. Reshape it into a Joplin-profile lookalike with raw SQL.
	raw, err := sql.Open("sqlite", filepath.Join(dir, "gojoplin.sqlite"))
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	exec := func(q string) {
		t.Helper()
		if _, err := raw.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`CREATE TABLE version (version INT NOT NULL, table_fields_version INT NOT NULL)`)
	exec(`INSERT INTO version VALUES (54, 54)`)
	exec(`DELETE FROM key_values WHERE key = 'schema_version'`)
	exec(`CREATE TABLE revisions (
		id TEXT PRIMARY KEY, parent_id TEXT NOT NULL DEFAULT '', item_type INT NOT NULL,
		item_id TEXT NOT NULL, item_updated_time INT NOT NULL,
		title_diff TEXT NOT NULL DEFAULT '', body_diff TEXT NOT NULL DEFAULT '',
		metadata_diff TEXT NOT NULL DEFAULT '')`)
	exec(`CREATE TABLE notes_fts (id TEXT)`)
	// A DB created before the sidecars existed would not have them.
	exec(`DROP TABLE jl_revisions`)
	exec(`DROP TABLE jl_notes_fts`)
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	// 3. Re-open: must recognise the real profile and install sidecars beside it.
	db, err := Open(dir)
	if err != nil {
		t.Fatalf("Open real-profile lookalike: %v", err)
	}
	defer db.Close()

	if v := db.schemaVersion(); v != 54 {
		t.Fatalf("schemaVersion = %d, want 54 (from the version table)", v)
	}
	// A real profile must not gain a fabricated joplin-lite marker.
	if v, err := db.GetSetting("schema_version"); err != nil || v != "" {
		t.Fatalf("key_values.schema_version = %q, %v; want empty (do not fake it into a real profile)", v, err)
	}

	// Joplin's own tables must be intact.
	var titleDiff int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('revisions') WHERE name = 'title_diff'`).Scan(&titleDiff); err != nil || titleDiff != 1 {
		t.Fatalf("Joplin revisions shape changed: title_diff count = %d, %v", titleDiff, err)
	}
	var ourCol int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('revisions') WHERE name = 'created_time'`).Scan(&ourCol); err != nil || ourCol != 0 {
		t.Fatalf("our columns leaked into Joplin's revisions table: created_time count = %d, %v", ourCol, err)
	}
	var notesFTS int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'notes_fts'`).Scan(&notesFTS); err != nil || notesFTS != 1 {
		t.Fatalf("Joplin's notes_fts table was disturbed: count = %d, %v", notesFTS, err)
	}

	// Our sidecars exist and search works through our own index.
	var jlRev int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'jl_revisions'`).Scan(&jlRev); err != nil || jlRev != 1 {
		t.Fatalf("jl_revisions not created: %d, %v", jlRev, err)
	}
	if err := db.CreateNote(&models.Note{Title: "needle", Body: "haystack body"}); err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	got, _, err := db.SearchNotes("needle", 10, 0)
	if err != nil {
		t.Fatalf("SearchNotes: %v", err)
	}
	if len(got) != 1 || got[0].Title != "needle" {
		t.Fatalf("SearchNotes = %d notes, want the inserted one", len(got))
	}

	// The revision index is ours, independent of Joplin's revisions table.
	if err := db.UpsertRevision(&models.Revision{ID: "rev1", ItemID: "note1", ItemType: models.TypeNote}); err != nil {
		t.Fatalf("UpsertRevision: %v", err)
	}
	if n, err := db.CountRevisions(); err != nil || n != 1 {
		t.Fatalf("CountRevisions = %d, %v; want 1", n, err)
	}
}
