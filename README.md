# 9router-go

Lean local AI router in Go — a single OpenAI-compatible endpoint with automatic fallback across multiple AI providers.

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
- HTTP server with health endpoints
- OpenAI-compatible `/v1/chat/completions` endpoint
- YAML configuration loader
- Provider abstraction layer
- Route alias resolution
- Fallback chain skeleton (stub)

## Documentation

Complete documentation is available in the [`docs/`](docs/) folder:

- **[Product Requirements](docs/prd-final.md)** — full product vision, architecture, and roadmap
- **[Agent Instructions](AGENTS.md)** — for AI agents working on this codebase

## Tech Stack

- Go 1.24.0
- chi router
- zerolog
- SQLite (planned)

## Next Steps

- Implement real OpenAI-compatible provider adapter
- Implement real Anthropic provider adapter
- Complete fallback chain logic with retry policies
- Add SQLite persistence layer
