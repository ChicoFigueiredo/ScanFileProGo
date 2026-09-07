package indexer

import (
	"fmt"
	"runtime"
	"testing"

	"scanfile/pkg/scanner"
)

// benchTotalFiles é a amostra do benchmark: grande o bastante para o custo por
// arquivo aparecer acima do ruído do heap, pequena o bastante para rodar em
// segundos.
const benchTotalFiles = 200000

// maxBytesPorArquivoIndexado é o teto de memória que o Índice de Duplicados
// pode gastar por arquivo indexado, fora os FileNodes.
//
// Antes desta correção o índice guardava, por arquivo hasheado, um caminho
// absoluto minúsculo recém-montado mais a chave composta em texto, e media
// 241 B por arquivo — mais que os 139 B do próprio nó compacto do ADR-0001.
// Sem caminho por arquivo a medida cai para ~129 B, quase toda ela o objeto
// DuplicateGroup, que é o produto do índice, e não o rastro por arquivo. O teto
// tem folga para variações de versão de Go e de tabela de hash, mas não para o
// custo por caminho voltar.
const maxBytesPorArquivoIndexado = 170.0

// arvoreParaIndice monta uma árvore parecida com a de uma Varredura real: 20
// arquivos por pasta, dois terços deles duplicados em pares e o resto único.
// Os arquivos entram na árvore, então cada nó tem a pasta de verdade como pai
// (ADR-0001) em vez de uma pasta sintética por arquivo.
func arvoreParaIndice(total int) (*scanner.TreeManager, []*scanner.FileNode) {
	tm := scanner.NewTreeManager()
	tm.GetOrCreateRoot("C:\\")

	files := make([]*scanner.FileNode, 0, total)
	for i := 0; i < total; i++ {
		dir := fmt.Sprintf("C:\\Dados\\Colecao%03d\\Pasta%04d", i/20000, i/20)
		name := fmt.Sprintf("arquivo_%08d.bin", i)

		// Dois terços dos arquivos formam pares de duplicados; o terço restante
		// tem conteúdo único e fica parqueado como candidato solitário.
		var hash string
		var size int64
		if i%3 == 2 {
			hash = fmt.Sprintf("xxh64:%016x", i)
			size = int64(1024 + i%4096)
		} else {
			par := i / 2
			hash = fmt.Sprintf("xxh64:%016x", 0xD0D0D0D0D0D0+par)
			size = int64(4096 + par%4096)
		}

		f := scanner.NewFileNode(scanner.FileMeta{
			Name:      name,
			Size:      size,
			ModTime:   int64(1700000000 + i),
			Hash:      hash,
			Extension: ".bin",
		})
		tm.AddFileAt(dir, f)
		files = append(files, f)
	}
	return tm, files
}

// heapVivo devolve o heap ocupado por objetos vivos, depois de duas coletas
// para estabilizar a medida.
func heapVivo() uint64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

// medeBytesPorArquivoIndexado mede quantos bytes o Índice de Duplicados guarda
// por arquivo, descontando os FileNodes e a árvore, que já existiam antes.
func medeBytesPorArquivoIndexado(tb testing.TB, files []*scanner.FileNode) float64 {
	tb.Helper()

	antes := heapVivo()
	idx := NewDuplicateIndex()
	idx.RebuildIndex(files)
	depois := heapVivo()

	grupos, arquivos, _ := idx.GetSummaryStats()
	if grupos == 0 || arquivos == 0 {
		tb.Fatalf("índice vazio: %d grupos, %d arquivos", grupos, arquivos)
	}
	runtime.KeepAlive(idx)

	return float64(depois-antes) / float64(len(files))
}

// BenchmarkDuplicateIndexMemoriaPorArquivo publica a métrica do ADR-0001 para o
// Índice de Duplicados: nada de custo por arquivo proporcional ao caminho
// completo.
//
// Rode com: go test ./pkg/indexer/ -run '^$' -bench MemoriaPorArquivo -benchtime 1x
func BenchmarkDuplicateIndexMemoriaPorArquivo(b *testing.B) {
	tm, files := arvoreParaIndice(benchTotalFiles)

	var bytesPorArquivo float64
	for i := 0; i < b.N; i++ {
		bytesPorArquivo = medeBytesPorArquivoIndexado(b, files)
	}

	b.ReportMetric(bytesPorArquivo, "B/arquivo-indexado")
	runtime.KeepAlive(tm)
	runtime.KeepAlive(files)
}

// O índice não pode voltar a guardar um caminho por arquivo: o teto abaixo
// falha se ele voltar.
func TestDuplicateIndex_CustoDeMemoriaPorArquivo(t *testing.T) {
	if testing.Short() {
		t.Skip("medida de memória: pesada demais para -short")
	}

	tm, files := arvoreParaIndice(50000)
	bytesPorArquivo := medeBytesPorArquivoIndexado(t, files)
	t.Logf("índice de duplicados: %.1f B por arquivo indexado", bytesPorArquivo)

	if bytesPorArquivo > maxBytesPorArquivoIndexado {
		t.Fatalf("o índice gasta %.1f B por arquivo indexado, acima do teto de %.1f B: o custo por caminho voltou (ADR-0001)",
			bytesPorArquivo, maxBytesPorArquivoIndexado)
	}
	runtime.KeepAlive(tm)
	runtime.KeepAlive(files)
}
