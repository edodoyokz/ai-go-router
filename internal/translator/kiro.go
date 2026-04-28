package translator

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// kiroRequestTranslator converts OpenAI Chat Completions format to Kiro/AWS CodeWhisperer format.
type kiroRequestTranslator struct{}

func (t *kiroRequestTranslator) TranslateRequest(ctx context.Context, sourceFormat, targetFormat string, body map[string]interface{}) (map[string]interface{}, error) {
	messages, _ := body["messages"].([]interface{})
	tools, _ := body["tools"].([]interface{})
	model, _ := body["model"].(string)

	history, currentMessage := convertKiroMessages(messages, tools, model)

	var finalContent string
	if cm, ok := currentMessage["userInputMessage"].(map[string]interface{}); ok {
		finalContent, _ = cm["content"].(string)
	}
	timestamp := time.Now().UTC().Format(time.RFC3339)
	finalContent = fmt.Sprintf("[Context: Current time is %s]\n\n%s", timestamp, finalContent)

	currentMsgInner := map[string]interface{}{
		"content": finalContent,
		"modelId": model,
		"origin":  "AI_EDITOR",
	}

	if cm, ok := currentMessage["userInputMessage"].(map[string]interface{}); ok {
		if ctx2, ok := cm["userInputMessageContext"]; ok {
			currentMsgInner["userInputMessageContext"] = ctx2
		}
	}

	payload := map[string]interface{}{
		"conversationState": map[string]interface{}{
			"chatTriggerType": "MANUAL",
			"conversationId":  generateKiroID(),
			"currentMessage": map[string]interface{}{
				"userInputMessage": currentMsgInner,
			},
			"history": history,
		},
	}

	inferenceConfig := map[string]interface{}{}
	inferenceConfig["maxTokens"] = 32000
	if temp, ok := body["temperature"]; ok {
		inferenceConfig["temperature"] = temp
	}
	if topP, ok := body["top_p"]; ok {
		inferenceConfig["topP"] = topP
	}
	if len(inferenceConfig) > 0 {
		payload["inferenceConfig"] = inferenceConfig
	}

	return payload, nil
}

func convertKiroMessages(messages []interface{}, tools []interface{}, model string) ([]interface{}, map[string]interface{}) {
	type pending struct {
		role             string
		userContent      []string
		assistantContent []string
		toolResults      []interface{}
		images           []interface{}
	}

	var history []interface{}
	var current pending

	flush := func(p *pending) {
		if p.role == "user" {
			content := joinNonEmpty(p.userContent, "\n\n")
			if content == "" {
				content = "continue"
			}
			uim := map[string]interface{}{
				"content": content,
				"modelId": "",
			}
			if len(p.images) > 0 {
				uim["images"] = p.images
			}
			if len(p.toolResults) > 0 {
				uim["userInputMessageContext"] = map[string]interface{}{
					"toolResults": p.toolResults,
				}
			}
			if len(tools) > 0 && len(history) == 0 {
				ctx2, _ := uim["userInputMessageContext"].(map[string]interface{})
				if ctx2 == nil {
					ctx2 = map[string]interface{}{}
				}
				ctx2["tools"] = convertKiroTools(tools)
				uim["userInputMessageContext"] = ctx2
			}
			history = append(history, map[string]interface{}{"userInputMessage": uim})
		} else if p.role == "assistant" {
			content := joinNonEmpty(p.assistantContent, "\n\n")
			if content == "" {
				content = "..."
			}
			history = append(history, map[string]interface{}{"assistantResponseMessage": map[string]interface{}{"content": content}})
		}
		*p = pending{}
	}

	for _, raw := range messages {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		normalizedRole := role
		if role == "system" || role == "tool" {
			normalizedRole = "user"
		}

		if normalizedRole != current.role && current.role != "" {
			flush(&current)
		}
		current.role = normalizedRole

		switch role {
		case "tool":
			toolContent := extractPlainText(msg["content"])
			toolCallID, _ := msg["tool_call_id"].(string)
			current.toolResults = append(current.toolResults, map[string]interface{}{
				"toolUseId": toolCallID,
				"status":    "success",
				"content":   []interface{}{map[string]interface{}{"text": toolContent}},
			})

		case "system":
			current.userContent = append(current.userContent, extractPlainText(msg["content"]))

		case "user":
			if parts, ok := msg["content"].([]interface{}); ok {
				for _, partRaw := range parts {
					part, ok := partRaw.(map[string]interface{})
					if !ok {
						continue
					}
					switch part["type"] {
					case "text":
						if text, ok := part["text"].(string); ok {
							current.userContent = append(current.userContent, text)
						}
					case "image_url":
						if iu, ok := part["image_url"].(map[string]interface{}); ok {
							url, _ := iu["url"].(string)
							if b64Data, mediaType, ok := parseDataURL(url); ok {
								format := mediaTypeToFormat(mediaType)
								current.images = append(current.images, map[string]interface{}{
									"format": format,
									"source": map[string]interface{}{"bytes": b64Data},
								})
							} else if url != "" {
								current.userContent = append(current.userContent, "[Image: "+url+"]")
							}
						}
					case "image":
						if src, ok := part["source"].(map[string]interface{}); ok {
							if src["type"] == "base64" {
								mediaType, _ := src["media_type"].(string)
								data, _ := src["data"].(string)
								format := mediaTypeToFormat(mediaType)
								current.images = append(current.images, map[string]interface{}{
									"format": format,
									"source": map[string]interface{}{"bytes": data},
								})
							}
						}
					case "tool_result":
						toolUseID, _ := part["tool_use_id"].(string)
						toolContent := extractPlainText(part["content"])
						current.toolResults = append(current.toolResults, map[string]interface{}{
							"toolUseId": toolUseID,
							"status":    "success",
							"content":   []interface{}{map[string]interface{}{"text": toolContent}},
						})
					}
				}
			} else {
				content := extractPlainText(msg["content"])
				current.userContent = append(current.userContent, content)
			}

		case "assistant":
			var textContent string
			var toolUses []interface{}

			if parts, ok := msg["content"].([]interface{}); ok {
				for _, partRaw := range parts {
					part, ok := partRaw.(map[string]interface{})
					if !ok {
						continue
					}
					if part["type"] == "text" {
						text, _ := part["text"].(string)
						textContent += text
					} else if part["type"] == "tool_use" {
						toolUses = append(toolUses, part)
					}
				}
			} else {
				textContent = extractPlainText(msg["content"])
			}

			if tcs, ok := msg["tool_calls"].([]interface{}); ok {
				toolUses = tcs
			}

			if textContent != "" {
				current.assistantContent = append(current.assistantContent, textContent)
			}

			if len(toolUses) > 0 {
				if current.role == "assistant" {
					flush(&current)
				}
				content := joinNonEmpty(current.assistantContent, "\n\n")
				if content == "" {
					content = "..."
				}
				kiroToolUses := make([]interface{}, 0, len(toolUses))
				for _, tuRaw := range toolUses {
					tu, ok := tuRaw.(map[string]interface{})
					if !ok {
						continue
					}
					if fn, ok := tu["function"].(map[string]interface{}); ok {
						id, _ := tu["id"].(string)
						if id == "" {
							id = generateKiroID()
						}
						inputRaw := fn["arguments"]
						var inputObj interface{}
						if argStr, ok := inputRaw.(string); ok {
							_ = json.Unmarshal([]byte(argStr), &inputObj)
						} else {
							inputObj = inputRaw
						}
						kiroToolUses = append(kiroToolUses, map[string]interface{}{
							"toolUseId": id,
							"name":      fn["name"],
							"input":     inputObj,
						})
					} else {
						id, _ := tu["id"].(string)
						if id == "" {
							id = generateKiroID()
						}
						kiroToolUses = append(kiroToolUses, map[string]interface{}{
							"toolUseId": id,
							"name":      tu["name"],
							"input":     tu["input"],
						})
					}
				}
				history = append(history, map[string]interface{}{
					"assistantResponseMessage": map[string]interface{}{
						"content":  content,
						"toolUses": kiroToolUses,
					},
				})
				current = pending{}
			}
		}
	}

	if current.role != "" {
		flush(&current)
	}

	var currentMessage map[string]interface{}
	for i := len(history) - 1; i >= 0; i-- {
		if item, ok := history[i].(map[string]interface{}); ok {
			if _, hasUIM := item["userInputMessage"]; hasUIM {
				currentMessage = item
				history = append(history[:i], history[i+1:]...)
				break
			}
		}
	}
	if currentMessage == nil {
		currentMessage = map[string]interface{}{
			"userInputMessage": map[string]interface{}{"content": "continue", "modelId": model},
		}
	}

	firstHistoryTools := extractKiroHistoryTools(history)
	for _, rawItem := range history {
		item, ok := rawItem.(map[string]interface{})
		if !ok {
			continue
		}
		if uim, ok := item["userInputMessage"].(map[string]interface{}); ok {
			if ctx2, ok := uim["userInputMessageContext"].(map[string]interface{}); ok {
				delete(ctx2, "tools")
				if len(ctx2) == 0 {
					delete(uim, "userInputMessageContext")
				}
			}
			if uim["modelId"] == nil || uim["modelId"] == "" {
				uim["modelId"] = model
			}
		}
	}

	mergedHistory := mergeConsecutiveKiroUsers(history)

	if firstHistoryTools != nil {
		if cm, ok := currentMessage["userInputMessage"].(map[string]interface{}); ok {
			if ctx2, ok := cm["userInputMessageContext"].(map[string]interface{}); ok {
				if ctx2["tools"] == nil {
					ctx2["tools"] = firstHistoryTools
				}
			} else {
				cm["userInputMessageContext"] = map[string]interface{}{"tools": firstHistoryTools}
			}
		}
	}

	return mergedHistory, currentMessage
}

func extractKiroHistoryTools(history []interface{}) interface{} {
	if len(history) == 0 {
		return nil
	}
	item, ok := history[0].(map[string]interface{})
	if !ok {
		return nil
	}
	if uim, ok := item["userInputMessage"].(map[string]interface{}); ok {
		if ctx2, ok := uim["userInputMessageContext"].(map[string]interface{}); ok {
			return ctx2["tools"]
		}
	}
	return nil
}

func mergeConsecutiveKiroUsers(history []interface{}) []interface{} {
	var merged []interface{}
	for _, rawItem := range history {
		item, ok := rawItem.(map[string]interface{})
		if !ok {
			merged = append(merged, rawItem)
			continue
		}
		uim, hasUIM := item["userInputMessage"].(map[string]interface{})
		if hasUIM && len(merged) > 0 {
			if prevItem, ok := merged[len(merged)-1].(map[string]interface{}); ok {
				if prev, ok := prevItem["userInputMessage"].(map[string]interface{}); ok {
					prevContent, _ := prev["content"].(string)
					curContent, _ := uim["content"].(string)
					prev["content"] = prevContent + "\n\n" + curContent
					continue
				}
			}
		}
		merged = append(merged, item)
	}
	return merged
}

func convertKiroTools(tools []interface{}) []interface{} {
	var result []interface{}
	for _, toolRaw := range tools {
		tool, ok := toolRaw.(map[string]interface{})
		if !ok {
			continue
		}
		fn, _ := tool["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		desc, _ := fn["description"].(string)
		if desc == "" {
			desc = "Tool: " + name
		}
		schema := fn["parameters"]
		if schema == nil {
			schema = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "required": []interface{}{}}
		} else {
			schema = normalizeKiroSchema(schema)
		}
		result = append(result, map[string]interface{}{
			"toolSpecification": map[string]interface{}{
				"name":        name,
				"description": desc,
				"inputSchema": map[string]interface{}{"json": schema},
			},
		})
	}
	return result
}

func normalizeKiroSchema(schema interface{}) interface{} {
	m, ok := schema.(map[string]interface{})
	if !ok {
		return schema
	}
	if m["required"] == nil {
		m["required"] = []interface{}{}
	}
	return m
}

func generateKiroID() string {
	return fmt.Sprintf("kiro_%d", time.Now().UnixNano())
}

func extractPlainText(content interface{}) string {
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
			if text, ok := part["text"].(string); ok {
				parts = append(parts, text)
			}
		}
		return joinNonEmpty(parts, "\n")
	}
	return ""
}

func joinNonEmpty(ss []string, sep string) string {
	var out []string
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return ""
	}
	result := out[0]
	for _, s := range out[1:] {
		result += sep + s
	}
	return result
}

func parseDataURL(url string) (data, mediaType string, ok bool) {
	prefix := "data:"
	if len(url) <= len(prefix) || url[:len(prefix)] != prefix {
		return "", "", false
	}
	rest := url[len(prefix):]
	semi := -1
	for i, c := range rest {
		if c == ';' {
			semi = i
			break
		}
	}
	if semi < 0 {
		return "", "", false
	}
	mediaType = rest[:semi]
	rest = rest[semi+1:]
	if len(rest) < 7 || rest[:7] != "base64," {
		return "", "", false
	}
	return rest[7:], mediaType, true
}

func mediaTypeToFormat(mediaType string) string {
	if idx := indexOf(mediaType, '/'); idx >= 0 {
		return mediaType[idx+1:]
	}
	return mediaType
}

func indexOf(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// kiroResponseTranslator converts Kiro/AWS CodeWhisperer events to OpenAI format.
type kiroResponseTranslator struct{}

func (t *kiroResponseTranslator) TranslateResponse(ctx context.Context, sourceFormat, targetFormat string, body []byte) ([]byte, error) {
	return body, nil
}
