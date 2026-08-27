package ai

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// DirectLocalEngine provides a direct in-process intelligence engine.
type DirectLocalEngine struct {
	ModelPath string
}

// NewDirectLocalEngine creates a direct engine instance.
func NewDirectLocalEngine(modelPath string) *DirectLocalEngine {
	return &DirectLocalEngine{
		ModelPath: modelPath,
	}
}

// Chat handles in-process direct generation and tool execution fallback.
func (e *DirectLocalEngine) Chat(ctx context.Context, messages []Message, tools []ToolDefinition) (*Message, error) {
	if len(messages) == 0 {
		return nil, fmt.Errorf("nenhuma mensagem fornecida")
	}

	lastMsg := messages[len(messages)-1].Content
	lower := strings.ToLower(lastMsg)

	// Direct rule-based / regex semantic intelligence routing
	switch {
	case strings.Contains(lower, "duplicad") || strings.Contains(lower, "duplicat") || strings.Contains(lower, "iguais"):
		return &Message{
			Role: "assistant",
			ToolCalls: []ToolCall{
				{
					ID:   "call_direct_classify_duplicates",
					Type: "function",
					Function: FunctionCallInfo{
						Name:      "classify_files",
						Arguments: `{"duplicates_only":true,"limit":50}`,
					},
				},
			},
		}, nil

	case strings.Contains(lower, "pesad") || strings.Contains(lower, "grand") || strings.Contains(lower, "maior") || strings.Contains(lower, "espaco") || strings.Contains(lower, "espaço"):
		return &Message{
			Role: "assistant",
			ToolCalls: []ToolCall{
				{
					ID:   "call_direct_classify_large",
					Type: "function",
					Function: FunctionCallInfo{
						Name:      "classify_files",
						Arguments: `{"min_size_mb":100,"limit":50}`,
					},
				},
			},
		}, nil

	case strings.Contains(lower, "pdf") || strings.Contains(lower, "sqlite") || strings.Contains(lower, "texto") || strings.Contains(lower, "analis"):
		// Extract any potential filepath mentioned
		words := strings.Fields(lastMsg)
		var targetPath string
		for _, w := range words {
			if strings.Contains(w, `\`) || strings.Contains(w, `/`) || strings.HasPrefix(w, "C:") || strings.HasPrefix(w, "D:") || strings.HasPrefix(w, "P:") {
				targetPath = strings.Trim(w, `"',;:()`)
				break
			}
		}

		if targetPath != "" {
			return &Message{
				Role: "assistant",
				ToolCalls: []ToolCall{
					{
						ID:   "call_direct_analyze",
						Type: "function",
						Function: FunctionCallInfo{
							Name:      "analyze_file_content",
							Arguments: fmt.Sprintf(`{"filepath":%q}`, filepath.Clean(targetPath)),
						},
					},
				},
			}, nil
		}
	}

	// Default general response
	return &Message{
		Role: "assistant",
		Content: fmt.Sprintf("🤖 **Motor Direto Local**: Analisando seu pedido. Você pode me pedir para:\n\n" +
			"- *'Ache todas as duplicatas maiores que 50MB e proponha limpeza'*\n" +
			"- *'Classifique os arquivos da pasta por tamanho e extensão'*\n" +
			"- *'Analise o conteúdo do arquivo [caminho] (PDF, SQLite, TXT)'*\n" +
			"- *'Proponha mover arquivos antigos/duplicados para a lixeira do Windows'*"),
	}, nil
}
