package scanner

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestCache_ExportImportRoundtrip(t *testing.T) {
	tm := NewTreeManager()
	tm.GetOrCreateRoot("C:\\")

	f1 := &FileNode{
		Path:      "C:\\folder1\\file1.txt",
		Name:      "file1.txt",
		Size:      1024,
		ModTime:   1700000000,
		Hash:      "xxh64:1234567890abcdef",
		Extension: ".txt",
	}
	f2 := &FileNode{
		Path:      "C:\\folder1\\subfolder\\file2.png",
		Name:      "file2.png",
		Size:      2048,
		ModTime:   1700000001,
		Hash:      "xxh64:abcdef1234567890",
		Extension: ".png",
	}

	tm.AddFile(f1)
	tm.AddFile(f2)

	var buf bytes.Buffer
	config := ScanConfig{Roots: []string{"C:\\"}, HashAlgorithm: "xxhash"}

	err := ExportCache(tm, []string{"C:\\"}, config, &buf)
	if err != nil {
		t.Fatalf("ExportCache failed: %v", err)
	}

	loadedTree, snapshot, err := ImportCache(&buf)
	if err != nil {
		t.Fatalf("ImportCache failed: %v", err)
	}

	if snapshot.TotalFiles != 2 {
		t.Errorf("expected 2 files in snapshot, got %d", snapshot.TotalFiles)
	}

	if snapshot.TotalBytes != 3072 {
		t.Errorf("expected 3072 bytes in snapshot, got %d", snapshot.TotalBytes)
	}

	loadedFiles := loadedTree.GetAllFiles()
	if len(loadedFiles) != 2 {
		t.Fatalf("expected 2 files in loaded tree, got %d", len(loadedFiles))
	}

	folder1 := loadedTree.FindDir("C:\\folder1")
	if folder1 == nil {
		t.Fatalf("expected C:\\folder1 node to exist")
	}

	if folder1.TotalSize != 3072 {
		t.Errorf("expected folder1 TotalSize to be 3072, got %d", folder1.TotalSize)
	}
}

func TestCache_FileSaveLoad(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "scanfile_cache_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tm := NewTreeManager()
	tm.GetOrCreateRoot("D:\\")
	tm.AddFile(&FileNode{
		Path:      "D:\\data\\test.bin",
		Name:      "test.bin",
		Size:      5000,
		ModTime:   1700000000,
		Hash:      "xxh64:test",
		Extension: ".bin",
	})

	cachePath := filepath.Join(tempDir, "test_cache.scanfile.gz")
	config := ScanConfig{Roots: []string{"D:\\"}}

	err = SaveCacheToFile(tm, []string{"D:\\"}, config, cachePath)
	if err != nil {
		t.Fatalf("SaveCacheToFile failed: %v", err)
	}

	loadedTree, snapshot, err := LoadCacheFromFile(cachePath)
	if err != nil {
		t.Fatalf("LoadCacheFromFile failed: %v", err)
	}

	if snapshot.TotalFiles != 1 || snapshot.TotalBytes != 5000 {
		t.Errorf("unexpected snapshot data: files=%d, bytes=%d", snapshot.TotalFiles, snapshot.TotalBytes)
	}

	if len(loadedTree.GetAllFiles()) != 1 {
		t.Errorf("expected 1 file in loaded tree, got %d", len(loadedTree.GetAllFiles()))
	}
}
