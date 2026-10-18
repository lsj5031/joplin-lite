package store

import "testing"

// Settings KV (MOD-08): GetSetting/SetSetting are the exported settings seam
// for the UI service layer. They must round-trip values, report an unset key as
// an empty string without error, and survive a database reopen.

func TestSettingsKVRoundTrip(t *testing.T) {
	db := testDB(t)

	if got, err := db.GetSetting("sync.target"); err != nil || got != "" {
		t.Fatalf("GetSetting(unset) = %q, %v; want \"\", nil", got, err)
	}

	cases := []struct{ key, value string }{
		{"sync.target", "7"},
		{"api.token", "ui-token"},
		{"e2ee.master_password_hint", "correct horse battery staple"},
		{"unicode", "κληδούχος 🙂\n第二行"},
		{"empty", ""},
	}
	for _, c := range cases {
		if err := db.SetSetting(c.key, c.value); err != nil {
			t.Fatalf("SetSetting(%q): %v", c.key, err)
		}
		got, err := db.GetSetting(c.key)
		if err != nil {
			t.Fatalf("GetSetting(%q): %v", c.key, err)
		}
		if got != c.value {
			t.Errorf("GetSetting(%q) = %q, want %q", c.key, got, c.value)
		}
	}

	// Setting an existing key overwrites rather than duplicating it.
	if err := db.SetSetting("sync.target", "8"); err != nil {
		t.Fatalf("SetSetting(overwrite): %v", err)
	}
	if got, _ := db.GetSetting("sync.target"); got != "8" {
		t.Errorf("after overwrite, GetSetting = %q, want \"8\"", got)
	}
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM key_values WHERE key = 'sync.target'`).Scan(&rows); err != nil {
		t.Fatalf("count key_values: %v", err)
	}
	if rows != 1 {
		t.Errorf("key_values rows for sync.target = %d, want 1", rows)
	}
}

func TestSettingsKV_PersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.SetSetting("server.data_dir", "/tmp/vault"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { reopened.Close() })
	if got, err := reopened.GetSetting("server.data_dir"); err != nil || got != "/tmp/vault" {
		t.Fatalf("GetSetting after reopen = %q, %v; want \"/tmp/vault\", nil", got, err)
	}
}
