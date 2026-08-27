package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ledongthuc/pdf"
	_ "modernc.org/sqlite"

	"scanfile/pkg/indexer"
	"scanfile/pkg/recycle"
	"scanfile/pkg/scanner"
)

// MCPToolsContext holds references to ScanFile core engines.
type MCPToolsContext struct {
	Tree        *scanner.TreeManager
	Index       *indexer.DuplicateIndex
	FolderIndex *indexer.FolderDuplicateIndex
	
	// Active proposals cache for two-phase approval
	proposalsMu sync.RWMutex
	proposals   map[string]*ActionProposal
	
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

// ActionProposal represents a proposed disk action with dry-run capabilities.
type ActionProposal struct {
	ID          string    `json:"id"`
	ActionType  string    `json:"actionType"` // "RECYCLE", "MOVE", "TAG", "ARCHIVE"
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
}

// NewMCPToolsContext initializes the tools context.
func NewMCPToolsContext(tree *scanner.TreeManager, idx *indexer.DuplicateIndex, fIdx *indexer.FolderDuplicateIndex) *MCPToolsContext {
	return &MCPToolsContext{
		Tree:        tree,
		Index:       idx,
		FolderIndex: fIdx,
		proposals:   make(map[string]*ActionProposal),
		metadata:    make(map[string]FileMetadata),
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

		tc.Tree.RootsLock(func(roots map[string]*scanner.DirNode) {
			for _, root := range roots {
				walkFn(root)
				if len(results) >= params.Limit {
					break
				}
			}
		})
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
	Tables    []string          `json:"tables"`
	TotalRows int               `json:"totalRows"`
	Schema    map[string]string `json:"schema"`
	QueryRows []map[string]interface{} `json:"queryRows,omitempty"`
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

	cleanPath := filepath.Clean(params.FilePath)
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

	// 1. PDF Deep Extraction
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

	// 2. SQLite Database Inspection
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
	db, err := sql.Open("sqlite", dbPath+"?mode=ro")
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

	// If a safe SELECT query was provided
	if userQuery != "" && strings.HasPrefix(strings.ToUpper(strings.TrimSpace(userQuery)), "SELECT") {
		qRows, errQ := db.QueryContext(ctx, userQuery)
		if errQ == nil {
			defer qRows.Close()
			cols, _ := qRows.Columns()
			for qRows.Next() && len(res.QueryRows) < 20 {
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
		}
	}

	return res, nil
}

// =========================================================================
// 3. TOOL: Write File Metadata & Tags
// =========================================================================

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

	// Write sidecar JSON file next to target if requested and target is a file
	if params.Sidecar && fileExists(params.Target) {
		sidecarPath := params.Target + ".scanfile_meta.json"
		if data, err := json.MarshalIndent(meta, "", "  "); err == nil {
			_ = os.WriteFile(sidecarPath, data, 0644)
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

func (tc *MCPToolsContext) ProposeActions(params ProposeActionParams) (*ActionProposal, error) {
	if len(params.Files) == 0 {
		return nil, fmt.Errorf("nenhum arquivo especificado para ação")
	}

	var totalBytes int64
	var validFiles []string

	for _, f := range params.Files {
		clean := filepath.Clean(f)
		if info, err := os.Stat(clean); err == nil {
			totalBytes += info.Size()
			validFiles = append(validFiles, clean)
		}
	}

	if len(validFiles) == 0 {
		return nil, fmt.Errorf("nenhum dos arquivos fornecidos foi encontrado no disco")
	}

	proposalID := fmt.Sprintf("prop_%d", time.Now().UnixNano())
	proposal := &ActionProposal{
		ID:          proposalID,
		ActionType:  strings.ToUpper(params.ActionType),
		Description: params.Description,
		Files:       validFiles,
		FileCount:   len(validFiles),
		TotalBytes:  totalBytes,
		TotalSize:   formatBytes(totalBytes),
		Destination: params.Destination,
		Category:    params.Category,
		DryRun:      params.DryRun,
		Executed:    false,
		CreatedAt:   time.Now(),
	}

	if proposal.Description == "" {
		proposal.Description = fmt.Sprintf("Proposta de %s para %d arquivos (Total: %s)",
			proposal.ActionType, proposal.FileCount, proposal.TotalSize)
	}

	tc.proposalsMu.Lock()
	tc.proposals[proposalID] = proposal
	tc.proposalsMu.Unlock()

	if !params.DryRun {
		_, err := tc.ExecuteProposal(proposalID)
		if err != nil {
			return nil, err
		}
	}

	return proposal, nil
}

// ExecuteProposal executes an approved proposal.
func (tc *MCPToolsContext) ExecuteProposal(proposalID string) (*ActionProposal, error) {
	tc.proposalsMu.Lock()
	proposal, exists := tc.proposals[proposalID]
	tc.proposalsMu.Unlock()

	if !exists {
		return nil, fmt.Errorf("proposta %s não encontrada ou expirada", proposalID)
	}

	if proposal.Executed {
		return proposal, nil
	}

	switch proposal.ActionType {
	case "RECYCLE":
		fileSizes := make(map[string]int64)
		for _, f := range proposal.Files {
			if st, err := os.Stat(f); err == nil {
				fileSizes[f] = st.Size()
			}
		}
		res := recycle.BatchDelete(proposal.Files, fileSizes, true)
		if res.SuccessCount == 0 && res.FailedCount > 0 {
			return nil, fmt.Errorf("falha ao enviar arquivos para a lixeira: %s", strings.Join(res.Errors, ", "))
		}
		proposal.Executed = true

	case "MOVE":
		if proposal.Destination == "" {
			return nil, fmt.Errorf("destino não especificado para operação MOVE")
		}
		_ = os.MkdirAll(proposal.Destination, 0755)
		for _, f := range proposal.Files {
			destFile := filepath.Join(proposal.Destination, filepath.Base(f))
			_ = os.Rename(f, destFile)
		}
		proposal.Executed = true

	case "TAG":
		for _, f := range proposal.Files {
			_, _ = tc.WriteFileMetadata(WriteMetadataParams{
				Target:   f,
				Category: proposal.Category,
				Sidecar:  true,
			})
		}
		proposal.Executed = true
	}

	return proposal, nil
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
