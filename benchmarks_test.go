package router

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
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

// BenchmarkConcurrentMapAccess benchmarks concurrent map access
func BenchmarkConcurrentMapAccess(b *testing.B) {
	m := make(map[string]string)
	m["key1"] = "value1"
	m["key2"] = "value2"

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = m["key1"]
		}
	})
}

// BenchmarkConcurrentMapWrite benchmarks concurrent map writes with mutex
func BenchmarkConcurrentMapWrite(b *testing.B) {
	m := make(map[string]string)
	var mu sync.Mutex

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			mu.Lock()
			m["key"] = "value"
			mu.Unlock()
			i++
		}
	})
}

// BenchmarkStringConcat benchmarks string concatenation
func BenchmarkStringConcat(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s := "Hello"
		s += " "
		s += "World"
		_ = s
	}
}

// BenchmarkStringBuilder benchmarks string builder
func BenchmarkStringBuilder(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var sb strings.Builder
		sb.WriteString("Hello")
		sb.WriteString(" ")
		sb.WriteString("World")
		_ = sb.String()
	}
}

// BenchmarkIdleMemory measures idle memory usage
func BenchmarkIdleMemory(b *testing.B) {
	b.ReportAllocs()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	b.Logf("Idle memory: %d MB", m.Alloc/1024/1024)
}

// BenchmarkStartupTime measures actual binary startup time (--version flag, instant exit).
func BenchmarkStartupTime(b *testing.B) {
	// Ensure binary exists
	binPath := "/tmp/router-bench-binary"
	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		build := exec.Command("go", "build", "-o", binPath, "./cmd/router")
		build.Dir = "."
		if err := build.Run(); err != nil {
			b.Fatalf("Failed to build binary: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		cmd := exec.Command(binPath, "--version")
		_ = cmd.Run() // exits immediately after printing version
		duration := time.Since(start)
		b.Logf("Startup time: %v", duration)
	}
}

// BenchmarkBinarySize measures the compiled binary size
func BenchmarkBinarySize(b *testing.B) {
	// Build the binary if it doesn't exist
	if _, err := os.Stat("./router"); os.IsNotExist(err) {
		cmd := exec.Command("go", "build", "-o", "./router", "./cmd/router")
		cmd.Dir = "."
		if err := cmd.Run(); err != nil {
			b.Fatalf("Failed to build binary: %v", err)
		}
	}

	info, err := os.Stat("./router")
	if err != nil {
		b.Fatalf("Failed to stat binary: %v", err)
	}
	sizeMB := float64(info.Size()) / 1024 / 1024
	b.Logf("Binary size: %.2f MB", sizeMB)
}

// BenchmarkHTTPLoad benchmarks concurrent HTTP request handling against an in-process
// health-check handler — tests the server's HTTP stack throughput under load.
func BenchmarkHTTPLoad(b *testing.B) {
	// Minimal handler that mimics the /healthz endpoint
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := &http.Client{
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 100,
		},
		Timeout: 5 * time.Second,
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			resp, err := client.Get(srv.URL + "/healthz")
			if err != nil {
				b.Errorf("request failed: %v", err)
				return
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	})
}
