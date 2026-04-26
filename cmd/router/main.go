package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/edodoyokz/9router-go/internal/app"
	"github.com/edodoyokz/9router-go/internal/config"
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
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", command)
		fmt.Fprintf(os.Stderr, "available commands: serve, version, validate, providers, routes\n")
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
