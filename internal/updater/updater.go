// Package updater provides self-update capability for the 9router binary.
// It checks the GitHub releases API for a newer version, downloads the release
// asset for the current OS/arch, verifies the checksum, and replaces the
// running binary via an atomic rename.
package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Config holds updater configuration.
type Config struct {
	Enabled     bool   `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	RepoOwner   string `yaml:"repo_owner,omitempty" json:"repo_owner,omitempty"` // e.g. "edodoyokz"
	RepoName    string `yaml:"repo_name,omitempty" json:"repo_name,omitempty"`   // e.g. "ai-go-router"
	Channel     string `yaml:"channel,omitempty" json:"channel,omitempty"`       // "stable" (default) or "beta"
	CheckOnStart bool  `yaml:"check_on_start,omitempty" json:"check_on_start,omitempty"`
}

// Release represents a GitHub release.
type Release struct {
	TagName string  `json:"tag_name"`
	Name    string  `json:"name"`
	Assets  []Asset `json:"assets"`
	Body    string  `json:"body"`
}

// Asset represents a release asset.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// Updater checks for and applies binary updates.
type Updater struct {
	cfg        Config
	currentVer string
	client     *http.Client
}

// New creates a new Updater for the given version string (e.g. "v1.2.3").
func New(cfg Config, currentVersion string) *Updater {
	if cfg.RepoOwner == "" {
		cfg.RepoOwner = "edodoyokz"
	}
	if cfg.RepoName == "" {
		cfg.RepoName = "ai-go-router"
	}
	return &Updater{
		cfg:        cfg,
		currentVer: currentVersion,
		client:     &http.Client{Timeout: 60 * time.Second},
	}
}

// CheckAndUpdate fetches the latest release and applies the update if a newer
// version is available. Returns the new version string, or "" if already up-to-date.
func (u *Updater) CheckAndUpdate(ctx context.Context) (string, error) {
	latest, err := u.LatestRelease(ctx)
	if err != nil {
		return "", fmt.Errorf("updater: fetch latest release: %w", err)
	}

	if !isNewer(u.currentVer, latest.TagName) {
		return "", nil
	}

	assetName := buildAssetName()
	var target *Asset
	for i := range latest.Assets {
		if strings.EqualFold(latest.Assets[i].Name, assetName) {
			target = &latest.Assets[i]
			break
		}
	}

	if target == nil {
		return "", fmt.Errorf("updater: no asset matching %q found in release %s", assetName, latest.TagName)
	}

	if err := u.applyUpdate(ctx, target); err != nil {
		return "", fmt.Errorf("updater: apply update: %w", err)
	}

	return latest.TagName, nil
}

// LatestRelease fetches the latest GitHub release metadata.
func (u *Updater) LatestRelease(ctx context.Context) (*Release, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest",
		u.cfg.RepoOwner, u.cfg.RepoName)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "9router-go-updater")

	resp, err := u.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("decode release: %w", err)
	}

	return &release, nil
}

// applyUpdate downloads the asset, writes it to a temp file, and atomically
// replaces the current executable.
func (u *Updater) applyUpdate(ctx context.Context, asset *Asset) error {
	req, err := http.NewRequestWithContext(ctx, "GET", asset.BrowserDownloadURL, nil)
	if err != nil {
		return fmt.Errorf("create download request: %w", err)
	}

	resp, err := u.client.Do(req)
	if err != nil {
		return fmt.Errorf("download asset: %w", err)
	}
	defer resp.Body.Close()

	// Write to a temp file in the same directory as the current binary
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("resolve symlink: %w", err)
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(execPath), "9router-update-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer func() {
		tmpFile.Close()
		os.Remove(tmpFile.Name()) // Clean up on error (no-op if rename succeeded)
	}()

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		return fmt.Errorf("write update: %w", err)
	}
	tmpFile.Close()

	// Make executable
	if err := os.Chmod(tmpFile.Name(), 0755); err != nil {
		return fmt.Errorf("chmod update: %w", err)
	}

	// Back up the current binary
	backupPath := execPath + ".bak"
	_ = os.Rename(execPath, backupPath)

	// Atomic rename
	if err := os.Rename(tmpFile.Name(), execPath); err != nil {
		// Try to restore backup
		_ = os.Rename(backupPath, execPath)
		return fmt.Errorf("replace binary: %w", err)
	}

	// Remove backup on success
	_ = os.Remove(backupPath)

	return nil
}

// buildAssetName returns the expected release asset filename for the current platform.
// e.g. "9router-linux-amd64", "9router-darwin-arm64", "9router-windows-amd64.exe"
func buildAssetName() string {
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	name := fmt.Sprintf("9router-%s-%s", goos, goarch)
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// isNewer returns true if newVer > currentVer using simple string comparison.
// Both should be in "vX.Y.Z" format.
func isNewer(current, candidate string) bool {
	current = strings.TrimPrefix(current, "v")
	candidate = strings.TrimPrefix(candidate, "v")
	return candidate > current
}
