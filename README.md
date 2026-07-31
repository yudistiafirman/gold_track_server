# gold-track-be

Fondasi backend Go dengan arsitektur 3-layer: **Handler → Service → Repository**.

## Struktur

```
cmd/api/main.go          entrypoint: load config, connect DB, wire layer, start server
cmd/migrate/main.go      CLI migrasi database (golang-migrate + pgx)
migrations/              file SQL migrasi, satu tabel per file (up & down)
internal/config/         baca konfigurasi dari env (+ .env untuk lokal)
internal/logger/         structured logger (log/slog)
internal/database/       koneksi pool PostgreSQL (pgxpool)
internal/middleware/     logging, error-handling (recover), JWT auth & role check global
internal/repository/     akses data (query ke PostgreSQL)
internal/service/        business logic
internal/handler/        HTTP handler + router (chi)
pkg/apperror/            tipe error terpusat (status code + pesan aman)
pkg/response/            format response JSON konsisten
```

Alur tiap fitur mengikuti: `handler` menerima request → panggil `service` →
`service` panggil `repository` → hasil/­error mengalir balik lewat `pkg/response`
dan `pkg/apperror`.

## Strategi ID: BIGINT internal + public_id (UUID) eksternal

Semua tabel resource (semua kecuali `token_blacklist`) punya dua identifier:

- **`id BIGSERIAL`** — PK internal, dipakai untuk FK/join antar tabel (cepat, kecil, bagus buat
  laporan/transaksi yang banyak join). **Tidak pernah** keluar ke API, URL, response JSON, atau JWT.
- **`public_id UUID NOT NULL DEFAULT gen_random_uuid()`** (unique) — satu-satunya identifier yang
  dilihat klien: dipakai di path param URL (`/api/users/{id}` isinya UUID), di body response JSON
  (field `"id"`), dan di dalam klaim `user_id` JWT. Mencegah enumerasi resource lewat id sekuensial
  (`/users/1`, `/users/2`, dst).

Postgres 13+ punya `gen_random_uuid()` built-in di core — tidak perlu `CREATE EXTENSION pgcrypto`.

Pola buat tabel/endpoint baru: query repository selalu `SELECT ... public_id::text ...` (cast eksplisit
supaya aman di-scan ke Go `string`, terlepas dari codec UUID pgx), lalu di layer `service`/`handler`
hanya `public_id` yang dipetakan ke DTO/response — struct `model.X` (yang punya `ID int64` internal)
tidak pernah dikembalikan langsung ke handler.

## Menjalankan lokal

```bash
cp .env.example .env
docker compose up -d          # start PostgreSQL lokal
go run ./cmd/api
curl http://localhost:8080/health
```

## Migrasi database

Perintah dijalankan dari root project (butuh `migrations/` di working directory), koneksi DB
diambil dari env yang sama dengan `cmd/api`:

```bash
go run ./cmd/migrate up            # apply semua migrasi yang belum jalan
go run ./cmd/migrate down          # rollback semua (ke schema kosong)
go run ./cmd/migrate down 1        # rollback 1 langkah terakhir
go run ./cmd/migrate steps 2       # maju 2 langkah (atau -2 untuk mundur 2 langkah)
go run ./cmd/migrate version       # lihat versi migrasi saat ini
go run ./cmd/migrate force 5       # paksa set versi tanpa jalankan SQL (recovery dari state "dirty")
```

Tabel `schema_migrations` dibuat otomatis untuk melacak versi yang sudah diterapkan.
Urutan file (`000001`…`000015`) mengikuti dependency FK: `users` → `suppliers` /
`customers` / `expense_categories` → `products` / `gold_prices` → `purchase_orders` →
`purchase_order_items` → `stock_items` → `transactions` → `transaction_items` →
`expenses` → `settings` → `stock_opnames` → `stock_opname_items`.

Menambah migrasi baru: buat pasangan file
`migrations/{next_number}_{deskripsi}.up.sql` dan `.down.sql`.

## Seeder data awal

```bash
go run ./cmd/seed
```

Membuat (kalau belum ada — aman dijalankan berkali-kali, tidak duplikat):
- 1 user `SUPER_ADMIN` (email/password dari `SEED_ADMIN_*`, default `admin@goldtrack.local` / `ChangeMe123!` — **ganti setelah login pertama**), password disimpan sebagai bcrypt hash.
- 7 `expense_categories` default: Listrik, Wifi/Internet, ATK, Gaji Karyawan, Sewa Tempat, Transportasi, Lain-lain.
- 3 `settings` data toko (`shop_name`, `shop_address`, `shop_phone`) dari `SEED_SHOP_*`.

Data toko yang sudah pernah diubah lewat aplikasi tidak akan ditimpa ulang oleh seeder (`ON CONFLICT DO NOTHING`); password admin yang sudah ada juga tidak direset di run berikutnya.

## API

### POST /api/auth/login

```bash
curl -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@goldtrack.local","password":"ChangeMe123!"}'
```

Sukses (200):
```json
{"success":true,"data":{"token":"<jwt>","user":{"id":1,"name":"Super Admin","role":"SUPER_ADMIN"}}}
```

Gagal (401, berlaku sama untuk email tidak ada, password salah, maupun akun nonaktif —
sengaja disamakan supaya tidak membocorkan status akun):
```json
{"success":false,"error":{"code":"UNAUTHORIZED","message":"Email atau password salah"}}
```

Token JWT (HS256) berisi `user_id`, `role`, `jti`, `exp` (masa berlaku dari `JWT_EXPIRY`, default 24h),
ditandatangani dengan `JWT_SECRET`. `last_login_at` di-update setiap login sukses.

### POST /api/auth/logout

```bash
curl -X POST http://localhost:8080/api/auth/logout \
  -H "Authorization: Bearer <jwt>"
```

Sukses (200): `{"success":true,"data":{"message":"logout berhasil"}}`

Karena JWT stateless, invalidasi beneran butuh blacklist sisi server: `jti` token
disimpan di tabel `token_blacklist` sampai waktu expiry aslinya. Setiap request
(termasuk logout berikutnya) yang bawa token dengan `jti` itu akan ditolak 401
`"Token tidak valid atau sudah tidak berlaku"` — jadi token yang sama tidak bisa
dipakai dua kali setelah logout. Request tanpa header `Authorization: Bearer <jwt>`
yang valid juga ditolak 401.

### /api/users — CRUD user (SUPER_ADMIN saja)

Semua route ini butuh `Authorization: Bearer <jwt>` DAN role `SUPER_ADMIN` (role lain → 403).

```bash
GET    /api/users        # list semua user
POST   /api/users        # { name, email, password, role }         -> 201
GET    /api/users/{id}   # detail user                              -> 200 / 404
PUT    /api/users/{id}   # { name, email, role, is_active, password? } -> 200 / 404 / 409
DELETE /api/users/{id}   # soft delete (is_active=false)             -> 200 / 404
```

`{id}` di URL adalah `public_id` (UUID, contoh `097bbdc9-6e81-4af1-a167-705f2970a30b`), bukan
angka sekuensial — format lain langsung ditolak 400 sebelum sempat query ke DB.

Catatan:
- `password` di response **tidak pernah** dikembalikan.
- `email` unik — konflik → 409.
- `password` di `PUT` opsional; kosongkan untuk mempertahankan password lama.
- `DELETE` adalah **soft delete** (`is_active=false`), bukan hapus baris — semua tabel lain
  referensi `users.id` lewat FK tanpa `ON DELETE CASCADE`, jadi hard delete akan gagal begitu
  user itu pernah membuat data apa pun. Reaktivasi lewat `PUT` dengan `is_active: true`.
- SUPER_ADMIN tidak bisa menonaktifkan akun sendiri lewat `DELETE` (mencegah lockout).

## Middleware JWT & role (internal/middleware/auth.go)

- `appmw.JWTAuth(authService)` — verifikasi Bearer token (signature, expiry, dan status
  blacklist). Token hilang/rusak/kedaluwarsa/sudah di-blacklist → 401. Kalau lolos, claims
  (`user_id`, `role`, `jti`) disimpan di request context lewat `appmw.ClaimsFromContext(ctx)`.
- `appmw.RequireRole("ADMIN", "SUPER_ADMIN", ...)` — jalan setelah `JWTAuth`, cek `claims.Role`
  ada di daftar role yang diizinkan. Role tidak cocok → 403.

Cara pakai di route baru (lihat `internal/handler/router.go`):
```go
r.Group(func(r chi.Router) {
    r.Use(appmw.JWTAuth(authService))
    r.Get("/products", productHandler.List)                          // semua role login

    r.Group(func(r chi.Router) {
        r.Use(appmw.RequireRole("ADMIN", "SUPER_ADMIN"))
        r.Post("/products", productHandler.Create)                   // ADMIN/SUPER_ADMIN saja
    })
})
```

`POST /api/auth/login` sengaja **tidak** dipasangi `JWTAuth` — klien belum punya token saat
login. `POST /api/auth/logout` sudah jadi contoh nyata endpoint terproteksi (perlu token valid,
role bebas). Endpoint bisnis lain (products, stock, dst.) akan dipasangi middleware yang sama
saat dibuat di ticket berikutnya.

## Konfigurasi (env)

Lihat `.env.example` untuk daftar lengkap. Semua diambil dari environment
variable; file `.env` otomatis dibaca saat development (lihat `internal/config`).

## Menambah fitur baru

1. `internal/repository/xxx_repository.go` — interface + implementasi query.
2. `internal/service/xxx_service.go` — logic, panggil repository.
3. `internal/handler/xxx_handler.go` — terima request, panggil service, balas via `pkg/response`.
4. Daftarkan route di `internal/handler/router.go`.
