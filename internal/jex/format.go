// Package jex reads and writes Joplin's JEX export format.
//
// A JEX file is an uncompressed tar archive holding Joplin's "raw" export
// layout: one <id>.md file per item at the archive root, plus a resources/
// directory with attachment bytes named <id>.<file_extension>. Each item file
// is a title line, a blank line, the note body (notes only), a blank line, and
// a block of "property: value" lines ending in "type_".
//
// The layout and escaping rules here mirror Joplin's
// BaseItem.serialize/unserialize so that archives stay interchangeable.
package jex

import (
	"archive/tar"
	"fmt"
	"io"
	"math"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/lsj5031/joplin-lite/internal/models"
)

// maxItemBytes bounds how much of a single item file is read, so a corrupt
// archive cannot exhaust memory.
const maxItemBytes = 256 << 20

// Archive is the item content of a JEX file. Attachment bytes are not read;
// use ExtractResources for those.
type Archive struct {
	Notes     []*models.Note
	Folders   []*models.Folder
	Resources []*models.Resource
	Tags      []*models.Tag
	NoteTags  []*models.NoteTag
	Warnings  []string
}

// ReadArchive parses the item files of a JEX archive. Resource bytes are
// skipped; callers that need them call ExtractResources on the same file.
func ReadArchive(r io.Reader) (*Archive, error) {
	tr := tar.NewReader(r)
	a := &Archive{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read archive: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name := path.Clean(hdr.Name)
		// Item files are the root-level .md entries. Anything nested (such as
		// resources/<id>.md written by older Joplin versions) is not an item.
		if strings.Contains(name, "/") || !strings.HasSuffix(name, ".md") {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(tr, maxItemBytes))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		it, err := parseItem(string(data))
		if err != nil {
			a.Warnings = append(a.Warnings, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		a.add(it, name)
	}
	return a, nil
}

func (a *Archive) add(it item, name string) {
	switch it.typeVal() {
	case models.TypeNote:
		a.Notes = append(a.Notes, it.note())
	case models.TypeFolder:
		a.Folders = append(a.Folders, it.folder())
	case models.TypeResource:
		a.Resources = append(a.Resources, it.resource())
	case models.TypeTag:
		a.Tags = append(a.Tags, it.tag())
	case models.TypeNoteTag:
		a.NoteTags = append(a.NoteTags, it.noteTag())
	default:
		a.Warnings = append(a.Warnings, fmt.Sprintf("%s: unknown type_ %q", name, it.props["type_"]))
	}
}

// item is a parsed item file.
type item struct {
	title string
	body  string
	props map[string]string
}

func parseItem(content string) (item, error) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(content, "\n")
	props := make(map[string]string)
	cut := 0
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			cut = i
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return item{}, fmt.Errorf("invalid property line %q", line)
		}
		props[strings.TrimSpace(key)] = unescapeValue(strings.TrimSpace(value))
	}
	if _, ok := props["type_"]; !ok {
		return item{}, fmt.Errorf("missing type_")
	}
	it := item{props: props}
	if cut > 0 {
		head := lines[:cut]
		it.title = head[0]
		// Joplin drops the title and the blank line that follows it.
		rest := head[1:]
		if len(rest) > 1 {
			rest = rest[1:]
		} else {
			rest = nil
		}
		it.body = strings.Join(rest, "\n")
	}
	return it, nil
}

func (it item) typeVal() int { return atoi(it.props["type_"]) }

func (it item) str(key string) string { return it.props[key] }

func (it item) intVal(key string) int { return atoi(it.props[key]) }

// int64Val reads a millisecond value that Joplin stores as a plain integer
// (todo_due, todo_completed, deleted_time, sizes). Older exports sometimes
// wrote these as ISO dates, so both forms are accepted.
func (it item) int64Val(key string) int64 {
	v := strings.TrimSpace(it.props[key])
	if v == "" {
		return 0
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		return n
	}
	return it.timeVal(key)
}

func (it item) timeVal(key string) int64 {
	v := strings.TrimSpace(it.props[key])
	if v == "" {
		return 0
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.000Z", "2006-01-02T15:04:05Z"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t.UnixMilli()
		}
	}
	return 0
}

func (it item) floatVal(key string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(it.props[key]), 64)
	if err != nil {
		return 0
	}
	return v
}

func (it item) note() *models.Note {
	n := &models.Note{
		ID:                   it.str("id"),
		ParentID:             it.str("parent_id"),
		Title:                it.title,
		Body:                 it.body,
		CreatedTime:          it.timeVal("created_time"),
		UpdatedTime:          it.timeVal("updated_time"),
		IsConflict:           it.intVal("is_conflict"),
		Latitude:             it.floatVal("latitude"),
		Longitude:            it.floatVal("longitude"),
		Altitude:             it.floatVal("altitude"),
		Author:               it.str("author"),
		SourceURL:            it.str("source_url"),
		IsTodo:               it.intVal("is_todo"),
		TodoDue:              it.int64Val("todo_due"),
		TodoCompleted:        it.int64Val("todo_completed"),
		Source:               it.str("source"),
		SourceApplication:    it.str("source_application"),
		ApplicationData:      it.str("application_data"),
		Order:                it.floatVal("order"),
		UserCreatedTime:      it.timeVal("user_created_time"),
		UserUpdatedTime:      it.timeVal("user_updated_time"),
		EncryptionCipherText: it.str("encryption_cipher_text"),
		EncryptionApplied:    it.intVal("encryption_applied"),
		MarkupLanguage:       it.intVal("markup_language"),
		IsShared:             it.intVal("is_shared"),
		ShareID:              it.str("share_id"),
		ConflictOriginalID:   it.str("conflict_original_id"),
		MasterKeyID:          it.str("master_key_id"),
		UserData:             it.str("user_data"),
		DeletedTime:          it.int64Val("deleted_time"),
		Type_:                models.TypeNote,
	}
	return n
}

func (it item) folder() *models.Folder {
	return &models.Folder{
		ID:                   it.str("id"),
		Title:                it.title,
		CreatedTime:          it.timeVal("created_time"),
		UpdatedTime:          it.timeVal("updated_time"),
		UserCreatedTime:      it.timeVal("user_created_time"),
		UserUpdatedTime:      it.timeVal("user_updated_time"),
		EncryptionCipherText: it.str("encryption_cipher_text"),
		EncryptionApplied:    it.intVal("encryption_applied"),
		ParentID:             it.str("parent_id"),
		IsShared:             it.intVal("is_shared"),
		ShareID:              it.str("share_id"),
		MasterKeyID:          it.str("master_key_id"),
		Icon:                 it.str("icon"),
		UserData:             it.str("user_data"),
		DeletedTime:          it.int64Val("deleted_time"),
		Type_:                models.TypeFolder,
	}
}

func (it item) resource() *models.Resource {
	return &models.Resource{
		ID:                      it.str("id"),
		Title:                   it.title,
		Mime:                    it.str("mime"),
		Filename:                it.str("filename"),
		CreatedTime:             it.timeVal("created_time"),
		UpdatedTime:             it.timeVal("updated_time"),
		UserCreatedTime:         it.timeVal("user_created_time"),
		UserUpdatedTime:         it.timeVal("user_updated_time"),
		FileExtension:           it.str("file_extension"),
		EncryptionCipherText:    it.str("encryption_cipher_text"),
		EncryptionApplied:       it.intVal("encryption_applied"),
		EncryptionBlobEncrypted: it.intVal("encryption_blob_encrypted"),
		Size:                    it.int64Val("size"),
		IsShared:                it.intVal("is_shared"),
		ShareID:                 it.str("share_id"),
		MasterKeyID:             it.str("master_key_id"),
		UserData:                it.str("user_data"),
		BlobUpdatedTime:         it.int64Val("blob_updated_time"),
		Type_:                   models.TypeResource,
	}
}

func (it item) tag() *models.Tag {
	return &models.Tag{
		ID:                   it.str("id"),
		Title:                it.title,
		CreatedTime:          it.timeVal("created_time"),
		UpdatedTime:          it.timeVal("updated_time"),
		UserCreatedTime:      it.timeVal("user_created_time"),
		UserUpdatedTime:      it.timeVal("user_updated_time"),
		EncryptionCipherText: it.str("encryption_cipher_text"),
		EncryptionApplied:    it.intVal("encryption_applied"),
		IsShared:             it.intVal("is_shared"),
		ParentID:             it.str("parent_id"),
		UserData:             it.str("user_data"),
		Type_:                models.TypeTag,
	}
}

func (it item) noteTag() *models.NoteTag {
	return &models.NoteTag{
		ID:                   it.str("id"),
		NoteID:               it.str("note_id"),
		TagID:                it.str("tag_id"),
		CreatedTime:          it.timeVal("created_time"),
		UpdatedTime:          it.timeVal("updated_time"),
		UserCreatedTime:      it.timeVal("user_created_time"),
		UserUpdatedTime:      it.timeVal("user_updated_time"),
		EncryptionCipherText: it.str("encryption_cipher_text"),
		EncryptionApplied:    it.intVal("encryption_applied"),
		IsShared:             it.intVal("is_shared"),
		Type_:                models.TypeNoteTag,
	}
}

// ExtractResources copies resources/<id>.<ext> entries to fn as it walks the
// archive. Item metadata is not parsed here, so this is a second pass over an
// open archive file.
func ExtractResources(r io.Reader, fn func(name string, data io.Reader) error) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read archive: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name := path.Clean(hdr.Name)
		if !strings.HasPrefix(name, "resources/") || strings.Contains(strings.TrimPrefix(name, "resources/"), "/") {
			continue
		}
		if err := fn(path.Base(name), tr); err != nil {
			return err
		}
	}
}

// --- serialization ---

type field struct {
	key   string
	value string
}

func kv(key, value string) field { return field{key: key, value: value} }

func strField(key, value string) field { return field{key: key, value: escapeValue(value)} }

func intField(key string, value int) field {
	return field{key: key, value: strconv.Itoa(value)}
}

func int64Field(key string, value int64) field {
	return field{key: key, value: strconv.FormatInt(value, 10)}
}

func timeField(key string, ms int64) field {
	if ms == 0 {
		return field{key: key, value: ""}
	}
	return field{key: key, value: time.UnixMilli(ms).UTC().Format("2006-01-02T15:04:05.000Z")}
}

func coordField(key string, v float64, places int) field {
	if v == 0 || math.IsNaN(v) {
		v = 0
	}
	return field{key: key, value: strconv.FormatFloat(v, 'f', places, 64)}
}

func floatField(key string, v float64) field {
	return field{key: key, value: strconv.FormatFloat(v, 'f', -1, 64)}
}

func joinProps(fields []field) string {
	lines := make([]string, 0, len(fields))
	for _, f := range fields {
		lines = append(lines, f.key+": "+f.value)
	}
	return strings.Join(lines, "\n")
}

// itemFile renders one item exactly as Joplin's BaseItem.serialize does: the
// title, a blank line, the body when there is one, a blank line, then the
// property block ending in type_. There is no trailing newline.
func itemFile(title, body string, props []field, itemType int) string {
	block := joinProps(append(props, kv("type_", strconv.Itoa(itemType))))
	if title == "" && body == "" {
		return block
	}
	out := title + "\n\n"
	if body != "" {
		out += body + "\n\n"
	}
	return out + block
}

// escapeValue mirrors Joplin's serialize_format: an existing backslash escape
// becomes literal text and real newlines become \n, so values survive a single
// line of the property block.
func escapeValue(s string) string {
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, `\n`, `\\n`)
	s = strings.ReplaceAll(s, `\r`, `\\r`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	return s
}

func unescapeValue(s string) string {
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, `\n`, "\n")
	s = strings.ReplaceAll(s, `\r`, "\r")
	s = strings.ReplaceAll(s, "\\\n", `\n`)
	s = strings.ReplaceAll(s, "\\\r", `\r`)
	return s
}

func atoi(s string) int {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return v
}

// SerializeNote renders one note as a JEX item file.
func SerializeNote(n *models.Note) string {
	props := []field{
		strField("id", n.ID),
		strField("parent_id", n.ParentID),
		timeField("created_time", n.CreatedTime),
		timeField("updated_time", n.UpdatedTime),
		intField("is_conflict", n.IsConflict),
		coordField("latitude", n.Latitude, 8),
		coordField("longitude", n.Longitude, 8),
		coordField("altitude", n.Altitude, 4),
		strField("author", n.Author),
		strField("source_url", n.SourceURL),
		intField("is_todo", n.IsTodo),
		// Joplin only formats the created/updated/user/sync timestamps as
		// dates; these stay integer milliseconds, including a bare 0.
		int64Field("todo_due", n.TodoDue),
		int64Field("todo_completed", n.TodoCompleted),
		strField("source", n.Source),
		strField("source_application", n.SourceApplication),
		strField("application_data", n.ApplicationData),
		floatField("order", n.Order),
		timeField("user_created_time", n.UserCreatedTime),
		timeField("user_updated_time", n.UserUpdatedTime),
		strField("encryption_cipher_text", n.EncryptionCipherText),
		intField("encryption_applied", n.EncryptionApplied),
		intField("markup_language", n.MarkupLanguage),
		intField("is_shared", n.IsShared),
		strField("share_id", n.ShareID),
		strField("conflict_original_id", n.ConflictOriginalID),
		strField("master_key_id", n.MasterKeyID),
		strField("user_data", n.UserData),
		int64Field("deleted_time", n.DeletedTime),
	}
	return itemFile(n.Title, n.Body, props, models.TypeNote)
}

// SerializeFolder renders one notebook as a JEX item file.
func SerializeFolder(f *models.Folder) string {
	props := []field{
		strField("id", f.ID),
		timeField("created_time", f.CreatedTime),
		timeField("updated_time", f.UpdatedTime),
		timeField("user_created_time", f.UserCreatedTime),
		timeField("user_updated_time", f.UserUpdatedTime),
		strField("encryption_cipher_text", f.EncryptionCipherText),
		intField("encryption_applied", f.EncryptionApplied),
		strField("parent_id", f.ParentID),
		intField("is_shared", f.IsShared),
		strField("share_id", f.ShareID),
		strField("master_key_id", f.MasterKeyID),
		strField("icon", f.Icon),
		strField("user_data", f.UserData),
		int64Field("deleted_time", f.DeletedTime),
	}
	return itemFile(f.Title, "", props, models.TypeFolder)
}

// SerializeResource renders one attachment's metadata as a JEX item file.
func SerializeResource(r *models.Resource) string {
	props := []field{
		strField("id", r.ID),
		strField("mime", r.Mime),
		strField("filename", r.Filename),
		timeField("created_time", r.CreatedTime),
		timeField("updated_time", r.UpdatedTime),
		timeField("user_created_time", r.UserCreatedTime),
		timeField("user_updated_time", r.UserUpdatedTime),
		strField("file_extension", r.FileExtension),
		strField("encryption_cipher_text", r.EncryptionCipherText),
		intField("encryption_applied", r.EncryptionApplied),
		intField("encryption_blob_encrypted", r.EncryptionBlobEncrypted),
		int64Field("size", r.Size),
		intField("is_shared", r.IsShared),
		strField("share_id", r.ShareID),
		strField("master_key_id", r.MasterKeyID),
		strField("user_data", r.UserData),
		int64Field("blob_updated_time", r.BlobUpdatedTime),
	}
	title := r.Title
	if title == "" {
		title = r.Filename
	}
	if title == "" {
		title = r.ID + "." + r.FileExtension
	}
	return itemFile(title, "", props, models.TypeResource)
}

// SerializeTag renders one tag as a JEX item file.
func SerializeTag(t *models.Tag) string {
	props := []field{
		strField("id", t.ID),
		timeField("created_time", t.CreatedTime),
		timeField("updated_time", t.UpdatedTime),
		timeField("user_created_time", t.UserCreatedTime),
		timeField("user_updated_time", t.UserUpdatedTime),
		strField("encryption_cipher_text", t.EncryptionCipherText),
		intField("encryption_applied", t.EncryptionApplied),
		intField("is_shared", t.IsShared),
		strField("parent_id", t.ParentID),
		strField("user_data", t.UserData),
	}
	return itemFile(t.Title, "", props, models.TypeTag)
}

// SerializeNoteTag renders one note/tag link as a JEX item file.
func SerializeNoteTag(nt *models.NoteTag) string {
	props := []field{
		strField("id", nt.ID),
		strField("note_id", nt.NoteID),
		strField("tag_id", nt.TagID),
		timeField("created_time", nt.CreatedTime),
		timeField("updated_time", nt.UpdatedTime),
		timeField("user_created_time", nt.UserCreatedTime),
		timeField("user_updated_time", nt.UserUpdatedTime),
		strField("encryption_cipher_text", nt.EncryptionCipherText),
		intField("encryption_applied", nt.EncryptionApplied),
		intField("is_shared", nt.IsShared),
	}
	return itemFile("", "", props, models.TypeNoteTag)
}
