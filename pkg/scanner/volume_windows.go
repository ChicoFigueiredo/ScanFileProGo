//go:build windows

package scanner

import (
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var procGetVolumeInformationW = modkernel32.NewProc("GetVolumeInformationW")

// VolumeFileSystemName devolve o nome do sistema de arquivos do volume que
// contém o caminho (ex.: "NTFS", "exFAT", "9P"). Devolve "" quando não é
// possível determinar.
func VolumeFileSystemName(path string) string {
	rootPath := volumeRootOf(path)
	if rootPath == "" {
		return ""
	}

	ptr, err := syscall.UTF16PtrFromString(rootPath)
	if err != nil {
		return ""
	}

	var (
		volumeName [261]uint16
		fsName     [261]uint16
		serial     uint32
		maxCompLen uint32
		flags      uint32
	)

	ret, _, _ := procGetVolumeInformationW.Call(
		uintptr(unsafe.Pointer(ptr)),
		uintptr(unsafe.Pointer(&volumeName[0])),
		uintptr(len(volumeName)),
		uintptr(unsafe.Pointer(&serial)),
		uintptr(unsafe.Pointer(&maxCompLen)),
		uintptr(unsafe.Pointer(&flags)),
		uintptr(unsafe.Pointer(&fsName[0])),
		uintptr(len(fsName)),
	)
	if ret == 0 {
		return ""
	}
	return syscall.UTF16ToString(fsName[:])
}

// volumeRootOf devolve a raiz do volume com barra final, como exigido por
// GetVolumeInformationW.
func volumeRootOf(path string) string {
	clean := filepath.Clean(path)
	vol := filepath.VolumeName(clean)
	if vol == "" {
		return ""
	}
	if strings.HasPrefix(vol, `\`) {
		// Compartilhamento UNC: \servidor\share precisa da barra final.
		return vol + `\`
	}
	return strings.ToUpper(vol) + `\`
}
