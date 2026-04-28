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
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/edodoyokz/ai-go-router/internal/config"
)

const (
	grokWebDefaultBaseURL       = "https://grok.com/rest/app-chat/conversations/new"
	perplexityWebDefaultBaseURL = "https://www.perplexity.ai/rest/sse/perplexity_ask"
	grokWebUserAgent            = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36"
	perplexityWebUserAgent      = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36"
	perplexityWebAPIVersion     = "2.18"
)

type webCookieAccount struct {
	name        string
	token       string
	cookie      string
	accessToken string
}

type webAdapter struct {
	provider    string
	name        string
	baseURL     string
	headers     map[string]string
	errorConfig config.ErrorConfig
	httpClients []*http.Client
	proxyIdx    atomic.Uint64
	accounts    []webCookieAccount
	fallback    webCookieAccount
}

func NewGrokWebAdapter(cfg config.ProviderConfig, errorConfig config.ErrorConfig, proxyURL string) *webAdapter {
	return newWebAdapter("grok-web", cfg, errorConfig, proxyURL, grokWebDefaultBaseURL)
}

func NewPerplexityWebAdapter(cfg config.ProviderConfig, errorConfig config.ErrorConfig, proxyURL string) *webAdapter {
	return newWebAdapter("perplexity-web", cfg, errorConfig, proxyURL, perplexityWebDefaultBaseURL)
}

func newWebAdapter(provider string, cfg config.ProviderConfig, errorConfig config.ErrorConfig, proxyURL string, defaultBaseURL string) *webAdapter {
	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	clients := []*http.Client{createHTTPClient(proxyURL)}
	if len(cfg.ProxyURLs) > 0 {
		clients = clients[:0]
		for _, purl := range cfg.ProxyURLs {
			clients = append(clients, createHTTPClient(purl))
		}
	}

	accounts := make([]webCookieAccount, 0, len(cfg.Accounts))
	for _, account := range cfg.Accounts {
		if account.Enabled == false && (account.APIKey != "" || account.Cookie != "" || account.AccessToken != "") {
			account.Enabled = true
		}
		if !account.Enabled {
			continue
		}
		token := firstNonEmptyString(account.APIKey, account.Cookie)
		cookie := account.Cookie
		if cookie == "" {
			cookie = stringFromMap(account.ProviderSpecificData, "cookie")
		}
		if token == "" {
			token = extractWebCookieToken(provider, cookie)
		}
		if token == "" && account.AccessToken == "" {
			continue
		}
		name := account.Name
		if name == "" {
			name = account.ID
		}
		accounts = append(accounts, webCookieAccount{name: name, token: token, cookie: cookie, accessToken: account.AccessToken})
	}

	fallbackCookie := stringFromMap(cfg.ProviderSpecificData, "cookie")
	fallback := webCookieAccount{
		name:        "default",
		token:       firstNonEmptyString(cfg.APIKey, extractWebCookieToken(provider, fallbackCookie)),
		cookie:      fallbackCookie,
		accessToken: stringFromMap(cfg.ProviderSpecificData, "accessToken", "access_token"),
	}

	return &webAdapter{
		provider:    provider,
		name:        cfg.Name,
		baseURL:     baseURL,
		headers:     cfg.Headers,
		errorConfig: errorConfig,
		httpClients: clients,
		accounts:    accounts,
		fallback:    fallback,
	}
}

func (a *webAdapter) Name() string { return a.name }

func (a *webAdapter) AccountNames() []string {
	names := make([]string, 0, len(a.accounts))
	for _, account := range a.accounts {
		if account.name != "" {
			names = append(names, account.name)
		}
	}
	if len(names) == 0 && (a.fallback.token != "" || a.fallback.accessToken != "") {
		return []string{"default"}
	}
	return names
}

func (a *webAdapter) nextClient() *http.Client {
	if len(a.httpClients) == 1 {
		return a.httpClients[0]
	}
	idx := a.proxyIdx.Add(1) - 1
	return a.httpClients[idx%uint64(len(a.httpClients))]
}

func (a *webAdapter) selectAccount(ctx context.Context) webCookieAccount {
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

func (a *webAdapter) ChatCompletion(ctx context.Context, request ChatRequest, model string) (ChatResponse, error) {
	if a.provider == "perplexity-web" {
		return a.perplexityChatCompletion(ctx, request, model)
	}
	return a.grokChatCompletion(ctx, request, model)
}

func (a *webAdapter) StreamChatCompletion(ctx context.Context, request ChatRequest, model string) (<-chan ChatChunk, error) {
	if a.provider == "perplexity-web" {
		return a.perplexityStreamChatCompletion(ctx, request, model)
	}
	return a.grokStreamChatCompletion(ctx, request, model)
}

func (a *webAdapter) GetUsage(context.Context) (map[string]interface{}, error) {
	return map[string]interface{}{"unsupported": true, "provider": a.provider, "reason": "web session providers do not expose quota through the Go router"}, nil
}

func (a *webAdapter) Embeddings(context.Context, EmbeddingsRequest, string) (EmbeddingsResponse, error) {
	return EmbeddingsResponse{}, NewNonRetryableError(a.name, "", a.provider+" embeddings are not supported", nil)
}

func (a *webAdapter) AudioSpeech(context.Context, AudioSpeechRequest, string) (AudioSpeechResponse, error) {
	return AudioSpeechResponse{}, NewNonRetryableError(a.name, "", a.provider+" audio speech is not supported", nil)
}

func (a *webAdapter) ImagesGenerations(context.Context, ImagesGenerationsRequest, string) (ImagesGenerationsResponse, error) {
	return ImagesGenerationsResponse{}, NewNonRetryableError(a.name, "", a.provider+" image generation is not supported", nil)
}

func (a *webAdapter) grokChatCompletion(ctx context.Context, request ChatRequest, model string) (ChatResponse, error) {
	resp, err := a.doGrokRequest(ctx, request, model, false)
	if err != nil {
		return ChatResponse{}, err
	}
	defer resp.Body.Close()
	created := time.Now().Unix()
	cid := "chatcmpl-grok-" + randomHexString(6)
	modelInfo := grokWebModelInfoFor(model)
	content, reasoning, fingerprint, err := parseGrokNDJSON(resp.Body, modelInfo.isThinking, nil)
	if err != nil {
		return ChatResponse{}, err
	}
	promptTokens := estimateTokens(grokPromptFromMessages(request.Messages))
	completionTokens := estimateTokens(content)
	msg := ChatMessage{Role: "assistant", Content: content}
	if reasoning != "" {
		msg.ReasoningContent = reasoning
	}
	return ChatResponse{
		ID:                cid,
		Object:            "chat.completion",
		Created:           created,
		Model:             model,
		SystemFingerprint: fingerprint,
		Choices:           []ChatChoice{{Index: 0, Message: msg, FinishReason: "stop"}},
		Usage:             &Usage{PromptTokens: promptTokens, CompletionTokens: completionTokens, TotalTokens: promptTokens + completionTokens},
	}, nil
}

func (a *webAdapter) grokStreamChatCompletion(ctx context.Context, request ChatRequest, model string) (<-chan ChatChunk, error) {
	resp, err := a.doGrokRequest(ctx, request, model, true)
	if err != nil {
		return nil, err
	}
	ch := make(chan ChatChunk, 10)
	created := time.Now().Unix()
	cid := "chatcmpl-grok-" + randomHexString(6)
	isThinking := grokWebModelInfoFor(model).isThinking
	go func() {
		defer resp.Body.Close()
		defer close(ch)
		ch <- roleChunk(cid, model, created)
		var fingerprint string
		_, _, _, _ = parseGrokNDJSON(resp.Body, isThinking, func(event webContentEvent) {
			if event.fingerprint != "" {
				fingerprint = event.fingerprint
			}
			if event.thinking != "" {
				ch <- contentChunk(cid, model, created, fingerprint, "", event.thinking)
			}
			if event.delta != "" {
				ch <- contentChunk(cid, model, created, fingerprint, event.delta, "")
			}
			if event.fullMessage != "" {
				ch <- contentChunk(cid, model, created, fingerprint, event.fullMessage, "")
			}
		})
		ch <- finishChunk(cid, model, created, fingerprint)
	}()
	return ch, nil
}

func (a *webAdapter) doGrokRequest(ctx context.Context, request ChatRequest, model string, stream bool) (*http.Response, error) {
	account := a.selectAccount(ctx)
	token := firstNonEmptyString(account.token, extractWebCookieToken("grok-web", account.cookie))
	if token == "" {
		return nil, NewNonRetryableError(a.name, model, "grok-web requires a valid sso cookie token", nil)
	}
	message := grokPromptFromMessages(request.Messages)
	if strings.TrimSpace(message) == "" {
		return nil, NewNonRetryableError(a.name, model, "empty query after processing messages", nil)
	}
	modelInfo := grokWebModelInfoFor(model)
	body, err := json.Marshal(map[string]any{
		"temporary":                   true,
		"modelName":                   modelInfo.grokModel,
		"modelMode":                   modelInfo.modelMode,
		"message":                     message,
		"fileAttachments":             []any{},
		"imageAttachments":            []any{},
		"disableSearch":               false,
		"enableImageGeneration":       false,
		"returnImageBytes":            false,
		"returnRawGrokInXaiRequest":   false,
		"enableImageStreaming":        false,
		"imageGenerationCount":        0,
		"forceConcise":                false,
		"toolOverrides":               map[string]any{},
		"enableSideBySide":            true,
		"sendFinalMetadata":           true,
		"isReasoning":                 false,
		"disableTextFollowUps":        false,
		"disableMemory":               true,
		"forceSideBySide":             false,
		"isAsyncChat":                 false,
		"disableSelfHarmShortCircuit": false,
		"deviceEnvInfo": map[string]any{
			"darkModeEnabled": false, "devicePixelRatio": 2,
			"screenWidth": 2056, "screenHeight": 1329, "viewportWidth": 2056, "viewportHeight": 1083,
		},
	})
	if err != nil {
		return nil, NewNonRetryableError(a.name, model, "failed to marshal request", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, NewNonRetryableError(a.name, model, "failed to create request", err)
	}
	a.applyGrokHeaders(req, token)
	resp, err := a.nextClient().Do(req)
	if err != nil {
		return nil, NewRetryableError(a.name, model, "network error", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		return nil, a.classifyWebHTTPError(resp.StatusCode, model, string(respBody))
	}
	return resp, nil
}

func (a *webAdapter) perplexityChatCompletion(ctx context.Context, request ChatRequest, model string) (ChatResponse, error) {
	resp, currentMsg, err := a.doPerplexityRequest(ctx, request, model)
	if err != nil {
		return ChatResponse{}, err
	}
	defer resp.Body.Close()
	created := time.Now().Unix()
	cid := "chatcmpl-pplx-" + randomHexString(6)
	content, reasoning, err := parsePerplexitySSE(resp.Body, nil)
	if err != nil {
		return ChatResponse{}, err
	}
	promptTokens := estimateTokens(currentMsg)
	completionTokens := estimateTokens(content)
	msg := ChatMessage{Role: "assistant", Content: cleanPerplexityResponse(content, true)}
	if reasoning != "" {
		msg.ReasoningContent = reasoning
	}
	return ChatResponse{
		ID:      cid,
		Object:  "chat.completion",
		Created: created,
		Model:   model,
		Choices: []ChatChoice{{Index: 0, Message: msg, FinishReason: "stop"}},
		Usage:   &Usage{PromptTokens: promptTokens, CompletionTokens: completionTokens, TotalTokens: promptTokens + completionTokens},
	}, nil
}

func (a *webAdapter) perplexityStreamChatCompletion(ctx context.Context, request ChatRequest, model string) (<-chan ChatChunk, error) {
	resp, _, err := a.doPerplexityRequest(ctx, request, model)
	if err != nil {
		return nil, err
	}
	ch := make(chan ChatChunk, 10)
	created := time.Now().Unix()
	cid := "chatcmpl-pplx-" + randomHexString(6)
	go func() {
		defer resp.Body.Close()
		defer close(ch)
		ch <- roleChunk(cid, model, created)
		_, _, _ = parsePerplexitySSE(resp.Body, func(event webContentEvent) {
			if event.thinking != "" {
				ch <- contentChunk(cid, model, created, "", "", event.thinking+"\n")
			}
			if event.delta != "" {
				if cleaned := cleanPerplexityResponse(event.delta, false); cleaned != "" {
					ch <- contentChunk(cid, model, created, "", cleaned, "")
				}
			}
		})
		ch <- finishChunk(cid, model, created, "")
	}()
	return ch, nil
}

func (a *webAdapter) doPerplexityRequest(ctx context.Context, request ChatRequest, model string) (*http.Response, string, error) {
	account := a.selectAccount(ctx)
	token := firstNonEmptyString(account.token, extractWebCookieToken("perplexity-web", account.cookie))
	if token == "" && account.accessToken == "" {
		return nil, "", NewNonRetryableError(a.name, model, "perplexity-web requires a valid __Secure-next-auth.session-token cookie or access token", nil)
	}
	parsed := parseWebMessages(request.Messages)
	currentMsg := parsed.currentMsg
	query := perplexityBuildQuery(parsed, request.Tools)
	if strings.TrimSpace(query) == "" {
		return nil, "", NewNonRetryableError(a.name, model, "empty query after processing messages", nil)
	}
	mode, pref := perplexityModelPreference(model, request)
	body, err := json.Marshal(map[string]any{
		"query_str": query,
		"params": map[string]any{
			"query_str":             query,
			"search_focus":          "internet",
			"mode":                  mode,
			"model_preference":      pref,
			"sources":               []string{"web"},
			"attachments":           []any{},
			"frontend_uuid":         randomUUIDLike(),
			"frontend_context_uuid": randomUUIDLike(),
			"version":               perplexityWebAPIVersion,
			"language":              "en-US",
			"timezone":              "UTC",
			"search_recency_filter": nil,
			"is_incognito":          true,
			"use_schematized_api":   true,
			"last_backend_uuid":     nil,
		},
	})
	if err != nil {
		return nil, "", NewNonRetryableError(a.name, model, "failed to marshal request", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, "", NewNonRetryableError(a.name, model, "failed to create request", err)
	}
	a.applyPerplexityHeaders(req, token, account.accessToken)
	resp, err := a.nextClient().Do(req)
	if err != nil {
		return nil, "", NewRetryableError(a.name, model, "network error", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		return nil, "", a.classifyWebHTTPError(resp.StatusCode, model, string(respBody))
	}
	return resp, currentMsg, nil
}

func (a *webAdapter) applyGrokHeaders(req *http.Request, token string) {
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://grok.com")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("Referer", "https://grok.com/")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("User-Agent", grokWebUserAgent)
	req.Header.Set("x-xai-request-id", randomUUIDLike())
	req.Header.Set("Cookie", "sso="+strings.TrimPrefix(token, "sso="))
	for k, v := range a.headers {
		req.Header.Set(k, v)
	}
}

func (a *webAdapter) applyPerplexityHeaders(req *http.Request, token string, accessToken string) {
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://www.perplexity.ai")
	req.Header.Set("Referer", "https://www.perplexity.ai/")
	req.Header.Set("User-Agent", perplexityWebUserAgent)
	req.Header.Set("X-App-ApiClient", "default")
	req.Header.Set("X-App-ApiVersion", perplexityWebAPIVersion)
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	} else {
		req.Header.Set("Cookie", "__Secure-next-auth.session-token="+token)
	}
	for k, v := range a.headers {
		req.Header.Set(k, v)
	}
}

func (a *webAdapter) classifyWebHTTPError(status int, model string, message string) error {
	switch a.provider {
	case "grok-web":
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			message = "Grok auth failed - SSO cookie may be expired. Re-paste your sso cookie value from grok.com."
		} else if status == http.StatusTooManyRequests {
			message = "Grok rate limited. Wait a moment and retry, or rotate cookies."
		}
	case "perplexity-web":
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			message = "Perplexity auth failed - session cookie may be expired. Re-paste your __Secure-next-auth.session-token."
		} else if status == http.StatusTooManyRequests {
			message = "Perplexity rate limited. Wait a moment and retry."
		}
	}
	return ClassifyHTTPError(status, a.name, model, message, a.errorConfig)
}

type grokWebModelInfo struct {
	grokModel  string
	modelMode  string
	isThinking bool
}

func grokWebModelInfoFor(model string) grokWebModelInfo {
	models := map[string]grokWebModelInfo{
		"grok-3":            {"grok-3", "MODEL_MODE_GROK_3", false},
		"grok-3-mini":       {"grok-3", "MODEL_MODE_GROK_3_MINI_THINKING", true},
		"grok-3-thinking":   {"grok-3", "MODEL_MODE_GROK_3_THINKING", true},
		"grok-4":            {"grok-4", "MODEL_MODE_GROK_4", false},
		"grok-4-mini":       {"grok-4-mini", "MODEL_MODE_GROK_4_MINI_THINKING", true},
		"grok-4-thinking":   {"grok-4", "MODEL_MODE_GROK_4_THINKING", true},
		"grok-4-heavy":      {"grok-4", "MODEL_MODE_HEAVY", true},
		"grok-4.1-mini":     {"grok-4-1-thinking-1129", "MODEL_MODE_GROK_4_1_MINI_THINKING", true},
		"grok-4.1-fast":     {"grok-4-1-thinking-1129", "MODEL_MODE_FAST", false},
		"grok-4.1-expert":   {"grok-4-1-thinking-1129", "MODEL_MODE_EXPERT", true},
		"grok-4.1-thinking": {"grok-4-1-thinking-1129", "MODEL_MODE_GROK_4_1_THINKING", true},
		"grok-4.2":          {"grok-420", "MODEL_MODE_GROK_420", false},
		"grok-4.20":         {"grok-420", "MODEL_MODE_GROK_420", false},
		"grok-4.20-beta":    {"grok-420", "MODEL_MODE_GROK_420", false},
	}
	if info, ok := models[model]; ok {
		return info
	}
	return models["grok-4.1-fast"]
}

type parsedWebMessages struct {
	systemMsg  string
	history    []webHistoryMessage
	currentMsg string
}

type webHistoryMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func parseWebMessages(messages []ChatMessage) parsedWebMessages {
	var parsed parsedWebMessages
	for _, msg := range messages {
		role := msg.Role
		if role == "" {
			role = "user"
		}
		if role == "developer" {
			role = "system"
		}
		content := chatMessageText(msg.Content)
		if strings.TrimSpace(content) == "" {
			continue
		}
		switch role {
		case "system":
			parsed.systemMsg += content + "\n"
		case "user", "assistant":
			parsed.history = append(parsed.history, webHistoryMessage{Role: role, Content: content})
		}
	}
	if len(parsed.history) > 0 && parsed.history[len(parsed.history)-1].Role == "user" {
		parsed.currentMsg = parsed.history[len(parsed.history)-1].Content
		parsed.history = parsed.history[:len(parsed.history)-1]
	}
	return parsed
}

func grokPromptFromMessages(messages []ChatMessage) string {
	extracted := make([]webHistoryMessage, 0, len(messages))
	for _, msg := range messages {
		role := msg.Role
		if role == "" {
			role = "user"
		}
		if role == "developer" {
			role = "system"
		}
		text := chatMessageText(msg.Content)
		if strings.TrimSpace(text) != "" {
			extracted = append(extracted, webHistoryMessage{Role: role, Content: text})
		}
	}
	lastUser := -1
	for i := len(extracted) - 1; i >= 0; i-- {
		if extracted[i].Role == "user" {
			lastUser = i
			break
		}
	}
	parts := make([]string, 0, len(extracted))
	for i, item := range extracted {
		if i == lastUser {
			parts = append(parts, item.Content)
		} else {
			parts = append(parts, item.Role+": "+item.Content)
		}
	}
	return strings.Join(parts, "\n\n")
}

func chatMessageText(content any) string {
	switch v := content.(type) {
	case nil:
		return ""
	case string:
		return v
	case []ContentPart:
		parts := make([]string, 0, len(v))
		for _, part := range v {
			if part.Type == "text" || part.Type == "input_text" {
				parts = append(parts, part.Text)
			}
		}
		return strings.Join(parts, " ")
	case []any:
		parts := make([]string, 0, len(v))
		for _, raw := range v {
			if m, ok := raw.(map[string]any); ok {
				if typ, _ := m["type"].(string); typ == "text" || typ == "input_text" {
					if text, _ := m["text"].(string); text != "" {
						parts = append(parts, text)
					}
				}
			}
		}
		return strings.Join(parts, " ")
	default:
		body, _ := json.Marshal(v)
		var parts []ContentPart
		if json.Unmarshal(body, &parts) == nil {
			return chatMessageText(parts)
		}
		return ""
	}
}

func perplexityBuildQuery(parsed parsedWebMessages, tools []Tool) string {
	instructions := make([]string, 0, 3)
	if strings.TrimSpace(parsed.systemMsg) != "" {
		instructions = append(instructions, strings.TrimSpace(parsed.systemMsg))
	}
	if len(tools) > 0 {
		lines := []string{"Available tools (reference only, cannot invoke):"}
		for _, tool := range tools {
			var fn struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			}
			_ = json.Unmarshal(tool.Function, &fn)
			if fn.Name == "" {
				fn.Name = "unnamed"
			}
			if len(fn.Description) > 200 {
				fn.Description = fn.Description[:200]
			}
			lines = append(lines, "- "+fn.Name+": "+strings.Split(fn.Description, "\n")[0])
		}
		instructions = append(instructions, strings.Join(lines, "\n"))
	}
	instructions = append(instructions, "You have built-in web search. Answer questions directly using search results.")
	obj := map[string]any{"instructions": instructions}
	if len(parsed.history) > 0 {
		obj["history"] = parsed.history
	}
	obj["query"] = parsed.currentMsg
	body, _ := json.Marshal(obj)
	if len(body) > 96000 {
		return string(body[len(body)-96000:])
	}
	return string(body)
}

func perplexityModelPreference(model string, request ChatRequest) (string, string) {
	thinking := (request.Thinking != nil && request.Thinking.Enabled) || (request.Reasoning != nil && request.Reasoning.Effort != "" && request.Reasoning.Effort != "none")
	if thinking {
		switch model {
		case "pplx-gpt":
			return "copilot", "gpt54_thinking"
		case "pplx-sonnet":
			return "copilot", "claude46sonnetthinking"
		case "pplx-opus":
			return "copilot", "claude46opusthinking"
		}
	}
	switch model {
	case "pplx-auto":
		return "concise", "pplx_pro"
	case "pplx-sonar":
		return "copilot", "experimental"
	case "pplx-gpt":
		return "copilot", "gpt54"
	case "pplx-gemini":
		return "copilot", "gemini31pro_high"
	case "pplx-sonnet":
		return "copilot", "claude46sonnet"
	case "pplx-opus":
		return "copilot", "claude46opus"
	case "pplx-nemotron":
		return "copilot", "nv_nemotron_3_super"
	default:
		return "copilot", model
	}
}

type webContentEvent struct {
	delta       string
	fullMessage string
	thinking    string
	fingerprint string
}

func parseGrokNDJSON(r io.Reader, isThinking bool, emit func(webContentEvent)) (string, string, string, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var content, reasoning, fingerprint string
	thinkOpened := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event map[string]any
		if json.Unmarshal([]byte(line), &event) != nil {
			continue
		}
		if errObj, ok := event["error"].(map[string]any); ok {
			return "", "", "", NewProviderUnavailableError("grok-web", "", fmt.Sprint(errObj["message"]), nil)
		}
		resp := nestedMap(event, "result", "response")
		if resp == nil {
			continue
		}
		if fp := webNestedString(resp, "llmInfo", "modelHash"); fp != "" && fingerprint == "" {
			fingerprint = fp
		}
		if mr, ok := resp["modelResponse"].(map[string]any); ok {
			msg, _ := mr["message"].(string)
			if isThinking && thinkOpened && msg != "" {
				reasoning += msg
				if emit != nil {
					emit(webContentEvent{thinking: msg, fingerprint: fingerprint})
				}
				thinkOpened = false
			} else if msg != "" {
				content = msg
				if emit != nil {
					emit(webContentEvent{fullMessage: msg, fingerprint: fingerprint})
				}
			}
			if fp := webNestedString(mr, "metadata", "llm_info", "modelHash"); fp != "" {
				fingerprint = fp
			}
			continue
		}
		if token, ok := resp["token"].(string); ok && token != "" {
			content += token
			if emit != nil {
				emit(webContentEvent{delta: token, fingerprint: fingerprint})
			}
			if isThinking {
				thinkOpened = true
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", "", "", NewRetryableError("grok-web", "", "failed to read upstream stream", err)
	}
	return content, reasoning, fingerprint, nil
}

func parsePerplexitySSE(r io.Reader, emit func(webContentEvent)) (string, string, error) {
	decoder := NewSSEDecoder(r)
	var fullAnswer string
	var seenLen int
	seenThinking := map[string]bool{}
	reasoningParts := []string{}
	for {
		event, err := decoder.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			return "", "", NewRetryableError("perplexity-web", "", "failed to read upstream stream", err)
		}
		data := strings.TrimSpace(event.Data)
		if data == "" || data == "[DONE]" {
			if data == "[DONE]" {
				break
			}
			continue
		}
		var payload map[string]any
		if json.Unmarshal([]byte(data), &payload) != nil {
			continue
		}
		if msg, _ := payload["error_message"].(string); msg != "" {
			return "", "", NewProviderUnavailableError("perplexity-web", "", msg, nil)
		}
		blocks, _ := payload["blocks"].([]any)
		for _, rawBlock := range blocks {
			block, _ := rawBlock.(map[string]any)
			usage, _ := block["intended_usage"].(string)
			if usage == "pro_search_steps" || usage == "plan" {
				for _, thought := range perplexityThinkingFromBlock(block) {
					if !seenThinking[thought] {
						seenThinking[thought] = true
						reasoningParts = append(reasoningParts, thought)
						if emit != nil {
							emit(webContentEvent{thinking: thought})
						}
					}
				}
			}
			if !strings.Contains(usage, "markdown") {
				continue
			}
			mb, _ := block["markdown_block"].(map[string]any)
			chunksRaw, _ := mb["chunks"].([]any)
			if len(chunksRaw) == 0 {
				continue
			}
			chunks := make([]string, 0, len(chunksRaw))
			for _, chunk := range chunksRaw {
				chunks = append(chunks, fmt.Sprint(chunk))
			}
			chunkText := strings.Join(chunks, "")
			if progress, _ := mb["progress"].(string); progress == "DONE" {
				fullAnswer = chunkText
				seenLen = len(fullAnswer)
				continue
			}
			cumulative := fullAnswer + chunkText
			if len(cumulative) > seenLen {
				delta := cumulative[seenLen:]
				fullAnswer = cumulative
				seenLen = len(cumulative)
				if emit != nil {
					emit(webContentEvent{delta: delta})
				}
			}
		}
		if len(blocks) == 0 {
			if text, _ := payload["text"].(string); len(text) > seenLen {
				delta := text[seenLen:]
				fullAnswer = text
				seenLen = len(text)
				if emit != nil {
					emit(webContentEvent{delta: delta})
				}
			}
		}
	}
	return fullAnswer, strings.Join(reasoningParts, "\n"), nil
}

func perplexityThinkingFromBlock(block map[string]any) []string {
	plan, _ := block["plan_block"].(map[string]any)
	if plan == nil {
		return nil
	}
	out := []string{}
	if steps, _ := plan["steps"].([]any); len(steps) > 0 {
		for _, rawStep := range steps {
			step, _ := rawStep.(map[string]any)
			switch step["step_type"] {
			case "SEARCH_WEB":
				content, _ := step["search_web_content"].(map[string]any)
				for _, rawQ := range asAnySlice(content["queries"]) {
					q, _ := rawQ.(map[string]any)
					if query, _ := q["query"].(string); query != "" {
						out = append(out, "Searching: "+query)
					}
				}
			case "READ_RESULTS":
				content, _ := step["read_results_content"].(map[string]any)
				urls := asAnySlice(content["urls"])
				if len(urls) > 3 {
					urls = urls[:3]
				}
				for _, rawURL := range urls {
					if u, _ := rawURL.(string); u != "" {
						out = append(out, "Reading: "+u)
					}
				}
			}
		}
	}
	if goals, _ := plan["goals"].([]any); len(goals) > 0 {
		for _, rawGoal := range goals {
			goal, _ := rawGoal.(map[string]any)
			if desc, _ := goal["description"].(string); desc != "" {
				out = append(out, desc)
			}
		}
	}
	return out
}

func asAnySlice(value any) []any {
	items, _ := value.([]any)
	return items
}

func roleChunk(id, model string, created int64) ChatChunk {
	return ChatChunk{ID: id, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{Role: "assistant"}}}}
}

func contentChunk(id, model string, created int64, fingerprint, content, reasoning string) ChatChunk {
	return ChatChunk{
		ID: id, Object: "chat.completion.chunk", Created: created, Model: model, SystemFingerprint: fingerprint,
		Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{Content: content, ReasoningContent: reasoning}}},
	}
}

func finishChunk(id, model string, created int64, fingerprint string) ChatChunk {
	reason := "stop"
	return ChatChunk{ID: id, Object: "chat.completion.chunk", Created: created, Model: model, SystemFingerprint: fingerprint, Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{}, FinishReason: &reason}}}
}

func extractWebCookieToken(provider, cookie string) string {
	name := "sso"
	if provider == "perplexity-web" {
		name = "__Secure-next-auth.session-token"
	}
	token := extractWebCookieValue(cookie, name)
	if token == "" && !strings.Contains(cookie, "=") {
		token = cookie
	}
	return strings.TrimSpace(strings.Trim(token, `"`))
}

func extractWebCookieValue(cookie, name string) string {
	for _, part := range strings.Split(cookie, ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && key == name {
			return value
		}
	}
	return ""
}

func estimateTokens(text string) int {
	if text == "" {
		return 0
	}
	return (len(text) + 3) / 4
}

func randomHexString(bytesLen int) string {
	b := make([]byte, bytesLen)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func randomUUIDLike() string {
	raw := randomHexString(16)
	return raw[0:8] + "-" + raw[8:12] + "-" + raw[12:16] + "-" + raw[16:20] + "-" + raw[20:32]
}

func nestedMap(root map[string]any, keys ...string) map[string]any {
	current := root
	for _, key := range keys {
		next, _ := current[key].(map[string]any)
		if next == nil {
			return nil
		}
		current = next
	}
	return current
}

func webNestedString(root map[string]any, keys ...string) string {
	m := nestedMap(root, keys[:len(keys)-1]...)
	if m == nil {
		return ""
	}
	v, _ := m[keys[len(keys)-1]].(string)
	return v
}

var (
	perplexityCitationRE    = regexp.MustCompile(`\[\d+\]`)
	perplexityGrokTagRE     = regexp.MustCompile(`(?s)<grok:[^>]*>.*?</grok:[^>]*>`)
	perplexityGrokSelfRE    = regexp.MustCompile(`<grok:[^>]*/>`)
	perplexityXMLDeclRE     = regexp.MustCompile(`<\?xml[^?]*\?>`)
	perplexityResponseTagRE = regexp.MustCompile(`(?i)</?response\b[^>]*>`)
	perplexityMultiSpaceRE  = regexp.MustCompile(` {2,}`)
	perplexityMultiNLRE     = regexp.MustCompile(`\n{3,}`)
)

func cleanPerplexityResponse(text string, strip bool) string {
	text = perplexityXMLDeclRE.ReplaceAllString(text, "")
	text = perplexityCitationRE.ReplaceAllString(text, "")
	text = perplexityGrokTagRE.ReplaceAllString(text, "")
	text = perplexityGrokSelfRE.ReplaceAllString(text, "")
	text = perplexityResponseTagRE.ReplaceAllString(text, "")
	if strip {
		text = perplexityMultiSpaceRE.ReplaceAllString(text, " ")
		text = perplexityMultiNLRE.ReplaceAllString(text, "\n\n")
		text = strings.TrimSpace(text)
	}
	return text
}
