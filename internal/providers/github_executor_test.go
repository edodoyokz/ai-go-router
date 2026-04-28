package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/edodoyokz/ai-go-router/internal/config"
)

func TestGitHubExecutor_RefreshOn401Retry(t *testing.T) {
	chatCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/chat/completions":
			chatCalls++
			if chatCalls == 1 {
				if r.Header.Get("Authorization") != "Bearer expired-copilot" {
					t.Fatalf("first auth=%q", r.Header.Get("Authorization"))
				}
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"expired"}`))
				return
			}
			if r.Header.Get("Authorization") != "Bearer fresh-copilot" {
				t.Fatalf("retry auth=%q", r.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(w).Encode(ChatResponse{ID: "ok", Object: "chat.completion", Model: "gpt-test", Choices: []ChatChoice{{Index: 0, Message: ChatMessage{Role: "assistant", Content: "ok"}, FinishReason: "stop"}}})
		case "/oauth/token":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			if r.Form.Get("refresh_token") != "github-refresh" || r.Form.Get("client_id") != "client-test" {
				t.Fatalf("form=%v", r.Form)
			}
			_, _ = w.Write([]byte(`{"access_token":"fresh-github","refresh_token":"github-refresh-2","expires_in":3600}`))
		case "/copilot/token":
			if r.Header.Get("Authorization") == "Bearer stale-github" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"stale"}`))
				return
			}
			if r.Header.Get("Authorization") != "Bearer fresh-github" {
				t.Fatalf("copilot auth=%q", r.Header.Get("Authorization"))
			}
			_, _ = w.Write([]byte(`{"token":"fresh-copilot","expires_at":1893456000}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	exec, err := NewGitHubExecutor(config.ProviderConfig{
		Name:    "github",
		Type:    "github",
		BaseURL: server.URL,
		Accounts: []config.AccountConfig{{
			Name:         "github-account",
			AccessToken:  "stale-github",
			RefreshToken: "github-refresh",
			ProviderSpecificData: map[string]any{
				"copilotToken": "expired-copilot",
			},
		}},
		ProviderSpecificData: map[string]any{
			"token_url":         server.URL + "/oauth/token",
			"copilot_token_url": server.URL + "/copilot/token",
			"client_id":         "client-test",
		},
	}, config.ErrorConfig{})
	if err != nil {
		t.Fatalf("NewGitHubExecutor error: %v", err)
	}
	resp, err := exec.ChatCompletion(context.Background(), ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "hi"}}}, "gpt-test")
	if err != nil {
		t.Fatalf("ChatCompletion error: %v", err)
	}
	if resp.ID != "ok" || chatCalls != 2 {
		t.Fatalf("resp=%#v chatCalls=%d", resp, chatCalls)
	}
	github := exec.(*GitHubExecutor)
	acct := github.cfg.Accounts[0]
	if acct.AccessToken != "fresh-github" || acct.RefreshToken != "github-refresh-2" || acct.ProviderSpecificData["copilotToken"] != "fresh-copilot" {
		t.Fatalf("account=%#v", acct)
	}
	if _, err := time.Parse(time.RFC3339, acct.ProviderSpecificData["copilotTokenExpiresAt"].(string)); err != nil {
		t.Fatalf("invalid copilot expiry: %v", err)
	}
}

func TestGitHubExecutor_StreamRefreshOn401Retry(t *testing.T) {
	chatCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/chat/completions":
			chatCalls++
			if chatCalls == 1 {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"expired"}`))
				return
			}
			if r.Header.Get("Authorization") != "Bearer fresh-copilot" {
				t.Fatalf("retry auth=%q", r.Header.Get("Authorization"))
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"id\":\"chunk-1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-test\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"stream ok\"}}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case "/oauth/token":
			_, _ = w.Write([]byte(`{"access_token":"fresh-github","refresh_token":"github-refresh-2","expires_in":3600}`))
		case "/copilot/token":
			if r.Header.Get("Authorization") == "Bearer stale-github" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"stale"}`))
				return
			}
			_, _ = w.Write([]byte(`{"token":"fresh-copilot","expires_at":1893456000}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	exec, err := NewGitHubExecutor(config.ProviderConfig{
		Name:    "github",
		Type:    "github",
		BaseURL: server.URL,
		Accounts: []config.AccountConfig{{
			Name:         "github-account",
			AccessToken:  "stale-github",
			RefreshToken: "github-refresh",
			ProviderSpecificData: map[string]any{
				"copilotToken": "expired-copilot",
			},
		}},
		ProviderSpecificData: map[string]any{
			"token_url":         server.URL + "/oauth/token",
			"copilot_token_url": server.URL + "/copilot/token",
			"client_id":         "client-test",
		},
	}, config.ErrorConfig{})
	if err != nil {
		t.Fatalf("NewGitHubExecutor error: %v", err)
	}
	ch, err := exec.StreamChatCompletion(context.Background(), ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "hi"}}}, "gpt-test")
	if err != nil {
		t.Fatalf("StreamChatCompletion error: %v", err)
	}
	var text string
	for chunk := range ch {
		if len(chunk.Choices) == 0 {
			continue
		}
		if s, ok := chunk.Choices[0].Delta.Content.(string); ok {
			text += s
		}
	}
	if text != "stream ok" || chatCalls != 2 {
		t.Fatalf("text=%q chatCalls=%d", text, chatCalls)
	}
}
