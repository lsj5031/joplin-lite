package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lsj5031/joplin-lite/internal/config"
	"golang.org/x/crypto/pbkdf2"
)

// launchSettings contains paths and explicit permissions, never credentials.
type launchSettings struct {
	Config                string `json:"config"`
	Profile               string `json:"profile"`
	Addr                  string `json:"addr"`
	ReuseOfficialKeychain bool   `json:"reuse_official_keychain"`
	DropboxBlockDeletes   bool   `json:"dropbox_block_deletes"`
	keychain              func() ([]byte, error)
}

func launchSettingsPath() (string, error) {
	dir, err := os.UserConfigDir()
	return filepath.Join(dir, "joplin-lite", "launch.json"), err
}

// An explicit config or profile selects an independent launch, so remembered
// credential permissions must never leak into a CLI-selected profile.
func resolveLaunch(path, cfgPath, dataDir, addr string, explicit ...bool) (launchSettings, error) {
	if cfgPath != "" || dataDir != "" || addr != "" || (len(explicit) != 0 && explicit[0]) || os.Getenv("GOJOPLIN_CONFIG_PATH") != "" {
		if cfgPath == "" {
			cfgPath = os.Getenv("GOJOPLIN_CONFIG_PATH")
		}
		if cfgPath == "" {
			var err error
			cfgPath, err = defaultDesktopConfig()
			if err != nil {
				return launchSettings{}, err
			}
		}
		return launchSettings{Config: cfgPath, Profile: dataDir, Addr: addr}, nil
	}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		cfgPath, err := defaultDesktopConfig()
		return launchSettings{Config: cfgPath}, err
	}
	if err != nil {
		return launchSettings{}, fmt.Errorf("read native launch settings at %s: %w", path, err)
	}
	var s launchSettings
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&s); err != nil {
		return s, errors.New("invalid native launch settings")
	}
	if d.Decode(new(any)) != io.EOF || !filepath.IsAbs(s.Config) || !filepath.IsAbs(s.Profile) {
		return s, errors.New("native launch settings require absolute config and profile paths and one JSON object")
	}
	return s, nil
}

func officialKeychainPassword() ([]byte, error) {
	if runtime.GOOS != "darwin" {
		return nil, errors.New("official Keychain reuse requires macOS")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	// Only the wrapping key travels through stdout. Never include command output
	// in an error, and never pass a recovered credential through argv or env.
	out, err := exec.CommandContext(ctx, "/usr/bin/security", "find-generic-password", "-w",
		"-s", "Joplin Safe Storage", "-a", "Joplin Key").Output()
	if err != nil {
		return nil, errors.New("official Joplin Keychain access failed")
	}
	return bytes.TrimSuffix(out, []byte("\n")), nil
}

func decryptOfficialPassword(encoded string, wrapping []byte) (string, error) {
	fail := errors.New("cannot unlock official Joplin encryption password")
	blob, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(wrapping) == 0 || len(blob) < 3+aes.BlockSize || string(blob[:3]) != "v10" || (len(blob)-3)%aes.BlockSize != 0 {
		return "", fail
	}
	key := pbkdf2.Key(wrapping, []byte("saltysalt"), 1003, 16, sha1.New)
	block, err := aes.NewCipher(key)
	clear(key)
	if err != nil {
		return "", fail
	}
	raw := make([]byte, len(blob)-3)
	defer clear(raw)
	cipher.NewCBCDecrypter(block, bytes.Repeat([]byte(" "), 16)).CryptBlocks(raw, blob[3:])
	pad := int(raw[len(raw)-1])
	if pad < 1 || pad > aes.BlockSize || !bytes.Equal(raw[len(raw)-pad:], bytes.Repeat([]byte{byte(pad)}, pad)) {
		return "", fail
	}
	password := raw[:len(raw)-pad]
	if len(password) == 0 || !utf8.Valid(password) {
		return "", fail
	}
	return string(password), nil
}

func recoverOfficialCredentials(cfg *config.Config, keychain func() ([]byte, error)) error {
	if cfg.SyncTarget != 7 {
		return errors.New("official credential reuse requires the explicitly configured Dropbox target")
	}
	path := filepath.Join(cfg.DataDir, "database.sqlite")
	// A file URL needs forward slashes and a leading slash before a Windows
	// drive letter; otherwise SQLite receives an encoded backslash path.
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return errors.New("cannot read the selected official profile")
	}
	defer db.Close()
	var auth, encrypted string
	if err := db.QueryRow("SELECT value FROM settings WHERE key = ?", "sync.7.auth").Scan(&auth); err != nil || strings.TrimSpace(auth) == "" {
		return errors.New("selected official profile has no Dropbox authorization")
	}
	if err := db.QueryRow("SELECT value FROM key_values WHERE key = ?",
		"KeychainServiceDriver.secureStore.setting.encryption.masterPassword").Scan(&encrypted); err != nil || encrypted == "" {
		return errors.New("selected official profile has no encrypted master password")
	}
	wrapping, err := keychain()
	if err != nil {
		return errors.New("official Joplin Keychain access failed")
	}
	defer clear(wrapping)
	password, err := decryptOfficialPassword(encrypted, wrapping)
	if err != nil {
		return err
	}
	cfg.DropboxAuth, cfg.MasterPassword = auth, password
	if cfg.DropboxAppKey == "" {
		cfg.DropboxAppKey = "m044w3cvmxhzvop"
	}
	return nil
}
