package sync

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// blockingBackend holds the engine inside a pass (Get blocks) so a test can
// probe what happens when another trigger arrives while the pass is running.
type blockingBackend struct {
	entered chan struct{}
	release chan struct{}

	mu               sync.Mutex
	lockAcquisitions int
}

func (b *blockingBackend) Authenticate() error                { return nil }
func (b *blockingBackend) IsAuthenticated() bool              { return true }
func (b *blockingBackend) ReleaseLock(lock interface{}) error { return nil }
func (b *blockingBackend) Put(path string, content []byte) error {
	return nil
}
func (b *blockingBackend) Delete(path string) error { return nil }
func (b *blockingBackend) SyncTarget() int          { return 9 }

func (b *blockingBackend) AcquireLock() (interface{}, error) {
	b.mu.Lock()
	b.lockAcquisitions++
	b.mu.Unlock()
	return nil, nil
}

func (b *blockingBackend) Get(path string) ([]byte, error) {
	select {
	case b.entered <- struct{}{}:
	default:
	}
	<-b.release
	return nil, fmt.Errorf("not found: %s", path)
}

func (b *blockingBackend) locks() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lockAcquisitions
}

// TestEngineOverlappingTriggerIsIgnored pins SYNC-19: a trigger that arrives
// while a pass is running is refused (try-lock), never queued into a second
// overlapping pass over the same target and database.
func TestEngineOverlappingTriggerIsIgnored(t *testing.T) {
	db := testDB(t)
	backend := &blockingBackend{entered: make(chan struct{}, 1), release: make(chan struct{})}
	engine := NewEngine(backend, db, "")

	firstDone := make(chan error, 1)
	go func() { firstDone <- engine.Sync(context.Background()) }()

	select {
	case <-backend.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the first sync never reached the backend")
	}

	start := time.Now()
	if err := engine.Sync(context.Background()); err != nil {
		t.Fatalf("overlapping Sync = %v, want nil", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("overlapping Sync blocked for %v; it should return immediately", elapsed)
	}

	close(backend.release)
	// The first pass may succeed or report an empty backend; either way it ran
	// exactly once and released the try-lock.
	<-firstDone
	if got := backend.locks(); got != 1 {
		t.Fatalf("lock acquisitions = %d, want 1 (the overlapping trigger must not acquire)", got)
	}
}

// TestEngineTriggerChannelCoalesces pins the other half of SYNC-19: the
// buffered trigger channel keeps at most one pending signal, so rapid UI
// mutations cannot queue a burst of passes.
func TestEngineTriggerChannelCoalesces(t *testing.T) {
	engine := NewEngine(&mockBackend{}, testDB(t), "")

	for i := 0; i < 5; i++ {
		engine.TriggerSync()
	}

	select {
	case <-engine.TriggerCh():
	default:
		t.Fatal("no trigger was queued")
	}
	select {
	case <-engine.TriggerCh():
		t.Fatal("a second trigger was queued; want the buffered channel to coalesce")
	default:
	}
}
