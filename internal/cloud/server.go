package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Server struct {
	mu      sync.RWMutex
	syncs   map[string]map[string]any
	storage Storage
	client  *http.Client
}

func NewServer() *Server {
	return NewServerWithStorage(NewMemoryStorage())
}

func NewServerWithStorage(storage Storage) *Server {
	if storage == nil {
		storage = NewMemoryStorage()
	}
	syncs, err := storage.Load(context.Background())
	if err != nil {
		syncs = map[string]map[string]any{}
	}
	return &Server{
		syncs:   syncs,
		storage: storage,
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}

func NewServerWithSQLite(path string) (*Server, error) {
	storage, err := NewSQLiteStorage(path)
	if err != nil {
		return nil, err
	}
	return NewServerWithStorage(storage), nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handle)
	return cors(mux)
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	path := normalizePath(r.URL.Path)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	switch {
	case path == "/" && r.Method == http.MethodGet:
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<!doctype html><title>9router cloud</title><h1>9router cloud</h1>")
	case path == "/health" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	case path == "/api/tags" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"models": []map[string]any{
			{"name": "llama3.2", "model": "llama3.2", "modified_at": time.Unix(0, 0).UTC().Format(time.RFC3339)},
			{"name": "qwen2.5-coder", "model": "qwen2.5-coder", "modified_at": time.Unix(0, 0).UTC().Format(time.RFC3339)},
			{"name": "codellama", "model": "codellama", "modified_at": time.Unix(0, 0).UTC().Format(time.RFC3339)},
		}})
	case path == "/cache/clear" && r.Method == http.MethodPost:
		s.handleCacheClear(w, r)
	case strings.HasPrefix(path, "/sync/") && (r.Method == http.MethodGet || r.Method == http.MethodPost || r.Method == http.MethodDelete):
		s.handleSync(w, r, strings.TrimPrefix(path, "/sync/"))
	case path == "/forward" && r.Method == http.MethodPost:
		s.handleForward(w, r)
	case path == "/forward-raw" && r.Method == http.MethodPost:
		s.handleForward(w, r)
	case path == "/testClaude" && r.Method == http.MethodPost:
		s.handleTestClaude(w, r)
	case path == "/v1/verify" && r.Method == http.MethodGet:
		s.handleVerify(w, r, "")
	case isMachineVerifyPath(path) && r.Method == http.MethodGet:
		s.handleVerify(w, r, strings.Split(strings.Trim(path, "/"), "/")[0])
	case isCloudInferencePath(path) && r.Method == http.MethodPost:
		switch cloudInferenceSuffix(path) {
		case "/v1/messages/count_tokens":
			s.handleCountTokens(w, r)
			return
		case "/v1/api/chat":
			s.handleCloudOllamaChat(w, r, path)
			return
		}
		s.handleCloudInference(w, r, path)
	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Not Found"})
	}
}

func (s *Server) handleSync(w http.ResponseWriter, r *http.Request, machineID string) {
	if machineID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "machineId required"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.mu.RLock()
		data, ok := s.syncs[machineID]
		s.mu.RUnlock()
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "No data found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": data})
	case http.MethodPost:
		defer r.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid JSON body"})
			return
		}
		providers, ok := body["providers"].([]any)
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Missing providers array"})
			return
		}

		s.mu.Lock()
		existing := s.syncs[machineID]
		finalData, changes := mergeCloudSyncData(body, existing, providers)
		s.syncs[machineID] = finalData
		s.mu.Unlock()
		if err := s.storage.Save(r.Context(), machineID, finalData); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": finalData, "changes": changes})
	case http.MethodDelete:
		s.mu.Lock()
		delete(s.syncs, machineID)
		s.mu.Unlock()
		if err := s.storage.Delete(r.Context(), machineID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Data deleted successfully"})
	}
}

func mergeCloudSyncData(body, existing map[string]any, providers []any) (map[string]any, map[string][]string) {
	existingProviders := map[string]any{}
	if raw, ok := existing["providers"].(map[string]any); ok {
		existingProviders = raw
	}
	mergedProviders := map[string]any{}
	changes := map[string][]string{"updated": {}, "fromWorker": {}}

	for _, rawProvider := range providers {
		provider, ok := rawProvider.(map[string]any)
		if !ok {
			continue
		}
		id, _ := provider["id"].(string)
		if strings.TrimSpace(id) == "" {
			continue
		}
		if workerProvider, ok := existingProviders[id].(map[string]any); ok && isCloudObjectNewer(workerProvider, provider) {
			merged := copyMap(workerProvider)
			merged["updatedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
			mergedProviders[id] = merged
			changes["fromWorker"] = append(changes["fromWorker"], id)
			continue
		}
		merged := formatCloudProvider(provider)
		mergedProviders[id] = merged
		changes["updated"] = append(changes["updated"], id)
	}

	finalData := map[string]any{
		"providers":    mergedProviders,
		"modelAliases": valueOrExisting(body, existing, "modelAliases", map[string]any{}),
		"combos":       valueOrExisting(body, existing, "combos", []any{}),
		"apiKeys":      valueOrExisting(body, existing, "apiKeys", []any{}),
		"updatedAt":    time.Now().UTC().Format(time.RFC3339Nano),
	}
	return finalData, changes
}

func formatCloudProvider(provider map[string]any) map[string]any {
	out := copyMap(provider)
	if _, ok := out["providerSpecificData"]; !ok {
		out["providerSpecificData"] = map[string]any{}
	}
	if _, ok := out["status"]; !ok {
		out["status"] = "active"
	}
	if _, ok := out["lastError"]; !ok {
		out["lastError"] = nil
	}
	if _, ok := out["lastErrorAt"]; !ok {
		out["lastErrorAt"] = nil
	}
	if _, ok := out["errorCode"]; !ok {
		out["errorCode"] = nil
	}
	if _, ok := out["rateLimitedUntil"]; !ok {
		out["rateLimitedUntil"] = nil
	}
	if _, ok := out["backoffLevel"]; !ok {
		out["backoffLevel"] = 0
	}
	if _, ok := out["updatedAt"]; !ok {
		out["updatedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
	}
	return out
}

func isCloudObjectNewer(a, b map[string]any) bool {
	at, _ := time.Parse(time.RFC3339, stringField(a, "updatedAt"))
	if at.IsZero() {
		at, _ = time.Parse(time.RFC3339Nano, stringField(a, "updatedAt"))
	}
	bt, _ := time.Parse(time.RFC3339, stringField(b, "updatedAt"))
	if bt.IsZero() {
		bt, _ = time.Parse(time.RFC3339Nano, stringField(b, "updatedAt"))
	}
	return at.After(bt)
}

func valueOrExisting(body, existing map[string]any, key string, fallback any) any {
	if v, ok := body[key]; ok {
		return v
	}
	if v, ok := existing[key]; ok {
		return v
	}
	return fallback
}

func copyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func stringField(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func (s *Server) handleCacheClear(w http.ResponseWriter, r *http.Request) {
	machineID := ""
	defer r.Body.Close()
	var body struct {
		MachineID string `json:"machineId"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	machineID = strings.TrimSpace(body.MachineID)
	if machineID == "" {
		machineID = machineIDFromBearer(r.Header.Get("Authorization"))
	}
	if machineID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Missing machineId"})
		return
	}
	s.mu.Lock()
	if data, ok := s.syncs[machineID]; ok {
		if providers, ok := data["providers"].(map[string]any); ok {
			for _, raw := range providers {
				provider, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				if strings.EqualFold(stringField(provider, "status"), "cooldown") || strings.EqualFold(stringField(provider, "status"), "unavailable") {
					provider["status"] = "active"
				}
				provider["lastError"] = nil
				provider["lastErrorAt"] = nil
				provider["errorCode"] = nil
				provider["cooldownUntil"] = nil
				provider["rateLimitedUntil"] = nil
				provider["backoffLevel"] = 0
				provider["updatedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
			}
			data["updatedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
			_ = s.storage.Save(r.Context(), machineID, data)
		}
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "machineId": machineID, "message": "No cache layer"})
}

func (s *Server) handleVerify(w http.ResponseWriter, r *http.Request, machineID string) {
	apiKey := bearerToken(r.Header.Get("Authorization"))
	if apiKey == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "Missing or invalid Authorization header"})
		return
	}
	if machineID == "" {
		machineID = machineIDFromAPIKey(apiKey)
	}
	if machineID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "API key does not contain machineId"})
		return
	}
	s.mu.RLock()
	data, ok := s.syncs[machineID]
	s.mu.RUnlock()
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Machine not found"})
		return
	}
	if !cloudAPIKeyValid(data, apiKey) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "Invalid API key"})
		return
	}
	providersCount := 0
	if providers, ok := data["providers"].(map[string]any); ok {
		providersCount = len(providers)
	}
	writeJSON(w, http.StatusOK, map[string]any{"valid": true, "machineId": machineID, "providersCount": providersCount})
}

func bearerToken(authHeader string) string {
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
}

func machineIDFromBearer(authHeader string) string {
	return machineIDFromAPIKey(bearerToken(authHeader))
}

func machineIDFromAPIKey(apiKey string) string {
	parts := strings.Split(apiKey, "-")
	if len(parts) == 4 && parts[0] == "sk" {
		return parts[1]
	}
	return ""
}

func cloudAPIKeyValid(data map[string]any, apiKey string) bool {
	keys, ok := data["apiKeys"].([]any)
	if !ok {
		return false
	}
	for _, raw := range keys {
		switch key := raw.(type) {
		case string:
			if key == apiKey {
				return true
			}
		case map[string]any:
			if v, _ := key["key"].(string); v == apiKey {
				return true
			}
			if v, _ := key["api_key"].(string); v == apiKey {
				return true
			}
		}
	}
	return false
}

func (s *Server) handleForward(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var req struct {
		URL     string            `json:"url"`
		Method  string            `json:"method"`
		Headers map[string]string `json:"headers"`
		Body    json.RawMessage   `json:"body"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	if req.URL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "url required"})
		return
	}
	method := req.Method
	if method == "" {
		method = http.MethodPost
	}

	upstreamReq, err := http.NewRequestWithContext(r.Context(), method, req.URL, bytes.NewReader(req.Body))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	for k, v := range req.Headers {
		upstreamReq.Header.Set(k, v)
	}
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(upstreamReq)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	defer resp.Body.Close()
	for k, values := range resp.Header {
		for _, v := range values {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (s *Server) handleTestClaude(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid JSON body"})
		return
	}
	apiKey := strings.TrimSpace(stringField(body, "apiKey"))
	if apiKey == "" {
		apiKey = strings.TrimSpace(stringField(body, "key"))
	}
	if apiKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "apiKey required"})
		return
	}
	baseURL := strings.TrimSpace(stringField(body, "baseUrl"))
	if baseURL == "" {
		baseURL = strings.TrimSpace(stringField(body, "baseURL"))
	}
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	targetURL, err := cloudProviderURL(map[string]any{"provider": "anthropic", "baseUrl": baseURL, "apiKey": apiKey}, "/v1/messages")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	reqBody := map[string]any{
		"model":      valueOr(body, "model", "claude-3-haiku-20240307"),
		"max_tokens": valueOr(body, "max_tokens", float64(16)),
		"messages":   valueOr(body, "messages", []any{map[string]any{"role": "user", "content": "ping"}}),
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid request body"})
		return
	}
	upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, targetURL, bytes.NewReader(payload))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	setCloudProviderHeaders(upstreamReq, map[string]any{"provider": "anthropic", "apiKey": apiKey})
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(upstreamReq)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	defer resp.Body.Close()
	for k, values := range resp.Header {
		for _, v := range values {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (s *Server) handleCloudInference(w http.ResponseWriter, r *http.Request, path string) {
	apiKey := bearerToken(r.Header.Get("Authorization"))
	if apiKey == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "Missing or invalid Authorization header"})
		return
	}
	machineID := cloudMachineIDFromPath(path)
	if machineID == "" {
		machineID = machineIDFromAPIKey(apiKey)
	}
	if machineID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "API key does not contain machineId"})
		return
	}

	s.mu.RLock()
	data, ok := s.syncs[machineID]
	s.mu.RUnlock()
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Machine not found"})
		return
	}
	if !cloudAPIKeyValid(data, apiKey) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "Invalid API key"})
		return
	}

	defer r.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid JSON body"})
		return
	}
	model, _ := body["model"].(string)
	providerName, targetModel, err := resolveCloudModel(data, model)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	providers := selectCloudProviders(data, providerName)
	if len(providers) == 0 {
		if limited, ok := cloudAllRateLimited(data, providerName); ok {
			writeCloudAllRateLimited(w, providerName, targetModel, limited)
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "No active provider credentials for model"})
		return
	}
	body["model"] = targetModel
	reqBody, err := json.Marshal(body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid request body"})
		return
	}
	resp, respBody, status, err := s.forwardCloudWithFallback(r.Context(), machineID, providers, cloudInferenceSuffix(path), reqBody)
	if err != nil {
		if status != 0 {
			writeJSON(w, status, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	defer resp.Body.Close()
	for k, values := range resp.Header {
		for _, v := range values {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(respBody)
}

func (s *Server) handleCountTokens(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid JSON body"})
		return
	}
	totalChars := 0
	if messages, ok := body["messages"].([]any); ok {
		for _, rawMessage := range messages {
			message, ok := rawMessage.(map[string]any)
			if !ok {
				continue
			}
			switch content := message["content"].(type) {
			case string:
				totalChars += len(content)
			case []any:
				for _, rawPart := range content {
					part, ok := rawPart.(map[string]any)
					if !ok || stringField(part, "type") != "text" {
						continue
					}
					totalChars += len(stringField(part, "text"))
				}
			}
		}
	}
	inputTokens := totalChars / 4
	if totalChars%4 != 0 {
		inputTokens++
	}
	writeJSON(w, http.StatusOK, map[string]any{"input_tokens": inputTokens})
}

func (s *Server) handleCloudOllamaChat(w http.ResponseWriter, r *http.Request, path string) {
	apiKey := bearerToken(r.Header.Get("Authorization"))
	if apiKey == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "Missing or invalid Authorization header"})
		return
	}
	machineID := cloudMachineIDFromPath(path)
	if machineID == "" {
		machineID = machineIDFromAPIKey(apiKey)
	}
	if machineID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "API key does not contain machineId"})
		return
	}

	s.mu.RLock()
	data, ok := s.syncs[machineID]
	s.mu.RUnlock()
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Machine not found"})
		return
	}
	if !cloudAPIKeyValid(data, apiKey) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "Invalid API key"})
		return
	}

	defer r.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid JSON body"})
		return
	}
	model, _ := body["model"].(string)
	providerName, targetModel, err := resolveCloudModel(data, model)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	providers := selectCloudProviders(data, providerName)
	if len(providers) == 0 {
		if limited, ok := cloudAllRateLimited(data, providerName); ok {
			writeCloudAllRateLimited(w, providerName, targetModel, limited)
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "No active provider credentials for model"})
		return
	}
	body["model"] = targetModel
	body["stream"] = false
	reqBody, err := json.Marshal(body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid request body"})
		return
	}
	resp, respBody, status, err := s.forwardCloudWithFallback(r.Context(), machineID, providers, "/v1/chat/completions", reqBody)
	if err != nil {
		if status != 0 {
			writeJSON(w, status, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		for k, values := range resp.Header {
			for _, v := range values {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(respBody)
		return
	}
	writeJSON(w, http.StatusOK, cloudOpenAIToOllama(body, respBody))
}

func cloudMachineIDFromPath(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 3 && parts[1] == "v1" {
		return parts[0]
	}
	return ""
}

func cloudInferenceSuffix(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 3 && parts[1] == "v1" {
		return "/" + strings.Join(parts[1:], "/")
	}
	return path
}

func resolveCloudModel(data map[string]any, model string) (string, string, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return "", "", errString("model required")
	}
	if provider, target, ok := splitCloudModel(model); ok {
		return provider, target, nil
	}
	if aliases, ok := data["modelAliases"].(map[string]any); ok {
		if raw, ok := aliases[model]; ok {
			switch alias := raw.(type) {
			case string:
				if provider, target, ok := splitCloudModel(alias); ok {
					return provider, target, nil
				}
			case map[string]any:
				provider, _ := alias["provider"].(string)
				target, _ := alias["model"].(string)
				if strings.TrimSpace(provider) != "" && strings.TrimSpace(target) != "" {
					return strings.TrimSpace(provider), strings.TrimSpace(target), nil
				}
			}
		}
	}
	return "", "", errString("Invalid model format. Use provider/model or a synced model alias")
}

func splitCloudModel(model string) (string, string, bool) {
	parts := strings.SplitN(model, "/", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", false
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), true
}

func selectCloudProvider(data map[string]any, providerName string) (map[string]any, bool) {
	matches := selectCloudProviders(data, providerName)
	if len(matches) == 0 {
		return nil, false
	}
	return matches[0], true
}

func selectCloudProviders(data map[string]any, providerName string) []map[string]any {
	rawProviders, ok := data["providers"].(map[string]any)
	if !ok {
		return nil
	}
	var matches []map[string]any
	now := time.Now()
	for _, raw := range rawProviders {
		provider, ok := raw.(map[string]any)
		if !ok || !strings.EqualFold(stringField(provider, "provider"), providerName) {
			continue
		}
		if v, ok := provider["isActive"].(bool); ok && !v {
			continue
		}
		if strings.EqualFold(stringField(provider, "status"), "disabled") {
			continue
		}
		if cloudProviderUnavailable(provider, now) {
			continue
		}
		if cloudProviderCredential(provider) == "" {
			continue
		}
		matches = append(matches, provider)
	}
	sort.SliceStable(matches, func(i, j int) bool {
		ip, jp := cloudIntField(matches[i], "priority"), cloudIntField(matches[j], "priority")
		if ip != jp {
			return ip < jp
		}
		return cloudIntField(matches[i], "globalPriority") < cloudIntField(matches[j], "globalPriority")
	})
	return matches
}

type cloudLimitedState struct {
	Until     time.Time
	LastError string
	Code      string
}

func cloudAllRateLimited(data map[string]any, providerName string) (cloudLimitedState, bool) {
	rawProviders, ok := data["providers"].(map[string]any)
	if !ok {
		return cloudLimitedState{}, false
	}
	var out cloudLimitedState
	now := time.Now()
	foundActive := false
	for _, raw := range rawProviders {
		provider, ok := raw.(map[string]any)
		if !ok || !strings.EqualFold(stringField(provider, "provider"), providerName) {
			continue
		}
		if v, ok := provider["isActive"].(bool); ok && !v {
			continue
		}
		if cloudProviderCredential(provider) == "" {
			continue
		}
		foundActive = true
		until, ok := cloudUnavailableUntil(provider)
		if !ok || !until.After(now) {
			return cloudLimitedState{}, false
		}
		if out.Until.IsZero() || until.Before(out.Until) {
			out.Until = until
			out.LastError = stringField(provider, "lastError")
			out.Code = stringField(provider, "errorCode")
		}
	}
	return out, foundActive && !out.Until.IsZero()
}

func writeCloudAllRateLimited(w http.ResponseWriter, provider, model string, limited cloudLimitedState) {
	retryAfter := int(time.Until(limited.Until).Seconds())
	if retryAfter < 1 {
		retryAfter = 1
	}
	status := http.StatusServiceUnavailable
	if n, err := strconv.Atoi(limited.Code); err == nil && n >= 400 {
		status = n
	}
	message := limited.LastError
	if strings.TrimSpace(message) == "" {
		message = "Unavailable"
	}
	w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
	writeJSON(w, status, map[string]any{"error": map[string]any{"message": "[" + provider + "/" + model + "] " + message}})
}

func cloudProviderUnavailable(provider map[string]any, now time.Time) bool {
	until, ok := cloudUnavailableUntil(provider)
	if !ok {
		return false
	}
	return until.After(now)
}

func cloudUnavailableUntil(provider map[string]any) (time.Time, bool) {
	for _, key := range []string{"rateLimitedUntil", "cooldownUntil"} {
		raw := stringField(provider, key)
		if raw == "" {
			continue
		}
		until, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			until, err = time.Parse(time.RFC3339, raw)
		}
		if err == nil {
			return until, true
		}
	}
	return time.Time{}, false
}

func cloudCooldownActive(provider map[string]any, now time.Time) bool {
	untilRaw := stringField(provider, "cooldownUntil")
	if untilRaw == "" {
		return false
	}
	until, err := time.Parse(time.RFC3339Nano, untilRaw)
	if err != nil {
		until, err = time.Parse(time.RFC3339, untilRaw)
	}
	return err == nil && until.After(now)
}

func (s *Server) forwardCloudWithFallback(ctx context.Context, machineID string, providers []map[string]any, suffix string, reqBody []byte) (*http.Response, []byte, int, error) {
	var lastResp *http.Response
	var lastBody []byte
	var lastErr error
	var lastStatus int
	for i, provider := range providers {
		provider = s.refreshCloudProviderIfNeeded(ctx, machineID, provider)
		upstreamURL, err := cloudProviderURL(provider, suffix)
		if err != nil {
			return nil, nil, http.StatusBadRequest, err
		}
		upstreamReq, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(reqBody))
		if err != nil {
			return nil, nil, http.StatusBadRequest, err
		}
		setCloudProviderHeaders(upstreamReq, provider)
		resp, err := s.client.Do(upstreamReq)
		if err != nil {
			lastErr = err
			lastStatus = http.StatusBadGateway
			s.markCloudProviderCooldown(ctx, machineID, stringField(provider, "id"), "network_error", err.Error(), "")
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		if readErr != nil {
			_ = resp.Body.Close()
			lastErr = readErr
			lastStatus = http.StatusBadGateway
			s.markCloudProviderCooldown(ctx, machineID, stringField(provider, "id"), "read_error", readErr.Error(), "")
			continue
		}
		_ = resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(body))
		lastResp, lastBody, lastStatus = resp, body, resp.StatusCode
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			s.clearCloudProviderCooldown(ctx, machineID, stringField(provider, "id"))
			return resp, body, 0, nil
		}
		if !cloudStatusRetryable(resp.StatusCode) {
			return resp, body, 0, nil
		}
		s.markCloudProviderCooldown(ctx, machineID, stringField(provider, "id"), strconv.Itoa(resp.StatusCode), string(body), resp.Header.Get("Retry-After"))
		if i == len(providers)-1 {
			return resp, body, 0, nil
		}
	}
	if lastResp != nil {
		return lastResp, lastBody, 0, nil
	}
	if lastErr != nil {
		return nil, nil, lastStatus, lastErr
	}
	return nil, nil, http.StatusBadRequest, errString("No active provider credentials for model")
}

func cloudStatusRetryable(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusConflict, http.StatusTooEarly, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func (s *Server) markCloudProviderCooldown(ctx context.Context, machineID, providerID, code, message, retryAfter string) {
	if providerID == "" {
		return
	}
	cooldown := 30 * time.Second
	if retryAfter != "" {
		if seconds, err := strconv.Atoi(retryAfter); err == nil && seconds > 0 {
			cooldown = time.Duration(seconds) * time.Second
		}
	}
	s.updateCloudProvider(ctx, machineID, providerID, func(provider map[string]any) {
		now := time.Now().UTC()
		backoffLevel := cloudIntField(provider, "backoffLevel") + 1
		if backoffLevel > 6 {
			backoffLevel = 6
		}
		provider["status"] = "unavailable"
		provider["lastError"] = truncateCloudError(strings.TrimSpace(message))
		provider["lastErrorAt"] = now.Format(time.RFC3339Nano)
		provider["errorCode"] = code
		provider["cooldownUntil"] = now.Add(cooldown).Format(time.RFC3339Nano)
		provider["rateLimitedUntil"] = now.Add(cooldown).Format(time.RFC3339Nano)
		provider["backoffLevel"] = backoffLevel
		provider["updatedAt"] = now.Format(time.RFC3339Nano)
	})
}

func (s *Server) clearCloudProviderCooldown(ctx context.Context, machineID, providerID string) {
	if providerID == "" {
		return
	}
	s.updateCloudProvider(ctx, machineID, providerID, func(provider map[string]any) {
		if strings.EqualFold(stringField(provider, "status"), "cooldown") || strings.EqualFold(stringField(provider, "status"), "unavailable") {
			provider["status"] = "active"
		}
		provider["lastError"] = nil
		provider["lastErrorAt"] = nil
		provider["errorCode"] = nil
		provider["cooldownUntil"] = nil
		provider["rateLimitedUntil"] = nil
		provider["backoffLevel"] = 0
		provider["updatedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
	})
}

func truncateCloudError(message string) string {
	if len(message) <= 100 {
		return message
	}
	return message[:100]
}

func (s *Server) refreshCloudProviderIfNeeded(ctx context.Context, machineID string, provider map[string]any) map[string]any {
	if !cloudTokenExpiring(provider, 5*time.Minute) || stringField(provider, "refreshToken") == "" {
		return provider
	}
	tokenURL := cloudProviderTokenURL(provider)
	if tokenURL == "" {
		return provider
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", stringField(provider, "refreshToken"))
	if clientID := cloudProviderClientID(provider); clientID != "" {
		form.Set("client_id", clientID)
	}
	if clientSecret := cloudProviderClientSecret(provider); clientSecret != "" {
		form.Set("client_secret", clientSecret)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return provider
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.client.Do(req)
	if err != nil {
		return provider
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return provider
	}
	var parsed map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&parsed); err != nil {
		return provider
	}
	accessToken, _ := parsed["access_token"].(string)
	if accessToken == "" {
		return provider
	}
	updated := copyMap(provider)
	updated["accessToken"] = accessToken
	if refreshToken, _ := parsed["refresh_token"].(string); refreshToken != "" {
		updated["refreshToken"] = refreshToken
	}
	if expiresIn := cloudIntField(parsed, "expires_in"); expiresIn > 0 {
		updated["expiresAt"] = time.Now().UTC().Add(time.Duration(expiresIn) * time.Second).Format(time.RFC3339Nano)
		updated["expiresIn"] = expiresIn
	}
	updated["updatedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
	providerID := stringField(provider, "id")
	s.updateCloudProvider(ctx, machineID, providerID, func(p map[string]any) {
		for k, v := range updated {
			p[k] = v
		}
	})
	return updated
}

func cloudTokenExpiring(provider map[string]any, buffer time.Duration) bool {
	raw := stringField(provider, "expiresAt")
	if raw == "" {
		return false
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		expiresAt, err = time.Parse(time.RFC3339, raw)
	}
	return err == nil && time.Until(expiresAt) < buffer
}

func cloudProviderTokenURL(provider map[string]any) string {
	for _, key := range []string{"tokenUrl", "tokenURL", "token_url"} {
		if value := strings.TrimSpace(stringField(provider, key)); value != "" {
			return value
		}
	}
	if specific, ok := provider["providerSpecificData"].(map[string]any); ok {
		for _, key := range []string{"tokenUrl", "tokenURL", "token_url"} {
			if value := strings.TrimSpace(stringField(specific, key)); value != "" {
				return value
			}
		}
	}
	return ""
}

func cloudProviderClientID(provider map[string]any) string {
	for _, key := range []string{"clientId", "clientID", "client_id"} {
		if value := strings.TrimSpace(stringField(provider, key)); value != "" {
			return value
		}
	}
	if specific, ok := provider["providerSpecificData"].(map[string]any); ok {
		for _, key := range []string{"clientId", "clientID", "client_id"} {
			if value := strings.TrimSpace(stringField(specific, key)); value != "" {
				return value
			}
		}
	}
	return ""
}

func cloudProviderClientSecret(provider map[string]any) string {
	for _, key := range []string{"clientSecret", "client_secret"} {
		if value := strings.TrimSpace(stringField(provider, key)); value != "" {
			return value
		}
	}
	if specific, ok := provider["providerSpecificData"].(map[string]any); ok {
		for _, key := range []string{"clientSecret", "client_secret"} {
			if value := strings.TrimSpace(stringField(specific, key)); value != "" {
				return value
			}
		}
	}
	return ""
}

func (s *Server) updateCloudProvider(ctx context.Context, machineID, providerID string, update func(map[string]any)) {
	s.mu.Lock()
	data, ok := s.syncs[machineID]
	if !ok {
		s.mu.Unlock()
		return
	}
	providers, ok := data["providers"].(map[string]any)
	if !ok {
		s.mu.Unlock()
		return
	}
	provider, ok := providers[providerID].(map[string]any)
	if !ok {
		s.mu.Unlock()
		return
	}
	update(provider)
	data["updatedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
	s.mu.Unlock()
	_ = s.storage.Save(ctx, machineID, data)
}

func cloudProviderURL(provider map[string]any, suffix string) (string, error) {
	baseURL := cloudProviderBaseURL(provider)
	if baseURL == "" {
		baseURL = defaultCloudProviderBaseURL(stringField(provider, "provider"))
	}
	if baseURL == "" {
		return "", errString("Provider baseUrl required")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errString("Invalid provider baseUrl")
	}
	if strings.HasSuffix(strings.TrimRight(parsed.Path, "/"), "/v1") && strings.HasPrefix(suffix, "/v1/") {
		suffix = strings.TrimPrefix(suffix, "/v1")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/" + strings.TrimLeft(suffix, "/")
	return parsed.String(), nil
}

func cloudProviderBaseURL(provider map[string]any) string {
	for _, key := range []string{"baseUrl", "baseURL", "apiBaseUrl", "apiBaseURL"} {
		if value := strings.TrimSpace(stringField(provider, key)); value != "" {
			return value
		}
	}
	if specific, ok := provider["providerSpecificData"].(map[string]any); ok {
		for _, key := range []string{"baseUrl", "baseURL", "apiBaseUrl", "apiBaseURL"} {
			if value := strings.TrimSpace(stringField(specific, key)); value != "" {
				return value
			}
		}
	}
	return ""
}

func defaultCloudProviderBaseURL(provider string) string {
	switch strings.ToLower(provider) {
	case "openai":
		return "https://api.openai.com"
	case "anthropic", "claude":
		return "https://api.anthropic.com"
	default:
		return ""
	}
}

func setCloudProviderHeaders(req *http.Request, provider map[string]any) {
	req.Header.Set("Content-Type", "application/json")
	for k, v := range cloudProviderExtraHeaders(provider) {
		req.Header.Set(k, v)
	}
	token := cloudProviderCredential(provider)
	switch strings.ToLower(stringField(provider, "provider")) {
	case "anthropic", "claude":
		req.Header.Set("x-api-key", token)
		if req.Header.Get("anthropic-version") == "" {
			req.Header.Set("anthropic-version", "2023-06-01")
		}
	default:
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

func cloudProviderExtraHeaders(provider map[string]any) map[string]string {
	headers := map[string]string{}
	for _, raw := range []any{provider["headers"], provider["extraHeaders"]} {
		if values, ok := raw.(map[string]any); ok {
			for k, v := range values {
				if s, ok := v.(string); ok && strings.TrimSpace(k) != "" {
					headers[k] = s
				}
			}
		}
	}
	if specific, ok := provider["providerSpecificData"].(map[string]any); ok {
		for _, raw := range []any{specific["headers"], specific["extraHeaders"]} {
			if values, ok := raw.(map[string]any); ok {
				for k, v := range values {
					if s, ok := v.(string); ok && strings.TrimSpace(k) != "" {
						headers[k] = s
					}
				}
			}
		}
	}
	return headers
}

func cloudProviderCredential(provider map[string]any) string {
	for _, key := range []string{"accessToken", "access_token", "apiKey", "api_key", "token"} {
		if value := strings.TrimSpace(stringField(provider, key)); value != "" {
			return value
		}
	}
	if specific, ok := provider["providerSpecificData"].(map[string]any); ok {
		for _, key := range []string{"accessToken", "access_token", "apiKey", "api_key", "token"} {
			if value := strings.TrimSpace(stringField(specific, key)); value != "" {
				return value
			}
		}
	}
	return ""
}

func cloudIntField(m map[string]any, key string) int {
	switch v := m[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		n, err := strconv.Atoi(v)
		if err == nil {
			return n
		}
	}
	return 0
}

type errString string

func (e errString) Error() string { return string(e) }

func valueOr(m map[string]any, key string, fallback any) any {
	if v, ok := m[key]; ok {
		return v
	}
	return fallback
}

func cloudOpenAIToOllama(requestBody map[string]any, responseBody []byte) map[string]any {
	var upstream map[string]any
	_ = json.Unmarshal(responseBody, &upstream)
	model := stringField(upstream, "model")
	if model == "" {
		model = stringField(requestBody, "model")
	}
	content := ""
	if choices, ok := upstream["choices"].([]any); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]any); ok {
			if message, ok := choice["message"].(map[string]any); ok {
				switch v := message["content"].(type) {
				case string:
					content = v
				default:
					if v != nil {
						b, _ := json.Marshal(v)
						content = string(b)
					}
				}
			}
		}
	}
	out := map[string]any{
		"model":      model,
		"created_at": time.Now().UTC().Format(time.RFC3339Nano),
		"message":    map[string]any{"role": "assistant", "content": content},
		"done":       true,
	}
	if usage, ok := upstream["usage"].(map[string]any); ok {
		if v, ok := usage["prompt_tokens"]; ok {
			out["prompt_eval_count"] = v
		}
		if v, ok := usage["completion_tokens"]; ok {
			out["eval_count"] = v
		}
	}
	return out
}

func normalizePath(path string) string {
	if strings.HasPrefix(path, "/v1/v1/") {
		return strings.Replace(path, "/v1/v1/", "/v1/", 1)
	}
	if path == "/v1/v1" {
		return "/v1"
	}
	return path
}

func isCloudInferencePath(path string) bool {
	if path == "/v1/chat/completions" || path == "/v1/messages" || path == "/v1/messages/count_tokens" || path == "/v1/embeddings" || path == "/v1/responses" || path == "/v1/responses/compact" || path == "/v1/api/chat" {
		return true
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 3 && parts[1] == "v1" {
		suffix := "/" + strings.Join(parts[1:], "/")
		return isCloudInferencePath(suffix)
	}
	return false
}

func isMachineVerifyPath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return len(parts) == 3 && parts[1] == "v1" && parts[2] == "verify"
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
