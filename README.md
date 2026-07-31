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
Urutan file (`000001`…`000016`) mengikuti dependency FK: `users` → `suppliers` /
`customers` / `expense_categories` → `products` / `gold_prices` → `purchase_orders` →
`purchase_order_items` → `stock_items` → `transactions` → `transaction_items` →
`expenses` → `settings` → `stock_opnames` → `stock_opname_items` → `token_blacklist`.
`000017` (`categories`) dan `000018` (`brands`) berdiri sendiri, lalu `000019` meng-ALTER
`products` supaya `category_id`/`brand_id` FK ke keduanya (ganti kolom `category`/`brand` lama).

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

Contoh response `POST /api/users` (201):
```json
{
  "success": true,
  "data": {
    "id": "097bbdc9-6e81-4af1-a167-705f2970a30b",
    "name": "Kasir Baru",
    "email": "kasir-baru@goldtrack.local",
    "role": "KASIR",
    "is_active": true,
    "last_login_at": null,
    "created_at": "2026-07-31T09:00:00Z",
    "updated_at": "2026-07-31T09:00:00Z"
  }
}
```

Contoh response `GET /api/users` (200) — array objek yang sama:
```json
{
  "success": true,
  "data": [
    {
      "id": "097bbdc9-6e81-4af1-a167-705f2970a30b",
      "name": "Kasir Baru",
      "email": "kasir-baru@goldtrack.local",
      "role": "KASIR",
      "is_active": true,
      "last_login_at": null,
      "created_at": "2026-07-31T09:00:00Z",
      "updated_at": "2026-07-31T09:00:00Z"
    }
  ]
}
```

Contoh response error:
```json
// 403 — role selain SUPER_ADMIN
{"success":false,"error":{"code":"FORBIDDEN","message":"Anda tidak memiliki akses untuk aksi ini"}}

// 404 — public_id valid tapi tidak ada
{"success":false,"error":{"code":"NOT_FOUND","message":"user tidak ditemukan"}}

// 409 — email sudah dipakai
{"success":false,"error":{"code":"CONFLICT","message":"email sudah dipakai"}}

// 400 — {id} bukan format UUID
{"success":false,"error":{"code":"BAD_REQUEST","message":"id tidak valid"}}
```

Catatan:
- `password` di response **tidak pernah** dikembalikan.
- `email` unik — konflik → 409.
- `password` di `PUT` opsional; kosongkan untuk mempertahankan password lama.
- `DELETE` adalah **soft delete** (`is_active=false`), bukan hapus baris — semua tabel lain
  referensi `users.id` lewat FK tanpa `ON DELETE CASCADE`, jadi hard delete akan gagal begitu
  user itu pernah membuat data apa pun. Reaktivasi lewat `PUT` dengan `is_active: true`.
- SUPER_ADMIN tidak bisa menonaktifkan akun sendiri lewat `DELETE` (mencegah lockout).

### /api/categories & /api/brands — master data (ADMIN & SUPER_ADMIN)

Semua route ini butuh `Authorization: Bearer <jwt>` DAN role `ADMIN` atau `SUPER_ADMIN`
(role lain → 403). Dipakai FE buat dropdown/autocomplete kategori & brand produk.

```bash
GET    /api/categories        # list semua kategori
POST   /api/categories        # { name }                    -> 201
GET    /api/categories/{id}   # detail kategori               -> 200 / 404
PUT    /api/categories/{id}   # { name, is_active }           -> 200 / 404 / 409
DELETE /api/categories/{id}   # soft delete (is_active=false) -> 200 / 404

GET    /api/brands             # list semua brand
POST   /api/brands             # { name }                    -> 201
GET    /api/brands/{id}        # detail brand                  -> 200 / 404
PUT    /api/brands/{id}        # { name, is_active }           -> 200 / 404 / 409
DELETE /api/brands/{id}        # soft delete (is_active=false) -> 200 / 404
```

Contoh response `POST /api/categories` (201) — bentuk `brands` identik, cuma nama resource-nya beda:
```json
{
  "success": true,
  "data": {
    "id": "3fa85f64-5717-4562-b3fc-2c963f66afa6",
    "name": "Batangan",
    "is_active": true,
    "created_at": "2026-07-31T09:00:00Z",
    "updated_at": "2026-07-31T09:00:00Z"
  }
}
```

Contoh response `GET /api/categories` (200) — array objek yang sama:
```json
{
  "success": true,
  "data": [
    {
      "id": "3fa85f64-5717-4562-b3fc-2c963f66afa6",
      "name": "Batangan",
      "is_active": true,
      "created_at": "2026-07-31T09:00:00Z",
      "updated_at": "2026-07-31T09:00:00Z"
    }
  ]
}
```

Contoh response error (sama utk `/api/categories` maupun `/api/brands`):
```json
// 403 — role selain ADMIN/SUPER_ADMIN
{"success":false,"error":{"code":"FORBIDDEN","message":"Anda tidak memiliki akses untuk aksi ini"}}

// 404 — public_id valid tapi tidak ada
{"success":false,"error":{"code":"NOT_FOUND","message":"kategori tidak ditemukan"}}
// (brand: "brand tidak ditemukan")

// 409 — name sudah dipakai (case-insensitive)
{"success":false,"error":{"code":"CONFLICT","message":"nama kategori sudah dipakai"}}
// (brand: "nama brand sudah dipakai")

// 400 — {id} bukan format UUID
{"success":false,"error":{"code":"BAD_REQUEST","message":"id tidak valid"}}
```

Catatan:
- `{id}` juga `public_id` (UUID), sama seperti `/api/users`.
- `name` unik **case-insensitive** (unique index di `lower(name)`) — "Antam" dan "antam"
  dianggap sama, konflik → 409. Ini penting karena SKU produk (BE-201) nantinya diturunkan
  langsung dari `name` ini.
- `DELETE` adalah **soft delete** (`is_active=false`), tidak ada guard "tidak bisa hapus diri
  sendiri" seperti di `/api/users` karena resource ini bukan akun.
- `products.category_id`/`products.brand_id` (migration `000019`) adalah FK ke tabel ini —
  lihat `### POST /api/products` di bawah.

### POST /api/products — create produk dengan SKU auto-generate (ADMIN & SUPER_ADMIN)

```bash
POST /api/products   # { name, category_id, brand_id, weight_gram, description? } -> 201
```

`category_id`/`brand_id` di body adalah `public_id` (UUID) dari `/api/categories`/`/api/brands` —
klien harus create/pilih kategori & brand lewat endpoint itu dulu (dropdown/autocomplete FE).
Kategori/brand yang sudah `is_active=false` ditolak (400) — tidak bisa dipakai bikin produk baru.

**SKU auto-generate**, format `[KAT]-[BRAND]-[BERAT]-[URUT]`, dihitung di service layer
(`internal/service/product_service.go`):
- `[KAT]` / `[BRAND]` — 3 huruf/angka pertama nama kategori/brand, uppercase, karakter non-alfanumerik
  dibuang. Contoh: "Batangan" → `BAT`, "Antam" → `ANT`.
- `[BERAT]` — `weight_gram` apa adanya, trailing desimal nol dibuang (`10.000`→`10`, `10.500`→`10.5`).
- `[URUT]` — jumlah produk yang sudah punya prefix `KAT-BRAND-BERAT-` yang sama, +1, zero-padded
  3 digit (`001`, `002`, …). Dihitung + insert dalam **satu transaksi DB**
  (`ProductRepository.CreateWithGeneratedSKU`), dengan retry (maks 5x) kalau ada race
  concurrent-create yang kena unique constraint `uq_products_sku` — bukan cuma dihitung sekali lalu
  percaya begitu saja.

Contoh response `POST /api/products` (201):
```json
{
  "success": true,
  "data": {
    "id": "6d9f6a2a-2b0c-4e2b-8f1a-1a2b3c4d5e6f",
    "name": "Emas Batangan 10gr",
    "sku": "BAT-ANT-10-001",
    "category_id": "3fa85f64-5717-4562-b3fc-2c963f66afa6",
    "brand_id": "5c9e1a3b-8f2d-4a6e-9b1c-7d8e9f0a1b2c",
    "weight_gram": 10,
    "description": "Emas batangan Antam 10 gram",
    "is_active": true,
    "created_at": "2026-07-31T09:00:00Z",
    "updated_at": "2026-07-31T09:00:00Z"
  }
}
```
Dua produk berikutnya dengan kombinasi kategori+brand+berat yang sama akan dapat
`BAT-ANT-10-002`, `BAT-ANT-10-003`, dst.

Contoh response error:
```json
// 403 — role selain ADMIN/SUPER_ADMIN
{"success":false,"error":{"code":"FORBIDDEN","message":"Anda tidak memiliki akses untuk aksi ini"}}

// 400 — field wajib kosong
{"success":false,"error":{"code":"BAD_REQUEST","message":"name, category_id, dan brand_id wajib diisi"}}

// 400 — weight_gram <= 0
{"success":false,"error":{"code":"BAD_REQUEST","message":"weight_gram harus lebih besar dari 0"}}

// 400 — category_id/brand_id merujuk ke resource yang is_active=false
{"success":false,"error":{"code":"BAD_REQUEST","message":"kategori tidak aktif"}}

// 404 — category_id/brand_id valid UUID tapi tidak ada
{"success":false,"error":{"code":"NOT_FOUND","message":"kategori tidak ditemukan"}}
```

Catatan:
- `created_by` diambil dari `claims.UserID` (JWT), bukan dari body — tidak bisa dipalsukan klien.
- `is_active` selalu `true` saat create, tidak bisa di-set lewat body.
- Scope BE-201 cuma `POST` — belum ada `GET`/`PUT`/`DELETE` untuk `/api/products` (ticket terpisah).

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

## Testing

Dua lapis test:

- **Unit test** (`internal/**/*_test.go`, mis. `internal/middleware/auth_test.go`) — cepat, tanpa
  dependency eksternal (DB di-mock/di-skip). Jalankan:
  ```bash
  go test ./internal/...
  ```

- **E2E test** (`test/e2e/`) — jalanin router chi asli (`internal/app.New()`) di atas
  `httptest.Server`, lawan Postgres beneran (bukan mock), nembak tiap endpoint lewat HTTP
  sungguhan dari `handler` → `service` → `repository` → DB. Ini test suite yang jadi bukti
  wiring `internal/app` benar-benar nyambung ujung ke ujung.

  ```bash
  docker compose up -d postgres   # DB lokal, pastikan jalan dulu
  go test ./test/e2e/...
  go test ./test/e2e/... -v       # verbose, lihat nama tiap test
  go test ./test/e2e/... -run TestUsers_CreateListGetUpdateDelete -v   # satu test spesifik
  ```

  Database test (`gold_track_test` secara default, lihat `TestMain` di `test/e2e/main_test.go`)
  dibuat otomatis kalau belum ada dan di-migrate otomatis setiap run — tidak perlu setup manual
  selain `docker compose up -d postgres`. Tiap test top-level mulai dari `resetDB(t)`
  (`TRUNCATE ... RESTART IDENTITY CASCADE`) supaya independen dari urutan run.

  Ganti target DB test lewat env `DB_NAME` (default `gold_track_test`) kalau perlu isolasi dari
  DB development (`gold_track`).

### Skenario yang di-cover

**`health_test.go`**
- `GET /health` → 200 `success:true`

**`auth_test.go`** — `POST /api/auth/login`, `POST /api/auth/logout`
- Login sukses → token JWT non-kosong + data user benar
- Login password salah / email tidak ada / user nonaktif → 401 dengan pesan generik yang sama
  (tidak membocorkan status akun)
- Login field kosong → 400
- Login tidak butuh token (endpoint publik)
- Logout dengan token valid → 200
- Logout tanpa token → 401
- Logout dua kali pakai token yang sama → request kedua 401 (membuktikan blacklist `jti` jalan)

**`users_test.go`** — `/api/users` (CRUD, SUPER_ADMIN saja)
- Semua route `/api/users/*` tanpa token → 401
- Role selain SUPER_ADMIN (mis. KASIR) → 403
- Alur penuh create → list → get → update → delete:
  - create 201, response tidak pernah mengandung field `password`/`password_hash`
  - user baru muncul di list
  - get by `public_id` (UUID) mengembalikan data yang benar
  - update mengubah field (nama, role, dll)
  - delete = soft delete (`is_active=false`); user yang dinonaktifkan gagal login (401)
- Create dengan email duplikat → 409
- Get dengan id bukan UUID (mis. `/api/users/1`) → 400, dicegah sebelum sempat query DB
- Get id UUID valid tapi tidak ada → 404
- SUPER_ADMIN tidak bisa menonaktifkan akun sendiri lewat `DELETE` → 400 (guard anti-lockout)

**`categories_test.go`** / **`brands_test.go`** — `/api/categories`, `/api/brands` (CRUD, ADMIN & SUPER_ADMIN)
- Semua route tanpa token → 401
- Role selain ADMIN/SUPER_ADMIN (mis. KASIR) → 403
- Alur penuh create → list → get → update → delete: create 201 (`is_active=true` default),
  muncul di list, get by `public_id` benar, update mengubah `name`, delete = soft delete
  (get setelahnya tetap 200 tapi `is_active=false`)
- Create dengan nama duplikat (termasuk beda case, mis. `"Koin"` vs `"koin"`) → 409
  (unique index case-insensitive di `lower(name)`)
- Get dengan id bukan UUID → 400; UUID valid tapi tidak ada → 404

**`products_test.go`** — `POST /api/products` (create only, ADMIN & SUPER_ADMIN, BE-201)
- Tanpa token → 401; role KASIR → 403
- Create dengan kategori "Batangan" + brand "Antam" + berat 10 → `sku == "BAT-ANT-10-001"`,
  `is_active == true`, `category_id`/`brand_id` di response sama dengan yang dikirim
- Dua create dengan kombinasi kategori+brand+berat identik → SKU kedua `...-002` (urut naik)
- Field wajib kosong (`name`/`category_id`/`brand_id`) → 400; `weight_gram <= 0` → 400
- `category_id`/`brand_id` UUID valid tapi tidak ada → 404
- `category_id` merujuk kategori yang sudah `is_active=false` → 400

Konvensi menambah endpoint baru: tambah skenario di file `test/e2e/{resource}_test.go` baru
(ikuti pola `health_test.go` / `auth_test.go` / `users_test.go`) supaya suite ini tetap jadi peta
lengkap API surface, bukan cuma cover apa yang ada waktu ditulis (lihat komentar package di
`test/e2e/main_test.go`).

## Konfigurasi (env)

Lihat `.env.example` untuk daftar lengkap. Semua diambil dari environment
variable; file `.env` otomatis dibaca saat development (lihat `internal/config`).

## Menambah fitur baru

1. `internal/repository/xxx_repository.go` — interface + implementasi query.
2. `internal/service/xxx_service.go` — logic, panggil repository.
3. `internal/handler/xxx_handler.go` — terima request, panggil service, balas via `pkg/response`.
4. Daftarkan route di `internal/handler/router.go`.
