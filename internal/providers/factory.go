package providers

import (
	"fmt"

	"github.com/edodoyokz/ai-go-router/internal/config"
	"github.com/edodoyokz/ai-go-router/internal/translator"
)

func BuildRegistryFromConfig(cfg config.Config) (*Registry, error) {
	translatorRegistry := translator.NewRegistry()
	adapters := make([]Adapter, 0, len(cfg.Providers))

	// Get proxy URL from settings
	proxyURL := ""
	if cfg.Settings.OutboundProxyEnabled {
		proxyURL = cfg.Settings.OutboundProxyURL
	}

	for _, provider := range cfg.Providers {
		if !provider.Enabled {
			continue
		}

		var adapter Adapter
		switch provider.Type {
		case "openai_compat", "google", "groq", "deepseek", "cohere", "mistral":
			// Use OpenAI-compatible adapter for all OpenAI-compatible providers
			adapter = NewOpenAIAdapter(provider, cfg.Errors, proxyURL)
		case "openrouter":
			adapter = NewOpenRouterAdapter(provider, cfg.Errors, proxyURL)
		case "anthropic", "anthropic_compat":
			adapter = NewAnthropicAdapter(provider, cfg.Errors, translatorRegistry, proxyURL)
		default:
			return nil, fmt.Errorf("unsupported provider type: %s (provider: %s)", provider.Type, provider.Name)
		}

		adapters = append(adapters, adapter)
	}

	if len(adapters) == 0 {
		return nil, fmt.Errorf("no enabled providers configured")
	}

	return NewRegistry(adapters...), nil
}
