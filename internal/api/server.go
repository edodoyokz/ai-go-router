package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/edodoyokz/ai-go-router/internal/cache"
	"github.com/edodoyokz/ai-go-router/internal/config"
	"github.com/edodoyokz/ai-go-router/internal/i18n"
	"github.com/edodoyokz/ai-go-router/internal/mitm"
	"github.com/edodoyokz/ai-go-router/internal/nodes"
	"github.com/edodoyokz/ai-go-router/internal/oauth"
	"github.com/edodoyokz/ai-go-router/internal/policy"
	"github.com/edodoyokz/ai-go-router/internal/providers"
	"github.com/edodoyokz/ai-go-router/internal/providers/catalog"
	"github.com/edodoyokz/ai-go-router/internal/providers/validation"
	routing "github.com/edodoyokz/ai-go-router/internal/router"
	"github.com/edodoyokz/ai-go-router/internal/storage"
	cloudsync "github.com/edodoyokz/ai-go-router/internal/sync"
	"github.com/edodoyokz/ai-go-router/internal/translator"
	"github.com/edodoyokz/ai-go-router/internal/tunnel"
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
	nodeRegistry    *nodes.Registry
	syncManager     *cloudsync.Manager
	tunnelManager   *tunnel.Manager
	mitmManager     *mitm.Manager
	policyEngine    *policy.Engine
	i18nBundle      *i18n.Bundle
	providerCheck   *validation.Service
	oauthStore      *oauth.Store
	translatorLogMu sync.RWMutex
	translatorLogs  []map[string]any
}

var githubOAuthEndpoints = struct {
	ClientID        string
	DeviceCodeURL   string
	TokenURL        string
	UserInfoURL     string
	CopilotTokenURL string
}{
	ClientID:        "Iv1.b507a08c87ecfe98",
	DeviceCodeURL:   "https://github.com/login/device/code",
	TokenURL:        "https://github.com/login/oauth/access_token",
	UserInfoURL:     "https://api.github.com/user",
	CopilotTokenURL: "https://api.github.com/copilot_internal/v2/token",
}

var qwenOAuthEndpoints = struct {
	ClientID      string
	DeviceCodeURL string
	TokenURL      string
	Scope         string
}{
	ClientID:      "f0304373b74a44d2b584a3fb70ca9e56",
	DeviceCodeURL: "https://qwen.ai/api/v1/oauth2/device/code",
	TokenURL:      "https://qwen.ai/api/v1/oauth2/token",
	Scope:         "openid profile email model.completion",
}

var oauthProviderRegistry = oauth.DefaultProviderRegistry()

var kiroOAuthEndpoints = struct {
	SocialLoginURL   string
	SocialTokenURL   string
	SocialRefreshURL string
}{
	SocialLoginURL:   "https://prod.us-east-1.auth.desktop.kiro.dev/login",
	SocialTokenURL:   "https://prod.us-east-1.auth.desktop.kiro.dev/oauth/token",
	SocialRefreshURL: "https://prod.us-east-1.auth.desktop.kiro.dev/refreshToken",
}

var codexOAuthEndpoints = struct {
	ClientID string
	AuthURL  string
	TokenURL string
}{
	ClientID: "app_EMoamEEZ73f0CkXaXp7hrann",
	AuthURL:  "https://auth.openai.com/oauth/authorize",
	TokenURL: "https://auth.openai.com/oauth/token",
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

	// Initialize policy engine from config
	cfg := runtimeConfig.Get()
	policyRules := make([]policy.Policy, len(cfg.Policies))
	for i, r := range cfg.Policies {
		policyRules[i] = policy.Policy{
			Name:          r.Name,
			MatchModel:    r.MatchModel,
			MatchProvider: r.MatchProvider,
			MatchAPIKey:   r.MatchAPIKey,
			Action:        policy.Action(r.Action),
			RerouteModel:  r.RerouteModel,
			DenyMessage:   r.DenyMessage,
			Tag:           r.Tag,
		}
	}

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
		mitmManager:     mitm.NewManager(nil),
		policyEngine:    policy.NewEngine(policyRules),
		i18nBundle:      i18n.NewBundle(),
		providerCheck:   validation.NewService(),
	}
}

// SetNodeRegistry sets the node registry after construction (called from app.go).
func (s *Server) SetNodeRegistry(reg *nodes.Registry) {
	s.nodeRegistry = reg
}

// SetSyncManager sets the sync manager after construction (called from app.go).
func (s *Server) SetSyncManager(mgr *cloudsync.Manager) {
	s.syncManager = mgr
}

// SetTunnelManager sets the tunnel manager after construction (called from app.go).
func (s *Server) SetTunnelManager(mgr *tunnel.Manager) {
	s.tunnelManager = mgr
}

func (s *Server) SetMITMManager(mgr *mitm.Manager) {
	s.mitmManager = mgr
}

// SetOAuthStore sets the OAuth token store after construction (called from app.go).
func (s *Server) SetOAuthStore(store *oauth.Store) {
	s.oauthStore = store
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
	r.Handle("/", http.RedirectHandler("/ui/", http.StatusMovedPermanently))
	r.Handle("/ui", http.RedirectHandler("/ui/", http.StatusMovedPermanently))
	r.Handle("/ui/*", webui.Handler())
	r.Handle("/dashboard", http.RedirectHandler("/ui/", http.StatusMovedPermanently))

	// Public routes (no auth required)
	r.Get("/healthz", s.handleHealthz)
	r.Get("/api/health", s.handleHealthz)
	r.Get("/readyz", s.handleReadyz)
	r.Get("/metrics", s.handleMetrics)
	r.Get("/api/setup/status", s.handleSetupStatus)

	// Protected routes (auth required)
	r.Group(func(r chi.Router) {
		r.Use(AuthMiddlewareWithRuntimeConfig(s.runtimeConfig))
		r.Get("/api/init", s.handleInit)
		r.Post("/api/auth/login", s.handleAuthLogin)
		r.Post("/api/auth/logout", s.handleAuthLogout)
		r.Post("/api/shutdown", s.handleShutdown)
		r.Get("/v1/models", s.handleModels)
		r.Get("/api/v1/models", s.handleModels)
		r.Get("/api/v1", s.handleV1Root)
		r.Post("/v1/chat/completions", s.handleChatCompletions)
		r.Post("/api/v1/chat/completions", s.handleChatCompletions)
		r.Post("/v1/messages", s.handleMessages)
		r.Post("/api/v1/messages", s.handleMessages)
		r.Post("/v1/messages/count_tokens", s.handleMessagesCountTokens)
		r.Post("/api/v1/messages/count_tokens", s.handleMessagesCountTokens)
		r.Post("/v1/responses", s.handleResponses)
		r.Post("/api/v1/responses", s.handleResponses)
		r.Post("/v1/responses/compact", s.handleResponses)
		r.Post("/api/v1/responses/compact", s.handleResponses)
		r.Post("/v1/embeddings", s.handleEmbeddings)
		r.Post("/api/v1/embeddings", s.handleEmbeddings)
		r.Post("/v1/api/chat", s.handleOllamaChat)
		r.Post("/api/v1/api/chat", s.handleOllamaChat)
		r.Get("/v1beta/models", s.handleModels)
		r.Get("/api/v1beta/models", s.handleModels)
		r.Get("/v1beta/models/{model}", s.handleModelGet)
		r.Get("/api/v1beta/models/{model}", s.handleModelGet)
		r.Post("/v1beta/models/{path:.*}", s.handleV1BetaModelPost)
		r.Post("/api/v1beta/models/{path:.*}", s.handleV1BetaModelPost)
		r.Post("/codex/v1/responses", s.handleResponses)
		r.Post("/codex/{path}", s.handleCodexCompat)
		r.Post("/v1/audio/speech", s.handleAudioSpeech)
		r.Post("/api/v1/audio/speech", s.handleAudioSpeech)
		r.Post("/v1/audio/transcriptions", s.handleAudioTranscriptions)
		r.Post("/api/v1/audio/transcriptions", s.handleAudioTranscriptions)
		r.Post("/v1/images/generations", s.handleImagesGenerations)
		r.Post("/api/v1/images/generations", s.handleImagesGenerations)
		r.Post("/v1/web/search", s.handleWebSearch)
		r.Post("/api/v1/web/search", s.handleWebSearch)
		r.Post("/v1/web/fetch", s.handleWebFetch)
		r.Post("/api/v1/web/fetch", s.handleWebFetch)
		r.Get("/api/config/export", s.handleConfigExport)
		r.Post("/api/config/import", s.handleConfigImport)
		r.Get("/api/providers", s.handleProvidersList)
		r.Get("/api/providers/catalog", s.handleProvidersCatalog)
		r.Post("/api/providers/validate", s.handleProvidersValidate)
		r.Get("/api/providers/suggested-models", s.handleProvidersSuggestedModels)
		r.Get("/api/providers/client", s.handleProvidersClient)
		r.Post("/api/providers/test-batch", s.handleProvidersTestBatch)
		r.Get("/api/providers/kilo/free-models", s.handleProvidersKiloFreeModels)
		r.Post("/api/providers", s.handleProvidersCreate)
		r.Get("/api/providers/{name}", s.handleProvidersGet)
		r.Put("/api/providers/{name}", s.handleProvidersUpdate)
		r.Delete("/api/providers/{name}", s.handleProvidersDelete)
		r.Post("/api/providers/{name}/test", s.handleProviderTest)
		r.Post("/api/providers/{name}/test-models", s.handleProviderTestModels)
		r.Get("/api/providers/{name}/models", s.handleProviderModels)
		r.Get("/api/provider-nodes", s.handleProviderNodesList)
		r.Post("/api/provider-nodes", s.handleProviderNodesCreate)
		r.Post("/api/provider-nodes/validate", s.handleProviderNodesValidate)
		r.Get("/api/provider-nodes/{id}", s.handleProviderNodesGet)
		r.Put("/api/provider-nodes/{id}", s.handleProviderNodesUpdate)
		r.Delete("/api/provider-nodes/{id}", s.handleProviderNodesDelete)
		r.Get("/api/proxy-pools", s.handleProxyPoolsList)
		r.Post("/api/proxy-pools", s.handleProxyPoolsCreate)
		r.Post("/api/proxy-pools/vercel-deploy", s.handleProxyPoolsVercelDeploy)
		r.Get("/api/proxy-pools/{id}", s.handleProxyPoolsGet)
		r.Put("/api/proxy-pools/{id}", s.handleProxyPoolsUpdate)
		r.Delete("/api/proxy-pools/{id}", s.handleProxyPoolsDelete)
		r.Post("/api/proxy-pools/{id}/test", s.handleProxyPoolTest)
		r.Get("/api/combos", s.handleCombosList)
		r.Post("/api/combos", s.handleCombosCreate)
		r.Get("/api/combos/{name}", s.handleCombosGet)
		r.Put("/api/combos/{name}", s.handleCombosUpdate)
		r.Delete("/api/combos/{name}", s.handleCombosDelete)
		r.Get("/api/keys", s.handleKeysList)
		r.Post("/api/keys", s.handleKeysCreate)
		r.Get("/api/keys/{id}", s.handleKeysGet)
		r.Put("/api/keys/{id}", s.handleKeysUpdate)
		r.Delete("/api/keys/{id}", s.handleKeysDelete)
		r.Get("/api/models/alias", s.handleModelAliasesList)
		r.Post("/api/models/alias", s.handleModelAliasesCreate)
		r.Put("/api/models/alias", s.handleModelAliasesPut)
		r.Delete("/api/models/alias", s.handleModelAliasesDeleteQuery)
		r.Put("/api/models/alias/{name}", s.handleModelAliasesUpdate)
		r.Delete("/api/models/alias/{name}", s.handleModelAliasesDelete)
		r.Get("/api/models", s.handleDashboardModels)
		r.Put("/api/models", s.handleDashboardModelAliasPut)
		r.Post("/api/models/test", s.handleModelsTest)
		r.Get("/api/models/availability", s.handleModelsAvailability)
		r.Post("/api/models/availability", s.handleModelsAvailabilityAction)
		r.Get("/api/models/custom", s.handleModelsCustomList)
		r.Post("/api/models/custom", s.handleModelsCustomCreate)
		r.Delete("/api/models/custom", s.handleModelsCustomDeleteQuery)
		r.Put("/api/models/custom/{name}", s.handleModelsCustomUpdate)
		r.Delete("/api/models/custom/{name}", s.handleModelsCustomDelete)
		r.Get("/api/settings", s.handleSettingsGet)
		r.Put("/api/settings", s.handleSettingsPut)
		r.Patch("/api/settings", s.handleSettingsPatch)
		r.Get("/api/settings/database", s.handleSettingsDatabase)
		r.Post("/api/settings/database", s.handleSettingsDatabase)
		r.Post("/api/settings/proxy-test", s.handleSettingsProxyTest)
		r.Get("/api/logs", s.handleLogsList)
		r.Get("/api/usage", s.handleUsage)
		r.Get("/api/usage/stats", s.handleUsageStats)
		r.Get("/api/usage/history", s.handleUsageHistory)
		r.Get("/api/usage/chart", s.handleUsageChart)
		r.Get("/api/usage/providers", s.handleUsageProviders)
		r.Get("/api/usage/logs", s.handleLogsList)
		r.Get("/api/usage/request-logs", s.handleLogsList)
		r.Get("/api/usage/request-details", s.handleUsageRequestDetails)
		r.Get("/api/usage/stream", s.handleUsageStream)
		r.Get("/api/usage/{connectionId}", s.handleUsageConnection)
		r.Get("/api/providers/{name}/health", s.handleProviderHealth)
		r.Get("/api/providers/{name}/accounts/{account}/health", s.handleAccountHealth)
		r.Get("/api/config", s.handleConfigGet)
		r.Get("/api/metrics", s.handleMetricsJSON)
		r.Get("/api/pricing", s.handlePricing)
		r.Patch("/api/pricing", s.handlePricingPatch)
		r.Delete("/api/pricing", s.handlePricingDelete)
		r.Get("/api/settings/require-login", s.handleSettingsRequireLogin)
		r.Post("/api/locale", s.handleLocaleSet)
		r.Get("/api/tags", s.handleTags)
		r.Post("/api/cloud/auth", s.handleCloudAuth)
		r.Put("/api/cloud/credentials/update", s.handleCloudCredentialsUpdate)
		r.Post("/api/cloud/model/resolve", s.handleCloudModelResolve)
		r.Get("/api/cloud/models/alias", s.handleCloudModelsAliasGet)
		r.Put("/api/cloud/models/alias", s.handleCloudModelsAliasPut)
		r.Get("/api/media-providers/tts/voices", s.handleMediaTTSVoices)
		r.Get("/api/media-providers/tts/elevenlabs/voices", s.handleMediaTTSVoices)
		r.Get("/api/translator/load", s.handleTranslatorLoad)
		r.Post("/api/translator/save", s.handleTranslatorSave)
		r.Post("/api/translator/translate", s.handleTranslatorTranslate)
		r.Post("/api/translator/send", s.handleTranslatorSend)
		r.Get("/api/translator/console-logs", s.handleTranslatorConsoleLogs)
		r.Delete("/api/translator/console-logs", s.handleTranslatorConsoleLogsClear)
		r.Get("/api/translator/console-logs/stream", s.handleTranslatorConsoleLogsStream)
		r.Get("/api/cli-tools/antigravity-mitm", s.handleMITMStatus)
		r.Post("/api/cli-tools/antigravity-mitm", s.handleMITMStart)
		r.Delete("/api/cli-tools/antigravity-mitm", s.handleMITMStop)
		r.Patch("/api/cli-tools/antigravity-mitm", s.handleMITMPatch)
		for _, path := range []string{
			"/api/cli-tools/claude-settings",
			"/api/cli-tools/codex-settings",
			"/api/cli-tools/copilot-settings",
			"/api/cli-tools/droid-settings",
			"/api/cli-tools/hermes-settings",
			"/api/cli-tools/openclaw-settings",
			"/api/cli-tools/opencode-settings",
		} {
			r.Get(path, s.handleCLIToolSettingsGet)
			r.Post(path, s.handleCLIToolSettingsPost)
			r.Delete(path, s.handleCLIToolSettingsDelete)
			if strings.Contains(path, "opencode-settings") {
				r.Patch(path, s.handleCLIToolSettingsPost)
			}
		}
		r.Get("/api/cli-tools/{tool}-settings", s.handleCLIToolSettingsGet)
		r.Post("/api/cli-tools/{tool}-settings", s.handleCLIToolSettingsPost)
		r.Patch("/api/cli-tools/{tool}-settings", s.handleCLIToolSettingsPost)
		r.Delete("/api/cli-tools/{tool}-settings", s.handleCLIToolSettingsDelete)
		r.Get("/api/cli-tools/antigravity-mitm/alias", s.handleCLIToolAliasGet)
		r.Put("/api/cli-tools/antigravity-mitm/alias", s.handleCLIToolAliasPut)
		r.Get("/api/tunnel/tailscale-check", s.handleTailscaleCheck)
		r.Post("/api/tunnel/tailscale-enable", s.handleTunnelEnable)
		r.Post("/api/tunnel/tailscale-disable", s.handleTunnelDisable)
		r.Post("/api/tunnel/tailscale-login", s.handleTailscaleLogin)
		r.Post("/api/tunnel/tailscale-install", s.handleTailscaleInstall)
		r.Post("/api/tunnel/tailscale-start-daemon", s.handleTailscaleStartDaemon)
		r.Get("/api/tunnel/status", s.handleTunnelStatus)
		r.Post("/api/tunnel/enable", s.handleTunnelEnable)
		r.Post("/api/tunnel/disable", s.handleTunnelDisable)
		r.Get("/api/version", s.handleVersion)
		r.Post("/api/version/update", s.handleVersionUpdate)
		r.Get("/api/oauth/tokens", s.handleOAuthTokensList)
		r.Delete("/api/oauth/tokens/{provider}/{account}", s.handleOAuthTokenDelete)
		r.Get("/api/oauth/authorize", s.handleOAuthAuthorize)
		r.Get("/api/oauth/callback", s.handleOAuthCallback)
		r.Post("/api/oauth/exchange", s.handleOAuthExchange)
		r.Get("/api/oauth/poll/{provider}", s.handleOAuthPoll)
		r.Get("/api/oauth/cursor/auto-import", s.handleOAuthDynamicGet)
		r.Get("/api/oauth/cursor/import", s.handleOAuthDynamicGet)
		r.Post("/api/oauth/cursor/import", s.handleOAuthDynamicPost)
		r.Post("/api/oauth/gitlab/pat", s.handleOAuthDynamicPost)
		r.Post("/api/oauth/iflow/cookie", s.handleOAuthDynamicPost)
		r.Get("/api/oauth/kiro/auto-import", s.handleOAuthDynamicGet)
		r.Post("/api/oauth/kiro/import", s.handleOAuthDynamicPost)
		r.Get("/api/oauth/kiro/social-authorize", s.handleOAuthDynamicGet)
		r.Post("/api/oauth/kiro/social-exchange", s.handleOAuthDynamicPost)
		r.Get("/api/oauth/{provider}/{action}", s.handleOAuthDynamicGet)
		r.Post("/api/oauth/{provider}/{action}", s.handleOAuthDynamicPost)
		r.Get("/api/nodes", s.handleNodesList)
		r.Get("/api/metrics/json", s.handleMetricsJSON)
		r.Get("/api/sync/status", s.handleSyncStatus)
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
		if str, ok := v.(string); ok && strings.HasPrefix(str, "error") {
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

func (s *Server) handleSetupStatus(w http.ResponseWriter, _ *http.Request) {
	cfg := s.runtimeConfig.Get()
	enabledProviders := 0
	hasCredentialedEnabledProvider := false
	for _, provider := range cfg.Providers {
		if !provider.Enabled {
			continue
		}
		enabledProviders++
		if provider.APIKey != "" || len(provider.Accounts) > 0 {
			hasCredentialedEnabledProvider = true
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"configured":        hasCredentialedEnabledProvider,
		"has_providers":     len(cfg.Providers) > 0,
		"enabled_providers": enabledProviders,
		"auth_required":     cfg.Server.APIKey != "" || len(cfg.Server.AdminAPIKeys) > 0,
		"ui_path":           "/ui/",
	})
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
	connections := []map[string]any{}
	nodeNameMap := map[string]string{}
	if db := s.db(); db != nil {
		items, err := db.ListProviderConnections(r.Context(), storage.ProviderConnectionFilter{})
		if err == nil {
			nodes, nerr := db.ListProviderNodes(r.Context())
			if nerr == nil {
				for _, n := range nodes {
					nodeNameMap[generatedProviderID(n.Prefix, n.ID)] = n.Name
				}
			}
			connections = make([]map[string]any, 0, len(items))
			for _, c := range items {
				connections = append(connections, sanitizedConnection(c, nodeNameMap))
			}
		}
	}

	// Return list of providers from config
	providersList := s.runtimeConfig.ListProviders()
	providers := make([]map[string]any, 0, len(providersList))
	for _, provider := range providersList {
		providerID := provider.ProviderID
		if providerID == "" {
			providerID = catalog.InferProviderID(provider.Type, provider.Name)
		}
		p := map[string]any{
			"name":           provider.Name,
			"provider_id":    providerID,
			"type":           provider.Type,
			"format":         provider.Format,
			"base_url":       provider.BaseURL,
			"auth_type":      provider.AuthType,
			"tier":           provider.Tier,
			"enabled":        provider.Enabled,
			"accounts_count": len(provider.Accounts),
			"has_api_key":    provider.APIKey != "" || len(provider.Accounts) > 0,
			"has_headers":    len(provider.Headers) > 0,
		}
		// Don't expose API keys in the list
		providers = append(providers, p)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"providers":   providers,
		"connections": connections,
		"count":       len(connections),
	})
}

func (s *Server) handleProvidersCatalog(w http.ResponseWriter, r *http.Request) {
	items := catalog.List()
	serviceKind := strings.TrimSpace(r.URL.Query().Get("service_kind"))
	authType := strings.TrimSpace(r.URL.Query().Get("auth_type"))
	executionStatus := strings.TrimSpace(r.URL.Query().Get("execution_status"))
	runtimeSupported := strings.TrimSpace(r.URL.Query().Get("runtime_supported"))
	deprecated := strings.TrimSpace(r.URL.Query().Get("deprecated"))
	includeHidden := strings.EqualFold(r.URL.Query().Get("include_hidden"), "true")
	includePlanned := strings.EqualFold(r.URL.Query().Get("include_planned"), "true")
	filtered := make([]catalog.ProviderDefinition, 0, len(items))
	deprecatedFilterEnabled := strings.EqualFold(deprecated, "true") || strings.EqualFold(deprecated, "false")
	runtimeFilterEnabled := strings.EqualFold(runtimeSupported, "true") || strings.EqualFold(runtimeSupported, "false")
	runtimeOnly := strings.EqualFold(runtimeSupported, "true")
	deprecatedOnly := strings.EqualFold(deprecated, "true")
	for _, item := range items {
		if !includeHidden && item.Hidden {
			continue
		}
		if !includePlanned && item.ExecutionStatus == "planned" {
			continue
		}
		if runtimeFilterEnabled {
			isRuntimeSupported := item.ExecutionStatus == "supported"
			if runtimeOnly != isRuntimeSupported {
				continue
			}
		}
		if deprecatedFilterEnabled && deprecatedOnly != item.Deprecated {
			continue
		}
		if serviceKind != "" && !containsCI(item.ServiceKinds, serviceKind) {
			continue
		}
		if authType != "" && !containsCI(item.AuthTypes, authType) {
			continue
		}
		if executionStatus != "" && !strings.EqualFold(item.ExecutionStatus, executionStatus) {
			continue
		}
		filtered = append(filtered, item)
	}
	items = filtered
	writeJSON(w, http.StatusOK, map[string]any{"catalog": items, "providers": items, "count": len(items)})
}

func (s *Server) handleProvidersValidate(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req config.ProviderConfig
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid json", "invalid_request_error", "")
		return
	}
	res := s.providerCheck.ValidateProvider(r.Context(), req)
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleProvidersSuggestedModels(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	providerType := q.Get("type")
	url := q.Get("url")
	if strings.TrimSpace(providerType) == "" || strings.TrimSpace(url) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Missing url or type"})
		return
	}
	forceRefresh := strings.EqualFold(q.Get("force_refresh"), "true")
	res := s.providerCheck.SuggestedModels(r.Context(), providerType, url, forceRefresh)
	writeJSON(w, http.StatusOK, map[string]any{"data": res.Models, "models": res.Models})
}

func (s *Server) handleProviderTest(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "name")
	if db := s.db(); db != nil {
		if conn, err := db.GetProviderConnection(r.Context(), id); err == nil {
			provider := providerConfigFromConnection(conn)
			res := s.providerCheck.ValidateProvider(r.Context(), provider)
			conn.TestStatus = map[bool]string{true: "ok", false: "failed"}[res.Valid]
			conn.LastError = res.Error
			now := time.Now().UTC()
			conn.LastTestedAt = &now
			if !res.Valid {
				conn.LastErrorAt = &now
			}
			conn.ErrorCode = classifyValidationErrorCode(res)
			updated, uerr := db.UpdateProviderConnection(r.Context(), conn.ID, conn)
			if uerr != nil {
				writeOpenAIError(w, http.StatusInternalServerError, uerr.Error(), "server_error", "")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"provider":   id,
				"connection": sanitizedConnection(updated, nil),
				"result":     res,
				"valid":      res.Valid,
				"error":      res.Error,
				"refreshed":  false,
			})
			return
		}
	}
	name := id
	provider, ok := s.runtimeConfig.GetProvider(name)
	if !ok {
		writeOpenAIError(w, http.StatusNotFound, "provider not found", "invalid_request_error", "")
		return
	}
	res := s.providerCheck.ValidateProvider(r.Context(), provider)
	writeJSON(w, http.StatusOK, map[string]any{"provider": name, "result": res, "valid": res.Valid, "error": res.Error, "refreshed": false})
}

func (s *Server) handleProviderModels(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if db := s.db(); db != nil {
		if conn, err := db.GetProviderConnection(r.Context(), name); err == nil {
			res := s.providerCheck.ValidateProvider(r.Context(), providerConfigFromConnection(conn))
			writeJSON(w, http.StatusOK, map[string]any{"provider": conn.Provider, "connectionId": conn.ID, "models": res.Models, "status": res.Status, "error": res.Error, "cached": false})
			return
		}
	}
	provider, ok := s.runtimeConfig.GetProvider(name)
	if !ok {
		writeOpenAIError(w, http.StatusNotFound, "provider not found", "invalid_request_error", "")
		return
	}
	res := s.providerCheck.ValidateProvider(r.Context(), provider)
	writeJSON(w, http.StatusOK, map[string]any{"provider": name, "connectionId": name, "models": res.Models, "status": res.Status, "error": res.Error, "cached": false})
}

func (s *Server) handleProvidersCreate(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var payload map[string]any
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&payload); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid json", "invalid_request_error", "")
		return
	}
	if conn, ok := parseConnectionPayload(payload); ok {
		if err := s.validateConnectionPayload(r.Context(), payload, conn, true); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		if db := s.db(); db != nil {
			created, err := db.CreateProviderConnection(r.Context(), conn)
			if err != nil {
				writeOpenAIError(w, http.StatusBadRequest, err.Error(), "invalid_request_error", "")
				return
			}
			writeJSON(w, http.StatusCreated, map[string]any{"connection": sanitizedConnection(created, nil), "provider": sanitizedConnection(created, nil)})
			return
		}
	}

	raw, _ := json.Marshal(payload)
	var req config.ProviderConfig
	if err := json.Unmarshal(raw, &req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid provider payload", "invalid_request_error", "")
		return
	}

	// Use UpdateWithReconfigure for full transactional update with rollback
	if err := s.runtimeConfig.UpdateWithReconfigure(func(cfg *config.Config) error {
		if req.ProviderID == "" {
			req.ProviderID = catalog.InferProviderID(req.Type, req.Name)
		}
		if req.AuthType == "" && req.APIKey != "" {
			req.AuthType = "api_key"
		}
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

	writeJSON(w, http.StatusCreated, map[string]any{"provider": sanitizedProvider(req)})
}

func (s *Server) handleProvidersGet(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if db := s.db(); db != nil {
		if conn, err := db.GetProviderConnection(r.Context(), name); err == nil {
			writeJSON(w, http.StatusOK, map[string]any{"connection": sanitizedConnection(conn, nil)})
			return
		}
	}

	provider, ok := s.runtimeConfig.GetProvider(name)
	if !ok {
		writeOpenAIError(w, http.StatusNotFound, "provider not found", "invalid_request_error", "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"provider": sanitizedProvider(provider)})
}

func (s *Server) handleProvidersUpdate(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	name := chi.URLParam(r, "name")

	var payload map[string]any
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&payload); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid json", "invalid_request_error", "")
		return
	}
	if db := s.db(); db != nil {
		if existing, err := db.GetProviderConnection(r.Context(), name); err == nil {
			updated := applyConnectionPatch(existing, payload)
			if err := s.validateConnectionPayload(r.Context(), payload, updated, false); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
				return
			}
			saved, uerr := db.UpdateProviderConnection(r.Context(), name, updated)
			if uerr != nil {
				writeOpenAIError(w, http.StatusBadRequest, uerr.Error(), "invalid_request_error", "")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"connection": sanitizedConnection(saved, nil), "provider": sanitizedConnection(saved, nil)})
			return
		}
	}

	raw, _ := json.Marshal(payload)
	var req config.ProviderConfig
	if err := json.Unmarshal(raw, &req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid provider payload", "invalid_request_error", "")
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
				if req.ProviderID == "" {
					req.ProviderID = cfg.Providers[i].ProviderID
					if req.ProviderID == "" {
						req.ProviderID = catalog.InferProviderID(req.Type, req.Name)
					}
				}
				if req.APIKey == "" && len(req.Accounts) == 0 {
					req.APIKey = cfg.Providers[i].APIKey
					req.Accounts = cfg.Providers[i].Accounts
				}
				if req.AuthType == "" {
					req.AuthType = cfg.Providers[i].AuthType
					if req.AuthType == "" && (req.APIKey != "" || len(req.Accounts) > 0) {
						req.AuthType = "api_key"
					}
				}
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

	writeJSON(w, http.StatusOK, map[string]any{"provider": sanitizedProvider(req)})
}

func sanitizedProvider(provider config.ProviderConfig) map[string]any {
	providerID := provider.ProviderID
	if providerID == "" {
		providerID = catalog.InferProviderID(provider.Type, provider.Name)
	}
	accounts := make([]map[string]any, 0, len(provider.Accounts))
	for _, account := range provider.Accounts {
		accounts = append(accounts, map[string]any{
			"id":            account.ID,
			"name":          account.Name,
			"auth_type":     account.AuthType,
			"enabled":       account.Enabled,
			"priority":      account.Priority,
			"default_model": account.DefaultModel,
			"has_api_key":   account.APIKey != "",
			"has_token":     account.AccessToken != "" || account.RefreshToken != "" || account.IDToken != "",
			"has_cookie":    account.Cookie != "",
		})
	}
	return map[string]any{
		"name":           provider.Name,
		"provider_id":    providerID,
		"type":           provider.Type,
		"format":         provider.Format,
		"base_url":       provider.BaseURL,
		"auth_type":      provider.AuthType,
		"tier":           provider.Tier,
		"enabled":        provider.Enabled,
		"has_api_key":    provider.APIKey != "",
		"accounts_count": len(provider.Accounts),
		"accounts":       accounts,
		"has_headers":    len(provider.Headers) > 0,
	}
}

func (s *Server) handleProvidersDelete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if db := s.db(); db != nil {
		if err := db.DeleteProviderConnection(r.Context(), name); err == nil {
			writeJSON(w, http.StatusOK, map[string]any{"deleted": name, "type": "connection"})
			return
		}
	}

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

func (s *Server) db() *storage.DB {
	if s.asyncWriter == nil {
		return nil
	}
	return s.asyncWriter.GetDB()
}

func generatedProviderID(prefix, nodeID string) string {
	switch strings.ToLower(strings.TrimSpace(prefix)) {
	case "openai-compatible-":
		return "openai-compatible-" + nodeID
	case "anthropic-compatible-":
		return "anthropic-compatible-" + nodeID
	case "custom-embedding-":
		return "custom-embedding-" + nodeID
	default:
		return strings.TrimSpace(prefix) + nodeID
	}
}

func isCompatibleProviderID(id string) bool {
	v := strings.ToLower(strings.TrimSpace(id))
	return strings.HasPrefix(v, "openai-compatible-") || strings.HasPrefix(v, "anthropic-compatible-") || strings.HasPrefix(v, "custom-embedding-")
}

func (s *Server) validateConnectionPayload(ctx context.Context, payload map[string]any, conn storage.ProviderConnection, isCreate bool) error {
	provider := strings.TrimSpace(conn.Provider)
	if provider == "" {
		return fmt.Errorf("provider is required")
	}
	if authType := normalizeAuthType(conn.AuthType); authType != "" {
		conn.AuthType = authType
	}
	if db := s.db(); db != nil {
		if isCompatibleProviderID(provider) {
			nodeID := strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(provider, "openai-compatible-"), "anthropic-compatible-"), "custom-embedding-")
			node, err := db.GetProviderNode(ctx, nodeID)
			if err != nil {
				return fmt.Errorf("compatible node not found")
			}
			if conn.ProviderSpecificData == nil {
				conn.ProviderSpecificData = map[string]any{}
			}
			conn.ProviderSpecificData["baseUrl"] = node.BaseURL
			conn.ProviderSpecificData["nodeName"] = node.Name
			conn.ProviderSpecificData["prefix"] = node.Prefix
			conn.ProviderSpecificData["apiType"] = node.APIType
			items, err := db.ListProviderConnections(ctx, storage.ProviderConnectionFilter{Provider: provider})
			if err == nil {
				for _, item := range items {
					if !isCreate && item.ID == conn.ID {
						continue
					}
					return fmt.Errorf("only one connection is allowed for this compatible node")
				}
			}
		}
	}
	if conn.AuthType == "cookie" || normalizeAuthType(conn.AuthType) == "cookie" {
		if isCreate && strings.TrimSpace(conn.APIKey) == "" {
			return fmt.Errorf("cookie value is required")
		}
	}
	return nil
}

func containsCI(values []string, needle string) bool {
	for _, v := range values {
		if strings.EqualFold(v, needle) {
			return true
		}
	}
	return false
}

func parseConnectionPayload(payload map[string]any) (storage.ProviderConnection, bool) {
	provider, _ := payload["provider"].(string)
	if strings.TrimSpace(provider) == "" {
		provider, _ = payload["provider_id"].(string)
	}
	if strings.TrimSpace(provider) == "" {
		return storage.ProviderConnection{}, false
	}
	baseURL := stringField(payload, "base_url", "baseUrl")
	providerType, _ := payload["type"].(string)
	format, _ := payload["format"].(string)
	authType := stringField(payload, "auth_type", "authType")
	name, _ := payload["name"].(string)
	apiKey := stringField(payload, "api_key", "apiKey", "cookie")
	accessToken := stringField(payload, "access_token", "accessToken")
	refreshToken := stringField(payload, "refresh_token", "refreshToken")
	idToken := stringField(payload, "id_token", "idToken")
	defaultModel := stringField(payload, "default_model", "defaultModel")
	providerSpecificData := mapField(payload, "provider_specific_data", "providerSpecificData")
	if providerSpecificData == nil {
		providerSpecificData = map[string]any{}
	}
	if hasAnyKey(payload, "connectionProxyEnabled", "connectionProxyUrl", "connectionNoProxy") {
		if enabled, ok := boolField(payload, "connectionProxyEnabled"); ok {
			providerSpecificData["connectionProxyEnabled"] = enabled
		}
		if v, ok := stringFieldPresent(payload, "connectionProxyUrl"); ok {
			providerSpecificData["connectionProxyUrl"] = strings.TrimSpace(v)
		}
		if v, ok := stringFieldPresent(payload, "connectionNoProxy"); ok {
			providerSpecificData["connectionNoProxy"] = strings.TrimSpace(v)
		}
	}
	if v, ok := stringFieldPresent(payload, "proxyPoolId"); ok {
		s := strings.TrimSpace(v)
		if s == "" || s == "__none__" {
			delete(providerSpecificData, "proxyPoolId")
		} else {
			providerSpecificData["proxyPoolId"] = s
		}
	}
	priority := intField(payload, "priority")
	globalPriority := intField(payload, "global_priority", "globalPriority")
	isActive := true
	if v, ok := boolField(payload, "is_active", "isActive"); ok {
		isActive = v
	}
	enabled := true
	if v, ok := payload["enabled"].(bool); ok {
		enabled = v
	}
	if authType == "" {
		switch {
		case apiKey != "":
			authType = "apikey"
		case accessToken != "":
			authType = "oauth"
		default:
			authType = "apikey"
		}
	}
	authType = normalizeAuthType(authType)
	return storage.ProviderConnection{
		Provider:             provider,
		Name:                 name,
		DisplayName:          name,
		AuthType:             authType,
		APIKey:               apiKey,
		AccessToken:          accessToken,
		RefreshToken:         refreshToken,
		IDToken:              idToken,
		ProviderType:         providerType,
		Format:               format,
		BaseURL:              baseURL,
		DefaultModel:         defaultModel,
		ProviderSpecificData: providerSpecificData,
		Priority:             priority,
		GlobalPriority:       globalPriority,
		IsActive:             isActive,
		Enabled:              enabled,
	}, true
}

func providerConfigFromConnection(c storage.ProviderConnection) config.ProviderConfig {
	t := c.ProviderType
	if t == "" {
		t = c.Provider
	}
	f := c.Format
	if f == "" {
		if def, ok := catalog.Get(c.Provider); ok {
			f = def.Format
		}
	}
	base := c.BaseURL
	if base == "" {
		if def, ok := catalog.Get(c.Provider); ok {
			base = def.DefaultBaseURL
		}
	}
	apiKey := c.APIKey
	headers := c.Headers
	if c.Provider == "github" {
		if token, _ := c.ProviderSpecificData["copilotToken"].(string); strings.TrimSpace(token) != "" {
			apiKey = token
		} else if c.AccessToken != "" {
			apiKey = c.AccessToken
		}
		headers = mergeStringMaps(headers, githubCopilotHeaders())
	}
	return config.ProviderConfig{
		Name:       c.Name,
		ProviderID: c.Provider,
		Type:       t,
		Format:     f,
		BaseURL:    base,
		APIKey:     apiKey,
		AuthType:   c.AuthType,
		Headers:    headers,
		Enabled:    c.Enabled,
		Accounts:   []config.AccountConfig{{Name: c.Name, AccessToken: c.AccessToken, RefreshToken: c.RefreshToken, IDToken: c.IDToken, APIKey: apiKey}},
	}
}

func githubCopilotHeaders() map[string]string {
	return map[string]string{
		"copilot-integration-id": "vscode-chat",
		"editor-version":         "vscode/1.85.0",
		"editor-plugin-version":  "copilot-chat/0.26.7",
		"user-agent":             "GitHubCopilotChat/0.26.7",
		"x-github-api-version":   "2025-04-01",
	}
}

func mergeStringMaps(base map[string]string, overlay map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overlay {
		if strings.TrimSpace(out[k]) == "" {
			out[k] = v
		}
	}
	return out
}

func sanitizedConnection(c storage.ProviderConnection, nodeNameMap map[string]string) map[string]any {
	name := c.Name
	if name == "" {
		name = c.DisplayName
	}
	if nodeNameMap != nil {
		if v := nodeNameMap[c.Provider]; strings.TrimSpace(v) != "" {
			name = v
		} else if v, _ := c.ProviderSpecificData["nodeName"].(string); strings.TrimSpace(v) != "" {
			name = v
		}
	}
	return map[string]any{
		"id":                     c.ID,
		"provider":               c.Provider,
		"auth_type":              normalizeAuthType(c.AuthType),
		"name":                   name,
		"display_name":           c.DisplayName,
		"email":                  c.Email,
		"provider_id":            c.Provider,
		"type":                   c.ProviderType,
		"format":                 c.Format,
		"base_url":               c.BaseURL,
		"is_active":              c.IsActive,
		"enabled":                c.Enabled,
		"test_status":            c.TestStatus,
		"last_error":             c.LastError,
		"last_tested_at":         c.LastTestedAt,
		"last_error_at":          c.LastErrorAt,
		"error_code":             c.ErrorCode,
		"default_model":          c.DefaultModel,
		"priority":               c.Priority,
		"global_priority":        c.GlobalPriority,
		"provider_specific_data": c.ProviderSpecificData,
		"has_api_key":            c.APIKey != "",
		"has_token":              c.AccessToken != "" || c.RefreshToken != "" || c.IDToken != "",
		"has_cookie":             strings.EqualFold(normalizeAuthType(c.AuthType), "cookie") && c.APIKey != "",
		"created_at":             c.CreatedAt,
		"updated_at":             c.UpdatedAt,
	}
}

func applyConnectionPatch(existing storage.ProviderConnection, patch map[string]any) storage.ProviderConnection {
	clearSecret := false
	if v, ok := boolField(patch, "clear_secret", "clearSecret"); ok {
		clearSecret = v
	}
	if v, ok := patch["provider"].(string); ok && strings.TrimSpace(v) != "" {
		existing.Provider = v
	}
	if v, ok := patch["provider_id"].(string); ok && strings.TrimSpace(v) != "" {
		existing.Provider = v
	}
	if v, ok := patch["name"].(string); ok && strings.TrimSpace(v) != "" {
		existing.Name = v
		existing.DisplayName = v
	}
	if v := stringField(patch, "auth_type", "authType"); v != "" {
		existing.AuthType = normalizeAuthType(v)
	}
	if v, ok := stringFieldPresent(patch, "api_key", "apiKey", "cookie"); ok {
		if strings.TrimSpace(v) != "" {
			existing.APIKey = v
		} else if clearSecret {
			existing.APIKey = ""
		}
	}
	if v, ok := stringFieldPresent(patch, "access_token", "accessToken"); ok {
		if strings.TrimSpace(v) != "" {
			existing.AccessToken = v
		} else if clearSecret {
			existing.AccessToken = ""
		}
	}
	if v, ok := stringFieldPresent(patch, "refresh_token", "refreshToken"); ok {
		if strings.TrimSpace(v) != "" {
			existing.RefreshToken = v
		} else if clearSecret {
			existing.RefreshToken = ""
		}
	}
	if v, ok := stringFieldPresent(patch, "id_token", "idToken"); ok {
		if strings.TrimSpace(v) != "" {
			existing.IDToken = v
		} else if clearSecret {
			existing.IDToken = ""
		}
	}
	if v, ok := patch["type"].(string); ok {
		existing.ProviderType = v
	}
	if v, ok := patch["format"].(string); ok {
		existing.Format = v
	}
	if v, ok := stringFieldPresent(patch, "base_url", "baseUrl"); ok {
		existing.BaseURL = v
	}
	if v, ok := stringFieldPresent(patch, "default_model", "defaultModel"); ok {
		existing.DefaultModel = v
	}
	if v, ok := intFieldPresent(patch, "priority"); ok {
		existing.Priority = v
	}
	if v, ok := intFieldPresent(patch, "global_priority", "globalPriority"); ok {
		existing.GlobalPriority = v
	}
	if data := mapField(patch, "provider_specific_data", "providerSpecificData"); data != nil {
		if existing.ProviderSpecificData == nil {
			existing.ProviderSpecificData = map[string]any{}
		}
		for k, v := range data {
			existing.ProviderSpecificData[k] = v
		}
	}
	if hasAnyKey(patch, "connectionProxyEnabled", "connectionProxyUrl", "connectionNoProxy") {
		if existing.ProviderSpecificData == nil {
			existing.ProviderSpecificData = map[string]any{}
		}
		if v, ok := boolField(patch, "connectionProxyEnabled"); ok {
			existing.ProviderSpecificData["connectionProxyEnabled"] = v
		}
		if v, ok := stringFieldPresent(patch, "connectionProxyUrl"); ok {
			existing.ProviderSpecificData["connectionProxyUrl"] = strings.TrimSpace(v)
		}
		if v, ok := stringFieldPresent(patch, "connectionNoProxy"); ok {
			existing.ProviderSpecificData["connectionNoProxy"] = strings.TrimSpace(v)
		}
	}
	if hasAnyKey(patch, "proxyPoolId") {
		if existing.ProviderSpecificData == nil {
			existing.ProviderSpecificData = map[string]any{}
		}
		v := strings.TrimSpace(stringField(patch, "proxyPoolId"))
		if v == "" || v == "__none__" {
			delete(existing.ProviderSpecificData, "proxyPoolId")
		} else {
			existing.ProviderSpecificData["proxyPoolId"] = v
		}
	}
	if v, ok := patch["enabled"].(bool); ok {
		existing.Enabled = v
	}
	if v, ok := boolField(patch, "is_active", "isActive"); ok {
		existing.IsActive = v
	}
	return existing
}

func normalizeAuthType(v string) string {
	s := strings.ToLower(strings.TrimSpace(v))
	switch s {
	case "api_key", "apikey":
		return "apikey"
	default:
		return s
	}
}

func hasAnyKey(payload map[string]any, keys ...string) bool {
	for _, k := range keys {
		if _, ok := payload[k]; ok {
			return true
		}
	}
	return false
}

func stringField(payload map[string]any, names ...string) string {
	v, _ := stringFieldPresent(payload, names...)
	return v
}

func stringFieldPresent(payload map[string]any, names ...string) (string, bool) {
	for _, name := range names {
		if v, ok := payload[name].(string); ok {
			return v, true
		}
	}
	return "", false
}

func boolField(payload map[string]any, names ...string) (bool, bool) {
	for _, name := range names {
		if v, ok := payload[name].(bool); ok {
			return v, true
		}
	}
	return false, false
}

func intField(payload map[string]any, names ...string) int {
	v, _ := intFieldPresent(payload, names...)
	return v
}

func intFieldPresent(payload map[string]any, names ...string) (int, bool) {
	for _, name := range names {
		switch v := payload[name].(type) {
		case float64:
			return int(v), true
		case int:
			return v, true
		}
	}
	return 0, false
}

func mapField(payload map[string]any, names ...string) map[string]any {
	for _, name := range names {
		if v, ok := payload[name].(map[string]any); ok {
			return v
		}
	}
	return nil
}

func mapStringField(payload map[string]any, names ...string) map[string]string {
	for _, name := range names {
		if v, ok := payload[name].(map[string]string); ok {
			return v
		}
		if v, ok := payload[name].(map[string]any); ok {
			out := map[string]string{}
			for k, raw := range v {
				if s, sok := raw.(string); sok {
					out[k] = s
				}
			}
			return out
		}
	}
	return nil
}

func classifyValidationErrorCode(res validation.Result) string {
	if res.Valid {
		return ""
	}
	errText := strings.ToLower(res.Error)
	if strings.Contains(errText, "401") || strings.Contains(errText, "403") || strings.Contains(errText, "unauthorized") {
		return "invalid_auth"
	}
	if strings.Contains(errText, "timeout") || strings.Contains(errText, "dial") || strings.Contains(errText, "refused") || strings.Contains(errText, "tls") {
		return "network_error"
	}
	if strings.Contains(errText, "429") || strings.Contains(errText, "5") {
		return "unavailable"
	}
	return "unknown"
}

func (s *Server) handleProviderNodesList(w http.ResponseWriter, r *http.Request) {
	if db := s.db(); db != nil {
		nodes, err := db.ListProviderNodes(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		items := make([]map[string]any, 0, len(nodes))
		for _, n := range nodes {
			items = append(items, map[string]any{
				"id":                  n.ID,
				"prefix":              n.Prefix,
				"name":                n.Name,
				"apiType":             n.APIType,
				"baseUrl":             n.BaseURL,
				"headers":             n.Headers,
				"createdAt":           n.CreatedAt,
				"updatedAt":           n.UpdatedAt,
				"generatedProviderId": generatedProviderID(n.Prefix, n.ID),
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"nodes": items})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodes": []any{}})
}

func (s *Server) handleProviderNodesGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if db := s.db(); db != nil {
		n, err := db.GetProviderNode(r.Context(), id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "Node not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"node": map[string]any{"id": n.ID, "prefix": n.Prefix, "name": n.Name, "apiType": n.APIType, "baseUrl": n.BaseURL, "headers": n.Headers, "createdAt": n.CreatedAt, "updatedAt": n.UpdatedAt, "generatedProviderId": generatedProviderID(n.Prefix, n.ID)}})
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]any{"error": "Node not found"})
}

func (s *Server) handleProviderNodesCreate(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var body struct {
		ID      string            `json:"id"`
		Prefix  string            `json:"prefix"`
		Name    string            `json:"name"`
		APIType string            `json:"apiType"`
		BaseURL string            `json:"baseUrl"`
		Headers map[string]string `json:"headers"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	if strings.TrimSpace(body.Prefix) == "" || strings.TrimSpace(body.Name) == "" || strings.TrimSpace(body.BaseURL) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "prefix, name, and baseUrl are required"})
		return
	}
	if db := s.db(); db != nil {
		n, err := db.CreateProviderNode(r.Context(), storage.ProviderNode{ID: strings.TrimSpace(body.ID), Prefix: strings.TrimSpace(body.Prefix), Name: strings.TrimSpace(body.Name), APIType: strings.TrimSpace(body.APIType), BaseURL: strings.TrimSpace(body.BaseURL), Headers: body.Headers})
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"node": map[string]any{"id": n.ID, "prefix": n.Prefix, "name": n.Name, "apiType": n.APIType, "baseUrl": n.BaseURL, "headers": n.Headers, "createdAt": n.CreatedAt, "updatedAt": n.UpdatedAt, "generatedProviderId": generatedProviderID(n.Prefix, n.ID)}})
		return
	}
	writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "storage not enabled"})
}

func (s *Server) handleProviderNodesUpdate(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	id := chi.URLParam(r, "id")
	var body map[string]any
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	if db := s.db(); db != nil {
		existing, err := db.GetProviderNode(r.Context(), id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "Node not found"})
			return
		}
		if v := strings.TrimSpace(stringField(body, "prefix")); v != "" {
			existing.Prefix = v
		}
		if v := strings.TrimSpace(stringField(body, "name")); v != "" {
			existing.Name = v
		}
		if v := strings.TrimSpace(stringField(body, "apiType")); v != "" {
			existing.APIType = v
		}
		if v := strings.TrimSpace(stringField(body, "baseUrl", "base_url")); v != "" {
			existing.BaseURL = v
		}
		if h := mapStringField(body, "headers"); h != nil {
			existing.Headers = h
		}
		updated, err := db.UpdateProviderNode(r.Context(), id, existing)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"node": map[string]any{"id": updated.ID, "prefix": updated.Prefix, "name": updated.Name, "apiType": updated.APIType, "baseUrl": updated.BaseURL, "headers": updated.Headers, "createdAt": updated.CreatedAt, "updatedAt": updated.UpdatedAt, "generatedProviderId": generatedProviderID(updated.Prefix, updated.ID)}})
		return
	}
	writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "storage not enabled"})
}

func (s *Server) handleProviderNodesDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if db := s.db(); db != nil {
		if err := db.DeleteProviderNode(r.Context(), id); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "Node not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"message": "Node deleted successfully"})
		return
	}
	writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "storage not enabled"})
}

func (s *Server) handleProxyPoolsList(w http.ResponseWriter, r *http.Request) {
	db := s.db()
	if db == nil {
		writeJSON(w, http.StatusOK, map[string]any{"pools": []any{}, "count": 0})
		return
	}
	pools, err := db.ListProxyPools(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	items := make([]map[string]any, 0, len(pools))
	for _, pool := range pools {
		items = append(items, proxyPoolResponse(pool))
	}
	writeJSON(w, http.StatusOK, map[string]any{"pools": items, "count": len(items)})
}

func (s *Server) handleProxyPoolsGet(w http.ResponseWriter, r *http.Request) {
	db := s.db()
	if db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "storage not enabled"})
		return
	}
	pool, err := db.GetProxyPool(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "proxy pool not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pool": proxyPoolResponse(pool)})
}

func (s *Server) handleProxyPoolsCreate(w http.ResponseWriter, r *http.Request) {
	s.saveProxyPool(w, r, http.StatusCreated)
}

func (s *Server) handleProxyPoolsUpdate(w http.ResponseWriter, r *http.Request) {
	s.saveProxyPool(w, r, http.StatusOK)
}

func (s *Server) saveProxyPool(w http.ResponseWriter, r *http.Request, status int) {
	defer r.Body.Close()
	db := s.db()
	if db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "storage not enabled"})
		return
	}
	var payload struct {
		ID        string    `json:"id"`
		Name      string    `json:"name"`
		Proxies   []string  `json:"proxies"`
		ProxyURLs []string  `json:"proxy_urls"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	pool := storage.ProxyPool{
		ID:        payload.ID,
		Name:      payload.Name,
		Proxies:   payload.Proxies,
		CreatedAt: payload.CreatedAt,
		UpdatedAt: payload.UpdatedAt,
	}
	if len(pool.Proxies) == 0 && len(payload.ProxyURLs) > 0 {
		pool.Proxies = payload.ProxyURLs
	}
	if id := strings.TrimSpace(chi.URLParam(r, "id")); id != "" {
		pool.ID = id
	}
	pool.ID = strings.TrimSpace(pool.ID)
	pool.Name = strings.TrimSpace(pool.Name)
	for i := range pool.Proxies {
		pool.Proxies[i] = strings.TrimSpace(pool.Proxies[i])
	}
	pool.Proxies = compactStrings(pool.Proxies)
	if pool.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "name is required"})
		return
	}
	saved, err := db.SaveProxyPool(r.Context(), pool)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, status, map[string]any{"pool": proxyPoolResponse(saved)})
}

func (s *Server) handleProxyPoolsDelete(w http.ResponseWriter, r *http.Request) {
	db := s.db()
	if db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "storage not enabled"})
		return
	}
	id := chi.URLParam(r, "id")
	if err := db.DeleteProxyPool(r.Context(), id); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "proxy pool not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}

func (s *Server) handleOAuthDynamicGet(w http.ResponseWriter, r *http.Request) {
	provider := strings.TrimSpace(chi.URLParam(r, "provider"))
	action := strings.TrimSpace(chi.URLParam(r, "action"))
	if provider == "" || action == "" {
		provider, action = oauthProviderActionFromPath(r.URL.Path)
	}
	switch action {
	case "auto-import":
		switch provider {
		case "cursor":
			s.handleCursorAutoImport(w, r)
		case "kiro":
			s.handleKiroAutoImport(w, r)
		default:
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Unknown auto-import provider"})
		}
	case "import":
		if provider == "cursor" {
			writeJSON(w, http.StatusOK, map[string]any{
				"provider": "cursor",
				"method":   "import_token",
				"instructions": []string{
					"Open Cursor local state storage.",
					"Copy cursorAuth/accessToken and storage.serviceMachineId.",
					"Submit both values to this endpoint.",
				},
				"requiredFields": []map[string]any{
					{"name": "accessToken", "label": "Access Token", "description": "From cursorAuth/accessToken in state.vscdb", "type": "textarea"},
					{"name": "machineId", "label": "Machine ID", "description": "From storage.serviceMachineId in state.vscdb", "type": "text"},
				},
			})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Unknown import provider"})
	case "social-authorize":
		if provider != "kiro" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Unknown social authorize provider"})
			return
		}
		s.handleKiroSocialAuthorize(w, r)
	case "authorize":
		s.handleOAuthAuthorizeGeneric(w, r)
	case "device-code":
		s.handleOAuthDeviceCode(w, r)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Unknown action", "code": "invalid_oauth_action"})
	}
}

func (s *Server) handleOAuthDynamicPost(w http.ResponseWriter, r *http.Request) {
	provider := strings.TrimSpace(chi.URLParam(r, "provider"))
	action := strings.TrimSpace(chi.URLParam(r, "action"))
	if provider == "" || action == "" {
		provider, action = oauthProviderActionFromPath(r.URL.Path)
	}
	switch action {
	case "import":
		switch provider {
		case "cursor":
			s.handleCursorImport(w, r)
		case "kiro":
			s.handleKiroImport(w, r)
		default:
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Unknown import provider"})
		}
	case "cookie":
		if provider == "iflow" {
			s.handleIFlowCookie(w, r)
			return
		}
		if provider == "grok-web" || provider == "perplexity-web" {
			s.handleWebCookie(w, r, provider)
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Unknown cookie provider"})
	case "pat":
		if provider == "gitlab" {
			s.handleGitLabPAT(w, r)
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Unknown PAT provider"})
	case "social-exchange":
		if provider != "kiro" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Unknown social exchange provider"})
			return
		}
		s.handleKiroSocialExchange(w, r)
	case "exchange":
		s.handleOAuthExchange(w, r)
	case "poll":
		s.handleOAuthPoll(w, r)
	case "refresh":
		s.handleOAuthRefresh(w, r, provider)
	case "delete", "revoke":
		s.handleOAuthDelete(w, r, provider)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Unknown action", "code": "invalid_oauth_action"})
	}
}

func oauthProviderActionFromPath(path string) (string, string) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 3 || parts[0] != "api" || parts[1] != "oauth" {
		return "", ""
	}
	provider := parts[2]
	action := ""
	if len(parts) > 3 {
		action = strings.Join(parts[3:], "/")
	}
	return provider, action
}

func (s *Server) handleCursorImport(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var body struct {
		AccessToken string `json:"accessToken"`
		MachineID   string `json:"machineId"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid request body"})
		return
	}
	if strings.TrimSpace(body.AccessToken) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Access token is required"})
		return
	}
	if strings.TrimSpace(body.MachineID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Machine ID is required"})
		return
	}
	conn, err := s.createOAuthConnection(r.Context(), storage.ProviderConnection{
		Provider:    "cursor",
		AuthType:    "oauth",
		AccessToken: strings.TrimSpace(body.AccessToken),
		ProviderSpecificData: map[string]any{
			"machineId":  strings.TrimSpace(body.MachineID),
			"authMethod": "imported",
			"provider":   "Imported",
		},
		TestStatus: "active",
		IsActive:   true,
		Enabled:    true,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "connection": oauthConnectionSummary(conn)})
}

func (s *Server) handleCodexAuthorize(w http.ResponseWriter, r *http.Request) {
	redirectURI := strings.TrimSpace(r.URL.Query().Get("redirect_uri"))
	if redirectURI == "" {
		redirectURI = "http://localhost:8080/callback"
	}
	codeVerifier, codeChallenge, err := generatePKCEPair()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	state, err := generateStateToken()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	query := url.Values{}
	query.Set("client_id", codexOAuthEndpoints.ClientID)
	query.Set("response_type", "code")
	query.Set("redirect_uri", redirectURI)
	query.Set("scope", "openid profile email offline_access")
	query.Set("code_challenge", codeChallenge)
	query.Set("code_challenge_method", "S256")
	query.Set("state", state)
	writeJSON(w, http.StatusOK, map[string]any{
		"authUrl":       codexOAuthEndpoints.AuthURL + "?" + query.Encode(),
		"state":         state,
		"codeVerifier":  codeVerifier,
		"codeChallenge": codeChallenge,
		"redirectUri":   redirectURI,
	})
}

func (s *Server) handleCodexExchange(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var body struct {
		Code         string         `json:"code"`
		RedirectURI  string         `json:"redirectUri"`
		CodeVerifier string         `json:"codeVerifier"`
		State        string         `json:"state"`
		Meta         map[string]any `json:"meta"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid or empty request body"})
		return
	}
	if strings.TrimSpace(body.Code) == "" || strings.TrimSpace(body.RedirectURI) == "" || strings.TrimSpace(body.CodeVerifier) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Missing required fields"})
		return
	}
	tokenURL := codexOAuthEndpoints.TokenURL
	if v, ok := body.Meta["token_url"].(string); ok && strings.TrimSpace(v) != "" {
		tokenURL = strings.TrimSpace(v)
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", codexOAuthEndpoints.ClientID)
	form.Set("code", strings.TrimSpace(body.Code))
	form.Set("redirect_uri", strings.TrimSpace(body.RedirectURI))
	form.Set("code_verifier", strings.TrimSpace(body.CodeVerifier))
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": strings.TrimSpace(string(respBody))})
		return
	}
	var token struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
		TokenType    string `json:"token_type"`
	}
	if err := json.Unmarshal(respBody, &token); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	if token.AccessToken == "" {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "missing access_token"})
		return
	}
	expiresAt := time.Now().Add(time.Duration(firstPositiveInt(token.ExpiresIn, 3600)) * time.Second)
	conn, err := s.createOAuthConnection(r.Context(), storage.ProviderConnection{
		Provider:     "codex",
		AuthType:     "oauth",
		Name:         "codex-oauth",
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		IDToken:      token.IDToken,
		ExpiresAt:    &expiresAt,
		Email:        extractJWTEmail(token.IDToken),
		TestStatus:   "active",
		IsActive:     true,
		Enabled:      true,
		ProviderSpecificData: map[string]any{
			"scope":     token.Scope,
			"tokenType": firstNonEmpty(token.TokenType, "Bearer"),
		},
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "connection": oauthConnectionSummary(conn)})
}

func (s *Server) handleOAuthAuthorizeGeneric(w http.ResponseWriter, r *http.Request) {
	provider := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "provider")))
	cfg, ok := oauthProviderRegistry.Get(provider)
	if !ok || (cfg.FlowType != oauth.FlowTypeAuthCode && cfg.FlowType != oauth.FlowTypeAuthCodePKCE) {
		writeJSON(w, http.StatusNotImplemented, map[string]any{"error": "OAuth provider is not supported", "code": "unsupported_oauth"})
		return
	}

	redirectURI := strings.TrimSpace(r.URL.Query().Get("redirect_uri"))
	if redirectURI == "" {
		redirectURI = "http://localhost:8080/callback"
		if cfg.FixedPort > 0 {
			redirectURI = fmt.Sprintf("http://localhost:%d%s", cfg.FixedPort, cfg.CallbackPath)
		}
	}

	meta := make(map[string]string)
	for k, v := range r.URL.Query() {
		if len(v) > 0 {
			meta[k] = v[0]
		}
	}

	codeVerifier, codeChallenge, err := oauth.GeneratePKCEPair()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	state, err := oauth.GenerateStateToken()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	authURL, err := oauth.BuildAuthorizeURL(cfg, redirectURI, state, codeChallenge, meta)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	response := map[string]any{
		"authUrl":      authURL,
		"state":        state,
		"codeVerifier": codeVerifier,
		"redirectUri":  redirectURI,
		"flowType":     cfg.FlowType,
	}
	if cfg.FixedPort > 0 {
		response["fixedPort"] = cfg.FixedPort
	}
	if cfg.CallbackPath != "" {
		response["callbackPath"] = cfg.CallbackPath
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleOAuthDeviceCode(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	provider := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "provider")))
	cfg, ok := oauthProviderRegistry.Get(provider)
	if !ok || cfg.FlowType != oauth.FlowTypeDeviceCode {
		writeJSON(w, http.StatusNotImplemented, map[string]any{"error": "Device code flow is not supported", "code": "unsupported_device_code"})
		return
	}

	codeVerifier, codeChallenge, err := oauth.GeneratePKCEPair()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	// Parse options from query (for Kiro region/startUrl/authMethod)
	options := make(map[string]string)
	for k, v := range r.URL.Query() {
		if len(v) > 0 {
			options[k] = v[0]
		}
	}

	deviceData, err := oauth.RequestDeviceCode(r.Context(), cfg, codeChallenge, options)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}

	response := map[string]any{
		"device_code":               deviceData["device_code"],
		"user_code":                 deviceData["user_code"],
		"verification_uri":          deviceData["verification_uri"],
		"verification_uri_complete": deviceData["verification_uri_complete"],
		"expires_in":                deviceData["expires_in"],
		"interval":                  deviceData["interval"],
		"codeVerifier":              codeVerifier,
		"codeChallenge":             codeChallenge,
	}

	// Add Kiro-specific fields
	if provider == "kiro" {
		response["_clientId"] = deviceData["_clientId"]
		response["_clientSecret"] = deviceData["_clientSecret"]
		response["_region"] = deviceData["_region"]
		response["_authMethod"] = deviceData["_authMethod"]
		response["_startUrl"] = deviceData["_startUrl"]
	}

	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleOAuthExchange(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	provider := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "provider")))
	cfg, ok := oauthProviderRegistry.Get(provider)
	if !ok || (cfg.FlowType != oauth.FlowTypeAuthCode && cfg.FlowType != oauth.FlowTypeAuthCodePKCE) {
		writeJSON(w, http.StatusNotImplemented, map[string]any{"error": "OAuth provider is not supported", "code": "unsupported_oauth"})
		return
	}

	var body struct {
		Code         string         `json:"code"`
		RedirectURI  string         `json:"redirectUri"`
		CodeVerifier string         `json:"codeVerifier"`
		State        string         `json:"state"`
		Meta         map[string]any `json:"meta"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid or empty request body"})
		return
	}

	if strings.TrimSpace(body.Code) == "" || strings.TrimSpace(body.RedirectURI) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Missing required fields: code, redirectUri"})
		return
	}

	// Convert meta to string map
	meta := make(map[string]string)
	if body.Meta != nil {
		for k, v := range body.Meta {
			if s, ok := v.(string); ok {
				meta[k] = s
			}
		}
	}

	// Exchange token
	rawTokens, err := oauth.ExchangeToken(r.Context(), cfg, body.Code, body.RedirectURI, body.CodeVerifier, body.State, meta)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}

	// Post-exchange operations
	extra, err := oauth.PostExchange(r.Context(), cfg, rawTokens)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}

	// Map tokens
	tokenResult := oauth.MapTokens(cfg, rawTokens, extra)

	// Create connection
	expiresAt := time.Now().Add(time.Duration(tokenResult.ExpiresIn) * time.Second)
	conn, err := s.createOAuthConnection(r.Context(), storage.ProviderConnection{
		Provider:             provider,
		AuthType:             "oauth",
		AccessToken:          tokenResult.AccessToken,
		RefreshToken:         tokenResult.RefreshToken,
		ExpiresAt:            &expiresAt,
		Email:                tokenResult.Email,
		TestStatus:           "active",
		IsActive:             true,
		Enabled:              true,
		ProviderSpecificData: tokenResult.ProviderSpecificData,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":    true,
		"connection": oauthConnectionSummary(conn),
	})
}

func (s *Server) handleOAuthPoll(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	provider := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "provider")))
	cfg, ok := oauthProviderRegistry.Get(provider)
	if !ok || cfg.FlowType != oauth.FlowTypeDeviceCode {
		writeJSON(w, http.StatusNotImplemented, map[string]any{"success": false, "error": "Device code flow is not supported", "code": "unsupported_device_code", "pending": false})
		return
	}

	var body struct {
		DeviceCode   string         `json:"device_code"`
		CodeVerifier string         `json:"codeVerifier"`
		ExtraData    map[string]any `json:"extraData"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid or empty request body"})
		return
	}

	if strings.TrimSpace(body.DeviceCode) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Missing required field: device_code"})
		return
	}

	// Poll for token
	rawTokens, err := oauth.PollDeviceToken(r.Context(), cfg, body.DeviceCode, body.CodeVerifier, body.ExtraData)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"success": false, "error": err.Error(), "pending": false})
		return
	}

	// Check for pending/error states
	if errorCode, ok := rawTokens["error"].(string); ok {
		isPending := errorCode == "authorization_pending" || errorCode == "slow_down"
		writeJSON(w, http.StatusOK, map[string]any{
			"success":           false,
			"error":             errorCode,
			"error_description": rawTokens["error_description"],
			"pending":           isPending,
		})
		return
	}

	// Success - post-exchange operations
	extra, err := oauth.PostExchange(r.Context(), cfg, rawTokens)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"success": false, "error": err.Error(), "pending": false})
		return
	}

	// Map tokens
	tokenResult := oauth.MapTokens(cfg, rawTokens, extra)

	// Create connection
	expiresAt := time.Now().Add(time.Duration(tokenResult.ExpiresIn) * time.Second)
	conn, err := s.createOAuthConnection(r.Context(), storage.ProviderConnection{
		Provider:             provider,
		AuthType:             "oauth",
		AccessToken:          tokenResult.AccessToken,
		RefreshToken:         tokenResult.RefreshToken,
		ExpiresAt:            &expiresAt,
		Email:                tokenResult.Email,
		TestStatus:           "active",
		IsActive:             true,
		Enabled:              true,
		ProviderSpecificData: tokenResult.ProviderSpecificData,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "error": err.Error(), "pending": false})
		return
	}

	response := map[string]any{
		"success":    true,
		"connection": oauthConnectionSummary(conn),
	}

	// Add deprecated flag for Qwen
	if provider == "qwen" {
		response["deprecated"] = true
	}

	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleOAuthDelete(w http.ResponseWriter, r *http.Request, provider string) {
	defer r.Body.Close()

	db := s.db()
	if db == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Storage not enabled"})
		return
	}

	// Get connection ID from query or body
	connID := strings.TrimSpace(r.URL.Query().Get("id"))
	if connID == "" {
		var body struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err == nil {
			connID = strings.TrimSpace(body.ID)
		}
	}

	if connID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Missing connection id"})
		return
	}

	// Get the connection to verify provider
	conn, err := db.GetProviderConnection(r.Context(), connID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Connection not found"})
		return
	}

	if conn.Provider != provider {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Connection provider mismatch"})
		return
	}

	// Delete the connection from storage
	if err := db.DeleteProviderConnection(r.Context(), connID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	// Delete token from oauth store if present
	if s.oauthStore != nil {
		if err := s.oauthStore.DeleteToken(r.Context(), provider, conn.ID); err != nil {
			s.logger.Warn().Err(err).Str("provider", provider).Str("connection", connID).Msg("Failed to delete token from oauth store")
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// backfillCodexEmails scans existing codex connections and backfills missing email/chatgptAccountId from id_token.
func (s *Server) backfillCodexEmails(ctx context.Context) {
	db := s.db()
	if db == nil {
		return
	}

	connections, err := db.ListProviderConnections(ctx, storage.ProviderConnectionFilter{Provider: "codex"})
	if err != nil {
		s.logger.Warn().Err(err).Msg("backfillCodexEmails: failed to list connections")
		return
	}

	for _, conn := range connections {
		if conn.AuthType != "oauth" {
			continue
		}

		// Get id_token from providerSpecificData
		var idToken string
		if conn.ProviderSpecificData != nil {
			if it, ok := conn.ProviderSpecificData["idToken"].(string); ok {
				idToken = it
			}
		}
		if idToken == "" {
			continue
		}

		// Extract account info
		info := oauth.ExtractCodexAccountInfo(idToken)
		if info["email"] == "" && info["chatgptAccountId"] == "" {
			continue
		}

		// Build patch
		patch := make(map[string]any)
		if conn.Email == "" && info["email"] != "" {
			patch["email"] = info["email"]
		}

		if info["chatgptAccountId"] != "" || info["chatgptPlanType"] != "" {
			newProviderSpecificData := make(map[string]any)
			if conn.ProviderSpecificData != nil {
				for k, v := range conn.ProviderSpecificData {
					newProviderSpecificData[k] = v
				}
			}
			if info["chatgptAccountId"] != "" {
				newProviderSpecificData["chatgptAccountId"] = info["chatgptAccountId"]
			}
			if info["chatgptPlanType"] != "" {
				newProviderSpecificData["chatgptPlanType"] = info["chatgptPlanType"]
			}
			patch["providerSpecificData"] = newProviderSpecificData
		}

		// Apply patch if needed
		if len(patch) > 0 {
			// Update connection with new values
			updatedConn := conn
			if email, ok := patch["email"].(string); ok {
				updatedConn.Email = email
			}
			if providerSpecificData, ok := patch["providerSpecificData"].(map[string]any); ok {
				updatedConn.ProviderSpecificData = providerSpecificData
			}
			if _, err := db.UpdateProviderConnection(ctx, conn.ID, updatedConn); err != nil {
				s.logger.Warn().Err(err).Str("connection", conn.ID).Msg("backfillCodexEmails: failed to update connection")
			} else {
				s.logger.Info().Str("connection", conn.ID).Msg("backfillCodexEmails: updated connection")
			}
		}
	}
}

func (s *Server) handleCursorAutoImport(w http.ResponseWriter, r *http.Request) {
	candidates := cursorCandidatePaths(os.Getenv("HOME"), runtime.GOOS, os.Getenv("APPDATA"), os.Getenv("LOCALAPPDATA"))
	dbPath := ""
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			dbPath = candidate
			break
		}
	}
	if dbPath == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"found": false,
			"error": fmt.Sprintf("Cursor database not found. Checked locations:\n%s\n\nMake sure Cursor IDE is installed and opened at least once.", strings.Join(candidates, "\n")),
		})
		return
	}
	accessToken, machineID, err := readCursorStateDB(dbPath)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"found": false, "error": err.Error()})
		return
	}
	if accessToken == "" || machineID == "" {
		writeJSON(w, http.StatusOK, map[string]any{"found": false, "windowsManual": true, "dbPath": dbPath})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"found": true, "accessToken": accessToken, "machineId": machineID})
}

func cursorCandidatePaths(homeDir, goos, appData, localAppData string) []string {
	if strings.TrimSpace(homeDir) == "" {
		homeDir, _ = os.UserHomeDir()
	}
	switch goos {
	case "darwin":
		return []string{
			filepath.Join(homeDir, "Library/Application Support/Cursor/User/globalStorage/state.vscdb"),
			filepath.Join(homeDir, "Library/Application Support/Cursor - Insiders/User/globalStorage/state.vscdb"),
		}
	case "windows":
		if strings.TrimSpace(appData) == "" {
			appData = filepath.Join(homeDir, "AppData", "Roaming")
		}
		if strings.TrimSpace(localAppData) == "" {
			localAppData = filepath.Join(homeDir, "AppData", "Local")
		}
		return []string{
			filepath.Join(appData, "Cursor", "User", "globalStorage", "state.vscdb"),
			filepath.Join(appData, "Cursor - Insiders", "User", "globalStorage", "state.vscdb"),
			filepath.Join(localAppData, "Cursor", "User", "globalStorage", "state.vscdb"),
			filepath.Join(localAppData, "Programs", "Cursor", "User", "globalStorage", "state.vscdb"),
		}
	default:
		return []string{
			filepath.Join(homeDir, ".config", "Cursor", "User", "globalStorage", "state.vscdb"),
			filepath.Join(homeDir, ".config", "cursor", "User", "globalStorage", "state.vscdb"),
		}
	}
}

func readCursorStateDB(dbPath string) (string, string, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return "", "", fmt.Errorf("open cursor database: %w", err)
	}
	defer db.Close()
	accessToken := ""
	for _, key := range []string{"cursorAuth/accessToken", "cursorAuth/token"} {
		value, ok, err := readCursorStateValue(db, key)
		if err != nil {
			return "", "", fmt.Errorf("read cursor access token: %w", err)
		}
		if ok && strings.TrimSpace(value) != "" {
			accessToken = normalizeCursorStateValue(value)
			break
		}
	}
	machineID := ""
	for _, key := range []string{"storage.serviceMachineId", "storage.machineId", "telemetry.machineId"} {
		value, ok, err := readCursorStateValue(db, key)
		if err != nil {
			return "", "", fmt.Errorf("read cursor machine id: %w", err)
		}
		if ok && strings.TrimSpace(value) != "" {
			machineID = normalizeCursorStateValue(value)
			break
		}
	}
	return accessToken, machineID, nil
}

func readCursorStateValue(db *sql.DB, key string) (string, bool, error) {
	var value string
	err := db.QueryRow(`SELECT value FROM itemTable WHERE key = ? LIMIT 1`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

func normalizeCursorStateValue(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	var decoded string
	if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil {
		return decoded
	}
	return trimmed
}

func (s *Server) handleKiroImport(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var body struct {
		RefreshToken string `json:"refreshToken"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid request body"})
		return
	}
	if strings.TrimSpace(body.RefreshToken) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Refresh token is required"})
		return
	}
	validated, err := validateKiroImportToken(r.Context(), strings.TrimSpace(body.RefreshToken))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	conn, err := s.createOAuthConnection(r.Context(), storage.ProviderConnection{
		Provider:     "kiro",
		AuthType:     "oauth",
		AccessToken:  validated.AccessToken,
		RefreshToken: validated.RefreshToken,
		ExpiresAt:    validated.ExpiresAt,
		Email:        validated.Email,
		ProviderSpecificData: map[string]any{
			"profileArn": validated.ProfileARN,
			"authMethod": "imported",
			"provider":   "Imported",
		},
		TestStatus: "active",
		IsActive:   true,
		Enabled:    true,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "connection": oauthConnectionSummary(conn)})
}

func (s *Server) handleKiroAutoImport(w http.ResponseWriter, r *http.Request) {
	homeDir := os.Getenv("HOME")
	if strings.TrimSpace(homeDir) == "" {
		homeDir, _ = os.UserHomeDir()
	}
	cacheDir := filepath.Join(homeDir, ".aws", "sso", "cache")
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"found": false, "error": "AWS SSO cache not found. Please login to Kiro IDE first."})
		return
	}
	if token, source := findKiroRefreshToken(cacheDir, entries); token != "" {
		writeJSON(w, http.StatusOK, map[string]any{"found": true, "refreshToken": token, "source": source})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"found": false, "error": "Kiro token not found in AWS SSO cache. Please login to Kiro IDE first."})
}

func (s *Server) handleKiroSocialAuthorize(w http.ResponseWriter, r *http.Request) {
	provider := strings.TrimSpace(r.URL.Query().Get("provider"))
	if provider != "google" && provider != "github" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid provider. Use 'google' or 'github'"})
		return
	}
	codeVerifier, codeChallenge, err := generatePKCEPair()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	state, err := generateStateToken()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	idp := "Github"
	if provider == "google" {
		idp = "Google"
	}
	redirectURI := "kiro://kiro.kiroAgent/authenticate-success"
	authURL := kiroOAuthEndpoints.SocialLoginURL + "?idp=" + url.QueryEscape(idp) +
		"&redirect_uri=" + url.QueryEscape(redirectURI) +
		"&code_challenge=" + url.QueryEscape(codeChallenge) +
		"&code_challenge_method=S256&state=" + url.QueryEscape(state) +
		"&prompt=select_account"
	writeJSON(w, http.StatusOK, map[string]any{
		"authUrl":       authURL,
		"state":         state,
		"codeVerifier":  codeVerifier,
		"codeChallenge": codeChallenge,
		"provider":      provider,
	})
}

func (s *Server) handleKiroSocialExchange(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var body struct {
		Code         string `json:"code"`
		CodeVerifier string `json:"codeVerifier"`
		Provider     string `json:"provider"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid request body"})
		return
	}
	if strings.TrimSpace(body.Code) == "" || strings.TrimSpace(body.CodeVerifier) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Missing required fields"})
		return
	}
	provider := strings.TrimSpace(body.Provider)
	if provider != "google" && provider != "github" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid provider"})
		return
	}
	tokens, err := exchangeKiroSocialCode(r.Context(), strings.TrimSpace(body.Code), strings.TrimSpace(body.CodeVerifier))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	expiresAt := time.Now().Add(time.Duration(tokens.ExpiresIn) * time.Second)
	conn, err := s.createOAuthConnection(r.Context(), storage.ProviderConnection{
		Provider:     "kiro",
		AuthType:     "oauth",
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		ExpiresAt:    &expiresAt,
		Email:        extractJWTEmail(tokens.AccessToken),
		ProviderSpecificData: map[string]any{
			"profileArn": tokens.ProfileARN,
			"authMethod": provider,
			"provider":   strings.ToUpper(provider[:1]) + provider[1:],
		},
		TestStatus: "active",
		IsActive:   true,
		Enabled:    true,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "connection": oauthConnectionSummary(conn)})
}

type kiroValidatedToken struct {
	AccessToken  string
	RefreshToken string
	ProfileARN   string
	ExpiresAt    *time.Time
	Email        string
}

func validateKiroImportToken(ctx context.Context, refreshToken string) (kiroValidatedToken, error) {
	if !strings.HasPrefix(refreshToken, "aorAAAAAG") {
		return kiroValidatedToken{}, fmt.Errorf("Invalid token format. Token should start with aorAAAAAG...")
	}
	payload := map[string]string{"refreshToken": refreshToken}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, kiroOAuthEndpoints.SocialRefreshURL, bytes.NewReader(body))
	if err != nil {
		return kiroValidatedToken{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return kiroValidatedToken{}, fmt.Errorf("Token validation failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return kiroValidatedToken{}, fmt.Errorf("Token validation failed: %s", strings.TrimSpace(string(respBody)))
	}
	var parsed struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ProfileARN   string `json:"profileArn"`
		ExpiresIn    int    `json:"expiresIn"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return kiroValidatedToken{}, fmt.Errorf("Token validation failed: %w", err)
	}
	expiresAt := time.Now().Add(time.Duration(parsed.ExpiresIn) * time.Second)
	return kiroValidatedToken{
		AccessToken:  parsed.AccessToken,
		RefreshToken: firstNonEmpty(parsed.RefreshToken, refreshToken),
		ProfileARN:   parsed.ProfileARN,
		ExpiresAt:    &expiresAt,
		Email:        extractJWTEmail(parsed.AccessToken),
	}, nil
}

func exchangeKiroSocialCode(ctx context.Context, code, codeVerifier string) (struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ProfileARN   string `json:"profileArn"`
	ExpiresIn    int    `json:"expiresIn"`
}, error) {
	payload := map[string]string{
		"code":          code,
		"code_verifier": codeVerifier,
		"redirect_uri":  "kiro://kiro.kiroAgent/authenticate-success",
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, kiroOAuthEndpoints.SocialTokenURL, bytes.NewReader(body))
	if err != nil {
		return struct {
			AccessToken  string `json:"accessToken"`
			RefreshToken string `json:"refreshToken"`
			ProfileARN   string `json:"profileArn"`
			ExpiresIn    int    `json:"expiresIn"`
		}{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return struct {
			AccessToken  string `json:"accessToken"`
			RefreshToken string `json:"refreshToken"`
			ProfileARN   string `json:"profileArn"`
			ExpiresIn    int    `json:"expiresIn"`
		}{}, fmt.Errorf("Token exchange failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return struct {
			AccessToken  string `json:"accessToken"`
			RefreshToken string `json:"refreshToken"`
			ProfileARN   string `json:"profileArn"`
			ExpiresIn    int    `json:"expiresIn"`
		}{}, fmt.Errorf("Token exchange failed: %s", strings.TrimSpace(string(respBody)))
	}
	var parsed struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ProfileARN   string `json:"profileArn"`
		ExpiresIn    int    `json:"expiresIn"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return parsed, fmt.Errorf("Token exchange failed: %w", err)
	}
	if parsed.ExpiresIn == 0 {
		parsed.ExpiresIn = 3600
	}
	return parsed, nil
}

func findKiroRefreshToken(cacheDir string, entries []os.DirEntry) (string, string) {
	preferred := filepath.Join(cacheDir, "kiro-auth-token.json")
	if token := readKiroRefreshTokenFile(preferred); token != "" {
		return token, "kiro-auth-token.json"
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(cacheDir, entry.Name())
		if token := readKiroRefreshTokenFile(path); token != "" {
			return token, entry.Name()
		}
	}
	return "", ""
}

func readKiroRefreshTokenFile(path string) string {
	body, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	token := strings.TrimSpace(stringValue(payload["refreshToken"]))
	if strings.HasPrefix(token, "aorAAAAAG") {
		return token
	}
	return ""
}

func generateStateToken() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func extractJWTEmail(accessToken string) string {
	parts := strings.Split(accessToken, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	return firstNonEmpty(stringValue(claims["email"]), stringValue(claims["preferred_username"]), stringValue(claims["sub"]))
}

func (s *Server) handleIFlowCookie(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var body struct {
		Cookie string `json:"cookie"`
		APIKey string `json:"apiKey"`
		Name   string `json:"name"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid request body"})
		return
	}
	cookie := strings.TrimSpace(body.Cookie)
	if cookie == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Cookie is required"})
		return
	}
	if !strings.Contains(cookie, "BXAuth=") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Cookie must contain BXAuth field"})
		return
	}
	if !strings.HasSuffix(cookie, ";") {
		cookie += ";"
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = "iflow-cookie"
	}
	conn, err := s.createOAuthConnection(r.Context(), storage.ProviderConnection{
		Provider:   "iflow",
		AuthType:   "cookie",
		Name:       name,
		Email:      name,
		APIKey:     strings.TrimSpace(body.APIKey),
		TestStatus: "active",
		IsActive:   true,
		Enabled:    true,
		ProviderSpecificData: map[string]any{
			"cookie": cookie,
		},
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	summary := oauthConnectionSummary(conn)
	if body.APIKey != "" {
		summary["apiKey"] = redactSecret(body.APIKey)
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "connection": summary})
}

func (s *Server) handleWebCookie(w http.ResponseWriter, r *http.Request, provider string) {
	defer r.Body.Close()
	var body struct {
		Cookie string `json:"cookie"`
		Name   string `json:"name"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid request body"})
		return
	}
	cookie, token, err := normalizeWebProviderCookie(provider, body.Cookie)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = provider + "-cookie"
	}
	conn, err := s.createOAuthConnection(r.Context(), storage.ProviderConnection{
		Provider:   provider,
		AuthType:   "cookie",
		Name:       name,
		Email:      name,
		APIKey:     token,
		TestStatus: "stored",
		IsActive:   true,
		Enabled:    true,
		ProviderSpecificData: map[string]any{
			"cookieName": webCookieName(provider),
			"cookie":     cookie,
			"redacted":   redactSecret(token),
			"runtime":    "supported",
		},
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	summary := oauthConnectionSummary(conn)
	summary["cookie"] = redactSecret(token)
	summary["runtime"] = "supported"
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "connection": summary})
}

func (s *Server) handleGitLabPAT(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var body struct {
		Token   string `json:"token"`
		BaseURL string `json:"baseUrl"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid request body"})
		return
	}
	token := strings.TrimSpace(body.Token)
	if token == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Personal Access Token is required"})
		return
	}
	baseURL := strings.TrimRight(strings.TrimSpace(body.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://gitlab.com"
	}
	if _, err := s.createOAuthConnection(r.Context(), storage.ProviderConnection{
		Provider:    "gitlab",
		AuthType:    "oauth",
		AccessToken: token,
		TestStatus:  "active",
		IsActive:    true,
		Enabled:     true,
		ProviderSpecificData: map[string]any{
			"baseUrl":  baseURL,
			"authKind": "personal_access_token",
		},
	}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) handleGitHubDeviceCode(w http.ResponseWriter, r *http.Request) {
	form := url.Values{}
	form.Set("client_id", githubOAuthEndpoints.ClientID)
	form.Set("scope", "read:user")
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, githubOAuthEndpoints.DeviceCodeURL, strings.NewReader(form.Encode()))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	defer resp.Body.Close()
	var payload map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "Invalid GitHub device-code response"})
		return
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		writeJSON(w, resp.StatusCode, map[string]any{"error": stringValue(payload["error"]), "errorDescription": stringValue(payload["error_description"])})
		return
	}
	if _, ok := payload["codeVerifier"]; !ok {
		payload["codeVerifier"] = ""
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleGitHubPoll(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var body struct {
		DeviceCode string `json:"deviceCode"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid or empty request body"})
		return
	}
	if strings.TrimSpace(body.DeviceCode) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Missing device code"})
		return
	}
	token, err := pollGitHubDeviceToken(r.Context(), strings.TrimSpace(body.DeviceCode))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"success": false, "error": err.Error(), "pending": false})
		return
	}
	if token.Error != "" {
		pending := token.Error == "authorization_pending" || token.Error == "slow_down"
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": token.Error, "errorDescription": token.ErrorDescription, "pending": pending})
		return
	}
	copilotToken, copilotExpiresAt := fetchGitHubCopilotToken(r.Context(), token.AccessToken)
	userInfo := fetchGitHubUserInfo(r.Context(), token.AccessToken)
	expiresAt := (*time.Time)(nil)
	if token.ExpiresIn > 0 {
		t := time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
		expiresAt = &t
	}
	conn, err := s.createOAuthConnection(r.Context(), storage.ProviderConnection{
		Provider:     "github",
		AuthType:     "oauth",
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    expiresAt,
		Email:        stringValue(userInfo["email"]),
		DisplayName:  firstNonEmpty(stringValue(userInfo["name"]), stringValue(userInfo["login"]), "github"),
		TestStatus:   "active",
		IsActive:     true,
		Enabled:      true,
		ProviderSpecificData: map[string]any{
			"copilotToken":          copilotToken,
			"copilotTokenExpiresAt": copilotExpiresAt,
			"githubUserId":          userInfo["id"],
			"githubLogin":           userInfo["login"],
			"githubName":            userInfo["name"],
			"githubEmail":           userInfo["email"],
		},
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "error": err.Error(), "pending": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "connection": oauthConnectionSummary(conn)})
}

func (s *Server) handleQwenDeviceCode(w http.ResponseWriter, r *http.Request) {
	codeVerifier, codeChallenge, err := generatePKCEPair()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	form := url.Values{}
	form.Set("client_id", qwenOAuthEndpoints.ClientID)
	form.Set("scope", qwenOAuthEndpoints.Scope)
	form.Set("code_challenge", codeChallenge)
	form.Set("code_challenge_method", "S256")
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, qwenOAuthEndpoints.DeviceCodeURL, strings.NewReader(form.Encode()))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	defer resp.Body.Close()
	var payload map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "Invalid Qwen device-code response"})
		return
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		writeJSON(w, resp.StatusCode, map[string]any{"error": stringValue(payload["error"]), "errorDescription": stringValue(payload["error_description"])})
		return
	}
	payload["codeVerifier"] = codeVerifier
	payload["deprecated"] = true
	payload["warning"] = "Qwen OAuth free tier was discontinued on 2026-04-15; runtime requires an existing valid token."
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleQwenPoll(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var body struct {
		DeviceCode   string `json:"deviceCode"`
		CodeVerifier string `json:"codeVerifier"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid or empty request body"})
		return
	}
	if strings.TrimSpace(body.DeviceCode) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Missing device code"})
		return
	}
	if strings.TrimSpace(body.CodeVerifier) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Missing PKCE code verifier"})
		return
	}
	token, err := pollQwenDeviceToken(r.Context(), strings.TrimSpace(body.DeviceCode), strings.TrimSpace(body.CodeVerifier))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"success": false, "error": err.Error(), "pending": false})
		return
	}
	if token.Error != "" {
		pending := token.Error == "authorization_pending" || token.Error == "slow_down"
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": token.Error, "errorDescription": token.ErrorDescription, "pending": pending})
		return
	}
	expiresAt := (*time.Time)(nil)
	if token.ExpiresIn > 0 {
		t := time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
		expiresAt = &t
	}
	conn, err := s.createOAuthConnection(r.Context(), storage.ProviderConnection{
		Provider:     "qwen",
		AuthType:     "oauth",
		Name:         "qwen-oauth",
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    expiresAt,
		TestStatus:   "active",
		IsActive:     true,
		Enabled:      true,
		ProviderSpecificData: map[string]any{
			"resourceUrl": token.ResourceURL,
			"deprecated":  true,
			"warning":     "Qwen OAuth free tier was discontinued on 2026-04-15; runtime requires an existing valid token.",
		},
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "error": err.Error(), "pending": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "connection": oauthConnectionSummary(conn), "deprecated": true})
}

func (s *Server) handleOAuthRefresh(w http.ResponseWriter, r *http.Request, provider string) {
	defer r.Body.Close()
	var body struct {
		Account      string `json:"account"`
		ConnectionID string `json:"connectionId"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	}
	account := firstNonEmpty(strings.TrimSpace(body.Account), "default")
	cfg, ok := oauthProviderRegistry.Get(provider)
	if !ok || cfg.TokenURL == "" {
		writeJSON(w, http.StatusNotImplemented, map[string]any{"success": false, "error": "OAuth refresh is not supported", "code": "unsupported_refresh"})
		return
	}

	if s.oauthStore != nil {
		rec, err := s.oauthStore.GetToken(r.Context(), provider, account)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "error": err.Error(), "refreshed": false})
			return
		}
		if rec != nil {
			refreshed, err := oauth.RefreshToken(r.Context(), cfg, rec)
			if err != nil {
				writeJSON(w, http.StatusBadGateway, map[string]any{"success": false, "error": err.Error(), "refreshed": false})
				return
			}
			if err := s.oauthStore.SaveToken(r.Context(), *refreshed); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "error": err.Error(), "refreshed": false})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"success": true, "provider": provider, "account": refreshed.Account, "expires_at": refreshed.ExpiresAt, "refreshed": true})
			return
		}
	}

	if db := s.db(); db != nil {
		conn, err := findRefreshableConnection(r.Context(), db, provider, body.ConnectionID)
		if err == nil && conn.RefreshToken != "" {
			rec := &oauth.TokenRecord{Provider: provider, Account: account, RefreshToken: conn.RefreshToken, Scope: strings.Join(cfg.Scopes, " ")}
			refreshed, err := oauth.RefreshToken(r.Context(), cfg, rec)
			if err != nil {
				writeJSON(w, http.StatusBadGateway, map[string]any{"success": false, "error": err.Error(), "refreshed": false})
				return
			}
			conn.AccessToken = refreshed.AccessToken
			conn.RefreshToken = refreshed.RefreshToken
			conn.ExpiresAt = &refreshed.ExpiresAt
			if _, err := db.UpdateProviderConnection(r.Context(), conn.ID, conn); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "error": err.Error(), "refreshed": false})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"success": true, "connection": oauthConnectionSummary(conn), "expires_at": refreshed.ExpiresAt, "refreshed": true})
			return
		}
	}

	writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "error": "refreshable token not found", "code": "token_not_found", "refreshed": false})
}

func findRefreshableConnection(ctx context.Context, db *storage.DB, provider, connectionID string) (storage.ProviderConnection, error) {
	if strings.TrimSpace(connectionID) != "" {
		return db.GetProviderConnection(ctx, connectionID)
	}
	conns, err := db.ListProviderConnections(ctx, storage.ProviderConnectionFilter{Provider: provider})
	if err != nil {
		return storage.ProviderConnection{}, err
	}
	for _, conn := range conns {
		if conn.RefreshToken != "" {
			return conn, nil
		}
	}
	return storage.ProviderConnection{}, fmt.Errorf("refreshable connection not found")
}

type githubDeviceTokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	ExpiresIn        int    `json:"expires_in"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

type qwenDeviceTokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	ExpiresIn        int    `json:"expires_in"`
	ResourceURL      string `json:"resource_url"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func pollGitHubDeviceToken(ctx context.Context, deviceCode string) (githubDeviceTokenResponse, error) {
	form := url.Values{}
	form.Set("client_id", githubOAuthEndpoints.ClientID)
	form.Set("device_code", deviceCode)
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, githubOAuthEndpoints.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return githubDeviceTokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return githubDeviceTokenResponse{}, err
	}
	defer resp.Body.Close()
	var out githubDeviceTokenResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return githubDeviceTokenResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if out.Error == "" {
			out.Error = fmt.Sprintf("github_token_status_%d", resp.StatusCode)
		}
	}
	return out, nil
}

func pollQwenDeviceToken(ctx context.Context, deviceCode string, codeVerifier string) (qwenDeviceTokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
	form.Set("client_id", qwenOAuthEndpoints.ClientID)
	form.Set("device_code", deviceCode)
	form.Set("code_verifier", codeVerifier)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, qwenOAuthEndpoints.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return qwenDeviceTokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return qwenDeviceTokenResponse{}, err
	}
	defer resp.Body.Close()
	var out qwenDeviceTokenResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return qwenDeviceTokenResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if out.Error == "" {
			out.Error = fmt.Sprintf("qwen_token_status_%d", resp.StatusCode)
		}
	}
	return out, nil
}

func fetchGitHubCopilotToken(ctx context.Context, accessToken string) (string, any) {
	if accessToken == "" {
		return "", nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubOAuthEndpoints.CopilotTokenURL, nil)
	if err != nil {
		return "", nil
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "GitHubCopilotChat/0.26.7")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", nil
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", nil
	}
	var payload map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return "", nil
	}
	return stringValue(payload["token"]), payload["expires_at"]
}

func fetchGitHubUserInfo(ctx context.Context, accessToken string) map[string]any {
	if accessToken == "" {
		return map[string]any{}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubOAuthEndpoints.UserInfoURL, nil)
	if err != nil {
		return map[string]any{}
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "GitHubCopilotChat/0.26.7")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return map[string]any{}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return map[string]any{}
	}
	var payload map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return map[string]any{}
	}
	return payload
}

func (s *Server) createOAuthConnection(ctx context.Context, conn storage.ProviderConnection) (storage.ProviderConnection, error) {
	db := s.db()
	if db == nil {
		return storage.ProviderConnection{}, fmt.Errorf("storage not enabled")
	}
	if conn.Name == "" {
		conn.Name = conn.Provider
	}
	if conn.DisplayName == "" {
		conn.DisplayName = conn.Name
	}
	return db.CreateProviderConnection(ctx, conn)
}

func oauthConnectionSummary(conn storage.ProviderConnection) map[string]any {
	return map[string]any{
		"id":       conn.ID,
		"provider": conn.Provider,
		"email":    conn.Email,
	}
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

func (s *Server) handleCombosGet(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	route, ok := s.runtimeConfig.ListRoutes()[name]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Combo not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": name, "name": name, "strategy": route.Strategy, "targets": route.Targets})
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
		items = append(items, adminKeyResponse(strconv.Itoa(i), key, false))
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
	req.APIKey = strings.TrimSpace(req.APIKey)
	generated := false
	if req.APIKey == "" {
		key, err := generateAdminAPIKey()
		if err != nil {
			writeOpenAIError(w, http.StatusInternalServerError, "failed to generate api key", "internal_error", "")
			return
		}
		req.APIKey = key
		generated = true
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
	response := adminKeyResponse(strconv.Itoa(len(s.runtimeConfig.ListAdminAPIKeys())-1), req.APIKey, true)
	response["message"] = "key created"
	response["generated"] = generated
	writeJSON(w, http.StatusCreated, response)
}

func (s *Server) handleKeysGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	idx, err := strconv.Atoi(id)
	if err != nil || idx < 0 {
		writeOpenAIError(w, http.StatusBadRequest, "invalid key id", "invalid_request_error", "")
		return
	}
	keys := s.runtimeConfig.ListAdminAPIKeys()
	if idx >= len(keys) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Key not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": adminKeyResponse(id, keys[idx], false)})
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

func (s *Server) handleModelAliasesPut(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req struct {
		Alias    string `json:"alias"`
		Model    string `json:"model"`
		Provider string `json:"provider,omitempty"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid json", "invalid_request_error", "")
		return
	}
	if req.Alias == "" || req.Model == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Model and alias required"})
		return
	}
	provider := req.Provider
	model := req.Model
	if strings.Contains(req.Model, "/") && req.Provider == "" {
		provider, model = parseTranslatorModel(req.Model)
	}
	alias := config.ModelAlias{Provider: provider, Model: model}
	if err := s.runtimeConfig.UpdateWithReconfigure(func(cfg *config.Config) error {
		if cfg.ModelAliases == nil {
			cfg.ModelAliases = make(map[string]config.ModelAlias)
		}
		cfg.ModelAliases[req.Alias] = alias
		return nil
	}, s.reconfigureFromConfig); err != nil {
		writeJSON(w, mapConfigErrorToHTTP(err), map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "model": req.Model, "alias": req.Alias})
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

func (s *Server) handleModelAliasesDeleteQuery(w http.ResponseWriter, r *http.Request) {
	alias := r.URL.Query().Get("alias")
	if alias == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Alias required"})
		return
	}
	if err := s.runtimeConfig.UpdateWithReconfigure(func(cfg *config.Config) error {
		if _, exists := cfg.ModelAliases[alias]; !exists {
			return fmt.Errorf("model alias '%s' not found", alias)
		}
		delete(cfg.ModelAliases, alias)
		return nil
	}, s.reconfigureFromConfig); err != nil {
		writeJSON(w, mapConfigErrorToHTTP(err), map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
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

func (s *Server) handleModelsCustomDeleteQuery(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	providerAlias := r.URL.Query().Get("providerAlias")
	if id == "" || providerAlias == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "providerAlias and id required"})
		return
	}
	name := providerAlias + "/" + id
	if err := s.runtimeConfig.UpdateWithReconfigure(func(cfg *config.Config) error {
		if _, exists := cfg.CustomModels[name]; exists {
			delete(cfg.CustomModels, name)
			return nil
		}
		if _, exists := cfg.CustomModels[id]; exists {
			delete(cfg.CustomModels, id)
			return nil
		}
		return fmt.Errorf("custom model '%s' not found", id)
	}, s.reconfigureFromConfig); err != nil {
		writeJSON(w, mapConfigErrorToHTTP(err), map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) handleSettingsGet(w http.ResponseWriter, r *http.Request) {
	cfg := s.runtimeConfig.Get()
	writeJSON(w, http.StatusOK, cfg.Settings)
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
		apiLogs = append(apiLogs, apiRequestLog(log))
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

func apiRequestLog(log storage.RequestLog) map[string]any {
	item := map[string]any{
		"request_id":        log.RequestID,
		"id":                log.RequestID,
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
		"input_cost":        log.InputCost,
		"output_cost":       log.OutputCost,
		"total_cost":        log.TotalCost,
		"currency":          log.Currency,
	}
	if log.ErrorMessage != "" {
		item["error_message"] = log.ErrorMessage
	}
	return item
}

func (s *Server) handleModels(w http.ResponseWriter, _ *http.Request) {
	models := make([]map[string]any, 0)

	// Add routes (combos/fallback chains)
	routesMap := s.runtimeConfig.ListRoutes()
	for alias := range routesMap {
		models = append(models, map[string]any{
			"id":       alias,
			"object":   "model",
			"type":     "route",
			"owned_by": "9router",
			"created":  0,
		})
	}

	// Add model aliases
	aliasesMap := s.runtimeConfig.ListModelAliases()
	for alias := range aliasesMap {
		models = append(models, map[string]any{
			"id":       alias,
			"object":   "model",
			"type":     "alias",
			"owned_by": "9router",
			"created":  0,
		})
	}

	// Add custom models
	customModels := s.runtimeConfig.ListCustomModels()
	for name, cm := range customModels {
		entry := map[string]any{
			"id":       name,
			"object":   "model",
			"type":     "custom",
			"owned_by": "9router",
			"created":  0,
		}
		if cm.Description != "" {
			entry["description"] = cm.Description
		}
		models = append(models, entry)
	}

	// Add provider/model wildcards for enabled providers
	providersList := s.runtimeConfig.ListProviders()
	for _, provider := range providersList {
		if provider.Enabled {
			models = append(models, map[string]any{
				"id":       provider.Name + "/*",
				"object":   "model",
				"type":     "provider",
				"owned_by": provider.Name,
				"created":  0,
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

	// Evaluate policy rules
	if s.policyEngine != nil {
		apiKey := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		policyResult := s.policyEngine.Evaluate(policy.Request{
			Model:  request.Model,
			APIKey: apiKey,
		})
		if policyResult.Matched {
			switch policyResult.Action {
			case policy.ActionDeny:
				s.metrics.mu.Lock()
				s.metrics.RequestsError++
				s.metrics.mu.Unlock()
				writeOpenAIError(w, http.StatusForbidden, policyResult.DenyMessage, "invalid_request_error", "policy_denied")
				return
			case policy.ActionReroute:
				policy.ApplyToRequest(&request, policyResult)
				s.logger.Debug().Str("policy", policyResult.Policy.Name).Str("model", request.Model).Msg("policy: rerouted request")
			case policy.ActionTag:
				s.logger.Info().Str("policy", policyResult.Policy.Name).Str("tag", policyResult.Tag).Str("model", request.Model).Msg("policy: tagged request")
			}
		}
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

		locale := i18n.Locale(cfg.Settings.Locale)
		if locale == "" {
			locale = i18n.LocaleEnglish
		}
		errMsg := s.i18nBundle.T(locale, "error.all_providers_down")
		if errMsg == "" || errMsg == "error.all_providers_down" {
			errMsg = err.Error()
		}
		status := http.StatusBadGateway
		if strings.Contains(err.Error(), "router is not configured yet") || strings.Contains(err.Error(), "provider not registered") {
			status = http.StatusServiceUnavailable
			errMsg = err.Error()
		}
		writeOpenAIError(w, status, errMsg, "api_error", "")
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

		status := http.StatusBadGateway
		if strings.Contains(err.Error(), "router is not configured yet") || strings.Contains(err.Error(), "provider not registered") {
			status = http.StatusServiceUnavailable
		}
		writeOpenAIError(w, status, err.Error(), "api_error", "")
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
		Input       interface{}                `json:"input"`
		Model       string                     `json:"model"`
		Temperature *float64                   `json:"temperature,omitempty"`
		TopP        *float64                   `json:"top_p,omitempty"`
		MaxTokens   *int                       `json:"max_tokens,omitempty"`
		Reasoning   any                        `json:"reasoning,omitempty"`
		Store       *bool                      `json:"store,omitempty"`
		Extra       map[string]json.RawMessage `json:"-"`
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
	chatReq.Extra = map[string]json.RawMessage{}
	if isCompactResponsesPath(r.URL.Path) {
		chatReq.Extra["_compact"] = json.RawMessage(`true`)
	}
	if strings.HasPrefix(r.URL.Path, "/codex/") {
		chatReq.Extra["codex_compat"] = json.RawMessage(`true`)
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

		status := http.StatusBadGateway
		if strings.Contains(err.Error(), "router is not configured yet") || strings.Contains(err.Error(), "provider not registered") {
			status = http.StatusServiceUnavailable
		}
		writeOpenAIError(w, status, err.Error(), "api_error", "")
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

	var req providers.AudioSpeechRequest
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

		// Call provider's AudioSpeech method
		audioReq := req
		audioReq.Model = target.Model
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

		if strings.EqualFold(req.ResponseFormat, "json") {
			format := "mp3"
			if strings.Contains(response.ContentType, "/") {
				format = strings.TrimSpace(strings.Split(strings.Split(response.ContentType, ";")[0], "/")[1])
			}
			if format == "mpeg" {
				format = "mp3"
			}
			writeJSON(w, http.StatusOK, map[string]any{"audio": base64.StdEncoding.EncodeToString(response.Data), "format": format})
			return
		}

		w.Header().Set("Content-Type", response.ContentType)
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(response.Data); err != nil {
			s.logger.Error().Err(err).Msg("failed to write audio response")
		}
		return
	}

	// All targets exhausted
	writeOpenAIError(w, http.StatusInternalServerError, fmt.Sprintf("all audio/speech targets failed: %s", strings.Join(allErrors, " | ")), "internal_error", "")
}

func (s *Server) handleAudioTranscriptions(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var req providers.AudioTranscriptionRequest
	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "multipart/form-data") {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			writeOpenAIError(w, http.StatusBadRequest, "invalid multipart form", "invalid_request_error", "")
			return
		}
		req.Model = r.FormValue("model")
		req.Language = r.FormValue("language")
		req.Prompt = r.FormValue("prompt")
		if file, _, err := r.FormFile("file"); err == nil {
			defer file.Close()
			data, err := io.ReadAll(io.LimitReader(file, 32<<20))
			if err != nil {
				writeOpenAIError(w, http.StatusBadRequest, "failed to read audio file", "invalid_request_error", "")
				return
			}
			req.AudioBase64 = base64.StdEncoding.EncodeToString(data)
		}
	} else {
		if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&req); err != nil {
			writeOpenAIError(w, http.StatusBadRequest, "invalid json", "invalid_request_error", "")
			return
		}
	}
	if req.Model == "" {
		writeOpenAIError(w, http.StatusBadRequest, "model is required", "invalid_request_error", "")
		return
	}
	if strings.TrimSpace(req.AudioURL) == "" && strings.TrimSpace(req.AudioBase64) == "" {
		writeOpenAIError(w, http.StatusBadRequest, "audio_url or audio_base64 is required", "invalid_request_error", "")
		return
	}

	targets := s.engine.ResolveTargets(req.Model)
	if len(targets) == 0 {
		writeOpenAIError(w, http.StatusNotFound, "model not found", "invalid_request_error", "")
		return
	}
	var allErrors []string
	for targetIdx, target := range targets {
		adapter, err := s.engine.GetRegistry().Get(target.Provider)
		if err != nil {
			allErrors = append(allErrors, fmt.Sprintf("target[%d] %s: provider not found", targetIdx, target.Provider))
			continue
		}
		transcriber, ok := adapter.(providers.AudioTranscriber)
		if !ok {
			err := providers.NewNonRetryableError(target.Provider, target.Model, "audio transcription not supported", nil)
			writeOpenAIError(w, http.StatusBadRequest, fmt.Sprintf("audio/transcriptions failed: %v", err), "invalid_request_error", "")
			return
		}
		response, err := transcriber.TranscribeAudio(r.Context(), req, target.Model)
		if err != nil {
			allErrors = append(allErrors, fmt.Sprintf("target[%d] %s/%s: %v", targetIdx, target.Provider, target.Model, err))
			if providers.IsRetryable(err) {
				continue
			}
			writeOpenAIError(w, http.StatusBadRequest, fmt.Sprintf("audio/transcriptions failed: %v", err), "invalid_request_error", "")
			return
		}
		writeJSON(w, http.StatusOK, response)
		return
	}
	writeOpenAIError(w, http.StatusInternalServerError, fmt.Sprintf("all audio/transcriptions targets failed: %s", strings.Join(allErrors, " | ")), "internal_error", "")
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

func (s *Server) handleWebSearch(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var body struct {
		Provider    string `json:"provider"`
		Query       string `json:"query"`
		MaxResults  int    `json:"max_results"`
		SearchDepth string `json:"search_depth"`
		IncludeRaw  bool   `json:"include_raw_content"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid json", "invalid_request_error", "")
		return
	}
	if strings.TrimSpace(body.Query) == "" {
		writeOpenAIError(w, http.StatusBadRequest, "query is required", "invalid_request_error", "")
		return
	}
	adapter, providerName, err := s.serviceAdapter(body.Provider, "webSearch")
	if err != nil {
		writeOpenAIError(w, http.StatusNotFound, err.Error(), "invalid_request_error", "")
		return
	}
	searcher, ok := adapter.(providers.WebSearcher)
	if !ok {
		writeOpenAIError(w, http.StatusBadRequest, providerName+" does not support web search", "invalid_request_error", "")
		return
	}
	resp, err := searcher.WebSearch(r.Context(), providers.WebSearchRequest{
		Query:       body.Query,
		MaxResults:  body.MaxResults,
		SearchDepth: body.SearchDepth,
		IncludeRaw:  body.IncludeRaw,
	})
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, err.Error(), "upstream_error", "")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleWebFetch(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var body struct {
		Provider string   `json:"provider"`
		URL      string   `json:"url"`
		Formats  []string `json:"formats"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid json", "invalid_request_error", "")
		return
	}
	if strings.TrimSpace(body.URL) == "" {
		writeOpenAIError(w, http.StatusBadRequest, "url is required", "invalid_request_error", "")
		return
	}
	adapter, providerName, err := s.serviceAdapter(body.Provider, "webFetch")
	if err != nil {
		writeOpenAIError(w, http.StatusNotFound, err.Error(), "invalid_request_error", "")
		return
	}
	fetcher, ok := adapter.(providers.WebFetcher)
	if !ok {
		writeOpenAIError(w, http.StatusBadRequest, providerName+" does not support web fetch", "invalid_request_error", "")
		return
	}
	resp, err := fetcher.WebFetch(r.Context(), providers.WebFetchRequest{URL: body.URL, Formats: body.Formats})
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, err.Error(), "upstream_error", "")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) serviceAdapter(providerName, serviceKind string) (providers.Adapter, string, error) {
	registry := s.engine.GetRegistry()
	if strings.TrimSpace(providerName) != "" {
		adapter, err := registry.Get(providerName)
		return adapter, providerName, err
	}
	cfg := s.runtimeConfig.Get()
	for _, provider := range cfg.Providers {
		if !provider.Enabled {
			continue
		}
		providerID := provider.ProviderID
		if providerID == "" {
			providerID = catalog.InferProviderID(provider.Type, provider.Name)
		}
		if def, ok := catalog.Get(providerID); ok && containsCI(def.ServiceKinds, serviceKind) {
			adapter, err := registry.Get(provider.Name)
			return adapter, provider.Name, err
		}
	}
	return nil, "", fmt.Errorf("no enabled provider supports %s", serviceKind)
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
	if err := s.runtimeConfig.Update(func(current *config.Config) error {
		*current = cfg
		return nil
	}); err != nil {
		http.Error(w, "Failed to update config", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "success"}); err != nil {
		s.logger.Error().Err(err).Msg("failed to encode config update response")
	}
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

func (s *Server) handlePricingPatch(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "pricing": body})
}

func (s *Server) handlePricingDelete(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) handleCloudAuth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": false, "enabled": false})
}

func (s *Server) handleCloudCredentialsUpdate(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) handleCloudModelResolve(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var body struct {
		Model string `json:"model"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	targets := s.engine.ResolveTargets(body.Model)
	if body.Model == "" || len(targets) == 0 {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "model not found", "model": body.Model})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"model": body.Model, "target": targets[0], "targets": targets})
}

func (s *Server) handleCloudModelsAliasGet(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"aliases": s.runtimeConfig.ListModelAliases()})
}

func (s *Server) handleCloudModelsAliasPut(w http.ResponseWriter, r *http.Request) {
	s.handleDashboardModelAliasPut(w, r)
}

func (s *Server) handleMediaTTSVoices(w http.ResponseWriter, r *http.Request) {
	providerID := strings.TrimSpace(r.URL.Query().Get("provider"))
	if providerID == "" {
		providerID = "edge-tts"
	}
	if strings.Contains(r.URL.Path, "/elevenlabs/voices") {
		providerID = "elevenlabs"
	}
	lang := strings.TrimSpace(r.URL.Query().Get("lang"))
	apiKey := strings.TrimSpace(r.URL.Query().Get("apiKey"))

	var adapter providers.Adapter
	if apiKey != "" {
		adapter = providers.NewMediaSearchAdapter(config.ProviderConfig{
			Name:       providerID,
			ProviderID: providerID,
			Type:       providerID,
			APIKey:     apiKey,
			Enabled:    true,
		}, config.ErrorConfig{}, "")
	} else if existing, _, err := s.serviceAdapter(providerID, "tts"); err == nil {
		adapter = existing
	} else if providerID == "edge-tts" || providerID == "google-tts" || providerID == "local-device" {
		adapter = providers.NewMediaSearchAdapter(config.ProviderConfig{
			Name:       providerID,
			ProviderID: providerID,
			Type:       providerID,
			Enabled:    true,
		}, config.ErrorConfig{}, "")
	} else {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "No " + providerID + " connection found"})
		return
	}

	lister, ok := adapter.(providers.TTSVoiceLister)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Provider '" + providerID + "' does not support voice listing"})
		return
	}
	resp, err := lister.ListTTSVoices(r.Context(), lang)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleInit(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"initialized": true, "status": "ok"})
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var body struct {
		Password string `json:"password"`
		APIKey   string `json:"api_key"`
		Key      string `json:"key"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	candidate := strings.TrimSpace(body.Password)
	if candidate == "" {
		candidate = strings.TrimSpace(body.APIKey)
	}
	if candidate == "" {
		candidate = strings.TrimSpace(body.Key)
	}

	for _, key := range s.runtimeConfig.ListAdminAPIKeys() {
		if candidate != "" && candidate == key {
			http.SetCookie(w, &http.Cookie{
				Name:     "auth_token",
				Value:    candidate,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
				MaxAge:   86400,
			})
			writeJSON(w, http.StatusOK, map[string]any{"success": true, "api_key": candidate})
			return
		}
	}
	writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "Invalid password"})
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, _ *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "auth_token", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) handleShutdown(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusForbidden, map[string]any{"success": false, "message": "shutdown via API is disabled in the Go router"})
}

func (s *Server) handleV1Root(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "router",
		"routes": []string{"/v1/models", "/v1/chat/completions", "/v1/messages", "/v1/responses", "/v1/embeddings"},
	})
}

func (s *Server) handleOllamaChat(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var req struct {
		Model    string                  `json:"model"`
		Messages []providers.ChatMessage `json:"messages"`
		Prompt   string                  `json:"prompt"`
		Stream   bool                    `json:"stream"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid json", "invalid_request_error", "")
		return
	}
	if req.Model == "" {
		writeOpenAIError(w, http.StatusBadRequest, "model is required", "invalid_request_error", "")
		return
	}
	messages := req.Messages
	if len(messages) == 0 && req.Prompt != "" {
		messages = []providers.ChatMessage{{Role: "user", Content: req.Prompt}}
	}

	cfg := s.runtimeConfig.Get()
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(cfg.Server.RequestTimeoutSeconds)*time.Second)
	defer cancel()
	resp, _, err := s.engine.ChatCompletion(ctx, providers.ChatRequest{Model: req.Model, Messages: messages, Stream: false})
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, err.Error(), "api_error", "")
		return
	}

	content := ""
	if len(resp.Choices) > 0 {
		switch v := resp.Choices[0].Message.Content.(type) {
		case string:
			content = v
		default:
			b, _ := json.Marshal(v)
			content = string(b)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"model":      req.Model,
		"created_at": time.Now().UTC().Format(time.RFC3339Nano),
		"message":    map[string]any{"role": "assistant", "content": content},
		"done":       true,
	})
}

func (s *Server) handleV1BetaModelPost(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "path")
	writeJSON(w, http.StatusNotFound, map[string]any{"error": map[string]any{"message": "v1beta model path not supported: " + path}})
}

func (s *Server) handleProvidersClient(w http.ResponseWriter, r *http.Request) {
	go s.backfillCodexEmails(r.Context())
	cfg := s.runtimeConfig.Get()
	writeJSON(w, http.StatusOK, map[string]any{"connections": cfg.Providers, "providers": cfg.Providers})
}

func (s *Server) handleProvidersTestBatch(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var body struct {
		Providers []config.ProviderConfig `json:"providers"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	if len(body.Providers) == 0 {
		body.Providers = s.runtimeConfig.ListProviders()
	}
	results := make([]any, 0, len(body.Providers))
	for _, provider := range body.Providers {
		res := s.providerCheck.ValidateProvider(r.Context(), provider)
		results = append(results, map[string]any{"provider": provider.Name, "valid": res.Valid, "status": res.Status, "error": res.Error, "models": res.Models})
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func (s *Server) handleProvidersKiloFreeModels(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"models": []any{}})
}

func (s *Server) handleProviderTestModels(w http.ResponseWriter, r *http.Request) {
	s.handleProviderModels(w, r)
}

func (s *Server) handleProviderNodesValidate(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var body struct {
		BaseURL string `json:"base_url"`
		URL     string `json:"url"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	target := body.BaseURL
	if target == "" {
		target = body.URL
	}
	if target == "" || !(strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://")) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"valid": false, "error": "valid base_url is required"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"valid": true, "base_url": target})
}

func (s *Server) handleProxyPoolTest(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": chi.URLParam(r, "id"), "tested": false})
}

func (s *Server) handleProxyPoolsVercelDeploy(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusAccepted, map[string]any{"started": false, "message": "vercel proxy deployment is not managed by the Go router"})
}

func (s *Server) handleDashboardModels(w http.ResponseWriter, _ *http.Request) {
	data := []map[string]any{}
	for name, alias := range s.runtimeConfig.ListModelAliases() {
		data = append(data, map[string]any{"fullModel": alias.Provider + "/" + alias.Model, "provider": alias.Provider, "model": alias.Model, "alias": name})
	}
	for name, model := range s.runtimeConfig.ListCustomModels() {
		data = append(data, map[string]any{"fullModel": name, "provider": model.Provider, "model": model.Model, "alias": name, "description": model.Description})
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": data})
}

func (s *Server) handleDashboardModelAliasPut(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var body struct {
		Model string `json:"model"`
		Alias string `json:"alias"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil || body.Model == "" || body.Alias == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "model and alias required"})
		return
	}
	providerName, modelName, ok := strings.Cut(body.Model, "/")
	if !ok {
		providerName = ""
		modelName = body.Model
	}
	if err := s.runtimeConfig.UpdateAndPersist(func(cfg *config.Config) error {
		if cfg.ModelAliases == nil {
			cfg.ModelAliases = map[string]config.ModelAlias{}
		}
		cfg.ModelAliases[body.Alias] = config.ModelAlias{Provider: providerName, Model: modelName}
		return nil
	}); err != nil {
		writeJSON(w, mapConfigErrorToHTTP(err), map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "model": body.Model, "alias": body.Alias})
}

func (s *Server) handleModelsTest(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var body struct {
		Model string `json:"model"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	if body.Model == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "model is required"})
		return
	}
	targets := s.engine.ResolveTargets(body.Model)
	writeJSON(w, http.StatusOK, map[string]any{"ok": len(targets) > 0, "targets": targets})
}

func (s *Server) handleModelsAvailability(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"models": []any{}, "unavailableCount": 0})
}

func (s *Server) handleModelsAvailabilityAction(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleSettingsPatch(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	current := s.runtimeConfig.Get().Settings
	patched := current
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&patched); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid json", "invalid_request_error", "")
		return
	}

	if err := s.runtimeConfig.UpdateWithReconfigure(func(cfg *config.Config) error {
		cfg.Settings = patched
		return nil
	}, s.reconfigureFromConfig); err != nil {
		writeOpenAIError(w, mapConfigErrorToHTTP(err), err.Error(), "invalid_request_error", "")
		return
	}

	writeJSON(w, http.StatusOK, patched)
}

func (s *Server) handleSettingsDatabase(w http.ResponseWriter, _ *http.Request) {
	cfg := s.runtimeConfig.Get()
	writeJSON(w, http.StatusOK, map[string]any{"sqlite_path": cfg.Storage.SQLitePath, "path": cfg.Storage.SQLitePath})
}

func (s *Server) handleSettingsProxyTest(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var body struct {
		URL string `json:"url"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	if body.URL != "" && !(strings.HasPrefix(body.URL, "http://") || strings.HasPrefix(body.URL, "https://") || strings.HasPrefix(body.URL, "socks5://")) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "unsupported proxy URL"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "tested": false})
}

func (s *Server) handleUsageStats(w http.ResponseWriter, r *http.Request) {
	db := s.db()
	if db == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"stats": map[string]any{
				"requests_total":      0,
				"requests_success":    0,
				"requests_error":      0,
				"prompt_tokens":       0,
				"completion_tokens":   0,
				"total_tokens":        0,
				"total_cost":          0,
				"success_rate":        0,
				"average_cost":        0,
				"average_tokens":      0,
				"average_duration_ms": 0,
			},
		})
		return
	}

	summary, err := db.UsageSummary(r.Context())
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to query usage stats")
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to query usage stats"})
		return
	}

	successRate := 0.0
	averageCost := 0.0
	averageTokens := 0.0
	if summary.RequestsTotal > 0 {
		successRate = float64(summary.RequestsSuccess) / float64(summary.RequestsTotal)
		averageCost = summary.TotalCost / float64(summary.RequestsTotal)
		averageTokens = float64(summary.TotalTokens) / float64(summary.RequestsTotal)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"stats": map[string]any{
			"requests_total":    summary.RequestsTotal,
			"requests_success":  summary.RequestsSuccess,
			"requests_error":    summary.RequestsError,
			"prompt_tokens":     summary.PromptTokens,
			"completion_tokens": summary.CompletionTokens,
			"total_tokens":      summary.TotalTokens,
			"total_cost":        summary.TotalCost,
			"success_rate":      successRate,
			"average_cost":      averageCost,
			"average_tokens":    averageTokens,
		},
	})
}

func (s *Server) handleUsageHistory(w http.ResponseWriter, r *http.Request) {
	history, err := s.usageHistoryRows(r)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to query usage history")
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to query usage history"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"history": history})
}

func (s *Server) handleUsageChart(w http.ResponseWriter, r *http.Request) {
	rows, err := s.usageHistoryRows(r)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to query usage chart")
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to query usage chart"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": rows})
}

func (s *Server) handleUsageProviders(w http.ResponseWriter, r *http.Request) {
	summaries := map[string]storage.UsageProviderSummary{}
	if db := s.db(); db != nil {
		rows, err := db.UsageProviders(r.Context())
		if err != nil {
			s.logger.Error().Err(err).Msg("failed to query usage providers")
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to query usage providers"})
			return
		}
		for _, row := range rows {
			summaries[row.Provider] = row
		}
	}

	providersList := s.runtimeConfig.ListProviders()
	items := make([]map[string]any, 0, len(providersList))
	for _, provider := range providersList {
		item := map[string]any{"provider": provider.Name, "name": provider.Name, "enabled": provider.Enabled}
		if summary, ok := summaries[provider.Name]; ok {
			item["request_count"] = summary.RequestCount
			item["prompt_tokens"] = summary.PromptTokens
			item["completion_tokens"] = summary.CompletionTokens
			item["total_tokens"] = summary.TotalTokens
			item["total_cost"] = summary.TotalCost
			if !summary.LastRequestAt.IsZero() {
				item["last_request_at"] = summary.LastRequestAt.Unix()
			}
		} else {
			item["request_count"] = 0
			item["prompt_tokens"] = 0
			item["completion_tokens"] = 0
			item["total_tokens"] = 0
			item["total_cost"] = 0
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": items})
}

func (s *Server) handleUsageRequestDetails(w http.ResponseWriter, r *http.Request) {
	requestID := r.URL.Query().Get("request_id")
	if requestID == "" {
		requestID = r.URL.Query().Get("id")
	}
	if requestID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "request_id required"})
		return
	}
	db := s.db()
	if db == nil {
		writeJSON(w, http.StatusOK, map[string]any{"request_id": requestID, "details": nil})
		return
	}
	details, err := db.GetRequestDetails(r.Context(), requestID)
	if err != nil {
		s.logger.Error().Err(err).Str("request_id", requestID).Msg("failed to query request details")
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to query request details"})
		return
	}
	if details == nil {
		writeJSON(w, http.StatusOK, map[string]any{"request_id": requestID, "details": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"request_id": requestID,
		"details": map[string]any{
			"request_id":    details.RequestID,
			"request_body":  details.RequestBody,
			"response_body": details.ResponseBody,
			"status_code":   details.StatusCode,
			"created_at":    details.CreatedAt.Unix(),
		},
	})
}

func (s *Server) handleUsageStream(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	fmt.Fprintf(w, "data: {\"type\":\"ready\"}\n\n")
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (s *Server) handleUsageConnection(w http.ResponseWriter, r *http.Request) {
	connectionID := chi.URLParam(r, "connectionId")
	if connectionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "connectionId required"})
		return
	}
	db := s.db()
	if db == nil {
		writeJSON(w, http.StatusOK, map[string]any{"connectionId": connectionID, "usage": []any{}})
		return
	}
	logs, total, err := db.QueryLogs(r.Context(), storage.LogQueryParams{Provider: connectionID, Limit: 100})
	if err != nil {
		s.logger.Error().Err(err).Str("connection_id", connectionID).Msg("failed to query connection usage")
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to query connection usage"})
		return
	}
	usageRows := make([]map[string]any, 0, len(logs))
	for _, log := range logs {
		usageRows = append(usageRows, apiRequestLog(log))
	}
	writeJSON(w, http.StatusOK, map[string]any{"connectionId": connectionID, "usage": usageRows, "total": total})
}

func (s *Server) usageHistoryRows(r *http.Request) ([]map[string]any, error) {
	db := s.db()
	if db == nil {
		return []map[string]any{}, nil
	}
	days := 30
	if raw := r.URL.Query().Get("days"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			days = parsed
		}
	}
	rows, err := db.UsageHistory(r.Context(), days)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"date":              row.Date,
			"provider":          row.Provider,
			"model":             row.Model,
			"request_count":     row.RequestCount,
			"prompt_tokens":     row.PromptTokens,
			"completion_tokens": row.CompletionTokens,
			"total_tokens":      row.TotalTokens,
			"total_cost":        row.TotalCost,
		})
	}
	return out, nil
}

func (s *Server) handleTranslatorLoad(w http.ResponseWriter, r *http.Request) {
	file := strings.TrimSpace(r.URL.Query().Get("file"))
	if !translatorFileAllowed(file) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "File parameter required"})
		return
	}
	content, err := os.ReadFile(filepath.Join("logs", "translator", file))
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "error": "File not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "content": string(content)})
}

func (s *Server) handleTranslatorSave(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req struct {
		File    string `json:"file"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "invalid json"})
		return
	}
	if !translatorFileAllowed(req.File) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "File and content required"})
		return
	}
	dir := filepath.Join("logs", "translator")
	if err := os.MkdirAll(dir, 0755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "error": err.Error()})
		return
	}
	if err := os.WriteFile(filepath.Join(dir, req.File), []byte(req.Content), 0644); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "error": err.Error()})
		return
	}
	s.appendTranslatorLog("info", "saved translator file: "+req.File)
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) handleTranslatorTranslate(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req struct {
		Step         int             `json:"step"`
		Body         json.RawMessage `json:"body"`
		Provider     string          `json:"provider"`
		Model        string          `json:"model"`
		SourceFormat string          `json:"sourceFormat"`
		TargetFormat string          `json:"targetFormat"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "invalid json"})
		return
	}
	if req.Step == 0 {
		req.Step = 2
	}
	if len(req.Body) == 0 || string(req.Body) == "null" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "Step and body required"})
		return
	}

	body, err := unwrapTranslatorBody(req.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": err.Error()})
		return
	}

	switch req.Step {
	case 1:
		provider, model := parseTranslatorModel(stringValue(body["model"]))
		sourceFormat := translator.DetectFormat("", body)
		targetFormat := s.translatorTargetFormat(provider)
		s.appendTranslatorLog("info", fmt.Sprintf("translator step 1 detected provider=%s model=%s source=%s target=%s", provider, model, sourceFormat, targetFormat))
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"result": map[string]any{
				"provider":     provider,
				"model":        model,
				"sourceFormat": sourceFormat,
				"targetFormat": targetFormat,
			},
		})
	case 2:
		provider, model := parseTranslatorModel(stringValue(body["model"]))
		sourceFormat := req.SourceFormat
		if sourceFormat == "" {
			sourceFormat = translator.DetectFormat("", body)
		}
		translated, err := s.translatorRegistry().TranslateRequestJSON(r.Context(), normalizeTranslatorFormat(sourceFormat), translator.FormatOpenAI, mustMarshalRaw(body))
		if err != nil {
			s.appendTranslatorLog("error", "translator step 2 failed: "+err.Error())
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": err.Error()})
			return
		}
		var translatedBody map[string]any
		_ = json.Unmarshal(translated, &translatedBody)
		s.appendTranslatorLog("info", "translator step 2 translated request to openai")
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"result": map[string]any{
				"provider": provider,
				"model":    model,
				"body":     translatedBody,
			},
		})
	case 3:
		provider := strings.TrimSpace(req.Provider)
		model := strings.TrimSpace(req.Model)
		if provider == "" {
			provider, _ = parseTranslatorModel(stringValue(body["model"]))
		}
		if model == "" {
			_, model = parseTranslatorModel(stringValue(body["model"]))
		}
		if provider == "" || model == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "provider and model required"})
			return
		}
		targetFormat := req.TargetFormat
		if targetFormat == "" {
			targetFormat = s.translatorTargetFormat(provider)
		}
		translated, err := s.translatorRegistry().TranslateRequestJSON(r.Context(), translator.FormatOpenAI, normalizeTranslatorFormat(targetFormat), mustMarshalRaw(body))
		if err != nil {
			s.appendTranslatorLog("error", "translator step 3 failed: "+err.Error())
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": err.Error()})
			return
		}
		var finalBody map[string]any
		_ = json.Unmarshal(translated, &finalBody)
		url, headers, err := s.translatorRequestPreview(r.Context(), provider, model)
		if err != nil {
			s.appendTranslatorLog("error", "translator step 3 preview failed: "+err.Error())
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": err.Error()})
			return
		}
		s.appendTranslatorLog("info", fmt.Sprintf("translator step 3 built target preview provider=%s model=%s", provider, model))
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"result": map[string]any{
				"url":     url,
				"headers": headers,
				"body":    finalBody,
			},
		})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "Invalid step (1-3)"})
	}
}

func (s *Server) handleTranslatorSend(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req struct {
		Provider string          `json:"provider"`
		Model    string          `json:"model"`
		Body     json.RawMessage `json:"body"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "invalid json"})
		return
	}
	if strings.TrimSpace(req.Provider) == "" || strings.TrimSpace(req.Model) == "" || len(req.Body) == 0 || string(req.Body) == "null" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "provider, model, and body required"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(s.runtimeConfig.Get().Server.RequestTimeoutSeconds)*time.Second)
	defer cancel()
	url, headers, err := s.translatorExecutionRequest(ctx, req.Provider, req.Model)
	if err != nil {
		s.appendTranslatorLog("error", err.Error())
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": err.Error()})
		return
	}
	upstreamReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(req.Body))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": err.Error()})
		return
	}
	for k, v := range headers {
		upstreamReq.Header.Set(k, v)
	}
	resp, err := (&http.Client{Timeout: time.Duration(s.runtimeConfig.Get().Server.RequestTimeoutSeconds) * time.Second}).Do(upstreamReq)
	if err != nil {
		s.appendTranslatorLog("error", "translator send upstream error: "+err.Error())
		writeJSON(w, http.StatusBadGateway, map[string]any{"success": false, "error": err.Error()})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		details, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		s.appendTranslatorLog("error", fmt.Sprintf("translator send provider error status=%d", resp.StatusCode))
		writeJSON(w, resp.StatusCode, map[string]any{"success": false, "error": fmt.Sprintf("Provider error: %d", resp.StatusCode), "details": string(details)})
		return
	}
	for k, values := range resp.Header {
		for _, v := range values {
			w.Header().Add(k, v)
		}
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "text/event-stream")
	}
	s.appendTranslatorLog("info", fmt.Sprintf("translator send forwarded provider=%s model=%s", req.Provider, req.Model))
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (s *Server) translatorRegistry() *translator.Registry {
	if s.translators == nil {
		s.translators = translator.NewRegistry()
	}
	return s.translators
}

func unwrapTranslatorBody(raw json.RawMessage) (map[string]any, error) {
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("body must be a JSON object")
	}
	if nested, ok := body["body"].(map[string]any); ok {
		return nested, nil
	}
	return body, nil
}

func parseTranslatorModel(model string) (string, string) {
	model = strings.TrimSpace(model)
	if model == "" {
		return "", ""
	}
	if idx := strings.Index(model, "/"); idx > 0 {
		provider := strings.TrimSpace(model[:idx])
		if def, ok := catalog.ResolveAlias(provider); ok {
			provider = def.ID
		}
		return provider, strings.TrimSpace(model[idx+1:])
	}
	return "", model
}

func (s *Server) translatorTargetFormat(provider string) string {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return translator.FormatOpenAI
	}
	if cfg, ok := s.runtimeConfig.GetProvider(provider); ok {
		if cfg.Format != "" {
			return normalizeTranslatorFormat(cfg.Format)
		}
		if cfg.ProviderID != "" {
			provider = cfg.ProviderID
		}
	}
	if def, ok := catalog.ResolveAlias(provider); ok && def.Format != "" {
		return normalizeTranslatorFormat(def.Format)
	}
	return translator.FormatOpenAI
}

func normalizeTranslatorFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "openai", "chat":
		return translator.FormatOpenAI
	case "anthropic", "claude", "messages":
		return translator.FormatClaude
	case "openai-responses", "responses":
		return translator.FormatOpenAIResp
	case "gemini":
		return translator.FormatGemini
	case "ollama":
		return translator.FormatOllama
	default:
		return strings.ToLower(strings.TrimSpace(format))
	}
}

func (s *Server) translatorRequestPreview(ctx context.Context, provider, model string) (string, map[string]string, error) {
	headers := map[string]string{"Content-Type": "application/json"}
	var baseURL string
	var format string
	if db := s.db(); db != nil {
		conns, err := db.ListProviderConnections(ctx, storage.ProviderConnectionFilter{Provider: provider})
		if err != nil {
			return "", nil, fmt.Errorf("list provider connections: %w", err)
		}
		for _, conn := range conns {
			if !conn.IsActive || !conn.Enabled {
				continue
			}
			baseURL = conn.BaseURL
			format = conn.Format
			for k, v := range conn.Headers {
				headers[k] = v
			}
			if conn.APIKey != "" {
				if normalizeTranslatorFormat(format) == translator.FormatClaude {
					headers["x-api-key"] = redactSecret(conn.APIKey)
				} else {
					headers["Authorization"] = "Bearer " + redactSecret(conn.APIKey)
				}
			}
			break
		}
	}
	if baseURL == "" {
		if cfg, ok := s.runtimeConfig.GetProvider(provider); ok {
			baseURL = cfg.BaseURL
			format = cfg.Format
			for k, v := range cfg.Headers {
				headers[k] = v
			}
			if cfg.APIKey != "" {
				if normalizeTranslatorFormat(format) == translator.FormatClaude {
					headers["x-api-key"] = redactSecret(cfg.APIKey)
				} else {
					headers["Authorization"] = "Bearer " + redactSecret(cfg.APIKey)
				}
			}
		}
	}
	if baseURL == "" {
		if def, ok := catalog.ResolveAlias(provider); ok {
			baseURL = def.DefaultBaseURL
			format = def.Format
			for k, v := range def.Headers {
				headers[k] = v
			}
		}
	}
	if baseURL == "" {
		return "", nil, fmt.Errorf("no active connection for provider: %s", provider)
	}
	return buildTranslatorURL(baseURL, normalizeTranslatorFormat(format), model), headers, nil
}

func (s *Server) translatorExecutionRequest(ctx context.Context, provider, model string) (string, map[string]string, error) {
	headers := map[string]string{"Content-Type": "application/json"}
	var baseURL string
	var format string
	var apiKey string
	if db := s.db(); db != nil {
		conns, err := db.ListProviderConnections(ctx, storage.ProviderConnectionFilter{Provider: provider})
		if err != nil {
			return "", nil, fmt.Errorf("list provider connections: %w", err)
		}
		for _, conn := range conns {
			if !conn.IsActive || !conn.Enabled {
				continue
			}
			baseURL = conn.BaseURL
			format = conn.Format
			apiKey = firstNonEmpty(conn.APIKey, conn.AccessToken)
			for k, v := range conn.Headers {
				headers[k] = v
			}
			break
		}
	}
	if baseURL == "" {
		if cfg, ok := s.runtimeConfig.GetProvider(provider); ok {
			baseURL = cfg.BaseURL
			format = cfg.Format
			apiKey = cfg.APIKey
			if apiKey == "" {
				for _, account := range cfg.Accounts {
					if !account.Enabled && len(cfg.Accounts) > 1 {
						continue
					}
					apiKey = firstNonEmpty(account.APIKey, account.AccessToken)
					if apiKey != "" {
						break
					}
				}
			}
			for k, v := range cfg.Headers {
				headers[k] = v
			}
		}
	}
	if baseURL == "" {
		return "", nil, fmt.Errorf("no active connection for provider: %s", provider)
	}
	if apiKey != "" {
		if normalizeTranslatorFormat(format) == translator.FormatClaude {
			headers["x-api-key"] = apiKey
			if headers["anthropic-version"] == "" {
				headers["anthropic-version"] = "2023-06-01"
			}
		} else {
			headers["Authorization"] = "Bearer " + apiKey
		}
	}
	return buildTranslatorURL(baseURL, normalizeTranslatorFormat(format), model), headers, nil
}

func buildTranslatorURL(baseURL, format, model string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	switch normalizeTranslatorFormat(format) {
	case translator.FormatClaude:
		if strings.HasSuffix(baseURL, "/messages") {
			return baseURL
		}
		return baseURL + "/messages"
	case translator.FormatOpenAIResp:
		if strings.HasSuffix(baseURL, "/responses") {
			return baseURL
		}
		return baseURL + "/responses"
	case translator.FormatGemini:
		if model != "" {
			return baseURL + "/" + model + ":generateContent"
		}
		return baseURL
	case translator.FormatOllama:
		if strings.HasSuffix(baseURL, "/api/chat") {
			return baseURL
		}
		return baseURL + "/api/chat"
	default:
		if strings.HasSuffix(baseURL, "/chat/completions") {
			return baseURL
		}
		return baseURL + "/chat/completions"
	}
}

func redactSecret(secret string) string {
	secret = strings.TrimSpace(secret)
	if len(secret) <= 8 {
		return "REDACTED"
	}
	return secret[:4] + "..." + secret[len(secret)-4:]
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func proxyPoolResponse(pool storage.ProxyPool) map[string]any {
	proxies := append([]string(nil), pool.Proxies...)
	return map[string]any{
		"id":         pool.ID,
		"name":       pool.Name,
		"proxies":    proxies,
		"proxy_urls": proxies,
		"created_at": pool.CreatedAt,
		"updated_at": pool.UpdatedAt,
	}
}

func generateAdminAPIKey() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "sk-admin-" + base64.RawURLEncoding.EncodeToString(raw), nil
}

func adminKeyResponse(id, key string, includeRaw bool) map[string]any {
	key = strings.TrimSpace(key)
	prefix := key
	suffix := key
	switch {
	case len(key) > 8:
		prefix = key[:4]
		suffix = key[len(key)-4:]
	case len(key) > 4:
		prefix = key[:4]
		suffix = key[len(key)-2:]
	}
	masked := redactSecret(key)
	item := map[string]any{
		"id":         id,
		"name":       "Key " + strconv.Itoa(mustAtoi(id)+1),
		"api_key":    masked,
		"masked_key": masked,
		"prefix":     prefix,
		"suffix":     suffix,
		"created_at": nil,
	}
	if includeRaw {
		item["api_key"] = key
		item["key"] = key
		item["masked_key"] = masked
	}
	return item
}

func mustAtoi(value string) int {
	i, _ := strconv.Atoi(value)
	return i
}

func generatePKCEPair() (string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	verifier := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

func normalizeWebProviderCookie(provider, raw string) (string, string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", "", fmt.Errorf("cookie is required")
	}
	name := webCookieName(provider)
	if name == "" {
		return "", "", fmt.Errorf("unknown cookie provider")
	}
	token := extractCookieValue(value, name)
	if token == "" && !strings.Contains(value, "=") {
		token = value
	}
	token = strings.TrimSpace(strings.Trim(token, `"`))
	if len(token) < 8 {
		return "", "", fmt.Errorf("%s cookie value is too short or missing", name)
	}
	if strings.ContainsAny(token, "\r\n;") {
		return "", "", fmt.Errorf("%s cookie value contains invalid characters", name)
	}
	return name + "=" + token + ";", token, nil
}

func webCookieName(provider string) string {
	switch provider {
	case "grok-web":
		return "sso"
	case "perplexity-web":
		return "__Secure-next-auth.session-token"
	default:
		return ""
	}
}

func extractCookieValue(cookie, name string) string {
	for _, part := range strings.Split(cookie, ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		if key == name {
			return value
		}
	}
	return ""
}

func mustMarshalRaw(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func firstPositiveInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func (s *Server) handleTranslatorConsoleLogs(w http.ResponseWriter, _ *http.Request) {
	s.translatorLogMu.RLock()
	logs := append([]map[string]any(nil), s.translatorLogs...)
	s.translatorLogMu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "logs": logs})
}

func (s *Server) handleTranslatorConsoleLogsClear(w http.ResponseWriter, _ *http.Request) {
	s.translatorLogMu.Lock()
	s.translatorLogs = nil
	s.translatorLogMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) handleTranslatorConsoleLogsStream(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	s.translatorLogMu.RLock()
	initPayload, _ := json.Marshal(map[string]any{"type": "init", "logs": s.translatorLogs})
	s.translatorLogMu.RUnlock()
	fmt.Fprintf(w, "data: %s\n\n", initPayload)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func translatorFileAllowed(file string) bool {
	switch file {
	case "1_req_client.json",
		"2_req_source.json",
		"3_req_openai.json",
		"4_req_target.json",
		"5_res_provider.txt",
		"6_res_openai.txt",
		"7_res_client.txt",
		"7_res_client.json":
		return true
	default:
		return false
	}
}

func (s *Server) appendTranslatorLog(level, message string) {
	s.translatorLogMu.Lock()
	defer s.translatorLogMu.Unlock()
	line := map[string]any{
		"level":     level,
		"message":   message,
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
	}
	s.translatorLogs = append(s.translatorLogs, line)
	if len(s.translatorLogs) > 500 {
		s.translatorLogs = append([]map[string]any(nil), s.translatorLogs[len(s.translatorLogs)-500:]...)
	}
}

func (s *Server) handleCLIToolSettingsGet(w http.ResponseWriter, r *http.Request) {
	tool := cliToolNameFromRequest(r)
	state, err := readCLIToolState(tool)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handleCLIToolSettingsPost(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	tool := cliToolNameFromRequest(r)
	result, err := writeCLIToolState(tool, body, r.Method)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleCLIToolSettingsDelete(w http.ResponseWriter, r *http.Request) {
	tool := cliToolNameFromRequest(r)
	result, err := deleteCLIToolState(tool, r.URL.Query().Get("model"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleMITMStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.ensureMITMManager().Status())
}

func (s *Server) handleMITMStart(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var body struct {
		APIKey            string `json:"apiKey"`
		SudoPassword      string `json:"sudoPassword"`
		MITMRouterBaseURL string `json:"mitmRouterBaseUrl"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	status, err := s.ensureMITMManager().Start(r.Context(), body.APIKey, body.SudoPassword, body.MITMRouterBaseURL)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if strings.Contains(err.Error(), "Missing") || strings.Contains(err.Error(), "Invalid MITM") || strings.Contains(err.Error(), "must use") {
			statusCode = http.StatusBadRequest
		}
		writeJSON(w, statusCode, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "running": status.Running, "pid": status.PID})
}

func (s *Server) handleMITMStop(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var body struct {
		SudoPassword string `json:"sudoPassword"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	status, err := s.ensureMITMManager().Stop(r.Context(), body.SudoPassword)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "running": status.Running})
}

func (s *Server) handleMITMPatch(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var body struct {
		Tool         string `json:"tool"`
		Action       string `json:"action"`
		SudoPassword string `json:"sudoPassword"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	manager := s.ensureMITMManager()
	var (
		status mitm.Status
		err    error
	)
	switch body.Action {
	case "enable":
		status, err = manager.EnableDNS(r.Context(), body.Tool, body.SudoPassword)
	case "disable":
		status, err = manager.DisableDNS(r.Context(), body.Tool, body.SudoPassword)
	case "trust-cert":
		status, err = manager.TrustCert(r.Context(), body.SudoPassword)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "action must be enable, disable, or trust-cert"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if body.Action == "trust-cert" {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "certTrusted": status.CertTrusted})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "dnsStatus": status.DNSStatus})
}

func (s *Server) handleCLIToolAliasGet(w http.ResponseWriter, r *http.Request) {
	if cliToolNameFromRequest(r) == "antigravity-mitm" {
		tool := strings.TrimSpace(r.URL.Query().Get("tool"))
		writeJSON(w, http.StatusOK, map[string]any{"aliases": s.ensureMITMManager().Aliases(tool)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"aliases": map[string]any{}})
}

func (s *Server) handleCLIToolAliasPut(w http.ResponseWriter, r *http.Request) {
	if cliToolNameFromRequest(r) == "antigravity-mitm" {
		defer r.Body.Close()
		var body struct {
			Tool     string            `json:"tool"`
			Mappings map[string]string `json:"mappings"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
			return
		}
		aliases, err := s.ensureMITMManager().SetAliases(body.Tool, body.Mappings)
		if err != nil {
			status := http.StatusBadRequest
			if strings.Contains(err.Error(), "DNS must be enabled") {
				status = http.StatusForbidden
			}
			writeJSON(w, status, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "aliases": aliases})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) ensureMITMManager() *mitm.Manager {
	if s.mitmManager == nil {
		s.mitmManager = mitm.NewManager(nil)
	}
	return s.mitmManager
}

func cliToolNameFromRequest(r *http.Request) string {
	tool := chi.URLParam(r, "tool")
	if tool != "" {
		return tool
	}
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/cli-tools/")
	if strings.HasSuffix(trimmed, "-settings") {
		return strings.TrimSuffix(trimmed, "-settings")
	}
	if trimmed == "antigravity-mitm" || strings.HasPrefix(trimmed, "antigravity-mitm/") {
		return "antigravity-mitm"
	}
	return trimmed
}

func cliHomeDir() string {
	if override := strings.TrimSpace(os.Getenv("NINEROUTER_CLI_HOME")); override != "" {
		return override
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "."
	}
	return home
}

func cliToolPath(tool string) string {
	home := cliHomeDir()
	switch tool {
	case "claude":
		return filepath.Join(home, ".claude", "settings.json")
	case "codex":
		return filepath.Join(home, ".codex", "config.toml")
	case "codex-auth":
		return filepath.Join(home, ".codex", "auth.json")
	case "copilot":
		return filepath.Join(home, ".config", "Code", "User", "chatLanguageModels.json")
	case "droid":
		return filepath.Join(home, ".factory", "settings.json")
	case "hermes":
		return filepath.Join(home, ".hermes", "config.yaml")
	case "hermes-env":
		return filepath.Join(home, ".hermes", ".env")
	case "openclaw":
		return filepath.Join(home, ".openclaw", "openclaw.json")
	case "opencode":
		return filepath.Join(home, ".config", "opencode", "opencode.json")
	default:
		return filepath.Join(home, ".config", "9router-go", "cli-tools", tool+".json")
	}
}

func readCLIToolState(tool string) (map[string]any, error) {
	path := cliToolPath(tool)
	switch tool {
	case "claude":
		settings, exists, err := readJSONFileMap(path)
		if err != nil {
			return nil, err
		}
		_, has9Router := nestedStringMap(settings, "env")["ANTHROPIC_BASE_URL"]
		return map[string]any{"tool": tool, "installed": exists, "settings": nullableMap(settings, exists), "has9Router": has9Router, "settingsPath": path, "configured": has9Router}, nil
	case "codex":
		content, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return map[string]any{"tool": tool, "installed": false, "config": nil, "has9Router": false, "configPath": path, "configured": false}, nil
			}
			return nil, err
		}
		config := string(content)
		has9Router := strings.Contains(config, `model_provider = "9router"`) || strings.Contains(config, "[model_providers.9router]")
		return map[string]any{"tool": tool, "installed": true, "config": config, "has9Router": has9Router, "configPath": path, "configured": has9Router}, nil
	case "opencode":
		config, exists, err := readJSONFileMap(path)
		if err != nil {
			return nil, err
		}
		providers := nestedAnyMap(config, "provider")
		_, has9Router := providers["9router"]
		out := map[string]any{"tool": tool, "installed": exists, "config": nullableMap(config, exists), "has9Router": has9Router, "configPath": path, "configured": has9Router}
		if providerConfig, ok := providers["9router"].(map[string]any); ok {
			models := []string{}
			if modelMap, ok := providerConfig["models"].(map[string]any); ok {
				for model := range modelMap {
					models = append(models, model)
				}
				sort.Strings(models)
			}
			active := ""
			if model, ok := config["model"].(string); ok && strings.HasPrefix(model, "9router/") {
				active = strings.TrimPrefix(model, "9router/")
			}
			options := nestedAnyMap(providerConfig, "options")
			out["opencode"] = map[string]any{"models": models, "activeModel": active, "baseURL": options["baseURL"]}
		}
		return out, nil
	case "copilot":
		config, exists, err := readJSONArrayFile(path)
		if err != nil {
			return nil, err
		}
		entry := copilot9RouterEntry(config)
		return map[string]any{"tool": tool, "installed": true, "config": nullableSlice(config, exists), "has9Router": entry != nil, "configPath": path, "configured": entry != nil, "currentModel": firstCopilotModelID(entry), "currentUrl": firstCopilotModelURL(entry)}, nil
	case "droid":
		settings, exists, err := readJSONFileMap(path)
		if err != nil {
			return nil, err
		}
		has9Router := false
		if models, ok := settings["customModels"].([]any); ok {
			for _, raw := range models {
				if model, ok := raw.(map[string]any); ok && strings.HasPrefix(stringFieldAny(model, "id"), "custom:9Router") {
					has9Router = true
					break
				}
			}
		}
		return map[string]any{"tool": tool, "installed": exists, "settings": nullableMap(settings, exists), "has9Router": has9Router, "settingsPath": path, "configured": has9Router}, nil
	case "hermes":
		yaml, exists, err := readTextFile(path)
		if err != nil {
			return nil, err
		}
		model := parseHermesModelBlock(yaml)
		has9Router := strings.EqualFold(stringFieldAny(model, "provider"), "custom") && strings.Contains(stringFieldAny(model, "base_url"), "/v1")
		return map[string]any{"tool": tool, "installed": exists, "settings": map[string]any{"model": nullableMap(model, model != nil)}, "has9Router": has9Router, "configPath": path, "configured": has9Router}, nil
	case "openclaw":
		settings, exists, err := readJSONFileMap(path)
		if err != nil {
			return nil, err
		}
		providers := nestedAnyMap(nestedAnyMap(settings, "models"), "providers")
		_, has9Router := providers["9router"]
		return map[string]any{"tool": tool, "installed": exists, "settings": nullableMap(settings, exists), "has9Router": has9Router, "settingsPath": path, "configured": has9Router}, nil
	default:
		settings, exists, err := readJSONFileMap(path)
		if err != nil {
			return nil, err
		}
		return map[string]any{"tool": tool, "installed": exists, "settings": nullableMap(settings, exists), "configured": exists, "settingsPath": path}, nil
	}
}

func writeCLIToolState(tool string, body map[string]any, method string) (map[string]any, error) {
	switch tool {
	case "claude":
		env, ok := body["env"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Invalid env object")
		}
		path := cliToolPath(tool)
		settings, _, err := readJSONFileMap(path)
		if err != nil {
			return nil, err
		}
		currentEnv := nestedAnyMap(settings, "env")
		for k, v := range env {
			if k == "ANTHROPIC_BASE_URL" {
				if raw, ok := v.(string); ok && raw != "" && !strings.HasSuffix(raw, "/v1") {
					v = strings.TrimRight(raw, "/") + "/v1"
				}
			}
			currentEnv[k] = v
		}
		settings["hasCompletedOnboarding"] = true
		settings["env"] = currentEnv
		if err := writeJSONFile(path, settings); err != nil {
			return nil, err
		}
		return map[string]any{"success": true, "message": "Settings updated successfully", "settingsPath": path}, nil
	case "codex":
		baseURL := stringValue(body["baseUrl"])
		apiKey := stringValue(body["apiKey"])
		model := stringValue(body["model"])
		if baseURL == "" || apiKey == "" || model == "" {
			return nil, fmt.Errorf("baseUrl, apiKey and model are required")
		}
		if !strings.HasSuffix(baseURL, "/v1") {
			baseURL = strings.TrimRight(baseURL, "/") + "/v1"
		}
		subagentModel := stringValue(body["subagentModel"])
		if subagentModel == "" {
			subagentModel = model
		}
		configPath := cliToolPath("codex")
		config := fmt.Sprintf("model = %q\nmodel_provider = \"9router\"\n\n[model_providers.9router]\nname = \"9Router\"\nbase_url = %q\nwire_api = \"responses\"\n\n[agents.subagent]\nmodel = %q\n", model, baseURL, subagentModel)
		if err := writeTextFile(configPath, config); err != nil {
			return nil, err
		}
		auth := map[string]any{"OPENAI_API_KEY": apiKey, "auth_mode": "apikey"}
		if err := writeJSONFile(cliToolPath("codex-auth"), auth); err != nil {
			return nil, err
		}
		return map[string]any{"success": true, "message": "Codex settings applied successfully!", "configPath": configPath}, nil
	case "opencode":
		if method == http.MethodPatch {
			return patchOpenCodeState(body)
		}
		return writeOpenCodeState(body)
	case "copilot":
		return writeCopilotState(body)
	case "droid":
		return writeDroidState(body)
	case "hermes":
		return writeHermesState(body)
	case "openclaw":
		return writeOpenClawState(body)
	default:
		path := cliToolPath(tool)
		if err := writeJSONFile(path, body); err != nil {
			return nil, err
		}
		return map[string]any{"success": true, "settings": body, "settingsPath": path}, nil
	}
}

func patchOpenCodeState(body map[string]any) (map[string]any, error) {
	path := cliToolPath("opencode")
	config, _, err := readJSONFileMap(path)
	if err != nil {
		return nil, err
	}
	if clear, _ := body["clearActiveModel"].(bool); clear {
		if model, _ := config["model"].(string); strings.HasPrefix(model, "9router/") {
			config["model"] = ""
		}
	}
	if err := writeJSONFile(path, config); err != nil {
		return nil, err
	}
	return map[string]any{"success": true, "message": "Settings updated"}, nil
}

func writeOpenCodeState(body map[string]any) (map[string]any, error) {
	baseURL := stringValue(body["baseUrl"])
	if baseURL == "" {
		return nil, fmt.Errorf("baseUrl and at least one model are required")
	}
	models := stringSliceValue(body["models"])
	if model := stringValue(body["model"]); model != "" && len(models) == 0 {
		models = []string{model}
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("baseUrl and at least one model are required")
	}
	if !strings.HasSuffix(baseURL, "/v1") {
		baseURL = strings.TrimRight(baseURL, "/") + "/v1"
	}
	config, _, err := readJSONFileMap(cliToolPath("opencode"))
	if err != nil {
		return nil, err
	}
	providers := nestedAnyMap(config, "provider")
	provider := nestedAnyMap(providers, "9router")
	options := nestedAnyMap(provider, "options")
	options["baseURL"] = baseURL
	apiKey := stringValue(body["apiKey"])
	if apiKey == "" {
		apiKey = "sk_9router"
	}
	options["apiKey"] = apiKey
	provider["options"] = options
	modelMap := nestedAnyMap(provider, "models")
	for _, model := range models {
		modelMap[model] = map[string]any{"name": model}
	}
	provider["models"] = modelMap
	provider["npm"] = "@ai-sdk/openai-compatible"
	providers["9router"] = provider
	config["provider"] = providers
	active := stringValue(body["activeModel"])
	if active == "" {
		active = models[0]
	}
	config["model"] = "9router/" + active
	agent := nestedAnyMap(config, "agent")
	subagentModel := stringValue(body["subagentModel"])
	if subagentModel == "" {
		subagentModel = models[0]
	}
	agent["explorer"] = map[string]any{"description": "Fast explorer subagent for codebase exploration", "mode": "subagent", "model": "9router/" + subagentModel}
	config["agent"] = agent
	path := cliToolPath("opencode")
	if err := writeJSONFile(path, config); err != nil {
		return nil, err
	}
	return map[string]any{"success": true, "message": "OpenCode settings applied successfully!", "configPath": path}, nil
}

func writeCopilotState(body map[string]any) (map[string]any, error) {
	baseURL := strings.TrimRight(stringValue(body["baseUrl"]), "/")
	models := stringSliceValue(body["models"])
	if model := stringValue(body["model"]); model != "" && len(models) == 0 {
		models = []string{model}
	}
	if baseURL == "" || len(models) == 0 {
		return nil, fmt.Errorf("baseUrl and models are required")
	}
	config, _, err := readJSONArrayFile(cliToolPath("copilot"))
	if err != nil {
		return nil, err
	}
	endpointURL := baseURL + "/chat/completions#models.ai.azure.com"
	apiKey := stringValue(body["apiKey"])
	if apiKey == "" {
		apiKey = "sk_9router"
	}
	newEntry := map[string]any{
		"name":   "9Router",
		"vendor": "azure",
		"apiKey": apiKey,
		"models": copilotModels(models, endpointURL),
	}
	replaced := false
	for i, raw := range config {
		if entry, ok := raw.(map[string]any); ok && stringFieldAny(entry, "name") == "9Router" {
			config[i] = newEntry
			replaced = true
			break
		}
	}
	if !replaced {
		config = append(config, newEntry)
	}
	path := cliToolPath("copilot")
	if err := writeJSONFile(path, config); err != nil {
		return nil, err
	}
	return map[string]any{"success": true, "message": "Copilot settings applied! Reload VS Code to take effect.", "configPath": path}, nil
}

func copilotModels(models []string, endpointURL string) []map[string]any {
	out := make([]map[string]any, 0, len(models))
	for _, model := range models {
		out = append(out, map[string]any{
			"id":              model,
			"name":            model,
			"url":             endpointURL,
			"toolCalling":     true,
			"vision":          false,
			"maxInputTokens":  128000,
			"maxOutputTokens": 16000,
		})
	}
	return out
}

func writeDroidState(body map[string]any) (map[string]any, error) {
	baseURL := normalizeV1URL(stringValue(body["baseUrl"]))
	apiKey := stringValue(body["apiKey"])
	if apiKey == "" {
		apiKey = "your_api_key"
	}
	models := stringSliceValue(body["models"])
	if model := stringValue(body["model"]); model != "" && len(models) == 0 {
		models = []string{model}
	}
	if baseURL == "" || len(models) == 0 {
		return nil, fmt.Errorf("baseUrl and at least one model are required")
	}
	settings, _, err := readJSONFileMap(cliToolPath("droid"))
	if err != nil {
		return nil, err
	}
	existing := []any{}
	if raw, ok := settings["customModels"].([]any); ok {
		for _, item := range raw {
			if model, ok := item.(map[string]any); ok && strings.HasPrefix(stringFieldAny(model, "id"), "custom:9Router") {
				continue
			}
			existing = append(existing, item)
		}
	}
	defaultIndex := 0
	if active := stringValue(body["activeModel"]); active != "" {
		for i, model := range models {
			if model == active {
				defaultIndex = i
				break
			}
		}
	}
	added := make([]any, 0, len(models))
	for i, model := range models {
		added = append(added, map[string]any{
			"model":           model,
			"id":              fmt.Sprintf("custom:9Router-%d", i),
			"index":           i,
			"baseUrl":         baseURL,
			"apiKey":          apiKey,
			"displayName":     model,
			"maxOutputTokens": 131072,
			"noImageSupport":  false,
			"provider":        "openai",
		})
	}
	if defaultIndex > 0 && defaultIndex < len(added) {
		selected := added[defaultIndex]
		added = append([]any{selected}, append(added[:defaultIndex], added[defaultIndex+1:]...)...)
		for i, raw := range added {
			if model, ok := raw.(map[string]any); ok {
				model["index"] = i
			}
		}
	}
	settings["customModels"] = append(existing, added...)
	path := cliToolPath("droid")
	if err := writeJSONFile(path, settings); err != nil {
		return nil, err
	}
	return map[string]any{"success": true, "message": "Factory Droid settings applied successfully!", "settingsPath": path}, nil
}

func writeHermesState(body map[string]any) (map[string]any, error) {
	baseURL := normalizeV1URL(stringValue(body["baseUrl"]))
	model := stringValue(body["model"])
	if baseURL == "" || model == "" {
		return nil, fmt.Errorf("baseUrl and model are required")
	}
	path := cliToolPath("hermes")
	yaml, _, err := readTextFile(path)
	if err != nil {
		return nil, err
	}
	block := fmt.Sprintf("model:\n  default: %q\n  provider: \"custom\"\n  base_url: %q\n", model, baseURL)
	if strings.HasPrefix(yaml, "model:\n") {
		yaml = removeHermesModelBlock(yaml)
	}
	if yaml != "" && !strings.HasPrefix(yaml, "\n") {
		yaml = block + "\n" + yaml
	} else {
		yaml = block + yaml
	}
	if err := writeTextFile(path, yaml); err != nil {
		return nil, err
	}
	if apiKey := stringValue(body["apiKey"]); apiKey != "" {
		envPath := cliToolPath("hermes-env")
		env, _, err := readTextFile(envPath)
		if err != nil {
			return nil, err
		}
		env = upsertEnvLine(env, "OPENAI_API_KEY", apiKey)
		if err := writeTextFile(envPath, env); err != nil {
			return nil, err
		}
	}
	return map[string]any{"success": true, "message": "Hermes settings applied successfully!", "configPath": path}, nil
}

func writeOpenClawState(body map[string]any) (map[string]any, error) {
	baseURL := normalizeV1URL(stringValue(body["baseUrl"]))
	model := stringValue(body["model"])
	if baseURL == "" || model == "" {
		return nil, fmt.Errorf("baseUrl and model are required")
	}
	apiKey := stringValue(body["apiKey"])
	if apiKey == "" {
		apiKey = "your_api_key"
	}
	settings, _, err := readJSONFileMap(cliToolPath("openclaw"))
	if err != nil {
		return nil, err
	}
	agents := nestedAnyMap(settings, "agents")
	defaults := nestedAnyMap(agents, "defaults")
	defaultModel := nestedAnyMap(defaults, "model")
	defaultModels := nestedAnyMap(defaults, "models")
	for key := range defaultModels {
		if strings.HasPrefix(key, "9router/") {
			delete(defaultModels, key)
		}
	}
	fullModelID := "9router/" + model
	defaultModel["primary"] = fullModelID
	modelSet := map[string]struct{}{model: {}}
	agentModels := mapStringAny(body["agentModels"])
	for _, raw := range agentModels {
		if agentModel, ok := raw.(string); ok && strings.TrimSpace(agentModel) != "" {
			modelSet[agentModel] = struct{}{}
		}
	}
	modelNames := make([]string, 0, len(modelSet))
	for name := range modelSet {
		modelNames = append(modelNames, name)
		defaultModels["9router/"+name] = map[string]any{}
	}
	sort.Strings(modelNames)
	defaults["model"] = defaultModel
	defaults["models"] = defaultModels
	agents["defaults"] = defaults
	modelRoot := nestedAnyMap(settings, "models")
	providers := nestedAnyMap(modelRoot, "providers")
	providers["9router"] = map[string]any{
		"baseUrl": baseURL,
		"apiKey":  apiKey,
		"api":     "openai-completions",
		"models":  openClawModels(modelNames),
	}
	modelRoot["providers"] = providers
	settings["models"] = modelRoot
	settings["agents"] = agents
	path := cliToolPath("openclaw")
	if err := writeJSONFile(path, settings); err != nil {
		return nil, err
	}
	return map[string]any{"success": true, "message": "Open Claw settings applied successfully!", "settingsPath": path}, nil
}

func openClawModels(models []string) []map[string]any {
	out := make([]map[string]any, 0, len(models))
	for _, model := range models {
		out = append(out, map[string]any{"id": model, "name": model})
	}
	return out
}

func deleteCLIToolState(tool, model string) (map[string]any, error) {
	switch tool {
	case "claude":
		path := cliToolPath(tool)
		settings, exists, err := readJSONFileMap(path)
		if err != nil || !exists {
			return map[string]any{"success": true, "message": "No settings file to reset"}, err
		}
		env := nestedAnyMap(settings, "env")
		for _, key := range []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_DEFAULT_OPUS_MODEL", "ANTHROPIC_DEFAULT_SONNET_MODEL", "ANTHROPIC_DEFAULT_HAIKU_MODEL", "API_TIMEOUT_MS"} {
			delete(env, key)
		}
		if len(env) == 0 {
			delete(settings, "env")
		} else {
			settings["env"] = env
		}
		return map[string]any{"success": true, "message": "Settings reset successfully"}, writeJSONFile(path, settings)
	case "codex":
		_ = os.Remove(cliToolPath("codex"))
		auth, exists, err := readJSONFileMap(cliToolPath("codex-auth"))
		if err != nil {
			return nil, err
		}
		if exists {
			delete(auth, "OPENAI_API_KEY")
			delete(auth, "auth_mode")
			if err := writeJSONFile(cliToolPath("codex-auth"), auth); err != nil {
				return nil, err
			}
		}
		return map[string]any{"success": true, "message": "Codex settings reset successfully"}, nil
	case "opencode":
		return deleteOpenCodeState(model)
	case "copilot":
		return deleteCopilotState()
	case "droid":
		return deleteDroidState()
	case "hermes":
		return deleteHermesState()
	case "openclaw":
		return deleteOpenClawState()
	default:
		err := os.Remove(cliToolPath(tool))
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		return map[string]any{"success": true}, nil
	}
}

func deleteOpenCodeState(model string) (map[string]any, error) {
	path := cliToolPath("opencode")
	config, exists, err := readJSONFileMap(path)
	if err != nil || !exists {
		return map[string]any{"success": true, "message": "No config file to reset"}, err
	}
	providers := nestedAnyMap(config, "provider")
	provider := nestedAnyMap(providers, "9router")
	models := nestedAnyMap(provider, "models")
	if model != "" && len(models) > 0 {
		delete(models, model)
		if len(models) == 0 {
			delete(providers, "9router")
		} else {
			provider["models"] = models
			providers["9router"] = provider
		}
	} else {
		delete(providers, "9router")
	}
	config["provider"] = providers
	if active, _ := config["model"].(string); strings.HasPrefix(active, "9router/") && (model == "" || active == "9router/"+model) {
		config["model"] = ""
	}
	if err := writeJSONFile(path, config); err != nil {
		return nil, err
	}
	return map[string]any{"success": true, "message": "9Router settings removed from OpenCode"}, nil
}

func deleteCopilotState() (map[string]any, error) {
	path := cliToolPath("copilot")
	config, exists, err := readJSONArrayFile(path)
	if err != nil || !exists {
		return map[string]any{"success": true, "message": "No config file to reset"}, err
	}
	filtered := make([]any, 0, len(config))
	for _, raw := range config {
		if entry, ok := raw.(map[string]any); ok && stringFieldAny(entry, "name") == "9Router" {
			continue
		}
		filtered = append(filtered, raw)
	}
	if err := writeJSONFile(path, filtered); err != nil {
		return nil, err
	}
	return map[string]any{"success": true, "message": "9Router removed from Copilot config"}, nil
}

func deleteDroidState() (map[string]any, error) {
	path := cliToolPath("droid")
	settings, exists, err := readJSONFileMap(path)
	if err != nil || !exists {
		return map[string]any{"success": true, "message": "No settings file to reset"}, err
	}
	if models, ok := settings["customModels"].([]any); ok {
		filtered := make([]any, 0, len(models))
		for _, raw := range models {
			if model, ok := raw.(map[string]any); ok && strings.HasPrefix(stringFieldAny(model, "id"), "custom:9Router") {
				continue
			}
			filtered = append(filtered, raw)
		}
		if len(filtered) == 0 {
			delete(settings, "customModels")
		} else {
			settings["customModels"] = filtered
		}
	}
	if err := writeJSONFile(path, settings); err != nil {
		return nil, err
	}
	return map[string]any{"success": true, "message": "9Router settings removed successfully"}, nil
}

func deleteHermesState() (map[string]any, error) {
	path := cliToolPath("hermes")
	yaml, exists, err := readTextFile(path)
	if err != nil || !exists {
		return map[string]any{"success": true, "message": "No config file to reset"}, err
	}
	if err := writeTextFile(path, removeHermesModelBlock(yaml)); err != nil {
		return nil, err
	}
	return map[string]any{"success": true, "message": "9router model block removed"}, nil
}

func deleteOpenClawState() (map[string]any, error) {
	path := cliToolPath("openclaw")
	settings, exists, err := readJSONFileMap(path)
	if err != nil || !exists {
		return map[string]any{"success": true, "message": "No settings file to reset"}, err
	}
	modelRoot := nestedAnyMap(settings, "models")
	providers := nestedAnyMap(modelRoot, "providers")
	delete(providers, "9router")
	if len(providers) == 0 {
		delete(modelRoot, "providers")
	} else {
		modelRoot["providers"] = providers
	}
	agents := nestedAnyMap(settings, "agents")
	defaults := nestedAnyMap(agents, "defaults")
	defaultModels := nestedAnyMap(defaults, "models")
	for key := range defaultModels {
		if strings.HasPrefix(key, "9router/") {
			delete(defaultModels, key)
		}
	}
	defaultModel := nestedAnyMap(defaults, "model")
	if strings.HasPrefix(stringFieldAny(defaultModel, "primary"), "9router/") {
		delete(defaultModel, "primary")
	}
	if err := writeJSONFile(path, settings); err != nil {
		return nil, err
	}
	return map[string]any{"success": true, "message": "9Router settings removed successfully"}, nil
}

func readJSONArrayFile(path string) ([]any, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []any{}, false, nil
		}
		return nil, false, err
	}
	var out []any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, true, err
	}
	return out, true, nil
}

func readTextFile(path string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	return string(data), true, nil
}

func readJSONFileMap(path string) (map[string]any, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, false, nil
		}
		return nil, false, err
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, true, err
	}
	return out, true, nil
}

func writeJSONFile(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeTextFile(path, string(data)+"\n")
}

func writeTextFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0600)
}

func nestedAnyMap(parent map[string]any, key string) map[string]any {
	if raw, ok := parent[key].(map[string]any); ok {
		return raw
	}
	out := map[string]any{}
	parent[key] = out
	return out
}

func nestedStringMap(parent map[string]any, key string) map[string]any {
	return nestedAnyMap(parent, key)
}

func nullableMap(m map[string]any, exists bool) any {
	if !exists {
		return nil
	}
	return m
}

func nullableSlice(items []any, exists bool) any {
	if !exists {
		return nil
	}
	return items
}

func stringFieldAny(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	return stringValue(m[key])
}

func mapStringAny(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func normalizeV1URL(raw string) string {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" || strings.HasSuffix(raw, "/v1") {
		return raw
	}
	return raw + "/v1"
}

func copilot9RouterEntry(config []any) map[string]any {
	for _, raw := range config {
		if entry, ok := raw.(map[string]any); ok && stringFieldAny(entry, "name") == "9Router" {
			return entry
		}
	}
	return nil
}

func firstCopilotModelID(entry map[string]any) any {
	if entry == nil {
		return nil
	}
	models, _ := entry["models"].([]any)
	if len(models) == 0 {
		return nil
	}
	model, _ := models[0].(map[string]any)
	if id := stringFieldAny(model, "id"); id != "" {
		return id
	}
	return nil
}

func firstCopilotModelURL(entry map[string]any) any {
	if entry == nil {
		return nil
	}
	models, _ := entry["models"].([]any)
	if len(models) == 0 {
		return nil
	}
	model, _ := models[0].(map[string]any)
	if url := stringFieldAny(model, "url"); url != "" {
		return url
	}
	return nil
}

func parseHermesModelBlock(yaml string) map[string]any {
	lines := strings.Split(yaml, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != "model:" {
			continue
		}
		out := map[string]any{}
		for _, bodyLine := range lines[i+1:] {
			if strings.TrimSpace(bodyLine) == "" {
				continue
			}
			if !strings.HasPrefix(bodyLine, " ") && !strings.HasPrefix(bodyLine, "\t") {
				break
			}
			parts := strings.SplitN(strings.TrimSpace(bodyLine), ":", 2)
			if len(parts) != 2 {
				continue
			}
			out[parts[0]] = strings.Trim(strings.TrimSpace(parts[1]), `"'`)
		}
		return out
	}
	return nil
}

func removeHermesModelBlock(yaml string) string {
	lines := strings.Split(yaml, "\n")
	out := make([]string, 0, len(lines))
	skipping := false
	for _, line := range lines {
		if !skipping && strings.TrimSpace(line) == "model:" {
			skipping = true
			continue
		}
		if skipping {
			if strings.TrimSpace(line) == "" || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
				continue
			}
			skipping = false
		}
		out = append(out, line)
	}
	return strings.TrimLeft(strings.Join(out, "\n"), "\n")
}

func upsertEnvLine(envText, key, value string) string {
	line := key + "=" + value
	lines := strings.Split(envText, "\n")
	for i, existing := range lines {
		if strings.HasPrefix(existing, key+"=") {
			lines[i] = line
			return strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
		}
	}
	if strings.TrimSpace(envText) == "" {
		return line + "\n"
	}
	return strings.TrimRight(envText, "\n") + "\n" + line + "\n"
}

func stringSliceValue(v any) []string {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}

func (s *Server) handleTailscaleCheck(w http.ResponseWriter, r *http.Request) {
	if s.tunnelManager != nil {
		writeJSON(w, http.StatusOK, s.tunnelManager.CheckTailscale(r.Context()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"installed": false, "loggedIn": false, "running": false})
}

func (s *Server) handleTailscaleLogin(w http.ResponseWriter, r *http.Request) {
	if s.tunnelManager == nil {
		writeJSON(w, http.StatusAccepted, map[string]any{"started": false})
		return
	}
	result, err := s.tunnelManager.LoginTailscale(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"started": false, "error": err.Error(), "stderr": result.Stderr})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"started": true, "stdout": result.Stdout, "stderr": result.Stderr})
}

func (s *Server) handleTailscaleInstall(w http.ResponseWriter, r *http.Request) {
	if s.tunnelManager == nil {
		writeJSON(w, http.StatusAccepted, map[string]any{"started": false})
		return
	}
	result, err := s.tunnelManager.InstallTailscale(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"started": false, "error": err.Error(), "stderr": result.Stderr})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"started": true, "stdout": result.Stdout, "stderr": result.Stderr})
}

func (s *Server) handleTailscaleStartDaemon(w http.ResponseWriter, r *http.Request) {
	if s.tunnelManager == nil {
		writeJSON(w, http.StatusAccepted, map[string]any{"started": false})
		return
	}
	result, err := s.tunnelManager.StartDaemon(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"started": false, "error": err.Error(), "stderr": result.Stderr})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"started": true, "stdout": result.Stdout, "stderr": result.Stderr})
}

// handleSettingsRequireLogin mirrors the reference dashboard bootstrap endpoint.
func (s *Server) handleSettingsRequireLogin(w http.ResponseWriter, _ *http.Request) {
	cfg := s.runtimeConfig.Get()
	requireLogin := cfg.Server.APIKey != "" || len(cfg.Server.AdminAPIKeys) > 0
	tunnelURL := ""
	if cfg.Tunnel.Hostname != "" {
		tunnelURL = "https://" + strings.TrimPrefix(strings.TrimPrefix(cfg.Tunnel.Hostname, "https://"), "http://")
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"requireLogin":            requireLogin,
		"tunnelDashboardAccess":   true,
		"tunnelUrl":               tunnelURL,
		"tailscaleUrl":            "",
		"auth_required":           requireLogin,
		"tunnel_dashboard_access": true,
		"tunnel_url":              tunnelURL,
		"tailscale_url":           "",
	})
}

// handleLocaleSet persists the dashboard locale in settings.
func (s *Server) handleLocaleSet(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var body struct {
		Locale string `json:"locale"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}

	locale := strings.TrimSpace(body.Locale)
	switch i18n.Locale(locale) {
	case i18n.LocaleEnglish, i18n.LocaleIndonesian, i18n.LocaleChinese, i18n.LocaleJapanese:
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid locale"})
		return
	}

	if err := s.runtimeConfig.UpdateAndPersist(func(cfg *config.Config) error {
		cfg.Settings.Locale = locale
		return nil
	}); err != nil {
		writeJSON(w, mapConfigErrorToHTTP(err), map[string]any{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"success": true, "locale": locale})
}

// handleTags returns an Ollama-compatible model tag list derived from /v1/models.
func (s *Server) handleTags(w http.ResponseWriter, _ *http.Request) {
	models := make([]map[string]any, 0)

	addModel := func(name string, kind string) {
		if strings.TrimSpace(name) == "" {
			return
		}
		models = append(models, map[string]any{
			"name":        name,
			"model":       name,
			"modified_at": time.Unix(0, 0).UTC().Format(time.RFC3339),
			"size":        0,
			"digest":      "",
			"details": map[string]any{
				"family":             kind,
				"format":             "router",
				"parameter_size":     "",
				"quantization_level": "",
			},
		})
	}

	for name := range s.runtimeConfig.ListRoutes() {
		addModel(name, "route")
	}
	for name := range s.runtimeConfig.ListModelAliases() {
		addModel(name, "alias")
	}
	for name := range s.runtimeConfig.ListCustomModels() {
		addModel(name, "custom")
	}
	for _, provider := range s.runtimeConfig.ListProviders() {
		if provider.Enabled {
			addModel(provider.Name+"/*", "provider")
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

// handleTunnelStatus exposes current Cloudflare/Tailscale tunnel state for the dashboard.
func (s *Server) handleTunnelStatus(w http.ResponseWriter, _ *http.Request) {
	cfg := s.runtimeConfig.Get()
	status := map[string]any{
		"enabled":  cfg.Tunnel.Enabled,
		"provider": cfg.Tunnel.Provider,
		"hostname": cfg.Tunnel.Hostname,
		"running":  false,
	}
	if s.tunnelManager != nil {
		tunnelStatus := s.tunnelManager.Status()
		status["enabled"] = tunnelStatus.Enabled
		status["provider"] = tunnelStatus.Provider
		status["hostname"] = tunnelStatus.Hostname
		status["running"] = tunnelStatus.Running
		status["url"] = tunnelStatus.URL
		status["lastError"] = tunnelStatus.LastError
	}
	tailscale := map[string]any{"enabled": cfg.Tunnel.Enabled && cfg.Tunnel.Provider == "tailscale", "running": status["running"]}
	download := map[string]any{"installed": false}
	if s.tunnelManager != nil {
		tunnelStatus := s.tunnelManager.Status()
		tailscale = map[string]any{
			"enabled":   cfg.Tunnel.Enabled && cfg.Tunnel.Provider == "tailscale",
			"installed": tunnelStatus.Tailscale.Installed,
			"loggedIn":  tunnelStatus.Tailscale.LoggedIn,
			"running":   tunnelStatus.Tailscale.Running || tunnelStatus.Running,
			"url":       tunnelStatus.Tailscale.URL,
		}
		download = map[string]any{"installed": tunnelStatus.Download.Installed, "path": tunnelStatus.Download.Path}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tunnel":    status,
		"tailscale": tailscale,
		"download":  download,
	})
}

func (s *Server) handleTunnelEnable(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var body struct {
		Provider string `json:"provider"`
		Hostname string `json:"hostname"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)

	if err := s.runtimeConfig.UpdateAndPersist(func(cfg *config.Config) error {
		cfg.Tunnel.Enabled = true
		if body.Provider != "" {
			cfg.Tunnel.Provider = body.Provider
		}
		if cfg.Tunnel.Provider == "" {
			cfg.Tunnel.Provider = "cloudflare"
		}
		if body.Hostname != "" {
			cfg.Tunnel.Hostname = body.Hostname
		}
		return nil
	}); err != nil {
		writeJSON(w, mapConfigErrorToHTTP(err), map[string]any{"error": err.Error()})
		return
	}
	if s.tunnelManager != nil {
		cfg := s.runtimeConfig.Get()
		localAddr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
		if err := s.tunnelManager.Enable(r.Context(), cfg.Tunnel.Provider, localAddr); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "tunnel enabled",
		"status":  tunnelStatusMap(s.tunnelManager),
	})
}

func (s *Server) handleTunnelDisable(w http.ResponseWriter, r *http.Request) {
	if s.tunnelManager != nil {
		_ = s.tunnelManager.Disable(r.Context())
	}
	if err := s.runtimeConfig.UpdateAndPersist(func(cfg *config.Config) error {
		cfg.Tunnel.Enabled = false
		return nil
	}); err != nil {
		writeJSON(w, mapConfigErrorToHTTP(err), map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "status": tunnelStatusMap(s.tunnelManager)})
}

func tunnelStatusMap(manager *tunnel.Manager) any {
	if manager == nil {
		return nil
	}
	return manager.Status()
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"currentVersion": "dev",
		"latestVersion":  nil,
		"hasUpdate":      false,
	})
}

func (s *Server) handleVersionUpdate(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusAccepted, map[string]any{
		"started": false,
		"message": "self-update is available from the CLI with `router update`",
	})
}

// handleOAuthTokensList returns a list of stored OAuth token records (without secrets).
func (s *Server) handleOAuthTokensList(w http.ResponseWriter, r *http.Request) {
	if s.oauthStore == nil {
		writeJSON(w, http.StatusOK, map[string]any{"tokens": []any{}})
		return
	}

	tokens, err := s.oauthStore.ListTokens(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to list tokens"})
		return
	}

	items := make([]map[string]any, 0, len(tokens))
	for _, t := range tokens {
		item := map[string]any{
			"provider":   t.Provider,
			"account":    t.Account,
			"expires_at": t.ExpiresAt,
			"scope":      t.Scope,
			"token_type": t.TokenType,
		}
		items = append(items, item)
	}

	writeJSON(w, http.StatusOK, map[string]any{"tokens": items})
}

// handleOAuthTokenDelete removes an OAuth token for a provider/account.
func (s *Server) handleOAuthTokenDelete(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	account := chi.URLParam(r, "account")
	if provider == "" || account == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "provider and account required"})
		return
	}

	if s.oauthStore != nil {
		if err := s.oauthStore.DeleteToken(r.Context(), provider, account); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to delete token"})
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "provider": provider, "account": account})
}

// handleNodesList returns the status of all registered peer nodes.
func (s *Server) handleNodesList(w http.ResponseWriter, _ *http.Request) {
	if s.nodeRegistry == nil {
		writeJSON(w, http.StatusOK, map[string]any{"nodes": []any{}})
		return
	}
	writeJSON(w, http.StatusOK, s.nodeRegistry)
}

// handleMetricsJSON returns runtime metrics as JSON (for dashboard consumption).
func (s *Server) handleMetricsJSON(w http.ResponseWriter, _ *http.Request) {
	s.metrics.mu.RLock()
	defer s.metrics.mu.RUnlock()

	providerUsage := make(map[string]int64, len(s.metrics.ProviderUsage))
	for k, v := range s.metrics.ProviderUsage {
		providerUsage[k] = v
	}

	var cacheHits, cacheMisses int64
	if s.cache != nil {
		cacheHits, cacheMisses = s.cache.Stats()
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"requests_total":   s.metrics.RequestsTotal,
		"requests_success": s.metrics.RequestsSuccess,
		"requests_error":   s.metrics.RequestsError,
		"provider_usage":   providerUsage,
		"cache_hits":       cacheHits,
		"cache_misses":     cacheMisses,
	})
}

// handleSyncStatus returns the current cloud sync manager status.
func (s *Server) handleSyncStatus(w http.ResponseWriter, _ *http.Request) {
	if s.syncManager == nil {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
		return
	}
	writeJSON(w, http.StatusOK, s.syncManager.GetStatus())
}

// handleOAuthAuthorize builds an OAuth authorization URL.
// Query params: provider, auth_url, token_url, client_id, client_secret, scopes (comma-sep), redirect_uri.
func (s *Server) handleOAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	providerName := q.Get("provider")
	authURL := q.Get("auth_url")
	if providerName == "" || authURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "provider and auth_url query params required"})
		return
	}

	var scopes []string
	if scopeStr := q.Get("scopes"); scopeStr != "" {
		scopes = strings.Split(scopeStr, ",")
	}

	state := providerName + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	builtURL := oauth.BuildAuthURL(oauth.ProviderOAuthConfig{
		Name:         providerName,
		AuthURL:      authURL,
		TokenURL:     q.Get("token_url"),
		ClientID:     q.Get("client_id"),
		ClientSecret: q.Get("client_secret"),
		Scopes:       scopes,
		RedirectURL:  q.Get("redirect_uri"),
	}, state)

	http.Redirect(w, r, builtURL, http.StatusFound)
}

// handleOAuthCallback handles the OAuth authorization code callback.
// Query params: code, state, token_url, client_id, client_secret, redirect_uri.
func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	code := q.Get("code")
	state := q.Get("state")
	if code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "code query param required"})
		return
	}

	// Extract provider name from state (format: "providerName-timestamp")
	providerName := state
	if idx := strings.LastIndex(state, "-"); idx > 0 {
		providerName = state[:idx]
	}

	rec, err := oauth.ExchangeCode(r.Context(), oauth.ProviderOAuthConfig{
		Name:         providerName,
		TokenURL:     q.Get("token_url"),
		ClientID:     q.Get("client_id"),
		ClientSecret: q.Get("client_secret"),
		RedirectURL:  q.Get("redirect_uri"),
	}, code)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}

	rec.Provider = providerName
	if rec.Account == "" {
		rec.Account = "default"
	}

	if s.oauthStore != nil {
		if err := s.oauthStore.SaveToken(r.Context(), *rec); err != nil {
			s.logger.Error().Err(err).Str("provider", providerName).Msg("oauth callback: failed to persist token")
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"provider":   providerName,
		"expires_at": rec.ExpiresAt,
		"scope":      rec.Scope,
	})
}

// handleMessagesCountTokens implements POST /v1/messages/count_tokens (Anthropic-compatible).
// It returns an estimated input_tokens count based on a rough heuristic (4 chars ≈ 1 token).
// A real implementation would use a provider-specific tokenizer; this satisfies the API contract
// so that clients like Claude Code do not error out.
func (s *Server) handleMessagesCountTokens(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var req struct {
		Model    string                   `json:"model"`
		Messages []map[string]interface{} `json:"messages"`
		System   interface{}              `json:"system,omitempty"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid json", "invalid_request_error", "")
		return
	}

	// Rough estimation: 4 chars ≈ 1 token (BPE average for English text).
	charCount := 0
	for _, msg := range req.Messages {
		if c, ok := msg["content"].(string); ok {
			charCount += len(c)
		}
	}
	if sys, ok := req.System.(string); ok {
		charCount += len(sys)
	}
	inputTokens := charCount / 4
	if inputTokens < 1 && charCount > 0 {
		inputTokens = 1
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"input_tokens": inputTokens,
	})
}

// handleModelGet implements GET /v1beta/models/{model} (Gemini-style single model lookup).
func (s *Server) handleModelGet(w http.ResponseWriter, r *http.Request) {
	modelID := chi.URLParam(r, "model")

	// Search in routes, aliases, custom models, providers.
	routesMap := s.runtimeConfig.ListRoutes()
	if _, ok := routesMap[modelID]; ok {
		writeJSON(w, http.StatusOK, map[string]any{
			"id":       modelID,
			"object":   "model",
			"type":     "route",
			"owned_by": "9router",
		})
		return
	}

	aliasesMap := s.runtimeConfig.ListModelAliases()
	if _, ok := aliasesMap[modelID]; ok {
		writeJSON(w, http.StatusOK, map[string]any{
			"id":       modelID,
			"object":   "model",
			"type":     "alias",
			"owned_by": "9router",
		})
		return
	}

	customModels := s.runtimeConfig.ListCustomModels()
	if cm, ok := customModels[modelID]; ok {
		writeJSON(w, http.StatusOK, map[string]any{
			"id":          modelID,
			"object":      "model",
			"type":        "custom",
			"owned_by":    "9router",
			"description": cm.Description,
		})
		return
	}

	writeJSON(w, http.StatusNotFound, map[string]any{
		"error": map[string]any{
			"message": "model not found: " + modelID,
			"type":    "invalid_request_error",
			"code":    "model_not_found",
		},
	})
}

// handleCodexCompat handles /codex/* requests that are not the canonical /codex/v1/responses path.
// It returns a 404 with a helpful message directing clients to /v1/responses.
func (s *Server) handleCodexCompat(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(chi.URLParam(r, "path"))
	if path == "v1/responses" {
		s.handleResponses(w, r)
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]any{
		"error": map[string]any{
			"message": "codex path not supported: /codex/" + path + " — use POST /v1/responses or /v1/responses/compact instead",
			"type":    "invalid_request_error",
			"code":    "unsupported_path",
		},
	})
}

func isCompactResponsesPath(path string) bool {
	path = strings.TrimSpace(path)
	return strings.HasSuffix(path, "/responses/compact")
}
