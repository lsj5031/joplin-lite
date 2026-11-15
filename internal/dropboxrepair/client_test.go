package dropboxrepair

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReferenceContentHashes(t *testing.T) {
	// Existing independent Dropbox-reference vectors from sync/dropbox_cache_test.go.
	for _, tc := range []struct {
		size int
		hash string
	}{
		{0, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		{1, "bf5d3affb73efd2ec6c36ad3112dd933efed63c4e1cbffcfa88e2759c144f2d8"},
		{4194303, "49c0a79929eacf8ccbe77f99ff043ca55f97000c68bccefc644791170969a3b2"},
		{4194304, "907a506cf5e706bda5c7a29b43c9c65d8344bd2fa2f22339b359c214812af5a1"},
		{4194305, "5f858b62ccd88447586305aec6fd53c96747cfebf527cbba129a6dfed47d9624"},
		{8388608, "941162ca0d3fcd4e4b5bcaf179e3a156ae2b66b39e87993a7e626e61938ab2c9"},
	} {
		if got := ContentHash(bytes.Repeat([]byte("a"), tc.size)); got != tc.hash {
			t.Fatalf("size %d: %s", tc.size, got)
		}
	}
}

func TestInventoryPaginationAndDownloadIntegrity(t *testing.T) {
	pages := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/2/files/list_folder":
			pages++
			json.NewEncoder(w).Encode(map[string]any{"entries": []Metadata{{Tag: "folder", Path: "/.resource"}}, "cursor": "page1", "has_more": true})
		case "/2/files/list_folder/continue":
			pages++
			var arg map[string]string
			json.NewDecoder(r.Body).Decode(&arg)
			if arg["cursor"] != "page1" {
				t.Error("wrong continuation cursor")
			}
			json.NewEncoder(w).Encode(map[string]any{"entries": []Metadata{{Tag: "folder", Path: "/locks"}}, "cursor": "page2", "has_more": false})
		case "/2/files/download":
			meta, _ := json.Marshal(Metadata{Path: "/info.json", Rev: "r1", Hash: ContentHash([]byte("original")), Size: 7})
			w.Header().Set("Dropbox-API-Result", string(meta))
			w.Write([]byte("damaged"))
		default:
			t.Error("unexpected endpoint")
			w.WriteHeader(400)
		}
	}))
	defer server.Close()
	c := NewClient("synthetic")
	c.API, c.Content = server.URL, server.URL
	inv, cursor, err := c.Inventory(context.Background())
	if err != nil || len(inv) != 2 || cursor != "page2" || pages != 2 {
		t.Fatalf("pagination: %v", err)
	}
	if _, _, err := c.Download(context.Background(), "/info.json"); err == nil {
		t.Fatal("corrupt download accepted")
	}
}
