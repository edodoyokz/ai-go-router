package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/edodoyokz/9router-go/internal/config"
)

// OpenAIAdapter implements the Adapter interface for OpenAI-compatible APIs
type OpenAIAdapter struct {
	name        string
	baseURL     string
	apiKey      string            // Deprecated: use accounts instead
	accounts    map[string]string // account name -> API key
	headers     map[string]string
	errorConfig config.ErrorConfig
	httpClient  *http.Client
	accountIdx  int // round-robin index for account selection
	accountMu   sync.Mutex
}

// NewOpenAIAdapter creates a new OpenAI-compatible adapter
func NewOpenAIAdapter(cfg config.ProviderConfig, errorConfig config.ErrorConfig) *OpenAIAdapter {
	adapter := &OpenAIAdapter{
		name:        cfg.Name,
		baseURL:     cfg.BaseURL,
		apiKey:      cfg.APIKey,
		headers:     cfg.Headers,
		errorConfig: errorConfig,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		accounts: make(map[string]string),
	}

	// Populate accounts from config
	for _, account := range cfg.Accounts {
		adapter.accounts[account.Name] = account.APIKey
	}

	return adapter
}

func (a *OpenAIAdapter) Name() string {
	return a.name
}

// getNextAccount selects the next account using round-robin
func (a *OpenAIAdapter) getNextAccount() (string, string) {
	a.accountMu.Lock()
	defer a.accountMu.Unlock()

	// If no accounts configured, use deprecated APIKey
	if len(a.accounts) == 0 {
		return "default", a.apiKey
	}

	// Get account names in a consistent order (sorted for deterministic round-robin)
	accountNames := make([]string, 0, len(a.accounts))
	for name := range a.accounts {
		accountNames = append(accountNames, name)
	}
	// Sort for deterministic order
	for i := 0; i < len(accountNames); i++ {
		for j := i + 1; j < len(accountNames); j++ {
			if accountNames[i] > accountNames[j] {
				accountNames[i], accountNames[j] = accountNames[j], accountNames[i]
			}
		}
	}

	// Round-robin selection
	accountName := accountNames[a.accountIdx%len(accountNames)]
	a.accountIdx++
	apiKey := a.accounts[accountName]

	return accountName, apiKey
}

func (a *OpenAIAdapter) ChatCompletion(ctx context.Context, request ChatRequest, model string) (ChatResponse, error) {
	// Override model with the target model from routing
	request.Model = model

	// Get account from context if specified, otherwise use round-robin
	apiKey := ""
	if account := ctx.Value(AccountContextKey); account != nil {
		if accountStr, ok := account.(string); ok {
			// Look up API key for specific account
			a.accountMu.Lock()
			if key, exists := a.accounts[accountStr]; exists {
				apiKey = key
			} else {
				// Account not found, fall back to deprecated APIKey
				apiKey = a.apiKey
			}
			a.accountMu.Unlock()
		}
	}

	// If no account specified in context, use round-robin
	if apiKey == "" {
		_, apiKey = a.getNextAccount()
	}

	// Marshal request to JSON
	body, err := json.Marshal(request)
	if err != nil {
		return ChatResponse{}, NewNonRetryableError(a.name, model, "failed to marshal request", err)
	}

	// Create HTTP request
	endpoint := a.baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return ChatResponse{}, NewNonRetryableError(a.name, model, "failed to create request", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

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
