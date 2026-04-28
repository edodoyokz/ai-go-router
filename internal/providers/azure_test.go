package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/edodoyokz/ai-go-router/internal/config"
)

func newAzureTestServer(t *testing.T) (*AzureAdapter, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("api-key") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":{"message":"missing api-key header"}}`))
			return
		}
		if r.Header.Get("Content-Type") != "application/json" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if r.URL.Path == "/openai/deployments/test-model/chat/completions" {
			if r.URL.Query().Get("api-version") == "" {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"error":{"message":"missing api-version"}}`))
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{
				"id": "test-id",
				"object": "chat.completion",
				"created": 1234567890,
				"model": "test-model",
				"choices": [{"index": 0, "message": {"role": "assistant", "content": "hello"}, "finish_reason": "stop"}]
			}`))
			return
		}

		if r.URL.Path == "/openai/deployments/test-model/embeddings" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{
				"object": "list",
				"data": [{"object": "embedding", "embedding": [0.1, 0.2], "index": 0}],
				"model": "test-model",
				"usage": {"prompt_tokens": 5, "total_tokens": 5}
			}`))
			return
		}

		if r.URL.Path == "/openai/deployments/test-model/images/generations" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{
				"created": 1234567890,
				"data": [{"url": "https://example.com/image.png"}]
			}`))
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	cfg := config.ProviderConfig{
		Name:    "azure",
		Type:    "azure",
		BaseURL: server.URL,
		APIKey:  "test-azure-key",
		Enabled: true,
	}

	adapter := NewAzureAdapter(cfg, config.ErrorConfig{}, "")
	return adapter, server
}

func TestAzureAdapter_ChatCompletion(t *testing.T) {
	adapter, _ := newAzureTestServer(t)

	resp, err := adapter.ChatCompletion(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []ChatMessage{{Role: "user", Content: "test"}},
	}, "test-model")

	if err != nil {
		t.Fatalf("ChatCompletion() error = %v", err)
	}
	if resp.ID != "test-id" {
		t.Errorf("ID = %s, want test-id", resp.ID)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("Choices length = %d, want 1", len(resp.Choices))
	}
	if resp.Choices[0].Message.Content != "hello" {
		t.Errorf("Content = %v, want hello", resp.Choices[0].Message.Content)
	}
}

func TestAzureAdapter_ChatCompletion_MissingAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("api-key") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := config.ProviderConfig{
		Name:    "azure-noauth",
		Type:    "azure",
		BaseURL: server.URL,
		Enabled: true,
	}

	adapter := NewAzureAdapter(cfg, config.ErrorConfig{}, "")
	_, err := adapter.ChatCompletion(context.Background(), ChatRequest{}, "test-model")
	if err == nil {
		t.Fatal("expected error for missing api-key")
	}
}

func TestAzureAdapter_Embeddings(t *testing.T) {
	adapter, _ := newAzureTestServer(t)

	resp, err := adapter.Embeddings(context.Background(), EmbeddingsRequest{
		Input: "test input",
		Model: "test-model",
	}, "test-model")

	if err != nil {
		t.Fatalf("Embeddings() error = %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("Data length = %d, want 1", len(resp.Data))
	}
	if len(resp.Data[0].Embedding) != 2 {
		t.Errorf("Embedding length = %d, want 2", len(resp.Data[0].Embedding))
	}
}

func TestAzureAdapter_ImagesGenerations(t *testing.T) {
	adapter, _ := newAzureTestServer(t)

	resp, err := adapter.ImagesGenerations(context.Background(), ImagesGenerationsRequest{
		Model:  "test-model",
		Prompt: "a cat",
	}, "test-model")

	if err != nil {
		t.Fatalf("ImagesGenerations() error = %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("Data length = %d, want 1", len(resp.Data))
	}
	if resp.Data[0].URL != "https://example.com/image.png" {
		t.Errorf("URL = %s, want https://example.com/image.png", resp.Data[0].URL)
	}
}

func TestAzureAdapter_BuildURL(t *testing.T) {
	cfg := config.ProviderConfig{
		Name:    "azure",
		Type:    "azure",
		BaseURL: "https://test.openai.azure.com/",
		APIKey:  "test",
		Enabled: true,
	}
	adapter := NewAzureAdapter(cfg, config.ErrorConfig{}, "")

	url := adapter.buildAzureURL("gpt-4", "2024-10-01-preview")
	expected := "https://test.openai.azure.com/openai/deployments/gpt-4/chat/completions?api-version=2024-10-01-preview"
	if url != expected {
		t.Errorf("URL = %s, want %s", url, expected)
	}
}

func TestAzureAdapter_FactoryBuildable(t *testing.T) {
	cfg := config.Config{Providers: []config.ProviderConfig{{
		Name:       "azure",
		ProviderID: "azure",
		Type:       "azure",
		BaseURL:    "https://test.openai.azure.com",
		APIKey:     "test-key",
		Enabled:    true,
	}}}

	_, err := BuildRegistryFromConfig(cfg)
	if err != nil {
		t.Fatalf("expected azure to be buildable: %v", err)
	}
}

func TestAzureAdapter_StreamingResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"id\":\"chunk-1\",\"object\":\"chat.completion.chunk\",\"created\":123,\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}\n\n"))
		w.Write([]byte("data: {\"id\":\"chunk-2\",\"object\":\"chat.completion.chunk\",\"created\":123,\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\" there\"},\"finish_reason\":\"stop\"}]}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	cfg := config.ProviderConfig{
		Name:    "azure-stream",
		Type:    "azure",
		BaseURL: server.URL,
		APIKey:  "test-key",
		Enabled: true,
	}

	adapter := NewAzureAdapter(cfg, config.ErrorConfig{}, "")
	ch, err := adapter.StreamChatCompletion(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []ChatMessage{{Role: "user", Content: "test"}},
	}, "test-model")

	if err != nil {
		t.Fatalf("StreamChatCompletion() error = %v", err)
	}

	var chunks []ChatChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0].ID != "chunk-1" {
		t.Errorf("chunk 0 ID = %s, want chunk-1", chunks[0].ID)
	}
}

func TestAzureRedactedOutput(t *testing.T) {
	adapter, _ := newAzureTestServer(t)

	_ = adapter.AccountNames()

	serialized, _ := json.Marshal(map[string]string{"name": adapter.Name()})
	if containsSensitive(serialized, "test-azure-key") {
		t.Errorf("API key leaked in serialized output: %s", string(serialized))
	}
}

func containsSensitive(data []byte, sensitive string) bool {
	return len(sensitive) > 0 && jsonContains(data, sensitive)
}

func jsonContains(data []byte, needle string) bool {
	return len(data) > 0 && len(needle) > 0 && stringContains(string(data), needle)
}

func stringContains(s, substr string) bool {
	return len(substr) > 0 && len(s) >= len(substr) && func() bool {
		for i := 0; i+len(substr) <= len(s); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	}()
}
