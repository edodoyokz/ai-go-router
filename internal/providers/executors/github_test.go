package executors

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/edodoyokz/ai-go-router/internal/config"
	"github.com/edodoyokz/ai-go-router/internal/providers"
)

func TestGitHubExecutor_ChatCompletion_Standard(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		// Verify headers
		if r.Header.Get("Authorization") == "" {
			t.Error("missing Authorization header")
		}
		if r.Header.Get("copilot-integration-id") != "vscode-chat" {
			t.Error("missing or incorrect copilot-integration-id header")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-123",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "gpt-4",
			"choices": []map[string]interface{}{
				{
					"index": 0,
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "Hello! How can I help you?",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]interface{}{
				"prompt_tokens":     10,
				"completion_tokens": 20,
				"total_tokens":      30,
			},
		})
	}))
	defer server.Close()

	cfg := config.ProviderConfig{
		Name:    "github-test",
		Type:    "github",
		BaseURL: server.URL,
		Accounts: []config.AccountConfig{
			{
				Name: "test-account",
				ProviderSpecificData: map[string]any{
					"copilotToken": "test-token",
				},
			},
		},
	}

	executor, err := NewGitHubExecutor(cfg, config.ErrorConfig{})
	if err != nil {
		t.Fatalf("failed to create executor: %v", err)
	}

	req := providers.ChatRequest{
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "Hello"},
		},
	}

	resp, err := executor.ChatCompletion(context.Background(), req, "gpt-4")
	if err != nil {
		t.Fatalf("ChatCompletion failed: %v", err)
	}

	if resp.ID != "chatcmpl-123" {
		t.Errorf("unexpected ID: %s", resp.ID)
	}

	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}

	if resp.Choices[0].Message.Role != "assistant" {
		t.Errorf("unexpected role: %s", resp.Choices[0].Message.Role)
	}
}

func TestGitHubExecutor_ChatCompletion_FallbackToResponses(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++

		if callCount == 1 {
			// First call to /chat/completions returns error
			if r.URL.Path != "/chat/completions" {
				t.Errorf("first call should be to /chat/completions, got: %s", r.URL.Path)
			}
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error": "not accessible via the /chat/completions endpoint"}`))
			return
		}

		// Second call should be to /v1/responses
		if r.URL.Path != "/v1/responses" {
			t.Errorf("second call should be to /v1/responses, got: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "resp-123",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "gpt-4-codex",
			"choices": []map[string]interface{}{
				{
					"index": 0,
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "Code response",
					},
					"finish_reason": "stop",
				},
			},
		})
	}))
	defer server.Close()

	cfg := config.ProviderConfig{
		Name:    "github-test",
		Type:    "github",
		BaseURL: server.URL,
		Accounts: []config.AccountConfig{
			{
				Name: "test-account",
				ProviderSpecificData: map[string]any{
					"copilotToken": "test-token",
				},
			},
		},
	}

	executor, err := NewGitHubExecutor(cfg, config.ErrorConfig{})
	if err != nil {
		t.Fatalf("failed to create executor: %v", err)
	}

	req := providers.ChatRequest{
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "Hello"},
		},
	}

	// First call should fail and fallback to /responses
	_, err = executor.ChatCompletion(context.Background(), req, "gpt-4-codex")
	if err == nil {
		t.Error("expected error on first call, got nil")
	}

	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}
}

func TestGitHubExecutor_StreamChatCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("streaming not supported")
		}

		// Send chunks
		chunks := []string{
			`data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1234567890,"model":"gpt-4","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"},"finish_reason":null}]}`,
			`data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1234567890,"model":"gpt-4","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":null}]}`,
			`data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1234567890,"model":"gpt-4","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
			`data: [DONE]`,
		}

		for _, chunk := range chunks {
			w.Write([]byte(chunk + "\n\n"))
			flusher.Flush()
		}
	}))
	defer server.Close()

	cfg := config.ProviderConfig{
		Name:    "github-test",
		Type:    "github",
		BaseURL: server.URL,
		Accounts: []config.AccountConfig{
			{
				Name: "test-account",
				ProviderSpecificData: map[string]any{
					"copilotToken": "test-token",
				},
			},
		},
	}

	executor, err := NewGitHubExecutor(cfg, config.ErrorConfig{})
	if err != nil {
		t.Fatalf("failed to create executor: %v", err)
	}

	req := providers.ChatRequest{
		Messages: []providers.ChatMessage{
			{Role: "user", Content: "Hello"},
		},
	}

	ch, err := executor.StreamChatCompletion(context.Background(), req, "gpt-4")
	if err != nil {
		t.Fatalf("StreamChatCompletion failed: %v", err)
	}

	chunks := []providers.ChatChunk{}
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	if len(chunks) < 2 {
		t.Errorf("expected at least 2 chunks, got %d", len(chunks))
	}
}

func TestGitHubExecutor_RefreshCopilotToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "token test-github-token" {
			t.Errorf("unexpected auth header: %s", auth)
		}

		w.Header().Set("Content-Type", "application/json")
		expiresAt := time.Now().Add(1 * time.Hour).Unix()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"token":      "new-copilot-token",
			"expires_at": expiresAt,
		})
	}))
	defer server.Close()

	cfg := config.ProviderConfig{
		Name:    "github-test",
		Type:    "github",
		BaseURL: server.URL,
	}

	executor, err := NewGitHubExecutor(cfg, config.ErrorConfig{})
	if err != nil {
		t.Fatalf("failed to create executor: %v", err)
	}

	// Cast to concrete type to access RefreshCopilotToken
	githubExec, ok := executor.(*GitHubExecutor)
	if !ok {
		t.Fatal("executor is not *GitHubExecutor")
	}

	// Create a custom client that uses our test server
	githubExec.client = &http.Client{
		Transport: &testTransport{
			server: server,
		},
	}

	token, expiresAt, err := githubExec.RefreshCopilotToken(context.Background(), "test-github-token")
	if err != nil {
		t.Fatalf("RefreshCopilotToken failed: %v", err)
	}

	if token != "new-copilot-token" {
		t.Errorf("unexpected token: %s", token)
	}

	if expiresAt.IsZero() {
		t.Error("expiresAt should not be zero")
	}
}

// testTransport redirects all requests to the test server
type testTransport struct {
	server *httptest.Server
}

func (t *testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Redirect to test server
	req.URL.Scheme = "http"
	req.URL.Host = t.server.Listener.Addr().String()
	return http.DefaultTransport.RoundTrip(req)
}

func TestGitHubExecutor_SanitizeMessages(t *testing.T) {
	cfg := config.ProviderConfig{
		Name: "github-test",
		Type: "github",
		Accounts: []config.AccountConfig{
			{
				Name: "test-account",
				ProviderSpecificData: map[string]any{
					"copilotToken": "test-token",
				},
			},
		},
	}

	executor, err := NewGitHubExecutor(cfg, config.ErrorConfig{})
	if err != nil {
		t.Fatalf("failed to create executor: %v", err)
	}

	tests := []struct {
		name     string
		input    map[string]interface{}
		wantMsgs int
	}{
		{
			name: "string content preserved",
			input: map[string]interface{}{
				"messages": []providers.ChatMessage{
					{Role: "user", Content: "Hello"},
				},
			},
			wantMsgs: 1,
		},
		{
			name: "text and image_url preserved",
			input: map[string]interface{}{
				"messages": []providers.ChatMessage{
					{
						Role: "user",
						Content: []interface{}{
							map[string]interface{}{"type": "text", "text": "What is this?"},
							map[string]interface{}{"type": "image_url", "image_url": map[string]string{"url": "http://example.com/img.jpg"}},
						},
					},
				},
			},
			wantMsgs: 1,
		},
		{
			name: "tool_use converted to text",
			input: map[string]interface{}{
				"messages": []providers.ChatMessage{
					{
						Role: "assistant",
						Content: []interface{}{
							map[string]interface{}{"type": "tool_use", "name": "search", "input": map[string]string{"query": "test"}},
						},
					},
				},
			},
			wantMsgs: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Cast to concrete type to access private methods
			githubExec, ok := executor.(*GitHubExecutor)
			if !ok {
				t.Fatal("executor is not *GitHubExecutor")
			}
			
			result := githubExec.sanitizeMessagesForChatCompletions(tt.input)
			messagesJSON, _ := json.Marshal(result["messages"])
			var messages []providers.ChatMessage
			json.Unmarshal(messagesJSON, &messages)
			if len(messages) != tt.wantMsgs {
				t.Errorf("expected %d messages, got %d", tt.wantMsgs, len(messages))
			}
		})
	}
}

func TestGitHubExecutor_ModelSpecificBehavior(t *testing.T) {
	cfg := config.ProviderConfig{
		Name: "github-test",
		Type: "github",
		Accounts: []config.AccountConfig{
			{
				Name: "test-account",
				ProviderSpecificData: map[string]any{
					"copilotToken": "test-token",
				},
			},
		},
	}

	executor, err := NewGitHubExecutor(cfg, config.ErrorConfig{})
	if err != nil {
		t.Fatalf("failed to create executor: %v", err)
	}

	// Cast to concrete type to access private methods
	githubExec, ok := executor.(*GitHubExecutor)
	if !ok {
		t.Fatal("executor is not *GitHubExecutor")
	}

	tests := []struct {
		model                       string
		requiresMaxCompletionTokens bool
		supportsTemperature         bool
		supportsThinking            bool
	}{
		{"gpt-4", false, true, true},
		{"gpt-5", true, true, true},
		{"gpt-5.4", true, false, true},
		{"o1-preview", true, true, true},
		{"claude-3-opus", false, true, false},
		{"claude-3.5-sonnet", false, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			if got := githubExec.requiresMaxCompletionTokens(tt.model); got != tt.requiresMaxCompletionTokens {
				t.Errorf("requiresMaxCompletionTokens(%s) = %v, want %v", tt.model, got, tt.requiresMaxCompletionTokens)
			}
			if got := githubExec.supportsTemperature(tt.model); got != tt.supportsTemperature {
				t.Errorf("supportsTemperature(%s) = %v, want %v", tt.model, got, tt.supportsTemperature)
			}
			if got := githubExec.supportsThinking(tt.model); got != tt.supportsThinking {
				t.Errorf("supportsThinking(%s) = %v, want %v", tt.model, got, tt.supportsThinking)
			}
		})
	}
}

func TestGitHubExecutor_NeedsRefresh(t *testing.T) {
	tests := []struct {
		name                 string
		providerSpecificData map[string]any
		want                 bool
	}{
		{
			name:                 "no copilot token",
			providerSpecificData: map[string]any{},
			want:                 true,
		},
		{
			name: "valid token not expiring soon",
			providerSpecificData: map[string]any{
				"copilotToken":          "test-token",
				"copilotTokenExpiresAt": time.Now().Add(1 * time.Hour).Format(time.RFC3339),
			},
			want: false,
		},
		{
			name: "token expiring soon",
			providerSpecificData: map[string]any{
				"copilotToken":          "test-token",
				"copilotTokenExpiresAt": time.Now().Add(2 * time.Minute).Format(time.RFC3339),
			},
			want: true,
		},
		{
			name: "token expired",
			providerSpecificData: map[string]any{
				"copilotToken":          "test-token",
				"copilotTokenExpiresAt": time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.ProviderConfig{
				Name: "github-test",
				Type: "github",
				Accounts: []config.AccountConfig{
					{
						Name:                 "test-account",
						ProviderSpecificData: tt.providerSpecificData,
					},
				},
			}

			executor, err := NewGitHubExecutor(cfg, config.ErrorConfig{})
			if err != nil {
				t.Fatalf("failed to create executor: %v", err)
			}

			// Cast to concrete type to access NeedsRefresh
			githubExec, ok := executor.(*GitHubExecutor)
			if !ok {
				t.Fatal("executor is not *GitHubExecutor")
			}

			if got := githubExec.NeedsRefresh(); got != tt.want {
				t.Errorf("NeedsRefresh() = %v, want %v", got, tt.want)
			}
		})
	}
}
