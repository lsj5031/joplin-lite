package sync

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestParallelEachRunsEveryItemOnce(t *testing.T) {
	const n = 50
	var seen [n]int32
	var inFlight, peak int32

	err := parallelEach(n, 4, func(i int) error {
		cur := atomic.AddInt32(&inFlight, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if cur <= p || atomic.CompareAndSwapInt32(&peak, p, cur) {
				break
			}
		}
		time.Sleep(time.Millisecond)
		atomic.AddInt32(&inFlight, -1)
		atomic.AddInt32(&seen[i], 1)
		return nil
	})
	if err != nil {
		t.Fatalf("parallelEach: %v", err)
	}
	for i := range seen {
		if got := atomic.LoadInt32(&seen[i]); got != 1 {
			t.Fatalf("item %d ran %d times, want 1", i, got)
		}
	}
	if peak > 4 {
		t.Fatalf("peak concurrency = %d, want <= 4", peak)
	}
	if peak < 2 {
		t.Fatalf("peak concurrency = %d; work was not parallel", peak)
	}
}

func TestParallelEachReportsFirstErrorAndFinishes(t *testing.T) {
	var done int32
	want := errors.New("boom")
	err := parallelEach(10, 3, func(i int) error {
		atomic.AddInt32(&done, 1)
		if i == 0 {
			return want
		}
		return nil
	})
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
	if done != 10 {
		t.Fatalf("ran %d items, want 10 (one failure must not cancel the wave)", done)
	}
}

func TestParallelEachEmpty(t *testing.T) {
	if err := parallelEach(0, 4, func(int) error { return errors.New("should not run") }); err != nil {
		t.Fatalf("parallelEach(0) = %v", err)
	}
}
