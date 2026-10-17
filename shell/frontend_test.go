package main

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func testHandler(t *testing.T, rt runtimeConfig) http.Handler {
	t.Helper()
	fsys := fstest.MapFS{
		"index.html":    {Data: []byte("<html><head><title>x</title></head><body>shell</body></html>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
		"favicon.ico":   {Data: []byte("ico")},
	}
	h, err := newFrontendHandler(fsys, rt)
	if err != nil {
		t.Fatalf("newFrontendHandler: %v", err)
	}
	return h
}

func get(t *testing.T, h http.Handler, path string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	body, _ := io.ReadAll(rec.Result().Body)
	return rec, string(body)
}

func TestFrontendInjectsRuntimeConfig(t *testing.T) {
	h := testHandler(t, runtimeConfig{Base: "http://127.0.0.1:4123", Token: "secret", Shell: "desktop", Profile: "profile-a"})

	rec, body := get(t, h, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(body, `window.__GOJOPLIN__={"base":"http://127.0.0.1:4123","token":"secret","shell":"desktop","profile":"profile-a"}`) {
		t.Fatalf("runtime config not injected before </head>:\n%s", body)
	}
	if strings.Index(body, "__GOJOPLIN__") > strings.Index(body, "</head>") {
		t.Fatal("runtime config injected after </head>; the app bundle would run first")
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("index Cache-Control = %q", got)
	}
}

func TestFrontendEscapesScriptBreakout(t *testing.T) {
	h := testHandler(t, runtimeConfig{Base: "http://127.0.0.1:1", Token: `</script><script>alert(1)</script>`, Shell: "desktop"})
	_, body := get(t, h, "/")
	if strings.Contains(body, "<script>alert(1)") {
		t.Fatal("token can break out of the injected script tag")
	}
	if !strings.Contains(body, `\u003c/script\u003e`) {
		t.Fatalf("token not JSON-escaped:\n%s", body)
	}
}

func TestFrontendSPAFallbackAndAssets(t *testing.T) {
	h := testHandler(t, runtimeConfig{Base: "http://127.0.0.1:1", Token: "t", Shell: "desktop"})

	rec, body := get(t, h, "/notes/some-note")
	if rec.Code != http.StatusOK || !strings.Contains(body, "<body>shell</body>") {
		t.Fatalf("client route did not fall back to index: %d %q", rec.Code, body)
	}

	rec, body = get(t, h, "/assets/app.js")
	if rec.Code != http.StatusOK || body != "console.log(1)" {
		t.Fatalf("asset = %d %q", rec.Code, body)
	}
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Fatalf("asset Cache-Control = %q", got)
	}

	rec, _ = get(t, h, "/assets/missing.js")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing asset status = %d, want 404", rec.Code)
	}
}

func TestFrontendServesHeadWithoutBody(t *testing.T) {
	h := testHandler(t, runtimeConfig{Shell: "desktop"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/", nil))
	body, _ := io.ReadAll(rec.Result().Body)
	if rec.Code != http.StatusOK || len(body) != 0 {
		t.Fatalf("HEAD / = %d, %d body bytes; want 200 with no body", rec.Code, len(body))
	}
	if rec.Header().Get("Content-Length") == "" {
		t.Fatal("HEAD / has no Content-Length")
	}
}

func TestFrontendRejectsWrites(t *testing.T) {
	h := testHandler(t, runtimeConfig{Shell: "desktop"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want 405", rec.Code)
	}
}

func TestBaseURLForLoopback(t *testing.T) {
	cases := []struct {
		addr *net.TCPAddr
		want string
	}{
		{&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 41888}, "http://127.0.0.1:41888"},
		{&net.TCPAddr{IP: net.ParseIP("::1"), Port: 41888}, "http://127.0.0.1:41888"},
		{&net.TCPAddr{IP: net.IPv4zero, Port: 41888}, "http://127.0.0.1:41888"},
		{&net.TCPAddr{IP: net.ParseIP("192.168.1.5"), Port: 80}, "http://192.168.1.5:80"},
	}
	for _, c := range cases {
		if got := baseURLFor(c.addr); got != c.want {
			t.Errorf("baseURLFor(%v) = %q, want %q", c.addr, got, c.want)
		}
	}
}
