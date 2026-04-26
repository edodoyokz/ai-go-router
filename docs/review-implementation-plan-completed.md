# Review Implementasi Item Selesai

Status: ✅ Verified complete

## Scope

Dokumen ini mereview item yang ditandai selesai pada `docs/implementation-plan.md`, dengan fokus pada kesesuaian klaim implementasi terhadap perilaku runtime, konsistensi state, kualitas observability, dan kecukupan test/dokumentasi.

## Ringkasan

Secara umum, banyak item backend inti memang sudah terpasang dan dapat dikompilasi, termasuk endpoint utama, admin CRUD, persistence SQLite, dan jalur streaming. Namun, beberapa item yang sudah diberi status selesai masih memiliki gap fungsional yang material. Temuan paling penting adalah ketidak-atomikan update konfigurasi runtime, potensi desinkronisasi state saat persist gagal, serta jalur streaming yang melewati logika routing resiliency utama. Selain itu, ada gap antara klaim dokumentasi/plan dengan implementasi aktual, khususnya pada `/readyz`, referensi API, dan config reference.

## Temuan Kritis

1. **Update konfigurasi runtime masih shallow-copy dan tidak atomik.**
   `internal/config/runtime.go:35` membuat salinan dengan `newConfig := rc.config`, sehingga map/slice di dalam `Config` masih berbagi referensi. Ini membuka peluang mutasi parsial pada state lama saat update gagal atau saat data turunan ikut berubah. Klaim `UpdateAndPersist` sebagai atomik juga tidak akurat karena update memori dilakukan lebih dulu lalu persist ke disk belakangan di `internal/config/runtime.go:57`.

2. **Admin CRUD dapat membuat memori/auth/router/disk keluar sinkron jika persist atau reconfigure gagal.**
   Banyak handler mengubah runtime config, lalu memanggil `Persist()`, lalu `reconfigureFromRuntimeConfig()` secara terpisah, misalnya `internal/api/server.go:349`, `internal/api/server.go:353`, dan `internal/api/server.go:357`. Jika persist gagal, state memori sudah berubah tetapi file YAML belum ikut berubah. Jika reconfigure gagal, auth/runtime config bisa berubah sementara engine router belum mengikuti. Ini berdampak langsung ke endpoint provider/combo/key/model/settings yang diklaim selesai pada fase 2.

3. **Jalur streaming melewati fallback/retry/cooldown/model-lock.**
   Jalur non-streaming memakai `s.engine.ChatCompletion(...)` di `internal/api/server.go:1042`, tetapi streaming langsung memanggil `ResolveTargets()` lalu memilih target pertama dan adapter pertama di `internal/api/server.go:1136`, `internal/api/server.go:1152`, dan `internal/api/server.go:1169`. Dengan desain ini, retry, fallback chain, cooldown tracker, dan model lock yang ada di engine tidak berlaku untuk request streaming.

4. **`/readyz` selalu ready walau plan mengklaim cek SQLite/provider.**
   `docs/implementation-plan.md:67` menyebut enhancement untuk cek SQLite dan konektivitas provider, tetapi implementasi `internal/api/server.go:173` hanya mengembalikan `{"status":"ready"}` tanpa verifikasi apa pun. Test yang ada di `internal/api/server_test.go:127` juga hanya memastikan status 200, bukan readiness aktual.

## Temuan Menengah

1. **Request log dan usage counter menyimpan model alias yang diminta, bukan target upstream yang benar-benar dipakai.**
   Pada jalur non-streaming, `TargetModel` diisi dari `request.Model` pada `internal/api/server.go:1062` dan `internal/api/server.go:1098`, lalu usage juga diinkremen dengan `request.Model` pada `internal/api/server.go:1106`. Pola yang sama muncul di `/v1/messages` dan `/v1/responses` pada `internal/api/server.go:1307`, `internal/api/server.go:1368`, `internal/api/server.go:1376`, `internal/api/server.go:1480`, dan `internal/api/server.go:1533`. Akibatnya `request_logs` dan `usage_counters` di `internal/storage/db.go:147` dan `internal/storage/db.go:164` tidak selalu merepresentasikan target upstream yang sebenarnya.

2. **CLI `router logs` punya bug `db-path` dan risiko duplikasi pada follow mode.**
   Default `dbPath` diambil dari `configPath` pada `cmd/router/main.go:120`, sehingga bila user memakai config non-default, command bisa mencoba membuka file YAML sebagai SQLite database. Selain itu, `followLogs()` memakai `lastTime := time.Now().Unix()` dan filter berbasis detik di `cmd/router/main.go:183` serta `cmd/router/main.go:190`; banyak log pada detik yang sama berisiko terlewat atau tercetak ulang.

3. **README sudah stale terhadap fitur yang ditandai selesai.**
   `README.md:127` masih menaruh `/v1/models`, `/v1/messages`, SQLite persistence, streaming, dan CLI admin dalam bagian “Planned next”, padahal `docs/implementation-plan.md` menandai item-item itu selesai. Ini menurunkan kepercayaan pada status proyek dan bisa membingungkan operator baru.

4. **`docs/api-reference.md` salah mendokumentasikan format respons `/v1/messages`.**
   Dokumen menyatakan respons diterjemahkan ke format OpenAI pada `docs/api-reference.md:124`, sementara implementasi nyata mengubah respons OpenAI kembali ke format Claude lewat translator di `internal/api/server.go:1330` dan mengembalikannya sebagai objek Claude di `internal/api/server.go:1381`.

5. **`docs/config-reference.md` sudah stale dan sebagian tidak valid.**
   File ini masih menyebut default config `./config/config.yaml` pada `docs/config-reference.md:7`, padahal CLI default memakai `./config/config.example.yaml` di `cmd/router/main.go:26`. Beberapa field dan contoh juga tidak lagi selaras dengan schema runtime terkini, termasuk representasi retry/settings/provider yang tidak lengkap atau tidak akurat terhadap implementasi sekarang.

## Temuan Rendah

1. **CORS `Access-Control-Max-Age` dibentuk dengan konversi rune, bukan string angka.**
   `internal/api/middleware.go:257` memakai `string(rune(maxAge))`, yang menghasilkan karakter Unicode tunggal, bukan nilai numerik HTTP header seperti `600`.

2. **Klaim testing untuk endpoint selesai masih lebih kuat daripada cakupan test aktual.**
   Sejumlah endpoint yang ditandai selesai belum terlihat memiliki test langsung yang spesifik, terutama untuk `/v1/responses`, `/api/providers/{name}/health`, `/api/providers/{name}/accounts/{account}/health`, `/metrics`, dan beberapa cabang error/admin edge case. Test readiness yang ada juga sangat dangkal di `internal/api/server_test.go:127`.

## Gap Testing dan Dokumentasi

- Banyak endpoint selesai belum punya direct handler/integration tests yang memverifikasi kontrak HTTP, body, error mapping, dan efek persistence.
- Streaming completion belum punya integration test end-to-end yang memakai provider uji nyata/mock HTTP SSE. Test yang ada di `internal/providers/streaming_test.go:11` hanya menguji parser scanner SSE, bukan integrasi fallback, cancellation, header, flushing, atau translasi chunk lintas provider.
- `docs/api-reference.md` dan `docs/config-reference.md` perlu diselaraskan ulang dengan implementasi yang benar-benar berjalan.
- `README.md` perlu diperbarui agar status fitur tidak kontradiktif dengan `docs/implementation-plan.md`.

## Perbaikan Prioritas

1. **Perbaiki fondasi runtime config.** Gunakan deep-copy untuk seluruh struktur mutable dan satukan validasi + persist + publish state agar benar-benar atomik dari perspektif memori dan disk.
2. **Buat jalur admin CRUD transactional.** Hindari pola update memori lebih dulu tanpa rollback; idealnya commit ke snapshot baru, persist, lalu swap runtime/engine/auth secara konsisten atau rollback penuh saat salah satu langkah gagal.
3. **Satukan jalur streaming dengan mesin routing resiliency.** Streaming harus melewati fallback, retry policy, cooldown, dan model-lock yang sama dengan non-streaming, atau status implementasinya perlu diturunkan dari “selesai”.
4. **Implementasikan readiness check yang nyata.** `/readyz` sebaiknya memeriksa akses SQLite dan minimal status dependensi penting yang benar-benar dijanjikan plan.
5. **Perbaiki observability dan akurasi data.** Simpan alias yang diminta dan target upstream yang ter-resolve sebagai dua field berbeda untuk log, usage, dan output CLI.
6. **Rapikan CLI/logging/docs/tests.** Benahi bug `router logs`, koreksi `Access-Control-Max-Age`, lalu tambahkan test langsung untuk endpoint selesai dan integration test streaming.

## Kesimpulan

Status “completed” pada `docs/implementation-plan.md` cukup mencerminkan bahwa banyak bagian sudah ada secara struktural, tetapi belum selalu production-safe atau sesuai klaim perilaku. Tiga area paling mendesak adalah atomisitas runtime mutation, konsistensi admin CRUD, dan kesenjangan resiliency pada streaming. Setelah itu, proyek perlu menutup gap readiness, observability, dokumentasi, dan pengujian agar status selesai benar-benar dapat dipertanggungjawabkan.
