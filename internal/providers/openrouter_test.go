package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/edodoyokz/ai-go-router/internal/config"
)

func TestOpenRouterAdapterStreamingResponse(t *testing.T) {
	var gotPath, gotAuth, gotReferer, gotTitle string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotReferer = r.Header.Get("HTTP-Referer")
		gotTitle = r.Header.Get("X-Title")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"id\":\"chunk-1\",\"object\":\"chat.completion.chunk\",\"created\":123,\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}\n\n"))
		w.Write([]byte("data: {\"id\":\"chunk-2\",\"object\":\"chat.completion.chunk\",\"created\":123,\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\" there\"},\"finish_reason\":\"stop\"}]}\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	adapter := NewOpenRouterAdapter(config.ProviderConfig{
		Name:    "openrouter",
		BaseURL: server.URL,
		APIKey:  "sk-openrouter",
		Enabled: true,
	}, config.ErrorConfig{}, "")
	ch, err := adapter.StreamChatCompletion(context.Background(), ChatRequest{
		Model:    "ignored",
		Messages: []ChatMessage{{Role: "user", Content: "test"}},
	}, "openrouter/model")
	if err != nil {
		t.Fatalf("StreamChatCompletion() error = %v", err)
	}
	var chunks []ChatChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}
	if gotPath != "/chat/completions" {
		t.Fatalf("path=%q", gotPath)
	}
	if gotAuth != "Bearer sk-openrouter" || gotReferer == "" || gotTitle == "" {
		t.Fatalf("headers auth=%q referer=%q title=%q", gotAuth, gotReferer, gotTitle)
	}
	if gotBody["model"] != "openrouter/model" || gotBody["stream"] != true {
		t.Fatalf("body=%#v", gotBody)
	}
	if len(chunks) != 2 || chunks[0].ID != "chunk-1" || chunks[1].Choices[0].Delta.Content != " there" {
		t.Fatalf("chunks=%#v", chunks)
	}
}
