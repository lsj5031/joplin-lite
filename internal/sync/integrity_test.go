package sync

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lsj5031/joplin-lite/internal/e2ee"
	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/internal/store"
)

func TestEncryptedNoteConflictIntegrity(t *testing.T) {
	const base = int64(1772000000000)
	key := testMasterKey(t)
	svc := e2ee.NewService()
	must := func(t *testing.T, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(t, svc.LoadMasterKey(key.ID, key.Content, "test-password", key.EncryptionMethod))
	for _, tc := range []struct {
		name        string
		localCipher bool
		change      func(*models.Note)
		fault       string
	}{
		{name: "same content, different timestamps"},
		{name: "same content, two authenticated randomized envelopes", localCipher: true},
		{name: "both encrypted with different content", localCipher: true, change: func(n *models.Note) { n.Body = "remote-body" }},
		{name: "title", change: func(n *models.Note) { n.Title = "remote-title" }},
		{name: "body", change: func(n *models.Note) { n.Body = "remote-body" }},
		{name: "folder", change: func(n *models.Note) { n.ParentID = "cccccccccccccccccccccccccccccccc" }},
		{name: "to-do", change: func(n *models.Note) { n.TodoCompleted = base + 1 }},
		{name: "trash", change: func(n *models.Note) { n.DeletedTime = base + 1 }},
		{name: "unknown metadata", fault: "unknown"},
		{name: "key unavailable", fault: "key"},
		{name: "corrupt cipher", fault: "corrupt"},
		{name: "authenticated but wrong identity", fault: "identity"},
		{name: "outer timestamp hides newer authenticated revision", fault: "timestamp"},
		{name: "authenticated but malformed item", fault: "malformed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, dir := testDB(t), t.TempDir()
			local := models.Note{
				ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ParentID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				Title: "synthetic-private-title", Body: "synthetic-private-body",
				CreatedTime: base, UserCreatedTime: base, UpdatedTime: base + 1, UserUpdatedTime: base + 1,
				IsTodo: 1, MarkupLanguage: 1, Type_: models.TypeNote,
			}
			envelope := func(note *models.Note, ciphertext string) string {
				return models.SerializeEncryptedEnvelope(envelopeMeta(models.NoteFields(note), ciphertext, key.ID))
			}
			if tc.localCipher {
				ct, err := svc.EncryptString(key.ID, models.SerializeNote(&local))
				must(t, err)
				wrapped := envelope(&local, ct)
				must(t, db.ApplyPulledItem(models.DeserializeNote(wrapped), 2, []byte(wrapped)))
			} else {
				must(t, db.CreateNote(&local))
			}
			must(t, db.UpsertSyncItemAt(local.ID, models.TypeNote, 2, base))
			remote := local
			remote.UpdatedTime, remote.UserUpdatedTime = base+2, base+2
			if tc.change != nil {
				tc.change(&remote)
			}
			inner := remote
			if tc.fault == "identity" {
				inner.ID = "dddddddddddddddddddddddddddddddd"
			}
			plaintext := models.SerializeNote(&inner)
			if tc.fault == "malformed" {
				plaintext = strings.Replace(plaintext, "is_todo: 1", "is_todo: not-a-number", 1)
			}
			if tc.fault == "unknown" {
				plaintext = strings.Replace(plaintext, "type_: 1", "is_locked: 1\ntype_: 1", 1)
			}
			ciphertext, err := svc.EncryptString(key.ID, plaintext)
			must(t, err)
			if tc.fault == "corrupt" {
				ciphertext = ciphertext[:len(ciphertext)-1]
			}
			outer := remote
			if tc.fault == "timestamp" {
				outer.UpdatedTime = base
			}
			content := envelope(&outer, ciphertext)
			path := filepath.Join(dir, local.ID+".md")
			must(t, os.WriteFile(path, []byte(content), 0o600))
			info, err := json.Marshal(map[string]any{
				"version": 3, "e2ee": map[string]any{"value": true},
				"activeMasterKeyId": map[string]any{"value": key.ID}, "masterKeys": []any{key},
			})
			must(t, err)
			must(t, os.WriteFile(filepath.Join(dir, "info.json"), info, 0o600))

			password := "test-password"
			if tc.fault == "key" {
				password = ""
			}
			err = NewEngine(NewLocalFolderBackend(dir), db, password).Sync(context.Background())
			got, getErr := db.GetNote(local.ID)
			must(t, getErr)
			conflicts := conflictNoteRows(t, db)
			if tc.fault != "" && tc.fault != "unknown" {
				if err == nil || strings.Contains(err.Error(), local.Body) || got == nil || *got != local || len(conflicts) != 0 {
					t.Fatalf("uncertain comparison must leave the local original and defer sync (err=%v, note=%+v, conflicts=%d)", err, got, len(conflicts))
				}
				after, err := os.ReadFile(path)
				must(t, err)
				if string(after) != content {
					t.Fatal("uncertain comparison changed the remote note")
				}
				return
			}
			must(t, err)
			if got == nil || *got != remote {
				t.Fatalf("canonical note does not match the remote revision: %+v", got)
			}
			if tc.change == nil && tc.fault == "" {
				if len(conflicts) != 0 {
					t.Fatalf("identical decrypted content made %d unnecessary conflict copies", len(conflicts))
				}
				return
			}
			if len(conflicts) != 1 || conflicts[0]["body"] != local.Body || conflicts[0]["parent_id"] != local.ParentID || conflicts[0]["conflict_original_id"] != local.ID {
				t.Fatalf("concurrent change did not preserve the local copy: %+v", conflicts)
			}
			uploaded, err := os.ReadFile(filepath.Join(dir, conflicts[0]["id"]+".md"))
			must(t, err)
			if !strings.Contains(string(uploaded), "encryption_applied: 1") || strings.Contains(string(uploaded), local.Body) || strings.Contains(string(uploaded), local.Title) {
				t.Fatal("conflict copy was not uploaded as an encrypted envelope")
			}
		})
	}
}

// Exercise the engine's provider switch without external accounts or network.
type alternateFolderTarget struct{ *LocalFolderBackend }

func (alternateFolderTarget) SyncTarget() int { return 9 }

func TestDeleteQueueTargetIntegrity(t *testing.T) {
	for _, scenario := range []string{"switch away and back", "queued before switching away", "same provider different directory", "legacy upgrade"} {
		t.Run(scenario, func(t *testing.T) {
			const stale = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			const current = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
			dbDir, a, b := t.TempDir(), t.TempDir(), t.TempDir()
			db, err := store.Open(dbDir)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			remoteA, remoteB := NewLocalFolderBackend(a), NewLocalFolderBackend(b)
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			syncWith := func(backend SyncBackend) {
				t.Helper()
				must(NewEngine(backend, db, "").Sync(context.Background()))
			}
			seed := func(dir, id string) {
				t.Helper()
				n := &models.Note{ID: id, Title: "fixture", UpdatedTime: 1772000000000}
				must(os.WriteFile(filepath.Join(dir, id+".md"), []byte(models.SerializeNote(n)), 0o600))
			}
			seed(a, stale)
			targetDir := a
			if scenario == "legacy upgrade" {
				// Reopen an actual v49-shaped queue with rows created before
				// bindings existed, including a separate pending ordinary edit.
				const edit = "dddddddddddddddddddddddddddddddd"
				seed(a, edit)
				must(db.CreateNote(&models.Note{ID: edit, Title: "Pending edit", UpdatedTime: 1772000000001}))
				must(db.UpsertSyncItemAt(edit, models.TypeNote, 2, 1772000000000))
				must(db.UpsertSyncItemAt(stale, models.TypeNote, 2, 1772000000000))
				must(db.SetSyncCursor("1"))
				_, err := db.Exec(`ALTER TABLE deleted_items DROP COLUMN jl_scope`)
				must(err)
				_, err = db.Exec(`INSERT INTO deleted_items (item_type, item_id, deleted_time, sync_target) VALUES (1, ?, 1772000000001, 2)`, stale)
				must(err)
				must(db.Close())
				db, err = store.Open(dbDir)
				must(err)
				syncWith(remoteA)
				raw, err := os.ReadFile(filepath.Join(a, edit+".md"))
				must(err)
				if models.DeserializeNote(string(raw)).Title != "Pending edit" {
					t.Fatal("default upgrade lost an ordinary pending edit")
				}
			} else {
				syncWith(remoteA)
				if scenario == "queued before switching away" {
					must(db.DeleteNote(stale))
				}
				if scenario == "switch away and back" || scenario == "queued before switching away" {
					syncWith(alternateFolderTarget{remoteB})
				}
				if scenario != "queued before switching away" {
					must(db.DeleteNote(stale))
				}
			}
			if scenario == "same provider different directory" {
				targetDir = b
				seed(b, stale)
				syncWith(remoteB)
			} else {
				syncWith(remoteA)
			}
			if _, err := os.Stat(filepath.Join(targetDir, stale+".md")); err != nil {
				t.Errorf("stale delete touched the remote note: %v", err)
			}
			queued, err := db.GetDeletedItems(2)
			must(err)
			if len(queued) != 1 || queued[0].ItemID != stale {
				t.Errorf("stale delete was not retained for recovery: %+v", queued)
			}

			seed(targetDir, current)
			active := NewLocalFolderBackend(targetDir)
			syncWith(active)
			must(db.DeleteNote(current))
			// A normal restart on the same confirmed remote keeps legitimate
			// deletes eligible, even next to an older held row.
			must(db.Close())
			db, err = store.Open(dbDir)
			must(err)
			syncWith(active)
			if _, err := os.Stat(filepath.Join(targetDir, current+".md")); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("ordinary current delete did not propagate: %v", err)
			}
			if _, err := os.Stat(filepath.Join(targetDir, stale+".md")); err != nil {
				t.Errorf("repeated sync replayed stale delete: %v", err)
			}
		})
	}
}
