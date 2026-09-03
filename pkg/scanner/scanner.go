package scanner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ErrScanInProgress é devolvido por StartScan quando já existe uma Varredura em
// curso no mesmo Scanner. O servidor traduz isso para HTTP 409.
var ErrScanInProgress = errors.New("varredura já está em andamento")

// Scanner handles Phase 1: High-speed multithreaded directory traversal and metadata discovery.
type Scanner struct {
	Tree       *TreeManager
	Status     ScanStatus
	statusMu   sync.RWMutex
	DiskLogger *DiskErrorLogger
	loggerMu   sync.Mutex

	cancelMu   sync.Mutex
	cancelFunc context.CancelFunc
	isRunning  atomic.Bool

	scannedFiles          atomic.Int64
	scannedDirs           atomic.Int64
	scannedBytes          atomic.Int64
	scannedAllocatedBytes atomic.Int64
	compressedFiles       atomic.Int64
	errorsCount           atomic.Int64
	skippedCount          atomic.Int64
	reusedFiles           atomic.Int64
	reusedBytes           atomic.Int64
	modifiedFiles         atomic.Int64
	newFiles              atomic.Int64

	quickScanLookup map[string]*FileNode
	quickLookupMu   sync.RWMutex

	// wslRoots guarda as Raízes Varridas detectadas como WSL, em minúsculas.
	// A detecção acontece uma vez por raiz, no início da Varredura.
	wslRoots   []string
	wslRootsMu sync.RWMutex

	visitedDirs   sync.Map // map[string]bool: só alvos de reparse points já visitados
	activeWorkers sync.Map // map[int]*ActiveWorker
	recentFiles   []FileLogEntry
	recentMu      sync.RWMutex
	errorLogs     []ErrorLogEntry
	errorMu       sync.RWMutex
	skipped       []SkippedEntry
	skippedMu     sync.RWMutex
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
		skipped:     make([]SkippedEntry, 0, 64),
	}
}

// SetQuickScanLookup configures previous snapshot files to enable fast O(1) hash reuse in QuickScanMode.
func (s *Scanner) SetQuickScanLookup(lookup map[string]*FileNode) {
	s.quickLookupMu.Lock()
	defer s.quickLookupMu.Unlock()
	s.quickScanLookup = lookup
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
	st.SkippedCount = s.skippedCount.Load()
	st.ReusedFilesCount = s.reusedFiles.Load()
	st.ReusedBytesCount = s.reusedBytes.Load()
	st.ModifiedFilesCount = s.modifiedFiles.Load()
	st.NewFilesCount = s.newFiles.Load()

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

// Cancel interrompe a Varredura em curso. É seguro chamar de várias goroutines,
// mais de uma vez e sem nenhuma Varredura ativa.
func (s *Scanner) Cancel() {
	s.cancelMu.Lock()
	fn := s.cancelFunc
	s.cancelMu.Unlock()
	if fn != nil {
		fn()
	}
}

func (s *Scanner) setCancel(fn context.CancelFunc) {
	s.cancelMu.Lock()
	s.cancelFunc = fn
	s.cancelMu.Unlock()
}

// IsRunning informa se há uma Varredura em curso.
func (s *Scanner) IsRunning() bool { return s.isRunning.Load() }

// SkippedCount devolve o total de Itens Pulados na Varredura corrente.
func (s *Scanner) SkippedCount() int64 { return s.skippedCount.Load() }

// GetSkipped devolve os últimos Itens Pulados (anel de maxSkippedRing entradas).
func (s *Scanner) GetSkipped() []SkippedEntry {
	s.skippedMu.RLock()
	defer s.skippedMu.RUnlock()
	out := make([]SkippedEntry, len(s.skipped))
	copy(out, s.skipped)
	return out
}

// GetErrorLogs devolve os últimos erros registrados em memória.
func (s *Scanner) GetErrorLogs() []ErrorLogEntry {
	s.errorMu.RLock()
	defer s.errorMu.RUnlock()
	out := make([]ErrorLogEntry, len(s.errorLogs))
	copy(out, s.errorLogs)
	return out
}

// LoggerPath devolve o caminho do log de erros da Varredura corrente.
func (s *Scanner) LoggerPath() string {
	s.loggerMu.Lock()
	defer s.loggerMu.Unlock()
	return s.DiskLogger.GetFilePath()
}

// CloseLogger encerra o log de erros em disco. É idempotente.
func (s *Scanner) CloseLogger() {
	s.loggerMu.Lock()
	logger := s.DiskLogger
	s.loggerMu.Unlock()
	_ = logger.Close()
}

func (s *Scanner) logFile(entry FileLogEntry) {
	s.recentMu.Lock()
	s.recentFiles = append(s.recentFiles, entry)
	if len(s.recentFiles) > 100 {
		s.recentFiles = s.recentFiles[len(s.recentFiles)-100:]
	}
	s.recentMu.Unlock()
}

// logSkipped registra um Item Pulado: contador, anel em memória e linha no log
// em disco com a fase SKIPPED. Nenhuma decisão de pular pode ser silenciosa.
func (s *Scanner) logSkipped(path string, reason string) {
	s.skippedCount.Add(1)

	s.skippedMu.Lock()
	s.skipped = append(s.skipped, SkippedEntry{
		Timestamp: time.Now(),
		Path:      path,
		Reason:    reason,
	})
	if len(s.skipped) > maxSkippedRing {
		s.skipped = s.skipped[len(s.skipped)-maxSkippedRing:]
	}
	s.skippedMu.Unlock()

	s.loggerMu.Lock()
	logger := s.DiskLogger
	s.loggerMu.Unlock()
	logger.Log("SKIPPED", path, reason)
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
	s.loggerMu.Lock()
	logger := s.DiskLogger
	s.loggerMu.Unlock()
	logger.Log(phase, path, err.Error())

	s.logFile(FileLogEntry{
		Timestamp: time.Now(),
		Path:      path,
		Status:    "ERROR",
		Message:   err.Error(),
	})
}

// detectWSLRoots resolve, uma única vez por Raiz Varrida, se ela é do WSL.
func (s *Scanner) detectWSLRoots(roots []string) {
	var detected []string
	for _, root := range roots {
		if isWSLRoot(root, VolumeFileSystemName(root)) {
			detected = append(detected, strings.ToLower(strings.TrimRight(filepath.Clean(root), `\/`)))
		}
	}
	s.wslRootsMu.Lock()
	s.wslRoots = detected
	s.wslRootsMu.Unlock()
}

// isUnderWSLRoot informa se o caminho está dentro de alguma Raiz Varrida do WSL.
func (s *Scanner) isUnderWSLRoot(path string) bool {
	s.wslRootsMu.RLock()
	roots := s.wslRoots
	s.wslRootsMu.RUnlock()
	if len(roots) == 0 {
		return false
	}
	for _, r := range roots {
		if pathHasPrefix(path, r) {
			return true
		}
	}
	return false
}

// StartScan executes Phase 1: Metadata discovery across selected drive roots.
// Devolve ErrScanInProgress se já houver uma Varredura ativa e ctx.Err() se for
// cancelada.
func (s *Scanner) StartScan(ctx context.Context, config ScanConfig, onProgress func(ScanStatus)) error {
	if !s.isRunning.CompareAndSwap(false, true) {
		return ErrScanInProgress
	}
	defer s.isRunning.Store(false)

	ctx, cancel := context.WithCancel(ctx)
	s.setCancel(cancel)
	defer func() {
		cancel()
		s.setCancel(nil)
	}()

	// Fecha o log da Varredura anterior antes de abrir o novo (achado M5).
	s.loggerMu.Lock()
	if s.DiskLogger != nil {
		_ = s.DiskLogger.Close()
		s.DiskLogger = nil
	}
	if logger, err := NewDiskErrorLogger(config.LogDir); err == nil {
		s.DiskLogger = logger
	}
	s.loggerMu.Unlock()

	s.scannedFiles.Store(0)
	s.scannedDirs.Store(0)
	s.scannedBytes.Store(0)
	s.scannedAllocatedBytes.Store(0)
	s.compressedFiles.Store(0)
	s.errorsCount.Store(0)
	s.skippedCount.Store(0)
	s.reusedFiles.Store(0)
	s.reusedBytes.Store(0)
	s.modifiedFiles.Store(0)
	s.newFiles.Store(0)

	s.recentMu.Lock()
	s.recentFiles = make([]FileLogEntry, 0, 100)
	s.recentMu.Unlock()

	s.errorMu.Lock()
	s.errorLogs = make([]ErrorLogEntry, 0, 100)
	s.errorMu.Unlock()

	s.skippedMu.Lock()
	s.skipped = make([]SkippedEntry, 0, 64)
	s.skippedMu.Unlock()

	s.detectWSLRoots(config.Roots)

	// visitedDirs só guarda alvos canônicos de reparse points; pastas comuns não
	// entram, o que evita uma string por pasta e uma chamada de EvalSymlinks por
	// pasta (achado M2).
	s.visitedDirs = sync.Map{}
	for _, root := range config.Roots {
		s.visitedDirs.Store(strings.ToLower(strings.TrimRight(filepath.Clean(root), `\/`)), true)
	}

	workers := ResolveWorkers(config.WorkerThreads, PhaseMetadata)
	hashWorkers := ResolveWorkers(config.WorkerThreads, PhaseHashing)

	startTime := time.Now().Unix()
	s.SetStatus(func(st *ScanStatus) {
		st.Phase = "phase1_metadata"
		st.StartTime = startTime
		st.ProgressPercent = 0
		st.IsQuickScan = config.QuickScanMode
		st.SkippedCount = 0
		st.Phase1Workers = workers
		st.Phase2Workers = hashWorkers
		st.CurrentPath = "Iniciando varredura paralela multithread..."
	})

	// Primeiro retrato do estado, antes de qualquer trabalho: a interface passa a
	// ver a fase "phase1_metadata" imediatamente, sem esperar o primeiro tique.
	if onProgress != nil {
		onProgress(s.GetStatus())
	}

	if err := ctx.Err(); err != nil {
		return s.finishCancelled(err)
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
	queueWatchDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			queue.Cancel()
		case <-queueWatchDone:
		}
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
	progressDone := make(chan struct{})
	var progressWG sync.WaitGroup
	progressWG.Add(1)
	go func() {
		defer progressWG.Done()
		progressTicker := time.NewTicker(250 * time.Millisecond)
		defer progressTicker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-progressDone:
				return
			case <-progressTicker.C:
				if onProgress != nil {
					onProgress(s.GetStatus())
				}
			}
		}
	}()

	wg.Wait()
	close(queueWatchDone)
	close(progressDone)
	progressWG.Wait()

	// Clean up any remaining active workers in map
	for i := 0; i < workers; i++ {
		s.activeWorkers.Delete(i)
	}

	if err := ctx.Err(); err != nil {
		return s.finishCancelled(err)
	}

	// Compute full in-memory tree size aggregation
	s.Tree.ComputeAggregatedSizes()

	s.SetStatus(func(st *ScanStatus) {
		st.CurrentPath = "Fase 1 (Mapeamento de Metadados) concluída."
		st.TotalFilesScanned = s.scannedFiles.Load()
		st.TotalDirsScanned = s.scannedDirs.Load()
		st.TotalBytesScanned = s.scannedBytes.Load()
	})

	s.loggerMu.Lock()
	logger := s.DiskLogger
	s.loggerMu.Unlock()
	logger.Flush()

	if onProgress != nil {
		onProgress(s.GetStatus())
	}

	return nil
}

// finishCancelled encerra a Varredura interrompida: registra o estado para a
// interface, descarrega o log em disco e propaga o erro do contexto.
func (s *Scanner) finishCancelled(err error) error {
	s.SetStatus(func(st *ScanStatus) {
		st.CurrentPath = "Varredura cancelada."
	})
	s.loggerMu.Lock()
	logger := s.DiskLogger
	s.loggerMu.Unlock()
	logger.Flush()
	return err
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

	// Laços de junção acabam estourando a profundidade máxima. O pulo é
	// registrado como qualquer outro Item Pulado.
	if exceedsMaxDepth(dirPath) {
		s.logSkipped(dirPath, ReasonMaxDepth)
		return
	}

	entries, err := os.ReadDir(dirPath)
	if err != nil {
		s.logError(dirPath, err, "phase1_readdir")
		return
	}

	s.scannedDirs.Add(1)

	underWSL := s.isUnderWSLRoot(dirPath)
	algo := NormalizeHashAlgorithm(config.HashAlgorithm)

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
		fullPath := filepath.Join(dirPath, name)

		if skip, reason := classifyIgnoredName(name, dirPath, underWSL); skip {
			s.logSkipped(fullPath, reason)
			continue
		}

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
					// Padrão: não descer em pastas ligadas, para não contar duas
					// vezes nem entrar em laço.
					subDirNames = append(subDirNames, name)
					s.logSkipped(fullPath, ReasonVisitedTarget+" (link não seguido): -> "+target)
					continue
				}

				// EvalSymlinks só aqui: é a única situação em que o caminho
				// canônico importa (achado M2).
				canonicalTarget, err := filepath.EvalSymlinks(fullPath)
				if err != nil {
					subDirNames = append(subDirNames, name)
					s.logSkipped(fullPath, ReasonVisitedTarget+" (alvo ilegível)")
					continue
				}
				lowerTarget := strings.ToLower(strings.TrimRight(canonicalTarget, `\/`))
				if _, alreadyVisited := s.visitedDirs.LoadOrStore(lowerTarget, true); alreadyVisited {
					subDirNames = append(subDirNames, name)
					s.logSkipped(fullPath, ReasonVisitedTarget+": -> "+target)
					continue
				}

				// Check ancestor loop
				if isAncestorPath(canonicalTarget, fullPath) {
					subDirNames = append(subDirNames, name)
					s.logSkipped(fullPath, ReasonAncestorLoop+": -> "+target)
					continue
				}
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

			// Pseudo-arquivos (sockets, devices, FIFOs) têm tamanho artificial e
			// não representam espaço em disco.
			if info.Mode()&os.ModeType != 0 && info.Mode()&os.ModeIrregular != 0 && !isSymlinkOrJunction {
				s.logSkipped(fullPath, ReasonIrregularFile)
				continue
			}

			size := info.Size()
			allocatedSize, isCompressed := GetAllocatedFileSize(fullPath, info)
			modTime, createTime, accessTime := ExtractFileTimestamps(info)
			ext := strings.ToLower(filepath.Ext(name))

			fileNode := NewFileNode(FileMeta{
				Name:          name,
				Size:          size,
				AllocatedSize: allocatedSize,
				IsCompressed:  isCompressed,
				ModTime:       modTime,
				CreateTime:    createTime,
				AccessTime:    accessTime,
				Extension:     ext,
			})

			// Quick Scan Hash & Metadata Reuse
			if config.QuickScanMode {
				s.quickLookupMu.RLock()
				lookup := s.quickScanLookup
				s.quickLookupMu.RUnlock()

				if len(lookup) > 0 {
					normPath := strings.ToLower(filepath.Clean(fullPath))
					if cached, exists := lookup[normPath]; exists && cached != nil {
						// Só reaproveita hash calculado com o algoritmo atual
						// (achado M14).
						if cached.Size == size && cached.ModTime() == modTime && HashMatchesAlgorithm(cached.Hash(), algo) {
							fileNode.SetHash(cached.Hash())
							fileNode.SetQuickHash(cached.QuickHash())
							fileNode.SetReusedFromCache(true)
							s.reusedFiles.Add(1)
							s.reusedBytes.Add(size)
						} else {
							s.modifiedFiles.Add(1)
						}
					} else {
						s.newFiles.Add(1)
					}
				}
			}

			if isSymlinkOrJunction {
				target, _ := os.Readlink(fullPath)
				fileNode.SetSymlink(target)
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
