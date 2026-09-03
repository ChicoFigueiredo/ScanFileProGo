package mcp

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ledongthuc/pdf"
	_ "modernc.org/sqlite"

	"scanfile/pkg/ai"
	"scanfile/pkg/indexer"
	"scanfile/pkg/recycle"
	"scanfile/pkg/scanner"
)

// ProposalTTL is how long a pending Proposta stays executable before expiring.
const ProposalTTL = 30 * time.Minute

// RecycleFunc sends the given paths to the Windows Recycle Bin. It is injectable so
// that tests never touch the real Recycle Bin.
type RecycleFunc func(paths []string) recycle.BatchDeleteResult

// MCPToolsContext holds references to ScanFile core engines and optional VL client.
type MCPToolsContext struct {
	Tree         *scanner.TreeManager
	Index        *indexer.DuplicateIndex
	FolderIndex  *indexer.FolderDuplicateIndex
	OllamaClient *ai.OllamaClient
	OllamaModel  string

	// RecycleFunc performs the RECYCLE action of an approved Proposta.
	// Defaults to recycle.BatchDelete with the sizes measured just before the call.
	RecycleFunc RecycleFunc

	// Raízes Varridas: the only paths file-reading tools are allowed to touch.
	rootsMu      sync.RWMutex
	allowedRoots []string

	// Active proposals cache for two-phase approval
	proposalsMu sync.RWMutex
	proposals   map[string]*ActionProposal

	// execMu serialises approved executions so two approvals of the same Proposta
	// never act on the disk concurrently.
	execMu sync.Mutex

	// Metadata store
	metadataMu sync.RWMutex
	metadata   map[string]FileMetadata
}

// FileMetadata holds AI-tagged metadata for a file or hash.
type FileMetadata struct {
	FilePath  string    `json:"filePath"`
	Hash      string    `json:"hash,omitempty"`
	Category  string    `json:"category"`
	Tags      []string  `json:"tags,omitempty"`
	Notes     string    `json:"notes,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ActionProposal represents a Proposta: an action over files that stays pending until
// the user approves it in the interface. The Assistente never executes it directly.
type ActionProposal struct {
	ID          string    `json:"id"`
	ActionType  string    `json:"actionType"` // "RECYCLE", "MOVE", "TAG", "MARK_REVIEW"
	Description string    `json:"description"`
	Files       []string  `json:"files"`
	FileCount   int       `json:"fileCount"`
	TotalBytes  int64     `json:"totalBytes"`
	TotalSize   string    `json:"totalSize"`
	Destination string    `json:"destination,omitempty"`
	Category    string    `json:"category,omitempty"`
	DryRun      bool      `json:"dryRun"`
	Executed    bool      `json:"executed"`
	CreatedAt   time.Time `json:"createdAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
	Errors      []string  `json:"errors,omitempty"`
}

// NewMCPToolsContext initializes the tools context with optional vision model access.
func NewMCPToolsContext(tree *scanner.TreeManager, idx *indexer.DuplicateIndex, fIdx *indexer.FolderDuplicateIndex, ollamaClient *ai.OllamaClient, ollamaModel string) *MCPToolsContext {
	if ollamaModel == "" {
		ollamaModel = "qwen3-vl:8b"
	}
	return &MCPToolsContext{
		Tree:         tree,
		Index:        idx,
		FolderIndex:  fIdx,
		OllamaClient: ollamaClient,
		OllamaModel:  ollamaModel,
		proposals:    make(map[string]*ActionProposal),
		metadata:     make(map[string]FileMetadata),
	}
}

// =========================================================================
// 1. TOOL: Classify Files
// =========================================================================

type ClassifyFilesParams struct {
	Directory      string   `json:"directory,omitempty"`
	MinSizeMB      float64  `json:"min_size_mb,omitempty"`
	MaxSizeMB      float64  `json:"max_size_mb,omitempty"`
	Extensions     []string `json:"extensions,omitempty"`
	NamePattern    string   `json:"name_pattern,omitempty"`
	DuplicatesOnly bool     `json:"duplicates_only,omitempty"`
	Limit          int      `json:"limit,omitempty"`
}

type ClassifiedFileItem struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	Extension   string `json:"extension"`
	SizeBytes   int64  `json:"sizeBytes"`
	SizeDisplay string `json:"sizeDisplay"`
	ModTime     string `json:"modTime"`
	IsDuplicate bool   `json:"isDuplicate"`
	Hash        string `json:"hash,omitempty"`
}

func (tc *MCPToolsContext) ClassifyFiles(ctx context.Context, params ClassifyFilesParams) ([]ClassifiedFileItem, error) {
	if params.Limit <= 0 || params.Limit > 200 {
		params.Limit = 50
	}

	var results []ClassifiedFileItem

	// 1. If searching for duplicates specifically from the index
	if params.DuplicatesOnly && tc.Index != nil {
		minBytes := int64(params.MinSizeMB * 1024 * 1024)
		qRes := tc.Index.Query(indexer.QueryFilter{
			MinSize: minBytes,
			Search:  params.Directory,
			Limit:   params.Limit,
		})

		for _, group := range qRes.Groups {
			for i, f := range group.Files {
				if params.Directory != "" && !strings.HasPrefix(strings.ToLower(f.Path), strings.ToLower(params.Directory)) {
					continue
				}
				results = append(results, ClassifiedFileItem{
					Path:        f.Path,
					Name:        f.Name,
					Extension:   f.Extension,
					SizeBytes:   f.Size,
					SizeDisplay: formatBytes(f.Size),
					ModTime:     time.Unix(f.ModTime, 0).Format("2006-01-02 15:04"),
					IsDuplicate: i > 0,
					Hash:        group.Hash,
				})
				if len(results) >= params.Limit {
					return results, nil
				}
			}
		}
		return results, nil
	}

	// 2. Query from TreeManager
	if tc.Tree != nil {
		minBytes := int64(params.MinSizeMB * 1024 * 1024)
		maxBytes := int64(params.MaxSizeMB * 1024 * 1024)

		extMap := make(map[string]bool)
		for _, e := range params.Extensions {
			if !strings.HasPrefix(e, ".") {
				e = "." + e
			}
			extMap[strings.ToLower(e)] = true
		}

		var walkFn func(dir *scanner.DirNode)
		walkFn = func(dir *scanner.DirNode) {
			if dir == nil || len(results) >= params.Limit {
				return
			}
			// Inspect files in dir
			for _, f := range dir.Files {
				if params.Directory != "" && !strings.HasPrefix(strings.ToLower(f.Path), strings.ToLower(params.Directory)) {
					continue
				}
				if minBytes > 0 && f.Size < minBytes {
					continue
				}
				if maxBytes > 0 && f.Size > maxBytes {
					continue
				}
				if len(extMap) > 0 {
					ext := strings.ToLower(f.Extension)
					if !extMap[ext] {
						continue
					}
				}
				if params.NamePattern != "" && !strings.Contains(strings.ToLower(f.Name), strings.ToLower(params.NamePattern)) {
					continue
				}

				results = append(results, ClassifiedFileItem{
					Path:        f.Path,
					Name:        f.Name,
					Extension:   f.Extension,
					SizeBytes:   f.Size,
					SizeDisplay: formatBytes(f.Size),
					ModTime:     time.Unix(f.ModTime, 0).Format("2006-01-02 15:04"),
					IsDuplicate: f.Hash != "",
					Hash:        f.Hash,
				})

				if len(results) >= params.Limit {
					return
				}
			}

			// Traverse subdirectories
			for _, child := range dir.Children {
				walkFn(child)
			}
		}

		roots := tc.Tree.GetRootsSnapshot()
		for _, root := range roots {
			walkFn(root)
			if len(results) >= params.Limit {
				break
			}
		}
	}

	return results, nil
}

// =========================================================================
// 2. TOOL: Analyze File Content (PDF, SQLite, TXT, Logs, Code)
// =========================================================================

type AnalyzeFileParams struct {
	FilePath    string `json:"filepath"`
	MaxLines    int    `json:"max_lines,omitempty"`
	SQLiteQuery string `json:"sqlite_query,omitempty"`
}

type FileAnalysisResult struct {
	FilePath    string                 `json:"filepath"`
	FileName    string                 `json:"fileName"`
	FileType    string                 `json:"fileType"`
	SizeBytes   int64                  `json:"sizeBytes"`
	SizeDisplay string                 `json:"sizeDisplay"`
	MIMEType    string                 `json:"mimeType"`
	Summary     string                 `json:"summary"`
	SampleText  string                 `json:"sampleText,omitempty"`
	SQLiteData  *SQLiteInspection      `json:"sqliteData,omitempty"`
	PDFData     *PDFInspection         `json:"pdfData,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

type SQLiteInspection struct {
	Tables     []string                 `json:"tables"`
	TotalRows  int                      `json:"totalRows"`
	Schema     map[string]string        `json:"schema"`
	QueryRows  []map[string]interface{} `json:"queryRows,omitempty"`
	QueryError string                   `json:"queryError,omitempty"`
}

type PDFInspection struct {
	NumPages  int    `json:"numPages"`
	Author    string `json:"author,omitempty"`
	Title     string `json:"title,omitempty"`
	Extracted string `json:"extracted"`
}

func (tc *MCPToolsContext) AnalyzeFileContent(ctx context.Context, params AnalyzeFileParams) (*FileAnalysisResult, error) {
	if params.FilePath == "" {
		return nil, fmt.Errorf("filepath é obrigatório")
	}

	cleanPath, err := tc.ensurePathAllowed(params.FilePath)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("arquivo não encontrado: %w", err)
	}

	if info.IsDir() {
		return nil, fmt.Errorf("o caminho especificado é um diretório, não um arquivo")
	}

	ext := strings.ToLower(filepath.Ext(cleanPath))
	res := &FileAnalysisResult{
		FilePath:    cleanPath,
		FileName:    info.Name(),
		FileType:    ext,
		SizeBytes:   info.Size(),
		SizeDisplay: formatBytes(info.Size()),
		Metadata:    make(map[string]interface{}),
	}

	if params.MaxLines <= 0 {
		params.MaxLines = 60
	}

	// 1. Image Inspection (Native Multimodal Vision with Qwen3-VL)
	if isImageExtension(ext) {
		imgRes, err := tc.AnalyzeImageVisual(ctx, AnalyzeImageParams{
			FilePath: cleanPath,
		})
		if err == nil {
			res.FileType = fmt.Sprintf("Imagem %s (%s)", strings.ToUpper(imgRes.Format), imgRes.AspectRatio)
			res.MIMEType = "image/" + strings.TrimPrefix(ext, ".")
			res.Summary = fmt.Sprintf("Imagem %s (%dx%d, %s). Categoria: %s. %s", 
				imgRes.Format, imgRes.Width, imgRes.Height, imgRes.SizeDisplay, imgRes.SuggestedCategory, imgRes.VisualDescription)
			if imgRes.DetectedTextOCR != "" {
				res.SampleText = fmt.Sprintf("=== TEXTO DETECTADO VIA OCR (Qwen3-VL) ===\n%s", imgRes.DetectedTextOCR)
			}
			res.Metadata["dimensions"] = fmt.Sprintf("%dx%d", imgRes.Width, imgRes.Height)
			res.Metadata["aspectRatio"] = imgRes.AspectRatio
			res.Metadata["documentType"] = imgRes.DocumentType
			res.Metadata["quality"] = imgRes.QualityAssessment
			return res, nil
		}
	}

	// 2. PDF Deep Extraction
	if ext == ".pdf" {
		pdfData, err := inspectPDF(cleanPath, params.MaxLines)
		if err == nil {
			res.PDFData = pdfData
			res.FileType = "PDF Document"
			res.MIMEType = "application/pdf"
			res.Summary = fmt.Sprintf("Documento PDF com %d páginas. Título: '%s'. Autor: '%s'.", pdfData.NumPages, pdfData.Title, pdfData.Author)
			res.SampleText = pdfData.Extracted
			return res, nil
		}
	}

	// 3. SQLite Database Inspection
	if ext == ".sqlite" || ext == ".db" || ext == ".sqlite3" {
		sqliteData, err := inspectSQLite(ctx, cleanPath, params.SQLiteQuery)
		if err == nil {
			res.SQLiteData = sqliteData
			res.FileType = "SQLite Database"
			res.MIMEType = "application/x-sqlite3"
			res.Summary = fmt.Sprintf("Banco SQLite com %d tabelas: [%s].", len(sqliteData.Tables), strings.Join(sqliteData.Tables, ", "))
			return res, nil
		}
	}

	// 3. Text / Code / CSV / JSON / Log Inspection
	f, err := os.Open(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("falha ao abrir arquivo: %w", err)
	}
	defer f.Close()

	buf := make([]byte, 16384) // 16KB sample
	n, _ := f.Read(buf)
	sample := buf[:n]

	res.MIMEType = http.DetectContentType(sample)

	sampleStr := string(sample)
	lines := strings.Split(sampleStr, "\n")
	if len(lines) > params.MaxLines {
		lines = lines[:params.MaxLines]
		sampleStr = strings.Join(lines, "\n") + "\n... [truncado pelo limite de amostra]"
	}

	res.SampleText = sampleStr
	res.Summary = fmt.Sprintf("Arquivo (%s), %d bytes. Amostra com %d linhas.", res.MIMEType, info.Size(), len(lines))

	return res, nil
}

func inspectPDF(pdfPath string, maxLines int) (*PDFInspection, error) {
	file, reader, err := pdf.Open(pdfPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	numPages := reader.NumPage()
	var extracted strings.Builder

	maxPages := numPages
	if maxPages > 3 {
		maxPages = 3
	}

	for pageIndex := 1; pageIndex <= maxPages; pageIndex++ {
		p := reader.Page(pageIndex)
		if p.V.IsNull() {
			continue
		}
		text, errPage := p.GetPlainText(nil)
		if errPage == nil {
			extracted.WriteString(fmt.Sprintf("--- Página %d ---\n", pageIndex))
			extracted.WriteString(text)
			extracted.WriteString("\n")
		}
	}

	lines := strings.Split(extracted.String(), "\n")
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}

	return &PDFInspection{
		NumPages:  numPages,
		Extracted: strings.Join(lines, "\n"),
	}, nil
}

func inspectSQLite(ctx context.Context, dbPath string, userQuery string) (*SQLiteInspection, error) {
	db, err := openReadOnlySQLite(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, "SELECT name, sql FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []string
	schema := make(map[string]string)
	for rows.Next() {
		var name, sqlDef sql.NullString
		if errScan := rows.Scan(&name, &sqlDef); errScan == nil && name.Valid {
			tables = append(tables, name.String)
			schema[name.String] = sqlDef.String
		}
	}

	res := &SQLiteInspection{
		Tables: tables,
		Schema: schema,
	}

	if strings.TrimSpace(userQuery) == "" {
		return res, nil
	}

	if errVal := validateUserSQLiteQuery(userQuery); errVal != nil {
		res.QueryError = fmt.Sprintf("consulta recusada: %v", errVal)
		return res, nil
	}

	qCtx, cancel := context.WithTimeout(ctx, sqliteQueryTimeoutSeconds*time.Second)
	defer cancel()

	qRows, errQ := db.QueryContext(qCtx, userQuery)
	if errQ != nil {
		res.QueryError = fmt.Sprintf("falha ao executar a consulta: %v", errQ)
		return res, nil
	}
	defer qRows.Close()

	cols, _ := qRows.Columns()
	for qRows.Next() && len(res.QueryRows) < maxSQLiteQueryRows {
		columns := make([]interface{}, len(cols))
		columnPointers := make([]interface{}, len(cols))
		for i := range columns {
			columnPointers[i] = &columns[i]
		}
		if errScan := qRows.Scan(columnPointers...); errScan == nil {
			rowMap := make(map[string]interface{})
			for i, colName := range cols {
				val := columns[i]
				if b, ok := val.([]byte); ok {
					rowMap[colName] = string(b)
				} else {
					rowMap[colName] = val
				}
			}
			res.QueryRows = append(res.QueryRows, rowMap)
		}
	}
	if errRows := qRows.Err(); errRows != nil {
		res.QueryError = fmt.Sprintf("falha ao ler o resultado da consulta: %v", errRows)
	}

	return res, nil
}

func isImageExtension(ext string) bool {
	switch ext {
	case ".png", ".jpg", ".jpeg", ".webp", ".bmp", ".gif", ".tiff", ".tif", ".ico", ".svg":
		return true
	default:
		return false
	}
}

// =========================================================================
// 2.1 TOOL: Analyze Image Visual (Qwen3-VL Multimodal Vision & OCR)
// =========================================================================

type AnalyzeImageParams struct {
	FilePath      string `json:"filepath"`
	Task          string `json:"task,omitempty"` // "describe", "ocr", "classify", "quality"
	IncludeBase64 bool   `json:"include_base64,omitempty"`
}

type ImageVisualAnalysis struct {
	FilePath          string `json:"filePath"`
	FileName          string `json:"fileName"`
	Format            string `json:"format"`
	Width             int    `json:"width"`
	Height            int    `json:"height"`
	AspectRatio       string `json:"aspectRatio"`
	SizeBytes         int64  `json:"sizeBytes"`
	SizeDisplay       string `json:"sizeDisplay"`
	VisualDescription string `json:"visualDescription"`
	DetectedTextOCR   string `json:"detectedTextOCR,omitempty"`
	DocumentType      string `json:"documentType"`
	SuggestedCategory string `json:"suggestedCategory"`
	QualityAssessment string `json:"qualityAssessment"`
	Base64Data        string `json:"base64Data,omitempty"`
}

func (tc *MCPToolsContext) AnalyzeImageVisual(ctx context.Context, params AnalyzeImageParams) (*ImageVisualAnalysis, error) {
	if params.FilePath == "" {
		return nil, fmt.Errorf("filepath é obrigatório")
	}

	cleanPath, err := tc.ensurePathAllowed(params.FilePath)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("imagem não encontrada: %w", err)
	}

	format, width, height, b64, errImg := decodeImageMetaAndBase64(cleanPath, 15*1024*1024)
	if errImg != nil {
		format = strings.TrimPrefix(filepath.Ext(cleanPath), ".")
	}

	aspectRatio := "Desconhecido"
	if width > 0 && height > 0 {
		ratio := float64(width) / float64(height)
		if ratio >= 1.7 && ratio <= 1.85 {
			aspectRatio = "16:9 (Widescreen)"
		} else if ratio >= 1.3 && ratio <= 1.4 {
			aspectRatio = "4:3 (Padrão Fotográfico)"
		} else if ratio >= 0.95 && ratio <= 1.05 {
			aspectRatio = "1:1 (Quadrado / Ícone)"
		} else if ratio < 0.65 {
			aspectRatio = "9:16 (Vertical / Smartphone / Story)"
		} else {
			aspectRatio = fmt.Sprintf("%.2f:1 (%dx%d)", ratio, width, height)
		}
	}

	res := &ImageVisualAnalysis{
		FilePath:          cleanPath,
		FileName:          info.Name(),
		Format:            strings.ToUpper(format),
		Width:             width,
		Height:            height,
		AspectRatio:       aspectRatio,
		SizeBytes:         info.Size(),
		SizeDisplay:       formatBytes(info.Size()),
		DocumentType:      "Imagem / Fotografia",
		SuggestedCategory: "Mídia Visual",
		QualityAssessment: "Resolução e metadados válidos",
	}

	if params.IncludeBase64 {
		res.Base64Data = b64
	}

	// If Ollama is available, query Qwen3-VL directly with vision
	if tc.OllamaClient != nil && b64 != "" {
		model := tc.OllamaModel
		if model == "" {
			model = "qwen3-vl:8b"
		}

		prompt := fmt.Sprintf("Você é o modelo multimodal Qwen3-VL. Analise esta imagem (%s, %dx%d, %s) com máxima precisão e responda em português estruturado:\n1. Descrição visual detalhada do conteúdo da imagem/documento;\n2. Transcreva todo o texto legível via OCR (se houver notas fiscais, placas, títulos, tabelas ou recibos);\n3. Tipo de arquivo (ex: Documento/Fatura, Foto Pessoal, Screenshot de Software, Logotipo, Wallpaper, Diagrama Técnico);\n4. Sugestão de categoria para organização de disco;\n5. Qualidade visual e se parece duplicata/descartável.", info.Name(), width, height, formatBytes(info.Size()))

		if params.Task == "ocr" {
			prompt = "Extraia todo o texto visível nesta imagem com máxima fidelidade (OCR). Se for um documento fiscal, certidão ou fatura, transcreva os campos essenciais."
		}

		msg := ai.Message{
			Role:    "user",
			Content: prompt,
			Images:  []string{b64},
		}

		resp, errChat := tc.OllamaClient.Chat(ctx, model, []ai.Message{msg}, nil)
		if errChat == nil && resp != nil && resp.Content != "" {
			res.VisualDescription = resp.Content
			// Check if contains OCR text
			if strings.Contains(strings.ToLower(resp.Content), "texto") || strings.Contains(strings.ToLower(resp.Content), "ocr") {
				res.DetectedTextOCR = resp.Content
			}
			if strings.Contains(strings.ToLower(resp.Content), "fatura") || strings.Contains(strings.ToLower(resp.Content), "documento") || strings.Contains(strings.ToLower(resp.Content), "recibo") {
				res.DocumentType = "Documento Digital / Recibo"
				res.SuggestedCategory = "Documentos e Finanças"
			} else if strings.Contains(strings.ToLower(resp.Content), "screenshot") || strings.Contains(strings.ToLower(resp.Content), "captura") {
				res.DocumentType = "Screenshot de Tela"
				res.SuggestedCategory = "Capturas Temporárias"
			} else if strings.Contains(strings.ToLower(resp.Content), "foto") || strings.Contains(strings.ToLower(resp.Content), "paisagem") || strings.Contains(strings.ToLower(resp.Content), "pessoa") {
				res.DocumentType = "Fotografia"
				res.SuggestedCategory = "Fotos e Lembranças"
			}
		} else {
			res.VisualDescription = fmt.Sprintf("Imagem %s (%dx%d, %s, %s). Pronta para inspeção visual multimodal.", res.Format, res.Width, res.Height, res.AspectRatio, res.SizeDisplay)
		}
	} else {
		res.VisualDescription = fmt.Sprintf("Imagem %s (%dx%d, %s, %s). Metadados de geometria e formato extraídos com sucesso.", res.Format, res.Width, res.Height, res.AspectRatio, res.SizeDisplay)
	}

	return res, nil
}

// =========================================================================
// 2.2 TOOL: Compare Visual Similarity (Near-Duplicates with Qwen3-VL)
// =========================================================================

type CompareVisualParams struct {
	FilePathA string `json:"filepath_a"`
	FilePathB string `json:"filepath_b"`
}

type ImageBrief struct {
	FilePath    string `json:"filePath"`
	Dimensions  string `json:"dimensions"`
	SizeBytes   int64  `json:"sizeBytes"`
	SizeDisplay string `json:"sizeDisplay"`
	Format      string `json:"format"`
}

type VisualComparisonResult struct {
	ImageA            ImageBrief `json:"imageA"`
	ImageB            ImageBrief `json:"imageB"`
	IsVisualDuplicate bool       `json:"isVisualDuplicate"`
	Confidence        float64    `json:"confidence"`
	VisualDifferences string     `json:"visualDifferences"`
	RecommendedKeep   string     `json:"recommendedKeep"` // "IMAGE_A", "IMAGE_B", "BOTH"
	Rationale         string     `json:"rationale"`
}

func (tc *MCPToolsContext) CompareVisualSimilarity(ctx context.Context, params CompareVisualParams) (*VisualComparisonResult, error) {
	if params.FilePathA == "" || params.FilePathB == "" {
		return nil, fmt.Errorf("filepath_a e filepath_b são obrigatórios")
	}

	pathA, err := tc.ensurePathAllowed(params.FilePathA)
	if err != nil {
		return nil, err
	}
	pathB, err := tc.ensurePathAllowed(params.FilePathB)
	if err != nil {
		return nil, err
	}

	infoA, errA := os.Stat(pathA)
	infoB, errB := os.Stat(pathB)
	if errA != nil || errB != nil {
		return nil, fmt.Errorf("falha ao acessar arquivos para comparação visual: %v / %v", errA, errB)
	}

	fmtA, wA, hA, b64A, _ := decodeImageMetaAndBase64(pathA, 10*1024*1024)
	fmtB, wB, hB, b64B, _ := decodeImageMetaAndBase64(pathB, 10*1024*1024)

	res := &VisualComparisonResult{
		ImageA: ImageBrief{
			FilePath:    pathA,
			Dimensions:  fmt.Sprintf("%dx%d", wA, hA),
			SizeBytes:   infoA.Size(),
			SizeDisplay: formatBytes(infoA.Size()),
			Format:      strings.ToUpper(fmtA),
		},
		ImageB: ImageBrief{
			FilePath:    pathB,
			Dimensions:  fmt.Sprintf("%dx%d", wB, hB),
			SizeBytes:   infoB.Size(),
			SizeDisplay: formatBytes(infoB.Size()),
			Format:      strings.ToUpper(fmtB),
		},
		RecommendedKeep: "IMAGE_A",
	}

	if wA*hA < wB*hB {
		res.RecommendedKeep = "IMAGE_B"
	}

	// Query Qwen3-VL with both images
	if tc.OllamaClient != nil && b64A != "" && b64B != "" {
		model := tc.OllamaModel
		if model == "" {
			model = "qwen3-vl:8b"
		}

		prompt := fmt.Sprintf("Você é o especialista visual Qwen3-VL comparando duas imagens de disco:\n- Imagem 1: %s (%dx%d, %s)\n- Imagem 2: %s (%dx%d, %s)\n\nResponda em português:\n1. Elas são a mesma imagem/conteúdo (duplicata visual / cópia redimensionada)? Responda SIM ou NÃO.\n2. Quais as diferenças visuais identificadas (qualidade, resolução, cortes, compressão)?\n3. Qual deve ser mantida no disco (IMAGE_A ou IMAGE_B) e qual pode ser reciclada?", filepath.Base(pathA), wA, hA, formatBytes(infoA.Size()), filepath.Base(pathB), wB, hB, formatBytes(infoB.Size()))

		msg := ai.Message{
			Role:    "user",
			Content: prompt,
			Images:  []string{b64A, b64B},
		}

		resp, errChat := tc.OllamaClient.Chat(ctx, model, []ai.Message{msg}, nil)
		if errChat == nil && resp != nil {
			res.Rationale = resp.Content
			lower := strings.ToLower(resp.Content)
			if strings.Contains(lower, "sim") || strings.Contains(lower, "mesma imagem") || strings.Contains(lower, "duplicata") {
				res.IsVisualDuplicate = true
				res.Confidence = 0.95
			} else {
				res.IsVisualDuplicate = false
				res.Confidence = 0.85
			}
			res.VisualDifferences = resp.Content
		}
	} else {
		// Fallback comparison by dimension and size
		if wA == wB && hA == hB && infoA.Size() == infoB.Size() {
			res.IsVisualDuplicate = true
			res.Confidence = 0.90
			res.Rationale = "Mesma dimensão e tamanho exato de arquivo."
		} else {
			res.IsVisualDuplicate = false
			res.Confidence = 0.60
			res.Rationale = "Dimensões ou formatos distintos. Recomendada inspeção com Qwen3-VL ativo."
		}
	}

	return res, nil
}

func decodeImageMetaAndBase64(filePath string, maxBytes int64) (format string, width, height int, b64 string, err error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", 0, 0, "", err
	}
	defer f.Close()

	cfg, fmtName, errCfg := image.DecodeConfig(f)
	if errCfg == nil {
		format = fmtName
		width = cfg.Width
		height = cfg.Height
	}

	_, _ = f.Seek(0, 0)
	var r io.Reader = f
	if maxBytes > 0 {
		r = io.LimitReader(f, maxBytes)
	}

	data, errRead := io.ReadAll(r)
	if errRead != nil {
		return format, width, height, "", errRead
	}

	b64 = base64.StdEncoding.EncodeToString(data)
	return format, width, height, b64, nil
}

type WriteMetadataParams struct {
	Target   string   `json:"target"` // filepath or hash
	Category string   `json:"category"`
	Tags     []string `json:"tags,omitempty"`
	Notes    string   `json:"notes,omitempty"`
	Sidecar  bool     `json:"sidecar,omitempty"` // if true, writes file.scanfile_meta.json
}

func (tc *MCPToolsContext) WriteFileMetadata(params WriteMetadataParams) (FileMetadata, error) {
	if params.Target == "" {
		return FileMetadata{}, fmt.Errorf("target (caminho ou hash) é obrigatório")
	}

	// The sidecar writes to disk, so it is only allowed inside the Raízes Varridas.
	sidecarPath := ""
	if params.Sidecar {
		clean, err := tc.ensurePathAllowed(params.Target)
		if err != nil {
			return FileMetadata{}, err
		}
		if !fileExists(clean) {
			return FileMetadata{}, fmt.Errorf("alvo do sidecar não existe no disco: %s", clean)
		}
		sidecarPath = clean + ".scanfile_meta.json"
	}

	meta := FileMetadata{
		FilePath:  params.Target,
		Category:  params.Category,
		Tags:      params.Tags,
		Notes:     params.Notes,
		UpdatedAt: time.Now(),
	}

	tc.metadataMu.Lock()
	tc.metadata[params.Target] = meta
	tc.metadataMu.Unlock()

	if sidecarPath != "" {
		data, err := json.MarshalIndent(meta, "", "  ")
		if err != nil {
			return meta, fmt.Errorf("falha ao serializar metadados: %w", err)
		}
		if err := os.WriteFile(sidecarPath, data, 0o644); err != nil {
			return meta, fmt.Errorf("falha ao gravar sidecar %s: %w", sidecarPath, err)
		}
	}

	return meta, nil
}

// =========================================================================
// 4. TOOL: Propose Actions (Dry-Run & Execution)
// =========================================================================

type ProposeActionParams struct {
	ActionType  string   `json:"action_type"` // "RECYCLE", "MOVE", "TAG", "MARK_REVIEW"
	Files       []string `json:"files"`
	Description string   `json:"description,omitempty"`
	Destination string   `json:"destination,omitempty"`
	Category    string   `json:"category,omitempty"`
	DryRun      bool     `json:"dry_run"`
}

// ProposeActions registers a pending Proposta. It NEVER touches the disk: the
// dry_run argument sent by the model is ignored and the answer always reports
// dryRun: true. Execution only happens through ExecuteProposal, which the server
// calls after the user approves the Proposta in the interface.
func (tc *MCPToolsContext) ProposeActions(params ProposeActionParams) (*ActionProposal, error) {
	if len(params.Files) == 0 {
		return nil, fmt.Errorf("nenhum arquivo especificado para ação")
	}

	actionType := strings.ToUpper(strings.TrimSpace(params.ActionType))
	if !isSupportedActionType(actionType) {
		return nil, fmt.Errorf("tipo de ação não suportado: %q (use RECYCLE, MOVE, TAG ou MARK_REVIEW)", params.ActionType)
	}

	var totalBytes int64
	var validFiles []string

	for _, f := range params.Files {
		clean, err := tc.ensurePathAllowed(f)
		if err != nil {
			return nil, err
		}
		if info, statErr := os.Stat(clean); statErr == nil {
			totalBytes += info.Size()
			validFiles = append(validFiles, clean)
		}
	}

	if len(validFiles) == 0 {
		return nil, fmt.Errorf("nenhum dos arquivos fornecidos foi encontrado no disco")
	}

	destination := params.Destination
	if actionType == "MOVE" {
		if strings.TrimSpace(destination) == "" {
			return nil, fmt.Errorf("destino não especificado para operação MOVE")
		}
		cleanDest, err := tc.ensurePathAllowed(destination)
		if err != nil {
			return nil, err
		}
		destination = cleanDest
	}

	now := time.Now()
	proposalID := newProposalID(now)
	proposal := &ActionProposal{
		ID:          proposalID,
		ActionType:  actionType,
		Description: params.Description,
		Files:       validFiles,
		FileCount:   len(validFiles),
		TotalBytes:  totalBytes,
		TotalSize:   formatBytes(totalBytes),
		Destination: destination,
		Category:    params.Category,
		// A Proposta is always pending: the model can never request execution.
		DryRun:    true,
		Executed:  false,
		CreatedAt: now,
		ExpiresAt: now.Add(ProposalTTL),
	}

	if proposal.Description == "" {
		proposal.Description = fmt.Sprintf("Proposta de %s para %d arquivos (Total: %s). Pendente de aprovação do usuário.",
			proposal.ActionType, proposal.FileCount, proposal.TotalSize)
	}

	tc.proposalsMu.Lock()
	tc.purgeExpiredProposalsLocked(now)
	tc.proposals[proposalID] = proposal
	tc.proposalsMu.Unlock()

	return proposal, nil
}

// GetProposal returns a copy of a pending Proposta without executing it.
func (tc *MCPToolsContext) GetProposal(proposalID string) (*ActionProposal, bool) {
	tc.proposalsMu.RLock()
	defer tc.proposalsMu.RUnlock()

	p, ok := tc.proposals[proposalID]
	if !ok || time.Now().After(p.ExpiresAt) {
		return nil, false
	}

	snapshot := *p
	return &snapshot, true
}

// ExecuteProposal executes a Proposta already approved by the user. It is never
// reachable from a tool call issued by the model.
func (tc *MCPToolsContext) ExecuteProposal(proposalID string) (*ActionProposal, error) {
	// Serialise approvals: the disk work happens outside proposalsMu so that the
	// interface can keep reading proposals while a batch is running.
	tc.execMu.Lock()
	defer tc.execMu.Unlock()

	now := time.Now()

	tc.proposalsMu.Lock()
	tc.purgeExpiredProposalsLocked(now)
	stored, exists := tc.proposals[proposalID]
	var proposal ActionProposal
	if exists {
		proposal = *stored
	}
	tc.proposalsMu.Unlock()

	if !exists {
		return nil, fmt.Errorf("proposta %s não encontrada ou expirada (as propostas valem por %d minutos)", proposalID, int(ProposalTTL.Minutes()))
	}

	if proposal.Executed {
		return &proposal, nil
	}

	var errs []string
	var execErr error
	executed := false

	switch proposal.ActionType {
	case "RECYCLE":
		res := tc.recycleFunc()(proposal.Files)
		errs = append(errs, res.Errors...)
		if res.FailedCount > 0 {
			execErr = fmt.Errorf("falha ao enviar %d de %d arquivos para a Lixeira: %s",
				res.FailedCount, len(proposal.Files), strings.Join(res.Errors, "; "))
		} else {
			executed = true
		}

	case "MOVE":
		switch {
		case strings.TrimSpace(proposal.Destination) == "":
			execErr = fmt.Errorf("destino não especificado para operação MOVE")
		default:
			if err := os.MkdirAll(proposal.Destination, 0o755); err != nil {
				execErr = fmt.Errorf("falha ao criar pasta de destino %s: %w", proposal.Destination, err)
				break
			}
			for _, f := range proposal.Files {
				destFile := filepath.Join(proposal.Destination, filepath.Base(f))
				if _, err := os.Lstat(destFile); err == nil {
					errs = append(errs, fmt.Sprintf("%s: destino já existe (%s), nada foi sobrescrito", f, destFile))
					continue
				}
				if err := os.Rename(f, destFile); err != nil {
					errs = append(errs, fmt.Sprintf("%s: %v", f, err))
				}
			}
			if len(errs) > 0 {
				execErr = fmt.Errorf("mover arquivos falhou em %d item(ns): %s", len(errs), strings.Join(errs, "; "))
			} else {
				executed = true
			}
		}

	case "TAG":
		for _, f := range proposal.Files {
			if _, err := tc.WriteFileMetadata(WriteMetadataParams{
				Target:   f,
				Category: proposal.Category,
				Sidecar:  true,
			}); err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", f, err))
			}
		}
		if len(errs) > 0 {
			execErr = fmt.Errorf("marcar arquivos falhou em %d item(ns): %s", len(errs), strings.Join(errs, "; "))
		} else {
			executed = true
		}

	case "MARK_REVIEW":
		// Nothing to do on disk: the Proposta only flags files for human review.
		executed = true

	default:
		execErr = fmt.Errorf("tipo de ação não suportado: %s", proposal.ActionType)
	}

	tc.proposalsMu.Lock()
	if current, ok := tc.proposals[proposalID]; ok {
		current.Errors = errs
		current.Executed = executed
		proposal = *current
	} else {
		proposal.Errors = errs
		proposal.Executed = executed
	}
	tc.proposalsMu.Unlock()

	return &proposal, execErr
}

// proposalSeq disambiguates proposals created within the same clock tick.
var proposalSeq atomic.Uint64

// newProposalID builds a Proposta identifier that is unique inside the process.
// The wall clock alone is not enough: on Windows time.Now() has a resolution of
// milliseconds, so two proposals registered in quick succession would share an ID
// and one would silently replace the other in the pending map — the user could
// then approve a Proposta showing different files from the ones executed.
func newProposalID(now time.Time) string {
	return fmt.Sprintf("prop_%d_%d", now.UnixNano(), proposalSeq.Add(1))
}

// recycleFunc returns the injected RecycleFunc or the production default.
func (tc *MCPToolsContext) recycleFunc() RecycleFunc {
	if tc.RecycleFunc != nil {
		return tc.RecycleFunc
	}
	return defaultRecycleFunc
}

// defaultRecycleFunc measures the sizes right before sending the paths to the
// Windows Recycle Bin so that freedBytes is accurate.
func defaultRecycleFunc(paths []string) recycle.BatchDeleteResult {
	sizes := make(map[string]int64, len(paths))
	for _, p := range paths {
		if st, err := os.Stat(p); err == nil {
			sizes[p] = st.Size()
		}
	}
	return recycle.BatchDelete(paths, sizes, true)
}

// purgeExpiredProposalsLocked drops proposals older than ProposalTTL.
// The caller must hold proposalsMu for writing.
func (tc *MCPToolsContext) purgeExpiredProposalsLocked(now time.Time) {
	for id, p := range tc.proposals {
		if now.After(p.ExpiresAt) {
			delete(tc.proposals, id)
		}
	}
}

func isSupportedActionType(actionType string) bool {
	switch actionType {
	case "RECYCLE", "MOVE", "TAG", "MARK_REVIEW":
		return true
	default:
		return false
	}
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
