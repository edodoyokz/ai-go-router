package providers

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"sync"
)

type contextKey string

const AccountContextKey contextKey = "account"

type ChatRequest struct {
	Model             string          `json:"model"`
	Messages          []ChatMessage   `json:"messages"`
	Stream            bool            `json:"stream,omitempty"`
	Temperature       *float64        `json:"temperature,omitempty"`
	TopP              *float64        `json:"top_p,omitempty"`
	MaxTokens         *int            `json:"max_tokens,omitempty"`
	Stop              []string        `json:"stop,omitempty"`
	Thinking          *ThinkingParams `json:"thinking,omitempty"`
	NativePassthrough bool            `json:"-"`
}

// ThinkingParams carries extended reasoning configuration for providers that support it.
type ThinkingParams struct {
	Enabled          bool `json:"enabled"`
	MaxTokens        int  `json:"budget_tokens,omitempty"`
	IncludeReasoning bool `json:"include_reasoning,omitempty"`
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

type EmbeddingsRequest struct {
	Input          string `json:"input"`
	Model          string `json:"model"`
	EncodingFormat string `json:"encoding_format,omitempty"`
}

type EmbeddingsResponse struct {
	Object string         `json:"object"`
	Data   []Embedding    `json:"data"`
	Model  string         `json:"model"`
	Usage  EmbeddingUsage `json:"usage"`
}

type Embedding struct {
	Object    string    `json:"object"`
	Embedding []float64 `json:"embedding"`
	Index     int       `json:"index"`
}

type EmbeddingUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type AudioSpeechRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
	Voice string `json:"voice"`
}

type AudioSpeechResponse struct {
	ContentType string `json:"content_type"`
	Data        []byte `json:"data"`
}

type ImagesGenerationsRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	N      int    `json:"n,omitempty"`
	Size   string `json:"size,omitempty"`
}

type ImagesGenerationsResponse struct {
	Created int64           `json:"created"`
	Data    []ImageResponse `json:"data"`
}

type ImageResponse struct {
	URL string `json:"url"`
}

type Adapter interface {
	Name() string
	ChatCompletion(ctx context.Context, request ChatRequest, model string) (ChatResponse, error)
	StreamChatCompletion(ctx context.Context, request ChatRequest, model string) (<-chan ChatChunk, error)
	GetUsage(ctx context.Context) (map[string]interface{}, error)
	Embeddings(ctx context.Context, request EmbeddingsRequest, model string) (EmbeddingsResponse, error)
	AudioSpeech(ctx context.Context, request AudioSpeechRequest, model string) (AudioSpeechResponse, error)
	ImagesGenerations(ctx context.Context, request ImagesGenerationsRequest, model string) (ImagesGenerationsResponse, error)
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
	mu        sync.RWMutex
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
	r.mu.RLock()
	defer r.mu.RUnlock()

	adapter, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider not registered: %s", name)
	}
	return adapter, nil
}

func (r *Registry) ReplaceAll(adapters ...Adapter) {
	r.mu.Lock()
	defer r.mu.Unlock()

	items := make(map[string]Adapter, len(adapters))
	for _, adapter := range adapters {
		items[adapter.Name()] = adapter
	}
	r.providers = items
}

// sseScanner wraps bufio.Scanner for SSE (Server-Sent Events) parsing
type sseScanner struct {
	scanner *bufio.Scanner
}

func newSSEScanner(r io.Reader) *sseScanner {
	return &sseScanner{scanner: bufio.NewScanner(r)}
}

func (s *sseScanner) Scan() bool {
	return s.scanner.Scan()
}

func (s *sseScanner) Text() string {
	return s.scanner.Text()
}

func (s *sseScanner) Err() error {
	return s.scanner.Err()
}
