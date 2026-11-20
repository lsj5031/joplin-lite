package sync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lsj5031/joplin-lite/internal/e2ee"
	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/internal/store"
)

type targetSwitchChildrenBackend struct {
	*LocalFolderBackend
	unreadable string
}

var errTargetSwitchRead = errors.New("fixture: child content unavailable")

func (backend targetSwitchChildrenBackend) Get(path string) ([]byte, error) {
	switch {
	case strings.HasPrefix(path, "/api/items/root:/:/delta"):
		return []byte(`{"cursor":"children-only","items":[]}`), nil
	case path == backend.unreadable:
		return nil, errTargetSwitchRead
	default:
		return backend.LocalFolderBackend.Get(path)
	}
}

func TestTargetSwitchPreservesPendingNote(t *testing.T) {
	const base = int64(1772000000000)
	const noteID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const absentID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	const staleDeleteID = "cccccccccccccccccccccccccccccccc"
	const cleanID = "dddddddddddddddddddddddddddddddd"
	const password = "test-password"
	for _, test := range []struct {
		name         string
		childrenOnly bool
		encrypted    bool
		unreadable   bool
		locked       bool
		selectFirst  bool
	}{
		{name: "delta plaintext"},
		{name: "children plaintext", childrenOnly: true},
		{name: "edit after selecting B", selectFirst: true},
		{name: "delta encrypted", encrypted: true},
		{name: "children encrypted", childrenOnly: true, encrypted: true},
		{name: "delta locked then retry", encrypted: true, locked: true},
		{name: "children locked then retry", childrenOnly: true, encrypted: true, locked: true},
		{name: "children read failure then retry", childrenOnly: true, unreadable: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			profile, directoryA, directoryB := t.TempDir(), t.TempDir(), t.TempDir()
			db, err := store.Open(profile)
			must(err)
			t.Cleanup(func() { _ = db.Close() })
			remoteA, remoteB := NewLocalFolderBackend(directoryA), NewLocalFolderBackend(directoryB)
			writeNote := func(directory string, note models.Note) {
				t.Helper()
				must(os.WriteFile(filepath.Join(directory, note.ID+".md"), []byte(models.SerializeNote(&note)), 0o600))
			}
			original := models.Note{ID: noteID, Title: "Shared note", Body: "Previous body", UpdatedTime: base}
			writeNote(directoryA, original)
			writeNote(directoryA, models.Note{ID: absentID, Title: "Only in A", UpdatedTime: base})
			writeNote(directoryA, models.Note{ID: staleDeleteID, Title: "Old scope delete", UpdatedTime: base})
			writeNote(directoryA, models.Note{ID: cleanID, Title: "Clean note in A", UpdatedTime: base})
			must(NewEngine(remoteA, db, "").Sync(context.Background()))
			must(db.DeleteNote(staleDeleteID))
			if test.selectFirst {
				must(SelectTarget(db, remoteB))
			}

			local := original
			local.Body, local.UpdatedTime = "Pending local edit", base+1
			must(db.UpsertNote(&local))
			remote := original
			remote.Body, remote.UpdatedTime = "Different note in B", base-1
			remoteBytes := []byte(models.SerializeNote(&remote))
			var svc *e2ee.Service
			syncPassword := ""
			if test.encrypted {
				key := testMasterKey(t)
				svc = e2ee.NewService()
				must(svc.LoadMasterKey(key.ID, key.Content, password, key.EncryptionMethod))
				ciphertext, err := svc.EncryptString(key.ID, string(remoteBytes))
				must(err)
				remoteBytes = []byte(models.SerializeEncryptedEnvelope(envelopeMeta(models.NoteFields(&remote), ciphertext, key.ID)))
				info, err := json.Marshal(map[string]any{
					"version": 3, "e2ee": map[string]any{"value": true},
					"activeMasterKeyId": map[string]any{"value": key.ID}, "masterKeys": []any{key},
				})
				must(err)
				must(os.WriteFile(filepath.Join(directoryB, "info.json"), info, 0o600))
				syncPassword = password
			}
			remotePath := filepath.Join(directoryB, noteID+".md")
			must(os.WriteFile(remotePath, remoteBytes, 0o600))
			writeNote(directoryB, models.Note{ID: staleDeleteID, Title: "Must survive in B", UpdatedTime: base})
			writeNote(directoryB, models.Note{ID: cleanID, Title: "Clean note in B", UpdatedTime: base - 1})

			var backend SyncBackend = remoteB
			if test.childrenOnly {
				backend = targetSwitchChildrenBackend{LocalFolderBackend: remoteB}
			}
			if test.locked || test.unreadable {
				failedBackend, failedPassword := backend, syncPassword
				wantErr := errUncertainNoteConflict
				if test.locked {
					failedPassword = ""
				} else {
					wantErr = errTargetSwitchRead
					failedBackend = targetSwitchChildrenBackend{
						LocalFolderBackend: remoteB,
						unreadable:         fmt.Sprintf("/api/items/root:/%s.md:/content", noteID),
					}
				}
				err := NewEngine(failedBackend, db, failedPassword).Sync(context.Background())
				if !errors.Is(err, wantErr) {
					t.Fatalf("sync error = %v, want %v", err, wantErr)
				}
				got, err := db.GetNote(noteID)
				must(err)
				if got == nil || got.Body != local.Body || len(conflictNoteRows(t, db)) != 0 {
					t.Fatalf("failed sync changed the pending note: %+v", got)
				}
				syncPoint, err := db.GetSyncItem(noteID, remoteB.SyncTarget())
				must(err)
				if syncPoint != nil {
					t.Fatalf("failed sync recorded a sync point: %+v", syncPoint)
				}
				gotBytes, err := os.ReadFile(remotePath)
				must(err)
				if !bytes.Equal(gotBytes, remoteBytes) {
					t.Fatal("failed reconciliation overwrote the remote")
				}
				must(db.Close())
				reopened, err := store.Open(profile)
				must(err)
				db = reopened
			}
			must(NewEngine(backend, db, syncPassword).Sync(context.Background()))
			got, err := db.GetNote(noteID)
			must(err)
			if got == nil || got.Body != remote.Body {
				t.Fatalf("canonical note = %+v, want B's note", got)
			}
			conflicts := conflictNoteRows(t, db)
			if len(conflicts) != 1 || conflicts[0]["conflict_original_id"] != noteID || conflicts[0]["body"] != local.Body {
				t.Fatalf("pending edit was not saved in a single conflict copy: %+v", conflicts)
			}
			if test.encrypted {
				raw, err := os.ReadFile(filepath.Join(directoryB, conflicts[0]["id"]+".md"))
				must(err)
				envelope := models.DeserializeNote(string(raw))
				plain, err := svc.DecryptString(envelope.EncryptionCipherText)
				must(err)
				if envelope.EncryptionApplied != 1 || models.DeserializeNote(plain).Body != local.Body || bytes.Contains(raw, []byte(local.Body)) {
					t.Fatal("conflict copy was not uploaded with the pending edit encrypted")
				}
			}
			absent, err := db.GetNote(absentID)
			must(err)
			if absent == nil || absent.Title != "Only in A" {
				t.Fatal("B's listing pruned a note from A")
			}
			_, err = os.Stat(filepath.Join(directoryB, absentID+".md"))
			must(err)
			staleDelete, err := os.ReadFile(filepath.Join(directoryB, staleDeleteID+".md"))
			must(err)
			if models.DeserializeNote(string(staleDelete)).Title != "Must survive in B" {
				t.Fatal("A's queued delete affected B")
			}
			clean, err := db.GetNote(cleanID)
			must(err)
			if clean == nil || clean.Title != "Clean note in B" {
				t.Fatalf("clean collision = %+v, want B's note", clean)
			}
			must(NewEngine(backend, db, syncPassword).Sync(context.Background()))
			if got := len(conflictNoteRows(t, db)); got != 1 {
				t.Fatalf("repeated sync left %d conflict copies, want one", got)
			}
		})
	}
}

func TestTargetSwitchRejectsUnsafeNoteReplacement(t *testing.T) {
	const noteID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const otherID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	for _, fixture := range []struct {
		name       string
		isConflict int
		fileID     string
		contentID  string
		encrypted  bool
		wantErr    error
	}{
		{name: "dirty conflict note", isConflict: 1, fileID: noteID, contentID: noteID, wantErr: errTargetSwitchConflictNote},
		{name: "protected filename with mismatched content ID", fileID: noteID, contentID: otherID, wantErr: errUncertainNoteConflict},
		{name: "protected content ID with mismatched filename", fileID: otherID, contentID: noteID, wantErr: errUncertainNoteConflict},
		{name: "protected envelope with mismatched inner ID", fileID: noteID, contentID: otherID, encrypted: true, wantErr: errUncertainNoteConflict},
	} {
		for _, route := range []string{"delta", "children"} {
			t.Run(fixture.name+"/"+route, func(t *testing.T) {
				must := func(err error) {
					t.Helper()
					if err != nil {
						t.Fatal(err)
					}
				}
				db := testDB(t)
				remoteA := NewLocalFolderBackend(t.TempDir())
				remoteB := NewLocalFolderBackend(t.TempDir())
				original := models.Note{
					ID: noteID, Title: "Original note", Body: "Previous body",
					UpdatedTime: 1000, IsConflict: fixture.isConflict,
				}
				must(os.WriteFile(filepath.Join(remoteA.dir, noteID+".md"), []byte(models.SerializeNote(&original)), 0o600))
				must(NewEngine(remoteA, db, "").Sync(context.Background()))
				local := original
				local.Body, local.UpdatedTime = "Pending local edit", 2000
				must(db.UpsertNote(&local))

				remote := original
				remote.ID, remote.Body, remote.UpdatedTime = fixture.contentID, "Different remote body", 3000
				raw := []byte(models.SerializeNote(&remote))
				password := ""
				if fixture.encrypted {
					password = "test-password"
					key, svc := testMasterKey(t), e2ee.NewService()
					must(db.UpsertMasterKey(key))
					must(svc.LoadMasterKey(key.ID, key.Content, password, key.EncryptionMethod))
					ciphertext, err := svc.EncryptString(key.ID, string(raw))
					must(err)
					remote.ID = fixture.fileID
					raw = []byte(models.SerializeEncryptedEnvelope(envelopeMeta(models.NoteFields(&remote), ciphertext, key.ID)))
				}
				path := filepath.Join(remoteB.dir, fixture.fileID+".md")
				must(os.WriteFile(path, raw, 0o600))
				var backend SyncBackend = remoteB
				if route == "children" {
					backend = targetSwitchChildrenBackend{LocalFolderBackend: remoteB}
				}
				err := NewEngine(backend, db, password).Sync(context.Background())
				if !errors.Is(err, fixture.wantErr) {
					t.Fatalf("sync error = %v, want %v", err, fixture.wantErr)
				}
				got, err := db.GetNote(noteID)
				must(err)
				if got == nil || got.Body != local.Body || got.UpdatedTime != local.UpdatedTime || got.IsConflict != fixture.isConflict {
					t.Fatalf("unsafe replacement changed the pending note: %+v", got)
				}
				point, err := db.GetSyncItem(noteID, remoteB.SyncTarget())
				must(err)
				if point != nil {
					t.Fatalf("unsafe replacement recorded a sync point: %+v", point)
				}
				gotRemote, err := os.ReadFile(path)
				must(err)
				if !bytes.Equal(gotRemote, raw) {
					t.Fatal("unsafe replacement changed B's file")
				}
				if fixture.fileID != noteID {
					if _, err := os.Stat(filepath.Join(remoteB.dir, noteID+".md")); !os.IsNotExist(err) {
						t.Fatalf("unsafe replacement uploaded the pending note: stat error = %v", err)
					}
				}
			})
		}
	}
}
