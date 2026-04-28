package providers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/edodoyokz/ai-go-router/internal/config"
	"github.com/edodoyokz/ai-go-router/internal/translator"
)

const codexDefaultInstructions = "You are Codex, a coding agent. Follow the user's instructions precisely, prefer concise answers, and produce correct code changes when requested."

const codexDefaultTokenURL = "https://auth.openai.com/oauth/token"

type CodexTokenRefreshResult struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	TokenType    string
	Scope        string
}

type CodexExecutor struct {
	cfg        config.ProviderConfig
	errorCfg   config.ErrorConfig
	client     *http.Client
	translator *translator.Registry
	baseURL    string
}

func NewCodexExecutor(cfg config.ProviderConfig, errorCfg config.ErrorConfig) (Executor, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://chatgpt.com/backend-api/codex/responses"
	}
	if err := validateCodexCredentials(cfg); err != nil {
		return nil, err
	}
	return &CodexExecutor{
		cfg:        cfg,
		errorCfg:   errorCfg,
		client:     &http.Client{Timeout: 120 * time.Second},
		translator: translator.NewRegistry(),
		baseURL:    baseURL,
	}, nil
}

func validateCodexCredentials(cfg config.ProviderConfig) error {
	hasToken := strings.TrimSpace(cfg.APIKey) != ""
	hasRefresh := false
	for _, account := range cfg.Accounts {
		if strings.TrimSpace(account.AccessToken) != "" {
			hasToken = true
		}
		if strings.TrimSpace(account.RefreshToken) != "" {
			hasRefresh = true
		}
	}
	if !hasToken && !hasRefresh {
		return fmt.Errorf("codex credentials incomplete: provide access token/api_key or refresh_token")
	}
	return nil
}

func (e *CodexExecutor) ProviderID() string { return "codex" }

func (e *CodexExecutor) Supports(kind string) bool { return kind == "llm" }

func (e *CodexExecutor) ChatCompletion(ctx context.Context, req ChatRequest, model string) (ChatResponse, error) {
	body, compact, err := e.buildRequestBody(ctx, req, model, false)
	if err != nil {
		return ChatResponse{}, err
	}
	respBody, err := e.doRequest(ctx, compact, false, body)
	if err != nil {
		return ChatResponse{}, err
	}
	translated, err := e.translator.TranslateResponseJSON(ctx, translator.FormatOpenAIResp, translator.FormatOpenAI, respBody)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("translate response: %w", err)
	}
	var out ChatResponse
	if err := json.Unmarshal(translated, &out); err != nil {
		return ChatResponse{}, fmt.Errorf("decode translated response: %w", err)
	}
	return out, nil
}

func (e *CodexExecutor) StreamChatCompletion(ctx context.Context, req ChatRequest, model string) (<-chan ChatChunk, error) {
	body, compact, err := e.buildRequestBody(ctx, req, model, true)
	if err != nil {
		return nil, err
	}
	url := e.baseURL
	if compact {
		url += "/compact"
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	for k, v := range e.buildHeaders(true, body) {
		httpReq.Header.Set(k, v)
	}
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		return nil, ClassifyHTTPError(resp.StatusCode, e.cfg.Name, model, string(respBody), e.errorCfg)
	}
	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		ch := make(chan ChatChunk, 16)
		go e.readResponsesSSE(resp.Body, model, ch)
		return ch, nil
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	ch := make(chan ChatChunk, 16)
	go func() {
		defer close(ch)
		translated, err := e.translator.TranslateResponseJSON(ctx, translator.FormatOpenAIResp, translator.FormatOpenAI, respBody)
		if err != nil {
			return
		}
		var resp ChatResponse
		if err := json.Unmarshal(translated, &resp); err != nil {
			return
		}
		created := resp.Created
		if created == 0 {
			created = time.Now().Unix()
		}
		responseID := resp.ID
		if responseID == "" {
			responseID = fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
		}
		if len(resp.Choices) > 0 {
			content := resp.Choices[0].Message.Content
			if text, ok := content.(string); ok && text != "" {
				ch <- ChatChunk{ID: responseID, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{Role: "assistant", Content: text}}}}
			}
		}
		finish := "stop"
		ch <- ChatChunk{ID: responseID, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{}, FinishReason: &finish}}, Usage: resp.Usage}
	}()
	return ch, nil
}

func (e *CodexExecutor) readResponsesSSE(body io.ReadCloser, model string, ch chan<- ChatChunk) {
	defer close(ch)
	defer body.Close()
	decoder := NewSSEDecoder(body)
	responseID := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	created := time.Now().Unix()
	var usage *Usage
	for {
		evt, err := decoder.Next()
		if err != nil {
			break
		}
		data := strings.TrimSpace(evt.Data)
		if data == "" || data == "[DONE]" {
			if data == "[DONE]" {
				break
			}
			continue
		}
		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			continue
		}
		if id := stringValueForCodex(payload["response_id"]); id != "" {
			responseID = id
		} else if id := stringValueForCodex(payload["id"]); id != "" {
			responseID = id
		}
		eventType := firstNonEmptyString(stringValueForCodex(payload["type"]), evt.Event)
		switch eventType {
		case "response.output_text.delta", "response.refusal.delta":
			if delta := stringValueForCodex(payload["delta"]); delta != "" {
				ch <- ChatChunk{ID: responseID, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{Role: "assistant", Content: delta}}}}
			}
		case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
			if delta := stringValueForCodex(payload["delta"]); delta != "" {
				ch <- ChatChunk{ID: responseID, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{ReasoningContent: delta, Thinking: delta}}}}
			}
		case "response.completed":
			if response, ok := payload["response"].(map[string]interface{}); ok {
				usage = codexUsageFromResponse(response["usage"])
			}
		}
	}
	finish := "stop"
	ch <- ChatChunk{ID: responseID, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{}, FinishReason: &finish}}, Usage: usage}
}

func codexUsageFromResponse(raw interface{}) *Usage {
	m, ok := raw.(map[string]interface{})
	if !ok {
		return nil
	}
	in := int(numberValue(m["input_tokens"]))
	out := int(numberValue(m["output_tokens"]))
	if in == 0 {
		in = int(numberValue(m["prompt_tokens"]))
	}
	if out == 0 {
		out = int(numberValue(m["completion_tokens"]))
	}
	total := int(numberValue(m["total_tokens"]))
	if total == 0 {
		total = in + out
	}
	if total == 0 {
		return nil
	}
	return &Usage{PromptTokens: in, CompletionTokens: out, TotalTokens: total, InputTokens: in, OutputTokens: out}
}

func (e *CodexExecutor) buildRequestBody(ctx context.Context, req ChatRequest, model string, stream bool) (json.RawMessage, bool, error) {
	body := map[string]interface{}{
		"model":       model,
		"messages":    req.Messages,
		"tools":       req.Tools,
		"stream":      true,
		"temperature": req.Temperature,
		"top_p":       req.TopP,
	}
	if req.MaxTokens != nil {
		body["max_tokens"] = *req.MaxTokens
	}
	if req.Reasoning != nil {
		body["reasoning"] = req.Reasoning
	}
	compact := false
	for k, v := range req.Extra {
		if k == "_compact" {
			compact = boolValue(v)
		}
		body[k] = json.RawMessage(v)
	}
	translated, err := e.translator.TranslateRequestJSON(ctx, translator.FormatOpenAI, translator.FormatOpenAIResp, mustMarshal(body))
	if err != nil {
		return nil, false, fmt.Errorf("translate request: %w", err)
	}
	var codex map[string]interface{}
	if err := json.Unmarshal(translated, &codex); err != nil {
		return nil, false, fmt.Errorf("decode translated request: %w", err)
	}
	delete(codex, "_compact")
	e.transformCodexRequest(codex, model)
	encoded, err := json.Marshal(codex)
	if err != nil {
		return nil, false, err
	}
	return encoded, compact, nil
}

func (e *CodexExecutor) transformCodexRequest(body map[string]interface{}, model string) {
	if body["input"] == nil {
		body["input"] = []interface{}{map[string]interface{}{"type": "message", "role": "user", "content": []interface{}{map[string]interface{}{"type": "input_text", "text": "..."}}}}
	}
	body["stream"] = true
	body["store"] = false
	if strings.TrimSpace(stringValueForCodex(body["instructions"])) == "" {
		body["instructions"] = codexDefaultInstructions
	}
	if body["reasoning"] == nil {
		body["reasoning"] = map[string]interface{}{"effort": codexReasoningEffort(model, body), "summary": "auto"}
	} else if reasoning, ok := body["reasoning"].(map[string]interface{}); ok && reasoning["summary"] == nil {
		reasoning["summary"] = "auto"
	}
	if reasoning, ok := body["reasoning"].(map[string]interface{}); ok {
		if effort := stringValueForCodex(reasoning["effort"]); effort != "" && effort != "none" {
			body["include"] = []interface{}{"reasoning.encrypted_content"}
		}
	}
	for _, field := range []string{"temperature", "top_p", "frequency_penalty", "presence_penalty", "logprobs", "top_logprobs", "n", "seed", "max_tokens", "user", "prompt_cache_retention", "metadata", "stream_options", "safety_identifier"} {
		delete(body, field)
	}
}

func codexReasoningEffort(model string, body map[string]interface{}) string {
	if effort := stringValueForCodex(body["reasoning_effort"]); effort != "" {
		delete(body, "reasoning_effort")
		return effort
	}
	for _, level := range []string{"none", "low", "medium", "high", "xhigh"} {
		if strings.HasSuffix(model, "-"+level) {
			return level
		}
	}
	return "low"
}

func (e *CodexExecutor) doRequest(ctx context.Context, compact bool, stream bool, body json.RawMessage) (json.RawMessage, error) {
	url := e.baseURL
	if compact {
		url += "/compact"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	for k, v := range e.buildHeaders(stream, body) {
		req.Header.Set(k, v)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, ClassifyHTTPError(resp.StatusCode, e.cfg.Name, stringValueForCodex(mustDecodeCodexModel(body)), string(respBody), e.errorCfg)
	}
	return respBody, nil
}

func (e *CodexExecutor) buildHeaders(stream bool, body []byte) map[string]string {
	headers := map[string]string{
		"Content-Type": "application/json",
		"Accept":       "application/json",
		"originator":   "codex-cli",
		"User-Agent":   "codex-cli/1.0.18 (linux; amd64)",
		"session_id":   codexSessionID(body),
	}
	if stream {
		headers["Accept"] = "text/event-stream, application/json"
	}
	if token := e.accessToken(); token != "" {
		headers["Authorization"] = "Bearer " + token
	}
	for k, v := range e.cfg.Headers {
		headers[k] = v
	}
	return headers
}

func (e *CodexExecutor) accessToken() string {
	if len(e.cfg.Accounts) > 0 && e.cfg.Accounts[0].AccessToken != "" {
		return e.cfg.Accounts[0].AccessToken
	}
	return e.cfg.APIKey
}

func (e *CodexExecutor) NeedsRefresh(leeway time.Duration) bool {
	if len(e.cfg.Accounts) == 0 {
		return false
	}
	account := e.cfg.Accounts[0]
	if account.RefreshToken == "" {
		return false
	}
	if account.AccessToken == "" || account.ExpiresAt == nil {
		return true
	}
	return time.Until(*account.ExpiresAt) <= leeway
}

func (e *CodexExecutor) RefreshCredentials(ctx context.Context) (CodexTokenRefreshResult, error) {
	if len(e.cfg.Accounts) == 0 || strings.TrimSpace(e.cfg.Accounts[0].RefreshToken) == "" {
		return CodexTokenRefreshResult{}, fmt.Errorf("codex refresh token is required")
	}
	account := e.cfg.Accounts[0]
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", account.RefreshToken)
	if clientID := e.codexClientID(); clientID != "" {
		form.Set("client_id", clientID)
	}
	if clientSecret := e.codexClientSecret(); clientSecret != "" {
		form.Set("client_secret", clientSecret)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.codexTokenURL(), strings.NewReader(form.Encode()))
	if err != nil {
		return CodexTokenRefreshResult{}, fmt.Errorf("create codex refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := e.client.Do(req)
	if err != nil {
		return CodexTokenRefreshResult{}, fmt.Errorf("execute codex refresh request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return CodexTokenRefreshResult{}, fmt.Errorf("codex refresh failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var parsed struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		TokenType    string `json:"token_type"`
		Scope        string `json:"scope"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return CodexTokenRefreshResult{}, fmt.Errorf("decode codex refresh response: %w", err)
	}
	if parsed.AccessToken == "" {
		return CodexTokenRefreshResult{}, fmt.Errorf("codex refresh response missing access_token")
	}
	if parsed.RefreshToken == "" {
		parsed.RefreshToken = account.RefreshToken
	}
	if parsed.ExpiresIn == 0 {
		parsed.ExpiresIn = 3600
	}
	if parsed.TokenType == "" {
		parsed.TokenType = "Bearer"
	}
	return CodexTokenRefreshResult{
		AccessToken:  parsed.AccessToken,
		RefreshToken: parsed.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(parsed.ExpiresIn) * time.Second),
		TokenType:    parsed.TokenType,
		Scope:        parsed.Scope,
	}, nil
}

func (e *CodexExecutor) codexTokenURL() string {
	if v, ok := e.cfg.ProviderSpecificData["token_url"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	if len(e.cfg.Accounts) > 0 && e.cfg.Accounts[0].ProviderSpecificData != nil {
		if v, ok := e.cfg.Accounts[0].ProviderSpecificData["token_url"].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return codexDefaultTokenURL
}

func (e *CodexExecutor) codexClientID() string {
	if v, ok := e.cfg.ProviderSpecificData["client_id"].(string); ok {
		return v
	}
	return "app_EMoamEEZ73f0CkXaXp7hrann"
}

func (e *CodexExecutor) codexClientSecret() string {
	if v, ok := e.cfg.ProviderSpecificData["client_secret"].(string); ok {
		return v
	}
	return ""
}

func codexSessionID(body []byte) string {
	sum := sha256.Sum256(body)
	return "sess_" + hex.EncodeToString(sum[:8])
}

func mustDecodeCodexModel(body []byte) interface{} {
	var raw map[string]interface{}
	_ = json.Unmarshal(body, &raw)
	return raw["model"]
}

func stringValueForCodex(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func boolValue(v interface{}) bool {
	switch x := v.(type) {
	case bool:
		return x
	case json.RawMessage:
		var b bool
		_ = json.Unmarshal(x, &b)
		return b
	default:
		return false
	}
}

func init() {
	RegisterExecutor("codex", func(cfg config.ProviderConfig, errorCfg config.ErrorConfig) (Executor, error) {
		return NewCodexExecutor(cfg, errorCfg)
	})
}
