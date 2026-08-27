package ai

import (
	"context"
	"fmt"
	"strings"

	"scanfile/pkg/indexer"
	"scanfile/pkg/scanner"
)

// ToolsExecutor defines the interface for executing tools called by the AI.
type ToolsExecutor interface {
	ExecuteTool(ctx context.Context, name string, argsJSON string) (string, *ActionProposal, error)
}

// AgentCoordinator manages multi-turn ReAct conversations with Tool Calling.
type AgentCoordinator struct {
	OllamaClient *OllamaClient
	OpenRouter   *OpenRouterClient
	DirectEngine *DirectLocalEngine
	ToolsExec    ToolsExecutor
	ToolsDefs    []ToolDefinition
}

// NewAgentCoordinator initializes the agent coordinator.
func NewAgentCoordinator(ollamaEndpoint string, openRouterKey string, directModelPath string, toolsExec ToolsExecutor, toolsDefs []ToolDefinition) *AgentCoordinator {
	return &AgentCoordinator{
		OllamaClient: NewOllamaClient(ollamaEndpoint),
		OpenRouter:   NewOpenRouterClient(openRouterKey),
		DirectEngine: NewDirectLocalEngine(directModelPath),
		ToolsExec:    toolsExec,
		ToolsDefs:    toolsDefs,
	}
}

// BuildSystemPrompt creates a rich contextual prompt for the LLM.
func BuildSystemPrompt(tree *scanner.TreeManager, idx *indexer.DuplicateIndex, selectedFolder string) string {
	var sb strings.Builder
	sb.WriteString("Você é o Assistente Inteligente do ScanFile Pro alimentado pelo modelo multimodal Qwen3-VL (8B), especialista em gestão de disco, inspeção visual de documentos e imagens, deduplicação por hash e organização de arquivos.\n")
	sb.WriteString("Você possui visão computacional nativa (VL) e ferramentas avançadas para inspecionar tanto arquivos de texto quanto imagens, fotos, recibos e documentos PDF escaneados.\n\n")

	sb.WriteString("Diretrizes de Execução:\n")
	sb.WriteString("1. Para listar, achar ou classificar arquivos por extensão/tamanho/padrão, use 'classify_files'.\n")
	sb.WriteString("2. Para inspecionar visualmente imagens (PNG, JPG, WebP, etc.), extrair texto via OCR (faturas, certidões, fotos) ou classificar o conteúdo visual, use 'analyze_image_visual'.\n")
	sb.WriteString("3. Para comparar se duas imagens são duplicatas visuais (mesmo que com resoluções, cortes ou compressões diferentes) e recomendar qual manter, use 'compare_visual_similarity'.\n")
	sb.WriteString("4. Para documentos PDF, bancos de dados SQLite ou arquivos de texto/código, use 'analyze_file_content'.\n")
	sb.WriteString("5. Ao sugerir limpezas, exclusões ou movimentações, NUNCA delete diretamente: sempre use 'propose_actions' com 'dry_run: true'. Isso gerará um card seguro para o usuário revisar e aprovar.\n")
	sb.WriteString("6. Seja claro, conciso, utilize Markdown com listas, negrito e tabelas formatadas sempre que apropriado.\n")
	sb.WriteString("7. Sempre informe os tamanhos em formato legível (KB, MB, GB).\n\n")

	if selectedFolder != "" {
		sb.WriteString(fmt.Sprintf("Contexto Atual de Pasta: %s\n", selectedFolder))
	}

	if tree != nil {
		totalFiles := tree.GetTotalFileCount()
		sb.WriteString(fmt.Sprintf("Estatísticas do ScanFile em Memória: %d arquivos escaneados.\n", totalFiles))
	}

	if idx != nil {
		groupCount, fileCount, wasted := idx.GetSummaryStats()
		sb.WriteString(fmt.Sprintf("Duplicatas Identificadas: %d grupos (%d arquivos duplicados, %.2f MB desperdiçados).\n",
			groupCount, fileCount, float64(wasted)/(1024*1024)))
	}

	return sb.String()
}

// RunAgentExecution executes the ReAct loop and streams events to the callback.
func (ac *AgentCoordinator) RunAgentExecution(
	ctx context.Context,
	req ChatRequest,
	systemPrompt string,
	onEvent func(event StreamEvent),
) (*Message, error) {
	messages := make([]Message, 0, len(req.History)+5)

	// Ensure system prompt is first
	messages = append(messages, Message{
		Role:    "system",
		Content: systemPrompt,
	})

	// Add conversation history
	for _, h := range req.History {
		if h.Role != "system" {
			messages = append(messages, h)
		}
	}

	// Add current user prompt
	messages = append(messages, Message{
		Role:    "user",
		Content: req.Prompt,
	})

	maxIterations := 5 // Prevent infinite loops
	var lastAssistantMsg *Message
	var lastProposal *ActionProposal

	if onEvent != nil {
		onEvent(StreamEvent{
			Type:    "thought",
			Content: fmt.Sprintf("Iniciando análise com provedor [%s / %s]...", req.Provider, req.Model),
		})
	}

	for iter := 0; iter < maxIterations; iter++ {
		var resp *Message
		var err error

		switch req.Provider {
		case ProviderOpenRouter:
			resp, err = ac.OpenRouter.Chat(ctx, req.Model, messages, ac.ToolsDefs)
		case ProviderDirectLocal:
			resp, err = ac.DirectEngine.Chat(ctx, messages, ac.ToolsDefs)
		case ProviderOllama:
			fallthrough
		default:
			resp, err = ac.OllamaClient.Chat(ctx, req.Model, messages, ac.ToolsDefs)
		}

		if err != nil {
			if onEvent != nil {
				onEvent(StreamEvent{
					Type:    "error",
					Content: fmt.Sprintf("Erro do modelo (%s): %v", req.Provider, err),
				})
			}
			return nil, err
		}

		lastAssistantMsg = resp
		messages = append(messages, *resp)

		// Check if LLM requested Tool Calls
		if len(resp.ToolCalls) == 0 {
			// No tools requested, LLM provided final textual answer
			if onEvent != nil && resp.Content != "" {
				onEvent(StreamEvent{
					Type:     "token",
					Content:  resp.Content,
					Proposal: lastProposal,
				})
			}
			break
		}

		// Execute all requested tools sequentially
		for _, tc := range resp.ToolCalls {
			toolName := tc.Function.Name
			toolArgs := tc.Function.Arguments

			if onEvent != nil {
				onEvent(StreamEvent{
					Type:     "tool_start",
					ToolName: toolName,
					ToolArgs: toolArgs,
				})
			}

			var toolResult string
			var proposal *ActionProposal
			if ac.ToolsExec != nil {
				toolResult, proposal, err = ac.ToolsExec.ExecuteTool(ctx, toolName, toolArgs)
				if err != nil {
					toolResult = fmt.Sprintf("Erro ao executar ferramenta %s: %v", toolName, err)
				}
				if proposal != nil {
					lastProposal = proposal
					if onEvent != nil {
						onEvent(StreamEvent{
							Type:     "proposal",
							Proposal: proposal,
						})
					}
				}
			} else {
				toolResult = "Ferramentas não conectadas ao contexto do ScanFile."
			}

			if onEvent != nil {
				onEvent(StreamEvent{
					Type:     "tool_end",
					ToolName: toolName,
					Content:  truncateString(toolResult, 400),
				})
			}

			// Append tool response turn to message history
			messages = append(messages, Message{
				Role:       "tool",
				Content:    toolResult,
				ToolCallID: tc.ID,
				Name:       toolName,
			})
		}
	}

	if onEvent != nil {
		onEvent(StreamEvent{
			Type:     "done",
			Proposal: lastProposal,
		})
	}

	return lastAssistantMsg, nil
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
