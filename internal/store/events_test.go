package store

import (
	"sort"
	"strings"
	"testing"

	"github.com/lsj5031/joplin-lite/internal/models"
)

// Event log (MOD-13): every note/folder/tag mutation appends an item_changes
// row and the feed is read strictly after a cursor, with no duplicates and no
// skipped rows across pages.

func TestEventsPagingByCursor(t *testing.T) {
	db := testDB(t)

	note := &models.Note{ID: strings.Repeat("a", 32), Title: "event note", Body: "one"}
	if err := db.CreateNote(note); err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	folder := &models.Folder{Title: "Events inbox"}
	if err := db.CreateFolder(folder); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	tag := &models.Tag{Title: "events-tag"}
	if err := db.CreateTag(tag); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	note.Title = "event note (edited)"
	if err := db.UpdateNote(note); err != nil {
		t.Fatalf("UpdateNote: %v", err)
	}
	if err := db.AddNoteTag(note.ID, tag.ID); err != nil {
		t.Fatalf("AddNoteTag: %v", err)
	}
	if err := db.DeleteTag(tag.ID); err != nil {
		t.Fatalf("DeleteTag: %v", err)
	}

	var total int
	if err := db.QueryRow(`SELECT COUNT(*) FROM item_changes`).Scan(&total); err != nil {
		t.Fatalf("count item_changes: %v", err)
	}
	if total < 6 {
		t.Fatalf("item_changes rows = %d, want at least 6 after 6 mutations", total)
	}

	// A page of zero/negative size falls back to the default and still pages.
	if events, _, err := db.GetEvents(0, 0); err != nil || len(events) != total {
		t.Fatalf("GetEvents(0, 0) = %d events, err=%v; want all %d", len(events), err, total)
	}

	seen := map[int]bool{}
	var ids []int
	cursor, pages := 0, 0
	for {
		events, hasMore, err := db.GetEvents(cursor, 2)
		if err != nil {
			t.Fatalf("GetEvents(%d, 2): %v", cursor, err)
		}
		if len(events) == 0 {
			break
		}
		for _, e := range events {
			if seen[e.ID] {
				t.Fatalf("event %d returned twice", e.ID)
			}
			if e.ID <= cursor {
				t.Errorf("event %d not after cursor %d", e.ID, cursor)
			}
			if e.ItemID == "" || e.ItemType == 0 || e.CreatedTime == 0 {
				t.Errorf("event %d is missing fields: %+v", e.ID, e)
			}
			seen[e.ID] = true
			ids = append(ids, e.ID)
		}
		pages++
		if !hasMore {
			break
		}
		cursor = events[len(events)-1].ID
		if pages > 100 {
			t.Fatal("paging did not terminate")
		}
	}
	if !sort.IntsAreSorted(ids) {
		t.Errorf("event ids are not ascending: %v", ids)
	}
	if len(ids) != total {
		t.Errorf("paged %d events, want %d", len(ids), total)
	}

	// has_more reflects whether another row exists past the page.
	if events, hasMore, err := db.GetEvents(0, total-1); err != nil || len(events) != total-1 || !hasMore {
		t.Fatalf("GetEvents(0, total-1) = %d events hasMore=%v err=%v; want %d, true", len(events), hasMore, err, total-1)
	}
	if events, hasMore, err := db.GetEvents(0, total); err != nil || len(events) != total || hasMore {
		t.Fatalf("GetEvents(0, total) = %d events hasMore=%v err=%v; want %d, false", len(events), hasMore, err, total)
	}

	// A single event is retrievable by id; an unknown id is a nil miss.
	first := ids[0]
	one, err := db.GetEvent(first)
	if err != nil || one == nil || one.ID != first {
		t.Fatalf("GetEvent(%d) = %+v, %v; want the event", first, one, err)
	}
	if missing, err := db.GetEvent(1 << 30); err != nil || missing != nil {
		t.Fatalf("GetEvent(unknown) = %+v, %v; want nil, nil", missing, err)
	}

	// New mutations appear after the last cursor, so the feed is resumable.
	if err := db.CreateNote(&models.Note{ID: strings.Repeat("b", 32), Title: "after", Body: "two"}); err != nil {
		t.Fatalf("CreateNote (after): %v", err)
	}
	last := ids[len(ids)-1]
	newer, _, err := db.GetEvents(last, 10)
	if err != nil || len(newer) != 1 || newer[0].ID <= last {
		t.Fatalf("GetEvents(last cursor) = %d events, err=%v; want the one new event", len(newer), err)
	}
}
