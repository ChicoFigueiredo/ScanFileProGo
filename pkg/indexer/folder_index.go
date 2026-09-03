package indexer

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

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
	// AllFilesHashed is true only when every file in the subtree has a full
	// content hash. When false the Merkle hash fell back to size + modification
	// time, which is a hint and never proof of identical content (achado M14).
	AllFilesHashed bool `json:"allFilesHashed"`
}

// DuplicateFolderGroup represents 2 or more directories that have 100% identical contents.
type DuplicateFolderGroup struct {
	ID            string `json:"id"` // Composite key: folderHash + "|" + size
	FolderHash    string `json:"folderHash"`
	FolderSize    int64  `json:"folderSize"`
	FileCount     int64  `json:"fileCount"`
	SubDirCount   int64  `json:"subDirCount"` // Total subdirectories inside this folder
	FolderCount   int    `json:"folderCount"`
	WastedBytes   int64  `json:"wastedBytes"` // (FolderCount - 1) * FolderSize
	IsTopLevel    bool   `json:"isTopLevel"`  // True if root duplicate parent (not nested inside another clone group)
	ParentGroupID string `json:"parentGroupId,omitempty"`
	// Confidence is ConfidenceHash when every folder in the group had all of its
	// files hashed, and ConfidenceSizeMTime otherwise (achado M14).
	Confidence string           `json:"confidence"`
	Folders    []*FolderSummary `json:"folders"`
}

// FolderDuplicateIndex indexes and queries duplicate directories across scanned storage.
type FolderDuplicateIndex struct {
	mu     sync.RWMutex
	groups map[string]*DuplicateFolderGroup
	// dirty is raised by Monitoramento whenever the tree changes, so the costly
	// Merkle pass runs once on demand instead of on every query (achado M3).
	dirty atomic.Bool
}

// NewFolderDuplicateIndex creates an empty folder duplicate index.
func NewFolderDuplicateIndex() *FolderDuplicateIndex {
	return &FolderDuplicateIndex{
		groups: make(map[string]*DuplicateFolderGroup),
	}
}

// MarkDirty records that the tree changed and the folder index is stale.
func (fidx *FolderDuplicateIndex) MarkDirty() {
	fidx.dirty.Store(true)
}

// IsDirty reports whether a rebuild is pending.
func (fidx *FolderDuplicateIndex) IsDirty() bool {
	return fidx.dirty.Load()
}

// RebuildIfDirty rebuilds the index only when MarkDirty was called since the
// last rebuild, and reports whether the rebuild actually happened.
//
// O sinalizador é baixado aqui, antes de esperar o lock e antes de ler a
// árvore, e nunca mais é tocado depois disso: qualquer MarkDirty levantado
// durante a reconstrução — enquanto ela espera o lock ou enquanto caminha —
// continua de pé e pede outra reconstrução. Limpar depois de começar a ler
// engoliria essa marcação e deixaria a visão de Pastas Clones velha para
// sempre. Uma reconstrução a mais é barata; uma marcação perdida, não.
func (fidx *FolderDuplicateIndex) RebuildIfDirty(tm *scanner.TreeManager) bool {
	if tm == nil || !fidx.dirty.CompareAndSwap(true, false) {
		return false
	}
	fidx.rebuild(tm)
	return true
}

// RebuildFolderIndex traverses the tree in a single bottom-up pass and identifies identical duplicate folders.
func (fidx *FolderDuplicateIndex) RebuildFolderIndex(tm *scanner.TreeManager) {
	// Mesma ordem de RebuildIfDirty: baixa o sinalizador antes de ler a árvore.
	fidx.dirty.Store(false)
	fidx.rebuild(tm)
}

// rebuild does the traversal itself. It never touches the dirty flag: whoever
// asked for the rebuild already lowered it before the tree was read.
func (fidx *FolderDuplicateIndex) rebuild(tm *scanner.TreeManager) {
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
				Confidence:  ConfidenceHash,
				Folders:     make([]*FolderSummary, 0, 2),
			}
			fidx.groups[key] = grp
		}
		if summary.SubDirCount > grp.SubDirCount {
			grp.SubDirCount = summary.SubDirCount
		}
		if !summary.AllFilesHashed {
			// One unhashed file anywhere downgrades the whole group (achado M14).
			grp.Confidence = ConfidenceSizeMTime
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
// It also propagates whether every file in the subtree carries a full content
// hash, which decides the confidence reported to the user (achado M14).
func computeAndCollectFolderMerkle(node *scanner.DirNode, list *[]*FolderSummary) (contentHash string, allHashed bool) {
	if node == nil {
		return "", true
	}

	nodePath, nodeName, totalSize, fileCount, subDirCount, modTime := node.GetInfo()
	children := node.GetChildren()

	// Child Merkle hashes collected bottom-up
	type childHashEntry struct {
		name string
		hash string
	}
	childHashes := make([]childHashEntry, 0, len(children))
	allHashed = true
	for _, child := range children {
		cHash, cHashed := computeAndCollectFolderMerkle(child, list)
		if !cHashed {
			allHashed = false
		}
		if cHash != "" {
			childHashes = append(childHashes, childHashEntry{name: child.Name, hash: cHash})
		}
	}

	if fileCount == 0 {
		return "", allHashed
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
		return directFiles[i].Name() < directFiles[j].Name()
	})
	for _, f := range directFiles {
		h := f.Hash()
		if h == "" {
			// No content hash available: fall back to size + modification time,
			// and remember that this folder cannot claim hash confidence.
			h = fmt.Sprintf("sz:%d|mt:%d", f.Size, f.ModTime())
			allHashed = false
		}
		_, _ = hasher.WriteString(fmt.Sprintf("F|%s|%d|%s\n", f.Name(), f.Size, h))
	}

	// Sort subdirectories deterministically by Name
	sort.Slice(childHashes, func(i, j int) bool {
		return childHashes[i].name < childHashes[j].name
	})
	for _, ch := range childHashes {
		_, _ = hasher.WriteString(fmt.Sprintf("D|%s|%s\n", ch.name, ch.hash))
	}

	contentHash = fmt.Sprintf("dir_xxh64:%016x", hasher.Sum64())

	*list = append(*list, &FolderSummary{
		Path:              nodePath,
		Name:              nodeName,
		TotalSize:         totalSize,
		FileCount:         fileCount,
		SubDirCount:       subDirCount,
		FolderContentHash: contentHash,
		ModTime:           modTime,
		AllFilesHashed:    allHashed,
	})

	return contentHash, allHashed
}

// ComputeFolderContentHash computes a deterministic content-based Merkle hash for a folder subtree.
func ComputeFolderContentHash(dirNode *scanner.DirNode) string {
	if dirNode == nil {
		return ""
	}
	var dummyList []*FolderSummary
	hash, _ := computeAndCollectFolderMerkle(dirNode, &dummyList)
	return hash
}

// FolderSummaryOf returns the summary of a single folder, including its Merkle
// content hash and whether every file below it is hashed.
func FolderSummaryOf(dirNode *scanner.DirNode) *FolderSummary {
	if dirNode == nil {
		return nil
	}
	var list []*FolderSummary
	_, _ = computeAndCollectFolderMerkle(dirNode, &list)
	path, _, _, _, _, _ := dirNode.GetInfo()
	for _, summary := range list {
		if summary.Path == path {
			return summary
		}
	}
	return nil
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
	PathA       string `json:"pathA"`
	PathB       string `json:"pathB"`
	NameA       string `json:"nameA"`
	NameB       string `json:"nameB"`
	TotalSizeA  int64  `json:"totalSizeA"`
	TotalSizeB  int64  `json:"totalSizeB"`
	TotalFilesA int64  `json:"totalFilesA"`
	TotalFilesB int64  `json:"totalFilesB"`
	FolderHashA string `json:"folderHashA"`
	FolderHashB string `json:"folderHashB"`
	// Confidence is ConfidenceHash only when every compared file on both sides
	// had a full content hash; otherwise the verdict rests on size + modification
	// time and Is100PercentMatch stays false (achado M14).
	Confidence        string           `json:"confidence"`
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

	pathA_, nameA, sizeA, filesCountA, _, _ := nodeA.GetInfo()
	pathB_, nameB, sizeB, filesCountB, _, _ := nodeB.GetInfo()

	filesA := nodeA.GetAllFiles()
	filesB := nodeB.GetAllFiles()

	mapA := make(map[string]*scanner.FileNode)
	for _, f := range filesA {
		rel, _ := filepath.Rel(pathA_, f.Path())
		rel = strings.ReplaceAll(rel, "\\", "/")
		mapA[rel] = f
	}

	mapB := make(map[string]*scanner.FileNode)
	for _, f := range filesB {
		rel, _ := filepath.Rel(pathB_, f.Path())
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

	// Starts optimistic and is downgraded by the first file lacking a hash.
	confidence := ConfidenceHash
	for _, f := range filesA {
		if f.Hash() == "" {
			confidence = ConfidenceSizeMTime
			break
		}
	}
	if confidence == ConfidenceHash {
		for _, f := range filesB {
			if f.Hash() == "" {
				confidence = ConfidenceSizeMTime
				break
			}
		}
	}

	for _, rel := range allRelPaths {
		fA, inA := mapA[rel]
		fB, inB := mapB[rel]

		if inA && inB {
			// Compare hashes or sizes
			isSame := false
			if fA.Hash() != "" && fB.Hash() != "" {
				isSame = (fA.Hash() == fB.Hash()) && (fA.Size == fB.Size)
			} else {
				// Without hashes the comparison is a heuristic, never proof.
				confidence = ConfidenceSizeMTime
				isSame = (fA.Size == fB.Size) && (fA.ModTime() == fB.ModTime())
			}

			if isSame {
				identicalCount++
				identicalBytes += fA.Size
				diffEntries = append(diffEntries, &FileDiffEntry{
					RelativePath: rel,
					Status:       "IDENTICAL",
					SizeA:        fA.Size,
					SizeB:        fB.Size,
					HashA:        fA.Hash(),
					HashB:        fB.Hash(),
					ModTimeA:     fA.ModTime(),
					ModTimeB:     fB.ModTime(),
				})
			} else {
				modifiedCount++
				diffEntries = append(diffEntries, &FileDiffEntry{
					RelativePath: rel,
					Status:       "MODIFIED",
					SizeA:        fA.Size,
					SizeB:        fB.Size,
					HashA:        fA.Hash(),
					HashB:        fB.Hash(),
					ModTimeA:     fA.ModTime(),
					ModTimeB:     fB.ModTime(),
				})
			}
		} else if inA && !inB {
			onlyInACount++
			onlyInABytes += fA.Size
			diffEntries = append(diffEntries, &FileDiffEntry{
				RelativePath: rel,
				Status:       "ONLY_IN_A",
				SizeA:        fA.Size,
				HashA:        fA.Hash(),
				ModTimeA:     fA.ModTime(),
			})
		} else if !inA && inB {
			onlyInBCount++
			onlyInBBytes += fB.Size
			diffEntries = append(diffEntries, &FileDiffEntry{
				RelativePath: rel,
				Status:       "ONLY_IN_B",
				SizeB:        fB.Size,
				HashB:        fB.Hash(),
				ModTimeB:     fB.ModTime(),
			})
		}
	}

	hashA := ComputeFolderContentHash(nodeA)
	hashB := ComputeFolderContentHash(nodeB)
	// Achado M14: only a hash-backed comparison may claim a 100% match.
	is100Match := confidence == ConfidenceHash &&
		(hashA == hashB) && (modifiedCount == 0) && (onlyInACount == 0) && (onlyInBCount == 0)

	var matchPct float64
	totalDistinct := len(allRelPaths)
	if totalDistinct > 0 {
		matchPct = (float64(identicalCount) / float64(totalDistinct)) * 100.0
	}

	return &FolderComparisonResult{
		PathA:             pathA_,
		PathB:             pathB_,
		NameA:             nameA,
		NameB:             nameB,
		TotalSizeA:        sizeA,
		TotalSizeB:        sizeB,
		TotalFilesA:       filesCountA,
		TotalFilesB:       filesCountB,
		FolderHashA:       hashA,
		FolderHashB:       hashB,
		Confidence:        confidence,
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
