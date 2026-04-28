package mitm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/edodoyokz/ai-go-router/internal/tunnel"
)

const DefaultRouterBaseURL = "http://localhost:20128"

type Manager struct {
	mu              sync.RWMutex
	runner          tunnel.CommandRunner
	process         tunnel.Process
	running         bool
	pid             int
	certExists      bool
	certTrusted     bool
	dnsStatus       map[string]bool
	aliases         map[string]map[string]string
	hasCachedSudo   bool
	routerBaseURL   string
	lastError       string
	lastUpdatedTime string
	statePath       string
}

func NewManager(runner tunnel.CommandRunner) *Manager {
	if runner == nil {
		runner = tunnel.ExecRunner{}
	}
	manager := &Manager{
		runner:          runner,
		dnsStatus:       map[string]bool{},
		aliases:         map[string]map[string]string{},
		routerBaseURL:   DefaultRouterBaseURL,
		lastUpdatedTime: time.Now().UTC().Format(time.RFC3339Nano),
	}
	manager.statePath = defaultStatePath()
	manager.loadState()
	return manager
}

type Status struct {
	Running           bool            `json:"running"`
	PID               any             `json:"pid"`
	CertExists        bool            `json:"certExists"`
	CertTrusted       bool            `json:"certTrusted"`
	DNSStatus         map[string]bool `json:"dnsStatus"`
	HasCachedPassword bool            `json:"hasCachedPassword"`
	IsAdmin           bool            `json:"isAdmin"`
	MITMRouterBaseURL string          `json:"mitmRouterBaseUrl"`
	LastError         string          `json:"lastError,omitempty"`
	UpdatedAt         string          `json:"updatedAt,omitempty"`
}

func (m *Manager) Status() Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.process != nil {
		m.running = m.process.Running()
	}
	dns := make(map[string]bool, len(m.dnsStatus))
	for k, v := range m.dnsStatus {
		dns[k] = v
	}
	var pid any
	if m.pid > 0 {
		pid = m.pid
	}
	return Status{
		Running:           m.running,
		PID:               pid,
		CertExists:        m.certExists,
		CertTrusted:       m.certTrusted,
		DNSStatus:         dns,
		HasCachedPassword: m.hasCachedSudo,
		IsAdmin:           runtime.GOOS != "windows",
		MITMRouterBaseURL: m.routerBaseURL,
		LastError:         m.lastError,
		UpdatedAt:         m.lastUpdatedTime,
	}
}

func (m *Manager) Start(ctx context.Context, apiKey, sudoPassword, routerBaseURL string) (Status, error) {
	if strings.TrimSpace(apiKey) == "" {
		return m.Status(), fmt.Errorf("Missing apiKey")
	}
	if runtime.GOOS != "windows" && strings.TrimSpace(sudoPassword) == "" {
		return m.Status(), fmt.Errorf("Missing apiKey or sudoPassword")
	}
	if routerBaseURL != "" {
		normalized, err := NormalizeRouterBaseURL(routerBaseURL)
		if err != nil {
			return m.Status(), err
		}
		m.mu.Lock()
		m.routerBaseURL = normalized
		m.mu.Unlock()
	}
	proc, err := m.runner.Start(ctx, "antigravity-mitm", "--router-base-url", m.Status().MITMRouterBaseURL)
	if err != nil {
		m.setError(err.Error())
		return m.Status(), err
	}
	m.mu.Lock()
	m.process = proc
	m.running = true
	m.certExists = true
	m.hasCachedSudo = runtime.GOOS != "windows" && sudoPassword != ""
	m.lastError = ""
	m.lastUpdatedTime = time.Now().UTC().Format(time.RFC3339Nano)
	m.saveStateLocked()
	m.mu.Unlock()
	return m.Status(), nil
}

func (m *Manager) Stop(_ context.Context, sudoPassword string) (Status, error) {
	if runtime.GOOS != "windows" && strings.TrimSpace(sudoPassword) == "" && !m.Status().HasCachedPassword {
		return m.Status(), fmt.Errorf("Missing sudoPassword")
	}
	m.mu.Lock()
	proc := m.process
	m.process = nil
	m.running = false
	for tool := range m.dnsStatus {
		m.dnsStatus[tool] = false
	}
	if sudoPassword != "" {
		m.hasCachedSudo = true
	}
	m.lastUpdatedTime = time.Now().UTC().Format(time.RFC3339Nano)
	m.saveStateLocked()
	m.mu.Unlock()
	if proc != nil {
		_ = proc.Kill()
	}
	return m.Status(), nil
}

func (m *Manager) EnableDNS(_ context.Context, tool, sudoPassword string) (Status, error) {
	if strings.TrimSpace(tool) == "" {
		return m.Status(), fmt.Errorf("tool and action required")
	}
	if runtime.GOOS != "windows" && strings.TrimSpace(sudoPassword) == "" && !m.Status().HasCachedPassword {
		return m.Status(), fmt.Errorf("Missing sudoPassword")
	}
	m.mu.Lock()
	m.dnsStatus[tool] = true
	if sudoPassword != "" {
		m.hasCachedSudo = true
	}
	m.lastUpdatedTime = time.Now().UTC().Format(time.RFC3339Nano)
	m.saveStateLocked()
	m.mu.Unlock()
	return m.Status(), nil
}

func (m *Manager) DisableDNS(_ context.Context, tool, sudoPassword string) (Status, error) {
	if strings.TrimSpace(tool) == "" {
		return m.Status(), fmt.Errorf("tool and action required")
	}
	if runtime.GOOS != "windows" && strings.TrimSpace(sudoPassword) == "" && !m.Status().HasCachedPassword {
		return m.Status(), fmt.Errorf("Missing sudoPassword")
	}
	m.mu.Lock()
	m.dnsStatus[tool] = false
	if sudoPassword != "" {
		m.hasCachedSudo = true
	}
	m.lastUpdatedTime = time.Now().UTC().Format(time.RFC3339Nano)
	m.saveStateLocked()
	m.mu.Unlock()
	return m.Status(), nil
}

func (m *Manager) TrustCert(_ context.Context, sudoPassword string) (Status, error) {
	if runtime.GOOS != "windows" && strings.TrimSpace(sudoPassword) == "" && !m.Status().HasCachedPassword {
		return m.Status(), fmt.Errorf("Missing sudoPassword")
	}
	m.mu.Lock()
	m.certExists = true
	m.certTrusted = true
	if sudoPassword != "" {
		m.hasCachedSudo = true
	}
	m.lastUpdatedTime = time.Now().UTC().Format(time.RFC3339Nano)
	m.saveStateLocked()
	m.mu.Unlock()
	return m.Status(), nil
}

func (m *Manager) Aliases(tool string) map[string]map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := map[string]map[string]string{}
	for t, mappings := range m.aliases {
		if tool != "" && t != tool {
			continue
		}
		out[t] = copyStringMap(mappings)
	}
	return out
}

func (m *Manager) SetAliases(tool string, mappings map[string]string) (map[string]string, error) {
	if strings.TrimSpace(tool) == "" {
		return nil, fmt.Errorf("tool and mappings required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.dnsStatus[tool] {
		return nil, fmt.Errorf("DNS must be enabled for %s before editing model mappings", tool)
	}
	filtered := map[string]string{}
	for alias, model := range mappings {
		if strings.TrimSpace(alias) != "" && strings.TrimSpace(model) != "" {
			filtered[strings.TrimSpace(alias)] = strings.TrimSpace(model)
		}
	}
	m.aliases[tool] = filtered
	m.lastUpdatedTime = time.Now().UTC().Format(time.RFC3339Nano)
	m.saveStateLocked()
	return copyStringMap(filtered), nil
}

func NormalizeRouterBaseURL(input string) (string, error) {
	if strings.TrimSpace(input) == "" {
		return DefaultRouterBaseURL, nil
	}
	trimmed := strings.TrimRight(strings.TrimSpace(input), "/")
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("Invalid MITM router URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("MITM router URL must use http or https")
	}
	return trimmed, nil
}

func (m *Manager) setError(message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastError = message
	m.running = false
	m.lastUpdatedTime = time.Now().UTC().Format(time.RFC3339Nano)
	m.saveStateLocked()
}

type persistedState struct {
	CertExists        bool                         `json:"certExists"`
	CertTrusted       bool                         `json:"certTrusted"`
	DNSStatus         map[string]bool              `json:"dnsStatus"`
	Aliases           map[string]map[string]string `json:"aliases"`
	HasCachedPassword bool                         `json:"hasCachedPassword"`
	MITMRouterBaseURL string                       `json:"mitmRouterBaseUrl"`
	LastError         string                       `json:"lastError,omitempty"`
	UpdatedAt         string                       `json:"updatedAt,omitempty"`
}

func defaultStatePath() string {
	if override := strings.TrimSpace(os.Getenv("NINEROUTER_MITM_STATE")); override != "" {
		return override
	}
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "9router", "mitm-state.json")
	}
	return filepath.Join(os.TempDir(), "9router-mitm-state.json")
}

func (m *Manager) loadState() {
	raw, err := os.ReadFile(m.statePath)
	if err != nil {
		return
	}
	var state persistedState
	if err := json.Unmarshal(raw, &state); err != nil {
		return
	}
	if state.DNSStatus != nil {
		m.dnsStatus = state.DNSStatus
	}
	if state.Aliases != nil {
		m.aliases = state.Aliases
	}
	if state.MITMRouterBaseURL != "" {
		m.routerBaseURL = state.MITMRouterBaseURL
	}
	m.certExists = state.CertExists
	m.certTrusted = state.CertTrusted
	m.hasCachedSudo = state.HasCachedPassword
	m.lastError = state.LastError
	if state.UpdatedAt != "" {
		m.lastUpdatedTime = state.UpdatedAt
	}
}

func (m *Manager) saveStateLocked() {
	if m.statePath == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(m.statePath), 0o755); err != nil {
		return
	}
	raw, err := json.MarshalIndent(persistedState{
		CertExists:        m.certExists,
		CertTrusted:       m.certTrusted,
		DNSStatus:         m.dnsStatus,
		Aliases:           m.aliases,
		HasCachedPassword: m.hasCachedSudo,
		MITMRouterBaseURL: m.routerBaseURL,
		LastError:         m.lastError,
		UpdatedAt:         m.lastUpdatedTime,
	}, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(m.statePath, raw, 0o600)
}

func copyStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
