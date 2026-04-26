package config

import (
	"os"
	"testing"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name        string
		config      string
		envVars     map[string]string
		wantErr     bool
		errContains string
	}{
		{
			name: "valid minimal config",
			config: `
server:
  host: "127.0.0.1"
  port: 20128
  api_key: "test-key"
logging:
  level: "info"
storage:
  sqlite_path: "./data/test.db"
retry:
  max_attempts: 3
  initial_backoff_ms: 100
  max_backoff_ms: 2000
providers:
  - name: "openai"
    type: "openai_compat"
    base_url: "https://api.openai.com"
    api_key: "sk-test"
    enabled: true
`,
			wantErr: false,
		},
		{
			name: "valid config with env var expansion",
			config: `
server:
  host: "127.0.0.1"
  port: 20128
  api_key: "${API_KEY}"
logging:
  level: "info"
storage:
  sqlite_path: "./data/test.db"
retry:
  max_attempts: 3
  initial_backoff_ms: 100
  max_backoff_ms: 2000
providers:
  - name: "openai"
    type: "openai_compat"
    base_url: "https://api.openai.com"
    api_key: "${OPENAI_KEY}"
    enabled: true
`,
			envVars: map[string]string{
				"API_KEY":    "test-api-key",
				"OPENAI_KEY": "sk-test-key",
			},
			wantErr: false,
		},
		{
			name: "missing api_key",
			config: `
server:
  host: "127.0.0.1"
  port: 20128
logging:
  level: "info"
storage:
  sqlite_path: "./data/test.db"
retry:
  max_attempts: 3
  initial_backoff_ms: 100
  max_backoff_ms: 2000
providers:
  - name: "openai"
    type: "openai_compat"
    base_url: "https://api.openai.com"
    api_key: "sk-test"
    enabled: true
`,
			wantErr:     true,
			errContains: "server.api_key is required",
		},
		{
			name: "invalid port",
			config: `
server:
  host: "127.0.0.1"
  port: 99999
  api_key: "test-key"
logging:
  level: "info"
storage:
  sqlite_path: "./data/test.db"
retry:
  max_attempts: 3
  initial_backoff_ms: 100
  max_backoff_ms: 2000
providers:
  - name: "openai"
    type: "openai_compat"
    base_url: "https://api.openai.com"
    api_key: "sk-test"
    enabled: true
`,
			wantErr:     true,
			errContains: "server.port must be between 1 and 65535",
		},
		{
			name: "no providers",
			config: `
server:
  host: "127.0.0.1"
  port: 20128
  api_key: "test-key"
logging:
  level: "info"
storage:
  sqlite_path: "./data/test.db"
retry:
  max_attempts: 3
  initial_backoff_ms: 100
  max_backoff_ms: 2000
providers: []
`,
			wantErr:     true,
			errContains: "at least one provider is required",
		},
		{
			name: "no enabled providers",
			config: `
server:
  host: "127.0.0.1"
  port: 20128
  api_key: "test-key"
logging:
  level: "info"
storage:
  sqlite_path: "./data/test.db"
retry:
  max_attempts: 3
  initial_backoff_ms: 100
  max_backoff_ms: 2000
providers:
  - name: "openai"
    type: "openai_compat"
    base_url: "https://api.openai.com"
    api_key: "sk-test"
    enabled: false
`,
			wantErr:     true,
			errContains: "at least one provider must be enabled",
		},
		{
			name: "invalid provider type",
			config: `
server:
  host: "127.0.0.1"
  port: 20128
  api_key: "test-key"
logging:
  level: "info"
storage:
  sqlite_path: "./data/test.db"
retry:
  max_attempts: 3
  initial_backoff_ms: 100
  max_backoff_ms: 2000
providers:
  - name: "invalid"
    type: "invalid_type"
    base_url: "https://api.openai.com"
    api_key: "sk-test"
    enabled: true
`,
			wantErr:     true,
			errContains: "provider[0].type must be one of",
		},
		{
			name: "duplicate provider names",
			config: `
server:
  host: "127.0.0.1"
  port: 20128
  api_key: "test-key"
logging:
  level: "info"
storage:
  sqlite_path: "./data/test.db"
retry:
  max_attempts: 3
  initial_backoff_ms: 100
  max_backoff_ms: 2000
providers:
  - name: "openai"
    type: "openai_compat"
    base_url: "https://api.openai.com"
    api_key: "sk-test"
    enabled: true
  - name: "openai"
    type: "anthropic"
    base_url: "https://api.anthropic.com"
    api_key: "sk-test"
    enabled: true
`,
			wantErr:     true,
			errContains: "duplicate provider name: openai",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			for k, v := range tt.envVars {
				os.Setenv(k, v)
				defer os.Unsetenv(k)
			}

			// Write config to temp file
			tmpFile, err := os.CreateTemp("", "config-*.yaml")
			if err != nil {
				t.Fatalf("failed to create temp file: %v", err)
			}
			defer os.Remove(tmpFile.Name())

			if _, err := tmpFile.WriteString(tt.config); err != nil {
				t.Fatalf("failed to write config: %v", err)
			}
			tmpFile.Close()

			// Load config
			cfg, err := Load(tmpFile.Name())

			if tt.wantErr {
				if err == nil {
					t.Errorf("Load() expected error but got none")
					return
				}
				if tt.errContains != "" {
					if !contains(err.Error(), tt.errContains) {
						t.Errorf("Load() error = %v, want error containing %q", err, tt.errContains)
					}
				}
				return
			}

			if err != nil {
				t.Errorf("Load() unexpected error: %v", err)
				return
			}

			// Verify defaults were applied
			if cfg.Server.Host != "127.0.0.1" {
				t.Errorf("Server.Host = %v, want 127.0.0.1", cfg.Server.Host)
			}
			if cfg.Server.Port != 20128 {
				t.Errorf("Server.Port = %v, want 20128", cfg.Server.Port)
			}
			if cfg.Logging.Level != "info" {
				t.Errorf("Logging.Level = %v, want info", cfg.Logging.Level)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
