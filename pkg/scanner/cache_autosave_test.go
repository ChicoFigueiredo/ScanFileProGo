package scanner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// cache_autosave_test.go cobre as duas falhas de gravação apontadas na revisão:
// o temporário fixo compartilhado por gravações concorrentes e a listagem que
// mostra um temporário órfão como se fosse um Snapshot do usuário.

// arvoreAutoSave monta uma árvore pequena porém não trivial, para que o
// Snapshot tenha conteúdo suficiente para a corrupção aparecer.
func arvoreAutoSave(t *testing.T, arquivos int) (*TreeManager, ScanConfig) {
	t.Helper()
	tm := NewTreeManager()
	tm.GetOrCreateRoot(`C:\`)
	batch := make([]*FileNode, arquivos)
	for i := range batch {
		batch[i] = NewFileNode(FileMeta{
			Name:          fmt.Sprintf("arquivo_%05d.bin", i),
			Size:          int64(1000 + i),
			AllocatedSize: int64(1000 + i),
			ModTime:       1_700_000_000 + int64(i),
			Extension:     ".bin",
			Hash:          fmt.Sprintf("xxh64:%016x", uint64(i)+1),
		})
	}
	tm.FastSetDir(`C:\AutoSave`, batch, nil)
	return tm, ScanConfig{Roots: []string{`C:\`}, HashAlgorithm: "xxhash"}
}

// TestSaveAutoSaveConcorrenteNaoTrunca prova que duas gravações simultâneas não
// escrevem no mesmo arquivo temporário. Com o temporário fixo antigo, uma
// gravação trunca a outra e o autosave_latest.sfz resultante é um gzip cortado.
func TestSaveAutoSaveConcorrenteNaoTrunca(t *testing.T) {
	dir := t.TempDir()
	tm, config := arvoreAutoSave(t, 3000)

	const gravacoes = 6
	var wg sync.WaitGroup
	erros := make([]error, gravacoes)
	for i := 0; i < gravacoes; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, erros[i] = SaveAutoSave(tm, []string{`C:\`}, config, dir)
		}()
	}
	wg.Wait()

	for i, err := range erros {
		if err != nil {
			t.Fatalf("gravação %d falhou: %v", i, err)
		}
	}

	// Tanto o snapshot ativo quanto o backup rotacionado precisam ser
	// snapshots completos, nunca um gzip pela metade.
	for _, nome := range []string{DefaultAutoSaveFileName, BackupAutoSaveFileName} {
		caminho := filepath.Join(dir, nome)
		if _, err := os.Stat(caminho); os.IsNotExist(err) {
			continue
		}
		_, resumo, err := LoadCacheSummaryFromFile(caminho, nil)
		if err != nil {
			t.Fatalf("%s ficou corrompido: %v", nome, err)
		}
		if resumo.TotalFiles != 3000 {
			t.Fatalf("%s tem %d arquivos, esperado 3000", nome, resumo.TotalFiles)
		}
	}

	// Nenhum temporário pode sobrar, e nenhum deles pode usar a extensão de
	// Snapshot.
	entradas, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entradas {
		nome := e.Name()
		if nome != DefaultAutoSaveFileName && nome != BackupAutoSaveFileName {
			t.Errorf("sobrou o arquivo %q na pasta de autosave", nome)
		}
	}
}

// TestListSavedCachesIgnoraTemporarios prova que um temporário órfão — herdado
// de uma queda de uma versão anterior, que usava autosave_temp.sfz — não é
// oferecido ao usuário como Snapshot.
func TestListSavedCachesIgnoraTemporarios(t *testing.T) {
	dir := t.TempDir()
	tm, config := arvoreAutoSave(t, 10)

	if _, err := SaveAutoSave(tm, []string{`C:\`}, config, dir); err != nil {
		t.Fatalf("SaveAutoSave: %v", err)
	}
	if err := SaveCacheToFile(tm, []string{`C:\`}, config, filepath.Join(dir, "meu_scan.sfz")); err != nil {
		t.Fatalf("SaveCacheToFile: %v", err)
	}

	// Órfãos: o nome fixo antigo e um temporário da forma nova.
	orfaos := []string{
		legacyAutoSaveTempName,
		fmt.Sprintf("autosave_%d_1%s", os.Getpid(), autoSaveTempExt),
	}
	for _, nome := range orfaos {
		if err := os.WriteFile(filepath.Join(dir, nome), []byte("gzip pela metade"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	lista, err := ListSavedCaches(dir)
	if err != nil {
		t.Fatalf("ListSavedCaches: %v", err)
	}

	vistos := make(map[string]bool, len(lista))
	for _, item := range lista {
		vistos[item.FileName] = true
		for _, nome := range orfaos {
			if item.FileName == nome {
				t.Errorf("temporário %q apareceu como Snapshot", nome)
			}
		}
		if strings.HasSuffix(item.FileName, autoSaveTempExt) {
			t.Errorf("temporário %q apareceu como Snapshot", item.FileName)
		}
	}
	if !vistos[DefaultAutoSaveFileName] {
		t.Errorf("o autosave ativo sumiu da lista: %+v", lista)
	}
	if !vistos["meu_scan.sfz"] {
		t.Errorf("o Snapshot do usuário sumiu da lista: %+v", lista)
	}
}
