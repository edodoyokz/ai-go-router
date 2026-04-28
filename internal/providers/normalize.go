package providers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var invalidToolNameChars = regexp.MustCompile(`[^A-Za-z0-9_-]`)

type ToolNameMap map[string]string

func NormalizeToolCalls(messages []ChatMessage) ([]ChatMessage, ToolNameMap, error) {
	nameMap := make(ToolNameMap)
	toolCallIDs := make(map[string]struct{})
	normalized := make([]ChatMessage, len(messages))
	copy(normalized, messages)

	for msgIdx := range normalized {
		msg := &normalized[msgIdx]
		for callIdx := range msg.ToolCalls {
			call := &msg.ToolCalls[callIdx]
			if call.ID == "" {
				call.ID = newToolCallID()
			}
			toolCallIDs[call.ID] = struct{}{}
			if call.Type == "" {
				call.Type = "function"
			}
			if call.Function != nil && call.Function.Name != "" {
				original := call.Function.Name
				clean := CloakToolName(original)
				call.Function.Name = clean
				if clean != original {
					nameMap[clean] = original
				}
			}
		}
	}

	for _, msg := range normalized {
		if msg.Role != "tool" || msg.ToolCallID == "" {
			continue
		}
		if _, ok := toolCallIDs[msg.ToolCallID]; !ok {
			return nil, nil, fmt.Errorf("tool result references unknown tool_call_id %q", msg.ToolCallID)
		}
	}

	return normalized, nameMap, nil
}

func CloakToolName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "tool"
	}
	name = invalidToolNameChars.ReplaceAllString(name, "_")
	if len(name) > 64 {
		name = name[:64]
	}
	return name
}

func RestoreToolName(name string, nameMap ToolNameMap) string {
	if original, ok := nameMap[name]; ok {
		return original
	}
	return name
}

func newToolCallID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "call_generated"
	}
	return "call_" + hex.EncodeToString(b[:])
}

type StreamAccumulator struct {
	Response ChatResponse
}

func NewStreamAccumulator() *StreamAccumulator {
	return &StreamAccumulator{Response: ChatResponse{Object: "chat.completion"}}
}

func (a *StreamAccumulator) Add(chunk ChatChunk) {
	if a.Response.ID == "" {
		a.Response.ID = chunk.ID
	}
	if a.Response.Created == 0 {
		a.Response.Created = chunk.Created
	}
	if a.Response.Model == "" {
		a.Response.Model = chunk.Model
	}
	if a.Response.SystemFingerprint == "" && chunk.SystemFingerprint != "" {
		a.Response.SystemFingerprint = chunk.SystemFingerprint
	}
	if a.Response.ServiceTier == "" && chunk.ServiceTier != "" {
		a.Response.ServiceTier = chunk.ServiceTier
	}
	if chunk.Usage != nil {
		a.Response.Usage = chunk.Usage
	}
	for _, choice := range chunk.Choices {
		for len(a.Response.Choices) <= choice.Index {
			a.Response.Choices = append(a.Response.Choices, ChatChoice{Index: len(a.Response.Choices), Message: ChatMessage{Role: "assistant"}})
		}
		out := &a.Response.Choices[choice.Index]
		if choice.Delta.Role != "" {
			out.Message.Role = choice.Delta.Role
		}
		switch content := choice.Delta.Content.(type) {
		case string:
			existing, _ := out.Message.Content.(string)
			out.Message.Content = existing + content
		case nil:
		default:
			raw, _ := json.Marshal(content)
			existing, _ := out.Message.Content.(string)
			out.Message.Content = existing + string(raw)
		}
		if len(choice.Delta.ToolCalls) > 0 {
			out.Message.ToolCalls = mergeToolCallDeltas(out.Message.ToolCalls, choice.Delta.ToolCalls)
		}
		if choice.Delta.Reasoning != "" {
			out.Message.Reasoning = append(out.Message.Reasoning, []byte(choice.Delta.Reasoning)...)
		}
		if choice.Delta.Thinking != "" {
			out.Message.Thinking = append(out.Message.Thinking, []byte(choice.Delta.Thinking)...)
		}
		if choice.Delta.ReasoningContent != "" {
			out.Message.ReasoningContent += choice.Delta.ReasoningContent
		}
		if choice.FinishReason != nil {
			out.FinishReason = *choice.FinishReason
		}
	}
}

func (a *StreamAccumulator) ChatResponse() ChatResponse {
	return a.Response
}

func mergeToolCallDeltas(existing []ToolCall, deltas []ToolCallDelta) []ToolCall {
	for _, delta := range deltas {
		for len(existing) <= delta.Index {
			existing = append(existing, ToolCall{Type: "function", Function: &FunctionCall{}})
		}
		call := &existing[delta.Index]
		if delta.ID != "" {
			call.ID = delta.ID
		}
		if delta.Type != "" {
			call.Type = delta.Type
		}
		if delta.Function != nil {
			if call.Function == nil {
				call.Function = &FunctionCall{}
			}
			if delta.Function.Name != "" {
				call.Function.Name = delta.Function.Name
			}
			if len(delta.Function.Arguments) > 0 {
				call.Function.Arguments = append(call.Function.Arguments, delta.Function.Arguments...)
			}
		}
	}
	return existing
}
