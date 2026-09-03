package recycle

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Per-item outcome of a batch operation.
const (
	// StatusRecycled: the item was accepted by the Windows Recycle Bin.
	StatusRecycled = "recycled"
	// StatusDeleted: the item was permanently deleted.
	StatusDeleted = "deleted"
	// StatusRefused: the item was rejected before any change on disk.
	StatusRefused = "refused"
	// StatusFailed: the operation was attempted and the system rejected it.
	StatusFailed = "failed"
)

// ItemResult is the outcome of a single path in a batch operation.
type ItemResult struct {
	Path   string `json:"path"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

// BatchDeleteResult summarizes a batch operation. Items carries one entry per
// requested path, in the same order as the request.
type BatchDeleteResult struct {
	TotalRequested int          `json:"totalRequested"`
	SuccessCount   int          `json:"successCount"`
	FailedCount    int          `json:"failedCount"`
	RefusedCount   int          `json:"refusedCount"`
	FreedBytes     int64        `json:"freedBytes"`
	Errors         []string     `json:"errors,omitempty"`
	Items          []ItemResult `json:"items"`
}

// Preflight reports whether path can actually be moved to the Windows Recycle
// Bin. It refuses Pasta Protegida, volumes without a Recycle Bin (network
// shares, CD-ROM, removable media formatted without one), volumes configured to
// destroy files instead of recycling them, and items larger than the volume's
// Recycle Bin capacity. The reason is a message for the user, in Portuguese.
func Preflight(path string) (bool, string) {
	if protected, reason := IsProtectedPath(path); protected {
		return false, reason
	}
	ok, reason, _ := preflight(path)
	return ok, reason
}

// BatchDeleteItems sends every path to the Recycle Bin (toRecycleBin) or
// permanently deletes it, and reports the outcome of each one. Pasta Protegida
// is always refused, even when the process runs elevated; recycling also runs
// Preflight so a volume that cannot recycle never silently destroys the item.
//
// Scope validation against the Raízes Varridas is the caller's responsibility
// (see IsWithinRoots): this package does not know which roots are loaded.
func BatchDeleteItems(paths []string, toRecycleBin bool) BatchDeleteResult {
	res := BatchDeleteResult{
		TotalRequested: len(paths),
		Items:          make([]ItemResult, 0, len(paths)),
	}

	for _, p := range paths {
		item := ItemResult{Path: p}

		if protected, reason := IsProtectedPath(p); protected {
			item.Status, item.Reason = StatusRefused, reason
			res.RefusedCount++
			res.Items = append(res.Items, item)
			continue
		}

		var size int64
		if toRecycleBin {
			ok, reason, sz := preflight(p)
			if !ok {
				item.Status, item.Reason = StatusRefused, reason
				res.RefusedCount++
				res.Items = append(res.Items, item)
				continue
			}
			size = sz
		} else {
			var err error
			size, err = ItemSize(p)
			if err != nil {
				item.Status, item.Reason = StatusFailed, err.Error()
				res.FailedCount++
				res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", p, err))
				res.Items = append(res.Items, item)
				continue
			}
		}

		var err error
		if toRecycleBin {
			err = SendToRecycleBin(p)
		} else {
			err = DeletePermanent(p)
		}

		if err != nil {
			item.Status, item.Reason = StatusFailed, err.Error()
			res.FailedCount++
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", p, err))
			res.Items = append(res.Items, item)
			continue
		}

		if toRecycleBin {
			item.Status = StatusRecycled
		} else {
			item.Status = StatusDeleted
		}
		res.SuccessCount++
		res.FreedBytes += size
		res.Items = append(res.Items, item)
	}

	return res
}

// BatchDelete is the legacy entry point kept for the current server and MCP
// callers. It delegates to BatchDeleteItems and, when a caller-provided size is
// known for a path, prefers it over the size measured on disk.
func BatchDelete(files []string, fileSizes map[string]int64, toRecycleBin bool) BatchDeleteResult {
	res := BatchDeleteItems(files, toRecycleBin)

	if len(fileSizes) > 0 {
		var freed int64
		for _, item := range res.Items {
			if item.Status != StatusRecycled && item.Status != StatusDeleted {
				continue
			}
			if sz, ok := fileSizes[item.Path]; ok {
				freed += sz
			}
		}
		if freed > 0 {
			res.FreedBytes = freed
		}
	}

	return res
}

// ItemSize returns the size in bytes of a file or, for a folder, the sum of
// every regular file below it. Unreadable entries are skipped instead of
// aborting the walk.
func ItemSize(path string) (int64, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return info.Size(), nil
	}

	var total int64
	err = filepath.WalkDir(path, func(_ string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil //nolint:nilerr // an unreadable entry must not abort the sum
		}
		if d.IsDir() {
			return nil
		}
		fi, statErr := d.Info()
		if statErr != nil {
			return nil
		}
		if fi.Mode().IsRegular() {
			total += fi.Size()
		}
		return nil
	})
	if err != nil {
		return total, err
	}
	return total, nil
}

// capacityRefusal returns a refusal message when an item of size bytes does not
// fit in a Recycle Bin of maxCapacity bytes. A negative maxCapacity means the
// capacity could not be determined and never refuses.
func capacityRefusal(size, maxCapacity int64) string {
	if maxCapacity < 0 || size <= maxCapacity {
		return ""
	}
	return fmt.Sprintf(
		"o item ocupa %s e a Lixeira deste volume comporta no máximo %s: o Windows apagaria o item em vez de reciclá-lo",
		formatBytes(size), formatBytes(maxCapacity),
	)
}

// formatBytes renders a byte count for the user, using the Brazilian decimal
// comma.
func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit && exp < 4; v /= unit {
		div *= unit
		exp++
	}
	value := float64(n) / float64(div)
	suffix := [...]string{"KB", "MB", "GB", "TB", "PB"}[exp]
	return strings.Replace(fmt.Sprintf("%.1f %s", value, suffix), ".", ",", 1)
}
