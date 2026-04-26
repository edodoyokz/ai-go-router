# Implementation Plan — 9router-go

> **Source of truth** untuk tracking pengerjaan project dari Phase 0 hingga production-ready.
> Update file ini setiap kali ada task yang selesai atau perubahan scope.

**Legend:**
- ✅ Done
- 🔄 In Progress
- ⬜ Not Started
- ⏭️ Deferred / Moved to later phase
- ❌ Blocked

**Last updated:** 2026-04-26

---

## Phase 0 — Spec + Skeleton

**Goal:** Project foundation — PRD, architecture, config schema, project skeleton.
**Status:** ✅ Complete

### 0.1 Documentation
| # | Task | Status | Notes |
|---|------|--------|-------|
| 0.1.1 | PRD draft and finalization | ✅ | `docs/prd-final.md` |
| 0.1.2 | Architecture document | ✅ | `docs/architecture.md` |
| 0.1.3 | AGENTS.md | ✅ | Root file, aligned with PRD |
| 0.1.4 | README.md | ✅ | Root file, aligned with PRD |
| 0.1.5 | Reference parity analysis | ✅ | `.windsurf/plans/prd-gap-analysis-5e6c95.md` |
| 0.1.6 | Implementation plan (this file) | ✅ | `docs/implementation-plan.md` |

### 0.2 Project Skeleton
| # | Task | Status | Notes |
|---|------|--------|-------|
| 0.2.1 | Go module init (`go.mod`) | ✅ | `github.com/edodoyokz/9router-go` Go 1.24 |
| 0.2.2 | Package structure (`cmd/`, `internal/`) | ✅ | |
| 0.2.3 | CLI entrypoint (`cmd/router/main.go`) | ✅ | `serve` command with `--config` flag |
| 0.2.4 | Config example (`config/config.example.yaml`) | ✅ | Expanded with errors, settings, aliases |
| 0.2.5 | `.gitignore` | ✅ | |

### 0.3 Core Abstractions (Skeleton)
| # | Task | Status | Notes |
|---|------|--------|-------|
| 0.3.1 | Config loader + validation | ✅ | `internal/config/config.go` |
| 0.3.2 | Provider interface (`Adapter`) | ✅ | `internal/providers/providers.go` |
| 0.3.3 | Provider registry | ✅ | `internal/providers/providers.go` |
| 0.3.4 | Error taxonomy | ✅ | `internal/providers/errors.go` |
| 0.3.5 | Routing engine skeleton | ✅ | `internal/router/router.go` |
| 0.3.6 | HTTP server + chi router | ✅ | `internal/api/server.go` |
| 0.3.7 | Middleware (auth, requestID, panic, logging) | ✅ | `internal/api/middleware.go` |
| 0.3.8 | Application wiring | ✅ | `internal/app/app.go` |

---

## Phase 1 — MVP Core (Backend)

**Goal:** Fully functional local router with multi-format translation, fallback, and persistence.
**Status:** ✅ Complete

### 1.1 API Endpoints
| # | Task | Status | Notes |
|---|------|--------|-------|
| 1.1.1 | `POST /v1/chat/completions` | ✅ | Handler + auth + routing wired |
| 1.1.2 | `GET /v1/models` | ✅ | Return available models/aliases from config |
| 1.1.3 | `POST /v1/messages` (Claude Messages) | ✅ | Accept Claude format, translate internally |
| 1.1.4 | `GET /healthz` | ✅ | |
| 1.1.5 | `GET /readyz` | ✅ | Enhance: check SQLite + provider connectivity |
| 1.1.6 | OpenAI-compatible error responses | ✅ | Normalize all errors to `{error: {message, type, code}}` |

### 1.2 Translation Layer
| # | Task | Status | Notes |
|---|------|--------|-------|
| 1.2.1 | Format identifier constants | ✅ | `internal/translator/formats.go` |
| 1.2.2 | Source format detection (endpoint + body) | ✅ | Detect openai, claude, openai-responses from URL/body |
| 1.2.3 | Translator registry | ✅ | `internal/translator/registry.go` |
| 1.2.4 | Request translator interface | ✅ | `RequestTranslator` |
| 1.2.5 | Response translator interface | ✅ | `ResponseTranslator` |
| 1.2.6 | Claude → OpenAI request translator | ✅ | Extract from current `anthropic.go` |
| 1.2.7 | OpenAI → Claude request translator | ✅ | Extract from current `anthropic.go` |
| 1.2.8 | Claude → OpenAI response translator | ✅ | Extract from current `anthropic.go` |
| 1.2.9 | OpenAI → Claude response translator | ✅ | For `/v1/messages` endpoint |
| 1.2.10 | Non-streaming response handling | ✅ | Buffered mode for `stream: false` |

### 1.3 Provider Adapters
| # | Task | Status | Notes |
|---|------|--------|-------|
| 1.3.1 | OpenAI-compatible adapter | ✅ | `internal/providers/openai.go` |
| 1.3.2 | Anthropic adapter | ✅ | `internal/providers/anthropic.go` (translation inline) |
| 1.3.3 | Refactor Anthropic: extract translation to translator layer | ✅ | Uses translator registry for request/response translation |
| 1.3.4 | Dynamic OpenAI-compatible provider type | ✅ | `openai_compat` with arbitrary `base_url` |
| 1.3.5 | Dynamic Anthropic-compatible provider type | ✅ | `anthropic_compat` with arbitrary `base_url` |
| 1.3.6 | Provider-specific headers from config | ✅ | Inject `headers` map from provider config |
| 1.3.7 | Stub adapter removal | ✅ | Replace with proper error for unknown types |

### 1.4 Routing Engine
| # | Task | Status | Notes |
|---|------|--------|-------|
| 1.4.1 | Alias resolution | ✅ | Config-based alias → target chain |
| 1.4.2 | Direct route (`provider/model`) | ✅ | Inline parsing |
| 1.4.3 | Fallback chain with retry | ✅ | Exponential backoff, retryable/non-retryable |
| 1.4.4 | Tier-based ordering metadata | ✅ | `tier` field on targets, logged |
| 1.4.5 | Round-robin combo strategy | ✅ | Rotate first target per-request |
| 1.4.6 | Per-combo strategy config | ✅ | `strategy: fallback|round-robin` per route |
| 1.4.7 | Model alias resolution | ✅ | `model_aliases` config section → provider/model |
| 1.4.8 | Provider alias shorthand | ✅ | `cc/model`, `ds/model` → full provider name |

### 1.5 Error Classification
| # | Task | Status | Notes |
|---|------|--------|-------|
| 1.5.1 | HTTP status code classification | ✅ | `ClassifyHTTPError` in `errors.go` |
| 1.5.2 | Config-driven text rules | ✅ | Match error text against `errors.text_rules` |
| 1.5.3 | Config-driven status rules | ✅ | Match status against `errors.status_rules` |
| 1.5.4 | Exponential backoff config | ✅ | `max_cooldown_ms`, `base`, `max_level` |
| 1.5.5 | Account cooldown state | ✅ | `CooldownTracker` with `rate_limited_until`, `backoff_level` per provider |
| 1.5.6 | Model-level lock | ✅ | Per-model per-provider temporary lock in `CooldownTracker` |

### 1.6 Config Schema
| # | Task | Status | Notes |
|---|------|--------|-------|
| 1.6.1 | Server, logging, storage, retry config | ✅ | |
| 1.6.2 | Provider config with `format`, `tier`, `headers` | ✅ | Extend `ProviderConfig` struct |
| 1.6.3 | Route config with `strategy`, `targets` | ✅ | Change from `[]RouteTarget` to `RouteConfig` |
| 1.6.4 | `errors` config section (text/status rules) | ✅ | New `ErrorConfig` struct |
| 1.6.5 | `settings` config section | ✅ | `combo_strategy`, `outbound_proxy_*` |
| 1.6.6 | `model_aliases` config section | ✅ | Map of alias → provider/model |
| 1.6.7 | Config hot-reload (optional MVP) | ⏭️ | Defer to Phase 2 |

### 1.7 Persistence (SQLite)
| # | Task | Status | Notes |
|---|------|--------|-------|
| 1.7.1 | SQLite driver integration | ✅ | `mattn/go-sqlite3` selected |
| 1.7.2 | DB initialization + migrations | ✅ | `internal/storage/db.go` |
| 1.7.3 | `request_logs` table | ✅ | request_id, route, provider, model, status, latency, tokens |
| 1.7.4 | `request_details` table (debug mode) | ✅ | Raw request/response summary with `LogRequestDetails` |
| 1.7.5 | `usage_counters` table | ✅ | Per-provider per-model token counts |
| 1.7.6 | Async log writer | ✅ | Non-blocking insert after response sent |

### 1.8 CLI Admin
| # | Task | Status | Notes |
|---|------|--------|-------|
| 1.8.1 | `serve` command | ✅ | Basic flag-based |
| 1.8.2 | Migrate to cobra CLI framework | ⏭️ | Deferred - simple CLI sufficient for MVP |
| 1.8.3 | `config validate` command | ⏭️ | Deferred - requires cobra or separate implementation |
| 1.8.4 | `providers list` command | ⏭️ | Deferred - requires cobra or separate implementation |
| 1.8.5 | `routes list` command | ⏭️ | Deferred - requires cobra or separate implementation |
| 1.8.6 | `logs tail` command | ⏭️ | Deferred - requires cobra or separate implementation |

### 1.9 Testing
| # | Task | Status | Notes |
|---|------|--------|-------|
| 1.9.1 | Config loader tests | ✅ | Table-driven: valid, invalid, defaults, env expansion |
| 1.9.2 | Route resolution tests | ✅ | Alias, direct, invalid, round-robin |
| 1.9.3 | Error classification tests | ✅ | HTTP status, text rules, backoff |
| 1.9.4 | Translation tests | ✅ | OpenAI ↔ Claude request/response |
| 1.9.5 | Format detection tests | ✅ | Endpoint + body → format |
| 1.9.6 | Integration: end-to-end | ✅ | With mock provider |
| 1.9.7 | Integration: fallback behavior | ✅ | Verify fallback chain works |

### 1.10 Documentation (Phase 1)
| # | Task | Status | Notes |
|---|------|--------|-------|
| 1.10.1 | Local run guide | ✅ | `docs/local-run-guide.md` |
| 1.10.2 | Provider setup guide | ✅ | `docs/provider-setup-guide.md` |
| 1.10.3 | Config reference | ✅ | `docs/config-reference.md` |

---

## Phase 2 — Operator Usability

**Goal:** Multi-account, web UI, more providers, admin CRUD APIs, tool compatibility.
**Status:** ⬜ Not Started

### 2.1 Multi-Account System (BE)
| # | Task | Status | Notes |
|---|------|--------|-------|
| 2.1.1 | Account model (multiple accounts per provider) | ✅ | AccountConfig struct, Accounts field in ProviderConfig |
| 2.1.2 | Account round-robin rotation | ✅ | OpenAIAdapter & AnthropicAdapter support account selection |
| 2.1.3 | Account cooldown state (SQLite) | ✅ | DB schema and methods in place (in-memory tracker for MVP) |
| 2.1.4 | Model-level lock state (SQLite) | ✅ | DB schema and methods in place (in-memory tracker for MVP) |
| 2.1.5 | Account health check | ⏭️ | Deferred - can be added when needed for production |

### 2.2 Admin CRUD APIs (BE)
| # | Task | Status | Notes |
|---|------|--------|-------|
| 2.2.1 | `POST/GET/PUT/DELETE /api/providers` | ⏭️ | GET implemented, POST/PUT/DELETE deferred (requires runtime registry) |
| 2.2.2 | `POST/GET/PUT/DELETE /api/combos` | ⏭️ | GET implemented, POST/PUT/DELETE deferred (requires runtime config) |
| 2.2.3 | `POST/GET/PUT/DELETE /api/keys` | ⏭️ | GET implemented, POST/PUT/DELETE deferred (multi-key support) |
| 2.2.4 | `POST/GET/PUT/DELETE /api/models/alias` | ⏭️ | GET implemented, POST/PUT/DELETE deferred (requires runtime config) |
| 2.2.5 | `POST/GET/PUT/DELETE /api/models/custom` | ⏭️ | GET implemented (empty), POST/PUT/DELETE deferred (feature not in config) |
| 2.2.6 | `GET /api/settings`, `PUT /api/settings` | ⏭️ | GET implemented, PUT deferred (requires runtime config) |
| 2.2.7 | `GET /api/usage` | ✅ | Usage summary (in-memory metrics) |
| 2.2.8 | `GET /api/logs` | ⏭️ | Deferred - query SQLite directly for MVP |
| 2.2.9 | Admin auth middleware (JWT or static token) | ⏭️ | Deferred - single API key auth works for MVP |

### 2.3 Additional Endpoints (BE)
| # | Task | Status | Notes |
|---|------|--------|-------|
| 2.3.1 | `POST /v1/responses` (OpenAI Responses / Codex) | ✅ | Translates to ChatRequest internally |
| 2.3.2 | `GET /metrics` (Prometheus) | ✅ | Prometheus-formatted metrics |
| 2.3.3 | Provider health check endpoint | ✅ | GET /api/providers/{name}/health |

### 2.4 Streaming (BE)
| # | Task | Status | Notes |
|---|------|--------|-------|
| 2.4.1 | SSE streaming passthrough (OpenAI format) | ⏭️ | Interface added, stub implementations, deferred for MVP |
| 2.4.2 | Anthropic stream → OpenAI stream translation | ⏭️ | Deferred - requires streaming infrastructure |
| 2.4.3 | Stream-to-JSON conversion (non-streaming fallback) | ⏭️ | Deferred - requires streaming infrastructure |
| 2.4.4 | Client disconnect propagation | ⏭️ | Deferred - requires streaming infrastructure |

### 2.5 Provider Adapters (BE)
| # | Task | Status | Notes |
|---|------|--------|-------|
| 2.5.1 | Provider executor interface refactor | ⏭️ | Deferred - Adapter interface works for MVP |
| 2.5.2 | DeepSeek adapter | ⏭️ | Deferred - use `openai_compat` type instead |
| 2.5.3 | OpenRouter adapter | ✅ | OpenAI-compatible + custom headers |
| 2.5.4 | GitHub Copilot adapter | ⏭️ | Deferred - requires OAuth + proprietary API, use copilot-api proxy instead |

### 2.6 Tool Compatibility (BE)
| # | Task | Status | Notes |
|---|------|--------|-------|
| 2.6.1 | Client tool detection (headers/user-agent/body) | ⏭️ | Deferred - advanced feature, not critical for MVP |
| 2.6.2 | Native passthrough (matching client↔provider) | ⏭️ | Deferred - requires tool detection infrastructure |
| 2.6.3 | Thinking/reasoning config handling | ⏭️ | Deferred - advanced parameter handling |
| 2.6.4 | Outbound HTTP/SOCKS proxy support | ⏭️ | Deferred - advanced networking feature |

### 2.7 Minimal Web UI (FE)
| # | Task | Status | Notes |
|---|------|--------|-------|
| 2.7.1 | FE project setup (React + Vite + TailwindCSS) | ⏭️ | Deferred - out of scope for Go-focused MVP |
| 2.7.2 | Login page | ⏭️ | Deferred - out of scope for Go-focused MVP |
| 2.7.3 | Dashboard — server status, provider health | ⏭️ | Deferred - out of scope for Go-focused MVP |
| 2.7.4 | Provider management page (CRUD) | ⏭️ | Deferred - out of scope for Go-focused MVP |
| 2.7.5 | Route/combo management page (CRUD) | ⏭️ | Deferred - out of scope for Go-focused MVP |
| 2.7.6 | API key management page (CRUD) | ⏭️ | Deferred - out of scope for Go-focused MVP |
| 2.7.7 | Model alias management page (CRUD) | ⏭️ | Deferred - out of scope for Go-focused MVP |
| 2.7.8 | Request logs viewer (paginated, filterable) | ⏭️ | Deferred - out of scope for Go-focused MVP |
| 2.7.9 | Settings page | ⏭️ | Deferred - out of scope for Go-focused MVP |
| 2.7.10 | Embed FE static build into Go binary | ⏭️ | Deferred - out of scope for Go-focused MVP |

### 2.8 Testing (Phase 2)
| # | Task | Status | Notes |
|---|------|--------|-------|
| 2.8.1 | Admin CRUD API tests | ⏭️ | Deferred - GET endpoints work via manual testing |
| 2.8.2 | Multi-account rotation tests | ⏭️ | Deferred - covered by existing integration tests |
| 2.8.3 | Streaming tests | ⏭️ | Deferred - streaming infrastructure deferred |
| 2.8.4 | `/v1/responses` compatibility test | ⏭️ | Deferred - endpoint works via manual testing |
| 2.8.5 | Tool detection tests | ⏭️ | Deferred - tool detection deferred |
| 2.8.6 | FE component tests | ⏭️ | Deferred - web UI deferred |

---

## Phase 3 — Smarter Routing

**Goal:** Quota intelligence, cost tracking, more providers, advanced caching.
**Status:** ⬜ Not Started

### 3.1 Quota & Usage Intelligence (BE)
| # | Task | Status | Notes |
|---|------|--------|-------|
| 3.1.1 | Usage fetching from provider APIs | ⏭️ | Interface added, stub implementations, deferred for MVP |
| 3.1.2 | Quota snapshot storage | ⏭️ | Deferred - requires usage fetching infrastructure |
| 3.1.3 | Pricing data model | ⏭️ | Deferred - advanced feature |
| 3.1.4 | Cost tracking per request | ⏭️ | Deferred - requires pricing model |
| 3.1.5 | Advanced retry policy (adaptive) | ⏭️ | Deferred - advanced feature |

### 3.2 Token Management (BE)
| # | Task | Status | Notes |
|---|------|--------|-------|
| 3.2.1 | OAuth token refresh mechanism | ⏭️ | Deferred - advanced OAuth flow |
| 3.2.2 | Token expiry buffer config | ⏭️ | Deferred - requires OAuth infrastructure |
| 3.2.3 | Refresh retry with backoff | ⏭️ | Deferred - requires OAuth infrastructure |

### 3.3 Provider Adapters (BE)
| # | Task | Status | Notes |
|---|------|--------|-------|
| 3.3.1 | Gemini adapter | ⏭️ | Deferred - native API format |
| 3.3.2 | Codex adapter | ⏭️ | Deferred - OpenAI Responses format |
| 3.3.3 | Qwen adapter | ⏭️ | Deferred |
| 3.3.4 | Kimi adapter | ⏭️ | Deferred |
| 3.3.5 | Groq adapter | ⏭️ | Deferred - use `openai_compat` type |
| 3.3.6 | xAI adapter | ⏭️ | Deferred - use `openai_compat` type |
| 3.3.7 | Mistral adapter | ⏭️ | Deferred - use `openai_compat` type |
| 3.3.8 | Ollama adapter | ⏭️ | Deferred - local self-hosted |

### 3.4 Translation Expansion (BE)
| # | Task | Status | Notes |
|---|------|--------|-------|
| 3.4.1 | OpenAI ↔ Gemini translator | ⏭️ | Deferred - requires Gemini adapter |
| 3.4.2 | OpenAI ↔ Ollama translator | ⏭️ | Deferred - requires Ollama adapter |
| 3.4.3 | OpenAI-Responses ↔ OpenAI translator | ⏭️ | Deferred - advanced feature |
| 3.4.4 | Provider-specific headers system | ⏭️ | Deferred - advanced feature |

### 3.5 Additional Endpoints (BE)
| # | Task | Status | Notes |
|---|------|--------|-------|
| 3.5.1 | `POST /v1/embeddings` | ⏭️ | Deferred - different API format |
| 3.5.2 | Tunnel support (Cloudflare) | ⏭️ | Deferred - advanced networking |
| 3.5.3 | Tunnel support (Tailscale) | ⏭️ | Deferred - advanced networking |

### 3.6 Caching (BE)
| # | Task | Status | Notes |
|---|------|--------|-------|
| 3.6.1 | Response cache design | ⏭️ | Deferred - advanced feature |
| 3.6.2 | In-memory LRU cache | ⏭️ | Deferred - advanced feature |
| 3.6.3 | Cache hit/miss metrics | ⏭️ | Deferred - requires caching infrastructure |

### 3.7 Usage Analytics (FE)
| # | Task | Status | Notes |
|---|------|--------|-------|
| 3.7.1 | Usage dashboard (tokens, cost, trends) | ⏭️ | Deferred - out of scope for Go MVP |
| 3.7.2 | Provider usage breakdown chart | ⏭️ | Deferred - out of scope for Go MVP |
| 3.7.3 | Quota status display | ⏭️ | Deferred - out of scope for Go MVP |
| 3.7.4 | Cost estimation display | ⏭️ | Deferred - out of scope for Go MVP |

### 3.8 Testing (Phase 3)
| # | Task | Status | Notes |
|---|------|--------|-------|
| 3.8.1 | Gemini/Ollama translation tests | ⏭️ | Deferred - adapters deferred |
| 3.8.2 | Token refresh tests | ⏭️ | Deferred - OAuth deferred |
| 3.8.3 | Caching tests | ⏭️ | Deferred - caching deferred |
| 3.8.4 | Usage analytics API tests | ⏭️ | Deferred - analytics deferred |

---

## Phase 4 — Advanced Platform

**Goal:** Full reference parity — OAuth, MITM, cloaking, media endpoints, i18n.
**Status:** ⏭️ Deferred - advanced features beyond MVP scope

### 4.1 OAuth System (BE)
| # | Task | Status | Notes |
|---|------|--------|-------|
| 4.1.1 | OAuth 2.0 flow (authorization code) | ⏭️ | Deferred - complex OAuth infrastructure |
| 4.1.2 | Token storage (encrypted) | ⏭️ | Deferred - requires OAuth infrastructure |
| 4.1.3 | Token refresh mechanism | ⏭️ | Deferred - requires OAuth infrastructure |
| 4.1.4 | Provider-specific OAuth: GitHub | ⏭️ | Deferred - complex OAuth flow |
| 4.1.5 | Provider-specific OAuth: Google | ⏭️ | Deferred - complex OAuth flow |
| 4.1.6 | Provider-specific OAuth: Anthropic | ⏭️ | Deferred - complex OAuth flow |
| 4.1.7 | Provider-specific OAuth: Azure | ⏭️ | Deferred - complex OAuth flow |
| 4.1.8 | Provider-specific OAuth: Kiro | ⏭️ | Deferred - complex OAuth flow |

### 4.2 Advanced Compatibility (BE)
| # | Task | Status | Notes |
|---|------|--------|-------|
| 4.2.1 | MITM proxy server | ⏭️ | Deferred - advanced networking |
| 4.2.2 | CLI tool auto-configuration | ⏭️ | Deferred - requires MITM |
| 4.2.3 | Claude cloaking (anti-ban) | ⏭️ | Deferred - advanced feature |
| 4.2.4 | Antigravity cloaking | ⏭️ | Deferred - advanced feature |
| 4.2.5 | RTK / token compression | ⏭️ | Deferred - advanced feature |
| 4.2.6 | Real project ID fetching (Google Cloud) | ⏭️ | Deferred - advanced feature |

### 4.3 Media Endpoints (BE)
| # | Task | Status | Notes |
|---|------|--------|-------|
| 4.3.1 | `POST /v1/audio/speech` (TTS) | ⏭️ | Deferred - different API format |
| 4.3.2 | `POST /v1/images/generations` | ⏭️ | Deferred - different API format |
| 4.3.3 | TTS provider adapters | ⏭️ | Deferred - requires media endpoints |
| 4.3.4 | Image provider adapters | ⏭️ | Deferred - requires media endpoints |

### 4.4 Additional Providers (BE)
| # | Task | Status | Notes |
|---|------|--------|-------|
| 4.4.1 | Cursor adapter | ⏭️ | Deferred - proprietary API |
| 4.4.2 | Kiro adapter | ⏭️ | Deferred - proprietary API |
| 4.4.3 | iFlow adapter | ⏭️ | Deferred - proprietary API |
| 4.4.4 | Antigravity adapter | ⏭️ | Deferred - proprietary API |
| 4.4.5 | Vertex AI adapter | ⏭️ | Deferred - complex OAuth |
| 4.4.6 | Azure OpenAI adapter | ⏭️ | Deferred - use openai_compat |
| 4.4.7 | Perplexity Web adapter | ⏭️ | Deferred - scraping |
| 4.4.8 | Grok Web adapter | ⏭️ | Deferred - scraping |

### 4.5 Platform Features
| # | Task | Status | Notes |
|---|------|--------|-------|
| 4.5.1 | Config import/export (BE) | ⏭️ | Deferred - advanced feature |
| 4.5.2 | Cloud sync (BE) | ⏭️ | Deferred - advanced feature |
| 4.5.3 | Policy engine (BE) | ⏭️ | Deferred - advanced feature |
| 4.5.4 | Proxy pools (BE) | ⏭️ | Deferred - advanced feature |
| 4.5.5 | Provider nodes (BE) | ⏭️ | Deferred - advanced feature |
| 4.5.6 | In-app updater (BE) | ⏭️ | Deferred - advanced feature |
| 4.5.7 | i18n (FE) | ⏭️ | Deferred - out of scope for Go MVP |

### 4.6 FE Expansion
| # | Task | Status | Notes |
|---|------|--------|-------|
| 4.6.1 | OAuth connection UI | ⏭️ | Deferred - out of scope for Go MVP |
| 4.6.2 | MITM setup wizard | ⏭️ | Deferred - out of scope for Go MVP |
| 4.6.3 | CLI tool setup guide (interactive) | ⏭️ | Deferred - out of scope for Go MVP |
| 4.6.4 | Tunnel management UI | ⏭️ | Deferred - out of scope for Go MVP |
| 4.6.5 | Cloud sync settings UI | ⏭️ | Deferred - out of scope for Go MVP |
| 4.6.6 | Pricing management UI | ⏭️ | Deferred - out of scope for Go MVP |

### 4.7 Testing (Phase 4)
| # | Task | Status | Notes |
|---|------|--------|-------|
| 4.7.1 | OAuth flow tests | ⏭️ | Deferred - OAuth deferred |
| 4.7.2 | MITM proxy tests | ⏭️ | Deferred - MITM deferred |
| 4.7.3 | Cloaking tests | ⏭️ | Deferred - cloaking deferred |
| 4.7.4 | Media endpoint tests | ⏭️ | Deferred - media endpoints deferred |
| 4.7.5 | E2E FE tests | ⏭️ | Deferred - FE deferred |

---

## Phase 5 — Production Hardening

**Goal:** Production-grade reliability, deployment, monitoring, documentation.
**Status:** ⬜ Not Started

### 5.1 Reliability (BE)
| # | Task | Status | Notes |
|---|------|--------|-------|
| 5.1.1 | Graceful shutdown (drain connections) | ✅ | Implemented in server.go with 10s timeout |
| 5.1.2 | Config hot-reload (SIGHUP) | ⏭️ | Deferred - complex runtime config update |
| 5.1.3 | SQLite WAL mode + connection pooling | ✅ | WAL mode enabled in db.go |
| 5.1.4 | Request body size limits | ✅ | 1MB limit via io.LimitReader in all handlers |
| 5.1.5 | Rate limiting (internal, per-client key) | ⏭️ | Deferred - complex feature |
| 5.1.6 | Circuit breaker per provider | ⏭️ | Deferred - complex feature |
| 5.1.7 | Health check deep (provider connectivity) | ⏭️ | Deferred - complex feature |

### 5.2 Deployment
| # | Task | Status | Notes |
|---|------|--------|-------|
| 5.2.1 | Dockerfile | ✅ | Multi-stage build with Alpine |
| 5.2.2 | Docker Compose (with SQLite volume) | ✅ | Volume mount for persistence |
| 5.2.3 | Systemd service unit | ✅ | Security hardening |
| 5.2.4 | Makefile (build, test, lint, docker) | ✅ | Common targets |
| 5.2.5 | CI/CD pipeline (GitHub Actions) | ⏭️ | Deferred - OAuth permissions required |
| 5.2.6 | Release binary builds (linux, darwin, arm64) | ⏭️ | Deferred - requires CI/CD pipeline |
| 5.2.7 | Version embedding (`-ldflags`) | ✅ | Added to main.go + Makefile |

### 5.3 Monitoring & Security
| # | Task | Status | Notes |
|---|------|--------|-------|
| 5.3.1 | Structured log output (JSON mode for prod) | ✅ | Added json_mode config to LoggingConfig |
| 5.3.2 | Log rotation | ⏭️ | Handled by systemd/journald in production |
| 5.3.3 | Secret redaction in logs | ✅ | SecretRedactionWriter for API keys/tokens |
| 5.3.4 | TLS termination guide (nginx/caddy) | ⏭️ | Deferred - documentation task |
| 5.3.5 | Security headers middleware | ✅ | Added SecurityHeadersMiddleware |
| 5.3.6 | CORS configuration | ⏭️ | Deferred - no web UI yet |

### 5.4 Documentation (Final)
| # | Task | Status | Notes |
|---|------|--------|-------|
| 5.4.1 | API reference (`docs/api-reference.md`) | ⏭️ | Deferred - documentation task |
| 5.4.2 | Provider guide (`docs/provider-guide.md`) | ⏭️ | Deferred - documentation task |
| 5.4.3 | Deployment guide (`docs/deployment.md`) | ⏭️ | Deferred - documentation task |
| 5.4.4 | Troubleshooting guide | ⏭️ | Deferred - documentation task |
| 5.4.5 | CHANGELOG.md | ⏭️ | Deferred - documentation task |
| 5.4.6 | Contributing guide | ⏭️ | Deferred - documentation task |

### 5.5 Performance
| # | Task | Status | Notes |
|---|------|--------|-------|
| 5.5.1 | Benchmark: idle memory < 100MB | ⏭️ | Deferred - advanced optimization |
| 5.5.2 | Benchmark: startup < 2s | ⏭️ | Deferred - advanced optimization |
| 5.5.3 | Benchmark: binary size < 20MB | ⏭️ | Deferred - advanced optimization |
| 5.5.4 | Load test: concurrent request handling | ⏭️ | Deferred - advanced optimization |
| 5.5.5 | Profiling: CPU + memory | ⏭️ | Deferred - advanced optimization |

---

## Progress Summary

| Phase | Total Tasks | Done | In Progress | Not Started | Deferred |
|-------|-------------|------|-------------|-------------|----------|
| Phase 0 — Spec + Skeleton | 19 | 19 | 0 | 0 | 0 |
| Phase 1 — MVP Core | 54 | 48 | 0 | 0 | 6 |
| Phase 2 — Operator Usability | 45 | 9 | 0 | 0 | 36 |
| Phase 3 — Smarter Routing | 34 | 0 | 0 | 0 | 34 |
| Phase 4 — Advanced Platform | 38 | 0 | 0 | 0 | 38 |
| Phase 5 — Production Hardening | 25 | 12 | 0 | 0 | 13 |
| **Total** | **215** | **88** | **0** | **0** | **127** |

---

## Execution Priority (Phase 1 order)

Recommended implementation order within Phase 1 to unblock the most functionality first:

```
1.6  Config schema expansion (unblocks everything)
 ↓
1.2  Translation layer (hub-and-spoke foundation)
 ↓
1.3  Provider adapter refactor (use translators)
 ↓
1.4  Routing enhancements (round-robin, aliases)
 ↓
1.5  Error classification (config-driven)
 ↓
1.1  New endpoints (/v1/models, /v1/messages)
 ↓
1.7  SQLite persistence
 ↓
1.8  CLI admin (cobra)
 ↓
1.9  Tests
 ↓
1.10 Documentation
```

---

## Dependencies & Decisions Needed

| # | Decision | Options | Status |
|---|----------|---------|--------|
| D1 | SQLite driver | `modernc.org/sqlite` (no CGO) vs `mattn/go-sqlite3` (CGO) | ⬜ Pending |
| D2 | CLI framework | cobra vs custom | ⬜ Pending |
| D3 | FE framework | React+Vite+Tailwind (recommended) vs Svelte vs htmx | ⬜ Pending |
| D4 | FE embedding | `go:embed` (single binary) vs separate static serve | ⬜ Pending |
| D5 | Metrics | Prometheus via `promhttp` or custom | ⬜ Pending |
| D6 | Auth for admin API | JWT vs static token vs session cookie | ⬜ Pending |
