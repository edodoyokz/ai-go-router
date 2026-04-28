package translator

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type responsesToOpenAIRequestTranslator struct{}

func (t *responsesToOpenAIRequestTranslator) TranslateRequest(ctx context.Context, sourceFormat, targetFormat string, body map[string]interface{}) (map[string]interface{}, error) {
	result := map[string]interface{}{}
	if model, ok := body["model"]; ok {
		result["model"] = model
	}
	if stream, ok := body["stream"]; ok {
		result["stream"] = stream
	}
	if maxTokens, ok := body["max_output_tokens"]; ok {
		result["max_tokens"] = maxTokens
	}
	if maxTokens, ok := body["max_tokens"]; ok {
		result["max_tokens"] = maxTokens
	}
	if tools, ok := body["tools"]; ok {
		result["tools"] = tools
	}
	if reasoning, ok := body["reasoning"]; ok {
		result["reasoning"] = reasoning
	}
	result["messages"] = responsesInputToMessages(body["input"])
	return result, nil
}

type openAIToResponsesRequestTranslator struct{}

func (t *openAIToResponsesRequestTranslator) TranslateRequest(ctx context.Context, sourceFormat, targetFormat string, body map[string]interface{}) (map[string]interface{}, error) {
	result := map[string]interface{}{}
	if model, ok := body["model"]; ok {
		result["model"] = model
	}
	if stream, ok := body["stream"]; ok {
		result["stream"] = stream
	}
	if maxTokens, ok := body["max_tokens"]; ok {
		result["max_output_tokens"] = maxTokens
	}
	if tools, ok := body["tools"]; ok {
		result["tools"] = tools
	}
	if reasoning, ok := body["reasoning"]; ok {
		result["reasoning"] = reasoning
	}
	if messages, ok := body["messages"].([]interface{}); ok {
		result["input"] = messages
	}
	return result, nil
}

func responsesInputToMessages(input interface{}) []interface{} {
	switch v := input.(type) {
	case string:
		return []interface{}{map[string]interface{}{"role": "user", "content": v}}
	case []interface{}:
		messages := make([]interface{}, 0, len(v))
		for _, item := range v {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if role, ok := m["role"]; ok {
				messages = append(messages, map[string]interface{}{"role": role, "content": m["content"]})
				continue
			}
			if typ, _ := m["type"].(string); typ == "message" {
				messages = append(messages, map[string]interface{}{"role": m["role"], "content": m["content"]})
			}
		}
		return messages
	default:
		return nil
	}
}

type openAIToResponsesResponseTranslator struct{}

func (t *openAIToResponsesResponseTranslator) TranslateResponse(ctx context.Context, sourceFormat, targetFormat string, body []byte) ([]byte, error) {
	var openAIResp map[string]interface{}
	if err := json.Unmarshal(body, &openAIResp); err != nil {
		return nil, err
	}
	choices, _ := openAIResp["choices"].([]interface{})
	if len(choices) == 0 {
		return nil, fmt.Errorf("openai response has no choices")
	}
	choice, _ := choices[0].(map[string]interface{})
	message, _ := choice["message"].(map[string]interface{})
	output := []interface{}{map[string]interface{}{
		"type":    "message",
		"role":    message["role"],
		"content": []interface{}{map[string]interface{}{"type": "output_text", "text": message["content"]}},
	}}
	resp := map[string]interface{}{
		"id":      openAIResp["id"],
		"object":  "response",
		"created": time.Now().Unix(),
		"model":   openAIResp["model"],
		"output":  output,
		"usage":   openAIResp["usage"],
	}
	return json.Marshal(resp)
}

type responsesToOpenAIResponseTranslator struct{}

func (t *responsesToOpenAIResponseTranslator) TranslateResponse(ctx context.Context, sourceFormat, targetFormat string, body []byte) ([]byte, error) {
	var resp map[string]interface{}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	content := ""
	if output, ok := resp["output"].([]interface{}); ok {
		for _, item := range output {
			m, _ := item.(map[string]interface{})
			blocks, _ := m["content"].([]interface{})
			for _, block := range blocks {
				b, _ := block.(map[string]interface{})
				if text, ok := b["text"].(string); ok {
					content += text
				}
			}
		}
	}
	openAIResp := map[string]interface{}{
		"id":      resp["id"],
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   resp["model"],
		"choices": []interface{}{map[string]interface{}{
			"index": 0,
			"message": map[string]interface{}{
				"role":    "assistant",
				"content": content,
			},
			"finish_reason": "stop",
		}},
		"usage": resp["usage"],
	}
	return json.Marshal(openAIResp)
}
