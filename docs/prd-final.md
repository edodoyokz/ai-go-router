# PRD Final — 9Router Clone (Go Edition)

## 1. Project Overview

**Working name:** `9router-go`  
**Type:** Local-first AI model router / proxy / fallback gateway  
**Primary goal:** Membangun clone fungsional dari konsep 9router dengan pendekatan **Go-first** agar **lebih cepat jadi, lebih ringan dari stack Node/Next, dan lebih mudah dioperasikan sebagai daemon**.

Produk ini akan menjadi **gateway lokal / self-hosted** yang menerima request dari tools AI coding dan productivity yang mendukung endpoint OpenAI-compatible, lalu merutekan request tersebut ke provider/model terbaik berdasarkan aturan fallback, quota, availability, dan preferensi user.

---

## 2. Product Vision

Menyediakan router AI yang:
- ringan dijalankan di laptop, VPS kecil, atau mini PC
- cepat dibangun dan diiterasikan
- stabil untuk long-running process
- mendukung fallback otomatis antar provider/model
- memudahkan penggunaan model subscription, murah, dan gratis dalam satu endpoint lokal

Fokus utama bukan meniru semua fitur 9router sekaligus, tetapi membangun **versi yang lebih lean, cepat usable, dan production-practical**.

---

## 3. Problem Statement

Tool seperti Codex, Claude Code, OpenClaw, Cursor, Cline, dan tool lain sering hanya butuh:
- satu endpoint lokal yang stabil
- model alias yang konsisten
- fallback otomatis saat quota habis / provider gagal
- transparansi log dan routing

Masalah dari workflow biasa:
- user harus pindah provider manual
- quota subscription terbuang
- coding session berhenti saat kena limit/error
- biaya sulit dikontrol
- konfigurasi tersebar di banyak tool
- stack web modern JS cenderung boros memory untuk daemon yang harus hidup lama

---

## 4. Goals

### Primary Goals
1. Membuat local AI router berbasis Go yang cepat usable.
2. Menyediakan endpoint OpenAI-compatible untuk tool eksternal.
3. Mendukung fallback chain antar provider/model.
4. Mengurangi memory footprint dibanding implementasi Node/Next-style.
5. Menyediakan persistence sederhana untuk config, usage, dan logs.
6. Dapat dijalankan sebagai single binary di localhost atau VPS.

### Secondary Goals
1. Menyediakan admin UI ringan.
2. Menyediakan observability dasar: logs, metrics, health.
3. Menyediakan dasar untuk multi-account dan quota tracking.

---

## 5. Non-Goals (Phase 1)

Hal-hal berikut **tidak wajib** masuk versi awal:
- cloud sync
- team collaboration / multi-user RBAC kompleks
- distributed cluster mode
- billing engine
- provider count puluhan sejak awal
- dashboard yang sangat polished
- format translation lengkap untuk semua provider di fase pertama (MVP hanya wajib OpenAI ↔ Anthropic)
- mobile app
- full parity dengan 9router sejak rilis pertama

---

## 6. Target Users

### Primary Users
- solo developer
- AI power user
- coder yang pakai beberapa provider/model
- operator OpenClaw / CLI tools yang butuh 1 endpoint stabil

### Secondary Users
- small engineering team
- self-hosting enthusiast
- VPS users dengan resource terbatas

---

## 7. Core Use Cases

1. **As a developer**, saya ingin mengarahkan Codex / OpenClaw / Cursor ke satu endpoint lokal agar tidak perlu ganti config tiap provider.
2. **As a user**, saya ingin request otomatis fallback ke model lain saat provider utama gagal atau quota habis.
3. **As an operator**, saya ingin melihat request log, error, dan provider mana yang dipakai.
4. **As a cost-conscious user**, saya ingin mengutamakan subscription / murah / gratis dalam urutan tertentu.
5. **As a self-hoster**, saya ingin menjalankan sistem ini sebagai single binary dengan memory footprint ringan.

---

## 8. Product Scope

## Phase 1 — MVP
MVP harus fokus pada nilai inti:

### Included
- local HTTP server
- OpenAI-compatible API endpoint
- provider abstraction layer
- **format translation minimal (OpenAI ↔ Anthropic Messages)**
- routing by alias/model dengan **tier-based fallback (subscription → cheap → free)**
- fallback chain
- basic retry policy
- streaming passthrough dasar
- config file
- SQLite persistence
- request logging dasar
- health endpoint
- admin auth sederhana
- minimal web UI atau CLI admin

### Excluded
- full OAuth provider zoo
- complex analytics
- cloud sync
- multi-tenant
- advanced policy engine

---

## 9. Functional Requirements

## 9.1 API Gateway
Sistem harus menyediakan endpoint yang kompatibel dengan format OpenAI-compatible minimal:
- `POST /v1/chat/completions`
- `GET /healthz`
- `GET /readyz`
- `GET /metrics` (opsional di MVP, dianjurkan)

Versi awal boleh membatasi scope ke `chat/completions` lebih dulu.

### Expected behavior
- menerima request JSON dari client tool
- memvalidasi auth internal API key
- resolve model alias / combo
- memilih provider target
- **menerjemahkan request** ke format native provider (jika bukan OpenAI-compatible)
- meneruskan request ke provider
- **menerjemahkan response** dari format native provider ke format OpenAI-compatible
- mengembalikan response yang sesuai ke client

---

## 9.2 Routing Engine
Sistem harus mendukung:
- direct model route
- alias route
- combo route (prioritas berantai)
- **tier-based routing (subscription → cheap → free)**
- fallback berdasarkan error/quota/timeout

### Tier System
Setiap provider/account memiliki `tier` yang menentukan urutan preferensi:
- **subscription** — provider berbayar dengan quota bulanan (Claude Pro, ChatGPT Plus)
- **cheap** — provider murah per-token (DeepSeek, Kimi, MiniMax)
- **free** — provider gratis sebagai safety net (iFlow, Qwen free tier)

Router akan otomatis fallback mengikuti urutan tier saat quota habis atau error terjadi.

Contoh:
```yaml
routes:
  coding-default:
    - provider: anthropic
      model: claude-sonnet
      tier: subscription
    - provider: deepseek
      model: deepseek-chat
      tier: cheap
    - provider: openai_compat
      model: qwen-coder
      tier: free
```

### Routing triggers
Fallback dapat dipicu oleh:
- HTTP 429
- timeout
- koneksi gagal
- auth expired tertentu
- provider unavailable
- explicit quota exhausted signal

---

## 9.3 Provider Support
### MVP providers
Wajib ada minimal:
1. **OpenAI-compatible provider** (passthrough, tanpa translation)
2. **Anthropic provider** (dengan translation OpenAI format → Anthropic Messages API)

### Nice-to-have MVP+
3. **Gemini provider**

Tujuannya agar arsitektur sudah modular, tapi delivery tetap cepat.

### 9.3.1 Format Translation
Karena client selalu mengirim format OpenAI-compatible, setiap provider adapter bertanggung jawab menerjemahkan request/response:
- **OpenAI adapter:** passthrough (tidak perlu translation)
- **Anthropic adapter:** translate OpenAI Chat Completions → Anthropic Messages API, dan sebaliknya untuk response
- **Provider lain (Phase 2+):** implementasi translation sesuai kebutuhan

Translation mencakup:
- request body mapping (messages format, system prompt handling, tool/function calls)
- response body mapping (content blocks, usage, stop reason)
- streaming event translation (SSE format differences)

---

## 9.4 Streaming
Sistem harus mendukung streaming minimal untuk request yang membutuhkan low-latency output.

### MVP rule
- support streaming passthrough
- jika translation penuh sulit di awal, prioritaskan provider yang format stream-nya paling mudah diadaptasi
- fallback setelah partial stream dimulai **tidak wajib** di MVP

### Explicit constraint
Jika stream sudah mulai dikirim ke client, request dianggap committed ke provider aktif.

---

## 9.5 Config Management
Sistem harus memiliki config yang mudah diedit manual.

### Format
Pilih salah satu:
- YAML
- TOML

**Recommendation:** YAML untuk readability.

### Config content
- bind address / port
- admin password hash / token
- API key internal
- provider definitions
- routes / combos
- timeout defaults
- retry policy defaults
- log retention settings
- SQLite path

---

## 9.6 Persistence
Gunakan **SQLite** untuk durability ringan.

### Data yang disimpan
- providers
- accounts / credentials metadata
- route definitions (opsional bisa dari file)
- request logs metadata
- usage counters
- quota snapshots
- auth/session sederhana

### Non-requirement
Tidak perlu langsung menyimpan full request/response body permanen secara default.

---

## 9.7 Logging
Sistem harus menyimpan log operasional dasar:
- timestamp
- request id
- route alias
- provider yang dipilih
- model final
- response status
- latency
- token usage jika tersedia
- error reason jika gagal

### Debug mode
Harus ada mode debug untuk troubleshooting.

---

## 9.8 Authentication
### Client auth
- gunakan internal API key untuk akses endpoint router

### Admin auth
- password-based login sederhana atau static admin token
- phase awal tidak perlu RBAC kompleks

---

## 9.9 Admin Interface
Ada dua opsi implementasi awal:

### Opsi A — CLI-first
- `router run`
- `router config validate`
- `router providers list`
- `router routes list`
- `router logs tail`

### Opsi B — Minimal Web UI
- login
- dashboard status
- provider list
- route list
- recent requests
- config viewer

**Recommended delivery order:** CLI-first, lalu UI tipis.

---

## 10. Non-Functional Requirements

## 10.1 Performance
- startup cepat
- low idle memory
- mampu menangani concurrent requests ringan-menengah
- latency overhead router harus rendah

### Initial target
- idle memory target: **< 100 MB** untuk mode core-only
- dengan UI ringan: target tetap jauh lebih rendah daripada stack Next.js setara

## 10.2 Reliability
- request ID unik
- graceful shutdown
- retry/fallback deterministic
- panic recovery pada HTTP layer

## 10.3 Security
- secrets tidak hardcoded
- credentials terenkripsi atau minimal dipisahkan aman
- audit logging dasar
- admin endpoint tidak terbuka tanpa auth
- bind default ke localhost

## 10.4 Portability
- single binary Linux first
- build cross-platform bila memungkinkan
- mudah dijalankan di systemd / Docker

---

## 11. Technical Direction

## 11.1 Language
**Go** dipilih karena:
- lebih cepat delivery daripada Rust
- cukup hemat memory dibanding Node
- networking / proxy / concurrency sangat cocok
- deployment simpel sebagai single binary
- maintenance jangka menengah lebih ringan

## 11.2 Suggested Stack
- HTTP router: `chi` atau `net/http`
- HTTP client: `net/http` + custom transport
- storage: `sqlite` via `modernc.org/sqlite` atau `mattn/go-sqlite3`
- config: `yaml.v3`
- logging: `zerolog`
- CLI: `cobra`
- metrics: `prometheus/client_golang`
- templates UI: `templ` atau `html/template`

### Recommendation
- Core: `net/http` + `chi`
- Logs: `zerolog`
- DB: SQLite
- CLI: `cobra`
- UI: `html/template` dulu kalau mau super cepat

---

## 12. Proposed Architecture

```text
+---------------------+
| AI Client Tools     |
| Codex/OpenClaw/etc  |
+----------+----------+
           |
           v
+---------------------+
| HTTP API Layer      |
| Auth, validation    |
+----------+----------+
           |
           v
+---------------------+
| Routing Engine      |
| alias/combo/fallback|
+-----+---------+-----+
      |         |
      v         v
+----------+ +----------------+
| Provider | | Provider       |
| Adapter  | | Adapter        |
| OpenAI   | | Anthropic/...  |
+----------+ +----------------+
      |              |
      +------+-------+
             |
             v
+---------------------+
| Persistence         |
| SQLite + config     |
+---------------------+
```

### Main modules
- `cmd/router`
- `internal/api`
- `internal/auth`
- `internal/router`
- `internal/providers`
- `internal/storage`
- `internal/config`
- `internal/logging`
- `internal/metrics`
- `internal/admin`

---

## 13. API / Behavior Specification

## 13.1 Incoming Request Lifecycle
1. client kirim request ke local endpoint
2. API key diverifikasi
3. model/alias dibaca
4. route chain di-resolve
5. provider pertama dipilih
6. request diteruskan
7. bila gagal dengan error retryable, fallback ke provider berikutnya
8. response dikirim ke client
9. usage/log dicatat

## 13.2 Error Handling Rules
### Retryable
- network timeout
- connection refused
- 429
- 5xx tertentu
- quota exhausted

### Non-retryable
- invalid client request
- invalid schema
- unauthorized dari router internal
- unsupported model alias

---

## 14. UX Requirements

## 14.1 Setup UX
User harus bisa:
1. download binary
2. jalankan init/config
3. tambahkan provider
4. set combo route
5. copy endpoint + API key ke tool AI

## 14.2 Observability UX
User harus bisa cepat tahu:
- router hidup atau tidak
- request terakhir sukses/gagal
- provider yang sedang aktif
- fallback terjadi atau tidak

---

## 15. Success Metrics

### MVP success
- tool eksternal bisa menggunakan endpoint tanpa perubahan besar
- fallback chain bekerja di skenario dasar
- request log terlihat jelas
- idle memory signifikan lebih rendah dari stack Node/Next setara
- dapat dipakai harian untuk routing pribadi

### Example measurable targets
- setup pertama < 10 menit
- 95% request normal sukses tanpa intervention
- fallback sukses untuk error retryable dasar
- idle RAM core-only < 100 MB target awal

---

## 16. Risks

## 16.1 Technical Risks
1. streaming translation antar provider bisa rumit
2. format request/response provider berbeda
3. quota semantics antar provider tidak konsisten
4. auth refresh bisa berbeda-beda per provider
5. fallback setelah partial stream mulai hampir pasti kompleks

## 16.2 Product Risks
1. scope melebar karena ingin feature parity penuh dengan 9router
2. UI menghabiskan waktu terlalu banyak di fase awal
3. terlalu banyak provider di fase awal memperlambat delivery

### Mitigation
- fokus MVP kecil
- provider modular
- streaming minimal dulu
- CLI-first bila UI menghambat

---

## 17. Roadmap

## Phase 0 — Spec + Skeleton
- finalize PRD
- define config schema
- define provider interface
- define DB schema
- setup project skeleton

## Phase 1 — MVP Core
- OpenAI-compatible endpoint
- alias routing
- fallback chain
- OpenAI-compatible adapter
- Anthropic adapter
- request logs
- YAML config
- SQLite persistence
- CLI admin basic

## Phase 2 — Operator Usability
- minimal web UI
- metrics endpoint
- log viewer
- route editor
- provider health checks
- **multi-account per provider (round-robin rotation)**
- **`/v1/responses` endpoint (Codex CLI compatibility)**

## Phase 3 — Smarter Routing
- quota tracking
- advanced retry policy
- Gemini adapter
- usage analytics
- **response caching (optimized per-tool, e.g. Claude Code cache)**
- **Ollama format support (self-hosted model compatibility)**

## Phase 4 — Advanced Platform
- OAuth flows
- config import/export
- cloud sync optional
- policy engine

---

## 18. MVP Deliverables

### Code deliverables
- Go module initialized
- runnable HTTP server
- `/v1/chat/completions`
- config loader
- SQLite integration
- provider interface
- two provider adapters minimum (dengan format translation OpenAI ↔ Anthropic)
- routing + fallback logic dengan tier-based routing
- request logs
- health endpoint
- CLI commands basic

### Docs deliverables
- README
- config example
- local run guide
- provider setup guide
- architecture note

---

## 19. Open Decisions

Hal yang masih perlu diputuskan:
1. ~~`chi` vs `net/http` only~~ → **Decided: `chi`** (sudah diimplementasi)
2. ~~YAML vs TOML~~ → **Decided: YAML** (sudah diimplementasi)
3. ~~UI fase 1 langsung ada atau CLI-only dulu~~ → **Decided: CLI-first**
4. `modernc` SQLite vs CGO SQLite
5. log retention default berapa hari
6. apakah metrics masuk MVP wajib atau MVP+

---

## 20. Final Product Decision

Proyek ini akan dibangun sebagai:

- **Bahasa:** Go
- **Prioritas:** cepat usable, hemat resource, mudah deploy
- **Target awal:** local-first single-binary AI router
- **MVP focus:** routing + fallback + observability dasar
- **Strategi delivery:** bangun core dulu, UI tipis belakangan

---

## 21. One-Sentence Product Definition

> `9router-go` adalah local AI routing gateway berbasis Go yang menyediakan satu endpoint OpenAI-compatible dengan fallback otomatis, logging, dan konfigurasi ringan agar tool AI bisa tetap berjalan cepat, murah, dan stabil tanpa overhead stack web berat.
