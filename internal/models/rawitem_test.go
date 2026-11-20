package models

import (
	"strings"
	"testing"
)

func TestParseItemCRLFMetadata(t *testing.T) {
	note := &Note{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Title: "Title", UpdatedTime: 1, Author: " Jane ", IsTodo: 1}
	content := strings.ReplaceAll(SerializeNote(note), "\n", "\r\n") + "\r\nfuture_field: value \r\n"
	raw, err := ParseItem(content)
	if err != nil {
		t.Fatal(err)
	}
	if raw.Type != TypeNote {
		t.Fatalf("type = %d, want %d", raw.Type, TypeNote)
	}
	fields := make(map[string]string)
	for _, meta := range raw.Meta {
		fields[meta.Key] = meta.Value
	}
	for key, want := range NoteFields(note) {
		if got := fields[key]; got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if got := fields["future_field"]; got != "value " {
		t.Errorf("future_field = %q, want %q", got, "value ")
	}
	if got := raw.SerializeWithOverrides(NoteFields(note)); got != content {
		t.Fatalf("unchanged metadata merge changed CRLF bytes:\n got: %q\nwant: %q", got, content)
	}
}

func TestDeserializePreservesTerminalAuthorWhitespace(t *testing.T) {
	content := "Title\n\ntype_: 1\nauthor:  Jane \r\n"
	itemType, fields := Deserialize(content)
	if itemType != TypeNote || fields["author"] != " Jane " {
		t.Fatalf("type = %d, author = %q", itemType, fields["author"])
	}
}
