package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/edodoyokz/ai-go-router/internal/config"
	"gopkg.in/yaml.v3"
)

func runInit(configPath string, force bool) error {
	userConfigPath, err := defaultUserConfigPath()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}
	if configPath == "./config/config.example.yaml" {
		configPath = userConfigPath
	}

	dbPath, err := defaultDataPath()
	if err != nil {
		return fmt.Errorf("resolve data directory: %w", err)
	}

	if _, err := os.Stat(configPath); err == nil && !force {
		cfg, loadErr := config.Load(configPath)
		if loadErr != nil {
			return fmt.Errorf("existing config is invalid; use --force to replace it: %w", loadErr)
		}
		printInitSummary(configPath, cfg.Storage.SQLitePath, cfg.Server.Host, cfg.Server.Port, firstConfiguredKey(cfg), false)
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("stat config: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}

	apiKey := "admin"

	cfg := config.Config{
		Server: config.ServerConfig{
			Host:                     "127.0.0.1",
			Port:                     1988,
			APIKey:                   apiKey,
			RequestTimeoutSeconds:    60,
			ReadTimeoutSeconds:       30,
			WriteTimeoutSeconds:      30,
			IdleTimeoutSeconds:       120,
			ReadHeaderTimeoutSeconds: 10,
			MaxHeaderBytes:           1048576,
			CORS: config.CORSConfig{
				AllowedOrigins:   []string{},
				AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
				AllowedHeaders:   []string{"Authorization", "Content-Type"},
				AllowCredentials: false,
				MaxAgeSeconds:    86400,
			},
		},
		Logging: config.LoggingConfig{Level: "info", JSONMode: false},
		Storage: config.StorageConfig{SQLitePath: dbPath},
		Retry: config.RetryConfig{
			MaxAttempts:      3,
			InitialBackoffMs: 100,
			MaxBackoffMs:     2000,
			MaxCooldownMs:    300000,
			CircuitBreaker: config.CircuitBreakerConfig{
				FailureThreshold: 5,
				OpenTimeoutMs:    300000,
				SuccessThreshold: 2,
			},
		},
		Errors: config.ErrorConfig{
			TextRules: []config.ErrorTextRule{
				{Text: "rate limit", Backoff: true},
				{Text: "too many requests", Backoff: true},
				{Text: "quota exceeded", Backoff: true},
				{Text: "no credentials", CooldownMs: 120000},
			},
			StatusRules: []config.ErrorStatusRule{
				{Status: 429, Backoff: true},
				{Status: 503, Backoff: true},
				{Status: 401, CooldownMs: 120000},
			},
		},
		Settings:     config.SettingsConfig{ComboStrategy: "fallback"},
		Providers:    []config.ProviderConfig{},
		Routes:       map[string]config.RouteConfig{},
		ModelAliases: map[string]config.ModelAlias{},
		CustomModels: map[string]config.CustomModel{},
	}

	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	printInitSummary(configPath, dbPath, cfg.Server.Host, cfg.Server.Port, apiKey, true)
	return nil
}

func defaultDataPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local/share/router/router.db"), nil
}

func firstConfiguredKey(cfg config.Config) string {
	if cfg.Server.APIKey != "" {
		return cfg.Server.APIKey
	}
	if len(cfg.Server.AdminAPIKeys) > 0 {
		return cfg.Server.AdminAPIKeys[0]
	}
	return ""
}

func printInitSummary(configPath, dbPath, host string, port int, apiKey string, created bool) {
	status := "created"
	if !created {
		status = "exists"
	}
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	fmt.Println("router init")
	fmt.Printf("  status      : %s\n", status)
	fmt.Printf("  config      : %s\n", configPath)
	fmt.Printf("  database    : %s\n", dbPath)
	fmt.Printf("  UI          : http://%s:%d\n", host, port)
	fmt.Printf("  admin key   : %s\n", maskKey(apiKey))
	fmt.Println()
	fmt.Println("Start the server with:")
	fmt.Println("  router serve")
	fmt.Println()
	fmt.Println("Default Web UI password: admin")
}
