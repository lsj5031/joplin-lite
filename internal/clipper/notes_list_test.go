package clipper

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/lsj5031/joplin-lite/internal/models"
)

// TestNotesListExcludesConflicts pins the Data API's include_conflicts
// semantics on every note list route: GET /notes, GET /folders/{id}/notes,
// GET /tags/{id}/notes and GET /search. Conflict copies are excluded by
// default (they live in Joplin's virtual Conflicts notebook) and included
// with include_conflicts=1, in both full and summary modes.
func TestNotesListExcludesConflicts(t *testing.T) {
	s, ts := newTestServer(t, &testPrompt{accept: true, called: make(chan string, 1)})

	nb := &models.Folder{Title: "Inbox"}
	if err := s.db.CreateFolder(nb); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	visible := &models.Note{ParentID: nb.ID, Title: "visible note", Body: "body"}
	if err := s.db.CreateNote(visible); err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	conflict := &models.Note{ParentID: nb.ID, Title: "conflicted copy", Body: "body", IsConflict: 1}
	if err := s.db.CreateNote(conflict); err != nil {
		t.Fatalf("CreateNote (conflict): %v", err)
	}
	tag := &models.Tag{Title: "inbox-tag"}
	if err := s.db.CreateTag(tag); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	for _, id := range []string{visible.ID, conflict.ID} {
		if err := s.db.AddNoteTag(id, tag.ID); err != nil {
			t.Fatalf("AddNoteTag(%s): %v", id, err)
		}
	}

	page := struct {
		Items []map[string]interface{} `json:"items"`
	}{}
	get := func(path string) []map[string]interface{} {
		t.Helper()
		status, body := doReq(t, http.MethodGet, ts.URL+path, nil)
		if status != http.StatusOK {
			t.Fatalf("GET %s = %d: %s", path, status, body)
		}
		if err := json.Unmarshal(body, &page); err != nil {
			t.Fatalf("GET %s body = %q (err=%v)", path, body, err)
		}
		return page.Items
	}

	paths := []string{
		"/notes?token=" + testAPIToken,
		"/folders/" + nb.ID + "/notes?token=" + testAPIToken,
		"/tags/" + tag.ID + "/notes?token=" + testAPIToken,
		"/search?query=body&token=" + testAPIToken,
	}
	for _, path := range paths {
		if items := get(path); len(items) != 1 {
			t.Fatalf("GET %s = %d items (%v), want 1 (conflict excluded)", path, len(items), items)
		}
		if items := get(path + "&include_conflicts=1"); len(items) != 2 {
			t.Fatalf("GET %s&include_conflicts=1 = %d items, want 2", path, len(items))
		}
		// Summary mode is what the workspace uses; it must agree.
		if items := get(path + "&summary=1"); len(items) != 1 {
			t.Fatalf("GET %s&summary=1 = %d items (%v), want 1", path, len(items), items)
		}
		if items := get(path + "&summary=1&include_conflicts=1"); len(items) != 2 {
			t.Fatalf("GET %s&summary=1&include_conflicts=1 = %d items, want 2", path, len(items))
		}
	}
}
