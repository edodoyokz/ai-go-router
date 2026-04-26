package mitm

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// CloakingMode controls how the request/response headers and body are transformed
// to avoid pattern-based bans from AI providers.
type CloakingMode string

const (
	CloakingModeNone        CloakingMode = ""
	CloakingModeClaude      CloakingMode = "claude"      // anti-ban for Anthropic
	CloakingModeAntigravity CloakingMode = "antigravity" // generic stealth transforms
)

// CloakingMiddleware wraps an http.Handler and applies request/response transforms
// based on the configured cloaking mode.
type CloakingMiddleware struct {
	mode CloakingMode
	next http.Handler
}

// NewCloakingMiddleware returns a new middleware.  Pass CloakingModeNone to
// install a pass-through that still normalises suspicious headers.
func NewCloakingMiddleware(mode CloakingMode, next http.Handler) *CloakingMiddleware {
	return &CloakingMiddleware{mode: mode, next: next}
}

func (m *CloakingMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch m.mode {
	case CloakingModeClaude:
		m.applyClaude(r)
	case CloakingModeAntigravity:
		m.applyAntigravity(r)
	}
	m.next.ServeHTTP(w, r)
}

// applyClaude strips or rewrites headers that Anthropic's abuse-detection uses
// to identify automated / non-browser clients sending requests to claude.ai.
func (m *CloakingMiddleware) applyClaude(r *http.Request) {
	// Remove fingerprinting headers
	for _, h := range []string{
		"X-Stainless-Lang",
		"X-Stainless-Package-Version",
		"X-Stainless-Runtime",
		"X-Stainless-Runtime-Version",
		"X-Stainless-Async",
		"X-Stainless-OS",
		"X-Stainless-Arch",
	} {
		r.Header.Del(h)
	}

	// Rewrite User-Agent to something browser-like
	ua := r.Header.Get("User-Agent")
	if ua == "" || strings.HasPrefix(ua, "python-") || strings.HasPrefix(ua, "Go-http") {
		r.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	}

	// Mask origin to look like browser
	if r.Header.Get("Origin") == "" {
		r.Header.Set("Origin", "https://claude.ai")
	}

	// Anthropic SDK sets this — strip it to avoid SDK detection
	r.Header.Del("anthropic-version")
}

// applyAntigravity applies generic anti-fingerprinting transforms suitable for
// any provider (strips SDK headers, normalises User-Agent, removes tracing).
func (m *CloakingMiddleware) applyAntigravity(r *http.Request) {
	// Remove common SDK / automation fingerprints
	for _, h := range []string{
		"X-Stainless-Lang",
		"X-Stainless-Package-Version",
		"X-Stainless-Runtime",
		"X-Stainless-Runtime-Version",
		"X-Stainless-Async",
		"X-Stainless-OS",
		"X-Stainless-Arch",
		"X-Request-Id",
		"traceparent",
		"tracestate",
		"baggage",
	} {
		r.Header.Del(h)
	}

	// Generic browser-like User-Agent
	ua := r.Header.Get("User-Agent")
	if ua == "" || strings.Contains(strings.ToLower(ua), "python") ||
		strings.Contains(strings.ToLower(ua), "go-http") {
		r.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	}

	// Normalise Accept header
	if r.Header.Get("Accept") == "" {
		r.Header.Set("Accept", "application/json, */*")
	}
}

// ScrubResponseBody removes fields from a JSON response body that may contain
// provider-specific identifiers or abuse signals before forwarding to the client.
// It only operates on JSON content-type; other bodies are passed through unchanged.
func ScrubResponseBody(resp *http.Response, stripFields []string) {
	if resp == nil || resp.Body == nil {
		return
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		return
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}
	_ = resp.Body.Close()

	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		// Not valid JSON; restore original body
		resp.Body = io.NopCloser(strings.NewReader(string(raw)))
		return
	}

	for _, f := range stripFields {
		delete(body, f)
	}

	scrubbed, err := json.Marshal(body)
	if err != nil {
		resp.Body = io.NopCloser(strings.NewReader(string(raw)))
		return
	}

	resp.Body = io.NopCloser(strings.NewReader(string(scrubbed)))
	resp.ContentLength = int64(len(scrubbed))
}
