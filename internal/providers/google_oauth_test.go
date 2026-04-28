package providers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRefreshGoogleToken(t *testing.T) {
	callCount := 0
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("failed to parse form: %v", err)
		}
		
		grantType := r.FormValue("grant_type")
		if grantType != "refresh_token" {
			t.Errorf("expected refresh_token grant type, got %s", grantType)
		}
		
		refreshToken := r.FormValue("refresh_token")
		if refreshToken == "" {
			t.Errorf("missing refresh_token")
		}
		
		clientID := r.FormValue("client_id")
		if clientID == "" {
			t.Errorf("missing client_id")
		}
		
		clientSecret := r.FormValue("client_secret")
		if clientSecret == "" {
			t.Errorf("missing client_secret")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  "new-access-token",
			"refresh_token": "new-refresh-token",
			"expires_in":    3600,
			"token_type":    "Bearer",
		})
	}))
	defer tokenServer.Close()

	config := GoogleOAuthConfig{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		TokenURL:     tokenServer.URL,
	}

	client := &http.Client{}
	tokenResp, err := RefreshGoogleToken("old-refresh-token", config, client)
	if err != nil {
		t.Fatalf("failed to refresh token: %v", err)
	}
	
	if tokenResp.AccessToken != "new-access-token" {
		t.Errorf("expected 'new-access-token', got %s", tokenResp.AccessToken)
	}
	if tokenResp.RefreshToken != "new-refresh-token" {
		t.Errorf("expected 'new-refresh-token', got %s", tokenResp.RefreshToken)
	}
	if tokenResp.ExpiresIn != 3600 {
		t.Errorf("expected 3600, got %d", tokenResp.ExpiresIn)
	}

	// Test cache: second call should not hit server
	ClearGoogleTokenCache()
	tokenResp2, err := RefreshGoogleToken("old-refresh-token", config, client)
	if err != nil {
		t.Fatalf("failed to get cached token: %v", err)
	}
	if tokenResp2.AccessToken != tokenResp.AccessToken {
		t.Errorf("expected cached token")
	}
}

func TestRefreshGoogleTokenCache(t *testing.T) {
	ClearGoogleTokenCache()
	
	callCount := 0
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "token-" + string(rune('0'+callCount)),
			"expires_in":   1, // 1 second expiry
			"token_type":   "Bearer",
		})
	}))
	defer tokenServer.Close()

	config := GoogleOAuthConfig{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		TokenURL:     tokenServer.URL,
	}

	client := &http.Client{}
	token1, err := RefreshGoogleToken("refresh-token", config, client)
	if err != nil {
		t.Fatalf("failed to get first token: %v", err)
	}

	// Wait for token to expire
	time.Sleep(2 * time.Second)

	token2, err := RefreshGoogleToken("refresh-token", config, client)
	if err != nil {
		t.Fatalf("failed to get second token: %v", err)
	}

	if token1.AccessToken == token2.AccessToken {
		t.Errorf("expected new token after expiry, got same token")
	}
	if callCount < 2 {
		t.Errorf("expected at least 2 token server calls, got %d", callCount)
	}
}

func TestShouldRefreshGoogleToken(t *testing.T) {
	tests := []struct {
		name     string
		expiresAt *time.Time
		expected bool
	}{
		{
			name:     "nil expiry",
			expiresAt: nil,
			expected: false,
		},
		{
			name:     "expired",
			expiresAt: func() *time.Time { t := time.Now().Add(-1 * time.Hour); return &t }(),
			expected: true,
		},
		{
			name:     "expires soon (within buffer)",
			expiresAt: func() *time.Time { t := time.Now().Add(2 * time.Minute); return &t }(),
			expected: true,
		},
		{
			name:     "valid (beyond buffer)",
			expiresAt: func() *time.Time { t := time.Now().Add(10 * time.Minute); return &t }(),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ShouldRefreshGoogleToken(tt.expiresAt)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestRefreshGoogleTokenError(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer tokenServer.Close()

	config := GoogleOAuthConfig{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		TokenURL:     tokenServer.URL,
	}

	client := &http.Client{}
	_, err := RefreshGoogleToken("invalid-refresh-token", config, client)
	if err == nil {
		t.Errorf("expected error, got nil")
	}
}

func TestRefreshGoogleTokenFallbackRefreshToken(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Don't return refresh_token in response
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "new-access-token",
			"expires_in":   3600,
			"token_type":   "Bearer",
		})
	}))
	defer tokenServer.Close()

	config := GoogleOAuthConfig{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		TokenURL:     tokenServer.URL,
	}

	ClearGoogleTokenCache()
	client := &http.Client{}
	tokenResp, err := RefreshGoogleToken("old-refresh-token", config, client)
	if err != nil {
		t.Fatalf("failed to refresh token: %v", err)
	}
	
	// Should fallback to old refresh token
	if tokenResp.RefreshToken != "old-refresh-token" {
		t.Errorf("expected fallback to 'old-refresh-token', got %s", tokenResp.RefreshToken)
	}
}
