package assistant

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/internal/store"
)

func noteTestManager(t *testing.T) (*Manager, *models.Folder, *models.Folder) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	m, err := NewManager(db, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5e9)
		defer cancel()
		_ = m.Close(ctx)
	})
	a := &models.Folder{Title: "Selected"}
	b := &models.Folder{Title: "Elsewhere"}
	if err := db.CreateFolder(a); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateFolder(b); err != nil {
		t.Fatal(err)
	}
	return m, a, b
}

func testRun(scope Scope) *Run {
	return &Run{
		ID:       uuid(),
		Scope:    scope,
		limits:   DefaultLimits(),
		evidence: newEvidenceRegistry(),
		budget:   newRunBudget(DefaultLimits()),
		search:   map[string][]store.AssistantSearchHit{},
	}
}

func TestReadNoteClampsUTF8AndRegistersEvidence(t *testing.T) {
	m, folder, _ := noteTestManager(t)
	note := &models.Note{ParentID: folder.ID, Title: "Unicode", Body: "héllo 世界 camera repair"}
	if err := m.vault.CreateNote(note); err != nil {
		t.Fatal(err)
	}
	run := testRun(Scope{Kind: "note", NoteID: note.ID})
	passage, err := m.readNote(context.Background(), run, ReadInput{NoteID: note.ID, StartByte: 1, MaxBytes: 6})
	if err != nil {
		t.Fatal(err)
	}
	if !utf8.ValidString(passage.Text) {
		t.Fatalf("read split a UTF-8 rune: %q", passage.Text)
	}
	if passage.StartByte != 1 || passage.EndByte <= passage.StartByte {
		t.Fatalf("unexpected bounds: %+v", passage)
	}
	if passage.EvidenceID != "E1" {
		t.Fatalf("first read should mint E1, got %q", passage.EvidenceID)
	}
	if !strings.Contains(passage.Text, "é") {
		t.Fatalf("passage lost its leading rune: %q", passage.Text)
	}
	citations := run.evidence.cite("Answer [E1] and an unknown [E9].")
	if len(citations) != 1 || citations[0].Token != "E1" || citations[0].NoteID != note.ID || !citations[0].Verified {
		t.Fatalf("citation resolution = %+v", citations)
	}
	_ = m.Close(context.Background())
}

func TestReadNoteRejectsOtherScopeAndChangedVersion(t *testing.T) {
	m, selected, elsewhere := noteTestManager(t)
	note := &models.Note{ParentID: selected.ID, Title: "Camera", Body: "camera repair"}
	if err := m.vault.CreateNote(note); err != nil {
		t.Fatal(err)
	}
	other := &models.Note{ParentID: elsewhere.ID, Title: "Other", Body: "camera repair"}
	if err := m.vault.CreateNote(other); err != nil {
		t.Fatal(err)
	}
	run := testRun(Scope{Kind: "notebook", NotebookID: selected.ID})
	_, err := m.readNote(context.Background(), run, ReadInput{NoteID: other.ID})
	var tool *ToolError
	if !errors.As(err, &tool) || tool.Code != "out_of_scope" {
		t.Fatalf("other notebook note should be out_of_scope, got %v", err)
	}
	_, err = m.readNote(context.Background(), run, ReadInput{NoteID: note.ID, ExpectedVersion: "stale"})
	if !errors.As(err, &tool) || tool.Code != "note_changed" {
		t.Fatalf("stale version should be note_changed, got %v", err)
	}
}

func TestReadNoteBudgetIsEnforced(t *testing.T) {
	m, folder, _ := noteTestManager(t)
	note := &models.Note{ParentID: folder.ID, Title: "Long", Body: strings.Repeat("camera ", 2000)}
	if err := m.vault.CreateNote(note); err != nil {
		t.Fatal(err)
	}
	run := testRun(Scope{Kind: "note", NoteID: note.ID})
	run.budget = newRunBudget(Limits{NoteToolCalls: 1, EvidenceBytes: 10, ReadBytes: 12 << 10})
	_, err := m.readNote(context.Background(), run, ReadInput{NoteID: note.ID, MaxBytes: 1024})
	var tool *ToolError
	if !errors.As(err, &tool) || tool.Code != "budget_exceeded" {
		t.Fatalf("evidence budget should stop the read, got %v", err)
	}
	_ = m.Close(context.Background())
}
