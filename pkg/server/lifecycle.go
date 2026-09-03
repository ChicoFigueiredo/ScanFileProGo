package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"scanfile/pkg/config"
	"scanfile/pkg/privileges"
)

// Version vive em handlers_files.go: é a mesma variável usada por
// GET /api/instance e por GET /api/system/info, preenchida pelo main a partir
// das flags de build.

const (
	// DefaultPresenceGrace é a janela de tolerância sem nenhum cliente
	// conectado antes de o servidor decidir encerrar (contrato 1.9).
	DefaultPresenceGrace = 10 * time.Second

	// DefaultStartupGrace é a tolerância na partida, antes de a primeira
	// Janela conectar: abrir o Edge frio leva bem mais que os 10 s de regime.
	// Sem ela, um servidor cuja Janela nunca abriu ficaria órfão para sempre.
	DefaultStartupGrace = 60 * time.Second

	// defaultShutdownFlushDelay dá tempo ao evento SSE "shutdown" de chegar às
	// Janelas abertas antes de o listener fechar.
	defaultShutdownFlushDelay = 400 * time.Millisecond

	// handoffBindTimeout é quanto o filho elevado espera o pai liberar a porta.
	handoffBindTimeout = 120 * time.Second

	// HandoffWaitTimeout é quanto o pai espera o filho elevado se anunciar.
	HandoffWaitTimeout = 120 * time.Second

	// appIdentity identifica o processo em GET /api/instance.
	appIdentity = "scanfile"
)

// ErrInstanceAlreadyRunning indica que a porta pedida já é servida por outra
// instância do ScanFile Pro; quem chama deve focar aquela Janela e encerrar.
var ErrInstanceAlreadyRunning = errors.New("outra instância do ScanFile Pro já está em execução nesta porta")

// activeScanPhases são as fases em que a Varredura ainda está em curso
// (contrato 1.2); o desligamento por ausência espera todas elas terminarem.
var activeScanPhases = map[string]bool{
	"phase1_metadata": true,
	"phase2_hashing":  true,
	"indexing":        true,
	"cancelling":      true,
	"loading_cache":   true,
}

// InstanceInfo é o conteúdo de %LOCALAPPDATA%\ScanFile\instance.json.
type InstanceInfo struct {
	Port  int    `json:"port"`
	PID   int    `json:"pid"`
	Token string `json:"token"`
}

// InstanceIdentity é a resposta de GET /api/instance (sem token, contrato 1.1).
type InstanceIdentity struct {
	App     string `json:"app"`
	Version string `json:"version"`
	PID     int    `json:"pid"`
}

// ShutdownEvent é o payload do evento SSE "shutdown" (contrato 1.8).
type ShutdownEvent struct {
	Reason    string `json:"reason"`
	InSeconds int    `json:"inSeconds"`
}

// lifecycleState guarda o estado de ciclo de vida que ainda não tem campo em
// AppServer. server.go pertence ao Agente S2; enquanto os campos abaixo não
// forem promovidos para lá, eles vivem nesta tabela lateral por servidor.
// Todos os campos são protegidos por AppServer.presenceMu.
type lifecycleState struct {
	presenceGrace      time.Duration
	startupGrace       time.Duration
	shutdownFlushDelay time.Duration
	presenceTimer      *time.Timer
	windowOpener       func(url string)
	scanActive         func() bool
	handoff            bool
	url                string
	port               int
}

var lifecycleStates sync.Map // *AppServer -> *lifecycleState

// lc devolve (criando se preciso) o estado de ciclo de vida deste servidor.
func (s *AppServer) lc() *lifecycleState {
	if v, ok := lifecycleStates.Load(s); ok {
		return v.(*lifecycleState)
	}
	v, _ := lifecycleStates.LoadOrStore(s, &lifecycleState{
		presenceGrace:      DefaultPresenceGrace,
		startupGrace:       DefaultStartupGrace,
		shutdownFlushDelay: defaultShutdownFlushDelay,
	})
	return v.(*lifecycleState)
}

// ---------------------------------------------------------------------------
// Configuração injetável (usada pelo main e pelos testes)
// ---------------------------------------------------------------------------

// SetNoWindow liga o modo sem Janela: desativa todo o desligamento por
// presença e a reabertura da Janela (flag --no-window).
func (s *AppServer) SetNoWindow(v bool) {
	s.presenceMu.Lock()
	defer s.presenceMu.Unlock()
	s.noWindow = v
}

// SetWindowOpener instala a função que (re)abre a Janela. O main injeta o
// lançador do Edge; sem ela, POST /api/instance/focus apenas confirma presença.
func (s *AppServer) SetWindowOpener(fn func(url string)) {
	s.presenceMu.Lock()
	defer s.presenceMu.Unlock()
	s.lc().windowOpener = fn
}

// SetPresenceGrace ajusta a tolerância sem clientes antes do desligamento.
func (s *AppServer) SetPresenceGrace(d time.Duration) {
	if d <= 0 {
		return
	}
	s.presenceMu.Lock()
	defer s.presenceMu.Unlock()
	s.lc().presenceGrace = d
}

// SetScanActiveFunc substitui a detecção de "Varredura em curso". O padrão
// consulta o Scanner; os testes injetam um predicado determinístico.
func (s *AppServer) SetScanActiveFunc(fn func() bool) {
	s.presenceMu.Lock()
	defer s.presenceMu.Unlock()
	s.lc().scanActive = fn
}

// SetHandoffMode marca este processo como o filho elevado de um handoff: a
// porta é obrigatória (espera o pai liberá-la) e nenhuma Janela nova é aberta.
func (s *AppServer) SetHandoffMode(v bool) {
	s.presenceMu.Lock()
	defer s.presenceMu.Unlock()
	s.lc().handoff = v
}

// AdoptSession adota o token de Sessão de uma instância anterior (handoff de
// elevação): a Janela já aberta continua autenticada contra o novo processo.
//
// A escrita é delegada a SetSessionToken (auth.go), dona do campo e do mutex
// que o protege.
func (s *AppServer) AdoptSession(token string) {
	s.SetSessionToken(token)
}

// Port devolve a porta efetivamente escutada (0 antes de Start).
func (s *AppServer) Port() int {
	s.presenceMu.Lock()
	defer s.presenceMu.Unlock()
	return s.lc().port
}

// URL devolve a URL base do servidor ("" antes de Start).
func (s *AppServer) URL() string {
	s.presenceMu.Lock()
	defer s.presenceMu.Unlock()
	return s.lc().url
}

// Done fecha quando o servidor encerra, seja por Stop, por ausência da Janela
// ou pelo handoff de elevação. O main espera neste canal além do Ctrl+C.
func (s *AppServer) Done() <-chan struct{} {
	s.presenceMu.Lock()
	defer s.presenceMu.Unlock()
	if s.shutdownCh == nil {
		s.shutdownCh = make(chan struct{})
	}
	return s.shutdownCh
}

// ---------------------------------------------------------------------------
// Rotas e handler
// ---------------------------------------------------------------------------

// Handler builds the complete HTTP handler (routes + middlewares) without binding a listener.
// It is used by Start and by tests (httptest).
func (s *AppServer) Handler() http.Handler {
	mux := http.NewServeMux()

	s.registerScanRoutes(mux)
	s.registerFileRoutes(mux)
	s.registerLifecycleRoutes(mux)

	// Static UI assets (uiHandler vive em static.go, propriedade do Agente S1)
	mux.Handle("/", s.uiHandler())

	return s.authMiddleware(s.debugMiddleware(s.presenceMiddleware(mux)))
}

// registerLifecycleRoutes registers routes owned by the lifecycle (instance, elevation, presence).
func (s *AppServer) registerLifecycleRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/system/elevate", s.handleElevateProcess)
	mux.HandleFunc("/api/instance", s.handleInstanceIdentity)
	mux.HandleFunc("/api/instance/focus", s.handleInstanceFocus)
	mux.HandleFunc("/api/ui/closed", s.handleUIClosed)
}

// presenceMiddleware conta as conexões SSE como presença da Janela.
//
// NOTA DE INTEGRAÇÃO (S2, sse.go): a contagem já acontece aqui, então handleSSE
// NÃO precisa chamar clientConnected/clientDisconnected. Se preferir contar
// dentro de handleSSE (por exemplo só depois de escrever os cabeçalhos), chame
// o par de métodos lá e remova este wrap — a contagem em dobro não quebraria a
// decisão de desligamento (só o zero importa), mas uma fonte só é mais clara.
func (s *AppServer) presenceMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/events") {
			next.ServeHTTP(w, r)
			return
		}
		s.clientConnected()
		defer s.clientDisconnected()
		next.ServeHTTP(w, r)
	})
}

func (s *AppServer) handleInstanceIdentity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(InstanceIdentity{
		App:     appIdentity,
		Version: Version,
		PID:     os.Getpid(),
	})
}

func (s *AppServer) handleInstanceFocus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Alguém pediu a Janela de volta: a instância deixa de estar ausente.
	s.markClientSeen()

	s.presenceMu.Lock()
	st := s.lc()
	opener, url, noWindow := st.windowOpener, st.url, s.noWindow
	s.presenceMu.Unlock()

	status := "focused"
	if opener != nil && !noWindow {
		go opener(url)
	} else {
		status = "no_window"
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": status, "url": url})
}

// handleUIClosed recebe o sendBeacon do pagehide: a Janela sumiu agora, sem
// esperar o timeout da conexão SSE (contrato 1.9).
func (s *AppServer) handleUIClosed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.markClientGone()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// ---------------------------------------------------------------------------
// Presença (contrato 1.9)
// ---------------------------------------------------------------------------

// clientConnected registra uma Janela conectada (uma conexão SSE) e cancela
// qualquer desligamento agendado.
func (s *AppServer) clientConnected() {
	s.presenceMu.Lock()
	defer s.presenceMu.Unlock()
	s.sseCount++
	s.lastClientSeen = time.Now()
	s.shutdownWhenDone = false
	s.cancelPresenceTimerLocked()
}

// clientDisconnected registra a saída de uma Janela; ao chegar a zero, agenda
// o desligamento depois da tolerância.
func (s *AppServer) clientDisconnected() {
	s.presenceMu.Lock()
	defer s.presenceMu.Unlock()
	s.sseCount--
	if s.sseCount > 0 {
		return
	}
	s.sseCount = 0
	s.lastClientSeen = time.Now()
	s.armPresenceTimerLocked(0)
}

// markClientSeen trata a instância como presente de novo (foco pedido).
func (s *AppServer) markClientSeen() {
	s.presenceMu.Lock()
	defer s.presenceMu.Unlock()
	s.lastClientSeen = time.Now()
	s.shutdownWhenDone = false
	s.cancelPresenceTimerLocked()
}

// markClientGone declara ausência imediata (POST /api/ui/closed).
func (s *AppServer) markClientGone() {
	s.presenceMu.Lock()
	defer s.presenceMu.Unlock()
	s.sseCount = 0
	s.lastClientSeen = time.Now()
	s.armPresenceTimerLocked(0)
}

// armPresenceTimerLocked agenda a checagem de ausência. grace <= 0 usa a
// tolerância configurada. Exige presenceMu.
func (s *AppServer) armPresenceTimerLocked(grace time.Duration) {
	if s.noWindow || s.stoppedLocked() {
		return
	}
	st := s.lc()
	if grace <= 0 {
		grace = st.presenceGrace
	}
	if grace <= 0 {
		grace = DefaultPresenceGrace
	}
	if st.presenceTimer != nil {
		st.presenceTimer.Stop()
	}
	st.presenceTimer = time.AfterFunc(grace, s.onPresenceExpired)
}

// stoppedLocked diz se Stop já rodou. Exige presenceMu.
func (s *AppServer) stoppedLocked() bool {
	if s.shutdownCh == nil {
		return false
	}
	select {
	case <-s.shutdownCh:
		return true
	default:
		return false
	}
}

// cancelPresenceTimerLocked desarma o desligamento agendado. Exige presenceMu.
func (s *AppServer) cancelPresenceTimerLocked() {
	st := s.lc()
	if st.presenceTimer != nil {
		st.presenceTimer.Stop()
		st.presenceTimer = nil
	}
}

// onPresenceExpired roda quando a tolerância sem clientes esgota.
func (s *AppServer) onPresenceExpired() {
	s.presenceMu.Lock()
	if s.noWindow || s.sseCount > 0 || s.stoppedLocked() {
		s.presenceMu.Unlock()
		return
	}
	s.presenceMu.Unlock()

	if s.scanInProgress() {
		// Varredura em curso: encerra ao concluir. O rearme cobre o caso de o
		// dono do pipeline não chamar onScanFinished.
		s.presenceMu.Lock()
		s.shutdownWhenDone = true
		s.armPresenceTimerLocked(0)
		s.presenceMu.Unlock()
		return
	}

	s.shutdownNow("no_clients")
}

// onScanFinished é o gancho para o dono do pipeline de Varredura (S2): avisa
// que a Varredura acabou, para o desligamento adiado acontecer na hora.
func (s *AppServer) onScanFinished() {
	s.presenceMu.Lock()
	pending := s.shutdownWhenDone && !s.noWindow && s.sseCount == 0
	s.presenceMu.Unlock()
	if !pending {
		return
	}
	if s.scanInProgress() {
		return
	}
	s.shutdownNow("scan_finished")
}

// scanInProgress diz se há Varredura em curso (fases do contrato 1.2).
func (s *AppServer) scanInProgress() bool {
	s.presenceMu.Lock()
	fn := s.lc().scanActive
	s.presenceMu.Unlock()
	if fn != nil {
		return fn()
	}
	if s.Scanner == nil {
		return false
	}
	if s.Scanner.IsRunning() {
		return true
	}
	return activeScanPhases[s.Scanner.GetStatus().Phase]
}

// shutdownNow avisa as Janelas pelo SSE e encerra o servidor.
func (s *AppServer) shutdownNow(reason string) {
	s.broadcastSSE("shutdown", ShutdownEvent{Reason: reason, InSeconds: 0})

	s.presenceMu.Lock()
	delay := s.lc().shutdownFlushDelay
	s.presenceMu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}

	log.Printf("[*] Encerrando o ScanFile Pro (%s).\n", reason)
	s.Stop()
}

// ---------------------------------------------------------------------------
// instance.json e descoberta de instância (contrato 1.9)
// ---------------------------------------------------------------------------

// InstanceDir é a pasta de estado por usuário (%LOCALAPPDATA%\ScanFile).
func InstanceDir() string {
	if base := os.Getenv("LOCALAPPDATA"); base != "" {
		return filepath.Join(base, "ScanFile")
	}
	if base, err := os.UserConfigDir(); err == nil && base != "" {
		return filepath.Join(base, "ScanFile")
	}
	return filepath.Join(os.TempDir(), "ScanFile")
}

// InstanceFilePath é o caminho de instance.json.
func InstanceFilePath() string {
	return filepath.Join(InstanceDir(), "instance.json")
}

// WriteInstanceFile grava instance.json de forma atômica e, quando o sistema
// respeita permissões POSIX, com modo 0600.
func WriteInstanceFile(info InstanceInfo) error {
	dir := InstanceDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, fmt.Sprintf("instance.json.%d.tmp", os.Getpid()))
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, InstanceFilePath()); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// ReadInstanceFile lê instance.json.
func ReadInstanceFile() (InstanceInfo, error) {
	var info InstanceInfo
	data, err := os.ReadFile(InstanceFilePath())
	if err != nil {
		return info, err
	}
	if err := json.Unmarshal(data, &info); err != nil {
		return info, fmt.Errorf("instance.json inválido: %w", err)
	}
	return info, nil
}

// removeInstanceFile apaga instance.json apenas se ele ainda for nosso: num
// handoff o arquivo já pertence ao filho elevado.
func (s *AppServer) removeInstanceFile() {
	info, err := ReadInstanceFile()
	if err != nil {
		return
	}
	if info.PID != os.Getpid() {
		return
	}
	_ = os.Remove(InstanceFilePath())
}

// ProbeInstance pergunta a GET /api/instance se quem está na porta é uma
// instância nossa. Não usa token (contrato 1.1).
func ProbeInstance(port int, timeout time.Duration) (InstanceIdentity, bool) {
	var id InstanceIdentity
	if port <= 0 {
		return id, false
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/instance", port))
	if err != nil {
		return id, false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return id, false
	}
	if err := json.NewDecoder(resp.Body).Decode(&id); err != nil {
		return id, false
	}
	if id.App != appIdentity {
		return id, false
	}
	return id, true
}

// FocusInstance pede à instância em execução que traga a Janela de volta.
func FocusInstance(info InstanceInfo, timeout time.Duration) error {
	req, err := http.NewRequest(http.MethodPost,
		fmt.Sprintf("http://127.0.0.1:%d/api/instance/focus", info.Port), nil)
	if err != nil {
		return err
	}
	if info.Token != "" {
		req.Header.Set("X-ScanFile-Token", info.Token)
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("a instância em execução recusou o foco (HTTP %d)", resp.StatusCode)
	}
	return nil
}

// AnnounceHandoff grava instance.json com o PID deste processo antes mesmo de
// conseguir a porta. É o sinal que o pai espera para liberar o listener: ele
// ainda ocupa a porta, então o filho não teria como responder em /api/instance.
func AnnounceHandoff(port int, token string) error {
	return WriteInstanceFile(InstanceInfo{Port: port, PID: os.Getpid(), Token: token})
}

// ---------------------------------------------------------------------------
// Start / Stop
// ---------------------------------------------------------------------------

// Start launches the local HTTP/SSE server on the fixed port (contrato 1.9).
// port <= 0 usa config.ServerPort (47321). Se a porta estiver ocupada por
// outra instância nossa, devolve ErrInstanceAlreadyRunning; ocupada por
// qualquer outro processo, cai numa porta livre.
func (s *AppServer) Start(port int) (string, error) {
	if port <= 0 {
		port = config.LoadConfig().ServerPort
	}
	if port <= 0 || port > 65535 {
		port = config.DefaultServerPort
	}

	s.presenceMu.Lock()
	handoff := s.lc().handoff
	s.presenceMu.Unlock()

	ln, err := s.listen(port, handoff)
	if err != nil {
		return "", err
	}

	s.listener = ln
	addr := ln.Addr().String()
	actualPort := ln.Addr().(*net.TCPAddr).Port
	url := "http://" + addr

	s.presenceMu.Lock()
	st := s.lc()
	st.port = actualPort
	st.url = url
	s.startedAt = time.Now()
	s.lastClientSeen = time.Now()
	// A Janela ainda não conectou: a partida também conta como ausência, com
	// uma tolerância maior (contrato 1.9 + folga para o Edge abrir).
	s.armPresenceTimerLocked(st.startupGrace)
	s.presenceMu.Unlock()

	// H2: sem WriteTimeout global (ele mataria SSE e downloads longos); o
	// limite de escrita fica por handler, via http.ResponseController.
	s.httpServer = &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		_ = s.httpServer.Serve(ln)
	}()

	if err := WriteInstanceFile(InstanceInfo{Port: actualPort, PID: os.Getpid(), Token: s.sessionToken}); err != nil {
		log.Printf("[!] Não foi possível gravar %s: %v\n", InstanceFilePath(), err)
	}

	return url, nil
}

// listen resolve a porta segundo as regras de instância única.
func (s *AppServer) listen(port int, handoff bool) (net.Listener, error) {
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	if handoff {
		// Filho elevado: a porta é a mesma da Janela já aberta, então vale a
		// pena esperar o pai fechar o listener em vez de mudar de porta.
		deadline := time.Now().Add(handoffBindTimeout)
		for {
			ln, err := net.Listen("tcp", addr)
			if err == nil {
				return ln, nil
			}
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("a porta %d não foi liberada pela instância anterior: %w", port, err)
			}
			time.Sleep(200 * time.Millisecond)
		}
	}

	ln, err := net.Listen("tcp", addr)
	if err == nil {
		return ln, nil
	}

	// Porta ocupada: se for uma instância nossa, quem chama deve focá-la.
	if _, ours := ProbeInstance(port, 2*time.Second); ours {
		return nil, ErrInstanceAlreadyRunning
	}

	log.Printf("[!] Porta %d ocupada por outro processo; usando uma porta livre.\n", port)
	return net.Listen("tcp", "127.0.0.1:0")
}

// Stop gracefully stops the server. É idempotente e fecha o canal de Done.
func (s *AppServer) Stop() {
	s.shutdownOnce.Do(func() {
		s.presenceMu.Lock()
		s.cancelPresenceTimerLocked()
		s.presenceMu.Unlock()

		s.removeInstanceFile()

		if s.Watcher != nil {
			s.Watcher.Stop()
		}
		if s.httpServer != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = s.httpServer.Shutdown(ctx)
		} else if s.listener != nil {
			_ = s.listener.Close()
		}

		s.presenceMu.Lock()
		if s.shutdownCh == nil {
			s.shutdownCh = make(chan struct{})
		}
		close(s.shutdownCh)
		s.presenceMu.Unlock()

		lifecycleStates.Delete(s)
	})
}

// ---------------------------------------------------------------------------
// Handoff de elevação (contrato 1.9, Q13)
// ---------------------------------------------------------------------------

// handoffArgs monta os argumentos do filho elevado sem duplicar --handoff.
func handoffArgs(args []string) []string {
	out := make([]string, 0, len(args)+1)
	for _, a := range args {
		if a == "--handoff" || a == "-handoff" {
			continue
		}
		out = append(out, a)
	}
	return append(out, "--handoff")
}

func (s *AppServer) handleElevateProcess(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if privileges.CheckPrivilegeStatus().IsElevated {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":  "already_elevated",
			"message": "O ScanFile Pro já está em Modo Administrador.",
		})
		return
	}

	pid, err := privileges.LaunchElevatedHandoff(handoffArgs(os.Args[1:]))
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":   "elevation_failed",
			"message": fmt.Sprintf("Falha ao solicitar elevação: %v", err),
		})
		return
	}

	// A resposta sai antes da troca: o listener morre logo em seguida.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "elevating",
		"pid":     pid,
		"message": "Instância com Modo Administrador solicitada; esta janela será reconectada.",
	})

	go s.completeHandoff(pid, HandoffWaitTimeout)
}

// completeHandoff espera o filho elevado assumir e então encerra este processo.
func (s *AppServer) completeHandoff(childPID uint32, timeout time.Duration) {
	if s.waitForHandoffChild(childPID, timeout) {
		log.Printf("[*] Instância elevada (PID %d) assumiu; liberando a porta.\n", childPID)
		s.Stop()
		return
	}
	log.Printf("[!] A instância elevada (PID %d) não respondeu em %s; mantendo esta em execução.\n", childPID, timeout)
}

// waitForHandoffChild espera o filho responder em GET /api/instance ou, quando
// ele ainda espera a nossa porta, se anunciar em instance.json com o próprio PID.
func (s *AppServer) waitForHandoffChild(childPID uint32, timeout time.Duration) bool {
	self := os.Getpid()
	ourPort := s.Port()
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		info, err := ReadInstanceFile()
		if err == nil && info.PID != 0 && info.PID != self &&
			(childPID == 0 || info.PID == int(childPID)) {
			if info.Port == 0 || info.Port == ourPort {
				// O filho anunciou a nossa porta: ele está esperando o
				// listener fechar, então não teria como responder ainda.
				return true
			}
			if id, ok := ProbeInstance(info.Port, 2*time.Second); ok && id.PID == info.PID {
				return true
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}
