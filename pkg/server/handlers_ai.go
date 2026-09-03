package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"scanfile/pkg/ai"
	"scanfile/pkg/config"
)

// handleAIModels answers the catalogue as a plain array of models, the shape
// the interface consumes (contrato 1.11).
func (s *AppServer) handleAIModels(w http.ResponseWriter, r *http.Request) {
	cfg := config.LoadConfig()
	catalog := ai.BuildCatalog(r.Context(), cfg.AIOllamaEndpoint)

	models := catalog.Models
	if models == nil {
		models = []ai.ModelInfo{}
	}
	writeJSON(w, http.StatusOK, models)
}

func (s *AppServer) handleAIStatus(w http.ResponseWriter, r *http.Request) {
	cfg := config.LoadConfig()
	tags, ver, err := s.AIAgent.OllamaClient.ListInstalledModels(r.Context())

	writeJSON(w, http.StatusOK, map[string]any{
		"ollamaOnline":    err == nil,
		"ollamaVersion":   ver,
		"ollamaEndpoint":  cfg.AIOllamaEndpoint,
		"installedCount":  len(tags),
		"installedModels": tags,
		// The key itself never leaves the server: only whether one is stored.
		"hasOpenRouterKey": cfg.Public().HasOpenRouterKey,
	})
}

func (s *AppServer) handleAIPullModel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeAPIError(w, http.StatusMethodNotAllowed, "Método não permitido: use POST.", "method_not_allowed")
		return
	}

	var req struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, maxRequestBody)).Decode(&req); err != nil || req.Model == "" {
		writeAPIError(w, http.StatusBadRequest, "Nome do modelo é obrigatório.", "model_required")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAPIError(w, http.StatusInternalServerError, "Streaming não suportado.", "no_streaming")
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

	doneData, _ := json.Marshal(map[string]any{"status": "success", "percent": 100})
	fmt.Fprintf(w, "data: %s\n\n", string(doneData))
	flusher.Flush()
}

func (s *AppServer) handleAIChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeAPIError(w, http.StatusMethodNotAllowed, "Método não permitido: use POST.", "method_not_allowed")
		return
	}

	var req ai.ChatRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxRequestBody)).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "Payload JSON inválido: "+err.Error(), "invalid_body")
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

	// Update live clients in Agent with latest config. The OpenRouter key comes
	// out of the protected store, never out of the configuration document.
	s.AIAgent.OllamaClient = ai.NewOllamaClient(cfg.AIOllamaEndpoint)
	s.AIAgent.OpenRouter = ai.NewOpenRouterClient(config.OpenRouterKey(cfg))

	systemPrompt := ai.BuildSystemPrompt(s.Tree(), s.Index, req.SelectedFolder)

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

// handleAIExecuteAction runs a Proposta only after an explicit human approval:
// without confirm:true nothing is executed (contrato 1.11).
func (s *AppServer) handleAIExecuteAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeAPIError(w, http.StatusMethodNotAllowed, "Método não permitido: use POST.", "method_not_allowed")
		return
	}

	var req ai.ActionExecuteRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxRequestBody)).Decode(&req); err != nil || req.ProposalID == "" {
		writeAPIError(w, http.StatusBadRequest, "proposalId é obrigatório.", "proposal_required")
		return
	}

	if !req.Confirm {
		writeAPIError(w, http.StatusBadRequest,
			"A Proposta só é executada com aprovação explícita: envie confirm:true.",
			"confirmation_required")
		return
	}

	proposal, err := s.MCPContext.ExecuteProposal(req.ProposalID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError,
			fmt.Sprintf("Falha ao executar a Proposta %s: %v", req.ProposalID, err),
			"execute_failed")
		return
	}

	writeJSON(w, http.StatusOK, ai.ActionExecuteResult{
		Success:    proposal.Executed,
		ActionType: proposal.ActionType,
		Affected:   proposal.FileCount,
		FreedBytes: proposal.TotalBytes,
		FreedSize:  proposal.TotalSize,
		Message:    fmt.Sprintf("Ação %s executada com sucesso para %d arquivos.", proposal.ActionType, proposal.FileCount),
	})
}
