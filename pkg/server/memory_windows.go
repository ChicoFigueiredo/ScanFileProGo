//go:build windows

package server

import (
	"unsafe"

	"golang.org/x/sys/windows"

	"scanfile/pkg/scanner"
)

// memoryStatusEx mirrors the Win32 MEMORYSTATUSEX structure.
type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

var (
	modkernel32              = windows.NewLazySystemDLL("kernel32.dll")
	procGlobalMemoryStatusEx = modkernel32.NewProc("GlobalMemoryStatusEx")
)

// getSystemPhysicalMemory preenche as métricas de RAM física do host. Vive num
// arquivo _windows.go porque depende de kernel32.dll: mantê-la em server.go
// quebrava `GOOS=linux go build ./...`.
func getSystemPhysicalMemory(payload *scanner.MemoryStatsPayload, allocBytes uint64) {
	if payload == nil {
		return
	}

	var stat memoryStatusEx
	stat.Length = uint32(unsafe.Sizeof(stat))

	ret, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&stat)))
	if ret == 0 || stat.TotalPhys == 0 {
		return
	}

	payload.SystemTotalRAMMB = stat.TotalPhys / (1024 * 1024)
	payload.SystemFreeRAMMB = stat.AvailPhys / (1024 * 1024)
	if stat.TotalPhys >= stat.AvailPhys {
		payload.SystemUsedRAMMB = (stat.TotalPhys - stat.AvailPhys) / (1024 * 1024)
	}
	payload.SystemPercent = float64(stat.MemoryLoad)
	payload.AppPercentOfSys = (float64(allocBytes) / float64(stat.TotalPhys)) * 100.0
}
