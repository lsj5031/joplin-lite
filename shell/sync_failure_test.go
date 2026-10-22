package main

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"syscall"
	"testing"
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
