//go:build windows

package recycle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// skipUnlessRecyclable skips the test when the volume holding dir has no
// Recycle Bin (a build agent may run from a RAM disk or a network share).
func skipUnlessRecyclable(t *testing.T, dir string) {
	t.Helper()
	root, _, ok := splitVolume(dir)
	if !ok {
		t.Skipf("diretório temporário %q não tem volume reconhecível", dir)
	}
	if _, err := os.Stat(filepath.Join(root, "$Recycle.Bin")); err != nil {
		t.Skipf("volume %s não tem $Recycle.Bin", root)
	}
}

func TestPreflightRefusesVolumeRootQuickly(t *testing.T) {
	start := time.Now()
	ok, reason := Preflight(`C:\`)
	elapsed := time.Since(start)

	if ok {
		t.Fatal("a raiz de um volume nunca pode ser reciclada")
	}
	if reason == "" {
		t.Fatal("recusa sem motivo")
	}
	// A raiz é recusada pela Pasta Protegida, antes de qualquer medição:
	// somar C:\ inteiro levaria minutos.
	if elapsed > 2*time.Second {
		t.Errorf("Preflight(C:\\) levou %v: a recusa deve ser imediata", elapsed)
	}
}

func TestPreflightRefusesUNCPath(t *testing.T) {
	start := time.Now()
	ok, reason := Preflight(`\\servidor-inexistente-scanfile\compartilhamento\arquivo.bin`)
	elapsed := time.Since(start)

	if ok {
		t.Fatal("caminho de rede não deve passar no preflight")
	}
	if !strings.Contains(reason, "rede") && !strings.Contains(reason, "disponível") {
		t.Errorf("motivo inesperado para caminho de rede: %q", reason)
	}
	if elapsed > 5*time.Second {
		t.Errorf("Preflight em UNC inexistente levou %v: não deve esperar a rede", elapsed)
	}
}

func TestPreflightRefusesMissingItem(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nao-existe.bin")
	ok, reason := Preflight(missing)
	if ok {
		t.Fatal("item inexistente não pode passar no preflight")
	}
	if reason == "" {
		t.Fatal("recusa sem motivo")
	}
}

func TestPreflightAcceptsOrdinaryFile(t *testing.T) {
	dir := t.TempDir()
	skipUnlessRecyclable(t, dir)

	path := filepath.Join(dir, "arquivo.txt")
	if err := os.WriteFile(path, []byte("conteúdo"), 0o644); err != nil {
		t.Fatal(err)
	}

	if ok, reason := Preflight(path); !ok {
		t.Fatalf("arquivo comum recusado pelo preflight: %s", reason)
	}
}

func TestBatchDeleteItemsPermanentDeletesAndCounts(t *testing.T) {
	dir := t.TempDir()

	a := filepath.Join(dir, "a.bin")
	b := filepath.Join(dir, "b.bin")
	if err := os.WriteFile(a, make([]byte, 1000), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, make([]byte, 24), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "fantasma.bin")

	res := BatchDeleteItems([]string{a, b, missing}, false)

	if res.TotalRequested != 3 {
		t.Errorf("TotalRequested = %d, esperado 3", res.TotalRequested)
	}
	if res.SuccessCount != 2 {
		t.Errorf("SuccessCount = %d, esperado 2", res.SuccessCount)
	}
	if res.FailedCount != 1 {
		t.Errorf("FailedCount = %d, esperado 1", res.FailedCount)
	}
	if res.FreedBytes != 1024 {
		t.Errorf("FreedBytes = %d, esperado 1024", res.FreedBytes)
	}
	if len(res.Items) != 3 {
		t.Fatalf("Items = %d, esperado 3", len(res.Items))
	}
	if res.Items[0].Status != StatusDeleted || res.Items[1].Status != StatusDeleted {
		t.Errorf("status inesperado: %+v", res.Items)
	}
	if res.Items[2].Status != StatusFailed || res.Items[2].Reason == "" {
		t.Errorf("item inexistente deveria falhar com motivo: %+v", res.Items[2])
	}
	if _, err := os.Stat(a); !os.IsNotExist(err) {
		t.Error("o arquivo a.bin deveria ter sido apagado")
	}
}

func TestBatchDeleteItemsPermanentRemovesFolderAndSumsSize(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "pasta")
	if err := os.MkdirAll(filepath.Join(sub, "interna"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "1.bin"), make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "interna", "2.bin"), make([]byte, 200), 0o644); err != nil {
		t.Fatal(err)
	}

	res := BatchDeleteItems([]string{sub}, false)

	if res.SuccessCount != 1 || res.FreedBytes != 300 {
		t.Fatalf("esperado 1 sucesso e 300 bytes, obtido %+v", res)
	}
	if _, err := os.Stat(sub); !os.IsNotExist(err) {
		t.Error("a pasta deveria ter sido removida")
	}
}

func TestBatchDeleteLegacyStillWorks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legado.bin")
	if err := os.WriteFile(path, make([]byte, 10), 0o644); err != nil {
		t.Fatal(err)
	}

	res := BatchDelete([]string{path}, map[string]int64{path: 4096}, false)

	if res.SuccessCount != 1 {
		t.Fatalf("BatchDelete devia continuar funcionando: %+v", res)
	}
	if res.FreedBytes != 4096 {
		t.Errorf("FreedBytes = %d: o tamanho informado pelo chamador tem precedência", res.FreedBytes)
	}
	if len(res.Items) != 1 || res.Items[0].Status != StatusDeleted {
		t.Errorf("BatchDelete deve preencher Items: %+v", res.Items)
	}
}

func TestItemSizeIgnoresUnreadableEntries(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.bin"), make([]byte, 7), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ItemSize(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != 7 {
		t.Errorf("ItemSize = %d, esperado 7", got)
	}
}

// TestSendToRecycleBinRoundTrip toca a Lixeira real do usuário, por isso só roda
// sob pedido explícito: SCANFILE_TEST_RECYCLE=1 go test ./pkg/recycle/
func TestSendToRecycleBinRoundTrip(t *testing.T) {
	if os.Getenv("SCANFILE_TEST_RECYCLE") != "1" {
		t.Skip("defina SCANFILE_TEST_RECYCLE=1 para reciclar um arquivo de verdade")
	}
	dir := t.TempDir()
	skipUnlessRecyclable(t, dir)

	path := filepath.Join(dir, "scanfile-teste-lixeira.txt")
	if err := os.WriteFile(path, []byte("teste"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := BatchDeleteItems([]string{path}, true)
	if res.SuccessCount != 1 {
		t.Fatalf("reciclagem falhou: %+v", res)
	}
	if res.Items[0].Status != StatusRecycled {
		t.Errorf("status = %q, esperado %q", res.Items[0].Status, StatusRecycled)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("o arquivo deveria ter saído do disco")
	}
}
