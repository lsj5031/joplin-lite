package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/lsj5031/joplin-lite/internal/config"
	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/service"
)

// TestDesktopBackendManualSync runs the real sync loop against a file-system
// target: it waits for the initial pass, pushes a new note with SyncNow, and
// checks that the target received it.
func TestDesktopBackendManualSync(t *testing.T) {
	target := t.TempDir()
	cfg := writeDesktopConfig(t, fmt.Sprintf("sync:\n  target: 2\n  local_dir: %q\n", filepath.ToSlash(target)))
	be, err := startBackend(cfg, filepath.Join(t.TempDir(), "vault"), false, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("startBackend: %v", err)
	}
	t.Cleanup(be.Close)

	if got := be.SyncState(); !got.Enabled {
		t.Fatal("sync is disabled in a backend configured with target 2")
	}
	first := waitDesktopSync(t, be, time.Time{})
	if first.Err != "" {
		t.Fatalf("initial sync failed: %s", first.Err)
	}

	note := &models.Note{Title: "Sync me", Body: "manual sync body"}
	if err := service.New(be.db).CreateNote(note); err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	if !be.SyncNow() {
		t.Fatal("SyncNow reported false with an engine present")
	}
	if st := be.SyncState(); !st.Pending && !st.Running {
		t.Fatalf("SyncNow did not mark the pass pending: %+v", st)
	}
	last := waitDesktopSync(t, be, first.Last)
	if last.Err != "" {
		t.Fatalf("manual sync failed: %s", last.Err)
	}

	found := false
	_ = filepath.Walk(target, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		body, err := os.ReadFile(path)
		if err == nil && (strings.Contains(string(body), note.ID) || strings.Contains(string(body), "Sync me")) {
			found = true
		}
		return nil
	})
	if !found {
		t.Fatalf("pushed note not found in target %s", target)
	}
}

// waitDesktopSync waits for a pass that started after the given time to end.
func waitDesktopSync(t *testing.T, be *backend, after time.Time) syncSnapshot {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		st := be.SyncState()
		if !st.Running && !st.Pending && !st.Last.IsZero() && st.Last.After(after) {
			return st
		}
		time.Sleep(25 * time.Millisecond)
	}
	st := be.SyncState()
	t.Fatalf("sync did not finish: %+v", st)
	return st
}

func writeDesktopConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// get does an HTTP GET and returns status + body ("" body on transport error).
func httpGet(url string) (int, string) {
	resp, err := http.Get(url)
	if err != nil {
		return 0, ""
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// TestDesktopBackendLifecycle exercises the parts of the desktop shell that do
// not need the webview: it binds the in-process API before any window exists,
// mints an ephemeral token, honours the address/data-dir overrides, and closes
// cleanly. Covers SH-04, SH-05, SH-08, SH-14, SH-15 and SH-16.
func TestDesktopBackendLifecycle(t *testing.T) {
	t.Setenv("GOJOPLIN_API_TOKEN", "")
	t.Setenv("GOJOPLIN_API_KEY", "")
	t.Setenv("GOJOPLIN_DATA_DIR", "")
	t.Setenv("GOJOPLIN_PORT", "")

	// No api.token in the file -> the session must mint an ephemeral one.
	cfg := writeDesktopConfig(t, "api:\n  key: \"desktop-key\"\n")
	dataDir := filepath.Join(t.TempDir(), "vault")

	// no-sync (SH-14) + explicit addr/data-dir overrides (SH-15/SH-16).
	be, err := startBackend(cfg, dataDir, true, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("startBackend: %v", err)
	}

	// SH-05: a fresh 48-hex token, not the configured one.
	if !regexp.MustCompile(`^[0-9a-f]{48}$`).MatchString(be.token) {
		t.Fatalf("ephemeral token = %q, want 48 hex chars", be.token)
	}

	// SH-16: the data-dir override is what was opened, and the profile key is
	// derived from it.
	abs, _ := filepath.Abs(dataDir)
	wantProfile := fmt.Sprintf("%x", sha256.Sum256([]byte(abs)))
	if be.profile != wantProfile {
		t.Fatalf("profile = %q, want sha256(dataDir) = %q", be.profile, wantProfile)
	}
	if _, err := os.Stat(filepath.Join(abs, "gojoplin.sqlite")); err != nil {
		t.Fatalf("database not created in overridden data dir: %v", err)
	}

	// SH-15 + SH-04: the API answers on the overridden loopback address,
	// before any window exists.
	if !strings.HasPrefix(be.baseURL, "http://127.0.0.1:") {
		t.Fatalf("baseURL = %q, want loopback", be.baseURL)
	}
	if code, body := httpGet(be.baseURL + "/ping"); code != 200 || body != "JoplinClipperServer" {
		t.Fatalf("GET /ping = %d %q", code, body)
	}

	// The token authorises data requests and is never persisted to the vault.
	if code, _ := httpGet(be.baseURL + "/notes?token=" + be.token); code != 200 {
		t.Fatalf("authorized GET /notes = %d, want 200", code)
	}
	db, err := os.ReadFile(filepath.Join(abs, "gojoplin.sqlite"))
	if err != nil {
		t.Fatalf("read db: %v", err)
	}
	if strings.Contains(string(db), be.token) {
		t.Fatal("ephemeral token was persisted into the database")
	}

	// SH-08: Close stops the server and is idempotent.
	be.Close()
	be.Close()
	if code, _ := httpGet(be.baseURL + "/ping"); code != 0 {
		t.Fatalf("API still answering after Close: %d", code)
	}
}

// TestDesktopBackendConfiguredTokenKeepsStable covers the other half of SH-05:
// when a token is configured it is reused verbatim (so a web clipper can paste
// it once), rather than replaced per session.
func TestDesktopBackendConfiguredTokenKeepsStable(t *testing.T) {
	t.Setenv("GOJOPLIN_API_TOKEN", "")
	t.Setenv("GOJOPLIN_API_KEY", "")
	t.Setenv("GOJOPLIN_DATA_DIR", "")

	const fixed = "0123456789abcdef0123456789abcdef"
	cfg := writeDesktopConfig(t, "api:\n  token: \""+fixed+"\"\n  key: \"desktop-key\"\n")

	be, err := startBackend(cfg, filepath.Join(t.TempDir(), "vault"), true, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("startBackend: %v", err)
	}
	defer be.Close()
	if be.token != fixed {
		t.Fatalf("token = %q, want the configured %q", be.token, fixed)
	}
	if code, _ := httpGet(be.baseURL + "/notes?token=" + fixed); code != 200 {
		t.Fatalf("configured token rejected: %d", code)
	}
}

// TestNewSyncBackendLocalFolder pins the file-system target (2) so the desktop
// shell can run the local E2EE/sync rehearsal, not only remote targets.
func TestNewSyncBackendLocalFolder(t *testing.T) {
	dir := t.TempDir()
	be, err := newSyncBackend(&config.Config{SyncTarget: 2, LocalDir: dir})
	if err != nil {
		t.Fatalf("newSyncBackend: %v", err)
	}
	if be.SyncTarget() != 2 {
		t.Fatalf("SyncTarget() = %d, want 2", be.SyncTarget())
	}
}

// TestDesktopBackendPortCollision covers SH-09's backend half: a bind failure
// is a clear error naming the address, not a silent fallback.
func TestDesktopBackendPortCollision(t *testing.T) {
	t.Setenv("GOJOPLIN_API_TOKEN", "")
	t.Setenv("GOJOPLIN_API_KEY", "")

	cfg := writeDesktopConfig(t, "api:\n  key: \"desktop-key\"\n")
	first, err := startBackend(cfg, filepath.Join(t.TempDir(), "a"), true, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("first startBackend: %v", err)
	}
	defer first.Close()

	addr := strings.TrimPrefix(first.baseURL, "http://")
	_, err = startBackend(cfg, filepath.Join(t.TempDir(), "b"), true, addr)
	if err == nil {
		t.Fatal("second bind on the same address unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), addr) {
		t.Fatalf("error %q does not name the address %q", err, addr)
	}
}

// TestDesktopBackendApplySettings covers the native settings path: a save is
// validated, persisted in key_values, and applied without a restart — the
// clipper accepts only the new token and sync moves to the new folder. A
// relaunch then picks the saved overlay up instead of the config file's target.
func TestDesktopBackendApplySettings(t *testing.T) {
	t.Setenv("GOJOPLIN_API_TOKEN", "")
	t.Setenv("GOJOPLIN_API_KEY", "")
	t.Setenv("GOJOPLIN_DATA_DIR", "")

	firstDir := t.TempDir()
	secondDir := t.TempDir()
	const oldToken = "old-token-0123456789abcdef0123456789abcdef"
	const newToken = "new-token-0123456789abcdef0123456789abcdef"

	cfgPath := writeDesktopConfig(t, fmt.Sprintf(
		"sync:\n  target: 2\n  local_dir: %q\napi:\n  token: %q\n",
		filepath.ToSlash(firstDir), oldToken))
	dataDir := filepath.Join(t.TempDir(), "vault")
	be, err := startBackend(cfgPath, dataDir, false, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("startBackend: %v", err)
	}
	t.Cleanup(be.Close)
	first := waitDesktopSync(t, be, time.Time{})
	if first.Err != "" {
		t.Fatalf("initial sync failed: %s", first.Err)
	}
	if code, _ := httpGet(be.baseURL + "/notes?token=" + oldToken); code != 200 {
		t.Fatalf("configured token before save = %d, want 200", code)
	}

	// An invalid save reports the missing field and changes nothing.
	if err := be.ApplySettings(desktopSettings{Target: targetServer, APIToken: newToken}); err == nil {
		t.Fatal("ApplySettings accepted a server target without a URL")
	}
	if code, _ := httpGet(be.baseURL + "/notes?token=" + oldToken); code != 200 {
		t.Fatalf("invalid save rotated the token anyway: %d", code)
	}

	next := desktopSettings{
		Target:   targetFolder,
		LocalDir: secondDir,
		APIToken: newToken,
	}
	if err := be.ApplySettings(next); err != nil {
		t.Fatalf("ApplySettings: %v", err)
	}
	if got := be.Settings(); got.LocalDir != secondDir || got.APIToken != newToken || got.Target != targetFolder {
		t.Fatalf("Settings after save = %+v", got)
	}

	// The new token works immediately; the old one is rejected.
	if code, _ := httpGet(be.baseURL + "/notes?token=" + oldToken); code != http.StatusForbidden {
		t.Fatalf("old token after save = %d, want 403", code)
	}
	if code, _ := httpGet(be.baseURL + "/notes?token=" + newToken); code != http.StatusOK {
		t.Fatalf("new token after save = %d, want 200", code)
	}

	// The restarted engine pushes to the new folder.
	note := &models.Note{Title: "After settings", Body: "moved target"}
	if err := service.New(be.db).CreateNote(note); err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	if !be.SyncNow() {
		t.Fatal("SyncNow reported false after the settings change")
	}
	if st := waitDesktopSync(t, be, first.Last); st.Err != "" {
		t.Fatalf("sync after settings change failed: %s", st.Err)
	}
	if !folderContains(t, secondDir, note.ID) {
		t.Fatalf("pushed note not found in the new target %s", secondDir)
	}

	// A relaunch reopens the same vault and inherits the saved overlay.
	be.Close()
	again, err := startBackend(cfgPath, dataDir, false, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("restart: %v", err)
	}
	defer again.Close()
	if got := again.Settings(); got.LocalDir != secondDir || got.APIToken != newToken || got.Target != targetFolder {
		t.Fatalf("Settings after relaunch = %+v", got)
	}
	if again.token != newToken {
		t.Fatalf("relaunched token = %q, want the saved one", again.token)
	}
	if code, _ := httpGet(again.baseURL + "/notes?token=" + newToken); code != 200 {
		t.Fatalf("saved token after relaunch = %d, want 200", code)
	}
}

// folderContains reports whether any file under dir mentions want.
func folderContains(t *testing.T, dir, want string) bool {
	t.Helper()
	found := false
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		body, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(body), want) {
			found = true
		}
		return nil
	})
	return found
}
