package config

import (
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server       ServerConfig           `yaml:"server"`
	Logging      LoggingConfig          `yaml:"logging"`
	Storage      StorageConfig          `yaml:"storage"`
	Retry        RetryConfig            `yaml:"retry"`
	Providers    []ProviderConfig       `yaml:"providers"`
	Routes       map[string]RouteConfig `yaml:"routes"`
	Errors       ErrorConfig            `yaml:"errors"`
	Settings     SettingsConfig         `yaml:"settings"`
	ModelAliases map[string]ModelAlias  `yaml:"model_aliases"`
}

type ServerConfig struct {
	Host                     string `yaml:"host"`
	Port                     int    `yaml:"port"`
	APIKey                   string `yaml:"api_key"`
	RequestTimeoutSeconds    int    `yaml:"request_timeout_seconds"`
	ReadTimeoutSeconds       int    `yaml:"read_timeout_seconds"`
	WriteTimeoutSeconds      int    `yaml:"write_timeout_seconds"`
	IdleTimeoutSeconds       int    `yaml:"idle_timeout_seconds"`
	ReadHeaderTimeoutSeconds int    `yaml:"read_header_timeout_seconds"`
	MaxHeaderBytes           int    `yaml:"max_header_bytes"`
}

type LoggingConfig struct {
	Level string `yaml:"level"`
	Debug bool   `yaml:"debug"`
}

type StorageConfig struct {
	SQLitePath string `yaml:"sqlite_path"`
}

type RetryConfig struct {
	MaxAttempts      int `yaml:"max_attempts"`
	InitialBackoffMs int `yaml:"initial_backoff_ms"`
	MaxBackoffMs     int `yaml:"max_backoff_ms"`
	MaxCooldownMs    int `yaml:"max_cooldown_ms"`
}

type ProviderConfig struct {
	Name     string            `yaml:"name"`
	Type     string            `yaml:"type"`
	Format   string            `yaml:"format,omitempty"`
	BaseURL  string            `yaml:"base_url"`
	APIKey   string            `yaml:"api_key,omitempty"` // Deprecated: use accounts instead
	Accounts []AccountConfig   `yaml:"accounts,omitempty"`
	Tier     string            `yaml:"tier,omitempty"`
	Headers  map[string]string `yaml:"headers,omitempty"`
	Enabled  bool              `yaml:"enabled"`
}

type AccountConfig struct {
	Name   string `yaml:"name"`
	APIKey string `yaml:"api_key"`
}

type RouteConfig struct {
	Strategy string        `yaml:"strategy,omitempty"`
	Targets  []RouteTarget `yaml:"targets"`
}

type RouteTarget struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
	Tier     string `yaml:"tier,omitempty"`
}

type ErrorConfig struct {
	TextRules   []ErrorTextRule   `yaml:"text_rules"`
	StatusRules []ErrorStatusRule `yaml:"status_rules"`
}

type ErrorTextRule struct {
	Text       string `yaml:"text"`
	Backoff    bool   `yaml:"backoff,omitempty"`
	CooldownMs int    `yaml:"cooldown_ms,omitempty"`
}

type ErrorStatusRule struct {
	Status     int  `yaml:"status"`
	Backoff    bool `yaml:"backoff,omitempty"`
	CooldownMs int  `yaml:"cooldown_ms,omitempty"`
}

type SettingsConfig struct {
	ComboStrategy        string `yaml:"combo_strategy,omitempty"`
	OutboundProxyEnabled bool   `yaml:"outbound_proxy_enabled,omitempty"`
	OutboundProxyURL     string `yaml:"outbound_proxy_url,omitempty"`
}

type ModelAlias struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
}

var envPattern = regexp.MustCompile(`\$\{([A-Z0-9_]+)\}`)

func Load(path string) (Config, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	expanded := envPattern.ReplaceAllStringFunc(string(content), func(match string) string {
		parts := envPattern.FindStringSubmatch(match)
		if len(parts) != 2 {
			return match
		}
		return os.Getenv(parts[1])
	})

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}

	applyDefaults(&cfg)
	if err := validate(cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.Server.Host == "" {
		cfg.Server.Host = "127.0.0.1"
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 20128
	}
	if cfg.Server.RequestTimeoutSeconds == 0 {
		cfg.Server.RequestTimeoutSeconds = 60
	}
	if cfg.Server.ReadTimeoutSeconds == 0 {
		cfg.Server.ReadTimeoutSeconds = 30
	}
	if cfg.Server.WriteTimeoutSeconds == 0 {
		cfg.Server.WriteTimeoutSeconds = 30
	}
	if cfg.Server.IdleTimeoutSeconds == 0 {
		cfg.Server.IdleTimeoutSeconds = 120
	}
	if cfg.Server.ReadHeaderTimeoutSeconds == 0 {
		cfg.Server.ReadHeaderTimeoutSeconds = 10
	}
	if cfg.Server.MaxHeaderBytes == 0 {
		cfg.Server.MaxHeaderBytes = 1048576 // 1MB
	}
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "info"
	}
	if cfg.Storage.SQLitePath == "" {
		cfg.Storage.SQLitePath = "./data/9router.db"
	}
	if cfg.Retry.MaxAttempts == 0 {
		cfg.Retry.MaxAttempts = 3
	}
	if cfg.Retry.InitialBackoffMs == 0 {
		cfg.Retry.InitialBackoffMs = 100
	}
	if cfg.Retry.MaxBackoffMs == 0 {
		cfg.Retry.MaxBackoffMs = 2000
	}
	if cfg.Routes == nil {
		cfg.Routes = map[string]RouteConfig{}
	}
	if cfg.ModelAliases == nil {
		cfg.ModelAliases = map[string]ModelAlias{}
	}
	if cfg.Settings.ComboStrategy == "" {
		cfg.Settings.ComboStrategy = "fallback"
	}
}

func validate(cfg Config) error {
	// Server validation
	if cfg.Server.APIKey == "" {
		return fmt.Errorf("server.api_key is required")
	}
	if cfg.Server.Port < 1 || cfg.Server.Port > 65535 {
		return fmt.Errorf("server.port must be between 1 and 65535")
	}
	if cfg.Server.RequestTimeoutSeconds < 1 || cfg.Server.RequestTimeoutSeconds > 300 {
		return fmt.Errorf("server.request_timeout_seconds must be between 1 and 300")
	}

	// Logging validation
	validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLevels[cfg.Logging.Level] {
		return fmt.Errorf("logging.level must be one of: debug, info, warn, error")
	}

	// Storage validation
	if cfg.Storage.SQLitePath == "" {
		return fmt.Errorf("storage.sqlite_path is required")
	}

	// Retry validation
	if cfg.Retry.MaxAttempts < 1 || cfg.Retry.MaxAttempts > 10 {
		return fmt.Errorf("retry.max_attempts must be between 1 and 10")
	}
	if cfg.Retry.InitialBackoffMs < 10 || cfg.Retry.InitialBackoffMs > 5000 {
		return fmt.Errorf("retry.initial_backoff_ms must be between 10 and 5000")
	}
	if cfg.Retry.MaxBackoffMs < cfg.Retry.InitialBackoffMs {
		return fmt.Errorf("retry.max_backoff_ms must be >= initial_backoff_ms")
	}

	// Provider validation
	if len(cfg.Providers) == 0 {
		return fmt.Errorf("at least one provider is required")
	}

	providerNames := make(map[string]bool)
	enabledCount := 0
	for i, provider := range cfg.Providers {
		if provider.Name == "" {
			return fmt.Errorf("provider[%d].name is required", i)
		}
		if providerNames[provider.Name] {
			return fmt.Errorf("duplicate provider name: %s", provider.Name)
		}
		providerNames[provider.Name] = true

		if provider.Type == "" {
			return fmt.Errorf("provider[%d].type is required", i)
		}
		validTypes := map[string]bool{"openai_compat": true, "openrouter": true, "anthropic": true, "anthropic_compat": true}
		if !validTypes[provider.Type] {
			return fmt.Errorf("provider[%d].type must be one of: openai_compat, openrouter, anthropic, anthropic_compat", i)
		}

		if provider.Enabled {
			enabledCount++
			if provider.BaseURL == "" {
				return fmt.Errorf("provider[%d].base_url is required when enabled", i)
			}

			// Validate either APIKey (deprecated) or Accounts (new)
			if provider.APIKey == "" && len(provider.Accounts) == 0 {
				return fmt.Errorf("provider[%d].api_key or accounts is required when enabled (use ${ENV_VAR} for secrets)", i)
			}

			// Validate accounts if present
			for j, account := range provider.Accounts {
				if account.Name == "" {
					return fmt.Errorf("provider[%d].accounts[%d].name is required", i, j)
				}
				if account.APIKey == "" {
					return fmt.Errorf("provider[%d].accounts[%d].api_key is required (use ${ENV_VAR} for secrets)", i, j)
				}
			}

			// Check for duplicate account names
			accountNames := make(map[string]bool)
			for _, account := range provider.Accounts {
				if accountNames[account.Name] {
					return fmt.Errorf("provider[%d].accounts has duplicate account name: %s", i, account.Name)
				}
				accountNames[account.Name] = true
			}
		}
	}

	if enabledCount == 0 {
		return fmt.Errorf("at least one provider must be enabled")
	}

	// Route validation
	for alias, routeConfig := range cfg.Routes {
		if len(routeConfig.Targets) == 0 {
			return fmt.Errorf("route '%s' has no targets", alias)
		}
		if routeConfig.Strategy != "" {
			validStrategies := map[string]bool{"fallback": true, "round-robin": true}
			if !validStrategies[routeConfig.Strategy] {
				return fmt.Errorf("route '%s' strategy must be one of: fallback, round-robin", alias)
			}
		}
		for i, target := range routeConfig.Targets {
			if target.Provider == "" {
				return fmt.Errorf("route '%s' target[%d].provider is required", alias, i)
			}
			if !providerNames[target.Provider] {
				return fmt.Errorf("route '%s' target[%d] references unknown provider: %s", alias, i, target.Provider)
			}
			if target.Model == "" {
				return fmt.Errorf("route '%s' target[%d].model is required", alias, i)
			}
			if target.Tier != "" {
				validTiers := map[string]bool{"primary": true, "secondary": true, "tertiary": true}
				if !validTiers[target.Tier] {
					return fmt.Errorf("route '%s' target[%d].tier must be one of: primary, secondary, tertiary", alias, i)
				}
			}
		}
	}

	return nil
}
