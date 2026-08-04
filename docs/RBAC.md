# Hak Akses per Role (RBAC) — Gold Track

Dokumen ini merangkum **siapa boleh melakukan apa** di aplikasi, sesuai yang berjalan sekarang.
Silakan direview — kalau ada yang mau diubah, tulis di kolom **"Perubahan Diminta"** pada baris yang
sesuai. Baris yang sudah sesuai tidak perlu diisi.

---

## 1. Role yang tersedia

| Role | Biasanya dipakai oleh | Gambaran umum akses |
|---|---|---|
| **Super Admin** | Pemilik toko / pengelola tertinggi | Akses penuh ke semua fitur, termasuk kelola akun karyawan |
| **Admin** | Pengelola operasional toko sehari-hari | Akses ke hampir semua fitur, **kecuali** kelola akun karyawan |
| **Kasir** | Staff kasir di lantai toko | Akses terbatas: transaksi jual-beli, cek produk/stok, data pelanggan |

Semua orang wajib login dulu (punya akun) sebelum bisa memakai fitur apa pun di aplikasi ini.

---

## 2. Matrix hak akses

Legenda: ✅ boleh · ❌ tidak boleh

### Kelola Akun Karyawan (User)

| Aktivitas | Super Admin | Admin | Kasir | Perubahan Diminta |
|---|---|---|---|---|
| Melihat daftar & detail karyawan | ✅ | ❌ | ❌ | |
| Menambah akun karyawan baru | ✅ | ❌ | ❌ | |
| Mengubah data karyawan (nama, role, dll) | ✅ | ❌ | ❌ | |
| Menonaktifkan akun karyawan | ✅ | ❌ | ❌ | |

### Kategori & Merek Produk

| Aktivitas | Super Admin | Admin | Kasir | Perubahan Diminta |
|---|---|---|---|---|
| Melihat daftar kategori & merek | ✅ | ✅ | ❌ | |
| Menambah kategori/merek baru | ✅ | ✅ | ❌ | |
| Mengubah kategori/merek | ✅ | ✅ | ❌ | |
| Menghapus (menonaktifkan) kategori/merek | ✅ | ✅ | ❌ | |

### Produk

| Aktivitas | Super Admin | Admin | Kasir | Perubahan Diminta |
|---|---|---|---|---|
| Melihat daftar & detail produk | ✅ | ✅ | ✅ | |
| Menambah produk baru | ✅ | ✅ | ❌ | |
| Mengubah data produk | ✅ | ✅ | ❌ | |
| Menghapus (mengarsipkan) produk | ✅ | ✅ | ❌ | |

### Stok Barang (unit fisik per produk)

| Aktivitas | Super Admin | Admin | Kasir | Perubahan Diminta |
|---|---|---|---|---|
| Melihat daftar & detail stok barang | ✅ | ✅ | ✅ | |
| Cari barang lewat scan barcode | ✅ | ✅ | ✅ | |
| Cetak label barcode | ✅ | ✅ | ✅ | |
| Menambah unit stok baru | ✅ | ✅ | ❌ | |
| Mengubah data unit stok | ✅ | ✅ | ❌ | |
| Menghapus unit stok | ✅ | ✅ | ❌ | |

### Supplier

| Aktivitas | Super Admin | Admin | Kasir | Perubahan Diminta |
|---|---|---|---|---|
| Melihat daftar, detail & riwayat transaksi supplier | ✅ | ✅ | ❌ | |
| Menambah supplier baru | ✅ | ✅ | ❌ | |
| Mengubah data supplier | ✅ | ✅ | ❌ | |
| Menghapus (menonaktifkan) supplier | ✅ | ✅ | ❌ | |

### Pelanggan (Customer)

| Aktivitas | Super Admin | Admin | Kasir | Perubahan Diminta |
|---|---|---|---|---|
| Melihat daftar, detail & riwayat transaksi pelanggan | ✅ | ✅ | ✅ | |
| Menambah pelanggan baru | ✅ | ✅ | ✅ | |
| Mengubah data pelanggan | ✅ | ✅ | ❌ | |
| Menghapus (menonaktifkan) pelanggan | ✅ | ✅ | ❌ | |

### Transaksi (Jual, Beli, Jual-ke-Supplier)

| Aktivitas | Super Admin | Admin | Kasir | Perubahan Diminta |
|---|---|---|---|---|
| Membuat transaksi (checkout jual/beli) | ✅ | ✅ | ✅ | |
| Melihat detail transaksi | ✅ | ✅ | ✅ | |
| Mencetak struk transaksi | ✅ | ✅ | ✅ | |

### Purchase Order (Pemesanan Barang ke Supplier)

| Aktivitas | Super Admin | Admin | Kasir | Perubahan Diminta |
|---|---|---|---|---|
| Melihat daftar & detail pesanan | ✅ | ✅ | ❌ | |
| Membuat pesanan baru | ✅ | ✅ | ❌ | |
| Menerima barang pesanan (jadi stok) | ✅ | ✅ | ❌ | |
| Membatalkan pesanan | ✅ | ✅ | ❌ | |

### Stok Opname (Cek Fisik Stok)

| Aktivitas | Super Admin | Admin | Kasir | Perubahan Diminta |
|---|---|---|---|---|
| Membuka sesi pengecekan stok | ✅ | ✅ | ❌ | |
| Melihat detail sesi pengecekan | ✅ | ✅ | ❌ | |
| Scan barang saat pengecekan | ✅ | ✅ | ❌ | |
| Menyelesaikan sesi pengecekan | ✅ | ✅ | ❌ | |

### Pengeluaran Operasional (Biaya Toko)

| Aktivitas | Super Admin | Admin | Kasir | Perubahan Diminta |
|---|---|---|---|---|
| Melihat daftar kategori & data pengeluaran | ✅ | ✅ | ❌ | |
| Menambah kategori/pengeluaran baru | ✅ | ✅ | ❌ | |
| Mengubah data pengeluaran | ✅ | ✅ | ❌ | |
| Menghapus pengeluaran | ✅ | ✅ | ❌ | |

### Laporan

| Aktivitas | Super Admin | Admin | Kasir | Perubahan Diminta |
|---|---|---|---|---|
| Melihat laporan transaksi | ✅ | ✅ | ❌ | |
| Melihat laporan stok | ✅ | ✅ | ❌ | |
| Melihat laporan keuangan (untung/rugi) | ✅ | ✅ | ❌ | |
| Melihat dashboard ringkasan | ✅ | ✅ | ❌ | |

### Pengaturan Toko & Harga Emas

| Aktivitas | Super Admin | Admin | Kasir | Perubahan Diminta |
|---|---|---|---|---|
| Melihat & mengubah pengaturan toko (nama, alamat, telp) | ✅ | ✅ | ❌ | |
| Melihat harga emas referensi terkini | ✅ | ✅ | ✅ | |

---

## 3. Untuk Client — kebutuhan baru di luar tabel

> Isi baris di bawah kalau ada role baru yang dibutuhkan (mis. role khusus investor/auditor yang
> cuma boleh lihat laporan), atau ada pembatasan akses lain yang belum tercakup di atas.

| Kebutuhan Baru | Deskripsi | Prioritas | Status |
|---|---|---|---|
| _(contoh)_ Role "Investor" | Cuma boleh lihat laporan keuangan & dashboard, tidak boleh apa-apa lagi | Medium | Menunggu review |
| | | | |
| | | | |
