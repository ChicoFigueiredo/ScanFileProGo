package ai

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// QuickRouter implements Comandos Rápidos: a keyword router that triggers the
// Assistente tools without any language model. It is not a Provedor of inference,
// so it never generates free text beyond the fixed help message.
type QuickRouter struct{}

// NewQuickRouter creates the Comandos Rápidos router.
func NewQuickRouter() *QuickRouter {
	return &QuickRouter{}
}

// quickHelpMessage is shown when no keyword matches.
const quickHelpMessage = "**Comandos Rápidos (sem modelo)**: nenhum modelo de linguagem está em uso. Reconheço pedidos como:\n\n" +
	"- *\"ache as duplicatas\"* — lista os Grupos de Duplicados encontrados;\n" +
	"- *\"quais são os arquivos maiores\"* — lista os arquivos que mais ocupam espaço;\n" +
	"- *\"analise C:\\\\pasta\\\\arquivo.pdf\"* — inspeciona o conteúdo de um arquivo dentro das Raízes Varridas.\n\n" +
	"Qualquer ação sobre arquivos vira uma Proposta e só acontece depois da sua aprovação."

// Chat routes the last user message to a tool call by keyword.
func (r *QuickRouter) Chat(ctx context.Context, messages []Message, tools []ToolDefinition) (*Message, error) {
	if len(messages) == 0 {
		return nil, fmt.Errorf("nenhuma mensagem fornecida")
	}

	lastMsg := messages[len(messages)-1].Content
	lower := strings.ToLower(lastMsg)

	switch {
	case containsAny(lower, "duplicad", "duplicat", "iguais", "repetid"):
		return toolCallMessage("call_quick_duplicates", "classify_files", `{"duplicates_only":true,"limit":50}`), nil

	case containsAny(lower, "pesad", "grand", "maior", "espaco", "espaço", "ocupa"):
		return toolCallMessage("call_quick_large", "classify_files", `{"min_size_mb":100,"limit":50}`), nil

	case containsAny(lower, "pdf", "sqlite", "texto", "analis", "inspecion", "conteudo", "conteúdo"):
		if targetPath := extractPathCandidate(lastMsg); targetPath != "" {
			return toolCallMessage("call_quick_analyze", "analyze_file_content",
				fmt.Sprintf(`{"filepath":%q}`, filepath.Clean(targetPath))), nil
		}
	}

	return &Message{Role: "assistant", Content: quickHelpMessage}, nil
}

func containsAny(haystack string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}

func toolCallMessage(id, name, args string) *Message {
	return &Message{
		Role: "assistant",
		ToolCalls: []ToolCall{
			{
				ID:   id,
				Type: "function",
				Function: FunctionCallInfo{
					Name:      name,
					Arguments: args,
				},
			},
		},
	}
}

// extractPathCandidate picks the first token in text that looks like a file path.
func extractPathCandidate(text string) string {
	for _, w := range strings.Fields(text) {
		trimmed := strings.Trim(w, `"',;:()[]`)
		if trimmed == "" {
			continue
		}
		if strings.Contains(trimmed, `\`) || strings.Contains(trimmed, "/") || looksLikeDriveLetter(trimmed) {
			return trimmed
		}
	}
	return ""
}

func looksLikeDriveLetter(s string) bool {
	return len(s) >= 2 && s[1] == ':' &&
		((s[0] >= 'a' && s[0] <= 'z') || (s[0] >= 'A' && s[0] <= 'Z'))
}
