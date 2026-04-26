package router

import (
	"context"
	"fmt"
	"strings"

	"github.com/edodoyokz/9router-go/internal/config"
	"github.com/edodoyokz/9router-go/internal/providers"
)

type Engine struct {
	routes   map[string][]config.RouteTarget
	registry *providers.Registry
}

func NewEngine(routes map[string][]config.RouteTarget, registry *providers.Registry) *Engine {
	return &Engine{
		routes:   routes,
		registry: registry,
	}
}

func (e *Engine) ResolveTargets(model string) []config.RouteTarget {
	if targets, ok := e.routes[model]; ok && len(targets) > 0 {
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

	var errors []string
	for _, target := range targets {
		adapter, err := e.registry.Get(target.Provider)
		if err != nil {
			errors = append(errors, err.Error())
			continue
		}

		response, err := adapter.ChatCompletion(ctx, request, target.Model)
		if err != nil {
			errors = append(errors, fmt.Sprintf("provider=%s model=%s err=%v", target.Provider, target.Model, err))
			continue
		}

		return response, target.Provider, nil
	}

	return providers.ChatResponse{}, "", fmt.Errorf("all route targets failed: %s", strings.Join(errors, " | "))
}
