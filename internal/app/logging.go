package app

import (
	"io"
	"strings"
	"sync"
)

// SecretRedactionWriter wraps an io.Writer and redacts sensitive information
type SecretRedactionWriter struct {
	mu     sync.Mutex
	writer io.Writer
}

func NewSecretRedactionWriter(w io.Writer) io.Writer {
	return &SecretRedactionWriter{writer: w}
}

func (w *SecretRedactionWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	redacted := w.redact(string(p))
	return w.writer.Write([]byte(redacted))
}

func (w *SecretRedactionWriter) redact(s string) string {
	// Redact common secret patterns
	redactions := []struct {
		pattern     string
		replacement string
	}{
		// Bearer tokens
		{`Bearer [a-zA-Z0-9_-]{20,}`, "Bearer [REDACTED]"},
		// OpenAI API keys
		{`sk-[a-zA-Z0-9]{20,}`, "sk-[REDACTED]"},
		// GitHub tokens
		{`gho_[a-zA-Z0-9]{36}`, "gho_[REDACTED]"},
		{`ghp_[a-zA-Z0-9]{36}`, "ghp_[REDACTED]"},
		{`ghu_[a-zA-Z0-9]{36}`, "ghu_[REDACTED]"},
		{`ghs_[a-zA-Z0-9]{36}`, "ghs_[REDACTED]"},
		{`ghr_[a-zA-Z0-9]{36}`, "ghr_[REDACTED]"},
		// Generic API keys
		{`api[_-]?key["\s:=]+[a-zA-Z0-9_-]{10,}`, "api_key=[REDACTED]"},
		{`apikey["\s:=]+[a-zA-Z0-9_-]{10,}`, "apikey=[REDACTED]"},
		{`api-key["\s:=]+[a-zA-Z0-9_-]{10,}`, "api-key=[REDACTED]"},
		// Tokens and secrets
		{`token["\s:=]+[a-zA-Z0-9_-]{20,}`, "token=[REDACTED]"},
		{`secret["\s:=]+[a-zA-Z0-9_-]{20,}`, "secret=[REDACTED]"},
		{`password["\s:=]+[^\s"']+`, "password=[REDACTED]"},
		{`passwd["\s:=]+[^\s"']+`, "passwd=[REDACTED]"},
		{`pwd["\s:=]+[^\s"']+`, "pwd=[REDACTED]"},
	}

	for _, r := range redactions {
		s = strings.ReplaceAll(s, r.pattern, r.replacement)
	}

	return s
}
