package router

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/edodoyokz/ai-go-router/internal/config"
	"github.com/edodoyokz/ai-go-router/internal/providers"
	"github.com/edodoyokz/ai-go-router/internal/providers/catalog"
)

type Engine struct {
	mu                    sync.RWMutex
	routes                map[string]config.RouteConfig
	modelAliases          map[string]config.ModelAlias
	registry              *providers.Registry
	retryConfig           config.RetryConfig
	cooldownTracker       *providers.CooldownTracker
	circuitBreakerManager *providers.CircuitBreakerManager
	roundRobinIdx         map[string]int // per-route round-robin index
	rrMu                  sync.Mutex     // protects roundRobinIdx
}

func NewEngine(routes map[string]config.RouteConfig, modelAliases map[string]config.ModelAlias, registry *providers.Registry, retryConfig config.RetryConfig) *Engine {
	cbConfig := providers.CircuitBreakerConfig{
		FailureThreshold: retryConfig.CircuitBreaker.FailureThreshold,
		OpenTimeout:      time.Duration(retryConfig.CircuitBreaker.OpenTimeoutMs) * time.Millisecond,
		SuccessThreshold: retryConfig.CircuitBreaker.SuccessThreshold,
	}

	return &Engine{
		routes:                routes,
		modelAliases:          modelAliases,
		registry:              registry,
		retryConfig:           retryConfig,
		cooldownTracker:       providers.NewCooldownTracker(),
		circuitBreakerManager: providers.NewCircuitBreakerManager(cbConfig),
		roundRobinIdx:         make(map[string]int),
	}
}

func (e *Engine) GetRegistry() *providers.Registry {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.registry
}

func (e *Engine) SetCooldownTracker(tracker *providers.CooldownTracker) {
	if tracker == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cooldownTracker = tracker
}

func (e *Engine) Reconfigure(routes map[string]config.RouteConfig, modelAliases map[string]config.ModelAlias, registry *providers.Registry, retryConfig config.RetryConfig) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.routes = routes
	e.modelAliases = modelAliases
	e.registry = registry
	e.retryConfig = retryConfig

	// Reconfigure circuit breaker manager with new config
	cbConfig := providers.CircuitBreakerConfig{
		FailureThreshold: retryConfig.CircuitBreaker.FailureThreshold,
		OpenTimeout:      time.Duration(retryConfig.CircuitBreaker.OpenTimeoutMs) * time.Millisecond,
		SuccessThreshold: retryConfig.CircuitBreaker.SuccessThreshold,
	}
	e.circuitBreakerManager = providers.NewCircuitBreakerManager(cbConfig)
}

func (e *Engine) ResolveTargets(model string) []config.RouteTarget {
	e.mu.RLock()
	routes := e.routes
	aliases := e.modelAliases
	e.mu.RUnlock()

	// Check route configs before aliases to match reference combo precedence.
	if routeConfig, ok := routes[model]; ok && len(routeConfig.Targets) > 0 {
		targets := make([]config.RouteTarget, len(routeConfig.Targets))
		copy(targets, routeConfig.Targets)

		// Apply round-robin rotation if strategy is round-robin
		if routeConfig.Strategy == "round-robin" {
			e.rrMu.Lock()
			idx := e.roundRobinIdx[model]
			e.roundRobinIdx[model] = (idx + 1) % len(targets)
			e.rrMu.Unlock()

			// Rotate targets by moving first idx elements to end
			if idx > 0 {
				rotated := append(targets[idx:], targets[:idx]...)
				return rotated
			}
		}

		return targets
	}

	if alias, ok := aliases[model]; ok {
		return []config.RouteTarget{{
			Provider: alias.Provider,
			Model:    alias.Model,
		}}
	}

	parts := strings.SplitN(model, "/", 2)
	if len(parts) == 2 {
		providerName := parts[0]
		if resolved, ok := catalog.ResolveAlias(providerName); ok {
			providerName = resolved.ID
		}
		return []config.RouteTarget{{
			Provider: providerName,
			Model:    parts[1],
		}}
	}

	return nil
}

func (e *Engine) ChatCompletion(ctx context.Context, request providers.ChatRequest) (providers.ChatResponse, string, error) {
	e.mu.RLock()
	registry := e.registry
	e.mu.RUnlock()

	targets := e.ResolveTargets(request.Model)
	if len(targets) == 0 {
		return providers.ChatResponse{}, "", fmt.Errorf("router is not configured yet: no route targets for model %q; add a provider and route in the dashboard", request.Model)
	}

	var allErrors []string

	// Try each target in the fallback chain
	for targetIdx, target := range targets {
		if e.circuitBreakerManager.IsOpen(target.Provider) {
			allErrors = append(allErrors, fmt.Sprintf("target[%d] %s/%s: circuit breaker open", targetIdx, target.Provider, target.Model))
			continue
		}

		adapter, err := registry.Get(target.Provider)
		if err != nil {
			allErrors = append(allErrors, fmt.Sprintf("target[%d] %s/%s: registry error: %v", targetIdx, target.Provider, target.Model, err))
			continue
		}

		accounts := e.accountNames(adapter)
		for _, account := range accounts {
			if e.cooldownTracker.IsInCooldown(target.Provider, account) {
				remaining := e.cooldownTracker.GetCooldownRemaining(target.Provider, account)
				allErrors = append(allErrors, fmt.Sprintf("target[%d] %s/%s account=%s: in cooldown for %v", targetIdx, target.Provider, target.Model, account, remaining))
				continue
			}
			if e.cooldownTracker.IsModelLocked(target.Provider, account, target.Model) {
				remaining := e.cooldownTracker.GetModelLockRemaining(target.Provider, account, target.Model)
				allErrors = append(allErrors, fmt.Sprintf("target[%d] %s/%s account=%s: model locked for %v", targetIdx, target.Provider, target.Model, account, remaining))
				continue
			}

			ctxWithAccount := context.WithValue(ctx, providers.AccountContextKey, account)

			response, err := e.attemptWithRetry(ctxWithAccount, adapter, request, target)
			if err != nil {
				allErrors = append(allErrors, fmt.Sprintf("target[%d] %s/%s account=%s tier=%s: %v", targetIdx, target.Provider, target.Model, account, target.Tier, err))

				e.circuitBreakerManager.RecordFailure(target.Provider)

				if providers.IsQuotaExhausted(err) || providers.IsAuthFailure(err) {
					cooldownDuration := e.calculateCooldownDuration(target.Provider, account)
					e.cooldownTracker.SetCooldown(target.Provider, account, cooldownDuration)
				}

				if providers.IsUnsupportedModel(err) {
					e.cooldownTracker.SetModelLock(target.Provider, account, target.Model, 5*time.Minute)
				}

				if providers.IsRetryable(err) {
					continue
				}

				return providers.ChatResponse{}, "", fmt.Errorf("non-retryable error from %s/%s account=%s: %w", target.Provider, target.Model, account, err)
			}

			e.cooldownTracker.ResetBackoffLevel(target.Provider, account)
			e.cooldownTracker.ClearModelLock(target.Provider, account, target.Model)
			e.circuitBreakerManager.RecordSuccess(target.Provider)
			return response, target.Provider, nil
		}
	}

	// All targets exhausted
	return providers.ChatResponse{}, "", fmt.Errorf("all %d route targets failed: %s", len(targets), strings.Join(allErrors, " | "))
}

// StreamingChatCompletion executes a streaming chat completion with fallback, retry, cooldown, and model-lock
func (e *Engine) StreamingChatCompletion(ctx context.Context, request providers.ChatRequest) (<-chan providers.ChatChunk, string, error) {
	e.mu.RLock()
	registry := e.registry
	e.mu.RUnlock()

	targets := e.ResolveTargets(request.Model)
	if len(targets) == 0 {
		return nil, "", fmt.Errorf("router is not configured yet: no route targets for model %q; add a provider and route in the dashboard", request.Model)
	}

	var allErrors []string

	// Try each target in the fallback chain
	for targetIdx, target := range targets {
		// Check if provider circuit breaker is open
		if e.circuitBreakerManager.IsOpen(target.Provider) {
			allErrors = append(allErrors, fmt.Sprintf("target[%d] %s/%s: circuit breaker open", targetIdx, target.Provider, target.Model))
			continue
		}

		adapter, err := registry.Get(target.Provider)
		if err != nil {
			allErrors = append(allErrors, fmt.Sprintf("target[%d] %s/%s: registry error: %v", targetIdx, target.Provider, target.Model, err))
			continue
		}

		accounts := e.accountNames(adapter)
		for _, account := range accounts {
			if e.cooldownTracker.IsInCooldown(target.Provider, account) {
				remaining := e.cooldownTracker.GetCooldownRemaining(target.Provider, account)
				allErrors = append(allErrors, fmt.Sprintf("target[%d] %s/%s account=%s: in cooldown for %v", targetIdx, target.Provider, target.Model, account, remaining))
				continue
			}

			if e.cooldownTracker.IsModelLocked(target.Provider, account, target.Model) {
				remaining := e.cooldownTracker.GetModelLockRemaining(target.Provider, account, target.Model)
				allErrors = append(allErrors, fmt.Sprintf("target[%d] %s/%s account=%s: model locked for %v", targetIdx, target.Provider, target.Model, account, remaining))
				continue
			}

			ctxWithAccount := context.WithValue(ctx, providers.AccountContextKey, account)

			chunks, err := e.attemptStreamingWithRetry(ctxWithAccount, adapter, request, target)
			if err != nil {
				allErrors = append(allErrors, fmt.Sprintf("target[%d] %s/%s account=%s tier=%s: %v", targetIdx, target.Provider, target.Model, account, target.Tier, err))

				e.circuitBreakerManager.RecordFailure(target.Provider)

				if providers.IsQuotaExhausted(err) || providers.IsAuthFailure(err) {
					cooldownDuration := e.calculateCooldownDuration(target.Provider, account)
					e.cooldownTracker.SetCooldown(target.Provider, account, cooldownDuration)
				}

				if providers.IsUnsupportedModel(err) {
					e.cooldownTracker.SetModelLock(target.Provider, account, target.Model, 5*time.Minute)
				}

				if providers.IsRetryable(err) {
					continue
				}

				return nil, "", fmt.Errorf("non-retryable error from %s/%s account=%s: %w", target.Provider, target.Model, account, err)
			}

			e.cooldownTracker.ResetBackoffLevel(target.Provider, account)
			e.cooldownTracker.ClearModelLock(target.Provider, account, target.Model)
			e.circuitBreakerManager.RecordSuccess(target.Provider)
			return chunks, target.Provider, nil
		}
	}

	// All targets exhausted
	return nil, "", fmt.Errorf("all %d route targets failed: %s", len(targets), strings.Join(allErrors, " | "))
}

// attemptStreamingWithRetry executes a streaming request with exponential backoff retry logic
func (e *Engine) attemptStreamingWithRetry(ctx context.Context, adapter providers.Adapter, request providers.ChatRequest, target config.RouteTarget) (<-chan providers.ChatChunk, error) {
	var lastErr error

	for attempt := 1; attempt <= e.retryConfig.MaxAttempts; attempt++ {
		// Check context before attempting
		if ctx.Err() != nil {
			return nil, fmt.Errorf("context cancelled before attempt %d: %w", attempt, ctx.Err())
		}

		// Execute streaming request
		chunks, err := adapter.StreamChatCompletion(ctx, request, target.Model)
		if err == nil {
			// Success
			return chunks, nil
		}

		lastErr = err

		// Check if error is retryable
		if !providers.IsRetryable(err) {
			// Non-retryable error - fail immediately
			return nil, err
		}

		// Don't sleep after last attempt
		if attempt < e.retryConfig.MaxAttempts {
			backoff := e.calculateBackoff(attempt)

			// Check if we have time for backoff
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("context cancelled during backoff: %w", ctx.Err())
			case <-time.After(backoff):
				// Continue to next attempt
			}
		}
	}

	// All retries exhausted
	return nil, fmt.Errorf("exhausted %d retry attempts: %w", e.retryConfig.MaxAttempts, lastErr)
}

// attemptWithRetry executes a request with exponential backoff retry logic
func (e *Engine) attemptWithRetry(ctx context.Context, adapter providers.Adapter, request providers.ChatRequest, target config.RouteTarget) (providers.ChatResponse, error) {
	var lastErr error

	for attempt := 1; attempt <= e.retryConfig.MaxAttempts; attempt++ {
		// Check context before attempting
		if ctx.Err() != nil {
			return providers.ChatResponse{}, fmt.Errorf("context cancelled before attempt %d: %w", attempt, ctx.Err())
		}

		// Execute request
		response, err := adapter.ChatCompletion(ctx, request, target.Model)
		if err == nil {
			// Success
			return response, nil
		}

		lastErr = err

		// Check if error is retryable
		if !providers.IsRetryable(err) {
			// Non-retryable error - fail immediately
			return providers.ChatResponse{}, err
		}

		// Don't sleep after last attempt
		if attempt < e.retryConfig.MaxAttempts {
			backoff := e.calculateBackoff(attempt)

			// Check if we have time for backoff
			select {
			case <-ctx.Done():
				return providers.ChatResponse{}, fmt.Errorf("context cancelled during backoff: %w", ctx.Err())
			case <-time.After(backoff):
				// Continue to next attempt
			}
		}
	}

	// All retries exhausted
	return providers.ChatResponse{}, fmt.Errorf("exhausted %d retry attempts: %w", e.retryConfig.MaxAttempts, lastErr)
}

// calculateBackoff computes exponential backoff with jitter
func (e *Engine) calculateBackoff(attempt int) time.Duration {
	// Exponential backoff: initialBackoff * 2^(attempt-1)
	backoffMs := float64(e.retryConfig.InitialBackoffMs) * math.Pow(2, float64(attempt-1))

	// Cap at max backoff
	if backoffMs > float64(e.retryConfig.MaxBackoffMs) {
		backoffMs = float64(e.retryConfig.MaxBackoffMs)
	}

	// Add jitter: random +/- 25% to prevent thundering herd
	jitter := backoffMs * 0.25 * (2*rand.Float64() - 1)
	backoffMs += jitter

	return time.Duration(backoffMs) * time.Millisecond
}

// calculateCooldownDuration computes cooldown duration based on backoff level
func (e *Engine) accountNames(adapter providers.Adapter) []string {
	if accountAware, ok := adapter.(providers.AccountAwareAdapter); ok {
		return accountAware.AccountNames()
	}
	return []string{"default"}
}

func (e *Engine) calculateCooldownDuration(provider, account string) time.Duration {
	backoffLevel := e.cooldownTracker.GetBackoffLevel(provider, account)
	if backoffLevel < 1 {
		backoffLevel = 1
	}

	// Exponential cooldown: maxCooldownMs * 2^(backoffLevel-1)
	cooldownMs := float64(e.retryConfig.MaxCooldownMs) * math.Pow(2, float64(backoffLevel-1))

	// Cap at 1 hour maximum
	maxCooldownMs := 60 * 60 * 1000 // 1 hour
	if cooldownMs > float64(maxCooldownMs) {
		cooldownMs = float64(maxCooldownMs)
	}

	return time.Duration(cooldownMs) * time.Millisecond
}
