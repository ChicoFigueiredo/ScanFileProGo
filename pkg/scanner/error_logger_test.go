package scanner

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestDiskErrorLogger_FlushesEvery32Lines(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewDiskErrorLogger(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })

	for i := 0; i < flushEveryLines; i++ {
		logger.Log("phase1_readdir", `C:\pasta\arquivo.txt`, "acesso negado")
	}

	data, err := os.ReadFile(logger.GetFilePath())
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Count(string(data), "acesso negado")
	if got != flushEveryLines {
		t.Errorf("esperava %d linhas em disco após o gatilho de flush, obtive %d", flushEveryLines, got)
	}
}

func TestDiskErrorLogger_TickerFlushes(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewDiskErrorLogger(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })

	logger.Log("SKIPPED", `C:\solitaria`, "linha unica")

	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(logger.GetFilePath())
		if err == nil && strings.Contains(string(data), "linha unica") {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("o ticker de %v deveria ter descarregado a linha em disco", flushInterval)
}

func TestDiskErrorLogger_CloseIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewDiskErrorLogger(dir)
	if err != nil {
		t.Fatal(err)
	}
	logger.Log("phase2_hash_open", `C:\travado.bin`, "bloqueado")

	if logger.IsClosed() {
		t.Error("logger recém-criado não deveria estar fechado")
	}
	if err := logger.Close(); err != nil {
		t.Fatalf("primeiro Close: %v", err)
	}
	if !logger.IsClosed() {
		t.Error("IsClosed deveria ser verdadeiro após Close")
	}
	if err := logger.Close(); err != nil {
		t.Fatalf("segundo Close deveria ser inócuo: %v", err)
	}
	if err := logger.Close(); err != nil {
		t.Fatalf("terceiro Close deveria ser inócuo: %v", err)
	}

	// Log após Close não pode entrar em pânico nem escrever.
	logger.Log("phase1", `C:\depois`, "ignorado")

	data, err := os.ReadFile(logger.GetFilePath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "bloqueado") {
		t.Error("Close deveria descarregar as linhas pendentes")
	}
	if strings.Contains(string(data), "ignorado") {
		t.Error("Log após Close não deveria escrever")
	}
}

func TestDiskErrorLogger_NilReceiverIsSafe(t *testing.T) {
	var logger *DiskErrorLogger
	logger.Log("x", "y", "z")
	logger.Flush()
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	if logger.GetFilePath() != "" {
		t.Error("logger nulo deveria devolver caminho vazio")
	}
	if !logger.IsClosed() {
		t.Error("logger nulo conta como fechado")
	}
}
