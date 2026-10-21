//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lsj5031/joplin-lite/internal/store"
)

func TestDefaultLaunchStartsWithoutOfficialCredentials(t *testing.T) {
	for _, official := range []string{"", `{"sync.target":7}`} {
		t.Run(official, func(t *testing.T) {
			// Given an independent desktop installation, with no usable official credentials.
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			t.Setenv("APPDATA", filepath.Join(home, "AppData"))
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
			t.Setenv("GOJOPLIN_CONFIG_PATH", "")
			t.Setenv("GOJOPLIN_DATA_DIR", "")
			t.Setenv("GOJOPLIN_NO_SYNC", "")
			if official != "" {
				dir := filepath.Join(home, ".config", "joplin-desktop")
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(official), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			// When launching without a config argument.
			launch, err := resolveLaunch(filepath.Join(home, "missing-launch.json"), "", "", "")
			if err != nil {
				t.Fatal(err)
			}
			be, err := startBackend(launch.Config, filepath.Join(home, "vault"), false, "127.0.0.1:0", launch)
			if err != nil {
				t.Fatalf("default desktop launch must open locally without official credentials: %v", err)
			}
			defer be.Close()

			// Then the desktop is usable without inheriting an unconfigured remote.
			if be.cfg.SyncTarget != targetOff {
				t.Fatalf("default desktop inherited sync target %d", be.cfg.SyncTarget)
			}
		})
	}
}

func TestDefaultLaunchRestoresSavedSyncTarget(t *testing.T) {
	// Given a desktop profile configured through its native settings.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", filepath.Join(home, "AppData"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GOJOPLIN_CONFIG_PATH", "")
	t.Setenv("GOJOPLIN_NO_SYNC", "")
	profile := filepath.Join(home, "vault")
	db, err := store.Open(profile)
	if err != nil {
		t.Fatal(err)
	}
	settings := desktopSettings{Target: targetFolder, LocalDir: t.TempDir(), APIToken: "saved-token"}
	if err := settings.save(db); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	// When the independent default configuration is used on the next launch.
	launch, err := resolveLaunch(filepath.Join(home, "missing-launch.json"), "", profile, "")
	if err != nil {
		t.Fatal(err)
	}
	be, err := startBackend(launch.Config, launch.Profile, false, "127.0.0.1:0", launch)
	if err != nil {
		t.Fatal(err)
	}
	defer be.Close()

	// Then local defaults do not prevent the saved target from becoming active.
	if be.cfg.NoSync || be.cfg.SyncTarget != targetFolder || be.cfg.LocalDir != settings.LocalDir {
		t.Fatal("default config prevented the saved native sync target from being restored")
	}
}
