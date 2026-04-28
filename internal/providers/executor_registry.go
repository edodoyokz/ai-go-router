package providers

import (
	"fmt"
	"strings"

	"github.com/edodoyokz/ai-go-router/internal/config"
)

type ExecutorBuilder func(cfg config.ProviderConfig, errorCfg config.ErrorConfig) (Executor, error)

var executorBuilders = map[string]ExecutorBuilder{}

func RegisterExecutor(providerID string, builder ExecutorBuilder) {
	executorBuilders[strings.ToLower(strings.TrimSpace(providerID))] = builder
}

func BuildExecutor(providerID string, cfg config.ProviderConfig, errorCfg config.ErrorConfig) (Executor, error) {
	builder, ok := executorBuilders[strings.ToLower(strings.TrimSpace(providerID))]
	if !ok {
		return nil, fmt.Errorf("no executor registered for provider: %s", providerID)
	}
	return builder(cfg, errorCfg)
}
