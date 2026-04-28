package providers

import (
	"bytes"
	"strings"
	"testing"
)

func TestSSEDecoderMultiLineData(t *testing.T) {
	decoder := NewSSEDecoder(strings.NewReader("id: 1\nevent: delta\ndata: {\ndata: \"x\":1}\n\n"))
	event, err := decoder.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if event.ID != "1" || event.Event != "delta" {
		t.Fatalf("event metadata = %#v", event)
	}
	if event.Data != "{\n\"x\":1}" {
		t.Fatalf("data = %q", event.Data)
	}
}

func TestSSEDecoderLargeData(t *testing.T) {
	large := strings.Repeat("x", 128*1024)
	decoder := NewSSEDecoder(strings.NewReader("data: " + large + "\n\n"))
	event, err := decoder.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if len(event.Data) != len(large) {
		t.Fatalf("data len = %d, want %d", len(event.Data), len(large))
	}
}

func TestSSEWriter(t *testing.T) {
	var buf bytes.Buffer
	writer := NewSSEWriter(&buf)
	if err := writer.WriteEvent(SSEEvent{ID: "7", Event: "delta", Data: "a\nb"}); err != nil {
		t.Fatalf("WriteEvent() error = %v", err)
	}
	want := "id: 7\nevent: delta\ndata: a\ndata: b\n\n"
	if buf.String() != want {
		t.Fatalf("SSE output = %q, want %q", buf.String(), want)
	}
}

func TestAccumulateSSEData(t *testing.T) {
	items, err := AccumulateSSEData(strings.NewReader("data: {\"delta\":\"a\"}\n\ndata: {\"delta\":\"b\"}\n\ndata: [DONE]\n\n"))
	if err != nil {
		t.Fatalf("AccumulateSSEData() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items len = %d, want 2", len(items))
	}
	if string(items[0]) != `{"delta":"a"}` {
		t.Fatalf("first item = %s", items[0])
	}
}
