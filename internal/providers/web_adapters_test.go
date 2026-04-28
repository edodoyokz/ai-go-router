package providers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/edodoyokz/ai-go-router/internal/config"
)

func TestGrokWebAdapterChatCompletionParsesNDJSONAndSkipsMalformedLines(t *testing.T) {
	var gotCookie, gotOrigin, gotModelMode string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie = r.Header.Get("Cookie")
		gotOrigin = r.Header.Get("Origin")
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		gotModelMode, _ = body["modelMode"].(string)
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte("not-json\n"))
		_, _ = w.Write([]byte(`{"result":{"response":{"llmInfo":{"modelHash":"fp-grok"},"token":"hel"}}}` + "\n"))
		_, _ = w.Write([]byte(`{"result":{"response":{"token":"lo"}}}` + "\n"))
	}))
	defer server.Close()

	adapter := NewGrokWebAdapter(config.ProviderConfig{Name: "grok-web", BaseURL: server.URL, APIKey: "grok-token", Enabled: true}, config.ErrorConfig{}, "")
	resp, err := adapter.ChatCompletion(context.Background(), ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "hi"}}}, "grok-4.1-fast")
	if err != nil {
		t.Fatalf("ChatCompletion() error = %v", err)
	}
	if gotCookie != "sso=grok-token" || gotOrigin != "https://grok.com" || gotModelMode != "MODEL_MODE_FAST" {
		t.Fatalf("headers/body cookie=%q origin=%q modelMode=%q", gotCookie, gotOrigin, gotModelMode)
	}
	if resp.Choices[0].Message.Content != "hello" || resp.SystemFingerprint != "fp-grok" {
		t.Fatalf("response=%#v", resp)
	}
}

func TestGrokWebAdapterStreamAndExpiredCookie(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.RawQuery, "expired") {
			http.Error(w, "expired", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"result":{"response":{"token":"a"}}}` + "\n"))
		_, _ = w.Write([]byte("malformed\n"))
		_, _ = w.Write([]byte(`{"result":{"response":{"token":"b"}}}` + "\n"))
	}))
	defer server.Close()

	adapter := NewGrokWebAdapter(config.ProviderConfig{Name: "grok-web", BaseURL: server.URL, APIKey: "grok-token", Enabled: true}, config.ErrorConfig{}, "")
	ch, err := adapter.StreamChatCompletion(context.Background(), ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "hi"}}}, "grok-4")
	if err != nil {
		t.Fatalf("StreamChatCompletion() error = %v", err)
	}
	var content strings.Builder
	for chunk := range ch {
		if v, ok := chunk.Choices[0].Delta.Content.(string); ok {
			content.WriteString(v)
		}
	}
	if content.String() != "ab" {
		t.Fatalf("stream content=%q", content.String())
	}

	expired := NewGrokWebAdapter(config.ProviderConfig{Name: "grok-web", BaseURL: server.URL + "?expired=1", APIKey: "grok-token", Enabled: true}, config.ErrorConfig{}, "")
	_, err = expired.ChatCompletion(context.Background(), ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "hi"}}}, "grok-4")
	if err == nil || !strings.Contains(err.Error(), "SSO cookie may be expired") {
		t.Fatalf("expired error=%v", err)
	}
}

func TestGrokWebAdapterRequiresCookie(t *testing.T) {
	adapter := NewGrokWebAdapter(config.ProviderConfig{Name: "grok-web", BaseURL: "https://example.invalid", Enabled: true}, config.ErrorConfig{}, "")
	_, err := adapter.ChatCompletion(context.Background(), ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "hi"}}}, "grok-4")
	if err == nil || !strings.Contains(err.Error(), "requires a valid sso cookie") {
		t.Fatalf("error=%v", err)
	}
}

func TestPerplexityWebAdapterChatCompletionParsesSSEAndSkipsMalformedEvents(t *testing.T) {
	var gotCookie, gotClient, gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie = r.Header.Get("Cookie")
		gotClient = r.Header.Get("X-App-ApiClient")
		var body struct {
			Query string `json:"query_str"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		gotQuery = body.Query
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: not-json\n\n"))
		_, _ = w.Write([]byte(`data: {"blocks":[{"intended_usage":"pro_search_steps","plan_block":{"steps":[{"step_type":"SEARCH_WEB","search_web_content":{"queries":[{"query":"golang"}]}}]}}]}` + "\n\n"))
		_, _ = w.Write([]byte(`data: {"blocks":[{"intended_usage":"markdown","markdown_block":{"chunks":["Hello [1] <response>world</response>"],"progress":"DONE"}}],"final":true}` + "\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	adapter := NewPerplexityWebAdapter(config.ProviderConfig{Name: "perplexity-web", BaseURL: server.URL, APIKey: "pplx-cookie", Enabled: true}, config.ErrorConfig{}, "")
	resp, err := adapter.ChatCompletion(context.Background(), ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "what?"}}}, "pplx-auto")
	if err != nil {
		t.Fatalf("ChatCompletion() error = %v", err)
	}
	if gotCookie != "__Secure-next-auth.session-token=pplx-cookie" || gotClient != "default" || !strings.Contains(gotQuery, "what?") {
		t.Fatalf("headers/query cookie=%q client=%q query=%q", gotCookie, gotClient, gotQuery)
	}
	if resp.Choices[0].Message.Content != "Hello world" || !strings.Contains(resp.Choices[0].Message.ReasoningContent, "Searching: golang") {
		t.Fatalf("response=%#v", resp)
	}
}

func TestPerplexityWebAdapterStreamAndExpiredCookie(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.RawQuery, "expired") {
			http.Error(w, "expired", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"blocks":[{"intended_usage":"markdown","markdown_block":{"chunks":["hi"]}}]}` + "\n\n"))
		_, _ = w.Write([]byte("data: malformed\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	adapter := NewPerplexityWebAdapter(config.ProviderConfig{Name: "perplexity-web", BaseURL: server.URL, APIKey: "pplx-cookie", Enabled: true}, config.ErrorConfig{}, "")
	ch, err := adapter.StreamChatCompletion(context.Background(), ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "hi"}}}, "pplx-auto")
	if err != nil {
		t.Fatalf("StreamChatCompletion() error = %v", err)
	}
	var content strings.Builder
	for chunk := range ch {
		if v, ok := chunk.Choices[0].Delta.Content.(string); ok {
			content.WriteString(v)
		}
	}
	if content.String() != "hi" {
		t.Fatalf("stream content=%q", content.String())
	}

	expired := NewPerplexityWebAdapter(config.ProviderConfig{Name: "perplexity-web", BaseURL: server.URL + "?expired=1", APIKey: "pplx-cookie", Enabled: true}, config.ErrorConfig{}, "")
	_, err = expired.ChatCompletion(context.Background(), ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "hi"}}}, "pplx-auto")
	if err == nil || !strings.Contains(err.Error(), "session cookie may be expired") {
		t.Fatalf("expired error=%v", err)
	}
}

func TestPerplexityWebAdapterRequiresCookie(t *testing.T) {
	adapter := NewPerplexityWebAdapter(config.ProviderConfig{Name: "perplexity-web", BaseURL: "https://example.invalid", Enabled: true}, config.ErrorConfig{}, "")
	_, err := adapter.ChatCompletion(context.Background(), ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "hi"}}}, "pplx-auto")
	if err == nil {
		t.Fatal("expected missing cookie error")
	}
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) || !strings.Contains(providerErr.Message, "__Secure-next-auth.session-token") {
		t.Fatalf("error=%v", err)
	}
}
