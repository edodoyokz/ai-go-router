package executors

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// RefreshCopilotToken refreshes the Copilot token using GitHub access token.
func (e *GitHubExecutor) RefreshCopilotToken(ctx context.Context, githubAccessToken string) (string, time.Time, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", githubTokenURL, nil)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "token "+githubAccessToken)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Editor-Version", "vscode/"+vscodeVersion)
	req.Header.Set("Editor-Plugin-Version", "copilot-chat/"+copilotChatVersion)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-github-api-version", githubAPIVersion)

	resp, err := e.client.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", time.Time{}, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expires_at"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", time.Time{}, fmt.Errorf("unmarshal response: %w", err)
	}

	expiresAt := time.Unix(result.ExpiresAt, 0)
	return result.Token, expiresAt, nil
}

// RefreshGitHubToken refreshes the GitHub access token using refresh token.
func (e *GitHubExecutor) RefreshGitHubToken(ctx context.Context, refreshToken, clientID, clientSecret string) (string, string, int, error) {
	params := url.Values{}
	params.Set("grant_type", "refresh_token")
	params.Set("refresh_token", refreshToken)
	params.Set("client_id", clientID)
	if clientSecret != "" {
		params.Set("client_secret", clientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", githubOAuthTokenURL, bytes.NewBufferString(params.Encode()))
	if err != nil {
		return "", "", 0, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return "", "", 0, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", 0, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", "", 0, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", "", 0, fmt.Errorf("unmarshal response: %w", err)
	}

	// Use existing refresh token if new one not provided
	newRefreshToken := result.RefreshToken
	if newRefreshToken == "" {
		newRefreshToken = refreshToken
	}

	return result.AccessToken, newRefreshToken, result.ExpiresIn, nil
}

// RefreshCredentials attempts to refresh both Copilot and GitHub tokens.
func (e *GitHubExecutor) RefreshCredentials(ctx context.Context) (map[string]interface{}, error) {
	if len(e.cfg.Accounts) == 0 {
		return nil, fmt.Errorf("no accounts configured")
	}

	account := e.cfg.Accounts[0]
	accessToken := account.AccessToken
	refreshToken := account.RefreshToken
	
	clientID := ""
	clientSecret := ""
	if e.cfg.ProviderSpecificData != nil {
		if id, ok := e.cfg.ProviderSpecificData["clientId"].(string); ok {
			clientID = id
		}
		if secret, ok := e.cfg.ProviderSpecificData["clientSecret"].(string); ok {
			clientSecret = secret
		}
	}

	// Try refreshing Copilot token first
	if accessToken != "" {
		copilotToken, expiresAt, err := e.RefreshCopilotToken(ctx, accessToken)
		if err == nil {
			return map[string]interface{}{
				"accessToken":           accessToken,
				"refreshToken":          refreshToken,
				"copilotToken":          copilotToken,
				"copilotTokenExpiresAt": expiresAt.Format(time.RFC3339),
			}, nil
		}
	}

	// If Copilot refresh failed and we have refresh token, try refreshing GitHub token
	if refreshToken != "" && clientID != "" {
		newAccessToken, newRefreshToken, expiresIn, err := e.RefreshGitHubToken(ctx, refreshToken, clientID, clientSecret)
		if err != nil {
			return nil, fmt.Errorf("refresh GitHub token: %w", err)
		}

		// Try refreshing Copilot token with new access token
		copilotToken, copilotExpiresAt, err := e.RefreshCopilotToken(ctx, newAccessToken)
		if err != nil {
			// Return GitHub tokens even if Copilot refresh fails
			return map[string]interface{}{
				"accessToken":  newAccessToken,
				"refreshToken": newRefreshToken,
				"expiresIn":    expiresIn,
			}, nil
		}

		return map[string]interface{}{
			"accessToken":           newAccessToken,
			"refreshToken":          newRefreshToken,
			"expiresIn":             expiresIn,
			"copilotToken":          copilotToken,
			"copilotTokenExpiresAt": copilotExpiresAt.Format(time.RFC3339),
		}, nil
	}

	return nil, fmt.Errorf("no valid credentials for refresh")
}

// NeedsRefresh checks if credentials need refreshing.
func (e *GitHubExecutor) NeedsRefresh() bool {
	if len(e.cfg.Accounts) == 0 {
		return true
	}

	account := e.cfg.Accounts[0]
	
	// Check if copilotToken exists
	copilotToken := ""
	if account.ProviderSpecificData != nil {
		if token, ok := account.ProviderSpecificData["copilotToken"].(string); ok {
			copilotToken = token
		}
	}
	
	// Always refresh if no copilotToken
	if copilotToken == "" {
		return true
	}

	// Check copilotToken expiry
	if account.ProviderSpecificData != nil {
		if expiresAtStr, ok := account.ProviderSpecificData["copilotTokenExpiresAt"].(string); ok && expiresAtStr != "" {
			expiresAt, err := time.Parse(time.RFC3339, expiresAtStr)
			if err == nil {
				// Refresh if expires in less than 5 minutes
				if time.Until(expiresAt) < 5*time.Minute {
					return true
				}
			}
		}
	}

	return false
}

// GetUsageQuota fetches usage quota from GitHub Copilot.
func (e *GitHubExecutor) GetUsageQuota(ctx context.Context) (map[string]interface{}, error) {
	token := e.getToken()
	if token == "" {
		return nil, fmt.Errorf("no token available")
	}

	url := "https://api.githubcopilot.com/copilot_internal/user"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Editor-Version", "vscode/"+vscodeVersion)
	req.Header.Set("Editor-Plugin-Version", "copilot-chat/"+copilotChatVersion)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-github-api-version", githubAPIVersion)

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return result, nil
}
