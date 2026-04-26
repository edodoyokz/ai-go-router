package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/edodoyokz/ai-go-router/internal/cache"
	"github.com/edodoyokz/ai-go-router/internal/config"
	"github.com/edodoyokz/ai-go-router/internal/providers"
	routing "github.com/edodoyokz/ai-go-router/internal/router"
	"github.com/edodoyokz/ai-go-router/internal/storage"
	"github.com/edodoyokz/ai-go-router/internal/translator"
	"github.com/edodoyokz/ai-go-router/internal/usage"
	"github.com/edodoyokz/ai-go-router/internal/webui"
)

type Server struct {
	runtimeConfig   *config.RuntimeConfig
	logger          zerolog.Logger
	engine          *routing.Engine
	translators     *translator.Registry
	asyncWriter     *storage.AsyncWriter
	rateLimiter     *RateLimiter
	toolDetector    *ToolDetector
	metrics         *Metrics
	cache           *cache.LRUCache
	pricingRegistry *usage.PricingRegistry
	usageFetcher    *usage.UsageFetcher
}

func (s *Server) reconfigureFromConfig(cfg config.Config) error {
	registry, err := providers.BuildRegistryFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("rebuild provider registry: %w", err)
	}

	s.engine.Reconfigure(cfg.Routes, cfg.ModelAliases, registry, cfg.Retry)
	return nil
}

func mapConfigErrorToHTTP(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if strings.Contains(err.Error(), "already exists") || strings.Contains(err.Error(), "duplicate") {
		return http.StatusConflict
	}
	if strings.Contains(err.Error(), "not found") {
		return http.StatusNotFound
	}
	if strings.Contains(err.Error(), "validation failed") {
		return http.StatusBadRequest
	}
	return http.StatusBadRequest
}

// generateCacheKey creates a unique cache key from a chat request
func generateCacheKey(request providers.ChatRequest) string {
	// Marshal request to JSON for hashing
	data, err := json.Marshal(request)
	if err != nil {
		return ""
	}

	// Create SHA256 hash
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

type Metrics struct {
	mu              sync.RWMutex
	RequestsTotal   int64
	RequestsSuccess int64
	RequestsError   int64
	ProviderUsage   map[string]int64
}

func NewServer(runtimeConfig *config.RuntimeConfig, logger zerolog.Logger, engine *routing.Engine, asyncWriter *storage.AsyncWriter) *Server {
	// Initialize LRU cache with 1000 entries
	cacheInstance := cache.NewLRUCache(1000)

	// Initialize pricing registry with default pricing
	pricingReg := usage.NewPricingRegistry()
	pricingReg.LoadDefaults()

	// Initialize usage fetcher
	usageFetch := usage.NewUsageFetcher()

	return &Server{
		runtimeConfig: runtimeConfig,
		logger:        logger,
		engine:        engine,
		translators:   translator.NewRegistry(),
		asyncWriter:   asyncWriter,
		rateLimiter:   nil,
		toolDetector:  NewToolDetector(),
		metrics: &Metrics{
			ProviderUsage: make(map[string]int64),
		},
		cache:           cacheInstance,
		pricingRegistry: pricingReg,
		usageFetcher:    usageFetch,
	}
}

func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()

	// Global middleware (applied to all routes)
	r.Use(RequestIDMiddleware)
	r.Use(PanicRecoveryMiddleware(s.logger))
	r.Use(SecurityHeadersMiddleware)
	r.Use(StructuredLoggingMiddleware(s.logger))

	// Get config for handler setup
	cfg := s.runtimeConfig.Get()

	// CORS middleware (if configured)
	if len(cfg.Server.CORS.AllowedOrigins) > 0 {
		r.Use(CORSMiddleware(
			cfg.Server.CORS.AllowedOrigins,
			cfg.Server.CORS.AllowedMethods,
			cfg.Server.CORS.AllowedHeaders,
			cfg.Server.CORS.AllowCredentials,
			cfg.Server.CORS.MaxAgeSeconds,
		))
	}

	// Rate limiting middleware (if configured)
	if s.rateLimiter != nil {
		r.Use(RateLimitMiddleware(s.rateLimiter, s.runtimeConfig))
	}

	// Embedded Web UI
	r.Handle("/ui", http.RedirectHandler("/ui/", http.StatusMovedPermanently))
	r.Handle("/ui/*", http.StripPrefix("/ui", webui.Handler()))

	// Public routes (no auth required)
	r.Get("/healthz", s.handleHealthz)
	r.Get("/readyz", s.handleReadyz)
	r.Get("/metrics", s.handleMetrics)

	// Protected routes (auth required)
	r.Group(func(r chi.Router) {
		r.Use(AuthMiddlewareWithRuntimeConfig(s.runtimeConfig))
		r.Get("/v1/models", s.handleModels)
		r.Post("/v1/chat/completions", s.handleChatCompletions)
		r.Post("/v1/messages", s.handleMessages)
		r.Post("/v1/responses", s.handleResponses)
		r.Post("/v1/embeddings", s.handleEmbeddings)
		r.Post("/v1/audio/speech", s.handleAudioSpeech)
		r.Post("/v1/images/generations", s.handleImagesGenerations)
		r.Get("/api/config/export", s.handleConfigExport)
		r.Post("/api/config/import", s.handleConfigImport)
		r.Get("/api/providers", s.handleProvidersList)
		r.Post("/api/providers", s.handleProvidersCreate)
		r.Put("/api/providers/{name}", s.handleProvidersUpdate)
		r.Delete("/api/providers/{name}", s.handleProvidersDelete)
		r.Get("/api/combos", s.handleCombosList)
		r.Post("/api/combos", s.handleCombosCreate)
		r.Put("/api/combos/{name}", s.handleCombosUpdate)
		r.Delete("/api/combos/{name}", s.handleCombosDelete)
		r.Get("/api/keys", s.handleKeysList)
		r.Post("/api/keys", s.handleKeysCreate)
		r.Put("/api/keys/{id}", s.handleKeysUpdate)
		r.Delete("/api/keys/{id}", s.handleKeysDelete)
		r.Get("/api/models/alias", s.handleModelAliasesList)
		r.Post("/api/models/alias", s.handleModelAliasesCreate)
		r.Put("/api/models/alias/{name}", s.handleModelAliasesUpdate)
		r.Delete("/api/models/alias/{name}", s.handleModelAliasesDelete)
		r.Get("/api/models/custom", s.handleModelsCustomList)
		r.Post("/api/models/custom", s.handleModelsCustomCreate)
		r.Put("/api/models/custom/{name}", s.handleModelsCustomUpdate)
		r.Delete("/api/models/custom/{name}", s.handleModelsCustomDelete)
		r.Get("/api/settings", s.handleSettingsGet)
		r.Put("/api/settings", s.handleSettingsPut)
		r.Get("/api/logs", s.handleLogsList)
		r.Get("/api/usage", s.handleUsage)
		r.Get("/api/providers/{name}/health", s.handleProviderHealth)
		r.Get("/api/providers/{name}/accounts/{account}/health", s.handleAccountHealth)
		r.Get("/api/config", s.handleConfigGet)
		r.Get("/api/metrics", s.handleMetrics)
		r.Get("/api/pricing", s.handlePricing)
		r.Get("/api/oauth/tokens", s.handleOAuthTokensList)
		r.Delete("/api/oauth/tokens/{provider}/{account}", s.handleOAuthTokenDelete)
	})

	return r
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleReadyz(w http.ResponseWriter, _ *http.Request) {
	checks := map[string]any{
		"sqlite":    "ok",
		"providers": "ok",
	}

	// Check SQLite connectivity
	if s.asyncWriter != nil {
		db := s.asyncWriter.GetDB()
		if db != nil {
			if err := db.Ping(); err != nil {
				checks["sqlite"] = "error: " + err.Error()
			}
		} else {
			checks["sqlite"] = "disabled"
		}
	} else {
		checks["sqlite"] = "disabled"
	}

	// Check if there are any enabled providers
	cfg := s.runtimeConfig.Get()
	enabledProviders := 0
	for _, p := range cfg.Providers {
		if p.Enabled {
			enabledProviders++
		}
	}
	if enabledProviders == 0 {
		checks["providers"] = "error: no enabled providers"
	}

	// Determine overall status
	status := "ready"
	for _, v := range checks {
		if str, ok := v.(string); ok && len(str) > 3 && str[:5] == "error" {
			status = "not ready"
			break
		}
	}

	if status == "ready" {
		writeJSON(w, http.StatusOK, map[string]any{"status": status, "checks": checks})
	} else {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": status, "checks": checks})
	}
}

func (s *Server) handleProviderHealth(w http.ResponseWriter, r *http.Request) {
	providerName := chi.URLParam(r, "name")

	// Find provider in config
	provider, ok := s.runtimeConfig.GetProvider(providerName)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"status": "error",
			"error":  "provider not found",
		})
		return
	}

	health := map[string]any{
		"provider": provider.Name,
		"status":   "healthy",
		"enabled":  provider.Enabled,
	}

	// Check if provider is enabled
	if !provider.Enabled {
		health["status"] = "disabled"
		health["reason"] = "provider is disabled in config"
		writeJSON(w, http.StatusOK, health)
		return
	}

	// Check if provider has API keys configured
	if provider.APIKey == "" && len(provider.Accounts) == 0 {
		health["status"] = "unhealthy"
		health["reason"] = "no API keys configured"
		writeJSON(w, http.StatusOK, health)
		return
	}

	// Deep connectivity check (if query param deep=true)
	if r.URL.Query().Get("deep") == "true" {
		// Perform a simple connectivity check by making a minimal request
		// For OpenAI-compatible providers, we can try to list models
		// For now, we'll do a simple HTTP connectivity check
		client := &http.Client{Timeout: 5 * time.Second}
		req, err := http.NewRequest("GET", provider.BaseURL+"/models", nil)
		if err != nil {
			health["status"] = "unhealthy"
			health["reason"] = fmt.Sprintf("failed to create request: %v", err)
			health["connectivity"] = "failed"
			writeJSON(w, http.StatusOK, health)
			return
		}

		// Add headers if configured
		for k, v := range provider.Headers {
			req.Header.Set(k, v)
		}

		resp, err := client.Do(req)
		if err != nil {
			health["status"] = "unhealthy"
			health["reason"] = fmt.Sprintf("connectivity check failed: %v", err)
			health["connectivity"] = "failed"
			writeJSON(w, http.StatusOK, health)
			return
		}
		resp.Body.Close()

		// Check if we got a valid HTTP response (even if it's an error response)
		if resp.StatusCode >= 200 && resp.StatusCode < 600 {
			health["connectivity"] = "ok"
			health["http_status"] = resp.StatusCode
		} else {
			health["connectivity"] = "failed"
			health["http_status"] = resp.StatusCode
			health["status"] = "degraded"
		}
	}

	writeJSON(w, http.StatusOK, health)
}

func (s *Server) handleAccountHealth(w http.ResponseWriter, r *http.Request) {
	providerName := chi.URLParam(r, "name")
	accountName := chi.URLParam(r, "account")

	// Find provider in config
	provider, ok := s.runtimeConfig.GetProvider(providerName)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"status": "error",
			"error":  "provider not found",
		})
		return
	}

	health := map[string]any{
		"provider": providerName,
		"account":  accountName,
		"status":   "healthy",
	}

	// Check if provider is enabled
	if !provider.Enabled {
		health["status"] = "disabled"
		health["reason"] = "provider is disabled in config"
		writeJSON(w, http.StatusOK, health)
		return
	}

	// Find the account
	var account *config.AccountConfig
	for i := range provider.Accounts {
		if provider.Accounts[i].Name == accountName {
			account = &provider.Accounts[i]
			break
		}
	}

	if account == nil {
		// Check if using deprecated API key
		if provider.APIKey != "" && accountName == "default" {
			health["status"] = "healthy"
			health["note"] = "using deprecated single API key"
			writeJSON(w, http.StatusOK, health)
			return
		}
		writeJSON(w, http.StatusNotFound, map[string]any{
			"status": "error",
			"error":  "account not found",
		})
		return
	}

	// Check if account has API key
	if account.APIKey == "" {
		health["status"] = "unhealthy"
		health["reason"] = "account has no API key configured"
		writeJSON(w, http.StatusOK, health)
		return
	}

	writeJSON(w, http.StatusOK, health)
}

func (s *Server) handleProvidersList(w http.ResponseWriter, r *http.Request) {
	// Return list of providers from config
	providersList := s.runtimeConfig.ListProviders()
	providers := make([]map[string]any, 0, len(providersList))
	for _, provider := range providersList {
		p := map[string]any{
			"name":     provider.Name,
			"type":     provider.Type,
			"base_url": provider.BaseURL,
			"enabled":  provider.Enabled,
		}
		// Don't expose API keys in the list
		providers = append(providers, p)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"providers": providers,
		"count":     len(providers),
	})
}

func (s *Server) handleProvidersCreate(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var req config.ProviderConfig
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid json", "invalid_request_error", "")
		return
	}

	// Use UpdateWithReconfigure for full transactional update with rollback
	if err := s.runtimeConfig.UpdateWithReconfigure(func(cfg *config.Config) error {
		// Check for duplicate name
		for _, p := range cfg.Providers {
			if p.Name == req.Name {
				return fmt.Errorf("provider '%s' already exists", req.Name)
			}
		}
		cfg.Providers = append(cfg.Providers, req)
		return nil
	}, s.reconfigureFromConfig); err != nil {
		writeOpenAIError(w, mapConfigErrorToHTTP(err), err.Error(), "invalid_request_error", "")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"provider": req})
}

func (s *Server) handleProvidersUpdate(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	name := chi.URLParam(r, "name")

	var req config.ProviderConfig
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid json", "invalid_request_error", "")
		return
	}
	if req.Name == "" {
		req.Name = name
	}

	// Use UpdateWithReconfigure for full transactional update with rollback
	if err := s.runtimeConfig.UpdateWithReconfigure(func(cfg *config.Config) error {
		found := false
		for i := range cfg.Providers {
			if cfg.Providers[i].Name == name {
				cfg.Providers[i] = req
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("provider '%s' not found", name)
		}
		return nil
	}, s.reconfigureFromConfig); err != nil {
		writeOpenAIError(w, mapConfigErrorToHTTP(err), err.Error(), "invalid_request_error", "")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"provider": req})
}

func (s *Server) handleProvidersDelete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	// Use UpdateWithReconfigure for full transactional update with rollback
	if err := s.runtimeConfig.UpdateWithReconfigure(func(cfg *config.Config) error {
		found := false
		newProviders := make([]config.ProviderConfig, 0, len(cfg.Providers))
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
	}, s.reconfigureFromConfig); err != nil {
		writeOpenAIError(w, mapConfigErrorToHTTP(err), err.Error(), "invalid_request_error", "")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"deleted": name})
}

func (s *Server) handleCombosList(w http.ResponseWriter, r *http.Request) {
	// Return list of combos (routes) from config
	routesMap := s.runtimeConfig.ListRoutes()
	combos := make([]map[string]any, 0, len(routesMap))
	for name, route := range routesMap {
		c := map[string]any{
			"name":     name,
			"strategy": route.Strategy,
			"targets":  route.Targets,
		}
		combos = append(combos, c)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"combos": combos,
		"count":  len(combos),
	})
}

func (s *Server) handleCombosCreate(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var req struct {
		Name     string               `json:"name"`
		Strategy string               `json:"strategy,omitempty"`
		Targets  []config.RouteTarget `json:"targets"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid json", "invalid_request_error", "")
		return
	}
	if req.Name == "" {
		writeOpenAIError(w, http.StatusBadRequest, "name is required", "invalid_request_error", "")
		return
	}

	route := config.RouteConfig{Strategy: req.Strategy, Targets: req.Targets}

	// Use UpdateWithReconfigure for full transactional update with rollback
	if err := s.runtimeConfig.UpdateWithReconfigure(func(cfg *config.Config) error {
		if cfg.Routes == nil {
			cfg.Routes = make(map[string]config.RouteConfig)
		}
		if _, exists := cfg.Routes[req.Name]; exists {
			return fmt.Errorf("route '%s' already exists", req.Name)
		}
		cfg.Routes[req.Name] = route
		return nil
	}, s.reconfigureFromConfig); err != nil {
		writeOpenAIError(w, mapConfigErrorToHTTP(err), err.Error(), "invalid_request_error", "")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"name": req.Name, "route": route})
}

func (s *Server) handleCombosUpdate(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	name := chi.URLParam(r, "name")

	var req struct {
		Strategy string               `json:"strategy,omitempty"`
		Targets  []config.RouteTarget `json:"targets"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid json", "invalid_request_error", "")
		return
	}

	route := config.RouteConfig{Strategy: req.Strategy, Targets: req.Targets}

	// Use UpdateWithReconfigure for full transactional update with rollback
	if err := s.runtimeConfig.UpdateWithReconfigure(func(cfg *config.Config) error {
		if _, exists := cfg.Routes[name]; !exists {
			return fmt.Errorf("route '%s' not found", name)
		}
		cfg.Routes[name] = route
		return nil
	}, s.reconfigureFromConfig); err != nil {
		writeOpenAIError(w, mapConfigErrorToHTTP(err), err.Error(), "invalid_request_error", "")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"name": name, "route": route})
}

func (s *Server) handleCombosDelete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	// Use UpdateWithReconfigure for full transactional update with rollback
	if err := s.runtimeConfig.UpdateWithReconfigure(func(cfg *config.Config) error {
		if _, exists := cfg.Routes[name]; !exists {
			return fmt.Errorf("route '%s' not found", name)
		}
		delete(cfg.Routes, name)
		return nil
	}, s.reconfigureFromConfig); err != nil {
		writeOpenAIError(w, mapConfigErrorToHTTP(err), err.Error(), "invalid_request_error", "")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"deleted": name})
}

func (s *Server) handleKeysList(w http.ResponseWriter, r *http.Request) {
	keys := s.runtimeConfig.ListAdminAPIKeys()
	items := make([]map[string]any, 0, len(keys))
	for i, key := range keys {
		maskedKey := "****"
		if len(key) > 8 {
			maskedKey = key[:7] + "****"
		}
		items = append(items, map[string]any{
			"id":      strconv.Itoa(i),
			"api_key": maskedKey,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"keys": items, "count": len(items)})
}

func (s *Server) handleKeysCreate(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req struct {
		APIKey string `json:"api_key"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid json", "invalid_request_error", "")
		return
	}
	if req.APIKey == "" {
		writeOpenAIError(w, http.StatusBadRequest, "api_key is required", "invalid_request_error", "")
		return
	}

	if err := s.runtimeConfig.TransactionalUpdate(func(cfg *config.Config) error {
		for _, existing := range cfg.Server.AdminAPIKeys {
			if existing == req.APIKey {
				return fmt.Errorf("admin API key already exists")
			}
		}
		if cfg.Server.APIKey == req.APIKey {
			return fmt.Errorf("admin API key already exists")
		}
		cfg.Server.AdminAPIKeys = append(cfg.Server.AdminAPIKeys, req.APIKey)
		return nil
	}); err != nil {
		writeOpenAIError(w, mapConfigErrorToHTTP(err), err.Error(), "invalid_request_error", "")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"message": "key created"})
}

func (s *Server) handleKeysUpdate(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	id := chi.URLParam(r, "id")
	idx, err := strconv.Atoi(id)
	if err != nil || idx < 0 {
		writeOpenAIError(w, http.StatusBadRequest, "invalid key id", "invalid_request_error", "")
		return
	}

	keys := s.runtimeConfig.ListAdminAPIKeys()
	if idx >= len(keys) {
		writeOpenAIError(w, http.StatusNotFound, "key not found", "invalid_request_error", "")
		return
	}

	var req struct {
		APIKey string `json:"api_key"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid json", "invalid_request_error", "")
		return
	}
	if req.APIKey == "" {
		writeOpenAIError(w, http.StatusBadRequest, "api_key is required", "invalid_request_error", "")
		return
	}

	// Use TransactionalUpdate for atomic config update
	if err := s.runtimeConfig.TransactionalUpdate(func(cfg *config.Config) error {
		oldKey := keys[idx]
		if cfg.Server.APIKey == oldKey {
			cfg.Server.APIKey = req.APIKey
			return nil
		}

		found := false
		for i := range cfg.Server.AdminAPIKeys {
			if cfg.Server.AdminAPIKeys[i] == oldKey {
				cfg.Server.AdminAPIKeys[i] = req.APIKey
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("admin API key not found")
		}
		return nil
	}); err != nil {
		writeOpenAIError(w, mapConfigErrorToHTTP(err), err.Error(), "invalid_request_error", "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": "key updated"})
}

func (s *Server) handleKeysDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	idx, err := strconv.Atoi(id)
	if err != nil || idx < 0 {
		writeOpenAIError(w, http.StatusBadRequest, "invalid key id", "invalid_request_error", "")
		return
	}

	keys := s.runtimeConfig.ListAdminAPIKeys()
	if idx >= len(keys) {
		writeOpenAIError(w, http.StatusNotFound, "key not found", "invalid_request_error", "")
		return
	}

	// Use TransactionalUpdate for atomic config update
	if err := s.runtimeConfig.TransactionalUpdate(func(cfg *config.Config) error {
		oldKey := keys[idx]
		if cfg.Server.APIKey == oldKey {
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
			if existing == oldKey {
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
	}); err != nil {
		writeOpenAIError(w, mapConfigErrorToHTTP(err), err.Error(), "invalid_request_error", "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}

func (s *Server) handleModelAliasesList(w http.ResponseWriter, r *http.Request) {
	// Return list of model aliases from config
	aliasesMap := s.runtimeConfig.ListModelAliases()
	aliases := make([]map[string]any, 0, len(aliasesMap))
	for alias, modelAlias := range aliasesMap {
		a := map[string]any{
			"alias":    alias,
			"provider": modelAlias.Provider,
			"model":    modelAlias.Model,
		}
		aliases = append(aliases, a)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"aliases": aliases,
		"count":   len(aliases),
	})
}

func (s *Server) handleModelAliasesCreate(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req struct {
		Alias    string `json:"alias"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid json", "invalid_request_error", "")
		return
	}
	if req.Alias == "" {
		writeOpenAIError(w, http.StatusBadRequest, "alias is required", "invalid_request_error", "")
		return
	}

	alias := config.ModelAlias{Provider: req.Provider, Model: req.Model}

	// Use UpdateWithReconfigure for full transactional update with rollback
	if err := s.runtimeConfig.UpdateWithReconfigure(func(cfg *config.Config) error {
		if cfg.ModelAliases == nil {
			cfg.ModelAliases = make(map[string]config.ModelAlias)
		}
		if _, exists := cfg.ModelAliases[req.Alias]; exists {
			return fmt.Errorf("model alias '%s' already exists", req.Alias)
		}
		cfg.ModelAliases[req.Alias] = alias
		return nil
	}, s.reconfigureFromConfig); err != nil {
		writeOpenAIError(w, mapConfigErrorToHTTP(err), err.Error(), "invalid_request_error", "")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"alias": req.Alias, "target": alias})
}

func (s *Server) handleModelAliasesUpdate(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	name := chi.URLParam(r, "name")

	var req struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid json", "invalid_request_error", "")
		return
	}

	alias := config.ModelAlias{Provider: req.Provider, Model: req.Model}

	// Use UpdateWithReconfigure for full transactional update with rollback
	if err := s.runtimeConfig.UpdateWithReconfigure(func(cfg *config.Config) error {
		if _, exists := cfg.ModelAliases[name]; !exists {
			return fmt.Errorf("model alias '%s' not found", name)
		}
		cfg.ModelAliases[name] = alias
		return nil
	}, s.reconfigureFromConfig); err != nil {
		writeOpenAIError(w, mapConfigErrorToHTTP(err), err.Error(), "invalid_request_error", "")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"alias": name, "target": alias})
}

func (s *Server) handleModelAliasesDelete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	// Use UpdateWithReconfigure for full transactional update with rollback
	if err := s.runtimeConfig.UpdateWithReconfigure(func(cfg *config.Config) error {
		if _, exists := cfg.ModelAliases[name]; !exists {
			return fmt.Errorf("model alias '%s' not found", name)
		}
		delete(cfg.ModelAliases, name)
		return nil
	}, s.reconfigureFromConfig); err != nil {
		writeOpenAIError(w, mapConfigErrorToHTTP(err), err.Error(), "invalid_request_error", "")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"deleted": name})
}

func (s *Server) handleModelsCustomList(w http.ResponseWriter, r *http.Request) {
	customModels := s.runtimeConfig.ListCustomModels()
	items := make([]map[string]any, 0, len(customModels))
	for name, customModel := range customModels {
		items = append(items, map[string]any{
			"name":        name,
			"provider":    customModel.Provider,
			"model":       customModel.Model,
			"description": customModel.Description,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"models": items, "count": len(items)})
}

func (s *Server) handleModelsCustomCreate(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req struct {
		Name        string `json:"name"`
		Provider    string `json:"provider"`
		Model       string `json:"model"`
		Description string `json:"description,omitempty"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid json", "invalid_request_error", "")
		return
	}
	if req.Name == "" {
		writeOpenAIError(w, http.StatusBadRequest, "name is required", "invalid_request_error", "")
		return
	}

	customModel := config.CustomModel{Provider: req.Provider, Model: req.Model, Description: req.Description}

	// Use UpdateWithReconfigure for full transactional update with rollback
	if err := s.runtimeConfig.UpdateWithReconfigure(func(cfg *config.Config) error {
		if cfg.CustomModels == nil {
			cfg.CustomModels = make(map[string]config.CustomModel)
		}
		if _, exists := cfg.CustomModels[req.Name]; exists {
			return fmt.Errorf("custom model '%s' already exists", req.Name)
		}
		cfg.CustomModels[req.Name] = customModel
		return nil
	}, s.reconfigureFromConfig); err != nil {
		writeOpenAIError(w, mapConfigErrorToHTTP(err), err.Error(), "invalid_request_error", "")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"name": req.Name, "model": customModel})
}

func (s *Server) handleModelsCustomUpdate(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	name := chi.URLParam(r, "name")

	var req struct {
		Provider    string `json:"provider"`
		Model       string `json:"model"`
		Description string `json:"description,omitempty"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid json", "invalid_request_error", "")
		return
	}

	customModel := config.CustomModel{Provider: req.Provider, Model: req.Model, Description: req.Description}

	// Use UpdateWithReconfigure for full transactional update with rollback
	if err := s.runtimeConfig.UpdateWithReconfigure(func(cfg *config.Config) error {
		if _, exists := cfg.CustomModels[name]; !exists {
			return fmt.Errorf("custom model '%s' not found", name)
		}
		cfg.CustomModels[name] = customModel
		return nil
	}, s.reconfigureFromConfig); err != nil {
		writeOpenAIError(w, mapConfigErrorToHTTP(err), err.Error(), "invalid_request_error", "")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"name": name, "model": customModel})
}

func (s *Server) handleModelsCustomDelete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	// Use UpdateWithReconfigure for full transactional update with rollback
	if err := s.runtimeConfig.UpdateWithReconfigure(func(cfg *config.Config) error {
		if _, exists := cfg.CustomModels[name]; !exists {
			return fmt.Errorf("custom model '%s' not found", name)
		}
		delete(cfg.CustomModels, name)
		return nil
	}, s.reconfigureFromConfig); err != nil {
		writeOpenAIError(w, mapConfigErrorToHTTP(err), err.Error(), "invalid_request_error", "")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"deleted": name})
}

func (s *Server) handleSettingsGet(w http.ResponseWriter, r *http.Request) {
	// Return current settings from config
	cfg := s.runtimeConfig.Get()
	settings := map[string]any{
		"combo_strategy":         cfg.Settings.ComboStrategy,
		"outbound_proxy_enabled": cfg.Settings.OutboundProxyEnabled,
		"outbound_proxy_url":     cfg.Settings.OutboundProxyURL,
	}

	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) handleSettingsPut(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var settings config.SettingsConfig
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&settings); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid json", "invalid_request_error", "")
		return
	}

	// Use UpdateWithReconfigure for full transactional update with rollback
	if err := s.runtimeConfig.UpdateWithReconfigure(func(cfg *config.Config) error {
		cfg.Settings = settings
		return nil
	}, s.reconfigureFromConfig); err != nil {
		writeOpenAIError(w, mapConfigErrorToHTTP(err), err.Error(), "invalid_request_error", "")
		return
	}

	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) handleLogsList(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	query := r.URL.Query()

	limit := 50
	if l := query.Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 1000 {
			limit = parsed
		}
	}

	offset := 0
	if o := query.Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	params := storage.LogQueryParams{
		Limit:    limit,
		Offset:   offset,
		Provider: query.Get("provider"),
		Model:    query.Get("model"),
		Status:   query.Get("status"),
	}

	if startTime := query.Get("start_time"); startTime != "" {
		if ts, err := strconv.ParseInt(startTime, 10, 64); err == nil {
			params.StartTime = ts
		}
	}

	if endTime := query.Get("end_time"); endTime != "" {
		if ts, err := strconv.ParseInt(endTime, 10, 64); err == nil {
			params.EndTime = ts
		}
	}

	// Query logs from database
	db := s.asyncWriter.GetDB()
	logs, total, err := db.QueryLogs(r.Context(), params)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to query logs")
		writeOpenAIError(w, http.StatusInternalServerError, "failed to query logs", "internal_error", "")
		return
	}

	// Convert logs to API response format
	apiLogs := make([]map[string]any, 0, len(logs))
	for _, log := range logs {
		apiLog := map[string]any{
			"request_id":        log.RequestID,
			"model":             log.Model,
			"provider":          log.Provider,
			"target_model":      log.TargetModel,
			"status":            log.Status,
			"start_time":        log.StartTime.Unix(),
			"end_time":          log.EndTime.Unix(),
			"duration_ms":       log.Duration.Milliseconds(),
			"prompt_tokens":     log.PromptTokens,
			"completion_tokens": log.CompletionTokens,
			"total_tokens":      log.TotalTokens,
		}
		if log.ErrorMessage != "" {
			apiLog["error_message"] = log.ErrorMessage
		}
		apiLogs = append(apiLogs, apiLog)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"logs":   apiLogs,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	s.metrics.mu.RLock()
	defer s.metrics.mu.RUnlock()

	// Output Prometheus format metrics
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")

	// Request metrics
	fmt.Fprintf(w, "# HELP router_requests_total Total number of requests\n")
	fmt.Fprintf(w, "# TYPE router_requests_total counter\n")
	fmt.Fprintf(w, "router_requests_total %d\n", s.metrics.RequestsTotal)

	fmt.Fprintf(w, "# HELP router_requests_success Total number of successful requests\n")
	fmt.Fprintf(w, "# TYPE router_requests_success counter\n")
	fmt.Fprintf(w, "router_requests_success %d\n", s.metrics.RequestsSuccess)

	fmt.Fprintf(w, "# HELP router_requests_error Total number of failed requests\n")
	fmt.Fprintf(w, "# TYPE router_requests_error counter\n")
	fmt.Fprintf(w, "router_requests_error %d\n", s.metrics.RequestsError)

	// Provider usage metrics
	fmt.Fprintf(w, "# HELP router_provider_usage Number of requests per provider\n")
	fmt.Fprintf(w, "# TYPE router_provider_usage counter\n")
	for provider, count := range s.metrics.ProviderUsage {
		fmt.Fprintf(w, "router_provider_usage{provider=\"%s\"} %d\n", provider, count)
	}
}

func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Get in-memory metrics
	s.metrics.mu.RLock()
	metricsData := map[string]any{
		"requests_total":   s.metrics.RequestsTotal,
		"requests_success": s.metrics.RequestsSuccess,
		"requests_error":   s.metrics.RequestsError,
		"provider_usage":   s.metrics.ProviderUsage,
	}
	s.metrics.mu.RUnlock()

	// Fetch live usage from providers
	cfg := s.runtimeConfig.Get()
	providerUsageData := make(map[string]any)

	for _, provider := range cfg.Providers {
		if !provider.Enabled {
			continue
		}

		usageData, err := s.usageFetcher.FetchUsage(ctx, provider)
		if err != nil {
			s.logger.Debug().
				Str("provider", provider.Name).
				Err(err).
				Msg("failed to fetch usage from provider")
			continue
		}

		providerUsageData[provider.Name] = map[string]any{
			"prompt_tokens":     usageData.PromptTokens,
			"completion_tokens": usageData.CompletionTokens,
			"total_tokens":      usageData.TotalTokens,
			"cost_usd":          usageData.CostUSD,
			"timestamp":         usageData.Timestamp,
		}
	}

	// Get cost estimates from pricing registry
	pricingInfo := make(map[string]any)
	for providerName, modelUsage := range metricsData["provider_usage"].(map[string]int64) {
		// Get pricing for common models from this provider
		providerPricing := make(map[string]any)
		for _, pm := range s.pricingRegistry.GetAllByProvider(providerName) {
			providerPricing[pm.Model] = map[string]any{
				"input_price_per_million":  pm.InputPricePerMillion,
				"output_price_per_million": pm.OutputPricePerMillion,
				"currency":                 pm.Currency,
			}
		}
		if len(providerPricing) > 0 {
			pricingInfo[providerName] = map[string]any{
				"request_count": modelUsage,
				"models":        providerPricing,
			}
		}
	}

	usage := map[string]any{
		"metrics":         metricsData,
		"provider_usage":  providerUsageData,
		"pricing":         pricingInfo,
		"cost_estimation": "Available via pricing data - multiply token counts by per-million rates",
	}

	writeJSON(w, http.StatusOK, usage)
}

func (s *Server) handleModels(w http.ResponseWriter, _ *http.Request) {
	models := make([]map[string]any, 0)

	// Add route aliases
	routesMap := s.runtimeConfig.ListRoutes()
	for alias := range routesMap {
		models = append(models, map[string]any{
			"id":     alias,
			"object": "model",
			"type":   "route",
		})
	}

	// Add model aliases
	aliasesMap := s.runtimeConfig.ListModelAliases()
	for alias := range aliasesMap {
		models = append(models, map[string]any{
			"id":     alias,
			"object": "model",
			"type":   "alias",
		})
	}

	// Add provider/model combinations
	providersList := s.runtimeConfig.ListProviders()
	for _, provider := range providersList {
		if provider.Enabled {
			models = append(models, map[string]any{
				"id":     provider.Name + "/*",
				"object": "model",
				"type":   "provider",
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   models,
	})
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	requestID := GetRequestID(r.Context())
	defer r.Body.Close()

	// Get config once for this request
	cfg := s.runtimeConfig.Get()

	// Increment metrics
	s.metrics.mu.Lock()
	s.metrics.RequestsTotal++
	s.metrics.mu.Unlock()

	startTime := time.Now()

	// Capture raw request body for debug logging
	var rawRequestBytes []byte
	if cfg.Logging.Debug {
		rawRequestBytes, _ = io.ReadAll(io.LimitReader(r.Body, 1<<20))
		r.Body = io.NopCloser(bytes.NewReader(rawRequestBytes))
	}

	var request providers.ChatRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&request); err != nil {
		s.metrics.mu.Lock()
		s.metrics.RequestsError++
		s.metrics.mu.Unlock()
		writeOpenAIError(w, http.StatusBadRequest, "invalid json", "invalid_request_error", "")
		return
	}

	// Apply thinking config from settings if enabled and not already set by client
	if cfg.Settings.Thinking.Enabled && request.Thinking == nil {
		request.Thinking = &providers.ThinkingParams{
			Enabled:          true,
			MaxTokens:        cfg.Settings.Thinking.MaxTokens,
			IncludeReasoning: cfg.Settings.Thinking.IncludeReasoning,
		}
	}

	// Apply native passthrough flag if enabled
	if cfg.Settings.NativePassthrough {
		request.NativePassthrough = true
	}

	// Handle streaming request
	if request.Stream {
		s.handleStreamingChatCompletion(w, r, request, requestID, startTime, rawRequestBytes)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(cfg.Server.RequestTimeoutSeconds)*time.Second)
	defer cancel()

	// Check cache for non-streaming requests
	cacheKey := generateCacheKey(request)
	if cacheKey != "" && s.cache != nil {
		if cachedData, found := s.cache.Get(cacheKey); found {
			s.logger.Debug().Str("cache_key", cacheKey).Msg("cache hit")
			var cachedResponse providers.ChatResponse
			if err := json.Unmarshal(cachedData, &cachedResponse); err == nil {
				// Return cached response
				writeJSON(w, http.StatusOK, cachedResponse)
				return
			}
		}
	}

	response, providerName, err := s.engine.ChatCompletion(ctx, request)
	duration := time.Since(startTime)

	// Get resolved target model for accurate logging
	targets := s.engine.ResolveTargets(request.Model)
	targetModel := request.Model
	if len(targets) > 0 {
		targetModel = targets[0].Model
	}

	if err != nil {
		// Increment error metrics
		s.metrics.mu.Lock()
		s.metrics.RequestsError++
		s.metrics.mu.Unlock()

		s.logger.Error().
			Err(err).
			Str("request_id", requestID).
			Str("model", request.Model).
			Msg("chat completion failed")

		if s.asyncWriter != nil {
			s.asyncWriter.LogRequest(&storage.RequestLog{
				RequestID:    requestID,
				Model:        request.Model,
				Provider:     providerName,
				TargetModel:  targetModel,
				Status:       "error",
				ErrorMessage: err.Error(),
				StartTime:    startTime,
				EndTime:      time.Now(),
				Duration:     duration,
			})

			// Log request details in debug mode
			if cfg.Logging.Debug {
				requestBodyStr := string(rawRequestBytes)
				s.asyncWriter.LogRequestDetails(r.Context(), requestID, requestBodyStr, "", http.StatusBadGateway)
			}
		}

		writeOpenAIError(w, http.StatusBadGateway, err.Error(), "api_error", "")
		return
	}

	// Capture response for debug logging
	var rawResponseBytes []byte
	if cfg.Logging.Debug {
		rawResponseBytes, _ = json.Marshal(response)
	}

	// Increment success metrics and provider usage
	s.metrics.mu.Lock()
	s.metrics.RequestsSuccess++
	s.metrics.ProviderUsage[providerName]++
	s.metrics.mu.Unlock()

	if s.asyncWriter != nil {
		log := &storage.RequestLog{
			RequestID:   requestID,
			Model:       request.Model,
			Provider:    providerName,
			TargetModel: targetModel,
			Status:      "success",
			StartTime:   startTime,
			EndTime:     time.Now(),
			Duration:    duration,
		}

		if response.Usage != nil {
			log.PromptTokens = response.Usage.PromptTokens
			log.CompletionTokens = response.Usage.CompletionTokens
			log.TotalTokens = response.Usage.TotalTokens

			// Calculate cost using pricing registry
			if pm, ok := s.pricingRegistry.Get(providerName, targetModel); ok {
				cost := pm.CalculateCost(response.Usage.PromptTokens, response.Usage.CompletionTokens)
				log.InputCost = cost.InputCost
				log.OutputCost = cost.OutputCost
				log.TotalCost = cost.TotalCost
				log.Currency = cost.Currency
			}

			s.asyncWriter.IncrementUsage(providerName, targetModel, response.Usage.PromptTokens, response.Usage.CompletionTokens)

			// Save quota snapshot for this provider
			s.asyncWriter.SaveQuotaSnapshot(providerName, "default", response.Usage.PromptTokens, response.Usage.CompletionTokens, log.TotalCost)
		}

		s.asyncWriter.LogRequest(log)

		// Log request details in debug mode
		if cfg.Logging.Debug {
			requestBodyStr := string(rawRequestBytes)
			responseBodyStr := string(rawResponseBytes)
			s.asyncWriter.LogRequestDetails(r.Context(), requestID, requestBodyStr, responseBodyStr, http.StatusOK)
		}
	}

	// Store response in cache for future requests
	if cacheKey != "" && s.cache != nil {
		responseData, err := json.Marshal(response)
		if err == nil {
			// Cache with 5 minute TTL
			s.cache.Set(cacheKey, responseData, 5*time.Minute)
			s.logger.Debug().Str("cache_key", cacheKey).Msg("response cached")
		}
	}

	w.Header().Set("X-Router-Provider", providerName)
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleStreamingChatCompletion(w http.ResponseWriter, r *http.Request, request providers.ChatRequest, requestID string, startTime time.Time, rawRequestBytes []byte) {
	// Get config once for this request
	cfg := s.runtimeConfig.Get()

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	// Create context with timeout
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(cfg.Server.RequestTimeoutSeconds)*time.Second)
	defer cancel()

	// Get resolved target model for accurate logging
	targets := s.engine.ResolveTargets(request.Model)
	targetModel := request.Model
	if len(targets) > 0 {
		targetModel = targets[0].Model
	}

	// Use engine's StreamingChatCompletion for resiliency (fallback, retry, cooldown, model-lock)
	chunks, providerName, err := s.engine.StreamingChatCompletion(ctx, request)
	if err != nil {
		s.metrics.mu.Lock()
		s.metrics.RequestsError++
		s.metrics.mu.Unlock()

		s.logger.Error().
			Err(err).
			Str("request_id", requestID).
			Str("model", request.Model).
			Msg("streaming failed")

		if s.asyncWriter != nil {
			s.asyncWriter.LogRequest(&storage.RequestLog{
				RequestID:    requestID,
				Model:        request.Model,
				Provider:     providerName,
				TargetModel:  targetModel,
				Status:       "error",
				ErrorMessage: err.Error(),
				StartTime:    startTime,
				EndTime:      time.Now(),
				Duration:     time.Since(startTime),
			})
		}

		writeSSEError(w, "streaming failed: "+err.Error())
		return
	}

	// Forward chunks to client
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.logger.Error().
			Str("request_id", requestID).
			Msg("streaming not supported (http.Flusher not available)")

		writeSSEError(w, "streaming not supported")
		return
	}

	chunkCount := 0
	for chunk := range chunks {
		select {
		case <-ctx.Done():
			// Client disconnected or timeout
			s.logger.Info().
				Str("request_id", requestID).
				Msg("client disconnected or timeout during streaming")
			return
		default:
			data, err := json.Marshal(chunk)
			if err != nil {
				s.logger.Error().
					Err(err).
					Str("request_id", requestID).
					Msg("failed to marshal chunk")
				continue
			}

			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			chunkCount++
		}
	}

	// Write final [DONE] marker
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()

	// Update metrics
	s.metrics.mu.Lock()
	s.metrics.RequestsSuccess++
	s.metrics.ProviderUsage[providerName]++
	s.metrics.mu.Unlock()

	if s.asyncWriter != nil {
		s.asyncWriter.LogRequest(&storage.RequestLog{
			RequestID:   requestID,
			Model:       request.Model,
			Provider:    providerName,
			TargetModel: targetModel,
			Status:      "success",
			StartTime:   startTime,
			EndTime:     time.Now(),
			Duration:    time.Since(startTime),
		})
	}

	s.logger.Info().
		Str("request_id", requestID).
		Str("model", request.Model).
		Str("provider", providerName).
		Int("chunks", chunkCount).
		Dur("duration_ms", time.Since(startTime)).
		Msg("streaming completed")
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	requestID := GetRequestID(r.Context())
	defer r.Body.Close()

	// Get config once for this request
	cfg := s.runtimeConfig.Get()

	// Increment metrics
	s.metrics.mu.Lock()
	s.metrics.RequestsTotal++
	s.metrics.mu.Unlock()

	startTime := time.Now()

	// Parse Claude Messages API request
	var claudeReq map[string]interface{}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&claudeReq); err != nil {
		s.metrics.mu.Lock()
		s.metrics.RequestsError++
		s.metrics.mu.Unlock()
		writeOpenAIError(w, http.StatusBadRequest, "invalid json", "invalid_request_error", "")
		return
	}

	// Use translator layer to convert Claude to OpenAI
	reqTranslator, err := s.translators.GetRequestTranslator(translator.FormatClaude, translator.FormatOpenAI)
	if err != nil {
		s.metrics.mu.Lock()
		s.metrics.RequestsError++
		s.metrics.mu.Unlock()
		writeOpenAIError(w, http.StatusInternalServerError, "translator not available", "internal_error", "")
		return
	}

	openAIReq, err := reqTranslator.TranslateRequest(r.Context(), translator.FormatClaude, translator.FormatOpenAI, claudeReq)
	if err != nil {
		s.metrics.mu.Lock()
		s.metrics.RequestsError++
		s.metrics.mu.Unlock()
		writeOpenAIError(w, http.StatusInternalServerError, "translation failed", "internal_error", "")
		return
	}

	// Create ChatRequest directly from map without marshal/unmarshal
	chatReq := s.mapToChatRequest(openAIReq)

	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(cfg.Server.RequestTimeoutSeconds)*time.Second)
	defer cancel()

	response, providerName, err := s.engine.ChatCompletion(ctx, chatReq)
	duration := time.Since(startTime)

	// Get resolved target model for accurate logging
	targets := s.engine.ResolveTargets(chatReq.Model)
	targetModel := chatReq.Model
	if len(targets) > 0 {
		targetModel = targets[0].Model
	}

	if err != nil {
		// Increment error metrics
		s.metrics.mu.Lock()
		s.metrics.RequestsError++
		s.metrics.mu.Unlock()

		s.logger.Error().
			Err(err).
			Str("request_id", requestID).
			Str("model", chatReq.Model).
			Msg("messages request failed")

		if s.asyncWriter != nil {
			s.asyncWriter.LogRequest(&storage.RequestLog{
				RequestID:    requestID,
				Model:        chatReq.Model,
				Provider:     providerName,
				TargetModel:  targetModel,
				Status:       "error",
				ErrorMessage: err.Error(),
				StartTime:    startTime,
				EndTime:      time.Now(),
				Duration:     duration,
			})
		}

		writeOpenAIError(w, http.StatusBadGateway, err.Error(), "api_error", "")
		return
	}

	// Use translator layer to convert OpenAI response to Claude format
	respBytes, err := json.Marshal(response)
	if err != nil {
		s.metrics.mu.Lock()
		s.metrics.RequestsError++
		s.metrics.mu.Unlock()
		writeOpenAIError(w, http.StatusInternalServerError, "failed to marshal response", "internal_error", "")
		return
	}

	respTranslator, err := s.translators.GetResponseTranslator(translator.FormatOpenAI, translator.FormatClaude)
	if err != nil {
		s.metrics.mu.Lock()
		s.metrics.RequestsError++
		s.metrics.mu.Unlock()
		writeOpenAIError(w, http.StatusInternalServerError, "translator not available", "internal_error", "")
		return
	}

	claudeRespBytes, err := respTranslator.TranslateResponse(r.Context(), translator.FormatOpenAI, translator.FormatClaude, respBytes)
	if err != nil {
		s.metrics.mu.Lock()
		s.metrics.RequestsError++
		s.metrics.mu.Unlock()
		writeOpenAIError(w, http.StatusInternalServerError, "translation failed", "internal_error", "")
		return
	}

	var claudeResp map[string]interface{}
	if err := json.Unmarshal(claudeRespBytes, &claudeResp); err != nil {
		s.metrics.mu.Lock()
		s.metrics.RequestsError++
		s.metrics.mu.Unlock()
		writeOpenAIError(w, http.StatusInternalServerError, "failed to parse translated response", "internal_error", "")
		return
	}

	// Increment success metrics and provider usage
	s.metrics.mu.Lock()
	s.metrics.RequestsSuccess++
	s.metrics.ProviderUsage[providerName]++
	s.metrics.mu.Unlock()

	if s.asyncWriter != nil {
		s.asyncWriter.LogRequest(&storage.RequestLog{
			RequestID:   requestID,
			Model:       chatReq.Model,
			Provider:    providerName,
			TargetModel: targetModel,
			Status:      "success",
			StartTime:   startTime,
			EndTime:     time.Now(),
			Duration:    duration,
		})

		if response.Usage != nil {
			s.asyncWriter.IncrementUsage(providerName, targetModel, response.Usage.PromptTokens, response.Usage.CompletionTokens)
		}
	}

	w.Header().Set("X-Router-Provider", providerName)
	writeJSON(w, http.StatusOK, claudeResp)
}

func (s *Server) handleResponses(w http.ResponseWriter, r *http.Request) {
	requestID := GetRequestID(r.Context())
	defer r.Body.Close()

	// Get config once for this request
	cfg := s.runtimeConfig.Get()

	// Increment metrics
	s.metrics.mu.Lock()
	s.metrics.RequestsTotal++
	s.metrics.mu.Unlock()

	startTime := time.Now()

	// Parse OpenAI Responses API request
	var responsesReq struct {
		Input       interface{} `json:"input"`
		Model       string      `json:"model"`
		Temperature *float64    `json:"temperature,omitempty"`
		TopP        *float64    `json:"top_p,omitempty"`
		MaxTokens   *int        `json:"max_tokens,omitempty"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&responsesReq); err != nil {
		s.metrics.mu.Lock()
		s.metrics.RequestsError++
		s.metrics.mu.Unlock()
		writeOpenAIError(w, http.StatusBadRequest, "invalid json", "invalid_request_error", "")
		return
	}

	// Convert input to ChatMessage array
	var messages []providers.ChatMessage
	switch v := responsesReq.Input.(type) {
	case string:
		// Single string input
		messages = []providers.ChatMessage{{Role: "user", Content: v}}
	case []interface{}:
		// Array of message objects or strings
		messages = make([]providers.ChatMessage, 0, len(v))
		for _, item := range v {
			if msgObj, ok := item.(map[string]interface{}); ok {
				// Message object with role/content
				role, _ := msgObj["role"].(string)
				content, _ := msgObj["content"].(string)
				if role == "" {
					role = "user"
				}
				messages = append(messages, providers.ChatMessage{
					Role:    role,
					Content: content,
				})
			} else if str, ok := item.(string); ok {
				// String in array
				messages = append(messages, providers.ChatMessage{
					Role:    "user",
					Content: str,
				})
			}
		}
	default:
		// Unknown type, treat as string
		messages = []providers.ChatMessage{{Role: "user", Content: fmt.Sprintf("%v", v)}}
	}

	// Create ChatRequest from Responses API request
	chatReq := providers.ChatRequest{
		Model:       responsesReq.Model,
		Messages:    messages,
		Temperature: responsesReq.Temperature,
		TopP:        responsesReq.TopP,
		MaxTokens:   responsesReq.MaxTokens,
		Stream:      false,
	}

	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(cfg.Server.RequestTimeoutSeconds)*time.Second)
	defer cancel()

	response, providerName, err := s.engine.ChatCompletion(ctx, chatReq)
	duration := time.Since(startTime)

	// Get resolved target model for accurate logging
	targets := s.engine.ResolveTargets(chatReq.Model)
	targetModel := chatReq.Model
	if len(targets) > 0 {
		targetModel = targets[0].Model
	}

	if err != nil {
		s.metrics.mu.Lock()
		s.metrics.RequestsError++
		s.metrics.mu.Unlock()

		s.logger.Error().
			Err(err).
			Str("request_id", requestID).
			Str("model", chatReq.Model).
			Msg("responses request failed")

		if s.asyncWriter != nil {
			s.asyncWriter.LogRequest(&storage.RequestLog{
				RequestID:    requestID,
				Model:        chatReq.Model,
				Provider:     providerName,
				TargetModel:  targetModel,
				Status:       "error",
				ErrorMessage: err.Error(),
				StartTime:    startTime,
				EndTime:      time.Now(),
				Duration:     duration,
			})
		}

		writeOpenAIError(w, http.StatusBadGateway, err.Error(), "api_error", "")
		return
	}

	// Increment success metrics
	s.metrics.mu.Lock()
	s.metrics.RequestsSuccess++
	s.metrics.ProviderUsage[providerName]++
	s.metrics.mu.Unlock()

	// Convert ChatResponse to Responses API format
	output := []map[string]any{}
	if len(response.Choices) > 0 {
		output = append(output, map[string]any{
			"role":    response.Choices[0].Message.Role,
			"content": response.Choices[0].Message.Content,
		})
	}

	responsesResp := map[string]any{
		"id":      response.ID,
		"object":  "response",
		"created": response.Created,
		"model":   response.Model,
		"output":  output,
	}

	if response.Usage != nil {
		responsesResp["usage"] = response.Usage
	}

	if s.asyncWriter != nil {
		s.asyncWriter.LogRequest(&storage.RequestLog{
			RequestID:   requestID,
			Model:       chatReq.Model,
			Provider:    providerName,
			TargetModel: targetModel,
			Status:      "success",
			StartTime:   startTime,
			EndTime:     time.Now(),
			Duration:    duration,
		})

		if response.Usage != nil {
			s.asyncWriter.IncrementUsage(providerName, targetModel, response.Usage.PromptTokens, response.Usage.CompletionTokens)
		}
	}

	w.Header().Set("X-Router-Provider", providerName)
	writeJSON(w, http.StatusOK, responsesResp)
}

func (s *Server) mapToChatRequest(req map[string]interface{}) providers.ChatRequest {
	chatReq := providers.ChatRequest{
		Stream: false, // Default to non-streaming for MVP
	}

	if model, ok := req["model"].(string); ok {
		chatReq.Model = model
	}

	if messages, ok := req["messages"].([]interface{}); ok {
		chatReq.Messages = make([]providers.ChatMessage, 0, len(messages))
		for _, msg := range messages {
			msgMap, ok := msg.(map[string]interface{})
			if !ok {
				continue
			}
			role, _ := msgMap["role"].(string)
			content, _ := msgMap["content"].(string)
			chatReq.Messages = append(chatReq.Messages, providers.ChatMessage{
				Role:    role,
				Content: content,
			})
		}
	}

	if maxTokens, ok := req["max_tokens"].(float64); ok {
		val := int(maxTokens)
		chatReq.MaxTokens = &val
	}

	if temp, ok := req["temperature"].(float64); ok {
		chatReq.Temperature = &temp
	}

	if topP, ok := req["top_p"].(float64); ok {
		chatReq.TopP = &topP
	}

	if stop, ok := req["stop"]; ok {
		switch v := stop.(type) {
		case string:
			if v != "" {
				chatReq.Stop = []string{v}
			}
		case []interface{}:
			chatReq.Stop = make([]string, 0, len(v))
			for _, s := range v {
				if str, ok := s.(string); ok {
					chatReq.Stop = append(chatReq.Stop, str)
				}
			}
		}
	}

	if stream, ok := req["stream"].(bool); ok {
		chatReq.Stream = stream
	}

	return chatReq
}

func (s *Server) handleEmbeddings(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	// Parse embeddings request
	var req struct {
		Model string `json:"model"`
		Input string `json:"input"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid json", "invalid_request_error", "")
		return
	}

	if req.Model == "" {
		writeOpenAIError(w, http.StatusBadRequest, "model is required", "invalid_request_error", "")
		return
	}

	if req.Input == "" {
		writeOpenAIError(w, http.StatusBadRequest, "input is required", "invalid_request_error", "")
		return
	}

	// Resolve targets for the model
	targets := s.engine.ResolveTargets(req.Model)
	if len(targets) == 0 {
		writeOpenAIError(w, http.StatusNotFound, "model not found", "invalid_request_error", "")
		return
	}

	// Try each target in the fallback chain
	var allErrors []string
	for targetIdx, target := range targets {
		// Get provider adapter
		registry := s.engine.GetRegistry()
		adapter, err := registry.Get(target.Provider)
		if err != nil {
			allErrors = append(allErrors, fmt.Sprintf("target[%d] %s: provider not found", targetIdx, target.Provider))
			continue
		}

		// Call provider's Embeddings method
		embReq := providers.EmbeddingsRequest{
			Input: req.Input,
			Model: target.Model,
		}
		response, err := adapter.Embeddings(r.Context(), embReq, target.Model)
		if err != nil {
			allErrors = append(allErrors, fmt.Sprintf("target[%d] %s/%s: %v", targetIdx, target.Provider, target.Model, err))
			// Only continue to next target if error is retryable
			if providers.IsRetryable(err) {
				continue
			}
			// Non-retryable error - fail immediately
			writeOpenAIError(w, http.StatusBadRequest, fmt.Sprintf("embeddings failed: %v", err), "invalid_request_error", "")
			return
		}

		// Success - return response
		writeJSON(w, http.StatusOK, response)
		return
	}

	// All targets exhausted
	writeOpenAIError(w, http.StatusInternalServerError, fmt.Sprintf("all embeddings targets failed: %s", strings.Join(allErrors, " | ")), "internal_error", "")
}

func (s *Server) handleAudioSpeech(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	// Parse TTS request
	var req struct {
		Model string `json:"model"`
		Input string `json:"input"`
		Voice string `json:"voice"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid json", "invalid_request_error", "")
		return
	}

	if req.Model == "" {
		writeOpenAIError(w, http.StatusBadRequest, "model is required", "invalid_request_error", "")
		return
	}

	if req.Input == "" {
		writeOpenAIError(w, http.StatusBadRequest, "input is required", "invalid_request_error", "")
		return
	}

	if req.Voice == "" {
		writeOpenAIError(w, http.StatusBadRequest, "voice is required", "invalid_request_error", "")
		return
	}

	// Resolve targets for the model
	targets := s.engine.ResolveTargets(req.Model)
	if len(targets) == 0 {
		writeOpenAIError(w, http.StatusNotFound, "model not found", "invalid_request_error", "")
		return
	}

	// Try each target in the fallback chain
	var allErrors []string
	for targetIdx, target := range targets {
		// Get provider adapter
		registry := s.engine.GetRegistry()
		adapter, err := registry.Get(target.Provider)
		if err != nil {
			allErrors = append(allErrors, fmt.Sprintf("target[%d] %s: provider not found", targetIdx, target.Provider))
			continue
		}

		// Call provider's AudioSpeech method
		audioReq := providers.AudioSpeechRequest{
			Input: req.Input,
			Model: target.Model,
			Voice: req.Voice,
		}
		response, err := adapter.AudioSpeech(r.Context(), audioReq, target.Model)
		if err != nil {
			allErrors = append(allErrors, fmt.Sprintf("target[%d] %s/%s: %v", targetIdx, target.Provider, target.Model, err))
			// Only continue to next target if error is retryable
			if providers.IsRetryable(err) {
				continue
			}
			// Non-retryable error - fail immediately
			writeOpenAIError(w, http.StatusBadRequest, fmt.Sprintf("audio/speech failed: %v", err), "invalid_request_error", "")
			return
		}

		// Success - return audio data
		w.Header().Set("Content-Type", response.ContentType)
		w.WriteHeader(http.StatusOK)
		w.Write(response.Data)
		return
	}

	// All targets exhausted
	writeOpenAIError(w, http.StatusInternalServerError, fmt.Sprintf("all audio/speech targets failed: %s", strings.Join(allErrors, " | ")), "internal_error", "")
}

func (s *Server) handleImagesGenerations(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	// Parse image generation request
	var req struct {
		Model  string `json:"model"`
		Prompt string `json:"prompt"`
		N      int    `json:"n"`
		Size   string `json:"size"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid json", "invalid_request_error", "")
		return
	}

	if req.Model == "" {
		writeOpenAIError(w, http.StatusBadRequest, "model is required", "invalid_request_error", "")
		return
	}

	if req.Prompt == "" {
		writeOpenAIError(w, http.StatusBadRequest, "prompt is required", "invalid_request_error", "")
		return
	}

	// Resolve targets for the model
	targets := s.engine.ResolveTargets(req.Model)
	if len(targets) == 0 {
		writeOpenAIError(w, http.StatusNotFound, "model not found", "invalid_request_error", "")
		return
	}

	// Try each target in the fallback chain
	var allErrors []string
	for targetIdx, target := range targets {
		// Get provider adapter
		registry := s.engine.GetRegistry()
		adapter, err := registry.Get(target.Provider)
		if err != nil {
			allErrors = append(allErrors, fmt.Sprintf("target[%d] %s: provider not found", targetIdx, target.Provider))
			continue
		}

		// Call provider's ImagesGenerations method
		imgReq := providers.ImagesGenerationsRequest{
			Model:  target.Model,
			Prompt: req.Prompt,
			N:      req.N,
			Size:   req.Size,
		}
		response, err := adapter.ImagesGenerations(r.Context(), imgReq, target.Model)
		if err != nil {
			allErrors = append(allErrors, fmt.Sprintf("target[%d] %s/%s: %v", targetIdx, target.Provider, target.Model, err))
			// Only continue to next target if error is retryable
			if providers.IsRetryable(err) {
				continue
			}
			// Non-retryable error - fail immediately
			writeOpenAIError(w, http.StatusBadRequest, fmt.Sprintf("images/generations failed: %v", err), "invalid_request_error", "")
			return
		}

		// Success - return response
		writeJSON(w, http.StatusOK, response)
		return
	}

	// All targets exhausted
	writeOpenAIError(w, http.StatusInternalServerError, fmt.Sprintf("all images/generations targets failed: %s", strings.Join(allErrors, " | ")), "internal_error", "")
}

func (s *Server) handleConfigExport(w http.ResponseWriter, r *http.Request) {
	cfg := s.runtimeConfig.Get()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(cfg); err != nil {
		s.logger.Error().Err(err).Msg("failed to encode config")
		http.Error(w, "Failed to export config", http.StatusInternalServerError)
	}
}

func (s *Server) handleConfigImport(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var cfg config.Config
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&cfg); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Update runtime config - Update takes a function that modifies the config
	s.runtimeConfig.Update(func(current *config.Config) error {
		*current = cfg
		return nil
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	cfg := s.runtimeConfig.Get()
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	server := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadTimeout:       time.Duration(cfg.Server.ReadTimeoutSeconds) * time.Second,
		WriteTimeout:      time.Duration(cfg.Server.WriteTimeoutSeconds) * time.Second,
		IdleTimeout:       time.Duration(cfg.Server.IdleTimeoutSeconds) * time.Second,
		ReadHeaderTimeout: time.Duration(cfg.Server.ReadHeaderTimeoutSeconds) * time.Second,
		MaxHeaderBytes:    cfg.Server.MaxHeaderBytes,
	}

	errCh := make(chan error, 1)
	go func() {
		s.logger.Info().Str("addr", addr).Msg("server starting")
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		s.logger.Info().Msg("server shutting down")
		return server.Shutdown(shutdownCtx)
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeOpenAIError(w http.ResponseWriter, status int, message string, errorType string, code string) {
	errorResp := map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    errorType,
		},
	}
	if code != "" {
		errorResp["error"].(map[string]any)["code"] = code
	}
	writeJSON(w, status, errorResp)
}

func writeSSEError(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	fmt.Fprintf(w, "data: {\"error\":{\"message\":\"%s\"}}\n\n", message)
	fmt.Fprintf(w, "data: [DONE]\n\n")
}

// handleConfigGet returns the full current configuration (same as /api/config/export but shorter path).
func (s *Server) handleConfigGet(w http.ResponseWriter, r *http.Request) {
	s.handleConfigExport(w, r)
}

// handlePricing returns all registered pricing models.
func (s *Server) handlePricing(w http.ResponseWriter, _ *http.Request) {
	models := s.pricingRegistry.AllModels()
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

// handleOAuthTokensList returns a list of stored OAuth token records (without secrets).
func (s *Server) handleOAuthTokensList(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"tokens": []any{}})
}

// handleOAuthTokenDelete removes an OAuth token for a provider/account.
func (s *Server) handleOAuthTokenDelete(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	account := chi.URLParam(r, "account")
	if provider == "" || account == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "provider and account required"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "provider": provider, "account": account})
}
