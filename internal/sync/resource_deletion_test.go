package sync

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/internal/store"
)

type resourceDeletionBackend struct {
	*LocalFolderBackend
	failPath string
	failErr  error
	calls    []string
}

func (backend *resourceDeletionBackend) Delete(path string) error {
	backend.calls = append(backend.calls, path)
	if path == backend.failPath {
		return backend.failErr
	}
	return backend.LocalFolderBackend.Delete(path)
}

func TestResourceDeletionRetries(t *testing.T) {
	const id = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	blobName := ".resource/" + id
	metadataName := id + ".md"
	blobPath := "/api/items/root:/" + url.PathEscape(blobName) + ":/content"
	metadataPath := "/api/items/root:/" + metadataName + ":/content"
	for _, test := range []struct {
		name       string
		failPath   string
		firstCalls []string
	}{
		{"blob failure", blobPath, []string{blobPath}},
		{"metadata failure", metadataPath, []string{blobPath, metadataPath}},
	} {
		t.Run(test.name, func(t *testing.T) {
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			db, err := store.Open(t.TempDir())
			must(err)
			t.Cleanup(func() { _ = db.Close() })
			targetDir := t.TempDir()
			failure := errors.New("injected remote delete failure")
			backend := &resourceDeletionBackend{
				LocalFolderBackend: NewLocalFolderBackend(targetDir),
				failPath:           test.failPath,
				failErr:            failure,
			}
			must(SelectTarget(db, backend))
			must(backend.Put(blobPath, []byte("blob")))
			must(backend.Put(metadataPath, []byte("metadata")))
			must(db.UpsertSyncItemAt(id, models.TypeResource, backend.SyncTarget(), 1))
			must(db.SaveItemRaw(id, []byte("metadata"), 1))
			must(db.DeleteResource(id))

			checkPending := func(want bool) {
				t.Helper()
				queued, err := db.PushableDeletedItems(backend.SyncTarget(), deleteScope(backend))
				must(err)
				if want {
					if len(queued) != 1 || queued[0].ItemID != id {
						t.Fatalf("want one queued deletion for %s, got %v", id, queued)
					}
				} else if len(queued) != 0 {
					t.Fatalf("deletion queue has %d rows, want empty", len(queued))
				}
				_, _, rawExists, err := db.GetItemRaw(id)
				must(err)
				if rawExists != want {
					t.Fatalf("raw metadata present=%v, want %v", rawExists, want)
				}
			}
			checkPending(true)

			err = PushChanges(backend, db, nil)
			var partial *PartialPushError
			if !errors.As(err, &partial) || !errors.Is(err, failure) {
				t.Errorf("PushChanges = %v, want partial push with cause %v", err, failure)
			}
			if !slices.Equal(backend.calls, test.firstCalls) {
				t.Errorf("first delete calls = %v, want %v", backend.calls, test.firstCalls)
			}
			checkPending(true)

			backend.failPath = ""
			backend.calls = nil
			must(PushChanges(backend, db, nil))
			if !slices.Equal(backend.calls, []string{blobPath, metadataPath}) {
				t.Errorf("retry delete calls = %v, want blob then metadata", backend.calls)
			}
			checkPending(false)
			for _, name := range []string{blobName, metadataName} {
				path := filepath.Join(targetDir, filepath.FromSlash(name))
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("remote path %s still present or inaccessible: %v", name, err)
				}
			}
		})
	}
}

func TestResourceDeletionServerNotFound(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
	}{
		{"already absent", http.StatusNotFound},
		{"permission denied", http.StatusForbidden},
		{"server failure", http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, req *http.Request) {
				if req.Method != http.MethodDelete {
					t.Errorf("request method = %s, want DELETE", req.Method)
				}
				response.WriteHeader(test.status)
			}))
			defer server.Close()
			backend := &Client{baseURL: server.URL, httpClient: server.Client()}
			err := backend.Delete("/api/items/root:/.resource%2Faaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa:/content")
			if wantError := test.status != http.StatusNotFound; (err != nil) != wantError {
				t.Errorf("Delete returned %v for HTTP %d, want error=%v", err, test.status, wantError)
			}
		})
	}
}
