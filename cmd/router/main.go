package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/edodoyokz/ai-go-router/internal/app"
	"github.com/edodoyokz/ai-go-router/internal/config"
	"github.com/edodoyokz/ai-go-router/internal/storage"
)

// Version information (set via -ldflags at build time)
var (
	version   = "dev"
	buildTime = "unknown"
	gitCommit = "unknown"
)

func main() {
	command, configPath, args, err := parseCommand(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	switch command {
	case "--version", "-version":
		fmt.Printf("router %s\n", version)
		return
	case "setup":
		if err := runSetup(configPath); err != nil {
			fmt.Fprintf(os.Stderr, "setup failed: %v\n", err)
			os.Exit(1)
		}
	case "init":
		fs := flag.NewFlagSet("init", flag.ExitOnError)
		force := fs.Bool("force", false, "overwrite existing config")
		if err := fs.Parse(args); err != nil {
			fmt.Fprintf(os.Stderr, "init failed: %v\n", err)
			os.Exit(1)
		}
		if err := runInit(configPath, *force); err != nil {
			fmt.Fprintf(os.Stderr, "init failed: %v\n", err)
			os.Exit(1)
		}
	case "serve":
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		if err := app.RunWithReload(ctx, configPath); err != nil {
			fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
			os.Exit(1)
		}
	case "version":
		fmt.Printf("router version %s\n", version)
		fmt.Printf("build time: %s\n", buildTime)
		fmt.Printf("git commit: %s\n", gitCommit)
	case "validate":
		if err := validateConfig(configPath); err != nil {
			fmt.Fprintf(os.Stderr, "config validation failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("config is valid")
	case "providers":
		if err := listProviders(configPath); err != nil {
			fmt.Fprintf(os.Stderr, "failed to list providers: %v\n", err)
			os.Exit(1)
		}
	case "routes":
		if err := listRoutes(configPath); err != nil {
			fmt.Fprintf(os.Stderr, "failed to list routes: %v\n", err)
			os.Exit(1)
		}
	case "logs":
		if err := tailLogs(configPath, args); err != nil {
			fmt.Fprintf(os.Stderr, "failed to tail logs: %v\n", err)
			os.Exit(1)
		}
	case "update":
		if err := runUpdate(); err != nil {
			fmt.Fprintf(os.Stderr, "update failed: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", command)
		fmt.Fprintf(os.Stderr, "available commands: serve, init, setup, update, version, validate, providers, routes, logs\n")
		os.Exit(1)
	}
}

func defaultConfigPath() string {
	defaultConfig := "./config/config.example.yaml"
	if home, err := os.UserHomeDir(); err == nil {
		userConfig := filepath.Join(home, ".config/router/config.yaml")
		if _, err := os.Stat(userConfig); err == nil {
			return userConfig
		}
	}
	return defaultConfig
}

func defaultUserConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config/router/config.yaml"), nil
}

func parseCommand(args []string) (string, string, []string, error) {
	configPath := defaultConfigPath()
	command := "serve"

	if len(args) > 0 && (args[0] == "--version" || args[0] == "-version") {
		return args[0], configPath, nil, nil
	}

	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		command = args[0]
		remaining, extractedConfigPath, err := extractConfigFlag(args[1:], configPath)
		if err != nil {
			return "", "", nil, err
		}
		return command, extractedConfigPath, remaining, nil
	}

	fs := flag.NewFlagSet("router", flag.ContinueOnError)
	fs.StringVar(&configPath, "config", configPath, "path to config file")
	if err := fs.Parse(args); err != nil {
		return "", "", nil, err
	}
	if fs.NArg() > 0 {
		command = fs.Arg(0)
		return command, configPath, fs.Args()[1:], nil
	}
	return command, configPath, nil, nil
}

func extractConfigFlag(args []string, configPath string) ([]string, string, error) {
	remaining := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--config":
			if i+1 >= len(args) {
				return nil, "", fmt.Errorf("--config requires a value")
			}
			configPath = args[i+1]
			i++
		case strings.HasPrefix(arg, "--config="):
			configPath = strings.TrimPrefix(arg, "--config=")
		default:
			remaining = append(remaining, arg)
		}
	}
	return remaining, configPath, nil
}

func validateConfig(configPath string) error {
	_, err := config.Load(configPath)
	return err
}

func listProviders(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	fmt.Println("Providers:")
	for _, p := range cfg.Providers {
		status := "enabled"
		if !p.Enabled {
			status = "disabled"
		}
		fmt.Printf("  - %s (%s): %s\n", p.Name, p.Type, status)
	}
	return nil
}

func listRoutes(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	fmt.Println("Routes:")
	for alias, route := range cfg.Routes {
		fmt.Printf("  - %s (strategy: %s):\n", alias, route.Strategy)
		for i, target := range route.Targets {
			fmt.Printf("      [%d] %s/%s (tier: %s)\n", i, target.Provider, target.Model, target.Tier)
		}
	}

	fmt.Println("\nModel Aliases:")
	for alias, aliasConfig := range cfg.ModelAliases {
		fmt.Printf("  - %s -> %s/%s\n", alias, aliasConfig.Provider, aliasConfig.Model)
	}
	return nil
}

func tailLogs(configPath string, args []string) error {
	// Parse flags for logs command
	dbPath := "./data/router.db"

	limit := 50
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	fs.StringVar(&dbPath, "db-path", dbPath, "path to SQLite database")
	fs.IntVar(&limit, "limit", 50, "number of logs to show")
	provider := fs.String("provider", "", "filter by provider")
	model := fs.String("model", "", "filter by model")
	status := fs.String("status", "", "filter by status")
	follow := fs.Bool("follow", false, "follow logs (poll for new entries)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Open database
	db, err := storage.NewDB(dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	params := storage.LogQueryParams{
		Limit:    limit,
		Offset:   0,
		Provider: *provider,
		Model:    *model,
		Status:   *status,
	}

	if *follow {
		return followLogs(db, params)
	}

	logs, total, err := db.QueryLogs(context.Background(), params)
	if err != nil {
		return fmt.Errorf("query logs: %w", err)
	}

	fmt.Printf("Showing %d of %d logs\n\n", len(logs), total)
	for _, log := range logs {
		fmt.Printf("[%s] %s | %s -> %s/%s | %s | %dms\n",
			log.StartTime.Format("2006-01-02 15:04:05"),
			log.RequestID,
			log.Model,
			log.Provider,
			log.TargetModel,
			log.Status,
			log.Duration.Milliseconds(),
		)
		if log.ErrorMessage != "" {
			fmt.Printf("  Error: %s\n", log.ErrorMessage)
		}
	}

	return nil
}

func followLogs(db *storage.DB, params storage.LogQueryParams) error {
	fmt.Println("Following logs (Ctrl+C to stop)...")

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	lastTime := time.Now().Unix()
	for {
		select {
		case <-ctx.Done():
			fmt.Println("\nStopped following logs")
			return nil
		default:
			params.StartTime = lastTime
			logs, _, err := db.QueryLogs(context.Background(), params)
			if err != nil {
				fmt.Fprintf(os.Stderr, "query error: %v\n", err)
				time.Sleep(2 * time.Second)
				continue
			}

			for _, log := range logs {
				fmt.Printf("[%s] %s | %s -> %s/%s | %s | %dms\n",
					log.StartTime.Format("2006-01-02 15:04:05.000"),
					log.RequestID,
					log.Model,
					log.Provider,
					log.TargetModel,
					log.Status,
					log.Duration.Milliseconds(),
				)
				if log.ErrorMessage != "" {
					fmt.Printf("  Error: %s\n", log.ErrorMessage)
				}
				if log.StartTime.Unix() > lastTime {
					lastTime = log.StartTime.Unix()
				}
			}

			time.Sleep(2 * time.Second)
		}
	}
}
