package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	dropboxAttempts   = 4
	dropboxRetryWait  = 12 * time.Second
	dropboxWriteSpace = 100 * time.Millisecond
)

// DropboxAPIError preserves the provider status and earliest allowed retry
// through engine and partial-push errors.
type DropboxAPIError struct {
	Status  int
	RetryAt time.Time
	summary string
}

func (e *DropboxAPIError) Error() string {
	if e.summary != "" {
		return fmt.Sprintf("dropbox API error %d: %s", e.Status, e.summary)
	}
	return fmt.Sprintf("dropbox API error %d", e.Status)
}

func (e *DropboxAPIError) Retryable() bool {
	return e.Status == http.StatusTooManyRequests || (e.Status >= 500 && e.Status < 600)
}

// DropboxRetryAt finds the latest provider hint across joined push errors.
// A transient error without a provider hint returns a zero time and true.
func DropboxRetryAt(err error) (at time.Time, retryable bool) {
	switch e := err.(type) {
	case *DropboxAPIError:
		return e.RetryAt, e.Retryable()
	case interface{ Unwrap() []error }:
		for _, cause := range e.Unwrap() {
			when, ok := DropboxRetryAt(cause)
			if ok && (!retryable || when.After(at)) {
				at, retryable = when, true
			}
		}
	case interface{ Unwrap() error }:
		return DropboxRetryAt(e.Unwrap())
	}
	return at, retryable
}

func dropboxError(status int, header http.Header, body []byte, now time.Time) error {
	var e struct {
		ErrorSummary string      `json:"error_summary"`
		RetryAfter   json.Number `json:"retry_after"`
		Error        struct {
			Tag        string      `json:".tag"`
			RetryAfter json.Number `json:"retry_after"`
			Path       struct {
				Tag string `json:".tag"`
			} `json:"path"`
			PathLookup struct {
				Tag string `json:".tag"`
			} `json:"path_lookup"`
		} `json:"error"`
	}
	// Only decode the fields needed for decisions; never echo arbitrary bodies.
	_ = json.Unmarshal(body, &e)
	if status == http.StatusConflict && e.Error.Tag == "reset" {
		return errDropboxCursorReset
	}
	out := &DropboxAPIError{Status: status, summary: e.ErrorSummary}
	if e.ErrorSummary == "" {
		switch e.Error.Tag {
		case "path":
			out.summary = "path/" + e.Error.Path.Tag
		case "path_lookup":
			out.summary = "path_lookup/" + e.Error.PathLookup.Tag
		}
	}
	for _, s := range []string{header.Get("Retry-After"), string(e.RetryAfter), string(e.Error.RetryAfter)} {
		if at := retryTime(s, now); at.After(out.RetryAt) {
			out.RetryAt = at
		}
	}
	return out
}

func retryTime(s string, now time.Time) time.Time {
	s = strings.TrimSpace(s)
	if seconds, err := strconv.ParseInt(s, 10, 64); err == nil && seconds >= 0 {
		return now.Add(time.Duration(min(seconds, int64((1<<63-1)/time.Second))) * time.Second)
	}
	if date, err := http.ParseTime(s); err == nil && date.After(now) {
		return date
	}
	return time.Time{}
}

func retryableDropbox(err error) bool {
	var api *DropboxAPIError
	var network net.Error
	return (errors.As(err, &api) && api.Retryable()) ||
		(errors.As(err, &network) && (network.Timeout() || network.Temporary())) ||
		errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE)
}

// A definitively rejected request has nothing to verify. Do not make an
// extra metadata request while Dropbox is asking the client to wait.
func ambiguousDropboxResponse(err error) bool {
	var api *DropboxAPIError
	if errors.As(err, &api) {
		return api.Status >= 500 && api.Status < 600 && !api.RetryAt.After(time.Now())
	}
	return retryableDropbox(err)
}

func dropboxBackoff(attempt int) time.Duration {
	base := 200 * time.Millisecond << min(attempt, 5)
	return base + rand.N(base/2)
}

func waitUntil(ctx context.Context, at time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d := time.Until(at); d > 0 {
		timer := time.NewTimer(d)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return ctx.Err()
}

// A rate limit applies across concurrent workers, not just the rejected
// request. After a 429, space out writes as workers drain the backlog.
type dropboxPacer struct {
	mu         sync.Mutex
	paused     *DropboxAPIError
	pauseUntil time.Time
	paceUntil  time.Time
	nextWrite  time.Time
}

func (p *dropboxPacer) throttle(err *DropboxAPIError, until time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if until.After(p.pauseUntil) {
		p.pauseUntil, p.paused = until, err
	}
	p.paceUntil = p.pauseUntil.Add(time.Minute)
}

func (p *dropboxPacer) admit(ctx context.Context, write bool, budget time.Time) error {
	for ctx.Err() == nil {
		p.mu.Lock()
		now := time.Now()
		at := p.pauseUntil
		paced := write && now.Before(p.paceUntil)
		if paced && p.nextWrite.After(at) {
			at = p.nextWrite
		}
		if !now.Before(at) {
			if paced {
				p.nextWrite = now.Add(dropboxWriteSpace)
			}
			p.mu.Unlock()
			return ctx.Err()
		}
		reason := p.paused
		p.mu.Unlock()
		if at.After(budget) {
			return reason
		}
		if err := waitUntil(ctx, at); err != nil {
			return err
		}
	}
	return ctx.Err()
}

// do replays only reads and revision-guarded mutations. The setup builds an
// identical request body for every attempt; there is no generic POST replay.
func (b *DropboxBackend) do(ctx context.Context, endpoint string, requestTimeout time.Duration, write bool, setup func(*http.Request)) ([]byte, error) {
	refreshed := false
	for attempt := 0; ; {
		token, err := b.accessToken(ctx, nil)
		if err != nil {
			return nil, err
		}
		if err := b.pacer.admit(ctx, write, time.Now().Add(dropboxRetryWait)); err != nil {
			return nil, err
		}
		body, status, err := b.request(ctx, endpoint, requestTimeout, token.AccessToken, setup)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if status == http.StatusUnauthorized {
			if refreshed {
				return nil, b.rejectedToken(ctx, token)
			}
			if _, err := b.accessToken(ctx, token); err != nil {
				return nil, err
			}
			refreshed = true
			continue
		}
		if err == nil {
			return body, nil
		}
		if !retryableDropbox(err) {
			return nil, err
		}
		at := time.Now().Add(dropboxBackoff(attempt))
		var api *DropboxAPIError
		if errors.As(err, &api) {
			if api.RetryAt.After(at) {
				at = api.RetryAt
			}
			if status == http.StatusTooManyRequests {
				b.pacer.throttle(api, at)
			}
		}
		attempt++
		if attempt >= dropboxAttempts || time.Until(at) > dropboxRetryWait {
			return nil, err
		}
		if err := waitUntil(ctx, at); err != nil {
			return nil, err
		}
	}
}

func (b *DropboxBackend) request(ctx context.Context, endpoint string, timeout time.Duration, access string, setup func(*http.Request)) ([]byte, int, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "", nil)
	if err != nil {
		return nil, 0, err
	}
	setup(req)
	req.Header.Set("Authorization", "Bearer "+access)
	b.requests.Add(1)
	started := time.Now()
	resp, err := b.http.Do(req)
	if err != nil {
		slog.Warn("Dropbox request failed", "endpoint", endpoint, "stage", "response headers", "duration", time.Since(started), "error", err)
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
	if err != nil {
		slog.Warn("Dropbox request failed", "endpoint", endpoint, "stage", "response body", "duration", time.Since(started), "status", resp.StatusCode, "error", err)
		// A known error status still carries retry information in headers.
		if resp.StatusCode != http.StatusOK {
			return nil, resp.StatusCode, dropboxError(resp.StatusCode, resp.Header, body, time.Now())
		}
		return nil, resp.StatusCode, err
	}
	if elapsed := time.Since(started); elapsed >= 5*time.Second || resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
		slog.Warn("Dropbox request finished", "endpoint", endpoint, "duration", elapsed, "status", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, dropboxError(resp.StatusCode, resp.Header, body, time.Now())
	}
	return body, resp.StatusCode, nil
}
