package translator

import (
	"context"
)

const defaultVertexThinkingSignature = "EqoBCkgIARAAGAEiQNSIMiCeGhcN/2SrjZ0RYhaMKpFWPQpyvkE4gR26IZ4qfP1g0J9XiLEjDWN7Xs6x52FHn7i9J/BVvHPT6baDFsIQABgBIiDq5A2fVMnT/aDRe0J5Jt38PwqFhAQiD4N9+FMNT0J1sQ=="

// vertexRequestTranslator converts OpenAI format to Vertex AI format.
// Vertex AI uses Gemini format with two extra post-processing steps:
//  1. Replace synthetic thoughtSignature with Vertex-native signature.
//  2. Strip `id` from functionCall and functionResponse (Vertex rejects these).
type vertexRequestTranslator struct {
	gemini geminiRequestTranslator
}

func (t *vertexRequestTranslator) TranslateRequest(ctx context.Context, sourceFormat, targetFormat string, body map[string]interface{}) (map[string]interface{}, error) {
	result, err := t.gemini.TranslateRequest(ctx, FormatOpenAI, FormatGemini, body)
	if err != nil {
		return nil, err
	}
	postProcessForVertex(result)
	return result, nil
}

func postProcessForVertex(body map[string]interface{}) {
	contents, ok := body["contents"].([]interface{})
	if !ok {
		return
	}
	for _, turnRaw := range contents {
		turn, ok := turnRaw.(map[string]interface{})
		if !ok {
			continue
		}
		parts, ok := turn["parts"].([]interface{})
		if !ok {
			continue
		}
		for _, partRaw := range parts {
			part, ok := partRaw.(map[string]interface{})
			if !ok {
				continue
			}
			if _, has := part["thoughtSignature"]; has {
				part["thoughtSignature"] = defaultVertexThinkingSignature
			}
			if fc, ok := part["functionCall"].(map[string]interface{}); ok {
				delete(fc, "id")
			}
			if fr, ok := part["functionResponse"].(map[string]interface{}); ok {
				delete(fr, "id")
			}
		}
	}
}

// vertexResponseTranslator is a passthrough — Gemini response translator handles it.
type vertexResponseTranslator struct {
	gemini geminiResponseTranslator
}

func (t *vertexResponseTranslator) TranslateResponse(ctx context.Context, sourceFormat, targetFormat string, body []byte) ([]byte, error) {
	return t.gemini.TranslateResponse(ctx, FormatGemini, FormatOpenAI, body)
}
