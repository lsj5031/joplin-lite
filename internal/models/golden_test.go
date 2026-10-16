package models

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The golden corpus is the M0 format gate: every fixture is Joplin's canonical
// serialized item text and must round-trip byte-for-byte through our parser and
// serializers. See testdata/golden/README.md for the fixture rules.

func TestGoldenCorpus(t *testing.T) {
	files, err := filepath.Glob("testdata/golden/*.md")
	if err != nil {
		t.Fatal(err)
	}
	fixtures := 0
	for _, file := range files {
		name := filepath.Base(file)
		if name == "README.md" {
			continue
		}
		fixtures++
		rawBytes, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		raw := string(rawBytes)
		passthroughOnly := strings.HasPrefix(name, "envelope_")
		extraKeys := strings.Contains(name, "_extra")

		t.Run(name, func(t *testing.T) {
			if strings.HasSuffix(raw, "\n") {
				t.Fatalf("fixture must be canonical Joplin output: no trailing newline")
			}

			pi, err := ParseItem(raw)
			if err != nil {
				t.Fatalf("ParseItem: %v", err)
			}

			// 1. Raw round-trip: the exact bytes are preserved.
			if got := pi.Bytes(); got != raw {
				t.Fatalf("raw round-trip mismatch:\n got: %q\nwant: %q", got, raw)
			}

			if passthroughOnly {
				// Envelope forms carry a reduced keep-keys metadata block;
				// only the raw layer applies to them.
				return
			}

			// 2. Merge round-trip: typed fields patched over the raw item keep
			// unknown keys verbatim and produce identical bytes. This is the
			// push path for a changed item.
			over := typedFieldsFor(t, raw)
			if got := pi.SerializeWithOverrides(over); got != raw {
				t.Fatalf("merge round-trip mismatch:\n got: %q\nwant: %q", got, raw)
			}

			if extraKeys {
				// Unknown keys survive only through the raw layer; typed
				// serialization legitimately drops them.
				return
			}

			// 3. Typed round-trip for fixtures with only model-known keys.
			if got := serializeTyped(raw); got != raw {
				t.Fatalf("typed round-trip mismatch:\n got: %q\nwant: %q", got, raw)
			}
		})
	}
	if fixtures < 10 {
		t.Fatalf("golden corpus too small: %d fixtures", fixtures)
	}
}

// typedFieldsFor returns the formatted field map (including title/body where
// the type has them) for a fixture, exactly as the push path builds it.
func typedFieldsFor(t *testing.T, raw string) map[string]string {
	t.Helper()
	itemType, _ := Deserialize(raw)
	switch itemType {
	case TypeNote:
		n := DeserializeNote(raw)
		f := NoteFields(n)
		f["title"] = n.Title
		f["body"] = n.Body
		return f
	case TypeFolder:
		f := DeserializeFolder(raw)
		m := FolderFields(f)
		m["title"] = f.Title
		return m
	case TypeTag:
		tg := DeserializeTag(raw)
		m := TagFields(tg)
		m["title"] = tg.Title
		return m
	case TypeResource:
		r := DeserializeResource(raw)
		m := ResourceFields(r)
		m["title"] = r.Title
		return m
	case TypeNoteTag:
		return NoteTagFields(DeserializeNoteTag(raw))
	case TypeMasterKey:
		return MasterKeyFields(DeserializeMasterKey(raw))
	}
	t.Fatalf("unknown item type %d", itemType)
	return nil
}

func serializeTyped(raw string) string {
	itemType, _ := Deserialize(raw)
	switch itemType {
	case TypeNote:
		return SerializeNote(DeserializeNote(raw))
	case TypeFolder:
		return SerializeFolder(DeserializeFolder(raw))
	case TypeTag:
		return SerializeTag(DeserializeTag(raw))
	case TypeResource:
		return SerializeResource(DeserializeResource(raw))
	case TypeNoteTag:
		return SerializeNoteTag(DeserializeNoteTag(raw))
	case TypeMasterKey:
		return SerializeMasterKey(DeserializeMasterKey(raw))
	}
	return ""
}
