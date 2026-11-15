package config

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestOfficialDropboxAuthReadOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "database.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE settings (key TEXT, value TEXT); INSERT INTO settings VALUES ('sync.7.auth','synthetic-bearer')"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := OfficialDropboxAuth(dir)
	if err != nil || auth != "synthetic-bearer" {
		t.Fatalf("auth recovery: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("profile database changed")
	}
	missing := t.TempDir()
	if _, err := OfficialDropboxAuth(missing); err == nil {
		t.Fatal("missing profile accepted")
	}
	if _, err := os.Stat(filepath.Join(missing, "database.sqlite")); !os.IsNotExist(err) {
		t.Fatal("created missing profile")
	}
}
