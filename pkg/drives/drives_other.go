//go:build !windows

package drives

// GetLogicalDrives returns fallback drive info on non-Windows platforms.
func GetLogicalDrives() ([]DriveInfo, error) {
	return []DriveInfo{
		{
			Letter:          "/",
			VolumeLabel:     "RootFS",
			FileSystem:      "ext4",
			TotalBytes:      100 * 1024 * 1024 * 1024,
			FreeBytes:       50 * 1024 * 1024 * 1024,
			UsedBytes:       50 * 1024 * 1024 * 1024,
			UsedPercent:     50.0,
			DriveType:       DriveTypeFixed,
			IsWSL:           false,
			DefaultSelected: true,
		},
	}, nil
}
