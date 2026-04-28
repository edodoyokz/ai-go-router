package providers

import (
	"testing"
	"time"

	"github.com/edodoyokz/ai-go-router/internal/config"
	"github.com/edodoyokz/ai-go-router/internal/storage"
)

func TestConnectionToProviderConfig(t *testing.T) {
	now := time.Now()
	conn := storage.ProviderConnection{
		ID:           "conn-123",
		Provider:     "openai",
		ProviderType: "openai_compat",
		AuthType:     "api_key",
		Name:         "test-account",
		APIKey:       "sk-test",
		BaseURL:      "https://api.openai.com/v1",
		Enabled:      true,
		IsActive:     true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	cfg, err := ConnectionToProviderConfig(conn)
	if err != nil {
		t.Fatalf("ConnectionToProviderConfig() error = %v", err)
	}

	if cfg.Name != "db-conn-123" {
		t.Errorf("Name = %s, want db-conn-123", cfg.Name)
	}
	if cfg.ProviderID != "openai" {
		t.Errorf("ProviderID = %s, want openai", cfg.ProviderID)
	}
	if cfg.Type != "openai_compat" {
		t.Errorf("Type = %s, want openai_compat", cfg.Type)
	}
	if !cfg.Enabled {
		t.Errorf("Enabled = false, want true")
	}
	if len(cfg.Accounts) != 1 {
		t.Fatalf("Accounts length = %d, want 1", len(cfg.Accounts))
	}
	if cfg.Accounts[0].APIKey != "sk-test" {
		t.Errorf("Account APIKey = %s, want sk-test", cfg.Accounts[0].APIKey)
	}
}

func TestConnectionToProviderConfig_Disabled(t *testing.T) {
	conn := storage.ProviderConnection{
		ID:       "conn-123",
		Provider: "openai",
		Enabled:  false,
	}

	_, err := ConnectionToProviderConfig(conn)
	if err == nil {
		t.Errorf("Expected error for disabled connection, got nil")
	}
}

func TestHydrateProvidersFromDB(t *testing.T) {
	yamlProviders := []config.ProviderConfig{
		{
			Name:    "yaml-openai",
			Type:    "openai_compat",
			BaseURL: "https://api.openai.com/v1",
			APIKey:  "sk-yaml",
			Enabled: true,
		},
	}

	dbConnections := []storage.ProviderConnection{
		{
			ID:           "conn-1",
			Provider:     "anthropic",
			ProviderType: "anthropic_compat",
			APIKey:       "sk-db-1",
			BaseURL:      "https://api.anthropic.com",
			Enabled:      true,
			IsActive:     true,
		},
		{
			ID:           "conn-2",
			Provider:     "groq",
			ProviderType: "openai_compat",
			APIKey:       "sk-db-2",
			BaseURL:      "https://api.groq.com/openai/v1",
			Enabled:      true,
			IsActive:     true,
		},
		{
			ID:       "conn-3",
			Provider: "disabled",
			Enabled:  false,
		},
	}

	result, err := HydrateProvidersFromDB(yamlProviders, dbConnections)
	if err != nil {
		t.Fatalf("HydrateProvidersFromDB() error = %v", err)
	}

	// Should have 3 providers: 1 YAML + 2 enabled DB connections
	if len(result) != 3 {
		t.Errorf("Result length = %d, want 3", len(result))
	}

	// First should be YAML provider
	if result[0].Name != "yaml-openai" {
		t.Errorf("First provider name = %s, want yaml-openai", result[0].Name)
	}

	// Check DB providers are present
	foundAnthropic := false
	foundGroq := false
	for _, p := range result {
		if p.ProviderID == "anthropic" {
			foundAnthropic = true
		}
		if p.ProviderID == "groq" {
			foundGroq = true
		}
	}

	if !foundAnthropic {
		t.Errorf("Anthropic DB connection not found in result")
	}
	if !foundGroq {
		t.Errorf("Groq DB connection not found in result")
	}
}

func TestHydrateProvidersFromDB_NameConflict(t *testing.T) {
	yamlProviders := []config.ProviderConfig{
		{
			Name:    "db-conn-1",
			Type:    "openai_compat",
			Enabled: true,
		},
	}

	dbConnections := []storage.ProviderConnection{
		{
			ID:           "conn-1",
			Provider:     "openai",
			ProviderType: "openai_compat",
			Enabled:      true,
			IsActive:     true,
		},
	}

	_, err := HydrateProvidersFromDB(yamlProviders, dbConnections)
	if err == nil {
		t.Errorf("Expected name conflict error, got nil")
	}
}

func TestConnectionToRuntimeAccount(t *testing.T) {
	expires := time.Now().Add(time.Hour)
	conn := storage.ProviderConnection{
		ID:           "conn-123",
		Provider:     "openai",
		Name:         "test-account",
		AuthType:     "api_key",
		APIKey:       "sk-test",
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		IDToken:      "id-token",
		ExpiresAt:    &expires,
		BaseURL:      "https://api.openai.com/v1",
		Headers:      map[string]string{"X-Custom": "value"},
		ProviderSpecificData: map[string]any{
			"custom_field": "custom_value",
		},
	}

	account := ConnectionToRuntimeAccount(conn)

	if account.ProviderID != "openai" {
		t.Errorf("ProviderID = %s, want openai", account.ProviderID)
	}
	if account.ConnectionID != "conn-123" {
		t.Errorf("ConnectionID = %s, want conn-123", account.ConnectionID)
	}
	if account.Name != "test-account" {
		t.Errorf("Name = %s, want test-account", account.Name)
	}
	if account.APIKey != "sk-test" {
		t.Errorf("APIKey = %s, want sk-test", account.APIKey)
	}
	if account.AccessToken != "access-token" {
		t.Errorf("AccessToken = %s, want access-token", account.AccessToken)
	}
	if account.ExpiresAt == nil || !account.ExpiresAt.Equal(expires) {
		t.Errorf("ExpiresAt mismatch")
	}
	if account.Headers["X-Custom"] != "value" {
		t.Errorf("Headers not preserved")
	}
	if account.ProviderSpecificData["custom_field"] != "custom_value" {
		t.Errorf("ProviderSpecificData not preserved")
	}
}

func TestHasCredentials(t *testing.T) {
	tests := []struct {
		name string
		conn storage.ProviderConnection
		want bool
	}{
		{
			name: "has api key",
			conn: storage.ProviderConnection{APIKey: "sk-test"},
			want: true,
		},
		{
			name: "has access token",
			conn: storage.ProviderConnection{AccessToken: "token"},
			want: true,
		},
		{
			name: "has refresh token",
			conn: storage.ProviderConnection{RefreshToken: "refresh"},
			want: true,
		},
		{
			name: "has id token",
			conn: storage.ProviderConnection{IDToken: "id"},
			want: true,
		},
		{
			name: "no credentials",
			conn: storage.ProviderConnection{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasCredentials(tt.conn)
			if got != tt.want {
				t.Errorf("hasCredentials() = %v, want %v", got, tt.want)
			}
		})
	}
}
