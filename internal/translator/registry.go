package translator

import (
	"context"
	"encoding/json"
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
	// Claude ↔ OpenAI
	r.registerRequestTranslator(FormatClaude, FormatOpenAI, &claudeToOpenAIRequestTranslator{})
	r.registerRequestTranslator(FormatOpenAI, FormatClaude, &openAIToClaudeRequestTranslator{})
	r.registerRequestTranslator(FormatOpenAIResp, FormatOpenAI, &responsesToOpenAIRequestTranslator{})
	r.registerRequestTranslator(FormatOpenAI, FormatOpenAIResp, &openAIToResponsesRequestTranslator{})
	r.registerResponseTranslator(FormatClaude, FormatOpenAI, &claudeToOpenAIResponseTranslator{})
	r.registerResponseTranslator(FormatOpenAI, FormatClaude, &openAIToClaudeResponseTranslator{})
	r.registerResponseTranslator(FormatOpenAI, FormatOpenAIResp, &openAIToResponsesResponseTranslator{})
	r.registerResponseTranslator(FormatOpenAIResp, FormatOpenAI, &responsesToOpenAIResponseTranslator{})

	// OpenAI ↔ Gemini
	r.registerRequestTranslator(FormatOpenAI, FormatGemini, &geminiRequestTranslator{})
	r.registerResponseTranslator(FormatGemini, FormatOpenAI, &geminiResponseTranslator{})

	// OpenAI ↔ Ollama
	r.registerRequestTranslator(FormatOpenAI, FormatOllama, &ollamaRequestTranslator{})
	r.registerResponseTranslator(FormatOllama, FormatOpenAI, &ollamaResponseTranslator{})

	// OpenAI ↔ Cursor
	r.registerRequestTranslator(FormatOpenAI, FormatCursor, &cursorRequestTranslator{})
	r.registerResponseTranslator(FormatCursor, FormatOpenAI, &cursorResponseTranslator{})

	// OpenAI ↔ Kiro
	r.registerRequestTranslator(FormatOpenAI, FormatKiro, &kiroRequestTranslator{})
	r.registerResponseTranslator(FormatKiro, FormatOpenAI, &kiroResponseTranslator{})

	// OpenAI ↔ Vertex (Gemini + post-processing)
	r.registerRequestTranslator(FormatOpenAI, FormatVertex, &vertexRequestTranslator{})
	r.registerResponseTranslator(FormatVertex, FormatOpenAI, &vertexResponseTranslator{})

	// Antigravity → OpenAI
	r.registerRequestTranslator(FormatAntigravity, FormatOpenAI, &antigravityRequestTranslator{})
	r.registerResponseTranslator(FormatAntigravity, FormatOpenAI, &antigravityResponseTranslator{})
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

func (r *Registry) TranslateRequestJSON(ctx context.Context, source, target string, body json.RawMessage) (json.RawMessage, error) {
	translator, err := r.GetRequestTranslator(source, target)
	if err != nil {
		return nil, err
	}
	if jsonTranslator, ok := translator.(JSONRequestTranslator); ok {
		return jsonTranslator.TranslateRequestJSON(ctx, source, target, body)
	}
	var object map[string]interface{}
	if err := json.Unmarshal(body, &object); err != nil {
		return nil, err
	}
	translated, err := translator.TranslateRequest(ctx, source, target, object)
	if err != nil {
		return nil, err
	}
	return json.Marshal(translated)
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

func (r *Registry) TranslateResponseJSON(ctx context.Context, source, target string, body json.RawMessage) (json.RawMessage, error) {
	translator, err := r.GetResponseTranslator(source, target)
	if err != nil {
		return nil, err
	}
	if jsonTranslator, ok := translator.(JSONResponseTranslator); ok {
		return jsonTranslator.TranslateResponseJSON(ctx, source, target, body)
	}
	translated, err := translator.TranslateResponse(ctx, source, target, body)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(translated), nil
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
