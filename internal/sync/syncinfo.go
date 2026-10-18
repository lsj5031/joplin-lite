package sync

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/internal/store"
)

// SyncInfo represents Joplin's info.json file stored at the sync target root.
// Master keys are stored here, NOT as individual .md items.
type SyncInfo struct {
	Version           int                   `json:"version"`
	E2EE              syncInfoValue[bool]   `json:"e2ee"`
	ActiveMasterKeyID syncInfoValue[string] `json:"activeMasterKeyId"`
	MasterKeys        []models.MasterKey    `json:"masterKeys"`
	AppMinVersion     string                `json:"appMinVersion"`
}

type syncInfoValue[T any] struct {
	Value       T     `json:"value"`
	UpdatedTime int64 `json:"updatedTime"`
}

const (
	// SyncInfoVersion is the sync target format version we speak — Joplin's
	// hidden "syncVersion" setting (currently 3).
	SyncInfoVersion = 3
	// AppMinVersion is the format floor we publish in info.json: the Joplin
	// sync format this client was ported from (3.7.0).
	AppMinVersion = "3.7.0"
	// infoPath is the engine path of info.json on the target.
	infoPath = "/api/items/root:/info.json:/content"
)

// ErrSyncTargetUnsupported reports that the sync target uses a format newer
// than this client supports; syncing must be refused (Joplin refuses when
// info.json version > syncVersion, or appMinVersion > the app version).
var ErrSyncTargetUnsupported = errors.New("sync target format is newer than this client supports")

// PullSyncInfo fetches info.json from the sync target, enforces the target
// version, and stores any master keys found in the local database.
func PullSyncInfo(backend SyncBackend, db *store.DB) error {
	data, err := backend.Get(infoPath)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("cannot fetch info.json: %w", err)
	}
	if data == nil || len(data) == 0 {
		// info.json may not exist yet (e.g. fresh target)
		return nil
	}

	var info SyncInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return fmt.Errorf("cannot parse info.json: %w", err)
	}

	if info.Version == 0 {
		return fmt.Errorf("invalid info.json: missing \"version\" field")
	}
	if info.Version > SyncInfoVersion {
		return fmt.Errorf("%w: info.json version %d (supported %d)", ErrSyncTargetUnsupported, info.Version, SyncInfoVersion)
	}
	if info.AppMinVersion != "" && compareVersionStrings(info.AppMinVersion, AppMinVersion) > 0 {
		return fmt.Errorf("%w: sync target requires app version %s (supported %s)", ErrSyncTargetUnsupported, info.AppMinVersion, AppMinVersion)
	}

	slog.Info("loaded sync info", "version", info.Version, "e2ee", info.E2EE.Value,
		"master_keys", len(info.MasterKeys), "active_master_key", info.ActiveMasterKeyID.Value)

	for _, mk := range info.MasterKeys {
		if mk.ID == "" {
			continue
		}
		if err := db.UpsertMasterKey(&mk); err != nil {
			slog.Error("cannot upsert master key from info.json", "id", mk.ID, "error", err)
			continue
		}
		slog.Info("stored master key from info.json", "id", mk.ID)
	}

	if info.ActiveMasterKeyID.Value != "" {
		if err := db.SetActiveMasterKeyID(info.ActiveMasterKeyID.Value); err != nil {
			slog.Warn("cannot store active master key id", "error", err)
		}
	}

	return nil
}

// PushSyncInfo writes info.json when local state (master keys, active master
// key, e2ee flag) is not reflected on the target. The merge follows Joplin's
// syncInfoUtils: unknown fields are preserved verbatim (ppk, noteLockKey,
// revisionService*), remote master key entries win on id collision (they may
// carry fields our model doesn't know), and values we don't track a change
// timestamp for never override a remote value — the remote's e2ee flag or
// active key is only filled in when the target has none.
func PushSyncInfo(backend SyncBackend, db *store.DB) error {
	remoteRaw, err := backend.Get(infoPath)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			return fmt.Errorf("cannot fetch info.json: %w", err)
		}
		remoteRaw = nil // fresh target: no info.json yet
	}

	merged := map[string]json.RawMessage{}
	if len(remoteRaw) > 0 {
		if err := json.Unmarshal(remoteRaw, &merged); err != nil {
			return fmt.Errorf("cannot parse info.json: %w", err)
		}
	}
	changed := false

	// version: keep the target's (newer ones were refused on pull), else ours.
	if _, ok := merged["version"]; !ok {
		if v, err := json.Marshal(SyncInfoVersion); err == nil {
			merged["version"] = v
			changed = true
		}
	}

	// masterKeys: union by id, remote entries win on collision.
	var remoteKeys []map[string]json.RawMessage
	if raw, ok := merged["masterKeys"]; ok {
		if err := json.Unmarshal(raw, &remoteKeys); err != nil {
			return fmt.Errorf("cannot parse info.json masterKeys: %w", err)
		}
	}
	known := make(map[string]bool)
	for _, mk := range remoteKeys {
		var id string
		if err := json.Unmarshal(mk["id"], &id); err == nil && id != "" {
			known[id] = true
		}
	}
	localKeys, err := db.ListMasterKeys()
	if err != nil {
		return fmt.Errorf("cannot list master keys: %w", err)
	}
	keysChanged := false
	for i := range localKeys {
		mk := localKeys[i]
		if mk.ID == "" || known[mk.ID] {
			continue
		}
		raw, err := json.Marshal(mk)
		if err != nil {
			return err
		}
		var entry map[string]json.RawMessage
		if err := json.Unmarshal(raw, &entry); err != nil {
			return err
		}
		remoteKeys = append(remoteKeys, entry)
		known[mk.ID] = true
		keysChanged = true
		slog.Info("adding master key to info.json", "id", mk.ID)
	}
	if keysChanged {
		raw, err := json.Marshal(remoteKeys)
		if err != nil {
			return err
		}
		merged["masterKeys"] = raw
		changed = true
	}

	// e2ee / activeMasterKeyId: fill in only when the target has none — we
	// have no change timestamp for our local value, and a stale local value
	// must never clobber the target's (Joplin mergeSyncInfos prefers the
	// newer updatedTime; "no timestamp" loses).
	activeKeyID, err := db.GetActiveMasterKeyID()
	if err != nil {
		return err
	}
	if activeKeyID != "" {
		var remote syncInfoValue[string]
		done := false
		if raw, ok := merged["activeMasterKeyId"]; ok {
			if err := json.Unmarshal(raw, &remote); err == nil && remote.Value != "" {
				done = true // target already has a value — keep it
			}
		}
		if !done {
			raw, err := json.Marshal(syncInfoValue[string]{Value: activeKeyID, UpdatedTime: time.Now().UnixMilli()})
			if err != nil {
				return err
			}
			merged["activeMasterKeyId"] = raw
			changed = true
		}

		var remoteE2EE syncInfoValue[bool]
		done = false
		if raw, ok := merged["e2ee"]; ok {
			if err := json.Unmarshal(raw, &remoteE2EE); err == nil && remoteE2EE.Value {
				done = true
			}
		}
		if !done {
			raw, err := json.Marshal(syncInfoValue[bool]{Value: true, UpdatedTime: time.Now().UnixMilli()})
			if err != nil {
				return err
			}
			merged["e2ee"] = raw
			changed = true
		}
	}

	// appMinVersion: publish our format floor on a fresh target. An existing
	// value is left alone (Joplin bumps it when its own floor is higher).
	if _, ok := merged["appMinVersion"]; !ok {
		raw, err := json.Marshal(AppMinVersion)
		if err != nil {
			return err
		}
		merged["appMinVersion"] = raw
		changed = true
	}

	if !changed {
		return nil
	}
	out, err := json.MarshalIndent(merged, "", "\t")
	if err != nil {
		return err
	}
	if err := backend.Put(infoPath, out); err != nil {
		return fmt.Errorf("cannot write info.json: %w", err)
	}
	slog.Info("wrote sync info", "master_keys", len(remoteKeys), "active_master_key", activeKeyID)
	return nil
}

// compareVersionStrings compares dotted numeric versions ("3.7.0" style).
// Returns -1, 0 or 1. Missing parts count as 0.
func compareVersionStrings(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		na, nb := 0, 0
		if i < len(pa) {
			na, _ = strconv.Atoi(strings.TrimSpace(pa[i]))
		}
		if i < len(pb) {
			nb, _ = strconv.Atoi(strings.TrimSpace(pb[i]))
		}
		if na < nb {
			return -1
		}
		if na > nb {
			return 1
		}
	}
	return 0
}
