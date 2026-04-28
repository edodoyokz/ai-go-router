package translator

import (
	"context"
	"encoding/json"
	"testing"
)

func TestOllamaRequestTranslator_BasicFields(t *testing.T) {
	tr := &ollamaRequestTranslator{}
	body := map[string]interface{}{
		"model": "llama3",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "Hello"},
		},
	}
	result, err := tr.TranslateRequest(context.Background(), FormatOpenAI, FormatOllama, body)
	if err != nil {
		t.Fatal(err)
	}
	if result["model"] != "llama3" {
		t.Fatalf("model = %v, want llama3", result["model"])
	}
	msgs, ok := result["messages"].([]interface{})
	if !ok || len(msgs) == 0 {
		t.Fatal("expected messages")
	}
}

func TestOllamaRequestTranslator_Options(t *testing.T) {
	tr := &ollamaRequestTranslator{}
	body := map[string]interface{}{
		"model":       "llama3",
		"temperature": 0.7,
		"top_p":       0.9,
		"max_tokens":  256,
		"stop":        []interface{}{"END"},
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hi"},
		},
	}
	result, err := tr.TranslateRequest(context.Background(), FormatOpenAI, FormatOllama, body)
	if err != nil {
		t.Fatal(err)
	}
	opts, ok := result["options"].(map[string]interface{})
	if !ok {
		t.Fatal("expected options map")
	}
	if opts["temperature"] != 0.7 {
		t.Fatalf("temperature = %v, want 0.7", opts["temperature"])
	}
	if opts["top_p"] != 0.9 {
		t.Fatalf("top_p = %v, want 0.9", opts["top_p"])
	}
	if opts["num_predict"] != 256 {
		t.Fatalf("num_predict = %v, want 256 (from max_tokens)", opts["num_predict"])
	}
	stops, _ := opts["stop"].([]interface{})
	if len(stops) != 1 {
		t.Fatalf("stop length = %d, want 1", len(stops))
	}
}

func TestOllamaRequestTranslator_StreamDefault(t *testing.T) {
	tr := &ollamaRequestTranslator{}
	body := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hi"},
		},
	}
	result, err := tr.TranslateRequest(context.Background(), FormatOpenAI, FormatOllama, body)
	if err != nil {
		t.Fatal(err)
	}
	if result["stream"] != false {
		t.Fatalf("stream default should be false, got %v", result["stream"])
	}
}

func TestOllamaRequestTranslator_StreamTrue(t *testing.T) {
	tr := &ollamaRequestTranslator{}
	body := map[string]interface{}{
		"stream": true,
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hi"},
		},
	}
	result, err := tr.TranslateRequest(context.Background(), FormatOpenAI, FormatOllama, body)
	if err != nil {
		t.Fatal(err)
	}
	if result["stream"] != true {
		t.Fatalf("stream should be true, got %v", result["stream"])
	}
}

func TestOllamaResponseTranslator_BasicMessage(t *testing.T) {
	tr := &ollamaResponseTranslator{}
	body := []byte(`{
		"model": "llama3",
		"message": {"role": "assistant", "content": "Hello there"},
		"done_reason": "stop",
		"prompt_eval_count": 10,
		"eval_count": 20
	}`)
	result, err := tr.TranslateResponse(context.Background(), FormatOllama, FormatOpenAI, body)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(result, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded["object"] != "chat.completion" {
		t.Fatalf("object = %v, want chat.completion", decoded["object"])
	}
	choices := decoded["choices"].([]interface{})
	msg := choices[0].(map[string]interface{})["message"].(map[string]interface{})
	if msg["content"] != "Hello there" {
		t.Fatalf("content = %v, want Hello there", msg["content"])
	}
	if choices[0].(map[string]interface{})["finish_reason"] != "stop" {
		t.Fatalf("finish_reason = %v, want stop", choices[0].(map[string]interface{})["finish_reason"])
	}
}

func TestOllamaResponseTranslator_UsageTokens(t *testing.T) {
	tr := &ollamaResponseTranslator{}
	body := []byte(`{
		"model": "llama3",
		"message": {"role": "assistant", "content": "hi"},
		"done_reason": "stop",
		"prompt_eval_count": 7,
		"eval_count": 13
	}`)
	result, err := tr.TranslateResponse(context.Background(), FormatOllama, FormatOpenAI, body)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(result, &decoded); err != nil {
		t.Fatal(err)
	}
	usage := decoded["usage"].(map[string]interface{})
	if usage["prompt_tokens"] != float64(7) {
		t.Fatalf("prompt_tokens = %v, want 7", usage["prompt_tokens"])
	}
	if usage["completion_tokens"] != float64(13) {
		t.Fatalf("completion_tokens = %v, want 13", usage["completion_tokens"])
	}
	if usage["total_tokens"] != float64(20) {
		t.Fatalf("total_tokens = %v, want 20", usage["total_tokens"])
	}
}

func TestOllamaResponseTranslator_LengthFinishReason(t *testing.T) {
	tr := &ollamaResponseTranslator{}
	body := []byte(`{"model":"llama3","message":{"role":"assistant","content":"..."},"done_reason":"length"}`)
	result, err := tr.TranslateResponse(context.Background(), FormatOllama, FormatOpenAI, body)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(result, &decoded); err != nil {
		t.Fatal(err)
	}
	choices := decoded["choices"].([]interface{})
	if choices[0].(map[string]interface{})["finish_reason"] != "length" {
		t.Fatal("done_reason=length should map to finish_reason=length")
	}
}

func TestOllamaTranslator_Registered(t *testing.T) {
	r := NewRegistry()
	raw, _ := json.Marshal(map[string]interface{}{
		"model":    "llama3",
		"messages": []interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
	})
	out, err := r.TranslateRequestJSON(context.Background(), FormatOpenAI, FormatOllama, raw)
	if err != nil {
		t.Fatalf("TranslateRequestJSON: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["messages"]; !ok {
		t.Fatal("expected messages in ollama format")
	}
	// Ollama should default stream to false
	if decoded["stream"] != false {
		t.Fatalf("stream default = %v, want false", decoded["stream"])
	}
}
