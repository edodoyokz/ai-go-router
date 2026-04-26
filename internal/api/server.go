package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/edodoyokz/9router-go/internal/config"
	"github.com/edodoyokz/9router-go/internal/providers"
	routing "github.com/edodoyokz/9router-go/internal/router"
	"github.com/edodoyokz/9router-go/internal/storage"
	"github.com/edodoyokz/9router-go/internal/translator"
)

type Server struct {
	config      config.Config
	logger      zerolog.Logger
	engine      *routing.Engine
	translators *translator.Registry
	asyncWriter *storage.AsyncWriter
	metrics     *Metrics
}

type Metrics struct {
	mu              sync.RWMutex
	RequestsTotal   int64
	RequestsSuccess int64
	RequestsError   int64
	ProviderUsage   map[string]int64 // provider name -> count
}

func NewServer(cfg config.Config, logger zerolog.Logger, engine *routing.Engine, asyncWriter *storage.AsyncWriter) *Server {
	return &Server{
		config:      cfg,
		logger:      logger,
		engine:      engine,
		translators: translator.NewRegistry(),
		asyncWriter: asyncWriter,
		metrics: &Metrics{
			ProviderUsage: make(map[string]int64),
		},
	}
}

func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()

	// Global middleware (applied to all routes)
	r.Use(RequestIDMiddleware)
	r.Use(PanicRecoveryMiddleware(s.logger))
	r.Use(SecurityHeadersMiddleware)
	r.Use(StructuredLoggingMiddleware(s.logger))

	// Public routes (no auth required)
	r.Get("/healthz", s.handleHealthz)
	r.Get("/readyz", s.handleReadyz)
	r.Get("/metrics", s.handleMetrics)

	// Protected routes (auth required)
	r.Group(func(r chi.Router) {
		r.Use(AuthMiddleware(s.config.Server.APIKey))
		r.Get("/v1/models", s.handleModels)
		r.Post("/v1/chat/completions", s.handleChatCompletions)
		r.Post("/v1/messages", s.handleMessages)
		r.Post("/v1/responses", s.handleResponses)
		r.Get("/api/providers", s.handleProvidersList)
		r.Post("/api/providers", s.handleProvidersCreate)
		r.Put("/api/providers/{name}", s.handleProvidersUpdate)
		r.Delete("/api/providers/{name}", s.handleProvidersDelete)
		r.Get("/api/combos", s.handleCombosList)
		r.Post("/api/combos", s.handleCombosCreate)
		r.Put("/api/combos/{name}", s.handleCombosUpdate)
		r.Delete("/api/combos/{name}", s.handleCombosDelete)
		r.Get("/api/keys", s.handleKeysList)
		r.Post("/api/keys", s.handleKeysCreate)
		r.Put("/api/keys/{id}", s.handleKeysUpdate)
		r.Delete("/api/keys/{id}", s.handleKeysDelete)
		r.Get("/api/models/alias", s.handleModelAliasesList)
		r.Post("/api/models/alias", s.handleModelAliasesCreate)
		r.Put("/api/models/alias/{name}", s.handleModelAliasesUpdate)
		r.Delete("/api/models/alias/{name}", s.handleModelAliasesDelete)
		r.Get("/api/models/custom", s.handleModelsCustomList)
		r.Post("/api/models/custom", s.handleModelsCustomCreate)
		r.Put("/api/models/custom/{name}", s.handleModelsCustomUpdate)
		r.Delete("/api/models/custom/{name}", s.handleModelsCustomDelete)
		r.Get("/api/settings", s.handleSettingsGet)
		r.Put("/api/settings", s.handleSettingsPut)
		r.Get("/api/logs", s.handleLogsList)
		r.Get("/api/usage", s.handleUsage)
		r.Get("/api/providers/{name}/health", s.handleProviderHealth)
	})

	return r
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleReadyz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ready"})
}

func (s *Server) handleProviderHealth(w http.ResponseWriter, r *http.Request) {
	providerName := chi.URLParam(r, "name")

	// Find provider in config
	var provider *config.ProviderConfig
	for i := range s.config.Providers {
		if s.config.Providers[i].Name == providerName {
			provider = &s.config.Providers[i]
			break
		}
	}

	if provider == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"status": "error",
			"error":  "provider not found",
		})
		return
	}

	health := map[string]any{
		"provider": provider.Name,
		"status":   "healthy",
		"enabled":  provider.Enabled,
	}

	// Check if provider is enabled
	if !provider.Enabled {
		health["status"] = "disabled"
		health["reason"] = "provider is disabled in config"
		writeJSON(w, http.StatusOK, health)
		return
	}

	// Check if provider has API keys configured
	if provider.APIKey == "" && len(provider.Accounts) == 0 {
		health["status"] = "unhealthy"
		health["reason"] = "no API keys configured"
		writeJSON(w, http.StatusOK, health)
		return
	}

	// Check if provider is in cooldown (requires access to cooldown tracker)
	// For now, we'll skip this as it requires passing cooldown tracker to server
	// This can be added later when needed

	writeJSON(w, http.StatusOK, health)
}

func (s *Server) handleProvidersList(w http.ResponseWriter, r *http.Request) {
	// Return list of providers from config
	providers := make([]map[string]any, 0, len(s.config.Providers))
	for _, provider := range s.config.Providers {
		p := map[string]any{
			"name":     provider.Name,
			"type":     provider.Type,
			"base_url": provider.BaseURL,
			"enabled":  provider.Enabled,
		}
		// Don't expose API keys in the list
		providers = append(providers, p)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"providers": providers,
		"count":     len(providers),
	})
}

func (s *Server) handleProvidersCreate(w http.ResponseWriter, r *http.Request) {
	// Dynamic provider creation requires runtime registry management and persistence
	// Deferred for MVP - use YAML config instead
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"error":   "dynamic provider creation not implemented",
		"message": "For MVP, add providers to config YAML and restart the server",
	})
}

func (s *Server) handleProvidersUpdate(w http.ResponseWriter, r *http.Request) {
	// Dynamic provider updates require runtime registry management and persistence
	// Deferred for MVP - use YAML config instead
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"error":   "dynamic provider updates not implemented",
		"message": "For MVP, update providers in config YAML and restart the server",
	})
}

func (s *Server) handleProvidersDelete(w http.ResponseWriter, r *http.Request) {
	// Dynamic provider deletion requires runtime registry management and persistence
	// Deferred for MVP - use YAML config instead
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"error":   "dynamic provider deletion not implemented",
		"message": "For MVP, remove providers from config YAML and restart the server",
	})
}

func (s *Server) handleCombosList(w http.ResponseWriter, r *http.Request) {
	// Return list of combos (routes) from config
	combos := make([]map[string]any, 0, len(s.config.Routes))
	for name, route := range s.config.Routes {
		c := map[string]any{
			"name":     name,
			"strategy": route.Strategy,
			"targets":  route.Targets,
		}
		combos = append(combos, c)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"combos": combos,
		"count":  len(combos),
	})
}

func (s *Server) handleCombosCreate(w http.ResponseWriter, r *http.Request) {
	// Dynamic combo creation requires runtime config management and persistence
	// Deferred for MVP - use YAML config instead
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"error":   "dynamic combo creation not implemented",
		"message": "For MVP, add combos to config YAML and restart the server",
	})
}

func (s *Server) handleCombosUpdate(w http.ResponseWriter, r *http.Request) {
	// Dynamic combo updates require runtime config management and persistence
	// Deferred for MVP - use YAML config instead
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"error":   "dynamic combo updates not implemented",
		"message": "For MVP, update combos in config YAML and restart the server",
	})
}

func (s *Server) handleCombosDelete(w http.ResponseWriter, r *http.Request) {
	// Dynamic combo deletion requires runtime config management and persistence
	// Deferred for MVP - use YAML config instead
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"error":   "dynamic combo deletion not implemented",
		"message": "For MVP, remove combos from config YAML and restart the server",
	})
}

func (s *Server) handleKeysList(w http.ResponseWriter, r *http.Request) {
	// Return current API key from config (masked)
	// For MVP, single API key - multi-key support deferred
	maskedKey := "sk-****"
	if len(s.config.Server.APIKey) > 8 {
		maskedKey = s.config.Server.APIKey[:7] + "****"
	}

	key := map[string]any{
		"id":      "default",
		"api_key": maskedKey,
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"keys":  []map[string]any{key},
		"count": 1,
	})
}

func (s *Server) handleKeysCreate(w http.ResponseWriter, r *http.Request) {
	// Multi-key support requires config structure changes and persistence
	// Deferred for MVP - single API key in config
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"error":   "multi-key support not implemented",
		"message": "For MVP, use single API key in config YAML",
	})
}

func (s *Server) handleKeysUpdate(w http.ResponseWriter, r *http.Request) {
	// Multi-key support requires config structure changes and persistence
	// Deferred for MVP - single API key in config
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"error":   "multi-key support not implemented",
		"message": "For MVP, use single API key in config YAML",
	})
}

func (s *Server) handleKeysDelete(w http.ResponseWriter, r *http.Request) {
	// Multi-key support requires config structure changes and persistence
	// Deferred for MVP - single API key in config
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"error":   "multi-key support not implemented",
		"message": "For MVP, use single API key in config YAML",
	})
}

func (s *Server) handleModelAliasesList(w http.ResponseWriter, r *http.Request) {
	// Return list of model aliases from config
	aliases := make([]map[string]any, 0, len(s.config.ModelAliases))
	for alias, modelAlias := range s.config.ModelAliases {
		a := map[string]any{
			"alias":    alias,
			"provider": modelAlias.Provider,
			"model":    modelAlias.Model,
		}
		aliases = append(aliases, a)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"aliases": aliases,
		"count":   len(aliases),
	})
}

func (s *Server) handleModelAliasesCreate(w http.ResponseWriter, r *http.Request) {
	// Dynamic alias creation requires runtime config management and persistence
	// Deferred for MVP - use YAML config instead
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"error":   "dynamic alias creation not implemented",
		"message": "For MVP, add aliases to config YAML and restart the server",
	})
}

func (s *Server) handleModelAliasesUpdate(w http.ResponseWriter, r *http.Request) {
	// Dynamic alias updates require runtime config management and persistence
	// Deferred for MVP - use YAML config instead
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"error":   "dynamic alias updates not implemented",
		"message": "For MVP, update aliases in config YAML and restart the server",
	})
}

func (s *Server) handleModelAliasesDelete(w http.ResponseWriter, r *http.Request) {
	// Dynamic alias deletion requires runtime config management and persistence
	// Deferred for MVP - use YAML config instead
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"error":   "dynamic alias deletion not implemented",
		"message": "For MVP, remove aliases from config YAML and restart the server",
	})
}

func (s *Server) handleModelsCustomList(w http.ResponseWriter, r *http.Request) {
	// Custom models not implemented in config yet
	// Return empty list for MVP
	writeJSON(w, http.StatusOK, map[string]any{
		"models": []map[string]any{},
		"count":  0,
	})
}

func (s *Server) handleModelsCustomCreate(w http.ResponseWriter, r *http.Request) {
	// Custom models require config structure changes and persistence
	// Deferred for MVP
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"error":   "custom models not implemented",
		"message": "Custom models feature not yet supported",
	})
}

func (s *Server) handleModelsCustomUpdate(w http.ResponseWriter, r *http.Request) {
	// Custom models require config structure changes and persistence
	// Deferred for MVP
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"error":   "custom models not implemented",
		"message": "Custom models feature not yet supported",
	})
}

func (s *Server) handleModelsCustomDelete(w http.ResponseWriter, r *http.Request) {
	// Custom models require config structure changes and persistence
	// Deferred for MVP
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"error":   "custom models not implemented",
		"message": "Custom models feature not yet supported",
	})
}

func (s *Server) handleSettingsGet(w http.ResponseWriter, r *http.Request) {
	// Return current settings from config
	settings := map[string]any{
		"combo_strategy":         s.config.Settings.ComboStrategy,
		"outbound_proxy_enabled": s.config.Settings.OutboundProxyEnabled,
		"outbound_proxy_url":     s.config.Settings.OutboundProxyURL,
	}

	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) handleSettingsPut(w http.ResponseWriter, r *http.Request) {
	// Dynamic settings updates require runtime config management and persistence
	// Deferred for MVP - use YAML config instead
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"error":   "dynamic settings updates not implemented",
		"message": "For MVP, update settings in config YAML and restart the server",
	})
}

func (s *Server) handleLogsList(w http.ResponseWriter, r *http.Request) {
	// Request logs require database queries with pagination
	// Deferred for MVP - use SQLite direct access if needed
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"error":   "logs API not implemented",
		"message": "For MVP, query SQLite directly: SELECT * FROM request_logs",
	})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	s.metrics.mu.RLock()
	defer s.metrics.mu.RUnlock()

	// Output Prometheus format metrics
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")

	// Request metrics
	fmt.Fprintf(w, "# HELP router_requests_total Total number of requests\n")
	fmt.Fprintf(w, "# TYPE router_requests_total counter\n")
	fmt.Fprintf(w, "router_requests_total %d\n", s.metrics.RequestsTotal)

	fmt.Fprintf(w, "# HELP router_requests_success Total number of successful requests\n")
	fmt.Fprintf(w, "# TYPE router_requests_success counter\n")
	fmt.Fprintf(w, "router_requests_success %d\n", s.metrics.RequestsSuccess)

	fmt.Fprintf(w, "# HELP router_requests_error Total number of failed requests\n")
	fmt.Fprintf(w, "# TYPE router_requests_error counter\n")
	fmt.Fprintf(w, "router_requests_error %d\n", s.metrics.RequestsError)

	// Provider usage metrics
	fmt.Fprintf(w, "# HELP router_provider_usage Number of requests per provider\n")
	fmt.Fprintf(w, "# TYPE router_provider_usage counter\n")
	for provider, count := range s.metrics.ProviderUsage {
		fmt.Fprintf(w, "router_provider_usage{provider=\"%s\"} %d\n", provider, count)
	}
}

func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	// Return summary from in-memory metrics
	// A more complete implementation would query SQLite for historical data
	s.metrics.mu.RLock()
	defer s.metrics.mu.RUnlock()

	usage := map[string]any{
		"requests_total":   s.metrics.RequestsTotal,
		"requests_success": s.metrics.RequestsSuccess,
		"requests_error":   s.metrics.RequestsError,
		"provider_usage":   s.metrics.ProviderUsage,
	}

	writeJSON(w, http.StatusOK, usage)
}

func (s *Server) handleModels(w http.ResponseWriter, _ *http.Request) {
	models := make([]map[string]any, 0)

	// Add route aliases
	for alias := range s.config.Routes {
		models = append(models, map[string]any{
			"id":     alias,
			"object": "model",
			"type":   "route",
		})
	}

	// Add model aliases
	for alias := range s.config.ModelAliases {
		models = append(models, map[string]any{
			"id":     alias,
			"object": "model",
			"type":   "alias",
		})
	}

	// Add provider/model combinations
	for _, provider := range s.config.Providers {
		if provider.Enabled {
			models = append(models, map[string]any{
				"id":     provider.Name + "/*",
				"object": "model",
				"type":   "provider",
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   models,
	})
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	requestID := GetRequestID(r.Context())
	defer r.Body.Close()

	// Increment metrics
	s.metrics.mu.Lock()
	s.metrics.RequestsTotal++
	s.metrics.mu.Unlock()

	startTime := time.Now()

	// Capture raw request body for debug logging
	var rawRequestBytes []byte
	if s.config.Logging.Debug {
		rawRequestBytes, _ = io.ReadAll(io.LimitReader(r.Body, 1<<20))
		r.Body = io.NopCloser(bytes.NewReader(rawRequestBytes))
	}

	var request providers.ChatRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&request); err != nil {
		s.metrics.mu.Lock()
		s.metrics.RequestsError++
		s.metrics.mu.Unlock()
		writeOpenAIError(w, http.StatusBadRequest, "invalid json", "invalid_request_error", "")
		return
	}

	// Handle streaming request
	if request.Stream {
		s.handleStreamingChatCompletion(w, r, request, requestID, startTime, rawRequestBytes)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(s.config.Server.RequestTimeoutSeconds)*time.Second)
	defer cancel()

	response, providerName, err := s.engine.ChatCompletion(ctx, request)
	duration := time.Since(startTime)

	if err != nil {
		// Increment error metrics
		s.metrics.mu.Lock()
		s.metrics.RequestsError++
		s.metrics.mu.Unlock()

		s.logger.Error().
			Err(err).
			Str("request_id", requestID).
			Str("model", request.Model).
			Msg("chat completion failed")

		if s.asyncWriter != nil {
			s.asyncWriter.LogRequest(&storage.RequestLog{
				RequestID:    requestID,
				Model:        request.Model,
				Provider:     providerName,
				TargetModel:  request.Model,
				Status:       "error",
				ErrorMessage: err.Error(),
				StartTime:    startTime,
				EndTime:      time.Now(),
				Duration:     duration,
			})

			// Log request details in debug mode
			if s.config.Logging.Debug {
				requestBodyStr := string(rawRequestBytes)
				s.asyncWriter.LogRequestDetails(r.Context(), requestID, requestBodyStr, "", http.StatusBadGateway)
			}
		}

		writeOpenAIError(w, http.StatusBadGateway, err.Error(), "api_error", "")
		return
	}

	// Capture response for debug logging
	var rawResponseBytes []byte
	if s.config.Logging.Debug {
		rawResponseBytes, _ = json.Marshal(response)
	}

	// Increment success metrics and provider usage
	s.metrics.mu.Lock()
	s.metrics.RequestsSuccess++
	s.metrics.ProviderUsage[providerName]++
	s.metrics.mu.Unlock()

	if s.asyncWriter != nil {
		s.asyncWriter.LogRequest(&storage.RequestLog{
			RequestID:   requestID,
			Model:       request.Model,
			Provider:    providerName,
			TargetModel: request.Model,
			Status:      "success",
			StartTime:   startTime,
			EndTime:     time.Now(),
			Duration:    duration,
		})

		if response.Usage != nil {
			s.asyncWriter.IncrementUsage(providerName, request.Model, response.Usage.PromptTokens, response.Usage.CompletionTokens)
		}

		// Log request details in debug mode
		if s.config.Logging.Debug {
			requestBodyStr := string(rawRequestBytes)
			responseBodyStr := string(rawResponseBytes)
			s.asyncWriter.LogRequestDetails(r.Context(), requestID, requestBodyStr, responseBodyStr, http.StatusOK)
		}
	}

	w.Header().Set("X-Router-Provider", providerName)
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleStreamingChatCompletion(w http.ResponseWriter, r *http.Request, request providers.ChatRequest, requestID string, startTime time.Time, rawRequestBytes []byte) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	// Create context with timeout
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(s.config.Server.RequestTimeoutSeconds)*time.Second)
	defer cancel()

	// Get provider adapter from routing
	targets := s.engine.ResolveTargets(request.Model)
	if len(targets) == 0 {
		s.metrics.mu.Lock()
		s.metrics.RequestsError++
		s.metrics.mu.Unlock()

		s.logger.Error().
			Str("request_id", requestID).
			Str("model", request.Model).
			Msg("no route targets for model")

		writeSSEError(w, "no route targets for model: "+request.Model)
		return
	}

	// Get adapter from registry
	adapter, err := s.engine.GetRegistry().Get(targets[0].Provider)
	if err != nil {
		s.metrics.mu.Lock()
		s.metrics.RequestsError++
		s.metrics.mu.Unlock()

		s.logger.Error().
			Err(err).
			Str("request_id", requestID).
			Str("model", request.Model).
			Msg("provider not found")

		writeSSEError(w, "provider not found: "+targets[0].Provider)
		return
	}

	// Call streaming completion
	chunks, err := adapter.StreamChatCompletion(ctx, request, targets[0].Model)
	if err != nil {
		s.metrics.mu.Lock()
		s.metrics.RequestsError++
		s.metrics.mu.Unlock()

		s.logger.Error().
			Err(err).
			Str("request_id", requestID).
			Str("model", request.Model).
			Msg("streaming failed")

		writeSSEError(w, "streaming failed: "+err.Error())
		return
	}

	// Forward chunks to client
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.logger.Error().
			Str("request_id", requestID).
			Msg("streaming not supported (http.Flusher not available)")

		writeSSEError(w, "streaming not supported")
		return
	}

	chunkCount := 0
	for chunk := range chunks {
		select {
		case <-ctx.Done():
			// Client disconnected or timeout
			s.logger.Info().
				Str("request_id", requestID).
				Msg("client disconnected or timeout during streaming")
			return
		default:
			data, err := json.Marshal(chunk)
			if err != nil {
				s.logger.Error().
					Err(err).
					Str("request_id", requestID).
					Msg("failed to marshal chunk")
				continue
			}

			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			chunkCount++
		}
	}

	// Write final [DONE] marker
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()

	// Update metrics
	s.metrics.mu.Lock()
	s.metrics.RequestsSuccess++
	s.metrics.mu.Unlock()

	s.logger.Info().
		Str("request_id", requestID).
		Str("model", request.Model).
		Int("chunks", chunkCount).
		Dur("duration_ms", time.Since(startTime)).
		Msg("streaming completed")
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	requestID := GetRequestID(r.Context())
	defer r.Body.Close()

	// Increment metrics
	s.metrics.mu.Lock()
	s.metrics.RequestsTotal++
	s.metrics.mu.Unlock()

	startTime := time.Now()

	// Parse Claude Messages API request
	var claudeReq map[string]interface{}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&claudeReq); err != nil {
		s.metrics.mu.Lock()
		s.metrics.RequestsError++
		s.metrics.mu.Unlock()
		writeOpenAIError(w, http.StatusBadRequest, "invalid json", "invalid_request_error", "")
		return
	}

	// Use translator layer to convert Claude to OpenAI
	reqTranslator, err := s.translators.GetRequestTranslator(translator.FormatClaude, translator.FormatOpenAI)
	if err != nil {
		s.metrics.mu.Lock()
		s.metrics.RequestsError++
		s.metrics.mu.Unlock()
		writeOpenAIError(w, http.StatusInternalServerError, "translator not available", "internal_error", "")
		return
	}

	openAIReq, err := reqTranslator.TranslateRequest(r.Context(), translator.FormatClaude, translator.FormatOpenAI, claudeReq)
	if err != nil {
		s.metrics.mu.Lock()
		s.metrics.RequestsError++
		s.metrics.mu.Unlock()
		writeOpenAIError(w, http.StatusInternalServerError, "translation failed", "internal_error", "")
		return
	}

	// Create ChatRequest directly from map without marshal/unmarshal
	chatReq := s.mapToChatRequest(openAIReq)

	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(s.config.Server.RequestTimeoutSeconds)*time.Second)
	defer cancel()

	response, providerName, err := s.engine.ChatCompletion(ctx, chatReq)
	duration := time.Since(startTime)

	if err != nil {
		// Increment error metrics
		s.metrics.mu.Lock()
		s.metrics.RequestsError++
		s.metrics.mu.Unlock()

		s.logger.Error().
			Err(err).
			Str("request_id", requestID).
			Str("model", chatReq.Model).
			Msg("messages request failed")

		if s.asyncWriter != nil {
			s.asyncWriter.LogRequest(&storage.RequestLog{
				RequestID:    requestID,
				Model:        chatReq.Model,
				Provider:     providerName,
				TargetModel:  chatReq.Model,
				Status:       "error",
				ErrorMessage: err.Error(),
				StartTime:    startTime,
				EndTime:      time.Now(),
				Duration:     duration,
			})
		}

		writeOpenAIError(w, http.StatusBadGateway, err.Error(), "api_error", "")
		return
	}

	// Use translator layer to convert OpenAI response to Claude format
	respBytes, err := json.Marshal(response)
	if err != nil {
		s.metrics.mu.Lock()
		s.metrics.RequestsError++
		s.metrics.mu.Unlock()
		writeOpenAIError(w, http.StatusInternalServerError, "failed to marshal response", "internal_error", "")
		return
	}

	respTranslator, err := s.translators.GetResponseTranslator(translator.FormatOpenAI, translator.FormatClaude)
	if err != nil {
		s.metrics.mu.Lock()
		s.metrics.RequestsError++
		s.metrics.mu.Unlock()
		writeOpenAIError(w, http.StatusInternalServerError, "translator not available", "internal_error", "")
		return
	}

	claudeRespBytes, err := respTranslator.TranslateResponse(r.Context(), translator.FormatOpenAI, translator.FormatClaude, respBytes)
	if err != nil {
		s.metrics.mu.Lock()
		s.metrics.RequestsError++
		s.metrics.mu.Unlock()
		writeOpenAIError(w, http.StatusInternalServerError, "translation failed", "internal_error", "")
		return
	}

	var claudeResp map[string]interface{}
	if err := json.Unmarshal(claudeRespBytes, &claudeResp); err != nil {
		s.metrics.mu.Lock()
		s.metrics.RequestsError++
		s.metrics.mu.Unlock()
		writeOpenAIError(w, http.StatusInternalServerError, "failed to parse translated response", "internal_error", "")
		return
	}

	// Increment success metrics and provider usage
	s.metrics.mu.Lock()
	s.metrics.RequestsSuccess++
	s.metrics.ProviderUsage[providerName]++
	s.metrics.mu.Unlock()

	if s.asyncWriter != nil {
		s.asyncWriter.LogRequest(&storage.RequestLog{
			RequestID:   requestID,
			Model:       chatReq.Model,
			Provider:    providerName,
			TargetModel: chatReq.Model,
			Status:      "success",
			StartTime:   startTime,
			EndTime:     time.Now(),
			Duration:    duration,
		})

		if response.Usage != nil {
			s.asyncWriter.IncrementUsage(providerName, chatReq.Model, response.Usage.PromptTokens, response.Usage.CompletionTokens)
		}
	}

	w.Header().Set("X-Router-Provider", providerName)
	writeJSON(w, http.StatusOK, claudeResp)
}

func (s *Server) handleResponses(w http.ResponseWriter, r *http.Request) {
	requestID := GetRequestID(r.Context())
	defer r.Body.Close()

	// Increment metrics
	s.metrics.mu.Lock()
	s.metrics.RequestsTotal++
	s.metrics.mu.Unlock()

	startTime := time.Now()

	// Parse OpenAI Responses API request
	var responsesReq struct {
		Input       interface{} `json:"input"`
		Model       string      `json:"model"`
		Temperature *float64    `json:"temperature,omitempty"`
		TopP        *float64    `json:"top_p,omitempty"`
		MaxTokens   *int        `json:"max_tokens,omitempty"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&responsesReq); err != nil {
		s.metrics.mu.Lock()
		s.metrics.RequestsError++
		s.metrics.mu.Unlock()
		writeOpenAIError(w, http.StatusBadRequest, "invalid json", "invalid_request_error", "")
		return
	}

	// Convert input to ChatMessage array
	var messages []providers.ChatMessage
	switch v := responsesReq.Input.(type) {
	case string:
		// Single string input
		messages = []providers.ChatMessage{{Role: "user", Content: v}}
	case []interface{}:
		// Array of message objects or strings
		messages = make([]providers.ChatMessage, 0, len(v))
		for _, item := range v {
			if msgObj, ok := item.(map[string]interface{}); ok {
				// Message object with role/content
				role, _ := msgObj["role"].(string)
				content, _ := msgObj["content"].(string)
				if role == "" {
					role = "user"
				}
				messages = append(messages, providers.ChatMessage{
					Role:    role,
					Content: content,
				})
			} else if str, ok := item.(string); ok {
				// String in array
				messages = append(messages, providers.ChatMessage{
					Role:    "user",
					Content: str,
				})
			}
		}
	default:
		// Unknown type, treat as string
		messages = []providers.ChatMessage{{Role: "user", Content: fmt.Sprintf("%v", v)}}
	}

	// Create ChatRequest from Responses API request
	chatReq := providers.ChatRequest{
		Model:       responsesReq.Model,
		Messages:    messages,
		Temperature: responsesReq.Temperature,
		TopP:        responsesReq.TopP,
		MaxTokens:   responsesReq.MaxTokens,
		Stream:      false,
	}

	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(s.config.Server.RequestTimeoutSeconds)*time.Second)
	defer cancel()

	response, providerName, err := s.engine.ChatCompletion(ctx, chatReq)
	duration := time.Since(startTime)

	if err != nil {
		s.metrics.mu.Lock()
		s.metrics.RequestsError++
		s.metrics.mu.Unlock()

		s.logger.Error().
			Err(err).
			Str("request_id", requestID).
			Str("model", chatReq.Model).
			Msg("responses request failed")

		if s.asyncWriter != nil {
			s.asyncWriter.LogRequest(&storage.RequestLog{
				RequestID:    requestID,
				Model:        chatReq.Model,
				Provider:     providerName,
				TargetModel:  chatReq.Model,
				Status:       "error",
				ErrorMessage: err.Error(),
				StartTime:    startTime,
				EndTime:      time.Now(),
				Duration:     duration,
			})
		}

		writeOpenAIError(w, http.StatusBadGateway, err.Error(), "api_error", "")
		return
	}

	// Increment success metrics
	s.metrics.mu.Lock()
	s.metrics.RequestsSuccess++
	s.metrics.ProviderUsage[providerName]++
	s.metrics.mu.Unlock()

	// Convert ChatResponse to Responses API format
	output := []map[string]any{}
	if len(response.Choices) > 0 {
		output = append(output, map[string]any{
			"role":    response.Choices[0].Message.Role,
			"content": response.Choices[0].Message.Content,
		})
	}

	responsesResp := map[string]any{
		"id":      response.ID,
		"object":  "response",
		"created": response.Created,
		"model":   response.Model,
		"output":  output,
	}

	if response.Usage != nil {
		responsesResp["usage"] = response.Usage
	}

	if s.asyncWriter != nil {
		s.asyncWriter.LogRequest(&storage.RequestLog{
			RequestID:   requestID,
			Model:       chatReq.Model,
			Provider:    providerName,
			TargetModel: chatReq.Model,
			Status:      "success",
			StartTime:   startTime,
			EndTime:     time.Now(),
			Duration:    duration,
		})

		if response.Usage != nil {
			s.asyncWriter.IncrementUsage(providerName, chatReq.Model, response.Usage.PromptTokens, response.Usage.CompletionTokens)
		}
	}

	w.Header().Set("X-Router-Provider", providerName)
	writeJSON(w, http.StatusOK, responsesResp)
}

func (s *Server) mapToChatRequest(req map[string]interface{}) providers.ChatRequest {
	chatReq := providers.ChatRequest{
		Stream: false, // Default to non-streaming for MVP
	}

	if model, ok := req["model"].(string); ok {
		chatReq.Model = model
	}

	if messages, ok := req["messages"].([]interface{}); ok {
		chatReq.Messages = make([]providers.ChatMessage, 0, len(messages))
		for _, msg := range messages {
			msgMap, ok := msg.(map[string]interface{})
			if !ok {
				continue
			}
			role, _ := msgMap["role"].(string)
			content, _ := msgMap["content"].(string)
			chatReq.Messages = append(chatReq.Messages, providers.ChatMessage{
				Role:    role,
				Content: content,
			})
		}
	}

	if maxTokens, ok := req["max_tokens"].(float64); ok {
		val := int(maxTokens)
		chatReq.MaxTokens = &val
	}

	if temp, ok := req["temperature"].(float64); ok {
		chatReq.Temperature = &temp
	}

	if topP, ok := req["top_p"].(float64); ok {
		chatReq.TopP = &topP
	}

	if stop, ok := req["stop"]; ok {
		switch v := stop.(type) {
		case string:
			if v != "" {
				chatReq.Stop = []string{v}
			}
		case []interface{}:
			chatReq.Stop = make([]string, 0, len(v))
			for _, s := range v {
				if str, ok := s.(string); ok {
					chatReq.Stop = append(chatReq.Stop, str)
				}
			}
		}
	}

	if stream, ok := req["stream"].(bool); ok {
		chatReq.Stream = stream
	}

	return chatReq
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	addr := fmt.Sprintf("%s:%d", s.config.Server.Host, s.config.Server.Port)
	server := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadTimeout:       time.Duration(s.config.Server.ReadTimeoutSeconds) * time.Second,
		WriteTimeout:      time.Duration(s.config.Server.WriteTimeoutSeconds) * time.Second,
		IdleTimeout:       time.Duration(s.config.Server.IdleTimeoutSeconds) * time.Second,
		ReadHeaderTimeout: time.Duration(s.config.Server.ReadHeaderTimeoutSeconds) * time.Second,
		MaxHeaderBytes:    s.config.Server.MaxHeaderBytes,
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

func writeOpenAIError(w http.ResponseWriter, status int, message string, errorType string, code string) {
	errorResp := map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    errorType,
		},
	}
	if code != "" {
		errorResp["error"].(map[string]any)["code"] = code
	}
	writeJSON(w, status, errorResp)
}

func writeSSEError(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	fmt.Fprintf(w, "data: {\"error\":{\"message\":\"%s\"}}\n\n", message)
	fmt.Fprintf(w, "data: [DONE]\n\n")
}
