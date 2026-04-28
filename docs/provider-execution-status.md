# Provider Execution Status — Final (Fase 10)

Updated: 2026-04-28. Source of truth: `internal/providers/catalog/catalog.go`.

The provider catalog is intentionally conservative: a provider is marked `supported` only when `9router-go` has an executable adapter path and tests for that path.

## Execution Kind Summary

| Kind | Description | Count |
|------|-------------|-------|
| `openai_compatible` | Generic OpenAI-format proxy | 28 |
| `anthropic_compatible` | Claude Messages format proxy | 1 |
| `native` | Provider-specific executor | 12 |
| `ollama` | Ollama API adapter/catalog entry | 2 |
| `media` | Media provider adapter (TTS/STT/image) | 12 |
| `search` | Web search/fetch adapter | 6 |
| `qwen` | Deprecated Qwen executor | 1 |
| `web` | Cookie/session web executor | 2 |

## Supported Providers (63)

### OpenAI-Compatible (28)

openai, openrouter, deepseek, groq, mistral, cohere, google, xai, perplexity, together, fireworks, cerebras, nebius, siliconflow, hyperbolic, chutes, nvidia, byteplus, glm, glm-cn, kimi, minimax, minimax-cn, alicode, alicode-intl, volcengine-ark, blackbox, opencode

### Native Executors (12)

| Provider | Executor | Auth | Notes |
|----------|----------|------|-------|
| codex | native | oauth | Responses executor, SSE, OAuth authorize/exchange/refresh |
| github | native | device_code | Copilot token exchange, refresh-on-401 |
| kiro | native | device_code, import | AWS EventStream, social auth, auto-import |
| cursor | native | import | Protobuf framing, ConnectRPC, auto-import |
| vertex | native | service_account, api_key | SA JWT minting, project/location resolution, raw key support |
| vertex-partner | native | service_account, api_key | Partner model routing, auto-resolve project ID from raw key |
| gemini-cli | native | oauth | OAuth refresh, project ID from account metadata; deprecated CLI-only |
| antigravity | native | oauth | OAuth refresh, project/session from account; deprecated IDE-only |
| iflow | native | oauth, cookie | Cookie/PAT import, OAuth refresh, signature validation |
| qoder | native | oauth | OAuth token |
| opencode-go | native | api_key | Go-specific endpoint |
| azure | native | api_key | Azure deployment URL rewriting |

### Anthropic-Compatible (1)

- `anthropic` — Claude Messages format translation with API key/OAuth credentials.

### Web/Cookie Executors (2)

| Provider | Auth | Notes |
|----------|------|-------|
| grok-web | cookie | Browser-compatible cookie/session runtime with explicit expired-cookie/rate-limit errors |
| perplexity-web | cookie | Browser-compatible SSE runtime with explicit expired-cookie/rate-limit errors |

### Qwen Deprecated Executor (1)

- `qwen` — deprecated-safe runtime that requires an existing valid OAuth token.

### Deprecated-Safe (3)

| Provider | Reason |
|----------|--------|
| qwen | Free tier discontinued 2026-04-15; works only with existing tokens |
| antigravity | IDE-only; may cause account restrictions |
| gemini-cli | CLI-only; may cause account restrictions |

### Ollama (1)

- `ollama` — native `/api/chat` adapter with OpenAI hub translation

### Media/Search Providers (18 supported)

| Provider | Kind | Service |
|----------|------|---------|
| deepgram | stt | Speech-to-text |
| assemblyai | stt | Speech-to-text |
| nanobanana | image | Image generation |
| elevenlabs | tts | Text-to-speech |
| cartesia | tts | Text-to-speech |
| playht | tts | Text-to-speech |
| local-device | tts | Local OS text-to-speech |
| google-tts | tts | Google Translate web text-to-speech |
| edge-tts | tts | Edge/Bing web text-to-speech |
| sdwebui | image | Stable Diffusion WebUI |
| comfyui | image | ComfyUI |
| huggingface | image | HuggingFace inference |
| tavily | search | Web search |
| brave-search | search | Web search |
| serper | search | Google SERP |
| exa | search | Semantic search |
| searxng | search | Self-hosted meta-search |
| firecrawl | webFetch | Web content extraction |

### Search/Fetch (6 supported)

tavily, brave-search, serper, exa, searxng, firecrawl

## Still Planned (1)

| Provider | Reason |
|----------|--------|
| ollama-local | Requires local/cloud catalog split |

## Coverage

- **63 of 64** catalog providers are runtime-supported (98.4%)
- **1** remains planned with documented blocker
- **3** are deprecated-safe (supported but not recommended)
- **0** known reference providers are missing from catalog
