package router

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/edodoyokz/9router-go/internal/config"
	"github.com/edodoyokz/9router-go/internal/providers"
)

type Engine struct {
	routes          map[string]config.RouteConfig
	modelAliases    map[string]config.ModelAlias
	registry        *providers.Registry
	retryConfig     config.RetryConfig
	cooldownTracker *providers.CooldownTracker
	roundRobinIdx   map[string]int // per-route round-robin index
	rrMu            sync.Mutex     // protects roundRobinIdx
}

func NewEngine(routes map[string]config.RouteConfig, modelAliases map[string]config.ModelAlias, registry *providers.Registry, retryConfig config.RetryConfig) *Engine {
	return &Engine{
		routes:          routes,
		modelAliases:    modelAliases,
		registry:        registry,
		retryConfig:     retryConfig,
		cooldownTracker: providers.NewCooldownTracker(),
		roundRobinIdx:   make(map[string]int),
	}
}

func (e *Engine) GetRegistry() *providers.Registry {
	return e.registry
}

func (e *Engine) ResolveTargets(model string) []config.RouteTarget {
	// Check model aliases first
	if alias, ok := e.modelAliases[model]; ok {
		return []config.RouteTarget{{
			Provider: alias.Provider,
			Model:    alias.Model,
		}}
	}

	// Check route configs
	if routeConfig, ok := e.routes[model]; ok && len(routeConfig.Targets) > 0 {
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

	parts := strings.SplitN(model, "/", 2)
	if len(parts) == 2 {
		return []config.RouteTarget{{
			Provider: parts[0],
			Model:    parts[1],
		}}
	}

	return nil
}

func (e *Engine) ChatCompletion(ctx context.Context, request providers.ChatRequest) (providers.ChatResponse, string, error) {
	targets := e.ResolveTargets(request.Model)
	if len(targets) == 0 {
		return providers.ChatResponse{}, "", fmt.Errorf("no route targets for model: %s", request.Model)
	}

	var allErrors []string

	// Try each target in the fallback chain
	for targetIdx, target := range targets {
		// Check if provider is in cooldown
		if e.cooldownTracker.IsInCooldown(target.Provider, "") {
			remaining := e.cooldownTracker.GetCooldownRemaining(target.Provider, "")
			allErrors = append(allErrors, fmt.Sprintf("target[%d] %s/%s: in cooldown for %v", targetIdx, target.Provider, target.Model, remaining))
			continue
		}

		// Check if model is locked
		if e.cooldownTracker.IsModelLocked(target.Provider, "", target.Model) {
			remaining := e.cooldownTracker.GetModelLockRemaining(target.Provider, "", target.Model)
			allErrors = append(allErrors, fmt.Sprintf("target[%d] %s/%s: model locked for %v", targetIdx, target.Provider, target.Model, remaining))
			continue
		}

		adapter, err := e.registry.Get(target.Provider)
		if err != nil {
			allErrors = append(allErrors, fmt.Sprintf("target[%d] %s/%s: registry error: %v", targetIdx, target.Provider, target.Model, err))
			continue
		}

		// Pass provider and account info via context for multi-account support
		// Don't set account in context - let adapter use round-robin selection
		ctxWithAccount := ctx

		// Attempt with retry logic
		response, err := e.attemptWithRetry(ctxWithAccount, adapter, request, target)
		if err != nil {
			allErrors = append(allErrors, fmt.Sprintf("target[%d] %s/%s tier=%s: %v", targetIdx, target.Provider, target.Model, target.Tier, err))

			// Trigger cooldown on quota exhausted errors
			if providers.IsQuotaExhausted(err) {
				cooldownDuration := e.calculateCooldownDuration(target.Provider)
				e.cooldownTracker.SetCooldown(target.Provider, "", cooldownDuration)
			}

			// Trigger model lock on unsupported model errors
			if providers.IsUnsupportedModel(err) {
				// Lock model for 5 minutes
				e.cooldownTracker.SetModelLock(target.Provider, "", target.Model, 5*time.Minute)
			}

			// Only continue to next target if error is retryable
			if providers.IsRetryable(err) {
				continue
			}

			// Non-retryable error - fail immediately without trying other targets
			return providers.ChatResponse{}, "", fmt.Errorf("non-retryable error from %s/%s: %w", target.Provider, target.Model, err)
		}

		// Success - reset backoff level and clear model lock
		e.cooldownTracker.ResetBackoffLevel(target.Provider, "")
		e.cooldownTracker.ClearModelLock(target.Provider, "", target.Model)

		// Success - return response
		return response, target.Provider, nil
	}

	// All targets exhausted
	return providers.ChatResponse{}, "", fmt.Errorf("all %d route targets failed: %s", len(targets), strings.Join(allErrors, " | "))
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
func (e *Engine) calculateCooldownDuration(provider string) time.Duration {
	backoffLevel := e.cooldownTracker.GetBackoffLevel(provider, "")

	// Exponential cooldown: maxCooldownMs * 2^(backoffLevel-1)
	cooldownMs := float64(e.retryConfig.MaxCooldownMs) * math.Pow(2, float64(backoffLevel-1))

	// Cap at 1 hour maximum
	maxCooldownMs := 60 * 60 * 1000 // 1 hour
	if cooldownMs > float64(maxCooldownMs) {
		cooldownMs = float64(maxCooldownMs)
	}

	return time.Duration(cooldownMs) * time.Millisecond
}
