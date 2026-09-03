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
	InactiveDays int    `json:"inactiveDays"` // Compatibility alias for frontend
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
	AgeBuckets     []AgeBucket      `json:"ageBuckets"` // Compatibility alias
	TopFiles       []*IdleFileEntry `json:"topFiles"`
	Offset         int              `json:"offset"`
	Limit          int              `json:"limit"`
}

// QueryIdleFilesStreaming scans files in the tree in-place without duplicating 50M slices and returns paginated results.
func QueryIdleFilesStreaming(tree *scanner.TreeManager, minAgeDays int, minSizeBytes int64, extension, search, sortBy string, offset, limit int) IdleFilesSummary {
	nowUnix := time.Now().Unix()

	if minAgeDays <= 0 {
		minAgeDays = 180 // Default: 6 months
	}
	if limit <= 0 {
		limit = 50
	} else if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
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

	tree.IterateFiles(func(f *scanner.FileNode) bool {
		if f.Size < minSizeBytes {
			return true
		}

		if extLower != "" && !strings.EqualFold(f.Extension(), extLower) {
			return true
		}

		if searchLower != "" && !strings.Contains(strings.ToLower(f.Path()), searchLower) {
			return true
		}

		lastInteraction := f.ModTime()
		if f.AccessTime() > lastInteraction {
			lastInteraction = f.AccessTime()
		}

		if lastInteraction <= 0 {
			return true
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
				Path:         f.Path(),
				Name:         f.Name(),
				Size:         f.Size,
				ModTime:      f.ModTime(),
				CreateTime:   f.CreateTime(),
				AccessTime:   f.AccessTime(),
				DaysInactive: daysInactive,
				InactiveDays: daysInactive,
				Extension:    f.Extension(),
			})
			totalBytes += f.Size
		}
		return true
	})

	// Sorting
	switch sortBy {
	case "days_desc":
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].DaysInactive > candidates[j].DaysInactive
		})
	case "name_asc":
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].Name < candidates[j].Name
		})
	case "size_desc":
		fallthrough
	default:
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].Size > candidates[j].Size
		})
	}

	totalCount := len(candidates)
	start := offset
	if start > totalCount {
		start = totalCount
	}
	end := totalCount
	if limit > 0 && start+limit < end {
		end = start + limit
	}

	paginated := candidates[start:end]

	return IdleFilesSummary{
		TotalIdleFiles: totalCount,
		TotalIdleBytes: totalBytes,
		Buckets:        buckets,
		AgeBuckets:     buckets,
		TopFiles:       paginated,
		Offset:         start,
		Limit:          limit,
	}
}
