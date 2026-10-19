package sync

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/lsj5031/joplin-lite/internal/e2ee"
	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/internal/store"
)

// infoBackend is a minimal SyncBackend whose only item is info.json.
type infoBackend struct {
	remote []byte
	puts   [][]byte
}

func (b *infoBackend) Authenticate() error               { return nil }
func (b *infoBackend) IsAuthenticated() bool             { return true }
func (b *infoBackend) AcquireLock() (interface{}, error) { return nil, nil }
func (b *infoBackend) ReleaseLock(interface{}) error     { return nil }
func (b *infoBackend) SyncTarget() int                   { return 7 }
func (b *infoBackend) Get(path string) ([]byte, error) {
	if strings.Contains(path, "info.json") {
		return b.remote, nil
	}
	return nil, fmt.Errorf("unexpected get: %s", path)
}
func (b *infoBackend) Put(path string, content []byte) error {
	if !strings.Contains(path, "info.json") {
		return fmt.Errorf("unexpected put: %s", path)
	}
	b.puts = append(b.puts, append([]byte(nil), content...))
	b.remote = append([]byte(nil), content...)
	return nil
}
func (b *infoBackend) Delete(path string) error { return nil }

func testMasterKey(t *testing.T) *models.MasterKey {
	t.Helper()
	// 256-byte key 0x00..0xff under password "test-password", KeyV1 packet
	// {"salt","iv","ct"} (same shape the e2ee tests use).
	packet := buildTestKeyV1Packet(t, "test-password")
	return &models.MasterKey{
		ID:                "0123456789abcdef0123456789abcdef",
		CreatedTime:       1772000000000,
		UpdatedTime:       1772000000000,
		SourceApplication: "joplin-lite",
		EncryptionMethod:  e2ee.MethodKeyV1,
		Content:           packet,
	}
}

func infoTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestPullSyncInfo_VersionEnforcement(t *testing.T) {
	db := infoTestDB(t)

	tests := []struct {
		name        string
		info        string
		wantRefusal bool
		wantErr     bool
	}{
		{
			name:        "supported version",
			info:        `{"version":3,"e2ee":{"value":true,"updatedTime":1},"activeMasterKeyId":{"value":"0123456789abcdef0123456789abcdef"},"masterKeys":[],"appMinVersion":"3.7.0"}`,
			wantRefusal: false,
		},
		{
			name:        "older appMinVersion",
			info:        `{"version":3,"masterKeys":[],"appMinVersion":"3.0.0"}`,
			wantRefusal: false,
		},
		{
			name:        "newer version refused",
			info:        `{"version":4,"masterKeys":[],"appMinVersion":"3.7.0"}`,
			wantRefusal: true,
		},
		{
			name:        "newer appMinVersion refused",
			info:        `{"version":3,"masterKeys":[],"appMinVersion":"9.9.9"}`,
			wantRefusal: true,
		},
		{
			name:        "missing version field is invalid",
			info:        `{"masterKeys":[],"appMinVersion":"3.7.0"}`,
			wantRefusal: false,
			wantErr:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &infoBackend{remote: []byte(tt.info)}
			err := PullSyncInfo(b, db)
			switch {
			case tt.wantRefusal:
				if !errors.Is(err, ErrSyncTargetUnsupported) {
					t.Fatalf("PullSyncInfo = %v, want ErrSyncTargetUnsupported", err)
				}
			case tt.wantErr:
				if err == nil {
					t.Fatal("PullSyncInfo = nil, want error")
				}
			default:
				if err != nil {
					t.Fatalf("PullSyncInfo: %v", err)
				}
			}
		})
	}
}

func TestPullSyncInfo_StoresMasterKeys(t *testing.T) {
	db := infoTestDB(t)
	mk := testMasterKey(t)
	raw, err := json.Marshal(map[string]any{
		"version":           3,
		"e2ee":              map[string]any{"value": true, "updatedTime": 1772000000000},
		"activeMasterKeyId": map[string]any{"value": mk.ID, "updatedTime": 1772000000000},
		"masterKeys":        []any{mk},
		"appMinVersion":     "3.7.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	b := &infoBackend{remote: raw}
	if err := PullSyncInfo(b, db); err != nil {
		t.Fatalf("PullSyncInfo: %v", err)
	}
	got, err := db.ListMasterKeys()
	if err != nil || len(got) != 1 || got[0].ID != mk.ID || got[0].Content != mk.Content {
		t.Fatalf("master keys = %+v (err=%v), want the one from info.json", got, err)
	}
	active, err := db.GetActiveMasterKeyID()
	if err != nil || active != mk.ID {
		t.Fatalf("active master key = %q (err=%v), want %q", active, err, mk.ID)
	}
}

func TestPushSyncInfo_WritesFreshTarget(t *testing.T) {
	db := infoTestDB(t)
	mk := testMasterKey(t)
	if err := db.UpsertMasterKey(mk); err != nil {
		t.Fatalf("UpsertMasterKey: %v", err)
	}
	if err := db.SetActiveMasterKeyID(mk.ID); err != nil {
		t.Fatalf("SetActiveMasterKeyID: %v", err)
	}

	b := &infoBackend{}
	if err := PushSyncInfo(b, db); err != nil {
		t.Fatalf("PushSyncInfo: %v", err)
	}
	if len(b.puts) != 1 {
		t.Fatalf("puts = %d, want 1", len(b.puts))
	}

	var info map[string]json.RawMessage
	if err := json.Unmarshal(b.puts[0], &info); err != nil {
		t.Fatalf("written info.json is not JSON: %v", err)
	}
	var version int
	json.Unmarshal(info["version"], &version)
	if version != SyncInfoVersion {
		t.Errorf("version = %d, want %d", version, SyncInfoVersion)
	}
	var appMin string
	json.Unmarshal(info["appMinVersion"], &appMin)
	if appMin != AppMinVersion {
		t.Errorf("appMinVersion = %q, want %q", appMin, AppMinVersion)
	}
	// e2ee must be a JSON *boolean* value like Joplin's SyncInfoValueBoolean.
	var e2eeVal syncInfoValue[bool]
	if err := json.Unmarshal(info["e2ee"], &e2eeVal); err != nil || !e2eeVal.Value {
		t.Errorf("e2ee = %s (err=%v), want {value:true}", info["e2ee"], err)
	}
	var active syncInfoValue[string]
	if err := json.Unmarshal(info["activeMasterKeyId"], &active); err != nil || active.Value != mk.ID {
		t.Errorf("activeMasterKeyId = %s (err=%v), want %q", info["activeMasterKeyId"], err, mk.ID)
	}
	var keys []map[string]json.RawMessage
	if err := json.Unmarshal(info["masterKeys"], &keys); err != nil || len(keys) != 1 {
		t.Fatalf("masterKeys = %s (err=%v), want 1 key", info["masterKeys"], err)
	}

	// Idempotent: a second push writes nothing.
	if err := PushSyncInfo(b, db); err != nil {
		t.Fatalf("PushSyncInfo (2nd): %v", err)
	}
	if len(b.puts) != 1 {
		t.Fatalf("second PushSyncInfo wrote again (%d puts)", len(b.puts))
	}
}

func TestPushSyncInfo_PreservesRemoteState(t *testing.T) {
	db := infoTestDB(t)
	local := testMasterKey(t)
	if err := db.UpsertMasterKey(local); err != nil {
		t.Fatalf("UpsertMasterKey: %v", err)
	}

	// A Joplin client wrote info.json: it carries fields our model doesn't
	// know (ppk, noteLockKey, revisionService*, hasBeenUsed on keys) and its
	// own e2ee/activeMasterKeyId. None of that may be lost or clobbered.
	remote := `{
	"version": 3,
	"e2ee": {"value": true, "updatedTime": 1772000000000},
	"activeMasterKeyId": {"value": "ffffffffffffffffffffffffffffffff", "updatedTime": 1772000000000},
	"masterKeys": [{"id": "ffffffffffffffffffffffffffffffff", "updated_time": 1772000000000, "content": "JED01remote", "hasBeenUsed": true}],
	"noteLockKey": {"id": "note", "content": "x"},
	"ppk": {"value": {"keySize": 2048}, "updatedTime": 1},
	"revisionServiceEnabled": {"value": true, "updatedTime": 1},
	"appMinVersion": "3.7.0"
	}`
	b := &infoBackend{remote: []byte(remote)}
	if err := PushSyncInfo(b, db); err != nil {
		t.Fatalf("PushSyncInfo: %v", err)
	}
	if len(b.puts) != 1 {
		t.Fatalf("puts = %d, want 1 (local key missing on target)", len(b.puts))
	}

	var info map[string]json.RawMessage
	if err := json.Unmarshal(b.puts[0], &info); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"ppk", "noteLockKey", "revisionServiceEnabled"} {
		if _, ok := info[key]; !ok {
			t.Errorf("%s dropped from info.json", key)
		}
	}
	// The remote active key stays — our local value has no change timestamp
	// and must not clobber it.
	var active syncInfoValue[string]
	json.Unmarshal(info["activeMasterKeyId"], &active)
	if active.Value != "ffffffffffffffffffffffffffffffff" {
		t.Errorf("activeMasterKeyId = %q, want the remote value kept", active.Value)
	}
	// Remote key entry preserved byte-identical (hasBeenUsed etc. survive);
	// our local key is added alongside it.
	var keys []map[string]json.RawMessage
	if err := json.Unmarshal(info["masterKeys"], &keys); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 {
		t.Fatalf("masterKeys = %d entries, want 2", len(keys))
	}
	var remoteKept bool
	for _, k := range keys {
		var id string
		json.Unmarshal(k["id"], &id)
		if id == "ffffffffffffffffffffffffffffffff" {
			remoteKept = string(k["hasBeenUsed"]) == "true" && string(k["content"]) == `"JED01remote"`
		}
	}
	if !remoteKept {
		t.Errorf("remote master key entry was altered: %v", keys)
	}
}
