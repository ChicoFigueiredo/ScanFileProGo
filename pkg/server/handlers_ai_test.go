package server

import (
	"net/http"
	"testing"

	"scanfile/pkg/ai"
	"scanfile/pkg/config"
	"scanfile/pkg/mcp"
)

// =========================================================================
// Catálogo e estado do Assistente (contrato 1.11)
// =========================================================================

func TestAIModelsAnswersAnArray(t *testing.T) {
	useTempConfig(t, offlineConfig())
	app, ts := newTestServer(t)

	res := doAPI(t, app, ts, http.MethodGet, "/api/ai/models", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, esperado 200", res.StatusCode)
	}

	// O contrato 1.11 pede um array de modelos, não um objeto envelope.
	var models []ai.ModelInfo
	decodeJSONBody(t, res, &models)

	if len(models) == 0 {
		t.Fatal("o catálogo veio vazio")
	}

	byID := make(map[string]ai.ModelInfo, len(models))
	for _, m := range models {
		byID[m.ID] = m
	}

	padrao, ok := byID[ai.DefaultOllamaModel]
	if !ok {
		t.Fatalf("o catálogo não traz o modelo padrão %s", ai.DefaultOllamaModel)
	}
	if !padrao.Vision || !padrao.Tools || !padrao.Recommended {
		t.Fatalf("modelo padrão = %+v, esperado visão, ferramentas e recomendação", padrao)
	}

	for _, want := range []string{"qwen2.5vl:7b", "gemma3:12b", "qwen3:14b", "gpt-oss:20b", "devstral:24b"} {
		if _, ok := byID[want]; !ok {
			t.Fatalf("o catálogo não traz %s", want)
		}
	}
}

func TestAIStatusReportsTheKeyWithoutRevealingIt(t *testing.T) {
	useTempConfig(t, offlineConfig())
	app, ts := newTestServer(t)

	res := doAPI(t, app, ts, http.MethodGet, "/api/ai/status", nil)
	var status map[string]any
	decodeJSONBody(t, res, &status)

	if status["hasOpenRouterKey"] != false {
		t.Fatalf("hasOpenRouterKey = %v, esperado false sem chave configurada", status["hasOpenRouterKey"])
	}

	res = doAPI(t, app, ts, http.MethodPost, "/api/config", map[string]any{"aiOpenRouterKey": "sk-status-999"})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("POST da chave: status = %d, esperado 200", res.StatusCode)
	}

	res = doAPI(t, app, ts, http.MethodGet, "/api/ai/status", nil)
	status = nil
	decodeJSONBody(t, res, &status)

	if status["hasOpenRouterKey"] != true {
		t.Fatalf("hasOpenRouterKey = %v, esperado true após gravar a chave", status["hasOpenRouterKey"])
	}
	for key, value := range status {
		if s, ok := value.(string); ok && s == "sk-status-999" {
			t.Fatalf("o campo %q devolveu a chave da OpenRouter", key)
		}
	}
}

func TestOpenRouterKeyComesFromTheProtectedStore(t *testing.T) {
	base := offlineConfig()
	base.AIProvider = config.ProviderOpenRouter
	useTempConfig(t, base)

	app, ts := newTestServer(t)

	res := doAPI(t, app, ts, http.MethodPost, "/api/config", map[string]any{"aiOpenRouterKey": "sk-protegida"})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("POST da chave: status = %d, esperado 200", res.StatusCode)
	}

	// LoadConfig não devolve mais a chave em claro: só o cofre a conhece.
	cfg := config.LoadConfig()
	if cfg.AIOpenRouterKey != "" {
		t.Fatalf("AIOpenRouterKey = %q, esperado vazio em LoadConfig", cfg.AIOpenRouterKey)
	}
	if got := config.OpenRouterKey(cfg); got != "sk-protegida" {
		t.Fatalf("OpenRouterKey = %q, esperado sk-protegida", got)
	}
}

// =========================================================================
// Execução de Proposta (contrato 1.11)
// =========================================================================

func TestExecuteActionRequiresExplicitConfirmation(t *testing.T) {
	useTempConfig(t, offlineConfig())
	app, ts := newTestServer(t)

	proposal := storeProposal(t, app)

	for _, body := range []map[string]any{
		{"proposalId": proposal},
		{"proposalId": proposal, "confirm": false},
	} {
		res := doAPI(t, app, ts, http.MethodPost, "/api/ai/actions/execute", body)
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("corpo %v: status = %d, esperado 400 sem confirm:true", body, res.StatusCode)
		}

		stored, ok := app.MCPContext.GetProposal(proposal)
		if !ok {
			t.Fatal("a Proposta sumiu do contexto")
		}
		if stored.Executed {
			t.Fatal("a Proposta foi executada sem confirmação explícita")
		}
	}
}

func TestExecuteActionRequiresProposalID(t *testing.T) {
	useTempConfig(t, offlineConfig())
	app, ts := newTestServer(t)

	res := doAPI(t, app, ts, http.MethodPost, "/api/ai/actions/execute", map[string]any{"confirm": true})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400 sem proposalId", res.StatusCode)
	}
}

func TestExecuteActionWithConfirmationRunsTheProposal(t *testing.T) {
	useTempConfig(t, offlineConfig())
	app, ts := newTestServer(t)

	proposal := storeProposal(t, app)

	res := doAPI(t, app, ts, http.MethodPost, "/api/ai/actions/execute", map[string]any{
		"proposalId": proposal,
		"confirm":    true,
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 com confirm:true", res.StatusCode)
	}

	var result ai.ActionExecuteResult
	decodeJSONBody(t, res, &result)
	if result.ActionType != "MARK_REVIEW" {
		t.Fatalf("actionType = %q, esperado MARK_REVIEW", result.ActionType)
	}
	if !result.Success {
		t.Fatal("a Proposta aprovada não foi marcada como executada")
	}
}

func TestExecuteActionRejectsOtherMethods(t *testing.T) {
	useTempConfig(t, offlineConfig())
	app, ts := newTestServer(t)

	res := doAPI(t, app, ts, http.MethodGet, "/api/ai/actions/execute", nil)
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, esperado 405", res.StatusCode)
	}
}

// storeProposal creates a pending Proposta over a throwaway file and returns
// its identifier. MARK_REVIEW touches nothing on disk, which keeps these tests
// about the approval rule and not about file system side effects.
func storeProposal(t *testing.T, app *AppServer) string {
	t.Helper()

	dir := tempDir(t)
	file := writeTempFile(t, dir, "candidato.bin", 32)
	app.MCPContext.SetAllowedRoots([]string{dir})

	proposal, err := app.MCPContext.ProposeActions(mcp.ProposeActionParams{
		ActionType:  "MARK_REVIEW",
		Description: "Proposta de teste",
		Files:       []string{file},
		Category:    "teste",
	})
	if err != nil {
		t.Fatalf("não foi possível criar a Proposta: %v", err)
	}
	if proposal.Executed {
		t.Fatal("a Proposta nasceu executada: propose_actions nunca executa")
	}
	return proposal.ID
}
