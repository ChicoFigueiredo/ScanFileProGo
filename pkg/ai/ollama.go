package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OllamaClient manages HTTP communications with the local Ollama daemon.
type OllamaClient struct {
	BaseURL    string
	httpClient *http.Client
}

// NewOllamaClient creates an Ollama client pointing to the given endpoint (defaults to http://localhost:11434).
func NewOllamaClient(endpoint string) *OllamaClient {
	if endpoint == "" {
		endpoint = "http://127.0.0.1:11434"
	}
	endpoint = strings.TrimRight(endpoint, "/")
	return &OllamaClient{
		BaseURL: endpoint,
		httpClient: &http.Client{
			Timeout: 120 * time.Second, // Long timeout for generation
		},
	}
}

// OllamaTagResponse represents /api/tags response.
type OllamaTagResponse struct {
	Models []struct {
		Name       string `json:"name"`
		Model      string `json:"model"`
		Size       int64  `json:"size"`
		ModifiedAt string `json:"modified_at"`
		Details    struct {
			Format            string   `json:"format"`
			Family            string   `json:"family"`
			Families          []string `json:"families"`
			ParameterSize     string   `json:"parameter_size"`
			QuantizationLevel string   `json:"quantization_level"`
		} `json:"details"`
	} `json:"models"`
}

// OllamaVersionResponse represents /api/version response.
type OllamaVersionResponse struct {
	Version string `json:"version"`
}

// InstalledModel is a model already downloaded in the local Ollama installation.
type InstalledModel struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"sizeBytes"`
}

// ListModels returns the installed models with their sizes plus the Ollama version.
func (c *OllamaClient) ListModels(ctx context.Context) ([]InstalledModel, string, error) {
	ctxShort, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctxShort, "GET", c.BaseURL+"/api/tags", nil)
	if err != nil {
		return nil, "", err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("ollama offline em %s: %w", c.BaseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("ollama retornou status %d", resp.StatusCode)
	}

	var tags OllamaTagResponse
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return nil, "", err
	}

	list := make([]InstalledModel, 0, len(tags.Models))
	for _, m := range tags.Models {
		name := m.Name
		if name == "" {
			name = m.Model
		}
		if name == "" {
			continue
		}
		list = append(list, InstalledModel{Name: name, SizeBytes: m.Size})
	}

	// Fetch version
	var verResp OllamaVersionResponse
	reqVer, _ := http.NewRequestWithContext(ctxShort, "GET", c.BaseURL+"/api/version", nil)
	if rVer, errVer := c.httpClient.Do(reqVer); errVer == nil {
		_ = json.NewDecoder(rVer.Body).Decode(&verResp)
		rVer.Body.Close()
	}

	return list, verResp.Version, nil
}

// ListInstalledModels returns the list of installed model names and the Ollama version.
func (c *OllamaClient) ListInstalledModels(ctx context.Context) ([]string, string, error) {
	models, version, err := c.ListModels(ctx)
	if err != nil {
		return nil, "", err
	}

	names := make([]string, 0, len(models))
	for _, m := range models {
		names = append(names, m.Name)
	}
	return names, version, nil
}

// PullProgress represents progress chunks during model download.
type PullProgress struct {
	Status    string  `json:"status"`
	Digest    string  `json:"digest,omitempty"`
	Total     int64   `json:"total,omitempty"`
	Completed int64   `json:"completed,omitempty"`
	Error     string  `json:"error,omitempty"`
	Percent   float64 `json:"percent,omitempty"`
}

// PullModel downloads an Ollama model with a streaming progress callback.
func (c *OllamaClient) PullModel(ctx context.Context, modelName string, onProgress func(p PullProgress)) error {
	payload, _ := json.Marshal(map[string]interface{}{
		"name":   modelName,
		"stream": true,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/api/pull", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	// Pull can take minutes, use a dedicated client without short timeout
	pullClient := &http.Client{Timeout: 30 * time.Minute}
	resp, err := pullClient.Do(req)
	if err != nil {
		return fmt.Errorf("falha ao conectar no Ollama para download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("erro do Ollama no download (%d): %s", resp.StatusCode, string(body))
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var p PullProgress
		if err := json.Unmarshal(line, &p); err == nil {
			if p.Error != "" {
				return fmt.Errorf("erro no download: %s", p.Error)
			}
			if p.Total > 0 {
				p.Percent = (float64(p.Completed) / float64(p.Total)) * 100.0
			}
			if onProgress != nil {
				onProgress(p)
			}
		}
	}
	return scanner.Err()
}

// OllamaChatRequest schema for /api/chat.
type OllamaChatRequest struct {
	Model    string                 `json:"model"`
	Messages []Message              `json:"messages"`
	Tools    []ToolDefinition       `json:"tools,omitempty"`
	Stream   bool                   `json:"stream"`
	Options  map[string]interface{} `json:"options,omitempty"`
}

// OllamaChatResponse schema from /api/chat.
type OllamaChatResponse struct {
	Model           string  `json:"model"`
	CreatedAt       string  `json:"created_at"`
	Message         Message `json:"message"`
	Done            bool    `json:"done"`
	TotalDuration   int64   `json:"total_duration,omitempty"`
	PromptEvalCount int     `json:"prompt_eval_count,omitempty"`
	EvalCount       int     `json:"eval_count,omitempty"`
}

// Chat calls Ollama chat endpoint with tool definitions and returns the assistant message.
func (c *OllamaClient) Chat(ctx context.Context, model string, messages []Message, tools []ToolDefinition) (*Message, error) {
	reqBody := OllamaChatRequest{
		Model:    model,
		Messages: messages,
		Tools:    tools,
		Stream:   false,
		Options: map[string]interface{}{
			"temperature": 0.1, // Low temperature for deterministic tool execution
		},
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/api/chat", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("erro de comunicação com Ollama (%s): %w", c.BaseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama erro HTTP %d: %s", resp.StatusCode, string(b))
	}

	var chatResp OllamaChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("erro decodificando resposta do Ollama: %w", err)
	}

	return &chatResp.Message, nil
}
