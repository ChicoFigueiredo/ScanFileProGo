package scanner

import (
	"testing"
)

func TestTreeManager_ReplaceFile(t *testing.T) {
	tm := NewTreeManager()
	tm.GetOrCreateRoot("C:\\")

	tm.AddFile(&FileNode{Path: "C:\\Folder1\\a.txt", Name: "a.txt", Size: 1000, ModTime: 10})
	tm.AddFile(&FileNode{Path: "C:\\Folder1\\Sub\\b.txt", Name: "b.txt", Size: 500, ModTime: 20})

	root := tm.FindDir("C:\\")
	if root.TotalSize != 1500 || root.FileCount != 2 {
		t.Fatalf("estado inicial inesperado: size=%d count=%d", root.TotalSize, root.FileCount)
	}

	// Replacing an existing file must adjust sizes without changing the file count.
	prev, replaced := tm.ReplaceFile(&FileNode{Path: "C:\\Folder1\\a.txt", Name: "a.txt", Size: 4000, ModTime: 99})
	if !replaced {
		t.Fatal("esperava replaced=true para arquivo existente")
	}
	if prev != 1000 {
		t.Fatalf("esperava previousSize 1000, obtive %d", prev)
	}
	if root.TotalSize != 4500 || root.FileCount != 2 {
		t.Fatalf("raiz inconsistente após substituição: size=%d count=%d", root.TotalSize, root.FileCount)
	}
	// FileCount is aggregated: a.txt plus Sub\b.txt.
	f1 := tm.FindDir("C:\\Folder1")
	if f1.TotalSize != 4500 || f1.FileCount != 2 {
		t.Fatalf("Folder1 inconsistente: size=%d count=%d", f1.TotalSize, f1.FileCount)
	}

	// The node stored in the tree must be the new one.
	var found *FileNode
	tm.IterateFiles(func(f *FileNode) bool {
		if f.Path == "C:\\Folder1\\a.txt" {
			found = f
		}
		return true
	})
	if found == nil || found.ModTime != 99 || found.Size != 4000 {
		t.Fatalf("nó não foi substituído: %+v", found)
	}

	// Replacing an unknown path must add it.
	prev, replaced = tm.ReplaceFile(&FileNode{Path: "C:\\Folder1\\Sub\\c.txt", Name: "c.txt", Size: 250})
	if replaced || prev != 0 {
		t.Fatalf("esperava inserção (replaced=false, prev=0), obtive replaced=%v prev=%d", replaced, prev)
	}
	if root.TotalSize != 4750 || root.FileCount != 3 {
		t.Fatalf("raiz inconsistente após inserção: size=%d count=%d", root.TotalSize, root.FileCount)
	}
}

func TestTreeManager_ReplaceFileIsCaseInsensitive(t *testing.T) {
	tm := NewTreeManager()
	tm.AddFile(&FileNode{Path: "C:\\Dir\\Foto.JPG", Name: "Foto.JPG", Size: 100})

	prev, replaced := tm.ReplaceFile(&FileNode{Path: "C:\\dir\\foto.jpg", Name: "foto.jpg", Size: 300})
	if !replaced || prev != 100 {
		t.Fatalf("esperava substituição case-insensitive, obtive replaced=%v prev=%d", replaced, prev)
	}
	root := tm.FindDir("C:\\")
	if root.FileCount != 1 || root.TotalSize != 300 {
		t.Fatalf("raiz inconsistente: size=%d count=%d", root.TotalSize, root.FileCount)
	}
}

func TestTreeManager_FindFile(t *testing.T) {
	tm := NewTreeManager()
	tm.AddFile(&FileNode{Path: "C:\\Dir\\Sub\\Foto.JPG", Name: "Foto.JPG", Size: 42})

	if got := tm.FindFile("C:\\Dir\\Sub\\Foto.JPG"); got == nil || got.Size != 42 {
		t.Fatalf("esperava encontrar o arquivo exato, obtive %v", got)
	}
	if got := tm.FindFile("c:\\dir\\sub\\foto.jpg"); got == nil {
		t.Fatal("esperava busca case-insensitive, como no Windows")
	}
	if got := tm.FindFile("C:\\Dir\\Sub\\outro.jpg"); got != nil {
		t.Fatalf("esperava nil para caminho desconhecido, obtive %v", got)
	}
	if got := tm.FindFile("C:\\Nada\\arquivo.txt"); got != nil {
		t.Fatalf("esperava nil para pasta desconhecida, obtive %v", got)
	}
	if got := tm.FindFile(""); got != nil {
		t.Fatal("esperava nil para caminho vazio")
	}
}

func TestTreeManager_RemoveDir(t *testing.T) {
	tm := NewTreeManager()
	tm.AddFile(&FileNode{Path: "C:\\Keep\\keep.txt", Name: "keep.txt", Size: 7})
	tm.AddFile(&FileNode{Path: "C:\\Parent\\Target\\a.bin", Name: "a.bin", Size: 100})
	tm.AddFile(&FileNode{Path: "C:\\Parent\\Target\\deep\\b.bin", Name: "b.bin", Size: 300})
	tm.AddFile(&FileNode{Path: "C:\\Parent\\other.bin", Name: "other.bin", Size: 11})

	root := tm.FindDir("C:\\")
	if root.TotalSize != 418 || root.FileCount != 4 {
		t.Fatalf("estado inicial inesperado: size=%d count=%d", root.TotalSize, root.FileCount)
	}

	bytes, files, ok := tm.RemoveDir("C:\\Parent\\Target")
	if !ok {
		t.Fatal("esperava ok=true ao remover pasta existente")
	}
	if bytes != 400 || files != 2 {
		t.Fatalf("esperava 400 bytes e 2 arquivos removidos, obtive %d/%d", bytes, files)
	}

	if tm.FindDir("C:\\Parent\\Target") != nil {
		t.Fatal("pasta removida ainda encontrada na árvore")
	}
	if tm.FindDir("C:\\Parent\\Target\\deep") != nil {
		t.Fatal("subpasta da pasta removida ainda encontrada")
	}

	parent := tm.FindDir("C:\\Parent")
	if parent.TotalSize != 11 || parent.FileCount != 1 {
		t.Fatalf("pai inconsistente: size=%d count=%d", parent.TotalSize, parent.FileCount)
	}
	if parent.SubDirCount != 0 {
		t.Fatalf("esperava SubDirCount 0 no pai, obtive %d", parent.SubDirCount)
	}
	if root.TotalSize != 18 || root.FileCount != 2 {
		t.Fatalf("raiz inconsistente: size=%d count=%d", root.TotalSize, root.FileCount)
	}

	// Removing again must be a no-op.
	if _, _, ok := tm.RemoveDir("C:\\Parent\\Target"); ok {
		t.Fatal("esperava ok=false ao remover pasta inexistente")
	}

	// Files under the removed subtree must be gone from iteration.
	count := 0
	tm.IterateFiles(func(f *FileNode) bool {
		count++
		return true
	})
	if count != 2 {
		t.Fatalf("esperava 2 arquivos restantes, obtive %d", count)
	}
}

func TestTreeManager_RemoveDirRoot(t *testing.T) {
	tm := NewTreeManager()
	tm.AddFile(&FileNode{Path: "C:\\a\\x.txt", Name: "x.txt", Size: 5})
	tm.AddFile(&FileNode{Path: "D:\\b\\y.txt", Name: "y.txt", Size: 9})

	bytes, files, ok := tm.RemoveDir("C:\\")
	if !ok || bytes != 5 || files != 1 {
		t.Fatalf("esperava remoção da raiz C:\\ (5/1), obtive ok=%v %d/%d", ok, bytes, files)
	}
	if tm.FindDir("C:\\") != nil {
		t.Fatal("raiz removida ainda encontrada")
	}
	if tm.FindDir("D:\\") == nil {
		t.Fatal("raiz D:\\ não deveria ter sido afetada")
	}
	if tm.GetTotalFileCount() != 1 {
		t.Fatalf("esperava 1 arquivo restante, obtive %d", tm.GetTotalFileCount())
	}
}
