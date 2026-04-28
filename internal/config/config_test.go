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
  port: 1988
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
  port: 1988
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
  port: 1988
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
			errContains: "server.api_key or server.admin_api_keys is required",
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
  port: 1988
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
			wantErr: false,
		},
		{
			name: "no enabled providers allowed for onboarding",
			config: `
server:
  host: "127.0.0.1"
  port: 1988
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
			wantErr: false,
		},
		{
			name: "invalid provider type",
			config: `
server:
  host: "127.0.0.1"
  port: 1988
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
			errContains: "provider[0].type must be a known catalog provider or alias",
		},
		{
			name: "duplicate provider names",
			config: `
server:
  host: "127.0.0.1"
  port: 1988
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
			if cfg.Server.Port != 1988 {
				t.Errorf("Server.Port = %v, want 1988", cfg.Server.Port)
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

func TestConfigClone_PreservesExtendedFields(t *testing.T) {
	original := Config{
		Server: ServerConfig{
			APIKey:                "test-key",
			RequestTimeoutSeconds: 30,
		},
		Logging: LoggingConfig{Level: "info"},
		Storage: StorageConfig{SQLitePath: "./data/test.db"},
		Retry: RetryConfig{
			MaxAttempts:      1,
			InitialBackoffMs: 100,
			MaxBackoffMs:     1000,
		},
		Tunnel: TunnelConfig{
			Enabled:  true,
			Provider: "cloudflare",
			Hostname: "router.example.com",
		},
		MITM: MITMConfig{
			Enabled:    true,
			ListenAddr: "127.0.0.1:8081",
		},
		Policies: []PolicyRule{{
			Name:       "policy-1",
			Action:     "allow",
			MatchModel: "fast",
		}},
		Sync: SyncConfig{
			Enabled:  true,
			Provider: "s3",
			Bucket:   "router-bucket",
		},
		Nodes: []NodeConfig{{
			Name:    "node-1",
			BaseURL: "http://127.0.0.1:20129",
			Enabled: true,
		}},
	}

	cloned := original.Clone()

	if !cloned.Tunnel.Enabled || cloned.Tunnel.Provider != "cloudflare" {
		t.Fatalf("tunnel config not cloned: %+v", cloned.Tunnel)
	}
	if !cloned.MITM.Enabled || cloned.MITM.ListenAddr != "127.0.0.1:8081" {
		t.Fatalf("mitm config not cloned: %+v", cloned.MITM)
	}
	if len(cloned.Policies) != 1 || cloned.Policies[0].Name != "policy-1" {
		t.Fatalf("policies not cloned: %+v", cloned.Policies)
	}
	if !cloned.Sync.Enabled || cloned.Sync.Provider != "s3" {
		t.Fatalf("sync config not cloned: %+v", cloned.Sync)
	}
	if len(cloned.Nodes) != 1 || cloned.Nodes[0].Name != "node-1" {
		t.Fatalf("nodes not cloned: %+v", cloned.Nodes)
	}
}
