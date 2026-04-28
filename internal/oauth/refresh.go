// Package oauth provides provider-specific OAuth token refresh logic.
package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// RefreshProviderToken refreshes a provider's token using provider-specific logic.
func RefreshProviderToken(ctx context.Context, cfg ProviderOAuthConfig, rec *TokenRecord, providerSpecificData map[string]any) (*TokenRecord, error) {
	switch cfg.Name {
	case "kiro":
		return refreshKiroToken(ctx, cfg, rec, providerSpecificData)
	case "github", "copilot":
		return refreshGitHubToken(ctx, cfg, rec)
	case "codebuddy":
		return refreshCodeBuddyToken(ctx, cfg, rec)
	case "qwen":
		// Qwen uses standard refresh but is deprecated
		refreshed, err := RefreshToken(ctx, cfg, rec)
		if err != nil {
			return nil, err
		}
		refreshed.ProviderSpecificData = map[string]any{"deprecated": true}
		return refreshed, nil
	case "cursor", "kilocode":
		// These providers don't support refresh (import-only)
		return nil, fmt.Errorf("provider %s does not support token refresh (import-only)", cfg.Name)
	default:
		// Standard OAuth2 refresh (claude, codex, gemini-cli, antigravity, iflow, qoder, gitlab, cline)
		return RefreshToken(ctx, cfg, rec)
	}
}

// refreshKiroToken handles Kiro's dual refresh paths (AWS SSO OIDC or Cognito social).
func refreshKiroToken(ctx context.Context, cfg ProviderOAuthConfig, rec *TokenRecord, providerSpecificData map[string]any) (*TokenRecord, error) {
	// Determine refresh path based on authMethod
	authMethod := ""
	if providerSpecificData != nil {
		if am, ok := providerSpecificData["authMethod"].(string); ok {
			authMethod = am
		}
	}

	// AWS SSO OIDC refresh path
	if authMethod == "builder-id" || authMethod == "idc" {
		return refreshKiroSSOToken(ctx, cfg, rec, providerSpecificData)
	}

	// Cognito social refresh path (Google/GitHub)
	return refreshKiroSocialToken(ctx, cfg, rec)
}

// refreshKiroSSOToken refreshes Kiro AWS SSO OIDC token.
func refreshKiroSSOToken(ctx context.Context, cfg ProviderOAuthConfig, rec *TokenRecord, providerSpecificData map[string]any) (*TokenRecord, error) {
	region := "us-east-1"
	if providerSpecificData != nil {
		if r, ok := providerSpecificData["region"].(string); ok {
			region = r
		}
	}

	clientId := ""
	if providerSpecificData != nil {
		if cid, ok := providerSpecificData["clientId"].(string); ok {
			clientId = cid
		}
	}
	clientSecret := ""
	if providerSpecificData != nil {
		if csec, ok := providerSpecificData["clientSecret"].(string); ok {
			clientSecret = csec
		}
	}

	if clientId == "" || clientSecret == "" {
		return nil, fmt.Errorf("kiro SSO refresh requires clientId and clientSecret in providerSpecificData")
	}

	tokenURL := fmt.Sprintf("https://oidc.%s.amazonaws.com/token", region)
	payload := map[string]any{
		"clientId":     clientId,
		"clientSecret": clientSecret,
		"refreshToken": rec.RefreshToken,
		"grantType":    "refresh_token",
	}

	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("kiro SSO refresh: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(string(jsonBody)))
	if err != nil {
		return nil, fmt.Errorf("kiro SSO refresh: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("kiro SSO refresh: request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("kiro SSO refresh: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("kiro SSO refresh failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var tokenData map[string]any
	if err := json.Unmarshal(respBody, &tokenData); err != nil {
		return nil, fmt.Errorf("kiro SSO refresh: parse response: %w", err)
	}

	// Normalize camelCase to snake_case
	accessToken := ""
	if at, ok := tokenData["accessToken"].(string); ok {
		accessToken = at
	}
	if accessToken == "" {
		accessToken = stringValue(tokenData["access_token"])
	}

	refreshToken := rec.RefreshToken
	if rt, ok := tokenData["refreshToken"].(string); ok && rt != "" {
		refreshToken = rt
	}

	expiresIn := 3600
	if ei, ok := tokenData["expiresIn"].(float64); ok {
		expiresIn = int(ei)
	}

	refreshed := &TokenRecord{
		Provider:     rec.Provider,
		Account:      rec.Account,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(expiresIn) * time.Second),
		Scope:        rec.Scope,
		TokenType:    rec.TokenType,
	}

	// Preserve provider-specific data for next refresh
	refreshed.ProviderSpecificData = make(map[string]any)
	for k, v := range providerSpecificData {
		refreshed.ProviderSpecificData[k] = v
	}

	return refreshed, nil
}

// refreshKiroSocialToken refreshes Kiro Cognito social token (Google/GitHub).
func refreshKiroSocialToken(ctx context.Context, cfg ProviderOAuthConfig, rec *TokenRecord) (*TokenRecord, error) {
	socialRefreshURL := cfg.Extra["socialRefreshUrl"]
	payload := map[string]any{
		"refreshToken": rec.RefreshToken,
	}

	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("kiro social refresh: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", socialRefreshURL, strings.NewReader(string(jsonBody)))
	if err != nil {
		return nil, fmt.Errorf("kiro social refresh: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("kiro social refresh: request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("kiro social refresh: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("kiro social refresh failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var tokenData map[string]any
	if err := json.Unmarshal(respBody, &tokenData); err != nil {
		return nil, fmt.Errorf("kiro social refresh: parse response: %w", err)
	}

	accessToken := stringValue(tokenData["accessToken"])
	refreshToken := rec.RefreshToken
	if rt, ok := tokenData["refreshToken"].(string); ok && rt != "" {
		refreshToken = rt
	}

	expiresIn := 3600
	if ei, ok := tokenData["expiresIn"].(float64); ok {
		expiresIn = int(ei)
	}

	profileArn := ""
	if pa, ok := tokenData["profileArn"].(string); ok {
		profileArn = pa
	}

	refreshed := &TokenRecord{
		Provider:     rec.Provider,
		Account:      rec.Account,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(expiresIn) * time.Second),
		Scope:        rec.Scope,
		TokenType:    rec.TokenType,
	}

	if profileArn != "" {
		refreshed.ProviderSpecificData = map[string]any{"profileArn": profileArn}
	}

	return refreshed, nil
}

// refreshGitHubToken refreshes GitHub token and re-fetches Copilot token.
func refreshGitHubToken(ctx context.Context, cfg ProviderOAuthConfig, rec *TokenRecord) (*TokenRecord, error) {
	// First refresh GitHub token
	refreshed, err := RefreshToken(ctx, cfg, rec)
	if err != nil {
		return nil, fmt.Errorf("github token refresh: %w", err)
	}

	// Then re-fetch Copilot token
	if copilotToken, expiresAt, err := fetchGitHubCopilotToken(ctx, cfg, refreshed.AccessToken); err == nil {
		if refreshed.ProviderSpecificData == nil {
			refreshed.ProviderSpecificData = make(map[string]any)
		}
		refreshed.ProviderSpecificData["copilotToken"] = copilotToken
		refreshed.ProviderSpecificData["copilotTokenExpiresAt"] = expiresAt
	}

	return refreshed, nil
}

// refreshCodeBuddyToken refreshes CodeBuddy (Tencent) token.
func refreshCodeBuddyToken(ctx context.Context, cfg ProviderOAuthConfig, rec *TokenRecord) (*TokenRecord, error) {
	refreshURL := cfg.Extra["refreshUrl"]
	payload := map[string]any{
		"refreshToken": rec.RefreshToken,
	}

	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("codebuddy refresh: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", refreshURL, strings.NewReader(string(jsonBody)))
	if err != nil {
		return nil, fmt.Errorf("codebuddy refresh: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", cfg.Extra["userAgent"])
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("X-Domain", "copilot.tencent.com")
	req.Header.Set("X-No-Authorization", "true")
	req.Header.Set("X-No-User-Id", "true")
	req.Header.Set("X-Product", "SaaS")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("codebuddy refresh: request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("codebuddy refresh: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("codebuddy refresh failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("codebuddy refresh: parse response: %w", err)
	}

	// CodeBuddy uses code field
	if code, ok := result["code"].(float64); ok && code != 0 {
		return nil, fmt.Errorf("codebuddy refresh failed with code %d: %v", int(code), result["message"])
	}

	accessToken := ""
	if data, ok := result["data"].(map[string]any); ok {
		accessToken = stringValue(data["accessToken"])
	}

	if accessToken == "" {
		return nil, fmt.Errorf("codebuddy refresh: no access token in response")
	}

	refreshToken := rec.RefreshToken
	if data, ok := result["data"].(map[string]any); ok {
		if rt, ok := data["refreshToken"].(string); ok && rt != "" {
			refreshToken = rt
		}
	}

	expiresIn := 3600
	if data, ok := result["data"].(map[string]any); ok {
		if ei, ok := data["expiresIn"].(float64); ok {
			expiresIn = int(ei)
		}
	}

	refreshed := &TokenRecord{
		Provider:     rec.Provider,
		Account:      rec.Account,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(expiresIn) * time.Second),
		Scope:        rec.Scope,
		TokenType:    rec.TokenType,
	}

	return refreshed, nil
}

// RefreshWithProviderDispatch is a convenience function that uses the registry to dispatch refresh.
func RefreshWithProviderDispatch(ctx context.Context, registry *ProviderRegistry, rec *TokenRecord, providerSpecificData map[string]any) (*TokenRecord, error) {
	cfg, ok := registry.Get(rec.Provider)
	if !ok {
		return nil, fmt.Errorf("unknown provider: %s", rec.Provider)
	}
	return RefreshProviderToken(ctx, cfg, rec, providerSpecificData)
}
