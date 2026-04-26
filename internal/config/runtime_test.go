package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeConfig_GetAndUpdate(t *testing.T) {
	cfg := Config{
		Server: ServerConfig{
			Host:                  "localhost",
			Port:                  8080,
			APIKey:                "test-key",
			RequestTimeoutSeconds: 60,
		},
		Providers: []ProviderConfig{
			{Name: "test-provider", Type: "openai_compat", BaseURL: "https://api.test.com", APIKey: "test-key", Enabled: true},
		},
		Routes:       map[string]RouteConfig{},
		ModelAliases: map[string]ModelAlias{},
		Logging:      LoggingConfig{Level: "info"},
		Storage:      StorageConfig{SQLitePath: "./data/test.db"},
		Retry: RetryConfig{
			MaxAttempts:      3,
			InitialBackoffMs: 100,
			MaxBackoffMs:     2000,
		},
	}

	rc := NewRuntimeConfig(cfg, "")

	// Test Get
	retrieved := rc.Get()
	if retrieved.Server.Port != 8080 {
		t.Errorf("expected port 8080, got %d", retrieved.Server.Port)
	}

	// Test Update
	err := rc.Update(func(c *Config) error {
		c.Server.Port = 9090
		return nil
	})
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}

	updated := rc.Get()
	if updated.Server.Port != 9090 {
		t.Errorf("expected port 9090 after update, got %d", updated.Server.Port)
	}
}

func TestRuntimeConfig_AddProvider(t *testing.T) {
	cfg := Config{
		Server: ServerConfig{
			Host:                  "localhost",
			Port:                  8080,
			APIKey:                "test-key",
			RequestTimeoutSeconds: 60,
		},
		Providers:    []ProviderConfig{},
		Routes:       map[string]RouteConfig{},
		ModelAliases: map[string]ModelAlias{},
		Logging:      LoggingConfig{Level: "info"},
		Storage:      StorageConfig{SQLitePath: "./data/test.db"},
		Retry: RetryConfig{
			MaxAttempts:      3,
			InitialBackoffMs: 100,
			MaxBackoffMs:     2000,
		},
	}

	rc := NewRuntimeConfig(cfg, "")

	// Add provider
	provider := ProviderConfig{
		Name:    "new-provider",
		Type:    "openai_compat",
		BaseURL: "https://api.new.com",
		APIKey:  "test-key",
		Enabled: true,
	}

	err := rc.AddProvider(provider)
	if err != nil {
		t.Fatalf("add provider failed: %v", err)
	}

	// Verify
	retrieved, ok := rc.GetProvider("new-provider")
	if !ok {
		t.Fatal("provider not found after add")
	}
	if retrieved.Name != "new-provider" {
		t.Errorf("expected name 'new-provider', got %s", retrieved.Name)
	}

	// Test duplicate
	err = rc.AddProvider(provider)
	if err == nil {
		t.Error("expected error when adding duplicate provider")
	}
}

func TestRuntimeConfig_UpdateProvider(t *testing.T) {
	cfg := Config{
		Server: ServerConfig{
			Host:                  "localhost",
			Port:                  8080,
			APIKey:                "test-key",
			RequestTimeoutSeconds: 60,
		},
		Providers: []ProviderConfig{
			{Name: "test-provider", Type: "openai_compat", BaseURL: "https://api.test.com", APIKey: "test-key", Enabled: true},
		},
		Routes:       map[string]RouteConfig{},
		ModelAliases: map[string]ModelAlias{},
		Logging:      LoggingConfig{Level: "info"},
		Storage:      StorageConfig{SQLitePath: "./data/test.db"},
		Retry: RetryConfig{
			MaxAttempts:      3,
			InitialBackoffMs: 100,
			MaxBackoffMs:     2000,
		},
	}

	rc := NewRuntimeConfig(cfg, "")

	// Update provider (keep enabled=true to satisfy validation)
	updated := ProviderConfig{
		Name:    "test-provider",
		Type:    "openai_compat",
		BaseURL: "https://api.updated.com",
		APIKey:  "test-key",
		Enabled: true,
	}

	err := rc.UpdateProvider("test-provider", updated)
	if err != nil {
		t.Fatalf("update provider failed: %v", err)
	}

	// Verify
	retrieved, ok := rc.GetProvider("test-provider")
	if !ok {
		t.Fatal("provider not found after update")
	}
	if retrieved.BaseURL != "https://api.updated.com" {
		t.Errorf("expected base_url 'https://api.updated.com', got %s", retrieved.BaseURL)
	}

	// Test non-existent
	err = rc.UpdateProvider("non-existent", updated)
	if err == nil {
		t.Error("expected error when updating non-existent provider")
	}
}

func TestRuntimeConfig_DeleteProvider(t *testing.T) {
	cfg := Config{
		Server: ServerConfig{
			Host:                  "localhost",
			Port:                  8080,
			APIKey:                "test-key",
			RequestTimeoutSeconds: 60,
		},
		Providers: []ProviderConfig{
			{Name: "test-provider", Type: "openai_compat", BaseURL: "https://api.test.com", APIKey: "test-key", Enabled: true},
			{Name: "backup-provider", Type: "openai_compat", BaseURL: "https://api.backup.com", APIKey: "test-key", Enabled: true},
		},
		Routes:       map[string]RouteConfig{},
		ModelAliases: map[string]ModelAlias{},
		Logging:      LoggingConfig{Level: "info"},
		Storage:      StorageConfig{SQLitePath: "./data/test.db"},
		Retry: RetryConfig{
			MaxAttempts:      3,
			InitialBackoffMs: 100,
			MaxBackoffMs:     2000,
		},
	}

	rc := NewRuntimeConfig(cfg, "")

	// Delete provider (keep at least one provider for validation)
	err := rc.DeleteProvider("test-provider")
	if err != nil {
		t.Fatalf("delete provider failed: %v", err)
	}

	// Verify
	_, ok := rc.GetProvider("test-provider")
	if ok {
		t.Error("provider still exists after delete")
	}

	// Test non-existent
	err = rc.DeleteProvider("non-existent")
	if err == nil {
		t.Error("expected error when deleting non-existent provider")
	}
}

func TestRuntimeConfig_RouteOperations(t *testing.T) {
	cfg := Config{
		Server: ServerConfig{
			Host:                  "localhost",
			Port:                  8080,
			APIKey:                "test-key",
			RequestTimeoutSeconds: 60,
		},
		Providers: []ProviderConfig{
			{Name: "test-provider", Type: "openai_compat", BaseURL: "https://api.test.com", APIKey: "test-key", Enabled: true},
		},
		Routes:       map[string]RouteConfig{},
		ModelAliases: map[string]ModelAlias{},
		Logging:      LoggingConfig{Level: "info"},
		Storage:      StorageConfig{SQLitePath: "./data/test.db"},
		Retry: RetryConfig{
			MaxAttempts:      3,
			InitialBackoffMs: 100,
			MaxBackoffMs:     2000,
		},
	}

	rc := NewRuntimeConfig(cfg, "")

	// Add route
	route := RouteConfig{
		Strategy: "fallback",
		Targets: []RouteTarget{
			{Provider: "test-provider", Model: "gpt-4"},
		},
	}

	err := rc.AddRoute("test-route", route)
	if err != nil {
		t.Fatalf("add route failed: %v", err)
	}

	// Verify
	retrieved, ok := rc.GetRoute("test-route")
	if !ok {
		t.Fatal("route not found after add")
	}
	if retrieved.Strategy != "fallback" {
		t.Errorf("expected strategy 'fallback', got %s", retrieved.Strategy)
	}

	// Update route
	route.Strategy = "round-robin"
	err = rc.UpdateRoute("test-route", route)
	if err != nil {
		t.Fatalf("update route failed: %v", err)
	}

	retrieved, _ = rc.GetRoute("test-route")
	if retrieved.Strategy != "round-robin" {
		t.Errorf("expected strategy 'round-robin', got %s", retrieved.Strategy)
	}

	// Delete route
	err = rc.DeleteRoute("test-route")
	if err != nil {
		t.Fatalf("delete route failed: %v", err)
	}

	_, ok = rc.GetRoute("test-route")
	if ok {
		t.Error("route still exists after delete")
	}
}

func TestRuntimeConfig_ModelAliasOperations(t *testing.T) {
	cfg := Config{
		Server: ServerConfig{
			Host:                  "localhost",
			Port:                  8080,
			APIKey:                "test-key",
			RequestTimeoutSeconds: 60,
		},
		Providers: []ProviderConfig{
			{Name: "test-provider", Type: "openai_compat", BaseURL: "https://api.test.com", APIKey: "test-key", Enabled: true},
		},
		Routes:       map[string]RouteConfig{},
		ModelAliases: map[string]ModelAlias{},
		Logging:      LoggingConfig{Level: "info"},
		Storage:      StorageConfig{SQLitePath: "./data/test.db"},
		Retry: RetryConfig{
			MaxAttempts:      3,
			InitialBackoffMs: 100,
			MaxBackoffMs:     2000,
		},
	}

	rc := NewRuntimeConfig(cfg, "")

	// Add alias
	alias := ModelAlias{
		Provider: "test-provider",
		Model:    "gpt-4",
	}

	err := rc.AddModelAlias("gpt4", alias)
	if err != nil {
		t.Fatalf("add model alias failed: %v", err)
	}

	// Verify
	retrieved, ok := rc.GetModelAlias("gpt4")
	if !ok {
		t.Fatal("model alias not found after add")
	}
	if retrieved.Model != "gpt-4" {
		t.Errorf("expected model 'gpt-4', got %s", retrieved.Model)
	}

	// Update alias
	alias.Model = "gpt-4-turbo"
	err = rc.UpdateModelAlias("gpt4", alias)
	if err != nil {
		t.Fatalf("update model alias failed: %v", err)
	}

	retrieved, _ = rc.GetModelAlias("gpt4")
	if retrieved.Model != "gpt-4-turbo" {
		t.Errorf("expected model 'gpt-4-turbo', got %s", retrieved.Model)
	}

	// Delete alias
	err = rc.DeleteModelAlias("gpt4")
	if err != nil {
		t.Fatalf("delete model alias failed: %v", err)
	}

	_, ok = rc.GetModelAlias("gpt4")
	if ok {
		t.Error("model alias still exists after delete")
	}
}

func TestRuntimeConfig_Persist(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test-config.yaml")

	cfg := Config{
		Server: ServerConfig{
			Host:                  "localhost",
			Port:                  8080,
			APIKey:                "test-key",
			RequestTimeoutSeconds: 60,
		},
		Providers: []ProviderConfig{
			{Name: "test-provider", Type: "openai_compat", BaseURL: "https://api.test.com", APIKey: "test-key", Enabled: true},
		},
		Routes:       map[string]RouteConfig{},
		ModelAliases: map[string]ModelAlias{},
		Logging:      LoggingConfig{Level: "info"},
		Storage:      StorageConfig{SQLitePath: "./data/test.db"},
		Retry: RetryConfig{
			MaxAttempts:      3,
			InitialBackoffMs: 100,
			MaxBackoffMs:     2000,
		},
	}

	rc := NewRuntimeConfig(cfg, configPath)

	// Persist
	err := rc.Persist()
	if err != nil {
		t.Fatalf("persist failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("config file not created")
	}

	// Load and verify
	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("load persisted config failed: %v", err)
	}

	if loaded.Server.Port != 8080 {
		t.Errorf("expected port 8080, got %d", loaded.Server.Port)
	}
	if len(loaded.Providers) != 1 {
		t.Errorf("expected 1 provider, got %d", len(loaded.Providers))
	}
}

func TestRuntimeConfig_UpdateAndPersist(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test-config.yaml")

	cfg := Config{
		Server: ServerConfig{
			Host:                  "localhost",
			Port:                  8080,
			APIKey:                "test-key",
			RequestTimeoutSeconds: 60,
		},
		Providers: []ProviderConfig{
			{Name: "test-provider", Type: "openai_compat", BaseURL: "https://api.test.com", APIKey: "test-key", Enabled: true},
		},
		Routes:       map[string]RouteConfig{},
		ModelAliases: map[string]ModelAlias{},
		Logging:      LoggingConfig{Level: "info"},
		Storage:      StorageConfig{SQLitePath: "./data/test.db"},
		Retry: RetryConfig{
			MaxAttempts:      3,
			InitialBackoffMs: 100,
			MaxBackoffMs:     2000,
		},
	}

	rc := NewRuntimeConfig(cfg, configPath)

	// Update and persist
	err := rc.UpdateAndPersist(func(c *Config) error {
		c.Server.Port = 9090
		return nil
	})
	if err != nil {
		t.Fatalf("update and persist failed: %v", err)
	}

	// Load and verify
	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("load persisted config failed: %v", err)
	}

	if loaded.Server.Port != 9090 {
		t.Errorf("expected port 9090, got %d", loaded.Server.Port)
	}
}

func TestRuntimeConfig_ValidationOnUpdate(t *testing.T) {
	cfg := Config{
		Server: ServerConfig{
			Host:   "localhost",
			Port:   8080,
			APIKey: "test-key",
		},
		Providers: []ProviderConfig{
			{Name: "test-provider", Type: "openai_compat", BaseURL: "https://api.test.com", Enabled: true},
		},
		Routes:       map[string]RouteConfig{},
		ModelAliases: map[string]ModelAlias{},
		Logging:      LoggingConfig{Level: "info"},
		Storage:      StorageConfig{SQLitePath: "./data/test.db"},
		Retry: RetryConfig{
			MaxAttempts:      3,
			InitialBackoffMs: 100,
			MaxBackoffMs:     2000,
		},
	}

	rc := NewRuntimeConfig(cfg, "")

	// Try invalid update (empty API key)
	err := rc.Update(func(c *Config) error {
		c.Server.APIKey = ""
		return nil
	})
	if err == nil {
		t.Error("expected validation error for empty API key")
	}

	// Verify config unchanged
	retrieved := rc.Get()
	if retrieved.Server.APIKey != "test-key" {
		t.Error("config was modified despite validation failure")
	}
}
