package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"

	"github.com/edodoyokz/ai-go-router/internal/config"
	"github.com/edodoyokz/ai-go-router/internal/providers/endpoints"
)

const qwenDefaultBaseURL = "https://portal.qwen.ai/v1"

var qwenSystemMessage = ChatMessage{
	Role: "system",
	Content: []ContentPart{{
		Type:         "text",
		Text:         "",
		CacheControl: json.RawMessage(`{"type":"ephemeral"}`),
	}},
}

type qwenAccount struct {
	name                 string
	token                string
	providerSpecificData map[string]any
}

// QwenAdapter implements the deprecated-safe Qwen Code runtime.
type QwenAdapter struct {
	name        string
	baseURL     string
	headers     map[string]string
	errorConfig config.ErrorConfig
	httpClients []*http.Client
	proxyIdx    atomic.Uint64
	accounts    []qwenAccount
	fallback    qwenAccount
}

func NewQwenAdapter(cfg config.ProviderConfig, errorConfig config.ErrorConfig, proxyURL string) *QwenAdapter {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = qwenDefaultBaseURL
	}

	accounts := make([]qwenAccount, 0, len(cfg.Accounts))
	for _, account := range cfg.Accounts {
		if !account.Enabled && (account.APIKey != "" || account.AccessToken != "") {
			// Preserve historical configs where enabled was omitted.
			account.Enabled = true
		}
		if !account.Enabled {
			continue
		}
		token := firstNonEmptyString(account.AccessToken, account.APIKey)
		if token == "" {
			continue
		}
		name := account.Name
		if name == "" {
			name = account.ID
		}
		accounts = append(accounts, qwenAccount{name: name, token: token, providerSpecificData: account.ProviderSpecificData})
	}

	fallback := qwenAccount{
		name:                 "default",
		token:                cfg.APIKey,
		providerSpecificData: cfg.ProviderSpecificData,
	}

	clients := []*http.Client{createHTTPClient(proxyURL)}
	if len(cfg.ProxyURLs) > 0 {
		clients = clients[:0]
		for _, purl := range cfg.ProxyURLs {
			clients = append(clients, createHTTPClient(purl))
		}
	}

	return &QwenAdapter{
		name:        cfg.Name,
		baseURL:     baseURL,
		headers:     cfg.Headers,
		errorConfig: errorConfig,
		httpClients: clients,
		accounts:    accounts,
		fallback:    fallback,
	}
}

func (a *QwenAdapter) Name() string {
	return a.name
}

func (a *QwenAdapter) AccountNames() []string {
	names := make([]string, 0, len(a.accounts))
	for _, account := range a.accounts {
		if account.name != "" {
			names = append(names, account.name)
		}
	}
	return names
}

func (a *QwenAdapter) nextClient() *http.Client {
	if len(a.httpClients) == 1 {
		return a.httpClients[0]
	}
	idx := a.proxyIdx.Add(1) - 1
	return a.httpClients[idx%uint64(len(a.httpClients))]
}

func (a *QwenAdapter) selectAccount(ctx context.Context) qwenAccount {
	if accountName, ok := ctx.Value(AccountContextKey).(string); ok && accountName != "" {
		for _, account := range a.accounts {
			if account.name == accountName {
				return account
			}
		}
	}
	if len(a.accounts) > 0 {
		return a.accounts[0]
	}
	return a.fallback
}

func (a *QwenAdapter) ChatCompletion(ctx context.Context, request ChatRequest, model string) (ChatResponse, error) {
	request.Model = model
	account := a.selectAccount(ctx)
	if account.token == "" {
		return ChatResponse{}, NewNonRetryableError(a.name, model, "qwen requires an existing valid OAuth token; the discontinued free-tier flow cannot run without credentials", nil)
	}
	body, err := json.Marshal(a.transformRequest(request, false))
	if err != nil {
		return ChatResponse{}, NewNonRetryableError(a.name, model, "failed to marshal request", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.chatURL(account), bytes.NewReader(body))
	if err != nil {
		return ChatResponse{}, NewNonRetryableError(a.name, model, "failed to create request", err)
	}
	a.applyHeaders(req, account.token, false)

	resp, err := a.nextClient().Do(req)
	if err != nil {
		return ChatResponse{}, NewRetryableError(a.name, model, "network error", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return ChatResponse{}, NewRetryableError(a.name, model, "failed to read response", err)
	}
	if resp.StatusCode != http.StatusOK {
		return ChatResponse{}, a.classifyHTTPError(resp.StatusCode, model, string(respBody))
	}
	var out ChatResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return ChatResponse{}, NewNonRetryableError(a.name, model, "invalid upstream response", err)
	}
	return out, nil
}

func (a *QwenAdapter) StreamChatCompletion(ctx context.Context, request ChatRequest, model string) (<-chan ChatChunk, error) {
	request.Model = model
	request.Stream = true
	account := a.selectAccount(ctx)
	if account.token == "" {
		return nil, NewNonRetryableError(a.name, model, "qwen requires an existing valid OAuth token; the discontinued free-tier flow cannot run without credentials", nil)
	}
	body, err := json.Marshal(a.transformRequest(request, true))
	if err != nil {
		return nil, NewNonRetryableError(a.name, model, "failed to marshal request", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.chatURL(account), bytes.NewReader(body))
	if err != nil {
		return nil, NewNonRetryableError(a.name, model, "failed to create request", err)
	}
	a.applyHeaders(req, account.token, true)

	resp, err := a.nextClient().Do(req)
	if err != nil {
		return nil, NewRetryableError(a.name, model, "network error", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		return nil, a.classifyHTTPError(resp.StatusCode, model, string(respBody))
	}

	ch := make(chan ChatChunk, 10)
	go func() {
		defer resp.Body.Close()
		defer close(ch)
		decoder := NewSSEDecoder(resp.Body)
		for {
			event, err := decoder.Next()
			if err != nil {
				return
			}
			data := strings.TrimSpace(event.Data)
			if data == "" || data == "[DONE]" {
				if data == "[DONE]" {
					return
				}
				continue
			}
			var chunk ChatChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}
			ch <- chunk
		}
	}()
	return ch, nil
}

func (a *QwenAdapter) GetUsage(context.Context) (map[string]interface{}, error) {
	return map[string]interface{}{"unsupported": true, "provider": "qwen", "reason": "qwen usage quota is not exposed by the Go router"}, nil
}

func (a *QwenAdapter) Embeddings(context.Context, EmbeddingsRequest, string) (EmbeddingsResponse, error) {
	return EmbeddingsResponse{}, NewNonRetryableError(a.name, "", "qwen embeddings are not supported", nil)
}

func (a *QwenAdapter) AudioSpeech(context.Context, AudioSpeechRequest, string) (AudioSpeechResponse, error) {
	return AudioSpeechResponse{}, NewNonRetryableError(a.name, "", "qwen audio speech is not supported", nil)
}

func (a *QwenAdapter) ImagesGenerations(context.Context, ImagesGenerationsRequest, string) (ImagesGenerationsResponse, error) {
	return ImagesGenerationsResponse{}, NewNonRetryableError(a.name, "", "qwen image generation is not supported", nil)
}

func (a *QwenAdapter) chatURL(account qwenAccount) string {
	if resourceURL := stringFromMap(account.providerSpecificData, "resourceUrl", "resource_url"); resourceURL != "" {
		u := strings.TrimRight(resourceURL, "/")
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			u = "https://" + u
		}
		if parsed, err := url.Parse(u); err == nil && (parsed.Path == "" || parsed.Path == "/") {
			u += "/v1"
		}
		return endpoints.BuildOpenAI(u, "/chat/completions")
	}
	return endpoints.BuildOpenAI(a.baseURL, "/chat/completions")
}

func (a *QwenAdapter) applyHeaders(req *http.Request, token string, stream bool) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "QwenCode/0.12.3 (linux; x64)")
	req.Header.Set("X-DashScope-AuthType", "qwen-oauth")
	req.Header.Set("X-DashScope-CacheControl", "enable")
	req.Header.Set("X-DashScope-UserAgent", "QwenCode/0.12.3 (linux; x64)")
	req.Header.Set("X-Stainless-Arch", "x64")
	req.Header.Set("X-Stainless-Lang", "go")
	req.Header.Set("X-Stainless-Os", "Linux")
	req.Header.Set("X-Stainless-Package-Version", "5.11.0")
	req.Header.Set("X-Stainless-Retry-Count", "1")
	req.Header.Set("X-Stainless-Runtime", "go")
	req.Header.Set("X-Stainless-Runtime-Version", "1.24.0")
	req.Header.Set("Accept-Language", "*")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	for k, v := range a.headers {
		req.Header.Set(k, v)
	}
}

func (a *QwenAdapter) transformRequest(request ChatRequest, stream bool) ChatRequest {
	request.Messages = append([]ChatMessage{qwenSystemMessage}, request.Messages...)
	if stream {
		if request.Extra == nil {
			request.Extra = map[string]json.RawMessage{}
		}
		if _, ok := request.Extra["stream_options"]; !ok {
			request.Extra["stream_options"] = json.RawMessage(`{"include_usage":true}`)
		}
	}
	if qwenThinkingActive(request) && qwenToolChoiceIncompatible(request.ToolChoice) {
		request.ToolChoice = json.RawMessage(`"auto"`)
	}
	return request
}

func (a *QwenAdapter) classifyHTTPError(status int, model string, message string) error {
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		message = "qwen auth failed or deprecated free tier is unavailable; refresh/import a valid existing Qwen token"
	}
	return ClassifyHTTPError(status, a.name, model, message, a.errorConfig)
}

func qwenThinkingActive(request ChatRequest) bool {
	if request.Thinking != nil && request.Thinking.Enabled {
		return true
	}
	if request.Reasoning != nil && (request.Reasoning.Effort != "" || request.Reasoning.MaxTokens > 0) {
		return true
	}
	if raw, ok := request.Extra["enable_thinking"]; ok && bytes.Equal(bytes.TrimSpace(raw), []byte("true")) {
		return true
	}
	if raw, ok := request.Extra["thinking"]; ok {
		trimmed := bytes.TrimSpace(raw)
		if bytes.Equal(trimmed, []byte("true")) {
			return true
		}
		var parsed map[string]any
		if json.Unmarshal(trimmed, &parsed) == nil && parsed["type"] == "enabled" {
			return true
		}
	}
	return false
}

func qwenToolChoiceIncompatible(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return false
	}
	if bytes.Equal(trimmed, []byte(`"required"`)) {
		return true
	}
	return len(trimmed) > 0 && trimmed[0] == '{'
}

func stringFromMap(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
