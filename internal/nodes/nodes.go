// Package nodes implements distributed provider node discovery and forwarding.
// Each 9router instance can register itself as a node and forward requests to
// peer nodes, enabling a multi-node mesh where each node exposes its own
// providers to the rest of the cluster.
package nodes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
)

// NodeConfig describes a single remote 9router node.
type NodeConfig struct {
	Name    string `yaml:"name" json:"name"`
	BaseURL string `yaml:"base_url" json:"base_url"` // e.g. "http://host2:20128"
	APIKey  string `yaml:"api_key,omitempty" json:"api_key,omitempty"`
	Enabled bool   `yaml:"enabled" json:"enabled"`
	// Weight controls how often this node is selected (default 1)
	Weight int `yaml:"weight,omitempty" json:"weight,omitempty"`
}

// Registry manages remote provider nodes.
type Registry struct {
	mu        sync.RWMutex
	nodes     []*nodeState
	logger    zerolog.Logger
	client    *http.Client
	globalIdx atomic.Uint64
}

type nodeState struct {
	cfg     NodeConfig
	healthy atomic.Bool
}

// NewRegistry creates a node registry from config.
func NewRegistry(nodes []NodeConfig, logger zerolog.Logger) *Registry {
	r := &Registry{
		logger: logger,
		client: &http.Client{Timeout: 60 * time.Second},
	}
	for _, cfg := range nodes {
		if !cfg.Enabled {
			continue
		}
		ns := &nodeState{cfg: cfg}
		ns.healthy.Store(true)
		r.nodes = append(r.nodes, ns)
	}
	return r
}

// StartHealthChecks periodically pings all nodes and marks them healthy/unhealthy.
func (r *Registry) StartHealthChecks(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.checkAll(ctx)
		}
	}
}

func (r *Registry) checkAll(ctx context.Context) {
	r.mu.RLock()
	nodes := r.nodes
	r.mu.RUnlock()

	for _, ns := range nodes {
		go func(ns *nodeState) {
			checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(checkCtx, "GET", ns.cfg.BaseURL+"/healthz", nil)
			if err != nil {
				ns.healthy.Store(false)
				return
			}
			if ns.cfg.APIKey != "" {
				req.Header.Set("Authorization", "Bearer "+ns.cfg.APIKey)
			}

			resp, err := r.client.Do(req)
			if err != nil || resp.StatusCode != http.StatusOK {
				ns.healthy.Store(false)
				r.logger.Warn().Str("node", ns.cfg.Name).Msg("node unhealthy")
				return
			}
			resp.Body.Close()
			ns.healthy.Store(true)
		}(ns)
	}
}

// Forward sends a chat completion request to the next healthy node using
// round-robin selection weighted by NodeConfig.Weight.
func (r *Registry) Forward(ctx context.Context, body []byte) ([]byte, error) {
	r.mu.RLock()
	nodes := r.nodes
	r.mu.RUnlock()

	// Build candidate list (healthy nodes, repeated Weight times)
	var candidates []*nodeState
	for _, ns := range nodes {
		if !ns.healthy.Load() {
			continue
		}
		weight := ns.cfg.Weight
		if weight <= 0 {
			weight = 1
		}
		for i := 0; i < weight; i++ {
			candidates = append(candidates, ns)
		}
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("nodes: no healthy nodes available")
	}

	// Global round-robin across candidates using persistent counter on Registry
	idx := r.globalIdx.Add(1) - 1
	ns := candidates[idx%uint64(len(candidates))]

	return r.forwardTo(ctx, ns, body)
}

func (r *Registry) forwardTo(ctx context.Context, ns *nodeState, body []byte) ([]byte, error) {
	url := ns.cfg.BaseURL + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("nodes: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if ns.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+ns.cfg.APIKey)
	}
	req.Header.Set("X-9Router-Forwarded-From", "node-mesh")

	resp, err := r.client.Do(req)
	if err != nil {
		ns.healthy.Store(false)
		return nil, fmt.Errorf("nodes: request to %s: %w", ns.cfg.Name, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("nodes: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("nodes: %s returned %d: %s", ns.cfg.Name, resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// HealthyNodes returns names of currently healthy nodes.
func (r *Registry) HealthyNodes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var names []string
	for _, ns := range r.nodes {
		if ns.healthy.Load() {
			names = append(names, ns.cfg.Name)
		}
	}
	return names
}

// ListNodes returns all node configs.
func (r *Registry) ListNodes() []NodeConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]NodeConfig, len(r.nodes))
	for i, ns := range r.nodes {
		out[i] = ns.cfg
	}
	return out
}

// MarshalJSON implements json.Marshaler for status reporting.
func (r *Registry) MarshalJSON() ([]byte, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	type nodeStatus struct {
		Name    string `json:"name"`
		BaseURL string `json:"base_url"`
		Healthy bool   `json:"healthy"`
		Weight  int    `json:"weight"`
	}
	var statuses []nodeStatus
	for _, ns := range r.nodes {
		statuses = append(statuses, nodeStatus{
			Name:    ns.cfg.Name,
			BaseURL: ns.cfg.BaseURL,
			Healthy: ns.healthy.Load(),
			Weight:  ns.cfg.Weight,
		})
	}
	return json.Marshal(statuses)
}
