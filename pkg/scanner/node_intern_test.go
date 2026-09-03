package scanner

import (
	"fmt"
	"sync"
	"testing"
	"unsafe"
)

// TestExtensionInterning prova que milhares de arquivos com a mesma extensão
// compartilham uma única string: é isso que tira 16 bytes por item do nó.
func TestExtensionInterning(t *testing.T) {
	const ext = ".teste-internacao"

	antes := InternedExtensionCount()
	primeiro := NewFileNode(FileMeta{Name: "a" + ext, Extension: ext})
	depois := InternedExtensionCount()
	if depois != antes+1 {
		t.Fatalf("tabela cresceu de %d para %d, esperava um item novo", antes, depois)
	}

	base := unsafe.StringData(primeiro.Extension())
	for i := 0; i < 1000; i++ {
		f := NewFileNode(FileMeta{Name: fmt.Sprintf("f%d%s", i, ext), Extension: ext})
		if f.Extension() != ext {
			t.Fatalf("Extension() = %q, esperado %q", f.Extension(), ext)
		}
		if unsafe.StringData(f.Extension()) != base {
			t.Fatal("a extensão não foi internada: cada arquivo guarda a própria cópia")
		}
	}
	if got := InternedExtensionCount(); got != depois {
		t.Errorf("tabela cresceu para %d com extensões repetidas", got)
	}

	// Extensão vazia não ocupa lugar na tabela.
	semExt := NewFileNode(FileMeta{Name: "LEIAME"})
	if semExt.Extension() != "" {
		t.Errorf("Extension() = %q, esperado vazio", semExt.Extension())
	}
}

// TestExtensionInterningConcorrente cobre a tabela sendo alimentada pelas
// threads da Fase 1 ao mesmo tempo.
func TestExtensionInterningConcorrente(t *testing.T) {
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				ext := fmt.Sprintf(".conc%d", i%37)
				f := NewFileNode(FileMeta{Name: "x" + ext, Extension: ext})
				if f.Extension() != ext {
					t.Errorf("worker %d: Extension() = %q, esperado %q", w, f.Extension(), ext)
					return
				}
			}
		}(w)
	}
	wg.Wait()
}
