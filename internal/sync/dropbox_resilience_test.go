package sync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lsj5031/joplin-lite/internal/config"
	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/internal/store"
)

func mockDropbox(t *testing.T, f *fakeDropbox, intercept func(http.ResponseWriter, *http.Request) bool) *DropboxBackend {
	t.Helper()
	handler := f.handler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !intercept(w, r) {
			handler.ServeHTTP(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	oauth := NewDropboxOAuth("mock-app", "")
	oauth.TokenURL = srv.URL + "/oauth2/token"
	b := newDropboxBackend(oauth, &DropboxToken{AccessToken: "mock-access"}, "")
	b.apiBase, b.contentBase = srv.URL, srv.URL
	return b
}

func mockProfile(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func contentRequestPath(r *http.Request) string {
	var arg struct{ Path string }
	_ = json.Unmarshal([]byte(r.Header.Get("Dropbox-API-Arg")), &arg)
	return arg.Path
}

func TestDropboxTransientDeltaDownload(t *testing.T) {
	f := newFakeDropbox()
	note := &models.Note{ID: strings.Repeat("c", 32), Title: "Recovered", UpdatedTime: 1000}
	path := "/" + note.ID + ".md"
	f.put(path, []byte(models.SerializeNote(note)))
	var attempts atomic.Int32
	b := mockDropbox(t, f, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path == "/2/files/download" && contentRequestPath(r) == path && attempts.Add(1) == 1 {
			http.Error(w, "temporary", http.StatusInternalServerError)
			return true
		}
		return false
	})
	db := mockProfile(t)
	if err := NewEngine(NewTracedBackend(b, "dropbox"), db, "").Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, err := db.GetNote(note.ID); err != nil || got == nil || got.Title != note.Title || attempts.Load() != 2 {
		t.Fatalf("download did not recover: note=%+v attempts=%d err=%v", got, attempts.Load(), err)
	}
}

func TestDropboxPartialPushReportsNoteTag(t *testing.T) {
	f := newFakeDropbox()
	noteID, tagID, linkID := strings.Repeat("a", 32), strings.Repeat("b", 32), strings.Repeat("c", 32)
	var fail atomic.Bool
	fail.Store(true)
	b := mockDropbox(t, f, func(w http.ResponseWriter, r *http.Request) bool {
		if fail.Load() && r.URL.Path == "/2/files/upload" && contentRequestPath(r) == "/"+linkID+".md" {
			http.Error(w, "injected failure", http.StatusForbidden)
			return true
		}
		return false
	})
	db := mockProfile(t)
	if err := db.CreateNote(&models.Note{ID: noteID, Title: "Local"}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateTag(&models.Tag{ID: tagID, Title: "Local tag"}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertNoteTag(&models.NoteTag{ID: linkID, NoteID: noteID, TagID: tagID, UpdatedTime: 1000}); err != nil {
		t.Fatal(err)
	}
	engine := NewEngine(b, db, "")
	err := engine.Sync(context.Background())
	if err == nil || !strings.Contains(err.Error(), "note_tag") || !strings.Contains(err.Error(), linkID) {
		t.Fatalf("pass did not report the failed note-tag: %v", err)
	}
	checkSynced := func(id string, want int) {
		t.Helper()
		var got int
		if err := db.QueryRow("SELECT count(*) FROM sync_items WHERE sync_target=7 AND item_id=?", id).Scan(&got); err != nil || got != want {
			t.Fatalf("sync state for %s: got %d, want %d (%v)", id, got, want, err)
		}
	}
	checkSynced(noteID, 1)
	checkSynced(tagID, 1)
	checkSynced(linkID, 0)
	fail.Store(false)
	if err := engine.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	checkSynced(linkID, 1)
}

func TestDropboxEngineBulkPushMakesMonotonicProgress(t *testing.T) {
	f := newFakeDropbox()
	const n = 500
	var recovered atomic.Bool
	var calls [n]atomic.Int32
	b := mockDropbox(t, f, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/2/files/upload" {
			return false
		}
		path := contentRequestPath(r)
		if len(path) != 36 {
			return false
		}
		var i int
		if _, err := fmt.Sscanf(path, "/%x.md", &i); err != nil || i < 0 || i >= n {
			return false
		}
		calls[i].Add(1)
		if i%100 == 0 && !recovered.Load() {
			w.WriteHeader(http.StatusForbidden)
			return true
		}
		return false
	})
	db := mockProfile(t)
	for i := range n {
		if err := db.CreateNote(&models.Note{ID: fmt.Sprintf("%032x", i), Title: "Synthetic offline note"}); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	engine := NewEngine(NewTracedBackend(b, "dropbox"), db, "")
	countSynced := func(want int) {
		t.Helper()
		var got int
		if err := db.QueryRow("SELECT count(*) FROM sync_items WHERE sync_target=7 AND item_type=?", models.TypeNote).Scan(&got); err != nil || got != want {
			t.Fatalf("synced %d, want %d: %v", got, want, err)
		}
	}
	var partial *PartialPushError
	if err := engine.Sync(ctx); !errors.As(err, &partial) || !strings.Contains(err.Error(), "5 failed") {
		t.Fatalf("bulk pass did not report five failures: %v", err)
	}
	countSynced(495)
	recovered.Store(true)
	if err := engine.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	countSynced(500)
	for i := range n {
		want := int32(1)
		if i%100 == 0 {
			want = 2
		}
		if got := calls[i].Load(); got != want {
			t.Fatalf("note %d attempted %d times, want %d", i, got, want)
		}
	}
}

func TestDropboxRefreshesRejectedUnexpiredToken(t *testing.T) {
	f := newFakeDropbox()
	b := mockDropbox(t, f, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path == "/2/files/download" && r.Header.Get("Authorization") == "Bearer mock-access" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error_summary":"expired_access_token/"}`)
			return true
		}
		return false
	})
	b.token.Store(&DropboxToken{AccessToken: "mock-access", RefreshToken: "mock-refresh"})
	if _, err := b.Get("/api/items/root:/info.json:/content"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refreshCalls != 1 || f.bearer != "new-token" {
		t.Fatalf("refresh was not used: calls=%d", f.refreshCalls)
	}
}

func TestDropboxLockListingRetriesRequestTimeout(t *testing.T) {
	f := newFakeDropbox()
	var stalled atomic.Bool
	b := mockDropbox(t, f, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/2/files/list_folder" {
			return false
		}
		body := newBody(r)
		r.Body = io.NopCloser(bytes.NewReader(body))
		if !bytes.Contains(body, []byte(`"/locks"`)) || stalled.Swap(true) {
			return false
		}
		<-r.Context().Done()
		return true
	})
	b.lockTimeout = 60 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := NewEngine(b, mockProfile(t), "").Sync(ctx); err != nil || !stalled.Load() {
		t.Fatalf("lock-list stall should recover within the caller deadline: %v", err)
	}
}

func TestDropboxEngineLockLease(t *testing.T) {
	for _, scenario := range []string{"rate limit recovers", "expires", "removed", "exclusive", "removed between list and update"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFakeDropbox()
			note := &models.Note{ID: strings.Repeat("9", 32), Title: "Local work"}
			path := "/" + note.ID + ".md"
			var lists, uploads, writes atomic.Int32
			pushing, refreshed, canceled := make(chan struct{}), make(chan struct{}), make(chan struct{})
			original := f.handler()
			var retryFrom time.Time
			var lockPath string
			var expiry time.Time
			var mu sync.Mutex
			b := mockDropbox(t, f, func(w http.ResponseWriter, r *http.Request) bool {
				switch r.URL.Path {
				case "/2/files/list_folder":
					body := newBody(r)
					r.Body = io.NopCloser(bytes.NewReader(body))
					if !bytes.Contains(body, []byte(`"/locks"`)) || lists.Add(1) != 3 {
						return false
					}
					// The tested refresh response is sent during a real data
					// request, not merely during an idle waiting loop.
					select {
					case <-pushing:
					case <-r.Context().Done():
						return true
					}
					mu.Lock()
					switch scenario {
					case "expires":
						retryFrom = time.Now()
						w.Header().Set("Retry-After", "10")
						w.WriteHeader(http.StatusTooManyRequests)
						_, _ = io.WriteString(w, `{"error":{".tag":"too_many_write_operations","retry_after":1}}`)
						mu.Unlock()
						return true
					case "removed":
						f.mu.Lock()
						delete(f.files, lockPath)
						f.mu.Unlock()
					case "exclusive":
						f.put("/locks/2_1_other.json", []byte("{}"))
					}
					mu.Unlock()
				case "/2/files/upload":
					remote := contentRequestPath(r)
					if strings.HasPrefix(remote, "/locks/") {
						count := uploads.Add(1)
						if count == 1 {
							original.ServeHTTP(w, r)
							mu.Lock()
							lockPath = remote
							f.mu.Lock()
							expiry = f.times[remote].Add(550 * time.Millisecond)
							f.mu.Unlock()
							mu.Unlock()
							return true
						}
						if scenario == "removed between list and update" {
							f.mu.Lock()
							delete(f.files, remote)
							f.mu.Unlock()
						}
						if scenario == "rate limit recovers" && count == 2 {
							mu.Lock()
							retryFrom = time.Now()
							mu.Unlock()
							w.WriteHeader(http.StatusTooManyRequests)
							_, _ = io.WriteString(w, `{"error":{".tag":"too_many_write_operations","retry_after":1}}`)
							return true
						}
						if scenario == "rate limit recovers" && count == 3 {
							mu.Lock()
							elapsed := time.Since(retryFrom)
							mu.Unlock()
							if elapsed < time.Second {
								t.Errorf("lock refresh ignored JSON Retry-After: %s", elapsed)
							}
							original.ServeHTTP(w, r)
							close(refreshed)
							return true
						}
					}
					if remote == path && writes.Add(1) == 1 {
						body := newBody(r)
						r.Body = io.NopCloser(bytes.NewReader(body))
						close(pushing)
						select {
						case <-refreshed:
							return false
						case <-r.Context().Done():
							close(canceled)
							return true
						}
					}
				}
				return false
			})
			b.lockInterval, b.lockTTL = 100*time.Millisecond, 550*time.Millisecond
			if scenario == "rate limit recovers" {
				b.lockTTL = 2 * time.Second
			}
			db := mockProfile(t)
			if err := db.CreateNote(note); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			err := NewEngine(NewTracedBackend(b, "dropbox"), db, "").Sync(ctx)
			if scenario == "rate limit recovers" {
				if err != nil || uploads.Load() < 3 {
					t.Fatalf("transient lock refresh canceled unrelated push: %v", err)
				}
			} else {
				want := ErrLockExpired
				if scenario == "exclusive" {
					want = ErrExclusiveLock
				}
				if !errors.Is(err, want) {
					t.Fatalf("sync returned %v; want %v", err, want)
				}
				select {
				case <-canceled:
				case <-time.After(time.Second):
					t.Fatal("the in-flight write was not canceled")
				}
				f.mu.Lock()
				_, wrote := f.files[path]
				f.mu.Unlock()
				if wrote {
					t.Fatal("wrote user content after losing the lock")
				}
				if scenario == "expires" {
					mu.Lock()
					untilExpiry := time.Until(expiry)
					mu.Unlock()
					if untilExpiry > 0 || uploads.Load() != 1 {
						t.Fatalf("did not wait for actual expiry or retried an expired write: time left=%s uploads=%d", untilExpiry, uploads.Load())
					}
				}
			}
		})
	}
}

func TestDropboxAmbiguousMutationKeepsPeerRevision(t *testing.T) {
	for _, verb := range []string{"add", "update", "delete"} {
		for _, peer := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/peer=%t", verb, peer), func(t *testing.T) {
				f := newFakeDropbox()
				const path, apiPath = "/item.md", "/api/items/root:/item.md:/content"
				if verb != "add" {
					f.put(path, []byte("original"))
				}
				var attempts atomic.Int32
				original := f.handler()
				b := mockDropbox(t, f, func(w http.ResponseWriter, r *http.Request) bool {
					if r.URL.Path != "/2/files/upload" && r.URL.Path != "/2/files/delete_v2" {
						return false
					}
					if attempts.Add(1) != 1 {
						return false
					}
					result := httptest.NewRecorder()
					original.ServeHTTP(result, r)
					if result.Code != http.StatusOK {
						t.Errorf("mock first mutation did not commit: %d", result.Code)
					}
					if peer {
						f.put(path, []byte("peer replacement"))
					}
					// Lose the successful response, once at the transport
					// and elsewhere as a server error after commit.
					if verb == "delete" {
						conn, _, err := w.(http.Hijacker).Hijack()
						if err != nil {
							t.Error(err)
						} else {
							_ = conn.Close()
						}
					} else {
						w.WriteHeader(http.StatusBadGateway)
					}
					return true
				})
				var err error
				if verb == "delete" {
					err = b.Delete(apiPath)
				} else {
					err = b.Put(apiPath, []byte("desired"))
				}
				if (err != nil) != peer || attempts.Load() != 2 {
					t.Fatalf("guarded replay: error=%v attempts=%d", err, attempts.Load())
				}
				f.mu.Lock()
				defer f.mu.Unlock()
				got := string(f.files[path])
				want := "desired"
				if verb == "delete" {
					want = ""
				}
				if peer {
					want = "peer replacement"
				}
				if got != want || len(f.files) > 1 {
					t.Fatalf("replay changed target state: %q, count=%d; want %q", got, len(f.files), want)
				}
			})
		}
	}
}

func TestDropboxRetryAfterPacesConcurrentWrites(t *testing.T) {
	f := newFakeDropbox()
	var first atomic.Bool
	var mu sync.Mutex
	var rejected time.Time
	var writeTimes []time.Time
	b := mockDropbox(t, f, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/2/files/upload" {
			return false
		}
		mu.Lock()
		defer mu.Unlock()
		if !first.Swap(true) {
			rejected = time.Now()
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return true
		}
		writeTimes = append(writeTimes, time.Now())
		return false
	})
	if err := b.Put("/api/items/root:/first.md:/content", []byte("a")); err != nil {
		t.Fatal(err)
	}
	if err := parallelEach(4, 4, func(i int) error {
		return b.Put(fmt.Sprintf("/api/items/root:/%d.md:/content", i), []byte("b"))
	}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(writeTimes) != 5 || writeTimes[0].Sub(rejected) < time.Second {
		t.Fatalf("missing retry or ignored header: %v", writeTimes)
	}
	for i := 1; i < len(writeTimes); i++ {
		// Allow loopback scheduling slack around the promised 100ms spacing.
		if gap := writeTimes[i].Sub(writeTimes[i-1]); gap < 75*time.Millisecond {
			t.Fatalf("writes were not paced after rate limiting: %s", gap)
		}
	}
}

func TestDropboxDeadlineStopsThrottledMutation(t *testing.T) {
	var writes atomic.Int32
	b := mockDropbox(t, newFakeDropbox(), func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/2/files/upload" {
			return false
		}
		writes.Add(1)
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		return true
	})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	defer b.BeginSync(ctx, nil)()
	err := b.Put("/api/items/root:/item.md:/content", []byte("desired"))
	if !errors.Is(err, context.DeadlineExceeded) || writes.Load() != 1 {
		t.Fatalf("mutation retried past caller deadline: %v, attempts=%d", err, writes.Load())
	}
}

func TestDropboxLongRetryHintIsReportedWithoutWaiting(t *testing.T) {
	var calls atomic.Int32
	b := mockDropbox(t, newFakeDropbox(), func(w http.ResponseWriter, r *http.Request) bool {
		calls.Add(1)
		// HTTP-date is rounded down to a second. The JSON delay is larger
		// and must win, including when this was a server (not 429) error.
		w.Header().Set("Retry-After", time.Now().Add(30*time.Second).UTC().Format(http.TimeFormat))
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"retry_after":60}`)
		return true
	})
	now := time.Now()
	_, err := b.Get("/api/items/root:/info.json:/content")
	at, ok := DropboxRetryAt(err)
	if !ok || calls.Load() != 1 || at.Before(now.Add(time.Minute)) || time.Since(now) > 2*time.Second {
		t.Fatalf("long retry hint not surfaced intact: %v, hint=%s, attempts=%d", err, at.Sub(now), calls.Load())
	}
}

func TestDropboxConcurrentRefreshPersistsAcrossBackendRestart(t *testing.T) {
	dir := t.TempDir()
	token := &DropboxToken{AccessToken: "synthetic-old", RefreshToken: "synthetic-refresh", AccountID: "mock-account"}
	data, err := json.Marshal(token)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(DropboxTokenPath(dir), data, 0o644); err != nil {
		t.Fatal(err)
	}
	f := newFakeDropbox()
	b := mockDropbox(t, f, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path == "/2/files/download" && r.Header.Get("Authorization") == "Bearer synthetic-old" {
			w.WriteHeader(http.StatusUnauthorized)
			return true
		}
		return false
	})
	b.dataDir = dir
	b.token.Store(token)
	if err := parallelEach(12, 12, func(int) error {
		_, err := b.Get("/api/items/root:/info.json:/content")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(DropboxTokenPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("refreshed cache permissions = %v", info.Mode().Perm())
	}
	reloaded, err := NewDropboxBackend(&config.Config{DataDir: dir, DropboxAuth: "legacy-bootstrap"})
	if err != nil {
		t.Fatal(err)
	}
	reloaded.apiBase, reloaded.contentBase = b.apiBase, b.contentBase
	if _, err := reloaded.Get("/api/items/root:/info.json:/content"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refreshCalls != 1 || f.bearer != "new-token" || reloaded.token.Load().RefreshToken != "rt-new" {
		t.Fatalf("refresh or reload failed: refresh calls=%d", f.refreshCalls)
	}
}

func TestDropboxLegacyAndRevokedAuthorization(t *testing.T) {
	for _, scenario := range []string{"valid legacy", "missing after 401", "known expired", "revoked", "refresh without rotation"} {
		t.Run(scenario, func(t *testing.T) {
			var refreshes atomic.Int32
			b := mockDropbox(t, newFakeDropbox(), func(w http.ResponseWriter, r *http.Request) bool {
				if r.URL.Path == "/oauth2/token" {
					refreshes.Add(1)
					if scenario == "revoked" {
						w.WriteHeader(http.StatusBadRequest)
						_, _ = io.WriteString(w, `{"error":"invalid_grant","error_description":"sensitive synthetic details"}`)
					} else {
						_, _ = io.WriteString(w, `{"access_token":"replacement","expires_in":3600}`)
					}
					return true
				}
				if scenario == "valid legacy" || r.Header.Get("Authorization") == "Bearer replacement" {
					return false
				}
				w.WriteHeader(http.StatusUnauthorized)
				return true
			})
			token := &DropboxToken{AccessToken: "legacy", ExpiresAt: time.Now().Add(20 * time.Second).UnixMilli()}
			if scenario == "revoked" || scenario == "refresh without rotation" {
				token.RefreshToken = "synthetic-refresh"
			}
			if scenario == "known expired" {
				token.ExpiresAt = time.Now().Add(-time.Second).UnixMilli()
			}
			b.token.Store(token)
			var wantRefresh int32
			if scenario == "revoked" || scenario == "refresh without rotation" {
				wantRefresh = 1
			}
			for range 2 {
				_, err := b.Get("/api/items/root:/info.json:/content")
				switch scenario {
				case "valid legacy", "refresh without rotation":
					if err != nil {
						t.Fatal(err)
					}
				default:
					if !errors.Is(err, ErrDropboxReauthorize) || !strings.Contains(err.Error(), "dropbox-auth") || strings.Contains(err.Error(), "sensitive synthetic details") {
						t.Fatalf("expected actionable, sanitized authorization error, got %v", err)
					}
				}
			}
			if refreshes.Load() != wantRefresh {
				t.Fatalf("refreshes=%d, want %d", refreshes.Load(), wantRefresh)
			}
			if scenario == "refresh without rotation" && b.token.Load().RefreshToken != token.RefreshToken {
				t.Fatal("refresh response without a new token discarded the existing refresh token")
			}
		})
	}
}
