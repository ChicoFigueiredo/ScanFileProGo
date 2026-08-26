package drives

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
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
	DriveType   string  `json:"driveType"`   // e.g. "Fixed", "Removable", "Network", "CD-ROM"
}

var (
	modmpr                  = windows.NewLazySystemDLL("mpr.dll")
	procWNetGetConnectionW  = modmpr.NewProc("WNetGetConnectionW")
	procWNetAddConnection2W = modmpr.NewProc("WNetAddConnection2W")
)

// GetLogicalDrives retrieves all mounted logical drives on Windows.
// It also bridges mapped network drives from standard user sessions into elevated Administrator sessions.
func GetLogicalDrives() ([]DriveInfo, error) {
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	getLogicalDriveStringsW := kernel32.NewProc("GetLogicalDriveStringsW")
	getDriveTypeW := kernel32.NewProc("GetDriveTypeW")

	// Step 1: Query logical drives via API
	r1, _, err := getLogicalDriveStringsW.Call(0, 0)
	if r1 == 0 {
		return nil, fmt.Errorf("failed to get drive strings buffer size: %w", err)
	}

	buf := make([]uint16, r1)
	r1, _, err = getLogicalDriveStringsW.Call(uintptr(len(buf)), uintptr(unsafe.Pointer(&buf[0])))
	if r1 == 0 {
		return nil, fmt.Errorf("failed to get drive strings: %w", err)
	}

	seenLetters := make(map[string]bool)
	var drives []DriveInfo
	var currentDrive []uint16

	for _, c := range buf {
		if c == 0 {
			if len(currentDrive) > 0 {
				drivePath := syscall.UTF16ToString(currentDrive)
				upper := strings.ToUpper(drivePath)
				seenLetters[upper] = true
				info, err := getDriveDetails(drivePath, getDriveTypeW)
				if err == nil {
					drives = append(drives, info)
				}
				currentDrive = nil
			}
		} else {
			currentDrive = append(currentDrive, c)
		}
	}

	// Step 2: Query User Network Mappings in Registry (HKCU\Network)
	// In elevated admin sessions, Windows isolates network drives. This ensures L:\, Z:\ etc. remain mapped!
	networkDrives := discoverMappedNetworkDrives()
	for _, nd := range networkDrives {
		normalizedPath := strings.ToUpper(nd.Letter)
		if !strings.HasSuffix(normalizedPath, "\\") {
			normalizedPath += "\\"
		}

		if !seenLetters[normalizedPath] {
			// Attempt to link connection for elevated session
			if nd.RemotePath != "" {
				_ = linkNetworkDrive(nd.Letter, nd.RemotePath)
			}

			info, err := getDriveDetails(normalizedPath, getDriveTypeW)
			if err == nil {
				if info.VolumeLabel == "" && nd.RemotePath != "" {
					info.VolumeLabel = nd.RemotePath
				}
				info.DriveType = "Network"
				drives = append(drives, info)
				seenLetters[normalizedPath] = true
			}
		}
	}

	return drives, nil
}

type mappedNetDrive struct {
	Letter     string
	RemotePath string
}

func discoverMappedNetworkDrives() []mappedNetDrive {
	var list []mappedNetDrive

	// Check HKCU\Network
	k, err := registry.OpenKey(registry.CURRENT_USER, `Network`, registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE)
	if err == nil {
		defer k.Close()
		subkeys, err := k.ReadSubKeyNames(-1)
		if err == nil {
			for _, sub := range subkeys {
				if len(sub) == 1 {
					letter := strings.ToUpper(sub) + ":"
					subKey, err := registry.OpenKey(k, sub, registry.QUERY_VALUE)
					if err == nil {
						remotePath, _, _ := subKey.GetStringValue("RemotePath")
						subKey.Close()
						list = append(list, mappedNetDrive{
							Letter:     letter,
							RemotePath: remotePath,
						})
					}
				}
			}
		}
	}

	// Check letters A-Z via WNetGetConnection
	for r := 'C'; r <= 'Z'; r++ {
		letter := fmt.Sprintf("%c:", r)
		if remote, ok := getWNetRemotePath(letter); ok {
			alreadyInList := false
			for _, item := range list {
				if strings.EqualFold(item.Letter, letter) {
					alreadyInList = true
					break
				}
			}
			if !alreadyInList {
				list = append(list, mappedNetDrive{
					Letter:     letter,
					RemotePath: remote,
				})
			}
		}
	}

	return list
}

func getWNetRemotePath(localDrive string) (string, bool) {
	drivePtr, err := syscall.UTF16PtrFromString(localDrive)
	if err != nil {
		return "", false
	}

	var buf [512]uint16
	var bufLen uint32 = uint32(len(buf))

	r1, _, _ := procWNetGetConnectionW.Call(
		uintptr(unsafe.Pointer(drivePtr)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bufLen)),
	)

	if r1 == 0 {
		return syscall.UTF16ToString(buf[:]), true
	}
	return "", false
}

func linkNetworkDrive(localDrive, remotePath string) error {
	type NETRESOURCE struct {
		Scope       uint32
		Type        uint32
		DisplayType uint32
		Usage       uint32
		LocalName   *uint16
		RemoteName  *uint16
		Comment     *uint16
		Provider    *uint16
	}

	localPtr, _ := syscall.UTF16PtrFromString(localDrive)
	remotePtr, _ := syscall.UTF16PtrFromString(remotePath)

	nr := NETRESOURCE{
		Type:       1, // RESOURCETYPE_DISK
		LocalName:  localPtr,
		RemoteName: remotePtr,
	}

	r1, _, err := procWNetAddConnection2W.Call(
		uintptr(unsafe.Pointer(&nr)),
		0, // Password (use existing session)
		0, // User (use existing session)
		0, // Flags
	)

	if r1 == 0 || r1 == 1219 || r1 == 85 { // 1219 = multiple credentials, 85 = already assigned
		return nil
	}
	return err
}

func getDriveDetails(drivePath string, getDriveTypeProc *windows.LazyProc) (DriveInfo, error) {
	drivePtr, err := syscall.UTF16PtrFromString(drivePath)
	if err != nil {
		return DriveInfo{}, err
	}

	// Drive Type
	driveTypeVal, _, _ := getDriveTypeProc.Call(uintptr(unsafe.Pointer(drivePtr)))
	var driveTypeStr string
	switch driveTypeVal {
	case 2:
		driveTypeStr = "Removable"
	case 3:
		driveTypeStr = "Fixed (SSD/HDD)"
	case 4:
		driveTypeStr = "Network"
	case 5:
		driveTypeStr = "CD-ROM"
	case 6:
		driveTypeStr = "RAM Disk"
	default:
		driveTypeStr = "Unknown"
	}

	// Volume information
	var volumeNameBuf [260]uint16
	var fileSystemNameBuf [260]uint16
	var serialNumber, maxComponentLen, fsFlags uint32

	_ = windows.GetVolumeInformation(
		drivePtr,
		&volumeNameBuf[0],
		uint32(len(volumeNameBuf)),
		&serialNumber,
		&maxComponentLen,
		&fsFlags,
		&fileSystemNameBuf[0],
		uint32(len(fileSystemNameBuf)),
	)

	volumeLabel := syscall.UTF16ToString(volumeNameBuf[:])
	fileSystem := syscall.UTF16ToString(fileSystemNameBuf[:])

	// Free and total space
	var freeBytesAvailable, totalBytes, totalFreeBytes uint64
	err = windows.GetDiskFreeSpaceEx(
		drivePtr,
		&freeBytesAvailable,
		&totalBytes,
		&totalFreeBytes,
	)

	var usedBytes uint64
	var usedPercent float64
	if err == nil && totalBytes > 0 {
		usedBytes = totalBytes - totalFreeBytes
		usedPercent = (float64(usedBytes) / float64(totalBytes)) * 100.0
	}

	return DriveInfo{
		Letter:      drivePath,
		VolumeLabel: volumeLabel,
		FileSystem:  fileSystem,
		TotalBytes:  totalBytes,
		FreeBytes:   totalFreeBytes,
		UsedBytes:   usedBytes,
		UsedPercent: usedPercent,
		DriveType:   driveTypeStr,
	}, nil
}
