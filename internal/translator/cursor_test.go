package translator

import (
	"context"
	"encoding/json"
	"testing"
)

func TestCursorTranslator_SystemMessage(t *testing.T) {
	tr := &cursorRequestTranslator{}
	body := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{"role": "system", "content": "You are helpful."},
			map[string]interface{}{"role": "user", "content": "Hello"},
		},
	}
	result, err := tr.TranslateRequest(context.Background(), FormatOpenAI, FormatCursor, body)
	if err != nil {
		t.Fatal(err)
	}
	msgs := result["messages"].([]interface{})
	if len(msgs) < 1 {
		t.Fatal("expected messages")
	}
	first := msgs[0].(map[string]interface{})
	if first["role"] != "user" {
		t.Fatalf("system should become user, got %q", first["role"])
	}
	content := first["content"].(string)
	if content == "" || len(content) < 5 {
		t.Fatalf("system content should be wrapped, got %q", content)
	}
	// Should be prefixed with [System Instructions]
	if content[:20] != "[System Instructions" {
		t.Fatalf("expected system instructions prefix, got %q", content)
	}
}

func TestCursorTranslator_ToolResult(t *testing.T) {
	tr := &cursorRequestTranslator{}
	body := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{
				"role": "assistant",
				"tool_calls": []interface{}{
					map[string]interface{}{
						"id":   "call_abc",
						"type": "function",
						"function": map[string]interface{}{
							"name":      "read_file",
							"arguments": `{"path":"foo.go"}`,
						},
					},
				},
				"content": "",
			},
			map[string]interface{}{
				"role":         "tool",
				"tool_call_id": "call_abc",
				"content":      "package main\nfunc main() {}",
			},
		},
	}
	result, err := tr.TranslateRequest(context.Background(), FormatOpenAI, FormatCursor, body)
	if err != nil {
		t.Fatal(err)
	}
	msgs := result["messages"].([]interface{})
	// Find the tool result message
	var found bool
	for _, rawMsg := range msgs {
		msg := rawMsg.(map[string]interface{})
		if msg["role"] == "user" {
			content, _ := msg["content"].(string)
			if len(content) > 0 && len(content) >= 12 && content[:12] == "<tool_result" {
				found = true
				if !containsString(content, "<tool_name>read_file</tool_name>") {
					t.Fatalf("expected tool name in block, got: %s", content)
				}
			}
		}
	}
	if !found {
		t.Fatal("expected tool_result XML block in user message")
	}
}

func TestCursorTranslator_StripForbiddenFields(t *testing.T) {
	tr := &cursorRequestTranslator{}
	body := map[string]interface{}{
		"model":          "claude-4",
		"user":           "user123",
		"metadata":       map[string]interface{}{"key": "val"},
		"tool_choice":    "auto",
		"stream_options": map[string]interface{}{"include_usage": true},
		"system":         "sys",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hi"},
		},
	}
	result, err := tr.TranslateRequest(context.Background(), FormatOpenAI, FormatCursor, body)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"user", "metadata", "tool_choice", "stream_options", "system"} {
		if _, ok := result[forbidden]; ok {
			t.Fatalf("field %q should be stripped from cursor request", forbidden)
		}
	}
	if result["max_tokens"] != 32000 {
		t.Fatalf("max_tokens should be forced to 32000, got %v", result["max_tokens"])
	}
}

func TestCursorTranslator_Registered(t *testing.T) {
	r := NewRegistry()
	body := map[string]interface{}{
		"model": "gpt-4",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hello"},
		},
	}
	rawBody, _ := json.Marshal(body)
	out, err := r.TranslateRequestJSON(context.Background(), FormatOpenAI, FormatCursor, rawBody)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatal("expected non-empty translated body")
	}
}

func containsString(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > len(sub) && stringContains(s, sub))
}

func stringContains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
