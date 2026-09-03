package scanner

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestFileNodePathDerivado prova que Path() reconstrói o caminho completo a
// partir do nome e da cadeia de pastas: arquivo na raiz, arquivo em subpasta
// profunda e nomes com acentos.
func TestFileNodePathDerivado(t *testing.T) {
	casos := []struct {
		nome string
		dir  string
		arq  string
	}{
		{"raiz do volume", `C:\`, "raiz.txt"},
		{"primeiro nível", `C:\Windows`, "notepad.exe"},
		{"subpasta profunda", `C:\Users\chico\AppData\Local\Programs\ScanFile\dados\2026\09`, "relatorio.json"},
		{"acentos no caminho", `C:\Usuários\João\Área de Trabalho\Ãçentuação`, "balanço anual.xlsx"},
		{"acentos na raiz", `D:\`, "índice.md"},
		{"outro volume", `E:\Backup\Fotos 2024`, "IMG_0001.JPG"},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			tm := NewTreeManager()
			esperado := filepath.Join(c.dir, c.arq)

			f := NewFileNode(FileMeta{Name: c.arq, Size: 42})
			tm.AddFileAt(c.dir, f)

			if got := f.Path(); got != esperado {
				t.Fatalf("Path() = %q, esperado %q", got, esperado)
			}
			if got := f.Name(); got != c.arq {
				t.Fatalf("Name() = %q, esperado %q", got, c.arq)
			}

			// O nó também precisa ser encontrável pelo caminho derivado.
			if found := tm.FindFile(esperado); found != f {
				t.Fatalf("FindFile(%q) não devolveu o nó inserido", esperado)
			}

			// E a pasta precisa devolver o mesmo caminho.
			dirNode := tm.FindDir(c.dir)
			if dirNode == nil {
				t.Fatalf("FindDir(%q) devolveu nil", c.dir)
			}
			if got := dirNode.Path(); !strings.EqualFold(got, filepath.Clean(c.dir)) && got != c.dir {
				t.Fatalf("DirNode.Path() = %q, esperado %q", got, c.dir)
			}
		})
	}
}

// TestFileNodePathAcompanhaAPasta prova que o caminho é derivado de verdade:
// nada é copiado no momento da inserção.
func TestFileNodePathAcompanhaAPasta(t *testing.T) {
	tm := NewTreeManager()
	f := NewFileNode(FileMeta{Name: "doc.txt"})
	tm.AddFileAt(`C:\Um\Dois\Três`, f)

	if got, want := f.Path(), `C:\Um\Dois\Três\doc.txt`; got != want {
		t.Fatalf("Path() = %q, esperado %q", got, want)
	}

	// O nó solto, sem pasta, devolve só o nome.
	solto := NewFileNode(FileMeta{Name: "solto.bin"})
	if got := solto.Path(); got != "solto.bin" {
		t.Fatalf("nó sem pai: Path() = %q, esperado %q", got, "solto.bin")
	}
}

// TestNewFileNodeAtPathCompleto cobre o construtor usado por testes e pelo
// Monitoramento, que parte de um caminho completo.
func TestNewFileNodeAtPathCompleto(t *testing.T) {
	casos := []string{
		`C:\arquivo na raiz.txt`,
		`C:\a\b\c\d\e\f\g\h\arquivo fundo.dat`,
		`D:\Coração\São Paulo\ação.pdf`,
	}
	for _, p := range casos {
		f := NewFileNodeAt(p, FileMeta{Size: 1})
		if got := f.Path(); got != p {
			t.Errorf("NewFileNodeAt(%q).Path() = %q", p, got)
		}
		if got, want := f.Name(), filepath.Base(p); got != want {
			t.Errorf("Name() = %q, esperado %q", got, want)
		}
	}
}
