package scanner

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
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

func TestAutoSave_AtomicRotation(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "scanfile_autosave_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tm := NewTreeManager()
	tm.GetOrCreateRoot("C:\\")
	tm.AddFile(&FileNode{
		Path:      "C:\\data\\test1.txt",
		Name:      "test1.txt",
		Size:      1234,
		ModTime:   1700000000,
		Hash:      "xxh64:test1",
		Extension: ".txt",
	})

	config := ScanConfig{Roots: []string{"C:\\"}}

	// First AutoSave
	path1, err := SaveAutoSave(tm, []string{"C:\\"}, config, tempDir)
	if err != nil {
		t.Fatalf("First SaveAutoSave failed: %v", err)
	}
	if filepath.Base(path1) != DefaultAutoSaveFileName {
		t.Errorf("expected %s, got %s", DefaultAutoSaveFileName, filepath.Base(path1))
	}

	latest, err := GetLatestAutoSave(tempDir)
	if err != nil {
		t.Fatalf("GetLatestAutoSave failed: %v", err)
	}
	if latest.FileName != DefaultAutoSaveFileName {
		t.Errorf("expected latest autosave %s, got %s", DefaultAutoSaveFileName, latest.FileName)
	}

	// Add second file and AutoSave again (triggers backup rotation)
	tm.AddFile(&FileNode{
		Path:      "C:\\data\\test2.txt",
		Name:      "test2.txt",
		Size:      5678,
		ModTime:   1700000002,
		Hash:      "xxh64:test2",
		Extension: ".txt",
	})

	path2, err := SaveAutoSave(tm, []string{"C:\\"}, config, tempDir)
	if err != nil {
		t.Fatalf("Second SaveAutoSave failed: %v", err)
	}
	if path2 != path1 {
		t.Errorf("expected path %s, got %s", path1, path2)
	}

	// Verify that backup file now exists
	backupPath := filepath.Join(tempDir, BackupAutoSaveFileName)
	if _, err := os.Stat(backupPath); err != nil {
		t.Errorf("expected backup autosave %s to exist: %v", BackupAutoSaveFileName, err)
	}

	// Load latest autosave and verify 2 files
	_, snap, err := LoadCacheFromFile(path2)
	if err != nil {
		t.Fatalf("failed to load latest autosave: %v", err)
	}
	if snap.TotalFiles != 2 {
		t.Errorf("expected 2 files in latest autosave, got %d", snap.TotalFiles)
	}
}

func TestQuickScan_LookupAndReuse(t *testing.T) {
	snap := &CacheSnapshot{
		Version: 2,
		Files: []*FileNode{
			{
				Path:      "C:\\Project\\doc.pdf",
				Name:      "doc.pdf",
				Size:      50000,
				ModTime:   1700000100,
				Hash:      "xxh64:cached_hash_123",
				QuickHash: 1234567,
			},
		},
	}

	lookup := BuildQuickScanLookup(snap)
	if len(lookup) != 1 {
		t.Fatalf("expected 1 entry in lookup, got %d", len(lookup))
	}

	normKey := strings.ToLower(filepath.Clean("C:\\Project\\doc.pdf"))
	cached, ok := lookup[normKey]
	if !ok || cached == nil {
		t.Fatalf("expected to find normalized path in lookup")
	}

	if cached.Hash != "xxh64:cached_hash_123" {
		t.Errorf("expected hash xxh64:cached_hash_123, got %s", cached.Hash)
	}
}
