package translator

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// FormatOllama is the native Ollama /api/chat format.
const FormatOllama = "ollama"

// ollamaRequestTranslator converts OpenAI Chat Completions format → Ollama /api/chat format.
type ollamaRequestTranslator struct{}

func (t *ollamaRequestTranslator) TranslateRequest(_ context.Context, _, _ string, body map[string]interface{}) (map[string]interface{}, error) {
	result := map[string]interface{}{}

	if model, ok := body["model"]; ok {
		result["model"] = model
	}

	// Ollama uses the same messages array structure as OpenAI
	if messages, ok := body["messages"]; ok {
		result["messages"] = messages
	}

	// Map options
	options := map[string]interface{}{}
	if temp, ok := body["temperature"]; ok {
		options["temperature"] = temp
	}
	if topP, ok := body["top_p"]; ok {
		options["top_p"] = topP
	}
	if maxTokens, ok := body["max_tokens"]; ok {
		options["num_predict"] = maxTokens
	}
	if stop, ok := body["stop"]; ok {
		options["stop"] = stop
	}
	if len(options) > 0 {
		result["options"] = options
	}

	// Ollama streaming flag
	if stream, ok := body["stream"]; ok {
		result["stream"] = stream
	} else {
		result["stream"] = false
	}

	return result, nil
}

// ollamaResponseTranslator converts Ollama /api/chat response → OpenAI Chat Completions format.
type ollamaResponseTranslator struct{}

func (t *ollamaResponseTranslator) TranslateResponse(_ context.Context, _, _ string, body []byte) ([]byte, error) {
	var ollamaResp map[string]interface{}
	if err := json.Unmarshal(body, &ollamaResp); err != nil {
		return nil, fmt.Errorf("ollama translator: unmarshal response: %w", err)
	}

	text := ""
	finishReason := "stop"

	if msg, ok := ollamaResp["message"].(map[string]interface{}); ok {
		text, _ = msg["content"].(string)
	}
	if dr, ok := ollamaResp["done_reason"].(string); ok {
		switch dr {
		case "stop":
			finishReason = "stop"
		case "length":
			finishReason = "length"
		default:
			finishReason = "stop"
		}
	}

	// Usage
	usage := map[string]interface{}{}
	if pt, ok := ollamaResp["prompt_eval_count"]; ok {
		usage["prompt_tokens"] = pt
	}
	if ct, ok := ollamaResp["eval_count"]; ok {
		usage["completion_tokens"] = ct
	}

	model, _ := ollamaResp["model"].(string)

	openaiResp := map[string]interface{}{
		"id":      fmt.Sprintf("ollama-%d", time.Now().UnixMicro()),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": text,
				},
				"finish_reason": finishReason,
			},
		},
		"usage": usage,
	}

	return json.Marshal(openaiResp)
}
