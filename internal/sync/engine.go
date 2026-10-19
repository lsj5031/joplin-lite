package sync

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/lsj5031/joplin-lite/internal/e2ee"
	"github.com/lsj5031/joplin-lite/internal/store"
)

// Engine orchestrates the sync process: authenticate -> lock -> pull -> decrypt -> push -> unlock.
type Engine struct {
	backend        SyncBackend
	db             *store.DB
	e2ee           *e2ee.Service
	masterPassword string
	triggerCh      chan struct{}

	// syncMu is a try-lock: a trigger that arrives while a sync is running
	// must not start a second overlapping pass over the same target and DB.
	syncMu sync.Mutex
}

// NewEngine creates a new sync engine with the given sync backend (HTTP client or S3).
func NewEngine(backend SyncBackend, db *store.DB, masterPassword string) *Engine {
	return &Engine{
		backend:        backend,
		db:             db,
		e2ee:           e2ee.NewService(),
		masterPassword: masterPassword,
		triggerCh:      make(chan struct{}, 1),
	}
}

// TriggerSync signals the background sync loop to run immediately.
func (e *Engine) TriggerSync() {
	select {
	case e.triggerCh <- struct{}{}:
	default:
		// Already a trigger pending
	}
}

// TriggerCh returns the channel that receives sync trigger signals.
func (e *Engine) TriggerCh() <-chan struct{} {
	return e.triggerCh
}

// usesFileLocks reports whether a sync target uses flat-file locks
// (locks/<type>_<clientType>_<clientId>.json): 2 = file system,
// 7 = Dropbox, 8 = S3. Joplin Server (9) and Joplin Cloud (10) have built-in
// locks through /api/locks.
func usesFileLocks(syncTarget int) bool {
	switch syncTarget {
	case 2, 7, 8:
		return true
	}
	return false
}

// LockRefresher is optionally implemented by backends with TTL locks so the
// engine can refresh the held lock during long syncs (Joplin: every 60 s
// against a 3-minute TTL).
type LockRefresher interface {
	RefreshLock(lock interface{}) error
}

// startLockRefresh refreshes the held lock until the returned stop is called.
// An expired lock is not re-acquired — the failure is reported and the loop
// stops (Joplin 'lockExpired' cancels the operation).
func startLockRefresh(r LockRefresher, lock interface{}) func() {
	stopCh := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(LockRefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				if err := r.RefreshLock(lock); err != nil {
					slog.Error("lock refresh failed", "error", err)
					return
				}
			}
		}
	}()
	return func() {
		close(stopCh)
		<-done
	}
}

// Sync performs a full sync cycle. Overlapping calls are refused (not queued):
// the buffered trigger already coalesces requests, and two passes racing on
// one target is how duplicate pushes and lock churn happen.
func (e *Engine) Sync(ctx context.Context) error {
	if !e.syncMu.TryLock() {
		slog.Info("sync already running; ignoring overlapping trigger")
		return nil
	}
	defer e.syncMu.Unlock()

	// Authenticate if needed
	if !e.backend.IsAuthenticated() {
		if err := e.backend.Authenticate(); err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
	}

	// Acquire lock. On file targets an active EXCLUSIVE lock (or any lock
	// failure) refuses the sync — proceeding unlocked risks data loss on a
	// shared target. Joplin Server/Cloud keep the lenient fallback because
	// some server versions don't support locks.
	lock, err := e.backend.AcquireLock()
	if err != nil {
		if usesFileLocks(e.backend.SyncTarget()) {
			return fmt.Errorf("sync refused: %w", err)
		}
		slog.Warn("cannot acquire sync lock, proceeding without lock", "error", err)
	}
	defer func() {
		if lock != nil {
			e.backend.ReleaseLock(lock)
		}
	}()
	if lock != nil {
		if refresher, ok := e.backend.(LockRefresher); ok {
			defer startLockRefresh(refresher, lock)()
		}
	}

	// Check for cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Pull changes from server
	slog.Info("pulling changes from server")
	if err := PullChanges(e.backend, e.db); err != nil {
		return fmt.Errorf("pull failed: %w", err)
	}

	// Reconcile: fetch any items the delta missed (e.g. master keys)
	if err := PullMissingItems(e.backend, e.db); err != nil {
		return fmt.Errorf("reconciliation failed: %w", err)
	}

	// Fetch master keys from info.json (Joplin stores them there, not as .md
	// items) and enforce the target format version.
	if err := PullSyncInfo(e.backend, e.db); err != nil {
		return fmt.Errorf("sync refused: %w", err)
	}

	// Decrypt pulled items
	if e.masterPassword != "" {
		slog.Info("decrypting pulled items")
		if err := DecryptPulledItems(e.db, e.e2ee, e.masterPassword); err != nil {
			return fmt.Errorf("decryption failed: %w", err)
		}
	}

	// Check for cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if err := requireEncryptionKey(e.db, e.e2ee); err != nil {
		return err
	}

	// Publish local master keys / e2ee state to info.json before pushing
	// items encrypted with those keys, so other clients can decrypt them.
	if err := PushSyncInfo(e.backend, e.db); err != nil {
		return fmt.Errorf("push sync info failed: %w", err)
	}

	// Push local changes to server
	slog.Info("pushing local changes to server")
	if err := PushChanges(e.backend, e.db, e.e2ee); err != nil {
		return fmt.Errorf("push failed: %w", err)
	}

	slog.Info("sync completed")
	return nil
}
