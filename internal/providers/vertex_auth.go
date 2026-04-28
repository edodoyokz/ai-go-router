package providers

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// VertexServiceAccount represents a parsed GCP Service Account JSON.
type VertexServiceAccount struct {
	Type                    string `json:"type"`
	ProjectID               string `json:"project_id"`
	PrivateKeyID            string `json:"private_key_id"`
	PrivateKey              string `json:"private_key"`
	ClientEmail             string `json:"client_email"`
	ClientID                string `json:"client_id"`
	AuthURI                 string `json:"auth_uri"`
	TokenURI                string `json:"token_uri"`
	AuthProviderX509CertURL string `json:"auth_provider_x509_cert_url"`
	ClientX509CertURL       string `json:"client_x509_cert_url"`
}

// VertexTokenCache holds cached access tokens for service accounts.
type VertexTokenCache struct {
	mu     sync.RWMutex
	tokens map[string]*cachedToken
}

type cachedToken struct {
	AccessToken string
	ExpiresAt   time.Time
}

var (
	vertexTokenCache = &VertexTokenCache{
		tokens: make(map[string]*cachedToken),
	}
	// Token refresh lead time: refresh if expires within 5 minutes
	tokenRefreshBuffer = 5 * time.Minute
)

// ParseVertexServiceAccount parses a Service Account JSON string.
func ParseVertexServiceAccount(apiKey string) (*VertexServiceAccount, error) {
	trimmed := strings.TrimSpace(apiKey)
	if !strings.HasPrefix(trimmed, "{") {
		return nil, nil // Not a JSON, likely a raw API key
	}

	var sa VertexServiceAccount
	if err := json.Unmarshal([]byte(trimmed), &sa); err != nil {
		return nil, fmt.Errorf("invalid service account JSON: %w", err)
	}

	// Validate required fields
	if sa.Type != "service_account" {
		return nil, fmt.Errorf("invalid service account type: %s", sa.Type)
	}
	if sa.ClientEmail == "" {
		return nil, fmt.Errorf("missing client_email in service account JSON")
	}
	if sa.PrivateKey == "" {
		return nil, fmt.Errorf("missing private_key in service account JSON")
	}
	if sa.ProjectID == "" {
		return nil, fmt.Errorf("missing project_id in service account JSON")
	}

	return &sa, nil
}

// GetVertexAccessToken mints or retrieves a cached Bearer token for Vertex AI.
// Tokens are cached per service account email until 5 minutes before expiry.
func GetVertexAccessToken(sa *VertexServiceAccount, httpClient *http.Client) (string, error) {
	cacheKey := sa.ClientEmail

	// Check cache first
	vertexTokenCache.mu.RLock()
	cached, exists := vertexTokenCache.tokens[cacheKey]
	vertexTokenCache.mu.RUnlock()

	if exists && time.Until(cached.ExpiresAt) > tokenRefreshBuffer {
		return cached.AccessToken, nil
	}

	// Mint new token
	accessToken, expiresAt, err := mintVertexToken(sa, httpClient)
	if err != nil {
		return "", err
	}

	// Cache the token
	vertexTokenCache.mu.Lock()
	vertexTokenCache.tokens[cacheKey] = &cachedToken{
		AccessToken: accessToken,
		ExpiresAt:   expiresAt,
	}
	vertexTokenCache.mu.Unlock()

	return accessToken, nil
}

// mintVertexToken creates a JWT assertion and exchanges it for an access token.
func mintVertexToken(sa *VertexServiceAccount, httpClient *http.Client) (string, time.Time, error) {
	// Parse the private key
	privateKey, err := parsePrivateKey(sa.PrivateKey)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("failed to parse private key: %w", err)
	}

	// Create JWT claims
	now := time.Now()
	expiresAt := now.Add(1 * time.Hour)

	claims := jwt.MapClaims{
		"iss":   sa.ClientEmail,
		"scope": "https://www.googleapis.com/auth/cloud-platform",
		"aud":   "https://oauth2.googleapis.com/token",
		"iat":   now.Unix(),
		"exp":   expiresAt.Unix(),
	}

	// Sign the JWT
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	assertion, err := token.SignedString(privateKey)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("failed to sign JWT: %w", err)
	}

	// Exchange JWT for access token
	tokenURI := sa.TokenURI
	if tokenURI == "" {
		tokenURI = "https://oauth2.googleapis.com/token"
	}

	formData := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {assertion},
	}

	resp, err := httpClient.PostForm(tokenURI, formData)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("failed to exchange JWT for token: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", time.Time{}, fmt.Errorf("token exchange failed (status %d): %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		TokenType   string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", time.Time{}, fmt.Errorf("failed to parse token response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return "", time.Time{}, fmt.Errorf("no access token in response")
	}

	// Calculate actual expiry time
	expiresIn := tokenResp.ExpiresIn
	if expiresIn == 0 {
		expiresIn = 3600 // Default 1 hour
	}
	actualExpiresAt := now.Add(time.Duration(expiresIn) * time.Second)

	return tokenResp.AccessToken, actualExpiresAt, nil
}

// parsePrivateKey parses a PEM-encoded RSA private key.
func parsePrivateKey(pemKey string) (*rsa.PrivateKey, error) {
	// Handle escaped newlines
	pemKey = strings.ReplaceAll(pemKey, "\\n", "\n")

	block, _ := pem.Decode([]byte(pemKey))
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	// Try PKCS8 first (most common for GCP service accounts)
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if rsaKey, ok := key.(*rsa.PrivateKey); ok {
			return rsaKey, nil
		}
		return nil, fmt.Errorf("private key is not RSA")
	}

	// Fallback to PKCS1
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}

	return nil, fmt.Errorf("failed to parse private key as PKCS8 or PKCS1")
}

// ResolveVertexProjectID attempts to auto-resolve project ID from a raw Vertex API key.
// It sends a probe request and parses "projects/{id}/" from the error message.
func ResolveVertexProjectID(apiKey string, httpClient *http.Client) (string, error) {
	probeURL := fmt.Sprintf("https://aiplatform.googleapis.com/v1/publishers/google/models/__probe__:generateContent?key=%s", apiKey)

	resp, err := httpClient.Post(probeURL, "application/json", strings.NewReader("{}"))
	if err != nil {
		return "", fmt.Errorf("failed to probe project ID: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	// Parse error message to extract project ID
	var errorResp struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	// Try single error object
	if err := json.Unmarshal(body, &errorResp); err == nil && errorResp.Error.Message != "" {
		if projectID := extractProjectID(errorResp.Error.Message); projectID != "" {
			return projectID, nil
		}
	}

	// Try array of errors
	var errorArray []struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &errorArray); err == nil && len(errorArray) > 0 {
		if projectID := extractProjectID(errorArray[0].Error.Message); projectID != "" {
			return projectID, nil
		}
	}

	return "", fmt.Errorf("could not resolve project ID from API key")
}

// extractProjectID extracts project ID from error message containing "projects/{id}/".
func extractProjectID(message string) string {
	// Look for pattern: projects/{projectId}/
	start := strings.Index(message, "projects/")
	if start == -1 {
		return ""
	}
	start += len("projects/")
	end := strings.Index(message[start:], "/")
	if end == -1 {
		return ""
	}
	return message[start : start+end]
}
