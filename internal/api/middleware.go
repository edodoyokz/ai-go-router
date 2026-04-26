package api

import (
	"context"
	"crypto/subtle"
	"net/http"
	"time"

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
					w.Header().Set("Access-Control-Max-Age", string(rune(maxAge)))
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
