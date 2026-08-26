package watcher

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"scanfile/pkg/hasher"
	"scanfile/pkg/indexer"
	"scanfile/pkg/scanner"
)

// FSWatcher coordinates OS-level filesystem hooks and updates memory data structures in real-time.
type FSWatcher struct {
	watcher    *fsnotify.Watcher
	tree       *scanner.TreeManager
	index      *indexer.DuplicateIndex
	hashAlgo   string
	cancelFunc context.CancelFunc
	isRunning  bool
	mu         sync.Mutex

	onEvent func(scanner.FSEventLog)
}

// NewFSWatcher initializes a new real-time OS watcher.
func NewFSWatcher(tree *scanner.TreeManager, index *indexer.DuplicateIndex, hashAlgo string) (*FSWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	return &FSWatcher{
		watcher:  w,
		tree:     tree,
		index:    index,
		hashAlgo: hashAlgo,
	}, nil
}

// Start begins monitoring the specified root paths.
func (fw *FSWatcher) Start(ctx context.Context, roots []string, onEvent func(scanner.FSEventLog)) error {
	fw.mu.Lock()
	if fw.isRunning {
		fw.mu.Unlock()
		return nil
	}
	fw.isRunning = true
	fw.onEvent = onEvent
	ctx, cancel := context.WithCancel(ctx)
	fw.cancelFunc = cancel
	fw.mu.Unlock()

	// Register roots with watcher
	for _, root := range roots {
		_ = fw.watcher.Add(root)
	}

	go fw.eventLoop(ctx)
	return nil
}

// Stop stops the watcher.
func (fw *FSWatcher) Stop() {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	if !fw.isRunning {
		return
	}
	fw.isRunning = false
	if fw.cancelFunc != nil {
		fw.cancelFunc()
	}
	if fw.watcher != nil {
		_ = fw.watcher.Close()
	}
}

func (fw *FSWatcher) eventLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-fw.watcher.Events:
			if !ok {
				return
			}
			fw.handleEvent(event)
		case _, ok := <-fw.watcher.Errors:
			if !ok {
				return
			}
		}
	}
}

func (fw *FSWatcher) handleEvent(event fsnotify.Event) {
	path := event.Name
	if strings.Contains(path, "$Recycle.Bin") || strings.Contains(path, "System Volume Information") {
		return
	}

	var opName string
	var sizeDelta int64

	switch {
	case event.Has(fsnotify.Create):
		opName = "CREATE"
		info, err := os.Stat(path)
		if err == nil {
			if info.IsDir() {
				_ = fw.watcher.Add(path)
				fw.tree.EnsureDirNode(path)
			} else {
				size := info.Size()
				sizeDelta = size
				modTime := info.ModTime().Unix()
				ext := strings.ToLower(filepath.Ext(path))

				// Compute hash in background or synchronously
				hVal, _, _ := hasher.ComputeSingleFileHash(path, fw.hashAlgo)

				node := &scanner.FileNode{
					Path:      path,
					Name:      filepath.Base(path),
					Size:      size,
					ModTime:   modTime,
					Hash:      hVal,
					Extension: ext,
				}
				fw.tree.AddFile(node)
				if hVal != "" {
					fw.index.RebuildIndex(fw.tree.GetAllFiles())
				}
			}
		}

	case event.Has(fsnotify.Write):
		opName = "WRITE"
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			// Update file size and hash
			oldSize, _ := fw.tree.RemoveFile(path)
			newSize := info.Size()
			sizeDelta = newSize - oldSize
			modTime := info.ModTime().Unix()
			ext := strings.ToLower(filepath.Ext(path))

			hVal, _, _ := hasher.ComputeSingleFileHash(path, fw.hashAlgo)

			node := &scanner.FileNode{
				Path:      path,
				Name:      filepath.Base(path),
				Size:      newSize,
				ModTime:   modTime,
				Hash:      hVal,
				Extension: ext,
			}
			fw.tree.AddFile(node)
			if hVal != "" {
				fw.index.RebuildIndex(fw.tree.GetAllFiles())
			}
		}

	case event.Has(fsnotify.Remove):
		opName = "REMOVE"
		removedSize, found := fw.tree.RemoveFile(path)
		if found {
			sizeDelta = -removedSize
			fw.index.RemoveFileFromIndex(path)
		}

	case event.Has(fsnotify.Rename):
		opName = "RENAME"
		removedSize, found := fw.tree.RemoveFile(path)
		if found {
			sizeDelta = -removedSize
			fw.index.RemoveFileFromIndex(path)
		}
	}

	if opName != "" && fw.onEvent != nil {
		fw.onEvent(scanner.FSEventLog{
			Timestamp: time.Now(),
			Op:        opName,
			Path:      path,
			SizeDelta: sizeDelta,
		})
	}
}
