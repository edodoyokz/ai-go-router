# Plan B Compatibility Surface

Plan B owns operational/API compatibility around the Plan A runtime contracts.

## Implemented Baseline

- `/metrics` remains Prometheus plaintext for monitoring.
- `/api/metrics` and `/api/metrics/json` return dashboard JSON metrics.
- `/api/nodes` returns registered peer nodes or an empty list when node sync is not configured.
- `/v1/messages/count_tokens`, `/v1/responses/compact`, `/v1beta/models`, and `/codex/*` compatibility routes are present.
- Provider factory rejects catalog entries marked `planned` or `disabled` before runtime routing.
- Provider factory supports catalog aliases for OpenAI-compatible and Anthropic-compatible executable providers.
- Startup hydrates in-memory router cooldown/model-lock state from SQLite `account_cooldowns` and `model_locks`.
- Provider connection CRUD is backed by SQLite and returns sanitized connection data that does not expose API keys or tokens.
- Proxy pool CRUD is backed by SQLite through `/api/proxy-pools`.
- OAuth token refresh has an integration helper, `oauth.EnsureFreshToken`, that loads encrypted tokens, refreshes expired tokens, persists the refreshed record, and reports whether refresh occurred.

## Completion Status

Plan B is complete for the scoped compatibility surface in `.kilo/plans/1777289248400-quiet-mountain.md`:

- Compatibility endpoints are present and covered by contract tests.
- Admin provider setup surfaces have SQLite-backed provider connection storage.
- Admin proxy pool surfaces have SQLite-backed CRUD storage.
- Provider catalog/factory behavior is explicit about supported versus planned providers.
- `opencode` and `ollama` have been promoted from planned to supported because they now have executable adapter paths.
- OAuth refresh has a reusable, tested integration point for providers that support refresh tokens.
- Runtime cooldown/model-lock hydration from SQLite is wired at startup.

## Explicit Non-Claims

- Catalog entries marked `planned` are discoverable for UI/setup but are not executable adapters.
- OAuth endpoints expose setup scaffolding; automatic refresh is available as a helper and concrete adapter use remains provider-specific.
- Dashboard polish and complete reference UI parity are intentionally separate from gateway/API compatibility.

## Verification

```bash
go test ./internal/api ./internal/providers ./internal/storage ./internal/oauth ./internal/app
go test ./...
```
