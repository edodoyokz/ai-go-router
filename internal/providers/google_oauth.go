package providers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// GoogleOAuthConfig holds OAuth configuration for Google providers.
type GoogleOAuthConfig struct {
	ClientID     string
	ClientSecret string
	TokenURL     string
}

// GoogleTokenResponse represents the response from Google's token endpoint.
type GoogleTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

// GoogleTokenCache holds cached access tokens for Google OAuth.
type GoogleTokenCache struct {
	mu     sync.RWMutex
	tokens map[string]*cachedGoogleToken
}

type cachedGoogleToken struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

var (
	googleTokenCache = &GoogleTokenCache{
		tokens: make(map[string]*cachedGoogleToken),
	}
	// Google token refresh lead time: refresh if expires within 5 minutes
	googleRefreshBuffer = 5 * time.Minute
)

// RefreshGoogleToken refreshes a Google OAuth access token using a refresh token.
// It caches the result per refresh token until 5 minutes before expiry.
func RefreshGoogleToken(refreshToken string, config GoogleOAuthConfig, httpClient *http.Client) (*GoogleTokenResponse, error) {
	if refreshToken == "" {
		return nil, fmt.Errorf("refresh token is required")
	}

	// Check cache first
	cacheKey := refreshToken
	googleTokenCache.mu.RLock()
	cached, exists := googleTokenCache.tokens[cacheKey]
	googleTokenCache.mu.RUnlock()

	if exists && time.Until(cached.ExpiresAt) > googleRefreshBuffer {
		return &GoogleTokenResponse{
			AccessToken:  cached.AccessToken,
			RefreshToken: cached.RefreshToken,
			ExpiresIn:    int(time.Until(cached.ExpiresAt).Seconds()),
		}, nil
	}

	// Perform token refresh
	tokenURL := config.TokenURL
	if tokenURL == "" {
		tokenURL = "https://oauth2.googleapis.com/token"
	}

	formData := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {config.ClientID},
		"client_secret": {config.ClientSecret},
	}

	resp, err := httpClient.PostForm(tokenURL, formData)
	if err != nil {
		return nil, fmt.Errorf("failed to refresh Google token: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Google token refresh failed (status %d): %s", resp.StatusCode, string(body))
	}

	var tokenResp GoogleTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("no access token in response")
	}

	// Use new refresh token if provided, otherwise keep the old one
	if tokenResp.RefreshToken == "" {
		tokenResp.RefreshToken = refreshToken
	}

	// Calculate expiry time
	expiresIn := tokenResp.ExpiresIn
	if expiresIn == 0 {
		expiresIn = 3600 // Default 1 hour
	}
	expiresAt := time.Now().Add(time.Duration(expiresIn) * time.Second)

	// Cache the token
	googleTokenCache.mu.Lock()
	googleTokenCache.tokens[cacheKey] = &cachedGoogleToken{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    expiresAt,
	}
	googleTokenCache.mu.Unlock()

	return &tokenResp, nil
}

// ShouldRefreshGoogleToken checks if a token should be refreshed based on expiry time.
func ShouldRefreshGoogleToken(expiresAt *time.Time) bool {
	if expiresAt == nil {
		return false
	}
	return time.Until(*expiresAt) <= googleRefreshBuffer
}

// ClearGoogleTokenCache clears the token cache (useful for testing).
func ClearGoogleTokenCache() {
	googleTokenCache.mu.Lock()
	googleTokenCache.tokens = make(map[string]*cachedGoogleToken)
	googleTokenCache.mu.Unlock()
}
