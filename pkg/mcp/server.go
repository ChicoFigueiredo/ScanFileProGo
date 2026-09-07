package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"scanfile/pkg/ai"
)

// ServerName and ServerVersion identify the ScanFile MCP server to its clients.
const (
	ServerName    = "ScanFile-AI-Intelligence"
	ServerVersion = "1.0.0"
)

// NewStdioServer builds the MCP server with every ScanFile tool registered.
// server.WithRecovery() keeps a panic inside a tool handler from taking the whole
// stdio server down (finding M8).
func NewStdioServer(tc *MCPToolsContext) *server.MCPServer {
	s := server.NewMCPServer(
		ServerName,
		ServerVersion,
		server.WithToolCapabilities(true),
		server.WithRecovery(),
	)

	registerClassifyFiles(s, tc)
	registerAnalyzeFileContent(s, tc)
	registerAnalyzeImageVisual(s, tc)
	registerCompareVisualSimilarity(s, tc)
	registerWriteFileMetadata(s, tc)
	registerProposeActions(s, tc)

	return s
}

// StartStdioServer launches the MCP server communicating over standard input/output.
func StartStdioServer(tc *MCPToolsContext) error {
	return server.ServeStdio(NewStdioServer(tc))
}

// decodeParams unmarshals the raw tool arguments into out and returns the raw JSON.
func decodeParams(req mcp.CallToolRequest, out interface{}) []byte {
	var argBytes []byte
	if raw, ok := req.GetRawArguments().(json.RawMessage); ok && len(raw) > 0 {
		argBytes = raw
	} else {
		argBytes, _ = json.Marshal(req.GetArguments())
	}
	_ = json.Unmarshal(argBytes, out)
	return argBytes
}

func jsonResult(v interface{}) *mcp.CallToolResult {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("falha ao serializar resposta: %v", err))
	}
	return mcp.NewToolResultText(string(data))
}

// 1. Tool: classify_files
func registerClassifyFiles(s *server.MCPServer, tc *MCPToolsContext) {
	tool := mcp.NewTool("classify_files",
		mcp.WithDescription("Classifica e filtra arquivos da árvore varrida do ScanFile por nome, faixa de tamanho em MB, extensão ou se são duplicatas."),
		mcp.WithString("directory", mcp.Description("Diretório raiz para filtrar (opcional)")),
		mcp.WithNumber("min_size_mb", mcp.Description("Tamanho mínimo em Megabytes")),
		mcp.WithNumber("max_size_mb", mcp.Description("Tamanho máximo em Megabytes")),
		mcp.WithString("extensions", mcp.Description("Extensões separadas por vírgula (ex: .pdf,.sqlite,.mp4)")),
		mcp.WithString("name_pattern", mcp.Description("Trecho de texto contido no nome do arquivo")),
		mcp.WithBoolean("duplicates_only", mcp.Description("Se true, retorna apenas arquivos duplicados por hash")),
		mcp.WithNumber("limit", mcp.Description("Quantidade máxima de itens retornados (padrão: 50)")),
	)

	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var params ClassifyFilesParams
		decodeParams(req, &params)

		items, err := tc.ClassifyFiles(ctx, params)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("erro ao classificar arquivos: %v", err)), nil
		}
		return jsonResult(items), nil
	})
}

// 2. Tool: analyze_file_content
func registerAnalyzeFileContent(s *server.MCPServer, tc *MCPToolsContext) {
	tool := mcp.NewTool("analyze_file_content",
		mcp.WithDescription("Analisa o conteúdo interno de arquivos dentro das Raízes Varridas (extração de texto de PDFs, tabelas de SQLite ou leitura de amostras de texto/código)."),
		mcp.WithString("filepath", mcp.Required(), mcp.Description("Caminho absoluto do arquivo a inspecionar, obrigatoriamente dentro das Raízes Varridas")),
		mcp.WithNumber("max_lines", mcp.Description("Número máximo de linhas ou amostras a extrair (padrão: 60)")),
		mcp.WithString("sqlite_query", mcp.Description("Consulta SELECT somente-leitura opcional para bancos SQLite (sem ';', ATTACH, PRAGMA ou load_extension)")),
	)

	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var params AnalyzeFileParams
		decodeParams(req, &params)

		if params.FilePath == "" {
			return mcp.NewToolResultError("filepath é obrigatório"), nil
		}

		res, err := tc.AnalyzeFileContent(ctx, params)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return jsonResult(res), nil
	})
}

// 3. Tool: analyze_image_visual
func registerAnalyzeImageVisual(s *server.MCPServer, tc *MCPToolsContext) {
	tool := mcp.NewTool("analyze_image_visual",
		mcp.WithDescription("Inspeciona visualmente uma imagem dentro das Raízes Varridas (PNG, JPG, WebP...): descreve a cena, extrai texto legível via OCR, identifica o tipo e avalia a qualidade. Exige um modelo com visão."),
		mcp.WithString("filepath", mcp.Required(), mcp.Description("Caminho absoluto da imagem, obrigatoriamente dentro das Raízes Varridas")),
		mcp.WithString("task", mcp.Description("Tarefa de visão: 'describe', 'ocr', 'classify' ou 'quality'")),
	)

	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var params AnalyzeImageParams
		decodeParams(req, &params)

		if params.FilePath == "" {
			return mcp.NewToolResultError("filepath é obrigatório"), nil
		}

		res, err := tc.AnalyzeImageVisual(ctx, params)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return jsonResult(res), nil
	})
}

// 4. Tool: compare_visual_similarity
func registerCompareVisualSimilarity(s *server.MCPServer, tc *MCPToolsContext) {
	tool := mcp.NewTool("compare_visual_similarity",
		mcp.WithDescription("Compara duas imagens dentro das Raízes Varridas para identificar duplicatas visuais (resoluções, cortes ou formatos diferentes) e recomendar qual manter."),
		mcp.WithString("filepath_a", mcp.Required(), mcp.Description("Caminho da primeira imagem")),
		mcp.WithString("filepath_b", mcp.Required(), mcp.Description("Caminho da segunda imagem")),
	)

	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var params CompareVisualParams
		decodeParams(req, &params)

		if params.FilePathA == "" || params.FilePathB == "" {
			return mcp.NewToolResultError("filepath_a e filepath_b são obrigatórios"), nil
		}

		res, err := tc.CompareVisualSimilarity(ctx, params)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return jsonResult(res), nil
	})
}

// 5. Tool: write_file_metadata
func registerWriteFileMetadata(s *server.MCPServer, tc *MCPToolsContext) {
	tool := mcp.NewTool("write_file_metadata",
		mcp.WithDescription("Escreve tags, categorias e notas semânticas de metadados para um arquivo ou hash no ScanFile."),
		mcp.WithString("target", mcp.Required(), mcp.Description("Caminho absoluto do arquivo ou hash")),
		mcp.WithString("category", mcp.Required(), mcp.Description("Categoria (ex: 'Financeiro', 'Temporário', 'Duplicata', 'Backup')")),
		mcp.WithString("notes", mcp.Description("Notas adicionais geradas pelo Assistente")),
		mcp.WithBoolean("sidecar", mcp.Description("Se true, grava um arquivo .scanfile_meta.json ao lado do alvo (exige alvo dentro das Raízes Varridas)")),
	)

	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var params WriteMetadataParams
		decodeParams(req, &params)

		if params.Target == "" || params.Category == "" {
			return mcp.NewToolResultError("target e category são obrigatórios"), nil
		}

		meta, err := tc.WriteFileMetadata(params)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return jsonResult(meta), nil
	})
}

// 6. Tool: propose_actions
func registerProposeActions(s *server.MCPServer, tc *MCPToolsContext) {
	tool := mcp.NewTool("propose_actions",
		mcp.WithDescription("Registra uma Proposta de ação sobre arquivos (Reciclagem, mover, marcar). A Proposta fica SEMPRE pendente: nada é alterado no disco até o usuário aprovar na interface do ScanFile."),
		mcp.WithString("action_type", mcp.Required(), mcp.Description("Tipo de ação: 'RECYCLE' (Lixeira), 'MOVE', 'TAG' ou 'MARK_REVIEW'")),
		mcp.WithString("files_json", mcp.Description("Array JSON com os caminhos absolutos dos arquivos, todos dentro das Raízes Varridas")),
		mcp.WithString("description", mcp.Description("Explicação da Proposta apresentada ao usuário")),
		mcp.WithString("destination", mcp.Description("Pasta de destino para ações do tipo 'MOVE'")),
		mcp.WithString("category", mcp.Description("Categoria aplicada em ações do tipo 'TAG'")),
	)

	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var params ProposeActionParams
		argBytes := decodeParams(req, &params)

		if len(params.Files) == 0 {
			// Some clients send the list as a JSON string in files_json.
			var rawMap map[string]interface{}
			_ = json.Unmarshal(argBytes, &rawMap)
			if fStr, ok := rawMap["files_json"].(string); ok && fStr != "" {
				_ = json.Unmarshal([]byte(fStr), &params.Files)
			}
		}

		// The model can never request execution: ProposeActions only registers.
		params.DryRun = true

		proposal, err := tc.ProposeActions(params)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return jsonResult(proposal), nil
	})
}

// GetOpenAIToolDefinitions returns tool definitions formatted for Ollama / OpenRouter Tool Calling.
func GetOpenAIToolDefinitions() []ai.ToolDefinition {
	return []ai.ToolDefinition{
		{
			Type: "function",
			Function: ai.FunctionDefinition{
				Name:        "classify_files",
				Description: "Classifica e busca arquivos da árvore varrida do ScanFile por pasta, tamanho mínimo/máximo em MB, extensões, padrão de nome ou somente duplicatas.",
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
				Description: "Analisa o conteúdo local de um arquivo dentro das Raízes Varridas (extrai texto de PDFs, lista tabelas e esquemas de bancos SQLite em modo somente leitura ou obtém amostras de arquivos de texto/código).",
				Parameters: map[string]interface{}{
					"type":     "object",
					"required": []string{"filepath"},
					"properties": map[string]interface{}{
						"filepath": map[string]interface{}{
							"type":        "string",
							"description": "Caminho absoluto do arquivo a ser analisado, obrigatoriamente dentro das Raízes Varridas",
						},
						"max_lines": map[string]interface{}{
							"type":        "integer",
							"description": "Máximo de linhas/amostras para retornar (padrão 60)",
						},
						"sqlite_query": map[string]interface{}{
							"type":        "string",
							"description": "Consulta SELECT somente-leitura opcional se o arquivo for .sqlite / .db. Não pode conter ';', ATTACH, PRAGMA nem load_extension",
						},
					},
				},
			},
		},
		{
			Type: "function",
			Function: ai.FunctionDefinition{
				Name:        "analyze_image_visual",
				Description: "Inspeciona visualmente uma imagem (PNG, JPG, WebP, etc.) dentro das Raízes Varridas usando um modelo com visão: descreve a cena, extrai texto legível via OCR (documentos/faturas/recibos), identifica o tipo e avalia a qualidade.",
				Parameters: map[string]interface{}{
					"type":     "object",
					"required": []string{"filepath"},
					"properties": map[string]interface{}{
						"filepath": map[string]interface{}{
							"type":        "string",
							"description": "Caminho absoluto da imagem, obrigatoriamente dentro das Raízes Varridas",
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
				Description: "Compara duas imagens das Raízes Varridas para identificar se são duplicatas visuais (mesmo com resoluções, cortes ou formatos diferentes), recomendando qual manter.",
				Parameters: map[string]interface{}{
					"type":     "object",
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
				Description: "Grava tags e metadados categorizados para um arquivo ou hash no ScanFile. O sidecar em disco só é permitido dentro das Raízes Varridas.",
				Parameters: map[string]interface{}{
					"type":     "object",
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
							"description": "Resumo ou notas geradas pelo Assistente sobre o arquivo",
						},
						"sidecar": map[string]interface{}{
							"type":        "boolean",
							"description": "Se verdadeiro, grava arquivo sidecar .scanfile_meta.json ao lado do alvo",
						},
					},
				},
			},
		},
		{
			Type: "function",
			Function: ai.FunctionDefinition{
				Name:        "propose_actions",
				Description: "Registra uma Proposta de limpeza ou organização de arquivos. A Proposta fica SEMPRE pendente de aprovação humana: esta ferramenta nunca altera o disco.",
				Parameters: map[string]interface{}{
					"type":     "object",
					"required": []string{"action_type", "files"},
					"properties": map[string]interface{}{
						"action_type": map[string]interface{}{
							"type":        "string",
							"enum":        []string{"RECYCLE", "MOVE", "TAG", "MARK_REVIEW"},
							"description": "Tipo de ação proposta",
						},
						"files": map[string]interface{}{
							"type":        "array",
							"items":       map[string]interface{}{"type": "string"},
							"description": "Lista de caminhos absolutos dos arquivos afetados, todos dentro das Raízes Varridas",
						},
						"description": map[string]interface{}{
							"type":        "string",
							"description": "Explicação resumida do que essa ação fará e quanto espaço será liberado",
						},
						"destination": map[string]interface{}{
							"type":        "string",
							"description": "Pasta de destino para ações do tipo MOVE",
						},
						"category": map[string]interface{}{
							"type":        "string",
							"description": "Categoria aplicada em ações do tipo TAG",
						},
					},
				},
			},
		},
	}
}
