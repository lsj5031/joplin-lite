package clipper

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/lsj5031/joplin-lite/internal/models"
)

// TestSummaryListsShipNoBodies pins the list contract the webview uses:
// summary=1 returns rows without body (with an excerpt instead) and pages them
// by cursor, while the plain Data API keeps returning full notes.
func TestSummaryListsShipNoBodies(t *testing.T) {
	s, ts := newTestServer(t, &testPrompt{accept: true, called: make(chan string, 1)})

	nb := &models.Folder{Title: "Inbox"}
	if err := s.db.CreateFolder(nb); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	var ids []string
	for i, body := range []string{"# zebraword heading\nrest of the body", "second zebraword body"} {
		n := &models.Note{
			ID:          models.NewID(),
			ParentID:    nb.ID,
			Title:       "note",
			Body:        body,
			CreatedTime: 100,
			UpdatedTime: int64(1000 + i), // deterministic newest-first order
		}
		if err := s.db.UpsertNote(n); err != nil {
			t.Fatalf("UpsertNote: %v", err)
		}
		ids = append(ids, n.ID)
	}

	get := func(path string) (items []map[string]interface{}, next string) {
		t.Helper()
		status, raw := doReq(t, http.MethodGet, ts.URL+path+"&token="+testAPIToken, nil)
		if status != http.StatusOK {
			t.Fatalf("GET %s = %d: %s", path, status, raw)
		}
		var page struct {
			Items      []map[string]interface{} `json:"items"`
			NextCursor string                   `json:"next_cursor"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		return page.Items, page.NextCursor
	}

	for _, path := range []string{
		"/notes?summary=1&limit=10",
		"/folders/" + nb.ID + "/notes?summary=1&limit=10",
		"/search?type=note&summary=1&limit=10&query=" + url.QueryEscape("zebraword"),
	} {
		items, _ := get(path)
		if len(items) != 2 {
			t.Fatalf("GET %s = %d items, want 2", path, len(items))
		}
		for _, it := range items {
			if _, ok := it["body"]; ok {
				t.Fatalf("GET %s leaked body: %v", path, it)
			}
			if it["excerpt"] == "" {
				t.Fatalf("GET %s has empty excerpt: %v", path, it)
			}
		}
	}

	// First row is the newest note; its excerpt is the first line with the
	// markdown heading marker stripped.
	items, next := get("/notes?summary=1&limit=1")
	if len(items) != 1 || items[0]["id"] != ids[1] {
		t.Fatalf("first summary page = %v, want %s", items, ids[1])
	}
	if items[0]["excerpt"] != "second zebraword body" {
		t.Fatalf("excerpt = %v", items[0]["excerpt"])
	}
	if next == "" {
		t.Fatal("limit=1 on two notes returned no next_cursor")
	}

	items, _ = get("/notes?summary=1&limit=1&cursor=" + url.QueryEscape(next))
	if len(items) != 1 || items[0]["id"] != ids[0] {
		t.Fatalf("second summary page = %v, want %s", items, ids[0])
	}

	// The unmodified Data API still carries bodies.
	status, raw := doReq(t, http.MethodGet, ts.URL+"/notes?limit=10&token="+testAPIToken, nil)
	if status != http.StatusOK {
		t.Fatalf("GET /notes = %d: %s", status, raw)
	}
	var full struct {
		Items []map[string]interface{} `json:"items"`
	}
	if err := json.Unmarshal(raw, &full); err != nil {
		t.Fatalf("GET /notes: %v", err)
	}
	if body, ok := full.Items[0]["body"].(string); !ok || body == "" {
		t.Fatalf("plain /notes lost the body: %v", full.Items[0])
	}
}
