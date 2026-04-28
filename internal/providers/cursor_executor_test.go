package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/edodoyokz/ai-go-router/internal/config"
)

func TestCursorExecutor_StreamChatCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/aiserver.v1.ChatService/StreamUnifiedChatWithTools" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("x-cursor-checksum") == "" || r.Header.Get("x-client-key") == "" {
			t.Fatalf("missing cursor native headers")
		}
		body, _ := io.ReadAll(r.Body)
		frame, ok := cursorParseConnectRPCFrame(body)
		if !ok || len(frame.Payload) == 0 || json.Valid(frame.Payload) {
			t.Fatalf("expected framed protobuf payload, ok=%v payload=%q", ok, string(frame.Payload))
		}
		payload := encodeProtoField(2, encodeProtoField(1, encodeProtoString("hello from cursor executor")))
		_, _ = w.Write(cursorWrapConnectRPCFrame(payload))
	}))
	defer server.Close()

	exec, err := NewCursorExecutor(config.ProviderConfig{
		Name:    "cursor",
		Type:    "cursor",
		BaseURL: server.URL,
		Accounts: []config.AccountConfig{{
			Name:        "cursor-account",
			AccessToken: "cursor-token",
			ProviderSpecificData: map[string]any{
				"machineId": "machine-123",
			},
		}},
	}, config.ErrorConfig{})
	if err != nil {
		t.Fatalf("NewCursorExecutor error: %v", err)
	}
	ch, err := exec.StreamChatCompletion(context.Background(), ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "hi"}}}, "cursor-model")
	if err != nil {
		t.Fatalf("StreamChatCompletion error: %v", err)
	}
	var got string
	for chunk := range ch {
		if len(chunk.Choices) == 0 {
			continue
		}
		if text, ok := chunk.Choices[0].Delta.Content.(string); ok {
			got += text
		}
	}
	if got != "hello from cursor executor" {
		t.Fatalf("got %q", got)
	}
}

func TestCursorExecutor_ChatCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := encodeProtoField(2, encodeProtoField(1, encodeProtoString("final cursor answer")))
		_, _ = w.Write(cursorWrapConnectRPCFrame(payload))
	}))
	defer server.Close()

	exec, err := NewCursorExecutor(config.ProviderConfig{
		Name:    "cursor",
		Type:    "cursor",
		BaseURL: server.URL,
		Accounts: []config.AccountConfig{{
			Name:        "cursor-account",
			AccessToken: "cursor-token",
			ProviderSpecificData: map[string]any{
				"machineId": "machine-123",
			},
		}},
	}, config.ErrorConfig{})
	if err != nil {
		t.Fatalf("NewCursorExecutor error: %v", err)
	}
	resp, err := exec.ChatCompletion(context.Background(), ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "hi"}}}, "cursor-model")
	if err != nil {
		t.Fatalf("ChatCompletion error: %v", err)
	}
	if resp.Choices[0].Message.Content != "final cursor answer" {
		encoded, _ := json.Marshal(resp)
		t.Fatalf("unexpected response: %s", string(encoded))
	}
}
