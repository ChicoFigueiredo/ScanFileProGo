package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"scanfile/pkg/config"
	"scanfile/pkg/drives"
	"scanfile/pkg/privileges"
	"scanfile/pkg/recycle"
	"scanfile/pkg/scanner"
)

// Version is the build version reported by GET /api/system/info. main overwrites
// it at startup (agente S3); "dev" keeps the package honest on its own.
var Version = "dev"

// deleteConfirmWord is the word the user types to approve an Exclusão
// Permanente (contrato 1.5).
const deleteConfirmWord = "EXCLUIR"

// maxRequestBody caps the JSON bodies this area accepts. A batch of a few
// thousand paths fits comfortably; anything larger is a mistake or an attack.
const maxRequestBody = 8 << 20 // 8 MiB

// =========================================================================
// DISCOS (contrato 1.12)
// =========================================================================

func (s *AppServer) handleGetDrives(w http.ResponseWriter, r *http.Request) {
	driveList, err := drives.GetLogicalDrives()
	if err != nil || len(driveList) == 0 {
		// Last resort so the interface still offers something to scan. The WSL
		// and selection flags are computed the same way as for a real volume.
		const letter = "C:\\"
		const fileSystem = "NTFS"
		driveList = []drives.DriveInfo{
			{
				Letter:          letter,
				VolumeLabel:     "Disco Local (C:)",
				FileSystem:      fileSystem,
				DriveType:       drives.DriveTypeFixed,
				IsWSL:           drives.IsWSLVolume(fileSystem, letter),
				DefaultSelected: drives.IsDefaultSelected(drives.DriveTypeFixed, fileSystem, letter),
			},
		}
	}
	writeJSON(w, http.StatusOK, driveList)
}

// =========================================================================
// RECICLAGEM E EXCLUSÃO PERMANENTE (contrato 1.5)
// =========================================================================

// fileActionRequest is the body of POST /api/files/recycle and
// POST /api/files/delete.
type fileActionRequest struct {
	Paths []string `json:"paths"`
	// ConfirmName must equal the base name of every folder in the batch.
	ConfirmName string `json:"confirmName"`
	// ConfirmText must be exactly "EXCLUIR" on the permanent deletion endpoint.
	ConfirmText string `json:"confirmText"`
}

// fileActionResponse is the answer of both endpoints: one entry per requested
// path, in the order they were sent, plus the totals the interface shows.
type fileActionResponse struct {
	Items      []recycle.ItemResult `json:"items"`
	Recycled   int                  `json:"recycled"`
	Deleted    int                  `json:"deleted"`
	Refused    int                  `json:"refused"`
	Failed     int                  `json:"failed"`
	FreedBytes int64                `json:"freedBytes"`
}

// fileActionPlan is a single path after the checks that pkg/recycle cannot make
// on its own: scope against the Raízes Varridas and the typed confirmation of a
// folder name.
type fileActionPlan struct {
	path    string
	isDir   bool
	refusal string
}

func (s *AppServer) handleRecycleFiles(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeFileAction(w, r)
	if !ok {
		return
	}
	s.runFileAction(w, req, true)
}

func (s *AppServer) handleDeleteFiles(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeFileAction(w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(req.ConfirmText) != deleteConfirmWord {
		writeAPIError(w, http.StatusBadRequest,
			"Digite "+deleteConfirmWord+" para confirmar a Exclusão Permanente.",
			"confirm_text_invalid")
		return
	}
	s.runFileAction(w, req, false)
}

// decodeFileAction validates the method and parses the body.
func decodeFileAction(w http.ResponseWriter, r *http.Request) (fileActionRequest, bool) {
	var req fileActionRequest

	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeAPIError(w, http.StatusMethodNotAllowed, "Método não permitido: use POST.", "method_not_allowed")
		return req, false
	}

	if err := json.NewDecoder(io.LimitReader(r.Body, maxRequestBody)).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "Corpo JSON inválido: "+err.Error(), "invalid_body")
		return req, false
	}

	return req, true
}

// runFileAction plans, executes and reports one batch, and drops the items that
// really left the disk from the árvore and from the índice.
func (s *AppServer) runFileAction(w http.ResponseWriter, req fileActionRequest, toRecycleBin bool) {
	if len(req.Paths) == 0 {
		writeAPIError(w, http.StatusBadRequest, "Informe ao menos um caminho em paths.", "no_paths")
		return
	}

	plan := s.planFileAction(req.Paths, strings.TrimSpace(req.ConfirmName), true)
	batch := s.executeFileAction(plan, toRecycleBin)

	resp := fileActionResponse{Items: batch.Items, FreedBytes: batch.FreedBytes}
	for _, item := range batch.Items {
		switch item.Status {
		case recycle.StatusRecycled:
			resp.Recycled++
		case recycle.StatusDeleted:
			resp.Deleted++
		case recycle.StatusRefused:
			resp.Refused++
		default:
			resp.Failed++
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// planFileAction applies the two rules the library cannot check. Pasta
// Protegida and the Lixeira preflight run later, inside BatchDeleteItems.
//
// requireConfirmName is false for the Assistente: an approved Proposta was
// already confirmed by a human in the interface.
func (s *AppServer) planFileAction(paths []string, confirmName string, requireConfirmName bool) []fileActionPlan {
	roots := s.scanRoots()
	plan := make([]fileActionPlan, 0, len(paths))

	for _, raw := range paths {
		trimmed := strings.TrimSpace(raw)
		item := fileActionPlan{path: raw, isDir: s.pathIsDir(trimmed)}

		switch {
		case trimmed == "":
			item.refusal = "caminho vazio"
		case !recycle.IsWithinRoots(trimmed, roots):
			item.refusal = outOfRootsReason(roots)
		case requireConfirmName && item.isDir && !strings.EqualFold(confirmName, filepath.Base(trimmed)):
			item.refusal = fmt.Sprintf(
				"confirmação da pasta ausente ou incorreta: digite %q para confirmar",
				filepath.Base(trimmed))
		}

		plan = append(plan, item)
	}

	return plan
}

// executeFileAction sends the accepted paths to pkg/recycle and rebuilds the
// full result in the order of the request, keeping the refusals decided here.
func (s *AppServer) executeFileAction(plan []fileActionPlan, toRecycleBin bool) recycle.BatchDeleteResult {
	accepted := make([]string, 0, len(plan))
	for _, item := range plan {
		if item.refusal == "" {
			accepted = append(accepted, strings.TrimSpace(item.path))
		}
	}

	var batch recycle.BatchDeleteResult
	if len(accepted) > 0 {
		batch = recycle.BatchDeleteItems(accepted, toRecycleBin)
	}

	out := recycle.BatchDeleteResult{
		TotalRequested: len(plan),
		FreedBytes:     batch.FreedBytes,
		Errors:         batch.Errors,
		Items:          make([]recycle.ItemResult, 0, len(plan)),
	}

	next := 0
	for _, item := range plan {
		if item.refusal != "" {
			out.Items = append(out.Items, recycle.ItemResult{
				Path:   item.path,
				Status: recycle.StatusRefused,
				Reason: item.refusal,
			})
			out.RefusedCount++
			continue
		}

		result := recycle.ItemResult{
			Path:   item.path,
			Status: recycle.StatusFailed,
			Reason: "a operação não devolveu resultado para este caminho",
		}
		if next < len(batch.Items) {
			result = batch.Items[next]
			result.Path = item.path // answer with the path exactly as requested
			next++
		}
		out.Items = append(out.Items, result)

		switch result.Status {
		case recycle.StatusRecycled, recycle.StatusDeleted:
			out.SuccessCount++
			s.forgetItem(strings.TrimSpace(item.path), item.isDir)
		case recycle.StatusRefused:
			out.RefusedCount++
		default:
			out.FailedCount++
		}
	}

	return out
}

// scopedRecycle is the RecycleFunc handed to the Assistente: an approved
// Proposta recycles through the same scope rules as POST /api/files/recycle.
// The folder confirmation does not apply — the human approved the Proposta in
// the interface, not a typed name.
func (s *AppServer) scopedRecycle(paths []string) recycle.BatchDeleteResult {
	return s.executeFileAction(s.planFileAction(paths, "", false), true)
}

// forgetItem drops an item that really left the disk from the árvore and from
// the índice, so the interface stops offering it.
func (s *AppServer) forgetItem(path string, isDir bool) {
	if path == "" {
		return
	}

	tree := s.Tree()
	if isDir {
		if tree != nil {
			tree.RemoveDir(path)
		}
		if s.Index != nil {
			s.Index.RemoveDirFromIndex(path)
		}
	} else {
		if tree != nil {
			tree.RemoveFile(path)
		}
		if s.Index != nil {
			s.Index.RemoveFileFromIndex(path)
		}
	}

	if s.FolderIndex != nil {
		s.FolderIndex.MarkDirty()
	}
}

// pathIsDir reports whether the path is a folder. The disk is the authority;
// the árvore answers for a path already gone from disk.
func (s *AppServer) pathIsDir(path string) bool {
	if path == "" {
		return false
	}
	if info, err := os.Lstat(path); err == nil {
		return info.IsDir()
	}
	tree := s.Tree()
	return tree != nil && tree.FindDir(path) != nil
}

// scanRoots copies the Raízes Varridas currently loaded.
func (s *AppServer) scanRoots() []string {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	roots := make([]string, len(s.activeRoots))
	copy(roots, s.activeRoots)
	return roots
}

// outOfRootsReason explains, in the user's language, why a path was refused.
func outOfRootsReason(roots []string) string {
	if len(roots) == 0 {
		return "nenhuma Raiz Varrida carregada: execute uma Varredura ou carregue um Snapshot antes de reciclar ou excluir"
	}
	return "caminho fora das Raízes Varridas (" + strings.Join(roots, ", ") + ")"
}

// =========================================================================
// SISTEMA E CONFIGURAÇÃO (contratos 1.3 e 1.6)
// =========================================================================

func (s *AppServer) handleGetPrivileges(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, privileges.CheckPrivilegeStatus())
}

// systemInfoResponse is the body of GET /api/system/info (contrato 1.3).
type systemInfoResponse struct {
	NumCPU        int    `json:"numCPU"`
	ThreadOptions []int  `json:"threadOptions"`
	MaxThreads    int    `json:"maxThreads"`
	Version       string `json:"version"`
	Port          int    `json:"port"`
	Elevated      bool   `json:"elevated"`
}

func (s *AppServer) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, systemInfoResponse{
		NumCPU:        runtime.NumCPU(),
		ThreadOptions: scanner.ThreadOptions(),
		MaxThreads:    scanner.MaxThreads(),
		Version:       Version,
		Port:          s.serverPort(),
		Elevated:      privileges.CheckPrivilegeStatus().IsElevated,
	})
}

// serverPort reports the port actually in use, falling back to the configured
// one while the listener does not exist yet.
func (s *AppServer) serverPort() int {
	if s.listener != nil {
		if addr, ok := s.listener.Addr().(*net.TCPAddr); ok && addr.Port > 0 {
			return addr.Port
		}
	}
	return config.LoadConfig().ServerPort
}

// handleConfig reads and writes the Configuração. The GET never carries the
// OpenRouter key; the POST accepts a partial document and only changes the keys
// it actually contains (contrato 1.6).
func (s *AppServer) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, config.LoadConfig().Public())

	case http.MethodPost:
		body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBody))
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "Não foi possível ler a configuração: "+err.Error(), "invalid_body")
			return
		}
		merged, err := config.MergeJSON(config.LoadConfig(), body)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error(), "invalid_config")
			return
		}
		if err := config.SaveConfig(merged); err != nil {
			writeAPIError(w, http.StatusInternalServerError,
				"Não foi possível gravar a configuração: "+err.Error(), "save_failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})

	default:
		w.Header().Set("Allow", "GET, POST")
		writeAPIError(w, http.StatusMethodNotAllowed, "Método não permitido: use GET ou POST.", "method_not_allowed")
	}
}

// =========================================================================
// ROTAS
// =========================================================================

// registerFileRoutes registers the routes owned by the files/config/AI area
// (agente S1) and wires the Assistente to the same recycling rules the HTTP
// endpoints use.
func (s *AppServer) registerFileRoutes(mux *http.ServeMux) {
	if s.MCPContext != nil && s.MCPContext.RecycleFunc == nil {
		s.MCPContext.RecycleFunc = s.scopedRecycle
	}

	mux.HandleFunc("/api/drives", s.handleGetDrives)
	mux.HandleFunc("/api/files/recycle", s.handleRecycleFiles)
	mux.HandleFunc("/api/files/delete", s.handleDeleteFiles)
	mux.HandleFunc("/api/system/privileges", s.handleGetPrivileges)
	mux.HandleFunc("/api/system/info", s.handleSystemInfo)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/ai/models", s.handleAIModels)
	mux.HandleFunc("/api/ai/models/pull", s.handleAIPullModel)
	mux.HandleFunc("/api/ai/chat", s.handleAIChat)
	mux.HandleFunc("/api/ai/actions/execute", s.handleAIExecuteAction)
	mux.HandleFunc("/api/ai/status", s.handleAIStatus)
}
