package translator

import (
	"context"
	"encoding/json"
)

// RequestTranslator converts request bodies from one format to another
type RequestTranslator interface {
	// TranslateRequest converts a request from source format to target format
	TranslateRequest(ctx context.Context, sourceFormat, targetFormat string, body map[string]interface{}) (map[string]interface{}, error)
}

// ResponseTranslator converts response bodies from one format to another
type ResponseTranslator interface {
	// TranslateResponse converts a response from source format to target format
	TranslateResponse(ctx context.Context, sourceFormat, targetFormat string, body []byte) ([]byte, error)
}

type JSONRequestTranslator interface {
	TranslateRequestJSON(ctx context.Context, sourceFormat, targetFormat string, body json.RawMessage) (json.RawMessage, error)
}

type JSONResponseTranslator interface {
	TranslateResponseJSON(ctx context.Context, sourceFormat, targetFormat string, body json.RawMessage) (json.RawMessage, error)
}
