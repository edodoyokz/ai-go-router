# AGENTS.md — 9router-go

**9router-go** is a local-first AI model router/proxy/fallback gateway built in Go. It provides multi-format endpoints (OpenAI, Claude Messages, OpenAI Responses) that route requests to multiple AI providers with automatic fallback, hub-and-spoke format translation, and config-driven error classification — making it easier to manage multiple AI subscriptions and maintain stable coding sessions.

**Current status:** reference-parity implementation in progress. The project has moved beyond MVP scaffolding: core routing, dashboard APIs, SQLite usage/logging, translator preview/send, cloud compatibility, Web UI, CLI tool config, OAuth import/device flows, and several provider runtimes exist. The active goal is to reach 100% functional parity with `reference/9router`.

**Tech stack:** Go 1.24.0, chi router, zerolog, YAML config, SQLite, Vite/React/Tailwind for the embedded dashboard.

---

## Core Behavioral Principles

These principles reduce common mistakes when working with this codebase. They bias toward caution over speed—use judgment for trivial tasks.

### 1. Think Before Coding

**Don't assume provider behavior. Don't hide confusion. Surface tradeoffs.**

Before implementing:
- State your assumptions explicitly about routing, fallback, or provider behavior. If uncertain, ask.
- If multiple interpretations exist (e.g., "retry on timeout" vs "retry on any error"), present them—don't pick silently.
- If a simpler approach exists (e.g., config-driven vs hardcoded), say so. Push back when warranted.
- If something is unclear about reference parity scope or provider contracts, stop. Name what's confusing. Ask.

**Context for this project:**
- Do not assume provider behavior. Verify against `reference/9router/open-sse/**` before implementing runtime, streaming, token refresh, usage, or error/fallback behavior.
- Do not assume API response shapes. Verify against `reference/9router/src/app/api/**/route.js`.
- Do not assume cloud behavior. Verify against `reference/9router/cloud/**`.
- Do not assume OAuth/import/device/cookie lifecycle. Verify against `reference/9router/src/lib/oauth/**` and the matching API route.
- When a feature crosses the old MVP boundary, do not reject it as out of scope. The active target is reference parity; use `docs/reference-parity-roadmap-to-100.md` to determine priority and acceptance criteria.

### 2. Simplicity First

**Minimum Go code that solves the problem. Nothing speculative.**

- No features beyond what was asked.
- No abstractions for single-use code (e.g., don't create a "retry framework" for one provider).
- No "flexibility" or "configurability" that wasn't requested (e.g., don't add plugin systems unprompted).
- No error handling for impossible scenarios (e.g., don't handle "negative timeout" if config validation prevents it).
- If you write 200 lines and it could be 50, rewrite it.

**Context for this project:**
- Prefer Go standard library patterns over third-party abstractions.
- Use existing config/storage/API patterns before adding new ones.
- Keep provider adapters/executors minimal, but complete enough to match reference behavior and tests.
- Do not invent features beyond reference parity unless explicitly requested.

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
- Don't reorganize package structure (`cmd/` vs `internal/`) without explicit need.
- Don't change logging patterns across the codebase—match what's there.
- Don't refactor the config schema while adding a provider unless the reference parity contract requires a persisted field.
- Preserve dirty worktree changes. Never revert unrelated user/agent work.

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

### Active North Star: 100% Reference Parity

The current project objective is not MVP completion. It is functional 1:1 parity with `reference/9router`.

Read these first for parity work:
1. `docs/reference-parity-roadmap-to-100.md` — current roadmap from repo state to 100%.
2. `docs/reference-parity-analysis.md` — audit context and known gaps.
3. `docs/reference-route-map.md` — local API route coverage.
4. `docs/reference-provider-map.md` and `docs/provider-execution-status.md` — provider status.
5. `reference/9router/**` — final source of truth whenever docs and code disagree.

Source-of-truth map:
- Local/dashboard APIs: `reference/9router/src/app/api/**`.
- Runtime inference, executors, translators, streaming, RTK, usage, token refresh: `reference/9router/open-sse/**`.
- OAuth/import/device/cookie flows: `reference/9router/src/lib/oauth/**`.
- Cloud backend: `reference/9router/cloud/**`.
- Dashboard UI: `reference/9router/src/app/(dashboard)/dashboard/**` and `reference/9router/src/shared/**`.

Parity rules:
- A route is not complete until method, status codes, request validation, and response shape match the reference for happy path and representative invalid input.
- A provider is not `supported` until factory build, runtime call, auth lifecycle, fallback/error behavior, and mock-server tests pass.
- Deprecated providers may remain visible, but must not be promoted unless runtime behavior is safe and tests document the risk.
- If the reference depends on local/external state that is unavailable in Go, implement a documented reference-equivalent safe no-op shape instead of a generic placeholder.
- Remove temporary shell behavior once the real parity behavior lands, or document the exact blocker in `docs/`.

### Package Structure
```
cmd/router/              - CLI entrypoint and server command
cmd/cloud/               - Cloud compatibility entrypoint
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
internal/tunnel/         - Cloudflare/Tailscale tunnel support
internal/mitm/           - MITM proxy support
internal/cloud/          - Cloud backend compatibility server
web/                     - Vite/React/Tailwind dashboard source
config/                  - Example configuration files
```

**Rules:**
- Keep separation between `cmd/` (entrypoints) and `internal/` (implementation).
- New providers go in `internal/providers/` and must implement the common interface.
- Format translators go in `internal/translator/` — never in provider adapters.
- Routing logic stays in `internal/router/`—don't mix with provider code.
- Config schema changes require updating `internal/config/` and `config/config.example.yaml`.

### Reference Parity Scope

The old MVP boundary is no longer the implementation boundary. The following are in scope because the reference includes them and the roadmap requires them:

- Full local API parity for every route in `reference/9router/src/app/api/**`.
- Runtime provider parity for supported catalog providers.
- OAuth, import, device-code, cookie, service-account, and refresh flows.
- SQLite persistence for provider connections, tokens, request logs/details, quota, proxy pools, tunnel state, CLI tool state, and cloud sync state.
- Cloud compatibility binary and all reference cloud routes.
- Embedded dashboard parity using Vite/React/Tailwind.
- Tunnel and MITM operational APIs using safe command-runner abstractions.
- Media/search provider runtime where reference supports it.

Still out of scope unless explicitly requested:
- Features that do not exist in the reference.
- Plugin systems or dynamic provider loading beyond current catalog/factory patterns.
- Live third-party integration tests in CI. Use mock servers instead.

### Go Idioms for This Project

- **Error handling:** Distinguish retryable (network timeout, 429) from non-retryable (400, auth failure). Wrap errors with context using `fmt.Errorf("context: %w", err)`.
- **Logging:** Use zerolog consistently. Log at appropriate levels (Debug for trace, Info for lifecycle, Warn for retries, Error for failures).
- **Configuration:** Prefer config-driven behavior over hardcoded logic. Add YAML fields rather than environment variables or flags.
- **Interfaces:** Keep provider interface minimal. Don't add methods until multiple providers need them.
- **Testing:** Write table-driven tests for routing logic. Use mocks/stubs for provider tests.
- **Provider promotion:** Keep catalog entries `planned` until adapter/executor and tests are real. Update catalog tests when promoting.
- **OAuth secrets:** Persist tokens using existing storage patterns, redact secrets in API responses/logs, and test redaction.
- **Streaming:** Use existing SSE decoder/parser helpers; do not reintroduce line-length-limited scanners for provider streams.
- **Cloud parity:** Prefer reusing local translator/executor/token-refresh logic over creating divergent cloud-only behavior.
- **Frontend:** Keep Vite/React/Tailwind and embedded Go static serving. Match reference workflows, but follow existing local component/style patterns where equivalent.

### Common Work Areas

- **Adding a provider:** Implement interface in `internal/providers/`, add to registry in `internal/app/`, update example config.
- **Adding a format translator:** Implement in `internal/translator/request/` and `internal/translator/response/`, register in `internal/translator/registry.go`.
- **Modifying routing:** Work in `internal/router/`, test with various alias/combo configurations.
- **Changing API:** Modify handlers in `internal/api/`, ensure OpenAI/Claude/Responses compatibility.
- **Config schema:** Update structs in `internal/config/`, regenerate example YAML, document in `docs/`.
- **Error classification:** Update rules in config or `internal/providers/errors.go`.
- **CLI commands:** Extend `cmd/router/`, follow cobra patterns if using a CLI framework.
- **Cloud backend:** Work in `cmd/cloud` and `internal/cloud`; keep `/v1/*` and `/{machineId}/v1/*` behavior compatible.
- **OAuth flows:** Keep endpoint shapes aligned with `reference/9router/src/app/api/oauth/**`; use `internal/storage.ProviderConnection` for persisted account state.
- **CLI tools:** Write real tool config files under an overrideable home for tests (`NINEROUTER_CLI_HOME`), not generic JSON unless the reference has no tool-specific behavior.
- **Web UI:** Work in `web/`; generated output lands in `internal/webui/dist` only after `npm run build`.

### Provider Implementation Checklist

Before marking a provider `supported`:
1. Catalog definition has correct auth types, default URL, format, execution kind, service kinds, and deprecation notice if applicable.
2. Factory/hydration can build the adapter/executor from config and from SQLite provider connections.
3. Runtime non-streaming works against a mock server.
4. Runtime streaming works where reference supports it.
5. Auth lifecycle works: API key, OAuth/device/import/cookie/service-account as applicable.
6. Refresh-on-expiry or refresh-on-401 behavior is implemented where reference supports it.
7. Error classification triggers retry/fallback/cooldown consistently.
8. Usage/quota behavior is implemented or returns a structured local-log fallback without breaking dashboard calls.
9. Tests cover success, invalid input, auth failure, retryable failure, and redaction.
10. Docs are updated in `docs/provider-execution-status.md` and related parity maps.

### API Parity Checklist

For any route copied from the reference:
1. Inspect the exact `route.js`.
2. Match HTTP methods and path registration.
3. Match request body/query validation for required fields.
4. Match success response shape.
5. Match representative error status/body shape.
6. Add or update route parity tests.
7. Add focused handler tests for non-trivial behavior.

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
├── reference-parity-roadmap-to-100.md - Current parity roadmap (READ THIS FIRST for parity work)
├── prd-final.md          - Product requirements document
├── architecture.md       - System design and architecture
├── api-reference.md      - API endpoint documentation
├── provider-guide.md     - How to add new providers
├── provider-execution-status.md - Supported/planned provider status
├── reference-route-map.md - Reference API route coverage
├── reference-provider-map.md - Reference provider coverage
└── deployment.md         - Deployment and operations guide
```

**Why this matters:**
- Consistency—one location for all documentation regardless of tool used.
- Discoverability—users know where to find information.
- Maintainability—easier to keep docs in sync with code.

**When working on this project:**
- Read `docs/reference-parity-roadmap-to-100.md` first for current parity goals.
- Read `docs/prd-final.md` for product requirements and context when touching core routing/product behavior.
- If you create documentation, put it in `docs/`.
- Don't duplicate long content between `AGENTS.md`, `README.md`, and `docs/`—link instead.
- If you promote/demote provider support, update provider docs in the same change.
- If you add/remove API routes, update route parity docs/tests in the same change.

---

## Quick Start for Agents

```bash
# Recommended verification cache location
export GOCACHE=/tmp/9router-go-build-cache

# Run the server
go run ./cmd/router serve --config ./config/config.example.yaml

# Test health endpoint
curl -s http://127.0.0.1:1988/healthz

# Test chat completions endpoint (once providers are implemented)
curl -s http://127.0.0.1:1988/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4","messages":[{"role":"user","content":"test"}]}'
```

Common verification commands:

```bash
GOCACHE=/tmp/9router-go-build-cache go test ./...
GOCACHE=/tmp/9router-go-build-cache go build ./cmd/router ./cmd/cloud
npm run build --prefix web
```

Some tests use `httptest.NewServer`; sandboxed environments may require permission to open local listener sockets. If a test fails with `listen ... operation not permitted`, rerun the same `go test` with the appropriate approval rather than changing the test.

---

**These guidelines are working if:** parity gaps shrink, supported providers are truthful, tests prove runtime behavior, and implementation work can continue one slice at a time without re-auditing the whole repo.

**Last updated:** 2026-04-27
