package translator

import (
	"context"
	"encoding/json"
	"strings"
)

// cursorRequestTranslator converts OpenAI messages to Cursor ask/agent format.
// Tool outputs are represented as structured text blocks (XML-like) in user messages
// to avoid Cursor loop bugs with protobuf tool_results.
type cursorRequestTranslator struct{}

func (t *cursorRequestTranslator) TranslateRequest(ctx context.Context, sourceFormat, targetFormat string, body map[string]interface{}) (map[string]interface{}, error) {
	messages, _ := body["messages"].([]interface{})
	converted := convertCursorMessages(messages)

	result := make(map[string]interface{})
	for k, v := range body {
		switch k {
		case "user", "metadata", "tool_choice", "stream_options", "system", "messages":
			continue
		default:
			result[k] = v
		}
	}
	result["messages"] = converted
	result["max_tokens"] = 32000
	return result, nil
}

func convertCursorMessages(messages []interface{}) []interface{} {
	toolCallMetaMap := map[string]string{}

	for _, raw := range messages {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if msg["role"] == "assistant" {
			if tcs, ok := msg["tool_calls"].([]interface{}); ok {
				for _, tcRaw := range tcs {
					tc, ok := tcRaw.(map[string]interface{})
					if !ok {
						continue
					}
					id, _ := tc["id"].(string)
					fn, _ := tc["function"].(map[string]interface{})
					name, _ := fn["name"].(string)
					if id != "" {
						toolCallMetaMap[id] = name
					}
				}
			}
			if content, ok := msg["content"].([]interface{}); ok {
				for _, partRaw := range content {
					part, ok := partRaw.(map[string]interface{})
					if !ok {
						continue
					}
					if part["type"] == "tool_use" {
						id, _ := part["id"].(string)
						name, _ := part["name"].(string)
						if id != "" {
							toolCallMetaMap[id] = name
						}
					}
				}
			}
		}
	}

	var result []interface{}
	for _, raw := range messages {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)

		switch role {
		case "system":
			result = append(result, map[string]interface{}{
				"role":    "user",
				"content": "[System Instructions]\n" + extractCursorText(msg["content"]),
			})

		case "tool":
			toolCallID, _ := msg["tool_call_id"].(string)
			toolName := toolCallMetaMap[normalizeToolCallID(toolCallID)]
			if toolName == "" {
				toolName = "tool"
			}
			toolContent := extractCursorText(msg["content"])
			result = append(result, map[string]interface{}{
				"role":    "user",
				"content": buildCursorToolResultBlock(toolName, toolCallID, toolContent),
			})

		case "user":
			if parts, ok := msg["content"].([]interface{}); ok {
				var textParts []string
				for _, partRaw := range parts {
					part, ok := partRaw.(map[string]interface{})
					if !ok {
						continue
					}
					switch part["type"] {
					case "text":
						if text, ok := part["text"].(string); ok {
							textParts = append(textParts, text)
						}
					case "tool_result":
						toolUseID, _ := part["tool_use_id"].(string)
						meta := toolCallMetaMap[toolUseID]
						if meta == "" {
							meta = toolCallMetaMap[normalizeToolCallID(toolUseID)]
						}
						if meta == "" {
							meta = "tool"
						}
						toolContent := extractCursorText(part["content"])
						textParts = append(textParts, buildCursorToolResultBlock(meta, toolUseID, toolContent))
					}
				}
				joined := strings.Join(filterEmpty(textParts), "\n")
				if joined != "" {
					result = append(result, map[string]interface{}{"role": "user", "content": joined})
				}
			} else {
				content := extractCursorText(msg["content"])
				if content != "" {
					result = append(result, map[string]interface{}{"role": "user", "content": content})
				}
			}

		case "assistant":
			content := extractCursorText(msg["content"])
			tcs, hasTCs := extractCursorToolCalls(msg)

			if hasTCs {
				m := map[string]interface{}{"role": "assistant", "content": content}
				m["tool_calls"] = tcs
				result = append(result, m)
			} else if parts, ok := msg["content"].([]interface{}); ok {
				var extracted []interface{}
				for _, pRaw := range parts {
					p, ok := pRaw.(map[string]interface{})
					if !ok {
						continue
					}
					if p["type"] == "tool_use" {
						id, _ := p["id"].(string)
						name, _ := p["name"].(string)
						inputRaw, _ := json.Marshal(p["input"])
						extracted = append(extracted, map[string]interface{}{
							"id":   id,
							"type": "function",
							"function": map[string]interface{}{
								"name":      name,
								"arguments": string(inputRaw),
							},
						})
					}
				}
				if len(extracted) > 0 {
					result = append(result, map[string]interface{}{
						"role":       "assistant",
						"content":    content,
						"tool_calls": extracted,
					})
				} else if content != "" {
					result = append(result, map[string]interface{}{"role": "assistant", "content": content})
				}
			} else if content != "" {
				result = append(result, map[string]interface{}{"role": "assistant", "content": content})
			}
		}
	}
	return result
}

func extractCursorText(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var parts []string
		for _, partRaw := range v {
			part, ok := partRaw.(map[string]interface{})
			if !ok {
				continue
			}
			if part["type"] == "text" {
				if text, ok := part["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "")
	}
	return ""
}

func extractCursorToolCalls(msg map[string]interface{}) ([]interface{}, bool) {
	tcs, ok := msg["tool_calls"].([]interface{})
	if !ok || len(tcs) == 0 {
		return nil, false
	}
	var result []interface{}
	for _, tcRaw := range tcs {
		tc, ok := tcRaw.(map[string]interface{})
		if !ok {
			continue
		}
		clean := make(map[string]interface{})
		for k, v := range tc {
			if k != "index" {
				clean[k] = v
			}
		}
		result = append(result, clean)
	}
	return result, len(result) > 0
}

func buildCursorToolResultBlock(toolName, toolCallID, result string) string {
	clean := sanitizeCursorToolResultText(result)
	return strings.Join([]string{
		"<tool_result>",
		"<tool_name>" + escapeXMLCursor(toolName) + "</tool_name>",
		"<tool_call_id>" + escapeXMLCursor(toolCallID) + "</tool_call_id>",
		"<result>" + escapeXMLCursor(clean) + "</result>",
		"</tool_result>",
	}, "\n")
}

func sanitizeCursorToolResultText(text string) string {
	var b strings.Builder
	for _, r := range text {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func escapeXMLCursor(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func normalizeToolCallID(id string) string {
	if idx := strings.IndexByte(id, '\n'); idx >= 0 {
		return id[:idx]
	}
	return id
}

func filterEmpty(ss []string) []string {
	var out []string
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// cursorResponseTranslator is a passthrough — CursorExecutor already emits OpenAI format.
type cursorResponseTranslator struct{}

func (t *cursorResponseTranslator) TranslateResponse(ctx context.Context, sourceFormat, targetFormat string, body []byte) ([]byte, error) {
	return body, nil
}
