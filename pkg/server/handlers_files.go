package server

import (
	"encoding/json"
	"net/http"
	"scanfile/pkg/config"
	"scanfile/pkg/drives"
	"scanfile/pkg/privileges"
	"scanfile/pkg/recycle"
)

func (s *AppServer) handleGetDrives(w http.ResponseWriter, r *http.Request) {
	driveList, err := drives.GetLogicalDrives()
	if err != nil || len(driveList) == 0 {
		driveList = []drives.DriveInfo{
			{
				Letter:      "C:\\",
				VolumeLabel: "Disco Local (C:)",
				FileSystem:  "NTFS",
				DriveType:   "Fixed (SSD/HDD)",
			},
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(driveList)
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

func (s *AppServer) handleGetPrivileges(w http.ResponseWriter, r *http.Request) {
	status := privileges.CheckPrivilegeStatus()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
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

// registerFileRoutes registers routes owned by the files/config/AI area (Agente S1).
func (s *AppServer) registerFileRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/drives", s.handleGetDrives)
	mux.HandleFunc("/api/files/recycle", s.handleRecycleFiles)
	mux.HandleFunc("/api/files/delete", s.handleDeleteFiles)
	mux.HandleFunc("/api/system/privileges", s.handleGetPrivileges)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/ai/models", s.handleAIModels)
	mux.HandleFunc("/api/ai/models/pull", s.handleAIPullModel)
	mux.HandleFunc("/api/ai/chat", s.handleAIChat)
	mux.HandleFunc("/api/ai/actions/execute", s.handleAIExecuteAction)
	mux.HandleFunc("/api/ai/status", s.handleAIStatus)
}
