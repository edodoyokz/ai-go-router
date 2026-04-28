package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/edodoyokz/ai-go-router/internal/config"
	"github.com/edodoyokz/ai-go-router/internal/translator"
)

type CursorExecutor struct {
	cfg        config.ProviderConfig
	errorCfg   config.ErrorConfig
	client     *http.Client
	translator *translator.Registry
	baseURL    string
	chatPath   string
}

func NewCursorExecutor(cfg config.ProviderConfig, errorCfg config.ErrorConfig) (Executor, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://api2.cursor.sh"
	}
	if err := validateCursorCredentials(cfg); err != nil {
		return nil, err
	}
	return &CursorExecutor{
		cfg:        cfg,
		errorCfg:   errorCfg,
		client:     &http.Client{Timeout: 120 * time.Second},
		translator: translator.NewRegistry(),
		baseURL:    baseURL,
		chatPath:   "/aiserver.v1.ChatService/StreamUnifiedChatWithTools",
	}, nil
}

func validateCursorCredentials(cfg config.ProviderConfig) error {
	if strings.TrimSpace(cfg.APIKey) != "" {
		return nil
	}
	for _, account := range cfg.Accounts {
		if strings.TrimSpace(account.AccessToken) != "" {
			return nil
		}
	}
	return fmt.Errorf("cursor credentials incomplete: provide access_token or api_key")
}

func (e *CursorExecutor) ProviderID() string { return "cursor" }

func (e *CursorExecutor) Supports(kind string) bool { return kind == "llm" }

func (e *CursorExecutor) ChatCompletion(ctx context.Context, req ChatRequest, model string) (ChatResponse, error) {
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
			continue
		}
		delta := chunk.Choices[0].Delta
		if content, ok := delta.Content.(string); ok && content != "" {
			resp.Choices[0].Message.Content = cursorAppendString(resp.Choices[0].Message.Content, content)
		}
		if delta.Thinking != "" {
			resp.Choices[0].Message.ReasoningContent += delta.Thinking
		}
		for _, tc := range delta.ToolCalls {
			toolCalls = append(toolCalls, ToolCall{ID: tc.ID, Type: tc.Type, Function: tc.Function})
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

func (e *CursorExecutor) StreamChatCompletion(ctx context.Context, req ChatRequest, model string) (<-chan ChatChunk, error) {
	body, headers, err := e.buildRequest(ctx, req, model)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+e.chatPath, bytes.NewReader(body))
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
		return nil, ClassifyHTTPError(resp.StatusCode, e.cfg.Name, model, string(respBody), e.errorCfg)
	}
	ch := make(chan ChatChunk, 32)
	go e.readStream(resp.Body, model, ch)
	return ch, nil
}

func (e *CursorExecutor) buildRequest(ctx context.Context, req ChatRequest, model string) ([]byte, map[string]string, error) {
	accessToken := strings.TrimSpace(e.cfg.APIKey)
	machineID := ""
	ghostMode := true
	if len(e.cfg.Accounts) > 0 {
		acct := e.cfg.Accounts[0]
		if acct.AccessToken != "" {
			accessToken = acct.AccessToken
		}
		if acct.ProviderSpecificData != nil {
			if mid, ok := acct.ProviderSpecificData["machineId"].(string); ok {
				machineID = mid
			}
			if gm, ok := acct.ProviderSpecificData["ghostMode"].(bool); ok {
				ghostMode = gm
			}
		}
	}
	headers, err := cursorBuildHeaders(accessToken, machineID, ghostMode, time.Now())
	if err != nil {
		return nil, nil, err
	}
	reasoningEffort := ""
	if req.Reasoning != nil {
		reasoningEffort = req.Reasoning.Effort
	}
	requestBody := map[string]interface{}{
		"model":             model,
		"messages":          req.Messages,
		"tools":             req.Tools,
		"reasoning_effort":  reasoningEffort,
	}
	translated, err := e.translator.TranslateRequestJSON(ctx, translator.FormatOpenAI, translator.FormatCursor, mustMarshal(requestBody))
	if err != nil {
		return nil, nil, fmt.Errorf("translate request: %w", err)
	}
	var translatedBody struct {
		Messages []ChatMessage `json:"messages"`
	}
	if err := json.Unmarshal(translated, &translatedBody); err != nil {
		return nil, nil, fmt.Errorf("decode translated cursor request: %w", err)
	}
	payload := cursorGenerateBody(translatedBody.Messages, model)
	for k, v := range e.cfg.Headers {
		headers[k] = v
	}
	return payload, headers, nil
}

func (e *CursorExecutor) readStream(body io.ReadCloser, model string, ch chan<- ChatChunk) {
	defer close(ch)
	defer body.Close()
	buf, err := io.ReadAll(body)
	if err != nil {
		return
	}
	responseID := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	created := time.Now().Unix()
	for len(buf) > 0 {
		frame, ok := cursorParseConnectRPCFrame(buf)
		if !ok {
			break
		}
		buf = buf[frame.Consumed:]
		decoded := cursorExtractResponse(frame.Payload)
		if decoded.Text != "" {
			ch <- ChatChunk{ID: responseID, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{Role: "assistant", Content: decoded.Text}}}}
		}
		if decoded.Thinking != "" {
			ch <- ChatChunk{ID: responseID, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{Thinking: decoded.Thinking}}}}
		}
		if decoded.ToolCall != nil {
			ch <- ChatChunk{ID: responseID, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{Role: "assistant", ToolCalls: []ToolCallDelta{{Index: 0, ID: decoded.ToolCall.ID, Type: decoded.ToolCall.Type, Function: decoded.ToolCall.Function}}}}}}
		}
	}
	finish := "stop"
	ch <- ChatChunk{ID: responseID, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{}, FinishReason: &finish}}}
}

func cursorAppendString(existing any, extra string) string {
	if s, ok := existing.(string); ok {
		return s + extra
	}
	return extra
}

func init() {
	RegisterExecutor("cursor", func(cfg config.ProviderConfig, errorCfg config.ErrorConfig) (Executor, error) {
		return NewCursorExecutor(cfg, errorCfg)
	})
}
