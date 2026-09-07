package scanner

import (
	"fmt"
	"runtime"
	"testing"
)

// benchDirCount e benchFilesPerDir descrevem a árvore sintética do benchmark de
// memória: 100 mil pastas folha com 10 arquivos cada, ou seja 1 milhão de itens.
const (
	benchDirCount    = 100_000
	benchFilesPerDir = 10

	// maxBytesPerItem é o teto do ADR-0001: 150 bytes por item, ou seja 7,5 GB
	// para os 50 milhões de itens da máquina de referência.
	maxBytesPerItem = 150.0
)

// benchExtensions imita a distribuição real de um disco: poucas centenas de
// extensões distintas para milhões de arquivos.
var benchExtensions = []string{
	".txt", ".log", ".jpg", ".png", ".gif", ".pdf", ".docx", ".xlsx", ".pptx",
	".mp3", ".mp4", ".mkv", ".avi", ".zip", ".rar", ".7z", ".iso", ".exe",
	".dll", ".sys", ".ini", ".json", ".xml", ".html", ".css", ".js", ".go",
	".c", ".h", ".cpp", ".py", ".java", ".cs", ".sql", ".bak", ".tmp", ".dat",
	".bin", ".cfg", ".md",
}

// benchBaseNames imita nomes de arquivo reais, com acentos e espaços.
var benchBaseNames = []string{
	"relatório_final", "backup diário", "Orçamento 2024", "IMG_20240115",
	"apresentação-cliente", "notas da reunião", "instalador", "config",
	"planilha de custos", "vídeo institucional", "manual do usuário",
	"contrato assinado", "recibo", "declaração", "extrato bancário",
}

// buildBenchTree monta a árvore sintética usada pelo benchmark de memória.
func buildBenchTree() *TreeManager {
	tm := NewTreeManager()
	files := make([]*FileNode, 0, benchFilesPerDir)
	for d := 0; d < benchDirCount; d++ {
		dirPath := fmt.Sprintf(`C:\Bench\a%02d\b%03d\pasta_%06d`, d/10000, (d/100)%100, d)
		files = files[:0]
		batch := make([]*FileNode, benchFilesPerDir)
		for j := 0; j < benchFilesPerDir; j++ {
			idx := d*benchFilesPerDir + j
			ext := benchExtensions[idx%len(benchExtensions)]
			name := fmt.Sprintf("%s_%06d%s", benchBaseNames[idx%len(benchBaseNames)], idx, ext)
			batch[j] = newBenchFile(name, ext, idx)
		}
		tm.FastSetDir(dirPath, batch, nil)
	}
	return tm
}

// newBenchFile monta um arquivo sintético do mesmo jeito que a Fase 1: sem
// caminho, que FastSetDir deriva da pasta.
func newBenchFile(name, ext string, idx int) *FileNode {
	return NewFileNode(FileMeta{
		Name:          name,
		Size:          int64(idx % 4_000_000),
		AllocatedSize: int64(idx % 4_000_000),
		ModTime:       1_700_000_000 + int64(idx%100_000),
		CreateTime:    1_600_000_000,
		AccessTime:    1_700_000_000,
		Extension:     ext,
	})
}

// BenchmarkTreeMemoryPerItem mede o custo de memória por item da árvore em
// memória (ADR-0001). A meta é ficar em 150 bytes por arquivo ou menos.
func BenchmarkTreeMemoryPerItem(b *testing.B) {
	var bytesPerFile, bytesPerNode float64
	var dirCount int64

	for i := 0; i < b.N; i++ {
		runtime.GC()
		runtime.GC()
		var before runtime.MemStats
		runtime.ReadMemStats(&before)

		tm := buildBenchTree()

		runtime.GC()
		runtime.GC()
		var after runtime.MemStats
		runtime.ReadMemStats(&after)

		used := float64(after.HeapAlloc) - float64(before.HeapAlloc)
		fileCount := float64(benchDirCount * benchFilesPerDir)
		dirCount = countDirNodes(tm)
		bytesPerFile = used / fileCount
		bytesPerNode = used / (fileCount + float64(dirCount))

		runtime.KeepAlive(tm)
	}

	if bytesPerFile > maxBytesPerItem {
		b.Errorf("%.1f bytes por item, acima do teto de %.0f do ADR-0001", bytesPerFile, maxBytesPerItem)
	}

	b.ReportMetric(bytesPerFile, "B/item")
	b.ReportMetric(bytesPerNode, "B/nó")
	b.ReportMetric(float64(dirCount), "pastas")
}

func countDirNodes(tm *TreeManager) int64 {
	var n int64
	for _, r := range tm.GetRootsSnapshot() {
		walkDirs(r, func(*DirNode, []*FileNode) bool {
			n++
			return true
		})
	}
	return n
}
