package hasher

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"scanfile/pkg/scanner"
)

func TestRunHashing_CancelReturnsFast(t *testing.T) {
	dir := t.TempDir()
	const fileSize = 4 << 20
	blob := bytes.Repeat([]byte("K"), fileSize)

	var files []*scanner.FileNode
	for i := 0; i < 24; i++ {
		p := filepath.Join(dir, fmt.Sprintf("big%02d.bin", i))
		if err := os.WriteFile(p, blob, 0o644); err != nil {
			t.Fatal(err)
		}
		files = append(files, &scanner.FileNode{Path: p, Name: filepath.Base(p), Size: fileSize})
	}

	h := NewHasher()
	done := make(chan error, 1)
	go func() {
		done <- h.RunHashing(context.Background(), files, ComputeHashOptions{
			Algorithm:     scanner.HashSHA256,
			HashAllFiles:  true,
			MinSize:       1,
			WorkerThreads: 1,
		})
	}()

	time.Sleep(80 * time.Millisecond)
	start := time.Now()
	h.Cancel()

	select {
	case err := <-done:
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Errorf("Cancelamento levou %v, deveria ser < 1s", elapsed)
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("RunHashing deveria devolver context.Canceled, obtive %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("RunHashing não retornou após o Cancelamento")
	}

	if h.IsRunning() {
		t.Error("IsRunning deveria ser falso após o Cancelamento")
	}
}

func TestRunHashing_AlreadyCancelledContext(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.bin")
	if err := os.WriteFile(p, []byte("conteudo"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &scanner.FileNode{Path: p, Name: "a.bin", Size: 8}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	h := NewHasher()
	err := h.RunHashing(ctx, []*scanner.FileNode{f}, ComputeHashOptions{
		Algorithm:    scanner.HashXXHash,
		HashAllFiles: true,
		MinSize:      1,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("esperava context.Canceled, obtive %v", err)
	}
}

// Cancel antes de qualquer execução não pode entrar em pânico nem bloquear.
func TestHasher_CancelBeforeRunIsSafe(t *testing.T) {
	h := NewHasher()
	h.Cancel()
	h.Cancel()
}

// Cancel concorrente com RunHashing não pode disparar corrida de dados.
func TestHasher_ConcurrentCancel(t *testing.T) {
	dir := t.TempDir()
	var files []*scanner.FileNode
	for i := 0; i < 40; i++ {
		p := filepath.Join(dir, fmt.Sprintf("f%02d.bin", i))
		if err := os.WriteFile(p, bytes.Repeat([]byte("c"), 200000), 0o644); err != nil {
			t.Fatal(err)
		}
		files = append(files, &scanner.FileNode{Path: p, Name: filepath.Base(p), Size: 200000})
	}

	h := NewHasher()
	done := make(chan error, 1)
	go func() {
		done <- h.RunHashing(context.Background(), files, ComputeHashOptions{
			Algorithm:     scanner.HashXXHash,
			HashAllFiles:  true,
			MinSize:       1,
			WorkerThreads: 4,
		})
	}()
	for i := 0; i < 20; i++ {
		go h.Cancel()
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunHashing travou sob Cancelamento concorrente")
	}
}

func TestRunHashing_ClampsWorkers(t *testing.T) {
	if got := scanner.ResolveWorkers(1_000_000, scanner.PhaseHashing); got != scanner.MaxThreads() {
		t.Fatalf("clamp da Fase 2 = %d, quer %d", got, scanner.MaxThreads())
	}
}
