package translator

import (
	"context"
	"encoding/json"
	"testing"
)

func TestGeminiRequestTranslator_BasicText(t *testing.T) {
	tr := &geminiRequestTranslator{}
	body := map[string]interface{}{
		"model": "gemini-2.0-flash",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "Hello"},
		},
	}
	result, err := tr.TranslateRequest(context.Background(), FormatOpenAI, FormatGemini, body)
	if err != nil {
		t.Fatal(err)
	}
	contents, ok := result["contents"].([]map[string]interface{})
	if !ok || len(contents) == 0 {
		t.Fatal("expected contents array")
	}
	if contents[0]["role"] != "user" {
		t.Fatalf("expected role user, got %v", contents[0]["role"])
	}
	parts := contents[0]["parts"].([]map[string]interface{})
	if parts[0]["text"] != "Hello" {
		t.Fatalf("expected text Hello, got %v", parts[0]["text"])
	}
}

func TestGeminiRequestTranslator_AssistantRoleBecomesModel(t *testing.T) {
	tr := &geminiRequestTranslator{}
	body := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "ping"},
			map[string]interface{}{"role": "assistant", "content": "pong"},
		},
	}
	result, err := tr.TranslateRequest(context.Background(), FormatOpenAI, FormatGemini, body)
	if err != nil {
		t.Fatal(err)
	}
	contents := result["contents"].([]map[string]interface{})
	if contents[1]["role"] != "model" {
		t.Fatalf("assistant role should become model, got %v", contents[1]["role"])
	}
}

func TestGeminiRequestTranslator_GenerationConfig(t *testing.T) {
	tr := &geminiRequestTranslator{}
	body := map[string]interface{}{
		"temperature": 0.8,
		"top_p":       0.95,
		"max_tokens":  512,
		"stop":        []interface{}{"STOP"},
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hi"},
		},
	}
	result, err := tr.TranslateRequest(context.Background(), FormatOpenAI, FormatGemini, body)
	if err != nil {
		t.Fatal(err)
	}
	cfg, ok := result["generationConfig"].(map[string]interface{})
	if !ok {
		t.Fatal("expected generationConfig")
	}
	if cfg["temperature"] != 0.8 {
		t.Fatalf("temperature = %v, want 0.8", cfg["temperature"])
	}
	if cfg["topP"] != 0.95 {
		t.Fatalf("topP = %v, want 0.95", cfg["topP"])
	}
	if cfg["maxOutputTokens"] != 512 {
		t.Fatalf("maxOutputTokens = %v, want 512", cfg["maxOutputTokens"])
	}
	stops, _ := cfg["stopSequences"].([]interface{})
	if len(stops) != 1 {
		t.Fatalf("stopSequences length = %d, want 1", len(stops))
	}
}

func TestGeminiRequestTranslator_NoGenerationConfigIfEmpty(t *testing.T) {
	tr := &geminiRequestTranslator{}
	body := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hi"},
		},
	}
	result, err := tr.TranslateRequest(context.Background(), FormatOpenAI, FormatGemini, body)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := result["generationConfig"]; ok {
		t.Fatal("should not include generationConfig when empty")
	}
}

func TestGeminiResponseTranslator_BasicText(t *testing.T) {
	tr := &geminiResponseTranslator{}
	body := []byte(`{
		"candidates": [{
			"content": {"parts": [{"text": "Hello back"}]},
			"finishReason": "STOP"
		}],
		"usageMetadata": {"promptTokenCount": 5, "candidatesTokenCount": 3, "totalTokenCount": 8}
	}`)
	result, err := tr.TranslateResponse(context.Background(), FormatGemini, FormatOpenAI, body)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(result, &decoded); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if decoded["object"] != "chat.completion" {
		t.Fatalf("object = %v, want chat.completion", decoded["object"])
	}
	choices := decoded["choices"].([]interface{})
	msg := choices[0].(map[string]interface{})["message"].(map[string]interface{})
	if msg["content"] != "Hello back" {
		t.Fatalf("content = %v, want Hello back", msg["content"])
	}
	if choices[0].(map[string]interface{})["finish_reason"] != "stop" {
		t.Fatalf("finish_reason = %v, want stop", choices[0].(map[string]interface{})["finish_reason"])
	}
	usage := decoded["usage"].(map[string]interface{})
	if usage["prompt_tokens"] != float64(5) {
		t.Fatalf("prompt_tokens = %v, want 5", usage["prompt_tokens"])
	}
}

func TestGeminiResponseTranslator_MaxTokensFinishReason(t *testing.T) {
	tr := &geminiResponseTranslator{}
	body := []byte(`{"candidates": [{"content": {"parts": [{"text": "..."}]}, "finishReason": "MAX_TOKENS"}]}`)
	result, err := tr.TranslateResponse(context.Background(), FormatGemini, FormatOpenAI, body)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(result, &decoded); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	choices := decoded["choices"].([]interface{})
	if choices[0].(map[string]interface{})["finish_reason"] != "length" {
		t.Fatalf("MAX_TOKENS should map to length")
	}
}

func TestGeminiTranslator_Registered(t *testing.T) {
	r := NewRegistry()
	raw, _ := json.Marshal(map[string]interface{}{
		"model":    "gemini-2.0-flash",
		"messages": []interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
	})
	out, err := r.TranslateRequestJSON(context.Background(), FormatOpenAI, FormatGemini, raw)
	if err != nil {
		t.Fatalf("TranslateRequestJSON: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["contents"]; !ok {
		t.Fatal("expected contents in gemini format")
	}
}
