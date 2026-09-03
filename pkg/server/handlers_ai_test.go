package server

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"scanfile/pkg/ai"
	"scanfile/pkg/config"
	"scanfile/pkg/mcp"
	"scanfile/pkg/recycle"
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

// --- Desfecho real da execução de uma Proposta -------------------------------

// TestExecuteActionReportsOnlyWhatLeftTheDisk cobre o caso em que a Proposta é
// recusada por inteiro: nada saiu do disco, então o resultado não pode anunciar
// sucesso nem bytes liberados, e a Proposta continua pendente para nova
// tentativa.
func TestExecuteActionReportsOnlyWhatLeftTheDisk(t *testing.T) {
	app, ts := newAuthedTestServer(t)

	dir := tempDir(t)
	alvo := writeTempFile(t, dir, "recusado.bin", 4096)

	// Sem Raízes Varridas o Assistente recusa tudo o que tocaria o disco.
	app.MCPContext.SetAllowedRoots([]string{dir})
	prop, err := app.MCPContext.ProposeActions(mcp.ProposeActionParams{
		ActionType: "RECYCLE",
		Files:      []string{alvo},
	})
	if err != nil {
		t.Fatalf("não foi possível criar a Proposta: %v", err)
	}

	// A Reciclagem recusa todos os itens, como faria uma Pasta Protegida ou um
	// volume sem Lixeira.
	app.MCPContext.RecycleFunc = func(paths []string) recycle.BatchDeleteResult {
		items := make([]recycle.ItemResult, 0, len(paths))
		for _, p := range paths {
			items = append(items, recycle.ItemResult{
				Path:   p,
				Status: recycle.StatusRefused,
				Reason: "volume sem Lixeira disponível",
			})
		}
		return recycle.BatchDeleteResult{
			TotalRequested: len(paths),
			RefusedCount:   len(paths),
			Items:          items,
		}
	}

	resp := postJSON(t, ts, "/api/ai/actions/execute", map[string]any{
		"proposalId": prop.ID,
		"confirm":    true,
	})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 mesmo com tudo recusado", resp.StatusCode)
	}

	var out ai.ActionExecuteResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("resposta inválida: %v", err)
	}

	if out.Success {
		t.Error("success = true, mas nenhum arquivo saiu do disco")
	}
	if out.Affected != 0 {
		t.Errorf("affected = %d, esperado 0", out.Affected)
	}
	if out.FreedBytes != 0 {
		t.Errorf("freedBytes = %d, esperado 0: nada foi liberado", out.FreedBytes)
	}
	if len(out.Errors) == 0 {
		t.Error("o motivo da recusa deveria chegar à interface")
	}
	if !strings.Contains(out.Message, "recusado") {
		t.Errorf("mensagem = %q, deveria dizer que houve recusa", out.Message)
	}

	// O arquivo continua lá e a Proposta segue pendente.
	if _, err := os.Stat(alvo); err != nil {
		t.Errorf("o arquivo recusado deveria continuar no disco: %v", err)
	}
	if stored, ok := app.MCPContext.GetProposal(prop.ID); !ok || stored.Executed {
		t.Error("uma Proposta inteiramente recusada não pode ficar marcada como executada")
	}
}
