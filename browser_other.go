//go:build !windows

package main

import "os/exec"

// openURLNative abre a URL no navegador padrão fora do Windows. O ScanFile é um
// programa de Windows; isto existe para o `GOOS=linux go build ./...` do CI
// continuar compilando.
func openURLNative(targetURL string) error {
	return exec.Command("xdg-open", targetURL).Start()
}
