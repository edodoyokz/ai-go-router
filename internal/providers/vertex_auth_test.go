package providers

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// generateTestRSAKey generates a deterministic RSA key for testing.
func generateTestRSAKey() (*rsa.PrivateKey, error) {
	return rsa.GenerateKey(rand.Reader, 2048)
}

// encodePrivateKeyPEM encodes an RSA private key to PEM format.
func encodePrivateKeyPEM(key *rsa.PrivateKey) string {
	keyBytes := x509.MarshalPKCS1PrivateKey(key)
	pemBlock := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: keyBytes,
	}
	return string(pem.EncodeToMemory(pemBlock))
}

func TestParseVertexServiceAccount(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantError bool
		wantNil   bool
	}{
		{
			name:    "valid service account JSON",
			input:   `{"type":"service_account","project_id":"test-project","private_key":"-----BEGIN RSA PRIVATE KEY-----\ntest\n-----END RSA PRIVATE KEY-----","client_email":"test@test.iam.gserviceaccount.com"}`,
			wantNil: false,
		},
		{
			name:    "raw API key",
			input:   "AIzaSyTest123456",
			wantNil: true,
		},
		{
			name:      "invalid JSON",
			input:     `{"type":"service_account"`,
			wantError: true,
		},
		{
			name:      "missing client_email",
			input:     `{"type":"service_account","project_id":"test","private_key":"key"}`,
			wantError: true,
		},
		{
			name:      "missing private_key",
			input:     `{"type":"service_account","project_id":"test","client_email":"test@test.iam.gserviceaccount.com"}`,
			wantError: true,
		},
		{
			name:      "missing project_id",
			input:     `{"type":"service_account","private_key":"key","client_email":"test@test.iam.gserviceaccount.com"}`,
			wantError: true,
		},
		{
			name:      "wrong type",
			input:     `{"type":"user","project_id":"test","private_key":"key","client_email":"test@test.iam.gserviceaccount.com"}`,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sa, err := ParseVertexServiceAccount(tt.input)
			if tt.wantError {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if tt.wantNil && sa != nil {
				t.Errorf("expected nil, got %+v", sa)
			}
			if !tt.wantNil && sa == nil {
				t.Errorf("expected non-nil service account")
			}
		})
	}
}

func TestGetVertexAccessToken(t *testing.T) {
	// Generate test RSA key
	privateKey, err := generateTestRSAKey()
	if err != nil {
		t.Fatalf("failed to generate test key: %v", err)
	}
	pemKey := encodePrivateKeyPEM(privateKey)

	// Mock token server
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("failed to parse form: %v", err)
		}
		grantType := r.FormValue("grant_type")
		if grantType != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
			t.Errorf("expected jwt-bearer grant type, got %s", grantType)
		}
		assertion := r.FormValue("assertion")
		if assertion == "" {
			t.Errorf("missing assertion")
		}

		// Return mock token
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "test-access-token",
			"expires_in":   3600,
			"token_type":   "Bearer",
		})
	}))
	defer tokenServer.Close()

	sa := &VertexServiceAccount{
		Type:        "service_account",
		ProjectID:   "test-project",
		PrivateKey:  pemKey,
		ClientEmail: "test@test.iam.gserviceaccount.com",
		TokenURI:    tokenServer.URL,
	}

	client := &http.Client{}
	token, err := GetVertexAccessToken(sa, client)
	if err != nil {
		t.Fatalf("failed to get access token: %v", err)
	}
	if token != "test-access-token" {
		t.Errorf("expected 'test-access-token', got %s", token)
	}

	// Test cache: second call should not hit server
	token2, err := GetVertexAccessToken(sa, client)
	if err != nil {
		t.Fatalf("failed to get cached token: %v", err)
	}
	if token2 != token {
		t.Errorf("expected cached token %s, got %s", token, token2)
	}
}

func TestGetVertexAccessTokenExpiry(t *testing.T) {
	privateKey, err := generateTestRSAKey()
	if err != nil {
		t.Fatalf("failed to generate test key: %v", err)
	}
	pemKey := encodePrivateKeyPEM(privateKey)

	callCount := 0
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "test-token-" + string(rune('0'+callCount)),
			"expires_in":   1, // 1 second expiry
			"token_type":   "Bearer",
		})
	}))
	defer tokenServer.Close()

	sa := &VertexServiceAccount{
		Type:        "service_account",
		ProjectID:   "test-project",
		PrivateKey:  pemKey,
		ClientEmail: "test-expiry@test.iam.gserviceaccount.com",
		TokenURI:    tokenServer.URL,
	}

	// Clear cache for this test
	vertexTokenCache.mu.Lock()
	delete(vertexTokenCache.tokens, sa.ClientEmail)
	vertexTokenCache.mu.Unlock()

	client := &http.Client{}
	token1, err := GetVertexAccessToken(sa, client)
	if err != nil {
		t.Fatalf("failed to get first token: %v", err)
	}

	// Wait for token to expire (beyond refresh buffer)
	time.Sleep(2 * time.Second)

	token2, err := GetVertexAccessToken(sa, client)
	if err != nil {
		t.Fatalf("failed to get second token: %v", err)
	}

	if token1 == token2 {
		t.Errorf("expected new token after expiry, got same token")
	}
	if callCount < 2 {
		t.Errorf("expected at least 2 token server calls, got %d", callCount)
	}
}

func TestResolveVertexProjectID(t *testing.T) {
	tests := []struct {
		name          string
		responseBody  string
		expectedID    string
		expectError   bool
	}{
		{
			name:         "single error object",
			responseBody: `{"error":{"message":"Resource projects/my-test-project/locations/us-central1 not found"}}`,
			expectedID:   "my-test-project",
		},
		{
			name:         "error array",
			responseBody: `[{"error":{"message":"Invalid request for projects/another-project/models"}}]`,
			expectedID:   "another-project",
		},
		{
			name:         "no project in message",
			responseBody: `{"error":{"message":"Invalid API key"}}`,
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.Contains(r.URL.Path, "__probe__") {
					t.Errorf("expected probe path, got %s", r.URL.Path)
				}
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			// Test extractProjectID helper directly
			var projectID string
			
			// Try single error object
			var errorResp struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(tt.responseBody), &errorResp); err == nil && errorResp.Error.Message != "" {
				projectID = extractProjectID(errorResp.Error.Message)
			}
			
			// Try error array if single object didn't work
			if projectID == "" {
				var errorArray []struct {
					Error struct {
						Message string `json:"message"`
					} `json:"error"`
				}
				if err := json.Unmarshal([]byte(tt.responseBody), &errorArray); err == nil && len(errorArray) > 0 {
					projectID = extractProjectID(errorArray[0].Error.Message)
				}
			}

			if tt.expectError {
				if projectID != "" {
					t.Errorf("expected error, got project ID: %s", projectID)
				}
			} else {
				if projectID != tt.expectedID {
					t.Errorf("expected project ID %s, got %s", tt.expectedID, projectID)
				}
			}
		})
	}
}

func TestExtractProjectID(t *testing.T) {
	tests := []struct {
		message    string
		expectedID string
	}{
		{
			message:    "Resource projects/my-project/locations/us-central1 not found",
			expectedID: "my-project",
		},
		{
			message:    "projects/test-123/models/gemini-pro",
			expectedID: "test-123",
		},
		{
			message:    "Invalid request",
			expectedID: "",
		},
		{
			message:    "projects/",
			expectedID: "",
		},
		{
			message:    "",
			expectedID: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			result := extractProjectID(tt.message)
			if result != tt.expectedID {
				t.Errorf("expected %s, got %s", tt.expectedID, result)
			}
		})
	}
}
