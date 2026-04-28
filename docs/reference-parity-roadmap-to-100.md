# Reference Parity Roadmap to 100%

Dokumen ini adalah rencana implementasi dari kondisi repo saat ini menuju parity 100% dengan `reference/9router`. Source of truth tetap:

- `reference/9router/src/app/api/**` untuk API dashboard/local.
- `reference/9router/open-sse/**` untuk runtime inference, executor, translator, SSE, RTK, usage, token refresh.
- `reference/9router/src/lib/oauth/**` untuk OAuth/device/import/cookie lifecycle.
- `reference/9router/cloud/**` untuk cloud backend.
- `reference/9router/src/app/(dashboard)/dashboard/**` dan `reference/9router/src/shared/**` untuk Web UI.

## Kondisi Saat Ini

### Sudah Berjalan

- Route parity API sudah ditest melalui generated route scan terhadap `reference/9router/src/app/api/**/route.js`.
- Usage dashboard utama sudah membaca SQLite request logs/details/history/chart/provider stats.
- Translator dashboard sudah punya step 1/2/3, preview request, save/load, console logs, dan `/api/translator/send` sudah executable untuk active provider.
- Cloud backend sudah punya sync merge, verify, cache clear, forwarding chat/messages/responses/embeddings basic, count tokens lokal, Ollama `/v1/api/chat`, `/testClaude`, dan legacy `/{machineId}/v1/*`.
- CLI tool config sudah file-backed untuk Claude, Codex, OpenCode, Copilot, Droid, Hermes, OpenClaw, plus generic fallback.
- OAuth import/cookie/PAT sudah ada untuk Cursor import, Kiro import, iFlow cookie, GitLab PAT.
- GitHub Copilot sudah punya device-code + poll, menyimpan connection, menukar Copilot token, dan runtime basic OpenAI-compatible supported.
- Supported runtime saat ini mencakup OpenAI-compatible family, Anthropic-compatible, OpenRouter streaming, Ollama chat/stream/embeddings, Azure, Qoder, OpenCode Go, OpenCode free, GitHub Copilot basic.
- `go test ./...` dan `go build ./cmd/router ./cmd/cloud` sudah hijau pada baseline terakhir.

### Belum 100%

- Provider native batch besar masih belum parity: `codex`, `cursor`, `kiro`, `gemini-cli`, `vertex`, `vertex-partner`, `antigravity`, `iflow`, `qwen`, `grok-web`, `perplexity-web`.
- Media/search providers masih planned: TTS, STT, image, image understanding, web search, web fetch.
- OAuth lifecycle belum lengkap untuk semua provider: authorize/exchange/device/poll/refresh/delete belum semua parity.
- Translator: cursor/kiro/vertex/antigravity translator pairs sudah diimplementasi dan di-test. ✅
- RTK filters sudah diport: tree, grep, find, ls, git status, git diff, smart-truncate, dedup-log, search-list, read-numbered. ✅
- Hub structs sudah diperluas: cache_control, reasoning_content, thinking, signature, PromptTokensDetails, CompletionTokensDetails, system_fingerprint, service_tier, usage on ChatChunk. ✅
- Streaming accumulator sudah parity: thinking/reasoning deltas, usage dari chunk akhir, system_fingerprint/service_tier propagation. ✅
- Request detail logging sudah parity: translated_body, upstream_status, upstream_body, selected_provider/account/model, fallback_attempts, usage_json, error_category. ✅
- Translator belum golden fixture tests penuh untuk semua format pair edge cases.
- Provider native runtime belum parity: cursor, kiro, gemini-cli, vertex, antigravity, iflow, qwen, grok-web, perplexity-web.
- Cloud backend belum punya account fallback/token-refresh/rate-limit parity penuh.
- Tunnel/MITM masih belum operational parity penuh.
- Web UI masih belum reference-equivalent; sekarang masih Vite UI kecil dibanding dashboard reference.
- Playwright/Vitest/manual smoke final belum lengkap.

## Prinsip Implementasi

- Jangan promosikan provider dari `planned` ke `supported` sebelum semua ini lulus: factory build, runtime call, auth lifecycle, error/fallback behavior, mock-server tests.
- Untuk provider deprecated, tetap tampil di catalog tetapi runtime harus safe: tidak auto-enable, tidak misleading, dan error harus eksplisit.
- Jangan memakai live third-party service di test. Semua provider/OAuth/cloud tests memakai `httptest` atau command runner mock.
- API response shape harus mengikuti reference. Jika reference behavior tergantung local/external state yang tidak tersedia, return reference-equivalent safe no-op yang terdokumentasi.
- Pertahankan backward compatibility untuk config YAML, SQLite, `/v1/*`, `/api/v1/*`, `/codex/*`, dan cloud legacy `/{machineId}/v1/*`.
- Semua dokumentasi baru masuk `docs/`.

## Fase 1 - Runtime Core dan Translator Completion

Tujuan fase ini adalah membuat satu jalur runtime Go yang setara dengan `open-sse` dan bisa dipakai semua provider.

### Implementasi

- Lengkapi hub structs di `internal/providers` agar semua request/response field reference bisa round-trip:
  - text, image, tool calls, tool results, function calls, reasoning, thinking, metadata, structured output, response format, cache/control fields, unknown raw fields.
- Lengkapi translator registry Go agar parity dengan:
  - `openai <-> claude`
  - `openai <-> gemini`
  - `openai <-> ollama`
  - `openai <-> responses`
  - `openai <-> cursor`
  - `openai <-> kiro`
  - `openai <-> vertex`
  - `antigravity -> openai`
- Implement streaming accumulator parity:
  - SSE event parser sudah ada, tetapi perlu usage extraction, reasoning/thinking deltas, tool-call assembly, finish reasons, and malformed event skip policy.
- Implement request detail logging parity:
  - original body, translated body, upstream status/body/chunks, selected provider/account/model, fallback attempts, usage, error category.
- Port RTK filters:
  - `tree`, `grep`, `find`, `ls`, `git status`, `git diff`, `smart truncate`, `dedup log`, `search list`, `read numbered`.

### Tests

- Golden translator fixtures dari reference untuk setiap format pair.
- SSE-to-JSON and stream accumulator tests untuk content, tool calls, reasoning, usage, finish reason, malformed chunks.
- RTK filter golden tests menggunakan input/output fixture dari reference behavior.
- Regression test bahwa `/api/translator/translate` step 1/2/3 dan `/api/translator/send` memakai translator/runtime yang sama.

### Acceptance

- `go test ./internal/translator ./internal/providers ./internal/api` hijau.
- Semua translator golden cases lulus.
- Tidak ada field penting dari reference yang hilang saat request/response ditranslate.

## Fase 2 - Provider Batch A: Codex, GitHub, Cursor, Kiro

Tujuan fase ini adalah menyelesaikan provider paling penting untuk coding agents.

### Codex

- Port `reference/9router/open-sse/executors/codex.js`.
- Implement OpenAI/Codex OAuth authorize/exchange/refresh.
- Support `/codex/*` and OpenAI Responses runtime path, termasuk `/v1/responses/compact`.
- Implement compact request marker `_compact` and endpoint selection `/responses` vs `/responses/compact`.
- Persist tokens and refresh before expiry.
- Promote `codex` ke `supported` hanya setelah non-streaming, streaming, compact, refresh, and error tests pass.

### GitHub Copilot

- Current status: device-code/poll/basic runtime supported sudah ada.
- Lengkapi parity:
  - Copilot `/responses` switching for models that require it.
  - sanitize messages sesuai executor reference.
  - token refresh: refresh Copilot token using GitHub access token; fallback to GitHub refresh token if present.
  - GitHub Copilot usage quota fetch from `copilot_internal/user`.
  - model-specific headers and response_format handling.
- Add tests for:
  - 401 refresh retry.
  - `/chat/completions` and `/responses` routing.
  - quota parse.

### Cursor

- Port Cursor executor and protobuf/checksum helpers from `open-sse/utils/cursor*`.
- Complete local auto-import:
  - locate Cursor state DB/files per OS.
  - read token and machine ID.
  - store connection safely.
- Implement native request signing/headers and response transform.
- Promote only after import + runtime + stream tests pass.

### Kiro

- Port Kiro executor and translator request/response.
- Complete device-code flow:
  - AWS Builder ID.
  - IDC start URL/region.
  - social Google/GitHub authorize/exchange.
- Implement refresh token behavior and account cooldown.
- Promote only after device/import/social mock tests and runtime tests pass.

### Tests

- One mock server per provider.
- For each provider: auth, non-streaming, streaming, refresh-on-401, fallback error classification, usage/logging.
- OAuth endpoint shape tests against reference routes.

### Acceptance

- `codex`, `github`, `cursor`, `kiro` are `supported`.
- Factory accepts supported configs and rejects incomplete credentials with clear validation.
- `go test ./internal/providers ./internal/oauth ./internal/api` hijau.

## Fase 3 - Provider Batch B: Gemini CLI, Vertex, Vertex Partner, Antigravity, iFlow

### Gemini CLI

- Port Google OAuth flow and Gemini CLI executor.
- Support project ID detection and provider-specific model endpoints.
- Implement deprecated warning but allow runtime only for valid imported/OAuth credential.
- Add refresh and project ID tests.

### Vertex and Vertex Partner

- Implement service account auth:
  - parse service account JSON.
  - mint access token using JWT assertion.
  - cache token until expiry.
- Implement Vertex request paths and translation.
- Support project/location/model fields in provider config and connection metadata.
- Implement Vertex Partner endpoint rules separately if reference differs.

### Antigravity

- Port Google OAuth flow and native executor.
- Preserve deprecation/account-risk notice in catalog/UI.
- Implement project ID and token refresh behavior.
- Ensure use outside Antigravity is visible as deprecated-risk, not silently normal.

### iFlow

- Current status: cookie import exists.
- Port native executor and token/cookie refresh behavior.
- Validate required cookie fields and store sanitized metadata.

### Tests

- Service-account signing unit tests with local deterministic key fixture.
- Mock token endpoint and mock Vertex/Gemini/Antigravity/iFlow API.
- Refresh-on-expiry and refresh-on-401 tests.

### Acceptance

- `gemini-cli`, `vertex`, `vertex-partner`, `antigravity`, `iflow` are either fully supported with tests or remain planned with exact blocker documented.

## Fase 4 - Deprecated and Web Providers: Qwen, Grok Web, Perplexity Web

### Qwen

- Port device-code PKCE flow.
- Enforce deprecated-safe policy:
  - visible in catalog.
  - not default-recommended.
  - runtime only if user has existing valid token.
  - clear error if upstream free tier is unavailable.

### Grok Web and Perplexity Web

- Port cookie/session executors.
- Add strict cookie validation and redaction.
- Implement browser-web response parsing and streaming where reference supports it.
- Do not mark supported until session replay is safe and testable.

### Tests

- Cookie validation tests.
- Mock web executor tests for success, expired session, malformed event stream.
- Deprecated provider UI/API behavior tests.

### Acceptance

- Deprecated providers are safe, explicit, and never falsely advertised as stable.

## Fase 5 - Media and Search Providers

Tujuan fase ini adalah membuat service kinds selain LLM benar-benar executable.

### Interfaces and API

- Add runtime service interfaces for:
  - TTS.
  - STT.
  - image generation.
  - image understanding.
  - embeddings.
  - web search.
  - web fetch.
- Expose API routes matching reference media/search dashboard and runtime routes.
- Normalize outputs to reference-compatible JSON or binary/audio responses.

### Providers

- TTS:
  - ElevenLabs, Cartesia, PlayHT, Google TTS, Edge TTS, local-device.
- STT/image understanding:
  - Deepgram, AssemblyAI, HuggingFace.
- Image:
  - NanoBanana, SD WebUI, ComfyUI, HuggingFace.
- Search/fetch:
  - Tavily, Brave Search, Serper, Exa, SearXNG, Firecrawl.

### Tests

- Mock-server tests for auth headers, request mapping, response normalization.
- Binary/audio response tests for TTS.
- Error classification tests per provider.
- Catalog promotion tests.

### Acceptance

- Every provider with `ExecutionStatus: supported` has a concrete service implementation and tests.
- Media provider dashboard can create/test/use each supported provider.

## Fase 6 - OAuth Lifecycle Completion

Tujuan fase ini adalah membuat `/api/oauth/*` exact parity, bukan sekadar import endpoints.

### Implementasi

- Add generic OAuth provider registry for:
  - authorize.
  - exchange.
  - device-code.
  - poll.
  - refresh.
  - delete/revoke where reference supports it.
- Normalize response shapes:
  - success response includes connection summary.
  - pending device flow returns `{success:false,pending:true,error,...}`.
  - invalid input returns reference-compatible 400.
- Store encrypted tokens in SQLite if encryption helper exists; otherwise add encryption-at-rest helper using local secret file and document migration.
- Implement token refresh service used by runtime providers and cloud sync.

### Tests

- Endpoint shape tests for each provider/action.
- Persistence tests for token metadata.
- Refresh tests for expiry and 401/403 retry.
- Redaction tests for logs and API responses.

### Acceptance

- No supported OAuth provider has `StatusNotImplemented` path.
- Runtime provider can refresh without user re-authentication where reference can.

## Fase 7 - Cloud Backend Full Parity

Current cloud backend is functional but not full `open-sse` parity.

### Implementasi

- Reuse same provider executor/translator/token-refresh logic as local runtime.
- Complete chat forwarding parity:
  - account fallback loop.
  - rate-limit cooldown.
  - retry-after header.
  - unavailable account marking and clearing.
- Complete endpoints:
  - `/v1/chat/completions`
  - `/v1/messages`
  - `/v1/embeddings`
  - `/v1/responses`
  - `/v1/responses/compact`
  - `/v1/api/chat`
  - `/v1/messages/count_tokens`
  - legacy `/{machineId}/v1/*`
  - `/forward`
  - `/forward-raw`
  - `/testClaude`
- Add persistent storage adapter:
  - SQLite default.
  - optional Postgres or S3-compatible object storage for production.
- Add cache cleanup behavior from reference.

### Tests

- Cloud sync merge/verify/cache lifecycle.
- Cloud chat/messages/responses/compact/embeddings/count_tokens.
- Machine ID in API key and legacy path.
- Token refresh and fallback.
- Persistence restart test.

### Acceptance

- `cmd/cloud` can run independently with persistent sync data.
- Cloud mock provider tests cover all public cloud routes.

## Fase 8 - Tunnel and MITM Operational Parity

### Tunnel

- Replace config-only behavior with runtime manager API:
  - Cloudflare quick tunnel.
  - Cloudflare named tunnel.
  - Tailscale check/login/start daemon/funnel enable/disable.
- Persist tunnel state and last known URL.
- Return reference-compatible `/api/tunnel/*` payloads.

### MITM

- Complete Antigravity MITM API:
  - status.
  - start/stop.
  - cert exists/trusted.
  - per-tool DNS enable/disable.
  - cached sudo password metadata.
  - model alias mapping.
- Use command runner abstraction so tests never mutate system DNS/certs.

### Tests

- Mock command runner tests for every tunnel/MITM action.
- Status transition tests.
- Alias mapping tests.
- Failure mode tests for missing binary/permission denied.

### Acceptance

- Dashboard can operate tunnel/MITM flows without shell placeholders.
- CI remains safe because system operations are mocked.

## Fase 9 - Web UI Reference Parity

Current UI is smaller than reference. This fase replaces it with reference-equivalent workflows while keeping Vite/React/Tailwind and Go embedded static serving.

### Pages

- Dashboard overview.
- Providers list and provider detail.
- Media providers.
- Combos/routes.
- Usage, history, request details, quotas.
- Proxy pools.
- Endpoint/config.
- CLI tools.
- MITM.
- Translator.
- Console log.
- Basic chat.
- Profile.
- Settings/pricing.

### Components

- Provider icons and status badges.
- Model selector modal.
- OAuth/device/import modals.
- CLI tool cards.
- Usage/quota tables.
- Sidebar/layout/theme/i18n.
- Pricing panels.

### API Integration

- UI must call Go endpoints directly.
- Remove mock-only states for completed backend features.
- Keep empty/safe states for unsupported planned providers.

### Tests

- Vitest for API client and transforms.
- Playwright smoke:
  - login.
  - provider CRUD.
  - OAuth device flow UI with mocked backend.
  - chat request.
  - usage/logs.
  - proxy pool.
  - CLI tools.
  - tunnel/MITM.
  - translator.

### Acceptance

- `npm run build` passes.
- Playwright smoke passes on local router.
- Embedded `internal/webui/dist` serves expected app.

## Fase 10 - Final Reports, Docs, and Release Readiness

### Reports

- Generate/update:
  - `docs/reference-route-map.md`
  - `docs/reference-provider-map.md`
  - `docs/provider-execution-status.md`
  - `docs/oauth-lifecycle-matrix.md`
  - `docs/reference-parity-final-report.md`
- Reports must state:
  - supported providers.
  - planned/deprecated providers and exact reason.
  - API route parity.
  - cloud route parity.
  - UI parity.

### Packaging

- Update:
  - Dockerfile.
  - docker-compose.
  - Makefile.
  - install script.
  - systemd service.
- Ensure both binaries are covered:
  - local router.
  - cloud compatibility server.

### Final Verification

Run and record:

```bash
go test ./...
go build ./cmd/router ./cmd/cloud
npm run build
pnpm playwright test
```

Manual smoke:

1. Fresh config and empty SQLite.
2. Login to dashboard.
3. Add provider.
4. Test model.
5. Send chat.
6. See logs/usage/request details.
7. Restart router.
8. Confirm state persists.
9. Run cloud sync and cloud inference smoke.
10. Run CLI tool config smoke for Claude/Codex/OpenCode/Copilot/Droid/Hermes/OpenClaw.

## Completion Criteria

The project can be called 100% reference parity only when all conditions below are true:

- Every reference API route exists and has compatible happy/error response shape.
- Every catalog `supported` provider has tested executable runtime behavior.
- Every planned provider either becomes supported with tests or remains planned with documented reference blocker.
- OAuth lifecycle works for every provider that reference supports.
- Translator golden tests pass for all reference formats.
- Cloud backend passes route, sync, forwarding, refresh, and persistence tests.
- Web UI exposes reference workflows without relying on missing backend features.
- Final Go, web, Playwright, and manual smoke verification are all recorded in docs.

