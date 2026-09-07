package indexer

import (
	"testing"

	"scanfile/pkg/scanner"
)

func statsOf(t *testing.T, idx *DuplicateIndex) (int, int, int64) {
	t.Helper()
	g, f, w := idx.GetSummaryStats()
	return g, f, w
}

func TestDuplicateIndex_UpsertBuildsGroupsIncrementally(t *testing.T) {
	idx := NewDuplicateIndex()

	a := scanner.NewFileNodeAt("C:\\a\\one.bin", scanner.FileMeta{Name: "one.bin", Size: 100, ModTime: 10, Hash: "xxh64:aaaa"})
	b := scanner.NewFileNodeAt("C:\\b\\two.bin", scanner.FileMeta{Name: "two.bin", Size: 100, ModTime: 20, Hash: "xxh64:aaaa"})
	c := scanner.NewFileNodeAt("C:\\c\\three.bin", scanner.FileMeta{Name: "three.bin", Size: 100, ModTime: 30, Hash: "xxh64:aaaa"})

	idx.UpsertFile(a)
	if g, f, w := statsOf(t, idx); g != 0 || f != 0 || w != 0 {
		t.Fatalf("um arquivo único não pode formar grupo: %d/%d/%d", g, f, w)
	}

	idx.UpsertFile(b)
	if g, f, w := statsOf(t, idx); g != 1 || f != 2 || w != 100 {
		t.Fatalf("esperava 1 grupo/2 arquivos/100 desperdiçados, obtive %d/%d/%d", g, f, w)
	}

	idx.UpsertFile(c)
	if g, f, w := statsOf(t, idx); g != 1 || f != 3 || w != 200 {
		t.Fatalf("esperava 1 grupo/3 arquivos/200 desperdiçados, obtive %d/%d/%d", g, f, w)
	}

	// Files inside the group stay sorted by ModTime ascending (oldest first).
	res := idx.Query(QueryFilter{})
	if len(res.Groups) != 1 || len(res.Groups[0].Files) != 3 {
		t.Fatalf("consulta inesperada: %+v", res)
	}
	if res.Groups[0].Files[0].Path() != a.Path() || res.Groups[0].Files[2].Path() != c.Path() {
		t.Fatalf("grupo fora de ordem por ModTime: %v", res.Groups[0].Files)
	}
	if res.Groups[0].Confidence != ConfidenceHash {
		t.Fatalf("grupo de arquivos deve ter confiança hash, obtive %q", res.Groups[0].Confidence)
	}
}

func TestDuplicateIndex_UpsertReplacesSameFileWithoutDoubleCounting(t *testing.T) {
	idx := NewDuplicateIndex()
	idx.UpsertFile(scanner.NewFileNodeAt("C:\\a\\x.bin", scanner.FileMeta{Size: 100, ModTime: 1, Hash: "xxh64:aaaa"}))
	idx.UpsertFile(scanner.NewFileNodeAt("C:\\b\\x.bin", scanner.FileMeta{Size: 100, ModTime: 2, Hash: "xxh64:aaaa"}))

	// The very same path is re-upserted (a rewrite that produced the same content).
	idx.UpsertFile(scanner.NewFileNodeAt("C:\\b\\x.bin", scanner.FileMeta{Size: 100, ModTime: 5, Hash: "xxh64:aaaa"}))
	if g, f, w := statsOf(t, idx); g != 1 || f != 2 || w != 100 {
		t.Fatalf("reinserção duplicou entradas: %d/%d/%d", g, f, w)
	}

	// Now the content changes: the file leaves the old group and becomes unique.
	idx.UpsertFile(scanner.NewFileNodeAt("C:\\b\\x.bin", scanner.FileMeta{Size: 250, ModTime: 6, Hash: "xxh64:bbbb"}))
	if g, f, w := statsOf(t, idx); g != 0 || f != 0 || w != 0 {
		t.Fatalf("grupo deveria ter se desfeito: %d/%d/%d", g, f, w)
	}

	// A twin of the new content re-forms a group with the new size.
	idx.UpsertFile(scanner.NewFileNodeAt("C:\\c\\y.bin", scanner.FileMeta{Size: 250, ModTime: 7, Hash: "xxh64:bbbb"}))
	if g, f, w := statsOf(t, idx); g != 1 || f != 2 || w != 250 {
		t.Fatalf("esperava novo grupo de 250 bytes, obtive %d/%d/%d", g, f, w)
	}
}

func TestDuplicateIndex_UpsertWithoutHashIsIgnored(t *testing.T) {
	idx := NewDuplicateIndex()
	idx.UpsertFile(scanner.NewFileNodeAt("C:\\a\\x.bin", scanner.FileMeta{Size: 100, Hash: "xxh64:aaaa"}))
	idx.UpsertFile(scanner.NewFileNodeAt("C:\\b\\x.bin", scanner.FileMeta{Size: 100, Hash: "xxh64:aaaa"}))
	if g, _, _ := statsOf(t, idx); g != 1 {
		t.Fatalf("esperava 1 grupo, obtive %d", g)
	}

	// A locked file comes back without a hash: it must leave the index instead of
	// pretending to still be a duplicate.
	idx.UpsertFile(scanner.NewFileNodeAt("C:\\b\\x.bin", scanner.FileMeta{Size: 100, Hash: ""}))
	if g, f, w := statsOf(t, idx); g != 0 || f != 0 || w != 0 {
		t.Fatalf("arquivo sem hash deveria sair do índice: %d/%d/%d", g, f, w)
	}
}

func TestDuplicateIndex_RemoveFileKeepsWastedBytesConsistent(t *testing.T) {
	idx := NewDuplicateIndex()
	files := []*scanner.FileNode{
		scanner.NewFileNodeAt("C:\\a\\1.bin", scanner.FileMeta{Size: 1000, ModTime: 1, Hash: "xxh64:aaaa"}),
		scanner.NewFileNodeAt("C:\\b\\2.bin", scanner.FileMeta{Size: 1000, ModTime: 2, Hash: "xxh64:aaaa"}),
		scanner.NewFileNodeAt("C:\\c\\3.bin", scanner.FileMeta{Size: 1000, ModTime: 3, Hash: "xxh64:aaaa"}),
		scanner.NewFileNodeAt("C:\\d\\4.bin", scanner.FileMeta{Size: 40, ModTime: 4, Hash: "xxh64:bbbb"}),
		scanner.NewFileNodeAt("C:\\e\\5.bin", scanner.FileMeta{Size: 40, ModTime: 5, Hash: "xxh64:bbbb"}),
	}
	idx.RebuildIndex(files)
	if g, f, w := statsOf(t, idx); g != 2 || f != 5 || w != 2040 {
		t.Fatalf("estado inicial inesperado: %d/%d/%d", g, f, w)
	}

	idx.RemoveFileFromIndex("C:\\c\\3.bin")
	if g, f, w := statsOf(t, idx); g != 2 || f != 4 || w != 1040 {
		t.Fatalf("após remover 1 de 3: %d/%d/%d", g, f, w)
	}

	// Dropping to a single survivor dissolves the group.
	idx.RemoveFileFromIndex("C:\\e\\5.bin")
	if g, f, w := statsOf(t, idx); g != 1 || f != 2 || w != 1000 {
		t.Fatalf("após dissolver o grupo pequeno: %d/%d/%d", g, f, w)
	}

	// The survivor is still known, so a new twin re-forms the group.
	idx.UpsertFile(scanner.NewFileNodeAt("C:\\f\\6.bin", scanner.FileMeta{Size: 40, ModTime: 6, Hash: "xxh64:bbbb"}))
	if g, f, w := statsOf(t, idx); g != 2 || f != 4 || w != 1040 {
		t.Fatalf("grupo não voltou a se formar: %d/%d/%d", g, f, w)
	}

	// Removing an unknown path is a no-op.
	idx.RemoveFileFromIndex("C:\\nao\\existe.bin")
	if g, f, w := statsOf(t, idx); g != 2 || f != 4 || w != 1040 {
		t.Fatalf("remoção de caminho desconhecido alterou o índice: %d/%d/%d", g, f, w)
	}
}

func TestDuplicateIndex_RemoveIsCaseInsensitive(t *testing.T) {
	idx := NewDuplicateIndex()
	idx.UpsertFile(scanner.NewFileNodeAt("C:\\Fotos\\IMG.JPG", scanner.FileMeta{Size: 10, Hash: "xxh64:aaaa"}))
	idx.UpsertFile(scanner.NewFileNodeAt("C:\\Backup\\img.jpg", scanner.FileMeta{Size: 10, Hash: "xxh64:aaaa"}))

	idx.RemoveFileFromIndex("c:\\fotos\\img.jpg")
	if g, f, _ := statsOf(t, idx); g != 0 || f != 0 {
		t.Fatalf("remoção case-insensitive falhou: %d/%d", g, f)
	}
}

func TestDuplicateIndex_QueryReturnsDetachedCopies(t *testing.T) {
	idx := NewDuplicateIndex()
	idx.UpsertFile(scanner.NewFileNodeAt("C:\\a\\1.bin", scanner.FileMeta{Size: 10, Hash: "xxh64:aaaa"}))
	idx.UpsertFile(scanner.NewFileNodeAt("C:\\b\\2.bin", scanner.FileMeta{Size: 10, Hash: "xxh64:aaaa"}))

	res := idx.Query(QueryFilter{})
	if len(res.Groups) != 1 {
		t.Fatalf("esperava 1 grupo, obtive %d", len(res.Groups))
	}
	grp := res.Groups[0]

	// Mutating the index afterwards must not touch the slice already handed out,
	// otherwise the JSON encoder in pkg/server races with Monitoramento.
	idx.UpsertFile(scanner.NewFileNodeAt("C:\\c\\3.bin", scanner.FileMeta{Size: 10, Hash: "xxh64:aaaa"}))
	if len(grp.Files) != 2 {
		t.Fatalf("consulta devolveu o grupo vivo do índice: %d arquivos", len(grp.Files))
	}
}

func TestDuplicateIndex_RebuildResetsFolderTracking(t *testing.T) {
	idx := NewDuplicateIndex()
	idx.RebuildIndex([]*scanner.FileNode{
		scanner.NewFileNodeAt("C:\\a\\1.bin", scanner.FileMeta{Size: 10, Hash: "xxh64:aaaa"}),
		scanner.NewFileNodeAt("C:\\b\\2.bin", scanner.FileMeta{Size: 10, Hash: "xxh64:aaaa"}),
	})
	idx.RebuildIndex([]*scanner.FileNode{
		scanner.NewFileNodeAt("C:\\x\\9.bin", scanner.FileMeta{Size: 70, Hash: "xxh64:cccc"}),
	})

	// Arquivos da construção anterior não podem sobrar no rastro por pasta.
	idx.RemoveFileFromIndex("C:\\a\\1.bin")
	idx.UpsertFile(scanner.NewFileNodeAt("C:\\y\\8.bin", scanner.FileMeta{Size: 70, Hash: "xxh64:cccc"}))
	if g, f, w := statsOf(t, idx); g != 1 || f != 2 || w != 70 {
		t.Fatalf("reconstrução deixou estado sujo: %d/%d/%d", g, f, w)
	}
}

// O Monitoramento tira o arquivo da árvore antes de avisar o índice, e os
// handlers tiram a pasta inteira antes de avisar. Como o índice guarda o próprio
// ponteiro do nó, e não uma consulta à árvore, a remoção continua funcionando
// depois que o nó já saiu de lá.
func TestDuplicateIndex_RemocaoFuncionaDepoisDeSairDaArvore(t *testing.T) {
	tm := scanner.NewTreeManager()
	tm.GetOrCreateRoot("C:\\")

	a := scanner.NewFileNode(scanner.FileMeta{Name: "copia.bin", Size: 500, ModTime: 1, Hash: "xxh64:aaaa"})
	b := scanner.NewFileNode(scanner.FileMeta{Name: "copia.bin", Size: 500, ModTime: 2, Hash: "xxh64:aaaa"})
	tm.AddFileAt("C:\\Origem", a)
	tm.AddFileAt("C:\\Backup", b)

	idx := NewDuplicateIndex()
	idx.RebuildIndex(tm.GetAllFiles())
	if g, f, w := statsOf(t, idx); g != 1 || f != 2 || w != 500 {
		t.Fatalf("estado inicial inesperado: %d/%d/%d", g, f, w)
	}

	// Ordem do Monitoramento: sai da árvore, depois sai do índice.
	if _, ok := tm.RemoveFile("C:\\Backup\\copia.bin"); !ok {
		t.Fatal("o arquivo deveria estar na árvore")
	}
	idx.RemoveFileFromIndex("C:\\Backup\\copia.bin")
	if g, f, w := statsOf(t, idx); g != 0 || f != 0 || w != 0 {
		t.Fatalf("grupo deveria ter se desfeito: %d/%d/%d", g, f, w)
	}

	// Ordem dos handlers: a pasta sai da árvore, depois sai do índice.
	idx.UpsertFile(scanner.NewFileNodeAt("C:\\Backup\\copia.bin", scanner.FileMeta{Name: "copia.bin", Size: 500, ModTime: 3, Hash: "xxh64:aaaa"}))
	if g, _, _ := statsOf(t, idx); g != 1 {
		t.Fatal("o par deveria ter voltado a se formar")
	}
	tm.RemoveDir("C:\\Origem")
	if n := idx.RemoveDirFromIndex("C:\\Origem"); n != 1 {
		t.Fatalf("a pasta removida tinha 1 arquivo indexado, o índice largou %d", n)
	}
	if g, f, _ := statsOf(t, idx); g != 0 || f != 0 {
		t.Fatalf("índice deveria ter esvaziado o grupo: %d/%d", g, f)
	}
}

// RemoveDirFromIndex pega os arquivos diretos da pasta e os das subpastas, e não
// encosta em pasta irmã com nome parecido.
func TestDuplicateIndex_RemoveDirPegaDiretosENinhados(t *testing.T) {
	idx := NewDuplicateIndex()
	idx.RebuildIndex([]*scanner.FileNode{
		scanner.NewFileNodeAt("C:\\Fotos\\a.jpg", scanner.FileMeta{Name: "a.jpg", Size: 10, Hash: "xxh64:aaaa"}),
		scanner.NewFileNodeAt("C:\\Fotos\\2024\\b.jpg", scanner.FileMeta{Name: "b.jpg", Size: 10, Hash: "xxh64:aaaa"}),
		scanner.NewFileNodeAt("C:\\FotosAntigas\\c.jpg", scanner.FileMeta{Name: "c.jpg", Size: 10, Hash: "xxh64:aaaa"}),
		scanner.NewFileNodeAt("C:\\Outra\\d.jpg", scanner.FileMeta{Name: "d.jpg", Size: 10, Hash: "xxh64:aaaa"}),
	})
	if _, f, _ := statsOf(t, idx); f != 4 {
		t.Fatalf("esperava 4 arquivos no grupo, obtive %d", f)
	}

	if n := idx.RemoveDirFromIndex("C:\\Fotos"); n != 2 {
		t.Fatalf("esperava largar 2 arquivos (direto + ninhado), larguei %d", n)
	}
	if _, f, _ := statsOf(t, idx); f != 2 {
		t.Fatalf("a pasta irmã de nome parecido foi levada junto: sobraram %d arquivos", f)
	}

	// E o que sobrou continua removível pelo caminho.
	idx.RemoveFileFromIndex("C:\\FotosAntigas\\c.jpg")
	if g, f, _ := statsOf(t, idx); g != 0 || f != 0 {
		t.Fatalf("remoção por caminho após a remoção da pasta falhou: %d/%d", g, f)
	}
}

// A Varredura Rápida descarta, direto no nó vivo, o hash calculado com outro
// algoritmo. Um arquivo removido depois disso ainda tem de sair do grupo dele:
// o índice guarda sob que conteúdo arquivou o nó, em vez de perguntar ao nó na
// hora de remover.
func TestDuplicateIndex_RemocaoAposHashReescritoNoNo(t *testing.T) {
	idx := NewDuplicateIndex()
	a := scanner.NewFileNodeAt("C:\\a\\1.bin", scanner.FileMeta{Name: "1.bin", Size: 100, ModTime: 1, Hash: "xxh64:aaaa"})
	b := scanner.NewFileNodeAt("C:\\b\\2.bin", scanner.FileMeta{Name: "2.bin", Size: 100, ModTime: 2, Hash: "xxh64:aaaa"})
	idx.UpsertFile(a)
	idx.UpsertFile(b)
	if g, f, _ := statsOf(t, idx); g != 1 || f != 2 {
		t.Fatalf("estado inicial inesperado: %d/%d", g, f)
	}

	b.SetHash("")
	idx.RemoveFileFromIndex("C:\\b\\2.bin")
	if g, f, w := statsOf(t, idx); g != 0 || f != 0 || w != 0 {
		t.Fatalf("o arquivo removido virou fantasma no grupo: %d/%d/%d", g, f, w)
	}
}

// Nós montados avulsos (Monitoramento, leitura de Snapshot, testes) ganham uma
// pasta sintética cada um: dois arquivos da mesma pasta podem ter ponteiros de
// pai diferentes, e mesmo assim os dois têm de ser removíveis pelo caminho.
func TestDuplicateIndex_MesmaPastaComPaisSinteticosDistintos(t *testing.T) {
	idx := NewDuplicateIndex()
	um := scanner.NewFileNodeAt("C:\\Mesma\\um.bin", scanner.FileMeta{Name: "um.bin", Size: 10, Hash: "xxh64:aaaa"})
	dois := scanner.NewFileNodeAt("C:\\Mesma\\dois.bin", scanner.FileMeta{Name: "dois.bin", Size: 10, Hash: "xxh64:aaaa"})
	if um.Parent() == dois.Parent() {
		t.Fatal("o teste perdeu o sentido: os nós avulsos deveriam ter pais distintos")
	}

	idx.UpsertFile(um)
	idx.UpsertFile(dois)
	if g, f, _ := statsOf(t, idx); g != 1 || f != 2 {
		t.Fatalf("esperava 1 grupo com 2 arquivos, obtive %d/%d", g, f)
	}

	idx.RemoveFileFromIndex("C:\\Mesma\\dois.bin")
	idx.RemoveFileFromIndex("C:\\Mesma\\um.bin")
	if g, f, _ := statsOf(t, idx); g != 0 || f != 0 {
		t.Fatalf("os dois arquivos deveriam ter saído do índice: %d/%d", g, f)
	}
}
