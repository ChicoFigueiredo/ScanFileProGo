package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"scanfile/pkg/hasher"
	"scanfile/pkg/indexer"
	"scanfile/pkg/scanner"
	"scanfile/pkg/watcher"
)

// Estágios da orquestração, usados pelo seam de testes scanBarrier.
const (
	stagePhase1   = "phase1"
	stagePhase2   = "phase2"
	stageIndexing = "indexing"
	stageWatching = "watching"
	stageRescan   = "rescan"
)

// barrier dá aos testes um ponto de parada determinístico entre as etapas da
// orquestração. Em produção scanBarrier é nil e a chamada não custa nada.
func (s *AppServer) barrier(stage string) {
	if s.scanBarrier != nil {
		s.scanBarrier(stage)
	}
}

func (s *AppServer) handleStartScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var config scanner.ScanConfig
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		http.Error(w, "Invalid body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if len(config.Roots) == 0 {
		http.Error(w, "No roots selected", http.StatusBadRequest)
		return
	}

	config.HashAlgorithm = scanner.NormalizeHashAlgorithm(config.HashAlgorithm)
	if config.MinSizeForHash == 0 {
		config.MinSizeForHash = 1
	}
	if config.LogDir == "" {
		config.LogDir = "logs"
	}

	// Threads efetivas das duas fases (contrato 1.3). O motor aplica o mesmo
	// cálculo; resolvê-lo aqui deixa o HUD com os números certos desde o
	// primeiro retrato de status, antes de a Fase 1 começar.
	phase1Workers := scanner.ResolveWorkers(config.WorkerThreads, scanner.PhaseMetadata)
	phase2Workers := scanner.ResolveWorkers(config.WorkerThreads, scanner.PhaseHashing)

	// A reserva da vaga e a troca de fase acontecem sob o mesmo lock: um segundo
	// start logo depois deste já enxerga "phase1_metadata" e recebe 409.
	s.scanMu.Lock()
	if phase := s.currentPhase(); s.scanDone != nil || isBusyPhase(phase) {
		s.scanMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "scan_in_progress",
			"phase": phase,
		})
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.scanCancel = cancel
	s.scanDone = done
	s.Scanner.SetStatus(func(st *ScanStatus) {
		st.Phase = PhaseMetadata
		st.CurrentPath = "Preparando varredura..."
		st.ProgressPercent = 0
		st.PrehashCount = 0
		st.Phase1Workers = phase1Workers
		st.Phase2Workers = phase2Workers
	})
	s.scanMu.Unlock()

	s.setScanState(config.Roots, config)

	// O Assistente só lê arquivos dentro das Raízes Varridas (contrato 1.11).
	if s.MCPContext != nil {
		s.MCPContext.SetAllowedRoots(config.Roots)
	}

	// Quick Scan: o índice de reaproveitamento sai da árvore em memória ou, na
	// falta dela, do último Autosave lido em streaming (achado H3).
	if config.QuickScanMode {
		s.Scanner.SetQuickScanLookup(s.buildQuickScanLookup())
	} else {
		s.Scanner.SetQuickScanLookup(nil)
	}

	// Monitoramento e árvore antiga saem de cena antes da nova Varredura.
	s.stopWatcher()
	s.Tree().Reset()

	s.noteAutoSaveBaseline(config, time.Now())
	s.ensureAutoSaveLoop(config)

	go s.orchestrateScan(ctx, cancel, done, config, phase1Workers, phase2Workers)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "started"})
}

// buildQuickScanLookup monta o índice do Quick Scan sem nunca materializar a
// lista de arquivos de um Snapshot na memória.
func (s *AppServer) buildQuickScanLookup() map[string]*scanner.FileNode {
	if tree := s.Tree(); tree != nil && tree.GetTotalFileCount() > 0 {
		return scanner.BuildQuickScanLookupFromTree(tree)
	}
	info, err := scanner.GetLatestAutoSave(s.autoSaveDir())
	if err != nil || info == nil {
		return nil
	}
	lookup, _, err := scanner.LoadQuickScanLookupFromFile(info.FilePath)
	if err != nil {
		return nil
	}
	return lookup
}

// orchestrateScan conduz o ciclo completo da Varredura sob um contexto próprio:
// Fase 1, Autosave, Fase 2, indexação e Monitoramento. Qualquer Cancelamento
// aborta o ciclo inteiro e o status final é "cancelled", sem Autosave.
func (s *AppServer) orchestrateScan(ctx context.Context, cancel context.CancelFunc, done chan struct{}, config scanner.ScanConfig, phase1Workers, phase2Workers int) {
	defer func() {
		s.scanMu.Lock()
		if s.scanDone == done {
			s.scanDone = nil
			s.scanCancel = nil
		}
		s.scanMu.Unlock()
		cancel()
		close(done)
		// Avisa o ciclo de vida: se a Janela fechou durante a Varredura, o
		// desligamento adiado acontece agora (contrato 1.9, Q33).
		s.onScanFinished()
	}()

	// FASE 1: metadados.
	s.barrier(stagePhase1)
	if err := ctx.Err(); err != nil {
		s.Scanner.SetQuickScanLookup(nil)
		s.finishScanAborted(err)
		return
	}
	err := s.Scanner.StartScan(ctx, config, func(st scanner.ScanStatus) {
		s.broadcastSSE("scan_progress", st)
	})

	// O índice do Quick Scan só serve à Fase 1. Segurá-lo depois prendia a
	// árvore anterior inteira — nós e caminhos em minúsculas do mapa — pelo
	// resto da sessão, desfazendo parte do ganho do ADR-0001.
	s.Scanner.SetQuickScanLookup(nil)

	if err != nil {
		s.finishScanAborted(err)
		return
	}

	// Autosave ao concluir a Fase 1 (contrato 1.7).
	s.writeAutoSave(config, time.Now())

	// FASE 2: Hash Completo dos Candidatos a Duplicado.
	s.barrier(stagePhase2)
	if err := ctx.Err(); err != nil {
		s.finishScanAborted(err)
		return
	}
	s.setPhase(PhaseHashing, "Iniciando cálculo de hashes em multithread...")

	allFiles := s.Tree().GetAllFiles()
	opts := hasher.ComputeHashOptions{
		Algorithm:     config.HashAlgorithm,
		HashAllFiles:  config.HashAllFiles,
		MinSize:       config.MinSizeForHash,
		WorkerThreads: phase2Workers,
		DiskLogger:    s.Scanner.DiskLogger,
		OnDetailedProgress: func(p hasher.HashProgress) {
			s.Scanner.SetStatus(func(st *ScanStatus) {
				st.FilesToHashCount = p.TotalCount
				st.FilesHashedCount = p.HashedCount
				st.BytesHashed = p.BytesHashed
				st.PrehashCount = p.PrehashCount
				st.CurrentPath = p.CurrentPath
				st.HashRateMBPerSec = p.RateMBps
				st.ActiveWorkers = p.ActiveWorkers
				st.RecentFiles = p.RecentFiles
				st.Phase1Workers = phase1Workers
				st.Phase2Workers = phase2Workers
				if p.TotalCount > 0 {
					st.ProgressPercent = (float64(p.HashedCount) / float64(p.TotalCount)) * 100.0
				}
			})
			s.broadcastStatus()
		},
	}

	if err := s.Hasher.Engine.RunHashing(ctx, allFiles, opts); err != nil {
		s.finishScanAborted(err)
		return
	}

	// Autosave ao concluir a Fase 2 (contrato 1.7).
	s.writeAutoSave(config, time.Now())

	// INDEXAÇÃO.
	s.barrier(stageIndexing)
	if err := ctx.Err(); err != nil {
		s.finishScanAborted(err)
		return
	}
	s.setPhase(PhaseIndexing, "Indexando duplicados e pastas clones...")
	s.broadcastStatus()

	s.Index.RebuildIndex(allFiles)
	grpCount, fileCount, wasted := s.Index.GetSummaryStats()

	if err := ctx.Err(); err != nil {
		s.finishScanAborted(err)
		return
	}

	s.FolderIndex.RebuildFolderIndex(s.Tree())
	fGrpCount, fCount, fWasted := s.FolderIndex.GetSummaryStats()

	if err := ctx.Err(); err != nil {
		s.finishScanAborted(err)
		return
	}

	s.Scanner.SetStatus(func(st *ScanStatus) {
		st.Phase = PhaseCompleted
		st.CurrentPath = "Varredura, indexação de hashes e análise de pastas finalizadas com sucesso!"
		st.DuplicateGroupsCount = grpCount
		st.DuplicateFilesCount = fileCount
		st.DuplicateWastedBytes = wasted
		st.DuplicateFolderGroupsCount = fGrpCount
		st.DuplicateFoldersCount = fCount
		st.DuplicateFolderWastedBytes = fWasted
		st.ProgressPercent = 100
	})
	s.broadcastStatus()

	// MONITORAMENTO.
	s.barrier(stageWatching)
	if ctx.Err() != nil {
		return
	}
	s.startWatching(config)
}

// finishScanAborted publica o desfecho de uma Varredura que não chegou ao fim.
func (s *AppServer) finishScanAborted(err error) {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		s.setPhase(PhaseCancelled, "Varredura cancelada.")
	case errors.Is(err, scanner.ErrScanInProgress), errors.Is(err, hasher.ErrHashingInProgress):
		s.setPhase(PhaseIdle, "O motor já estava ocupado; nenhuma Varredura foi iniciada.")
	default:
		s.setPhase(PhaseIdle, fmt.Sprintf("Varredura interrompida: %v", err))
	}
	s.broadcastStatus()
}

// startWatching liga o Monitoramento das Raízes Varridas.
//
// O Monitoramento NÃO usa o contexto da Varredura: ele sobrevive ao fim do
// ciclo e só termina em stopWatcher/StopBackground.
func (s *AppServer) startWatching(config scanner.ScanConfig) {
	algo := scanner.NormalizeHashAlgorithm(config.HashAlgorithm)
	fw, err := watcher.New(watcher.Options{
		Tree:        s.Tree(),
		Index:       s.Index,
		FolderIndex: s.FolderIndex,
		HashFunc: func(p string) (string, error) {
			h, _, err := hasher.ComputeSingleFileHash(p, algo)
			return h, err
		},
		OnEvent:    s.onWatcherEvent,
		OnOverflow: s.rescanRoot,
	})
	if err != nil {
		log.Printf("[MONITORAMENTO] não foi possível criar o observador: %v\n", err)
		return
	}

	if err := fw.Start(context.Background(), config.Roots); err != nil {
		log.Printf("[MONITORAMENTO] não foi possível observar as raízes: %v\n", err)
		return
	}

	s.setWatcher(fw)
	s.Scanner.SetStatus(func(st *ScanStatus) {
		st.Phase = PhaseWatching
		st.IsWatching = true
	})
	s.broadcastStatus()

	// Com Monitoramento ativo o Autosave passa a gravar a cada 10 min, e só
	// quando o contador de mudanças anda.
	s.noteAutoSaveBaseline(config, time.Now())
	s.ensureAutoSaveLoop(config)
}

// onWatcherEvent registra a mudança observada e atualiza os totais do HUD.
func (s *AppServer) onWatcherEvent(ev scanner.FSEventLog) {
	s.appendRecentLog(ev)

	g, f, wasted := s.Index.GetSummaryStats()
	s.Scanner.SetStatus(func(st *ScanStatus) {
		st.DuplicateGroupsCount = g
		st.DuplicateFilesCount = f
		st.DuplicateWastedBytes = wasted
		st.IsWatching = true
	})

	s.broadcastSSE("fs_event", ev)
}

// rescanRoot revarre uma única Raiz Varrida depois de o buffer de notificações
// do sistema estourar: os eventos daquela raiz se perderam, então só ela é
// remapeada, reaproveitando os hashes já conhecidos (Quick Scan).
func (s *AppServer) rescanRoot(root string) {
	if root == "" {
		return
	}

	s.scanMu.Lock()
	if s.scanDone != nil {
		// Já há um ciclo em curso; ele mesmo vai remapear a raiz.
		s.scanMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.scanCancel = cancel
	s.scanDone = done
	s.scanMu.Unlock()

	config := s.lastScanConfig()
	config.Roots = []string{root}
	config.QuickScanMode = true

	go func() {
		var scanErr error
		defer func() {
			// StartScan publica "phase1_metadata" antes de qualquer trabalho e
			// não a desfaz ao devolver erro. Sem este acerto a fase ficava presa
			// numa fase ocupada e todo start de Varredura e todo carregamento de
			// Snapshot respondia 409 até o usuário apertar Cancelar. A fase é
			// acertada ANTES de a vaga ser liberada: quem vê a vaga livre já vê
			// a fase coerente.
			s.settleAfterRescan(root, scanErr)

			s.scanMu.Lock()
			if s.scanDone == done {
				s.scanDone = nil
				s.scanCancel = nil
			}
			s.scanMu.Unlock()
			cancel()
			close(done)
		}()

		log.Printf("[MONITORAMENTO] buffer estourou em %s: revarrendo apenas esta raiz\n", root)
		s.barrier(stageRescan)
		s.Scanner.SetQuickScanLookup(scanner.BuildQuickScanLookupFromTree(s.Tree()))
		scanErr = s.Scanner.StartScan(ctx, config, nil)
		s.Scanner.SetQuickScanLookup(nil)
		if scanErr != nil {
			return
		}

		s.Index.RebuildIndex(s.Tree().GetAllFiles())
		s.FolderIndex.MarkDirty()
	}()
}

// settleAfterRescan publica o desfecho de uma revarredura de raiz.
//
// Com o Monitoramento de pé a fase volta para "watching": é o que o aplicativo
// está de fato fazendo, com revarredura ou sem ela. Sem Monitoramento o desfecho
// é terminal — "cancelled" se houve Cancelamento, "idle" se a revarredura morreu
// por outro motivo e "completed" se ela terminou.
func (s *AppServer) settleAfterRescan(root string, err error) {
	watching := s.currentWatcher() != nil
	cancelled := s.currentPhase() == PhaseCancelling ||
		errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)

	phase := PhaseWatching
	message := fmt.Sprintf("Raiz %s remapeada após estouro do buffer de eventos.", root)
	switch {
	case err == nil && !watching:
		phase = PhaseCompleted
	case cancelled:
		message = fmt.Sprintf("Revarredura de %s cancelada.", root)
		if !watching {
			phase = PhaseCancelled
		}
	case err != nil:
		message = fmt.Sprintf("Revarredura de %s interrompida: %v", root, err)
		if !watching {
			phase = PhaseIdle
		}
	}

	s.Scanner.SetStatus(func(st *ScanStatus) {
		st.Phase = phase
		st.IsWatching = watching
		st.CurrentPath = message
	})
	s.broadcastStatus()
}

func (s *AppServer) handleGetFolderDuplicates(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sortBy := q.Get("sortBy")
	if sortBy == "" {
		sortBy = "wasted_desc"
	}
	minSize, _ := strconv.ParseInt(q.Get("minSize"), 10, 64)
	search := q.Get("search")
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))

	if limit <= 0 {
		limit = 100 // Safe default so server never exhausts memory on huge scans
	} else if limit > 500 {
		limit = 500
	}

	topLevelOnly := q.Get("topLevelOnly") == "true" || q.Get("topLevelOnly") == "1"

	filter := indexer.FolderQueryFilter{
		SortBy:       sortBy,
		MinSize:      minSize,
		Search:       search,
		TopLevelOnly: topLevelOnly,
		Limit:        limit,
		Offset:       offset,
	}

	// Reconstrói só quando o Monitoramento marcou o índice sujo. Zero grupos
	// depois de um filtro não é sinal de índice velho: reconstruir por causa
	// disso varria a árvore inteira a cada consulta (achado M3).
	s.FolderIndex.RebuildIfDirty(s.Tree())
	res := s.FolderIndex.Query(filter)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

func (s *AppServer) handleCompareFolders(w http.ResponseWriter, r *http.Request) {
	pathA := r.URL.Query().Get("pathA")
	pathB := r.URL.Query().Get("pathB")

	if pathA == "" || pathB == "" {
		http.Error(w, "Parâmetros 'pathA' e 'pathB' são obrigatórios", http.StatusBadRequest)
		return
	}

	result, err := indexer.CompareFolders(s.Tree(), pathA, pathB)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// handleCancelScan aborta o pipeline inteiro: Fase 1, Fase 2, Autosave e
// indexação. A resposta é imediata; o status final "cancelled" chega pelo SSE.
func (s *AppServer) handleCancelScan(w http.ResponseWriter, r *http.Request) {
	s.scanMu.Lock()
	cancel := s.scanCancel
	active := s.scanDone != nil
	s.scanMu.Unlock()

	phase := s.currentPhase()
	if active {
		s.setPhase(PhaseCancelling, "Cancelando a Varredura...")
	}

	if cancel != nil {
		cancel()
	}
	s.Scanner.Cancel()
	s.Hasher.Engine.Cancel()

	if !active && isBusyPhase(phase) {
		// Fase ocupada sem ciclo em curso: nada resta rodando.
		s.setPhase(PhaseCancelled, "Varredura cancelada.")
	}
	s.broadcastStatus()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "cancelling"})
}

func (s *AppServer) handleGetStatus(w http.ResponseWriter, r *http.Request) {
	st := s.Scanner.GetStatus()
	st.MemoryStats = GetLiveMemoryStats()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(st)
}

func (s *AppServer) handleGetMemoryStats(w http.ResponseWriter, r *http.Request) {
	stats := GetLiveMemoryStats()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(stats)
}

func (s *AppServer) handleGetTree(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	depthStr := r.URL.Query().Get("depth")
	depth := 4
	if d, err := strconv.Atoi(depthStr); err == nil && d > 0 {
		depth = d
	}
	if depth > 8 {
		depth = 8
	}

	if DebugMode {
		log.Printf("[DEBUG /api/tree] Consulta de árvore: path=%q depth=%d\n", path, depth)
	}

	w.Header().Set("Content-Type", "application/json")

	// A árvore nunca devolve mais que DefaultSummaryMaxFiles arquivos diretos
	// por pasta; fileCount e directFileCount contam os que existem de verdade e
	// a paginação completa fica em /api/tree/files (contrato 1.4).
	if path == "" || path == "Meus Discos" {
		rootSummaries := make([]*scanner.DirSummary, 0)
		tree := s.Tree()
		roots := tree.GetRootsSnapshot()
		for _, rNode := range roots {
			summary := tree.GetDirSummary(rNode.Path(), depth, scanner.DefaultSummaryMaxFiles)
			if summary != nil {
				rootSummaries = append(rootSummaries, summary)
			}
		}
		_ = json.NewEncoder(w).Encode(rootSummaries)
		return
	}

	summary := s.Tree().GetDirSummary(path, depth, scanner.DefaultSummaryMaxFiles)
	if summary == nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"path":            path,
			"name":            filepath.Base(path),
			"totalSize":       0,
			"fileCount":       0,
			"directFileCount": 0,
			"subDirCount":     0,
			"subDirs":         []*scanner.DirSummary{},
			"files":           []*scanner.FileNode{},
		})
		return
	}

	_ = json.NewEncoder(w).Encode(summary)
}

// TreeFilesPage é a resposta paginada de /api/tree/files (contrato 1.4).
type TreeFilesPage struct {
	Path   string              `json:"path"`
	Total  int                 `json:"total"`
	Offset int                 `json:"offset"`
	Limit  int                 `json:"limit"`
	SortBy string              `json:"sortBy"`
	Files  []*scanner.FileNode `json:"files"`
}

func (s *AppServer) handleGetTreeFiles(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	path := q.Get("path")
	offset, _ := strconv.Atoi(q.Get("offset"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	sortBy := q.Get("sortBy")

	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > scanner.MaxFilesPageLimit {
		limit = scanner.MaxFilesPageLimit
	}
	switch sortBy {
	case scanner.SortNameAsc, scanner.SortModDesc, scanner.SortSizeDesc:
	default:
		sortBy = scanner.SortSizeDesc
	}

	if path == "" {
		http.Error(w, "Parâmetro 'path' é obrigatório", http.StatusBadRequest)
		return
	}

	total, files := s.Tree().GetFilesPage(path, offset, limit, sortBy)
	if files == nil {
		files = []*scanner.FileNode{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(TreeFilesPage{
		Path:   path,
		Total:  total,
		Offset: offset,
		Limit:  limit,
		SortBy: sortBy,
		Files:  files,
	})
}

func (s *AppServer) handleGetDuplicates(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sortBy := q.Get("sortBy")
	if sortBy == "" {
		sortBy = "size_desc"
	}
	minSize, _ := strconv.ParseInt(q.Get("minSize"), 10, 64)
	ext := q.Get("extension")
	search := q.Get("search")
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))

	if limit <= 0 {
		limit = 50 // Safe default
	} else if limit > 500 {
		limit = 500
	}

	filter := indexer.QueryFilter{
		SortBy:    sortBy,
		MinSize:   minSize,
		Extension: ext,
		Search:    search,
		Limit:     limit,
		Offset:    offset,
	}

	// Sem reconstrução por "zero grupos": o índice é mantido de forma
	// incremental pelo Monitoramento e reconstruído no fim da Varredura. Um
	// filtro que não casa nada não pode custar uma varredura da árvore inteira.
	res := s.Index.Query(filter)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

// handleGetIdleFiles finds stale/unused files taking up disk space with in-place streaming
func (s *AppServer) handleGetIdleFiles(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	minAgeDays, _ := strconv.Atoi(q.Get("minAgeDays"))
	minSize, _ := strconv.ParseInt(q.Get("minSize"), 10, 64)
	ext := q.Get("extension")
	search := q.Get("search")
	sortBy := q.Get("sortBy")
	offset, _ := strconv.Atoi(q.Get("offset"))
	limit, _ := strconv.Atoi(q.Get("limit"))

	summary := indexer.QueryIdleFilesStreaming(s.Tree(), minAgeDays, minSize, ext, search, sortBy, offset, limit)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(summary)
}

func (s *AppServer) handleGetExtensionStats(w http.ResponseWriter, r *http.Request) {
	statsList := s.Tree().AggregateExtensionStats()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(statsList)
}

func (s *AppServer) handleGetLogs(w http.ResponseWriter, r *http.Request) {
	s.recentLogsMu.RLock()
	defer s.recentLogsMu.RUnlock()

	logs := make([]scanner.FSEventLog, len(s.recentLogs))
	copy(logs, s.recentLogs)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(logs)
}

// handleGetSkippedLogs devolve os Itens Pulados da Varredura corrente
// (contrato 1.10). Os mais recentes ficam no fim da lista.
func (s *AppServer) handleGetSkippedLogs(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 200
	}
	if limit > 500 {
		limit = 500
	}

	entries := s.Scanner.GetSkipped()
	if len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}
	if entries == nil {
		entries = []scanner.SkippedEntry{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(entries)
}

func (s *AppServer) handleListErrorLogs(w http.ResponseWriter, r *http.Request) {
	list, err := scanner.ListDiskErrorLogs("logs")
	if err != nil {
		http.Error(w, fmt.Sprintf("Erro ao listar logs de erro: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
}

func (s *AppServer) handleGetActiveErrorLog(w http.ResponseWriter, r *http.Request) {
	currentPath := s.Scanner.LoggerPath()
	status := s.Scanner.GetStatus()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"activeLogPath": currentPath,
		"errorsCount":   status.ErrorsCount,
		"skippedCount":  status.SkippedCount,
	})
}

// registerScanRoutes registers routes owned by the scan/state area (Agente S2).
func (s *AppServer) registerScanRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/scan/start", s.handleStartScan)
	mux.HandleFunc("/api/scan/cancel", s.handleCancelScan)
	mux.HandleFunc("/api/scan/status", s.handleGetStatus)
	mux.HandleFunc("/api/tree", s.handleGetTree)
	mux.HandleFunc("/api/tree/files", s.handleGetTreeFiles)
	mux.HandleFunc("/api/duplicates", s.handleGetDuplicates)
	mux.HandleFunc("/api/stats/extensions", s.handleGetExtensionStats)
	mux.HandleFunc("/api/stats/idle-files", s.handleGetIdleFiles)
	mux.HandleFunc("/api/events", s.handleSSE)
	mux.HandleFunc("/api/logs", s.handleGetLogs)
	mux.HandleFunc("/api/logs/skipped", s.handleGetSkippedLogs)
	mux.HandleFunc("/api/logs/errors/list", s.handleListErrorLogs)
	mux.HandleFunc("/api/logs/errors/active", s.handleGetActiveErrorLog)
	mux.HandleFunc("/api/system/memory", s.handleGetMemoryStats)
	mux.HandleFunc("/api/cache/save", s.handleSaveCache)
	mux.HandleFunc("/api/cache/load", s.handleLoadCache)
	mux.HandleFunc("/api/cache/list", s.handleListCaches)
	mux.HandleFunc("/api/cache/autosave/status", s.handleGetAutoSaveStatus)
	mux.HandleFunc("/api/cache/autosave/restore", s.handleRestoreAutoSave)
	mux.HandleFunc("/api/folders/duplicates", s.handleGetFolderDuplicates)
	mux.HandleFunc("/api/folders/compare", s.handleCompareFolders)
}
