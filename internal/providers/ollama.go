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

type OllamaAdapter struct {
	name               string
	baseURL            string
	headers            map[string]string
	errorConfig        config.ErrorConfig
	httpClient         *http.Client
	translatorRegistry *translator.Registry
	accountSelector    *AccountSelector
}

func NewOllamaAdapter(cfg config.ProviderConfig, errorConfig config.ErrorConfig, registry *translator.Registry, proxyURL string) *OllamaAdapter {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = "http://127.0.0.1:11434"
	}
	accounts := make(map[string]string)
	for _, account := range cfg.Accounts {
		accounts[account.Name] = account.APIKey
	}
	return &OllamaAdapter{
		name:               cfg.Name,
		baseURL:            baseURL,
		headers:            cfg.Headers,
		errorConfig:        errorConfig,
		httpClient:         createHTTPClient(proxyURL),
		translatorRegistry: registry,
		accountSelector:    NewAccountSelector(accounts, cfg.APIKey),
	}
}

func (a *OllamaAdapter) Name() string { return a.name }

func (a *OllamaAdapter) AccountNames() []string { return a.accountSelector.AccountNames() }

func (a *OllamaAdapter) ChatCompletion(ctx context.Context, request ChatRequest, model string) (ChatResponse, error) {
	request.Model = model
	request.Stream = false
	body, err := json.Marshal(request)
	if err != nil {
		return ChatResponse{}, NewNonRetryableError(a.name, model, "failed to marshal request", err)
	}
	translated, err := a.translatorRegistry.TranslateRequestJSON(ctx, translator.FormatOpenAI, translator.FormatOllama, body)
	if err != nil {
		return ChatResponse{}, NewNonRetryableError(a.name, model, "failed to translate request", err)
	}

	respBody, err := a.doChat(ctx, translated, model)
	if err != nil {
		return ChatResponse{}, err
	}
	openAIResp, err := a.translatorRegistry.TranslateResponseJSON(ctx, translator.FormatOllama, translator.FormatOpenAI, respBody)
	if err != nil {
		return ChatResponse{}, &ProviderError{Provider: a.name, Model: model, Type: ErrInvalidUpstreamResponse, Message: "failed to translate response", Cause: err}
	}
	var chatResp ChatResponse
	if err := json.Unmarshal(openAIResp, &chatResp); err != nil {
		return ChatResponse{}, &ProviderError{Provider: a.name, Model: model, Type: ErrInvalidUpstreamResponse, Message: "failed to parse translated response", Cause: err}
	}
	return chatResp, nil
}

func (a *OllamaAdapter) StreamChatCompletion(ctx context.Context, request ChatRequest, model string) (<-chan ChatChunk, error) {
	request.Model = model
	request.Stream = true
	body, err := json.Marshal(request)
	if err != nil {
		return nil, NewNonRetryableError(a.name, model, "failed to marshal request", err)
	}
	translated, err := a.translatorRegistry.TranslateRequestJSON(ctx, translator.FormatOpenAI, translator.FormatOllama, body)
	if err != nil {
		return nil, NewNonRetryableError(a.name, model, "failed to translate request", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/api/chat", bytes.NewReader(translated))
	if err != nil {
		return nil, NewNonRetryableError(a.name, model, "failed to create request", err)
	}
	a.setHeaders(req)
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, NewRetryableError(a.name, model, "network error", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		return nil, ClassifyHTTPError(resp.StatusCode, a.name, model, string(respBody), a.errorConfig)
	}

	chunks := make(chan ChatChunk, 10)
	go func() {
		defer close(chunks)
		defer resp.Body.Close()
		decoder := json.NewDecoder(resp.Body)
		for {
			var event map[string]any
			if err := decoder.Decode(&event); err != nil {
				return
			}
			chunk := ollamaEventToChunk(event)
			select {
			case chunks <- chunk:
			case <-ctx.Done():
				return
			}
			if done, _ := event["done"].(bool); done {
				return
			}
		}
	}()
	return chunks, nil
}

func (a *OllamaAdapter) doChat(ctx context.Context, body []byte, model string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, NewNonRetryableError(a.name, model, "failed to create request", err)
	}
	a.setHeaders(req)
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, NewRetryableError(a.name, model, "network error", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, NewRetryableError(a.name, model, "failed to read response", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, ClassifyHTTPError(resp.StatusCode, a.name, model, string(respBody), a.errorConfig)
	}
	return respBody, nil
}

func (a *OllamaAdapter) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	for key, value := range a.headers {
		req.Header.Set(key, value)
	}
}

func ollamaEventToChunk(event map[string]any) ChatChunk {
	model, _ := event["model"].(string)
	content := ""
	if msg, ok := event["message"].(map[string]any); ok {
		content, _ = msg["content"].(string)
	}
	finishReason := (*string)(nil)
	if done, _ := event["done"].(bool); done {
		reason := "stop"
		if doneReason, _ := event["done_reason"].(string); doneReason == "length" {
			reason = "length"
		}
		finishReason = &reason
	}
	return ChatChunk{
		ID:      fmt.Sprintf("ollama-%d", time.Now().UnixMicro()),
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{Role: "assistant", Content: content}, FinishReason: finishReason}},
	}
}

func (a *OllamaAdapter) GetUsage(ctx context.Context) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

func (a *OllamaAdapter) Embeddings(ctx context.Context, request EmbeddingsRequest, model string) (EmbeddingsResponse, error) {
	payload := map[string]any{
		"model":  model,
		"prompt": request.Input,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return EmbeddingsResponse{}, NewNonRetryableError(a.name, model, "failed to marshal embeddings request", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/api/embeddings", bytes.NewReader(body))
	if err != nil {
		return EmbeddingsResponse{}, NewNonRetryableError(a.name, model, "failed to create embeddings request", err)
	}
	a.setHeaders(req)
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return EmbeddingsResponse{}, NewRetryableError(a.name, model, "network error", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return EmbeddingsResponse{}, NewRetryableError(a.name, model, "failed to read embeddings response", err)
	}
	if resp.StatusCode != http.StatusOK {
		return EmbeddingsResponse{}, ClassifyHTTPError(resp.StatusCode, a.name, model, string(respBody), a.errorConfig)
	}
	embedding, err := parseOllamaEmbedding(respBody)
	if err != nil {
		return EmbeddingsResponse{}, &ProviderError{Provider: a.name, Model: model, Type: ErrInvalidUpstreamResponse, Message: "failed to parse embeddings response", Cause: err}
	}
	tokenEstimate := len(request.Input) / 4
	if len(request.Input)%4 != 0 {
		tokenEstimate++
	}
	return EmbeddingsResponse{
		Object: "list",
		Model:  model,
		Data:   []Embedding{{Object: "embedding", Embedding: embedding, Index: 0}},
		Usage:  EmbeddingUsage{PromptTokens: tokenEstimate, TotalTokens: tokenEstimate},
	}, nil
}

func parseOllamaEmbedding(body []byte) ([]float64, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	if embedding, ok := floatSlice(raw["embedding"]); ok {
		return embedding, nil
	}
	if embeddings, ok := raw["embeddings"].([]any); ok && len(embeddings) > 0 {
		if embedding, ok := floatSlice(embeddings[0]); ok {
			return embedding, nil
		}
	}
	return nil, fmt.Errorf("missing embedding")
}

func floatSlice(v any) ([]float64, bool) {
	items, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]float64, 0, len(items))
	for _, item := range items {
		n, ok := item.(float64)
		if !ok {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}

func (a *OllamaAdapter) AudioSpeech(ctx context.Context, request AudioSpeechRequest, model string) (AudioSpeechResponse, error) {
	return AudioSpeechResponse{}, NewNonRetryableError(a.name, model, "ollama audio speech is not supported", nil)
}

func (a *OllamaAdapter) ImagesGenerations(ctx context.Context, request ImagesGenerationsRequest, model string) (ImagesGenerationsResponse, error) {
	return ImagesGenerationsResponse{}, NewNonRetryableError(a.name, model, "ollama image generation is not supported", nil)
}
