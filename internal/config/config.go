package config

import (
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server    ServerConfig              `yaml:"server"`
	Logging   LoggingConfig             `yaml:"logging"`
	Providers []ProviderConfig          `yaml:"providers"`
	Routes    map[string][]RouteTarget  `yaml:"routes"`
}

type ServerConfig struct {
	Host                  string `yaml:"host"`
	Port                  int    `yaml:"port"`
	APIKey                string `yaml:"api_key"`
	RequestTimeoutSeconds int    `yaml:"request_timeout_seconds"`
}

type LoggingConfig struct {
	Level string `yaml:"level"`
}

type ProviderConfig struct {
	Name    string `yaml:"name"`
	Type    string `yaml:"type"`
	BaseURL string `yaml:"base_url"`
	APIKey  string `yaml:"api_key"`
	Enabled bool   `yaml:"enabled"`
}

type RouteTarget struct {
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
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "info"
	}
	if cfg.Routes == nil {
		cfg.Routes = map[string][]RouteTarget{}
	}
}

func validate(cfg Config) error {
	if cfg.Server.APIKey == "" {
		return fmt.Errorf("server.api_key is required")
	}
	if len(cfg.Providers) == 0 {
		return fmt.Errorf("at least one provider is required")
	}
	return nil
}
