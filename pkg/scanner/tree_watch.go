package scanner

import (
	"path/filepath"
	"strings"
)

// tree_watch.go holds the incremental tree mutations used by Monitoramento
// (pkg/watcher). They keep the aggregated sizes and counters of every ancestor
// consistent, so the UI never sees a stale total after a file changes on disk.

// ReplaceFile replaces the node stored under f.Path (matched case-insensitively,
// as Windows paths are) with f, or appends f when the path is unknown.
// It returns the size of the previous node and whether a replacement happened,
// so callers can compute an exact size delta for the event log.
func (tm *TreeManager) ReplaceFile(f *FileNode) (previousSize int64, replaced bool) {
	if f == nil || f.Path == "" {
		return 0, false
	}

	dirPath := filepath.Dir(f.Path)
	dirNode := tm.EnsureDirNode(dirPath)
	if dirNode == nil {
		return 0, false
	}

	dirNode.mu.Lock()
	for i, existing := range dirNode.Files {
		if existing == nil {
			continue
		}
		if existing.Path == f.Path || strings.EqualFold(existing.Path, f.Path) {
			previousSize = existing.Size
			dirNode.Files[i] = f
			dirNode.TotalSize += f.Size - previousSize
			replaced = true
			break
		}
	}
	if !replaced {
		dirNode.Files = append(dirNode.Files, f)
		dirNode.FileCount++
		dirNode.TotalSize += f.Size
	}
	dirNode.mu.Unlock()

	if replaced {
		tm.bubbleUpSize(dirPath, f.Size-previousSize, 0)
	} else {
		tm.bubbleUpSize(dirPath, f.Size, 1)
	}

	return previousSize, replaced
}

// FindFile returns the file node stored at filePath, matched case-insensitively
// like the Windows filesystem does, or nil when the path is unknown.
func (tm *TreeManager) FindFile(filePath string) *FileNode {
	if filePath == "" {
		return nil
	}
	dirNode := tm.FindDir(filepath.Dir(filePath))
	if dirNode == nil {
		return nil
	}

	dirNode.mu.RLock()
	defer dirNode.mu.RUnlock()
	for _, f := range dirNode.Files {
		if f == nil {
			continue
		}
		if f.Path == filePath || strings.EqualFold(f.Path, filePath) {
			return f
		}
	}
	return nil
}

// RemoveDir detaches the whole subtree rooted at dirPath and subtracts its
// aggregated size and file count from every ancestor. It returns the freed
// bytes, the number of files that were dropped, and whether the folder existed.
// Removing a scanned root drops the root itself.
func (tm *TreeManager) RemoveDir(dirPath string) (removedBytes int64, removedFiles int64, ok bool) {
	if dirPath == "" || dirPath == "." {
		return 0, 0, false
	}

	node := tm.FindDir(dirPath)
	if node == nil {
		return 0, 0, false
	}

	removedBytes, removedFiles = sumSubtree(node)

	clean := filepath.Clean(dirPath)
	rootKey := normalizeRootKey(clean)
	vol := filepath.VolumeName(clean)
	isRoot := strings.EqualFold(clean, rootKey) || (vol != "" && strings.EqualFold(clean, vol))

	if isRoot {
		tm.mu.Lock()
		for k, r := range tm.Roots {
			if r == node || strings.EqualFold(k, rootKey) {
				delete(tm.Roots, k)
			}
		}
		tm.mu.Unlock()
		return removedBytes, removedFiles, true
	}

	parentPath := filepath.Dir(clean)
	parent := tm.FindDir(parentPath)
	if parent == nil {
		return 0, 0, false
	}

	base := filepath.Base(clean)
	detached := false
	parent.mu.Lock()
	for name, child := range parent.Children {
		if child == node || name == base || strings.EqualFold(name, base) {
			delete(parent.Children, name)
			detached = true
			break
		}
	}
	if detached {
		parent.SubDirCount = int64(len(parent.Children))
		parent.TotalSize -= removedBytes
		parent.FileCount -= removedFiles
	}
	parent.mu.Unlock()

	if !detached {
		return 0, 0, false
	}

	tm.bubbleUpSize(parentPath, -removedBytes, -removedFiles)
	return removedBytes, removedFiles, true
}

// sumSubtree walks a subtree adding up the real file sizes and counts, instead
// of trusting cached aggregates that a partial scan may have left stale.
func sumSubtree(node *DirNode) (totalBytes int64, totalFiles int64) {
	if node == nil {
		return 0, 0
	}

	node.mu.RLock()
	for _, f := range node.Files {
		if f == nil {
			continue
		}
		totalBytes += f.Size
		totalFiles++
	}
	children := make([]*DirNode, 0, len(node.Children))
	for _, child := range node.Children {
		children = append(children, child)
	}
	node.mu.RUnlock()

	for _, child := range children {
		cBytes, cFiles := sumSubtree(child)
		totalBytes += cBytes
		totalFiles += cFiles
	}
	return totalBytes, totalFiles
}
