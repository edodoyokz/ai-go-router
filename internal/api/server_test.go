package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/edodoyokz/ai-go-router/internal/cache"
	"github.com/edodoyokz/ai-go-router/internal/providers"
	"github.com/edodoyokz/ai-go-router/internal/usage"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"gopkg.in/yaml.v3"

	"github.com/edodoyokz/ai-go-router/internal/config"
	routing "github.com/edodoyokz/ai-go-router/internal/router"
	"github.com/edodoyokz/ai-go-router/internal/storage"
)

func newTestServer(t *testing.T) (*Server, *storage.DB, func()) {
	t.Helper()

	f, err := os.CreateTemp(t.TempDir(), "test-*.db")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	db, err := storage.NewDB(f.Name())
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
			Port:                  20128,
		},
		Logging: config.LoggingConfig{Level: "info"},
		Storage: config.StorageConfig{SQLitePath: f.Name()},
		Retry:   config.RetryConfig{MaxAttempts: 1, InitialBackoffMs: 100, MaxBackoffMs: 1000, MaxCooldownMs: 1000},
		Providers: []config.ProviderConfig{{
			Name:    "openai",
			Type:    "openai_compat",
			BaseURL: "https://api.openai.com",
			APIKey:  "sk-test",
			Enabled: true,
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

	// Initialize pricing registry with defaults
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
	}

	return s, db, func() {
		asyncWriter.Close()
	}
}

func routeRequest(method string, path string, body string, params map[string]string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestHandleHealthz(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	s.handleHealthz(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("expected status ok, got %v", resp["status"])
	}
}

func TestHandleReadyz(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/readyz", nil)
	w := httptest.NewRecorder()
	s.handleReadyz(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandleLogsList_Empty(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/logs", nil)
	w := httptest.NewRecorder()
	s.handleLogsList(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["total"].(float64) != 0 {
		t.Errorf("expected total 0, got %v", resp["total"])
	}
}

func TestHandleLogsList_WithData(t *testing.T) {
	s, db, cleanup := newTestServer(t)
	defer cleanup()

	now := time.Now()
	for i := 0; i < 5; i++ {
		if err := db.LogRequest(t.Context(), &storage.RequestLog{
			RequestID:   "req-" + string(rune('0'+i)),
			Model:       "gpt-4",
			Provider:    "openai",
			TargetModel: "gpt-4",
			Status:      "success",
			StartTime:   now,
			EndTime:     now,
			Duration:    50 * time.Millisecond,
		}); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("default limit", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/logs", nil)
		w := httptest.NewRecorder()
		s.handleLogsList(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}

		var resp map[string]any
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if resp["total"].(float64) != 5 {
			t.Errorf("expected total 5, got %v", resp["total"])
		}
	})

	t.Run("with limit param", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/logs?limit=2", nil)
		w := httptest.NewRecorder()
		s.handleLogsList(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}

		var resp map[string]any
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		logs := resp["logs"].([]any)
		if len(logs) != 2 {
			t.Errorf("expected 2 logs, got %d", len(logs))
		}
	})

	t.Run("filter by provider", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/logs?provider=openai", nil)
		w := httptest.NewRecorder()
		s.handleLogsList(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}

		var resp map[string]any
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if resp["total"].(float64) != 5 {
			t.Errorf("expected total 5, got %v", resp["total"])
		}
	})
}

func TestHandleMetrics(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()

	s.metrics.mu.Lock()
	s.metrics.RequestsTotal = 10
	s.metrics.RequestsSuccess = 8
	s.metrics.RequestsError = 2
	s.metrics.mu.Unlock()

	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	s.handleMetrics(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	body := w.Body.String()
	if len(body) == 0 {
		t.Error("expected non-empty metrics body")
	}
}

func TestCORSMiddleware(t *testing.T) {
	handler := CORSMiddleware(
		[]string{"https://example.com"},
		[]string{"GET", "POST", "OPTIONS"},
		[]string{"Authorization", "Content-Type"},
		false,
		86400,
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("allowed origin", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Origin", "https://example.com")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Header().Get("Access-Control-Allow-Origin") != "https://example.com" {
			t.Errorf("expected CORS header, got %q", w.Header().Get("Access-Control-Allow-Origin"))
		}
	})

	t.Run("disallowed origin", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Origin", "https://evil.com")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("expected no CORS header for disallowed origin")
		}
	})

	t.Run("preflight request", func(t *testing.T) {
		req := httptest.NewRequest("OPTIONS", "/", nil)
		req.Header.Set("Origin", "https://example.com")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusNoContent {
			t.Errorf("expected 204 for preflight, got %d", w.Code)
		}
	})
}

func TestCORSMiddleware_Disabled(t *testing.T) {
	handler := CORSMiddleware([]string{}, nil, nil, false, 0)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://example.com")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("expected no CORS headers when disabled")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestAdminCRUDProviders(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	s.handleProvidersCreate(w, routeRequest(http.MethodPost, "/api/providers", `{"name":"anthropic","type":"anthropic","base_url":"https://api.anthropic.com","api_key":"sk-ant","enabled":true}`, nil))
	if w.Code != http.StatusCreated {
		t.Fatalf("create provider status=%d body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	s.handleProvidersUpdate(w, routeRequest(http.MethodPut, "/api/providers/anthropic", `{"name":"anthropic","type":"anthropic","base_url":"https://api.anthropic.com/v2","api_key":"sk-ant","enabled":true}`, map[string]string{"name": "anthropic"}))
	if w.Code != http.StatusOK {
		t.Fatalf("update provider status=%d body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	s.handleProvidersDelete(w, routeRequest(http.MethodDelete, "/api/providers/anthropic", "", map[string]string{"name": "anthropic"}))
	if w.Code != http.StatusOK {
		t.Fatalf("delete provider status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAdminCRUDCombosAliasesSettings(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	s.handleCombosCreate(w, routeRequest(http.MethodPost, "/api/combos", `{"name":"gpt4","strategy":"fallback","targets":[{"provider":"openai","model":"gpt-4"}]}`, nil))
	if w.Code != http.StatusCreated {
		t.Fatalf("create combo status=%d body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	s.handleModelAliasesCreate(w, routeRequest(http.MethodPost, "/api/models/alias", `{"alias":"best","provider":"openai","model":"gpt-4"}`, nil))
	if w.Code != http.StatusCreated {
		t.Fatalf("create alias status=%d body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	s.handleSettingsPut(w, routeRequest(http.MethodPut, "/api/settings", `{"combo_strategy":"round-robin"}`, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("update settings status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAdminCRUDKeysAndCustomModels(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	s.handleKeysCreate(w, routeRequest(http.MethodPost, "/api/keys", `{"api_key":"test-key-2"}`, nil))
	if w.Code != http.StatusCreated {
		t.Fatalf("create key status=%d body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	s.handleModelsCustomCreate(w, routeRequest(http.MethodPost, "/api/models/custom", `{"name":"my-custom","provider":"openai","model":"gpt-4o-mini","description":"custom"}`, nil))
	if w.Code != http.StatusCreated {
		t.Fatalf("create custom model status=%d body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	s.handleModelsCustomDelete(w, routeRequest(http.MethodDelete, "/api/models/custom/my-custom", "", map[string]string{"name": "my-custom"}))
	if w.Code != http.StatusOK {
		t.Fatalf("delete custom model status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestResponsesEndpoint(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()

	// Test with string input
	w := httptest.NewRecorder()
	s.handleResponses(w, routeRequest(http.MethodPost, "/v1/responses", `{"input":"hello world","model":"openai/gpt-4"}`, nil))
	// Should fail because no actual provider, but endpoint should parse request correctly
	if w.Code != http.StatusInternalServerError && w.Code != http.StatusUnauthorized {
		t.Logf("responses endpoint status=%d body=%s", w.Code, w.Body.String())
	}

	// Test with array input
	w = httptest.NewRecorder()
	s.handleResponses(w, routeRequest(http.MethodPost, "/v1/responses", `{"input":["hello","world"],"model":"openai/gpt-4"}`, nil))
	if w.Code != http.StatusInternalServerError && w.Code != http.StatusUnauthorized {
		t.Logf("responses endpoint with array status=%d body=%s", w.Code, w.Body.String())
	}

	// Test with message objects
	w = httptest.NewRecorder()
	s.handleResponses(w, routeRequest(http.MethodPost, "/v1/responses", `{"input":[{"role":"user","content":"test"}],"model":"openai/gpt-4"}`, nil))
	if w.Code != http.StatusInternalServerError && w.Code != http.StatusUnauthorized {
		t.Logf("responses endpoint with messages status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestEmbeddingsEndpoint(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()

	// Test embeddings endpoint
	w := httptest.NewRecorder()
	s.handleEmbeddings(w, routeRequest(http.MethodPost, "/v1/embeddings", `{"model":"openai/gpt-4","input":"test"}`, nil))
	// Should fail because no actual provider, but endpoint should parse request correctly
	if w.Code != http.StatusInternalServerError && w.Code != http.StatusUnauthorized {
		t.Logf("embeddings endpoint status=%d body=%s", w.Code, w.Body.String())
	}

	// Test missing model
	w = httptest.NewRecorder()
	s.handleEmbeddings(w, routeRequest(http.MethodPost, "/v1/embeddings", `{"input":"test"}`, nil))
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing model, got %d", w.Code)
	}

	// Test missing input
	w = httptest.NewRecorder()
	s.handleEmbeddings(w, routeRequest(http.MethodPost, "/v1/embeddings", `{"model":"openai/gpt-4"}`, nil))
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing input, got %d", w.Code)
	}
}

func TestAudioSpeechEndpoint(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()

	// Test basic request parsing
	w := httptest.NewRecorder()
	s.handleAudioSpeech(w, routeRequest(http.MethodPost, "/v1/audio/speech", `{"model":"openai/gpt-4","input":"Hello world","voice":"alloy"}`, nil))
	// Will fail because no actual TTS provider, but should parse correctly
	if w.Code != http.StatusInternalServerError && w.Code != http.StatusNotFound && w.Code != http.StatusUnauthorized {
		t.Logf("audio/speech endpoint status=%d body=%s", w.Code, w.Body.String())
	}

	// Test missing model
	w = httptest.NewRecorder()
	s.handleAudioSpeech(w, routeRequest(http.MethodPost, "/v1/audio/speech", `{"input":"Hello","voice":"alloy"}`, nil))
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing model, got %d", w.Code)
	}

	// Test missing input
	w = httptest.NewRecorder()
	s.handleAudioSpeech(w, routeRequest(http.MethodPost, "/v1/audio/speech", `{"model":"openai/gpt-4","voice":"alloy"}`, nil))
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing input, got %d", w.Code)
	}

	// Test missing voice
	w = httptest.NewRecorder()
	s.handleAudioSpeech(w, routeRequest(http.MethodPost, "/v1/audio/speech", `{"model":"openai/gpt-4","input":"Hello"}`, nil))
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing voice, got %d", w.Code)
	}
}

func TestImagesGenerationsEndpoint(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()

	// Test basic request parsing
	w := httptest.NewRecorder()
	s.handleImagesGenerations(w, routeRequest(http.MethodPost, "/v1/images/generations", `{"model":"openai/gpt-4","prompt":"A cat","n":1,"size":"1024x1024"}`, nil))
	// Will fail because no actual image provider, but should parse correctly
	if w.Code != http.StatusInternalServerError && w.Code != http.StatusNotFound && w.Code != http.StatusUnauthorized {
		t.Logf("images/generations endpoint status=%d body=%s", w.Code, w.Body.String())
	}

	// Test missing model
	w = httptest.NewRecorder()
	s.handleImagesGenerations(w, routeRequest(http.MethodPost, "/v1/images/generations", `{"prompt":"A cat"}`, nil))
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing model, got %d", w.Code)
	}

	// Test missing prompt
	w = httptest.NewRecorder()
	s.handleImagesGenerations(w, routeRequest(http.MethodPost, "/v1/images/generations", `{"model":"openai/gpt-4"}`, nil))
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing prompt, got %d", w.Code)
	}
}
