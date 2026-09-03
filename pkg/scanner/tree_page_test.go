package scanner

import (
	"fmt"
	"sync"
	"testing"
)

func TestTreeManager_ChangeCounter(t *testing.T) {
	tm := NewTreeManager()
	if got := tm.ChangeCounter(); got != 0 {
		t.Fatalf("contador inicial = %d, quer 0", got)
	}

	tm.GetOrCreateRoot(`C:\`)
	afterRoot := tm.ChangeCounter()

	tm.EnsureDirNode(`C:\a\b`)
	afterEnsure := tm.ChangeCounter()
	if afterEnsure <= afterRoot {
		t.Errorf("EnsureDirNode deveria avançar o contador (%d -> %d)", afterRoot, afterEnsure)
	}

	tm.AddFile(&FileNode{Path: `C:\a\b\f.txt`, Name: "f.txt", Size: 10})
	afterAdd := tm.ChangeCounter()
	if afterAdd <= afterEnsure {
		t.Errorf("AddFile deveria avançar o contador (%d -> %d)", afterEnsure, afterAdd)
	}

	tm.FastSetDir(`C:\a\c`, []*FileNode{{Path: `C:\a\c\g.txt`, Name: "g.txt", Size: 5}}, []string{"d"})
	afterFast := tm.ChangeCounter()
	if afterFast <= afterAdd {
		t.Errorf("FastSetDir deveria avançar o contador (%d -> %d)", afterAdd, afterFast)
	}

	if _, ok := tm.RemoveFile(`C:\a\b\f.txt`); !ok {
		t.Fatal("RemoveFile não encontrou o arquivo")
	}
	afterRemove := tm.ChangeCounter()
	if afterRemove <= afterFast {
		t.Errorf("RemoveFile deveria avançar o contador (%d -> %d)", afterFast, afterRemove)
	}

	// Remoção de arquivo inexistente não conta como mudança.
	if _, ok := tm.RemoveFile(`C:\a\b\inexistente.txt`); ok {
		t.Fatal("RemoveFile deveria falhar para arquivo inexistente")
	}
	if tm.ChangeCounter() != afterRemove {
		t.Error("remoção sem efeito não deveria avançar o contador")
	}
}

func TestTreeManager_ChangeCounterIsConcurrencySafe(t *testing.T) {
	tm := NewTreeManager()
	tm.GetOrCreateRoot(`C:\`)

	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				tm.AddFile(&FileNode{
					Path: fmt.Sprintf(`C:\conc\w%d\f%d.txt`, w, i),
					Name: fmt.Sprintf("f%d.txt", i),
					Size: 1,
				})
			}
		}(w)
	}
	wg.Wait()

	if got := tm.ChangeCounter(); got < 800 {
		t.Errorf("contador = %d, esperava ao menos 800 mudanças", got)
	}
}

func buildPageTree(t *testing.T, n int) *TreeManager {
	t.Helper()
	tm := NewTreeManager()
	tm.GetOrCreateRoot(`C:\`)
	for i := 0; i < n; i++ {
		tm.AddFile(&FileNode{
			Path:      fmt.Sprintf(`C:\dados\f%04d.bin`, i),
			Name:      fmt.Sprintf("f%04d.bin", i),
			Size:      int64(i + 1),
			ModTime:   int64(1_700_000_000 + i),
			Extension: ".bin",
		})
	}
	tm.ComputeAggregatedSizes()
	return tm
}

func TestTreeManager_GetFilesPage(t *testing.T) {
	tm := buildPageTree(t, 1200)

	total, files := tm.GetFilesPage(`C:\dados`, 0, 100, "size_desc")
	if total != 1200 {
		t.Fatalf("total = %d, quer 1200", total)
	}
	if len(files) != 100 {
		t.Fatalf("página com %d arquivos, quer 100", len(files))
	}
	if files[0].Size != 1200 || files[99].Size != 1101 {
		t.Errorf("ordenação size_desc errada: primeiro=%d último=%d", files[0].Size, files[99].Size)
	}

	// Segunda página continua de onde a primeira parou.
	_, page2 := tm.GetFilesPage(`C:\dados`, 100, 100, "size_desc")
	if len(page2) != 100 || page2[0].Size != 1100 {
		t.Errorf("segunda página inesperada: len=%d primeiro=%d", len(page2), page2[0].Size)
	}

	// name_asc
	_, byName := tm.GetFilesPage(`C:\dados`, 0, 3, "name_asc")
	if byName[0].Name != "f0000.bin" || byName[2].Name != "f0002.bin" {
		t.Errorf("ordenação name_asc errada: %v", []string{byName[0].Name, byName[1].Name, byName[2].Name})
	}

	// mod_desc
	_, byMod := tm.GetFilesPage(`C:\dados`, 0, 2, "mod_desc")
	if byMod[0].ModTime < byMod[1].ModTime {
		t.Errorf("ordenação mod_desc errada: %d < %d", byMod[0].ModTime, byMod[1].ModTime)
	}

	// Ordenação desconhecida cai em size_desc.
	_, fallback := tm.GetFilesPage(`C:\dados`, 0, 1, "inexistente")
	if fallback[0].Size != 1200 {
		t.Errorf("ordenação desconhecida deveria cair em size_desc, obtive %d", fallback[0].Size)
	}
}

func TestTreeManager_GetFilesPageBoundaries(t *testing.T) {
	tm := buildPageTree(t, 10)

	if total, files := tm.GetFilesPage(`C:\dados`, 50, 10, "size_desc"); total != 10 || len(files) != 0 {
		t.Errorf("offset além do fim: total=%d len=%d", total, len(files))
	}
	if total, files := tm.GetFilesPage(`C:\dados`, -5, 0, "size_desc"); total != 10 || len(files) != 10 {
		t.Errorf("offset negativo e limit 0 deveriam devolver a lista inteira até o teto: total=%d len=%d", total, len(files))
	}
	if _, files := tm.GetFilesPage(`C:\dados`, 0, 5000, "size_desc"); len(files) != 10 {
		t.Errorf("limit acima do teto deveria ser aparado ao total: len=%d", len(files))
	}
	if total, files := tm.GetFilesPage(`C:\inexistente`, 0, 10, "size_desc"); total != 0 || files != nil {
		t.Errorf("pasta inexistente deveria devolver 0/nil, obtive %d/%v", total, files)
	}
	if _, files := tm.GetFilesPage(`C:\dados`, 8, 100, "size_desc"); len(files) != 2 {
		t.Errorf("última página deveria ter 2 arquivos, obtive %d", len(files))
	}
}

func TestTreeManager_GetFilesPageMaxLimit(t *testing.T) {
	tm := buildPageTree(t, 900)
	_, files := tm.GetFilesPage(`C:\dados`, 0, 900, "size_desc")
	if len(files) != MaxFilesPageLimit {
		t.Errorf("limit deveria ser aparado em %d, obtive %d", MaxFilesPageLimit, len(files))
	}
}

func TestTreeManager_GetDirSummaryCapsFiles(t *testing.T) {
	tm := buildPageTree(t, 900)

	summary := tm.GetDirSummary(`C:\dados`, 1)
	if summary == nil {
		t.Fatal("esperava resumo")
	}
	if len(summary.Files) != DefaultSummaryMaxFiles {
		t.Errorf("resumo trouxe %d arquivos, quer o teto de %d", len(summary.Files), DefaultSummaryMaxFiles)
	}
	if summary.DirectFileCount != 900 {
		t.Errorf("DirectFileCount = %d, quer 900", summary.DirectFileCount)
	}
	// Os maiores primeiro.
	if summary.Files[0].Size != 900 {
		t.Errorf("o resumo deveria trazer os maiores: primeiro = %d", summary.Files[0].Size)
	}

	custom := tm.GetDirSummary(`C:\dados`, 1, 10)
	if len(custom.Files) != 10 {
		t.Errorf("maxFiles=10 deveria devolver 10 arquivos, obtive %d", len(custom.Files))
	}
	if custom.DirectFileCount != 900 {
		t.Errorf("DirectFileCount = %d, quer 900 mesmo com a lista aparada", custom.DirectFileCount)
	}
}
