package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/edodoyokz/ai-go-router/internal/config"
	"github.com/edodoyokz/ai-go-router/internal/providers/endpoints"
)

type MediaSearchAdapter struct {
	name        string
	providerID  string
	baseURL     string
	apiKey      string
	headers     map[string]string
	data        map[string]any
	errorConfig config.ErrorConfig
	client      *http.Client
}

type TTSVoice struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Locale           string `json:"locale,omitempty"`
	Lang             string `json:"lang,omitempty"`
	Country          string `json:"country,omitempty"`
	CountryName      string `json:"countryName,omitempty"`
	LangName         string `json:"langName,omitempty"`
	Gender           string `json:"gender,omitempty"`
	Category         string `json:"category,omitempty"`
	FreeUsersAllowed *bool  `json:"free_users_allowed,omitempty"`
}

type TTSLanguageGroup struct {
	Code   string     `json:"code"`
	Name   string     `json:"name"`
	Voices []TTSVoice `json:"voices"`
}

type TTSVoicesResponse struct {
	Voices    []TTSVoice                  `json:"voices"`
	Languages []TTSLanguageGroup          `json:"languages"`
	ByLang    map[string]TTSLanguageGroup `json:"byLang"`
}

type TTSVoiceLister interface {
	ListTTSVoices(ctx context.Context, lang string) (TTSVoicesResponse, error)
}

func NewMediaSearchAdapter(cfg config.ProviderConfig, errorConfig config.ErrorConfig, proxyURL string) *MediaSearchAdapter {
	providerID := normalizeMediaSearchProviderID(firstNonEmptyString(cfg.ProviderID, cfg.Type))
	apiKey := cfg.APIKey
	for _, account := range cfg.Accounts {
		if apiKey != "" {
			break
		}
		if account.APIKey != "" || account.AccessToken != "" {
			apiKey = firstNonEmptyString(account.APIKey, account.AccessToken)
		}
	}
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultMediaSearchBaseURL(providerID)
	}
	return &MediaSearchAdapter{
		name:        cfg.Name,
		providerID:  providerID,
		baseURL:     baseURL,
		apiKey:      apiKey,
		headers:     cfg.Headers,
		data:        cfg.ProviderSpecificData,
		errorConfig: errorConfig,
		client:      createHTTPClient(proxyURL),
	}
}

func normalizeMediaSearchProviderID(id string) string {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "el":
		return "elevenlabs"
	case "dg":
		return "deepgram"
	case "aai":
		return "assemblyai"
	case "nb":
		return "nanobanana"
	case "hf":
		return "huggingface"
	case "brave":
		return "brave-search"
	case "googletts", "google_tts":
		return "google-tts"
	case "edgetts", "edge_tts":
		return "edge-tts"
	default:
		return strings.ToLower(strings.TrimSpace(id))
	}
}

func (a *MediaSearchAdapter) Name() string { return a.name }

func (a *MediaSearchAdapter) ChatCompletion(context.Context, ChatRequest, string) (ChatResponse, error) {
	return ChatResponse{}, NewNonRetryableError(a.name, "", "chat completions not supported by media/search adapter", nil)
}

func (a *MediaSearchAdapter) StreamChatCompletion(context.Context, ChatRequest, string) (<-chan ChatChunk, error) {
	return nil, NewNonRetryableError(a.name, "", "streaming chat not supported by media/search adapter", nil)
}

func (a *MediaSearchAdapter) GetUsage(context.Context) (map[string]interface{}, error) {
	return map[string]interface{}{"unsupported": true, "provider": a.providerID}, nil
}

func (a *MediaSearchAdapter) Embeddings(context.Context, EmbeddingsRequest, string) (EmbeddingsResponse, error) {
	return EmbeddingsResponse{}, NewNonRetryableError(a.name, "", "embeddings not supported by media/search adapter", nil)
}

func (a *MediaSearchAdapter) AudioSpeech(ctx context.Context, request AudioSpeechRequest, model string) (AudioSpeechResponse, error) {
	switch a.providerID {
	case "elevenlabs":
		return a.elevenLabsSpeech(ctx, request, model)
	case "cartesia":
		return a.cartesiaSpeech(ctx, request, model)
	case "playht":
		return a.playHTSpeech(ctx, request, model)
	case "google-tts":
		return a.googleSpeech(ctx, request, model)
	case "edge-tts":
		return a.edgeSpeech(ctx, request, model)
	case "local-device":
		return a.localDeviceSpeech(ctx, request, model)
	default:
		return AudioSpeechResponse{}, NewNonRetryableError(a.name, model, "audio speech not supported by "+a.providerID, nil)
	}
}

func (a *MediaSearchAdapter) ListTTSVoices(ctx context.Context, lang string) (TTSVoicesResponse, error) {
	var voices []TTSVoice
	var err error
	switch a.providerID {
	case "elevenlabs":
		voices, err = a.elevenLabsVoices(ctx)
	case "edge-tts":
		voices, err = a.edgeVoices(ctx)
	case "local-device":
		voices, err = a.localDeviceVoices(ctx)
	case "google-tts":
		voices = googleTTSVoices()
	case "cartesia":
		voices = []TTSVoice{{ID: "default", Name: "Default", Lang: "en", Locale: "en", LangName: "English"}}
	case "playht":
		voices = []TTSVoice{{ID: "s3://voice-cloning-zero-shot/default", Name: "Default", Lang: "en", Locale: "en", LangName: "English"}}
	default:
		return TTSVoicesResponse{}, NewNonRetryableError(a.name, "", "voice listing not supported by "+a.providerID, nil)
	}
	if err != nil {
		return TTSVoicesResponse{}, err
	}
	if lang != "" {
		filtered := voices[:0]
		for _, voice := range voices {
			if voice.Lang == lang {
				filtered = append(filtered, voice)
			}
		}
		voices = filtered
	}
	return groupTTSVoices(voices), nil
}

func (a *MediaSearchAdapter) ImagesGenerations(ctx context.Context, request ImagesGenerationsRequest, model string) (ImagesGenerationsResponse, error) {
	switch a.providerID {
	case "nanobanana", "sdwebui", "comfyui", "huggingface":
		return a.imageGeneration(ctx, request, model)
	default:
		return ImagesGenerationsResponse{}, NewNonRetryableError(a.name, model, "image generation not supported by "+a.providerID, nil)
	}
}

func (a *MediaSearchAdapter) TranscribeAudio(ctx context.Context, request AudioTranscriptionRequest, model string) (AudioTranscriptionResponse, error) {
	switch a.providerID {
	case "deepgram":
		return a.deepgramTranscribe(ctx, request, model)
	case "assemblyai":
		return a.assemblyAITranscribe(ctx, request, model)
	default:
		return AudioTranscriptionResponse{}, NewNonRetryableError(a.name, model, "audio transcription not supported by "+a.providerID, nil)
	}
}

func (a *MediaSearchAdapter) WebSearch(ctx context.Context, request WebSearchRequest) (WebSearchResponse, error) {
	switch a.providerID {
	case "tavily":
		return a.tavilySearch(ctx, request)
	case "brave-search":
		return a.braveSearch(ctx, request)
	case "serper":
		return a.serperSearch(ctx, request)
	case "exa":
		return a.exaSearch(ctx, request)
	case "searxng":
		return a.searxngSearch(ctx, request)
	default:
		return WebSearchResponse{}, NewNonRetryableError(a.name, "", "web search not supported by "+a.providerID, nil)
	}
}

func (a *MediaSearchAdapter) WebFetch(ctx context.Context, request WebFetchRequest) (WebFetchResponse, error) {
	switch a.providerID {
	case "firecrawl":
		return a.firecrawlFetch(ctx, request)
	default:
		return WebFetchResponse{}, NewNonRetryableError(a.name, "", "web fetch not supported by "+a.providerID, nil)
	}
}

func (a *MediaSearchAdapter) elevenLabsSpeech(ctx context.Context, request AudioSpeechRequest, model string) (AudioSpeechResponse, error) {
	if a.apiKey == "" {
		return AudioSpeechResponse{}, NewNonRetryableError(a.name, model, "elevenlabs api key is required", nil)
	}
	voice := firstNonEmptyString(request.Voice, "Rachel")
	body := map[string]any{
		"text":           request.Input,
		"model_id":       firstNonEmptyString(model, request.Model, "eleven_flash_v2_5"),
		"voice_settings": map[string]any{"stability": 0.5, "similarity_boost": 0.75},
	}
	endpoint := buildURL(a.baseURL, "/v1/text-to-speech/"+url.PathEscape(voice))
	req, err := a.newJSONRequest(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return AudioSpeechResponse{}, NewNonRetryableError(a.name, model, "failed to create request", err)
	}
	req.Header.Set("xi-api-key", a.apiKey)
	respBody, contentType, err := a.doBytes(req, model)
	if err != nil {
		return AudioSpeechResponse{}, err
	}
	if contentType == "" {
		contentType = "audio/mpeg"
	}
	return AudioSpeechResponse{ContentType: contentType, Data: respBody}, nil
}

func (a *MediaSearchAdapter) cartesiaSpeech(ctx context.Context, request AudioSpeechRequest, model string) (AudioSpeechResponse, error) {
	if a.apiKey == "" {
		return AudioSpeechResponse{}, NewNonRetryableError(a.name, model, "cartesia api key is required", nil)
	}
	voice := firstNonEmptyString(request.Voice, stringFromMap(a.data, "voice_id", "voiceId"), "default")
	body := map[string]any{
		"model_id":   firstNonEmptyString(model, request.Model, "sonic-3"),
		"transcript": request.Input,
		"voice":      map[string]any{"mode": "id", "id": voice},
		"output_format": map[string]any{
			"container": firstNonEmptyString(request.ResponseFormat, "mp3"),
		},
	}
	req, err := a.newJSONRequest(ctx, http.MethodPost, buildURL(a.baseURL, "/tts/bytes"), body)
	if err != nil {
		return AudioSpeechResponse{}, NewNonRetryableError(a.name, model, "failed to create request", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Cartesia-Version", firstNonEmptyString(a.headers["Cartesia-Version"], stringFromMap(a.data, "cartesia_version", "cartesiaVersion"), "2026-03-01"))
	respBody, contentType, err := a.doBytes(req, model)
	if err != nil {
		return AudioSpeechResponse{}, err
	}
	return AudioSpeechResponse{ContentType: firstNonEmptyString(contentType, "audio/mpeg"), Data: respBody}, nil
}

func (a *MediaSearchAdapter) playHTSpeech(ctx context.Context, request AudioSpeechRequest, model string) (AudioSpeechResponse, error) {
	if a.apiKey == "" {
		return AudioSpeechResponse{}, NewNonRetryableError(a.name, model, "playht api key is required", nil)
	}
	userID := firstNonEmptyString(a.headers["X-USER-ID"], stringFromMap(a.data, "userId", "user_id", "playht_user_id"))
	if userID == "" {
		return AudioSpeechResponse{}, NewNonRetryableError(a.name, model, "playht X-USER-ID is required", nil)
	}
	body := map[string]any{
		"text":          request.Input,
		"voice":         firstNonEmptyString(request.Voice, stringFromMap(a.data, "voice"), "s3://voice-cloning-zero-shot/default"),
		"output_format": firstNonEmptyString(request.ResponseFormat, "mp3"),
	}
	if modelID := firstNonEmptyString(model, request.Model); modelID != "" {
		body["model"] = modelID
	}
	req, err := a.newJSONRequest(ctx, http.MethodPost, buildURL(a.baseURL, "/api/v2/tts/stream"), body)
	if err != nil {
		return AudioSpeechResponse{}, NewNonRetryableError(a.name, model, "failed to create request", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("X-USER-ID", userID)
	respBody, contentType, err := a.doBytes(req, model)
	if err != nil {
		return AudioSpeechResponse{}, err
	}
	return AudioSpeechResponse{ContentType: firstNonEmptyString(contentType, "audio/mpeg"), Data: respBody}, nil
}

func (a *MediaSearchAdapter) googleSpeech(ctx context.Context, request AudioSpeechRequest, model string) (AudioSpeechResponse, error) {
	lang := firstNonEmptyString(model, request.Model, request.Voice, "en")
	audio, err := a.googleTTS(ctx, request.Input, lang)
	if err != nil {
		return AudioSpeechResponse{}, err
	}
	return AudioSpeechResponse{ContentType: "audio/mpeg", Data: audio}, nil
}

func (a *MediaSearchAdapter) edgeSpeech(ctx context.Context, request AudioSpeechRequest, model string) (AudioSpeechResponse, error) {
	voice := firstNonEmptyString(request.Voice, model, request.Model, "vi-VN-HoaiMyNeural")
	audio, err := a.edgeTTS(ctx, request.Input, voice)
	if err != nil {
		return AudioSpeechResponse{}, err
	}
	return AudioSpeechResponse{ContentType: "audio/mpeg", Data: audio}, nil
}

func (a *MediaSearchAdapter) localDeviceSpeech(ctx context.Context, request AudioSpeechRequest, model string) (AudioSpeechResponse, error) {
	voice := firstNonEmptyString(request.Voice, model, request.Model)
	audio, err := localDeviceTTS(ctx, request.Input, voice)
	if err != nil {
		return AudioSpeechResponse{}, NewNonRetryableError(a.name, model, "local-device tts failed", err)
	}
	return AudioSpeechResponse{ContentType: "audio/mpeg", Data: audio}, nil
}

func (a *MediaSearchAdapter) imageGeneration(ctx context.Context, request ImagesGenerationsRequest, model string) (ImagesGenerationsResponse, error) {
	body := map[string]any{"prompt": request.Prompt, "model": firstNonEmptyString(model, request.Model)}
	if request.N > 0 {
		body["n"] = request.N
	}
	if request.Size != "" {
		body["size"] = request.Size
	}
	path := "/v1/images/generations"
	if a.providerID == "sdwebui" {
		path = "/sdapi/v1/txt2img"
		body = sdWebUIBody(request)
	}
	if a.providerID == "nanobanana" {
		body = nanoBananaBody(request, firstNonEmptyString(model, request.Model))
	}
	endpoint := buildURL(a.baseURL, path)
	if a.providerID == "huggingface" && firstNonEmptyString(model, request.Model) != "" {
		endpoint = buildURL(a.baseURL, "/"+strings.TrimPrefix(firstNonEmptyString(model, request.Model), "/"))
	}
	req, err := a.newJSONRequest(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return ImagesGenerationsResponse{}, NewNonRetryableError(a.name, model, "failed to create request", err)
	}
	a.applyBearer(req)
	if a.providerID == "huggingface" {
		respBody, contentType, err := a.doBytes(req, model)
		if err != nil {
			return ImagesGenerationsResponse{}, err
		}
		return ImagesGenerationsResponse{Created: time.Now().Unix(), Data: []ImageResponse{{URL: dataURLForBytes(contentType, respBody)}}}, nil
	}
	var raw map[string]any
	if err := a.doJSON(req, model, &raw); err != nil {
		return ImagesGenerationsResponse{}, err
	}
	return normalizeImageResponse(raw, firstNonEmptyString(model, request.Model)), nil
}

func (a *MediaSearchAdapter) deepgramTranscribe(ctx context.Context, request AudioTranscriptionRequest, model string) (AudioTranscriptionResponse, error) {
	if a.apiKey == "" {
		return AudioTranscriptionResponse{}, NewNonRetryableError(a.name, model, "deepgram api key is required", nil)
	}
	endpoint := buildURL(a.baseURL, "/v1/listen")
	req, err := a.audioRequest(ctx, endpoint, request)
	if err != nil {
		return AudioTranscriptionResponse{}, NewNonRetryableError(a.name, model, "failed to create request", err)
	}
	req.Header.Set("Authorization", "Token "+a.apiKey)
	var raw map[string]any
	if err := a.doJSON(req, model, &raw); err != nil {
		return AudioTranscriptionResponse{}, err
	}
	text := nestedString(raw, "results", "channels", "0", "alternatives", "0", "transcript")
	return AudioTranscriptionResponse{Text: text, Raw: raw}, nil
}

func (a *MediaSearchAdapter) assemblyAITranscribe(ctx context.Context, request AudioTranscriptionRequest, model string) (AudioTranscriptionResponse, error) {
	if a.apiKey == "" {
		return AudioTranscriptionResponse{}, NewNonRetryableError(a.name, model, "assemblyai api key is required", nil)
	}
	body := map[string]any{}
	if request.AudioURL != "" {
		body["audio_url"] = request.AudioURL
	} else {
		body["audio_base64"] = request.AudioBase64
	}
	endpoint := buildURL(a.baseURL, "/v1/audio/transcriptions")
	req, err := a.newJSONRequest(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return AudioTranscriptionResponse{}, NewNonRetryableError(a.name, model, "failed to create request", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	var raw map[string]any
	if err := a.doJSON(req, model, &raw); err != nil {
		return AudioTranscriptionResponse{}, err
	}
	return AudioTranscriptionResponse{Text: stringFromAny(raw["text"]), Raw: raw}, nil
}

func (a *MediaSearchAdapter) tavilySearch(ctx context.Context, request WebSearchRequest) (WebSearchResponse, error) {
	body := map[string]any{"query": request.Query, "api_key": a.apiKey}
	if request.MaxResults > 0 {
		body["max_results"] = request.MaxResults
	}
	if request.SearchDepth != "" {
		body["search_depth"] = request.SearchDepth
	}
	var raw map[string]any
	if err := a.postSearchJSON(ctx, "/search", body, &raw); err != nil {
		return WebSearchResponse{}, err
	}
	return normalizeSearchResponse(request.Query, raw, "results"), nil
}

func (a *MediaSearchAdapter) braveSearch(ctx context.Context, request WebSearchRequest) (WebSearchResponse, error) {
	u, _ := url.Parse(buildURL(a.baseURL, "/res/v1/web/search"))
	q := u.Query()
	q.Set("q", request.Query)
	if request.MaxResults > 0 {
		q.Set("count", fmt.Sprintf("%d", request.MaxResults))
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return WebSearchResponse{}, NewNonRetryableError(a.name, "", "failed to create search request", err)
	}
	req.Header.Set("X-Subscription-Token", a.apiKey)
	var raw map[string]any
	if err := a.doJSON(req, "", &raw); err != nil {
		return WebSearchResponse{}, err
	}
	return normalizeSearchResponse(request.Query, map[string]any{"results": nestedAnySlice(raw, "web", "results"), "raw": raw}, "results"), nil
}

func (a *MediaSearchAdapter) serperSearch(ctx context.Context, request WebSearchRequest) (WebSearchResponse, error) {
	body := map[string]any{"q": request.Query}
	var raw map[string]any
	req, err := a.newJSONRequest(ctx, http.MethodPost, buildURL(a.baseURL, "/search"), body)
	if err != nil {
		return WebSearchResponse{}, NewNonRetryableError(a.name, "", "failed to create search request", err)
	}
	req.Header.Set("X-API-KEY", a.apiKey)
	if err := a.doJSON(req, "", &raw); err != nil {
		return WebSearchResponse{}, err
	}
	return normalizeSearchResponse(request.Query, map[string]any{"results": raw["organic"], "raw": raw}, "results"), nil
}

func (a *MediaSearchAdapter) exaSearch(ctx context.Context, request WebSearchRequest) (WebSearchResponse, error) {
	body := map[string]any{"query": request.Query}
	if request.MaxResults > 0 {
		body["numResults"] = request.MaxResults
	}
	var raw map[string]any
	req, err := a.newJSONRequest(ctx, http.MethodPost, buildURL(a.baseURL, "/search"), body)
	if err != nil {
		return WebSearchResponse{}, NewNonRetryableError(a.name, "", "failed to create search request", err)
	}
	req.Header.Set("x-api-key", a.apiKey)
	if err := a.doJSON(req, "", &raw); err != nil {
		return WebSearchResponse{}, err
	}
	return normalizeSearchResponse(request.Query, raw, "results"), nil
}

func (a *MediaSearchAdapter) searxngSearch(ctx context.Context, request WebSearchRequest) (WebSearchResponse, error) {
	u, _ := url.Parse(buildURL(a.baseURL, "/search"))
	q := u.Query()
	q.Set("q", request.Query)
	q.Set("format", "json")
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return WebSearchResponse{}, NewNonRetryableError(a.name, "", "failed to create search request", err)
	}
	var raw map[string]any
	if err := a.doJSON(req, "", &raw); err != nil {
		return WebSearchResponse{}, err
	}
	return normalizeSearchResponse(request.Query, raw, "results"), nil
}

func (a *MediaSearchAdapter) firecrawlFetch(ctx context.Context, request WebFetchRequest) (WebFetchResponse, error) {
	body := map[string]any{"url": request.URL}
	if len(request.Formats) > 0 {
		body["formats"] = request.Formats
	}
	var raw map[string]any
	req, err := a.newJSONRequest(ctx, http.MethodPost, buildURL(a.baseURL, "/v1/scrape"), body)
	if err != nil {
		return WebFetchResponse{}, NewNonRetryableError(a.name, "", "failed to create fetch request", err)
	}
	a.applyBearer(req)
	if err := a.doJSON(req, "", &raw); err != nil {
		return WebFetchResponse{}, err
	}
	data := raw
	if nested, ok := raw["data"].(map[string]any); ok {
		data = nested
	}
	return WebFetchResponse{
		URL:      firstNonEmptyString(stringFromAny(data["url"]), request.URL),
		Title:    stringFromAny(data["title"]),
		Content:  firstNonEmptyString(stringFromAny(data["content"]), stringFromAny(data["text"])),
		Markdown: stringFromAny(data["markdown"]),
		HTML:     stringFromAny(data["html"]),
		Raw:      raw,
	}, nil
}

func (a *MediaSearchAdapter) postSearchJSON(ctx context.Context, path string, body map[string]any, out any) error {
	req, err := a.newJSONRequest(ctx, http.MethodPost, buildURL(a.baseURL, path), body)
	if err != nil {
		return NewNonRetryableError(a.name, "", "failed to create search request", err)
	}
	return a.doJSON(req, "", out)
}

func (a *MediaSearchAdapter) audioRequest(ctx context.Context, endpoint string, request AudioTranscriptionRequest) (*http.Request, error) {
	if request.AudioURL != "" {
		return a.newJSONRequest(ctx, http.MethodPost, endpoint, map[string]any{"url": request.AudioURL})
	}
	audio, err := base64.StdEncoding.DecodeString(request.AudioBase64)
	if err != nil {
		return nil, err
	}
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "audio")
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(audio); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req, nil
}

func (a *MediaSearchAdapter) newJSONRequest(ctx context.Context, method, endpoint string, body any) (*http.Request, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range a.headers {
		req.Header.Set(k, v)
	}
	return req, nil
}

func (a *MediaSearchAdapter) applyBearer(req *http.Request) {
	if a.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.apiKey)
	}
}

func (a *MediaSearchAdapter) doJSON(req *http.Request, model string, out any) error {
	resp, err := a.client.Do(req)
	if err != nil {
		return NewRetryableError(a.name, model, "network error", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return NewRetryableError(a.name, model, "failed to read response", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ClassifyHTTPError(resp.StatusCode, a.name, model, string(body), a.errorConfig)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return &ProviderError{Provider: a.name, Model: model, Type: ErrInvalidUpstreamResponse, Message: "failed to parse response", Cause: err}
	}
	return nil
}

func (a *MediaSearchAdapter) doBytes(req *http.Request, model string) ([]byte, string, error) {
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, "", NewRetryableError(a.name, model, "network error", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", NewRetryableError(a.name, model, "failed to read response", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", ClassifyHTTPError(resp.StatusCode, a.name, model, string(body), a.errorConfig)
	}
	return body, resp.Header.Get("Content-Type"), nil
}

func defaultMediaSearchBaseURL(providerID string) string {
	switch providerID {
	case "elevenlabs":
		return "https://api.elevenlabs.io"
	case "cartesia":
		return "https://api.cartesia.ai"
	case "playht":
		return "https://api.play.ht"
	case "google-tts":
		return "https://translate.google.com"
	case "edge-tts":
		return "https://www.bing.com"
	case "local-device":
		return "local-device"
	case "deepgram":
		return "https://api.deepgram.com"
	case "assemblyai":
		return "https://api.assemblyai.com"
	case "nanobanana":
		return "https://api.nanobananaapi.ai"
	case "sdwebui":
		return "http://127.0.0.1:7860"
	case "comfyui":
		return "http://127.0.0.1:8188"
	case "huggingface":
		return "https://api-inference.huggingface.co/models"
	case "tavily":
		return "https://api.tavily.com"
	case "brave-search":
		return "https://api.search.brave.com"
	case "serper":
		return "https://google.serper.dev"
	case "exa":
		return "https://api.exa.ai"
	case "searxng":
		return "http://127.0.0.1:8080"
	case "firecrawl":
		return "https://api.firecrawl.dev"
	default:
		return ""
	}
}

func buildURL(baseURL, path string) string {
	if strings.Contains(baseURL, "/v1") && (strings.HasPrefix(path, "/v1/") || path == "/v1") {
		return strings.TrimRight(baseURL, "/") + strings.TrimPrefix(path, "/v1")
	}
	return endpoints.NormalizeBaseURL(baseURL) + path
}

func normalizeImageResponse(raw map[string]any, model string) ImagesGenerationsResponse {
	resp := ImagesGenerationsResponse{Created: time.Now().Unix()}
	if data, ok := raw["data"].([]any); ok {
		for _, item := range data {
			m, _ := item.(map[string]any)
			if u := stringFromAny(m["url"]); u != "" {
				resp.Data = append(resp.Data, ImageResponse{URL: u})
			}
			if b64 := stringFromAny(m["b64_json"]); b64 != "" {
				resp.Data = append(resp.Data, ImageResponse{URL: "data:image/png;base64," + b64})
			}
		}
	}
	if images, ok := raw["images"].([]any); ok {
		for _, item := range images {
			if s := stringFromAny(item); s != "" {
				resp.Data = append(resp.Data, ImageResponse{URL: "data:image/png;base64," + s})
			}
		}
	}
	if urlValue := stringFromAny(raw["url"]); urlValue != "" {
		resp.Data = append(resp.Data, ImageResponse{URL: urlValue})
	}
	if imageValue := stringFromAny(raw["image"]); imageValue != "" {
		resp.Data = append(resp.Data, ImageResponse{URL: "data:image/png;base64," + imageValue})
	}
	return resp
}

func nanoBananaBody(request ImagesGenerationsRequest, model string) map[string]any {
	sizeMap := map[string]string{
		"1024x1024": "1:1",
		"1024x1792": "9:16",
		"1792x1024": "16:9",
	}
	if model == "" {
		model = request.Model
	}
	n := request.N
	if n <= 0 {
		n = 1
	}
	ratio := firstNonEmptyString(sizeMap[request.Size], "1:1")
	return map[string]any{"prompt": request.Prompt, "model": model, "type": "TEXTTOIAMGE", "numImages": n, "image_size": ratio}
}

func sdWebUIBody(request ImagesGenerationsRequest) map[string]any {
	width, height := 512, 512
	if request.Size != "" {
		parts := strings.Split(request.Size, "x")
		if len(parts) == 2 {
			if w, ok := parsePositiveInt(parts[0]); ok {
				width = w
			}
			if h, ok := parsePositiveInt(parts[1]); ok {
				height = h
			}
		}
	}
	n := request.N
	if n <= 0 {
		n = 1
	}
	return map[string]any{"prompt": request.Prompt, "width": width, "height": height, "steps": 20, "batch_size": n}
}

func parsePositiveInt(s string) (int, bool) {
	var n int
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, n > 0
}

func dataURLForBytes(contentType string, data []byte) string {
	if contentType == "" {
		contentType = "image/png"
	}
	if i := strings.Index(contentType, ";"); i >= 0 {
		contentType = contentType[:i]
	}
	return "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func normalizeSearchResponse(query string, raw map[string]any, key string) WebSearchResponse {
	items, _ := raw[key].([]any)
	results := make([]WebSearchResult, 0, len(items))
	for _, item := range items {
		m, _ := item.(map[string]any)
		results = append(results, WebSearchResult{
			Title:   firstNonEmptyString(stringFromAny(m["title"]), stringFromAny(m["name"])),
			URL:     firstNonEmptyString(stringFromAny(m["url"]), stringFromAny(m["link"])),
			Content: firstNonEmptyString(stringFromAny(m["content"]), stringFromAny(m["snippet"]), stringFromAny(m["text"])),
			Score:   floatFromAny(m["score"]),
		})
	}
	return WebSearchResponse{Query: query, Results: results, Answer: stringFromAny(raw["answer"]), Raw: raw}
}

func nestedString(raw map[string]any, path ...string) string {
	v := nestedValue(raw, path...)
	return stringFromAny(v)
}

func nestedAnySlice(raw map[string]any, path ...string) []any {
	v := nestedValue(raw, path...)
	if items, ok := v.([]any); ok {
		return items
	}
	return nil
}

func nestedValue(v any, path ...string) any {
	cur := v
	for _, part := range path {
		if idx, ok := parseSmallIndex(part); ok {
			items, _ := cur.([]any)
			if idx < 0 || idx >= len(items) {
				return nil
			}
			cur = items[idx]
			continue
		}
		m, _ := cur.(map[string]any)
		if m == nil {
			return nil
		}
		cur = m[part]
	}
	return cur
}

func parseSmallIndex(s string) (int, bool) {
	if len(s) != 1 || s[0] < '0' || s[0] > '9' {
		return 0, false
	}
	return int(s[0] - '0'), true
}

func stringFromAny(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func floatFromAny(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	default:
		return 0
	}
}

var (
	googleTTSCacheMu sync.Mutex
	googleTTSCache   struct {
		fsid string
		bl   string
		at   time.Time
	}
	edgeTTSCacheMu sync.Mutex
	edgeTTSCache   struct {
		key    string
		token  string
		cookie string
		at     time.Time
	}
	googleFsidRE = regexp.MustCompile(`"FdrFJe":"(.*?)"`)
	googleBLRE   = regexp.MustCompile(`"cfb2h":"(.*?)"`)
	edgeTokenRE  = regexp.MustCompile(`params_AbusePreventionHelper\s*=\s*\[([^,]+),([^,]+),`)
)

const mediaSearchUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36"

func (a *MediaSearchAdapter) googleToken(ctx context.Context) (string, string, error) {
	googleTTSCacheMu.Lock()
	if googleTTSCache.fsid != "" && time.Since(googleTTSCache.at) < 11*time.Minute {
		fsid, bl := googleTTSCache.fsid, googleTTSCache.bl
		googleTTSCacheMu.Unlock()
		return fsid, bl, nil
	}
	googleTTSCacheMu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, buildURL(a.baseURL, "/"), nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", mediaSearchUA)
	resp, err := a.client.Do(req)
	if err != nil {
		return "", "", NewRetryableError(a.name, "", "google tts token fetch failed", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", ClassifyHTTPError(resp.StatusCode, a.name, "", string(body), a.errorConfig)
	}
	fsid := regexFirst(googleFsidRE, string(body))
	bl := regexFirst(googleBLRE, string(body))
	if fsid == "" || bl == "" {
		return "", "", NewNonRetryableError(a.name, "", "failed to parse google tts token", nil)
	}
	googleTTSCacheMu.Lock()
	googleTTSCache.fsid, googleTTSCache.bl, googleTTSCache.at = fsid, bl, time.Now()
	googleTTSCacheMu.Unlock()
	return fsid, bl, nil
}

func (a *MediaSearchAdapter) googleTTS(ctx context.Context, text, lang string) ([]byte, error) {
	fsid, bl, err := a.googleToken(ctx)
	if err != nil {
		return nil, err
	}
	cleanText := strings.NewReplacer("@", " ", "^", " ", "*", " ", "(", " ", ")", " ", "\\", " ", "/", " ", "-", " ", "_", " ", "+", " ", "=", " ", ">", " ", "<", " ", `"`, " ", `'`, " ").Replace(text)
	query := url.Values{}
	query.Set("rpcids", "jQ1olc")
	query.Set("f.sid", fsid)
	query.Set("bl", bl)
	query.Set("hl", lang)
	query.Set("soc-app", "1")
	query.Set("soc-platform", "1")
	query.Set("soc-device", "1")
	query.Set("_reqid", fmt.Sprintf("%d", time.Now().UnixMilli()%100000000))
	query.Set("rt", "c")
	payload, _ := json.Marshal([]any{cleanText, lang, nil, "undefined", []int{0}})
	freq, _ := json.Marshal([]any{[]any{[]any{"jQ1olc", string(payload), nil, "generic"}}})
	form := url.Values{}
	form.Set("f.req", string(freq))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, buildURL(a.baseURL, "/_/TranslateWebserverUi/data/batchexecute")+"?"+query.Encode(), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, NewNonRetryableError(a.name, "", "failed to create google tts request", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", "https://translate.google.com/")
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, NewRetryableError(a.name, "", "google tts request failed", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, ClassifyHTTPError(resp.StatusCode, a.name, "", string(body), a.errorConfig)
	}
	base64Audio := parseGoogleTTSBase64(string(body))
	if base64Audio == "" {
		return nil, NewNonRetryableError(a.name, "", "google tts returned empty audio", nil)
	}
	return base64.StdEncoding.DecodeString(base64Audio)
}

func parseGoogleTTSBase64(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "[") {
			continue
		}
		var outer []any
		if json.Unmarshal([]byte(line), &outer) != nil || len(outer) == 0 {
			continue
		}
		row, _ := outer[0].([]any)
		if len(row) < 3 {
			continue
		}
		innerRaw, _ := row[2].(string)
		var inner []any
		if json.Unmarshal([]byte(innerRaw), &inner) == nil && len(inner) > 0 {
			if audio, _ := inner[0].(string); audio != "" {
				return audio
			}
		}
	}
	return ""
}

func (a *MediaSearchAdapter) edgeToken(ctx context.Context) (string, string, string, error) {
	edgeTTSCacheMu.Lock()
	if edgeTTSCache.key != "" && time.Since(edgeTTSCache.at) < 5*time.Minute {
		key, token, cookie := edgeTTSCache.key, edgeTTSCache.token, edgeTTSCache.cookie
		edgeTTSCacheMu.Unlock()
		return key, token, cookie, nil
	}
	edgeTTSCacheMu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, buildURL(a.baseURL, "/translator"), nil)
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("User-Agent", mediaSearchUA)
	req.Header.Set("Accept-Language", "vi,en-US;q=0.9,en;q=0.8")
	resp, err := a.client.Do(req)
	if err != nil {
		return "", "", "", NewRetryableError(a.name, "", "edge tts token fetch failed", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", "", ClassifyHTTPError(resp.StatusCode, a.name, "", string(body), a.errorConfig)
	}
	m := edgeTokenRE.FindStringSubmatch(string(body))
	if len(m) < 3 {
		return "", "", "", NewNonRetryableError(a.name, "", "failed to parse edge tts token", nil)
	}
	cookies := make([]string, 0, len(resp.Cookies()))
	for _, cookie := range resp.Cookies() {
		cookies = append(cookies, cookie.Name+"="+cookie.Value)
	}
	key := strings.TrimSpace(m[1])
	token := strings.Trim(m[2], `" `)
	cookie := strings.Join(cookies, "; ")
	edgeTTSCacheMu.Lock()
	edgeTTSCache.key, edgeTTSCache.token, edgeTTSCache.cookie, edgeTTSCache.at = key, token, cookie, time.Now()
	edgeTTSCacheMu.Unlock()
	return key, token, cookie, nil
}

func (a *MediaSearchAdapter) edgeTTS(ctx context.Context, text, voice string) ([]byte, error) {
	key, token, cookie, err := a.edgeToken(ctx)
	if err != nil {
		return nil, err
	}
	audio, status, err := a.edgeTTSRequest(ctx, text, voice, key, token, cookie)
	if status == http.StatusTooManyRequests || status == http.StatusForbidden {
		edgeTTSCacheMu.Lock()
		edgeTTSCache = struct {
			key    string
			token  string
			cookie string
			at     time.Time
		}{}
		edgeTTSCacheMu.Unlock()
		key, token, cookie, err = a.edgeToken(ctx)
		if err == nil {
			audio, status, err = a.edgeTTSRequest(ctx, text, voice, key, token, cookie)
		}
	}
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, ClassifyHTTPError(status, a.name, "", "", a.errorConfig)
	}
	return audio, nil
}

func (a *MediaSearchAdapter) edgeTTSRequest(ctx context.Context, text, voice, key, token, cookie string) ([]byte, int, error) {
	parts := strings.Split(voice, "-")
	xmlLang := "en-US"
	if len(parts) >= 2 {
		xmlLang = parts[0] + "-" + parts[1]
	}
	gender := "Female"
	if strings.Contains(strings.ToLower(voice), "male") {
		gender = "Male"
	}
	ssml := fmt.Sprintf("<speak version='1.0' xml:lang='%s'><voice xml:lang='%s' xml:gender='%s' name='%s'><prosody rate='0.00%%'>%s</prosody></voice></speak>", xmlLang, xmlLang, gender, voice, xmlEscape(text))
	form := url.Values{}
	form.Set("ssml", ssml)
	form.Set("token", token)
	form.Set("key", key)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, buildURL(a.baseURL, "/tfettts?isVertical=1&&IG=1&IID=translator.5023&SFX=1"), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, 0, NewNonRetryableError(a.name, "", "failed to create edge tts request", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Origin", "https://www.bing.com")
	req.Header.Set("Referer", "https://www.bing.com/translator")
	req.Header.Set("User-Agent", mediaSearchUA)
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, 0, NewRetryableError(a.name, "", "edge tts request failed", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return body, resp.StatusCode, nil
	}
	if len(body) == 0 {
		return nil, resp.StatusCode, NewNonRetryableError(a.name, "", "edge tts returned empty audio", nil)
	}
	return body, resp.StatusCode, nil
}

func localDeviceTTS(ctx context.Context, text, voice string) ([]byte, error) {
	switch runtime.GOOS {
	case "darwin":
		return localDeviceMacTTS(ctx, text, voice)
	case "windows":
		return nil, fmt.Errorf("windows local-device tts is not available in this Go build")
	default:
		return localDeviceLinuxTTS(ctx, text, voice)
	}
}

func localDeviceMacTTS(ctx context.Context, text, voice string) ([]byte, error) {
	sayPath, err := exec.LookPath("say")
	if err != nil {
		return nil, fmt.Errorf("say command not found")
	}
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("ffmpeg command not found")
	}
	dir, err := os.MkdirTemp("", "9router-tts-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	aiff := filepath.Join(dir, "out.aiff")
	mp3 := filepath.Join(dir, "out.mp3")
	args := []string{"-o", aiff, text}
	if voice != "" {
		args = []string{"-v", voice, "-o", aiff, text}
	}
	if out, err := exec.CommandContext(ctx, sayPath, args...).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("say failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	if out, err := exec.CommandContext(ctx, ffmpegPath, "-y", "-i", aiff, "-codec:a", "libmp3lame", "-qscale:a", "4", mp3).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("ffmpeg failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return os.ReadFile(mp3)
}

func localDeviceLinuxTTS(ctx context.Context, text, voice string) ([]byte, error) {
	espeakPath, err := exec.LookPath("espeak")
	if err != nil {
		return nil, fmt.Errorf("espeak command not found")
	}
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("ffmpeg command not found")
	}
	dir, err := os.MkdirTemp("", "9router-tts-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	wav := filepath.Join(dir, "out.wav")
	mp3 := filepath.Join(dir, "out.mp3")
	args := []string{"-w", wav}
	if voice != "" {
		args = append(args, "-v", voice)
	}
	args = append(args, text)
	if out, err := exec.CommandContext(ctx, espeakPath, args...).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("espeak failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	if out, err := exec.CommandContext(ctx, ffmpegPath, "-y", "-i", wav, "-codec:a", "libmp3lame", "-qscale:a", "4", mp3).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("ffmpeg failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return os.ReadFile(mp3)
}

func (a *MediaSearchAdapter) elevenLabsVoices(ctx context.Context) ([]TTSVoice, error) {
	if a.apiKey == "" {
		return nil, NewNonRetryableError(a.name, "", "elevenlabs api key is required", nil)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, buildURL(a.baseURL, "/v1/voices"), nil)
	if err != nil {
		return nil, NewNonRetryableError(a.name, "", "failed to create voices request", err)
	}
	req.Header.Set("xi-api-key", a.apiKey)
	var raw struct {
		Voices []map[string]any `json:"voices"`
	}
	if err := a.doJSON(req, "", &raw); err != nil {
		return nil, err
	}
	voices := make([]TTSVoice, 0, len(raw.Voices))
	for _, v := range raw.Voices {
		labels, _ := v["labels"].(map[string]any)
		lang := firstNonEmptyString(stringFromAny(labels["language"]), "en")
		free := stringFromAny(v["category"]) == "premade"
		voices = append(voices, TTSVoice{
			ID:               stringFromAny(v["voice_id"]),
			Name:             stringFromAny(v["name"]),
			Locale:           lang,
			Lang:             strings.Split(lang, "-")[0],
			LangName:         languageName(strings.Split(lang, "-")[0]),
			Gender:           stringFromAny(labels["gender"]),
			Category:         stringFromAny(v["category"]),
			FreeUsersAllowed: &free,
		})
	}
	return voices, nil
}

func (a *MediaSearchAdapter) edgeVoices(ctx context.Context) ([]TTSVoice, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://speech.platform.bing.com/consumer/speech/synthesize/readaloud/voices/list?trustedclienttoken=6A5AA1D4EAFF4E9FB37E23D68491D6F4", nil)
	if err != nil {
		return nil, NewNonRetryableError(a.name, "", "failed to create edge voices request", err)
	}
	req.Header.Set("User-Agent", mediaSearchUA)
	var raw []map[string]any
	if err := a.doJSON(req, "", &raw); err != nil {
		return nil, err
	}
	voices := make([]TTSVoice, 0, len(raw))
	for _, v := range raw {
		locale := stringFromAny(v["Locale"])
		parts := strings.Split(locale, "-")
		lang, country := locale, ""
		if len(parts) > 0 {
			lang = parts[0]
		}
		if len(parts) > 1 {
			country = parts[1]
		}
		name := firstNonEmptyString(stringFromAny(v["FriendlyName"]), stringFromAny(v["ShortName"]))
		name = strings.ReplaceAll(name, "Microsoft ", "")
		name = strings.ReplaceAll(name, " Online (Natural) - ", " (")
		voices = append(voices, TTSVoice{ID: stringFromAny(v["ShortName"]), Name: name, Locale: locale, Lang: lang, Country: country, CountryName: countryName(country), LangName: languageName(lang), Gender: stringFromAny(v["Gender"])})
	}
	return voices, nil
}

func (a *MediaSearchAdapter) localDeviceVoices(ctx context.Context) ([]TTSVoice, error) {
	if runtime.GOOS == "darwin" {
		sayPath, err := exec.LookPath("say")
		if err != nil {
			return []TTSVoice{}, nil
		}
		out, err := exec.CommandContext(ctx, sayPath, "-v", "?").Output()
		if err != nil {
			return []TTSVoice{}, nil
		}
		return parseMacVoices(string(out)), nil
	}
	return []TTSVoice{{ID: "default", Name: "System Default Voice", Locale: "en-US", Lang: "en", Country: "US", CountryName: "United States", LangName: "English"}}, nil
}

func parseMacVoices(out string) []TTSVoice {
	voices := []TTSVoice{}
	re := regexp.MustCompile(`^([^\s].*?)\s{2,}([a-z]{2})_([A-Z]{2})`)
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		m := re.FindStringSubmatch(scanner.Text())
		if len(m) != 4 {
			continue
		}
		locale := m[2] + "-" + m[3]
		voices = append(voices, TTSVoice{ID: strings.TrimSpace(m[1]), Name: strings.TrimSpace(m[1]), Locale: locale, Lang: m[2], Country: m[3], CountryName: countryName(m[3]), LangName: languageName(m[2])})
	}
	return voices
}

func googleTTSVoices() []TTSVoice {
	return []TTSVoice{
		{ID: "en", Name: "English", Locale: "en", Lang: "en", LangName: "English"},
		{ID: "id", Name: "Indonesian", Locale: "id", Lang: "id", LangName: "Indonesian"},
		{ID: "ja", Name: "Japanese", Locale: "ja", Lang: "ja", LangName: "Japanese"},
		{ID: "ko", Name: "Korean", Locale: "ko", Lang: "ko", LangName: "Korean"},
		{ID: "zh-CN", Name: "Chinese", Locale: "zh-CN", Lang: "zh", Country: "CN", CountryName: "China", LangName: "Chinese"},
		{ID: "fr", Name: "French", Locale: "fr", Lang: "fr", LangName: "French"},
		{ID: "de", Name: "German", Locale: "de", Lang: "de", LangName: "German"},
		{ID: "es", Name: "Spanish", Locale: "es", Lang: "es", LangName: "Spanish"},
	}
}

func groupTTSVoices(voices []TTSVoice) TTSVoicesResponse {
	byLang := map[string]TTSLanguageGroup{}
	for _, voice := range voices {
		lang := firstNonEmptyString(voice.Lang, strings.Split(voice.Locale, "-")[0], "unknown")
		name := firstNonEmptyString(voice.LangName, languageName(lang), lang)
		group := byLang[lang]
		if group.Code == "" {
			group = TTSLanguageGroup{Code: lang, Name: name}
		}
		group.Voices = append(group.Voices, voice)
		byLang[lang] = group
	}
	languages := make([]TTSLanguageGroup, 0, len(byLang))
	for _, group := range byLang {
		languages = append(languages, group)
	}
	return TTSVoicesResponse{Voices: voices, Languages: languages, ByLang: byLang}
}

func languageName(code string) string {
	names := map[string]string{"en": "English", "id": "Indonesian", "vi": "Vietnamese", "zh": "Chinese", "ja": "Japanese", "ko": "Korean", "fr": "French", "de": "German", "es": "Spanish"}
	if name := names[strings.ToLower(code)]; name != "" {
		return name
	}
	return code
}

func countryName(code string) string {
	names := map[string]string{"US": "United States", "GB": "United Kingdom", "CN": "China", "VN": "Vietnam", "JP": "Japan", "KR": "South Korea", "FR": "France", "DE": "Germany", "ES": "Spain"}
	if name := names[strings.ToUpper(code)]; name != "" {
		return name
	}
	return code
}

func regexFirst(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}

func xmlEscape(s string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return replacer.Replace(s)
}
