package recycle

import "strings"

// Protection reasons returned by IsProtectedPath. They are shown to the user,
// so they are written in Brazilian Portuguese.
const (
	reasonInvalidPath   = "caminho inválido: informe um caminho absoluto de volume (C:\\...) ou de rede (\\\\servidor\\compartilhamento\\...)"
	reasonVolumeRoot    = "raiz de volume é uma Pasta Protegida e nunca pode ser reciclada ou excluída"
	reasonWindowsFolder = "a pasta Windows do volume e todo o seu conteúdo são Pasta Protegida"
	reasonSystemVolume  = "System Volume Information é Pasta Protegida do sistema"
)

const systemVolumeInformation = "system volume information"

// IsProtectedPath reports whether path is a Pasta Protegida: the root of any
// volume, the Windows folder of any volume and everything below it, or any
// "System Volume Information" folder and everything below it. The check is
// case-insensitive, normalizes separators and resolves "." / ".." segments, and
// applies even when the process runs elevated.
//
// A path that is not an absolute Windows path (drive-qualified or UNC) is also
// reported as protected: the caller cannot prove where it points to.
func IsProtectedPath(path string) (bool, string) {
	_, rest, ok := splitVolume(path)
	if !ok {
		return true, reasonInvalidPath
	}

	segments := splitSegments(rest)
	if len(segments) == 0 {
		return true, reasonVolumeRoot
	}

	if strings.EqualFold(segments[0], "Windows") {
		return true, reasonWindowsFolder
	}

	for _, seg := range segments {
		if strings.EqualFold(seg, systemVolumeInformation) {
			return true, reasonSystemVolume
		}
	}

	return false, ""
}

// IsWithinRoots reports whether path is inside at least one of roots. The
// comparison is case-insensitive, normalizes separators, resolves "." and ".."
// and only accepts matches on a separator boundary, so "C:\Users2" is never
// considered to be inside "C:\Users". An empty roots list accepts nothing.
func IsWithinRoots(path string, roots []string) bool {
	target, ok := normalizePath(path)
	if !ok {
		return false
	}

	for _, r := range roots {
		root, ok := normalizePath(r)
		if !ok {
			continue
		}
		if strings.EqualFold(target, root) {
			return true
		}
		prefix := root
		if !strings.HasSuffix(prefix, `\`) {
			prefix += `\`
		}
		if len(target) > len(prefix) && strings.EqualFold(target[:len(prefix)], prefix) {
			return true
		}
	}

	return false
}

// normalizePath returns the canonical form of a Windows path: separators turned
// into backslashes, "." and ".." resolved, redundant separators removed, no
// trailing separator except on a volume root. It reports false when path is not
// an absolute drive-qualified or UNC path.
func normalizePath(path string) (string, bool) {
	root, rest, ok := splitVolume(path)
	if !ok {
		return "", false
	}
	segments := splitSegments(rest)
	if len(segments) == 0 {
		return root, true
	}
	return root + strings.Join(segments, `\`), true
}

// splitVolume separates the volume root ("C:\" or "\\server\share\") from the
// remainder of the path. It reports false when the path has no volume.
func splitVolume(path string) (root string, rest string, ok bool) {
	p := strings.TrimSpace(path)
	if p == "" {
		return "", "", false
	}
	p = strings.ReplaceAll(p, "/", `\`)

	// UNC: \\server\share\...
	if strings.HasPrefix(p, `\\`) {
		body := strings.TrimLeft(p, `\`)
		parts := strings.SplitN(body, `\`, 3)
		if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
			return "", "", false
		}
		root = `\\` + parts[0] + `\` + parts[1] + `\`
		if len(parts) == 3 {
			rest = parts[2]
		}
		return root, rest, true
	}

	// Drive-qualified: C:\...
	if len(p) >= 2 && p[1] == ':' && isDriveLetter(p[0]) {
		root = strings.ToUpper(p[:1]) + `:\`
		rest = strings.TrimPrefix(p[2:], `\`)
		// "C:rel" is relative to the drive's current directory: not acceptable.
		if len(p) > 2 && p[2] != '\\' {
			return "", "", false
		}
		return root, rest, true
	}

	return "", "", false
}

// splitSegments breaks the part of a path after the volume root into segments,
// dropping empty and "." segments and resolving ".." against the parent. A ".."
// that would escape the volume root is dropped, so the result never leaves the
// volume.
func splitSegments(rest string) []string {
	if rest == "" {
		return nil
	}
	var out []string
	for _, raw := range strings.Split(rest, `\`) {
		seg := strings.TrimSpace(raw)
		switch seg {
		case "", ".":
			continue
		case "..":
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
		default:
			out = append(out, seg)
		}
	}
	return out
}

func isDriveLetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
