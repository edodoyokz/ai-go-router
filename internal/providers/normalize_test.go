package providers

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNormalizeToolCallsGeneratesIDsAndValidatesResults(t *testing.T) {
	args := json.RawMessage(`{"q":"x"}`)
	messages, nameMap, err := NormalizeToolCalls([]ChatMessage{
		{Role: "assistant", ToolCalls: []ToolCall{{Function: &FunctionCall{Name: "bad tool", Arguments: args}}}},
	})
	if err != nil {
		t.Fatalf("NormalizeToolCalls() error = %v", err)
	}
	call := messages[0].ToolCalls[0]
	if call.ID == "" {
		t.Fatalf("tool call ID was not generated")
	}
	if call.Function.Name != "bad_tool" {
		t.Fatalf("tool name = %q, want bad_tool", call.Function.Name)
	}
	if RestoreToolName("bad_tool", nameMap) != "bad tool" {
		t.Fatalf("tool name map did not preserve original")
	}

	_, _, err = NormalizeToolCalls([]ChatMessage{{Role: "tool", ToolCallID: "missing", Content: "{}"}})
	if err == nil {
		t.Fatalf("expected missing tool_call_id correlation error")
	}
}

func TestStreamAccumulatorToolAndContentAssembly(t *testing.T) {
	finish := "tool_calls"
	acc := NewStreamAccumulator()
	acc.Add(ChatChunk{ID: "1", Model: "m", Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{Role: "assistant", Content: "hel"}}}})

	firstDelta := ToolCallDelta{Index: 0, ID: "call_1", Type: "function", Function: &FunctionCall{Name: "search", Arguments: json.RawMessage(`{"q"`)}}
	acc.Add(ChatChunk{Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{Content: "lo", ToolCalls: []ToolCallDelta{firstDelta}}}}})

	secondDelta := ToolCallDelta{Index: 0, Function: &FunctionCall{Arguments: json.RawMessage(`:"x"}`)}}
	acc.Add(ChatChunk{Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{ToolCalls: []ToolCallDelta{secondDelta}}, FinishReason: &finish}}})

	resp := acc.ChatResponse()
	if resp.Choices[0].Message.Content != "hello" {
		t.Fatalf("content = %v, want hello", resp.Choices[0].Message.Content)
	}
	if string(resp.Choices[0].Message.ToolCalls[0].Function.Arguments) != `{"q":"x"}` {
		t.Fatalf("arguments = %s", resp.Choices[0].Message.ToolCalls[0].Function.Arguments)
	}
	if resp.Choices[0].FinishReason != "tool_calls" {
		t.Fatalf("finish = %s", resp.Choices[0].FinishReason)
	}
}

func TestStreamAccumulator_UsageFromFinalChunk(t *testing.T) {
	acc := NewStreamAccumulator()
	finish := "stop"
	acc.Add(ChatChunk{ID: "1", Model: "gpt-4", Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{Role: "assistant", Content: "hello"}}}})
	acc.Add(ChatChunk{
		Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{}, FinishReason: &finish}},
		Usage:   &Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	})
	resp := acc.ChatResponse()
	if resp.Usage == nil {
		t.Fatal("usage should be set from final chunk")
	}
	if resp.Usage.PromptTokens != 10 {
		t.Fatalf("prompt_tokens = %d, want 10", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 5 {
		t.Fatalf("completion_tokens = %d, want 5", resp.Usage.CompletionTokens)
	}
}

func TestStreamAccumulator_ReasoningContentAccumulation(t *testing.T) {
	acc := NewStreamAccumulator()
	acc.Add(ChatChunk{ID: "1", Model: "m", Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{ReasoningContent: "step 1; "}}}})
	acc.Add(ChatChunk{Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{ReasoningContent: "step 2"}}}})
	resp := acc.ChatResponse()
	if resp.Choices[0].Message.ReasoningContent != "step 1; step 2" {
		t.Fatalf("reasoning_content = %q, want 'step 1; step 2'", resp.Choices[0].Message.ReasoningContent)
	}
}

func TestStreamAccumulator_ThinkingAccumulation(t *testing.T) {
	acc := NewStreamAccumulator()
	acc.Add(ChatChunk{ID: "1", Model: "m", Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{Thinking: "think A"}}}})
	acc.Add(ChatChunk{Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{Thinking: " think B"}}}})
	resp := acc.ChatResponse()
	thinking := string(resp.Choices[0].Message.Thinking)
	if thinking != "think A think B" {
		t.Fatalf("thinking = %q, want 'think A think B'", thinking)
	}
}

func TestStreamAccumulator_SystemFingerprint(t *testing.T) {
	acc := NewStreamAccumulator()
	acc.Add(ChatChunk{ID: "1", Model: "m", SystemFingerprint: "fp_abc", Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{Content: "hi"}}}})
	// Second chunk should NOT overwrite first fingerprint
	acc.Add(ChatChunk{SystemFingerprint: "fp_other", Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{Content: "!"}}}})
	resp := acc.ChatResponse()
	if resp.SystemFingerprint != "fp_abc" {
		t.Fatalf("system_fingerprint = %q, want fp_abc (first wins)", resp.SystemFingerprint)
	}
}

func TestStreamAccumulator_ServiceTier(t *testing.T) {
	acc := NewStreamAccumulator()
	acc.Add(ChatChunk{ID: "1", Model: "m", ServiceTier: "default", Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{Content: "hi"}}}})
	resp := acc.ChatResponse()
	if resp.ServiceTier != "default" {
		t.Fatalf("service_tier = %q, want default", resp.ServiceTier)
	}
}

func TestStreamAccumulator_EmptyDeltaPassthrough(t *testing.T) {
	finish := "stop"
	acc := NewStreamAccumulator()
	acc.Add(ChatChunk{ID: "1", Model: "m", Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{Content: "ok"}}}})
	// Empty delta with finish reason — should set finish reason without panic
	acc.Add(ChatChunk{Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{}, FinishReason: &finish}}})
	resp := acc.ChatResponse()
	if resp.Choices[0].FinishReason != "stop" {
		t.Fatalf("finish_reason = %q, want stop", resp.Choices[0].FinishReason)
	}
	if resp.Choices[0].Message.Content != "ok" {
		t.Fatalf("content = %v, want ok", resp.Choices[0].Message.Content)
	}
}

func TestStreamAccumulator_MultipleChoices(t *testing.T) {
	acc := NewStreamAccumulator()
	acc.Add(ChatChunk{ID: "1", Model: "m", Choices: []ChunkChoice{
		{Index: 0, Delta: ChunkDelta{Role: "assistant", Content: "choice0"}},
		{Index: 1, Delta: ChunkDelta{Role: "assistant", Content: "choice1"}},
	}})
	resp := acc.ChatResponse()
	if len(resp.Choices) != 2 {
		t.Fatalf("expected 2 choices, got %d", len(resp.Choices))
	}
	if resp.Choices[0].Message.Content != "choice0" {
		t.Fatalf("choice[0] content = %v, want choice0", resp.Choices[0].Message.Content)
	}
	if resp.Choices[1].Message.Content != "choice1" {
		t.Fatalf("choice[1] content = %v, want choice1", resp.Choices[1].Message.Content)
	}
}

func TestStreamAccumulator_FirstChunkSetsMeta(t *testing.T) {
	acc := NewStreamAccumulator()
	acc.Add(ChatChunk{ID: "chatcmpl-first", Created: 1234567890, Model: "gpt-4o", Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{Content: "x"}}}})
	// Second chunk has different ID/model — should be ignored
	acc.Add(ChatChunk{ID: "chatcmpl-second", Model: "gpt-4", Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{Content: "y"}}}})
	resp := acc.ChatResponse()
	if resp.ID != "chatcmpl-first" {
		t.Fatalf("ID = %q, want chatcmpl-first", resp.ID)
	}
	if resp.Model != "gpt-4o" {
		t.Fatalf("Model = %q, want gpt-4o", resp.Model)
	}
}

func TestCooldownTrackerAccountWideLock(t *testing.T) {
	tracker := NewCooldownTracker()
	tracker.SetAccountLock("p", "a", time.Minute)
	if !tracker.IsAccountLocked("p", "a") {
		t.Fatalf("account lock not active")
	}
	if !tracker.IsModelLocked("p", "a", "any-model") {
		t.Fatalf("account-wide lock did not apply to model")
	}
	tracker.ClearAccountLock("p", "a")
	if tracker.IsModelLocked("p", "a", "any-model") {
		t.Fatalf("account lock was not cleared")
	}
}
