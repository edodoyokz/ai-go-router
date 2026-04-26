# Deferred Tasks Audit — 9router-go

**Generated:** 2026-04-26  
**Purpose:** Comprehensive audit of all 101 deferred tasks from implementation-plan.md

---

## Executive Summary

**Total Deferred Tasks:** 101  
**Distribution:**
- Phase 2 (Operator Usability): 29 tasks
- Phase 3 (Smarter Routing): 31 tasks
- Phase 4 (Advanced Platform): 38 tasks
- Phase 5 (Production Hardening): 3 tasks

**Critical Dependencies Identified:**
1. **Runtime Config Mutation Foundation** — blocks 18 admin CRUD tasks
2. **OAuth Infrastructure** — blocks 9 provider-specific OAuth tasks
3. **Frontend Project Setup** — blocks 15 UI-related tasks
4. **Provider Adapter Framework** — blocks 8 additional provider tasks
5. **Caching Infrastructure** — blocks 3 cache-related tasks

---

## Stage 0: Current State Analysis

### Already Implemented (Partial)
These tasks are marked deferred but have GET endpoints working:
- `2.2.1` GET /api/providers ✅ (POST/PUT/DELETE deferred)
- `2.2.2` GET /api/combos ✅ (POST/PUT/DELETE deferred)
- `2.2.3` GET /api/keys ✅ (POST/PUT/DELETE deferred)
- `2.2.4` GET /api/models/alias ✅ (POST/PUT/DELETE deferred)
- `2.2.5` GET /api/models/custom ✅ (POST/PUT/DELETE deferred)
- `2.2.6` GET /api/settings ✅ (PUT deferred)

### Infrastructure Gaps
1. **No runtime config mutation** — all config is static from YAML
2. **No config persistence layer** — changes can't be saved back to YAML
3. **No multi-key support** — single API key only
4. **No custom models schema** — config structure doesn't exist
5. **No OAuth token storage** — no encrypted storage mechanism

---

## Dependency-Ordered Implementation Plan

### Stage 1: Runtime Config Mutation Foundation (HIGH PRIORITY)

**Goal:** Enable safe runtime modification of config with persistence

**Tasks:**
1. Design `RuntimeConfig` wrapper with RWMutex for thread-safe access
2. Implement config validation for runtime updates
3. Add YAML persistence layer (save modified config back to file)
4. Add config reload mechanism with rollback on validation failure
5. Wire runtime config into Server and Engine

**Blocks:** All POST/PUT/DELETE admin APIs (18 tasks)

**Estimated Complexity:** Medium (3-4 files, ~500 lines)

---

### Stage 2: Admin CRUD APIs (HIGH PRIORITY)

**Depends on:** Stage 1 (Runtime Config Mutation)

**Tasks:**
- `2.2.1` POST/PUT/DELETE /api/providers
- `2.2.2` POST/PUT/DELETE /api/combos
- `2.2.3` POST/PUT/DELETE /api/keys (requires multi-key schema)
- `2.2.4` POST/PUT/DELETE /api/models/alias
- `2.2.5` POST/PUT/DELETE /api/models/custom (requires custom models schema)
- `2.2.6` PUT /api/settings

**Implementation Order:**
1. Providers CRUD (simplest, existing schema)
2. Combos CRUD (existing schema)
3. Model Aliases CRUD (existing schema)
4. Settings Update (existing schema)
5. Keys CRUD (requires new multi-key schema)
6. Custom Models CRUD (requires new schema)

**Estimated Complexity:** Medium-High (~800 lines across handlers)

---

### Stage 3: Tool Compatibility (MEDIUM PRIORITY)

**Depends on:** None (independent feature)

**Tasks:**
- `2.6.1` Client tool detection (headers/user-agent/body)
- `2.6.2` Native passthrough (matching client↔provider)
- `2.6.3` Thinking/reasoning config handling
- `2.6.4` Outbound HTTP/SOCKS proxy support

**Implementation Order:**
1. Tool detection abstraction
2. Passthrough decision logic
3. Thinking/reasoning parameter handling
4. Outbound proxy transport configuration

**Estimated Complexity:** Medium (~400 lines)

---

### Stage 4: Minimal Web UI (MEDIUM PRIORITY)

**Depends on:** Stage 2 (Admin APIs must exist)

**Tasks:**
- `2.7.1` FE project setup (React + Vite + TailwindCSS)
- `2.7.2` Login page
- `2.7.3` Dashboard — server status, provider health
- `2.7.4` Provider management page (CRUD)
- `2.7.5` Route/combo management page (CRUD)
- `2.7.6` API key management page (CRUD)
- `2.7.7` Model alias management page (CRUD)
- `2.7.8` Request logs viewer (paginated, filterable)
- `2.7.9` Settings page
- `2.7.10` Embed FE static build into Go binary

**Implementation Order:**
1. FE project scaffold + auth
2. Dashboard + health views
3. CRUD pages (providers, routes, keys, aliases, settings)
4. Logs viewer
5. Build embedding

**Estimated Complexity:** High (~2000+ lines FE code)

---

### Stage 5: Deferred Backend Tests (HIGH PRIORITY)

**Depends on:** Stages 1-4 (test what's implemented)

**Tasks:**
- `2.8.1` Admin CRUD API tests
- `2.8.2` Multi-account rotation tests
- `2.8.3` Streaming tests
- `2.8.4` `/v1/responses` compatibility test
- `2.8.5` Tool detection tests
- `2.8.6` FE component tests

**Estimated Complexity:** Medium (~600 lines test code)

---

### Stage 6: Usage, Pricing, Adaptive Routing (MEDIUM PRIORITY)

**Depends on:** None (independent feature)

**Tasks:**
- `3.1.1` Usage fetching from provider APIs
- `3.1.2` Quota snapshot storage
- `3.1.3` Pricing data model
- `3.1.4` Cost tracking per request
- `3.1.5` Advanced retry policy (adaptive)

**Estimated Complexity:** Medium-High (~500 lines)

---

### Stage 7: OAuth and Token Management (HIGH COMPLEXITY)

**Depends on:** Encrypted storage design

**Tasks:**
- `3.2.1` OAuth token refresh mechanism
- `3.2.2` Token expiry buffer config
- `3.2.3` Refresh retry with backoff
- `4.1.1` OAuth 2.0 flow (authorization code)
- `4.1.2` Token storage (encrypted)
- `4.1.3` Token refresh mechanism
- `4.1.4-4.1.8` Provider-specific OAuth (GitHub, Google, Anthropic, Azure, Kiro)

**Estimated Complexity:** Very High (~1500+ lines)

---

### Stage 8: Additional Providers and Translators (MEDIUM PRIORITY)

**Tasks:**
- `3.3.1` Gemini adapter
- `3.3.2` Codex adapter
- `3.3.3` Qwen adapter
- `3.3.4` Kimi adapter
- `3.3.8` Ollama adapter
- `3.4.1` OpenAI ↔ Gemini translator
- `3.4.2` OpenAI ↔ Ollama translator
- `3.4.3` OpenAI-Responses ↔ OpenAI translator
- `3.4.4` Provider-specific headers system
- `4.4.1-4.4.8` Additional proprietary/web providers

**Estimated Complexity:** High (~1200 lines)

---

### Stage 9: Additional Endpoints and Media (MEDIUM PRIORITY)

**Tasks:**
- `3.5.1` POST /v1/embeddings
- `3.5.2` Tunnel support (Cloudflare)
- `3.5.3` Tunnel support (Tailscale)
- `4.3.1` POST /v1/audio/speech (TTS)
- `4.3.2` POST /v1/images/generations
- `4.3.3` TTS provider adapters
- `4.3.4` Image provider adapters

**Estimated Complexity:** High (~800 lines)

---

### Stage 10: Caching and Platform Features (MEDIUM PRIORITY)

**Tasks:**
- `3.6.1` Response cache design
- `3.6.2` In-memory LRU cache
- `3.6.3` Cache hit/miss metrics
- `4.2.1` MITM proxy server
- `4.2.2` CLI tool auto-configuration
- `4.2.3-4.2.6` Cloaking features
- `4.5.1-4.5.7` Platform features (import/export, cloud sync, policy engine, etc.)

**Estimated Complexity:** Very High (~2000+ lines)

---

### Stage 11: Production Hardening (HIGH PRIORITY)

**Tasks:**
- `5.1.2` Config hot-reload (SIGHUP)
- `5.1.5` Rate limiting (internal, per-client key)
- `5.1.6` Circuit breaker per provider
- `5.1.7` Health check deep (provider connectivity)

**Estimated Complexity:** Medium (~400 lines)

---

## Implementation Sequence (First Batch)

**Immediate Focus:** Stages 1-2 (Foundation + Admin APIs)

1. ✅ Stage 0: Audit complete
2. 🔄 Stage 1: Runtime config mutation (NEXT)
3. ⏳ Stage 2: Admin CRUD APIs
4. ⏳ Stage 3: Tool compatibility
5. ⏳ Stage 5: Backend tests for Stages 1-3

**Success Criteria:**
- All admin POST/PUT/DELETE endpoints functional
- Config changes persist to YAML
- Runtime updates don't require server restart
- Full test coverage for new APIs

---

## Risk Assessment

**High Risk:**
- OAuth implementation (complex flows, security-sensitive)
- MITM proxy (networking complexity, certificate handling)
- Cloaking features (anti-detection, may violate ToS)
- Web scraping adapters (fragile, maintenance burden)

**Medium Risk:**
- Frontend embedding (build pipeline complexity)
- Caching (invalidation strategy, memory management)
- Multi-key auth (migration from single key)

**Low Risk:**
- Admin CRUD APIs (straightforward REST)
- Tool compatibility (detection heuristics)
- Additional providers (follow existing patterns)

---

## Notes

- Some "deferred" items may already have partial implementation
- Streaming infrastructure exists but tests are deferred
- Multi-account system is implemented but rotation tests deferred
- Several OpenAI-compatible providers work via `openai_compat` type

**Next Action:** Begin Stage 1 implementation (Runtime Config Mutation)
