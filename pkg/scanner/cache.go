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

// CacheSnapshot represents a persistent snapshot of the scan tree and file hashes.
type CacheSnapshot struct {
	Version      int              `json:"version"`
	Timestamp    time.Time        `json:"timestamp"`
	Roots        []string         `json:"roots"`
	TotalFiles   int64            `json:"totalFiles"`
	TotalDirs    int64            `json:"totalDirs"`
	TotalBytes   int64            `json:"totalBytes"`
	Files        []*FileNode      `json:"files"`
	Directories  []string         `json:"directories,omitempty"` // Preserves empty folders
	ScanSettings ScanConfig       `json:"scanSettings,omitempty"`
}

// CacheFileInfo represents metadata for a saved cache file on disk.
type CacheFileInfo struct {
	FileName  string    `json:"fileName"`
	FilePath  string    `json:"filePath"`
	SizeBytes int64     `json:"sizeBytes"`
	ModTime   time.Time `json:"modTime"`
}

// ExportCache serializes the TreeManager and all file metadata/hashes to a gzip-compressed writer.
func ExportCache(tm *TreeManager, roots []string, config ScanConfig, w io.Writer) error {
	allFiles := tm.GetAllFiles()
	var allDirs []string

	var totalBytes int64
	for _, f := range allFiles {
		totalBytes += f.Size
	}

	// Collect all directory paths
	tm.RootsLock(func(rootsMap map[string]*DirNode) {
		for _, r := range rootsMap {
			collectDirPaths(r, &allDirs)
		}
	})

	snapshot := CacheSnapshot{
		Version:      1,
		Timestamp:    time.Now(),
		Roots:        roots,
		TotalFiles:   int64(len(allFiles)),
		TotalDirs:    int64(len(allDirs)),
		TotalBytes:   totalBytes,
		Files:        allFiles,
		Directories:  allDirs,
		ScanSettings: config,
	}

	gzWriter := gzip.NewWriter(w)
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

// ImportCache reads and reconstructs a TreeManager from a gzip-compressed or plain JSON reader.
func ImportCache(r io.Reader) (*TreeManager, *CacheSnapshot, error) {
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

	tm := NewTreeManager()

	// Initialize roots
	for _, rootPath := range snapshot.Roots {
		tm.GetOrCreateRoot(rootPath)
	}

	// Recreate directory nodes
	for _, dirPath := range snapshot.Directories {
		tm.EnsureDirNode(dirPath)
	}

	// Insert files directly into their respective directory nodes
	dirMap := make(map[string][]*FileNode)
	for _, f := range snapshot.Files {
		dir := filepath.Dir(f.Path)
		dirMap[dir] = append(dirMap[dir], f)
	}

	for dirPath, files := range dirMap {
		node := tm.EnsureDirNode(dirPath)
		node.mu.Lock()
		node.Files = files
		node.mu.Unlock()
	}

	// Recompute all aggregated sizes and file counts bottom-up
	tm.ComputeAggregatedSizes()

	return tm, &snapshot, nil
}

// LoadCacheFromFile loads a saved cache snapshot from a file path.
func LoadCacheFromFile(filePath string) (*TreeManager, *CacheSnapshot, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("falha ao abrir arquivo de cache: %w", err)
	}
	defer file.Close()

	return ImportCache(file)
}

// ListSavedCaches discovers all saved cache files in the given directory.
func ListSavedCaches(dirPath string) ([]CacheFileInfo, error) {
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
		if strings.HasSuffix(name, ".scanfile.gz") || strings.HasSuffix(name, ".scanfile") || strings.HasSuffix(name, ".json.gz") || strings.HasSuffix(name, ".json") {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			fullPath := filepath.Join(dirPath, name)
			list = append(list, CacheFileInfo{
				FileName:  name,
				FilePath:  fullPath,
				SizeBytes: info.Size(),
				ModTime:   info.ModTime(),
			})
		}
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].ModTime.After(list[j].ModTime)
	})

	return list, nil
}
