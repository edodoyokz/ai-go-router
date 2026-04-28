package router

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/edodoyokz/ai-go-router/internal/config"
	"github.com/edodoyokz/ai-go-router/internal/providers"
)

func TestResolveTargets(t *testing.T) {
	tests := []struct {
		name         string
		model        string
		routes       map[string]config.RouteConfig
		modelAliases map[string]config.ModelAlias
		wantTargets  []config.RouteTarget
		wantNil      bool
	}{
		{
			name:  "alias resolution",
			model: "gpt-4",
			modelAliases: map[string]config.ModelAlias{
				"gpt-4": {
					Provider: "openai",
					Model:    "gpt-4-turbo",
				},
			},
			wantTargets: []config.RouteTarget{
				{Provider: "openai", Model: "gpt-4-turbo"},
			},
		},
		{
			name:  "route config with single target",
			model: "claude-3-opus",
			routes: map[string]config.RouteConfig{
				"claude-3-opus": {
					Targets: []config.RouteTarget{
						{Provider: "anthropic", Model: "claude-3-opus-20240229"},
					},
				},
			},
			wantTargets: []config.RouteTarget{
				{Provider: "anthropic", Model: "claude-3-opus-20240229"},
			},
		},
		{
			name:  "route wins over alias with same name",
			model: "shared-name",
			routes: map[string]config.RouteConfig{
				"shared-name": {Targets: []config.RouteTarget{{Provider: "route-provider", Model: "route-model"}}},
			},
			modelAliases: map[string]config.ModelAlias{
				"shared-name": {Provider: "alias-provider", Model: "alias-model"},
			},
			wantTargets: []config.RouteTarget{{Provider: "route-provider", Model: "route-model"}},
		},
		{
			name:  "route config with multiple targets",
			model: "combo-model",
			routes: map[string]config.RouteConfig{
				"combo-model": {
					Targets: []config.RouteTarget{
						{Provider: "openai", Model: "gpt-4", Tier: "primary"},
						{Provider: "anthropic", Model: "claude-3-opus", Tier: "secondary"},
					},
				},
			},
			wantTargets: []config.RouteTarget{
				{Provider: "openai", Model: "gpt-4", Tier: "primary"},
				{Provider: "anthropic", Model: "claude-3-opus", Tier: "secondary"},
			},
		},
		{
			name:  "direct route provider/model",
			model: "openai/gpt-4",
			wantTargets: []config.RouteTarget{
				{Provider: "openai", Model: "gpt-4"},
			},
		},
		{
			name:    "invalid model - no alias or route",
			model:   "unknown-model",
			wantNil: true,
		},
		{
			name:    "invalid direct route - no slash",
			model:   "invalid-format",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := providers.NewRegistry()
			retryConfig := config.RetryConfig{
				MaxAttempts:      3,
				InitialBackoffMs: 100,
				MaxBackoffMs:     2000,
			}

			engine := NewEngine(tt.routes, tt.modelAliases, registry, retryConfig)

			targets := engine.ResolveTargets(tt.model)

			if tt.wantNil {
				if targets != nil {
					t.Errorf("ResolveTargets() expected nil, got %v", targets)
				}
				return
			}

			if targets == nil {
				t.Errorf("ResolveTargets() expected non-nil, got nil")
				return
			}

			if len(targets) != len(tt.wantTargets) {
				t.Errorf("ResolveTargets() got %d targets, want %d", len(targets), len(tt.wantTargets))
				return
			}

			for i, target := range targets {
				if target.Provider != tt.wantTargets[i].Provider {
					t.Errorf("ResolveTargets() target[%d].Provider = %v, want %v", i, target.Provider, tt.wantTargets[i].Provider)
				}
				if target.Model != tt.wantTargets[i].Model {
					t.Errorf("ResolveTargets() target[%d].Model = %v, want %v", i, target.Model, tt.wantTargets[i].Model)
				}
				if target.Tier != tt.wantTargets[i].Tier {
					t.Errorf("ResolveTargets() target[%d].Tier = %v, want %v", i, target.Tier, tt.wantTargets[i].Tier)
				}
			}
		})
	}
}

type accountAwareStreamingProvider struct {
	name     string
	accounts []string
	failed   map[string]bool
	seen     []string
}

func (m *accountAwareStreamingProvider) Name() string { return m.name }

func (m *accountAwareStreamingProvider) AccountNames() []string { return m.accounts }

func (m *accountAwareStreamingProvider) ChatCompletion(ctx context.Context, request providers.ChatRequest, model string) (providers.ChatResponse, error) {
	return providers.ChatResponse{}, fmt.Errorf("not implemented")
}

func (m *accountAwareStreamingProvider) StreamChatCompletion(ctx context.Context, request providers.ChatRequest, model string) (<-chan providers.ChatChunk, error) {
	account, _ := ctx.Value(providers.AccountContextKey).(string)
	m.seen = append(m.seen, account)
	if m.failed[account] {
		return nil, providers.NewQuotaExhaustedError(m.name, model, "quota exhausted")
	}
	chunks := make(chan providers.ChatChunk, 1)
	chunks <- providers.ChatChunk{Choices: []providers.ChunkChoice{{Delta: providers.ChunkDelta{Content: "ok"}}}}
	close(chunks)
	return chunks, nil
}

func (m *accountAwareStreamingProvider) GetUsage(ctx context.Context) (map[string]interface{}, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *accountAwareStreamingProvider) Embeddings(ctx context.Context, request providers.EmbeddingsRequest, model string) (providers.EmbeddingsResponse, error) {
	return providers.EmbeddingsResponse{}, fmt.Errorf("not implemented")
}

func (m *accountAwareStreamingProvider) AudioSpeech(ctx context.Context, request providers.AudioSpeechRequest, model string) (providers.AudioSpeechResponse, error) {
	return providers.AudioSpeechResponse{}, fmt.Errorf("not implemented")
}

func (m *accountAwareStreamingProvider) ImagesGenerations(ctx context.Context, request providers.ImagesGenerationsRequest, model string) (providers.ImagesGenerationsResponse, error) {
	return providers.ImagesGenerationsResponse{}, fmt.Errorf("not implemented")
}

func TestRoundRobin(t *testing.T) {
	routes := map[string]config.RouteConfig{
		"combo-model": {
			Strategy: "round-robin",
			Targets: []config.RouteTarget{
				{Provider: "openai", Model: "gpt-4"},
				{Provider: "anthropic", Model: "claude-3-opus"},
				{Provider: "deepseek", Model: "deepseek-chat"},
			},
		},
	}

	registry := providers.NewRegistry()
	retryConfig := config.RetryConfig{
		MaxAttempts:      3,
		InitialBackoffMs: 100,
		MaxBackoffMs:     2000,
	}

	engine := NewEngine(routes, nil, registry, retryConfig)

	// Test round-robin rotation
	firstCall := engine.ResolveTargets("combo-model")
	if firstCall[0].Provider != "openai" {
		t.Errorf("First call expected openai, got %s", firstCall[0].Provider)
	}

	secondCall := engine.ResolveTargets("combo-model")
	if secondCall[0].Provider != "anthropic" {
		t.Errorf("Second call expected anthropic, got %s", secondCall[0].Provider)
	}

	thirdCall := engine.ResolveTargets("combo-model")
	if thirdCall[0].Provider != "deepseek" {
		t.Errorf("Third call expected deepseek, got %s", thirdCall[0].Provider)
	}

	fourthCall := engine.ResolveTargets("combo-model")
	if fourthCall[0].Provider != "openai" {
		t.Errorf("Fourth call expected openai (wrap around), got %s", fourthCall[0].Provider)
	}
}

func TestChatCompletion_NoTargets(t *testing.T) {
	registry := providers.NewRegistry()
	retryConfig := config.RetryConfig{
		MaxAttempts:      3,
		InitialBackoffMs: 100,
		MaxBackoffMs:     2000,
	}

	engine := NewEngine(nil, nil, registry, retryConfig)

	request := providers.ChatRequest{
		Model: "unknown-model",
	}

	_, _, err := engine.ChatCompletion(context.Background(), request)
	if err == nil {
		t.Errorf("ChatCompletion() expected error for no targets, got nil")
	}
}

func TestStreamingChatCompletion_NoTargets(t *testing.T) {
	registry := providers.NewRegistry()
	retryConfig := config.RetryConfig{
		MaxAttempts:      3,
		InitialBackoffMs: 100,
		MaxBackoffMs:     2000,
	}

	engine := NewEngine(nil, nil, registry, retryConfig)

	request := providers.ChatRequest{
		Model: "unknown-model",
	}

	_, _, err := engine.StreamingChatCompletion(context.Background(), request)
	if err == nil {
		t.Errorf("StreamingChatCompletion() expected error for no targets, got nil")
	}
}

func TestStreamingChatCompletion_AccountFallback(t *testing.T) {
	provider := &accountAwareStreamingProvider{
		name:     "mock",
		accounts: []string{"a", "b"},
		failed:   map[string]bool{"a": true},
	}
	engine := NewEngine(map[string]config.RouteConfig{
		"combo": {Targets: []config.RouteTarget{{Provider: "mock", Model: "model"}}},
	}, nil, providers.NewRegistry(provider), config.RetryConfig{
		MaxAttempts:      1,
		InitialBackoffMs: 1,
		MaxBackoffMs:     1,
		MaxCooldownMs:    1,
	})

	chunks, selectedProvider, err := engine.StreamingChatCompletion(context.Background(), providers.ChatRequest{Model: "combo"})
	if err != nil {
		t.Fatalf("StreamingChatCompletion() error = %v", err)
	}
	if selectedProvider != "mock" {
		t.Fatalf("provider = %s, want mock", selectedProvider)
	}
	if len(provider.seen) != 2 || provider.seen[0] != "a" || provider.seen[1] != "b" {
		t.Fatalf("seen accounts = %v, want [a b]", provider.seen)
	}
	chunk := <-chunks
	if chunk.Choices[0].Delta.Content != "ok" {
		t.Fatalf("chunk content = %v, want ok", chunk.Choices[0].Delta.Content)
	}
}

func TestCircuitBreaker(t *testing.T) {
	// Test circuit breaker basic functionality
	cbConfig := providers.CircuitBreakerConfig{
		FailureThreshold: 2,
		OpenTimeout:      1 * time.Second,
		SuccessThreshold: 1,
	}
	manager := providers.NewCircuitBreakerManager(cbConfig)

	// Initial state should be closed
	if manager.GetState("test-provider") != providers.CircuitClosed {
		t.Errorf("Initial state should be CircuitClosed")
	}

	// Record failures to trip the breaker
	manager.RecordFailure("test-provider")
	if manager.GetState("test-provider") != providers.CircuitClosed {
		t.Errorf("State should still be CircuitClosed after 1 failure")
	}

	manager.RecordFailure("test-provider")
	if manager.GetState("test-provider") != providers.CircuitOpen {
		t.Errorf("State should be CircuitOpen after 2 failures")
	}

	// Should be open
	if !manager.IsOpen("test-provider") {
		t.Errorf("Circuit should be open")
	}

	// Wait for timeout
	time.Sleep(1100 * time.Millisecond)

	// Should transition to half-open
	if manager.GetState("test-provider") != providers.CircuitHalfOpen {
		t.Errorf("State should be CircuitHalfOpen after timeout")
	}

	// Record success to close
	manager.RecordSuccess("test-provider")
	if manager.GetState("test-provider") != providers.CircuitClosed {
		t.Errorf("State should be CircuitClosed after success")
	}
}
