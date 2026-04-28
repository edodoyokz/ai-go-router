package providers

import (
	"encoding/json"
	"testing"
)

func TestChatRequestPreservesRichOpenAIFields(t *testing.T) {
	body := []byte(`{
		"model":"gpt-4o",
		"messages":[{
			"role":"user",
			"content":[{"type":"text","text":"describe"},{"type":"image_url","image_url":{"url":"data:image/png;base64,abc"}}]
		}],
		"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],
		"response_format":{"type":"json_schema","json_schema":{"name":"answer"}},
		"reasoning":{"effort":"medium"},
		"parallel_tool_calls":true,
		"vendor_flag":{"x":1}
	}`)

	var req ChatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal rich request: %v", err)
	}
	if req.Messages[0].Content == nil {
		t.Fatalf("content parts were not preserved")
	}
	if len(req.Tools) != 1 {
		t.Fatalf("tools len = %d, want 1", len(req.Tools))
	}
	if req.ResponseFormat == nil || req.ResponseFormat.Type != "json_schema" {
		t.Fatalf("response format not preserved: %#v", req.ResponseFormat)
	}
	if req.Reasoning == nil || req.Reasoning.Effort != "medium" {
		t.Fatalf("reasoning not preserved: %#v", req.Reasoning)
	}
	if req.ParallelToolCalls == nil || !*req.ParallelToolCalls {
		t.Fatalf("parallel_tool_calls not preserved")
	}
	if _, ok := req.Extra["vendor_flag"]; !ok {
		t.Fatalf("unknown request field not preserved")
	}

	encoded, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal rich request: %v", err)
	}
	var roundTrip map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatalf("unmarshal roundtrip: %v", err)
	}
	if _, ok := roundTrip["vendor_flag"]; !ok {
		t.Fatalf("unknown request field not emitted")
	}
}
