package scanner

import (
	"os"
	"syscall"
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
