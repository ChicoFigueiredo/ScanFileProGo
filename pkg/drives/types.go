package drives

import "strings"

// Drive type labels shown in the interface and consumed by the UI when deciding
// which volumes may be scanned.
const (
	DriveTypeFixed       = "Fixed (SSD/HDD)"
	DriveTypeRemovable   = "Removable"
	DriveTypeNetwork     = "Network"
	DriveTypeCDRom       = "CD-ROM"
	DriveTypeRAMDisk     = "RAM Disk"
	DriveTypeOther       = "Outro"
	DriveTypeUnavailable = "Desconectado / Timeout"
)

// DriveInfo contains information about a logical storage drive.
type DriveInfo struct {
	Letter      string  `json:"letter"`      // e.g. "C:\\"
	VolumeLabel string  `json:"volumeLabel"` // e.g. "Windows"
	FileSystem  string  `json:"fileSystem"`  // e.g. "NTFS"
	TotalBytes  uint64  `json:"totalBytes"`
	FreeBytes   uint64  `json:"freeBytes"`
	UsedBytes   uint64  `json:"usedBytes"`
	UsedPercent float64 `json:"usedPercent"`
	DriveType   string  `json:"driveType"` // e.g. "Fixed (SSD/HDD)", "Removable", "Network", "CD-ROM"

	// IsWSL marks a WSL pseudo-volume: walking it is pointless and slow, and its
	// pseudo-files break size accounting.
	IsWSL bool `json:"isWSL"`
	// DefaultSelected tells the interface whether the volume starts checked.
	DefaultSelected bool `json:"defaultSelected"`
}

// IsWSLVolume reports whether a volume belongs to the Windows Subsystem for
// Linux: its file system is 9P, or its path lives under \\wsl$ / \\wsl.localhost.
func IsWSLVolume(fileSystem, path string) bool {
	if strings.EqualFold(strings.TrimSpace(fileSystem), "9P") {
		return true
	}
	p := strings.ToLower(strings.ReplaceAll(path, "/", `\`))
	return strings.HasPrefix(p, `\\wsl$`) || strings.HasPrefix(p, `\\wsl.localhost`) || strings.HasPrefix(p, `\\wsl\`)
}

// IsDefaultSelected reports whether a volume should start selected for a
// Varredura. WSL, network and CD-ROM volumes never do, and neither does a
// volume we could not probe.
func IsDefaultSelected(driveType, fileSystem, path string) bool {
	if IsWSLVolume(fileSystem, path) {
		return false
	}
	switch driveType {
	case DriveTypeNetwork, DriveTypeCDRom, DriveTypeUnavailable:
		return false
	}
	return true
}
