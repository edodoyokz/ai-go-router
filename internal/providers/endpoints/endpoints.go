package endpoints

import (
	"net/url"
	"strings"
)

type Style string

const (
	StyleAPIRoot               Style = "api_root"
	StyleFullChatEndpoint      Style = "full_chat_endpoint"
	StyleFullMessagesEndpoint  Style = "full_messages_endpoint"
	StyleFullResponsesEndpoint Style = "full_responses_endpoint"
	StyleTemplate              Style = "template"
)

type Definition struct {
	Style         Style
	ChatPath      string
	MessagesPath  string
	ResponsesPath string
}

func NormalizeBaseURL(baseURL string) string {
	trimmed := strings.TrimSpace(baseURL)
	trimmed = strings.TrimRight(trimmed, "/")
	for _, suffix := range []string{"/chat/completions", "/embeddings", "/audio/speech", "/images/generations", "/v1/messages", "/messages", "/responses", "/v1/responses", "/v1/models", "/models"} {
		if strings.HasSuffix(trimmed, suffix) {
			return strings.TrimSuffix(trimmed, suffix)
		}
	}
	return trimmed
}

func BuildOpenAI(baseURL, path string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(base, path) {
		return base
	}
	if parsed, err := url.Parse(base); err == nil {
		if parsed.Path == "" || parsed.Path == "/" {
			return base + path
		}
	}
	if strings.HasSuffix(base, "/v1") {
		return base + path
	}
	if strings.Contains(base, "/v1/") {
		return NormalizeBaseURL(base) + path
	}
	return base + "/v1" + path
}

func BuildAnthropicMessages(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(base, "/v1/messages") || strings.HasSuffix(base, "/messages") {
		if strings.HasSuffix(base, "/messages") && !strings.HasSuffix(base, "/v1/messages") {
			return strings.TrimSuffix(base, "/messages") + "/v1/messages"
		}
		return base
	}
	if strings.HasSuffix(base, "/v1") {
		return base + "/messages"
	}
	return base + "/v1/messages"
}
