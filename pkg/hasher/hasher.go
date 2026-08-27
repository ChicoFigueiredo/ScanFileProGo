package hasher

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cespare/xxhash/v2"
	"scanfile/pkg/scanner"
)

// Hasher manages Phase 2: Multithreaded file content hashing with large file progress and error resilience.
type Hasher struct {
	cancelFunc context.CancelFunc
	isRunning  atomic.Bool

	hashedFiles atomic.Int64
	hashedBytes atomic.Int64
	errorsCount atomic.Int64

	activeWorkers sync.Map // map[int]*scanner.ActiveWorker
	recentFiles   []scanner.FileLogEntry
	recentMu      sync.RWMutex
}

// NewHasher initializes a new Hasher.
func NewHasher() *Hasher {
	return &Hasher{
		recentFiles: make([]scanner.FileLogEntry, 0, 100),
	}
}

// Cancel cancels any running hash operation.
func (h *Hasher) Cancel() {
	if h.cancelFunc != nil {
		h.cancelFunc()
	}
}

// GetActiveWorkers returns the snapshot of currently active worker routines and what file they are reading.
func (h *Hasher) GetActiveWorkers() []scanner.ActiveWorker {
	var workers []scanner.ActiveWorker
	h.activeWorkers.Range(func(key, value any) bool {
		if w, ok := value.(*scanner.ActiveWorker); ok && w != nil {
			workers = append(workers, *w)
		}
		return true
	})
	return workers
}

// GetRecentLogs returns the latest processed file logs.
func (h *Hasher) GetRecentLogs() []scanner.FileLogEntry {
	h.recentMu.RLock()
	defer h.recentMu.RUnlock()
	logs := make([]scanner.FileLogEntry, len(h.recentFiles))
	copy(logs, h.recentFiles)
	return logs
}

func (h *Hasher) logFile(entry scanner.FileLogEntry) {
	h.recentMu.Lock()
	h.recentFiles = append(h.recentFiles, entry)
	if len(h.recentFiles) > 100 {
		h.recentFiles = h.recentFiles[len(h.recentFiles)-100:]
	}
	h.recentMu.Unlock()
}

// ComputeHashOptions holds options for hashing files.
type ComputeHashOptions struct {
	Algorithm      string                   // "xxhash" or "sha256"
	HashAllFiles   bool                     // If true, hashes every file; if false, only candidates with identical sizes
	MinSize        int64                    // Min file size to hash (e.g. 1 byte)
	WorkerThreads  int
	DiskLogger     *scanner.DiskErrorLogger // Real-time disk error logging
	OnProgress     func(hashedCount, totalCount, bytesHashed int64, currentPath string, rateMBps float64, activeWorkers []scanner.ActiveWorker, recentFiles []scanner.FileLogEntry)
}

// RunHashing executes Phase 2 hashing across the files collected in the Tree.
func (h *Hasher) RunHashing(ctx context.Context, allFiles []*scanner.FileNode, opts ComputeHashOptions) error {
	if !h.isRunning.CompareAndSwap(false, true) {
		return nil
	}
	defer h.isRunning.Store(false)

	ctx, cancel := context.WithCancel(ctx)
	h.cancelFunc = cancel
	defer cancel()

	h.hashedFiles.Store(0)
	h.hashedBytes.Store(0)
	h.errorsCount.Store(0)

	h.recentMu.Lock()
	h.recentFiles = make([]scanner.FileLogEntry, 0, 100)
	h.recentMu.Unlock()

	// Filter files to hash
	var targetFiles []*FileCandidate

	if opts.HashAllFiles {
		for _, f := range allFiles {
			if f.Size >= opts.MinSize {
				if f.Hash != "" {
					// Already hashed and reused from cache (Quick Scan)
					h.hashedFiles.Add(1)
					h.hashedBytes.Add(f.Size)
					continue
				}
				targetFiles = append(targetFiles, &FileCandidate{Node: f})
			}
		}
	} else {
		// Group files by size
		sizeMap := make(map[int64][]*scanner.FileNode)
		for _, f := range allFiles {
			if f.Size >= opts.MinSize {
				sizeMap[f.Size] = append(sizeMap[f.Size], f)
			}
		}

		// Only include files that share the exact same byte length
		for _, files := range sizeMap {
			if len(files) > 1 {
				for _, f := range files {
					if f.Hash != "" {
						// Already hashed and reused from cache (Quick Scan)
						h.hashedFiles.Add(1)
						h.hashedBytes.Add(f.Size)
						continue
					}
					targetFiles = append(targetFiles, &FileCandidate{Node: f})
				}
			}
		}
	}

	totalToHash := int64(len(targetFiles)) + h.hashedFiles.Load()
	if len(targetFiles) == 0 {
		// All files were already hashed from cache
		if opts.OnProgress != nil {
			opts.OnProgress(totalToHash, totalToHash, h.hashedBytes.Load(), "Todos os hashes foram reaproveitados do cache com sucesso!", 0, nil, nil)
		}
		return nil
	}

	workers := opts.WorkerThreads
	if workers <= 0 {
		workers = runtime.NumCPU()
	}

	startTime := time.Now()
	taskChan := make(chan *FileCandidate, 1000)
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		workerID := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 1024*1024) // 1MB buffer for high-speed sequential I/O
			for {
				select {
				case <-ctx.Done():
					return
				case candidate, ok := <-taskChan:
					if !ok {
						return
					}
					h.hashSingleFileWithProgress(ctx, workerID, candidate, opts.Algorithm, opts.DiskLogger, buf)
				}
			}
		}()
	}

	// Progress ticker
	progressTicker := time.NewTicker(200 * time.Millisecond)
	defer progressTicker.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-progressTicker.C:
				if opts.OnProgress != nil {
					elapsed := time.Since(startTime).Seconds()
					var rateMBps float64
					if elapsed > 0 {
						rateMBps = (float64(h.hashedBytes.Load()) / (1024 * 1024)) / elapsed
					}

					activeList := h.GetActiveWorkers()
					recentList := h.GetRecentLogs()
					var currentPath string
					if len(activeList) > 0 {
						currentPath = fmt.Sprintf("[%d threads ativas] %s", len(activeList), activeList[0].Path)
					}

					opts.OnProgress(h.hashedFiles.Load(), totalToHash, h.hashedBytes.Load(), currentPath, rateMBps, activeList, recentList)
				}
			}
		}
	}()

	// Feed tasks into channel
	go func() {
		defer close(taskChan)
		for _, item := range targetFiles {
			select {
			case <-ctx.Done():
				return
			case taskChan <- item:
			}
		}
	}()

	wg.Wait()

	if opts.OnProgress != nil {
		elapsed := time.Since(startTime).Seconds()
		var rateMBps float64
		if elapsed > 0 {
			rateMBps = (float64(h.hashedBytes.Load()) / (1024 * 1024)) / elapsed
		}
		opts.OnProgress(h.hashedFiles.Load(), totalToHash, h.hashedBytes.Load(), "Cálculo de hashes concluído!", rateMBps, nil, h.GetRecentLogs())
	}

	return nil
}

// FileCandidate wraps FileNode during hashing stages.
type FileCandidate struct {
	Node *scanner.FileNode
}

func (h *Hasher) hashSingleFileWithProgress(ctx context.Context, workerID int, candidate *FileCandidate, algo string, diskLogger *scanner.DiskErrorLogger, buf []byte) {
	fileStart := time.Now()
	workerInfo := &scanner.ActiveWorker{
		WorkerID:  workerID,
		Path:      candidate.Node.Path,
		TotalSize: candidate.Node.Size,
		BytesDone: 0,
		Percent:   0,
		Phase:     "hashing",
	}
	h.activeWorkers.Store(workerID, workerInfo)
	defer h.activeWorkers.Delete(workerID)

	file, err := os.Open(candidate.Node.Path)
	if err != nil {
		h.errorsCount.Add(1)
		if diskLogger != nil {
			diskLogger.Log("phase2_hash_open", candidate.Node.Path, fmt.Sprintf("Bloqueado ou sem permissão: %v", err))
		}
		h.logFile(scanner.FileLogEntry{
			Timestamp: time.Now(),
			Path:      candidate.Node.Path,
			Size:      candidate.Node.Size,
			Status:    "LOCKED",
			Message:   fmt.Sprintf("Bloqueado ou sem permissão: %v", err),
		})
		return
	}
	defer file.Close()

	var bytesRead int64
	var hashResult string

	if algo == "sha256" {
		hasher := sha256.New()
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			n, err := file.Read(buf)
			if n > 0 {
				hasher.Write(buf[:n])
				bytesRead += int64(n)
				h.hashedBytes.Add(int64(n))

				// Update active worker progress on large files
				workerInfo.BytesDone = bytesRead
				if candidate.Node.Size > 0 {
					workerInfo.Percent = (float64(bytesRead) / float64(candidate.Node.Size)) * 100.0
				}
			}
			if err != nil {
				if err == io.EOF {
					break
				}
				h.errorsCount.Add(1)
				h.logFile(scanner.FileLogEntry{
					Timestamp: time.Now(),
					Path:      candidate.Node.Path,
					Size:      candidate.Node.Size,
					Status:    "ERROR",
					Message:   fmt.Sprintf("Erro de leitura I/O: %v", err),
				})
				return
			}
		}
		hashHex := hex.EncodeToString(hasher.Sum(nil))
		hashResult = "sha256:" + hashHex
	} else {
		hasher := xxhash.New()
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			n, err := file.Read(buf)
			if n > 0 {
				hasher.Write(buf[:n])
				bytesRead += int64(n)
				h.hashedBytes.Add(int64(n))

				workerInfo.BytesDone = bytesRead
				if candidate.Node.Size > 0 {
					workerInfo.Percent = (float64(bytesRead) / float64(candidate.Node.Size)) * 100.0
				}
			}
			if err != nil {
				if err == io.EOF {
					break
				}
				h.errorsCount.Add(1)
				h.logFile(scanner.FileLogEntry{
					Timestamp: time.Now(),
					Path:      candidate.Node.Path,
					Size:      candidate.Node.Size,
					Status:    "ERROR",
					Message:   fmt.Sprintf("Erro de leitura I/O: %v", err),
				})
				return
			}
		}
		hashResult = fmt.Sprintf("xxh64:%016x", hasher.Sum64())
	}

	candidate.Node.Hash = hashResult
	h.hashedFiles.Add(1)

	durationMs := time.Since(fileStart).Milliseconds()
	h.logFile(scanner.FileLogEntry{
		Timestamp:  time.Now(),
		Path:       candidate.Node.Path,
		Size:       candidate.Node.Size,
		Hash:       hashResult,
		DurationMs: durationMs,
		Status:     "OK",
	})
}

// ComputeSingleFileHash calculates the hash of an individual file on demand.
func ComputeSingleFileHash(filePath string, algo string) (string, int64, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return "", 0, err
	}

	buf := make([]byte, 1024*1024)
	if algo == "sha256" {
		hasher := sha256.New()
		if _, err := io.CopyBuffer(hasher, file, buf); err != nil {
			return "", 0, err
		}
		return "sha256:" + hex.EncodeToString(hasher.Sum(nil)), info.Size(), nil
	}

	hasher := xxhash.New()
	if _, err := io.CopyBuffer(hasher, file, buf); err != nil {
		return "", 0, err
	}
	return fmt.Sprintf("xxh64:%016x", hasher.Sum64()), info.Size(), nil
}
