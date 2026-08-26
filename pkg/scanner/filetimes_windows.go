//go:build windows

package scanner

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	modkernel32               = syscall.NewLazyDLL("kernel32.dll")
	procGetCompressedFileSize = modkernel32.NewProc("GetCompressedFileSizeW")
)

const (
	FILE_ATTRIBUTE_SPARSE_FILE = 0x00000200
	FILE_ATTRIBUTE_COMPRESSED  = 0x00000800
)

// ExtractFileTimestamps retrieves Modification, Creation and LastAccess times on Windows.
func ExtractFileTimestamps(info os.FileInfo) (modTime, createTime, accessTime int64) {
	modTime = info.ModTime().Unix()
	createTime = modTime
	accessTime = modTime

	if sys := info.Sys(); sys != nil {
		if d, ok := sys.(*syscall.Win32FileAttributeData); ok {
			createTime = d.CreationTime.Nanoseconds() / 1e9
			accessTime = d.LastAccessTime.Nanoseconds() / 1e9
		}
	}
	return
}

// GetAllocatedFileSize returns the physical allocated size on disk and whether the file is NTFS compressed/sparse.
func GetAllocatedFileSize(path string, info os.FileInfo) (allocatedSize int64, isCompressed bool) {
	if info == nil {
		return 0, false
	}
	logicalSize := info.Size()
	allocatedSize = logicalSize

	if sys := info.Sys(); sys != nil {
		if d, ok := sys.(*syscall.Win32FileAttributeData); ok {
			if d.FileAttributes&(FILE_ATTRIBUTE_COMPRESSED|FILE_ATTRIBUTE_SPARSE_FILE) != 0 {
				isCompressed = true
				pathPtr, err := syscall.UTF16PtrFromString(path)
				if err == nil {
					var high uint32
					low, _, _ := procGetCompressedFileSize.Call(uintptr(unsafe.Pointer(pathPtr)), uintptr(unsafe.Pointer(&high)))
					if low != 0xFFFFFFFF || high != 0xFFFFFFFF {
						allocatedSize = int64(high)<<32 | int64(low)
					}
				}
			}
		}
	}
	return
}
