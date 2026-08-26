package scanner

import (
	"sync"
)

// DirWorkQueue is a thread-safe, unbounded dynamic work queue for concurrent directory traversal.
// It avoids channel capacity deadlocks by dynamically buffering directory paths in memory.
type DirWorkQueue struct {
	mu            sync.Mutex
	cond          *sync.Cond
	items         []string
	activeWorkers int
	closed        bool
	cancelled     bool
}

// NewDirWorkQueue creates a new unbounded work queue, optionally initialized with root paths.
func NewDirWorkQueue(initial ...string) *DirWorkQueue {
	q := &DirWorkQueue{
		items: make([]string, 0, 1024),
	}
	q.cond = sync.NewCond(&q.mu)
	if len(initial) > 0 {
		q.items = append(q.items, initial...)
	}
	return q
}

// Push adds one or more directory paths into the queue and notifies waiting workers.
func (q *DirWorkQueue) Push(paths ...string) {
	if len(paths) == 0 {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed || q.cancelled {
		return
	}

	q.items = append(q.items, paths...)
	if len(paths) == 1 {
		q.cond.Signal()
	} else {
		q.cond.Broadcast()
	}
}

// Pop retrieves the next directory path from the queue.
// It blocks if the queue is empty while other workers are still actively processing.
// Returns (path, true) when an item is retrieved.
// Returns ("", false) when all workers have finished and no items remain, or if cancelled.
func (q *DirWorkQueue) Pop() (string, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for len(q.items) == 0 && !q.closed && !q.cancelled {
		q.cond.Wait()
	}

	if (q.closed || q.cancelled) && len(q.items) == 0 {
		return "", false
	}

	item := q.items[0]
	q.items = q.items[1:]
	q.activeWorkers++
	return item, true
}

// Done signals that an active worker has finished scanning its popped directory.
// When activeWorkers reaches 0 AND the queue is empty, the entire traversal is complete.
func (q *DirWorkQueue) Done() {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.activeWorkers > 0 {
		q.activeWorkers--
	}
	if q.activeWorkers == 0 && len(q.items) == 0 {
		q.closed = true
		q.cond.Broadcast()
	}
}

// Cancel immediately terminates the queue, unblocking all waiting workers.
func (q *DirWorkQueue) Cancel() {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.cancelled = true
	q.closed = true
	q.items = nil
	q.cond.Broadcast()
}

// Len returns the current number of pending items in the queue.
func (q *DirWorkQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// ActiveWorkers returns the count of currently busy worker threads.
func (q *DirWorkQueue) ActiveWorkers() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.activeWorkers
}
