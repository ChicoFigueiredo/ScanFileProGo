package mcp

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ErrNoAllowedRoots is returned whenever a file-reading tool is invoked before any
// Raiz Varrida has been loaded into the context.
var ErrNoAllowedRoots = errors.New("nenhuma Raiz Varrida carregada: execute uma Varredura ou carregue um Snapshot antes de usar ferramentas que leem arquivos")

// ErrorIsNoRoots reports whether err was caused by the absence of Raízes Varridas.
func ErrorIsNoRoots(err error) bool {
	return errors.Is(err, ErrNoAllowedRoots)
}

// SetAllowedRoots replaces the set of Raízes Varridas the tools may read from.
// Passing nil or an empty slice makes every file-reading tool refuse.
func (tc *MCPToolsContext) SetAllowedRoots(roots []string) {
	cleaned := make([]string, 0, len(roots))
	for _, r := range roots {
		if strings.TrimSpace(r) == "" {
			continue
		}
		cleaned = append(cleaned, r)
	}

	tc.rootsMu.Lock()
	tc.allowedRoots = cleaned
	tc.rootsMu.Unlock()
}

// AllowedRoots returns a copy of the currently configured Raízes Varridas.
func (tc *MCPToolsContext) AllowedRoots() []string {
	tc.rootsMu.RLock()
	defer tc.rootsMu.RUnlock()

	out := make([]string, len(tc.allowedRoots))
	copy(out, tc.allowedRoots)
	return out
}

// ensurePathAllowed validates that path lives inside one of the Raízes Varridas and
// returns its cleaned form. It refuses when no root is loaded.
func (tc *MCPToolsContext) ensurePathAllowed(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("caminho vazio: informe o caminho absoluto do arquivo")
	}

	roots := tc.AllowedRoots()
	if len(roots) == 0 {
		return "", ErrNoAllowedRoots
	}

	clean := filepath.Clean(path)
	if !pathWithinRoots(path, roots) {
		return "", fmt.Errorf("caminho fora das Raízes Varridas (acesso negado): %s. Raízes permitidas: %s", clean, strings.Join(roots, ", "))
	}

	return clean, nil
}

// pathWithinRoots reports whether path is inside at least one of roots.
// Comparison is case-insensitive, uses filepath.Clean and respects separator
// boundaries, so `C:\Users2\x` is NOT inside `C:\Users`.
func pathWithinRoots(path string, roots []string) bool {
	target := normalizePathForCompare(path)
	if target == "" {
		return false
	}

	for _, root := range roots {
		base := normalizePathForCompare(root)
		if base == "" {
			continue
		}
		if target == base {
			return true
		}
		prefix := base
		if !strings.HasSuffix(prefix, compareSeparator) {
			prefix += compareSeparator
		}
		if strings.HasPrefix(target, prefix) {
			return true
		}
	}

	return false
}

// compareSeparator is the canonical separator used while comparing paths.
const compareSeparator = `\`

// normalizePathForCompare canonicalises a path for containment checks: it applies
// filepath.Clean, folds both separators into a single canonical one, lowercases the
// result and drops any trailing separator (except for a bare root).
func normalizePathForCompare(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}

	// Fold alternate separators before cleaning so that Clean can collapse
	// duplicated separators and resolve "." / ".." on every platform.
	p = strings.ReplaceAll(p, "/", compareSeparator)
	p = strings.ReplaceAll(p, string(filepath.Separator), compareSeparator)
	p = collapseSeparators(p)
	p = cleanWindowsStyle(p)
	p = strings.ToLower(p)

	for len(p) > 1 && strings.HasSuffix(p, compareSeparator) {
		p = p[:len(p)-1]
	}

	return p
}

func collapseSeparators(p string) string {
	// Preserve a leading UNC "\\" prefix, collapse every other repetition.
	prefix := ""
	if strings.HasPrefix(p, compareSeparator+compareSeparator) {
		prefix = compareSeparator + compareSeparator
		p = strings.TrimLeft(p, compareSeparator)
	}
	for strings.Contains(p, compareSeparator+compareSeparator) {
		p = strings.ReplaceAll(p, compareSeparator+compareSeparator, compareSeparator)
	}
	return prefix + p
}

// cleanWindowsStyle resolves "." and ".." segments treating `\` as the separator,
// independently of the host operating system.
func cleanWindowsStyle(p string) string {
	prefix := ""
	rest := p

	switch {
	case strings.HasPrefix(rest, compareSeparator+compareSeparator):
		prefix = compareSeparator + compareSeparator
		rest = rest[2:]
	case len(rest) >= 2 && rest[1] == ':':
		prefix = rest[:2]
		rest = rest[2:]
		if strings.HasPrefix(rest, compareSeparator) {
			prefix += compareSeparator
			rest = rest[1:]
		}
	case strings.HasPrefix(rest, compareSeparator):
		prefix = compareSeparator
		rest = rest[1:]
	}

	rooted := prefix != "" && strings.HasSuffix(prefix, compareSeparator)

	var out []string
	for _, seg := range strings.Split(rest, compareSeparator) {
		switch seg {
		case "", ".":
			continue
		case "..":
			if len(out) > 0 && out[len(out)-1] != ".." {
				out = out[:len(out)-1]
			} else if !rooted {
				out = append(out, "..")
			}
		default:
			out = append(out, seg)
		}
	}

	joined := strings.Join(out, compareSeparator)
	if prefix == "" && joined == "" {
		return "."
	}
	return prefix + joined
}
