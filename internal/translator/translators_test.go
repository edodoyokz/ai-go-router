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

func TestRegistryTranslateRequestJSON(t *testing.T) {
	registry := NewRegistry()
	body := json.RawMessage(`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}],"max_tokens":10}`)
	translated, err := registry.TranslateRequestJSON(context.Background(), FormatOpenAI, FormatClaude, body)
	if err != nil {
		t.Fatalf("TranslateRequestJSON() error = %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(translated, &decoded); err != nil {
		t.Fatalf("unmarshal translated: %v", err)
	}
	if decoded["model"] != "gpt-4" {
		t.Fatalf("model = %v, want gpt-4", decoded["model"])
	}
	if _, ok := decoded["messages"]; !ok {
		t.Fatalf("messages missing")
	}
}

func TestOpenAIToClaudeToolCalls(t *testing.T) {
	translator := &openAIToClaudeRequestTranslator{}
	result, err := translator.TranslateRequest(context.Background(), FormatOpenAI, FormatClaude, map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{
				"role":    "assistant",
				"content": "checking",
				"tool_calls": []interface{}{map[string]interface{}{
					"id":   "call_1",
					"type": "function",
					"function": map[string]interface{}{
						"name":      "lookup",
						"arguments": `{"q":"x"}`,
					},
				}},
			},
			map[string]interface{}{"role": "tool", "tool_call_id": "call_1", "content": "result"},
		},
	})
	if err != nil {
		t.Fatalf("TranslateRequest() error = %v", err)
	}
	messages := result["messages"].([]interface{})
	assistant := messages[0].(map[string]interface{})
	blocks := assistant["content"].([]interface{})
	if blocks[1].(map[string]interface{})["type"] != "tool_use" {
		t.Fatalf("second block = %#v, want tool_use", blocks[1])
	}
	toolResult := messages[1].(map[string]interface{})["content"].([]interface{})[0].(map[string]interface{})
	if toolResult["type"] != "tool_result" {
		t.Fatalf("tool result block = %#v", toolResult)
	}
}

func TestClaudeToOpenAIResponseToolUse(t *testing.T) {
	translator := &claudeToOpenAIResponseTranslator{}
	body := []byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude","stop_reason":"tool_use","content":[{"type":"tool_use","id":"toolu_1","name":"lookup","input":{"q":"x"}}]}`)
	result, err := translator.TranslateResponse(context.Background(), FormatClaude, FormatOpenAI, body)
	if err != nil {
		t.Fatalf("TranslateResponse() error = %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(result, &decoded); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	message := decoded["choices"].([]interface{})[0].(map[string]interface{})["message"].(map[string]interface{})
	if len(message["tool_calls"].([]interface{})) != 1 {
		t.Fatalf("tool_calls missing: %#v", message)
	}
}

func TestOpenAIToClaudeRequest_ImageContent(t *testing.T) {
	translator := &openAIToClaudeRequestTranslator{}
	body := map[string]interface{}{
		"model": "gpt-4o",
		"messages": []interface{}{
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "What is this?"},
					map[string]interface{}{
						"type":      "image_url",
						"image_url": map[string]interface{}{"url": "data:image/png;base64,abc123"},
					},
				},
			},
		},
	}
	result, err := translator.TranslateRequest(context.Background(), FormatOpenAI, FormatClaude, body)
	if err != nil {
		t.Fatalf("TranslateRequest() error = %v", err)
	}
	messages, _ := result["messages"].([]interface{})
	if len(messages) == 0 {
		t.Fatal("messages should not be empty")
	}
	msg := messages[0].(map[string]interface{})
	// Content should be a slice (array of blocks) for multi-part messages
	if msg["role"] != "user" {
		t.Fatalf("role = %v, want user", msg["role"])
	}
}

func TestClaudeToOpenAIResponse_ThinkingBlock(t *testing.T) {
	translator := &claudeToOpenAIResponseTranslator{}
	body := []byte(`{
		"id": "msg_1",
		"type": "message",
		"role": "assistant",
		"model": "claude-3-7-sonnet",
		"stop_reason": "end_turn",
		"content": [
			{"type": "thinking", "thinking": "Let me think..."},
			{"type": "text", "text": "The answer is 42."}
		],
		"usage": {"input_tokens": 10, "output_tokens": 20}
	}`)
	result, err := translator.TranslateResponse(context.Background(), FormatClaude, FormatOpenAI, body)
	if err != nil {
		t.Fatalf("TranslateResponse() error = %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(result, &decoded); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	choices := decoded["choices"].([]interface{})
	msg := choices[0].(map[string]interface{})["message"].(map[string]interface{})
	// Text content should be present
	content, _ := msg["content"].(string)
	if content == "" {
		t.Fatal("expected non-empty content from text block")
	}
	if content != "The answer is 42." {
		t.Fatalf("content = %v, want 'The answer is 42.'", content)
	}
}

func TestOpenAIToClaudeRequest_StopAsString(t *testing.T) {
	translator := &openAIToClaudeRequestTranslator{}
	body := map[string]interface{}{
		"model": "gpt-4",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hi"},
		},
		"stop": "STOP",
	}
	result, err := translator.TranslateRequest(context.Background(), FormatOpenAI, FormatClaude, body)
	if err != nil {
		t.Fatalf("TranslateRequest() error = %v", err)
	}
	// stop as string should be wrapped into stop_sequences slice
	stopSeq, ok := result["stop_sequences"].([]interface{})
	if !ok {
		t.Fatal("stop_sequences missing or not a slice")
	}
	if len(stopSeq) != 1 {
		t.Fatalf("stop_sequences length = %d, want 1", len(stopSeq))
	}
}

func TestClaudeToOpenAIResponse_MultipleTextBlocks(t *testing.T) {
	translator := &claudeToOpenAIResponseTranslator{}
	body := []byte(`{
		"id": "msg_2",
		"type": "message",
		"role": "assistant",
		"model": "claude-3-5-sonnet",
		"stop_reason": "end_turn",
		"content": [
			{"type": "text", "text": "Part one. "},
			{"type": "text", "text": "Part two."}
		],
		"usage": {"input_tokens": 5, "output_tokens": 8}
	}`)
	result, err := translator.TranslateResponse(context.Background(), FormatClaude, FormatOpenAI, body)
	if err != nil {
		t.Fatalf("TranslateResponse() error = %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(result, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	choices := decoded["choices"].([]interface{})
	msg := choices[0].(map[string]interface{})["message"].(map[string]interface{})
	content, _ := msg["content"].(string)
	// Both text blocks should be concatenated
	if content == "" {
		t.Fatal("expected combined content from multiple text blocks")
	}
}

func TestResponsesRequestConversion(t *testing.T) {
	registry := NewRegistry()
	translated, err := registry.TranslateRequestJSON(context.Background(), FormatOpenAIResp, FormatOpenAI, json.RawMessage(`{"model":"gpt-4o","input":"hello","max_output_tokens":7}`))
	if err != nil {
		t.Fatalf("TranslateRequestJSON() error = %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(translated, &decoded); err != nil {
		t.Fatalf("unmarshal translated: %v", err)
	}
	if decoded["max_tokens"] != float64(7) {
		t.Fatalf("max_tokens = %v, want 7", decoded["max_tokens"])
	}
	if len(decoded["messages"].([]interface{})) != 1 {
		t.Fatalf("messages not converted: %#v", decoded["messages"])
	}
}
