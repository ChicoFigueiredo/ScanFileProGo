package indexer

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/cespare/xxhash/v2"
	"scanfile/pkg/scanner"
)

// Confidence levels reported by the indexes. Only ConfidenceHash means the
// comparison was made over the full content hash of every file involved.
const (
	// ConfidenceHash means every file compared had a full content hash.
	ConfidenceHash = "hash"
	// ConfidenceSizeMTime means at least one file fell back to size + modification time.
	ConfidenceSizeMTime = "size_mtime"
)

// DuplicateGroup represents a set of identical files sharing the same content hash and size.
type DuplicateGroup struct {
	ID          string              `json:"id"` // Composite key: hash:size
	Hash        string              `json:"hash"`
	FileSize    int64               `json:"fileSize"`
	FileCount   int                 `json:"fileCount"`
	WastedBytes int64               `json:"wastedBytes"` // (FileCount - 1) * FileSize
	Confidence  string              `json:"confidence"`  // Always ConfidenceHash: unhashed files never enter this index
	Files       []*scanner.FileNode `json:"files"`
}

// clone returns a detached copy so callers can serialize a group while
// Monitoramento keeps mutating the live index.
func (grp *DuplicateGroup) clone() *DuplicateGroup {
	if grp == nil {
		return nil
	}
	cp := *grp
	cp.Files = make([]*scanner.FileNode, len(grp.Files))
	copy(cp.Files, grp.Files)
	return &cp
}

// contentKey identifies a set of identical files without keeping one string per
// file: the Hash Completo is folded into 64 bits and paired with the Tamanho
// Lógico, which isolates same-hash files of different sizes.
//
// A dobra tem colisão possível, e por isso ela nunca decide sozinha: antes de
// juntar um arquivo a um grupo ou a um candidato solitário, o Hash Completo em
// texto é comparado. Uma colisão (mesmos 64 bits dobrados e mesmo tamanho, algo
// em torno de 1 em 100 mil para 50 milhões de arquivos) deixa um arquivo fora do
// índice até a próxima reconstrução; ela nunca produz um falso duplicado.
type contentKey struct {
	fingerprint uint64
	size        int64
}

// contentKeyOf folds a Hash Completo and a size into the map key.
func contentKeyOf(hash string, size int64) contentKey {
	return contentKey{fingerprint: xxhash.Sum64String(hash), size: size}
}

// trackedFile is what a folder record keeps per indexed file: o ponteiro do nó,
// que é a identidade dele, mais a dobra do hash sob o qual ele foi arquivado.
// São 16 bytes, sem caminho e sem string: o nome sai do próprio nó (ADR-0001).
//
// A dobra é guardada em vez de recalculada porque o hash de um nó pode ser
// reescrito no lugar depois que ele entrou no índice — a Varredura Rápida
// descarta hash de outro algoritmo assim —, e aí o arquivo removido não seria
// achado no grupo dele e viraria um fantasma. O Tamanho Lógico não tem esse
// problema: nada no projeto reescreve Size num nó vivo, o Monitoramento sempre
// monta um nó novo.
type trackedFile struct {
	node        *scanner.FileNode
	fingerprint uint64
}

// key rebuilds the content key the file was filed under.
func (tracked trackedFile) key() contentKey {
	return contentKey{fingerprint: tracked.fingerprint, size: tracked.node.Size}
}

// DuplicateIndex stores and queries grouped duplicate files.
//
// groups only ever holds sets of two or more files. A file whose content key is
// still unique is parked in singles, so a later UpsertFile promotes the pair in
// O(1) instead of forcing a full rebuild (achado M3).
//
// Apagar um arquivo do índice também é O(1), e sem guardar caminho nenhum
// (ADR-0001): dirFiles guarda uma entrada por *pasta* — o caminho normalizado
// dela — apontando para os arquivos indexados que moram ali. O ponteiro do
// FileNode é a identidade, e o nome do arquivo vem do próprio nó, então o custo
// por arquivo são 16 bytes na fatia da pasta, não um caminho absoluto minúsculo
// recém-montado. A busca dentro da pasta é limitada aos arquivos hasheados
// dela, nunca ao índice inteiro (achado H4).
type DuplicateIndex struct {
	mu       sync.RWMutex
	groups   map[contentKey]*DuplicateGroup
	singles  map[contentKey]*scanner.FileNode
	dirFiles map[string][]trackedFile // normalized directory path -> indexed files

	// lastParent e lastDirPath lembram a última pasta cujo caminho foi derivado.
	// Uma reconstrução caminha a árvore em ordem, então os arquivos de uma mesma
	// pasta chegam juntos e o caminho é montado uma vez por pasta, não por
	// arquivo. Protegidos pelo lock de escrita, como o resto do índice. O cache
	// guarda só o caminho: uma pasta apagada de dirFiles volta sozinha no
	// próximo arquivo indexado ali.
	lastParent  *scanner.DirNode
	lastDirPath string
}

// NewDuplicateIndex creates a new index instance.
func NewDuplicateIndex() *DuplicateIndex {
	return &DuplicateIndex{
		groups:   make(map[contentKey]*DuplicateGroup),
		singles:  make(map[contentKey]*scanner.FileNode),
		dirFiles: make(map[string][]trackedFile),
	}
}

// normalizePath folds a Windows path so lookups ignore case and separator noise.
func normalizePath(path string) string {
	return strings.ToLower(filepath.Clean(path))
}

// groupKey is the composite identifier exposed to the interface as DuplicateGroup.ID.
// It is built once per group, never once per file.
func groupKey(hash string, size int64) string {
	return fmt.Sprintf("%s|%d", hash, size)
}

// dirPathOf returns the normalized path of the folder holding f, reusing the
// last derived one whenever the parent repeats. The caller holds the write lock.
func (idx *DuplicateIndex) dirPathOf(f *scanner.FileNode) string {
	parent := f.Parent()
	if parent == nil {
		return normalizePath(filepath.Dir(f.Path()))
	}
	if parent == idx.lastParent {
		return idx.lastDirPath
	}
	path := normalizePath(parent.Path())
	idx.lastParent, idx.lastDirPath = parent, path
	return path
}

// trackLocked remembers which folder f lives in. The caller holds the write lock.
func (idx *DuplicateIndex) trackLocked(dir string, f *scanner.FileNode, key contentKey) {
	idx.dirFiles[dir] = append(idx.dirFiles[dir], trackedFile{node: f, fingerprint: key.fingerprint})
}

// untrackLocked drops the file named base from folder dir and returns what was
// tracked there. ok is false when the folder never held that name. Names are
// matched case-insensitively, as Windows does. The caller holds the write lock.
func (idx *DuplicateIndex) untrackLocked(dir, base string) (trackedFile, bool) {
	list, known := idx.dirFiles[dir]
	if !known {
		return trackedFile{}, false
	}

	for i, tracked := range list {
		if tracked.node == nil || !strings.EqualFold(tracked.node.Name(), base) {
			continue
		}
		last := len(list) - 1
		list[i] = list[last]
		list[last] = trackedFile{}
		list = list[:last]
		if len(list) == 0 {
			delete(idx.dirFiles, dir)
		} else {
			idx.dirFiles[dir] = list
		}
		return tracked, true
	}
	return trackedFile{}, false
}

// detachLocked drops one tracked file from its group or from the singles park,
// keeping FileCount and WastedBytes consistent. The caller holds the write lock.
func (idx *DuplicateIndex) detachLocked(tracked trackedFile) {
	f, key := tracked.node, tracked.key()

	if grp, exists := idx.groups[key]; exists {
		for i, other := range grp.Files {
			if other != f {
				continue
			}
			// A ordem por ModTime é parte do contrato do grupo, então a remoção
			// preserva a ordem em vez de trocar com o último.
			grp.Files = append(grp.Files[:i], grp.Files[i+1:]...)
			grp.FileCount = len(grp.Files)
			if grp.FileCount < 2 {
				delete(idx.groups, key)
				if grp.FileCount == 1 {
					// The survivor stays known so a future twin re-forms the group.
					idx.singles[key] = grp.Files[0]
				}
				return
			}
			grp.WastedBytes = int64(grp.FileCount-1) * grp.FileSize
			return
		}
		return
	}

	if single, exists := idx.singles[key]; exists && single == f {
		delete(idx.singles, key)
	}
}

// RebuildIndex scans all FileNodes and builds duplicate groups.
func (idx *DuplicateIndex) RebuildIndex(files []*scanner.FileNode) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	idx.groups = make(map[contentKey]*DuplicateGroup)
	idx.singles = make(map[contentKey]*scanner.FileNode)
	idx.dirFiles = make(map[string][]trackedFile)
	idx.lastParent, idx.lastDirPath = nil, ""

	// Step 1: Bucket files by content key (hash + size)
	for _, f := range files {
		if f == nil {
			continue
		}
		hash := f.Hash()
		if hash == "" {
			continue
		}

		// Collision safety: the size in the key isolates different sized files
		// with a hypothetical same hash; grp.Hash isolates a folded fingerprint
		// that happened to repeat.
		key := contentKeyOf(hash, f.Size)
		grp, exists := idx.groups[key]
		if !exists {
			grp = &DuplicateGroup{
				ID:         groupKey(hash, f.Size),
				Hash:       hash,
				FileSize:   f.Size,
				Confidence: ConfidenceHash,
				Files:      make([]*scanner.FileNode, 0, 2),
			}
			idx.groups[key] = grp
		} else if grp.Hash != hash {
			continue
		}
		grp.Files = append(grp.Files, f)
		grp.FileCount = len(grp.Files)
		if grp.FileCount > 1 {
			grp.WastedBytes = int64(grp.FileCount-1) * grp.FileSize
		}
		idx.trackLocked(idx.dirPathOf(f), f, key)
	}

	// Step 2: Park groups with a single file. They are not duplicates today, but
	// keeping them lets Monitoramento promote them without a full rebuild.
	for key, grp := range idx.groups {
		if grp.FileCount < 2 {
			idx.singles[key] = grp.Files[0]
			delete(idx.groups, key)
			continue
		}
		// Sort files in group by ModTime ascending (oldest first)
		sort.Slice(grp.Files, func(i, j int) bool {
			return grp.Files[i].ModTime() < grp.Files[j].ModTime()
		})
	}
}

// UpsertFile inserts or refreshes one file in O(1), keeping FileCount and
// WastedBytes consistent. A file that arrives without a hash (locked, or not
// hashed yet) leaves the index instead of posing as a duplicate.
func (idx *DuplicateIndex) UpsertFile(f *scanner.FileNode) {
	if f == nil || f.Name() == "" {
		return
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	// O nó que chega do Monitoramento é novo em folha, mas mora na mesma pasta
	// com o mesmo nome do anterior: é assim que a versão velha sai do índice.
	dir := idx.dirPathOf(f)
	if old, tracked := idx.untrackLocked(dir, f.Name()); tracked {
		idx.detachLocked(old)
	}

	hash := f.Hash()
	if hash == "" {
		return
	}
	key := contentKeyOf(hash, f.Size)

	if grp, exists := idx.groups[key]; exists {
		if grp.Hash != hash {
			return
		}
		grp.Files = append(grp.Files, f)
		grp.FileCount = len(grp.Files)
		grp.WastedBytes = int64(grp.FileCount-1) * grp.FileSize
		sortByModTime(grp.Files)
		idx.trackLocked(dir, f, key)
		return
	}

	if other, exists := idx.singles[key]; exists {
		if other.Hash() != hash {
			return
		}
		delete(idx.singles, key)
		grp := &DuplicateGroup{
			ID:         groupKey(hash, f.Size),
			Hash:       hash,
			FileSize:   f.Size,
			Confidence: ConfidenceHash,
			Files:      []*scanner.FileNode{other, f},
		}
		sortByModTime(grp.Files)
		grp.FileCount = len(grp.Files)
		grp.WastedBytes = int64(grp.FileCount-1) * grp.FileSize
		idx.groups[key] = grp
		idx.trackLocked(dir, f, key)
		return
	}

	idx.singles[key] = f
	idx.trackLocked(dir, f, key)
}

func sortByModTime(files []*scanner.FileNode) {
	sort.Slice(files, func(i, j int) bool {
		return files[i].ModTime() < files[j].ModTime()
	})
}

// removeLocked drops one path from the index, matching case-insensitively.
// The caller must hold idx.mu for writing.
func (idx *DuplicateIndex) removeLocked(filePath string) bool {
	norm := normalizePath(filePath)
	tracked, known := idx.untrackLocked(filepath.Dir(norm), filepath.Base(norm))
	if !known {
		return false
	}
	idx.detachLocked(tracked)
	return true
}

// GetSummaryStats returns total duplicate counts and wasted space.
func (idx *DuplicateIndex) GetSummaryStats() (groupCount, fileCount int, wastedBytes int64) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	for _, grp := range idx.groups {
		groupCount++
		fileCount += grp.FileCount
		wastedBytes += grp.WastedBytes
	}
	return
}

// QueryFilter defines filtering and sorting options for duplicate querying.
type QueryFilter struct {
	SortBy    string `json:"sortBy"`    // "size_desc", "wasted_desc", "count_desc", "name_asc"
	MinSize   int64  `json:"minSize"`   // Minimum file size in bytes
	Extension string `json:"extension"` // e.g. ".zip"
	Search    string `json:"search"`    // Search text in path/name
	Limit     int    `json:"limit"`     // Pagination limit (0 = all)
	Offset    int    `json:"offset"`    // Pagination offset
}

// QueryResult returns matching groups and pagination metadata.
type QueryResult struct {
	TotalGroups int               `json:"totalGroups"`
	TotalFiles  int               `json:"totalFiles"`
	WastedBytes int64             `json:"wastedBytes"`
	Groups      []*DuplicateGroup `json:"groups"`
	Offset      int               `json:"offset"`
	Limit       int               `json:"limit"`
}

// Query returns filtered and sorted duplicate groups. The returned groups are
// detached copies, safe to serialize while Monitoramento updates the index.
func (idx *DuplicateIndex) Query(filter QueryFilter) QueryResult {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	var resultList []*DuplicateGroup
	var totalMatchedFiles int
	var totalMatchedWasted int64

	searchLower := strings.ToLower(filter.Search)
	extLower := strings.ToLower(filter.Extension)

	for _, grp := range idx.groups {
		// Size filter
		if filter.MinSize > 0 && grp.FileSize < filter.MinSize {
			continue
		}

		// Filter files in group if search or extension filter is present
		matches := false
		if searchLower == "" && extLower == "" {
			matches = true
		} else {
			for _, f := range grp.Files {
				extMatch := extLower == "" || strings.EqualFold(f.Extension(), extLower)
				searchMatch := searchLower == "" || strings.Contains(strings.ToLower(f.Path()), searchLower)
				if extMatch && searchMatch {
					matches = true
					break
				}
			}
		}

		if matches {
			resultList = append(resultList, grp)
			totalMatchedFiles += grp.FileCount
			totalMatchedWasted += grp.WastedBytes
		}
	}

	// Sorting
	switch filter.SortBy {
	case "wasted_desc":
		sort.Slice(resultList, func(i, j int) bool {
			return resultList[i].WastedBytes > resultList[j].WastedBytes
		})
	case "count_desc":
		sort.Slice(resultList, func(i, j int) bool {
			return resultList[i].FileCount > resultList[j].FileCount
		})
	case "name_asc":
		sort.Slice(resultList, func(i, j int) bool {
			if len(resultList[i].Files) > 0 && len(resultList[j].Files) > 0 {
				return resultList[i].Files[0].Name() < resultList[j].Files[0].Name()
			}
			return false
		})
	case "size_desc":
		fallthrough
	default:
		// Default: Sorted by file size descending (Largest duplicate files first)
		sort.Slice(resultList, func(i, j int) bool {
			return resultList[i].FileSize > resultList[j].FileSize
		})
	}

	totalGroups := len(resultList)
	start := filter.Offset
	if start > totalGroups {
		start = totalGroups
	}

	end := totalGroups
	if filter.Limit > 0 && start+filter.Limit < end {
		end = start + filter.Limit
	}

	paginated := make([]*DuplicateGroup, 0, end-start)
	for _, grp := range resultList[start:end] {
		paginated = append(paginated, grp.clone())
	}

	return QueryResult{
		TotalGroups: totalGroups,
		TotalFiles:  totalMatchedFiles,
		WastedBytes: totalMatchedWasted,
		Groups:      paginated,
		Offset:      start,
		Limit:       filter.Limit,
	}
}

// RemoveFileFromIndex removes a deleted file from the index without sweeping
// every group (achado H4): the folder of the path leads straight to the node,
// and the node's own hash leads to its group.
func (idx *DuplicateIndex) RemoveFileFromIndex(filePath string) {
	if filePath == "" {
		return
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.removeLocked(filePath)
}

// RemoveDirFromIndex removes every indexed file under dirPath and reports how
// many were dropped. Used when Monitoramento sees a whole folder disappear.
// A varredura é sobre as pastas conhecidas, não sobre os arquivos.
func (idx *DuplicateIndex) RemoveDirFromIndex(dirPath string) int {
	if dirPath == "" {
		return 0
	}

	root := normalizePath(dirPath)
	below := root
	if !strings.HasSuffix(below, string(filepath.Separator)) {
		below += string(filepath.Separator)
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	removed := 0
	for dir, list := range idx.dirFiles {
		if dir != root && !strings.HasPrefix(dir, below) {
			continue
		}
		for _, tracked := range list {
			if tracked.node == nil {
				continue
			}
			idx.detachLocked(tracked)
			removed++
		}
		delete(idx.dirFiles, dir)
	}
	return removed
}
