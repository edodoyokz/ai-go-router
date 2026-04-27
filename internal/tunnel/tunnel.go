// Package tunnel provides support for exposing the router to the internet
// via Cloudflare Tunnel (cloudflared) or Tailscale Funnel.
package tunnel

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/rs/zerolog"

	"github.com/edodoyokz/ai-go-router/internal/config"
)

// Manager manages the lifecycle of a tunnel process.
type Manager struct {
	cfg    config.TunnelConfig
	logger zerolog.Logger
	cmd    *exec.Cmd
}

// NewManager creates a tunnel manager from the given config.
func NewManager(cfg config.TunnelConfig, logger zerolog.Logger) *Manager {
	return &Manager{cfg: cfg, logger: logger}
}

// Start launches the tunnel subprocess and blocks until ctx is cancelled.
// Returns nil if tunnels are disabled.
func (m *Manager) Start(ctx context.Context, localAddr string) error {
	if !m.cfg.Enabled {
		return nil
	}

	switch m.cfg.Provider {
	case "cloudflare":
		return m.startCloudflare(ctx, localAddr)
	case "tailscale":
		return m.startTailscale(ctx, localAddr)
	default:
		return fmt.Errorf("tunnel: unsupported provider %q (supported: cloudflare, tailscale)", m.cfg.Provider)
	}
}

// startCloudflare runs `cloudflared tunnel --url <localAddr>` or a named tunnel
// when hostname + auth_token are configured.
func (m *Manager) startCloudflare(ctx context.Context, localAddr string) error {
	var args []string

	if m.cfg.Hostname != "" && m.cfg.AuthToken != "" {
		// Named tunnel with credentials
		args = []string{
			"tunnel",
			"--credentials-file", "/dev/stdin",
			"run",
			"--url", "http://" + localAddr,
			m.cfg.Hostname,
		}
		m.logger.Info().
			Str("provider", "cloudflare").
			Str("hostname", m.cfg.Hostname).
			Msg("starting cloudflare named tunnel")
	} else {
		// Quick tunnel (no account required)
		args = []string{"tunnel", "--url", "http://" + localAddr}
		m.logger.Info().
			Str("provider", "cloudflare").
			Msg("starting cloudflare quick tunnel")
	}

	m.cmd = exec.CommandContext(ctx, "cloudflared", args...)

	if err := m.cmd.Start(); err != nil {
		return fmt.Errorf("tunnel: failed to start cloudflared: %w (ensure cloudflared is installed and in PATH)", err)
	}

	m.logger.Info().Msg("cloudflare tunnel process started")

	if err := m.cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return nil // context cancelled — expected shutdown
		}
		return fmt.Errorf("tunnel: cloudflared exited unexpectedly: %w", err)
	}
	return nil
}

// startTailscale runs `tailscale funnel <port>` to expose the local server.
func (m *Manager) startTailscale(ctx context.Context, localAddr string) error {
	// Extract port from localAddr (host:port)
	port := localAddr
	for i := len(localAddr) - 1; i >= 0; i-- {
		if localAddr[i] == ':' {
			port = localAddr[i+1:]
			break
		}
	}

	args := []string{"funnel", port}
	if m.cfg.Hostname != "" {
		args = append(args, "--bg")
	}

	m.logger.Info().
		Str("provider", "tailscale").
		Str("port", port).
		Msg("starting tailscale funnel")

	m.cmd = exec.CommandContext(ctx, "tailscale", args...)

	if err := m.cmd.Start(); err != nil {
		return fmt.Errorf("tunnel: failed to start tailscale funnel: %w (ensure tailscale is installed and authenticated)", err)
	}

	m.logger.Info().Msg("tailscale funnel process started")

	if err := m.cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("tunnel: tailscale exited unexpectedly: %w", err)
	}
	return nil
}

// Stop sends a signal to the tunnel subprocess to shut down gracefully.
func (m *Manager) Stop() {
	if m.cmd != nil && m.cmd.Process != nil {
		_ = m.cmd.Process.Kill()
	}
}

// IsRunning reports whether the tunnel subprocess is currently active.
func (m *Manager) IsRunning() bool {
	return m.cmd != nil && m.cmd.Process != nil && m.cmd.ProcessState == nil
}

// TunnelStatus holds runtime status of the tunnel manager.
type TunnelStatus struct {
	Enabled  bool   `json:"enabled"`
	Provider string `json:"provider,omitempty"`
	Running  bool   `json:"running"`
}

// Status returns the current tunnel status.
func (m *Manager) Status() TunnelStatus {
	return TunnelStatus{
		Enabled:  m.cfg.Enabled,
		Provider: m.cfg.Provider,
		Running:  m.IsRunning(),
	}
}
