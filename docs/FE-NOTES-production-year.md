# Catatan FE — Field Baru `production_year`

Backend baru nambah field `production_year` (tahun produksi/cetak unit fisik emas) di
`stock_items`. **Field ini opsional** — nullable, boleh dikosongkan, tidak wajib diisi di form
manapun. Dokumen ini isinya titik-titik yang perlu diubah di FE.

Referensi lengkap request/response ada di `README.md` (section `/api/products/{productId}/stock-items
& /api/stock-items`, `/api/transactions`, `/api/purchase-orders`) — dokumen ini cuma ringkasan
dampaknya dari sisi FE.

---

## 1. Tipe & validasi

- Nama field: **`production_year`** (snake_case, sama semua endpoint).
- Tipe: **integer atau `null`** — bukan string. Kirim `2024`, bukan `"2024"`.
- Kalau diisi, harus **2000 s.d. tahun berjalan + 1**. Di luar itu → `422 UNPROCESSABLE_ENTITY`
  dengan `message: "production_year tidak valid"`.
- Saran UI: pakai input number dengan `min=2000`, `max=<tahun sekarang>+1`, placeholder
  "opsional" — biar user nggak trial-error ke server buat tahu batasnya.

---

## 2. Form yang perlu ditambah input baru (3 titik)

Field ini muncul di **3 form berbeda**, semuanya opsional:

### a. Tambah stok manual — `POST /api/products/{productId}/stock-items`
Form admin nambah unit stok satu-satu. Tambah 1 input baru di samping `serial_number`/`condition`/
`purchase_price`/`purchase_date` yang sudah ada.

```json
// request body, field baru di baris terakhir
{ "serial_number": "SN-0001", "condition": "GOOD", "purchase_price": 1000000, "purchase_date": "2026-07-01", "production_year": 2024 }
```

Field yang sama juga bisa diedit lewat **`PUT /api/stock-items/{id}`** (form edit unit stok) —
sama seperti `notes`/`condition`, ini field yang boleh diubah belakangan (bukan field terkunci
seperti `barcode`).

### b. Form buyback (BUY) — `POST /api/transactions` dengan `type: "BUY"`
Tiap `items[]` di form beli-emas-dari-pelanggan bisa dikasih 1 input opsional tambahan:

```json
{ "product_id": "...", "serial_number": "SN-BUYBACK-01", "condition": "GOOD", "price_total": 900000, "production_year": 2024 }
```

⚠️ **Catatan penting**: field ini **tidak muncul balik** di response transaksi (lihat section 4
di bawah) — kalau FE perlu nampilin/konfirmasi nilainya setelah submit, harus fetch
`GET /api/stock-items/{stock_item_id}` (id-nya ada di `items[].stock_item_id` pada response
transaksi).

### c. Form terima PO — `POST /api/purchase-orders/{id}/receive`
Field ini per **serial**, bukan per item PO (karena satu shipment bisa campur tahun produksi
dalam satu produk yang sama) — sama polanya dengan `condition` yang juga per-serial:

```json
{
  "items": [
    {
      "product_id": "...",
      "serials": [
        { "serial_number": "PO-SN-1", "condition": "GOOD", "production_year": 2024 },
        { "serial_number": "PO-SN-2", "condition": "BAD" }
      ]
    }
  ]
}
```

Beda dari form BUY di atas, di sini nilainya **langsung muncul balik** di response
(`received_units[].production_year`) — lihat section 3.

---

## 3. Response yang sekarang punya field `production_year`

Semua endpoint di bawah ini sekarang balikin `"production_year": <angka atau null>`:

- `POST /api/products/{productId}/stock-items` (create)
- `PUT /api/stock-items/{id}` (update)
- `GET /api/stock-items/{id}` (detail)
- `GET /api/products/{productId}/stock-items` (list per produk — tiap item di `items[]`)
- `GET /api/stock-items/lookup` (hasil scan barcode)
- `POST /api/purchase-orders/{id}/receive` → di dalam `received_units[]`

Kalau field-nya belum pernah diisi, nilainya `null` (bukan `0` atau string kosong) — pastikan FE
handle `null` dengan aman (tampilkan "-" atau kosongkan input, jangan render `"null"` sebagai teks).

---

## 4. Response yang **TIDAK** punya field ini (jangan salah expect)

- **Item response transaksi** — `POST /api/transactions`, `GET /api/transactions/{id}`,
  `GET /api/transactions/{id}/receipt` (struk). Field `items[]` di sini **tidak** menyertakan
  `production_year` sama sekali, konsisten dengan `condition`/`purchase_price` yang juga sudah
  tidak ditampilkan di respons transaksi. Kalau butuh nilainya (mis. buat cetak label unit hasil
  BUY), ambil dari `GET /api/stock-items/{id}` pakai `stock_item_id` yang ada di tiap item.
- **`GET /api/stock-items/{id}/label`** (data cetak label barcode) — tidak berubah, tidak ada
  `production_year` di situ.

---

## 5. Ringkasan cepat

| Form/Halaman | Field baru? | Wajib? | Tampil balik di response create-nya sendiri? |
|---|---|---|---|
| Tambah/edit stok manual | Ya | Tidak | Ya |
| Form buyback (BUY) | Ya | Tidak | **Tidak** — fetch `GET /api/stock-items/{id}` |
| Form terima PO | Ya (per serial) | Tidak | Ya (`received_units[]`) |
| Halaman transaksi/struk | Tidak berubah | — | — |
| Label cetak barcode | Tidak berubah | — | — |
