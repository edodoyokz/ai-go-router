# Reference Parity Implementation Plan

This document is the durable implementation record for reference parity work. The source execution split is `.kilo/plans/1777289248400-quiet-mountain.md`; this file captures the project-facing Plan A scope and handoff contract.

## Plan A: Core Gateway Runtime

Plan A owns runtime gateway contracts that Plan B consumes:

- Rich hub request/response schema in `internal/providers`.
- Model resolution order in `internal/router`: route/combo, alias, direct provider/model, catalog alias.
- Account-visible execution contract so router fallback can reason about provider/account/model state.
- Error taxonomy categories used for retry, fallback, cooldown, auth refresh, and model lock decisions.
- Hub-oriented translator contracts and compatibility wrappers.
- Streaming parser/writer helpers and chunk schema for text, tool, reasoning, usage, and finish deltas.

## Current Milestone

Plan A runtime implementation now establishes the core contracts without broad API or storage rewrites:

- Preserve text-only OpenAI chat behavior.
- Add fields needed for tools, multimodal content, OpenAI Responses input items, reasoning/thinking, structured output, metadata, and raw unknown field passthrough.
- Change route-vs-alias precedence to match reference behavior.
- Expose adapter account names so router can set `providers.AccountContextKey` explicitly.
- Keep in-memory cooldown/model lock behavior while preparing account-scoped storage integration for Plan B.
- Replace scanner-limited SSE parsing with an event-aware decoder that handles multi-line `data:` and large events.
- Normalize tool calls by generating missing IDs, validating tool result correlation, and cloaking invalid tool names.
- Translate core OpenAI Chat, Claude Messages, and OpenAI Responses request/response shapes through compatibility wrappers.
- Assemble streaming text/tool-call deltas into a non-streaming chat response representation.
- Support account-wide model locks through the in-memory cooldown tracker; SQLite persistence remains a Plan B integration point.

## Remaining Plan B Integration Points

The runtime contracts are ready for Plan B integration, but these items deliberately remain outside Plan A ownership:

- SQLite persistence and hydration for cooldown/model locks.
- API endpoint rewrites and admin/UI exposure.
- Provider catalog/factory truthfulness and provider-zoo expansion.
- OAuth token refresh wiring into concrete provider accounts.

## Plan B Handoff

Plan B should consume these contracts rather than redefining them:

- `providers.ChatRequest`, `providers.ChatMessage`, `providers.ChatChunk`, and related hub structs.
- `providers.AccountAwareAdapter` and `providers.AccountContextKey` for selected-account execution.
- Error category helpers such as `providers.IsRetryable`, `providers.IsQuotaExhausted`, `providers.IsAuthFailure`, and `providers.IsUnsupportedModel`.
- Router cooldown/model-lock calls should be wired to SQLite persistence through a small storage interface in Plan B.

## Verification

Plan A package verification:

```bash
go test ./internal/providers ./internal/router ./internal/translator
```

If API parsing is changed in a later Plan A pass, also run:

```bash
go test ./internal/api
```
