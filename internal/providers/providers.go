package providers

import (
	"context"
	"fmt"
)

type contextKey string

const AccountContextKey contextKey = "account"

type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Stream      bool          `json:"stream,omitempty"`
	Temperature *float64      `json:"temperature,omitempty"`
	TopP        *float64      `json:"top_p,omitempty"`
	MaxTokens   *int          `json:"max_tokens,omitempty"`
	Stop        []string      `json:"stop,omitempty"`
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []ChatChoice `json:"choices"`
	Usage   *Usage       `json:"usage,omitempty"`
}

type ChatChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason,omitempty"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type Adapter interface {
	Name() string
	ChatCompletion(ctx context.Context, request ChatRequest, model string) (ChatResponse, error)
	StreamChatCompletion(ctx context.Context, request ChatRequest, model string) (<-chan ChatChunk, error)
}

type ChatChunk struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Created int64         `json:"created"`
	Model   string        `json:"model"`
	Choices []ChunkChoice `json:"choices"`
}

type ChunkChoice struct {
	Index        int        `json:"index"`
	Delta        ChunkDelta `json:"delta"`
	FinishReason *string    `json:"finish_reason,omitempty"`
}

type ChunkDelta struct {
	Content string `json:"content,omitempty"`
	Role    string `json:"role,omitempty"`
}

type Registry struct {
	providers map[string]Adapter
}

func NewRegistry(adapters ...Adapter) *Registry {
	items := make(map[string]Adapter, len(adapters))
	for _, adapter := range adapters {
		items[adapter.Name()] = adapter
	}
	return &Registry{providers: items}
}

func (r *Registry) Get(name string) (Adapter, error) {
	adapter, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider not registered: %s", name)
	}
	return adapter, nil
}
