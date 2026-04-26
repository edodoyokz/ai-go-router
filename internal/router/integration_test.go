package router

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/edodoyokz/ai-go-router/internal/config"
	"github.com/edodoyokz/ai-go-router/internal/providers"
)

// mockProvider is a test adapter that simulates a provider
type mockProvider struct {
	name      string
	fail      bool
	failMsg   string
	retryable bool
}

func (m *mockProvider) Name() string {
	return m.name
}

func (m *mockProvider) ChatCompletion(ctx context.Context, request providers.ChatRequest, model string) (providers.ChatResponse, error) {
	if m.fail {
		if m.retryable {
			return providers.ChatResponse{}, providers.NewRetryableError(m.name, model, m.failMsg, nil)
		}
		return providers.ChatResponse{}, providers.NewNonRetryableError(m.name, model, m.failMsg, nil)
	}

	return providers.ChatResponse{
		ID:      "test-id",
		Object:  "chat.completion",
		Created: 1234567890,
		Model:   model,
		Choices: []providers.ChatChoice{
			{
				Index: 0,
				Message: providers.ChatMessage{
					Role:    "assistant",
					Content: "Test response from " + m.name,
				},
				FinishReason: "stop",
			},
		},
		Usage: &providers.Usage{
			PromptTokens:     10,
			CompletionTokens: 20,
			TotalTokens:      30,
		},
	}, nil
}

func (m *mockProvider) StreamChatCompletion(ctx context.Context, request providers.ChatRequest, model string) (<-chan providers.ChatChunk, error) {
	// Streaming not implemented in mock
	return nil, fmt.Errorf("streaming not implemented in mock provider")
}

func (m *mockProvider) GetUsage(ctx context.Context) (map[string]interface{}, error) {
	// Usage fetching not implemented in mock
	return nil, fmt.Errorf("usage fetching not implemented in mock provider")
}

func (m *mockProvider) Embeddings(ctx context.Context, request providers.EmbeddingsRequest, model string) (providers.EmbeddingsResponse, error) {
	// Embeddings not implemented in mock
	return providers.EmbeddingsResponse{}, fmt.Errorf("embeddings not implemented in mock provider")
}

func (m *mockProvider) AudioSpeech(ctx context.Context, request providers.AudioSpeechRequest, model string) (providers.AudioSpeechResponse, error) {
	// AudioSpeech not implemented in mock
	return providers.AudioSpeechResponse{}, fmt.Errorf("audio/speech not implemented in mock provider")
}

func (m *mockProvider) ImagesGenerations(ctx context.Context, request providers.ImagesGenerationsRequest, model string) (providers.ImagesGenerationsResponse, error) {
	// ImagesGenerations not implemented in mock
	return providers.ImagesGenerationsResponse{}, fmt.Errorf("images/generations not implemented in mock provider")
}

func TestIntegrationEndToEnd(t *testing.T) {
	// Create mock providers
	mockOpenAI := &mockProvider{name: "openai"}
	mockAnthropic := &mockProvider{name: "anthropic"}

	registry := providers.NewRegistry(mockOpenAI, mockAnthropic)

	// Configure routes
	routes := map[string]config.RouteConfig{
		"test-model": {
			Strategy: "fallback",
			Targets: []config.RouteTarget{
				{Provider: "openai", Model: "gpt-4"},
				{Provider: "anthropic", Model: "claude-3-opus"},
			},
		},
	}

	retryConfig := config.RetryConfig{
		MaxAttempts:      3,
		InitialBackoffMs: 10,
		MaxBackoffMs:     100,
	}

	engine := NewEngine(routes, nil, registry, retryConfig)

	// Test successful request
	t.Run("successful request", func(t *testing.T) {
		request := providers.ChatRequest{
			Model: "test-model",
			Messages: []providers.ChatMessage{
				{Role: "user", Content: "Hello"},
			},
		}

		response, provider, err := engine.ChatCompletion(context.Background(), request)
		if err != nil {
			t.Fatalf("ChatCompletion() error = %v", err)
		}

		if response.ID != "test-id" {
			t.Errorf("Response ID = %v, want test-id", response.ID)
		}

		if provider != "openai" {
			t.Errorf("Provider = %v, want openai", provider)
		}

		if len(response.Choices) == 0 {
			t.Error("Response has no choices")
		} else {
			if response.Choices[0].Message.Content != "Test response from openai" {
				t.Errorf("Content = %v, want 'Test response from openai'", response.Choices[0].Message.Content)
			}
		}
	})

	// Test direct route
	t.Run("direct route", func(t *testing.T) {
		request := providers.ChatRequest{
			Model: "anthropic/claude-3-opus",
			Messages: []providers.ChatMessage{
				{Role: "user", Content: "Hello"},
			},
		}

		_, provider, err := engine.ChatCompletion(context.Background(), request)
		if err != nil {
			t.Fatalf("ChatCompletion() error = %v", err)
		}

		if provider != "anthropic" {
			t.Errorf("Provider = %v, want anthropic", provider)
		}
	})
}

func TestIntegrationWithHTTPServer(t *testing.T) {
	// Create a mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		// Return a mock OpenAI response
		response := map[string]interface{}{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"created": 1234567890,
			"model":   "gpt-4",
			"choices": []map[string]interface{}{
				{
					"index": 0,
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "Mock response",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]interface{}{
				"prompt_tokens":     10,
				"completion_tokens": 20,
				"total_tokens":      30,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	// Create a real OpenAI adapter pointing to the mock server
	cfg := config.ProviderConfig{
		Name:    "mock-openai",
		Type:    "openai_compat",
		BaseURL: server.URL,
		APIKey:  "test-key",
		Headers: map[string]string{},
		Enabled: true,
	}

	errorConfig := config.ErrorConfig{
		TextRules:   []config.ErrorTextRule{},
		StatusRules: []config.ErrorStatusRule{},
	}

	adapter := providers.NewOpenAIAdapter(cfg, errorConfig, "")

	// Test the adapter directly
	t.Run("adapter call to mock server", func(t *testing.T) {
		request := providers.ChatRequest{
			Model: "gpt-4",
			Messages: []providers.ChatMessage{
				{Role: "user", Content: "Hello"},
			},
		}

		resp, err := adapter.ChatCompletion(context.Background(), request, "gpt-4")
		if err != nil {
			t.Fatalf("ChatCompletion() error = %v", err)
		}

		if resp.ID != "chatcmpl-test" {
			t.Errorf("Response ID = %v, want chatcmpl-test", resp.ID)
		}

		if resp.Choices[0].Message.Content != "Mock response" {
			t.Errorf("Content = %v, want 'Mock response'", resp.Choices[0].Message.Content)
		}
	})
}

func TestIntegrationFallbackBehavior(t *testing.T) {
	// Create mock providers - first one fails, second succeeds
	mockFail := &mockProvider{name: "failing", fail: true, failMsg: "provider unavailable", retryable: true}
	mockSuccess := &mockProvider{name: "success"}

	registry := providers.NewRegistry(mockFail, mockSuccess)

	// Configure routes with fallback chain
	routes := map[string]config.RouteConfig{
		"test-model": {
			Strategy: "fallback",
			Targets: []config.RouteTarget{
				{Provider: "failing", Model: "gpt-4"},
				{Provider: "success", Model: "claude-3-opus"},
			},
		},
	}

	retryConfig := config.RetryConfig{
		MaxAttempts:      2,
		InitialBackoffMs: 10,
		MaxBackoffMs:     50,
	}

	engine := NewEngine(routes, nil, registry, retryConfig)

	// Test fallback from failing to success
	t.Run("fallback from failing to success", func(t *testing.T) {
		request := providers.ChatRequest{
			Model: "test-model",
			Messages: []providers.ChatMessage{
				{Role: "user", Content: "Hello"},
			},
		}

		response, provider, err := engine.ChatCompletion(context.Background(), request)
		if err != nil {
			t.Fatalf("ChatCompletion() error = %v", err)
		}

		if provider != "success" {
			t.Errorf("Provider = %v, want success (fallback worked)", provider)
		}

		if response.ID != "test-id" {
			t.Errorf("Response ID = %v, want test-id", response.ID)
		}

		if response.Choices[0].Message.Content != "Test response from success" {
			t.Errorf("Content = %v, want 'Test response from success'", response.Choices[0].Message.Content)
		}
	})

	// Test all providers fail
	t.Run("all providers fail", func(t *testing.T) {
		mockAllFail1 := &mockProvider{name: "fail1", fail: true, failMsg: "error 1", retryable: true}
		mockAllFail2 := &mockProvider{name: "fail2", fail: true, failMsg: "error 2", retryable: true}

		registryAllFail := providers.NewRegistry(mockAllFail1, mockAllFail2)
		engineAllFail := NewEngine(routes, nil, registryAllFail, retryConfig)

		request := providers.ChatRequest{
			Model: "test-model",
			Messages: []providers.ChatMessage{
				{Role: "user", Content: "Hello"},
			},
		}

		_, _, err := engineAllFail.ChatCompletion(context.Background(), request)
		if err == nil {
			t.Error("ChatCompletion() expected error when all providers fail, got nil")
		}
	})

	// Test non-retryable error stops fallback
	t.Run("non-retryable error stops fallback", func(t *testing.T) {
		mockNonRetryable := &mockProvider{name: "nonretryable", fail: true, failMsg: "auth failed", retryable: false}

		// Only register the non-retryable provider - no fallback
		registryNonRetryable := providers.NewRegistry(mockNonRetryable)
		engineNonRetryable := NewEngine(routes, nil, registryNonRetryable, retryConfig)

		request := providers.ChatRequest{
			Model: "test-model",
			Messages: []providers.ChatMessage{
				{Role: "user", Content: "Hello"},
			},
		}

		_, _, err := engineNonRetryable.ChatCompletion(context.Background(), request)
		if err == nil {
			t.Error("ChatCompletion() expected error for non-retryable, got nil")
		}
	})
}
