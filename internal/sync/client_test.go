package sync

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A fresh sync target has no info.json, and Joplin Server answers that read
// with 404. Get reports a typed error so sync-info can treat it as missing
// while item/blob fetches still refuse to overwrite local data with nothing.
func TestClientGet_MissingItemReturnsNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/items/root:/info.json:/content":
			http.NotFound(w, r)
		case "/api/items/root:/broken:/content":
			http.Error(w, "boom", http.StatusInternalServerError)
		default:
			w.Write([]byte("payload"))
		}
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, httpClient: srv.Client()}

	data, err := c.Get("/api/items/root:/info.json:/content")
	if data != nil || !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(404) = (%q, %v), want nil and ErrNotFound", data, err)
	}

	data, err = c.Get("/api/items/root:/note.md:/content")
	if err != nil || string(data) != "payload" {
		t.Fatalf("Get(200) = (%q, %v), want payload", data, err)
	}

	if _, err := c.Get("/api/items/root:/broken:/content"); err == nil {
		t.Fatal("Get(500) returned no error")
	}
}

// The sync-info path is the one caller allowed to read a 404 as "nothing
// there": it must still write the first info.json instead of failing.
func TestPushSyncInfo_MissingRemoteIsCreated(t *testing.T) {
	backend := &missingInfoBackend{}
	if err := PushSyncInfo(backend, infoTestDB(t)); err != nil {
		t.Fatalf("PushSyncInfo on a fresh target: %v", err)
	}
	if len(backend.puts) != 1 {
		t.Fatalf("info.json writes = %d, want 1", len(backend.puts))
	}
}

type missingInfoBackend struct {
	infoBackend
}

func (b *missingInfoBackend) Get(path string) ([]byte, error) {
	return nil, ErrNotFound
}
