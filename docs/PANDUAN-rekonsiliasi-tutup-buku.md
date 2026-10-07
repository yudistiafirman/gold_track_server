# Panduan Fitur — Rekonsiliasi Saldo & Tutup Buku Harian

**Untuk:** Pemilik / Manajemen Toko
**Hak akses:** Super Admin
**Lokasi menu:** Laporan › **Tutup Buku** dan Laporan › **Rekonsiliasi**

Cek otomatis apakah pergerakan kas & stok emas hari ini benar-benar sesuai dengan laba yang
tercatat — menggantikan pengecekan manual antar-sheet Excel.

---

## Latar belakang

Selama ini pengecekan dilakukan manual: satu sheet Excel per hari dengan baris *"Total Saldo"*,
lalu tiap pagi saldo hari ini dibandingkan dengan saldo kemarin — selisihnya harus sama dengan
laba hari itu. Kalau tidak cocok, berarti ada yang salah catat.

Fitur ini melakukan perbandingan tersebut secara otomatis. Terdiri dari dua bagian yang saling
melengkapi:

| Komponen | Menu | Fungsi |
|---|---|---|
| **1. Tutup Buku Harian** | Tutup Buku | Menyimpan snapshot beku saldo di akhir hari sebagai titik acuan (*baseline*). Setara "mengunci sheet hari ini" di Excel. |
| **2. Rekonsiliasi Saldo** | Rekonsiliasi | Membandingkan saldo toko saat ini dengan *baseline* terakhir + laba yang sudah dibukukan sejak itu. Menampilkan status **Sinkron / Tidak Sinkron**. |

---

## Komponen 1 — Tutup Buku Harian

Di halaman **Tutup Buku** terdapat tombol **"Tutup Hari Ini"**. Saat ditekan, sistem mengambil
foto (*snapshot*) posisi keuangan toko saat itu juga dan menyimpannya permanen dengan tanggal hari
itu. Muncul dialog konfirmasi terlebih dahulu sebelum tersimpan.

### Isi setiap baris penutupan

| Kolom | Arti |
|---|---|
| **Saldo Uang** | Total kas + saldo seluruh rekening pada saat tombol ditekan. |
| **Nilai Emas** | Nilai seluruh stok emas yang masih ada di toko saat itu. |
| **Total Saldo** | Saldo Uang + Nilai Emas. **Inilah angka acuan (*baseline*)** yang dipakai rekonsiliasi keesokan harinya. |
| **Tanggal Ditutup** | Tanggal buku yang ditutup. |
| **Ditutup Pada** | Tanggal & jam persis tombol ditekan (jejak waktu). |

### Aturan penting

- **Dilakukan manual, tiap akhir hari kerja.** Tidak ada proses otomatis yang menutup hari
  sendiri. Petugas harus menekan tombolnya.
- **Tidak bisa diubah atau dihapus.** Setiap penutupan adalah catatan historis permanen. Tidak ada
  fitur edit "saldo yang tercatat kemarin".
- **Satu hari hanya bisa ditutup sekali.** Mencoba menutup hari yang sama untuk kedua kalinya akan
  ditolak sistem.

---

## Komponen 2 — Rekonsiliasi Saldo

Halaman **Rekonsiliasi** bersifat baca-saja dan tanpa filter. Setiap dibuka, sistem langsung
menghitung ulang dan membandingkan **saldo toko sekarang** dengan **penutupan buku terakhir + laba
sejak saat itu**.

### Lima angka yang ditampilkan

| Kartu | Cara dihitung |
|---|---|
| **Saldo Penutupan Terakhir** | Total Saldo dari penutupan terakhir **sebelum hari ini** (beserta tanggalnya). |
| **Laba Periode Berjalan** | Pendapatan − HPP − Pengeluaran, dihitung sejak hari setelah penutupan itu sampai hari ini. |
| **Saldo Diharapkan** | Saldo Penutupan Terakhir **+** Laba Periode Berjalan. Inilah saldo yang *seharusnya* dimiliki toko sekarang. |
| **Saldo Aktual** | Kas + saldo rekening + nilai stok emas **saat ini juga**. |
| **Selisih** | Saldo Aktual − Saldo Diharapkan. Idealnya nol. |

Di bawah kartu-kartu tersebut ada tabel **Rincian Laba Periode Berjalan** (Pendapatan, HPP,
Laba, lalu Pengeluaran sebagai catatan yang tidak mengurangi laba) supaya angka labanya bisa ditelusuri asal-usulnya.

### Arti status

| Status | Arti |
|---|---|
| 🟢 **Sinkron** | Selisih mendekati nol. Pergerakan saldo toko sudah persis sesuai laba yang tercatat — pembukuan bersih, tidak ada yang perlu ditindaklanjuti. |
| 🔴 **Tidak Sinkron** | Ada selisih yang nyata. Kemungkinan besar ada uang masuk/keluar yang belum tercatat — misalnya hasil penjualan yang belum dimasukkan ke rekening, pengeluaran yang tercatat dobel, atau salah input transaksi. |

- **Selisih negatif** → uang riil *lebih sedikit* dari yang seharusnya (paling sering: ada
  pemasukan yang belum dicatat ke rekening).
- **Selisih positif** → ada uang *lebih* yang tidak ada transaksi penjelasnya.

---

## Contoh Kasus — Membaca hasil rekonsiliasi

Toko menutup buku tanggal **30 Agustus 2026** dengan Total Saldo **Rp 158.500.000**. Keesokan
harinya (31 Agustus) terjadi penjualan: pendapatan Rp 1.500.000, HPP Rp 1.000.000 → laba
**Rp 500.000**.

### Skenario A — hasil penjualan belum dicatat ke rekening

```
Saldo Penutupan Terakhir (30 Agu 2026)   Rp 158.500.000
Laba Periode Berjalan (31 Agu)          + Rp     500.000
──────────────────────────────────────────────────────
Saldo Diharapkan                         Rp 159.000.000
Saldo Aktual (sekarang)                  Rp 157.500.000
──────────────────────────────────────────────────────
Selisih                                − Rp   1.500.000   → TIDAK SINKRON
```

Saldo aktual kurang Rp 1.500.000 dari yang diharapkan — nilai penjualan hari itu belum masuk ke
rekening. **Tindak lanjut:** catat pemasukan tersebut ke menu Saldo / rekening.

### Skenario B — semua transaksi sudah tercatat benar

```
Saldo Penutupan Terakhir (30 Agu 2026)   Rp 158.500.000
Laba Periode Berjalan (31 Agu)          + Rp     500.000
──────────────────────────────────────────────────────
Saldo Diharapkan                         Rp 159.000.000
Saldo Aktual (sekarang)                  Rp 159.000.000
──────────────────────────────────────────────────────
Selisih                                  Rp           0   → SINKRON
```

Kenaikan saldo persis sebesar laba. Tidak ada yang perlu ditindaklanjuti.

---

## Cara Pakai Harian

1. **Setiap sore, sebelum tutup toko** — buka menu **Tutup Buku**, tekan **"Tutup Hari Ini"**,
   lalu konfirmasi. Snapshot saldo hari ini tersimpan sebagai acuan.
2. **Keesokan harinya** — buka menu **Rekonsiliasi** dan lihat badge status di bagian atas.
3. **Jika Sinkron** — aman, lanjut operasional seperti biasa.
   **Jika Tidak Sinkron** — telusuri transaksi & pengeluaran hari sebelumnya sampai selisihnya
   ketemu, lalu perbaiki pencatatannya.

---

## Catatan

- **Kalau lupa tutup buku beberapa hari.** Periode rekonsiliasi otomatis mundur ke penutupan
  terakhir yang ada, dan laba seluruh rentang hari yang terlewat tetap ikut dihitung — sama
  seperti sheet Excel yang bolong, angkanya tidak hilang.
- **Belum pernah tutup buku sama sekali.** Halaman Rekonsiliasi menampilkan pesan *"Belum Ada
  Penutupan"* beserta tombol menuju halaman Tutup Buku — bukan angka perbandingan palsu.
  Rekonsiliasi butuh minimal satu *baseline*.
- **Dana & utang eksternal tidak ikut dihitung.** Keduanya adalah input manual yang terpisah.
  Sengaja dikecualikan dari rumus saldo agar tidak menambah gangguan pada angka perbandingan.
- **Hanya untuk Super Admin.** Kedua menu berada di grup Laporan dan hanya dapat diakses oleh
  peran Super Admin, setara dengan laporan keuangan lainnya.

> **Inti fitur:** di pembukuan yang sehat, kenaikan Total Saldo dari satu penutupan ke saat ini
> harus selalu sama dengan laba pada periode itu (pengeluaran hanya catatan, tidak mengurangi laba maupun saldo yang diharapkan). Rekonsiliasi menjaga persamaan tersebut —
> setiap selisih adalah sinyal ada pencatatan yang belum lengkap.

---

## Referensi teknis (untuk developer)

Kontrak request/response lengkap ada di `README.md`:

- `GET /api/reports/reconciliation` — hasil rekonsiliasi (field `has_baseline`, `last_closing_date`,
  `period_from`/`period_to`, `expected_saldo`, `actual_saldo`, `difference`, `in_sync`, dll).
- `GET/POST /api/daily-closings`, `GET /api/daily-closings/{id}` — penutupan harian (SUPER_ADMIN).

Perhitungan tanggal memakai UTC, konsisten dengan seluruh endpoint `/api/reports/*`. `POST
/api/daily-closings` selalu menutup **hari ini** (tidak bisa *backdate*); toleransi `in_sync`
adalah 0,01 (hanya untuk noise pembulatan, bukan selisih pembukuan nyata).
