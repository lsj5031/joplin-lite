package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/lsj5031/joplin-lite/internal/models"
)

// AssistantScope is the immutable note boundary chosen by the user for one Ask
// run. The model never supplies this value.
type AssistantScope struct {
	Kind               string `json:"kind"`
	NoteID             string `json:"noteId,omitempty"`
	NotebookID         string `json:"notebookId,omitempty"`
	IncludeDescendants bool   `json:"includeDescendants,omitempty"`
}

// AssistantSearchQuery is a ranked, scope-bounded full-text query.
type AssistantSearchQuery struct {
	Query  string
	TagID  string
	Scope  AssistantScope
	Limit  int
	Offset int
}

// AssistantSearchHit is a bounded search result. The body is represented only
// by an FTS snippet, never by the whole note.
type AssistantSearchHit struct {
	NoteID        string  `json:"noteId"`
	Title         string  `json:"title"`
	NotebookID    string  `json:"notebookId"`
	NotebookTitle string  `json:"notebookTitle"`
	UpdatedTime   int64   `json:"updatedTime"`
	Snippet       string  `json:"snippet"`
	Score         float64 `json:"score"`
}

// AssistantScopeSQL returns the SQL predicate for a scope and its arguments.
// It assumes the notes table is aliased as n.
func AssistantScopeSQL(scope AssistantScope) (string, []any, error) {
	switch scope.Kind {
	case "vault":
		return "", nil, nil
	case "note":
		if len(scope.NoteID) != 32 {
			return "", nil, fmt.Errorf("invalid note scope")
		}
		return "n.id = ?", []any{scope.NoteID}, nil
	case "notebook":
		if len(scope.NotebookID) != 32 {
			return "", nil, fmt.Errorf("invalid notebook scope")
		}
		if scope.IncludeDescendants {
			return `n.parent_id IN (WITH RECURSIVE ask_scope(id) AS (
					SELECT ?
					UNION ALL
					SELECT f.id FROM folders f JOIN ask_scope s ON f.parent_id = s.id
				) SELECT id FROM ask_scope)`, []any{scope.NotebookID}, nil
		}
		return "n.parent_id = ?", []any{scope.NotebookID}, nil
	default:
		return "", nil, fmt.Errorf("invalid scope kind")
	}
}

// AssistantSearch ranks notes with BM25, giving the title a stronger weight and
// IDs as a deterministic tie-breaker. Conflict copies, trashed notes and
// encryption-applied notes are excluded. The index column order is
// (id UNINDEXED, title, body); BM25 takes one weight per column in that order.
func (db *DB) AssistantSearch(ctx context.Context, q AssistantSearchQuery) ([]AssistantSearchHit, error) {
	query := strings.TrimSpace(q.Query)
	if query == "" {
		return []AssistantSearchHit{}, nil
	}
	fts := ftsQuery(query)
	if fts == "" {
		return []AssistantSearchHit{}, nil
	}
	if q.Limit <= 0 {
		q.Limit = 8
	}
	if q.Limit > 50 {
		q.Limit = 50
	}
	where := []string{
		"jl_notes_fts MATCH ?",
		"n.is_conflict = 0",
		"n.deleted_time = 0",
		"n.encryption_applied = 0",
	}
	args := []any{fts}
	if q.TagID != "" {
		where = append(where, "EXISTS (SELECT 1 FROM note_tags nt WHERE nt.note_id = n.id AND nt.tag_id = ?)")
		args = append(args, q.TagID)
	}
	scopeSQL, scopeArgs, err := AssistantScopeSQL(q.Scope)
	if err != nil {
		return nil, err
	}
	if scopeSQL != "" {
		where = append(where, scopeSQL)
		args = append(args, scopeArgs...)
	}
	args = append(args, q.Limit, q.Offset)
	rows, err := db.QueryContext(ctx, `SELECT n.id, n.title, n.parent_id,
		COALESCE(f.title, ''), n.updated_time,
		snippet(jl_notes_fts, 2, '', '', '…', 28),
		bm25(jl_notes_fts, 0.0, 4.0, 1.0)
		FROM notes n
		JOIN jl_notes_fts ON n.rowid = jl_notes_fts.rowid
		LEFT JOIN folders f ON f.id = n.parent_id
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY bm25(jl_notes_fts, 0.0, 4.0, 1.0) ASC, n.id ASC
		LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AssistantSearchHit, 0, q.Limit)
	for rows.Next() {
		var hit AssistantSearchHit
		var snippet sql.NullString
		if err := rows.Scan(&hit.NoteID, &hit.Title, &hit.NotebookID, &hit.NotebookTitle, &hit.UpdatedTime, &snippet, &hit.Score); err != nil {
			return nil, err
		}
		hit.Snippet = snippet.String
		out = append(out, hit)
	}
	return out, rows.Err()
}

// AssistantNoteInScope reports whether a note currently belongs to the run's
// scope and is readable at all. It is re-checked before every read.
func (db *DB) AssistantNoteInScope(ctx context.Context, scope AssistantScope, noteID string) (bool, error) {
	if len(noteID) != 32 {
		return false, nil
	}
	where := []string{"n.id = ?", "n.is_conflict = 0", "n.deleted_time = 0", "n.encryption_applied = 0"}
	args := []any{noteID}
	scopeSQL, scopeArgs, err := AssistantScopeSQL(scope)
	if err != nil {
		return false, err
	}
	if scopeSQL != "" {
		where = append(where, scopeSQL)
		args = append(args, scopeArgs...)
	}
	var one int
	err = db.QueryRowContext(ctx, `SELECT 1 FROM notes n WHERE `+strings.Join(where, " AND ")+` LIMIT 1`, args...).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

// AssistantNotebooks lists readable notebooks for the scope sidebar and tools.
func (db *DB) AssistantNotebooks(ctx context.Context, scope AssistantScope) ([]*models.Folder, error) {
	where := []string{"deleted_time = 0", "encryption_applied = 0"}
	args := []any{}
	switch scope.Kind {
	case "note", "notebook":
		// Listing is orientation only; return the whole readable notebook tree
		// so the model understands where the scoped note lives.
	case "vault":
	default:
		return nil, fmt.Errorf("invalid scope kind")
	}
	rows, err := db.QueryContext(ctx, `SELECT id, title, parent_id FROM folders WHERE `+strings.Join(where, " AND ")+` ORDER BY title COLLATE NOCASE, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.Folder
	for rows.Next() {
		f := &models.Folder{}
		if err := rows.Scan(&f.ID, &f.Title, &f.ParentID); err != nil {
			return nil, err
		}
		f.Type_ = models.TypeFolder
		out = append(out, f)
	}
	return out, rows.Err()
}

// AssistantTags lists readable tags for search refinement.
func (db *DB) AssistantTags(ctx context.Context) ([]*models.Tag, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, title FROM tags WHERE encryption_applied = 0 ORDER BY title COLLATE NOCASE, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.Tag
	for rows.Next() {
		t := &models.Tag{}
		if err := rows.Scan(&t.ID, &t.Title); err != nil {
			return nil, err
		}
		t.Type_ = models.TypeTag
		out = append(out, t)
	}
	return out, rows.Err()
}
