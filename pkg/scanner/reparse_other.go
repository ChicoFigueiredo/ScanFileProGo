//go:build !windows

package scanner

import "os"

// isReparsePoint fallback on non-Windows platforms.
func isReparsePoint(info os.FileInfo) bool {
	return false
}
