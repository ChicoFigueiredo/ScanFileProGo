//go:build !windows

package scanner

// VolumeFileSystemName não tem equivalente fora do Windows: a detecção de
// Raízes WSL passa a depender só do prefixo do caminho.
func VolumeFileSystemName(path string) string {
	return ""
}
