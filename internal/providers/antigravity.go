package providers

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/edodoyokz/ai-go-router/internal/config"
)

const (
	agToolSuffix   = "_ide"
	agUserAgent    = "antigravity/1.107.0"
	agInternalPath = "/v1internal"
)

// agFallbackBaseURLs mirrors the reference two-URL fallback list.
var agFallbackBaseURLs = []string{
	"https://daily-cloudcode-pa.googleapis.com",
	"https://daily-cloudcode-pa.sandbox.googleapis.com",
}

// agDefaultTools is the set of AG native tool names that must NOT be suffixed.
var agDefaultTools = map[string]bool{
	"browser_subagent": true, "command_status": true, "find_by_name": true,
	"generate_image": true, "grep_search": true, "list_dir": true,
	"list_resources": true, "mcp_sequential-thinking_sequentialthinking": true,
	"multi_replace_file_content": true, "notify_user": true, "read_resource": true,
	"read_terminal": true, "read_url_content": true, "replace_file_content": true,
	"run_command": true, "search_web": true, "send_command_input": true,
	"task_boundary": true, "view_content_chunk": true, "view_file": true,
	"write_to_file": true,
}

// agDecoyTools are injected after client tools to satisfy AG's expected tool list.
var agDecoyTools = func() []map[string]interface{} {
	names := []string{
		"browser_subagent", "command_status", "find_by_name", "generate_image",
		"grep_search", "list_dir", "list_resources",
		"mcp_sequential-thinking_sequentialthinking", "multi_replace_file_content",
		"notify_user", "read_resource", "read_terminal", "read_url_content",
		"replace_file_content", "run_command", "search_web", "send_command_input",
		"task_boundary", "view_content_chunk", "view_file", "write_to_file",
	}
	decoys := make([]map[string]interface{}, 0, len(names))
	for _, n := range names {
		decoys = append(decoys, map[string]interface{}{
			"name":        n,
			"description": "This tool is currently unavailable.",
			"parameters":  map[string]interface{}{"type": "OBJECT", "properties": map[string]interface{}{}, "required": []interface{}{}},
		})
	}
	return decoys
}()

// AntigravityAdapter implements the Adapter interface for Antigravity.
// It wraps OpenAI-format requests into the Antigravity native request envelope.
type AntigravityAdapter struct {
	name                 string
	baseURL              string
	headers              map[string]string
	errorConfig          config.ErrorConfig
	httpClient           *http.Client
	accountSelector      *AccountSelector
	accounts             []config.AccountConfig
	providerSpecificData map[string]any
	oauthConfig          GoogleOAuthConfig
}

func NewAntigravityAdapter(cfg config.ProviderConfig, errorConfig config.ErrorConfig, proxyURL string) *AntigravityAdapter {
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
	
	return &AntigravityAdapter{
		name:                 cfg.Name,
		baseURL:              cfg.BaseURL,
		headers:              cfg.Headers,
		errorConfig:          errorConfig,
		httpClient:           createHTTPClient(proxyURL),
		accountSelector:      NewAccountSelector(accounts, cfg.APIKey),
		accounts:             cfg.Accounts,
		providerSpecificData: cfg.ProviderSpecificData,
		oauthConfig:          oauthConfig,
	}
}

func (a *AntigravityAdapter) Name() string           { return a.name }
func (a *AntigravityAdapter) AccountNames() []string { return a.accountSelector.AccountNames() }

// antigravityCredentials holds resolved credentials for an Antigravity request.
type antigravityCredentials struct {
	accessToken  string
	refreshToken string
	expiresAt    *time.Time
	projectID    string
	sessionID    string
	email        string
}

// resolveAntigravityCredentials resolves credentials from account config.
func (a *AntigravityAdapter) resolveAntigravityCredentials(accountName string) (*antigravityCredentials, error) {
	_, apiKey := a.accountSelector.GetAccount(accountName)
	
	creds := &antigravityCredentials{
		sessionID: agSessionID(), // Default random session
	}

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
			if email, ok := matchedAccount.ProviderSpecificData["email"].(string); ok && email != "" {
				creds.email = email
			}
			if sessionID, ok := matchedAccount.ProviderSpecificData["sessionId"].(string); ok && sessionID != "" {
				creds.sessionID = sessionID
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

	// Generate project ID if not available
	if creds.projectID == "" {
		creds.projectID = randomAntigravityProjectID()
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

// buildURLs returns the list of base URLs to try (primary + fallback).
func (a *AntigravityAdapter) buildURLs(stream bool) []string {
	action := "generateContent"
	if stream {
		action = "streamGenerateContent?alt=sse"
	}
	bases := agFallbackBaseURLs
	if a.baseURL != "" {
		bases = []string{a.baseURL}
	}
	urls := make([]string, len(bases))
	for i, b := range bases {
		urls[i] = strings.TrimRight(b, "/") + agInternalPath + ":" + action
	}
	return urls
}

func randomAntigravityProjectID() string {
	adj := []string{"useful", "bright", "swift", "calm", "bold"}
	noun := []string{"fuze", "wave", "spark", "flow", "core"}
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s-%s-%s", adj[time.Now().UnixNano()%int64(len(adj))], noun[time.Now().UnixNano()%int64(len(noun))], hex.EncodeToString(b[:3]))
}

func agSessionID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func agUserAgentHeader() string {
	return fmt.Sprintf("%s %s/%s", agUserAgent, runtime.GOOS, runtime.GOARCH)
}

// cloakTools renames client tool names with _ide suffix and appends AG decoy tools.
// Returns (cloakedContents, cloakedTools, toolNameMap).
func cloakTools(contents []map[string]interface{}, tools []map[string]interface{}) (
	[]map[string]interface{}, []map[string]interface{}, map[string]string,
) {
	if len(tools) == 0 {
		return contents, tools, nil
	}

	toolNameMap := make(map[string]string)
	var clientDeclarations []map[string]interface{}

	for _, group := range tools {
		fds, _ := group["functionDeclarations"].([]interface{})
		for _, fdRaw := range fds {
			fd, _ := fdRaw.(map[string]interface{})
			if fd == nil {
				continue
			}
			name, _ := fd["name"].(string)
			if agDefaultTools[name] {
				clientDeclarations = append(clientDeclarations, fd)
				continue
			}
			suffixed := name + agToolSuffix
			toolNameMap[suffixed] = name
			cloned := make(map[string]interface{}, len(fd))
			for k, v := range fd {
				cloned[k] = v
			}
			cloned["name"] = suffixed
			clientDeclarations = append(clientDeclarations, cloned)
		}
	}

	allDeclarations := append(clientDeclarations, agDecoyTools...)
	cloakedTools := []map[string]interface{}{{"functionDeclarations": allDeclarations}}

	// Rename tool references in contents
	cloakedContents := make([]map[string]interface{}, len(contents))
	for i, msg := range contents {
		parts, _ := msg["parts"].([]map[string]interface{})
		if parts == nil {
			cloakedContents[i] = msg
			continue
		}
		cloakedParts := make([]map[string]interface{}, len(parts))
		for j, part := range parts {
			cp := make(map[string]interface{}, len(part))
			for k, v := range part {
				cp[k] = v
			}
			if fc, ok := part["functionCall"].(map[string]interface{}); ok {
				fname, _ := fc["name"].(string)
				if !agDefaultTools[fname] {
					newFC := make(map[string]interface{}, len(fc))
					for k, v := range fc {
						newFC[k] = v
					}
					newFC["name"] = fname + agToolSuffix
					cp["functionCall"] = newFC
				}
			}
			if fr, ok := part["functionResponse"].(map[string]interface{}); ok {
				fname, _ := fr["name"].(string)
				if !agDefaultTools[fname] {
					newFR := make(map[string]interface{}, len(fr))
					for k, v := range fr {
						newFR[k] = v
					}
					newFR["name"] = fname + agToolSuffix
					cp["functionResponse"] = newFR
				}
			}
			cloakedParts[j] = cp
		}
		clonedMsg := make(map[string]interface{}, len(msg))
		for k, v := range msg {
			clonedMsg[k] = v
		}
		clonedMsg["parts"] = cloakedParts
		cloakedContents[i] = clonedMsg
	}

	return cloakedContents, cloakedTools, toolNameMap
}

// stripThoughtParts removes thought-only parts from contents per reference spec.
func stripThoughtParts(contents []map[string]interface{}) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(contents))
	for _, msg := range contents {
		parts, _ := msg["parts"].([]map[string]interface{})
		if parts == nil {
			result = append(result, msg)
			continue
		}
		filtered := make([]map[string]interface{}, 0, len(parts))
		for _, part := range parts {
			_, hasThought := part["thought"]
			_, hasFuncCall := part["functionCall"]
			_, hasThoughtSig := part["thoughtSignature"]
			_, hasText := part["text"]
			// Skip thought-only parts (no functionCall)
			if hasThought && !hasFuncCall {
				continue
			}
			// Skip thoughtSignature-only parts (no functionCall, no text)
			if hasThoughtSig && !hasFuncCall && !hasText {
				continue
			}
			filtered = append(filtered, part)
		}
		cloned := make(map[string]interface{}, len(msg))
		for k, v := range msg {
			cloned[k] = v
		}
		cloned["parts"] = filtered
		result = append(result, cloned)
	}
	return result
}

func (a *AntigravityAdapter) buildRequestBody(request ChatRequest, model string, creds *antigravityCredentials) ([]byte, error) {
	var contents []map[string]interface{}
	var systemInstruction interface{}

	for _, msg := range request.Messages {
		role := msg.Role
		if role == "assistant" {
			role = "model"
		}

		if role == "system" {
			systemInstruction = map[string]interface{}{
				"parts": []map[string]interface{}{
					{"text": fmt.Sprintf("%v", msg.Content)},
				},
			}
			continue
		}

		parts := []map[string]interface{}{
			{"text": fmt.Sprintf("%v", msg.Content)},
		}

		if len(msg.ToolCalls) > 0 {
			for _, tc := range msg.ToolCalls {
				parts = append(parts, map[string]interface{}{
					"functionCall": map[string]interface{}{
						"name": tc.Function.Name,
						"args": json.RawMessage(tc.Function.Arguments),
						"id":   tc.ID,
					},
				})
			}
		}

		if msg.Role == "tool" && msg.ToolCallID != "" {
			parts = []map[string]interface{}{
				{
					"functionResponse": map[string]interface{}{
						"name":     msg.Name,
						"id":       msg.ToolCallID,
						"response": map[string]interface{}{"result": fmt.Sprintf("%v", msg.Content)},
					},
				},
			}
			role = "user"
		}

		contents = append(contents, map[string]interface{}{
			"role":  role,
			"parts": parts,
		})
	}

	// Strip thought-only parts (reference spec)
	contents = stripThoughtParts(contents)

	genConfig := map[string]interface{}{}
	if request.Temperature != nil && *request.Temperature > 0 {
		genConfig["temperature"] = *request.Temperature
	}
	if request.TopP != nil && *request.TopP > 0 {
		genConfig["topP"] = *request.TopP
	}
	if request.MaxTokens != nil && *request.MaxTokens > 0 {
		genConfig["maxOutputTokens"] = *request.MaxTokens
	}

	innerReq := map[string]interface{}{
		"contents":         contents,
		"generationConfig": genConfig,
		"sessionId":        creds.sessionID,
	}
	if systemInstruction != nil {
		innerReq["systemInstruction"] = systemInstruction
	}

	// Convert tools to Antigravity format and apply cloaking
	if len(request.Tools) > 0 {
		var fds []interface{}
		for _, tool := range request.Tools {
			var fn map[string]interface{}
			_ = json.Unmarshal(tool.Function, &fn)
			fds = append(fds, fn)
		}
		rawTools := []map[string]interface{}{{"functionDeclarations": fds}}
		cloakedContents, cloakedTools, _ := cloakTools(contents, rawTools)
		innerReq["contents"] = cloakedContents
		innerReq["tools"] = cloakedTools
		innerReq["toolConfig"] = map[string]interface{}{
			"functionCallingConfig": map[string]interface{}{"mode": "VALIDATED"},
		}
	}

	body := map[string]interface{}{
		"project":     creds.projectID,
		"model":       model,
		"userAgent":   "antigravity",
		"requestType": "agent",
		"requestId":   fmt.Sprintf("agent-%s", creds.sessionID[:8]),
		"request":     innerReq,
	}
	return json.Marshal(body)
}

func (a *AntigravityAdapter) applyHeaders(req *http.Request, creds *antigravityCredentials, stream bool) {
	req.Header.Set("Content-Type", "application/json")
	if creds.accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+creds.accessToken)
	}
	req.Header.Set("User-Agent", agUserAgentHeader())
	req.Header.Set("x-request-source", "local")
	if creds.sessionID != "" {
		req.Header.Set("X-Machine-Session-Id", creds.sessionID)
	}
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	for k, v := range a.headers {
		req.Header.Set(k, v)
	}
}

func (a *AntigravityAdapter) ChatCompletion(ctx context.Context, request ChatRequest, model string) (ChatResponse, error) {
	creds, err := a.resolveAntigravityCredentials("")
	if err != nil {
		return ChatResponse{}, NewNonRetryableError(a.name, model, "failed to resolve credentials", err)
	}

	body, err := a.buildRequestBody(request, model, creds)
	if err != nil {
		return ChatResponse{}, NewNonRetryableError(a.name, model, "failed to build request", err)
	}

	var lastErr error
	for _, url := range a.buildURLs(false) {
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
		if err != nil {
			return ChatResponse{}, NewNonRetryableError(a.name, model, "failed to create request", err)
		}
		a.applyHeaders(req, creds, false)
		resp, err := a.httpClient.Do(req)
		if err != nil {
			lastErr = NewRetryableError(a.name, model, "network error", err)
			continue
		}
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		
		// Handle 401 with refresh retry
		if resp.StatusCode == http.StatusUnauthorized && creds.refreshToken != "" {
			tokenResp, refreshErr := RefreshGoogleToken(creds.refreshToken, a.oauthConfig, a.httpClient)
			if refreshErr == nil {
				creds.accessToken = tokenResp.AccessToken
				creds.refreshToken = tokenResp.RefreshToken
				
				// Retry request with new token
				req2, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
				if err != nil {
					return ChatResponse{}, NewNonRetryableError(a.name, model, "failed to create retry request", err)
				}
				a.applyHeaders(req2, creds, false)
				
				resp2, err := a.httpClient.Do(req2)
				if err != nil {
					lastErr = NewRetryableError(a.name, model, "network error on retry", err)
					continue
				}
				defer resp2.Body.Close()
				respBody2, _ := io.ReadAll(resp2.Body)
				if resp2.StatusCode != http.StatusOK {
					if resp2.StatusCode == http.StatusTooManyRequests || resp2.StatusCode == http.StatusServiceUnavailable {
						lastErr = ClassifyHTTPError(resp2.StatusCode, a.name, model, string(respBody2), a.errorConfig)
						continue
					}
					return ChatResponse{}, ClassifyHTTPError(resp2.StatusCode, a.name, model, string(respBody2), a.errorConfig)
				}
				var out ChatResponse
				if err := json.Unmarshal(respBody2, &out); err != nil {
					return ChatResponse{}, NewNonRetryableError(a.name, model, "invalid upstream response", err)
				}
				return out, nil
			}
		}
		
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
			lastErr = ClassifyHTTPError(resp.StatusCode, a.name, model, string(respBody), a.errorConfig)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return ChatResponse{}, ClassifyHTTPError(resp.StatusCode, a.name, model, string(respBody), a.errorConfig)
		}
		var out ChatResponse
		if err := json.Unmarshal(respBody, &out); err != nil {
			return ChatResponse{}, NewNonRetryableError(a.name, model, "invalid upstream response", err)
		}
		return out, nil
	}
	if lastErr != nil {
		return ChatResponse{}, lastErr
	}
	return ChatResponse{}, NewRetryableError(a.name, model, "all antigravity endpoints failed", nil)
}

func (a *AntigravityAdapter) StreamChatCompletion(ctx context.Context, request ChatRequest, model string) (<-chan ChatChunk, error) {
	creds, err := a.resolveAntigravityCredentials("")
	if err != nil {
		return nil, NewNonRetryableError(a.name, model, "failed to resolve credentials", err)
	}

	body, err := a.buildRequestBody(request, model, creds)
	if err != nil {
		return nil, NewNonRetryableError(a.name, model, "failed to build request", err)
	}

	var lastErr error
	for _, url := range a.buildURLs(true) {
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
		if err != nil {
			return nil, NewNonRetryableError(a.name, model, "failed to create request", err)
		}
		a.applyHeaders(req, creds, true)
		resp, err := a.httpClient.Do(req)
		if err != nil {
			lastErr = NewRetryableError(a.name, model, "network error", err)
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
			resp.Body.Close()
			lastErr = ClassifyHTTPError(resp.StatusCode, a.name, model, "", a.errorConfig)
			continue
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
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, NewRetryableError(a.name, model, "all antigravity endpoints failed", nil)
}

func (a *AntigravityAdapter) GetUsage(context.Context) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}
func (a *AntigravityAdapter) Embeddings(context.Context, EmbeddingsRequest, string) (EmbeddingsResponse, error) {
	return EmbeddingsResponse{}, &ProviderError{Provider: a.name, Type: ErrNonRetryable, Message: "embeddings not supported"}
}
func (a *AntigravityAdapter) AudioSpeech(context.Context, AudioSpeechRequest, string) (AudioSpeechResponse, error) {
	return AudioSpeechResponse{}, &ProviderError{Provider: a.name, Type: ErrNonRetryable, Message: "audio speech not supported"}
}
func (a *AntigravityAdapter) ImagesGenerations(context.Context, ImagesGenerationsRequest, string) (ImagesGenerationsResponse, error) {
	return ImagesGenerationsResponse{}, &ProviderError{Provider: a.name, Type: ErrNonRetryable, Message: "images not supported"}
}
