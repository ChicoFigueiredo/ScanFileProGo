package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
	"weak"

	"scanfile/pkg/scanner"
)

// tempDir vive em handlers_files_test.go: cria uma pasta temporária apagada no
// fim do teste sem reprovar o teste se o Windows recusar a remoção (arquivos
// recém-fechados ficam em "delete pending" e o t.TempDir transforma isso em
// falha).

// newScanTestServer devolve um AppServer de teste com a pasta de Snapshots
// isolada e todas as tarefas de fundo encerradas ao final.
func newScanTestServer(t *testing.T) (*AppServer, *httptest.Server) {
	t.Helper()
	app, ts := newAuthedTestServer(t)
	app.savedScansDir = tempDir(t)
	t.Cleanup(app.StopBackground)
	return app, ts
}

func postJSON(t *testing.T, ts *httptest.Server, path string, body any) *http.Response {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("não foi possível serializar o corpo: %v", err)
		}
	}
	resp, err := ts.Client().Post(ts.URL+path, "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("POST %s falhou: %v", path, err)
	}
	return resp
}

func getJSON(t *testing.T, ts *httptest.Server, path string, out any) *http.Response {
	t.Helper()
	resp, err := ts.Client().Get(ts.URL + path)
	if err != nil {
		t.Fatalf("GET %s falhou: %v", path, err)
	}
	defer resp.Body.Close()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("GET %s devolveu JSON inválido: %v", path, err)
		}
	} else {
		_, _ = io.Copy(io.Discard, resp.Body)
	}
	return resp
}

// waitForPhase espera o status da Varredura chegar à fase pedida.
func waitForPhase(t *testing.T, app *AppServer, phase string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if app.currentPhase() == phase {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("a Varredura não chegou à fase %q em %s (fase atual: %q)", phase, timeout, app.currentPhase())
}

// waitScanIdle espera o ciclo da Varredura liberar a vaga.
func waitScanIdle(t *testing.T, app *AppServer, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		app.scanMu.Lock()
		done := app.scanDone
		app.scanMu.Unlock()
		if done == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("o ciclo da Varredura não terminou a tempo")
}

// scanRootWithFiles cria uma Raiz Varrida temporária com alguns arquivos.
func scanRootWithFiles(t *testing.T, count int) string {
	t.Helper()
	root := tempDir(t)
	for i := 0; i < count; i++ {
		name := filepath.Join(root, fmt.Sprintf("arquivo_%03d.bin", i))
		if err := os.WriteFile(name, bytes.Repeat([]byte{byte(i)}, 64), 0o644); err != nil {
			t.Fatalf("não foi possível criar o arquivo de teste: %v", err)
		}
	}
	return root
}

// testBarrier monta o seam de testes da orquestração. onPhase1, quando definido,
// segura a Fase 1 num ponto conhecido. O estágio de Monitoramento é sempre
// abortado: no Windows o observador segura descritores das pastas temporárias e
// a limpeza do t.TempDir falharia.
func testBarrier(app *AppServer, onPhase1 func()) func(string) {
	return func(stage string) {
		switch stage {
		case stagePhase1:
			if onPhase1 != nil {
				onPhase1()
			}
		case stageWatching:
			app.scanMu.Lock()
			cancel := app.scanCancel
			app.scanMu.Unlock()
			if cancel != nil {
				cancel()
			}
		}
	}
}

func TestStartScanDuringScanReturns409WithPhase(t *testing.T) {
	app, ts := newScanTestServer(t)

	reached := make(chan struct{})
	release := make(chan struct{})
	app.scanBarrier = testBarrier(app, func() {
		close(reached)
		<-release
	})

	root := scanRootWithFiles(t, 3)
	cfg := scanner.ScanConfig{Roots: []string{root}, WorkerThreads: 1, LogDir: tempDir(t)}

	resp := postJSON(t, ts, "/api/scan/start", cfg)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("primeiro start devolveu %d, esperado 200", resp.StatusCode)
	}

	<-reached

	second := postJSON(t, ts, "/api/scan/start", cfg)
	defer second.Body.Close()
	if second.StatusCode != http.StatusConflict {
		t.Fatalf("segundo start devolveu %d, esperado 409", second.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(second.Body).Decode(&body); err != nil {
		t.Fatalf("409 devolveu JSON inválido: %v", err)
	}
	if body["error"] != "scan_in_progress" {
		t.Errorf(`error = %q, esperado "scan_in_progress"`, body["error"])
	}
	if body["phase"] != PhaseMetadata {
		t.Errorf("phase = %q, esperado %q", body["phase"], PhaseMetadata)
	}

	close(release)
	waitScanIdle(t, app, 10*time.Second)
	// O Monitoramento segura um descritor da raiz temporária; desligá-lo aqui
	// deixa a limpeza do t.TempDir apagar a pasta.
	app.StopBackground()
}

func TestCancelScanEndsCancelledAndSkipsAutosave(t *testing.T) {
	app, ts := newScanTestServer(t)

	reached := make(chan struct{})
	release := make(chan struct{})
	app.scanBarrier = testBarrier(app, func() {
		close(reached)
		<-release
	})

	root := scanRootWithFiles(t, 5)
	resp := postJSON(t, ts, "/api/scan/start", scanner.ScanConfig{Roots: []string{root}, WorkerThreads: 1, LogDir: tempDir(t)})
	resp.Body.Close()

	<-reached

	start := time.Now()
	cancelResp := postJSON(t, ts, "/api/scan/cancel", nil)
	var cancelBody map[string]string
	if err := json.NewDecoder(cancelResp.Body).Decode(&cancelBody); err != nil {
		t.Fatalf("cancel devolveu JSON inválido: %v", err)
	}
	cancelResp.Body.Close()
	if cancelBody["status"] != "cancelling" {
		t.Errorf(`status do cancel = %q, esperado "cancelling"`, cancelBody["status"])
	}
	if got := app.currentPhase(); got != PhaseCancelling {
		t.Errorf("fase logo após o cancel = %q, esperado %q", got, PhaseCancelling)
	}

	close(release)
	waitForPhase(t, app, PhaseCancelled, 2*time.Second)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("o Cancelamento levou %s, o teto é 2s", elapsed)
	}
	waitScanIdle(t, app, 2*time.Second)

	// Nenhum Autosave é gravado ao cancelar (contrato 1.2).
	if _, err := os.Stat(filepath.Join(app.savedScansDir, scanner.DefaultAutoSaveFileName)); !os.IsNotExist(err) {
		t.Errorf("um Autosave foi gravado durante o Cancelamento (err=%v)", err)
	}

	// E nada mais roda depois: nem o Autosave periódico.
	if app.maybeAutoSave(time.Now().Add(time.Hour)) {
		t.Error("o Autosave periódico gravou depois do Cancelamento")
	}

	// O status final continua sendo cancelled e traz os Itens Pulados.
	var status scanner.ScanStatus
	getJSON(t, ts, "/api/scan/status", &status)
	if status.Phase != PhaseCancelled {
		t.Errorf("phase no status = %q, esperado %q", status.Phase, PhaseCancelled)
	}
	if status.SkippedCount < 0 {
		t.Errorf("skippedCount inválido: %d", status.SkippedCount)
	}
}

func TestFullScanReportsWorkerThreads(t *testing.T) {
	app, ts := newScanTestServer(t)
	app.scanBarrier = testBarrier(app, nil)

	root := scanRootWithFiles(t, 4)
	resp := postJSON(t, ts, "/api/scan/start", scanner.ScanConfig{Roots: []string{root}, WorkerThreads: 8, LogDir: tempDir(t)})
	resp.Body.Close()

	waitScanIdle(t, app, 20*time.Second)
	app.StopBackground()

	var status scanner.ScanStatus
	getJSON(t, ts, "/api/scan/status", &status)
	if status.Phase1Workers != scanner.ResolveWorkers(8, scanner.PhaseMetadata) {
		t.Errorf("phase1Workers = %d, esperado %d", status.Phase1Workers, scanner.ResolveWorkers(8, scanner.PhaseMetadata))
	}
	if status.Phase2Workers != scanner.ResolveWorkers(8, scanner.PhaseHashing) {
		t.Errorf("phase2Workers = %d, esperado %d", status.Phase2Workers, scanner.ResolveWorkers(8, scanner.PhaseHashing))
	}
	if status.TotalFilesScanned != 4 {
		t.Errorf("totalFilesScanned = %d, esperado 4", status.TotalFilesScanned)
	}
}

// fillDir insere `count` arquivos sintéticos numa pasta da árvore.
func fillDir(t *testing.T, app *AppServer, dir string, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		app.Tree().AddFile(scanner.NewFileNodeAt(filepath.Join(dir, fmt.Sprintf("f%04d.bin", i)), scanner.FileMeta{Name: fmt.Sprintf("f%04d.bin", i), Size: int64(i + 1), Extension: ".bin", ModTime: int64(1700000000 + i)}))
	}
}

func TestTreeNeverReturnsMoreThan500Files(t *testing.T) {
	app, ts := newScanTestServer(t)

	dir := filepath.Join(tempDir(t), "grande")
	fillDir(t, app, dir, 600)

	var summary scanner.DirSummary
	getJSON(t, ts, "/api/tree?path="+urlValue(dir)+"&depth=1", &summary)

	if len(summary.Files) != scanner.DefaultSummaryMaxFiles {
		t.Errorf("a árvore devolveu %d arquivos, o teto é %d", len(summary.Files), scanner.DefaultSummaryMaxFiles)
	}
	if summary.DirectFileCount != 600 {
		t.Errorf("directFileCount = %d, esperado 600", summary.DirectFileCount)
	}
	if summary.FileCount != 600 {
		t.Errorf("fileCount = %d, esperado 600", summary.FileCount)
	}
	// Os 500 devolvidos são os maiores.
	if summary.Files[0].Size != 600 {
		t.Errorf("o primeiro arquivo tem %d bytes, esperado o maior (600)", summary.Files[0].Size)
	}
}

func TestTreeFilesPaginatesAndCapsAt500(t *testing.T) {
	app, ts := newScanTestServer(t)

	dir := filepath.Join(tempDir(t), "paginada")
	fillDir(t, app, dir, 600)

	var page TreeFilesPage
	getJSON(t, ts, "/api/tree/files?path="+urlValue(dir)+"&offset=0&limit=100", &page)
	if page.Total != 600 {
		t.Errorf("total = %d, esperado 600", page.Total)
	}
	if len(page.Files) != 100 {
		t.Errorf("primeira página trouxe %d arquivos, esperado 100", len(page.Files))
	}
	if page.SortBy != scanner.SortSizeDesc {
		t.Errorf("sortBy = %q, esperado %q", page.SortBy, scanner.SortSizeDesc)
	}

	var last TreeFilesPage
	getJSON(t, ts, "/api/tree/files?path="+urlValue(dir)+"&offset=550&limit=100", &last)
	if len(last.Files) != 50 {
		t.Errorf("última página trouxe %d arquivos, esperado 50", len(last.Files))
	}

	var capped TreeFilesPage
	getJSON(t, ts, "/api/tree/files?path="+urlValue(dir)+"&offset=0&limit=5000", &capped)
	if capped.Limit != scanner.MaxFilesPageLimit {
		t.Errorf("limit = %d, o teto é %d", capped.Limit, scanner.MaxFilesPageLimit)
	}
	if len(capped.Files) != scanner.MaxFilesPageLimit {
		t.Errorf("a página trouxe %d arquivos, o teto é %d", len(capped.Files), scanner.MaxFilesPageLimit)
	}

	var byName TreeFilesPage
	getJSON(t, ts, "/api/tree/files?path="+urlValue(dir)+"&limit=3&sortBy=name_asc", &byName)
	if len(byName.Files) != 3 || byName.Files[0].Name() != "f0000.bin" {
		t.Errorf("ordenação por nome falhou: %+v", byName.Files)
	}

	resp := getJSON(t, ts, "/api/tree/files", nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("sem 'path' devolveu %d, esperado 400", resp.StatusCode)
	}
}

func TestSkippedLogsEndpoint(t *testing.T) {
	_, ts := newScanTestServer(t)

	var entries []scanner.SkippedEntry
	resp := getJSON(t, ts, "/api/logs/skipped?limit=200", &entries)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/api/logs/skipped devolveu %d, esperado 200", resp.StatusCode)
	}
	if entries == nil {
		t.Error("a resposta precisa ser uma lista, nunca null")
	}
}

func TestSkippedLogsRespectLimit(t *testing.T) {
	app, ts := newScanTestServer(t)
	app.scanBarrier = testBarrier(app, nil)

	// Uma Varredura numa raiz inexistente registra o erro e mantém a lista viva;
	// aqui basta provar o recorte do parâmetro limit sobre o anel do motor.
	root := scanRootWithFiles(t, 1)
	resp := postJSON(t, ts, "/api/scan/start", scanner.ScanConfig{Roots: []string{root}, WorkerThreads: 1, LogDir: tempDir(t)})
	resp.Body.Close()
	waitScanIdle(t, app, 20*time.Second)
	// O Monitoramento segura um descritor da raiz temporária; desligá-lo aqui
	// deixa a limpeza do t.TempDir apagar a pasta.
	app.StopBackground()

	var entries []scanner.SkippedEntry
	getJSON(t, ts, "/api/logs/skipped?limit=1", &entries)
	if len(entries) > 1 {
		t.Errorf("limit=1 devolveu %d entradas", len(entries))
	}
}

// TestFullScanEntersWatchingPhase prova o caminho completo, com Monitoramento.
// A raiz é criada ANTES do servidor para que a limpeza do t.TempDir rode depois
// de StopBackground fechar o observador.
func TestFullScanEntersWatchingPhase(t *testing.T) {
	root := tempDir(t)
	if err := os.WriteFile(filepath.Join(root, "a.bin"), []byte("conteudo"), 0o644); err != nil {
		t.Fatalf("não foi possível criar o arquivo de teste: %v", err)
	}

	app, ts := newScanTestServer(t)
	resp := postJSON(t, ts, "/api/scan/start", scanner.ScanConfig{Roots: []string{root}, WorkerThreads: 1, LogDir: tempDir(t)})
	resp.Body.Close()

	waitScanIdle(t, app, 20*time.Second)
	waitForPhase(t, app, PhaseWatching, 5*time.Second)
	if app.currentWatcher() == nil {
		t.Error("o Monitoramento não subiu ao fim da Varredura")
	}
	app.StopBackground()
	if app.currentWatcher() != nil {
		t.Error("StopBackground não desligou o Monitoramento")
	}
}

// urlValue escapa um caminho do Windows para uso em query string.
func urlValue(v string) string {
	out := make([]byte, 0, len(v)*3)
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_', c == '.', c == '~':
			out = append(out, c)
		default:
			out = append(out, '%', "0123456789ABCDEF"[c>>4], "0123456789ABCDEF"[c&0x0f])
		}
	}
	return string(out)
}

// TestScanStateHasNoDataRaceBetweenReadersAndWriters exercita, ao mesmo tempo,
// quem escreve a árvore ativa, as Raízes Varridas e a última Configuração
// (iniciar Varredura, carregar Snapshot) e quem as lê de outras goroutines (o
// relógio do Autosave, a Reciclagem e a gravação de Snapshot). Com -race, uma
// escrita sem lock reprova o teste.
func TestScanStateHasNoDataRaceBetweenReadersAndWriters(t *testing.T) {
	root := scanRootWithFiles(t, 2)
	app, ts := newScanTestServer(t)
	app.scanBarrier = testBarrier(app, nil)

	snapshotRoot := tempDir(t)
	snapshot := filepath.Join(app.savedScansDir, "concorrente.scanfile.gz")
	if err := scanner.SaveCacheToFile(buildSnapshotTree(t, snapshotRoot), []string{snapshotRoot}, scanner.ScanConfig{Roots: []string{snapshotRoot}}, snapshot); err != nil {
		t.Fatalf("não foi possível gravar o Snapshot de teste: %v", err)
	}

	// MinSizeForHash acima do tamanho dos arquivos deixa a Fase 2 sem nenhum
	// candidato. Não é conveniência: com o Hash Completo em curso, serializar um
	// Snapshot corre contra FileNode.ensureDigest em pkg/scanner, um defeito
	// alheio a este pacote que abafaria o que este teste mede.
	cfg := scanner.ScanConfig{Roots: []string{root}, WorkerThreads: 1, LogDir: tempDir(t), MinSizeForHash: 1 << 30}

	post := func(path string, body any) {
		payload, err := json.Marshal(body)
		if err != nil {
			return
		}
		resp, err := ts.Client().Post(ts.URL+path, "application/json", bytes.NewReader(payload))
		if err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup

	readers := []func(){
		func() { _ = app.scanRoots() },
		func() { app.writeAutoSave(cfg, time.Now()) },
		func() { post("/api/cache/save", SaveCacheReq{FileName: "leitura.scanfile.gz"}) },
	}
	for _, read := range readers {
		wg.Add(1)
		go func(read func()) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				read()
				time.Sleep(time.Millisecond)
			}
		}(read)
	}

	for i := 0; i < 30; i++ {
		post("/api/scan/start", cfg)
		post("/api/cache/load", LoadCacheReq{FilePath: snapshot})
	}
	close(stop)
	wg.Wait()

	waitScanIdle(t, app, 30*time.Second)
	app.StopBackground()
}

// heapAllocBytes devolve o heap vivo depois de uma coleta.
func heapAllocBytes() uint64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

// weakFileRef guarda uma referência fraca a um nó da árvore. O nó só continua
// vivo se alguém no processo ainda o segurar de verdade.
func weakFileRef(t *testing.T, app *AppServer, path string) weak.Pointer[scanner.FileNode] {
	t.Helper()
	node := app.Tree().FindFile(path)
	if node == nil {
		t.Fatalf("o arquivo %s não entrou na árvore anterior", path)
	}
	return weak.Make(node)
}

// TestQuickScanLookupIsReleasedAfterScan prova que o índice do Quick Scan, que
// sai da árvore anterior, é liberado ao fim da Varredura. Preso no motor, ele
// segura a árvore anterior inteira — até 50 milhões de nós — pelo resto da
// sessão, desfazendo parte do ganho do ADR-0001.
func TestQuickScanLookupIsReleasedAfterScan(t *testing.T) {
	root := tempDir(t)
	if err := os.WriteFile(filepath.Join(root, "novo.bin"), []byte("conteudo"), 0o644); err != nil {
		t.Fatalf("não foi possível criar o arquivo da nova Varredura: %v", err)
	}

	app, ts := newScanTestServer(t)
	app.scanBarrier = testBarrier(app, nil)

	const previousFiles = 50000
	previousDir := filepath.Join(tempDir(t), "anterior")
	emptyHeap := heapAllocBytes()
	fillDir(t, app, previousDir, previousFiles)

	withPreviousTree := heapAllocBytes()
	held := weakFileRef(t, app, filepath.Join(previousDir, "f0000.bin"))

	resp := postJSON(t, ts, "/api/scan/start", scanner.ScanConfig{
		Roots:         []string{root},
		WorkerThreads: 1,
		LogDir:        tempDir(t),
		QuickScanMode: true,
	})
	resp.Body.Close()

	waitScanIdle(t, app, 30*time.Second)
	app.StopBackground()

	runtime.GC()
	runtime.GC()

	afterScan := heapAllocBytes()
	mb := func(v uint64) float64 { return float64(v) / (1 << 20) }
	t.Logf("heap com %d nós: %.1f MB vazio, %.1f MB com a árvore anterior, %.1f MB depois da Varredura (%.0f bytes por nó liberados)",
		previousFiles, mb(emptyHeap), mb(withPreviousTree), mb(afterScan),
		(float64(withPreviousTree)-float64(afterScan))/previousFiles)

	if held.Value() != nil {
		t.Error("o índice do Quick Scan continuou preso ao motor, segurando a árvore anterior inteira")
	}
}

// TestRescanRootRestoresPhaseWhenScanFails prova que uma revarredura de raiz que
// não chega ao fim devolve a fase a um estado coerente. Presa em
// "phase1_metadata", toda Varredura e todo carregamento de Snapshot passariam a
// responder 409 até o usuário apertar Cancelar.
func TestRescanRootRestoresPhaseWhenScanFails(t *testing.T) {
	root := scanRootWithFiles(t, 2)
	app, ts := newScanTestServer(t)

	// Estado de partida: Monitoramento ativo, que é de onde vem o estouro do
	// buffer de notificações.
	app.setScanState([]string{root}, scanner.ScanConfig{Roots: []string{root}, WorkerThreads: 1, LogDir: tempDir(t)})
	app.setPhase(PhaseWatching, "")

	// O seam aborta a revarredura antes da Fase 1: StartScan publica
	// "phase1_metadata" e devolve context.Canceled em seguida.
	app.scanBarrier = func(stage string) {
		if stage != stageRescan {
			return
		}
		app.scanMu.Lock()
		cancel := app.scanCancel
		app.scanMu.Unlock()
		if cancel != nil {
			cancel()
		}
	}

	app.rescanRoot(root)
	waitScanIdle(t, app, 20*time.Second)

	if phase := app.currentPhase(); isBusyPhase(phase) {
		t.Errorf("a fase ficou presa em %q depois de a revarredura falhar", phase)
	}

	// E a interface volta a aceitar uma Varredura nova.
	app.scanBarrier = testBarrier(app, nil)
	resp := postJSON(t, ts, "/api/scan/start", scanner.ScanConfig{Roots: []string{root}, WorkerThreads: 1, LogDir: tempDir(t), MinSizeForHash: 1 << 30})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("iniciar Varredura depois da revarredura falha devolveu %d, esperado 200", resp.StatusCode)
	}
	waitScanIdle(t, app, 30*time.Second)
	app.StopBackground()
}
