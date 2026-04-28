package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/edodoyokz/ai-go-router/internal/config"
)

func TestBatchAExecutors_ClassifyAuthFailures(t *testing.T) {
	t.Run("codex 401", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"expired"}`))
		}))
		defer server.Close()
		exec, _ := NewCodexExecutor(config.ProviderConfig{Name: "codex", Type: "codex", BaseURL: server.URL, APIKey: "t"}, config.ErrorConfig{})
		_, err := exec.ChatCompletion(context.Background(), ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "hi"}}}, "gpt-5-codex")
		if err == nil || !IsAuthFailure(err) {
			t.Fatalf("expected auth failure, got %v", err)
		}
	})

	t.Run("cursor 401", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"expired"}`))
		}))
		defer server.Close()
		exec, _ := NewCursorExecutor(config.ProviderConfig{Name: "cursor", Type: "cursor", BaseURL: server.URL, APIKey: "t"}, config.ErrorConfig{})
		_, err := exec.ChatCompletion(context.Background(), ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "hi"}}}, "cursor-model")
		if err == nil || !IsAuthFailure(err) {
			t.Fatalf("expected auth failure, got %v", err)
		}
	})

	t.Run("kiro 401", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"expired"}`))
		}))
		defer server.Close()
		exec, _ := NewKiroExecutor(config.ProviderConfig{Name: "kiro", Type: "kiro", BaseURL: server.URL, APIKey: "t"}, config.ErrorConfig{})
		_, err := exec.ChatCompletion(context.Background(), ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "hi"}}}, "kiro-model")
		if err == nil || !IsAuthFailure(err) {
			t.Fatalf("expected auth failure, got %v", err)
		}
	})
}
