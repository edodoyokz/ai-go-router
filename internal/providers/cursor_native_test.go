package providers

import (
	"encoding/json"
	"testing"
	"time"
)

func TestCursorBuildHeaders(t *testing.T) {
	headers, err := cursorBuildHeaders("token-123", "machine-xyz", true, time.Unix(1710000000, 0))
	if err != nil {
		t.Fatalf("cursorBuildHeaders error: %v", err)
	}
	for _, key := range []string{
		"authorization",
		"x-client-key",
		"x-cursor-checksum",
		"x-cursor-client-version",
		"x-session-id",
	} {
		if headers[key] == "" {
			t.Fatalf("expected header %s", key)
		}
	}
	if headers["authorization"] != "Bearer token-123" {
		t.Fatalf("authorization=%q", headers["authorization"])
	}
	if headers["x-ghost-mode"] != "true" {
		t.Fatalf("ghost mode=%q", headers["x-ghost-mode"])
	}
}

func TestCursorGenerateChecksumStableForTimestamp(t *testing.T) {
	now := time.Unix(1710000000, 0)
	a := cursorGenerateChecksum("machine-xyz", now)
	b := cursorGenerateChecksum("machine-xyz", now)
	if a != b {
		t.Fatalf("checksum should be stable for same input: %q != %q", a, b)
	}
	if len(a) <= len("machine-xyz") || a[len(a)-len("machine-xyz"):] != "machine-xyz" {
		t.Fatalf("checksum format=%q", a)
	}
}

func TestCursorParseConnectRPCFrame(t *testing.T) {
	payload := []byte("hello")
	frame := append([]byte{0, 0, 0, 0, byte(len(payload))}, payload...)
	parsed, ok := cursorParseConnectRPCFrame(frame)
	if !ok {
		t.Fatal("expected frame to parse")
	}
	if string(parsed.Payload) != "hello" || parsed.Consumed != len(frame) {
		t.Fatalf("parsed=%+v", parsed)
	}
}

func TestCursorExtractResponseTextOnly(t *testing.T) {
	responseText := encodeProtoField(1, encodeProtoString("hello from cursor"))
	responseEnvelope := encodeProtoField(2, responseText)
	decoded := cursorExtractResponse(responseEnvelope)
	if decoded.Text != "hello from cursor" {
		t.Fatalf("decoded=%+v", decoded)
	}
}

func TestCursorExtractResponseToolCall(t *testing.T) {
	tool := joinProto(
		encodeProtoField(3, encodeProtoString("call_123\nmc_abc")),
		encodeProtoField(9, encodeProtoString("read_file")),
		encodeProtoField(10, encodeProtoString(`{"path":"main.go"}`)),
		encodeProtoVarintField(11, 1),
	)
	decoded := cursorExtractResponse(encodeProtoField(1, tool))
	if decoded.ToolCall == nil {
		t.Fatalf("expected tool call, got %+v", decoded)
	}
	if decoded.ToolCall.Function == nil || decoded.ToolCall.Function.Name != "read_file" {
		t.Fatalf("toolCall=%+v", decoded.ToolCall)
	}
	var args map[string]any
	if err := json.Unmarshal(decoded.ToolCall.Function.Arguments, &args); err != nil {
		t.Fatalf("arguments json: %v", err)
	}
	if args["path"] != "main.go" || !decoded.IsLast {
		t.Fatalf("decoded=%+v args=%#v", decoded, args)
	}
}

func encodeProtoField(fieldNum int, payload []byte) []byte {
	tag := uint64(fieldNum<<3 | 2)
	return joinProto(encodeProtoVarint(tag), encodeProtoVarint(uint64(len(payload))), payload)
}

func encodeProtoString(value string) []byte {
	return []byte(value)
}

func encodeProtoVarintField(fieldNum int, value uint64) []byte {
	tag := uint64(fieldNum<<3 | 0)
	return joinProto(encodeProtoVarint(tag), encodeProtoVarint(value))
}

func encodeProtoVarint(value uint64) []byte {
	buf := make([]byte, 0, 10)
	for value >= 0x80 {
		buf = append(buf, byte(value)|0x80)
		value >>= 7
	}
	buf = append(buf, byte(value))
	return buf
}

func joinProto(parts ...[]byte) []byte {
	total := 0
	for _, p := range parts {
		total += len(p)
	}
	out := make([]byte, 0, total)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}
