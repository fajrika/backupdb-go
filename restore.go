package main

import (
	"fmt"
	"os"
	"time"
)

// Restore memuat isi file backup (.sql.gz atau .sql) ke server pada koneksi
// tujuan. Menggunakan GoRestore (pure Go), tidak membutuhkan client `mysql`.
func Restore(target Connection, backupFile string) (BackupRun, error) {
	run := BackupRun{
		Kind:         KindRestore,
		ConnectionID: target.ID,
		ConnName:     target.Name,
		DestName:     baseNama(backupFile),
		FilePath:     backupFile,
		StartedAt:    time.Now(),
		Status:       StatusRunning,
	}

	info, err := os.Stat(backupFile)
	if err != nil {
		return gagal(run, fmt.Errorf("file backup tidak ditemukan: %w", err)), err
	}
	run.SizeBytes = info.Size()

	if err := GoRestore(target, backupFile); err != nil {
		return gagal(run, err), err
	}

	run.FinishedAt = time.Now()
	run.Status = StatusSuccess
	run.Message = "Restore berhasil ke " + target.Name
	return run, nil
}

func baseNama(p string) string {
	if i := p[len(p)-1]; i == '/' || i == '\\' {
		return p
	}
	if i := len(p) - 1; i >= 0 {
		for j := i; j >= 0; j-- {
			if p[j] == '/' || p[j] == '\\' {
				return p[j+1:]
			}
		}
	}
	return p
}
