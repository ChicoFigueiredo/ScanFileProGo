//go:build !windows

package server

import "scanfile/pkg/scanner"

// getSystemPhysicalMemory não tem implementação portátil fora do Windows, que é
// a plataforma alvo do ScanFile. Fora dele os campos de RAM do host ficam
// zerados: a interface mostra apenas as métricas do processo, sem inventar
// números para o sistema operacional.
func getSystemPhysicalMemory(payload *scanner.MemoryStatsPayload, allocBytes uint64) {
	_ = payload
	_ = allocBytes
}
