// Package oauth provides generic OAuth 2.0 flow handlers.
package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// TokenResult represents a normalized token response from an OAuth flow.
type TokenResult struct {
	AccessToken          string
	RefreshToken         string
	ExpiresIn            int
	Scope                string
	Email                string
	ProviderSpecificData map[string]any
}

// ExchangeToken exchanges an authorization code for tokens using the provider config.
func ExchangeToken(ctx context.Context, cfg ProviderOAuthConfig, code, redirectURI, codeVerifier, state string, meta map[string]string) (map[string]any, error) {
	var body io.Reader
	var contentType string

	// Determine request body format
	if cfg.ContentType == "application/json" {
		// JSON body (Claude, Cline)
		payload := map[string]any{
			"grant_type":   "authorization_code",
			"client_id":    cfg.ClientID,
			"code":         code,
			"redirect_uri": redirectURI,
		}
		if codeVerifier != "" {
			payload["code_verifier"] = codeVerifier
		}
		if state != "" {
			payload["state"] = state
		}

		// Claude-specific: code may contain state after #
		if cfg.Name == "claude" && strings.Contains(code, "#") {
			parts := strings.Split(code, "#")
			payload["code"] = parts[0]
			payload["state"] = parts[1]
		}

		// Cline-specific: code is base64-encoded JSON
		if cfg.Name == "cline" {
			// Try to decode base64 code
			if decoded, err := decodeClineToken(code); err == nil {
				payload["code"] = decoded.Code
				if decoded.RefreshToken != "" {
					payload["refresh_token"] = decoded.RefreshToken
				}
			}
		}

		// Extra auth params
		for k, v := range cfg.ExtraAuthParams {
			payload[k] = v
		}

		// Meta params (GitLab clientId, clientSecret)
		if meta != nil {
			if clientId := meta["clientId"]; clientId != "" {
				payload["client_id"] = clientId
			}
			if clientSecret := meta["clientSecret"]; clientSecret != "" {
				payload["client_secret"] = clientSecret
			}
		}

		jsonBody, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("oauth: marshal json body: %w", err)
		}
		body = strings.NewReader(string(jsonBody))
		contentType = "application/json"
	} else {
		// Form-encoded body (default)
		form := url.Values{}
		form.Set("grant_type", "authorization_code")
		form.Set("code", code)
		form.Set("redirect_uri", redirectURI)
		if cfg.ClientID != "" {
			form.Set("client_id", cfg.ClientID)
		}
		if cfg.ClientSecret != "" {
			form.Set("client_secret", cfg.ClientSecret)
		}
		if codeVerifier != "" {
			form.Set("code_verifier", codeVerifier)
		}
		if state != "" {
			form.Set("state", state)
		}

		// Meta params (GitLab clientId, clientSecret)
		if meta != nil {
			if clientId := meta["clientId"]; clientId != "" {
				form.Set("client_id", clientId)
			}
			if clientSecret := meta["clientSecret"]; clientSecret != "" {
				form.Set("client_secret", clientSecret)
			}
		}

		// Extra auth params
		for k, v := range cfg.ExtraAuthParams {
			form.Set(k, v)
		}

		body = strings.NewReader(form.Encode())
		contentType = "application/x-www-form-urlencoded"
	}

	// Handle GitLab dynamic URLs
	tokenURL := cfg.TokenURL
	if cfg.Name == "gitlab" {
		baseURL := ""
		if meta != nil {
			baseURL = meta["baseUrl"]
		}
		if baseURL == "" {
			baseURL = cfg.Extra["defaultBaseUrl"]
		}
		tokenURL = baseURL + cfg.Extra["tokenUrlPath"]
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, body)
	if err != nil {
		return nil, fmt.Errorf("oauth: create token request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")

	// iFlow and Qoder use Basic Auth
	if cfg.Name == "iflow" || cfg.Name == "qoder" {
		auth := base64.StdEncoding.EncodeToString([]byte(cfg.ClientID + ":" + cfg.ClientSecret))
		req.Header.Set("Authorization", "Basic "+auth)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oauth: token exchange request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("oauth: read token response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("oauth: token exchange failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var tokenResp map[string]any
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return nil, fmt.Errorf("oauth: parse token response: %w", err)
	}

	return tokenResp, nil
}

// clineToken represents decoded Cline token data.
type clineToken struct {
	Code         string
	AccessToken  string
	RefreshToken string
	Email        string
	FirstName    string
	LastName     string
	ExpiresAt    string
}

// decodeClineToken attempts to decode a base64-encoded Cline token.
func decodeClineToken(code string) (clineToken, error) {
	var result clineToken

	// Add padding if needed
	padding := 4 - (len(code) % 4)
	if padding != 4 {
		code += strings.Repeat("=", padding)
	}

	decoded, err := base64.StdEncoding.DecodeString(code)
	if err != nil {
		return result, err
	}

	decodedStr := string(decoded)
	lastBrace := strings.LastIndex(decodedStr, "}")
	if lastBrace == -1 {
		return result, fmt.Errorf("no JSON found in decoded code")
	}

	var tokenData map[string]any
	if err := json.Unmarshal([]byte(decodedStr[:lastBrace+1]), &tokenData); err != nil {
		return result, err
	}

	if accessToken, ok := tokenData["accessToken"].(string); ok {
		result.AccessToken = accessToken
	}
	if refreshToken, ok := tokenData["refreshToken"].(string); ok {
		result.RefreshToken = refreshToken
	}
	if email, ok := tokenData["email"].(string); ok {
		result.Email = email
	}
	if firstName, ok := tokenData["firstName"].(string); ok {
		result.FirstName = firstName
	}
	if lastName, ok := tokenData["lastName"].(string); ok {
		result.LastName = lastName
	}
	if expiresAt, ok := tokenData["expiresAt"].(string); ok {
		result.ExpiresAt = expiresAt
	}

	return result, nil
}

// RequestDeviceCode requests a device code for the provider.
func RequestDeviceCode(ctx context.Context, cfg ProviderOAuthConfig, codeChallenge string, options map[string]string) (map[string]any, error) {
	deviceCodeURL := cfg.DeviceCodeURL

	// Kiro: dynamic region-based URL
	if cfg.Name == "kiro" {
		_ = "us-east-1"                                // Default region, passed to requestKiroDeviceCode via options
		deviceCodeURL = cfg.Extra["registerClientUrl"] // First register client
	}

	var body io.Reader
	var contentType string

	if cfg.ContentType == "application/json" {
		// JSON body (Kiro, KiloCode, CodeBuddy)
		payload := map[string]any{}

		if cfg.Name == "kiro" {
			region := "us-east-1"
			startUrl := cfg.Extra["startUrl"]
			authMethod := "builder-id"
			if options != nil {
				if r := options["region"]; r != "" {
					region = r
				}
				if s := options["startUrl"]; s != "" {
					startUrl = s
				}
				if a := options["authMethod"]; a != "" {
					authMethod = a
				}
			}
			// Kiro 2-step: first register client
			return requestKiroDeviceCode(ctx, cfg, region, startUrl, authMethod)
		}

		if cfg.Name == "kilocode" {
			// KiloCode uses POST to initiate with no body
		}

		if cfg.Name == "codebuddy" {
			platform := cfg.Extra["platform"]
			if platform == "" {
				platform = "CLI"
			}
			// CodeBuddy expects empty JSON body
		}

		jsonBody, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("oauth: marshal json body: %w", err)
		}
		body = strings.NewReader(string(jsonBody))
		contentType = "application/json"
	} else {
		// Form-encoded body (default: GitHub, Qwen)
		form := url.Values{}
		form.Set("client_id", cfg.ClientID)

		if len(cfg.Scopes) > 0 {
			form.Set("scope", strings.Join(cfg.Scopes, " "))
		} else if cfg.Scope != "" {
			form.Set("scope", cfg.Scope)
		}

		if codeChallenge != "" && !cfg.NoPKCEForDeviceCode {
			form.Set("code_challenge", codeChallenge)
			form.Set("code_challenge_method", cfg.CodeChallengeMethod)
		}

		body = strings.NewReader(form.Encode())
		contentType = "application/x-www-form-urlencoded"
	}

	req, err := http.NewRequestWithContext(ctx, "POST", deviceCodeURL, body)
	if err != nil {
		return nil, fmt.Errorf("oauth: create device code request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")

	// CodeBuddy-specific headers
	if cfg.Name == "codebuddy" {
		req.Header.Set("User-Agent", cfg.Extra["userAgent"])
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		req.Header.Set("X-Domain", "copilot.tencent.com")
		req.Header.Set("X-No-Authorization", "true")
		req.Header.Set("X-No-User-Id", "true")
		req.Header.Set("X-Product", "SaaS")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oauth: device code request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("oauth: read device code response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("oauth: device code failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("oauth: parse device code response: %w", err)
	}

	// Normalize CodeBuddy response
	if cfg.Name == "codebuddy" {
		if data, ok := result["data"].(map[string]any); ok {
			result["device_code"] = data["state"]
			result["verification_uri"] = data["authUrl"]
			result["user_code"] = ""
			result["interval"] = cfg.Extra["pollInterval"]
			result["_isCodeBuddy"] = true
		}
	}

	return result, nil
}

// requestKiroDeviceCode handles the 2-step Kiro device code flow.
func requestKiroDeviceCode(ctx context.Context, cfg ProviderOAuthConfig, region, startUrl, authMethod string) (map[string]any, error) {
	// Step 1: Register client
	registerURL := fmt.Sprintf("https://oidc.%s.amazonaws.com/client/register", region)
	registerPayload := map[string]any{
		"clientName": cfg.Extra["clientName"],
		"clientType": cfg.Extra["clientType"],
		"scopes":     []string{"codewhisperer:completions", "codewhisperer:analysis", "codewhisperer:conversations"},
		"grantTypes": []string{"urn:ietf:params:oauth:grant-type:device_code", "refresh_token"},
		"issuerUrl":  cfg.Extra["issuerUrl"],
	}

	jsonBody, _ := json.Marshal(registerPayload)
	req, _ := http.NewRequestWithContext(ctx, "POST", registerURL, strings.NewReader(string(jsonBody)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oauth: kiro register client: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("oauth: kiro register client failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var clientInfo map[string]any
	if err := json.Unmarshal(respBody, &clientInfo); err != nil {
		return nil, fmt.Errorf("oauth: kiro register client: parse response: %w", err)
	}

	clientId := stringValue(clientInfo["clientId"])
	clientSecret := stringValue(clientInfo["clientSecret"])

	// Step 2: Request device authorization
	deviceAuthURL := fmt.Sprintf("https://oidc.%s.amazonaws.com/device_authorization", region)
	devicePayload := map[string]any{
		"clientId":     clientId,
		"clientSecret": clientSecret,
		"startUrl":     startUrl,
	}

	jsonBody, _ = json.Marshal(devicePayload)
	req, _ = http.NewRequestWithContext(ctx, "POST", deviceAuthURL, strings.NewReader(string(jsonBody)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err = client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oauth: kiro device auth: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ = io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("oauth: kiro device auth failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var deviceData map[string]any
	if err := json.Unmarshal(respBody, &deviceData); err != nil {
		return nil, fmt.Errorf("oauth: kiro device auth: parse response: %w", err)
	}

	// Return combined data with client credentials for polling
	result := map[string]any{
		"device_code":               deviceData["deviceCode"],
		"user_code":                 deviceData["userCode"],
		"verification_uri":          deviceData["verificationUri"],
		"verification_uri_complete": deviceData["verificationUriComplete"],
		"expires_in":                deviceData["expiresIn"],
		"interval":                  deviceData["interval"],
		"_clientId":                 clientId,
		"_clientSecret":             clientSecret,
		"_region":                   region,
		"_authMethod":               authMethod,
		"_startUrl":                 startUrl,
	}

	return result, nil
}

// PollDeviceToken polls for a token using device code.
func PollDeviceToken(ctx context.Context, cfg ProviderOAuthConfig, deviceCode, codeVerifier string, extraData map[string]any) (map[string]any, error) {
	tokenURL := cfg.TokenURL

	// Dynamic URLs based on provider
	if cfg.Name == "kiro" {
		region := "us-east-1"
		if extraData != nil {
			if r := extraData["_region"]; r != nil {
				region = stringValue(r)
			}
		}
		tokenURL = fmt.Sprintf("https://oidc.%s.amazonaws.com/token", region)
	}
	if cfg.Name == "kilocode" {
		pollURLBase := cfg.Extra["pollUrlBase"]
		tokenURL = pollURLBase + "/" + deviceCode
	}

	var body io.Reader
	var contentType string

	if cfg.ContentType == "application/json" {
		// JSON body (Kiro, KiloCode, CodeBuddy)
		payload := map[string]any{}

		if cfg.Name == "kiro" {
			payload["clientId"] = extraData["_clientId"]
			payload["clientSecret"] = extraData["_clientSecret"]
			payload["deviceCode"] = deviceCode
			payload["grantType"] = "urn:ietf:params:oauth:grant-type:device_code"
		}

		if cfg.Name == "kilocode" {
			// KiloCode poll is a GET, no body
			return pollKilocodeToken(ctx, cfg, deviceCode)
		}

		if cfg.Name == "codebuddy" {
			payload["state"] = deviceCode
		}

		jsonBody, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("oauth: marshal json body: %w", err)
		}
		body = strings.NewReader(string(jsonBody))
		contentType = "application/json"
	} else {
		// Form-encoded body (GitHub, Qwen)
		form := url.Values{}
		form.Set("client_id", cfg.ClientID)
		form.Set("device_code", deviceCode)
		form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")

		if codeVerifier != "" && !cfg.NoPKCEForDeviceCode {
			form.Set("code_verifier", codeVerifier)
		}

		body = strings.NewReader(form.Encode())
		contentType = "application/x-www-form-urlencoded"
	}

	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, body)
	if err != nil {
		return nil, fmt.Errorf("oauth: create poll request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")

	// CodeBuddy-specific headers
	if cfg.Name == "codebuddy" {
		req.Header.Set("User-Agent", cfg.Extra["userAgent"])
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		req.Header.Set("X-Domain", "copilot.tencent.com")
		req.Header.Set("X-No-Authorization", "true")
		req.Header.Set("X-No-User-Id", "true")
		req.Header.Set("X-Product", "SaaS")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oauth: poll request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("oauth: read poll response: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		// Try to parse as text error
		return map[string]any{"error": "invalid_response", "error_description": string(respBody)}, nil
	}

	// Normalize Kiro camelCase responses
	if cfg.Name == "kiro" {
		if accessToken, ok := result["accessToken"].(string); ok {
			result["access_token"] = accessToken
		}
		if refreshToken, ok := result["refreshToken"].(string); ok {
			result["refresh_token"] = refreshToken
		}
		if expiresIn, ok := result["expiresIn"]; ok {
			result["expires_in"] = expiresIn
		}
		// Preserve client credentials for refresh
		result["_clientId"] = extraData["_clientId"]
		result["_clientSecret"] = extraData["_clientSecret"]
		result["_region"] = extraData["_region"]
		result["_authMethod"] = extraData["_authMethod"]
		result["_startUrl"] = extraData["_startUrl"]
	}

	// Normalize CodeBuddy response
	if cfg.Name == "codebuddy" {
		if code, ok := result["code"].(float64); ok && code == 0 {
			if data, ok := result["data"].(map[string]any); ok {
				result["access_token"] = data["accessToken"]
				result["refresh_token"] = data["refreshToken"]
				result["token_type"] = data["tokenType"]
			}
		}
		if code, ok := result["code"].(float64); ok && code == 11217 {
			result["error"] = "authorization_pending"
		}
	}

	return result, nil
}

// pollKilocodeToken handles KiloCode's custom polling.
func pollKilocodeToken(ctx context.Context, cfg ProviderOAuthConfig, deviceCode string) (map[string]any, error) {
	pollURL := cfg.Extra["pollUrlBase"] + "/" + deviceCode

	req, err := http.NewRequestWithContext(ctx, "GET", pollURL, nil)
	if err != nil {
		return nil, fmt.Errorf("oauth: create kilocode poll request: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oauth: kilocode poll request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 202 {
		return map[string]any{"error": "authorization_pending"}, nil
	}
	if resp.StatusCode == 403 {
		return map[string]any{"error": "access_denied", "error_description": "Authorization denied by user"}, nil
	}
	if resp.StatusCode == 410 {
		return map[string]any{"error": "expired_token", "error_description": "Authorization code expired"}, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return map[string]any{"error": "poll_failed", "error_description": fmt.Sprintf("Poll failed: %d", resp.StatusCode)}, nil
	}

	respBody, _ := io.ReadAll(resp.Body)
	var data map[string]any
	json.Unmarshal(respBody, &data)

	if status, ok := data["status"].(string); ok && status == "approved" {
		if token, ok := data["token"].(string); ok {
			result := map[string]any{
				"access_token": token,
				"_userEmail":   data["userEmail"],
			}
			// Fetch profile for orgId
			if orgId, err := fetchKilocodeOrgId(ctx, cfg, token); err == nil && orgId != "" {
				result["_orgId"] = orgId
			}
			return result, nil
		}
	}

	return map[string]any{"error": "authorization_pending"}, nil
}

// fetchKilocodeOrgId fetches the org ID from KiloCode profile.
func fetchKilocodeOrgId(ctx context.Context, cfg ProviderOAuthConfig, token string) (string, error) {
	profileURL := cfg.Extra["apiBaseUrl"] + "/api/profile"
	req, _ := http.NewRequestWithContext(ctx, "GET", profileURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", nil
	}

	respBody, _ := io.ReadAll(resp.Body)
	var profile map[string]any
	json.Unmarshal(respBody, &profile)

	if organizations, ok := profile["organizations"].([]any); ok && len(organizations) > 0 {
		if org, ok := organizations[0].(map[string]any); ok {
			if id, ok := org["id"].(string); ok {
				return id, nil
			}
		}
	}

	return "", nil
}

// PostExchange performs post-exchange operations (fetch user info, project ID, etc.).
func PostExchange(ctx context.Context, cfg ProviderOAuthConfig, tokens map[string]any) (map[string]any, error) {
	extra := make(map[string]any)

	accessToken := stringValue(tokens["access_token"])
	if accessToken == "" {
		return extra, nil
	}

	switch cfg.Name {
	case "github", "copilot":
		// Fetch Copilot token
		if copilotToken, expiresAt, err := fetchGitHubCopilotToken(ctx, cfg, accessToken); err == nil {
			extra["copilotToken"] = copilotToken
			extra["copilotTokenExpiresAt"] = expiresAt
		}
		// Fetch GitHub user info
		if userInfo, err := fetchGitHubUserInfo(ctx, cfg, accessToken); err == nil {
			extra["githubUserId"] = userInfo["id"]
			extra["githubLogin"] = userInfo["login"]
			extra["githubName"] = userInfo["name"]
			extra["githubEmail"] = userInfo["email"]
		}

	case "gemini-cli", "antigravity":
		// Fetch user info
		if userInfo, err := fetchGoogleUserInfo(ctx, cfg, accessToken); err == nil {
			extra["email"] = userInfo["email"]
		}
		// Fetch project ID via loadCodeAssist
		if projectId, tierId, err := fetchGoogleProjectId(ctx, cfg, accessToken); err == nil {
			extra["projectId"] = projectId
			if tierId != "" {
				extra["tierId"] = tierId
			}
			// Antigravity: fire-and-forget onboard
			if cfg.Name == "antigravity" && projectId != "" {
				go fireAntigravityOnboard(ctx, cfg, accessToken, projectId, tierId)
			}
		}

	case "iflow":
		// Fetch user info (MUST succeed for API key)
		userInfo, err := fetchIFlowUserInfo(ctx, cfg, accessToken)
		if err != nil {
			return nil, err
		}
		apiKey, _ := userInfo["apiKey"].(string)
		if apiKey == "" {
			return nil, fmt.Errorf("empty API key returned from iFlow")
		}
		extra["apiKey"] = apiKey
		if email, ok := userInfo["email"].(string); ok {
			extra["email"] = email
		}
		if nickname, ok := userInfo["nickname"].(string); ok {
			extra["displayName"] = nickname
		}

	case "qoder":
		// Fetch user info (MUST succeed for API key)
		userInfo, err := fetchQoderUserInfo(ctx, cfg, accessToken)
		if err != nil {
			return nil, err
		}
		apiKey, _ := userInfo["apiKey"].(string)
		if apiKey == "" {
			return nil, fmt.Errorf("empty API key returned from Qoder")
		}
		extra["apiKey"] = apiKey
		if email, ok := userInfo["email"].(string); ok {
			extra["email"] = email
		}
		if nickname, ok := userInfo["nickname"].(string); ok {
			extra["displayName"] = nickname
		}

	case "gitlab":
		// Fetch user info
		if userInfo, err := fetchGitLabUserInfo(ctx, cfg, accessToken, ""); err == nil {
			extra["username"] = userInfo["username"]
			extra["email"] = userInfo["email"]
			extra["name"] = userInfo["name"]
		}
	}

	return extra, nil
}

// fetchGitHubCopilotToken fetches the Copilot token using GitHub access token.
func fetchGitHubCopilotToken(ctx context.Context, cfg ProviderOAuthConfig, accessToken string) (string, string, error) {
	copilotTokenURL := cfg.Extra["copilotTokenURL"]
	req, _ := http.NewRequestWithContext(ctx, "GET", copilotTokenURL, nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-GitHub-Api-Version", cfg.Extra["apiVersion"])
	req.Header.Set("User-Agent", cfg.Extra["userAgent"])

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", nil
	}

	respBody, _ := io.ReadAll(resp.Body)
	var payload map[string]any
	json.Unmarshal(respBody, &payload)

	token := stringValue(payload["token"])
	expiresAt := ""
	if e, ok := payload["expires_at"]; ok {
		expiresAt = fmt.Sprintf("%v", e)
	}

	return token, expiresAt, nil
}

// fetchGitHubUserInfo fetches GitHub user info.
func fetchGitHubUserInfo(ctx context.Context, cfg ProviderOAuthConfig, accessToken string) (map[string]any, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", cfg.UserInfoURL, nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-GitHub-Api-Version", cfg.Extra["apiVersion"])
	req.Header.Set("User-Agent", cfg.Extra["userAgent"])

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var userInfo map[string]any
	json.Unmarshal(respBody, &userInfo)
	return userInfo, nil
}

// fetchGoogleUserInfo fetches Google user info.
func fetchGoogleUserInfo(ctx context.Context, cfg ProviderOAuthConfig, accessToken string) (map[string]any, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", cfg.UserInfoURL+"?alt=json", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("x-request-source", "local")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil
	}

	respBody, _ := io.ReadAll(resp.Body)
	var userInfo map[string]any
	json.Unmarshal(respBody, &userInfo)
	return userInfo, nil
}

// fetchGoogleProjectId fetches project ID via loadCodeAssist.
func fetchGoogleProjectId(ctx context.Context, cfg ProviderOAuthConfig, accessToken string) (string, string, error) {
	loadCodeAssistURL := cfg.Extra["loadCodeAssistEndpoint"]
	metadata := getOAuthClientMetadata()

	payload := map[string]any{
		"metadata": metadata,
		"mode":     1,
	}

	jsonBody, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "POST", loadCodeAssistURL, strings.NewReader(string(jsonBody)))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", cfg.Extra["loadCodeAssistUserAgent"])
	req.Header.Set("X-Goog-Api-Client", cfg.Extra["loadCodeAssistApiClient"])
	req.Header.Set("Client-Metadata", cfg.Extra["loadCodeAssistClientMetadata"])
	req.Header.Set("x-request-source", "local")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", nil
	}

	respBody, _ := io.ReadAll(resp.Body)
	var data map[string]any
	json.Unmarshal(respBody, &data)

	projectId := ""
	if project, ok := data["cloudaicompanionProject"].(map[string]any); ok {
		if id, ok := project["id"].(string); ok {
			projectId = id
		} else if idStr, ok := project["project_id"].(string); ok {
			projectId = idStr
		}
	}

	tierId := ""
	if tiers, ok := data["allowedTiers"].([]any); ok {
		for _, tier := range tiers {
			if t, ok := tier.(map[string]any); ok {
				if isDefault, ok := t["isDefault"].(bool); ok && isDefault {
					if id, ok := t["id"].(string); ok {
						tierId = id
						break
					}
				}
			}
		}
	}

	return projectId, tierId, nil
}

// fireAntigravityOnboard fires and forgets the Antigravity onboard request.
func fireAntigravityOnboard(ctx context.Context, cfg ProviderOAuthConfig, accessToken, projectId, tierId string) {
	onboardURL := cfg.Extra["onboardUserEndpoint"]
	metadata := getOAuthClientMetadata()

	payload := map[string]any{
		"tierId":   tierId,
		"metadata": metadata,
	}

	jsonBody, _ := json.Marshal(payload)

	for i := 0; i < 10; i++ {
		req, _ := http.NewRequestWithContext(ctx, "POST", onboardURL, strings.NewReader(string(jsonBody)))
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", cfg.Extra["loadCodeAssistUserAgent"])
		req.Header.Set("X-Goog-Api-Client", cfg.Extra["loadCodeAssistApiClient"])
		req.Header.Set("Client-Metadata", cfg.Extra["loadCodeAssistClientMetadata"])
		req.Header.Set("x-request-source", "local")

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			break
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			var result map[string]any
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			json.Unmarshal(respBody, &result)
			if done, ok := result["done"].(bool); ok && done {
				break
			}
		} else {
			resp.Body.Close()
		}
	}
}

// fetchIFlowUserInfo fetches iFlow user info.
func fetchIFlowUserInfo(ctx context.Context, cfg ProviderOAuthConfig, accessToken string) (map[string]any, error) {
	userInfoURL := cfg.UserInfoURL + "?accessToken=" + url.QueryEscape(accessToken)
	req, _ := http.NewRequestWithContext(ctx, "GET", userInfoURL, nil)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]any
	json.Unmarshal(respBody, &result)

	if success, ok := result["success"].(bool); !ok || !success {
		return nil, fmt.Errorf("iFlow user info request failed: %v", result["message"])
	}

	data, ok := result["data"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("iFlow user info missing data")
	}

	return data, nil
}

// fetchQoderUserInfo fetches Qoder user info.
func fetchQoderUserInfo(ctx context.Context, cfg ProviderOAuthConfig, accessToken string) (map[string]any, error) {
	userInfoURL := cfg.UserInfoURL + "?accessToken=" + url.QueryEscape(accessToken)
	req, _ := http.NewRequestWithContext(ctx, "GET", userInfoURL, nil)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]any
	json.Unmarshal(respBody, &result)

	if success, ok := result["success"].(bool); !ok || !success {
		return nil, fmt.Errorf("Qoder user info request failed: %v", result["message"])
	}

	data, ok := result["data"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("Qoder user info missing data")
	}

	return data, nil
}

// fetchGitLabUserInfo fetches GitLab user info.
func fetchGitLabUserInfo(ctx context.Context, cfg ProviderOAuthConfig, accessToken, baseURL string) (map[string]any, error) {
	if baseURL == "" {
		baseURL = cfg.Extra["defaultBaseUrl"]
	}
	userInfoURL := baseURL + cfg.Extra["userInfoUrlPath"]

	req, _ := http.NewRequestWithContext(ctx, "GET", userInfoURL, nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil
	}

	respBody, _ := io.ReadAll(resp.Body)
	var userInfo map[string]any
	json.Unmarshal(respBody, &userInfo)
	return userInfo, nil
}

// MapTokens maps raw token response to normalized TokenResult.
func MapTokens(cfg ProviderOAuthConfig, rawTokens map[string]any, extra map[string]any) TokenResult {
	result := TokenResult{
		AccessToken:  stringValue(rawTokens["access_token"]),
		RefreshToken: stringValue(rawTokens["refresh_token"]),
		Scope:        stringValue(rawTokens["scope"]),
	}

	// ExpiresIn
	if expiresIn, ok := rawTokens["expires_in"].(float64); ok {
		result.ExpiresIn = int(expiresIn)
	} else if expiresIn, ok := rawTokens["expiresIn"].(float64); ok {
		result.ExpiresIn = int(expiresIn)
	} else {
		result.ExpiresIn = 3600
	}

	// Email from various sources
	if extra != nil {
		if email, ok := extra["email"].(string); ok {
			result.Email = email
		}
	}
	if result.Email == "" {
		result.Email = ExtractEmailFromJWT(result.AccessToken)
	}

	// Provider-specific data
	result.ProviderSpecificData = make(map[string]any)

	// Copy extra data
	if extra != nil {
		for k, v := range extra {
			result.ProviderSpecificData[k] = v
		}
	}

	// Provider-specific mappings
	switch cfg.Name {
	case "github", "copilot":
		result.ProviderSpecificData["copilotToken"] = extra["copilotToken"]
		result.ProviderSpecificData["copilotTokenExpiresAt"] = extra["copilotTokenExpiresAt"]
		result.ProviderSpecificData["githubUserId"] = extra["githubUserId"]
		result.ProviderSpecificData["githubLogin"] = extra["githubLogin"]
		result.ProviderSpecificData["githubName"] = extra["githubName"]
		result.ProviderSpecificData["githubEmail"] = extra["githubEmail"]

	case "kiro":
		result.ProviderSpecificData["profileArn"] = extra["profileArn"]
		result.ProviderSpecificData["clientId"] = rawTokens["_clientId"]
		result.ProviderSpecificData["clientSecret"] = rawTokens["_clientSecret"]
		result.ProviderSpecificData["region"] = rawTokens["_region"]
		result.ProviderSpecificData["authMethod"] = rawTokens["_authMethod"]
		result.ProviderSpecificData["startUrl"] = rawTokens["_startUrl"]

	case "gemini-cli", "antigravity":
		result.ProviderSpecificData["projectId"] = extra["projectId"]
		if tierId, ok := extra["tierId"].(string); ok {
			result.ProviderSpecificData["tierId"] = tierId
		}

	case "qwen":
		if resourceUrl, ok := rawTokens["resource_url"].(string); ok {
			result.ProviderSpecificData["resourceUrl"] = resourceUrl
		}

	case "kilocode":
		result.Email = stringValue(rawTokens["_userEmail"])
		if orgId, ok := rawTokens["_orgId"].(string); ok {
			result.ProviderSpecificData["orgId"] = orgId
		}

	case "cline":
		if email, ok := extra["email"].(string); ok {
			result.Email = email
		}
		if firstName, ok := extra["firstName"].(string); ok {
			result.ProviderSpecificData["firstName"] = firstName
		}
		if lastName, ok := extra["lastName"].(string); ok {
			result.ProviderSpecificData["lastName"] = lastName
		}

	case "gitlab":
		result.ProviderSpecificData["username"] = extra["username"]
		result.ProviderSpecificData["baseUrl"] = extra["baseUrl"]
		result.ProviderSpecificData["clientId"] = extra["clientId"]
		result.ProviderSpecificData["authKind"] = "oauth"
	}

	return result
}

// getOAuthClientMetadata returns the client metadata for Google OAuth.
func getOAuthClientMetadata() map[string]any {
	return map[string]any{
		"ideType":    9,
		"platform":   getOAuthPlatformEnum(),
		"pluginType": 2,
	}
}

// stringValue safely extracts a string value.
func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if f, ok := v.(float64); ok {
		return fmt.Sprintf("%.0f", f)
	}
	return ""
}
