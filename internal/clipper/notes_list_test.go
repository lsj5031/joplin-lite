package clipper

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/lsj5031/joplin-lite/internal/models"
)

// TestNotesListExcludesConflicts pins the Data API's include_conflicts
// semantics on GET /notes and GET /folders/{id}/notes: conflict copies are
// excluded by default (they live in Joplin's virtual Conflicts notebook) and
// included with include_conflicts=1.
func TestNotesListExcludesConflicts(t *testing.T) {
	s, ts := newTestServer(t, &testPrompt{accept: true, called: make(chan string, 1)})

	nb := &models.Folder{Title: "Inbox"}
	if err := s.db.CreateFolder(nb); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if err := s.db.CreateNote(&models.Note{ParentID: nb.ID, Title: "visible note", Body: "body"}); err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	if err := s.db.CreateNote(&models.Note{ParentID: nb.ID, Title: "conflicted copy", Body: "body", IsConflict: 1}); err != nil {
		t.Fatalf("CreateNote (conflict): %v", err)
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

	for _, path := range []string{
		"/notes?token=" + testAPIToken,
		"/folders/" + nb.ID + "/notes?token=" + testAPIToken,
	} {
		if items := get(path); len(items) != 1 {
			t.Fatalf("GET %s = %d items (%v), want 1 (conflict excluded)", path, len(items), items)
		}
		if items := get(path + "&include_conflicts=1"); len(items) != 2 {
			t.Fatalf("GET %s&include_conflicts=1 = %d items, want 2", path, len(items))
		}
	}
}
