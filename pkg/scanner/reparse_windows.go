//go:build windows

package scanner

import (
	"os"
	"syscall"
)

// isReparsePoint checks if a directory or file is an NTFS Junction Point or Reparse Point on Windows.
func isReparsePoint(info os.FileInfo) bool {
	if info == nil {
		return false
	}
	if winData, ok := info.Sys().(*syscall.Win32FileAttributeData); ok && winData != nil {
		return winData.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
	}
	return false
}
