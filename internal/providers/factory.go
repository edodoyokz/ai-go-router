package providers

import (
	"fmt"
	"strings"

	"github.com/edodoyokz/ai-go-router/internal/config"
	"github.com/edodoyokz/ai-go-router/internal/providers/catalog"
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

		providerID := strings.TrimSpace(provider.ProviderID)
		if providerID == "" {
			providerID = catalog.InferProviderID(provider.Type, provider.Name)
		}
		if def, ok := catalog.Get(providerID); ok {
			if def.ExecutionStatus == "planned" || def.ExecutionStatus == "disabled" {
				return nil, fmt.Errorf("provider %s is %s and cannot be used for runtime routing", providerID, def.ExecutionStatus)
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
		case "azure":
			adapter = NewAzureAdapter(provider, cfg.Errors, proxyURL)
		case "qwen", "qw":
			adapter = NewQwenAdapter(provider, cfg.Errors, proxyURL)
		case "grok-web", "gw":
			adapter = NewGrokWebAdapter(provider, cfg.Errors, proxyURL)
		case "perplexity-web", "pw":
			adapter = NewPerplexityWebAdapter(provider, cfg.Errors, proxyURL)
		case "elevenlabs", "el", "cartesia", "playht", "local-device", "google-tts", "edge-tts", "deepgram", "dg", "assemblyai", "aai", "nanobanana", "nb", "sdwebui", "comfyui", "huggingface", "hf", "tavily", "brave-search", "brave", "serper", "exa", "searxng", "firecrawl":
			adapter = NewMediaSearchAdapter(provider, cfg.Errors, proxyURL)
		case "opencode-go", "ocg":
			adapter = NewOpenCodeGoAdapter(provider, cfg.Errors, proxyURL)
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
						return nil, fmt.Errorf("provider %s executor build failed: %w", def.ID, err)
					}
					adapter, err = NewExecutorBridge(provider.Name, executor)
					if err != nil {
						return nil, fmt.Errorf("provider %s executor bridge failed: %w", def.ID, err)
					}
				}
			}
			if adapter != nil {
				break
			}
			return nil, fmt.Errorf("unsupported provider type: %s (provider: %s)", provider.Type, provider.Name)
		}

		adapters = append(adapters, adapter)
	}

	// Allow an empty registry during initial onboarding/setup mode.
	// Readiness and inference handlers can surface the not-configured state
	// without preventing the app and UI from starting.
	return NewRegistry(adapters...), nil
}

func withGitHubCopilotHeaders(provider config.ProviderConfig) config.ProviderConfig {
	if provider.BaseURL == "" {
		provider.BaseURL = "https://api.githubcopilot.com"
	}
	if provider.Headers == nil {
		provider.Headers = map[string]string{}
	}
	defaults := map[string]string{
		"copilot-integration-id": "vscode-chat",
		"editor-version":         "vscode/1.85.0",
		"editor-plugin-version":  "copilot-chat/0.26.7",
		"user-agent":             "GitHubCopilotChat/0.26.7",
		"x-github-api-version":   "2025-04-01",
	}
	for key, value := range defaults {
		if strings.TrimSpace(provider.Headers[key]) == "" {
			provider.Headers[key] = value
		}
	}
	return provider
}
