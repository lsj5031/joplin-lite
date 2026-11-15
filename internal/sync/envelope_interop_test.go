package sync

import (
	"strings"
	"testing"

	"github.com/lsj5031/joplin-lite/internal/e2ee"
	"github.com/lsj5031/joplin-lite/internal/models"
)

func TestEncryptedProducersPreserveCiphertextAndConfidentiality(t *testing.T) {
	db := testDB(t)
	key := testMasterKey(t)
	svc := e2ee.NewService()
	if err := svc.LoadMasterKey(key.ID, key.Content, "test-password", key.EncryptionMethod); err != nil {
		t.Fatal(err)
	}
	const id = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const updated = int64(1772000000000)
	n := &models.Note{ID: id, Title: "private title", Body: "private body", Author: "private author", UpdatedTime: updated}
	f := &models.Folder{ID: id, Title: "private title", UpdatedTime: updated}
	tag := &models.Tag{ID: id, Title: "private title", UpdatedTime: updated}
	nt := &models.NoteTag{ID: id, NoteID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", TagID: "cccccccccccccccccccccccccccccccc", UpdatedTime: updated}
	r := &models.Resource{ID: id, Title: "private title", Filename: "private filename", Mime: "private mime", EncryptionBlobEncrypted: 1, UpdatedTime: updated}
	for _, tc := range []struct {
		name, plain string
		produce     func([]byte) []byte
	}{
		{"note", models.SerializeNote(n), func(p []byte) []byte { return serializeNoteForSync(n, p, true, db, true, svc, key.ID) }},
		{"folder", models.SerializeFolder(f), func(p []byte) []byte { return serializeFolderForSync(f, p, true, db, true, svc, key.ID) }},
		{"tag", models.SerializeTag(tag), func(p []byte) []byte { return serializeTagForSync(tag, p, true, db, true, svc, key.ID) }},
		{"note-tag", models.SerializeNoteTag(nt), func(p []byte) []byte { return serializeNoteTagForSync(nt, p, true, db, true, svc, key.ID) }},
		{"resource", models.SerializeResource(r), func(p []byte) []byte { return serializeResourceForSync(r, p, true, db, true, svc, key.ID) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plain := tc.plain + "\nfuture_secret: private unknown"
			raw := tc.produce([]byte(plain))
			if len(raw) == 0 || strings.Contains(string(raw), "private") {
				t.Fatal("empty envelope or plaintext disclosure")
			}
			kind, fields := models.Deserialize(string(raw))
			missing, supported := models.EncryptedEnvelopeMissing(kind, fields)
			if !supported || len(missing) != 0 {
				t.Fatalf("incomplete schema: %v", missing)
			}
			ciphertext := fields["encryption_cipher_text"]
			decrypted, err := svc.DecryptString(ciphertext)
			if err != nil || decrypted != plain {
				t.Fatalf("plaintext changed: %v", err)
			}
			// Only metadata serialization changed; the encrypted payload is
			// passed through byte-for-byte, including future private fields.
			meta := envelopeMeta(fields, ciphertext, key.ID)
			_, again := models.Deserialize(models.SerializeEncryptedEnvelope(meta))
			if again["encryption_cipher_text"] != ciphertext {
				t.Fatal("ciphertext changed")
			}
			if tc.name == "resource" {
				_, inside := models.Deserialize(decrypted)
				if fields["encryption_blob_encrypted"] != "" || inside["encryption_blob_encrypted"] != "1" {
					t.Fatal("resource blob encryption state changed")
				}
			}
		})
	}
}
