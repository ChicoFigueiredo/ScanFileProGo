package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"scanfile/pkg/ai"
	"scanfile/pkg/config"
	"scanfile/pkg/drives"
	"scanfile/pkg/hasher"
	"scanfile/pkg/indexer"
	"scanfile/pkg/mcp"
	"scanfile/pkg/privileges"
	"scanfile/pkg/recycle"
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
		cfg.AIOpenRouterKey,
		"",
		&serverToolsExecutor{mcpCtx: mcpCtx},
		toolsDefs,
	)

	return &AppServer{
		Tree:        tree,
		Scanner:     sc,
		Hasher:      &HasherManager{Engine: hEngine},
		Index:       idx,
		FolderIndex: fIdx,
		MCPContext:  mcpCtx,
		AIAgent:     agent,
		uiFS:        uiFS,
		sseClients:  make(map[chan string]bool),
		recentLogs:  make([]scanner.FSEventLog, 0, 100),
	}
}

// Start launches the local HTTP/SSE server on an ephemeral or designated port.
func (s *AppServer) Start(port int) (string, error) {
	mux := http.NewServeMux()

	// API Routes
	mux.HandleFunc("/api/drives", s.handleGetDrives)
	mux.HandleFunc("/api/scan/start", s.handleStartScan)
	mux.HandleFunc("/api/scan/cancel", s.handleCancelScan)
	mux.HandleFunc("/api/scan/status", s.handleGetStatus)
	mux.HandleFunc("/api/tree", s.handleGetTree)
	mux.HandleFunc("/api/duplicates", s.handleGetDuplicates)
	mux.HandleFunc("/api/stats/extensions", s.handleGetExtensionStats)
	mux.HandleFunc("/api/stats/idle-files", s.handleGetIdleFiles)
	mux.HandleFunc("/api/files/recycle", s.handleRecycleFiles)
	mux.HandleFunc("/api/files/delete", s.handleDeleteFiles)
	mux.HandleFunc("/api/events", s.handleSSE)
	// Logs & Error Log Routes
	mux.HandleFunc("/api/logs", s.handleGetLogs)
	mux.HandleFunc("/api/logs/errors/list", s.handleListErrorLogs)
	mux.HandleFunc("/api/logs/errors/active", s.handleGetActiveErrorLog)

	// System Privilege & UAC Elevation Routes
	mux.HandleFunc("/api/system/privileges", s.handleGetPrivileges)
	mux.HandleFunc("/api/system/elevate", s.handleElevateProcess)
	mux.HandleFunc("/api/system/memory", s.handleGetMemoryStats)

	// Cache Persistence & AutoSave Routes
	mux.HandleFunc("/api/cache/save", s.handleSaveCache)
	mux.HandleFunc("/api/cache/load", s.handleLoadCache)
	mux.HandleFunc("/api/cache/list", s.handleListCaches)
	mux.HandleFunc("/api/cache/autosave/status", s.handleGetAutoSaveStatus)
	mux.HandleFunc("/api/cache/autosave/restore", s.handleRestoreAutoSave)

	// Folder Comparison & Duplicate Folder Routes
	mux.HandleFunc("/api/folders/duplicates", s.handleGetFolderDuplicates)
	mux.HandleFunc("/api/folders/compare", s.handleCompareFolders)

	// AI Assistant Routes
	mux.HandleFunc("/api/ai/models", s.handleAIModels)
	mux.HandleFunc("/api/ai/models/pull", s.handleAIPullModel)
	mux.HandleFunc("/api/ai/chat", s.handleAIChat)
	mux.HandleFunc("/api/ai/actions/execute", s.handleAIExecuteAction)
	mux.HandleFunc("/api/ai/status", s.handleAIStatus)

	// User Preferences Config Routes
	mux.HandleFunc("/api/config", s.handleConfig)

	// Static UI assets
	if s.uiFS != nil {
		mux.Handle("/", http.FileServer(http.FS(s.uiFS)))
	}

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		// Fallback to any available port
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return "", err
		}
	}

	s.listener = ln
	s.httpServer = &http.Server{
		Handler:      s.corsMiddleware(s.debugMiddleware(mux)),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	go func() {
		_ = s.httpServer.Serve(ln)
	}()

	return fmt.Sprintf("http://%s", ln.Addr().String()), nil
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

// Stop gracefully stops the server.
func (s *AppServer) Stop() {
	if s.Watcher != nil {
		s.Watcher.Stop()
	}
	if s.httpServer != nil {
		_ = s.httpServer.Shutdown(context.Background())
	}
}

func (s *AppServer) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *AppServer) broadcastSSE(eventType string, data any) {
	bytes, err := json.Marshal(data)
	if err != nil {
		return
	}
	msg := fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, string(bytes))

	s.eventsMu.RLock()
	defer s.eventsMu.RUnlock()
	for ch := range s.sseClients {
		select {
		case ch <- msg:
		default:
		}
	}
}

func (s *AppServer) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan string, 100)
	s.eventsMu.Lock()
	s.sseClients[ch] = true
	s.eventsMu.Unlock()

	defer func() {
		s.eventsMu.Lock()
		delete(s.sseClients, ch)
		s.eventsMu.Unlock()
		close(ch)
	}()

	notify := r.Context().Done()
	for {
		select {
		case <-notify:
			return
		case msg := <-ch:
			_, _ = fmt.Fprint(w, msg)
			flusher.Flush()
		}
	}
}

func (s *AppServer) handleGetDrives(w http.ResponseWriter, r *http.Request) {
	driveList, err := drives.GetLogicalDrives()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(driveList)
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

	if config.HashAlgorithm == "" {
		config.HashAlgorithm = "xxhash"
	}
	if config.MinSizeForHash == 0 {
		config.MinSizeForHash = 1
	}

	s.activeRoots = config.Roots
	s.lastConfig = config

	// Quick Scan Mode: Prepare fast lookup map from previous in-memory tree or latest autosave
	if config.QuickScanMode {
		var lookup map[string]*scanner.FileNode
		if s.Tree != nil && s.Tree.GetTotalFileCount() > 0 {
			lookup = scanner.BuildQuickScanLookup(&scanner.CacheSnapshot{Files: s.Tree.GetAllFiles()})
		} else if autoInfo, err := scanner.GetLatestAutoSave("saved_scans"); err == nil {
			if _, snap, err := scanner.LoadCacheFromFile(autoInfo.FilePath); err == nil {
				lookup = scanner.BuildQuickScanLookup(snap)
			}
		}
		s.Scanner.SetQuickScanLookup(lookup)
	} else {
		s.Scanner.SetQuickScanLookup(nil)
	}

	// Reset tree and index for fresh scan
	s.Tree.Reset()
	if s.Watcher != nil {
		s.Watcher.Stop()
	}

	// Launch background orchestration
	go s.orchestrateScan(config)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "started"})
}

func (s *AppServer) orchestrateScan(config scanner.ScanConfig) {
	ctx := context.Background()

	// Periodic AutoSave Background Ticker
	autoInterval := config.AutoSaveIntervalMinutes
	if autoInterval <= 0 {
		autoInterval = 5 // Default: autosave every 5 minutes
	}
	autoTicker := time.NewTicker(time.Duration(autoInterval) * time.Minute)
	defer autoTicker.Stop()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-autoTicker.C:
				if s.Tree != nil && s.Tree.GetTotalFileCount() > 0 {
					savedPath, err := scanner.SaveAutoSave(s.Tree, s.activeRoots, config, "saved_scans")
					if err == nil {
						now := time.Now().Unix()
						s.Scanner.SetStatus(func(st *scanner.ScanStatus) {
							st.LastAutoSaveTime = now
							st.AutoSaveFilePath = savedPath
						})
						s.broadcastSSE("autosave_done", map[string]any{
							"filePath": savedPath,
							"time":     now,
						})
					}
				}
			}
		}
	}()

	// PHASE 1: Metadata scan
	_ = s.Scanner.StartScan(ctx, config, func(st scanner.ScanStatus) {
		s.broadcastSSE("scan_progress", st)
	})

	// AutoSave immediately after Phase 1 finishes
	if p1Path, err := scanner.SaveAutoSave(s.Tree, s.activeRoots, config, "saved_scans"); err == nil {
		s.Scanner.SetStatus(func(st *scanner.ScanStatus) {
			st.LastAutoSaveTime = time.Now().Unix()
			st.AutoSaveFilePath = p1Path
		})
	}

	// Get all scanned files
	allFiles := s.Tree.GetAllFiles()

	// PHASE 2: Content Hashing
	s.Scanner.SetStatus(func(st *ScanStatus) {
		st.Phase = "phase2_hashing"
		st.CurrentPath = "Iniciando cálculo de hashes em multithread..."
	})

	opts := hasher.ComputeHashOptions{
		Algorithm:     config.HashAlgorithm,
		HashAllFiles:  config.HashAllFiles,
		MinSize:       config.MinSizeForHash,
		WorkerThreads: config.WorkerThreads,
		DiskLogger:    s.Scanner.DiskLogger,
		OnProgress: func(hashedCount, totalCount, bytesHashed int64, currentPath string, rateMBps float64, activeWorkers []scanner.ActiveWorker, recentFiles []scanner.FileLogEntry) {
			s.Scanner.SetStatus(func(st *ScanStatus) {
				st.FilesToHashCount = totalCount
				st.FilesHashedCount = hashedCount
				st.BytesHashed = bytesHashed
				st.CurrentPath = currentPath
				st.HashRateMBPerSec = rateMBps
				st.ActiveWorkers = activeWorkers
				st.RecentFiles = recentFiles
				if totalCount > 0 {
					st.ProgressPercent = (float64(hashedCount) / float64(totalCount)) * 100.0
				}
			})
			s.broadcastSSE("scan_progress", s.Scanner.GetStatus())
		},
	}

	_ = s.Hasher.Engine.RunHashing(ctx, allFiles, opts)

	// Rebuild duplicate file index
	s.Index.RebuildIndex(allFiles)
	grpCount, fileCount, wasted := s.Index.GetSummaryStats()

	// Rebuild duplicate folder index
	s.FolderIndex.RebuildFolderIndex(s.Tree)
	fGrpCount, fCount, fWasted := s.FolderIndex.GetSummaryStats()

	// AutoSave after Phase 2 / full scan completion
	if finalPath, err := scanner.SaveAutoSave(s.Tree, s.activeRoots, config, "saved_scans"); err == nil {
		s.Scanner.SetStatus(func(st *scanner.ScanStatus) {
			st.LastAutoSaveTime = time.Now().Unix()
			st.AutoSaveFilePath = finalPath
		})
	}

	s.Scanner.SetStatus(func(st *ScanStatus) {
		st.Phase = "completed"
		st.CurrentPath = "Varredura, indexação de hashes e análise de pastas finalizadas com sucesso!"
		st.DuplicateGroupsCount = grpCount
		st.DuplicateFilesCount = fileCount
		st.DuplicateWastedBytes = wasted
		st.DuplicateFolderGroupsCount = fGrpCount
		st.DuplicateFoldersCount = fCount
		st.DuplicateFolderWastedBytes = fWasted
		st.ProgressPercent = 100
	})
	s.broadcastSSE("scan_progress", s.Scanner.GetStatus())

	// Start Windows Real-time Watcher hooks
	w, err := watcher.NewFSWatcher(s.Tree, s.Index, config.HashAlgorithm)
	if err == nil {
		s.Watcher = w
		_ = s.Watcher.Start(ctx, config.Roots, func(ev scanner.FSEventLog) {
			s.recentLogsMu.Lock()
			s.recentLogs = append(s.recentLogs, ev)
			if len(s.recentLogs) > 200 {
				s.recentLogs = s.recentLogs[len(s.recentLogs)-200:]
			}
			s.recentLogsMu.Unlock()

			// Update duplicate stats in status
			g, f, wasted := s.Index.GetSummaryStats()
			s.Scanner.SetStatus(func(st *ScanStatus) {
				st.DuplicateGroupsCount = g
				st.DuplicateFilesCount = f
				st.DuplicateWastedBytes = wasted
				st.IsWatching = true
			})

			s.broadcastSSE("fs_event", ev)
		})

		s.Scanner.SetStatus(func(st *ScanStatus) {
			st.Phase = "watching"
			st.IsWatching = true
		})
		s.broadcastSSE("scan_progress", s.Scanner.GetStatus())
	}
}

func (s *AppServer) handleGetAutoSaveStatus(w http.ResponseWriter, r *http.Request) {
	info, err := scanner.GetLatestAutoSave("saved_scans")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"exists": false})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"exists":   true,
		"autoSave": info,
	})
}

func (s *AppServer) handleRestoreAutoSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	info, err := scanner.GetLatestAutoSave("saved_scans")
	if err != nil {
		http.Error(w, "Nenhum arquivo de autosave encontrado", http.StatusNotFound)
		return
	}

	s.Scanner.SetStatus(func(st *scanner.ScanStatus) {
		st.Phase = "loading_cache"
		st.CurrentPath = fmt.Sprintf("Carregando snapshot de autosave: %s", filepath.Base(info.FilePath))
		st.ProgressPercent = 5
	})
	s.broadcastSSE("scan_progress", s.Scanner.GetStatus())

	tm, snapshot, err := scanner.LoadCacheFromFileWithProgress(info.FilePath, func(stage string, percent float64, details string) {
		s.Scanner.SetStatus(func(st *scanner.ScanStatus) {
			st.Phase = "loading_cache"
			st.CurrentPath = fmt.Sprintf("%s - %s", stage, details)
			st.ProgressPercent = percent
		})
		s.broadcastSSE("scan_progress", s.Scanner.GetStatus())
	})
	if err != nil {
		s.Scanner.SetStatus(func(st *scanner.ScanStatus) {
			st.Phase = "idle"
			st.ProgressPercent = 0
		})
		s.broadcastSSE("scan_progress", s.Scanner.GetStatus())
		http.Error(w, "Erro ao carregar autosave: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.Scanner.SetStatus(func(st *scanner.ScanStatus) {
		st.Phase = "loading_cache"
		st.CurrentPath = "Reconstruindo índice de duplicatas por hash..."
		st.ProgressPercent = 88
	})
	s.broadcastSSE("scan_progress", s.Scanner.GetStatus())

	s.Tree = tm
	s.activeRoots = snapshot.Roots
	s.lastConfig = snapshot.ScanSettings

	allFiles := s.Tree.GetAllFiles()
	s.Index.RebuildIndex(allFiles)
	grpCount, fileCount, wasted := s.Index.GetSummaryStats()

	s.Scanner.SetStatus(func(st *scanner.ScanStatus) {
		st.Phase = "loading_cache"
		st.CurrentPath = "Identificando e classificando pastas clones..."
		st.ProgressPercent = 95
	})
	s.broadcastSSE("scan_progress", s.Scanner.GetStatus())

	s.FolderIndex.RebuildFolderIndex(s.Tree)
	fGrpCount, fCount, fWasted := s.FolderIndex.GetSummaryStats()

	s.Scanner.SetStatus(func(st *scanner.ScanStatus) {
		st.Phase = "completed"
		st.CurrentPath = fmt.Sprintf("Autosave restaurado com sucesso (%d arquivos).", snapshot.TotalFiles)
		st.TotalFilesScanned = snapshot.TotalFiles
		st.TotalDirsScanned = snapshot.TotalDirs
		st.TotalBytesScanned = snapshot.TotalBytes
		st.TotalAllocatedBytesScanned = snapshot.TotalAllocatedBytes
		st.DuplicateGroupsCount = grpCount
		st.DuplicateFilesCount = fileCount
		st.DuplicateWastedBytes = wasted
		st.DuplicateFolderGroupsCount = fGrpCount
		st.DuplicateFoldersCount = fCount
		st.DuplicateFolderWastedBytes = fWasted
		st.ProgressPercent = 100
		st.LastAutoSaveTime = info.ModTime.Unix()
		st.AutoSaveFilePath = info.FilePath
	})

	s.broadcastSSE("scan_progress", s.Scanner.GetStatus())

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":   "restored",
		"snapshot": snapshot,
	})
}

// SaveCacheReq parameters for saving in-memory cache to disk.
type SaveCacheReq struct {
	FileName string `json:"fileName,omitempty"`
	FilePath string `json:"filePath,omitempty"`
}

func (s *AppServer) handleSaveCache(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req SaveCacheReq
	_ = json.NewDecoder(r.Body).Decode(&req)

	targetPath := req.FilePath
	if targetPath == "" {
		fileName := req.FileName
		if fileName == "" {
			fileName = fmt.Sprintf("scanfile_cache_%s.scanfile.gz", time.Now().Format("2006-01-02_150405"))
		}
		if !strings.HasSuffix(fileName, ".scanfile.gz") && !strings.HasSuffix(fileName, ".scanfile") && !strings.HasSuffix(fileName, ".json.gz") {
			fileName += ".scanfile.gz"
		}
		targetPath = filepath.Join("saved_scans", fileName)
	}

	err := scanner.SaveCacheToFile(s.Tree, s.activeRoots, s.lastConfig, targetPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Erro ao salvar cache: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":   "saved",
		"filePath": targetPath,
		"fileName": filepath.Base(targetPath),
	})
}

// LoadCacheReq parameters for loading saved cache.
type LoadCacheReq struct {
	FilePath string `json:"filePath"`
}

func (s *AppServer) handleLoadCache(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req LoadCacheReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.FilePath == "" {
		http.Error(w, "Caminho do arquivo de cache não especificado", http.StatusBadRequest)
		return
	}

	s.Scanner.SetStatus(func(st *ScanStatus) {
		st.Phase = "loading_cache"
		st.CurrentPath = fmt.Sprintf("Carregando snapshot de cache: %s", filepath.Base(req.FilePath))
		st.ProgressPercent = 5
	})
	s.broadcastSSE("scan_progress", s.Scanner.GetStatus())

	loadedTree, snapshot, err := scanner.LoadCacheFromFileWithProgress(req.FilePath, func(stage string, percent float64, details string) {
		s.Scanner.SetStatus(func(st *ScanStatus) {
			st.Phase = "loading_cache"
			st.CurrentPath = fmt.Sprintf("%s - %s", stage, details)
			st.ProgressPercent = percent
		})
		s.broadcastSSE("scan_progress", s.Scanner.GetStatus())
	})
	if err != nil {
		s.Scanner.SetStatus(func(st *ScanStatus) {
			st.Phase = "idle"
			st.ProgressPercent = 0
		})
		s.broadcastSSE("scan_progress", s.Scanner.GetStatus())
		http.Error(w, fmt.Sprintf("Erro ao carregar cache: %v", err), http.StatusInternalServerError)
		return
	}

	s.Scanner.SetStatus(func(st *ScanStatus) {
		st.Phase = "loading_cache"
		st.CurrentPath = "Reconstruindo índice de arquivos duplicados por hash..."
		st.ProgressPercent = 88
	})
	s.broadcastSSE("scan_progress", s.Scanner.GetStatus())

	// Replace active Tree
	s.Tree = loadedTree
	s.Scanner.Tree = loadedTree
	s.activeRoots = snapshot.Roots
	s.lastConfig = snapshot.ScanSettings

	// Rebuild Duplicate Index
	allFiles := s.Tree.GetAllFiles()
	s.Index.RebuildIndex(allFiles)
	grpCount, fileCount, wasted := s.Index.GetSummaryStats()

	s.Scanner.SetStatus(func(st *ScanStatus) {
		st.Phase = "loading_cache"
		st.CurrentPath = "Identificando e classificando pastas clones..."
		st.ProgressPercent = 95
	})
	s.broadcastSSE("scan_progress", s.Scanner.GetStatus())

	// Rebuild Folder Duplicate Index
	s.FolderIndex.RebuildFolderIndex(s.Tree)
	fGrpCount, fCount, fWasted := s.FolderIndex.GetSummaryStats()

	s.Scanner.SetStatus(func(st *ScanStatus) {
		st.Phase = "completed"
		st.CurrentPath = fmt.Sprintf("Cache carregado com sucesso de %s (%s)", filepath.Base(req.FilePath), snapshot.Timestamp.Format("02/01/2006 15:04"))
		st.TotalFilesScanned = snapshot.TotalFiles
		st.TotalDirsScanned = snapshot.TotalDirs
		st.TotalBytesScanned = snapshot.TotalBytes
		st.DuplicateGroupsCount = grpCount
		st.DuplicateFilesCount = fileCount
		st.DuplicateWastedBytes = wasted
		st.DuplicateFolderGroupsCount = fGrpCount
		st.DuplicateFoldersCount = fCount
		st.DuplicateFolderWastedBytes = fWasted
		st.ProgressPercent = 100
	})
	s.broadcastSSE("scan_progress", s.Scanner.GetStatus())

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":      "loaded",
		"snapshot":    snapshot,
		"groupCount":  grpCount,
		"fileCount":   fileCount,
		"wastedBytes": wasted,
	})
}

func (s *AppServer) handleListCaches(w http.ResponseWriter, r *http.Request) {
	list, err := scanner.ListSavedCaches("saved_scans")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
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

	res := s.FolderIndex.Query(filter)
	if res.TotalGroups == 0 {
		// Dynamic query fallback: Rebuild using what has already been hashed in memory
		s.FolderIndex.RebuildFolderIndex(s.Tree)
		res = s.FolderIndex.Query(filter)
	}

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

	result, err := indexer.CompareFolders(s.Tree, pathA, pathB)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// Type alias for status inside server
type ScanStatus = scanner.ScanStatus

func (s *AppServer) handleCancelScan(w http.ResponseWriter, r *http.Request) {
	s.Scanner.Cancel()
	s.Hasher.Engine.Cancel()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "cancelled"})
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

func getSystemPhysicalMemory(payload *scanner.MemoryStatsPayload, allocBytes uint64) {
	if runtime.GOOS == "windows" {
		type memoryStatusEx struct {
			cbSize                  uint32
			dwMemoryLoad            uint32
			ullTotalPhys            uint64
			ullAvailPhys            uint64
			ullTotalPageFile        uint64
			ullAvailPageFile        uint64
			ullTotalVirtual         uint64
			ullAvailVirtual         uint64
			ullAvailExtendedVirtual uint64
		}
		var stat memoryStatusEx
		stat.cbSize = uint32(unsafe.Sizeof(stat))
		kernel32 := syscall.NewLazyDLL("kernel32.dll")
		globalMemoryStatusEx := kernel32.NewProc("GlobalMemoryStatusEx")
		ret, _, _ := globalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&stat)))
		if ret != 0 && stat.ullTotalPhys > 0 {
			payload.SystemTotalRAMMB = stat.ullTotalPhys / (1024 * 1024)
			payload.SystemFreeRAMMB = stat.ullAvailPhys / (1024 * 1024)
			if stat.ullTotalPhys >= stat.ullAvailPhys {
				payload.SystemUsedRAMMB = (stat.ullTotalPhys - stat.ullAvailPhys) / (1024 * 1024)
			}
			payload.SystemPercent = float64(stat.dwMemoryLoad)
			if stat.ullTotalPhys > 0 {
				payload.AppPercentOfSys = (float64(allocBytes) / float64(stat.ullTotalPhys)) * 100.0
			}
		}
	}
}

func (s *AppServer) handleGetTree(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	depthStr := r.URL.Query().Get("depth")
	depth := 1
	if d, err := strconv.Atoi(depthStr); err == nil && d > 0 {
		depth = d
	}

	if DebugMode {
		log.Printf("[DEBUG /api/tree] Consulta de árvore: path=%q depth=%d\n", path, depth)
	}

	w.Header().Set("Content-Type", "application/json")

	if path == "" {
		// Return summary of all roots
		rootSummaries := make([]*scanner.DirSummary, 0)
		s.Tree.RootsLock(func(roots map[string]*scanner.DirNode) {
			for _, rNode := range roots {
				summary := s.Tree.GetDirSummary(rNode.Path, depth)
				if summary != nil {
					rootSummaries = append(rootSummaries, summary)
				}
			}
		})
		_ = json.NewEncoder(w).Encode(rootSummaries)
		return
	}

	summary := s.Tree.GetDirSummary(path, depth)
	if summary == nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"path":        path,
			"name":        filepath.Base(path),
			"totalSize":   0,
			"fileCount":   0,
			"subDirCount": 0,
			"subDirs":     []*scanner.DirSummary{},
			"files":       []*scanner.FileNode{},
		})
		return
	}

	_ = json.NewEncoder(w).Encode(summary)
}

// Helper method on scanner.TreeManager for locked roots iteration
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

	res := s.Index.Query(filter)
	if res.TotalGroups == 0 {
		// Dynamic query fallback: Rebuild using what has already been hashed in memory so far
		allFiles := s.Tree.GetAllFiles()
		s.Index.RebuildIndex(allFiles)
		res = s.Index.Query(filter)
	}

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

	summary := indexer.QueryIdleFilesStreaming(s.Tree, minAgeDays, minSize, ext, search, sortBy, offset, limit)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(summary)
}

func (s *AppServer) handleGetExtensionStats(w http.ResponseWriter, r *http.Request) {
	statsList := s.Tree.AggregateExtensionStats()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(statsList)
}


type BatchFileReq struct {
	Paths []string `json:"paths"`
}

func (s *AppServer) handleRecycleFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req BatchFileReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	sizes := make(map[string]int64)
	for _, p := range req.Paths {
		if f := s.findFileInTree(p); f != nil {
			sizes[p] = f.Size
		}
	}

	res := recycle.BatchDelete(req.Paths, sizes, true)

	// Update in-memory tree & index for successfully deleted items
	for _, p := range req.Paths {
		s.Tree.RemoveFile(p)
		s.Index.RemoveFileFromIndex(p)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

func (s *AppServer) handleDeleteFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req BatchFileReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	sizes := make(map[string]int64)
	for _, p := range req.Paths {
		if f := s.findFileInTree(p); f != nil {
			sizes[p] = f.Size
		}
	}

	res := recycle.BatchDelete(req.Paths, sizes, false)

	for _, p := range req.Paths {
		s.Tree.RemoveFile(p)
		s.Index.RemoveFileFromIndex(p)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

func (s *AppServer) handleGetLogs(w http.ResponseWriter, r *http.Request) {
	s.recentLogsMu.RLock()
	defer s.recentLogsMu.RUnlock()

	logs := make([]scanner.FSEventLog, len(s.recentLogs))
	copy(logs, s.recentLogs)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(logs)
}

func (s *AppServer) findFileInTree(filePath string) *scanner.FileNode {
	dir := filepath.Dir(filePath)
	node := s.Tree.FindDir(dir)
	if node == nil {
		return nil
	}
	for _, f := range node.Files {
		if f.Path == filePath {
			return f
		}
	}
	return nil
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
	var currentPath string
	if s.Scanner.DiskLogger != nil {
		currentPath = s.Scanner.DiskLogger.GetFilePath()
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"activeLogPath": currentPath,
		"errorsCount":   s.Scanner.GetStatus().ErrorsCount,
	})
}

func (s *AppServer) handleGetPrivileges(w http.ResponseWriter, r *http.Request) {
	status := privileges.CheckPrivilegeStatus()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

func (s *AppServer) handleElevateProcess(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	err := privileges.RelaunchAsAdmin()
	if err != nil {
		http.Error(w, fmt.Sprintf("Falha ao solicitar elevação UAC: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "elevating",
		"message": "Nova janela solicitada como Administrador com permissões completas.",
	})
}

func (s *AppServer) handleConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodPost {
		var cfg config.AppConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			http.Error(w, "Invalid configuration format: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := config.SaveConfig(cfg); err != nil {
			http.Error(w, "Failed to save configuration: "+err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
		return
	}

	cfg := config.LoadConfig()
	_ = json.NewEncoder(w).Encode(cfg)
}

// =========================================================================
// AI ASSISTANT & MCP HANDLERS
// =========================================================================

func (s *AppServer) handleAIModels(w http.ResponseWriter, r *http.Request) {
	cfg := config.LoadConfig()
	catalog := ai.BuildCatalog(r.Context(), cfg.AIOllamaEndpoint)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(catalog)
}

func (s *AppServer) handleAIStatus(w http.ResponseWriter, r *http.Request) {
	cfg := config.LoadConfig()
	tags, ver, err := s.AIAgent.OllamaClient.ListInstalledModels(r.Context())
	
	status := map[string]interface{}{
		"ollamaOnline":    err == nil,
		"ollamaVersion":   ver,
		"ollamaEndpoint":  cfg.AIOllamaEndpoint,
		"installedCount":  len(tags),
		"installedModels": tags,
		"hasOpenRouterKey": cfg.AIOpenRouterKey != "",
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

func (s *AppServer) handleAIPullModel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Model == "" {
		http.Error(w, "Nome do modelo é obrigatório", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	err := s.AIAgent.OllamaClient.PullModel(r.Context(), req.Model, func(p ai.PullProgress) {
		data, _ := json.Marshal(p)
		fmt.Fprintf(w, "data: %s\n\n", string(data))
		flusher.Flush()
	})

	if err != nil {
		errData, _ := json.Marshal(map[string]string{"error": err.Error()})
		fmt.Fprintf(w, "data: %s\n\n", string(errData))
		flusher.Flush()
		return
	}

	doneData, _ := json.Marshal(map[string]interface{}{"status": "success", "percent": 100})
	fmt.Fprintf(w, "data: %s\n\n", string(doneData))
	flusher.Flush()
}

func (s *AppServer) handleAIChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ai.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Payload JSON inválido: "+err.Error(), http.StatusBadRequest)
		return
	}

	cfg := config.LoadConfig()
	if req.Provider == "" {
		req.Provider = ai.ProviderType(cfg.AIProvider)
		if req.Provider == "" {
			req.Provider = ai.ProviderOllama
		}
	}
	if req.Model == "" {
		if req.Provider == ai.ProviderOpenRouter {
			req.Model = cfg.AIOpenRouterModel
		} else {
			req.Model = cfg.AIOllamaModel
		}
	}

	// Update live clients in Agent with latest config
	s.AIAgent.OllamaClient = ai.NewOllamaClient(cfg.AIOllamaEndpoint)
	s.AIAgent.OpenRouter = ai.NewOpenRouterClient(cfg.AIOpenRouterKey)

	systemPrompt := ai.BuildSystemPrompt(s.Tree, s.Index, req.SelectedFolder)

	// Stream responses via SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, hasFlusher := w.(http.Flusher)

	_, err := s.AIAgent.RunAgentExecution(r.Context(), req, systemPrompt, func(event ai.StreamEvent) {
		if hasFlusher {
			data, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", string(data))
			flusher.Flush()
		}
	})

	if err != nil {
		if hasFlusher {
			errEvent, _ := json.Marshal(ai.StreamEvent{
				Type:    "error",
				Content: err.Error(),
			})
			fmt.Fprintf(w, "data: %s\n\n", string(errEvent))
			flusher.Flush()
		}
	}
}

func (s *AppServer) handleAIExecuteAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ai.ActionExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ProposalID == "" {
		http.Error(w, "proposalId é obrigatório", http.StatusBadRequest)
		return
	}

	proposal, err := s.MCPContext.ExecuteProposal(req.ProposalID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Falha ao executar proposta %s: %v", req.ProposalID, err), http.StatusInternalServerError)
		return
	}

	res := ai.ActionExecuteResult{
		Success:    proposal.Executed,
		ActionType: proposal.ActionType,
		Affected:   proposal.FileCount,
		FreedBytes: proposal.TotalBytes,
		FreedSize:  proposal.TotalSize,
		Message:    fmt.Sprintf("Ação %s executada com sucesso para %d arquivos.", proposal.ActionType, proposal.FileCount),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}



