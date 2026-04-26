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
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", command)
		os.Exit(1)
	}
}
