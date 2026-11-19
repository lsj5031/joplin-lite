package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"syscall"
	"testing"
	"time"

	jsync "github.com/lsj5031/joplin-lite/internal/sync"
)

func TestSyncFilesystemFailuresAreNotOffline(t *testing.T) {
	for _, cause := range []error{syscall.EACCES, syscall.ENOENT, syscall.ENOSPC, syscall.EIO, syscall.ETIMEDOUT} {
		for _, local := range []error{
			&os.PathError{Op: "open", Path: "no such host/i/o timeout.pem", Err: cause},
			&os.LinkError{Op: "rename", Old: "old", New: "new", Err: cause},
		} {
			err := fmt.Errorf("pull failed: cannot apply delta item: %w", local)
			if got := syncFailureFor(err); got != syncFailureOther {
				t.Errorf("%v classified as %s", err, got)
			}
			if retryAfter(err) != 0 {
				t.Errorf("local failure retried automatically: %v", err)
			}
			notice := syncNoticeFor(syncSnapshot{Enabled: true, Err: err.Error(), Kind: syncFailureFor(err)})
			if notice.Line != "Sync failed" || !notice.Err {
				t.Errorf("local failure notice = %+v", notice)
			}
		}
	}
	if isOffline(syscall.EACCES) || isOffline(nil) {
		t.Fatal("bare permission error or nil classified as offline")
	}
}

func TestSyncNetworkFailuresStillRetry(t *testing.T) {
	for _, cause := range []error{
		&net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED},
		&net.DNSError{Err: "unknown host", Name: "example.invalid", IsNotFound: true},
		os.ErrDeadlineExceeded,
	} {
		err := fmt.Errorf("pull failed: %w", &url.Error{Op: "Get", URL: "https://example.invalid", Err: cause})
		if syncFailureFor(err) != syncFailureOffline || retryAfter(err) != syncRetryNetwork {
			t.Errorf("network failure no longer retries: %v", err)
		}
	}
	if isOffline(errors.New("invalid credentials")) {
		t.Fatal("authentication failure classified as offline")
	}
}

func TestSyncLoopHonorsDropboxRetryTime(t *testing.T) {
	at := time.Now().Add(1500 * time.Millisecond)
	err := fmt.Errorf("push failed: %w", errors.Join(
		&jsync.DropboxAPIError{Status: http.StatusForbidden},
		&jsync.DropboxAPIError{Status: http.StatusTooManyRequests, RetryAt: at.Add(-time.Second)},
		&jsync.DropboxAPIError{Status: http.StatusServiceUnavailable, RetryAt: at},
	))
	engine := &fakeSyncer{errs: []error{err}, trigger: make(chan struct{}, 1)}
	status := &syncStatus{}
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		syncLoop(ctx, engine, status)
	}()
	defer func() { cancel(); <-done }()
	for status.snapshot(true).Retry.IsZero() {
		select {
		case <-ctx.Done():
			t.Fatal("no desktop retry was scheduled")
		case <-time.After(5 * time.Millisecond):
		}
	}
	snap := status.snapshot(true)
	if snap.Kind != syncFailureDropbox || snap.Retry.Before(at) || snap.Retry.After(at.Add(200*time.Millisecond)) {
		t.Fatalf("desktop retry did not use the latest provider hint: %+v", snap)
	}
	if notice := syncNoticeFor(snap); notice.Line != "Dropbox is busy · sync paused" || notice.Hint != "retrying at "+snap.Retry.Format("15:04:05") {
		t.Fatalf("retry time is not shown in the desktop footer: %+v", notice)
	}
	engine.trigger <- struct{}{}
	time.Sleep(50 * time.Millisecond)
	if calls := engine.count(); calls != 1 {
		t.Fatalf("queued edit bypassed Dropbox retry window: %d passes", calls)
	}
	for engine.count() != 2 || status.snapshot(true).Err != "" {
		select {
		case <-ctx.Done():
			t.Fatal("desktop did not retry and clear its status")
		case <-time.After(5 * time.Millisecond):
		}
	}
	serverError := &jsync.DropboxAPIError{Status: http.StatusInternalServerError}
	if retryAfter(serverError) != syncRetryNetwork {
		t.Fatal("5xx without a provider hint should retry sooner than the regular tick")
	}
	if retryAfter(jsync.ErrDropboxReauthorize) != 0 || syncFailureFor(jsync.ErrDropboxReauthorize) != syncFailureAuth {
		t.Fatal("reauthorization should be actionable, not treated as transient")
	}
}
