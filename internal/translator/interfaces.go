package translator

import "context"

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
