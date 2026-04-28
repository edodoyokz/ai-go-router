package mitm

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/edodoyokz/ai-go-router/internal/tunnel"
)

type fakeRunner struct {
	started bool
}

func (f *fakeRunner) LookPath(file string) (string, error) { return "", errors.New("not used") }

func (f *fakeRunner) Run(ctx context.Context, name string, args ...string) (tunnel.CommandResult, error) {
	return tunnel.CommandResult{ExitCode: 0}, nil
}

func (f *fakeRunner) Start(ctx context.Context, name string, args ...string) (tunnel.Process, error) {
	f.started = true
	return &fakeProcess{running: true, done: make(chan struct{})}, nil
}

type fakeProcess struct {
	mu      sync.RWMutex
	running bool
	done    chan struct{}
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

func TestManagerStartStopAndDNS(t *testing.T) {
	t.Setenv("NINEROUTER_MITM_STATE", filepath.Join(t.TempDir(), "state.json"))
	runner := &fakeRunner{}
	manager := NewManager(runner)
	status, err := manager.Start(context.Background(), "key", "sudo", "http://localhost:20128/")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if !runner.started || !status.Running || !status.CertExists || status.MITMRouterBaseURL != "http://localhost:20128" {
		t.Fatalf("status after start %#v", status)
	}

	status, err = manager.EnableDNS(context.Background(), "antigravity", "")
	if err != nil {
		t.Fatalf("enable dns: %v", err)
	}
	if !status.DNSStatus["antigravity"] {
		t.Fatalf("dns not enabled %#v", status.DNSStatus)
	}
	aliases, err := manager.SetAliases("antigravity", map[string]string{"fast": "openai/gpt-4.1-mini", "blank": ""})
	if err != nil {
		t.Fatalf("set aliases: %v", err)
	}
	if aliases["fast"] != "openai/gpt-4.1-mini" || len(aliases) != 1 {
		t.Fatalf("aliases=%#v", aliases)
	}

	status, err = manager.Stop(context.Background(), "")
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	if status.Running || status.DNSStatus["antigravity"] {
		t.Fatalf("status after stop %#v", status)
	}
}

func TestManagerPersistsDNSAndAliases(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	t.Setenv("NINEROUTER_MITM_STATE", statePath)
	manager := NewManager(&fakeRunner{})
	if _, err := manager.EnableDNS(context.Background(), "antigravity", "sudo"); err != nil {
		t.Fatalf("enable dns: %v", err)
	}
	if _, err := manager.SetAliases("antigravity", map[string]string{"fast": "openai/gpt"}); err != nil {
		t.Fatalf("set aliases: %v", err)
	}

	reloaded := NewManager(&fakeRunner{})
	status := reloaded.Status()
	if !status.DNSStatus["antigravity"] || !status.HasCachedPassword {
		t.Fatalf("status=%#v", status)
	}
	if got := reloaded.Aliases("antigravity")["antigravity"]["fast"]; got != "openai/gpt" {
		t.Fatalf("alias=%q", got)
	}
}

func TestNormalizeRouterBaseURL(t *testing.T) {
	got, err := NormalizeRouterBaseURL("https://example.test///")
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if got != "https://example.test" {
		t.Fatalf("got %q", got)
	}
	if _, err := NormalizeRouterBaseURL("ftp://example.test"); err == nil {
		t.Fatal("expected invalid scheme")
	}
}
