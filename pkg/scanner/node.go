package scanner

import (
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

// node.go implementa o nó de arquivo compacto do ADR-0001.
//
// O caminho completo deixou de ser guardado em cada arquivo: ele é derivado do
// nome mais a cadeia de pastas. A extensão é internada, o hash vira bytes fixos
// com um identificador de algoritmo, e os booleanos viram um campo de bits.
// Campos raros (alvo de link, tempos fora do alcance do delta, extensão que não
// coube na tabela internada) moram num bloco `extra` alocado só quando existem.
//
// O JSON e o formato do Snapshot continuam idênticos: veja MarshalJSON.

// Bits de FileNode.flags.
const (
	flagSymlink uint8 = 1 << iota
	flagCompressed
	flagReusedFromCache
	flagWideCreate // CreateTime não coube no delta: está em extra
	flagWideAccess // AccessTime não coube no delta: está em extra
	flagExtraExt   // extensão não coube na tabela internada: está em extra
)

// FileNode representa um arquivo na árvore em memória.
//
// Não construa a struct diretamente: use NewFileNode ou NewFileNodeAt. Os
// campos exportados Size e AllocatedSize continuam sendo lidos e escritos
// livremente; o resto passa por acessores.
type FileNode struct {
	parent *DirNode    // pasta que contém o arquivo (nil enquanto solto)
	name   string      // nome do arquivo, sem caminho
	digest *fileDigest // Hash Completo e Pré-hash (nil enquanto não houver)
	extra  *fileExtra  // campos raros (nil no caso comum)

	// Size é o Tamanho Lógico em bytes.
	Size int64
	// AllocatedSize é o Tamanho Físico ocupado no disco, em bytes.
	AllocatedSize int64

	modTime     int64 // Unix, em segundos
	createDelta int32 // CreateTime - ModTime
	accessDelta int32 // AccessTime - ModTime
	extID       uint32
	flags       uint8
}

// fileDigest guarda o Hash Completo em bytes fixos mais o Pré-hash. Só é
// alocado para os arquivos que passaram pela Fase 2.
type fileDigest struct {
	quick uint64   // Pré-hash (0 = ausente)
	sum   [32]byte // bytes do Hash Completo
	algo  uint8    // índice em hashAlgoByCode (0 = sem hash reconhecido)
	n     uint8    // bytes válidos em sum
	raw   string   // hash em formato não reconhecido, guardado literalmente
}

// fileExtra guarda o que quase nenhum arquivo tem.
type fileExtra struct {
	linkTarget string
	ext        string
	createTime int64
	accessTime int64
}

// FileMeta são os dados de um arquivo na forma plana, do jeito que o Snapshot e
// a API os expõem. É a entrada de NewFileNode e a saída de Meta.
type FileMeta struct {
	Name              string
	Size              int64
	AllocatedSize     int64
	ModTime           int64
	CreateTime        int64
	AccessTime        int64
	Hash              string
	QuickHash         uint64
	Extension         string
	IsSymlink         bool
	LinkTarget        string
	IsCompressed      bool
	IsReusedFromCache bool
}

// NewFileNode monta um nó solto, ainda sem pasta. O caminho completo só existe
// depois que ele entra na árvore (AddFileAt, FastSetDir, ReplaceFile).
func NewFileNode(meta FileMeta) *FileNode {
	f := &FileNode{
		name:          meta.Name,
		Size:          meta.Size,
		AllocatedSize: meta.AllocatedSize,
		modTime:       meta.ModTime,
	}
	f.SetCreateTime(meta.CreateTime)
	f.SetAccessTime(meta.AccessTime)
	f.SetExtension(meta.Extension)
	f.SetHash(meta.Hash)
	f.SetQuickHash(meta.QuickHash)
	f.setFlag(flagSymlink, meta.IsSymlink)
	if meta.LinkTarget != "" {
		f.ensureExtra().linkTarget = meta.LinkTarget
	}
	f.SetCompressed(meta.IsCompressed)
	f.SetReusedFromCache(meta.IsReusedFromCache)
	return f
}

// NewFileNodeAt monta um nó a partir de um caminho completo, pendurado numa
// pasta sintética. Serve para quem só tem o caminho em mãos — o Monitoramento,
// a leitura de Snapshot e os testes. Ao entrar na árvore o nó é repai-ado para
// a pasta de verdade.
func NewFileNodeAt(fullPath string, meta FileMeta) *FileNode {
	dir, base := filepath.Split(filepath.Clean(fullPath))
	if meta.Name == "" {
		meta.Name = base
	}
	if meta.Extension == "" {
		meta.Extension = strings.ToLower(filepath.Ext(meta.Name))
	}
	f := NewFileNode(meta)
	if dir != "" {
		f.parent = &DirNode{Name: filepath.Clean(dir)}
	}
	return f
}

// Parent devolve a pasta que contém o arquivo, ou nil se ele estiver solto.
func (f *FileNode) Parent() *DirNode { return f.parent }

// Name devolve o nome do arquivo, sem caminho.
func (f *FileNode) Name() string {
	if f == nil {
		return ""
	}
	return f.name
}

// Path deriva o caminho completo subindo pela cadeia de pastas. É a leitura que
// antes era um campo: o custo passou a ser uma junção por chamada, e a memória
// por item caiu (ADR-0001).
func (f *FileNode) Path() string {
	if f == nil {
		return ""
	}
	if f.parent == nil {
		return f.name
	}
	return f.parent.joinChild(f.name)
}

// Extension devolve a extensão em minúsculas, com o ponto.
func (f *FileNode) Extension() string {
	if f == nil {
		return ""
	}
	if f.flags&flagExtraExt != 0 {
		return f.extra.ext
	}
	return internedExtension(f.extID)
}

// SetExtension registra a extensão, internando-a.
func (f *FileNode) SetExtension(ext string) {
	if ext == "" {
		f.extID = 0
		f.flags &^= flagExtraExt
		return
	}
	if id, ok := internExtension(ext); ok {
		f.extID = id
		f.flags &^= flagExtraExt
		return
	}
	f.ensureExtra().ext = ext
	f.flags |= flagExtraExt
}

// ModTime devolve a data de modificação em segundos Unix.
func (f *FileNode) ModTime() int64 { return f.modTime }

// SetModTime troca a data de modificação sem mexer nas outras duas.
func (f *FileNode) SetModTime(t int64) {
	create, access := f.CreateTime(), f.AccessTime()
	f.modTime = t
	f.SetCreateTime(create)
	f.SetAccessTime(access)
}

// CreateTime devolve a data de criação em segundos Unix.
func (f *FileNode) CreateTime() int64 {
	if f.flags&flagWideCreate != 0 {
		return f.extra.createTime
	}
	return f.modTime + int64(f.createDelta)
}

// SetCreateTime registra a data de criação. Datas distantes demais da data de
// modificação (o zero do FILETIME do Windows, por exemplo) vão para extra, sem
// perda de precisão.
func (f *FileNode) SetCreateTime(t int64) {
	if d, ok := timeDelta(t, f.modTime); ok {
		f.createDelta = d
		f.flags &^= flagWideCreate
		return
	}
	f.ensureExtra().createTime = t
	f.flags |= flagWideCreate
}

// AccessTime devolve a data do último acesso em segundos Unix.
func (f *FileNode) AccessTime() int64 {
	if f.flags&flagWideAccess != 0 {
		return f.extra.accessTime
	}
	return f.modTime + int64(f.accessDelta)
}

// SetAccessTime registra a data do último acesso.
func (f *FileNode) SetAccessTime(t int64) {
	if d, ok := timeDelta(t, f.modTime); ok {
		f.accessDelta = d
		f.flags &^= flagWideAccess
		return
	}
	f.ensureExtra().accessTime = t
	f.flags |= flagWideAccess
}

// timeDelta calcula t-base quando a diferença cabe em int32 (cerca de 68 anos
// para cada lado). Devolve ok=false quando não cabe.
func timeDelta(t, base int64) (int32, bool) {
	d := t - base
	// A subtração pode transbordar int64 com datas absurdas; a comparação
	// abaixo cobre esse caso porque o resultado transbordado não cabe em int32.
	if d < -(1<<31) || d > (1<<31)-1 {
		return 0, false
	}
	if (t > base) != (d > 0) && d != 0 {
		return 0, false // houve transbordo na subtração
	}
	return int32(d), true
}

// Hash devolve o Hash Completo como string com prefixo (ex.: "xxh64:ab...").
// Vazio quando o arquivo ainda não passou pela Fase 2.
func (f *FileNode) Hash() string {
	if f == nil || f.digest == nil {
		return ""
	}
	return f.digest.String()
}

// SetHash grava o Hash Completo. Strings no formato "<prefixo>:<hex>" viram
// bytes fixos; qualquer outro formato é guardado literalmente, para que o
// round-trip do Snapshot continue exato.
func (f *FileNode) SetHash(hash string) {
	if hash == "" {
		if f.digest != nil {
			f.digest.algo = 0
			f.digest.n = 0
			f.digest.raw = ""
			if f.digest.quick == 0 {
				f.digest = nil
			}
		}
		return
	}
	d := f.ensureDigest()
	d.raw = ""
	d.algo = 0
	d.n = 0
	if algo, sum, ok := decodeHash(hash); ok {
		d.algo = algo
		d.n = uint8(len(sum))
		copy(d.sum[:], sum)
		return
	}
	d.raw = hash
}

// QuickHash devolve o Pré-hash (0 = ausente).
func (f *FileNode) QuickHash() uint64 {
	if f == nil || f.digest == nil {
		return 0
	}
	return f.digest.quick
}

// SetQuickHash grava o Pré-hash.
func (f *FileNode) SetQuickHash(q uint64) {
	if q == 0 {
		if f.digest != nil {
			f.digest.quick = 0
			if f.digest.algo == 0 && f.digest.raw == "" {
				f.digest = nil
			}
		}
		return
	}
	f.ensureDigest().quick = q
}

// IsSymlink informa se o arquivo é um link simbólico ou uma junção.
func (f *FileNode) IsSymlink() bool { return f.flags&flagSymlink != 0 }

// LinkTarget devolve o alvo do link, quando houver.
func (f *FileNode) LinkTarget() string {
	if f.extra == nil {
		return ""
	}
	return f.extra.linkTarget
}

// SetSymlink marca o arquivo como link e registra o alvo.
func (f *FileNode) SetSymlink(target string) {
	f.flags |= flagSymlink
	if target != "" || f.extra != nil {
		f.ensureExtra().linkTarget = target
	}
}

// IsCompressed informa se o arquivo está comprimido ou esparso no NTFS.
func (f *FileNode) IsCompressed() bool { return f.flags&flagCompressed != 0 }

// SetCompressed marca o arquivo como comprimido ou esparso.
func (f *FileNode) SetCompressed(v bool) { f.setFlag(flagCompressed, v) }

// IsReusedFromCache informa se o hash veio do Snapshot anterior (Quick Scan).
func (f *FileNode) IsReusedFromCache() bool { return f.flags&flagReusedFromCache != 0 }

// SetReusedFromCache marca o arquivo como reaproveitado do Snapshot anterior.
func (f *FileNode) SetReusedFromCache(v bool) { f.setFlag(flagReusedFromCache, v) }

func (f *FileNode) setFlag(bit uint8, v bool) {
	if v {
		f.flags |= bit
	} else {
		f.flags &^= bit
	}
}

func (f *FileNode) ensureExtra() *fileExtra {
	if f.extra == nil {
		f.extra = &fileExtra{}
	}
	return f.extra
}

func (f *FileNode) ensureDigest() *fileDigest {
	if f.digest == nil {
		f.digest = &fileDigest{}
	}
	return f.digest
}

// Meta devolve os dados do arquivo na forma plana, com o caminho já derivado.
func (f *FileNode) Meta() FileMeta {
	return FileMeta{
		Name:              f.name,
		Size:              f.Size,
		AllocatedSize:     f.AllocatedSize,
		ModTime:           f.ModTime(),
		CreateTime:        f.CreateTime(),
		AccessTime:        f.AccessTime(),
		Hash:              f.Hash(),
		QuickHash:         f.QuickHash(),
		Extension:         f.Extension(),
		IsSymlink:         f.IsSymlink(),
		LinkTarget:        f.LinkTarget(),
		IsCompressed:      f.IsCompressed(),
		IsReusedFromCache: f.IsReusedFromCache(),
	}
}

// fileNodeJSON é o formato em disco e na API: exatamente as chaves, a ordem e
// as regras de omitempty do FileNode original. Não mexa sem quebrar Snapshots.
type fileNodeJSON struct {
	Path              string `json:"path"`
	Name              string `json:"name"`
	Size              int64  `json:"size"`
	AllocatedSize     int64  `json:"allocatedSize"`
	ModTime           int64  `json:"modTime"`
	CreateTime        int64  `json:"createTime"`
	AccessTime        int64  `json:"accessTime"`
	Hash              string `json:"hash,omitempty"`
	QuickHash         uint64 `json:"quickHash,omitempty"`
	Extension         string `json:"extension"`
	IsSymlink         bool   `json:"isSymlink,omitempty"`
	LinkTarget        string `json:"linkTarget,omitempty"`
	IsCompressed      bool   `json:"isCompressed,omitempty"`
	IsReusedFromCache bool   `json:"isReusedFromCache,omitempty"`
}

// jsonView monta a visão serializável usando um caminho de pasta já conhecido,
// para o exportador não repetir a derivação por arquivo.
func (f *FileNode) jsonView(dirPath string) fileNodeJSON {
	path := f.name
	if dirPath != "" {
		path = joinPathElem(dirPath, f.name)
	} else if f.parent != nil {
		path = f.Path()
	}
	m := f.Meta()
	return fileNodeJSON{
		Path:              path,
		Name:              m.Name,
		Size:              m.Size,
		AllocatedSize:     m.AllocatedSize,
		ModTime:           m.ModTime,
		CreateTime:        m.CreateTime,
		AccessTime:        m.AccessTime,
		Hash:              m.Hash,
		QuickHash:         m.QuickHash,
		Extension:         m.Extension,
		IsSymlink:         m.IsSymlink,
		LinkTarget:        m.LinkTarget,
		IsCompressed:      m.IsCompressed,
		IsReusedFromCache: m.IsReusedFromCache,
	}
}

// MarshalJSON reproduz byte a byte o JSON do FileNode original. O receptor é
// por valor de propósito: assim uma cópia do nó também serializa direito.
func (f FileNode) MarshalJSON() ([]byte, error) {
	return json.Marshal(f.jsonView(""))
}

// UnmarshalJSON reconstrói o nó a partir do JSON histórico. O caminho vira uma
// pasta sintética; ao entrar na árvore o nó é repai-ado.
func (f *FileNode) UnmarshalJSON(data []byte) error {
	var raw fileNodeJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*f = *fileNodeFromJSON(raw)
	return nil
}

// fileNodeFromJSON monta o nó a partir da visão serializável.
func fileNodeFromJSON(raw fileNodeJSON) *FileNode {
	meta := FileMeta{
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
	}
	if raw.Path == "" {
		return NewFileNode(meta)
	}
	if meta.Name == "" {
		meta.Name = filepath.Base(raw.Path)
	}
	return NewFileNodeAt(raw.Path, meta)
}

// ---------------------------------------------------------------------------
// Hash em bytes fixos
// ---------------------------------------------------------------------------

// hashAlgoByCode mapeia o código guardado em fileDigest.algo para o algoritmo.
// A ordem é parte do formato em memória; acrescente no fim.
var hashAlgoByCode = []string{"", HashXXHash, HashBlake3, HashMD5, HashSHA256}

// hashAlgoBytes é o tamanho do digest de cada algoritmo, na mesma ordem.
var hashAlgoBytes = []int{0, 8, 32, 16, 32}

// decodeHash quebra "<prefixo>:<hex>" nos bytes do digest. Só aceita o hex com
// exatamente o tamanho do algoritmo, para que a reconstrução seja idêntica.
func decodeHash(hash string) (algo uint8, sum []byte, ok bool) {
	colon := strings.IndexByte(hash, ':')
	if colon <= 0 {
		return 0, nil, false
	}
	prefix := hash[:colon+1]
	code := uint8(0)
	for i := 1; i < len(hashAlgoByCode); i++ {
		if hashPrefixes[hashAlgoByCode[i]] == prefix {
			code = uint8(i)
			break
		}
	}
	if code == 0 {
		return 0, nil, false
	}
	hexPart := hash[colon+1:]
	if len(hexPart) != hashAlgoBytes[code]*2 {
		return 0, nil, false
	}
	buf, err := hex.DecodeString(hexPart)
	if err != nil {
		return 0, nil, false
	}
	// hex.EncodeToString sempre devolve minúsculas: um hex em maiúsculas não
	// voltaria idêntico, então fica como raw.
	if hex.EncodeToString(buf) != hexPart {
		return 0, nil, false
	}
	return code, buf, true
}

// String reconstrói a string com prefixo gravada no Snapshot e devolvida pela API.
func (d *fileDigest) String() string {
	if d == nil {
		return ""
	}
	if d.algo == 0 {
		return d.raw
	}
	prefix := hashPrefixes[hashAlgoByCode[d.algo]]
	return prefix + hex.EncodeToString(d.sum[:d.n])
}

// ---------------------------------------------------------------------------
// Internação de extensões
// ---------------------------------------------------------------------------

// maxInternedExtensions limita a tabela de extensões. Um disco normal tem
// algumas centenas; o teto existe só para que nomes patológicos (um sufixo
// único por arquivo) não transformem a tabela num vazamento.
const maxInternedExtensions = 1 << 16

// extTable é a tabela internada de extensões. Ela é imutável depois de
// publicada: crescer significa publicar uma tabela nova, o que deixa a leitura
// livre de lock.
type extTable struct {
	ids  map[string]uint32
	list []string
}

var extIntern struct {
	mu    sync.Mutex
	table atomic.Pointer[extTable]
}

func init() {
	extIntern.table.Store(&extTable{ids: map[string]uint32{"": 0}, list: []string{""}})
}

// internExtension devolve o identificador da extensão, criando-o se preciso.
// ok=false quando a tabela está cheia e o chamador precisa guardar a string.
func internExtension(ext string) (uint32, bool) {
	if ext == "" {
		return 0, true
	}
	if id, ok := extIntern.table.Load().ids[ext]; ok {
		return id, true
	}

	extIntern.mu.Lock()
	defer extIntern.mu.Unlock()

	current := extIntern.table.Load()
	if id, ok := current.ids[ext]; ok {
		return id, true
	}
	if len(current.list) >= maxInternedExtensions {
		return 0, false
	}

	next := &extTable{
		ids:  make(map[string]uint32, len(current.ids)+1),
		list: make([]string, len(current.list), len(current.list)+1),
	}
	for k, v := range current.ids {
		next.ids[k] = v
	}
	copy(next.list, current.list)
	id := uint32(len(next.list))
	next.list = append(next.list, ext)
	next.ids[ext] = id
	extIntern.table.Store(next)
	return id, true
}

// internedExtension devolve a extensão de um identificador, sem lock.
func internedExtension(id uint32) string {
	if id == 0 {
		return ""
	}
	list := extIntern.table.Load().list
	if int(id) < len(list) {
		return list[id]
	}
	return ""
}

// InternedExtensionCount informa quantas extensões distintas a tabela guarda.
// Existe para os testes e para o diagnóstico de memória.
func InternedExtensionCount() int {
	return len(extIntern.table.Load().list)
}
