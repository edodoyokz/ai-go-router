package translator

import (
	"context"
	"encoding/json"
	"testing"
)

func TestVertexRequestTranslator_BaseGeminiConversion(t *testing.T) {
	tr := &vertexRequestTranslator{}
	body := map[string]interface{}{
		"model": "gemini-2.0-flash",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "Hello"},
		},
	}
	result, err := tr.TranslateRequest(context.Background(), FormatOpenAI, FormatVertex, body)
	if err != nil {
		t.Fatal(err)
	}
	contents, ok := result["contents"].([]map[string]interface{})
	if !ok || len(contents) == 0 {
		t.Fatal("expected Gemini-format contents")
	}
}

func TestVertexRequestTranslator_ThoughtSignatureReplaced(t *testing.T) {
	body := map[string]interface{}{
		"contents": []interface{}{
			map[string]interface{}{
				"role": "model",
				"parts": []interface{}{
					map[string]interface{}{
						"thoughtSignature": "old-sig",
					},
				},
			},
		},
	}
	postProcessForVertex(body)
	contents := body["contents"].([]interface{})
	turn := contents[0].(map[string]interface{})
	parts := turn["parts"].([]interface{})
	part := parts[0].(map[string]interface{})
	if part["thoughtSignature"] == "old-sig" {
		t.Fatal("thoughtSignature should be replaced with Vertex native")
	}
	if part["thoughtSignature"] != defaultVertexThinkingSignature {
		t.Fatalf("thoughtSignature = %v, want default vertex sig", part["thoughtSignature"])
	}
}

func TestVertexRequestTranslator_FunctionCallIDStripped(t *testing.T) {
	body := map[string]interface{}{
		"contents": []interface{}{
			map[string]interface{}{
				"role": "model",
				"parts": []interface{}{
					map[string]interface{}{
						"functionCall": map[string]interface{}{
							"id":   "call_abc",
							"name": "my_func",
							"args": map[string]interface{}{},
						},
					},
				},
			},
		},
	}
	postProcessForVertex(body)
	contents := body["contents"].([]interface{})
	parts := contents[0].(map[string]interface{})["parts"].([]interface{})
	fc := parts[0].(map[string]interface{})["functionCall"].(map[string]interface{})
	if _, hasID := fc["id"]; hasID {
		t.Fatal("functionCall id should be stripped for Vertex")
	}
	if fc["name"] != "my_func" {
		t.Fatal("functionCall name should be preserved")
	}
}

func TestVertexRequestTranslator_FunctionResponseIDStripped(t *testing.T) {
	body := map[string]interface{}{
		"contents": []interface{}{
			map[string]interface{}{
				"role": "user",
				"parts": []interface{}{
					map[string]interface{}{
						"functionResponse": map[string]interface{}{
							"id":       "call_abc",
							"name":     "my_func",
							"response": map[string]interface{}{"result": "ok"},
						},
					},
				},
			},
		},
	}
	postProcessForVertex(body)
	contents := body["contents"].([]interface{})
	parts := contents[0].(map[string]interface{})["parts"].([]interface{})
	fr := parts[0].(map[string]interface{})["functionResponse"].(map[string]interface{})
	if _, hasID := fr["id"]; hasID {
		t.Fatal("functionResponse id should be stripped for Vertex")
	}
}

func TestVertexResponseTranslator_Passthrough(t *testing.T) {
	tr := &vertexResponseTranslator{}
	// Vertex response translator delegates to gemini — verify basic path works
	body := []byte(`{
		"candidates": [{"content": {"parts": [{"text": "Hello"}]}, "finishReason": "STOP"}]
	}`)
	result, err := tr.TranslateResponse(context.Background(), FormatVertex, FormatOpenAI, body)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(result, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["object"] != "chat.completion" {
		t.Fatalf("expected chat.completion, got %v", decoded["object"])
	}
}

func TestVertexTranslator_Registered(t *testing.T) {
	r := NewRegistry()
	raw, _ := json.Marshal(map[string]interface{}{
		"model":    "gemini-2.0-flash",
		"messages": []interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
	})
	out, err := r.TranslateRequestJSON(context.Background(), FormatOpenAI, FormatVertex, raw)
	if err != nil {
		t.Fatalf("TranslateRequestJSON: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["contents"]; !ok {
		t.Fatal("expected contents in vertex/gemini format")
	}
}
