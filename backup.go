package main

import (
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Backup menjalankan satu backup: dump (pure Go) -> gzip -> serahkan ke destinasi.
// Tidak membutuhkan mysqldump binary.
func Backup(c Connection, d Destination, tmpDir string) (BackupRun, error) {
	run := BackupRun{
		Kind:         KindBackup,
		ConnectionID: c.ID,
		ConnName:     c.Name,
		DestName:     d.Name,
		StartedAt:    time.Now(),
		Status:       StatusRunning,
	}

	dest, err := BuatDestinator(d)
	if err != nil {
		return gagal(run, err), err
	}

	// Nama file: <koneksi>-<YYYYmmdd-HHMMSS>.sql.gz
	stamp := run.StartedAt.Format("20060102-150405")
	namaAman := amankanNama(c.Name)
	namaFile := fmt.Sprintf("%s-%s.sql.gz", namaAman, stamp)

	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return gagal(run, fmt.Errorf("gagal menyiapkan folder sementara: %w", err)), err
	}
	tmpFile := filepath.Join(tmpDir, namaFile)

	if err := dumpKeGzip(c, tmpFile); err != nil {
		_ = os.Remove(tmpFile)
		return gagal(run, err), err
	}

	// Serahkan ke destinasi.
	lokasi, err := dest.Simpan(tmpFile)
	if err != nil {
		_ = os.Remove(tmpFile)
		return gagal(run, fmt.Errorf("dump berhasil tapi gagal disimpan ke tujuan: %w", err)), err
	}

	// Rapikan backup lama sesuai retensi.
	if err := dest.Rapikan(d.Retention); err != nil {
		run.Message = "peringatan: gagal merapikan backup lama: " + err.Error()
	}

	info, _ := os.Stat(lokasi)
	run.FinishedAt = time.Now()
	run.Status = StatusSuccess
	run.FilePath = lokasi
	if info != nil {
		run.SizeBytes = info.Size()
	}
	if run.Message == "" {
		run.Message = "Backup berhasil"
	}
	return run, nil
}

// dumpKeGzip menjalankan GoDump dan menulis SQL langsung ke file gzip.
func dumpKeGzip(c Connection, tujuan string) error {
	out, err := os.Create(tujuan)
	if err != nil {
		return fmt.Errorf("gagal membuat file backup: %w", err)
	}
	defer out.Close()

	gz := gzip.NewWriter(out)
	if err := GoDump(c, gz); err != nil {
		gz.Close()
		_ = os.Remove(tujuan)
		return err
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("gagal menutup file gzip: %w", err)
	}
	return out.Close()
}

func amankanNama(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		s = "backup"
	}
	repl := func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}
	return strings.Map(repl, s)
}

func gagal(run BackupRun, err error) BackupRun {
	run.FinishedAt = time.Now()
	run.Status = StatusFailed
	run.Message = err.Error()
	return run
}
