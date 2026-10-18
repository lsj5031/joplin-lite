package store

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"github.com/lsj5031/joplin-lite/internal/models"
)

// NoteQuery selects a page of note summaries. Query takes precedence only in
// the sense that it is ANDed with the other filters; callers that expose the
// Data API's "query wins" semantics decide that before calling.
//
// Cursor is the keyset position returned by the previous page for Sort. Keyset
// paging is what keeps a deep page from scanning every row before it — OFFSET
// pagination degrades linearly on a large library.
type NoteQuery struct {
	FolderID         string
	TagID            string
	Query            string
	ConflictOnly     bool
	IncludeConflicts bool
	Sort             NoteSort
	Cursor           string
}

// NoteSort is the note-list order. The empty value is the default,
// updated_time descending (Joplin's "last modified" order).
type NoteSort string

const (
	SortUpdatedDesc NoteSort = ""
	SortUpdatedAsc  NoteSort = "updated-asc"
	SortCreatedDesc NoteSort = "created-desc"
	SortCreatedAsc  NoteSort = "created-asc"
	SortTitleAsc    NoteSort = "title-asc"
	SortTitleDesc   NoteSort = "title-desc"
)

// ParseNoteSort accepts the public spellings and falls back to the default for
// anything else, so a stored or API-supplied value can never reach the SQL.
func ParseNoteSort(s string) NoteSort {
	switch NoteSort(s) {
	case SortUpdatedAsc, SortCreatedDesc, SortCreatedAsc, SortTitleAsc, SortTitleDesc:
		return NoteSort(s)
	}
	return SortUpdatedDesc
}

// noteSortSpec names the column, direction and type of one order. Keyset
// comparison and ORDER BY are derived from it, so they can never disagree.
type noteSortSpec struct {
	sort   NoteSort
	column string
	desc   bool
	text   bool
}

func noteSortSpecFor(s NoteSort) noteSortSpec {
	switch s {
	case SortUpdatedAsc:
		return noteSortSpec{s, "n.updated_time", false, false}
	case SortCreatedDesc:
		return noteSortSpec{s, "n.created_time", true, false}
	case SortCreatedAsc:
		return noteSortSpec{s, "n.created_time", false, false}
	case SortTitleAsc:
		return noteSortSpec{s, "n.title", false, true}
	case SortTitleDesc:
		return noteSortSpec{s, "n.title", true, true}
	}
	return noteSortSpec{SortUpdatedDesc, "n.updated_time", true, false}
}

// NoteCursor serializes a keyset position for the default order. IDs are
// 32-hex, so ":" is safe as the separator.
func NoteCursor(updatedTime int64, id string) string {
	return noteCursor(SortUpdatedDesc, strconv.FormatInt(updatedTime, 10), id)
}

// noteCursor serializes a position for any order. The sort is part of the key
// so a cursor is only reused with the query that produced it; text values are
// base64 (URL-safe) because a title may contain ":".
func noteCursor(sort NoteSort, value, id string) string {
	key := sortKey(sort)
	return "s1:" + key + ":" + base64.RawURLEncoding.EncodeToString([]byte(value)) + ":" + id
}

func sortKey(s NoteSort) string {
	if s == SortUpdatedDesc {
		return "updated-desc"
	}
	return string(s)
}

type noteCursorPos struct {
	sort  NoteSort
	value string
	id    string
}

func parseNoteCursor(c string) (noteCursorPos, bool) {
	rest, ok := strings.CutPrefix(c, "s1:")
	if !ok {
		// Legacy "<updated_time>:<id>" cursors (one running app across the
		// upgrade) still page the default order.
		ts, id, ok := strings.Cut(c, ":")
		if !ok || id == "" {
			return noteCursorPos{}, false
		}
		if _, err := strconv.ParseInt(ts, 10, 64); err != nil {
			return noteCursorPos{}, false
		}
		return noteCursorPos{SortUpdatedDesc, ts, id}, true
	}
	parts := strings.SplitN(rest, ":", 3)
	if len(parts) != 3 || parts[2] == "" {
		return noteCursorPos{}, false
	}
	value, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return noteCursorPos{}, false
	}
	return noteCursorPos{ParseNoteSort(parts[0]), string(value), parts[2]}, true
}

// ListNoteSummaries returns one page of list rows in q.Sort order (newest
// first by default). It never selects the full body: only a 320-byte prefix
// reaches Go, which is trimmed to a single-line excerpt. limit+1 rows are
// fetched to report has_more, and the cursor of the last returned row is the
// next page's key.
func (db *DB) ListNoteSummaries(q NoteQuery, limit int, cursor string) ([]*models.NoteSummary, bool, string, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	joins := strings.Builder{}
	where := make([]string, 0, 6)
	args := make([]any, 0, 8)

	if q.Query != "" {
		fts := ftsQuery(q.Query)
		if fts == "" {
			return []*models.NoteSummary{}, false, "", nil
		}
		joins.WriteString(" JOIN jl_notes_fts ON n.rowid = jl_notes_fts.rowid")
		where = append(where, "jl_notes_fts MATCH ?")
		args = append(args, fts)
	}
	if q.ConflictOnly {
		where = append(where, "n.is_conflict = 1")
	} else if !q.IncludeConflicts {
		where = append(where, "n.is_conflict = 0")
	}
	if q.FolderID != "" {
		where = append(where, "n.parent_id = ?")
		args = append(args, q.FolderID)
	}
	if q.TagID != "" {
		where = append(where, "EXISTS (SELECT 1 FROM note_tags nt WHERE nt.note_id = n.id AND nt.tag_id = ?)")
		args = append(args, q.TagID)
	}
	spec := noteSortSpecFor(q.Sort)
	// A cursor made for a different order cannot describe a position in this
	// one; ignore it and start from the first page instead of returning a
	// confusing slice.
	if cursor != "" {
		if pos, ok := parseNoteCursor(cursor); ok && pos.sort == spec.sort {
			op := "<"
			if !spec.desc {
				op = ">"
			}
			column := spec.column
			if spec.text {
				column += " COLLATE NOCASE"
			}
			if spec.text {
				where = append(where, fmt.Sprintf("(%s %s ? OR (%s = ? AND n.id %s ?))", column, op, column, op))
				args = append(args, pos.value, pos.value, pos.id)
			} else if ts, err := strconv.ParseInt(pos.value, 10, 64); err == nil {
				where = append(where, fmt.Sprintf("(%s %s ? OR (%s = ? AND n.id %s ?))", column, op, column, op))
				args = append(args, ts, ts, pos.id)
			}
		}
	}

	sql := `SELECT n.id, n.parent_id, n.title, substr(n.body, 1, 320), n.created_time, n.updated_time,
		n.is_conflict, n.is_todo, n.todo_due, n.todo_completed, n.conflict_original_id,
		COALESCE((SELECT group_concat(nt.tag_id, ',') FROM note_tags nt WHERE nt.note_id = n.id), '')
		FROM notes n` + joins.String()
	if len(where) > 0 {
		sql += " WHERE " + strings.Join(where, " AND ")
	}
	dir := "DESC"
	if !spec.desc {
		dir = "ASC"
	}
	orderCol := spec.column
	if spec.text {
		orderCol += " COLLATE NOCASE"
	}
	sql += fmt.Sprintf(" ORDER BY %s %s, n.id %s LIMIT ?", orderCol, dir, dir)
	args = append(args, limit+1)

	rows, err := db.queryStmt(sql, args...)
	if err != nil {
		return nil, false, "", err
	}
	defer rows.Close()

	out := make([]*models.NoteSummary, 0, limit)
	for rows.Next() {
		s := &models.NoteSummary{}
		var tagIDs string
		if err := rows.Scan(
			&s.ID, &s.ParentID, &s.Title, &s.Excerpt, &s.CreatedTime, &s.UpdatedTime,
			&s.IsConflict, &s.IsTodo, &s.TodoDue, &s.TodoCompleted, &s.ConflictOriginalID,
			&tagIDs,
		); err != nil {
			return nil, false, "", err
		}
		s.Excerpt = noteExcerpt(s.Excerpt)
		if tagIDs != "" {
			s.TagIDs = strings.Split(tagIDs, ",")
		}
		s.Type_ = models.TypeNote
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, false, "", err
	}

	hasMore := len(out) > limit
	if hasMore {
		out = out[:limit]
	}
	next := ""
	if n := len(out); n > 0 {
		last := out[n-1]
		value := strconv.FormatInt(last.UpdatedTime, 10)
		switch spec.sort {
		case SortCreatedDesc, SortCreatedAsc:
			value = strconv.FormatInt(last.CreatedTime, 10)
		case SortTitleAsc, SortTitleDesc:
			value = last.Title
		}
		next = noteCursor(spec.sort, value, last.ID)
	}
	return out, hasMore, next, nil
}

// noteExcerpt reduces a body prefix to the single readable line a list row
// shows: first line, markdown heading markers stripped, whitespace collapsed,
// and hard-truncated by runes so a long CJK title cannot overflow the row.
//
// The fast path does not allocate: most bodies are already one short line,
// and this runs once per list row on every page (the benchmark in
// summaries_bench_test.go tracks it).
func noteExcerpt(body string) string {
	body = strings.TrimSpace(body)
	if i := strings.IndexAny(body, "\r\n"); i >= 0 {
		body = body[:i]
	}
	body = strings.TrimLeft(body, "#> \t")

	if needsSpaceCollapse(body) {
		body = strings.Join(strings.Fields(body), " ")
	}

	const maxRunes = 140
	if len(body) <= maxRunes { // ASCII and short: bytes == runes, nothing to cut
		return body
	}
	r := []rune(body)
	if len(r) > maxRunes {
		// Keep the whole excerpt inside the budget: 139 runes + ellipsis.
		return string(r[:maxRunes-1]) + "…"
	}
	return body
}

// needsSpaceCollapse reports whether the excerpt has a tab or a run of
// spaces that the one-line display would render literally.
func needsSpaceCollapse(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '\t' || s[i] == '\r' || s[i] == '\n' {
			return true
		}
		if s[i] == ' ' && i+1 < len(s) && s[i+1] == ' ' {
			return true
		}
	}
	return false
}

// ftsQuery turns free text into a safe FTS5 expression: every whitespace
// token is quoted and joined with AND. Raw user input is not valid FTS5
// syntax (a stray "-", ":" or unbalanced quote makes MATCH fail), and phrase
// semantics on the whole string would require words to be adjacent.
func ftsQuery(s string) string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		parts = append(parts, `"`+strings.ReplaceAll(f, `"`, `""`)+`"`)
	}
	return strings.Join(parts, " AND ")
}

// String makes NoteQuery easy to log without dumping every field by hand.
func (q NoteQuery) String() string {
	return fmt.Sprintf("folder=%q tag=%q query=%q conflict=%v include=%v cursor=%q",
		q.FolderID, q.TagID, q.Query, q.ConflictOnly, q.IncludeConflicts, q.Cursor)
}
