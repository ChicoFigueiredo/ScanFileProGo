package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"scanfile/pkg/hasher"
	"scanfile/pkg/indexer"
	"scanfile/pkg/scanner"
	"scanfile/pkg/watcher"
	"strconv"
	"time"
)

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

	if path == "" || path == "Meus Discos" {
		// Return summary of all roots using snapshot to avoid mutex deadlocks
		rootSummaries := make([]*scanner.DirSummary, 0)
		roots := s.Tree.GetRootsSnapshot()
		for _, rNode := range roots {
			summary := s.Tree.GetDirSummary(rNode.Path, depth)
			if summary != nil {
				rootSummaries = append(rootSummaries, summary)
			}
		}
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

func (s *AppServer) handleGetLogs(w http.ResponseWriter, r *http.Request) {
	s.recentLogsMu.RLock()
	defer s.recentLogsMu.RUnlock()

	logs := make([]scanner.FSEventLog, len(s.recentLogs))
	copy(logs, s.recentLogs)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(logs)
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

// registerScanRoutes registers routes owned by the scan/state area (Agente S2).
func (s *AppServer) registerScanRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/scan/start", s.handleStartScan)
	mux.HandleFunc("/api/scan/cancel", s.handleCancelScan)
	mux.HandleFunc("/api/scan/status", s.handleGetStatus)
	mux.HandleFunc("/api/tree", s.handleGetTree)
	mux.HandleFunc("/api/duplicates", s.handleGetDuplicates)
	mux.HandleFunc("/api/stats/extensions", s.handleGetExtensionStats)
	mux.HandleFunc("/api/stats/idle-files", s.handleGetIdleFiles)
	mux.HandleFunc("/api/events", s.handleSSE)
	mux.HandleFunc("/api/logs", s.handleGetLogs)
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
