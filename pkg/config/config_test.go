package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigLoadSave(t *testing.T) {
	tempDir := t.TempDir()
	testConfigFile := filepath.Join(tempDir, "test_config.json")
	configFile = testConfigFile

	// 1. Default config
	cfg := LoadConfig()
	if cfg.WorkerThreads != 8 {
		t.Errorf("Expected WorkerThreads 8, got %d", cfg.WorkerThreads)
	}
	if cfg.HashAlgorithm != "xxhash" {
		t.Errorf("Expected HashAlgorithm xxhash, got %s", cfg.HashAlgorithm)
	}

	// 2. Modify and Save
	cfg.WorkerThreads = 16
	cfg.HashAlgorithm = "blake3"
	cfg.TreemapDepth = 12
	cfg.TreemapColorMode = "age"
	cfg.SelectedRoots = []string{"C:\\", "D:\\"}
	cfg.UIZoom = 125

	err := SaveConfig(cfg)
	if err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	if _, err := os.Stat(testConfigFile); os.IsNotExist(err) {
		t.Fatalf("Expected config file to be created at %s", testConfigFile)
	}

	// 3. Reload and verify
	loaded := LoadConfig()
	if loaded.WorkerThreads != 16 {
		t.Errorf("Expected WorkerThreads 16, got %d", loaded.WorkerThreads)
	}
	if loaded.HashAlgorithm != "blake3" {
		t.Errorf("Expected HashAlgorithm blake3, got %s", loaded.HashAlgorithm)
	}
	if loaded.TreemapDepth != 12 {
		t.Errorf("Expected TreemapDepth 12, got %d", loaded.TreemapDepth)
	}
	if loaded.TreemapColorMode != "age" {
		t.Errorf("Expected TreemapColorMode age, got %s", loaded.TreemapColorMode)
	}
	if loaded.UIZoom != 125 {
		t.Errorf("Expected UIZoom 125, got %d", loaded.UIZoom)
	}
	if len(loaded.SelectedRoots) != 2 || loaded.SelectedRoots[0] != "C:\\" {
		t.Errorf("Expected SelectedRoots [C:\\ D:\\], got %v", loaded.SelectedRoots)
	}
}
