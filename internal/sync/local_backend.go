package sync

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// LocalFolderBackend implements SyncBackend over a plain directory. It is the
// dry-run rehearsal target for the sync engine before the Dropbox driver (M2):
// the same Joplin-Server-style API paths the engine already speaks, backed by
// files on disk instead of HTTP.
//
// Delta is emulated from file mtimes with at-least-once delivery (items at the
// cursor boundary are re-sent; applying an item is idempotent). Deletions are
// not reconstructable from mtimes, so they surface through the reconcile pass
// (PullMissingItems), which is also how a missed delta delete is handled.
type LocalFolderBackend struct {
	dir           string
	clientID      string
	authenticated bool
	locks         *fileLockManager
}

// NewLocalFolderBackend creates a backend rooted at dir.
func NewLocalFolderBackend(dir string) *LocalFolderBackend {
	b := &LocalFolderBackend{dir: dir, clientID: "joplin-lite"}
	b.locks = newFileLockManager(b, LockClientTypeCLI, b.clientID)
	return b
}

func (b *LocalFolderBackend) Authenticate() error {
	b.authenticated = true
	return nil
}

func (b *LocalFolderBackend) IsAuthenticated() bool {
	return b.authenticated
}

// SyncTarget returns 2 (Joplin's file-system sync target id).
func (b *LocalFolderBackend) SyncTarget() int {
	return 2
}

// AcquireLock acquires the flat-file SYNC lock Joplin uses on file targets
// (locks/<type>_<clientType>_<clientId>.json). It returns ErrExclusiveLock
// when another client holds an active EXCLUSIVE lock.
func (b *LocalFolderBackend) AcquireLock() (interface{}, error) {
	return b.locks.Acquire()
}

func (b *LocalFolderBackend) ReleaseLock(lock interface{}) error {
	if lock == nil {
		return nil
	}
	return b.locks.Release()
}

// RefreshLock refreshes our held lock while it is still active.
func (b *LocalFolderBackend) RefreshLock(lock interface{}) error {
	if lock == nil {
		return nil
	}
	return b.locks.Refresh()
}

// lockStore implementation (flat-file lock protocol).

func (b *LocalFolderBackend) lockPath(name string) string {
	return filepath.Join(b.dir, "locks", filepath.FromSlash(name))
}

func (b *LocalFolderBackend) getLock(name string) ([]byte, error) {
	data, err := os.ReadFile(b.lockPath(name))
	if os.IsNotExist(err) {
		return nil, nil
	}
	return data, err
}

func (b *LocalFolderBackend) putLock(name string, data []byte) error {
	if err := os.MkdirAll(filepath.Join(b.dir, "locks"), 0o755); err != nil {
		return err
	}
	return os.WriteFile(b.lockPath(name), data, 0o644)
}

func (b *LocalFolderBackend) deleteLock(name string) error {
	err := os.Remove(b.lockPath(name))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (b *LocalFolderBackend) listLocks() ([]LockFile, error) {
	entries, err := os.ReadDir(filepath.Join(b.dir, "locks"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []LockFile
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return nil, err
		}
		out = append(out, LockFile{Name: e.Name(), UpdatedTime: info.ModTime().UnixMilli()})
	}
	return out, nil
}

// itemNameFromPath extracts the item name from a Joplin-Server-style content
// path: /api/items/root:/<name>:/content. Names may contain "/" (the resource
// blobs live under .resource/<id>) and arrive path-escaped like the S3
// driver's keys — .resource%2F<id> — so they are unescaped here.
func localItemName(path string) (string, error) {
	const prefix = "/api/items/root:/"
	const suffix = ":/content"
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return "", fmt.Errorf("unsupported path %q", path)
	}
	name := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	if unescaped, err := url.PathUnescape(name); err == nil {
		name = unescaped
	}
	return name, nil
}

func (b *LocalFolderBackend) Get(path string) ([]byte, error) {
	if strings.Contains(path, ":/delta") {
		return b.delta(path)
	}
	if strings.Contains(path, ":/children") {
		return b.children()
	}
	name, err := localItemName(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(b.dir, filepath.FromSlash(name)))
	if os.IsNotExist(err) {
		return nil, nil // "may not exist yet" — same convention as the S3 backend
	}
	return data, err
}

func (b *LocalFolderBackend) Put(path string, content []byte) error {
	name, err := localItemName(path)
	if err != nil {
		return err
	}
	p := filepath.Join(b.dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, content, 0o644)
}

func (b *LocalFolderBackend) Delete(path string) error {
	name, err := localItemName(path)
	if err != nil {
		return err
	}
	err = os.Remove(filepath.Join(b.dir, filepath.FromSlash(name)))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// listItems scans the target root for sync items: <id>.md files and
// .resource/<id> blobs. info.json, locks/ and temp/ are not items.
func (b *LocalFolderBackend) listItems() ([]DeltaItem, error) {
	var items []DeltaItem
	err := filepath.WalkDir(b.dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != b.dir && (d.Name() == "locks" || d.Name() == "temp") {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(b.dir, p)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if name == "info.json" || (!strings.HasSuffix(name, ".md") && !strings.HasPrefix(name, ".resource/")) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		items = append(items, DeltaItem{
			ID:          strings.TrimSuffix(name, ".md"),
			ItemName:    name,
			Type:        1, // PUT
			UpdatedTime: info.ModTime().UnixMilli(),
		})
		return nil
	})
	sort.Slice(items, func(i, j int) bool { return items[i].ItemName < items[j].ItemName })
	return items, err
}

func (b *LocalFolderBackend) delta(path string) ([]byte, error) {
	cursor := int64(0)
	if i := strings.Index(path, "cursor="); i >= 0 {
		cursor, _ = strconv.ParseInt(path[i+len("cursor="):], 10, 64)
	}

	all, err := b.listItems()
	if err != nil {
		return nil, err
	}

	maxMs := cursor
	resp := DeltaResponse{Cursor: strconv.FormatInt(cursor, 10)}
	for _, it := range all {
		if it.UpdatedTime > maxMs {
			maxMs = it.UpdatedTime
		}
		if cursor != 0 && it.UpdatedTime < cursor {
			continue
		}
		resp.Items = append(resp.Items, it)
	}
	resp.Cursor = strconv.FormatInt(maxMs, 10)
	return json.Marshal(resp)
}

func (b *LocalFolderBackend) children() ([]byte, error) {
	all, err := b.listItems()
	if err != nil {
		return nil, err
	}
	resp := ChildrenResponse{}
	for _, it := range all {
		resp.Items = append(resp.Items, ChildItem{ID: it.ID, Name: it.ItemName})
	}
	return json.Marshal(resp)
}
