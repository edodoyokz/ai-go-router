package translator

import (
	"net/http"
	"strings"
)

// DetectFormat identifies the source format from endpoint path and request body
func DetectFormat(path string, body map[string]interface{}) string {
	// Check endpoint path for format hints
	if strings.Contains(path, "/v1/messages") {
		return FormatClaude
	}
	if strings.Contains(path, "/v1/responses") {
		return FormatOpenAIResp
	}
	if strings.Contains(path, "/v1/chat/completions") {
		return FormatOpenAI
	}

	// Fallback to body content detection
	if body != nil {
		// Check for Claude-specific fields
		if _, hasMaxTokens := body["max_tokens"]; hasMaxTokens {
			if _, hasMessages := body["messages"]; hasMessages {
				// Both OpenAI and Claude have messages, check for Claude-specific fields
				if _, hasSystem := body["system"]; hasSystem {
					return FormatClaude
				}
			}
		}
		
		// Check for Anthropic-specific headers in body
		if _, hasAnthropicVersion := body["anthropic_version"]; hasAnthropicVersion {
			return FormatClaude
		}
	}

	// Default to OpenAI format
	return FormatOpenAI
}

// DetectFormatFromRequest extracts format from HTTP request
func DetectFormatFromRequest(r *http.Request, body map[string]interface{}) string {
	return DetectFormat(r.URL.Path, body)
}
