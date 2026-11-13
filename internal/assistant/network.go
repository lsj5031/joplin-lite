package assistant

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxModelResponseBytes = 32 << 20

// endpointTransport permits exactly one configured scheme, host, port and base
// path. It injects the user's credential where the SDK put its non-secret
// placeholder; the placeholder never leaves the process.
type endpointTransport struct {
	base          *http.Transport
	target        *url.URL
	key           string
	beforeRequest func() error
}

func newEndpointClient(settings Settings) (*http.Client, error) {
	target, err := url.Parse(settings.BaseURL)
	if err != nil {
		return nil, err
	}
	base := &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          4,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		ExpectContinueTimeout: time.Second,
		DisableCompression:    false,
	}
	transport := &endpointTransport{base: base, target: target, key: settings.APIKey}
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return errors.New("redirects are not allowed for Ask model requests")
		},
	}, nil
}

func (t *endpointTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := t.allowed(req.URL); err != nil {
		return nil, err
	}
	if t.beforeRequest != nil {
		if err := t.beforeRequest(); err != nil {
			return nil, err
		}
	}
	clone := req.Clone(req.Context())
	clone.Header = req.Header.Clone()
	if t.key != "" {
		clone.Header.Set("Authorization", "Bearer "+t.key)
	} else {
		clone.Header.Del("Authorization")
	}
	// No ambient cookies or unrelated credentials cross this boundary.
	clone.Header.Del("Cookie")
	response, err := t.base.RoundTrip(clone)
	if err != nil {
		return nil, err
	}
	response.Body = &boundedBody{ReadCloser: response.Body, remaining: maxModelResponseBytes}
	return response, nil
}

func (t *endpointTransport) allowed(u *url.URL) error {
	if u == nil {
		return errors.New("model request has no URL")
	}
	if u.Scheme != t.target.Scheme || u.Hostname() != t.target.Hostname() || effectivePort(u) != effectivePort(t.target) {
		return fmt.Errorf("model request to %s is outside the configured endpoint", u.Host)
	}
	basePath := strings.TrimSuffix(t.target.Path, "/")
	if basePath != "" && u.Path != basePath && !strings.HasPrefix(u.Path, basePath+"/") {
		return fmt.Errorf("model request path %s is outside the configured API base", u.Path)
	}
	return nil
}

type boundedBody struct {
	io.ReadCloser
	remaining int64
}

func (b *boundedBody) Read(p []byte) (int, error) {
	if b.remaining <= 0 {
		return 0, errors.New("model response exceeded the size limit")
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.ReadCloser.Read(p)
	b.remaining -= int64(n)
	if b.remaining <= 0 && err == nil {
		return n, errors.New("model response exceeded the size limit")
	}
	return n, err
}

func effectivePort(u *url.URL) string {
	if u.Port() != "" {
		return u.Port()
	}
	if u.Scheme == "https" {
		return "443"
	}
	return "80"
}

func allowModelRequest(target *url.URL) func(*http.Request) error {
	return func(req *http.Request) error {
		if req.URL == nil {
			return errors.New("model request has no URL")
		}
		if req.URL.Scheme != target.Scheme || req.URL.Hostname() != target.Hostname() || effectivePort(req.URL) != effectivePort(target) {
			return fmt.Errorf("fetch to %s is outside the configured model endpoint", req.URL.Host)
		}
		return nil
	}
}
