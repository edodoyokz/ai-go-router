# 9router-go

Lean local AI router in Go — multi-format AI endpoints (OpenAI, Claude, Responses) with automatic fallback across multiple providers, hub-and-spoke format translation, and config-driven error classification.

## Quick Start

```bash
# Install dependencies
go mod tidy

# Run the server
go run ./cmd/router serve --config ./config/config.example.yaml

# Test health endpoint
curl -s http://127.0.0.1:20128/healthz
```

## Current Status

MVP scaffold with core infrastructure in place:
- HTTP server with health endpoints (`/healthz`, `/readyz`)
- OpenAI-compatible `/v1/chat/completions` endpoint
- Anthropic adapter with OpenAI ↔ Claude format translation
- OpenAI-compatible passthrough adapter
- YAML configuration loader with env-var expansion
- Provider abstraction layer with error taxonomy
- Route alias resolution with tier-based ordering
- Fallback chain with exponential backoff retry

## Documentation

Complete documentation is available in the [`docs/`](docs/) folder:

- **[Product Requirements](docs/prd-final.md)** — full product vision, roadmap, and reference parity checklist
- **[Architecture](docs/architecture.md)** — system design, translation hub, executor contracts, config schema
- **[Agent Instructions](AGENTS.md)** — for AI agents working on this codebase

## Tech Stack

- Go 1.24.0
- chi router
- zerolog
- SQLite (planned)
- YAML config

## Next Steps (Phase 1 MVP)

- Hub-and-spoke translator registry with source format detection
- `/v1/models` endpoint for model discovery
- `/v1/messages` endpoint (Claude Messages passthrough)
- Round-robin combo strategy
- Config-driven error classification with text/status rules
- Dynamic OpenAI-compatible / Anthropic-compatible provider types
- Non-streaming response handling
- SQLite persistence layer
- CLI admin commands
