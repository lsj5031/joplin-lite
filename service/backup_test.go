package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/internal/store"
)

func newTestStore(t *testing.T) (*store.DB, *Service) {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db, New(db)
}

func TestExportImportRoundTrip(t *testing.T) {
	_, src := newTestStore(t)

	notebook := &models.Folder{Title: "Projects"}
	if err := src.CreateNotebook(notebook); err != nil {
		t.Fatal(err)
	}
	attachment, ref, err := src.AttachBytes("diagram.png", "image/png", []byte("PNGDATA"))
	if err != nil {
		t.Fatal(err)
	}
	note := &models.Note{ParentID: notebook.ID, Title: "Design", Body: "See " + ref}
	if err := src.CreateNote(note); err != nil {
		t.Fatal(err)
	}

	archive := filepath.Join(t.TempDir(), "backup.jex")
	stats, err := src.ExportJEX(archive, nil)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if stats.Notes != 1 || stats.Folders != 1 || stats.Resources != 1 {
		t.Fatalf("stats: %+v", stats)
	}
	if info, err := os.Stat(archive); err != nil || info.Size() == 0 {
		t.Fatalf("archive missing or empty: %v", err)
	}
	if _, err := os.Stat(archive + ".part"); !os.IsNotExist(err) {
		t.Error("temporary archive was left behind")
	}

	dstDB, dst := newTestStore(t)

	// A dry run must not write anything.
	plan, err := dst.ImportJEX(archive, ImportOptions{DryRun: true})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if plan.Notes != 1 || plan.Folders != 1 || plan.Resources != 1 {
		t.Fatalf("plan: %+v", plan)
	}
	if notes, _ := dstDB.AllNotes(); len(notes) != 0 {
		t.Fatalf("dry run wrote %d notes", len(notes))
	}

	result, err := dst.ImportJEX(archive, ImportOptions{})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Notes != 1 || result.Folders != 1 || result.Resources != 1 {
		t.Fatalf("result: %+v", result)
	}

	notes, err := dstDB.AllNotes()
	if err != nil || len(notes) != 1 {
		t.Fatalf("imported notes: %v %v", len(notes), err)
	}
	folders, err := dstDB.AllFolders()
	if err != nil || len(folders) != 1 || folders[0].Title != "Projects" {
		t.Fatalf("imported notebooks: %+v %v", folders, err)
	}
	if notes[0].ParentID != folders[0].ID {
		t.Errorf("note parent not remapped: %q vs %q", notes[0].ParentID, folders[0].ID)
	}
	if notes[0].ID == note.ID {
		t.Error("imported note kept its original id")
	}

	resources, err := dstDB.AllResources()
	if err != nil || len(resources) != 1 {
		t.Fatalf("imported attachments: %v %v", len(resources), err)
	}
	res := resources[0]
	if res.ID == attachment.ID {
		t.Error("imported attachment kept its original id")
	}
	if !strings.Contains(notes[0].Body, res.ID) {
		t.Errorf("body not remapped to new attachment id: %q", notes[0].Body)
	}
	if strings.Contains(notes[0].Body, attachment.ID) {
		t.Errorf("body still references the old attachment id: %q", notes[0].Body)
	}
	blob, err := os.ReadFile(filepath.Join(dstDB.ResourceDir(), res.ID+"."+res.FileExtension))
	if err != nil || string(blob) != "PNGDATA" {
		t.Fatalf("attachment bytes: %q %v", blob, err)
	}

	// Importing again adds a second copy rather than merging.
	second, err := dst.ImportJEX(archive, ImportOptions{})
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if second.Notes != 1 {
		t.Fatalf("second import: %+v", second)
	}
	if notes, _ := dstDB.AllNotes(); len(notes) != 2 {
		t.Errorf("expected 2 notes after importing twice, got %d", len(notes))
	}
	folders, _ = dstDB.AllFolders()
	if len(folders) != 2 || folders[1].Title != "Projects (1)" {
		t.Errorf("notebook titles should stay unique: %+v", folders)
	}
}

// TestImportKeepsNoteTimestamps keeps date-sorted lists in their original
// order: rewriting updated_time to the import time would flatten every note to
// the same instant and scramble the order the archive was taken from.
func TestImportKeepsNoteTimestamps(t *testing.T) {
	srcDB, src := newTestStore(t)
	notebook := &models.Folder{Title: "Journal"}
	if err := src.CreateNotebook(notebook); err != nil {
		t.Fatal(err)
	}
	older := &models.Note{ParentID: notebook.ID, Title: "Older", Body: "a"}
	newer := &models.Note{ParentID: notebook.ID, Title: "Newer", Body: "b"}
	if err := src.CreateNote(older); err != nil {
		t.Fatal(err)
	}
	if err := src.CreateNote(newer); err != nil {
		t.Fatal(err)
	}

	stamps := map[string]int64{"Older": 1600000000000, "Newer": 1650000000000}
	for title, ms := range stamps {
		if _, err := srcDB.Exec(`UPDATE notes SET created_time=?, updated_time=?,
			user_created_time=?, user_updated_time=? WHERE title=?`,
			ms, ms, ms, ms, title); err != nil {
			t.Fatalf("set timestamps: %v", err)
		}
	}

	archive := filepath.Join(t.TempDir(), "stamps.jex")
	if _, err := src.ExportJEX(archive, nil); err != nil {
		t.Fatal(err)
	}

	dstDB, dst := newTestStore(t)
	if _, err := dst.ImportJEX(archive, ImportOptions{}); err != nil {
		t.Fatalf("import: %v", err)
	}
	notes, err := dstDB.AllNotes()
	if err != nil || len(notes) != 2 {
		t.Fatalf("imported notes: %v %v", len(notes), err)
	}
	for _, n := range notes {
		want, ok := stamps[n.Title]
		if !ok {
			t.Fatalf("unexpected note %q", n.Title)
		}
		if n.UpdatedTime != want || n.CreatedTime != want {
			t.Errorf("%s times = created %d, updated %d; want %d",
				n.Title, n.CreatedTime, n.UpdatedTime, want)
		}
		if n.UserUpdatedTime != want {
			t.Errorf("%s user_updated_time = %d, want %d", n.Title, n.UserUpdatedTime, want)
		}
	}
}

func TestImportIntoExistingNotebook(t *testing.T) {
	_, src := newTestStore(t)
	notebook := &models.Folder{Title: "Source"}
	if err := src.CreateNotebook(notebook); err != nil {
		t.Fatal(err)
	}
	if err := src.CreateNote(&models.Note{ParentID: notebook.ID, Title: "One", Body: "x"}); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "one.jex")
	if _, err := src.ExportJEX(archive, nil); err != nil {
		t.Fatal(err)
	}

	dstDB, dst := newTestStore(t)
	target := &models.Folder{Title: "Inbox"}
	if err := dst.CreateNotebook(target); err != nil {
		t.Fatal(err)
	}

	if _, err := dst.ImportJEX(archive, ImportOptions{NotebookID: target.ID}); err != nil {
		t.Fatalf("import: %v", err)
	}
	folders, _ := dstDB.AllFolders()
	if len(folders) != 1 {
		t.Errorf("import into an existing notebook should not create notebooks, got %d", len(folders))
	}
	notes, _ := dstDB.AllNotes()
	if len(notes) != 1 || notes[0].ParentID != target.ID {
		t.Errorf("note not placed in the target notebook: %+v", notes)
	}
}

func TestImportTagsAndLinks(t *testing.T) {
	_, src := newTestStore(t)
	notebook := &models.Folder{Title: "Notes"}
	if err := src.CreateNotebook(notebook); err != nil {
		t.Fatal(err)
	}
	note := &models.Note{ParentID: notebook.ID, Title: "Tagged", Body: "body"}
	if err := src.CreateNote(note); err != nil {
		t.Fatal(err)
	}
	tag := &models.Tag{Title: "idea"}
	if err := src.CreateTag(tag); err != nil {
		t.Fatal(err)
	}
	if err := src.AddTag(note.ID, tag.ID); err != nil {
		t.Fatal(err)
	}

	archive := filepath.Join(t.TempDir(), "tags.jex")
	if _, err := src.ExportJEX(archive, nil); err != nil {
		t.Fatal(err)
	}

	dstDB, dst := newTestStore(t)
	// An existing tag with the same name must be reused, not duplicated.
	existing := &models.Tag{Title: "idea"}
	if err := dst.CreateTag(existing); err != nil {
		t.Fatal(err)
	}
	if _, err := dst.ImportJEX(archive, ImportOptions{}); err != nil {
		t.Fatalf("import: %v", err)
	}

	tags, err := dstDB.AllTags()
	if err != nil || len(tags) != 1 {
		t.Fatalf("tags after import: %+v %v", tags, err)
	}
	if tags[0].ID != existing.ID {
		t.Errorf("existing tag was not reused")
	}
	links, err := dstDB.AllNoteTags()
	if err != nil || len(links) != 1 {
		t.Fatalf("tag links after import: %+v %v", links, err)
	}
	notes, _ := dstDB.AllNotes()
	if notes[0].ID != links[0].NoteID || existing.ID != links[0].TagID {
		t.Errorf("link not remapped to the imported note and existing tag: %+v", links[0])
	}
}

func TestImportRejectsMissingNotebook(t *testing.T) {
	_, src := newTestStore(t)
	if err := src.CreateNote(&models.Note{Title: "One", Body: "x"}); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "one.jex")
	if _, err := src.ExportJEX(archive, nil); err != nil {
		t.Fatal(err)
	}

	dstDB, dst := newTestStore(t)
	if _, err := dst.ImportJEX(archive, ImportOptions{NotebookID: "missing"}); err == nil {
		t.Fatal("expected an error for an unknown destination notebook")
	}
	if notes, _ := dstDB.AllNotes(); len(notes) != 0 {
		t.Error("failed import wrote notes")
	}
}

func TestExportRefusesMissingTarget(t *testing.T) {
	_, svc := newTestStore(t)
	dir := t.TempDir()
	if _, err := svc.ExportJEX(filepath.Join(dir, "nope", "x.jex"), nil); err == nil {
		t.Fatal("expected an error when the destination directory does not exist")
	}
}
