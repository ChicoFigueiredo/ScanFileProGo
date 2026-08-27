package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"scanfile/pkg/ai"
)

// StartStdioServer launches the MCP server communicating over standard input/output.
func StartStdioServer(tc *MCPToolsContext) error {
	s := server.NewMCPServer(
		"ScanFile-AI-Intelligence",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	// 1. Tool: classify_files
	classifyTool := mcp.NewTool("classify_files",
		mcp.WithDescription("Classifica e filtra arquivos da árvore varrida do ScanFile por nome, faixa de tamanho em MB, extensão ou se são duplicatas."),
		mcp.WithString("directory", mcp.Description("Diretório raiz para filtrar (opcional)")),
		mcp.WithNumber("min_size_mb", mcp.Description("Tamanho mínimo em Megabytes")),
		mcp.WithNumber("max_size_mb", mcp.Description("Tamanho máximo em Megabytes")),
		mcp.WithString("extensions", mcp.Description("Extensões separadas por vírgula (ex: .pdf,.sqlite,.mp4)")),
		mcp.WithString("name_pattern", mcp.Description("Trecho de texto contido no nome do arquivo")),
		mcp.WithBoolean("duplicates_only", mcp.Description("Se true, retorna apenas arquivos duplicados por hash")),
		mcp.WithNumber("limit", mcp.Description("Quantidade máxima de itens retornados (padrão: 50)")),
	)
	s.AddTool(classifyTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		argBytes, _ := json.Marshal(req.Params.Arguments)
		var params ClassifyFilesParams
		_ = json.Unmarshal(argBytes, &params)

		items, err := tc.ClassifyFiles(ctx, params)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("erro ao classificar arquivos: %v", err)), nil
		}

		data, _ := json.MarshalIndent(items, "", "  ")
		return mcp.NewToolResultText(string(data)), nil
	})

	// 2. Tool: analyze_file_content
	analyzeTool := mcp.NewTool("analyze_file_content",
		mcp.WithDescription("Analisa o conteúdo interno de arquivos locais (extração de texto de PDFs, tabelas de SQLite ou leitura de amostras de texto/código)."),
		mcp.WithString("filepath", mcp.Required(), mcp.Description("Caminho absoluto do arquivo a inspecionar")),
		mcp.WithNumber("max_lines", mcp.Description("Número máximo de linhas ou amostras a extrair (padrão: 60)")),
		mcp.WithString("sqlite_query", mcp.Description("Query SELECT somente-leitura opcional para bancos SQLite")),
	)
	s.AddTool(analyzeTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		argBytes, _ := json.Marshal(req.Params.Arguments)
		var params AnalyzeFileParams
		_ = json.Unmarshal(argBytes, &params)

		if params.FilePath == "" {
			return mcp.NewToolResultError("filepath é obrigatório"), nil
		}

		res, err := tc.AnalyzeFileContent(ctx, params)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		data, _ := json.MarshalIndent(res, "", "  ")
		return mcp.NewToolResultText(string(data)), nil
	})

	// 3. Tool: write_file_metadata
	metaTool := mcp.NewTool("write_file_metadata",
		mcp.WithDescription("Escreve tags, categorias e notas semânticas de metadados para um arquivo ou hash no ScanFile."),
		mcp.WithString("target", mcp.Required(), mcp.Description("Caminho absoluto do arquivo ou hash SHA256")),
		mcp.WithString("category", mcp.Required(), mcp.Description("Categoria (ex: 'Financeiro', 'Temporário', 'Duplicata', 'Backup')")),
		mcp.WithString("notes", mcp.Description("Notas adicionais geradas pela IA")),
		mcp.WithBoolean("sidecar", mcp.Description("Se true, cria um arquivo .scanfile_meta.json no disco ao lado do arquivo")),
	)
	s.AddTool(metaTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		argBytes, _ := json.Marshal(req.Params.Arguments)
		var params WriteMetadataParams
		_ = json.Unmarshal(argBytes, &params)

		if params.Target == "" || params.Category == "" {
			return mcp.NewToolResultError("target e category são obrigatórios"), nil
		}

		meta, err := tc.WriteFileMetadata(params)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		data, _ := json.MarshalIndent(meta, "", "  ")
		return mcp.NewToolResultText(string(data)), nil
	})

	// 4. Tool: propose_actions
	actionTool := mcp.NewTool("propose_actions",
		mcp.WithDescription("Gera ou executa uma proposta de ação no disco (como mover para a Lixeira segura do Windows ou mover de pasta)."),
		mcp.WithString("action_type", mcp.Required(), mcp.Description("Tipo de ação: 'RECYCLE' (lixeira segura), 'MOVE', 'TAG', 'MARK_REVIEW'")),
		mcp.WithString("files_json", mcp.Description("Array JSON ou lista de caminhos absolutos dos arquivos")),
		mcp.WithBoolean("dry_run", mcp.Required(), mcp.Description("Se true, simula sem alterar nada no disco")),
		mcp.WithString("description", mcp.Description("Explicação da proposta gerada para o usuário")),
		mcp.WithString("destination", mcp.Description("Pasta de destino para ações do tipo 'MOVE'")),
	)
	s.AddTool(actionTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		argBytes, _ := json.Marshal(req.Params.Arguments)
		var params ProposeActionParams
		_ = json.Unmarshal(argBytes, &params)

		if len(params.Files) == 0 {
			// Support if sent as files_json string
			var rawMap map[string]interface{}
			_ = json.Unmarshal(argBytes, &rawMap)
			if fStr, ok := rawMap["files_json"].(string); ok && fStr != "" {
				_ = json.Unmarshal([]byte(fStr), &params.Files)
			}
		}

		proposal, err := tc.ProposeActions(params)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		data, _ := json.MarshalIndent(proposal, "", "  ")
		return mcp.NewToolResultText(string(data)), nil
	})

	return server.ServeStdio(s)
}

// GetOpenAIToolDefinitions returns tool definitions formatted for Ollama / OpenRouter Tool Calling.
func GetOpenAIToolDefinitions() []ai.ToolDefinition {
	return []ai.ToolDefinition{
		{
			Type: "function",
			Function: ai.FunctionDefinition{
				Name:        "classify_files",
				Description: "Classifica e busca arquivos da árvore escaneada do ScanFile por pasta, tamanho mínimo/máximo em MB, extensões, padrão de nome ou somente duplicatas.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"directory": map[string]interface{}{
							"type":        "string",
							"description": "Pasta raiz para filtrar (ex: 'C:\\Projetos')",
						},
						"min_size_mb": map[string]interface{}{
							"type":        "number",
							"description": "Tamanho mínimo em Megabytes (ex: 50.0)",
						},
						"max_size_mb": map[string]interface{}{
							"type":        "number",
							"description": "Tamanho máximo em Megabytes",
						},
						"extensions": map[string]interface{}{
							"type":        "array",
							"items":       map[string]interface{}{"type": "string"},
							"description": "Lista de extensões desejadas (ex: ['.pdf', '.sqlite', '.mp4'])",
						},
						"name_pattern": map[string]interface{}{
							"type":        "string",
							"description": "Palavra ou trecho no nome do arquivo",
						},
						"duplicates_only": map[string]interface{}{
							"type":        "boolean",
							"description": "Se verdadeiro, traz apenas arquivos que possuem duplicatas exatas por hash",
						},
						"limit": map[string]interface{}{
							"type":        "integer",
							"description": "Limite de arquivos no retorno (padrão 50)",
						},
					},
				},
			},
		},
		{
			Type: "function",
			Function: ai.FunctionDefinition{
				Name:        "analyze_file_content",
				Description: "Analisa profundamente o conteúdo local de um arquivo (extrai texto de PDFs, lista tabelas e esquemas de bancos SQLite ou obtém amostras de arquivos de texto/código).",
				Parameters: map[string]interface{}{
					"type": "object",
					"required": []string{"filepath"},
					"properties": map[string]interface{}{
						"filepath": map[string]interface{}{
							"type":        "string",
							"description": "Caminho absoluto do arquivo a ser analisado",
						},
						"max_lines": map[string]interface{}{
							"type":        "integer",
							"description": "Máximo de linhas/amostras para retornar (padrão 60)",
						},
						"sqlite_query": map[string]interface{}{
							"type":        "string",
							"description": "Consulta SQL SELECT somente-leitura opcional se o arquivo for .sqlite / .db",
						},
					},
				},
			},
		},
		{
			Type: "function",
			Function: ai.FunctionDefinition{
				Name:        "analyze_image_visual",
				Description: "Inspeciona visualmente uma imagem (PNG, JPG, WebP, etc.) usando visão multimodal (Qwen3-VL): descreve a cena, extrai texto legível via OCR (documentos/faturas/recibos), identifica o tipo e avalia a qualidade.",
				Parameters: map[string]interface{}{
					"type": "object",
					"required": []string{"filepath"},
					"properties": map[string]interface{}{
						"filepath": map[string]interface{}{
							"type":        "string",
							"description": "Caminho absoluto da imagem no disco",
						},
						"task": map[string]interface{}{
							"type":        "string",
							"enum":        []string{"describe", "ocr", "classify", "quality"},
							"description": "Tarefa específica de visão computacional (ex: 'ocr' para extração textual estrita)",
						},
					},
				},
			},
		},
		{
			Type: "function",
			Function: ai.FunctionDefinition{
				Name:        "compare_visual_similarity",
				Description: "Compara duas imagens visualmente com Qwen3-VL para identificar se são duplicatas visuais (mesmo que com resoluções, cortes ou formatos diferentes), recomendando qual manter.",
				Parameters: map[string]interface{}{
					"type": "object",
					"required": []string{"filepath_a", "filepath_b"},
					"properties": map[string]interface{}{
						"filepath_a": map[string]interface{}{
							"type":        "string",
							"description": "Caminho da primeira imagem",
						},
						"filepath_b": map[string]interface{}{
							"type":        "string",
							"description": "Caminho da segunda imagem a ser comparada",
						},
					},
				},
			},
		},
		{
			Type: "function",
			Function: ai.FunctionDefinition{
				Name:        "write_file_metadata",
				Description: "Grava tags e metadados categorizados para um arquivo ou hash no ScanFile.",
				Parameters: map[string]interface{}{
					"type": "object",
					"required": []string{"target", "category"},
					"properties": map[string]interface{}{
						"target": map[string]interface{}{
							"type":        "string",
							"description": "Caminho absoluto do arquivo ou hash",
						},
						"category": map[string]interface{}{
							"type":        "string",
							"description": "Categoria (ex: 'Documentos Fiscais', 'Backup Antigo', 'Código Fonte', 'Lixo Temporário')",
						},
						"notes": map[string]interface{}{
							"type":        "string",
							"description": "Resumo ou notas geradas pela IA sobre o arquivo",
						},
						"sidecar": map[string]interface{}{
							"type":        "boolean",
							"description": "Se verdadeiro, grava arquivo sidecar .scanfile_meta.json ao lado do arquivo",
						},
					},
				},
			},
		},
		{
			Type: "function",
			Function: ai.FunctionDefinition{
				Name:        "propose_actions",
				Description: "Formula uma proposta de ação de limpeza/organização de arquivos com modo de simulação seguro (dry_run).",
				Parameters: map[string]interface{}{
					"type": "object",
					"required": []string{"action_type", "files", "dry_run"},
					"properties": map[string]interface{}{
						"action_type": map[string]interface{}{
							"type":        "string",
							"enum":        []string{"RECYCLE", "MOVE", "TAG", "MARK_REVIEW"},
							"description": "Tipo de ação a ser executada",
						},
						"files": map[string]interface{}{
							"type":        "array",
							"items":       map[string]interface{}{"type": "string"},
							"description": "Lista de caminhos absolutos dos arquivos afetados",
						},
						"dry_run": map[string]interface{}{
							"type":        "boolean",
							"description": "Se true, apenas calcula o impacto e gera proposta para aprovação sem alterar o disco",
						},
						"description": map[string]interface{}{
							"type":        "string",
							"description": "Explicação resumida do que essa ação fará e quanto espaço será liberado",
						},
						"destination": map[string]interface{}{
							"type":        "string",
							"description": "Pasta de destino para ações do tipo MOVE",
						},
					},
				},
			},
		},
	}
}
