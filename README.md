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
`barcode`/`public_id` yang unik di tabel itu.

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
walau requester-nya SUPER_ADMIN.

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
GET    /api/suppliers        # ?search=&page=&limit= -> 200
POST   /api/suppliers        # { name, phone?, address?, notes? }          -> 201
GET    /api/suppliers/{id}   # detail supplier                              -> 200 / 404
PUT    /api/suppliers/{id}   # { name, phone?, address?, notes?, is_active } -> 200 / 404
DELETE /api/suppliers/{id}   # soft delete (is_active=false)                -> 200 / 404
```

`{id}` adalah `public_id` (UUID). Cuma `name` yang wajib — `phone`/`address`/`notes` opsional,
dikosongkan (`""`) di request berarti `NULL` di DB. `PUT` full-replace seperti resource lain
(kirim semua field yang bisa diedit, bukan partial patch).

Beda dari `/api/products` (BE-202): `GET /api/suppliers` **tidak** memfilter `is_active` — supplier
yang sudah diarsipkan tetap muncul di list (field `is_active` di response tinggal dicek klien),
karena admin mungkin masih perlu cari supplier lama buat referensi PO/transaksi historis. `?search`
cocok ke `name` (`ILIKE`, case-insensitive substring); `page`/`limit` sama seperti `/api/products`
(default `1`/`20`, `limit` di-cap `100`).

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

Contoh response error:
```json
// 403 — role selain ADMIN/SUPER_ADMIN
{"success":false,"error":{"code":"FORBIDDEN","message":"Anda tidak memiliki akses untuk aksi ini"}}

// 400 — name kosong
{"success":false,"error":{"code":"BAD_REQUEST","message":"name wajib diisi"}}

// 404 — public_id valid tapi tidak ada
{"success":false,"error":{"code":"NOT_FOUND","message":"supplier tidak ditemukan"}}

// 400 — {id} bukan format UUID
{"success":false,"error":{"code":"BAD_REQUEST","message":"id tidak valid"}}
```

### /api/customers — CRUD pelanggan (role split: create/read vs edit/delete)

```bash
GET    /api/customers                 # ?search=&page=&limit=                              -> 200 (ADMIN, KASIR, SUPER_ADMIN)
POST   /api/customers                 # { name, phone?, email?, id_type?, id_number?, address?, notes? } -> 201 (ADMIN, KASIR, SUPER_ADMIN)
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

Cuma `name` yang wajib. `id_type`, kalau diisi, harus `KTP`/`SIM`/`PASSPORT` (400 kalau bukan).
`?search` cocok ke `name` **atau** `phone` (satu query param, match salah satu). Sama seperti
`/api/suppliers`: list **tidak** memfilter `is_active` — pelanggan yang diarsipkan tetap muncul
di list (berguna buat cari histori transaksi pelanggan lama).

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
- **Validasi create pakai status `422`** (`Unprocessable Entity`), bukan `400` seperti endpoint
  lain — `serial_number`, `condition` (harus `GOOD`/`BAD`), `purchase_price` (harus `> 0`), dan
  `purchase_date` (format `YYYY-MM-DD`) semua wajib diisi & valid, kalau tidak → 422.
- **Barcode auto-generate**, format `{SKU}-{urut 4 digit}` (mis. SKU produk `BAT-ANT-10-001` →
  barcode unit pertama `BAT-ANT-10-001-0001`, unit kedua `...-0002`, dst.) — dihitung + insert
  dalam satu transaksi DB dengan retry (maks 5x) kalau kena unique constraint
  `uq_stock_items_barcode`, persis pola SKU produk (BE-201) tapi 4 digit, bukan 3.
- **Create ditolak (400) kalau produknya sudah diarsipkan** (`is_active=false`) — tidak bisa
  nambah stok ke produk yang sudah tidak dijual.
- **`PUT` mengunci `barcode` dan `product_id`** — kolom itu sengaja tidak ada di query `UPDATE`,
  persis pola SKU produk yang immutable (BE-203). Field yang bisa diedit: `serial_number`,
  `condition`, `purchase_price`, `purchase_date`, `notes`. Unit yang `SOLD` ditolak (409) —
  guard-nya fetch-lalu-cek di service (bukan level SQL seperti `DELETE`, karena edit bukan
  operasi destruktif tanpa-undo).
- **`GET /api/stock-items/{id}/label`** berlaku untuk unit `AVAILABLE` **maupun** `SOLD` — cetak
  ulang label diperbolehkan, tidak difilter status.
- `supplier_id`/`po_id` di luar scope saat ini — selalu `NULL` (belum ada resource purchase
  order, dan `supplier_id` belum diterima lewat body ini).

#### GET /api/stock-items/lookup — cari unit via barcode (BE-701/BE-703)

Dipakai kasir buat scan barcode fisik pas nambahin item ke keranjang jual. `?barcode=` wajib.
`?type=` opsional (`SELL` atau `SELL_SUPPLIER`) — kalau `type=SELL` dan unit `condition=BAD`,
response punya `"requires_confirmation": true` supaya klien nampilin konfirmasi **sebelum** unit
itu masuk keranjang. `type=SELL_SUPPLIER` atau `?type` dikosongkan → `requires_confirmation`
selalu `false` (jual ke supplier tidak butuh konfirmasi kondisi). Barcode tidak ditemukan → 404;
unit yang sudah `SOLD` → 409. Field `condition` selalu ada di response biar klien juga bisa decide
sendiri kalau perlu.

```bash
GET /api/stock-items/lookup?barcode=BAT-ANT-10-001-0001&type=SELL
```

Contoh response (200):
```json
{
  "success": true,
  "data": {
    "id": "1a2b3c4d-5e6f-7890-abcd-ef1234567890",
    "product": { "id": "6d9f6a2a-2b0c-4e2b-8f1a-1a2b3c4d5e6f", "name": "Emas Batangan 10gr" },
    "barcode": "BAT-ANT-10-001-0001",
    "serial_number": "SN-0001",
    "condition": "BAD",
    "purchase_price": 1000000,
    "purchase_date": "2026-07-01",
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
    "product": { "id": "6d9f6a2a-2b0c-4e2b-8f1a-1a2b3c4d5e6f", "name": "Emas Batangan 10gr" },
    "barcode": "BAT-ANT-10-001-0001",
    "serial_number": "SN-0001",
    "condition": "GOOD",
    "purchase_price": 1000000,
    "purchase_date": "2026-07-01",
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
    "barcode": "BAT-ANT-10-001-0001",
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

// 400 — create ke produk yang sudah diarsipkan
{"success":false,"error":{"code":"BAD_REQUEST","message":"produk sudah diarsipkan, tidak bisa menambah stok"}}

// 409 — update/delete unit yang sudah SOLD
{"success":false,"error":{"code":"CONFLICT","message":"unit sudah terjual (SOLD), tidak bisa diedit"}}
{"success":false,"error":{"code":"CONFLICT","message":"unit sudah terjual (SOLD), tidak bisa dihapus"}}

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
    { "product_id": "<public_id produk>", "serial_number": "SN-BUYBACK-01", "condition": "GOOD", "price_total": 900000 }
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
- Tiap item bikin baris `stock_items` baru: `status=AVAILABLE`, `barcode` auto-generate (format
  `{SKU}-{urut 4 digit}` sama seperti BE-501, dihitung ulang per item — kalau 2 item di satu
  request sama-sama merujuk produk yang sama, barcode-nya tetap urut `-0001`/`-0002`),
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
- `cogs` transaction_item-nya `NULL` — belum diketahui sampai unit ini nanti terjual lagi.
- Produk harus ada (404) dan aktif (`is_active=true`, sama seperti guard BE-501 — 400 kalau
  produknya sudah diarsipkan).

`transaction_code` format `TRX-YYYYMMDD-XXXX` (urut 4 digit per hari, mis. `TRX-20260731-0001`,
`...-0002`), dihitung + insert dalam transaksi yang sama dengan retry (maks 5x) kalau kena race
di constraint unique-nya — pola yang sama dengan `sku` produk dan `barcode` unit stok.

Setiap unit yang berhasil terjual (`SELL`/`SELL_SUPPLIER`) otomatis jadi `status=SOLD` +
`sold_at` terisi (tidak bisa di-`PUT`/`DELETE` lagi setelahnya — lihat guard di section
stock-items di atas).

Response tiap item menyertakan `stock_item_id` (public_id) dan `barcode` unit yang
bersangkutan — buat `SELL`/`SELL_SUPPLIER` itu unit yang sudah ada, buat `BUY` itu unit yang
baru dibuat (langsung kepakai buat cetak label via `GET /api/stock-items/{id}/label`). Response
**tidak pernah menyertakan `cogs`** (harga pokok/beli unit) — itu data margin toko, sengaja
tidak diekspos ke response checkout yang kemungkinan dilihat kasir.

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
    "payment_method": "CASH",
    "status": "COMPLETED",
    "items": [
      {
        "id": "3c4d5e6f-7081-9012-cdef-012345678901",
        "stock_item_id": "1a2b3c4d-5e6f-7890-abcd-ef1234567890",
        "barcode": "BAT-ANT-10-001-0001",
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
    "status": "COMPLETED",
    "items": [
      {
        "id": "5e6f7081-9203-4415-ef01-234567890123",
        "stock_item_id": "6f708192-0334-5526-f012-345678901234",
        "barcode": "BAT-ANT-10-001-0002",
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
{"success":false,"error":{"code":"BAD_REQUEST","message":"payment_method harus CASH, TRANSFER, atau QRIS"}}

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
  "items": [{ "id": "...", "stock_item_id": "...", "barcode": "...", "product_name": "...",
              "weight_gram": 10, "price_per_gram": 150000, "price_total": 1500000 }],
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
POST /api/purchase-orders/{id}/receive  # { items[]:{product_id,serials[],condition} }                         -> 200 / 404 / 409
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
lebih sama-sama ditolak 400, bukan cuma kasus kurang). Untuk tiap serial: satu `stock_items` baru
(`status=AVAILABLE`, `barcode` auto-generate pola `{SKU}-{urut 4 digit}` sama seperti
BE-501/BE-801, `purchase_price` diambil dari PO — **bukan** dari request receive,
`po_id`/`supplier_id` ikut terisi di unit itu — pertama kalinya kedua kolom ini kepakai, sebelumnya
selalu `NULL` baik dari create-stock-item langsung (BE-501) maupun buyback (BE-801)). Semuanya
atomik dalam satu transaksi DB, PO row di-lock (`FOR UPDATE`) selama itu supaya PO yang sama tidak
bisa diterima dua kali secara konkuren; setelah semua unit masuk, PO jadi `status=DITERIMA` +
`received_at` terisi. PO yang sudah `DITERIMA`/`DIBATALKAN` tidak bisa di-receive lagi → 409.

`POST /{id}/cancel` cuma bisa dari `status=BELUM_DITERIMA` → `DIBATALKAN` (guard di level SQL,
pola yang sama dengan hard-delete `stock_items` di BE-504); PO yang sudah `DITERIMA` atau
`DIBATALKAN` → 409.

**Validasi**: field item `POST` (`product_id`/`quantity`/`purchase_price`) pakai tier `400` (di
titik ini belum ada `stock_items` yang dibuat, jadi bukan tier validasi "unit fisik"). Field item
`receive` (`serial_number`/`condition`) pakai tier `422`, sama seperti BE-501/BE-801 — langkah ini
memang bikin `stock_items` baru. `serial_number` duplikat (lawan unit yang sudah ada, atau sesama
item dalam satu batch receive) → `409`, pakai constraint & sentinel yang sama dengan BE-801
(`uq_stock_items_serial_number`). Produk harus aktif buat `POST` maupun `receive` (400 kalau
diarsipkan, pesan sama dengan BE-501); tidak ada pengecekan aktif buat supplier (konsisten dengan
`SELL_SUPPLIER` di BE-702 yang juga tidak mengeceknya).

Response `GET /` (list) cuma header (tanpa `items[]`); `GET /{id}` dan `POST` sertakan `items[]`
(dengan nama & SKU produk, di-join langsung — `purchase_order_items` tidak punya kolom snapshot
nama produk seperti `transaction_items`, jadi live join memang satu-satunya cara). Response
`receive` juga sertakan `received_units[]` (`stock_item_id`/`barcode`/`product_name`/`serial_number`
per unit baru) — biar admin yang baru nerima PO langsung punya barcode buat cetak label, sama
alasannya dengan `stock_item_id`/`barcode` di tiap item transaksi (BE-702/BE-801).

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

Contoh response `POST /api/purchase-orders/{id}/receive` (200) — `status`/`received_at` berubah,
`received_units[]` muncul:
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
        "barcode": "BAT-ANT-10-001-0001",
        "product_name": "Emas Batangan 10gr",
        "serial_number": "PO-SN-1"
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

// 404 — {id}/supplier_id/product_id tidak ditemukan
{"success":false,"error":{"code":"NOT_FOUND","message":"purchase order tidak ditemukan"}}

// 409 — receive PO yang sudah DITERIMA/DIBATALKAN, atau cancel PO yang sudah DITERIMA/DIBATALKAN
{"success":false,"error":{"code":"CONFLICT","message":"PO sudah diterima atau dibatalkan, tidak bisa diterima lagi"}}
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

**`stock_items_test.go`** — `/api/products/{productId}/stock-items` & `/api/stock-items`
(BE-501 create, BE-502 list/detail, BE-503 update, BE-504 hard delete, BE-505 label)
- `POST` (ADMIN & SUPER_ADMIN): tanpa token → 401; role KASIR → 403
- Create → `barcode == "{SKU}-0001"`, `status == "AVAILABLE"`; dua unit di produk yang sama →
  barcode kedua `...-0002` (urut naik, sama pola dengan SKU produk)
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
