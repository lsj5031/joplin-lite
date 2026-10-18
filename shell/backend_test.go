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

	"github.com/lsj5031/joplin-lite/internal/config"
)

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
