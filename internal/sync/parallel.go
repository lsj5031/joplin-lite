package sync

import "sync"

// Bounded concurrency for the sync I/O paths. Joplin's targets are latency
// bound (one HTTPS round-trip per item), so a handful of workers turns a
// first sync from N sequential calls into N/workers waves. The limits stay
// small: six in-flight request bodies is enough to saturate a home uplink, and
// resource blobs get fewer workers because each one is read fully into memory.
const (
	pushWorkers         = 6
	resourcePushWorkers = 3
	deltaWorkers        = 6
)

// parallelEach runs fn for indexes [0,n) with a bounded worker pool. It waits
// for every item before returning, so callers never leave work in flight, and
// reports the first error while still letting the remaining items finish.
func parallelEach(n, workers int, fn func(i int) error) error {
	if n <= 0 {
		return nil
	}
	if workers <= 0 {
		workers = 1
	}
	if workers > n {
		workers = n
	}

	jobs := make(chan int)
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if err := fn(i); err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
				}
			}
		}()
	}
	for i := 0; i < n; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return firstErr
}
