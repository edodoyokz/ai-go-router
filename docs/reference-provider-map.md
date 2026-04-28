# Reference Provider Map — Final (Fase 10)

Updated: 2026-04-28. Source of truth: `internal/providers/catalog/catalog.go`.

## Status Legend

| Status | Meaning |
|--------|---------|
| **supported** | Present in catalog, factory builds it, runtime executor works |
| **planned** | Present in catalog, runtime intentionally blocked |
| **deprecated** | Supported but carries deprecation notice |

## Generic OpenAI-Compatible (27 providers)

| ID | Name | Category | Status |
|----|------|----------|--------|
| openai | OpenAI | api_key | supported |
| openrouter | OpenRouter | free_tier | supported |
| deepseek | DeepSeek | api_key | supported |
| groq | Groq | api_key | supported |
| mistral | Mistral | api_key | supported |
| cohere | Cohere | api_key | supported |
| google | Google | api_key | supported |
| xai | xAI | api_key | supported |
| perplexity | Perplexity | api_key | supported |
| together | Together | api_key | supported |
| fireworks | Fireworks | api_key | supported |
| cerebras | Cerebras | api_key | supported |
| nebius | Nebius | api_key | supported |
| siliconflow | SiliconFlow | api_key | supported |
| hyperbolic | Hyperbolic | api_key | supported |
| chutes | Chutes | api_key | supported |
| nvidia | NVIDIA NIM | api_key | supported |
| byteplus | BytePlus | api_key | supported |
| glm | GLM | api_key | supported |
| glm-cn | GLM CN | api_key | supported |
| kimi | Kimi | api_key | supported |
| minimax | MiniMax | api_key | supported |
| minimax-cn | MiniMax CN | api_key | supported |
| alicode | Alicode | api_key | supported |
| alicode-intl | Alicode Intl | api_key | supported |
| volcengine-ark | Volcengine Ark | api_key | supported |
| blackbox | Blackbox | api_key | supported |

## Native/OAuth/Cookie/Service Account (17 providers)

| ID | Name | Category | Auth | Status | Notes |
|----|------|----------|------|--------|-------|
| anthropic | Anthropic | api_key | api_key, oauth | supported | Claude format |
| codex | OpenAI Codex | oauth | oauth | supported | Native Responses executor |
| github | GitHub Copilot | oauth | device_code | supported | Native executor |
| kiro | Kiro AI | free | device_code, import | supported | Native AWS EventStream |
| qwen | Qwen Code | free | device_code | deprecated | Free tier discontinued |
| opencode | OpenCode | free | no_auth | supported | OpenAI-compatible |
| cursor | Cursor | oauth | import | supported | Native protobuf executor |
| antigravity | Antigravity | oauth | oauth | supported | OAuth refresh, deprecated IDE-only |
| grok-web | Grok Web | cookie | cookie | supported | Deprecated-safe cookie/session replay with explicit expired-session errors |
| perplexity-web | Perplexity Web | cookie | cookie | supported | Deprecated-safe cookie/session SSE replay with explicit expired-session errors |
| ollama | Ollama | free | no_auth | supported | Ollama format |
| vertex | Vertex | api_key | service_account, api_key | supported | SA JWT minting, project/location resolution |
| vertex-partner | Vertex Partner | api_key | service_account, api_key | supported | Auto-resolve project ID from raw key |
| gemini-cli | Gemini CLI | free | oauth | supported | OAuth refresh, deprecated CLI-only |
| iflow | iFlow AI | free | oauth, cookie | supported | Cookie/PAT import, OAuth refresh |
| qoder | Qoder AI | free | oauth | supported | Native executor |
| opencode-go | OpenCode Go | api_key | api_key | supported | Native executor |
| azure | Azure OpenAI | api_key | api_key | supported | Azure deployment style |

## Media/Search/WebFetch (19 providers)

| ID | Name | Category | Kind | Status |
|----|------|----------|------|--------|
| deepgram | Deepgram | api_key | stt/media | supported |
| assemblyai | AssemblyAI | api_key | stt/media | supported |
| nanobanana | NanoBanana | api_key | image/media | supported |
| elevenlabs | ElevenLabs | api_key | tts/media | supported |
| cartesia | Cartesia | api_key | tts/media | supported |
| playht | PlayHT | api_key | tts/media | supported |
| local-device | Local Device | free | tts/media | supported |
| google-tts | Google TTS | free | tts/media | supported |
| edge-tts | Edge TTS | free | tts/media | supported |
| sdwebui | SD WebUI | api_key | image/media | supported |
| comfyui | ComfyUI | api_key | image/media | supported |
| huggingface | HuggingFace | api_key | image/media | supported |
| ollama-local | Ollama Local | free | llm/ollama | planned |
| tavily | Tavily | api_key | search | supported |
| brave-search | Brave Search | api_key | search | supported |
| serper | Serper | api_key | search | supported |
| exa | Exa | api_key | search | supported |
| searxng | SearXNG | free | search | supported |
| firecrawl | Firecrawl | api_key | webFetch | supported |

## Summary

| Category | Supported | Planned | Deprecated-safe subset | Total |
|----------|-----------|---------|------------------------|-------|
| OpenAI-compatible | 27 | 0 | 0 | 27 |
| Native/OAuth/cookie/web | 18 | 0 | 3 | 18 |
| Media/search/fetch | 18 | 1 | 0 | 19 |
| **Total** | **63** | **1** | **3** | **64** |

## Missing Reference Providers

None known from `reference/9router/src/shared/constants/providers.js`.
