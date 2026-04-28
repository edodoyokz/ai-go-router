package translator

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// claudeToOpenAIRequestTranslator converts Claude Messages API format to OpenAI Chat Completions format
type claudeToOpenAIRequestTranslator struct{}

func (t *claudeToOpenAIRequestTranslator) TranslateRequest(ctx context.Context, sourceFormat, targetFormat string, body map[string]interface{}) (map[string]interface{}, error) {
	result := make(map[string]interface{})

	// Copy model
	if model, ok := body["model"]; ok {
		result["model"] = model
	}

	// Convert messages
	if messages, ok := body["messages"].([]interface{}); ok {
		result["messages"] = t.convertClaudeToOpenAIMessages(messages)
	}

	// Handle system message (Claude has separate system field, OpenAI uses role=system in messages)
	if system, ok := body["system"].(string); ok && system != "" {
		if existingMessages, ok := result["messages"].([]interface{}); ok {
			result["messages"] = append([]interface{}{map[string]interface{}{
				"role":    "system",
				"content": system,
			}}, existingMessages...)
		}
	}

	// Convert max_tokens (same in both)
	if maxTokens, ok := body["max_tokens"]; ok {
		result["max_tokens"] = maxTokens
	}

	// Convert temperature (same in both)
	if temp, ok := body["temperature"]; ok {
		result["temperature"] = temp
	}

	// Convert top_p (same in both)
	if topP, ok := body["top_p"]; ok {
		result["top_p"] = topP
	}

	// Handle stop_sequences (Claude) -> stop (OpenAI)
	if stopSequences, ok := body["stop_sequences"].([]interface{}); ok {
		if len(stopSequences) > 0 {
			result["stop"] = stopSequences
		}
	}

	// Handle stream (same in both)
	if stream, ok := body["stream"]; ok {
		result["stream"] = stream
	}

	return result, nil
}

func (t *claudeToOpenAIRequestTranslator) convertClaudeToOpenAIMessages(messages []interface{}) []interface{} {
	converted := make([]interface{}, 0, len(messages))

	for _, msg := range messages {
		msgMap, ok := msg.(map[string]interface{})
		if !ok {
			continue
		}

		convertedMsg := make(map[string]interface{})

		// Role mapping (same names)
		if role, ok := msgMap["role"].(string); ok {
			convertedMsg["role"] = role
		}

		// Content handling
		if content, ok := msgMap["content"]; ok {
			switch v := content.(type) {
			case string:
				convertedMsg["content"] = v
			case []interface{}:
				convertedMsg["content"] = t.convertClaudeContentBlocks(v)
			}
		}

		converted = append(converted, convertedMsg)
	}

	return converted
}

func (t *claudeToOpenAIRequestTranslator) convertClaudeContentBlocks(blocks []interface{}) interface{} {
	parts := make([]interface{}, 0, len(blocks))
	for _, block := range blocks {
		blockMap, ok := block.(map[string]interface{})
		if !ok {
			continue
		}
		blockType, _ := blockMap["type"].(string)
		switch blockType {
		case "text":
			parts = append(parts, map[string]interface{}{"type": "text", "text": blockMap["text"]})
		case "image":
			parts = append(parts, map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": blockMap["source"]}})
		}
	}
	if len(parts) == 1 {
		if part, ok := parts[0].(map[string]interface{}); ok && part["type"] == "text" {
			return part["text"]
		}
	}
	return parts
}

// openAIToClaudeRequestTranslator converts OpenAI Chat Completions format to Claude Messages API format
type openAIToClaudeRequestTranslator struct{}

func (t *openAIToClaudeRequestTranslator) TranslateRequest(ctx context.Context, sourceFormat, targetFormat string, body map[string]interface{}) (map[string]interface{}, error) {
	result := make(map[string]interface{})

	// Copy model
	if model, ok := body["model"]; ok {
		result["model"] = model
	}

	// Convert messages and extract system message
	if messages, ok := body["messages"].([]interface{}); ok {
		convertedMessages, systemMsg := t.convertOpenAIToClaudeMessages(messages)
		result["messages"] = convertedMessages
		if systemMsg != "" {
			result["system"] = systemMsg
		}
	}

	// Convert max_tokens (same in both)
	if maxTokens, ok := body["max_tokens"]; ok {
		result["max_tokens"] = maxTokens
	}

	// Convert temperature (same in both)
	if temp, ok := body["temperature"]; ok {
		result["temperature"] = temp
	}

	// Convert top_p (same in both)
	if topP, ok := body["top_p"]; ok {
		result["top_p"] = topP
	}

	// Handle stop (OpenAI) -> stop_sequences (Claude)
	if stop, ok := body["stop"]; ok {
		switch v := stop.(type) {
		case string:
			if v != "" {
				result["stop_sequences"] = []interface{}{v}
			}
		case []interface{}:
			if len(v) > 0 {
				result["stop_sequences"] = v
			}
		}
	}

	// Handle stream (same in both)
	if stream, ok := body["stream"]; ok {
		result["stream"] = stream
	}

	return result, nil
}

func (t *openAIToClaudeRequestTranslator) convertOpenAIToClaudeMessages(messages []interface{}) ([]interface{}, string) {
	converted := make([]interface{}, 0, len(messages))
	var systemMsg string

	for _, msg := range messages {
		msgMap, ok := msg.(map[string]interface{})
		if !ok {
			continue
		}

		role, ok := msgMap["role"].(string)
		if !ok {
			continue
		}

		// Extract system message
		if role == "system" {
			if content, ok := msgMap["content"].(string); ok {
				systemMsg = content
			}
			continue
		}

		convertedMsg := make(map[string]interface{})
		convertedMsg["role"] = role

		if role == "tool" {
			convertedMsg["role"] = "user"
			convertedMsg["content"] = []interface{}{map[string]interface{}{
				"type":        "tool_result",
				"tool_use_id": msgMap["tool_call_id"],
				"content":     msgMap["content"],
			}}
			converted = append(converted, convertedMsg)
			continue
		}

		if content, ok := msgMap["content"]; ok {
			convertedMsg["content"] = content
		}

		if toolCalls, ok := msgMap["tool_calls"].([]interface{}); ok && len(toolCalls) > 0 {
			blocks := make([]interface{}, 0, len(toolCalls))
			if text, ok := convertedMsg["content"].(string); ok && text != "" {
				blocks = append(blocks, map[string]interface{}{"type": "text", "text": text})
			}
			for _, rawCall := range toolCalls {
				call, ok := rawCall.(map[string]interface{})
				if !ok {
					continue
				}
				fn, _ := call["function"].(map[string]interface{})
				blocks = append(blocks, map[string]interface{}{
					"type":  "tool_use",
					"id":    call["id"],
					"name":  fn["name"],
					"input": parseMaybeJSON(fn["arguments"]),
				})
			}
			convertedMsg["content"] = blocks
		}

		converted = append(converted, convertedMsg)
	}

	return converted, systemMsg
}

func parseMaybeJSON(value interface{}) interface{} {
	s, ok := value.(string)
	if !ok || s == "" {
		return value
	}
	var decoded interface{}
	if err := json.Unmarshal([]byte(s), &decoded); err != nil {
		return value
	}
	return decoded
}

// claudeToOpenAIResponseTranslator converts Claude response format to OpenAI response format
type claudeToOpenAIResponseTranslator struct{}

func (t *claudeToOpenAIResponseTranslator) TranslateResponse(ctx context.Context, sourceFormat, targetFormat string, body []byte) ([]byte, error) {
	// Parse Claude response
	var claudeResp struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Role    string `json:"role"`
		Content []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
		Model      string `json:"model"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &claudeResp); err != nil {
		return nil, err
	}

	// Convert to OpenAI format
	var content string
	var toolCalls []map[string]interface{}
	for i, block := range claudeResp.Content {
		if block.Type == "text" {
			if i > 0 && content != "" {
				content += " "
			}
			content += block.Text
		} else if block.Type == "tool_use" {
			toolCalls = append(toolCalls, map[string]interface{}{
				"id":   block.ID,
				"type": "function",
				"function": map[string]interface{}{
					"name":      block.Name,
					"arguments": string(block.Input),
				},
			})
		}
	}
	message := map[string]interface{}{
		"role":    "assistant",
		"content": content,
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}

	// Map stop reason
	finishReason := "stop"
	switch claudeResp.StopReason {
	case "end_turn":
		finishReason = "stop"
	case "max_tokens":
		finishReason = "length"
	case "stop_sequence":
		finishReason = "stop"
	}

	openAIResp := map[string]interface{}{
		"id":      claudeResp.ID,
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   claudeResp.Model,
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"message":       message,
				"finish_reason": finishReason,
			},
		},
		"usage": map[string]interface{}{
			"prompt_tokens":     claudeResp.Usage.InputTokens,
			"completion_tokens": claudeResp.Usage.OutputTokens,
			"total_tokens":      claudeResp.Usage.InputTokens + claudeResp.Usage.OutputTokens,
		},
	}

	return json.Marshal(openAIResp)
}

// openAIToClaudeResponseTranslator converts OpenAI response format to Claude response format
type openAIToClaudeResponseTranslator struct{}

func (t *openAIToClaudeResponseTranslator) TranslateResponse(ctx context.Context, sourceFormat, targetFormat string, body []byte) ([]byte, error) {
	// Parse OpenAI response
	var openAIResp struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		Model   string `json:"model"`
		Choices []struct {
			Index   int `json:"index"`
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage,omitempty"`
	}
	if err := json.Unmarshal(body, &openAIResp); err != nil {
		return nil, err
	}

	// Convert to Claude format
	if len(openAIResp.Choices) == 0 {
		return nil, fmt.Errorf("openai response has no choices")
	}

	content := []map[string]interface{}{
		{
			"type": "text",
			"text": openAIResp.Choices[0].Message.Content,
		},
	}

	// Map finish reason
	stopReason := "end_turn"
	switch openAIResp.Choices[0].FinishReason {
	case "length":
		stopReason = "max_tokens"
	case "stop":
		stopReason = "end_turn"
	}

	claudeResp := map[string]interface{}{
		"id":          openAIResp.ID,
		"type":        "message",
		"role":        "assistant",
		"content":     content,
		"model":       openAIResp.Model,
		"stop_reason": stopReason,
	}

	if openAIResp.Usage != nil {
		claudeResp["usage"] = map[string]interface{}{
			"input_tokens":  openAIResp.Usage.PromptTokens,
			"output_tokens": openAIResp.Usage.CompletionTokens,
		}
	}

	return json.Marshal(claudeResp)
}
