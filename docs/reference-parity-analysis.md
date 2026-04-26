# Analisis Paritas Referensi 9router vs 9router-go

## Ruang Lingkup & Metode

Laporan ini membandingkan implementasi `9router-go` dengan aplikasi referensi di `reference/9router` untuk menilai kedalaman paritas fungsional, kontrak FE↔BE, modul lanjutan, dan bukti pengujian. Fokus analisis berada pada:

- permukaan API dan dashboard,
- kontrak frontend React/Vite terhadap backend Go,
- modul lanjutan seperti OAuth, policy, nodes, sync, MITM, tunnel, updater, i18n, dan cache,
- kekuatan bukti test versus klaim implementasi.

Metode yang dipakai:

1. Membaca dokumentasi referensi: `reference/9router/README.md` dan `reference/9router/docs/ARCHITECTURE.md`.
2. Membaca implementasi utama backend saat ini: `internal/api/server.go`, `internal/app/app.go`, `internal/config/config.go`, serta modul-modul lanjutan terkait.
3. Membaca frontend saat ini: `web/src/api.js` dan `web/src/pages/Settings.jsx`.
4. Mengaudit bukti pengujian dari file `*_test.go` yang tersedia di `internal/` dan memeriksa area yang tidak memiliki test.
5. Memeriksa keselarasan antara klaim rencana implementasi di `docs/implementation-plan.md` dan bukti kode/test yang benar-benar ada.

## Ringkasan Eksekutif

Secara backend inti, `9router-go` sudah kuat pada fondasi router OpenAI-compatible, translasi format, config runtime, storage, logging, dan cache. Bukti untuk area ini cukup nyata melalui test pada router, translator, config, storage, error taxonomy, dan cache, misalnya di `internal/router/router_test.go`, `internal/router/integration_test.go`, `internal/translator/translators_test.go`, `internal/config/config_test.go`, `internal/storage/db_test.go`, `internal/providers/errors_test.go`, dan `internal/cache/cache_test.go`.

Namun, bila dibandingkan dengan aplikasi referensi, paritas produk secara keseluruhan masih belum dalam. Aplikasi referensi mendeskripsikan proses lokal yang menyajikan dashboard manajemen luas di root/dashboard plus banyak domain API manajemen di bawah `/api/*` dan kompatibilitas `/v1/*` (`reference/9router/README.md`, `reference/9router/docs/ARCHITECTURE.md`). Sebaliknya, backend Go saat ini menaruh UI embedded hanya di `/ui/*` melalui `r.Handle("/ui/*", http.StripPrefix("/ui", webui.Handler()))` (`internal/api/server.go:149`) dan menyediakan surface API dashboard yang lebih sempit.

Ada mismatch kontrak FE↔BE yang signifikan dan langsung memengaruhi kegunaan UI saat ini:

- `web/src/api.js:17` memanggil `/api/metrics` dan selalu mem-parsing JSON, tetapi backend mengembalikan Prometheus plaintext melalui `handleMetrics` dengan `Content-Type: text/plain; version=0.0.4` (`internal/api/server.go:195`, `internal/api/server.go:1081`).
- Frontend tidak mengirim header auth sama sekali (`web/src/api.js:3-13`), sementara mayoritas route `/api/*` dibungkus `AuthMiddlewareWithRuntimeConfig` (`internal/api/server.go:157-199`).
- Frontend memakai base route kosong `const BASE = ''` dan asumsi route aplikasi di `/` (`web/src/api.js:1`, `web/src/pages/Settings.jsx`), sedangkan backend hanya me-mount UI di `/ui/*` (`internal/api/server.go:148-149`, `internal/webui/embed.go:1-37`).
- Endpoint settings GET hanya mengembalikan shape sempit, sementara halaman settings mengedit field lebih banyak termasuk `locale`, `native_passthrough`, dan `thinking.*` (`internal/api/server.go:966-996`, `web/src/pages/Settings.jsx:75-137`, `internal/config/config.go:210-223`).
- FE mengekspor helper `/api/nodes` (`web/src/api.js:33`) tetapi route tersebut tidak ada di `internal/api/server.go`.

Kesimpulannya: paritas fondasi gateway cukup baik, tetapi paritas produk end-to-end terhadap referensi masih parsial. Area paling mendesak bukan sekadar menambah modul baru, melainkan menyelaraskan kontrak dashboard, route base, auth flow UI, dan surface admin/API agar frontend yang sudah ada dapat benar-benar bekerja.

## Tabel Status Singkat

| Area | Status | Notes |
| --- | --- | --- |
| Routing inti, fallback, alias | Strong | Engine dan test kuat; lihat `internal/router/router_test.go` dan `internal/router/integration_test.go`. |
| Translasi format | Strong | Request/response translation ditest nyata di `internal/translator/translators_test.go`. |
| Config runtime & validation | Strong | Struktur config dan runtime reload punya coverage baik di `internal/config/config.go`, `internal/config/config_test.go`, `internal/config/runtime_test.go`. |
| Storage/logging dasar | Strong | DB dan async writer cukup terbukti via `internal/storage/db_test.go`. |
| Cache | Strong | Area advanced terkuat; implementasi + test komprehensif di `internal/cache/cache_test.go`. |
| Dashboard/API manajemen | Partial | Ada CRUD inti, tetapi permukaan lebih sempit dari referensi dan beberapa route hilang di `internal/api/server.go`. |
| FE↔BE contract | Missing/Unproven | Mismatch JSON metrics, auth, base route, settings shape, dan `/api/nodes`. |
| OAuth | Scaffold | Storage/refresh/exchange ada di `internal/oauth/oauth.go`, wiring API/dashboard masih minim. |
| Policy engine | Scaffold | Engine ada di `internal/policy/policy.go`, bukti integrasi penggunaan belum terlihat. |
| Nodes | Partial | Registry/health ada dan startup wired, tetapi API/routing parity belum ada; RR tampak bug di `internal/nodes/nodes.go:137-140`. |
| Usage/pricing | Partial | Pricing registry dan fetcher ada, tetapi bukti usage fetch lemah dan Anthropic placeholder. |
| Sync | Scaffold | Backup/restore periodik ada di `internal/sync/sync.go`, belum setara sync API/merge referensi. |
| MITM | Partial | Proxy HTTP/CONNECT ada di `internal/mitm/mitm.go`, parity cloaking/HTTPS MITM penuh belum terbukti. |
| Tunnel | Scaffold | Launcher subprocess ada di `internal/tunnel/tunnel.go`, belum ada status/control/reconnect/persistence parity. |
| Updater | Partial | Self-update CLI ada di `internal/updater/updater.go` dan `cmd/router/update.go`, parity UX/robustness belum penuh. |
| i18n | Scaffold | Catalog ada di `internal/i18n/i18n.go`, bukti penggunaan produk belum terlihat. |
| FE tests | Missing/Unproven | Tidak ada file test di `web/`. |
| Advanced module tests | Missing/Unproven | Tidak ada test langsung untuk oauth/mitm/tunnel/sync/policy/nodes/updater/i18n/webui. |

## Temuan per Kategori

### 1. Surface produk dan arsitektur dashboard/API

Aplikasi referensi secara eksplisit memosisikan dashboard sebagai bagian utama dari runtime lokal. README referensi menyebut dashboard terbuka di root host dan default URL dashboard adalah `http://localhost:20128/dashboard` (`reference/9router/README.md:75`, `reference/9router/README.md:109-112`). Arsitektur referensi juga memetakan banyak domain manajemen ke `src/app/api/*`, termasuk auth/settings, providers, provider nodes, OAuth, keys/aliases/combos/pricing, usage, sync/cloud, dan helper CLI (`reference/9router/docs/ARCHITECTURE.md:97-120`).

Implementasi Go saat ini menyajikan UI embedded hanya di `/ui/*` dan redirect `/ui` → `/ui/` (`internal/api/server.go:147-149`). Public route juga hanya `healthz`, `readyz`, dan `metrics`; sisanya berada di route protected tertentu (`internal/api/server.go:151-199`). Ini menunjukkan paritas produk untuk dashboard/admin belum seluas referensi, walaupun fondasi server HTTP dan protected admin CRUD sudah ada.

### 2. Kontrak frontend terhadap backend

Frontend saat ini masih belum sinkron dengan backend Go pada beberapa titik kontrak utama:

- `web/src/api.js` selalu memanggil `fetch()` lalu `return res.json()` (`web/src/api.js:3-13`). Ini gagal untuk `/api/metrics` karena backend mengarahkan route itu ke `handleMetrics`, yang mengembalikan plaintext Prometheus (`internal/api/server.go:195`, `internal/api/server.go:1081`).
- Frontend tidak mengirim bearer/admin token apa pun (`web/src/api.js:4-7`), padahal hampir seluruh `/api/*` dibungkus middleware auth (`internal/api/server.go:157-199`).
- Frontend menggunakan `const BASE = ''` (`web/src/api.js:1`), sehingga semua route diasumsikan dari root app. Ini tidak konsisten dengan UI embedded yang hanya dilayani di `/ui/*` (`internal/api/server.go:148-149`).
- Halaman settings mengharapkan shape data yang dapat diedit langsung untuk `locale`, `native_passthrough`, `combo_strategy`, dan nested `thinking` (`web/src/pages/Settings.jsx:75-137`). Tetapi GET settings saat ini hanya mengembalikan tiga field: `combo_strategy`, `outbound_proxy_enabled`, dan `outbound_proxy_url` (`internal/api/server.go:966-975`), walaupun `config.SettingsConfig` sebenarnya mendukung field yang lebih luas (`internal/config/config.go:210-223`).
- Frontend mengekspos helper `nodes: () => req('/api/nodes')` (`web/src/api.js:33`), tetapi route `/api/nodes` tidak terdaftar di `internal/api/server.go:157-199`.

Mismatch ini cukup untuk menyimpulkan bahwa FE existing belum dapat berfungsi penuh di atas backend Go tanpa adaptasi kontrak yang sengaja.

### 3. Modul lanjutan

Modul lanjutan pada `9router-go` umumnya sudah memiliki scaffold atau core building block, tetapi banyak yang belum mencapai paritas produk referensi yang lebih matang. Detail audit per modul dibahas pada bagian khusus di bawah.

### 4. Bukti pengujian

Test evidence kuat pada area core gateway, tetapi sangat tipis pada area dashboard end-to-end dan hampir tidak ada pada modul lanjutan. Selain itu, beberapa klaim di `docs/implementation-plan.md` tampak lebih optimistis daripada bukti test aktualnya.

## Paritas Backend

Pada level gateway inti, backend Go sudah memiliki banyak komponen yang relevan dengan referensi:

- route kompatibilitas utama seperti `/v1/models`, `/v1/chat/completions`, `/v1/messages`, `/v1/responses`, `/v1/embeddings`, `/v1/audio/speech`, dan `/v1/images/generations` sudah ada di `internal/api/server.go:159-165`;
- translasi format sudah menjadi komponen eksplisit lewat registry translator di `Server` dan test translasi request/response di `internal/translator/translators_test.go`;
- routing fallback/alias/round-robin inti sudah punya engine tersendiri dan coverage yang cukup baik di `internal/router/router_test.go` dan `internal/router/integration_test.go`.

Namun dibanding referensi, backend parity masih lebih sempit pada dimensi dashboard/manajemen:

- Referensi menyebut domain API manajemen luas, termasuk auth/settings/providers/provider nodes/oauth/keys/aliases/combos/pricing/usage/sync/cloud/cli tools (`reference/9router/docs/ARCHITECTURE.md:111-120`).
- Backend Go memang punya CRUD providers/combos/keys/aliases/custom models/settings/logs/usage/pricing dan token listing OAuth (`internal/api/server.go:166-198`), tetapi belum menunjukkan parity yang setara untuk auth dashboard, provider nodes API, sync/cloud control API, dan helper domain lain.
- UI route base juga berbeda: referensi menaruh dashboard di root-facing path seperti `/dashboard` (`reference/9router/README.md:109-112`), sedangkan Go menaruh embedded UI hanya di `/ui/*` (`internal/api/server.go:148-149`).

Jadi, backend/API parity dapat dinilai kuat untuk core router, tetapi hanya parsial untuk permukaan produk/dashboard yang lebih luas.

## Mismatch Kontrak FE↔BE

### Metrics: FE mengharap JSON, BE mengirim plaintext

- FE: `api.metrics: () => req('/api/metrics')` dan `req()` selalu `return res.json()` (`web/src/api.js:3-18`).
- BE: `/api/metrics` diarahkan ke `handleMetrics` (`internal/api/server.go:195`), dan handler menetapkan `Content-Type: text/plain; version=0.0.4` (`internal/api/server.go:1081`).

Dampak: halaman/komponen yang memanggil metrics akan gagal parse response.

### Auth: FE tanpa header, BE melindungi mayoritas route

- FE tidak menyertakan `Authorization` pada helper default (`web/src/api.js:4-7`).
- BE membungkus hampir seluruh route `/api/*` dan `/v1/*` dengan `AuthMiddlewareWithRuntimeConfig` (`internal/api/server.go:157-199`).

Dampak: UI dashboard saat ini kemungkinan akan mendapat 401/403 untuk sebagian besar operasi selain public routes.

### Route base: FE mengasumsikan root, BE mount UI di `/ui/*`

- FE `const BASE = ''` dan semua path absolut root (`web/src/api.js:1`, `web/src/api.js:16-33`).
- BE hanya menyajikan SPA di `/ui/*` (`internal/api/server.go:148-149`), dengan fallback client-side routing pada handler embedded (`internal/webui/embed.go:13-36`).

Dampak: jika asset/runtime FE dibangun dengan asumsi root `/`, navigasi, asset resolution, dan panggilan API berpotensi salah ketika UI dilayani dari `/ui/`.

### Settings shape: GET lebih sempit daripada editor FE

- FE Settings mengedit `locale`, `native_passthrough`, `combo_strategy`, dan `thinking.enabled/max_tokens/include_reasoning` (`web/src/pages/Settings.jsx:75-137`).
- Struct backend memang mendukung field-field ini di `config.SettingsConfig` (`internal/config/config.go:210-223`).
- Tetapi GET settings hanya mengembalikan `combo_strategy`, `outbound_proxy_enabled`, dan `outbound_proxy_url` (`internal/api/server.go:966-975`).

Dampak: UI tidak mendapatkan state penuh untuk field yang bisa diedit; sebagian control akan fallback ke default lokal, bukan nilai backend aktual.

### `/api/nodes` helper FE tidak punya route backend

- FE menyediakan `nodes()` (`web/src/api.js:33`).
- Route `/api/nodes` tidak muncul pada registrasi route di `internal/api/server.go:157-199`.

Dampak: fitur node pada FE tidak akan bekerja terhadap backend saat ini.

## Audit Modul Lanjutan

### OAuth

Area ini punya fondasi yang nyata tetapi belum mencapai parity produk referensi.

Yang sudah ada:

- encrypted token storage berbasis SQLite di `internal/oauth/oauth.go:48-168`;
- token refresh di `internal/oauth/oauth.go:226-306`;
- authorization code exchange di `internal/oauth/oauth.go:308-362`.

Yang belum terlihat setara:

- API wiring dashboard yang luas untuk authorize/exchange/poll/callback; backend saat ini hanya mengekspos list/delete token di `internal/api/server.go:197-198`;
- parity alur browser callback/PKCE seperti yang lazim pada onboarding OAuth referensi (`reference/9router/docs/ARCHITECTURE.md:242-260` menggambarkan lifecycle authorize/exchange/poll dari UI ke API OAuth);
- test khusus package OAuth tidak ditemukan.

Status: `Scaffold`.

### Policy

`internal/policy/policy.go` menyediakan engine allow/deny/reroute/tag yang cukup rapi (`internal/policy/policy.go:11-120`). Namun hasil grep hanya menunjukkan definisi engine dan helper `ApplyToRequest`, tanpa bukti penggunaan nyata di request path server/router. Tidak ada test package policy dan tidak terlihat wiring di `internal/api/server.go` atau engine runtime.

Status: `Scaffold`.

### Nodes

Yang sudah ada:

- registry node, health check, forward, dan list/status di `internal/nodes/nodes.go`;
- startup wiring health check dari config di `internal/app/app.go:126-140`.

Masalah utama:

- tidak ada route `/api/nodes` walau FE mengharapkannya (`web/src/api.js:33`, `internal/api/server.go:157-199`);
- belum terlihat integrasi node forwarding ke routing path utama;
- ada indikasi bug round-robin: `Forward()` mendeklarasikan `var globalIdx atomic.Uint64` secara lokal setiap pemanggilan, lalu langsung `Add(1)` (`internal/nodes/nodes.go:137-140`). Karena variabel ini lokal, indeks kemungkinan reset pada tiap call dan round-robin global tidak benar-benar berlangsung.

Status: `Partial`.

### Usage/Pricing

Yang sudah ada:

- pricing registry dan default pricing diinisialisasi saat `NewServer()` (`internal/api/server.go:95-115`);
- package usage memiliki pricing dan fetcher serta route `GET /api/usage` dan `GET /api/pricing` (`internal/api/server.go:191`, `internal/api/server.go:196`).

Keterbatasan:

- `internal/usage/fetcher_test.go` sendiri mengakui fetcher masih memakai hardcoded URL dan test sukses OpenAI sebenarnya tidak menguji server mock yang dibuat; test lalu memanggil API nyata dengan invalid key dan hanya memverifikasi bahwa error muncul (`internal/usage/fetcher_test.go:23-60`);
- Anthropic usage masih placeholder dengan error `anthropic usage API not yet implemented` (`internal/usage/fetcher_test.go:76-88`);
- dibanding referensi yang menaruh usage/cost tracking sebagai capability sentral (`reference/9router/docs/ARCHITECTURE.md:17-19`, `reference/9router/docs/ARCHITECTURE.md:136-148`), area ini masih lebih dangkal.

Status: `Partial`.

### Sync

`internal/sync/sync.go` menyediakan manager untuk backup periodik dan restore file melalui HTTPS/S3-like endpoint (`internal/sync/sync.go:30-178`), dan startup wiring ada di `internal/app/app.go:143-156`. Ini berguna sebagai fondasi.

Tetapi referensi menggambarkan control route sync/cloud khusus (`reference/9router/docs/ARCHITECTURE.md:119`, `reference/9router/docs/ARCHITECTURE.md:157-162`) dan cloud sync sebagai bagian orkestrasi produk. Pada implementasi Go, belum terlihat API sync yang setara, status endpoint, maupun merge semantics/state reconciliation ala referensi.

Status: `Scaffold`.

### MITM

Yang sudah ada:

- proxy reverse HTTP dan CONNECT tunneling di `internal/mitm/mitm.go:32-197`;
- startup wiring di `internal/app/app.go:110-123`;
- cloaking helper ada di `internal/mitm/cloaking.go` menurut grep.

Kesenjangan:

- dari `internal/mitm/mitm.go`, HTTPS CONNECT saat ini lebih berupa tunnel/redirect TCP ke upstream (`internal/mitm/mitm.go:107-152`), bukan bukti jelas dari full HTTPS interception parity end-to-end dengan sertifikat dinamis/browser-grade flow;
- belum tampak bukti bahwa cloaking benar-benar diterapkan pada jalur request proxy aktif;
- tidak ada test package MITM.

Status: `Partial`.

### Tunnel

`internal/tunnel/tunnel.go` hanya menunjukkan manager subprocess untuk `cloudflared` dan `tailscale funnel`, plus `Stop()` sederhana (`internal/tunnel/tunnel.go:15-130`). Ini cukup sebagai bootstrap, tetapi belum setara dengan parity operasional yang lebih matang seperti status runtime, start/stop API, reconnect, observability, atau persistence sesi/config.

Status: `Scaffold`.

### Updater

Yang sudah ada:

- self-update via GitHub releases API di `internal/updater/updater.go:66-186`;
- CLI command `update` di `cmd/router/update.go:10-31`.

Keterbatasan:

- meski komentar file menyebut verifikasi checksum, implementasi yang terbaca belum memperlihatkan checksum asset/signature validation nyata (`internal/updater/updater.go:1-5` vs implementasi `applyUpdate` di `internal/updater/updater.go:128-186`);
- tidak ada progress feedback, resumable download, atau relaunch flow;
- `isNewer()` memakai simple string comparison (`internal/updater/updater.go:201-207`), yang rapuh untuk versi semver multi-digit.

Status: `Partial`.

### i18n

`internal/i18n/i18n.go` menyediakan katalog `en/id/zh/ja` dan helper translasi (`internal/i18n/i18n.go:12-171`). `config.SettingsConfig` juga punya field `Locale` (`internal/config/config.go:210-217`). Namun pencarian kode tidak menunjukkan penggunaan nyata `i18n.T()` pada permukaan HTTP/UI yang dianalisis. Dengan demikian, i18n lebih tepat dinilai sebagai aset fondasi, bukan fitur produk terintegrasi.

Status: `Scaffold`.

### Cache

Ini adalah area advanced yang relatif paling kuat.

- server menginisialisasi `cache.NewLRUCache(1000)` di `NewServer()` (`internal/api/server.go:91-116`);
- package cache punya coverage yang luas untuk set/get, TTL, eviction, order, delete, clear, stats, dan clean expired di `internal/cache/cache_test.go`.

Tetap saja, dibanding referensi, perilaku cache produk bisa jadi masih lebih sempit. Namun berdasarkan bukti yang tersedia, cache adalah modul lanjutan paling matang dan paling terbukti.

Status: `Strong`.

## Audit Bukti Pengujian

### Yang punya bukti kuat

Beberapa area core memiliki test yang nyata dan relevan:

- router dan fallback: `internal/router/router_test.go`, `internal/router/integration_test.go`;
- klasifikasi error provider: `internal/providers/errors_test.go`;
- translator: `internal/translator/detector_test.go`, `internal/translator/translators_test.go`;
- config dan runtime config: `internal/config/config_test.go`, `internal/config/runtime_test.go`;
- storage: `internal/storage/db_test.go`;
- cache: `internal/cache/cache_test.go`.

Ini cukup untuk menyatakan fondasi gateway non-UI sudah memiliki bukti yang solid.

### Yang buktinya lemah atau dangkal

- endpoint `responses`, `embeddings`, `audio/speech`, dan `images/generations` di `internal/api/server_test.go` sebagian besar hanya smoke test parsing dan menerima kegagalan provider sebagai hasil yang masih diterima (`internal/api/server_test.go:404-517`);
- admin CRUD test memang ada, tetapi masih sebatas smoke path create/update/delete tanpa coverage kontrak UI, auth middleware, atau edge cases penting (`internal/api/server_test.go:335-402`);
- streaming evidence di `internal/providers/streaming_test.go` hanya menguji scanner/tokenizer SSE level rendah, bukan perilaku streaming end-to-end provider/router/API (`internal/providers/streaming_test.go:11-148`);
- usage fetch tests lemah karena tidak benar-benar menggunakan mock server yang telah disiapkan dan cenderung hanya mengharapkan error (`internal/usage/fetcher_test.go:23-70`).

### Yang belum punya bukti langsung

Tidak ditemukan bukti test untuk:

- frontend (`web/` tidak memiliki file test);
- oauth;
- mitm;
- tunnel;
- sync;
- policy;
- nodes;
- updater;
- i18n;
- web UI embedding;
- banyak route HTTP management secara integration-level penuh.

Karena itu, beberapa klaim di `docs/implementation-plan.md` perlu dibaca hati-hati. Contohnya:

- `4.7.1 OAuth flow tests` ditandai ✅ tetapi catatannya hanya menyebut package compile dan integration tests deferred (`docs/implementation-plan.md:396`);
- `4.7.2 MITM proxy tests` ditandai ✅ tetapi catatannya hanya menyebut package compile dan logic unit-testable (`docs/implementation-plan.md:397`);
- beberapa area advanced lain juga ditandai ✅ walaupun bukti test langsung tidak terlihat.

Kesimpulan audit test: coverage inti cukup baik, tetapi coverage produk lengkap dan advanced modules masih belum terbukti.

## Yang Sudah Kuat

- Fondasi router/fallback/alias sudah matang relatif terhadap scope gateway inti, dengan coverage nyata di `internal/router/router_test.go` dan `internal/router/integration_test.go`.
- Translasi format request/response sudah menjadi komponen eksplisit dan teruji di `internal/translator/translators_test.go`.
- Konfigurasi runtime cukup kaya dan tervalidasi, termasuk settings, retry, nodes, sync, mitm, dan tunnel pada level schema di `internal/config/config.go`, didukung test config/runtime.
- Persistence/logging dasar sudah ada, termasuk async writer dan query logs yang diuji di `internal/storage/db_test.go` serta `internal/api/server_test.go`.
- Cache adalah area advanced paling kuat, baik dari implementasi maupun bukti test, di `internal/cache/*`.
- Backend sudah menyediakan banyak endpoint compatibility modern di luar chat dasar: `/v1/responses`, `/v1/embeddings`, `/v1/audio/speech`, `/v1/images/generations` (`internal/api/server.go:161-165`), walau bukti kedalaman implementasinya belum merata.

## Yang Masih Parsial/Scaffolded

- Surface dashboard/admin ada, tetapi belum seluas referensi dan belum sinkron penuh dengan FE (`internal/api/server.go`, `reference/9router/docs/ARCHITECTURE.md`).
- OAuth sudah punya storage/refresh/exchange, tetapi wiring onboarding lengkap dan test belum ada (`internal/oauth/oauth.go`, `internal/api/server.go:197-198`).
- Nodes punya registry dan health checks, tetapi belum terintegrasi penuh ke API/routing serta memiliki indikasi bug round-robin (`internal/nodes/nodes.go`, `internal/app/app.go:126-140`).
- Usage/pricing ada tetapi fetch evidence lemah dan dukungan provider belum dalam (`internal/usage/fetcher_test.go`).
- Sync, tunnel, updater, i18n, dan kemungkinan cloaking MITM masih lebih cocok disebut scaffold operasional daripada parity produk lengkap.

## Yang Masih Hilang

- Kontrak FE↔BE yang benar-benar operasional untuk dashboard saat ini: auth header flow, route base `/ui`, JSON metrics, shape settings penuh, dan route `/api/nodes`.
- Parity dashboard root/dashboard path seperti referensi (`reference/9router/README.md:75`, `reference/9router/README.md:109-112`).
- API domain yang lebih kaya seperti sync/cloud control, auth dashboard, dan provider nodes management seperti yang digambarkan arsitektur referensi (`reference/9router/docs/ARCHITECTURE.md:111-120`, `reference/9router/docs/ARCHITECTURE.md:157-162`).
- Bukti test untuk advanced modules dan frontend.
- Parity streaming end-to-end yang kuat; bukti saat ini baru di level parser SSE.
- Integrasi nyata policy engine dan i18n ke jalur request/response atau dashboard.

## Rencana Aksi Prioritas

1. **Selaraskan kontrak FE↔BE yang memblokir dashboard saat ini**
   - Ubah `/api/metrics` agar memiliki endpoint JSON terpisah untuk dashboard atau sesuaikan FE agar tidak memanggil `res.json()` untuk metrics.
   - Tambahkan mekanisme auth FE ke semua request admin.
   - Putuskan satu base path resmi untuk UI: root/dashboard atau `/ui`, lalu konsistenkan build/runtime FE dan backend mount.
   - Perluas `handleSettingsGet` agar mengembalikan shape penuh yang memang diedit FE (`locale`, `native_passthrough`, `thinking`, dst.).
   - Tambahkan `/api/nodes` atau hapus ekspektasi FE tersebut sementara.

2. **Tutup gap produk dashboard terhadap referensi**
   - Audit permukaan API manajemen referensi dari `reference/9router/docs/ARCHITECTURE.md:111-120` lalu petakan mana yang wajib untuk MVP parity dashboard.
   - Prioritaskan auth/settings/providers/nodes/oauth/usage/sync control yang paling berdampak ke UX dashboard.

3. **Perbaiki integrasi modul nodes**
   - Betulkan bug round-robin lokal di `internal/nodes/nodes.go:137-140` dengan state indeks persisten di registry.
   - Tambahkan route admin `/api/nodes` dan integrasi ke routing/forward path bila memang diinginkan sebagai fitur produk.
   - Tambahkan test nodes unit + integration.

4. **Naikkan OAuth dari scaffold ke fitur operasional**
   - Tambahkan authorize/exchange/callback API yang lengkap.
   - Putuskan dukungan PKCE/browser callback yang dibutuhkan.
   - Tambahkan test store, refresh, dan minimal integration flow HTTP.

5. **Koreksi klaim dan tambah bukti test**
   - Revisi `docs/implementation-plan.md` agar status advanced modules membedakan `implemented`, `wired`, dan `tested`.
   - Tambahkan test FE minimal untuk helper API/Settings page.
   - Tambahkan integration tests untuk route HTTP utama dan advanced modules yang sudah diklaim selesai.

6. **Matangkan modul advanced yang sudah ada fondasinya**
   - Usage/pricing: injeksi base URL/mockable fetcher, implementasi Anthropic, dan test nyata.
   - MITM: buktikan cloaking terhubung ke jalur aktif dan tambahkan test behavior utama.
   - Tunnel/updater/sync/i18n: tambahkan status/control surface dan pastikan tiap modul punya use-path yang jelas di produk.

## Kesimpulan

`9router-go` sudah memiliki inti gateway yang cukup kuat dan lebih terstruktur secara Go untuk routing, translasi, config, storage, dan cache. Jika tolok ukurnya adalah core local AI router, proyek ini sudah berada pada level yang menjanjikan.

Tetapi bila tolok ukurnya adalah paritas mendalam terhadap aplikasi referensi `9router` sebagai produk lengkap, status saat ini masih `Partial` secara keseluruhan. Gap terbesar bukan hanya jumlah modul yang belum selesai, melainkan ketidaksinkronan kontrak antara frontend dan backend serta belum matangnya permukaan dashboard/admin dibanding referensi. Prioritas tertinggi adalah menyatukan route base, auth, shape response dashboard, dan API admin yang benar-benar dipakai FE; setelah itu baru modul lanjutan seperti OAuth, nodes, sync, dan MITM dapat dikejar dengan dasar produk yang konsisten.
