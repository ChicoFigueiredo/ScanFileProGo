//go:build !windows

package ai

// detectTotalMemoryBytes has no portable implementation outside Windows, which is
// the target platform of the ScanFile. Returning 0 means "unknown" and makes the
// catalogue fall back to its ~14 GB ceiling when deciding FitsMemory.
func detectTotalMemoryBytes() int64 {
	return 0
}
