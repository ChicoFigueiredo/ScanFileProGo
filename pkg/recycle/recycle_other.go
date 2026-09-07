//go:build !windows

package recycle

import (
	"errors"
	"os"
)

// errNoRecycleBin is returned instead of quietly destroying a file: outside
// Windows there is no Recycle Bin to restore anything from.
var errNoRecycleBin = errors.New("a Lixeira do Windows não existe nesta plataforma: o item não foi apagado")

// SendToRecycleBin always fails outside Windows. Removing the file here would
// turn a Reciclagem into an Exclusão Permanente, which the product forbids.
func SendToRecycleBin(filePath string) error {
	return errNoRecycleBin
}

// DeletePermanent removes a file or a whole folder irreversibly.
func DeletePermanent(filePath string) error {
	info, err := os.Lstat(filePath)
	if err != nil {
		return err
	}
	if info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		return os.RemoveAll(filePath)
	}
	return os.Remove(filePath)
}

// preflight always refuses: there is no Recycle Bin on this platform.
func preflight(path string) (bool, string, int64) {
	return false, errNoRecycleBin.Error(), 0
}
