package indexer

import (
	"testing"

	"scanfile/pkg/scanner"
)

func TestDuplicateIndex_RebuildAndSorting(t *testing.T) {
	idx := NewDuplicateIndex()

	files := []*scanner.FileNode{
		// Group 1: 5GB ISO files (duplicated 3 times)
		scanner.NewFileNodeAt("C:\\Downloads\\ubuntu.iso", scanner.FileMeta{Name: "ubuntu.iso", Size: 5000000000, ModTime: 100, Hash: "xxh:1111"}),
		scanner.NewFileNodeAt("D:\\ISOs\\ubuntu_backup.iso", scanner.FileMeta{Name: "ubuntu_backup.iso", Size: 5000000000, ModTime: 200, Hash: "xxh:1111"}),
		scanner.NewFileNodeAt("E:\\ISOs\\ubuntu_copy.iso", scanner.FileMeta{Name: "ubuntu_copy.iso", Size: 5000000000, ModTime: 300, Hash: "xxh:1111"}),

		// Group 2: 10MB Video files (duplicated 2 times)
		scanner.NewFileNodeAt("C:\\Videos\\clip.mp4", scanner.FileMeta{Name: "clip.mp4", Size: 10000000, ModTime: 150, Hash: "xxh:2222"}),
		scanner.NewFileNodeAt("D:\\Backup\\clip_copy.mp4", scanner.FileMeta{Name: "clip_copy.mp4", Size: 10000000, ModTime: 250, Hash: "xxh:2222"}),

		// Unique file (should not be in duplicate index)
		scanner.NewFileNodeAt("C:\\Docs\\notes.txt", scanner.FileMeta{Name: "notes.txt", Size: 500, ModTime: 50, Hash: "xxh:3333"}),

		// Collision simulation: hypothetical identical hash "xxh:4444" but DIFFERENT sizes
		scanner.NewFileNodeAt("C:\\A\\fileA.bin", scanner.FileMeta{Name: "fileA.bin", Size: 1234, ModTime: 10, Hash: "xxh:4444"}),
		scanner.NewFileNodeAt("C:\\B\\fileB.bin", scanner.FileMeta{Name: "fileB.bin", Size: 9999, ModTime: 20, Hash: "xxh:4444"}),
	}

	idx.RebuildIndex(files)

	grpCount, fileCount, wasted := idx.GetSummaryStats()
	if grpCount != 2 {
		t.Fatalf("expected 2 duplicate groups, got %d", grpCount)
	}

	if fileCount != 5 {
		t.Fatalf("expected 5 duplicate files, got %d", fileCount)
	}

	// 5GB * 2 (wasted) + 10MB * 1 (wasted)
	expectedWasted := int64(5000000000*2 + 10000000*1)
	if wasted != expectedWasted {
		t.Fatalf("expected wasted bytes %d, got %d", expectedWasted, wasted)
	}

	// Query sorted by size_desc (Default: largest file size first)
	res := idx.Query(QueryFilter{SortBy: "size_desc"})
	if len(res.Groups) != 2 {
		t.Fatalf("expected 2 groups returned, got %d", len(res.Groups))
	}

	if res.Groups[0].FileSize != 5000000000 {
		t.Fatalf("expected largest group (5GB) to be first, got size %d", res.Groups[0].FileSize)
	}

	// Verify collision safety: fileA and fileB had same hash but different sizes, so neither formed a duplicate group of count >= 2
	for _, grp := range res.Groups {
		if grp.Hash == "xxh:4444" {
			t.Fatal("collision safety failed: different sized files with same hash should not form a duplicate group")
		}
	}
}
