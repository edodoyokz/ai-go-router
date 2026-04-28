package providers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/edodoyokz/ai-go-router/internal/config"
)

func TestMediaSearchAdapterElevenLabsSpeech(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/text-to-speech/voice-1" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if r.Header.Get("xi-api-key") != "el-key" {
			t.Fatalf("api key header=%q", r.Header.Get("xi-api-key"))
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("mp3"))
	}))
	defer server.Close()

	adapter := NewMediaSearchAdapter(config.ProviderConfig{Name: "elevenlabs", ProviderID: "elevenlabs", Type: "elevenlabs", BaseURL: server.URL, APIKey: "el-key", Enabled: true}, config.ErrorConfig{}, "")
	resp, err := adapter.AudioSpeech(context.Background(), AudioSpeechRequest{Input: "hello", Voice: "voice-1"}, "eleven_flash_v2_5")
	if err != nil {
		t.Fatalf("AudioSpeech: %v", err)
	}
	if resp.ContentType != "audio/mpeg" || string(resp.Data) != "mp3" {
		t.Fatalf("resp=%#v", resp)
	}
}

func TestMediaSearchAdapterCartesiaSpeech(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tts/bytes" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer ca-key" || r.Header.Get("Cartesia-Version") != "2026-03-01" {
			t.Fatalf("headers auth=%q version=%q", r.Header.Get("Authorization"), r.Header.Get("Cartesia-Version"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["model_id"] != "sonic-3" || body["transcript"] != "hello" {
			t.Fatalf("body=%#v", body)
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("cartesia-mp3"))
	}))
	defer server.Close()

	adapter := NewMediaSearchAdapter(config.ProviderConfig{Name: "cartesia", ProviderID: "cartesia", Type: "cartesia", BaseURL: server.URL, APIKey: "ca-key", Enabled: true}, config.ErrorConfig{}, "")
	resp, err := adapter.AudioSpeech(context.Background(), AudioSpeechRequest{Input: "hello", Voice: "voice-1"}, "sonic-3")
	if err != nil {
		t.Fatalf("AudioSpeech: %v", err)
	}
	if string(resp.Data) != "cartesia-mp3" || resp.ContentType != "audio/mpeg" {
		t.Fatalf("resp=%#v", resp)
	}
}

func TestMediaSearchAdapterPlayHTSpeech(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/tts/stream" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer ph-key" || r.Header.Get("X-USER-ID") != "user-1" {
			t.Fatalf("headers auth=%q user=%q", r.Header.Get("Authorization"), r.Header.Get("X-USER-ID"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["voice"] != "voice-1" || body["text"] != "hello" {
			t.Fatalf("body=%#v", body)
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("playht-mp3"))
	}))
	defer server.Close()

	adapter := NewMediaSearchAdapter(config.ProviderConfig{
		Name:       "playht",
		ProviderID: "playht",
		Type:       "playht",
		BaseURL:    server.URL,
		APIKey:     "ph-key",
		Enabled:    true,
		ProviderSpecificData: map[string]any{
			"userId": "user-1",
		},
	}, config.ErrorConfig{}, "")
	resp, err := adapter.AudioSpeech(context.Background(), AudioSpeechRequest{Input: "hello", Voice: "voice-1"}, "Play3.0-mini")
	if err != nil {
		t.Fatalf("AudioSpeech: %v", err)
	}
	if string(resp.Data) != "playht-mp3" {
		t.Fatalf("resp=%#v", resp)
	}
}

func TestMediaSearchAdapterGoogleAndEdgeSpeech(t *testing.T) {
	t.Run("google-tts", func(t *testing.T) {
		resetMediaSearchTTSCaches()
		audio := base64.StdEncoding.EncodeToString([]byte("google-mp3"))
		line, _ := json.Marshal([]any{[]any{nil, nil, `["` + audio + `"]`}})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/":
				_, _ = w.Write([]byte(`"FdrFJe":"fsid","cfb2h":"blid"`))
			case "/_/TranslateWebserverUi/data/batchexecute":
				if r.Method != http.MethodPost || r.URL.Query().Get("f.sid") != "fsid" {
					t.Fatalf("google request method=%s query=%s", r.Method, r.URL.RawQuery)
				}
				_, _ = w.Write([]byte("\n\n\n" + string(line)))
			default:
				t.Fatalf("path=%s", r.URL.Path)
			}
		}))
		defer server.Close()

		adapter := NewMediaSearchAdapter(config.ProviderConfig{Name: "google-tts", ProviderID: "google-tts", Type: "google-tts", BaseURL: server.URL, Enabled: true}, config.ErrorConfig{}, "")
		resp, err := adapter.AudioSpeech(context.Background(), AudioSpeechRequest{Input: "hello"}, "en")
		if err != nil {
			t.Fatalf("AudioSpeech: %v", err)
		}
		if string(resp.Data) != "google-mp3" {
			t.Fatalf("resp=%#v", resp)
		}
	})

	t.Run("edge-tts", func(t *testing.T) {
		resetMediaSearchTTSCaches()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/translator":
				http.SetCookie(w, &http.Cookie{Name: "edge", Value: "cookie"})
				_, _ = w.Write([]byte(`params_AbusePreventionHelper = [123,"tok",456,];`))
			case "/tfettts":
				if r.Method != http.MethodPost || r.Header.Get("Cookie") != "edge=cookie" {
					t.Fatalf("edge request method=%s cookie=%q", r.Method, r.Header.Get("Cookie"))
				}
				_, _ = w.Write([]byte("edge-mp3"))
			default:
				t.Fatalf("path=%s", r.URL.Path)
			}
		}))
		defer server.Close()

		adapter := NewMediaSearchAdapter(config.ProviderConfig{Name: "edge-tts", ProviderID: "edge-tts", Type: "edge-tts", BaseURL: server.URL, Enabled: true}, config.ErrorConfig{}, "")
		resp, err := adapter.AudioSpeech(context.Background(), AudioSpeechRequest{Input: "hello", Voice: "en-US-AriaNeural"}, "")
		if err != nil {
			t.Fatalf("AudioSpeech: %v", err)
		}
		if string(resp.Data) != "edge-mp3" {
			t.Fatalf("resp=%#v", resp)
		}
	})
}

func TestMediaSearchAdapterLocalDeviceSpeechWithFakeCommands(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("fake command test is linux-specific")
	}
	bin := t.TempDir()
	writeExecutable(t, filepath.Join(bin, "espeak"), `#!/bin/sh
out=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-w" ]; then shift; out="$1"; fi
  shift || true
done
printf wav > "$out"
`)
	writeExecutable(t, filepath.Join(bin, "ffmpeg"), `#!/bin/sh
last=""
for arg in "$@"; do last="$arg"; done
printf mp3 > "$last"
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	adapter := NewMediaSearchAdapter(config.ProviderConfig{Name: "local-device", ProviderID: "local-device", Type: "local-device", Enabled: true}, config.ErrorConfig{}, "")
	resp, err := adapter.AudioSpeech(context.Background(), AudioSpeechRequest{Input: "hello", Voice: "en"}, "")
	if err != nil {
		t.Fatalf("AudioSpeech: %v", err)
	}
	if string(resp.Data) != "mp3" {
		t.Fatalf("resp=%#v", resp)
	}
}

func TestMediaSearchAdapterDeepgramTranscribe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/listen" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Token dg-key" {
			t.Fatalf("auth=%q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":{"channels":[{"alternatives":[{"transcript":"hello world"}]}]}}`))
	}))
	defer server.Close()

	adapter := NewMediaSearchAdapter(config.ProviderConfig{Name: "deepgram", ProviderID: "deepgram", Type: "deepgram", BaseURL: server.URL, APIKey: "dg-key", Enabled: true}, config.ErrorConfig{}, "")
	resp, err := adapter.TranscribeAudio(context.Background(), AudioTranscriptionRequest{AudioURL: "https://example.test/audio.wav"}, "nova-2")
	if err != nil {
		t.Fatalf("TranscribeAudio: %v", err)
	}
	if resp.Text != "hello world" {
		t.Fatalf("resp=%#v", resp)
	}
}

func TestMediaSearchAdapterSearchAndFetch(t *testing.T) {
	t.Run("tavily", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/search" {
				t.Fatalf("path=%s", r.URL.Path)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body["api_key"] != "tv-key" || body["query"] != "golang" {
				t.Fatalf("body=%#v", body)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"answer":"ok","results":[{"title":"Go","url":"https://go.dev","content":"The Go language","score":0.9}]}`))
		}))
		defer server.Close()

		adapter := NewMediaSearchAdapter(config.ProviderConfig{Name: "tavily", ProviderID: "tavily", Type: "tavily", BaseURL: server.URL, APIKey: "tv-key", Enabled: true}, config.ErrorConfig{}, "")
		resp, err := adapter.WebSearch(context.Background(), WebSearchRequest{Query: "golang", MaxResults: 1})
		if err != nil {
			t.Fatalf("WebSearch: %v", err)
		}
		if resp.Answer != "ok" || len(resp.Results) != 1 || resp.Results[0].URL != "https://go.dev" {
			t.Fatalf("resp=%#v", resp)
		}
	})

	t.Run("brave-search", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/res/v1/web/search" || r.URL.Query().Get("q") != "golang" {
				t.Fatalf("path=%s query=%s", r.URL.Path, r.URL.RawQuery)
			}
			if r.Header.Get("X-Subscription-Token") != "br-key" {
				t.Fatalf("auth=%q", r.Header.Get("X-Subscription-Token"))
			}
			_, _ = w.Write([]byte(`{"web":{"results":[{"title":"Go","url":"https://go.dev","description":"ignored","snippet":"Go snippet"}]}}`))
		}))
		defer server.Close()
		adapter := NewMediaSearchAdapter(config.ProviderConfig{Name: "brave", ProviderID: "brave-search", Type: "brave-search", BaseURL: server.URL, APIKey: "br-key", Enabled: true}, config.ErrorConfig{}, "")
		resp, err := adapter.WebSearch(context.Background(), WebSearchRequest{Query: "golang"})
		if err != nil {
			t.Fatalf("WebSearch: %v", err)
		}
		if len(resp.Results) != 1 || resp.Results[0].URL != "https://go.dev" {
			t.Fatalf("resp=%#v", resp)
		}
	})

	t.Run("serper", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-API-KEY") != "sp-key" {
				t.Fatalf("auth=%q", r.Header.Get("X-API-KEY"))
			}
			_, _ = w.Write([]byte(`{"organic":[{"title":"Go","link":"https://go.dev","snippet":"Go"}]}`))
		}))
		defer server.Close()
		adapter := NewMediaSearchAdapter(config.ProviderConfig{Name: "serper", ProviderID: "serper", Type: "serper", BaseURL: server.URL, APIKey: "sp-key", Enabled: true}, config.ErrorConfig{}, "")
		resp, err := adapter.WebSearch(context.Background(), WebSearchRequest{Query: "golang"})
		if err != nil {
			t.Fatalf("WebSearch: %v", err)
		}
		if len(resp.Results) != 1 || resp.Results[0].Content != "Go" {
			t.Fatalf("resp=%#v", resp)
		}
	})

	t.Run("exa", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("x-api-key") != "exa-key" {
				t.Fatalf("auth=%q", r.Header.Get("x-api-key"))
			}
			_, _ = w.Write([]byte(`{"results":[{"title":"Go","url":"https://go.dev","text":"Go text","score":0.7}]}`))
		}))
		defer server.Close()
		adapter := NewMediaSearchAdapter(config.ProviderConfig{Name: "exa", ProviderID: "exa", Type: "exa", BaseURL: server.URL, APIKey: "exa-key", Enabled: true}, config.ErrorConfig{}, "")
		resp, err := adapter.WebSearch(context.Background(), WebSearchRequest{Query: "golang"})
		if err != nil {
			t.Fatalf("WebSearch: %v", err)
		}
		if len(resp.Results) != 1 || resp.Results[0].Score != 0.7 {
			t.Fatalf("resp=%#v", resp)
		}
	})

	t.Run("searxng", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("format") != "json" {
				t.Fatalf("query=%s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"results":[{"title":"Go","url":"https://go.dev","content":"Go"}]}`))
		}))
		defer server.Close()
		adapter := NewMediaSearchAdapter(config.ProviderConfig{Name: "searxng", ProviderID: "searxng", Type: "searxng", BaseURL: server.URL, Enabled: true}, config.ErrorConfig{}, "")
		resp, err := adapter.WebSearch(context.Background(), WebSearchRequest{Query: "golang"})
		if err != nil {
			t.Fatalf("WebSearch: %v", err)
		}
		if len(resp.Results) != 1 || resp.Results[0].Title != "Go" {
			t.Fatalf("resp=%#v", resp)
		}
	})

	t.Run("firecrawl", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/scrape" {
				t.Fatalf("path=%s", r.URL.Path)
			}
			if r.Header.Get("Authorization") != "Bearer fc-key" {
				t.Fatalf("auth=%q", r.Header.Get("Authorization"))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"url":"https://example.test","title":"Example","markdown":"# Example"}}`))
		}))
		defer server.Close()

		adapter := NewMediaSearchAdapter(config.ProviderConfig{Name: "firecrawl", ProviderID: "firecrawl", Type: "firecrawl", BaseURL: server.URL, APIKey: "fc-key", Enabled: true}, config.ErrorConfig{}, "")
		resp, err := adapter.WebFetch(context.Background(), WebFetchRequest{URL: "https://example.test"})
		if err != nil {
			t.Fatalf("WebFetch: %v", err)
		}
		if resp.Title != "Example" || resp.Markdown != "# Example" {
			t.Fatalf("resp=%#v", resp)
		}
	})
}

func TestMediaSearchAdapterImageGenerationMappings(t *testing.T) {
	t.Run("nanobanana", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body["type"] != "TEXTTOIAMGE" || body["image_size"] != "16:9" {
				t.Fatalf("body=%#v", body)
			}
			_, _ = w.Write([]byte(`{"image":"abc123"}`))
		}))
		defer server.Close()
		adapter := NewMediaSearchAdapter(config.ProviderConfig{Name: "nanobanana", ProviderID: "nanobanana", Type: "nanobanana", BaseURL: server.URL, APIKey: "nb-key", Enabled: true}, config.ErrorConfig{}, "")
		resp, err := adapter.ImagesGenerations(context.Background(), ImagesGenerationsRequest{Prompt: "cat", Size: "1792x1024"}, "nanobanana-flash")
		if err != nil {
			t.Fatalf("ImagesGenerations: %v", err)
		}
		if len(resp.Data) != 1 || !strings.Contains(resp.Data[0].URL, "abc123") {
			t.Fatalf("resp=%#v", resp)
		}
	})

	t.Run("sdwebui", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/sdapi/v1/txt2img" {
				t.Fatalf("path=%s", r.URL.Path)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body["width"] != float64(1024) || body["height"] != float64(512) {
				t.Fatalf("body=%#v", body)
			}
			_, _ = w.Write([]byte(`{"images":["sd-b64"]}`))
		}))
		defer server.Close()
		adapter := NewMediaSearchAdapter(config.ProviderConfig{Name: "sdwebui", ProviderID: "sdwebui", Type: "sdwebui", BaseURL: server.URL, Enabled: true}, config.ErrorConfig{}, "")
		resp, err := adapter.ImagesGenerations(context.Background(), ImagesGenerationsRequest{Prompt: "cat", Size: "1024x512"}, "sdxl")
		if err != nil {
			t.Fatalf("ImagesGenerations: %v", err)
		}
		if len(resp.Data) != 1 || !strings.Contains(resp.Data[0].URL, "sd-b64") {
			t.Fatalf("resp=%#v", resp)
		}
	})

	t.Run("huggingface", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/black-forest-labs/FLUX.1-schnell" {
				t.Fatalf("path=%s", r.URL.Path)
			}
			if r.Header.Get("Authorization") != "Bearer hf-key" {
				t.Fatalf("auth=%q", r.Header.Get("Authorization"))
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("png-bytes"))
		}))
		defer server.Close()
		adapter := NewMediaSearchAdapter(config.ProviderConfig{Name: "huggingface", ProviderID: "huggingface", Type: "huggingface", BaseURL: server.URL, APIKey: "hf-key", Enabled: true}, config.ErrorConfig{}, "")
		resp, err := adapter.ImagesGenerations(context.Background(), ImagesGenerationsRequest{Prompt: "cat"}, "black-forest-labs/FLUX.1-schnell")
		if err != nil {
			t.Fatalf("ImagesGenerations: %v", err)
		}
		if len(resp.Data) != 1 || !strings.HasPrefix(resp.Data[0].URL, "data:image/png;base64,") {
			t.Fatalf("resp=%#v", resp)
		}
	})
}

func TestMediaSearchAdapterVoiceListing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/voices" || r.Header.Get("xi-api-key") != "el-key" {
			t.Fatalf("path=%s auth=%q", r.URL.Path, r.Header.Get("xi-api-key"))
		}
		_, _ = w.Write([]byte(`{"voices":[{"voice_id":"v1","name":"Rachel","category":"premade","labels":{"language":"en","gender":"female"}}]}`))
	}))
	defer server.Close()
	adapter := NewMediaSearchAdapter(config.ProviderConfig{Name: "elevenlabs", ProviderID: "elevenlabs", Type: "elevenlabs", BaseURL: server.URL, APIKey: "el-key", Enabled: true}, config.ErrorConfig{}, "")
	resp, err := adapter.ListTTSVoices(context.Background(), "en")
	if err != nil {
		t.Fatalf("ListTTSVoices: %v", err)
	}
	if len(resp.Voices) != 1 || len(resp.ByLang["en"].Voices) != 1 {
		t.Fatalf("resp=%#v", resp)
	}
}

func resetMediaSearchTTSCaches() {
	googleTTSCacheMu.Lock()
	googleTTSCache = struct {
		fsid string
		bl   string
		at   time.Time
	}{}
	googleTTSCacheMu.Unlock()
	edgeTTSCacheMu.Lock()
	edgeTTSCache = struct {
		key    string
		token  string
		cookie string
		at     time.Time
	}{}
	edgeTTSCacheMu.Unlock()
}

func writeExecutable(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0755); err != nil {
		t.Fatal(err)
	}
}
