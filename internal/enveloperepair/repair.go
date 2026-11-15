// Package enveloperepair repairs disposable, offline copies of sync targets.
// It has no sync backend, configuration loader, profile access, or key access.
package enveloperepair

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lsj5031/joplin-lite/internal/e2ee"
	"github.com/lsj5031/joplin-lite/internal/models"
)

var itemID = regexp.MustCompile(`^[a-f0-9]{32}$`)

type Entry struct {
	ID     string `json:"id"`
	Before string `json:"before_sha256"`
	After  string `json:"after_sha256"`
}

type manifest struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

type change struct {
	Entry
	before, after []byte
}

func digest(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }

// Correct adds missing blank placeholders, never rewriting an existing byte.
// Ambiguous or malformed encrypted items fail closed, without echoing data.
func Correct(raw []byte, id string) ([]byte, error) {
	item, err := models.ParseItem(string(raw))
	if err != nil {
		return nil, fmt.Errorf("invalid item metadata")
	}
	fields := make(map[string]string)
	for _, m := range item.Meta {
		if _, exists := fields[m.Key]; exists {
			return nil, fmt.Errorf("duplicate metadata key")
		}
		fields[m.Key] = m.Value
	}
	if fields["id"] != id || !itemID.MatchString(id) {
		return nil, fmt.Errorf("item ID does not match filename")
	}
	if fields["encryption_applied"] == "" || fields["encryption_applied"] == "0" {
		if fields["encryption_cipher_text"] != "" {
			return nil, fmt.Errorf("inconsistent encryption fields")
		}
		return raw, nil
	}
	if fields["encryption_applied"] != "1" || item.HasHead {
		return nil, fmt.Errorf("invalid encrypted envelope")
	}
	missing, supported := models.EncryptedEnvelopeMissing(item.Type, fields)
	if item.Type == models.TypeRevision {
		// Official revision history is not written by Lite. Recognize only a
		// complete official base envelope, without correcting/re-uploading it.
		// The later additive is_locked column is optional for old revisions.
		// Both schemas are independently derived by qa/official-envelope.mjs.
		for _, key := range strings.Split("id,parent_id,item_type,item_id,item_updated_time,title_diff,body_diff,metadata_diff,encryption_cipher_text,encryption_applied,updated_time,created_time", ",") {
			if _, ok := fields[key]; !ok {
				return nil, fmt.Errorf("incomplete revision envelope is outside repair scope")
			}
		}
		supported = true
	}
	if !supported || fields["type_"] != strconv.Itoa(item.Type) {
		return nil, fmt.Errorf("unsupported encrypted item type")
	}
	if err := e2ee.ValidateEncryptedString(fields["encryption_cipher_text"]); err != nil {
		return nil, err
	}
	// Keep existing unknown fields, but never accept a plaintext title/body.
	if _, ok := fields["title"]; ok {
		return nil, fmt.Errorf("unexpected encrypted title field")
	}
	if _, ok := fields["body"]; ok {
		return nil, fmt.Errorf("unexpected encrypted body field")
	}
	if _, err := time.Parse(time.RFC3339Nano, fields["updated_time"]); err != nil {
		return nil, fmt.Errorf("missing or invalid envelope update time")
	}
	keys := make([]string, 0, len(missing))
	for key := range missing {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var prefix strings.Builder
	for _, key := range keys {
		prefix.WriteString(key + ": \n")
	}
	return append([]byte(prefix.String()), raw...), nil
}

// Run is dry-run unless apply is explicit. The caller must own an offline copy
// exclusively. backup must not exist; rollback names an existing backup.
// Only root-level <32 lowercase hex>.md files are considered.
func Run(dir, backup, rollback string, apply bool) ([]Entry, error) {
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("export directory must be absolute")
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("export must be a non-symlink directory")
	}
	var changes []change
	if rollback != "" {
		changes, err = planRollback(dir, rollback)
	} else {
		changes, err = plan(dir)
	}
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(changes))
	for _, c := range changes {
		entries = append(entries, c.Entry)
	}
	if !apply || len(changes) == 0 {
		return entries, nil
	}
	if rollback == "" {
		if err := saveBackup(dir, backup, changes); err != nil {
			return nil, err
		}
	}
	for _, c := range changes {
		path := filepath.Join(dir, c.ID+".md")
		current, err := readRegular(path)
		if err != nil || !bytes.Equal(current, c.before) {
			return nil, fmt.Errorf("item %s changed; stop and re-export (backup retained)", c.ID)
		}
		if err := replace(path, c.after); err != nil {
			return nil, fmt.Errorf("item %s write failed (backup retained): %w", c.ID, err)
		}
	}
	return entries, nil
}

func plan(dir string) ([]change, error) {
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var changes []change
	for _, file := range files {
		name := file.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		id := strings.TrimSuffix(name, ".md")
		if !itemID.MatchString(id) {
			return nil, fmt.Errorf("unexpected markdown filename in export")
		}
		raw, err := readRegular(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("item %s is not a readable regular file", id)
		}
		corrected, err := Correct(raw, id)
		if err != nil {
			return nil, fmt.Errorf("item %s: %w", id, err)
		}
		if !bytes.Equal(raw, corrected) {
			changes = append(changes, change{Entry{id, digest(raw), digest(corrected)}, raw, corrected})
		}
	}
	return changes, nil
}

func readRegular(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("expected regular file")
	}
	return os.ReadFile(path)
}

func saveBackup(dir, backup string, changes []change) error {
	if !filepath.IsAbs(backup) {
		return fmt.Errorf("--backup requires a new absolute directory outside the export")
	}
	dirReal, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(backup))
	if err != nil {
		return err
	}
	backup = filepath.Join(parent, filepath.Base(backup))
	rel, err := filepath.Rel(dirReal, backup)
	if err != nil || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))) {
		return fmt.Errorf("backup must be outside the export")
	}
	if err := os.Mkdir(backup, 0700); err != nil {
		return fmt.Errorf("backup must be new: %w", err)
	}
	m := manifest{Version: 1}
	for _, c := range changes {
		if err := writeNew(filepath.Join(backup, c.ID+".md"), c.before); err != nil {
			return err
		}
		m.Entries = append(m.Entries, c.Entry)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := writeNew(filepath.Join(backup, "manifest.json"), data); err != nil {
		return err
	}
	if err := syncDir(backup); err != nil {
		return err
	}
	return syncDir(parent)
}

func planRollback(dir, backup string) ([]change, error) {
	if !filepath.IsAbs(backup) {
		return nil, fmt.Errorf("rollback backup must be absolute")
	}
	data, err := readRegular(filepath.Join(backup, "manifest.json"))
	if err != nil {
		return nil, err
	}
	var m manifest
	if json.Unmarshal(data, &m) != nil || m.Version != 1 || len(m.Entries) == 0 {
		return nil, fmt.Errorf("invalid backup manifest")
	}
	var changes []change
	seen := map[string]bool{}
	for _, e := range m.Entries {
		if !itemID.MatchString(e.ID) || seen[e.ID] {
			return nil, fmt.Errorf("invalid backup item ID")
		}
		seen[e.ID] = true
		for _, hash := range []string{e.Before, e.After} {
			if b, err := hex.DecodeString(hash); err != nil || len(b) != 32 {
				return nil, fmt.Errorf("invalid backup digest")
			}
		}
		original, err := readRegular(filepath.Join(backup, e.ID+".md"))
		if err != nil || digest(original) != e.Before {
			return nil, fmt.Errorf("item %s backup mismatch", e.ID)
		}
		corrected, err := Correct(original, e.ID)
		if err != nil || digest(corrected) != e.After {
			return nil, fmt.Errorf("item %s backup correction mismatch", e.ID)
		}
		current, err := readRegular(filepath.Join(dir, e.ID+".md"))
		if err != nil {
			return nil, fmt.Errorf("item %s cannot be read", e.ID)
		}
		if bytes.Equal(current, original) {
			continue
		}
		if !bytes.Equal(current, corrected) {
			return nil, fmt.Errorf("item %s changed since repair; refusing rollback", e.ID)
		}
		changes = append(changes, change{e, current, original})
	}
	return changes, nil
}

func writeNew(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func replace(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".envelope-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

func syncDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
