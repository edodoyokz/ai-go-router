package endpoints

import "testing"

func TestBuildOpenAI(t *testing.T) {
	if got := BuildOpenAI("https://api.openai.com/v1", "/chat/completions"); got != "https://api.openai.com/v1/chat/completions" {
		t.Fatalf("unexpected endpoint: %s", got)
	}
	if got := BuildOpenAI("https://api.openai.com/v1/chat/completions", "/chat/completions"); got != "https://api.openai.com/v1/chat/completions" {
		t.Fatalf("unexpected full endpoint: %s", got)
	}
}

func TestBuildAnthropicMessages(t *testing.T) {
	cases := map[string]string{
		"https://api.anthropic.com":             "https://api.anthropic.com/v1/messages",
		"https://api.anthropic.com/v1":          "https://api.anthropic.com/v1/messages",
		"https://api.anthropic.com/v1/messages": "https://api.anthropic.com/v1/messages",
	}
	for input, expected := range cases {
		if got := BuildAnthropicMessages(input); got != expected {
			t.Fatalf("%s => %s (want %s)", input, got, expected)
		}
	}
}
