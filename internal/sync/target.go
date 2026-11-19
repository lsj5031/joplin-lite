package sync

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/lsj5031/joplin-lite/internal/store"
)

func targetScope(parts ...string) string {
	raw, _ := json.Marshal(parts)
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

func deleteScope(backend SyncBackend) string {
	if b, ok := backend.(interface{ remoteDeleteScope() string }); ok {
		return b.remoteDeleteScope()
	}
	return "" // Never authorize deletes for an unidentified backend.
}

// SelectTarget records a change of remote, or disables remote deletion when
// backend is nil. It does not contact the target.
func SelectTarget(db *store.DB, backend SyncBackend) error {
	var target int
	if backend != nil {
		target = backend.SyncTarget()
	}
	n, err := db.SelectDeleteTarget(target, deleteScope(backend))
	if err != nil {
		return fmt.Errorf("cannot select sync target: %w", err)
	}
	if n > 0 {
		slog.Warn("pending remote deletes held after target change", "count", n)
	}
	return nil
}

func (t *tracedBackend) remoteDeleteScope() string { return deleteScope(t.inner) }

func (b *LocalFolderBackend) remoteDeleteScope() string {
	path, err := filepath.Abs(b.dir)
	if err != nil {
		return ""
	}
	// Resolve the actual root, not just the configured alias: a symlink may
	// have been redirected to another sync directory.
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return ""
	}
	return targetScope("folder", path)
}

func (c *Client) remoteDeleteScope() string {
	return targetScope("joplin-server", c.baseURL, c.username)
}

func (b *DropboxBackend) remoteDeleteScope() string {
	token := b.token
	if token == nil {
		return ""
	}
	// A refresh grant covers one account and app folder, and remains stable
	// when the access token rotates. Without a grant a changed access token
	// is deliberately treated as an uncertain/new remote.
	grant := token.RefreshToken
	if grant == "" {
		grant = token.AccessToken
	}
	if grant == "" {
		return ""
	}
	var app string
	if b.oauth != nil {
		app = b.oauth.AppKey
	}
	return targetScope("dropbox", b.apiBase, b.contentBase, app, grant)
}
