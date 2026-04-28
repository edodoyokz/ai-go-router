package mitm

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

// --- Proxy tests ---

func TestNewProxy_DefaultsApplied(t *testing.T) {
	p, err := NewProxy(Config{}, zerolog.Nop())
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}
	if p.cfg.UpstreamURL != "http://127.0.0.1:1988" {
		t.Errorf("unexpected default upstream: %s", p.cfg.UpstreamURL)
	}
	if p.cfg.ListenAddr != "127.0.0.1:8877" {
		t.Errorf("unexpected default listen addr: %s", p.cfg.ListenAddr)
	}
}

func TestNewProxy_InvalidUpstream(t *testing.T) {
	_, err := NewProxy(Config{UpstreamURL: "://bad-url"}, zerolog.Nop())
	if err == nil {
		t.Fatal("expected error for invalid upstream URL")
	}
}

func TestProxy_ForwardsToUpstream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	p, err := NewProxy(Config{UpstreamURL: upstream.URL, ListenAddr: "127.0.0.1:0"}, zerolog.Nop())
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{}`))
	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestProxy_InjectsAPIKey(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	p, err := NewProxy(Config{UpstreamURL: upstream.URL, APIKey: "test-key"}, zerolog.Nop())
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{}`))
	p.ServeHTTP(rec, req)

	if gotAuth != "Bearer test-key" {
		t.Errorf("expected 'Bearer test-key', got %q", gotAuth)
	}
}

func TestProxy_DoesNotOverrideExistingAuth(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	p, err := NewProxy(Config{UpstreamURL: upstream.URL, APIKey: "should-not-be-used"}, zerolog.Nop())
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer client-key")
	p.ServeHTTP(rec, req)

	if gotAuth != "Bearer client-key" {
		t.Errorf("expected client key to be preserved, got %q", gotAuth)
	}
}

// --- Cloaking tests ---

func TestCloakingMode_None_Passthrough(t *testing.T) {
	var gotUA string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
	})
	mw := NewCloakingMiddleware(CloakingModeNone, handler)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("User-Agent", "python-requests/2.31")
	mw.ServeHTTP(rec, req)

	// None mode does not alter headers
	if gotUA != "python-requests/2.31" {
		t.Errorf("expected original UA preserved in None mode, got %q", gotUA)
	}
}

func TestCloakingMode_Claude_StripsSdkHeaders(t *testing.T) {
	var gotStainless string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotStainless = r.Header.Get("X-Stainless-Lang")
		w.WriteHeader(http.StatusOK)
	})
	mw := NewCloakingMiddleware(CloakingModeClaude, handler)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("X-Stainless-Lang", "python")
	mw.ServeHTTP(rec, req)

	if gotStainless != "" {
		t.Errorf("expected X-Stainless-Lang to be stripped, got %q", gotStainless)
	}
}

func TestCloakingMode_Claude_RewritesUserAgent(t *testing.T) {
	var gotUA string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
	})
	mw := NewCloakingMiddleware(CloakingModeClaude, handler)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("User-Agent", "python-requests/2.31")
	mw.ServeHTTP(rec, req)

	if strings.Contains(gotUA, "python") {
		t.Errorf("expected python UA to be rewritten, got %q", gotUA)
	}
	if !strings.Contains(gotUA, "Mozilla") {
		t.Errorf("expected Mozilla UA, got %q", gotUA)
	}
}

func TestCloakingMode_Antigravity_RemovesTracingHeaders(t *testing.T) {
	var gotTraceparent string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTraceparent = r.Header.Get("traceparent")
		w.WriteHeader(http.StatusOK)
	})
	mw := NewCloakingMiddleware(CloakingModeAntigravity, handler)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("traceparent", "00-abc-def-01")
	mw.ServeHTTP(rec, req)

	if gotTraceparent != "" {
		t.Errorf("expected traceparent to be stripped, got %q", gotTraceparent)
	}
}

// --- ScrubResponseBody tests ---

func TestScrubResponseBody_RemovesFields(t *testing.T) {
	body := `{"id":"chatcmpl-123","model":"gpt-4","choices":[]}`
	resp := &http.Response{
		Header: http.Header{"Content-Type": []string{"application/json"}},
		Body:   io.NopCloser(bytes.NewBufferString(body)),
	}

	ScrubResponseBody(resp, []string{"id"})

	out, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(out), `"id"`) {
		t.Errorf("expected 'id' to be scrubbed, got: %s", string(out))
	}
	if !strings.Contains(string(out), `"model"`) {
		t.Errorf("expected 'model' to remain, got: %s", string(out))
	}
}

func TestScrubResponseBody_NonJSON_Passthrough(t *testing.T) {
	body := "plain text response"
	resp := &http.Response{
		Header: http.Header{"Content-Type": []string{"text/plain"}},
		Body:   io.NopCloser(bytes.NewBufferString(body)),
	}
	ScrubResponseBody(resp, []string{"id"})
	out, _ := io.ReadAll(resp.Body)
	if string(out) != body {
		t.Errorf("expected passthrough for non-JSON, got: %s", string(out))
	}
}
