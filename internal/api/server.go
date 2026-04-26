package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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

	// Convert input to string for the message
	var inputStr string
	switch v := responsesReq.Input.(type) {
	case string:
		inputStr = v
	case []interface{}:
		// Join array of strings with newlines
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if str, ok := item.(string); ok {
				parts = append(parts, str)
			}
		}
		inputStr = strings.Join(parts, "\n")
	}

	// Create ChatRequest from Responses API request
	chatReq := providers.ChatRequest{
		Model:       responsesReq.Model,
		Messages:    []providers.ChatMessage{{Role: "user", Content: inputStr}},
		Temperature: responsesReq.Temperature,
		TopP:        responsesReq.TopP,
		MaxTokens:   responsesReq.MaxTokens,
		Stream:      false,
	}

	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(s.config.Server.RequestTimeoutSeconds)*time.Second)
	defer cancel()

	response, providerName, err := s.engine.ChatCompletion(ctx, chatReq)

	if err != nil {
		s.metrics.mu.Lock()
		s.metrics.RequestsError++
		s.metrics.mu.Unlock()

		s.logger.Error().
			Err(err).
			Str("request_id", requestID).
			Str("model", chatReq.Model).
			Msg("responses request failed")

		writeOpenAIError(w, http.StatusBadGateway, err.Error(), "api_error", "")
		return
	}

	// Increment success metrics
	s.metrics.mu.Lock()
	s.metrics.RequestsSuccess++
	s.metrics.ProviderUsage[providerName]++
	s.metrics.mu.Unlock()

	// Convert ChatResponse to Responses API format
	responsesResp := map[string]any{
		"id":      response.ID,
		"object":  "response",
		"created": response.Created,
		"model":   response.Model,
		"choices": []map[string]any{},
	}

	if len(response.Choices) > 0 {
		responsesResp["text"] = response.Choices[0].Message.Content
	}

	if response.Usage != nil {
		responsesResp["usage"] = response.Usage
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
