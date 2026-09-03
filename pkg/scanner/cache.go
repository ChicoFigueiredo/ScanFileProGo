package scanner

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// CurrentCacheVersion represents the active snapshot format version.
	CurrentCacheVersion = 2

	// DefaultAutoSaveFileName is the primary autosave filename.
	DefaultAutoSaveFileName = "autosave_latest.sfz"

	// BackupAutoSaveFileName is the rollback autosave filename.
	BackupAutoSaveFileName = "autosave_previous.sfz"
)

// CacheSnapshot representa o cabeçalho do snapshot em disco.
//
// A partir da escrita e leitura em streaming, a importação NÃO preenche mais
// Files nem Directories: 930 MB de JSON não voltam para a memória só para
// contar arquivos (achado H3). Use CacheSnapshotSummary nas APIs novas e o
// próprio TreeManager para percorrer os arquivos.
type CacheSnapshot struct {
	Version             int         `json:"version"`
	Timestamp           time.Time   `json:"timestamp"`
	Roots               []string    `json:"roots"`
	TotalFiles          int64       `json:"totalFiles"`
	TotalDirs           int64       `json:"totalDirs"`
	TotalBytes          int64       `json:"totalBytes"`
	TotalAllocatedBytes int64       `json:"totalAllocatedBytes,omitempty"`
	Files               []*FileNode `json:"files"`
	Directories         []string    `json:"directories,omitempty"` // Preserves empty folders
	ScanSettings        ScanConfig  `json:"scanSettings,omitempty"`
}

// CacheSnapshotSummary é o resumo leve devolvido pelas importações e pelas
// respostas de /api/cache/load e /api/cache/autosave/restore. Nunca carrega a
// lista de arquivos.
type CacheSnapshotSummary struct {
	Version             int        `json:"version"`
	Roots               []string   `json:"roots"`
	TotalFiles          int64      `json:"totalFiles"`
	TotalDirs           int64      `json:"totalDirs"`
	TotalBytes          int64      `json:"totalBytes"`
	TotalAllocatedBytes int64      `json:"totalAllocatedBytes,omitempty"`
	Timestamp           time.Time  `json:"timestamp"`
	HashAlgorithm       string     `json:"hashAlgorithm"`
	ScanSettings        ScanConfig `json:"scanSettings,omitempty"`
}

// ToSnapshot converte o resumo no cabeçalho histórico, sem lista de arquivos.
func (s *CacheSnapshotSummary) ToSnapshot() *CacheSnapshot {
	if s == nil {
		return nil
	}
	return &CacheSnapshot{
		Version:             s.Version,
		Timestamp:           s.Timestamp,
		Roots:               s.Roots,
		TotalFiles:          s.TotalFiles,
		TotalDirs:           s.TotalDirs,
		TotalBytes:          s.TotalBytes,
		TotalAllocatedBytes: s.TotalAllocatedBytes,
		ScanSettings:        s.ScanSettings,
	}
}

// CacheFileInfo represents metadata for a saved cache file on disk.
type CacheFileInfo struct {
	FileName   string    `json:"fileName"`
	FilePath   string    `json:"filePath"`
	SizeBytes  int64     `json:"sizeBytes"`
	ModTime    time.Time `json:"modTime"`
	IsAutoSave bool      `json:"isAutoSave,omitempty"`
}

// ExportCache serializa a árvore num JSON gzip escrito em streaming: as chaves
// saem uma a uma e os arrays `files`/`directories` elemento a elemento, sem
// nunca montar o documento inteiro na memória (achado H3).
//
// As chaves e a ordem são as mesmas do formato v2 original, para que snapshots
// novos continuem legíveis por leitores antigos.
func ExportCache(tm *TreeManager, roots []string, config ScanConfig, w io.Writer) error {
	if tm == nil {
		return fmt.Errorf("não há árvore em memória para exportar")
	}

	gzWriter, err := gzip.NewWriterLevel(w, gzip.DefaultCompression)
	if err != nil {
		gzWriter = gzip.NewWriter(w)
	}

	bw := bufio.NewWriterSize(gzWriter, 256*1024)

	if err := writeSnapshotStream(tm, roots, config, bw); err != nil {
		_ = gzWriter.Close()
		return err
	}
	if err := bw.Flush(); err != nil {
		_ = gzWriter.Close()
		return err
	}
	return gzWriter.Close()
}

// snapshotTotals percorre a árvore uma vez só para contar. É bem mais barato
// que materializar uma fatia com todos os ponteiros de arquivo.
func snapshotTotals(tm *TreeManager) (totalFiles, totalDirs, totalBytes, totalAllocated int64) {
	if tm == nil {
		return 0, 0, 0, 0
	}
	for _, r := range tm.GetRootsSnapshot() {
		walkDirs(r, func(node *DirNode, files []*FileNode) bool {
			totalDirs++
			for _, f := range files {
				totalFiles++
				totalBytes += f.Size
				if f.AllocatedSize > 0 {
					totalAllocated += f.AllocatedSize
				} else {
					totalAllocated += f.Size
				}
			}
			return true
		})
	}
	return
}

// walkDirs percorre a árvore em profundidade entregando, para cada pasta, uma
// cópia curta da lista de arquivos tirada sob lock. Devolver false interrompe.
func walkDirs(node *DirNode, fn func(node *DirNode, files []*FileNode) bool) bool {
	if node == nil {
		return true
	}
	node.mu.RLock()
	files := make([]*FileNode, len(node.Files))
	copy(files, node.Files)
	children := make([]*DirNode, 0, len(node.Children))
	for _, child := range node.Children {
		children = append(children, child)
	}
	node.mu.RUnlock()

	if !fn(node, files) {
		return false
	}
	for _, child := range children {
		if !walkDirs(child, fn) {
			return false
		}
	}
	return true
}

func writeSnapshotStream(tm *TreeManager, roots []string, config ScanConfig, w *bufio.Writer) error {
	totalFiles, totalDirs, totalBytes, totalAllocated := snapshotTotals(tm)

	writeRaw := func(s string) error {
		_, err := w.WriteString(s)
		return err
	}
	// Os valores escalares do cabeçalho são curtos: json.Marshal neles não
	// pesa. Só `files` e `directories` precisam sair elemento a elemento.
	writeKeyValue := func(key string, v any) error {
		if err := writeRaw(`"` + key + `":`); err != nil {
			return err
		}
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
		return writeRaw(",")
	}

	if err := writeRaw("{"); err != nil {
		return err
	}
	if err := writeKeyValue("version", CurrentCacheVersion); err != nil {
		return err
	}
	if err := writeKeyValue("timestamp", time.Now()); err != nil {
		return err
	}
	if err := writeKeyValue("roots", roots); err != nil {
		return err
	}
	if err := writeKeyValue("totalFiles", totalFiles); err != nil {
		return err
	}
	if err := writeKeyValue("totalDirs", totalDirs); err != nil {
		return err
	}
	if err := writeKeyValue("totalBytes", totalBytes); err != nil {
		return err
	}
	if err := writeKeyValue("totalAllocatedBytes", totalAllocated); err != nil {
		return err
	}

	// files: um elemento por vez
	if err := writeRaw(`"files":[`); err != nil {
		return err
	}
	var writeErr error
	first := true
	for _, r := range tm.GetRootsSnapshot() {
		walkDirs(r, func(node *DirNode, files []*FileNode) bool {
			// O caminho da pasta é derivado uma vez só; cada arquivo apenas o
			// completa com o próprio nome (ADR-0001).
			dirPath := node.Path()
			for _, f := range files {
				data, err := json.Marshal(f.jsonView(dirPath))
				if err != nil {
					writeErr = err
					return false
				}
				if !first {
					if _, err := w.WriteString(","); err != nil {
						writeErr = err
						return false
					}
				}
				first = false
				if _, err := w.Write(data); err != nil {
					writeErr = err
					return false
				}
			}
			return true
		})
		if writeErr != nil {
			return writeErr
		}
	}
	if err := writeRaw(`],`); err != nil {
		return err
	}

	// directories: um caminho por vez
	if err := writeRaw(`"directories":[`); err != nil {
		return err
	}
	first = true
	for _, r := range tm.GetRootsSnapshot() {
		walkDirs(r, func(node *DirNode, _ []*FileNode) bool {
			data, err := json.Marshal(node.Path())
			if err != nil {
				writeErr = err
				return false
			}
			if !first {
				if _, err := w.WriteString(","); err != nil {
					writeErr = err
					return false
				}
			}
			first = false
			if _, err := w.Write(data); err != nil {
				writeErr = err
				return false
			}
			return true
		})
		if writeErr != nil {
			return writeErr
		}
	}
	if err := writeRaw(`],`); err != nil {
		return err
	}

	settings, err := json.Marshal(config)
	if err != nil {
		return err
	}
	if err := writeRaw(`"scanSettings":`); err != nil {
		return err
	}
	if _, err := w.Write(settings); err != nil {
		return err
	}
	return writeRaw("}\n")
}

// SaveCacheToFile saves the in-memory tree state to a compressed file on disk.
func SaveCacheToFile(tm *TreeManager, roots []string, config ScanConfig, targetPath string) error {
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("falha ao criar diretório de cache: %w", err)
	}

	file, err := os.Create(targetPath)
	if err != nil {
		return fmt.Errorf("falha ao criar arquivo de cache: %w", err)
	}
	defer file.Close()

	if err := ExportCache(tm, roots, config, file); err != nil {
		return fmt.Errorf("falha ao exportar cache: %w", err)
	}

	return nil
}

// SaveAutoSave atomically writes an autosave snapshot and rotates the previous autosave backup.
func SaveAutoSave(tm *TreeManager, roots []string, config ScanConfig, targetDir string) (string, error) {
	if targetDir == "" {
		targetDir = "saved_scans"
	}
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return "", fmt.Errorf("falha ao criar pasta de autosave: %w", err)
	}

	tempPath := filepath.Join(targetDir, "autosave_temp.sfz")
	latestPath := filepath.Join(targetDir, DefaultAutoSaveFileName)
	backupPath := filepath.Join(targetDir, BackupAutoSaveFileName)

	// Step 1: Write to temporary file
	file, err := os.Create(tempPath)
	if err != nil {
		return "", fmt.Errorf("falha ao criar arquivo temporário de autosave: %w", err)
	}

	if err := ExportCache(tm, roots, config, file); err != nil {
		file.Close()
		_ = os.Remove(tempPath)
		return "", fmt.Errorf("falha ao serializar autosave: %w", err)
	}
	file.Close()

	// Step 2: Rotate latest to backup if latest exists
	if _, err := os.Stat(latestPath); err == nil {
		_ = os.Remove(backupPath) // Remove old backup if any
		_ = os.Rename(latestPath, backupPath)
	}

	// Step 3: Rename temp to latest (Atomic swap)
	if err := os.Rename(tempPath, latestPath); err != nil {
		// Fallback: If direct rename fails, copy and remove
		data, rErr := os.ReadFile(tempPath)
		if rErr == nil {
			_ = os.WriteFile(latestPath, data, 0644)
			_ = os.Remove(tempPath)
		} else {
			return "", fmt.Errorf("falha ao finalizar autosave atômico: %w", err)
		}
	}

	return latestPath, nil
}

// CacheProgressFunc callback reporting stage name, estimated percentage and detailed message.
type CacheProgressFunc func(stage string, percent float64, details string)

// ImportCache reads and reconstructs a TreeManager from a gzip-compressed (.sfz, .scanfile.gz) or plain JSON reader.
//
// Compatibilidade: continua devolvendo *CacheSnapshot, mas sem a lista de
// arquivos. Prefira ImportCacheStream nas chamadas novas.
func ImportCache(r io.Reader) (*TreeManager, *CacheSnapshot, error) {
	return ImportCacheWithProgress(r, nil)
}

// ImportCacheWithProgress reads and reconstructs a TreeManager while reporting stage progress.
func ImportCacheWithProgress(r io.Reader, onProgress CacheProgressFunc) (*TreeManager, *CacheSnapshot, error) {
	tm, summary, err := ImportCacheStream(r, onProgress)
	if err != nil {
		return nil, nil, err
	}
	return tm, summary.ToSnapshot(), nil
}

// ImportCacheStream lê o snapshot em streaming com json.Decoder.Token(),
// inserindo cada arquivo na árvore conforme lê. Tolera qualquer ordem de
// chaves e ignora chaves desconhecidas.
func ImportCacheStream(r io.Reader, onProgress CacheProgressFunc) (*TreeManager, *CacheSnapshotSummary, error) {
	if onProgress != nil {
		onProgress("Descompactando snapshot Gzip...", 10, "Lendo stream compactado do disco")
	}

	// Espia os dois primeiros bytes para decidir entre gzip e JSON puro sem
	// perder o começo do fluxo.
	br := bufio.NewReaderSize(r, 256*1024)
	var reader io.Reader = br
	if magic, err := br.Peek(2); err == nil && magic[0] == 0x1f && magic[1] == 0x8b {
		gzReader, err := gzip.NewReader(br)
		if err != nil {
			return nil, nil, fmt.Errorf("snapshot gzip inválido: %w", err)
		}
		defer gzReader.Close()
		reader = gzReader
	}

	tm := NewTreeManager()
	summary := &CacheSnapshotSummary{Version: CurrentCacheVersion}

	dec := json.NewDecoder(bufio.NewReaderSize(reader, 256*1024))
	dec.UseNumber()

	tok, err := dec.Token()
	if err != nil {
		return nil, nil, fmt.Errorf("formato de arquivo de cache inválido ou corrompido: %w", err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, nil, fmt.Errorf("formato de arquivo de cache inválido: esperava um objeto JSON no topo")
	}

	var pendingRoots []string
	inserter := newTreeInserter(tm)

	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, nil, fmt.Errorf("snapshot truncado: %w", err)
		}
		key, _ := keyTok.(string)

		switch key {
		case "version":
			var v int
			if err := dec.Decode(&v); err != nil {
				return nil, nil, err
			}
			if v > 0 {
				summary.Version = v
			}
		case "timestamp":
			var v time.Time
			if err := dec.Decode(&v); err != nil {
				// Timestamp ilegível não invalida o snapshot.
				continue
			}
			summary.Timestamp = v
		case "roots":
			if err := dec.Decode(&pendingRoots); err != nil {
				return nil, nil, err
			}
			summary.Roots = pendingRoots
			for _, root := range pendingRoots {
				tm.GetOrCreateRoot(root)
			}
		case "totalFiles":
			if err := dec.Decode(&summary.TotalFiles); err != nil {
				return nil, nil, err
			}
		case "totalDirs":
			if err := dec.Decode(&summary.TotalDirs); err != nil {
				return nil, nil, err
			}
		case "totalBytes":
			if err := dec.Decode(&summary.TotalBytes); err != nil {
				return nil, nil, err
			}
		case "totalAllocatedBytes":
			if err := dec.Decode(&summary.TotalAllocatedBytes); err != nil {
				return nil, nil, err
			}
		case "scanSettings":
			if err := dec.Decode(&summary.ScanSettings); err != nil {
				return nil, nil, err
			}
			summary.HashAlgorithm = NormalizeHashAlgorithm(summary.ScanSettings.HashAlgorithm)
		case "directories":
			if err := streamStringArray(dec, func(dirPath string) {
				if dirPath != "" {
					tm.EnsureDirNode(dirPath)
				}
			}); err != nil {
				return nil, nil, err
			}
			if onProgress != nil {
				onProgress("Reconstruindo estrutura de diretórios...", 45, "Pastas recriadas em memória")
			}
		case "files":
			count := 0
			if err := streamFileArray(dec, func(dirPath string, f *FileNode) {
				inserter.add(dirPath, f)
				count++
				if onProgress != nil && count%250_000 == 0 {
					onProgress("Mapeando arquivos em memória...", 65, fmt.Sprintf("%d arquivos carregados", count))
				}
			}); err != nil {
				return nil, nil, err
			}
			if onProgress != nil {
				onProgress("Mapeando arquivos em memória...", 70, fmt.Sprintf("%d arquivos carregados", count))
			}
		default:
			// Chave desconhecida: consome o valor inteiro e segue.
			var discard json.RawMessage
			if err := dec.Decode(&discard); err != nil {
				return nil, nil, err
			}
		}
	}

	inserter.flush()

	if summary.HashAlgorithm == "" {
		summary.HashAlgorithm = DefaultHashAlgorithm
	}

	if onProgress != nil {
		onProgress("Calculando métricas agregadas da árvore...", 88, "Computando tamanhos e contadores recursivos")
	}
	tm.ComputeAggregatedSizes()

	if onProgress != nil {
		onProgress("Snapshot carregado.", 100, fmt.Sprintf("%d arquivos e %d pastas", summary.TotalFiles, summary.TotalDirs))
	}

	return tm, summary, nil
}

// streamStringArray consome um array de strings elemento a elemento.
func streamStringArray(dec *json.Decoder, fn func(string)) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if tok == nil {
		return nil // null
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '[' {
		return fmt.Errorf("esperava um array de strings")
	}
	for dec.More() {
		var s string
		if err := dec.Decode(&s); err != nil {
			return err
		}
		fn(s)
	}
	_, err = dec.Token() // ']'
	return err
}

// streamFileArray consome um array de FileNode elemento a elemento.
//
// A decodificação passa pela visão plana (fileNodeJSON) em vez de FileNode:
// assim o caminho lido do Snapshot vira a pasta do nó direto, sem construir uma
// pasta sintética por arquivo.
func streamFileArray(dec *json.Decoder, fn func(dirPath string, f *FileNode)) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if tok == nil {
		return nil // null
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '[' {
		return fmt.Errorf("esperava um array de arquivos")
	}
	var raw fileNodeJSON
	for dec.More() {
		raw = fileNodeJSON{}
		if err := dec.Decode(&raw); err != nil {
			return err
		}
		if raw.Path == "" {
			continue
		}
		// Retrocompatibilidade: calcula AllocatedSize quando ausente.
		if raw.AllocatedSize == 0 && raw.Size > 0 && !raw.IsCompressed {
			raw.AllocatedSize = raw.Size
		}
		dir, base := filepath.Split(filepath.Clean(raw.Path))
		if raw.Name == "" {
			raw.Name = base
		}
		f := NewFileNode(FileMeta{
			Name:              raw.Name,
			Size:              raw.Size,
			AllocatedSize:     raw.AllocatedSize,
			ModTime:           raw.ModTime,
			CreateTime:        raw.CreateTime,
			AccessTime:        raw.AccessTime,
			Hash:              raw.Hash,
			QuickHash:         raw.QuickHash,
			Extension:         raw.Extension,
			IsSymlink:         raw.IsSymlink,
			LinkTarget:        raw.LinkTarget,
			IsCompressed:      raw.IsCompressed,
			IsReusedFromCache: raw.IsReusedFromCache,
		})
		fn(filepath.Clean(dir), f)
	}
	_, err = dec.Token() // ']'
	return err
}

// treeInserter agrupa arquivos por pasta enquanto o fluxo é lido, para não
// pagar EnsureDirNode nem bubbleUpSize por arquivo. O snapshot sai da árvore
// em ordem de pasta, então o lote quase sempre tem só uma pasta aberta.
type treeInserter struct {
	tm       *TreeManager
	lastDir  string
	lastNode *DirNode
	batch    []*FileNode
}

func newTreeInserter(tm *TreeManager) *treeInserter {
	return &treeInserter{tm: tm, batch: make([]*FileNode, 0, 1024)}
}

func (ti *treeInserter) add(dir string, f *FileNode) {
	if dir != ti.lastDir || ti.lastNode == nil {
		ti.flush()
		ti.lastDir = dir
		ti.lastNode = ti.tm.EnsureDirNode(dir)
	}
	f.parent = ti.lastNode
	ti.batch = append(ti.batch, f)
}

func (ti *treeInserter) flush() {
	if ti.lastNode == nil || len(ti.batch) == 0 {
		ti.batch = ti.batch[:0]
		return
	}
	ti.lastNode.mu.Lock()
	ti.lastNode.Files = append(ti.lastNode.Files, ti.batch...)
	ti.lastNode.mu.Unlock()
	ti.batch = make([]*FileNode, 0, 1024)
}

// LoadCacheFromFile loads a saved cache snapshot from a file path.
func LoadCacheFromFile(filePath string) (*TreeManager, *CacheSnapshot, error) {
	return LoadCacheFromFileWithProgress(filePath, nil)
}

// LoadCacheFromFileWithProgress loads a saved cache snapshot while reporting progress.
func LoadCacheFromFileWithProgress(filePath string, onProgress CacheProgressFunc) (*TreeManager, *CacheSnapshot, error) {
	tm, summary, err := LoadCacheSummaryFromFile(filePath, onProgress)
	if err != nil {
		return nil, nil, err
	}
	return tm, summary.ToSnapshot(), nil
}

// LoadCacheSummaryFromFile carrega um snapshot devolvendo o resumo leve.
func LoadCacheSummaryFromFile(filePath string, onProgress CacheProgressFunc) (*TreeManager, *CacheSnapshotSummary, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("falha ao abrir arquivo de cache: %w", err)
	}
	defer file.Close()

	return ImportCacheStream(file, onProgress)
}

// BuildQuickScanLookup builds a fast index map from a CacheSnapshot to allow O(1) hash and metadata reuse during Quick Scan.
//
// Só funciona com snapshots que ainda carregam Files. Depois da leitura em
// streaming, use BuildQuickScanLookupFromTree ou LoadQuickScanLookupFromFile.
func BuildQuickScanLookup(snapshot *CacheSnapshot) map[string]*FileNode {
	if snapshot == nil || len(snapshot.Files) == 0 {
		return make(map[string]*FileNode)
	}

	lookup := make(map[string]*FileNode, len(snapshot.Files))
	for _, f := range snapshot.Files {
		if f == nil || f.Name() == "" {
			continue
		}
		normPath := strings.ToLower(filepath.Clean(f.Path()))
		lookup[normPath] = f
	}
	return lookup
}

// BuildQuickScanLookupFromTree monta o índice de reaproveitamento do Quick Scan
// direto da árvore em memória, sem passar por um CacheSnapshot.
func BuildQuickScanLookupFromTree(tm *TreeManager) map[string]*FileNode {
	if tm == nil {
		return make(map[string]*FileNode)
	}
	lookup := make(map[string]*FileNode)
	tm.IterateFiles(func(f *FileNode) bool {
		if f == nil || f.Name() == "" {
			return true
		}
		lookup[strings.ToLower(filepath.Clean(f.Path()))] = f
		return true
	})
	return lookup
}

// LoadQuickScanLookupFromFile monta o índice do Quick Scan lendo um snapshot em
// streaming, sem reconstruir a árvore inteira.
func LoadQuickScanLookupFromFile(filePath string) (map[string]*FileNode, *CacheSnapshotSummary, error) {
	tm, summary, err := LoadCacheSummaryFromFile(filePath, nil)
	if err != nil {
		return nil, nil, err
	}
	return BuildQuickScanLookupFromTree(tm), summary, nil
}

// GetLatestAutoSave discovers if an active autosave is available in targetDir.
func GetLatestAutoSave(dirPath string) (*CacheFileInfo, error) {
	if dirPath == "" {
		dirPath = "saved_scans"
	}
	latestPath := filepath.Join(dirPath, DefaultAutoSaveFileName)
	info, err := os.Stat(latestPath)
	if err == nil && info.Size() > 0 {
		return &CacheFileInfo{
			FileName:   DefaultAutoSaveFileName,
			FilePath:   latestPath,
			SizeBytes:  info.Size(),
			ModTime:    info.ModTime(),
			IsAutoSave: true,
		}, nil
	}

	// Fallback to previous backup if latest is missing
	backupPath := filepath.Join(dirPath, BackupAutoSaveFileName)
	bInfo, bErr := os.Stat(backupPath)
	if bErr == nil && bInfo.Size() > 0 {
		return &CacheFileInfo{
			FileName:   BackupAutoSaveFileName,
			FilePath:   backupPath,
			SizeBytes:  bInfo.Size(),
			ModTime:    bInfo.ModTime(),
			IsAutoSave: true,
		}, nil
	}

	return nil, os.ErrNotExist
}

// ListSavedCaches discovers all saved cache and autosave files in the given directory.
func ListSavedCaches(dirPath string) ([]CacheFileInfo, error) {
	if dirPath == "" {
		dirPath = "saved_scans"
	}
	if _, err := os.Stat(dirPath); os.IsNotExist(err) {
		return []CacheFileInfo{}, nil
	}

	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, err
	}

	var list []CacheFileInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".sfz") || strings.HasSuffix(name, ".scanfile.gz") || strings.HasSuffix(name, ".scanfile") || strings.HasSuffix(name, ".json.gz") || strings.HasSuffix(name, ".json") {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			fullPath := filepath.Join(dirPath, name)
			list = append(list, CacheFileInfo{
				FileName:   name,
				FilePath:   fullPath,
				SizeBytes:  info.Size(),
				ModTime:    info.ModTime(),
				IsAutoSave: strings.HasPrefix(name, "autosave_"),
			})
		}
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].ModTime.After(list[j].ModTime)
	})

	return list, nil
}
