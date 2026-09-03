//go:build windows

package watcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// These tests exercise the real ReadDirectoryChangesW pipeline over temporary
// folders. Everything is synchronized by polling with a timeout, never by a
// fixed sleep, so they stay deterministic on slow machines.

// startWatching starts the watcher over the harness folder as a real root.
func (h *harness) startWatching() {
	h.t.Helper()
	if err := h.watcher.Start(context.Background(), []string{h.dir}); err != nil {
		h.t.Fatalf("Start falhou: %v", err)
	}
	h.t.Cleanup(h.watcher.Stop)
}

func TestFSWatcher_Windows_ReflectsRealChangesInNestedFolders(t *testing.T) {
	h := newHarness(t)
	h.startWatching()

	nested := filepath.Join("nivel1", "nivel2", "nivel3")
	created := h.touch(filepath.Join(nested, "relatorio.bin"), "conteudo original")

	waitFor(t, 5*time.Second, "criação em subpasta de 3 níveis chegar à árvore", func() bool {
		n := h.fileInTree(created)
		return n != nil && n.Hash != ""
	})
	if h.tree.FindDir(filepath.Join(h.dir, nested)) == nil {
		t.Fatal("as três subpastas deveriam estar na árvore")
	}

	// A twin makes a duplicate group appear on its own.
	twin := h.touch(filepath.Join(nested, "copia.bin"), "conteudo original")
	waitFor(t, 5*time.Second, "grupo de duplicados aparecer", func() bool {
		g, files, _ := h.index.GetSummaryStats()
		return g == 1 && files == 2
	})

	// Change the content: the group dissolves and the size is refreshed.
	novo := "conteudo alterado e bem maior que o anterior"
	if err := os.WriteFile(twin, []byte(novo), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	waitFor(t, 5*time.Second, "alteração chegar à árvore", func() bool {
		n := h.fileInTree(twin)
		return n != nil && n.Size == int64(len(novo))
	})
	waitFor(t, 5*time.Second, "grupo de duplicados se desfazer", func() bool {
		g, _, _ := h.index.GetSummaryStats()
		return g == 0
	})

	// Rename.
	renamed := filepath.Join(h.dir, nested, "renomeado.bin")
	if err := os.Rename(twin, renamed); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	waitFor(t, 5*time.Second, "renomeação chegar à árvore", func() bool {
		return h.fileInTree(twin) == nil && h.fileInTree(renamed) != nil
	})

	// Removal.
	if err := os.Remove(renamed); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	waitFor(t, 5*time.Second, "remoção chegar à árvore", func() bool {
		return h.fileInTree(renamed) == nil
	})

	// Whole folder removal.
	if err := os.RemoveAll(filepath.Join(h.dir, "nivel1")); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	waitFor(t, 8*time.Second, "pasta removida sair da árvore", func() bool {
		return h.tree.FindDir(filepath.Join(h.dir, "nivel1")) == nil &&
			h.fileInTree(created) == nil
	})
	waitFor(t, 5*time.Second, "índice de duplicados esvaziar", func() bool {
		_, files, wasted := h.index.GetSummaryStats()
		return files == 0 && wasted == 0
	})
}

func TestFSWatcher_Windows_BurstOfWritesProducesOneHash(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.Debounce = burstDebounce })
	h.startWatching()

	path := filepath.Join(h.dir, "ocupado.log")
	for i := 0; i < 50; i++ {
		if err := os.WriteFile(path, []byte(strings.Repeat("x", i+1)), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	waitFor(t, 10*time.Second, "arquivo ser hasheado", func() bool {
		return h.hashCalls.Load() >= 1 && h.fileInTree(path) != nil
	})
	// Room for a second (wrong) dispatch before asserting.
	time.Sleep(2 * burstDebounce)

	if calls := h.hashCalls.Load(); calls != 1 {
		t.Fatalf("esperava 1 hash para 50 escritas em ~1 s, obtive %d", calls)
	}
	if n := h.fileInTree(path); n == nil || n.Size != 50 {
		t.Fatalf("esperava o último tamanho na árvore, obtive %+v", n)
	}
}

func TestFSWatcher_Windows_BufferOverflowCallsOnOverflow(t *testing.T) {
	h := newHarness(t, func(o *Options) {
		// 64 bytes barely fit one FILE_NOTIFY_INFORMATION record, so a burst of
		// creations is guaranteed to overrun the kernel buffer.
		o.BufferSize = 64
	})
	h.startWatching()

	churn := filepath.Join(h.dir, "estouro")
	if err := os.MkdirAll(churn, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && h.overflowed.Load() == 0 {
		for i := 0; i < 100; i++ {
			name := filepath.Join(churn, fmt.Sprintf("arquivo_de_teste_%05d.dat", i))
			if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
		}
		time.Sleep(20 * time.Millisecond)
	}

	if h.overflowed.Load() == 0 {
		t.Fatal("esperava OnOverflow após estourar o buffer de notificações")
	}
	if !h.watcher.IsRunning() {
		t.Fatal("o watcher deve continuar observando após o estouro")
	}

	// Still alive: a later change is either seen or overflows again.
	seen := h.overflowed.Load()
	late := filepath.Join(h.dir, "depois.txt")
	if err := os.WriteFile(late, []byte("ok"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	waitFor(t, 8*time.Second, "watcher continuar reagindo após o estouro", func() bool {
		return h.fileInTree(late) != nil || h.overflowed.Load() > seen
	})
}

func TestFSWatcher_Windows_StopReleasesEveryGoroutine(t *testing.T) {
	h := newHarness(t)

	settle := func() {
		for i := 0; i < 20; i++ {
			runtime.GC()
			time.Sleep(10 * time.Millisecond)
		}
	}
	settle()
	before := runtime.NumGoroutine()

	if err := h.watcher.Start(context.Background(), []string{h.dir}); err != nil {
		t.Fatalf("Start falhou: %v", err)
	}
	path := filepath.Join(h.dir, "arquivo.bin")
	if err := os.WriteFile(path, []byte("dados"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	waitFor(t, 5*time.Second, "arquivo chegar à árvore", func() bool {
		return h.fileInTree(path) != nil
	})

	h.watcher.Stop()
	settle()

	after := runtime.NumGoroutine()
	if after > before {
		t.Fatalf("Stop vazou goroutines: antes %d, depois %d", before, after)
	}

	// Restarting after Stop must work.
	if err := h.watcher.Start(context.Background(), []string{h.dir}); err != nil {
		t.Fatalf("Start após Stop falhou: %v", err)
	}
	h.watcher.Stop()
}

func TestFSWatcher_Windows_MultipleRootsAreWatched(t *testing.T) {
	h := newHarness(t)
	second := t.TempDir()

	if err := h.watcher.Start(context.Background(), []string{h.dir, second}); err != nil {
		t.Fatalf("Start falhou: %v", err)
	}
	t.Cleanup(h.watcher.Stop)

	a := filepath.Join(h.dir, "primeira.bin")
	b := filepath.Join(second, "segunda.bin")
	if err := os.WriteFile(a, []byte("um"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(b, []byte("dois"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	waitFor(t, 5*time.Second, "as duas raízes reportarem mudanças", func() bool {
		return h.fileInTree(a) != nil && h.fileInTree(b) != nil
	})
	if len(h.watcher.Roots()) != 2 {
		t.Fatalf("esperava 2 raízes, obtive %v", h.watcher.Roots())
	}
}
