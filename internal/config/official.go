package config

import (
	"database/sql"
	"errors"
	"net/url"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// OfficialDropboxAuth reads the existing authorization only. It never opens a
// writable profile, migrates its schema, or persists a recovered credential.
// Official Joplin keeps this setting in SQLite, not settings.json.
func OfficialDropboxAuth(profile string) (string, error) {
	path := filepath.ToSlash(filepath.Join(profile, "database.sqlite"))
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return "", errors.New("cannot read official profile authorization")
	}
	defer db.Close()
	var auth string
	if err := db.QueryRow("SELECT value FROM settings WHERE key = ?", "sync.7.auth").Scan(&auth); err != nil || strings.TrimSpace(auth) == "" {
		return "", errors.New("official profile has no Dropbox authorization")
	}
	return auth, nil
}
