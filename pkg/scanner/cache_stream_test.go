package scanner

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func sampleTree() *TreeManager {
	tm := NewTreeManager()
	tm.GetOrCreateRoot(`C:\`)
	tm.AddFile(&FileNode{Path: `C:\legacy\readme.txt`, Name: "readme.txt", Size: 500, AllocatedSize: 4096, ModTime: 1700000000, Hash: "xxh64:0000000000000001", QuickHash: 11, Extension: ".txt"})
	tm.AddFile(&FileNode{Path: `C:\legacy\docs\manual.pdf`, Name: "manual.pdf", Size: 8000, AllocatedSize: 8192, ModTime: 1700000001, Hash: "xxh64:0000000000000002", QuickHash: 22, Extension: ".pdf"})
	tm.AddFile(&FileNode{Path: `C:\legacy\docs\notas.txt`, Name: "notas.txt", Size: 12000, AllocatedSize: 12288, ModTime: 1700000002, Hash: "xxh64:0000000000000003", QuickHash: 33, Extension: ".txt"})
	tm.AddFile(&FileNode{Path: `C:\legacy\media\video.mp4`, Name: "video.mp4", Size: 40000, AllocatedSize: 40960, ModTime: 1700000003, Hash: "xxh64:0000000000000004", QuickHash: 44, Extension: ".mp4"})
	tm.AddFile(&FileNode{Path: `C:\legacy\media\capa.png`, Name: "capa.png", Size: 8000, AllocatedSize: 8192, ModTime: 1700000004, Hash: "xxh64:0000000000000005", QuickHash: 55, Extension: ".png"})
	tm.ComputeAggregatedSizes()
	return tm
}

// As chaves de topo do snapshot não podem mudar: o formato v2 continua o mesmo.
func TestExportCache_KeepsJSONKeys(t *testing.T) {
	var buf bytes.Buffer
	if err := ExportCache(sampleTree(), []string{`C:\`}, ScanConfig{Roots: []string{`C:\`}, HashAlgorithm: HashXXHash}, &buf); err != nil {
		t.Fatal(err)
	}

	gz, err := gzip.NewReader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(gz).Decode(&raw); err != nil {
		t.Fatalf("saída não é um JSON válido: %v", err)
	}

	want := []string{"version", "timestamp", "roots", "totalFiles", "totalDirs", "totalBytes", "totalAllocatedBytes", "files", "directories", "scanSettings"}
	for _, k := range want {
		if _, ok := raw[k]; !ok {
			t.Errorf("chave %q sumiu do snapshot", k)
		}
	}
	if len(raw) != len(want) {
		got := make([]string, 0, len(raw))
		for k := range raw {
			got = append(got, k)
		}
		sort.Strings(got)
		t.Errorf("snapshot ganhou/perdeu chaves: %v", got)
	}

	// Cada arquivo mantém as chaves que a interface já consome.
	var files []map[string]any
	if err := json.Unmarshal(raw["files"], &files); err != nil {
		t.Fatal(err)
	}
	if len(files) != 5 {
		t.Fatalf("esperava 5 arquivos, obtive %d", len(files))
	}
	for _, k := range []string{"path", "name", "size", "allocatedSize", "modTime", "hash", "quickHash", "extension"} {
		if _, ok := files[0][k]; !ok {
			t.Errorf("chave %q sumiu do FileNode serializado", k)
		}
	}
}

func TestCache_StreamingRoundtrip(t *testing.T) {
	var buf bytes.Buffer
	cfg := ScanConfig{Roots: []string{`C:\`}, HashAlgorithm: HashBlake3, WorkerThreads: 8}
	if err := ExportCache(sampleTree(), []string{`C:\`}, cfg, &buf); err != nil {
		t.Fatal(err)
	}

	tm, summary, err := ImportCacheStream(&buf, nil)
	if err != nil {
		t.Fatal(err)
	}

	if summary.TotalFiles != 5 {
		t.Errorf("TotalFiles = %d, quer 5", summary.TotalFiles)
	}
	if summary.TotalDirs != 4 {
		t.Errorf("TotalDirs = %d, quer 4", summary.TotalDirs)
	}
	if summary.TotalBytes != 68500 {
		t.Errorf("TotalBytes = %d, quer 68500", summary.TotalBytes)
	}
	if summary.HashAlgorithm != HashBlake3 {
		t.Errorf("HashAlgorithm = %q, quer %q", summary.HashAlgorithm, HashBlake3)
	}
	if len(summary.Roots) != 1 || summary.Roots[0] != `C:\` {
		t.Errorf("Roots = %v", summary.Roots)
	}
	if summary.Timestamp.IsZero() {
		t.Error("Timestamp não foi preenchido")
	}

	files := tm.GetAllFiles()
	if len(files) != 5 {
		t.Fatalf("árvore reconstruída com %d arquivos, quer 5", len(files))
	}
	docs := tm.FindDir(`C:\legacy\docs`)
	if docs == nil {
		t.Fatal("C:\\legacy\\docs deveria existir")
	}
	if docs.TotalSize != 20000 {
		t.Errorf("docs.TotalSize = %d, quer 20000", docs.TotalSize)
	}
	root := tm.FindDir(`C:\`)
	if root.FileCount != 5 || root.TotalSize != 68500 {
		t.Errorf("raiz agregada errada: %d arquivos, %d bytes", root.FileCount, root.TotalSize)
	}
	for _, f := range files {
		if f.Hash == "" {
			t.Errorf("%s perdeu o hash no roundtrip", f.Path)
		}
		if f.QuickHash == 0 {
			t.Errorf("%s perdeu o Pré-hash no roundtrip", f.Path)
		}
	}
}

// O fixture foi gerado com o exportador ANTERIOR (documento único). O
// importador em streaming precisa lê-lo com as mesmas contagens.
func TestImportCache_LegacyV2Fixture(t *testing.T) {
	path := filepath.Join("testdata", "legacy_v2.scanfile.gz")
	tm, summary, err := LoadCacheSummaryFromFile(path, nil)
	if err != nil {
		t.Fatalf("falha ao importar o fixture legado: %v", err)
	}

	if summary.Version != CurrentCacheVersion {
		t.Errorf("Version = %d, quer %d", summary.Version, CurrentCacheVersion)
	}
	if summary.TotalFiles != 5 {
		t.Errorf("TotalFiles = %d, quer 5", summary.TotalFiles)
	}
	if summary.TotalDirs != 4 {
		t.Errorf("TotalDirs = %d, quer 4", summary.TotalDirs)
	}
	if summary.TotalBytes != 68500 {
		t.Errorf("TotalBytes = %d, quer 68500", summary.TotalBytes)
	}
	if summary.TotalAllocatedBytes != 73728 {
		t.Errorf("TotalAllocatedBytes = %d, quer 73728", summary.TotalAllocatedBytes)
	}
	if summary.HashAlgorithm != HashXXHash {
		t.Errorf("HashAlgorithm = %q, quer %q", summary.HashAlgorithm, HashXXHash)
	}

	files := tm.GetAllFiles()
	if len(files) != 5 {
		t.Fatalf("árvore com %d arquivos, quer 5", len(files))
	}
	media := tm.FindDir(`C:\legacy\media`)
	if media == nil || media.FileCount != 2 || media.TotalSize != 48000 {
		t.Errorf("C:\\legacy\\media reconstruída errada: %+v", media)
	}

	// A API histórica continua respondendo, agora sem devolver a lista.
	_, snap, err := LoadCacheFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if snap.TotalFiles != 5 || snap.TotalBytes != 68500 {
		t.Errorf("CacheSnapshot legado com totais errados: %+v", snap)
	}
	if len(snap.Files) != 0 {
		t.Errorf("a importação em streaming não deve materializar %d arquivos no snapshot", len(snap.Files))
	}
}

// O importador tolera qualquer ordem de chaves e chaves desconhecidas.
func TestImportCacheStream_ToleratesKeyOrder(t *testing.T) {
	doc := `{
		"files":[
			{"path":"C:\\x\\a.txt","name":"a.txt","size":100,"extension":".txt","hash":"xxh64:aa"},
			{"path":"C:\\x\\y\\b.txt","name":"b.txt","size":200,"extension":".txt"}
		],
		"chaveDesconhecida":{"qualquer":[1,2,3]},
		"directories":["C:\\","C:\\x","C:\\x\\y","C:\\vazia"],
		"scanSettings":{"roots":["C:\\"],"hashAlgorithm":"md5"},
		"totalBytes":300,
		"roots":["C:\\"],
		"totalFiles":2,
		"version":2,
		"timestamp":"2026-01-02T03:04:05Z",
		"totalDirs":4
	}`

	tm, summary, err := ImportCacheStream(strings.NewReader(doc), nil)
	if err != nil {
		t.Fatalf("importação falhou: %v", err)
	}
	if summary.TotalFiles != 2 || summary.TotalBytes != 300 || summary.TotalDirs != 4 {
		t.Errorf("resumo errado: %+v", summary)
	}
	if summary.HashAlgorithm != HashMD5 {
		t.Errorf("HashAlgorithm = %q, quer md5", summary.HashAlgorithm)
	}
	if len(tm.GetAllFiles()) != 2 {
		t.Errorf("esperava 2 arquivos na árvore")
	}
	// Pasta vazia declarada em "directories" precisa existir mesmo sem arquivos.
	if tm.FindDir(`C:\vazia`) == nil {
		t.Error("pasta vazia não foi recriada")
	}
	if d := tm.FindDir(`C:\x`); d == nil || d.TotalSize != 300 {
		t.Errorf("C:\\x agregada errada: %+v", d)
	}
}

func TestImportCacheStream_RejectsGarbage(t *testing.T) {
	if _, _, err := ImportCacheStream(strings.NewReader("isso não é json"), nil); err == nil {
		t.Fatal("esperava erro para conteúdo inválido")
	}
	if _, _, err := ImportCacheStream(strings.NewReader(`["array no topo"]`), nil); err == nil {
		t.Fatal("esperava erro para JSON que não é objeto")
	}
}

func TestImportCacheStream_ReportsProgress(t *testing.T) {
	var buf bytes.Buffer
	if err := ExportCache(sampleTree(), []string{`C:\`}, ScanConfig{}, &buf); err != nil {
		t.Fatal(err)
	}
	var stages []string
	if _, _, err := ImportCacheStream(&buf, func(stage string, percent float64, details string) {
		stages = append(stages, stage)
		if percent < 0 || percent > 100 {
			t.Errorf("percentual fora da faixa: %v", percent)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if len(stages) == 0 {
		t.Error("nenhum estágio foi reportado")
	}
}

func TestBuildQuickScanLookupFromTree(t *testing.T) {
	tm := sampleTree()
	lookup := BuildQuickScanLookupFromTree(tm)
	if len(lookup) != 5 {
		t.Fatalf("lookup com %d entradas, quer 5", len(lookup))
	}
	key := strings.ToLower(filepath.Clean(`C:\legacy\docs\notas.txt`))
	f, ok := lookup[key]
	if !ok || f == nil {
		t.Fatalf("chave normalizada %q não encontrada", key)
	}
	if f.Hash != "xxh64:0000000000000003" {
		t.Errorf("hash = %q", f.Hash)
	}

	if got := BuildQuickScanLookupFromTree(nil); len(got) != 0 {
		t.Errorf("árvore nula deveria devolver mapa vazio, obtive %d", len(got))
	}
}

func TestSaveAndLoadCacheSummary(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "snap.scanfile.gz")
	cfg := ScanConfig{Roots: []string{`C:\`}, HashAlgorithm: HashSHA256}
	if err := SaveCacheToFile(sampleTree(), []string{`C:\`}, cfg, target); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Fatal("arquivo de snapshot vazio")
	}

	tm, summary, err := LoadCacheSummaryFromFile(target, nil)
	if err != nil {
		t.Fatal(err)
	}
	if summary.TotalFiles != 5 || summary.HashAlgorithm != HashSHA256 {
		t.Errorf("resumo inesperado: %+v", summary)
	}
	if len(tm.GetAllFiles()) != 5 {
		t.Error("árvore reconstruída incompleta")
	}
}
