package indexer

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/cespare/xxhash/v2"
	"scanfile/pkg/scanner"
)

// FolderSummary contains aggregated content and hash metadata for a directory.
type FolderSummary struct {
	Path              string `json:"path"`
	Name              string `json:"name"`
	TotalSize         int64  `json:"totalSize"`
	FileCount         int64  `json:"fileCount"`
	SubDirCount       int64  `json:"subDirCount"`
	FolderContentHash string `json:"folderContentHash"`
	ModTime           int64  `json:"modTime"`
}

// DuplicateFolderGroup represents 2 or more directories that have 100% identical contents.
type DuplicateFolderGroup struct {
	ID            string           `json:"id"` // Composite key: folderHash + "|" + size
	FolderHash    string           `json:"folderHash"`
	FolderSize    int64            `json:"folderSize"`
	FileCount     int64            `json:"fileCount"`
	SubDirCount   int64            `json:"subDirCount"` // Total subdirectories inside this folder
	FolderCount   int              `json:"folderCount"`
	WastedBytes   int64            `json:"wastedBytes"` // (FolderCount - 1) * FolderSize
	IsTopLevel    bool             `json:"isTopLevel"`  // True if root duplicate parent (not nested inside another clone group)
	ParentGroupID string           `json:"parentGroupId,omitempty"`
	Folders       []*FolderSummary `json:"folders"`
}

// FolderDuplicateIndex indexes and queries duplicate directories across scanned storage.
type FolderDuplicateIndex struct {
	mu     sync.RWMutex
	groups map[string]*DuplicateFolderGroup
}

// NewFolderDuplicateIndex creates an empty folder duplicate index.
func NewFolderDuplicateIndex() *FolderDuplicateIndex {
	return &FolderDuplicateIndex{
		groups: make(map[string]*DuplicateFolderGroup),
	}
}

// RebuildFolderIndex traverses the tree in a single bottom-up pass and identifies identical duplicate folders.
func (fidx *FolderDuplicateIndex) RebuildFolderIndex(tm *scanner.TreeManager) {
	fidx.mu.Lock()
	defer fidx.mu.Unlock()

	fidx.groups = make(map[string]*DuplicateFolderGroup)

	// Step 1: Collect folder summaries using a single-pass post-order bottom-up Merkle traversal O(N)
	var summaries []*FolderSummary
	roots := tm.GetRootsSnapshot()
	for _, r := range roots {
		computeAndCollectFolderMerkle(r, &summaries)
	}

	// Step 2: Bucket directories by FolderContentHash + FolderSize
	for _, summary := range summaries {
		if summary.FolderContentHash == "" || summary.FileCount == 0 {
			continue
		}

		key := fmt.Sprintf("%s|%d", summary.FolderContentHash, summary.TotalSize)
		grp, exists := fidx.groups[key]
		if !exists {
			grp = &DuplicateFolderGroup{
				ID:          key,
				FolderHash:  summary.FolderContentHash,
				FolderSize:  summary.TotalSize,
				FileCount:   summary.FileCount,
				SubDirCount: summary.SubDirCount,
				IsTopLevel:  true,
				Folders:     make([]*FolderSummary, 0, 2),
			}
			fidx.groups[key] = grp
		}
		if summary.SubDirCount > grp.SubDirCount {
			grp.SubDirCount = summary.SubDirCount
		}
		grp.Folders = append(grp.Folders, summary)
		grp.FolderCount = len(grp.Folders)
		if grp.FolderCount > 1 {
			grp.WastedBytes = int64(grp.FolderCount-1) * grp.FolderSize
		}
	}

	// Step 3: Purge groups with only 1 folder
	for key, grp := range fidx.groups {
		if grp.FolderCount < 2 {
			delete(fidx.groups, key)
		} else {
			// Sort folders within group by Path
			sort.Slice(grp.Folders, func(i, j int) bool {
				return grp.Folders[i].Path < grp.Folders[j].Path
			})
		}
	}

	// Step 4: Detect hierarchy (IsTopLevel vs Sub-Duplicate) in O(N * Depth) via fast path map lookup
	pathGroupMap := make(map[string]*DuplicateFolderGroup, len(fidx.groups)*2)
	for _, grp := range fidx.groups {
		grp.IsTopLevel = true
		for _, f := range grp.Folders {
			normPath := strings.ToLower(filepath.Clean(f.Path))
			pathGroupMap[normPath] = grp
		}
	}

	for path, grp := range pathGroupMap {
		curr := path
		for {
			parent := filepath.Dir(curr)
			if parent == curr || parent == "." || parent == "" {
				break
			}
			if parentGrp, exists := pathGroupMap[parent]; exists && parentGrp.ID != grp.ID {
				grp.IsTopLevel = false
				grp.ParentGroupID = parentGrp.ID
				break
			}
			curr = parent
		}
	}
}

// computeAndCollectFolderMerkle computes Merkle hash bottom-up in O(N) single pass and collects summaries.
func computeAndCollectFolderMerkle(node *scanner.DirNode, list *[]*FolderSummary) string {
	if node == nil {
		return ""
	}

	nodePath, nodeName, totalSize, fileCount, subDirCount, modTime := node.GetInfo()
	children := node.GetChildren()

	// Child Merkle hashes collected bottom-up
	type childHashEntry struct {
		name string
		hash string
	}
	childHashes := make([]childHashEntry, 0, len(children))
	for _, child := range children {
		cHash := computeAndCollectFolderMerkle(child, list)
		if cHash != "" {
			childHashes = append(childHashes, childHashEntry{name: child.Name, hash: cHash})
		}
	}

	if fileCount == 0 {
		return ""
	}

	// Hash direct files of this node
	var directFiles []*scanner.FileNode
	node.RLock(func() {
		directFiles = make([]*scanner.FileNode, len(node.Files))
		copy(directFiles, node.Files)
	})

	hasher := xxhash.New()

	// Sort direct files deterministically by Name
	sort.Slice(directFiles, func(i, j int) bool {
		return directFiles[i].Name < directFiles[j].Name
	})
	for _, f := range directFiles {
		h := f.Hash
		if h == "" {
			h = fmt.Sprintf("sz:%d|mt:%d", f.Size, f.ModTime)
		}
		_, _ = hasher.WriteString(fmt.Sprintf("F|%s|%d|%s\n", f.Name, f.Size, h))
	}

	// Sort subdirectories deterministically by Name
	sort.Slice(childHashes, func(i, j int) bool {
		return childHashes[i].name < childHashes[j].name
	})
	for _, ch := range childHashes {
		_, _ = hasher.WriteString(fmt.Sprintf("D|%s|%s\n", ch.name, ch.hash))
	}

	contentHash := fmt.Sprintf("dir_xxh64:%016x", hasher.Sum64())

	*list = append(*list, &FolderSummary{
		Path:              nodePath,
		Name:              nodeName,
		TotalSize:         totalSize,
		FileCount:         fileCount,
		SubDirCount:       subDirCount,
		FolderContentHash: contentHash,
		ModTime:           modTime,
	})

	return contentHash
}

// ComputeFolderContentHash computes a deterministic content-based Merkle hash for a folder subtree.
func ComputeFolderContentHash(dirNode *scanner.DirNode) string {
	if dirNode == nil {
		return ""
	}
	var dummyList []*FolderSummary
	return computeAndCollectFolderMerkle(dirNode, &dummyList)
}


// FolderQueryFilter options for duplicate folder searches.
type FolderQueryFilter struct {
	SortBy       string `json:"sortBy"`       // "subdirs_desc", "wasted_desc", "files_desc", "size_desc", "count_desc", "name_asc"
	MinSize      int64  `json:"minSize"`      // Minimum folder size
	Search       string `json:"search"`       // Search in path
	TopLevelOnly bool   `json:"topLevelOnly"` // Filter for top-level clone roots only
	Limit        int    `json:"limit"`
	Offset       int    `json:"offset"`
}

// FolderQueryResult represents duplicate folder search results.
type FolderQueryResult struct {
	TotalGroups    int                     `json:"totalGroups"`
	TotalFolders   int                     `json:"totalFolders"`
	WastedBytes    int64                   `json:"wastedBytes"`
	TopLevelGroups int                     `json:"topLevelGroups"`
	Groups         []*DuplicateFolderGroup `json:"groups"`
	Offset         int                     `json:"offset"`
	Limit          int                     `json:"limit"`
}

// Query returns filtered duplicate folder groups.
func (fidx *FolderDuplicateIndex) Query(filter FolderQueryFilter) FolderQueryResult {
	fidx.mu.RLock()
	defer fidx.mu.RUnlock()

	var resultList []*DuplicateFolderGroup
	var totalFolders int
	var totalWasted int64
	var topLevelCount int

	searchLower := strings.ToLower(filter.Search)

	for _, grp := range fidx.groups {
		if grp.IsTopLevel {
			topLevelCount++
		}

		if filter.TopLevelOnly && !grp.IsTopLevel {
			continue
		}

		if filter.MinSize > 0 && grp.FolderSize < filter.MinSize {
			continue
		}

		if searchLower != "" {
			matches := false
			for _, folder := range grp.Folders {
				if strings.Contains(strings.ToLower(folder.Path), searchLower) {
					matches = true
					break
				}
			}
			if !matches {
				continue
			}
		}

		resultList = append(resultList, grp)
		totalFolders += grp.FolderCount
		totalWasted += grp.WastedBytes
	}

	// Sorting
	switch filter.SortBy {
	case "subdirs_desc":
		// Priority: Highest level / most subfolders first
		sort.Slice(resultList, func(i, j int) bool {
			if resultList[i].IsTopLevel != resultList[j].IsTopLevel {
				return resultList[i].IsTopLevel
			}
			if resultList[i].SubDirCount != resultList[j].SubDirCount {
				return resultList[i].SubDirCount > resultList[j].SubDirCount
			}
			if resultList[i].FileCount != resultList[j].FileCount {
				return resultList[i].FileCount > resultList[j].FileCount
			}
			return resultList[i].WastedBytes > resultList[j].WastedBytes
		})
	case "files_desc":
		sort.Slice(resultList, func(i, j int) bool {
			if resultList[i].IsTopLevel != resultList[j].IsTopLevel {
				return resultList[i].IsTopLevel
			}
			if resultList[i].FileCount != resultList[j].FileCount {
				return resultList[i].FileCount > resultList[j].FileCount
			}
			return resultList[i].WastedBytes > resultList[j].WastedBytes
		})
	case "size_desc":
		sort.Slice(resultList, func(i, j int) bool {
			if resultList[i].IsTopLevel != resultList[j].IsTopLevel {
				return resultList[i].IsTopLevel
			}
			return resultList[i].FolderSize > resultList[j].FolderSize
		})
	case "count_desc":
		sort.Slice(resultList, func(i, j int) bool {
			return resultList[i].FolderCount > resultList[j].FolderCount
		})
	case "name_asc":
		sort.Slice(resultList, func(i, j int) bool {
			if len(resultList[i].Folders) > 0 && len(resultList[j].Folders) > 0 {
				return resultList[i].Folders[0].Name < resultList[j].Folders[0].Name
			}
			return false
		})
	case "wasted_desc":
		fallthrough
	default:
		sort.Slice(resultList, func(i, j int) bool {
			if resultList[i].IsTopLevel != resultList[j].IsTopLevel {
				return resultList[i].IsTopLevel
			}
			if resultList[i].SubDirCount != resultList[j].SubDirCount {
				return resultList[i].SubDirCount > resultList[j].SubDirCount
			}
			return resultList[i].WastedBytes > resultList[j].WastedBytes
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

	return FolderQueryResult{
		TotalGroups:    totalGroups,
		TotalFolders:   totalFolders,
		WastedBytes:    totalWasted,
		TopLevelGroups: topLevelCount,
		Groups:         paginated,
		Offset:         start,
		Limit:          filter.Limit,
	}
}

// GetSummaryStats returns summary statistics for duplicate folders.
func (fidx *FolderDuplicateIndex) GetSummaryStats() (groupCount, folderCount int, wastedBytes int64) {
	fidx.mu.RLock()
	defer fidx.mu.RUnlock()

	for _, grp := range fidx.groups {
		groupCount++
		folderCount += grp.FolderCount
		wastedBytes += grp.WastedBytes
	}
	return
}

// FileDiffEntry describes the comparison status of an individual file in a folder diff.
type FileDiffEntry struct {
	RelativePath string `json:"relativePath"`
	Status       string `json:"status"` // "IDENTICAL", "MODIFIED", "ONLY_IN_A", "ONLY_IN_B"
	SizeA        int64  `json:"sizeA"`
	SizeB        int64  `json:"sizeB"`
	HashA        string `json:"hashA,omitempty"`
	HashB        string `json:"hashB,omitempty"`
	ModTimeA     int64  `json:"modTimeA,omitempty"`
	ModTimeB     int64  `json:"modTimeB,omitempty"`
}

// FolderComparisonResult holds the detailed side-by-side comparison between 2 folders.
type FolderComparisonResult struct {
	PathA             string           `json:"pathA"`
	PathB             string           `json:"pathB"`
	NameA             string           `json:"nameA"`
	NameB             string           `json:"nameB"`
	TotalSizeA        int64            `json:"totalSizeA"`
	TotalSizeB        int64            `json:"totalSizeB"`
	TotalFilesA       int64            `json:"totalFilesA"`
	TotalFilesB       int64            `json:"totalFilesB"`
	FolderHashA       string           `json:"folderHashA"`
	FolderHashB       string           `json:"folderHashB"`
	Is100PercentMatch bool             `json:"is100PercentMatch"`
	MatchPercentage   float64          `json:"matchPercentage"`
	IdenticalCount    int              `json:"identicalCount"`
	IdenticalBytes    int64            `json:"identicalBytes"`
	ModifiedCount     int              `json:"modifiedCount"`
	OnlyInACount      int              `json:"onlyInACount"`
	OnlyInABytes      int64            `json:"onlyInABytes"`
	OnlyInBCount      int              `json:"onlyInBCount"`
	OnlyInBBytes      int64            `json:"onlyInBBytes"`
	DiffEntries       []*FileDiffEntry `json:"diffEntries"`
}

// CompareFolders performs a full side-by-side content comparison between Folder A and Folder B.
func CompareFolders(tm *scanner.TreeManager, pathA, pathB string) (*FolderComparisonResult, error) {
	nodeA := tm.FindDir(pathA)
	if nodeA == nil {
		return nil, fmt.Errorf("pasta A não encontrada na árvore: %s", pathA)
	}

	nodeB := tm.FindDir(pathB)
	if nodeB == nil {
		return nil, fmt.Errorf("pasta B não encontrada na árvore: %s", pathB)
	}

	filesA := nodeA.GetAllFiles()
	filesB := nodeB.GetAllFiles()

	mapA := make(map[string]*scanner.FileNode)
	for _, f := range filesA {
		rel, _ := filepath.Rel(nodeA.Path, f.Path)
		rel = strings.ReplaceAll(rel, "\\", "/")
		mapA[rel] = f
	}

	mapB := make(map[string]*scanner.FileNode)
	for _, f := range filesB {
		rel, _ := filepath.Rel(nodeB.Path, f.Path)
		rel = strings.ReplaceAll(rel, "\\", "/")
		mapB[rel] = f
	}

	// Collect all distinct relative paths
	allRelPathsMap := make(map[string]bool)
	for p := range mapA {
		allRelPathsMap[p] = true
	}
	for p := range mapB {
		allRelPathsMap[p] = true
	}

	var allRelPaths []string
	for p := range allRelPathsMap {
		allRelPaths = append(allRelPaths, p)
	}
	sort.Strings(allRelPaths)

	var diffEntries []*FileDiffEntry
	var identicalCount, modifiedCount, onlyInACount, onlyInBCount int
	var identicalBytes, onlyInABytes, onlyInBBytes int64

	for _, rel := range allRelPaths {
		fA, inA := mapA[rel]
		fB, inB := mapB[rel]

		if inA && inB {
			// Compare hashes or sizes
			isSame := false
			if fA.Hash != "" && fB.Hash != "" {
				isSame = (fA.Hash == fB.Hash) && (fA.Size == fB.Size)
			} else {
				isSame = (fA.Size == fB.Size) && (fA.ModTime == fB.ModTime)
			}

			if isSame {
				identicalCount++
				identicalBytes += fA.Size
				diffEntries = append(diffEntries, &FileDiffEntry{
					RelativePath: rel,
					Status:       "IDENTICAL",
					SizeA:        fA.Size,
					SizeB:        fB.Size,
					HashA:        fA.Hash,
					HashB:        fB.Hash,
					ModTimeA:     fA.ModTime,
					ModTimeB:     fB.ModTime,
				})
			} else {
				modifiedCount++
				diffEntries = append(diffEntries, &FileDiffEntry{
					RelativePath: rel,
					Status:       "MODIFIED",
					SizeA:        fA.Size,
					SizeB:        fB.Size,
					HashA:        fA.Hash,
					HashB:        fB.Hash,
					ModTimeA:     fA.ModTime,
					ModTimeB:     fB.ModTime,
				})
			}
		} else if inA && !inB {
			onlyInACount++
			onlyInABytes += fA.Size
			diffEntries = append(diffEntries, &FileDiffEntry{
				RelativePath: rel,
				Status:       "ONLY_IN_A",
				SizeA:        fA.Size,
				HashA:        fA.Hash,
				ModTimeA:     fA.ModTime,
			})
		} else if !inA && inB {
			onlyInBCount++
			onlyInBBytes += fB.Size
			diffEntries = append(diffEntries, &FileDiffEntry{
				RelativePath: rel,
				Status:       "ONLY_IN_B",
				SizeB:        fB.Size,
				HashB:        fB.Hash,
				ModTimeB:     fB.ModTime,
			})
		}
	}

	hashA := ComputeFolderContentHash(nodeA)
	hashB := ComputeFolderContentHash(nodeB)
	is100Match := (hashA == hashB) && (modifiedCount == 0) && (onlyInACount == 0) && (onlyInBCount == 0)

	var matchPct float64
	totalDistinct := len(allRelPaths)
	if totalDistinct > 0 {
		matchPct = (float64(identicalCount) / float64(totalDistinct)) * 100.0
	}

	return &FolderComparisonResult{
		PathA:             nodeA.Path,
		PathB:             nodeB.Path,
		NameA:             nodeA.Name,
		NameB:             nodeB.Name,
		TotalSizeA:        nodeA.TotalSize,
		TotalSizeB:        nodeB.TotalSize,
		TotalFilesA:       nodeA.FileCount,
		TotalFilesB:       nodeB.FileCount,
		FolderHashA:       hashA,
		FolderHashB:       hashB,
		Is100PercentMatch: is100Match,
		MatchPercentage:   matchPct,
		IdenticalCount:    identicalCount,
		IdenticalBytes:    identicalBytes,
		ModifiedCount:     modifiedCount,
		OnlyInACount:      onlyInACount,
		OnlyInABytes:      onlyInABytes,
		OnlyInBCount:      onlyInBCount,
		OnlyInBBytes:      onlyInBBytes,
		DiffEntries:       diffEntries,
	}, nil
}
