package hasher

import (
	"os"
	"path/filepath"
	"testing"
)

func TestComputeSingleFileHash(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.txt")

	content := []byte("ScanFile Pro - Fast Hashing Engine Unit Test")
	if err := os.WriteFile(testFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	// xxhash
	xxh, size, err := ComputeSingleFileHash(testFile, "xxhash")
	if err != nil {
		t.Fatalf("xxhash failed: %v", err)
	}
	if size != int64(len(content)) {
		t.Fatalf("expected size %d, got %d", len(content), size)
	}
	if len(xxh) == 0 {
		t.Fatal("expected non-empty xxhash")
	}

	// sha256
	sha, _, err := ComputeSingleFileHash(testFile, "sha256")
	if err != nil {
		t.Fatalf("sha256 failed: %v", err)
	}
	if len(sha) == 0 {
		t.Fatal("expected non-empty sha256")
	}
}
