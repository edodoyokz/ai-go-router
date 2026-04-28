package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/edodoyokz/ai-go-router/internal/providers/catalog"
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
	CustomModels map[string]CustomModel `yaml:"custom_models,omitempty"`
	Tunnel       TunnelConfig           `yaml:"tunnel,omitempty"`
	MITM         MITMConfig             `yaml:"mitm,omitempty"`
	Policies     []PolicyRule           `yaml:"policies,omitempty"`
	Sync         SyncConfig             `yaml:"sync,omitempty"`
	Nodes        []NodeConfig           `yaml:"nodes,omitempty"`
}

// Clone creates a deep copy of the Config struct
func (c Config) Clone() Config {
	// Deep copy slices and maps
	providers := make([]ProviderConfig, len(c.Providers))
	copy(providers, c.Providers)

	// Deep copy Providers (each has Accounts slice and Headers map)
	for i := range providers {
		providers[i].Accounts = make([]AccountConfig, len(c.Providers[i].Accounts))
		copy(providers[i].Accounts, c.Providers[i].Accounts)
		if c.Providers[i].Headers != nil {
			providers[i].Headers = make(map[string]string, len(c.Providers[i].Headers))
			for k, v := range c.Providers[i].Headers {
				providers[i].Headers[k] = v
			}
		}
	}

	routes := make(map[string]RouteConfig, len(c.Routes))
	for k, v := range c.Routes {
		routes[k] = RouteConfig{
			Strategy: v.Strategy,
			Targets:  make([]RouteTarget, len(v.Targets)),
		}
		copy(routes[k].Targets, v.Targets)
	}

	modelAliases := make(map[string]ModelAlias, len(c.ModelAliases))
	for k, v := range c.ModelAliases {
		modelAliases[k] = v
	}

	customModels := make(map[string]CustomModel, len(c.CustomModels))
	for k, v := range c.CustomModels {
		customModels[k] = v
	}

	policies := make([]PolicyRule, len(c.Policies))
	copy(policies, c.Policies)

	nodes := make([]NodeConfig, len(c.Nodes))
	copy(nodes, c.Nodes)

	// Deep copy ServerConfig (AdminAPIKeys slice, CORS slices)
	server := c.Server
	server.AdminAPIKeys = make([]string, len(c.Server.AdminAPIKeys))
	copy(server.AdminAPIKeys, c.Server.AdminAPIKeys)
	server.CORS = CORSConfig{
		AllowedOrigins:   make([]string, len(c.Server.CORS.AllowedOrigins)),
		AllowedMethods:   make([]string, len(c.Server.CORS.AllowedMethods)),
		AllowedHeaders:   make([]string, len(c.Server.CORS.AllowedHeaders)),
		AllowCredentials: c.Server.CORS.AllowCredentials,
		MaxAgeSeconds:    c.Server.CORS.MaxAgeSeconds,
	}
	copy(server.CORS.AllowedOrigins, c.Server.CORS.AllowedOrigins)
	copy(server.CORS.AllowedMethods, c.Server.CORS.AllowedMethods)
	copy(server.CORS.AllowedHeaders, c.Server.CORS.AllowedHeaders)

	// Deep copy ErrorConfig (slices)
	errors := c.Errors
	errors.TextRules = make([]ErrorTextRule, len(c.Errors.TextRules))
	copy(errors.TextRules, c.Errors.TextRules)
	errors.StatusRules = make([]ErrorStatusRule, len(c.Errors.StatusRules))
	copy(errors.StatusRules, c.Errors.StatusRules)

	return Config{
		Server:       server,
		Logging:      c.Logging,
		Storage:      c.Storage,
		Retry:        c.Retry,
		Providers:    providers,
		Routes:       routes,
		Errors:       errors,
		Settings:     c.Settings,
		ModelAliases: modelAliases,
		CustomModels: customModels,
		Tunnel:       c.Tunnel,
		MITM:         c.MITM,
		Policies:     policies,
		Sync:         c.Sync,
		Nodes:        nodes,
	}
}

type ServerConfig struct {
	Host                     string          `yaml:"host" json:"host"`
	Port                     int             `yaml:"port" json:"port"`
	APIKey                   string          `yaml:"api_key" json:"api_key"`
	AdminAPIKeys             []string        `yaml:"admin_api_keys,omitempty" json:"admin_api_keys,omitempty"`
	RequestTimeoutSeconds    int             `yaml:"request_timeout_seconds" json:"request_timeout_seconds"`
	ReadTimeoutSeconds       int             `yaml:"read_timeout_seconds" json:"read_timeout_seconds"`
	WriteTimeoutSeconds      int             `yaml:"write_timeout_seconds" json:"write_timeout_seconds"`
	IdleTimeoutSeconds       int             `yaml:"idle_timeout_seconds" json:"idle_timeout_seconds"`
	ReadHeaderTimeoutSeconds int             `yaml:"read_header_timeout_seconds" json:"read_header_timeout_seconds"`
	MaxHeaderBytes           int             `yaml:"max_header_bytes" json:"max_header_bytes"`
	CORS                     CORSConfig      `yaml:"cors" json:"cors"`
	RateLimit                RateLimitConfig `yaml:"rate_limit,omitempty" json:"rate_limit,omitempty"`
}

type RateLimitConfig struct {
	Enabled           bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	RequestsPerMinute int  `yaml:"requests_per_minute,omitempty" json:"requests_per_minute,omitempty"`
}

type CORSConfig struct {
	AllowedOrigins   []string `yaml:"allowed_origins"`
	AllowedMethods   []string `yaml:"allowed_methods"`
	AllowedHeaders   []string `yaml:"allowed_headers"`
	AllowCredentials bool     `yaml:"allow_credentials"`
	MaxAgeSeconds    int      `yaml:"max_age_seconds"`
}

type LoggingConfig struct {
	Level    string            `yaml:"level"`
	Debug    bool              `yaml:"debug"`
	JSONMode bool              `yaml:"json_mode"`
	Rotation LogRotationConfig `yaml:"rotation,omitempty"`
}

type LogRotationConfig struct {
	Enabled    bool   `yaml:"enabled,omitempty"`
	MaxSizeMB  int    `yaml:"max_size_mb,omitempty"`
	MaxBackups int    `yaml:"max_backups,omitempty"`
	MaxAgeDays int    `yaml:"max_age_days,omitempty"`
	FilePath   string `yaml:"file_path,omitempty"`
}

type StorageConfig struct {
	SQLitePath string `yaml:"sqlite_path"`
}

type RetryConfig struct {
	MaxAttempts      int                  `yaml:"max_attempts"`
	InitialBackoffMs int                  `yaml:"initial_backoff_ms"`
	MaxBackoffMs     int                  `yaml:"max_backoff_ms"`
	MaxCooldownMs    int                  `yaml:"max_cooldown_ms"`
	CircuitBreaker   CircuitBreakerConfig `yaml:"circuit_breaker,omitempty"`
}

type CircuitBreakerConfig struct {
	FailureThreshold int `yaml:"failure_threshold,omitempty"`
	OpenTimeoutMs    int `yaml:"open_timeout_ms,omitempty"`
	SuccessThreshold int `yaml:"success_threshold,omitempty"`
}

type ProviderConfig struct {
	Name                 string            `yaml:"name" json:"name"`
	ProviderID           string            `yaml:"provider_id,omitempty" json:"provider_id,omitempty"`
	Type                 string            `yaml:"type" json:"type"`
	Format               string            `yaml:"format,omitempty" json:"format,omitempty"`
	BaseURL              string            `yaml:"base_url" json:"base_url"`
	APIKey               string            `yaml:"api_key,omitempty" json:"api_key,omitempty"` // Deprecated: use accounts instead
	AuthType             string            `yaml:"auth_type,omitempty" json:"auth_type,omitempty"`
	Accounts             []AccountConfig   `yaml:"accounts,omitempty" json:"accounts,omitempty"`
	Tier                 string            `yaml:"tier,omitempty" json:"tier,omitempty"`
	Headers              map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	Enabled              bool              `yaml:"enabled" json:"enabled"`
	ProxyURLs            []string          `yaml:"proxy_urls,omitempty" json:"proxy_urls,omitempty"`         // Pool of proxy URLs; round-robined per request
	GCPProjectID         string            `yaml:"gcp_project_id,omitempty" json:"gcp_project_id,omitempty"` // Real GCP project ID for Vertex AI
	ProviderSpecificData map[string]any    `yaml:"provider_specific_data,omitempty" json:"provider_specific_data,omitempty"`
}

type AccountConfig struct {
	ID                   string         `yaml:"id,omitempty" json:"id,omitempty"`
	Name                 string         `yaml:"name" json:"name"`
	AuthType             string         `yaml:"auth_type,omitempty" json:"auth_type,omitempty"`
	APIKey               string         `yaml:"api_key,omitempty" json:"api_key,omitempty"`
	AccessToken          string         `yaml:"access_token,omitempty" json:"access_token,omitempty"`
	RefreshToken         string         `yaml:"refresh_token,omitempty" json:"refresh_token,omitempty"`
	IDToken              string         `yaml:"id_token,omitempty" json:"id_token,omitempty"`
	ExpiresAt            *time.Time     `yaml:"expires_at,omitempty" json:"expires_at,omitempty"`
	Cookie               string         `yaml:"cookie,omitempty" json:"cookie,omitempty"`
	ProjectID            string         `yaml:"project_id,omitempty" json:"project_id,omitempty"`
	ProviderSpecificData map[string]any `yaml:"provider_specific_data,omitempty" json:"provider_specific_data,omitempty"`
	Enabled              bool           `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Priority             int            `yaml:"priority,omitempty" json:"priority,omitempty"`
	DefaultModel         string         `yaml:"default_model,omitempty" json:"default_model,omitempty"`
	ProxyURL             string         `yaml:"proxy_url,omitempty" json:"proxy_url,omitempty"`
}

type RouteConfig struct {
	Strategy string        `yaml:"strategy,omitempty" json:"strategy,omitempty"`
	Targets  []RouteTarget `yaml:"targets" json:"targets"`
}

type RouteTarget struct {
	Provider string `yaml:"provider" json:"provider"`
	Model    string `yaml:"model" json:"model"`
	Tier     string `yaml:"tier,omitempty" json:"tier,omitempty"`
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
	ComboStrategy        string         `yaml:"combo_strategy,omitempty" json:"combo_strategy,omitempty"`
	OutboundProxyEnabled bool           `yaml:"outbound_proxy_enabled,omitempty" json:"outbound_proxy_enabled,omitempty"`
	OutboundProxyURL     string         `yaml:"outbound_proxy_url,omitempty" json:"outbound_proxy_url,omitempty"`
	Thinking             ThinkingConfig `yaml:"thinking,omitempty" json:"thinking,omitempty"`
	NativePassthrough    bool           `yaml:"native_passthrough,omitempty" json:"native_passthrough,omitempty"`
	Locale               string         `yaml:"locale,omitempty" json:"locale,omitempty"` // e.g. "en", "id", "zh", "ja"
}

type ThinkingConfig struct {
	Enabled          bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	MaxTokens        int  `yaml:"max_tokens,omitempty" json:"max_tokens,omitempty"`
	IncludeReasoning bool `yaml:"include_reasoning,omitempty" json:"include_reasoning,omitempty"`
}

// PolicyRule defines a request routing policy rule (mirrors policy.Policy).
type PolicyRule struct {
	Name          string `yaml:"name" json:"name"`
	MatchModel    string `yaml:"match_model,omitempty" json:"match_model,omitempty"`
	MatchProvider string `yaml:"match_provider,omitempty" json:"match_provider,omitempty"`
	MatchAPIKey   string `yaml:"match_api_key,omitempty" json:"match_api_key,omitempty"`
	Action        string `yaml:"action" json:"action"`
	RerouteModel  string `yaml:"reroute_model,omitempty" json:"reroute_model,omitempty"`
	DenyMessage   string `yaml:"deny_message,omitempty" json:"deny_message,omitempty"`
	Tag           string `yaml:"tag,omitempty" json:"tag,omitempty"`
}

// NodeConfig describes a remote router peer node.
type NodeConfig struct {
	Name    string `yaml:"name" json:"name"`
	BaseURL string `yaml:"base_url" json:"base_url"`
	APIKey  string `yaml:"api_key,omitempty" json:"api_key,omitempty"`
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Weight  int    `yaml:"weight,omitempty" json:"weight,omitempty"`
}

// SyncConfig configures cloud backup/restore.
type SyncConfig struct {
	Enabled         bool   `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Provider        string `yaml:"provider,omitempty" json:"provider,omitempty"`
	Endpoint        string `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`
	Bucket          string `yaml:"bucket,omitempty" json:"bucket,omitempty"`
	Prefix          string `yaml:"prefix,omitempty" json:"prefix,omitempty"`
	AccessKey       string `yaml:"access_key,omitempty" json:"access_key,omitempty"`
	SecretKey       string `yaml:"secret_key,omitempty" json:"secret_key,omitempty"`
	IntervalMinutes int    `yaml:"interval_minutes,omitempty" json:"interval_minutes,omitempty"`
}

// MITMConfig configures the built-in MITM proxy server.
type MITMConfig struct {
	Enabled     bool   `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	ListenAddr  string `yaml:"listen_addr,omitempty" json:"listen_addr,omitempty"`
	UpstreamURL string `yaml:"upstream_url,omitempty" json:"upstream_url,omitempty"`
	TLSCert     string `yaml:"tls_cert,omitempty" json:"tls_cert,omitempty"`
	TLSKey      string `yaml:"tls_key,omitempty" json:"tls_key,omitempty"`
	APIKey      string `yaml:"api_key,omitempty" json:"api_key,omitempty"`
	Cloaking    string `yaml:"cloaking,omitempty" json:"cloaking,omitempty"` // "", "claude", or "antigravity"
}

// TunnelConfig configures exposing the router to the internet via Cloudflare or Tailscale.
type TunnelConfig struct {
	Enabled   bool   `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Provider  string `yaml:"provider,omitempty" json:"provider,omitempty"` // "cloudflare" or "tailscale"
	Hostname  string `yaml:"hostname,omitempty" json:"hostname,omitempty"` // e.g. "router.example.com"
	AuthToken string `yaml:"auth_token,omitempty" json:"auth_token,omitempty"`
}

type ModelAlias struct {
	Provider string `yaml:"provider" json:"provider"`
	Model    string `yaml:"model" json:"model"`
}

type CustomModel struct {
	Provider    string `yaml:"provider" json:"provider"`
	Model       string `yaml:"model" json:"model"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
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
		cfg.Server.Port = 1988
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
		cfg.Storage.SQLitePath = "./data/router.db"
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
	if cfg.Retry.CircuitBreaker.FailureThreshold == 0 {
		cfg.Retry.CircuitBreaker.FailureThreshold = 5
	}
	if cfg.Retry.CircuitBreaker.OpenTimeoutMs == 0 {
		cfg.Retry.CircuitBreaker.OpenTimeoutMs = 300000 // 5 minutes
	}
	if cfg.Retry.CircuitBreaker.SuccessThreshold == 0 {
		cfg.Retry.CircuitBreaker.SuccessThreshold = 2
	}
	if cfg.Routes == nil {
		cfg.Routes = map[string]RouteConfig{}
	}
	if cfg.ModelAliases == nil {
		cfg.ModelAliases = map[string]ModelAlias{}
	}
	if cfg.CustomModels == nil {
		cfg.CustomModels = map[string]CustomModel{}
	}
	if cfg.Settings.ComboStrategy == "" {
		cfg.Settings.ComboStrategy = "fallback"
	}
}

func validate(cfg Config) error {
	// Server validation
	if cfg.Server.APIKey == "" && len(cfg.Server.AdminAPIKeys) == 0 {
		return fmt.Errorf("server.api_key or server.admin_api_keys is required")
	}
	if cfg.Server.Port < 1 || cfg.Server.Port > 65535 {
		return fmt.Errorf("server.port must be between 1 and 65535")
	}
	if cfg.Server.RequestTimeoutSeconds < 1 || cfg.Server.RequestTimeoutSeconds > 300 {
		return fmt.Errorf("server.request_timeout_seconds must be between 1 and 300")
	}
	adminKeys := make(map[string]bool)
	for i, key := range cfg.Server.AdminAPIKeys {
		if key == "" {
			return fmt.Errorf("server.admin_api_keys[%d] cannot be empty", i)
		}
		if adminKeys[key] {
			return fmt.Errorf("server.admin_api_keys contains duplicate key")
		}
		if cfg.Server.APIKey != "" && key == cfg.Server.APIKey {
			return fmt.Errorf("server.admin_api_keys contains duplicate of server.api_key")
		}
		adminKeys[key] = true
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
	providerNames := make(map[string]bool)
	for i, provider := range cfg.Providers {
		if provider.Name == "" {
			return fmt.Errorf("provider[%d].name is required", i)
		}
		if providerNames[provider.Name] {
			return fmt.Errorf("duplicate provider name: %s", provider.Name)
		}
		providerNames[provider.Name] = true

		if provider.ProviderID == "" {
			cfg.Providers[i].ProviderID = catalog.InferProviderID(provider.Type, provider.Name)
		}
		if provider.AuthType == "" {
			if provider.APIKey != "" {
				cfg.Providers[i].AuthType = "api_key"
			}
		}

		if provider.Type == "" {
			return fmt.Errorf("provider[%d].type is required", i)
		}
		if def, ok := catalog.ResolveAlias(provider.Type); ok {
			if cfg.Providers[i].ProviderID == "" {
				cfg.Providers[i].ProviderID = def.ID
			}
			if cfg.Providers[i].BaseURL == "" && def.DefaultBaseURL != "" {
				cfg.Providers[i].BaseURL = def.DefaultBaseURL
			}
		} else {
			return fmt.Errorf("provider[%d].type must be a known catalog provider or alias", i)
		}

		if provider.Enabled {
			if cfg.Providers[i].BaseURL == "" {
				return fmt.Errorf("provider[%d].base_url is required when enabled", i)
			}

			// Validate either APIKey (deprecated) or Accounts (new)
			if provider.APIKey == "" && len(provider.Accounts) == 0 {
				return fmt.Errorf("provider[%d].api_key or accounts is required when enabled (use ${ENV_VAR} for secrets)", i)
			}

			// Validate accounts if present
			for j, account := range provider.Accounts {
				authType := strings.TrimSpace(account.AuthType)
				if authType == "" {
					if account.APIKey != "" {
						authType = "api_key"
						cfg.Providers[i].Accounts[j].AuthType = authType
					}
				}
				if account.Name == "" {
					return fmt.Errorf("provider[%d].accounts[%d].name is required", i, j)
				}
				if account.APIKey == "" && account.AccessToken == "" && account.Cookie == "" && authType != "no_auth" {
					return fmt.Errorf("provider[%d].accounts[%d] requires credential for auth_type %q", i, j, authType)
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

	// Allow zero enabled providers during initial onboarding.
	// The app can start in setup mode, while /readyz reports not ready
	// until the user configures and enables at least one provider.

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

	// Model alias validation
	for alias, modelAlias := range cfg.ModelAliases {
		if modelAlias.Provider == "" {
			return fmt.Errorf("model_aliases '%s' provider is required", alias)
		}
		if !providerNames[modelAlias.Provider] {
			return fmt.Errorf("model_aliases '%s' references unknown provider: %s", alias, modelAlias.Provider)
		}
		if modelAlias.Model == "" {
			return fmt.Errorf("model_aliases '%s' model is required", alias)
		}
	}

	// Custom models validation
	for name, customModel := range cfg.CustomModels {
		if customModel.Provider == "" {
			return fmt.Errorf("custom_models '%s' provider is required", name)
		}
		if !providerNames[customModel.Provider] {
			return fmt.Errorf("custom_models '%s' references unknown provider: %s", name, customModel.Provider)
		}
		if customModel.Model == "" {
			return fmt.Errorf("custom_models '%s' model is required", name)
		}
	}

	return nil
}
