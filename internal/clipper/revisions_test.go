package clipper

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/lsj5031/joplin-lite/internal/models"
)

const testRevisionRaw = `id: 3c4d5e6f708192a3b4c5d6e7f8091a2b
parent_id: 0d28d893565c4b2989301f2259164273
item_id: 0d28d893565c4b2989301f2259164273
item_type: 1
item_updated_time: 1772000000000
title_diff: [{"diffs":[[1,"Old title"]],"start1":0,"start2":0,"length1":0,"length2":9}]
body_diff: [{"diffs":[[1,"line one"]],"start1":0,"start2":0,"length1":0,"length2":8}]
metadata_diff: {"new":{},"deleted":[]}
created_time: 2026-03-02T10:00:00.000Z
updated_time: 2026-03-02T10:00:00.000Z
user_created_time: 2026-03-02T10:00:00.000Z
user_updated_time: 2026-03-02T10:00:00.000Z
encryption_cipher_text: 
encryption_applied: 0
master_key_id: 
user_data: 
is_locked: 0
type_: 13`

const testRevisionID = "3c4d5e6f708192a3b4c5d6e7f8091a2b"

func seedRevision(t *testing.T, s *Server) {
	t.Helper()
	rev := models.DeserializeRevision(testRevisionRaw)
	if rev == nil {
		t.Fatal("cannot parse revision fixture")
	}
	if err := s.db.UpsertRevision(rev); err != nil {
		t.Fatal(err)
	}
	if err := s.db.SaveItemRaw(rev.ID, []byte(testRevisionRaw), rev.UpdatedTime); err != nil {
		t.Fatal(err)
	}
	// Mark it synced so a local delete queues the remote one.
	if err := s.db.UpsertSyncItem(rev.ID, models.TypeRevision, 9); err != nil {
		t.Fatal(err)
	}
}

// TestClipperRevisionsAPI serves real note history: entities carry the diff
// fields from the raw item with API-typed values (ms numbers, not ISO).
func TestClipperRevisionsAPI(t *testing.T) {
	s, ts := newTestServer(t, nil)
	seedRevision(t, s)

	status, body := doReq(t, http.MethodGet, ts.URL+"/revisions?token="+testAPIToken, nil)
	if status != http.StatusOK {
		t.Fatalf("GET /revisions = %d %s", status, body)
	}
	var page struct {
		Items   []map[string]interface{} `json:"items"`
		HasMore bool                     `json:"has_more"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("GET /revisions items = %d, want 1", len(page.Items))
	}
	item := page.Items[0]
	if item["id"] != testRevisionID {
		t.Errorf("id = %v", item["id"])
	}
	// Diff fields survive verbatim from the raw item.
	if td, ok := item["title_diff"].(string); !ok || td == "" {
		t.Errorf("title_diff = %v (%T), want a string", item["title_diff"], item["title_diff"])
	}
	if bd, ok := item["body_diff"].(string); !ok || bd == "" {
		t.Errorf("body_diff = %v (%T), want a string", item["body_diff"], item["body_diff"])
	}
	// Typed values: times are unix-ms numbers in the API, not the ISO/mixed
	// strings of the file format.
	for _, key := range []string{"item_updated_time", "created_time", "updated_time", "user_created_time", "item_type"} {
		if _, ok := item[key].(float64); !ok {
			t.Errorf("%s = %v (%T), want a JSON number", key, item[key], item[key])
		}
	}
	if got := item["item_updated_time"].(float64); got != 1772000000000 {
		t.Errorf("item_updated_time = %v, want 1772000000000", got)
	}

	// GET /revisions/{id} returns the same entity.
	status, body = doReq(t, http.MethodGet, ts.URL+"/revisions/"+testRevisionID+"?token="+testAPIToken, nil)
	if status != http.StatusOK {
		t.Fatalf("GET /revisions/{id} = %d %s", status, body)
	}
	var entity map[string]interface{}
	if err := json.Unmarshal(body, &entity); err != nil {
		t.Fatal(err)
	}
	if entity["id"] != testRevisionID {
		t.Fatalf("GET /revisions/{id} = %v", entity)
	}

	// Unknown id -> 404.
	status, _ = doReq(t, http.MethodGet, ts.URL+"/revisions/ffffffffffffffffffffffffffffffff?token="+testAPIToken, nil)
	if status != http.StatusNotFound {
		t.Fatalf("GET unknown revision = %d, want 404", status)
	}

	// DELETE removes it and queues the remote deletion.
	status, body = doReq(t, http.MethodDelete, ts.URL+"/revisions/"+testRevisionID+"?token="+testAPIToken, nil)
	if status != http.StatusOK {
		t.Fatalf("DELETE /revisions/{id} = %d %s", status, body)
	}
	status, _ = doReq(t, http.MethodGet, ts.URL+"/revisions/"+testRevisionID+"?token="+testAPIToken, nil)
	if status != http.StatusNotFound {
		t.Fatalf("GET after delete = %d, want 404", status)
	}
	deleted, err := s.db.GetDeletedItems(9)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 1 || deleted[0].ItemID != testRevisionID {
		t.Fatalf("deleted_items = %+v, want the revision queued for remote delete", deleted)
	}
}

// TestClipperRevisionsFieldsFilter: ?fields= trims the entity like the rest
// of the Data API.
func TestClipperRevisionsFieldsFilter(t *testing.T) {
	s, ts := newTestServer(t, nil)
	seedRevision(t, s)

	_, body := doReq(t, http.MethodGet, ts.URL+"/revisions?token="+testAPIToken+"&fields=id,item_id", nil)
	var page struct {
		Items []map[string]interface{} `json:"items"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("items = %d", len(page.Items))
	}
	for k := range page.Items[0] {
		if k != "id" && k != "item_id" {
			t.Errorf("fields filter leaked %q", k)
		}
	}
}

// TestClipperRevisionRewriteRefused: history is immutable through the API.
// PUT on an existing revision is rejected and the stored bytes are untouched;
// PUT on an unknown id is a plain 404 (the route exists, it is not 405).
func TestClipperRevisionRewriteRefused(t *testing.T) {
	s, ts := newTestServer(t, nil)
	seedRevision(t, s)

	before, err := s.db.GetRevision(testRevisionID)
	if err != nil || before == nil {
		t.Fatalf("seed revision not found: %v", err)
	}

	status, body := doReq(t, http.MethodPut, ts.URL+"/revisions/"+testRevisionID+"?token="+testAPIToken, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("PUT /revisions/{id} = %d %s, want 400", status, body)
	}

	after, err := s.db.GetRevision(testRevisionID)
	if err != nil || after == nil {
		t.Fatalf("revision vanished after refused PUT: %v", err)
	}
	if after.EncryptionCipherText != before.EncryptionCipherText ||
		after.ParentID != before.ParentID ||
		after.ItemUpdatedTime != before.ItemUpdatedTime {
		t.Fatalf("refused PUT still mutated the revision: %+v -> %+v", before, after)
	}

	status, _ = doReq(t, http.MethodPut, ts.URL+"/revisions/ffffffffffffffffffffffffffffffff?token="+testAPIToken, nil)
	if status != http.StatusNotFound {
		t.Fatalf("PUT unknown revision = %d, want 404", status)
	}
}
