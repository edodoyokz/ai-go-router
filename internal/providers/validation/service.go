package validation

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/edodoyokz/ai-go-router/internal/config"
	"github.com/edodoyokz/ai-go-router/internal/providers/catalog"
	"github.com/edodoyokz/ai-go-router/internal/providers/endpoints"
)

type Result struct {
	Valid             bool     `json:"valid"`
	Models            []string `json:"models"`
	Error             string   `json:"error,omitempty"`
	NormalizedBaseURL string   `json:"normalized_base_url,omitempty"`
	Status            int      `json:"status,omitempty"`
	LatencyMs         int64    `json:"latency_ms,omitempty"`
}

type Service struct {
	httpClient *http.Client
	mu         sync.Mutex
	cache      map[string]cachedModels
	ttl        time.Duration
}

type cachedModels struct {
	models  []string
	expires time.Time
}

func NewService() *Service {
	return &Service{httpClient: &http.Client{Timeout: 20 * time.Second}, cache: make(map[string]cachedModels), ttl: 2 * time.Minute}
}

func NormalizeBaseURL(provider config.ProviderConfig) string {
	return endpoints.NormalizeBaseURL(provider.BaseURL)
}

func (s *Service) ValidateProvider(ctx context.Context, provider config.ProviderConfig) Result {
	start := time.Now()
	res := Result{Valid: false, Models: []string{}}
	normalized := NormalizeBaseURL(provider)
	res.NormalizedBaseURL = normalized
	if normalized == "" {
		res.Error = "base_url is required"
		return res
	}

	modelURL, headerAuth := s.modelsEndpoint(provider, normalized)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelURL, nil)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	for k, v := range provider.Headers {
		req.Header.Set(k, v)
	}
	if headerAuth != "" {
		req.Header.Set("Authorization", headerAuth)
	}
	if strings.Contains(provider.Type, "anthropic") {
		if key := s.resolveAPIKey(provider); key != "" {
			req.Header.Set("x-api-key", key)
		}
		if req.Header.Get("anthropic-version") == "" {
			req.Header.Set("anthropic-version", "2023-06-01")
		}
	}

	httpRes, err := s.httpClient.Do(req)
	if err != nil {
		res.Error = classifyNetworkError(err)
		res.LatencyMs = time.Since(start).Milliseconds()
		return res
	}
	defer httpRes.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(httpRes.Body, 2<<20))

	res.Status = httpRes.StatusCode
	res.LatencyMs = time.Since(start).Milliseconds()
	if httpRes.StatusCode < 200 || httpRes.StatusCode >= 300 {
		res.Error = fmt.Sprintf("status %d: %s", httpRes.StatusCode, strings.TrimSpace(string(body)))
		return res
	}

	res.Valid = true
	res.Models = extractModels(body)
	return res
}

func (s *Service) SuggestedModels(ctx context.Context, providerType, url string, forceRefresh bool) Result {
	provider := config.ProviderConfig{Type: providerType, BaseURL: url}
	if def, ok := catalog.ResolveAlias(providerType); ok {
		provider.Type = def.ID
		if strings.TrimSpace(provider.BaseURL) == "" {
			provider.BaseURL = def.DefaultBaseURL
		}
	}
	key := provider.Type + "|" + NormalizeBaseURL(provider)

	if !forceRefresh {
		s.mu.Lock()
		entry, ok := s.cache[key]
		if ok && time.Now().Before(entry.expires) {
			models := make([]string, len(entry.models))
			copy(models, entry.models)
			s.mu.Unlock()
			return Result{Valid: true, Models: models, NormalizedBaseURL: NormalizeBaseURL(provider)}
		}
		s.mu.Unlock()
	}

	res := s.ValidateProvider(ctx, provider)
	if res.Valid {
		s.mu.Lock()
		s.cache[key] = cachedModels{models: res.Models, expires: time.Now().Add(s.ttl)}
		s.mu.Unlock()
	}
	return res
}

func classifyNetworkError(err error) string {
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return "network timeout"
	}
	errText := strings.ToLower(err.Error())
	switch {
	case strings.Contains(errText, "no such host"):
		return "dns lookup failed"
	case strings.Contains(errText, "connection refused"):
		return "connection refused"
	case strings.Contains(errText, "tls"):
		return "tls handshake failed"
	default:
		return err.Error()
	}
}

func (s *Service) modelsEndpoint(provider config.ProviderConfig, normalized string) (string, string) {
	if provider.Type == "openrouter" || strings.Contains(normalized, "openrouter.ai") {
		return "https://openrouter.ai/api/v1/models", bearer(s.resolveAPIKey(provider))
	}
	if strings.Contains(provider.Type, "anthropic") || strings.Contains(normalized, "anthropic.com") {
		return normalized + "/v1/models", ""
	}
	if strings.Contains(normalized, "/v1") {
		return normalized + "/models", bearer(s.resolveAPIKey(provider))
	}
	return normalized + "/v1/models", bearer(s.resolveAPIKey(provider))
}

func (s *Service) resolveAPIKey(provider config.ProviderConfig) string {
	if provider.APIKey != "" {
		return provider.APIKey
	}
	if len(provider.Accounts) > 0 {
		if provider.Accounts[0].APIKey != "" {
			return provider.Accounts[0].APIKey
		}
		if provider.Accounts[0].AccessToken != "" {
			return provider.Accounts[0].AccessToken
		}
	}
	return ""
}

func bearer(token string) string {
	if token == "" {
		return ""
	}
	return "Bearer " + token
}

func extractModels(body []byte) []string {
	var data struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &data); err == nil && len(data.Data) > 0 {
		models := make([]string, 0, len(data.Data))
		for _, m := range data.Data {
			if strings.TrimSpace(m.ID) != "" {
				models = append(models, m.ID)
			}
		}
		return models
	}

	var arr []string
	if err := json.Unmarshal(body, &arr); err == nil {
		return arr
	}
	return []string{}
}
