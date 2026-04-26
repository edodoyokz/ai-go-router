package translator

import (
	"net/http"
	"net/url"
	"testing"
)

func TestDetectFormat(t *testing.T) {
	tests := []struct {
		name string
		path string
		body map[string]interface{}
		want string
	}{
		{
			name: "OpenAI chat completions endpoint",
			path: "/v1/chat/completions",
			body: nil,
			want: FormatOpenAI,
		},
		{
			name: "Claude messages endpoint",
			path: "/v1/messages",
			body: nil,
			want: FormatClaude,
		},
		{
			name: "OpenAI responses endpoint",
			path: "/v1/responses",
			body: nil,
			want: FormatOpenAIResp,
		},
		{
			name: "default to OpenAI for unknown endpoint",
			path: "/unknown/endpoint",
			body: nil,
			want: FormatOpenAI,
		},
		{
			name: "Claude format detection from body - system field",
			path: "/unknown",
			body: map[string]interface{}{
				"max_tokens": 4096,
				"messages": []interface{}{
					map[string]interface{}{"role": "user", "content": "test"},
				},
				"system": "You are helpful",
			},
			want: FormatClaude,
		},
		{
			name: "Claude format detection from body - anthropic_version",
			path: "/unknown",
			body: map[string]interface{}{
				"anthropic_version": "2023-06-01",
			},
			want: FormatClaude,
		},
		{
			name: "OpenAI format detection from body - no system field",
			path: "/unknown",
			body: map[string]interface{}{
				"max_tokens": 4096,
				"messages": []interface{}{
					map[string]interface{}{"role": "user", "content": "test"},
				},
			},
			want: FormatOpenAI,
		},
		{
			name: "endpoint takes precedence over body",
			path: "/v1/chat/completions",
			body: map[string]interface{}{
				"system": "You are helpful",
			},
			want: FormatOpenAI,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectFormat(tt.path, tt.body)
			if got != tt.want {
				t.Errorf("DetectFormat() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDetectFormatFromRequest(t *testing.T) {
	tests := []struct {
		name string
		path string
		body map[string]interface{}
		want string
	}{
		{
			name: "chat completions request",
			path: "/v1/chat/completions",
			body: map[string]interface{}{
				"model": "gpt-4",
				"messages": []interface{}{
					map[string]interface{}{"role": "user", "content": "test"},
				},
			},
			want: FormatOpenAI,
		},
		{
			name: "messages request",
			path: "/v1/messages",
			body: map[string]interface{}{
				"model": "claude-3-opus",
				"messages": []interface{}{
					map[string]interface{}{"role": "user", "content": "test"},
				},
			},
			want: FormatClaude,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, _ := url.Parse(tt.path)
			req := &http.Request{
				URL: u,
			}
			got := DetectFormatFromRequest(req, tt.body)
			if got != tt.want {
				t.Errorf("DetectFormatFromRequest() = %v, want %v", got, tt.want)
			}
		})
	}
}
