package sync

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/internal/store"
)

// TestEngineLocalFolderRehearsal is the dry-run rehearsal for the Dropbox
// driver (M2): the full sync engine running against a local folder backend.
// Two clients share one target through create -> sync -> pull, edit -> sync ->
// pull, and delete -> sync -> prune.
//
// The delete leg runs end-to-end: DeleteNote queues deleted_items rows (the
// store's delete paths do this now), push turns them into remote deletes, and
// the other client prunes ("deletion propagates both ways" is an M2 gate).
func TestEngineLocalFolderRehearsal(t *testing.T) {
	remote := t.TempDir()
	openStore := func() *store.DB {
		db, err := store.Open(t.TempDir())
		if err != nil {
			t.Fatalf("store.Open: %v", err)
		}
		t.Cleanup(func() { db.Close() })
		return db
	}
	dbA, dbB := openStore(), openStore()
	engA := NewEngine(NewLocalFolderBackend(remote), dbA, "")
	engB := NewEngine(NewLocalFolderBackend(remote), dbB, "")
	ctx := context.Background()

	const (
		folderID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa1"
		noteID   = "0d28d893565c4b2989301f2259164273"
		base     = int64(1772000000000)
	)

	// --- Client A creates a folder and a note, then pushes.
	if err := dbA.CreateFolder(&models.Folder{
		ID: folderID, Title: "Rehearsal",
		CreatedTime: base, UpdatedTime: base,
		UserCreatedTime: base, UserUpdatedTime: base,
	}); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	note := &models.Note{
		ID: noteID, ParentID: folderID, Title: "Rehearsal note",
		Body: "original body", MarkupLanguage: 1,
		CreatedTime: base, UpdatedTime: base,
		UserCreatedTime: base, UserUpdatedTime: base,
	}
	if err := dbA.CreateNote(note); err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	if err := engA.Sync(ctx); err != nil {
		t.Fatalf("A sync (push): %v", err)
	}

	// The pushed file must be the canonical serialization.
	notePath := filepath.Join(remote, noteID+".md")
	got, err := os.ReadFile(notePath)
	if err != nil {
		t.Fatalf("pushed note file missing: %v", err)
	}
	if want := models.SerializeNote(note); string(got) != want {
		t.Fatalf("pushed bytes != canonical serialization:\n got: %q\nwant: %q", got, want)
	}

	// --- Client B pulls the change.
	if err := engB.Sync(ctx); err != nil {
		t.Fatalf("B sync (pull): %v", err)
	}
	gotNote, err := dbB.GetNote(noteID)
	if err != nil || gotNote == nil {
		t.Fatalf("B did not pull the note (note=%v err=%v)", gotNote, err)
	}
	if gotNote.Title != "Rehearsal note" || gotNote.Body != "original body" {
		t.Fatalf("B pulled wrong content: %+v", gotNote)
	}
	// Raw-passthrough: B kept the exact bytes pulled.
	raw, rawTime, ok, err := dbB.GetItemRaw(noteID)
	if err != nil || !ok {
		t.Fatalf("B has no raw copy (ok=%v err=%v)", ok, err)
	}
	if string(raw) != string(got) {
		t.Fatalf("B raw copy != pulled bytes")
	}
	if rawTime != base {
		t.Fatalf("B raw timestamp = %d, want %d", rawTime, base)
	}

	// --- Client B edits the note; the edit reaches A.
	gotNote.Body = "edited during rehearsal"
	gotNote.UpdatedTime = base + 5000
	if err := dbB.UpdateNote(gotNote); err != nil {
		t.Fatalf("B UpdateNote: %v", err)
	}
	if err := engB.Sync(ctx); err != nil {
		t.Fatalf("B sync (push edit): %v", err)
	}
	got, err = os.ReadFile(notePath)
	if err != nil || !strings.Contains(string(got), "edited during rehearsal") {
		t.Fatalf("edit not pushed to target: %v\n%s", err, got)
	}
	if err := engA.Sync(ctx); err != nil {
		t.Fatalf("A sync (pull edit): %v", err)
	}
	gotNote, err = dbA.GetNote(noteID)
	if err != nil || gotNote == nil || gotNote.Body != "edited during rehearsal" {
		t.Fatalf("A did not receive the edit (note=%v err=%v)", gotNote, err)
	}

	// --- Client A deletes the note; it disappears from the target and from B.
	if err := dbA.DeleteNote(noteID); err != nil {
		t.Fatalf("A DeleteNote: %v", err)
	}
	if err := engA.Sync(ctx); err != nil {
		t.Fatalf("A sync (push delete): %v", err)
	}
	if _, err := os.Stat(notePath); !os.IsNotExist(err) {
		t.Fatalf("note file still on target after delete (stat err=%v)", err)
	}
	if err := engB.Sync(ctx); err != nil {
		t.Fatalf("B sync (prune): %v", err)
	}
	gotNote, err = dbB.GetNote(noteID)
	if err != nil {
		t.Fatalf("B GetNote after prune: %v", err)
	}
	if gotNote != nil {
		t.Fatalf("B still has the deleted note: %+v", gotNote)
	}

	// --- info.json master keys reach the client through the sync loop.
	info := `{"version":3,"e2ee":{"value":true,"updatedTime":1772000000000},` +
		`"activeMasterKeyId":{"value":"ffffffffffffffffffffffffffffffff","updatedTime":1772000000000},` +
		`"masterKeys":[{"id":"ffffffffffffffffffffffffffffffff","created_time":1772000000000,` +
		`"updated_time":1772000000000,"source_application":"joplin-desktop","encryption_method":8,` +
		`"checksum":"0123","content":"JED01fake"}],"appMinVersion":"3.0.0"}`
	if err := os.WriteFile(filepath.Join(remote, "info.json"), []byte(info), 0o644); err != nil {
		t.Fatalf("write info.json: %v", err)
	}
	if err := engB.Sync(ctx); err != nil {
		if !strings.Contains(err.Error(), "unlock the active encryption key") {
			t.Fatalf("B sync (info.json): %v", err)
		}
	} else {
		t.Fatal("sync accepted an encrypted target without an unlocked key")
	}
	active, err := dbB.GetActiveMasterKeyID()
	if err != nil || active != "ffffffffffffffffffffffffffffffff" {
		t.Fatalf("B active master key = %q (err=%v), want ffff...", active, err)
	}
}

func TestEngineRefusesWritesWithoutEncryptionKey(t *testing.T) {
	for _, scenario := range []string{"missing-password", "wrong-password", "missing-active-key", "invalid-info"} {
		t.Run(scenario, func(t *testing.T) {
			db := infoTestDB(t)
			remote := t.TempDir()
			key := testMasterKey(t)
			active := key.ID
			if scenario == "missing-active-key" {
				active = ""
				if err := db.SetActiveMasterKeyID(key.ID); err != nil {
					t.Fatal(err)
				}
			}
			info, err := json.Marshal(map[string]any{
				"version": 3, "e2ee": map[string]any{"value": true},
				"activeMasterKeyId": map[string]any{"value": active}, "masterKeys": []any{key},
			})
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "invalid-info" {
				info = []byte("{")
			}
			if err := os.WriteFile(filepath.Join(remote, "info.json"), info, 0o600); err != nil {
				t.Fatal(err)
			}
			local := &models.Note{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Title: "Local", Body: "must never upload in plaintext"}
			if err := db.CreateNote(local); err != nil {
				t.Fatal(err)
			}
			remoteNote := &models.Note{ID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Title: "Keep remotely", Body: "preserved"}
			remoteBody := models.SerializeNote(remoteNote)
			remotePath := filepath.Join(remote, remoteNote.ID+".md")
			if err := os.WriteFile(remotePath, []byte(remoteBody), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := db.AddDeletedItem(remoteNote.ID, models.TypeNote, 2); err != nil {
				t.Fatal(err)
			}
			password := ""
			if scenario == "wrong-password" {
				password = "wrong-password"
			} else if scenario == "missing-active-key" {
				password = "test-password"
			}
			if err := NewEngine(NewLocalFolderBackend(remote), db, password).Sync(context.Background()); err == nil {
				t.Fatal("sync did not refuse unsafe writes")
			}
			if got, err := os.ReadFile(remotePath); err != nil || string(got) != remoteBody {
				t.Fatalf("remote note was removed or modified: %v", err)
			}
			if got, err := os.ReadFile(filepath.Join(remote, "info.json")); err != nil || string(got) != string(info) {
				t.Fatalf("remote encryption state was modified: %v", err)
			}
			if _, err := os.Stat(filepath.Join(remote, local.ID+".md")); !os.IsNotExist(err) {
				t.Fatalf("local plaintext was uploaded: %v", err)
			}
		})
	}
}
