// Command gen produz o fixture `legacy_v2.scanfile.gz` usado pelo teste de
// compatibilidade do importador em streaming.
//
// O arquivo foi gerado com o exportador ORIGINAL (encoding/json em documento
// único, versão 2 do formato), antes da reescrita de `cache.go` para escrita e
// leitura em streaming. Ele existe para provar que o importador novo continua
// lendo snapshots antigos, com as mesmas contagens.
//
// ATENÇÃO: NÃO regenere `legacy_v2.scanfile.gz` com este programa. Ele chama
// scanner.ExportCache, que hoje é o exportador em STREAMING; rodá-lo de novo
// substituiria o fixture legado por um arquivo escrito pelo código sob teste, e
// o teste de compatibilidade deixaria de provar qualquer coisa. O programa fica
// no repositório como documentação de como o fixture nasceu e como produzir um
// novo fixture, com OUTRO nome, se o formato mudar:
//
//	go run ./pkg/scanner/testdata/gen
//
// Este diretório está sob `testdata/`, portanto é ignorado por `go build ./...`
// e por `go test ./...`.
//
// Conteúdo: 3 pastas (C:\legacy, C:\legacy\docs, C:\legacy\media) mais a raiz
// C:\, e 5 arquivos somando 68.500 bytes lógicos e 73.728 alocados.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"scanfile/pkg/scanner"
)

func main() {
	tm := scanner.NewTreeManager()
	tm.GetOrCreateRoot("C:\\")

	files := []*scanner.FileNode{
		{Path: `C:\legacy\readme.txt`, Name: "readme.txt", Size: 500, AllocatedSize: 4096, ModTime: 1700000000, CreateTime: 1699000000, AccessTime: 1700000500, Hash: "xxh64:0000000000000001", QuickHash: 11, Extension: ".txt"},
		{Path: `C:\legacy\docs\manual.pdf`, Name: "manual.pdf", Size: 8000, AllocatedSize: 8192, ModTime: 1700000001, CreateTime: 1699000001, AccessTime: 1700000501, Hash: "xxh64:0000000000000002", QuickHash: 22, Extension: ".pdf"},
		{Path: `C:\legacy\docs\notas.txt`, Name: "notas.txt", Size: 12000, AllocatedSize: 12288, ModTime: 1700000002, CreateTime: 1699000002, AccessTime: 1700000502, Hash: "xxh64:0000000000000003", QuickHash: 33, Extension: ".txt"},
		{Path: `C:\legacy\media\video.mp4`, Name: "video.mp4", Size: 40000, AllocatedSize: 40960, ModTime: 1700000003, CreateTime: 1699000003, AccessTime: 1700000503, Hash: "xxh64:0000000000000004", QuickHash: 44, Extension: ".mp4"},
		{Path: `C:\legacy\media\capa.png`, Name: "capa.png", Size: 8000, AllocatedSize: 8192, ModTime: 1700000004, CreateTime: 1699000004, AccessTime: 1700000504, Hash: "xxh64:0000000000000005", QuickHash: 55, Extension: ".png"},
	}
	for _, f := range files {
		tm.AddFile(f)
	}
	tm.ComputeAggregatedSizes()

	config := scanner.ScanConfig{
		Roots:          []string{"C:\\"},
		WorkerThreads:  8,
		HashAlgorithm:  "xxhash",
		MinSizeForHash: 1,
	}

	out := filepath.Join("pkg", "scanner", "testdata", "legacy_v2.scanfile.gz")
	f, err := os.Create(out)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	if err := scanner.ExportCache(tm, []string{"C:\\"}, config, f); err != nil {
		panic(err)
	}
	fmt.Println("fixture gerado em", out)
}
