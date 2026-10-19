package sync

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Dropbox OAuth2 (authorization code + PKCE) for sync target 7 — the user's
// own Dropbox OAuth app (scoped, "App folder" permission):
//
//   - authorization URL with code_challenge (S256), token_access_type=offline
//     so we get a refresh token, loopback redirect on 127.0.0.1;
//   - scopes files.content.read files.content.write files.metadata.read
//     (the delta emulation lists the app folder, which needs metadata.read);
//   - token exchange / refresh against https://api.dropboxapi.com/oauth2/token;
//   - the token is persisted as JSON in <dataDir>/dropbox_token.json. A
//     sync.7.auth JSON value (Joplin's config key) is accepted as bootstrap.
//
// The exact redirect URI must be registered in the Dropbox app console.

// DropboxToken is the OAuth2 token set for the Dropbox sync target.
type DropboxToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresAt    int64  `json:"expires_at"` // unix ms; 0 = no expiry
	TokenType    string `json:"token_type,omitempty"`
	Scope        string `json:"scope,omitempty"`
	UID          string `json:"uid,omitempty"`
	AccountID    string `json:"account_id,omitempty"`
}

// tokenExpirySkew is how early a token is considered expired.
const tokenExpirySkew = 60 * time.Second

func (t *DropboxToken) expired(now time.Time) bool {
	if t == nil || t.AccessToken == "" {
		return true
	}
	if t.ExpiresAt == 0 {
		return false
	}
	return now.UnixMilli() >= t.ExpiresAt-tokenExpirySkew.Milliseconds()
}

// DropboxOAuth runs the PKCE flow and renews tokens.
type DropboxOAuth struct {
	AppKey    string
	AppSecret string // optional: PKCE public clients don't need it
	AuthURL   string // default https://www.dropbox.com/oauth2/authorize
	TokenURL  string // default https://api.dropboxapi.com/oauth2/token
	Redirect  string // loopback redirect URI, default http://127.0.0.1:41723
	Scope     string // default files.content.read files.content.write files.metadata.read
	HTTP      *http.Client
}

// NewDropboxOAuth creates an OAuth client for the user's Dropbox app.
func NewDropboxOAuth(appKey, appSecret string) *DropboxOAuth {
	return &DropboxOAuth{
		AppKey:    appKey,
		AppSecret: appSecret,
		AuthURL:   "https://www.dropbox.com/oauth2/authorize",
		TokenURL:  "https://api.dropboxapi.com/oauth2/token",
		Redirect:  "http://127.0.0.1:41723",
		Scope:     "files.content.read files.content.write files.metadata.read",
		HTTP:      &http.Client{Timeout: 30 * time.Second},
	}
}

// newPKCE returns a code verifier and its S256 challenge.
func newPKCE() (verifier, challenge string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

// AuthCodeURL builds the authorization URL for the given PKCE challenge.
func (o *DropboxOAuth) AuthCodeURL(state, challenge string) string {
	v := url.Values{}
	v.Set("client_id", o.AppKey)
	v.Set("response_type", "code")
	v.Set("code_challenge", challenge)
	v.Set("code_challenge_method", "S256")
	v.Set("token_access_type", "offline")
	v.Set("redirect_uri", o.Redirect)
	v.Set("state", state)
	if o.Scope != "" {
		v.Set("scope", o.Scope)
	}
	return o.AuthURL + "?" + v.Encode()
}

// Exchange trades an authorization code for tokens (PKCE).
func (o *DropboxOAuth) Exchange(ctx context.Context, code, verifier string) (*DropboxToken, error) {
	form := url.Values{}
	form.Set("code", code)
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", o.AppKey)
	form.Set("redirect_uri", o.Redirect)
	form.Set("code_verifier", verifier)
	return o.tokenRequest(ctx, form)
}

// Refresh renews the access token using the refresh token.
func (o *DropboxOAuth) Refresh(ctx context.Context, refreshToken string) (*DropboxToken, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", o.AppKey)
	return o.tokenRequest(ctx, form)
}

func (o *DropboxOAuth) tokenRequest(ctx context.Context, form url.Values) (*DropboxToken, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if o.AppSecret != "" {
		req.SetBasicAuth(o.AppKey, o.AppSecret)
	}
	resp, err := o.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dropbox token endpoint returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var raw struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
		RefreshToken string `json:"refresh_token"`
		Scope        string `json:"scope"`
		UID          string `json:"uid"`
		AccountID    string `json:"account_id"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("cannot parse token response: %w", err)
	}
	if raw.AccessToken == "" {
		return nil, fmt.Errorf("token response has no access_token")
	}
	tok := &DropboxToken{
		AccessToken:  raw.AccessToken,
		RefreshToken: raw.RefreshToken,
		TokenType:    raw.TokenType,
		Scope:        raw.Scope,
		UID:          raw.UID,
		AccountID:    raw.AccountID,
	}
	if raw.ExpiresIn > 0 {
		tok.ExpiresAt = time.Now().UnixMilli() + raw.ExpiresIn*1000
	}
	return tok, nil
}

// DropboxTokenPath is where the OAuth token is persisted for a data dir.
func DropboxTokenPath(dataDir string) string {
	return filepath.Join(dataDir, "dropbox_token.json")
}

// LoadDropboxToken reads the persisted token, falling back to sync.7.auth:
// either Lite's JSON token set or official Joplin's existing bearer token.
func LoadDropboxToken(dataDir, authJSON string) (*DropboxToken, error) {
	if dataDir != "" {
		if data, err := os.ReadFile(DropboxTokenPath(dataDir)); err == nil {
			var t DropboxToken
			if err := json.Unmarshal(data, &t); err != nil {
				return nil, fmt.Errorf("cannot parse %s: %w", DropboxTokenPath(dataDir), err)
			}
			if t.AccessToken != "" {
				return &t, nil
			}
		}
	}
	if auth := strings.TrimSpace(authJSON); auth != "" {
		if !strings.ContainsAny(auth, "{}[]\" \t\r\n") {
			return &DropboxToken{AccessToken: auth, TokenType: "bearer"}, nil
		}
		var t DropboxToken
		if err := json.Unmarshal([]byte(auth), &t); err != nil {
			return nil, fmt.Errorf("cannot parse sync.7.auth: %w", err)
		}
		if t.AccessToken != "" {
			return &t, nil
		}
	}
	return nil, nil
}

// SaveDropboxToken persists the token under the data dir.
func SaveDropboxToken(dataDir string, t *DropboxToken) error {
	if dataDir == "" {
		return fmt.Errorf("no data dir to store the Dropbox token in")
	}
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(DropboxTokenPath(dataDir), data, 0o600)
}
