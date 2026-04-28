package providers

import (
	"strings"
	"testing"

	"github.com/edodoyokz/ai-go-router/internal/config"
)

func TestBatchANativeExecutors_RejectIncompleteCredentials(t *testing.T) {
	tests := []struct {
		name        string
		providerID  string
		cfg         config.ProviderConfig
		errContains string
	}{
		{name: "codex missing creds", providerID: "codex", cfg: config.ProviderConfig{Name: "codex", Type: "codex"}, errContains: "codex credentials incomplete"},
		{name: "github missing creds", providerID: "github", cfg: config.ProviderConfig{Name: "github", Type: "github"}, errContains: "github credentials incomplete"},
		{name: "cursor missing creds", providerID: "cursor", cfg: config.ProviderConfig{Name: "cursor", Type: "cursor"}, errContains: "cursor credentials incomplete"},
		{name: "kiro missing creds", providerID: "kiro", cfg: config.ProviderConfig{Name: "kiro", Type: "kiro"}, errContains: "kiro credentials incomplete"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BuildExecutor(tt.providerID, tt.cfg, config.ErrorConfig{})
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.errContains)) {
				t.Fatalf("error=%v want contains %q", err, tt.errContains)
			}
		})
	}
}

func TestBatchANativeExecutors_AcceptMinimalValidCredentials(t *testing.T) {
	tests := []struct {
		name       string
		providerID string
		cfg        config.ProviderConfig
	}{
		{name: "codex api key", providerID: "codex", cfg: config.ProviderConfig{Name: "codex", Type: "codex", APIKey: "token"}},
		{name: "github api key", providerID: "github", cfg: config.ProviderConfig{Name: "github", Type: "github", APIKey: "token"}},
		{name: "cursor access token", providerID: "cursor", cfg: config.ProviderConfig{Name: "cursor", Type: "cursor", Accounts: []config.AccountConfig{{Name: "acc", AccessToken: "token"}}}},
		{name: "kiro refresh token", providerID: "kiro", cfg: config.ProviderConfig{Name: "kiro", Type: "kiro", Accounts: []config.AccountConfig{{Name: "acc", RefreshToken: "refresh"}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := BuildExecutor(tt.providerID, tt.cfg, config.ErrorConfig{}); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
