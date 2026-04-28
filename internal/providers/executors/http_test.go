package executors

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPExecutor_Execute(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	executor := NewHTTPExecutor(nil)
	req, _ := http.NewRequest("GET", server.URL, nil)
	
	resp, err := executor.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestHTTPExecutor_ExecuteJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"message":"hello","count":42}`))
	}))
	defer server.Close()

	executor := NewHTTPExecutor(nil)
	req, _ := http.NewRequest("GET", server.URL, nil)
	
	var result struct {
		Message string `json:"message"`
		Count   int    `json:"count"`
	}

	err := executor.ExecuteJSON(context.Background(), req, &result)
	if err != nil {
		t.Fatalf("ExecuteJSON() error = %v", err)
	}

	if result.Message != "hello" {
		t.Errorf("Message = %s, want hello", result.Message)
	}
	if result.Count != 42 {
		t.Errorf("Count = %d, want 42", result.Count)
	}
}

func TestSSEParser(t *testing.T) {
	input := `event: message
data: first line
data: second line

data: single line event

event: custom
data: custom event data
id: 123

: this is a comment
data: after comment

`

	parser := NewSSEParser(strings.NewReader(input))

	// First event
	event, err := parser.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if event.Event != "message" {
		t.Errorf("Event = %s, want message", event.Event)
	}
	if event.Data != "first line\nsecond line" {
		t.Errorf("Data = %q, want multi-line", event.Data)
	}

	// Second event
	event, err = parser.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if event.Data != "single line event" {
		t.Errorf("Data = %s, want single line event", event.Data)
	}

	// Third event
	event, err = parser.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if event.Event != "custom" {
		t.Errorf("Event = %s, want custom", event.Event)
	}
	if event.ID != "123" {
		t.Errorf("ID = %s, want 123", event.ID)
	}

	// Fourth event (after comment)
	event, err = parser.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if event.Data != "after comment" {
		t.Errorf("Data = %s, want after comment", event.Data)
	}

	// EOF
	_, err = parser.Next()
	if err != io.EOF {
		t.Errorf("Expected EOF, got %v", err)
	}
}

func TestNDJSONParser(t *testing.T) {
	input := `{"type":"start","id":1}
{"type":"data","value":"hello"}

{"type":"end","id":2}
`

	parser := NewNDJSONParser(strings.NewReader(input))

	var result struct {
		Type  string `json:"type"`
		ID    int    `json:"id,omitempty"`
		Value string `json:"value,omitempty"`
	}

	// First line
	err := parser.Next(&result)
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if result.Type != "start" || result.ID != 1 {
		t.Errorf("First line = %+v, want start/1", result)
	}

	// Second line
	err = parser.Next(&result)
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if result.Type != "data" || result.Value != "hello" {
		t.Errorf("Second line = %+v, want data/hello", result)
	}

	// Third line (after empty line)
	err = parser.Next(&result)
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if result.Type != "end" || result.ID != 2 {
		t.Errorf("Third line = %+v, want end/2", result)
	}

	// EOF
	err = parser.Next(&result)
	if err != io.EOF {
		t.Errorf("Expected EOF, got %v", err)
	}
}

func TestParseHTTPError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       []byte
		want       ErrorClassification
	}{
		{
			name:       "429 rate limit",
			statusCode: 429,
			body:       []byte(`{"error":"rate limit exceeded"}`),
			want: ErrorClassification{
				Retryable:   true,
				RateLimited: true,
				Backoff:     true,
				CooldownMs:  60000,
			},
		},
		{
			name:       "401 unauthorized",
			statusCode: 401,
			body:       []byte(`{"error":"invalid token"}`),
			want: ErrorClassification{
				Retryable:   true,
				RefreshAuth: true,
			},
		},
		{
			name:       "403 forbidden",
			statusCode: 403,
			body:       []byte(`{"error":"access denied"}`),
			want: ErrorClassification{
				AuthInvalid: true,
			},
		},
		{
			name:       "503 service unavailable",
			statusCode: 503,
			body:       []byte(`{"error":"service temporarily unavailable"}`),
			want: ErrorClassification{
				Retryable: true,
				Backoff:   true,
			},
		},
		{
			name:       "402 quota exceeded",
			statusCode: 402,
			body:       []byte(`{"error":"quota exceeded"}`),
			want: ErrorClassification{
				QuotaExhausted: true,
			},
		},
		{
			name:       "body contains quota",
			statusCode: 400,
			body:       []byte(`{"error":"insufficient quota"}`),
			want: ErrorClassification{
				QuotaExhausted: true,
			},
		},
		{
			name:       "body contains model locked",
			statusCode: 400,
			body:       []byte(`{"error":"model is locked"}`),
			want: ErrorClassification{
				ModelLocked: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseHTTPError(tt.statusCode, tt.body)
			
			if got.Retryable != tt.want.Retryable {
				t.Errorf("Retryable = %v, want %v", got.Retryable, tt.want.Retryable)
			}
			if got.RateLimited != tt.want.RateLimited {
				t.Errorf("RateLimited = %v, want %v", got.RateLimited, tt.want.RateLimited)
			}
			if got.RefreshAuth != tt.want.RefreshAuth {
				t.Errorf("RefreshAuth = %v, want %v", got.RefreshAuth, tt.want.RefreshAuth)
			}
			if got.AuthInvalid != tt.want.AuthInvalid {
				t.Errorf("AuthInvalid = %v, want %v", got.AuthInvalid, tt.want.AuthInvalid)
			}
			if got.QuotaExhausted != tt.want.QuotaExhausted {
				t.Errorf("QuotaExhausted = %v, want %v", got.QuotaExhausted, tt.want.QuotaExhausted)
			}
			if got.ModelLocked != tt.want.ModelLocked {
				t.Errorf("ModelLocked = %v, want %v", got.ModelLocked, tt.want.ModelLocked)
			}
		})
	}
}

func TestSSEParser_LargeEvent(t *testing.T) {
	// Test with large data payload
	largeData := strings.Repeat("x", 100000)
	input := "data: " + largeData + "\n\n"

	parser := NewSSEParser(bytes.NewReader([]byte(input)))
	event, err := parser.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}

	if event.Data != largeData {
		t.Errorf("Large data not preserved, got length %d, want %d", len(event.Data), len(largeData))
	}
}
