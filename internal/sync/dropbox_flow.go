package sync

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

// RunDropboxOAuthFlow runs the loopback PKCE flow: it serves the redirect
// URI on 127.0.0.1, prints the authorization URL and waits for the browser
// callback before trading the code for tokens.
func RunDropboxOAuthFlow(ctx context.Context, oauth *DropboxOAuth, out io.Writer) (*DropboxToken, error) {
	verifier, challenge, err := newPKCE()
	if err != nil {
		return nil, err
	}
	state, err := randomState()
	if err != nil {
		return nil, err
	}
	redirect, err := url.Parse(oauth.Redirect)
	if err != nil {
		return nil, fmt.Errorf("invalid redirect URI %q: %w", oauth.Redirect, err)
	}
	ln, err := net.Listen("tcp", redirect.Host)
	if err != nil {
		return nil, fmt.Errorf("cannot listen on %s (is another client using it?): %w", redirect.Host, err)
	}
	defer ln.Close()

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if e := q.Get("error"); e != "" {
			http.Error(w, "Authorization denied: "+e, http.StatusBadRequest)
			errCh <- fmt.Errorf("authorization denied: %s", e)
			return
		}
		if q.Get("state") != state {
			http.Error(w, "State mismatch — go back to the terminal and retry.", http.StatusBadRequest)
			errCh <- fmt.Errorf("state mismatch in OAuth callback")
			return
		}
		fmt.Fprintln(w, "Dropbox authorized — you can close this tab and return to the terminal.")
		codeCh <- q.Get("code")
	})}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	fmt.Fprintf(out, "Open this URL in your browser and approve access:\n\n  %s\n\nWaiting for the redirect on %s ...\n",
		oauth.AuthCodeURL(state, challenge), redirect.Host)

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-errCh:
		return nil, err
	case code := <-codeCh:
		return oauth.Exchange(ctx, code, verifier)
	case <-time.After(5 * time.Minute):
		return nil, fmt.Errorf("timed out waiting for the Dropbox redirect")
	}
}

func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
