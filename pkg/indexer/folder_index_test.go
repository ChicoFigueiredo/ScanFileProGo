package indexer

import (
	"testing"
	"time"

	"scanfile/pkg/scanner"
)

func TestFolderIndex_DuplicateFoldersAndCompare(t *testing.T) {
	tm := scanner.NewTreeManager()
	tm.GetOrCreateRoot("C:\\")

	// Create Folder A
	fA1 := scanner.NewFileNodeAt("C:\\FolderA\\doc.txt", scanner.FileMeta{Name: "doc.txt", Size: 100, Hash: "xxh64:111111", Extension: ".txt"})
	fA2 := scanner.NewFileNodeAt("C:\\FolderA\\sub\\image.png", scanner.FileMeta{Name: "image.png", Size: 500, Hash: "xxh64:222222", Extension: ".png"})

	// Create Folder B (Exact identical clone of Folder A)
	fB1 := scanner.NewFileNodeAt("C:\\FolderB\\doc.txt", scanner.FileMeta{Name: "doc.txt", Size: 100, Hash: "xxh64:111111", Extension: ".txt"})
	fB2 := scanner.NewFileNodeAt("C:\\FolderB\\sub\\image.png", scanner.FileMeta{Name: "image.png", Size: 500, Hash: "xxh64:222222", Extension: ".png"})

	// Create Folder C (Different content)
	fC1 := scanner.NewFileNodeAt("C:\\FolderC\\doc.txt", scanner.FileMeta{Name: "doc.txt", Size: 200, Hash: "xxh64:333333", Extension: ".txt"})

	tm.AddFile(fA1)
	tm.AddFile(fA2)
	tm.AddFile(fB1)
	tm.AddFile(fB2)
	tm.AddFile(fC1)

	folderIdx := NewFolderDuplicateIndex()
	folderIdx.RebuildFolderIndex(tm)

	res := folderIdx.Query(FolderQueryFilter{})
	if res.TotalGroups == 0 {
		t.Fatalf("expected at least 1 duplicate folder group")
	}

	grpCount, folderCount, wasted := folderIdx.GetSummaryStats()
	if grpCount != 2 || folderCount != 4 {
		t.Errorf("expected 2 groups with 4 folders (FolderA/B and sub/sub), got grpCount=%d, folderCount=%d", grpCount, folderCount)
	}
	if wasted != 1100 {
		t.Errorf("expected 1100 wasted bytes (600 + 500), got %d", wasted)
	}

	// Compare Folder A and Folder B (Should be 100% match)
	compAB, err := CompareFolders(tm, "C:\\FolderA", "C:\\FolderB")
	if err != nil {
		t.Fatalf("CompareFolders A-B failed: %v", err)
	}
	if !compAB.Is100PercentMatch {
		t.Errorf("expected Folder A and Folder B to be 100%% match")
	}
	if compAB.IdenticalCount != 2 {
		t.Errorf("expected 2 identical files, got %d", compAB.IdenticalCount)
	}

	// Compare Folder A and Folder C (Should NOT match)
	compAC, err := CompareFolders(tm, "C:\\FolderA", "C:\\FolderC")
	if err != nil {
		t.Fatalf("CompareFolders A-C failed: %v", err)
	}
	if compAC.Is100PercentMatch {
		t.Errorf("expected Folder A and Folder C to NOT match")
	}
	if compAC.ModifiedCount != 1 || compAC.OnlyInACount != 1 {
		t.Errorf("expected 1 modified and 1 onlyInA, got mod=%d onlyInA=%d", compAC.ModifiedCount, compAC.OnlyInACount)
	}
}

// treeParaReconstrucao monta uma árvore mínima com duas Pastas Clones.
func treeParaReconstrucao(t *testing.T) *scanner.TreeManager {
	t.Helper()
	tm := scanner.NewTreeManager()
	tm.GetOrCreateRoot("C:\\")
	tm.AddFile(scanner.NewFileNodeAt("C:\\PastaA\\doc.txt", scanner.FileMeta{Name: "doc.txt", Size: 100, Hash: "xxh64:1111"}))
	tm.AddFile(scanner.NewFileNodeAt("C:\\PastaB\\doc.txt", scanner.FileMeta{Name: "doc.txt", Size: 100, Hash: "xxh64:1111"}))
	return tm
}

// Uma marcação levantada enquanto a reconstrução está em curso — aqui, enquanto
// ela espera o lock que uma consulta longa segura — não pode ser engolida: o
// sinalizador só é limpo antes de ler a árvore, nunca depois. Se for engolido, a
// visão de Pastas Clones fica velha até alguém marcar de novo.
func TestFolderIndex_MarcacaoDuranteReconstrucaoNaoEhEngolida(t *testing.T) {
	tm := treeParaReconstrucao(t)
	fidx := NewFolderDuplicateIndex()
	fidx.MarkDirty()

	// Segura o índice como uma consulta longa (ou outra reconstrução) faria.
	fidx.mu.Lock()

	comecou := make(chan struct{})
	terminou := make(chan bool, 1)
	go func() {
		close(comecou)
		terminou <- fidx.RebuildIfDirty(tm)
	}()
	<-comecou
	// A goroutine já entrou em RebuildIfDirty e está parada no lock.
	time.Sleep(100 * time.Millisecond)

	// O Monitoramento vê uma mudança enquanto a reconstrução está em curso.
	fidx.MarkDirty()
	fidx.mu.Unlock()

	if !<-terminou {
		t.Fatal("a reconstrução deveria ter acontecido")
	}
	if !fidx.IsDirty() {
		t.Fatal("a marcação levantada durante a reconstrução foi engolida: o índice de Pastas Clones fica velho")
	}

	// E a próxima chamada realmente reconstrói, em vez de achar que está limpa.
	if !fidx.RebuildIfDirty(tm) {
		t.Fatal("a marcação preservada deveria disparar uma nova reconstrução")
	}
	if fidx.IsDirty() {
		t.Fatal("depois da reconstrução seguinte o índice deveria estar limpo")
	}
}

// Uma marcação levantada durante a caminhada da árvore também sobrevive.
func TestFolderIndex_MarcacaoDuranteCaminhadaSobrevive(t *testing.T) {
	tm := treeParaReconstrucao(t)
	fidx := NewFolderDuplicateIndex()

	// Fura a caminhada segurando a raiz da árvore: a reconstrução para dentro de
	// GetRootsSnapshot até o teste soltar.
	liberado := make(chan struct{})
	segurando := make(chan struct{})
	go func() {
		tm.RootsLock(func(map[string]*scanner.DirNode) {
			close(segurando)
			<-liberado
		})
	}()
	<-segurando

	terminou := make(chan struct{})
	go func() {
		defer close(terminou)
		fidx.RebuildFolderIndex(tm)
	}()

	time.Sleep(100 * time.Millisecond)
	fidx.MarkDirty()
	close(liberado)
	<-terminou

	if !fidx.IsDirty() {
		t.Fatal("marcação levantada durante a caminhada foi engolida")
	}
}
