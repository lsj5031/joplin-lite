package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/lsj5031/joplin-lite/internal/config"
	"github.com/lsj5031/joplin-lite/internal/store"
)

// The native settings dialog persists what it changes in the vault's local
// key_values table, which already holds Joplin's app settings (schema_version
// lives there too). It never rewrites the config file — a config may be
// Joplin desktop's settings.json, which that app owns — and key_values is not
// synced, so these stay per-device settings.
//
// The master password is stored like Joplin desktop stores it in
// settings.json: locally, unencrypted at rest. It never leaves the device.
const (
	settingSyncTarget     = "desktop.sync.target"
	settingLocalDir       = "desktop.sync.2.path"
	settingServerURL      = "desktop.sync.9.path"
	settingUsername       = "desktop.sync.9.username"
	settingPassword       = "desktop.sync.9.password"
	settingMasterPassword = "desktop.e2ee.master_password"
	settingAPIToken       = "desktop.api.token"
	settingDark           = "desktop.dark"
)

// Sync targets the native dialog can configure. Dropbox (7) and S3 (8) need an
// OAuth app or bucket credentials, so they stay config-file work: a vault
// already configured for them keeps that target and the dialog says so.
const (
	targetOff    = 0
	targetFolder = 2
	targetServer = 9
)

// desktopSettings is the settings the native workspace can change without
// editing a config file.
type desktopSettings struct {
	Target         int
	LocalDir       string
	ServerURL      string
	Username       string
	Password       string
	MasterPassword string
	APIToken       string
	Dark           bool
}

// settingsFromConfig fills the dialog with the config that is actually in
// effect (file + env + stored overrides).
func settingsFromConfig(cfg *config.Config) desktopSettings {
	s := desktopSettings{
		Target:         cfg.SyncTarget,
		LocalDir:       cfg.LocalDir,
		ServerURL:      cfg.ServerURL,
		Username:       cfg.Username,
		Password:       cfg.Password,
		MasterPassword: cfg.MasterPassword,
		APIToken:       cfg.APIToken,
	}
	if cfg.NoSync {
		s.Target = targetOff
	}
	return s
}

// loadStoredSettings reads the dialog's settings. The second result reports
// whether anything was stored, so a first run keeps the config file's target
// instead of silently disabling sync.
func loadStoredSettings(db *store.DB) (desktopSettings, bool, error) {
	var s desktopSettings
	raw, err := db.GetSetting(settingSyncTarget)
	if err != nil {
		return s, false, err
	}
	if raw == "" {
		return s, false, nil
	}
	target, err := strconv.Atoi(raw)
	if err != nil {
		return s, false, fmt.Errorf("stored sync target %q is not a number", raw)
	}
	s.Target = target
	for key, dst := range map[string]*string{
		settingLocalDir:       &s.LocalDir,
		settingServerURL:      &s.ServerURL,
		settingUsername:       &s.Username,
		settingPassword:       &s.Password,
		settingMasterPassword: &s.MasterPassword,
		settingAPIToken:       &s.APIToken,
	} {
		v, err := db.GetSetting(key)
		if err != nil {
			return s, false, err
		}
		*dst = v
	}
	s.Dark = loadDarkSetting(db)
	return s, true, nil
}

// loadDarkSetting reads the persisted appearance preference from key_values.
func loadDarkSetting(db *store.DB) bool {
	if db == nil {
		return false
	}
	v, err := db.GetSetting(settingDark)
	return err == nil && v == "1"
}

// saveDarkSetting stores the appearance preference in key_values.
func saveDarkSetting(db *store.DB, dark bool) error {
	if db == nil {
		return nil
	}
	val := "0"
	if dark {
		val = "1"
	}
	return db.SetSetting(settingDark, val)
}

// save writes every field, so the dialog is the single source of truth after
// the first save and clearing a field really clears it.
func (s desktopSettings) save(db *store.DB) error {
	darkVal := "0"
	if s.Dark {
		darkVal = "1"
	}
	values := map[string]string{
		settingSyncTarget:     strconv.Itoa(s.Target),
		settingLocalDir:       strings.TrimSpace(s.LocalDir),
		settingServerURL:      strings.TrimSpace(s.ServerURL),
		settingUsername:       strings.TrimSpace(s.Username),
		settingPassword:       s.Password,
		settingMasterPassword: s.MasterPassword,
		settingAPIToken:       strings.TrimSpace(s.APIToken),
		settingDark:           darkVal,
	}
	for key, value := range values {
		if err := db.SetSetting(key, value); err != nil {
			return fmt.Errorf("save %s: %w", key, err)
		}
	}
	return nil
}

// applyTo overlays the settings on a loaded config. The S3/Dropbox fields are
// left untouched because the dialog does not edit them.
func (s desktopSettings) applyTo(cfg *config.Config) {
	cfg.SyncTarget = s.Target
	cfg.LocalDir = strings.TrimSpace(s.LocalDir)
	cfg.ServerURL = strings.TrimSpace(s.ServerURL)
	cfg.Username = strings.TrimSpace(s.Username)
	cfg.Password = s.Password
	cfg.MasterPassword = s.MasterPassword
	cfg.APIToken = strings.TrimSpace(s.APIToken)
	cfg.NoSync = s.Target == targetOff
}

// validate rejects a save that could not sync, before anything is persisted or
// the running engine is replaced.
func (s desktopSettings) validate() error {
	switch s.Target {
	case targetOff:
	case targetFolder:
		if strings.TrimSpace(s.LocalDir) == "" {
			return fmt.Errorf("choose the folder to sync with")
		}
	case targetServer:
		if strings.TrimSpace(s.ServerURL) == "" {
			return fmt.Errorf("enter the Joplin Server URL")
		}
		if strings.TrimSpace(s.Username) == "" || s.Password == "" {
			return fmt.Errorf("enter the Joplin Server email and password")
		}
	case 7, 8:
		// Configured in the file (OAuth app / bucket); the dialog keeps it.
	default:
		return fmt.Errorf("sync target %d cannot be configured here", s.Target)
	}
	if strings.TrimSpace(s.APIToken) == "" {
		return fmt.Errorf("the clipper token cannot be empty")
	}
	return nil
}

// targetLabel names a target for the dialog and the footer.
func targetLabel(target int) string {
	switch target {
	case targetOff:
		return "Local only"
	case targetFolder:
		return "File system"
	case targetServer:
		return "Joplin Server"
	case 7:
		return "Dropbox"
	case 8:
		return "S3"
	}
	return fmt.Sprintf("Target %d", target)
}
