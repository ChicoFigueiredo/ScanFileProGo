package scanner

import (
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// TreeManager manages the in-memory hierarchy of drives, directories, and files.
type TreeManager struct {
	mu    sync.RWMutex
	Roots map[string]*DirNode // Keyed by normalized root path e.g. "C:\\"
}

// NewTreeManager initializes an empty tree.
func NewTreeManager() *TreeManager {
	return &TreeManager{
		Roots: make(map[string]*DirNode),
	}
}

// Reset clears the tree.
func (tm *TreeManager) Reset() {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.Roots = make(map[string]*DirNode)
}

// GetRootsSnapshot returns a slice copy of the current root nodes under a brief read-lock (Safe from deadlocks).
func (tm *TreeManager) GetRootsSnapshot() []*DirNode {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	roots := make([]*DirNode, 0, len(tm.Roots))
	for _, r := range tm.Roots {
		if r != nil {
			roots = append(roots, r)
		}
	}
	return roots
}

// RootsLock executes a callback with read lock over the roots map.
func (tm *TreeManager) RootsLock(fn func(roots map[string]*DirNode)) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	fn(tm.Roots)
}

// normalizeRootKey standardizes Windows and Unix root drive paths (e.g. "c:" or "c:\" -> "C:\").
func normalizeRootKey(rootPath string) string {
	clean := filepath.Clean(rootPath)
	vol := filepath.VolumeName(clean)
	if vol != "" {
		vol = strings.ToUpper(vol)
		return vol + "\\"
	}
	if len(clean) == 2 && clean[1] == ':' {
		return strings.ToUpper(clean) + "\\"
	}
	if !strings.HasSuffix(clean, "\\") && !strings.HasSuffix(clean, "/") {
		clean += string(filepath.Separator)
	}
	return clean
}

// GetTotalFileCount returns the aggregated total number of files across all roots.
func (tm *TreeManager) GetTotalFileCount() int64 {
	roots := tm.GetRootsSnapshot()
	var total int64
	for _, root := range roots {
		if root != nil {
			root.mu.RLock()
			total += root.FileCount
			root.mu.RUnlock()
		}
	}
	return total
}

// GetOrCreateRoot returns or creates the root directory node for a drive/folder.
func (tm *TreeManager) GetOrCreateRoot(rootPath string) *DirNode {
	rootKey := normalizeRootKey(rootPath)

	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Check exact or case-insensitive match
	for k, r := range tm.Roots {
		if strings.EqualFold(k, rootKey) || strings.EqualFold(k, rootPath) {
			return r
		}
	}

	root := &DirNode{
		Path:     rootKey,
		Name:     rootKey,
		Children: make(map[string]*DirNode),
		Files:    make([]*FileNode, 0),
	}
	tm.Roots[rootKey] = root
	return root
}

// EnsureDirNode finds or creates a directory node along the path.
func (tm *TreeManager) EnsureDirNode(dirPath string) *DirNode {
	clean := filepath.Clean(dirPath)
	rootKey := normalizeRootKey(clean)
	vol := filepath.VolumeName(clean)

	tm.mu.RLock()
	root, exists := tm.Roots[rootKey]
	if !exists {
		for k, r := range tm.Roots {
			if strings.EqualFold(k, rootKey) || strings.EqualFold(k, vol+"\\") {
				root = r
				exists = true
				break
			}
		}
	}
	tm.mu.RUnlock()

	if !exists {
		root = tm.GetOrCreateRoot(rootKey)
	}

	if clean == rootKey || strings.EqualFold(clean, rootKey) || clean == vol || strings.EqualFold(clean, vol) {
		return root
	}

	// Split path relative to root
	rel, err := filepath.Rel(rootKey, clean)
	if err != nil || rel == "." {
		return root
	}

	parts := strings.Split(rel, string(filepath.Separator))
	curr := root

	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		curr.mu.Lock()
		child, ok := curr.Children[part]
		if !ok {
			// Case-insensitive check for existing children
			for cName, cNode := range curr.Children {
				if strings.EqualFold(cName, part) {
					child = cNode
					ok = true
					break
				}
			}
		}
		if !ok {
			childPath := filepath.Join(curr.Path, part)
			child = &DirNode{
				Path:     childPath,
				Name:     part,
				Children: make(map[string]*DirNode),
				Files:    make([]*FileNode, 0),
			}
			curr.Children[part] = child
			curr.SubDirCount++
		}
		curr.mu.Unlock()
		curr = child
	}

	return curr
}

// FastSetDir sets files and subdirectories directly on a node during high-speed scanning without bubbling.
func (tm *TreeManager) FastSetDir(dirPath string, files []*FileNode, subDirNames []string) *DirNode {
	node := tm.EnsureDirNode(dirPath)
	node.mu.Lock()
	node.Files = files
	for _, sub := range subDirNames {
		if _, ok := node.Children[sub]; !ok {
			childPath := filepath.Join(dirPath, sub)
			node.Children[sub] = &DirNode{
				Path:     childPath,
				Name:     sub,
				Children: make(map[string]*DirNode),
				Files:    make([]*FileNode, 0),
			}
		}
	}
	node.SubDirCount = int64(len(node.Children))
	node.mu.Unlock()
	return node
}

// SetDirSymlink marks a directory node as a symbolic link or junction.
func (tm *TreeManager) SetDirSymlink(dirPath string, target string) {
	node := tm.EnsureDirNode(dirPath)
	node.mu.Lock()
	node.IsSymlink = true
	node.LinkTarget = target
	node.mu.Unlock()
}

// ComputeAggregatedSizes calculates TotalSize and FileCount bottom-up for all nodes in the tree in a single fast pass.
func (tm *TreeManager) ComputeAggregatedSizes() {
	tm.mu.RLock()
	roots := make([]*DirNode, 0, len(tm.Roots))
	for _, r := range tm.Roots {
		roots = append(roots, r)
	}
	tm.mu.RUnlock()

	for _, r := range roots {
		computeNodeSize(r)
	}
}

func computeNodeSize(node *DirNode) (totalSize int64, totalAllocated int64, fileCount int64, compressedFiles int64) {
	if node == nil {
		return 0, 0, 0, 0
	}

	node.mu.Lock()
	var directSize int64
	var directAllocated int64
	var directCompressed int64
	for _, f := range node.Files {
		directSize += f.Size
		alloc := f.AllocatedSize
		if alloc == 0 && f.Size > 0 && !f.IsCompressed {
			alloc = f.Size
		}
		directAllocated += alloc
		if f.IsCompressed {
			directCompressed++
		}
	}
	directFiles := int64(len(node.Files))
	children := make([]*DirNode, 0, len(node.Children))
	for _, child := range node.Children {
		children = append(children, child)
	}
	node.mu.Unlock()

	totalSize = directSize
	totalAllocated = directAllocated
	fileCount = directFiles
	compressedFiles = directCompressed

	for _, child := range children {
		cSize, cAlloc, cCount, cComp := computeNodeSize(child)
		totalSize += cSize
		totalAllocated += cAlloc
		fileCount += cCount
		compressedFiles += cComp
	}

	node.mu.Lock()
	node.TotalSize = totalSize
	node.TotalAllocatedSize = totalAllocated
	node.FileCount = fileCount
	node.CompressedFileCount = compressedFiles
	node.SubDirCount = int64(len(children))
	node.mu.Unlock()

	return totalSize, totalAllocated, fileCount, compressedFiles
}

// AddFile inserts a file into the tree and updates parent directories.
func (tm *TreeManager) AddFile(file *FileNode) {
	dirPath := filepath.Dir(file.Path)
	dirNode := tm.EnsureDirNode(dirPath)

	dirNode.mu.Lock()
	dirNode.Files = append(dirNode.Files, file)
	dirNode.FileCount++
	dirNode.TotalSize += file.Size
	dirNode.mu.Unlock()

	tm.bubbleUpSize(dirPath, file.Size, 1)
}

// bubbleUpSize adds size and file count to all ancestors of dirPath.
func (tm *TreeManager) bubbleUpSize(dirPath string, sizeDelta int64, fileCountDelta int64) {
	if sizeDelta == 0 && fileCountDelta == 0 {
		return
	}

	clean := filepath.Clean(dirPath)
	parent := filepath.Dir(clean)

	for parent != clean && parent != "." && parent != "" {
		node := tm.FindDir(parent)
		if node != nil {
			node.mu.Lock()
			node.TotalSize += sizeDelta
			node.FileCount += fileCountDelta
			node.mu.Unlock()
		}
		clean = parent
		parent = filepath.Dir(clean)
	}
}

// FindDir returns the DirNode at path, or nil if not found.
func (tm *TreeManager) FindDir(dirPath string) *DirNode {
	if dirPath == "" || dirPath == "." || dirPath == "Meus Discos" {
		return nil
	}

	clean := filepath.Clean(dirPath)
	vol := filepath.VolumeName(clean)
	rootKey := normalizeRootKey(clean)

	tm.mu.RLock()
	root, exists := tm.Roots[rootKey]
	if !exists {
		// Fallback: check case-insensitively across Roots
		for rKey, rNode := range tm.Roots {
			if strings.EqualFold(rKey, rootKey) || strings.EqualFold(rKey, clean) || strings.EqualFold(rKey, vol+"\\") {
				root = rNode
				exists = true
				break
			}
		}
	}
	tm.mu.RUnlock()

	if !exists {
		return nil
	}

	if clean == rootKey || strings.EqualFold(clean, rootKey) || clean == vol || strings.EqualFold(clean, vol) {
		return root
	}

	rel, err := filepath.Rel(rootKey, clean)
	if err != nil || rel == "." {
		return root
	}

	parts := strings.Split(rel, string(filepath.Separator))
	curr := root

	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		curr.mu.RLock()
		child, ok := curr.Children[part]
		if !ok {
			// Fallback: case-insensitive match for Windows paths
			for cName, cNode := range curr.Children {
				if strings.EqualFold(cName, part) {
					child = cNode
					ok = true
					break
				}
			}
		}
		curr.mu.RUnlock()
		if !ok {
			return nil
		}
		curr = child
	}

	return curr
}

// RemoveFile removes a file from the tree and decrements sizes.
func (tm *TreeManager) RemoveFile(filePath string) (int64, bool) {
	dirPath := filepath.Dir(filePath)
	dirNode := tm.FindDir(dirPath)
	if dirNode == nil {
		return 0, false
	}

	dirNode.mu.Lock()
	var removedSize int64
	var found bool
	newFiles := make([]*FileNode, 0, len(dirNode.Files))
	for _, f := range dirNode.Files {
		if f.Path == filePath {
			removedSize = f.Size
			found = true
		} else {
			newFiles = append(newFiles, f)
		}
	}
	if found {
		dirNode.Files = newFiles
		dirNode.FileCount--
		dirNode.TotalSize -= removedSize
	}
	dirNode.mu.Unlock()

	if found {
		tm.bubbleUpSize(dirPath, -removedSize, -1)
	}

	return removedSize, found
}

// GetDirSummary returns a summary of the requested directory for tree navigation in the UI.
func (tm *TreeManager) GetDirSummary(dirPath string, maxDepth int) *DirSummary {
	if maxDepth <= 0 {
		maxDepth = 5
	} else if maxDepth > 10 {
		maxDepth = 10
	}

	node := tm.FindDir(dirPath)
	if node == nil {
		return nil
	}

	return tm.buildSummary(node, 1, maxDepth)
}

func (tm *TreeManager) buildSummary(node *DirNode, currentDepth, maxDepth int) *DirSummary {
	if node == nil {
		return nil
	}

	node.mu.RLock()
	summary := &DirSummary{
		Path:                node.Path,
		Name:                node.Name,
		TotalSize:           node.TotalSize,
		TotalAllocatedSize:  node.TotalAllocatedSize,
		FileCount:           node.FileCount,
		CompressedFileCount: node.CompressedFileCount,
		SubDirCount:         node.SubDirCount,
		ModTime:             node.ModTime,
		CreateTime:          node.CreateTime,
		AccessTime:          node.AccessTime,
		IsSymlink:           node.IsSymlink,
		LinkTarget:          node.LinkTarget,
	}

	// Preserve files for treemap and table visualization:
	// For the current folder (depth 1), include all direct files.
	// For subfolders (depth > 1), include the top largest files (up to 50 per folder)
	// so huge files (e.g. disk.vhdx, ISOs, ZIPs, virtual disks) are rendered in the Treemap
	// while keeping payload light and protecting memory.
	if currentDepth == 1 {
		summary.Files = make([]*FileNode, len(node.Files))
		copy(summary.Files, node.Files)
	} else if len(node.Files) > 0 {
		sortedFiles := make([]*FileNode, len(node.Files))
		copy(sortedFiles, node.Files)
		sort.Slice(sortedFiles, func(i, j int) bool {
			return sortedFiles[i].Size > sortedFiles[j].Size
		})
		maxFilesPerDir := 50
		if len(sortedFiles) > maxFilesPerDir {
			sortedFiles = sortedFiles[:maxFilesPerDir]
		}
		summary.Files = sortedFiles
	} else {
		summary.Files = []*FileNode{}
	}

	var children []*DirNode
	if currentDepth <= maxDepth {
		children = make([]*DirNode, 0, len(node.Children))
		for _, child := range node.Children {
			children = append(children, child)
		}
	}
	node.mu.RUnlock()

	if currentDepth <= maxDepth && len(children) > 0 {
		subDirs := make([]*DirSummary, 0, len(children))
		for _, child := range children {
			subSummary := tm.buildSummary(child, currentDepth+1, maxDepth)
			if subSummary != nil {
				subDirs = append(subDirs, subSummary)
			}
		}
		sort.Slice(subDirs, func(i, j int) bool {
			return subDirs[i].TotalSize > subDirs[j].TotalSize
		})
		// Cap subdirs: allow up to 500 for the direct folder (depth 1), and 50 for deeper levels to keep treemap snappy
		maxSubDirs := 50
		if currentDepth == 1 {
			maxSubDirs = 500
		}
		if len(subDirs) > maxSubDirs {
			subDirs = subDirs[:maxSubDirs]
		}
		summary.SubDirs = subDirs
	}

	return summary
}

// ExtensionStatResult aggregates count and size per file extension.
type ExtensionStatResult struct {
	Extension  string  `json:"extension"`
	TotalBytes int64   `json:"totalBytes"`
	FileCount  int     `json:"fileCount"`
	Percentage float64 `json:"percentage"`
}

// AggregateExtensionStats computes file extension statistics directly on the tree in a single pass without copying 50M pointers.
func (tm *TreeManager) AggregateExtensionStats() []*ExtensionStatResult {
	extMap := make(map[string]*ExtensionStatResult)
	var grandTotalBytes int64

	tm.IterateFiles(func(f *FileNode) bool {
		ext := strings.ToLower(f.Extension)
		if ext == "" {
			ext = "(sem extensão)"
		}
		st, ok := extMap[ext]
		if !ok {
			st = &ExtensionStatResult{Extension: ext}
			extMap[ext] = st
		}
		st.TotalBytes += f.Size
		st.FileCount++
		grandTotalBytes += f.Size
		return true
	})

	var statsList []*ExtensionStatResult
	for _, st := range extMap {
		if grandTotalBytes > 0 {
			st.Percentage = (float64(st.TotalBytes) / float64(grandTotalBytes)) * 100.0
		}
		statsList = append(statsList, st)
	}

	sort.Slice(statsList, func(i, j int) bool {
		return statsList[i].TotalBytes > statsList[j].TotalBytes
	})

	if len(statsList) > 50 {
		statsList = statsList[:50]
	}

	return statsList
}

// IterateFiles walks all files in the tree, executing fn for each. If fn returns false, iteration stops.
func (tm *TreeManager) IterateFiles(fn func(f *FileNode) bool) {
	tm.mu.RLock()
	roots := make([]*DirNode, 0, len(tm.Roots))
	for _, r := range tm.Roots {
		roots = append(roots, r)
	}
	tm.mu.RUnlock()

	for _, r := range roots {
		if !iterateNodeFiles(r, fn) {
			return
		}
	}
}

func iterateNodeFiles(node *DirNode, fn func(f *FileNode) bool) bool {
	if node == nil {
		return true
	}

	node.mu.RLock()
	files := make([]*FileNode, len(node.Files))
	copy(files, node.Files)
	children := make([]*DirNode, 0, len(node.Children))
	for _, child := range node.Children {
		children = append(children, child)
	}
	node.mu.RUnlock()

	for _, f := range files {
		if !fn(f) {
			return false
		}
	}

	for _, child := range children {
		if !iterateNodeFiles(child, fn) {
			return false
		}
	}

	return true
}

// GetAllFiles collects all FileNodes from all roots in the tree.
func (tm *TreeManager) GetAllFiles() []*FileNode {
	tm.mu.RLock()
	roots := make([]*DirNode, 0, len(tm.Roots))
	for _, r := range tm.Roots {
		roots = append(roots, r)
	}
	tm.mu.RUnlock()

	var allFiles []*FileNode
	for _, r := range roots {
		tm.collectFiles(r, &allFiles)
	}
	return allFiles
}


func (tm *TreeManager) collectFiles(node *DirNode, list *[]*FileNode) {
	if node == nil {
		return
	}

	node.mu.RLock()
	*list = append(*list, node.Files...)
	children := make([]*DirNode, 0, len(node.Children))
	for _, child := range node.Children {
		children = append(children, child)
	}
	node.mu.RUnlock()

	for _, child := range children {
		tm.collectFiles(child, list)
	}
}

// RLock executes a read-locked function on the DirNode.
func (node *DirNode) RLock(fn func()) {
	node.mu.RLock()
	defer node.mu.RUnlock()
	fn()
}

// GetAllFiles collects all files recursively within this subtree.
func (node *DirNode) GetAllFiles() []*FileNode {
	var list []*FileNode
	node.collectSubtreeFiles(&list)
	return list
}

func (node *DirNode) collectSubtreeFiles(list *[]*FileNode) {
	node.mu.RLock()
	*list = append(*list, node.Files...)
	children := make([]*DirNode, 0, len(node.Children))
	for _, child := range node.Children {
		children = append(children, child)
	}
	node.mu.RUnlock()

	for _, child := range children {
		child.collectSubtreeFiles(list)
	}
}

// GetChildren returns a slice copy of the child DirNodes.
func (node *DirNode) GetChildren() []*DirNode {
	node.mu.RLock()
	defer node.mu.RUnlock()
	children := make([]*DirNode, 0, len(node.Children))
	for _, child := range node.Children {
		children = append(children, child)
	}
	return children
}

// GetInfo returns a snapshot copy of the DirNode's scalar fields.
func (node *DirNode) GetInfo() (path, name string, totalSize, fileCount, subDirCount, modTime int64) {
	node.mu.RLock()
	defer node.mu.RUnlock()
	return node.Path, node.Name, node.TotalSize, node.FileCount, node.SubDirCount, node.ModTime
}

