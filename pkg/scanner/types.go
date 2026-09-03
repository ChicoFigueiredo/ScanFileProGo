package scanner

import (
	"path/filepath"
	"sync"
	"time"
)

// DirNode represents a folder in the hierarchy, aggregating its children's stats.
//
// O caminho completo não é guardado: Path() o deriva subindo por parent, do
// mesmo jeito que FileNode.Path (ADR-0001). `parent` e `Name` são imutáveis
// depois da criação do nó, então Path() dispensa lock.
type DirNode struct {
	parent *DirNode
	Name   string

	TotalSize           int64 // Total logical size
	TotalAllocatedSize  int64 // Total physical allocated size
	FileCount           int64
	CompressedFileCount int64
	SubDirCount         int64

	IsSymlink  bool
	LinkTarget string

	Children map[string]*DirNode
	Files    []*FileNode
	mu       sync.RWMutex
}

// maxPathChain é o teto de níveis que Path() percorre sem alocar. A Fase 1 já
// recusa caminhos com mais de MaxScanDepth níveis (skip.go).
const maxPathChain = MaxScanDepth + 16

// Path deriva o caminho completo da pasta a partir da cadeia de pais.
func (node *DirNode) Path() string {
	return node.appendPath("")
}

// joinChild deriva o caminho completo de um filho direto desta pasta.
func (node *DirNode) joinChild(child string) string {
	return node.appendPath(child)
}

// appendPath monta o caminho da pasta, opcionalmente com um elemento a mais no
// fim. Usa filepath.Join para reproduzir exatamente o caminho que o código
// antigo montava elemento a elemento.
func (node *DirNode) appendPath(child string) string {
	if node == nil {
		return child
	}

	var buf [maxPathChain]string
	chain := buf[:0]
	for cur := node; cur != nil; cur = cur.parent {
		if len(chain) == cap(chain) {
			// Cadeia mais funda que o teto: cai para uma fatia no heap.
			return node.appendPathSlow(child)
		}
		chain = append(chain, cur.Name)
	}

	// A cadeia foi colhida da folha para a raiz; inverte no lugar.
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	if child != "" {
		chain = append(chain, child)
	}
	return filepath.Join(chain...)
}

func (node *DirNode) appendPathSlow(child string) string {
	var chain []string
	for cur := node; cur != nil; cur = cur.parent {
		chain = append(chain, cur.Name)
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	if child != "" {
		chain = append(chain, child)
	}
	return filepath.Join(chain...)
}

// joinPathElem junta uma pasta e um elemento simples, com a mesma semântica de
// filepath.Join para nomes já limpos.
func joinPathElem(dir, name string) string {
	if dir == "" {
		return name
	}
	if name == "" {
		return dir
	}
	return filepath.Join(dir, name)
}

// setChildLocked registra um filho, criando o mapa na primeira vez. Pastas
// folha — a maioria — nunca alocam o mapa (ADR-0001). O chamador segura o lock.
func (node *DirNode) setChildLocked(name string, child *DirNode) {
	if node.Children == nil {
		node.Children = make(map[string]*DirNode, 1)
	}
	node.Children[name] = child
}

// DirSummary is a lightweight view of a directory for UI tree browsing.
type DirSummary struct {
	Path                string        `json:"path"`
	Name                string        `json:"name"`
	TotalSize           int64         `json:"totalSize"`
	TotalAllocatedSize  int64         `json:"totalAllocatedSize"`
	FileCount           int64         `json:"fileCount"`
	CompressedFileCount int64         `json:"compressedFileCount"`
	SubDirCount         int64         `json:"subDirCount"`
	ModTime             int64         `json:"modTime"`
	CreateTime          int64         `json:"createTime"`
	AccessTime          int64         `json:"accessTime"`
	IsSymlink           bool          `json:"isSymlink,omitempty"`
	LinkTarget          string        `json:"linkTarget,omitempty"`
	DirectFileCount     int64         `json:"directFileCount"`
	SubDirs             []*DirSummary `json:"subDirs,omitempty"`
	Files               []*FileNode   `json:"files,omitempty"`
}

// ScanConfig holds parameters for initiating a scan.
type ScanConfig struct {
	Roots                   []string `json:"roots"`          // List of drive roots or paths, e.g. ["C:\\", "D:\\"]
	WorkerThreads           int      `json:"workerThreads"`  // Number of parallel scanner threads
	HashAlgorithm           string   `json:"hashAlgorithm"`  // "xxhash" or "sha256"
	HashAllFiles            bool     `json:"hashAllFiles"`   // If false, smartly hashes only duplicate candidates (same size)
	MinSizeForHash          int64    `json:"minSizeForHash"` // Ignore files smaller than this byte threshold (default 1)
	FollowSymlinks          bool     `json:"followSymlinks"`
	QuickScanMode           bool     `json:"quickScanMode,omitempty"`           // Incremental quick scan reusing existing hashes if path+size+modTime match
	AutoSaveIntervalMinutes int      `json:"autoSaveIntervalMinutes,omitempty"` // Periodic autosave interval (0 = disabled, default 5)
	AutoSaveOnComplete      bool     `json:"autoSaveOnComplete,omitempty"`      // Automatically save snapshot upon scan completion
	AutoSavePath            string   `json:"autoSavePath,omitempty"`            // Custom target path for autosave (optional)
	LogDir                  string   `json:"logDir,omitempty"`                  // Pasta do log de erros desta Varredura (padrão "logs")
}

// ActiveWorker represents the real-time state of an individual worker thread.
type ActiveWorker struct {
	WorkerID  int     `json:"workerId"`
	Path      string  `json:"path"`
	TotalSize int64   `json:"totalSize"`
	BytesDone int64   `json:"bytesDone"`
	Percent   float64 `json:"percent"`
	SpeedMBps float64 `json:"speedMBps"`
	Phase     string  `json:"phase"` // "scanning" or "hashing"
}

// FileLogEntry represents a logged file event with size and status.
type FileLogEntry struct {
	Timestamp  time.Time `json:"timestamp"`
	Path       string    `json:"path"`
	Size       int64     `json:"size"`
	Hash       string    `json:"hash,omitempty"`
	DurationMs int64     `json:"durationMs"`
	Status     string    `json:"status"` // "OK", "READING", "LOCKED", "ERROR", "SKIPPED"
	Message    string    `json:"message,omitempty"`
}

// ErrorLogEntry represents a skipped file/folder due to OS or permission errors.
type ErrorLogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Path      string    `json:"path"`
	ErrorMsg  string    `json:"errorMsg"`
	Phase     string    `json:"phase"`
}

// SkippedEntry registra um Item Pulado: pasta ou arquivo que a Varredura
// decidiu não percorrer, com o motivo em pt-BR.
type SkippedEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Path      string    `json:"path"`
	Reason    string    `json:"reason"`
}

// ScanStatus represents the active progress state of the scanner and hasher.
type ScanStatus struct {
	Phase                      string              `json:"phase"` // "idle", "phase1_metadata", "phase2_hashing", "completed", "watching"
	TotalFilesScanned          int64               `json:"totalFilesScanned"`
	TotalDirsScanned           int64               `json:"totalDirsScanned"`
	TotalBytesScanned          int64               `json:"totalBytesScanned"`          // Logical size
	TotalAllocatedBytesScanned int64               `json:"totalAllocatedBytesScanned"` // Physical allocated size on disk
	CompressedFilesCount       int64               `json:"compressedFilesCount"`       // Count of compressed files
	CompressedSpaceSavedBytes  int64               `json:"compressedSpaceSavedBytes"`  // Bytes saved: Logical - Allocated
	CompressionRatio           float64             `json:"compressionRatio"`           // Ratio: Logical / Allocated
	FilesToHashCount           int64               `json:"filesToHashCount"`
	FilesHashedCount           int64               `json:"filesHashedCount"`
	BytesHashed                int64               `json:"bytesHashed"`
	DuplicateGroupsCount       int                 `json:"duplicateGroupsCount"`
	DuplicateFilesCount        int                 `json:"duplicateFilesCount"`
	DuplicateWastedBytes       int64               `json:"duplicateWastedBytes"`
	DuplicateFolderGroupsCount int                 `json:"duplicateFolderGroupsCount"`
	DuplicateFoldersCount      int                 `json:"duplicateFoldersCount"`
	DuplicateFolderWastedBytes int64               `json:"duplicateFolderWastedBytes"`
	CurrentPath                string              `json:"currentPath"`
	ScanRateFilesPerSec        float64             `json:"scanRateFilesPerSec"`
	HashRateMBPerSec           float64             `json:"hashRateMBPerSec"`
	ProgressPercent            float64             `json:"progressPercent"`
	StartTime                  int64               `json:"startTime"`
	ElapsedTimeSec             float64             `json:"elapsedTimeSec"`
	IsWatching                 bool                `json:"isWatching"`
	IsQuickScan                bool                `json:"isQuickScan,omitempty"`
	ReusedFilesCount           int64               `json:"reusedFilesCount,omitempty"`
	ReusedBytesCount           int64               `json:"reusedBytesCount,omitempty"`
	ModifiedFilesCount         int64               `json:"modifiedFilesCount,omitempty"`
	NewFilesCount              int64               `json:"newFilesCount,omitempty"`
	LastAutoSaveTime           int64               `json:"lastAutoSaveTime,omitempty"`
	AutoSaveFilePath           string              `json:"autoSaveFilePath,omitempty"`
	ActiveWorkers              []ActiveWorker      `json:"activeWorkers,omitempty"`
	RecentFiles                []FileLogEntry      `json:"recentFiles,omitempty"`
	ErrorsCount                int                 `json:"errorsCount"`
	SkippedCount               int64               `json:"skippedCount"`
	PrehashCount               int64               `json:"prehashCount"`
	Phase1Workers              int                 `json:"phase1Workers,omitempty"`
	Phase2Workers              int                 `json:"phase2Workers,omitempty"`
	MemoryStats                *MemoryStatsPayload `json:"memoryStats,omitempty"`
}

// MemoryStatsPayload holds process and host OS RAM utilization metrics.
type MemoryStatsPayload struct {
	AllocMB          uint64  `json:"allocMB"`          // RAM in use by Go app (Alloc)
	TotalAllocMB     uint64  `json:"totalAllocMB"`     // Cumulative RAM allocated
	SysMB            uint64  `json:"sysMB"`            // RAM obtained from OS (Heap Sys)
	NumGC            uint32  `json:"numGC"`            // Number of GC runs completed
	Goroutines       int     `json:"goroutines"`       // Current active goroutines
	SystemTotalRAMMB uint64  `json:"systemTotalRAMMB"` // Total Physical RAM installed in host
	SystemUsedRAMMB  uint64  `json:"systemUsedRAMMB"`  // Used Physical RAM in host
	SystemFreeRAMMB  uint64  `json:"systemFreeRAMMB"`  // Available/Free Physical RAM in host
	SystemPercent    float64 `json:"systemPercent"`    // % of Host RAM utilized
	AppPercentOfSys  float64 `json:"appPercentOfSys"`  // % of Host RAM used by this process
}

// FSEventLog represents a real-time OS file change event.
type FSEventLog struct {
	Timestamp time.Time `json:"timestamp"`
	Op        string    `json:"op"` // "CREATE", "WRITE", "REMOVE", "RENAME"
	Path      string    `json:"path"`
	SizeDelta int64     `json:"sizeDelta"`
}
