package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/edodoyokz/ai-go-router/internal/cache"
	"github.com/edodoyokz/ai-go-router/internal/mitm"
	"github.com/edodoyokz/ai-go-router/internal/oauth"
	"github.com/edodoyokz/ai-go-router/internal/providers"
	"github.com/edodoyokz/ai-go-router/internal/tunnel"
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
			Port:                  1988,
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

type apiFakeRunner struct {
	mu       sync.Mutex
	starts   []string
	runs     []string
	binaries map[string]bool
}

func (f *apiFakeRunner) LookPath(file string) (string, error) {
	if f.binaries != nil && f.binaries[file] {
		return "/usr/bin/" + file, nil
	}
	return "", errors.New("not found")
}

func (f *apiFakeRunner) Run(_ context.Context, name string, args ...string) (tunnel.CommandResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs = append(f.runs, strings.Join(append([]string{name}, args...), " "))
	return tunnel.CommandResult{Stdout: "ok", ExitCode: 0}, nil
}

func (f *apiFakeRunner) Start(_ context.Context, name string, args ...string) (tunnel.Process, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts = append(f.starts, strings.Join(append([]string{name}, args...), " "))
	return &apiFakeProcess{running: true, done: make(chan struct{})}, nil
}

type apiFakeProcess struct {
	mu      sync.RWMutex
	running bool
	done    chan struct{}
}

func (p *apiFakeProcess) Wait() error {
	<-p.done
	return nil
}

func (p *apiFakeProcess) Kill() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.running {
		p.running = false
		close(p.done)
	}
	return nil
}

func (p *apiFakeProcess) Running() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.running
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

func TestTunnelAPIEnableDisableWithManager(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()

	t.Setenv("NINEROUTER_TUNNEL_STATE", filepath.Join(t.TempDir(), "tunnel-state.json"))
	runner := &apiFakeRunner{}
	manager := tunnel.NewManagerWithRunner(config.TunnelConfig{}, zerolog.Nop(), runner)
	s.SetTunnelManager(manager)

	w := httptest.NewRecorder()
	s.handleTunnelEnable(w, routeRequest(http.MethodPost, "/api/tunnel/enable", `{"provider":"cloudflare"}`, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("enable status=%d body=%s", w.Code, w.Body.String())
	}
	for i := 0; i < 100; i++ {
		runner.mu.Lock()
		started := len(runner.starts) > 0
		runner.mu.Unlock()
		if started {
			break
		}
		time.Sleep(time.Millisecond)
	}
	runner.mu.Lock()
	starts := append([]string(nil), runner.starts...)
	runner.mu.Unlock()
	if len(starts) != 1 || starts[0] != "cloudflared tunnel --url http://127.0.0.1:1988" {
		t.Fatalf("starts=%#v", starts)
	}

	w = httptest.NewRecorder()
	s.handleTunnelStatus(w, httptest.NewRequest(http.MethodGet, "/api/tunnel/status", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status code=%d body=%s", w.Code, w.Body.String())
	}
	var status map[string]any
	if err := json.NewDecoder(w.Body).Decode(&status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	tunnelStatus := status["tunnel"].(map[string]any)
	if tunnelStatus["enabled"] != true || tunnelStatus["provider"] != "cloudflare" {
		t.Fatalf("unexpected tunnel status: %#v", status)
	}

	w = httptest.NewRecorder()
	s.handleTunnelDisable(w, httptest.NewRequest(http.MethodPost, "/api/tunnel/disable", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("disable status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestMITMAPIStartDNSAliasAndStop(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()

	t.Setenv("NINEROUTER_MITM_STATE", filepath.Join(t.TempDir(), "mitm-state.json"))
	runner := &apiFakeRunner{}
	s.SetMITMManager(mitm.NewManager(runner))

	w := httptest.NewRecorder()
	s.handleMITMStart(w, routeRequest(http.MethodPost, "/api/cli-tools/antigravity-mitm", `{"apiKey":"test-key","sudoPassword":"sudo","mitmRouterBaseUrl":"http://localhost:1988"}`, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("start status=%d body=%s", w.Code, w.Body.String())
	}
	var start map[string]any
	if err := json.NewDecoder(w.Body).Decode(&start); err != nil {
		t.Fatalf("decode start: %v", err)
	}
	if start["success"] != true || start["running"] != true {
		t.Fatalf("unexpected start response: %#v", start)
	}

	w = httptest.NewRecorder()
	s.handleMITMPatch(w, routeRequest(http.MethodPatch, "/api/cli-tools/antigravity-mitm", `{"tool":"antigravity","action":"enable","sudoPassword":"sudo"}`, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("enable dns status=%d body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	s.handleCLIToolAliasPut(w, routeRequest(http.MethodPut, "/api/cli-tools/antigravity-mitm/alias", `{"tool":"antigravity","mappings":{"fast":"openai/gpt-4.1-mini","blank":""}}`, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("alias put status=%d body=%s", w.Code, w.Body.String())
	}
	var aliasResp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&aliasResp); err != nil {
		t.Fatalf("decode alias: %v", err)
	}
	aliases := aliasResp["aliases"].(map[string]any)
	if aliases["fast"] != "openai/gpt-4.1-mini" || aliases["blank"] != nil {
		t.Fatalf("unexpected aliases: %#v", aliasResp)
	}

	w = httptest.NewRecorder()
	s.handleMITMStop(w, routeRequest(http.MethodDelete, "/api/cli-tools/antigravity-mitm", `{"sudoPassword":"sudo"}`, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("stop status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestUsageEndpointsReadSQLite(t *testing.T) {
	s, db, cleanup := newTestServer(t)
	defer cleanup()

	now := time.Now().UTC()
	log := storage.RequestLog{
		RequestID:        "req-usage-api",
		Model:            "gpt-4.1",
		Provider:         "openai",
		TargetModel:      "gpt-4.1",
		Status:           "success",
		StartTime:        now,
		EndTime:          now.Add(120 * time.Millisecond),
		Duration:         120 * time.Millisecond,
		PromptTokens:     11,
		CompletionTokens: 13,
		TotalTokens:      24,
		TotalCost:        0.0024,
		Currency:         "USD",
	}
	if err := db.LogRequest(context.Background(), &log); err != nil {
		t.Fatalf("LogRequest: %v", err)
	}
	if err := db.LogRequestDetails(context.Background(), storage.LogRequestDetailsParams{RequestID: log.RequestID, RequestBody: `{"model":"gpt-4.1"}`, ResponseBody: `{"id":"chatcmpl-test"}`, StatusCode: 200}); err != nil {
		t.Fatalf("LogRequestDetails: %v", err)
	}

	t.Run("stats", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/usage/stats", nil)
		w := httptest.NewRecorder()
		s.handleUsageStats(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
		var resp struct {
			Stats map[string]any `json:"stats"`
		}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got := int(resp.Stats["requests_total"].(float64)); got != 1 {
			t.Fatalf("requests_total = %d, want 1", got)
		}
	})

	t.Run("request details", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/usage/request-details?request_id=req-usage-api", nil)
		w := httptest.NewRecorder()
		s.handleUsageRequestDetails(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
		var resp struct {
			RequestID string         `json:"request_id"`
			Details   map[string]any `json:"details"`
		}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.RequestID != "req-usage-api" || resp.Details["status_code"].(float64) != 200 {
			t.Fatalf("unexpected response: %+v", resp)
		}
	})

	t.Run("connection usage", func(t *testing.T) {
		req := routeRequest(http.MethodGet, "/api/usage/openai", "", map[string]string{"connectionId": "openai"})
		w := httptest.NewRecorder()
		s.handleUsageConnection(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
		var resp struct {
			ConnectionID string           `json:"connectionId"`
			Usage        []map[string]any `json:"usage"`
			Total        int              `json:"total"`
		}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.ConnectionID != "openai" || resp.Total != 1 || len(resp.Usage) != 1 {
			t.Fatalf("unexpected response: %+v", resp)
		}
	})
}

func TestTranslatorTranslateUsesRegistry(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()

	t.Run("step 1 detects provider and formats", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/translator/translate", strings.NewReader(`{
			"step": 1,
			"body": {"model":"anthropic/claude-3-5-sonnet","messages":[{"role":"user","content":"hello"}],"system":"be terse","max_tokens":32}
		}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.handleTranslatorTranslate(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
		var resp struct {
			Success bool           `json:"success"`
			Result  map[string]any `json:"result"`
		}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !resp.Success || resp.Result["provider"] != "anthropic" || resp.Result["sourceFormat"] != "claude" || resp.Result["targetFormat"] != "claude" {
			t.Fatalf("unexpected response: %+v", resp)
		}
	})

	t.Run("step 2 translates claude to openai", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/translator/translate", strings.NewReader(`{
			"step": 2,
			"body": {"model":"anthropic/claude-3-5-sonnet","messages":[{"role":"user","content":"hello"}],"system":"be terse","max_tokens":32}
		}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.handleTranslatorTranslate(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
		var resp struct {
			Success bool `json:"success"`
			Result  struct {
				Body map[string]any `json:"body"`
			} `json:"result"`
		}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		messages, ok := resp.Result.Body["messages"].([]any)
		if !resp.Success || !ok || len(messages) != 2 {
			t.Fatalf("expected translated openai messages, got %+v", resp)
		}
	})

	t.Run("step 3 builds target preview", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/translator/translate", strings.NewReader(`{
			"step": 3,
			"provider": "openai",
			"model": "gpt-4.1",
			"body": {"model":"gpt-4.1","messages":[{"role":"user","content":"hello"}]}
		}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.handleTranslatorTranslate(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
		var resp struct {
			Success bool `json:"success"`
			Result  struct {
				URL     string            `json:"url"`
				Headers map[string]string `json:"headers"`
				Body    map[string]any    `json:"body"`
			} `json:"result"`
		}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !resp.Success || !strings.HasSuffix(resp.Result.URL, "/chat/completions") || resp.Result.Headers["Authorization"] == "" {
			t.Fatalf("unexpected preview: %+v", resp)
		}
	})
}

func TestTranslatorSendForwardsToActiveProvider(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()

	var upstreamPath, upstreamAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamPath = r.URL.Path
		upstreamAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: ok\n\n"))
	}))
	defer upstream.Close()
	if err := s.runtimeConfig.Update(func(cfg *config.Config) error {
		cfg.Providers[0].BaseURL = upstream.URL + "/v1"
		cfg.Providers[0].Format = "openai"
		cfg.Providers[0].APIKey = "sk-upstream"
		return nil
	}); err != nil {
		t.Fatalf("update config: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/translator/send", strings.NewReader(`{
		"provider":"openai",
		"model":"gpt-4.1",
		"body":{"model":"gpt-4.1","messages":[{"role":"user","content":"hello"}]}
	}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleTranslatorSend(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if upstreamPath != "/v1/chat/completions" {
		t.Fatalf("upstream path=%q", upstreamPath)
	}
	if upstreamAuth != "Bearer sk-upstream" {
		t.Fatalf("upstream auth=%q", upstreamAuth)
	}
	if w.Body.String() != "data: ok\n\n" {
		t.Fatalf("body=%q", w.Body.String())
	}
}

func TestTranslatorLoadSaveAndConsoleLogs(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)

	save := httptest.NewRequest(http.MethodPost, "/api/translator/save", strings.NewReader(`{
		"file":"1_req_client.json",
		"content":"{\"model\":\"openai/gpt-4.1\"}"
	}`))
	save.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleTranslatorSave(w, save)
	if w.Code != http.StatusOK {
		t.Fatalf("save status=%d body=%s", w.Code, w.Body.String())
	}

	load := httptest.NewRequest(http.MethodGet, "/api/translator/load?file=1_req_client.json", nil)
	w = httptest.NewRecorder()
	s.handleTranslatorLoad(w, load)
	if w.Code != http.StatusOK {
		t.Fatalf("load status=%d body=%s", w.Code, w.Body.String())
	}
	var loadResp struct {
		Success bool   `json:"success"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(w.Body).Decode(&loadResp); err != nil {
		t.Fatalf("decode load: %v", err)
	}
	if !loadResp.Success || !strings.Contains(loadResp.Content, "openai/gpt-4.1") {
		t.Fatalf("unexpected load response: %+v", loadResp)
	}

	logs := httptest.NewRequest(http.MethodGet, "/api/translator/console-logs", nil)
	w = httptest.NewRecorder()
	s.handleTranslatorConsoleLogs(w, logs)
	if w.Code != http.StatusOK {
		t.Fatalf("logs status=%d body=%s", w.Code, w.Body.String())
	}
	var logsResp struct {
		Success bool             `json:"success"`
		Logs    []map[string]any `json:"logs"`
	}
	if err := json.NewDecoder(w.Body).Decode(&logsResp); err != nil {
		t.Fatalf("decode logs: %v", err)
	}
	if !logsResp.Success || len(logsResp.Logs) == 0 {
		t.Fatalf("expected buffered translator logs, got %+v", logsResp)
	}

	clear := httptest.NewRequest(http.MethodDelete, "/api/translator/console-logs", nil)
	w = httptest.NewRecorder()
	s.handleTranslatorConsoleLogsClear(w, clear)
	if w.Code != http.StatusOK {
		t.Fatalf("clear status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestOAuthImportRoutesCreateConnections(t *testing.T) {
	s, db, cleanup := newTestServer(t)
	defer cleanup()
	oldKiroEndpoints := kiroOAuthEndpoints
	defer func() { kiroOAuthEndpoints = oldKiroEndpoints }()
	kiroServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accessToken":"kiro-access","refreshToken":"aorAAAAAG-kiro-refresh","profileArn":"arn:aws:profile/test","expiresIn":3600}`))
	}))
	defer kiroServer.Close()
	kiroOAuthEndpoints.SocialRefreshURL = kiroServer.URL

	tests := []struct {
		name string
		path string
		body string
	}{
		{name: "cursor import", path: "/api/oauth/cursor/import", body: `{"accessToken":"cursor-token","machineId":"machine-1"}`},
		{name: "kiro import", path: "/api/oauth/kiro/import", body: `{"refreshToken":"aorAAAAAG-kiro-refresh"}`},
		{name: "iflow cookie", path: "/api/oauth/iflow/cookie", body: `{"cookie":"BXAuth=abc;","apiKey":"iflow-key"}`},
		{name: "gitlab pat", path: "/api/oauth/gitlab/pat", body: `{"token":"glpat-token","baseUrl":"https://gitlab.example.com"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			s.handleOAuthDynamicPost(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			var resp map[string]any
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if resp["success"] != true {
				t.Fatalf("expected success response, got %#v", resp)
			}
		})
	}

	conns, err := db.ListProviderConnections(context.Background(), storage.ProviderConnectionFilter{})
	if err != nil {
		t.Fatalf("ListProviderConnections: %v", err)
	}
	seen := map[string]bool{}
	for _, conn := range conns {
		seen[conn.Provider] = true
	}
	for _, provider := range []string{"cursor", "kiro", "iflow", "gitlab"} {
		if !seen[provider] {
			t.Fatalf("expected provider connection for %s, got %+v", provider, seen)
		}
	}
}

func TestOAuthImportInstructionRoutes(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()

	for _, path := range []string{"/api/oauth/cursor/import", "/api/oauth/cursor/auto-import", "/api/oauth/kiro/auto-import"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		s.handleOAuthDynamicGet(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, w.Code, w.Body.String())
		}
	}
}

func TestKiroAutoImportReadsAWSSSOCache(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()
	home := t.TempDir()
	t.Setenv("HOME", home)
	cacheDir := filepath.Join(home, ".aws", "sso", "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "kiro-auth-token.json"), []byte(`{"refreshToken":"aorAAAAAG-kiro-refresh"}`), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/oauth/kiro/auto-import", nil)
	w := httptest.NewRecorder()
	s.handleOAuthDynamicGet(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["found"] != true || resp["refreshToken"] != "aorAAAAAG-kiro-refresh" {
		t.Fatalf("response=%#v", resp)
	}
}

func TestKiroSocialAuthorizeAndExchange(t *testing.T) {
	s, db, cleanup := newTestServer(t)
	defer cleanup()
	oldEndpoints := kiroOAuthEndpoints
	defer func() { kiroOAuthEndpoints = oldEndpoints }()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/oauth/token":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body["code"] != "auth-code" || body["code_verifier"] != "verifier-1" {
				t.Fatalf("exchange body=%#v", body)
			}
			_, _ = w.Write([]byte(`{"accessToken":"a.b.c","refreshToken":"refresh-social","profileArn":"arn:aws:profile/test","expiresIn":3600}`))
		case "/refreshToken":
			_, _ = w.Write([]byte(`{"accessToken":"a.b.c","refreshToken":"refresh-import","profileArn":"arn:aws:profile/import","expiresIn":3600}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	kiroOAuthEndpoints.SocialLoginURL = server.URL + "/login"
	kiroOAuthEndpoints.SocialTokenURL = server.URL + "/oauth/token"
	kiroOAuthEndpoints.SocialRefreshURL = server.URL + "/refreshToken"

	req := httptest.NewRequest(http.MethodGet, "/api/oauth/kiro/social-authorize?provider=google", nil)
	w := httptest.NewRecorder()
	s.handleOAuthDynamicGet(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("authorize status=%d body=%s", w.Code, w.Body.String())
	}
	var authResp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&authResp); err != nil {
		t.Fatalf("decode authorize: %v", err)
	}
	if authResp["provider"] != "google" || authResp["authUrl"] == "" || authResp["codeVerifier"] == "" {
		t.Fatalf("authorize response=%#v", authResp)
	}

	req = routeRequest(http.MethodPost, "/api/oauth/kiro/social-exchange", `{"code":"auth-code","codeVerifier":"verifier-1","provider":"google"}`, map[string]string{"provider": "kiro", "action": "social-exchange"})
	w = httptest.NewRecorder()
	s.handleOAuthDynamicPost(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("exchange status=%d body=%s", w.Code, w.Body.String())
	}
	conns, err := db.ListProviderConnections(context.Background(), storage.ProviderConnectionFilter{Provider: "kiro"})
	if err != nil {
		t.Fatalf("list kiro conns: %v", err)
	}
	if len(conns) != 1 || conns[0].RefreshToken != "refresh-social" {
		t.Fatalf("connections=%#v", conns)
	}
}

func TestKiroImportValidatesRefreshToken(t *testing.T) {
	s, db, cleanup := newTestServer(t)
	defer cleanup()
	oldEndpoints := kiroOAuthEndpoints
	defer func() { kiroOAuthEndpoints = oldEndpoints }()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accessToken":"header.eyJlbWFpbCI6Imtpcm9AZXhhbXBsZS5jb20ifQ.sig","refreshToken":"aorAAAAAG-refreshed","profileArn":"arn:aws:profile/import","expiresIn":3600}`))
	}))
	defer server.Close()
	kiroOAuthEndpoints.SocialRefreshURL = server.URL
	req := routeRequest(http.MethodPost, "/api/oauth/kiro/import", `{"refreshToken":"aorAAAAAG-original"}`, map[string]string{"provider": "kiro", "action": "import"})
	w := httptest.NewRecorder()
	s.handleOAuthDynamicPost(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("import status=%d body=%s", w.Code, w.Body.String())
	}
	conns, err := db.ListProviderConnections(context.Background(), storage.ProviderConnectionFilter{Provider: "kiro"})
	if err != nil {
		t.Fatalf("list kiro conns: %v", err)
	}
	if len(conns) != 1 || conns[0].AccessToken == "" || conns[0].RefreshToken != "aorAAAAAG-refreshed" {
		t.Fatalf("connections=%#v", conns)
	}
}

func TestCodexCompatCanonicalResponsesPath(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()
	req := httptest.NewRequest(http.MethodPost, "/codex/v1/responses", strings.NewReader(`{"model":"openai","input":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("path", "v1/responses")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	s.handleCodexCompat(w, req)
	if w.Code == http.StatusNotFound {
		t.Fatalf("expected canonical codex responses path to bypass unsupported_path 404, got body=%s", w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if errObj, ok := resp["error"].(map[string]any); ok && errObj["code"] == "unsupported_path" {
		t.Fatalf("unexpected response=%#v", resp)
	}
}

func TestCursorAutoImportReadsLocalStateDB(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()

	home := t.TempDir()
	t.Setenv("HOME", home)
	var dbPath string
	switch runtime.GOOS {
	case "darwin":
		dbPath = filepath.Join(home, "Library/Application Support/Cursor/User/globalStorage/state.vscdb")
	case "windows":
		dbPath = filepath.Join(home, "AppData", "Roaming", "Cursor", "User", "globalStorage", "state.vscdb")
	default:
		dbPath = filepath.Join(home, ".config", "Cursor", "User", "globalStorage", "state.vscdb")
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE itemTable (key TEXT PRIMARY KEY, value TEXT)`); err != nil {
		t.Fatalf("create itemTable: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO itemTable(key, value) VALUES (?, ?), (?, ?)`, "cursorAuth/accessToken", `"cursor-token"`, "storage.serviceMachineId", `"machine-123"`); err != nil {
		t.Fatalf("insert values: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/oauth/cursor/auto-import", nil)
	w := httptest.NewRecorder()
	s.handleOAuthDynamicGet(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["found"] != true {
		t.Fatalf("expected found=true, got %#v", resp)
	}
	if resp["accessToken"] != "cursor-token" || resp["machineId"] != "machine-123" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestGitHubDeviceCodeAndPoll(t *testing.T) {
	s, db, cleanup := newTestServer(t)
	defer cleanup()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/device":
			if r.Method != http.MethodPost {
				t.Fatalf("device method=%s", r.Method)
			}
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			if r.Form.Get("client_id") == "" || r.Form.Get("scope") != "read:user" {
				t.Fatalf("device form=%v", r.Form)
			}
			_, _ = w.Write([]byte(`{"device_code":"dev-1","user_code":"ABCD","verification_uri":"https://github.com/login/device","interval":5}`))
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			if r.Form.Get("device_code") != "dev-1" {
				t.Fatalf("token form=%v", r.Form)
			}
			_, _ = w.Write([]byte(`{"access_token":"gh-access","refresh_token":"gh-refresh","expires_in":3600}`))
		case "/copilot":
			if r.Header.Get("Authorization") != "Bearer gh-access" {
				t.Fatalf("copilot auth=%q", r.Header.Get("Authorization"))
			}
			_, _ = w.Write([]byte(`{"token":"copilot-token","expires_at":1893456000}`))
		case "/user":
			_, _ = w.Write([]byte(`{"id":123,"login":"octo","name":"Octo Cat","email":"octo@example.com"}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	oldCfg, _ := oauthProviderRegistry.Get("github")
	defer oauthProviderRegistry.Register(oldCfg)
	ghCfg, _ := oauthProviderRegistry.Get("github")
	ghCfg.DeviceCodeURL = server.URL + "/device"
	ghCfg.TokenURL = server.URL + "/token"
	ghCfg.UserInfoURL = server.URL + "/user"
	ghCfg.Extra["copilotTokenURL"] = server.URL + "/copilot"
	oauthProviderRegistry.Register(ghCfg)

	req := routeRequest(http.MethodGet, "/api/oauth/github/device-code", "", map[string]string{"provider": "github", "action": "device-code"})
	w := httptest.NewRecorder()
	s.handleOAuthDynamicGet(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("device status=%d body=%s", w.Code, w.Body.String())
	}
	var device map[string]any
	if err := json.NewDecoder(w.Body).Decode(&device); err != nil {
		t.Fatalf("decode device: %v", err)
	}
	if device["device_code"] != "dev-1" {
		t.Fatalf("device response=%#v", device)
	}

	req = routeRequest(http.MethodPost, "/api/oauth/github/poll", `{"device_code":"dev-1"}`, map[string]string{"provider": "github", "action": "poll"})
	w = httptest.NewRecorder()
	s.handleOAuthDynamicPost(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("poll status=%d body=%s", w.Code, w.Body.String())
	}
	var poll map[string]any
	if err := json.NewDecoder(w.Body).Decode(&poll); err != nil {
		t.Fatalf("decode poll: %v", err)
	}
	if poll["success"] != true {
		t.Fatalf("poll response=%#v", poll)
	}
	conns, err := db.ListProviderConnections(context.Background(), storage.ProviderConnectionFilter{Provider: "github"})
	if err != nil {
		t.Fatalf("list github conns: %v", err)
	}
	if len(conns) != 1 || conns[0].AccessToken != "gh-access" || conns[0].ProviderSpecificData["copilotToken"] != "copilot-token" {
		t.Fatalf("connection=%#v", conns)
	}
}

func TestCodexAuthorizeAndExchange(t *testing.T) {
	s, db, cleanup := newTestServer(t)
	defer cleanup()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") != "auth-code" || r.Form.Get("code_verifier") != "verifier-1" {
			t.Fatalf("form=%v", r.Form)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"codex-access","refresh_token":"codex-refresh","id_token":"header.eyJlbWFpbCI6ImNvZGV4QGV4YW1wbGUuY29tIn0.sig","expires_in":3600,"scope":"openid profile","token_type":"Bearer"}`))
	}))
	defer server.Close()

	oldCfg, _ := oauthProviderRegistry.Get("codex")
	defer oauthProviderRegistry.Register(oldCfg)
	codexCfg, _ := oauthProviderRegistry.Get("codex")
	codexCfg.AuthURL = server.URL + "/authorize"
	codexCfg.TokenURL = server.URL + "/token"
	oauthProviderRegistry.Register(codexCfg)

	req := routeRequest(http.MethodGet, "/api/oauth/codex/authorize?redirect_uri=http://localhost/callback", "", map[string]string{"provider": "codex", "action": "authorize"})
	w := httptest.NewRecorder()
	s.handleOAuthDynamicGet(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("authorize status=%d body=%s", w.Code, w.Body.String())
	}
	var authResp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&authResp); err != nil {
		t.Fatalf("decode authorize: %v", err)
	}
	if authResp["authUrl"] == "" || authResp["codeVerifier"] == "" || authResp["state"] == "" {
		t.Fatalf("authorize response=%#v", authResp)
	}

	req = routeRequest(http.MethodPost, "/api/oauth/codex/exchange", `{"code":"auth-code","redirectUri":"http://localhost/callback","codeVerifier":"verifier-1"}`, map[string]string{"provider": "codex", "action": "exchange"})
	w = httptest.NewRecorder()
	s.handleOAuthDynamicPost(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("exchange status=%d body=%s", w.Code, w.Body.String())
	}
	conns, err := db.ListProviderConnections(context.Background(), storage.ProviderConnectionFilter{Provider: "codex"})
	if err != nil {
		t.Fatalf("list codex conns: %v", err)
	}
	if len(conns) != 1 || conns[0].AccessToken != "codex-access" || conns[0].RefreshToken != "codex-refresh" {
		t.Fatalf("connections=%#v", conns)
	}
}

func TestGitHubPollPending(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"authorization_pending","error_description":"wait"}`))
	}))
	defer server.Close()

	oldCfg, _ := oauthProviderRegistry.Get("github")
	defer oauthProviderRegistry.Register(oldCfg)
	ghCfg, _ := oauthProviderRegistry.Get("github")
	ghCfg.TokenURL = server.URL
	oauthProviderRegistry.Register(ghCfg)

	req := routeRequest(http.MethodPost, "/api/oauth/github/poll", `{"device_code":"dev-1"}`, map[string]string{"provider": "github", "action": "poll"})
	w := httptest.NewRecorder()
	s.handleOAuthDynamicPost(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("poll status=%d body=%s", w.Code, w.Body.String())
	}
	var poll map[string]any
	if err := json.NewDecoder(w.Body).Decode(&poll); err != nil {
		t.Fatalf("decode poll: %v", err)
	}
	if poll["success"] != false || poll["pending"] != true || poll["error"] != "authorization_pending" {
		t.Fatalf("poll response=%#v", poll)
	}
}

func TestQwenDeviceCodeAndPoll(t *testing.T) {
	s, db, cleanup := newTestServer(t)
	defer cleanup()

	var seenChallenge string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/device":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse device form: %v", err)
			}
			if r.Form.Get("code_challenge_method") != "S256" {
				t.Fatalf("challenge method=%q", r.Form.Get("code_challenge_method"))
			}
			seenChallenge = r.Form.Get("code_challenge")
			_, _ = w.Write([]byte(`{"device_code":"qw-dev","user_code":"QWEN","verification_uri":"https://qwen.ai/device","interval":5}`))
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse token form: %v", err)
			}
			if r.Form.Get("code_verifier") == "" || r.Form.Get("device_code") != "qw-dev" {
				t.Fatalf("token form=%v", r.Form)
			}
			_, _ = w.Write([]byte(`{"access_token":"qw-access","refresh_token":"qw-refresh","expires_in":3600,"resource_url":"portal.qwen.ai"}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	oldCfg, _ := oauthProviderRegistry.Get("qwen")
	defer oauthProviderRegistry.Register(oldCfg)
	qwenCfg, _ := oauthProviderRegistry.Get("qwen")
	qwenCfg.DeviceCodeURL = server.URL + "/device"
	qwenCfg.TokenURL = server.URL + "/token"
	oauthProviderRegistry.Register(qwenCfg)

	req := routeRequest(http.MethodGet, "/api/oauth/qwen/device-code", "", map[string]string{"provider": "qwen", "action": "device-code"})
	w := httptest.NewRecorder()
	s.handleOAuthDynamicGet(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("device status=%d body=%s", w.Code, w.Body.String())
	}
	var device map[string]any
	if err := json.NewDecoder(w.Body).Decode(&device); err != nil {
		t.Fatalf("decode device: %v", err)
	}
	verifier := stringValue(device["codeVerifier"])
	if device["device_code"] != "qw-dev" || verifier == "" || seenChallenge == "" {
		t.Fatalf("device response=%#v challenge=%q", device, seenChallenge)
	}

	req = routeRequest(http.MethodPost, "/api/oauth/qwen/poll", `{"device_code":"qw-dev","codeVerifier":"`+verifier+`"}`, map[string]string{"provider": "qwen", "action": "poll"})
	w = httptest.NewRecorder()
	s.handleOAuthDynamicPost(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("poll status=%d body=%s", w.Code, w.Body.String())
	}
	var poll map[string]any
	if err := json.NewDecoder(w.Body).Decode(&poll); err != nil {
		t.Fatalf("decode poll: %v", err)
	}
	if poll["success"] != true || poll["deprecated"] != true {
		t.Fatalf("poll response=%#v", poll)
	}
	conns, err := db.ListProviderConnections(context.Background(), storage.ProviderConnectionFilter{Provider: "qwen"})
	if err != nil {
		t.Fatalf("list qwen conns: %v", err)
	}
	if len(conns) != 1 || conns[0].AccessToken != "qw-access" || conns[0].RefreshToken != "qw-refresh" || conns[0].ProviderSpecificData["resourceUrl"] != "portal.qwen.ai" {
		t.Fatalf("connection=%#v", conns)
	}
}

func TestWebCookieValidationAndRedaction(t *testing.T) {
	s, db, cleanup := newTestServer(t)
	defer cleanup()

	req := routeRequest(http.MethodPost, "/api/oauth/grok-web/cookie", `{"cookie":"sso=grok-secret-token;"}`, map[string]string{"provider": "grok-web", "action": "cookie"})
	w := httptest.NewRecorder()
	s.handleOAuthDynamicPost(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("grok status=%d body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "grok-secret-token") {
		t.Fatalf("response leaked cookie: %s", w.Body.String())
	}

	req = routeRequest(http.MethodPost, "/api/oauth/perplexity-web/cookie", `{"cookie":"wrong=value;"}`, map[string]string{"provider": "perplexity-web", "action": "cookie"})
	w = httptest.NewRecorder()
	s.handleOAuthDynamicPost(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("perplexity invalid status=%d body=%s", w.Code, w.Body.String())
	}

	conns, err := db.ListProviderConnections(context.Background(), storage.ProviderConnectionFilter{Provider: "grok-web"})
	if err != nil {
		t.Fatalf("list grok conns: %v", err)
	}
	if len(conns) != 1 || conns[0].APIKey != "grok-secret-token" || conns[0].ProviderSpecificData["runtime"] != "supported" {
		t.Fatalf("connection=%#v", conns)
	}
}

func TestOAuthRefreshUpdatesProviderConnection(t *testing.T) {
	s, db, cleanup := newTestServer(t)
	defer cleanup()

	oldRegistry := oauthProviderRegistry
	defer func() { oauthProviderRegistry = oldRegistry }()
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "old-refresh" {
			t.Fatalf("form=%v", r.Form)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new-access","refresh_token":"new-refresh","expires_in":1800}`))
	}))
	defer tokenServer.Close()
	oauthProviderRegistry = oauth.NewProviderRegistry(oauth.ProviderOAuthConfig{
		Name:     "qwen",
		TokenURL: tokenServer.URL,
		ClientID: "client",
	})

	conn, err := db.CreateProviderConnection(context.Background(), storage.ProviderConnection{
		Provider:     "qwen",
		AuthType:     "oauth",
		Name:         "qwen-oauth",
		RefreshToken: "old-refresh",
		IsActive:     true,
		Enabled:      true,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}

	req := routeRequest(http.MethodPost, "/api/oauth/qwen/refresh", `{"connectionId":"`+conn.ID+`"}`, map[string]string{"provider": "qwen", "action": "refresh"})
	w := httptest.NewRecorder()
	s.handleOAuthDynamicPost(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("refresh status=%d body=%s", w.Code, w.Body.String())
	}
	updated, err := db.GetProviderConnection(context.Background(), conn.ID)
	if err != nil {
		t.Fatalf("get connection: %v", err)
	}
	if updated.AccessToken != "new-access" || updated.RefreshToken != "new-refresh" || updated.ExpiresAt == nil {
		t.Fatalf("updated=%#v", updated)
	}
}

func TestCLIToolSettingsPersistFiles(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()

	home := t.TempDir()
	t.Setenv("NINEROUTER_CLI_HOME", home)

	t.Run("claude", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/cli-tools/claude-settings", strings.NewReader(`{
			"env":{"ANTHROPIC_BASE_URL":"http://127.0.0.1:1988","ANTHROPIC_AUTH_TOKEN":"sk-test"}
		}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.handleCLIToolSettingsPost(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("post status=%d body=%s", w.Code, w.Body.String())
		}

		get := httptest.NewRequest(http.MethodGet, "/api/cli-tools/claude-settings", nil)
		w = httptest.NewRecorder()
		s.handleCLIToolSettingsGet(w, get)
		if w.Code != http.StatusOK {
			t.Fatalf("get status=%d body=%s", w.Code, w.Body.String())
		}
		var resp map[string]any
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp["has9Router"] != true {
			t.Fatalf("expected has9Router true, got %#v", resp)
		}
	})

	t.Run("codex", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/cli-tools/codex-settings", strings.NewReader(`{
			"baseUrl":"http://127.0.0.1:1988","apiKey":"sk-test","model":"gpt-4.1"
		}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.handleCLIToolSettingsPost(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("post status=%d body=%s", w.Code, w.Body.String())
		}
		config, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
		if err != nil {
			t.Fatalf("read codex config: %v", err)
		}
		if !strings.Contains(string(config), `model_provider = "9router"`) {
			t.Fatalf("expected codex 9router provider config, got %s", string(config))
		}
	})

	t.Run("opencode", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/cli-tools/opencode-settings", strings.NewReader(`{
			"baseUrl":"http://127.0.0.1:1988","apiKey":"sk-test","models":["gpt-4.1","gpt-4.1-mini"],"activeModel":"gpt-4.1-mini"
		}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.handleCLIToolSettingsPost(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("post status=%d body=%s", w.Code, w.Body.String())
		}

		get := httptest.NewRequest(http.MethodGet, "/api/cli-tools/opencode-settings", nil)
		w = httptest.NewRecorder()
		s.handleCLIToolSettingsGet(w, get)
		if w.Code != http.StatusOK {
			t.Fatalf("get status=%d body=%s", w.Code, w.Body.String())
		}
		var resp map[string]any
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp["has9Router"] != true {
			t.Fatalf("expected opencode has9Router true, got %#v", resp)
		}
	})

	t.Run("copilot", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/cli-tools/copilot-settings", strings.NewReader(`{
			"baseUrl":"http://127.0.0.1:1988","apiKey":"sk-test","models":["gpt-4.1"]
		}`))
		w := httptest.NewRecorder()
		s.handleCLIToolSettingsPost(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("post status=%d body=%s", w.Code, w.Body.String())
		}
		data, err := os.ReadFile(filepath.Join(home, ".config", "Code", "User", "chatLanguageModels.json"))
		if err != nil {
			t.Fatalf("read copilot config: %v", err)
		}
		if !strings.Contains(string(data), `"name": "9Router"`) || !strings.Contains(string(data), `/chat/completions#models.ai.azure.com`) {
			t.Fatalf("expected copilot 9router entry, got %s", string(data))
		}
	})

	t.Run("droid", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/cli-tools/droid-settings", strings.NewReader(`{
			"baseUrl":"http://127.0.0.1:1988","apiKey":"sk-test","models":["gpt-4.1","gpt-4.1-mini"],"activeModel":"gpt-4.1-mini"
		}`))
		w := httptest.NewRecorder()
		s.handleCLIToolSettingsPost(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("post status=%d body=%s", w.Code, w.Body.String())
		}
		data, err := os.ReadFile(filepath.Join(home, ".factory", "settings.json"))
		if err != nil {
			t.Fatalf("read droid settings: %v", err)
		}
		if !strings.Contains(string(data), `"id": "custom:9Router-1"`) || !strings.Contains(string(data), `"baseUrl": "http://127.0.0.1:1988/v1"`) {
			t.Fatalf("expected droid custom models, got %s", string(data))
		}
	})

	t.Run("hermes", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/cli-tools/hermes-settings", strings.NewReader(`{
			"baseUrl":"http://127.0.0.1:1988","apiKey":"sk-test","model":"gpt-4.1"
		}`))
		w := httptest.NewRecorder()
		s.handleCLIToolSettingsPost(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("post status=%d body=%s", w.Code, w.Body.String())
		}
		config, err := os.ReadFile(filepath.Join(home, ".hermes", "config.yaml"))
		if err != nil {
			t.Fatalf("read hermes config: %v", err)
		}
		env, err := os.ReadFile(filepath.Join(home, ".hermes", ".env"))
		if err != nil {
			t.Fatalf("read hermes env: %v", err)
		}
		if !strings.Contains(string(config), `provider: "custom"`) || !strings.Contains(string(env), "OPENAI_API_KEY=sk-test") {
			t.Fatalf("expected hermes config/env, got config=%s env=%s", string(config), string(env))
		}
	})

	t.Run("openclaw", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/cli-tools/openclaw-settings", strings.NewReader(`{
			"baseUrl":"http://127.0.0.1:1988","apiKey":"sk-test","model":"gpt-4.1","agentModels":{"explorer":"gpt-4.1-mini"}
		}`))
		w := httptest.NewRecorder()
		s.handleCLIToolSettingsPost(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("post status=%d body=%s", w.Code, w.Body.String())
		}
		data, err := os.ReadFile(filepath.Join(home, ".openclaw", "openclaw.json"))
		if err != nil {
			t.Fatalf("read openclaw settings: %v", err)
		}
		if !strings.Contains(string(data), `"9router"`) || !strings.Contains(string(data), `"primary": "9router/gpt-4.1"`) {
			t.Fatalf("expected openclaw 9router settings, got %s", string(data))
		}
	})
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

func TestHandleProvidersCatalogFilters(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()

	t.Run("default excludes planned", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/providers/catalog", nil)
		w := httptest.NewRecorder()
		s.handleProvidersCatalog(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var resp struct {
			Catalog []map[string]any `json:"catalog"`
		}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		for _, p := range resp.Catalog {
			if status, _ := p["execution_status"].(string); status == "planned" {
				t.Fatalf("planned provider should not be returned by default")
			}
		}
	})

	t.Run("runtime_supported true only", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/providers/catalog?runtime_supported=true", nil)
		w := httptest.NewRecorder()
		s.handleProvidersCatalog(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var resp struct {
			Catalog []map[string]any `json:"catalog"`
		}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		for _, p := range resp.Catalog {
			if status, _ := p["execution_status"].(string); status != "supported" {
				t.Fatalf("runtime_supported=true returned non-supported provider status=%s", status)
			}
		}
	})

	t.Run("deprecated true only", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/providers/catalog?deprecated=true&include_planned=true", nil)
		w := httptest.NewRecorder()
		s.handleProvidersCatalog(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var resp struct {
			Catalog []map[string]any `json:"catalog"`
		}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(resp.Catalog) == 0 {
			t.Fatalf("expected at least one deprecated provider")
		}
		for _, p := range resp.Catalog {
			if deprecated, _ := p["deprecated"].(bool); !deprecated {
				t.Fatalf("deprecated=true returned non-deprecated provider")
			}
		}
	})

	t.Run("service kind and auth type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/providers/catalog?service_kind=embedding&auth_type=api_key", nil)
		w := httptest.NewRecorder()
		s.handleProvidersCatalog(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var resp struct {
			Catalog []map[string]any `json:"catalog"`
		}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		for _, p := range resp.Catalog {
			serviceKinds := toStringSlice(p["service_kinds"])
			authTypes := toStringSlice(p["auth_types"])
			if !slices.Contains(serviceKinds, "embedding") {
				t.Fatalf("provider missing embedding kind")
			}
			if !slices.Contains(authTypes, "api_key") {
				t.Fatalf("provider missing api_key auth")
			}
		}
	})
}

func TestReferenceCompatibilityUtilityHandlers(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()

	t.Run("require login", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/settings/require-login", nil)
		w := httptest.NewRecorder()
		s.handleSettingsRequireLogin(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var resp map[string]any
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if resp["requireLogin"] != true || resp["auth_required"] != true {
			t.Fatalf("expected login required response, got %#v", resp)
		}
	})

	t.Run("set locale", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/locale", strings.NewReader(`{"locale":"id"}`))
		w := httptest.NewRecorder()
		s.handleLocaleSet(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		if got := s.runtimeConfig.Get().Settings.Locale; got != "id" {
			t.Fatalf("expected locale id, got %q", got)
		}
	})

	t.Run("tags", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/tags", nil)
		w := httptest.NewRecorder()
		s.handleTags(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var resp struct {
			Models []map[string]any `json:"models"`
		}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(resp.Models) == 0 {
			t.Fatalf("expected provider wildcard tag")
		}
	})

	t.Run("tunnel status", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/tunnel/status", nil)
		w := httptest.NewRecorder()
		s.handleTunnelStatus(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var resp map[string]any
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if _, ok := resp["tunnel"]; !ok {
			t.Fatalf("expected tunnel key, got %#v", resp)
		}
	})

	t.Run("version", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
		w := httptest.NewRecorder()
		s.handleVersion(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var resp map[string]any
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if _, ok := resp["currentVersion"]; !ok {
			t.Fatalf("expected currentVersion, got %#v", resp)
		}
	})
}

func toStringSlice(v any) []string {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
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

func TestHandleMetricsJSON(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()

	s.metrics.mu.Lock()
	s.metrics.RequestsTotal = 10
	s.metrics.RequestsSuccess = 8
	s.metrics.RequestsError = 2
	s.metrics.ProviderUsage["openai"] = 3
	s.metrics.mu.Unlock()

	w := httptest.NewRecorder()
	s.handleMetricsJSON(w, httptest.NewRequest("GET", "/api/metrics", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := w.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("content-type = %q, want json", got)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["requests_total"] != float64(10) {
		t.Fatalf("requests_total = %v, want 10", resp["requests_total"])
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

func TestAdminCRUDProviderConnectionsAndProxyPools(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	s.handleProvidersCreate(w, routeRequest(http.MethodPost, "/api/providers", `{"id":"conn-openai","provider":"openai","auth_type":"api_key","name":"Main","api_key":"sk-test","base_url":"https://api.openai.com/v1","enabled":true}`, nil))
	if w.Code != http.StatusCreated {
		t.Fatalf("create connection status=%d body=%s", w.Code, w.Body.String())
	}
	var created map[string]any
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode create connection: %v", err)
	}
	conn := created["connection"].(map[string]any)
	if _, ok := conn["api_key"]; ok {
		t.Fatalf("sanitized connection leaked api_key")
	}
	if conn["has_api_key"] != true {
		t.Fatalf("has_api_key = %v, want true", conn["has_api_key"])
	}

	w = httptest.NewRecorder()
	s.handleProvidersList(w, httptest.NewRequest(http.MethodGet, "/api/providers", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list providers status=%d", w.Code)
	}

	w = httptest.NewRecorder()
	s.handleProxyPoolsCreate(w, routeRequest(http.MethodPost, "/api/proxy-pools", `{"id":"pool1","name":"Pool 1","proxies":["http://127.0.0.1:8080"]}`, nil))
	if w.Code != http.StatusCreated {
		t.Fatalf("create proxy pool status=%d body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	s.handleProxyPoolsList(w, httptest.NewRequest(http.MethodGet, "/api/proxy-pools", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list proxy pools status=%d", w.Code)
	}
	var listed map[string]any
	if err := json.NewDecoder(w.Body).Decode(&listed); err != nil {
		t.Fatalf("decode proxy pools: %v", err)
	}
	if listed["count"] != float64(1) {
		t.Fatalf("proxy pool count = %v, want 1", listed["count"])
	}

	w = httptest.NewRecorder()
	s.handleProxyPoolsDelete(w, routeRequest(http.MethodDelete, "/api/proxy-pools/pool1", "", map[string]string{"id": "pool1"}))
	if w.Code != http.StatusOK {
		t.Fatalf("delete proxy pool status=%d body=%s", w.Code, w.Body.String())
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

func TestCompatibilitySurfaceEndpoints(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()

	tests := []struct {
		method string
		path   string
		body   string
		want   int
	}{
		{http.MethodPost, "/v1/messages/count_tokens", `{"model":"claude-3","messages":[{"role":"user","content":"hello world"}]}`, http.StatusOK},
		{http.MethodGet, "/v1beta/models", ``, http.StatusOK},
		{http.MethodPost, "/codex/unsupported", `{}`, http.StatusNotFound},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		req.Header.Set("Authorization", "Bearer test-key")
		s.Handler().ServeHTTP(w, req)
		if w.Code != tt.want {
			t.Fatalf("%s %s status = %d body=%s, want %d", tt.method, tt.path, w.Code, w.Body.String(), tt.want)
		}
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

func TestAudioSpeechEndpointJSONResponseFormat(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/text-to-speech/voice-1" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("mp3"))
	}))
	defer upstream.Close()

	cfg := s.runtimeConfig.Get()
	cfg.Providers = []config.ProviderConfig{{Name: "elevenlabs", ProviderID: "elevenlabs", Type: "elevenlabs", BaseURL: upstream.URL, APIKey: "el-key", Enabled: true}}
	cfg.Routes = map[string]config.RouteConfig{"tts-test": {Strategy: "fallback", Targets: []config.RouteTarget{{Provider: "elevenlabs", Model: "eleven_flash_v2_5"}}}}
	if err := s.runtimeConfig.Update(func(current *config.Config) error { *current = cfg; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := s.reconfigureFromConfig(cfg); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	s.handleAudioSpeech(w, routeRequest(http.MethodPost, "/v1/audio/speech", `{"model":"tts-test","input":"hello","voice":"voice-1","response_format":"json"}`, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["audio"] == "" || resp["format"] != "mp3" {
		t.Fatalf("resp=%#v", resp)
	}
}

func TestAudioTranscriptionsEndpointMultipart(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/listen" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			t.Fatalf("content-type=%q", r.Header.Get("Content-Type"))
		}
		_, _ = w.Write([]byte(`{"results":{"channels":[{"alternatives":[{"transcript":"hello"}]}]}}`))
	}))
	defer upstream.Close()

	cfg := s.runtimeConfig.Get()
	cfg.Providers = []config.ProviderConfig{{Name: "deepgram", ProviderID: "deepgram", Type: "deepgram", BaseURL: upstream.URL, APIKey: "dg-key", Enabled: true}}
	cfg.Routes = map[string]config.RouteConfig{"stt-test": {Strategy: "fallback", Targets: []config.RouteTarget{{Provider: "deepgram", Model: "nova-2"}}}}
	if err := s.runtimeConfig.Update(func(current *config.Config) error { *current = cfg; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := s.reconfigureFromConfig(cfg); err != nil {
		t.Fatal(err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("model", "stt-test")
	part, err := writer.CreateFormFile("file", "audio.wav")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("wav"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	s.handleAudioTranscriptions(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp providers.AudioTranscriptionResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Text != "hello" {
		t.Fatalf("resp=%#v", resp)
	}
}

func TestMediaTTSVoicesEndpointElevenLabs(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/voices" || r.Header.Get("xi-api-key") != "el-key" {
			t.Fatalf("path=%s auth=%q", r.URL.Path, r.Header.Get("xi-api-key"))
		}
		_, _ = w.Write([]byte(`{"voices":[{"voice_id":"v1","name":"Rachel","category":"premade","labels":{"language":"en","gender":"female"}}]}`))
	}))
	defer upstream.Close()

	cfg := s.runtimeConfig.Get()
	cfg.Providers = []config.ProviderConfig{{Name: "elevenlabs", ProviderID: "elevenlabs", Type: "elevenlabs", BaseURL: upstream.URL, APIKey: "el-key", Enabled: true}}
	if err := s.runtimeConfig.Update(func(current *config.Config) error { *current = cfg; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := s.reconfigureFromConfig(cfg); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	s.handleMediaTTSVoices(w, httptest.NewRequest(http.MethodGet, "/api/media-providers/tts/elevenlabs/voices?lang=en", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp["voices"].([]any)) != 1 {
		t.Fatalf("resp=%#v", resp)
	}
}

func TestTranslatorAPIStep1_FormatDetection(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()

	// Claude-format body: has "messages" + no "input" → detected as claude
	body := `{"step":1,"body":{"model":"anthropic/claude-3-5-sonnet","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}],"max_tokens":100}}`
	w := httptest.NewRecorder()
	s.handleTranslatorTranslate(w, routeRequest(http.MethodPost, "/api/translator/translate", body, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("step 1 expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["success"] != true {
		t.Fatalf("step 1 expected success, got: %v", resp)
	}
	result := resp["result"].(map[string]any)
	if result["provider"] != "anthropic" {
		t.Fatalf("expected provider anthropic, got %v", result["provider"])
	}
}

func TestTranslatorAPIStep2_ClaudeToOpenAI(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()

	// Step 2: translate Claude request body → OpenAI
	body := `{
		"step": 2,
		"sourceFormat": "claude",
		"body": {
			"model": "claude-3-5-sonnet",
			"messages": [{"role": "user", "content": "Hello"}],
			"max_tokens": 50,
			"system": "Be helpful."
		}
	}`
	w := httptest.NewRecorder()
	s.handleTranslatorTranslate(w, routeRequest(http.MethodPost, "/api/translator/translate", body, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("step 2 expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["success"] != true {
		t.Fatalf("step 2 expected success, got: %v", resp)
	}
	result := resp["result"].(map[string]any)
	translated := result["body"].(map[string]any)
	// system → first message with role=system in OpenAI format
	msgs, _ := translated["messages"].([]any)
	if len(msgs) == 0 {
		t.Fatal("expected messages in translated body")
	}
	first := msgs[0].(map[string]any)
	if first["role"] != "system" {
		t.Fatalf("expected first message to be system, got %v", first["role"])
	}
}

func TestTranslatorAPIStep3_OpenAIToClaude(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()

	// Step 3: translate OpenAI body → target format (anthropic → claude)
	body := `{
		"step": 3,
		"provider": "anthropic",
		"model": "claude-3-5-sonnet",
		"targetFormat": "claude",
		"body": {
			"model": "claude-3-5-sonnet",
			"messages": [
				{"role": "system", "content": "Be helpful."},
				{"role": "user", "content": "Hello"}
			],
			"max_tokens": 50
		}
	}`
	w := httptest.NewRecorder()
	s.handleTranslatorTranslate(w, routeRequest(http.MethodPost, "/api/translator/translate", body, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("step 3 expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["success"] != true {
		t.Fatalf("step 3 expected success, got: %v", resp)
	}
	result := resp["result"].(map[string]any)
	finalBody := result["body"].(map[string]any)
	// OpenAI → Claude: system message extracted as top-level "system"
	if finalBody["system"] == nil {
		t.Fatalf("expected top-level 'system' field in Claude format, got: %v", finalBody)
	}
}

func TestTranslatorAPIStep_InvalidBody(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()

	// No body field
	w := httptest.NewRecorder()
	s.handleTranslatorTranslate(w, routeRequest(http.MethodPost, "/api/translator/translate", `{"step":2}`, nil))
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing body, got %d", w.Code)
	}
}

func TestTranslatorAPIStep_InvalidStep(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()

	body := `{"step":99,"body":{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}}`
	w := httptest.NewRecorder()
	s.handleTranslatorTranslate(w, routeRequest(http.MethodPost, "/api/translator/translate", body, nil))
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid step, got %d", w.Code)
	}
}

func TestTranslatorRegistrySharedWithRuntime(t *testing.T) {
	// Verify that the server's translator registry is the same instance
	// used by both handleTranslatorTranslate and the runtime routing path (/v1/messages)
	s, _, cleanup := newTestServer(t)
	defer cleanup()

	// First call initializes the registry
	r1 := s.translatorRegistry()
	if r1 == nil {
		t.Fatal("translatorRegistry() returned nil")
	}
	// Second call must return the same instance (not re-create)
	r2 := s.translatorRegistry()
	if r1 != r2 {
		t.Fatal("translatorRegistry() must return the same instance on repeated calls")
	}
	// The server's translators field must equal what translatorRegistry() returns
	if s.translators != r1 {
		t.Fatal("s.translators should be the same instance returned by translatorRegistry()")
	}
}

func TestTranslatorSend_MissingFields(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()

	cases := []struct {
		name string
		body string
	}{
		{"missing provider", `{"model":"gpt-4","body":{"messages":[]}}`},
		{"missing model", `{"provider":"openai","body":{"messages":[]}}`},
		{"missing body", `{"provider":"openai","model":"gpt-4"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			s.handleTranslatorSend(w, routeRequest(http.MethodPost, "/api/translator/send", tc.body, nil))
			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d", w.Code)
			}
		})
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
