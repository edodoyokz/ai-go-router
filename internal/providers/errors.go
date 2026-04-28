package providers

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/edodoyokz/ai-go-router/internal/config"
)

// Error types for provider operations
var (
	// ErrRetryable indicates a temporary failure that may succeed on retry
	ErrRetryable = errors.New("retryable error")

	// ErrNonRetryable indicates a permanent failure that won't succeed on retry
	ErrNonRetryable = errors.New("non-retryable error")

	// ErrQuotaExhausted indicates the provider quota is exhausted
	ErrQuotaExhausted = errors.New("quota exhausted")

	// ErrProviderUnauthorized indicates provider authentication failed
	ErrProviderUnauthorized = errors.New("provider unauthorized")

	ErrAuthExpiredRefreshable = errors.New("auth expired refreshable")

	ErrAuthInvalid = errors.New("auth invalid")

	ErrRateLimited = errors.New("rate limited")

	ErrInvalidRequest = errors.New("invalid request")

	ErrInvalidToolSchema = errors.New("invalid tool schema")

	// ErrProviderUnavailable indicates the provider is temporarily unavailable
	ErrProviderUnavailable = errors.New("provider unavailable")

	// ErrInvalidUpstreamResponse indicates the provider returned malformed data
	ErrInvalidUpstreamResponse = errors.New("invalid upstream response")

	// ErrUnsupportedModel indicates the requested model is not supported
	ErrUnsupportedModel = errors.New("unsupported model")

	// ErrModelLocked indicates the requested model is temporarily locked
	ErrModelLocked = errors.New("model locked")
)

// ProviderError wraps provider-specific errors with classification
type ProviderError struct {
	Provider string
	Model    string
	Type     error // One of the Err* types above
	Message  string
	Cause    error
}

func (e *ProviderError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s/%s: %s: %s: %v", e.Provider, e.Model, e.Type, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s/%s: %s: %s", e.Provider, e.Model, e.Type, e.Message)
}

func (e *ProviderError) Unwrap() error {
	return e.Cause
}

// IsRetryable checks if an error should trigger retry/fallback
func IsRetryable(err error) bool {
	var provErr *ProviderError
	if errors.As(err, &provErr) {
		return errors.Is(provErr.Type, ErrRetryable) ||
			errors.Is(provErr.Type, ErrQuotaExhausted) ||
			errors.Is(provErr.Type, ErrRateLimited) ||
			errors.Is(provErr.Type, ErrAuthExpiredRefreshable) ||
			errors.Is(provErr.Type, ErrProviderUnavailable)
	}
	return false
}

// IsQuotaExhausted checks if an error is a quota exhausted error
func IsQuotaExhausted(err error) bool {
	var provErr *ProviderError
	if errors.As(err, &provErr) {
		return errors.Is(provErr.Type, ErrQuotaExhausted) || errors.Is(provErr.Type, ErrRateLimited)
	}
	return false
}

func IsAuthFailure(err error) bool {
	var provErr *ProviderError
	if errors.As(err, &provErr) {
		return errors.Is(provErr.Type, ErrProviderUnauthorized) ||
			errors.Is(provErr.Type, ErrAuthExpiredRefreshable) ||
			errors.Is(provErr.Type, ErrAuthInvalid)
	}
	return false
}

// IsUnsupportedModel checks if an error is an unsupported model error
func IsUnsupportedModel(err error) bool {
	var provErr *ProviderError
	if errors.As(err, &provErr) {
		return errors.Is(provErr.Type, ErrUnsupportedModel)
	}
	return false
}

func classifyBadRequest(message string) error {
	lower := strings.ToLower(message)
	if strings.Contains(lower, "tool") && (strings.Contains(lower, "schema") || strings.Contains(lower, "invalid")) {
		return ErrInvalidToolSchema
	}
	return ErrInvalidRequest
}

// ClassifyHTTPError maps HTTP status codes to error types with config-driven rules
func ClassifyHTTPError(statusCode int, provider, model, message string, errorConfig config.ErrorConfig) error {
	var errType error
	var msg string

	// Check config-driven status rules first
	for _, rule := range errorConfig.StatusRules {
		if rule.Status == statusCode {
			if rule.Backoff {
				errType = ErrRetryable
			} else {
				errType = ErrNonRetryable
			}
			msg = message
			if msg == "" {
				msg = fmt.Sprintf("status %d", statusCode)
			}
			return &ProviderError{
				Provider: provider,
				Model:    model,
				Type:     errType,
				Message:  msg,
			}
		}
	}

	// Check config-driven text rules
	for _, rule := range errorConfig.TextRules {
		if message != "" && strings.Contains(strings.ToLower(message), strings.ToLower(rule.Text)) {
			if rule.Backoff {
				errType = ErrRetryable
			} else {
				errType = ErrNonRetryable
			}
			msg = message
			return &ProviderError{
				Provider: provider,
				Model:    model,
				Type:     errType,
				Message:  msg,
			}
		}
	}

	// Fall back to default status code classification
	switch {
	case statusCode == http.StatusTooManyRequests: // 429
		errType = ErrRateLimited
		msg = "rate limit exceeded"
	case statusCode == http.StatusServiceUnavailable: // 503
		errType = ErrProviderUnavailable
		msg = "service unavailable"
	case statusCode == http.StatusBadGateway: // 502
		errType = ErrRetryable
		msg = "bad gateway"
	case statusCode == http.StatusGatewayTimeout: // 504
		errType = ErrRetryable
		msg = "gateway timeout"
	case statusCode == http.StatusUnauthorized: // 401
		errType = ErrAuthExpiredRefreshable
		msg = "authentication failed"
	case statusCode == http.StatusForbidden: // 403
		errType = ErrAuthInvalid
		msg = "forbidden"
	case statusCode == http.StatusBadRequest: // 400
		errType = classifyBadRequest(message)
		msg = "bad request"
	case statusCode == http.StatusNotFound: // 404
		errType = ErrUnsupportedModel
		msg = "model not found"
	case statusCode >= 500:
		errType = ErrRetryable
		msg = "server error"
	default:
		errType = ErrNonRetryable
		msg = "client error"
	}

	if message != "" {
		msg = message
	}

	return &ProviderError{
		Provider: provider,
		Model:    model,
		Type:     errType,
		Message:  msg,
	}
}

// NewRetryableError creates a retryable error
func NewRetryableError(provider, model, message string, cause error) error {
	return &ProviderError{
		Provider: provider,
		Model:    model,
		Type:     ErrRetryable,
		Message:  message,
		Cause:    cause,
	}
}

// NewNonRetryableError creates a non-retryable error
func NewNonRetryableError(provider, model, message string, cause error) error {
	return &ProviderError{
		Provider: provider,
		Model:    model,
		Type:     ErrNonRetryable,
		Message:  message,
		Cause:    cause,
	}
}

// NewQuotaExhaustedError creates a quota exhausted error
func NewQuotaExhaustedError(provider, model, message string) error {
	return &ProviderError{
		Provider: provider,
		Model:    model,
		Type:     ErrQuotaExhausted,
		Message:  message,
	}
}

// NewProviderUnavailableError creates a provider unavailable error
func NewProviderUnavailableError(provider, model, message string, cause error) error {
	return &ProviderError{
		Provider: provider,
		Model:    model,
		Type:     ErrProviderUnavailable,
		Message:  message,
		Cause:    cause,
	}
}

// CooldownState tracks cooldown state for a provider/account
type CooldownState struct {
	RateLimitedUntil time.Time            // Timestamp when cooldown expires
	BackoffLevel     int                  // Current exponential backoff level
	ModelLocks       map[string]time.Time // model -> lock expiry timestamp
	mu               sync.Mutex
}

// CooldownTracker manages cooldown states for multiple provider/accounts
type CooldownTracker struct {
	states map[string]*CooldownState // "provider/account" -> cooldown state
	mu     sync.RWMutex
}

// NewCooldownTracker creates a new cooldown tracker
func NewCooldownTracker() *CooldownTracker {
	return &CooldownTracker{
		states: make(map[string]*CooldownState),
	}
}

// IsInCooldown checks if a provider/account is currently in cooldown
func (ct *CooldownTracker) IsInCooldown(provider, account string) bool {
	if account == "" {
		account = "default"
	}
	key := provider + "/" + account
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	state, exists := ct.states[key]
	if !exists {
		return false
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	return time.Now().Before(state.RateLimitedUntil)
}

// GetCooldownRemaining returns the remaining cooldown duration for a provider/account
func (ct *CooldownTracker) GetCooldownRemaining(provider, account string) time.Duration {
	if account == "" {
		account = "default"
	}
	key := provider + "/" + account
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	state, exists := ct.states[key]
	if !exists {
		return 0
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	remaining := time.Until(state.RateLimitedUntil)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// SetCooldown sets a provider/account into cooldown with the specified duration
func (ct *CooldownTracker) SetCooldown(provider, account string, cooldownDuration time.Duration) {
	if account == "" {
		account = "default"
	}
	key := provider + "/" + account
	ct.mu.Lock()
	defer ct.mu.Unlock()

	state, exists := ct.states[key]
	if !exists {
		state = &CooldownState{}
		ct.states[key] = state
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	state.RateLimitedUntil = time.Now().Add(cooldownDuration)
	state.BackoffLevel++
}

// ClearCooldown clears cooldown state for a provider/account
func (ct *CooldownTracker) ClearCooldown(provider, account string) {
	if account == "" {
		account = "default"
	}
	key := provider + "/" + account
	ct.mu.Lock()
	defer ct.mu.Unlock()

	state, exists := ct.states[key]
	if !exists {
		return
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	state.RateLimitedUntil = time.Time{}
	state.BackoffLevel = 0
}

// GetBackoffLevel returns the current backoff level for a provider/account
func (ct *CooldownTracker) GetBackoffLevel(provider, account string) int {
	if account == "" {
		account = "default"
	}
	key := provider + "/" + account
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	state, exists := ct.states[key]
	if !exists {
		return 0
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	return state.BackoffLevel
}

// ResetBackoffLevel resets the backoff level for a provider/account (e.g., after successful request)
func (ct *CooldownTracker) ResetBackoffLevel(provider, account string) {
	if account == "" {
		account = "default"
	}
	key := provider + "/" + account
	ct.mu.Lock()
	defer ct.mu.Unlock()

	state, exists := ct.states[key]
	if !exists {
		return
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	state.BackoffLevel = 0
}

// IsModelLocked checks if a specific model within a provider/account is locked
func (ct *CooldownTracker) IsModelLocked(provider, account, model string) bool {
	if account == "" {
		account = "default"
	}
	key := provider + "/" + account
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	state, exists := ct.states[key]
	if !exists {
		return false
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	if state.ModelLocks == nil {
		return false
	}

	lockExpiry, exists := state.ModelLocks[model]
	if !exists {
		lockExpiry, exists = state.ModelLocks["*"]
	}
	if !exists {
		return false
	}

	return time.Now().Before(lockExpiry)
}

func (ct *CooldownTracker) IsAccountLocked(provider, account string) bool {
	return ct.IsModelLocked(provider, account, "*")
}

func (ct *CooldownTracker) SetAccountLock(provider, account string, lockDuration time.Duration) {
	ct.SetModelLock(provider, account, "*", lockDuration)
}

func (ct *CooldownTracker) ClearAccountLock(provider, account string) {
	ct.ClearModelLock(provider, account, "*")
}

// GetModelLockRemaining returns the remaining lock duration for a model
func (ct *CooldownTracker) GetModelLockRemaining(provider, account, model string) time.Duration {
	if account == "" {
		account = "default"
	}
	key := provider + "/" + account
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	state, exists := ct.states[key]
	if !exists {
		return 0
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	if state.ModelLocks == nil {
		return 0
	}

	lockExpiry, exists := state.ModelLocks[model]
	if !exists {
		lockExpiry, exists = state.ModelLocks["*"]
	}
	if !exists {
		return 0
	}

	remaining := time.Until(lockExpiry)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// SetModelLock sets a lock on a specific model within a provider/account
func (ct *CooldownTracker) SetModelLock(provider, account, model string, lockDuration time.Duration) {
	if account == "" {
		account = "default"
	}
	key := provider + "/" + account
	ct.mu.Lock()
	defer ct.mu.Unlock()

	state, exists := ct.states[key]
	if !exists {
		state = &CooldownState{
			ModelLocks: make(map[string]time.Time),
		}
		ct.states[key] = state
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	if state.ModelLocks == nil {
		state.ModelLocks = make(map[string]time.Time)
	}

	state.ModelLocks[model] = time.Now().Add(lockDuration)
}

// ClearModelLock clears the lock on a specific model within a provider/account
func (ct *CooldownTracker) ClearModelLock(provider, account, model string) {
	if account == "" {
		account = "default"
	}
	key := provider + "/" + account
	ct.mu.Lock()
	defer ct.mu.Unlock()

	state, exists := ct.states[key]
	if !exists {
		return
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	if state.ModelLocks == nil {
		return
	}

	delete(state.ModelLocks, model)
}

// CircuitBreakerState represents the state of a circuit breaker
type CircuitBreakerState int

const (
	CircuitClosed   CircuitBreakerState = iota // Normal operation
	CircuitOpen                                // Provider is blocked
	CircuitHalfOpen                            // Testing if provider recovered
)

// CircuitBreaker implements a circuit breaker pattern for providers
type CircuitBreaker struct {
	state           CircuitBreakerState
	failureCount    int
	successCount    int
	lastFailureTime time.Time
	openUntil       time.Time
	mu              sync.Mutex
}

// CircuitBreakerConfig configures circuit breaker behavior
type CircuitBreakerConfig struct {
	FailureThreshold int           // Number of failures before opening
	OpenTimeout      time.Duration // How long to stay open
	SuccessThreshold int           // Number of successes to close in half-open
}

// CircuitBreakerManager manages circuit breakers for multiple providers
type CircuitBreakerManager struct {
	breakers map[string]*CircuitBreaker // provider name -> circuit breaker
	config   CircuitBreakerConfig
	mu       sync.RWMutex
}

// NewCircuitBreakerManager creates a new circuit breaker manager
func NewCircuitBreakerManager(config CircuitBreakerConfig) *CircuitBreakerManager {
	if config.FailureThreshold <= 0 {
		config.FailureThreshold = 5
	}
	if config.OpenTimeout <= 0 {
		config.OpenTimeout = 5 * time.Minute
	}
	if config.SuccessThreshold <= 0 {
		config.SuccessThreshold = 2
	}
	return &CircuitBreakerManager{
		breakers: make(map[string]*CircuitBreaker),
		config:   config,
	}
}

// IsOpen checks if a provider's circuit breaker is open (blocking requests)
func (cbm *CircuitBreakerManager) IsOpen(provider string) bool {
	cbm.mu.Lock()
	defer cbm.mu.Unlock()

	cb, exists := cbm.breakers[provider]
	if !exists {
		return false
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	// Auto-transition from open to half-open after timeout
	if cb.state == CircuitOpen && time.Now().After(cb.openUntil) {
		cb.state = CircuitHalfOpen
		cb.failureCount = 0
		return false
	}

	return cb.state == CircuitOpen
}

// RecordSuccess records a successful request for a provider
func (cbm *CircuitBreakerManager) RecordSuccess(provider string) {
	cbm.mu.Lock()
	defer cbm.mu.Unlock()

	cb, exists := cbm.breakers[provider]
	if !exists {
		return
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitHalfOpen:
		cb.successCount++
		if cb.successCount >= cbm.config.SuccessThreshold {
			cb.state = CircuitClosed
			cb.failureCount = 0
			cb.successCount = 0
		}
	case CircuitClosed:
		cb.failureCount = 0
		cb.successCount = 0
	}
}

// RecordFailure records a failed request for a provider
func (cbm *CircuitBreakerManager) RecordFailure(provider string) {
	cbm.mu.Lock()
	defer cbm.mu.Unlock()

	cb, exists := cbm.breakers[provider]
	if !exists {
		cb = &CircuitBreaker{state: CircuitClosed}
		cbm.breakers[provider] = cb
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount++
	cb.lastFailureTime = time.Now()

	// Open circuit if threshold reached
	if cb.failureCount >= cbm.config.FailureThreshold {
		cb.state = CircuitOpen
		cb.openUntil = time.Now().Add(cbm.config.OpenTimeout)
	}
}

// GetState returns the current state of a provider's circuit breaker
func (cbm *CircuitBreakerManager) GetState(provider string) CircuitBreakerState {
	cbm.mu.Lock()
	defer cbm.mu.Unlock()

	cb, exists := cbm.breakers[provider]
	if !exists {
		return CircuitClosed
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	// Auto-transition check
	if cb.state == CircuitOpen && time.Now().After(cb.openUntil) {
		cb.state = CircuitHalfOpen
		cb.failureCount = 0
	}

	return cb.state
}

// Reset resets a provider's circuit breaker to closed state
func (cbm *CircuitBreakerManager) Reset(provider string) {
	cbm.mu.Lock()
	defer cbm.mu.Unlock()

	cb, exists := cbm.breakers[provider]
	if !exists {
		return
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = CircuitClosed
	cb.failureCount = 0
	cb.successCount = 0
	cb.openUntil = time.Time{}
}
