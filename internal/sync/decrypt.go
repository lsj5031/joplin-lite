package sync

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/lsj5031/joplin-lite/internal/e2ee"
	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/internal/store"
)

// DecryptPulledItems loads master keys, then decrypts all items with encryption_applied = 1.
func DecryptPulledItems(db *store.DB, svc *e2ee.Service, masterPassword string) error {
	if masterPassword == "" {
		slog.Debug("no master password configured, skipping decryption")
		return nil
	}

	// Load master keys
	masterKeys, err := db.ListMasterKeys()
	if err != nil {
		return fmt.Errorf("cannot list master keys: %w", err)
	}

	if len(masterKeys) == 0 {
		slog.Warn("no master keys in database, skipping decryption")
		return nil
	}

	slog.Info("found master keys in database", "count", len(masterKeys))

	for _, mk := range masterKeys {
		if svc.HasMasterKey(mk.ID) {
			continue
		}
		if err := svc.LoadMasterKey(mk.ID, mk.Content, masterPassword, mk.EncryptionMethod); err != nil {
			slog.Error("cannot decrypt master key", "id", mk.ID, "error", err)
			continue
		}
		slog.Info("loaded master key", "id", mk.ID)
	}

	// Decrypt items with encryption_applied = 1
	items, err := db.GetEncryptedItems()
	if err != nil {
		return fmt.Errorf("cannot get encrypted items: %w", err)
	}

	slog.Info("decrypting items", "count", len(items))

	for _, item := range items {
		if err := decryptItem(db, svc, item); err != nil {
			slog.Error("failed to decrypt item", "id", item.ID, "type", item.ItemType, "error", err)
			continue
		}
	}

	// Decrypt resource blobs
	encResources, err := db.GetEncryptedResourceBlobs()
	if err != nil {
		return fmt.Errorf("cannot get encrypted resource blobs: %w", err)
	}

	for _, er := range encResources {
		if err := decryptResourceBlob(db, svc, er.ID); err != nil {
			slog.Error("failed to decrypt resource blob", "id", er.ID, "error", err)
			continue
		}
	}

	return nil
}

func decryptItem(db *store.DB, svc *e2ee.Service, item store.EncryptedItem) error {
	plaintext, err := svc.DecryptString(item.CipherText)
	if err != nil {
		return err
	}

	// The decrypted text is in Joplin's serialized format — re-deserialize and update
	switch item.ItemType {
	case models.TypeNote:
		note := models.DeserializeNote(plaintext)
		note.ID = item.ID
		note.EncryptionCipherText = ""
		note.EncryptionApplied = 0
		if err := db.UpsertNote(note); err != nil {
			return err
		}
		return saveDecryptedRaw(db, item.ID, plaintext, note.UpdatedTime)

	case models.TypeFolder:
		folder := models.DeserializeFolder(plaintext)
		folder.ID = item.ID
		folder.EncryptionCipherText = ""
		folder.EncryptionApplied = 0
		if err := db.UpsertFolder(folder); err != nil {
			return err
		}
		return saveDecryptedRaw(db, item.ID, plaintext, folder.UpdatedTime)

	case models.TypeTag:
		tag := models.DeserializeTag(plaintext)
		tag.ID = item.ID
		tag.EncryptionCipherText = ""
		tag.EncryptionApplied = 0
		if err := db.UpsertTag(tag); err != nil {
			return err
		}
		return saveDecryptedRaw(db, item.ID, plaintext, tag.UpdatedTime)

	case models.TypeRevision:
		// Note-history envelope: the plaintext carries the index fields
		// (item_id, diffs) that were inside the cipher text.
		rev := models.DeserializeRevision(plaintext)
		if rev == nil {
			return fmt.Errorf("cannot parse decrypted revision %s", item.ID)
		}
		rev.ID = item.ID
		rev.EncryptionCipherText = ""
		rev.EncryptionApplied = 0
		if err := db.UpsertRevision(rev); err != nil {
			return err
		}
		return saveDecryptedRaw(db, item.ID, plaintext, rev.UpdatedTime)

	case models.TypeResource:
		resource := models.DeserializeResource(plaintext)
		resource.ID = item.ID
		resource.EncryptionCipherText = ""
		resource.EncryptionApplied = 0
		// encryption_blob_encrypted stays as decrypted from the plaintext: it
		// marks the blob at the target as FileV1-encrypted and drives the blob
		// decryption pass below.
		if err := db.UpsertResource(resource); err != nil {
			return err
		}
		return saveDecryptedRaw(db, item.ID, plaintext, resource.UpdatedTime)

	default:
		return fmt.Errorf("unsupported item type %d for decryption", item.ItemType)
	}
}

// saveDecryptedRaw replaces the raw sidecar with the decrypted plaintext.
// The pull path stored the envelope bytes; keeping those would make the
// merge path re-serialize an envelope as if it were a plaintext item (and
// push would encrypt it twice). The plaintext is the canonical source for
// later re-serialization — same role the raw copy has for plain pulls.
func saveDecryptedRaw(db *store.DB, id, plaintext string, updatedTime int64) error {
	if err := db.SaveItemRaw(id, []byte(plaintext), updatedTime); err != nil {
		slog.Error("failed to store decrypted raw copy", "id", id, "error", err)
	}
	return nil
}

func decryptResourceBlob(db *store.DB, svc *e2ee.Service, resourceID string) error {
	// Read the encrypted blob from disk
	blobPath := filepath.Join(db.ResourceDir(), resourceID)
	data, err := os.ReadFile(blobPath)
	if err != nil {
		return fmt.Errorf("cannot read resource blob: %w", err)
	}

	// Only a FileV1 envelope can be decrypted. Anything else is already the
	// plaintext, and the row must stop claiming otherwise, or every later sync
	// would keep trying it.
	if !bytes.HasPrefix(data, []byte(fileV1Prefix)) {
		slog.Debug("resource blob is not a FileV1 envelope; nothing to decrypt", "id", resourceID)
		resource, err := db.GetResource(resourceID)
		if err != nil || resource == nil || resource.EncryptionBlobEncrypted == 0 {
			return err
		}
		resource.EncryptionBlobEncrypted = 0
		return db.UpsertResource(resource)
	}

	decrypted, err := svc.DecryptFile(string(data))
	if err != nil {
		return err
	}

	// Write decrypted blob back
	if err := os.WriteFile(blobPath, decrypted, 0o644); err != nil {
		return fmt.Errorf("cannot write decrypted resource blob: %w", err)
	}

	// Update the resource record
	resource, err := db.GetResource(resourceID)
	if err != nil {
		return err
	}
	if resource != nil {
		resource.EncryptionBlobEncrypted = 0
		resource.Size = int64(len(decrypted))
		return db.UpsertResource(resource)
	}

	return nil
}
