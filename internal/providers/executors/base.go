package executors

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

// BuildURL joins base URL and path for executor requests.
func BuildURL(baseURL, path string) string {
	trimmedBase := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath == "" {
		return trimmedBase
	}
	if strings.HasPrefix(trimmedPath, "http://") || strings.HasPrefix(trimmedPath, "https://") {
		return trimmedPath
	}
	if !strings.HasPrefix(trimmedPath, "/") {
		trimmedPath = "/" + trimmedPath
	}
	return trimmedBase + trimmedPath
}

// BuildHeaders merges default and override headers.
func BuildHeaders(defaults, overrides map[string]string) http.Header {
	headers := make(http.Header)
	for k, v := range defaults {
		headers.Set(k, v)
	}
	for k, v := range overrides {
		headers.Set(k, v)
	}
	return headers
}

// RedactSecret masks token-like values for logs/errors.
func RedactSecret(value string) string {
	v := strings.TrimSpace(value)
	if v == "" {
		return ""
	}
	if len(v) <= 6 {
		return "***"
	}
	return fmt.Sprintf("%s***%s", v[:3], v[len(v)-2:])
}

var redactPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(authorization\s*:\s*(?:bearer|basic)\s+)([^\s,;]+)`),
	regexp.MustCompile(`(?i)(x-api-key\s*:\s*)([^\s,;]+)`),
	regexp.MustCompile(`(?i)(api[_-]?key["'=:\s]+)([^"\s,;]+)`),
	regexp.MustCompile(`(?i)(access[_-]?token["'=:\s]+)([^"\s,;]+)`),
	regexp.MustCompile(`(?i)(refresh[_-]?token["'=:\s]+)([^"\s,;]+)`),
	regexp.MustCompile(`(?i)(cookie\s*:\s*)([^\n\r]+)`),
}

// RedactText masks common secret patterns from logs/errors.
func RedactText(input string) string {
	out := input
	for _, pattern := range redactPatterns {
		out = pattern.ReplaceAllStringFunc(out, func(match string) string {
			groups := pattern.FindStringSubmatch(match)
			if len(groups) < 3 {
				return "***"
			}
			return groups[1] + RedactSecret(groups[2])
		})
	}
	return out
}
