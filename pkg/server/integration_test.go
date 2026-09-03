package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"scanfile/pkg/scanner"
)

// Estes testes cobrem as amarrações entre as áreas do servidor — nenhum dos
// agentes que escreveram auth.go, handlers_scan.go e lifecycle.go podia
// exercitá-las sozinho, porque cada um só editava a sua metade.

// TestStopAlsoStopsBackgroundTasks garante que encerrar o servidor encerra
// também Varredura, Monitoramento, relógio do Autosave e o log de erros em
// disco. Sem isso o log da última Varredura nunca é finalizado (achado M5).
func TestStopAlsoStopsBackgroundTasks(t *testing.T) {
	isolateInstanceDir(t)
	app := NewAppServer(testUIFS())
	app.savedScansDir = tempDir(t)

	app.Stop()

	app.watcherMu.Lock()
	stopped := app.bgStopped
	app.watcherMu.Unlock()
	if !stopped {
		t.Error("Stop deveria encerrar as tarefas de fundo (StopBackground)")
	}
}

// TestScanCompletionTriggersDeferredShutdown prova o contrato 1.9 com Q33: a
// Janela fecha durante a Varredura, o desligamento fica adiado, e é o fim da
// Varredura — não um relógio — que encerra o processo.
func TestScanCompletionTriggersDeferredShutdown(t *testing.T) {
	app := newLifecycleServer(t, 20*time.Millisecond)
	app.savedScansDir = tempDir(t)
	ts := newAuthedHTTPServer(t, app)

	// Enquanto a Varredura roda, a ausência da Janela só marca a intenção.
	scanRunning := make(chan struct{})
	release := make(chan struct{})
	app.scanBarrier = func(stage string) {
		switch stage {
		case stagePhase1:
			close(scanRunning)
			<-release
		case stageWatching:
			app.scanMu.Lock()
			cancel := app.scanCancel
			app.scanMu.Unlock()
			if cancel != nil {
				cancel()
			}
		}
	}

	root := tempDir(t)
	if err := os.WriteFile(filepath.Join(root, "a.bin"), []byte("conteudo"), 0o600); err != nil {
		t.Fatalf("não foi possível criar o arquivo de teste: %v", err)
	}

	resp := postJSON(t, ts, "/api/scan/start", scanner.ScanConfig{
		Roots: []string{root}, WorkerThreads: 1, LogDir: tempDir(t),
	})
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("start devolveu %d, esperado 200", resp.StatusCode)
	}
	<-scanRunning

	// A Janela fecha no meio da Varredura.
	app.markClientGone()

	select {
	case <-app.Done():
		close(release)
		t.Fatal("com Varredura em curso o desligamento deve ser adiado, não imediato")
	case <-time.After(150 * time.Millisecond):
	}

	app.presenceMu.Lock()
	deferred := app.shutdownWhenDone
	app.presenceMu.Unlock()
	if !deferred {
		t.Error("a ausência durante a Varredura deveria marcar o desligamento adiado")
	}

	// Terminada a Varredura, o processo encerra sozinho.
	close(release)
	if !waitClosed(t, app.Done(), 20*time.Second) {
		t.Fatal("terminada a Varredura, o desligamento adiado deveria acontecer")
	}
}

// TestInstanceFileCarriesUsableToken protege a regressão que a fusão expôs: o
// arquivo de instância gravava o campo cru do token, ainda vazio no momento do
// Start, e a instância seguinte tinha o foco recusado com 401.
func TestInstanceFileCarriesUsableToken(t *testing.T) {
	isolateInstanceDir(t)
	app := NewAppServer(testUIFS())
	t.Cleanup(app.Stop)

	if _, err := app.Start(freePort(t)); err != nil {
		t.Fatalf("Start falhou: %v", err)
	}

	info, err := ReadInstanceFile()
	if err != nil {
		t.Fatalf("instance.json: %v", err)
	}
	if info.Token == "" {
		t.Fatal("instance.json foi gravado sem o token da Sessão")
	}
	if info.Token != app.token() {
		t.Errorf("token do arquivo = %q, esperado o da Sessão", info.Token)
	}
}

// newAuthedHTTPServer é como newAuthedTestServer, mas para um AppServer já
// construído pelo teste.
func newAuthedHTTPServer(t *testing.T, app *AppServer) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(authedHandler(app))
	t.Cleanup(func() {
		ts.Close()
		app.StopBackground()
	})
	return ts
}
