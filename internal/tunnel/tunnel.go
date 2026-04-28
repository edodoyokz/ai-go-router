// Package tunnel provides support for exposing the router to the internet
// via Cloudflare Tunnel (cloudflared) or Tailscale Funnel.
package tunnel

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/edodoyokz/ai-go-router/internal/config"
)

// Manager manages the lifecycle of a tunnel process.
type Manager struct {
	cfg       config.TunnelConfig
	logger    zerolog.Logger
	runner    CommandRunner
	cancel    context.CancelFunc
	proc      Process
	statePath string
	mu        sync.RWMutex
	wg        sync.WaitGroup
	state     TunnelStatus
}

var quickTunnelURLPattern = regexp.MustCompile(`https://([a-z0-9-]+)\.trycloudflare\.com`)

// NewManager creates a tunnel manager from the given config.
func NewManager(cfg config.TunnelConfig, logger zerolog.Logger) *Manager {
	return NewManagerWithRunner(cfg, logger, ExecRunner{})
}

func NewManagerWithRunner(cfg config.TunnelConfig, logger zerolog.Logger, runner CommandRunner) *Manager {
	if runner == nil {
		runner = ExecRunner{}
	}
	manager := &Manager{
		cfg:       cfg,
		logger:    logger,
		runner:    runner,
		statePath: defaultStatePath(),
		state: TunnelStatus{
			Enabled:   cfg.Enabled,
			Provider:  cfg.Provider,
			Hostname:  cfg.Hostname,
			Running:   false,
			UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		},
	}
	manager.loadState()
	return manager
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

	proc, err := m.runner.Start(ctx, "cloudflared", args...)
	if err != nil {
		m.setError(err.Error())
		return fmt.Errorf("tunnel: failed to start cloudflared: %w (ensure cloudflared is installed and in PATH)", err)
	}
	m.setProcess("cloudflare", proc, "")

	m.logger.Info().Msg("cloudflare tunnel process started")

	if err := proc.Wait(); err != nil {
		if ctx.Err() != nil {
			return nil // context cancelled — expected shutdown
		}
		m.setStopped(err.Error())
		return fmt.Errorf("tunnel: cloudflared exited unexpectedly: %w", err)
	}
	m.setStopped("")
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

	proc, err := m.runner.Start(ctx, "tailscale", args...)
	if err != nil {
		m.setError(err.Error())
		return fmt.Errorf("tunnel: failed to start tailscale funnel: %w (ensure tailscale is installed and authenticated)", err)
	}
	m.setProcess("tailscale", proc, "")

	m.logger.Info().Msg("tailscale funnel process started")

	if err := proc.Wait(); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		m.setStopped(err.Error())
		return fmt.Errorf("tunnel: tailscale exited unexpectedly: %w", err)
	}
	m.setStopped("")
	return nil
}

// Stop sends a signal to the tunnel subprocess to shut down gracefully.
func (m *Manager) Stop() {
	m.mu.Lock()
	cancel := m.cancel
	proc := m.proc
	m.cancel = nil
	m.proc = nil
	m.state.Running = false
	m.state.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	m.saveStateLocked()
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if proc != nil {
		_ = proc.Kill()
	}
}

// IsRunning reports whether the tunnel subprocess is currently active.
func (m *Manager) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.proc != nil && m.proc.Running()
}

// TunnelStatus holds runtime status of the tunnel manager.
type TunnelStatus struct {
	Enabled      bool            `json:"enabled"`
	Provider     string          `json:"provider,omitempty"`
	Hostname     string          `json:"hostname,omitempty"`
	Running      bool            `json:"running"`
	URL          string          `json:"url,omitempty"`
	LastError    string          `json:"lastError,omitempty"`
	UpdatedAt    string          `json:"updatedAt,omitempty"`
	Tailscale    TailscaleStatus `json:"tailscale"`
	Download     DownloadStatus  `json:"download"`
	Capabilities []string        `json:"capabilities,omitempty"`
	Metadata     map[string]any  `json:"metadata,omitempty"`
}

type TailscaleStatus struct {
	Installed bool   `json:"installed"`
	LoggedIn  bool   `json:"loggedIn"`
	Running   bool   `json:"running"`
	URL       string `json:"url,omitempty"`
}

type DownloadStatus struct {
	Installed bool   `json:"installed"`
	Path      string `json:"path,omitempty"`
}

// Status returns the current tunnel status.
func (m *Manager) Status() TunnelStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	status := m.state
	if m.proc != nil {
		status.Running = m.proc.Running()
	}
	return status
}

func (m *Manager) CheckTailscale(ctx context.Context) TailscaleStatus {
	status := TailscaleStatus{}
	if _, err := m.runner.LookPath("tailscale"); err == nil {
		status.Installed = true
	}
	if result, err := m.runner.Run(ctx, "tailscale", "status", "--json"); err == nil && result.ExitCode == 0 {
		status.Running = true
		status.LoggedIn = true
	}
	m.mu.Lock()
	m.state.Tailscale = status
	m.state.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	m.saveStateLocked()
	m.mu.Unlock()
	return status
}

func (m *Manager) LoginTailscale(ctx context.Context) (CommandResult, error) {
	return m.runner.Run(ctx, "tailscale", "login")
}

func (m *Manager) InstallTailscale(ctx context.Context) (CommandResult, error) {
	if _, err := m.runner.LookPath("tailscale"); err == nil {
		return CommandResult{ExitCode: 0, Stdout: "tailscale already installed"}, nil
	}
	return m.runner.Run(ctx, "sh", "-c", "curl -fsSL https://tailscale.com/install.sh | sh")
}

func (m *Manager) StartDaemon(ctx context.Context) (CommandResult, error) {
	if _, err := m.runner.LookPath("tailscaled"); err == nil {
		return m.runner.Run(ctx, "tailscaled")
	}
	return m.runner.Run(ctx, "tailscale", "up")
}

func (m *Manager) Enable(ctx context.Context, provider, localAddr string) error {
	if strings.TrimSpace(provider) == "" {
		provider = m.cfg.Provider
	}
	if strings.TrimSpace(provider) == "" {
		provider = "cloudflare"
	}
	cfg := m.cfg
	cfg.Enabled = true
	cfg.Provider = provider
	m.cfg = cfg
	runCtx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.cancel = cancel
	m.state.Enabled = true
	m.state.Provider = provider
	m.state.Hostname = cfg.Hostname
	m.state.LastError = ""
	m.state.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	m.saveStateLocked()
	m.mu.Unlock()
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		if err := m.Start(runCtx, localAddr); err != nil && ctx.Err() == nil {
			m.logger.Warn().Err(err).Str("provider", provider).Msg("tunnel enable failed")
		}
	}()
	return nil
}

func (m *Manager) Disable(context.Context) error {
	m.Stop()
	m.wg.Wait()
	m.mu.Lock()
	m.cfg.Enabled = false
	m.state.Enabled = false
	m.state.Running = false
	m.state.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	m.saveStateLocked()
	m.mu.Unlock()
	return nil
}

func (m *Manager) setProcess(provider string, proc Process, url string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.proc = proc
	m.state.Provider = provider
	m.state.Running = true
	if url != "" {
		m.state.URL = url
	}
	m.state.LastError = ""
	m.state.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	m.saveStateLocked()
	if logs, ok := proc.(LogProcess); ok {
		go m.watchProcessLogs(logs.Logs())
	}
}

func (m *Manager) setError(message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.LastError = message
	m.state.Running = false
	m.state.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	m.saveStateLocked()
}

func (m *Manager) setStopped(message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.proc = nil
	m.state.Running = false
	m.state.LastError = message
	m.state.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	m.saveStateLocked()
}

func (m *Manager) watchProcessLogs(logs <-chan string) {
	for line := range logs {
		if url := quickTunnelURL(line); url != "" {
			m.mu.Lock()
			m.state.URL = url
			if m.state.Metadata == nil {
				m.state.Metadata = map[string]any{}
			}
			m.state.Metadata["tunnelUrl"] = url
			m.state.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
			m.saveStateLocked()
			m.mu.Unlock()
		}
	}
}

func quickTunnelURL(message string) string {
	matches := quickTunnelURLPattern.FindAllStringSubmatch(message, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		if len(matches[i]) >= 2 && matches[i][1] != "api" {
			return matches[i][0]
		}
	}
	return ""
}

func defaultStatePath() string {
	if override := strings.TrimSpace(os.Getenv("NINEROUTER_TUNNEL_STATE")); override != "" {
		return override
	}
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "9router", "tunnel-state.json")
	}
	return filepath.Join(os.TempDir(), "9router-tunnel-state.json")
}

func (m *Manager) loadState() {
	raw, err := os.ReadFile(m.statePath)
	if err != nil {
		return
	}
	var state TunnelStatus
	if err := json.Unmarshal(raw, &state); err != nil {
		return
	}
	if state.Provider == "" {
		state.Provider = m.cfg.Provider
	}
	if state.UpdatedAt == "" {
		state.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	m.state = state
}

func (m *Manager) saveStateLocked() {
	if m.statePath == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(m.statePath), 0o755); err != nil {
		return
	}
	raw, err := json.MarshalIndent(m.state, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(m.statePath, raw, 0o600)
}

type CommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type Process interface {
	Wait() error
	Kill() error
	Running() bool
}

type LogProcess interface {
	Process
	Logs() <-chan string
}

type CommandRunner interface {
	LookPath(file string) (string, error)
	Run(ctx context.Context, name string, args ...string) (CommandResult, error)
	Start(ctx context.Context, name string, args ...string) (Process, error)
}

type ExecRunner struct{}

func (ExecRunner) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) (CommandResult, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := CommandResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: 0}
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	return result, err
}

func (ExecRunner) Start(ctx context.Context, name string, args ...string) (Process, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	proc := &execProcess{cmd: cmd, logs: make(chan string, 32)}
	go proc.scanOutput(stdout)
	go proc.scanOutput(stderr)
	return proc, nil
}

type execProcess struct {
	cmd  *exec.Cmd
	mu   sync.RWMutex
	logs chan string
}

func (p *execProcess) Wait() error {
	err := p.cmd.Wait()
	p.mu.Lock()
	defer p.mu.Unlock()
	return err
}

func (p *execProcess) Kill() error {
	if p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Kill()
}

func (p *execProcess) Running() bool {
	return p.cmd.Process != nil && p.cmd.ProcessState == nil
}

func (p *execProcess) Logs() <-chan string {
	return p.logs
}

func (p *execProcess) scanOutput(r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		select {
		case p.logs <- scanner.Text():
		default:
		}
	}
}
