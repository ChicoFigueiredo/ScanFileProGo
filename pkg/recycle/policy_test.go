package recycle

import "testing"

func TestIsProtectedPath(t *testing.T) {
	cases := []struct {
		path      string
		protected bool
		what      string
	}{
		// Volume roots.
		{`C:\`, true, "raiz de volume"},
		{`C:`, true, "raiz de volume sem barra"},
		{`c:/`, true, "raiz de volume com barra invertida"},
		{`D:\`, true, "raiz de outro volume"},
		{`\\srv\share\`, true, "raiz de compartilhamento UNC"},
		{`\\srv\share`, true, "raiz UNC sem barra final"},
		{`\\srv\share\dados\..`, true, "raiz UNC apos limpeza"},
		{`C:\Users\..`, true, "raiz de volume apos limpeza"},
		{`C:\Users\..\Windows`, true, "pasta do Windows apos limpeza"},

		// Windows folder and everything below it.
		{`C:\Windows`, true, "pasta do Windows"},
		{`C:\windows`, true, "pasta do Windows minuscula"},
		{`C:\Windows\`, true, "pasta do Windows com barra"},
		{`C:\Windows\Temp\x`, true, "subarvore do Windows"},
		{`C:\Windows\System32\drivers\etc\hosts`, true, "arquivo dentro do Windows"},
		{`D:\Windows`, true, "pasta Windows em outro volume"},

		// System Volume Information at any depth.
		{`D:\System Volume Information\a`, true, "System Volume Information"},
		{`C:\System Volume Information`, true, "System Volume Information na raiz"},
		{`E:\dados\system volume information\x\y`, true, "System Volume Information aninhada"},

		// Not protected.
		{`C:\Users\x`, false, "pasta de usuario"},
		{`C:\Windows2`, false, "prefixo falso de Windows"},
		{`C:\Windows2\sub`, false, "subpasta de prefixo falso"},
		{`C:\WindowsApps`, false, "outro prefixo falso"},
		{`C:\Users\x\Windows`, false, "pasta chamada Windows fora da raiz"},
		{`D:\System Volume Information2`, false, "prefixo falso de SVI"},
		{`\\srv\share\dados`, false, "pasta dentro do compartilhamento"},
		{`C:\Users\x\arquivo.txt`, false, "arquivo comum"},
	}

	for _, tc := range cases {
		got, reason := IsProtectedPath(tc.path)
		if got != tc.protected {
			t.Errorf("IsProtectedPath(%q) = %v (%s), esperado %v [%s]", tc.path, got, reason, tc.protected, tc.what)
			continue
		}
		if got && reason == "" {
			t.Errorf("IsProtectedPath(%q) protegido sem motivo [%s]", tc.path, tc.what)
		}
		if !got && reason != "" {
			t.Errorf("IsProtectedPath(%q) nao protegido mas devolveu motivo %q", tc.path, reason)
		}
	}
}

func TestIsProtectedPathRejectsRelative(t *testing.T) {
	for _, p := range []string{"", "   ", "relativo\\x", ".", "..", `\sem-volume\x`} {
		if ok, reason := IsProtectedPath(p); !ok || reason == "" {
			t.Errorf("IsProtectedPath(%q) = %v, %q; esperado protegido com motivo", p, ok, reason)
		}
	}
}

func TestIsWithinRoots(t *testing.T) {
	roots := []string{`C:\Users`, `D:\dados\`, `\\srv\share`}

	cases := []struct {
		path string
		want bool
	}{
		{`C:\Users`, true},
		{`C:\Users\`, true},
		{`c:\users\chico\a.txt`, true},
		{`C:/Users/chico`, true},
		{`C:\Users2`, false},
		{`C:\Users2\chico`, false},
		{`C:\Windows`, false},
		{`D:\dados\sub\x`, true},
		{`D:\dados`, true},
		{`D:\dados2`, false},
		{`\\srv\share\a\b`, true},
		{`\\srv\share2\a`, false},
		{`E:\qualquer`, false},
		{`C:\Users\..\Windows\x`, false},
		{`C:\Users\chico\..\..\Windows`, false},
		{``, false},
	}

	for _, tc := range cases {
		if got := IsWithinRoots(tc.path, roots); got != tc.want {
			t.Errorf("IsWithinRoots(%q, %v) = %v, esperado %v", tc.path, roots, got, tc.want)
		}
	}
}

func TestIsWithinRootsEmptyRootsRefusesEverything(t *testing.T) {
	if IsWithinRoots(`C:\Users\chico`, nil) {
		t.Error("sem raizes carregadas nenhum caminho deve ser aceito")
	}
	if IsWithinRoots(`C:\Users\chico`, []string{""}) {
		t.Error("raiz vazia nao deve autorizar caminho algum")
	}
}
