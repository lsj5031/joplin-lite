package models

// Revision is a note-history entry (ModelType 13). On the sync target a
// revision item is a metadata-only block whose keys are the Revision model's
// fields (verified against packages/lib/models/Revision.ts): the content of
// the historical note version is carried as diff fields, not as the title/
// body head section.
//
// joplin-lite stores revisions raw (item_raw) and re-pushes the exact bytes;
// this struct only indexes what listing/filtering needs. There is no
// serializer on purpose: re-serializing a revision would risk mangling the
// patch fields, and losslessness is by construction.

// Revision fields — see the note above. On an encrypted target the item
// arrives as an envelope: only id/parent_id/updated_time are in the clear
// and the index fields are filled after the decrypt pass.
type Revision struct {
	ID                   string `json:"id"`
	ParentID             string `json:"parent_id"` // previous revision in the chain
	ItemID               string `json:"item_id"`
	ItemType             int    `json:"item_type"`
	ItemUpdatedTime      int64  `json:"item_updated_time"`
	CreatedTime          int64  `json:"created_time"`
	UpdatedTime          int64  `json:"updated_time"`
	EncryptionCipherText string `json:"encryption_cipher_text"`
	EncryptionApplied    int    `json:"encryption_applied"`
}

// DeserializeRevision parses a serialized revision item (or its encrypted
// envelope — the index fields that survive in the clear are filled in).
func DeserializeRevision(content string) *Revision {
	_, fields := Deserialize(content)
	return RevisionFromFields(fields)
}

// RevisionFromFields maps parsed metadata fields to a Revision.
func RevisionFromFields(fields map[string]string) *Revision {
	r := &Revision{
		ID:                   fields["id"],
		ParentID:             fields["parent_id"],
		ItemID:               fields["item_id"],
		ItemType:             parseInt(fields["item_type"]),
		ItemUpdatedTime:      parseInt64(fields["item_updated_time"]),
		CreatedTime:          parseTime(fields["created_time"]),
		UpdatedTime:          parseTime(fields["updated_time"]),
		EncryptionCipherText: fields["encryption_cipher_text"],
		EncryptionApplied:    parseInt(fields["encryption_applied"]),
	}
	if r.ID == "" {
		return nil
	}
	return r
}

// TypedMetaValue converts a parsed metadata value to its API entity type,
// following the verified serialization rules: created_time/updated_time/
// user_created_time/user_updated_time are ISO in the file but unix ms in the
// API; item_updated_time and friends are plain ms numbers; type/item flags
// are ints; everything else stays a string.
func TypedMetaValue(key, value string) interface{} {
	switch key {
	case "created_time", "updated_time", "user_created_time", "user_updated_time":
		return parseTime(value)
	case "item_updated_time", "deleted_time", "todo_due", "todo_completed", "blob_updated_time", "order":
		return parseInt64(value)
	case "type_", "item_type", "encryption_applied", "encryption_blob_encrypted",
		"is_conflict", "is_shared", "is_todo", "is_locked", "markup_language", "encryption_method":
		return parseInt(value)
	}
	return value
}
