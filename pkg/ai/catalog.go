package ai

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
)

const (
	// CatalogMaxSizeGB is the ceiling of the curated local catalogue (~14 GB).
	CatalogMaxSizeGB = 14.0

	// DefaultOllamaModel is the model the Assistente uses when none is chosen.
	DefaultOllamaModel = "qwen3-vl:8b"

	// memoryHeadroomGB is the RAM the operating system and the ScanFile process are
	// assumed to need on top of the model itself.
	memoryHeadroomGB = 2.0
)

// ModelInfo describes a model offered to the Assistente, in the shape consumed by
// GET /api/ai/models.
type ModelInfo struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Provider      string  `json:"provider"` // "ollama" | "openrouter"
	SizeGB        float64 `json:"sizeGB"`
	Vision        bool    `json:"vision"`
	Tools         bool    `json:"tools"`
	Installed     bool    `json:"installed"`
	Recommended   bool    `json:"recommended"`
	FitsMemory    bool    `json:"fitsMemory"`
	ContextWindow int     `json:"contextWindow,omitempty"`
	Notes         string  `json:"notes,omitempty"`
}

// CuratedOllamaModels is the curated local catalogue, capped at ~14 GB.
// Vision and Tools mirror the capabilities Ollama publishes for each model.
var CuratedOllamaModels = []ModelInfo{
	{
		ID:            "qwen3-vl:8b",
		Name:          "Qwen3-VL 8B",
		Provider:      string(ProviderOllama),
		SizeGB:        6.0,
		Vision:        true,
		Tools:         true,
		Recommended:   true,
		ContextWindow: 131072,
		Notes:         "Padrão do Assistente: lê imagens, faz OCR de documentos e chama ferramentas.",
	},
	{
		ID:            "qwen2.5vl:7b",
		Name:          "Qwen2.5-VL 7B",
		Provider:      string(ProviderOllama),
		SizeGB:        6.0,
		Vision:        true,
		Tools:         false,
		ContextWindow: 131072,
		Notes:         "Visão consolidada para fotos e documentos, sem chamada de ferramentas.",
	},
	{
		ID:            "gemma3:12b",
		Name:          "Gemma 3 12B",
		Provider:      string(ProviderOllama),
		SizeGB:        8.1,
		Vision:        true,
		Tools:         false,
		ContextWindow: 131072,
		Notes:         "Multimodal do Google, forte em texto longo; não chama ferramentas.",
	},
	{
		ID:            "qwen3:14b",
		Name:          "Qwen3 14B",
		Provider:      string(ProviderOllama),
		SizeGB:        9.3,
		Vision:        false,
		Tools:         true,
		ContextWindow: 40960,
		Notes:         "Raciocínio e ferramentas com alta precisão em português; sem visão.",
	},
	{
		ID:            "gpt-oss:20b",
		Name:          "GPT-OSS 20B",
		Provider:      string(ProviderOllama),
		SizeGB:        14.0,
		Vision:        false,
		Tools:         true,
		ContextWindow: 131072,
		Notes:         "Modelo aberto da OpenAI com ferramentas nativas; sem visão.",
	},
	{
		ID:            "devstral:24b",
		Name:          "Devstral 24B",
		Provider:      string(ProviderOllama),
		SizeGB:        14.0,
		Vision:        false,
		Tools:         true,
		ContextWindow: 131072,
		Notes:         "Especialista em código e uso de ferramentas; sem visão.",
	},
}

// CuratedOpenRouterModels lists the cloud models offered through OpenRouter.
var CuratedOpenRouterModels = []ModelInfo{
	{
		ID:            "anthropic/claude-sonnet-4.5",
		Name:          "Claude Sonnet 4.5",
		Provider:      string(ProviderOpenRouter),
		Vision:        true,
		Tools:         true,
		Recommended:   true,
		ContextWindow: 200000,
		Notes:         "Executa na nuvem: os caminhos e nomes enviados saem da máquina.",
	},
	{
		ID:            "google/gemini-2.5-flash",
		Name:          "Gemini 2.5 Flash",
		Provider:      string(ProviderOpenRouter),
		Vision:        true,
		Tools:         true,
		ContextWindow: 1000000,
		Notes:         "Executa na nuvem: os caminhos e nomes enviados saem da máquina.",
	},
	{
		ID:            "openai/gpt-4o-mini",
		Name:          "GPT-4o Mini",
		Provider:      string(ProviderOpenRouter),
		Vision:        true,
		Tools:         true,
		ContextWindow: 128000,
		Notes:         "Executa na nuvem: os caminhos e nomes enviados saem da máquina.",
	},
	{
		ID:            "qwen/qwen3-vl-8b-instruct",
		Name:          "Qwen3-VL 8B (nuvem)",
		Provider:      string(ProviderOpenRouter),
		Vision:        true,
		Tools:         true,
		ContextWindow: 131072,
		Notes:         "Mesmo modelo do catálogo local, hospedado na nuvem.",
	},
}

// CatalogResponse is the payload behind GET /api/ai/models.
type CatalogResponse struct {
	Models          []ModelInfo `json:"models"`
	InstalledModels []string    `json:"installedModels"`
	OllamaOnline    bool        `json:"ollamaOnline"`
	OllamaVersion   string      `json:"ollamaVersion"`
	TotalMemoryGB   float64     `json:"totalMemoryGB"`
	MaxSizeGB       float64     `json:"maxSizeGB"`
	DefaultModel    string      `json:"defaultModel"`
}

// BuildCatalog queries the local Ollama daemon and merges the result with the
// curated catalogue, using the machine's physical memory to decide FitsMemory.
func BuildCatalog(ctx context.Context, ollamaEndpoint string) CatalogResponse {
	return BuildCatalogWithMemory(ctx, ollamaEndpoint, detectTotalMemoryBytes())
}

// BuildCatalogWithMemory is BuildCatalog with an explicit memory budget, which
// makes the FitsMemory decision testable. A totalMemoryBytes of 0 means "unknown"
// and falls back to the catalogue ceiling.
func BuildCatalogWithMemory(ctx context.Context, ollamaEndpoint string, totalMemoryBytes int64) CatalogResponse {
	client := NewOllamaClient(ollamaEndpoint)

	installed, version, err := client.ListModels(ctx)
	online := err == nil

	installedNames := make([]string, 0, len(installed))
	installedByID := make(map[string]InstalledModel, len(installed)*2)
	for _, m := range installed {
		installedNames = append(installedNames, m.Name)
		key := strings.ToLower(m.Name)
		installedByID[key] = m
		installedByID[strings.TrimSuffix(key, ":latest")] = m
	}

	memoryGB := float64(totalMemoryBytes) / (1024 * 1024 * 1024)
	budgetGB := CatalogMaxSizeGB
	if memoryGB > 0 {
		budgetGB = memoryGB - memoryHeadroomGB
	}

	models := make([]ModelInfo, 0, len(CuratedOllamaModels)+len(CuratedOpenRouterModels)+len(installed))

	for _, m := range CuratedOllamaModels {
		m.Installed = isInstalled(installedByID, m.ID)
		m.FitsMemory = m.SizeGB <= budgetGB
		models = append(models, m)
	}

	// Models present in Ollama but outside the curated catalogue still deserve to
	// be selectable: they are already on disk.
	for _, inst := range installed {
		if isCurated(inst.Name) {
			continue
		}
		sizeGB := roundGB(float64(inst.SizeBytes) / (1024 * 1024 * 1024))
		models = append(models, ModelInfo{
			ID:         inst.Name,
			Name:       inst.Name,
			Provider:   string(ProviderOllama),
			SizeGB:     sizeGB,
			Installed:  true,
			FitsMemory: sizeGB <= budgetGB,
			Notes:      "Detectado na sua instalação local do Ollama; capacidades de visão e ferramentas não verificadas.",
		})
	}

	for _, m := range CuratedOpenRouterModels {
		// Cloud models do not consume local memory.
		m.FitsMemory = true
		models = append(models, m)
	}

	// Installed first, then by size, then by id for a stable order.
	sort.SliceStable(models, func(i, j int) bool {
		if models[i].Installed != models[j].Installed {
			return models[i].Installed
		}
		if models[i].Provider != models[j].Provider {
			return models[i].Provider == string(ProviderOllama)
		}
		if models[i].SizeGB != models[j].SizeGB {
			return models[i].SizeGB < models[j].SizeGB
		}
		return models[i].ID < models[j].ID
	})

	return CatalogResponse{
		Models:          models,
		InstalledModels: installedNames,
		OllamaOnline:    online,
		OllamaVersion:   version,
		TotalMemoryGB:   roundGB(memoryGB),
		MaxSizeGB:       CatalogMaxSizeGB,
		DefaultModel:    DefaultOllamaModel,
	}
}

func isInstalled(installedByID map[string]InstalledModel, id string) bool {
	key := strings.ToLower(id)
	if _, ok := installedByID[key]; ok {
		return true
	}
	_, ok := installedByID[key+":latest"]
	return ok
}

func isCurated(name string) bool {
	lower := strings.ToLower(strings.TrimSuffix(strings.ToLower(name), ":latest"))
	for _, m := range CuratedOllamaModels {
		if strings.ToLower(m.ID) == lower {
			return true
		}
	}
	return false
}

func roundGB(v float64) float64 {
	if v <= 0 {
		return 0
	}
	return math.Round(v*100) / 100
}

// FindModel returns the catalogue entry for id, if any.
func FindCuratedModel(id string) (ModelInfo, bool) {
	for _, m := range CuratedOllamaModels {
		if strings.EqualFold(m.ID, id) {
			return m, true
		}
	}
	for _, m := range CuratedOpenRouterModels {
		if strings.EqualFold(m.ID, id) {
			return m, true
		}
	}
	return ModelInfo{}, false
}

// DescribeModel renders a short human-readable line for logs and errors.
func DescribeModel(m ModelInfo) string {
	caps := make([]string, 0, 2)
	if m.Vision {
		caps = append(caps, "visão")
	}
	if m.Tools {
		caps = append(caps, "ferramentas")
	}
	if len(caps) == 0 {
		caps = append(caps, "somente texto")
	}
	return fmt.Sprintf("%s (%.1f GB, %s)", m.Name, m.SizeGB, strings.Join(caps, " + "))
}
