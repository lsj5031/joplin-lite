package main

import (
	"path/filepath"
	"testing"

	"github.com/lsj5031/joplin-lite/internal/config"
	"github.com/lsj5031/joplin-lite/internal/store"
)

func TestDesktopSettingsRoundTrip(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "vault"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	if _, ok, err := loadStoredSettings(db); err != nil || ok {
		t.Fatalf("fresh vault: ok=%v err=%v, want no stored settings", ok, err)
	}

	saved := desktopSettings{
		Target:         targetServer,
		ServerURL:      " https://sync.example.com ",
		Username:       "me@example.com",
		Password:       "secret",
		MasterPassword: "  spaced master  ",
		APIToken:       "token-1",
	}
	if err := saved.save(db); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, ok, err := loadStoredSettings(db)
	if err != nil || !ok {
		t.Fatalf("load: ok=%v err=%v", ok, err)
	}
	// Trimmed fields are trimmed; secrets keep their bytes.
	want := saved
	want.ServerURL = "https://sync.example.com"
	if got != want {
		t.Fatalf("loaded = %+v, want %+v", got, want)
	}

	// A later save rewrites every field, so clearing one really clears it.
	if err := (desktopSettings{Target: targetOff, MasterPassword: "m", APIToken: "token-2"}).save(db); err != nil {
		t.Fatalf("second save: %v", err)
	}
	got, _, err = loadStoredSettings(db)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Target != targetOff || got.ServerURL != "" || got.Password != "" || got.APIToken != "token-2" {
		t.Fatalf("after clearing = %+v", got)
	}
}

func TestDesktopSettingsValidate(t *testing.T) {
	base := desktopSettings{Target: targetFolder, LocalDir: "/tmp/sync", APIToken: "token"}
	cases := []struct {
		name string
		s    desktopSettings
		ok   bool
	}{
		{"local only", desktopSettings{Target: targetOff, APIToken: "token"}, true},
		{"file system", base, true},
		{"folder without path", desktopSettings{Target: targetFolder, APIToken: "token"}, false},
		{"server without url", desktopSettings{Target: targetServer, Username: "u", Password: "p", APIToken: "token"}, false},
		{"server without credentials", desktopSettings{Target: targetServer, ServerURL: "https://x", APIToken: "token"}, false},
		{"server", desktopSettings{Target: targetServer, ServerURL: "https://x", Username: "u", Password: "p", APIToken: "token"}, true},
		{"empty clipper token", desktopSettings{Target: targetOff, APIToken: "  "}, false},
		// Dropbox/S3 stay config-file work but must round-trip through a save.
		{"dropbox from config", desktopSettings{Target: 7, APIToken: "token"}, true},
		{"unknown target", desktopSettings{Target: 4, APIToken: "token"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.s.validate()
			if tc.ok && err != nil {
				t.Fatalf("validate = %v, want nil", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("validate accepted invalid settings")
			}
		})
	}
}

func TestDesktopSettingsApplyToConfig(t *testing.T) {
	cfg := &config.Config{SyncTarget: 8, S3Bucket: "bucket", S3Region: "region"}
	settingsFromConfig(cfg).applyTo(cfg)
	if cfg.SyncTarget != 8 || cfg.S3Bucket != "bucket" || cfg.S3Region != "region" {
		t.Fatalf("applyTo dropped the file-configured S3 fields: %+v", cfg)
	}

	desktopSettings{Target: targetFolder, LocalDir: " /vault/sync ", APIToken: "t"}.applyTo(cfg)
	if cfg.SyncTarget != targetFolder || cfg.LocalDir != "/vault/sync" || cfg.NoSync {
		t.Fatalf("applyTo folder = %+v", cfg)
	}

	desktopSettings{Target: targetOff, APIToken: "t"}.applyTo(cfg)
	if !cfg.NoSync || cfg.SyncTarget != targetOff {
		t.Fatalf("local-only applyTo = %+v", cfg)
	}

	// settingsFromConfig reports a -no-sync run as local only.
	if got := settingsFromConfig(cfg); got.Target != targetOff {
		t.Fatalf("settingsFromConfig(-no-sync) target = %d", got.Target)
	}
}
