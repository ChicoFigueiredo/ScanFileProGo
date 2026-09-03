package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"scanfile/pkg/scanner"
)

// buildSnapshotTree monta uma árvore sintética e devolve suas raízes.
func buildSnapshotTree(t *testing.T, root string) *scanner.TreeManager {
	t.Helper()
	tm := scanner.NewTreeManager()
	tm.GetOrCreateRoot(root)
	for i := 0; i < 3; i++ {
		name := filepath.Join(root, "sub", "a.bin")
		if i > 0 {
			name = filepath.Join(root, "sub", "b"+string(rune('0'+i))+".bin")
		}
		tm.AddFile(scanner.NewFileNodeAt(name, scanner.FileMeta{Name: filepath.Base(name), Size: int64(100 * (i + 1)), Extension: ".bin", ModTime: 1700000000}))
	}
	tm.ComputeAggregatedSizes()
	return tm
}

func TestRestoreAutoSaveReturnsSummaryAndSwapsScannerTree(t *testing.T) {
	app, ts := newScanTestServer(t)

	root := tempDir(t)
	tm := buildSnapshotTree(t, root)
	if _, err := scanner.SaveAutoSave(tm, []string{root}, scanner.ScanConfig{Roots: []string{root}, HashAlgorithm: "xxhash"}, app.savedScansDir); err != nil {
		t.Fatalf("não foi possível gravar o Autosave de teste: %v", err)
	}

	previousTree := app.Tree

	resp := postJSON(t, ts, "/api/cache/autosave/restore", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("restore devolveu %d, esperado 200", resp.StatusCode)
	}

	var body map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("restore devolveu JSON inválido: %v", err)
	}

	var status string
	_ = json.Unmarshal(body["status"], &status)
	if status != "restored" {
		t.Errorf(`status = %q, esperado "restored"`, status)
	}
	if _, ok := body["snapshot"]; ok {
		t.Error("restore não pode mais devolver o campo snapshot")
	}
	raw, ok := body["summary"]
	if !ok {
		t.Fatal("restore precisa devolver o campo summary")
	}

	// O resumo nunca carrega a lista de arquivos (achado H3).
	var asMap map[string]any
	if err := json.Unmarshal(raw, &asMap); err != nil {
		t.Fatalf("summary inválido: %v", err)
	}
	if _, ok := asMap["files"]; ok {
		t.Error("summary não pode conter a lista de arquivos")
	}

	var summary scanner.CacheSnapshotSummary
	if err := json.Unmarshal(raw, &summary); err != nil {
		t.Fatalf("summary não decodifica em CacheSnapshotSummary: %v", err)
	}
	if summary.TotalFiles != 3 {
		t.Errorf("totalFiles = %d, esperado 3", summary.TotalFiles)
	}
	if len(summary.Roots) != 1 || summary.Roots[0] != root {
		t.Errorf("roots = %v, esperado [%s]", summary.Roots, root)
	}

	// A árvore ativa foi trocada em TODOS os lugares (achado C4).
	if app.Tree == previousTree {
		t.Error("a árvore ativa não foi substituída")
	}
	if app.Scanner.Tree != app.Tree {
		t.Error("Scanner.Tree continuou apontando para a árvore antiga (achado C4)")
	}
	if app.MCPContext != nil && app.MCPContext.Tree != app.Tree {
		t.Error("MCPContext.Tree continuou apontando para a árvore antiga")
	}

	// O Assistente recebe as Raízes Varridas do Snapshot (contrato 1.11).
	if got := app.MCPContext.AllowedRoots(); len(got) != 1 || got[0] != root {
		t.Errorf("raízes permitidas do Assistente = %v, esperado [%s]", got, root)
	}
}

func TestRestoreAutoSaveWithoutFileReturns404(t *testing.T) {
	_, ts := newScanTestServer(t)

	resp := postJSON(t, ts, "/api/cache/autosave/restore", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("restore sem Autosave devolveu %d, esperado 404", resp.StatusCode)
	}
}

func TestLoadCacheReturnsSummaryNotFileList(t *testing.T) {
	app, ts := newScanTestServer(t)

	root := tempDir(t)
	tm := buildSnapshotTree(t, root)
	target := filepath.Join(app.savedScansDir, "manual.scanfile.gz")
	if err := scanner.SaveCacheToFile(tm, []string{root}, scanner.ScanConfig{Roots: []string{root}}, target); err != nil {
		t.Fatalf("não foi possível gravar o Snapshot de teste: %v", err)
	}

	resp := postJSON(t, ts, "/api/cache/load", LoadCacheReq{FilePath: target})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("load devolveu %d, esperado 200", resp.StatusCode)
	}

	var body map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("load devolveu JSON inválido: %v", err)
	}
	if _, ok := body["snapshot"]; ok {
		t.Error("load não pode mais devolver o campo snapshot")
	}

	var summary scanner.CacheSnapshotSummary
	if err := json.Unmarshal(body["summary"], &summary); err != nil {
		t.Fatalf("summary inválido: %v", err)
	}
	if summary.TotalFiles != 3 {
		t.Errorf("totalFiles = %d, esperado 3", summary.TotalFiles)
	}
	if app.Scanner.Tree != app.Tree {
		t.Error("Scanner.Tree continuou apontando para a árvore antiga (achado C4)")
	}
	if got := app.MCPContext.AllowedRoots(); len(got) != 1 || got[0] != root {
		t.Errorf("raízes permitidas do Assistente = %v, esperado [%s]", got, root)
	}
}

func TestLoadCacheDuringScanReturns409(t *testing.T) {
	app, ts := newScanTestServer(t)

	reached := make(chan struct{})
	release := make(chan struct{})
	app.scanBarrier = testBarrier(app, func() {
		close(reached)
		<-release
	})

	root := scanRootWithFiles(t, 2)
	resp := postJSON(t, ts, "/api/scan/start", scanner.ScanConfig{Roots: []string{root}, WorkerThreads: 1, LogDir: tempDir(t)})
	resp.Body.Close()
	<-reached

	conflict := postJSON(t, ts, "/api/cache/load", LoadCacheReq{FilePath: filepath.Join(app.savedScansDir, "x.sfz")})
	defer conflict.Body.Close()
	if conflict.StatusCode != http.StatusConflict {
		t.Errorf("load durante a Varredura devolveu %d, esperado 409", conflict.StatusCode)
	}

	close(release)
	waitScanIdle(t, app, 10*time.Second)
	// O Monitoramento segura um descritor da raiz temporária; desligá-lo aqui
	// deixa a limpeza do t.TempDir apagar a pasta.
	app.StopBackground()
}
