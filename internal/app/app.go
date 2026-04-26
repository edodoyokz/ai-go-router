package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/edodoyokz/9router-go/internal/api"
	"github.com/edodoyokz/9router-go/internal/config"
	"github.com/edodoyokz/9router-go/internal/providers"
	routing "github.com/edodoyokz/9router-go/internal/router"
	"github.com/edodoyokz/9router-go/internal/storage"
	"github.com/edodoyokz/9router-go/internal/translator"
)

func Run(ctx context.Context, configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	logger := buildLogger(cfg.Logging.Level, cfg.Logging.JSONMode)

	// Initialize storage
	db, err := storage.NewDB(cfg.Storage.SQLitePath)
	if err != nil {
		return fmt.Errorf("initialize database: %w", err)
	}
	defer db.Close()

	asyncWriter := storage.NewAsyncWriter(db, logger)
	defer asyncWriter.Close()

	registry, err := buildRegistry(cfg)
	if err != nil {
		return fmt.Errorf("build provider registry: %w", err)
	}

	engine := routing.NewEngine(cfg.Routes, cfg.ModelAliases, registry, cfg.Retry)
	server := api.NewServer(cfg, logger, engine, asyncWriter)

	logger.Info().Str("config", configPath).Msg("configuration loaded")
	return server.ListenAndServe(ctx)
}

func buildLogger(level string, jsonMode bool) zerolog.Logger {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	var output io.Writer
	if jsonMode {
		output = NewSecretRedactionWriter(os.Stdout)
	} else {
		output = NewSecretRedactionWriter(zerolog.ConsoleWriter{Out: os.Stdout})
	}

	logger := log.Output(output)

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

func buildRegistry(cfg config.Config) (*providers.Registry, error) {
	translatorRegistry := translator.NewRegistry()
	adapters := make([]providers.Adapter, 0, len(cfg.Providers))
	for _, provider := range cfg.Providers {
		if !provider.Enabled {
			continue
		}

		var adapter providers.Adapter
		switch provider.Type {
		case "openai_compat":
			adapter = providers.NewOpenAIAdapter(provider, cfg.Errors)
		case "openrouter":
			adapter = providers.NewOpenRouterAdapter(provider, cfg.Errors)
		case "anthropic", "anthropic_compat":
			adapter = providers.NewAnthropicAdapter(provider, cfg.Errors, translatorRegistry)
		default:
			return nil, fmt.Errorf("unsupported provider type: %s (provider: %s)", provider.Type, provider.Name)
		}

		adapters = append(adapters, adapter)
	}

	if len(adapters) == 0 {
		return nil, fmt.Errorf("no enabled providers configured")
	}

	return providers.NewRegistry(adapters...), nil
}
