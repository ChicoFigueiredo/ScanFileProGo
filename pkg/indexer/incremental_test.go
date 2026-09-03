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

	a := &scanner.FileNode{Path: "C:\\a\\one.bin", Name: "one.bin", Size: 100, ModTime: 10, Hash: "xxh64:aaaa"}
	b := &scanner.FileNode{Path: "C:\\b\\two.bin", Name: "two.bin", Size: 100, ModTime: 20, Hash: "xxh64:aaaa"}
	c := &scanner.FileNode{Path: "C:\\c\\three.bin", Name: "three.bin", Size: 100, ModTime: 30, Hash: "xxh64:aaaa"}

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
	if res.Groups[0].Files[0].Path != a.Path || res.Groups[0].Files[2].Path != c.Path {
		t.Fatalf("grupo fora de ordem por ModTime: %v", res.Groups[0].Files)
	}
	if res.Groups[0].Confidence != ConfidenceHash {
		t.Fatalf("grupo de arquivos deve ter confiança hash, obtive %q", res.Groups[0].Confidence)
	}
}

func TestDuplicateIndex_UpsertReplacesSameFileWithoutDoubleCounting(t *testing.T) {
	idx := NewDuplicateIndex()
	idx.UpsertFile(&scanner.FileNode{Path: "C:\\a\\x.bin", Size: 100, ModTime: 1, Hash: "xxh64:aaaa"})
	idx.UpsertFile(&scanner.FileNode{Path: "C:\\b\\x.bin", Size: 100, ModTime: 2, Hash: "xxh64:aaaa"})

	// The very same path is re-upserted (a rewrite that produced the same content).
	idx.UpsertFile(&scanner.FileNode{Path: "C:\\b\\x.bin", Size: 100, ModTime: 5, Hash: "xxh64:aaaa"})
	if g, f, w := statsOf(t, idx); g != 1 || f != 2 || w != 100 {
		t.Fatalf("reinserção duplicou entradas: %d/%d/%d", g, f, w)
	}

	// Now the content changes: the file leaves the old group and becomes unique.
	idx.UpsertFile(&scanner.FileNode{Path: "C:\\b\\x.bin", Size: 250, ModTime: 6, Hash: "xxh64:bbbb"})
	if g, f, w := statsOf(t, idx); g != 0 || f != 0 || w != 0 {
		t.Fatalf("grupo deveria ter se desfeito: %d/%d/%d", g, f, w)
	}

	// A twin of the new content re-forms a group with the new size.
	idx.UpsertFile(&scanner.FileNode{Path: "C:\\c\\y.bin", Size: 250, ModTime: 7, Hash: "xxh64:bbbb"})
	if g, f, w := statsOf(t, idx); g != 1 || f != 2 || w != 250 {
		t.Fatalf("esperava novo grupo de 250 bytes, obtive %d/%d/%d", g, f, w)
	}
}

func TestDuplicateIndex_UpsertWithoutHashIsIgnored(t *testing.T) {
	idx := NewDuplicateIndex()
	idx.UpsertFile(&scanner.FileNode{Path: "C:\\a\\x.bin", Size: 100, Hash: "xxh64:aaaa"})
	idx.UpsertFile(&scanner.FileNode{Path: "C:\\b\\x.bin", Size: 100, Hash: "xxh64:aaaa"})
	if g, _, _ := statsOf(t, idx); g != 1 {
		t.Fatalf("esperava 1 grupo, obtive %d", g)
	}

	// A locked file comes back without a hash: it must leave the index instead of
	// pretending to still be a duplicate.
	idx.UpsertFile(&scanner.FileNode{Path: "C:\\b\\x.bin", Size: 100, Hash: ""})
	if g, f, w := statsOf(t, idx); g != 0 || f != 0 || w != 0 {
		t.Fatalf("arquivo sem hash deveria sair do índice: %d/%d/%d", g, f, w)
	}
}

func TestDuplicateIndex_RemoveFileKeepsWastedBytesConsistent(t *testing.T) {
	idx := NewDuplicateIndex()
	files := []*scanner.FileNode{
		{Path: "C:\\a\\1.bin", Size: 1000, ModTime: 1, Hash: "xxh64:aaaa"},
		{Path: "C:\\b\\2.bin", Size: 1000, ModTime: 2, Hash: "xxh64:aaaa"},
		{Path: "C:\\c\\3.bin", Size: 1000, ModTime: 3, Hash: "xxh64:aaaa"},
		{Path: "C:\\d\\4.bin", Size: 40, ModTime: 4, Hash: "xxh64:bbbb"},
		{Path: "C:\\e\\5.bin", Size: 40, ModTime: 5, Hash: "xxh64:bbbb"},
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
	idx.UpsertFile(&scanner.FileNode{Path: "C:\\f\\6.bin", Size: 40, ModTime: 6, Hash: "xxh64:bbbb"})
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
	idx.UpsertFile(&scanner.FileNode{Path: "C:\\Fotos\\IMG.JPG", Size: 10, Hash: "xxh64:aaaa"})
	idx.UpsertFile(&scanner.FileNode{Path: "C:\\Backup\\img.jpg", Size: 10, Hash: "xxh64:aaaa"})

	idx.RemoveFileFromIndex("c:\\fotos\\img.jpg")
	if g, f, _ := statsOf(t, idx); g != 0 || f != 0 {
		t.Fatalf("remoção case-insensitive falhou: %d/%d", g, f)
	}
}

func TestDuplicateIndex_QueryReturnsDetachedCopies(t *testing.T) {
	idx := NewDuplicateIndex()
	idx.UpsertFile(&scanner.FileNode{Path: "C:\\a\\1.bin", Size: 10, Hash: "xxh64:aaaa"})
	idx.UpsertFile(&scanner.FileNode{Path: "C:\\b\\2.bin", Size: 10, Hash: "xxh64:aaaa"})

	res := idx.Query(QueryFilter{})
	if len(res.Groups) != 1 {
		t.Fatalf("esperava 1 grupo, obtive %d", len(res.Groups))
	}
	grp := res.Groups[0]

	// Mutating the index afterwards must not touch the slice already handed out,
	// otherwise the JSON encoder in pkg/server races with Monitoramento.
	idx.UpsertFile(&scanner.FileNode{Path: "C:\\c\\3.bin", Size: 10, Hash: "xxh64:aaaa"})
	if len(grp.Files) != 2 {
		t.Fatalf("consulta devolveu o grupo vivo do índice: %d arquivos", len(grp.Files))
	}
}

func TestDuplicateIndex_RebuildResetsPathMap(t *testing.T) {
	idx := NewDuplicateIndex()
	idx.RebuildIndex([]*scanner.FileNode{
		{Path: "C:\\a\\1.bin", Size: 10, Hash: "xxh64:aaaa"},
		{Path: "C:\\b\\2.bin", Size: 10, Hash: "xxh64:aaaa"},
	})
	idx.RebuildIndex([]*scanner.FileNode{
		{Path: "C:\\x\\9.bin", Size: 70, Hash: "xxh64:cccc"},
	})

	// Paths from the previous build must not linger in the path map.
	idx.RemoveFileFromIndex("C:\\a\\1.bin")
	idx.UpsertFile(&scanner.FileNode{Path: "C:\\y\\8.bin", Size: 70, Hash: "xxh64:cccc"})
	if g, f, w := statsOf(t, idx); g != 1 || f != 2 || w != 70 {
		t.Fatalf("reconstrução deixou estado sujo: %d/%d/%d", g, f, w)
	}
}
