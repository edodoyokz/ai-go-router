package providers

import (
	"bufio"
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestSSEScanner(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		want     []string
		wantErr  bool
	}{
		{
			name:  "single event",
			input: "data: {\"content\":\"hello\"}\n\n",
			want:  []string{"data: {\"content\":\"hello\"}"},
		},
		{
			name: "multiple events",
			input: `data: {"content":"hello"}

data: {"content":"world"}

`,
			want: []string{
				"data: {\"content\":\"hello\"}",
				"data: {\"content\":\"world\"}",
			},
		},
		{
			name: "event with field",
			input: "id: 1\ndata: {\"content\":\"test\"}\n\n",
			want: []string{"id: 1\ndata: {\"content\":\"test\"}"},
		},
		{
			name: "event with comment",
			input: ": comment\ndata: {\"content\":\"test\"}\n\n",
			want: []string{": comment\ndata: {\"content\":\"test\"}"},
		},
		{
			name:  "done signal",
			input: "data: [DONE]\n\n",
			want:  []string{"data: [DONE]"},
		},
		{
			name:  "empty input",
			input: "",
			want:  []string(nil),
		},
		{
			name:  "incomplete event",
			input: "data: {\"content\":\"incomplete",
			want:  []string{"data: {\"content\":\"incomplete"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scanner := newSSEReader(strings.NewReader(tt.input))
			var got []string
			for scanner.Scan() {
				got = append(got, scanner.Text())
			}
			if err := scanner.Err(); err != nil && !tt.wantErr {
				t.Errorf("SSEReader.Scan() error = %v", err)
			}

			if len(got) != len(tt.want) {
				t.Errorf("SSEReader.Scan() got %d events, want %d", len(got), len(tt.want))
				return
			}

			for i, event := range got {
				if event != tt.want[i] {
					t.Errorf("Event %d = %q, want %q", i, event, tt.want[i])
				}
			}
		})
	}
}

func TestSSEReader_Empty(t *testing.T) {
	scanner := newSSEReader(strings.NewReader(""))
	count := 0
	for scanner.Scan() {
		count++
	}
	if count != 0 {
		t.Errorf("Expected 0 events from empty input, got %d", count)
	}
}

func TestSSEReader_ContinuousStream(t *testing.T) {
	input := `data: {"delta":"H"}

data: {"delta":"e"}

data: {"delta":"l"}

data: {"delta":"l"}

data: {"delta":"o"}

data: [DONE]

`
	scanner := newSSEReader(strings.NewReader(input))
	var events []string
	for scanner.Scan() {
		events = append(events, scanner.Text())
	}

	if len(events) != 6 {
		t.Errorf("Expected 6 events, got %d", len(events))
	}
}

func newSSEReader(r io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(r)
	scanner.Split(splitSSEEvents)
	return scanner
}

func splitSSEEvents(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}

	if i := bytes.Index(data, []byte("\n\n")); i >= 0 {
		token = bytes.TrimRight(data[:i], "\n")
		return i + 2, token, nil
	}

	if atEOF {
		token = bytes.TrimRight(data, "\n")
		if len(token) == 0 {
			return len(data), nil, nil
		}
		return len(data), token, nil
	}

	return 0, nil, nil
}
