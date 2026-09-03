// Package watcher implements Monitoramento: the state that follows a Varredura
// Completa, where the Raízes Varridas are observed recursively and the tree and
// the indexes are updated as files change on disk.
//
// The OS-specific part only reports which paths changed (watcher_windows.go uses
// ReadDirectoryChangesW; watcher_other.go is an honest fsnotify stub). Everything
// else lives here: coalescing per path, deciding what changed by looking at the
// filesystem, and a bounded background queue that hashes files off the event
// loop, so a burst of writes never blocks the OS notification buffer (achado H4).
package watcher

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"scanfile/pkg/hasher"
	"scanfile/pkg/indexer"
	"scanfile/pkg/scanner"
)

const (
	// DefaultDebounce is the silence window applied per path before a change is
	// acted upon, so a file written 50 times in a row is hashed once.
	DefaultDebounce = 2 * time.Second
	// DefaultHashWorkers is the number of background hashing workers.
	DefaultHashWorkers = 2
	// maxDelayFactor forces a dispatch after Debounce*maxDelayFactor even if the
	// path keeps changing, so a continuously appended log is not starved forever.
	maxDelayFactor = 15
	// minMaxDelay floors that safety valve: a file still being written is not
	// worth hashing, so the valve must never cut a legitimate burst short.
	minMaxDelay = 30 * time.Second
	// hashQueueCapacity bounds the background hashing queue.
	hashQueueCapacity = 4096
)

// ErrAlreadyRunning is returned by Start when Monitoramento is already active.
var ErrAlreadyRunning = errors.New("monitoramento já está em execução")

// ErrNoTree is returned by New when no TreeManager was supplied.
var ErrNoTree = errors.New("monitoramento exige uma árvore (Options.Tree)")

// Options configures a FSWatcher.
type Options struct {
	Tree        *scanner.TreeManager
	Index       *indexer.DuplicateIndex
	FolderIndex *indexer.FolderDuplicateIndex
	Debounce    time.Duration                              // default 2s
	HashWorkers int                                        // default 2
	HashFunc    func(path string) (hash string, err error) // nil: files are tracked without hash
	Ignore      func(path string) bool                     // nil: DefaultIgnore
	OnEvent     func(scanner.FSEventLog)
	OnOverflow  func(root string)
	BufferSize  int // OS notification buffer, only for tests
}

// FSWatcher coordinates OS-level filesystem hooks and updates the in-memory tree
// and indexes in near real time.
type FSWatcher struct {
	opts Options

	mu        sync.Mutex
	running   bool
	cancel    context.CancelFunc
	stopRoots []func()
	roots     []string

	wg sync.WaitGroup

	pendingMu sync.Mutex
	pending   map[string]*pendingChange

	queueMu sync.Mutex
	queue   chan string
	queued  map[string]bool

	changeCount atomic.Uint64
}

// pendingChange holds the coalescing state of a single path.
type pendingChange struct {
	first   time.Time
	last    time.Time
	renamed bool
}

// New creates a watcher. It does not touch the filesystem until Start is called.
func New(opts Options) (*FSWatcher, error) {
	if opts.Tree == nil {
		return nil, ErrNoTree
	}
	if opts.Debounce <= 0 {
		opts.Debounce = DefaultDebounce
	}
	if opts.HashWorkers <= 0 {
		opts.HashWorkers = DefaultHashWorkers
	}
	if opts.Ignore == nil {
		opts.Ignore = DefaultIgnore
	}
	return &FSWatcher{
		opts:    opts,
		pending: make(map[string]*pendingChange),
		queued:  make(map[string]bool),
	}, nil
}

// NewFSWatcher builds a watcher with the legacy positional arguments.
//
// Deprecated: use New with Options. Kept so pkg/server keeps compiling while the
// integration step of the contract is not done.
func NewFSWatcher(tree *scanner.TreeManager, index *indexer.DuplicateIndex, hashAlgo string) (*FSWatcher, error) {
	return New(Options{
		Tree:  tree,
		Index: index,
		HashFunc: func(path string) (string, error) {
			h, _, err := hasher.ComputeSingleFileHash(path, hashAlgo)
			return h, err
		},
	})
}

// DefaultIgnore skips Windows areas that generate noise and are never useful to
// the user: the recycle bin, restore points, and the paging files.
func DefaultIgnore(path string) bool {
	lower := strings.ToLower(path)
	if strings.Contains(lower, "$recycle.bin") ||
		strings.Contains(lower, "system volume information") ||
		strings.Contains(lower, "\\$extend\\") {
		return true
	}
	switch strings.ToLower(filepath.Base(path)) {
	case "pagefile.sys", "hiberfil.sys", "swapfile.sys":
		return true
	}
	return false
}

// Start begins watching roots until ctx is cancelled or Stop is called.
//
// The optional onEvent argument is deprecated and only exists for the legacy
// call shape used by pkg/server; prefer Options.OnEvent.
func (fw *FSWatcher) Start(ctx context.Context, roots []string, onEvent ...func(scanner.FSEventLog)) error {
	fw.mu.Lock()
	if fw.running {
		fw.mu.Unlock()
		return ErrAlreadyRunning
	}
	if len(onEvent) > 0 && onEvent[0] != nil {
		fw.opts.OnEvent = onEvent[0]
	}

	ctx, cancel := context.WithCancel(ctx)
	fw.cancel = cancel
	fw.running = true
	fw.stopRoots = nil
	fw.roots = normalizeRoots(roots)

	fw.pendingMu.Lock()
	fw.pending = make(map[string]*pendingChange)
	fw.pendingMu.Unlock()

	fw.queueMu.Lock()
	fw.queue = make(chan string, hashQueueCapacity)
	fw.queued = make(map[string]bool)
	queue := fw.queue
	fw.queueMu.Unlock()

	rootsToWatch := fw.roots
	fw.mu.Unlock()

	fw.wg.Add(1)
	go fw.coalesceLoop(ctx)

	for i := 0; i < fw.opts.HashWorkers; i++ {
		fw.wg.Add(1)
		go fw.hashWorker(ctx, queue)
	}

	var errs []error
	started := 0
	for _, root := range rootsToWatch {
		root := root
		stop, err := startRootWatch(root, fw.bufferSizeFor(root), changeSink{
			Change:   func(path string, renamed bool) { fw.notifyChange(path, renamed) },
			Overflow: func() { fw.notifyOverflow(root) },
		})
		if err != nil {
			errs = append(errs, err)
			continue
		}
		fw.mu.Lock()
		fw.stopRoots = append(fw.stopRoots, stop)
		fw.mu.Unlock()
		started++
	}

	if started == 0 && len(rootsToWatch) > 0 {
		fw.Stop()
		return errors.Join(errs...)
	}
	return nil
}

// Stop cancels the pending OS reads, waits for every goroutine started by the
// watcher and leaves the watcher reusable.
func (fw *FSWatcher) Stop() {
	fw.mu.Lock()
	if !fw.running {
		fw.mu.Unlock()
		return
	}
	fw.running = false
	cancel := fw.cancel
	stops := fw.stopRoots
	fw.cancel = nil
	fw.stopRoots = nil
	fw.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	for _, stop := range stops {
		if stop != nil {
			stop()
		}
	}
	fw.wg.Wait()
}

// IsRunning reports whether Monitoramento is active.
func (fw *FSWatcher) IsRunning() bool {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	return fw.running
}

// ChangeCount returns how many changes were applied to the tree since creation.
func (fw *FSWatcher) ChangeCount() uint64 {
	return fw.changeCount.Load()
}

// Roots returns the roots currently being watched.
func (fw *FSWatcher) Roots() []string {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	out := make([]string, len(fw.roots))
	copy(out, fw.roots)
	return out
}

// bufferSizeFor picks the OS notification buffer: 1 MiB for local volumes and
// 64 KiB for network paths, which reject larger buffers.
func (fw *FSWatcher) bufferSizeFor(root string) int {
	if fw.opts.BufferSize > 0 {
		return fw.opts.BufferSize
	}
	if isNetworkPath(root) {
		return 64 * 1024
	}
	return 1024 * 1024
}

// notifyChange records a raw OS notification for later coalescing.
func (fw *FSWatcher) notifyChange(path string, renamed bool) {
	if path == "" || fw.opts.Ignore(path) {
		return
	}

	now := time.Now()
	fw.pendingMu.Lock()
	entry, ok := fw.pending[path]
	if !ok {
		entry = &pendingChange{first: now}
		fw.pending[path] = entry
	}
	entry.last = now
	entry.renamed = entry.renamed || renamed
	fw.pendingMu.Unlock()
}

// notifyOverflow reports that the OS dropped notifications for a root; the
// caller is expected to re-scan it.
func (fw *FSWatcher) notifyOverflow(root string) {
	if fw.opts.OnOverflow != nil {
		fw.opts.OnOverflow(root)
	}
}

// coalesceLoop drains paths that have been silent for Debounce.
func (fw *FSWatcher) coalesceLoop(ctx context.Context) {
	defer fw.wg.Done()

	tick := fw.opts.Debounce / 4
	if tick < 5*time.Millisecond {
		tick = 5 * time.Millisecond
	}
	if tick > 500*time.Millisecond {
		tick = 500 * time.Millisecond
	}

	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, due := range fw.takeDuePaths(time.Now()) {
				if ctx.Err() != nil {
					return
				}
				fw.dispatch(ctx, due.path, due.renamed)
			}
		}
	}
}

type duePath struct {
	path    string
	renamed bool
}

// takeDuePaths removes and returns the paths whose debounce window elapsed.
func (fw *FSWatcher) takeDuePaths(now time.Time) []duePath {
	maxDelay := fw.opts.Debounce * maxDelayFactor
	if maxDelay < minMaxDelay {
		maxDelay = minMaxDelay
	}

	fw.pendingMu.Lock()
	defer fw.pendingMu.Unlock()

	var due []duePath
	for path, entry := range fw.pending {
		quiet := now.Sub(entry.last) >= fw.opts.Debounce
		starving := now.Sub(entry.first) >= maxDelay
		if quiet || starving {
			due = append(due, duePath{path: path, renamed: entry.renamed})
			delete(fw.pending, path)
		}
	}
	return due
}

// dispatch decides what happened to a path by looking at the filesystem, which
// makes renames fall out naturally as a removal plus a creation.
func (fw *FSWatcher) dispatch(ctx context.Context, path string, renamed bool) {
	info, err := os.Lstat(path)
	if err != nil {
		fw.handleRemoval(path, renamed)
		return
	}

	if info.IsDir() {
		if fw.opts.Tree.FindDir(path) != nil {
			// Already known: a child changed, nothing to record for the folder.
			return
		}
		fw.opts.Tree.EnsureDirNode(path)
		fw.markFolderIndexDirty()
		fw.changeCount.Add(1)
		fw.emit(opFor("CREATE", renamed), path, 0)
		return
	}

	fw.enqueueHash(ctx, path)
}

// handleRemoval drops a vanished file or folder from the tree and the indexes.
func (fw *FSWatcher) handleRemoval(path string, renamed bool) {
	if size, found := fw.removeFileFromTree(path); found {
		if fw.opts.Index != nil {
			fw.opts.Index.RemoveFileFromIndex(path)
		}
		fw.markFolderIndexDirty()
		fw.changeCount.Add(1)
		fw.emit(opFor("REMOVE", renamed), path, -size)
		return
	}

	if node := fw.opts.Tree.FindDir(path); node != nil {
		if fw.opts.Index != nil {
			fw.opts.Index.RemoveDirFromIndex(path)
		}
		if removedBytes, _, ok := fw.opts.Tree.RemoveDir(path); ok {
			fw.markFolderIndexDirty()
			fw.changeCount.Add(1)
			fw.emit(opFor("REMOVE", renamed), path, -removedBytes)
		}
	}
}

// removeFileFromTree drops a file, retrying with the stored path when Windows
// reports a different letter case than the one recorded by the Varredura.
func (fw *FSWatcher) removeFileFromTree(path string) (int64, bool) {
	if size, found := fw.opts.Tree.RemoveFile(path); found {
		return size, true
	}
	if node := fw.opts.Tree.FindFile(path); node != nil {
		return fw.opts.Tree.RemoveFile(node.Path)
	}
	return 0, false
}

// enqueueHash pushes a file onto the bounded background queue, skipping paths
// already waiting so a burst collapses into a single hash.
func (fw *FSWatcher) enqueueHash(ctx context.Context, path string) {
	fw.queueMu.Lock()
	queue := fw.queue
	if queue == nil || fw.queued[path] {
		fw.queueMu.Unlock()
		return
	}
	fw.queued[path] = true
	fw.queueMu.Unlock()

	select {
	case queue <- path:
	case <-ctx.Done():
		fw.queueMu.Lock()
		delete(fw.queued, path)
		fw.queueMu.Unlock()
	}
}

// hashWorker consumes the queue: it reads metadata, computes the hash and
// applies the change to the tree and to the duplicate index.
func (fw *FSWatcher) hashWorker(ctx context.Context, queue chan string) {
	defer fw.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case path, ok := <-queue:
			if !ok {
				return
			}
			fw.queueMu.Lock()
			delete(fw.queued, path)
			fw.queueMu.Unlock()
			fw.processFile(path)
		}
	}
}

// processFile refreshes a single file in the tree and in the duplicate index.
func (fw *FSWatcher) processFile(path string) {
	info, err := os.Lstat(path)
	if err != nil {
		fw.handleRemoval(path, false)
		return
	}
	if info.IsDir() {
		fw.opts.Tree.EnsureDirNode(path)
		fw.markFolderIndexDirty()
		return
	}

	modTime, createTime, accessTime := scanner.ExtractFileTimestamps(info)
	allocated, compressed := scanner.GetAllocatedFileSize(path, info)

	node := &scanner.FileNode{
		Path:          path,
		Name:          filepath.Base(path),
		Size:          info.Size(),
		AllocatedSize: allocated,
		ModTime:       modTime,
		CreateTime:    createTime,
		AccessTime:    accessTime,
		Extension:     strings.ToLower(filepath.Ext(path)),
		IsCompressed:  compressed,
		IsSymlink:     info.Mode()&os.ModeSymlink != 0,
	}

	if fw.opts.HashFunc != nil {
		// A locked file keeps an empty hash: it stays in the tree but leaves the
		// duplicate index, instead of posing as a duplicate of its old content.
		if hash, hashErr := fw.opts.HashFunc(path); hashErr == nil {
			node.Hash = hash
		}
	}

	previousSize, replaced := fw.opts.Tree.ReplaceFile(node)
	if fw.opts.Index != nil {
		fw.opts.Index.UpsertFile(node)
	}
	fw.markFolderIndexDirty()
	fw.changeCount.Add(1)

	if replaced {
		fw.emit("WRITE", path, node.Size-previousSize)
		return
	}
	fw.emit("CREATE", path, node.Size)
}

func (fw *FSWatcher) markFolderIndexDirty() {
	if fw.opts.FolderIndex != nil {
		fw.opts.FolderIndex.MarkDirty()
	}
}

func (fw *FSWatcher) emit(op, path string, sizeDelta int64) {
	if fw.opts.OnEvent == nil {
		return
	}
	fw.opts.OnEvent(scanner.FSEventLog{
		Timestamp: time.Now(),
		Op:        op,
		Path:      path,
		SizeDelta: sizeDelta,
	})
}

// opFor labels an event RENAME when the OS reported a rename for that path.
func opFor(op string, renamed bool) string {
	if renamed {
		return "RENAME"
	}
	return op
}

// normalizeRoots cleans the roots and drops case-insensitive duplicates.
func normalizeRoots(roots []string) []string {
	seen := make(map[string]bool, len(roots))
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		clean := filepath.Clean(root)
		key := strings.ToLower(clean)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, clean)
	}
	return out
}

// isNetworkPath reports whether a root lives on a UNC share.
func isNetworkPath(root string) bool {
	return strings.HasPrefix(root, "\\\\") || strings.HasPrefix(root, "//")
}

// changeSink is how the OS-specific code reports what it saw.
type changeSink struct {
	// Change reports one absolute path that changed.
	Change func(path string, renamed bool)
	// Overflow reports that the OS buffer overflowed and notifications were lost.
	Overflow func()
}
