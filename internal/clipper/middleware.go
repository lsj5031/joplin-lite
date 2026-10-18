package clipper

import (
	"net/http"
	"strings"
)

// apiAuth implements Joplin's checkToken_ (packages/lib/services/rest/Api.ts):
//
//   - the ping and pairing endpoints are open — the extension ping-scans
//     41184–41194 unauthenticated and pairs without a token;
//   - everything else takes the API token as a **query parameter**
//     (`?token=<API_TOKEN>`) exactly like the official extension sends it;
//     `Authorization: Bearer <key>` is additionally accepted for our own
//     tooling;
//   - with no token configured, no auth is required (Joplin: "If the API has
//     been initialized without a token, it means no auth is needed").
func (s *Server) apiAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.Trim(r.URL.Path, "/")
		if isAuthOpen(r.Method, path) {
			next.ServeHTTP(w, r)
			return
		}
		apiToken := s.APIToken()
		if apiToken == "" && s.apiKey == "" {
			next.ServeHTTP(w, r)
			return
		}

		// Joplin Data API auth: ?token=...
		if token, ok := lookupParam(r.URL.Query(), "token"); ok {
			if token == apiToken {
				next.ServeHTTP(w, r)
				return
			}
			writeError(w, http.StatusForbidden, `Invalid "token" parameter`)
			return
		}

		// Bearer (our own CLI/tooling).
		if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
			t := strings.TrimPrefix(auth, "Bearer ")
			if t == s.apiKey || t == apiToken {
				next.ServeHTTP(w, r)
				return
			}
			writeError(w, http.StatusForbidden, "Invalid API key")
			return
		}

		writeError(w, http.StatusForbidden, `Missing "token" parameter`)
	})
}

// isAuthOpen mirrors Joplin's checkToken_ whitelist (plus our /health probe).
func isAuthOpen(method, path string) bool {
	switch method + " " + path {
	case "GET ping", "GET auth", "POST auth", "GET auth/check", "GET health":
		return true
	}
	return false
}
