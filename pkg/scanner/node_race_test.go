package scanner

import (
	"fmt"
	"io"
	"sync"
	"testing"
)

// node_race_test.go cobre o cenário que o contrato 1.7 torna normal: o Autosave
// periódico grava o Snapshot em streaming DURANTE a Varredura, inclusive
// enquanto os workers da Fase 2 escrevem o Hash Completo e o Pré-hash nos
// mesmos nós que o exportador está lendo. Os handlers /api/tree e
// /api/duplicates fazem a mesma leitura concorrente.
//
// Rode com `go test -race ./pkg/scanner/`: sem publicação atômica do dígito o
// detector aponta ensureDigest/SetHash contra Hash/QuickHash/Meta/jsonView.

// buildRaceTree monta uma árvore com dirs*perDir arquivos já pendurados,
// devolvendo também a lista plana (é o que a Fase 2 recebe do scanner).
func buildRaceTree(dirs, perDir int) (*TreeManager, []*FileNode) {
	tm := NewTreeManager()
	files := make([]*FileNode, 0, dirs*perDir)
	for d := 0; d < dirs; d++ {
		batch := make([]*FileNode, perDir)
		for j := 0; j < perDir; j++ {
			idx := d*perDir + j
			batch[j] = NewFileNode(FileMeta{
				Name:          fmt.Sprintf("arquivo_%05d.bin", idx),
				Size:          int64(idx % 977),
				AllocatedSize: int64(idx % 977),
				ModTime:       1_700_000_000 + int64(idx),
				CreateTime:    1_600_000_000,
				AccessTime:    1_700_000_000,
				Extension:     ".bin",
			})
		}
		tm.FastSetDir(fmt.Sprintf(`C:\Corrida\pasta_%03d`, d), batch, nil)
		files = append(files, batch...)
	}
	return tm, files
}

// TestFileNodeDigestConcorrenteComSnapshot roda a Fase 2 e o Autosave ao mesmo
// tempo sobre a mesma árvore. Cada arquivo tem um único escritor, como na Fase
// 2 real; o que se cruza é escrita de hash contra leitura do exportador.
func TestFileNodeDigestConcorrenteComSnapshot(t *testing.T) {
	const (
		dirs       = 40
		perDir     = 100
		escritores = 4
		rodadas    = 15
	)

	tm, files := buildRaceTree(dirs, perDir)

	var wg sync.WaitGroup
	largada := make(chan struct{})

	// Escritores: imitam hasher.runPrehashStage e hasher.runHashStage.
	for w := 0; w < escritores; w++ {
		w := w
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-largada
			for r := 0; r < rodadas; r++ {
				for i, f := range files {
					if i%escritores != w {
						continue
					}
					n := uint64(r*len(files)+i) | 1
					f.SetQuickHash(n)
					f.SetHash(fmt.Sprintf("xxh64:%016x", n))
					if r%5 == 4 {
						// selectCandidates descarta hash de outro algoritmo.
						f.SetHash("")
					}
				}
			}
		}()
	}

	// Leitor 1 e 2: o Autosave em streaming (ExportCache -> jsonView).
	for e := 0; e < 2; e++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-largada
			for r := 0; r < rodadas; r++ {
				if err := ExportCache(tm, []string{`C:\`}, ScanConfig{}, io.Discard); err != nil {
					t.Errorf("ExportCache durante a Fase 2: %v", err)
					return
				}
			}
		}()
	}

	// Leitor 3: os handlers HTTP, que leem Hash/QuickHash/Meta dos nós.
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-largada
		for r := 0; r < rodadas; r++ {
			for _, f := range files {
				_ = f.Hash()
				_ = f.QuickHash()
				_ = f.Meta()
			}
		}
	}()

	close(largada)
	wg.Wait()
}

// TestFileNodeDigestEscritaConcorrenteNoMesmoNo cobre o caso em que Pré-hash e
// Hash Completo do MESMO arquivo são gravados de goroutines diferentes: as duas
// metades do dígito não podem se perder uma à outra.
func TestFileNodeDigestEscritaConcorrenteNoMesmoNo(t *testing.T) {
	const nos = 2000

	files := make([]*FileNode, nos)
	for i := range files {
		files[i] = NewFileNode(FileMeta{Name: fmt.Sprintf("n%04d.bin", i), Size: int64(i)})
	}

	const hashFixo = "xxh64:0123456789abcdef"
	const quickFixo = uint64(0xfeedfacecafebeef)

	var wg sync.WaitGroup
	largada := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-largada
		for _, f := range files {
			f.SetHash(hashFixo)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-largada
		for _, f := range files {
			f.SetQuickHash(quickFixo)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-largada
		for _, f := range files {
			_ = f.Hash()
			_ = f.QuickHash()
		}
	}()

	close(largada)
	wg.Wait()

	for i, f := range files {
		if got := f.Hash(); got != hashFixo {
			t.Fatalf("nó %d: Hash() = %q, esperado %q (escrita perdida)", i, got, hashFixo)
		}
		if got := f.QuickHash(); got != quickFixo {
			t.Fatalf("nó %d: QuickHash() = %#x, esperado %#x (escrita perdida)", i, got, quickFixo)
		}
	}
}
