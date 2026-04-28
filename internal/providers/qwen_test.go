package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/edodoyokz/ai-go-router/internal/config"
)

func TestQwenAdapterChatCompletionHeadersAndTransform(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer qw-token" {
			t.Fatalf("auth=%q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("X-DashScope-AuthType") != "qwen-oauth" {
			t.Fatalf("missing qwen auth header")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		messages, ok := body["messages"].([]any)
		if !ok || len(messages) != 2 {
			t.Fatalf("messages=%#v", body["messages"])
		}
		first, _ := messages[0].(map[string]any)
		if first["role"] != "system" {
			t.Fatalf("first message=%#v", first)
		}
		if body["tool_choice"] != "auto" {
			t.Fatalf("tool_choice=%#v", body["tool_choice"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-qwen","object":"chat.completion","created":1,"model":"qwen3-coder-plus","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	adapter := NewQwenAdapter(testProviderConfig("qwen-test", server.URL+"/v1", "qw-token"), testErrorConfig(), "")
	req := ChatRequest{
		Messages:   []ChatMessage{{Role: "user", Content: "hello"}},
		Thinking:   &ThinkingParams{Enabled: true},
		ToolChoice: json.RawMessage(`"required"`),
	}
	resp, err := adapter.ChatCompletion(context.Background(), req, "qwen3-coder-plus")
	if err != nil {
		t.Fatalf("chat failed: %v", err)
	}
	if resp.ID != "chatcmpl-qwen" || resp.Choices[0].Message.Content != "ok" {
		t.Fatalf("response=%#v", resp)
	}
}

func TestQwenAdapterRequiresExistingToken(t *testing.T) {
	adapter := NewQwenAdapter(testProviderConfig("qwen-test", "https://portal.qwen.ai", ""), testErrorConfig(), "")
	_, err := adapter.ChatCompletion(context.Background(), ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "hello"}}}, "qwen3-coder-plus")
	if err == nil {
		t.Fatal("expected missing token error")
	}
	if got := err.Error(); got == "" || !containsAll(got, "existing valid OAuth token") {
		t.Fatalf("error=%v", err)
	}
}

func TestQwenAdapterStreamIncludesUsageOption(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		streamOptions, ok := body["stream_options"].(map[string]any)
		if !ok || streamOptions["include_usage"] != true {
			t.Fatalf("stream_options=%#v", body["stream_options"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chunk-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"qwen\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	adapter := NewQwenAdapter(testProviderConfig("qwen-test", server.URL+"/v1", "qw-token"), testErrorConfig(), "")
	ch, err := adapter.StreamChatCompletion(context.Background(), ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "hello"}}}, "qwen")
	if err != nil {
		t.Fatalf("stream failed: %v", err)
	}
	chunk := <-ch
	if chunk.ID != "chunk-1" || chunk.Choices[0].Delta.Content != "hi" {
		t.Fatalf("chunk=%#v", chunk)
	}
}

func testProviderConfig(name, baseURL, token string) config.ProviderConfig {
	return config.ProviderConfig{Name: name, Type: "qwen", BaseURL: baseURL, APIKey: token, Enabled: true}
}

func testErrorConfig() config.ErrorConfig {
	return config.ErrorConfig{}
}

func containsAll(haystack string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(haystack, needle) {
			return false
		}
	}
	return true
}
