package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/edodoyokz/ai-go-router/internal/config"
	"github.com/edodoyokz/ai-go-router/internal/providers/endpoints"
)

// createHTTPClient creates an HTTP client with optional proxy support
func createHTTPClient(proxyURL string) *http.Client {
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}

	if proxyURL != "" {
		if parsedProxyURL, err := url.Parse(proxyURL); err == nil {
			transport.Proxy = http.ProxyURL(parsedProxyURL)
		}
	}

	return &http.Client{
		Timeout:   60 * time.Second,
		Transport: transport,
	}
}

// OpenAIAdapter implements the Adapter interface for OpenAI-compatible APIs
type OpenAIAdapter struct {
	name            string
	baseURL         string
	headers         map[string]string
	errorConfig     config.ErrorConfig
	httpClients     []*http.Client // one per proxy URL; index 0 = no proxy
	proxyIdx        atomic.Uint64  // round-robin index for proxy pool
	accountSelector *AccountSelector
	gcpProjectID    string
}

// NewOpenAIAdapter creates a new OpenAI-compatible adapter
func NewOpenAIAdapter(cfg config.ProviderConfig, errorConfig config.ErrorConfig, proxyURL string) *OpenAIAdapter {
	// Build accounts map
	accounts := make(map[string]string)
	for _, account := range cfg.Accounts {
		accounts[account.Name] = account.APIKey
	}

	// Build HTTP client pool: provider proxy_urls take priority, fallback to global proxyURL
	var clients []*http.Client
	if len(cfg.ProxyURLs) > 0 {
		for _, purl := range cfg.ProxyURLs {
			clients = append(clients, createHTTPClient(purl))
		}
	} else {
		clients = []*http.Client{createHTTPClient(proxyURL)}
	}

	return &OpenAIAdapter{
		name:            cfg.Name,
		baseURL:         cfg.BaseURL,
		headers:         cfg.Headers,
		errorConfig:     errorConfig,
		httpClients:     clients,
		accountSelector: NewAccountSelector(accounts, cfg.APIKey),
		gcpProjectID:    cfg.GCPProjectID,
	}
}

// nextClient returns the next HTTP client from the pool using round-robin
func (a *OpenAIAdapter) nextClient() *http.Client {
	if len(a.httpClients) == 1 {
		return a.httpClients[0]
	}
	idx := a.proxyIdx.Add(1) - 1
	return a.httpClients[idx%uint64(len(a.httpClients))]
}

func (a *OpenAIAdapter) Name() string {
	return a.name
}

func (a *OpenAIAdapter) AccountNames() []string {
	return a.accountSelector.AccountNames()
}

func (a *OpenAIAdapter) ChatCompletion(ctx context.Context, request ChatRequest, model string) (ChatResponse, error) {
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
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	// Apply provider-specific headers
	for key, value := range a.headers {
		req.Header.Set(key, value)
	}

	// Inject GCP project ID header for Vertex AI / Google Cloud endpoints
	if a.gcpProjectID != "" {
		req.Header.Set("X-Goog-User-Project", a.gcpProjectID)
	}

	// Execute request
	resp, err := a.nextClient().Do(req)
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

func (a *OpenAIAdapter) StreamChatCompletion(ctx context.Context, request ChatRequest, model string) (<-chan ChatChunk, error) {
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
		return nil, NewNonRetryableError(a.name, model, "failed to marshal request", err)
	}

	// Create HTTP request
	endpoint := endpoints.BuildOpenAI(a.baseURL, "/chat/completions")
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, NewNonRetryableError(a.name, model, "failed to create request", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Accept", "text/event-stream")

	// Apply provider-specific headers
	for key, value := range a.headers {
		req.Header.Set(key, value)
	}

	// Execute request
	resp, err := a.nextClient().Do(req)
	if err != nil {
		return nil, NewRetryableError(a.name, model, "network error", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		return nil, ClassifyHTTPError(resp.StatusCode, a.name, model, string(respBody), a.errorConfig)
	}

	// Create output channel
	chunks := make(chan ChatChunk, 10)

	// Start goroutine to read SSE stream and forward chunks
	go func() {
		defer close(chunks)
		defer resp.Body.Close()

		scanner := newSSEScanner(resp.Body)
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
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
		}
	}()

	return chunks, nil
}

func (a *OpenAIAdapter) GetUsage(ctx context.Context) (map[string]interface{}, error) {
	return map[string]interface{}{
		"provider":  a.name,
		"supported": false,
		"reason":    "provider usage API is not available through this adapter; use local request logs",
	}, nil
}

func (a *OpenAIAdapter) Embeddings(ctx context.Context, request EmbeddingsRequest, model string) (EmbeddingsResponse, error) {
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

	// Apply provider-specific headers
	for key, value := range a.headers {
		req.Header.Set(key, value)
	}

	// Execute request
	resp, err := a.nextClient().Do(req)
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

func (a *OpenAIAdapter) AudioSpeech(ctx context.Context, request AudioSpeechRequest, model string) (AudioSpeechResponse, error) {
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
		return AudioSpeechResponse{}, NewNonRetryableError(a.name, model, "failed to marshal request", err)
	}

	// Create HTTP request
	endpoint := endpoints.BuildOpenAI(a.baseURL, "/audio/speech")
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return AudioSpeechResponse{}, NewNonRetryableError(a.name, model, "failed to create request", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	// Apply provider-specific headers
	for key, value := range a.headers {
		req.Header.Set(key, value)
	}

	// Execute request
	resp, err := a.nextClient().Do(req)
	if err != nil {
		// Network errors are retryable
		return AudioSpeechResponse{}, NewRetryableError(a.name, model, "network error", err)
	}
	defer resp.Body.Close()

	// Check for HTTP errors
	if resp.StatusCode != http.StatusOK {
		// Try to parse error response
		respBody, _ := io.ReadAll(resp.Body)
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

		return AudioSpeechResponse{}, ClassifyHTTPError(resp.StatusCode, a.name, model, message, a.errorConfig)
	}

	// Read audio data
	audioData, err := io.ReadAll(resp.Body)
	if err != nil {
		return AudioSpeechResponse{}, NewRetryableError(a.name, model, "failed to read response", err)
	}

	// Get content type
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "audio/mpeg" // Default to MP3
	}

	return AudioSpeechResponse{
		ContentType: contentType,
		Data:        audioData,
	}, nil
}

func (a *OpenAIAdapter) ImagesGenerations(ctx context.Context, request ImagesGenerationsRequest, model string) (ImagesGenerationsResponse, error) {
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
		return ImagesGenerationsResponse{}, NewNonRetryableError(a.name, model, "failed to marshal request", err)
	}

	// Create HTTP request
	endpoint := endpoints.BuildOpenAI(a.baseURL, "/images/generations")
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return ImagesGenerationsResponse{}, NewNonRetryableError(a.name, model, "failed to create request", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	// Apply provider-specific headers
	for key, value := range a.headers {
		req.Header.Set(key, value)
	}

	// Execute request
	resp, err := a.nextClient().Do(req)
	if err != nil {
		// Network errors are retryable
		return ImagesGenerationsResponse{}, NewRetryableError(a.name, model, "network error", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return ImagesGenerationsResponse{}, NewRetryableError(a.name, model, "failed to read response", err)
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

		return ImagesGenerationsResponse{}, ClassifyHTTPError(resp.StatusCode, a.name, model, message, a.errorConfig)
	}

	// Parse successful response
	var imgResp ImagesGenerationsResponse
	if err := json.Unmarshal(respBody, &imgResp); err != nil {
		return ImagesGenerationsResponse{}, &ProviderError{
			Provider: a.name,
			Model:    model,
			Type:     ErrInvalidUpstreamResponse,
			Message:  "failed to parse response",
			Cause:    err,
		}
	}

	return imgResp, nil
}
