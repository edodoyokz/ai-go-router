package providers

import (
	"context"
	"fmt"
)

type ChatRequest struct {
	Model    string         `json:"model"`
	Messages []ChatMessage  `json:"messages"`
	Stream   bool           `json:"stream,omitempty"`
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatResponse struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Model   string        `json:"model"`
	Choices []ChatChoice  `json:"choices"`
}

type ChatChoice struct {
	Index   int                `json:"index"`
	Message ChatMessage        `json:"message"`
	Reason  string             `json:"finish_reason,omitempty"`
}

type Adapter interface {
	Name() string
	ChatCompletion(ctx context.Context, request ChatRequest, model string) (ChatResponse, error)
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

type StubAdapter struct {
	providerName string
}

func NewStubAdapter(name string) *StubAdapter {
	return &StubAdapter{providerName: name}
}

func (s *StubAdapter) Name() string {
	return s.providerName
}

func (s *StubAdapter) ChatCompletion(_ context.Context, request ChatRequest, model string) (ChatResponse, error) {
	lastContent := ""
	if len(request.Messages) > 0 {
		lastContent = request.Messages[len(request.Messages)-1].Content
	}

	return ChatResponse{
		ID:     "chatcmpl_stub_" + s.providerName,
		Object: "chat.completion",
		Model:  model,
		Choices: []ChatChoice{
			{
				Index: 0,
				Message: ChatMessage{
					Role:    "assistant",
					Content: fmt.Sprintf("stub response from provider=%s model=%s prompt=%q", s.providerName, model, lastContent),
				},
				Reason: "stop",
			},
		},
	}, nil
}
