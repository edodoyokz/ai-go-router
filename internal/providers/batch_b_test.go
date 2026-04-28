package providers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/edodoyokz/ai-go-router/internal/config"
	"github.com/edodoyokz/ai-go-router/internal/translator"
)

// --- iFlow ---

func TestIFlowAdapter_SignatureHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("session-id") == "" {
			t.Fatalf("missing session-id header")
		}
		if r.Header.Get("x-iflow-timestamp") == "" {
			t.Fatalf("missing x-iflow-timestamp header")
		}
		if r.Header.Get("x-iflow-signature") == "" {
			t.Fatalf("missing x-iflow-signature header")
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Fatalf("unexpected authorization: %s", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"iflow-ok","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	cfg := config.ProviderConfig{Name: "iflow", Type: "iflow", BaseURL: server.URL, APIKey: "test-key", Enabled: true}
	a := NewIFlowAdapter(cfg, config.ErrorConfig{}, "")
	resp, err := a.ChatCompletion(context.Background(), ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "hi"}}}, "iflow-model")
	if err != nil {
		t.Fatalf("ChatCompletion error: %v", err)
	}
	if resp.ID != "iflow-ok" {
		t.Fatalf("unexpected id: %s", resp.ID)
	}
}

func TestIFlowAdapter_SignatureComputation(t *testing.T) {
	userAgent := "iFlow-Test"
	sessionID := "session-test-123"
	ts := int64(1234567890)
	apiKey := "my-secret-key"

	sig := createIFlowSignature(userAgent, sessionID, ts, apiKey)
	if sig == "" {
		t.Fatalf("signature should not be empty")
	}

	// Verify HMAC-SHA256 computation
	payload := fmt.Sprintf("%s:%s:%d", userAgent, sessionID, ts)
	mac := hmac.New(sha256.New, []byte(apiKey))
	mac.Write([]byte(payload))
	expectedSig := fmt.Sprintf("%x", mac.Sum(nil))
	if sig != expectedSig {
		t.Fatalf("signature mismatch: got %s, want %s", sig, expectedSig)
	}
}

func TestIFlowAdapter_StreamOptionsInjection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Fatalf("expected Accept: text/event-stream, got %s", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"s1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hi\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	cfg := config.ProviderConfig{Name: "iflow", Type: "iflow", BaseURL: server.URL, APIKey: "k", Enabled: true}
	a := NewIFlowAdapter(cfg, config.ErrorConfig{}, "")
	chunks, err := a.StreamChatCompletion(context.Background(), ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "hi"}}}, "m")
	if err != nil {
		t.Fatalf("StreamChatCompletion error: %v", err)
	}
	var count int
	for range chunks {
		count++
	}
	if count != 1 {
		t.Fatalf("expected 1 chunk, got %d", count)
	}
}

// --- Vertex ---

func TestVertexAdapter_URLBuilding(t *testing.T) {
	tr := translator.NewRegistry()

	// Vertex with project ID and SA credentials (Gemini format)
	cfg := config.ProviderConfig{Name: "vertex", Type: "vertex", ProviderID: "vertex", BaseURL: "", APIKey: "raw-key", GCPProjectID: "my-project", Enabled: true}
	a := NewVertexAdapter(cfg, config.ErrorConfig{}, tr, "")
	creds := &vertexCredentials{isSA: true, accessToken: "test-token", projectID: "my-project", location: "us-central1"}
	url := a.buildURL("gemini-1.5-pro", false, creds)
	want := "https://aiplatform.googleapis.com/v1/projects/my-project/locations/us-central1/publishers/google/models/gemini-1.5-pro:generateContent"
	if url != want {
		t.Fatalf("vertex URL mismatch: got %s, want %s", url, want)
	}

	// Vertex streaming with SA
	urlStream := a.buildURL("gemini-1.5-pro", true, creds)
	wantStream := "https://aiplatform.googleapis.com/v1/projects/my-project/locations/us-central1/publishers/google/models/gemini-1.5-pro:streamGenerateContent?alt=sse"
	if urlStream != wantStream {
		t.Fatalf("vertex stream URL mismatch: got %s, want %s", urlStream, wantStream)
	}

	// Vertex partner with raw key
	cfgPartner := config.ProviderConfig{Name: "vxp", Type: "vertex-partner", ProviderID: "vertex-partner", BaseURL: "", APIKey: "raw-key", GCPProjectID: "partner-proj", Enabled: true}
	aPartner := NewVertexAdapter(cfgPartner, config.ErrorConfig{}, tr, "")
	credsPartner := &vertexCredentials{rawKey: "raw-key", projectID: "partner-proj", location: "us-central1"}
	urlPartner := aPartner.buildURL("llama-3", false, credsPartner)
	wantPartner := "https://aiplatform.googleapis.com/v1/projects/partner-proj/locations/global/endpoints/openapi/chat/completions?key=raw-key"
	if urlPartner != wantPartner {
		t.Fatalf("vertex-partner URL mismatch: got %s, want %s", urlPartner, wantPartner)
	}

	// Vertex without project ID (raw key fallback to global endpoint)
	cfgNoProject := config.ProviderConfig{Name: "vertex", Type: "vertex", ProviderID: "vertex", BaseURL: "", APIKey: "raw-key", Enabled: true}
	aNoProject := NewVertexAdapter(cfgNoProject, config.ErrorConfig{}, tr, "")
	credsNoProject := &vertexCredentials{rawKey: "raw-key", location: "us-central1"}
	urlNoProject := aNoProject.buildURL("gemini-1.5-pro", false, credsNoProject)
	wantNoProject := "https://aiplatform.googleapis.com/v1/publishers/google/models/gemini-1.5-pro:generateContent?key=raw-key"
	if urlNoProject != wantNoProject {
		t.Fatalf("vertex no-project URL mismatch: got %s, want %s", urlNoProject, wantNoProject)
	}

	// Vertex streaming without project (raw key with ?alt=sse)
	urlNoProjectStream := aNoProject.buildURL("gemini-1.5-pro", true, credsNoProject)
	wantNoProjectStream := "https://aiplatform.googleapis.com/v1/publishers/google/models/gemini-1.5-pro:streamGenerateContent?alt=sse&key=raw-key"
	if urlNoProjectStream != wantNoProjectStream {
		t.Fatalf("vertex no-project stream URL mismatch: got %s, want %s", urlNoProjectStream, wantNoProjectStream)
	}
}

func TestVertexAdapter_ChatCompletion_Partner(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/chat/completions") {
			t.Fatalf("expected chat/completions path, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"vx-ok","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"vertex ok"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	cfg := config.ProviderConfig{Name: "vxp", Type: "vertex-partner", ProviderID: "vertex-partner", BaseURL: server.URL, APIKey: "k", GCPProjectID: "p1", Enabled: true}
	a := NewVertexAdapter(cfg, config.ErrorConfig{}, translator.NewRegistry(), "")
	resp, err := a.ChatCompletion(context.Background(), ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "hi"}}}, "llama-3")
	if err != nil {
		t.Fatalf("ChatCompletion error: %v", err)
	}
	if resp.ID != "vx-ok" {
		t.Fatalf("unexpected id: %s", resp.ID)
	}
}

// --- Gemini CLI ---

func TestGeminiCLIAdapter_URLBuilding(t *testing.T) {
	tr := translator.NewRegistry()

	cfg := config.ProviderConfig{Name: "gc", Type: "gemini-cli", ProviderID: "gemini-cli", BaseURL: "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash", Enabled: true}
	a := NewGeminiCLIAdapter(cfg, config.ErrorConfig{}, tr, "")
	url := a.buildURL("gemini-2.0-flash", false)
	want := "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:generateContent"
	if url != want {
		t.Fatalf("URL mismatch: got %s, want %s", url, want)
	}
	urlStream := a.buildURL("gemini-2.0-flash", true)
	wantStream := "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:streamGenerateContent?alt=sse"
	if urlStream != wantStream {
		t.Fatalf("stream URL mismatch: got %s, want %s", urlStream, wantStream)
	}
}

func TestGeminiCLIAdapter_Headers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "GeminiCLI/") {
			t.Fatalf("expected GeminiCLI user-agent, got %s", r.Header.Get("User-Agent"))
		}
		if r.Header.Get("X-Goog-Api-Client") == "" {
			t.Fatalf("missing X-Goog-Api-Client header")
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-token" {
			t.Fatalf("unexpected authorization: %s", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		// Return Gemini-format response (translated by gemini translator to OpenAI)
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":2,"totalTokenCount":7}}`))
	}))
	defer server.Close()

	// BaseURL must include the model path since buildURL appends :generateContent directly
	cfg := config.ProviderConfig{Name: "gc", Type: "gemini-cli", BaseURL: server.URL + "/models/gemini-2.0-flash", APIKey: "test-token", Enabled: true}
	a := NewGeminiCLIAdapter(cfg, config.ErrorConfig{}, translator.NewRegistry(), "")
	resp, err := a.ChatCompletion(context.Background(), ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "hi"}}}, "gemini-2.0-flash")
	if err != nil {
		t.Fatalf("ChatCompletion error: %v", err)
	}
	content, _ := resp.Choices[0].Message.Content.(string)
	if content != "ok" {
		t.Fatalf("unexpected content: %s", content)
	}
}

// --- Antigravity ---

func TestAntigravityAdapter_RequestBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// URL must contain /v1internal:
		if !strings.Contains(r.URL.Path, "/v1internal") {
			t.Fatalf("expected /v1internal path, got %s", r.URL.Path)
		}
		// Must have X-Machine-Session-Id and User-Agent
		if r.Header.Get("X-Machine-Session-Id") == "" {
			t.Fatalf("missing X-Machine-Session-Id header")
		}
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "antigravity/") {
			t.Fatalf("expected antigravity user-agent, got %s", r.Header.Get("User-Agent"))
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["project"] == nil {
			t.Fatalf("missing project field")
		}
		if body["model"] != "ag-model" {
			t.Fatalf("unexpected model: %v", body["model"])
		}
		if body["userAgent"] != "antigravity" {
			t.Fatalf("unexpected userAgent: %v", body["userAgent"])
		}
		req, ok := body["request"].(map[string]interface{})
		if !ok {
			t.Fatalf("missing request wrapper")
		}
		if req["contents"] == nil {
			t.Fatalf("missing contents")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ag-ok","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	cfg := config.ProviderConfig{Name: "ag", Type: "antigravity", BaseURL: server.URL, APIKey: "k", Enabled: true}
	a := NewAntigravityAdapter(cfg, config.ErrorConfig{}, "")
	resp, err := a.ChatCompletion(context.Background(), ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "hello"}}}, "ag-model")
	if err != nil {
		t.Fatalf("ChatCompletion error: %v", err)
	}
	if resp.ID != "ag-ok" {
		t.Fatalf("unexpected id: %s", resp.ID)
	}
}

func TestAntigravityAdapter_SystemMessageConverted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		req := body["request"].(map[string]interface{})
		_, ok := req["systemInstruction"].(map[string]interface{})
		if !ok {
			t.Fatalf("missing systemInstruction")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ag-sys","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	cfg := config.ProviderConfig{Name: "ag", Type: "antigravity", BaseURL: server.URL, APIKey: "k", Enabled: true}
	a := NewAntigravityAdapter(cfg, config.ErrorConfig{}, "")
	_, err := a.ChatCompletion(context.Background(), ChatRequest{Messages: []ChatMessage{
		{Role: "system", Content: "be helpful"},
		{Role: "user", Content: "hello"},
	}}, "ag-model")
	if err != nil {
		t.Fatalf("ChatCompletion error: %v", err)
	}
}

func TestAntigravityAdapter_BuildURLs(t *testing.T) {
	// No baseURL configured → should use fallback list with /v1internal path
	a := &AntigravityAdapter{}
	urls := a.buildURLs(false)
	if len(urls) != 2 {
		t.Fatalf("expected 2 fallback URLs, got %d", len(urls))
	}
	if !strings.Contains(urls[0], "/v1internal:generateContent") {
		t.Fatalf("unexpected URL: %s", urls[0])
	}
	urlsStream := a.buildURLs(true)
	if !strings.Contains(urlsStream[0], "/v1internal:streamGenerateContent") {
		t.Fatalf("unexpected stream URL: %s", urlsStream[0])
	}

	// With explicit baseURL → single URL
	aCustom := &AntigravityAdapter{baseURL: "https://custom.example.com"}
	urlsCustom := aCustom.buildURLs(false)
	if len(urlsCustom) != 1 {
		t.Fatalf("expected 1 URL for custom base, got %d", len(urlsCustom))
	}
	if urlsCustom[0] != "https://custom.example.com/v1internal:generateContent" {
		t.Fatalf("unexpected custom URL: %s", urlsCustom[0])
	}
}

func TestAntigravityAdapter_CloakTools(t *testing.T) {
	contents := []map[string]interface{}{
		{"role": "model", "parts": []map[string]interface{}{
			{"functionCall": map[string]interface{}{"name": "my_tool", "args": map[string]interface{}{}}},
		}},
	}
	tools := []map[string]interface{}{
		{"functionDeclarations": []interface{}{
			map[string]interface{}{"name": "my_tool", "description": "does something"},
		}},
	}
	cloakedContents, cloakedTools, toolNameMap := cloakTools(contents, tools)
	if toolNameMap["my_tool_ide"] != "my_tool" {
		t.Fatalf("expected toolNameMap[my_tool_ide]=my_tool, got %v", toolNameMap)
	}
	// Tool should be suffixed in declarations
	fds := cloakedTools[0]["functionDeclarations"].([]map[string]interface{})
	if fds[0]["name"] != "my_tool_ide" {
		t.Fatalf("expected my_tool_ide in declarations, got %v", fds[0]["name"])
	}
	// functionCall in contents should be suffixed
	parts := cloakedContents[0]["parts"].([]map[string]interface{})
	fc := parts[0]["functionCall"].(map[string]interface{})
	if fc["name"] != "my_tool_ide" {
		t.Fatalf("expected functionCall name my_tool_ide, got %v", fc["name"])
	}
}
