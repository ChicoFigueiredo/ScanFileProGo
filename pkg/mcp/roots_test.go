package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestPathWithinRoots_Table(t *testing.T) {
	roots := []string{`C:\Users`, `D:\Dados\Projetos\`}

	cases := []struct {
		name string
		path string
		want bool
	}{
		{"arquivo direto na raiz", `C:\Users\a.txt`, true},
		{"arquivo profundo", `C:\Users\chico\docs\nota.pdf`, true},
		{"caixa diferente", `c:\USERS\Chico\a.txt`, true},
		{"a própria raiz", `C:\Users`, true},
		{"a própria raiz com barra final", `C:\Users\`, true},
		{"prefixo falso não casa", `C:\Users2\x`, false},
		{"prefixo falso colado", `C:\UsersOutros\x`, false},
		{"outro volume", `E:\Users\a.txt`, false},
		{"segunda raiz", `D:\Dados\Projetos\app\main.go`, true},
		{"prefixo falso da segunda raiz", `D:\Dados\Projetos2\app\main.go`, false},
		{"barra invertida dupla normalizada", `C:\Users\\chico\\a.txt`, true},
		{"barra normal aceita como separador", `C:/Users/chico/a.txt`, true},
		{"escape por dot dot", `C:\Users\..\Windows\system32\x.dll`, false},
		{"caminho vazio", ``, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pathWithinRoots(tc.path, roots); got != tc.want {
				t.Fatalf("pathWithinRoots(%q) = %v, esperado %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestPathWithinRoots_VolumeRoot(t *testing.T) {
	roots := []string{`C:\`}
	if !pathWithinRoots(`C:\Windows\notepad.exe`, roots) {
		t.Fatal("caminho dentro do volume C: deveria ser permitido")
	}
	if pathWithinRoots(`D:\x`, roots) {
		t.Fatal("caminho em outro volume não deveria ser permitido")
	}
}

func TestEnsurePathAllowed_SemRaizes(t *testing.T) {
	tc := NewMCPToolsContext(nil, nil, nil, nil, "")
	_, err := tc.ensurePathAllowed(`C:\qualquer\arquivo.txt`)
	if err == nil {
		t.Fatal("esperado erro quando não há Raízes Varridas carregadas")
	}
	if !strings.Contains(err.Error(), "Raiz Varrida") && !strings.Contains(err.Error(), "Raízes Varridas") {
		t.Fatalf("mensagem deve mencionar Raízes Varridas em pt-BR, obtido: %v", err)
	}
	if !ErrorIsNoRoots(err) {
		t.Fatalf("erro deveria ser classificado como ausência de raízes: %v", err)
	}
}

func TestSetAllowedRoots_CopiaDefensiva(t *testing.T) {
	tc := NewMCPToolsContext(nil, nil, nil, nil, "")
	src := []string{`C:\Users`}
	tc.SetAllowedRoots(src)
	src[0] = `D:\`

	got := tc.AllowedRoots()
	if len(got) != 1 || got[0] != `C:\Users` {
		t.Fatalf("SetAllowedRoots deveria copiar o slice, obtido %v", got)
	}

	got[0] = `Z:\`
	if again := tc.AllowedRoots(); again[0] != `C:\Users` {
		t.Fatalf("AllowedRoots deveria devolver uma cópia, obtido %v", again)
	}
}

func TestMCPToolsContext_ConcorrenciaRaizesEPropostas(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	tc := NewMCPToolsContext(nil, nil, nil, nil, "")
	tc.SetAllowedRoots([]string{dir})

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 25; j++ {
				tc.SetAllowedRoots([]string{dir})
				_ = tc.AllowedRoots()
				if p, err := tc.ProposeActions(ProposeActionParams{ActionType: "MARK_REVIEW", Files: []string{f}}); err == nil {
					_, _ = tc.GetProposal(p.ID)
				}
				_, _ = tc.WriteFileMetadata(WriteMetadataParams{Target: "hash:abc", Category: "x"})
			}
		}()
	}
	wg.Wait()
}

func TestAnalyzeFileContent_RecusaForaDasRaizes(t *testing.T) {
	dir := t.TempDir()
	allowed := filepath.Join(dir, "dentro")
	outside := filepath.Join(dir, "fora")
	if err := os.MkdirAll(allowed, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}

	inFile := filepath.Join(allowed, "ok.txt")
	outFile := filepath.Join(outside, "nao.txt")
	if err := os.WriteFile(inFile, []byte("conteudo permitido"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outFile, []byte("conteudo proibido"), 0o644); err != nil {
		t.Fatal(err)
	}

	tc := NewMCPToolsContext(nil, nil, nil, nil, "")
	tc.SetAllowedRoots([]string{allowed})

	if _, err := tc.AnalyzeFileContent(context.Background(), AnalyzeFileParams{FilePath: inFile}); err != nil {
		t.Fatalf("arquivo dentro da raiz deveria ser lido: %v", err)
	}

	_, err := tc.AnalyzeFileContent(context.Background(), AnalyzeFileParams{FilePath: outFile})
	if err == nil {
		t.Fatal("arquivo fora das Raízes Varridas deveria ser recusado")
	}
	if !strings.Contains(err.Error(), "fora das Raízes Varridas") {
		t.Fatalf("mensagem inesperada: %v", err)
	}
}

func TestAnalyzeImageVisual_RecusaForaDasRaizes(t *testing.T) {
	dir := t.TempDir()
	img := filepath.Join(dir, "img.png")
	if err := os.WriteFile(img, []byte("nao e png de verdade"), 0o644); err != nil {
		t.Fatal(err)
	}

	tc := NewMCPToolsContext(nil, nil, nil, nil, "")
	tc.SetAllowedRoots([]string{filepath.Join(dir, "outra")})

	if _, err := tc.AnalyzeImageVisual(context.Background(), AnalyzeImageParams{FilePath: img}); err == nil {
		t.Fatal("imagem fora das Raízes Varridas deveria ser recusada")
	}
}

func TestCompareVisualSimilarity_RecusaForaDasRaizes(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.png")
	b := filepath.Join(dir, "b.png")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	tc := NewMCPToolsContext(nil, nil, nil, nil, "")
	tc.SetAllowedRoots([]string{dir})
	if _, err := tc.CompareVisualSimilarity(context.Background(), CompareVisualParams{FilePathA: a, FilePathB: b}); err != nil {
		t.Fatalf("ambas dentro da raiz deveriam ser aceitas: %v", err)
	}

	tc.SetAllowedRoots([]string{filepath.Join(dir, "somente-aqui")})
	if _, err := tc.CompareVisualSimilarity(context.Background(), CompareVisualParams{FilePathA: a, FilePathB: b}); err == nil {
		t.Fatal("comparação fora das Raízes Varridas deveria ser recusada")
	}
}

func TestWriteFileMetadata_SidecarExigeRaiz(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "alvo.bin")
	if err := os.WriteFile(target, []byte("dados"), 0o644); err != nil {
		t.Fatal(err)
	}

	tc := NewMCPToolsContext(nil, nil, nil, nil, "")

	// Sem raízes: sidecar recusado.
	if _, err := tc.WriteFileMetadata(WriteMetadataParams{Target: target, Category: "Teste", Sidecar: true}); err == nil {
		t.Fatal("sidecar sem Raízes Varridas deveria ser recusado")
	}
	if _, err := os.Stat(target + ".scanfile_meta.json"); err == nil {
		t.Fatal("nenhum sidecar deveria ter sido gravado")
	}

	// Sem sidecar, a anotação em memória é permitida (o alvo pode ser um hash).
	if _, err := tc.WriteFileMetadata(WriteMetadataParams{Target: "sha256:abc", Category: "Teste"}); err != nil {
		t.Fatalf("metadados em memória não deveriam exigir raízes: %v", err)
	}

	// Com raiz configurada, o sidecar é gravado.
	tc.SetAllowedRoots([]string{dir})
	if _, err := tc.WriteFileMetadata(WriteMetadataParams{Target: target, Category: "Teste", Sidecar: true}); err != nil {
		t.Fatalf("sidecar dentro da raiz deveria funcionar: %v", err)
	}
	if _, err := os.Stat(target + ".scanfile_meta.json"); err != nil {
		t.Fatalf("sidecar deveria existir: %v", err)
	}
}
