//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modshell32BrowserDLL = windows.NewLazySystemDLL("shell32.dll")
	procShellExecuteOpen = modshell32BrowserDLL.NewProc("ShellExecuteW")
)

// openURLNative abre a URL no navegador padrão do sistema com ShellExecuteW e o
// verbo "open" — a mesma API que o Explorer usa quando alguém clica num link.
//
// O caminho anterior era `rundll32 url.dll,FileProtocolHandler <url>`, que faz a
// mesma coisa. A diferença não é funcional, é de reputação: um processo que
// lança rundll32 para executar um handler é um padrão LOLBin catalogado, e a
// heurística comportamental de antivírus pontua isso. Num binário sem assinatura,
// cada ponto conta. Ver docs/ASSINATURA-DE-CODIGO.md.
func openURLNative(targetURL string) error {
	verbPtr, err := syscall.UTF16PtrFromString("open")
	if err != nil {
		return fmt.Errorf("verbo inválido: %w", err)
	}
	urlPtr, err := syscall.UTF16PtrFromString(targetURL)
	if err != nil {
		return fmt.Errorf("URL inválida: %w", err)
	}

	r1, _, callErr := procShellExecuteOpen.Call(
		0, // hwnd: sem janela pai
		uintptr(unsafe.Pointer(verbPtr)),
		uintptr(unsafe.Pointer(urlPtr)),
		0, // parâmetros: nenhum
		0, // diretório de trabalho: o atual
		uintptr(windows.SW_SHOWNORMAL),
	)

	// ShellExecuteW devolve um valor maior que 32 em caso de sucesso; qualquer
	// coisa até 32 é código de erro.
	if r1 <= 32 {
		return fmt.Errorf("ShellExecuteW devolveu %d: %w", r1, callErr)
	}
	return nil
}
