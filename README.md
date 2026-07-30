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
internal/middleware/     logging & error-handling (recover) global
internal/repository/     akses data (query ke PostgreSQL)
internal/service/        business logic
internal/handler/        HTTP handler + router (chi)
pkg/apperror/            tipe error terpusat (status code + pesan aman)
pkg/response/            format response JSON konsisten
```

Alur tiap fitur mengikuti: `handler` menerima request → panggil `service` →
`service` panggil `repository` → hasil/­error mengalir balik lewat `pkg/response`
dan `pkg/apperror`.

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

## Konfigurasi (env)

Lihat `.env.example` untuk daftar lengkap. Semua diambil dari environment
variable; file `.env` otomatis dibaca saat development (lihat `internal/config`).

## Menambah fitur baru

1. `internal/repository/xxx_repository.go` — interface + implementasi query.
2. `internal/service/xxx_service.go` — logic, panggil repository.
3. `internal/handler/xxx_handler.go` — terima request, panggil service, balas via `pkg/response`.
4. Daftarkan route di `internal/handler/router.go`.
