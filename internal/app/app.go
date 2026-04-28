package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/edodoyokz/ai-go-router/internal/api"
	"github.com/edodoyokz/ai-go-router/internal/config"
	"github.com/edodoyokz/ai-go-router/internal/mitm"
	"github.com/edodoyokz/ai-go-router/internal/nodes"
	"github.com/edodoyokz/ai-go-router/internal/oauth"
	"github.com/edodoyokz/ai-go-router/internal/providers"
	routing "github.com/edodoyokz/ai-go-router/internal/router"
	"github.com/edodoyokz/ai-go-router/internal/storage"
	cloudsync "github.com/edodoyokz/ai-go-router/internal/sync"
	"github.com/edodoyokz/ai-go-router/internal/tunnel"
)

func Run(ctx context.Context, configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	// Wrap config in RuntimeConfig for thread-safe mutation
	runtimeCfg := config.NewRuntimeConfig(cfg, configPath)

	logger := buildLogger(cfg.Logging)

	// Initialize storage
	db, err := storage.NewDB(cfg.Storage.SQLitePath)
	if err != nil {
		return fmt.Errorf("initialize database: %w", err)
	}
	defer db.Close()

	asyncWriter := storage.NewAsyncWriter(db, logger)
	defer asyncWriter.Close()

	registry, err := providers.BuildHydratedRegistry(ctx, cfg, db)
	if err != nil {
		return fmt.Errorf("build provider registry: %w", err)
	}

	engine := routing.NewEngine(cfg.Routes, cfg.ModelAliases, registry, cfg.Retry)
	if err := hydrateRuntimeState(ctx, db, engine, logger); err != nil {
		logger.Warn().Err(err).Msg("failed to hydrate runtime state")
	}
	server := api.NewServer(runtimeCfg, logger, engine, asyncWriter)
	server.SetTunnelManager(tunnel.NewManager(cfg.Tunnel, logger))

	oauthStore, err := oauth.NewStore(cfg.Storage.SQLitePath, cfg.Server.APIKey)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to initialize OAuth store")
	} else {
		server.SetOAuthStore(oauthStore)
		defer oauthStore.Close()
	}

	logger.Info().Str("config", configPath).Msg("configuration loaded")
	return server.ListenAndServe(ctx)
}

func RunWithReload(ctx context.Context, configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	// Wrap config in RuntimeConfig for thread-safe mutation
	runtimeCfg := config.NewRuntimeConfig(cfg, configPath)

	logger := buildLogger(cfg.Logging)

	// Initialize storage
	db, err := storage.NewDB(cfg.Storage.SQLitePath)
	if err != nil {
		return fmt.Errorf("initialize database: %w", err)
	}
	defer db.Close()

	asyncWriter := storage.NewAsyncWriter(db, logger)
	defer asyncWriter.Close()

	registry, err := providers.BuildHydratedRegistry(ctx, cfg, db)
	if err != nil {
		return fmt.Errorf("build provider registry: %w", err)
	}

	engine := routing.NewEngine(cfg.Routes, cfg.ModelAliases, registry, cfg.Retry)
	if err := hydrateRuntimeState(ctx, db, engine, logger); err != nil {
		logger.Warn().Err(err).Msg("failed to hydrate runtime state")
	}
	server := api.NewServer(runtimeCfg, logger, engine, asyncWriter)
	tunnelMgr := tunnel.NewManager(cfg.Tunnel, logger)
	server.SetTunnelManager(tunnelMgr)

	oauthStore, err := oauth.NewStore(cfg.Storage.SQLitePath, cfg.Server.APIKey)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to initialize OAuth store")
	} else {
		server.SetOAuthStore(oauthStore)
		defer oauthStore.Close()
	}

	logger.Info().Str("config", configPath).Msg("configuration loaded")

	// Start tunnel if configured
	if cfg.Tunnel.Enabled {
		localAddr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
		go func() {
			if err := tunnelMgr.Start(ctx, localAddr); err != nil {
				logger.Error().Err(err).Msg("tunnel error")
			}
		}()
		defer tunnelMgr.Stop()
	}

	// Start MITM proxy if configured
	if cfg.MITM.Enabled {
		mitmCfg := mitm.Config{
			Enabled:     cfg.MITM.Enabled,
			ListenAddr:  cfg.MITM.ListenAddr,
			UpstreamURL: cfg.MITM.UpstreamURL,
			TLSCert:     cfg.MITM.TLSCert,
			TLSKey:      cfg.MITM.TLSKey,
			APIKey:      cfg.MITM.APIKey,
		}
		proxy, err := mitm.NewProxy(mitmCfg, logger)
		if err != nil {
			logger.Error().Err(err).Msg("failed to create MITM proxy")
		} else {
			go func() {
				if err := proxy.ListenAndServe(); err != nil {
					logger.Error().Err(err).Msg("MITM proxy stopped")
				}
			}()
			defer proxy.Close()
		}
	}

	// Start provider node health checks if nodes are configured
	if len(cfg.Nodes) > 0 {
		nodeCfgs := make([]nodes.NodeConfig, len(cfg.Nodes))
		for i, n := range cfg.Nodes {
			nodeCfgs[i] = nodes.NodeConfig{
				Name:    n.Name,
				BaseURL: n.BaseURL,
				APIKey:  n.APIKey,
				Enabled: n.Enabled,
				Weight:  n.Weight,
			}
		}
		nodeReg := nodes.NewRegistry(nodeCfgs, logger)
		server.SetNodeRegistry(nodeReg)
		go nodeReg.StartHealthChecks(ctx, 30*time.Second)
		logger.Info().Int("count", len(cfg.Nodes)).Msg("node registry started")
	}

	// Start cloud sync if configured
	if cfg.Sync.Enabled {
		syncMgr := cloudsync.NewManager(cloudsync.Config{
			Enabled:         cfg.Sync.Enabled,
			Provider:        cfg.Sync.Provider,
			Endpoint:        cfg.Sync.Endpoint,
			Bucket:          cfg.Sync.Bucket,
			Prefix:          cfg.Sync.Prefix,
			AccessKey:       cfg.Sync.AccessKey,
			SecretKey:       cfg.Sync.SecretKey,
			IntervalMinutes: cfg.Sync.IntervalMinutes,
		}, logger)
		server.SetSyncManager(syncMgr)
		go syncMgr.Start(ctx, cfg.Storage.SQLitePath, configPath)
	}

	// Set up SIGHUP handler for config hot-reload
	hupChan := make(chan os.Signal, 1)
	signal.Notify(hupChan, syscall.SIGHUP)

	// Start server in background
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.ListenAndServe(ctx)
	}()

	// Handle signals
	for {
		select {
		case err := <-serverErr:
			return err
		case <-hupChan:
			logger.Info().Msg("received SIGHUP, reloading configuration")
			if err := runtimeCfg.Reload(); err != nil {
				logger.Error().Err(err).Msg("failed to reload configuration")
				continue
			}
			newCfg := runtimeCfg.Get()
			logger.Info().Str("config", configPath).Msg("configuration reloaded")

			// Rebuild provider registry with new config + DB connections
			newRegistry, err := providers.BuildHydratedRegistry(ctx, newCfg, db)
			if err != nil {
				logger.Error().Err(err).Msg("failed to rebuild provider registry")
				continue
			}

			// Reconfigure engine with new config
			engine.Reconfigure(newCfg.Routes, newCfg.ModelAliases, newRegistry, newCfg.Retry)
			logger.Info().Msg("routing engine reconfigured")
		}
	}
}

func hydrateRuntimeState(ctx context.Context, db *storage.DB, engine *routing.Engine, logger zerolog.Logger) error {
	tracker := providers.NewCooldownTracker()
	cooldowns, err := db.LoadAccountCooldowns(ctx)
	if err != nil {
		return err
	}
	for key, state := range cooldowns {
		provider, account, ok := strings.Cut(key, "/")
		if !ok {
			continue
		}
		if remaining := time.Until(state.RateLimitedUntil); remaining > 0 {
			tracker.SetCooldown(provider, account, remaining)
		}
	}
	locks, err := db.LoadModelLocks(ctx)
	if err != nil {
		return err
	}
	for key, models := range locks {
		provider, account, ok := strings.Cut(key, "/")
		if !ok {
			continue
		}
		for model, until := range models {
			if remaining := time.Until(until); remaining > 0 {
				tracker.SetModelLock(provider, account, model, remaining)
			}
		}
	}
	engine.SetCooldownTracker(tracker)
	logger.Debug().Int("cooldowns", len(cooldowns)).Int("lock_accounts", len(locks)).Msg("hydrated runtime state")
	return nil
}

func buildLogger(logCfg config.LoggingConfig) zerolog.Logger {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	var baseWriter io.Writer = os.Stdout

	// Wire log rotation when enabled
	if logCfg.Rotation.Enabled && logCfg.Rotation.FilePath != "" {
		if rw, err := NewRotatingFileWriter(logCfg.Rotation); err == nil {
			baseWriter = rw
		} else {
			fmt.Fprintf(os.Stderr, "log rotation init failed: %v — falling back to stdout\n", err)
		}
	}

	var output io.Writer
	if logCfg.JSONMode {
		output = NewSecretRedactionWriter(baseWriter)
	} else {
		output = NewSecretRedactionWriter(zerolog.ConsoleWriter{Out: baseWriter})
	}

	logger := log.Output(output)

	switch strings.ToLower(logCfg.Level) {
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
