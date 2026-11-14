package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/internal/store"
)

const cjkBenchUnit = "这是一段很长的中文笔记，包含标点和链接。"

func cjkBenchBody(runes int) string {
	var b strings.Builder
	b.Grow(runes * 3)
	for b.Len() < runes*3 {
		b.WriteString(cjkBenchUnit)
	}
	return string([]rune(b.String())[:runes])
}

// cjkBenchParagraphs writes the same text as cjkBenchBody, but as many short
// paragraphs: the shape of a real long note, which has no giant paragraph to
// split.
func cjkBenchParagraphs(runes int) string {
	var b strings.Builder
	b.Grow(runes * 3)
	for b.Len() < runes*3 {
		b.WriteString(cjkBenchUnit)
		b.WriteString("\n\n")
	}
	return string([]rune(b.String())[:runes])
}

func seedCJKBenchVault(b *testing.B) *store.DB {
	b.Helper()
	db, err := store.Open(filepath.Join(b.TempDir(), "vault"))
	if err != nil {
		b.Fatalf("store.Open: %v", err)
	}
	b.Cleanup(func() { _ = db.Close() })

	base := cjkBenchBody(3200)
	for i := 0; i < 1000; i++ {
		n := &models.Note{
			ID:             fmt.Sprintf("cjk-base-%04d", i),
			Title:          fmt.Sprintf("长笔记 %04d：关于周末散步、阅读和写作的详细记录", i),
			Body:           base,
			CreatedTime:    int64(i),
			UpdatedTime:    int64(1000 + i),
			MarkupLanguage: 1,
		}
		if err := db.UpsertNote(n); err != nil {
			b.Fatalf("UpsertNote: %v", err)
		}
	}

	for _, size := range []struct {
		name  string
		runes int
	}{
		{"3k", 3000},
		{"12k", 12000},
		{"50k", 50000},
	} {
		body := cjkBenchBody(size.runes)
		many := cjkBenchParagraphs(size.runes)
		for i := 0; i < 2; i++ {
			n := &models.Note{
				ID:             fmt.Sprintf("cjk-%s-%d", size.name, i),
				Title:          fmt.Sprintf("长笔记 %s：中文渲染测试 %d", size.name, i),
				Body:           body,
				CreatedTime:    int64(5000 + size.runes + i),
				UpdatedTime:    int64(5000 + size.runes + i),
				MarkupLanguage: 1,
			}
			if err := db.UpsertNote(n); err != nil {
				b.Fatalf("UpsertNote: %v", err)
			}
			n = &models.Note{
				ID:             fmt.Sprintf("cjk-many-%s-%d", size.name, i),
				Title:          fmt.Sprintf("长笔记 %s：多段落中文渲染测试 %d", size.name, i),
				Body:           many,
				CreatedTime:    int64(9000 + size.runes + i),
				UpdatedTime:    int64(9000 + size.runes + i),
				MarkupLanguage: 1,
			}
			if err := db.UpsertNote(n); err != nil {
				b.Fatalf("UpsertNote: %v", err)
			}
		}
	}
	return db
}

// BenchmarkNativeLongCJK exercises the real native view over a vault where
// every note is a long CJK note. Each case measures a full app frame, including
// the list reload or note open, rather than an isolated widget or store call.
func BenchmarkNativeLongCJK(b *testing.B) {
	registerFonts()
	db := seedCJKBenchVault(b)
	app := newNativeApp(db)
	tt := ui.NewTester(app.view, 1280, 800)
	tt.Frame()

	b.Run("ReloadList", func(b *testing.B) {
		app.query = ""
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			app.reloadNotes()
			tt.Frame()
		}
	})

	b.Run("SearchCJK", func(b *testing.B) {
		app.query = "长笔记"
		app.reloadNotes()
		if len(app.notes) == 0 {
			b.Fatal("CJK title search returned no notes")
		}
		tt.Frame()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			app.reloadNotes()
			tt.Frame()
		}
		b.StopTimer()
		app.query = ""
	})

	for _, tc := range []struct {
		name string
		ids  []string
	}{
		{"Show3K", []string{"cjk-3k-0", "cjk-3k-1"}},
		{"Show12K", []string{"cjk-12k-0", "cjk-12k-1"}},
		{"Show50K", []string{"cjk-50k-0", "cjk-50k-1"}},
		{"ShowMany50K", []string{"cjk-many-50k-0", "cjk-many-50k-1"}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			app.query = ""
			app.reloadNotes()
			// Start from the second note so the first measured show is a
			// real load rather than the early return for the open note.
			app.showNote(tc.ids[1])
			tt.Frame()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				app.showNote(tc.ids[i%2])
				tt.Frame()
			}
		})
	}
}
