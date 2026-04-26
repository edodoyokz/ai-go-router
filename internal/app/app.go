package app

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/edodoyokz/9router-go/internal/api"
	"github.com/edodoyokz/9router-go/internal/config"
	"github.com/edodoyokz/9router-go/internal/providers"
	routing "github.com/edodoyokz/9router-go/internal/router"
)

func Run(ctx context.Context, configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	logger := buildLogger(cfg.Logging.Level)
	registry := buildRegistry(cfg)
	engine := routing.NewEngine(cfg.Routes, registry)
	server := api.NewServer(cfg, logger, engine)

	logger.Info().Str("config", configPath).Msg("configuration loaded")
	return server.ListenAndServe(ctx)
}

func buildLogger(level string) zerolog.Logger {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	logger := log.Output(zerolog.ConsoleWriter{Out: os.Stdout})

	switch strings.ToLower(level) {
	case "debug":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case "warn":
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case "error":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	default:
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}

	return logger
}

func buildRegistry(cfg config.Config) *providers.Registry {
	adapters := make([]providers.Adapter, 0, len(cfg.Providers))
	for _, provider := range cfg.Providers {
		if !provider.Enabled {
			continue
		}
		adapters = append(adapters, providers.NewStubAdapter(provider.Name))
	}

	if len(adapters) == 0 {
		panic(fmt.Errorf("no enabled providers configured"))
	}

	return providers.NewRegistry(adapters...)
}
