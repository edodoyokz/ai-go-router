package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/edodoyokz/9router-go/internal/app"
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
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", command)
		fmt.Fprintf(os.Stderr, "available commands: serve, version\n")
		os.Exit(1)
	}
}
