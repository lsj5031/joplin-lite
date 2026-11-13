package assistant

import (
	"context"
	"testing"
	"time"
)

func TestStorageFailureDoesNotAnnounceCompleted(t *testing.T) {
	db := openTestVault(t)
	events := make(chan Event, 8)
	m, err := NewManager(db, t.TempDir(), func(e Event) { events <- e })
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(context.Background())
	run := admittedTestRun(m)
	if err := m.asst.db.Close(); err != nil {
		t.Fatal(err)
	}
	m.completeRun(run, "An answer still available to copy.")
	select {
	case event := <-events:
		if event.Type != EventRunFailed || event.Code != "storage_failed" {
			t.Fatalf("storage failure announced as %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("no storage failure event")
	}
	snapshot := m.Snapshot("")
	if snapshot.Running || len(snapshot.Messages) != 1 || snapshot.Messages[0].Text != "An answer still available to copy." {
		t.Fatalf("answer lost after storage failure: %+v", snapshot)
	}
}

func TestExpiredIdleTimerCannotDisposeNewerWork(t *testing.T) {
	for _, active := range []bool{false, true} {
		t.Run(map[bool]string{false: "superseded", true: "active"}[active], func(t *testing.T) {
			db := openTestVault(t)
			m, err := NewManager(db, t.TempDir(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close(context.Background())
			old := time.NewTimer(time.Hour)
			current := time.NewTimer(time.Hour)
			defer old.Stop()
			defer current.Stop()
			rt := &agentRuntime{}
			m.runtime = rt
			m.idle = current
			if active {
				m.idle = old
				m.active = &Run{}
			}
			m.expireIdleRuntime(old)
			if m.runtime != rt {
				t.Fatal("stale idle callback detached newer work")
			}
			m.runtime, m.active = nil, nil
		})
	}
}
