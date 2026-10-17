package clipper

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/url"
)

// Clipper pairing handshake (spec: joplinapp.org/help/dev/spec/clipper_auth,
// verified against packages/lib/services/rest/routes/auth.ts):
//
//	POST /auth            -> {"auth_token": "<random>"}  (status: waiting)
//	GET  /auth/check?auth_token=<x>
//	                     -> {"status":"waiting"}
//	                      | {"status":"accepted","token":"<API_TOKEN>"}
//	                      | {"status":"rejected"}
//	GET  /auth/check?token=<x> -> {"valid": true|false}  (token validation)
//
// The app shows a real Accept/Reject prompt for the pairing request — the
// previous auto-accept is gone: without a decision the request stays
// "waiting" forever (Joplin shows a notification until it is answered).

// AuthPrompt decides clipper pairing requests (Joplin shows a native
// Accept/Reject dialog). PromptAuth blocks until the user decides and reports
// whether the request was accepted.
type AuthPrompt interface {
	PromptAuth(authToken string) bool
}

// authState mirrors Joplin's Api auth token state: one current pairing
// request at a time, waiting -> accepted/rejected.
type authState struct {
	token  string
	status string // "waiting" | "accepted" | "rejected"
}

const (
	authStatusWaiting  = "waiting"
	authStatusAccepted = "accepted"
	authStatusRejected = "rejected"
)

// ResolveAuth accepts or rejects a pending pairing request. This is the hook
// for UIs (the M4 shell); the console prompt calls it too. Reports whether
// the request was still pending.
func (s *Server) ResolveAuth(authToken string, accept bool) bool {
	s.authMu.Lock()
	defer s.authMu.Unlock()
	if s.auth == nil || s.auth.token != authToken || s.auth.status != authStatusWaiting {
		return false
	}
	if accept {
		s.auth.status = authStatusAccepted
	} else {
		s.auth.status = authStatusRejected
	}
	return true
}

// PendingAuth returns the current pairing request's auth token and status
// ("" when there is none) — for surfacing the prompt in a UI.
func (s *Server) PendingAuth() (authToken, status string) {
	s.authMu.Lock()
	defer s.authMu.Unlock()
	if s.auth == nil {
		return "", ""
	}
	return s.auth.token, s.auth.status
}

// handleAuth handles POST /auth: generate the auth token and wait for the
// user's Accept/Reject decision.
func (s *Server) handleAuth(w http.ResponseWriter, r *http.Request) {
	if !allowPairingOrigin(r) {
		writeError(w, http.StatusForbidden, "This endpoint cannot be called from a web page")
		return
	}
	token, err := newAuthToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot generate auth token")
		return
	}

	s.authMu.Lock()
	s.auth = &authState{token: token, status: authStatusWaiting}
	prompt := s.prompt
	s.authMu.Unlock()

	if prompt != nil {
		// Ask the user in the background — the extension polls /auth/check.
		go func() {
			s.ResolveAuth(token, prompt.PromptAuth(token))
		}()
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"auth_token": token})
}

// handleAuthCheck handles GET /auth/check (see the spec above).
func (s *Server) handleAuthCheck(w http.ResponseWriter, r *http.Request) {
	if !allowPairingOrigin(r) {
		writeError(w, http.StatusForbidden, "This endpoint cannot be called from a web page")
		return
	}
	q := r.URL.Query()

	if authToken, ok := lookupParam(q, "auth_token"); ok {
		s.authMu.Lock()
		st := s.auth
		s.authMu.Unlock()
		if st == nil || st.token != authToken {
			// Joplin throws "Invalid auth token: <x>" here.
			writeError(w, http.StatusBadRequest, "Invalid auth token: "+authToken)
			return
		}
		resp := map[string]interface{}{"status": st.status}
		if st.status == authStatusAccepted {
			resp["token"] = s.apiToken
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	if token, ok := lookupParam(q, "token"); ok {
		writeJSON(w, http.StatusOK, map[string]interface{}{"valid": token == s.apiToken})
		return
	}

	writeError(w, http.StatusBadRequest, "Invalid request")
}

// lookupParam returns a query parameter and whether it is present at all
// (Joplin distinguishes "auth_token in request.query" from its value).
func lookupParam(q url.Values, name string) (string, bool) {
	vals, ok := q[name]
	if !ok {
		return "", false
	}
	if len(vals) == 0 {
		return "", true
	}
	return vals[0], true
}

// allowPairingOrigin mirrors ClipperServer.ts isAllowedPairingOrigin: the
// pairing endpoints hand out the API token without requiring one, so they
// must not be callable from a web page. The extension calls them from an
// extension origin (chrome-extension://, moz-extension://); a web page
// carries an http(s) origin. No-Origin requests (native clients) are allowed.
func allowPairingOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return u.Scheme != "http" && u.Scheme != "https"
}

func newAuthToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
