package enveloperepair

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/lsj5031/joplin-lite/internal/models"
)

func TestOfficialRevisionValidationOnly(t *testing.T) {
	raw, err := os.ReadFile("../models/testdata/official-envelope/items.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Revision struct {
			Base []struct {
				Name string `json:"name"`
			} `json:"baseColumns"`
			Text string `json:"text"`
		} `json:"revisionEnvelope"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Revision.Base) != 12 {
		t.Fatal("missing independent historical revision schema")
	}
	id := strings.Repeat("a", 32)
	// Use the official producer bytes, replacing only its synthetic crypto
	// boundary marker with our independently framing-valid synthetic packet.
	_, fields := models.Deserialize(string(oldEnvelope(id, 1)))
	revision := []byte(strings.ReplaceAll(fixture.Revision.Text, "synthetic-ciphertext", fields["encryption_cipher_text"]))
	fixed, err := Correct(revision, id)
	if err != nil || !bytes.Equal(fixed, revision) {
		t.Fatalf("complete revision changed: %v", err)
	}
	legacy := bytes.ReplaceAll(revision, []byte("is_locked: \n"), nil)
	if fixed, err := Correct(legacy, id); err != nil || !bytes.Equal(fixed, legacy) {
		t.Fatalf("legacy revision changed: %v", err)
	}
	for _, column := range fixture.Revision.Base {
		if column.Name == "id" || column.Name == "encryption_cipher_text" || column.Name == "encryption_applied" || column.Name == "updated_time" {
			continue
		}
		incomplete := bytes.ReplaceAll(legacy, []byte(column.Name+": \n"), nil)
		if _, err := Correct(incomplete, id); err == nil {
			t.Fatalf("incomplete revision accepted without %s", column.Name)
		}
	}
}
