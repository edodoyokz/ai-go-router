package oauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"
)

func tempStore(t *testing.T) *Store {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "oauth_test_*.db")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	store, err := NewStore(f.Name(), "test-passphrase")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestSaveAndGetToken(t *testing.T) {
	store := tempStore(t)
	ctx := context.Background()

	rec := TokenRecord{
		Provider:     "openai",
		Account:      "default",
		AccessToken:  "sk-test-access",
		RefreshToken: "rt-test-refresh",
		ExpiresAt:    time.Now().Add(time.Hour).Truncate(time.Second),
		Scope:        "read write",
		TokenType:    "Bearer",
	}

	if err := store.SaveToken(ctx, rec); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	got, err := store.GetToken(ctx, "openai", "default")
	if err != nil {
		t.Fatalf("GetToken: %v", err)
	}
	if got == nil {
		t.Fatal("expected token, got nil")
	}
	if got.AccessToken != rec.AccessToken {
		t.Errorf("AccessToken: got %q, want %q", got.AccessToken, rec.AccessToken)
	}
	if got.RefreshToken != rec.RefreshToken {
		t.Errorf("RefreshToken: got %q, want %q", got.RefreshToken, rec.RefreshToken)
	}
	if got.Scope != rec.Scope {
		t.Errorf("Scope: got %q, want %q", got.Scope, rec.Scope)
	}
}

func TestGetToken_NotFound(t *testing.T) {
	store := tempStore(t)
	got, err := store.GetToken(context.Background(), "missing", "none")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for missing token")
	}
}

func TestDeleteToken(t *testing.T) {
	store := tempStore(t)
	ctx := context.Background()

	rec := TokenRecord{
		Provider: "anthropic", Account: "main",
		AccessToken: "tok", ExpiresAt: time.Now().Add(time.Hour),
	}
	store.SaveToken(ctx, rec) //nolint:errcheck

	if err := store.DeleteToken(ctx, "anthropic", "main"); err != nil {
		t.Fatalf("DeleteToken: %v", err)
	}

	got, err := store.GetToken(ctx, "anthropic", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestIsExpired(t *testing.T) {
	expired := &TokenRecord{ExpiresAt: time.Now().Add(-time.Minute)}
	if !IsExpired(expired, 0) {
		t.Error("expected expired token to be expired")
	}

	valid := &TokenRecord{ExpiresAt: time.Now().Add(time.Hour)}
	if IsExpired(valid, 0) {
		t.Error("expected valid token to not be expired")
	}

	// With leeway: token expiring in 5 min but leeway is 10 min → should be expired
	soonExpiring := &TokenRecord{ExpiresAt: time.Now().Add(5 * time.Minute)}
	if !IsExpired(soonExpiring, 10*time.Minute) {
		t.Error("expected token within leeway to be treated as expired")
	}
}

func TestBuildAuthURL(t *testing.T) {
	cfg := ProviderOAuthConfig{
		Name:        "github",
		AuthURL:     "https://github.com/login/oauth/authorize",
		ClientID:    "my-client-id",
		RedirectURL: "http://localhost:1988/callback",
		Scopes:      []string{"read:user", "repo"},
	}
	authURL := BuildAuthURL(cfg, "test-state")
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("invalid auth URL: %v", err)
	}
	q := parsed.Query()
	if q.Get("client_id") != "my-client-id" {
		t.Errorf("client_id: got %q", q.Get("client_id"))
	}
	if q.Get("state") != "test-state" {
		t.Errorf("state: got %q", q.Get("state"))
	}
	if q.Get("response_type") != "code" {
		t.Errorf("response_type: got %q", q.Get("response_type"))
	}
}

func TestExchangeCode_WithMockServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.FormValue("grant_type") != "authorization_code" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access-token",
			"refresh_token": "new-refresh-token",
			"expires_in":    3600,
			"scope":         "read",
		})
	}))
	defer srv.Close()

	cfg := ProviderOAuthConfig{
		Name:         "test",
		TokenURL:     srv.URL,
		ClientID:     "cid",
		ClientSecret: "csec",
		RedirectURL:  "http://localhost/callback",
	}

	rec, err := ExchangeCode(context.Background(), cfg, "auth-code-123")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if rec.AccessToken != "new-access-token" {
		t.Errorf("AccessToken: got %q", rec.AccessToken)
	}
	if rec.RefreshToken != "new-refresh-token" {
		t.Errorf("RefreshToken: got %q", rec.RefreshToken)
	}
	if rec.Scope != "read" {
		t.Errorf("Scope: got %q", rec.Scope)
	}
}

func TestRefreshToken_WithMockServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm() //nolint:errcheck
		if r.FormValue("grant_type") != "refresh_token" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "refreshed-token",
			"expires_in":   7200,
		})
	}))
	defer srv.Close()

	cfg := ProviderOAuthConfig{
		Name:         "test",
		TokenURL:     srv.URL,
		ClientID:     "cid",
		ClientSecret: "csec",
	}
	old := &TokenRecord{
		Provider:     "test",
		Account:      "main",
		RefreshToken: "old-refresh",
	}

	newRec, err := RefreshToken(context.Background(), cfg, old)
	if err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}
	if newRec.AccessToken != "refreshed-token" {
		t.Errorf("AccessToken: got %q", newRec.AccessToken)
	}
	// RefreshToken should be carried over when not returned by server
	if newRec.RefreshToken != "old-refresh" {
		t.Errorf("RefreshToken: expected old refresh to be kept, got %q", newRec.RefreshToken)
	}
}

func TestEnsureFreshTokenRefreshesAndPersists(t *testing.T) {
	store := tempStore(t)
	ctx := context.Background()
	if err := store.SaveToken(ctx, TokenRecord{
		Provider:     "test",
		Account:      "main",
		AccessToken:  "old-access",
		RefreshToken: "old-refresh",
		ExpiresAt:    time.Now().Add(-time.Minute),
		TokenType:    "Bearer",
	}); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm() //nolint:errcheck
		if r.FormValue("refresh_token") != "old-refresh" {
			t.Fatalf("refresh_token = %q", r.FormValue("refresh_token"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"access_token": "fresh-access", "refresh_token": "fresh-refresh", "expires_in": 3600})
	}))
	defer srv.Close()

	rec, refreshed, err := EnsureFreshToken(ctx, store, ProviderOAuthConfig{Name: "test", TokenURL: srv.URL, ClientID: "cid"}, "test", "main", time.Minute)
	if err != nil {
		t.Fatalf("EnsureFreshToken: %v", err)
	}
	if !refreshed {
		t.Fatalf("expected token refresh")
	}
	if rec.AccessToken != "fresh-access" {
		t.Fatalf("access token = %q", rec.AccessToken)
	}
	stored, err := store.GetToken(ctx, "test", "main")
	if err != nil {
		t.Fatalf("GetToken: %v", err)
	}
	if stored.RefreshToken != "fresh-refresh" {
		t.Fatalf("stored refresh token = %q", stored.RefreshToken)
	}
}
