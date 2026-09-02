package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"scanfile/pkg/scanner"
	"strings"
	"time"
)

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
