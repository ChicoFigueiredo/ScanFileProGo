package watcher

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"scanfile/pkg/indexer"
	"scanfile/pkg/scanner"
)

// testDebounce keeps the tests fast while still exercising the real coalescing
// logic (the production default is DefaultDebounce).
const testDebounce = 150 * time.Millisecond

// burstDebounce is used by the burst tests. It has to stay comfortably above the
// interval between two writes even under -race, where a single WriteFile on
// Windows can take hundreds of milliseconds; otherwise the silence window would
// legitimately open in the middle of the burst.
const burstDebounce = 1500 * time.Millisecond

// waitFor polls cond until it holds or the timeout expires. Every watcher test
// synchronizes this way: never a bare sleep as the only synchronization.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !cond() {
		t.Fatalf("tempo esgotado esperando: %s", what)
	}
}

// eventRecorder collects the events emitted by the watcher from several goroutines.
type eventRecorder struct {
	mu     sync.Mutex
	events []scanner.FSEventLog
}

func (r *eventRecorder) record(ev scanner.FSEventLog) {
	r.mu.Lock()
	r.events = append(r.events, ev)
	r.mu.Unlock()
}

func (r *eventRecorder) opsFor(path string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var ops []string
	for _, ev := range r.events {
		if strings.EqualFold(ev.Path, path) {
			ops = append(ops, ev.Op)
		}
	}
	return ops
}

// harness wires a watcher over a real temporary folder without relying on OS
// notifications: tests inject the changes themselves, which keeps the tree,
// index and coalescing assertions deterministic on every platform.
type harness struct {
	t          *testing.T
	dir        string
	tree       *scanner.TreeManager
	index      *indexer.DuplicateIndex
	folders    *indexer.FolderDuplicateIndex
	watcher    *FSWatcher
	events     *eventRecorder
	hashCalls  atomic.Int64
	overflowed atomic.Int64
}

func newHarness(t *testing.T, mutate ...func(*Options)) *harness {
	t.Helper()

	h := &harness{
		t:       t,
		dir:     t.TempDir(),
		tree:    scanner.NewTreeManager(),
		index:   indexer.NewDuplicateIndex(),
		folders: indexer.NewFolderDuplicateIndex(),
		events:  &eventRecorder{},
	}

	opts := Options{
		Tree:        h.tree,
		Index:       h.index,
		FolderIndex: h.folders,
		Debounce:    testDebounce,
		HashWorkers: 2,
		HashFunc: func(path string) (string, error) {
			h.hashCalls.Add(1)
			data, err := os.ReadFile(path)
			if err != nil {
				return "", err
			}
			return "xxh64:" + fakeHash(data), nil
		},
		OnEvent:    h.events.record,
		OnOverflow: func(string) { h.overflowed.Add(1) },
	}
	for _, m := range mutate {
		m(&opts)
	}

	fw, err := New(opts)
	if err != nil {
		t.Fatalf("New falhou: %v", err)
	}
	h.watcher = fw

	// The tree starts with the root registered, as it would after a Varredura.
	h.tree.GetOrCreateRoot(filepath.VolumeName(h.dir) + "\\")
	h.tree.EnsureDirNode(h.dir)
	return h
}

// fakeHash is a tiny content digest: enough for the index to group twins.
func fakeHash(data []byte) string {
	var sum uint64 = 1469598103934665603
	for _, b := range data {
		sum ^= uint64(b)
		sum *= 1099511628211
	}
	const hex = "0123456789abcdef"
	out := make([]byte, 16)
	for i := 15; i >= 0; i-- {
		out[i] = hex[sum&0xf]
		sum >>= 4
	}
	return string(out)
}

// startInjected starts the watcher with no OS roots: the test drives it.
func (h *harness) startInjected() {
	h.t.Helper()
	if err := h.watcher.Start(context.Background(), nil); err != nil {
		h.t.Fatalf("Start falhou: %v", err)
	}
	h.t.Cleanup(h.watcher.Stop)
}

func (h *harness) touch(rel, content string) string {
	h.t.Helper()
	path := filepath.Join(h.dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		h.t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		h.t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func (h *harness) fileInTree(path string) *scanner.FileNode {
	var found *scanner.FileNode
	h.tree.IterateFiles(func(f *scanner.FileNode) bool {
		if strings.EqualFold(f.Path(), path) {
			found = f
			return false
		}
		return true
	})
	return found
}

func TestNew_RequiresTree(t *testing.T) {
	if _, err := New(Options{}); err != ErrNoTree {
		t.Fatalf("esperava ErrNoTree, obtive %v", err)
	}
}

func TestNew_AppliesDefaults(t *testing.T) {
	fw, err := New(Options{Tree: scanner.NewTreeManager()})
	if err != nil {
		t.Fatalf("New falhou: %v", err)
	}
	if fw.opts.Debounce != DefaultDebounce {
		t.Fatalf("esperava debounce padrão de 2s, obtive %v", fw.opts.Debounce)
	}
	if fw.opts.HashWorkers != DefaultHashWorkers {
		t.Fatalf("esperava 2 workers de hash, obtive %d", fw.opts.HashWorkers)
	}
	if fw.opts.Ignore == nil {
		t.Fatal("esperava filtro Ignore padrão")
	}
	if fw.IsRunning() {
		t.Fatal("watcher não pode nascer em execução")
	}
}

func TestDefaultIgnore(t *testing.T) {
	cases := []struct {
		path   string
		ignore bool
	}{
		{"C:\\$Recycle.Bin\\S-1-5-21\\arquivo.txt", true},
		{"C:\\System Volume Information\\tracking.log", true},
		{"C:\\pagefile.sys", true},
		{"C:\\Users\\chico\\Documentos\\relatorio.docx", false},
		{"C:\\Users\\chico\\recycle.bin.txt", false},
	}
	for _, tc := range cases {
		if got := DefaultIgnore(tc.path); got != tc.ignore {
			t.Errorf("DefaultIgnore(%q) = %v, esperava %v", tc.path, got, tc.ignore)
		}
	}
}

func TestFSWatcher_StartTwiceIsRefused(t *testing.T) {
	h := newHarness(t)
	h.startInjected()
	if !h.watcher.IsRunning() {
		t.Fatal("esperava IsRunning true após Start")
	}
	if err := h.watcher.Start(context.Background(), nil); err != ErrAlreadyRunning {
		t.Fatalf("esperava ErrAlreadyRunning, obtive %v", err)
	}
	h.watcher.Stop()
	if h.watcher.IsRunning() {
		t.Fatal("esperava IsRunning false após Stop")
	}
	// Stop is idempotent.
	h.watcher.Stop()
}

func TestFSWatcher_CreateUpdateAndRemoveReachTreeAndIndex(t *testing.T) {
	h := newHarness(t)
	h.startInjected()

	nested := filepath.Join("um", "dois", "tres")
	created := h.touch(filepath.Join(nested, "arquivo.bin"), "conteudo original")
	h.watcher.notifyChange(created, false)

	waitFor(t, 5*time.Second, "arquivo criado aparecer na árvore", func() bool {
		return h.fileInTree(created) != nil
	})

	node := h.fileInTree(created)
	if node.Size != int64(len("conteudo original")) {
		t.Fatalf("tamanho errado na árvore: %d", node.Size)
	}
	if node.Hash() == "" {
		t.Fatal("esperava hash calculado em segundo plano")
	}
	if ops := h.events.opsFor(created); len(ops) == 0 || ops[0] != "CREATE" {
		t.Fatalf("esperava evento CREATE, obtive %v", ops)
	}

	// A twin file forms a duplicate group.
	twin := h.touch(filepath.Join(nested, "copia.bin"), "conteudo original")
	h.watcher.notifyChange(twin, false)
	waitFor(t, 5*time.Second, "grupo de duplicados se formar", func() bool {
		g, _, _ := h.index.GetSummaryStats()
		return g == 1
	})
	_, files, wasted := h.index.GetSummaryStats()
	if files != 2 || wasted != int64(len("conteudo original")) {
		t.Fatalf("índice inconsistente: %d arquivos, %d desperdiçados", files, wasted)
	}

	// Changing the content breaks the group and updates the size.
	if err := os.WriteFile(twin, []byte("conteudo bem maior do que o original"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	h.watcher.notifyChange(twin, false)
	waitFor(t, 5*time.Second, "alteração refletir na árvore", func() bool {
		n := h.fileInTree(twin)
		return n != nil && n.Size == int64(len("conteudo bem maior do que o original"))
	})
	waitFor(t, 5*time.Second, "grupo de duplicados se desfazer", func() bool {
		g, _, _ := h.index.GetSummaryStats()
		return g == 0
	})
	if ops := h.events.opsFor(twin); ops[len(ops)-1] != "WRITE" {
		t.Fatalf("esperava último evento WRITE para o arquivo alterado, obtive %v", ops)
	}

	// Removing it drops the file from tree and index.
	if err := os.Remove(twin); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	h.watcher.notifyChange(twin, false)
	waitFor(t, 5*time.Second, "remoção refletir na árvore", func() bool {
		return h.fileInTree(twin) == nil
	})

	dir := h.tree.FindDir(filepath.Join(h.dir, nested))
	if dir == nil {
		t.Fatal("pasta de três níveis deveria existir na árvore")
	}
	if _, _, _, count, _, _ := dir.GetInfo(); count != 1 {
		t.Fatalf("esperava 1 arquivo restante na pasta, obtive %d", count)
	}
	if h.watcher.ChangeCount() == 0 {
		t.Fatal("ChangeCount deveria ter avançado")
	}
}

func TestFSWatcher_RenameIsReportedAsRename(t *testing.T) {
	h := newHarness(t)
	h.startInjected()

	origin := h.touch(filepath.Join("pasta", "antigo.txt"), "dados")
	h.watcher.notifyChange(origin, false)
	waitFor(t, 5*time.Second, "arquivo original entrar na árvore", func() bool {
		return h.fileInTree(origin) != nil
	})

	target := filepath.Join(h.dir, "pasta", "novo.txt")
	if err := os.Rename(origin, target); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	h.watcher.notifyChange(origin, true)
	h.watcher.notifyChange(target, true)

	waitFor(t, 5*time.Second, "renomeação refletir na árvore", func() bool {
		return h.fileInTree(origin) == nil && h.fileInTree(target) != nil
	})
	if ops := h.events.opsFor(origin); len(ops) == 0 || ops[len(ops)-1] != "RENAME" {
		t.Fatalf("esperava evento RENAME para o caminho antigo, obtive %v", ops)
	}
}

func TestFSWatcher_RemovedFolderLeavesTreeAndIndex(t *testing.T) {
	h := newHarness(t)
	h.startInjected()

	a := h.touch(filepath.Join("alvo", "a.bin"), "iguais")
	b := h.touch(filepath.Join("alvo", "sub", "b.bin"), "iguais")
	h.watcher.notifyChange(a, false)
	h.watcher.notifyChange(b, false)

	waitFor(t, 5*time.Second, "dois arquivos entrarem no índice", func() bool {
		_, files, _ := h.index.GetSummaryStats()
		return files == 2
	})

	folder := filepath.Join(h.dir, "alvo")
	if err := os.RemoveAll(folder); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	h.watcher.notifyChange(folder, false)

	waitFor(t, 5*time.Second, "pasta sair da árvore", func() bool {
		return h.tree.FindDir(folder) == nil
	})
	waitFor(t, 5*time.Second, "índice esvaziar", func() bool {
		g, files, wasted := h.index.GetSummaryStats()
		return g == 0 && files == 0 && wasted == 0
	})
	if h.fileInTree(a) != nil || h.fileInTree(b) != nil {
		t.Fatal("arquivos da pasta removida ainda estão na árvore")
	}
}

func TestFSWatcher_BurstOfWritesProducesOneHash(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.Debounce = burstDebounce })
	h.startInjected()

	path := h.touch("ocupado.log", "0")
	// 50 writes spread over roughly one second, each one notified.
	for i := 0; i < 50; i++ {
		if err := os.WriteFile(path, []byte(strings.Repeat("x", i+1)), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		h.watcher.notifyChange(path, false)
		time.Sleep(10 * time.Millisecond)
	}

	waitFor(t, 10*time.Second, "arquivo ser hasheado uma vez", func() bool {
		return h.hashCalls.Load() >= 1 && h.fileInTree(path) != nil
	})
	// Give the coalescer room to (wrongly) fire again before asserting.
	time.Sleep(2 * burstDebounce)

	if calls := h.hashCalls.Load(); calls != 1 {
		t.Fatalf("esperava 1 hash para a rajada de 50 escritas, obtive %d", calls)
	}
	node := h.fileInTree(path)
	if node == nil || node.Size != 50 {
		t.Fatalf("esperava o último tamanho (50 bytes) na árvore, obtive %+v", node)
	}
}

func TestFSWatcher_IgnoredPathsNeverReachTheTree(t *testing.T) {
	h := newHarness(t, func(o *Options) {
		o.Ignore = func(path string) bool { return strings.HasSuffix(path, ".tmp") }
	})
	h.startInjected()

	ignored := h.touch("temporario.tmp", "lixo")
	kept := h.touch("guardado.txt", "dados")
	h.watcher.notifyChange(ignored, false)
	h.watcher.notifyChange(kept, false)

	waitFor(t, 5*time.Second, "arquivo válido entrar na árvore", func() bool {
		return h.fileInTree(kept) != nil
	})
	if h.fileInTree(ignored) != nil {
		t.Fatal("caminho ignorado não deveria entrar na árvore")
	}
	if h.hashCalls.Load() != 1 {
		t.Fatalf("esperava 1 hash, obtive %d", h.hashCalls.Load())
	}
}

func TestFSWatcher_MarksFolderIndexDirty(t *testing.T) {
	h := newHarness(t)
	h.startInjected()

	if h.folders.IsDirty() {
		t.Fatal("índice de pastas não deveria nascer sujo")
	}
	path := h.touch(filepath.Join("pasta", "arquivo.bin"), "conteudo")
	h.watcher.notifyChange(path, false)

	waitFor(t, 5*time.Second, "índice de pastas ser marcado como sujo", func() bool {
		return h.folders.IsDirty()
	})
}

func TestFSWatcher_StopDoesNotLeakGoroutines(t *testing.T) {
	h := newHarness(t)

	settle := func() {
		for i := 0; i < 20; i++ {
			runtime.GC()
			time.Sleep(10 * time.Millisecond)
		}
	}
	settle()
	before := runtime.NumGoroutine()

	h.watcher.Start(context.Background(), nil)
	path := h.touch("arquivo.bin", "dados")
	h.watcher.notifyChange(path, false)
	waitFor(t, 5*time.Second, "arquivo ser processado", func() bool {
		return h.fileInTree(path) != nil
	})

	h.watcher.Stop()
	settle()

	after := runtime.NumGoroutine()
	if after > before {
		t.Fatalf("Stop vazou goroutines: antes %d, depois %d", before, after)
	}
}

func TestFSWatcher_StartOnMissingRootFails(t *testing.T) {
	h := newHarness(t)
	missing := filepath.Join(h.dir, "pasta-que-nao-existe")
	if err := h.watcher.Start(context.Background(), []string{missing}); err == nil {
		h.watcher.Stop()
		t.Fatal("esperava erro ao observar raiz inexistente")
	}
	if h.watcher.IsRunning() {
		t.Fatal("watcher deveria ter parado após falhar em todas as raízes")
	}
}

func TestNormalizeRoots(t *testing.T) {
	got := normalizeRoots([]string{"C:\\Dados\\", "", "  ", "c:\\dados", "D:\\Outra"})
	if len(got) != 2 {
		t.Fatalf("esperava 2 raízes distintas, obtive %v", got)
	}
	if got[0] != filepath.Clean("C:\\Dados") || got[1] != filepath.Clean("D:\\Outra") {
		t.Fatalf("raízes normalizadas inesperadas: %v", got)
	}
}
