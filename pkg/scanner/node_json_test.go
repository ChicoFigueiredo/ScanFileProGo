package scanner

import (
	"encoding/json"
	"testing"
)

// Os literais abaixo foram capturados do FileNode ORIGINAL (struct com Path,
// Name, Hash e Extension como campos), antes do nó compacto do ADR-0001. Eles
// são a especificação do formato: qualquer mudança neles quebra os Snapshots
// gravados e a API HTTP. NÃO os regenere com o código novo.
const (
	jsonNodeCompleto = `{"path":"C:\\Documentos\\Relatórios\\balanço \"2024\".xlsx","name":"balanço \"2024\".xlsx","size":123456789,"allocatedSize":123457536,"modTime":1700000000,"createTime":1699000000,"accessTime":1700000500,"hash":"sha256:0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20","quickHash":18446744073709551615,"extension":".xlsx","isSymlink":true,"linkTarget":"D:\\alvo\\balanço.xlsx","isCompressed":true,"isReusedFromCache":true}`

	jsonNodeMinimo = `{"path":"C:\\a.txt","name":"a.txt","size":0,"allocatedSize":0,"modTime":-11644473600,"createTime":0,"accessTime":4102444800,"extension":".txt"}`
)

func nodeCompletoDeTeste() *FileNode {
	return NewFileNodeAt(`C:\Documentos\Relatórios\balanço "2024".xlsx`, FileMeta{
		Size:              123456789,
		AllocatedSize:     123457536,
		ModTime:           1700000000,
		CreateTime:        1699000000,
		AccessTime:        1700000500,
		Hash:              "sha256:0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20",
		QuickHash:         18446744073709551615,
		Extension:         ".xlsx",
		IsSymlink:         true,
		LinkTarget:        `D:\alvo\balanço.xlsx`,
		IsCompressed:      true,
		IsReusedFromCache: true,
	})
}

func nodeMinimoDeTeste() *FileNode {
	return NewFileNodeAt(`C:\a.txt`, FileMeta{
		ModTime:    -11644473600,
		CreateTime: 0,
		AccessTime: 4102444800,
		Extension:  ".txt",
	})
}

// TestFileNodeJSONByteAByte prova que o nó compacto serializa exatamente o
// mesmo JSON do nó antigo, byte a byte (regra 1 da etapa 3).
func TestFileNodeJSONByteAByte(t *testing.T) {
	casos := []struct {
		nome     string
		node     *FileNode
		esperado string
	}{
		{"completo", nodeCompletoDeTeste(), jsonNodeCompleto},
		{"mínimo", nodeMinimoDeTeste(), jsonNodeMinimo},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			got, err := json.Marshal(c.node)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if string(got) != c.esperado {
				t.Fatalf("JSON mudou.\nobtido:   %s\nesperado: %s", got, c.esperado)
			}
		})
	}
}

// TestFileNodeJSONRoundTrip prova que o JSON antigo volta para um nó
// equivalente e que reserializá-lo devolve exatamente os mesmos bytes.
func TestFileNodeJSONRoundTrip(t *testing.T) {
	for _, esperado := range []string{jsonNodeCompleto, jsonNodeMinimo} {
		var f FileNode
		if err := json.Unmarshal([]byte(esperado), &f); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		got, err := json.Marshal(&f)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		if string(got) != esperado {
			t.Fatalf("round-trip mudou o JSON.\nobtido:   %s\nesperado: %s", got, esperado)
		}
	}
}

// TestFileNodeCamposApos prova que os acessores devolvem o conteúdo original.
func TestFileNodeCampos(t *testing.T) {
	f := nodeCompletoDeTeste()

	if got := f.Path(); got != `C:\Documentos\Relatórios\balanço "2024".xlsx` {
		t.Errorf("Path() = %q", got)
	}
	if got := f.Name(); got != `balanço "2024".xlsx` {
		t.Errorf("Name() = %q", got)
	}
	if got := f.Extension(); got != ".xlsx" {
		t.Errorf("Extension() = %q", got)
	}
	if got := f.Hash(); got != "sha256:0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20" {
		t.Errorf("Hash() = %q", got)
	}
	if got := f.QuickHash(); got != 18446744073709551615 {
		t.Errorf("QuickHash() = %d", got)
	}
	if f.ModTime() != 1700000000 || f.CreateTime() != 1699000000 || f.AccessTime() != 1700000500 {
		t.Errorf("tempos = %d/%d/%d", f.ModTime(), f.CreateTime(), f.AccessTime())
	}
	if !f.IsSymlink() || f.LinkTarget() != `D:\alvo\balanço.xlsx` {
		t.Errorf("symlink = %v %q", f.IsSymlink(), f.LinkTarget())
	}
	if !f.IsCompressed() || !f.IsReusedFromCache() {
		t.Errorf("flags = %v %v", f.IsCompressed(), f.IsReusedFromCache())
	}
}

// TestFileNodeHashFormatos prova que todo hash volta idêntico ao que entrou,
// inclusive os que não seguem o formato "<prefixo>:<hex do tamanho certo>".
func TestFileNodeHashFormatos(t *testing.T) {
	hashes := []string{
		"",
		"xxh64:0000000000000001",
		"xxh64:ffffffffffffffff",
		"blake3:0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20",
		"md5:d41d8cd98f00b204e9800998ecf8427e",
		"sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		// Formatos fora do padrão usados por testes e por snapshots antigos.
		"xxh:4444",
		"xxh64:aaaa",
		"xxh64:XYZ",
		"hash-sem-prefixo",
		"sha256:0102",
	}

	for _, h := range hashes {
		f := NewFileNode(FileMeta{Name: "x.bin", Hash: h})
		if got := f.Hash(); got != h {
			t.Errorf("Hash() = %q, esperado %q", got, h)
		}
	}
}

// TestFileNodeTemposExtremos garante que datas fora de 1970..2106 continuam
// exatas, inclusive o zero do FILETIME do Windows (ano 1601).
func TestFileNodeTemposExtremos(t *testing.T) {
	casos := [][3]int64{
		{0, 0, 0},
		{1700000000, 1699000000, 1700000500},
		{-11644473600, -11644473600, -11644473600}, // FILETIME zero (1601)
		{-62135596800, 1700000000, 4102444800},     // time.Time zero
		{4102444800, -11644473600, 253402300799},   // 2100 / 1601 / 9999
		{1 << 40, -(1 << 40), 1 << 41},
	}

	for _, c := range casos {
		f := NewFileNode(FileMeta{Name: "t.bin", ModTime: c[0], CreateTime: c[1], AccessTime: c[2]})
		if f.ModTime() != c[0] || f.CreateTime() != c[1] || f.AccessTime() != c[2] {
			t.Errorf("tempos %v voltaram como %d/%d/%d", c, f.ModTime(), f.CreateTime(), f.AccessTime())
		}

		// Reescrever modTime não pode arrastar os outros dois.
		f.SetModTime(999)
		if f.ModTime() != 999 || f.CreateTime() != c[1] || f.AccessTime() != c[2] {
			t.Errorf("SetModTime bagunçou %v: %d/%d/%d", c, f.ModTime(), f.CreateTime(), f.AccessTime())
		}
	}
}
