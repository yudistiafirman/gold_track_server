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
`000020` meng-ALTER `customers` nambah kolom `is_active` (tabel aslinya di `000001`…`000016`
belum punya, beda dari resource lain yang dari awal sudah punya). `000021` meng-ALTER
`stock_items` nambah `UNIQUE` constraint di `serial_number` (BE-801) — sebelumnya cuma
`barcode`/`public_id` yang unik di tabel itu. `000022` meng-ALTER `suppliers`/`customers`
supaya kolom `phone` jadi `NOT NULL` (sebelumnya nullable/opsional). `000023` meng-ALTER
CHECK constraint `payment_method` di `transactions` supaya terima metode baru (lihat
`### /api/transactions` di bawah), bukan cuma `CASH`/`TRANSFER`/`QRIS`. `000024` meng-ALTER
`gold_prices` supaya `created_by` jadi nullable (BE-404, lihat `### /api/gold-prices/active` di
bawah) — baris hasil sync otomatis tidak punya actor manusia. `000025` meng-ALTER `stock_items`
nambah kolom `production_year` (nullable) — atribut opsional per unit fisik, lihat
`### /api/products/{productId}/stock-items & /api/stock-items` di bawah. `000026` nambah partial
unique index di `stock_opnames` supaya cuma boleh ada 1 sesi `status='IN_PROGRESS'` dalam satu
waktu, lihat `### /api/stock-opnames` di bawah. `000027` nambah sequence `stock_items_barcode_seq`
— barcode `stock_items` pindah dari format `{SKU}-{urut}` (kepanjangan buat label fisik) ke angka
global 8 digit, lihat `### /api/products/{productId}/stock-items & /api/stock-items` di atas.

Menambah migrasi baru: buat pasangan file
`migrations/{next_number}_{deskripsi}.up.sql` dan `.down.sql`.

## Seeder data awal

```bash
go run ./cmd/seed
```

Membuat (kalau belum ada — aman dijalankan berkali-kali, tidak duplikat):
- 1 user `SUPER_ADMIN` (email/password dari `SEED_ADMIN_*`, default `admin@goldtrack.local` / `ChangeMe123!` — **ganti setelah login pertama**), password disimpan sebagai bcrypt hash.
- 7 `expense_categories` default: Listrik, Wifi/Internet, ATK, Gaji Karyawan, Sewa Tempat, Transportasi, Lain-lain.
- 4 `categories` default: Batangan, Koin, Perhiasan, Galeri.
- 5 `brands` default: Antam, UBS, Galeri24, Lotus Archi, King Halim.
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

// 400 — password kurang dari 8 karakter
{"success":false,"error":{"code":"BAD_REQUEST","message":"password minimal 8 karakter"}}

// 400 — password tidak punya kombinasi huruf besar/kecil/angka/simbol lengkap
{"success":false,"error":{"code":"BAD_REQUEST","message":"password harus mengandung kombinasi huruf besar, huruf kecil, angka, dan simbol"}}

// 400 — password ada di denylist password umum/lemah
{"success":false,"error":{"code":"BAD_REQUEST","message":"password terlalu umum/mudah ditebak, gunakan kombinasi lain"}}
```

**Aturan password** (`validatePassword`, `internal/service/user_service.go`), berlaku di `POST`
(wajib) maupun `PUT` (kalau field `password` diisi):
- Minimal 8 karakter.
- Wajib kombinasi lengkap: huruf besar, huruf kecil, angka, **dan** simbol (karakter non-huruf/angka)
  — kalau ada salah satu kategori yang hilang, ditolak 400.
- Ditolak juga (400) kalau persis cocok (case-insensitive) dengan salah satu password umum/lemah di
  denylist bawaan (`commonWeakPasswords`) — mis. `password123`, `qwerty123!`, `p@ssw0rd!`,
  `12345678`, dst. Ini denylist statis, **bukan** pengecekan ke database breach eksternal (supaya
  password user tidak pernah keluar/dikirim ke pihak ketiga).
- Password admin dari seeder (`cmd/seed`, env `SEED_ADMIN_PASSWORD`) **tidak** lewat validasi ini
  — seeder insert langsung ke DB, bukan lewat `userService.Create` — jadi kalau nanti diganti nilai
  default/env-nya ke sesuatu yang lebih lemah, seeder tetap jalan (baru kena aturan ini pas
  direset lewat `PUT /api/users/{id}` dari aplikasi).

Catatan:
- `password` di response **tidak pernah** dikembalikan.
- `email` unik — konflik → 409.
- `password` di `PUT` opsional; kosongkan untuk mempertahankan password lama (aturan di atas cuma
  dicek kalau field-nya diisi non-kosong).
- `DELETE` adalah **soft delete** (`is_active=false`), bukan hapus baris — semua tabel lain
  referensi `users.id` lewat FK tanpa `ON DELETE CASCADE`, jadi hard delete akan gagal begitu
  user itu pernah membuat data apa pun. Reaktivasi lewat `PUT` dengan `is_active: true`.
- SUPER_ADMIN tidak bisa menonaktifkan akun sendiri lewat `DELETE` (mencegah lockout).
- `GET /api/users` (list, tanpa pagination) urut **terbaru duluan** (`ORDER BY id DESC`).

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
- `GET /api/categories`/`GET /api/brands` (list, tanpa pagination) urut **terbaru duluan**
  (`ORDER BY id DESC`).

### /api/products — katalog produk

```bash
POST   /api/products        # { name, category_id, brand_id, weight_gram, description? } -> 201 (ADMIN & SUPER_ADMIN)
GET    /api/products        # ?search=&category_id=&brand_id=&page=&limit=          -> 200 (semua role, token valid)
GET    /api/products/{id}   # detail lengkap, termasuk produk yang sudah diarsipkan -> 200 / 404 (semua role, token valid)
PUT    /api/products/{id}   # { name, category_id, brand_id, weight_gram, description?, is_active } -> 200 / 404 (ADMIN & SUPER_ADMIN)
DELETE /api/products/{id}   # soft delete (is_active=false), ditolak kalau masih ada stok AVAILABLE -> 200 / 404 / 409 (ADMIN & SUPER_ADMIN)
```

`POST`/`PUT`/`DELETE` dibatasi `ADMIN`/`SUPER_ADMIN` (role lain → 403); `GET` (list & detail) bisa
diakses semua role selama tokennya valid — tidak ada `RequireRole` tambahan, cuma perlu login
(lihat `internal/handler/router.go`: kedua route `GET` ini sengaja diletakkan di luar grup
`RequireRole`).

#### POST /api/products — create dengan SKU auto-generate

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

Contoh response `POST /api/products` (201) — `category`/`brand` nested `{id, name}`, bukan UUID
polos, supaya klien nggak perlu lookup terpisah buat nampilin namanya:
```json
{
  "success": true,
  "data": {
    "id": "6d9f6a2a-2b0c-4e2b-8f1a-1a2b3c4d5e6f",
    "name": "Emas Batangan 10gr",
    "sku": "BAT-ANT-10-001",
    "category": { "id": "3fa85f64-5717-4562-b3fc-2c963f66afa6", "name": "Batangan" },
    "brand": { "id": "5c9e1a3b-8f2d-4a6e-9b1c-7d8e9f0a1b2c", "name": "Antam" },
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

Catatan:
- `created_by` diambil dari `claims.UserID` (JWT), bukan dari body — tidak bisa dipalsukan klien.
- `is_active` selalu `true` saat create, tidak bisa di-set lewat body.
- Arsipkan produk lewat `DELETE` (lihat di bawah, ada guard stok `AVAILABLE`); reaktivasi lewat
  `PUT` dengan `is_active: true`.

#### PUT /api/products/{id} — edit produk, SKU tidak berubah

Body-nya **full replace**, sama seperti `PUT` di `/api/users`/`/api/categories`/`/api/brands` —
klien wajib kirim semua field yang bisa diedit setiap kali (`name`, `category_id`, `brand_id`,
`weight_gram`, `description`, `is_active`), bukan partial patch. Validasi & aturan
kategori/brand-nya sama persis dengan `POST` (wajib ada, harus `is_active=true`).

`sku` **tidak pernah** ikut berubah lewat endpoint ini — kolom `sku` sengaja tidak ada di
query `UPDATE` (`ProductRepository.Update`), jadi walau `name`/`category_id`/`brand_id`/`weight_gram`
diubah, SKU yang sudah ter-generate saat create tetap sama. `{id}` yang tidak ditemukan → 404.
Karena body mencakup `is_active`, endpoint ini juga jadi cara mereaktivasi produk yang sudah
diarsipkan lewat `DELETE` (`is_active: true` → muncul lagi di `GET /api/products`) — tidak kena
guard stok `AVAILABLE`, karena guard itu cuma berlaku untuk `DELETE`.

Contoh response `PUT /api/products/{id}` (200) — misal produk yang tadinya `BAT-ANT-10-001`
diubah nama/kategori/brand/beratnya:
```json
{
  "success": true,
  "data": {
    "id": "6d9f6a2a-2b0c-4e2b-8f1a-1a2b3c4d5e6f",
    "name": "Koin Emas UBS 20gr",
    "sku": "BAT-ANT-10-001",
    "category": { "id": "...", "name": "Koin" },
    "brand": { "id": "...", "name": "UBS" },
    "weight_gram": 20,
    "description": "Diubah jadi koin",
    "is_active": true,
    "created_at": "2026-07-31T09:00:00Z",
    "updated_at": "2026-07-31T10:15:00Z"
  }
}
```
`sku` tetap `BAT-ANT-10-001` meski kategori/brand/berat berubah total — bukti SKU immutable.

#### DELETE /api/products/{id} — arsipkan produk (dijaga stok AVAILABLE)

Soft delete (`is_active=false`) — baris `products` tidak pernah dihapus, jadi data historis
(transaksi lama, `stock_items` yang sudah `SOLD`) tetap utuh dan tetap bisa di-join/dilaporkan.

**Guard**: ditolak (`409`) kalau produk masih punya baris `stock_items` dengan `status='AVAILABLE'`
— mengarsipkan produk yang stoknya masih bisa dijual akan bikin stok itu "yatim" (tidak muncul di
katalog tapi masih ada fisiknya). Produk yang stoknya semua sudah `SOLD` (atau belum punya stok
sama sekali) boleh diarsipkan.

```bash
DELETE /api/products/{id}
```

Contoh response sukses (200): `{"success":true,"data":{"message":"produk diarsipkan"}}`

Contoh response gagal (409) — masih ada stok `AVAILABLE`:
```json
{"success":false,"error":{"code":"CONFLICT","message":"produk masih memiliki stok tersedia (AVAILABLE), tidak bisa diarsipkan"}}
```

`{id}` tidak ditemukan → 404 (`"produk tidak ditemukan"`). Reaktivasi lewat `PUT` dengan
`is_active: true` (lihat di atas) — `PUT` tidak kena guard stok ini.

#### GET /api/products — list terpaginasi & terfilter

Query params, semua opsional:
- `search` — cocok ke `name` (`ILIKE`, case-insensitive substring).
- `category_id` / `brand_id` — `public_id` (UUID). Kalau nilainya tidak match kategori/brand
  manapun, hasilnya list kosong (`items: []`, `total: 0`), **bukan** 404 — ini filter, bukan lookup
  resource wajib.
- `page` (default `1`), `limit` (default `20`, di-cap maksimum `100`).

List **selalu** `WHERE is_active = true` — produk yang diarsipkan tidak pernah muncul di sini,
walau requester-nya SUPER_ADMIN. Urut **terbaru duluan** (`ORDER BY id DESC`) — produk yang baru
saja dibuat langsung muncul di halaman pertama, bukan kekubur di halaman terakhir.

Contoh response `GET /api/products?search=emas&page=1&limit=20` (200):
```json
{
  "success": true,
  "data": {
    "items": [
      {
        "id": "6d9f6a2a-2b0c-4e2b-8f1a-1a2b3c4d5e6f",
        "name": "Emas Batangan 10gr",
        "sku": "BAT-ANT-10-001",
        "category": { "id": "3fa85f64-5717-4562-b3fc-2c963f66afa6", "name": "Batangan" },
        "brand": { "id": "5c9e1a3b-8f2d-4a6e-9b1c-7d8e9f0a1b2c", "name": "Antam" },
        "weight_gram": 10,
        "description": "Emas batangan Antam 10 gram",
        "is_active": true,
        "created_at": "2026-07-31T09:00:00Z",
        "updated_at": "2026-07-31T09:00:00Z"
      }
    ],
    "pagination": { "page": 1, "limit": 20, "total": 1, "total_pages": 1 }
  }
}
```

#### GET /api/products/{id} — detail lengkap

`{id}` adalah `public_id` (UUID). Tidak difilter `is_active` — produk yang sudah diarsipkan tetap
bisa diambil detailnya (cuma disembunyikan dari list, bukan dihapus/disamarkan datanya). Response-nya
objek produk yang sama seperti item di list / response `POST`.

Contoh response error (berlaku utk `POST`, `PUT`, `GET`, dan `GET /{id}` — `PUT` pakai pesan yang
sama persis dengan `POST` untuk validasi field/kategori/brand):
```json
// 403 — POST/PUT oleh role selain ADMIN/SUPER_ADMIN
{"success":false,"error":{"code":"FORBIDDEN","message":"Anda tidak memiliki akses untuk aksi ini"}}

// 400 — POST/PUT, field wajib kosong
{"success":false,"error":{"code":"BAD_REQUEST","message":"name, category_id, dan brand_id wajib diisi"}}

// 400 — POST/PUT, weight_gram <= 0
{"success":false,"error":{"code":"BAD_REQUEST","message":"weight_gram harus lebih besar dari 0"}}

// 404 — PUT, {id} tidak ditemukan
{"success":false,"error":{"code":"NOT_FOUND","message":"produk tidak ditemukan"}}

// 400 — POST, category_id/brand_id merujuk ke resource yang is_active=false
{"success":false,"error":{"code":"BAD_REQUEST","message":"kategori tidak aktif"}}

// 404 — POST: category_id/brand_id valid UUID tapi tidak ada; atau GET /{id} tidak ditemukan
{"success":false,"error":{"code":"NOT_FOUND","message":"kategori tidak ditemukan"}}
// (GET /{id} not found: "produk tidak ditemukan")

// 400 — GET /{id} dengan id bukan format UUID
{"success":false,"error":{"code":"BAD_REQUEST","message":"id tidak valid"}}
```

### /api/suppliers — CRUD supplier (ADMIN & SUPER_ADMIN)

```bash
GET    /api/suppliers                  # ?search=&page=&limit=                        -> 200
POST   /api/suppliers                  # { name, phone, address?, notes? }             -> 201
GET    /api/suppliers/{id}             # detail supplier                                -> 200 / 404
PUT    /api/suppliers/{id}             # { name, phone, address?, notes?, is_active }   -> 200 / 404
DELETE /api/suppliers/{id}             # soft delete (is_active=false)                  -> 200 / 404
GET    /api/suppliers/{id}/transactions # ?page=&limit= — riwayat gabungan (lihat bawah) -> 200 / 404
```

`{id}` adalah `public_id` (UUID). `name` dan `phone` wajib (migration `000022` menambahkan
`NOT NULL` di kolom `phone`, ditambah validasi 400 di service kalau kosong) — `address`/`notes`
opsional, dikosongkan (`""`) di request berarti `NULL` di DB. `PUT` full-replace seperti resource
lain (kirim semua field yang bisa diedit, bukan partial patch).

Beda dari `/api/products` (BE-202): `GET /api/suppliers` **tidak** memfilter `is_active` — supplier
yang sudah diarsipkan tetap muncul di list (field `is_active` di response tinggal dicek klien),
karena admin mungkin masih perlu cari supplier lama buat referensi PO/transaksi historis. `?search`
cocok ke `name` (`ILIKE`, case-insensitive substring); `page`/`limit` sama seperti `/api/products`
(default `1`/`20`, `limit` di-cap `100`). Urut **terbaru duluan** (`ORDER BY id DESC`), sama seperti
`/api/products`.

Contoh response `POST /api/suppliers` (201):
```json
{
  "success": true,
  "data": {
    "id": "8a1b2c3d-4e5f-6789-abcd-ef0123456789",
    "name": "Toko Emas Makmur",
    "phone": "081234567890",
    "address": "Jl. Emas No. 1",
    "notes": "Supplier utama",
    "is_active": true,
    "created_at": "2026-07-31T09:00:00Z",
    "updated_at": "2026-07-31T09:00:00Z"
  }
}
```

Contoh response `GET /api/suppliers?search=emas&page=1&limit=20` (200):
```json
{
  "success": true,
  "data": {
    "items": [ { "...": "objek supplier yang sama seperti di atas" } ],
    "pagination": { "page": 1, "limit": 20, "total": 1, "total_pages": 1 }
  }
}
```

#### GET /api/suppliers/{id}/transactions — riwayat gabungan supplier

Beda dengan `GET /api/customers/{id}/transactions` (BE-602) yang cuma baca satu tabel
(`transactions`), hubungan toko-supplier tersebar di **dua tabel yang tidak berelasi**: `
purchase_orders` (toko **beli** stok dari supplier — relasi utama, paling sering kejadian) dan
`transactions` bertipe `SELL_SUPPLIER` (toko **jual balik** unit ke supplier — retur/likuidasi,
lebih jarang). Endpoint ini menggabungkan keduanya jadi satu list terpaginasi, urut `created_at`
terbaru duluan, lewat satu query `UNION ALL` (jadi `ORDER BY`/`LIMIT`/`OFFSET`-nya satu
merge-sort di Postgres, bukan digabung manual di Go). Tiap baris punya field `source`
(`PURCHASE_ORDER` atau `SELL_SUPPLIER`) buat bedain asalnya; `code` adalah `po_code` atau
`transaction_code` tergantung sumbernya. `{id}` tidak ditemukan → 404; format bukan UUID → 400;
supplier tanpa riwayat sama sekali → `items: []`, bukan error.

**Detail per baris**: list ini header-only (sama pola dengan `transactionSummaryResponse` di
riwayat customer), sengaja tidak membawa `items[]`. Buat detail lengkap satu entri, pakai `id`
baris itu (bukan `id` supplier) ke endpoint detail yang sudah ada, dipilih berdasarkan `source`:
`source=PURCHASE_ORDER` → `GET /api/purchase-orders/{id}`, `source=SELL_SUPPLIER` → `GET
/api/transactions/{id}`. Tidak ada endpoint detail terpadu baru — kedua endpoint itu sudah
lengkap (items, status, dll), jadi tidak perlu duplikasi logic.

Contoh response (200):
```json
{
  "success": true,
  "data": {
    "items": [
      { "id": "...", "source": "SELL_SUPPLIER", "code": "TRX-20260731-0002", "status": "COMPLETED", "total_amount": 100000, "created_at": "2026-07-31T10:00:00Z" },
      { "id": "...", "source": "PURCHASE_ORDER", "code": "PO-20260731-0001", "status": "BELUM_DITERIMA", "total_amount": 1600000, "created_at": "2026-07-31T09:00:00Z" }
    ],
    "pagination": { "page": 1, "limit": 20, "total": 2, "total_pages": 1 }
  }
}
```

Contoh response error:
```json
// 403 — role selain ADMIN/SUPER_ADMIN
{"success":false,"error":{"code":"FORBIDDEN","message":"Anda tidak memiliki akses untuk aksi ini"}}

// 400 — name kosong
{"success":false,"error":{"code":"BAD_REQUEST","message":"name wajib diisi"}}

// 400 — phone kosong
{"success":false,"error":{"code":"BAD_REQUEST","message":"phone wajib diisi"}}

// 404 — public_id valid tapi tidak ada
{"success":false,"error":{"code":"NOT_FOUND","message":"supplier tidak ditemukan"}}

// 400 — {id} bukan format UUID
{"success":false,"error":{"code":"BAD_REQUEST","message":"id tidak valid"}}
```

### /api/customers — CRUD pelanggan (role split: create/read vs edit/delete)

```bash
GET    /api/customers                 # ?search=&page=&limit=                              -> 200 (ADMIN, KASIR, SUPER_ADMIN)
POST   /api/customers                 # { name, phone, email?, id_type?, id_number?, address?, notes? } -> 201 (ADMIN, KASIR, SUPER_ADMIN)
GET    /api/customers/{id}            # detail pelanggan                                     -> 200 / 404 (ADMIN, KASIR, SUPER_ADMIN)
GET    /api/customers/{id}/transactions  # riwayat transaksi SELL & BUY, ?page=&limit=        -> 200 / 404 (semua role, token valid)
PUT    /api/customers/{id}            # field yang sama + is_active                          -> 200 / 404 (ADMIN & SUPER_ADMIN)
DELETE /api/customers/{id}            # soft delete (is_active=false)                        -> 200 / 404 (ADMIN & SUPER_ADMIN)
```

**Satu-satunya resource di API ini dengan role berbeda per operasi**: `POST`/`GET` (create & read)
bisa diakses `KASIR` juga (bukan cuma `ADMIN`/`SUPER_ADMIN`) — kasir perlu bisa cepat daftarin
pelanggan baru & cari data pelanggan pas transaksi. `PUT`/`DELETE` tetap dikunci ke
`ADMIN`/`SUPER_ADMIN` (lihat `internal/handler/router.go`: `POST`/`GET /customers` ada di grup
`JWTAuth` tanpa `RequireRole` tambahan, sedangkan `PUT`/`DELETE /customers/{id}` ada di grup
`RequireRole("ADMIN", "SUPER_ADMIN")` yang sama dengan resource admin-only lainnya).

`name` dan `phone` wajib (migration `000022`, sama seperti `/api/suppliers`). `id_type`, kalau
diisi, harus `KTP`/`SIM`/`PASSPORT` (400 kalau bukan). `?search` cocok ke `name` **atau** `phone`
(satu query param, match salah satu). Sama seperti `/api/suppliers`: list **tidak** memfilter
`is_active` — pelanggan yang diarsipkan tetap muncul di list (berguna buat cari histori
transaksi pelanggan lama). Urut **terbaru duluan** (`ORDER BY id DESC`).

Contoh response `POST /api/customers` (201):
```json
{
  "success": true,
  "data": {
    "id": "9f8e7d6c-5b4a-3210-9876-543210fedcba",
    "name": "Budi Santoso",
    "phone": "081234567890",
    "email": "budi@example.com",
    "id_type": "KTP",
    "id_number": "3201010101010001",
    "address": "Jl. Melati No. 5",
    "notes": "",
    "is_active": true,
    "created_at": "2026-07-31T09:00:00Z",
    "updated_at": "2026-07-31T09:00:00Z"
  }
}
```

Contoh response error:
```json
// 403 — PUT/DELETE oleh role selain ADMIN/SUPER_ADMIN (KASIR boleh POST/GET, tapi bukan ini)
{"success":false,"error":{"code":"FORBIDDEN","message":"Anda tidak memiliki akses untuk aksi ini"}}

// 400 — name kosong
{"success":false,"error":{"code":"BAD_REQUEST","message":"name wajib diisi"}}

// 400 — phone kosong
{"success":false,"error":{"code":"BAD_REQUEST","message":"phone wajib diisi"}}

// 400 — id_type bukan KTP/SIM/PASSPORT
{"success":false,"error":{"code":"BAD_REQUEST","message":"id_type harus KTP, SIM, atau PASSPORT"}}

// 404 — public_id valid tapi tidak ada
{"success":false,"error":{"code":"NOT_FOUND","message":"pelanggan tidak ditemukan"}}
```

#### GET /api/customers/{id}/transactions — riwayat transaksi pelanggan (BE-602)

Terbuka buat semua role (token valid) — bukan cuma admin/kasir, sama seperti `GET /api/products`.
Menggabungkan transaksi `type=SELL` dan `type=BUY` milik pelanggan itu (`type=SELL_SUPPLIER` tidak
pernah punya `customer_id`, jadi otomatis tidak ikut, tapi filternya tetap eksplisit
`customer_id = ? AND type IN ('SELL','BUY')` di query). Urut **terbaru duluan**
(`ORDER BY created_at DESC`), paginated (`?page=`/`?limit=`, default & cap sama seperti list
lainnya). Baris di list ini **cuma header** (tanpa `items[]`) — buat detail lengkap tiap transaksi
(termasuk item-item buat struk), lihat `GET /api/transactions/{id}` di bawah, yang linknya
langsung dari `id` tiap baris di list ini.

Contoh response (200):
```json
{
  "success": true,
  "data": {
    "items": [
      {
        "id": "4d5e6f70-8192-0334-def0-123456789012",
        "transaction_code": "TRX-20260731-0002",
        "type": "BUY",
        "total_amount": 900000,
        "total_weight": 10,
        "payment_method": "CASH",
        "status": "COMPLETED",
        "created_at": "2026-07-31T09:05:00Z",
        "completed_at": "2026-07-31T09:05:00Z"
      },
      {
        "id": "2b3c4d5e-6f70-8901-bcde-f01234567890",
        "transaction_code": "TRX-20260731-0001",
        "type": "SELL",
        "total_amount": 1500000,
        "total_weight": 10,
        "payment_method": "CASH",
        "status": "COMPLETED",
        "created_at": "2026-07-31T09:00:00Z",
        "completed_at": "2026-07-31T09:00:00Z"
      }
    ],
    "pagination": { "page": 1, "limit": 20, "total": 2, "total_pages": 1 }
  }
}
```
`{id}` yang bukan pelanggan valid → 404 (`"pelanggan tidak ditemukan"`).

### /api/products/{productId}/stock-items & /api/stock-items — unit stok fisik

```bash
POST   /api/products/{productId}/stock-items   # tambah unit stok, barcode auto-generate      -> 201 (ADMIN & SUPER_ADMIN)
GET    /api/products/{productId}/stock-items   # ?status=&condition=&search=&page=&limit=     -> 200 (semua role, token valid)
GET    /api/stock-items/lookup                 # ?barcode=&type=  cari unit buat keranjang jual -> 200 / 404 / 409 (semua role, token valid)
GET    /api/stock-items/{id}                   # detail unit lengkap (termasuk barcode)         -> 200 / 404 (semua role, token valid)
GET    /api/stock-items/{id}/label             # data buat cetak label CODE128                  -> 200 / 404 (semua role, token valid)
PUT    /api/stock-items/{id}                   # edit unit, barcode & product_id terkunci        -> 200 / 404 / 409 (ADMIN & SUPER_ADMIN)
DELETE /api/stock-items/{id}                   # HARD delete, hanya unit AVAILABLE               -> 200 / 404 / 409 (ADMIN & SUPER_ADMIN)
```

`{productId}` maupun `{id}` adalah `public_id` (UUID). Beda dari resource lain di API ini:

- **`DELETE` di sini HARD delete**, satu-satunya di seluruh API — setiap resource lain
  (`users`/`categories`/`brands`/`products`/`suppliers`) soft-delete via `is_active`. Unit stok
  yang `AVAILABLE` (belum terjual) dihapus permanen dari DB; unit yang `SOLD` **tidak bisa**
  dihapus (409) — data transaksi historis harus tetap utuh. Guard-nya di level SQL
  (`DELETE ... WHERE public_id = $1 AND status = 'AVAILABLE'`), bukan cuma cek-lalu-hapus di
  level aplikasi — kalau baris tidak ke-delete, baru dicek ulang buat bedain "tidak ada" (404)
  vs "ada tapi SOLD" (409).
- **Ditolak (409) juga kalau unit masih direferensikan tabel lain**, walau statusnya
  `AVAILABLE` — dua kasus nyata: unit hasil `BUY` (buyback) yang belum terjual lagi (masih ada
  baris `transaction_items` yang nunjuk ke situ), atau unit yang pernah discan di sesi stock
  opname (baris `stock_opname_items`). Kedua tabel itu FK ke `stock_items.id` tanpa
  `ON DELETE CASCADE`, jadi hard-delete-nya bakal ditolak Postgres di level FK — repository
  nangkep error itu (`isForeignKeyViolation`, pola yang sama dipakai `expense_categories`) dan
  balikin 409 yang rapi, bukan 500 mentah.
- **Validasi create pakai status `422`** (`Unprocessable Entity`), bukan `400` seperti endpoint
  lain — `serial_number`, `condition` (harus `GOOD`/`BAD`), `purchase_price` (harus `> 0`), dan
  `purchase_date` (format `YYYY-MM-DD`) semua wajib diisi & valid, kalau tidak → 422.
- **Barcode auto-generate, 8 digit angka global** (`00000001`, `00000002`, dst., dari sequence DB
  `stock_items_barcode_seq`, migrasi `000027`) — **bukan** lagi `{SKU}-{urut 4 digit}` seperti
  sebelumnya. Diganti karena barcode lama (mis. `BAT-UBS-0.5-001-0004`, 21 karakter) kepanjangan
  buat dicetak & dibaca scanner di label fisik 50×25mm. Angka murni (bukan campur huruf) juga
  dipilih sengaja: subset-C CODE128 ngepak 2 digit per simbol, jadi barcode-nya makin ringkas
  secara render dibanding kalau tetap ada prefix huruf. Sequence-nya **global** (lintas produk,
  bukan per-SKU) dan dijamin unik oleh Postgres sendiri (`nextval`) — tidak perlu retry-on-conflict
  kayak skema lama, dan barcode sekarang sama sekali tidak mengandung info produk (identitas
  produk tetap ada lewat field `product` nested di response, bukan di-encode ke barcode-nya).
  `serial_number` tetap terpisah dan independen (bebas format, cuma wajib unik global lewat
  `uq_stock_items_serial_number`) — barcode dan serial number dua konsep beda, tidak saling
  menurunkan nilai satu sama lain.
- **Create ditolak (400) kalau produknya sudah diarsipkan** (`is_active=false`) — tidak bisa
  nambah stok ke produk yang sudah tidak dijual.
- **`GET .../stock-items` (list per produk) urut terbaru duluan** (`ORDER BY si.id DESC`) —
  unit yang baru ditambah langsung muncul di halaman pertama.
- **`PUT` mengunci `barcode` dan `product_id`** — kolom itu sengaja tidak ada di query `UPDATE`,
  persis pola SKU produk yang immutable (BE-203). Field yang bisa diedit: `serial_number`,
  `condition`, `purchase_price`, `purchase_date`, `production_year`, `notes`. Unit yang `SOLD`
  ditolak (409) — guard-nya fetch-lalu-cek di service (bukan level SQL seperti `DELETE`, karena
  edit bukan operasi destruktif tanpa-undo).
- **`production_year`** (migrasi `000025`) — tahun produksi/cetak unit fisik, **opsional**
  (`null` kalau tidak diisi, bukan wajib seperti `serial_number`/`condition`). Kalau diisi, harus
  angka masuk akal (2000 s.d. tahun berjalan + 1), kalau tidak → 422, sama tier validasinya
  dengan field per-unit lain. Sengaja ditaruh di `stock_items` bukan `products` — dua unit dari
  produk/SKU yang sama bisa punya tahun produksi berbeda kalau masuk stok di waktu yang berbeda.
  Field ini juga diterima (opsional, per unit) lewat item `BUY` di `POST /api/transactions` dan
  per serial di `POST /api/purchase-orders/{id}/receive` — lihat section masing-masing.
- **`GET /api/stock-items/{id}/label`** berlaku untuk unit `AVAILABLE` **maupun** `SOLD` — cetak
  ulang label diperbolehkan, tidak difilter status.
- `supplier_id`/`po_id` di luar scope saat ini — selalu `NULL` (belum ada resource purchase
  order, dan `supplier_id` belum diterima lewat body ini).
- **`product.weight_gram` ikut di setiap response** (create, get, list, lookup) — nested di
  dalam objek `product` (bukan top-level), karena berat itu atribut produk (semua unit dari
  produk yang sama beratnya pasti sama), pakai DTO terpisah dari `productRefResponse` yang
  dipakai category/brand/supplier di tempat lain (yang tidak punya berat).

#### GET /api/stock-items/lookup — cari unit via barcode (BE-701/BE-703)

Dipakai kasir buat scan barcode fisik pas nambahin item ke keranjang jual. `?barcode=` wajib.
`?type=` opsional (`SELL` atau `SELL_SUPPLIER`) — kalau `type=SELL` dan unit `condition=BAD`,
response punya `"requires_confirmation": true` supaya klien nampilin konfirmasi **sebelum** unit
itu masuk keranjang. `type=SELL_SUPPLIER` atau `?type` dikosongkan → `requires_confirmation`
selalu `false` (jual ke supplier tidak butuh konfirmasi kondisi). Barcode tidak ditemukan → 404;
unit yang sudah `SOLD` → 409. Field `condition` selalu ada di response biar klien juga bisa decide
sendiri kalau perlu.

```bash
GET /api/stock-items/lookup?barcode=00000001&type=SELL
```

Contoh response (200):
```json
{
  "success": true,
  "data": {
    "id": "1a2b3c4d-5e6f-7890-abcd-ef1234567890",
    "product": { "id": "6d9f6a2a-2b0c-4e2b-8f1a-1a2b3c4d5e6f", "name": "Emas Batangan 10gr", "weight_gram": 10 },
    "barcode": "00000001",
    "serial_number": "SN-0001",
    "condition": "BAD",
    "purchase_price": 1000000,
    "purchase_date": "2026-07-01",
    "production_year": null,
    "status": "AVAILABLE",
    "sold_at": null,
    "notes": "",
    "created_at": "2026-07-31T09:00:00Z",
    "updated_at": "2026-07-31T09:00:00Z",
    "requires_confirmation": true
  }
}
```

Contoh response `POST /api/products/{productId}/stock-items` (201):
```json
{
  "success": true,
  "data": {
    "id": "1a2b3c4d-5e6f-7890-abcd-ef1234567890",
    "product": { "id": "6d9f6a2a-2b0c-4e2b-8f1a-1a2b3c4d5e6f", "name": "Emas Batangan 10gr", "weight_gram": 10 },
    "barcode": "00000001",
    "serial_number": "SN-0001",
    "condition": "GOOD",
    "purchase_price": 1000000,
    "purchase_date": "2026-07-01",
    "production_year": 2024,
    "status": "AVAILABLE",
    "sold_at": null,
    "notes": "",
    "created_at": "2026-07-31T09:00:00Z",
    "updated_at": "2026-07-31T09:00:00Z"
  }
}
```

Contoh response `GET /api/stock-items/{id}/label` (200):
```json
{
  "success": true,
  "data": {
    "barcode": "00000001",
    "product_name": "Emas Batangan 10gr",
    "weight_gram": 10,
    "serial_number": "SN-0001"
  }
}
```

Contoh response error:
```json
// 403 — role selain ADMIN/SUPER_ADMIN (POST/PUT/DELETE)
{"success":false,"error":{"code":"FORBIDDEN","message":"Anda tidak memiliki akses untuk aksi ini"}}

// 422 — field wajib kosong/invalid saat create atau update
{"success":false,"error":{"code":"UNPROCESSABLE_ENTITY","message":"serial_number wajib diisi"}}
{"success":false,"error":{"code":"UNPROCESSABLE_ENTITY","message":"condition wajib diisi dan harus GOOD atau BAD"}}
{"success":false,"error":{"code":"UNPROCESSABLE_ENTITY","message":"purchase_price wajib diisi lebih besar dari 0"}}
{"success":false,"error":{"code":"UNPROCESSABLE_ENTITY","message":"purchase_date wajib diisi dengan format YYYY-MM-DD"}}
{"success":false,"error":{"code":"UNPROCESSABLE_ENTITY","message":"production_year tidak valid"}}

// 400 — create ke produk yang sudah diarsipkan
{"success":false,"error":{"code":"BAD_REQUEST","message":"produk sudah diarsipkan, tidak bisa menambah stok"}}

// 409 — update/delete unit yang sudah SOLD
{"success":false,"error":{"code":"CONFLICT","message":"unit sudah terjual (SOLD), tidak bisa diedit"}}
{"success":false,"error":{"code":"CONFLICT","message":"unit sudah terjual (SOLD), tidak bisa dihapus"}}
{"success":false,"error":{"code":"CONFLICT","message":"unit stok sudah tercatat di transaksi atau stock opname, tidak bisa dihapus"}}

// 404 — {productId} atau {id} tidak ditemukan
{"success":false,"error":{"code":"NOT_FOUND","message":"produk tidak ditemukan"}}
{"success":false,"error":{"code":"NOT_FOUND","message":"unit stok tidak ditemukan"}}
```

### /api/transactions — checkout penjualan & buyback (BE-602/BE-702/BE-703/BE-801)

```bash
POST /api/transactions        # semua role, token valid                                -> 201
GET  /api/transactions/{id}   # detail lengkap (dengan items[]) — struk, SELL maupun BUY -> 200 / 404 (semua role, token valid)
```

Tiga `type` didukung: `SELL` (jual ke pelanggan), `SELL_SUPPLIER` (jual ke supplier), `BUY`
(beli emas dari pelanggan — buyback). Bentuk `items[]` beda tergantung `type`:

```json
// SELL / SELL_SUPPLIER — menjual unit yang SUDAH ADA
{
  "type": "SELL",
  "customer_id": "<public_id customer, wajib kalau type=SELL>",
  "supplier_id": "<public_id supplier, wajib kalau type=SELL_SUPPLIER>",
  "payment_method": "CASH",
  "payment_ref": "",
  "notes": "",
  "items": [
    { "stock_item_id": "<public_id unit>", "price_total": 1500000, "confirmed": false }
  ]
}
```
```json
// BUY — membeli emas dari pelanggan, tiap item BIKIN unit stok baru
{
  "type": "BUY",
  "customer_id": "<public_id customer, wajib>",
  "payment_method": "CASH",
  "items": [
    { "product_id": "<public_id produk>", "serial_number": "SN-BUYBACK-01", "condition": "GOOD", "price_total": 900000, "production_year": 2024 }
  ]
}
```

Satu transaksi bisa berisi banyak `items`, tiap item **harga negonya sendiri-sendiri**
(`price_total`) — bukan dihitung dari harga per gram. `price_per_gram` di response adalah
turunan (`price_total ÷ weight_gram`), dihitung server buat kebutuhan laporan, bukan input.

**Atomik & aman dari race condition**: seluruh proses jalan dalam **satu transaksi DB** —
untuk `SELL`/`SELL_SUPPLIER`, tiap `stock_items` yang direferensikan di-lock
(`SELECT ... FOR UPDATE`) dan dicek ulang statusnya di dalam lock itu, jadi kalau dua request
nyoba jual unit yang sama nyaris bersamaan, yang kedua pasti dapat 409, bukan ikut lolos gara-gara
keduanya sempat lihat status `AVAILABLE` sebelum salah satu commit (diverifikasi test
`TestTransactions_CreateConcurrentDoubleSellOnlyOneWins`, dua goroutine nembak unit yang sama
bersamaan → persis satu 201 dan satu 409, berulang kali). Untuk `BUY`, atomiknya soal
"transaksi + semua unit baru + semua transaction_items, semua-atau-tidak-sama-sekali" — kalau
satu item gagal (misal `serial_number` bentrok), **seluruh** transaksi dibatalkan, tidak ada unit
yang setengah-tersimpan.

**Aturan per `type`**:
- `SELL` → `customer_id` wajib, `supplier_id` harus kosong. Item pakai `stock_item_id` (unit yang
  sudah ada) + `price_total` (+ `confirmed` kalau unitnya `BAD`, lihat di bawah).
- `SELL_SUPPLIER` → `supplier_id` wajib, `customer_id` harus kosong. Sama seperti `SELL` soal
  bentuk item, tapi tidak pernah butuh `confirmed`.
- `BUY` → `customer_id` wajib (tidak ada `supplier_id` buat `BUY`). Item pakai `product_id`
  (produk yang mau ditambah stoknya) + `serial_number` + `condition` + `price_total` — **bukan**
  `stock_item_id`, karena unitnya belum ada, baru dibuat oleh request ini.

**Konfirmasi kondisi BAD (BE-703, cuma berlaku utk `SELL`)**: kalau `type=SELL` dan unit yang
direferensikan `condition=BAD`, item itu wajib `"confirmed": true` — kalau tidak, seluruh
transaksi ditolak (409), bukan cuma item itu. `SELL_SUPPLIER` tidak pernah butuh `confirmed`,
walau unitnya `BAD`. Cek dulu lewat `GET /api/stock-items/lookup?barcode=...&type=SELL` buat tau
butuh konfirmasi atau tidak sebelum checkout — tapi validasi ini tetap dipaksakan lagi di server
saat `POST`, klien tidak pernah dipercaya begitu saja buat hal finansial begini.

**Khusus `BUY`** (BE-801):
- Tiap item bikin baris `stock_items` baru: `status=AVAILABLE`, `barcode` auto-generate (8 digit
  angka dari `stock_items_barcode_seq`, sama pola global dengan BE-501 — lihat
  `### /api/products/{productId}/stock-items & /api/stock-items` di atas — bukan lagi per-SKU),
  `purchase_price` = `price_total` yang diinput (jadi modal unit itu buat laporan margin nanti),
  `purchase_date` = hari ini (server yang isi, bukan field request).
- `serial_number` **wajib** dan **unik** — baik terhadap unit yang sudah ada di DB
  (`UNIQUE` constraint `uq_stock_items_serial_number`, migration `000021`) maupun antar item
  dalam satu batch request yang sama (dicek duluan di service sebelum ke DB). Ini juga berarti
  endpoint create-stock-item biasa (`POST /api/products/{productId}/stock-items`, BE-501) sekarang
  ikut menolak (409) `serial_number` duplikat, bukan cuma error 500 seperti sebelum constraint
  ini ada.
- `condition` (`GOOD`/`BAD`) wajib per item, ditentukan manual per unit fisik — tidak ada
  guard konfirmasi BAD di sisi `BUY` (itu cuma buat `SELL`).
- `production_year` **opsional** per item (sama aturan validasi dengan BE-501: kalau diisi harus
  2000 s.d. tahun berjalan+1, kalau tidak → 422). Tersimpan di unit `stock_items` baru tapi
  **tidak ikut ditampilkan** di response item transaksi (`transactionItemResponse`) — sama seperti
  `condition`/`purchase_price` yang juga tidak diekspos di situ; cek langsung ke
  `GET /api/stock-items/{id}` (pakai `stock_item_id` dari response) kalau perlu lihat nilainya.
- `cogs` transaction_item-nya `NULL` — belum diketahui sampai unit ini nanti terjual lagi.
- Produk harus ada (404) dan aktif (`is_active=true`, sama seperti guard BE-501 — 400 kalau
  produknya sudah diarsipkan).

`transaction_code` format `TRX-YYYYMMDD-XXXX` (urut 4 digit per hari, mis. `TRX-20260731-0001`,
`...-0002`), dihitung + insert dalam transaksi yang sama dengan retry (maks 5x) kalau kena race
di constraint unique-nya — pola yang sama dengan `sku` produk dan `barcode` unit stok.

Setiap unit yang berhasil terjual (`SELL`/`SELL_SUPPLIER`) otomatis jadi `status=SOLD` +
`sold_at` terisi (tidak bisa di-`PUT`/`DELETE` lagi setelahnya — lihat guard di section
stock-items di atas).

Response tiap item menyertakan `stock_item_id` (public_id), `barcode`, dan `serial_number` unit
yang bersangkutan — buat `SELL`/`SELL_SUPPLIER` itu unit yang sudah ada, buat `BUY` itu unit yang
baru dibuat (langsung kepakai buat cetak label via `GET /api/stock-items/{id}/label`). Berlaku
juga di `GET /api/transactions/{id}` dan `GET /api/transactions/{id}/receipt` (BE-1001) — bukan
cuma di response create. Response **tidak pernah menyertakan `cogs`** (harga pokok/beli unit) —
itu data margin toko, sengaja tidak diekspos ke response checkout yang kemungkinan dilihat kasir.

`payment_method` harus salah satu dari `CASH`, `TRANSFER`, `QRIS`, `DEBIT`, `KREDIT`, `GOPAY`,
`OVO`, `DANA`, `SHOPEEPAY` (guard di `allowedPaymentMethods`, `internal/service/transaction_service.go`,
plus CHECK constraint yang sama di DB, migration `000023`) — nilai lain → 400.

`payment_ref` adalah identifier bebas buat pembayaran non-tunai (nomor referensi transfer, nama
e-wallet, dll — bukan enum tetap, `payment_method` sendiri yang sudah membedakan metodenya).
Selalu ikut kebalikin di response — di sini, di `GET /api/transactions/{id}`, maupun di
`GET /api/customers/{id}/transactions` (riwayat) — kosong (`""`) kalau tidak diisi (lazimnya
buat `CASH`).

Contoh response `SELL` (201):
```json
{
  "success": true,
  "data": {
    "id": "2b3c4d5e-6f70-8901-bcde-f01234567890",
    "transaction_code": "TRX-20260731-0001",
    "type": "SELL",
    "total_amount": 1500000,
    "total_weight": 10,
    "payment_method": "TRANSFER",
    "payment_ref": "BCA - 88812345",
    "status": "COMPLETED",
    "items": [
      {
        "id": "3c4d5e6f-7081-9012-cdef-012345678901",
        "stock_item_id": "1a2b3c4d-5e6f-7890-abcd-ef1234567890",
        "barcode": "00000001",
        "serial_number": "SN-0001",
        "product_name": "Emas Batangan 10gr",
        "weight_gram": 10,
        "price_per_gram": 150000,
        "price_total": 1500000
      }
    ],
    "created_at": "2026-07-31T09:00:00Z",
    "completed_at": "2026-07-31T09:00:00Z"
  }
}
```

Contoh response `BUY` (201) — unit barunya langsung `AVAILABLE`:
```json
{
  "success": true,
  "data": {
    "id": "4d5e6f70-8192-0334-def0-123456789012",
    "transaction_code": "TRX-20260731-0002",
    "type": "BUY",
    "total_amount": 900000,
    "total_weight": 10,
    "payment_method": "CASH",
    "payment_ref": "",
    "status": "COMPLETED",
    "items": [
      {
        "id": "5e6f7081-9203-4415-ef01-234567890123",
        "stock_item_id": "6f708192-0334-5526-f012-345678901234",
        "barcode": "00000002",
        "serial_number": "SN-BUYBACK-01",
        "product_name": "Emas Batangan 10gr",
        "weight_gram": 10,
        "price_per_gram": 90000,
        "price_total": 900000
      }
    ],
    "created_at": "2026-07-31T09:05:00Z",
    "completed_at": "2026-07-31T09:05:00Z"
  }
}
```

Contoh response error:
```json
// 400 — type bukan SELL/SELL_SUPPLIER/tidak didukung, atau customer_id/supplier_id tidak sesuai type
{"success":false,"error":{"code":"BAD_REQUEST","message":"type SELL wajib mengisi customer_id dan tidak boleh mengisi supplier_id"}}

// 400 — payment_method invalid, items kosong, price_total <= 0, atau BUY produk diarsipkan
{"success":false,"error":{"code":"BAD_REQUEST","message":"payment_method harus salah satu dari CASH, TRANSFER, QRIS, DEBIT, KREDIT, GOPAY, OVO, DANA, SHOPEEPAY"}}

// 422 — BUY, serial_number/condition kosong atau invalid per item
{"success":false,"error":{"code":"UNPROCESSABLE_ENTITY","message":"serial_number wajib diisi di setiap item"}}
{"success":false,"error":{"code":"UNPROCESSABLE_ENTITY","message":"condition wajib diisi dan harus GOOD atau BAD di setiap item"}}

// 404 — customer_id/supplier_id/stock_item_id/product_id tidak ditemukan
{"success":false,"error":{"code":"NOT_FOUND","message":"pelanggan tidak ditemukan"}}

// 409 — unit sudah SOLD (termasuk hasil race FOR UPDATE)
{"success":false,"error":{"code":"CONFLICT","message":"unit sudah terjual (SOLD), tidak bisa dijual"}}

// 409 — unit BAD dijual ke pelanggan tanpa confirmed=true
{"success":false,"error":{"code":"CONFLICT","message":"unit kondisi BAD perlu konfirmasi (confirmed=true) untuk dijual ke pelanggan"}}

// 409 — BUY, serial_number sudah dipakai (unit lain, atau item lain di batch yang sama)
{"success":false,"error":{"code":"CONFLICT","message":"serial_number sudah dipakai"}}
```

#### GET /api/transactions/{id} — detail transaksi (BE-602)

Response-nya **objek yang sama persis** dengan response `POST /api/transactions` (fungsi mapping
`toTransactionResponse` yang sama dipakai keduanya) — lengkap dengan `items[]`
(`stock_item_id`/`barcode` per item buat cetak label ulang kalau perlu), tanpa `cogs`. Berlaku
buat transaksi `SELL`, `SELL_SUPPLIER`, maupun `BUY`. `{id}` tidak ditemukan → 404
(`"transaksi tidak ditemukan"`); bukan format UUID → 400.

#### GET /api/transactions/{id}/receipt — struk transaksi (BE-1001)

Payload untuk cetak struk (format dot matrix / continuous form) — backend cuma mengembalikan
data terstruktur JSON, **tidak** merender PDF/gambar dan **tidak** menyimpan file apa pun.
Rendering visual serta komunikasi ke printer sepenuhnya tanggung jawab FE/print-agent (sama
prinsipnya dengan label barcode di `GET /api/stock-items/{id}/label`). Karena kebutuhan saat
ini cukup cetak langsung (belum ada kirim struk lewat email dll.), tidak ada integrasi cloud
storage/PDF di mana pun pada endpoint ini.

```json
{
  "id": "...", "transaction_code": "TRX-20260731-0001", "type": "SELL",
  "total_amount": 1500000, "total_weight": 10, "payment_method": "CASH", "status": "COMPLETED",
  "items": [{ "id": "...", "stock_item_id": "...", "barcode": "...", "serial_number": "...",
              "product_name": "...", "weight_gram": 10, "price_per_gram": 150000, "price_total": 1500000 }],
  "customer": { "name": "Budi Santoso", "phone": "0811111111", "address": "Jl. Mawar No. 2" },
  "supplier": null,
  "store": { "name": "Toko Emas Sejahtera", "address": "Jl. Testing No. 1, Jakarta", "phone": "021-1234567" },
  "invoice_url": "/api/transactions/.../receipt",
  "created_at": "...", "completed_at": "..."
}
```

- **Snapshot, bukan master live**: `items[]` dibaca dari `transaction_items` (nama produk,
  berat, harga per gram — semua sudah ter-snapshot saat transaksi dibuat), sama seperti response
  `GET /api/transactions/{id}` — tidak pernah join ulang ke tabel `products` yang bisa saja
  sudah berubah.
- **`customer`/`supplier`**: hanya salah satu yang terisi sesuai `type` transaksi (`SELL`/`BUY`
  → `customer`, `SELL_SUPPLIER` → `supplier`), sisanya `null`.
- **`store`**: diambil dari tabel `settings` (`shop_name`/`shop_address`/`shop_phone`, yang sama
  dipakai `cmd/seed/main.go`). Kalau setting belum di-seed, field-nya kosong (`""`), bukan error.
- **`invoice_url` di-cache**: kolom `transactions.invoice_url` awalnya `NULL`; panggilan pertama
  ke endpoint ini menghitung `"/api/transactions/{id}/receipt"` dan menyimpannya — panggilan
  berikutnya memakai nilai yang sudah tersimpan (idempotent, tidak ada race karena nilainya
  murni fungsi dari `public_id` transaksi sendiri).
- Berlaku untuk semua tipe transaksi (`SELL`, `BUY`, `SELL_SUPPLIER`). `{id}` tidak ditemukan →
  404 (`"transaksi tidak ditemukan"`); bukan format UUID → 400.

### /api/purchase-orders — order stok ke supplier (BE-901/BE-902/BE-903/BE-904, ADMIN & SUPER_ADMIN)

```bash
POST /api/purchase-orders            # { supplier_id, notes?, items[]:{product_id,quantity,purchase_price} } -> 201
GET  /api/purchase-orders            # ?status=&page=&limit=                                                  -> 200
GET  /api/purchase-orders/{id}       # header + items                                                          -> 200 / 404
POST /api/purchase-orders/{id}/receive  # { items[]:{product_id,serials[]:{serial_number,condition}} }         -> 200 / 404 / 409
POST /api/purchase-orders/{id}/cancel   # tanpa body                                                            -> 200 / 404 / 409
```

Beda dari `/api/products`/`/api/stock-items`: modul ini **cuma** `ADMIN`/`SUPER_ADMIN` — tidak ada
akses `KASIR` sama sekali (PO murni urusan procurement/back-office, semua ticket-nya eksplisit
"Sebagai Admin", beda dari `/api/products` yang butuh diakses kasir buat jualan).

**Alur**: `POST` bikin PO dengan `status=BELUM_DITERIMA` — **belum** ada `stock_items` yang
dibuat sama sekali di titik ini (barang masih "dalam perjalanan", modal sudah keluar tapi stok
belum siap jual). `total_amount` dihitung server (`Σ quantity × purchase_price`), tidak dipercaya
dari input klien. `po_code` format `PO-YYYYMMDD-XXXX` (urut 4 digit per hari), pola generate yang
sama dengan `sku`/`barcode`/`transaction_code` (hitung + insert 1 transaksi DB, retry kalau race).

`POST /{id}/receive` adalah titik barang beneran jadi stok: **satu kali tembak, harus mencakup
semua produk di PO itu** — request `items[]` wajib punya persis produk yang sama dengan item-item
PO, dan `len(serials)` tiap produk harus **persis sama** dengan `quantity` PO-nya (kurang maupun
lebih sama-sama ditolak 400, bukan cuma kasus kurang). **`condition` per serial, bukan per
item** — tiap objek di `serials[]` punya `serial_number` **dan** `condition` sendiri-sendiri
(`{"serial_number": "...", "condition": "GOOD", "production_year": 2024}` — `production_year`
opsional per serial, sama pola & validasi dengan BE-501/BE-801), karena satu shipment/PO tidak
dijamin semua unitnya seragam kondisinya maupun tahun produksinya (bisa campur dalam satu produk
yang sama). Untuk tiap serial: satu `stock_items` baru (`status=AVAILABLE`, `barcode` auto-generate
8 digit angka dari `stock_items_barcode_seq`, sama pola global dengan BE-501/BE-801,
`purchase_price` diambil dari PO — **bukan** dari request receive, `po_id`/`supplier_id` ikut terisi di unit itu — pertama
kalinya kedua kolom ini kepakai, sebelumnya selalu `NULL` baik dari create-stock-item langsung
(BE-501) maupun buyback (BE-801)). Semuanya atomik dalam satu transaksi DB, PO row di-lock
(`FOR UPDATE`) selama
itu supaya PO yang sama tidak bisa diterima dua kali secara konkuren; setelah semua unit masuk,
PO jadi `status=DITERIMA` + `received_at` terisi. PO yang sudah `DITERIMA`/`DIBATALKAN` tidak
bisa di-receive lagi → 409.

`POST /{id}/cancel` cuma bisa dari `status=BELUM_DITERIMA` → `DIBATALKAN` (guard di level SQL,
pola yang sama dengan hard-delete `stock_items` di BE-504); PO yang sudah `DITERIMA` atau
`DIBATALKAN` → 409.

**Validasi**: field item `POST` (`product_id`/`quantity`/`purchase_price`) pakai tier `400` (di
titik ini belum ada `stock_items` yang dibuat, jadi bukan tier validasi "unit fisik"). Field
serial `receive` (`serial_number`/`condition`, masing-masing dicek per objek di `serials[]`)
pakai tier `422`, sama seperti BE-501/BE-801 — langkah ini memang bikin `stock_items` baru.
`serial_number` duplikat (lawan unit yang sudah ada, atau sesama item dalam satu batch receive)
→ `409`, pakai constraint & sentinel yang sama dengan BE-801 (`uq_stock_items_serial_number`).
Produk harus aktif buat `POST` maupun `receive` (400 kalau diarsipkan, pesan sama dengan BE-501);
tidak ada pengecekan aktif buat supplier (konsisten dengan `SELL_SUPPLIER` di BE-702 yang juga
tidak mengeceknya).

Response `GET /` (list) cuma header (tanpa `items[]`); `GET /{id}` dan `POST` sertakan `items[]`
(dengan nama & SKU produk, di-join langsung — `purchase_order_items` tidak punya kolom snapshot
nama produk seperti `transaction_items`, jadi live join memang satu-satunya cara). Response
`receive` juga sertakan `received_units[]`
(`stock_item_id`/`barcode`/`product_name`/`serial_number`/`condition` per unit baru — `condition`
di sini persis kondisi yang diinput per serial di request, bukan disamaratakan) — biar admin yang
baru nerima PO langsung punya barcode buat cetak label, sama alasannya dengan
`stock_item_id`/`barcode` di tiap item transaksi (BE-702/BE-801).

Contoh response `POST /api/purchase-orders` (201):
```json
{
  "success": true,
  "data": {
    "id": "5e6f7081-9203-4415-ef01-234567890123",
    "po_code": "PO-20260731-0001",
    "supplier": { "id": "5c9e1a3b-8f2d-4a6e-9b1c-7d8e9f0a1b2c", "name": "Toko Emas Jaya" },
    "total_amount": 2400000,
    "status": "BELUM_DITERIMA",
    "notes": "",
    "items": [
      {
        "id": "6f708192-0334-5526-f012-345678901234",
        "product": { "id": "6d9f6a2a-2b0c-4e2b-8f1a-1a2b3c4d5e6f", "name": "Emas Batangan 10gr" },
        "quantity": 3,
        "purchase_price": 800000
      }
    ],
    "created_at": "2026-07-31T09:00:00Z",
    "received_at": null
  }
}
```

Contoh request `POST /api/purchase-orders/{id}/receive` — dua unit produk yang sama, kondisi
berbeda (`GOOD` & `BAD`):
```json
{
  "items": [
    {
      "product_id": "6d9f6a2a-2b0c-4e2b-8f1a-1a2b3c4d5e6f",
      "serials": [
        { "serial_number": "PO-SN-1", "condition": "GOOD", "production_year": 2024 },
        { "serial_number": "PO-SN-2", "condition": "BAD" }
      ]
    }
  ]
}
```

Contoh response `POST /api/purchase-orders/{id}/receive` (200) — `status`/`received_at` berubah,
`received_units[]` muncul, `condition` per unit mengikuti request (bukan disamaratakan):
```json
{
  "success": true,
  "data": {
    "id": "5e6f7081-9203-4415-ef01-234567890123",
    "po_code": "PO-20260731-0001",
    "supplier": { "id": "5c9e1a3b-8f2d-4a6e-9b1c-7d8e9f0a1b2c", "name": "Toko Emas Jaya" },
    "total_amount": 2400000,
    "status": "DITERIMA",
    "notes": "",
    "items": [ { "...": "sama seperti response POST" } ],
    "received_units": [
      {
        "stock_item_id": "1a2b3c4d-5e6f-7890-abcd-ef1234567890",
        "barcode": "00000001",
        "product_name": "Emas Batangan 10gr",
        "serial_number": "PO-SN-1",
        "condition": "GOOD",
        "production_year": 2024
      },
      {
        "stock_item_id": "2b3c4d5e-6f70-8901-bcde-f01234567890",
        "barcode": "00000002",
        "product_name": "Emas Batangan 10gr",
        "serial_number": "PO-SN-2",
        "condition": "BAD",
        "production_year": null
      }
    ],
    "created_at": "2026-07-31T09:00:00Z",
    "received_at": "2026-07-31T10:00:00Z"
  }
}
```

Contoh response error:
```json
// 403 — role selain ADMIN/SUPER_ADMIN
{"success":false,"error":{"code":"FORBIDDEN","message":"Anda tidak memiliki akses untuk aksi ini"}}

// 400 — POST, field item tidak valid
{"success":false,"error":{"code":"BAD_REQUEST","message":"quantity setiap item harus lebih besar dari 0"}}

// 400 — receive, items tidak mencakup persis semua produk PO / jumlah serial tidak sama dengan quantity
{"success":false,"error":{"code":"BAD_REQUEST","message":"items harus mencakup semua produk di PO ini, tidak kurang tidak lebih"}}

// 422 — receive, serial_number/condition kosong atau invalid
{"success":false,"error":{"code":"UNPROCESSABLE_ENTITY","message":"serial_number wajib diisi di setiap unit"}}
{"success":false,"error":{"code":"UNPROCESSABLE_ENTITY","message":"production_year tidak valid"}}

// 404 — {id}/supplier_id/product_id tidak ditemukan
{"success":false,"error":{"code":"NOT_FOUND","message":"purchase order tidak ditemukan"}}

// 409 — receive PO yang sudah DITERIMA/DIBATALKAN, atau cancel PO yang sudah DITERIMA/DIBATALKAN
{"success":false,"error":{"code":"CONFLICT","message":"PO sudah diterima atau dibatalkan, tidak bisa diterima lagi"}}
```

### /api/stock-opnames — stock opname / cocok stok fisik (BE-1101/BE-1102/BE-1103, ADMIN & SUPER_ADMIN)

```bash
GET  /api/stock-opnames               # ?status=&page=&limit=          -> 200
POST /api/stock-opnames               # { notes? }                    -> 201 / 409
GET  /api/stock-opnames/{id}          # detail + items[] + summary     -> 200 / 404
POST /api/stock-opnames/{id}/scan     # { barcode }                    -> 200 / 404 / 409
POST /api/stock-opnames/{id}/complete # tanpa body                     -> 200 / 404 / 409
```

Sama seperti `/api/purchase-orders`: **cuma** `ADMIN`/`SUPER_ADMIN`, tidak ada akses `KASIR` —
semua ticket-nya eksplisit "Sebagai Admin".

**Alur**: `POST` bikin sesi baru `status=IN_PROGRESS`, `opname_code` format `OPN-YYYYMMDD-XXXX`
(pola generate yang sama dengan `sku`/`barcode`/`transaction_code`/`po_code`), `opname_date` diisi
`CURRENT_DATE` di level DB (bukan input klien).

**Cuma boleh ada 1 sesi `IN_PROGRESS` dalam satu waktu** — `POST` ditolak `409` kalau masih ada
sesi lain yang belum di-`complete`. Guard-nya di level DB (partial unique index
`uq_stock_opnames_single_in_progress` di kolom `status` `WHERE status = 'IN_PROGRESS'`, migrasi
`000026`), bukan cek-lalu-insert di aplikasi — jadi dua request `POST` konkuren nggak bisa
sama-sama lolos bikin 2 sesi `IN_PROGRESS`. Sesi yang `IN_PROGRESS` **bisa dilanjutkan** kapan saja
lewat `POST /{id}/scan` (belum ada "resume" khusus — tinggal lanjut scan ke sesi yang sama, itu
sebabnya sesi `IN_PROGRESS` juga muncul di `GET /api/stock-opnames` list, bukan cuma yang
`COMPLETED`, supaya sesi yang ketinggalan gampang ditemukan lagi dari UI).

#### GET /api/stock-opnames — daftar sesi, header-only

Sama pola dengan `GET /api/purchase-orders`: list `items[]` **header-only** — tiap baris nggak
bawa daftar unit yang discan (field `items` di-omit, sama kayak PO list), dan `summary` selalu
`{match:0, missing:0, unexpected:0}` (dihitung dari items yang memang tidak di-fetch buat list,
bukan `summary` sesi yang sebenarnya) — buat hitungan asli, fetch `GET /{id}`. Urut `id DESC`
(terbaru duluan), paginated (`?page=`/`?limit=`, default `1`/`20`, `limit` di-cap `100`). `?status=`
opsional — kalau diisi harus `IN_PROGRESS`, `COMPLETED`, atau `CANCELLED` (400 kalau bukan;
`CANCELLED` ada di `CHECK` constraint DB tapi belum ada endpoint yang men-set status itu, jadi
filter ini akan selalu balik list kosong sampai ada fitur cancel opname).

`POST /{id}/scan` mencatat satu kali scan barcode fisik ke `stock_opname_items`:
- Barcode dikenal & `stock_items.status=AVAILABLE` → `system_status=AVAILABLE`,
  `physical_status=FOUND`, `result=MATCH`.
- Barcode dikenal tapi `stock_items.status=SOLD` (unit yang menurut sistem sudah tidak seharusnya
  ada secara fisik, tapi ternyata discan) → `result=UNEXPECTED`.
- **Barcode sama sekali tidak dikenal** (tidak ada baris `stock_items` yang cocok) → **ditolak
  404** (`"barcode tidak ditemukan"`, pesan yang sama dengan `GET /stock-items/lookup`), **bukan**
  dicatat sebagai `UNEXPECTED` — `stock_opname_items.stock_item_id` adalah FK `NOT NULL`, jadi unit
  yang benar-benar tidak dikenal sistem memang tidak bisa direkam sebagai baris di tabel ini.
- Unit yang sama discan dua kali dalam satu sesi → 409 (`"unit sudah discan di sesi ini"`) —
  dicegah eksplisit di level repository (tidak ada unique constraint di kolom ini), supaya tidak
  dobel hitung.
- Scan terhadap sesi yang bukan `IN_PROGRESS` (sudah `COMPLETED`) → 409. Sesi `{id}` tidak
  ditemukan → 404. `barcode` kosong → 400.

Response `scan` cuma item yang baru discan (bukan seluruh sesi) — kasir/admin yang lagi scan satu
per satu butuh feedback instan buat unit itu saja, bukan re-fetch semua item yang sudah discan.

`POST /{id}/complete` menutup sesi: tiap unit `stock_items.status=AVAILABLE` yang **belum pernah
discan** di sesi ini otomatis dapat baris `system_status=AVAILABLE`, `physical_status=NOT_FOUND`,
`result=MISSING` (satu `INSERT ... SELECT ... WHERE NOT EXISTS` atomik, bukan loop per unit), lalu
`status` sesi jadi `COMPLETED`. Sesi yang sudah `COMPLETED` di-complete lagi → 409; setelah
`COMPLETED`, `scan` juga otomatis ditolak 409 (guard yang sama). Baik `scan` maupun `complete`
mengunci baris `stock_opnames` (`FOR UPDATE`) dalam transaksi yang sama dengan pengecekan status +
tulisan datanya — pola yang sama dengan `receive` PO (BE-903) — supaya scan tidak bisa balapan
dengan complete yang konkuren.

`GET /{id}` (dipakai juga sebagai response `POST`/`scan`/`complete`) selalu menyertakan `summary`
(`{match, missing, unexpected}`, dihitung dari `items[]` yang sudah di-fetch, bukan query terpisah)
dan `items[]` (barcode/nama produk di-join dari `stock_items`/`products`, sama pola "list/detail
sertakan nama, bukan FK mentah" seperti `StockItemWithRefs`/`PurchaseOrderWithSupplier`).

Contoh response `GET /api/stock-opnames?status=IN_PROGRESS` (200) — header-only, `summary` selalu nol:
```json
{
  "success": true,
  "data": {
    "items": [
      {
        "id": "7a8b9c0d-1e2f-3456-7890-abcdef123456",
        "opname_code": "OPN-20260731-0002",
        "opname_date": "2026-07-31",
        "status": "IN_PROGRESS",
        "notes": "",
        "summary": { "match": 0, "missing": 0, "unexpected": 0 },
        "created_at": "2026-07-31T10:00:00Z"
      }
    ],
    "pagination": { "page": 1, "limit": 20, "total": 1, "total_pages": 1 }
  }
}
```

Contoh response `POST /api/stock-opnames/{id}/complete` (200):
```json
{
  "success": true,
  "data": {
    "id": "7a8b9c0d-1e2f-3456-7890-abcdef123456",
    "opname_code": "OPN-20260731-0001",
    "opname_date": "2026-07-31",
    "status": "COMPLETED",
    "notes": "",
    "items": [
      {
        "id": "8b9c0d1e-2f34-5678-90ab-cdef12345678",
        "stock_item_id": "1a2b3c4d-5e6f-7890-abcd-ef1234567890",
        "barcode": "00000001",
        "product_name": "Emas Batangan 10gr",
        "system_status": "AVAILABLE",
        "physical_status": "FOUND",
        "result": "MATCH"
      },
      {
        "id": "9c0d1e2f-3456-7890-abcd-ef1234567890",
        "stock_item_id": "2b3c4d5e-6f78-9012-bcde-f12345678901",
        "barcode": "00000002",
        "product_name": "Emas Batangan 10gr",
        "system_status": "AVAILABLE",
        "physical_status": "NOT_FOUND",
        "result": "MISSING"
      }
    ],
    "summary": { "match": 1, "missing": 1, "unexpected": 0 },
    "created_at": "2026-07-31T09:00:00Z"
  }
}
```

Contoh response error:
```json
// 404 — barcode tidak dikenal sama sekali
{"success":false,"error":{"code":"NOT_FOUND","message":"barcode tidak ditemukan"}}

// 409 — unit yang sama discan dua kali dalam satu sesi
{"success":false,"error":{"code":"CONFLICT","message":"unit sudah discan di sesi ini"}}

// 409 — scan/complete terhadap sesi yang sudah COMPLETED
{"success":false,"error":{"code":"CONFLICT","message":"sesi opname sudah selesai, tidak bisa discan lagi"}}

// 409 — POST create, masih ada sesi lain yang IN_PROGRESS
{"success":false,"error":{"code":"CONFLICT","message":"masih ada sesi opname yang belum selesai (IN_PROGRESS), selesaikan atau lanjutkan sesi itu dulu sebelum membuat sesi baru"}}

// 400 — GET list, ?status= bukan IN_PROGRESS/COMPLETED/CANCELLED
{"success":false,"error":{"code":"BAD_REQUEST","message":"status harus IN_PROGRESS, COMPLETED, atau CANCELLED"}}
```

### /api/expense-categories & /api/expenses — pengeluaran operasional (BE-1201/BE-1202, ADMIN & SUPER_ADMIN)

```bash
POST   /api/expense-categories        # { name }                                          -> 201
GET    /api/expense-categories        # tanpa pagination, list flat                        -> 200
GET    /api/expense-categories/{id}                                                        -> 200 / 404
PUT    /api/expense-categories/{id}   # { name }                                           -> 200 / 404 / 409
DELETE /api/expense-categories/{id}                                                        -> 200 / 404 / 409

POST   /api/expenses                  # { category_id, amount, description?, expense_date } -> 201
GET    /api/expenses                  # ?category_id=&date_from=&date_to=&page=&limit=      -> 200
GET    /api/expenses/{id}                                                                   -> 200 / 404
PUT    /api/expenses/{id}             # { category_id, amount, description?, expense_date } -> 200 / 404
DELETE /api/expenses/{id}                                                                   -> 200 / 404
```

**Beda dari resource CRUD lain di codebase ini**: `expense_categories` dan `expenses` **tidak
punya kolom `is_active` maupun `updated_at`** (migrasi `000004`/`000012`) — satu-satunya resource
di seluruh backend ini tanpa soft-delete. `DELETE` di sini karenanya **hapus beneran** (hard
delete), bukan nonaktifkan seperti `categories`/`brands`/`suppliers`/`customers`. `name` di
`expense_categories` unik **case-sensitive** (`UNIQUE (name)` polos), beda dengan `categories`
yang case-insensitive (`UNIQUE (lower(name))`) — jadi "Listrik" dan "listrik" dianggap dua nama
berbeda di sini.

`DELETE /api/expense-categories/{id}` ditolak `409` kalau kategori itu masih dipakai satu atau
lebih `expenses` (`expenses.category_id` adalah FK `RESTRICT` ke `expense_categories`, jadi
Postgres sendiri yang menegakkan integritasnya — bukan pre-check manual yang rawan race, error
foreign-key-violation dari DB langsung ditangkap dan dipetakan ke 409).

`GET /api/expense-categories` (list flat, tanpa pagination) urut **terbaru duluan**
(`ORDER BY id DESC`).

`amount`/`category_id`/`expense_date` di `POST`/`PUT /api/expenses` wajib diisi (`400` kalau
kosong/invalid — bukan tier `422`, karena tier itu di codebase ini khusus validasi pembuatan unit
stok fisik, bukan buat resource operasional seperti ini); `amount` juga harus `> 0`.
`category_id` yang tidak ditemukan → `404`.

`GET /api/expenses` filter periode & kategori (AC BE-1202): `?category_id=` (harus `public_id`
kategori yang valid, 404 kalau tidak ada), `?date_from=&date_to=` (masing-masing independen,
inklusif di kedua ujung — bisa isi salah satu saja buat rentang open-ended), format tanggal
`YYYY-MM-DD` sama seperti `purchase_date` di `stock_items`. Urutan default: `expense_date`
terbaru duluan.

Contoh response `GET /api/expenses/{id}` (200):
```json
{
  "success": true,
  "data": {
    "id": "3f4a5b6c-7d8e-9f01-2345-6789abcdef01",
    "category": { "id": "2e3f4a5b-6c7d-8e9f-0123-456789abcdef", "name": "Listrik" },
    "amount": 500000,
    "description": "Tagihan bulan Juli",
    "expense_date": "2026-07-01",
    "created_at": "2026-07-31T09:00:00Z"
  }
}
```

Contoh response error:
```json
// 409 — hapus kategori yang masih dipakai expense
{"success":false,"error":{"code":"CONFLICT","message":"kategori masih dipakai oleh pengeluaran, tidak bisa dihapus"}}

// 400 — field wajib expense kosong/invalid
{"success":false,"error":{"code":"BAD_REQUEST","message":"expense_date wajib diisi"}}
```

### /api/reports — laporan (BE-1301/BE-1302/BE-1303, ADMIN & SUPER_ADMIN)

```bash
GET /api/reports/transactions   # ?from=&to=&type=                          -> 200
GET /api/reports/stock          # ?threshold=                                -> 200
GET /api/reports/finance        # ?from=&to=                                 -> 200
GET /api/reports/dashboard      # ?from=&to=&threshold=&pending_limit=       -> 200
```

Empat endpoint read-only, tanpa pagination (hasil agregat yang jumlahnya sudah kebatasi — beberapa
baris per tipe, satu baris per produk aktif, beberapa angka — bukan listing per baris).

**`GET /api/reports/transactions`** — rekap `transaction_count`/`total_amount`/`total_weight`
per tipe (`SELL`, `BUY`, `SELL_SUPPLIER`). `?from=&to=` masing-masing independen opsional
(format `YYYY-MM-DD`, inklusif di kedua ujung, filter di `created_at::date` karena `transactions`
tidak punya kolom `DATE` polos); `?type=` opsional, kalau diisi harus salah satu dari ketiga tipe
(400 kalau bukan) dan `breakdown[]` cuma berisi tipe itu. `breakdown[]` cuma memuat tipe yang
punya minimal 1 transaksi di rentang yang difilter (tidak ada baris nol buat tipe yang kosong).
`total` adalah jumlah seluruh `breakdown[]` (dihitung di Go, bukan query kedua).

**`GET /api/reports/stock`** — satu baris per **produk aktif** (`is_active=true`), `LEFT JOIN` ke
`stock_items` supaya produk yang stoknya benar-benar habis (0 unit `AVAILABLE`) **tetap muncul**
dengan `available_count: 0` dan `low_stock: true` — justru itu kasus paling mendesak buat
peringatan stok menipis. Produk yang diarsipkan tidak muncul sama sekali (tidak akan di-restock,
jadi peringatan buat produk itu tidak actionable). `good_count`/`bad_count` cuma menghitung unit
`AVAILABLE` (unit `SOLD` tidak dihitung sama sekali, di kondisi manapun). `?threshold=` opsional,
default `5` kalau tidak diisi atau ≤0 — `low_stock = available_count <= threshold`, dihitung ulang
tiap request, tidak disimpan di mana pun (tidak ada setting baru buat ini).

**`GET /api/reports/finance`** — bukan cuma satu angka net, tapi breakdown lengkap biar auditable:

- `sales_breakdown[]` — satu baris per tipe yang dianggap penjualan (**`SELL`** dan
  **`SELL_SUPPLIER`**, keduanya sama-sama punya `cogs` yang ke-set saat checkout), masing-masing
  dengan `transaction_count`, `total_revenue` (Σ `price_total`), `total_cogs` (Σ `cogs`), dan
  `gross_profit` (`total_revenue - total_cogs`) per tipe itu sendiri. **`BUY`** tidak pernah
  muncul di sini — `cogs`-nya memang selalu `NULL` (unit baru masuk stok, belum ada yang dijual).
- `expense_breakdown[]` — satu baris per `expense_categories` yang punya minimal satu `expenses`
  di rentang yang difilter, dengan `total_amount` per kategori.
- `total_revenue`, `total_cogs`, `gross_profit` — jumlah seluruh `sales_breakdown[]` (dihitung di
  Go, bukan query kedua, sama pola dengan `total` di laporan transaksi).
- `gross_margin_percent = gross_profit / total_revenue * 100` — `0` (bukan `NaN`/`Inf`, yang bakal
  bikin JSON marshal gagal total) kalau `total_revenue` masih nol (belum ada penjualan sama sekali
  di periode itu).
- `total_expenses` — jumlah seluruh `expense_breakdown[]`. `net_profit = gross_profit -
  total_expenses`.

`?from=&to=` sama seperti laporan transaksi — masing-masing independen opsional, filter transaksi
di `created_at::date`, filter expense langsung di `expense_date` (kolom `DATE` asli).

**`GET /api/reports/dashboard`** — satu payload gabungan buat halaman dashboard admin, isinya
komposisi dari tiga laporan di atas plus satu data baru (PO yang masih pending):

- `finance` — persis isi `/api/reports/finance` (tanpa `from`/`to` di dalamnya, karena sudah ada
  di level atas `dashboard`), dipanggil lewat `FinanceReport` internal — **bukan** query terpisah,
  jadi angkanya dijamin selalu konsisten dengan endpoint finance yang berdiri sendiri.
- `transaction_breakdown[]`/`transaction_total` — persis isi `/api/reports/transactions` (tanpa
  filter `type`, karena dashboard mau semua tipe), juga lewat `TransactionReport` internal.
- `low_stock_items[]`/`low_stock_threshold` — subset dari `/api/reports/stock`, **difilter cuma
  yang `low_stock=true`** (dashboard cuma mau yang actionable, bukan seluruh katalog produk
  aktif); `?threshold=` sama seperti laporan stok, default `5`.
- `pending_purchase_orders[]`/`pending_purchase_orders_total` — PO yang masih `status=BELUM_DITERIMA`,
  terbaru duluan, dibatasi `?pending_limit=` (default `5`) — tapi `_total` selalu jumlah
  sebenarnya, walau lebih besar dari yang ditampilkan di `pending_purchase_orders[]`.

**`?from=&to=` default ke bulan berjalan** (tanggal 1 bulan ini sampai hari ini, dalam **UTC** —
bukan timezone lokal server, karena `created_at::date` di Postgres dibandingkan dalam timezone
sesi DB yang UTC; pakai timezone lokal di sini bisa diam-diam menghilangkan transaksi "hari ini"
kalau tanggal lokal server sudah ganti hari duluan sebelum UTC-nya) kalau keduanya tidak diisi;
kalau salah satu/keduanya diisi, dipakai persis seperti apa adanya (sama seperti laporan lain).

Contoh response `GET /api/reports/transactions` (200):
```json
{
  "success": true,
  "data": {
    "from": "2026-07-01",
    "to": "2026-07-31",
    "breakdown": [
      { "type": "BUY", "transaction_count": 5, "total_amount": 4500000, "total_weight": 45 },
      { "type": "SELL", "transaction_count": 12, "total_amount": 15000000, "total_weight": 120.5 },
      { "type": "SELL_SUPPLIER", "transaction_count": 2, "total_amount": 900000, "total_weight": 9 }
    ],
    "total": { "transaction_count": 19, "total_amount": 20400000, "total_weight": 174.5 }
  }
}
```

Contoh response `GET /api/reports/finance` (200):
```json
{
  "success": true,
  "data": {
    "from": "2026-07-01",
    "to": "2026-07-31",
    "sales_breakdown": [
      { "type": "SELL", "transaction_count": 12, "total_revenue": 15000000, "total_cogs": 10500000, "gross_profit": 4500000 },
      { "type": "SELL_SUPPLIER", "transaction_count": 2, "total_revenue": 900000, "total_cogs": 700000, "gross_profit": 200000 }
    ],
    "expense_breakdown": [
      { "category": { "id": "...", "name": "Listrik" }, "total_amount": 500000 },
      { "category": { "id": "...", "name": "Gaji Karyawan" }, "total_amount": 700000 }
    ],
    "total_revenue": 15900000,
    "total_cogs": 11200000,
    "gross_profit": 4700000,
    "gross_margin_percent": 29.56,
    "total_expenses": 1200000,
    "net_profit": 3500000
  }
}
```

Contoh response `GET /api/reports/dashboard` (200):
```json
{
  "success": true,
  "data": {
    "from": "2026-08-01",
    "to": "2026-08-01",
    "finance": {
      "sales_breakdown": [ { "...": "sama seperti /api/reports/finance, tanpa from/to" } ],
      "expense_breakdown": [ { "...": "idem" } ],
      "total_revenue": 1500000, "total_cogs": 1000000, "gross_profit": 500000,
      "gross_margin_percent": 33.33, "total_expenses": 0, "net_profit": 500000
    },
    "transaction_breakdown": [
      { "type": "SELL", "transaction_count": 1, "total_amount": 1500000, "total_weight": 10 }
    ],
    "transaction_total": { "transaction_count": 1, "total_amount": 1500000, "total_weight": 10 },
    "low_stock_threshold": 5,
    "low_stock_items": [
      { "product": { "id": "...", "name": "Cincin Emas 5gr", "sku": "..." }, "available_count": 1, "good_count": 1, "bad_count": 0, "low_stock": true }
    ],
    "pending_purchase_orders": [
      { "id": "...", "po_code": "PO-20260801-0001", "supplier_name": "Toko Emas Jaya", "total_amount": 800000, "created_at": "2026-08-01T09:00:00Z" }
    ],
    "pending_purchase_orders_total": 1
  }
}
```

### /api/settings — pengaturan toko (ADMIN & SUPER_ADMIN)

```bash
GET /api/settings   # list shop_name, shop_address, shop_phone            -> 200
PUT /api/settings   # { shop_name?, shop_address?, shop_phone? }          -> 200 / 400
```

Key-value store (tabel `settings`, migrasi `000013`) — dipakai buat data toko yang tampil di
struk transaksi (`GET /api/transactions/{id}/receipt`, lihat `transactionService.GetReceipt`).
Endpoint ini **hanya mengekspos 3 key yang sudah di-seed** (`cmd/seed`): `shop_name`,
`shop_address`, `shop_phone`. Beda dari resource CRUD lain, `PUT` **tidak bisa bikin key baru** —
kalau key belum ada baris di tabel (mis. seeder belum jalan), update dibalas `500` (bukan 404,
karena dari sisi API key ini seharusnya selalu sudah ada).

Semua field di body `PUT` **opsional** (pakai pointer di Go) — kirim cuma `shop_phone` misalnya,
`shop_name`/`shop_address` tidak ikut berubah. Field yang dikirim tidak boleh string kosong
(`400`). Minimal satu field harus diisi, body kosong ditolak `400`. `updated_by` diambil otomatis
dari user yang login (JWT claims), bukan dari body request.

Contoh response `GET /api/settings` (200):
```json
{
  "success": true,
  "data": [
    { "key": "shop_address", "value": "Jl. Contoh No. 1, Jakarta", "description": "Alamat toko untuk struk", "updated_at": "2026-08-01T09:00:00Z" },
    { "key": "shop_name", "value": "Gold Track Store", "description": "Nama toko untuk struk & laporan", "updated_at": "2026-08-01T09:00:00Z" },
    { "key": "shop_phone", "value": "0800-0000-0000", "description": "Nomor telepon toko untuk struk", "updated_at": "2026-08-01T09:00:00Z" }
  ]
}
```

Contoh response error:
```json
// 400 — body kosong / semua field null
{"success":false,"error":{"code":"BAD_REQUEST","message":"minimal satu field harus diisi"}}

// 400 — field dikirim tapi string kosong
{"success":false,"error":{"code":"BAD_REQUEST","message":"shop_phone tidak boleh kosong"}}
```

Nambah key settings baru (di luar 3 yang ada) perlu dua langkah manual: insert baris lewat
`cmd/seed` (atau migrasi baru), lalu tambahkan key-nya ke `shopSettingKeys` di
`internal/service/settings_service.go` — repository dan handler tidak perlu diubah.

### /api/gold-prices/active — harga emas rujukan (BE-404, semua role login)

```bash
GET /api/gold-prices/active   # read-only, tidak fetch on-demand           -> 200
```

Beda dari resource lain di backend ini: **bukan dipicu request user**. Sebuah goroutine di
`cmd/api/main.go` (`runGoldPriceSync`, pakai `time.Ticker`) fetch harga dari API pihak ketiga
([`logam-mulia-api.iamutaki.workers.dev`](https://logam-mulia-api.iamutaki.workers.dev)) tiap
interval (env `GOLD_PRICE_SYNC_INTERVAL`, default `1h`), sekali langsung saat startup lalu
berulang. Endpoint ini **cuma baca baris terakhir yang berhasil di-sync dari DB** — response time
tidak pernah bergantung ke API eksternal, dan tetap bisa dijawab walau API itu sedang down.

Sengaja ditaruh di `cmd/api/main.go`, **bukan** di `internal/app/app.go` — `app.New` juga dipanggil
oleh e2e test harness (`test/e2e`), jadi kalau goroutine sync-nya ditaruh di situ, tiap test run
bakal ikut manggil API eksternal beneran. `GoldPriceService` di-expose lewat `App` struct supaya
`main.go` yang menjalankan goroutine-nya sendiri.

Source yang dipakai: feed `anekalogam` (bukan `pegadaian` dkk — cuma feed ini yang punya harga
emas batangan Antam asli, bukan pecahan gram kecil), difilter ke entry dengan
`weight=1` **dan** `materialType="LM Antam produksi tahun 2026"` — sengaja bukan entry lain yang
juga kebetulan 1gr (`"...Certicard gramasi 100 gram..."`, materialType-nya cuma muncul di 1gr,
tidak konsisten di berat lain, jadi tidak dipakai). Kalau API eksternal ubah bentuk response atau
source ini hilang, sync gagal (fallback di bawah) sampai ada perubahan kode.

**Insert atomik**: satu transaksi DB men-nonaktifkan baris `is_active=true` yang lama
(`effective_until=now()`) lalu insert baris baru `is_active=true` — tidak pernah 0 atau lebih dari
1 baris aktif (`GoldPriceRepository.ReplaceActive`).

**Fallback kalau fetch gagal/timeout** (5 detik, API eksternal ini "hobby project" tanpa SLA):
baris aktif yang ada **tidak disentuh sama sekali**, error di-log di server (`gold price sync
failed`), lanjut ke tick berikutnya — tidak pernah bikin endpoint baca ini 5xx gara-gara API
eksternal down.

`created_by` di `gold_prices` (migrasi `000006`) awalnya `NOT NULL` — diubah jadi nullable di
migrasi `000024` khusus buat baris hasil sync ini (`NULL` = "diisi sistem otomatis", bukan pura-pura
pakai user id tertentu). Baris yang dibuat manual (kalau nanti ada endpoint override) tetap wajib
isi `created_by`.

Kalau belum pernah ada sync sukses (fresh deploy, goroutine belum sempat jalan), response tetap
`200` dengan `data: null` — **bukan** `404`/`500` — supaya FE tinggal sembunyikan widget-nya kalau
null. Field ini dikirim eksplisit sebagai `null` (bukan field yang di-omit), beda dari envelope
default `pkg/response` yang pakai `omitempty` — lihat `goldPriceEnvelope` di
`internal/handler/gold_price_handler.go`.

Harga di sini **murni referensi/tampilan** — tidak dipakai sebagai default value di form input
harga manual transaksi (`BE-702`/`BE-801`), dan tidak ada endpoint `POST`/`PUT` manual di scope
ticket ini.

Contoh response (200, ada data):
```json
{
  "success": true,
  "data": {
    "source": "logam-mulia-api:anekalogam",
    "price_buy": 2525000,
    "price_sell": 2605000,
    "price_reference": 2605000,
    "fetched_at": "2026-08-04T01:13:27.116436+07:00"
  }
}
```

Contoh response (200, belum pernah sync):
```json
{"success": true, "data": null}
```

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

**`products_test.go`** — `/api/products` (BE-201 create + BE-202 list/detail + BE-203 update + BE-204 archive)
- `POST` (ADMIN & SUPER_ADMIN): tanpa token → 401; role KASIR → 403
- Create dengan kategori "Batangan" + brand "Antam" + berat 10 → `sku == "BAT-ANT-10-001"`,
  `is_active == true`, `category`/`brand` nested `{id, name}` sama dengan yang dikirim/dipilih
- Dua create dengan kombinasi kategori+brand+berat identik → SKU kedua `...-002` (urut naik)
- Field wajib kosong (`name`/`category_id`/`brand_id`) → 400; `weight_gram <= 0` → 400
- `category_id`/`brand_id` UUID valid tapi tidak ada → 404
- `category_id` merujuk kategori yang sudah `is_active=false` → 400
- `GET` list & detail (semua role, token valid): tanpa token → 401; role KASIR tetap bisa akses
  (beda dari `POST` yang admin-only)
- List: produk yang diarsipkan (`is_active=false`, di-set langsung lewat SQL di test ini — di
  produksi lewat `DELETE`, lihat BE-204 di bawah) tidak muncul, `pagination.total` cuma
  menghitung yang aktif
- List: `?search=`, `?category_id=`, `?brand_id=` masing-masing menyempitkan hasil dengan benar
- List: `?limit=&page=` membagi halaman dengan benar, `pagination.total_pages` sesuai
- List: `?category_id=` UUID valid tapi tidak ada → 200 dengan `items: []` (bukan 404 — ini filter)
- Detail: mengembalikan data lengkap termasuk `category`/`brand` nested; UUID valid tapi tidak ada
  → 404; format id bukan UUID → 400
- Detail: produk yang sudah diarsipkan **tetap** bisa diambil (200, `is_active: false`) — cuma
  disembunyikan dari list, bukan dari lookup-by-id
- `PUT` (ADMIN & SUPER_ADMIN): tanpa token → 401; role KASIR → 403
- Update mengubah name/category/brand/weight/description, tapi **`sku` tetap sama** dengan sebelum
  diubah — inti acceptance criteria BE-203
- `{id}` tidak ditemukan → 404; format id bukan UUID → 400
- Field wajib kosong → 400; `weight_gram <= 0` → 400
- `category_id`/`brand_id` UUID valid tapi tidak ada → 404; merujuk yang `is_active=false` → 400
- Update dengan `is_active: true` pada produk yang sudah diarsipkan (lewat SQL) → mereaktivasi
  (200, `is_active == true`, muncul lagi di `GET /api/products`)
- `DELETE` (ADMIN & SUPER_ADMIN): tanpa token → 401; role KASIR → 403
- Produk tanpa `stock_items` sama sekali → diarsipkan (200), `is_active=false` di detail, hilang
  dari list
- Produk dengan `stock_items` `status='AVAILABLE'` (di-insert langsung lewat SQL — belum ada
  endpoint stok) → 409 dengan pesan jelas, produk tetap `is_active=true`
- Produk yang stoknya semua `status='SOLD'` → tetap boleh diarsipkan (200) — guard-nya spesifik
  ke `AVAILABLE`, bukan "ada histori stok apapun"
- `{id}` tidak ditemukan → 404; format id bukan UUID → 400

**`suppliers_test.go`** — `/api/suppliers` (CRUD, ADMIN & SUPER_ADMIN, BE-301)
- Semua route tanpa token → 401; role KASIR → 403
- Alur penuh create → list → get → update → delete: create cuma dengan `name` berhasil
  (field opsional kosong di response), muncul di list, get by `public_id` benar, update
  mengubah semua field, delete = soft delete (get setelahnya tetap 200, `is_active=false`)
- Create tanpa `name` → 400
- List: `?search=` cocok ke `name`; `?limit=&page=` paginasi dengan benar
  (`pagination.total`/`total_pages` sesuai)
- List: supplier yang sudah diarsipkan **tetap** muncul di list (beda dari `/api/products`)
- Get dengan id bukan UUID → 400; UUID valid tapi tidak ada → 404
- `GET /{id}/transactions` (riwayat gabungan): tanpa token → 401; role KASIR → 403; supplier
  dengan 1 PO + 1 transaksi `SELL_SUPPLIER` → `items[]` berisi keduanya urut terbaru duluan,
  `source`/`code`/`status`/`total_amount` masing-masing sesuai asalnya (`po_code`/`status` PO vs
  `transaction_code`/`status` transaksi); riwayat milik supplier lain tidak ikut ke-list;
  `?page=&limit=` paginasi lintas kedua sumber dengan benar; supplier tanpa riwayat →
  `items: []` (bukan error); supplier tidak ditemukan → 404; format id bukan UUID → 400

**`stock_items_test.go`** — `/api/products/{productId}/stock-items` & `/api/stock-items`
(BE-501 create, BE-502 list/detail, BE-503 update, BE-504 hard delete, BE-505 label)
- `POST` (ADMIN & SUPER_ADMIN): tanpa token → 401; role KASIR → 403
- Create → `barcode` 8 digit angka (`nextval('stock_items_barcode_seq')`), `status == "AVAILABLE"`;
  dua unit (produk sama maupun beda) → barcode berbeda & naik terus, bukan lagi per-produk
- `serial_number` kosong / `condition` kosong atau bukan `GOOD`/`BAD` / `purchase_price <= 0` /
  `purchase_date` kosong atau format salah → **422** (bukan 400 — status code baru khusus
  validasi field stock item)
- Create ke produk yang sudah diarsipkan (`is_active=false`) → 400; `{productId}` tidak ada → 404
- Create dengan `serial_number` yang sudah dipakai unit lain → 409 (`uq_stock_items_serial_number`, BE-801)
- `GET` list & detail (semua role, token valid): KASIR bisa akses (beda dari endpoint write)
- List: filter `?status=`/`?condition=` menyempitkan hasil dengan benar (unit `SOLD` di-set
  langsung lewat SQL di test — belum ada endpoint "mark as sold"); `?search=` cocok ke
  `serial_number`; `?limit=&page=` paginasi dengan benar
- Detail: response mengandung `barcode` lengkap; id bukan UUID → 400; tidak ditemukan → 404
- `PUT` (ADMIN & SUPER_ADMIN): mengubah `serial_number`/`condition`/`purchase_price`/
  `purchase_date`/`notes`, tapi **`barcode` dan `product.id` tetap sama** — inti acceptance
  criteria BE-503
- `PUT` ke unit yang sudah `SOLD` (lewat SQL) → 409; `{id}` tidak ditemukan → 404; field
  invalid → 422
- `DELETE` (ADMIN & SUPER_ADMIN) unit `AVAILABLE` → 200, lalu `GET /{id}` setelahnya → **404**
  (kolomnya beneran hilang dari DB — hard delete, bukan soft delete kayak resource lain)
- `DELETE` unit yang sudah `SOLD` → 409, unit tetap ada setelahnya (guard di level SQL
  `AND status = 'AVAILABLE'` di `stock_item_repository.go`, bukan cuma cek aplikasi)
- `GET /{id}/label` mengembalikan `barcode`/`product_name`/`weight_gram`/`serial_number`;
  tetap 200 untuk unit yang sudah `SOLD` (cetak ulang diperbolehkan); tidak ditemukan → 404

**`customers_test.go`** — `/api/customers` (CRUD dengan role split, BE-601)
- `POST`/`GET`/`GET /{id}` tanpa token → 401
- **KASIR bisa `POST`/`GET`/`GET /{id}`** (beda dari semua resource lain — di sini create/read
  memang dibuka buat kasir), tapi **403** kalau KASIR coba `PUT`/`DELETE`
- Alur penuh create → list → get → update → delete: create cuma dengan `name` berhasil (field
  opsional kosong di response), muncul di list, get benar, update mengubah semua field, delete =
  soft delete (get setelahnya tetap 200, `is_active=false`)
- Create tanpa `name` → 400; `id_type` di luar `KTP`/`SIM`/`PASSPORT` → 400
- List: `?search=` cocok ke `name` **atau** `phone`; `?limit=&page=` paginasi dengan benar
- List: pelanggan yang diarsipkan **tetap** muncul di list (sama seperti `/api/suppliers`)
- Get dengan id bukan UUID → 400; UUID valid tapi tidak ada → 404
- Skenario `GET /api/customers/{id}/transactions` (riwayat, BE-602) ada di `transactions_test.go`,
  bukan di sini — lihat di bawah, karena endpoint ini secara implementasi di-handle oleh
  `TransactionHandler`, bukan `CustomerHandler` (sama alasan `ListByProduct` ada di
  `StockItemHandler`, bukan `ProductHandler`).

**`stock_items_test.go`** (tambahan) — `GET /api/stock-items/lookup` (BE-701/BE-703)
- Tanpa token → 401
- Barcode ditemukan & `AVAILABLE` → 200, response berisi unit + produk + `condition`
- Barcode tidak ditemukan → 404; unit sudah `SOLD` → 409
- `?type=SELL` + unit `condition=BAD` → `requires_confirmation: true`
- `?type=SELL_SUPPLIER` + unit `condition=BAD` → `requires_confirmation: false`
- `?type` dikosongkan + unit `condition=BAD` → `requires_confirmation: false`

**`transactions_test.go`** — `POST /api/transactions` (BE-702/BE-703/BE-801)
- Tanpa token → 401
- Jual ke pelanggan (`type=SELL`) → 201, `transaction_code` format `TRX-YYYYMMDD-0001`,
  `total_amount`/`total_weight` terjumlah benar, `items[].price_per_gram == price_total / weight_gram`,
  unit yang terjual jadi `status=SOLD` (dicek lewat `GET /api/stock-items/{id}`)
- `type=SELL_SUPPLIER` tanpa `supplier_id` → 400; isi `customer_id` alih-alih `supplier_id` → 400
- `type=SELL` tanpa `customer_id` → 400
- Dua transaksi di hari yang sama → `transaction_code` urut naik (`-0001`, `-0002`)
- Jual unit yang sama dua kali (request kedua) → 409, transaksi pertama tidak terganggu
- **Dua request konkuren menjual unit yang sama** (goroutine, bukan raw `doRequest` di dalam
  goroutine — `t.Fatalf` tidak boleh dipanggil dari goroutine selain punya test) → tepat satu
  `201` dan satu `409`, diverifikasi berulang (`-count=5`) — bukti `FOR UPDATE` beneran
  menyerialisasi, bukan sekadar cek-lalu-eksekusi yang rawan race
- Unit `condition=BAD` dijual ke pelanggan tanpa `confirmed` → 409; dengan `confirmed: true` → 201
- Unit `condition=BAD` dijual ke supplier (`SELL_SUPPLIER`) tanpa `confirmed` → tetap 201 (guard
  konfirmasi cuma berlaku buat `SELL`)
- `payment_method` invalid → 400; `items` kosong → 400
- Response tidak pernah mengandung field `cogs` di mana pun (cek raw JSON)
- `type=BUY` dari pelanggan → 201, bikin `stock_item` baru `status=AVAILABLE` dengan `barcode`
  format `{SKU}-0001`, `purchase_price` di DB sama dengan `price_total` yang diinput,
  `price_per_gram` di response == `price_total / weight_gram`, response item punya
  `stock_item_id`/`barcode` (dicek lewat `GET /api/stock-items/{stock_item_id}`), tidak ada `cogs`
  di mana pun
- `BUY` dua item produk yang sama dalam satu request → barcode urut `-0001`/`-0002`
- `BUY` menambah jumlah stok produk (dicek lewat `GET /api/products/{productId}/stock-items`
  sebelum/sesudah)
- `BUY` dua item `serial_number` sama dalam satu batch → 409, **tidak ada yang tersimpan**
  (atomik, dicek stok produk masih kosong setelahnya)
- `BUY` `serial_number` yang sudah dipakai unit lain (dibuat sebelumnya) → 409
- `BUY` `serial_number` kosong / `condition` invalid → 422; `price_total <= 0` → 400
- `BUY` tanpa `customer_id` → 400; `product_id` tidak ditemukan → 404; produk diarsipkan → 400
- `GET /api/customers/{id}/transactions` (BE-602): tanpa token → 401; `{id}` pelanggan tidak
  ditemukan → 404; menggabungkan `SELL` **dan** `BUY` milik pelanggan yang sama; transaksi milik
  pelanggan lain tidak ikut ke-list; urut **terbaru duluan** (dicek pakai urutan
  `transaction_code`, bukan timestamp mentah); `?limit=&page=` paginasi dengan benar
- `GET /api/transactions/{id}` (BE-602): detail lengkap dengan `items[]` (`stock_item_id`/`barcode`
  ikut kebawa) sama persis kayak response `POST`, berlaku buat `SELL` maupun `BUY`, tanpa `cogs`;
  tidak ditemukan → 404; format id bukan UUID → 400
- `payment_ref` round-trip: diisi saat create (mis. non-tunai) → ikut kebalikin persis sama di
  response create, `GET /api/transactions/{id}`, **dan** `GET /api/customers/{id}/transactions`
  (riwayat); tidak diisi (lazimnya `CASH`) → `""` di semua response, bukan `null` atau hilang

**`purchase_orders_test.go`** — `/api/purchase-orders` (BE-901/BE-902/BE-903/BE-904)
- Semua route tanpa token → 401; role KASIR → 403 di semua endpoint (beda dari
  `/api/products`/`/api/stock-items` yang GET-nya kebuka buat KASIR — di sini nggak sama sekali)
- `POST` create → 201, `po_code` format `PO-YYYYMMDD-0001`, `total_amount` terjumlah benar
  (`Σ quantity×purchase_price`), `status=BELUM_DITERIMA`; **belum ada** `stock_items` yang dibuat
  (dicek lewat `GET /api/products/{productId}/stock-items`, masih kosong)
- Dua PO di hari yang sama → `po_code` urut naik (`-0001`, `-0002`)
- `supplier_id`/`items` kosong → 400; `quantity`/`purchase_price` invalid → 400; supplier/produk
  tidak ditemukan → 404; produk diarsipkan → 400
- `GET` list: `?status=` filter dengan benar; `?limit=&page=` paginasi
- `GET` detail: `items[]` bawa nama & SKU produk (di-join), `supplier` bawa nama; tidak ditemukan
  → 404; format id bukan UUID → 400
- `POST /{id}/receive`: full receive (mencakup semua produk PO, jumlah serial pas dengan quantity)
  → 200, `received_units[]` kasih `stock_item_id`/`barcode` tiap unit baru (dicek lewat
  `GET /api/stock-items/{id}`: `status=AVAILABLE`, `purchase_price` sama dengan PO item,
  `po_id`/`supplier_id` terisi — dicek langsung ke `testPool` karena belum ada field itu di
  response stock-item manapun), PO jadi `status=DITERIMA` + `received_at` terisi
- `receive` dengan jumlah `serials` tidak sama dengan `quantity` PO → 400; request yang tidak
  mencakup semua produk PO → 400; `serial_number` kosong → 422; `condition` invalid → 422;
  `serial_number` sama antar item dalam satu batch → 409; PO yang sudah `DITERIMA` di-receive
  lagi → 409; `{id}` tidak ditemukan → 404
- `POST /{id}/cancel`: PO `BELUM_DITERIMA` → 200, `status=DIBATALKAN`; PO yang sudah `DITERIMA`
  atau sudah `DIBATALKAN` → 409; tidak ditemukan → 404

**`receipts_test.go`** — `GET /api/transactions/{id}/receipt` (BE-1001)
- Tanpa token → 401
- Struk transaksi `SELL`: `customer` terisi (nama/telepon/alamat dari fixture pelanggan),
  `supplier` `null`, `store` sesuai settings yang di-seed, `items[]` sama dengan yang dibuat saat
  checkout, `invoice_url == "/api/transactions/{id}/receipt"`
- Struk transaksi `BUY`: `customer` terisi, `supplier` `null`
- Struk transaksi `SELL_SUPPLIER`: `supplier` terisi (nama/telepon/alamat dari fixture supplier),
  `customer` `null`
- `invoice_url` di-cache: kolom `transactions.invoice_url` `NULL` sebelum panggilan pertama,
  terisi setelahnya (dicek langsung ke `testPool`); panggilan kedua mengembalikan `invoice_url`
  yang identik dengan panggilan pertama
- Struk tanpa `settings` di-seed sama sekali → tetap 200, field `store` kosong (`""`), tidak error
- `{id}` tidak ditemukan → 404; format id bukan UUID → 400

**`stock_opnames_test.go`** — `/api/stock-opnames` (BE-1101/BE-1102/BE-1103)
- Semua endpoint tanpa token → 401; role KASIR → 403 (sama seperti `/api/purchase-orders`, tidak
  ada akses KASIR sama sekali)
- `POST` create → 201, `opname_code` format `OPN-YYYYMMDD-0001`, `status=IN_PROGRESS`, `notes`
  tersimpan, `items[]` masih kosong; dua sesi di hari yang sama → `opname_code` urut naik
- `POST /{id}/scan` unit `AVAILABLE` yang dikenal → `result=MATCH`, `physical_status=FOUND`; unit
  yang statusnya `SOLD` (di-set langsung lewat SQL fixture) → `result=UNEXPECTED`,
  `system_status=SOLD`; barcode yang sama sekali tidak dikenal → 404; `barcode` kosong → 400; unit
  yang sama discan dua kali dalam satu sesi → 409 (scan pertama tidak terganggu); sesi `{id}` tidak
  ditemukan → 404; scan terhadap sesi yang sudah `COMPLETED` → 409
- `POST /{id}/complete`: sesi dengan 2 unit `AVAILABLE` (1 discan, 1 tidak) → 200,
  `summary={match:1,missing:1,unexpected:0}`, `status=COMPLETED`, `items[]` berisi kedua unit
  (termasuk yang otomatis jadi `MISSING`); `GET /{id}` setelahnya menunjukkan hasil yang sama;
  complete sesi yang sudah `COMPLETED` → 409; sesi tidak ditemukan → 404
- `GET /{id}` tidak ditemukan → 404; format id bukan UUID → 400
- Alur penuh (round trip lintas ketiga ticket): create → scan 2 dari 3 unit `AVAILABLE` →
  complete → `summary={match:2,missing:1,unexpected:0}`

**`expense_categories_test.go`** — `/api/expense-categories` (BE-1201)
- Semua endpoint tanpa token → 401; role KASIR → 403
- Create → 201; List/Get/Update round trip; `name` kosong → 400
- Duplikat `name` (persis sama) → 409 saat create maupun update
- Delete kategori yang tidak dipakai → 200, **beneran hilang** (`GET` setelahnya → 404, hard
  delete — bukan `is_active=false` seperti `categories`/`brands`); delete kategori yang masih
  dipakai satu `expense` → 409, kategori tetap ada setelahnya (delete tidak boleh separuh jalan)
- Tidak ditemukan → 404 (get/update/delete); format id bukan UUID → 400

**`expenses_test.go`** — `/api/expenses` (BE-1202)
- Semua endpoint tanpa token → 401; role KASIR → 403
- Create/Get/Update/Delete round trip (delete juga hard delete, `GET` setelahnya → 404)
- `category_id`/`amount`/`expense_date` kosong → 400 masing-masing; `amount <= 0` → 400;
  `expense_date` format salah → 400; `category_id` tidak ditemukan → 404
- `GET` list `?category_id=` cuma mengembalikan pengeluaran kategori itu; `?date_from=&date_to=`
  memfilter rentang tanggal dengan benar (termasuk batas inklusif di kedua ujung, dan rentang
  open-ended kalau cuma salah satu diisi); `?page=&limit=` paginasi; `?category_id=` yang tidak
  ditemukan → 404

**`reports_test.go`** — `/api/reports` (BE-1301/BE-1302/BE-1303, plus `/dashboard`)
- Semua endpoint tanpa token → 401; role KASIR → 403
- **Transactions**: satu transaksi `SELL`, `BUY`, dan `SELL_SUPPLIER` → `breakdown[]` berisi
  ketiganya dengan `transaction_count`/`total_amount`/`total_weight` yang benar, `total` adalah
  jumlah ketiganya; `?type=SELL` → `breakdown[]` cuma berisi satu baris itu; `?type=INVALID` →
  400; `?from=&to=` cuma menghitung transaksi di rentang itu (transaksi lain di-backdate langsung
  lewat SQL, tidak ada endpoint buat itu); format tanggal salah → 400
- **Stock**: produk dengan 2 unit GOOD + 1 unit BAD (semua `AVAILABLE`) + 1 unit `SOLD` →
  `available_count=3, good_count=2, bad_count=1` (unit `SOLD` tidak dihitung sama sekali); produk
  aktif tanpa stok sama sekali tetap muncul dengan semua hitungan nol dan `low_stock=true`; produk
  yang diarsipkan tidak muncul di laporan sama sekali; `?threshold=` mengganti default (`5`) dan
  mengubah `low_stock` sesuai
- **Finance**: transaksi `SELL` dan `SELL_SUPPLIER` dengan `price_total`/`purchase_price` (cogs)
  yang diketahui → `sales_breakdown[]` punya baris terpisah buat tiap tipe dengan
  `total_revenue`/`total_cogs`/`gross_profit` yang benar, `total_revenue`/`total_cogs`/
  `gross_profit` tingkat atas adalah jumlah keduanya, `gross_margin_percent` sesuai rumus; transaksi
  `BUY` di periode yang sama **tidak** muncul di `sales_breakdown[]` sama sekali dan tidak
  memengaruhi angka apa pun (`cogs`-nya selalu `NULL`); dua `expense` di kategori yang sama →
  `expense_breakdown[]` menjumlahkannya jadi satu baris, kategori berbeda → baris terpisah;
  `total_expenses` cocok, `net_profit == gross_profit - total_expenses`; `gross_margin_percent`
  tetap `0` (bukan `NaN`/`Inf`, yang bikin JSON decode gagal) kalau belum ada penjualan sama
  sekali di periode itu; `?from=&to=` memfilter baik transaksi maupun expense dengan benar;
  format tanggal salah → 400
- **Dashboard**: tanpa `?from=&to=` → default ke bulan berjalan dalam **UTC** (transaksi bulan
  lalu, di-backdate lewat SQL, tidak ikut kehitung; transaksi bulan ini ikut); `?from=&to=`
  override bekerja sama seperti laporan lain; `finance`/`transaction_breakdown`/
  `transaction_total` di response dashboard **sama persis** dengan hasil manggil
  `/api/reports/finance` dan `/api/reports/transactions` buat rentang yang sama (bukti dashboard
  benar-benar reuse service yang sama, bukan implementasi kedua yang bisa drift); `low_stock_items[]`
  cuma berisi produk yang `low_stock=true` (produk yang stoknya banyak tetap muncul di
  `/api/reports/stock` tapi tidak di sini); `?threshold=` override berlaku sama; `pending_purchase_orders[]`
  cuma PO `BELUM_DITERIMA` (PO yang sudah dibatalkan tidak ikut), terbaru duluan, dibatasi
  `?pending_limit=` (default 5) sementara `pending_purchase_orders_total` tetap jumlah sebenarnya
  walau melebihi batas tampil

Konvensi menambah endpoint baru: tambah skenario di file `test/e2e/{resource}_test.go` baru
(ikuti pola `health_test.go` / `auth_test.go` / `users_test.go`) supaya suite ini tetap jadi peta
lengkap API surface, bukan cuma cover apa yang ada waktu ditulis (lihat komentar package di
`test/e2e/main_test.go`).

## Konfigurasi (env)

Lihat `.env.example` untuk daftar lengkap. Semua diambil dari environment
variable; file `.env` otomatis dibaca saat development (lihat `internal/config`).

`CORS_ALLOWED_ORIGINS` — daftar origin yang boleh akses API dari browser, dipisah koma
(default `*` kalau tidak diisi). Middleware-nya (`internal/middleware/cors.go`, pakai
`github.com/go-chi/cors`) sengaja tidak mengaktifkan `AllowCredentials` — auth di API ini
pakai Bearer token di header `Authorization`, bukan cookie, jadi wildcard origin aman dipakai
di local/dev. Untuk staging/production, isi dengan domain frontend asli (mis.
`https://app.goldtrack.id`) supaya lebih ketat.

`GOLD_PRICE_SYNC_INTERVAL` — interval sinkronisasi harga emas rujukan (BE-404, lihat
`### /api/gold-prices/active` di atas), format `time.ParseDuration` Go (mis. `1h`, `30m`).
Default `1h` kalau tidak diisi atau tidak valid. Goroutine sync dijalankan dari `cmd/api/main.go`,
sekali langsung saat startup lalu berulang tiap interval ini.

## Menambah fitur baru

1. `internal/repository/xxx_repository.go` — interface + implementasi query.
2. `internal/service/xxx_service.go` — logic, panggil repository.
3. `internal/handler/xxx_handler.go` — terima request, panggil service, balas via `pkg/response`.
4. Daftarkan route di `internal/handler/router.go`.
