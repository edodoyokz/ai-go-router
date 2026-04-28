package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/edodoyokz/ai-go-router/internal/config"
	"github.com/edodoyokz/ai-go-router/internal/providers/catalog"
)

func TestBuildRegistryFromConfig_AllProvidersDisabled_AllowsEmptyRegistry(t *testing.T) {
	cfg := config.Config{
		Providers: []config.ProviderConfig{
			{
				Name:    "openai",
				Type:    "openai_compat",
				BaseURL: "https://api.openai.com/v1",
				APIKey:  "sk-test",
				Enabled: false,
			},
		},
	}

	registry, err := BuildRegistryFromConfig(cfg)
	if err != nil {
		t.Fatalf("BuildRegistryFromConfig() unexpected error: %v", err)
	}
	if registry == nil {
		t.Fatal("expected non-nil empty registry")
	}
	if _, err := registry.Get("openai"); err == nil {
		t.Fatal("expected disabled provider to be absent from registry")
	}
}

func TestBuildRegistryFromConfig_RejectsPlannedCatalogProvider(t *testing.T) {
	cfg := config.Config{Providers: []config.ProviderConfig{{
		Name:       "ollama-local",
		ProviderID: "ollama-local",
		Type:       "ollama-local",
		BaseURL:    "http://localhost:11434",
		Enabled:    true,
	}}}
	if _, err := BuildRegistryFromConfig(cfg); err == nil {
		t.Fatalf("expected planned provider to be rejected")
	}
}

func TestBuildRegistryFromConfig_GitHubCopilotAddsRuntimeHeaders(t *testing.T) {
	var gotAuth, gotIntegration, gotPlugin string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotIntegration = r.Header.Get("copilot-integration-id")
		gotPlugin = r.Header.Get("editor-plugin-version")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ChatResponse{
			ID:      "ok",
			Object:  "chat.completion",
			Model:   "gpt-test",
			Choices: []ChatChoice{{Index: 0, Message: ChatMessage{Role: "assistant", Content: "ok"}}},
		})
	}))
	defer server.Close()

	cfg := config.Config{Providers: []config.ProviderConfig{{
		Name:       "github",
		ProviderID: "github",
		Type:       "github",
		BaseURL:    server.URL,
		APIKey:     "copilot-token",
		Enabled:    true,
	}}}
	registry, err := BuildRegistryFromConfig(cfg)
	if err != nil {
		t.Fatalf("BuildRegistryFromConfig() error = %v", err)
	}
	adapter, err := registry.Get("github")
	if err != nil {
		t.Fatalf("Get github adapter: %v", err)
	}
	if _, err := adapter.ChatCompletion(context.Background(), ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "hi"}}}, "gpt-test"); err != nil {
		t.Fatalf("ChatCompletion() error = %v", err)
	}
	if gotAuth != "Bearer copilot-token" || gotIntegration != "vscode-chat" || gotPlugin == "" {
		t.Fatalf("headers auth=%q integration=%q plugin=%q", gotAuth, gotIntegration, gotPlugin)
	}
}

func TestBuildRegistryFromConfig_SupportsCatalogOpenAICompatibleAlias(t *testing.T) {
	cfg := config.Config{Providers: []config.ProviderConfig{{
		Name:    "perplexity",
		Type:    "pplx",
		BaseURL: "https://api.perplexity.ai",
		APIKey:  "test",
		Enabled: true,
	}}}
	registry, err := BuildRegistryFromConfig(cfg)
	if err != nil {
		t.Fatalf("BuildRegistryFromConfig() error = %v", err)
	}
	if _, err := registry.Get("perplexity"); err != nil {
		t.Fatalf("expected provider registered by configured name: %v", err)
	}
}

func TestBuildRegistryFromConfig_SupportsOllama(t *testing.T) {
	cfg := config.Config{Providers: []config.ProviderConfig{{
		Name:    "ollama",
		Type:    "ollama",
		BaseURL: "http://127.0.0.1:11434",
		Enabled: true,
	}}}
	registry, err := BuildRegistryFromConfig(cfg)
	if err != nil {
		t.Fatalf("BuildRegistryFromConfig() error = %v", err)
	}
	adapter, err := registry.Get("ollama")
	if err != nil {
		t.Fatalf("expected ollama adapter: %v", err)
	}
	if _, ok := adapter.(*OllamaAdapter); !ok {
		t.Fatalf("adapter type = %T, want *OllamaAdapter", adapter)
	}
}

func TestBuildRegistryFromConfig_AllSupportedCatalogProvidersBuildable(t *testing.T) {
	for _, def := range catalog.List() {
		if def.ExecutionStatus != "supported" {
			continue
		}
		providerType := def.ID
		if def.Alias != "" {
			providerType = def.Alias
		}
		cfg := config.Config{Providers: []config.ProviderConfig{{
			Name:       def.ID,
			ProviderID: def.ID,
			Type:       providerType,
			BaseURL:    firstNonEmpty(def.DefaultBaseURL, "https://example.invalid/v1"),
			APIKey:     "test-key",
			Enabled:    true,
		}}}
		if def.NoAuth {
			cfg.Providers[0].APIKey = ""
		}
		if _, err := BuildRegistryFromConfig(cfg); err != nil {
			t.Fatalf("supported provider %s not buildable: %v", def.ID, err)
		}
	}
}

func TestBuildRegistryFromConfig_AllPlannedOrDisabledProvidersRejected(t *testing.T) {
	for _, def := range catalog.List() {
		if def.ExecutionStatus != "planned" && def.ExecutionStatus != "disabled" {
			continue
		}
		providerType := def.ID
		if def.Alias != "" {
			providerType = def.Alias
		}
		cfg := config.Config{Providers: []config.ProviderConfig{{
			Name:       def.ID,
			ProviderID: def.ID,
			Type:       providerType,
			BaseURL:    "https://example.invalid/v1",
			APIKey:     "test-key",
			Enabled:    true,
		}}}
		_, err := BuildRegistryFromConfig(cfg)
		if err == nil {
			t.Fatalf("expected %s provider %s to be rejected", def.ExecutionStatus, def.ID)
		}
		if !strings.Contains(strings.ToLower(err.Error()), "cannot be used for runtime routing") {
			t.Fatalf("expected clear runtime rejection for provider %s, got: %v", def.ID, err)
		}
	}
}

func firstNonEmpty(items ...string) string {
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			return item
		}
	}
	return ""
}
