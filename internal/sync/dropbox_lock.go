package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (b *DropboxBackend) lockExpires(updated int64) time.Time {
	return time.UnixMilli(updated).Add(b.lockTTL)
}

// RefreshLock is a single bounded refresh, also useful outside an engine pass.
func (b *DropboxBackend) RefreshLock(lock interface{}) error {
	if lock == nil {
		return nil
	}
	held := lock.(*Lock)
	updated, err := b.refreshLease(b.ctx, held, b.lockExpires(held.UpdatedTime))
	if err == nil {
		held.UpdatedTime = updated
	}
	return err
}

// keepLock retries transient refresh failures only while the last confirmed
// lease is still active. Neither a retry nor an ambiguous response extends it.
func (b *DropboxBackend) keepLock(ctx context.Context, lock interface{}) error {
	held := lock.(*Lock)
	expires := b.lockExpires(held.UpdatedTime)
	next := expires.Add(-b.lockTTL).Add(b.lockInterval)
	var last error
	for attempt := 0; ; {
		if err := waitUntil(ctx, minTime(next, expires)); err != nil {
			return err
		}
		if !time.Now().Before(expires) {
			return errors.Join(ErrLockExpired, last)
		}
		updated, err := b.refreshLease(ctx, held, expires)
		if err == nil {
			expires = b.lockExpires(updated)
			next, attempt, last = time.UnixMilli(updated).Add(b.lockInterval), 0, nil
			continue
		}
		if errors.Is(err, ErrLockExpired) || errors.Is(err, ErrExclusiveLock) {
			return err
		}
		last = err
		if !retryableDropbox(err) {
			next = expires
			continue
		}
		next = time.Now().Add(dropboxBackoff(attempt))
		if at, ok := DropboxRetryAt(err); ok && at.After(next) {
			next = at
		}
		attempt++
	}
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func (b *DropboxBackend) refreshLease(ctx context.Context, lock *Lock, expires time.Time) (int64, error) {
	ctx, cancel := context.WithDeadline(ctx, expires)
	defer cancel()
	name := lockFileName(lock)
	path := "/locks/" + name
	entries, err := b.listFolder(ctx, "/locks")
	if isDropboxNotFound(err) {
		return 0, ErrLockExpired
	}
	if err != nil {
		return 0, fmt.Errorf("cannot list locks: %w", err)
	}
	now := time.Now()
	var ours *dropboxEntry
	for _, entry := range entries {
		if entry.Tag != "file" {
			continue
		}
		otherName := strings.TrimPrefix(entry.PathDisplay, "/locks/")
		other, ok := parseLockFileName(otherName)
		if !ok || !now.Before(b.lockExpires(entryTimeMs(entry))) {
			continue
		}
		if other.Type == LockTypeExclusive {
			return 0, ErrExclusiveLock
		}
		if otherName == name {
			ours = &entry
		}
	}
	if ours == nil {
		return 0, ErrLockExpired
	}
	// The revision is a fence: even if the file disappears after listing
	// (including between retry attempts), an update cannot recreate it.
	writeCtx, stop := context.WithDeadline(ctx, b.lockExpires(entryTimeMs(*ours)))
	defer stop()
	next := *lock
	next.UpdatedTime = now.UnixMilli()
	data, err := json.Marshal(&next)
	if err != nil {
		return 0, err
	}
	entry, err := b.uploadRevision(writeCtx, path, data, ours, b.lockTimeout)
	if isDropboxStatus(err, 409) {
		return 0, fmt.Errorf("%w: Dropbox lock revision changed: %v", ErrLockExpired, err)
	}
	if err != nil {
		return 0, err
	}
	updated := entryTimeMs(*entry)
	if updated == 0 || !time.Now().Before(b.lockExpires(updated)) {
		return 0, ErrLockExpired
	}
	return updated, nil
}
