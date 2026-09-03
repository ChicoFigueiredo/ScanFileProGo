package server

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"scanfile/pkg/scanner"
)

// seedTree põe um arquivo na árvore para que o Autosave tenha o que gravar.
func seedTree(t *testing.T, app *AppServer, name string) {
	t.Helper()
	dir := filepath.Join(tempDir(t), "raiz")
	app.Tree().AddFile(scanner.NewFileNodeAt(filepath.Join(dir, name), scanner.FileMeta{Name: name, Size: 1024, Extension: ".bin", ModTime: 1700000000}))
}

func TestAutoSaveDuringScanRespectsInterval(t *testing.T) {
	app, _ := newScanTestServer(t)
	seedTree(t, app, "a.bin")

	cfg := scanner.ScanConfig{AutoSaveIntervalMinutes: 5}
	app.setPhase(PhaseMetadata, "")
	base := time.Now()
	app.noteAutoSaveBaseline(cfg, base)

	if app.maybeAutoSave(base.Add(1 * time.Minute)) {
		t.Error("o Autosave gravou antes de vencer o intervalo de 5 min")
	}
	if !app.maybeAutoSave(base.Add(6 * time.Minute)) {
		t.Error("o Autosave não gravou depois de vencer o intervalo de 5 min")
	}
	if _, err := os.Stat(filepath.Join(app.savedScansDir, scanner.DefaultAutoSaveFileName)); err != nil {
		t.Errorf("o Autosave não chegou ao disco: %v", err)
	}
}

func TestAutoSaveWhileWatchingOnlyWritesWhenTreeChanged(t *testing.T) {
	app, _ := newScanTestServer(t)
	seedTree(t, app, "a.bin")

	cfg := scanner.ScanConfig{}
	app.setPhase(PhaseWatching, "")
	base := time.Now()
	app.noteAutoSaveBaseline(cfg, base)

	// Intervalo vencido, mas nada mudou na árvore: não grava.
	if app.maybeAutoSave(base.Add(11 * time.Minute)) {
		t.Error("o Autosave gravou com o Monitoramento ativo sem nenhuma mudança")
	}

	// Uma mudança na árvore destrava a próxima gravação.
	seedTree(t, app, "b.bin")
	if !app.maybeAutoSave(base.Add(11 * time.Minute)) {
		t.Fatal("o Autosave não gravou depois de a árvore mudar")
	}

	// E a segunda passagem, sem nova mudança, não grava de novo.
	if app.maybeAutoSave(base.Add(30 * time.Minute)) {
		t.Error("o Autosave gravou duas vezes sem mudança na árvore")
	}
}

func TestAutoSaveIsSilentOutsideScanAndWatching(t *testing.T) {
	app, _ := newScanTestServer(t)
	seedTree(t, app, "a.bin")

	cfg := scanner.ScanConfig{}
	app.noteAutoSaveBaseline(cfg, time.Now().Add(-time.Hour))

	for _, phase := range []string{PhaseIdle, PhaseCompleted, PhaseCancelling, PhaseCancelled, PhaseLoadingCache} {
		app.setPhase(phase, "")
		if app.maybeAutoSave(time.Now()) {
			t.Errorf("o Autosave gravou na fase %q", phase)
		}
	}
}

func TestSingleAutoSaveTickerPerProcess(t *testing.T) {
	app, _ := newScanTestServer(t)

	cfg := scanner.ScanConfig{AutoSaveIntervalMinutes: 5}
	app.setPhase(PhaseMetadata, "")

	app.ensureAutoSaveLoop(cfg)
	app.autosaveMu.Lock()
	first := app.autosaveStop
	running := app.autosaveRunning
	app.autosaveMu.Unlock()
	if !running || first == nil {
		t.Fatal("o relógio do Autosave não subiu")
	}

	for i := 0; i < 5; i++ {
		app.ensureAutoSaveLoop(cfg)
	}
	app.autosaveMu.Lock()
	second := app.autosaveStop
	app.autosaveMu.Unlock()
	if first != second {
		t.Error("uma segunda chamada criou outro relógio de Autosave; o contrato pede um por processo")
	}

	app.StopBackground()
	app.autosaveMu.Lock()
	stopped := !app.autosaveRunning
	app.autosaveMu.Unlock()
	if !stopped {
		t.Error("StopBackground não desligou o relógio do Autosave")
	}

	// Depois do desligamento, ensureAutoSaveLoop não religa nada.
	app.ensureAutoSaveLoop(cfg)
	app.autosaveMu.Lock()
	restarted := app.autosaveRunning
	app.autosaveMu.Unlock()
	if restarted {
		t.Error("o relógio do Autosave voltou depois do desligamento do servidor")
	}
}

func TestStopBackgroundIsIdempotent(t *testing.T) {
	app, _ := newScanTestServer(t)
	app.StopBackground()
	app.StopBackground()
}

func TestIsBusyPhase(t *testing.T) {
	busy := []string{PhaseMetadata, PhaseHashing, PhaseIndexing, PhaseCancelling, PhaseLoadingCache}
	free := []string{PhaseIdle, PhaseCompleted, PhaseCancelled, PhaseWatching}

	for _, p := range busy {
		if !isBusyPhase(p) {
			t.Errorf("a fase %q deveria bloquear uma nova Varredura", p)
		}
	}
	for _, p := range free {
		if isBusyPhase(p) {
			t.Errorf("a fase %q não deveria bloquear uma nova Varredura", p)
		}
	}
}

func TestGetLiveMemoryStats(t *testing.T) {
	stats := GetLiveMemoryStats()
	if stats == nil {
		t.Fatal("as métricas de memória não podem ser nulas")
	}
	if stats.Goroutines <= 0 {
		t.Errorf("goroutines = %d, esperado > 0", stats.Goroutines)
	}
}

// TestConcurrentAutoSaveWritesKeepSnapshotValid prova que duas gravações de
// Autosave ao mesmo tempo — o relógio do Autosave e a fronteira de fase da
// orquestração — não truncam uma o fluxo gzip da outra nem rodam o último
// backup bom para fora. É o mesmo tipo de defeito do achado C4, que já custou
// os dois Autosaves.
func TestConcurrentAutoSaveWritesKeepSnapshotValid(t *testing.T) {
	app, _ := newScanTestServer(t)

	const wantFiles = 3000
	dir := filepath.Join(tempDir(t), "raiz")
	fillDir(t, app, dir, wantFiles)
	app.setScanState([]string{dir}, scanner.ScanConfig{Roots: []string{dir}})

	cfg := scanner.ScanConfig{Roots: []string{dir}, HashAlgorithm: "xxhash"}
	app.setPhase(PhaseWatching, "")

	const writers = 8
	wrote := make([]bool, writers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			wrote[idx] = app.writeAutoSave(cfg, time.Now())
		}(i)
	}
	close(start)
	wg.Wait()

	for i, ok := range wrote {
		if !ok {
			t.Errorf("a gravação concorrente %d do Autosave falhou", i)
		}
	}

	// O Autosave final abre como gzip válido e traz a árvore inteira.
	latest := filepath.Join(app.savedScansDir, scanner.DefaultAutoSaveFileName)
	tm, summary, err := scanner.LoadCacheSummaryFromFile(latest, nil)
	if err != nil {
		t.Fatalf("o Autosave final não abre depois das gravações concorrentes: %v", err)
	}
	if summary.TotalFiles != wantFiles {
		t.Errorf("totalFiles no Autosave = %d, esperado %d", summary.TotalFiles, wantFiles)
	}
	if got := tm.GetTotalFileCount(); got != wantFiles {
		t.Errorf("a árvore relida tem %d arquivos, esperado %d", got, wantFiles)
	}

	// O backup rotacionado também precisa continuar legível: é ele o último
	// Autosave bom se o mais recente se perder.
	backup := filepath.Join(app.savedScansDir, scanner.BackupAutoSaveFileName)
	if _, statErr := os.Stat(backup); statErr == nil {
		if _, backupSummary, loadErr := scanner.LoadCacheSummaryFromFile(backup, nil); loadErr != nil {
			t.Errorf("o backup rotacionado ficou corrompido: %v", loadErr)
		} else if backupSummary.TotalFiles != wantFiles {
			t.Errorf("totalFiles no backup = %d, esperado %d", backupSummary.TotalFiles, wantFiles)
		}
	}

	// E nenhuma gravação deixou arquivo temporário para trás.
	entries, err := os.ReadDir(app.savedScansDir)
	if err != nil {
		t.Fatalf("não foi possível listar a pasta de Snapshots: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), "temp") {
			t.Errorf("sobrou um temporário de Autosave na pasta: %s", e.Name())
		}
	}
}
