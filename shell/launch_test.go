package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/lsj5031/joplin-lite/internal/config"
)

// Independently generated using Python cryptography, PBKDF2 + AES-CBC.
const syntheticCiphertext = "djEwGJqrcG+VvCsWBUj6m5GjRyKwkgByKCAGiUbHTYR1JlA="
const syntheticMaster = "synthetic master ☃"

func TestRememberedLaunchPrecedence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", filepath.Join(home, "AppData"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GOJOPLIN_CONFIG_PATH", "")
	defaultConfig := ""
	if runtime.GOOS == "windows" {
		defaultConfig = filepath.Join(home, "AppData", "joplin-lite", "config.json")
	}
	path := filepath.Join(t.TempDir(), "launch.json")
	want := launchSettings{Config: filepath.Join(home, "chosen", "config.yaml"), Profile: filepath.Join(home, "chosen", "profile"), Addr: "127.0.0.1:41984", ReuseOfficialKeychain: true, DropboxBlockDeletes: true}
	raw, _ := json.Marshal(want)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := resolveLaunch(path, "", "", "")
	if err != nil || got.Config != want.Config || got.Profile != want.Profile || got.Addr != want.Addr || !got.ReuseOfficialKeychain || !got.DropboxBlockDeletes {
		t.Fatalf("remembered launch not restored: %v", err)
	}
	got, err = resolveLaunch(path, "", "", "127.0.0.1:0")
	if err != nil || got.Addr != "127.0.0.1:0" || got.Config != defaultConfig || got.Profile != "" || got.ReuseOfficialKeychain {
		t.Fatal("explicit address inherited remembered profile permissions")
	}
	got, err = resolveLaunch(path, "", "", "", true)
	if err != nil || got.Config != defaultConfig || got.Profile != "" || got.ReuseOfficialKeychain {
		t.Fatal("explicit launch flag inherited remembered credentials")
	}
	got, err = resolveLaunch(path, "other.yaml", "other-profile", "127.0.0.1:0")
	if err != nil || got.Config != "other.yaml" || got.Profile != "other-profile" || got.ReuseOfficialKeychain || got.DropboxBlockDeletes {
		t.Fatal("CLI selection inherited remembered credentials")
	}
	t.Setenv("GOJOPLIN_CONFIG_PATH", "env.yaml")
	got, err = resolveLaunch(path, "", "", "")
	if err != nil || got.Config != "env.yaml" || got.ReuseOfficialKeychain {
		t.Fatal("environment config did not select an independent launch")
	}
	got, err = resolveLaunch(path, "cli.yaml", "", "")
	if err != nil || got.Config != "cli.yaml" {
		t.Fatal("explicit config did not override environment selection")
	}
	t.Setenv("GOJOPLIN_CONFIG_PATH", "")
	for _, raw := range []string{`{}`, `{"config":"relative","profile":"/p"}`, `{"config":"/c","profile":"/p","password":"secret"}`, `{"config":"/c","profile":"/p"} {}`} {
		if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := resolveLaunch(path, "", "", ""); err == nil {
			t.Fatal("accepted invalid launch file")
		}
	}
	got, err = resolveLaunch(path, "", "/other-profile", "")
	if err != nil || got.Profile != "/other-profile" || got.Config != defaultConfig || got.ReuseOfficialKeychain {
		t.Fatal("data-dir alone did not preserve ordinary default-config semantics")
	}
	got, err = resolveLaunch(filepath.Join(t.TempDir(), "missing"), "", "", "")
	if err != nil || got.Config != defaultConfig || got.Profile != "" || got.ReuseOfficialKeychain {
		t.Fatal("missing launch settings did not preserve ordinary defaults")
	}
}

func syntheticOfficialProfile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "database.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// store.Open migrates this synthetic database; no real profile is read.
	for _, query := range []string{
		`CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT)`,
		`CREATE TABLE key_values (key TEXT PRIMARY KEY, value TEXT, type INTEGER, updated_time INTEGER)`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO settings VALUES ('sync.7.auth', 'synthetic-bearer')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO key_values VALUES (?, ?, 1, 0)`, "KeychainServiceDriver.secureStore.setting.encryption.masterPassword", syntheticCiphertext); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestMissingLaunchAndDataDirOnlyUseDefaultConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("GOJOPLIN_CONFIG_PATH", "")
	t.Setenv("GOJOPLIN_DATA_DIR", "")
	configDir := filepath.Join(home, ".config", "go-joplin")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	defaultProfile := filepath.Join(home, "default-vault")
	raw := fmt.Sprintf("api:\n  token: default-config-token\nserver:\n  data_dir: %q\n", defaultProfile)
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, override := range []string{"", filepath.Join(home, "override-vault")} {
		launch, err := resolveLaunch(filepath.Join(home, "missing-launch.json"), "", override, "")
		if err != nil {
			t.Fatal(err)
		}
		be, err := startBackend(launch.Config, launch.Profile, true, "127.0.0.1:0", launch)
		if err != nil {
			t.Fatal(err)
		}
		want := override
		if want == "" {
			want = defaultProfile
		}
		if be.cfg.DataDir != want || be.token != "default-config-token" {
			t.Fatal("ordinary default config or data-dir precedence changed")
		}
		be.Close()
	}
}

func TestOfficialCredentialRecovery(t *testing.T) {
	profile := syntheticOfficialProfile(t)
	cfg := &config.Config{DataDir: profile, SyncTarget: 7}
	if err := recoverOfficialCredentials(cfg, func() ([]byte, error) { return []byte("fake wrapping key"), nil }); err != nil {
		t.Fatal(err)
	}
	if cfg.MasterPassword != syntheticMaster || cfg.DropboxAuth != "synthetic-bearer" || cfg.DropboxAppKey == "" {
		t.Fatal("did not recover expected synthetic credentials")
	}
	for _, keychain := range []func() ([]byte, error){
		func() ([]byte, error) { return nil, errors.New("sensitive command output") },
		func() ([]byte, error) { return []byte("wrong wrapping key"), nil },
	} {
		cfg := &config.Config{DataDir: profile, SyncTarget: 7}
		err := recoverOfficialCredentials(cfg, keychain)
		if err == nil || strings.Contains(err.Error(), "sensitive") || cfg.MasterPassword != "" || cfg.DropboxAuth != "" {
			t.Fatal("failed recovery did not fail closed")
		}
	}
	for _, blob := range []string{"", "not-base64", "djEx", "djEwAA=="} {
		if _, err := decryptOfficialPassword(blob, []byte("fake wrapping key")); err == nil {
			t.Fatal("accepted unsupported ciphertext")
		}
	}
	missing := filepath.Join(t.TempDir(), "missing-profile")
	if err := recoverOfficialCredentials(&config.Config{DataDir: missing, SyncTarget: 7}, func() ([]byte, error) {
		t.Fatal("Keychain called before profile validation")
		return nil, nil
	}); err == nil {
		t.Fatal("accepted missing profile")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("recovery created a profile")
	}
}

func TestOfficialRestartAndSettingsKeepSecretsInMemory(t *testing.T) {
	profile := syntheticOfficialProfile(t)
	cfgPath := filepath.Join(profile, "settings.json")
	const officialJSON = `{"sync.target":7,"api.token":"test-api-token"}`
	if err := os.WriteFile(cfgPath, []byte(officialJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	launch := launchSettings{ReuseOfficialKeychain: true, DropboxBlockDeletes: true,
		keychain: func() ([]byte, error) { return []byte("fake wrapping key"), nil }}
	open := func() *backend {
		t.Helper()
		be, err := startBackend(cfgPath, profile, true, "127.0.0.1:0", launch)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(be.Close)
		if be.cfg.MasterPassword != syntheticMaster || be.cfg.DropboxAuth != "synthetic-bearer" || !be.cfg.DropboxBlockDeletes || be.cfg.DataDir != profile {
			t.Fatal("startup lost profile, credentials or deletion guard")
		}
		return be
	}
	be := open()
	// Constructing a backend must ignore a pre-existing broken cache in this
	// mode. No engine/pass is started, so this never contacts Dropbox.
	cache := filepath.Join(profile, "dropbox_token.json")
	if err := os.WriteFile(cache, []byte("invalid token cache"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newSyncBackend(be.cfg, true); err != nil {
		t.Fatal("official credential mode used the token cache")
	}
	if err := os.Remove(cache); err != nil {
		t.Fatal(err)
	}
	if s := be.Settings(); s.MasterPassword != "" || s.Target != 7 {
		t.Fatal("recovered password exposed to Settings")
	}
	// Exercise the actual settings view, including Show passwords.
	registerFonts()
	app, _ := openNativeTestApp(t)
	app.be = be
	app.openSettings()
	tt := ui.NewTester(app.view, 1280, 800)
	click(t, tt, "Show passwords")
	if app.settings.MasterPassword != "" || app.settings.Password != "" || tt.HasText(syntheticMaster) || tt.HasText("synthetic-bearer") {
		t.Fatal("recovered secret entered UI state")
	}
	s := be.Settings()
	s.APIToken = "changed-api-token"
	if err := be.ApplySettings(s); err != nil {
		t.Fatal(err)
	}
	be.syncMu.Lock()
	kept := be.cfg.MasterPassword == syntheticMaster && be.cfg.NoSync && be.cfg.DropboxBlockDeletes
	be.syncMu.Unlock()
	if !kept {
		t.Fatal("settings cleared the in-memory password or enabled sync")
	}
	stored, _, err := loadStoredSettings(be.db)
	if err != nil || stored.MasterPassword != "" {
		t.Fatal("settings persisted the recovered password")
	}
	s.MasterPassword = syntheticMaster
	if err := be.ApplySettings(s); err == nil {
		t.Fatal("settings allowed plaintext persistence in Keychain mode")
	}
	be.Close()
	be = open()
	if be.token != "changed-api-token" {
		t.Fatal("restart did not retain the non-secret settings overlay")
	}
	be.Close()
	if raw, err := os.ReadFile(cfgPath); err != nil || string(raw) != officialJSON {
		t.Fatal("startup/settings rewrote official settings.json")
	}
	// Check every created file, including SQLite/WAL and desktop logs.
	if err := filepath.WalkDir(profile, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path)
		if strings.Contains(string(raw), syntheticMaster) || strings.Contains(string(raw), "fake wrapping key") {
			t.Fatal("plaintext password found in profile files")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(profile, "dropbox_token.json")); !os.IsNotExist(err) {
		t.Fatal("created plaintext token cache")
	}
	// Conflicting existing overlays must never replace the Keychain secret.
	for _, conflict := range []desktopSettings{
		{Target: targetOff, APIToken: "test-token"},
		{Target: 7, MasterPassword: "old plaintext", APIToken: "test-token"},
	} {
		db, err := sql.Open("sqlite", filepath.Join(profile, "database.sqlite"))
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.Exec(`UPDATE key_values SET value = ? WHERE key = ?`, fmt.Sprint(conflict.Target), settingSyncTarget)
		if err == nil {
			_, err = db.Exec(`UPDATE key_values SET value = ? WHERE key = ?`, conflict.MasterPassword, settingMasterPassword)
		}
		db.Close()
		if err != nil {
			t.Fatal(err)
		}
		if rejected, err := startBackend(cfgPath, profile, true, "127.0.0.1:0", launch); err == nil {
			rejected.Close()
			t.Fatal("accepted conflicting stored settings")
		}
	}
}

func TestLastSuccessIsScopedToAccountAndLocation(t *testing.T) {
	for _, pair := range [][2]config.Config{
		{{SyncTarget: 7, DropboxAuth: "account-one"}, {SyncTarget: 7, DropboxAuth: "account-two"}},
		{{SyncTarget: 9, ServerURL: "https://one", Username: "a"}, {SyncTarget: 9, ServerURL: "https://two", Username: "a"}},
		{{SyncTarget: 9, ServerURL: "https://one", Username: "a"}, {SyncTarget: 9, ServerURL: "https://one", Username: "b"}},
		{{SyncTarget: 8, S3Bucket: "one"}, {SyncTarget: 8, S3Bucket: "two"}},
	} {
		profile := filepath.Join(t.TempDir(), "vault")
		cfgPath := writeDesktopConfig(t, "api:\n  token: test-token\n")
		be, err := startBackend(cfgPath, profile, true, "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		// Use the backend's status persistence contract without contacting any
		// remote; these identities deliberately have no real credentials.
		be.cfg = &pair[0]
		if err := be.startSyncLocked(nil); err != nil {
			t.Fatal(err)
		}
		be.status.finish(nil)
		be.cfg = &pair[1]
		if err := be.startSyncLocked(nil); err != nil {
			t.Fatal(err)
		}
		if !be.SyncState().Last.IsZero() {
			t.Fatal("another account/location inherited success")
		}
		be.Close()
	}
}

func TestLastSuccessSurvivesRestartAndFailureButNotTargetChange(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "vault")
	target := t.TempDir()
	cfg := writeDesktopConfig(t, fmt.Sprintf("sync:\n  target: 2\n  local_dir: %q\n", target))
	be, err := startBackend(cfg, profile, false, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	first := waitDesktopSync(t, be, time.Time{}).Last
	be.Close()
	// Local-only reopen guarantees a new successful pass cannot hide a failed
	// restore. The actual profile DB is closed and reopened.
	be, err = startBackend(cfg, profile, true, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(be.Close)
	if !be.SyncState().Last.Equal(first) {
		t.Fatal("restart lost last successful sync")
	}
	if be.SyncState().Last.Location() != time.Local {
		t.Fatal("restored success time would display in UTC instead of local time")
	}
	be.status.finish(errors.New("synthetic failure"))
	if got := be.SyncState(); !got.Last.Equal(first) || got.Err == "" {
		t.Fatal("failure replaced last-success time")
	}
	be.Close()
	be, err = startBackend(cfg, profile, true, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(be.Close)
	if !be.SyncState().Last.Equal(first) {
		t.Fatal("failure corrupted persisted success time")
	}
	retiring := be.status
	if err := be.ApplySettings(desktopSettings{Target: targetOff, APIToken: be.token}); err != nil {
		t.Fatal(err)
	}
	retiring.finish(nil) // A previous target's pass finishes after the save.
	if !be.SyncState().Last.IsZero() {
		t.Fatal("retiring target published success in the replacement status")
	}
	be.Close()
	other := writeDesktopConfig(t, fmt.Sprintf("sync:\n  target: 2\n  local_dir: %q\n", t.TempDir()))
	be, err = startBackend(other, profile, true, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer be.Close()
	if !be.SyncState().Last.IsZero() {
		t.Fatal("new target inherited previous target success")
	}
}
