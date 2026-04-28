package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/edodoyokz/ai-go-router/internal/config"
	"github.com/edodoyokz/ai-go-router/internal/providers/endpoints"
)

// OpenRouterAdapter implements the Adapter interface for OpenRouter's OpenAI-compatible API
type OpenRouterAdapter struct {
	name            string
	baseURL         string
	headers         map[string]string
	errorConfig     config.ErrorConfig
	httpClient      *http.Client
	accountSelector *AccountSelector
}

// NewOpenRouterAdapter creates a new OpenRouter adapter
func NewOpenRouterAdapter(cfg config.ProviderConfig, errorConfig config.ErrorConfig, proxyURL string) *OpenRouterAdapter {
	// Build accounts map
	accounts := make(map[string]string)
	for _, account := range cfg.Accounts {
		accounts[account.Name] = account.APIKey
	}

	adapter := &OpenRouterAdapter{
		name:            cfg.Name,
		baseURL:         cfg.BaseURL,
		headers:         cfg.Headers,
		errorConfig:     errorConfig,
		httpClient:      createHTTPClient(proxyURL),
		accountSelector: NewAccountSelector(accounts, cfg.APIKey),
	}

	return adapter
}

func (a *OpenRouterAdapter) Name() string {
	return a.name
}

func (a *OpenRouterAdapter) AccountNames() []string {
	return a.accountSelector.AccountNames()
}

func (a *OpenRouterAdapter) ChatCompletion(ctx context.Context, request ChatRequest, model string) (ChatResponse, error) {
	// Override model with the target model from routing
	request.Model = model

	// Get account from context if specified, otherwise use round-robin
	accountName := ""
	if account := ctx.Value(AccountContextKey); account != nil {
		if accountStr, ok := account.(string); ok {
			accountName = accountStr
		}
	}
	_, apiKey := a.accountSelector.GetAccount(accountName)

	// Marshal request to JSON
	body, err := json.Marshal(request)
	if err != nil {
		return ChatResponse{}, NewNonRetryableError(a.name, model, "failed to marshal request", err)
	}

	// Create HTTP request
	endpoint := endpoints.BuildOpenAI(a.baseURL, "/chat/completions")
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return ChatResponse{}, NewNonRetryableError(a.name, model, "failed to create request", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("HTTP-Referer", "https://github.com/edodoyokz/ai-go-router") // OpenRouter requirement
	req.Header.Set("X-Title", "NusaNexus Router")                               // OpenRouter requirement

	// Apply provider-specific headers
	for key, value := range a.headers {
		req.Header.Set(key, value)
	}

	// Execute request
	resp, err := a.httpClient.Do(req)
	if err != nil {
		// Network errors are retryable
		return ChatResponse{}, NewRetryableError(a.name, model, "network error", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return ChatResponse{}, NewRetryableError(a.name, model, "failed to read response", err)
	}

	// Check for HTTP errors
	if resp.StatusCode != http.StatusOK {
		// Try to parse error response
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

	// Parse successful response
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

func (a *OpenRouterAdapter) StreamChatCompletion(ctx context.Context, request ChatRequest, model string) (<-chan ChatChunk, error) {
	// Override model with the target model from routing
	request.Model = model
	request.Stream = true

	accountName := ""
	if account := ctx.Value(AccountContextKey); account != nil {
		if accountStr, ok := account.(string); ok {
			accountName = accountStr
		}
	}
	_, apiKey := a.accountSelector.GetAccount(accountName)

	body, err := json.Marshal(request)
	if err != nil {
		return nil, NewNonRetryableError(a.name, model, "failed to marshal request", err)
	}

	endpoint := endpoints.BuildOpenAI(a.baseURL, "/chat/completions")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, NewNonRetryableError(a.name, model, "failed to create request", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("HTTP-Referer", "https://github.com/edodoyokz/ai-go-router")
	req.Header.Set("X-Title", "NusaNexus Router")
	for key, value := range a.headers {
		req.Header.Set(key, value)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, NewRetryableError(a.name, model, "network error", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		return nil, ClassifyHTTPError(resp.StatusCode, a.name, model, string(respBody), a.errorConfig)
	}

	chunks := make(chan ChatChunk, 10)
	go func() {
		defer close(chunks)
		defer resp.Body.Close()

		scanner := newSSEScanner(resp.Body)
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}
			line := scanner.Text()
			if line == "" || line == ": ping" {
				continue
			}
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				return
			}
			var chunk ChatChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}
			chunks <- chunk
		}
	}()

	return chunks, nil
}

func (a *OpenRouterAdapter) GetUsage(ctx context.Context) (map[string]interface{}, error) {
	return map[string]interface{}{
		"provider":  a.name,
		"supported": false,
		"reason":    "OpenRouter usage is tracked from local request logs in this router",
	}, nil
}

func (a *OpenRouterAdapter) Embeddings(ctx context.Context, request EmbeddingsRequest, model string) (EmbeddingsResponse, error) {
	// Override model with the target model from routing
	request.Model = model

	// Get account from context if specified, otherwise use round-robin
	accountName := ""
	if account := ctx.Value(AccountContextKey); account != nil {
		if accountStr, ok := account.(string); ok {
			accountName = accountStr
		}
	}
	_, apiKey := a.accountSelector.GetAccount(accountName)

	// Marshal request to JSON
	body, err := json.Marshal(request)
	if err != nil {
		return EmbeddingsResponse{}, NewNonRetryableError(a.name, model, "failed to marshal request", err)
	}

	// Create HTTP request
	endpoint := endpoints.BuildOpenAI(a.baseURL, "/embeddings")
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return EmbeddingsResponse{}, NewNonRetryableError(a.name, model, "failed to create request", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("HTTP-Referer", "https://github.com/edodoyokz/ai-go-router") // OpenRouter requirement
	req.Header.Set("X-Title", "NusaNexus Router")                               // OpenRouter requirement

	// Apply provider-specific headers
	for key, value := range a.headers {
		req.Header.Set(key, value)
	}

	// Execute request
	resp, err := a.httpClient.Do(req)
	if err != nil {
		// Network errors are retryable
		return EmbeddingsResponse{}, NewRetryableError(a.name, model, "network error", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return EmbeddingsResponse{}, NewRetryableError(a.name, model, "failed to read response", err)
	}

	// Check for HTTP errors
	if resp.StatusCode != http.StatusOK {
		// Try to parse error response
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

		return EmbeddingsResponse{}, ClassifyHTTPError(resp.StatusCode, a.name, model, message, a.errorConfig)
	}

	// Parse successful response
	var embResp EmbeddingsResponse
	if err := json.Unmarshal(respBody, &embResp); err != nil {
		return EmbeddingsResponse{}, &ProviderError{
			Provider: a.name,
			Model:    model,
			Type:     ErrInvalidUpstreamResponse,
			Message:  "failed to parse response",
			Cause:    err,
		}
	}

	return embResp, nil
}

func (a *OpenRouterAdapter) AudioSpeech(ctx context.Context, request AudioSpeechRequest, model string) (AudioSpeechResponse, error) {
	// OpenRouter doesn't have a native TTS API
	// This endpoint is not supported by OpenRouter
	return AudioSpeechResponse{}, NewNonRetryableError(a.name, model, "audio/speech not supported by OpenRouter", nil)
}

func (a *OpenRouterAdapter) ImagesGenerations(ctx context.Context, request ImagesGenerationsRequest, model string) (ImagesGenerationsResponse, error) {
	// OpenRouter doesn't have a native image generation API
	// This endpoint is not supported by OpenRouter
	return ImagesGenerationsResponse{}, NewNonRetryableError(a.name, model, "images/generations not supported by OpenRouter", nil)
}
