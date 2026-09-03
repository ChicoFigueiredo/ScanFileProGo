package ai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeProvider(t *testing.T) {
	cases := []struct {
		in   ProviderType
		want ProviderType
	}{
		{"ollama", ProviderOllama},
		{"OLLAMA", ProviderOllama},
		{"openrouter", ProviderOpenRouter},
		{"quick", ProviderQuick},
		{"QUICK", ProviderQuick},
		{"direct", ProviderQuick}, // alias legado
		{" direct ", ProviderQuick},
		{"", ProviderOllama},
		{"inexistente", ProviderOllama},
	}
	for _, tc := range cases {
		if got := NormalizeProvider(tc.in); got != tc.want {
			t.Fatalf("NormalizeProvider(%q) = %q, esperado %q", tc.in, got, tc.want)
		}
	}
}

func TestProviderDisplayName(t *testing.T) {
	if got := ProviderDisplayName(ProviderQuick); got != "Comandos Rápidos (sem modelo)" {
		t.Fatalf("nome exibido do provedor quick: %q", got)
	}
	if got := ProviderDisplayName("direct"); got != "Comandos Rápidos (sem modelo)" {
		t.Fatalf("o alias direct deve exibir o mesmo nome, obtido %q", got)
	}
	if got := ProviderDisplayName(ProviderOllama); !strings.Contains(got, "Ollama") {
		t.Fatalf("nome exibido do Ollama: %q", got)
	}
}

func routeQuick(t *testing.T, prompt string) *Message {
	t.Helper()
	msg, err := NewQuickRouter().Chat(context.Background(), []Message{{Role: "user", Content: prompt}}, nil)
	if err != nil {
		t.Fatalf("roteador falhou: %v", err)
	}
	return msg
}

func TestQuickRouter_Duplicados(t *testing.T) {
	msg := routeQuick(t, "quero achar arquivos duplicados no disco")
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Function.Name != "classify_files" {
		t.Fatalf("esperada chamada a classify_files, obtido %+v", msg.ToolCalls)
	}

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(msg.ToolCalls[0].Function.Arguments), &args); err != nil {
		t.Fatal(err)
	}
	if args["duplicates_only"] != true {
		t.Fatalf("argumentos inesperados: %v", args)
	}
}

func TestQuickRouter_ArquivosGrandes(t *testing.T) {
	msg := routeQuick(t, "quais são os maiores arquivos?")
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Function.Name != "classify_files" {
		t.Fatalf("esperada chamada a classify_files, obtido %+v", msg.ToolCalls)
	}
	if !strings.Contains(msg.ToolCalls[0].Function.Arguments, "min_size_mb") {
		t.Fatalf("argumentos inesperados: %s", msg.ToolCalls[0].Function.Arguments)
	}
}

func TestQuickRouter_AnalisarCaminho(t *testing.T) {
	msg := routeQuick(t, `analise o pdf C:\Projetos\relatorio.pdf por favor`)
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Function.Name != "analyze_file_content" {
		t.Fatalf("esperada chamada a analyze_file_content, obtido %+v", msg.ToolCalls)
	}

	var args map[string]string
	if err := json.Unmarshal([]byte(msg.ToolCalls[0].Function.Arguments), &args); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(args["filepath"], "relatorio.pdf") {
		t.Fatalf("caminho extraído incorreto: %v", args)
	}
}

func TestQuickRouter_SemCaminhoCaiNaAjuda(t *testing.T) {
	msg := routeQuick(t, "analise alguma coisa aí")
	if len(msg.ToolCalls) != 0 {
		t.Fatalf("sem caminho não deveria haver chamada de ferramenta: %+v", msg.ToolCalls)
	}
	if !strings.Contains(msg.Content, "Comandos Rápidos (sem modelo)") {
		t.Fatalf("a ajuda deveria se identificar como Comandos Rápidos: %q", msg.Content)
	}
	if !strings.Contains(strings.ToLower(msg.Content), "aprovação") {
		t.Fatalf("a ajuda deveria lembrar que ações exigem aprovação: %q", msg.Content)
	}
}

func TestQuickRouter_SaudacaoCaiNaAjuda(t *testing.T) {
	msg := routeQuick(t, "oi, tudo bem?")
	if len(msg.ToolCalls) != 0 {
		t.Fatalf("saudação não deveria acionar ferramenta: %+v", msg.ToolCalls)
	}
	if msg.Content == "" {
		t.Fatal("a resposta padrão não pode ser vazia")
	}
}

func TestQuickRouter_SemMensagens(t *testing.T) {
	if _, err := NewQuickRouter().Chat(context.Background(), nil, nil); err == nil {
		t.Fatal("esperado erro sem mensagens")
	}
}

func TestAgentCoordinator_UsaRoteadorQuick(t *testing.T) {
	ac := NewAgentCoordinator("http://127.0.0.1:1", "", "", nil, nil)
	if ac.QuickRouter == nil {
		t.Fatal("o coordenador deveria expor o roteador de Comandos Rápidos")
	}

	var events []StreamEvent
	msg, err := ac.RunAgentExecution(
		context.Background(),
		ChatRequest{Provider: "direct", Prompt: "oi"}, // alias legado
		"prompt de sistema",
		func(e StreamEvent) { events = append(events, e) },
	)
	if err != nil {
		t.Fatalf("o alias direct deveria rotear para os Comandos Rápidos sem tocar o Ollama: %v", err)
	}
	if msg == nil || msg.Content == "" {
		t.Fatal("resposta vazia do roteador")
	}

	for _, e := range events {
		if e.Type == "thought" && !strings.Contains(e.Content, "Comandos Rápidos (sem modelo)") {
			t.Fatalf("o evento inicial deveria nomear o provedor corretamente: %q", e.Content)
		}
	}
}
