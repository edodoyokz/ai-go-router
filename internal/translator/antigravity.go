package translator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// antigravityRequestTranslator converts Antigravity (Gemini-like) request format to OpenAI format.
// Antigravity body: { project, model, userAgent, requestType, requestId, request: { contents, systemInstruction, tools, toolConfig, generationConfig, sessionId } }
type antigravityRequestTranslator struct{}

func (t *antigravityRequestTranslator) TranslateRequest(ctx context.Context, sourceFormat, targetFormat string, body map[string]interface{}) (map[string]interface{}, error) {
	req, _ := body["request"].(map[string]interface{})
	if req == nil {
		req = body
	}

	model, _ := body["model"].(string)
	result := map[string]interface{}{
		"model":    model,
		"messages": []interface{}{},
	}

	if genConfig, ok := req["generationConfig"].(map[string]interface{}); ok {
		if maxOut, ok := genConfig["maxOutputTokens"]; ok {
			result["max_tokens"] = maxOut
		}
		if temp, ok := genConfig["temperature"]; ok {
			result["temperature"] = temp
		}
		if topP, ok := genConfig["topP"]; ok {
			result["top_p"] = topP
		}
		if topK, ok := genConfig["topK"]; ok {
			result["top_k"] = topK
		}
		if thinkingCfg, ok := genConfig["thinkingConfig"].(map[string]interface{}); ok {
			budget := toFloat64(thinkingCfg["thinkingBudget"])
			if budget > 0 {
				switch {
				case budget <= 2048:
					result["reasoning_effort"] = "low"
				case budget <= 16384:
					result["reasoning_effort"] = "medium"
				default:
					result["reasoning_effort"] = "high"
				}
			}
		}
	}

	var messages []interface{}

	if sysInstruction := req["systemInstruction"]; sysInstruction != nil {
		systemText := extractAntigravityText(sysInstruction)
		if systemText != "" {
			messages = append(messages, map[string]interface{}{
				"role":    "system",
				"content": systemText,
			})
		}
	}

	if contents, ok := req["contents"].([]interface{}); ok {
		for _, contentRaw := range contents {
			content, ok := contentRaw.(map[string]interface{})
			if !ok {
				continue
			}
			converted := convertAntigravityContent(content)
			if converted == nil {
				continue
			}
			if arr, ok := converted.([]interface{}); ok {
				messages = append(messages, arr...)
			} else {
				messages = append(messages, converted)
			}
		}
	}

	result["messages"] = messages

	if tools, ok := req["tools"].([]interface{}); ok {
		var openAITools []interface{}
		for _, toolRaw := range tools {
			tool, ok := toolRaw.(map[string]interface{})
			if !ok {
				continue
			}
			if fds, ok := tool["functionDeclarations"].([]interface{}); ok {
				for _, fdRaw := range fds {
					fd, ok := fdRaw.(map[string]interface{})
					if !ok {
						continue
					}
					params := normalizeAntigravitySchema(fd["parameters"])
					if params == nil {
						params = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
					}
					openAITools = append(openAITools, map[string]interface{}{
						"type": "function",
						"function": map[string]interface{}{
							"name":        fd["name"],
							"description": fmt.Sprintf("%v", fd["description"]),
							"parameters":  params,
						},
					})
				}
			}
		}
		if len(openAITools) > 0 {
			result["tools"] = openAITools
		}
	}

	return result, nil
}

func normalizeAntigravitySchema(schema interface{}) interface{} {
	switch v := schema.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{})
		for key, val := range v {
			if key == "enumDescriptions" {
				continue
			}
			if key == "type" {
				if s, ok := val.(string); ok {
					result[key] = strings.ToLower(s)
					continue
				}
			}
			result[key] = normalizeAntigravitySchema(val)
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(v))
		for i, item := range v {
			result[i] = normalizeAntigravitySchema(item)
		}
		return result
	}
	return schema
}

func convertAntigravityContent(content map[string]interface{}) interface{} {
	role, _ := content["role"].(string)
	if role == "model" {
		role = "assistant"
	} else if role == "" {
		role = "user"
	}

	parts, _ := content["parts"].([]interface{})
	if len(parts) == 0 {
		return nil
	}

	var textParts []interface{}
	var toolCalls []interface{}
	var toolResults []interface{}
	var reasoningContent string

	for _, partRaw := range parts {
		part, ok := partRaw.(map[string]interface{})
		if !ok {
			continue
		}

		if thought, _ := part["thought"].(bool); thought {
			if text, ok := part["text"].(string); ok {
				reasoningContent += text
			}
			continue
		}

		if _, hasSignature := part["thoughtSignature"]; hasSignature {
			if text, ok := part["text"].(string); ok {
				textParts = append(textParts, map[string]interface{}{"type": "text", "text": text})
			}
			continue
		}

		if text, ok := part["text"].(string); ok {
			textParts = append(textParts, map[string]interface{}{"type": "text", "text": text})
		}

		if inlineData, ok := part["inlineData"].(map[string]interface{}); ok {
			mimeType, _ := inlineData["mimeType"].(string)
			data, _ := inlineData["data"].(string)
			textParts = append(textParts, map[string]interface{}{
				"type": "image_url",
				"image_url": map[string]interface{}{
					"url": "data:" + mimeType + ";base64," + data,
				},
			})
		}

		if fc, ok := part["functionCall"].(map[string]interface{}); ok {
			id, _ := fc["id"].(string)
			if id == "" {
				id = fmt.Sprintf("call_%d_%s", len(toolCalls), safeString(fc["name"]))
			}
			argsJSON, _ := json.Marshal(fc["args"])
			toolCalls = append(toolCalls, map[string]interface{}{
				"id":   id,
				"type": "function",
				"function": map[string]interface{}{
					"name":      fc["name"],
					"arguments": string(argsJSON),
				},
			})
		}

		if fr, ok := part["functionResponse"].(map[string]interface{}); ok {
			id, _ := fr["id"].(string)
			if id == "" {
				id, _ = fr["name"].(string)
			}
			var respContent interface{}
			if resp, ok := fr["response"].(map[string]interface{}); ok {
				if result, ok := resp["result"]; ok {
					respContent = result
				} else {
					respContent = resp
				}
			}
			respJSON, _ := json.Marshal(respContent)
			toolResults = append(toolResults, map[string]interface{}{
				"role":         "tool",
				"tool_call_id": id,
				"content":      string(respJSON),
			})
		}
	}

	if len(toolResults) > 0 {
		return toolResults
	}

	if len(toolCalls) > 0 {
		msg := map[string]interface{}{"role": "assistant"}
		if len(textParts) > 0 {
			msg["content"] = flattenContentParts(textParts)
		}
		if reasoningContent != "" {
			msg["reasoning_content"] = reasoningContent
		}
		msg["tool_calls"] = toolCalls
		return msg
	}

	if len(textParts) > 0 || reasoningContent != "" {
		msg := map[string]interface{}{"role": role}
		if len(textParts) > 0 {
			msg["content"] = flattenContentParts(textParts)
		}
		if reasoningContent != "" {
			msg["reasoning_content"] = reasoningContent
		}
		return msg
	}

	return nil
}

func flattenContentParts(parts []interface{}) interface{} {
	if len(parts) == 1 {
		if p, ok := parts[0].(map[string]interface{}); ok {
			if p["type"] == "text" {
				return p["text"]
			}
		}
	}
	return parts
}

func extractAntigravityText(instruction interface{}) string {
	switch v := instruction.(type) {
	case string:
		return v
	case map[string]interface{}:
		if parts, ok := v["parts"].([]interface{}); ok {
			var texts []string
			for _, p := range parts {
				if pm, ok := p.(map[string]interface{}); ok {
					if text, ok := pm["text"].(string); ok {
						texts = append(texts, text)
					}
				}
			}
			return strings.Join(texts, "")
		}
	}
	return ""
}

func toFloat64(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return 0
}

func safeString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return "fn"
}

// antigravityResponseTranslator is a passthrough — response is already OpenAI format.
type antigravityResponseTranslator struct{}

func (t *antigravityResponseTranslator) TranslateResponse(ctx context.Context, sourceFormat, targetFormat string, body []byte) ([]byte, error) {
	return body, nil
}
