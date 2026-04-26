package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/edodoyokz/9router-go/internal/config"
	"github.com/edodoyokz/9router-go/internal/providers"
	routing "github.com/edodoyokz/9router-go/internal/router"
)

type Server struct {
	config config.Config
	logger zerolog.Logger
	engine *routing.Engine
}

func NewServer(cfg config.Config, logger zerolog.Logger, engine *routing.Engine) *Server {
	return &Server{config: cfg, logger: logger, engine: engine}
}

func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(s.requestLogger)
	r.Get("/healthz", s.handleHealthz)
	r.Get("/readyz", s.handleReadyz)
	r.Post("/v1/chat/completions", s.handleChatCompletions)
	return r
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleReadyz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ready"})
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if !s.isAuthorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]any{"message": "unauthorized"}})
		return
	}

	var request providers.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "invalid json"}})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(s.config.Server.RequestTimeoutSeconds)*time.Second)
	defer cancel()

	response, providerName, err := s.engine.ChatCompletion(ctx, request)
	if err != nil {
		s.logger.Error().Err(err).Str("model", request.Model).Msg("chat completion failed")
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": map[string]any{"message": err.Error()}})
		return
	}

	w.Header().Set("X-Router-Provider", providerName)
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		s.logger.Info().
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Dur("duration", time.Since(started)).
			Msg("request handled")
	})
}

func (s *Server) isAuthorized(r *http.Request) bool {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header == "" {
		return false
	}
	expected := "Bearer " + s.config.Server.APIKey
	return header == expected
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	addr := fmt.Sprintf("%s:%d", s.config.Server.Host, s.config.Server.Port)
	server := &http.Server{
		Addr:    addr,
		Handler: s.Handler(),
	}

	errCh := make(chan error, 1)
	go func() {
		s.logger.Info().Str("addr", addr).Msg("server starting")
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		s.logger.Info().Msg("server shutting down")
		return server.Shutdown(shutdownCtx)
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
