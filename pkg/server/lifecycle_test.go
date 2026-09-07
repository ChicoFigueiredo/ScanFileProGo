package server

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// isolateInstanceDir aponta %LOCALAPPDATA% para uma pasta temporária, para que
// nenhum teste leia ou apague o instance.json real do usuário.
func isolateInstanceDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("LOCALAPPDATA", dir)
	return dir
}

// newLifecycleServer cria um AppServer isolado com tolerância curta, para os
// testes de presença não esperarem os 10 s de produção.
func newLifecycleServer(t *testing.T, grace time.Duration) *AppServer {
	t.Helper()
	isolateInstanceDir(t)
	app := NewAppServer(testUIFS())
	app.presenceMu.Lock()
	st := app.lc()
	st.presenceGrace = grace
	st.shutdownFlushDelay = 0
	app.presenceMu.Unlock()
	t.Cleanup(app.Stop)
	return app
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("não foi possível reservar uma porta: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

func waitClosed(t *testing.T, ch <-chan struct{}, timeout time.Duration) bool {
	t.Helper()
	select {
	case <-ch:
		return true
	case <-time.After(timeout):
		return false
	}
}

// --- GET /api/instance -------------------------------------------------------

func TestInstanceIdentityRespondsWithoutToken(t *testing.T) {
	isolateInstanceDir(t)
	_, ts := newAuthedTestServer(t)

	resp, err := http.Get(ts.URL + "/api/instance")
	if err != nil {
		t.Fatalf("GET /api/instance falhou: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, quer 200 (a rota não pode exigir token)", resp.StatusCode)
	}
	var id InstanceIdentity
	if err := json.NewDecoder(resp.Body).Decode(&id); err != nil {
		t.Fatalf("resposta inválida: %v", err)
	}
	if id.App != "scanfile" {
		t.Errorf("app = %q, quer \"scanfile\"", id.App)
	}
	if id.PID != os.Getpid() {
		t.Errorf("pid = %d, quer %d", id.PID, os.Getpid())
	}
	if id.Version == "" {
		t.Error("version não pode vir vazia")
	}
}

func TestInstanceIdentityRejectsPost(t *testing.T) {
	isolateInstanceDir(t)
	_, ts := newAuthedTestServer(t)

	resp, err := http.Post(ts.URL+"/api/instance", "application/json", nil)
	if err != nil {
		t.Fatalf("POST falhou: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, quer 405", resp.StatusCode)
	}
}

// --- instance.json -----------------------------------------------------------

func TestInstanceFileWrittenOnStartAndRemovedOnStop(t *testing.T) {
	isolateInstanceDir(t)
	app := NewAppServer(testUIFS())
	app.sessionToken = "token-de-teste"

	url, err := app.Start(freePort(t))
	if err != nil {
		t.Fatalf("Start falhou: %v", err)
	}
	if !strings.HasPrefix(url, "http://127.0.0.1:") {
		t.Errorf("URL = %q, quer um endereço de loopback", url)
	}

	info, err := ReadInstanceFile()
	if err != nil {
		t.Fatalf("instance.json não foi criado: %v", err)
	}
	if info.Port != app.Port() {
		t.Errorf("port = %d, quer %d", info.Port, app.Port())
	}
	if info.PID != os.Getpid() {
		t.Errorf("pid = %d, quer %d", info.PID, os.Getpid())
	}
	if info.Token != "token-de-teste" {
		t.Errorf("token = %q, quer o token da Sessão", info.Token)
	}

	app.Stop()
	if _, err := os.Stat(InstanceFilePath()); !os.IsNotExist(err) {
		t.Errorf("instance.json deveria sumir no Stop, erro do Stat = %v", err)
	}
}

func TestRemoveInstanceFileKeepsFileOfAnotherProcess(t *testing.T) {
	isolateInstanceDir(t)
	app := NewAppServer(testUIFS())
	if err := WriteInstanceFile(InstanceInfo{Port: 47321, PID: os.Getpid() + 1000, Token: "x"}); err != nil {
		t.Fatalf("gravação falhou: %v", err)
	}

	app.Stop()

	if _, err := os.Stat(InstanceFilePath()); err != nil {
		t.Errorf("instance.json de outro processo (o filho do handoff) não pode ser apagado: %v", err)
	}
}

// --- Start: porta e timeouts -------------------------------------------------

func TestStartDropsWriteTimeoutAndSetsHeaderAndIdleTimeouts(t *testing.T) {
	isolateInstanceDir(t)
	app := NewAppServer(testUIFS())
	t.Cleanup(app.Stop)

	if _, err := app.Start(freePort(t)); err != nil {
		t.Fatalf("Start falhou: %v", err)
	}

	if app.httpServer.WriteTimeout != 0 {
		t.Errorf("WriteTimeout = %v, quer 0 (H2: SSE não pode ser cortado)", app.httpServer.WriteTimeout)
	}
	if app.httpServer.ReadHeaderTimeout != 15*time.Second {
		t.Errorf("ReadHeaderTimeout = %v, quer 15s", app.httpServer.ReadHeaderTimeout)
	}
	if app.httpServer.IdleTimeout != 120*time.Second {
		t.Errorf("IdleTimeout = %v, quer 120s", app.httpServer.IdleTimeout)
	}
}

func TestStartFallsBackWhenPortIsHeldByForeignProcess(t *testing.T) {
	isolateInstanceDir(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listener falso falhou: %v", err)
	}
	defer func() { _ = ln.Close() }()
	busy := ln.Addr().(*net.TCPAddr).Port
	foreign := &http.Server{Handler: http.NotFoundHandler(), ReadHeaderTimeout: time.Second}
	go func() { _ = foreign.Serve(ln) }()
	defer func() { _ = foreign.Close() }()

	app := NewAppServer(testUIFS())
	t.Cleanup(app.Stop)
	if _, err := app.Start(busy); err != nil {
		t.Fatalf("Start deveria cair numa porta livre, deu erro: %v", err)
	}
	if app.Port() == busy {
		t.Fatalf("Start assumiu a porta ocupada %d", busy)
	}
	if app.Port() == 0 {
		t.Fatal("Start não registrou a porta efetiva")
	}
}

func TestStartRefusesWhenPortBelongsToAnotherScanFile(t *testing.T) {
	isolateInstanceDir(t)

	first := NewAppServer(testUIFS())
	t.Cleanup(first.Stop)
	if _, err := first.Start(freePort(t)); err != nil {
		t.Fatalf("primeira instância falhou: %v", err)
	}

	second := NewAppServer(testUIFS())
	_, err := second.Start(first.Port())
	if err == nil {
		t.Fatal("a segunda instância não podia subir na porta da primeira")
	}
	if err != ErrInstanceAlreadyRunning {
		t.Fatalf("erro = %v, quer ErrInstanceAlreadyRunning", err)
	}
}

func TestProbeInstanceAndFocusRoundTrip(t *testing.T) {
	isolateInstanceDir(t)

	app := NewAppServer(testUIFS())
	t.Cleanup(app.Stop)
	var opened atomic.Int32
	app.SetWindowOpener(func(string) { opened.Add(1) })

	if _, err := app.Start(freePort(t)); err != nil {
		t.Fatalf("Start falhou: %v", err)
	}

	id, ok := ProbeInstance(app.Port(), 2*time.Second)
	if !ok {
		t.Fatal("ProbeInstance não reconheceu a própria instância")
	}
	if id.PID != os.Getpid() {
		t.Errorf("pid = %d, quer %d", id.PID, os.Getpid())
	}

	info, err := ReadInstanceFile()
	if err != nil {
		t.Fatalf("instance.json: %v", err)
	}
	if err := FocusInstance(info, 3*time.Second); err != nil {
		t.Fatalf("FocusInstance falhou: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for opened.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if opened.Load() == 0 {
		t.Error("o foco não reabriu a Janela")
	}
}

func TestProbeInstanceRejectsForeignServer(t *testing.T) {
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"app":"outro-app","version":"9","pid":1}`))
	}))
	defer foreign.Close()

	port := foreign.Listener.Addr().(*net.TCPAddr).Port
	if _, ok := ProbeInstance(port, 2*time.Second); ok {
		t.Error("um servidor alheio não pode ser confundido com uma instância nossa")
	}
	if _, ok := ProbeInstance(0, time.Second); ok {
		t.Error("porta 0 não é uma instância")
	}
}

// --- Presença e desligamento -------------------------------------------------

func TestPresenceSchedulesShutdownWhenLastClientLeaves(t *testing.T) {
	app := newLifecycleServer(t, 30*time.Millisecond)

	app.clientConnected()
	time.Sleep(60 * time.Millisecond)
	select {
	case <-app.Done():
		t.Fatal("com cliente conectado o servidor não pode encerrar")
	default:
	}

	app.clientDisconnected()
	if !waitClosed(t, app.Done(), 2*time.Second) {
		t.Fatal("zero clientes por mais que a tolerância deveria encerrar o servidor")
	}
}

func TestPresenceDefersShutdownWhileScanIsRunning(t *testing.T) {
	app := newLifecycleServer(t, 20*time.Millisecond)

	var scanning atomic.Bool
	scanning.Store(true)
	app.SetScanActiveFunc(scanning.Load)

	app.clientConnected()
	app.clientDisconnected()

	if waitClosed(t, app.Done(), 300*time.Millisecond) {
		t.Fatal("com Varredura em curso o desligamento tem de ser adiado")
	}
	app.presenceMu.Lock()
	pending := app.shutdownWhenDone
	app.presenceMu.Unlock()
	if !pending {
		t.Error("shutdownWhenDone deveria estar marcado")
	}

	scanning.Store(false)
	app.onScanFinished()
	if !waitClosed(t, app.Done(), 2*time.Second) {
		t.Fatal("terminada a Varredura, o desligamento adiado deveria acontecer")
	}
}

func TestNoWindowDisablesPresenceShutdown(t *testing.T) {
	app := newLifecycleServer(t, 20*time.Millisecond)
	app.SetNoWindow(true)

	app.clientConnected()
	app.clientDisconnected()
	app.markClientGone()

	if waitClosed(t, app.Done(), 300*time.Millisecond) {
		t.Fatal("--no-window não pode agendar desligamento por ausência")
	}
	app.presenceMu.Lock()
	timer := app.lc().presenceTimer
	app.presenceMu.Unlock()
	if timer != nil {
		t.Error("--no-window não pode nem armar o temporizador de presença")
	}
}

func TestUIClosedMarksAbsenceImmediately(t *testing.T) {
	app := newLifecycleServer(t, 30*time.Millisecond)
	ts := httptest.NewServer(authedHandler(app))
	defer ts.Close()

	app.clientConnected() // a Janela está aberta

	resp, err := http.Post(ts.URL+"/api/ui/closed?token=qualquer", "text/plain", nil)
	if err != nil {
		t.Fatalf("POST /api/ui/closed falhou: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, quer 200", resp.StatusCode)
	}

	if !waitClosed(t, app.Done(), 2*time.Second) {
		t.Fatal("o beacon de fechamento deveria levar ao desligamento")
	}
}

func TestClientReconnectCancelsPendingShutdown(t *testing.T) {
	app := newLifecycleServer(t, 150*time.Millisecond)

	app.clientConnected()
	app.clientDisconnected()
	time.Sleep(20 * time.Millisecond)
	app.clientConnected() // a Janela voltou antes da tolerância acabar

	if waitClosed(t, app.Done(), 400*time.Millisecond) {
		t.Fatal("uma reconexão dentro da tolerância cancela o desligamento")
	}
}

func TestFocusCancelsPendingShutdown(t *testing.T) {
	app := newLifecycleServer(t, 150*time.Millisecond)
	ts := httptest.NewServer(authedHandler(app))
	defer ts.Close()

	app.markClientGone()
	time.Sleep(20 * time.Millisecond)

	resp, err := http.Post(ts.URL+"/api/instance/focus", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /api/instance/focus falhou: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, quer 200", resp.StatusCode)
	}

	if waitClosed(t, app.Done(), 400*time.Millisecond) {
		t.Fatal("o foco devolve presença e cancela o desligamento agendado")
	}
}

// --- SSE: presença ponta a ponta e evento de shutdown ------------------------

func TestSSEConnectionCountsAsPresenceAndReleasesOnClose(t *testing.T) {
	app := newLifecycleServer(t, 40*time.Millisecond)
	ts := httptest.NewServer(authedHandler(app))
	defer ts.Close()

	// handleSSE só escreve no primeiro evento, então a resposta não chega antes
	// disso: a requisição vive numa goroutine.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/events", nil)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	deadline := time.Now().Add(3 * time.Second)
	for {
		app.presenceMu.Lock()
		n := app.sseCount
		app.presenceMu.Unlock()
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("sseCount = %d, quer 1 com um EventSource conectado", n)
		}
		time.Sleep(5 * time.Millisecond)
	}

	select {
	case <-app.Done():
		cancel()
		t.Fatal("com um EventSource conectado o servidor não encerra")
	case <-time.After(120 * time.Millisecond):
	}

	cancel()
	<-finished

	if !waitClosed(t, app.Done(), 3*time.Second) {
		t.Fatal("fechada a última conexão SSE, o servidor deveria encerrar")
	}
}

func TestShutdownBroadcastsSSEEventBeforeStopping(t *testing.T) {
	app := newLifecycleServer(t, time.Hour) // sem desligamento automático
	app.presenceMu.Lock()
	app.lc().shutdownFlushDelay = 150 * time.Millisecond
	app.presenceMu.Unlock()

	ts := httptest.NewServer(authedHandler(app))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/events", nil)

	lines := make(chan string, 32)
	go func() {
		defer close(lines)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return
		}
		defer func() { _ = resp.Body.Close() }()
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			select {
			case lines <- sc.Text():
			default:
			}
		}
	}()

	// Espera o cliente estar registrado antes de emitir o evento.
	deadline := time.Now().Add(3 * time.Second)
	for {
		app.presenceMu.Lock()
		n := app.sseCount
		app.presenceMu.Unlock()
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("sseCount = %d, quer 1", n)
		}
		time.Sleep(5 * time.Millisecond)
	}

	go app.shutdownNow("no_clients")

	var sawEvent, sawReason bool
	timeout := time.After(3 * time.Second)
	for !(sawEvent && sawReason) {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatal("a conexão SSE fechou antes do evento shutdown")
			}
			if line == "event: shutdown" {
				sawEvent = true
			}
			if strings.HasPrefix(line, "data: ") && strings.Contains(line, "no_clients") {
				sawReason = true
				if !strings.Contains(line, "inSeconds") {
					t.Errorf("payload sem inSeconds: %s", line)
				}
			}
		case <-timeout:
			t.Fatalf("evento shutdown não chegou (event=%v reason=%v)", sawEvent, sawReason)
		}
	}
}

// --- Handoff de elevação -----------------------------------------------------

func TestHandoffArgsAppendsFlagExactlyOnce(t *testing.T) {
	got := handoffArgs([]string{"--admin", "--port", "47321"})
	want := []string{"--admin", "--port", "47321", "--handoff"}
	if len(got) != len(want) {
		t.Fatalf("argumentos = %v, quer %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("argumentos = %v, quer %v", got, want)
		}
	}

	twice := handoffArgs(handoffArgs([]string{"--debug"}))
	count := 0
	for _, a := range twice {
		if a == "--handoff" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("--handoff apareceu %d vezes em %v", count, twice)
	}
}

func TestWaitForHandoffChildAcceptsAnnouncementOnOurPort(t *testing.T) {
	isolateInstanceDir(t)
	app := NewAppServer(testUIFS())
	t.Cleanup(app.Stop)
	if _, err := app.Start(freePort(t)); err != nil {
		t.Fatalf("Start falhou: %v", err)
	}

	// Nada anunciado ainda: o pai não pode desistir da porta.
	if app.waitForHandoffChild(0, 300*time.Millisecond) {
		t.Error("sem anúncio do filho o pai não pode encerrar")
	}

	child := os.Getpid() + 4242
	if err := WriteInstanceFile(InstanceInfo{Port: app.Port(), PID: child, Token: "t"}); err != nil {
		t.Fatalf("anúncio falhou: %v", err)
	}
	if !app.waitForHandoffChild(uint32(child), 2*time.Second) {
		t.Error("o anúncio do filho na nossa porta é o sinal de troca")
	}
	if app.waitForHandoffChild(uint32(child+1), 300*time.Millisecond) {
		t.Error("o anúncio de outro PID não vale para este handoff")
	}
}

func TestAnnounceHandoffWritesOwnPID(t *testing.T) {
	isolateInstanceDir(t)
	if err := AnnounceHandoff(47321, "tok"); err != nil {
		t.Fatalf("AnnounceHandoff falhou: %v", err)
	}
	info, err := ReadInstanceFile()
	if err != nil {
		t.Fatalf("leitura falhou: %v", err)
	}
	if info.PID != os.Getpid() || info.Port != 47321 || info.Token != "tok" {
		t.Errorf("instance.json = %+v", info)
	}
}

func TestAdoptSessionIgnoresEmptyToken(t *testing.T) {
	app := newLifecycleServer(t, time.Hour)
	app.sessionToken = "original"
	app.AdoptSession("")
	if app.sessionToken != "original" {
		t.Errorf("token = %q, quer o original preservado", app.sessionToken)
	}
	app.AdoptSession("herdado")
	if app.sessionToken != "herdado" {
		t.Errorf("token = %q, quer \"herdado\"", app.sessionToken)
	}
}

func TestStopIsIdempotent(t *testing.T) {
	app := newLifecycleServer(t, time.Hour)
	app.Stop()
	app.Stop()
	if !waitClosed(t, app.Done(), time.Second) {
		t.Error("Done deveria estar fechado após Stop")
	}
}

// --- Tolerância de partida ---------------------------------------------------

func TestStartupWithoutAnyWindowEventuallyShutsDown(t *testing.T) {
	isolateInstanceDir(t)
	app := NewAppServer(testUIFS())
	t.Cleanup(app.Stop)
	app.presenceMu.Lock()
	st := app.lc()
	st.startupGrace = 40 * time.Millisecond
	st.presenceGrace = 40 * time.Millisecond
	st.shutdownFlushDelay = 0
	app.presenceMu.Unlock()

	if _, err := app.Start(freePort(t)); err != nil {
		t.Fatalf("Start falhou: %v", err)
	}

	if !waitClosed(t, app.Done(), 3*time.Second) {
		t.Fatal("um servidor cuja Janela nunca conectou não pode ficar órfão")
	}
}

func TestStartupWithNoWindowFlagNeverShutsDownAlone(t *testing.T) {
	isolateInstanceDir(t)
	app := NewAppServer(testUIFS())
	t.Cleanup(app.Stop)
	app.SetNoWindow(true)
	app.presenceMu.Lock()
	st := app.lc()
	st.startupGrace = 20 * time.Millisecond
	st.presenceGrace = 20 * time.Millisecond
	st.shutdownFlushDelay = 0
	app.presenceMu.Unlock()

	if _, err := app.Start(freePort(t)); err != nil {
		t.Fatalf("Start falhou: %v", err)
	}

	if waitClosed(t, app.Done(), 300*time.Millisecond) {
		t.Fatal("--no-window mantém o servidor de pé sem nenhuma Janela")
	}
}
