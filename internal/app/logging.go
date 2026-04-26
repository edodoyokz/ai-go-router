package app

import (
	"io"
	"regexp"
	"sync"
)

// redactionRule is a pre-compiled regex pattern with its replacement
type redactionRule struct {
	re          *regexp.Regexp
	replacement string
}

// redactionRules are compiled once at init
var redactionRules = func() []redactionRule {
	patterns := []struct {
		pattern     string
		replacement string
	}{
		{`Bearer [a-zA-Z0-9_\-]{20,}`, "Bearer [REDACTED]"},
		{`sk-[a-zA-Z0-9]{20,}`, "sk-[REDACTED]"},
		{`gh[oprsu]_[a-zA-Z0-9]{36}`, "gh*_[REDACTED]"},
		{`(?i)api[_\-]?key["'\s:=]+[a-zA-Z0-9_\-]{10,}`, "api_key=[REDACTED]"},
		{`(?i)token["'\s:=]+[a-zA-Z0-9_\-]{20,}`, "token=[REDACTED]"},
		{`(?i)secret["'\s:=]+[a-zA-Z0-9_\-]{20,}`, "secret=[REDACTED]"},
		{`(?i)password["'\s:=]+[^\s"']+`, "password=[REDACTED]"},
	}

	rules := make([]redactionRule, 0, len(patterns))
	for _, p := range patterns {
		rules = append(rules, redactionRule{
			re:          regexp.MustCompile(p.pattern),
			replacement: p.replacement,
		})
	}
	return rules
}()

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

	s := string(p)
	for _, rule := range redactionRules {
		s = rule.re.ReplaceAllString(s, rule.replacement)
	}
	return w.writer.Write([]byte(s))
}
