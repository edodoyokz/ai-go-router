package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/edodoyokz/ai-go-router/internal/config"
)

// AzureAdapter implements the Adapter interface for Azure OpenAI
type AzureAdapter struct {
	name            string
	baseURL         string
	headers         map[string]string
	errorConfig     config.ErrorConfig
	httpClients     []*http.Client
	proxyIdx        atomic.Uint64
	accountSelector *AccountSelector
}

func NewAzureAdapter(cfg config.ProviderConfig, errorConfig config.ErrorConfig, proxyURL string) *AzureAdapter {
	accounts := make(map[string]string)
	for _, account := range cfg.Accounts {
		accounts[account.Name] = account.APIKey
	}

	var clients []*http.Client
	if len(cfg.ProxyURLs) > 0 {
		for _, purl := range cfg.ProxyURLs {
			clients = append(clients, createHTTPClient(purl))
		}
	} else {
		clients = []*http.Client{createHTTPClient(proxyURL)}
	}

	return &AzureAdapter{
		name:            cfg.Name,
		baseURL:         cfg.BaseURL,
		headers:         cfg.Headers,
		errorConfig:     errorConfig,
		httpClients:     clients,
		accountSelector: NewAccountSelector(accounts, cfg.APIKey),
	}
}

func (a *AzureAdapter) nextClient() *http.Client {
	if len(a.httpClients) == 1 {
		return a.httpClients[0]
	}
	idx := a.proxyIdx.Add(1) - 1
	return a.httpClients[idx%uint64(len(a.httpClients))]
}

func (a *AzureAdapter) Name() string { return a.name }

func (a *AzureAdapter) AccountNames() []string {
	return a.accountSelector.AccountNames()
}

func (a *AzureAdapter) buildAzureURL(deployment, apiVersion string) string {
	endpoint := strings.TrimRight(a.baseURL, "/")
	return fmt.Sprintf("%s/openai/deployments/%s/chat/completions?api-version=%s", endpoint, deployment, apiVersion)
}

func (a *AzureAdapter) ChatCompletion(ctx context.Context, request ChatRequest, model string) (ChatResponse, error) {
	request.Model = model

	accountName := ""
	if account := ctx.Value(AccountContextKey); account != nil {
		if accountStr, ok := account.(string); ok {
			accountName = accountStr
		}
	}
	_, apiKey := a.accountSelector.GetAccount(accountName)

	deployment := model
	apiVersion := "2024-10-01-preview"

	body, err := json.Marshal(request)
	if err != nil {
		return ChatResponse{}, NewNonRetryableError(a.name, model, "failed to marshal request", err)
	}

	endpoint := a.buildAzureURL(deployment, apiVersion)
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return ChatResponse{}, NewNonRetryableError(a.name, model, "failed to create request", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("api-key", apiKey)
	}
	for key, value := range a.headers {
		req.Header.Set(key, value)
	}

	resp, err := a.nextClient().Do(req)
	if err != nil {
		return ChatResponse{}, NewRetryableError(a.name, model, "network error", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return ChatResponse{}, NewRetryableError(a.name, model, "failed to read response", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errorResp struct {
			Error struct {
				Message string `json:"message"`
				Type    string `json:"type"`
				Code    string `json:"code"`
			} `json:"error"`
		}
		_ = json.Unmarshal(respBody, &errorResp)
		message := errorResp.Error.Message
		if message == "" {
			message = string(respBody)
		}
		return ChatResponse{}, ClassifyHTTPError(resp.StatusCode, a.name, model, message, a.errorConfig)
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return ChatResponse{}, &ProviderError{
			Provider: a.name,
			Model:    model,
			Type:     ErrInvalidUpstreamResponse,
			Message:  "failed to parse response",
			Cause:    err,
		}
	}

	return chatResp, nil
}

func (a *AzureAdapter) StreamChatCompletion(ctx context.Context, request ChatRequest, model string) (<-chan ChatChunk, error) {
	request.Model = model

	accountName := ""
	if account := ctx.Value(AccountContextKey); account != nil {
		if accountStr, ok := account.(string); ok {
			accountName = accountStr
		}
	}
	_, apiKey := a.accountSelector.GetAccount(accountName)

	deployment := model
	apiVersion := "2024-10-01-preview"

	body, err := json.Marshal(request)
	if err != nil {
		return nil, NewNonRetryableError(a.name, model, "failed to marshal request", err)
	}

	endpoint := a.buildAzureURL(deployment, apiVersion)
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, NewNonRetryableError(a.name, model, "failed to create request", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if apiKey != "" {
		req.Header.Set("api-key", apiKey)
	}
	for key, value := range a.headers {
		req.Header.Set(key, value)
	}

	resp, err := a.nextClient().Do(req)
	if err != nil {
		return nil, NewRetryableError(a.name, model, "network error", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var errorResp struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(respBody, &errorResp)
		message := errorResp.Error.Message
		if message == "" {
			message = string(respBody)
		}
		return nil, ClassifyHTTPError(resp.StatusCode, a.name, model, message, a.errorConfig)
	}

	chunkCh := make(chan ChatChunk, 64)
	go func() {
		defer close(chunkCh)
		defer resp.Body.Close()

		decoder := NewSSEDecoder(resp.Body)
		for {
			event, err := decoder.Next()
			if err != nil {
				if err != io.EOF {
					// Log error but don't fail the channel
				}
				return
			}
			data := strings.TrimSpace(event.Data)
			if data == "" || data == "[DONE]" {
				if data == "[DONE]" {
					return
				}
				continue
			}

			var chunk ChatChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}
			chunkCh <- chunk
		}
	}()

	return chunkCh, nil
}

func (a *AzureAdapter) GetUsage(ctx context.Context) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

func (a *AzureAdapter) Embeddings(ctx context.Context, request EmbeddingsRequest, model string) (EmbeddingsResponse, error) {
	accountName := ""
	if account := ctx.Value(AccountContextKey); account != nil {
		if accountStr, ok := account.(string); ok {
			accountName = accountStr
		}
	}
	_, apiKey := a.accountSelector.GetAccount(accountName)

	apiVersion := "2024-10-01-preview"
	endpoint := strings.TrimRight(a.baseURL, "/")
	url := fmt.Sprintf("%s/openai/deployments/%s/embeddings?api-version=%s", endpoint, model, apiVersion)

	body, err := json.Marshal(request)
	if err != nil {
		return EmbeddingsResponse{}, NewNonRetryableError(a.name, model, "failed to marshal request", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return EmbeddingsResponse{}, NewNonRetryableError(a.name, model, "failed to create request", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("api-key", apiKey)
	}

	resp, err := a.nextClient().Do(req)
	if err != nil {
		return EmbeddingsResponse{}, NewRetryableError(a.name, model, "network error", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return EmbeddingsResponse{}, NewRetryableError(a.name, model, "failed to read response", err)
	}

	if resp.StatusCode != http.StatusOK {
		return EmbeddingsResponse{}, ClassifyHTTPError(resp.StatusCode, a.name, model, string(respBody), a.errorConfig)
	}

	var embResp EmbeddingsResponse
	if err := json.Unmarshal(respBody, &embResp); err != nil {
		return EmbeddingsResponse{}, &ProviderError{
			Provider: a.name,
			Model:    model,
			Type:     ErrInvalidUpstreamResponse,
			Message:  "failed to parse embeddings response",
			Cause:    err,
		}
	}

	return embResp, nil
}

func (a *AzureAdapter) AudioSpeech(ctx context.Context, request AudioSpeechRequest, model string) (AudioSpeechResponse, error) {
	return AudioSpeechResponse{}, &ProviderError{Provider: a.name, Type: ErrNonRetryable, Message: "audio speech not supported by azure adapter"}
}

func (a *AzureAdapter) ImagesGenerations(ctx context.Context, request ImagesGenerationsRequest, model string) (ImagesGenerationsResponse, error) {
	accountName := ""
	if account := ctx.Value(AccountContextKey); account != nil {
		if accountStr, ok := account.(string); ok {
			accountName = accountStr
		}
	}
	_, apiKey := a.accountSelector.GetAccount(accountName)

	apiVersion := "2024-10-01-preview"
	endpoint := strings.TrimRight(a.baseURL, "/")
	url := fmt.Sprintf("%s/openai/deployments/%s/images/generations?api-version=%s", endpoint, model, apiVersion)

	body, err := json.Marshal(request)
	if err != nil {
		return ImagesGenerationsResponse{}, NewNonRetryableError(a.name, model, "failed to marshal request", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return ImagesGenerationsResponse{}, NewNonRetryableError(a.name, model, "failed to create request", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("api-key", apiKey)
	}

	resp, err := a.nextClient().Do(req)
	if err != nil {
		return ImagesGenerationsResponse{}, NewRetryableError(a.name, model, "network error", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return ImagesGenerationsResponse{}, NewRetryableError(a.name, model, "failed to read response", err)
	}

	if resp.StatusCode != http.StatusOK {
		return ImagesGenerationsResponse{}, ClassifyHTTPError(resp.StatusCode, a.name, model, string(respBody), a.errorConfig)
	}

	var imgResp ImagesGenerationsResponse
	if err := json.Unmarshal(respBody, &imgResp); err != nil {
		return ImagesGenerationsResponse{}, &ProviderError{
			Provider: a.name,
			Model:    model,
			Type:     ErrInvalidUpstreamResponse,
			Message:  "failed to parse images response",
			Cause:    err,
		}
	}

	return imgResp, nil
}

func init() {
	RegisterExecutor("azure", func(cfg config.ProviderConfig, errorCfg config.ErrorConfig) (Executor, error) {
		return &executorBridgeAdapter{adapter: NewAzureAdapter(cfg, errorCfg, "")}, nil
	})
}

type executorBridgeAdapter struct {
	adapter *AzureAdapter
}

func (e *executorBridgeAdapter) ProviderID() string { return "azure" }
func (e *executorBridgeAdapter) Supports(kind string) bool {
	return kind == "llm" || kind == "embedding" || kind == "image"
}
func (e *executorBridgeAdapter) ChatCompletion(ctx context.Context, req ChatRequest, model string) (ChatResponse, error) {
	return e.adapter.ChatCompletion(ctx, req, model)
}
func (e *executorBridgeAdapter) StreamChatCompletion(ctx context.Context, req ChatRequest, model string) (<-chan ChatChunk, error) {
	return e.adapter.StreamChatCompletion(ctx, req, model)
}
