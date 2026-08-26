//go:build !windows

package scanner

import "os"

// ExtractFileTimestamps fallback for non-Windows platforms.
func ExtractFileTimestamps(info os.FileInfo) (modTime, createTime, accessTime int64) {
	modTime = info.ModTime().Unix()
	createTime = modTime
	accessTime = modTime
	return
}
