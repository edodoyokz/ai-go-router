package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/edodoyokz/ai-go-router/internal/config"
	"github.com/edodoyokz/ai-go-router/internal/translator"
)

type KiroExecutor struct {
	cfg         config.ProviderConfig
	errorCfg    config.ErrorConfig
	client      *http.Client
	translator  *translator.Registry
	baseURL     string
	headers     map[string]string
}

type KiroRefreshResult struct {
	AccessToken  string
	RefreshToken string
	ProfileARN   string
	ExpiresAt    time.Time
}

func NewKiroExecutor(cfg config.ProviderConfig, errorCfg config.ErrorConfig) (Executor, error) {
	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = "https://codewhisperer.us-east-1.amazonaws.com/generateAssistantResponse"
	}
	if err := validateKiroCredentials(cfg); err != nil {
		return nil, err
	}
	return &KiroExecutor{
		cfg:        cfg,
		errorCfg:   errorCfg,
		client:     &http.Client{Timeout: 120 * time.Second},
		translator: translator.NewRegistry(),
		baseURL:    baseURL,
		headers:    cfg.Headers,
	}, nil
}

func validateKiroCredentials(cfg config.ProviderConfig) error {
	for _, account := range cfg.Accounts {
		if strings.TrimSpace(account.AccessToken) != "" || strings.TrimSpace(account.RefreshToken) != "" {
			return nil
		}
	}
	if strings.TrimSpace(cfg.APIKey) != "" {
		return nil
	}
	return fmt.Errorf("kiro credentials incomplete: provide access_token/api_key or refresh_token")
}

func (e *KiroExecutor) ProviderID() string { return "kiro" }

func (e *KiroExecutor) Supports(kind string) bool { return kind == "llm" }

func (e *KiroExecutor) ChatCompletion(ctx context.Context, req ChatRequest, model string) (ChatResponse, error) {
	stream, err := e.StreamChatCompletion(ctx, req, model)
	if err != nil {
		return ChatResponse{}, err
	}
	resp := ChatResponse{
		ID:      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []ChatChoice{{Index: 0, Message: ChatMessage{Role: "assistant"}}},
	}
	var toolCalls []ToolCall
	for chunk := range stream {
		if len(chunk.Choices) == 0 {
			if chunk.Usage != nil {
				resp.Usage = chunk.Usage
			}
			continue
		}
		delta := chunk.Choices[0].Delta
		if content, ok := delta.Content.(string); ok && content != "" {
			resp.Choices[0].Message.Content = kiroStringValue(resp.Choices[0].Message.Content) + content
		}
		if len(delta.ToolCalls) > 0 {
			for _, tc := range delta.ToolCalls {
				toolCalls = append(toolCalls, ToolCall{ID: tc.ID, Type: tc.Type, Function: tc.Function})
			}
		}
		if delta.Thinking != "" {
			resp.Choices[0].Message.ReasoningContent += delta.Thinking
		}
		if chunk.Usage != nil {
			resp.Usage = chunk.Usage
		}
		if chunk.Choices[0].FinishReason != nil {
			resp.Choices[0].FinishReason = *chunk.Choices[0].FinishReason
		}
	}
	if len(toolCalls) > 0 {
		resp.Choices[0].Message.ToolCalls = toolCalls
	}
	return resp, nil
}

func (e *KiroExecutor) StreamChatCompletion(ctx context.Context, req ChatRequest, model string) (<-chan ChatChunk, error) {
	body, err := e.buildRequestBody(ctx, req, model)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	for k, v := range e.buildHeaders() {
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
	ch := make(chan ChatChunk, 32)
	go e.readEventStream(resp.Body, model, ch)
	return ch, nil
}

func (e *KiroExecutor) buildHeaders() map[string]string {
	headers := map[string]string{
		"Content-Type":       "application/json",
		"Accept":             "application/vnd.amazon.eventstream",
		"X-Amz-Target":       "AmazonCodeWhispererStreamingService.GenerateAssistantResponse",
		"User-Agent":         "AWS-SDK-JS/3.0.0 kiro-ide/1.0.0",
		"X-Amz-User-Agent":   "aws-sdk-js/3.0.0 kiro-ide/1.0.0",
		"Amz-Sdk-Request":    "attempt=1; max=3",
		"Amz-Sdk-Invocation-Id": fmt.Sprintf("kiro-%d", time.Now().UnixNano()),
	}
	if len(e.cfg.Accounts) > 0 && e.cfg.Accounts[0].AccessToken != "" {
		headers["Authorization"] = "Bearer " + e.cfg.Accounts[0].AccessToken
	}
	for k, v := range e.headers {
		headers[k] = v
	}
	return headers
}

func (e *KiroExecutor) NeedsRefresh(leeway time.Duration) bool {
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

func (e *KiroExecutor) RefreshCredentials(ctx context.Context) (KiroRefreshResult, error) {
	if len(e.cfg.Accounts) == 0 || strings.TrimSpace(e.cfg.Accounts[0].RefreshToken) == "" {
		return KiroRefreshResult{}, fmt.Errorf("kiro refresh token is required")
	}
	payload := map[string]string{"refreshToken": e.cfg.Accounts[0].RefreshToken}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.kiroRefreshURL(), bytes.NewReader(body))
	if err != nil {
		return KiroRefreshResult{}, fmt.Errorf("create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := e.client.Do(req)
	if err != nil {
		return KiroRefreshResult{}, fmt.Errorf("execute refresh request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return KiroRefreshResult{}, fmt.Errorf("kiro refresh failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var parsed struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ProfileARN   string `json:"profileArn"`
		ExpiresIn    int    `json:"expiresIn"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return KiroRefreshResult{}, fmt.Errorf("decode refresh response: %w", err)
	}
	if parsed.AccessToken == "" {
		return KiroRefreshResult{}, fmt.Errorf("kiro refresh response missing accessToken")
	}
	if parsed.RefreshToken == "" {
		parsed.RefreshToken = e.cfg.Accounts[0].RefreshToken
	}
	if parsed.ExpiresIn == 0 {
		parsed.ExpiresIn = 3600
	}
	return KiroRefreshResult{AccessToken: parsed.AccessToken, RefreshToken: parsed.RefreshToken, ProfileARN: parsed.ProfileARN, ExpiresAt: time.Now().Add(time.Duration(parsed.ExpiresIn) * time.Second)}, nil
}

func (e *KiroExecutor) kiroRefreshURL() string {
	if v, ok := e.cfg.ProviderSpecificData["refresh_url"].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	if len(e.cfg.Accounts) > 0 && e.cfg.Accounts[0].ProviderSpecificData != nil {
		if v, ok := e.cfg.Accounts[0].ProviderSpecificData["refresh_url"].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return "https://prod.us-east-1.auth.desktop.kiro.dev/refreshToken"
}

func (e *KiroExecutor) buildRequestBody(ctx context.Context, req ChatRequest, model string) ([]byte, error) {
	body := map[string]interface{}{
		"model":       model,
		"messages":    req.Messages,
		"tools":       req.Tools,
		"temperature": req.Temperature,
		"top_p":       req.TopP,
	}
	if req.MaxTokens != nil {
		body["max_tokens"] = *req.MaxTokens
	}
	translated, err := e.translator.TranslateRequestJSON(ctx, translator.FormatOpenAI, translator.FormatKiro, mustMarshal(body))
	if err != nil {
		return nil, fmt.Errorf("translate request: %w", err)
	}
	return translated, nil
}

func (e *KiroExecutor) readEventStream(body io.ReadCloser, model string, ch chan<- ChatChunk) {
	defer close(ch)
	defer body.Close()
	reader := bufio.NewReader(body)
	responseID := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	created := time.Now().Unix()
	state := kiroStreamState{}
	for {
		frame, err := readKiroEventFrame(reader)
		if err != nil {
			if err == io.EOF {
				break
			}
			return
		}
		eventType := frame.Headers[":event-type"]
		e.handleKiroEvent(eventType, frame.Payload, responseID, created, model, &state, ch)
	}
	if !state.finishEmitted {
		ch <- kiroFinishChunk(responseID, created, model, state.hasToolCalls, state.usage)
	}
}

type kiroStreamState struct {
	hasToolCalls     bool
	finishEmitted    bool
	hasMeteringEvent bool
	hasContextUsage  bool
	totalContentLen  int
	usage            *Usage
	toolIndex        int
	toolSeen         map[string]int
}

type kiroEventFrame struct {
	Headers map[string]string
	Payload map[string]interface{}
}

func readKiroEventFrame(r *bufio.Reader) (kiroEventFrame, error) {
	prelude := make([]byte, 12)
	if _, err := io.ReadFull(r, prelude); err != nil {
		return kiroEventFrame{}, err
	}
	totalLength := binary.BigEndian.Uint32(prelude[0:4])
	headersLength := binary.BigEndian.Uint32(prelude[4:8])
	if totalLength < 16 {
		return kiroEventFrame{}, fmt.Errorf("invalid event frame")
	}
	rest := make([]byte, int(totalLength)-12)
	if _, err := io.ReadFull(r, rest); err != nil {
		return kiroEventFrame{}, err
	}
	data := append(prelude, rest...)
	headers, payload := parseKiroEventFrame(data, int(headersLength))
	return kiroEventFrame{Headers: headers, Payload: payload}, nil
}

func parseKiroEventFrame(data []byte, headersLength int) (map[string]string, map[string]interface{}) {
	headers := map[string]string{}
	offset := 12
	headerEnd := 12 + headersLength
	for offset < headerEnd && offset < len(data) {
		nameLen := int(data[offset])
		offset++
		if offset+nameLen > len(data) {
			break
		}
		name := string(data[offset : offset+nameLen])
		offset += nameLen
		if offset >= len(data) {
			break
		}
		headerType := data[offset]
		offset++
		if headerType != 7 || offset+2 > len(data) {
			break
		}
		valueLen := int(binary.BigEndian.Uint16(data[offset : offset+2]))
		offset += 2
		if offset+valueLen > len(data) {
			break
		}
		headers[name] = string(data[offset : offset+valueLen])
		offset += valueLen
	}
	payloadStart := 12 + headersLength
	payloadEnd := len(data) - 4
	if payloadEnd <= payloadStart {
		return headers, nil
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(data[payloadStart:payloadEnd], &payload); err != nil {
		return headers, map[string]interface{}{"raw": string(data[payloadStart:payloadEnd])}
	}
	return headers, payload
}

func (e *KiroExecutor) handleKiroEvent(eventType string, payload map[string]interface{}, responseID string, created int64, model string, state *kiroStreamState, ch chan<- ChatChunk) {
	if state.toolSeen == nil {
		state.toolSeen = map[string]int{}
	}
	switch eventType {
	case "assistantResponseEvent", "codeEvent":
		content := kiroStringValue(payload["content"])
		if content == "" {
			return
		}
		state.totalContentLen += len(content)
		ch <- ChatChunk{ID: responseID, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{Role: "assistant", Content: content}}}}
	case "toolUseEvent":
		state.hasToolCalls = true
		toolPayloads := []interface{}{payload}
		if arr, ok := payload["toolUseEvent"].([]interface{}); ok {
			toolPayloads = arr
		}
		for _, raw := range toolPayloads {
			toolMap, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			toolID := kiroFirstNonEmpty(kiroStringValue(toolMap["toolUseId"]), fmt.Sprintf("call_%d", time.Now().UnixNano()))
			idx, exists := state.toolSeen[toolID]
			if !exists {
				idx = state.toolIndex
				state.toolSeen[toolID] = idx
				state.toolIndex++
				ch <- ChatChunk{ID: responseID, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{Role: "assistant", ToolCalls: []ToolCallDelta{{Index: idx, ID: toolID, Type: "function", Function: &FunctionCall{Name: kiroStringValue(toolMap["name"]), Arguments: json.RawMessage(`""`)}}}}}}}
			}
			if input, ok := toolMap["input"]; ok {
				encoded, _ := json.Marshal(input)
				ch <- ChatChunk{ID: responseID, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{ToolCalls: []ToolCallDelta{{Index: idx, ID: toolID, Type: "function", Function: &FunctionCall{Arguments: encoded}}}}}}}
			}
		}
	case "messageStopEvent":
		state.finishEmitted = true
		ch <- kiroFinishChunk(responseID, created, model, state.hasToolCalls, state.usage)
	case "metricsEvent":
		metrics := payload
		if nested, ok := payload["metricsEvent"].(map[string]interface{}); ok {
			metrics = nested
		}
		in := int(numberValue(metrics["inputTokens"]))
		out := int(numberValue(metrics["outputTokens"]))
		if in > 0 || out > 0 {
			state.usage = &Usage{PromptTokens: in, CompletionTokens: out, TotalTokens: in + out}
		}
	case "meteringEvent":
		state.hasMeteringEvent = true
	case "contextUsageEvent":
		state.hasContextUsage = true
		if state.hasMeteringEvent && !state.finishEmitted {
			state.finishEmitted = true
			if state.usage == nil {
				estOut := maxInt(1, state.totalContentLen/4)
				state.usage = &Usage{CompletionTokens: estOut, TotalTokens: estOut}
			}
			ch <- kiroFinishChunk(responseID, created, model, state.hasToolCalls, state.usage)
		}
	}
}

func kiroFinishChunk(responseID string, created int64, model string, hasToolCalls bool, usage *Usage) ChatChunk {
	finishReason := "stop"
	if hasToolCalls {
		finishReason = "tool_calls"
	}
	return ChatChunk{ID: responseID, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{}, FinishReason: &finishReason}}, Usage: usage}
}

func numberValue(v interface{}) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	default:
		return 0
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func kiroStringValue(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func kiroFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func init() {
	RegisterExecutor("kiro", func(cfg config.ProviderConfig, errorCfg config.ErrorConfig) (Executor, error) {
		return NewKiroExecutor(cfg, errorCfg)
	})
}
