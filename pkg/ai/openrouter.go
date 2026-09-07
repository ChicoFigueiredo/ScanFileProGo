package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenRouterClient manages chat completions via the OpenRouter API.
type OpenRouterClient struct {
	APIKey     string
	BaseURL    string
	httpClient *http.Client
}

// NewOpenRouterClient creates a new client for OpenRouter.
func NewOpenRouterClient(apiKey string) *OpenRouterClient {
	return &OpenRouterClient{
		APIKey:  apiKey,
		BaseURL: "https://openrouter.ai/api/v1",
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

type openRouterRequest struct {
	Model       string                 `json:"model"`
	Messages    []Message              `json:"messages"`
	Tools       []ToolDefinition       `json:"tools,omitempty"`
	Temperature float32                `json:"temperature"`
	ExtraBody   map[string]interface{} `json:"extra_body,omitempty"`
}

type openRouterResponse struct {
	ID      string `json:"id"`
	Choices []struct {
		Index        int     `json:"index"`
		Message      Message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error,omitempty"`
}

// Chat executes a completion request via OpenRouter with tools support.
func (c *OpenRouterClient) Chat(ctx context.Context, model string, messages []Message, tools []ToolDefinition) (*Message, error) {
	if c.APIKey == "" {
		return nil, fmt.Errorf("chave de API do OpenRouter não configurada. Insira sua chave nas configurações de IA")
	}

	reqBody := openRouterRequest{
		Model:       model,
		Messages:    messages,
		Tools:       tools,
		Temperature: 0.2,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.APIKey))
	req.Header.Set("HTTP-Referer", "https://github.com/ChicoFigueiredo/ScanFileProGo")
	req.Header.Set("X-Title", "ScanFile Pro AI Engine")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("falha de conexão com OpenRouter: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("erro lendo corpo da resposta: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openrouter erro HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var orResp openRouterResponse
	if err := json.Unmarshal(bodyBytes, &orResp); err != nil {
		return nil, fmt.Errorf("erro decodificando JSON do OpenRouter: %w", err)
	}

	if orResp.Error != nil && orResp.Error.Message != "" {
		return nil, fmt.Errorf("erro da API OpenRouter: %s", orResp.Error.Message)
	}

	if len(orResp.Choices) == 0 {
		return nil, fmt.Errorf("nenhuma resposta gerada pelo modelo no OpenRouter")
	}

	return &orResp.Choices[0].Message, nil
}
