// Package policy provides a request routing policy engine for 9router.
// Policies are evaluated in order; the first matching policy wins.
package policy

import (
	"strings"

	"github.com/edodoyokz/ai-go-router/internal/providers"
)

// Action defines what to do when a policy matches.
type Action string

const (
	ActionAllow   Action = "allow"
	ActionDeny    Action = "deny"
	ActionReroute Action = "reroute" // change model/provider
	ActionTag     Action = "tag"     // annotate request for logging
)

// Policy defines a single routing policy rule.
type Policy struct {
	Name string `yaml:"name" json:"name"`

	// Match criteria — all non-empty fields must match.
	MatchModel    string `yaml:"match_model,omitempty" json:"match_model,omitempty"`       // exact or prefix match
	MatchProvider string `yaml:"match_provider,omitempty" json:"match_provider,omitempty"` // exact match
	MatchAPIKey   string `yaml:"match_api_key,omitempty" json:"match_api_key,omitempty"`   // exact match

	// Action to take.
	Action       Action `yaml:"action" json:"action"`
	RerouteModel string `yaml:"reroute_model,omitempty" json:"reroute_model,omitempty"` // used with ActionReroute
	DenyMessage  string `yaml:"deny_message,omitempty" json:"deny_message,omitempty"`   // used with ActionDeny
	Tag          string `yaml:"tag,omitempty" json:"tag,omitempty"`                     // used with ActionTag
}

// Request carries the policy evaluation context.
type Request struct {
	Model    string
	Provider string
	APIKey   string
}

// Result holds the outcome of policy evaluation.
type Result struct {
	Matched bool
	Policy  *Policy
	Action  Action
	// FinalModel is set when Action == ActionReroute.
	FinalModel string
	// DenyMessage is set when Action == ActionDeny.
	DenyMessage string
	// Tag is set when Action == ActionTag.
	Tag string
}

// Engine evaluates request routing policies.
type Engine struct {
	policies []Policy
}

// NewEngine creates a policy engine from the given policy list.
func NewEngine(policies []Policy) *Engine {
	return &Engine{policies: policies}
}

// Evaluate checks the request against all policies and returns the first match.
// If no policy matches, it returns Result{Matched: false, Action: ActionAllow}.
func (e *Engine) Evaluate(req Request) Result {
	for i := range e.policies {
		p := &e.policies[i]
		if !matches(p, req) {
			continue
		}

		result := Result{
			Matched: true,
			Policy:  p,
			Action:  p.Action,
		}

		switch p.Action {
		case ActionReroute:
			result.FinalModel = p.RerouteModel
		case ActionDeny:
			result.DenyMessage = p.DenyMessage
			if result.DenyMessage == "" {
				result.DenyMessage = "request denied by policy: " + p.Name
			}
		case ActionTag:
			result.Tag = p.Tag
		}

		return result
	}

	return Result{Matched: false, Action: ActionAllow}
}

// ApplyToRequest applies a reroute result to the chat request, modifying the model.
func ApplyToRequest(req *providers.ChatRequest, result Result) {
	if result.Action == ActionReroute && result.FinalModel != "" {
		req.Model = result.FinalModel
	}
}

func matches(p *Policy, req Request) bool {
	if p.MatchModel != "" {
		if !strings.HasPrefix(req.Model, p.MatchModel) && req.Model != p.MatchModel {
			return false
		}
	}
	if p.MatchProvider != "" && !strings.EqualFold(req.Provider, p.MatchProvider) {
		return false
	}
	if p.MatchAPIKey != "" && req.APIKey != p.MatchAPIKey {
		return false
	}
	return true
}
