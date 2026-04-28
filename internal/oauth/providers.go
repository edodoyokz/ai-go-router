// Package oauth provides OAuth 2.0 provider configurations and registry.
package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"runtime"
	"strings"
)

func envString(name string) string {
	return strings.TrimSpace(os.Getenv(name))
}

// DefaultProviderRegistry returns a registry with all reference providers pre-registered.
func DefaultProviderRegistry() *ProviderRegistry {
	r := NewProviderRegistry()

	// Claude OAuth (Authorization Code Flow with PKCE)
	r.Register(ProviderOAuthConfig{
		Name:                "claude",
		FlowType:            FlowTypeAuthCodePKCE,
		AuthURL:             "https://claude.ai/oauth/authorize",
		TokenURL:            "https://api.anthropic.com/v1/oauth/token",
		ClientID:            "9d1c250a-e61b-44d9-88ed-5944d1962f5e",
		Scopes:              []string{"org:create_api_key", "user:profile", "user:inference"},
		CodeChallengeMethod: "S256",
		ContentType:         "application/json",
		ExtraAuthParams:     map[string]string{"code": "true"},
	})

	// Codex OAuth (Authorization Code Flow with PKCE)
	r.Register(ProviderOAuthConfig{
		Name:                "codex",
		FlowType:            FlowTypeAuthCodePKCE,
		AuthURL:             "https://auth.openai.com/oauth/authorize",
		TokenURL:            "https://auth.openai.com/oauth/token",
		ClientID:            "app_EMoamEEZ73f0CkXaXp7hrann",
		Scope:               "openid profile email offline_access",
		CodeChallengeMethod: "S256",
		ContentType:         "application/x-www-form-urlencoded",
		FixedPort:           1455,
		CallbackPath:        "/auth/callback",
		ExtraAuthParams: map[string]string{
			"id_token_add_organizations": "true",
			"codex_cli_simplified_flow":  "true",
			"originator":                 "codex_cli_rs",
		},
	})

	// Gemini CLI (Authorization Code Flow)
	r.Register(ProviderOAuthConfig{
		Name:         "gemini-cli",
		FlowType:     FlowTypeAuthCode,
		AuthURL:      "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:     "https://oauth2.googleapis.com/token",
		ClientID:     envString("NINEROUTER_GEMINI_CLI_GOOGLE_CLIENT_ID"),
		ClientSecret: envString("NINEROUTER_GEMINI_CLI_GOOGLE_CLIENT_SECRET"),
		Scopes: []string{
			"https://www.googleapis.com/auth/cloud-platform",
			"https://www.googleapis.com/auth/userinfo.email",
			"https://www.googleapis.com/auth/userinfo.profile",
		},
		UserInfoURL: "https://www.googleapis.com/oauth2/v1/userinfo",
		ContentType: "application/x-www-form-urlencoded",
		Extra: map[string]string{
			"loadCodeAssistEndpoint": "https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist",
		},
	})

	// Qwen OAuth (Device Code Flow with PKCE)
	r.Register(ProviderOAuthConfig{
		Name:                "qwen",
		FlowType:            FlowTypeDeviceCode,
		DeviceCodeURL:       "https://qwen.ai/api/v1/oauth2/device/code",
		TokenURL:            "https://qwen.ai/api/v1/oauth2/token",
		ClientID:            "f0304373b74a44d2b584a3fb70ca9e56",
		Scope:               "openid profile email model.completion",
		CodeChallengeMethod: "S256",
		ContentType:         "application/x-www-form-urlencoded",
	})

	// Qoder OAuth (Authorization Code Flow)
	r.Register(ProviderOAuthConfig{
		Name:         "qoder",
		FlowType:     FlowTypeAuthCode,
		AuthURL:      "https://api2.qoder.sh/api/v1/auth/authorize",
		TokenURL:     "https://api2.qoder.sh/api/v1/auth/token",
		ClientID:     "10009311001",
		ClientSecret: "4Z3YjXycVsQvyGF1etiNlIBB4RsqSDtW",
		UserInfoURL:  "https://api2.qoder.sh/api/v1/userinfo",
		ContentType:  "application/x-www-form-urlencoded",
		Extra: map[string]string{
			"refreshUrl": "https://api2.qoder.sh/api/v3/user/refresh_token",
			"statusUrl":  "https://api2.qoder.sh/api/v3/user/status",
		},
	})

	// iFlow OAuth (Authorization Code Flow)
	r.Register(ProviderOAuthConfig{
		Name:         "iflow",
		FlowType:     FlowTypeAuthCode,
		AuthURL:      "https://iflow.cn/oauth",
		TokenURL:     "https://iflow.cn/oauth/token",
		ClientID:     "10009311001",
		ClientSecret: "4Z3YjXycVsQvyGF1etiNlIBB4RsqSDtW",
		UserInfoURL:  "https://iflow.cn/api/oauth/getUserInfo",
		ContentType:  "application/x-www-form-urlencoded",
		ExtraAuthParams: map[string]string{
			"loginMethod": "phone",
			"type":        "phone",
		},
	})

	// Antigravity OAuth (Authorization Code Flow with Google)
	r.Register(ProviderOAuthConfig{
		Name:         "antigravity",
		FlowType:     FlowTypeAuthCode,
		AuthURL:      "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:     "https://oauth2.googleapis.com/token",
		ClientID:     envString("NINEROUTER_ANTIGRAVITY_GOOGLE_CLIENT_ID"),
		ClientSecret: envString("NINEROUTER_ANTIGRAVITY_GOOGLE_CLIENT_SECRET"),
		Scopes: []string{
			"https://www.googleapis.com/auth/cloud-platform",
			"https://www.googleapis.com/auth/userinfo.email",
			"https://www.googleapis.com/auth/userinfo.profile",
			"https://www.googleapis.com/auth/cclog",
			"https://www.googleapis.com/auth/experimentsandconfigs",
		},
		UserInfoURL: "https://www.googleapis.com/oauth2/v1/userinfo",
		ContentType: "application/x-www-form-urlencoded",
		Extra: map[string]string{
			"loadCodeAssistEndpoint":  "https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist",
			"onboardUserEndpoint":     "https://cloudcode-pa.googleapis.com/v1internal:onboardUser",
			"loadCodeAssistUserAgent": "google-api-nodejs-client/9.15.1",
			"loadCodeAssistApiClient": "google-cloud-sdk vscode_cloudshelleditor/0.1",
		},
	})

	// OpenAI OAuth (Authorization Code Flow with PKCE)
	r.Register(ProviderOAuthConfig{
		Name:                "openai",
		FlowType:            FlowTypeAuthCodePKCE,
		AuthURL:             "https://auth.openai.com/oauth/authorize",
		TokenURL:            "https://auth.openai.com/oauth/token",
		ClientID:            "app_EMoamEEZ73f0CkXaXp7hrann",
		Scope:               "openid profile email offline_access",
		CodeChallengeMethod: "S256",
		ContentType:         "application/x-www-form-urlencoded",
		ExtraAuthParams: map[string]string{
			"id_token_add_organizations": "true",
			"originator":                 "openai_native",
		},
	})

	// GitHub Copilot OAuth (Device Code Flow)
	r.Register(ProviderOAuthConfig{
		Name:                "github",
		FlowType:            FlowTypeDeviceCode,
		DeviceCodeURL:       "https://github.com/login/device/code",
		TokenURL:            "https://github.com/login/oauth/access_token",
		ClientID:            "Iv1.b507a08c87ecfe98",
		Scope:               "read:user",
		ContentType:         "application/x-www-form-urlencoded",
		NoPKCEForDeviceCode: true,
		UserInfoURL:         "https://api.github.com/user",
		Extra: map[string]string{
			"copilotTokenURL": "https://api.github.com/copilot_internal/v2/token",
			"apiVersion":      "2022-11-28",
			"userAgent":       "GitHubCopilotChat/0.26.7",
		},
	})

	// Copilot (alias for GitHub)
	r.Register(ProviderOAuthConfig{
		Name:                "copilot",
		FlowType:            FlowTypeDeviceCode,
		DeviceCodeURL:       "https://github.com/login/device/code",
		TokenURL:            "https://github.com/login/oauth/access_token",
		ClientID:            "Iv1.b507a08c87ecfe98",
		Scope:               "read:user",
		ContentType:         "application/x-www-form-urlencoded",
		NoPKCEForDeviceCode: true,
		UserInfoURL:         "https://api.github.com/user",
		Extra: map[string]string{
			"copilotTokenURL": "https://api.github.com/copilot_internal/v2/token",
			"apiVersion":      "2022-11-28",
			"userAgent":       "GitHubCopilotChat/0.26.7",
		},
	})

	// Kiro OAuth (Device Code Flow - AWS SSO OIDC)
	r.Register(ProviderOAuthConfig{
		Name:                "kiro",
		FlowType:            FlowTypeDeviceCode,
		DeviceCodeURL:       "", // Dynamic based on region
		TokenURL:            "", // Dynamic based on region
		ContentType:         "application/json",
		NoPKCEForDeviceCode: true,
		Extra: map[string]string{
			"ssoOidcEndpoint":    "https://oidc.us-east-1.amazonaws.com",
			"registerClientUrl":  "https://oidc.us-east-1.amazonaws.com/client/register",
			"deviceAuthUrl":      "https://oidc.us-east-1.amazonaws.com/device_authorization",
			"startUrl":           "https://view.awsapps.com/start",
			"clientName":         "kiro-oauth-client",
			"clientType":         "public",
			"issuerUrl":          "https://identitycenter.amazonaws.com/ssoins-722374e8c3c8e6c6",
			"socialAuthEndpoint": "https://prod.us-east-1.auth.desktop.kiro.dev",
			"socialLoginUrl":     "https://prod.us-east-1.auth.desktop.kiro.dev/login",
			"socialTokenUrl":     "https://prod.us-east-1.auth.desktop.kiro.dev/oauth/token",
			"socialRefreshUrl":   "https://prod.us-east-1.auth.desktop.kiro.dev/refreshToken",
		},
	})

	// Cursor OAuth (Import Token Flow)
	r.Register(ProviderOAuthConfig{
		Name:     "cursor",
		FlowType: FlowTypeImportToken,
		Extra: map[string]string{
			"apiEndpoint": "https://api2.cursor.sh",
		},
	})

	// Kimi Coding OAuth (Device Code Flow)
	r.Register(ProviderOAuthConfig{
		Name:                "kimi-coding",
		FlowType:            FlowTypeDeviceCode,
		DeviceCodeURL:       "https://auth.kimi.com/api/oauth/device_authorization",
		TokenURL:            "https://auth.kimi.com/api/oauth/token",
		ClientID:            "17e5f671-d194-4dfb-9706-5516cb48c098",
		ContentType:         "application/x-www-form-urlencoded",
		NoPKCEForDeviceCode: true,
	})

	// KiloCode OAuth (Custom Device Auth Flow)
	r.Register(ProviderOAuthConfig{
		Name:                "kilocode",
		FlowType:            FlowTypeDeviceCode,
		DeviceCodeURL:       "https://api.kilo.ai/api/device-auth/codes",
		TokenURL:            "", // Dynamic: {apiBaseUrl}/api/device-auth/codes/{deviceCode}
		ContentType:         "application/json",
		NoPKCEForDeviceCode: true,
		Extra: map[string]string{
			"apiBaseUrl":   "https://api.kilo.ai",
			"pollUrlBase":  "https://api.kilo.ai/api/device-auth/codes",
			"pollInterval": "3000",
		},
	})

	// Cline OAuth (Authorization Code Flow)
	r.Register(ProviderOAuthConfig{
		Name:        "cline",
		FlowType:    FlowTypeAuthCode,
		AuthURL:     "https://api.cline.bot/api/v1/auth/authorize",
		TokenURL:    "https://api.cline.bot/api/v1/auth/token",
		ContentType: "application/json",
		ExtraAuthParams: map[string]string{
			"client_type": "extension",
		},
		Extra: map[string]string{
			"refreshUrl": "https://api.cline.bot/api/v1/auth/refresh",
		},
	})

	// GitLab OAuth (Authorization Code Flow with PKCE)
	r.Register(ProviderOAuthConfig{
		Name:                "gitlab",
		FlowType:            FlowTypeAuthCodePKCE,
		AuthURL:             "", // Dynamic: {baseUrl}/oauth/authorize
		TokenURL:            "", // Dynamic: {baseUrl}/oauth/token
		CodeChallengeMethod: "S256",
		ContentType:         "application/x-www-form-urlencoded",
		UserInfoURL:         "", // Dynamic: {baseUrl}/api/v4/user
		Extra: map[string]string{
			"defaultBaseUrl":   "https://gitlab.com",
			"authorizeUrlPath": "/oauth/authorize",
			"tokenUrlPath":     "/oauth/token",
			"userInfoUrlPath":  "/api/v4/user",
			"scope":            "api read_user",
		},
	})

	// CodeBuddy OAuth (Device Code Flow - Tencent)
	r.Register(ProviderOAuthConfig{
		Name:                "codebuddy",
		FlowType:            FlowTypeDeviceCode,
		DeviceCodeURL:       "https://copilot.tencent.com/v2/plugin/auth/state",
		TokenURL:            "https://copilot.tencent.com/v2/plugin/auth/token",
		ContentType:         "application/json",
		NoPKCEForDeviceCode: true,
		Extra: map[string]string{
			"baseUrl":      "https://copilot.tencent.com",
			"refreshUrl":   "https://copilot.tencent.com/v2/plugin/auth/token/refresh",
			"userAgent":    "CLI/2.63.2 CodeBuddy/2.63.2",
			"platform":     "CLI",
			"pollInterval": "5000",
		},
	})

	return r
}

// getOAuthPlatformEnum returns the platform enum value for Antigravity.
func getOAuthPlatformEnum() int {
	switch runtime.GOOS {
	case "darwin":
		if runtime.GOARCH == "arm64" {
			return 2
		}
		return 1
	case "linux":
		if runtime.GOARCH == "arm64" {
			return 4
		}
		return 3
	case "windows":
		return 5
	default:
		return 0
	}
}

// GeneratePKCEPair generates a PKCE code verifier and challenge.
func GeneratePKCEPair() (string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	verifier := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

// GenerateStateToken generates a random state token.
func GenerateStateToken() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// BuildAuthorizeURL constructs the authorization URL for a provider.
func BuildAuthorizeURL(cfg ProviderOAuthConfig, redirectURI, state, codeChallenge string, meta map[string]string) (string, error) {
	switch cfg.FlowType {
	case FlowTypeDeviceCode, FlowTypeImportToken:
		return "", fmt.Errorf("provider %s does not use authorization URL (flow type: %s)", cfg.Name, cfg.FlowType)
	}

	authURL := cfg.AuthURL

	// Handle GitLab dynamic baseUrl
	if cfg.Name == "gitlab" {
		baseURL := ""
		if meta != nil {
			baseURL = meta["baseUrl"]
		}
		if baseURL == "" {
			baseURL = cfg.Extra["defaultBaseUrl"]
		}
		authURL = baseURL + cfg.Extra["authorizeUrlPath"]
	}

	params := url.Values{}

	// Set standard params
	params.Set("response_type", "code")

	if cfg.ClientID != "" {
		params.Set("client_id", cfg.ClientID)
	}

	// GitLab uses dynamic clientId from meta
	if cfg.Name == "gitlab" {
		if meta != nil && meta["clientId"] != "" {
			params.Set("client_id", meta["clientId"])
		}
	}

	params.Set("redirect_uri", redirectURI)
	params.Set("state", state)

	// PKCE params
	if cfg.FlowType == FlowTypeAuthCodePKCE {
		params.Set("code_challenge", codeChallenge)
		params.Set("code_challenge_method", cfg.CodeChallengeMethod)
	}

	// Scopes
	if len(cfg.Scopes) > 0 {
		params.Set("scope", strings.Join(cfg.Scopes, " "))
	} else if cfg.Scope != "" {
		params.Set("scope", cfg.Scope)
	} else if cfg.Name == "gitlab" {
		params.Set("scope", cfg.Extra["scope"])
	}

	// Access type and prompt for Google OAuth
	if cfg.Name == "gemini-cli" || cfg.Name == "antigravity" {
		params.Set("access_type", "offline")
		params.Set("prompt", "consent")
	}

	// Provider-specific extra params
	for k, v := range cfg.ExtraAuthParams {
		params.Set(k, v)
	}

	// Meta params (for GitLab clientId/baseUrl, etc.)
	if meta != nil {
		reserved := map[string]bool{"baseUrl": true, "clientId": true, "clientSecret": true}
		for k, v := range meta {
			if !reserved[k] {
				params.Set(k, v)
			}
		}
	}

	return authURL + "?" + params.Encode(), nil
}

// ExtractEmailFromJWT extracts email from a JWT access token.
func ExtractEmailFromJWT(accessToken string) string {
	parts := strings.Split(accessToken, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	email := ""
	if e, ok := claims["email"].(string); ok {
		email = e
	}
	if email == "" {
		if e, ok := claims["preferred_username"].(string); ok {
			email = e
		}
	}
	if email == "" {
		if s, ok := claims["sub"].(string); ok {
			email = s
		}
	}
	return email
}
