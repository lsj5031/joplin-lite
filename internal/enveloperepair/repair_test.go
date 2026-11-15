package enveloperepair

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lsj5031/joplin-lite/internal/models"
)

// Syntactically valid, unauthenticated synthetic packet. Repair deliberately
// needs no key; actual encrypt/decrypt preservation is tested in internal/sync.
func oldEnvelope(id string, kind int) []byte {
	packet := `{"salt":"AAAAAAAAAAA=","iv":"AAAAAAAAAAA=","ct":"AAAAAAAAAAA="}`
	ciphertext := "JED010000220a" + strings.Repeat("b", 32) + fmt.Sprintf("%06x", len(packet)) + packet
	return []byte(fmt.Sprintf("id: %s\nupdated_time: 2026-02-25T00:00:00.000Z\nencryption_cipher_text: %s\nencryption_applied: 1\nfuture_field: opaque\\nvalue\ntype_: %d", id, ciphertext, kind))
}

func put(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func get(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestOfflineRepairApplyRerunAndRollback(t *testing.T) {
	root := t.TempDir()
	dir, backup := filepath.Join(root, "export"), filepath.Join(root, "backup")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	originals := map[string][]byte{}
	for _, kind := range []int{1, 2, 4, 5, 6} {
		id := fmt.Sprintf("%032x", kind)
		originals[id] = oldEnvelope(id, kind)
		put(t, filepath.Join(dir, id+".md"), originals[id])
	}
	plainID := strings.Repeat("c", 32)
	plain := []byte(models.SerializeNote(&models.Note{ID: plainID, Title: "private title", Body: "private body"}))
	put(t, filepath.Join(dir, plainID+".md"), plain)
	validID := strings.Repeat("d", 32)
	valid, err := Correct(oldEnvelope(validID, 1), validID)
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(dir, validID+".md"), valid)
	put(t, filepath.Join(dir, "info.json"), []byte(`{"version":3}`))
	if err := os.Mkdir(filepath.Join(dir, ".resource"), 0700); err != nil {
		t.Fatal(err)
	}
	blob := []byte("synthetic encrypted resource bytes")
	put(t, filepath.Join(dir, ".resource", plainID), blob)

	entries, err := Run(dir, "", "", false)
	if err != nil || len(entries) != 5 {
		t.Fatalf("dry-run: %v %v", entries, err)
	}
	for id, original := range originals {
		if !bytes.Equal(get(t, filepath.Join(dir, id+".md")), original) {
			t.Fatal("dry-run wrote")
		}
	}
	if _, err := Run(dir, "", "", true); err == nil {
		t.Fatal("apply without backup accepted")
	}
	entries, err = Run(dir, backup, "", true)
	if err != nil || len(entries) != 5 {
		t.Fatalf("apply: %v %v", entries, err)
	}
	for id, original := range originals {
		if !bytes.Equal(get(t, filepath.Join(backup, id+".md")), original) {
			t.Fatal("backup changed")
		}
		got := get(t, filepath.Join(dir, id+".md"))
		if !bytes.HasSuffix(got, original) {
			t.Fatal("existing bytes changed")
		}
		_, before := models.Deserialize(string(original))
		_, after := models.Deserialize(string(got))
		for key, value := range before {
			if after[key] != value {
				t.Fatalf("changed %s", key)
			}
		}
		again, err := Correct(got, id)
		if err != nil || !bytes.Equal(again, got) {
			t.Fatal("not idempotent")
		}
	}
	if entries, err = Run(dir, "", "", true); err != nil || len(entries) != 0 {
		t.Fatalf("rerun: %v %v", entries, err)
	}
	if !bytes.Equal(get(t, filepath.Join(dir, plainID+".md")), plain) ||
		!bytes.Equal(get(t, filepath.Join(dir, validID+".md")), valid) ||
		!bytes.Equal(get(t, filepath.Join(dir, ".resource", plainID)), blob) {
		t.Fatal("unaffected item or resource changed")
	}
	if entries, err = Run(dir, "", backup, false); err != nil || len(entries) != 5 {
		t.Fatalf("rollback dry: %v %v", entries, err)
	}
	// Simulate a crash partway through rollback.
	first := fmt.Sprintf("%032x", 1)
	put(t, filepath.Join(dir, first+".md"), originals[first])
	if entries, err = Run(dir, "", backup, true); err != nil || len(entries) != 4 {
		t.Fatalf("rollback: %v %v", entries, err)
	}
	if entries, err = Run(dir, "", backup, true); err != nil || len(entries) != 0 {
		t.Fatalf("rollback rerun: %v %v", entries, err)
	}
	for id, original := range originals {
		if !bytes.Equal(get(t, filepath.Join(dir, id+".md")), original) {
			t.Fatal("rollback changed bytes")
		}
	}
	info, err := os.Stat(filepath.Join(backup, "manifest.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("backup permissions: %v %v", info, err)
	}
}

func TestCorrectRejectsMalformedWithoutDataInErrors(t *testing.T) {
	id := strings.Repeat("a", 32)
	old := string(oldEnvelope(id, 1))
	_, fields := models.Deserialize(old)
	ciphertext := fields["encryption_cipher_text"]
	for name, raw := range map[string]string{
		"invalid-metadata":         "private body",
		"head":                     "private title\n\n" + old,
		"duplicate":                old + "\nid: " + id,
		"wrong-id":                 strings.Replace(old, id, strings.Repeat("c", 32), 1),
		"wrong-flag":               strings.Replace(old, "encryption_applied: 1", "encryption_applied: 2", 1),
		"plaintext-flag":           strings.Replace(old, "encryption_applied: 1", "encryption_applied: 0", 1),
		"wrong-type":               strings.Replace(old, "type_: 1", "type_: 9", 1),
		"invalid-ciphertext":       strings.Replace(old, "JED01", "private body", 1),
		"negative-metadata-length": strings.Replace(old, "000022", "-00001", 1),
		"negative-chunk-length":    strings.Replace(old, ciphertext, ciphertext[:45]+"-00001"+ciphertext[51:], 1),
		"truncated":                strings.Replace(old, "JED01000022", "JED010fffff", 1),
		"body-field":               old + "\nbody: private body",
		"missing-update":           strings.Replace(old, "updated_time: 2026-02-25T00:00:00.000Z\n", "", 1),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Correct([]byte(raw), id)
			if err == nil {
				t.Fatal("accepted malformed input")
			}
			if strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "JED") {
				t.Fatal("input disclosed")
			}
		})
	}
}

func TestRepairPreflightAndRollbackRefuseChanges(t *testing.T) {
	root := t.TempDir()
	dir, backup := filepath.Join(root, "export"), filepath.Join(root, "backup")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	id, other := strings.Repeat("a", 32), strings.Repeat("b", 32)
	old := oldEnvelope(id, 1)
	put(t, filepath.Join(dir, id+".md"), old)
	put(t, filepath.Join(dir, other+".md"), []byte("private malformed data"))
	if _, err := Run(dir, backup, "", true); err == nil {
		t.Fatal("accepted malformed export")
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatal("preflight created backup")
	}
	if !bytes.Equal(get(t, filepath.Join(dir, id+".md")), old) {
		t.Fatal("partial write")
	}
	if err := os.Remove(filepath.Join(dir, other+".md")); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(dir, filepath.Join(dir, "backup"), "", true); err == nil {
		t.Fatal("nested backup accepted")
	}
	if _, err := Run(dir, backup, "", true); err != nil {
		t.Fatal(err)
	}
	corrected := get(t, filepath.Join(dir, id+".md"))
	put(t, filepath.Join(dir, id+".md"), append(corrected, []byte("\nfuture: changed")...))
	if _, err := Run(dir, "", backup, true); err == nil {
		t.Fatal("overwrote concurrent change")
	}
	put(t, filepath.Join(dir, id+".md"), corrected)
	put(t, filepath.Join(backup, id+".md"), []byte("tampered backup"))
	if _, err := Run(dir, "", backup, true); err == nil {
		t.Fatal("accepted tampered backup")
	}
	if !bytes.Equal(get(t, filepath.Join(dir, id+".md")), corrected) {
		t.Fatal("rollback wrote")
	}
}

func TestRepairRefusesSymlinksAndExistingBackup(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "export")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("a", 32)
	original := oldEnvelope(id, 1)
	put(t, filepath.Join(root, "source"), original)
	if err := os.Symlink(filepath.Join(root, "source"), filepath.Join(dir, id+".md")); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(dir, "", "", false); err == nil {
		t.Fatal("accepted symlink item")
	}
	if err := os.Remove(filepath.Join(dir, id+".md")); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(dir, id+".md"), original)
	backup := filepath.Join(root, "backup")
	if err := os.Mkdir(backup, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(dir, backup, "", true); err == nil {
		t.Fatal("reused backup")
	}
	if !bytes.Equal(get(t, filepath.Join(dir, id+".md")), original) {
		t.Fatal("wrote after backup error")
	}
}

func TestInterruptedApplyRepairsOnlyRemainingItems(t *testing.T) {
	root := t.TempDir()
	dir, backup := filepath.Join(root, "export"), filepath.Join(root, "backup")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	a, b := strings.Repeat("a", 32), strings.Repeat("b", 32)
	put(t, filepath.Join(dir, a+".md"), oldEnvelope(a, 1))
	put(t, filepath.Join(dir, b+".md"), oldEnvelope(b, 4))
	if _, err := Run(dir, backup, "", true); err != nil {
		t.Fatal(err)
	}
	// State left by an interrupted apply: durable complete backup, but one
	// replacement has not occurred. Re-run needs no timestamp bump.
	put(t, filepath.Join(dir, b+".md"), get(t, filepath.Join(backup, b+".md")))
	entries, err := Run(dir, filepath.Join(root, "remaining-backup"), "", true)
	if err != nil || len(entries) != 1 || entries[0].ID != b {
		t.Fatalf("remaining items: %v %v", entries, err)
	}
	entries, err = Run(dir, "", backup, true)
	if err != nil || len(entries) != 2 {
		t.Fatalf("original complete backup rollback: %v %v", entries, err)
	}
}
