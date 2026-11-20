package sync

import (
	"errors"
	"strings"
	"testing"

	"github.com/lsj5031/joplin-lite/internal/models"
)

func TestApplyDeltaItemDispatchesTerminalPaddedNoteType(t *testing.T) {
	db := testDB(t)
	note := &models.Note{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Title: "Title", UpdatedTime: 1, Author: "Jane "}
	content := models.SerializeNote(note) + " \r\n"
	item := DeltaItem{ItemName: note.ID + ".md", Type: 1}
	if err := applyDeltaItem(nil, db, 9, item, []byte(content)); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetNote(note.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Author != note.Author {
		t.Fatalf("expected the pulled note with author %q, got %+v", note.Author, got)
	}
	if _, err := conflictNoteRaw([]byte(content), got); err != nil {
		t.Fatal(err)
	}
}

func TestConflictNoteRawPreservesWhitespace(t *testing.T) {
	note := &models.Note{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Title: "Title", Body: "Body", UpdatedTime: 1, Author: "\t Jane "}
	content := models.SerializeNote(note) + "\nfuture_field:  value\\n \t "
	parsed := models.DeserializeNote(content)
	raw, err := conflictNoteRaw([]byte(content), parsed)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Author != note.Author {
		t.Fatalf("author = %q, want %q", parsed.Author, note.Author)
	}
	if got := unknownNoteFields(raw)["future_field"]; got != " value\n \t " {
		t.Fatalf("unknown field = %q", got)
	}
	copyFields := noteOverrides(parsed)
	copyFields["author"] = strings.TrimSpace(parsed.Author)
	merged := raw.SerializeWithOverrides(copyFields)
	if !strings.HasSuffix(merged, "\nfuture_field:  value\\n \t ") || models.DeserializeNote(merged).Author != "Jane" {
		t.Fatalf("merge changed unknown metadata or retained stale author whitespace: %q", merged)
	}
}

func TestResolveNoteConflictKeepsUnknownWhitespaceChange(t *testing.T) {
	db := testDB(t)
	local := &models.Note{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Title: "Title", UpdatedTime: 1, Author: "Jane "}
	localContent := models.SerializeNote(local) + "\nfuture_field: value "
	if err := db.CreateNoteAndRaw(local, []byte(localContent)); err != nil {
		t.Fatal(err)
	}
	remote := *local
	remote.UpdatedTime++
	remoteContent := models.SerializeNote(&remote) + "\nfuture_field: value"
	if err := resolveNoteConflict(db, nil, local, models.DeserializeNote(remoteContent), []byte(remoteContent)); err != nil {
		t.Fatal(err)
	}
	conflicts := conflictNoteRows(t, db)
	if len(conflicts) != 1 {
		t.Fatalf("got %d conflicts, want 1", len(conflicts))
	}
	copyRaw, _, ok, err := db.GetItemRaw(conflicts[0]["id"])
	if err != nil {
		t.Fatal(err)
	}
	if !ok || models.DeserializeNote(string(copyRaw)).Author != local.Author ||
		!strings.HasSuffix(string(copyRaw), "\nfuture_field: value ") {
		t.Fatalf("conflict copy lost metadata whitespace: %q", copyRaw)
	}
}

func TestConflictNoteRawRejectsMalformedMetadata(t *testing.T) {
	note := &models.Note{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Title: "Title", Body: "Body", UpdatedTime: 1}
	content := models.SerializeNote(note)
	for _, test := range []struct {
		name string
		old  string
		new  string
	}{
		{"duplicate key", "author: ", "author: Jane\nauthor: Jane"},
		{"empty key", "author: ", ": "},
		{"missing separator space", "author: ", "author:"},
		{"padded key", "author: ", " author: "},
		{"shadowed title", "author: ", "title: Title"},
		{"invalid integer", "is_todo: 0", "is_todo: zero"},
		{"padded integer", "is_todo: 0", "is_todo:  0 "},
		{"nonfinite float", "order: 0", "order: NaN"},
		{"invalid timestamp", "1970-01-01T00:00:00.001Z", "tomorrow"},
		{"missing timestamp", "1970-01-01T00:00:00.001Z", ""},
		{"invalid type", "type_: 1", "type_: 1junk"},
		{"padded nonterminal type", "type_: 1", "type_: 1 \nfuture_field: value"},
		{"invalid head", "Title\n\nBody", "Title\nBody"},
	} {
		t.Run(test.name, func(t *testing.T) {
			malformed := strings.Replace(content, test.old, test.new, 1)
			if malformed == content {
				t.Fatal("fixture did not change")
			}
			if _, err := conflictNoteRaw([]byte(malformed), note); !errors.Is(err, errUncertainNoteConflict) {
				t.Fatalf("expected uncertain conflict, got %v", err)
			}
		})
	}
}
