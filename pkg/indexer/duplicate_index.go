package indexer

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"scanfile/pkg/scanner"
)

// DuplicateGroup represents a set of identical files sharing the same content hash and size.
type DuplicateGroup struct {
	ID          string              `json:"id"` // Composite key: hash:size
	Hash        string              `json:"hash"`
	FileSize    int64               `json:"fileSize"`
	FileCount   int                 `json:"fileCount"`
	WastedBytes int64               `json:"wastedBytes"` // (FileCount - 1) * FileSize
	Files       []*scanner.FileNode `json:"files"`
}

// DuplicateIndex stores and queries grouped duplicate files.
type DuplicateIndex struct {
	mu     sync.RWMutex
	groups map[string]*DuplicateGroup // Key: hash + "|" + size
}

// NewDuplicateIndex creates a new index instance.
func NewDuplicateIndex() *DuplicateIndex {
	return &DuplicateIndex{
		groups: make(map[string]*DuplicateGroup),
	}
}

// RebuildIndex scans all FileNodes and builds duplicate groups.
func (idx *DuplicateIndex) RebuildIndex(files []*scanner.FileNode) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	idx.groups = make(map[string]*DuplicateGroup)

	// Step 1: Bucket files by composite key (hash + size)
	for _, f := range files {
		if f.Hash == "" {
			continue
		}

		// Collision safety: composite key ensures different sized files with hypothetical same hash are isolated
		key := fmt.Sprintf("%s|%d", f.Hash, f.Size)
		grp, exists := idx.groups[key]
		if !exists {
			grp = &DuplicateGroup{
				ID:       key,
				Hash:     f.Hash,
				FileSize: f.Size,
				Files:    make([]*scanner.FileNode, 0, 2),
			}
			idx.groups[key] = grp
		}
		grp.Files = append(grp.Files, f)
		grp.FileCount = len(grp.Files)
		if grp.FileCount > 1 {
			grp.WastedBytes = int64(grp.FileCount-1) * grp.FileSize
		}
	}

	// Step 2: Purge groups with only 1 file (unique files)
	for key, grp := range idx.groups {
		if grp.FileCount < 2 {
			delete(idx.groups, key)
		} else {
			// Sort files in group by ModTime ascending (oldest first)
			sort.Slice(grp.Files, func(i, j int) bool {
				return grp.Files[i].ModTime < grp.Files[j].ModTime
			})
		}
	}
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
	TotalGroups  int               `json:"totalGroups"`
	TotalFiles   int               `json:"totalFiles"`
	WastedBytes  int64             `json:"wastedBytes"`
	Groups       []*DuplicateGroup `json:"groups"`
	Offset       int               `json:"offset"`
	Limit        int               `json:"limit"`
}

// Query returns filtered and sorted duplicate groups.
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
				extMatch := extLower == "" || strings.EqualFold(f.Extension, extLower)
				searchMatch := searchLower == "" || strings.Contains(strings.ToLower(f.Path), searchLower)
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
				return resultList[i].Files[0].Name < resultList[j].Files[0].Name
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

	paginated := resultList[start:end]

	return QueryResult{
		TotalGroups: totalGroups,
		TotalFiles:  totalMatchedFiles,
		WastedBytes: totalMatchedWasted,
		Groups:      paginated,
		Offset:      start,
		Limit:       filter.Limit,
	}
}

// RemoveFileFromIndex dynamically removes a deleted file from the index.
func (idx *DuplicateIndex) RemoveFileFromIndex(filePath string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	for key, grp := range idx.groups {
		var newFiles []*scanner.FileNode
		for _, f := range grp.Files {
			if f.Path != filePath {
				newFiles = append(newFiles, f)
			}
		}

		if len(newFiles) != len(grp.Files) {
			grp.Files = newFiles
			grp.FileCount = len(newFiles)
			if grp.FileCount < 2 {
				delete(idx.groups, key)
			} else {
				grp.WastedBytes = int64(grp.FileCount-1) * grp.FileSize
			}
		}
	}
}
