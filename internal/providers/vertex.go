package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/edodoyokz/ai-go-router/internal/config"
	"github.com/edodoyokz/ai-go-router/internal/translator"
)

// VertexAdapter implements the Adapter interface for Google Cloud Vertex AI.
// It supports both Gemini models (via Gemini format) and partner models
// (via an OpenAI-compatible endpoint).
type VertexAdapter struct {
	name                 string
	providerID           string
	baseURL              string
	headers              map[string]string
	errorConfig          config.ErrorConfig
	httpClient           *http.Client
	translator           *translator.Registry
	accountSelector      *AccountSelector
	gcpProjectID         string
	providerSpecificData map[string]any
	accounts             []config.AccountConfig
}

func NewVertexAdapter(cfg config.ProviderConfig, errorConfig config.ErrorConfig, translatorReg *translator.Registry, proxyURL string) *VertexAdapter {
	accounts := make(map[string]string)
	for _, account := range cfg.Accounts {
		accounts[account.Name] = account.APIKey
	}
	return &VertexAdapter{
		name:                 cfg.Name,
		providerID:           cfg.ProviderID,
		baseURL:              cfg.BaseURL,
		headers:              cfg.Headers,
		errorConfig:          errorConfig,
		httpClient:           createHTTPClient(proxyURL),
		translator:           translatorReg,
		accountSelector:      NewAccountSelector(accounts, cfg.APIKey),
		gcpProjectID:         cfg.GCPProjectID,
		providerSpecificData: cfg.ProviderSpecificData,
		accounts:             cfg.Accounts,
	}
}

func (a *VertexAdapter) Name() string           { return a.name }
func (a *VertexAdapter) AccountNames() []string { return a.accountSelector.AccountNames() }

func (a *VertexAdapter) isPartner() bool {
	return a.providerID == "vertex-partner"
}

// vertexCredentials holds resolved credentials for a Vertex request.
type vertexCredentials struct {
	accessToken string
	rawKey      string
	projectID   string
	location    string
	isSA        bool
}

// resolveVertexCredentials resolves credentials from account config or provider config.
func (a *VertexAdapter) resolveVertexCredentials(accountName string) (*vertexCredentials, error) {
	_, apiKey := a.accountSelector.GetAccount(accountName)
	
	creds := &vertexCredentials{
		location: "us-central1", // default
	}

	// Try to parse as Service Account JSON
	sa, err := ParseVertexServiceAccount(apiKey)
	if err != nil {
		return nil, fmt.Errorf("invalid service account JSON: %w", err)
	}

	if sa != nil {
		// Service Account flow: mint Bearer token
		creds.isSA = true
		creds.projectID = sa.ProjectID
		
		accessToken, err := GetVertexAccessToken(sa, a.httpClient)
		if err != nil {
			return nil, fmt.Errorf("failed to mint access token: %w", err)
		}
		creds.accessToken = accessToken
	} else {
		// Raw API key flow
		creds.rawKey = apiKey
	}

	// Resolve project ID from multiple sources (priority order)
	if creds.projectID == "" {
		// Check account-level project ID
		for _, acc := range a.accounts {
			if acc.Name == accountName || (accountName == "" && len(a.accounts) > 0) {
				if acc.ProjectID != "" {
					creds.projectID = acc.ProjectID
				}
				// Check account provider-specific data
				if acc.ProviderSpecificData != nil {
					if projID, ok := acc.ProviderSpecificData["projectId"].(string); ok && projID != "" {
						creds.projectID = projID
					}
					if loc, ok := acc.ProviderSpecificData["location"].(string); ok && loc != "" {
						creds.location = loc
					}
				}
				break
			}
		}
	}

	// Fallback to provider-level config
	if creds.projectID == "" && a.gcpProjectID != "" {
		creds.projectID = a.gcpProjectID
	}
	if creds.projectID == "" && a.providerSpecificData != nil {
		if projID, ok := a.providerSpecificData["projectId"].(string); ok && projID != "" {
			creds.projectID = projID
		}
	}

	// Check provider-level location
	if creds.location == "us-central1" && a.providerSpecificData != nil {
		if loc, ok := a.providerSpecificData["location"].(string); ok && loc != "" {
			creds.location = loc
		}
	}

	return creds, nil
}

func (a *VertexAdapter) buildURL(model string, stream bool, creds *vertexCredentials) string {
	if a.isPartner() {
		projectID := creds.projectID
		if projectID == "" {
			projectID = "unknown"
		}
		base := "https://aiplatform.googleapis.com"
		if a.baseURL != "" {
			base = strings.TrimRight(a.baseURL, "/")
		}
		url := fmt.Sprintf("%s/v1/projects/%s/locations/global/endpoints/openapi/chat/completions", base, projectID)
		if creds.rawKey != "" {
			url += "?key=" + creds.rawKey
		}
		return url
	}

	// Gemini on Vertex
	action := "generateContent"
	if stream {
		action = "streamGenerateContent"
	}

	// SA JSON + Bearer token: must use project-scoped path
	if creds.isSA && creds.projectID != "" {
		url := fmt.Sprintf("https://aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/google/models/%s:%s",
			creds.projectID, creds.location, model, action)
		if stream {
			url += "?alt=sse"
		}
		return url
	}

	// Raw API key: use global publishers endpoint
	url := fmt.Sprintf("https://aiplatform.googleapis.com/v1/publishers/google/models/%s:%s", model, action)
	if stream {
		url += "?alt=sse"
	}
	if creds.rawKey != "" {
		if stream {
			url += "&key=" + creds.rawKey
		} else {
			url += "?key=" + creds.rawKey
		}
	}
	return url
}

// applyHeaders sets headers for Vertex requests.
func (a *VertexAdapter) applyHeaders(req *http.Request, creds *vertexCredentials, stream bool) {
	req.Header.Set("Content-Type", "application/json")
	if creds.accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+creds.accessToken)
	}
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	for k, v := range a.headers {
		req.Header.Set(k, v)
	}
}

func (a *VertexAdapter) ChatCompletion(ctx context.Context, request ChatRequest, model string) (ChatResponse, error) {
	creds, err := a.resolveVertexCredentials("")
	if err != nil {
		return ChatResponse{}, NewNonRetryableError(a.name, model, "failed to resolve credentials", err)
	}

	// vertex-partner requires project ID
	if a.isPartner() && creds.projectID == "" {
		// Try to auto-resolve from raw key
		if creds.rawKey != "" {
			projectID, err := ResolveVertexProjectID(creds.rawKey, a.httpClient)
			if err != nil {
				return ChatResponse{}, NewNonRetryableError(a.name, model, "vertex-partner requires project_id; could not auto-resolve from API key", err)
			}
			creds.projectID = projectID
		} else {
			return ChatResponse{}, NewNonRetryableError(a.name, model, "vertex-partner requires project_id; add it in account or provider config", nil)
		}
	}

	url := a.buildURL(model, false, creds)

	var body []byte

	if a.isPartner() {
		request.Model = model
		body, err = json.Marshal(request)
	} else {
		body, err = a.translateRequest(ctx, request, model)
	}
	if err != nil {
		return ChatResponse{}, NewNonRetryableError(a.name, model, "failed to build request", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return ChatResponse{}, NewNonRetryableError(a.name, model, "failed to create request", err)
	}
	a.applyHeaders(req, creds, false)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return ChatResponse{}, NewRetryableError(a.name, model, "network error", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return ChatResponse{}, ClassifyHTTPError(resp.StatusCode, a.name, model, string(respBody), a.errorConfig)
	}

	if a.isPartner() {
		var out ChatResponse
		if err := json.Unmarshal(respBody, &out); err != nil {
			return ChatResponse{}, NewNonRetryableError(a.name, model, "invalid upstream response", err)
		}
		return out, nil
	}

	return a.translateResponse(ctx, respBody, model)
}

func (a *VertexAdapter) StreamChatCompletion(ctx context.Context, request ChatRequest, model string) (<-chan ChatChunk, error) {
	creds, err := a.resolveVertexCredentials("")
	if err != nil {
		return nil, NewNonRetryableError(a.name, model, "failed to resolve credentials", err)
	}

	// vertex-partner requires project ID
	if a.isPartner() && creds.projectID == "" {
		// Try to auto-resolve from raw key
		if creds.rawKey != "" {
			projectID, err := ResolveVertexProjectID(creds.rawKey, a.httpClient)
			if err != nil {
				return nil, NewNonRetryableError(a.name, model, "vertex-partner requires project_id; could not auto-resolve from API key", err)
			}
			creds.projectID = projectID
		} else {
			return nil, NewNonRetryableError(a.name, model, "vertex-partner requires project_id; add it in account or provider config", nil)
		}
	}

	url := a.buildURL(model, true, creds)

	var body []byte

	if a.isPartner() {
		request.Model = model
		request.Stream = true
		body, err = json.Marshal(request)
	} else {
		body, err = a.translateRequest(ctx, request, model)
	}
	if err != nil {
		return nil, NewNonRetryableError(a.name, model, "failed to build request", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, NewNonRetryableError(a.name, model, "failed to create request", err)
	}
	a.applyHeaders(req, creds, true)

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
	if a.isPartner() {
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
			var geminiChunk map[string]interface{}
			if json.Unmarshal([]byte(data), &geminiChunk) != nil {
				continue
			}
			openAIBody, err := a.translator.TranslateResponseJSON(ctx, translator.FormatVertex, translator.FormatOpenAI, []byte(data))
			if err != nil {
				continue
			}
			var chunk ChatChunk
			if json.Unmarshal(openAIBody, &chunk) == nil {
				chunks <- chunk
			}
		}
	}()
	return chunks, nil
}

func (a *VertexAdapter) translateRequest(ctx context.Context, request ChatRequest, model string) ([]byte, error) {
	messages := make([]map[string]interface{}, len(request.Messages))
	for i, msg := range request.Messages {
		messages[i] = map[string]interface{}{
			"role":    msg.Role,
			"content": msg.Content,
		}
	}
	requestBody := map[string]interface{}{
		"model":    model,
		"messages": messages,
		"stream":   request.Stream,
	}
	if request.Temperature != nil && *request.Temperature > 0 {
		requestBody["temperature"] = *request.Temperature
	}
	if request.TopP != nil && *request.TopP > 0 {
		requestBody["top_p"] = *request.TopP
	}
	if request.MaxTokens != nil && *request.MaxTokens > 0 {
		requestBody["max_tokens"] = *request.MaxTokens
	}
	if len(request.Stop) > 0 {
		requestBody["stop"] = request.Stop
	}

	vertexReq, err := a.translator.TranslateRequestJSON(ctx, translator.FormatOpenAI, translator.FormatVertex, mustMarshal(requestBody))
	if err != nil {
		return nil, fmt.Errorf("translate request: %w", err)
	}
	return vertexReq, nil
}

func (a *VertexAdapter) translateResponse(ctx context.Context, respBody []byte, model string) (ChatResponse, error) {
	openAIBody, err := a.translator.TranslateResponseJSON(ctx, translator.FormatVertex, translator.FormatOpenAI, respBody)
	if err != nil {
		return ChatResponse{}, &ProviderError{
			Provider: a.name,
			Model:    model,
			Type:     ErrInvalidUpstreamResponse,
			Message:  "failed to translate response",
			Cause:    err,
		}
	}
	var response ChatResponse
	if err := json.Unmarshal(openAIBody, &response); err != nil {
		return ChatResponse{}, &ProviderError{
			Provider: a.name,
			Model:    model,
			Type:     ErrInvalidUpstreamResponse,
			Message:  "failed to parse translated response",
			Cause:    err,
		}
	}
	return response, nil
}

func mustMarshal(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}

func (a *VertexAdapter) GetUsage(context.Context) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}
func (a *VertexAdapter) Embeddings(context.Context, EmbeddingsRequest, string) (EmbeddingsResponse, error) {
	return EmbeddingsResponse{}, &ProviderError{Provider: a.name, Type: ErrNonRetryable, Message: "embeddings not supported"}
}
func (a *VertexAdapter) AudioSpeech(context.Context, AudioSpeechRequest, string) (AudioSpeechResponse, error) {
	return AudioSpeechResponse{}, &ProviderError{Provider: a.name, Type: ErrNonRetryable, Message: "audio speech not supported"}
}
func (a *VertexAdapter) ImagesGenerations(context.Context, ImagesGenerationsRequest, string) (ImagesGenerationsResponse, error) {
	return ImagesGenerationsResponse{}, &ProviderError{Provider: a.name, Type: ErrNonRetryable, Message: "images not supported"}
}
