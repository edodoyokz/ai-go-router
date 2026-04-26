package api

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/edodoyokz/ai-go-router/internal/config"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type contextKey string

const (
	requestIDKey contextKey = "request_id"
)

// RequestIDMiddleware generates a unique request ID for each request
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := uuid.New().String()
		ctx := context.WithValue(r.Context(), requestIDKey, requestID)
		w.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetRequestID retrieves the request ID from context
func GetRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

// PanicRecoveryMiddleware recovers from panics and returns 500 error
func PanicRecoveryMiddleware(logger zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					requestID := GetRequestID(r.Context())
					logger.Error().
						Str("request_id", requestID).
						Str("method", r.Method).
						Str("path", r.URL.Path).
						Interface("panic", err).
						Msg("panic recovered")

					writeJSON(w, http.StatusInternalServerError, map[string]any{
						"error": map[string]any{
							"message": "internal server error",
							"type":    "internal_error",
						},
					})
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// AuthMiddleware validates API key using constant-time comparison
func AuthMiddleware(apiKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if header == "" {
				writeJSON(w, http.StatusUnauthorized, map[string]any{
					"error": map[string]any{
						"message": "missing authorization header",
						"type":    "invalid_request_error",
					},
				})
				return
			}

			// Expected format: "Bearer <api_key>"
			expected := "Bearer " + apiKey

			// Use constant-time comparison to prevent timing attacks
			if len(header) != len(expected) || subtle.ConstantTimeCompare([]byte(header), []byte(expected)) != 1 {
				writeJSON(w, http.StatusUnauthorized, map[string]any{
					"error": map[string]any{
						"message": "invalid api key",
						"type":    "invalid_request_error",
					},
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// AuthMiddlewareWithRuntimeConfig validates API key against runtime-configured keys.
func AuthMiddlewareWithRuntimeConfig(runtimeCfg *config.RuntimeConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			validKeys := runtimeCfg.ListAdminAPIKeys()
			if len(validKeys) == 0 {
				// If no API keys are configured, auth is disabled (local dev mode)
				next.ServeHTTP(w, r)
				return
			}

			header := r.Header.Get("Authorization")
			if header == "" {
				writeJSON(w, http.StatusUnauthorized, map[string]any{
					"error": map[string]any{
						"message": "missing authorization header",
						"type":    "invalid_request_error",
					},
				})
				return
			}

			if !strings.HasPrefix(header, "Bearer ") {
				writeJSON(w, http.StatusUnauthorized, map[string]any{
					"error": map[string]any{
						"message": "invalid api key",
						"type":    "invalid_request_error",
					},
				})
				return
			}

			provided := strings.TrimPrefix(header, "Bearer ")
			for _, key := range validKeys {
				if len(provided) == len(key) && subtle.ConstantTimeCompare([]byte(provided), []byte(key)) == 1 {
					next.ServeHTTP(w, r)
					return
				}
			}

			writeJSON(w, http.StatusUnauthorized, map[string]any{
				"error": map[string]any{
					"message": "invalid api key",
					"type":    "invalid_request_error",
				},
			})
		})
	}
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
}

func (rw *responseWriter) WriteHeader(code int) {
	if !rw.written {
		rw.statusCode = code
		rw.written = true
		rw.ResponseWriter.WriteHeader(code)
	}
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.written {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(b)
}

// StructuredLoggingMiddleware logs request details with status code and latency
func StructuredLoggingMiddleware(logger zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			wrapped := newResponseWriter(w)
			requestID := GetRequestID(r.Context())

			next.ServeHTTP(wrapped, r)

			logger.Info().
				Str("request_id", requestID).
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Int("status", wrapped.statusCode).
				Dur("duration_ms", time.Since(started)).
				Msg("request completed")
		})
	}
}

// SecurityHeadersMiddleware adds security headers for production hardening
func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")

		next.ServeHTTP(w, r)
	})
}

// CORSMiddleware handles Cross-Origin Resource Sharing
// If allowedOrigins is empty, CORS is disabled (no CORS headers set)
func CORSMiddleware(allowedOrigins []string, allowedMethods []string, allowedHeaders []string, allowCredentials bool, maxAge int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// If no origins allowed, skip CORS headers
			if len(allowedOrigins) == 0 {
				next.ServeHTTP(w, r)
				return
			}

			origin := r.Header.Get("Origin")

			// Check if origin is allowed
			allowed := false
			for _, allowedOrigin := range allowedOrigins {
				if allowedOrigin == "*" || allowedOrigin == origin {
					allowed = true
					break
				}
			}

			if allowed {
				// Set CORS headers
				w.Header().Set("Access-Control-Allow-Origin", origin)
				if allowCredentials {
					w.Header().Set("Access-Control-Allow-Credentials", "true")
				}

				if len(allowedMethods) > 0 {
					methodsStr := ""
					for i, method := range allowedMethods {
						if i > 0 {
							methodsStr += ", "
						}
						methodsStr += method
					}
					w.Header().Set("Access-Control-Allow-Methods", methodsStr)
				}

				if len(allowedHeaders) > 0 {
					headersStr := ""
					for i, header := range allowedHeaders {
						if i > 0 {
							headersStr += ", "
						}
						headersStr += header
					}
					w.Header().Set("Access-Control-Allow-Headers", headersStr)
				}

				if maxAge > 0 {
					w.Header().Set("Access-Control-Max-Age", strconv.Itoa(maxAge))
				}

				// Handle preflight requests
				if r.Method == "OPTIONS" {
					w.WriteHeader(http.StatusNoContent)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RateLimiter implements token bucket rate limiting per API key
type RateLimiter struct {
	mu          sync.RWMutex
	limiters    map[string]*tokenBucket
	requests    int           // max requests per window
	window      time.Duration // time window
	cleanup     time.Duration
	lastCleanup time.Time
}

type tokenBucket struct {
	tokens     int
	lastRefill time.Time
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(requests int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		limiters:    make(map[string]*tokenBucket),
		requests:    requests,
		window:      window,
		cleanup:     window * 2,
		lastCleanup: time.Now(),
	}
}

// Allow checks if a request from the given key is allowed
func (rl *RateLimiter) Allow(key string) bool {
	now := time.Now()

	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Periodic cleanup of old entries
	if now.Sub(rl.lastCleanup) > rl.cleanup {
		for k, bucket := range rl.limiters {
			if now.Sub(bucket.lastRefill) > rl.cleanup {
				delete(rl.limiters, k)
			}
		}
		rl.lastCleanup = now
	}

	bucket, exists := rl.limiters[key]
	if !exists {
		bucket = &tokenBucket{
			tokens:     rl.requests - 1,
			lastRefill: now,
		}
		rl.limiters[key] = bucket
		return true
	}

	// Refill tokens based on elapsed time
	elapsed := now.Sub(bucket.lastRefill)
	if elapsed >= rl.window {
		bucket.tokens = rl.requests - 1
		bucket.lastRefill = now
		return true
	}

	// Check if tokens available
	if bucket.tokens > 0 {
		bucket.tokens--
		return true
	}

	return false
}

// RateLimitMiddleware applies rate limiting per API key
func RateLimitMiddleware(limiter *RateLimiter, runtimeCfg *config.RuntimeConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract API key from authorization header
			header := r.Header.Get("Authorization")
			if header == "" {
				writeJSON(w, http.StatusUnauthorized, map[string]any{
					"error": map[string]any{
						"message": "missing authorization header",
						"type":    "invalid_request_error",
					},
				})
				return
			}

			if !strings.HasPrefix(header, "Bearer ") {
				writeJSON(w, http.StatusUnauthorized, map[string]any{
					"error": map[string]any{
						"message": "invalid api key",
						"type":    "invalid_request_error",
					},
				})
				return
			}

			apiKey := strings.TrimPrefix(header, "Bearer ")

			// Check rate limit
			if !limiter.Allow(apiKey) {
				writeJSON(w, http.StatusTooManyRequests, map[string]any{
					"error": map[string]any{
						"message": "rate limit exceeded",
						"type":    "rate_limit_error",
					},
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// ToolType represents the detected AI tool/client
type ToolType string

const (
	ToolUnknown   ToolType = "unknown"
	ToolCopilot   ToolType = "github-copilot"
	ToolCursor    ToolType = "cursor"
	ToolContinue  ToolType = "continue"
	ToolVSCode    ToolType = "vscode"
	ToolOpenAI    ToolType = "openai"
	ToolAnthropic ToolType = "anthropic"
	ToolCLI       ToolType = "cli"
	ToolCustom    ToolType = "custom"
)

const (
	toolKey contextKey = "tool"
)

// ToolDetectionMiddleware detects the client tool based on headers, user-agent, and request body
func ToolDetectionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tool := detectTool(r)
		ctx := context.WithValue(r.Context(), toolKey, tool)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetTool retrieves the detected tool from context
func GetTool(ctx context.Context) ToolType {
	if tool, ok := ctx.Value(toolKey).(ToolType); ok {
		return tool
	}
	return ToolUnknown
}

// detectTool identifies the client tool based on request characteristics
func detectTool(r *http.Request) ToolType {
	userAgent := r.Header.Get("User-Agent")
	xClientName := r.Header.Get("X-Client-Name")

	// Check explicit client headers
	if xClientName != "" {
		switch strings.ToLower(xClientName) {
		case "cursor":
			return ToolCursor
		case "continue":
			return ToolContinue
		case "github-copilot", "copilot":
			return ToolCopilot
		case "vscode":
			return ToolVSCode
		}
	}

	// Check User-Agent
	ua := strings.ToLower(userAgent)
	if strings.Contains(ua, "cursor") {
		return ToolCursor
	}
	if strings.Contains(ua, "copilot") || strings.Contains(ua, "github") {
		return ToolCopilot
	}
	if strings.Contains(ua, "continue") {
		return ToolContinue
	}
	if strings.Contains(ua, "vscode") || strings.Contains(ua, "code") {
		return ToolVSCode
	}
	if strings.Contains(ua, "anthropic") {
		return ToolAnthropic
	}
	if strings.Contains(ua, "openai") {
		return ToolOpenAI
	}
	if strings.Contains(ua, "curl") || strings.Contains(ua, "wget") {
		return ToolCLI
	}

	return ToolUnknown
}
