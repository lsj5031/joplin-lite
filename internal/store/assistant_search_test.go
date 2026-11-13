package store

import (
	"context"
	"testing"

	"github.com/lsj5031/joplin-lite/internal/models"
)

func assistantTestVault(t *testing.T) (*DB, *models.Folder, *models.Folder) {
	t.Helper()
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	a := &models.Folder{Title: "Camera"}
	b := &models.Folder{Title: "Unrelated"}
	if err := db.CreateFolder(a); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateFolder(b); err != nil {
		t.Fatal(err)
	}
	notes := []*models.Note{
		{ParentID: a.ID, Title: "Camera repair", Body: "The camera repair was approved. Follow up with the shop about the lens."},
		{ParentID: a.ID, Title: "Lens notes", Body: "The camera lens is scratched; ask the shop for a replacement."},
		{ParentID: b.ID, Title: "Camera shopping", Body: "A new camera is not needed."},
		{ParentID: a.ID, Title: "Conflict", Body: "camera camera camera", IsConflict: 1},
		{ParentID: a.ID, Title: "Locked", Body: "camera encrypted", EncryptionApplied: 1},
		{ParentID: a.ID, Title: "Deleted", Body: "camera deleted", DeletedTime: 1},
	}
	for _, note := range notes {
		if err := db.CreateNote(note); err != nil {
			t.Fatal(err)
		}
	}
	return db, a, b
}

func TestAssistantSearchRespectsNotebookScope(t *testing.T) {
	db, a, _ := assistantTestVault(t)
	ctx := context.Background()
	hits, err := db.AssistantSearch(ctx, AssistantSearchQuery{
		Query: "camera", Scope: AssistantScope{Kind: "notebook", NotebookID: a.ID}, Limit: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("got %d hits, want 2 readable non-conflict notes in the notebook: %+v", len(hits), hits)
	}
	for _, hit := range hits {
		if hit.NotebookID != a.ID {
			t.Fatalf("hit outside notebook: %+v", hit)
		}
		if hit.Snippet == "" {
			t.Fatalf("search must return a bounded snippet, got %+v", hit)
		}
	}
	if hits[0].Title != "Camera repair" {
		t.Fatalf("title weighting/tie-break returned %q first", hits[0].Title)
	}
}

func TestAssistantSearchNoteScopeAndExclusions(t *testing.T) {
	db, a, b := assistantTestVault(t)
	ctx := context.Background()
	page, _, _, err := db.ListNoteSummaries(NoteQuery{FolderID: a.ID}, 50, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page) == 0 {
		t.Fatal("no notes seeded")
	}
	var cameraRepair, lens string
	for _, note := range page {
		switch note.Title {
		case "Camera repair":
			cameraRepair = note.ID
		case "Lens notes":
			lens = note.ID
		}
	}
	hits, err := db.AssistantSearch(ctx, AssistantSearchQuery{
		Query: "camera", Scope: AssistantScope{Kind: "note", NoteID: cameraRepair}, Limit: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].NoteID != cameraRepair {
		t.Fatalf("note scope leaked: %+v", hits)
	}
	ok, err := db.AssistantNoteInScope(ctx, AssistantScope{Kind: "note", NoteID: cameraRepair}, cameraRepair)
	if err != nil || !ok {
		t.Fatalf("note scope should include its note: ok=%v err=%v", ok, err)
	}
	ok, err = db.AssistantNoteInScope(ctx, AssistantScope{Kind: "note", NoteID: lens}, cameraRepair)
	if err != nil || ok {
		t.Fatalf("another note must be out of note scope: ok=%v err=%v", ok, err)
	}
	ok, err = db.AssistantNoteInScope(ctx, AssistantScope{Kind: "notebook", NotebookID: b.ID}, cameraRepair)
	if err != nil || ok {
		t.Fatalf("other notebook must be out of scope: ok=%v err=%v", ok, err)
	}
}

func TestAssistantSearchDescendantsAndTag(t *testing.T) {
	db, a, _ := assistantTestVault(t)
	child := &models.Folder{Title: "Repair detail", ParentID: a.ID}
	if err := db.CreateFolder(child); err != nil {
		t.Fatal(err)
	}
	note := &models.Note{ParentID: child.ID, Title: "Repair receipt", Body: "camera repair receipt for the lens"}
	if err := db.CreateNote(note); err != nil {
		t.Fatal(err)
	}
	tag := &models.Tag{Title: "receipts"}
	if err := db.CreateTag(tag); err != nil {
		t.Fatal(err)
	}
	if err := db.AddNoteTag(note.ID, tag.ID); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	exact, err := db.AssistantSearch(ctx, AssistantSearchQuery{
		Query: "receipt", Scope: AssistantScope{Kind: "notebook", NotebookID: a.ID}, Limit: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(exact) != 0 {
		t.Fatalf("an exact notebook selection must exclude descendants: %+v", exact)
	}
	deep, err := db.AssistantSearch(ctx, AssistantSearchQuery{
		Query: "receipt", Scope: AssistantScope{Kind: "notebook", NotebookID: a.ID, IncludeDescendants: true}, Limit: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(deep) != 1 || deep[0].NoteID != note.ID {
		t.Fatalf("descendant search missed the child notebook note: %+v", deep)
	}
	filtered, err := db.AssistantSearch(ctx, AssistantSearchQuery{
		Query: "receipt", TagID: tag.ID, Scope: AssistantScope{Kind: "vault"}, Limit: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[0].NoteID != note.ID {
		t.Fatalf("tag filter returned %+v", filtered)
	}
}
