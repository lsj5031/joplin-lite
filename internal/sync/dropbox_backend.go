package sync

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lsj5031/joplin-lite/internal/config"
	"github.com/lsj5031/joplin-lite/internal/store"
)

// DropboxBackend implements SyncBackend over Dropbox API v2 (sync target 7,
// app-folder scope), translating the Joplin-Server-style engine paths the same
// way s3_backend.go does:
//
//   - files/upload, files/download for item content
//     (Dropbox-API-Arg header, raw body),
//   - files/list_folder + files/list_folder/continue for the delta/children
//     emulation, merging changes into a complete persisted inventory,
//   - files/delete_v2 for deletions.
//
// Deletions update the inventory and surface through the reconcile pass
// (PullMissingItems), same as LocalFolderBackend. Locks use
// the shared flat-file protocol (locks/<type>_<clientType>_<clientId>.json).
type DropboxBackend struct {
	oauth        *DropboxOAuth
	token        atomic.Pointer[DropboxToken]
	tokenGate    chan struct{}
	pendingToken *DropboxToken // guarded by tokenGate
	authErr      error         // guarded by tokenGate
	identity     string
	dataDir      string // token store location
	apiBase      string // default https://api.dropboxapi.com
	contentBase  string // default https://content.dropboxapi.com
	http         *http.Client
	locks        *fileLockManager
	blockDeletes bool
	ctx          context.Context
	inSync       bool
	rootListed   bool
	rootEntries  []dropboxEntry
	db           *store.DB
	inventory    *dropboxInventory
	cacheHits    atomic.Int64
	downloads    atomic.Int64
	requests     atomic.Int64
	pacer        dropboxPacer
	pushWorkers  int
	lockTTL      time.Duration
	lockInterval time.Duration
	lockTimeout  time.Duration
}

// NewDropboxBackend creates the Dropbox driver from config. Credentials come
// from the user's own Dropbox OAuth app (sync.7.appKey / sync.7.appSecret);
// the token comes from <dataDir>/dropbox_token.json or sync.7.auth.
func NewDropboxBackend(cfg *config.Config) (*DropboxBackend, error) {
	token, err := LoadDropboxToken(cfg.DataDir, cfg.DropboxAuth)
	if err != nil {
		return nil, err
	}
	if token == nil {
		return nil, fmt.Errorf("no Dropbox token found: %w", ErrDropboxReauthorize)
	}
	oauth := NewDropboxOAuth(cfg.DropboxAppKey, cfg.DropboxAppSecret)
	b := newDropboxBackend(oauth, token, cfg.DataDir)
	b.blockDeletes = cfg.DropboxBlockDeletes
	if cfg.DropboxPushWorkers > 0 {
		b.pushWorkers = min(cfg.DropboxPushWorkers, pushWorkers)
	}
	return b, nil
}

// newDropboxBackend wires a backend with explicit endpoints (tests point the
// API bases at a fake server).
func newDropboxBackend(oauth *DropboxOAuth, token *DropboxToken, dataDir string) *DropboxBackend {
	b := &DropboxBackend{
		oauth:        oauth,
		tokenGate:    make(chan struct{}, 1),
		dataDir:      dataDir,
		apiBase:      "https://api.dropboxapi.com",
		contentBase:  "https://content.dropboxapi.com",
		http:         &http.Client{Timeout: 120 * time.Second},
		ctx:          context.Background(),
		pushWorkers:  3,
		lockTTL:      LockTTL,
		lockInterval: LockRefreshInterval,
		lockTimeout:  30 * time.Second,
	}
	b.token.Store(token)
	if token != nil {
		b.identity = token.AccountID
		if b.identity == "" {
			b.identity = token.RefreshToken
		}
		if b.identity == "" {
			b.identity = token.AccessToken
		}
	}
	b.locks = newFileLockManager(b, LockClientTypeCLI, "joplin-lite")
	return b
}

// SyncTarget returns 7 (Dropbox).
func (b *DropboxBackend) SyncTarget() int { return 7 }

func (b *DropboxBackend) pushConcurrency(limit int) int {
	return min(b.pushWorkers, limit)
}

// BeginSync shares one fresh, complete inventory between delta and reconciliation.
// The profile retains its listing cursor and pending changes across restarts.
func (b *DropboxBackend) BeginSync(ctx context.Context, db *store.DB) func() {
	b.ctx, b.inSync = ctx, true
	b.db, b.inventory = db, nil
	b.rootEntries, b.rootListed = nil, false
	b.requests.Store(0)
	b.cacheHits.Store(0)
	b.downloads.Store(0)
	started := time.Now()
	return func() {
		slog.Info("Dropbox pass finished", "storage_requests", b.requests.Load(), "cached_items", b.cacheHits.Load(), "downloaded_items", b.downloads.Load(), "duration", time.Since(started))
		b.ctx, b.inSync = context.Background(), false
		b.db, b.inventory = nil, nil
		b.rootEntries, b.rootListed = nil, false
	}
}

// AcquireLock acquires the flat-file SYNC lock (ErrExclusiveLock when another
// client holds an active EXCLUSIVE lock).
func (b *DropboxBackend) AcquireLock() (interface{}, error) {
	lock, err := b.locks.Acquire()
	if err != nil {
		return nil, err
	}
	return lock, nil
}

// ReleaseLock releases our lock.
func (b *DropboxBackend) ReleaseLock(lock interface{}) error {
	if lock == nil {
		return nil
	}
	// Leave time for the worker to exit inside the shell's five-second wait.
	deadline := time.Now().Add(3 * time.Second)
	if earlier, ok := b.ctx.Deadline(); ok && earlier.Before(deadline) {
		deadline = earlier
	}
	ctx, cancel := context.WithDeadline(context.WithoutCancel(b.ctx), deadline)
	defer cancel()
	err := b.deletePath(ctx, "/locks/"+lockFileName(lock.(*Lock)))
	if err != nil {
		slog.Warn("failed to release Dropbox lock", "error", err)
	}
	return err
}

// Get implements SyncBackend for the Joplin-Server-style paths.
func (b *DropboxBackend) Get(path string) ([]byte, error) {
	if err := b.ctx.Err(); err != nil {
		return nil, err
	}
	path, rawQuery, _ := strings.Cut(path, "?")
	key, op := parseDropboxPath(path)
	if key == "" && op == "delta" {
		return b.getDelta(rawQuery)
	}
	if key == "" && op == "children" {
		return b.getChildren()
	}
	if op == "content" {
		return b.download(dropboxPath(key))
	}
	return nil, fmt.Errorf("unsupported Dropbox path: %s", path)
}

// Put implements SyncBackend.
func (b *DropboxBackend) Put(path string, content []byte) error {
	key, op := parseDropboxPath(path)
	if key == "" || op != "content" {
		return fmt.Errorf("unsupported Dropbox put path: %s", path)
	}
	return b.upload(dropboxPath(key), content)
}

// Delete implements SyncBackend.
func (b *DropboxBackend) Delete(path string) error {
	return b.deleteInScope(path, "")
}

type dropboxDeleteScopeKey struct{}

func (b *DropboxBackend) deleteInScope(path, scope string) error {
	if b.blockDeletes {
		return fmt.Errorf("remote deletion blocked: sync.dropbox.block_deletes is enabled; deletion remains queued")
	}
	key, op := parseDropboxPath(path)
	if key == "" || op != "content" {
		return fmt.Errorf("unsupported Dropbox delete path: %s", path)
	}
	ctx := b.ctx
	if scope != "" {
		ctx = context.WithValue(ctx, dropboxDeleteScopeKey{}, scope)
	}
	return b.deletePath(ctx, dropboxPath(key))
}

// parseDropboxPath extracts key and operation from engine paths
// (same shapes as s3_backend.parsePath):
//   - /api/items/root:/:/delta -> ("", "delta")
//   - /api/items/root:/:/children -> ("", "children")
//   - /api/items/root:/info.json:/content -> ("info.json", "content")
func parseDropboxPath(path string) (key, op string) {
	const prefix = "/api/items/root:/"
	if !strings.HasPrefix(path, prefix) {
		return "", ""
	}
	rest := path[len(prefix):]
	parts := strings.SplitN(rest, ":/", 2)
	if len(parts) < 2 {
		return "", ""
	}
	if parts[1] == "content" || strings.HasSuffix(parts[1], ":/content") {
		return parts[0], "content"
	}
	if parts[0] == "" || parts[0] == ":" {
		if strings.HasSuffix(rest, "delta") {
			return "", "delta"
		}
		if strings.HasSuffix(rest, "children") {
			return "", "children"
		}
	}
	return "", ""
}

// dropboxPath turns an engine item name (path-escaped, e.g.
// .resource%2F<id>) into a Dropbox path inside the app folder.
func dropboxPath(key string) string {
	if unescaped, err := url.PathUnescape(key); err == nil {
		key = unescaped
	}
	return "/" + strings.TrimPrefix(key, "/")
}

// dropboxEntry is one file from files/list_folder.
type dropboxEntry struct {
	Tag            string `json:".tag"`
	PathDisplay    string `json:"path_display"`
	PathLower      string `json:"path_lower"`
	ServerModified string `json:"server_modified"` // RFC3339 with ms
	Rev            string `json:"rev"`
	ContentHash    string `json:"content_hash"`
}

type listFolderResponse struct {
	Entries []dropboxEntry `json:"entries"`
	Cursor  string         `json:"cursor"`
	HasMore bool           `json:"has_more"`
}

// listAll lists the whole app folder recursively (all pages).
func (b *DropboxBackend) listAll(path string) ([]dropboxEntry, error) {
	if path == "" && b.inSync && b.rootListed {
		return b.rootEntries, nil
	}
	if path == "" && b.inSync && b.db != nil {
		return b.listInventory()
	}
	return b.listFolder(b.ctx, path)
}

func (b *DropboxBackend) listFolder(ctx context.Context, path string) ([]dropboxEntry, error) {
	started := time.Now()
	pages := 0
	var out []dropboxEntry
	var timeout time.Duration
	if path == "/locks" {
		timeout = b.lockTimeout
	}
	arg := map[string]any{"path": path, "recursive": true, "limit": 500}
	endpoint := "/2/files/list_folder"
	for {
		var resp listFolderResponse
		if err := b.jsonRequest(ctx, endpoint, timeout, false, arg, &resp); err != nil {
			return nil, err
		}
		pages++
		out = append(out, resp.Entries...)
		if !resp.HasMore {
			if path == "" {
				slog.Info("Dropbox inventory listed", "entries", len(out), "pages", pages, "duration", time.Since(started))
				if b.inSync {
					b.rootEntries, b.rootListed = out, true
				}
			}
			return out, nil
		}
		arg = map[string]any{"cursor": resp.Cursor}
		endpoint = "/2/files/list_folder/continue"
	}
}

// entryName converts a Dropbox path to a target item name (or "" for system
// paths). Item names mirror the flat-file layout: <id>.md, .resource/<id>.
func entryName(pathDisplay string) string {
	name := strings.TrimPrefix(pathDisplay, "/")
	if name == "" || name == "info.json" || strings.HasPrefix(name, "locks/") || strings.HasPrefix(name, "temp/") {
		return ""
	}
	return name
}

func entryTimeMs(e dropboxEntry) int64 {
	if t, err := time.Parse(time.RFC3339Nano, e.ServerModified); err == nil {
		return t.UnixMilli()
	}
	return 0
}

// checkpointScope isolates persisted state by endpoint and authorization.
func (b *DropboxBackend) checkpointScope() string {
	return fmt.Sprintf("dropbox:%x", sha256.Sum256([]byte(b.apiBase+"\x00"+b.contentBase+"\x00"+b.identity)))
}

// getDelta delivers pending inventory changes. Calls outside an engine pass
// retain the standalone timestamp-listing behavior.
func (b *DropboxBackend) getDelta(rawQuery string) ([]byte, error) {
	var cursor int64
	if rawQuery != "" {
		vals, _ := url.ParseQuery(rawQuery)
		cursor, _ = parseInt64(vals.Get("cursor"))
	}
	entries, err := b.listAll("")
	if err != nil {
		return nil, err
	}
	maxMs := cursor
	resp := DeltaResponse{CheckpointScope: b.checkpointScope()}
	for _, e := range entries {
		if e.Tag != "file" {
			continue
		}
		name := entryName(e.PathDisplay)
		if name == "" {
			continue
		}
		ts := entryTimeMs(e)
		if ts > maxMs {
			maxMs = ts
		}
		if b.inventory != nil {
			if !b.inventory.Pending[e.path()] {
				continue
			}
		} else if cursor != 0 && ts < cursor {
			continue
		}
		resp.Items = append(resp.Items, DeltaItem{
			ID:          strings.TrimSuffix(name, ".md"),
			ItemName:    name,
			Type:        1, // PUT
			UpdatedTime: ts,
			Revision:    e.Rev,
		})
	}
	resp.Cursor = fmt.Sprintf("%d", maxMs)
	if b.inventory != nil {
		resp.Cursor = b.inventory.applyCursor(resp.CheckpointScope)
	}
	return json.Marshal(resp)
}

func parseInt64(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}

func (b *DropboxBackend) getChildren() ([]byte, error) {
	entries, err := b.listAll("")
	if err != nil {
		return nil, err
	}
	resp := ChildrenResponse{}
	for _, e := range entries {
		if e.Tag != "file" {
			continue
		}
		if name := entryName(e.PathDisplay); name != "" {
			resp.Items = append(resp.Items, ChildItem{ID: strings.TrimSuffix(name, ".md"), Name: name})
		}
	}
	return json.Marshal(resp)
}

// apiJSON posts a JSON-RPC-style Dropbox request (apiBase endpoint).
func (b *DropboxBackend) apiJSON(ctx context.Context, endpoint string, arg any, out any) error {
	return b.jsonRequest(ctx, endpoint, 0, false, arg, out)
}

func (b *DropboxBackend) jsonRequest(ctx context.Context, endpoint string, timeout time.Duration, write bool, arg any, out any) error {
	body, err := json.Marshal(arg)
	if err != nil {
		return err
	}
	respBody, err := b.do(ctx, endpoint, timeout, write, func(req *http.Request) {
		req.URL, _ = url.Parse(b.apiBase + endpoint)
		req.Header.Set("Content-Type", "application/json")
		req.Body = io.NopCloser(bytes.NewReader(body))
		req.ContentLength = int64(len(body))
	})
	if err != nil {
		return err
	}
	if out != nil {
		return json.Unmarshal(respBody, out)
	}
	return nil
}

// Uploads are guarded by the revision observed before the first attempt. If
// the response was lost after commit, a replay cannot overwrite a peer's
// intervening edit or create a second copy.
func (b *DropboxBackend) upload(path string, content []byte) error {
	timeout := b.mutationTimeout(path)
	before, err := b.metadata(b.ctx, path, timeout)
	if err != nil {
		return err
	}
	_, err = b.uploadRevision(b.ctx, path, content, before, timeout)
	return err
}

func (b *DropboxBackend) uploadRevision(ctx context.Context, path string, content []byte, before *dropboxEntry, timeout time.Duration) (*dropboxEntry, error) {
	var mode any = "add"
	if before != nil {
		if before.Rev == "" {
			return nil, fmt.Errorf("cannot safely update %s without a Dropbox revision", path)
		}
		mode = map[string]string{".tag": "update", "update": before.Rev}
	}
	arg, err := json.Marshal(map[string]any{
		"path": path, "mode": mode, "autorename": false, "mute": true, "strict_conflict": true,
	})
	if err != nil {
		return nil, err
	}
	respBody, err := b.do(ctx, "/2/files/upload", timeout, true, func(req *http.Request) {
		req.URL, _ = url.Parse(b.contentBase + "/2/files/upload")
		req.Header.Set("Dropbox-API-Arg", string(arg))
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Body = io.NopCloser(bytes.NewReader(content))
		req.ContentLength = int64(len(content))
	})
	if err != nil {
		if ctx.Err() == nil && (isDropboxStatus(err, http.StatusConflict) || ambiguousDropboxResponse(err)) {
			current, checkErr := b.metadata(ctx, path, timeout)
			if checkErr != nil {
				return nil, errors.Join(err, checkErr)
			}
			if current != nil {
				hash, hashErr := hex.DecodeString(current.ContentHash)
				if hashErr == nil && matchesDropboxContent(content, hash) {
					return current, nil
				}
			}
		}
		return nil, err
	}
	var entry dropboxEntry
	if err := json.Unmarshal(respBody, &entry); err != nil {
		return nil, fmt.Errorf("cannot parse Dropbox upload response: %w", err)
	}
	return &entry, nil
}

// download fetches a Dropbox path (files/download). Missing files return
// nil, nil — the engine's "may not exist yet" convention.
func (b *DropboxBackend) download(path string) ([]byte, error) {
	if b.inventory != nil {
		if entry, ok := b.inventory.Entries[strings.ToLower(path)]; ok {
			if data, ok := cachedDropboxItem(b.db, entry); ok {
				b.cacheHits.Add(1)
				return data, nil
			}
		}
	}
	arg, err := json.Marshal(map[string]string{"path": path})
	if err != nil {
		return nil, err
	}
	respBody, err := b.do(b.ctx, "/2/files/download", 0, false, func(req *http.Request) {
		req.URL, _ = url.Parse(b.contentBase + "/2/files/download")
		req.Header.Set("Dropbox-API-Arg", string(arg))
	})
	if isDropboxNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(path, "/locks/") && path != "/info.json" {
		b.downloads.Add(1)
	}
	return respBody, nil
}

// deletePath deletes a Dropbox path (files/delete_v2). Missing files are a
// no-op (the item is already gone from the target).
func (b *DropboxBackend) deletePath(ctx context.Context, path string) error {
	timeout := b.mutationTimeout(path)
	before, err := b.metadata(ctx, path, timeout)
	if err != nil || before == nil {
		return err
	}
	if before.Rev == "" {
		return fmt.Errorf("cannot safely delete %s without a Dropbox revision", path)
	}
	err = b.jsonRequest(ctx, "/2/files/delete_v2", timeout, true, map[string]string{"path": path, "parent_rev": before.Rev}, nil)
	if isDropboxNotFound(err) {
		return nil
	}
	if ctx.Err() == nil && ambiguousDropboxResponse(err) {
		current, checkErr := b.metadata(ctx, path, timeout)
		if checkErr == nil && current == nil {
			return nil
		}
	}
	return err
}

func (b *DropboxBackend) metadata(ctx context.Context, path string, timeout time.Duration) (*dropboxEntry, error) {
	var out dropboxEntry
	err := b.jsonRequest(ctx, "/2/files/get_metadata", timeout, false, map[string]string{"path": path}, &out)
	if isDropboxNotFound(err) {
		return nil, nil
	}
	return &out, err
}

func (b *DropboxBackend) mutationTimeout(path string) time.Duration {
	if strings.HasPrefix(path, "/locks/") {
		return b.lockTimeout
	}
	return 0
}

func isDropboxStatus(err error, status int) bool {
	var api *DropboxAPIError
	return errors.As(err, &api) && api.Status == status
}

func isDropboxNotFound(err error) bool {
	var api *DropboxAPIError
	if !errors.As(err, &api) || api.Status != http.StatusConflict {
		return false
	}
	s := strings.TrimRight(api.summary, "/")
	return s == "path/not_found" || s == "path_lookup/not_found"
}

var errDropboxCursorReset = errors.New("Dropbox listing cursor reset")

// lockStore implementation (flat-file lock protocol over Dropbox).

func (b *DropboxBackend) getLock(name string) ([]byte, error) {
	return b.download("/locks/" + name)
}

func (b *DropboxBackend) putLock(name string, data []byte) error {
	return b.upload("/locks/"+name, data)
}

func (b *DropboxBackend) deleteLock(name string) error {
	return b.deletePath(b.ctx, "/locks/"+name)
}

func (b *DropboxBackend) listLocks() ([]LockFile, error) {
	entries, err := b.listAll("/locks")
	if err != nil {
		if isDropboxNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []LockFile
	for _, e := range entries {
		if e.Tag != "file" {
			continue
		}
		name := e.PathDisplay
		if i := strings.LastIndex(name, "/"); i >= 0 {
			name = name[i+1:]
		}
		out = append(out, LockFile{Name: name, UpdatedTime: entryTimeMs(e)})
	}
	return out, nil
}
