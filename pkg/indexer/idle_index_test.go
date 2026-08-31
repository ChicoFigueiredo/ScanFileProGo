package indexer

import (
	"testing"
	"time"

	"scanfile/pkg/scanner"
)

func TestIdleFiles_StreamingAndPagination(t *testing.T) {
	tm := scanner.NewTreeManager()
	tm.GetOrCreateRoot("C:\\")

	now := time.Now().Unix()
	twoYearsAgo := now - (2 * 365 * 86400)
	sixMonthsAgo := now - (180 * 86400)
	tenDaysAgo := now - (10 * 86400)

	// File 1: 2 years idle, 500MB
	tm.AddFile(&scanner.FileNode{
		Path:      "C:\\Backup\\old_archive.zip",
		Name:      "old_archive.zip",
		Size:      500 * 1024 * 1024,
		ModTime:   twoYearsAgo,
		Extension: ".zip",
	})

	// File 2: 6 months idle, 100MB
	tm.AddFile(&scanner.FileNode{
		Path:      "C:\\Videos\\clip.mp4",
		Name:      "clip.mp4",
		Size:      100 * 1024 * 1024,
		ModTime:   sixMonthsAgo,
		Extension: ".mp4",
	})

	// File 3: 10 days idle, 20MB
	tm.AddFile(&scanner.FileNode{
		Path:      "C:\\Docs\\notes.txt",
		Name:      "notes.txt",
		Size:      20 * 1024 * 1024,
		ModTime:   tenDaysAgo,
		Extension: ".txt",
	})

	// Query files idle > 30 days and > 50MB
	summary := QueryIdleFilesStreaming(tm, 30, 50*1024*1024, "", "", "size_desc", 0, 10)

	if summary.TotalIdleFiles != 2 {
		t.Fatalf("expected 2 idle files, got %d", summary.TotalIdleFiles)
	}

	expectedBytes := int64(600 * 1024 * 1024)
	if summary.TotalIdleBytes != expectedBytes {
		t.Errorf("expected %d bytes, got %d", expectedBytes, summary.TotalIdleBytes)
	}

	if len(summary.TopFiles) != 2 {
		t.Fatalf("expected 2 top files, got %d", len(summary.TopFiles))
	}

	// Verify size descending order
	if summary.TopFiles[0].Name != "old_archive.zip" {
		t.Errorf("expected first file to be old_archive.zip, got %s", summary.TopFiles[0].Name)
	}

	// Test pagination offset=1, limit=1
	paged := QueryIdleFilesStreaming(tm, 30, 50*1024*1024, "", "", "size_desc", 1, 1)
	if len(paged.TopFiles) != 1 || paged.TopFiles[0].Name != "clip.mp4" {
		t.Errorf("expected paged file to be clip.mp4, got %v", paged.TopFiles)
	}

	// Test Extension Aggregation
	extStats := tm.AggregateExtensionStats()
	if len(extStats) != 3 {
		t.Errorf("expected 3 extensions, got %d", len(extStats))
	}
	if extStats[0].Extension != ".zip" {
		t.Errorf("expected top extension to be .zip (500MB), got %s", extStats[0].Extension)
	}
}
