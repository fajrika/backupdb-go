package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Server struct {
	store *Store
	sched *Scheduler
}

func (srv *Server) routes(mux *http.ServeMux, index []byte) {
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(index)
	})

	mux.HandleFunc("/api/connections", srv.handleConnections)
	mux.HandleFunc("/api/connections/", srv.handleConnectionByID)
	mux.HandleFunc("/api/connections/test", srv.handleTestConnection)

	mux.HandleFunc("/api/destinations", srv.handleDestinations)
	mux.HandleFunc("/api/destinations/", srv.handleDestinationByID)

	mux.HandleFunc("/api/schedules", srv.handleSchedules)
	mux.HandleFunc("/api/schedules/", srv.handleScheduleByID)

	mux.HandleFunc("/api/run", srv.handleRunNow)
	mux.HandleFunc("/api/history", srv.handleHistory)
	mux.HandleFunc("/api/backups", srv.handleBackups)
	mux.HandleFunc("/api/restore", srv.handleRestore)
}

// ---- helpers --------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func ok(w http.ResponseWriter, v interface{}) {
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "data": v})
}

func fail(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]interface{}{"success": false, "message": msg})
}

func decode(r *http.Request, v interface{}) error {
	return json.NewDecoder(r.Body).Decode(v)
}

// idDari mengambil ID dari path seperti /api/connections/<id>.
func idDari(path, prefix string) string {
	return strings.Trim(strings.TrimPrefix(path, prefix), "/")
}

// ---- connections ----------------------------------------------------------

func (srv *Server) handleConnections(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// Sembunyikan password saat daftar dikirim ke UI.
		list := srv.store.Connections()
		for i := range list {
			list[i].Password = mask(list[i].Password)
		}
		ok(w, list)
	case http.MethodPost:
		var c Connection
		if err := decode(r, &c); err != nil {
			fail(w, http.StatusBadRequest, "data tidak valid: "+err.Error())
			return
		}
		if c.Driver == "" {
			c.Driver = "mysql"
		}
		if c.Port == "" {
			c.Port = "3306"
		}
		saved, err := srv.store.SaveConnection(c)
		if err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		saved.Password = mask(saved.Password)
		ok(w, saved)
	default:
		fail(w, http.StatusMethodNotAllowed, "metode tidak diizinkan")
	}
}

func (srv *Server) handleConnectionByID(w http.ResponseWriter, r *http.Request) {
	id := idDari(r.URL.Path, "/api/connections/")
	if id == "" || id == "test" {
		fail(w, http.StatusNotFound, "koneksi tidak ditemukan")
		return
	}

	switch r.Method {
	case http.MethodPut:
		var c Connection
		if err := decode(r, &c); err != nil {
			fail(w, http.StatusBadRequest, "data tidak valid: "+err.Error())
			return
		}
		c.ID = id
		// Password kosong saat edit = pertahankan yang lama (UI mengirim mask).
		if c.Password == "" || c.Password == maskValue {
			if ex, found := srv.store.Connection(id); found {
				c.Password = ex.Password
			}
		}
		if c.Driver == "" {
			c.Driver = "mysql"
		}
		saved, err := srv.store.SaveConnection(c)
		if err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		saved.Password = mask(saved.Password)
		ok(w, saved)
	case http.MethodDelete:
		if err := srv.store.DeleteConnection(id); err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		srv.sched.Reload()
		ok(w, nil)
	default:
		fail(w, http.StatusMethodNotAllowed, "metode tidak diizinkan")
	}
}

func (srv *Server) handleTestConnection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		fail(w, http.StatusMethodNotAllowed, "metode tidak diizinkan")
		return
	}
	var c Connection
	if err := decode(r, &c); err != nil {
		fail(w, http.StatusBadRequest, "data tidak valid: "+err.Error())
		return
	}
	// Jika password bermask (edit tanpa ganti), pakai yang tersimpan.
	if (c.Password == "" || c.Password == maskValue) && c.ID != "" {
		if ex, found := srv.store.Connection(c.ID); found {
			c.Password = ex.Password
		}
	}
	if err := TestConnection(c); err != nil {
		fail(w, http.StatusOK, err.Error()) // 200 dgn success:false — bukan error server
		return
	}
	ok(w, "Koneksi berhasil")
}

// ---- destinations ---------------------------------------------------------

func (srv *Server) handleDestinations(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ok(w, srv.store.Destinations())
	case http.MethodPost:
		var d Destination
		if err := decode(r, &d); err != nil {
			fail(w, http.StatusBadRequest, "data tidak valid: "+err.Error())
			return
		}
		if d.Type == "" {
			d.Type = "local"
		}
		saved, err := srv.store.SaveDestination(d)
		if err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		ok(w, saved)
	default:
		fail(w, http.StatusMethodNotAllowed, "metode tidak diizinkan")
	}
}

func (srv *Server) handleDestinationByID(w http.ResponseWriter, r *http.Request) {
	id := idDari(r.URL.Path, "/api/destinations/")
	if id == "" {
		fail(w, http.StatusNotFound, "destinasi tidak ditemukan")
		return
	}
	switch r.Method {
	case http.MethodPut:
		var d Destination
		if err := decode(r, &d); err != nil {
			fail(w, http.StatusBadRequest, "data tidak valid: "+err.Error())
			return
		}
		d.ID = id
		saved, err := srv.store.SaveDestination(d)
		if err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		ok(w, saved)
	case http.MethodDelete:
		if err := srv.store.DeleteDestination(id); err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		srv.sched.Reload()
		ok(w, nil)
	default:
		fail(w, http.StatusMethodNotAllowed, "metode tidak diizinkan")
	}
}

// ---- schedules ------------------------------------------------------------

func (srv *Server) handleSchedules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ok(w, srv.store.Schedules())
	case http.MethodPost:
		var sc Schedule
		if err := decode(r, &sc); err != nil {
			fail(w, http.StatusBadRequest, "data tidak valid: "+err.Error())
			return
		}
		if strings.TrimSpace(sc.Cron) == "" {
			fail(w, http.StatusBadRequest, "jadwal (cron) wajib diisi")
			return
		}
		saved, err := srv.store.SaveSchedule(sc)
		if err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		srv.sched.Reload()
		ok(w, saved)
	default:
		fail(w, http.StatusMethodNotAllowed, "metode tidak diizinkan")
	}
}

func (srv *Server) handleScheduleByID(w http.ResponseWriter, r *http.Request) {
	id := idDari(r.URL.Path, "/api/schedules/")
	if id == "" {
		fail(w, http.StatusNotFound, "jadwal tidak ditemukan")
		return
	}
	switch r.Method {
	case http.MethodPut:
		var sc Schedule
		if err := decode(r, &sc); err != nil {
			fail(w, http.StatusBadRequest, "data tidak valid: "+err.Error())
			return
		}
		sc.ID = id
		saved, err := srv.store.SaveSchedule(sc)
		if err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		srv.sched.Reload()
		ok(w, saved)
	case http.MethodDelete:
		if err := srv.store.DeleteSchedule(id); err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		srv.sched.Reload()
		ok(w, nil)
	default:
		fail(w, http.StatusMethodNotAllowed, "metode tidak diizinkan")
	}
}

// ---- run & history --------------------------------------------------------

func (srv *Server) handleRunNow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		fail(w, http.StatusMethodNotAllowed, "metode tidak diizinkan")
		return
	}
	var req struct {
		ConnectionID  string `json:"connection_id"`
		DestinationID string `json:"destination_id"`
	}
	if err := decode(r, &req); err != nil {
		fail(w, http.StatusBadRequest, "data tidak valid: "+err.Error())
		return
	}
	run, err := srv.sched.RunNow(req.ConnectionID, req.DestinationID)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if run.Status == StatusFailed {
		// Backup jalan tapi gagal — kirim pesannya, tetap 200 supaya UI bisa
		// menampilkan detail, bukan error mentah.
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": false, "message": run.Message, "data": run})
		return
	}
	ok(w, run)
}

func (srv *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	ok(w, srv.store.History())
}

// backupInfo adalah satu file backup yang tersedia untuk restore.
type backupInfo struct {
	ConnID   string `json:"conn_id"`
	ConnName string `json:"conn_name"`
	DestID   string `json:"dest_id"`
	DestName string `json:"dest_name"`
	File     string `json:"file"`
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	ModTime  string `json:"mtime"`
}

// handleBackups mendaftar semua file .sql.gz dari destinasi lokal, terbaru dulu.
func (srv *Server) handleBackups(w http.ResponseWriter, r *http.Request) {
	conns := srv.store.Connections()
	var out []backupInfo
	for _, d := range srv.store.Destinations() {
		if d.Type != "local" || strings.TrimSpace(d.Path) == "" {
			continue
		}
		entri, err := os.ReadDir(d.Path)
		if err != nil {
			continue
		}
		for _, e := range entri {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql.gz") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			connID, connName := tebakKoneksi(e.Name(), conns)
			out = append(out, backupInfo{
				ConnID:   connID,
				ConnName: connName,
				DestID:   d.ID,
				DestName: d.Name,
				File:     e.Name(),
				Path:     filepath.Join(d.Path, e.Name()),
				Size:     info.Size(),
				ModTime:  info.ModTime().Format(time.RFC3339),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime > out[j].ModTime })
	ok(w, out)
}

// tebakKoneksi mencocokkan nama file backup dengan koneksi yang ada.
// Format file: <namakoneksi>-<YYYYmmdd-HHMMSS>.sql.gz
func tebakKoneksi(filename string, conns []Connection) (connID, connName string) {
	base := strings.TrimSuffix(filename, ".sql.gz")
	// Cari timestamp pattern: -YYYYmmdd-HHMMSS
	for i := len(base) - 1; i >= 0; i-- {
		if base[i] == '-' {
			potential := base[i+1:]
			if len(potential) == 15 && potential[8] == '-' {
				prefix := base[:i]
				for _, c := range conns {
					namaAman := amankanNama(c.Name)
					if prefix == namaAman {
						return c.ID, c.Name
					}
				}
				return "", prefix
			}
		}
	}
	return "", ""
}

// handleRestore menjalankan restore. File yang diminta HARUS berada di dalam
// salah satu folder destinasi terdaftar — mencegah pembacaan file sembarang
// lewat API.
func (srv *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		fail(w, http.StatusMethodNotAllowed, "metode tidak diizinkan")
		return
	}
	var req struct {
		ConnectionID string `json:"connection_id"`
		Path         string `json:"path"`
	}
	if err := decode(r, &req); err != nil {
		fail(w, http.StatusBadRequest, "data tidak valid: "+err.Error())
		return
	}
	if req.ConnectionID == "" || req.Path == "" {
		fail(w, http.StatusBadRequest, "koneksi tujuan dan file backup wajib dipilih")
		return
	}

	if !srv.fileBackupSah(req.Path) {
		fail(w, http.StatusBadRequest, "file backup tidak berada di folder destinasi mana pun")
		return
	}

	run, err := srv.sched.RestoreNow(req.ConnectionID, req.Path)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": false, "message": run.Message, "data": run})
		return
	}
	ok(w, run)
}

// fileBackupSah memastikan path berada di dalam folder salah satu destinasi
// lokal terdaftar (setelah resolusi simbolik/relatif), agar API restore tidak
// bisa disuruh membaca file arbitrer.
func (srv *Server) fileBackupSah(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	abs = filepath.Clean(abs)
	for _, d := range srv.store.Destinations() {
		if d.Type != "local" || strings.TrimSpace(d.Path) == "" {
			continue
		}
		base, err := filepath.Abs(d.Path)
		if err != nil {
			continue
		}
		base = filepath.Clean(base)
		if abs == base {
			continue
		}
		if strings.HasPrefix(abs, base+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// ---- password masking -----------------------------------------------------

const maskValue = "••••••••"

func mask(s string) string {
	if s == "" {
		return ""
	}
	return maskValue
}
