package main

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// TestConnection memverifikasi bahwa koneksi benar-benar bisa dihubungi.
// Dipakai tombol "Tes Koneksi" di UI sebelum menyimpan.
func TestConnection(c Connection) error {
	switch c.Driver {
	case "", "mysql":
		return testMySQL(c)
	default:
		return fmt.Errorf("driver %q belum didukung (saat ini hanya mysql)", c.Driver)
	}
}

func testMySQL(c Connection) error {
	// Tanpa nama database di DSN: kalau Database kosong (backup semua DB),
	// koneksi tetap harus bisa diuji.
	dbName := c.Database
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?timeout=8s",
		c.User, c.Password, c.Host, c.Port, dbName)

	pool, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("konfigurasi koneksi salah: %w", err)
	}
	defer pool.Close()

	done := make(chan error, 1)
	go func() { done <- pool.Ping() }()

	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("tidak bisa terhubung: %w", err)
		}
		return nil
	case <-time.After(10 * time.Second):
		return fmt.Errorf("timeout menghubungi %s:%s", c.Host, c.Port)
	}
}
