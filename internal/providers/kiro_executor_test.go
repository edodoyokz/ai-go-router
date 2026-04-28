package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/edodoyokz/ai-go-router/internal/config"
)

func TestParseKiroEventFrame(t *testing.T) {
	frame := buildKiroFrame(":event-type", "assistantResponseEvent", map[string]any{"content": "hello"})
	headers, payload := parseKiroEventFrame(frame, int(binary.BigEndian.Uint32(frame[4:8])))
	if headers[":event-type"] != "assistantResponseEvent" {
		t.Fatalf("headers=%#v", headers)
	}
	if payload["content"] != "hello" {
		t.Fatalf("payload=%#v", payload)
	}
}

func TestKiroReadEventStream(t *testing.T) {
	body := bytes.NewBuffer(nil)
	body.Write(buildKiroFrame(":event-type", "assistantResponseEvent", map[string]any{"content": "Hello"}))
	body.Write(buildKiroFrame(":event-type", "toolUseEvent", map[string]any{"toolUseId": "call_1", "name": "read_file", "input": map[string]any{"path": "main.go"}}))
	body.Write(buildKiroFrame(":event-type", "metricsEvent", map[string]any{"inputTokens": 12, "outputTokens": 8}))
	body.Write(buildKiroFrame(":event-type", "meteringEvent", map[string]any{}))
	body.Write(buildKiroFrame(":event-type", "contextUsageEvent", map[string]any{"contextUsagePercentage": 4}))

	exec, err := NewKiroExecutor(testProviderConfig("kiro", "https://example.invalid", "kiro-token"), testErrorConfig())
	if err != nil {
		t.Fatalf("NewKiroExecutor error: %v", err)
	}
	kiroExec := exec.(*KiroExecutor)
	ch := make(chan ChatChunk, 32)
	kiroExec.readEventStream(io.NopCloser(bytes.NewReader(body.Bytes())), "kiro-model", ch)

	var sawText, sawTool, sawUsage bool
	for chunk := range ch {
		if len(chunk.Choices) > 0 {
			delta := chunk.Choices[0].Delta
			if text, ok := delta.Content.(string); ok && text == "Hello" {
				sawText = true
			}
			if len(delta.ToolCalls) > 0 && delta.ToolCalls[0].Function != nil && delta.ToolCalls[0].Function.Name == "read_file" {
				sawTool = true
			}
		}
		if chunk.Usage != nil && chunk.Usage.PromptTokens == 12 && chunk.Usage.CompletionTokens == 8 {
			sawUsage = true
		}
	}
	if !sawText || !sawTool || !sawUsage {
		t.Fatalf("expected text/tool/usage chunks sawText=%v sawTool=%v sawUsage=%v", sawText, sawTool, sawUsage)
	}
}

func TestReadKiroEventFrame(t *testing.T) {
	frame := buildKiroFrame(":event-type", "messageStopEvent", map[string]any{})
	parsed, err := readKiroEventFrame(bufio.NewReader(bytes.NewReader(frame)))
	if err != nil {
		t.Fatalf("readKiroEventFrame error: %v", err)
	}
	if parsed.Headers[":event-type"] != "messageStopEvent" {
		t.Fatalf("parsed=%#v", parsed)
	}
}

func buildKiroFrame(headerName, headerValue string, payload map[string]any) []byte {
	payloadBytes, _ := json.Marshal(payload)
	headerNameBytes := []byte(headerName)
	headerValueBytes := []byte(headerValue)
	headers := bytes.NewBuffer(nil)
	headers.WriteByte(byte(len(headerNameBytes)))
	headers.Write(headerNameBytes)
	headers.WriteByte(7)
	_ = binary.Write(headers, binary.BigEndian, uint16(len(headerValueBytes)))
	headers.Write(headerValueBytes)
	headersBytes := headers.Bytes()
	totalLength := uint32(12 + len(headersBytes) + len(payloadBytes) + 4)
	frame := bytes.NewBuffer(nil)
	_ = binary.Write(frame, binary.BigEndian, totalLength)
	_ = binary.Write(frame, binary.BigEndian, uint32(len(headersBytes)))
	_ = binary.Write(frame, binary.BigEndian, uint32(0))
	frame.Write(headersBytes)
	frame.Write(payloadBytes)
	_ = binary.Write(frame, binary.BigEndian, uint32(0))
	return frame.Bytes()
}

func TestKiroExecutor_RefreshCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["refreshToken"] != "kiro-refresh" {
			t.Fatalf("body=%#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accessToken":"kiro-access-2","refreshToken":"kiro-refresh-2","profileArn":"arn:aws:profile/test","expiresIn":1800}`))
	}))
	defer server.Close()
	exec, err := NewKiroExecutor(config.ProviderConfig{
		Name: "kiro",
		Type: "kiro",
		Accounts: []config.AccountConfig{{
			Name:         "kiro-account",
			RefreshToken: "kiro-refresh",
		}},
		ProviderSpecificData: map[string]any{"refresh_url": server.URL},
	}, config.ErrorConfig{})
	if err != nil {
		t.Fatalf("NewKiroExecutor error: %v", err)
	}
	result, err := exec.(*KiroExecutor).RefreshCredentials(context.Background())
	if err != nil {
		t.Fatalf("RefreshCredentials error: %v", err)
	}
	if result.AccessToken != "kiro-access-2" || result.RefreshToken != "kiro-refresh-2" || result.ProfileARN == "" {
		t.Fatalf("result=%#v", result)
	}
	if time.Until(result.ExpiresAt) < 1700*time.Second {
		t.Fatalf("expiresAt too soon: %s", result.ExpiresAt)
	}
}

func TestKiroExecutor_NeedsRefresh(t *testing.T) {
	soon := time.Now().Add(time.Minute)
	later := time.Now().Add(time.Hour)
	tests := []struct {
		name    string
		account config.AccountConfig
		want    bool
	}{
		{name: "no refresh", account: config.AccountConfig{AccessToken: "access"}, want: false},
		{name: "missing access", account: config.AccountConfig{RefreshToken: "refresh"}, want: true},
		{name: "soon", account: config.AccountConfig{AccessToken: "access", RefreshToken: "refresh", ExpiresAt: &soon}, want: true},
		{name: "fresh", account: config.AccountConfig{AccessToken: "access", RefreshToken: "refresh", ExpiresAt: &later}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exec, err := NewKiroExecutor(config.ProviderConfig{Name: "kiro", Type: "kiro", Accounts: []config.AccountConfig{tt.account}}, config.ErrorConfig{})
			if err != nil {
				t.Fatalf("NewKiroExecutor error: %v", err)
			}
			if got := exec.(*KiroExecutor).NeedsRefresh(5 * time.Minute); got != tt.want {
				t.Fatalf("NeedsRefresh=%v want %v", got, tt.want)
			}
		})
	}
}
