package sync

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/pbkdf2"

	"github.com/lsj5031/joplin-lite/internal/e2ee"
	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/internal/store"
)

// buildTestKeyV1Packet builds a KeyV1 master key packet
// ({"salt","iv","ct"}, PBKDF2-HMAC-SHA512 220k, AES-256-GCM) — the same shape
// the frozen e2ee package decrypts via LoadMasterKey. Test-only duplicate of
// the e2ee test helper, which is not visible from this package.
func buildTestKeyV1Packet(t *testing.T, password string) string {
	t.Helper()
	rawKey := make([]byte, 256)
	for i := range rawKey {
		rawKey[i] = byte(i)
	}
	salt := make([]byte, 32)
	for i := range salt {
		salt[i] = byte(i)
	}
	iv := make([]byte, 12)
	for i := range iv {
		iv[i] = byte(i + 10)
	}
	derived := pbkdf2.Key([]byte(password), salt, 220000, 32, sha512.New)
	block, err := aes.NewCipher(derived)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	ct := gcm.Seal(nil, iv, rawKey, nil)
	pkt := map[string]string{
		"salt": base64.StdEncoding.EncodeToString(salt),
		"iv":   base64.StdEncoding.EncodeToString(iv),
		"ct":   base64.StdEncoding.EncodeToString(ct),
	}
	b, err := json.Marshal(pkt)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestE2EE_ResourcePushPullRoundTrip runs the M2 encrypted-resource leg
// through the local-folder rehearsal: a resource pushed encrypted (metadata
// envelope + FileV1 blob) reaches the other client as plaintext again — the
// full loop info.json master key -> LoadMasterKey -> encrypt -> decrypt.
func TestE2EE_ResourcePushPullRoundTrip(t *testing.T) {
	const (
		password = "test-password"
		keyID    = "0123456789abcdef0123456789abcdef"
		resID    = "5b2c9e1d4a6f80b3c5d7e9f1a3b5c7d9"
		noteID   = "0d28d893565c4b2989301f2259164273"
		folderID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa1"
		base     = int64(1772000000000)
	)
	blob := []byte("hello encrypted resource blob \x00\x01\xff")

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

	// Client A has the master key (as pulled from a real target's info.json).
	mk := &models.MasterKey{
		ID: keyID, CreatedTime: base, UpdatedTime: base,
		SourceApplication: "joplin-desktop",
		EncryptionMethod:  e2ee.MethodKeyV1,
		Content:           buildTestKeyV1Packet(t, password),
	}
	if err := dbA.UpsertMasterKey(mk); err != nil {
		t.Fatalf("UpsertMasterKey: %v", err)
	}
	if err := dbA.SetActiveMasterKeyID(keyID); err != nil {
		t.Fatalf("SetActiveMasterKeyID: %v", err)
	}

	if err := dbA.CreateFolder(&models.Folder{
		ID: folderID, Title: "E2EE",
		CreatedTime: base, UpdatedTime: base,
		UserCreatedTime: base, UserUpdatedTime: base,
	}); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if err := dbA.CreateNote(&models.Note{
		ID: noteID, ParentID: folderID, Title: "secret note",
		Body: "original body", MarkupLanguage: 1,
		CreatedTime: base, UpdatedTime: base,
		UserCreatedTime: base, UserUpdatedTime: base,
	}); err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	if err := dbA.UpsertResource(&models.Resource{
		ID: resID, Title: "pic.png", Mime: "image/png", Filename: "pic.png",
		CreatedTime: base, UpdatedTime: base,
		UserCreatedTime: base, UserUpdatedTime: base,
		Size: int64(len(blob)), BlobUpdatedTime: base,
	}); err != nil {
		t.Fatalf("UpsertResource: %v", err)
	}
	if err := dbA.SaveResourceFile(resID, blob); err != nil {
		t.Fatalf("SaveResourceFile: %v", err)
	}

	engA := NewEngine(NewLocalFolderBackend(remote), dbA, password)
	ctx := context.Background()
	if err := engA.Sync(ctx); err != nil {
		t.Fatalf("A sync (push encrypted): %v", err)
	}

	// On the target: the resource item is an envelope without the title in
	// the clear, and the blob is a JED01 (FileV1) packet.
	metaBytes, err := os.ReadFile(filepath.Join(remote, resID+".md"))
	if err != nil {
		t.Fatalf("resource metadata not pushed: %v", err)
	}
	if !strings.Contains(string(metaBytes), "encryption_applied: 1") {
		t.Fatalf("resource metadata is not an encrypted envelope:\n%s", metaBytes)
	}
	if strings.Contains(string(metaBytes), "pic.png") {
		t.Fatalf("resource title leaked into the envelope:\n%s", metaBytes)
	}
	remoteBlob, err := os.ReadFile(filepath.Join(remote, ".resource", resID))
	if err != nil {
		t.Fatalf("resource blob not pushed: %v", err)
	}
	if !bytes.HasPrefix(remoteBlob, []byte("JED01")) {
		t.Fatalf("pushed blob is not FileV1-encrypted (starts %q)", remoteBlob[:min(8, len(remoteBlob))])
	}
	noteBytes, err := os.ReadFile(filepath.Join(remote, noteID+".md"))
	if err != nil {
		t.Fatalf("note not pushed: %v", err)
	}
	if strings.Contains(string(noteBytes), "original body") {
		t.Fatalf("note body leaked in the clear:\n%s", noteBytes)
	}

	// info.json carries the key, so client B can join without out-of-band key
	// exchange: pull -> LoadMasterKey(password) -> decrypt everything.
	engB := NewEngine(NewLocalFolderBackend(remote), dbB, password)
	if err := engB.Sync(ctx); err != nil {
		t.Fatalf("B sync (pull + decrypt): %v", err)
	}

	note, err := dbB.GetNote(noteID)
	if err != nil || note == nil {
		t.Fatalf("B has no note (note=%v err=%v)", note, err)
	}
	if note.Title != "secret note" || note.Body != "original body" || note.EncryptionApplied != 0 {
		t.Fatalf("B note not decrypted correctly: %+v", note)
	}
	res, err := dbB.GetResource(resID)
	if err != nil || res == nil {
		t.Fatalf("B has no resource (res=%v err=%v)", res, err)
	}
	if res.Title != "pic.png" || res.EncryptionApplied != 0 || res.EncryptionBlobEncrypted != 0 {
		t.Fatalf("B resource not decrypted correctly: %+v", res)
	}
	gotBlob, err := os.ReadFile(dbB.GetResourceFile(resID))
	if err != nil {
		t.Fatalf("B blob missing: %v", err)
	}
	if !bytes.Equal(gotBlob, blob) {
		t.Fatalf("B blob = %q, want the plaintext %q", gotBlob, blob)
	}
}

// TestE2EE_EditKeepsUnknownKeys pins the decrypted raw sidecar: after an
// encrypted pull, the raw copy must become the decrypted plaintext, so an
// edit re-serializes over it and unknown metadata keys (Joplin's is_locked
// etc.) survive verbatim through decrypt -> edit -> re-encrypt. The envelope
// the pull stored cannot serve this purpose: its unknown keys are inside the
// cipher text.
func TestE2EE_EditKeepsUnknownKeys(t *testing.T) {
	const (
		password = "test-password"
		keyID    = "0123456789abcdef0123456789abcdef"
		noteID   = "0d28d893565c4b2989301f2259164273"
		base     = int64(1772000000000)
	)

	remote := t.TempDir()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// A foreign client (Joplin) pushed an encrypted note whose plaintext
	// carries an unknown-to-us metadata key.
	svc := e2ee.NewService()
	if err := svc.LoadMasterKey(keyID, buildTestKeyV1Packet(t, password), password, e2ee.MethodKeyV1); err != nil {
		t.Fatalf("LoadMasterKey: %v", err)
	}
	note := &models.Note{
		ID: noteID, Title: "locked note", Body: "original body",
		MarkupLanguage: 1, CreatedTime: base, UpdatedTime: base,
		UserCreatedTime: base, UserUpdatedTime: base,
	}
	plain := models.SerializeNote(note) + "\nis_locked: 1"
	cipherText, err := svc.EncryptString(keyID, plain)
	if err != nil {
		t.Fatalf("EncryptString: %v", err)
	}
	fields := models.NoteFields(note)
	fields["type_"] = "1"
	envelope := models.SerializeEncryptedEnvelope(envelopeMeta(fields, cipherText, keyID))
	if err := os.WriteFile(filepath.Join(remote, noteID+".md"), []byte(envelope), 0o644); err != nil {
		t.Fatal(err)
	}
	info := `{"version":3,"e2ee":{"value":true,"updatedTime":` + itoa(base) + `},` +
		`"activeMasterKeyId":{"value":"` + keyID + `","updatedTime":` + itoa(base) + `},` +
		`"masterKeys":[{"id":"` + keyID + `","created_time":` + itoa(base) + `,"updated_time":` + itoa(base) + `,` +
		`"source_application":"joplin-desktop","encryption_method":` + itoa(int64(e2ee.MethodKeyV1)) + `,` +
		`"checksum":"","content":` + jsonString(buildTestKeyV1Packet(t, password)) + `}],"appMinVersion":"3.7.0"}`
	if err := os.WriteFile(filepath.Join(remote, "info.json"), []byte(info), 0o644); err != nil {
		t.Fatal(err)
	}

	// Pull + decrypt.
	eng := NewEngine(NewLocalFolderBackend(remote), db, password)
	ctx := context.Background()
	if err := eng.Sync(ctx); err != nil {
		t.Fatalf("sync (pull + decrypt): %v", err)
	}
	got, err := db.GetNote(noteID)
	if err != nil || got == nil {
		t.Fatalf("note not decrypted (note=%v err=%v)", got, err)
	}
	if got.Body != "original body" {
		t.Fatalf("decrypted body = %q", got.Body)
	}

	// Edit and push again.
	got.Body = "edited body"
	if err := db.UpdateNote(got); err != nil {
		t.Fatalf("UpdateNote: %v", err)
	}
	if err := eng.Sync(ctx); err != nil {
		t.Fatalf("sync (push edit): %v", err)
	}

	// The pushed envelope must decrypt to the edited body AND the unknown key.
	pushed, err := os.ReadFile(filepath.Join(remote, noteID+".md"))
	if err != nil {
		t.Fatal(err)
	}
	_, fields2 := models.Deserialize(string(pushed))
	ct := fields2["encryption_cipher_text"]
	if ct == "" {
		t.Fatalf("pushed note is not an encrypted envelope:\n%s", pushed)
	}
	dec, err := svc.DecryptString(ct)
	if err != nil {
		t.Fatalf("cannot decrypt pushed note: %v", err)
	}
	if !strings.Contains(dec, "edited body") {
		t.Errorf("pushed plaintext lost the edit:\n%s", dec)
	}
	if !strings.Contains(dec, "is_locked: 1") {
		t.Errorf("pushed plaintext lost the unknown metadata key:\n%s", dec)
	}
}

func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// TestE2EE_ResourceBlobStaysDecryptedOnRedelivery pins what a second pull does
// to an already-decrypted resource. A file target re-lists every item, so the
// encrypted blob arrives again on the next pass; storing it without recording
// that the stored bytes are encrypted would leave the vault serving ciphertext,
// because the blob-decrypt pass only looks at rows marked
// encryption_blob_encrypted = 1.
func TestE2EE_ResourceBlobStaysDecryptedOnRedelivery(t *testing.T) {
	const (
		password = "redelivery-password"
		keyID    = "0123456789abcdef0123456789abcdef"
		resID    = "5b2c9e1d4a6f80b3c5d7e9f1a3b5c7d9"
		base     = int64(1772000000000)
	)
	blob := []byte("blob that must survive a second pull \x00\x01\xff")

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
	ctx := context.Background()

	if err := dbA.UpsertMasterKey(&models.MasterKey{
		ID: keyID, CreatedTime: base, UpdatedTime: base,
		SourceApplication: "joplin-desktop",
		EncryptionMethod:  e2ee.MethodKeyV1,
		Content:           buildTestKeyV1Packet(t, password),
	}); err != nil {
		t.Fatalf("UpsertMasterKey: %v", err)
	}
	if err := dbA.SetActiveMasterKeyID(keyID); err != nil {
		t.Fatalf("SetActiveMasterKeyID: %v", err)
	}
	if err := dbA.UpsertResource(&models.Resource{
		ID: resID, Title: "pic.png", Mime: "image/png", Filename: "pic.png",
		CreatedTime: base, UpdatedTime: base,
		UserCreatedTime: base, UserUpdatedTime: base,
		Size: int64(len(blob)), BlobUpdatedTime: base,
	}); err != nil {
		t.Fatalf("UpsertResource: %v", err)
	}
	if err := dbA.SaveResourceFile(resID, blob); err != nil {
		t.Fatalf("SaveResourceFile: %v", err)
	}
	if err := NewEngine(NewLocalFolderBackend(remote), dbA, password).Sync(ctx); err != nil {
		t.Fatalf("A sync: %v", err)
	}

	engB := NewEngine(NewLocalFolderBackend(remote), dbB, password)
	if err := engB.Sync(ctx); err != nil {
		t.Fatalf("B sync #1: %v", err)
	}
	got, err := os.ReadFile(dbB.GetResourceFile(resID))
	if err != nil {
		t.Fatalf("B blob after sync #1: %v", err)
	}
	if !bytes.Equal(got, blob) {
		t.Fatalf("B blob after sync #1 = %q, want the plaintext", got)
	}

	// The target has not changed; a second pass must leave the vault as usable
	// as the first one did.
	if err := engB.Sync(ctx); err != nil {
		t.Fatalf("B sync #2: %v", err)
	}
	got, err = os.ReadFile(dbB.GetResourceFile(resID))
	if err != nil {
		t.Fatalf("B blob after sync #2: %v", err)
	}
	if !bytes.Equal(got, blob) {
		t.Fatalf("B blob after sync #2 = %q, want the plaintext (ciphertext was stored and never decrypted)", got)
	}
	res, err := dbB.GetResource(resID)
	if err != nil || res == nil {
		t.Fatalf("B resource after sync #2: %v (%v)", res, err)
	}
	if res.EncryptionBlobEncrypted != 0 || res.EncryptionApplied != 0 {
		t.Fatalf("B resource after sync #2 = %+v, want a plaintext resource", res)
	}
}
