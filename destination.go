package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Destinator adalah kontrak penyimpanan hasil backup. Inilah titik perluasan
// utama: menambah FTP atau Google Drive nanti cukup membuat implementasi baru
// tanpa menyentuh alur backup sama sekali.
type Destinator interface {
	// Simpan memindahkan file backup lokal ke tujuan akhir dan mengembalikan
	// lokasi akhirnya (untuk dicatat di riwayat).
	Simpan(fileLokal string) (lokasiAkhir string, err error)

	// Rapikan menghapus backup lama sesuai retensi (0 = simpan semua).
	Rapikan(retensi int) error

	Kind() string
}

// BuatDestinator memetakan Destination (data) ke implementasi (perilaku).
func BuatDestinator(d Destination) (Destinator, error) {
	switch d.Type {
	case "local":
		if strings.TrimSpace(d.Path) == "" {
			return nil, fmt.Errorf("destinasi lokal wajib punya folder tujuan")
		}
		return &LokalDest{folder: d.Path}, nil

	// Sudah disiapkan untuk versi berikutnya:
	case "ftp":
		return nil, fmt.Errorf("destinasi FTP belum tersedia (menyusul)")
	case "gdrive":
		return nil, fmt.Errorf("destinasi Google Drive belum tersedia (menyusul)")

	default:
		return nil, fmt.Errorf("tipe destinasi %q tidak dikenal", d.Type)
	}
}

// LokalDest menyimpan backup ke folder di komputer ini.
type LokalDest struct {
	folder string
}

func (l *LokalDest) Kind() string { return "local" }

func (l *LokalDest) Simpan(fileLokal string) (string, error) {
	if err := os.MkdirAll(l.folder, 0o755); err != nil {
		return "", fmt.Errorf("gagal menyiapkan folder tujuan: %w", err)
	}

	tujuan := filepath.Join(l.folder, filepath.Base(fileLokal))

	// Backup dibuat di folder sementara lalu dipindah ke tujuan. Rename gagal
	// bila beda filesystem, jadi ada fallback salin-lalu-hapus.
	if err := os.Rename(fileLokal, tujuan); err != nil {
		if err := salinFile(fileLokal, tujuan); err != nil {
			return "", err
		}
		_ = os.Remove(fileLokal)
	}
	return tujuan, nil
}

func (l *LokalDest) Rapikan(retensi int) error {
	if retensi <= 0 {
		return nil
	}

	entri, err := os.ReadDir(l.folder)
	if err != nil {
		return nil // folder belum ada = tidak ada yang dirapikan
	}

	type berkas struct {
		nama  string
		mtime int64
	}
	var backup []berkas
	for _, e := range entri {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql.gz") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		backup = append(backup, berkas{e.Name(), info.ModTime().UnixNano()})
	}

	if len(backup) <= retensi {
		return nil
	}

	// Urutkan terbaru dulu, hapus sisanya setelah N teratas.
	sort.Slice(backup, func(i, j int) bool { return backup[i].mtime > backup[j].mtime })
	for _, b := range backup[retensi:] {
		_ = os.Remove(filepath.Join(l.folder, b.nama))
	}
	return nil
}

func salinFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
