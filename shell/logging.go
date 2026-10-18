package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

// desktopLogName is the log file the desktop app appends to, inside the data
// directory. A packaged Windows build runs as a GUI program with no console, so
// this file is the only record of a startup failure, a failing sync pass or a
// shutdown problem. The headless `gojoplin serve` command keeps logging to
// stderr only.
const desktopLogName = "desktop.log"

// desktopLogMax rotates desktop.log to desktop.log.1 at the next launch. The
// app logs little, but a window left open with a failing sync loop must not
// grow the file forever.
const desktopLogMax = 1 << 20 // 1 MiB

// setupDesktopLog appends to <dataDir>/desktop.log and sends the default
// logger's output to the file and to stderr, so a console run keeps printing
// what the file receives. It returns a restore function that closes the file
// and puts the previous default logger back, for the next backend in the same
// process (a settings change rebuilds the sync engine, not the logger, but the
// tests start several backends).
func setupDesktopLog(dataDir string) (func(), error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	path := filepath.Join(dataDir, desktopLogName)
	if fi, err := os.Stat(path); err == nil && fi.Size() > desktopLogMax {
		_ = os.Remove(path + ".1")
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.MultiWriter(os.Stderr, f), &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))
	return func() {
		slog.SetDefault(prev)
		_ = f.Close()
	}, nil
}
