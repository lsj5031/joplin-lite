package sync

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lsj5031/joplin-lite/internal/config"
	"github.com/lsj5031/joplin-lite/internal/models"
)

// fakeDropbox emulates the Dropbox API v2 endpoints the driver uses, plus the
// OAuth2 token endpoint.
type fakeDropbox struct {
	mu            sync.Mutex
	files         map[string][]byte
	times         map[string]time.Time
	bearer        string
	refreshCalls  int
	tokenForms    []url.Values
	listLimit     int // entries per list page (0 = one page)
	pageCallbacks int
}

func newFakeDropbox() *fakeDropbox {
	return &fakeDropbox{
		files: map[string][]byte{},
		times: map[string]time.Time{},
	}
}

func (f *fakeDropbox) put(path string, content []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[path] = append([]byte(nil), content...)
	f.times[path] = time.Now()
}

func (f *fakeDropbox) handler() http.Handler {
	mux := http.NewServeMux()
	auth := func(r *http.Request) {
		f.mu.Lock()
		f.bearer = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		f.mu.Unlock()
	}

	mux.HandleFunc("/2/files/upload", func(w http.ResponseWriter, r *http.Request) {
		auth(r)
		var arg struct {
			Path string `json:"path"`
		}
		json.Unmarshal([]byte(r.Header.Get("Dropbox-API-Arg")), &arg)
		body, _ := io.ReadAll(r.Body)
		f.put(arg.Path, body)
		w.Write([]byte(`{}`))
	})

	mux.HandleFunc("/2/files/download", func(w http.ResponseWriter, r *http.Request) {
		auth(r)
		var arg struct {
			Path string `json:"path"`
		}
		json.Unmarshal([]byte(r.Header.Get("Dropbox-API-Arg")), &arg)
		f.mu.Lock()
		content, ok := f.files[arg.Path]
		f.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte(`{"error_summary": "path/not_found/"}`))
			return
		}
		w.Write(content)
	})

	listPage := func(w http.ResponseWriter, path string, offset, limit int) {
		f.mu.Lock()
		var paths []string
		for p := range f.files {
			if path == "" || p == path || strings.HasPrefix(p, path+"/") {
				paths = append(paths, p)
			}
		}
		sort.Strings(paths)
		resp := listFolderResponse{}
		end := offset + limit
		if end > len(paths) {
			end = len(paths)
		}
		for _, p := range paths[offset:end] {
			resp.Entries = append(resp.Entries, dropboxEntry{
				Tag:            "file",
				PathDisplay:    p,
				ServerModified: f.times[p].UTC().Format(time.RFC3339Nano),
			})
		}
		resp.HasMore = end < len(paths)
		resp.Cursor = "off:" + strconv.Itoa(end)
		if limit <= 0 {
			resp.HasMore = false
		}
		f.mu.Unlock()
		json.NewEncoder(w).Encode(resp)
	}

	mux.HandleFunc("/2/files/list_folder", func(w http.ResponseWriter, r *http.Request) {
		auth(r)
		var arg struct {
			Path  string `json:"path"`
			Limit int    `json:"limit"`
		}
		json.Unmarshal(newBody(r), &arg)
		limit := f.listLimit
		if limit <= 0 {
			limit = 500
		}
		listPage(w, arg.Path, 0, limit)
	})

	mux.HandleFunc("/2/files/list_folder/continue", func(w http.ResponseWriter, r *http.Request) {
		auth(r)
		f.mu.Lock()
		f.pageCallbacks++
		f.mu.Unlock()
		var arg struct {
			Cursor string `json:"cursor"`
		}
		json.Unmarshal(newBody(r), &arg)
		offset, _ := strconv.Atoi(strings.TrimPrefix(arg.Cursor, "off:"))
		limit := f.listLimit
		if limit <= 0 {
			limit = 500
		}
		listPage(w, "", offset, limit)
	})

	mux.HandleFunc("/2/files/delete_v2", func(w http.ResponseWriter, r *http.Request) {
		auth(r)
		var arg struct {
			Path string `json:"path"`
		}
		json.Unmarshal(newBody(r), &arg)
		f.mu.Lock()
		_, ok := f.files[arg.Path]
		delete(f.files, arg.Path)
		delete(f.times, arg.Path)
		f.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte(`{"error_summary": "path/not_found/"}`))
			return
		}
		w.Write([]byte(`{}`))
	})

	mux.HandleFunc("/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		f.mu.Lock()
		f.refreshCalls++
		f.tokenForms = append(f.tokenForms, r.Form)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"new-token","token_type":"bearer","expires_in":3600,` +
			`"refresh_token":"rt-new","scope":"files.content.read","uid":"42","account_id":"dbid:42"}`))
	})

	return mux
}

// newBody reads a request body (the handlers may be hit twice via httptest re-use).
func newBody(r *http.Request) []byte {
	b, _ := io.ReadAll(r.Body)
	return b
}

func newTestDropbox(t *testing.T) (*DropboxBackend, *fakeDropbox, func()) {
	t.Helper()
	f := newFakeDropbox()
	srv := httptest.NewServer(f.handler())
	oauth := NewDropboxOAuth("appkey", "")
	oauth.TokenURL = srv.URL + "/oauth2/token"
	b := newDropboxBackend(oauth, &DropboxToken{AccessToken: "tok-1"}, "")
	b.apiBase = srv.URL
	b.contentBase = srv.URL
	return b, f, srv.Close
}

func TestDropboxBackend_ExistingTokenWithoutOAuthApp(t *testing.T) {
	for _, auth := range []string{"synthetic-token", `{"access_token":"synthetic-token"}`} {
		t.Run(auth, func(t *testing.T) {
			b, err := NewDropboxBackend(&config.Config{DataDir: t.TempDir(), DropboxAuth: auth})
			if err != nil {
				t.Fatal(err)
			}
			f := newFakeDropbox()
			f.put("/info.json", []byte(`{"version":3}`))
			srv := httptest.NewServer(f.handler())
			defer srv.Close()
			b.apiBase, b.contentBase = srv.URL, srv.URL
			got, err := b.Get(infoPath)
			if err != nil || string(got) != `{"version":3}` || f.bearer != "synthetic-token" || f.refreshCalls != 0 {
				t.Fatalf("existing token read failed: data=%q err=%v refreshes=%d", got, err, f.refreshCalls)
			}
		})
	}
}

func TestDropboxBackend_BlockedDeletesPreserveRemoteAndQueue(t *testing.T) {
	for _, itemType := range []int{models.TypeNote, models.TypeResource} {
		for _, blocked := range []bool{true, false} {
			t.Run(fmt.Sprintf("%d/blocked=%t", itemType, blocked), func(t *testing.T) {
				b, err := NewDropboxBackend(&config.Config{
					DataDir: t.TempDir(), DropboxAuth: "synthetic-token", DropboxBlockDeletes: blocked,
				})
				if err != nil {
					t.Fatal(err)
				}
				f := newFakeDropbox()
				srv := httptest.NewServer(f.handler())
				defer srv.Close()
				b.apiBase, b.contentBase = srv.URL, srv.URL
				const id = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
				f.put("/"+id+".md", []byte("keep metadata"))
				if itemType == models.TypeResource {
					f.put("/.resource/"+id, []byte("keep attachment"))
				}
				if err := b.Put("/api/items/root:/"+id+".md:/content", []byte("keep metadata")); err != nil {
					t.Fatalf("deletion guard prevented an upload: %v", err)
				}
				// The guard must still permit lock acquisition and cleanup.
				lock, err := b.AcquireLock()
				if err != nil {
					t.Fatal(err)
				}
				if err := b.ReleaseLock(lock); err != nil {
					t.Fatal(err)
				}
				db := testDB(t)
				if err := db.AddDeletedItem(id, itemType, 7); err != nil {
					t.Fatal(err)
				}
				err = PushChanges(b, db, nil)
				if (err != nil) != blocked {
					t.Fatalf("blocked=%t: push error %v", blocked, err)
				}
				pending, err := db.GetDeletedItems(7)
				if err != nil || (len(pending) == 1) != blocked {
					t.Fatalf("blocked=%t: pending %v, err %v", blocked, pending, err)
				}
				for path, content := range f.files {
					if !blocked || (path != "/"+id+".md" && path != "/.resource/"+id) || len(content) == 0 {
						t.Fatalf("unexpected remaining remote file: %q", path)
					}
					want := "keep metadata"
					if path == "/.resource/"+id {
						want = "keep attachment"
					}
					if string(content) != want {
						t.Fatalf("remote file changed: %q", path)
					}
				}
				wantFiles := 0
				if blocked {
					wantFiles = 1
					if itemType == models.TypeResource {
						wantFiles++
					}
				}
				if len(f.files) != wantFiles {
					t.Fatalf("remote files=%d, want %d", len(f.files), wantFiles)
				}
			})
		}
	}
}

func TestDropboxBackend_RoundTrip(t *testing.T) {
	b, f, done := newTestDropbox(t)
	defer done()

	const resID = "5b2c9e1d4a6f80b3c5d7e9f1a3b5c7d9"
	content := []byte("note content")
	blobName := ".resource/" + resID

	if err := b.Put("/api/items/root:/0d28d893565c4b2989301f2259164273.md:/content", content); err != nil {
		t.Fatalf("Put note: %v", err)
	}
	if err := b.Put(fmt.Sprintf("/api/items/root:/%s:/content", url.PathEscape(blobName)), []byte{0x00, 0xff, 0x42}); err != nil {
		t.Fatalf("Put blob: %v", err)
	}

	// The engine escapes names; the driver unescapes them to Dropbox paths.
	if _, ok := f.files["/.resource/"+resID]; !ok {
		t.Fatalf("blob stored under wrong path: %v", keysOf(f.files))
	}

	got, err := b.Get("/api/items/root:/0d28d893565c4b2989301f2259164273.md:/content")
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("Get note = %q (err=%v)", got, err)
	}
	got, err = b.Get(fmt.Sprintf("/api/items/root:/%s:/content", url.PathEscape(blobName)))
	if err != nil || !bytes.Equal(got, []byte{0x00, 0xff, 0x42}) {
		t.Fatalf("Get blob = %v (err=%v)", got, err)
	}

	// Missing content follows the "may not exist yet" convention.
	got, err = b.Get("/api/items/root:/ffffffffffffffffffffffffffffffff.md:/content")
	if err != nil || got != nil {
		t.Fatalf("Get missing = %v (err=%v), want nil, nil", got, err)
	}
	// Deleting something already gone is a no-op.
	if err := b.Delete("/api/items/root:/ffffffffffffffffffffffffffffffff.md:/content"); err != nil {
		t.Fatalf("Delete missing: %v", err)
	}
	if err := b.Delete("/api/items/root:/0d28d893565c4b2989301f2259164273.md:/content"); err != nil {
		t.Fatalf("Delete note: %v", err)
	}
	got, err = b.Get("/api/items/root:/0d28d893565c4b2989301f2259164273.md:/content")
	if err != nil || got != nil {
		t.Fatalf("note still present after delete: %q (err=%v)", got, err)
	}
}

func keysOf(m map[string][]byte) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestDropboxBackend_DeltaAndChildren(t *testing.T) {
	b, f, done := newTestDropbox(t)
	defer done()
	f.listLimit = 1 // force list_folder/continue pagination

	base := time.Now().Add(-time.Hour)
	f.put("/aaa.md", []byte("a"))
	f.put("/bbb.md", []byte("b"))
	f.put("/.resource/ccc", []byte("c"))
	f.put("/info.json", []byte("{}"))
	f.put("/locks/1_3_x.json", []byte("{}"))
	// Deterministic listing times: aaa < bbb < ccc.
	f.mu.Lock()
	f.times["/aaa.md"] = base
	f.times["/bbb.md"] = base.Add(time.Second)
	f.times["/.resource/ccc"] = base.Add(2 * time.Second)
	f.mu.Unlock()

	// Children: all items, no system paths.
	data, err := b.Get("/api/items/root:/:/children")
	if err != nil {
		t.Fatalf("children: %v", err)
	}
	var children ChildrenResponse
	if err := json.Unmarshal(data, &children); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, c := range children.Items {
		names[c.Name] = true
	}
	for _, want := range []string{"aaa.md", "bbb.md", ".resource/ccc"} {
		if !names[want] {
			t.Errorf("children missing %q (got %v)", want, names)
		}
	}
	if names["info.json"] || names["locks/1_3_x.json"] {
		t.Errorf("children leaked system paths: %v", names)
	}
	if f.pageCallbacks == 0 {
		t.Errorf("expected list_folder/continue pagination with listLimit=1")
	}

	// Delta: full pass, then a cursor-filtered pass that only re-delivers the
	// boundary (at-least-once, like LocalFolderBackend).
	data, err = b.Get("/api/items/root:/:/delta")
	if err != nil {
		t.Fatalf("delta: %v", err)
	}
	var delta DeltaResponse
	if err := json.Unmarshal(data, &delta); err != nil {
		t.Fatal(err)
	}
	if len(delta.Items) != 3 {
		t.Fatalf("delta items = %d, want 3: %+v", len(delta.Items), delta.Items)
	}
	cursor, _ := strconv.ParseInt(delta.Cursor, 10, 64)
	if cursor == 0 {
		t.Fatalf("delta cursor = %q, want a timestamp", delta.Cursor)
	}

	// aaa moves past the cursor; the cursor-boundary item is re-delivered
	// (at-least-once) and items below the cursor are not.
	f.mu.Lock()
	f.times["/aaa.md"] = base.Add(10 * time.Second)
	f.mu.Unlock()
	data, err = b.Get("/api/items/root:/:/delta?cursor=" + delta.Cursor)
	if err != nil {
		t.Fatalf("delta with cursor: %v", err)
	}
	delta = DeltaResponse{}
	if err := json.Unmarshal(data, &delta); err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, it := range delta.Items {
		got[it.ItemName] = true
	}
	if !got["aaa.md"] {
		t.Errorf("changed item not delivered: %v", got)
	}
	if got["bbb.md"] {
		t.Errorf("item below the cursor re-delivered: %v", got)
	}
}

func TestDropboxEngineInventoryIsCompleteAndFreshEachPass(t *testing.T) {
	b, f, done := newTestDropbox(t)
	defer done()
	f.listLimit = 1
	var rootListings atomic.Int64
	var failPage atomic.Bool
	handler := f.handler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/2/files/list_folder/continue" && failPage.Swap(false) {
			http.Error(w, "injected incomplete inventory", http.StatusServiceUnavailable)
			return
		}
		if r.URL.Path == "/2/files/list_folder" {
			body := newBody(r)
			r.Body = io.NopCloser(bytes.NewReader(body))
			var arg struct{ Path string }
			_ = json.Unmarshal(body, &arg)
			if arg.Path == "" {
				rootListings.Add(1)
			}
		}
		handler.ServeHTTP(w, r)
	}))
	defer srv.Close()
	b.apiBase, b.contentBase = srv.URL, srv.URL
	db := testDB(t)
	engine := NewEngine(NewTracedBackend(b, "dropbox"), db, "")
	first := &models.Note{ID: strings.Repeat("a", 32), Title: "First", Body: "retain the complete inventory", CreatedTime: 1000, UpdatedTime: 1000}
	second := &models.Note{ID: strings.Repeat("b", 32), Title: "Second", Body: "fresh next pass", CreatedTime: 2000, UpdatedTime: 2000}
	f.put("/"+first.ID+".md", []byte(models.SerializeNote(first)))
	if err := engine.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, err := db.GetNote(first.ID); err != nil || got == nil || got.Body != first.Body {
		t.Fatalf("first pull = %+v, %v", got, err)
	}
	if got := rootListings.Load(); got != 1 {
		t.Errorf("full inventories in first pass = %d, want 1", got)
	}
	f.mu.Lock()
	delete(f.files, "/"+first.ID+".md")
	delete(f.times, "/"+first.ID+".md")
	f.mu.Unlock()
	f.put("/"+second.ID+".md", []byte(models.SerializeNote(second)))
	if err := engine.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, err := db.GetNote(first.ID); err != nil || got != nil {
		t.Fatalf("remotely deleted note remains: %+v, %v", got, err)
	}
	if got, err := db.GetNote(second.ID); err != nil || got == nil || got.Body != second.Body {
		t.Fatalf("second pull = %+v, %v", got, err)
	}
	if got := rootListings.Load(); got != 2 {
		t.Errorf("full inventories in two passes = %d, want 2", got)
	}
	// An incomplete listing must neither prune existing notes nor persist its
	// partial inventory/cursor into the retry.
	beforeCursor, err := db.GetSyncCursor()
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	delete(f.files, "/"+second.ID+".md")
	delete(f.times, "/"+second.ID+".md")
	f.mu.Unlock()
	failPage.Store(true)
	if err := engine.Sync(context.Background()); err == nil {
		t.Fatal("partial listing succeeded")
	}
	if got, err := db.GetNote(second.ID); err != nil || got == nil || got.Body != second.Body {
		t.Fatalf("incomplete inventory lost the existing note: %+v, %v", got, err)
	}
	if cursor, err := db.GetSyncCursor(); err != nil || cursor != beforeCursor {
		t.Fatalf("failed listing advanced cursor: %q, %v", cursor, err)
	}
	f.put("/"+first.ID+".md", []byte(models.SerializeNote(first)))
	if err := engine.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, err := db.GetNote(first.ID); err != nil || got == nil || got.Body != first.Body {
		t.Fatalf("retry missed fresh inventory: %+v, %v", got, err)
	}
	if got, err := db.GetNote(second.ID); err != nil || got != nil {
		t.Fatalf("retry missed remote deletion: %+v, %v", got, err)
	}
}

func TestDropboxEngineCancellationStopsSlowRequest(t *testing.T) {
	b, f, done := newTestDropbox(t)
	defer done()
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var held atomic.Bool
	handler := f.handler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/2/files/list_folder" {
			body := newBody(r)
			r.Body = io.NopCloser(bytes.NewReader(body))
			var arg struct{ Path string }
			_ = json.Unmarshal(body, &arg)
			if arg.Path == "" && held.CompareAndSwap(false, true) {
				entered <- struct{}{}
				select {
				case <-r.Context().Done():
					return
				case <-release:
				}
			}
		}
		handler.ServeHTTP(w, r)
	}))
	defer func() {
		close(release)
		srv.Close()
	}()
	b.apiBase, b.contentBase = srv.URL, srv.URL
	db := testDB(t)
	engine := NewEngine(NewTracedBackend(b, "dropbox"), db, "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- engine.Sync(ctx) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("sync did not reach remote listing")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled sync = %v", err)
		}
	case <-time.After(time.Second):
		t.Error("sync did not cancel its in-flight Dropbox request")
		// Let the baseline drain so its DB/server can close cleanly.
		srv.CloseClientConnections()
		<-result
	}
	note := &models.Note{ID: strings.Repeat("c", 32), Title: "Recovered", Body: "retry after cancellation", CreatedTime: 3000, UpdatedTime: 3000}
	f.put("/"+note.ID+".md", []byte(models.SerializeNote(note)))
	if err := engine.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, err := db.GetNote(note.ID); err != nil || got == nil || got.Body != note.Body {
		t.Fatalf("retry after cancellation = %+v, %v", got, err)
	}
}

func TestDropboxBackend_Locks(t *testing.T) {
	b, f, done := newTestDropbox(t)
	defer done()

	// A foreign EXCLUSIVE lock refuses acquisition.
	f.put("/locks/2_1_desktop.json", []byte(`{"type":2,"clientType":1,"clientId":"desktop"}`))
	if _, err := b.AcquireLock(); err == nil {
		t.Fatal("AcquireLock succeeded while an exclusive lock is held")
	} else if !strings.Contains(err.Error(), "exclusive") {
		t.Fatalf("AcquireLock error = %v, want the exclusive-lock refusal", err)
	}
	if _, ok := f.files["/locks/1_3_joplin-lite.json"]; ok {
		t.Fatal("our lock file exists after a refused acquire")
	}
	f.mu.Lock()
	delete(f.files, "/locks/2_1_desktop.json")
	f.mu.Unlock()

	// Acquire writes locks/1_3_joplin-lite.json on Dropbox.
	lock, err := b.AcquireLock()
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	if _, ok := f.files["/locks/1_3_joplin-lite.json"]; !ok {
		t.Fatalf("lock file not uploaded: %v", keysOf(f.files))
	}

	// The production tracing wrapper must preserve the refresh capability.
	old := time.Now().Add(-time.Minute)
	f.mu.Lock()
	f.times["/locks/1_3_joplin-lite.json"] = old
	f.mu.Unlock()
	if err := NewTracedBackend(b, "dropbox").(LockRefresher).RefreshLock(lock); err != nil {
		t.Fatalf("RefreshLock: %v", err)
	}
	f.mu.Lock()
	refreshed := f.times["/locks/1_3_joplin-lite.json"]
	f.mu.Unlock()
	if !refreshed.After(old) {
		t.Fatal("traced refresh did not update the remote lock")
	}
	if err := b.ReleaseLock(lock); err != nil {
		t.Fatalf("ReleaseLock: %v", err)
	}
	if _, ok := f.files["/locks/1_3_joplin-lite.json"]; ok {
		t.Fatal("lock file still present after release")
	}
}

func TestDropboxBackend_TokenRefresh(t *testing.T) {
	b, f, done := newTestDropbox(t)
	defer done()

	b.token = &DropboxToken{
		AccessToken:  "expired-token",
		RefreshToken: "rt-1",
		ExpiresAt:    time.Now().Add(-time.Hour).UnixMilli(),
	}
	if err := b.Authenticate(); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	f.mu.Lock()
	forms := append([]url.Values(nil), f.tokenForms...)
	f.mu.Unlock()
	if len(forms) != 1 || forms[0].Get("grant_type") != "refresh_token" || forms[0].Get("refresh_token") != "rt-1" {
		t.Fatalf("token endpoint calls = %v, want one refresh_token grant", forms)
	}
	if b.token.AccessToken != "new-token" || b.token.RefreshToken != "rt-new" {
		t.Fatalf("token after refresh = %+v", b.token)
	}

	// API calls now carry the fresh token; a 401 would refresh and retry.
	if err := b.Put("/api/items/root:/aaa.md:/content", []byte("x")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	f.mu.Lock()
	bearer := f.bearer
	f.mu.Unlock()
	if bearer != "new-token" {
		t.Fatalf("API bearer = %q, want new-token", bearer)
	}
}

func TestDropboxOAuth_LoopbackFlow(t *testing.T) {
	f := newFakeDropbox()
	srv := httptest.NewServer(f.handler())
	defer srv.Close()

	// Reserve a loopback port for the redirect.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	oauth := NewDropboxOAuth("appkey", "")
	oauth.TokenURL = srv.URL + "/oauth2/token"
	oauth.Redirect = "http://" + addr

	var out syncBuffer
	done := make(chan struct{})
	var flowTok *DropboxToken
	var flowErr error
	go func() {
		defer close(done)
		flowTok, flowErr = RunDropboxOAuthFlow(context.Background(), oauth, &out)
	}()

	// Wait for the authorization URL and complete the redirect with its state.
	var authURL string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, line := range strings.Split(out.String(), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "http") {
				authURL = line
			}
		}
		if authURL != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if authURL == "" {
		t.Fatalf("no authorization URL printed: %q", out.String())
	}
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("token_access_type") != "offline" {
		t.Fatalf("auth URL missing PKCE/offline params: %s", authURL)
	}
	if !strings.Contains(q.Get("scope"), "files.content.write") {
		t.Fatalf("auth URL scope = %q", q.Get("scope"))
	}

	resp, err := http.Get("http://" + addr + "/?code=the-code&state=" + url.QueryEscape(q.Get("state")))
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	resp.Body.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("flow did not finish")
	}
	if flowErr != nil {
		t.Fatalf("flow: %v", flowErr)
	}
	if flowTok.AccessToken != "new-token" || flowTok.RefreshToken != "rt-new" || flowTok.AccountID != "dbid:42" {
		t.Fatalf("flow token = %+v", flowTok)
	}
	if flowTok.ExpiresAt < time.Now().Add(30*time.Minute).UnixMilli() {
		t.Fatalf("expires_at not set from expires_in: %+v", flowTok)
	}

	// PKCE linkage: the code_verifier sent to the token endpoint must hash to
	// the code_challenge in the authorization URL.
	f.mu.Lock()
	forms := append([]url.Values(nil), f.tokenForms...)
	f.mu.Unlock()
	if len(forms) != 1 {
		t.Fatalf("token endpoint calls = %d, want 1", len(forms))
	}
	form := forms[0]
	if form.Get("code") != "the-code" || form.Get("grant_type") != "authorization_code" {
		t.Fatalf("token form = %v", form)
	}
	sum := sha256.Sum256([]byte(form.Get("code_verifier")))
	if base64.RawURLEncoding.EncodeToString(sum[:]) != q.Get("code_challenge") {
		t.Fatal("code_verifier does not match the code_challenge")
	}
}

// syncBuffer is a goroutine-safe buffer for capturing the flow output.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
