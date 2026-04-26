package translator

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// FormatGemini is the native Google Gemini generateContent API format.
const FormatGemini = "gemini"

// geminiRequestTranslator converts OpenAI Chat Completions format → Gemini generateContent format.
type geminiRequestTranslator struct{}

func (t *geminiRequestTranslator) TranslateRequest(_ context.Context, _, _ string, body map[string]interface{}) (map[string]interface{}, error) {
	result := map[string]interface{}{}

	// Convert messages to Gemini "contents" array
	var contents []map[string]interface{}
	if messages, ok := body["messages"].([]interface{}); ok {
		for _, m := range messages {
			msg, ok := m.(map[string]interface{})
			if !ok {
				continue
			}
			role, _ := msg["role"].(string)
			content, _ := msg["content"].(string)

			// Gemini uses "user" / "model" roles
			geminiRole := "user"
			if role == "assistant" {
				geminiRole = "model"
			}
			// system messages get prepended as user message
			contents = append(contents, map[string]interface{}{
				"role": geminiRole,
				"parts": []map[string]interface{}{
					{"text": content},
				},
			})
		}
	}
	result["contents"] = contents

	// Generation config
	genCfg := map[string]interface{}{}
	if temp, ok := body["temperature"]; ok {
		genCfg["temperature"] = temp
	}
	if topP, ok := body["top_p"]; ok {
		genCfg["topP"] = topP
	}
	if maxTokens, ok := body["max_tokens"]; ok {
		genCfg["maxOutputTokens"] = maxTokens
	}
	if stop, ok := body["stop"]; ok {
		genCfg["stopSequences"] = stop
	}
	if len(genCfg) > 0 {
		result["generationConfig"] = genCfg
	}

	return result, nil
}

// geminiResponseTranslator converts Gemini generateContent response → OpenAI Chat Completions format.
type geminiResponseTranslator struct{}

func (t *geminiResponseTranslator) TranslateResponse(_ context.Context, _, _ string, body []byte) ([]byte, error) {
	var geminiResp map[string]interface{}
	if err := json.Unmarshal(body, &geminiResp); err != nil {
		return nil, fmt.Errorf("gemini translator: unmarshal response: %w", err)
	}

	text := ""
	finishReason := "stop"

	if candidates, ok := geminiResp["candidates"].([]interface{}); ok && len(candidates) > 0 {
		if cand, ok := candidates[0].(map[string]interface{}); ok {
			if content, ok := cand["content"].(map[string]interface{}); ok {
				if parts, ok := content["parts"].([]interface{}); ok && len(parts) > 0 {
					if part, ok := parts[0].(map[string]interface{}); ok {
						text, _ = part["text"].(string)
					}
				}
			}
			if fr, ok := cand["finishReason"].(string); ok {
				switch fr {
				case "STOP":
					finishReason = "stop"
				case "MAX_TOKENS":
					finishReason = "length"
				default:
					finishReason = "stop"
				}
			}
		}
	}

	// Extract usage if present
	usage := map[string]interface{}{}
	if um, ok := geminiResp["usageMetadata"].(map[string]interface{}); ok {
		usage["prompt_tokens"] = um["promptTokenCount"]
		usage["completion_tokens"] = um["candidatesTokenCount"]
		usage["total_tokens"] = um["totalTokenCount"]
	}

	openaiResp := map[string]interface{}{
		"id":      fmt.Sprintf("gemini-%d", time.Now().UnixMicro()),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   "gemini",
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
