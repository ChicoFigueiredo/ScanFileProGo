package scanner

import (
	"strings"
	"testing"
)

// Tabela das heurísticas de Item Pulado. Os casos marcados com `wasBug`
// documentam pastas que a versão anterior pulava indevidamente (achado M1).
func TestClassifyIgnoredName(t *testing.T) {
	cases := []struct {
		name      string
		dirPath   string
		underWSL  bool
		wantSkip  bool
		wantMatch string // trecho esperado no motivo
		wasBug    bool
	}{
		// Nomes internos do Windows: sempre pulados, em qualquer nível.
		{"System Volume Information", `D:\`, false, true, "sistema", false},
		{"$Recycle.Bin", `C:\`, false, true, "sistema", false},
		{"pagefile.sys", `C:\`, false, true, "sistema", false},
		{"hiberfil.sys", `C:\`, false, true, "sistema", false},
		{"swapfile.sys", `C:\`, false, true, "sistema", false},
		{"System Volume Information", `E:\fotos`, false, true, "sistema", false},

		// Pseudo-sistemas do WSL: só dentro de uma Raiz WSL.
		{"proc", `\\wsl$\Ubuntu\`, true, true, "WSL", false},
		{"sys", `\\wsl.localhost\Debian\`, true, true, "WSL", false},
		{"dev", `\\wsl$\Ubuntu\`, true, true, "WSL", false},
		{"mnt", `\\wsl$\Ubuntu\`, true, true, "WSL", false},
		{"kcore", `\\wsl$\Ubuntu\proc`, true, true, "WSL", false},

		// Antes pulados indevidamente em volumes Windows comuns.
		{"dev", `C:\`, false, false, "", true},
		{"sys", `D:\`, false, false, "", true},
		{"proc", `E:\`, false, false, "", true},
		{"mnt", `C:\`, false, false, "", true},
		{"dev", `C:\projetos`, false, false, "", true},
		{"src", `C:\src\src`, false, false, "", true},
		{"Application Data", `C:\Users\x\AppData\Application Data`, false, false, "", true},

		// Nomes comuns nunca são pulados.
		{"Windows", `C:\`, false, false, "", false},
		{"Users", `C:\`, false, false, "", false},
		{"kcore", `C:\projetos`, false, false, "", true},
	}

	for _, c := range cases {
		skip, reason := classifyIgnoredName(c.name, c.dirPath, c.underWSL)
		if skip != c.wantSkip {
			t.Errorf("classifyIgnoredName(%q, %q, wsl=%v) = %v, quer %v (regressão antiga: %v)",
				c.name, c.dirPath, c.underWSL, skip, c.wantSkip, c.wasBug)
			continue
		}
		if skip && !strings.Contains(reason, c.wantMatch) {
			t.Errorf("motivo de %q = %q, esperava conter %q", c.name, reason, c.wantMatch)
		}
		if skip && reason == "" {
			t.Errorf("item pulado %q ficou sem motivo registrado", c.name)
		}
	}
}

func TestExceedsMaxDepth(t *testing.T) {
	shallow := `C:\a\b\c\d`
	if exceedsMaxDepth(shallow) {
		t.Errorf("%q não deveria estourar a profundidade máxima", shallow)
	}

	deep := `C:` + strings.Repeat(`\x`, MaxScanDepth+2)
	if !exceedsMaxDepth(deep) {
		t.Errorf("caminho com %d níveis deveria estourar a profundidade máxima", MaxScanDepth+2)
	}

	limit := `C:` + strings.Repeat(`\x`, MaxScanDepth)
	if exceedsMaxDepth(limit) {
		t.Errorf("caminho no limite exato (%d) não deveria ser pulado", MaxScanDepth)
	}
}

func TestIsWSLRoot(t *testing.T) {
	cases := []struct {
		root   string
		fsName string
		want   bool
	}{
		{`\\wsl$\Ubuntu`, "", true},
		{`\\WSL$\Ubuntu\home`, "", true},
		{`\\wsl.localhost\Debian`, "", true},
		{`Z:\`, "9P", true},
		{`Z:\`, "9p", true},
		{`C:\`, "NTFS", false},
		{`D:\`, "exFAT", false},
		{`C:\wsl`, "NTFS", false}, // pasta chamada wsl não é raiz WSL
		{`\\servidor\compartilhamento`, "NTFS", false},
	}
	for _, c := range cases {
		if got := isWSLRoot(c.root, c.fsName); got != c.want {
			t.Errorf("isWSLRoot(%q, %q) = %v, quer %v", c.root, c.fsName, got, c.want)
		}
	}
}

func TestScanner_SkippedRingIsBounded(t *testing.T) {
	s := NewScanner(NewTreeManager())
	for i := 0; i < maxSkippedRing+250; i++ {
		s.logSkipped(`C:\item`, "motivo de teste")
	}

	if got := s.SkippedCount(); got != int64(maxSkippedRing+250) {
		t.Errorf("SkippedCount = %d, quer %d", got, maxSkippedRing+250)
	}
	entries := s.GetSkipped()
	if len(entries) != maxSkippedRing {
		t.Errorf("anel guardou %d entradas, quer no máximo %d", len(entries), maxSkippedRing)
	}
	if len(entries) > 0 {
		if entries[0].Path != `C:\item` || entries[0].Reason != "motivo de teste" {
			t.Errorf("entrada inesperada no anel: %+v", entries[0])
		}
		if entries[0].Timestamp.IsZero() {
			t.Error("entrada do anel sem timestamp")
		}
	}
}
