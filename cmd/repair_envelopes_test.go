package cmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepairEnvelopesCLIHasNoProfileDependency(t *testing.T) {
	cmd := newRepairEnvelopesCmd()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{t.TempDir()})
	// This command must not load config or touch the default profile.
	previous := cfgFile
	cfgFile = filepath.Join(t.TempDir(), "does-not-exist.yaml")
	defer func() { cfgFile = previous }()
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if output.String() != "affected: 0\n" {
		t.Fatalf("unexpected output: %q", output.String())
	}
	cmd = newRepairEnvelopesCmd()
	cmd.SetArgs([]string{t.TempDir(), "--apply", "--backup", "/unused", "--rollback", "/unused"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("invalid flags: %v", err)
	}
}
