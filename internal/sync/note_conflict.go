package sync

import (
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/lsj5031/joplin-lite/internal/e2ee"
	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/internal/store"
)

var errUncertainNoteConflict = errors.New("cannot safely compare concurrent note revisions; unlock the encryption key or check the item")

func encryptedNote(n *models.Note) bool {
	return n.EncryptionApplied != 0 || n.EncryptionCipherText != ""
}

// resolveNoteConflict runs before changing the note or recording its revision.
// If either encrypted side cannot be authenticated and parsed, leave both
// originals in place and retry after the key or the remote item is repaired.
func resolveNoteConflict(db *store.DB, svc *e2ee.Service, local, remote *models.Note, content []byte) error {
	if local.IsConflict == 1 && !encryptedNote(local) {
		return nil // Existing conflict notes are not duplicated.
	}
	remoteRaw, err := conflictNoteRaw(content, remote)
	if err != nil {
		return err
	}
	var localRaw *models.RawItem
	if encryptedNote(local) {
		var plaintext []byte
		local, plaintext, err = decryptConflictNote(svc, local)
		if err == nil {
			localRaw, err = conflictNoteRaw(plaintext, local)
		}
	} else {
		var raw []byte
		raw, _, _, err = db.GetItemRaw(local.ID)
		if err == nil && len(raw) != 0 {
			localRaw, err = conflictNoteRaw(raw, local)
		}
	}
	if err != nil {
		return err
	}
	if local.IsConflict != 1 && (meaningfulNote(*local) != meaningfulNote(*remote) ||
		!maps.Equal(unknownNoteFields(localRaw), unknownNoteFields(remoteRaw))) {
		if _, err := saveNoteConflictCopy(db, local, true, localRaw); err != nil {
			return fmt.Errorf("cannot keep conflict copy of note %s: %w", remote.ID, err)
		}
		slog.Info("note conflict: local version kept as conflict note, remote is canonical", "id", remote.ID)
	}
	return nil
}

func meaningfulNote(n models.Note) models.Note {
	// Ignore revision bookkeeping and the key used to encrypt the revision,
	// not note state (including folder, to-do, trash, order and sharing).
	n.ID, n.MasterKeyID = "", ""
	n.CreatedTime, n.UpdatedTime, n.UserCreatedTime, n.UserUpdatedTime = 0, 0, 0, 0
	n.Type_ = 0
	return n
}

func unknownNoteFields(raw *models.RawItem) map[string]string {
	if raw == nil {
		return nil
	}
	known := models.NoteFields(&models.Note{})
	fields := make(map[string]string)
	for _, m := range raw.Meta {
		if _, ok := known[m.Key]; !ok {
			fields[m.Key] = m.Value
		}
	}
	return fields
}

func decryptConflictNote(svc *e2ee.Service, envelope *models.Note) (*models.Note, []byte, error) {
	if svc == nil || envelope.EncryptionApplied != 1 || envelope.EncryptionCipherText == "" {
		return nil, nil, errUncertainNoteConflict
	}
	plain, err := svc.DecryptString(envelope.EncryptionCipherText)
	if err != nil {
		return nil, nil, errUncertainNoteConflict
	}
	note := models.DeserializeNote(plain)
	if encryptedNote(note) || note.ID != envelope.ID || note.UpdatedTime != envelope.UpdatedTime {
		return nil, nil, errUncertainNoteConflict
	}
	return note, []byte(plain), nil
}

// The normal parser tolerates missing fields and can map invalid numbers to
// zero. Never use that fallback as proof that two revisions are equivalent.
// Do not propagate parser errors here: they can quote decrypted input.
func conflictNoteRaw(content []byte, note *models.Note) (*models.RawItem, error) {
	raw, err := models.ParseItem(string(content))
	if err != nil || raw.Type != models.TypeNote {
		return nil, errUncertainNoteConflict
	}
	_, fields := models.Deserialize(string(content))
	seen := make(map[string]bool, len(raw.Meta))
	for _, m := range raw.Meta {
		key, _, hasSeparator := strings.Cut(m.Raw, ": ")
		value, present := fields[m.Key]
		if !hasSeparator || key != m.Key || key == "" || key == "title" || key == "body" ||
			seen[key] || !present || value != m.Value || !validConflictField(key, value) {
			return nil, errUncertainNoteConflict
		}
		seen[key] = true
	}
	// Raw local metadata can be from before the most recent user edit. Bind
	// it to the same note, but use current row values for all known fields.
	if fields["id"] != note.ID || fields["updated_time"] == "" || fields["type_"] != "1" ||
		fields["encryption_cipher_text"] != "" || (fields["encryption_applied"] != "" && fields["encryption_applied"] != "0") ||
		raw.Title != fields["title"] || raw.Body != fields["body"] ||
		(raw.HasHead && strings.Contains(raw.Head, "\n") && !strings.HasPrefix(raw.Head[len(raw.Title):], "\n\n")) {
		return nil, errUncertainNoteConflict
	}
	return raw, nil
}

func validConflictField(key, value string) bool {
	if value == "" {
		return true // Optional fields may be omitted or empty.
	}
	switch key {
	case "is_conflict", "is_todo", "encryption_applied", "markup_language", "is_shared":
		_, err := strconv.Atoi(value)
		return err == nil
	case "order", "latitude", "longitude", "altitude":
		f, err := strconv.ParseFloat(value, 64)
		return err == nil && !math.IsNaN(f) && !math.IsInf(f, 0)
	case "created_time", "updated_time", "user_created_time", "user_updated_time", "todo_due", "todo_completed", "deleted_time":
		if _, err := strconv.ParseInt(value, 10, 64); err == nil {
			return true
		}
		_, err := time.Parse(time.RFC3339Nano, value)
		return err == nil
	default:
		return true
	}
}
