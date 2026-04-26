package usage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/edodoyokz/ai-go-router/internal/config"
)

func TestNewUsageFetcher(t *testing.T) {
	uf := NewUsageFetcher()
	if uf == nil {
		t.Fatal("expected non-nil usage fetcher")
	}
	if uf.httpClient == nil {
		t.Error("expected initialized http client")
	}
}

func TestFetchOpenAIUsage_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-api-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`{"total_usage":{"prompt_tokens":1000,"completion_tokens":500,"total_tokens":1500}}`)); err != nil {
			t.Fatalf("write response: %v", err)
		}
	}))
	defer server.Close()

	uf := NewUsageFetcher()
	uf.openAIBaseURL = server.URL

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	data, err := uf.FetchOpenAIUsage(ctx, "test-api-key")
	if err != nil {
		t.Fatalf("FetchOpenAIUsage: %v", err)
	}
	if data.PromptTokens != 1000 {
		t.Errorf("PromptTokens: got %d, want 1000", data.PromptTokens)
	}
	if data.CompletionTokens != 500 {
		t.Errorf("CompletionTokens: got %d, want 500", data.CompletionTokens)
	}
	if data.TotalTokens != 1500 {
		t.Errorf("TotalTokens: got %d, want 1500", data.TotalTokens)
	}
	if data.Provider != "openai" {
		t.Errorf("Provider: got %q, want %q", data.Provider, "openai")
	}
}

func TestFetchOpenAIUsage_AuthError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	uf := NewUsageFetcher()
	uf.openAIBaseURL = server.URL

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := uf.FetchOpenAIUsage(ctx, "bad-key")
	if err == nil {
		t.Error("expected error for 401 response")
	}
}

func TestFetchAnthropicUsage_NotImplemented(t *testing.T) {
	uf := NewUsageFetcher()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := uf.FetchAnthropicUsage(ctx, "test-key")
	if err == nil {
		t.Error("expected error for unimplemented anthropic usage")
	}
	if err.Error() != "anthropic usage API not yet implemented" {
		t.Errorf("expected specific error message, got: %s", err.Error())
	}
}

func TestFetchUsage_UnsupportedProvider(t *testing.T) {
	uf := NewUsageFetcher()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	provider := config.ProviderConfig{
		Type: "unsupported",
	}

	_, err := uf.FetchUsage(ctx, provider)
	if err == nil {
		t.Error("expected error for unsupported provider type")
	}
}

func TestFetchOpenAIUsageMultiAccount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`{"total_usage":{"prompt_tokens":200,"completion_tokens":100,"total_tokens":300}}`)); err != nil {
			t.Fatalf("write response: %v", err)
		}
	}))
	defer server.Close()

	uf := NewUsageFetcher()
	uf.openAIBaseURL = server.URL

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	accounts := []config.AccountConfig{
		{Name: "account1", APIKey: "key1"},
		{Name: "account2", APIKey: "key2"},
	}

	data, err := uf.FetchOpenAIUsageMultiAccount(ctx, accounts)
	if err != nil {
		t.Fatalf("FetchOpenAIUsageMultiAccount: %v", err)
	}
	if data.Provider != "openai" {
		t.Errorf("expected provider openai, got %s", data.Provider)
	}
	if data.Account != "all" {
		t.Errorf("expected account 'all', got %s", data.Account)
	}
	// Two accounts × 300 tokens each = 600 total
	if data.TotalTokens != 600 {
		t.Errorf("expected 600 total tokens (2 accounts), got %d", data.TotalTokens)
	}
}

func TestFetchAnthropicUsageMultiAccount_NotImplemented(t *testing.T) {
	uf := NewUsageFetcher()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	accounts := []config.AccountConfig{
		{Name: "account1", APIKey: "key1"},
	}

	_, err := uf.FetchAnthropicUsageMultiAccount(ctx, accounts)
	if err == nil {
		t.Error("expected error for unimplemented anthropic multi-account")
	}
}

func TestUsageData_Struct(t *testing.T) {
	now := time.Now()
	data := UsageData{
		Provider:         "openai",
		Account:          "default",
		PromptTokens:     1000,
		CompletionTokens: 500,
		TotalTokens:      1500,
		CostUSD:          0.045,
		Timestamp:        now,
	}

	if data.Provider != "openai" {
		t.Error("unexpected provider")
	}
	if data.TotalTokens != 1500 {
		t.Errorf("expected 1500 total tokens, got %d", data.TotalTokens)
	}
	if data.CostUSD != 0.045 {
		t.Errorf("expected cost 0.045, got %f", data.CostUSD)
	}
}
