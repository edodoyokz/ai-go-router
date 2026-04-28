package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/edodoyokz/ai-go-router/internal/config"
	"github.com/edodoyokz/ai-go-router/internal/translator"
)

func TestOllamaAdapterChatCompletion(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"model": "llama3",
			"message": map[string]any{
				"role":    "assistant",
				"content": "hello",
			},
			"done":              true,
			"done_reason":       "stop",
			"prompt_eval_count": 3,
			"eval_count":        4,
		})
	}))
	defer server.Close()

	adapter := NewOllamaAdapter(config.ProviderConfig{Name: "ollama", BaseURL: server.URL, Enabled: true}, config.ErrorConfig{}, translator.NewRegistry(), "")
	resp, err := adapter.ChatCompletion(context.Background(), ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "hi"}}}, "llama3")
	if err != nil {
		t.Fatalf("ChatCompletion() error = %v", err)
	}
	if gotPath != "/api/chat" {
		t.Fatalf("path = %s, want /api/chat", gotPath)
	}
	if gotBody["model"] != "llama3" {
		t.Fatalf("request model = %v, want llama3", gotBody["model"])
	}
	if resp.Choices[0].Message.Content != "hello" {
		t.Fatalf("content = %v, want hello", resp.Choices[0].Message.Content)
	}
	if resp.Usage.TotalTokens != 7 {
		t.Fatalf("total tokens = %d, want 7", resp.Usage.TotalTokens)
	}
}

func TestOllamaAdapterEmbeddings(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"embedding": []float64{0.1, 0.2}})
	}))
	defer server.Close()

	adapter := NewOllamaAdapter(config.ProviderConfig{Name: "ollama", BaseURL: server.URL, Enabled: true}, config.ErrorConfig{}, translator.NewRegistry(), "")
	resp, err := adapter.Embeddings(context.Background(), EmbeddingsRequest{Input: "hello"}, "nomic-embed-text")
	if err != nil {
		t.Fatalf("Embeddings() error = %v", err)
	}
	if gotPath != "/api/embeddings" {
		t.Fatalf("path = %s, want /api/embeddings", gotPath)
	}
	if gotBody["model"] != "nomic-embed-text" || gotBody["prompt"] != "hello" {
		t.Fatalf("body = %#v", gotBody)
	}
	if len(resp.Data) != 1 || len(resp.Data[0].Embedding) != 2 || resp.Data[0].Embedding[1] != 0.2 {
		t.Fatalf("embedding response = %#v", resp)
	}
	if resp.Usage.TotalTokens != 2 {
		t.Fatalf("usage = %#v", resp.Usage)
	}
}

func TestOllamaAdapterEmbeddingsParsesEmbedArray(t *testing.T) {
	embedding, err := parseOllamaEmbedding([]byte(`{"embeddings":[[1,2,3]]}`))
	if err != nil {
		t.Fatalf("parseOllamaEmbedding() error = %v", err)
	}
	if len(embedding) != 3 || embedding[2] != 3 {
		t.Fatalf("embedding=%#v", embedding)
	}
}
