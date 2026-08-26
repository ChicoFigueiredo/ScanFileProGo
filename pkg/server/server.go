package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"scanfile/pkg/config"
	"scanfile/pkg/drives"
	"scanfile/pkg/hasher"
	"scanfile/pkg/indexer"
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

// NewAppServer creates and initializes an AppServer.
func NewAppServer(uiFS fs.FS) *AppServer {
	tree := scanner.NewTreeManager()
	idx := indexer.NewDuplicateIndex()
	fIdx := indexer.NewFolderDuplicateIndex()
	sc := scanner.NewScanner(tree)
	hEngine := hasher.NewHasher()

	return &AppServer{
		Tree:        tree,
		Scanner:     sc,
		Hasher:      &HasherManager{Engine: hEngine},
		Index:       idx,
		FolderIndex: fIdx,
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

	// Cache Persistence Routes
	mux.HandleFunc("/api/cache/save", s.handleSaveCache)
	mux.HandleFunc("/api/cache/load", s.handleLoadCache)
	mux.HandleFunc("/api/cache/list", s.handleListCaches)

	// Folder Comparison & Duplicate Folder Routes
	mux.HandleFunc("/api/folders/duplicates", s.handleGetFolderDuplicates)
	mux.HandleFunc("/api/folders/compare", s.handleCompareFolders)

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
		Handler:      s.corsMiddleware(mux),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	go func() {
		_ = s.httpServer.Serve(ln)
	}()

	return fmt.Sprintf("http://%s", ln.Addr().String()), nil
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

	// PHASE 1: Metadata scan
	_ = s.Scanner.StartScan(ctx, config, func(st scanner.ScanStatus) {
		s.broadcastSSE("scan_progress", st)
	})

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

	loadedTree, snapshot, err := scanner.LoadCacheFromFile(req.FilePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Erro ao carregar cache: %v", err), http.StatusInternalServerError)
		return
	}

	// Replace active Tree
	s.Tree = loadedTree
	s.Scanner.Tree = loadedTree
	s.activeRoots = snapshot.Roots
	s.lastConfig = snapshot.ScanSettings

	// Rebuild Duplicate Index
	allFiles := s.Tree.GetAllFiles()
	s.Index.RebuildIndex(allFiles)
	grpCount, fileCount, wasted := s.Index.GetSummaryStats()

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

	filter := indexer.FolderQueryFilter{
		SortBy:  sortBy,
		MinSize: minSize,
		Search:  search,
		Limit:   limit,
		Offset:  offset,
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
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(st)
}

func (s *AppServer) handleGetTree(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	depthStr := r.URL.Query().Get("depth")
	depth := 1
	if d, err := strconv.Atoi(depthStr); err == nil && d > 0 {
		depth = d
	}

	if path == "" {
		// Return summary of all roots
		var rootSummaries []*scanner.DirSummary
		s.Tree.RootsLock(func(roots map[string]*scanner.DirNode) {
			for _, rNode := range roots {
				summary := s.Tree.GetDirSummary(rNode.Path, depth)
				if summary != nil {
					rootSummaries = append(rootSummaries, summary)
				}
			}
		})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rootSummaries)
		return
	}

	summary := s.Tree.GetDirSummary(path, depth)
	if summary == nil {
		http.Error(w, "Directory not found in tree", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
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

// handleGetIdleFiles finds stale/unused files taking up disk space
func (s *AppServer) handleGetIdleFiles(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	minAgeDays, _ := strconv.Atoi(q.Get("minAgeDays"))
	minSize, _ := strconv.ParseInt(q.Get("minSize"), 10, 64)
	ext := q.Get("extension")
	search := q.Get("search")
	limit, _ := strconv.Atoi(q.Get("limit"))

	allFiles := s.Tree.GetAllFiles()
	summary := indexer.QueryIdleFiles(allFiles, minAgeDays, minSize, ext, search, limit)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(summary)
}

// ExtensionStat aggregates count and size per file extension.
type ExtensionStat struct {
	Extension  string  `json:"extension"`
	TotalBytes int64   `json:"totalBytes"`
	FileCount  int     `json:"fileCount"`
	Percentage float64 `json:"percentage"`
}

func (s *AppServer) handleGetExtensionStats(w http.ResponseWriter, r *http.Request) {
	allFiles := s.Tree.GetAllFiles()
	extMap := make(map[string]*ExtensionStat)
	var grandTotalBytes int64

	for _, f := range allFiles {
		ext := strings.ToLower(f.Extension)
		if ext == "" {
			ext = "(sem extensão)"
		}
		st, ok := extMap[ext]
		if !ok {
			st = &ExtensionStat{Extension: ext}
			extMap[ext] = st
		}
		st.TotalBytes += f.Size
		st.FileCount++
		grandTotalBytes += f.Size
	}

	var statsList []*ExtensionStat
	for _, st := range extMap {
		if grandTotalBytes > 0 {
			st.Percentage = (float64(st.TotalBytes) / float64(grandTotalBytes)) * 100.0
		}
		statsList = append(statsList, st)
	}

	sort.Slice(statsList, func(i, j int) bool {
		return statsList[i].TotalBytes > statsList[j].TotalBytes
	})

	if len(statsList) > 20 {
		statsList = statsList[:20]
	}

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


