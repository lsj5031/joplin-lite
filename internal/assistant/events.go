package assistant

import "sync"

// One dispatcher preserves publication order without making a UI caller wait
// for a callback that is itself waiting for the UI thread.
type eventQueue struct {
	mu      sync.Mutex
	wake    *sync.Cond
	pending []Event
	closed  bool
}

func newEventQueue(emit func(Event)) *eventQueue {
	q := &eventQueue{}
	q.wake = sync.NewCond(&q.mu)
	if emit == nil {
		q.closed = true
		return q
	}
	go func() {
		for {
			q.mu.Lock()
			for len(q.pending) == 0 && !q.closed {
				q.wake.Wait()
			}
			if q.closed {
				q.mu.Unlock()
				return
			}
			event := q.pending[0]
			q.pending[0] = Event{}
			q.pending = q.pending[1:]
			q.mu.Unlock()
			emit(event)
		}
	}()
	return q
}

func (q *eventQueue) enqueue(event Event) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.closed {
		q.pending = append(q.pending, event)
		q.wake.Signal()
	}
}

// Shutdown never waits for an in-flight synchronous UI callback. It drops
// queued events; the dispatcher exits when that callback returns.
func (q *eventQueue) close() {
	q.mu.Lock()
	q.closed = true
	q.pending = nil
	q.wake.Signal()
	q.mu.Unlock()
}
