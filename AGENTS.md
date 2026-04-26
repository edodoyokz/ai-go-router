# AGENTS.md — 9router-go

**9router-go** is a local-first AI model router/proxy/fallback gateway built in Go. It provides multi-format endpoints (OpenAI, Claude Messages, OpenAI Responses) that route requests to multiple AI providers with automatic fallback, hub-and-spoke format translation, and config-driven error classification — making it easier to manage multiple AI subscriptions and maintain stable coding sessions.

**Current status:** MVP scaffold with core infrastructure (HTTP server, config loader, provider abstraction with OpenAI + Anthropic adapters, routing with tier-based fallback, error taxonomy, structured logging).

**Tech stack:** Go 1.24.0, chi router, zerolog, YAML config.

---

## Core Behavioral Principles

These principles reduce common mistakes when working with this codebase. They bias toward caution over speed—use judgment for trivial tasks.

### 1. Think Before Coding

**Don't assume provider behavior. Don't hide confusion. Surface tradeoffs.**

Before implementing:
- State your assumptions explicitly about routing, fallback, or provider behavior. If uncertain, ask.
- If multiple interpretations exist (e.g., "retry on timeout" vs "retry on any error"), present them—don't pick silently.
- If a simpler approach exists (e.g., config-driven vs hardcoded), say so. Push back when warranted.
- If something is unclear about the MVP scope or provider contracts, stop. Name what's confusing. Ask.

**Context for this project:**
- Don't assume how providers handle streaming, rate limits, or error codes—verify or ask.
- Don't assume fallback behavior—check `docs/prd-final.md` for requirements.
- If a feature feels out of MVP scope, flag it before implementing.

### 2. Simplicity First

**Minimum Go code that solves the problem. Nothing speculative.**

- No features beyond what was asked.
- No abstractions for single-use code (e.g., don't create a "retry framework" for one provider).
- No "flexibility" or "configurability" that wasn't requested (e.g., don't add plugin systems unprompted).
- No error handling for impossible scenarios (e.g., don't handle "negative timeout" if config validation prevents it).
- If you write 200 lines and it could be 50, rewrite it.

**Context for this project:**
- Prefer Go standard library patterns over third-party abstractions.
- Use YAML config for behavior changes, not code changes.
- Keep provider adapters minimal—implement only the interface contract.
- Don't add observability/metrics/tracing unless explicitly requested.

Ask yourself: "Would a senior Go engineer say this is overcomplicated?" If yes, simplify.

### 3. Surgical Changes

**Touch only what you must. Clean up only your own mess.**

When editing existing code:
- Don't "improve" adjacent code, comments, or formatting in other packages.
- Don't refactor `internal/router/` while working on `internal/providers/`.
- Match existing style (zerolog patterns, error wrapping, package organization).
- If you notice unrelated dead code, mention it—don't delete it.

When your changes create orphans:
- Remove imports/variables/functions that YOUR changes made unused.
- Don't remove pre-existing dead code unless asked.

**Context for this project:**
- Don't reorganize package structure (`cmd/` vs `internal/`) without explicit request.
- Don't change logging patterns across the codebase—match what's there.
- Don't refactor the config schema while adding a provider.

The test: Every changed line should trace directly to the user's request.

### 4. Goal-Driven Execution

**Define success criteria. Loop until verified.**

Transform tasks into verifiable goals:
- "Add OpenAI provider" → "Implement interface, test with curl to `/v1/chat/completions`, verify response format"
- "Fix fallback bug" → "Write test that reproduces it, then make it pass"
- "Add retry logic" → "Test with failing provider, verify retry count and backoff timing"

For multi-step tasks, state a brief plan:
```
1. Implement provider interface → verify: compiles, implements all methods
2. Add config schema → verify: example YAML loads without error
3. Wire into registry → verify: endpoint routes to new provider
4. Test end-to-end → verify: curl returns expected response
```

**Verification strategies for this project:**
- **Code changes:** `go test ./...` or `go build ./cmd/router`
- **Server changes:** Run server, test with `curl` to relevant endpoint
- **Config changes:** Load example config, verify no parse errors
- **Provider changes:** Test actual API call or use mock/stub

Don't run irrelevant verification (e.g., don't test all providers when changing logging format).

Strong success criteria let you loop independently. Weak criteria ("make it work") require constant clarification.

---

## Project-Specific Guidelines

### Package Structure
```
cmd/router/              - CLI entrypoint and server command
internal/api/            - HTTP API layer (handlers, middleware)
internal/router/         - Routing engine (alias resolution, combo, fallback chain)
internal/providers/      - Provider abstraction and adapters (OpenAI, Anthropic, etc.)
internal/providers/executors/ - Provider-specific execution behavior
internal/translator/     - Hub-and-spoke format translation registry
internal/models/         - Model aliases, custom models, discovery
internal/usage/          - Usage, quota, pricing, cost tracking
internal/config/         - YAML config loader and validation
internal/app/            - Application wiring and dependency injection
internal/storage/        - SQLite persistence
internal/tunnel/         - Cloudflare/Tailscale tunnel support (future)
internal/mitm/           - MITM proxy support (future)
config/                  - Example configuration files
```

**Rules:**
- Keep separation between `cmd/` (entrypoints) and `internal/` (implementation).
- New providers go in `internal/providers/` and must implement the common interface.
- Format translators go in `internal/translator/` — never in provider adapters.
- Routing logic stays in `internal/router/`—don't mix with provider code.
- Config schema changes require updating `internal/config/` and `config/config.example.yaml`.

### MVP Scope Boundaries

**In scope (Phase 1 MVP — see `docs/prd-final.md` Section 17):**
- Core routing with alias resolution, combo fallback + round-robin
- Provider abstraction and adapters (OpenAI, Anthropic)
- Hub-and-spoke format translation (source → OpenAI → target)
- Multi-format endpoints (`/v1/chat/completions`, `/v1/messages`, `/v1/models`)
- Config-driven error classification with backoff
- Dynamic OpenAI-compatible / Anthropic-compatible provider types
- Non-streaming response handling
- Fallback chain with retry policies and tier-based ordering
- YAML configuration
- SQLite persistence
- Structured logging
- CLI admin basic

**Out of scope (unless explicitly requested):**
- Metrics/Prometheus beyond logging
- Web UI or dashboard
- Plugin systems or dynamic provider loading
- Multi-account rotation (Phase 2)
- OAuth flows (Phase 4)

If a feature feels like it crosses the MVP boundary, ask before implementing.

### Go Idioms for This Project

- **Error handling:** Distinguish retryable (network timeout, 429) from non-retryable (400, auth failure). Wrap errors with context using `fmt.Errorf("context: %w", err)`.
- **Logging:** Use zerolog consistently. Log at appropriate levels (Debug for trace, Info for lifecycle, Warn for retries, Error for failures).
- **Configuration:** Prefer config-driven behavior over hardcoded logic. Add YAML fields rather than environment variables or flags.
- **Interfaces:** Keep provider interface minimal. Don't add methods until multiple providers need them.
- **Testing:** Write table-driven tests for routing logic. Use mocks/stubs for provider tests.

### Common Work Areas

- **Adding a provider:** Implement interface in `internal/providers/`, add to registry in `internal/app/`, update example config.
- **Adding a format translator:** Implement in `internal/translator/request/` and `internal/translator/response/`, register in `internal/translator/registry.go`.
- **Modifying routing:** Work in `internal/router/`, test with various alias/combo configurations.
- **Changing API:** Modify handlers in `internal/api/`, ensure OpenAI/Claude/Responses compatibility.
- **Config schema:** Update structs in `internal/config/`, regenerate example YAML, document in `docs/`.
- **Error classification:** Update rules in config or `internal/providers/errors.go`.
- **CLI commands:** Extend `cmd/router/`, follow cobra patterns if using a CLI framework.

---

## Documentation Policy

**All project documentation must be centralized in `docs/`.**

### Rules
1. **New documentation** — always create in `docs/`
2. **Substantive updates** — move content to `docs/` if it grows beyond a brief overview
3. **Root exceptions** — only files required by tooling or repository conventions stay in root:
   - `AGENTS.md` (this file—for AI agent context)
   - `README.md` (brief entrypoint that points to `docs/`)
   - Standard repo files (`LICENSE`, `.gitignore`, `go.mod`, etc.)

### Documentation Structure
```
docs/
├── prd-final.md          - Product requirements document (READ THIS FIRST)
├── architecture.md       - System design and architecture
├── api-reference.md      - API endpoint documentation (future)
├── provider-guide.md     - How to add new providers (future)
└── deployment.md         - Deployment and operations guide (future)
```

**Why this matters:**
- Consistency—one location for all documentation regardless of tool used.
- Discoverability—users know where to find information.
- Maintainability—easier to keep docs in sync with code.

**When working on this project:**
- Read `docs/prd-final.md` for complete product requirements and context.
- If you create documentation, put it in `docs/`.
- Don't duplicate content between `AGENTS.md`, `README.md`, and `docs/`—link instead.

---

## Quick Start for Agents

```bash
# Install dependencies
go mod tidy

# Run the server
go run ./cmd/router serve --config ./config/config.example.yaml

# Test health endpoint
curl -s http://127.0.0.1:20128/healthz

# Test chat completions endpoint (once providers are implemented)
curl -s http://127.0.0.1:20128/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4","messages":[{"role":"user","content":"test"}]}'
```

---

**These guidelines are working if:** fewer unnecessary changes in diffs, fewer rewrites due to overcomplication, and clarifying questions come before implementation rather than after mistakes.

**Last updated:** 2026-04-26
