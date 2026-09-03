package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"scanfile/pkg/scanner"
)

// buildAutosave creates a real autosave_latest.sfz from a small synthetic tree.
func buildAutosave(t *testing.T) (autosaveDir string, root string) {
	t.Helper()

	root = t.TempDir()
	autosaveDir = t.TempDir()

	tm := scanner.NewTreeManager()
	tm.GetOrCreateRoot(root)

	sub := filepath.Join(root, "docs")
	tm.EnsureDirNode(sub)

	now := time.Now().Unix()
	// Dois arquivos com o mesmo hash formam um Grupo de Duplicados.
	files := []*scanner.FileNode{
		{Path: filepath.Join(root, "a.pdf"), Name: "a.pdf", Size: 2048, Extension: ".pdf", ModTime: now, Hash: "xxh64:aaaa"},
		{Path: filepath.Join(sub, "b.pdf"), Name: "b.pdf", Size: 2048, Extension: ".pdf", ModTime: now, Hash: "xxh64:aaaa"},
		{Path: filepath.Join(sub, "c.txt"), Name: "c.txt", Size: 10, Extension: ".txt", ModTime: now},
	}
	for _, f := range files {
		tm.AddFile(f)
	}
	tm.ComputeAggregatedSizes()

	cfg := scanner.ScanConfig{Roots: []string{root}, HashAlgorithm: "xxhash"}
	if _, err := scanner.SaveAutoSave(tm, []string{root}, cfg, autosaveDir); err != nil {
		t.Fatalf("falha ao gravar autosave: %v", err)
	}

	if _, err := os.Stat(filepath.Join(autosaveDir, scanner.DefaultAutoSaveFileName)); err != nil {
		t.Fatalf("autosave_latest.sfz não foi criado: %v", err)
	}

	return autosaveDir, root
}

func TestNewMCPToolsContextFromAutosave(t *testing.T) {
	autosaveDir, root := buildAutosave(t)

	tc, snapshot, err := NewMCPToolsContextFromAutosave(autosaveDir, nil, "")
	if err != nil {
		t.Fatalf("carregar contexto do autosave falhou: %v", err)
	}
	if snapshot == nil {
		t.Fatal("o resumo do Snapshot deveria ser devolvido")
	}
	if snapshot.TotalFiles != 3 {
		t.Fatalf("esperados 3 arquivos no Snapshot, obtido %d", snapshot.TotalFiles)
	}
	if len(snapshot.Roots) != 1 || !strings.EqualFold(snapshot.Roots[0], root) {
		t.Fatalf("raízes inesperadas no Snapshot: %v", snapshot.Roots)
	}

	if tc.Tree == nil || tc.Index == nil || tc.FolderIndex == nil {
		t.Fatal("árvore e índices deveriam ser reconstruídos")
	}
	if got := tc.Tree.GetTotalFileCount(); got != 3 {
		t.Fatalf("árvore reconstruída com %d arquivos", got)
	}

	groups, dupFiles, _ := tc.Index.GetSummaryStats()
	if groups != 1 || dupFiles != 2 {
		t.Fatalf("índice de duplicados esperado 1 grupo com 2 arquivos, obtido %d/%d", groups, dupFiles)
	}

	// As Raízes Varridas vêm do Snapshot, então as ferramentas de leitura já funcionam.
	roots := tc.AllowedRoots()
	if len(roots) != 1 || !strings.EqualFold(roots[0], root) {
		t.Fatalf("SetAllowedRoots deveria ter sido aplicado a partir do Snapshot: %v", roots)
	}

	items, err := tc.ClassifyFiles(context.Background(), ClassifyFilesParams{Limit: 10})
	if err != nil {
		t.Fatalf("classify_files deveria funcionar no contexto do autosave: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("esperados 3 itens classificados, obtido %d", len(items))
	}
}

func TestNewMCPToolsContextFromAutosave_SemArquivo(t *testing.T) {
	dir := t.TempDir()
	_, _, err := NewMCPToolsContextFromAutosave(dir, nil, "")
	if err == nil {
		t.Fatal("esperado erro quando não há autosave na pasta")
	}
	if !strings.Contains(err.Error(), "Autosave") && !strings.Contains(err.Error(), "autosave") {
		t.Fatalf("mensagem em pt-BR deveria mencionar o Autosave: %v", err)
	}
}
