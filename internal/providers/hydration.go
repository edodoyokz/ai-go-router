package providers

import (
	"context"
	"fmt"
	"time"

	"github.com/edodoyokz/ai-go-router/internal/config"
	"github.com/edodoyokz/ai-go-router/internal/providers/catalog"
	"github.com/edodoyokz/ai-go-router/internal/storage"
	"github.com/edodoyokz/ai-go-router/internal/translator"
)

// ConnectionToProviderConfig converts a storage.ProviderConnection to config.ProviderConfig
func ConnectionToProviderConfig(conn storage.ProviderConnection) (config.ProviderConfig, error) {
	if !conn.Enabled {
		return config.ProviderConfig{}, fmt.Errorf("connection %s is disabled", conn.ID)
	}

	// Use connection ID as unique name to avoid conflicts with YAML providers
	name := fmt.Sprintf("db-%s", conn.ID)

	// Determine provider type from catalog or use stored type
	providerType := conn.ProviderType
	if providerType == "" {
		providerType = conn.Provider
	}

	cfg := config.ProviderConfig{
		Name:                 name,
		ProviderID:           conn.Provider,
		Type:                 providerType,
		Format:               conn.Format,
		BaseURL:              conn.BaseURL,
		APIKey:               conn.APIKey,
		AuthType:             conn.AuthType,
		Headers:              conn.Headers,
		Enabled:              true,
		ProviderSpecificData: conn.ProviderSpecificData,
	}

	// Convert connection to account if it has credentials
	if hasCredentials(conn) {
		account := config.AccountConfig{
			ID:                   conn.ID,
			Name:                 conn.Name,
			AuthType:             conn.AuthType,
			APIKey:               conn.APIKey,
			AccessToken:          conn.AccessToken,
			RefreshToken:         conn.RefreshToken,
			IDToken:              conn.IDToken,
			ExpiresAt:            conn.ExpiresAt,
			ProviderSpecificData: conn.ProviderSpecificData,
			Enabled:              conn.IsActive,
			Priority:             conn.Priority,
			DefaultModel:         conn.DefaultModel,
		}
		cfg.Accounts = []config.AccountConfig{account}
	}

	return cfg, nil
}

// hasCredentials checks if a connection has any credentials
func hasCredentials(conn storage.ProviderConnection) bool {
	return conn.APIKey != "" ||
		conn.AccessToken != "" ||
		conn.RefreshToken != "" ||
		conn.IDToken != ""
}

// HydrateProvidersFromDB adds enabled DB provider connections to the provider list
func HydrateProvidersFromDB(yamlProviders []config.ProviderConfig, dbConnections []storage.ProviderConnection) ([]config.ProviderConfig, error) {
	result := make([]config.ProviderConfig, 0, len(yamlProviders)+len(dbConnections))

	// Add YAML providers first (they have precedence)
	yamlNames := make(map[string]bool)
	for _, p := range yamlProviders {
		result = append(result, p)
		yamlNames[p.Name] = true
	}

	// Add DB connections
	for _, conn := range dbConnections {
		if !conn.Enabled || !conn.IsActive {
			continue
		}

		cfg, err := ConnectionToProviderConfig(conn)
		if err != nil {
			// Log but don't fail entire hydration
			continue
		}

		// Check for name conflicts
		if yamlNames[cfg.Name] {
			return nil, fmt.Errorf("provider name conflict: DB connection %s conflicts with YAML provider %s", conn.ID, cfg.Name)
		}

		result = append(result, cfg)
	}

	return result, nil
}

// BuildHydratedRegistry builds a provider registry from YAML config + DB connections
func BuildHydratedRegistry(ctx context.Context, cfg config.Config, db *storage.DB) (*Registry, error) {
	translatorRegistry := translator.NewRegistry()

	// Get proxy URL from settings
	proxyURL := ""
	if cfg.Settings.OutboundProxyEnabled {
		proxyURL = cfg.Settings.OutboundProxyURL
	}

	// Load DB connections
	var dbConnections []storage.ProviderConnection
	if db != nil {
		active := true
		connections, err := db.ListProviderConnections(ctx, storage.ProviderConnectionFilter{Active: &active})
		if err == nil {
			dbConnections = connections
		}
	}

	// Hydrate: merge YAML + DB providers
	allProviders, err := HydrateProvidersFromDB(cfg.Providers, dbConnections)
	if err != nil {
		return nil, fmt.Errorf("provider hydration failed: %w", err)
	}

	// Build adapters from merged provider list
	adapters := make([]Adapter, 0, len(allProviders))

	for _, provider := range allProviders {
		if !provider.Enabled {
			continue
		}

		providerID := provider.ProviderID
		if providerID == "" {
			providerID = catalog.InferProviderID(provider.Type, provider.Name)
		}

		// Check catalog status
		if def, ok := catalog.Get(providerID); ok {
			if def.ExecutionStatus == "planned" || def.ExecutionStatus == "disabled" {
				// Skip planned/disabled providers (likely from DB connections for future providers)
				continue
			}
			if def.ID == "github" {
				provider = withGitHubCopilotHeaders(provider)
			}
		}

		var adapter Adapter
		switch provider.Type {
		case "openai", "openai_compat", "google", "groq", "deepseek", "cohere", "mistral", "xai":
			adapter = NewOpenAIAdapter(provider, cfg.Errors, proxyURL)
		case "openrouter":
			adapter = NewOpenRouterAdapter(provider, cfg.Errors, proxyURL)
		case "anthropic", "anthropic_compat":
			adapter = NewAnthropicAdapter(provider, cfg.Errors, translatorRegistry, proxyURL)
		case "ollama":
			adapter = NewOllamaAdapter(provider, cfg.Errors, translatorRegistry, proxyURL)
		case "qwen", "qw":
			adapter = NewQwenAdapter(provider, cfg.Errors, proxyURL)
		case "grok-web", "gw":
			adapter = NewGrokWebAdapter(provider, cfg.Errors, proxyURL)
		case "perplexity-web", "pw":
			adapter = NewPerplexityWebAdapter(provider, cfg.Errors, proxyURL)
		case "elevenlabs", "el", "cartesia", "playht", "local-device", "google-tts", "edge-tts", "deepgram", "dg", "assemblyai", "aai", "nanobanana", "nb", "sdwebui", "comfyui", "huggingface", "hf", "tavily", "brave-search", "brave", "serper", "exa", "searxng", "firecrawl":
			adapter = NewMediaSearchAdapter(provider, cfg.Errors, proxyURL)
		case "qoder", "qd":
			adapter = NewQoderAdapter(provider, cfg.Errors, proxyURL)
		case "iflow", "if":
			adapter = NewIFlowAdapter(provider, cfg.Errors, proxyURL)
		case "vertex", "vx":
			adapter = NewVertexAdapter(provider, cfg.Errors, translatorRegistry, proxyURL)
		case "vertex-partner", "vxp":
			adapter = NewVertexAdapter(provider, cfg.Errors, translatorRegistry, proxyURL)
		case "gemini-cli", "gc":
			adapter = NewGeminiCLIAdapter(provider, cfg.Errors, translatorRegistry, proxyURL)
		case "antigravity", "ag":
			adapter = NewAntigravityAdapter(provider, cfg.Errors, proxyURL)
		default:
			if def, ok := catalog.ResolveAlias(provider.Type); ok {
				switch def.ExecutionKind {
				case "qwen":
					adapter = NewQwenAdapter(provider, cfg.Errors, proxyURL)
				case "web":
					if def.ID == "grok-web" {
						adapter = NewGrokWebAdapter(provider, cfg.Errors, proxyURL)
					} else if def.ID == "perplexity-web" {
						adapter = NewPerplexityWebAdapter(provider, cfg.Errors, proxyURL)
					}
				case "openai_compatible":
					adapter = NewOpenAIAdapter(provider, cfg.Errors, proxyURL)
				case "anthropic_compatible":
					adapter = NewAnthropicAdapter(provider, cfg.Errors, translatorRegistry, proxyURL)
				case "ollama":
					adapter = NewOllamaAdapter(provider, cfg.Errors, translatorRegistry, proxyURL)
				case "media", "search":
					adapter = NewMediaSearchAdapter(provider, cfg.Errors, proxyURL)
				case "native":
					executor, err := BuildExecutor(def.ID, provider, cfg.Errors)
					if err != nil {
						continue
					}
					adapter, err = NewExecutorBridge(provider.Name, executor)
					if err != nil {
						continue
					}
				}
			}
			if adapter == nil {
				continue
			}
		}

		adapters = append(adapters, adapter)
	}

	return NewRegistry(adapters...), nil
}

// ConnectionToRuntimeAccount converts a storage.ProviderConnection to a RuntimeAccount
func ConnectionToRuntimeAccount(conn storage.ProviderConnection) RuntimeAccount {
	return RuntimeAccount{
		ProviderID:           conn.Provider,
		ConnectionID:         conn.ID,
		Name:                 conn.Name,
		AuthType:             conn.AuthType,
		APIKey:               conn.APIKey,
		AccessToken:          conn.AccessToken,
		RefreshToken:         conn.RefreshToken,
		IDToken:              conn.IDToken,
		ExpiresAt:            conn.ExpiresAt,
		BaseURL:              conn.BaseURL,
		Headers:              conn.Headers,
		ProviderSpecificData: conn.ProviderSpecificData,
	}
}

// RuntimeAccount represents provider credentials used during request execution
type RuntimeAccount struct {
	ProviderID           string
	ConnectionID         string
	Name                 string
	AuthType             string
	APIKey               string
	AccessToken          string
	RefreshToken         string
	IDToken              string
	ExpiresAt            *time.Time
	BaseURL              string
	Headers              map[string]string
	ProviderSpecificData map[string]any
}
