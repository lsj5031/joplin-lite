package clipper

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lsj5031/joplin-lite/internal/store"
)

const (
	testAPIToken = "api-token-1"
	testAPIKey   = "api-key-1"
)

// testPrompt records the pairing request and returns a fixed decision.
type testPrompt struct {
	accept bool
	called chan string
}

func (p *testPrompt) PromptAuth(authToken string) bool {
	select {
	case p.called <- authToken:
	default:
	}
	return p.accept
}

func newTestServer(t *testing.T, prompt AuthPrompt) (*Server, *httptest.Server) {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	s := NewServer(db, testAPIToken, testAPIKey, nil, nil, prompt)
	ts := httptest.NewServer(s.Router())
	t.Cleanup(ts.Close)
	return s, ts
}

func doReq(t *testing.T, method, url string, headers map[string]string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

// TestClipperAuthHandshake runs the official extension's pairing flow:
// POST /auth -> poll /auth/check until the user decides (Joplin spec).
func TestClipperAuthHandshake(t *testing.T) {
	prompt := &testPrompt{accept: true, called: make(chan string, 1)}
	_, ts := newTestServer(t, prompt)

	status, body := doReq(t, http.MethodPost, ts.URL+"/auth", nil)
	if status != http.StatusOK {
		t.Fatalf("POST /auth = %d: %s", status, body)
	}
	var issued struct {
		AuthToken string `json:"auth_token"`
	}
	if err := json.Unmarshal(body, &issued); err != nil || issued.AuthToken == "" {
		t.Fatalf("POST /auth body = %q (err=%v), want {auth_token}", body, err)
	}

	// While pending: {"status":"waiting"} — even the very first poll may beat
	// the prompt goroutine, so poll until it settles.
	check := pollCheck(t, ts.URL, issued.AuthToken)
	if check.Status != authStatusAccepted {
		t.Fatalf("status = %q, want accepted (prompt accepted)", check.Status)
	}
	if check.Token != testAPIToken {
		t.Fatalf("token = %q, want the API token %q", check.Token, testAPIToken)
	}
	// The prompt saw exactly this pairing token.
	select {
	case tok := <-prompt.called:
		if tok != issued.AuthToken {
			t.Fatalf("prompt got %q, want %q", tok, issued.AuthToken)
		}
	default:
		t.Fatal("prompt was never called")
	}

	// A rejected pairing reports {"status":"rejected"} with no token.
	prompt.accept = false
	status, body = doReq(t, http.MethodPost, ts.URL+"/auth", nil)
	if status != http.StatusOK {
		t.Fatalf("POST /auth (2nd) = %d: %s", status, body)
	}
	issued = struct {
		AuthToken string `json:"auth_token"`
	}{}
	if err := json.Unmarshal(body, &issued); err != nil {
		t.Fatal(err)
	}
	check = pollCheck(t, ts.URL, issued.AuthToken)
	if check.Status != authStatusRejected {
		t.Fatalf("status = %q, want rejected", check.Status)
	}
	if check.Token != "" {
		t.Fatalf("rejected check leaked the API token: %q", check.Token)
	}

	// A still-pending request waits until ResolveAuth (no prompt wired).
	s2, ts2 := newTestServer(t, nil)
	status, body = doReq(t, http.MethodPost, ts2.URL+"/auth", nil)
	var issued2 struct {
		AuthToken string `json:"auth_token"`
	}
	json.Unmarshal(body, &issued2)
	check = pollCheck(t, ts2.URL, issued2.AuthToken)
	if check.Status != authStatusWaiting {
		t.Fatalf("status without prompt = %q, want waiting", check.Status)
	}
	if !s2.ResolveAuth(issued2.AuthToken, true) {
		t.Fatal("ResolveAuth failed on a pending request")
	}
	check = pollCheck(t, ts2.URL, issued2.AuthToken)
	if check.Status != authStatusAccepted || check.Token != testAPIToken {
		t.Fatalf("after ResolveAuth got %q (token %q)", check.Status, check.Token)
	}
	if s2.ResolveAuth(issued2.AuthToken, false) {
		t.Fatal("ResolveAuth must not re-decide a finished request")
	}
}

// pollCheck polls /auth/check until the status settles (the prompt decides
// asynchronously), decoding each response fresh.
func pollCheck(t *testing.T, baseURL, authToken string) struct {
	Status string `json:"status"`
	Token  string `json:"token"`
} {
	t.Helper()
	var check struct {
		Status string `json:"status"`
		Token  string `json:"token"`
	}
	for i := 0; i < 50; i++ {
		status, body := doReq(t, http.MethodGet, baseURL+"/auth/check?auth_token="+authToken, nil)
		if status != http.StatusOK {
			t.Fatalf("GET /auth/check = %d: %s", status, body)
		}
		check = struct {
			Status string `json:"status"`
			Token  string `json:"token"`
		}{}
		if err := json.Unmarshal(body, &check); err != nil {
			t.Fatal(err)
		}
		if check.Status != authStatusWaiting {
			return check
		}
	}
	return check
}

func TestClipperAuthCheckTokenValidation(t *testing.T) {
	_, ts := newTestServer(t, nil)

	// Unknown auth_token -> error (Joplin throws "Invalid auth token").
	status, body := doReq(t, http.MethodGet, ts.URL+"/auth/check?auth_token=bogus", nil)
	if status != http.StatusBadRequest || !strings.Contains(string(body), "Invalid auth token") {
		t.Fatalf("check with bad auth_token = %d %s", status, body)
	}

	// Bare /auth/check -> "Invalid request".
	status, body = doReq(t, http.MethodGet, ts.URL+"/auth/check", nil)
	if status != http.StatusBadRequest {
		t.Fatalf("bare check = %d %s", status, body)
	}

	// ?token= mode validates the API token.
	status, body = doReq(t, http.MethodGet, ts.URL+"/auth/check?token="+testAPIToken, nil)
	if status != http.StatusOK || !strings.Contains(string(body), `"valid":true`) {
		t.Fatalf("check?token=apiToken = %d %s", status, body)
	}
	status, body = doReq(t, http.MethodGet, ts.URL+"/auth/check?token=wrong", nil)
	if status != http.StatusOK || !strings.Contains(string(body), `"valid":false`) {
		t.Fatalf("check?token=wrong = %d %s", status, body)
	}
}

// TestClipperPairingOrigin: pairing must not be callable from a web page
// (ClipperServer.ts isAllowedPairingOrigin).
func TestClipperPairingOrigin(t *testing.T) {
	_, ts := newTestServer(t, nil)

	status, _ := doReq(t, http.MethodPost, ts.URL+"/auth", map[string]string{"Origin": "https://evil.example"})
	if status != http.StatusForbidden {
		t.Fatalf("POST /auth from web origin = %d, want 403", status)
	}
	status, _ = doReq(t, http.MethodPost, ts.URL+"/auth", map[string]string{"Origin": "http://localhost:8080"})
	if status != http.StatusForbidden {
		t.Fatalf("POST /auth from http origin = %d, want 403", status)
	}
	status, _ = doReq(t, http.MethodPost, ts.URL+"/auth", map[string]string{"Origin": "chrome-extension://abcdefgh"})
	if status != http.StatusOK {
		t.Fatalf("POST /auth from extension origin = %d, want 200", status)
	}
	status, _ = doReq(t, http.MethodPost, ts.URL+"/auth", nil)
	if status != http.StatusOK {
		t.Fatalf("POST /auth without Origin = %d, want 200", status)
	}
}

// TestClipperTokenAuth pins the Data API auth matrix (Joplin checkToken_):
// open ping/pairing, ?token= for everything else, Bearer for our tooling.
func TestClipperTokenAuth(t *testing.T) {
	_, ts := newTestServer(t, nil)

	// Open endpoints need no token.
	status, body := doReq(t, http.MethodGet, ts.URL+"/ping", nil)
	if status != http.StatusOK || string(body) != "JoplinClipperServer" {
		t.Fatalf("GET /ping = %d %q", status, body)
	}

	// No token -> 403 Missing "token" parameter.
	status, body = doReq(t, http.MethodGet, ts.URL+"/notes", nil)
	if status != http.StatusForbidden || !strings.Contains(string(body), `Missing \"token\" parameter`) {
		t.Fatalf("GET /notes without token = %d %s", status, body)
	}

	// Wrong token -> 403 Invalid "token" parameter.
	status, body = doReq(t, http.MethodGet, ts.URL+"/notes?token=wrong", nil)
	if status != http.StatusForbidden || !strings.Contains(string(body), `Invalid \"token\" parameter`) {
		t.Fatalf("GET /notes?token=wrong = %d %s", status, body)
	}

	// The extension's way: ?token=<API_TOKEN>.
	status, body = doReq(t, http.MethodGet, ts.URL+"/notes?token="+testAPIToken, nil)
	if status != http.StatusOK {
		t.Fatalf("GET /notes?token=apiToken = %d %s", status, body)
	}

	// Our tooling's way: Bearer.
	status, body = doReq(t, http.MethodGet, ts.URL+"/notes", map[string]string{"Authorization": "Bearer " + testAPIKey})
	if status != http.StatusOK {
		t.Fatalf("GET /notes with Bearer key = %d %s", status, body)
	}
}

// TestClipperRevisionsRoute: the route set includes /revisions (Joplin's
// does). A fresh store serves an empty collection and 404s unknown ids —
// seeded history is covered by TestClipperRevisionsAPI.
func TestClipperRevisionsRoute(t *testing.T) {
	_, ts := newTestServer(t, nil)

	status, body := doReq(t, http.MethodGet, ts.URL+"/revisions?token="+testAPIToken, nil)
	if status != http.StatusOK {
		t.Fatalf("GET /revisions = %d %s", status, body)
	}
	var page struct {
		Items   []map[string]interface{} `json:"items"`
		HasMore bool                     `json:"has_more"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("GET /revisions body = %q: %v", body, err)
	}
	if len(page.Items) != 0 || page.HasMore {
		t.Fatalf("GET /revisions = %v, want empty page", page)
	}

	status, _ = doReq(t, http.MethodGet, ts.URL+"/revisions/abcdef01234567890abcdef012345678?token="+testAPIToken, nil)
	if status != http.StatusNotFound {
		t.Fatalf("GET /revisions/{id} = %d, want 404", status)
	}
}

// TestClipperServePortCollision: the bind failure must say clearly that
// Joplin desktop is on the port.
func TestClipperServePortCollision(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	httpSrv := &http.Server{Addr: ln.Addr().String(), Handler: http.NewServeMux()}
	err = Serve(httpSrv)
	if err == nil {
		t.Fatal("Serve succeeded on an occupied port")
	}
	if !strings.Contains(err.Error(), "Joplin desktop") || !strings.Contains(err.Error(), "clipper port") {
		t.Fatalf("Serve error lacks the collision message: %v", err)
	}
}

// TestClipperTokenRotation covers SetAPIToken: the desktop settings dialog
// replaces the token without restarting the server, so the old token must stop
// working at once and the new one must authorise the same routes.
func TestClipperTokenRotation(t *testing.T) {
	s, ts := newTestServer(t, nil)

	if code, _ := doReq(t, "GET", ts.URL+"/notes?token="+testAPIToken, nil); code != http.StatusOK {
		t.Fatalf("old token GET /notes = %d, want 200", code)
	}
	s.SetAPIToken("api-token-2")
	if got := s.APIToken(); got != "api-token-2" {
		t.Fatalf("APIToken() = %q", got)
	}
	if code, _ := doReq(t, "GET", ts.URL+"/notes?token="+testAPIToken, nil); code != http.StatusForbidden {
		t.Fatalf("rotated-out token GET /notes = %d, want 403", code)
	}
	if code, _ := doReq(t, "GET", ts.URL+"/notes?token=api-token-2", nil); code != http.StatusOK {
		t.Fatalf("new token GET /notes = %d, want 200", code)
	}
	code, body := doReq(t, "GET", ts.URL+"/auth/check?token=api-token-2", nil)
	if code != http.StatusOK || !strings.Contains(string(body), `"valid":true`) {
		t.Fatalf("new token GET /auth/check = %d %s", code, body)
	}
}
