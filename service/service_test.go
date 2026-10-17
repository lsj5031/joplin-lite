package service

import (
	"testing"

	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/internal/store"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return New(db)
}

func TestNotebookNoteTagViews(t *testing.T) {
	svc := newTestService(t)

	// Notebooks.
	nb := &models.Folder{Title: "Projects"}
	if err := svc.CreateNotebook(nb); err != nil {
		t.Fatalf("CreateNotebook: %v", err)
	}
	if nb.ID == "" {
		t.Fatal("CreateNotebook did not assign an ID")
	}
	tree, err := svc.NotebookTree()
	if err != nil || len(tree) != 1 || tree[0].Title != "Projects" {
		t.Fatalf("NotebookTree = %v, %v", tree, err)
	}

	// Notes.
	n := &models.Note{ParentID: nb.ID, Title: "hello mygo", Body: "seiippai"}
	if err := svc.CreateNote(n); err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	page, err := svc.ListNotes(NoteFilter{FolderID: nb.ID, Page: Page{Limit: 10}})
	if err != nil || len(page.Notes) != 1 || page.Notes[0].Title != "hello mygo" {
		t.Fatalf("ListNotes = %d notes, %v", len(page.Notes), err)
	}

	// Conflict copies stay out of folder/all-notes views and live in the
	// Conflicts view (Joplin's virtual Conflicts notebook).
	conflict := &models.Note{ParentID: nb.ID, Title: "conflicted copy", Body: "gush", IsConflict: 1}
	if err := svc.CreateNote(conflict); err != nil {
		t.Fatalf("CreateNote (conflict): %v", err)
	}
	page, err = svc.ListNotes(NoteFilter{FolderID: nb.ID, Page: Page{Limit: 10}})
	if err != nil || len(page.Notes) != 1 {
		t.Fatalf("folder view must exclude conflict notes: got %d, %v", len(page.Notes), err)
	}
	all, err := svc.ListNotes(NoteFilter{Page: Page{Limit: 10}})
	if err != nil || len(all.Notes) != 1 {
		t.Fatalf("all-notes view must exclude conflict notes: got %d, %v", len(all.Notes), err)
	}
	cp, err := svc.ConflictNotes(Page{Limit: 10})
	if err != nil || len(cp.Notes) != 1 || cp.Notes[0].Title != "conflicted copy" {
		t.Fatalf("ConflictNotes = %d notes, %v", len(cp.Notes), err)
	}

	// Search.
	sp, err := svc.Search("seiippai", Page{Limit: 10})
	if err != nil || len(sp.Notes) != 1 || sp.Notes[0].ID != n.ID {
		t.Fatalf("Search = %d notes, %v", len(sp.Notes), err)
	}

	// Tags.
	tg := &models.Tag{Title: "music"}
	if err := svc.CreateTag(tg); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	if err := svc.AddTag(n.ID, tg.ID); err != nil {
		t.Fatalf("AddTag: %v", err)
	}
	nt, err := svc.NoteTags(n.ID)
	if err != nil || len(nt) != 1 || nt[0].Title != "music" {
		t.Fatalf("NoteTags = %v, %v", nt, err)
	}
	byTag, err := svc.NotesByTag(tg.ID, Page{Limit: 10})
	if err != nil || len(byTag.Notes) != 1 || byTag.Notes[0].ID != n.ID {
		t.Fatalf("NotesByTag = %d notes, %v", len(byTag.Notes), err)
	}
	if err := svc.RemoveTag(n.ID, tg.ID); err != nil {
		t.Fatalf("RemoveTag: %v", err)
	}
	if nt, err = svc.NoteTags(n.ID); err != nil || len(nt) != 0 {
		t.Fatalf("NoteTags after RemoveTag = %v, %v", nt, err)
	}

	// Settings.
	if err := svc.SetSetting("sync.target", "8"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if v, err := svc.Setting("sync.target"); err != nil || v != "8" {
		t.Fatalf("Setting = %q, %v", v, err)
	}
	if v, err := svc.Setting("nope"); err != nil || v != "" {
		t.Fatalf("missing Setting = %q, %v; want empty", v, err)
	}

	// Deletion.
	if err := svc.DeleteNote(n.ID); err != nil {
		t.Fatalf("DeleteNote: %v", err)
	}
	if got, err := svc.GetNote(n.ID); err != nil || got != nil {
		t.Fatalf("GetNote after delete = %v, %v", got, err)
	}
}
