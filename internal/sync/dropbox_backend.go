package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lsj5031/joplin-lite/internal/config"
)

// DropboxBackend implements SyncBackend over Dropbox API v2 (sync target 7,
// app-folder scope), translating the Joplin-Server-style engine paths the same
// way s3_backend.go does:
//
//   - files/upload, files/download for item content
//     (Dropbox-API-Arg header, raw body),
//   - files/list_folder + files/list_folder/continue for the delta/children
//     emulation (flat-file targets have no delta API — the listing is
//     synthesized like s3_backend.getDelta),
//   - files/delete_v2 for deletions.
//
// Deletions cannot be reconstructed from a listing; they surface through the
// reconcile pass (PullMissingItems), same as LocalFolderBackend. Locks use
// the shared flat-file protocol (locks/<type>_<clientType>_<clientId>.json).
type DropboxBackend struct {
	oauth       *DropboxOAuth
	token       *DropboxToken
	dataDir     string // token store location
	apiBase     string // default https://api.dropboxapi.com
	contentBase string // default https://content.dropboxapi.com
	http        *http.Client
	locks       *fileLockManager
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
		return nil, fmt.Errorf("no Dropbox token found — run 'dropbox-auth' to authorize this client")
	}
	oauth := NewDropboxOAuth(cfg.DropboxAppKey, cfg.DropboxAppSecret)
	b := newDropboxBackend(oauth, token, cfg.DataDir)
	return b, nil
}

// newDropboxBackend wires a backend with explicit endpoints (tests point the
// API bases at a fake server).
func newDropboxBackend(oauth *DropboxOAuth, token *DropboxToken, dataDir string) *DropboxBackend {
	b := &DropboxBackend{
		oauth:       oauth,
		token:       token,
		dataDir:     dataDir,
		apiBase:     "https://api.dropboxapi.com",
		contentBase: "https://content.dropboxapi.com",
		http:        &http.Client{Timeout: 120 * time.Second},
	}
	b.locks = newFileLockManager(b, LockClientTypeCLI, "joplin-lite")
	return b
}

// SyncTarget returns 7 (Dropbox).
func (b *DropboxBackend) SyncTarget() int { return 7 }

// Authenticate refreshes the access token if it is expired.
func (b *DropboxBackend) Authenticate() error {
	if b.token == nil {
		return fmt.Errorf("no Dropbox token — run 'dropbox-auth' first")
	}
	if !b.token.expired(time.Now()) {
		return nil
	}
	if b.token.RefreshToken == "" {
		return fmt.Errorf("Dropbox access token expired and there is no refresh token — run 'dropbox-auth' again")
	}
	if b.oauth == nil || b.oauth.AppKey == "" {
		return fmt.Errorf("refreshing this Dropbox token requires its original OAuth app key")
	}
	tok, err := b.oauth.Refresh(context.Background(), b.token.RefreshToken)
	if err != nil {
		return fmt.Errorf("cannot refresh Dropbox token: %w", err)
	}
	if tok.RefreshToken == "" {
		tok.RefreshToken = b.token.RefreshToken
	}
	b.token = tok
	if b.dataDir != "" {
		return SaveDropboxToken(b.dataDir, tok)
	}
	return nil
}

// IsAuthenticated reports whether a token is present (expiry is handled in
// Authenticate via the refresh token).
func (b *DropboxBackend) IsAuthenticated() bool {
	return b.token != nil && (b.token.AccessToken != "" || b.token.RefreshToken != "")
}

// AcquireLock acquires the flat-file SYNC lock (ErrExclusiveLock when another
// client holds an active EXCLUSIVE lock).
func (b *DropboxBackend) AcquireLock() (interface{}, error) {
	return b.locks.Acquire()
}

// ReleaseLock releases our lock.
func (b *DropboxBackend) ReleaseLock(lock interface{}) error {
	if lock == nil {
		return nil
	}
	return b.locks.Release()
}

// RefreshLock refreshes our held lock while it is still active.
func (b *DropboxBackend) RefreshLock(lock interface{}) error {
	if lock == nil {
		return nil
	}
	return b.locks.Refresh()
}

// Get implements SyncBackend for the Joplin-Server-style paths.
func (b *DropboxBackend) Get(path string) ([]byte, error) {
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
	key, op := parseDropboxPath(path)
	if key == "" || op != "content" {
		return fmt.Errorf("unsupported Dropbox delete path: %s", path)
	}
	return b.deletePath(dropboxPath(key))
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
	ServerModified string `json:"server_modified"` // RFC3339 with ms
}

type listFolderResponse struct {
	Entries []dropboxEntry `json:"entries"`
	Cursor  string         `json:"cursor"`
	HasMore bool           `json:"has_more"`
}

// listAll lists the whole app folder recursively (all pages).
func (b *DropboxBackend) listAll(path string) ([]dropboxEntry, error) {
	var out []dropboxEntry
	arg := map[string]any{"path": path, "recursive": true, "limit": 500}
	endpoint := "/2/files/list_folder"
	for {
		var resp listFolderResponse
		if err := b.apiJSON(endpoint, arg, &resp); err != nil {
			return nil, err
		}
		out = append(out, resp.Entries...)
		if !resp.HasMore {
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

// getDelta synthesizes a delta response from the listing (s3_backend model,
// at-least-once delivery at the cursor boundary like LocalFolderBackend).
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
	resp := DeltaResponse{}
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
		if cursor != 0 && ts < cursor {
			continue
		}
		resp.Items = append(resp.Items, DeltaItem{
			ID:          strings.TrimSuffix(name, ".md"),
			ItemName:    name,
			Type:        1, // PUT
			UpdatedTime: ts,
		})
	}
	resp.Cursor = fmt.Sprintf("%d", maxMs)
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
func (b *DropboxBackend) apiJSON(endpoint string, arg any, out any) error {
	body, err := json.Marshal(arg)
	if err != nil {
		return err
	}
	respBody, status, err := b.do(func(req *http.Request) {
		req.URL, _ = url.Parse(b.apiBase + endpoint)
		req.Header.Set("Content-Type", "application/json")
		req.Body = io.NopCloser(strings.NewReader(string(body)))
		req.ContentLength = int64(len(body))
	}, endpoint)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return dropboxError(status, respBody)
	}
	if out != nil {
		return json.Unmarshal(respBody, out)
	}
	return nil
}

// upload uploads raw content to a Dropbox path (files/upload).
func (b *DropboxBackend) upload(path string, content []byte) error {
	arg, err := json.Marshal(map[string]any{
		"path": path, "mode": "overwrite", "autorename": false, "mute": true,
	})
	if err != nil {
		return err
	}
	respBody, status, err := b.do(func(req *http.Request) {
		req.URL, _ = url.Parse(b.contentBase + "/2/files/upload")
		req.Header.Set("Dropbox-API-Arg", string(arg))
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Body = io.NopCloser(strings.NewReader(string(content)))
		req.ContentLength = int64(len(content))
	}, "/2/files/upload")
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return dropboxError(status, respBody)
	}
	return nil
}

// download fetches a Dropbox path (files/download). Missing files return
// nil, nil — the engine's "may not exist yet" convention.
func (b *DropboxBackend) download(path string) ([]byte, error) {
	arg, err := json.Marshal(map[string]string{"path": path})
	if err != nil {
		return nil, err
	}
	respBody, status, err := b.do(func(req *http.Request) {
		req.URL, _ = url.Parse(b.contentBase + "/2/files/download")
		req.Header.Set("Dropbox-API-Arg", string(arg))
	}, "/2/files/download")
	if err != nil {
		return nil, err
	}
	if status == http.StatusConflict && isNotFound(respBody) {
		return nil, nil
	}
	if status != http.StatusOK {
		return nil, dropboxError(status, respBody)
	}
	return respBody, nil
}

// deletePath deletes a Dropbox path (files/delete_v2). Missing files are a
// no-op (the item is already gone from the target).
func (b *DropboxBackend) deletePath(path string) error {
	var out any
	err := b.apiJSON("/2/files/delete_v2", map[string]string{"path": path}, &out)
	if err != nil && isNotFound([]byte(err.Error())) {
		return nil
	}
	return err
}

// do runs an authenticated request, refreshing the token once on 401.
func (b *DropboxBackend) do(setup func(*http.Request), endpoint string) ([]byte, int, error) {
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequest(http.MethodPost, "", nil)
		if err != nil {
			return nil, 0, err
		}
		setup(req)
		req.Header.Set("Authorization", "Bearer "+b.token.AccessToken)
		resp, err := b.http.Do(req)
		if err != nil {
			return nil, 0, err
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
		resp.Body.Close()
		if err != nil {
			return nil, resp.StatusCode, err
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			if aerr := b.Authenticate(); aerr != nil {
				return nil, resp.StatusCode, aerr
			}
			continue
		}
		return body, resp.StatusCode, nil
	}
	return nil, 0, fmt.Errorf("unauthorized: %s", endpoint)
}

// dropboxError renders a Dropbox API error body
// ({"error_summary": "path/not_found/..."}).
func dropboxError(status int, body []byte) error {
	var e struct {
		ErrorSummary string `json:"error_summary"`
	}
	_ = json.Unmarshal(body, &e)
	if e.ErrorSummary != "" {
		return fmt.Errorf("dropbox API error %d: %s", status, e.ErrorSummary)
	}
	return fmt.Errorf("dropbox API error %d: %s", status, strings.TrimSpace(string(body)))
}

func isNotFound(body []byte) bool {
	return strings.Contains(string(body), "not_found")
}

// lockStore implementation (flat-file lock protocol over Dropbox).

func (b *DropboxBackend) getLock(name string) ([]byte, error) {
	return b.download("/locks/" + name)
}

func (b *DropboxBackend) putLock(name string, data []byte) error {
	return b.upload("/locks/"+name, data)
}

func (b *DropboxBackend) deleteLock(name string) error {
	return b.deletePath("/locks/" + name)
}

func (b *DropboxBackend) listLocks() ([]LockFile, error) {
	entries, err := b.listAll("/locks")
	if err != nil {
		if isNotFound([]byte(err.Error())) {
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
