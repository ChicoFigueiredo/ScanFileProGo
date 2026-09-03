package hasher

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"scanfile/pkg/scanner"
)

func writeFile(t *testing.T, path string, data []byte) *scanner.FileNode {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return scanner.NewFileNodeAt(path, scanner.FileMeta{Name: filepath.Base(path), Size: int64(len(data)), Extension: filepath.Ext(path)})
}

func TestComputeQuickHash_HeaderAndFooterOnly(t *testing.T) {
	dir := t.TempDir()
	base := bytes.Repeat([]byte("A"), 64*1024)

	mid := make([]byte, len(base))
	copy(mid, base)
	mid[32*1024] = 'Z' // difere só no meio: o Pré-hash NÃO pode notar

	diffHead := make([]byte, len(base))
	copy(diffHead, base)
	diffHead[0] = 'Z'

	diffTail := make([]byte, len(base))
	copy(diffTail, base)
	diffTail[len(diffTail)-1] = 'Z'

	pBase := filepath.Join(dir, "base.bin")
	pMid := filepath.Join(dir, "mid.bin")
	pHead := filepath.Join(dir, "head.bin")
	pTail := filepath.Join(dir, "tail.bin")
	writeFile(t, pBase, base)
	writeFile(t, pMid, mid)
	writeFile(t, pHead, diffHead)
	writeFile(t, pTail, diffTail)

	qBase, err := ComputeQuickHash(pBase, int64(len(base)))
	if err != nil {
		t.Fatal(err)
	}
	qMid, err := ComputeQuickHash(pMid, int64(len(mid)))
	if err != nil {
		t.Fatal(err)
	}
	qHead, err := ComputeQuickHash(pHead, int64(len(diffHead)))
	if err != nil {
		t.Fatal(err)
	}
	qTail, err := ComputeQuickHash(pTail, int64(len(diffTail)))
	if err != nil {
		t.Fatal(err)
	}

	if qBase != qMid {
		t.Errorf("Pré-hash deveria ignorar o miolo: %x != %x", qBase, qMid)
	}
	if qBase == qHead {
		t.Error("Pré-hash deveria detectar cabeçalho diferente")
	}
	if qBase == qTail {
		t.Error("Pré-hash deveria detectar rodapé diferente")
	}
	if qBase == 0 {
		t.Error("Pré-hash não deveria ser zero")
	}
}

func TestComputeQuickHash_SmallFileReadsWholeFile(t *testing.T) {
	dir := t.TempDir()
	// Arquivo menor que 2 x 4096: cabeçalho e rodapé se sobrepõem.
	data := bytes.Repeat([]byte("x"), 5000)
	p := filepath.Join(dir, "small.bin")
	writeFile(t, p, data)

	q, err := ComputeQuickHash(p, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if q == 0 {
		t.Fatal("esperava Pré-hash não nulo")
	}
}

// O Pré-hash precisa evitar a leitura completa quando cabeçalho/rodapé diferem,
// e ainda assim detectar uma diferença no meio via Hash Completo.
func TestRunHashing_PrehashAvoidsFullRead(t *testing.T) {
	dir := t.TempDir()
	const size = 1 << 20 // 1 MiB

	base := bytes.Repeat([]byte("A"), size)

	mid := make([]byte, size)
	copy(mid, base)
	mid[size/2] = 'Z' // mesmo cabeçalho/rodapé, miolo diferente

	head := make([]byte, size)
	copy(head, base)
	head[0] = 'Z' // cabeçalho diferente: eliminado pelo Pré-hash

	fBase := writeFile(t, filepath.Join(dir, "base.bin"), base)
	fMid := writeFile(t, filepath.Join(dir, "mid.bin"), mid)
	fHead := writeFile(t, filepath.Join(dir, "head.bin"), head)

	h := NewHasher()
	err := h.RunHashing(context.Background(), []*scanner.FileNode{fBase, fMid, fHead}, ComputeHashOptions{
		Algorithm:     scanner.HashXXHash,
		MinSize:       1,
		WorkerThreads: 2,
	})
	if err != nil {
		t.Fatalf("RunHashing: %v", err)
	}

	if fHead.Hash() != "" {
		t.Errorf("head.bin não deveria receber Hash Completo, obtive %q", fHead.Hash())
	}
	if fHead.QuickHash() == 0 {
		t.Error("head.bin deveria ter Pré-hash gravado")
	}
	if fBase.Hash() == "" || fMid.Hash() == "" {
		t.Fatalf("base e mid deveriam ter Hash Completo: %q / %q", fBase.Hash(), fMid.Hash())
	}
	if fBase.Hash() == fMid.Hash() {
		t.Error("Hash Completo deveria detectar a diferença no meio")
	}

	if got := h.PrehashCount(); got != 3 {
		t.Errorf("PrehashCount = %d, quer 3", got)
	}
	if got := h.PrehashEliminated(); got != 1 {
		t.Errorf("PrehashEliminated = %d, quer 1", got)
	}
	// Sem Pré-hash, os 3 arquivos seriam lidos por inteiro (3 MiB).
	if got := h.BytesHashed(); got != 2*size {
		t.Errorf("BytesHashed = %d, quer %d", got, 2*size)
	}
	if got := h.BytesRead(); got >= 3*size {
		t.Errorf("BytesRead = %d, deveria ficar bem abaixo de %d", got, 3*size)
	}
}

// Arquivos com até 8192 bytes vão direto ao Hash Completo, sem Pré-hash.
func TestRunHashing_SmallFilesSkipPrehash(t *testing.T) {
	dir := t.TempDir()
	data := bytes.Repeat([]byte("s"), PrehashMinSize) // exatamente 8192
	a := writeFile(t, filepath.Join(dir, "a.bin"), data)
	b := writeFile(t, filepath.Join(dir, "b.bin"), data)

	h := NewHasher()
	if err := h.RunHashing(context.Background(), []*scanner.FileNode{a, b}, ComputeHashOptions{
		Algorithm: scanner.HashXXHash,
		MinSize:   1,
	}); err != nil {
		t.Fatal(err)
	}

	if a.Hash() == "" || b.Hash() == "" {
		t.Fatal("arquivos pequenos deveriam ir direto ao Hash Completo")
	}
	if a.Hash() != b.Hash() {
		t.Error("arquivos idênticos deveriam ter o mesmo hash")
	}
	if got := h.PrehashCount(); got != 0 {
		t.Errorf("PrehashCount = %d, quer 0 para arquivos <= %d bytes", got, PrehashMinSize)
	}
}

func TestRunHashing_HashAllFilesIgnoresPrehash(t *testing.T) {
	dir := t.TempDir()
	a := writeFile(t, filepath.Join(dir, "unico.bin"), bytes.Repeat([]byte("u"), 100000))

	h := NewHasher()
	if err := h.RunHashing(context.Background(), []*scanner.FileNode{a}, ComputeHashOptions{
		Algorithm:    scanner.HashBlake3,
		HashAllFiles: true,
		MinSize:      1,
	}); err != nil {
		t.Fatal(err)
	}
	if a.Hash() == "" {
		t.Fatal("HashAllFiles deveria hashear até arquivos sem par de tamanho")
	}
	if scanner.HashAlgorithmOf(a.Hash()) != scanner.HashBlake3 {
		t.Errorf("hash %q não tem prefixo blake3", a.Hash())
	}
	if got := h.PrehashCount(); got != 0 {
		t.Errorf("HashAllFiles não deveria usar Pré-hash, PrehashCount = %d", got)
	}
}

// Hashes reaproveitados do Quick Scan só valem se o prefixo for o do algoritmo
// atual; caso contrário o arquivo volta para a fila de Hash Completo.
func TestRunHashing_RehashesWhenAlgorithmChanged(t *testing.T) {
	dir := t.TempDir()
	data := bytes.Repeat([]byte("q"), 4096)
	a := writeFile(t, filepath.Join(dir, "a.bin"), data)
	b := writeFile(t, filepath.Join(dir, "b.bin"), data)
	// Reaproveitado de uma varredura em xxhash.
	a.SetHash("xxh64:deadbeefdeadbeef")
	b.SetHash("xxh64:deadbeefdeadbeef")

	h := NewHasher()
	if err := h.RunHashing(context.Background(), []*scanner.FileNode{a, b}, ComputeHashOptions{
		Algorithm: scanner.HashSHA256,
		MinSize:   1,
	}); err != nil {
		t.Fatal(err)
	}

	for _, f := range []*scanner.FileNode{a, b} {
		if scanner.HashAlgorithmOf(f.Hash()) != scanner.HashSHA256 {
			t.Errorf("%s manteve hash de outro algoritmo: %q", f.Name(), f.Hash())
		}
	}
}

func TestRunHashing_KeepsReusedHashOfSameAlgorithm(t *testing.T) {
	dir := t.TempDir()
	data := bytes.Repeat([]byte("k"), 4096)
	a := writeFile(t, filepath.Join(dir, "a.bin"), data)
	b := writeFile(t, filepath.Join(dir, "b.bin"), data)
	reused := "xxh64:deadbeefdeadbeef"
	a.SetHash(reused)
	b.SetHash(reused)

	h := NewHasher()
	if err := h.RunHashing(context.Background(), []*scanner.FileNode{a, b}, ComputeHashOptions{
		Algorithm: scanner.HashXXHash,
		MinSize:   1,
	}); err != nil {
		t.Fatal(err)
	}
	if a.Hash() != reused || b.Hash() != reused {
		t.Errorf("hash do mesmo algoritmo deveria ser reaproveitado: %q / %q", a.Hash(), b.Hash())
	}
	if got := h.BytesHashed(); got != 2*4096 {
		t.Errorf("BytesHashed = %d, quer %d (contabilizados como reaproveitados)", got, 2*4096)
	}
}

func TestRunHashing_DetailedProgressReported(t *testing.T) {
	dir := t.TempDir()
	var files []*scanner.FileNode
	for i := 0; i < 4; i++ {
		files = append(files, writeFile(t, filepath.Join(dir, fmt.Sprintf("f%d.bin", i)), bytes.Repeat([]byte("p"), 40000)))
	}

	var last HashProgress
	h := NewHasher()
	if err := h.RunHashing(context.Background(), files, ComputeHashOptions{
		Algorithm:          scanner.HashMD5,
		MinSize:            1,
		OnDetailedProgress: func(p HashProgress) { last = p },
	}); err != nil {
		t.Fatal(err)
	}

	if last.Stage != HashStageDone {
		t.Errorf("último estágio = %q, quer %q", last.Stage, HashStageDone)
	}
	if last.PrehashCount != 4 {
		t.Errorf("progresso.PrehashCount = %d, quer 4", last.PrehashCount)
	}
	if last.BytesRead < last.BytesHashed {
		t.Errorf("BytesRead (%d) deveria incluir os bytes do Pré-hash além dos %d hasheados", last.BytesRead, last.BytesHashed)
	}
	if last.HashedCount != 4 {
		t.Errorf("progresso.HashedCount = %d, quer 4", last.HashedCount)
	}
}
