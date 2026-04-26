package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/edodoyokz/9router-go/internal/config"
	"github.com/edodoyokz/9router-go/internal/translator"
)

// AnthropicAdapter implements the Adapter interface for Anthropic's Messages API
type AnthropicAdapter struct {
	name            string
	baseURL         string
	headers         map[string]string
	errorConfig     config.ErrorConfig
	httpClient      *http.Client
	translator      *translator.Registry
	accountSelector *AccountSelector
}

// NewAnthropicAdapter creates a new Anthropic adapter
func NewAnthropicAdapter(cfg config.ProviderConfig, errorConfig config.ErrorConfig, translator *translator.Registry) *AnthropicAdapter {
	// Build accounts map
	accounts := make(map[string]string)
	for _, account := range cfg.Accounts {
		accounts[account.Name] = account.APIKey
	}

	adapter := &AnthropicAdapter{
		name:        cfg.Name,
		baseURL:     cfg.BaseURL,
		headers:     cfg.Headers,
		errorConfig: errorConfig,
		translator:  translator,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		accountSelector: NewAccountSelector(accounts, cfg.APIKey),
	}

	return adapter
}

func (a *AnthropicAdapter) Name() string {
	return a.name
}

func (a *AnthropicAdapter) ChatCompletion(ctx context.Context, request ChatRequest, model string) (ChatResponse, error) {
	// Get account from context if specified, otherwise use round-robin
	accountName := ""
	apiKey := ""
	if account := ctx.Value(AccountContextKey); account != nil {
		if accountStr, ok := account.(string); ok {
			accountName = accountStr
		}
	}
	accountName, apiKey = a.accountSelector.GetAccount(accountName)

	// Convert ChatRequest to map for translation
	messages := make([]map[string]interface{}, len(request.Messages))
	for i, msg := range request.Messages {
		messages[i] = map[string]interface{}{
			"role":    msg.Role,
			"content": msg.Content,
		}
	}

	requestBody := map[string]interface{}{
		"model":    model,
		"messages": messages,
		"stream":   request.Stream,
	}

	// Only include optional fields if they have non-zero values
	if request.Temperature != nil && *request.Temperature > 0 {
		requestBody["temperature"] = *request.Temperature
	}
	if request.TopP != nil && *request.TopP > 0 {
		requestBody["top_p"] = *request.TopP
	}
	if request.MaxTokens != nil && *request.MaxTokens > 0 {
		requestBody["max_tokens"] = *request.MaxTokens
	}
	if request.Stop != nil && len(request.Stop) > 0 {
		requestBody["stop"] = request.Stop
	}

	// Translate OpenAI request to Anthropic format using translator layer
	reqTranslator, err := a.translator.GetRequestTranslator(translator.FormatOpenAI, translator.FormatClaude)
	if err != nil {
		return ChatResponse{}, NewNonRetryableError(a.name, model, "failed to get request translator", err)
	}

	anthropicReq, err := reqTranslator.TranslateRequest(ctx, translator.FormatOpenAI, translator.FormatClaude, requestBody)
	if err != nil {
		return ChatResponse{}, NewNonRetryableError(a.name, model, "failed to translate request", err)
	}

	// Marshal request to JSON
	body, err := json.Marshal(anthropicReq)
	if err != nil {
		return ChatResponse{}, NewNonRetryableError(a.name, model, "failed to marshal request", err)
	}

	// Create HTTP request
	endpoint := a.baseURL + "/v1/messages"
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return ChatResponse{}, NewNonRetryableError(a.name, model, "failed to create request", err)
	}

	// Set Anthropic-specific headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	// Apply provider-specific headers (can override defaults)
	for key, value := range a.headers {
		req.Header.Set(key, value)
	}

	// Execute request
	resp, err := a.httpClient.Do(req)
	if err != nil {
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
		var errorResp struct {
			Type  string `json:"type"`
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(respBody, &errorResp)

		message := errorResp.Error.Message
		if message == "" {
			message = string(respBody)
		}

		return ChatResponse{}, ClassifyHTTPError(resp.StatusCode, a.name, model, message, a.errorConfig)
	}

	// Translate Anthropic response to OpenAI format using translator layer
	respTranslator, err := a.translator.GetResponseTranslator(translator.FormatClaude, translator.FormatOpenAI)
	if err != nil {
		return ChatResponse{}, NewNonRetryableError(a.name, model, "failed to get response translator", err)
	}

	openaiRespBody, err := respTranslator.TranslateResponse(ctx, translator.FormatClaude, translator.FormatOpenAI, respBody)
	if err != nil {
		return ChatResponse{}, &ProviderError{
			Provider: a.name,
			Model:    model,
			Type:     ErrInvalidUpstreamResponse,
			Message:  "failed to translate response",
			Cause:    err,
		}
	}

	// Parse OpenAI format response
	var response ChatResponse
	if err := json.Unmarshal(openaiRespBody, &response); err != nil {
		return ChatResponse{}, &ProviderError{
			Provider: a.name,
			Model:    model,
			Type:     ErrInvalidUpstreamResponse,
			Message:  "failed to parse translated response",
			Cause:    err,
		}
	}

	return response, nil
}

func (a *AnthropicAdapter) StreamChatCompletion(ctx context.Context, request ChatRequest, model string) (<-chan ChatChunk, error) {
	// Streaming not implemented for MVP
	// This requires SSE parsing and chunk forwarding with translation
	return nil, fmt.Errorf("streaming not implemented for Anthropic adapter")
}

func (a *AnthropicAdapter) GetUsage(ctx context.Context) (map[string]interface{}, error) {
	// Usage fetching not implemented for MVP
	// This requires provider-specific API calls
	return nil, fmt.Errorf("usage fetching not implemented for Anthropic adapter")
}
