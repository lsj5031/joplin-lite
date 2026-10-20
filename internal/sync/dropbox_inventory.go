package sync

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
)

// The cursor and complete inventory commit together, only after all listing
// pages succeed. Pending survives until PullChanges commits applyCursor, so
// advancing the listing never acknowledges an unfinished download/apply.
type dropboxInventory struct {
	Cursor  string                  `json:"cursor"`
	Entries map[string]dropboxEntry `json:"entries"`
	Pending map[string]bool         `json:"pending"`
}

func (e dropboxEntry) path() string {
	if e.PathLower != "" {
		return e.PathLower
	}
	return strings.ToLower(e.PathDisplay)
}

func (s *dropboxInventory) applyCursor(scope string) string {
	return fmt.Sprintf("%s:%x", scope, sha256.Sum256([]byte(s.Cursor)))
}

func (b *DropboxBackend) listInventory() ([]dropboxEntry, error) {
	started := time.Now()
	scope := b.checkpointScope()
	key := "sync.dropbox.inventory.v1." + scope
	raw, err := b.db.GetSetting(key)
	if err != nil {
		return nil, fmt.Errorf("cannot read Dropbox inventory: %w", err)
	}
	var state dropboxInventory
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &state); err != nil {
			slog.Warn("invalid saved Dropbox inventory; rebuilding")
			state = dropboxInventory{}
		}
	}
	incremental := state.Cursor != "" && state.Entries != nil && state.Pending != nil
	if !incremental {
		state = dropboxInventory{Entries: make(map[string]dropboxEntry)}
	}
	if state.Pending == nil {
		state.Pending = make(map[string]bool)
	}
	applied, err := b.db.GetSyncCursor()
	if err != nil {
		return nil, err
	}
	if incremental && applied == state.applyCursor(scope) {
		state.Pending = make(map[string]bool)
	}
	arg := map[string]any{"path": "", "recursive": true, "limit": 500}
	endpoint := "/2/files/list_folder"
	if incremental {
		arg = map[string]any{"cursor": state.Cursor}
		endpoint = "/2/files/list_folder/continue"
	}
	pages := 0
	for {
		var resp listFolderResponse
		if err := b.apiJSON(b.ctx, endpoint, arg, &resp); err != nil {
			if incremental && errors.Is(err, errDropboxCursorReset) {
				// A reset invalidates the entire base, not just its cursor.
				state = dropboxInventory{Entries: make(map[string]dropboxEntry), Pending: make(map[string]bool)}
				incremental = false
				arg = map[string]any{"path": "", "recursive": true, "limit": 500}
				endpoint = "/2/files/list_folder"
				continue
			}
			return nil, err
		}
		pages++
		if resp.Cursor == "" {
			return nil, fmt.Errorf("Dropbox listing returned an empty cursor")
		}
		for _, entry := range resp.Entries {
			path := entry.path()
			if path == "" || path == "/" {
				return nil, fmt.Errorf("Dropbox listing entry has no item path")
			}
			switch entry.Tag {
			case "file":
				// A file can replace a directory at this path.
				if state.Entries[path].Tag == "folder" {
					for existing := range state.Entries {
						if strings.HasPrefix(existing, path+"/") {
							delete(state.Entries, existing)
							delete(state.Pending, existing)
						}
					}
				}
				state.Entries[path] = entry
				state.Pending[path] = true
			case "folder":
				state.Entries[path] = entry
				delete(state.Pending, path)
			case "deleted":
				// Dropbox can delete a directory without enumerating its files.
				for existing := range state.Entries {
					if existing == path || strings.HasPrefix(existing, path+"/") {
						delete(state.Entries, existing)
						delete(state.Pending, existing)
					}
				}
			default:
				return nil, fmt.Errorf("Dropbox listing returned an unknown entry type")
			}
		}
		state.Cursor = resp.Cursor
		if !resp.HasMore {
			break
		}
		arg = map[string]any{"cursor": resp.Cursor}
		endpoint = "/2/files/list_folder/continue"
	}
	if err := b.ctx.Err(); err != nil {
		return nil, err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	if err := b.db.SetSetting(key, string(data)); err != nil {
		return nil, fmt.Errorf("cannot save complete Dropbox inventory: %w", err)
	}
	// Freeze the pass's view before concurrent downloads and reconciliation.
	paths := make([]string, 0, len(state.Entries))
	for path := range state.Entries {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	entries := make([]dropboxEntry, 0, len(paths))
	for _, path := range paths {
		entries = append(entries, state.Entries[path])
	}
	b.inventory = &state
	b.rootEntries, b.rootListed = entries, true
	slog.Info("Dropbox inventory listed", "entries", len(entries), "pages", pages, "incremental", incremental, "duration", time.Since(started))
	return entries, nil
}
