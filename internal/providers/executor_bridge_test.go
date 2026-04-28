package providers

import (
	"context"
	"testing"

	"github.com/edodoyokz/ai-go-router/internal/config"
)

type testExecutor struct{}

func (testExecutor) ProviderID() string { return "test" }
func (testExecutor) Supports(kind string) bool {
	return kind == "chat"
}
func (testExecutor) ChatCompletion(context.Context, ChatRequest, string) (ChatResponse, error) {
	return ChatResponse{ID: "ok", Object: "chat.completion", Choices: []ChatChoice{}}, nil
}
func (testExecutor) StreamChatCompletion(context.Context, ChatRequest, string) (<-chan ChatChunk, error) {
	ch := make(chan ChatChunk, 1)
	ch <- ChatChunk{ID: "chunk", Object: "chat.completion.chunk"}
	close(ch)
	return ch, nil
}

func TestExecutorBridge_ImplementsAdapter(t *testing.T) {
	adapter, err := NewExecutorBridge("bridge", testExecutor{})
	if err != nil {
		t.Fatalf("NewExecutorBridge() error = %v", err)
	}
	if adapter.Name() != "bridge" {
		t.Fatalf("unexpected bridge name: %s", adapter.Name())
	}

	resp, err := adapter.ChatCompletion(context.Background(), ChatRequest{}, "m")
	if err != nil {
		t.Fatalf("ChatCompletion() error = %v", err)
	}
	if resp.ID != "ok" {
		t.Fatalf("unexpected response id: %s", resp.ID)
	}

	if _, err := adapter.Embeddings(context.Background(), EmbeddingsRequest{}, "m"); err == nil {
		t.Fatalf("expected unsupported embeddings error")
	}
}

func TestBuildExecutor_Registry(t *testing.T) {
	RegisterExecutor("bridge-test", func(config.ProviderConfig, config.ErrorConfig) (Executor, error) {
		return testExecutor{}, nil
	})

	exec, err := BuildExecutor("bridge-test", config.ProviderConfig{}, config.ErrorConfig{})
	if err != nil {
		t.Fatalf("BuildExecutor() error = %v", err)
	}
	if exec.ProviderID() != "test" {
		t.Fatalf("unexpected executor id: %s", exec.ProviderID())
	}

	if _, err := BuildExecutor("missing-provider", config.ProviderConfig{}, config.ErrorConfig{}); err == nil {
		t.Fatalf("expected missing executor registration error")
	}
}
