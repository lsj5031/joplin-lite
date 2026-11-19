package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const fixtureClipperToken = "synthetic-cli-regression-token"

func TestCLIProcess(t *testing.T) {
	// A fresh process exercises the real main, including Cobra's output and exit.
	if os.Getenv("GOJOPLIN_TEST_PROCESS") != "1" {
		return
	}
	separator := slices.Index(os.Args, "--")
	if separator == -1 {
		t.Fatal("missing CLI arguments")
	}
	os.Args = append(os.Args[:1:1], os.Args[separator+1:]...)
	main()
}

func runCLI(t *testing.T, dir string, args ...string) (string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], append([]string{"-test.run=^TestCLIProcess$", "--"}, args...)...)
	cmd.Env = []string{
		"GOJOPLIN_TEST_PROCESS=1",
		"HOME=" + dir,
		"USERPROFILE=" + dir,
		"GOJOPLIN_API_TOKEN=" + fixtureClipperToken,
	}
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("CLI %v timed out: %s", args, out)
	}
	if err == nil {
		return string(out), 0
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("run CLI %v: %v", args, err)
	}
	return string(out), exit.ExitCode()
}

func writeCLIConfig(t *testing.T, dir, contents string) string {
	t.Helper()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCLIServeStartupDoesNotLogClipperToken(t *testing.T) {
	dir := t.TempDir()
	// Hold an ephemeral port to exercise startup logging without racing for a
	// free port or leaving a server running.
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	config := writeCLIConfig(t, dir, fmt.Sprintf(`
api:
  token: "${GOJOPLIN_API_TOKEN}"
  key: "synthetic-cli-regression-key"
server:
  data_dir: data
  listen_host: "127.0.0.1"
  port: %d
`, listener.Addr().(*net.TCPAddr).Port))
	out, code := runCLI(t, dir, "serve", "--config", config, "--no-sync")
	if code != 1 || !strings.Contains(out, "starting clipper server") || !strings.Contains(out, "is already in use") {
		t.Fatalf("serve did not reach the expected bind failure (exit %d):\n%s", code, out)
	}
	if strings.Contains(out, fixtureClipperToken) {
		t.Errorf("serve logged the fixture API token:\n%s", out)
	}
}

func TestCLISyncOperationalFailureOmitsUsage(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "target"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "target", "locks"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	config := writeCLIConfig(t, dir, `
sync:
  target: 2
  local_dir: target
server:
  data_dir: data
`)
	out, code := runCLI(t, dir, "sync", "--config", config)
	if code != 1 || !strings.Contains(out, "sync failed: sync refused:") || !strings.Contains(out, filepath.Join("target", "locks")) {
		t.Errorf("sync lost its actionable failure (exit %d):\n%s", code, out)
	}
	if strings.Contains(out, "Usage:") {
		t.Errorf("sync printed usage for an operational failure:\n%s", out)
	}
}

func TestCLISyncHelp(t *testing.T) {
	dir := t.TempDir()
	// Help must not try to load this nonexistent config.
	config := filepath.Join(dir, "missing.yaml")
	out, code := runCLI(t, dir, "sync", "--config", config, "--help")
	if code != 0 || !strings.Contains(out, "Usage:") || !strings.Contains(out, "gojoplin sync [flags]") {
		t.Fatalf("sync --help did not show command help (exit %d):\n%s", code, out)
	}
}

func TestCLISyncConfigFailureOmitsUsage(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "missing.yaml")
	out, code := runCLI(t, dir, "sync", "--config", config)
	if code != 1 || !strings.Contains(out, "cannot read config file "+config) || strings.Contains(out, "Usage:") {
		t.Fatalf("sync config failure did not report the error without usage (exit %d):\n%s", code, out)
	}
}

func TestCLISyncInvalidFlagShowsUsage(t *testing.T) {
	dir := t.TempDir()
	out, code := runCLI(t, dir, "sync", "--config", filepath.Join(dir, "missing.yaml"), "--invalid-flag")
	if code != 1 || !strings.Contains(out, "unknown flag: --invalid-flag") || !strings.Contains(out, "Usage:") {
		t.Fatalf("sync did not report the invalid flag with usage (exit %d):\n%s", code, out)
	}
}
