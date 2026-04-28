package providers

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
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/edodoyokz/ai-go-router/internal/config"
)

const (
	githubCopilotBaseURL     = "https://api.githubcopilot.com"
	githubTokenURL           = "https://api.github.com/copilot_internal/v2/token"
	githubOAuthTokenURL      = "https://github.com/login/oauth/access_token"
	githubAPIVersion         = "2025-04-01"
	githubVSCodeVersion      = "1.85.0"
	githubCopilotChatVersion = "0.26.7"
	githubUserAgent          = "GitHubCopilotChat/0.26.7"
)

type GitHubExecutor struct {
	cfg              config.ProviderConfig
	errorCfg         config.ErrorConfig
	client           *http.Client
	knownCodexModels map[string]bool
}

type githubRefreshedCredentials struct {
	AccessToken           string
	RefreshToken          string
	ExpiresIn             int
	CopilotToken          string
	CopilotTokenExpiresAt time.Time
}

func NewGitHubExecutor(cfg config.ProviderConfig, errorCfg config.ErrorConfig) (Executor, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = githubCopilotBaseURL
	}
	if err := validateGitHubCredentials(cfg); err != nil {
		return nil, err
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

func (e *GitHubExecutor) ChatCompletion(ctx context.Context, req ChatRequest, model string) (ChatResponse, error) {
	if e.knownCodexModels[model] {
		return e.chatCompletionWithResponses(ctx, req, model)
	}
	resp, err := e.chatCompletionStandard(ctx, req, model)
	if err != nil {
		if strings.Contains(err.Error(), "not accessible via the /chat/completions endpoint") || strings.Contains(err.Error(), "The requested model is not supported") {
			e.knownCodexModels[model] = true
			return e.chatCompletionWithResponses(ctx, req, model)
		}
		return ChatResponse{}, err
	}
	return resp, nil
}

func (e *GitHubExecutor) StreamChatCompletion(ctx context.Context, req ChatRequest, model string) (<-chan ChatChunk, error) {
	if e.knownCodexModels[model] {
		return e.streamChatCompletionWithResponses(ctx, req, model)
	}
	ch, err := e.streamChatCompletionStandard(ctx, req, model)
	if err != nil {
		if strings.Contains(err.Error(), "not accessible via the /chat/completions endpoint") || strings.Contains(err.Error(), "The requested model is not supported") {
			e.knownCodexModels[model] = true
			return e.streamChatCompletionWithResponses(ctx, req, model)
		}
		return nil, err
	}
	return ch, nil
}

func (e *GitHubExecutor) chatCompletionStandard(ctx context.Context, req ChatRequest, model string) (ChatResponse, error) {
	body := e.transformRequest(model, req)
	body = e.sanitizeMessagesForChatCompletions(body)
	respBody, err := e.doJSONRequest(ctx, e.chatCompletionsURL(), false, body)
	if err != nil {
		return ChatResponse{}, err
	}
	var chatResp ChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return ChatResponse{}, fmt.Errorf("unmarshal response: %w", err)
	}
	return chatResp, nil
}

func (e *GitHubExecutor) streamChatCompletionStandard(ctx context.Context, req ChatRequest, model string) (<-chan ChatChunk, error) {
	body := e.transformRequest(model, req)
	body = e.sanitizeMessagesForChatCompletions(body)
	body["stream"] = true
	return e.doStreamRequest(ctx, e.chatCompletionsURL(), body)
}

func (e *GitHubExecutor) chatCompletionWithResponses(ctx context.Context, req ChatRequest, model string) (ChatResponse, error) {
	body := e.transformRequest(model, req)
	respBody, err := e.doJSONRequest(ctx, e.responsesURL(), false, body)
	if err != nil {
		return ChatResponse{}, err
	}
	var chatResp ChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return ChatResponse{}, fmt.Errorf("unmarshal response: %w", err)
	}
	return chatResp, nil
}

func (e *GitHubExecutor) streamChatCompletionWithResponses(ctx context.Context, req ChatRequest, model string) (<-chan ChatChunk, error) {
	body := e.transformRequest(model, req)
	body["stream"] = true
	return e.doStreamRequest(ctx, e.responsesURL(), body)
}

func (e *GitHubExecutor) doJSONRequest(ctx context.Context, url string, stream bool, body map[string]interface{}) ([]byte, error) {
	return e.doJSONRequestAttempt(ctx, url, stream, body, true)
}

func (e *GitHubExecutor) doJSONRequestAttempt(ctx context.Context, url string, stream bool, body map[string]interface{}, allowRefresh bool) ([]byte, error) {
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	for k, v := range e.buildHeaders(stream) {
		req.Header.Set(k, v)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized && allowRefresh {
			if refreshed, err := e.RefreshCredentials(ctx); err == nil {
				e.applyRefreshedCredentials(refreshed)
				return e.doJSONRequestAttempt(ctx, url, stream, body, false)
			}
		}
		return nil, ClassifyHTTPError(resp.StatusCode, e.cfg.Name, fmt.Sprint(body["model"]), string(respBody), e.errorCfg)
	}
	return respBody, nil
}

func (e *GitHubExecutor) doStreamRequest(ctx context.Context, url string, body map[string]interface{}) (<-chan ChatChunk, error) {
	return e.doStreamRequestAttempt(ctx, url, body, true)
}

func (e *GitHubExecutor) doStreamRequestAttempt(ctx context.Context, url string, body map[string]interface{}, allowRefresh bool) (<-chan ChatChunk, error) {
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	for k, v := range e.buildHeaders(true) {
		req.Header.Set(k, v)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == http.StatusUnauthorized && allowRefresh {
			if refreshed, err := e.RefreshCredentials(ctx); err == nil {
				e.applyRefreshedCredentials(refreshed)
				return e.doStreamRequestAttempt(ctx, url, body, false)
			}
		}
		return nil, ClassifyHTTPError(resp.StatusCode, e.cfg.Name, fmt.Sprint(body["model"]), string(respBody), e.errorCfg)
	}
	ch := make(chan ChatChunk, 10)
	go e.streamReader(resp.Body, ch)
	return ch, nil
}

func validateGitHubCredentials(cfg config.ProviderConfig) error {
	hasToken := strings.TrimSpace(cfg.APIKey) != ""
	hasRefresh := false
	hasClient := false
	if v, ok := cfg.ProviderSpecificData["client_id"].(string); ok && strings.TrimSpace(v) != "" {
		hasClient = true
	}
	if v, ok := cfg.ProviderSpecificData["clientId"].(string); ok && strings.TrimSpace(v) != "" {
		hasClient = true
	}
	for _, account := range cfg.Accounts {
		if strings.TrimSpace(account.AccessToken) != "" {
			hasToken = true
		}
		if strings.TrimSpace(account.RefreshToken) != "" {
			hasRefresh = true
		}
		if account.ProviderSpecificData != nil {
			if token, ok := account.ProviderSpecificData["copilotToken"].(string); ok && strings.TrimSpace(token) != "" {
				hasToken = true
			}
		}
	}
	if !hasToken && !(hasRefresh && hasClient) {
		return fmt.Errorf("github credentials incomplete: provide api_key/access_token/copilotToken, or refresh_token with client_id")
	}
	return nil
}

func (e *GitHubExecutor) buildHeaders(stream bool) map[string]string {
	token := e.getToken()
	headers := map[string]string{
		"Authorization":                       "Bearer " + token,
		"Content-Type":                        "application/json",
		"copilot-integration-id":              "vscode-chat",
		"editor-version":                      "vscode/" + githubVSCodeVersion,
		"editor-plugin-version":               "copilot-chat/" + githubCopilotChatVersion,
		"user-agent":                          githubUserAgent,
		"openai-intent":                       "conversation-panel",
		"x-github-api-version":                githubAPIVersion,
		"x-request-id":                        generateGitHubRequestID(),
		"x-vscode-user-agent-library-version": "electron-fetch",
		"X-Initiator":                         "user",
	}
	if stream {
		headers["Accept"] = "text/event-stream"
	} else {
		headers["Accept"] = "application/json"
	}
	for k, v := range e.cfg.Headers {
		headers[k] = v
	}
	return headers
}

func (e *GitHubExecutor) getToken() string {
	if len(e.cfg.Accounts) > 0 {
		account := e.cfg.Accounts[0]
		if account.ProviderSpecificData != nil {
			if copilotToken, ok := account.ProviderSpecificData["copilotToken"].(string); ok && copilotToken != "" {
				return copilotToken
			}
		}
		if account.AccessToken != "" {
			return account.AccessToken
		}
	}
	return e.cfg.APIKey
}

func (e *GitHubExecutor) RefreshCopilotToken(ctx context.Context, githubAccessToken string) (string, time.Time, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.githubCopilotTokenURL(), nil)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+githubAccessToken)
	req.Header.Set("User-Agent", githubUserAgent)
	req.Header.Set("Editor-Version", "vscode/"+githubVSCodeVersion)
	req.Header.Set("Editor-Plugin-Version", "copilot-chat/"+githubCopilotChatVersion)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-github-api-version", githubAPIVersion)
	resp, err := e.client.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", time.Time{}, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	var result struct {
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expires_at"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", time.Time{}, fmt.Errorf("unmarshal response: %w", err)
	}
	return result.Token, time.Unix(result.ExpiresAt, 0), nil
}

func (e *GitHubExecutor) RefreshGitHubToken(ctx context.Context, refreshToken, clientID, clientSecret string) (string, string, int, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", clientID)
	if clientSecret != "" {
		form.Set("client_secret", clientSecret)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.githubOAuthTokenURL(), strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := e.client.Do(req)
	if err != nil {
		return "", "", 0, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", "", 0, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", "", 0, fmt.Errorf("unmarshal response: %w", err)
	}
	if result.RefreshToken == "" {
		result.RefreshToken = refreshToken
	}
	if result.ExpiresIn == 0 {
		result.ExpiresIn = 3600
	}
	return result.AccessToken, result.RefreshToken, result.ExpiresIn, nil
}

func (e *GitHubExecutor) RefreshCredentials(ctx context.Context) (githubRefreshedCredentials, error) {
	if len(e.cfg.Accounts) == 0 {
		return githubRefreshedCredentials{}, fmt.Errorf("no accounts configured")
	}
	account := e.cfg.Accounts[0]
	accessToken := account.AccessToken
	refreshToken := account.RefreshToken
	if accessToken != "" {
		if copilotToken, expiresAt, err := e.RefreshCopilotToken(ctx, accessToken); err == nil {
			return githubRefreshedCredentials{AccessToken: accessToken, RefreshToken: refreshToken, CopilotToken: copilotToken, CopilotTokenExpiresAt: expiresAt}, nil
		}
	}
	clientID := e.githubClientID()
	if refreshToken == "" || clientID == "" {
		return githubRefreshedCredentials{}, fmt.Errorf("no valid credentials for refresh")
	}
	newAccessToken, newRefreshToken, expiresIn, err := e.RefreshGitHubToken(ctx, refreshToken, clientID, e.githubClientSecret())
	if err != nil {
		return githubRefreshedCredentials{}, fmt.Errorf("refresh GitHub token: %w", err)
	}
	result := githubRefreshedCredentials{AccessToken: newAccessToken, RefreshToken: newRefreshToken, ExpiresIn: expiresIn}
	if copilotToken, expiresAt, err := e.RefreshCopilotToken(ctx, newAccessToken); err == nil {
		result.CopilotToken = copilotToken
		result.CopilotTokenExpiresAt = expiresAt
	}
	return result, nil
}

func (e *GitHubExecutor) applyRefreshedCredentials(refreshed githubRefreshedCredentials) {
	if len(e.cfg.Accounts) == 0 {
		e.cfg.Accounts = []config.AccountConfig{{Name: "github"}}
	}
	account := &e.cfg.Accounts[0]
	if refreshed.AccessToken != "" {
		account.AccessToken = refreshed.AccessToken
	}
	if refreshed.RefreshToken != "" {
		account.RefreshToken = refreshed.RefreshToken
	}
	if account.ProviderSpecificData == nil {
		account.ProviderSpecificData = map[string]any{}
	}
	if refreshed.CopilotToken != "" {
		account.ProviderSpecificData["copilotToken"] = refreshed.CopilotToken
	}
	if !refreshed.CopilotTokenExpiresAt.IsZero() {
		account.ProviderSpecificData["copilotTokenExpiresAt"] = refreshed.CopilotTokenExpiresAt.Format(time.RFC3339)
	}
}

func (e *GitHubExecutor) githubOAuthTokenURL() string {
	if v, ok := e.cfg.ProviderSpecificData["token_url"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return githubOAuthTokenURL
}

func (e *GitHubExecutor) githubCopilotTokenURL() string {
	if v, ok := e.cfg.ProviderSpecificData["copilot_token_url"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return githubTokenURL
}

func (e *GitHubExecutor) githubClientID() string {
	if v, ok := e.cfg.ProviderSpecificData["client_id"].(string); ok {
		return v
	}
	if v, ok := e.cfg.ProviderSpecificData["clientId"].(string); ok {
		return v
	}
	return "Iv1.b507a08c87ecfe98"
}

func (e *GitHubExecutor) githubClientSecret() string {
	if v, ok := e.cfg.ProviderSpecificData["client_secret"].(string); ok {
		return v
	}
	if v, ok := e.cfg.ProviderSpecificData["clientSecret"].(string); ok {
		return v
	}
	return ""
}

func (e *GitHubExecutor) transformRequest(model string, req ChatRequest) map[string]interface{} {
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
	if len(req.Stop) > 0 {
		body["stop"] = req.Stop
	}
	if req.ResponseFormat != nil {
		body["response_format"] = req.ResponseFormat
	}
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
	messagesJSON, _ := json.Marshal(messagesRaw)
	var messages []ChatMessage
	if err := json.Unmarshal(messagesJSON, &messages); err != nil {
		return body
	}
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
			foundSystem := false
			for i, msg := range messages {
				if msg.Role == "system" {
					messages[i].Content = systemInstruction + "\n\n" + fmt.Sprint(msg.Content)
					foundSystem = true
					break
				}
			}
			if !foundSystem {
				messages = append([]ChatMessage{{Role: "system", Content: systemInstruction}}, messages...)
			}
			for i := len(messages) - 1; i >= 0; i-- {
				if messages[i].Role == "user" {
					messages[i].Content = "Respond with ONLY raw JSON (no markdown, no backticks, no code blocks): " + fmt.Sprint(messages[i].Content)
					break
				}
			}
		}
	}
	sanitized := make([]ChatMessage, 0, len(messages))
	for _, msg := range messages {
		if msg.Content != nil {
			if parts, ok := msg.Content.([]interface{}); ok {
				cleanParts := make([]interface{}, 0, len(parts))
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
					text := ""
					if t, ok := partMap["text"].(string); ok {
						text = t
					} else if c, ok := partMap["content"].(string); ok {
						text = c
					} else {
						encoded, _ := json.Marshal(part)
						text = string(encoded)
					}
					if text != "" {
						cleanParts = append(cleanParts, map[string]interface{}{"type": "text", "text": text})
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

func (e *GitHubExecutor) streamReader(body io.ReadCloser, ch chan<- ChatChunk) {
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
		var chunk ChatChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		ch <- chunk
	}
}

func (e *GitHubExecutor) chatCompletionsURL() string {
	return joinProviderURL(e.cfg.BaseURL, "/chat/completions")
}

func (e *GitHubExecutor) responsesURL() string {
	return joinProviderURL(e.cfg.BaseURL, "/v1/responses")
}

func joinProviderURL(baseURL, path string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return path
	}
	if strings.HasSuffix(base, "/v1") && strings.HasPrefix(path, "/v1/") {
		return base + strings.TrimPrefix(path, "/v1")
	}
	return base + path
}

func generateGitHubRequestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func init() {
	RegisterExecutor("github", func(cfg config.ProviderConfig, errorCfg config.ErrorConfig) (Executor, error) {
		return NewGitHubExecutor(cfg, errorCfg)
	})
}
