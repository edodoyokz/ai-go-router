package providers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/edodoyokz/ai-go-router/internal/config"
)

// IFlowAdapter implements the iFlow API with HMAC-SHA256 signing.
type IFlowAdapter struct {
	name            string
	baseURL         string
	headers         map[string]string
	errorConfig     config.ErrorConfig
	httpClient      *http.Client
	accountSelector *AccountSelector
}

func NewIFlowAdapter(cfg config.ProviderConfig, errorConfig config.ErrorConfig, proxyURL string) *IFlowAdapter {
	accounts := make(map[string]string)
	for _, account := range cfg.Accounts {
		accounts[account.Name] = account.APIKey
	}
	return &IFlowAdapter{
		name:            cfg.Name,
		baseURL:         cfg.BaseURL,
		headers:         cfg.Headers,
		errorConfig:     errorConfig,
		httpClient:      createHTTPClient(proxyURL),
		accountSelector: NewAccountSelector(accounts, cfg.APIKey),
	}
}

func (a *IFlowAdapter) Name() string           { return a.name }
func (a *IFlowAdapter) AccountNames() []string { return a.accountSelector.AccountNames() }

func randomIFlowSessionID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "session-" + hex.EncodeToString(b)
}

func createIFlowSignature(userAgent, sessionID string, timestamp int64, apiKey string) string {
	if apiKey == "" {
		return ""
	}
	payload := fmt.Sprintf("%s:%s:%d", userAgent, sessionID, timestamp)
	mac := hmac.New(sha256.New, []byte(apiKey))
	_, _ = mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func (a *IFlowAdapter) applyHeaders(req *http.Request, apiKey string, stream bool) {
	sessionID := randomIFlowSessionID()
	ts := time.Now().UnixMilli()
	userAgent := a.headers["User-Agent"]
	if userAgent == "" {
		userAgent = "iFlow-Cli"
	}
	sig := createIFlowSignature(userAgent, sessionID, ts, apiKey)

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("session-id", sessionID)
	req.Header.Set("x-iflow-timestamp", fmt.Sprintf("%d", ts))
	req.Header.Set("x-iflow-signature", sig)
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	for k, v := range a.headers {
		req.Header.Set(k, v)
	}
}

func (a *IFlowAdapter) ChatCompletion(ctx context.Context, request ChatRequest, model string) (ChatResponse, error) {
	request.Model = model
	_, apiKey := a.accountSelector.GetAccount("")
	body, err := json.Marshal(request)
	if err != nil {
		return ChatResponse{}, NewNonRetryableError(a.name, model, "failed to marshal request", err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", a.baseURL, bytes.NewReader(body))
	if err != nil {
		return ChatResponse{}, NewNonRetryableError(a.name, model, "failed to create request", err)
	}
	a.applyHeaders(req, apiKey, false)
	resp, err := a.httpClient.Do(req)
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

func (a *IFlowAdapter) StreamChatCompletion(ctx context.Context, request ChatRequest, model string) (<-chan ChatChunk, error) {
	request.Model = model
	request.Stream = true
	if request.Extra == nil {
		request.Extra = map[string]json.RawMessage{}
	}
	if _, ok := request.Extra["stream_options"]; !ok {
		request.Extra["stream_options"] = json.RawMessage(`{"include_usage":true}`)
	}
	_, apiKey := a.accountSelector.GetAccount("")
	body, err := json.Marshal(request)
	if err != nil {
		return nil, NewNonRetryableError(a.name, model, "failed to marshal request", err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", a.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, NewNonRetryableError(a.name, model, "failed to create request", err)
	}
	a.applyHeaders(req, apiKey, true)
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, NewRetryableError(a.name, model, "network error", err)
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

func (a *IFlowAdapter) GetUsage(context.Context) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}
func (a *IFlowAdapter) Embeddings(context.Context, EmbeddingsRequest, string) (EmbeddingsResponse, error) {
	return EmbeddingsResponse{}, &ProviderError{Provider: a.name, Type: ErrNonRetryable, Message: "embeddings not supported"}
}
func (a *IFlowAdapter) AudioSpeech(context.Context, AudioSpeechRequest, string) (AudioSpeechResponse, error) {
	return AudioSpeechResponse{}, &ProviderError{Provider: a.name, Type: ErrNonRetryable, Message: "audio speech not supported"}
}
func (a *IFlowAdapter) ImagesGenerations(context.Context, ImagesGenerationsRequest, string) (ImagesGenerationsResponse, error) {
	return ImagesGenerationsResponse{}, &ProviderError{Provider: a.name, Type: ErrNonRetryable, Message: "images not supported"}
}
