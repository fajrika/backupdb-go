# DB Backup Scheduler

Aplikasi backup database terjadwal dengan antarmuka web. Satu binary Go, tanpa
instalasi runtime — cukup jalankan, lalu atur lewat browser.

- **Koneksi**: master sumber database (saat ini **MySQL**)
- **Destinasi**: tujuan penyimpanan backup (saat ini **Lokal**; FTP & Google
  Drive sudah disiapkan di arsitektur, tinggal ditambah)
- **Jadwal**: cron yang mengikat koneksi → destinasi
- **Restore**: kembalikan file backup ke koneksi asal **atau** koneksi lain
- **Riwayat**: catatan tiap eksekusi backup/restore (sukses/gagal, ukuran, pesan)

## Cara pakai

```bash
./db-backup-scheduler
```

Otomatis membuka `http://localhost:3400`. Lalu:

1. Tab **Koneksi** → tambah koneksi MySQL, klik **Tes Koneksi** sebelum simpan.
2. Tab **Destinasi** → tambah folder tujuan + retensi (simpan N backup terakhir).
3. Tab **Jadwal** → pilih koneksi + destinasi + frekuensi. Bisa juga **▶ Backup**
   untuk menjalankan sekarang tanpa menunggu jadwal.
4. Tab **Riwayat** → pantau hasilnya.

## Restore

Tab **Restore** memuat file `.sql.gz` dari folder destinasi lokal, lalu
menjalankannya ke koneksi yang dipilih lewat client `mysql`. Tujuannya bebas —
koneksi **asal** (mengembalikan) atau koneksi **lain** (menyalin ke server
staging, dsb.).

> ⚠ Restore **menimpa** data di server tujuan dan tidak bisa dibatalkan. UI
> meminta konfirmasi yang menyebutkan koneksi tujuan + nama file sebelum jalan.

Karena backup dibuat dengan `--databases`, file berisi `CREATE DATABASE`/`USE`,
sehingga nama database mengikuti isi backup (bukan diganti). Ini yang diinginkan
untuk memindahkan/menyalin database utuh ke server lain. Demi keamanan, API
restore menolak file di luar folder destinasi terdaftar.

## Yang dibutuhkan di komputer target

**`mysqldump`** (untuk backup) dan **`mysql`** (untuk restore) harus tersedia.
Keduanya sengaja memakai tool resmi, bukan parser buatan sendiri — dump/restore
manual mudah salah pada trigger, routine, kolom JSON, dan escaping, dan hasil
yang diam-diam rusak jauh lebih berbahaya daripada gagal dengan pesan jelas.

- macOS: `brew install mysql-client`
- Windows: ikut dalam MySQL/MariaDB atau XAMPP. Bila tidak di PATH, set
  `MYSQLDUMP_PATH` / `MYSQL_PATH` ke lokasi binary-nya.
- Linux: `apt install mysql-client` / `mariadb-client`

Aplikasi tetap jalan walau keduanya belum ada — hanya backup/restore yang
tertunda, dengan peringatan jelas.

## Konfigurasi (opsional, lewat environment variable)

| Env | Default | Keterangan |
|---|---|---|
| `PORT` | `3400` | Port web server |
| `DATA_DIR` | folder config OS | Lokasi `data.json` (koneksi, jadwal, riwayat) |
| `MYSQLDUMP_PATH` | `mysqldump` | Path ke binary mysqldump bila tidak di PATH |
| `MYSQL_PATH` | `mysql` | Path ke client mysql (restore) bila tidak di PATH |

## Format jadwal (cron 5 kolom)

`menit jam tanggal bulan hari`. UI menyediakan pilihan Harian/Tiap-N-jam/
Mingguan/Custom. Contoh yang dihasilkan:

- Harian 02:30 → `30 2 * * *`
- Tiap 6 jam → `0 */6 * * *`
- Mingguan Jumat 23:00 → `0 23 * * 5`

## Keamanan

Password koneksi disimpan di `data.json` (di dalam `DATA_DIR`) dalam bentuk
teks. Perlakukan folder itu sebagai rahasia; `data/` sudah masuk `.gitignore`.
Saat dump berjalan, password diberikan ke mysqldump lewat file konfigurasi
sementara ber-izin 0600, **bukan** argumen CLI, sehingga tidak bocor di daftar
proses (`ps aux`).

## Build dari sumber

```bash
go build -o db-backup-scheduler .
```

Cross-compile (cgo-free, satu binary per OS):

```bash
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o dist/db-backup-scheduler-windows-amd64.exe .
CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -ldflags="-s -w" -o dist/db-backup-scheduler-linux-amd64 .
CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -ldflags="-s -w" -o dist/db-backup-scheduler-macos-arm64 .
```

## Menambah destinasi baru (FTP / Google Drive)

Arsitekturnya sudah disiapkan: implementasikan interface `Destinator`
(`Simpan`, `Rapikan`, `Kind`) di `destination.go`, lalu daftarkan di
`BuatDestinator`. Alur backup tidak perlu diubah sama sekali.

```go
type Destinator interface {
    Simpan(fileLokal string) (lokasiAkhir string, err error)
    Rapikan(retensi int) error
    Kind() string
}
```
