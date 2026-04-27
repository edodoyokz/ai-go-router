package policy

import (
	"testing"

	"github.com/edodoyokz/ai-go-router/internal/providers"
)

func TestAllowRule(t *testing.T) {
	eng := NewEngine([]Policy{
		{Name: "allow-all", Action: ActionAllow},
	})
	result := eng.Evaluate(Request{Model: "gpt-4"})
	if result.Action != ActionAllow {
		t.Fatalf("expected allow, got %s", result.Action)
	}
}

func TestDenyRule(t *testing.T) {
	eng := NewEngine([]Policy{
		{Name: "deny-gpt4", MatchModel: "gpt-4", Action: ActionDeny, DenyMessage: "gpt-4 is blocked"},
	})
	result := eng.Evaluate(Request{Model: "gpt-4"})
	if result.Action != ActionDeny {
		t.Fatalf("expected deny, got %s", result.Action)
	}
	if result.DenyMessage != "gpt-4 is blocked" {
		t.Fatalf("unexpected deny message: %s", result.DenyMessage)
	}
}

func TestDenyRuleDefaultMessage(t *testing.T) {
	eng := NewEngine([]Policy{
		{Name: "deny-x", MatchModel: "x", Action: ActionDeny},
	})
	result := eng.Evaluate(Request{Model: "x"})
	if result.DenyMessage == "" {
		t.Fatal("expected default deny message, got empty string")
	}
}

func TestRerouteRule(t *testing.T) {
	eng := NewEngine([]Policy{
		{Name: "reroute", MatchModel: "gpt-4", Action: ActionReroute, RerouteModel: "claude-3-sonnet"},
	})
	result := eng.Evaluate(Request{Model: "gpt-4"})
	if result.Action != ActionReroute {
		t.Fatalf("expected reroute, got %s", result.Action)
	}
	if result.FinalModel != "claude-3-sonnet" {
		t.Fatalf("expected claude-3-sonnet, got %s", result.FinalModel)
	}

	req := &providers.ChatRequest{Model: "gpt-4"}
	ApplyToRequest(req, result)
	if req.Model != "claude-3-sonnet" {
		t.Fatalf("ApplyToRequest did not update model: %s", req.Model)
	}
}

func TestTagRule(t *testing.T) {
	eng := NewEngine([]Policy{
		{Name: "tag-premium", MatchModel: "gpt-4", Action: ActionTag, Tag: "premium"},
	})
	result := eng.Evaluate(Request{Model: "gpt-4"})
	if result.Action != ActionTag {
		t.Fatalf("expected tag, got %s", result.Action)
	}
	if result.Tag != "premium" {
		t.Fatalf("expected tag=premium, got %s", result.Tag)
	}
}

func TestNoMatchReturnsAllow(t *testing.T) {
	eng := NewEngine([]Policy{
		{Name: "deny-gpt4", MatchModel: "gpt-4", Action: ActionDeny},
	})
	result := eng.Evaluate(Request{Model: "claude-3"})
	if result.Matched {
		t.Fatal("expected no match for claude-3")
	}
	if result.Action != ActionAllow {
		t.Fatalf("expected default allow, got %s", result.Action)
	}
}

func TestFirstMatchWins(t *testing.T) {
	eng := NewEngine([]Policy{
		{Name: "deny-first", MatchModel: "gpt-4", Action: ActionDeny, DenyMessage: "first"},
		{Name: "allow-second", MatchModel: "gpt-4", Action: ActionAllow},
	})
	result := eng.Evaluate(Request{Model: "gpt-4"})
	if result.DenyMessage != "first" {
		t.Fatalf("expected first policy to win, got: %s", result.DenyMessage)
	}
}

func TestMatchByAPIKey(t *testing.T) {
	eng := NewEngine([]Policy{
		{Name: "key-deny", MatchAPIKey: "bad-key", Action: ActionDeny, DenyMessage: "bad key"},
	})
	result := eng.Evaluate(Request{Model: "gpt-4", APIKey: "bad-key"})
	if result.Action != ActionDeny {
		t.Fatalf("expected deny for bad-key, got %s", result.Action)
	}

	result2 := eng.Evaluate(Request{Model: "gpt-4", APIKey: "good-key"})
	if result2.Matched {
		t.Fatal("expected no match for good-key")
	}
}
