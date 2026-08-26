package recycle

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	foDelete          = 0x0003
	fofAllowUndo      = 0x0040
	fofNoConfirmation = 0x0010
	fofSilent         = 0x0004
	fofNoErrorUI      = 0x0400
)

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

// SendToRecycleBin moves a file to the Windows Recycle Bin using the native Win32 Shell API.
func SendToRecycleBin(filePath string) error {
	shell32 := windows.NewLazySystemDLL("shell32.dll")
	shFileOperationW := shell32.NewProc("SHFileOperationW")

	// SHFileOperationW requires a double-null-terminated string buffer
	utf16Path, err := syscall.UTF16FromString(filePath)
	if err != nil {
		return err
	}
	// Append extra null character
	doubleNullBuffer := append(utf16Path, 0)

	op := shFileOpStructW{
		wFunc:  foDelete,
		pFrom:  &doubleNullBuffer[0],
		fFlags: fofAllowUndo | fofNoConfirmation | fofSilent | fofNoErrorUI,
	}

	ret, _, _ := shFileOperationW.Call(uintptr(unsafe.Pointer(&op)))
	if ret != 0 {
		return fmt.Errorf("SHFileOperationW failed with error code: %d", ret)
	}

	if op.fAnyOperationsAborted != 0 {
		return fmt.Errorf("recycle operation was aborted")
	}

	return nil
}

// DeletePermanent permanently deletes a file using os.Remove.
func DeletePermanent(filePath string) error {
	return os.Remove(filePath)
}

// BatchDeleteResult summarizes the batch deletion.
type BatchDeleteResult struct {
	TotalRequested int      `json:"totalRequested"`
	SuccessCount   int      `json:"successCount"`
	FailedCount    int      `json:"failedCount"`
	FreedBytes     int64    `json:"freedBytes"`
	Errors         []string `json:"errors,omitempty"`
}

// BatchDelete processes a list of files with their sizes, sending them to the Recycle Bin or deleting permanently.
func BatchDelete(files []string, fileSizes map[string]int64, toRecycleBin bool) BatchDeleteResult {
	res := BatchDeleteResult{
		TotalRequested: len(files),
	}

	for _, p := range files {
		var err error
		if toRecycleBin {
			err = SendToRecycleBin(p)
		} else {
			err = DeletePermanent(p)
		}

		if err != nil {
			res.FailedCount++
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", p, err))
		} else {
			res.SuccessCount++
			if sz, ok := fileSizes[p]; ok {
				res.FreedBytes += sz
			}
		}
	}

	return res
}
