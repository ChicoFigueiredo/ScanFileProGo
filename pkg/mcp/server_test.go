package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

var expectedToolNames = []string{
	"classify_files",
	"analyze_file_content",
	"analyze_image_visual",
	"compare_visual_similarity",
	"write_file_metadata",
	"propose_actions",
}

func TestNewStdioServer_RegistraTodasAsFerramentas(t *testing.T) {
	s := NewStdioServer(NewMCPToolsContext(nil, nil, nil, nil, ""))
	tools := s.ListTools()

	if len(tools) != len(expectedToolNames) {
		t.Fatalf("esperadas %d ferramentas, obtidas %d", len(expectedToolNames), len(tools))
	}
	for _, name := range expectedToolNames {
		if _, ok := tools[name]; !ok {
			t.Fatalf("ferramenta %q não registrada no servidor MCP stdio", name)
		}
	}
}

func TestGetOpenAIToolDefinitions_CobreAsMesmasFerramentas(t *testing.T) {
	defs := GetOpenAIToolDefinitions()
	got := make(map[string]bool, len(defs))
	for _, d := range defs {
		if d.Type != "function" {
			t.Fatalf("definição %s deveria ser do tipo function", d.Function.Name)
		}
		got[d.Function.Name] = true
	}
	for _, name := range expectedToolNames {
		if !got[name] {
			t.Fatalf("definição OpenAI ausente para %q", name)
		}
	}
	if len(defs) != len(expectedToolNames) {
		t.Fatalf("esperadas %d definições, obtidas %d", len(expectedToolNames), len(defs))
	}
}

// A ferramenta propose_actions não pode expor dry_run ao modelo: a Proposta é
// sempre pendente.
func TestProposeActionsTool_NaoExpoeDryRun(t *testing.T) {
	for _, d := range GetOpenAIToolDefinitions() {
		if d.Function.Name != "propose_actions" {
			continue
		}
		props, _ := d.Function.Parameters["properties"].(map[string]interface{})
		if _, ok := props["dry_run"]; ok {
			t.Fatal("propose_actions não deve aceitar dry_run do modelo")
		}
		if !strings.Contains(strings.ToLower(d.Function.Description), "aprovação") {
			t.Fatalf("a descrição deve deixar claro que exige aprovação: %q", d.Function.Description)
		}
		return
	}
	t.Fatal("definição de propose_actions não encontrada")
}

func callTool(t *testing.T, tc *MCPToolsContext, name string, args map[string]interface{}) *mcp.CallToolResult {
	t.Helper()

	s := NewStdioServer(tc)
	tool, ok := s.ListTools()[name]
	if !ok {
		t.Fatalf("ferramenta %q não registrada", name)
	}

	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}

	var req mcp.CallToolRequest
	req.Params.Name = name
	req.Params.Arguments = args
	req.Params.RawArguments = raw

	res, err := tool.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler devolveu erro de transporte: %v", err)
	}
	return res
}

func resultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

// Uma chamada do modelo a propose_actions nunca pode tocar no disco.
func TestProposeActionsTool_ViaMCPNaoExecuta(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "vitima.txt")
	writeFile(t, victim, "sobrevive")

	tc := NewMCPToolsContext(nil, nil, nil, nil, "")
	tc.SetAllowedRoots([]string{dir})

	res := callTool(t, tc, "propose_actions", map[string]interface{}{
		"action_type": "RECYCLE",
		"files":       []string{victim},
		"dry_run":     false, // injeção via argumento extra
	})
	if res.IsError {
		t.Fatalf("proposta válida deveria ser aceita: %s", resultText(t, res))
	}

	var prop ActionProposal
	if err := json.Unmarshal([]byte(resultText(t, res)), &prop); err != nil {
		t.Fatalf("resposta não é uma Proposta JSON: %v", err)
	}
	if !prop.DryRun || prop.Executed {
		t.Fatalf("a Proposta deveria estar pendente, obtido dryRun=%v executed=%v", prop.DryRun, prop.Executed)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatal("o arquivo foi tocado pela chamada do modelo")
	}
}

func TestAnalyzeFileContentTool_SemRaizesRecusa(t *testing.T) {
	tc := NewMCPToolsContext(nil, nil, nil, nil, "")

	res := callTool(t, tc, "analyze_file_content", map[string]interface{}{
		"filepath": `C:\Windows\System32\drivers\etc\hosts`,
	})
	if !res.IsError {
		t.Fatal("sem Raízes Varridas a leitura deveria ser recusada")
	}
	if !strings.Contains(resultText(t, res), "Raiz Varrida") {
		t.Fatalf("mensagem deveria explicar a ausência de Raízes Varridas: %s", resultText(t, res))
	}
}
