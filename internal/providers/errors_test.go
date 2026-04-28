package providers

import (
	"net/http"
	"testing"
	"time"

	"github.com/edodoyokz/ai-go-router/internal/config"
)

func TestClassifyHTTPError(t *testing.T) {
	errorConfig := config.ErrorConfig{
		TextRules: []config.ErrorTextRule{
			{Text: "rate limit exceeded", Backoff: true, CooldownMs: 60000},
			{Text: "quota exceeded", Backoff: true, CooldownMs: 120000},
		},
		StatusRules: []config.ErrorStatusRule{
			{Status: 429, Backoff: true, CooldownMs: 30000},
			{Status: 500, Backoff: true, CooldownMs: 5000},
		},
	}

	tests := []struct {
		name          string
		statusCode    int
		message       string
		wantRetryable bool
		wantType      error
	}{
		{
			name:          "429 rate limit",
			statusCode:    http.StatusTooManyRequests,
			message:       "rate limit exceeded",
			wantRetryable: true,
			wantType:      ErrRetryable, // Status rule takes precedence
		},
		{
			name:          "429 with status rule",
			statusCode:    http.StatusTooManyRequests,
			message:       "",
			wantRetryable: true,
			wantType:      ErrRetryable, // Status rule takes precedence
		},
		{
			name:          "503 service unavailable",
			statusCode:    http.StatusServiceUnavailable,
			message:       "service unavailable",
			wantRetryable: true,
			wantType:      ErrProviderUnavailable,
		},
		{
			name:          "502 bad gateway",
			statusCode:    http.StatusBadGateway,
			message:       "bad gateway",
			wantRetryable: true,
			wantType:      ErrRetryable,
		},
		{
			name:          "504 gateway timeout",
			statusCode:    http.StatusGatewayTimeout,
			message:       "gateway timeout",
			wantRetryable: true,
			wantType:      ErrRetryable,
		},
		{
			name:          "401 unauthorized",
			statusCode:    http.StatusUnauthorized,
			message:       "invalid api key",
			wantRetryable: true,
			wantType:      ErrAuthExpiredRefreshable,
		},
		{
			name:          "403 forbidden",
			statusCode:    http.StatusForbidden,
			message:       "access denied",
			wantRetryable: false,
			wantType:      ErrAuthInvalid,
		},
		{
			name:          "400 bad request",
			statusCode:    http.StatusBadRequest,
			message:       "invalid request",
			wantRetryable: false,
			wantType:      ErrInvalidRequest,
		},
		{
			name:          "404 not found",
			statusCode:    http.StatusNotFound,
			message:       "model not found",
			wantRetryable: false,
			wantType:      ErrUnsupportedModel,
		},
		{
			name:          "500 server error",
			statusCode:    http.StatusInternalServerError,
			message:       "internal server error",
			wantRetryable: true,
			wantType:      ErrRetryable,
		},
		{
			name:          "text rule match - quota",
			statusCode:    http.StatusOK,
			message:       "quota exceeded for this account",
			wantRetryable: true,
			wantType:      ErrRetryable,
		},
		{
			name:          "text rule match - rate limit",
			statusCode:    http.StatusOK,
			message:       "rate limit exceeded please try again",
			wantRetryable: true,
			wantType:      ErrRetryable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ClassifyHTTPError(tt.statusCode, "test-provider", "test-model", tt.message, errorConfig)

			provErr, ok := err.(*ProviderError)
			if !ok {
				t.Fatalf("ClassifyHTTPError() returned non-ProviderError: %T", err)
			}

			if provErr.Provider != "test-provider" {
				t.Errorf("Provider = %v, want test-provider", provErr.Provider)
			}
			if provErr.Model != "test-model" {
				t.Errorf("Model = %v, want test-model", provErr.Model)
			}

			isRetryable := IsRetryable(err)
			if isRetryable != tt.wantRetryable {
				t.Errorf("IsRetryable() = %v, want %v", isRetryable, tt.wantRetryable)
			}

			// Check error type
			if tt.wantType != nil && !isErrorType(err, tt.wantType) {
				t.Errorf("Error type = %v, want %v", provErr.Type, tt.wantType)
			}
		})
	}
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "retryable error",
			err:  NewRetryableError("test", "model", "network error", nil),
			want: true,
		},
		{
			name: "quota exhausted",
			err:  NewQuotaExhaustedError("test", "model", "quota exceeded"),
			want: true,
		},
		{
			name: "provider unavailable",
			err:  NewProviderUnavailableError("test", "model", "service unavailable", nil),
			want: true,
		},
		{
			name: "non-retryable error",
			err:  NewNonRetryableError("test", "model", "bad request", nil),
			want: false,
		},
		{
			name: "unauthorized",
			err: &ProviderError{
				Provider: "test",
				Model:    "model",
				Type:     ErrProviderUnauthorized,
				Message:  "auth failed",
			},
			want: false,
		},
		{
			name: "unsupported model",
			err: &ProviderError{
				Provider: "test",
				Model:    "model",
				Type:     ErrUnsupportedModel,
				Message:  "model not found",
			},
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "non-provider error",
			err:  &struct{ error }{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsRetryable(tt.err)
			if got != tt.want {
				t.Errorf("IsRetryable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsQuotaExhausted(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "quota exhausted",
			err:  NewQuotaExhaustedError("test", "model", "quota exceeded"),
			want: true,
		},
		{
			name: "retryable error",
			err:  NewRetryableError("test", "model", "network error", nil),
			want: false,
		},
		{
			name: "non-retryable error",
			err:  NewNonRetryableError("test", "model", "bad request", nil),
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsQuotaExhausted(tt.err)
			if got != tt.want {
				t.Errorf("IsQuotaExhausted() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCooldownTracker(t *testing.T) {
	tracker := NewCooldownTracker()

	t.Run("initial state not in cooldown", func(t *testing.T) {
		if tracker.IsInCooldown("test-provider", "") {
			t.Error("New provider should not be in cooldown")
		}
	})

	t.Run("set and check cooldown", func(t *testing.T) {
		tracker.SetCooldown("test-provider", "", 1*time.Second)
		if !tracker.IsInCooldown("test-provider", "") {
			t.Error("Provider should be in cooldown after SetCooldown")
		}
	})

	t.Run("get cooldown remaining", func(t *testing.T) {
		tracker.SetCooldown("test-provider", "", 5*time.Second)
		remaining := tracker.GetCooldownRemaining("test-provider", "")
		if remaining <= 0 {
			t.Error("Cooldown remaining should be positive")
		}
		if remaining > 6*time.Second {
			t.Error("Cooldown remaining should be less than 6 seconds")
		}
	})

	t.Run("clear cooldown", func(t *testing.T) {
		tracker.SetCooldown("test-provider", "", 1*time.Second)
		tracker.ClearCooldown("test-provider", "")
		if tracker.IsInCooldown("test-provider", "") {
			t.Error("Provider should not be in cooldown after ClearCooldown")
		}
	})

	t.Run("backoff level", func(t *testing.T) {
		tracker.SetCooldown("test-provider", "", 1*time.Second)
		level := tracker.GetBackoffLevel("test-provider", "")
		if level != 1 {
			t.Errorf("Backoff level = %d, want 1", level)
		}

		tracker.SetCooldown("test-provider", "", 1*time.Second)
		level = tracker.GetBackoffLevel("test-provider", "")
		if level != 2 {
			t.Errorf("Backoff level = %d, want 2", level)
		}

		tracker.ResetBackoffLevel("test-provider", "")
		level = tracker.GetBackoffLevel("test-provider", "")
		if level != 0 {
			t.Errorf("Backoff level = %d, want 0 after reset", level)
		}
	})

	t.Run("model lock", func(t *testing.T) {
		if tracker.IsModelLocked("test-provider", "", "test-model") {
			t.Error("Model should not be locked initially")
		}

		tracker.SetModelLock("test-provider", "", "test-model", 1*time.Second)
		if !tracker.IsModelLocked("test-provider", "", "test-model") {
			t.Error("Model should be locked after SetModelLock")
		}

		remaining := tracker.GetModelLockRemaining("test-provider", "", "test-model")
		if remaining <= 0 {
			t.Error("Lock remaining should be positive")
		}
		if remaining > 2*time.Second {
			t.Error("Lock remaining should be less than 2 seconds")
		}

		tracker.ClearModelLock("test-provider", "", "test-model")
		if tracker.IsModelLocked("test-provider", "", "test-model") {
			t.Error("Model should not be locked after ClearModelLock")
		}
	})

	t.Run("model lock with expiry", func(t *testing.T) {
		tracker.SetModelLock("test-provider", "", "test-model", 100*time.Millisecond)
		if !tracker.IsModelLocked("test-provider", "", "test-model") {
			t.Error("Model should be locked immediately")
		}

		time.Sleep(150 * time.Millisecond)
		if tracker.IsModelLocked("test-provider", "", "test-model") {
			t.Error("Model lock should have expired")
		}
	})

	t.Run("different models have independent locks", func(t *testing.T) {
		tracker.SetModelLock("test-provider", "", "model-a", 1*time.Second)
		if tracker.IsModelLocked("test-provider", "", "model-b") {
			t.Error("model-b should not be locked when only model-a is locked")
		}
	})
}

func isErrorType(err error, target error) bool {
	provErr, ok := err.(*ProviderError)
	if !ok {
		return false
	}
	return provErr.Type == target
}
