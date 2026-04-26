package router

import (
	"context"
	"testing"

	"github.com/edodoyokz/9router-go/internal/config"
	"github.com/edodoyokz/9router-go/internal/providers"
)

func TestResolveTargets(t *testing.T) {
	tests := []struct {
		name        string
		model       string
		routes      map[string]config.RouteConfig
		modelAliases map[string]config.ModelAlias
		wantTargets []config.RouteTarget
		wantNil     bool
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
			name: "route config with single target",
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
			name: "route config with multiple targets",
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
			name:  "invalid model - no alias or route",
			model: "unknown-model",
			wantNil: true,
		},
		{
			name:  "invalid direct route - no slash",
			model: "invalid-format",
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
