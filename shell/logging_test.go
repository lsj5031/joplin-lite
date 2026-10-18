package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDesktopLogWritesAndRotates covers the file a GUI launch logs to: a log
// past the size cap moves aside to desktop.log.1, the new file gets the
// messages, and the previous default logger comes back when the app closes.
func TestDesktopLogWritesAndRotates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, desktopLogName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	big := strings.Repeat("x", desktopLogMax+1)
	if err := os.WriteFile(path, []byte(big), 0o644); err != nil {
		t.Fatalf("seed a large log: %v", err)
	}

	before := slog.Default()
	restore, err := setupDesktopLog(dir)
	if err != nil {
		t.Fatalf("setupDesktopLog: %v", err)
	}
	slog.Info("logging to the data directory", "marker", "after-rotation")
	restore()

	if slog.Default() != before {
		t.Fatal("setup did not restore the previous default logger")
	}
	rotated, err := os.Stat(path + ".1")
	if err != nil {
		t.Fatalf("no rotated log: %v", err)
	}
	if rotated.Size() != int64(len(big)) {
		t.Fatalf("rotated size = %d, want %d", rotated.Size(), len(big))
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read desktop.log: %v", err)
	}
	if !strings.Contains(string(got), "logging to the data directory") || !strings.Contains(string(got), "after-rotation") {
		t.Fatalf("desktop.log = %q", got)
	}
	if strings.Contains(string(got), "xxxx") {
		t.Fatal("the rotated content stayed in the new log")
	}

	// The restored logger must still work: it writes to stderr, not to the
	// file that was just closed.
	slog.Info("after restore")
	if second, err := os.ReadFile(path); err != nil || strings.Contains(string(second), "after restore") {
		t.Fatalf("a closed log file still received output: %q (%v)", second, err)
	}
}

// TestStartBackendLogsToDataDir runs the real startup path and checks that the
// data directory holds what happened: the app banner, the migrations, the API
// address and the sync state.
func TestStartBackendLogsToDataDir(t *testing.T) {
	cfg := writeDesktopConfig(t, "sync:\n  target: 0\n")
	vault := filepath.Join(t.TempDir(), "vault")
	be, err := startBackend(cfg, vault, true, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("startBackend: %v", err)
	}
	be.Close()

	got, err := os.ReadFile(filepath.Join(vault, desktopLogName))
	if err != nil {
		t.Fatalf("read desktop.log: %v", err)
	}
	text := string(got)
	for _, want := range []string{
		"desktop app starting",
		"database opened",
		"desktop API listening",
		"sync disabled",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("desktop.log is missing %q:\n%s", want, text)
		}
	}
}
