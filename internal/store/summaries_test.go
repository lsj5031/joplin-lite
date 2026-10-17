package store

import (
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
		"# Title\n\nbody":           "Title",
		"  > quoted  line  \nrest":  "quoted line",
		"single line":               "single line",
		"":                          "",
	}
	for in, want := range cases {
		if got := noteExcerpt(in); got != want {
			t.Errorf("noteExcerpt(%q) = %q, want %q", in, got, want)
		}
	}
}
