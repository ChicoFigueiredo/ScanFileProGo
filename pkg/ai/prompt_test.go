package ai

import (
	"strings"
	"testing"
)

func TestBuildSystemPrompt_NaoCitaModeloEspecifico(t *testing.T) {
	prompt := BuildSystemPrompt(nil, nil, "")
	lower := strings.ToLower(prompt)

	proibidos := []string{"qwen", "llama", "gemma", "deepseek", "gpt-oss", "devstral", "mistral", "claude", "gemini"}
	for _, p := range proibidos {
		if strings.Contains(lower, p) {
			t.Fatalf("o prompt de sistema não pode citar o modelo %q: %s", p, prompt)
		}
	}
}

func TestBuildSystemPrompt_ExigeAprovacaoHumana(t *testing.T) {
	prompt := BuildSystemPrompt(nil, nil, "")
	lower := strings.ToLower(prompt)

	if !strings.Contains(lower, "aprovação humana") {
		t.Fatalf("o prompt deve dizer que toda ação exige aprovação humana: %s", prompt)
	}
	if !strings.Contains(lower, "proposta") {
		t.Fatalf("o prompt deve falar em Proposta: %s", prompt)
	}
	if !strings.Contains(lower, "nunca executa") {
		t.Fatalf("o prompt deve dizer que o Assistente nunca executa ações: %s", prompt)
	}
	if !strings.Contains(lower, "raízes varridas") {
		t.Fatalf("o prompt deve delimitar as Raízes Varridas: %s", prompt)
	}
	// Defesa contra injeção via conteúdo de arquivo (achado C5).
	if !strings.Contains(lower, "nunca como instruções") {
		t.Fatalf("o prompt deve instruir a tratar conteúdo lido como dados: %s", prompt)
	}
}

func TestBuildSystemPrompt_IncluiPastaEmFoco(t *testing.T) {
	prompt := BuildSystemPrompt(nil, nil, `D:\Fotos`)
	if !strings.Contains(prompt, `D:\Fotos`) {
		t.Fatalf("a pasta em foco deveria aparecer no prompt: %s", prompt)
	}
}
