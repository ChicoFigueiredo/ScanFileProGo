package indexer

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"scanfile/pkg/scanner"
)

// Confidence levels reported by the indexes. Only ConfidenceHash means the
// comparison was made over the full content hash of every file involved.
const (
	// ConfidenceHash means every file compared had a full content hash.
	ConfidenceHash = "hash"
	// ConfidenceSizeMTime means at least one file fell back to size + modification time.
	ConfidenceSizeMTime = "size_mtime"
)

// DuplicateGroup represents a set of identical files sharing the same content hash and size.
type DuplicateGroup struct {
	ID          string              `json:"id"` // Composite key: hash:size
	Hash        string              `json:"hash"`
	FileSize    int64               `json:"fileSize"`
	FileCount   int                 `json:"fileCount"`
	WastedBytes int64               `json:"wastedBytes"` // (FileCount - 1) * FileSize
	Confidence  string              `json:"confidence"`  // Always ConfidenceHash: unhashed files never enter this index
	Files       []*scanner.FileNode `json:"files"`
}

// clone returns a detached copy so callers can serialize a group while
// Monitoramento keeps mutating the live index.
func (grp *DuplicateGroup) clone() *DuplicateGroup {
	if grp == nil {
		return nil
	}
	cp := *grp
	cp.Files = make([]*scanner.FileNode, len(grp.Files))
	copy(cp.Files, grp.Files)
	return &cp
}

// DuplicateIndex stores and queries grouped duplicate files.
//
// groups only ever holds sets of two or more files. A file whose composite key
// is still unique is parked in singles, so a later UpsertFile promotes the pair
// in O(1) instead of forcing a full rebuild (achado M3). pathKeys maps a
// normalized path to its composite key, which makes removal O(1) too (achado H4).
type DuplicateIndex struct {
	mu       sync.RWMutex
	groups   map[string]*DuplicateGroup // Key: hash + "|" + size
	singles  map[string]*scanner.FileNode
	pathKeys map[string]string // normalized path -> composite key
}

// NewDuplicateIndex creates a new index instance.
func NewDuplicateIndex() *DuplicateIndex {
	return &DuplicateIndex{
		groups:   make(map[string]*DuplicateGroup),
		singles:  make(map[string]*scanner.FileNode),
		pathKeys: make(map[string]string),
	}
}

// normalizePath folds a Windows path so lookups ignore case and separator noise.
func normalizePath(path string) string {
	return strings.ToLower(filepath.Clean(path))
}

// groupKey is the composite key that isolates same-hash files of different sizes.
func groupKey(hash string, size int64) string {
	return fmt.Sprintf("%s|%d", hash, size)
}

// RebuildIndex scans all FileNodes and builds duplicate groups.
func (idx *DuplicateIndex) RebuildIndex(files []*scanner.FileNode) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	idx.groups = make(map[string]*DuplicateGroup)
	idx.singles = make(map[string]*scanner.FileNode)
	idx.pathKeys = make(map[string]string, len(files))

	// Step 1: Bucket files by composite key (hash + size)
	for _, f := range files {
		if f == nil || f.Hash() == "" {
			continue
		}

		// Collision safety: composite key ensures different sized files with hypothetical same hash are isolated
		key := groupKey(f.Hash(), f.Size)
		grp, exists := idx.groups[key]
		if !exists {
			grp = &DuplicateGroup{
				ID:         key,
				Hash:       f.Hash(),
				FileSize:   f.Size,
				Confidence: ConfidenceHash,
				Files:      make([]*scanner.FileNode, 0, 2),
			}
			idx.groups[key] = grp
		}
		grp.Files = append(grp.Files, f)
		grp.FileCount = len(grp.Files)
		if grp.FileCount > 1 {
			grp.WastedBytes = int64(grp.FileCount-1) * grp.FileSize
		}
		idx.pathKeys[normalizePath(f.Path())] = key
	}

	// Step 2: Park groups with a single file. They are not duplicates today, but
	// keeping them lets Monitoramento promote them without a full rebuild.
	for key, grp := range idx.groups {
		if grp.FileCount < 2 {
			idx.singles[key] = grp.Files[0]
			delete(idx.groups, key)
			continue
		}
		// Sort files in group by ModTime ascending (oldest first)
		sort.Slice(grp.Files, func(i, j int) bool {
			return grp.Files[i].ModTime() < grp.Files[j].ModTime()
		})
	}
}

// UpsertFile inserts or refreshes one file in O(1), keeping FileCount and
// WastedBytes consistent. A file that arrives without a hash (locked, or not
// hashed yet) leaves the index instead of posing as a duplicate.
func (idx *DuplicateIndex) UpsertFile(f *scanner.FileNode) {
	if f == nil || f.Path() == "" {
		return
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	idx.removeLocked(f.Path())
	if f.Hash() == "" {
		return
	}

	norm := normalizePath(f.Path())
	key := groupKey(f.Hash(), f.Size)

	if grp, exists := idx.groups[key]; exists {
		grp.Files = append(grp.Files, f)
		grp.FileCount = len(grp.Files)
		grp.WastedBytes = int64(grp.FileCount-1) * grp.FileSize
		sortByModTime(grp.Files)
		idx.pathKeys[norm] = key
		return
	}

	if other, exists := idx.singles[key]; exists {
		delete(idx.singles, key)
		grp := &DuplicateGroup{
			ID:         key,
			Hash:       f.Hash(),
			FileSize:   f.Size,
			Confidence: ConfidenceHash,
			Files:      []*scanner.FileNode{other, f},
		}
		sortByModTime(grp.Files)
		grp.FileCount = len(grp.Files)
		grp.WastedBytes = int64(grp.FileCount-1) * grp.FileSize
		idx.groups[key] = grp
		idx.pathKeys[norm] = key
		return
	}

	idx.singles[key] = f
	idx.pathKeys[norm] = key
}

func sortByModTime(files []*scanner.FileNode) {
	sort.Slice(files, func(i, j int) bool {
		return files[i].ModTime() < files[j].ModTime()
	})
}

// removeLocked drops one normalized-or-raw path from the index.
// The caller must hold idx.mu for writing.
func (idx *DuplicateIndex) removeLocked(filePath string) bool {
	norm := normalizePath(filePath)
	key, known := idx.pathKeys[norm]
	if !known {
		return false
	}
	delete(idx.pathKeys, norm)

	if grp, exists := idx.groups[key]; exists {
		remaining := make([]*scanner.FileNode, 0, len(grp.Files))
		for _, f := range grp.Files {
			if f == nil || normalizePath(f.Path()) == norm {
				continue
			}
			remaining = append(remaining, f)
		}
		grp.Files = remaining
		grp.FileCount = len(remaining)
		if grp.FileCount < 2 {
			delete(idx.groups, key)
			if grp.FileCount == 1 {
				// The survivor stays known so a future twin re-forms the group.
				idx.singles[key] = grp.Files[0]
			}
			return true
		}
		grp.WastedBytes = int64(grp.FileCount-1) * grp.FileSize
		return true
	}

	if single, exists := idx.singles[key]; exists && single != nil && normalizePath(single.Path()) == norm {
		delete(idx.singles, key)
	}
	return true
}

// GetSummaryStats returns total duplicate counts and wasted space.
func (idx *DuplicateIndex) GetSummaryStats() (groupCount, fileCount int, wastedBytes int64) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	for _, grp := range idx.groups {
		groupCount++
		fileCount += grp.FileCount
		wastedBytes += grp.WastedBytes
	}
	return
}

// QueryFilter defines filtering and sorting options for duplicate querying.
type QueryFilter struct {
	SortBy    string `json:"sortBy"`    // "size_desc", "wasted_desc", "count_desc", "name_asc"
	MinSize   int64  `json:"minSize"`   // Minimum file size in bytes
	Extension string `json:"extension"` // e.g. ".zip"
	Search    string `json:"search"`    // Search text in path/name
	Limit     int    `json:"limit"`     // Pagination limit (0 = all)
	Offset    int    `json:"offset"`    // Pagination offset
}

// QueryResult returns matching groups and pagination metadata.
type QueryResult struct {
	TotalGroups int               `json:"totalGroups"`
	TotalFiles  int               `json:"totalFiles"`
	WastedBytes int64             `json:"wastedBytes"`
	Groups      []*DuplicateGroup `json:"groups"`
	Offset      int               `json:"offset"`
	Limit       int               `json:"limit"`
}

// Query returns filtered and sorted duplicate groups. The returned groups are
// detached copies, safe to serialize while Monitoramento updates the index.
func (idx *DuplicateIndex) Query(filter QueryFilter) QueryResult {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	var resultList []*DuplicateGroup
	var totalMatchedFiles int
	var totalMatchedWasted int64

	searchLower := strings.ToLower(filter.Search)
	extLower := strings.ToLower(filter.Extension)

	for _, grp := range idx.groups {
		// Size filter
		if filter.MinSize > 0 && grp.FileSize < filter.MinSize {
			continue
		}

		// Filter files in group if search or extension filter is present
		matches := false
		if searchLower == "" && extLower == "" {
			matches = true
		} else {
			for _, f := range grp.Files {
				extMatch := extLower == "" || strings.EqualFold(f.Extension(), extLower)
				searchMatch := searchLower == "" || strings.Contains(strings.ToLower(f.Path()), searchLower)
				if extMatch && searchMatch {
					matches = true
					break
				}
			}
		}

		if matches {
			resultList = append(resultList, grp)
			totalMatchedFiles += grp.FileCount
			totalMatchedWasted += grp.WastedBytes
		}
	}

	// Sorting
	switch filter.SortBy {
	case "wasted_desc":
		sort.Slice(resultList, func(i, j int) bool {
			return resultList[i].WastedBytes > resultList[j].WastedBytes
		})
	case "count_desc":
		sort.Slice(resultList, func(i, j int) bool {
			return resultList[i].FileCount > resultList[j].FileCount
		})
	case "name_asc":
		sort.Slice(resultList, func(i, j int) bool {
			if len(resultList[i].Files) > 0 && len(resultList[j].Files) > 0 {
				return resultList[i].Files[0].Name() < resultList[j].Files[0].Name()
			}
			return false
		})
	case "size_desc":
		fallthrough
	default:
		// Default: Sorted by file size descending (Largest duplicate files first)
		sort.Slice(resultList, func(i, j int) bool {
			return resultList[i].FileSize > resultList[j].FileSize
		})
	}

	totalGroups := len(resultList)
	start := filter.Offset
	if start > totalGroups {
		start = totalGroups
	}

	end := totalGroups
	if filter.Limit > 0 && start+filter.Limit < end {
		end = start + filter.Limit
	}

	paginated := make([]*DuplicateGroup, 0, end-start)
	for _, grp := range resultList[start:end] {
		paginated = append(paginated, grp.clone())
	}

	return QueryResult{
		TotalGroups: totalGroups,
		TotalFiles:  totalMatchedFiles,
		WastedBytes: totalMatchedWasted,
		Groups:      paginated,
		Offset:      start,
		Limit:       filter.Limit,
	}
}

// RemoveFileFromIndex removes a deleted file from the index in O(1) through the
// path map, instead of sweeping every group (achado H4).
func (idx *DuplicateIndex) RemoveFileFromIndex(filePath string) {
	if filePath == "" {
		return
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.removeLocked(filePath)
}

// RemoveDirFromIndex removes every indexed file under dirPath and reports how
// many were dropped. Used when Monitoramento sees a whole folder disappear.
func (idx *DuplicateIndex) RemoveDirFromIndex(dirPath string) int {
	if dirPath == "" {
		return 0
	}
	prefix := normalizePath(dirPath)
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	victims := make([]string, 0, 16)
	for norm := range idx.pathKeys {
		if strings.HasPrefix(norm, prefix) {
			victims = append(victims, norm)
		}
	}
	for _, victim := range victims {
		idx.removeLocked(victim)
	}
	return len(victims)
}
