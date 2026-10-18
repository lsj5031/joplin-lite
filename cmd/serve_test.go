package cmd

import "testing"

func TestAPIBase(t *testing.T) {
	cases := []struct {
		addr string
		want string
	}{
		{"localhost:41184", "http://localhost:41184"},
		{"127.0.0.1:41888", "http://127.0.0.1:41888"},
		{"0.0.0.0:41184", "http://127.0.0.1:41184"},
		{"[::]:41184", "http://127.0.0.1:41184"},
		{"[::1]:41184", "http://127.0.0.1:41184"},
		{"[fe80::1]:41184", "http://[fe80::1]:41184"},
		{"no-port", "http://no-port"},
	}
	for _, c := range cases {
		if got := apiBase(c.addr); got != c.want {
			t.Errorf("apiBase(%q) = %q, want %q", c.addr, got, c.want)
		}
	}
}

func TestWebDirPath(t *testing.T) {
	oldDir := webDir
	t.Cleanup(func() { webDir = oldDir })
	t.Setenv("GOJOPLIN_WEB_DIR", "")

	webDir = ""
	if got := webDirPath(); got != "" {
		t.Fatalf("webDirPath() = %q, want empty (preview off)", got)
	}
	t.Setenv("GOJOPLIN_WEB_DIR", "web/dist/client")
	if got := webDirPath(); got != "web/dist/client" {
		t.Fatalf("webDirPath() from env = %q", got)
	}
	webDir = "tmp/dist"
	if got := webDirPath(); got != "tmp/dist" {
		t.Fatalf("flag must win, webDirPath() = %q", got)
	}
}
