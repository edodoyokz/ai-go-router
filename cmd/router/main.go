package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/edodoyokz/9router-go/internal/app"
	"github.com/edodoyokz/9router-go/internal/config"
	"github.com/edodoyokz/9router-go/internal/storage"
)

// Version information (set via -ldflags at build time)
var (
	version   = "dev"
	buildTime = "unknown"
	gitCommit = "unknown"
)

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "./config/config.example.yaml", "path to config file")
	flag.Parse()

	command := "serve"
	if flag.NArg() > 0 {
		command = flag.Arg(0)
	}

	switch command {
	case "serve":
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		if err := app.Run(ctx, configPath); err != nil {
			fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
			os.Exit(1)
		}
	case "version":
		fmt.Printf("9router-go version %s\n", version)
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
		if err := tailLogs(configPath); err != nil {
			fmt.Fprintf(os.Stderr, "failed to tail logs: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", command)
		fmt.Fprintf(os.Stderr, "available commands: serve, version, validate, providers, routes, logs\n")
		os.Exit(1)
	}
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

func tailLogs(configPath string) error {
	// Parse flags for logs command
	dbPath := configPath
	if dbPath == "./config/config.example.yaml" {
		dbPath = "./data/9router.db"
	}

	limit := 50
	flag.StringVar(&dbPath, "db-path", dbPath, "path to SQLite database")
	flag.IntVar(&limit, "limit", 50, "number of logs to show")
	provider := flag.String("provider", "", "filter by provider")
	model := flag.String("model", "", "filter by model")
	status := flag.String("status", "", "filter by status")
	follow := flag.Bool("follow", false, "follow logs (poll for new entries)")
	flag.Parse()

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
				if log.StartTime.Unix() > lastTime {
					lastTime = log.StartTime.Unix()
				}
			}

			time.Sleep(2 * time.Second)
		}
	}
}
