//go:build !windows

package recycle

import (
	"fmt"
	"os"
)

// SendToRecycleBin moves a file to recycle or removes on non-Windows.
func SendToRecycleBin(filePath string) error {
	return os.Remove(filePath)
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

// BatchDelete processes a list of files with their sizes.
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
