//go:build !windows

package scanner

import "os"

// ExtractFileTimestamps fallback for non-Windows platforms.
func ExtractFileTimestamps(info os.FileInfo) (modTime, createTime, accessTime int64) {
	if info == nil {
		return 0, 0, 0
	}
	modTime = info.ModTime().Unix()
	return modTime, modTime, modTime
}

// GetAllocatedFileSize fallback for non-Windows platforms.
func GetAllocatedFileSize(path string, info os.FileInfo) (allocatedSize int64, isCompressed bool) {
	if info == nil {
		return 0, false
	}
	return info.Size(), false
}
