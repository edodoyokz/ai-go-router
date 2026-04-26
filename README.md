# NusaNexus Router

**Local-first AI model router and fallback gateway** — satu endpoint stabil untuk menghubungkan AI coding tools ke banyak provider, dengan fallback otomatis, routing berbasis alias, dan translasi format request/response.

NusaNexus Router adalah rebrand dari eksperimen `9router-go` dan terinspirasi oleh konsep **9router**: membuat gateway lokal yang ringan, config-driven, dan aman untuk sesi coding panjang yang bergantung pada beberapa provider AI.

## Why It Exists

AI coding tools seperti Cursor, Cline, Codex, dan OpenClaw biasanya hanya membutuhkan satu endpoint yang stabil. Masalahnya, penggunaan banyak provider secara manual sering menambah friction:

- **Rate limit menghentikan sesi** — workflow coding berhenti saat quota atau limit provider tercapai
- **Switching provider merepotkan** — setiap tool perlu dikonfigurasi ulang
- **Subscription tidak optimal** — model berlangganan bisa tidak terpakai sementara fallback berbayar tetap dipakai
- **Visibilitas terbatas** — sulit tahu provider/model mana yang benar-benar menangani request

NusaNexus Router menyelesaikan ini dengan bertindak sebagai gateway lokal yang memilih provider/model terbaik, melakukan fallback otomatis saat terjadi error, dan menerjemahkan format OpenAI ↔ Anthropic dari satu binary ringan.

## Key Features

- **OpenAI-compatible endpoint** — drop-in endpoint untuk tool yang mendukung OpenAI API
- **Automatic fallback** — routing berbasis tier dengan retry dan exponential backoff
- **Format translation** — konversi pesan OpenAI ↔ Anthropic secara otomatis
- **Alias routing** — map nama sederhana seperti `fast` atau `smart` ke provider/model tertentu
- **Config-driven** — konfigurasi YAML dengan environment variable expansion
- **Local-first** — API keys tetap di mesin lokal atau server yang kamu kontrol
- **Single binary** — ringan untuk laptop, homelab, atau VPS kecil

## Quick Start

**Requirements:** Go 1.24+

```bash
# Clone and install dependencies
git clone https://github.com/yourusername/nusanexus-router.git
cd nusanexus-router
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
  -H "Authorization: Bearer sk_local_dev_nusanexus" \
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
  api_key: sk_local_dev_nusanexus

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
2. **Direct routing** — `model: "anthropic/claude-sonnet-4-5"` routes directly to a provider/model pair
3. **Fallback chain** — on retryable error, tries the next configured tier
4. **Retry logic** — exponential backoff for rate limits and transient provider failures
5. **Format translation** — automatically converts between OpenAI and Anthropic request/response formats

For detailed architecture, see [`docs/architecture.md`](docs/architecture.md).

## Current Status

**Working now:**

- **OpenAI-compatible API** — `/v1/chat/completions`
- **Anthropic adapter** — provider integration with format translation
- **OpenAI passthrough adapter** — direct OpenAI-compatible forwarding
- **Tier-based fallback** — retry logic and provider failover
- **Alias and combo route resolution** — friendly model names and route chains
- **YAML configuration** — env-var expansion for secrets
- **Error taxonomy** — retryable vs non-retryable provider errors
- **Structured logging** — zerolog-based runtime logs
- **Multi-format endpoints** — `/v1/models`, `/v1/messages`, `/v1/responses`
- **SQLite persistence** — request logs and usage tracking
- **Streaming support** — lower-latency tool responses with fallback
- **Admin CLI commands** — inspect providers, routes, models, and logs

This project is still an MVP. Core routing and fallback are in place, with observability, persistence, and admin features continuously evolving.

## Use Cases

- **One endpoint for coding tools** — configure Cursor, Cline, Codex, or similar tools once
- **Provider fallback for long sessions** — continue coding when one provider hits quota or transient errors
- **Subscription-first routing** — prioritize models you already pay for, then fallback when needed
- **Self-hosted gateway** — run locally or on a lightweight VPS
- **Local secret control** — keep provider API keys out of individual tools

## Documentation

Complete technical documentation is in the [`docs/`](docs/) folder:

- **[Product Requirements](docs/prd-final.md)** — product vision, use cases, and roadmap
- **[Architecture](docs/architecture.md)** — system design, routing engine, and config schema
- **[Configuration Reference](docs/config-reference.md)** — YAML options and examples
- **[Local Run Guide](docs/local-run-guide.md)** — local setup and development workflow
- **[Agent Instructions](AGENTS.md)** — guidance for AI agents contributing to this codebase

## Tech Stack

- **Go 1.24.0** — fast, lightweight, single-binary deployment
- **chi router** — HTTP routing
- **zerolog** — structured logging
- **YAML** — human-friendly configuration
- **SQLite** — local persistence target for usage/quota data

## Roadmap

**Phase 1: Core Gateway**

- **Core routing and fallback** — implemented
- **OpenAI + Anthropic adapters** — implemented
- **Format translation** — implemented

**Phase 2: Runtime Operations**

- **Additional endpoints** — `/v1/models`, `/v1/messages`
- **SQLite persistence** — usage and quota tracking
- **Usage and cost tracking** — provider/model accounting
- **Streaming support** — lower-latency tool responses

**Phase 3: Admin and Expansion**

- **Admin CLI commands** — inspect providers, routes, and logs
- **Enhanced observability** — better operational visibility
- **Multi-account rotation** — account-level routing and quotas
- **Additional provider adapters** — more OpenAI-compatible and native providers

See [`docs/prd-final.md`](docs/prd-final.md) for complete roadmap.

## Project Lineage

NusaNexus Router is inspired by `9router` and currently keeps parts of the original `9router-go` implementation structure while the codebase is being rebranded. Some internal module paths, binary names, or deployment examples may still reference `9router` during the transition.

## Contributing

Contributions are welcome. This project is in active development and should stay focused on being a lightweight local-first AI router.

- **Discuss changes first** — open an issue or proposal for larger changes
- **Read the architecture** — start with [`docs/architecture.md`](docs/architecture.md)
- **Keep changes focused** — avoid unrelated refactors
- **Test behavior** — verify routing, fallback, and provider changes
- **Follow Go idioms** — keep implementation simple and maintainable

## License

License: TBD
