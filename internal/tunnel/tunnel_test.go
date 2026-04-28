package tunnel

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/edodoyokz/ai-go-router/internal/config"
	"github.com/rs/zerolog"
)

type fakeRunner struct {
	mu       sync.Mutex
	starts   []string
	runs     []string
	binaries map[string]bool
}

func (f *fakeRunner) LookPath(file string) (string, error) {
	if f.binaries[file] {
		return "/usr/bin/" + file, nil
	}
	return "", errors.New("not found")
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) (CommandResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs = append(f.runs, strings.Join(append([]string{name}, args...), " "))
	return CommandResult{Stdout: "ok", ExitCode: 0}, nil
}

func (f *fakeRunner) Start(_ context.Context, name string, args ...string) (Process, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts = append(f.starts, strings.Join(append([]string{name}, args...), " "))
	logs := make(chan string, 4)
	return &fakeProcess{running: true, done: make(chan struct{}), logs: logs}, nil
}

type fakeProcess struct {
	mu      sync.RWMutex
	running bool
	done    chan struct{}
	logs    chan string
}

func (p *fakeProcess) Wait() error {
	<-p.done
	return nil
}

func (p *fakeProcess) Kill() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.running {
		p.running = false
		close(p.done)
	}
	return nil
}

func (p *fakeProcess) Running() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.running
}

func (p *fakeProcess) Logs() <-chan string {
	return p.logs
}

func TestManagerCheckTailscaleUsesRunner(t *testing.T) {
	t.Setenv("NINEROUTER_TUNNEL_STATE", filepath.Join(t.TempDir(), "state.json"))
	runner := &fakeRunner{binaries: map[string]bool{"tailscale": true}}
	manager := NewManagerWithRunner(config.TunnelConfig{}, zerolog.Nop(), runner)

	status := manager.CheckTailscale(context.Background())
	if !status.Installed || !status.Running || !status.LoggedIn {
		t.Fatalf("unexpected tailscale status %#v", status)
	}
	if len(runner.runs) != 1 || runner.runs[0] != "tailscale status --json" {
		t.Fatalf("runs=%#v", runner.runs)
	}
}

func TestManagerEnableDisableCloudflare(t *testing.T) {
	t.Setenv("NINEROUTER_TUNNEL_STATE", filepath.Join(t.TempDir(), "state.json"))
	runner := &fakeRunner{binaries: map[string]bool{}}
	manager := NewManagerWithRunner(config.TunnelConfig{Provider: "cloudflare"}, zerolog.Nop(), runner)

	if err := manager.Enable(context.Background(), "cloudflare", "127.0.0.1:1988"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	for i := 0; i < 100; i++ {
		if manager.Status().Running {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !manager.Status().Enabled || !manager.Status().Running {
		t.Fatalf("status after enable %#v", manager.Status())
	}
	if len(runner.starts) != 1 || runner.starts[0] != "cloudflared tunnel --url http://127.0.0.1:1988" {
		t.Fatalf("starts=%#v", runner.starts)
	}
	if err := manager.Disable(context.Background()); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if manager.Status().Running || manager.Status().Enabled {
		t.Fatalf("status after disable %#v", manager.Status())
	}
}

func TestManagerPersistsStateAndParsesQuickTunnelURL(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	t.Setenv("NINEROUTER_TUNNEL_STATE", statePath)
	manager := NewManagerWithRunner(config.TunnelConfig{Provider: "cloudflare"}, zerolog.Nop(), &fakeRunner{})
	if err := manager.Enable(context.Background(), "cloudflare", "127.0.0.1:1988"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	var proc *fakeProcess
	for i := 0; i < 100; i++ {
		manager.mu.RLock()
		proc, _ = manager.proc.(*fakeProcess)
		manager.mu.RUnlock()
		if proc != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if proc == nil {
		t.Fatal("process not started")
	}
	proc.logs <- "INF quick tunnel at https://api.trycloudflare.com then https://abc-def.trycloudflare.com"
	for i := 0; i < 100; i++ {
		if manager.Status().URL == "https://abc-def.trycloudflare.com" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if manager.Status().URL != "https://abc-def.trycloudflare.com" {
		t.Fatalf("url=%q", manager.Status().URL)
	}
	reloaded := NewManagerWithRunner(config.TunnelConfig{}, zerolog.Nop(), &fakeRunner{})
	if reloaded.Status().URL != "https://abc-def.trycloudflare.com" {
		t.Fatalf("persisted url=%q", reloaded.Status().URL)
	}
}
