package store

import (
	"fmt"
	"strings"
	"testing"

	"github.com/lsj5031/joplin-lite/internal/models"
)

// benchDB builds a 1000-note library with 2 KB bodies: enough for the list
// paths to show their per-row cost without making the benchmark slow.
func benchDB(b *testing.B) *DB {
	b.Helper()
	db, err := Open(b.TempDir())
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	b.Cleanup(func() { db.Close() })
	body := strings.Repeat("lorem ipsum dolor sit amet ", 80)
	for i := 0; i < 1000; i++ {
		n := &models.Note{
			ID:             fmt.Sprintf("bench-note-%04d", i),
			Title:          fmt.Sprintf("note %d", i),
			Body:           body + fmt.Sprintf("\n%d", i),
			CreatedTime:    int64(i),
			UpdatedTime:    int64(1000 + i),
			MarkupLanguage: 1,
		}
		if err := db.UpsertNote(n); err != nil {
			b.Fatalf("UpsertNote: %v", err)
		}
	}
	return db
}

func BenchmarkListNotesFull(b *testing.B) {
	db := benchDB(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		notes, _, err := db.ListNotes("", "", false, 50, 0)
		if err != nil || len(notes) != 50 {
			b.Fatalf("ListNotes: %d notes, %v", len(notes), err)
		}
	}
}

func BenchmarkListNoteSummaries(b *testing.B) {
	db := benchDB(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		notes, _, _, err := db.ListNoteSummaries(NoteQuery{}, 50, "")
		if err != nil || len(notes) != 50 {
			b.Fatalf("ListNoteSummaries: %d notes, %v", len(notes), err)
		}
	}
}

func BenchmarkListNoteSummariesDeepCursor(b *testing.B) {
	db := benchDB(b)
	// Walk to a deep page once, then measure seeking there by cursor.
	cursor := ""
	for i := 0; i < 15; i++ {
		_, _, next, err := db.ListNoteSummaries(NoteQuery{}, 50, cursor)
		if err != nil {
			b.Fatal(err)
		}
		cursor = next
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		notes, _, _, err := db.ListNoteSummaries(NoteQuery{}, 50, cursor)
		if err != nil || len(notes) == 0 {
			b.Fatalf("ListNoteSummaries: %d notes, %v", len(notes), err)
		}
	}
}
