package scanner

import (
	"testing"
)

func TestTreeManager_AddAndBubbleSize(t *testing.T) {
	tm := NewTreeManager()
	root := tm.GetOrCreateRoot("C:\\")

	if root == nil {
		t.Fatal("expected root node to be created")
	}

	file1 := &FileNode{
		Path:      "C:\\Folder1\\file1.txt",
		Name:      "file1.txt",
		Size:      1000,
		ModTime:   1700000000,
		Extension: ".txt",
	}

	file2 := &FileNode{
		Path:      "C:\\Folder1\\Subfolder\\file2.mp4",
		Name:      "file2.mp4",
		Size:      5000,
		ModTime:   1700000100,
		Extension: ".mp4",
	}

	tm.AddFile(file1)
	tm.AddFile(file2)

	// Check root aggregated size
	if root.TotalSize != 6000 {
		t.Fatalf("expected root total size 6000, got %d", root.TotalSize)
	}

	if root.FileCount != 2 {
		t.Fatalf("expected root file count 2, got %d", root.FileCount)
	}

	// Check Folder1 size
	f1 := tm.FindDir("C:\\Folder1")
	if f1 == nil || f1.TotalSize != 6000 {
		t.Fatalf("expected Folder1 total size 6000, got %v", f1)
	}

	// Test RemoveFile
	removedSize, found := tm.RemoveFile(file1.Path)
	if !found || removedSize != 1000 {
		t.Fatalf("expected removedSize 1000 and found=true, got %d, %v", removedSize, found)
	}

	if root.TotalSize != 5000 {
		t.Fatalf("expected root total size 5000 after removal, got %d", root.TotalSize)
	}
}

func TestTreeManager_GetDirSummary(t *testing.T) {
	tm := NewTreeManager()
	tm.AddFile(&FileNode{Path: "C:\\FolderA\\f1.bin", Name: "f1.bin", Size: 200})
	tm.AddFile(&FileNode{Path: "C:\\FolderB\\f2.bin", Name: "f2.bin", Size: 800})

	summary := tm.GetDirSummary("C:\\", 1)
	if summary == nil {
		t.Fatal("expected summary not to be nil")
	}

	if len(summary.SubDirs) != 2 {
		t.Fatalf("expected 2 subdirs, got %d", len(summary.SubDirs))
	}

	// Should be sorted by TotalSize descending (FolderB 800 > FolderA 200)
	if summary.SubDirs[0].Name != "FolderB" {
		t.Fatalf("expected largest folder FolderB first, got %s", summary.SubDirs[0].Name)
	}
}
