package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"scanfile/pkg/scanner"
)

func (s *AppServer) handleGetAutoSaveStatus(w http.ResponseWriter, r *http.Request) {
	info, err := scanner.GetLatestAutoSave(s.autoSaveDir())
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

// adoptSnapshot troca a árvore ativa pela recém-carregada e reconstrói os
// índices. Devolve os totais de duplicados para o status.
//
// A troca também alcança s.Scanner.Tree e s.MCPContext.Tree: sem isso a próxima
// Varredura continuava escrevendo na árvore antiga (achado C4) e o Assistente
// consultava um retrato que a interface não mostra mais.
//
// O Monitoramento para ANTES da troca. Ele capturou o TreeManager e os índices
// no momento em que subiu; como "watching" não é fase ocupada, carregar um
// Snapshot durante o Monitoramento o deixaria alterando a árvore descartada
// enquanto escreve no índice de duplicados recém-reconstruído. Religá-lo sobre
// as Raízes do Snapshot NÃO é automático: elas podem não existir nesta máquina
// (Snapshot trazido de outro disco ou de outro computador) e o desfecho
// publicado por estes handlers é "completed", não "watching". Quem quiser
// Monitoramento outra vez inicia uma Varredura.
func (s *AppServer) adoptSnapshot(tm *scanner.TreeManager, summary *scanner.CacheSnapshotSummary, progress func(percent float64, message string)) (grpCount, fileCount int, wasted int64, fGrpCount, fCount int, fWasted int64) {
	s.stopWatcher()

	progress(88, "Reconstruindo índice de arquivos duplicados por hash...")

	s.adoptTree(tm, summary.Roots, summary.ScanSettings)
	s.Scanner.Tree = tm
	if s.MCPContext != nil {
		s.MCPContext.Tree = tm
		// Sem Raízes Varridas o Assistente recusa toda leitura de arquivo
		// (contrato 1.11).
		s.MCPContext.SetAllowedRoots(summary.Roots)
	}

	s.Index.RebuildIndex(tm.GetAllFiles())
	grpCount, fileCount, wasted = s.Index.GetSummaryStats()

	progress(95, "Identificando e classificando pastas clones...")

	s.FolderIndex.RebuildFolderIndex(tm)
	fGrpCount, fCount, fWasted = s.FolderIndex.GetSummaryStats()

	return grpCount, fileCount, wasted, fGrpCount, fCount, fWasted
}

// loadProgress publica o andamento da leitura de um Snapshot.
func (s *AppServer) loadProgress(percent float64, message string) {
	s.Scanner.SetStatus(func(st *ScanStatus) {
		st.Phase = PhaseLoadingCache
		st.CurrentPath = message
		st.ProgressPercent = percent
	})
	s.broadcastStatus()
}

func (s *AppServer) handleRestoreAutoSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if phase := s.currentPhase(); isBusyPhase(phase) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "scan_in_progress", "phase": phase})
		return
	}

	info, err := scanner.GetLatestAutoSave(s.autoSaveDir())
	if err != nil {
		http.Error(w, "Nenhum arquivo de autosave encontrado", http.StatusNotFound)
		return
	}

	s.loadProgress(5, fmt.Sprintf("Carregando snapshot de autosave: %s", filepath.Base(info.FilePath)))

	// Leitura em streaming: o resumo volta sem a lista de arquivos (achado H3).
	tm, summary, err := scanner.LoadCacheSummaryFromFile(info.FilePath, func(stage string, percent float64, details string) {
		s.loadProgress(percent, fmt.Sprintf("%s - %s", stage, details))
	})
	if err != nil {
		s.setPhase(PhaseIdle, "")
		s.Scanner.SetStatus(func(st *ScanStatus) { st.ProgressPercent = 0 })
		s.broadcastStatus()
		http.Error(w, "Erro ao carregar autosave: "+err.Error(), http.StatusInternalServerError)
		return
	}

	grpCount, fileCount, wasted, fGrpCount, fCount, fWasted := s.adoptSnapshot(tm, summary, s.loadProgress)

	s.Scanner.SetStatus(func(st *ScanStatus) {
		st.Phase = PhaseCompleted
		st.CurrentPath = fmt.Sprintf("Autosave restaurado com sucesso (%d arquivos).", summary.TotalFiles)
		st.TotalFilesScanned = summary.TotalFiles
		st.TotalDirsScanned = summary.TotalDirs
		st.TotalBytesScanned = summary.TotalBytes
		st.TotalAllocatedBytesScanned = summary.TotalAllocatedBytes
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
	s.broadcastStatus()

	s.noteAutoSaveBaseline(summary.ScanSettings, time.Now())

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "restored",
		"summary": summary,
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
		targetPath = filepath.Join(s.autoSaveDir(), fileName)
	}

	err := scanner.SaveCacheToFile(s.Tree(), s.scanRoots(), s.lastScanConfig(), targetPath)
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

	if phase := s.currentPhase(); isBusyPhase(phase) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "scan_in_progress", "phase": phase})
		return
	}

	s.loadProgress(5, fmt.Sprintf("Carregando snapshot de cache: %s", filepath.Base(req.FilePath)))

	loadedTree, summary, err := scanner.LoadCacheSummaryFromFile(req.FilePath, func(stage string, percent float64, details string) {
		s.loadProgress(percent, fmt.Sprintf("%s - %s", stage, details))
	})
	if err != nil {
		s.setPhase(PhaseIdle, "")
		s.Scanner.SetStatus(func(st *ScanStatus) { st.ProgressPercent = 0 })
		s.broadcastStatus()
		http.Error(w, fmt.Sprintf("Erro ao carregar cache: %v", err), http.StatusInternalServerError)
		return
	}

	grpCount, fileCount, wasted, fGrpCount, fCount, fWasted := s.adoptSnapshot(loadedTree, summary, s.loadProgress)

	s.Scanner.SetStatus(func(st *ScanStatus) {
		st.Phase = PhaseCompleted
		st.CurrentPath = fmt.Sprintf("Cache carregado com sucesso de %s (%s)", filepath.Base(req.FilePath), summary.Timestamp.Format("02/01/2006 15:04"))
		st.TotalFilesScanned = summary.TotalFiles
		st.TotalDirsScanned = summary.TotalDirs
		st.TotalBytesScanned = summary.TotalBytes
		st.TotalAllocatedBytesScanned = summary.TotalAllocatedBytes
		st.DuplicateGroupsCount = grpCount
		st.DuplicateFilesCount = fileCount
		st.DuplicateWastedBytes = wasted
		st.DuplicateFolderGroupsCount = fGrpCount
		st.DuplicateFoldersCount = fCount
		st.DuplicateFolderWastedBytes = fWasted
		st.ProgressPercent = 100
	})
	s.broadcastStatus()

	s.noteAutoSaveBaseline(summary.ScanSettings, time.Now())

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":      "loaded",
		"summary":     summary,
		"groupCount":  grpCount,
		"fileCount":   fileCount,
		"wastedBytes": wasted,
	})
}

func (s *AppServer) handleListCaches(w http.ResponseWriter, r *http.Request) {
	list, err := scanner.ListSavedCaches(s.autoSaveDir())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
}
