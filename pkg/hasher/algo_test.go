package hasher

import (
	"os"
	"path/filepath"
	"testing"

	"scanfile/pkg/scanner"
)

// Vetores conhecidos, conferidos contra as especificações públicas de cada
// algoritmo. Servem de trava contra troca acidental de implementação.
var knownVectors = []struct {
	algo  string
	empty string
	abc   string
}{
	{scanner.HashXXHash, "xxh64:ef46db3751d8e999", "xxh64:44bc2cf5ad770999"},
	{scanner.HashBlake3, "blake3:af1349b9f5f9a1a6a0404dea36dcc9499bcb25c9adc112b7cc9a93cae41f3262", "blake3:6437b3ac38465133ffb63b75273a8db548c558465d79db03fd359c6cd5bd9d85"},
	{scanner.HashMD5, "md5:d41d8cd98f00b204e9800998ecf8427e", "md5:900150983cd24fb0d6963f7d28e17f72"},
	{scanner.HashSHA256, "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", "sha256:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"},
}

func TestKnownVectors_HashBytes(t *testing.T) {
	for _, v := range knownVectors {
		if got := HashBytes(v.algo, nil); got != v.empty {
			t.Errorf("%s vazio = %q, quer %q", v.algo, got, v.empty)
		}
		if got := HashBytes(v.algo, []byte("abc")); got != v.abc {
			t.Errorf("%s de abc = %q, quer %q", v.algo, got, v.abc)
		}
	}
}

func TestKnownVectors_ComputeSingleFileHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "abc.bin")
	if err := os.WriteFile(path, []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, v := range knownVectors {
		got, size, err := ComputeSingleFileHash(path, v.algo)
		if err != nil {
			t.Fatalf("%s: %v", v.algo, err)
		}
		if size != 3 {
			t.Errorf("%s: tamanho = %d, quer 3", v.algo, size)
		}
		if got != v.abc {
			t.Errorf("%s: %q, quer %q", v.algo, got, v.abc)
		}
	}
}

func TestComputeSingleFileHash_DefaultsToXXHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "abc.bin")
	if err := os.WriteFile(path, []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, _, err := ComputeSingleFileHash(path, "algoritmo-inexistente")
	if err != nil {
		t.Fatal(err)
	}
	if got != "xxh64:44bc2cf5ad770999" {
		t.Errorf("algoritmo desconhecido deveria cair em xxhash, obtive %q", got)
	}
}
