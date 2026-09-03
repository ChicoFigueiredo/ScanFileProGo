package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"scanfile/pkg/ai"
	"scanfile/pkg/config"
	"scanfile/pkg/hasher"
	"scanfile/pkg/indexer"
	"scanfile/pkg/mcp"
	"scanfile/pkg/scanner"
	"scanfile/pkg/watcher"
)

// AppServer orchestrates the REST & SSE API for the ScanFile application.
type AppServer struct {
	Tree        *scanner.TreeManager
	Scanner     *scanner.Scanner
	Hasher      *HasherManager
	Index       *indexer.DuplicateIndex
	FolderIndex *indexer.FolderDuplicateIndex
	Watcher     *watcher.FSWatcher
	MCPContext  *mcp.MCPToolsContext
	AIAgent     *ai.AgentCoordinator
	uiFS        fs.FS
	httpServer  *http.Server
	listener    net.Listener
	activeRoots []string
	lastConfig  scanner.ScanConfig

	eventsMu     sync.RWMutex
	sseClients   map[chan string]bool
	recentLogs   []scanner.FSEventLog
	recentLogsMu sync.RWMutex

	// Sessão e ciclo de vida (etapa 2, contrato 1.1 e 1.9)
	sessionToken     string
	startedAt        time.Time
	noWindow         bool
	shutdownOnce     sync.Once
	shutdownCh       chan struct{}
	presenceMu       sync.Mutex
	sseCount         int
	lastClientSeen   time.Time
	shutdownWhenDone bool

	// Varredura em curso (etapa 2, contrato 1.2 e 1.7)
	scanMu             sync.Mutex
	scanCancel         context.CancelFunc
	scanDone           chan struct{}
	autosaveMu         sync.Mutex
	lastAutoSaveChange uint64
	lastAutoSaveAt     time.Time
	autosaveConfig     scanner.ScanConfig
	autosaveRunning    bool
	autosaveStop       chan struct{}

	// scanBarrier é chamado no início de cada etapa da orquestração. Fica nil em
	// produção; os testes o usam para segurar o pipeline num ponto conhecido e
	// provar o 409 e o Cancelamento sem depender de tempo de relógio.
	scanBarrier func(stage string)

	// Monitoramento e desligamento das tarefas de fundo (etapa 2, contrato 1.10)
	watcherMu     sync.Mutex
	bgStopped     bool
	savedScansDir string
}

// HasherManager wraps hasher execution and state.
type HasherManager struct {
	Engine *hasher.Hasher
	Status scanner.ScanStatus
	mu     sync.RWMutex
}

type serverToolsExecutor struct {
	mcpCtx *mcp.MCPToolsContext
}

func (ste *serverToolsExecutor) ExecuteTool(ctx context.Context, name string, argsJSON string) (string, *ai.ActionProposal, error) {
	switch name {
	case "classify_files":
		var params mcp.ClassifyFilesParams
		_ = json.Unmarshal([]byte(argsJSON), &params)
		items, err := ste.mcpCtx.ClassifyFiles(ctx, params)
		if err != nil {
			return "", nil, err
		}
		data, _ := json.MarshalIndent(items, "", "  ")
		return string(data), nil, nil

	case "analyze_file_content":
		var params mcp.AnalyzeFileParams
		_ = json.Unmarshal([]byte(argsJSON), &params)
		res, err := ste.mcpCtx.AnalyzeFileContent(ctx, params)
		if err != nil {
			return "", nil, err
		}
		data, _ := json.MarshalIndent(res, "", "  ")
		return string(data), nil, nil

	case "analyze_image_visual":
		var params mcp.AnalyzeImageParams
		_ = json.Unmarshal([]byte(argsJSON), &params)
		res, err := ste.mcpCtx.AnalyzeImageVisual(ctx, params)
		if err != nil {
			return "", nil, err
		}
		data, _ := json.MarshalIndent(res, "", "  ")
		return string(data), nil, nil

	case "compare_visual_similarity":
		var params mcp.CompareVisualParams
		_ = json.Unmarshal([]byte(argsJSON), &params)
		res, err := ste.mcpCtx.CompareVisualSimilarity(ctx, params)
		if err != nil {
			return "", nil, err
		}
		data, _ := json.MarshalIndent(res, "", "  ")
		return string(data), nil, nil

	case "write_file_metadata":
		var params mcp.WriteMetadataParams
		_ = json.Unmarshal([]byte(argsJSON), &params)
		meta, err := ste.mcpCtx.WriteFileMetadata(params)
		if err != nil {
			return "", nil, err
		}
		data, _ := json.MarshalIndent(meta, "", "  ")
		return string(data), nil, nil

	case "propose_actions":
		var params mcp.ProposeActionParams
		_ = json.Unmarshal([]byte(argsJSON), &params)
		prop, err := ste.mcpCtx.ProposeActions(params)
		if err != nil {
			return "", nil, err
		}
		var aiProp *ai.ActionProposal
		if prop != nil {
			aiProp = &ai.ActionProposal{
				ProposalID:  prop.ID,
				ActionType:  prop.ActionType,
				Description: prop.Description,
				Files:       prop.Files,
				FileCount:   prop.FileCount,
				TotalBytes:  prop.TotalBytes,
				TotalSize:   prop.TotalSize,
				Category:    prop.Category,
				DryRun:      prop.DryRun,
				Executed:    prop.Executed,
				CreatedAt:   prop.CreatedAt.Format(time.RFC3339),
			}
		}
		data, _ := json.MarshalIndent(prop, "", "  ")
		return string(data), aiProp, nil

	default:
		return fmt.Sprintf("Ferramenta desconhecida: %s", name), nil, nil
	}
}

// NewAppServer creates and initializes an AppServer.
func NewAppServer(uiFS fs.FS) *AppServer {
	tree := scanner.NewTreeManager()
	idx := indexer.NewDuplicateIndex()
	fIdx := indexer.NewFolderDuplicateIndex()
	sc := scanner.NewScanner(tree)
	hEngine := hasher.NewHasher()

	cfg := config.LoadConfig()
	ollamaClient := ai.NewOllamaClient(cfg.AIOllamaEndpoint)
	mcpCtx := mcp.NewMCPToolsContext(tree, idx, fIdx, ollamaClient, cfg.AIOllamaModel)

	toolsDefs := mcp.GetOpenAIToolDefinitions()
	agent := ai.NewAgentCoordinator(
		cfg.AIOllamaEndpoint,
		config.OpenRouterKey(cfg),
		"",
		&serverToolsExecutor{mcpCtx: mcpCtx},
		toolsDefs,
	)

	return &AppServer{
		Tree:          tree,
		Scanner:       sc,
		Hasher:        &HasherManager{Engine: hEngine},
		Index:         idx,
		FolderIndex:   fIdx,
		MCPContext:    mcpCtx,
		AIAgent:       agent,
		uiFS:          uiFS,
		sseClients:    make(map[chan string]bool),
		recentLogs:    make([]scanner.FSEventLog, 0, 100),
		savedScansDir: DefaultSavedScansDir,
	}
}

// DebugMode controls verbose HTTP request and internal state logging.
var DebugMode bool

type responseRecorder struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int64
}

func (rec *responseRecorder) WriteHeader(code int) {
	rec.statusCode = code
	rec.ResponseWriter.WriteHeader(code)
}

func (rec *responseRecorder) Write(b []byte) (int, error) {
	n, err := rec.ResponseWriter.Write(b)
	rec.bytesWritten += int64(n)
	return n, err
}

func (rec *responseRecorder) Flush() {
	if f, ok := rec.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *AppServer) debugMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &responseRecorder{ResponseWriter: w, statusCode: http.StatusOK}

		defer func() {
			if recErr := recover(); recErr != nil {
				log.Printf("[PANIC RECOVER] %s %s: %v\nStack:\n%s\n", r.Method, r.URL.Path, recErr, string(debug.Stack()))
				http.Error(w, fmt.Sprintf("Internal Server Error: %v", recErr), http.StatusInternalServerError)
			}
			if DebugMode && !strings.HasPrefix(r.URL.Path, "/api/events") {
				duration := time.Since(start)
				log.Printf("[HTTP DEBUG] %s %s | Status: %d | Size: %d B | Time: %v | Remote: %s\n",
					r.Method, r.URL.RequestURI(), rec.statusCode, rec.bytesWritten, duration, r.RemoteAddr)
			}
		}()

		next.ServeHTTP(rec, r)
	})
}

// Type alias for status inside server
type ScanStatus = scanner.ScanStatus

// GetLiveMemoryStats gathers process memory and host OS RAM utilization.
func GetLiveMemoryStats() *scanner.MemoryStatsPayload {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	payload := &scanner.MemoryStatsPayload{
		AllocMB:      m.Alloc / (1024 * 1024),
		TotalAllocMB: m.TotalAlloc / (1024 * 1024),
		SysMB:        m.Sys / (1024 * 1024),
		NumGC:        m.NumGC,
		Goroutines:   runtime.NumGoroutine(),
	}

	getSystemPhysicalMemory(payload, m.Alloc)
	return payload
}

// Fases da Varredura publicadas em ScanStatus.Phase (contrato 1.2).
const (
	PhaseIdle         = "idle"
	PhaseMetadata     = "phase1_metadata"
	PhaseHashing      = "phase2_hashing"
	PhaseIndexing     = "indexing"
	PhaseCompleted    = "completed"
	PhaseCancelling   = "cancelling"
	PhaseCancelled    = "cancelled"
	PhaseLoadingCache = "loading_cache"
	PhaseWatching     = "watching"
)

const (
	// DefaultSavedScansDir é a pasta onde Snapshots e Autosaves são gravados.
	DefaultSavedScansDir = "saved_scans"

	// DefaultAutoSaveIntervalMinutes é o intervalo do Autosave durante a
	// Varredura quando a Configuração não diz outro.
	DefaultAutoSaveIntervalMinutes = 5

	// WatchAutoSaveInterval é o intervalo do Autosave com Monitoramento ativo.
	// Só grava se o contador de mudanças da árvore andou (contrato 1.7).
	WatchAutoSaveInterval = 10 * time.Minute

	// autosaveTick é o passo do único relógio de Autosave do processo. Ele só
	// decide se algo está vencido; quem decide o intervalo é a fase corrente.
	autosaveTick = 30 * time.Second
)

// isBusyPhase informa se a fase impede iniciar uma nova Varredura (contrato 1.2).
func isBusyPhase(phase string) bool {
	switch phase {
	case PhaseMetadata, PhaseHashing, PhaseIndexing, PhaseCancelling, PhaseLoadingCache:
		return true
	}
	return false
}

// currentPhase devolve a fase corrente da Varredura.
func (s *AppServer) currentPhase() string {
	return s.Scanner.GetStatus().Phase
}

// setPhase publica a fase e um texto de acompanhamento na interface.
func (s *AppServer) setPhase(phase, message string) {
	s.Scanner.SetStatus(func(st *ScanStatus) {
		st.Phase = phase
		if message != "" {
			st.CurrentPath = message
		}
	})
}

// changeSignal resume "algo mudou na árvore desde o último Autosave".
//
// O sinal soma o contador do TreeManager com o do Monitoramento porque
// ReplaceFile e RemoveDir (pkg/scanner/tree_watch.go, propriedade do Agente B)
// ainda não avançam o ChangeCounter da árvore; sem a segunda parcela o Autosave
// com Monitoramento ativo nunca gravaria.
func (s *AppServer) changeSignal() uint64 {
	var sig uint64
	if s.Tree != nil {
		sig += s.Tree.ChangeCounter()
	}
	if fw := s.currentWatcher(); fw != nil {
		sig += fw.ChangeCount()
	}
	return sig
}

// currentWatcher devolve o Monitoramento ativo sob o seu próprio lock.
func (s *AppServer) currentWatcher() *watcher.FSWatcher {
	s.watcherMu.Lock()
	defer s.watcherMu.Unlock()
	return s.Watcher
}

func (s *AppServer) setWatcher(fw *watcher.FSWatcher) {
	s.watcherMu.Lock()
	old := s.Watcher
	s.Watcher = fw
	s.watcherMu.Unlock()
	if old != nil && old != fw {
		old.Stop()
	}
}

// stopWatcher encerra o Monitoramento, se houver.
func (s *AppServer) stopWatcher() {
	s.watcherMu.Lock()
	fw := s.Watcher
	s.Watcher = nil
	s.watcherMu.Unlock()
	if fw != nil {
		fw.Stop()
	}
	s.Scanner.SetStatus(func(st *ScanStatus) { st.IsWatching = false })
}

// autoSaveDir devolve a pasta de Snapshots deste servidor.
func (s *AppServer) autoSaveDir() string {
	if s.savedScansDir == "" {
		return DefaultSavedScansDir
	}
	return s.savedScansDir
}

// noteAutoSaveBaseline zera o relógio do Autosave: o próximo periódico só vence
// um intervalo depois deste instante.
func (s *AppServer) noteAutoSaveBaseline(cfg scanner.ScanConfig, now time.Time) {
	s.autosaveMu.Lock()
	s.autosaveConfig = cfg
	s.lastAutoSaveAt = now
	s.lastAutoSaveChange = s.changeSignal()
	s.autosaveMu.Unlock()
}

// ensureAutoSaveLoop garante que existe UM relógio de Autosave no processo.
// Chamar de novo com outra Configuração só troca a Configuração; nunca cria um
// segundo relógio (contrato 1.7).
func (s *AppServer) ensureAutoSaveLoop(cfg scanner.ScanConfig) {
	s.autosaveMu.Lock()
	s.autosaveConfig = cfg
	if s.autosaveRunning || s.bgStopped {
		s.autosaveMu.Unlock()
		return
	}
	stop := make(chan struct{})
	s.autosaveStop = stop
	s.autosaveRunning = true
	s.autosaveMu.Unlock()

	go s.autoSaveLoop(stop)
}

func (s *AppServer) autoSaveLoop(stop chan struct{}) {
	ticker := time.NewTicker(autosaveTick)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			s.maybeAutoSave(time.Now())
			// O relógio se desliga quando não há mais nada para gravar; a
			// próxima Varredura ou o Monitoramento o religam.
			//
			// A fase é lida DENTRO do lock de propósito: quem liga o relógio
			// publica a fase nova antes de chamar ensureAutoSaveLoop, então uma
			// chamada que encontrou o relógio ligado garante que esta leitura
			// enxerga a fase nova e o relógio não se desliga por engano.
			s.autosaveMu.Lock()
			if !autoSavePhaseNeedsTicker(s.currentPhase()) && s.autosaveStop == stop {
				s.autosaveRunning = false
				s.autosaveStop = nil
				s.autosaveMu.Unlock()
				return
			}
			s.autosaveMu.Unlock()
		}
	}
}

// autoSavePhaseNeedsTicker informa se a fase ainda pede Autosave periódico.
func autoSavePhaseNeedsTicker(phase string) bool {
	switch phase {
	case PhaseMetadata, PhaseHashing, PhaseIndexing, PhaseWatching:
		return true
	}
	return false
}

// maybeAutoSave grava um Autosave se a fase corrente pedir e o intervalo tiver
// vencido. Com Monitoramento ativo só grava quando o contador de mudanças
// avançou desde o último Autosave. Devolve se gravou.
func (s *AppServer) maybeAutoSave(now time.Time) bool {
	phase := s.currentPhase()

	s.autosaveMu.Lock()
	cfg := s.autosaveConfig
	last := s.lastAutoSaveAt
	lastChange := s.lastAutoSaveChange
	s.autosaveMu.Unlock()

	var interval time.Duration
	switch phase {
	case PhaseMetadata, PhaseHashing, PhaseIndexing:
		minutes := cfg.AutoSaveIntervalMinutes
		if minutes <= 0 {
			minutes = DefaultAutoSaveIntervalMinutes
		}
		interval = time.Duration(minutes) * time.Minute
	case PhaseWatching:
		if s.changeSignal() == lastChange {
			return false
		}
		interval = WatchAutoSaveInterval
	default:
		// Cancelamento, idle, completed e carregamento de Snapshot não gravam.
		return false
	}

	if !last.IsZero() && now.Sub(last) < interval {
		return false
	}

	return s.writeAutoSave(cfg, now)
}

// writeAutoSave grava o Autosave agora, atualiza o status e avisa a interface.
// Nunca grava durante ou depois de um Cancelamento (contrato 1.2).
func (s *AppServer) writeAutoSave(cfg scanner.ScanConfig, now time.Time) bool {
	switch s.currentPhase() {
	case PhaseCancelling, PhaseCancelled:
		return false
	}
	if s.Tree == nil || s.Tree.GetTotalFileCount() == 0 {
		return false
	}

	savedPath, err := scanner.SaveAutoSave(s.Tree, s.activeRoots, cfg, s.autoSaveDir())
	if err != nil {
		if DebugMode {
			log.Printf("[AUTOSAVE] falha ao gravar: %v\n", err)
		}
		return false
	}

	s.autosaveMu.Lock()
	s.lastAutoSaveAt = now
	s.lastAutoSaveChange = s.changeSignal()
	s.autosaveMu.Unlock()

	unix := now.Unix()
	s.Scanner.SetStatus(func(st *ScanStatus) {
		st.LastAutoSaveTime = unix
		st.AutoSaveFilePath = savedPath
	})
	s.broadcastSSE("autosave_done", map[string]any{
		"filePath": savedPath,
		"time":     unix,
	})
	return true
}

// StopBackground encerra tudo que roda fora das requisições: a Varredura em
// curso, o Monitoramento, o relógio do Autosave e o log de erros em disco.
// É idempotente.
//
// O ciclo de vida (Stop, em lifecycle.go) é do Agente S3: ele precisa chamar
// este método para que o log de erros seja finalizado (contrato 8, item 9).
func (s *AppServer) StopBackground() {
	s.autosaveMu.Lock()
	if s.bgStopped {
		s.autosaveMu.Unlock()
	} else {
		s.bgStopped = true
		if s.autosaveStop != nil {
			close(s.autosaveStop)
			s.autosaveStop = nil
		}
		s.autosaveRunning = false
		s.autosaveMu.Unlock()
	}

	s.scanMu.Lock()
	cancel := s.scanCancel
	s.scanMu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.Scanner.Cancel()
	s.Hasher.Engine.Cancel()

	s.stopWatcher()
	s.Scanner.CloseLogger()
}
