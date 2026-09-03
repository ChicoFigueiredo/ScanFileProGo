package drives

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	modmpr                  = windows.NewLazySystemDLL("mpr.dll")
	procWNetGetConnectionW  = modmpr.NewProc("WNetGetConnectionW")
	procWNetAddConnection2W = modmpr.NewProc("WNetAddConnection2W")
)

// GetLogicalDrives retrieves all mounted logical drives on Windows safely and concurrently.
// It uses per-drive timeouts so disconnected network shares or slow devices never hang the application.
func GetLogicalDrives() ([]DriveInfo, error) {
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	getLogicalDriveStringsW := kernel32.NewProc("GetLogicalDriveStringsW")
	getDriveTypeW := kernel32.NewProc("GetDriveTypeW")

	candidateLetters := make(map[string]bool)

	// Step 1: Query logical drive strings from kernel32
	r1, _, _ := getLogicalDriveStringsW.Call(0, 0)
	if r1 > 0 {
		buf := make([]uint16, r1)
		r1, _, _ = getLogicalDriveStringsW.Call(uintptr(len(buf)), uintptr(unsafe.Pointer(&buf[0])))
		if r1 > 0 {
			var current []uint16
			for _, c := range buf {
				if c == 0 {
					if len(current) > 0 {
						p := strings.ToUpper(syscall.UTF16ToString(current))
						if !strings.HasSuffix(p, "\\") {
							p += "\\"
						}
						candidateLetters[p] = true
						current = nil
					}
				} else {
					current = append(current, c)
				}
			}
		}
	}

	// Step 2: Query User Mapped Network Drives from Registry (HKCU\Network)
	netDrives := discoverMappedNetworkDrivesFromRegistry()
	for _, nd := range netDrives {
		p := strings.ToUpper(nd.Letter)
		if !strings.HasSuffix(p, "\\") {
			p += "\\"
		}
		candidateLetters[p] = true
	}

	// If no candidate drives found from API, fallback to checking standard letters C through Z
	if len(candidateLetters) == 0 {
		candidateLetters["C:\\"] = true
		for r := 'D'; r <= 'Z'; r++ {
			candidateLetters[fmt.Sprintf("%c:\\", r)] = true
		}
	}

	// Step 3: Probe each candidate drive concurrently with a strict timeout
	var mu sync.Mutex
	var results []DriveInfo
	var wg sync.WaitGroup

	for letter := range candidateLetters {
		drivePath := letter
		wg.Add(1)
		go func() {
			defer wg.Done()

			infoChan := make(chan DriveInfo, 1)
			go func() {
				info, ok := probeSingleDrive(drivePath, getDriveTypeW)
				if ok {
					infoChan <- info
				} else {
					infoChan <- DriveInfo{}
				}
			}()

			select {
			case info := <-infoChan:
				if info.Letter != "" {
					mu.Lock()
					results = append(results, info)
					mu.Unlock()
				}
			case <-time.After(1500 * time.Millisecond):
				// Timeout on this specific drive (e.g. disconnected network mount or sleeping disk)
				mu.Lock()
				results = append(results, DriveInfo{
					Letter:          drivePath,
					VolumeLabel:     "Unidade Indisponível",
					FileSystem:      "N/A",
					DriveType:       DriveTypeUnavailable,
					DefaultSelected: false,
				})
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	// Ensure always sorted by letter
	sort.Slice(results, func(i, j int) bool {
		return results[i].Letter < results[j].Letter
	})

	// Absolute safety fallback: if nothing returned, at least provide C:\
	if len(results) == 0 {
		results = append(results, DriveInfo{
			Letter:          "C:\\",
			VolumeLabel:     "Disco Local (C:)",
			FileSystem:      "NTFS",
			DriveType:       DriveTypeFixed,
			DefaultSelected: true,
		})
	}

	return results, nil
}

type mappedNetDrive struct {
	Letter     string
	RemotePath string
}

// discoverMappedNetworkDrivesFromRegistry inspects HKCU\Network without any network I/O.
func discoverMappedNetworkDrivesFromRegistry() []mappedNetDrive {
	var list []mappedNetDrive

	k, err := registry.OpenKey(registry.CURRENT_USER, `Network`, registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE)
	if err != nil {
		return list
	}
	defer k.Close()

	subkeys, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return list
	}

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

	return list
}

func probeSingleDrive(drivePath string, getDriveTypeProc *windows.LazyProc) (DriveInfo, bool) {
	drivePtr, err := syscall.UTF16PtrFromString(drivePath)
	if err != nil {
		return DriveInfo{}, false
	}

	// 1. Query Drive Type
	driveTypeVal, _, _ := getDriveTypeProc.Call(uintptr(unsafe.Pointer(drivePtr)))
	// 0 = DRIVE_UNKNOWN, 1 = DRIVE_NO_ROOT_DIR
	if driveTypeVal <= 1 {
		return DriveInfo{}, false
	}

	var driveTypeStr string
	switch driveTypeVal {
	case 2:
		driveTypeStr = DriveTypeRemovable
	case 3:
		driveTypeStr = DriveTypeFixed
	case 4:
		driveTypeStr = DriveTypeNetwork
	case 5:
		driveTypeStr = DriveTypeCDRom
	case 6:
		driveTypeStr = DriveTypeRAMDisk
	default:
		driveTypeStr = DriveTypeOther
	}

	// 2. Query Volume Info (safe, non-failing)
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
	if volumeLabel == "" {
		if driveTypeVal == 3 {
			volumeLabel = fmt.Sprintf("Disco Local (%s)", strings.TrimSuffix(drivePath, "\\"))
		} else {
			volumeLabel = driveTypeStr
		}
	}
	if fileSystem == "" {
		fileSystem = "NTFS"
	}

	// 3. Query Free Space (safe, non-failing)
	var freeBytesAvailable, totalBytes, totalFreeBytes uint64
	_ = windows.GetDiskFreeSpaceEx(
		drivePtr,
		&freeBytesAvailable,
		&totalBytes,
		&totalFreeBytes,
	)

	var usedBytes uint64
	var usedPercent float64
	if totalBytes > 0 {
		usedBytes = totalBytes - totalFreeBytes
		usedPercent = (float64(usedBytes) / float64(totalBytes)) * 100.0
	}

	return DriveInfo{
		Letter:          drivePath,
		VolumeLabel:     volumeLabel,
		FileSystem:      fileSystem,
		TotalBytes:      totalBytes,
		FreeBytes:       totalFreeBytes,
		UsedBytes:       usedBytes,
		UsedPercent:     usedPercent,
		DriveType:       driveTypeStr,
		IsWSL:           IsWSLVolume(fileSystem, drivePath),
		DefaultSelected: IsDefaultSelected(driveTypeStr, fileSystem, drivePath),
	}, true
}
