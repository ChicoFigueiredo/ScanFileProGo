package scanner

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Scanner handles Phase 1: High-speed multithreaded directory traversal and metadata discovery.
type Scanner struct {
	Tree       *TreeManager
	Status     ScanStatus
	statusMu   sync.RWMutex
	DiskLogger *DiskErrorLogger

	cancelFunc context.CancelFunc
	isRunning  atomic.Bool

	scannedFiles          atomic.Int64
	scannedDirs           atomic.Int64
	scannedBytes          atomic.Int64
	scannedAllocatedBytes atomic.Int64
	compressedFiles       atomic.Int64
	errorsCount           atomic.Int64

	visitedDirs   sync.Map // map[string]bool canonical paths to prevent duplicate traversal & circular symlink loops
	activeWorkers sync.Map // map[int]*ActiveWorker
	recentFiles   []FileLogEntry
	recentMu      sync.RWMutex
	errorLogs     []ErrorLogEntry
	errorMu       sync.RWMutex
}

// NewScanner creates a new Scanner instance.
func NewScanner(tree *TreeManager) *Scanner {
	return &Scanner{
		Tree: tree,
		Status: ScanStatus{
			Phase: "idle",
		},
		recentFiles: make([]FileLogEntry, 0, 100),
		errorLogs:   make([]ErrorLogEntry, 0, 100),
	}
}

// GetStatus returns a snapshot of the current scan progress.
func (s *Scanner) GetStatus() ScanStatus {
	s.statusMu.RLock()
	st := s.Status
	s.statusMu.RUnlock()

	st.TotalFilesScanned = s.scannedFiles.Load()
	st.TotalDirsScanned = s.scannedDirs.Load()
	st.TotalBytesScanned = s.scannedBytes.Load()
	st.TotalAllocatedBytesScanned = s.scannedAllocatedBytes.Load()
	st.CompressedFilesCount = s.compressedFiles.Load()
	if st.TotalBytesScanned > st.TotalAllocatedBytesScanned && st.TotalAllocatedBytesScanned > 0 {
		st.CompressedSpaceSavedBytes = st.TotalBytesScanned - st.TotalAllocatedBytesScanned
		st.CompressionRatio = float64(st.TotalBytesScanned) / float64(st.TotalAllocatedBytesScanned)
	}
	st.ErrorsCount = int(s.errorsCount.Load())

	// Collect active workers (if not in Phase 2 where Hasher updates ActiveWorkers directly)
	if st.Phase != "phase2_hashing" {
		var workers []ActiveWorker
		s.activeWorkers.Range(func(key, value any) bool {
			if w, ok := value.(*ActiveWorker); ok && w != nil {
				workers = append(workers, *w)
			}
			return true
		})
		st.ActiveWorkers = workers

		// Collect recent files from scanner
		s.recentMu.RLock()
		st.RecentFiles = make([]FileLogEntry, len(s.recentFiles))
		copy(st.RecentFiles, s.recentFiles)
		s.recentMu.RUnlock()
	}

	if st.StartTime > 0 {
		elapsed := time.Since(time.Unix(st.StartTime, 0)).Seconds()
		if elapsed > 0 {
			st.ElapsedTimeSec = elapsed
			st.ScanRateFilesPerSec = float64(st.TotalFilesScanned) / elapsed
		}
	}
	return st
}

// SetStatus updates partial status info.
func (s *Scanner) SetStatus(update func(st *ScanStatus)) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	update(&s.Status)
}

// Cancel stops an ongoing scan.
func (s *Scanner) Cancel() {
	if s.cancelFunc != nil {
		s.cancelFunc()
	}
}

func (s *Scanner) logFile(entry FileLogEntry) {
	s.recentMu.Lock()
	s.recentFiles = append(s.recentFiles, entry)
	if len(s.recentFiles) > 100 {
		s.recentFiles = s.recentFiles[len(s.recentFiles)-100:]
	}
	s.recentMu.Unlock()
}

func (s *Scanner) logError(path string, err error, phase string) {
	s.errorsCount.Add(1)
	s.errorMu.Lock()
	s.errorLogs = append(s.errorLogs, ErrorLogEntry{
		Timestamp: time.Now(),
		Path:      path,
		ErrorMsg:  err.Error(),
		Phase:     phase,
	})
	if len(s.errorLogs) > 200 {
		s.errorLogs = s.errorLogs[len(s.errorLogs)-200:]
	}
	s.errorMu.Unlock()

	// Write to persistent disk error log
	if s.DiskLogger != nil {
		s.DiskLogger.Log(phase, path, err.Error())
	}

	s.logFile(FileLogEntry{
		Timestamp: time.Now(),
		Path:      path,
		Status:    "ERROR",
		Message:   err.Error(),
	})
}

// StartScan executes Phase 1: Metadata discovery across selected drive roots.
func (s *Scanner) StartScan(ctx context.Context, config ScanConfig, onProgress func(ScanStatus)) error {
	if !s.isRunning.CompareAndSwap(false, true) {
		return nil
	}
	defer s.isRunning.Store(false)

	ctx, cancel := context.WithCancel(ctx)
	s.cancelFunc = cancel
	defer cancel()

	// Initialize disk error logger for this scan session
	if logger, err := NewDiskErrorLogger("logs"); err == nil {
		s.DiskLogger = logger
	}

	s.scannedFiles.Store(0)
	s.scannedDirs.Store(0)
	s.scannedBytes.Store(0)
	s.scannedAllocatedBytes.Store(0)
	s.compressedFiles.Store(0)
	s.errorsCount.Store(0)

	s.recentMu.Lock()
	s.recentFiles = make([]FileLogEntry, 0, 100)
	s.recentMu.Unlock()

	s.errorMu.Lock()
	s.errorLogs = make([]ErrorLogEntry, 0, 100)
	s.errorMu.Unlock()

	s.visitedDirs = sync.Map{}
	for _, root := range config.Roots {
		if canonical, err := filepath.EvalSymlinks(root); err == nil {
			s.visitedDirs.Store(strings.ToLower(canonical), true)
		} else {
			s.visitedDirs.Store(strings.ToLower(root), true)
		}
	}

	startTime := time.Now().Unix()
	s.SetStatus(func(st *ScanStatus) {
		st.Phase = "phase1_metadata"
		st.StartTime = startTime
		st.ProgressPercent = 0
		st.CurrentPath = "Iniciando varredura paralela multithread..."
	})

	workers := config.WorkerThreads
	if workers <= 0 {
		workers = runtime.NumCPU() * 2
	}

	// Unbounded dynamic work queue - zero deadlock risk for millions of directories
	queue := NewDirWorkQueue()

	// Push initial roots into queue BEFORE launching workers
	for _, root := range config.Roots {
		s.Tree.GetOrCreateRoot(root)
		queue.Push(root)
	}

	var wg sync.WaitGroup

	// Cancel queue on context done
	go func() {
		<-ctx.Done()
		queue.Cancel()
	}()

	// Launch worker pool
	for i := 0; i < workers; i++ {
		workerID := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				dirPath, ok := queue.Pop()
				if !ok {
					return
				}

				s.activeWorkers.Store(workerID, &ActiveWorker{
					WorkerID: workerID,
					Path:     dirPath,
					Phase:    "scanning",
				})

				s.scanDirectory(ctx, dirPath, queue, config)

				s.activeWorkers.Delete(workerID)
				queue.Done()
			}
		}()
	}

	// Periodic progress ticker and tree aggregation
	progressTicker := time.NewTicker(250 * time.Millisecond)
	defer progressTicker.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-progressTicker.C:
				if onProgress != nil {
					onProgress(s.GetStatus())
				}
			}
		}
	}()

	wg.Wait()

	// Clean up any remaining active workers in map
	for i := 0; i < workers; i++ {
		s.activeWorkers.Delete(i)
	}

	// Compute full in-memory tree size aggregation
	s.Tree.ComputeAggregatedSizes()

	s.SetStatus(func(st *ScanStatus) {
		st.CurrentPath = "Fase 1 (Mapeamento de Metadados) concluída."
		st.TotalFilesScanned = s.scannedFiles.Load()
		st.TotalDirsScanned = s.scannedDirs.Load()
		st.TotalBytesScanned = s.scannedBytes.Load()
	})

	if onProgress != nil {
		onProgress(s.GetStatus())
	}

	return nil
}

func (s *Scanner) scanDirectory(ctx context.Context, dirPath string, queue *DirWorkQueue, config ScanConfig) {
	select {
	case <-ctx.Done():
		return
	default:
	}

	s.statusMu.Lock()
	s.Status.CurrentPath = dirPath
	s.statusMu.Unlock()

	// Check path depth to prevent infinite loops (e.g. max 50 levels)
	if strings.Count(dirPath, string(filepath.Separator)) > 50 {
		return
	}

	entries, err := os.ReadDir(dirPath)
	if err != nil {
		s.logError(dirPath, err, "phase1_readdir")
		return
	}

	s.scannedDirs.Add(1)

	var dirFiles []*FileNode
	var subDirNames []string
	var localBytes int64
	var localAllocatedBytes int64
	var localCompressedCount int64
	var localFilesCount int64

	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return
		default:
		}

		name := entry.Name()

		// Skip Windows System & Hidden internal files, WSL virtual filesystems, and /proc/kcore
		if isIgnoredSystemName(name, dirPath) {
			continue
		}

		fullPath := filepath.Join(dirPath, name)
		entryType := entry.Type()
		isSymlinkOrJunction := entryType&os.ModeSymlink != 0 || entryType&os.ModeIrregular != 0

		var info os.FileInfo
		if isSymlinkOrJunction || entry.IsDir() {
			if inf, err := entry.Info(); err == nil {
				info = inf
				if isReparsePoint(inf) {
					isSymlinkOrJunction = true
				}
			}
		}

		if entry.IsDir() {
			if isSymlinkOrJunction {
				// Read symlink / junction target path
				target, _ := os.Readlink(fullPath)
				if target == "" {
					if eval, err := filepath.EvalSymlinks(fullPath); err == nil {
						target = eval
					}
				}
				s.Tree.SetDirSymlink(fullPath, target)

				if !config.FollowSymlinks {
					// Default: Do NOT descend into symlink folders (prevents double counting & loops)
					subDirNames = append(subDirNames, name)
					continue
				}

				// If followSymlinks is enabled: Guard against circular loops and double counting
				canonicalTarget, err := filepath.EvalSymlinks(fullPath)
				if err != nil {
					subDirNames = append(subDirNames, name)
					continue
				}
				lowerTarget := strings.ToLower(canonicalTarget)
				if _, alreadyVisited := s.visitedDirs.LoadOrStore(lowerTarget, true); alreadyVisited {
					// Target folder already scanned or in progress! Skip traversal to prevent duplication/loops
					s.logFile(FileLogEntry{
						Timestamp: time.Now(),
						Path:      fullPath,
						Status:    "SYMLINK_LOOP_SKIPPED",
						Message:   "Link para pasta já mapeada: -> " + target,
					})
					subDirNames = append(subDirNames, name)
					continue
				}

				// Check ancestor loop
				if isAncestorPath(canonicalTarget, fullPath) {
					subDirNames = append(subDirNames, name)
					continue
				}
			}

			// Circular directory trap detection (e.g. A\B\A\B or AppData\Application Data\Application Data)
			lowerName := strings.ToLower(name)
			parts := strings.Split(strings.ToLower(fullPath), string(filepath.Separator))
			seenCount := 0
			for _, part := range parts {
				if part == lowerName {
					seenCount++
				}
			}
			if seenCount >= 3 {
				continue // Skip infinite directory loop
			}

			// Store canonical dir
			if canonical, err := filepath.EvalSymlinks(fullPath); err == nil {
				s.visitedDirs.Store(strings.ToLower(canonical), true)
			}

			subDirNames = append(subDirNames, name)
			// Enqueue subfolder directly without blocking
			queue.Push(fullPath)
		} else {
			if info == nil {
				inf, err := entry.Info()
				if err != nil {
					s.logError(fullPath, err, "phase1_stat")
					continue
				}
				info = inf
			}

			// Skip Linux / WSL virtual pseudo-files with artificial astronomical sizes (e.g. /proc/kcore = 128 TB)
			if name == "kcore" || (info.Mode()&os.ModeType != 0 && info.Mode()&os.ModeIrregular != 0 && !isSymlinkOrJunction) {
				continue
			}

			size := info.Size()
			allocatedSize, isCompressed := GetAllocatedFileSize(fullPath, info)
			modTime, createTime, accessTime := ExtractFileTimestamps(info)
			ext := strings.ToLower(filepath.Ext(name))

			fileNode := &FileNode{
				Path:          fullPath,
				Name:          name,
				Size:          size,
				AllocatedSize: allocatedSize,
				IsCompressed:  isCompressed,
				ModTime:       modTime,
				CreateTime:    createTime,
				AccessTime:    accessTime,
				Extension:     ext,
			}

			if isSymlinkOrJunction {
				target, _ := os.Readlink(fullPath)
				fileNode.IsSymlink = true
				fileNode.LinkTarget = target
			}

			dirFiles = append(dirFiles, fileNode)
			localBytes += size
			localAllocatedBytes += allocatedSize
			if isCompressed {
				localCompressedCount++
			}
			localFilesCount++

			// Log sample files for transparency
			if localFilesCount%50 == 0 || size > 100*1024*1024 {
				s.logFile(FileLogEntry{
					Timestamp: time.Now(),
					Path:      fullPath,
					Size:      size,
					Status:    "OK",
				})
			}
		}
	}

	// Insert into tree using high-speed FastSetDir without ancestor lock contention
	s.Tree.FastSetDir(dirPath, dirFiles, subDirNames)

	s.scannedFiles.Add(localFilesCount)
	s.scannedBytes.Add(localBytes)
	s.scannedAllocatedBytes.Add(localAllocatedBytes)
	s.compressedFiles.Add(localCompressedCount)
}

func isAncestorPath(target, current string) bool {
	target = strings.TrimRight(strings.ToLower(filepath.Clean(target)), `\/`)
	current = strings.TrimRight(strings.ToLower(filepath.Clean(current)), `\/`)
	if target == current {
		return true
	}
	return strings.HasPrefix(current, target+string(filepath.Separator))
}

func isIgnoredSystemName(name string, dirPath string) bool {
	lower := strings.ToLower(name)
	switch lower {
	case "system volume information", "$recycle.bin", "$winreagent",
		"pagefile.sys", "hiberfil.sys", "swapfile.sys", "dumpstack.log",
		"kcore":
		return true
	case "proc", "sys", "dev":
		// Ignore Linux virtual filesystems in WSL root or root directories
		clean := strings.TrimRight(dirPath, `\/`)
		if len(clean) <= 3 || strings.Contains(strings.ToLower(dirPath), "wsl") {
			return true
		}
	case "mnt":
		// Ignore /mnt inside WSL/Linux shares to prevent circular re-scanning of Windows drives C:\, E:\, etc.
		clean := strings.TrimRight(dirPath, `\/`)
		if len(clean) <= 3 || strings.Contains(strings.ToLower(dirPath), "wsl") {
			return true
		}
	}
	return false
}
