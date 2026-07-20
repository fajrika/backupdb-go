package main

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// Scheduler menjalankan backup sesuai jadwal cron dan menyediakan eksekusi
// manual. Aman dipakai lintas goroutine.
type Scheduler struct {
	store  *Store
	tmpDir string

	mu   sync.Mutex
	cron *cron.Cron
}

func NewScheduler(store *Store, tmpDir string) *Scheduler {
	return &Scheduler{store: store, tmpDir: tmpDir}
}

// Reload membangun ulang seluruh entri cron dari jadwal yang aktif. Dipanggil
// saat start dan setiap kali jadwal berubah lewat UI.
func (s *Scheduler) Reload() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cron != nil {
		s.cron.Stop()
	}
	s.cron = cron.New()

	for _, sc := range s.store.Schedules() {
		if !sc.Enabled {
			continue
		}
		sc := sc // salin untuk closure
		_, err := s.cron.AddFunc(sc.Cron, func() {
			s.jalankanJadwal(sc.ID)
		})
		if err != nil {
			log.Printf("[WARN] Jadwal %q dilewati — ekspresi cron tidak valid (%q): %s", sc.Name, sc.Cron, err)
		}
	}

	s.cron.Start()
}

// jalankanJadwal mengeksekusi backup untuk sebuah jadwal, lalu mencatat hasilnya.
func (s *Scheduler) jalankanJadwal(scheduleID string) {
	sc, ok := s.store.Schedule(scheduleID)
	if !ok {
		return
	}

	conn, ok := s.store.Connection(sc.ConnectionID)
	if !ok {
		log.Printf("[WARN] Jadwal %q: koneksi tidak ditemukan", sc.Name)
		return
	}
	dest, ok := s.store.Destination(sc.DestinationID)
	if !ok {
		log.Printf("[WARN] Jadwal %q: destinasi tidak ditemukan", sc.Name)
		return
	}

	s.eksekusi(conn, dest, sc.ID)
}

// RunNow menjalankan backup manual untuk pasangan koneksi+destinasi tertentu.
func (s *Scheduler) RunNow(connID, destID string) (BackupRun, error) {
	conn, ok := s.store.Connection(connID)
	if !ok {
		return BackupRun{}, fmt.Errorf("koneksi tidak ditemukan")
	}
	dest, ok := s.store.Destination(destID)
	if !ok {
		return BackupRun{}, fmt.Errorf("destinasi tidak ditemukan")
	}
	return s.eksekusi(conn, dest, ""), nil
}

// RestoreNow memuat file backup ke koneksi tujuan (boleh koneksi asal maupun
// koneksi lain), mencatat hasilnya ke riwayat, dan aman dari panic.
func (s *Scheduler) RestoreNow(targetID, backupFile string) (hasil BackupRun, err error) {
	target, ok := s.store.Connection(targetID)
	if !ok {
		return BackupRun{}, fmt.Errorf("koneksi tujuan tidak ditemukan")
	}

	awal := BackupRun{
		Kind:         KindRestore,
		ConnectionID: target.ID,
		ConnName:     target.Name,
		DestName:     baseNama(backupFile),
		FilePath:     backupFile,
		StartedAt:    time.Now(),
		Status:       StatusRunning,
	}
	awal = s.store.AddRun(awal)

	defer func() {
		if r := recover(); r != nil {
			hasil = awal
			hasil.FinishedAt = time.Now()
			hasil.Status = StatusFailed
			hasil.Message = fmt.Sprintf("kesalahan tak terduga: %v", r)
			s.store.UpdateRun(hasil)
			log.Printf("[ERROR] Restore panic: %v", r)
		}
	}()

	run, rerr := Restore(target, backupFile)
	run.ID = awal.ID
	s.store.UpdateRun(run)

	if rerr != nil {
		log.Printf("[ERROR] Restore ke %q gagal: %s", target.Name, rerr)
	} else {
		log.Printf("[OK] Restore ke %q sukses (%s)", target.Name, manusiawiByte(run.SizeBytes))
	}
	return run, rerr
}

// eksekusi menjalankan backup dan mencatat riwayat. Panic di dalam backup
// (mis. dependensi eksternal) tidak boleh menjatuhkan seluruh aplikasi.
func (s *Scheduler) eksekusi(conn Connection, dest Destination, scheduleID string) (hasil BackupRun) {
	// Catat entri "running" lebih dulu supaya terlihat di UI selama proses.
	awal := BackupRun{
		Kind:         KindBackup,
		ScheduleID:   scheduleID,
		ConnectionID: conn.ID,
		ConnName:     conn.Name,
		DestName:     dest.Name,
		StartedAt:    time.Now(),
		Status:       StatusRunning,
	}
	awal = s.store.AddRun(awal)

	defer func() {
		if r := recover(); r != nil {
			hasil = awal
			hasil.FinishedAt = time.Now()
			hasil.Status = StatusFailed
			hasil.Message = fmt.Sprintf("kesalahan tak terduga: %v", r)
			s.store.UpdateRun(hasil)
			if scheduleID != "" {
				s.store.MarkScheduleRun(scheduleID, StatusFailed, hasil.FinishedAt)
			}
			log.Printf("[ERROR] Backup panic: %v", r)
		}
	}()

	run, err := Backup(conn, dest, s.tmpDir)
	run.ID = awal.ID
	run.ScheduleID = scheduleID
	s.store.UpdateRun(run)

	if scheduleID != "" {
		s.store.MarkScheduleRun(scheduleID, run.Status, run.FinishedAt)
	}

	if err != nil {
		log.Printf("[ERROR] Backup %q -> %q gagal: %s", conn.Name, dest.Name, err)
	} else {
		log.Printf("[OK] Backup %q -> %q sukses (%s)", conn.Name, dest.Name, manusiawiByte(run.SizeBytes))
	}
	return run
}

func manusiawiByte(n int64) string {
	const u = 1024
	if n < u {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(u), 0
	for x := n / u; x >= u; x /= u {
		div *= u
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}
