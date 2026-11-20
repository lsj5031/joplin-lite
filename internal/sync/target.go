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

// SelectTarget selects the remote for sync bookkeeping and retains local note
// baselines across switches. A nil backend disables remote deletion.
// It does not contact the target.
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
	return b.deleteScopeForToken(b.token.Load())
}

func (b *DropboxBackend) deleteScopeForToken(token *DropboxToken) string {
	if token == nil {
		return ""
	}
	var app string
	if b.oauth != nil {
		app = b.oauth.AppKey
	}
	// A configured app key alone does not establish which app minted an
	// imported token. Use provider IDs only with app provenance on the token.
	if token.AppKey != "" && (app == "" || app == token.AppKey) {
		if token.AccountID != "" {
			return targetScope("dropbox", b.apiBase, b.contentBase, token.AppKey, "account", token.AccountID)
		}
		if token.UID != "" {
			return targetScope("dropbox", b.apiBase, b.contentBase, token.AppKey, "uid", token.UID)
		}
	}
	// Without proven identity, an unchanged grant (or legacy bearer) is
	// sufficient. Rotation of that credential does not carry delete eligibility.
	if token.RefreshToken != "" {
		return targetScope("dropbox", b.apiBase, b.contentBase, app, "refresh", token.RefreshToken)
	}
	if token.AccessToken != "" {
		return targetScope("dropbox", b.apiBase, b.contentBase, app, "access", token.AccessToken)
	}
	return ""
}
