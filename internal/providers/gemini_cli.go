package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/edodoyokz/ai-go-router/internal/config"
	"github.com/edodoyokz/ai-go-router/internal/translator"
)

const geminiCLIVersion = "0.31.0"
const geminiCLIAPIClient = "google-genai-sdk/1.41.0 gl-node/v22.19.0"

// GeminiCLIAdapter implements the Adapter interface for Gemini CLI.
// It communicates using the native Gemini generateContent format.
type GeminiCLIAdapter struct {
	name                 string
	baseURL              string
	headers              map[string]string
	errorConfig          config.ErrorConfig
	httpClient           *http.Client
	translator           *translator.Registry
	accountSelector      *AccountSelector
	accounts             []config.AccountConfig
	providerSpecificData map[string]any
	oauthConfig          GoogleOAuthConfig
}

func NewGeminiCLIAdapter(cfg config.ProviderConfig, errorConfig config.ErrorConfig, translatorReg *translator.Registry, proxyURL string) *GeminiCLIAdapter {
	accounts := make(map[string]string)
	for _, account := range cfg.Accounts {
		accounts[account.Name] = account.APIKey
	}
	
	// Extract OAuth config from provider-specific data
	oauthConfig := GoogleOAuthConfig{
		TokenURL: "https://oauth2.googleapis.com/token",
	}
	if cfg.ProviderSpecificData != nil {
		if clientID, ok := cfg.ProviderSpecificData["clientId"].(string); ok {
			oauthConfig.ClientID = clientID
		}
		if clientSecret, ok := cfg.ProviderSpecificData["clientSecret"].(string); ok {
			oauthConfig.ClientSecret = clientSecret
		}
	}
	
	return &GeminiCLIAdapter{
		name:                 cfg.Name,
		baseURL:              strings.TrimRight(cfg.BaseURL, "/"),
		headers:              cfg.Headers,
		errorConfig:          errorConfig,
		httpClient:           createHTTPClient(proxyURL),
		translator:           translatorReg,
		accountSelector:      NewAccountSelector(accounts, cfg.APIKey),
		accounts:             cfg.Accounts,
		providerSpecificData: cfg.ProviderSpecificData,
		oauthConfig:          oauthConfig,
	}
}

func (a *GeminiCLIAdapter) Name() string           { return a.name }
func (a *GeminiCLIAdapter) AccountNames() []string { return a.accountSelector.AccountNames() }

func geminiCLIUserAgent(model string) string {
	os := runtime.GOOS
	arch := runtime.GOARCH
	if model == "" {
		model = "unknown"
	}
	return fmt.Sprintf("GeminiCLI/%s/%s (%s; %s)", geminiCLIVersion, model, os, arch)
}

func (a *GeminiCLIAdapter) buildURL(model string, stream bool) string {
	action := "generateContent"
	if stream {
		action = "streamGenerateContent?alt=sse"
	}
	base := a.baseURL
	if base == "" {
		base = "https://cloudcode-pa.googleapis.com/v1internal"
	}
	return fmt.Sprintf("%s:%s", base, action)
}

// geminiCLICredentials holds resolved credentials for a Gemini CLI request.
type geminiCLICredentials struct {
	accessToken  string
	refreshToken string
	expiresAt    *time.Time
	projectID    string
}

// resolveGeminiCLICredentials resolves credentials from account config.
func (a *GeminiCLIAdapter) resolveGeminiCLICredentials(accountName string) (*geminiCLICredentials, error) {
	_, apiKey := a.accountSelector.GetAccount(accountName)
	
	creds := &geminiCLICredentials{}

	// Find matching account for richer metadata
	var matchedAccount *config.AccountConfig
	for i := range a.accounts {
		if a.accounts[i].Name == accountName || (accountName == "" && len(a.accounts) > 0) {
			matchedAccount = &a.accounts[i]
			break
		}
	}

	// Prefer AccessToken for OAuth credentials, fallback to APIKey
	if matchedAccount != nil && matchedAccount.AccessToken != "" {
		creds.accessToken = matchedAccount.AccessToken
		creds.refreshToken = matchedAccount.RefreshToken
		creds.expiresAt = matchedAccount.ExpiresAt
		
		// Get project ID from account
		if matchedAccount.ProjectID != "" {
			creds.projectID = matchedAccount.ProjectID
		}
		if matchedAccount.ProviderSpecificData != nil {
			if projID, ok := matchedAccount.ProviderSpecificData["projectId"].(string); ok && projID != "" {
				creds.projectID = projID
			}
		}
	} else {
		// Fallback to API key
		creds.accessToken = apiKey
	}

	// Fallback to provider-level project ID
	if creds.projectID == "" && a.providerSpecificData != nil {
		if projID, ok := a.providerSpecificData["projectId"].(string); ok && projID != "" {
			creds.projectID = projID
		}
	}

	// Legacy header fallback
	if creds.projectID == "" && a.headers != nil {
		if pid, ok := a.headers["x-project-id"]; ok {
			creds.projectID = pid
		}
	}

	// Check if token needs refresh
	if creds.refreshToken != "" && ShouldRefreshGoogleToken(creds.expiresAt) {
		tokenResp, err := RefreshGoogleToken(creds.refreshToken, a.oauthConfig, a.httpClient)
		if err != nil {
			return nil, fmt.Errorf("failed to refresh Google token: %w", err)
		}
		creds.accessToken = tokenResp.AccessToken
		creds.refreshToken = tokenResp.RefreshToken
		// Update expiry
		expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
		creds.expiresAt = &expiresAt
	}

	return creds, nil
}

func (a *GeminiCLIAdapter) applyHeaders(req *http.Request, creds *geminiCLICredentials, model string, stream bool) {
	req.Header.Set("Content-Type", "application/json")
	if creds.accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+creds.accessToken)
	}
	req.Header.Set("User-Agent", geminiCLIUserAgent(model))
	req.Header.Set("X-Goog-Api-Client", geminiCLIAPIClient)
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	for k, v := range a.headers {
		req.Header.Set(k, v)
	}
}

func (a *GeminiCLIAdapter) ChatCompletion(ctx context.Context, request ChatRequest, model string) (ChatResponse, error) {
	creds, err := a.resolveGeminiCLICredentials("")
	if err != nil {
		return ChatResponse{}, NewNonRetryableError(a.name, model, "failed to resolve credentials", err)
	}

	body, err := a.buildRequestBody(ctx, request, model, creds)
	if err != nil {
		return ChatResponse{}, NewNonRetryableError(a.name, model, "failed to build request", err)
	}

	url := a.buildURL(model, false)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return ChatResponse{}, NewNonRetryableError(a.name, model, "failed to create request", err)
	}
	a.applyHeaders(req, creds, model, false)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return ChatResponse{}, NewRetryableError(a.name, model, "network error", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	
	// Handle 401 with refresh retry
	if resp.StatusCode == http.StatusUnauthorized && creds.refreshToken != "" {
		// Try to refresh and retry once
		tokenResp, refreshErr := RefreshGoogleToken(creds.refreshToken, a.oauthConfig, a.httpClient)
		if refreshErr == nil {
			creds.accessToken = tokenResp.AccessToken
			creds.refreshToken = tokenResp.RefreshToken
			
			// Retry request with new token
			req2, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
			if err != nil {
				return ChatResponse{}, NewNonRetryableError(a.name, model, "failed to create retry request", err)
			}
			a.applyHeaders(req2, creds, model, false)
			
			resp2, err := a.httpClient.Do(req2)
			if err != nil {
				return ChatResponse{}, NewRetryableError(a.name, model, "network error on retry", err)
			}
			defer resp2.Body.Close()
			respBody2, _ := io.ReadAll(resp2.Body)
			if resp2.StatusCode != http.StatusOK {
				return ChatResponse{}, ClassifyHTTPError(resp2.StatusCode, a.name, model, string(respBody2), a.errorConfig)
			}
			return a.translateResponse(ctx, respBody2, model)
		}
	}
	
	if resp.StatusCode != http.StatusOK {
		return ChatResponse{}, ClassifyHTTPError(resp.StatusCode, a.name, model, string(respBody), a.errorConfig)
	}

	return a.translateResponse(ctx, respBody, model)
}

func (a *GeminiCLIAdapter) StreamChatCompletion(ctx context.Context, request ChatRequest, model string) (<-chan ChatChunk, error) {
	creds, err := a.resolveGeminiCLICredentials("")
	if err != nil {
		return nil, NewNonRetryableError(a.name, model, "failed to resolve credentials", err)
	}

	body, err := a.buildRequestBody(ctx, request, model, creds)
	if err != nil {
		return nil, NewNonRetryableError(a.name, model, "failed to build request", err)
	}

	url := a.buildURL(model, true)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, NewNonRetryableError(a.name, model, "failed to create request", err)
	}
	a.applyHeaders(req, creds, model, true)

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
			openAIBody, err := a.translator.TranslateResponseJSON(ctx, translator.FormatGemini, translator.FormatOpenAI, []byte(data))
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

func (a *GeminiCLIAdapter) buildRequestBody(ctx context.Context, request ChatRequest, model string, creds *geminiCLICredentials) ([]byte, error) {
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

	geminiReq, err := a.translator.TranslateRequestJSON(ctx, translator.FormatOpenAI, translator.FormatGemini, mustMarshal(requestBody))
	if err != nil {
		return nil, fmt.Errorf("translate request: %w", err)
	}

	var geminiBody map[string]interface{}
	_ = json.Unmarshal(geminiReq, &geminiBody)
	if geminiBody == nil {
		geminiBody = map[string]interface{}{}
	}

	// Inject projectId from credentials if available and not already present
	if creds.projectID != "" {
		if _, exists := geminiBody["project"]; !exists {
			geminiBody["project"] = creds.projectID
		}
	}

	return json.Marshal(geminiBody)
}

func (a *GeminiCLIAdapter) translateResponse(ctx context.Context, respBody []byte, model string) (ChatResponse, error) {
	openAIBody, err := a.translator.TranslateResponseJSON(ctx, translator.FormatGemini, translator.FormatOpenAI, respBody)
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

func (a *GeminiCLIAdapter) GetUsage(context.Context) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}
func (a *GeminiCLIAdapter) Embeddings(context.Context, EmbeddingsRequest, string) (EmbeddingsResponse, error) {
	return EmbeddingsResponse{}, &ProviderError{Provider: a.name, Type: ErrNonRetryable, Message: "embeddings not supported"}
}
func (a *GeminiCLIAdapter) AudioSpeech(context.Context, AudioSpeechRequest, string) (AudioSpeechResponse, error) {
	return AudioSpeechResponse{}, &ProviderError{Provider: a.name, Type: ErrNonRetryable, Message: "audio speech not supported"}
}
func (a *GeminiCLIAdapter) ImagesGenerations(context.Context, ImagesGenerationsRequest, string) (ImagesGenerationsResponse, error) {
	return ImagesGenerationsResponse{}, &ProviderError{Provider: a.name, Type: ErrNonRetryable, Message: "images not supported"}
}
