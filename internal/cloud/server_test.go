package cloud

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCloudHealthAndTags(t *testing.T) {
	handler := NewServer().Handler()

	for _, path := range []string{"/health", "/api/tags"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, w.Code, w.Body.String())
		}
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
			t.Fatalf("cors header = %q", got)
		}
	}
}

func TestCloudSyncLifecycle(t *testing.T) {
	handler := NewServer().Handler()

	missing := httptest.NewRequest(http.MethodGet, "/sync/machine-1", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, missing)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing get status=%d body=%s", w.Code, w.Body.String())
	}

	post := httptest.NewRequest(http.MethodPost, "/sync/machine-1", strings.NewReader(`{
		"providers":[{"id":"conn-1","provider":"openai","name":"OpenAI","updatedAt":"2026-04-27T00:00:00Z"}],
		"modelAliases":{"fast":"openai/gpt-4.1-mini"},
		"apiKeys":[{"key":"sk-machine-1-key-00000000"}]
	}`))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, post)
	if w.Code != http.StatusOK {
		t.Fatalf("post status=%d body=%s", w.Code, w.Body.String())
	}

	get := httptest.NewRequest(http.MethodGet, "/sync/machine-1", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, get)
	if w.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode sync data: %v", err)
	}
	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got %#v", body)
	}
	if _, ok := data["providers"]; !ok {
		t.Fatalf("expected synced providers, got %#v", body)
	}

	del := httptest.NewRequest(http.MethodDelete, "/sync/machine-1", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, del)
	if w.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestCloudSyncMergeKeepsNewerWorkerProvider(t *testing.T) {
	handler := NewServer().Handler()

	first := httptest.NewRequest(http.MethodPost, "/sync/machine-merge", strings.NewReader(`{
		"providers":[{"id":"conn-1","provider":"openai","name":"newer-worker","updatedAt":"2026-04-27T02:00:00Z"}],
		"apiKeys":["sk-machine-merge-key-00000000"]
	}`))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, first)
	if w.Code != http.StatusOK {
		t.Fatalf("first status=%d body=%s", w.Code, w.Body.String())
	}

	second := httptest.NewRequest(http.MethodPost, "/sync/machine-merge", strings.NewReader(`{
		"providers":[{"id":"conn-1","provider":"openai","name":"older-web","updatedAt":"2026-04-27T01:00:00Z"}]
	}`))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, second)
	if w.Code != http.StatusOK {
		t.Fatalf("second status=%d body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	data := body["data"].(map[string]any)
	providers := data["providers"].(map[string]any)
	conn := providers["conn-1"].(map[string]any)
	if conn["name"] != "newer-worker" {
		t.Fatalf("expected newer worker provider to win, got %#v", conn)
	}
}

func TestCloudVerifyAndCacheClear(t *testing.T) {
	handler := NewServer().Handler()
	apiKey := "sk-machinev-key-00000000"

	post := httptest.NewRequest(http.MethodPost, "/sync/machinev", strings.NewReader(`{
		"providers":[{"id":"conn-1","provider":"openai"}],
		"apiKeys":[{"key":"sk-machinev-key-00000000"}]
	}`))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, post)
	if w.Code != http.StatusOK {
		t.Fatalf("sync status=%d body=%s", w.Code, w.Body.String())
	}

	verify := httptest.NewRequest(http.MethodGet, "/v1/verify", nil)
	verify.Header.Set("Authorization", "Bearer "+apiKey)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, verify)
	if w.Code != http.StatusOK {
		t.Fatalf("verify status=%d body=%s", w.Code, w.Body.String())
	}

	oldVerify := httptest.NewRequest(http.MethodGet, "/machinev/v1/verify", nil)
	oldVerify.Header.Set("Authorization", "Bearer "+apiKey)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, oldVerify)
	if w.Code != http.StatusOK {
		t.Fatalf("old verify status=%d body=%s", w.Code, w.Body.String())
	}

	clear := httptest.NewRequest(http.MethodPost, "/cache/clear", strings.NewReader(`{"machineId":"machinev"}`))
	clear.Header.Set("Authorization", "Bearer "+apiKey)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, clear)
	if w.Code != http.StatusOK {
		t.Fatalf("cache clear status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestCloudInferenceForwarding(t *testing.T) {
	var upstreamPath, upstreamAuth string
	var upstreamBody map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamPath = r.URL.Path
		upstreamAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&upstreamBody); err != nil {
			t.Fatalf("decode upstream body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-test","object":"chat.completion"}`)
	}))
	defer upstream.Close()

	handler := NewServer().Handler()
	apiKey := "sk-machinef-key-00000000"
	sync := httptest.NewRequest(http.MethodPost, "/sync/machinef", strings.NewReader(`{
		"providers":[{"id":"conn-1","provider":"openai","apiKey":"upstream-key","baseUrl":"`+upstream.URL+`/v1","isActive":true,"priority":1}],
		"apiKeys":[{"key":"`+apiKey+`"}],
		"modelAliases":{"fast":"openai/gpt-test"}
	}`))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, sync)
	if w.Code != http.StatusOK {
		t.Fatalf("sync status=%d body=%s", w.Code, w.Body.String())
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"fast","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("forward status=%d body=%s", w.Code, w.Body.String())
	}
	if upstreamPath != "/v1/chat/completions" {
		t.Fatalf("upstream path=%q", upstreamPath)
	}
	if upstreamAuth != "Bearer upstream-key" {
		t.Fatalf("upstream auth=%q", upstreamAuth)
	}
	if upstreamBody["model"] != "gpt-test" {
		t.Fatalf("upstream model=%#v body=%#v", upstreamBody["model"], upstreamBody)
	}
}

func TestCloudInferenceForwardingMachinePath(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("upstream path=%q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer upstream.Close()

	handler := NewServer().Handler()
	apiKey := "sk-machineold-key-00000000"
	sync := httptest.NewRequest(http.MethodPost, "/sync/machineold", strings.NewReader(`{
		"providers":[{"id":"conn-1","provider":"openai","apiKey":"upstream-key","baseUrl":"`+upstream.URL+`","isActive":true}],
		"apiKeys":["`+apiKey+`"]
	}`))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, sync)
	if w.Code != http.StatusOK {
		t.Fatalf("sync status=%d body=%s", w.Code, w.Body.String())
	}

	req := httptest.NewRequest(http.MethodPost, "/machineold/v1/chat/completions", strings.NewReader(`{"model":"openai/gpt-test"}`))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("forward status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestCloudInferenceValidation(t *testing.T) {
	handler := NewServer().Handler()

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"openai/gpt-test"}`))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("missing auth status=%d body=%s", w.Code, w.Body.String())
	}

	apiKey := "sk-machineempty-key-00000000"
	sync := httptest.NewRequest(http.MethodPost, "/sync/machineempty", strings.NewReader(`{
		"providers":[{"id":"conn-1","provider":"openai","isActive":true}],
		"apiKeys":["`+apiKey+`"]
	}`))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, sync)
	if w.Code != http.StatusOK {
		t.Fatalf("sync status=%d body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"openai/gpt-test"}`))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing credentials status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestCloudCountTokensEstimatesLocally(t *testing.T) {
	handler := NewServer().Handler()

	req := httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", strings.NewReader(`{
		"messages":[
			{"role":"user","content":"hello"},
			{"role":"assistant","content":[{"type":"text","text":"world!"},{"type":"image","source":{}}]}
		]
	}`))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("count_tokens status=%d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["input_tokens"] != float64(3) {
		t.Fatalf("input_tokens=%#v", resp["input_tokens"])
	}

	oldReq := httptest.NewRequest(http.MethodPost, "/machineold/v1/messages/count_tokens", strings.NewReader(`{"messages":[{"content":"1234"}]}`))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, oldReq)
	if w.Code != http.StatusOK {
		t.Fatalf("old count_tokens status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestCloudTestClaudeForwardsAnthropicRequest(t *testing.T) {
	var gotPath, gotKey, gotVersion string
	var gotBody map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode upstream body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg-test","type":"message"}`)
	}))
	defer upstream.Close()

	handler := NewServer().Handler()
	req := httptest.NewRequest(http.MethodPost, "/testClaude", strings.NewReader(`{
		"apiKey":"anthropic-key",
		"baseUrl":"`+upstream.URL+`",
		"model":"claude-test",
		"messages":[{"role":"user","content":"hello"}]
	}`))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("testClaude status=%d body=%s", w.Code, w.Body.String())
	}
	if gotPath != "/v1/messages" {
		t.Fatalf("upstream path=%q", gotPath)
	}
	if gotKey != "anthropic-key" || gotVersion == "" {
		t.Fatalf("headers key=%q version=%q", gotKey, gotVersion)
	}
	if gotBody["model"] != "claude-test" {
		t.Fatalf("model=%#v body=%#v", gotBody["model"], gotBody)
	}
}

func TestCloudOllamaChatForwardsToChatCompletionsAndTransforms(t *testing.T) {
	var upstreamPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"chatcmpl-test",
			"object":"chat.completion",
			"model":"gpt-test",
			"choices":[{"index":0,"message":{"role":"assistant","content":"pong"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}
		}`)
	}))
	defer upstream.Close()

	handler := NewServer().Handler()
	apiKey := "sk-machineollama-key-00000000"
	sync := httptest.NewRequest(http.MethodPost, "/sync/machineollama", strings.NewReader(`{
		"providers":[{"id":"conn-1","provider":"openai","apiKey":"upstream-key","baseUrl":"`+upstream.URL+`/v1","isActive":true}],
		"apiKeys":["`+apiKey+`"],
		"modelAliases":{"llama3.2":"openai/gpt-test"}
	}`))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, sync)
	if w.Code != http.StatusOK {
		t.Fatalf("sync status=%d body=%s", w.Code, w.Body.String())
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/api/chat", strings.NewReader(`{"model":"llama3.2","messages":[{"role":"user","content":"ping"}],"stream":true}`))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ollama status=%d body=%s", w.Code, w.Body.String())
	}
	if upstreamPath != "/v1/chat/completions" {
		t.Fatalf("upstream path=%q", upstreamPath)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	message := resp["message"].(map[string]any)
	if message["content"] != "pong" || resp["done"] != true {
		t.Fatalf("unexpected ollama response %#v", resp)
	}
}

func TestCloudSyncPersistsWithSQLiteStorage(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cloud.db")
	server, err := NewServerWithSQLite(dbPath)
	if err != nil {
		t.Fatalf("new sqlite server: %v", err)
	}
	handler := server.Handler()
	apiKey := "sk-machinepersist-key-00000000"
	sync := httptest.NewRequest(http.MethodPost, "/sync/machinepersist", strings.NewReader(`{
		"providers":[{"id":"conn-1","provider":"openai","apiKey":"upstream-key","isActive":true}],
		"apiKeys":["`+apiKey+`"]
	}`))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, sync)
	if w.Code != http.StatusOK {
		t.Fatalf("sync status=%d body=%s", w.Code, w.Body.String())
	}

	restarted, err := NewServerWithSQLite(dbPath)
	if err != nil {
		t.Fatalf("restart sqlite server: %v", err)
	}
	get := httptest.NewRequest(http.MethodGet, "/sync/machinepersist", nil)
	w = httptest.NewRecorder()
	restarted.Handler().ServeHTTP(w, get)
	if w.Code != http.StatusOK {
		t.Fatalf("persisted get status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestCloudInferenceFallsBackAndMarksCooldown(t *testing.T) {
	var firstCalls, secondCalls int
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstCalls++
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":"rate limited"}`)
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"ok"}`)
	}))
	defer second.Close()

	handler := NewServer().Handler()
	apiKey := "sk-machinefb-key-00000000"
	sync := httptest.NewRequest(http.MethodPost, "/sync/machinefb", strings.NewReader(`{
		"providers":[
			{"id":"conn-1","provider":"openai","apiKey":"key-1","baseUrl":"`+first.URL+`/v1","isActive":true,"priority":1},
			{"id":"conn-2","provider":"openai","apiKey":"key-2","baseUrl":"`+second.URL+`/v1","isActive":true,"priority":2}
		],
		"apiKeys":["`+apiKey+`"]
	}`))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, sync)
	if w.Code != http.StatusOK {
		t.Fatalf("sync status=%d body=%s", w.Code, w.Body.String())
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"openai/gpt-test"}`))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("fallback status=%d body=%s", w.Code, w.Body.String())
	}
	if firstCalls != 1 || secondCalls != 1 {
		t.Fatalf("calls first=%d second=%d", firstCalls, secondCalls)
	}
	if w.Header().Get("Retry-After") != "" {
		t.Fatalf("retry-after leaked from failed provider: %q", w.Header().Get("Retry-After"))
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"openai/gpt-test"}`))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("cooldown skip status=%d body=%s", w.Code, w.Body.String())
	}
	if firstCalls != 1 || secondCalls != 2 {
		t.Fatalf("cooldown calls first=%d second=%d", firstCalls, secondCalls)
	}
}

func TestCloudInferenceReturnsLastRetryableFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "12")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":"still limited"}`)
	}))
	defer upstream.Close()

	handler := NewServer().Handler()
	apiKey := "sk-machinelimit-key-00000000"
	sync := httptest.NewRequest(http.MethodPost, "/sync/machinelimit", strings.NewReader(`{
		"providers":[{"id":"conn-1","provider":"openai","apiKey":"key-1","baseUrl":"`+upstream.URL+`/v1","isActive":true}],
		"apiKeys":["`+apiKey+`"]
	}`))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, sync)
	if w.Code != http.StatusOK {
		t.Fatalf("sync status=%d body=%s", w.Code, w.Body.String())
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"openai/gpt-test"}`))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if w.Header().Get("Retry-After") != "12" {
		t.Fatalf("retry-after=%q", w.Header().Get("Retry-After"))
	}
}

func TestCloudInferenceAllRateLimitedReturnsRetryAfter(t *testing.T) {
	handler := NewServer().Handler()
	apiKey := "sk-machinealllimited-key-00000000"
	sync := httptest.NewRequest(http.MethodPost, "/sync/machinealllimited", strings.NewReader(`{
		"providers":[{"id":"conn-1","provider":"openai","apiKey":"key-1","baseUrl":"http://127.0.0.1:9","isActive":true,"status":"unavailable","rateLimitedUntil":"`+time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano)+`","lastError":"rate limited","errorCode":"429"}],
		"apiKeys":["`+apiKey+`"]
	}`))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, sync)
	if w.Code != http.StatusOK {
		t.Fatalf("sync status=%d body=%s", w.Code, w.Body.String())
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"openai/gpt-test"}`))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatalf("missing Retry-After")
	}
}

func TestCloudInferenceRefreshesExpiringToken(t *testing.T) {
	var tokenRefreshBody string
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		tokenRefreshBody = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"fresh-access","refresh_token":"fresh-refresh","expires_in":3600}`)
	}))
	defer tokenServer.Close()

	var upstreamAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamAuth = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, `{"id":"ok"}`)
	}))
	defer upstream.Close()

	handler := NewServer().Handler()
	apiKey := "sk-machinerefresh-key-00000000"
	expiresAt := time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano)
	sync := httptest.NewRequest(http.MethodPost, "/sync/machinerefresh", strings.NewReader(`{
		"providers":[{"id":"conn-1","provider":"openai","accessToken":"stale-access","refreshToken":"old-refresh","expiresAt":"`+expiresAt+`","baseUrl":"`+upstream.URL+`/v1","isActive":true,"providerSpecificData":{"token_url":"`+tokenServer.URL+`","client_id":"cid"}}],
		"apiKeys":["`+apiKey+`"]
	}`))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, sync)
	if w.Code != http.StatusOK {
		t.Fatalf("sync status=%d body=%s", w.Code, w.Body.String())
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"openai/gpt-test"}`))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(tokenRefreshBody, "refresh_token=old-refresh") {
		t.Fatalf("refresh body=%q", tokenRefreshBody)
	}
	if upstreamAuth != "Bearer fresh-access" {
		t.Fatalf("upstream auth=%q", upstreamAuth)
	}
}
