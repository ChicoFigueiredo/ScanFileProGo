package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"scanfile/pkg/ai"
	"scanfile/pkg/config"
)

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
		"ollamaOnline":     err == nil,
		"ollamaVersion":    ver,
		"ollamaEndpoint":   cfg.AIOllamaEndpoint,
		"installedCount":   len(tags),
		"installedModels":  tags,
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
