package recycle

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	foDelete = 0x0003

	fofSilent          = 0x0004
	fofNoConfirmation  = 0x0010
	fofAllowUndo       = 0x0040
	fofNoErrorUI       = 0x0400
	fofWantNukeWarning = 0x4000 // partially overrides FOF_NOCONFIRMATION when
	// the shell is about to destroy the item instead of recycling it.
)

// GetDriveType return values.
const (
	driveUnknown   = 0
	driveNoRootDir = 1
	driveRemovable = 2
	driveFixed     = 3
	driveRemote    = 4
	driveCDRom     = 5
	driveRAMDisk   = 6
)

const bitBucketVolumeKey = `Software\Microsoft\Windows\CurrentVersion\Explorer\BitBucket\Volume\`

type shFileOpStructW struct {
	hwnd                  uintptr
	wFunc                 uint32
	pFrom                 *uint16
	pTo                   *uint16
	fFlags                uint16
	fAnyOperationsAborted int32
	hNameMappings         uintptr
	lpszProgressTitle     *uint16
}

var (
	modshell32           = windows.NewLazySystemDLL("shell32.dll")
	procSHFileOperationW = modshell32.NewProc("SHFileOperationW")
)

// shFileOpErrors maps the documented SHFileOperationW result codes we can
// actually explain to the user. Anything else is reported with its code.
var shFileOpErrors = map[uintptr]string{
	0x71:    "os arquivos de origem e destino são o mesmo",
	0x78:    "acesso negado ao item de origem",
	0x7C:    "caminho de origem inválido",
	0x7E:    "o caminho de origem é maior que o limite do Windows",
	0x10000: "erro no destino da operação",
	0x402:   "o item não foi encontrado",
	1223:    "operação cancelada pelo usuário",
}

// SendToRecycleBin moves a file or folder to the Windows Recycle Bin using the
// native Shell API. FOF_WANTNUKEWARNING is set so the shell asks the user
// instead of silently destroying an item that the Recycle Bin cannot hold; if
// the user declines, fAnyOperationsAborted is reported as an error and nothing
// is lost.
func SendToRecycleBin(filePath string) error {
	utf16Path, err := syscall.UTF16FromString(filePath)
	if err != nil {
		return err
	}
	// SHFileOperationW requires a double-null-terminated buffer.
	doubleNullBuffer := append(utf16Path, 0)

	op := shFileOpStructW{
		wFunc:  foDelete,
		pFrom:  &doubleNullBuffer[0],
		fFlags: fofAllowUndo | fofNoConfirmation | fofSilent | fofNoErrorUI | fofWantNukeWarning,
	}

	// The shell expects a stable thread for the duration of the operation.
	runtime.LockOSThread()
	ret, _, _ := procSHFileOperationW.Call(uintptr(unsafe.Pointer(&op)))
	aborted := op.fAnyOperationsAborted != 0
	runtime.UnlockOSThread()
	runtime.KeepAlive(doubleNullBuffer)

	if ret != 0 {
		if msg, ok := shFileOpErrors[ret]; ok {
			return fmt.Errorf("a Lixeira recusou o item: %s", msg)
		}
		return fmt.Errorf("a Lixeira recusou o item (código %d)", ret)
	}
	if aborted {
		return fmt.Errorf("operação cancelada: o item não foi enviado para a Lixeira")
	}

	return nil
}

// DeletePermanent removes a file or a whole folder irreversibly. A read-only
// attribute on the item is cleared before a second and final attempt.
func DeletePermanent(filePath string) error {
	info, err := os.Lstat(filePath)
	if err != nil {
		return err
	}

	remove := os.Remove
	if info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		remove = os.RemoveAll
	}

	if err := remove(filePath); err == nil {
		return nil
	} else if !clearReadOnly(filePath) {
		return err
	}

	return remove(filePath)
}

// clearReadOnly drops FILE_ATTRIBUTE_READONLY and reports whether it changed
// anything.
func clearReadOnly(path string) bool {
	ptr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	attrs, err := windows.GetFileAttributes(ptr)
	if err != nil || attrs&windows.FILE_ATTRIBUTE_READONLY == 0 {
		return false
	}
	return windows.SetFileAttributes(ptr, attrs&^windows.FILE_ATTRIBUTE_READONLY) == nil
}

// preflight answers whether the Recycle Bin of the item's volume can actually
// receive it, and returns the item size measured while checking. Pasta
// Protegida is checked by the exported Preflight before this runs.
func preflight(path string) (bool, string, int64) {
	root, _, ok := splitVolume(path)
	if !ok {
		return false, reasonInvalidPath, 0
	}

	// Checked first: it needs no I/O, so an unreachable network share is
	// refused without waiting for the network to time out.
	switch driveTypeOf(root) {
	case driveFixed, driveRemovable:
		// The only volume types that keep a Recycle Bin.
	case driveRemote:
		return false, "unidades de rede não têm Lixeira do Windows: o item seria apagado sem volta", 0
	case driveCDRom:
		return false, "unidades de CD/DVD não têm Lixeira do Windows", 0
	case driveRAMDisk:
		return false, "discos em memória não têm Lixeira do Windows", 0
	case driveNoRootDir, driveUnknown:
		return false, fmt.Sprintf("volume %s não está disponível", strings.TrimSuffix(root, `\`)), 0
	default:
		return false, fmt.Sprintf("volume %s não tem um tipo reconhecido", strings.TrimSuffix(root, `\`)), 0
	}

	if _, err := os.Lstat(path); err != nil {
		if os.IsNotExist(err) {
			return false, "o item não existe mais neste caminho", 0
		}
		return false, fmt.Sprintf("não foi possível ler o item: %v", err), 0
	}

	if _, err := os.Stat(filepath.Join(root, "$Recycle.Bin")); err != nil {
		return false, fmt.Sprintf("o volume %s não tem Lixeira ($Recycle.Bin): o item seria apagado sem volta", strings.TrimSuffix(root, `\`)), 0
	}

	maxCapacity, nukeOnDelete := recycleBinPolicy(root)
	if nukeOnDelete {
		return false, fmt.Sprintf("a Lixeira do volume %s está configurada para apagar os itens em vez de reciclá-los", strings.TrimSuffix(root, `\`)), 0
	}

	size, err := ItemSize(path)
	if err != nil {
		return false, fmt.Sprintf("não foi possível medir o tamanho do item: %v", err), 0
	}
	if msg := capacityRefusal(size, maxCapacity); msg != "" {
		return false, msg, size
	}

	return true, "", size
}

func driveTypeOf(root string) uint32 {
	ptr, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		return driveUnknown
	}
	return windows.GetDriveType(ptr)
}

// recycleBinPolicy returns the Recycle Bin capacity of a volume in bytes and
// whether the volume is configured to destroy files instead of recycling them.
// A capacity of -1 means "unknown", which never refuses an item.
func recycleBinPolicy(root string) (maxCapacity int64, nukeOnDelete bool) {
	maxCapacity = -1

	if guid, err := volumeGUID(root); err == nil && guid != "" {
		mb, nuke, found := bitBucketSettings(guid)
		nukeOnDelete = nuke
		if found && mb > 0 {
			return mb * 1024 * 1024, nukeOnDelete
		}
	}

	// No explicit setting: Windows sizes the bin as a share of the volume.
	if total := volumeTotalBytes(root); total > 0 {
		return total / 20, nukeOnDelete // 5%
	}

	return maxCapacity, nukeOnDelete
}

// volumeGUID resolves a mount point such as "C:\" into its volume GUID, the key
// under which the Recycle Bin settings are stored.
func volumeGUID(root string) (string, error) {
	rootPtr, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		return "", err
	}
	buf := make([]uint16, 64)
	if err := windows.GetVolumeNameForVolumeMountPoint(rootPtr, &buf[0], uint32(len(buf))); err != nil {
		return "", err
	}

	// "\\?\Volume{xxxxxxxx-...}\" -> "{xxxxxxxx-...}"
	name := syscall.UTF16ToString(buf)
	start := strings.Index(name, "{")
	end := strings.Index(name, "}")
	if start < 0 || end <= start {
		return "", fmt.Errorf("nome de volume inesperado: %q", name)
	}
	return name[start : end+1], nil
}

// bitBucketSettings reads MaxCapacity (in MB) and NukeOnDelete for a volume GUID.
func bitBucketSettings(guid string) (maxCapacityMB int64, nukeOnDelete bool, found bool) {
	k, err := registry.OpenKey(registry.CURRENT_USER, bitBucketVolumeKey+guid, registry.QUERY_VALUE)
	if err != nil {
		return 0, false, false
	}
	defer k.Close()

	if v, _, err := k.GetIntegerValue("MaxCapacity"); err == nil {
		maxCapacityMB = int64(v)
		found = true
	}
	if v, _, err := k.GetIntegerValue("NukeOnDelete"); err == nil && v != 0 {
		nukeOnDelete = true
		found = true
	}
	return maxCapacityMB, nukeOnDelete, found
}

func volumeTotalBytes(root string) int64 {
	ptr, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		return 0
	}
	var freeAvailable, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(ptr, &freeAvailable, &total, &totalFree); err != nil {
		return 0
	}
	if total > 1<<62 {
		return 0
	}
	return int64(total)
}
