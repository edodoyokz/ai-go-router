package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/edodoyokz/ai-go-router/internal/config"
)

var openCodeGoClaudeModels = map[string]struct{}{
	"minimax-m2.5": {},
	"minimax-m2.7": {},
}

const kimiReasoningPlaceholder = " "

// OpenCodeGoAdapter implements OpenCode Go provider behavior.
type OpenCodeGoAdapter struct {
	name            string
	baseURL         string
	errorConfig     config.ErrorConfig
	headers         map[string]string
	httpClient      *http.Client
	accountSelector *AccountSelector
}

func NewOpenCodeGoAdapter(cfg config.ProviderConfig, errorConfig config.ErrorConfig, proxyURL string) *OpenCodeGoAdapter {
	accounts := make(map[string]string)
	for _, account := range cfg.Accounts {
		accounts[account.Name] = account.APIKey
	}
	return &OpenCodeGoAdapter{
		name:            cfg.Name,
		baseURL:         strings.TrimRight(cfg.BaseURL, "/"),
		errorConfig:     errorConfig,
		headers:         cfg.Headers,
		httpClient:      createHTTPClient(proxyURL),
		accountSelector: NewAccountSelector(accounts, cfg.APIKey),
	}
}

func (a *OpenCodeGoAdapter) Name() string { return a.name }
func (a *OpenCodeGoAdapter) AccountNames() []string {
	return a.accountSelector.AccountNames()
}

func (a *OpenCodeGoAdapter) buildURL(model string) string {
	if _, ok := openCodeGoClaudeModels[model]; ok {
		return a.baseURL + "/messages"
	}
	return a.baseURL + "/chat/completions"
}

func (a *OpenCodeGoAdapter) transformRequest(req ChatRequest, model string) ChatRequest {
	if !strings.HasPrefix(model, "kimi-") {
		return req
	}
	for i := range req.Messages {
		msg := &req.Messages[i]
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			if msg.Extra == nil {
				msg.Extra = map[string]json.RawMessage{}
			}
			if _, exists := msg.Extra["reasoning_content"]; !exists {
				msg.Extra["reasoning_content"] = json.RawMessage("\"" + kimiReasoningPlaceholder + "\"")
			}
		}
	}
	return req
}

func (a *OpenCodeGoAdapter) applyHeaders(req *http.Request, model, apiKey string, stream bool) {
	req.Header.Set("Content-Type", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	if _, ok := openCodeGoClaudeModels[model]; ok {
		if apiKey != "" {
			req.Header.Set("x-api-key", apiKey)
		}
		req.Header.Set("anthropic-version", "2023-06-01")
	} else if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	for k, v := range a.headers {
		req.Header.Set(k, v)
	}
}

func (a *OpenCodeGoAdapter) ChatCompletion(ctx context.Context, request ChatRequest, model string) (ChatResponse, error) {
	request.Model = model
	request = a.transformRequest(request, model)
	_, apiKey := a.accountSelector.GetAccount("")
	body, err := json.Marshal(request)
	if err != nil {
		return ChatResponse{}, NewNonRetryableError(a.name, model, "failed to marshal request", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", a.buildURL(model), bytes.NewReader(body))
	if err != nil {
		return ChatResponse{}, NewNonRetryableError(a.name, model, "failed to create request", err)
	}
	a.applyHeaders(httpReq, model, apiKey, false)
	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		return ChatResponse{}, NewRetryableError(a.name, model, "network error", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return ChatResponse{}, ClassifyHTTPError(resp.StatusCode, a.name, model, string(respBody), a.errorConfig)
	}
	var out ChatResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return ChatResponse{}, NewNonRetryableError(a.name, model, "invalid upstream response", err)
	}
	return out, nil
}

func (a *OpenCodeGoAdapter) StreamChatCompletion(ctx context.Context, request ChatRequest, model string) (<-chan ChatChunk, error) {
	request.Model = model
	request.Stream = true
	request = a.transformRequest(request, model)
	_, apiKey := a.accountSelector.GetAccount("")
	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", a.buildURL(model), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	a.applyHeaders(httpReq, model, apiKey, true)
	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		return nil, ClassifyHTTPError(resp.StatusCode, a.name, model, string(respBody), a.errorConfig)
	}
	chunks := make(chan ChatChunk, 32)
	go func() {
		defer close(chunks)
		defer resp.Body.Close()
		dec := NewSSEDecoder(resp.Body)
		for {
			evt, err := dec.Next()
			if err != nil {
				return
			}
			data := strings.TrimSpace(evt.Data)
			if data == "" || data == "[DONE]" {
				if data == "[DONE]" {
					return
				}
				continue
			}
			var chunk ChatChunk
			if json.Unmarshal([]byte(data), &chunk) == nil {
				chunks <- chunk
			}
		}
	}()
	return chunks, nil
}

func (a *OpenCodeGoAdapter) GetUsage(context.Context) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}
func (a *OpenCodeGoAdapter) Embeddings(context.Context, EmbeddingsRequest, string) (EmbeddingsResponse, error) {
	return EmbeddingsResponse{}, &ProviderError{Provider: a.name, Type: ErrNonRetryable, Message: "embeddings not supported"}
}
func (a *OpenCodeGoAdapter) AudioSpeech(context.Context, AudioSpeechRequest, string) (AudioSpeechResponse, error) {
	return AudioSpeechResponse{}, &ProviderError{Provider: a.name, Type: ErrNonRetryable, Message: "audio speech not supported"}
}
func (a *OpenCodeGoAdapter) ImagesGenerations(context.Context, ImagesGenerationsRequest, string) (ImagesGenerationsResponse, error) {
	return ImagesGenerationsResponse{}, &ProviderError{Provider: a.name, Type: ErrNonRetryable, Message: "images not supported"}
}
