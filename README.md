# 9router-go

**Local-first AI model router with automatic fallback** — route your AI coding tools through a single stable endpoint that handles provider failures, quota limits, and format translation automatically.

## Why It Exists

Most AI coding tools (Cursor, Cline, Codex, OpenClaw) only need one thing: a stable endpoint. But managing multiple AI providers manually creates friction:

- Coding sessions stop when you hit rate limits or quota
- Switching providers means reconfiguring every tool
- Subscription models go unused while you pay for expensive fallbacks
- No visibility into which provider actually handled your request

9router-go solves this by acting as a local gateway that routes requests to the best available provider, falls back automatically on errors, and translates between OpenAI and Anthropic formats — all from a single lightweight binary.

## Key Features

- **OpenAI-compatible endpoint** — drop-in replacement for `api.openai.com`
- **Automatic fallback** — tier-based routing with retry and exponential backoff
- **Format translation** — seamless OpenAI ↔ Anthropic message conversion
- **Alias routing** — map friendly names like `fast` or `smart` to provider/model combos
- **Config-driven** — YAML configuration with environment variable expansion
- **Single binary** — no runtime dependencies, runs on laptop or VPS
- **Lightweight** — Go-based, minimal memory footprint for long-running daemon use

## Quick Start

**Requirements:** Go 1.24+

```bash
# Clone and install dependencies
git clone https://github.com/yourusername/9router-go.git
cd 9router-go
go mod tidy

# Set up provider API keys
export ANTHROPIC_API_KEY="your-key-here"
export OPENAI_API_KEY="your-key-here"

# Run the server
go run ./cmd/router serve --config ./config/config.example.yaml

# Test health endpoint
curl http://127.0.0.1:20128/healthz

# Test chat completions
curl http://127.0.0.1:20128/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk_local_dev_9router_go" \
  -d '{
    "model": "fast",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

## Example Configuration

```yaml
server:
  host: 127.0.0.1
  port: 20128
  api_key: sk_local_dev_9router_go

# Define friendly aliases
model_aliases:
  fast:
    provider: openai_compat
    model: gpt-4.1-mini
  smart:
    provider: anthropic
    model: claude-sonnet-4-5

# Define fallback chains
routes:
  coding-default:
    strategy: fallback
    targets:
      - provider: anthropic
        model: claude-sonnet-4-5
        tier: primary
      - provider: openai_compat
        model: gpt-4.1-mini
        tier: secondary

# Configure providers
providers:
  - name: anthropic
    type: anthropic
    base_url: https://api.anthropic.com
    api_key: ${ANTHROPIC_API_KEY}
    tier: primary
    
  - name: openai_compat
    type: openai_compat
    base_url: https://api.openai.com/v1
    api_key: ${OPENAI_API_KEY}
    tier: secondary
```

See [`config/config.example.yaml`](config/config.example.yaml) for complete configuration options.

## How Routing Works

1. **Alias resolution** — `model: "fast"` resolves to configured provider/model
2. **Direct routing** — `model: "anthropic/claude-sonnet-4-5"` routes directly
3. **Fallback chain** — on error, tries next tier (primary → secondary → tertiary)
4. **Retry logic** — exponential backoff for rate limits and transient errors
5. **Format translation** — automatically converts between OpenAI and Anthropic formats

For detailed architecture, see [`docs/architecture.md`](docs/architecture.md).

## Current Status

**Working now:**
- ✅ OpenAI-compatible `/v1/chat/completions` endpoint
- ✅ Anthropic adapter with format translation
- ✅ OpenAI passthrough adapter
- ✅ Tier-based fallback with retry logic
- ✅ Alias and combo route resolution
- ✅ YAML config with env-var expansion
- ✅ Error taxonomy and classification
- ✅ Structured logging with zerolog

**Planned next:**
- `/v1/models` endpoint for model discovery
- `/v1/messages` endpoint (Claude Messages API)
- Round-robin combo strategy
- SQLite persistence for usage tracking
- Streaming response support
- CLI admin commands

This is an MVP — core routing and fallback work, but observability and admin features are still in progress.

## Use Cases

- **Route coding tools to one endpoint** — configure Cursor, Cline, or Codex once, switch providers in config
- **Prioritize subscription models** — use your Claude subscription first, fallback to OpenAI when quota runs out
- **Self-host on lightweight VPS** — single binary runs on minimal resources
- **Local development** — keep API keys local, avoid embedding them in multiple tools

## Documentation

Complete technical documentation is in the [`docs/`](docs/) folder:

- **[Product Requirements](docs/prd-final.md)** — product vision, use cases, and roadmap
- **[Architecture](docs/architecture.md)** — system design, routing engine, and config schema
- **[Agent Instructions](AGENTS.md)** — for AI agents contributing to this codebase

## Tech Stack

- **Go 1.24.0** — fast, lightweight, single-binary deployment
- **chi router** — HTTP routing
- **zerolog** — structured logging
- **YAML** — human-friendly configuration

## Roadmap

**Phase 1 (Current):**
- Core routing and fallback ✅
- OpenAI + Anthropic adapters ✅
- Format translation ✅

**Phase 2:**
- Additional endpoints (`/v1/models`, `/v1/messages`)
- SQLite persistence
- Usage and cost tracking
- Streaming support

**Phase 3:**
- Admin CLI commands
- Enhanced observability
- Multi-account rotation
- Additional provider adapters

See [`docs/prd-final.md`](docs/prd-final.md) for complete roadmap.

## Contributing

Contributions welcome! This project is in active development.

- Check existing issues or open a new one to discuss changes
- Read [`docs/architecture.md`](docs/architecture.md) and [`AGENTS.md`](AGENTS.md) for codebase context
- Keep changes focused and well-tested
- Follow existing Go idioms and project structure

## License

License: TBD
