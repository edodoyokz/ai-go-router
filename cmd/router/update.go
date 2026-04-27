package main

import (
	"context"
	"fmt"

	"github.com/edodoyokz/ai-go-router/internal/updater"
)

func runUpdate() error {
	fmt.Printf("Checking for updates (current: %s)...\n", version)

	u := updater.New(updater.Config{
		Enabled:     true,
		RepoOwner:   "edodoyokz",
		RepoName:    "ai-go-router",
		Channel:     "stable",
	}, version)

	newVer, err := u.CheckAndUpdate(context.Background())
	if err != nil {
		return fmt.Errorf("update check failed: %w", err)
	}

	if newVer == "" {
		fmt.Printf("Already up to date (%s)\n", version)
		return nil
	}

	fmt.Printf("Updated to %s — please restart router\n", newVer)
	return nil
}
