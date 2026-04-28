package executors

import (
	"strings"
	"testing"
)

func TestBuildURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		path    string
		want    string
	}{
		{name: "joins relative path", baseURL: "https://example.com/v1/", path: "chat/completions", want: "https://example.com/v1/chat/completions"},
		{name: "preserves absolute path", baseURL: "https://example.com/v1", path: "https://upstream.test/x", want: "https://upstream.test/x"},
		{name: "empty path", baseURL: "https://example.com/v1/", path: "", want: "https://example.com/v1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := BuildURL(tc.baseURL, tc.path); got != tc.want {
				t.Fatalf("BuildURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRedactSecret(t *testing.T) {
	if got := RedactSecret(""); got != "" {
		t.Fatalf("empty value should remain empty")
	}
	if got := RedactSecret("abc"); got != "***" {
		t.Fatalf("short value redaction mismatch: %q", got)
	}
	if got := RedactSecret("abcdefghi"); got != "abc***hi" {
		t.Fatalf("long value redaction mismatch: %q", got)
	}
}

func TestRedactText(t *testing.T) {
	in := "Authorization: Bearer sk-super-secret-token x-api-key: very-secret cookie: session=abcd1234"
	out := RedactText(in)
	if out == in {
		t.Fatalf("expected redaction to change input")
	}
	if out == "" {
		t.Fatalf("expected non-empty output")
	}
	if containsAny(out, []string{"super-secret-token", "very-secret", "session=abcd1234"}) {
		t.Fatalf("sensitive values leaked in redacted output: %s", out)
	}
}

func containsAny(value string, needles []string) bool {
	for _, needle := range needles {
		if needle != "" && strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
