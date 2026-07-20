package main

import "time"

// Connection adalah sumber data yang akan di-backup.
//
// Untuk sekarang hanya "mysql". Field Driver sengaja ada sejak awal supaya
// menambah PostgreSQL dsb. nanti tidak perlu mengubah struktur.
type Connection struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Driver    string    `json:"driver"` // "mysql"
	Host      string    `json:"host"`
	Port      string    `json:"port"`
	User      string    `json:"user"`
	Password  string    `json:"password"`
	Database  string    `json:"database"` // kosong = seluruh database
	CreatedAt time.Time `json:"created_at"`
}

// Destination adalah tujuan penyimpanan hasil backup.
//
// Type "local" yang jalan sekarang. "ftp" dan "gdrive" sudah disiapkan di
// interface Destinator (lihat destination.go) sehingga tinggal menambah
// implementasi tanpa mengubah alur backup. Config menampung setelan
// spesifik-tipe (mis. host/user FTP) agar model tidak berubah tiap tipe baru.
type Destination struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Type      string            `json:"type"`      // "local" | (nanti) "ftp" | "gdrive"
	Path      string            `json:"path"`      // local: folder tujuan
	Retention int               `json:"retention"` // simpan N backup terakhir; 0 = simpan semua
	Config    map[string]string `json:"config,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
}

// Schedule mengikat sebuah koneksi ke sebuah destinasi pada jadwal cron.
type Schedule struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	ConnectionID  string    `json:"connection_id"`
	DestinationID string    `json:"destination_id"`
	Cron          string    `json:"cron"` // ekspresi cron 5-field
	Enabled       bool      `json:"enabled"`
	LastRun       time.Time `json:"last_run"`
	LastStatus    string    `json:"last_status"`
	CreatedAt     time.Time `json:"created_at"`
}

// BackupRun mencatat satu eksekusi backup ATAU restore (dibedakan lewat Kind).
type BackupRun struct {
	ID           string    `json:"id"`
	Kind         string    `json:"kind"`        // "backup" | "restore"
	ScheduleID   string    `json:"schedule_id"` // kosong = dijalankan manual
	ConnectionID string    `json:"connection_id"`
	ConnName     string    `json:"conn_name"`
	DestName     string    `json:"dest_name"` // backup: nama destinasi; restore: file sumber
	StartedAt    time.Time `json:"started_at"`
	FinishedAt   time.Time `json:"finished_at"`
	Status       string    `json:"status"` // "running" | "success" | "failed"
	SizeBytes    int64     `json:"size_bytes"`
	FilePath     string    `json:"file_path"`
	Message      string    `json:"message"`
}

const (
	StatusRunning = "running"
	StatusSuccess = "success"
	StatusFailed  = "failed"

	KindBackup  = "backup"
	KindRestore = "restore"
)
