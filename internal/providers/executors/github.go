package executors

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/edodoyokz/ai-go-router/internal/config"
	"github.com/edodoyokz/ai-go-router/internal/providers"
)

const (
	githubCopilotBaseURL     = "https://api.githubcopilot.com"
	githubCopilotResponseURL = "https://api.githubcopilot.com/v1/responses"
	githubTokenURL           = "https://api.github.com/copilot_internal/v2/token"
	githubOAuthTokenURL      = "https://github.com/login/oauth/access_token"
	githubAPIVersion         = "2025-04-01"
	vscodeVersion            = "1.85.0"
	copilotChatVersion       = "0.26.7"
	userAgent                = "GitHubCopilotChat/0.26.7"
)

type GitHubExecutor struct {
	cfg              config.ProviderConfig
	errorCfg         config.ErrorConfig
	client           *http.Client
	knownCodexModels map[string]bool
}

func NewGitHubExecutor(cfg config.ProviderConfig, errorCfg config.ErrorConfig) (providers.Executor, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = githubCopilotBaseURL
	}
	return &GitHubExecutor{
		cfg:              cfg,
		errorCfg:         errorCfg,
		client:           &http.Client{Timeout: 120 * time.Second},
		knownCodexModels: make(map[string]bool),
	}, nil
}

func (e *GitHubExecutor) ProviderID() string {
	return "github"
}

func (e *GitHubExecutor) Supports(kind string) bool {
	return kind == "llm"
}

func (e *GitHubExecutor) ChatCompletion(ctx context.Context, req providers.ChatRequest, model string) (providers.ChatResponse, error) {
	// Check if model needs /responses endpoint
	if e.knownCodexModels[model] {
		return e.chatCompletionWithResponses(ctx, req, model)
	}

	// Try /chat/completions first
	resp, err := e.chatCompletionStandard(ctx, req, model)
	if err != nil {
		// Check if error indicates need for /responses
		if strings.Contains(err.Error(), "not accessible via the /chat/completions endpoint") ||
			strings.Contains(err.Error(), "The requested model is not supported") {
			e.knownCodexModels[model] = true
			return e.chatCompletionWithResponses(ctx, req, model)
		}
		return providers.ChatResponse{}, err
	}
	return resp, nil
}

func (e *GitHubExecutor) StreamChatCompletion(ctx context.Context, req providers.ChatRequest, model string) (<-chan providers.ChatChunk, error) {
	// Check if model needs /responses endpoint
	if e.knownCodexModels[model] {
		return e.streamChatCompletionWithResponses(ctx, req, model)
	}

	// Try /chat/completions first
	ch, err := e.streamChatCompletionStandard(ctx, req, model)
	if err != nil {
		// Check if error indicates need for /responses
		if strings.Contains(err.Error(), "not accessible via the /chat/completions endpoint") ||
			strings.Contains(err.Error(), "The requested model is not supported") {
			e.knownCodexModels[model] = true
			return e.streamChatCompletionWithResponses(ctx, req, model)
		}
		return nil, err
	}
	return ch, nil
}

func (e *GitHubExecutor) chatCompletionStandard(ctx context.Context, req providers.ChatRequest, model string) (providers.ChatResponse, error) {
	url := BuildURL(e.cfg.BaseURL, "/chat/completions")
	headers := e.buildHeaders(false)

	body := e.transformRequest(model, req)
	body = e.sanitizeMessagesForChatCompletions(body)

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return providers.ChatResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return providers.ChatResponse{}, fmt.Errorf("create request: %w", err)
	}

	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return providers.ChatResponse{}, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return providers.ChatResponse{}, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return providers.ChatResponse{}, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp providers.ChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return providers.ChatResponse{}, fmt.Errorf("unmarshal response: %w", err)
	}

	return chatResp, nil
}

func (e *GitHubExecutor) streamChatCompletionStandard(ctx context.Context, req providers.ChatRequest, model string) (<-chan providers.ChatChunk, error) {
	url := BuildURL(e.cfg.BaseURL, "/chat/completions")
	headers := e.buildHeaders(true)

	body := e.transformRequest(model, req)
	body = e.sanitizeMessagesForChatCompletions(body)
	body["stream"] = true

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	ch := make(chan providers.ChatChunk, 10)
	go e.streamReader(resp.Body, ch)
	return ch, nil
}

func (e *GitHubExecutor) chatCompletionWithResponses(ctx context.Context, req providers.ChatRequest, model string) (providers.ChatResponse, error) {
	url := githubCopilotResponseURL
	headers := e.buildHeaders(false)

	// For now, use standard format - full responses translation will be added later
	body := e.transformRequest(model, req)

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return providers.ChatResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return providers.ChatResponse{}, fmt.Errorf("create request: %w", err)
	}

	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return providers.ChatResponse{}, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return providers.ChatResponse{}, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return providers.ChatResponse{}, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp providers.ChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return providers.ChatResponse{}, fmt.Errorf("unmarshal response: %w", err)
	}

	return chatResp, nil
}

func (e *GitHubExecutor) streamChatCompletionWithResponses(ctx context.Context, req providers.ChatRequest, model string) (<-chan providers.ChatChunk, error) {
	url := githubCopilotResponseURL
	headers := e.buildHeaders(true)

	// For now, use standard format - full responses translation will be added later
	body := e.transformRequest(model, req)
	body["stream"] = true

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	ch := make(chan providers.ChatChunk, 10)
	go e.streamReader(resp.Body, ch)
	return ch, nil
}

func (e *GitHubExecutor) buildHeaders(stream bool) map[string]string {
	token := e.getToken()
	requestID := generateRequestID()

	headers := map[string]string{
		"Authorization":                       "Bearer " + token,
		"Content-Type":                        "application/json",
		"copilot-integration-id":              "vscode-chat",
		"editor-version":                      "vscode/" + vscodeVersion,
		"editor-plugin-version":               "copilot-chat/" + copilotChatVersion,
		"user-agent":                          userAgent,
		"openai-intent":                       "conversation-panel",
		"x-github-api-version":                githubAPIVersion,
		"x-request-id":                        requestID,
		"x-vscode-user-agent-library-version": "electron-fetch",
		"X-Initiator":                         "user",
	}

	if stream {
		headers["Accept"] = "text/event-stream"
	} else {
		headers["Accept"] = "application/json"
	}

	// Merge custom headers
	for k, v := range e.cfg.Headers {
		headers[k] = v
	}

	return headers
}

func (e *GitHubExecutor) getToken() string {
	// Try first account's tokens
	if len(e.cfg.Accounts) > 0 {
		account := e.cfg.Accounts[0]
		if copilotToken, ok := account.ProviderSpecificData["copilotToken"].(string); ok && copilotToken != "" {
			return copilotToken
		}
		if account.AccessToken != "" {
			return account.AccessToken
		}
	}
	return e.cfg.APIKey
}

func (e *GitHubExecutor) transformRequest(model string, req providers.ChatRequest) map[string]interface{} {
	body := map[string]interface{}{
		"model":    model,
		"messages": req.Messages,
	}

	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		if e.requiresMaxCompletionTokens(model) {
			body["max_completion_tokens"] = *req.MaxTokens
		} else {
			body["max_tokens"] = *req.MaxTokens
		}
	}

	if req.Temperature != nil && e.supportsTemperature(model) {
		body["temperature"] = *req.Temperature
	}

	if req.TopP != nil {
		body["top_p"] = *req.TopP
	}

	if req.Stop != nil && len(req.Stop) > 0 {
		body["stop"] = req.Stop
	}

	if req.ResponseFormat != nil {
		body["response_format"] = req.ResponseFormat
	}

	// Strip thinking/reasoning for models that don't support it
	if e.supportsThinking(model) {
		if req.Thinking != nil {
			body["thinking"] = req.Thinking
		}
		if req.Reasoning != nil && req.Reasoning.Effort != "" {
			body["reasoning_effort"] = req.Reasoning.Effort
		}
	}

	return body
}

func (e *GitHubExecutor) sanitizeMessagesForChatCompletions(body map[string]interface{}) map[string]interface{} {
	messagesRaw, ok := body["messages"]
	if !ok {
		return body
	}

	// Convert to []providers.ChatMessage
	messagesJSON, _ := json.Marshal(messagesRaw)
	var messages []providers.ChatMessage
	if err := json.Unmarshal(messagesJSON, &messages); err != nil {
		return body
	}

	// Handle response_format for Claude models
	if responseFormat, ok := body["response_format"]; ok && strings.Contains(fmt.Sprint(body["model"]), "claude") {
		systemInstruction := ""
		if rf, ok := responseFormat.(map[string]interface{}); ok {
			if rf["type"] == "json_schema" {
				systemInstruction = "CRITICAL: You must ONLY output raw JSON. Never use markdown code blocks. Never use backticks. Never wrap JSON in triple backticks. Output ONLY the raw JSON object."
			} else if rf["type"] == "json_object" {
				systemInstruction = "CRITICAL: You must ONLY output raw JSON. Never use markdown code blocks. Never use backticks."
			}
		}

		if systemInstruction != "" {
			// Add to system message
			foundSystem := false
			for i, msg := range messages {
				if msg.Role == "system" {
					messages[i].Content = systemInstruction + "\n\n" + fmt.Sprint(msg.Content)
					foundSystem = true
					break
				}
			}
			if !foundSystem {
				messages = append([]providers.ChatMessage{{Role: "system", Content: systemInstruction}}, messages...)
			}

			// Prepend to last user message
			for i := len(messages) - 1; i >= 0; i-- {
				if messages[i].Role == "user" {
					userContent := fmt.Sprint(messages[i].Content)
					messages[i].Content = "Respond with ONLY raw JSON (no markdown, no backticks, no code blocks): " + userContent
					break
				}
			}
		}
	}

	// Sanitize message content
	sanitized := make([]providers.ChatMessage, 0, len(messages))
	for _, msg := range messages {
		// String content is fine
		if msg.Content != nil {
			// Array content: filter unsupported types
			if parts, ok := msg.Content.([]interface{}); ok {
				cleanParts := make([]interface{}, 0)
				for _, part := range parts {
					partMap, ok := part.(map[string]interface{})
					if !ok {
						continue
					}

					partType, _ := partMap["type"].(string)
					if partType == "text" || partType == "image_url" {
						cleanParts = append(cleanParts, part)
						continue
					}

					// Serialize other types as text
					text := ""
					if t, ok := partMap["text"].(string); ok {
						text = t
					} else if c, ok := partMap["content"].(string); ok {
						text = c
					} else {
						jsonBytes, _ := json.Marshal(part)
						text = string(jsonBytes)
					}

					if text != "" {
						cleanParts = append(cleanParts, map[string]interface{}{
							"type": "text",
							"text": text,
						})
					}
				}

				if len(cleanParts) > 0 {
					msg.Content = cleanParts
				} else {
					msg.Content = nil
				}
			}
		}

		sanitized = append(sanitized, msg)
	}

	body["messages"] = sanitized
	return body
}

func (e *GitHubExecutor) requiresMaxCompletionTokens(model string) bool {
	return regexp.MustCompile(`(?i)gpt-5|o[134]-`).MatchString(model)
}

func (e *GitHubExecutor) supportsTemperature(model string) bool {
	return !regexp.MustCompile(`(?i)gpt-5\.4`).MatchString(model)
}

func (e *GitHubExecutor) supportsThinking(model string) bool {
	return !regexp.MustCompile(`(?i)claude`).MatchString(model)
}

func (e *GitHubExecutor) streamReader(body io.ReadCloser, ch chan<- providers.ChatChunk) {
	defer close(ch)
	defer body.Close()

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line == "data: [DONE]" {
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		var chunk providers.ChatChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		ch <- chunk
	}
}

func generateRequestID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func init() {
	providers.RegisterExecutor("github", func(cfg config.ProviderConfig, errorCfg config.ErrorConfig) (providers.Executor, error) {
		return NewGitHubExecutor(cfg, errorCfg)
	})
}
