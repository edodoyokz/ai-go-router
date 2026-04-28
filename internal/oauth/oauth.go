// Package oauth provides token storage (AES-GCM encrypted), refresh, and
// per-provider OAuth 2.0 flow support for the router.
package oauth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// TokenRecord holds an OAuth 2.0 token for a provider/account.
type TokenRecord struct {
	Provider             string
	Account              string
	AccessToken          string
	RefreshToken         string
	ExpiresAt            time.Time
	Scope                string
	TokenType            string
	ProviderSpecificData map[string]any
}

// FlowType represents the OAuth flow type supported by a provider.
type FlowType string

const (
	FlowTypeAuthCodePKCE FlowType = "authorization_code_pkce"
	FlowTypeAuthCode     FlowType = "authorization_code"
	FlowTypeDeviceCode   FlowType = "device_code"
	FlowTypeImportToken  FlowType = "import_token"
)

// ProviderOAuthConfig describes how to perform the OAuth flow for a provider.
type ProviderOAuthConfig struct {
	Name          string
	FlowType      FlowType
	AuthURL       string
	TokenURL      string
	DeviceCodeURL string
	ClientID      string
	ClientSecret  string
	Scopes        []string
	Scope         string // For providers that use a single scope string
	// RedirectURL is typically "http://localhost:<port>/oauth/callback"
	RedirectURL         string
	CodeChallengeMethod string
	ExtraAuthParams     map[string]string
	UserInfoURL         string
	FixedPort           int
	CallbackPath        string
	ContentType         string // "application/json" or "application/x-www-form-urlencoded"
	NoPKCEForDeviceCode bool
	Extra               map[string]string // Provider-specific fields
}

type ProviderRegistry struct {
	mu        sync.RWMutex
	providers map[string]ProviderOAuthConfig
}

func NewProviderRegistry(configs ...ProviderOAuthConfig) *ProviderRegistry {
	r := &ProviderRegistry{providers: map[string]ProviderOAuthConfig{}}
	for _, cfg := range configs {
		r.Register(cfg)
	}
	return r
}

func (r *ProviderRegistry) Register(cfg ProviderOAuthConfig) {
	name := strings.ToLower(strings.TrimSpace(cfg.Name))
	if name == "" {
		return
	}
	cfg.Name = name
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[name] = cfg
}

func (r *ProviderRegistry) Get(name string) (ProviderOAuthConfig, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cfg, ok := r.providers[strings.ToLower(strings.TrimSpace(name))]
	return cfg, ok
}

// List returns all registered provider names.
func (r *ProviderRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}

// Store manages encrypted OAuth token storage backed by SQLite.
type Store struct {
	db     *sql.DB
	mu     sync.RWMutex
	encKey []byte // 32-byte AES key derived from passphrase
}

// NewStore opens (or creates) the OAuth token store in the given SQLite DB file.
// The passphrase is used to derive the AES-GCM encryption key.
func NewStore(dbPath, passphrase string) (*Store, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("oauth store: open db: %w", err)
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("oauth store: migrate: %w", err)
	}

	key := deriveKey(passphrase)
	return &Store{db: db, encKey: key}, nil
}

func deriveKey(passphrase string) []byte {
	h := sha256.Sum256([]byte(passphrase))
	return h[:]
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS oauth_tokens (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		provider      TEXT NOT NULL,
		account       TEXT NOT NULL,
		access_token  TEXT NOT NULL,  -- AES-GCM encrypted, base64
		refresh_token TEXT,           -- AES-GCM encrypted, base64
		expires_at    INTEGER,        -- Unix timestamp
		scope         TEXT,
		token_type    TEXT,
		updated_at    INTEGER NOT NULL DEFAULT (strftime('%s','now')),
		UNIQUE(provider, account)
	)`)
	return err
}

// SaveToken encrypts and persists a token record.
func (s *Store) SaveToken(ctx context.Context, rec TokenRecord) error {
	encAccess, err := s.encrypt(rec.AccessToken)
	if err != nil {
		return fmt.Errorf("oauth: encrypt access token: %w", err)
	}
	encRefresh := ""
	if rec.RefreshToken != "" {
		encRefresh, err = s.encrypt(rec.RefreshToken)
		if err != nil {
			return fmt.Errorf("oauth: encrypt refresh token: %w", err)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO oauth_tokens (provider, account, access_token, refresh_token, expires_at, scope, token_type, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(provider, account) DO UPDATE SET
			access_token  = excluded.access_token,
			refresh_token = excluded.refresh_token,
			expires_at    = excluded.expires_at,
			scope         = excluded.scope,
			token_type    = excluded.token_type,
			updated_at    = excluded.updated_at
	`, rec.Provider, rec.Account, encAccess, encRefresh, rec.ExpiresAt.Unix(), rec.Scope, rec.TokenType, time.Now().Unix())
	return err
}

// GetToken retrieves and decrypts the token for the given provider/account.
func (s *Store) GetToken(ctx context.Context, provider, account string) (*TokenRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	row := s.db.QueryRowContext(ctx, `
		SELECT provider, account, access_token, refresh_token, expires_at, scope, token_type
		FROM oauth_tokens WHERE provider = ? AND account = ?
	`, provider, account)

	var rec TokenRecord
	var encAccess, encRefresh string
	var expiresAt int64
	if err := row.Scan(&rec.Provider, &rec.Account, &encAccess, &encRefresh, &expiresAt, &rec.Scope, &rec.TokenType); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("oauth: get token: %w", err)
	}

	access, err := s.decrypt(encAccess)
	if err != nil {
		return nil, fmt.Errorf("oauth: decrypt access token: %w", err)
	}
	rec.AccessToken = access
	rec.ExpiresAt = time.Unix(expiresAt, 0)

	if encRefresh != "" {
		refresh, err := s.decrypt(encRefresh)
		if err != nil {
			return nil, fmt.Errorf("oauth: decrypt refresh token: %w", err)
		}
		rec.RefreshToken = refresh
	}

	return &rec, nil
}

// DeleteToken removes the token for the given provider/account.
func (s *Store) DeleteToken(ctx context.Context, provider, account string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.ExecContext(ctx, `DELETE FROM oauth_tokens WHERE provider = ? AND account = ?`, provider, account)
	return err
}

// ListTokens returns all stored token records with secrets redacted.
func (s *Store) ListTokens(ctx context.Context) ([]TokenRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `
		SELECT provider, account, expires_at, scope, token_type
		FROM oauth_tokens ORDER BY provider, account
	`)
	if err != nil {
		return nil, fmt.Errorf("oauth: list tokens: %w", err)
	}
	defer rows.Close()

	var records []TokenRecord
	for rows.Next() {
		var rec TokenRecord
		var expiresAt int64
		if err := rows.Scan(&rec.Provider, &rec.Account, &expiresAt, &rec.Scope, &rec.TokenType); err != nil {
			return nil, fmt.Errorf("oauth: scan token row: %w", err)
		}
		rec.ExpiresAt = time.Unix(expiresAt, 0)
		records = append(records, rec)
	}
	return records, nil
}

// IsExpired returns true if the token is expired or within the given leeway.
func IsExpired(rec *TokenRecord, leeway time.Duration) bool {
	return time.Now().Add(leeway).After(rec.ExpiresAt)
}

// --- Encryption helpers ---

func (s *Store) encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(s.encKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func (s *Store) decrypt(encoded string) (string, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(s.encKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// --- Token refresh ---

// RefreshToken exchanges a refresh token for a new access token using the provider config.
func RefreshToken(ctx context.Context, cfg ProviderOAuthConfig, rec *TokenRecord) (*TokenRecord, error) {
	if rec.RefreshToken == "" {
		return nil, fmt.Errorf("oauth: no refresh token available for %s/%s", cfg.Name, rec.Account)
	}

	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", rec.RefreshToken)
	data.Set("client_id", cfg.ClientID)
	data.Set("client_secret", cfg.ClientSecret)

	req, err := http.NewRequestWithContext(ctx, "POST", cfg.TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("oauth: create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oauth: refresh request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("oauth: read refresh response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oauth: refresh failed (%d): %s", resp.StatusCode, string(body))
	}

	var tokenResp map[string]interface{}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("oauth: parse refresh response: %w", err)
	}

	accessToken, _ := tokenResp["access_token"].(string)
	if accessToken == "" {
		return nil, fmt.Errorf("oauth: no access_token in refresh response")
	}

	newRec := &TokenRecord{
		Provider:    rec.Provider,
		Account:     rec.Account,
		AccessToken: accessToken,
		Scope:       rec.Scope,
		TokenType:   "Bearer",
	}

	if rt, ok := tokenResp["refresh_token"].(string); ok && rt != "" {
		newRec.RefreshToken = rt
	} else {
		newRec.RefreshToken = rec.RefreshToken
	}

	expiresIn := 3600
	if ei, ok := tokenResp["expires_in"].(float64); ok {
		expiresIn = int(ei)
	}
	newRec.ExpiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)

	return newRec, nil
}

func EnsureFreshToken(ctx context.Context, store *Store, cfg ProviderOAuthConfig, provider, account string, leeway time.Duration) (*TokenRecord, bool, error) {
	rec, err := store.GetToken(ctx, provider, account)
	if err != nil {
		return nil, false, err
	}
	if rec == nil {
		return nil, false, nil
	}
	if !IsExpired(rec, leeway) {
		return rec, false, nil
	}
	refreshed, err := RefreshToken(ctx, cfg, rec)
	if err != nil {
		return nil, false, err
	}
	if refreshed.Provider == "" {
		refreshed.Provider = provider
	}
	if refreshed.Account == "" {
		refreshed.Account = account
	}
	if err := store.SaveToken(ctx, *refreshed); err != nil {
		return nil, false, err
	}
	return refreshed, true, nil
}

// --- Authorization URL builder ---

// BuildAuthURL constructs the authorization URL to redirect users to for consent.
func BuildAuthURL(cfg ProviderOAuthConfig, state string) string {
	params := url.Values{}
	params.Set("client_id", cfg.ClientID)
	params.Set("redirect_uri", cfg.RedirectURL)
	params.Set("response_type", "code")
	params.Set("scope", strings.Join(cfg.Scopes, " "))
	params.Set("state", state)
	params.Set("access_type", "offline")
	params.Set("prompt", "consent")
	return cfg.AuthURL + "?" + params.Encode()
}

// ExchangeCode exchanges an authorization code for tokens.
func ExchangeCode(ctx context.Context, cfg ProviderOAuthConfig, code string) (*TokenRecord, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("redirect_uri", cfg.RedirectURL)
	data.Set("client_id", cfg.ClientID)
	data.Set("client_secret", cfg.ClientSecret)

	req, err := http.NewRequestWithContext(ctx, "POST", cfg.TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("oauth: create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oauth: token exchange request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oauth: token exchange failed (%d): %s", resp.StatusCode, string(body))
	}

	var tokenResp map[string]interface{}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("oauth: parse token response: %w", err)
	}

	accessToken, _ := tokenResp["access_token"].(string)
	if accessToken == "" {
		return nil, fmt.Errorf("oauth: no access_token in response")
	}

	rec := &TokenRecord{
		AccessToken: accessToken,
		TokenType:   "Bearer",
	}
	if rt, ok := tokenResp["refresh_token"].(string); ok {
		rec.RefreshToken = rt
	}
	expiresIn := 3600
	if ei, ok := tokenResp["expires_in"].(float64); ok {
		expiresIn = int(ei)
	}
	rec.ExpiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)
	if scope, ok := tokenResp["scope"].(string); ok {
		rec.Scope = scope
	}

	return rec, nil
}

func ExtractCodexAccountInfo(idToken string) map[string]string {
	info := make(map[string]string)

	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return info
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return info
	}

	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return info
	}

	// Extract email
	if email, ok := claims["email"].(string); ok {
		info["email"] = email
	}

	// Extract OpenAI-specific claims from https://api.openai.com/auth namespace
	if openaiAuth, ok := claims["https://api.openai.com/auth"].(map[string]any); ok {
		if accountId, ok := openaiAuth["chatgpt_account_id"].(string); ok {
			info["chatgptAccountId"] = accountId
		}
		if planType, ok := openaiAuth["chatgpt_plan_type"].(string); ok {
			info["chatgptPlanType"] = planType
		}
	}

	return info
}
