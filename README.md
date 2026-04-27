# NusaNexus Router

[![CI](https://github.com/edodoyokz/ai-go-router/actions/workflows/ci.yml/badge.svg)](https://github.com/edodoyokz/ai-go-router/actions/workflows/ci.yml)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go)](https://go.dev/)

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

- **OpenAI-compatible endpoint** — drop-in endpoint untuk Cursor, Cline, Codex, Continue, dan tool lainnya
- **Automatic fallback** — routing berbasis tier dengan retry dan exponential backoff
- **Format translation** — konversi OpenAI ↔ Anthropic ↔ Gemini ↔ Ollama secara otomatis
- **Alias routing** — map nama sederhana seperti `fast` atau `smart` ke provider/model tertentu
- **Provider alias shorthand** — `cc/model` → Anthropic, `ds/model` → DeepSeek, `oai/model` → OpenAI
- **Multi-account rotation** — round-robin antar API key per provider
- **Proxy pools** — multiple outbound proxy per provider dengan rotasi otomatis
- **OAuth token storage** — encrypted AES-GCM token storage di SQLite
- **MITM proxy** — intercept AI requests dari tool lain dengan cloaking Claude/Antigravity
- **Tunnel support** — expose gateway via Cloudflare Tunnel atau Tailscale Funnel
- **Policy engine** — allow/deny/reroute/tag rules berbasis model, provider, atau API key
- **Provider nodes** — distributed mesh antar instance 9router dengan health checks
- **Cloud sync** — backup/restore database ke S3/GCS/HTTPS endpoint
- **In-app updater** — self-update binary dari GitHub releases
- **Web UI** — dashboard React/Vite/Tailwind embedded di binary, 10 halaman
- **i18n** — dukungan bahasa EN/ID/ZH/JA
- **Config-driven** — konfigurasi YAML dengan environment variable expansion
- **Local-first** — API keys tetap di mesin lokal atau server yang kamu kontrol
- **Single binary** — ringan untuk laptop, homelab, atau VPS kecil

## Quick Start

**Requirements:** Go 1.24+, Node.js 18+ (for Web UI build)

If you want the fastest path, start with the example config and one or two providers first, then expand routes, aliases, proxies, or policy rules later.

```bash
# Clone and install dependencies
git clone https://github.com/edodoyokz/ai-go-router.git
cd ai-go-router
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

**Production-ready — 237/239 tasks complete.** Semua fitur inti dan advanced platform sudah diimplementasi:

| Kategori | Status |
|---|---|
| Multi-format API (`/v1/chat/completions`, `/v1/messages`, `/v1/responses`, `/v1/embeddings`) | ✅ |
| Provider adapters (OpenAI, Anthropic, Gemini, Ollama, DeepSeek, Groq, Mistral, dll) | ✅ |
| Format translation (OpenAI ↔ Anthropic ↔ Gemini ↔ Ollama) | ✅ |
| Tier-based fallback + retry + circuit breaker + cooldown | ✅ |
| Multi-account rotation + proxy pools | ✅ |
| OAuth token storage (AES-GCM encrypted) | ✅ |
| MITM proxy + Claude/Antigravity cloaking | ✅ |
| Tunnel (Cloudflare + Tailscale) | ✅ |
| Policy engine (allow/deny/reroute/tag) | ✅ |
| Provider nodes (distributed mesh) | ✅ |
| Cloud sync (S3/GCS/HTTPS) | ✅ |
| In-app binary updater | ✅ |
| Web UI (10 halaman, embedded) | ✅ |
| i18n (EN/ID/ZH/JA) | ✅ |
| Cost tracking + quota snapshots | ✅ |
| Log rotation built-in | ✅ |
| Admin CRUD API + hot-reload config | ✅ |
| CLI: `serve`, `setup`, `update`, `validate`, `providers`, `routes`, `logs` | ✅ |

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
- **[API Reference](docs/api-reference.md)** — endpoint documentation
- **[Provider Guide](docs/provider-guide.md)** — adding and configuring providers
- **[Deployment Guide](docs/deployment.md)** — Docker, systemd, TLS, cloud deployment
- **[Local Run Guide](docs/local-run-guide.md)** — local setup and development workflow
- **[Implementation Plan](docs/implementation-plan.md)** — task tracking dan progress (237/239 ✅)
- **[Agent Instructions](AGENTS.md)** — guidance for AI agents contributing to this codebase

## Tech Stack

**Backend:**
- **Go 1.24.0** — fast, lightweight, single-binary deployment
- **chi router** — HTTP routing
- **zerolog** — structured logging
- **YAML** — human-friendly configuration
- **SQLite (mattn/go-sqlite3)** — request logs, usage counters, quota snapshots, OAuth tokens

**Web UI:**
- **React 18 + Vite** — SPA frontend
- **Tailwind CSS** — utility-first styling
- **React Router** — client-side routing
- **Embedded via `go:embed`** — served at `/ui/` dari binary yang sama

## Build

```bash
# Build binary + Web UI (requires Node.js)
make build

# Build binary only (uses placeholder UI)
make build-go

# Build Web UI only
make build-ui

# Run tests
make test
```

## CLI Commands

```bash
9router serve --config ./config/config.yaml    # Start gateway
9router setup                                   # Auto-configure Cursor, Continue, Claude, OpenAI CLI
9router update                                  # Self-update binary from GitHub releases
9router validate --config ./config.yaml        # Validate config
9router providers --config ./config.yaml       # List configured providers
9router routes --config ./config.yaml          # List routes and aliases
9router logs --config ./config.yaml            # Tail request logs
9router version                                # Show version info
```

## Web UI

Setelah server berjalan, buka browser di:

```
http://127.0.0.1:20128/ui/
```

Halaman tersedia: Dashboard, Providers, Routes, Models, Logs, Metrics, Usage, Pricing, OAuth Tokens, Settings.

See [`docs/prd-final.md`](docs/prd-final.md) for complete product requirements.

## Project Lineage

NusaNexus Router is inspired by `9router` and currently keeps parts of the original `9router-go` implementation structure while the codebase is being rebranded. Some internal module paths, binary names, or deployment examples may still reference `9router` during the transition.

## Community

- [`CONTRIBUTING.md`](CONTRIBUTING.md) — how to build, test, and contribute
- [`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md) — expected behavior in project spaces
- [`SECURITY.md`](SECURITY.md) — how to report vulnerabilities responsibly

## Contributing

Contributions are welcome. This project is in active development and should stay focused on being a lightweight local-first AI router.

- **Discuss changes first** — open an issue or proposal for larger changes
- **Read the architecture** — start with [`docs/architecture.md`](docs/architecture.md)
- **Keep changes focused** — avoid unrelated refactors
- **Test behavior** — verify routing, fallback, and provider changes
- **Follow Go idioms** — keep implementation simple and maintainable

See also:
- [`CONTRIBUTING.md`](CONTRIBUTING.md) — development workflow and contribution guide
- [`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md) — expected community behavior
- [`SECURITY.md`](SECURITY.md) — how to report vulnerabilities responsibly

## License

This project is licensed under the **Apache License 2.0**.
See the [LICENSE](LICENSE) file for details.
