package sync

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/internal/store"
)

// The conflict-note pass (never lose data) through the same local-folder
// rehearsal target as engine_local_test.go. Joplin's convention, verified
// against packages/lib (Note.createConflictNote, Resource.
// createConflictResourceNote): the local version is kept as a conflict note
// (new id, is_conflict=1, conflict_original_id=<original>, title and parent
// unchanged) and the remote version becomes canonical under the original id.

// tick advances the wall clock past the millisecond stamps UpdateNote and the
// sync bookkeeping use, so "modified after the last sync" is deterministic.
func tick() { time.Sleep(3 * time.Millisecond) }

func conflictNoteRows(t *testing.T, db *store.DB) []map[string]string {
	t.Helper()
	rows, err := db.Query(`SELECT id, title, body, parent_id, conflict_original_id
		FROM notes WHERE is_conflict = 1`)
	if err != nil {
		t.Fatalf("query conflict notes: %v", err)
	}
	defer rows.Close()
	var out []map[string]string
	for rows.Next() {
		var id, title, body, parentID, origID string
		if err := rows.Scan(&id, &title, &body, &parentID, &origID); err != nil {
			t.Fatalf("scan conflict note: %v", err)
		}
		out = append(out, map[string]string{
			"id": id, "title": title, "body": body,
			"parent_id": parentID, "conflict_original_id": origID,
		})
	}
	return out
}

func TestRemoteDecision(t *testing.T) {
	const base = int64(1000)
	si := func(syncTime int64) *models.SyncItem { return &models.SyncItem{SyncTime: syncTime} }
	cases := []struct {
		name          string
		sync          *models.SyncItem
		local, remote int64
		conflictNote  bool
		want          pullDecision
	}{
		{"clean + newer remote applies", si(base), base, base + 10, false, pullApply},
		{"clean + same revision applies", si(base), base, base, false, pullApply},
		{"clean + rolled-back remote never regresses", si(base), base, base - 10, false, pullStale},
		{"dirty + remote not newer keeps local", si(base), base + 10, base, false, pullStale},
		{"dirty + newer remote conflicts", si(base), base + 10, base + 20, false, pullConflict},
		{"never synced here applies remote", nil, base, base + 10, false, pullApply},
		{"newer conflict note pushes over remote", si(base), base + 20, base + 10, true, pullStale},
		{"older conflict note takes remote", si(base), base + 10, base + 20, true, pullConflict},
	}
	for _, c := range cases {
		got := remoteDecision(c.sync, c.local, c.remote, c.conflictNote)
		if got != c.want {
			t.Errorf("%s: remoteDecision = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestConflictNoteOnDoubleEdit(t *testing.T) {
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
	if err := dbA.CreateFolder(&models.Folder{
		ID: folderID, Title: "Rehearsal",
		CreatedTime: base, UpdatedTime: base,
		UserCreatedTime: base, UserUpdatedTime: base,
	}); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if err := dbA.CreateNote(&models.Note{
		ID: noteID, ParentID: folderID, Title: "Shared", Body: "v1", MarkupLanguage: 1,
		CreatedTime: base, UpdatedTime: base,
		UserCreatedTime: base, UserUpdatedTime: base,
	}); err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	if err := engA.Sync(ctx); err != nil {
		t.Fatalf("A sync (push): %v", err)
	}
	if err := engB.Sync(ctx); err != nil {
		t.Fatalf("B sync (pull): %v", err)
	}

	// Both clients edit the note while "offline"...
	tick()
	noteA, err := dbA.GetNote(noteID)
	if err != nil || noteA == nil {
		t.Fatalf("A GetNote: %v (%v)", noteA, err)
	}
	noteA.Body = "A edit"
	if err := dbA.UpdateNote(noteA); err != nil {
		t.Fatalf("A UpdateNote: %v", err)
	}
	tick()
	noteB, err := dbB.GetNote(noteID)
	if err != nil || noteB == nil {
		t.Fatalf("B GetNote: %v (%v)", noteB, err)
	}
	noteB.Body = "B edit"
	if err := dbB.UpdateNote(noteB); err != nil {
		t.Fatalf("B UpdateNote: %v", err)
	}

	// ...and both sync: A's version reaches the target first.
	tick()
	if err := engA.Sync(ctx); err != nil {
		t.Fatalf("A sync (push edit): %v", err)
	}
	if err := engB.Sync(ctx); err != nil {
		t.Fatalf("B sync (conflict): %v", err)
	}

	// The remote version is canonical under the original id.
	canonical, err := dbB.GetNote(noteID)
	if err != nil || canonical == nil {
		t.Fatalf("B canonical note: %v (err=%v)", canonical, err)
	}
	if canonical.Body != "A edit" || canonical.IsConflict != 0 {
		t.Fatalf("canonical = %+v, want A's version as a normal note", canonical)
	}

	// B's version survives as a conflict note (title and parent unchanged).
	conflicts := conflictNoteRows(t, dbB)
	if len(conflicts) != 1 {
		t.Fatalf("got %d conflict notes, want 1: %+v", len(conflicts), conflicts)
	}
	c := conflicts[0]
	if c["body"] != "B edit" || c["title"] != "Shared" || c["parent_id"] != folderID ||
		c["conflict_original_id"] != noteID || c["id"] == noteID {
		t.Fatalf("conflict note = %+v, want B's version linked to %s", c, noteID)
	}

	// The conflict note was pushed to the target in the same cycle.
	pushed, err := os.ReadFile(filepath.Join(remote, c["id"]+".md"))
	if err != nil {
		t.Fatalf("conflict note not pushed: %v", err)
	}
	for _, want := range []string{"B edit", "is_conflict: 1", "conflict_original_id: " + noteID} {
		if !strings.Contains(string(pushed), want) {
			t.Fatalf("pushed conflict note missing %q:\n%s", want, pushed)
		}
	}

	// The conflict note propagates to the other client.
	if err := engA.Sync(ctx); err != nil {
		t.Fatalf("A sync (pull conflict note): %v", err)
	}
	got, err := dbA.GetNote(c["id"])
	if err != nil || got == nil {
		t.Fatalf("A did not pull the conflict note (%v, err=%v)", got, err)
	}
	if got.IsConflict != 1 || got.ConflictOriginalID != noteID {
		t.Fatalf("A's copy of the conflict note = %+v", got)
	}
}

func TestConflictNoteOnRemoteDelete(t *testing.T) {
	remote := t.TempDir()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	peer, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { peer.Close() })
	eng := NewEngine(NewLocalFolderBackend(remote), db, "")
	peerEng := NewEngine(NewLocalFolderBackend(remote), peer, "")
	ctx := context.Background()

	const (
		folderID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa1"
		noteID   = "0d28d893565c4b2989301f2259164273"
		base     = int64(1772000000000)
	)
	if err := peer.CreateFolder(&models.Folder{
		ID: folderID, Title: "Rehearsal",
		CreatedTime: base, UpdatedTime: base,
		UserCreatedTime: base, UserUpdatedTime: base,
	}); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if err := peer.CreateNote(&models.Note{
		ID: noteID, ParentID: folderID, Title: "Shared", Body: "v1", MarkupLanguage: 1,
		CreatedTime: base, UpdatedTime: base,
		UserCreatedTime: base, UserUpdatedTime: base,
	}); err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	if err := peerEng.Sync(ctx); err != nil {
		t.Fatalf("peer sync (push): %v", err)
	}
	if err := eng.Sync(ctx); err != nil {
		t.Fatalf("sync (pull): %v", err)
	}

	// We edit while the peer deletes the note.
	tick()
	n, err := db.GetNote(noteID)
	if err != nil || n == nil {
		t.Fatalf("GetNote: %v (err=%v)", n, err)
	}
	n.Body = "my offline edit"
	if err := db.UpdateNote(n); err != nil {
		t.Fatalf("UpdateNote: %v", err)
	}
	tick()
	if err := peer.DeleteNote(noteID); err != nil {
		t.Fatalf("peer DeleteNote: %v", err)
	}
	if err := peerEng.Sync(ctx); err != nil {
		t.Fatalf("peer sync (push delete): %v", err)
	}
	if _, err := os.Stat(filepath.Join(remote, noteID+".md")); !os.IsNotExist(err) {
		t.Fatalf("note file still on target (stat err=%v)", err)
	}

	// Our edit survives the remote deletion as a conflict note.
	if err := eng.Sync(ctx); err != nil {
		t.Fatalf("sync (prune): %v", err)
	}
	if got, err := db.GetNote(noteID); err != nil || got != nil {
		t.Fatalf("original note still local (%v, err=%v)", got, err)
	}
	conflicts := conflictNoteRows(t, db)
	if len(conflicts) != 1 {
		t.Fatalf("got %d conflict notes, want 1: %+v", len(conflicts), conflicts)
	}
	c := conflicts[0]
	if c["body"] != "my offline edit" || c["title"] != "Shared" || c["conflict_original_id"] != "" {
		t.Fatalf("conflict note = %+v, want our version with empty conflict_original_id", c)
	}
	// The conflict note was pushed in the same cycle.
	if _, err := os.Stat(filepath.Join(remote, c["id"]+".md")); err != nil {
		t.Fatalf("conflict note not pushed: %v", err)
	}
}

func TestResourceConflictOnDoubleEdit(t *testing.T) {
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
		resID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		base  = int64(1772000000000)
	)
	mkResource := func(db *store.DB, blob string, at int64) {
		if err := db.UpsertResource(&models.Resource{
			ID: resID, Title: "shot.png", Mime: "image/png",
			CreatedTime: base, UpdatedTime: at, BlobUpdatedTime: at,
			UserCreatedTime: base, UserUpdatedTime: at,
			Size: int64(len(blob)),
		}); err != nil {
			t.Fatalf("UpsertResource: %v", err)
		}
		if err := db.SaveResourceFile(resID, []byte(blob)); err != nil {
			t.Fatalf("SaveResourceFile: %v", err)
		}
	}
	mkResource(dbA, "v1!", base)
	if err := engA.Sync(ctx); err != nil {
		t.Fatalf("A sync (push): %v", err)
	}
	if err := engB.Sync(ctx); err != nil {
		t.Fatalf("B sync (pull): %v", err)
	}

	// Both clients replace the attachment blob while "offline"...
	tick()
	mkResource(dbB, "B-bytes", time.Now().UnixMilli())
	tick()
	mkResource(dbA, "A-bytes", time.Now().UnixMilli())

	// ...and both sync: A's blob reaches the target first.
	tick()
	if err := engA.Sync(ctx); err != nil {
		t.Fatalf("A sync (push blob): %v", err)
	}
	if err := engB.Sync(ctx); err != nil {
		t.Fatalf("B sync (conflict): %v", err)
	}

	// The remote blob is canonical under the original id.
	if data, err := os.ReadFile(dbB.GetResourceFile(resID)); err != nil || string(data) != "A-bytes" {
		t.Fatalf("canonical blob = %q (err=%v), want A-bytes", data, err)
	}

	// B's blob survives as a duplicated resource...
	rows, err := dbB.Query(`SELECT id, title, is_shared, share_id FROM resources WHERE id != ?`, resID)
	if err != nil {
		t.Fatalf("query duplicate: %v", err)
	}
	type dupRow struct{ id, title string }
	var dups []dupRow
	for rows.Next() {
		var d dupRow
		var isShared int
		var shareID string
		if err := rows.Scan(&d.id, &d.title, &isShared, &shareID); err != nil {
			rows.Close()
			t.Fatalf("scan duplicate: %v", err)
		}
		if isShared != 0 || shareID != "" {
			rows.Close()
			t.Fatalf("duplicate keeps share fields: %+v", d)
		}
		dups = append(dups, d)
	}
	rows.Close()
	if len(dups) != 1 {
		t.Fatalf("got %d duplicated resources, want 1", len(dups))
	}
	dup := dups[0]
	if dup.title != "shot.png" {
		t.Fatalf("duplicate title = %q, want shot.png", dup.title)
	}
	if data, err := os.ReadFile(dbB.GetResourceFile(dup.id)); err != nil || string(data) != "B-bytes" {
		t.Fatalf("duplicate blob = %q (err=%v), want B-bytes", data, err)
	}

	// ...and one pointer note in the "Conflicts (attachments)" folder.
	nrows, err := dbB.Query(`SELECT n.title, n.body, f.title FROM notes n
		JOIN folders f ON f.id = n.parent_id WHERE n.title LIKE 'Attachment conflict:%'`)
	if err != nil {
		t.Fatalf("query attachment conflict notes: %v", err)
	}
	defer nrows.Close()
	var notes [][3]string
	for nrows.Next() {
		var title, body, folderTitle string
		if err := nrows.Scan(&title, &body, &folderTitle); err != nil {
			t.Fatalf("scan attachment conflict note: %v", err)
		}
		notes = append(notes, [3]string{title, body, folderTitle})
	}
	if len(notes) != 1 {
		t.Fatalf("got %d attachment conflict notes, want 1: %+v", len(notes), notes)
	}
	if notes[0][0] != `Attachment conflict: "shot.png"` || notes[0][2] != "Conflicts (attachments)" {
		t.Fatalf("attachment conflict note = %+v", notes[0])
	}
	for _, want := range []string{conflictHelpURL, "(:" + "/" + dup.id + ")"} {
		if !strings.Contains(notes[0][1], want) {
			t.Fatalf("attachment conflict body missing %q:\n%s", want, notes[0][1])
		}
	}

	// The duplicate (metadata + blob) was pushed in the same cycle.
	if _, err := os.Stat(filepath.Join(remote, dup.id+".md")); err != nil {
		t.Fatalf("duplicate metadata not pushed: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(remote, ".resource", dup.id)); err != nil || string(data) != "B-bytes" {
		t.Fatalf("duplicate blob not pushed: %q (err=%v)", data, err)
	}
}
