package scanner

import (
	"sync"
	"time"
)

// FileNode represents an individual file in the tree.
type FileNode struct {
	Path       string `json:"path"`
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	ModTime    int64  `json:"modTime"`    // Unix timestamp in seconds (Modificação)
	CreateTime int64  `json:"createTime"` // Unix timestamp in seconds (Criação)
	AccessTime int64  `json:"accessTime"` // Unix timestamp in seconds (Último Acesso)
	Hash       string `json:"hash,omitempty"`
	QuickHash  uint64 `json:"quickHash,omitempty"`
	Extension  string `json:"extension"`
}

// DirNode represents a folder in the hierarchy, aggregating its children's stats.
type DirNode struct {
	Path        string              `json:"path"`
	Name        string              `json:"name"`
	TotalSize   int64               `json:"totalSize"`
	FileCount   int64               `json:"fileCount"`
	SubDirCount int64               `json:"subDirCount"`
	ModTime     int64               `json:"modTime"`
	CreateTime  int64               `json:"createTime"`
	AccessTime  int64               `json:"accessTime"`
	Children    map[string]*DirNode `json:"children,omitempty"`
	Files       []*FileNode         `json:"files,omitempty"`
	mu          sync.RWMutex
}

// DirSummary is a lightweight view of a directory for UI tree browsing.
type DirSummary struct {
	Path        string        `json:"path"`
	Name        string        `json:"name"`
	TotalSize   int64         `json:"totalSize"`
	FileCount   int64         `json:"fileCount"`
	SubDirCount int64         `json:"subDirCount"`
	ModTime     int64         `json:"modTime"`
	CreateTime  int64         `json:"createTime"`
	AccessTime  int64         `json:"accessTime"`
	SubDirs     []*DirSummary `json:"subDirs,omitempty"`
	Files       []*FileNode   `json:"files,omitempty"`
}

// ScanConfig holds parameters for initiating a scan.
type ScanConfig struct {
	Roots          []string `json:"roots"`          // List of drive roots or paths, e.g. ["C:\\", "D:\\"]
	WorkerThreads  int      `json:"workerThreads"`  // Number of parallel scanner threads
	HashAlgorithm  string   `json:"hashAlgorithm"`  // "xxhash" or "sha256"
	HashAllFiles   bool     `json:"hashAllFiles"`   // If false, smartly hashes only duplicate candidates (same size)
	MinSizeForHash int64    `json:"minSizeForHash"` // Ignore files smaller than this byte threshold (default 1)
	FollowSymlinks bool     `json:"followSymlinks"`
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

// ScanStatus represents the active progress state of the scanner and hasher.
type ScanStatus struct {
	Phase                string          `json:"phase"` // "idle", "phase1_metadata", "phase2_hashing", "completed", "watching"
	TotalFilesScanned    int64           `json:"totalFilesScanned"`
	TotalDirsScanned     int64           `json:"totalDirsScanned"`
	TotalBytesScanned    int64           `json:"totalBytesScanned"`
	FilesToHashCount     int64           `json:"filesToHashCount"`
	FilesHashedCount     int64           `json:"filesHashedCount"`
	BytesHashed          int64           `json:"bytesHashed"`
	DuplicateGroupsCount       int             `json:"duplicateGroupsCount"`
	DuplicateFilesCount        int             `json:"duplicateFilesCount"`
	DuplicateWastedBytes       int64           `json:"duplicateWastedBytes"`
	DuplicateFolderGroupsCount int             `json:"duplicateFolderGroupsCount"`
	DuplicateFoldersCount      int             `json:"duplicateFoldersCount"`
	DuplicateFolderWastedBytes int64           `json:"duplicateFolderWastedBytes"`
	CurrentPath                string          `json:"currentPath"`
	ScanRateFilesPerSec  float64         `json:"scanRateFilesPerSec"`
	HashRateMBPerSec     float64         `json:"hashRateMBPerSec"`
	ProgressPercent      float64         `json:"progressPercent"`
	StartTime            int64           `json:"startTime"`
	ElapsedTimeSec       float64         `json:"elapsedTimeSec"`
	IsWatching           bool            `json:"isWatching"`
	ActiveWorkers        []ActiveWorker  `json:"activeWorkers,omitempty"`
	RecentFiles          []FileLogEntry  `json:"recentFiles,omitempty"`
	ErrorsCount          int             `json:"errorsCount"`
}

// FSEventLog represents a real-time OS file change event.
type FSEventLog struct {
	Timestamp time.Time `json:"timestamp"`
	Op        string    `json:"op"` // "CREATE", "WRITE", "REMOVE", "RENAME"
	Path      string    `json:"path"`
	SizeDelta int64     `json:"sizeDelta"`
}
