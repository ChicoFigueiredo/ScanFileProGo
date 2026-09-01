package scanner

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// CurrentCacheVersion represents the active snapshot format version.
	CurrentCacheVersion = 2

	// DefaultAutoSaveFileName is the primary autosave filename.
	DefaultAutoSaveFileName = "autosave_latest.sfz"

	// BackupAutoSaveFileName is the rollback autosave filename.
	BackupAutoSaveFileName = "autosave_previous.sfz"
)

// CacheSnapshot represents a persistent, versioned snapshot of the scan tree and file hashes.
type CacheSnapshot struct {
	Version             int         `json:"version"`
	Timestamp           time.Time   `json:"timestamp"`
	Roots               []string    `json:"roots"`
	TotalFiles          int64       `json:"totalFiles"`
	TotalDirs           int64       `json:"totalDirs"`
	TotalBytes          int64       `json:"totalBytes"`
	TotalAllocatedBytes int64       `json:"totalAllocatedBytes,omitempty"`
	Files               []*FileNode `json:"files"`
	Directories         []string    `json:"directories,omitempty"` // Preserves empty folders
	ScanSettings        ScanConfig  `json:"scanSettings,omitempty"`
}

// CacheFileInfo represents metadata for a saved cache file on disk.
type CacheFileInfo struct {
	FileName  string    `json:"fileName"`
	FilePath  string    `json:"filePath"`
	SizeBytes int64     `json:"sizeBytes"`
	ModTime   time.Time `json:"modTime"`
	IsAutoSave bool     `json:"isAutoSave,omitempty"`
}

// ExportCache serializes the TreeManager and all file metadata/hashes to a gzip-compressed writer (Level: BestSpeed/Default).
func ExportCache(tm *TreeManager, roots []string, config ScanConfig, w io.Writer) error {
	allFiles := tm.GetAllFiles()
	var allDirs []string

	var totalBytes int64
	var totalAllocated int64
	for _, f := range allFiles {
		totalBytes += f.Size
		if f.AllocatedSize > 0 {
			totalAllocated += f.AllocatedSize
		} else {
			totalAllocated += f.Size
		}
	}

	// Collect all directory paths
	rootsSnapshot := tm.GetRootsSnapshot()
	for _, r := range rootsSnapshot {
		collectDirPaths(r, &allDirs)
	}

	snapshot := CacheSnapshot{
		Version:             CurrentCacheVersion,
		Timestamp:           time.Now(),
		Roots:               roots,
		TotalFiles:          int64(len(allFiles)),
		TotalDirs:           int64(len(allDirs)),
		TotalBytes:          totalBytes,
		TotalAllocatedBytes: totalAllocated,
		Files:               allFiles,
		Directories:         allDirs,
		ScanSettings:        config,
	}

	gzWriter, err := gzip.NewWriterLevel(w, gzip.DefaultCompression)
	if err != nil {
		gzWriter = gzip.NewWriter(w)
	}
	defer gzWriter.Close()

	enc := json.NewEncoder(gzWriter)
	return enc.Encode(snapshot)
}

func collectDirPaths(node *DirNode, list *[]string) {
	node.mu.RLock()
	*list = append(*list, node.Path)
	children := make([]*DirNode, 0, len(node.Children))
	for _, child := range node.Children {
		children = append(children, child)
	}
	node.mu.RUnlock()

	for _, child := range children {
		collectDirPaths(child, list)
	}
}

// SaveCacheToFile saves the in-memory tree state to a compressed file on disk.
func SaveCacheToFile(tm *TreeManager, roots []string, config ScanConfig, targetPath string) error {
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("falha ao criar diretório de cache: %w", err)
	}

	file, err := os.Create(targetPath)
	if err != nil {
		return fmt.Errorf("falha ao criar arquivo de cache: %w", err)
	}
	defer file.Close()

	if err := ExportCache(tm, roots, config, file); err != nil {
		return fmt.Errorf("falha ao exportar cache: %w", err)
	}

	return nil
}

// SaveAutoSave atomically writes an autosave snapshot and rotates the previous autosave backup.
func SaveAutoSave(tm *TreeManager, roots []string, config ScanConfig, targetDir string) (string, error) {
	if targetDir == "" {
		targetDir = "saved_scans"
	}
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return "", fmt.Errorf("falha ao criar pasta de autosave: %w", err)
	}

	tempPath := filepath.Join(targetDir, "autosave_temp.sfz")
	latestPath := filepath.Join(targetDir, DefaultAutoSaveFileName)
	backupPath := filepath.Join(targetDir, BackupAutoSaveFileName)

	// Step 1: Write to temporary file
	file, err := os.Create(tempPath)
	if err != nil {
		return "", fmt.Errorf("falha ao criar arquivo temporário de autosave: %w", err)
	}

	if err := ExportCache(tm, roots, config, file); err != nil {
		file.Close()
		_ = os.Remove(tempPath)
		return "", fmt.Errorf("falha ao serializar autosave: %w", err)
	}
	file.Close()

	// Step 2: Rotate latest to backup if latest exists
	if _, err := os.Stat(latestPath); err == nil {
		_ = os.Remove(backupPath) // Remove old backup if any
		_ = os.Rename(latestPath, backupPath)
	}

	// Step 3: Rename temp to latest (Atomic swap)
	if err := os.Rename(tempPath, latestPath); err != nil {
		// Fallback: If direct rename fails, copy and remove
		data, rErr := os.ReadFile(tempPath)
		if rErr == nil {
			_ = os.WriteFile(latestPath, data, 0644)
			_ = os.Remove(tempPath)
		} else {
			return "", fmt.Errorf("falha ao finalizar autosave atômico: %w", err)
		}
	}

	return latestPath, nil
}

// CacheProgressFunc callback reporting stage name, estimated percentage and detailed message.
type CacheProgressFunc func(stage string, percent float64, details string)

// ImportCache reads and reconstructs a TreeManager from a gzip-compressed (.sfz, .scanfile.gz) or plain JSON reader with retrocompatibility.
func ImportCache(r io.Reader) (*TreeManager, *CacheSnapshot, error) {
	return ImportCacheWithProgress(r, nil)
}

// ImportCacheWithProgress reads and reconstructs a TreeManager while reporting stage progress.
func ImportCacheWithProgress(r io.Reader, onProgress CacheProgressFunc) (*TreeManager, *CacheSnapshot, error) {
	if onProgress != nil {
		onProgress("Descompactando snapshot Gzip...", 15, "Lendo stream compactado do disco")
	}

	// Try reading as Gzip first
	var reader io.Reader
	gzReader, err := gzip.NewReader(r)
	if err == nil {
		defer gzReader.Close()
		reader = gzReader
	} else {
		// Fallback to plain reader
		reader = r
	}

	var snapshot CacheSnapshot
	dec := json.NewDecoder(reader)
	if err := dec.Decode(&snapshot); err != nil {
		return nil, nil, fmt.Errorf("formato de arquivo de cache inválido ou corrompido: %w", err)
	}

	if onProgress != nil {
		onProgress("Reconstruindo estrutura de diretórios...", 45, fmt.Sprintf("%d pastas e %d arquivos encontrados", len(snapshot.Directories), len(snapshot.Files)))
	}

	// Migrate version 1 to current version defaults
	if snapshot.Version == 0 {
		snapshot.Version = 1
	}

	tm := NewTreeManager()

	// Initialize roots
	for _, rootPath := range snapshot.Roots {
		tm.GetOrCreateRoot(rootPath)
	}

	// Recreate directory nodes
	for _, dirPath := range snapshot.Directories {
		tm.EnsureDirNode(dirPath)
	}

	if onProgress != nil {
		onProgress("Mapeando arquivos em memória...", 65, "Vinculando nós de arquivos e metadados de hash")
	}

	// Insert files directly into their respective directory nodes
	dirMap := make(map[string][]*FileNode)
	for _, f := range snapshot.Files {
		// Retrocompatibility: calculate AllocatedSize if missing
		if f.AllocatedSize == 0 && f.Size > 0 && !f.IsCompressed {
			f.AllocatedSize = f.Size
		}
		dir := filepath.Dir(f.Path)
		dirMap[dir] = append(dirMap[dir], f)
	}

	for dirPath, files := range dirMap {
		node := tm.EnsureDirNode(dirPath)
		node.mu.Lock()
		node.Files = files
		node.mu.Unlock()
	}

	if onProgress != nil {
		onProgress("Calculando métricas agregadas da árvore...", 85, "Computando tamanhos e contadores recursivos")
	}

	// Recompute all aggregated sizes and file counts bottom-up
	tm.ComputeAggregatedSizes()

	return tm, &snapshot, nil
}

// LoadCacheFromFile loads a saved cache snapshot from a file path.
func LoadCacheFromFile(filePath string) (*TreeManager, *CacheSnapshot, error) {
	return LoadCacheFromFileWithProgress(filePath, nil)
}

// LoadCacheFromFileWithProgress loads a saved cache snapshot while reporting progress.
func LoadCacheFromFileWithProgress(filePath string, onProgress CacheProgressFunc) (*TreeManager, *CacheSnapshot, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("falha ao abrir arquivo de cache: %w", err)
	}
	defer file.Close()

	return ImportCacheWithProgress(file, onProgress)
}

// BuildQuickScanLookup builds a fast index map from a CacheSnapshot to allow O(1) hash and metadata reuse during Quick Scan.
func BuildQuickScanLookup(snapshot *CacheSnapshot) map[string]*FileNode {
	if snapshot == nil || len(snapshot.Files) == 0 {
		return make(map[string]*FileNode)
	}

	lookup := make(map[string]*FileNode, len(snapshot.Files))
	for _, f := range snapshot.Files {
		if f == nil || f.Path == "" {
			continue
		}
		normPath := strings.ToLower(filepath.Clean(f.Path))
		lookup[normPath] = f
	}
	return lookup
}

// GetLatestAutoSave discovers if an active autosave is available in targetDir.
func GetLatestAutoSave(dirPath string) (*CacheFileInfo, error) {
	if dirPath == "" {
		dirPath = "saved_scans"
	}
	latestPath := filepath.Join(dirPath, DefaultAutoSaveFileName)
	info, err := os.Stat(latestPath)
	if err == nil && info.Size() > 0 {
		return &CacheFileInfo{
			FileName:   DefaultAutoSaveFileName,
			FilePath:   latestPath,
			SizeBytes:  info.Size(),
			ModTime:    info.ModTime(),
			IsAutoSave: true,
		}, nil
	}

	// Fallback to previous backup if latest is missing
	backupPath := filepath.Join(dirPath, BackupAutoSaveFileName)
	bInfo, bErr := os.Stat(backupPath)
	if bErr == nil && bInfo.Size() > 0 {
		return &CacheFileInfo{
			FileName:   BackupAutoSaveFileName,
			FilePath:   backupPath,
			SizeBytes:  bInfo.Size(),
			ModTime:    bInfo.ModTime(),
			IsAutoSave: true,
		}, nil
	}

	return nil, os.ErrNotExist
}

// ListSavedCaches discovers all saved cache and autosave files in the given directory.
func ListSavedCaches(dirPath string) ([]CacheFileInfo, error) {
	if dirPath == "" {
		dirPath = "saved_scans"
	}
	if _, err := os.Stat(dirPath); os.IsNotExist(err) {
		return []CacheFileInfo{}, nil
	}

	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, err
	}

	var list []CacheFileInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".sfz") || strings.HasSuffix(name, ".scanfile.gz") || strings.HasSuffix(name, ".scanfile") || strings.HasSuffix(name, ".json.gz") || strings.HasSuffix(name, ".json") {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			fullPath := filepath.Join(dirPath, name)
			list = append(list, CacheFileInfo{
				FileName:   name,
				FilePath:   fullPath,
				SizeBytes:  info.Size(),
				ModTime:    info.ModTime(),
				IsAutoSave: strings.HasPrefix(name, "autosave_"),
			})
		}
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].ModTime.After(list[j].ModTime)
	})

	return list, nil
}
