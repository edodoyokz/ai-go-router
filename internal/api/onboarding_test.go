package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/edodoyokz/ai-go-router/internal/cache"
	"github.com/edodoyokz/ai-go-router/internal/config"
	"github.com/edodoyokz/ai-go-router/internal/i18n"
	"github.com/edodoyokz/ai-go-router/internal/providers"
	routing "github.com/edodoyokz/ai-go-router/internal/router"
	"github.com/edodoyokz/ai-go-router/internal/storage"
	"github.com/edodoyokz/ai-go-router/internal/usage"
	"github.com/rs/zerolog"
	"gopkg.in/yaml.v3"
)

func newOnboardingServer(t *testing.T) (*Server, func()) {
	t.Helper()

	dbFile, err := os.CreateTemp(t.TempDir(), "onboarding-*.db")
	if err != nil {
		t.Fatal(err)
	}
	dbFile.Close()

	db, err := storage.NewDB(dbFile.Name())
	if err != nil {
		t.Fatal(err)
	}

	logger := zerolog.Nop()
	asyncWriter := storage.NewAsyncWriter(db, logger)

	cfg := config.Config{
		Server: config.ServerConfig{
			APIKey:                "test-key",
			RequestTimeoutSeconds: 30,
			Host:                  "127.0.0.1",
			Port:                  1988,
		},
		Logging: config.LoggingConfig{Level: "info"},
		Storage: config.StorageConfig{SQLitePath: dbFile.Name()},
		Retry:   config.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 100, MaxBackoffMs: 1000, MaxCooldownMs: 1000},
		Providers: []config.ProviderConfig{{
			Name:    "openai",
			Type:    "openai_compat",
			BaseURL: "https://api.openai.com",
			APIKey:  "sk-test",
			Enabled: false,
		}},
		Routes:       map[string]config.RouteConfig{},
		ModelAliases: map[string]config.ModelAlias{},
	}

	configFile, err := os.CreateTemp(t.TempDir(), "config-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	configFile.Close()

	configBytes, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configFile.Name(), configBytes, 0600); err != nil {
		t.Fatal(err)
	}

	runtimeCfg := config.NewRuntimeConfig(cfg, configFile.Name())
	registry, err := providers.BuildRegistryFromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}

	pricingReg := usage.NewPricingRegistry()
	pricingReg.LoadDefaults()

	s := &Server{
		runtimeConfig: runtimeCfg,
		logger:        logger,
		engine:        routing.NewEngine(cfg.Routes, cfg.ModelAliases, registry, cfg.Retry),
		asyncWriter:   asyncWriter,
		metrics: &Metrics{
			ProviderUsage: make(map[string]int64),
		},
		cache:           cache.NewLRUCache(100),
		pricingRegistry: pricingReg,
		usageFetcher:    usage.NewUsageFetcher(),
		i18nBundle:      i18n.NewBundle(),
	}

	return s, func() {
		asyncWriter.Close()
		db.Close()
	}
}

func TestHandler_RootRedirectsToUI(t *testing.T) {
	s, cleanup := newOnboardingServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusMovedPermanently {
		t.Fatalf("expected 301, got %d", w.Code)
	}
	if got := w.Header().Get("Location"); got != "/ui/" {
		t.Fatalf("expected redirect to /ui/, got %q", got)
	}
}

func TestHandleSetupStatus_OnboardingMode(t *testing.T) {
	s, cleanup := newOnboardingServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/setup/status", nil)
	w := httptest.NewRecorder()
	s.handleSetupStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["configured"] != false {
		t.Fatalf("expected configured false, got %v", resp["configured"])
	}
	if resp["has_providers"] != true {
		t.Fatalf("expected has_providers true, got %v", resp["has_providers"])
	}
	if resp["enabled_providers"].(float64) != 0 {
		t.Fatalf("expected zero enabled providers, got %v", resp["enabled_providers"])
	}
	if resp["auth_required"] != true {
		t.Fatalf("expected auth_required true, got %v", resp["auth_required"])
	}
}

func TestHandleReadyz_NoEnabledProviders_ReturnsNotReady(t *testing.T) {
	s, cleanup := newOnboardingServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	s.handleReadyz(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "not ready" {
		t.Fatalf("expected status not ready, got %v", resp["status"])
	}
}

func TestHandleChatCompletions_OnboardingMode_ReturnsServiceUnavailable(t *testing.T) {
	s, cleanup := newOnboardingServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"fast","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.handleChatCompletions(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}
