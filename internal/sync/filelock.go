package sync

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Flat-file lock protocol for file-based sync targets (Joplin's LockHandler,
// verified against packages/lib/services/synchronizer/LockHandler.ts):
//
//   - A lock is a file locks/<type>_<clientType>_<clientId>.json. The name is
//     the identity; the JSON body repeats type/clientType/clientId/updatedTime.
//   - Whether a lock is active is decided by the file's updated time on the
//     target against a 3-minute TTL — Joplin reads the timestamp from the file
//     listing, not from the JSON body.
//   - SYNC locks (type 1) can be held by several clients at once. An active
//     EXCLUSIVE lock (type 2) held by another client refuses sync
//     ("hasExclusiveLock" in Joplin).
//   - When several EXCLUSIVE locks are active, the winner is the one with the
//     oldest updatedTime, tie-broken by clientId (Joplin activeLock() sorts
//     ascending and takes the first).
//   - A held lock is refreshed every 60 s. If it expired it is NOT re-acquired
//     (the operation must be cancelled — Joplin 'lockExpired').
const (
	LockTypeSync      = 1
	LockTypeExclusive = 2

	LockClientTypeDesktop = 1
	LockClientTypeMobile  = 2
	LockClientTypeCLI     = 3

	// LockTTL is how long a lock file stays active without a refresh.
	LockTTL = 3 * time.Minute
	// LockRefreshInterval is how often a held lock is refreshed during a sync.
	LockRefreshInterval = time.Minute
)

// ErrExclusiveLock reports that another client holds an active EXCLUSIVE lock
// on the sync target. Syncing must be refused while it is held.
var ErrExclusiveLock = errors.New("sync target has an active exclusive lock")

// ErrLockExpired reports that our lock is no longer active and was therefore
// not refreshed (Joplin 'lockExpired').
var ErrLockExpired = errors.New("sync lock has expired")

// LockFile is one lock file as listed on the target.
type LockFile struct {
	Name        string // e.g. "1_3_abc.json"
	UpdatedTime int64  // file updated time on the target (unix ms)
}

// lockStore is the minimal flat-file target surface the lock protocol needs.
// LocalFolderBackend implements it over os calls; the Dropbox driver
// implements it over files/* calls.
type lockStore interface {
	getLock(name string) ([]byte, error) // nil, nil when missing
	putLock(name string, data []byte) error
	deleteLock(name string) error
	listLocks() ([]LockFile, error)
}

// fileLockManager runs the flat-file lock protocol against a lockStore for
// one client identity.
type fileLockManager struct {
	store      lockStore
	clientType int
	clientID   string
	nowMs      func() int64
}

func newFileLockManager(store lockStore, clientType int, clientID string) *fileLockManager {
	return &fileLockManager{store: store, clientType: clientType, clientID: clientID,
		nowMs: func() int64 { return time.Now().UnixMilli() }}
}

func lockFileName(l *Lock) string {
	return fmt.Sprintf("%d_%d_%s.json", l.Type, l.ClientType, l.ClientID)
}

// parseLockFileName parses "<type>_<clientType>_<clientId>.json" (Joplin
// lockNameToObject). The clientId is the remainder after the second underscore.
func parseLockFileName(name string) (Lock, bool) {
	if !strings.HasSuffix(name, ".json") {
		return Lock{}, false
	}
	base := strings.TrimSuffix(name, ".json")
	parts := strings.SplitN(base, "_", 3)
	if len(parts) != 3 {
		return Lock{}, false
	}
	t, err1 := strconv.Atoi(parts[0])
	ct, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || (t != LockTypeSync && t != LockTypeExclusive) {
		return Lock{}, false
	}
	return Lock{Type: t, ClientType: ct, ClientID: parts[2]}, true
}

// lockIsActive mirrors Joplin lockIsActive: now - updatedTime < TTL.
func (m *fileLockManager) lockIsActive(l *Lock) bool {
	return m.nowMs()-l.UpdatedTime < LockTTL.Milliseconds()
}

// listedLocks returns the locks on the target with their updated times (the
// file listing's timestamp is authoritative, as in Joplin).
func (m *fileLockManager) listedLocks() ([]Lock, error) {
	files, err := m.store.listLocks()
	if err != nil {
		return nil, err
	}
	var out []Lock
	for _, f := range files {
		l, ok := parseLockFileName(f.Name)
		if !ok {
			continue
		}
		l.UpdatedTime = f.UpdatedTime
		out = append(out, l)
	}
	return out, nil
}

// activeLock mirrors Joplin activeLock(): for EXCLUSIVE the winner among the
// active locks is the oldest (updatedTime, then clientId); for SYNC the first
// active lock matching the given client identity.
func activeLock(locks []Lock, nowMs, ttlMs int64, lockType, clientType int, clientID string) *Lock {
	active := func(l Lock) bool { return nowMs-l.UpdatedTime < ttlMs }
	if lockType == LockTypeExclusive {
		var candidates []Lock
		for _, l := range locks {
			if l.Type == LockTypeExclusive && active(l) {
				candidates = append(candidates, l)
			}
		}
		sort.Slice(candidates, func(i, j int) bool {
			if candidates[i].UpdatedTime == candidates[j].UpdatedTime {
				return candidates[i].ClientID < candidates[j].ClientID
			}
			return candidates[i].UpdatedTime < candidates[j].UpdatedTime
		})
		if len(candidates) == 0 {
			return nil
		}
		return &candidates[0]
	}
	for _, l := range locks {
		if l.Type != lockType || !active(l) {
			continue
		}
		if clientType != 0 && l.ClientType != clientType {
			continue
		}
		if clientID != "" && l.ClientID != clientID {
			continue
		}
		l := l
		return &l
	}
	return nil
}

// Acquire mirrors Joplin acquireSyncLock: refuse when an active EXCLUSIVE lock
// exists, refresh our own active SYNC lock, otherwise write ours and verify it
// reads back. On any failure our own lock file is released again.
func (m *fileLockManager) Acquire() (*Lock, error) {
	isFirstPass := true
	for {
		locks, err := m.listedLocks()
		if err != nil {
			return nil, fmt.Errorf("cannot list locks: %w", err)
		}
		nowMs := m.nowMs()

		if exclusive := activeLock(locks, nowMs, LockTTL.Milliseconds(), LockTypeExclusive, 0, ""); exclusive != nil {
			err := fmt.Errorf("%w: (clientType %d #%s)", ErrExclusiveLock, exclusive.ClientType, exclusive.ClientID)
			m.releaseOwn()
			return nil, err
		}

		ours := activeLock(locks, nowMs, LockTTL.Milliseconds(), LockTypeSync, m.clientType, m.clientID)
		if ours != nil {
			// Refresh if this is a re-acquire or the lock is more than 10 s old.
			if isFirstPass || nowMs-ours.UpdatedTime > 10_000 {
				if err := m.save(ours); err != nil {
					m.releaseOwn()
					return nil, err
				}
			}
			return ours, nil
		}

		if !isFirstPass {
			m.releaseOwn()
			return nil, errors.New("cannot acquire sync lock: the lock could not be written or was expired before it could be read back")
		}

		if err := m.save(&Lock{Type: LockTypeSync, ClientType: m.clientType, ClientID: m.clientID}); err != nil {
			return nil, err
		}
		isFirstPass = false
	}
}

// Refresh rewrites our held SYNC lock while it is still active (Joplin's
// auto-refresh). An expired lock is NOT re-acquired — the caller must stop.
func (m *fileLockManager) Refresh() error {
	locks, err := m.listedLocks()
	if err != nil {
		return fmt.Errorf("cannot list locks: %w", err)
	}
	ours := activeLock(locks, m.nowMs(), LockTTL.Milliseconds(), LockTypeSync, m.clientType, m.clientID)
	if ours == nil {
		return ErrLockExpired
	}
	return m.save(ours)
}

// Release deletes our lock file.
func (m *fileLockManager) Release() error {
	return m.releaseOwn()
}

func (m *fileLockManager) releaseOwn() error {
	l := &Lock{Type: LockTypeSync, ClientType: m.clientType, ClientID: m.clientID}
	if err := m.store.deleteLock(lockFileName(l)); err != nil {
		slog.Warn("failed to release lock", "name", lockFileName(l), "error", err)
		return err
	}
	return nil
}

func (m *fileLockManager) save(l *Lock) error {
	l.UpdatedTime = m.nowMs()
	data, err := json.Marshal(l)
	if err != nil {
		return err
	}
	return m.store.putLock(lockFileName(l), data)
}
