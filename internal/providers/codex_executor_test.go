package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/edodoyokz/ai-go-router/internal/config"
)

func TestCodexExecutor_ChatCompletion(t *testing.T) {
	var gotPath, gotAuth, gotSession string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotSession = r.Header.Get("session_id")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp-1","object":"response","model":"gpt-5-codex","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"codex ok"}]}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`))
	}))
	defer server.Close()

	exec, err := NewCodexExecutor(config.ProviderConfig{
		Name:    "codex",
		Type:    "codex",
		BaseURL: server.URL + "/backend-api/codex/responses",
		Accounts: []config.AccountConfig{{
			Name:        "codex-account",
			AccessToken: "codex-token",
		}},
	}, config.ErrorConfig{})
	if err != nil {
		t.Fatalf("NewCodexExecutor error: %v", err)
	}
	temp := 0.7
	resp, err := exec.ChatCompletion(context.Background(), ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "hi"}}, Temperature: &temp}, "gpt-5-codex")
	if err != nil {
		t.Fatalf("ChatCompletion error: %v", err)
	}
	if gotPath != "/backend-api/codex/responses" {
		t.Fatalf("path=%q", gotPath)
	}
	if gotAuth != "Bearer codex-token" || gotSession == "" {
		t.Fatalf("auth=%q session=%q", gotAuth, gotSession)
	}
	if gotBody["store"] != false || gotBody["stream"] != true || gotBody["instructions"] == "" {
		t.Fatalf("body missing codex defaults: %#v", gotBody)
	}
	if _, ok := gotBody["temperature"]; ok {
		t.Fatalf("temperature should be stripped: %#v", gotBody)
	}
	if resp.Choices[0].Message.Content != "codex ok" || resp.Usage == nil || resp.Usage.TotalTokens != 5 {
		t.Fatalf("response=%#v", resp)
	}
}

func TestCodexExecutor_CompactPath(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"id":"resp-compact","object":"response","model":"gpt-5-codex","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"compact ok"}]}]}`))
	}))
	defer server.Close()
	exec, err := NewCodexExecutor(config.ProviderConfig{
		Name:    "codex",
		Type:    "codex",
		BaseURL: server.URL + "/backend-api/codex/responses",
		APIKey:  "codex-token",
	}, config.ErrorConfig{})
	if err != nil {
		t.Fatalf("NewCodexExecutor error: %v", err)
	}
	_, err = exec.ChatCompletion(context.Background(), ChatRequest{
		Messages: []ChatMessage{{Role: "user", Content: "compact"}},
		Extra:    map[string]json.RawMessage{"_compact": json.RawMessage(`true`)},
	}, "gpt-5-codex")
	if err != nil {
		t.Fatalf("ChatCompletion error: %v", err)
	}
	if gotPath != "/backend-api/codex/responses/compact" {
		t.Fatalf("path=%q", gotPath)
	}
}

func TestCodexExecutor_StreamChatCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"resp-stream","object":"response","model":"gpt-5-codex","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"stream ok"}]}]}`))
	}))
	defer server.Close()
	exec, err := NewCodexExecutor(config.ProviderConfig{Name: "codex", Type: "codex", BaseURL: server.URL, APIKey: "codex-token"}, config.ErrorConfig{})
	if err != nil {
		t.Fatalf("NewCodexExecutor error: %v", err)
	}
	ch, err := exec.StreamChatCompletion(context.Background(), ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "hi"}}}, "gpt-5-codex")
	if err != nil {
		t.Fatalf("StreamChatCompletion error: %v", err)
	}
	var text string
	var sawFinish bool
	for chunk := range ch {
		if len(chunk.Choices) == 0 {
			continue
		}
		if s, ok := chunk.Choices[0].Delta.Content.(string); ok {
			text += s
		}
		if chunk.Choices[0].FinishReason != nil {
			sawFinish = true
		}
	}
	if text != "stream ok" || !sawFinish {
		t.Fatalf("text=%q sawFinish=%v", text, sawFinish)
	}
}

func TestCodexExecutor_StreamResponsesSSE(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.output_text.delta\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"response_id\":\"resp-sse\",\"delta\":\"hello \"}\n\n"))
		_, _ = w.Write([]byte("event: response.reasoning_summary_text.delta\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.reasoning_summary_text.delta\",\"response_id\":\"resp-sse\",\"delta\":\"thinking\"}\n\n"))
		_, _ = w.Write([]byte("event: response.output_text.delta\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"response_id\":\"resp-sse\",\"delta\":\"world\"}\n\n"))
		_, _ = w.Write([]byte("event: response.completed\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":4,\"output_tokens\":5}}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()
	exec, err := NewCodexExecutor(config.ProviderConfig{Name: "codex", Type: "codex", BaseURL: server.URL, APIKey: "codex-token"}, config.ErrorConfig{})
	if err != nil {
		t.Fatalf("NewCodexExecutor error: %v", err)
	}
	ch, err := exec.StreamChatCompletion(context.Background(), ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "hi"}}}, "gpt-5-codex")
	if err != nil {
		t.Fatalf("StreamChatCompletion error: %v", err)
	}
	var text, thinking string
	var usage *Usage
	for chunk := range ch {
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		if s, ok := chunk.Choices[0].Delta.Content.(string); ok {
			text += s
		}
		thinking += chunk.Choices[0].Delta.Thinking
	}
	if text != "hello world" || thinking != "thinking" {
		t.Fatalf("text=%q thinking=%q", text, thinking)
	}
	if usage == nil || usage.PromptTokens != 4 || usage.CompletionTokens != 5 || usage.TotalTokens != 9 {
		t.Fatalf("usage=%#v", usage)
	}
}

func TestCodexExecutor_RefreshCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "old-refresh" {
			t.Fatalf("form=%v", r.Form)
		}
		if r.Form.Get("client_id") != "client-test" || r.Form.Get("client_secret") != "secret-test" {
			t.Fatalf("client form=%v", r.Form)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new-access","refresh_token":"new-refresh","expires_in":1800,"token_type":"Bearer","scope":"openid profile"}`))
	}))
	defer server.Close()
	exec, err := NewCodexExecutor(config.ProviderConfig{
		Name: "codex",
		Type: "codex",
		Accounts: []config.AccountConfig{{
			Name:         "codex-account",
			RefreshToken: "old-refresh",
			ProviderSpecificData: map[string]any{
				"token_url": server.URL,
			},
		}},
		ProviderSpecificData: map[string]any{
			"client_id":     "client-test",
			"client_secret": "secret-test",
		},
	}, config.ErrorConfig{})
	if err != nil {
		t.Fatalf("NewCodexExecutor error: %v", err)
	}
	codex := exec.(*CodexExecutor)
	result, err := codex.RefreshCredentials(context.Background())
	if err != nil {
		t.Fatalf("RefreshCredentials error: %v", err)
	}
	if result.AccessToken != "new-access" || result.RefreshToken != "new-refresh" || result.Scope != "openid profile" {
		t.Fatalf("result=%#v", result)
	}
	if time.Until(result.ExpiresAt) < 1700*time.Second {
		t.Fatalf("expiresAt too soon: %s", result.ExpiresAt)
	}
}

func TestCodexExecutor_NeedsRefresh(t *testing.T) {
	soon := time.Now().Add(2 * time.Minute)
	later := time.Now().Add(2 * time.Hour)
	tests := []struct {
		name    string
		account config.AccountConfig
		want    bool
	}{
		{name: "no refresh token", account: config.AccountConfig{AccessToken: "access"}, want: false},
		{name: "missing access token", account: config.AccountConfig{RefreshToken: "refresh"}, want: true},
		{name: "expires soon", account: config.AccountConfig{AccessToken: "access", RefreshToken: "refresh", ExpiresAt: &soon}, want: true},
		{name: "fresh", account: config.AccountConfig{AccessToken: "access", RefreshToken: "refresh", ExpiresAt: &later}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exec, err := NewCodexExecutor(config.ProviderConfig{Name: "codex", Type: "codex", Accounts: []config.AccountConfig{tt.account}}, config.ErrorConfig{})
			if err != nil {
				t.Fatalf("NewCodexExecutor error: %v", err)
			}
			if got := exec.(*CodexExecutor).NeedsRefresh(5 * time.Minute); got != tt.want {
				t.Fatalf("NeedsRefresh=%v want %v", got, tt.want)
			}
		})
	}
}
