package translator

import (
	"context"
	"testing"
)

func TestAntigravityTranslator_BasicConversion(t *testing.T) {
	tr := &antigravityRequestTranslator{}
	body := map[string]interface{}{
		"model": "gemini-2-5-pro",
		"request": map[string]interface{}{
			"contents": []interface{}{
				map[string]interface{}{
					"role": "user",
					"parts": []interface{}{
						map[string]interface{}{"text": "Hello"},
					},
				},
			},
		},
	}
	result, err := tr.TranslateRequest(context.Background(), FormatAntigravity, FormatOpenAI, body)
	if err != nil {
		t.Fatal(err)
	}
	msgs, ok := result["messages"].([]interface{})
	if !ok || len(msgs) == 0 {
		t.Fatal("expected messages")
	}
	msg := msgs[0].(map[string]interface{})
	if msg["role"] != "user" {
		t.Fatalf("expected user role, got %v", msg["role"])
	}
	if msg["content"] != "Hello" {
		t.Fatalf("expected content 'Hello', got %v", msg["content"])
	}
}

func TestAntigravityTranslator_SystemInstruction(t *testing.T) {
	tr := &antigravityRequestTranslator{}
	body := map[string]interface{}{
		"model": "gemini-2-5-pro",
		"request": map[string]interface{}{
			"systemInstruction": map[string]interface{}{
				"parts": []interface{}{
					map[string]interface{}{"text": "You are an assistant."},
				},
			},
			"contents": []interface{}{
				map[string]interface{}{
					"role": "user",
					"parts": []interface{}{
						map[string]interface{}{"text": "Hi"},
					},
				},
			},
		},
	}
	result, err := tr.TranslateRequest(context.Background(), FormatAntigravity, FormatOpenAI, body)
	if err != nil {
		t.Fatal(err)
	}
	msgs := result["messages"].([]interface{})
	if len(msgs) < 2 {
		t.Fatalf("expected system + user message, got %d", len(msgs))
	}
	sys := msgs[0].(map[string]interface{})
	if sys["role"] != "system" {
		t.Fatalf("expected system role, got %v", sys["role"])
	}
	if sys["content"] != "You are an assistant." {
		t.Fatalf("expected system content, got %v", sys["content"])
	}
}

func TestAntigravityTranslator_ModelRole(t *testing.T) {
	tr := &antigravityRequestTranslator{}
	body := map[string]interface{}{
		"model": "gemini-2-5-pro",
		"request": map[string]interface{}{
			"contents": []interface{}{
				map[string]interface{}{
					"role": "model",
					"parts": []interface{}{
						map[string]interface{}{"text": "I can help."},
					},
				},
			},
		},
	}
	result, err := tr.TranslateRequest(context.Background(), FormatAntigravity, FormatOpenAI, body)
	if err != nil {
		t.Fatal(err)
	}
	msgs := result["messages"].([]interface{})
	if len(msgs) == 0 {
		t.Fatal("expected messages")
	}
	msg := msgs[0].(map[string]interface{})
	if msg["role"] != "assistant" {
		t.Fatalf("model role should become assistant, got %v", msg["role"])
	}
}

func TestAntigravityTranslator_FunctionCall(t *testing.T) {
	tr := &antigravityRequestTranslator{}
	body := map[string]interface{}{
		"model": "gemini-2-5-pro",
		"request": map[string]interface{}{
			"contents": []interface{}{
				map[string]interface{}{
					"role": "model",
					"parts": []interface{}{
						map[string]interface{}{
							"functionCall": map[string]interface{}{
								"id":   "call_1",
								"name": "read_file",
								"args": map[string]interface{}{"path": "foo.go"},
							},
						},
					},
				},
			},
		},
	}
	result, err := tr.TranslateRequest(context.Background(), FormatAntigravity, FormatOpenAI, body)
	if err != nil {
		t.Fatal(err)
	}
	msgs := result["messages"].([]interface{})
	if len(msgs) == 0 {
		t.Fatal("expected messages")
	}
	msg := msgs[0].(map[string]interface{})
	if msg["role"] != "assistant" {
		t.Fatalf("expected assistant, got %v", msg["role"])
	}
	tcs, ok := msg["tool_calls"].([]interface{})
	if !ok || len(tcs) == 0 {
		t.Fatal("expected tool_calls")
	}
	tc := tcs[0].(map[string]interface{})
	if tc["id"] != "call_1" {
		t.Fatalf("expected id call_1, got %v", tc["id"])
	}
}

func TestAntigravityTranslator_SchemaTypeNormalization(t *testing.T) {
	tr := &antigravityRequestTranslator{}
	body := map[string]interface{}{
		"model": "gemini-2-5-pro",
		"request": map[string]interface{}{
			"contents": []interface{}{
				map[string]interface{}{
					"role":  "user",
					"parts": []interface{}{map[string]interface{}{"text": "hi"}},
				},
			},
			"tools": []interface{}{
				map[string]interface{}{
					"functionDeclarations": []interface{}{
						map[string]interface{}{
							"name":        "my_func",
							"description": "A function",
							"parameters": map[string]interface{}{
								"type": "OBJECT",
								"properties": map[string]interface{}{
									"arg": map[string]interface{}{"type": "STRING"},
								},
								"enumDescriptions": []interface{}{"ignored"},
							},
						},
					},
				},
			},
		},
	}
	result, err := tr.TranslateRequest(context.Background(), FormatAntigravity, FormatOpenAI, body)
	if err != nil {
		t.Fatal(err)
	}
	tools, ok := result["tools"].([]interface{})
	if !ok || len(tools) == 0 {
		t.Fatal("expected tools")
	}
	fn := tools[0].(map[string]interface{})["function"].(map[string]interface{})
	params := fn["parameters"].(map[string]interface{})
	if params["type"] != "object" {
		t.Fatalf("expected lowercase 'object', got %v", params["type"])
	}
	if _, hasEnum := params["enumDescriptions"]; hasEnum {
		t.Fatal("enumDescriptions should be stripped")
	}
}
