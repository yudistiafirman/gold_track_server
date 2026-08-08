# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

Go backend for a gold/jewelry shop POS system (product catalog, physical stock units,
sales/buyback transactions, purchase orders, stock opname, expenses, reports). 3-layer
architecture: **Handler → Service → Repository**, wired in `internal/app/app.go`.

## Commands

```bash
cp .env.example .env
docker compose up -d              # start local PostgreSQL
go run ./cmd/api                  # run the API (localhost:8080)
curl http://localhost:8080/health

go run ./cmd/migrate up           # apply all pending migrations
go run ./cmd/migrate down 1       # rollback one step
go run ./cmd/migrate version      # current migration version
go run ./cmd/migrate force 5      # force-set version (recover from "dirty" state)

go run ./cmd/seed                 # seed admin user + default categories/brands/settings (idempotent)

go test ./internal/...            # unit tests (fast, no external deps)
go test ./test/e2e/...            # e2e tests — needs `docker compose up -d postgres`
go test ./test/e2e/... -v
go test ./test/e2e/... -run TestUsers_CreateListGetUpdateDelete -v   # single test
```

E2E tests target a `gold_track_test` DB (auto-created and auto-migrated by `test/e2e/main_test.go`
`TestMain`), separate from the dev DB (`gold_track`). Override with env `DB_NAME`. Each top-level
e2e test starts with `resetDB(t)` (`TRUNCATE ... RESTART IDENTITY CASCADE`) so tests don't depend
on run order.

## Architecture

`internal/app.New(ctx, cfg, log)` wires every repository → service → handler and builds the chi
router. Both `cmd/api` and the e2e test harness (`test/e2e/main_test.go`) call this same
constructor, so anything wired there is automatically live in both real runs and tests — a new
resource added to one is never missing from the other.

Adding a new resource/endpoint follows this order:
1. `internal/repository/xxx_repository.go` — interface + query implementation.
2. `internal/service/xxx_service.go` — business logic, calls repository.
3. `internal/handler/xxx_handler.go` — parses request, calls service, replies via `pkg/response`.
4. Register the route in `internal/handler/router.go`.
5. Add e2e scenarios in `test/e2e/xxx_test.go` — this suite is meant to stay a complete map of the
   API surface, not just cover what existed when it was written.

Errors flow up through `*apperror.AppError` (`pkg/apperror`) — a single type carrying HTTP status +
safe-to-expose message, constructed with helpers like `apperror.NotFound(...)`,
`apperror.Conflict(...)`, `apperror.UnprocessableEntity(...)`. Handlers pass any error straight to
`response.Error(w, err)`, which unwraps it via `apperror.As` (defaulting to 500 `INTERNAL_ERROR` if
it isn't an `*AppError`). All JSON responses use the same envelope (`pkg/response`):
`{"success": bool, "data": ...}` or `{"success": false, "error": {"code", "message"}}`.

### Auth & routing (`internal/handler/router.go`, `internal/middleware/auth.go`)

Every `/api` route except `POST /api/auth/login` sits behind `appmw.JWTAuth(authService)`
(verifies Bearer token signature/expiry/blacklist, stores claims in context). Role-restricted
routes additionally nest `appmw.RequireRole("ADMIN", "SUPER_ADMIN", ...)` inside a chi sub-group.
Roles: `SUPER_ADMIN`, `ADMIN`, `KASIR`. Logout works via a `token_blacklist` table keyed on JWT
`jti` (JWT is stateless, so real invalidation needs server-side bookkeeping) — blacklisted `jti`s
are rejected 401 even before expiry.

Most resources are admin-only (`ADMIN`/`SUPER_ADMIN`) for writes; a few (`products` GET,
`customers` POST/GET, `transactions`, `stock-items` lookup/get) are open to all authenticated roles
including `KASIR` because cashiers need them at checkout. `purchase-orders` and `stock-opnames` are
strictly `ADMIN`/`SUPER_ADMIN` — no `KASIR` access at all (back-office only). `reports` (including
`dashboard`) is stricter still — `SUPER_ADMIN` only, `ADMIN` gets 403 — business figures (revenue,
COGS, margin) are hidden from plain `ADMIN`, same tier as `users`.

### ID strategy: internal BIGINT + external public_id (UUID)

Every resource table has two identifiers:
- `id BIGSERIAL` — internal PK for FK/joins, **never** exposed via API/URL/JSON/JWT.
- `public_id UUID` (unique, `gen_random_uuid()` default) — the only identifier clients see: URL
  path params, JSON `"id"` field, JWT `user_id` claim. Prevents resource enumeration.

Repository queries always `SELECT ... public_id::text ...` (explicit cast for safe scan into Go
`string`); only `public_id` gets mapped into response DTOs — `model.X` structs (with internal
`ID int64`) are never returned directly to handlers.

### Soft delete vs hard delete

Every resource is soft-deleted. Most use `is_active=false`; `stock_items` instead uses a fourth
`status` value, `ARCHIVED` (`DELETE /api/stock-items/{id}` maps to an `UPDATE ... SET
status='ARCHIVED' WHERE public_id = $1 AND status IN ('AVAILABLE', 'SOLD')`, guarded at the SQL
level — a follow-up check distinguishes 404 "not found" from 409 "exists but VOID or already
ARCHIVED"). This used to be a real hard `DELETE` restricted to `AVAILABLE` units, but that broke as
soon as the unit was referenced by `transaction_items` (e.g. a BUY-created unit not yet resold) or
`stock_opname_items` (scanned in a session) — both are `NOT NULL` FKs with no cascade, so the
delete hit a foreign-key violation. Archiving sidesteps that entirely since the row never
disappears — which also made it safe to extend archiving to `SOLD` units too (the row, and every
FK pointing at it, survives regardless); `sold_at` is left untouched by the archive so the sale
timestamp isn't lost. `ListByProduct` hides `ARCHIVED` **and** `VOID` units from the default (no
`?status=`) listing — both are "dead" statuses no longer real stock — same as every other resource
hides `is_active=false` rows, but an explicit `?status=ARCHIVED`/`?status=VOID` still returns them.
`VOID` (set when a BUY transaction is cancelled, see Transactions below) is never allowed to
transition into `ARCHIVED` — it stays a distinct terminal status so *why* the unit is dead
(cancelled buyback vs. deliberately archived) isn't lost. Sold units must stay in the DB
permanently for historical transaction integrity — same for archived/void ones. **Cancel always
wins over archive**: `Cancel` (`transaction_repository.go`) unconditionally overwrites a referenced
stock item's status back to `AVAILABLE` (`SELL`/`SELL_SUPPLIER`) or `VOID` (`BUY`, unless it's
`SOLD` — actually resold, which still blocks the cancel) regardless of whether it's currently
`ARCHIVED` — cancelling the transaction that made a unit dead stock is exactly what should bring it
back, so an in-between archive doesn't get special treatment; it's simply overwritten. The
consequence is intentional: there's no restore/unarchive endpoint for `stock_items` (unlike
`products`, which reactivates via `PUT` with `is_active: true`), so a *permanent* archive of a
`SOLD` unit requires cancelling its sale first, then archiving — archiving before cancelling is not
durable.

### Auto-generated codes (same pattern repeated across resources)

SKU (`products`), barcode (`stock_items`), `transaction_code`, `po_code`, `opname_code` are all
generated the same way: computed + inserted in **one DB transaction**, with retry (max 5x) on a
unique-constraint race from concurrent creates — never just computed once and trusted.
- SKU: `[KAT]-[BRAND]-[BERAT]-[URUT]` (3-letter category/brand prefix, weight with trailing zeros
  stripped, 3-digit zero-padded sequence per exact prefix match). Immutable after create — `PUT
  /api/products/{id}` never updates `sku`.
- Barcode: `{SKU}-{4-digit sequence}`. Immutable — `PUT /api/stock-items/{id}` locks `barcode` and
  `product_id`.
- `transaction_code` / `po_code` / `opname_code`: `{PREFIX}-YYYYMMDD-{4-digit sequence per day}`.

### Validation status-code tiers

- `400` for request-shape/business-rule errors on resources that don't yet create a physical unit
  (e.g. missing `name`, invalid `category_id`, archived product referenced).
  `500` for a real backend server error.
- `422 UNPROCESSABLE_ENTITY` specifically for stock-unit-creating fields (`serial_number`,
  `condition`, `purchase_price`, `purchase_date` on stock-item create, transaction `BUY` items, PO
  `receive` serials) — anything that actually mints a new `stock_items` row.
- `409 CONFLICT` for state conflicts (duplicate unique key, wrong status transition, race lost).

### Transactions (`/api/transactions`) — the most complex write path

Three `type`s: `SELL` (to customer), `SELL_SUPPLIER` (to supplier), `BUY` (buyback from customer,
creates new `stock_items`). Runs fully inside one DB transaction; `SELL`/`SELL_SUPPLIER` row-lock
referenced `stock_items` (`SELECT ... FOR UPDATE`) so concurrent double-sells reliably produce one
201 and one 409. `BUY` is all-or-nothing across every item. `condition=BAD` units require
`items[].confirmed=true` for `SELL` (re-validated server-side, never trusted from the earlier
lookup call) — not required for `SELL_SUPPLIER`. `cogs` is never present in transaction responses
(margin data, hidden from cashier-facing checkout).

### Purchase orders / stock opname — two-phase flows with row locks

`purchase_orders`: `POST` creates `BELUM_DITERIMA` (no stock yet, money committed) →
`POST /{id}/receive` (single shot, must cover every PO line item exactly, per-serial `condition`)
creates the actual `stock_items` and flips to `DITERIMA` → or `POST /{id}/cancel` to
`DIBATALKAN`. PO row is locked `FOR UPDATE` during receive/cancel to prevent double-processing.

`stock_opnames`: `POST` opens `IN_PROGRESS` session → repeated `POST /{id}/scan {barcode}` records
MATCH/UNEXPECTED per unit (unknown barcode → 404, already-scanned-this-session → 409) →
`POST /{id}/complete` bulk-marks every un-scanned `AVAILABLE` unit as MISSING (one atomic `INSERT
... SELECT ... WHERE NOT EXISTS`, not a loop) and closes the session. Same `FOR UPDATE` locking
pattern as PO receive.

### Gold price sync (`/api/gold-prices/active`) — the one non-request-driven feature

A ticker goroutine (`runGoldPriceSync` in `cmd/api/main.go`, **not** in `internal/app/app.go`)
fetches from a third-party API every `GOLD_PRICE_SYNC_INTERVAL` (default `1h`), once at startup
then repeating. It's deliberately kept out of `app.New` because the e2e harness also calls
`app.New`, and putting it there would fire real external HTTP calls on every test run —
`GoldPriceService` is exposed on the `App` struct so only `main.go` drives the goroutine. The
endpoint only ever reads the last successfully-synced row from DB; a fetch failure/timeout leaves
the existing active row untouched and just logs, so this endpoint never 5xxs because of the
external API being down. No sync yet → `200` with `data: null` (not 404/500).

### Reports (`/api/reports/*`)

Read-only, no pagination. `dashboard` composes the other three report queries internally (not
separate calls) so numbers stay consistent with the standalone endpoints. Date filters
(`?from=&to=`) compare against `created_at::date` in UTC — dashboard defaults to "this month" in
UTC specifically to avoid losing "today"'s transactions to local/server timezone drift.
`SUPER_ADMIN`-only (client requirement — plain `ADMIN` doesn't get business-figures visibility),
registered in its own `RequireRole("SUPER_ADMIN")` group in `router.go`, separate from the shared
`ADMIN`/`SUPER_ADMIN` group the rest of back-office (`categories`, `products`, `purchase-orders`,
`expenses`, etc.) sits in.

## Conventions

- Every list endpoint follows the same pagination shape: `?page=` (default 1), `?limit=` (default
  20, capped 100), response `{"items": [...], "pagination": {"page","limit","total","total_pages"}}`.
- `PUT` endpoints are full-replace (client resends every editable field), not partial patch.
- Nested reference objects in responses (e.g. `category: {id, name}` on a product) avoid exposing
  raw FK UUIDs the client would have to look up separately.
- User-facing error messages are in Indonesian; keep new ones consistent with existing phrasing in
  `internal/service/*.go`.
- `README.md` documents the full request/response shape and error cases for every endpoint in
  detail — check it before guessing a response contract.
