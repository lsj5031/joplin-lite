package store

import (
	"strings"
	"testing"

	"github.com/lsj5031/joplin-lite/internal/models"
)

func seedSummaryNotes(t *testing.T, db *DB) (folder *models.Folder, tag *models.Tag, ids []string) {
	t.Helper()

	folder = &models.Folder{ID: "fold-1", Title: "Projects", CreatedTime: 1, UpdatedTime: 1}
	if err := db.UpsertFolder(folder); err != nil {
		t.Fatalf("UpsertFolder: %v", err)
	}
	tag = &models.Tag{ID: "tag-1", Title: "sync", CreatedTime: 1, UpdatedTime: 1}
	if err := db.UpsertTag(tag); err != nil {
		t.Fatalf("UpsertTag: %v", err)
	}

	bodies := []string{
		"# First line heading\nsecond line that must not be in the excerpt",
		"plain body mentioning seiippai once",
		"",
		"fourth",
		"fifth",
	}
	for i, body := range bodies {
		n := &models.Note{
			ID:          "note-" + string(rune('a'+i)),
			ParentID:    folder.ID,
			Title:       "note",
			Body:        body,
			CreatedTime: 100,
			UpdatedTime: int64(1000 - i*10), // newest first is note-a, note-b, ...
		}
		if err := db.UpsertNote(n); err != nil {
			t.Fatalf("UpsertNote: %v", err)
		}
		ids = append(ids, n.ID)
	}
	if err := db.AddNoteTag(ids[1], tag.ID); err != nil {
		t.Fatalf("AddNoteTag: %v", err)
	}
	return folder, tag, ids
}

func TestListNoteSummariesKeyset(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	_, tag, ids := seedSummaryNotes(t, db)

	// Page 1: no cursor. Page 2: cursor from page 1. Never overlap, and the
	// body is never shipped — only the excerpt.
	var got []string
	cursor := ""
	for page := 0; page < 3; page++ {
		notes, hasMore, next, err := db.ListNoteSummaries(NoteQuery{}, 2, cursor)
		if err != nil {
			t.Fatalf("ListNoteSummaries page %d: %v", page, err)
		}
		if page < 2 && !hasMore {
			t.Fatalf("page %d: has_more = false before the last page", page)
		}
		if page == 2 && hasMore {
			t.Fatalf("last page: has_more = true")
		}
		for _, n := range notes {
			got = append(got, n.ID)
			if n.Excerpt == "" && n.ID != ids[2] {
				t.Fatalf("%s: empty excerpt", n.ID)
			}
			if len(n.Excerpt) > 141 {
				t.Fatalf("%s: excerpt too long: %q", n.ID, n.Excerpt)
			}
		}
		if !hasMore {
			break
		}
		cursor = next
	}

	if len(got) != len(ids) {
		t.Fatalf("paged %d notes, want %d (%v)", len(got), len(ids), got)
	}
	seen := map[string]bool{}
	for _, id := range got {
		if seen[id] {
			t.Fatalf("note %s appeared on two pages", id)
		}
		seen[id] = true
	}
	// Order is updated_time DESC: note-a first.
	if got[0] != ids[0] {
		t.Fatalf("first row = %s, want %s", got[0], ids[0])
	}

	// The first line is the excerpt, without the markdown heading marker.
	first, _, _, err := db.ListNoteSummaries(NoteQuery{}, 1, "")
	if err != nil {
		t.Fatalf("ListNoteSummaries: %v", err)
	}
	if first[0].Excerpt != "First line heading" {
		t.Fatalf("excerpt = %q, want %q", first[0].Excerpt, "First line heading")
	}

	// Tag filter.
	byTag, _, _, err := db.ListNoteSummaries(NoteQuery{TagID: tag.ID}, 10, "")
	if err != nil {
		t.Fatalf("ListNoteSummaries by tag: %v", err)
	}
	if len(byTag) != 1 || byTag[0].ID != ids[1] {
		t.Fatalf("by tag = %+v, want only %s", byTag, ids[1])
	}
	if len(byTag[0].TagIDs) != 1 || byTag[0].TagIDs[0] != tag.ID {
		t.Fatalf("tag IDs = %v, want [%s]", byTag[0].TagIDs, tag.ID)
	}

	// Search runs on FTS5 and respects the same summary projection.
	found, _, _, err := db.ListNoteSummaries(NoteQuery{Query: "seiippai"}, 10, "")
	if err != nil {
		t.Fatalf("ListNoteSummaries search: %v", err)
	}
	if len(found) != 1 || found[0].ID != ids[1] {
		t.Fatalf("search = %+v, want only %s", found, ids[1])
	}

	// A query with FTS syntax characters must not error.
	if _, _, _, err := db.ListNoteSummaries(NoteQuery{Query: `-bad "quote`}, 10, ""); err != nil {
		t.Fatalf("search with punctuation errored: %v", err)
	}
}

func TestNoteExcerptStripsFirstLine(t *testing.T) {
	cases := map[string]string{
		"# Title\n\nbody":          "Title",
		"  > quoted  line  \nrest": "quoted line",
		"single line":              "single line",
		"":                         "",
	}
	for in, want := range cases {
		if got := noteExcerpt(in); got != want {
			t.Errorf("noteExcerpt(%q) = %q, want %q", in, got, want)
		}
	}
}

// seedSortedNotes gives each note a distinct title, created_time and
// updated_time so every order is unambiguous.
func seedSortedNotes(t *testing.T, db *DB) {
	t.Helper()
	notes := []*models.Note{
		{ID: "n-delta", Title: "Delta", CreatedTime: 100, UpdatedTime: 1200},
		{ID: "n-alpha", Title: "alpha", CreatedTime: 300, UpdatedTime: 1000},
		{ID: "n-charlie", Title: "Charlie", CreatedTime: 200, UpdatedTime: 1400},
		{ID: "n-bravo", Title: "bravo", CreatedTime: 500, UpdatedTime: 1100},
		{ID: "n-echo", Title: "Echo", CreatedTime: 400, UpdatedTime: 1300},
	}
	for _, n := range notes {
		if err := db.UpsertNote(n); err != nil {
			t.Fatalf("UpsertNote %s: %v", n.ID, err)
		}
	}
}

// pageIDs walks every page of one order with a small limit, returning the IDs
// in the order the caller would render them.
func pageIDs(t *testing.T, db *DB, q NoteQuery, limit int) []string {
	t.Helper()
	var ids []string
	cursor := ""
	for page := 0; page < 10; page++ {
		notes, hasMore, next, err := db.ListNoteSummaries(q, limit, cursor)
		if err != nil {
			t.Fatalf("ListNoteSummaries(%v) page %d: %v", q.Sort, page, err)
		}
		for _, n := range notes {
			ids = append(ids, n.ID)
		}
		if !hasMore {
			return ids
		}
		cursor = next
	}
	t.Fatalf("ListNoteSummaries(%v) did not terminate", q.Sort)
	return nil
}

func TestListNoteSummariesSorts(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	seedSortedNotes(t, db)

	cases := []struct {
		sort NoteSort
		want []string
	}{
		{SortUpdatedDesc, []string{"n-charlie", "n-echo", "n-delta", "n-bravo", "n-alpha"}},
		{SortUpdatedAsc, []string{"n-alpha", "n-bravo", "n-delta", "n-echo", "n-charlie"}},
		{SortCreatedDesc, []string{"n-bravo", "n-echo", "n-alpha", "n-charlie", "n-delta"}},
		{SortCreatedAsc, []string{"n-delta", "n-charlie", "n-alpha", "n-echo", "n-bravo"}},
		// Titles sort case-insensitively: alpha, bravo, Charlie, Delta, Echo.
		{SortTitleAsc, []string{"n-alpha", "n-bravo", "n-charlie", "n-delta", "n-echo"}},
		{SortTitleDesc, []string{"n-echo", "n-delta", "n-charlie", "n-bravo", "n-alpha"}},
	}
	for _, tc := range cases {
		t.Run(string(tc.sort), func(t *testing.T) {
			got := pageIDs(t, db, NoteQuery{Sort: tc.sort}, 2)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("order = %v, want %v", got, tc.want)
			}
		})
	}

	// A cursor from another order must not slice this one.
	first, _, next, err := db.ListNoteSummaries(NoteQuery{Sort: SortTitleAsc}, 2, "")
	if err != nil {
		t.Fatalf("title page 1: %v", err)
	}
	if len(first) != 2 {
		t.Fatalf("title page 1 = %d rows", len(first))
	}
	mismatched, _, _, err := db.ListNoteSummaries(NoteQuery{Sort: SortUpdatedDesc}, 2, next)
	if err != nil {
		t.Fatalf("mismatched cursor: %v", err)
	}
	if len(mismatched) == 0 || mismatched[0].ID != "n-charlie" {
		t.Fatalf("mismatched cursor did not restart at the first row: %+v", mismatched)
	}

	// Legacy "<updated_time>:<id>" cursors still page the default order.
	legacy, _, _, err := db.ListNoteSummaries(NoteQuery{}, 2, "1200:n-delta")
	if err != nil {
		t.Fatalf("legacy cursor: %v", err)
	}
	if len(legacy) != 2 || legacy[0].ID != "n-bravo" || legacy[1].ID != "n-alpha" {
		t.Fatalf("legacy cursor page = %+v, want bravo, alpha", legacy)
	}

	// A malformed cursor is ignored rather than erroring.
	if _, _, _, err := db.ListNoteSummaries(NoteQuery{}, 2, "s1:title-asc:%%%:nope"); err != nil {
		t.Fatalf("malformed cursor errored: %v", err)
	}
}

// TestListNoteSummariesTitleCursorColon keeps titles containing ":" pageable:
// the cursor base64s the value instead of splitting on it.
func TestListNoteSummariesTitleCursorColon(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	for _, n := range []*models.Note{
		{ID: "n-one", Title: "a: first", CreatedTime: 1, UpdatedTime: 1},
		{ID: "n-two", Title: "a: second", CreatedTime: 2, UpdatedTime: 2},
	} {
		if err := db.UpsertNote(n); err != nil {
			t.Fatalf("UpsertNote: %v", err)
		}
	}
	got := pageIDs(t, db, NoteQuery{Sort: SortTitleAsc}, 1)
	if strings.Join(got, ",") != "n-one,n-two" {
		t.Fatalf("paged title order = %v", got)
	}
}
