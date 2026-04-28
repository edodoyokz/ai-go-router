# Reference Parity Final Report

Updated: 2026-04-28.

## Summary

`9router-go` has passed the sequential Fase 1-10 verification gates used for the reference parity push. The current catalog contains 64 reference providers: 63 are executable/supported and 1 remains planned with a documented blocker (`ollama-local`, pending local/cloud catalog split).

## Phase Gate Results

| Phase | Area | Result | Verification |
|-------|------|--------|--------------|
| 1 | Runtime core, translators, streaming, RTK | passed | translator/providers/RTK/API translator gates |
| 2 | Codex, GitHub, Cursor, Kiro | passed | Batch A provider and OAuth/API gates |
| 3 | Gemini CLI, Vertex, Vertex Partner, Antigravity, iFlow | passed | Batch B provider/OAuth gates |
| 4 | Qwen, Grok Web, Perplexity Web | passed | deprecated/web provider gates |
| 5 | Media and search providers | passed | media/search provider, API, config/catalog gates |
| 6 | OAuth lifecycle | passed | OAuth API/storage gates and full `go test ./...` |
| 7 | Cloud backend | passed | `internal/cloud` tests and `cmd/cloud` build |
| 8 | Tunnel and MITM | passed | tunnel/MITM package tests plus API coverage |
| 9 | Web UI | passed | Vitest, Vite build, embedded webui test, Playwright smoke |
| 10 | Reports and release verification | passed after final verification | docs updated and release commands recorded |

## Provider Status

- Supported: 63/64 catalog providers.
- Planned: `ollama-local` only, because runtime enablement requires a local/cloud catalog split.
- Deprecated-safe supported providers: `qwen`, `gemini-cli`, `antigravity`.
- Cookie/session web providers `grok-web` and `perplexity-web` are supported with explicit expired-session/rate-limit errors and are not default-recommended.

## Route and Cloud Status

- Local API route parity is covered by generated reference route scanning in `internal/api/routes_test.go`.
- Cloud supports direct `/v1/*` inference routes and legacy `/{machineId}/v1/*` routes for chat, messages, responses, embeddings, count tokens, Ollama chat, verify, `/forward`, `/forward-raw`, `/testClaude`, sync, and cache clear.

## Final Verification Commands

Run from repo root unless noted:

```bash
GOCACHE=/tmp/9router-go-build-cache go test ./... -count=1
GOCACHE=/tmp/9router-go-build-cache go build ./cmd/router ./cmd/cloud
cd web && npm test
cd web && npm run build
cd web && npm run test:e2e
```

## Manual Smoke Checklist

The manual smoke flow to repeat before tagging a release:

1. Start with fresh config and empty SQLite.
2. Login to dashboard.
3. Add a provider.
4. Test provider/model.
5. Send chat request.
6. Confirm logs, usage, and request details.
7. Restart router and confirm state persists.
8. Exercise OAuth refresh with a mock/test connection.
9. Exercise media/search provider test paths.
10. Exercise tunnel/MITM through mocked command runner in tests.
11. Run cloud sync and cloud inference smoke.
12. Run CLI tool config smoke for Claude, Codex, OpenCode, Copilot, Droid, Hermes, and OpenClaw.
