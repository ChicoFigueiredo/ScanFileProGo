package scanner

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDirWorkQueue_Basic(t *testing.T) {
	q := NewDirWorkQueue()
	q.Push("dir1", "dir2", "dir3")

	if q.Len() != 3 {
		t.Fatalf("expected len 3, got %d", q.Len())
	}

	item, ok := q.Pop()
	if !ok || item != "dir1" {
		t.Fatalf("expected dir1, got %s (ok=%v)", item, ok)
	}

	q.Done()

	item, ok = q.Pop()
	if !ok || item != "dir2" {
		t.Fatalf("expected dir2, got %s", item)
	}
	q.Done()

	item, ok = q.Pop()
	if !ok || item != "dir3" {
		t.Fatalf("expected dir3, got %s", item)
	}
	q.Done()

	// All items consumed and all workers done -> should close and return false
	item, ok = q.Pop()
	if ok || item != "" {
		t.Fatalf("expected pop false, got item=%s ok=%v", item, ok)
	}
}

func TestDirWorkQueue_ConcurrentMassive(t *testing.T) {
	q := NewDirWorkQueue()
	const totalDirs = 200000 // 200,000 items - well above the old 100,000 buffer limit
	const numWorkers = 16

	// Push initial batch
	for i := 0; i < 100; i++ {
		q.Push(fmt.Sprintf("root_%d", i))
	}

	var processedCount atomic.Int64
	var pushedCount atomic.Int64
	pushedCount.Store(100)

	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				dir, ok := q.Pop()
				if !ok {
					return
				}

				processedCount.Add(1)

				// Dynamically discover more subdirs up to totalDirs
				currPushed := pushedCount.Load()
				if currPushed < totalDirs {
					toPush := 5
					if currPushed+int64(toPush) > totalDirs {
						toPush = int(totalDirs - currPushed)
					}
					if toPush > 0 && pushedCount.CompareAndSwap(currPushed, currPushed+int64(toPush)) {
						var batch []string
						for k := 0; k < toPush; k++ {
							batch = append(batch, fmt.Sprintf("%s/sub_%d", dir, k))
						}
						q.Push(batch...)
					}
				}

				q.Done()
			}
		}()
	}

	wg.Wait()

	if processedCount.Load() != totalDirs {
		t.Fatalf("expected processed %d, got %d", totalDirs, processedCount.Load())
	}
}

func TestDirWorkQueue_Cancel(t *testing.T) {
	q := NewDirWorkQueue()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, ok := q.Pop()
		if ok {
			t.Errorf("expected pop to return false on cancel")
		}
	}()

	time.Sleep(20 * time.Millisecond)
	q.Cancel()
	wg.Wait()
}
