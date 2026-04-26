package config

import (
	"fmt"
	"os"
	"sync"

	"gopkg.in/yaml.v3"
)

// RuntimeConfig provides thread-safe access to configuration with runtime mutation support
type RuntimeConfig struct {
	mu         sync.RWMutex
	config     Config
	configPath string
}

// NewRuntimeConfig creates a new runtime config wrapper
func NewRuntimeConfig(cfg Config, configPath string) *RuntimeConfig {
	return &RuntimeConfig{
		config:     cfg,
		configPath: configPath,
	}
}

// Get returns a copy of the current config (read-only)
func (rc *RuntimeConfig) Get() Config {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc.config
}

// Update atomically updates the config with validation
// Returns error if validation fails (config remains unchanged)
func (rc *RuntimeConfig) Update(updateFn func(*Config) error) error {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	// Create a deep copy for validation
	newConfig := rc.config.Clone()

	// Apply update function
	if err := updateFn(&newConfig); err != nil {
		return fmt.Errorf("update function failed: %w", err)
	}

	// Validate the new config
	if err := validate(newConfig); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Update successful - apply changes
	rc.config = newConfig
	return nil
}

// ValidateConfig validates a config function without applying it
func (rc *RuntimeConfig) ValidateConfig(updateFn func(*Config) error) error {
	rc.mu.RLock()
	defer rc.mu.RUnlock()

	// Create a deep copy for validation
	newConfig := rc.config.Clone()

	// Apply update function
	if err := updateFn(&newConfig); err != nil {
		return fmt.Errorf("update function failed: %w", err)
	}

	// Validate the new config
	if err := validate(newConfig); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	return nil
}

// UpdateAndPersist atomically updates config, validates, and persists to YAML
func (rc *RuntimeConfig) UpdateAndPersist(updateFn func(*Config) error) error {
	// First update in-memory
	if err := rc.Update(updateFn); err != nil {
		return err
	}

	// Then persist to disk
	return rc.Persist()
}

// TransactionalUpdate performs a transactional config update: validate, persist, then apply
// If persist fails, runtime is unchanged. If apply fails, we rollback from disk.
func (rc *RuntimeConfig) TransactionalUpdate(updateFn func(*Config) error) error {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	// Create a deep copy for validation and persistence
	newConfig := rc.config.Clone()

	// Apply update function
	if err := updateFn(&newConfig); err != nil {
		return fmt.Errorf("update function failed: %w", err)
	}

	// Validate the new config
	if err := validate(newConfig); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Persist new config to temp file
	tempPath := rc.configPath + ".tmp"
	data, err := yaml.Marshal(&newConfig)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(tempPath, data, 0600); err != nil {
		return fmt.Errorf("write temp config: %w", err)
	}

	// Atomically rename temp to final
	if err := os.Rename(tempPath, rc.configPath); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("rename config: %w", err)
	}

	// Update runtime config only after successful persist
	rc.config = newConfig

	return nil
}

// UpdateWithReconfigure performs a fully transactional update with engine reconfigure
// It validates, persists, applies to memory, then calls reconfigureFn.
// If reconfigureFn fails, it rolls back both memory and disk to the previous state.
func (rc *RuntimeConfig) UpdateWithReconfigure(updateFn func(*Config) error, reconfigureFn func(Config) error) error {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	// Backup current config for rollback (deep copy to avoid shared references)
	oldConfig := rc.config.Clone()

	// Create a deep copy for validation and persistence
	newConfig := rc.config.Clone()

	// Apply update function
	if err := updateFn(&newConfig); err != nil {
		return fmt.Errorf("update function failed: %w", err)
	}

	// Validate the new config
	if err := validate(newConfig); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Persist new config to temp file
	tempPath := rc.configPath + ".tmp"
	data, err := yaml.Marshal(&newConfig)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(tempPath, data, 0600); err != nil {
		return fmt.Errorf("write temp config: %w", err)
	}

	// Atomically rename temp to final
	if err := os.Rename(tempPath, rc.configPath); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("rename config: %w", err)
	}

	// Update runtime config memory
	rc.config = newConfig

	// Try to reconfigure engine with new config
	if reconfigureFn != nil {
		if reconfigureErr := reconfigureFn(newConfig); reconfigureErr != nil {
			// Reconfigure failed - rollback memory immediately
			rc.config = oldConfig

			// Restore old config to disk
			oldData, rollbackErr := yaml.Marshal(&oldConfig)
			if rollbackErr != nil {
				return fmt.Errorf("reconfigure failed: %w; disk rollback also failed (marshal): %v", reconfigureErr, rollbackErr)
			}

			if rollbackErr = os.WriteFile(tempPath, oldData, 0600); rollbackErr != nil {
				return fmt.Errorf("reconfigure failed: %w; disk rollback also failed (write): %v", reconfigureErr, rollbackErr)
			}

			if rollbackErr = os.Rename(tempPath, rc.configPath); rollbackErr != nil {
				os.Remove(tempPath)
				return fmt.Errorf("reconfigure failed: %w; disk rollback also failed (rename): %v", reconfigureErr, rollbackErr)
			}

			return fmt.Errorf("reconfigure failed, rolled back: %w", reconfigureErr)
		}
	}

	return nil
}

// Persist saves the current config to YAML file
func (rc *RuntimeConfig) Persist() error {
	rc.mu.RLock()
	defer rc.mu.RUnlock()

	// Marshal to YAML
	data, err := yaml.Marshal(&rc.config)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	// Write to file atomically (write to temp, then rename)
	tempPath := rc.configPath + ".tmp"
	if err := os.WriteFile(tempPath, data, 0600); err != nil {
		return fmt.Errorf("write temp config: %w", err)
	}

	if err := os.Rename(tempPath, rc.configPath); err != nil {
		os.Remove(tempPath) // Clean up temp file
		return fmt.Errorf("rename config: %w", err)
	}

	return nil
}

// Reload reloads config from disk and validates
func (rc *RuntimeConfig) Reload() error {
	newConfig, err := Load(rc.configPath)
	if err != nil {
		return fmt.Errorf("reload config: %w", err)
	}

	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.config = newConfig
	return nil
}

// GetProvider returns a copy of a specific provider config
func (rc *RuntimeConfig) GetProvider(name string) (ProviderConfig, bool) {
	rc.mu.RLock()
	defer rc.mu.RUnlock()

	for _, p := range rc.config.Providers {
		if p.Name == name {
			return p, true
		}
	}
	return ProviderConfig{}, false
}

// GetRoute returns a copy of a specific route config
func (rc *RuntimeConfig) GetRoute(name string) (RouteConfig, bool) {
	rc.mu.RLock()
	defer rc.mu.RUnlock()

	route, ok := rc.config.Routes[name]
	return route, ok
}

// GetModelAlias returns a copy of a specific model alias
func (rc *RuntimeConfig) GetModelAlias(alias string) (ModelAlias, bool) {
	rc.mu.RLock()
	defer rc.mu.RUnlock()

	modelAlias, ok := rc.config.ModelAliases[alias]
	return modelAlias, ok
}

// ListProviders returns a copy of all provider configs
func (rc *RuntimeConfig) ListProviders() []ProviderConfig {
	rc.mu.RLock()
	defer rc.mu.RUnlock()

	providers := make([]ProviderConfig, len(rc.config.Providers))
	copy(providers, rc.config.Providers)
	return providers
}

// ListRoutes returns a copy of all route configs
func (rc *RuntimeConfig) ListRoutes() map[string]RouteConfig {
	rc.mu.RLock()
	defer rc.mu.RUnlock()

	routes := make(map[string]RouteConfig, len(rc.config.Routes))
	for k, v := range rc.config.Routes {
		routes[k] = v
	}
	return routes
}

// ListModelAliases returns a copy of all model aliases
func (rc *RuntimeConfig) ListModelAliases() map[string]ModelAlias {
	rc.mu.RLock()
	defer rc.mu.RUnlock()

	aliases := make(map[string]ModelAlias, len(rc.config.ModelAliases))
	for k, v := range rc.config.ModelAliases {
		aliases[k] = v
	}
	return aliases
}

// ListCustomModels returns a copy of all custom models
func (rc *RuntimeConfig) ListCustomModels() map[string]CustomModel {
	rc.mu.RLock()
	defer rc.mu.RUnlock()

	customModels := make(map[string]CustomModel, len(rc.config.CustomModels))
	for k, v := range rc.config.CustomModels {
		customModels[k] = v
	}
	return customModels
}

// AddProvider adds a new provider to the config
func (rc *RuntimeConfig) AddProvider(provider ProviderConfig) error {
	return rc.Update(func(cfg *Config) error {
		// Check for duplicate name
		for _, p := range cfg.Providers {
			if p.Name == provider.Name {
				return fmt.Errorf("provider '%s' already exists", provider.Name)
			}
		}
		cfg.Providers = append(cfg.Providers, provider)
		return nil
	})
}

// UpdateProvider updates an existing provider
func (rc *RuntimeConfig) UpdateProvider(name string, provider ProviderConfig) error {
	return rc.Update(func(cfg *Config) error {
		found := false
		for i := range cfg.Providers {
			if cfg.Providers[i].Name == name {
				// Preserve name if not changing
				if provider.Name == "" {
					provider.Name = name
				}
				cfg.Providers[i] = provider
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("provider '%s' not found", name)
		}
		return nil
	})
}

// DeleteProvider removes a provider from the config
func (rc *RuntimeConfig) DeleteProvider(name string) error {
	return rc.Update(func(cfg *Config) error {
		found := false
		newProviders := make([]ProviderConfig, 0, len(cfg.Providers))
		for _, p := range cfg.Providers {
			if p.Name != name {
				newProviders = append(newProviders, p)
			} else {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("provider '%s' not found", name)
		}
		cfg.Providers = newProviders
		return nil
	})
}

// AddRoute adds a new route to the config
func (rc *RuntimeConfig) AddRoute(name string, route RouteConfig) error {
	return rc.Update(func(cfg *Config) error {
		if cfg.Routes == nil {
			cfg.Routes = make(map[string]RouteConfig)
		}
		if _, exists := cfg.Routes[name]; exists {
			return fmt.Errorf("route '%s' already exists", name)
		}
		cfg.Routes[name] = route
		return nil
	})
}

// UpdateRoute updates an existing route
func (rc *RuntimeConfig) UpdateRoute(name string, route RouteConfig) error {
	return rc.Update(func(cfg *Config) error {
		if _, exists := cfg.Routes[name]; !exists {
			return fmt.Errorf("route '%s' not found", name)
		}
		cfg.Routes[name] = route
		return nil
	})
}

// DeleteRoute removes a route from the config
func (rc *RuntimeConfig) DeleteRoute(name string) error {
	return rc.Update(func(cfg *Config) error {
		if _, exists := cfg.Routes[name]; !exists {
			return fmt.Errorf("route '%s' not found", name)
		}
		delete(cfg.Routes, name)
		return nil
	})
}

// AddModelAlias adds a new model alias to the config
func (rc *RuntimeConfig) AddModelAlias(alias string, modelAlias ModelAlias) error {
	return rc.Update(func(cfg *Config) error {
		if cfg.ModelAliases == nil {
			cfg.ModelAliases = make(map[string]ModelAlias)
		}
		if _, exists := cfg.ModelAliases[alias]; exists {
			return fmt.Errorf("model alias '%s' already exists", alias)
		}
		cfg.ModelAliases[alias] = modelAlias
		return nil
	})
}

// UpdateModelAlias updates an existing model alias
func (rc *RuntimeConfig) UpdateModelAlias(alias string, modelAlias ModelAlias) error {
	return rc.Update(func(cfg *Config) error {
		if _, exists := cfg.ModelAliases[alias]; !exists {
			return fmt.Errorf("model alias '%s' not found", alias)
		}
		cfg.ModelAliases[alias] = modelAlias
		return nil
	})
}

// DeleteModelAlias removes a model alias from the config
func (rc *RuntimeConfig) DeleteModelAlias(alias string) error {
	return rc.Update(func(cfg *Config) error {
		if _, exists := cfg.ModelAliases[alias]; !exists {
			return fmt.Errorf("model alias '%s' not found", alias)
		}
		delete(cfg.ModelAliases, alias)
		return nil
	})
}

// UpdateSettings updates the settings section
func (rc *RuntimeConfig) UpdateSettings(settings SettingsConfig) error {
	return rc.Update(func(cfg *Config) error {
		cfg.Settings = settings
		return nil
	})
}

// ListAdminAPIKeys returns all configured admin API keys (legacy key included if present)
func (rc *RuntimeConfig) ListAdminAPIKeys() []string {
	rc.mu.RLock()
	defer rc.mu.RUnlock()

	keys := make([]string, 0, len(rc.config.Server.AdminAPIKeys)+1)
	if rc.config.Server.APIKey != "" {
		keys = append(keys, rc.config.Server.APIKey)
	}
	keys = append(keys, rc.config.Server.AdminAPIKeys...)
	return keys
}

// AddAdminAPIKey appends a new admin API key
func (rc *RuntimeConfig) AddAdminAPIKey(key string) error {
	return rc.Update(func(cfg *Config) error {
		for _, existing := range cfg.Server.AdminAPIKeys {
			if existing == key {
				return fmt.Errorf("admin API key already exists")
			}
		}
		if cfg.Server.APIKey == key {
			return fmt.Errorf("admin API key already exists")
		}
		cfg.Server.AdminAPIKeys = append(cfg.Server.AdminAPIKeys, key)
		return nil
	})
}

// UpdateAdminAPIKey replaces an existing admin API key
func (rc *RuntimeConfig) UpdateAdminAPIKey(oldKey string, newKey string) error {
	return rc.Update(func(cfg *Config) error {
		if cfg.Server.APIKey == oldKey {
			cfg.Server.APIKey = newKey
			return nil
		}

		found := false
		for i := range cfg.Server.AdminAPIKeys {
			if cfg.Server.AdminAPIKeys[i] == oldKey {
				cfg.Server.AdminAPIKeys[i] = newKey
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("admin API key not found")
		}
		return nil
	})
}

// DeleteAdminAPIKey removes an admin API key while preserving at least one key
func (rc *RuntimeConfig) DeleteAdminAPIKey(key string) error {
	return rc.Update(func(cfg *Config) error {
		if cfg.Server.APIKey == key {
			if len(cfg.Server.AdminAPIKeys) == 0 {
				return fmt.Errorf("cannot delete the last admin API key")
			}
			cfg.Server.APIKey = cfg.Server.AdminAPIKeys[0]
			cfg.Server.AdminAPIKeys = cfg.Server.AdminAPIKeys[1:]
			return nil
		}

		newKeys := make([]string, 0, len(cfg.Server.AdminAPIKeys))
		found := false
		for _, existing := range cfg.Server.AdminAPIKeys {
			if existing == key {
				found = true
				continue
			}
			newKeys = append(newKeys, existing)
		}
		if !found {
			return fmt.Errorf("admin API key not found")
		}
		cfg.Server.AdminAPIKeys = newKeys
		return nil
	})
}

// AddCustomModel adds a custom model definition
func (rc *RuntimeConfig) AddCustomModel(name string, customModel CustomModel) error {
	return rc.Update(func(cfg *Config) error {
		if cfg.CustomModels == nil {
			cfg.CustomModels = make(map[string]CustomModel)
		}
		if _, exists := cfg.CustomModels[name]; exists {
			return fmt.Errorf("custom model '%s' already exists", name)
		}
		cfg.CustomModels[name] = customModel
		return nil
	})
}

// UpdateCustomModel updates an existing custom model definition
func (rc *RuntimeConfig) UpdateCustomModel(name string, customModel CustomModel) error {
	return rc.Update(func(cfg *Config) error {
		if _, exists := cfg.CustomModels[name]; !exists {
			return fmt.Errorf("custom model '%s' not found", name)
		}
		cfg.CustomModels[name] = customModel
		return nil
	})
}

// DeleteCustomModel removes a custom model definition
func (rc *RuntimeConfig) DeleteCustomModel(name string) error {
	return rc.Update(func(cfg *Config) error {
		if _, exists := cfg.CustomModels[name]; !exists {
			return fmt.Errorf("custom model '%s' not found", name)
		}
		delete(cfg.CustomModels, name)
		return nil
	})
}
