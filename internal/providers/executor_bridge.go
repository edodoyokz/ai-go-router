package providers

import (
	"context"
	"fmt"
)

// Executor provides specialized provider runtime behavior.
type Executor interface {
	ProviderID() string
	Supports(kind string) bool
	ChatCompletion(ctx context.Context, req ChatRequest, model string) (ChatResponse, error)
	StreamChatCompletion(ctx context.Context, req ChatRequest, model string) (<-chan ChatChunk, error)
}

type executorBridge struct {
	name     string
	executor Executor
}

func NewExecutorBridge(name string, executor Executor) (Adapter, error) {
	if executor == nil {
		return nil, fmt.Errorf("nil executor")
	}
	return &executorBridge{name: name, executor: executor}, nil
}

func (b *executorBridge) Name() string {
	return b.name
}

func (b *executorBridge) ChatCompletion(ctx context.Context, request ChatRequest, model string) (ChatResponse, error) {
	return b.executor.ChatCompletion(ctx, request, model)
}

func (b *executorBridge) StreamChatCompletion(ctx context.Context, request ChatRequest, model string) (<-chan ChatChunk, error) {
	return b.executor.StreamChatCompletion(ctx, request, model)
}

func (b *executorBridge) GetUsage(context.Context) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

func (b *executorBridge) Embeddings(context.Context, EmbeddingsRequest, string) (EmbeddingsResponse, error) {
	return EmbeddingsResponse{}, &ProviderError{Provider: b.name, Type: ErrNonRetryable, Message: "embeddings not supported by executor bridge"}
}

func (b *executorBridge) AudioSpeech(context.Context, AudioSpeechRequest, string) (AudioSpeechResponse, error) {
	return AudioSpeechResponse{}, &ProviderError{Provider: b.name, Type: ErrNonRetryable, Message: "audio speech not supported by executor bridge"}
}

func (b *executorBridge) ImagesGenerations(context.Context, ImagesGenerationsRequest, string) (ImagesGenerationsResponse, error) {
	return ImagesGenerationsResponse{}, &ProviderError{Provider: b.name, Type: ErrNonRetryable, Message: "image generation not supported by executor bridge"}
}
