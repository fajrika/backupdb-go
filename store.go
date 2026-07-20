package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Store menyimpan seluruh state ke satu file JSON, dijaga mutex.
//
// JSON dipilih ketimbang database tersemat supaya binary tetap satu file
// tanpa cgo dan tanpa dependensi eksternal. Skalanya kecil (puluhan koneksi/
// jadwal, ratusan riwayat), jadi menulis ulang seluruh file tiap perubahan
// masih murah dan jauh lebih sederhana daripada mengelola migrasi.
type Store struct {
	mu   sync.RWMutex
	path string
	data stateFile
}

type stateFile struct {
	Connections  []Connection  `json:"connections"`
	Destinations []Destination `json:"destinations"`
	Schedules    []Schedule    `json:"schedules"`
	History      []BackupRun   `json:"history"`
}

const maxHistory = 500 // batasi agar file tidak membengkak tanpa batas

func NewStore(path string) (*Store, error) {
	s := &Store{path: path}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("gagal menyiapkan folder data: %w", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		// File baru — mulai kosong.
		return s, s.flush()
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("gagal baca file data: %w", err)
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &s.data); err != nil {
			return nil, fmt.Errorf("file data rusak (%s): %w", path, err)
		}
	}
	return s, nil
}

// flush menulis state ke disk secara atomik (tulis ke file sementara lalu
// rename) supaya mati listrik di tengah penulisan tidak merusak file.
func (s *Store) flush() error {
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ---- Connections ----------------------------------------------------------

func (s *Store) Connections() []Connection {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Connection(nil), s.data.Connections...)
}

func (s *Store) Connection(id string) (Connection, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.data.Connections {
		if c.ID == id {
			return c, true
		}
	}
	return Connection{}, false
}

func (s *Store) SaveConnection(c Connection) (Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if c.ID == "" {
		c.ID = newID()
		c.CreatedAt = time.Now()
		s.data.Connections = append(s.data.Connections, c)
	} else {
		found := false
		for i, ex := range s.data.Connections {
			if ex.ID == c.ID {
				c.CreatedAt = ex.CreatedAt
				s.data.Connections[i] = c
				found = true
				break
			}
		}
		if !found {
			return Connection{}, fmt.Errorf("koneksi %s tidak ditemukan", c.ID)
		}
	}
	return c, s.flush()
}

func (s *Store) DeleteConnection(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Connections = filterConn(s.data.Connections, id)
	return s.flush()
}

// ---- Destinations ---------------------------------------------------------

func (s *Store) Destinations() []Destination {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Destination(nil), s.data.Destinations...)
}

func (s *Store) Destination(id string) (Destination, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, d := range s.data.Destinations {
		if d.ID == id {
			return d, true
		}
	}
	return Destination{}, false
}

func (s *Store) SaveDestination(d Destination) (Destination, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if d.ID == "" {
		d.ID = newID()
		d.CreatedAt = time.Now()
		s.data.Destinations = append(s.data.Destinations, d)
	} else {
		found := false
		for i, ex := range s.data.Destinations {
			if ex.ID == d.ID {
				d.CreatedAt = ex.CreatedAt
				s.data.Destinations[i] = d
				found = true
				break
			}
		}
		if !found {
			return Destination{}, fmt.Errorf("destinasi %s tidak ditemukan", d.ID)
		}
	}
	return d, s.flush()
}

func (s *Store) DeleteDestination(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Destinations = filterDest(s.data.Destinations, id)
	return s.flush()
}

// ---- Schedules ------------------------------------------------------------

func (s *Store) Schedules() []Schedule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Schedule(nil), s.data.Schedules...)
}

func (s *Store) Schedule(id string) (Schedule, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sc := range s.data.Schedules {
		if sc.ID == id {
			return sc, true
		}
	}
	return Schedule{}, false
}

func (s *Store) SaveSchedule(sc Schedule) (Schedule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sc.ID == "" {
		sc.ID = newID()
		sc.CreatedAt = time.Now()
		s.data.Schedules = append(s.data.Schedules, sc)
	} else {
		found := false
		for i, ex := range s.data.Schedules {
			if ex.ID == sc.ID {
				sc.CreatedAt = ex.CreatedAt
				// Pertahankan jejak eksekusi terakhir bila pemanggil tidak mengisinya.
				if sc.LastRun.IsZero() {
					sc.LastRun = ex.LastRun
					sc.LastStatus = ex.LastStatus
				}
				s.data.Schedules[i] = sc
				found = true
				break
			}
		}
		if !found {
			return Schedule{}, fmt.Errorf("jadwal %s tidak ditemukan", sc.ID)
		}
	}
	return sc, s.flush()
}

func (s *Store) DeleteSchedule(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.data.Schedules[:0]
	for _, sc := range s.data.Schedules {
		if sc.ID != id {
			out = append(out, sc)
		}
	}
	s.data.Schedules = out
	return s.flush()
}

func (s *Store) MarkScheduleRun(id, status string, when time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, sc := range s.data.Schedules {
		if sc.ID == id {
			s.data.Schedules[i].LastRun = when
			s.data.Schedules[i].LastStatus = status
			_ = s.flush()
			return
		}
	}
}

// ---- History --------------------------------------------------------------

func (s *Store) History() []BackupRun {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := append([]BackupRun(nil), s.data.History...)
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out
}

func (s *Store) AddRun(r BackupRun) BackupRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.ID == "" {
		r.ID = newID()
	}
	s.data.History = append(s.data.History, r)
	if len(s.data.History) > maxHistory {
		s.data.History = s.data.History[len(s.data.History)-maxHistory:]
	}
	_ = s.flush()
	return r
}

func (s *Store) UpdateRun(r BackupRun) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, ex := range s.data.History {
		if ex.ID == r.ID {
			s.data.History[i] = r
			_ = s.flush()
			return
		}
	}
}

func filterConn(list []Connection, id string) []Connection {
	out := list[:0]
	for _, c := range list {
		if c.ID != id {
			out = append(out, c)
		}
	}
	return out
}

func filterDest(list []Destination, id string) []Destination {
	out := list[:0]
	for _, d := range list {
		if d.ID != id {
			out = append(out, d)
		}
	}
	return out
}
