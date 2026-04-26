package router

import (
	"encoding/json"
	"testing"
)

// BenchmarkJSONMarshal benchmarks JSON marshaling for chat requests
func BenchmarkJSONMarshal(b *testing.B) {
	request := map[string]any{
		"model": "gpt-4",
		"messages": []map[string]string{
			{"role": "user", "content": "Hello, world!"},
		},
		"stream": false,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = json.Marshal(request)
	}
}

// BenchmarkJSONUnmarshal benchmarks JSON unmarshaling for chat requests
func BenchmarkJSONUnmarshal(b *testing.B) {
	jsonData := `{"model":"gpt-4","messages":[{"role":"user","content":"Hello, world!"}],"stream":false}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var req map[string]any
		_ = json.Unmarshal([]byte(jsonData), &req)
	}
}
