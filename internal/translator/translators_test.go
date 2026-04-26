package translator

import (
	"context"
	"encoding/json"
	"testing"
)

func TestClaudeToOpenAIRequestTranslator(t *testing.T) {
	translator := &claudeToOpenAIRequestTranslator{}

	tests := []struct {
		name    string
		body    map[string]interface{}
		wantErr bool
		check   func(t *testing.T, result map[string]interface{})
	}{
		{
			name: "basic request translation",
			body: map[string]interface{}{
				"model": "claude-3-opus",
				"messages": []interface{}{
					map[string]interface{}{
						"role":    "user",
						"content": "Hello",
					},
				},
				"max_tokens": 4096,
			},
			wantErr: false,
			check: func(t *testing.T, result map[string]interface{}) {
				if result["model"] != "claude-3-opus" {
					t.Errorf("model = %v, want claude-3-opus", result["model"])
				}
				if result["max_tokens"] != 4096 {
					t.Errorf("max_tokens = %v, want 4096", result["max_tokens"])
				}
			},
		},
		{
			name: "system message translation",
			body: map[string]interface{}{
				"model":  "claude-3-opus",
				"system": "You are a helpful assistant",
				"messages": []interface{}{
					map[string]interface{}{
						"role":    "user",
						"content": "Hello",
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result map[string]interface{}) {
				messages, ok := result["messages"].([]interface{})
				if !ok || len(messages) == 0 {
					t.Fatal("messages missing or empty")
				}
				firstMsg := messages[0].(map[string]interface{})
				if firstMsg["role"] != "system" {
					t.Errorf("first message role = %v, want system", firstMsg["role"])
				}
				if firstMsg["content"] != "You are a helpful assistant" {
					t.Errorf("first message content = %v, want 'You are a helpful assistant'", firstMsg["content"])
				}
			},
		},
		{
			name: "stop_sequences to stop",
			body: map[string]interface{}{
				"model": "claude-3-opus",
				"messages": []interface{}{
					map[string]interface{}{
						"role":    "user",
						"content": "Hello",
					},
				},
				"stop_sequences": []interface{}{"STOP", "END"},
			},
			wantErr: false,
			check: func(t *testing.T, result map[string]interface{}) {
				stop, ok := result["stop"].([]interface{})
				if !ok {
					t.Fatal("stop missing or not a slice")
				}
				if len(stop) != 2 {
					t.Errorf("stop length = %d, want 2", len(stop))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := translator.TranslateRequest(context.Background(), FormatClaude, FormatOpenAI, tt.body)
			if (err != nil) != tt.wantErr {
				t.Errorf("TranslateRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}

func TestOpenAIToClaudeRequestTranslator(t *testing.T) {
	translator := &openAIToClaudeRequestTranslator{}

	tests := []struct {
		name    string
		body    map[string]interface{}
		wantErr bool
		check   func(t *testing.T, result map[string]interface{})
	}{
		{
			name: "basic request translation",
			body: map[string]interface{}{
				"model": "gpt-4",
				"messages": []interface{}{
					map[string]interface{}{
						"role":    "user",
						"content": "Hello",
					},
				},
				"max_tokens": 4096,
			},
			wantErr: false,
			check: func(t *testing.T, result map[string]interface{}) {
				if result["model"] != "gpt-4" {
					t.Errorf("model = %v, want gpt-4", result["model"])
				}
				if result["max_tokens"] != 4096 {
					t.Errorf("max_tokens = %v, want 4096", result["max_tokens"])
				}
			},
		},
		{
			name: "system message extraction",
			body: map[string]interface{}{
				"model": "gpt-4",
				"messages": []interface{}{
					map[string]interface{}{
						"role":    "system",
						"content": "You are a helpful assistant",
					},
					map[string]interface{}{
						"role":    "user",
						"content": "Hello",
					},
				},
			},
			wantErr: false,
			check: func(t *testing.T, result map[string]interface{}) {
				if result["system"] != "You are a helpful assistant" {
					t.Errorf("system = %v, want 'You are a helpful assistant'", result["system"])
				}
				messages, ok := result["messages"].([]interface{})
				if !ok || len(messages) != 1 {
					t.Fatal("messages should have 1 message after extracting system")
				}
			},
		},
		{
			name: "stop to stop_sequences",
			body: map[string]interface{}{
				"model": "gpt-4",
				"messages": []interface{}{
					map[string]interface{}{
						"role":    "user",
						"content": "Hello",
					},
				},
				"stop": []interface{}{"STOP", "END"},
			},
			wantErr: false,
			check: func(t *testing.T, result map[string]interface{}) {
				stopSeq, ok := result["stop_sequences"].([]interface{})
				if !ok {
					t.Fatal("stop_sequences missing or not a slice")
				}
				if len(stopSeq) != 2 {
					t.Errorf("stop_sequences length = %d, want 2", len(stopSeq))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := translator.TranslateRequest(context.Background(), FormatOpenAI, FormatClaude, tt.body)
			if (err != nil) != tt.wantErr {
				t.Errorf("TranslateRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}

func TestClaudeToOpenAIResponseTranslator(t *testing.T) {
	translator := &claudeToOpenAIResponseTranslator{}

	claudeResponse := `{
		"id": "msg-123",
		"type": "message",
		"role": "assistant",
		"content": [
			{
				"type": "text",
				"text": "Hello, how can I help you?"
			}
		],
		"model": "claude-3-opus",
		"stop_reason": "end_turn",
		"usage": {
			"input_tokens": 10,
			"output_tokens": 20
		}
	}`

	result, err := translator.TranslateResponse(context.Background(), FormatClaude, FormatOpenAI, []byte(claudeResponse))
	if err != nil {
		t.Fatalf("TranslateResponse() error = %v", err)
	}

	// Parse result to verify structure
	var openAIResp map[string]interface{}
	if err := json.Unmarshal(result, &openAIResp); err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	if openAIResp["id"] != "msg-123" {
		t.Errorf("id = %v, want msg-123", openAIResp["id"])
	}
	if openAIResp["object"] != "chat.completion" {
		t.Errorf("object = %v, want chat.completion", openAIResp["object"])
	}
	if openAIResp["model"] != "claude-3-opus" {
		t.Errorf("model = %v, want claude-3-opus", openAIResp["model"])
	}

	choices, ok := openAIResp["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		t.Fatal("choices missing or empty")
	}

	choice := choices[0].(map[string]interface{})
	if choice["finish_reason"] != "stop" {
		t.Errorf("finish_reason = %v, want stop", choice["finish_reason"])
	}

	usage, ok := openAIResp["usage"].(map[string]interface{})
	if !ok {
		t.Fatal("usage missing")
	}
	if usage["prompt_tokens"] != float64(10) {
		t.Errorf("prompt_tokens = %v, want 10", usage["prompt_tokens"])
	}
	if usage["completion_tokens"] != float64(20) {
		t.Errorf("completion_tokens = %v, want 20", usage["completion_tokens"])
	}
}

func TestOpenAIToClaudeResponseTranslator(t *testing.T) {
	translator := &openAIToClaudeResponseTranslator{}

	openAIResponse := `{
		"id": "chatcmpl-123",
		"object": "chat.completion",
		"created": 1234567890,
		"model": "gpt-4",
		"choices": [
			{
				"index": 0,
				"message": {
					"role": "assistant",
					"content": "Hello, how can I help you?"
				},
				"finish_reason": "stop"
			}
		],
		"usage": {
			"prompt_tokens": 10,
			"completion_tokens": 20,
			"total_tokens": 30
		}
	}`

	result, err := translator.TranslateResponse(context.Background(), FormatOpenAI, FormatClaude, []byte(openAIResponse))
	if err != nil {
		t.Fatalf("TranslateResponse() error = %v", err)
	}

	// Parse result to verify structure
	var claudeResp map[string]interface{}
	if err := json.Unmarshal(result, &claudeResp); err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	if claudeResp["id"] != "chatcmpl-123" {
		t.Errorf("id = %v, want chatcmpl-123", claudeResp["id"])
	}
	if claudeResp["type"] != "message" {
		t.Errorf("type = %v, want message", claudeResp["type"])
	}
	if claudeResp["role"] != "assistant" {
		t.Errorf("role = %v, want assistant", claudeResp["role"])
	}
	if claudeResp["model"] != "gpt-4" {
		t.Errorf("model = %v, want gpt-4", claudeResp["model"])
	}
	if claudeResp["stop_reason"] != "end_turn" {
		t.Errorf("stop_reason = %v, want end_turn", claudeResp["stop_reason"])
	}

	usage, ok := claudeResp["usage"].(map[string]interface{})
	if !ok {
		t.Fatal("usage missing")
	}
	if usage["input_tokens"] != float64(10) {
		t.Errorf("input_tokens = %v, want 10", usage["input_tokens"])
	}
	if usage["output_tokens"] != float64(20) {
		t.Errorf("output_tokens = %v, want 20", usage["output_tokens"])
	}
}

func TestRegistry(t *testing.T) {
	registry := NewRegistry()

	t.Run("get request translator", func(t *testing.T) {
		translator, err := registry.GetRequestTranslator(FormatOpenAI, FormatClaude)
		if err != nil {
			t.Errorf("GetRequestTranslator() error = %v", err)
		}
		if translator == nil {
			t.Error("translator is nil")
		}
	})

	t.Run("get response translator", func(t *testing.T) {
		translator, err := registry.GetResponseTranslator(FormatClaude, FormatOpenAI)
		if err != nil {
			t.Errorf("GetResponseTranslator() error = %v", err)
		}
		if translator == nil {
			t.Error("translator is nil")
		}
	})

	t.Run("passthrough for same format", func(t *testing.T) {
		body := map[string]interface{}{"test": "value"}
		translator, err := registry.GetRequestTranslator(FormatOpenAI, FormatOpenAI)
		if err != nil {
			t.Errorf("GetRequestTranslator() error = %v", err)
		}
		result, err := translator.TranslateRequest(context.Background(), FormatOpenAI, FormatOpenAI, body)
		if err != nil {
			t.Errorf("TranslateRequest() error = %v", err)
		}
		if result["test"] != "value" {
			t.Errorf("passthrough modified body")
		}
	})

	t.Run("unknown translator", func(t *testing.T) {
		_, err := registry.GetRequestTranslator("unknown", FormatOpenAI)
		if err == nil {
			t.Error("expected error for unknown source format")
		}
	})
}
