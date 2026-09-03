package scanner

import (
	"path/filepath"
	"strings"
)

// MaxScanDepth é a profundidade máxima de pastas percorrida pela Fase 1.
// Passar disso significa, na prática, um laço de junções ou um caminho
// patológico; o item é pulado e registrado.
const MaxScanDepth = 50

// maxSkippedRing é o tamanho do anel de Itens Pulados exposto pela API.
const maxSkippedRing = 500

// Motivos de Item Pulado. São mostrados ao usuário, portanto em pt-BR.
const (
	ReasonSystemInternal = "Arquivo ou pasta interna do sistema Windows"
	ReasonWSLPseudo      = "Pseudo-sistema de arquivos do WSL"
	ReasonMaxDepth       = "Profundidade máxima de pastas atingida"
	ReasonVisitedTarget  = "Junção ou link para pasta já mapeada"
	ReasonAncestorLoop   = "Junção aponta para uma pasta ancestral"
	ReasonIrregularFile  = "Pseudo-arquivo sem conteúdo real"
)

// windowsInternalNames são nomes que nunca interessam à Varredura, em qualquer
// volume Windows.
var windowsInternalNames = map[string]bool{
	"system volume information": true,
	"$recycle.bin":              true,
	"$winreagent":               true,
	"pagefile.sys":              true,
	"hiberfil.sys":              true,
	"swapfile.sys":              true,
	"dumpstack.log":             true,
}

// wslPseudoNames só são pulados dentro de uma Raiz Varrida detectada como WSL.
var wslPseudoNames = map[string]bool{
	"proc":  true,
	"sys":   true,
	"dev":   true,
	"mnt":   true,
	"kcore": true,
}

// classifyIgnoredName decide se um item deve ser pulado só pelo nome e devolve
// o motivo em pt-BR.
//
// A heurística do WSL depende de underWSL, calculado uma vez por Raiz Varrida:
// sem isso, pastas legítimas como C:\dev e D:\sys eram descartadas em silêncio
// (achado M1).
func classifyIgnoredName(name string, dirPath string, underWSL bool) (bool, string) {
	lower := strings.ToLower(name)

	if windowsInternalNames[lower] {
		return true, ReasonSystemInternal
	}
	if underWSL && wslPseudoNames[lower] {
		return true, ReasonWSLPseudo
	}
	return false, ""
}

// exceedsMaxDepth informa se o caminho passou de MaxScanDepth níveis.
func exceedsMaxDepth(path string) bool {
	clean := strings.TrimRight(filepath.Clean(path), `\/`)
	depth := strings.Count(clean, `\`) + strings.Count(clean, "/")
	if strings.HasPrefix(clean, `\\`) {
		// Caminho UNC: os dois separadores iniciais não são níveis.
		depth -= 2
	}
	return depth > MaxScanDepth
}

// isWSLRoot detecta uma Raiz Varrida do WSL: caminho UNC \\wsl$ ou
// \\wsl.localhost, ou volume cujo sistema de arquivos é 9P.
func isWSLRoot(root string, fsName string) bool {
	lower := strings.ToLower(root)
	if strings.HasPrefix(lower, `\\wsl$`) || strings.HasPrefix(lower, `\\wsl.localhost`) ||
		strings.HasPrefix(lower, `//wsl$`) || strings.HasPrefix(lower, `//wsl.localhost`) {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(fsName), "9P")
}

// pathHasPrefix compara caminhos sem diferenciar caixa, respeitando a fronteira
// de separador (C:\Users2 não é prefixo de C:\Users).
func pathHasPrefix(path, prefix string) bool {
	p := strings.ToLower(strings.TrimRight(filepath.Clean(path), `\/`))
	q := strings.ToLower(strings.TrimRight(filepath.Clean(prefix), `\/`))
	if q == "" {
		return false
	}
	if p == q {
		return true
	}
	return strings.HasPrefix(p, q+`\`) || strings.HasPrefix(p, q+"/")
}
