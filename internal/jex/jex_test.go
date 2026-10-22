package jex

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/lsj5031/joplin-lite/internal/models"
)

// A note body full of things that could confuse the property block: blank
// lines, lines that look like properties, and a literal backslash-n.
const trickyBody = "line one\n\n- [ ] task\nnot_a_prop: value\nliteral \\n here\n\n"

func TestNoteRoundTrip(t *testing.T) {
	note := &models.Note{
		ID:             strings.Repeat("a", 32),
		ParentID:       strings.Repeat("b", 32),
		Title:          "Tricky: title",
		Body:           trickyBody,
		CreatedTime:    1749511869316,
		UpdatedTime:    1749511880855,
		IsTodo:         1,
		TodoDue:        1750000000000,
		TodoCompleted:  0,
		Latitude:       -36.8484597,
		Longitude:      174.7633315,
		Altitude:       12.5,
		Order:          3.5,
		MarkupLanguage: 1,
		Source:         "joplin-desktop",
		Type_:          models.TypeNote,
	}

	it, err := parseItem(SerializeNote(note))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := it.note()

	if got.Title != note.Title {
		t.Errorf("title: got %q want %q", got.Title, note.Title)
	}
	if got.Body != note.Body {
		t.Errorf("body:\ngot  %q\nwant %q", got.Body, note.Body)
	}
	if got.ID != note.ID || got.ParentID != note.ParentID {
		t.Errorf("ids: got %s/%s", got.ID, got.ParentID)
	}
	if got.CreatedTime != note.CreatedTime || got.UpdatedTime != note.UpdatedTime {
		t.Errorf("times: got %d/%d want %d/%d", got.CreatedTime, got.UpdatedTime, note.CreatedTime, note.UpdatedTime)
	}
	if got.IsTodo != 1 || got.TodoDue != note.TodoDue {
		t.Errorf("todo: got is_todo=%d due=%d", got.IsTodo, got.TodoDue)
	}
	if got.Latitude != note.Latitude || got.Longitude != note.Longitude || got.Altitude != note.Altitude {
		t.Errorf("coords: got %v,%v,%v", got.Latitude, got.Longitude, got.Altitude)
	}
	if got.Order != note.Order {
		t.Errorf("order: got %v want %v", got.Order, note.Order)
	}
}

// The serialized layout must match what Joplin writes, or re-importing our
// archives into official Joplin breaks.
func TestSerializeNoteLayout(t *testing.T) {
	note := &models.Note{
		ID:          strings.Repeat("c", 32),
		ParentID:    strings.Repeat("d", 32),
		Title:       "Hello",
		Body:        "Body text",
		CreatedTime: 1749511869316,
		UpdatedTime: 1749511880855,
	}
	out := SerializeNote(note)

	wantPrefix := "Hello\n\nBody text\n\nid: " + note.ID + "\nparent_id: " + note.ParentID + "\ncreated_time: 2025-06-09T23:31:09.316Z\n"
	if !strings.HasPrefix(out, wantPrefix) {
		t.Fatalf("prefix mismatch:\n got %q\nwant %q", prefixOf(out, len(wantPrefix)), wantPrefix)
	}
	if !strings.HasSuffix(out, "\ntype_: 1") {
		t.Errorf("archive item must end with type_ and no trailing newline, got %q", tailOf(out, 40))
	}
	if strings.Count(out, "type_: 1") != 1 {
		t.Errorf("type_ should appear once")
	}
	props := strings.Split(strings.TrimPrefix(out, "Hello\n\nBody text\n\n"), "\n")
	if props[0] != "id: "+note.ID {
		t.Errorf("first property should be id, got %q", props[0])
	}
	if props[len(props)-1] != "type_: 1" {
		t.Errorf("last property should be type_, got %q", props[len(props)-1])
	}
}

func TestEmptyBodyNote(t *testing.T) {
	note := &models.Note{ID: strings.Repeat("e", 32), Title: "No body", MarkupLanguage: 1}
	out := SerializeNote(note)
	if !strings.HasPrefix(out, "No body\n\nid: ") {
		t.Fatalf("empty body should not leave extra blank lines: %q", prefixOf(out, 60))
	}
	it, err := parseItem(out)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if it.title != "No body" || it.body != "" {
		t.Errorf("got title=%q body=%q", it.title, it.body)
	}
}

func TestValueEscaping(t *testing.T) {
	values := []string{"plain", "a\nb", `literal \n`, "carriage\rreturn", ""}
	for _, v := range values {
		round := unescapeValue(escapeValue(v))
		if round != v {
			t.Errorf("escape round trip: got %q want %q", round, v)
		}
	}
}

func TestReadArchive(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, strings.Repeat("1", 32)+".png"), []byte("PNGDATA"), 0o600); err != nil {
		t.Fatal(err)
	}
	data := ExportData{
		Folders: []*models.Folder{{
			ID: strings.Repeat("f", 32), Title: "Notebook", Icon: "1",
			CreatedTime: 1700000000000, UpdatedTime: 1700000001000,
		}},
		Notes: []*models.Note{{
			ID: strings.Repeat("n", 32), ParentID: strings.Repeat("f", 32),
			Title: "Note", Body: "See [file](:/" + strings.Repeat("1", 32) + ")", MarkupLanguage: 1,
			CreatedTime: 1700000000000, UpdatedTime: 1700000001000,
		}},
		Resources: []*models.Resource{{
			ID: strings.Repeat("1", 32), Title: "pic.png", Mime: "image/png",
			FileExtension: "png", Size: 7,
		}},
		Tags:        []*models.Tag{{ID: strings.Repeat("t", 32), Title: "tag"}},
		NoteTags:    []*models.NoteTag{{ID: strings.Repeat("x", 32), NoteID: strings.Repeat("n", 32), TagID: strings.Repeat("t", 32)}},
		ResourceDir: dir,
	}

	var buf bytes.Buffer
	stats, err := Export(&buf, data)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if stats.Notes != 1 || stats.Folders != 1 || stats.Resources != 1 || stats.Tags != 1 || stats.NoteTags != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}

	archive, err := ReadArchive(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(archive.Notes) != 1 || len(archive.Folders) != 1 || len(archive.Resources) != 1 ||
		len(archive.Tags) != 1 || len(archive.NoteTags) != 1 {
		t.Fatalf("archive counts: %d notes, %d folders, %d resources, %d tags, %d links",
			len(archive.Notes), len(archive.Folders), len(archive.Resources), len(archive.Tags), len(archive.NoteTags))
	}
	n := archive.Notes[0]
	if n.Title != "Note" || !strings.Contains(n.Body, ":/"+strings.Repeat("1", 32)) {
		t.Errorf("note did not round trip: %+v", n)
	}
	if archive.Folders[0].Icon != "1" {
		t.Errorf("folder icon lost: %q", archive.Folders[0].Icon)
	}
	r := archive.Resources[0]
	if r.FileExtension != "png" || r.Mime != "image/png" || r.Size != 7 {
		t.Errorf("resource metadata lost: %+v", r)
	}

	// Attachment bytes must be present under the Joplin expected path.
	if !bytes.Contains(buf.Bytes(), []byte("resources/"+strings.Repeat("1", 32)+".png")) {
		t.Error("attachment entry missing from archive")
	}
}

func TestParseJoplinSample(t *testing.T) {
	// Shape taken from a real Joplin export, with values changed.
	sample := "My note\n\nBody line\n\nid: " + strings.Repeat("a", 32) + "\n" +
		"parent_id: " + strings.Repeat("b", 32) + "\n" +
		"created_time: 2025-06-09T23:31:09.316Z\n" +
		"updated_time: 2025-06-09T23:31:20.855Z\n" +
		"is_conflict: 0\nlatitude: -36.84845970\nlongitude: 174.76333150\naltitude: 0.0000\n" +
		"author: \nsource_url: \nis_todo: 0\ntodo_due: 0\ntodo_completed: 0\n" +
		"source: joplin-desktop\nsource_application: net.cozic.joplin-desktop\napplication_data: \n" +
		"order: 0\nuser_created_time: 2025-06-09T23:31:09.316Z\nuser_updated_time: 2025-06-09T23:31:20.855Z\n" +
		"encryption_cipher_text: \nencryption_applied: 0\nmarkup_language: 1\nis_shared: 0\nshare_id: \n" +
		"conflict_original_id: \nmaster_key_id: \nuser_data: \ndeleted_time: 0\n" +
		"is_locked: 0\nextracted_resource_ids: \ntype_: 1"

	it, err := parseItem(sample)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	n := it.note()
	if n.Title != "My note" || n.Body != "Body line" {
		t.Errorf("title/body: %q / %q", n.Title, n.Body)
	}
	if n.CreatedTime != 1749511869316 {
		t.Errorf("created_time: got %d", n.CreatedTime)
	}
	if !reflect.DeepEqual([]float64{n.Latitude, n.Longitude, n.Altitude}, []float64{-36.8484597, 174.7633315, 0}) {
		t.Errorf("coords: %v %v %v", n.Latitude, n.Longitude, n.Altitude)
	}
	if it.typeVal() != models.TypeNote {
		t.Errorf("type_: got %d", it.typeVal())
	}
}

func TestExtractResources(t *testing.T) {
	dir := t.TempDir()
	id := strings.Repeat("9", 32)
	if err := os.WriteFile(filepath.Join(dir, id+".txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := Export(&buf, ExportData{
		Resources:   []*models.Resource{{ID: id, FileExtension: "txt", Title: "a.txt"}},
		ResourceDir: dir,
	}); err != nil {
		t.Fatal(err)
	}

	found := map[string]string{}
	if err := ExtractResources(bytes.NewReader(buf.Bytes()), func(name string, data io.Reader) error {
		b, err := io.ReadAll(data)
		found[name] = string(b)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if found[id+".txt"] != "hello" {
		t.Errorf("attachment bytes: %v", found)
	}
}

func prefixOf(s string, n int) string {
	if len(s) < n {
		return s
	}
	return s[:n]
}

func tailOf(s string, n int) string {
	if len(s) < n {
		return s
	}
	return s[len(s)-n:]
}
