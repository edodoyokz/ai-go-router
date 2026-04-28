package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/edodoyokz/ai-go-router/internal/config"
)

func TestOpenCodeGoAdapter_ChatCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got == "" {
			t.Fatalf("expected authorization header")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ok","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	cfg := config.ProviderConfig{Name: "ocg", Type: "opencode-go", BaseURL: server.URL, APIKey: "k", Enabled: true}
	a := NewOpenCodeGoAdapter(cfg, config.ErrorConfig{}, "")
	resp, err := a.ChatCompletion(context.Background(), ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "hi"}}}, "gpt-4")
	if err != nil {
		t.Fatalf("ChatCompletion error: %v", err)
	}
	if resp.ID != "ok" {
		t.Fatalf("unexpected id: %s", resp.ID)
	}
}

func TestQoderAdapter_SignatureAndHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("session-id") == "" || r.Header.Get("x-qoder-signature") == "" || r.Header.Get("x-qoder-timestamp") == "" {
			t.Fatalf("missing qoder signature headers")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ok","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	cfg := config.ProviderConfig{Name: "qoder", Type: "qoder", BaseURL: server.URL, APIKey: "k", Enabled: true}
	a := NewQoderAdapter(cfg, config.ErrorConfig{}, "")
	resp, err := a.ChatCompletion(context.Background(), ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "hi"}}}, "qoder-model")
	if err != nil {
		t.Fatalf("ChatCompletion error: %v", err)
	}
	if resp.ID != "ok" {
		t.Fatalf("unexpected id: %s", resp.ID)
	}
}
