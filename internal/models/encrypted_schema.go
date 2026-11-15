package models

import "strings"

// SQLite column order from the official 3.7.22 bundle's executed migrations.
// Includes fields our typed models do not carry (notably resource OCR fields).
// Provenance and offline regeneration: testdata/official-envelope/README.md.
var encryptedSchema = map[int][]string{
	TypeNote:     strings.Split("id,parent_id,title,body,created_time,updated_time,is_conflict,latitude,longitude,altitude,author,source_url,is_todo,todo_due,todo_completed,source,source_application,application_data,order,user_created_time,user_updated_time,encryption_cipher_text,encryption_applied,markup_language,is_shared,share_id,conflict_original_id,master_key_id,user_data,deleted_time,is_locked,extracted_resource_ids", ","),
	TypeFolder:   strings.Split("id,title,created_time,updated_time,user_created_time,user_updated_time,encryption_cipher_text,encryption_applied,parent_id,is_shared,share_id,master_key_id,icon,user_data,deleted_time", ","),
	TypeResource: strings.Split("id,title,mime,filename,created_time,updated_time,user_created_time,user_updated_time,file_extension,encryption_cipher_text,encryption_applied,encryption_blob_encrypted,size,is_shared,share_id,master_key_id,user_data,blob_updated_time,ocr_text,ocr_details,ocr_status,ocr_error,ocr_driver_id,is_locked", ","),
	TypeTag:      strings.Split("id,title,created_time,updated_time,user_created_time,user_updated_time,encryption_cipher_text,encryption_applied,is_shared,parent_id,user_data", ","),
	TypeNoteTag:  strings.Split("id,note_id,tag_id,created_time,updated_time,user_created_time,user_updated_time,encryption_cipher_text,encryption_applied,is_shared", ","),
}

// EncryptedEnvelopeMissing returns only absent schema metadata, with blank
// values. Existing and unknown metadata must be preserved by repair callers.
func EncryptedEnvelopeMissing(itemType int, present map[string]string) (map[string]string, bool) {
	keys, ok := encryptedSchema[itemType]
	missing := make(map[string]string)
	for _, key := range keys {
		if key == "title" || key == "body" {
			continue
		}
		if _, exists := present[key]; !exists {
			missing[key] = ""
		}
	}
	return missing, ok
}
