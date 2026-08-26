package indexer

import (
	"sort"
	"strings"
	"time"

	"scanfile/pkg/scanner"
)

// IdleFileEntry represents a stale file that hasn't been modified or accessed recently.
type IdleFileEntry struct {
	Path         string `json:"path"`
	Name         string `json:"name"`
	Size         int64  `json:"size"`
	ModTime      int64  `json:"modTime"`
	CreateTime   int64  `json:"createTime"`
	AccessTime   int64  `json:"accessTime"`
	DaysInactive int    `json:"daysInactive"`
	Extension    string `json:"extension"`
}

// AgeBucket aggregates count and bytes for an age range.
type AgeBucket struct {
	Label      string `json:"label"`
	MinDays    int    `json:"minDays"`
	MaxDays    int    `json:"maxDays"`
	FileCount  int    `json:"fileCount"`
	TotalBytes int64  `json:"totalBytes"`
}

// IdleFilesSummary holds the overall idle files report.
type IdleFilesSummary struct {
	TotalIdleFiles int              `json:"totalIdleFiles"`
	TotalIdleBytes int64            `json:"totalIdleBytes"`
	Buckets        []AgeBucket      `json:"buckets"`
	TopFiles       []*IdleFileEntry `json:"topFiles"`
}

// QueryIdleFiles scans all in-memory files and identifies large stale/idle data.
func QueryIdleFiles(allFiles []*scanner.FileNode, minAgeDays int, minSizeBytes int64, extension, search string, limit int) IdleFilesSummary {
	nowUnix := time.Now().Unix()

	if minAgeDays <= 0 {
		minAgeDays = 180 // Default: 6 months
	}
	if limit <= 0 {
		limit = 100
	}

	searchLower := strings.ToLower(search)
	extLower := strings.ToLower(extension)

	buckets := []AgeBucket{
		{Label: "Mais de 5 anos", MinDays: 1825, MaxDays: 99999},
		{Label: "2 a 5 anos", MinDays: 730, MaxDays: 1824},
		{Label: "1 a 2 anos", MinDays: 365, MaxDays: 729},
		{Label: "6 meses a 1 ano", MinDays: 180, MaxDays: 364},
	}

	var candidates []*IdleFileEntry
	var totalBytes int64

	for _, f := range allFiles {
		if f.Size < minSizeBytes {
			continue
		}

		if extLower != "" && !strings.EqualFold(f.Extension, extLower) {
			continue
		}

		if searchLower != "" && !strings.Contains(strings.ToLower(f.Path), searchLower) {
			continue
		}

		// Calculate inactivity in days using the latest interaction (max of modTime and accessTime)
		lastInteraction := f.ModTime
		if f.AccessTime > lastInteraction {
			lastInteraction = f.AccessTime
		}

		if lastInteraction <= 0 {
			continue
		}

		diffSeconds := nowUnix - lastInteraction
		if diffSeconds < 0 {
			diffSeconds = 0
		}
		daysInactive := int(diffSeconds / 86400)

		// Aggregate into age buckets
		for i := range buckets {
			if daysInactive >= buckets[i].MinDays && daysInactive <= buckets[i].MaxDays {
				buckets[i].FileCount++
				buckets[i].TotalBytes += f.Size
				break
			}
		}

		if daysInactive >= minAgeDays {
			candidates = append(candidates, &IdleFileEntry{
				Path:         f.Path,
				Name:         f.Name,
				Size:         f.Size,
				ModTime:      f.ModTime,
				CreateTime:   f.CreateTime,
				AccessTime:   f.AccessTime,
				DaysInactive: daysInactive,
				Extension:    f.Extension,
			})
			totalBytes += f.Size
		}
	}

	// Sort by size descending (largest idle files first to reclaim maximum storage)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Size > candidates[j].Size
	})

	top := candidates
	if len(top) > limit {
		top = top[:limit]
	}

	return IdleFilesSummary{
		TotalIdleFiles: len(candidates),
		TotalIdleBytes: totalBytes,
		Buckets:        buckets,
		TopFiles:       top,
	}
}
