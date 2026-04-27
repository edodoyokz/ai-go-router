package nodes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
)

func TestRoundRobinPersistsAcrossCalls(t *testing.T) {
	// Two mock nodes that always return 200
	hits := make([]int, 2)
	servers := make([]*httptest.Server, 2)
	for i := range servers {
		idx := i
		servers[i] = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits[idx]++
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"id":"1","choices":[]}`))
		}))
		defer servers[i].Close()
	}

	reg := NewRegistry([]NodeConfig{
		{Name: "n1", BaseURL: servers[0].URL, Enabled: true, Weight: 1},
		{Name: "n2", BaseURL: servers[1].URL, Enabled: true, Weight: 1},
	}, zerolog.Nop())

	body := []byte(`{"model":"gpt-4","messages":[]}`)
	for i := 0; i < 4; i++ {
		reg.Forward(context.Background(), body) //nolint:errcheck
	}

	// Each node should have been hit exactly 2 times (round-robin over 4 calls, 2 nodes)
	if hits[0] == 0 || hits[1] == 0 {
		t.Fatalf("expected both nodes to receive requests, got hits: %v", hits)
	}
	if hits[0]+hits[1] != 4 {
		t.Fatalf("expected 4 total hits, got %d", hits[0]+hits[1])
	}
}

func TestForwardSkipsUnhealthyNode(t *testing.T) {
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"1","choices":[]}`))
	}))
	defer healthy.Close()

	reg := NewRegistry([]NodeConfig{
		{Name: "sick", BaseURL: "http://127.0.0.1:1", Enabled: true, Weight: 1},
		{Name: "ok", BaseURL: healthy.URL, Enabled: true, Weight: 1},
	}, zerolog.Nop())

	// Mark first node unhealthy
	reg.nodes[0].healthy.Store(false)

	body := []byte(`{"model":"gpt-4","messages":[]}`)
	_, err := reg.Forward(context.Background(), body)
	if err != nil {
		t.Fatalf("expected forward to succeed via healthy node, got: %v", err)
	}
}

func TestForwardNoHealthyNodes(t *testing.T) {
	reg := NewRegistry([]NodeConfig{
		{Name: "sick", BaseURL: "http://127.0.0.1:1", Enabled: true, Weight: 1},
	}, zerolog.Nop())
	reg.nodes[0].healthy.Store(false)

	body := []byte(`{"model":"gpt-4","messages":[]}`)
	_, err := reg.Forward(context.Background(), body)
	if err == nil {
		t.Fatal("expected error when no healthy nodes, got nil")
	}
}

func TestHealthyNodesReturnsNames(t *testing.T) {
	reg := NewRegistry([]NodeConfig{
		{Name: "a", BaseURL: "http://a", Enabled: true},
		{Name: "b", BaseURL: "http://b", Enabled: true},
	}, zerolog.Nop())
	reg.nodes[1].healthy.Store(false)

	names := reg.HealthyNodes()
	if len(names) != 1 || names[0] != "a" {
		t.Fatalf("expected [a], got %v", names)
	}
}
