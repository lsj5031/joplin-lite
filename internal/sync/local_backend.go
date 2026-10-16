package sync

import (
	"encoding/json"
	"fmt"
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
	dir         string
	clientID    string
	authenticated bool
}

// NewLocalFolderBackend creates a backend rooted at dir.
func NewLocalFolderBackend(dir string) *LocalFolderBackend {
	return &LocalFolderBackend{dir: dir, clientID: "joplin-lite"}
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

// AcquireLock writes the flat-file lock Joplin uses on file targets
// (locks/<type>_<clientType>_<clientId>.json). TTL/refresh semantics are M2
// work shared with the Dropbox driver.
func (b *LocalFolderBackend) AcquireLock() (interface{}, error) {
	lock := &Lock{Type: 1, ClientType: 3, ClientID: b.clientID}
	data, err := json.Marshal(lock)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(b.dir, "locks"), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(b.lockPath(lock), data, 0o644); err != nil {
		return nil, err
	}
	return lock, nil
}

func (b *LocalFolderBackend) ReleaseLock(lock interface{}) error {
	l, _ := lock.(*Lock)
	if l == nil {
		return nil
	}
	_ = os.Remove(b.lockPath(l))
	return nil
}

func (b *LocalFolderBackend) lockPath(l *Lock) string {
	return filepath.Join(b.dir, "locks", fmt.Sprintf("%d_%d_%s.json", l.Type, l.ClientType, l.ClientID))
}

// itemNameFromPath extracts the item name from a Joplin-Server-style content
// path: /api/items/root:/<name>:/content. Names may contain "/" (the resource
// blobs live under .resource/<id>).
func localItemName(path string) (string, error) {
	const prefix = "/api/items/root:/"
	const suffix = ":/content"
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return "", fmt.Errorf("unsupported path %q", path)
	}
	return strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix), nil
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
