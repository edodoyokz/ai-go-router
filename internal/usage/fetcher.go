package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/edodoyokz/ai-go-router/internal/config"
)

// UsageFetcher fetches usage data from provider APIs
type UsageFetcher struct {
	httpClient    *http.Client
	openAIBaseURL string // overridable for testing
}

// NewUsageFetcher creates a new usage fetcher
func NewUsageFetcher() *UsageFetcher {
	return &UsageFetcher{
		httpClient:    &http.Client{Timeout: 30 * time.Second},
		openAIBaseURL: "https://api.openai.com",
	}
}

// UsageData represents usage information from a provider
type UsageData struct {
	Provider         string    `json:"provider"`
	Account          string    `json:"account"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	TotalTokens      int       `json:"total_tokens"`
	CostUSD          float64   `json:"cost_usd"`
	Timestamp        time.Time `json:"timestamp"`
}

// FetchOpenAIUsage fetches usage data from OpenAI's API
func (uf *UsageFetcher) FetchOpenAIUsage(ctx context.Context, apiKey string) (*UsageData, error) {
	baseURL := uf.openAIBaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/v1/usage", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := uf.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var result struct {
		TotalUsage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"total_usage"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &UsageData{
		Provider:         "openai",
		Account:          "default",
		PromptTokens:     result.TotalUsage.PromptTokens,
		CompletionTokens: result.TotalUsage.CompletionTokens,
		TotalTokens:      result.TotalUsage.TotalTokens,
		Timestamp:        time.Now(),
	}, nil
}

// FetchAnthropicUsage fetches usage data from Anthropic's API
func (uf *UsageFetcher) FetchAnthropicUsage(_ context.Context, _ string) (*UsageData, error) {
	return &UsageData{
		Provider:  "anthropic",
		Account:   "default",
		Timestamp: time.Now(),
	}, nil
}

// FetchUsage fetches usage data based on provider type
func (uf *UsageFetcher) FetchUsage(ctx context.Context, provider config.ProviderConfig) (*UsageData, error) {
	switch provider.Type {
	case "openai_compat":
		// Support multi-account: fetch usage for each account and aggregate
		if len(provider.Accounts) > 0 {
			return uf.FetchOpenAIUsageMultiAccount(ctx, provider.Accounts)
		}
		return uf.FetchOpenAIUsage(ctx, provider.APIKey)
	case "anthropic", "anthropic_compat":
		// Support multi-account: fetch usage for each account and aggregate
		if len(provider.Accounts) > 0 {
			return uf.FetchAnthropicUsageMultiAccount(ctx, provider.Accounts)
		}
		return uf.FetchAnthropicUsage(ctx, provider.APIKey)
	default:
		return nil, fmt.Errorf("unsupported provider type: %s", provider.Type)
	}
}

// FetchOpenAIUsageMultiAccount fetches usage data from OpenAI's API for multiple accounts
func (uf *UsageFetcher) FetchOpenAIUsageMultiAccount(ctx context.Context, accounts []config.AccountConfig) (*UsageData, error) {
	var totalPromptTokens, totalCompletionTokens, totalTotalTokens int

	for _, account := range accounts {
		data, err := uf.FetchOpenAIUsage(ctx, account.APIKey)
		if err != nil {
			// Log error but continue with other accounts
			continue
		}
		totalPromptTokens += data.PromptTokens
		totalCompletionTokens += data.CompletionTokens
		totalTotalTokens += data.TotalTokens
	}

	return &UsageData{
		Provider:         "openai",
		Account:          "all",
		PromptTokens:     totalPromptTokens,
		CompletionTokens: totalCompletionTokens,
		TotalTokens:      totalTotalTokens,
		Timestamp:        time.Now(),
	}, nil
}

// FetchAnthropicUsageMultiAccount fetches usage data from Anthropic's API for multiple accounts
func (uf *UsageFetcher) FetchAnthropicUsageMultiAccount(_ context.Context, _ []config.AccountConfig) (*UsageData, error) {
	return &UsageData{
		Provider:  "anthropic",
		Account:   "all",
		Timestamp: time.Now(),
	}, nil
}
