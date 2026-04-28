package translator

import (
	"context"
	"testing"
)

func TestKiroTranslator_BasicConversion(t *testing.T) {
	tr := &kiroRequestTranslator{}
	body := map[string]interface{}{
		"model": "claude-sonnet-4-5",
		"messages": []interface{}{
			map[string]interface{}{"role": "system", "content": "Be helpful."},
			map[string]interface{}{"role": "user", "content": "What is Go?"},
		},
	}
	result, err := tr.TranslateRequest(context.Background(), FormatOpenAI, FormatKiro, body)
	if err != nil {
		t.Fatal(err)
	}
	cs, ok := result["conversationState"].(map[string]interface{})
	if !ok {
		t.Fatal("expected conversationState")
	}
	if cs["chatTriggerType"] != "MANUAL" {
		t.Fatalf("expected MANUAL, got %v", cs["chatTriggerType"])
	}
	cm, ok := cs["currentMessage"].(map[string]interface{})
	if !ok {
		t.Fatal("expected currentMessage")
	}
	uim, ok := cm["userInputMessage"].(map[string]interface{})
	if !ok {
		t.Fatal("expected userInputMessage")
	}
	content, _ := uim["content"].(string)
	if content == "" {
		t.Fatal("expected non-empty content")
	}
}

func TestKiroTranslator_ToolsInjected(t *testing.T) {
	tr := &kiroRequestTranslator{}
	body := map[string]interface{}{
		"model": "some-model",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "call a tool"},
		},
		"tools": []interface{}{
			map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name":        "read_file",
					"description": "Reads a file",
					"parameters": map[string]interface{}{
						"type":       "object",
						"properties": map[string]interface{}{"path": map[string]interface{}{"type": "string"}},
						"required":   []interface{}{"path"},
					},
				},
			},
		},
	}
	result, err := tr.TranslateRequest(context.Background(), FormatOpenAI, FormatKiro, body)
	if err != nil {
		t.Fatal(err)
	}
	cs := result["conversationState"].(map[string]interface{})
	cm := cs["currentMessage"].(map[string]interface{})
	uim := cm["userInputMessage"].(map[string]interface{})
	ctx2, ok := uim["userInputMessageContext"].(map[string]interface{})
	if !ok {
		t.Fatal("expected userInputMessageContext with tools")
	}
	tools, ok := ctx2["tools"].([]interface{})
	if !ok || len(tools) == 0 {
		t.Fatal("expected tools in context")
	}
	toolSpec := tools[0].(map[string]interface{})["toolSpecification"].(map[string]interface{})
	if toolSpec["name"] != "read_file" {
		t.Fatalf("expected tool name read_file, got %v", toolSpec["name"])
	}
}

func TestKiroTranslator_InferenceConfig(t *testing.T) {
	tr := &kiroRequestTranslator{}
	body := map[string]interface{}{
		"model":       "claude-sonnet-4-5",
		"temperature": 0.7,
		"top_p":       0.9,
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hi"},
		},
	}
	result, err := tr.TranslateRequest(context.Background(), FormatOpenAI, FormatKiro, body)
	if err != nil {
		t.Fatal(err)
	}
	cfg, ok := result["inferenceConfig"].(map[string]interface{})
	if !ok {
		t.Fatal("expected inferenceConfig")
	}
	if cfg["temperature"] != 0.7 {
		t.Fatalf("expected temperature 0.7, got %v", cfg["temperature"])
	}
	if cfg["topP"] != 0.9 {
		t.Fatalf("expected topP 0.9, got %v", cfg["topP"])
	}
	if cfg["maxTokens"] != 32000 {
		t.Fatalf("expected maxTokens 32000, got %v", cfg["maxTokens"])
	}
}

func TestKiroTranslator_MultiTurnHistory(t *testing.T) {
	tr := &kiroRequestTranslator{}
	body := map[string]interface{}{
		"model": "some-model",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "turn1"},
			map[string]interface{}{"role": "assistant", "content": "reply1"},
			map[string]interface{}{"role": "user", "content": "turn2"},
		},
	}
	result, err := tr.TranslateRequest(context.Background(), FormatOpenAI, FormatKiro, body)
	if err != nil {
		t.Fatal(err)
	}
	cs := result["conversationState"].(map[string]interface{})
	history, ok := cs["history"].([]interface{})
	if !ok {
		t.Fatal("expected history array")
	}
	if len(history) < 2 {
		t.Fatalf("expected at least 2 history turns, got %d", len(history))
	}
}
