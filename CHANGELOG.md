# Changelog

All notable changes to 9router-go will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- SSE streaming support for OpenAI and Anthropic providers (Phase 2.4)
  - OpenAI streaming passthrough with SSE scanner
  - Anthropic stream to OpenAI stream translation
  - API layer streaming handler with chunk forwarding
  - Client disconnect propagation via context cancellation
- CORS configuration middleware with origin allowlist (Phase 5.3.6)
  - Configurable allowed origins, methods, headers
  - Support for credentials and max-age
  - Preflight OPTIONS request handling
- GET /api/logs endpoint with pagination and filtering (Phase 2.2.8)
  - Filter by provider, model, status, time range
  - Pagination with limit and offset
- CLI logs command (Phase 1.8.6)
  - `./router logs` with filtering options
  - Follow mode for real-time log tailing
  - `--limit`, `--provider`, `--model`, `--status`, `--follow` flags
- Account health check endpoint (Phase 2.1.5)
  - GET /api/providers/{name}/accounts/{account}/health
  - Validates provider and account configuration
- Test coverage for api and storage packages
  - 8 API tests (health, logs, metrics, CORS)
  - 7 storage tests (LogRequest, QueryLogs, IncrementUsage)
- API reference documentation (docs/api-reference.md)
- Deployment guide (docs/deployment.md)

### Changed
- Refactored provider adapters to use shared AccountSelector
  - Removed duplicate account selection logic
  - Consolidated SSE scanner helper to providers.go
- Updated config schema to include CORS configuration
- Updated example config with CORS section

### Fixed
- Provider registry access in streaming handler
  - Added GetRegistry() method to Engine
- SSE error handling in streaming responses
  - Added writeSSEError helper function

## [0.1.0] - 2026-04-26

### Added
- Initial MVP release
- Core routing engine with alias resolution
- Provider abstraction and adapters (OpenAI, Anthropic, OpenRouter)
- Hub-and-spoke format translation registry
- Multi-format endpoints (/v1/chat/completions, /v1/messages, /v1/responses)
- Config-driven error classification with backoff
- Dynamic OpenAI-compatible / Anthropic-compatible provider types
- SQLite persistence for request logs, usage counters, cooldowns
- Structured logging with zerolog
- CLI admin (serve, version, validate, providers, routes)
- Docker and systemd deployment support
- Security headers middleware
- Graceful shutdown with 10s timeout
- Request body size limits (1MB)
- SQLite WAL mode for concurrency
- Secret redaction in logs
- Metrics endpoint (/metrics) in Prometheus format
- Admin API endpoints (GET /api/providers, /api/combos, /api/keys, etc.)
- Provider health check endpoint
- Usage summary endpoint

### Provider Support
- OpenAI (openai_compat type)
- Anthropic (anthropic type)
- Anthropic-compatible (anthropic_compat type)
- OpenRouter (openrouter type)
- DeepSeek (via openai_compat type)

### Configuration
- YAML-based configuration
- Multi-account support with round-robin selection
- Route strategies: tier, round-robin, priority
- Model aliases
- Error classification rules
- Retry configuration
