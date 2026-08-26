//go:build !windows

package drives

// DriveInfo contains information about a logical storage drive.
type DriveInfo struct {
	Letter      string  `json:"letter"`      // e.g. "/"
	VolumeLabel string  `json:"volumeLabel"` // e.g. "Root"
	FileSystem  string  `json:"fileSystem"`  // e.g. "ext4"
	TotalBytes  uint64  `json:"totalBytes"`
	FreeBytes   uint64  `json:"freeBytes"`
	UsedBytes   uint64  `json:"usedBytes"`
	UsedPercent float64 `json:"usedPercent"`
	DriveType   string  `json:"driveType"`
}

// GetLogicalDrives returns fallback drive info on non-Windows platforms.
func GetLogicalDrives() ([]DriveInfo, error) {
	return []DriveInfo{
		{
			Letter:      "/",
			VolumeLabel: "RootFS",
			FileSystem:  "ext4",
			TotalBytes:  100 * 1024 * 1024 * 1024,
			FreeBytes:   50 * 1024 * 1024 * 1024,
			UsedBytes:   50 * 1024 * 1024 * 1024,
			UsedPercent: 50.0,
			DriveType:   "Fixed",
		},
	}, nil
}
