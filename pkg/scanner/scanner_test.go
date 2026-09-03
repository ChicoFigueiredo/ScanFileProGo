package scanner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// scanRoot cria a pasta temporária das varreduras com limpeza tolerante.
// No Windows, arquivos recém-criados podem ficar com remoção pendente por
// causa de antivírus e indexador, e t.TempDir() transforma isso em falha do
// teste ("A pasta não está vazia") mesmo sem nenhum handle nosso aberto.
func scanRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "scanfile_scan_*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for i := 0; i < 5; i++ {
			if err := os.RemoveAll(dir); err == nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	})
	return dir
}

func mkTree(t *testing.T, root string, files map[string]int) {
	t.Helper()
	for rel, size := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// mkChain cria uma corrente de pastas aninhadas, cada uma com filesPerDir
// arquivos. Com uma única thread, a Fase 1 precisa percorrê-las em sequência, o
// que dá ao teste uma janela previsível para cancelar no meio do caminho.
func mkChain(t *testing.T, root string, depth, filesPerDir int) {
	t.Helper()
	dir := root
	for d := 0; d < depth; d++ {
		dir = filepath.Join(dir, fmt.Sprintf("n%03d", d))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < filesPerDir; i++ {
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%03d.txt", i)), make([]byte, 64), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// Pastas com o nome repetido três vezes deixaram de ser tratadas como laço
// (achado M1): `src\src\src` precisa ser varrido normalmente.
func TestStartScan_ScansRepeatedDirectoryNames(t *testing.T) {
	root := t.TempDir()
	logDir := t.TempDir()
	mkTree(t, root, map[string]int{
		filepath.Join("src", "src", "src", "alvo.txt"): 128,
		filepath.Join("src", "raso.txt"):               64,
	})

	tm := NewTreeManager()
	s := NewScanner(tm)
	defer s.CloseLogger()

	err := s.StartScan(context.Background(), ScanConfig{
		Roots:         []string{root},
		WorkerThreads: 4,
		LogDir:        logDir,
	}, nil)
	if err != nil {
		t.Fatalf("StartScan: %v", err)
	}

	var found bool
	tm.IterateFiles(func(f *FileNode) bool {
		if filepath.Base(f.Path) == "alvo.txt" {
			found = true
			return false
		}
		return true
	})
	if !found {
		t.Fatalf("src\\src\\src\\alvo.txt deveria ter sido varrido; pulados: %+v", s.GetSkipped())
	}
	if s.SkippedCount() != 0 {
		t.Errorf("nenhum item deveria ter sido pulado, obtive %d: %+v", s.SkippedCount(), s.GetSkipped())
	}
}

// Pastas comuns não entram em visitedDirs e EvalSymlinks não é chamado para
// elas (achado M2): o mapa fica com uma entrada por Raiz Varrida.
func TestStartScan_VisitedDirsOnlyHoldsRoots(t *testing.T) {
	root := t.TempDir()
	logDir := t.TempDir()
	mkTree(t, root, map[string]int{
		filepath.Join("a", "b", "c", "x.txt"): 10,
		filepath.Join("a", "d", "y.txt"):      10,
		filepath.Join("e", "z.txt"):           10,
	})

	s := NewScanner(NewTreeManager())
	defer s.CloseLogger()
	if err := s.StartScan(context.Background(), ScanConfig{
		Roots:  []string{root},
		LogDir: logDir,
	}, nil); err != nil {
		t.Fatal(err)
	}

	count := 0
	s.visitedDirs.Range(func(_, _ any) bool {
		count++
		return true
	})
	if count != 1 {
		t.Errorf("visitedDirs guardou %d caminhos, esperava só a Raiz Varrida", count)
	}
}

func TestStartScan_SkippedItemsAreObservable(t *testing.T) {
	root := t.TempDir()
	logDir := t.TempDir()
	mkTree(t, root, map[string]int{
		filepath.Join("System Volume Information", "tracking.log"): 10,
		"pagefile.sys":                    10,
		filepath.Join("normal", "ok.txt"): 10,
	})

	s := NewScanner(NewTreeManager())
	defer s.CloseLogger()
	if err := s.StartScan(context.Background(), ScanConfig{
		Roots:  []string{root},
		LogDir: logDir,
	}, nil); err != nil {
		t.Fatal(err)
	}
	logPath := s.LoggerPath()
	s.CloseLogger()

	if s.SkippedCount() < 2 {
		t.Fatalf("esperava ao menos 2 itens pulados, obtive %d", s.SkippedCount())
	}
	joined := ""
	for _, e := range s.GetSkipped() {
		joined += e.Path + " | " + e.Reason + "\n"
		if e.Reason == "" {
			t.Errorf("item pulado sem motivo: %+v", e)
		}
		if e.Timestamp.IsZero() {
			t.Errorf("item pulado sem timestamp: %+v", e)
		}
	}
	if !strings.Contains(joined, "System Volume Information") || !strings.Contains(joined, "pagefile.sys") {
		t.Errorf("anel de pulados não registrou os itens esperados:\n%s", joined)
	}

	if s.GetStatus().SkippedCount != s.SkippedCount() {
		t.Error("ScanStatus.SkippedCount não reflete o contador")
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("log de erros não foi criado: %v", err)
	}
	if !strings.Contains(string(data), "SKIPPED") {
		t.Errorf("log em disco não tem linhas SKIPPED:\n%s", data)
	}
	if !strings.Contains(string(data), "pagefile.sys") {
		t.Errorf("log em disco não cita pagefile.sys:\n%s", data)
	}
}

func TestStartScan_MaxDepthIsLoggedAsSkipped(t *testing.T) {
	root := t.TempDir()
	logDir := t.TempDir()
	deep := root
	for i := 0; i < MaxScanDepth+3; i++ {
		deep = filepath.Join(deep, "n")
	}
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Skipf("sistema de arquivos não aceitou o caminho profundo: %v", err)
	}

	s := NewScanner(NewTreeManager())
	defer s.CloseLogger()
	if err := s.StartScan(context.Background(), ScanConfig{
		Roots:  []string{root},
		LogDir: logDir,
	}, nil); err != nil {
		t.Fatal(err)
	}

	var hit bool
	for _, e := range s.GetSkipped() {
		if strings.Contains(e.Reason, "Profundidade") {
			hit = true
		}
	}
	if !hit {
		t.Errorf("estouro de profundidade deveria virar Item Pulado; anel: %+v", s.GetSkipped())
	}
}

// Cancelamento disparado no primeiro retrato de progresso, antes de qualquer
// pasta ser lida: determinístico, sem depender de tempo.
func TestStartScan_CancelFromFirstProgressCallback(t *testing.T) {
	root := t.TempDir()
	logDir := t.TempDir()
	mkTree(t, root, map[string]int{"a.txt": 10})

	s := NewScanner(NewTreeManager())
	defer s.CloseLogger()

	var calls int
	err := s.StartScan(context.Background(), ScanConfig{
		Roots:  []string{root},
		LogDir: logDir,
	}, func(st ScanStatus) {
		calls++
		if calls == 1 {
			if st.Phase != "phase1_metadata" {
				t.Errorf("primeiro retrato deveria trazer a fase phase1_metadata, veio %q", st.Phase)
			}
			s.Cancel()
		}
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("StartScan deveria devolver context.Canceled, obtive %v", err)
	}
	if calls == 0 {
		t.Error("o primeiro retrato de progresso não foi emitido")
	}
	if s.IsRunning() {
		t.Error("IsRunning deveria ser falso após o Cancelamento")
	}
}

// Cancelamento no meio da Fase 1, com a fila e os workers já rodando.
func TestStartScan_CancelDuringPhase1(t *testing.T) {
	root := scanRoot(t)
	logDir := t.TempDir()
	// Corrente funda: uma única thread precisa abrir as 120 pastas em sequência,
	// então cancelar logo após as primeiras deixa a maior parte pela frente.
	mkChain(t, root, 120, 20)

	s := NewScanner(NewTreeManager())
	defer s.CloseLogger()

	done := make(chan error, 1)
	go func() {
		done <- s.StartScan(context.Background(), ScanConfig{
			Roots:         []string{root},
			WorkerThreads: 1,
			LogDir:        logDir,
		}, nil)
	}()

	// Espera a varredura efetivamente começar a ler pastas.
	deadline := time.Now().Add(5 * time.Second)
	for s.GetStatus().TotalDirsScanned < 2 {
		if time.Now().After(deadline) {
			t.Fatal("a Fase 1 não começou a ler pastas a tempo")
		}
	}

	start := time.Now()
	s.Cancel()

	select {
	case err := <-done:
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Errorf("Cancelamento levou %v, deveria ser < 1s", elapsed)
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("StartScan deveria devolver context.Canceled, obtive %v", err)
		}
		if st := s.GetStatus(); st.TotalDirsScanned >= 120 {
			t.Errorf("a varredura terminou sozinha (%d pastas): o teste não exercitou o Cancelamento", st.TotalDirsScanned)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("StartScan não retornou após o Cancelamento")
	}

	if s.IsRunning() {
		t.Error("IsRunning deveria ser falso após o Cancelamento")
	}
}

func TestStartScan_PreCancelledContext(t *testing.T) {
	root := t.TempDir()
	logDir := t.TempDir()
	mkTree(t, root, map[string]int{"a.txt": 10})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := NewScanner(NewTreeManager())
	defer s.CloseLogger()

	err := s.StartScan(ctx, ScanConfig{Roots: []string{root}, LogDir: logDir}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("esperava context.Canceled, obtive %v", err)
	}
}

func TestScanner_CancelBeforeStartIsSafe(t *testing.T) {
	s := NewScanner(NewTreeManager())
	s.Cancel()
	s.Cancel()
	if s.IsRunning() {
		t.Error("IsRunning deveria ser falso sem varredura em curso")
	}
}

// Cancel concorrente não pode entrar em pânico, travar nem disparar corrida.
func TestScanner_ConcurrentCancelIsSafe(t *testing.T) {
	root := scanRoot(t)
	logDir := t.TempDir()
	mkChain(t, root, 40, 10)

	s := NewScanner(NewTreeManager())
	defer s.CloseLogger()

	done := make(chan error, 1)
	go func() {
		done <- s.StartScan(context.Background(), ScanConfig{
			Roots:         []string{root},
			WorkerThreads: 4,
			LogDir:        logDir,
		}, func(ScanStatus) {})
	}()

	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 8; j++ {
				s.Cancel()
				_ = s.GetStatus()
				_ = s.GetSkipped()
				_ = s.SkippedCount()
				_ = s.IsRunning()
			}
		}()
	}
	wg.Wait()

	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("erro inesperado: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("StartScan travou sob Cancelamento concorrente")
	}
}

// Uma segunda Varredura durante a primeira é recusada com ErrScanInProgress.
// O teste dispara a segunda chamada de dentro do retrato de progresso, que roda
// com a primeira Varredura ativa: nada depende de tempo nem de árvore grande.
func TestStartScan_SecondScanIsRefused(t *testing.T) {
	root := t.TempDir()
	logDir := t.TempDir()
	mkTree(t, root, map[string]int{"a.txt": 10})

	s := NewScanner(NewTreeManager())
	defer s.CloseLogger()

	cfg := ScanConfig{Roots: []string{root}, LogDir: logDir}

	var secondErr error
	var checked bool
	err := s.StartScan(context.Background(), cfg, func(ScanStatus) {
		if checked {
			return
		}
		checked = true
		if !s.IsRunning() {
			t.Error("IsRunning deveria ser verdadeiro durante a Varredura")
		}
		secondErr = s.StartScan(context.Background(), cfg, nil)
	})
	if err != nil {
		t.Fatalf("primeira varredura: %v", err)
	}
	if !checked {
		t.Fatal("o retrato de progresso não foi emitido")
	}
	if !errors.Is(secondErr, ErrScanInProgress) {
		t.Errorf("segunda varredura deveria devolver ErrScanInProgress, obtive %v", secondErr)
	}
	if s.IsRunning() {
		t.Error("IsRunning deveria ser falso ao fim da Varredura")
	}
	// A recusa não pode ter derrubado a primeira Varredura.
	if s.GetStatus().TotalFilesScanned != 1 {
		t.Errorf("a primeira Varredura deveria ter concluído: %d arquivos", s.GetStatus().TotalFilesScanned)
	}
}

func TestStartScan_ResolvesWorkerThreads(t *testing.T) {
	root := t.TempDir()
	logDir := t.TempDir()
	mkTree(t, root, map[string]int{"a.txt": 10})

	s := NewScanner(NewTreeManager())
	defer s.CloseLogger()
	if err := s.StartScan(context.Background(), ScanConfig{
		Roots:         []string{root},
		WorkerThreads: 1_000_000, // acima do teto
		LogDir:        logDir,
	}, nil); err != nil {
		t.Fatal(err)
	}

	st := s.GetStatus()
	if st.Phase1Workers != MaxThreads() {
		t.Errorf("Phase1Workers = %d, quer %d (teto)", st.Phase1Workers, MaxThreads())
	}
	if st.Phase2Workers != MaxThreads() {
		t.Errorf("Phase2Workers = %d, quer %d (teto)", st.Phase2Workers, MaxThreads())
	}
}

func TestStartScan_AutoWorkerThreads(t *testing.T) {
	root := t.TempDir()
	logDir := t.TempDir()
	mkTree(t, root, map[string]int{"a.txt": 10})

	s := NewScanner(NewTreeManager())
	defer s.CloseLogger()
	if err := s.StartScan(context.Background(), ScanConfig{
		Roots:  []string{root},
		LogDir: logDir,
	}, nil); err != nil {
		t.Fatal(err)
	}

	st := s.GetStatus()
	if st.Phase1Workers != ResolveWorkers(0, PhaseMetadata) {
		t.Errorf("Auto na Fase 1 = %d, quer %d", st.Phase1Workers, ResolveWorkers(0, PhaseMetadata))
	}
	if st.Phase2Workers != ResolveWorkers(0, PhaseHashing) {
		t.Errorf("Auto na Fase 2 = %d, quer %d", st.Phase2Workers, ResolveWorkers(0, PhaseHashing))
	}
}

// Quick Scan só reaproveita hash cujo prefixo é o do algoritmo atual.
func TestStartScan_QuickScanRejectsForeignAlgorithm(t *testing.T) {
	root := t.TempDir()
	logDirA := t.TempDir()
	logDirB := t.TempDir()
	mkTree(t, root, map[string]int{"dado.bin": 256})
	target := filepath.Join(root, "dado.bin")

	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	modTime, _, _ := ExtractFileTimestamps(info)
	key := strings.ToLower(filepath.Clean(target))

	// Algoritmo atual xxhash: o hash sha256 gravado não pode ser reaproveitado.
	tm := NewTreeManager()
	s := NewScanner(tm)
	defer s.CloseLogger()
	s.SetQuickScanLookup(map[string]*FileNode{
		key: {Path: target, Size: 256, ModTime: modTime, Hash: "sha256:aaaabbbb", QuickHash: 99},
	})
	if err := s.StartScan(context.Background(), ScanConfig{
		Roots:         []string{root},
		QuickScanMode: true,
		HashAlgorithm: HashXXHash,
		LogDir:        logDirA,
	}, nil); err != nil {
		t.Fatal(err)
	}

	files := tm.GetAllFiles()
	if len(files) != 1 {
		t.Fatalf("esperava 1 arquivo, obtive %d", len(files))
	}
	if files[0].Hash != "" || files[0].IsReusedFromCache {
		t.Errorf("hash de outro algoritmo não deveria ser reaproveitado: %+v", files[0])
	}
	if got := s.GetStatus().ReusedFilesCount; got != 0 {
		t.Errorf("ReusedFilesCount = %d, quer 0", got)
	}
	if got := s.GetStatus().ModifiedFilesCount; got != 1 {
		t.Errorf("ModifiedFilesCount = %d, quer 1", got)
	}

	// Mesmo algoritmo: reaproveita.
	tm2 := NewTreeManager()
	s2 := NewScanner(tm2)
	defer s2.CloseLogger()
	s2.SetQuickScanLookup(map[string]*FileNode{
		key: {Path: target, Size: 256, ModTime: modTime, Hash: "xxh64:0123456789abcdef", QuickHash: 99},
	})
	if err := s2.StartScan(context.Background(), ScanConfig{
		Roots:         []string{root},
		QuickScanMode: true,
		HashAlgorithm: HashXXHash,
		LogDir:        logDirB,
	}, nil); err != nil {
		t.Fatal(err)
	}
	files2 := tm2.GetAllFiles()
	if len(files2) != 1 || files2[0].Hash != "xxh64:0123456789abcdef" || !files2[0].IsReusedFromCache {
		t.Errorf("hash do mesmo algoritmo deveria ser reaproveitado: %+v", files2)
	}
	if got := s2.GetStatus().ReusedFilesCount; got != 1 {
		t.Errorf("ReusedFilesCount = %d, quer 1", got)
	}
}

// Uma nova varredura fecha o logger da anterior (achado M5).
func TestStartScan_ClosesPreviousLogger(t *testing.T) {
	root := t.TempDir()
	logDir := t.TempDir()
	mkTree(t, root, map[string]int{"a.txt": 10})

	s := NewScanner(NewTreeManager())
	defer s.CloseLogger()

	cfg := ScanConfig{Roots: []string{root}, LogDir: logDir}
	if err := s.StartScan(context.Background(), cfg, nil); err != nil {
		t.Fatal(err)
	}
	first := s.DiskLogger
	firstPath := s.LoggerPath()

	if err := s.StartScan(context.Background(), cfg, nil); err != nil {
		t.Fatal(err)
	}
	if s.DiskLogger == first {
		t.Fatal("a segunda varredura deveria abrir um novo logger")
	}
	if !first.IsClosed() {
		t.Error("o logger da varredura anterior deveria ter sido fechado")
	}
	if data, err := os.ReadFile(firstPath); err != nil || !strings.Contains(string(data), "Fim da Sessão") {
		t.Errorf("o log anterior deveria ter sido finalizado em disco (err=%v)", err)
	}
}
