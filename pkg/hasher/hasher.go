package hasher

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"scanfile/pkg/scanner"
)

// ErrHashingInProgress é devolvido quando RunHashing é chamado enquanto outra
// Fase 2 ainda está em execução no mesmo Hasher.
var ErrHashingInProgress = errors.New("Fase 2 já está em andamento")

// Estágios reportados em HashProgress.Stage.
const (
	HashStagePrehash = "prehash"
	HashStageHash    = "hash"
	HashStageDone    = "done"
)

// Hasher manages Phase 2: Multithreaded file content hashing with large file progress and error resilience.
type Hasher struct {
	cancelMu   sync.Mutex
	cancelFunc context.CancelFunc
	isRunning  atomic.Bool

	hashedFiles       atomic.Int64
	hashedBytes       atomic.Int64
	bytesRead         atomic.Int64
	prehashFiles      atomic.Int64
	prehashEliminated atomic.Int64
	errorsCount       atomic.Int64
	totalToHash       atomic.Int64
	stage             atomic.Value // string

	activeWorkers sync.Map // map[int]*scanner.ActiveWorker
	recentFiles   []scanner.FileLogEntry
	recentMu      sync.RWMutex
}

// NewHasher initializes a new Hasher.
func NewHasher() *Hasher {
	h := &Hasher{
		recentFiles: make([]scanner.FileLogEntry, 0, 100),
	}
	h.stage.Store(HashStageHash)
	return h
}

// Cancel cancela a Fase 2 em andamento. É seguro chamar de várias goroutines,
// antes de qualquer execução e mais de uma vez.
func (h *Hasher) Cancel() {
	h.cancelMu.Lock()
	fn := h.cancelFunc
	h.cancelMu.Unlock()
	if fn != nil {
		fn()
	}
}

func (h *Hasher) setCancel(fn context.CancelFunc) {
	h.cancelMu.Lock()
	h.cancelFunc = fn
	h.cancelMu.Unlock()
}

// IsRunning informa se há uma Fase 2 em execução.
func (h *Hasher) IsRunning() bool { return h.isRunning.Load() }

// HashedCount devolve quantos arquivos já receberam Hash Completo (ou tiveram o
// hash reaproveitado) na execução corrente.
func (h *Hasher) HashedCount() int64 { return h.hashedFiles.Load() }

// BytesHashed devolve os bytes cobertos por Hash Completo.
func (h *Hasher) BytesHashed() int64 { return h.hashedBytes.Load() }

// BytesRead devolve todos os bytes lidos do disco, incluindo os do Pré-hash.
func (h *Hasher) BytesRead() int64 { return h.bytesRead.Load() }

// PrehashCount devolve quantos Candidatos a Duplicado passaram pelo Pré-hash.
func (h *Hasher) PrehashCount() int64 { return h.prehashFiles.Load() }

// PrehashEliminated devolve quantos candidatos o Pré-hash descartou sem leitura
// completa do arquivo.
func (h *Hasher) PrehashEliminated() int64 { return h.prehashEliminated.Load() }

// ErrorsCount devolve o número de arquivos que falharam na leitura.
func (h *Hasher) ErrorsCount() int64 { return h.errorsCount.Load() }

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

// HashProgress é o retrato detalhado da Fase 2 entregue a OnDetailedProgress.
type HashProgress struct {
	Stage             string                 `json:"stage"`
	HashedCount       int64                  `json:"hashedCount"`
	TotalCount        int64                  `json:"totalCount"`
	BytesHashed       int64                  `json:"bytesHashed"`
	BytesRead         int64                  `json:"bytesRead"`
	PrehashCount      int64                  `json:"prehashCount"`
	PrehashEliminated int64                  `json:"prehashEliminated"`
	CurrentPath       string                 `json:"currentPath"`
	RateMBps          float64                `json:"rateMBps"`
	ActiveWorkers     []scanner.ActiveWorker `json:"activeWorkers,omitempty"`
	RecentFiles       []scanner.FileLogEntry `json:"recentFiles,omitempty"`
}

// ComputeHashOptions holds options for hashing files.
type ComputeHashOptions struct {
	Algorithm      string // "xxhash", "blake3", "md5" ou "sha256"
	HashAllFiles   bool   // If true, hashes every file; if false, only candidates with identical sizes
	MinSize        int64  // Min file size to hash (e.g. 1 byte)
	WorkerThreads  int
	DisablePrehash bool                     // Desliga o Pré-hash (diagnóstico e testes)
	DiskLogger     *scanner.DiskErrorLogger // Real-time disk error logging

	// OnProgress mantém a assinatura histórica consumida por pkg/server.
	OnProgress func(hashedCount, totalCount, bytesHashed int64, currentPath string, rateMBps float64, activeWorkers []scanner.ActiveWorker, recentFiles []scanner.FileLogEntry)

	// OnDetailedProgress recebe o retrato completo, com Pré-hash e bytes lidos.
	OnDetailedProgress func(HashProgress)
}

// FileCandidate wraps FileNode during hashing stages.
type FileCandidate struct {
	Node *scanner.FileNode
}

// RunHashing executa a Fase 2 sobre os arquivos coletados na árvore.
//
// Pipeline (Q4/Q37):
//  1. Candidatos a Duplicado: arquivos que compartilham o tamanho exato.
//  2. Pré-hash: xxHash64 das duas pontas dos candidatos com mais de 8192 bytes.
//  3. Hash Completo: só para quem colide em (tamanho, Pré-hash) em grupo >= 2.
//
// Devolve ctx.Err() quando é cancelado.
func (h *Hasher) RunHashing(ctx context.Context, allFiles []*scanner.FileNode, opts ComputeHashOptions) error {
	if !h.isRunning.CompareAndSwap(false, true) {
		return ErrHashingInProgress
	}
	defer h.isRunning.Store(false)

	ctx, cancel := context.WithCancel(ctx)
	h.setCancel(cancel)
	defer func() {
		cancel()
		h.setCancel(nil)
	}()

	h.hashedFiles.Store(0)
	h.hashedBytes.Store(0)
	h.bytesRead.Store(0)
	h.prehashFiles.Store(0)
	h.prehashEliminated.Store(0)
	h.errorsCount.Store(0)
	h.totalToHash.Store(0)
	h.stage.Store(HashStageHash)

	h.recentMu.Lock()
	h.recentFiles = make([]scanner.FileLogEntry, 0, 100)
	h.recentMu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	algo := scanner.NormalizeHashAlgorithm(opts.Algorithm)
	workers := scanner.ResolveWorkers(opts.WorkerThreads, scanner.PhaseHashing)
	minSize := opts.MinSize
	if minSize <= 0 {
		minSize = 1
	}

	startTime := time.Now()
	stopProgress := h.startProgressTicker(ctx, opts, startTime)
	defer stopProgress()

	direct, prehashCandidates := h.selectCandidates(allFiles, algo, minSize, opts)
	h.totalToHash.Store(h.hashedFiles.Load() + int64(len(direct)+len(prehashCandidates)))

	// Estágio Pré-hash
	if len(prehashCandidates) > 0 {
		h.stage.Store(HashStagePrehash)
		if err := h.runPrehashStage(ctx, prehashCandidates, workers, opts.DiskLogger); err != nil {
			return err
		}
		survivors := h.regroupAfterPrehash(prehashCandidates)
		direct = append(direct, survivors...)
		h.totalToHash.Store(h.hashedFiles.Load() + int64(len(direct)))
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	// Estágio Hash Completo
	h.stage.Store(HashStageHash)
	if len(direct) > 0 {
		if err := h.runHashStage(ctx, direct, algo, workers, opts.DiskLogger); err != nil {
			return err
		}
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	h.stage.Store(HashStageDone)
	stopProgress()
	h.emitProgress(opts, "Cálculo de hashes concluído!", startTime, nil, h.GetRecentLogs())
	return nil
}

// selectCandidates separa os arquivos que vão direto ao Hash Completo dos que
// passam pelo Pré-hash, contabilizando de saída os hashes reaproveitados.
func (h *Hasher) selectCandidates(allFiles []*scanner.FileNode, algo string, minSize int64, opts ComputeHashOptions) (direct, prehash []*scanner.FileNode) {
	reuse := func(f *scanner.FileNode) bool {
		// Quick Scan: o hash gravado só vale se foi calculado com o algoritmo
		// atual (achado M14).
		if scanner.HashMatchesAlgorithm(f.Hash, algo) {
			h.hashedFiles.Add(1)
			h.hashedBytes.Add(f.Size)
			return true
		}
		// Hash de outro algoritmo: descarta para não contaminar os grupos.
		f.Hash = ""
		return false
	}

	if opts.HashAllFiles {
		for _, f := range allFiles {
			if f == nil || f.Size < minSize {
				continue
			}
			if reuse(f) {
				continue
			}
			direct = append(direct, f)
		}
		return direct, nil
	}

	sizeMap := make(map[int64][]*scanner.FileNode)
	for _, f := range allFiles {
		if f == nil || f.Size < minSize {
			continue
		}
		sizeMap[f.Size] = append(sizeMap[f.Size], f)
	}

	for size, group := range sizeMap {
		if len(group) < 2 {
			// Sem par de tamanho: não é Candidato a Duplicado. Um hash antigo de
			// outro algoritmo é descartado para não enganar o índice.
			for _, f := range group {
				if f.Hash != "" && !scanner.HashMatchesAlgorithm(f.Hash, algo) {
					f.Hash = ""
				}
			}
			continue
		}

		var pending []*scanner.FileNode
		anyReused := false
		for _, f := range group {
			if reuse(f) {
				anyReused = true
				continue
			}
			pending = append(pending, f)
		}
		if len(pending) == 0 {
			continue
		}

		// Se algum arquivo do grupo já tem Hash Completo válido, os demais
		// precisam do Hash Completo para poder ser comparados com ele: o
		// Pré-hash não decidiria nada e ainda custaria uma leitura extra.
		if anyReused || opts.DisablePrehash || size <= PrehashMinSize {
			direct = append(direct, pending...)
			continue
		}
		prehash = append(prehash, pending...)
	}
	return direct, prehash
}

// runPrehashStage calcula o Pré-hash dos candidatos em paralelo.
func (h *Hasher) runPrehashStage(ctx context.Context, files []*scanner.FileNode, workers int, diskLogger *scanner.DiskErrorLogger) error {
	taskChan := make(chan *scanner.FileNode, 256)
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		workerID := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case f, ok := <-taskChan:
					if !ok {
						return
					}
					h.activeWorkers.Store(workerID, &scanner.ActiveWorker{
						WorkerID:  workerID,
						Path:      f.Path,
						TotalSize: f.Size,
						Phase:     "prehash",
					})
					q, err := ComputeQuickHash(f.Path, f.Size)
					h.activeWorkers.Delete(workerID)
					if err != nil {
						h.errorsCount.Add(1)
						if diskLogger != nil {
							diskLogger.Log("phase2_prehash", f.Path, fmt.Sprintf("Bloqueado ou sem permissão: %v", err))
						}
						h.logFile(scanner.FileLogEntry{
							Timestamp: time.Now(),
							Path:      f.Path,
							Size:      f.Size,
							Status:    "LOCKED",
							Message:   fmt.Sprintf("Pré-hash falhou: %v", err),
						})
						continue
					}
					f.QuickHash = q
					h.bytesRead.Add(prehashBytesRead(f.Size))
					h.prehashFiles.Add(1)
				}
			}
		}()
	}

	go func() {
		defer close(taskChan)
		for _, f := range files {
			select {
			case <-ctx.Done():
				return
			case taskChan <- f:
			}
		}
	}()

	wg.Wait()
	for i := 0; i < workers; i++ {
		h.activeWorkers.Delete(i)
	}
	return ctx.Err()
}

// regroupAfterPrehash devolve só os arquivos que continuam colidindo em
// (tamanho, Pré-hash) com pelo menos um outro arquivo.
func (h *Hasher) regroupAfterPrehash(files []*scanner.FileNode) []*scanner.FileNode {
	type key struct {
		size  int64
		quick uint64
	}
	groups := make(map[key][]*scanner.FileNode, len(files))
	for _, f := range files {
		if f.QuickHash == 0 {
			continue // Pré-hash falhou: já contabilizado como erro
		}
		k := key{size: f.Size, quick: f.QuickHash}
		groups[k] = append(groups[k], f)
	}

	survivors := make([]*scanner.FileNode, 0, len(files))
	for _, g := range groups {
		if len(g) < 2 {
			h.prehashEliminated.Add(int64(len(g)))
			continue
		}
		survivors = append(survivors, g...)
	}
	return survivors
}

// runHashStage calcula o Hash Completo dos arquivos restantes.
func (h *Hasher) runHashStage(ctx context.Context, files []*scanner.FileNode, algo string, workers int, diskLogger *scanner.DiskErrorLogger) error {
	taskChan := make(chan *FileCandidate, 1000)
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		workerID := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			bufPtr := getBuffer()
			defer putBuffer(bufPtr)
			for {
				select {
				case <-ctx.Done():
					return
				case candidate, ok := <-taskChan:
					if !ok {
						return
					}
					h.hashSingleFileWithProgress(ctx, workerID, candidate, algo, diskLogger, *bufPtr)
				}
			}
		}()
	}

	go func() {
		defer close(taskChan)
		for _, f := range files {
			select {
			case <-ctx.Done():
				return
			case taskChan <- &FileCandidate{Node: f}:
			}
		}
	}()

	wg.Wait()
	for i := 0; i < workers; i++ {
		h.activeWorkers.Delete(i)
	}
	return ctx.Err()
}

// startProgressTicker liga o relógio de progresso e devolve a função que o
// desliga. A função pode ser chamada mais de uma vez.
func (h *Hasher) startProgressTicker(ctx context.Context, opts ComputeHashOptions, startTime time.Time) func() {
	if opts.OnProgress == nil && opts.OnDetailedProgress == nil {
		return func() {}
	}

	done := make(chan struct{})
	var stopOnce sync.Once
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				activeList := h.GetActiveWorkers()
				var currentPath string
				if len(activeList) > 0 {
					currentPath = fmt.Sprintf("[%d threads ativas] %s", len(activeList), activeList[0].Path)
				}
				h.emitProgress(opts, currentPath, startTime, activeList, h.GetRecentLogs())
			}
		}
	}()

	return func() {
		stopOnce.Do(func() { close(done) })
		wg.Wait()
	}
}

func (h *Hasher) emitProgress(opts ComputeHashOptions, currentPath string, startTime time.Time, activeList []scanner.ActiveWorker, recentList []scanner.FileLogEntry) {
	elapsed := time.Since(startTime).Seconds()
	var rateMBps float64
	if elapsed > 0 {
		rateMBps = (float64(h.bytesRead.Load()) / (1024 * 1024)) / elapsed
	}
	total := h.totalToHash.Load()
	hashed := h.hashedFiles.Load()

	if opts.OnProgress != nil {
		opts.OnProgress(hashed, total, h.hashedBytes.Load(), currentPath, rateMBps, activeList, recentList)
	}
	if opts.OnDetailedProgress != nil {
		stage, _ := h.stage.Load().(string)
		opts.OnDetailedProgress(HashProgress{
			Stage:             stage,
			HashedCount:       hashed,
			TotalCount:        total,
			BytesHashed:       h.hashedBytes.Load(),
			BytesRead:         h.bytesRead.Load(),
			PrehashCount:      h.prehashFiles.Load(),
			PrehashEliminated: h.prehashEliminated.Load(),
			CurrentPath:       currentPath,
			RateMBps:          rateMBps,
			ActiveWorkers:     activeList,
			RecentFiles:       recentList,
		})
	}
}

func (h *Hasher) hashSingleFileWithProgress(ctx context.Context, workerID int, candidate *FileCandidate, algo string, diskLogger *scanner.DiskErrorLogger, buf []byte) {
	fileStart := time.Now()
	// O retrato do worker é publicado por substituição, nunca por mutação: quem
	// lê o mapa recebe um ponteiro imutável e não corre com esta goroutine
	// (achado H7).
	publish := func(bytesDone int64) {
		var percent float64
		if candidate.Node.Size > 0 {
			percent = (float64(bytesDone) / float64(candidate.Node.Size)) * 100.0
		}
		h.activeWorkers.Store(workerID, &scanner.ActiveWorker{
			WorkerID:  workerID,
			Path:      candidate.Node.Path,
			TotalSize: candidate.Node.Size,
			BytesDone: bytesDone,
			Percent:   percent,
			Phase:     "hashing",
		})
	}
	publish(0)
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

	digest := NewDigest(algo)
	var bytesRead int64

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		n, readErr := file.Read(buf)
		if n > 0 {
			_, _ = digest.Write(buf[:n])
			bytesRead += int64(n)
			h.hashedBytes.Add(int64(n))
			h.bytesRead.Add(int64(n))
			publish(bytesRead)
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			h.errorsCount.Add(1)
			if diskLogger != nil {
				diskLogger.Log("phase2_hash_read", candidate.Node.Path, fmt.Sprintf("Erro de leitura I/O: %v", readErr))
			}
			h.logFile(scanner.FileLogEntry{
				Timestamp: time.Now(),
				Path:      candidate.Node.Path,
				Size:      candidate.Node.Size,
				Status:    "ERROR",
				Message:   fmt.Sprintf("Erro de leitura I/O: %v", readErr),
			})
			return
		}
	}

	hashResult := FormatDigest(algo, digest)
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

// ComputeSingleFileHash calcula o Hash Completo de um arquivo sob demanda.
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

	bufPtr := getBuffer()
	defer putBuffer(bufPtr)

	digest := NewDigest(algo)
	if _, err := io.CopyBuffer(digest, file, *bufPtr); err != nil {
		return "", 0, err
	}
	return FormatDigest(algo, digest), info.Size(), nil
}
