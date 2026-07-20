package main

import (
	_ "embed"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

//go:embed templates/index.html
var indexHTML []byte

func main() {
	defer tahanSaatPanik()

	port := getenv("PORT", "3400")
	dataDir := getenv("DATA_DIR", defaultDataDir())
	tmpDir := filepath.Join(dataDir, "tmp")

	store, err := NewStore(filepath.Join(dataDir, "data.json"))
	if err != nil {
		log.Printf("[GAGAL] Tidak bisa menyiapkan penyimpanan data: %s", err)
		tahanSelamanya()
		return
	}

	sched := NewScheduler(store, tmpDir)
	sched.Reload()

	srv := &Server{store: store, sched: sched}
	mux := http.NewServeMux()
	srv.routes(mux, indexHTML)

	addr := ":" + port
	fmt.Println("========================================")
	fmt.Println("  DB BACKUP SCHEDULER")
	fmt.Println("  (Pure Go — tidak perlu mysqldump)")
	fmt.Println("========================================")
	fmt.Printf("  Web      : http://localhost%s\n", addr)
	fmt.Printf("  Data     : %s\n", dataDir)
	fmt.Println("========================================")
	fmt.Println("[OK] Backup & restore menggunakan pure Go — tidak perlu install MySQL client.")

	go bukaBrowser("http://localhost" + addr)

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Printf("[GAGAL] Server web tidak bisa dijalankan: %s", err)
		log.Printf("        Kemungkinan port %s sedang dipakai program lain.", port)
		tahanSelamanya()
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func defaultDataDir() string {
	if base, err := os.UserConfigDir(); err == nil {
		return filepath.Join(base, "db-backup-scheduler")
	}
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), "data")
	}
	return "data"
}

func tahanSaatPanik() {
	if r := recover(); r != nil {
		log.Printf("[GAGAL] Terjadi kesalahan tak terduga: %v", r)
		tahanSelamanya()
	}
}

func tahanSelamanya() {
	fmt.Println()
	fmt.Println("------------------------------------------------------------")
	fmt.Println(" Aplikasi berhenti karena kesalahan di atas.")
	fmt.Println(" Jendela ini dibiarkan terbuka agar pesan bisa dibaca.")
	fmt.Println(" Tutup jendela ini (atau Ctrl+C) untuk keluar.")
	fmt.Println("------------------------------------------------------------")
	for {
		time.Sleep(time.Hour)
	}
}

func bukaBrowser(url string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[INFO] Gagal membuka browser: %v — buka manual ke %s", r, url)
		}
	}()

	time.Sleep(500 * time.Millisecond)

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		log.Printf("[INFO] Buka browser gagal: %s — buka manual ke %s", err, url)
	}
}
