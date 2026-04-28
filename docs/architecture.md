# Architecture — NusaNexus Router

## Overview

NusaNexus Router is a local-first AI model router that provides OpenAI-compatible, Claude-compatible, and tool-friendly endpoints with automatic fallback across multiple AI providers. Built in Go for low resource usage and simple deployment while preserving feature parity targets from the reference 9Router implementation.

## Core Design Principles

1. **Config + SQLite as Source of Truth**: YAML config bootstraps providers, routes, and policies. SQLite stores operational state, API keys, usage, aliases, custom models, cooldowns, and UI-managed data.
2. **Single-Node Focus**: Optimized for localhost/VPS deployment, not distributed systems.
3. **CLI-First Operations**: Admin operations via CLI before web UI.
4. **Deterministic Routing**: Predictable fallback behavior based on error classification.
5. **Reference Parity, Phased Delivery**: Features from `reference/9router` must be represented in architecture and roadmap even when deferred.
6. **Translation Hub**: Use OpenAI-compatible shape as the intermediate format for cross-provider translation.

## System Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                      AI Client Tools                         │
│     Cursor, Cline, Claude Code, Codex, OpenClaw, CLIs        │
└────────────────────────┬────────────────────────────────────┘
                         │ HTTP (/v1/chat/completions, /v1/messages, /v1/responses)
                         ▼
┌─────────────────────────────────────────────────────────────┐
│                      API Layer                               │
│  ┌──────────────────────────────────────────────────────┐   │
│  │ Middleware: Auth, RequestID, Panic Recovery, Logging │   │
│  └──────────────────────────────────────────────────────┘   │
│  ┌──────────────────────────────────────────────────────┐   │
│  │ Handlers: /v1/chat/completions, /v1/messages,       │   │
│  │           /v1/responses, /v1/models, /healthz,      │   │
│  │           /readyz, /metrics                         │   │
│  └──────────────────────────────────────────────────────┘   │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│                 Format + Routing Core                        │
│  • Source Format Detection (OpenAI, Claude, Responses, etc.)│
│  • Hub Translation (source → OpenAI → target)               │
│  • Alias Resolution (coding-default → provider chain)       │
│  • Direct Route (provider/model)                             │
│  • Tier-Based Ordering (subscription → cheap → free)        │
│  • Combo Strategy (fallback / round-robin)                  │
│  • Retry Policy (config-driven backoff)                     │
│  • Fallback Logic (retryable errors only)                   │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│          Provider Registry + Executor Layer                  │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │   OpenAI     │  │  Anthropic   │  │   Gemini     │      │
│  │  Executor    │  │  Executor    │  │  Executor    │      │
│  │ (passthrough)│  │ (headers/API)│  │ (native API) │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
│  Dynamic: openai-compatible-* / anthropic-compatible-*      │
└────────────────────────┬────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│                External Provider APIs                        │
│     OpenAI API, Anthropic API, Google AI, etc.              │
└─────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────┐
│                   Persistence Layer                          │
│  ┌──────────────────────────────────────────────────────┐   │
│  │ SQLite: keys, aliases, custom_models, accounts,      │   │
│  │ request_logs, request_details, usage, quota, pricing, │   │
│  │ cooldowns, model_locks, settings                     │   │
│  └──────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
```

## Package Structure

```
router/
├── cmd/
│   └── router/              # CLI entrypoint
│       └── main.go          # Binary entry, command setup
├── internal/
│   ├── api/                 # HTTP API layer
│   │   └── server.go        # Handlers, middleware mounting
│   ├── app/                 # Application wiring
│   │   └── app.go           # Dependency injection, startup
│   ├── auth/                # Authentication (future)
│   ├── config/              # Configuration
│   │   └── config.go        # YAML loader, validation
│   ├── logging/             # Structured logging (future)
│   ├── metrics/             # Prometheus metrics (future)
│   ├── providers/           # Provider abstraction
│   │   ├── providers.go     # Interface, registry, models
│   │   ├── openai.go        # OpenAI-compatible executor/adapter
│   │   ├── anthropic.go     # Anthropic executor/adapter
│   │   ├── errors.go        # Error taxonomy
│   │   └── executors/       # Provider-specific execution behavior
│   ├── translator/          # Hub-and-spoke format translation
│   │   ├── formats.go       # Format identifiers and detection
│   │   ├── registry.go      # Request/response translator registry
│   │   ├── request/         # Source → OpenAI → target request mapping
│   │   └── response/        # Target → OpenAI → source response mapping
│   ├── models/              # Model aliases, custom models, discovery
│   ├── usage/               # Usage, quota, pricing, cost tracking
│   ├── router/              # Routing engine
│   │   └── router.go        # Alias resolution, combo strategy, fallback
│   ├── storage/             # SQLite persistence
│   ├── tunnel/              # Cloudflare/Tailscale tunnel support (future)
│   └── mitm/                # MITM proxy support (future)
├── config/
│   └── config.example.yaml  # Example configuration
└── docs/
    ├── prd-final.md         # Product requirements
    └── architecture.md      # This file
```

## Request Lifecycle

### 1. Incoming Request
```
Client → API Layer
  ├─ Middleware: Request ID generation
  ├─ Middleware: Authentication (API key validation)
  ├─ Middleware: Panic recovery
  └─ Handler: /v1/chat/completions, /v1/messages, /v1/responses, /v1/models
```

### 2. Format Detection and Translation
```
Handler → Translator
  ├─ Detect source format from endpoint and body
  │   ├─ /v1/responses → openai-responses
  │   ├─ /v1/messages → claude
  │   └─ body.messages/input/contents → inferred format
  ├─ Determine target format from provider/model metadata
  └─ Translate source → OpenAI intermediate → target
```

### 3. Route Resolution
```
Handler → Routing Engine
  ├─ Parse model field from request
  ├─ Check if alias exists in config (e.g., "coding-default")
  │   └─ Yes: Return configured target chain
  └─ Check if direct route (e.g., "openai/gpt-4")
      └─ Yes: Parse provider/model and create single target
  └─ Apply combo strategy (fallback or round-robin)
```

### 4. Provider Execution with Fallback
```
For each target in chain:
  ├─ Skip account/model if cooldown or model lock is active
  ├─ Get executor from registry
  ├─ Execute request with retry policy
  │   ├─ Attempt 1
  │   ├─ If retryable error: classify + cooldown/backoff + Attempt 2
  │   └─ If retryable error: classify + cooldown/backoff + Attempt 3
  ├─ If success: return response
  └─ If all retries exhausted:
      └─ Continue to next target (fallback)

If all targets exhausted: return error
```

### 5. Response Translation and Logging
```
Response → Client
  ├─ Translate target → OpenAI intermediate → source format
  ├─ Log request metadata to SQLite (async)
  │   ├─ request_id, route_alias, provider_used
  │   ├─ model, status, latency, token_usage
  │   └─ fallback_count, cooldown, model_lock, error_reason
  └─ Return response in client-compatible format
```

## Error Taxonomy

### Retryable Errors (trigger retry/fallback)
- Network timeout
- Connection refused
- HTTP 429 (rate limit)
- HTTP 503 (service unavailable)
- Selected 5xx errors
- Provider quota exhausted signal
- Provider temporarily unavailable
- Text triggers such as `rate limit`, `too many requests`, `quota exceeded`, `capacity`, `overloaded`

### Non-Retryable Errors (fail immediately)
- HTTP 400 (bad request)
- Invalid request schema
- Unsupported model/alias
- Malformed upstream response
- Internal auth failure (router API key)

### Config-Driven Classification
Error classification must be data-driven:
1. Text-based rules checked first
2. Status-code rules checked second
3. Default transient fallback if no specific rule matches

Rate-limit rules update account state with exponential backoff and cooldown. Provider-reported long cooldowns must be capped by config.

## Routing Semantics

### Alias Routes
Defined in config, map friendly names to provider chains:
```yaml
routes:
  coding-default:
    - provider: anthropic
      model: claude-sonnet-4-5
      tier: subscription
    - provider: openai_compat
      model: gpt-4.1-mini
      tier: cheap
```

### Direct Routes
Inline format `provider/model`:
```
Request: {"model": "openai/gpt-4"}
→ Routes directly to openai provider with gpt-4 model
```

### Provider Aliases
Short aliases map to provider IDs:
```
cc/claude-sonnet → claude/claude-sonnet
ds/deepseek-chat → deepseek/deepseek-chat
openai/gpt-4.1 → openai/gpt-4.1
```

### Combo Strategies
- **fallback**: Preserve configured order and fall through only on retryable errors.
- **round-robin**: Rotate the first attempted target per request while preserving fallback order after rotation.

### Tier System
Three tiers define cost/priority:
- **subscription**: Paid subscriptions (Claude Pro, ChatGPT Plus)
- **cheap**: Pay-per-token services (DeepSeek, MiniMax)
- **free**: Free tiers (safety net)

Tier is metadata for policy, reporting, and default ordering. Explicit route order in config remains authoritative.

## Translation Architecture

### Format Identifiers
Supported formats are represented explicitly:
- `openai`
- `openai-responses`
- `claude`
- `gemini`
- `ollama`
- `cursor`
- `kiro`
- `antigravity`
- `vertex`

### Hub-and-Spoke Registry
All cross-format conversion goes through OpenAI-compatible intermediate format:
```
source format → openai → target format
target format → openai → source format
```

This avoids building translators for every source/target pair.

### Translator Contract
```go
type RequestTranslator interface {
    TranslateRequest(ctx context.Context, req TranslationRequest) (TranslationResult, error)
}

type ResponseTranslator interface {
    TranslateResponse(ctx context.Context, chunk TranslationChunk, state *TranslationState) ([]TranslationChunk, error)
}
```

### Translation Responsibilities
- Normalize tool call IDs and missing tool responses
- Map system prompts and role semantics
- Map multimodal content parts
- Map tool/function call schemas
- Normalize thinking/reasoning fields
- Convert streaming chunks and final non-streaming responses
- Preserve source format compatibility for clients

## Provider Executor Contract

### Interface
```go
type Executor interface {
    Name() string
    Execute(ctx context.Context, req ProviderRequest) (ProviderResponse, error)
}
```

### Responsibilities
1. **Authentication**: Add provider-specific auth headers
2. **URL Building**: Build provider endpoint for model, stream mode, and account metadata
3. **Provider-Specific Headers**: Inject headers required by GitHub Copilot, Claude, Gemini, etc.
4. **Execution**: Perform HTTP request and handle cancellation
5. **Error Parsing**: Return structured provider errors for classification
6. **Streaming**: Handle SSE/chunked/native event streams
7. **Usage Extraction**: Extract provider token usage when available

### OpenAI-Compatible Executor
- Passthrough: minimal translation needed
- Auth: `Authorization: Bearer <api_key>`
- Endpoint: `POST /v1/chat/completions`
- Dynamic base URL support for arbitrary OpenAI-compatible providers

### Anthropic Executor
- Translation required:
  - Request: OpenAI Chat Completions → Anthropic Messages API
  - Response: Anthropic format → OpenAI-compatible format
- Auth: `x-api-key: <api_key>`, `anthropic-version: 2023-06-01`
- Endpoint: `POST /v1/messages`

### Dynamic Provider Types
- `openai-compatible-*`: uses OpenAI executor with configurable `base_url`
- `anthropic-compatible-*`: uses Anthropic executor with configurable `base_url`

## Configuration Model

### Ownership
- **YAML Config**: Bootstrap providers, routes, policies, server settings
- **SQLite**: Request logs, request details, usage counters, quota snapshots, API keys, aliases, custom models, account cooldowns, model locks, pricing, and UI-managed settings

### Schema (MVP)
```yaml
server:
  host: 127.0.0.1
  port: 1988
  api_key: <secret>
  request_timeout_seconds: 60
  read_timeout_seconds: 30
  write_timeout_seconds: 30
  idle_timeout_seconds: 120

logging:
  level: info
  retention_days: 14

storage:
  sqlite_path: ./data/router.db

retry:
  max_attempts: 3
  initial_backoff_ms: 100
  max_backoff_ms: 2000
  max_cooldown_ms: 300000

errors:
  text_rules:
    - text: rate limit
      backoff: true
    - text: no credentials
      cooldown_ms: 120000
  status_rules:
    - status: 429
      backoff: true
    - status: 401
      cooldown_ms: 120000

settings:
  combo_strategy: fallback
  outbound_proxy_enabled: false
  outbound_proxy_url: ""

providers:
  - name: anthropic
    type: anthropic
    base_url: https://api.anthropic.com
    api_key: ${ANTHROPIC_API_KEY}
    format: claude
    tier: subscription
    headers:
      anthropic-version: "2023-06-01"
    enabled: true

model_aliases:
  fast:
    provider: openai_compat
    model: gpt-4.1-mini

routes:
  coding-default:
    strategy: fallback
    targets:
      - provider: anthropic
        model: claude-sonnet-4-5
        tier: subscription
```

## Streaming Strategy

### MVP Constraints
1. **Commit Point**: Once first chunk sent to client, request is committed to current provider
2. **Pre-Stream Fallback**: Retry/fallback allowed before first byte sent
3. **Post-Stream Failure**: If stream fails mid-response, report error but don't fallback
4. **Cancellation**: Propagate client disconnect to upstream

### Implementation Phases
1. Non-stream support first (buffered responses)
2. Stream passthrough for OpenAI-compatible
3. Stream translation for Anthropic (if feasible)

## Observability

### Structured Logging (zerolog)
- Request lifecycle events
- Provider selection and fallback
- Error details with context
- Performance metrics (latency)
- Account cooldown and model lock decisions
- Source/target format conversion metadata
- Request detail metadata in debug mode
- Token usage normalization and cost estimation

### Metrics (Prometheus, future)
- `request_total` (by route, provider, status)
- `request_duration_seconds` (histogram)
- `fallback_total` (by route, reason)
- `provider_requests_total` (by provider, status)
- `account_cooldown_total` (by provider, reason)
- `model_lock_total` (by provider, model)
- `token_usage_total` (by provider, model)

### Health Checks
- `/healthz`: Always returns 200 (liveness)
- `/readyz`: Returns 200 only if dependencies ready (config valid, storage accessible, providers initialized)

## Security Model

### Authentication
- **Client API Keys**: Required for all `/v1/*` endpoints; multiple keys with active/inactive state
- **Admin Auth**: Separate credentials for admin operations (future)
- **Provider Auth**: API key, OAuth access/refresh token, cookie/session auth, service account auth by provider type

### Secrets Management
- Environment variable expansion in config: `${VAR_NAME}`
- No secrets in logs or error messages (redaction)
- Constant-time comparison for API keys
- OAuth refresh token storage must be encrypted or isolated behind local-only storage permissions
- Provider-specific headers must be redacted when logged

### Network Security
- Default bind: `127.0.0.1` (localhost only)
- TLS termination via reverse proxy (nginx, caddy)
- No built-in TLS (single-node deployment assumption)

## Deployment Model

### Target Environments
1. **Localhost**: Developer workstation
2. **VPS**: Small cloud instance (1-2 GB RAM)
3. **Docker**: Containerized deployment

### Resource Targets
- Idle memory: < 100 MB
- Startup time: < 2 seconds
- Binary size: < 20 MB

### Deployment Patterns
- **Systemd**: Service unit for Linux servers
- **Docker**: Single container with volume for SQLite
- **Binary**: Direct execution for development

## Testing Strategy

### Unit Tests
- Config loading and validation
- Route resolution logic
- Error classification
- Format detection and translation registry
- Provider executors (with mocks)
- Combo fallback and round-robin strategy
- Account cooldown and model lock logic

### Integration Tests
- End-to-end request flow with mock providers
- Fallback behavior under various error conditions
- Streaming and non-streaming behavior
- `/v1/models`, `/v1/messages`, `/v1/responses` compatibility
- Native passthrough behavior

### Table-Driven Tests
- Route resolution (alias, direct, invalid)
- Retry logic (retryable vs non-retryable)
- Error mapping (HTTP status → error type)
- Translator mapping (OpenAI ↔ Claude, OpenAI ↔ Gemini)
- Source format detection

## Future Considerations

### Phase 2+ Features
- Multi-account rotation per provider
- Account cooldown and model locks
- Response caching (per-tool optimization)
- `/v1/responses` endpoint (Codex CLI compatibility)
- Ollama format support (self-hosted models)
- Web UI for admin operations
- Advanced quota tracking and analytics
- Provider usage fetching and pricing/cost tracking
- Token refresh
- Outbound proxy support
- Tunnels, MITM proxy, CLI tool auto-configuration
- RTK/token compression, anti-ban cloaking, TTS, image generation, embeddings

### Scalability Limits
- Single-node design: not suitable for high-traffic multi-tenant
- SQLite: appropriate for < 1000 req/sec
- No horizontal scaling: use load balancer + multiple instances if needed

## Decision Log

### Fixed Decisions (from PRD)
- **Language**: Go (chosen over Rust for faster delivery)
- **HTTP Router**: chi (chosen over net/http only)
- **Config Format**: YAML (chosen over TOML)
- **Database**: SQLite (chosen for simplicity)
- **Logging**: zerolog (chosen for performance)
- **Admin Interface**: CLI-first (web UI deferred)

### Open Decisions
- SQLite driver: `modernc.org/sqlite` (no CGO) vs `mattn/go-sqlite3` (CGO)
- Metrics in MVP: optional or required
- CLI framework: cobra (likely) vs custom

## References

- [PRD Final](./prd-final.md) - Complete product requirements
- [AGENTS.md](../AGENTS.md) - Development guidelines for AI agents
