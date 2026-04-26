package translator

import (
	"context"
	"fmt"
)

// Registry manages format translators
type Registry struct {
	requestTranslators  map[string]map[string]RequestTranslator  // source -> target -> translator
	responseTranslators map[string]map[string]ResponseTranslator // source -> target -> translator
}

// NewRegistry creates a new translator registry
func NewRegistry() *Registry {
	r := &Registry{
		requestTranslators:  make(map[string]map[string]RequestTranslator),
		responseTranslators: make(map[string]map[string]ResponseTranslator),
	}
	
	// Register built-in translators
	r.registerBuiltInTranslators()
	
	return r
}

// registerBuiltInTranslators registers all built-in format translators
func (r *Registry) registerBuiltInTranslators() {
	// Request translators
	claudeToOpenAIReq := &claudeToOpenAIRequestTranslator{}
	openAIToClaudeReq := &openAIToClaudeRequestTranslator{}
	
	r.registerRequestTranslator(FormatClaude, FormatOpenAI, claudeToOpenAIReq)
	r.registerRequestTranslator(FormatOpenAI, FormatClaude, openAIToClaudeReq)
	
	// Response translators
	claudeToOpenAIResp := &claudeToOpenAIResponseTranslator{}
	openAIToClaudeResp := &openAIToClaudeResponseTranslator{}
	
	r.registerResponseTranslator(FormatClaude, FormatOpenAI, claudeToOpenAIResp)
	r.registerResponseTranslator(FormatOpenAI, FormatClaude, openAIToClaudeResp)
}

// registerRequestTranslator registers a request translator for a source->target pair
func (r *Registry) registerRequestTranslator(source, target string, translator RequestTranslator) {
	if r.requestTranslators[source] == nil {
		r.requestTranslators[source] = make(map[string]RequestTranslator)
	}
	r.requestTranslators[source][target] = translator
}

// registerResponseTranslator registers a response translator for a source->target pair
func (r *Registry) registerResponseTranslator(source, target string, translator ResponseTranslator) {
	if r.responseTranslators[source] == nil {
		r.responseTranslators[source] = make(map[string]ResponseTranslator)
	}
	r.responseTranslators[source][target] = translator
}

// GetRequestTranslator returns a request translator for the given source and target formats
func (r *Registry) GetRequestTranslator(source, target string) (RequestTranslator, error) {
	if source == target {
		return &passthroughRequestTranslator{}, nil
	}
	
	if r.requestTranslators[source] == nil {
		return nil, fmt.Errorf("no request translators registered for source format: %s", source)
	}
	
	translator, ok := r.requestTranslators[source][target]
	if !ok {
		return nil, fmt.Errorf("no request translator for %s -> %s", source, target)
	}
	
	return translator, nil
}

// GetResponseTranslator returns a response translator for the given source and target formats
func (r *Registry) GetResponseTranslator(source, target string) (ResponseTranslator, error) {
	if source == target {
		return &passthroughResponseTranslator{}, nil
	}
	
	if r.responseTranslators[source] == nil {
		return nil, fmt.Errorf("no response translators registered for source format: %s", source)
	}
	
	translator, ok := r.responseTranslators[source][target]
	if !ok {
		return nil, fmt.Errorf("no response translator for %s -> %s", source, target)
	}
	
	return translator, nil
}

// passthroughRequestTranslator returns the request body unchanged
type passthroughRequestTranslator struct{}

func (t *passthroughRequestTranslator) TranslateRequest(ctx context.Context, sourceFormat, targetFormat string, body map[string]interface{}) (map[string]interface{}, error) {
	return body, nil
}

// passthroughResponseTranslator returns the response body unchanged
type passthroughResponseTranslator struct{}

func (t *passthroughResponseTranslator) TranslateResponse(ctx context.Context, sourceFormat, targetFormat string, body []byte) ([]byte, error) {
	return body, nil
}
