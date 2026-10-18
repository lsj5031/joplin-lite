// Command e2eeseed creates a local vault that already owns a KeyV1 master key,
// which is what makes a vault the E2EE owner: Joplin desktop generates that key
// when a user enables encryption, and joplin-lite joins an existing library
// rather than creating one, so a fresh vault cannot encrypt on its own.
//
// It is a rehearsal helper for the config-file path in
// docs/e2ee-sync-rehearsal.md and for qa/local-sync-e2e.sh:
//
//	go run ./tools/e2eeseed -data-dir tmp/vault-owner -password 'secret' \
//	  -title "encrypted note" -body "the encrypted body"
//
// The vault is then pushed encrypted by the normal CLI:
//
//	GOJOPLIN_MASTER_PASSWORD='secret' gojoplin sync --config owner.yaml
//
// It prints the ids it created (master key, folder, note, resource) as
// key=value lines. Use it only with a throwaway data directory.
package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"golang.org/x/crypto/pbkdf2"

	"github.com/lsj5031/joplin-lite/internal/e2ee"
	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/internal/store"
)

// keyID is fixed so a rerun of a rehearsal produces the same master key id.
const keyID = "0123456789abcdef0123456789abcdef"

func main() {
	dataDir := flag.String("data-dir", "", "vault directory to create or open (required)")
	password := flag.String("password", "", "master password the key is encrypted under (required)")
	title := flag.String("title", "encrypted note", "note title")
	body := flag.String("body", "the encrypted body", "note body")
	folderTitle := flag.String("folder", "E2EE rehearsal", "notebook title")
	withResource := flag.Bool("resource", true, "also create a resource, to cover encrypted blobs")
	flag.Parse()

	if *dataDir == "" || *password == "" {
		fmt.Fprintln(os.Stderr, "usage: e2eeseed -data-dir <vault> -password <master password> [-title T] [-body B]")
		os.Exit(2)
	}

	db, err := store.Open(*dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2eeseed: open %s: %v\n", *dataDir, err)
		os.Exit(1)
	}
	defer db.Close()

	now := time.Now().UnixMilli()
	if err := db.UpsertMasterKey(&models.MasterKey{
		ID: keyID, CreatedTime: now, UpdatedTime: now,
		SourceApplication: "joplin-desktop",
		EncryptionMethod:  e2ee.MethodKeyV1,
		Content:           keyV1Packet(*password),
	}); err != nil {
		fatal("store master key", err)
	}
	if err := db.SetActiveMasterKeyID(keyID); err != nil {
		fatal("set active master key", err)
	}

	folder := &models.Folder{
		ID: "ccccccccccccccccccccccccccccccc1", Title: *folderTitle,
		CreatedTime: now, UpdatedTime: now, UserCreatedTime: now, UserUpdatedTime: now,
	}
	if err := db.CreateFolder(folder); err != nil {
		fatal("create folder", err)
	}
	note := &models.Note{
		ID: "dddddddddddddddddddddddddddddddd", ParentID: folder.ID,
		Title: *title, Body: *body,
		MarkupLanguage: 1, CreatedTime: now, UpdatedTime: now,
		UserCreatedTime: now, UserUpdatedTime: now,
	}
	if err := db.CreateNote(note); err != nil {
		fatal("create note", err)
	}

	fmt.Printf("master_key=%s\nfolder=%s\nnote=%s\n", keyID, folder.ID, note.ID)
	if *withResource {
		const resID = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
		blob := []byte("encrypted resource blob\x00\x01\xff")
		if err := db.UpsertResource(&models.Resource{
			ID: resID, Title: "pic.png", Mime: "image/png", Filename: "pic.png",
			CreatedTime: now, UpdatedTime: now, UserCreatedTime: now, UserUpdatedTime: now,
			Size: int64(len(blob)), BlobUpdatedTime: now,
		}); err != nil {
			fatal("create resource", err)
		}
		if err := db.SaveResourceFile(resID, blob); err != nil {
			fatal("save resource file", err)
		}
		fmt.Printf("resource=%s\n", resID)
	}
}

func fatal(what string, err error) {
	fmt.Fprintf(os.Stderr, "e2eeseed: %s: %v\n", what, err)
	os.Exit(1)
}

// keyV1Packet builds a KeyV1 master-key packet encrypted under password
// (PBKDF2-HMAC-SHA512 220k, AES-256-GCM), the shape e2ee.LoadMasterKey reads.
func keyV1Packet(password string) string {
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
		panic(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	ct := gcm.Seal(nil, iv, rawKey, nil)
	packet, err := json.Marshal(map[string]string{
		"salt": base64.StdEncoding.EncodeToString(salt),
		"iv":   base64.StdEncoding.EncodeToString(iv),
		"ct":   base64.StdEncoding.EncodeToString(ct),
	})
	if err != nil {
		panic(err)
	}
	return string(packet)
}
