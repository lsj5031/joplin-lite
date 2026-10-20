package sync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/lsj5031/joplin-lite/internal/models"
	"github.com/lsj5031/joplin-lite/internal/store"
)

// writeTargetLock writes a lock file into a local-folder target's locks/ dir
// with the given age (mtime), as another client would have left it.
func writeTargetLock(t *testing.T, dir, name string, age time.Duration) {
	t.Helper()
	locks := filepath.Join(dir, "locks")
	if err := os.MkdirAll(locks, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(locks, name)
	if err := os.WriteFile(p, []byte(`{"type":2,"clientType":1,"clientId":"desktop"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(p, when, when); err != nil {
		t.Fatal(err)
	}
}

func TestFileLock_AcquireAndRelease(t *testing.T) {
	dir := t.TempDir()
	b := NewLocalFolderBackend(dir)

	lock, err := b.AcquireLock()
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	l, ok := lock.(*Lock)
	if !ok || l == nil || l.Type != LockTypeSync {
		t.Fatalf("AcquireLock returned %+v, want a SYNC *Lock", lock)
	}
	lockPath := filepath.Join(dir, "locks", lockFileName(l))
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("lock file not written: %v", err)
	}

	if err := b.ReleaseLock(lock); err != nil {
		t.Fatalf("ReleaseLock: %v", err)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("lock file still present after release (stat err=%v)", err)
	}
}

func TestFileLock_ExclusiveLockRefusesAcquire(t *testing.T) {
	dir := t.TempDir()
	b := NewLocalFolderBackend(dir)

	// Another client (desktop) holds a fresh EXCLUSIVE lock.
	writeTargetLock(t, dir, "2_1_desktop.json", 0)

	_, err := b.AcquireLock()
	if !errors.Is(err, ErrExclusiveLock) {
		t.Fatalf("AcquireLock = %v, want ErrExclusiveLock", err)
	}
	// We must not have left our own lock file behind.
	if files, err := os.ReadDir(filepath.Join(dir, "locks")); err != nil || len(files) != 1 {
		t.Fatalf("our lock file exists after refused acquire (stat err=%v)", err)
	}

	// An expired EXCLUSIVE lock (TTL is 3 min) no longer blocks.
	writeTargetLock(t, dir, "2_1_desktop.json", 4*time.Minute)
	lock, err := b.AcquireLock()
	if err != nil {
		t.Fatalf("AcquireLock with expired exclusive lock: %v", err)
	}
	if err := b.ReleaseLock(lock); err != nil {
		t.Fatalf("ReleaseLock: %v", err)
	}
}

func TestFileLock_RefreshAndExpiry(t *testing.T) {
	dir := t.TempDir()
	b := NewLocalFolderBackend(dir)

	lock, err := b.AcquireLock()
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	lockPath := filepath.Join(dir, "locks", lockFileName(lock.(*Lock)))

	// Age the lock to 2 min (still active) and refresh: the mtime is renewed.
	when := time.Now().Add(-2 * time.Minute)
	if err := os.Chtimes(lockPath, when, when); err != nil {
		t.Fatal(err)
	}
	if err := b.RefreshLock(lock); err != nil {
		t.Fatalf("RefreshLock: %v", err)
	}
	info, err := os.Stat(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if age := time.Since(info.ModTime()); age > time.Minute {
		t.Fatalf("lock not refreshed, mtime age %v", age)
	}

	// An expired lock is not re-acquired (Joplin 'lockExpired').
	when = time.Now().Add(-4 * time.Minute)
	if err := os.Chtimes(lockPath, when, when); err != nil {
		t.Fatal(err)
	}
	if err := b.RefreshLock(lock); !errors.Is(err, ErrLockExpired) {
		t.Fatalf("RefreshLock = %v, want ErrLockExpired", err)
	}

	if err := b.ReleaseLock(lock); err != nil {
		t.Fatalf("ReleaseLock: %v", err)
	}
}

func TestFileLock_ClientsReleaseOnlyTheirOwnLock(t *testing.T) {
	dir := t.TempDir()
	first, second := NewLocalFolderBackend(dir), NewLocalFolderBackend(dir)
	firstLock, err := first.AcquireLock()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { first.ReleaseLock(firstLock) })
	secondLock, err := second.AcquireLock()
	if err != nil {
		t.Fatal(err)
	}
	if err := second.ReleaseLock(secondLock); err != nil {
		t.Fatal(err)
	}
	if err := first.RefreshLock(firstLock); err != nil {
		t.Fatalf("another client's release invalidated our live lock: %v", err)
	}
}

type lostLockBackend struct {
	*LocalFolderBackend
	ctx context.Context
}

func (b *lostLockBackend) BeginSync(ctx context.Context, _ *store.DB) func() {
	b.ctx = ctx
	return func() {}
}

func (b *lostLockBackend) Get(string) ([]byte, error) {
	<-b.ctx.Done()
	return nil, b.ctx.Err()
}

func (b *lostLockBackend) RefreshLock(interface{}) error { return ErrLockExpired }

func TestEngineLostLockCancelsPull(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		backend := &lostLockBackend{LocalFolderBackend: NewLocalFolderBackend(dir)}
		engine := NewEngine(NewTracedBackend(backend, "test"), testDB(t), "")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		result := make(chan error, 1)
		go func() { result <- engine.Sync(ctx) }()
		time.Sleep(LockRefreshInterval + time.Second)
		select {
		case err := <-result:
			if !errors.Is(err, ErrLockExpired) {
				t.Fatalf("lost lock = %v, want ErrLockExpired", err)
			}
		default:
			cancel()
			<-result
			t.Fatal("sync continued pulling after losing its lock")
		}
		if files, err := os.ReadDir(filepath.Join(dir, "locks")); err != nil || len(files) != 0 {
			t.Fatalf("cancelled pass did not release its lock: %v", err)
		}
	})
}

func TestActiveLock_ExclusiveTieBreak(t *testing.T) {
	// Joplin activeLock(): among active EXCLUSIVE locks the oldest
	// updatedTime wins; equal timestamps tie-break by clientId.
	now := int64(1772000000000)
	ttl := LockTTL.Milliseconds()
	locks := []Lock{
		{Type: LockTypeExclusive, ClientType: 1, ClientID: "bbb", UpdatedTime: now - 1000},
		{Type: LockTypeExclusive, ClientType: 1, ClientID: "aaa", UpdatedTime: now - 1000},
		{Type: LockTypeExclusive, ClientType: 1, ClientID: "newer", UpdatedTime: now - 500},
		{Type: LockTypeSync, ClientType: 3, ClientID: "cli", UpdatedTime: now - 500},
	}
	winner := activeLock(locks, now, ttl, LockTypeExclusive, 0, "")
	if winner == nil || winner.ClientID != "aaa" {
		t.Fatalf("exclusive winner = %+v, want clientId aaa (oldest, then smallest id)", winner)
	}

	// Expired exclusive locks don't participate.
	locks[0].UpdatedTime = now - ttl - 1
	locks[1].UpdatedTime = now - ttl - 1
	winner = activeLock(locks, now, ttl, LockTypeExclusive, 0, "")
	if winner == nil || winner.ClientID != "newer" {
		t.Fatalf("exclusive winner = %+v, want clientId newer", winner)
	}
}

func TestEngineRefusesSyncWhileExclusiveLockHeld(t *testing.T) {
	remote := t.TempDir()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	const (
		folderID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa1"
		noteID   = "0d28d893565c4b2989301f2259164273"
		base     = int64(1772000000000)
	)
	if err := db.CreateFolder(&models.Folder{
		ID: folderID, Title: "Locked",
		CreatedTime: base, UpdatedTime: base,
		UserCreatedTime: base, UserUpdatedTime: base,
	}); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if err := db.CreateNote(&models.Note{
		ID: noteID, ParentID: folderID, Title: "must not leak",
		Body: "pending local change", MarkupLanguage: 1,
		CreatedTime: base, UpdatedTime: base,
		UserCreatedTime: base, UserUpdatedTime: base,
	}); err != nil {
		t.Fatalf("CreateNote: %v", err)
	}

	eng := NewEngine(NewLocalFolderBackend(remote), db, "")
	ctx := context.Background()

	// Joplin desktop holds an EXCLUSIVE lock: the sync must be refused
	// before anything touches the target.
	writeTargetLock(t, remote, "2_1_desktop.json", 0)
	err = eng.Sync(ctx)
	if !errors.Is(err, ErrExclusiveLock) {
		t.Fatalf("Sync = %v, want refusal with ErrExclusiveLock", err)
	}
	if _, statErr := os.Stat(filepath.Join(remote, noteID+".md")); !os.IsNotExist(statErr) {
		t.Fatalf("note was pushed while the target was locked (stat err=%v)", statErr)
	}

	// Lock released (expired): the same sync goes through.
	writeTargetLock(t, remote, "2_1_desktop.json", 4*time.Minute)
	if err := eng.Sync(ctx); err != nil {
		t.Fatalf("Sync after lock expiry: %v", err)
	}
	if _, err := os.Stat(filepath.Join(remote, noteID+".md")); err != nil {
		t.Fatalf("note not pushed after lock release: %v", err)
	}
}
