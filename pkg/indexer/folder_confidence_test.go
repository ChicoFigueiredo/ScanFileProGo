package indexer

import (
	"testing"

	"scanfile/pkg/scanner"
)

// buildTwinFolders creates two folders with the same layout. When hashed is
// false the files carry no hash, so the Merkle falls back to size + modTime.
func buildTwinFolders(hashed bool) *scanner.TreeManager {
	tm := scanner.NewTreeManager()
	hashA, hashB := "", ""
	if hashed {
		hashA, hashB = "xxh64:111111", "xxh64:222222"
	}
	tm.AddFile(scanner.NewFileNodeAt("C:\\FolderA\\doc.txt", scanner.FileMeta{Name: "doc.txt", Size: 100, ModTime: 7, Hash: hashA}))
	tm.AddFile(scanner.NewFileNodeAt("C:\\FolderA\\sub\\img.png", scanner.FileMeta{Name: "img.png", Size: 500, ModTime: 8, Hash: hashB}))
	tm.AddFile(scanner.NewFileNodeAt("C:\\FolderB\\doc.txt", scanner.FileMeta{Name: "doc.txt", Size: 100, ModTime: 7, Hash: hashA}))
	tm.AddFile(scanner.NewFileNodeAt("C:\\FolderB\\sub\\img.png", scanner.FileMeta{Name: "img.png", Size: 500, ModTime: 8, Hash: hashB}))
	return tm
}

func TestFolderIndex_ConfidenceHashWhenEveryFileIsHashed(t *testing.T) {
	tm := buildTwinFolders(true)
	fidx := NewFolderDuplicateIndex()
	fidx.RebuildFolderIndex(tm)

	res := fidx.Query(FolderQueryFilter{})
	if res.TotalGroups == 0 {
		t.Fatal("esperava ao menos 1 grupo de pastas clones")
	}
	for _, grp := range res.Groups {
		if grp.Confidence != ConfidenceHash {
			t.Fatalf("grupo %s deveria ter confiança hash, obtive %q", grp.ID, grp.Confidence)
		}
		for _, folder := range grp.Folders {
			if !folder.AllFilesHashed {
				t.Fatalf("pasta %s deveria estar marcada como totalmente hasheada", folder.Path)
			}
		}
	}
}

func TestFolderIndex_ConfidenceFallsBackWithoutHashes(t *testing.T) {
	tm := buildTwinFolders(false)
	fidx := NewFolderDuplicateIndex()
	fidx.RebuildFolderIndex(tm)

	res := fidx.Query(FolderQueryFilter{})
	if res.TotalGroups == 0 {
		t.Fatal("esperava agrupamento por tamanho+data mesmo sem hash")
	}
	for _, grp := range res.Groups {
		if grp.Confidence != ConfidenceSizeMTime {
			t.Fatalf("grupo %s sem hashes deveria ter confiança size_mtime, obtive %q", grp.ID, grp.Confidence)
		}
		for _, folder := range grp.Folders {
			if folder.AllFilesHashed {
				t.Fatalf("pasta %s não deveria estar marcada como hasheada", folder.Path)
			}
		}
	}
}

func TestFolderIndex_PartiallyHashedFolderIsNotFullyHashed(t *testing.T) {
	tm := scanner.NewTreeManager()
	tm.AddFile(scanner.NewFileNodeAt("C:\\Mix\\a.txt", scanner.FileMeta{Name: "a.txt", Size: 10, ModTime: 1, Hash: "xxh64:aaaa"}))
	tm.AddFile(scanner.NewFileNodeAt("C:\\Mix\\sub\\b.txt", scanner.FileMeta{Name: "b.txt", Size: 20, ModTime: 2, Hash: ""}))

	fidx := NewFolderDuplicateIndex()
	fidx.RebuildFolderIndex(tm)

	summary := FolderSummaryOf(tm.FindDir("C:\\Mix"))
	if summary == nil {
		t.Fatal("esperava resumo da pasta")
	}
	if summary.AllFilesHashed {
		t.Fatal("pasta com um arquivo sem hash não pode ser marcada como totalmente hasheada")
	}
	sub := FolderSummaryOf(tm.FindDir("C:\\Mix\\sub"))
	if sub == nil || sub.AllFilesHashed {
		t.Fatal("subpasta sem hash não pode ser marcada como totalmente hasheada")
	}
}

func TestCompareFolders_Is100PercentMatchOnlyWithHashConfidence(t *testing.T) {
	// With hashes: a real 100% match.
	tmHashed := buildTwinFolders(true)
	comp, err := CompareFolders(tmHashed, "C:\\FolderA", "C:\\FolderB")
	if err != nil {
		t.Fatalf("CompareFolders falhou: %v", err)
	}
	if comp.Confidence != ConfidenceHash {
		t.Fatalf("esperava confiança hash, obtive %q", comp.Confidence)
	}
	if !comp.Is100PercentMatch {
		t.Fatal("pastas idênticas com hash deveriam ser 100%% iguais")
	}

	// Without hashes: same size and modTime is not proof of identical content.
	tmPlain := buildTwinFolders(false)
	comp, err = CompareFolders(tmPlain, "C:\\FolderA", "C:\\FolderB")
	if err != nil {
		t.Fatalf("CompareFolders falhou: %v", err)
	}
	if comp.Confidence != ConfidenceSizeMTime {
		t.Fatalf("esperava confiança size_mtime, obtive %q", comp.Confidence)
	}
	if comp.Is100PercentMatch {
		t.Fatal("achado M14: sem hash não se pode afirmar 100%% idêntica")
	}
	if comp.IdenticalCount != 2 {
		t.Fatalf("esperava 2 arquivos equivalentes por tamanho+data, obtive %d", comp.IdenticalCount)
	}
}

func TestFolderIndex_MarkDirtyAndRebuildIfDirty(t *testing.T) {
	tm := buildTwinFolders(true)
	fidx := NewFolderDuplicateIndex()

	if fidx.IsDirty() {
		t.Fatal("índice recém-criado não deveria estar sujo")
	}
	// A brand new index has nothing indexed until the first rebuild.
	if g, _, _ := fidx.GetSummaryStats(); g != 0 {
		t.Fatalf("esperava índice vazio, obtive %d grupos", g)
	}

	fidx.MarkDirty()
	if !fidx.IsDirty() {
		t.Fatal("MarkDirty deveria marcar o índice como sujo")
	}

	if !fidx.RebuildIfDirty(tm) {
		t.Fatal("RebuildIfDirty deveria reconstruir quando sujo")
	}
	if fidx.IsDirty() {
		t.Fatal("RebuildIfDirty deveria limpar a marca")
	}
	if g, _, _ := fidx.GetSummaryStats(); g == 0 {
		t.Fatal("esperava grupos após reconstrução")
	}

	// A second call without changes must not rebuild again.
	if fidx.RebuildIfDirty(tm) {
		t.Fatal("RebuildIfDirty não deveria reconstruir sem mudanças")
	}
}
