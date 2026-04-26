package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/edodoyokz/9router-go/internal/config"
	"github.com/edodoyokz/9router-go/internal/storage"
)

func newTestServer(t *testing.T) (*Server, *storage.DB, func()) {
	t.Helper()

	f, err := os.CreateTemp(t.TempDir(), "test-*.db")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	db, err := storage.NewDB(f.Name())
	if err != nil {
		t.Fatal(err)
	}

	logger := zerolog.Nop()
	asyncWriter := storage.NewAsyncWriter(db, logger)

	cfg := config.Config{
		Server: config.ServerConfig{
			APIKey:                "test-key",
			RequestTimeoutSeconds: 30,
		},
	}

	s := &Server{
		config:      cfg,
		logger:      logger,
		asyncWriter: asyncWriter,
		metrics: &Metrics{
			ProviderUsage: make(map[string]int64),
		},
	}

	return s, db, func() {
		asyncWriter.Close()
	}
}

func TestHandleHealthz(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	s.handleHealthz(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("expected status ok, got %v", resp["status"])
	}
}

func TestHandleReadyz(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/readyz", nil)
	w := httptest.NewRecorder()
	s.handleReadyz(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandleLogsList_Empty(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/logs", nil)
	w := httptest.NewRecorder()
	s.handleLogsList(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["total"].(float64) != 0 {
		t.Errorf("expected total 0, got %v", resp["total"])
	}
}

func TestHandleLogsList_WithData(t *testing.T) {
	s, db, cleanup := newTestServer(t)
	defer cleanup()

	now := time.Now()
	for i := 0; i < 5; i++ {
		if err := db.LogRequest(t.Context(), &storage.RequestLog{
			RequestID:   "req-" + string(rune('0'+i)),
			Model:       "gpt-4",
			Provider:    "openai",
			TargetModel: "gpt-4",
			Status:      "success",
			StartTime:   now,
			EndTime:     now,
			Duration:    50 * time.Millisecond,
		}); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("default limit", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/logs", nil)
		w := httptest.NewRecorder()
		s.handleLogsList(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}

		var resp map[string]any
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if resp["total"].(float64) != 5 {
			t.Errorf("expected total 5, got %v", resp["total"])
		}
	})

	t.Run("with limit param", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/logs?limit=2", nil)
		w := httptest.NewRecorder()
		s.handleLogsList(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}

		var resp map[string]any
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		logs := resp["logs"].([]any)
		if len(logs) != 2 {
			t.Errorf("expected 2 logs, got %d", len(logs))
		}
	})

	t.Run("filter by provider", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/logs?provider=openai", nil)
		w := httptest.NewRecorder()
		s.handleLogsList(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}

		var resp map[string]any
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if resp["total"].(float64) != 5 {
			t.Errorf("expected total 5, got %v", resp["total"])
		}
	})
}

func TestHandleMetrics(t *testing.T) {
	s, _, cleanup := newTestServer(t)
	defer cleanup()

	s.metrics.mu.Lock()
	s.metrics.RequestsTotal = 10
	s.metrics.RequestsSuccess = 8
	s.metrics.RequestsError = 2
	s.metrics.mu.Unlock()

	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	s.handleMetrics(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	body := w.Body.String()
	if len(body) == 0 {
		t.Error("expected non-empty metrics body")
	}
}

func TestCORSMiddleware(t *testing.T) {
	handler := CORSMiddleware(
		[]string{"https://example.com"},
		[]string{"GET", "POST", "OPTIONS"},
		[]string{"Authorization", "Content-Type"},
		false,
		86400,
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("allowed origin", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Origin", "https://example.com")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Header().Get("Access-Control-Allow-Origin") != "https://example.com" {
			t.Errorf("expected CORS header, got %q", w.Header().Get("Access-Control-Allow-Origin"))
		}
	})

	t.Run("disallowed origin", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Origin", "https://evil.com")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("expected no CORS header for disallowed origin")
		}
	})

	t.Run("preflight request", func(t *testing.T) {
		req := httptest.NewRequest("OPTIONS", "/", nil)
		req.Header.Set("Origin", "https://example.com")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusNoContent {
			t.Errorf("expected 204 for preflight, got %d", w.Code)
		}
	})
}

func TestCORSMiddleware_Disabled(t *testing.T) {
	handler := CORSMiddleware([]string{}, nil, nil, false, 0)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://example.com")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("expected no CORS headers when disabled")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}
