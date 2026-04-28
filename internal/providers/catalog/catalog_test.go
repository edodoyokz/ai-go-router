package catalog

import (
	"testing"
)

var validServiceKinds = map[string]struct{}{
	"llm": {}, "embedding": {}, "tts": {}, "stt": {}, "image": {}, "imageToText": {}, "webSearch": {}, "webFetch": {}, "video": {}, "music": {},
}

var validExecutionStatuses = map[string]struct{}{
	"supported": {}, "planned": {}, "deprecated": {}, "disabled": {},
}

func TestResolveAliasCoverage(t *testing.T) {
	aliases := map[string]string{
		"cc":     "anthropic",
		"cx":     "codex",
		"gh":     "github",
		"ds":     "deepseek",
		"kr":     "kiro",
		"qw":     "qwen",
		"oc":     "opencode",
		"pplx":   "perplexity",
		"ollama": "ollama",
	}
	for alias, id := range aliases {
		def, ok := ResolveAlias(alias)
		if !ok {
			t.Fatalf("alias %s not found", alias)
		}
		if def.ID != id {
			t.Fatalf("alias %s resolved %s want %s", alias, def.ID, id)
		}
	}
}

func TestExecutableProviderStatuses(t *testing.T) {
	for _, id := range []string{"github", "opencode", "ollama"} {
		def, ok := Get(id)
		if !ok {
			t.Fatalf("provider %s missing", id)
		}
		if def.ExecutionStatus != "supported" {
			t.Fatalf("provider %s status = %s, want supported", id, def.ExecutionStatus)
		}
		if def.ExecutionKind == "" {
			t.Fatalf("provider %s missing execution kind", id)
		}
	}
}

func TestCatalogUniqueIDsAndAliases(t *testing.T) {
	idSeen := map[string]struct{}{}
	aliasSeen := map[string]string{}
	for _, def := range List() {
		if _, ok := idSeen[def.ID]; ok {
			t.Fatalf("duplicate provider id: %s", def.ID)
		}
		idSeen[def.ID] = struct{}{}

		aliases := make([]string, 0, len(def.Aliases)+1)
		if def.Alias != "" {
			aliases = append(aliases, def.Alias)
		}
		aliases = append(aliases, def.Aliases...)
		for _, alias := range aliases {
			if alias == "" {
				continue
			}
			if owner, ok := aliasSeen[alias]; ok && owner != def.ID {
				t.Fatalf("duplicate alias %q used by %s and %s", alias, owner, def.ID)
			}
			aliasSeen[alias] = def.ID
		}
	}
}

func TestSupportedProvidersHaveExecutionKind(t *testing.T) {
	for _, def := range List() {
		if def.ExecutionStatus != "supported" {
			continue
		}
		if def.ExecutionKind == "" {
			t.Fatalf("supported provider %s missing execution kind", def.ID)
		}
	}
}

func TestCatalogStatusesKindsAndRuntimeReasons(t *testing.T) {
	for _, def := range List() {
		if _, ok := validExecutionStatuses[def.ExecutionStatus]; !ok {
			t.Fatalf("provider %s has invalid execution status %q", def.ID, def.ExecutionStatus)
		}
		for _, kind := range def.ServiceKinds {
			if _, ok := validServiceKinds[kind]; !ok {
				t.Fatalf("provider %s has invalid service kind %q", def.ID, kind)
			}
		}
		if def.ExecutionStatus == "planned" && def.UnsupportedRuntimeReason == "" {
			t.Fatalf("planned provider %s missing unsupported runtime reason", def.ID)
		}
	}
}

func TestReferenceProviderIDsCovered(t *testing.T) {
	referenceIDs := []string{
		"kiro", "qwen", "gemini-cli", "iflow", "opencode", "openrouter", "nvidia", "ollama", "vertex", "google", "byteplus", "anthropic", "antigravity", "codex", "github", "cursor", "glm", "glm-cn", "kimi", "minimax", "minimax-cn", "alicode", "alicode-intl", "volcengine-ark", "openai", "opencode-go", "azure", "deepseek", "groq", "xai", "mistral", "perplexity", "together", "fireworks", "cerebras", "cohere", "nebius", "siliconflow", "hyperbolic", "deepgram", "assemblyai", "nanobanana", "elevenlabs", "cartesia", "playht", "local-device", "google-tts", "edge-tts", "sdwebui", "comfyui", "huggingface", "blackbox", "chutes", "ollama-local", "vertex-partner", "tavily", "brave-search", "serper", "exa", "searxng", "firecrawl", "grok-web", "perplexity-web",
	}
	for _, id := range referenceIDs {
		if _, ok := Get(id); !ok {
			t.Fatalf("reference provider %s missing from Go catalog", id)
		}
	}
}
