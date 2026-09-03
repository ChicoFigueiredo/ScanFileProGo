package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// DefaultServerPort is the fixed port the local server listens on, so the
// interface and a second instance always know where to find it.
const DefaultServerPort = 47321

// Assistente providers accepted in AIProvider.
const (
	ProviderOllama     = "ollama"
	ProviderOpenRouter = "openrouter"
	ProviderQuick      = "quick"
	// providerDirectLegacy is the old name of ProviderQuick, still found in
	// configuration files written by previous versions.
	providerDirectLegacy = "direct"
)

const configFileName = "scanfile_config.json"

// AppConfig stores user preferences across sessions.
//
// The OpenRouter key is never stored nor returned in clear text: use
// SetOpenRouterKey and OpenRouterKey, and Public before sending it anywhere.
type AppConfig struct {
	Theme                   string   `json:"theme"`
	SelectedRoots           []string `json:"selectedRoots"`
	WorkerThreads           int      `json:"workerThreads"` // 0 = Auto
	HashAlgorithm           string   `json:"hashAlgorithm"`
	HashMode                string   `json:"hashMode"` // "smart" or "all"
	MinFileSize             int64    `json:"minFileSize"`
	AutoSaveIntervalMinutes int      `json:"autoSaveIntervalMinutes"`
	TreemapDepth            int      `json:"treemapDepth"`
	TreemapColorMode        string   `json:"treemapColorMode"` // "extension", "depth", "age"
	TreemapViewMode         string   `json:"treemapViewMode"`  // "split", "treemap", "table"
	TreeTableLimit          int      `json:"treeTableLimit"`
	DuplicatesSortBy        string   `json:"duplicatesSortBy"`
	DuplicatesMinSize       int64    `json:"duplicatesMinSize"`
	DupLimit                int      `json:"dupLimit"`
	IdleMinAgeDays          int      `json:"idleMinAgeDays"`
	IdleMinSizeBytes        int64    `json:"idleMinSizeBytes"`
	IdleSortBy              string   `json:"idleSortBy"`
	IdleLimit               int      `json:"idleLimit"`
	FolderSortBy            string   `json:"folderSortBy"`
	FolderMinSize           int64    `json:"folderMinSize"`
	FolderDupLimit          int      `json:"folderDupLimit"`
	ChkFolderTopLevelOnly   bool     `json:"chkFolderTopLevelOnly"`
	ComparePathA            string   `json:"comparePathA"`
	ComparePathB            string   `json:"comparePathB"`
	UIZoom                  int      `json:"uiZoom"`

	// ServerPort is the fixed local port (see DefaultServerPort).
	ServerPort int `json:"serverPort"`

	// AI Assistant Settings
	AIProvider       string `json:"aiProvider"`       // "ollama", "openrouter", "quick"
	AIOllamaEndpoint string `json:"aiOllamaEndpoint"` // default: "http://127.0.0.1:11434"
	AIOllamaModel    string `json:"aiOllamaModel"`    // default: "qwen3-vl:8b"
	// AIOpenRouterKey only ever carries a key on its way in, from the interface
	// or from a file written by an older version. It is omitted when empty so
	// that an interface echoing back what it received cannot erase the stored
	// key; sending it explicitly empty still removes the key (see MergeJSON).
	AIOpenRouterKey   string `json:"aiOpenRouterKey,omitempty"`
	AIOpenRouterModel string `json:"aiOpenRouterModel"` // default: "anthropic/claude-3.7-sonnet"
	AIDryRunDefault   bool   `json:"aiDryRunDefault"`   // default: true

	// AIOpenRouterKeyEnc holds the key protected by DPAPI (base64). It is
	// written to disk but never sent to the interface.
	AIOpenRouterKeyEnc string `json:"aiOpenRouterKeyEnc,omitempty"`
	// AIOpenRouterKeyPlain marks a blob that is only base64-encoded because the
	// platform has no DPAPI. It is never true on Windows.
	AIOpenRouterKeyPlain bool `json:"aiOpenRouterKeyPlain,omitempty"`
	// HasOpenRouterKey is filled by Public for the interface and is never
	// persisted.
	HasOpenRouterKey bool `json:"hasOpenRouterKey"`
}

var (
	configMu sync.RWMutex

	pathMu       sync.Mutex
	resolvedPath string
)

// GetDefaultConfig returns default initial configuration.
func GetDefaultConfig() AppConfig {
	return AppConfig{
		Theme:                   "theme-ochre-dark",
		SelectedRoots:           []string{},
		WorkerThreads:           0, // Auto
		HashAlgorithm:           "xxhash",
		HashMode:                "smart",
		MinFileSize:             1,
		AutoSaveIntervalMinutes: 5,
		TreemapDepth:            5,
		TreemapColorMode:        "extension",
		TreemapViewMode:         "split",
		TreeTableLimit:          50,
		DuplicatesSortBy:        "size_desc",
		DuplicatesMinSize:       0,
		DupLimit:                50,
		IdleMinAgeDays:          365,
		IdleMinSizeBytes:        104857600, // 100 MB
		IdleSortBy:              "size_desc",
		IdleLimit:               50,
		FolderSortBy:            "wasted_desc",
		FolderMinSize:           0,
		FolderDupLimit:          50,
		ChkFolderTopLevelOnly:   false,
		ComparePathA:            "",
		ComparePathB:            "",
		UIZoom:                  100,
		ServerPort:              DefaultServerPort,
		AIProvider:              ProviderOllama,
		AIOllamaEndpoint:        "http://127.0.0.1:11434",
		AIOllamaModel:           "qwen3-vl:8b",
		AIOpenRouterKey:         "",
		AIOpenRouterModel:       "anthropic/claude-3.7-sonnet",
		AIDryRunDefault:         true,
	}
}

// ConfigPath returns the file the configuration is read from and written to.
// It is resolved once per process: the working directory when a configuration
// file already exists there, otherwise next to the executable.
func ConfigPath() string {
	pathMu.Lock()
	defer pathMu.Unlock()
	if resolvedPath == "" {
		resolvedPath = resolveConfigPath()
	}
	return resolvedPath
}

// SetConfigPath overrides the resolved configuration path. Call it before any
// load or save; it exists for tests and for a future command line flag.
func SetConfigPath(path string) {
	pathMu.Lock()
	defer pathMu.Unlock()
	resolvedPath = path
}

func resolveConfigPath() string {
	if abs, err := filepath.Abs(configFileName); err == nil {
		if _, err := os.Stat(abs); err == nil {
			return abs
		}
	}
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), configFileName)
	}
	if abs, err := filepath.Abs(configFileName); err == nil {
		return abs
	}
	return configFileName
}

// LoadConfig reads the configuration from ConfigPath, falling back to the
// defaults for anything missing or unreadable. A key still stored in clear text
// by an older version is moved to the protected field in memory and will be
// written back protected by the next SaveConfig.
func LoadConfig() AppConfig {
	path := ConfigPath()

	configMu.RLock()
	data, err := os.ReadFile(path)
	configMu.RUnlock()

	cfg := GetDefaultConfig()
	if err != nil {
		return cfg
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		// A broken file must never silently become a different configuration.
		return GetDefaultConfig()
	}

	cfg.migrateLegacySecret()
	cfg.applyDefaults()
	cfg.HasOpenRouterKey = false
	return cfg
}

// SaveConfig writes the configuration to ConfigPath atomically: a temporary
// file in the same directory, flushed to disk and then renamed over the target,
// so an interrupted write never leaves a truncated configuration behind.
func SaveConfig(cfg AppConfig) error {
	path := ConfigPath()

	cfg.migrateLegacySecret()
	cfg.applyDefaults()
	cfg.AIOpenRouterKey = ""     // never stored in clear text
	cfg.HasOpenRouterKey = false // computed by Public, never stored

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	configMu.Lock()
	defer configMu.Unlock()

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("não foi possível criar %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".scanfile_config-*.tmp")
	if err != nil {
		return fmt.Errorf("não foi possível criar o arquivo temporário: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}

	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("não foi possível substituir %s: %w", path, err)
	}

	return nil
}

// MergeJSON applies a partial configuration document over the current one:
// only the keys present in body are changed, everything else is preserved. This
// is what keeps a preference saved by one screen from erasing the Assistente
// settings of another.
//
// The key "aiOpenRouterKey" is special: when present and not empty it is
// protected and stored; when present and empty the stored key is removed; when
// absent the stored key is preserved. The protected fields themselves are
// ignored in the body, so echoing back a Public config never destroys the key.
func MergeJSON(cur AppConfig, body []byte) (AppConfig, error) {
	var present map[string]json.RawMessage
	if err := json.Unmarshal(body, &present); err != nil {
		return cur, fmt.Errorf("configuração inválida: %w", err)
	}

	out := cur
	if err := json.Unmarshal(body, &out); err != nil {
		return cur, fmt.Errorf("configuração inválida: %w", err)
	}

	// The stored secret only ever changes through "aiOpenRouterKey".
	out.AIOpenRouterKey = cur.AIOpenRouterKey
	out.AIOpenRouterKeyEnc = cur.AIOpenRouterKeyEnc
	out.AIOpenRouterKeyPlain = cur.AIOpenRouterKeyPlain
	out.HasOpenRouterKey = false

	if raw, ok := present["aiOpenRouterKey"]; ok {
		var plain string
		if err := json.Unmarshal(raw, &plain); err != nil && string(raw) != "null" {
			return cur, fmt.Errorf("aiOpenRouterKey deve ser um texto: %w", err)
		}
		SetOpenRouterKey(&out, plain)
	}

	out.applyDefaults()
	return out, nil
}

// Public returns a copy safe to send to the interface: the OpenRouter key and
// its protected blob are removed and HasOpenRouterKey reports whether one is
// configured.
func (c AppConfig) Public() AppConfig {
	pub := c
	pub.HasOpenRouterKey = c.AIOpenRouterKeyEnc != "" || c.AIOpenRouterKey != ""
	pub.AIOpenRouterKey = ""
	pub.AIOpenRouterKeyEnc = ""
	pub.AIOpenRouterKeyPlain = false
	return pub
}

// SetOpenRouterKey stores the key protected by the platform's secret store. An
// empty or blank key removes the stored secret.
func SetOpenRouterKey(cfg *AppConfig, plain string) {
	if cfg == nil {
		return
	}
	cfg.AIOpenRouterKey = ""

	plain = strings.TrimSpace(plain)
	if plain == "" {
		cfg.AIOpenRouterKeyEnc = ""
		cfg.AIOpenRouterKeyPlain = false
		return
	}

	blob, isPlain := protectSecret(plain)
	cfg.AIOpenRouterKeyEnc = blob
	cfg.AIOpenRouterKeyPlain = isPlain
}

// OpenRouterKey returns the configured key in clear text, or "" when none is
// configured. Never send the result to the interface.
func OpenRouterKey(cfg AppConfig) string {
	if cfg.AIOpenRouterKeyEnc != "" {
		if plain, err := unprotectSecret(cfg.AIOpenRouterKeyEnc, cfg.AIOpenRouterKeyPlain); err == nil {
			return plain
		}
		return ""
	}
	// A file written by an older version still carries the key in clear text.
	return cfg.AIOpenRouterKey
}

// migrateLegacySecret moves a clear text key into the protected field.
func (c *AppConfig) migrateLegacySecret() {
	if c.AIOpenRouterKey == "" {
		return
	}
	if c.AIOpenRouterKeyEnc != "" {
		c.AIOpenRouterKey = ""
		return
	}
	SetOpenRouterKey(c, c.AIOpenRouterKey)
}

// applyDefaults repairs values that would break the application if they were
// used as read, without touching legitimate user choices.
func (c *AppConfig) applyDefaults() {
	def := GetDefaultConfig()

	if c.Theme == "" {
		c.Theme = def.Theme
	}
	if c.SelectedRoots == nil {
		c.SelectedRoots = []string{}
	}
	if c.WorkerThreads < 0 {
		c.WorkerThreads = 0 // Auto
	}
	if c.HashAlgorithm == "" {
		c.HashAlgorithm = def.HashAlgorithm
	}
	if c.HashMode == "" {
		c.HashMode = def.HashMode
	}
	if c.AutoSaveIntervalMinutes <= 0 {
		c.AutoSaveIntervalMinutes = def.AutoSaveIntervalMinutes
	}
	if c.UIZoom <= 0 {
		c.UIZoom = def.UIZoom
	}
	if c.ServerPort <= 0 || c.ServerPort > 65535 {
		c.ServerPort = DefaultServerPort
	}

	switch strings.ToLower(strings.TrimSpace(c.AIProvider)) {
	case "":
		c.AIProvider = def.AIProvider
	case providerDirectLegacy, ProviderQuick:
		c.AIProvider = ProviderQuick
	case ProviderOllama:
		c.AIProvider = ProviderOllama
	case ProviderOpenRouter:
		c.AIProvider = ProviderOpenRouter
	default:
		c.AIProvider = def.AIProvider
	}

	if c.AIOllamaEndpoint == "" {
		c.AIOllamaEndpoint = def.AIOllamaEndpoint
	}
	if c.AIOllamaModel == "" {
		c.AIOllamaModel = def.AIOllamaModel
	}
	if c.AIOpenRouterModel == "" {
		c.AIOpenRouterModel = def.AIOpenRouterModel
	}
}
