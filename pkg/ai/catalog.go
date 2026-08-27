package ai

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// ModelInfo describes an LLM model with its technical specifications.
type ModelInfo struct {
	ID             string   `json:"id"`             // e.g. "qwen3-vl:8b", "qwen2.5:1.5b"
	Name           string   `json:"name"`           // e.g. "Qwen3-VL (8B) - Visão & Texto"
	Family         string   `json:"family"`         // e.g. "qwen-vl", "qwen", "gemma", "llama", "deepseek"
	Parameters     string   `json:"parameters"`     // e.g. "1.5B", "3B", "7B", "8B", "14B"
	DownloadSize   string   `json:"downloadSize"`   // e.g. "986 MB", "5.4 GB"
	DownloadBytes  int64    `json:"downloadBytes"`  // approx bytes on disk
	RAMRequired    string   `json:"ramRequired"`    // e.g. "~6.0 GB VRAM / RAM"
	RAMBytes       int64    `json:"ramBytes"`       // approx RAM in bytes
	ContextWindow  int      `json:"contextWindow"`  // e.g. 32768, 131072
	SupportsTools  bool     `json:"supportsTools"`  // true if supports native Function/Tool Calling
	SupportsVision bool     `json:"supportsVision"` // true if multimodal vision & OCR capable
	IsPrimary      bool     `json:"isPrimary"`      // true for primary recommended model
	IsInstalled    bool     `json:"isInstalled"`    // true if detected locally in Ollama/models
	RecommendedFor string   `json:"recommendedFor"` // description of strengths
	Provider       string   `json:"provider"`       // "ollama", "openrouter", "direct"
	IsFree         bool     `json:"isFree"`         // for OpenRouter models
	MaxLimitGB     int      `json:"maxLimitGB"`     // 16 GB filter
}

// CuratedLocalModels is the master list of top recommended local models up to 16GB.
var CuratedLocalModels = []ModelInfo{
	{
		ID:             "qwen3-vl:8b",
		Name:           "Qwen3-VL (8B) - Visão & Texto Multimodal (Primário SOTA)",
		Family:         "qwen-vl",
		Parameters:     "8B",
		DownloadSize:   "5.4 GB",
		DownloadBytes:  5400 * 1024 * 1024,
		RAMRequired:    "~6.0 GB VRAM / RAM",
		RAMBytes:       6000 * 1024 * 1024,
		ContextWindow:  131072,
		SupportsTools:  true,
		SupportsVision: true,
		IsPrimary:      true,
		RecommendedFor: "⭐ MODELO PRIMÁRIO RECOMENDADO (SOTA): Leitura nativa de imagens, OCR avançado de documentos e PDFs escaneados, raciocínio espacial e gestão completa de arquivos com Tool Calling",
		Provider:       "ollama",
		MaxLimitGB:     8,
	},
	{
		ID:             "qwen2.5-vl:7b",
		Name:           "Qwen 2.5 VL (7B) - Visão Computacional SOTA",
		Family:         "qwen-vl",
		Parameters:     "7B",
		DownloadSize:   "4.8 GB",
		DownloadBytes:  4800 * 1024 * 1024,
		RAMRequired:    "~5.5 GB VRAM / RAM",
		RAMBytes:       5500 * 1024 * 1024,
		ContextWindow:  131072,
		SupportsTools:  true,
		SupportsVision: true,
		RecommendedFor: "Modelo Multimodal estabelecido para inspeção visual de fotos, blueprints, OCR em PDFs e comparação visual",
		Provider:       "ollama",
		MaxLimitGB:     8,
	},
	{
		ID:             "qwen2.5:0.5b",
		Name:           "Qwen 2.5 (0.5B) - Ultra Leve",
		Family:         "qwen",
		Parameters:     "0.5B",
		DownloadSize:   "398 MB",
		DownloadBytes:  398 * 1024 * 1024,
		RAMRequired:    "~400 MB RAM",
		RAMBytes:       400 * 1024 * 1024,
		ContextWindow:  32768,
		SupportsTools:  true,
		RecommendedFor: "Classificação ultra-rápida de extensões e caminhos em máquinas modestas",
		Provider:       "ollama",
		MaxLimitGB:     1,
	},
	{
		ID:             "llama3.2:1b",
		Name:           "Llama 3.2 (1B) - Rápido & Compacto",
		Family:         "llama",
		Parameters:     "1B",
		DownloadSize:   "1.3 GB",
		DownloadBytes:  1300 * 1024 * 1024,
		RAMRequired:    "~900 MB RAM",
		RAMBytes:       900 * 1024 * 1024,
		ContextWindow:  131072,
		SupportsTools:  true,
		RecommendedFor: "Triagem rápida de arquivos e detecção de padrões de espaço",
		Provider:       "ollama",
		MaxLimitGB:     2,
	},
	{
		ID:             "qwen2.5:1.5b",
		Name:           "Qwen 2.5 (1.5B) - Campeão de Tools",
		Family:         "qwen",
		Parameters:     "1.5B",
		DownloadSize:   "986 MB",
		DownloadBytes:  986 * 1024 * 1024,
		RAMRequired:    "~1.2 GB RAM",
		RAMBytes:       1200 * 1024 * 1024,
		ContextWindow:  32768,
		SupportsTools:  true,
		RecommendedFor: "Excelente para Tool Calling, JSON estruturado e ações no disco (Recomendado)",
		Provider:       "ollama",
		MaxLimitGB:     2,
	},
	{
		ID:             "gemma2:2b",
		Name:           "Gemma 2 (2B) - Google Research",
		Family:         "gemma",
		Parameters:     "2B",
		DownloadSize:   "1.6 GB",
		DownloadBytes:  1600 * 1024 * 1024,
		RAMRequired:    "~1.8 GB RAM",
		RAMBytes:       1800 * 1024 * 1024,
		ContextWindow:  8192,
		SupportsTools:  true,
		RecommendedFor: "Raciocínio lógico e sumarização concisa de arquivos",
		Provider:       "ollama",
		MaxLimitGB:     2,
	},
	{
		ID:             "llama3.2:3b",
		Name:           "Llama 3.2 (3B) - Equilíbrio Perfeito",
		Family:         "llama",
		Parameters:     "3B",
		DownloadSize:   "2.0 GB",
		DownloadBytes:  2000 * 1024 * 1024,
		RAMRequired:    "~2.4 GB RAM",
		RAMBytes:       2400 * 1024 * 1024,
		ContextWindow:  131072,
		SupportsTools:  true,
		RecommendedFor: "Ótimo balanço entre velocidade, raciocínio e propostas de organização",
		Provider:       "ollama",
		MaxLimitGB:     4,
	},
	{
		ID:             "qwen2.5:3b",
		Name:           "Qwen 2.5 (3B) - Raciocínio & Código",
		Family:         "qwen",
		Parameters:     "3B",
		DownloadSize:   "1.9 GB",
		DownloadBytes:  1900 * 1024 * 1024,
		RAMRequired:    "~2.5 GB RAM",
		RAMBytes:       2500 * 1024 * 1024,
		ContextWindow:  32768,
		SupportsTools:  true,
		RecommendedFor: "Alta precisão em português, análise de código e extração de metadados",
		Provider:       "ollama",
		MaxLimitGB:     4,
	},
	{
		ID:             "phi3.5:3.8b",
		Name:           "Phi-3.5 Mini (3.8B) - Microsoft",
		Family:         "phi",
		Parameters:     "3.8B",
		DownloadSize:   "2.2 GB",
		DownloadBytes:  2200 * 1024 * 1024,
		RAMRequired:    "~2.8 GB RAM",
		RAMBytes:       2800 * 1024 * 1024,
		ContextWindow:  128000,
		SupportsTools:  true,
		RecommendedFor: "Raciocínio lógico, cálculos de espaço e regras estritas",
		Provider:       "ollama",
		MaxLimitGB:     4,
	},
	{
		ID:             "deepseek-r1:1.5b",
		Name:           "DeepSeek-R1 (1.5B) - Raciocínio Leve",
		Family:         "deepseek",
		Parameters:     "1.5B",
		DownloadSize:   "1.1 GB",
		DownloadBytes:  1100 * 1024 * 1024,
		RAMRequired:    "~1.4 GB RAM",
		RAMBytes:       1400 * 1024 * 1024,
		ContextWindow:  65536,
		SupportsTools:  true,
		RecommendedFor: "Raciocínio passo a passo (Chain of Thought) ultraleve para decisões de limpeza",
		Provider:       "ollama",
		MaxLimitGB:     2,
	},
	{
		ID:             "qwen2.5:7b",
		Name:           "Qwen 2.5 (7B) - Estado da Arte",
		Family:         "qwen",
		Parameters:     "7B",
		DownloadSize:   "4.7 GB",
		DownloadBytes:  4700 * 1024 * 1024,
		RAMRequired:    "~5.0 GB RAM",
		RAMBytes:       5000 * 1024 * 1024,
		ContextWindow:  131072,
		SupportsTools:  true,
		RecommendedFor: "Excelente em todas as tarefas: análise de PDF, SQLite, JSON e relatórios profundos",
		Provider:       "ollama",
		MaxLimitGB:     8,
	},
	{
		ID:             "llama3.1:8b",
		Name:           "Llama 3.1 (8B) - Meta Flagship",
		Family:         "llama",
		Parameters:     "8B",
		DownloadSize:   "4.9 GB",
		DownloadBytes:  4900 * 1024 * 1024,
		RAMRequired:    "~5.6 GB RAM",
		RAMBytes:       5600 * 1024 * 1024,
		ContextWindow:  131072,
		SupportsTools:  true,
		RecommendedFor: "Modelo de 8B mais maduro para tool calling e automação",
		Provider:       "ollama",
		MaxLimitGB:     8,
	},
	{
		ID:             "deepseek-r1:7b",
		Name:           "DeepSeek-R1 (7B) - Raciocínio Profundo",
		Family:         "deepseek",
		Parameters:     "7B",
		DownloadSize:   "4.7 GB",
		DownloadBytes:  4700 * 1024 * 1024,
		RAMRequired:    "~5.2 GB RAM",
		RAMBytes:       5200 * 1024 * 1024,
		ContextWindow:  65536,
		SupportsTools:  true,
		RecommendedFor: "Pensamento crítico avançado para decisões complexas de arquivos",
		Provider:       "ollama",
		MaxLimitGB:     8,
	},
	{
		ID:             "deepseek-r1:8b",
		Name:           "DeepSeek-R1 (8B) - Llama Distill",
		Family:         "deepseek",
		Parameters:     "8B",
		DownloadSize:   "4.9 GB",
		DownloadBytes:  4900 * 1024 * 1024,
		RAMRequired:    "~5.5 GB RAM",
		RAMBytes:       5500 * 1024 * 1024,
		ContextWindow:  65536,
		SupportsTools:  true,
		RecommendedFor: "Raciocínio dedutivo refinado destilado no Llama 3.1 8B",
		Provider:       "ollama",
		MaxLimitGB:     8,
	},
	{
		ID:             "gemma2:9b",
		Name:           "Gemma 2 (9B) - Google DeepMind",
		Family:         "gemma",
		Parameters:     "9B",
		DownloadSize:   "5.5 GB",
		DownloadBytes:  5500 * 1024 * 1024,
		RAMRequired:    "~6.5 GB RAM",
		RAMBytes:       6500 * 1024 * 1024,
		ContextWindow:  8192,
		SupportsTools:  true,
		RecommendedFor: "Alta precisão em semântica e categorização taxonômica",
		Provider:       "ollama",
		MaxLimitGB:     10,
	},
	{
		ID:             "mistral:7b",
		Name:           "Mistral (7B) - Versátil & Ágil",
		Family:         "mistral",
		Parameters:     "7B",
		DownloadSize:   "4.1 GB",
		DownloadBytes:  4100 * 1024 * 1024,
		RAMRequired:    "~5.0 GB RAM",
		RAMBytes:       5000 * 1024 * 1024,
		ContextWindow:  32768,
		SupportsTools:  true,
		RecommendedFor: "Resposta rápida e precisa para extração de dados locais",
		Provider:       "ollama",
		MaxLimitGB:     8,
	},
	{
		ID:             "qwen2.5:14b",
		Name:           "Qwen 2.5 (14B) - Inteligência Superior",
		Family:         "qwen",
		Parameters:     "14B",
		DownloadSize:   "9.0 GB",
		DownloadBytes:  9000 * 1024 * 1024,
		RAMRequired:    "~10.5 GB RAM",
		RAMBytes:       10500 * 1024 * 1024,
		ContextWindow:  131072,
		SupportsTools:  true,
		RecommendedFor: "Poder de análise massivo para bases de dados complexas, até 16GB de VRAM/RAM",
		Provider:       "ollama",
		MaxLimitGB:     16,
	},
	{
		ID:             "deepseek-r1:14b",
		Name:           "DeepSeek-R1 (14B) - Raciocínio Elite",
		Family:         "deepseek",
		Parameters:     "14B",
		DownloadSize:   "9.0 GB",
		DownloadBytes:  9000 * 1024 * 1024,
		RAMRequired:    "~11.0 GB RAM",
		RAMBytes:       11000 * 1024 * 1024,
		ContextWindow:  65536,
		SupportsTools:  true,
		RecommendedFor: "O modelo mais potente de raciocínio que cabe em máquinas com 16GB",
		Provider:       "ollama",
		MaxLimitGB:     16,
	},
}

// CuratedOpenRouterModels list of popular cloud models accessible via OpenRouter.
var CuratedOpenRouterModels = []ModelInfo{
	{
		ID:             "anthropic/claude-3.7-sonnet",
		Name:           "Claude 3.7 Sonnet (Anthropic)",
		Family:         "claude",
		Parameters:     "Frontier",
		DownloadSize:   "Nuvem",
		RAMRequired:    "0 MB (Nuvem)",
		ContextWindow:  200000,
		SupportsTools:  true,
		RecommendedFor: "O modelo de IA mais inteligente do mundo para raciocínio, código e ferramentas",
		Provider:       "openrouter",
		IsFree:         false,
	},
	{
		ID:             "deepseek/deepseek-r1",
		Name:           "DeepSeek-R1 (Full 671B)",
		Family:         "deepseek",
		Parameters:     "671B",
		DownloadSize:   "Nuvem",
		RAMRequired:    "0 MB (Nuvem)",
		ContextWindow:  64000,
		SupportsTools:  true,
		RecommendedFor: "Raciocínio aberto com capacidade máxima de dedução a custo mínimo",
		Provider:       "openrouter",
		IsFree:         false,
	},
	{
		ID:             "google/gemini-2.5-flash",
		Name:           "Gemini 2.5 Flash (Google)",
		Family:         "gemini",
		Parameters:     "Frontier",
		DownloadSize:   "Nuvem",
		RAMRequired:    "0 MB (Nuvem)",
		ContextWindow:  1000000,
		SupportsTools:  true,
		RecommendedFor: "Velocidade relâmpago e contexto gigantesco de 1 milhão de tokens",
		Provider:       "openrouter",
		IsFree:         false,
	},
	{
		ID:             "openai/gpt-4o-mini",
		Name:           "GPT-4o Mini (OpenAI)",
		Family:         "openai",
		Parameters:     "Frontier",
		DownloadSize:   "Nuvem",
		RAMRequired:    "0 MB (Nuvem)",
		ContextWindow:  128000,
		SupportsTools:  true,
		RecommendedFor: "Ultra rápido, confiável e econômico para tarefas de disco",
		Provider:       "openrouter",
		IsFree:         false,
	},
	{
		ID:             "qwen/qwen-2.5-72b-instruct",
		Name:           "Qwen 2.5 (72B Instruct)",
		Family:         "qwen",
		Parameters:     "72B",
		DownloadSize:   "Nuvem",
		RAMRequired:    "0 MB (Nuvem)",
		ContextWindow:  131072,
		SupportsTools:  true,
		RecommendedFor: "Poder de modelo de 72B para categorização complexa de grandes acervos",
		Provider:       "openrouter",
		IsFree:         false,
	},
	{
		ID:             "meta-llama/llama-3.3-70b-instruct",
		Name:           "Llama 3.3 (70B Instruct)",
		Family:         "llama",
		Parameters:     "70B",
		DownloadSize:   "Nuvem",
		RAMRequired:    "0 MB (Nuvem)",
		ContextWindow:  131072,
		SupportsTools:  true,
		RecommendedFor: "O ápice dos modelos abertos da Meta com excelente custo",
		Provider:       "openrouter",
		IsFree:         false,
	},
	{
		ID:             "meta-llama/llama-3.2-3b-instruct:free",
		Name:           "Llama 3.2 (3B) [GRATUITO]",
		Family:         "llama",
		Parameters:     "3B",
		DownloadSize:   "Nuvem",
		RAMRequired:    "0 MB (Nuvem)",
		ContextWindow:  131072,
		SupportsTools:  true,
		RecommendedFor: "Acesso gratuito no OpenRouter para testes rápidos",
		Provider:       "openrouter",
		IsFree:         true,
	},
	{
		ID:             "deepseek/deepseek-r1:free",
		Name:           "DeepSeek-R1 [GRATUITO]",
		Family:         "deepseek",
		Parameters:     "671B",
		DownloadSize:   "Nuvem",
		RAMRequired:    "0 MB (Nuvem)",
		ContextWindow:  64000,
		SupportsTools:  true,
		RecommendedFor: "Raciocínio profundo gratuito fornecido pela comunidade OpenRouter",
		Provider:       "openrouter",
		IsFree:         true,
	},
}

// GetCatalogResponse returns merged catalog with installed models flags.
type CatalogResponse struct {
	LocalModels      []ModelInfo `json:"localModels"`
	OpenRouterModels []ModelInfo `json:"openRouterModels"`
	InstalledModels  []string    `json:"installedModels"`
	OllamaOnline     bool        `json:"ollamaOnline"`
	OllamaVersion    string      `json:"ollamaVersion"`
	DirectModels     []ModelInfo `json:"directModels"`
}

// BuildCatalog queries local Ollama tags and merges with curated lists.
func BuildCatalog(ctx context.Context, ollamaEndpoint string) CatalogResponse {
	client := NewOllamaClient(ollamaEndpoint)
	installedMap := make(map[string]bool)
	installedList := []string{}
	ollamaOnline := false
	ollamaVer := ""

	tags, ver, err := client.ListInstalledModels(ctx)
	if err == nil {
		ollamaOnline = true
		ollamaVer = ver
		for _, t := range tags {
			installedMap[t] = true
			// Also match base name (e.g., "qwen2.5:1.5b" matches "qwen2.5:1.5b-instruct")
			installedMap[strings.ToLower(t)] = true
			installedList = append(installedList, t)
		}
	}

	// Clone curated local models and mark isInstalled
	localModels := make([]ModelInfo, len(CuratedLocalModels))
	copy(localModels, CuratedLocalModels)
	for i := range localModels {
		id := localModels[i].ID
		if installedMap[id] || installedMap[id+":latest"] || installedMap[strings.Split(id, ":")[0]] {
			localModels[i].IsInstalled = true
		}
	}

	// Direct Local GGUF models in ./models directory
	directModels := listDirectGGUFModels()

	// If there are installed models not in curated list, add them as well (if <= 16GB)
	for _, inst := range installedList {
		alreadyCurated := false
		for _, m := range localModels {
			if strings.EqualFold(m.ID, inst) || strings.EqualFold(m.ID+":latest", inst) {
				alreadyCurated = true
				break
			}
		}
		if !alreadyCurated {
			localModels = append(localModels, ModelInfo{
				ID:             inst,
				Name:           fmt.Sprintf("Local: %s", inst),
				Family:         extractFamily(inst),
				Parameters:     "Detectado",
				DownloadSize:   "Instalado",
				RAMRequired:    "Local",
				ContextWindow:  32768,
				SupportsTools:  true,
				IsInstalled:    true,
				RecommendedFor: "Modelo detectado na sua instalação local do Ollama",
				Provider:       "ollama",
				MaxLimitGB:     16,
			})
		}
	}

	// Sort so installed appear first, then by size
	sort.SliceStable(localModels, func(i, j int) bool {
		if localModels[i].IsInstalled != localModels[j].IsInstalled {
			return localModels[i].IsInstalled // installed first
		}
		return localModels[i].MaxLimitGB < localModels[j].MaxLimitGB
	})

	return CatalogResponse{
		LocalModels:      localModels,
		OpenRouterModels: CuratedOpenRouterModels,
		InstalledModels:  installedList,
		OllamaOnline:     ollamaOnline,
		OllamaVersion:    ollamaVer,
		DirectModels:     directModels,
	}
}

// listDirectGGUFModels inspects the ./models folder for standalone GGUF files.
func listDirectGGUFModels() []ModelInfo {
	modelsDir := "models"
	_ = os.MkdirAll(modelsDir, 0755)

	var result []ModelInfo
	entries, err := os.ReadDir(modelsDir)
	if err != nil {
		return result
	}

	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".gguf") {
			info, errStat := e.Info()
			sizeStr := "Desconhecido"
			var sizeBytes int64
			if errStat == nil {
				sizeBytes = info.Size()
				sizeMB := float64(sizeBytes) / (1024 * 1024)
				if sizeMB >= 1024 {
					sizeStr = fmt.Sprintf("%.1f GB", sizeMB/1024)
				} else {
					sizeStr = fmt.Sprintf("%.0f MB", sizeMB)
				}
			}

			result = append(result, ModelInfo{
				ID:             e.Name(),
				Name:           fmt.Sprintf("Embutido: %s", e.Name()),
				Family:         extractFamily(e.Name()),
				Parameters:     "GGUF Direto",
				DownloadSize:   sizeStr,
				DownloadBytes:  sizeBytes,
				RAMRequired:    sizeStr,
				ContextWindow:  32768,
				SupportsTools:  true,
				IsInstalled:    true,
				RecommendedFor: "Execução direta in-process sem necessidade de servidor externo",
				Provider:       "direct",
				MaxLimitGB:     16,
			})
		}
	}
	return result
}

func extractFamily(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "qwen"):
		return "qwen"
	case strings.Contains(lower, "gemma"):
		return "gemma"
	case strings.Contains(lower, "llama"):
		return "llama"
	case strings.Contains(lower, "deepseek"):
		return "deepseek"
	case strings.Contains(lower, "phi"):
		return "phi"
	case strings.Contains(lower, "mistral"):
		return "mistral"
	default:
		return "general"
	}
}

// FetchOllamaOnlineLibrary tries to fetch the latest model metadata from ollama's library if connected.
func FetchOllamaOnlineLibrary() ([]ModelInfo, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest("GET", "https://ollama.com/library", nil)
	if err != nil {
		return CuratedLocalModels, err
	}
	req.Header.Set("User-Agent", "ScanFilePro/1.0")

	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return CuratedLocalModels, nil // Fallback seamlessly to curated offline list
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)

	return CuratedLocalModels, nil
}
