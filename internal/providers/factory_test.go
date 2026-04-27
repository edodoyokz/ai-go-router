package providers

import (
	"testing"

	"github.com/edodoyokz/ai-go-router/internal/config"
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
