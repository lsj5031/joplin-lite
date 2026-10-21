package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// defaultDesktopConfig keeps Windows first launch independent of official
// credentials. Other platforms retain the config loader's existing defaults.
func defaultDesktopConfig() (_ string, err error) {
	if runtime.GOOS != "windows" {
		return "", nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	yaml := filepath.Join(home, ".config", "go-joplin", "config.yaml")
	if _, err := os.Stat(yaml); err == nil {
		return yaml, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect desktop YAML config: %w", err)
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find desktop config directory: %w", err)
	}
	dir = filepath.Join(dir, "joplin-lite")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create desktop config directory: %w", err)
	}
	path := filepath.Join(dir, "config.json")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return path, nil
	}
	if err != nil {
		return "", fmt.Errorf("create desktop config: %w", err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close desktop config: %w", closeErr))
		}
	}()
	if _, err := f.WriteString("{}\n"); err != nil {
		return "", fmt.Errorf("write desktop config: %w", err)
	}
	return path, nil
}
