package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

type contextKey string

const AccountContextKey contextKey = "account"

type ChatRequest struct {
	Model             string                     `json:"model"`
	Messages          []ChatMessage              `json:"messages,omitempty"`
	Input             []ResponseInputItem        `json:"input,omitempty"`
	Stream            bool                       `json:"stream,omitempty"`
	Temperature       *float64                   `json:"temperature,omitempty"`
	TopP              *float64                   `json:"top_p,omitempty"`
	MaxTokens         *int                       `json:"max_tokens,omitempty"`
	Stop              []string                   `json:"stop,omitempty"`
	Tools             []Tool                     `json:"tools,omitempty"`
	ToolChoice        json.RawMessage            `json:"tool_choice,omitempty"`
	ResponseFormat    *ResponseFormat            `json:"response_format,omitempty"`
	Reasoning         *ReasoningParams           `json:"reasoning,omitempty"`
	Thinking          *ThinkingParams            `json:"thinking,omitempty"`
	Metadata          map[string]json.RawMessage `json:"metadata,omitempty"`
	Modalities        []string                   `json:"modalities,omitempty"`
	Audio             json.RawMessage            `json:"audio,omitempty"`
	Prediction        json.RawMessage            `json:"prediction,omitempty"`
	Store             *bool                      `json:"store,omitempty"`
	ParallelToolCalls *bool                      `json:"parallel_tool_calls,omitempty"`
	NativePassthrough bool                       `json:"-"`
	Extra             map[string]json.RawMessage `json:"-"`
}

func (r *ChatRequest) UnmarshalJSON(data []byte) error {
	type chatRequest ChatRequest
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var decoded chatRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	deleteKnownChatRequestFields(raw)
	*r = ChatRequest(decoded)
	if len(raw) > 0 {
		r.Extra = raw
	}
	return nil
}

func (r ChatRequest) MarshalJSON() ([]byte, error) {
	type chatRequest ChatRequest
	body, err := json.Marshal(chatRequest(r))
	if err != nil {
		return nil, err
	}
	if len(r.Extra) == 0 {
		return body, nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	for key, value := range r.Extra {
		if _, exists := raw[key]; !exists {
			raw[key] = value
		}
	}
	return json.Marshal(raw)
}

func deleteKnownChatRequestFields(raw map[string]json.RawMessage) {
	for _, key := range []string{"model", "messages", "input", "stream", "temperature", "top_p", "max_tokens", "stop", "tools", "tool_choice", "response_format", "reasoning", "thinking", "metadata", "modalities", "audio", "prediction", "store", "parallel_tool_calls"} {
		delete(raw, key)
	}
}

// ThinkingParams carries extended reasoning configuration for providers that support it.
type ThinkingParams struct {
	Enabled          bool `json:"enabled"`
	MaxTokens        int  `json:"budget_tokens,omitempty"`
	IncludeReasoning bool `json:"include_reasoning,omitempty"`
}

type ReasoningParams struct {
	Effort           string `json:"effort,omitempty"`
	MaxTokens        int    `json:"max_tokens,omitempty"`
	IncludeReasoning bool   `json:"include_reasoning,omitempty"`
}

type ChatMessage struct {
	Role             string                     `json:"role"`
	Content          any                        `json:"content,omitempty"`
	Name             string                     `json:"name,omitempty"`
	ToolCallID       string                     `json:"tool_call_id,omitempty"`
	ToolCalls        []ToolCall                 `json:"tool_calls,omitempty"`
	FunctionCall     *FunctionCall              `json:"function_call,omitempty"`
	Reasoning        json.RawMessage            `json:"reasoning,omitempty"`
	Thinking         json.RawMessage            `json:"thinking,omitempty"`
	ReasoningContent string                     `json:"reasoning_content,omitempty"`
	CacheControl     json.RawMessage            `json:"cache_control,omitempty"`
	Extra            map[string]json.RawMessage `json:"-"`
}

func (m *ChatMessage) UnmarshalJSON(data []byte) error {
	type chatMessage ChatMessage
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var decoded chatMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	for _, key := range []string{"role", "content", "name", "tool_call_id", "tool_calls", "function_call", "reasoning", "thinking", "reasoning_content", "cache_control"} {
		delete(raw, key)
	}
	*m = ChatMessage(decoded)
	if len(raw) > 0 {
		m.Extra = raw
	}
	return nil
}

func (m ChatMessage) MarshalJSON() ([]byte, error) {
	type chatMessage ChatMessage
	body, err := json.Marshal(chatMessage(m))
	if err != nil {
		return nil, err
	}
	if len(m.Extra) == 0 {
		return body, nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	for key, value := range m.Extra {
		if _, exists := raw[key]; !exists {
			raw[key] = value
		}
	}
	return json.Marshal(raw)
}

type ContentPart struct {
	Type         string          `json:"type"`
	Text         string          `json:"text,omitempty"`
	ImageURL     json.RawMessage `json:"image_url,omitempty"`
	Source       json.RawMessage `json:"source,omitempty"`
	ID           string          `json:"id,omitempty"`
	Name         string          `json:"name,omitempty"`
	Input        json.RawMessage `json:"input,omitempty"`
	Content      json.RawMessage `json:"content,omitempty"`
	CacheControl json.RawMessage `json:"cache_control,omitempty"`
	Signature    string          `json:"signature,omitempty"`
	Thinking     string          `json:"thinking,omitempty"`
	Data         string          `json:"data,omitempty"`
	MediaType    string          `json:"media_type,omitempty"`
}

type Tool struct {
	Type     string          `json:"type"`
	Function json.RawMessage `json:"function,omitempty"`
	Extra    json.RawMessage `json:"-"`
}

type ToolCall struct {
	ID       string        `json:"id,omitempty"`
	Type     string        `json:"type"`
	Function *FunctionCall `json:"function,omitempty"`
}

type FunctionCall struct {
	Name      string          `json:"name,omitempty"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type ResponseFormat struct {
	Type       string          `json:"type"`
	JSONSchema json.RawMessage `json:"json_schema,omitempty"`
}

type ResponseInputItem struct {
	Type    string                     `json:"type,omitempty"`
	Role    string                     `json:"role,omitempty"`
	Content any                        `json:"content,omitempty"`
	ID      string                     `json:"id,omitempty"`
	CallID  string                     `json:"call_id,omitempty"`
	Name    string                     `json:"name,omitempty"`
	Extra   map[string]json.RawMessage `json:"-"`
}

type ChatResponse struct {
	ID                string       `json:"id"`
	Object            string       `json:"object"`
	Created           int64        `json:"created"`
	Model             string       `json:"model"`
	Choices           []ChatChoice `json:"choices"`
	Usage             *Usage       `json:"usage,omitempty"`
	SystemFingerprint string       `json:"system_fingerprint,omitempty"`
	ServiceTier       string       `json:"service_tier,omitempty"`
}

type ChatChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason,omitempty"`
}

type Usage struct {
	PromptTokens             int                      `json:"prompt_tokens"`
	CompletionTokens         int                      `json:"completion_tokens"`
	TotalTokens              int                      `json:"total_tokens"`
	PromptTokensDetails      *PromptTokensDetails     `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails  *CompletionTokensDetails `json:"completion_tokens_details,omitempty"`
	InputTokens              int                      `json:"input_tokens,omitempty"`
	OutputTokens             int                      `json:"output_tokens,omitempty"`
	CacheCreationInputTokens int                      `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int                      `json:"cache_read_input_tokens,omitempty"`
}

type PromptTokensDetails struct {
	CachedTokens int `json:"cached_tokens,omitempty"`
	AudioTokens  int `json:"audio_tokens,omitempty"`
}

type CompletionTokensDetails struct {
	ReasoningTokens          int `json:"reasoning_tokens,omitempty"`
	AudioTokens              int `json:"audio_tokens,omitempty"`
	AcceptedPredictionTokens int `json:"accepted_prediction_tokens,omitempty"`
	RejectedPredictionTokens int `json:"rejected_prediction_tokens,omitempty"`
}

type EmbeddingsRequest struct {
	Input          string `json:"input"`
	Model          string `json:"model"`
	EncodingFormat string `json:"encoding_format,omitempty"`
}

type EmbeddingsResponse struct {
	Object string         `json:"object"`
	Data   []Embedding    `json:"data"`
	Model  string         `json:"model"`
	Usage  EmbeddingUsage `json:"usage"`
}

type Embedding struct {
	Object    string    `json:"object"`
	Embedding []float64 `json:"embedding"`
	Index     int       `json:"index"`
}

type EmbeddingUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type AudioSpeechRequest struct {
	Model          string  `json:"model"`
	Input          string  `json:"input"`
	Voice          string  `json:"voice"`
	ResponseFormat string  `json:"response_format,omitempty"`
	Speed          float64 `json:"speed,omitempty"`
}

type AudioSpeechResponse struct {
	ContentType string `json:"content_type"`
	Data        []byte `json:"data"`
}

type ImagesGenerationsRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	N      int    `json:"n,omitempty"`
	Size   string `json:"size,omitempty"`
}

type ImagesGenerationsResponse struct {
	Created int64           `json:"created"`
	Data    []ImageResponse `json:"data"`
}

type ImageResponse struct {
	URL string `json:"url"`
}

type AudioTranscriptionRequest struct {
	Model       string `json:"model"`
	AudioURL    string `json:"audio_url,omitempty"`
	AudioBase64 string `json:"audio_base64,omitempty"`
	Language    string `json:"language,omitempty"`
	Prompt      string `json:"prompt,omitempty"`
}

type AudioTranscriptionResponse struct {
	Text     string         `json:"text"`
	Language string         `json:"language,omitempty"`
	Duration float64        `json:"duration,omitempty"`
	Raw      map[string]any `json:"raw,omitempty"`
}

type WebSearchRequest struct {
	Query       string         `json:"query"`
	MaxResults  int            `json:"max_results,omitempty"`
	SearchDepth string         `json:"search_depth,omitempty"`
	IncludeRaw  bool           `json:"include_raw_content,omitempty"`
	Extra       map[string]any `json:"extra,omitempty"`
}

type WebSearchResult struct {
	Title   string  `json:"title,omitempty"`
	URL     string  `json:"url,omitempty"`
	Content string  `json:"content,omitempty"`
	Score   float64 `json:"score,omitempty"`
}

type WebSearchResponse struct {
	Query   string            `json:"query"`
	Results []WebSearchResult `json:"results"`
	Answer  string            `json:"answer,omitempty"`
	Raw     map[string]any    `json:"raw,omitempty"`
}

type WebFetchRequest struct {
	URL     string         `json:"url"`
	Formats []string       `json:"formats,omitempty"`
	Extra   map[string]any `json:"extra,omitempty"`
}

type WebFetchResponse struct {
	URL      string         `json:"url"`
	Title    string         `json:"title,omitempty"`
	Content  string         `json:"content,omitempty"`
	Markdown string         `json:"markdown,omitempty"`
	HTML     string         `json:"html,omitempty"`
	Raw      map[string]any `json:"raw,omitempty"`
}

type AudioTranscriber interface {
	TranscribeAudio(ctx context.Context, request AudioTranscriptionRequest, model string) (AudioTranscriptionResponse, error)
}

type WebSearcher interface {
	WebSearch(ctx context.Context, request WebSearchRequest) (WebSearchResponse, error)
}

type WebFetcher interface {
	WebFetch(ctx context.Context, request WebFetchRequest) (WebFetchResponse, error)
}

type Adapter interface {
	Name() string
	ChatCompletion(ctx context.Context, request ChatRequest, model string) (ChatResponse, error)
	StreamChatCompletion(ctx context.Context, request ChatRequest, model string) (<-chan ChatChunk, error)
	GetUsage(ctx context.Context) (map[string]interface{}, error)
	Embeddings(ctx context.Context, request EmbeddingsRequest, model string) (EmbeddingsResponse, error)
	AudioSpeech(ctx context.Context, request AudioSpeechRequest, model string) (AudioSpeechResponse, error)
	ImagesGenerations(ctx context.Context, request ImagesGenerationsRequest, model string) (ImagesGenerationsResponse, error)
}

type AccountAwareAdapter interface {
	Adapter
	AccountNames() []string
}

type ChatChunk struct {
	ID                string        `json:"id"`
	Object            string        `json:"object"`
	Created           int64         `json:"created"`
	Model             string        `json:"model"`
	Choices           []ChunkChoice `json:"choices"`
	Usage             *Usage        `json:"usage,omitempty"`
	SystemFingerprint string        `json:"system_fingerprint,omitempty"`
	ServiceTier       string        `json:"service_tier,omitempty"`
}

type ChunkChoice struct {
	Index        int        `json:"index"`
	Delta        ChunkDelta `json:"delta"`
	FinishReason *string    `json:"finish_reason,omitempty"`
}

type ChunkDelta struct {
	Content          any             `json:"content,omitempty"`
	Role             string          `json:"role,omitempty"`
	ToolCalls        []ToolCallDelta `json:"tool_calls,omitempty"`
	Reasoning        string          `json:"reasoning,omitempty"`
	Thinking         string          `json:"thinking,omitempty"`
	ReasoningContent string          `json:"reasoning_content,omitempty"`
	FunctionCall     *FunctionCall   `json:"function_call,omitempty"`
	Refusal          string          `json:"refusal,omitempty"`
}

type ToolCallDelta struct {
	Index    int           `json:"index,omitempty"`
	ID       string        `json:"id,omitempty"`
	Type     string        `json:"type,omitempty"`
	Function *FunctionCall `json:"function,omitempty"`
}

type Registry struct {
	mu        sync.RWMutex
	providers map[string]Adapter
}

func NewRegistry(adapters ...Adapter) *Registry {
	items := make(map[string]Adapter, len(adapters))
	for _, adapter := range adapters {
		items[adapter.Name()] = adapter
	}
	return &Registry{providers: items}
}

func (r *Registry) Get(name string) (Adapter, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	adapter, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider not registered: %s", name)
	}
	return adapter, nil
}

func (r *Registry) ReplaceAll(adapters ...Adapter) {
	r.mu.Lock()
	defer r.mu.Unlock()

	items := make(map[string]Adapter, len(adapters))
	for _, adapter := range adapters {
		items[adapter.Name()] = adapter
	}
	r.providers = items
}

type SSEEvent struct {
	ID    string
	Event string
	Data  string
}

// sseScanner keeps the old line-oriented adapter API while using an event-aware SSE parser.
type sseScanner struct {
	decoder *SSEDecoder
	event   SSEEvent
	err     error
}

func newSSEScanner(r io.Reader) *sseScanner {
	return &sseScanner{decoder: NewSSEDecoder(r)}
}

func (s *sseScanner) Scan() bool {
	event, err := s.decoder.Next()
	if err != nil {
		if err != io.EOF {
			s.err = err
		}
		return false
	}
	s.event = event
	return true
}

func (s *sseScanner) Text() string {
	if s.event.Data == "" {
		return ""
	}
	return "data: " + s.event.Data
}

func (s *sseScanner) Err() error {
	return s.err
}

type SSEDecoder struct {
	r       io.Reader
	lastID  string
	pending []byte
}

func NewSSEDecoder(r io.Reader) *SSEDecoder {
	return &SSEDecoder{r: r}
}

type SSEWriter struct {
	w       io.Writer
	flusher http.Flusher
}

func NewSSEWriter(w io.Writer) *SSEWriter {
	writer := &SSEWriter{w: w}
	if flusher, ok := w.(http.Flusher); ok {
		writer.flusher = flusher
	}
	return writer
}

func (w *SSEWriter) WriteEvent(event SSEEvent) error {
	if event.ID != "" {
		if _, err := fmt.Fprintf(w.w, "id: %s\n", event.ID); err != nil {
			return err
		}
	}
	if event.Event != "" {
		if _, err := fmt.Fprintf(w.w, "event: %s\n", event.Event); err != nil {
			return err
		}
	}
	for _, line := range strings.Split(event.Data, "\n") {
		if _, err := fmt.Fprintf(w.w, "data: %s\n", line); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(w.w, "\n"); err != nil {
		return err
	}
	if w.flusher != nil {
		w.flusher.Flush()
	}
	return nil
}

func (w *SSEWriter) WriteData(data string) error {
	return w.WriteEvent(SSEEvent{Data: data})
}

func (w *SSEWriter) WriteDone() error {
	return w.WriteData("[DONE]")
}

func AccumulateSSEData(r io.Reader) ([]json.RawMessage, error) {
	decoder := NewSSEDecoder(r)
	var items []json.RawMessage
	for {
		event, err := decoder.Next()
		if err != nil {
			if err == io.EOF {
				return items, nil
			}
			return nil, err
		}
		data := strings.TrimSpace(event.Data)
		if data == "" || data == "[DONE]" {
			if data == "[DONE]" {
				return items, nil
			}
			continue
		}
		items = append(items, json.RawMessage(data))
	}
}

func (d *SSEDecoder) Next() (SSEEvent, error) {
	var event SSEEvent
	var data []string

	for {
		line, err := d.readLine()
		if err != nil {
			if err == io.EOF && len(data) > 0 {
				event.ID = d.lastID
				event.Data = strings.Join(data, "\n")
				return event, nil
			}
			return SSEEvent{}, err
		}

		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if line == "" {
			if len(data) == 0 && event.Event == "" && event.ID == "" {
				continue
			}
			if event.ID == "" {
				event.ID = d.lastID
			} else {
				d.lastID = event.ID
			}
			event.Data = strings.Join(data, "\n")
			return event, nil
		}
		if strings.HasPrefix(line, ":") {
			continue
		}

		field, value, ok := strings.Cut(line, ":")
		if !ok {
			field = line
			value = ""
		} else if strings.HasPrefix(value, " ") {
			value = value[1:]
		}

		switch field {
		case "data":
			data = append(data, value)
		case "event":
			event.Event = value
		case "id":
			event.ID = value
		}
	}
}

func (d *SSEDecoder) readLine() (string, error) {
	var line []byte
	for {
		if i := bytes.IndexByte(d.pending, '\n'); i >= 0 {
			line = append(line, d.pending[:i+1]...)
			d.pending = d.pending[i+1:]
			return string(line), nil
		}
		if len(d.pending) > 0 {
			line = append(line, d.pending...)
			d.pending = nil
		}

		buf := make([]byte, 4096)
		n, err := d.r.Read(buf)
		if n > 0 {
			d.pending = append(d.pending, buf[:n]...)
			continue
		}
		if err != nil {
			if err == io.EOF && len(line) > 0 {
				return string(line), nil
			}
			return "", err
		}
	}
}
