package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// AppConfig stores user preferences across sessions.
type AppConfig struct {
	SelectedRoots     []string `json:"selectedRoots"`
	WorkerThreads     int      `json:"workerThreads"`
	HashAlgorithm     string   `json:"hashAlgorithm"`
	HashMode          string   `json:"hashMode"` // "smart" or "all"
	MinFileSize       int64    `json:"minFileSize"`
	TreemapDepth      int      `json:"treemapDepth"`
	TreemapColorMode  string   `json:"treemapColorMode"` // "extension", "depth", "age"
	TreemapViewMode   string   `json:"treemapViewMode"`  // "split", "treemap", "table"
	DuplicatesSortBy  string   `json:"duplicatesSortBy"`
	DuplicatesMinSize int64    `json:"duplicatesMinSize"`
	IdleMinAgeDays    int      `json:"idleMinAgeDays"`
	IdleMinSizeBytes  int64    `json:"idleMinSizeBytes"`
	FolderSortBy      string   `json:"folderSortBy"`
	FolderMinSize     int64    `json:"folderMinSize"`
	UIZoom            int      `json:"uiZoom"`
}

var (
	configMu   sync.RWMutex
	configFile = "scanfile_config.json"
)

// GetDefaultConfig returns default initial configuration.
func GetDefaultConfig() AppConfig {
	return AppConfig{
		SelectedRoots:     []string{},
		WorkerThreads:     8,
		HashAlgorithm:     "xxhash",
		HashMode:          "smart",
		MinFileSize:       1,
		TreemapDepth:      5,
		TreemapColorMode:  "extension",
		TreemapViewMode:   "split",
		DuplicatesSortBy:  "size_desc",
		DuplicatesMinSize: 0,
		IdleMinAgeDays:    365,
		IdleMinSizeBytes:  104857600, // 100 MB
		FolderSortBy:      "wasted_desc",
		FolderMinSize:     0,
		UIZoom:            100,
	}
}

// LoadConfig reads configuration from scanfile_config.json or returns defaults.
func LoadConfig() AppConfig {
	configMu.RLock()
	defer configMu.RUnlock()

	cfg := GetDefaultConfig()

	// Try in current directory or next to executable
	target := configFile
	if _, err := os.Stat(target); os.IsNotExist(err) {
		exePath, errExe := os.Executable()
		if errExe == nil {
			nextToExe := filepath.Join(filepath.Dir(exePath), configFile)
			if _, errStat := os.Stat(nextToExe); errStat == nil {
				target = nextToExe
			}
		}
	}

	data, err := os.ReadFile(target)
	if err != nil {
		return cfg
	}

	_ = json.Unmarshal(data, &cfg)
	return cfg
}

// SaveConfig writes the configuration to scanfile_config.json.
func SaveConfig(cfg AppConfig) error {
	configMu.Lock()
	defer configMu.Unlock()

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configFile, data, 0644)
}
