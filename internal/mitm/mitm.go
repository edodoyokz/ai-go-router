// Package mitm provides a man-in-the-middle HTTP/HTTPS proxy that intercepts
// AI API calls from local tools (Cursor, Claude Code, etc.) and forwards them
// through the router engine for routing, fallback, and logging.
package mitm

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// Config holds configuration for the MITM proxy.
type Config struct {
	Enabled     bool   `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	ListenAddr  string `yaml:"listen_addr,omitempty" json:"listen_addr,omitempty"`   // e.g. "127.0.0.1:8877"
	UpstreamURL string `yaml:"upstream_url,omitempty" json:"upstream_url,omitempty"` // router base URL
	// TLSCert and TLSKey are optional; if omitted the proxy runs in plain HTTP mode.
	TLSCert string `yaml:"tls_cert,omitempty" json:"tls_cert,omitempty"`
	TLSKey  string `yaml:"tls_key,omitempty" json:"tls_key,omitempty"`
	// APIKey forwarded to the upstream router.
	APIKey string `yaml:"api_key,omitempty" json:"api_key,omitempty"`
}

// Proxy is an HTTP reverse-proxy that intercepts AI API requests and
// forwards them to the local router instance.
type Proxy struct {
	cfg    Config
	logger zerolog.Logger
	rp     *httputil.ReverseProxy
	srv    *http.Server
}

// NewProxy creates a new MITM proxy from config.
func NewProxy(cfg Config, logger zerolog.Logger) (*Proxy, error) {
	if cfg.UpstreamURL == "" {
		cfg.UpstreamURL = "http://127.0.0.1:1988"
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "127.0.0.1:8877"
	}

	upstream, err := url.Parse(cfg.UpstreamURL)
	if err != nil {
		return nil, fmt.Errorf("mitm: invalid upstream URL: %w", err)
	}

	rp := httputil.NewSingleHostReverseProxy(upstream)

	// Customize the director to rewrite Host + inject auth
	rp.Director = func(req *http.Request) {
		req.URL.Scheme = upstream.Scheme
		req.URL.Host = upstream.Host
		req.Host = upstream.Host

		// Inject the router's API key if not already set
		if cfg.APIKey != "" && req.Header.Get("Authorization") == "" {
			req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
		}

		// Mark the request as coming from MITM proxy
		req.Header.Set("X-9Router-MITM", "1")
	}

	rp.ErrorHandler = func(w http.ResponseWriter, req *http.Request, err error) {
		logger.Error().Err(err).Str("path", req.URL.Path).Msg("mitm: upstream error")
		http.Error(w, fmt.Sprintf("upstream error: %v", err), http.StatusBadGateway)
	}

	p := &Proxy{cfg: cfg, logger: logger, rp: rp}
	p.srv = &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      p,
		ReadTimeout:  120 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	return p, nil
}

// ServeHTTP handles incoming proxy requests.
// CONNECT tunnelling (for HTTPS) is handled separately; all other requests
// are forwarded to the upstream router.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		p.handleTunnel(w, r)
		return
	}

	p.logger.Debug().
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Str("remote", r.RemoteAddr).
		Msg("mitm: forwarding request")

	p.rp.ServeHTTP(w, r)
}

// handleTunnel handles HTTPS CONNECT tunnelling by establishing a TCP connection
// to the upstream and bridging the two connections.
func (p *Proxy) handleTunnel(w http.ResponseWriter, r *http.Request) {
	// Determine target: if host looks like an AI API endpoint, redirect to upstream.
	target := r.Host
	if isAIEndpoint(target) {
		upstream, _ := url.Parse(p.cfg.UpstreamURL)
		target = upstream.Host
		if !strings.Contains(target, ":") {
			target += ":443"
		}
	}

	destConn, err := net.DialTimeout("tcp", target, 10*time.Second)
	if err != nil {
		http.Error(w, fmt.Sprintf("cannot connect to %s: %v", target, err), http.StatusBadGateway)
		return
	}
	defer destConn.Close()

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		p.logger.Error().Err(err).Msg("mitm: hijack failed")
		return
	}
	defer clientConn.Close()

	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(destConn, clientConn)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(clientConn, destConn)
		done <- struct{}{}
	}()
	<-done
}

// isAIEndpoint returns true if the host looks like a known AI provider endpoint
// that should be intercepted and redirected to the local router.
func isAIEndpoint(host string) bool {
	known := []string{
		"api.openai.com",
		"api.anthropic.com",
		"generativelanguage.googleapis.com",
		"api.groq.com",
		"api.deepseek.com",
		"api.mistral.ai",
		"openrouter.ai",
	}
	// Strip port if present
	h := host
	if idx := strings.LastIndex(h, ":"); idx >= 0 {
		h = h[:idx]
	}
	for _, k := range known {
		if strings.EqualFold(h, k) {
			return true
		}
	}
	return false
}

// ListenAndServe starts the MITM proxy. Blocks until the server stops.
func (p *Proxy) ListenAndServe() error {
	if p.cfg.TLSCert != "" && p.cfg.TLSKey != "" {
		cert, err := tls.LoadX509KeyPair(p.cfg.TLSCert, p.cfg.TLSKey)
		if err != nil {
			return fmt.Errorf("mitm: load TLS cert: %w", err)
		}
		p.srv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
		p.logger.Info().Str("addr", p.cfg.ListenAddr).Msg("mitm proxy listening (TLS)")
		return p.srv.ListenAndServeTLS("", "")
	}
	p.logger.Info().Str("addr", p.cfg.ListenAddr).Msg("mitm proxy listening (HTTP)")
	return p.srv.ListenAndServe()
}

// Close shuts down the proxy server.
func (p *Proxy) Close() error {
	return p.srv.Close()
}
