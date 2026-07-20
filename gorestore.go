package main

import (
	"compress/gzip"
	"database/sql"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	_ "github.com/go-sql-driver/mysql"
)

// GoRestore adalah pure-Go MySQL restorer. Tidak membutuhkan client `mysql`.
// Membaca file SQL (atau .sql.gz) dan mengeksekusinya statement per statement.
func GoRestore(c Connection, backupFile string) error {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/?timeout=60s&interpolateParams=true",
		c.User, c.Password, c.Host, c.Port)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("gagal buka koneksi: %w", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		return fmt.Errorf("gagal hubungi server: %w", err)
	}

	reader, err := openSQLFile(backupFile)
	if err != nil {
		return err
	}
	defer reader.Close()

	content, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("gagal baca file backup: %w", err)
	}

	statements := splitStatements(string(content))
	total := len(statements)
	log.Printf("[INFO] Total %d statement SQL ditemukan", total)

	for i, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}

		// Tangani perintah khusus MySQL dump
		upper := strings.ToUpper(stmt)
		if strings.HasPrefix(upper, "SET ") || strings.HasPrefix(upper, "CREATE DATABASE") || strings.HasPrefix(upper, "USE ") {
			if _, err := db.Exec(stmt); err != nil {
				log.Printf("[WARN] Perintah khusus gagal (%d/%d): %s — %s", i+1, total, truncate(stmt, 80), err)
			}
			continue
		}

		if _, err := db.Exec(stmt); err != nil {
			// Jangan hentikan seluruh restore karena satu statement gagal
			log.Printf("[WARN] Statement gagal (%d/%d): %s — %s", i+1, total, truncate(stmt, 80), err)
		}

		// Progress log tiap 100 statement
		if (i+1)%100 == 0 {
			log.Printf("[INFO] Progress: %d/%d statement", i+1, total)
		}
	}

	log.Printf("[INFO] Restore selesai: %d statement diproses", total)
	return nil
}

// openSQLFile membuka file SQL, mendukung .sql dan .sql.gz.
func openSQLFile(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("gagal buka file: %w", err)
	}

	if strings.HasSuffix(path, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("file bukan gzip valid: %w", err)
		}
		return &readCloserCombo{Reader: gz, closer: f}, nil
	}

	return f, nil
}

type readCloserCombo struct {
	io.Reader
	closer io.Closer
}

func (r *readCloserCombo) Close() error {
	return r.closer.Close()
}

// splitStatements memecah string SQL menjadi per-statement, dengan memperhatikan
// string literal ('...') dan comment (-- ... atau /* ... */).
// DELIMITER tidak didukung (dump dari Go dumper tidak menggunakannya).
func splitStatements(sql string) []string {
	var statements []string
	var current strings.Builder
	inSingleQuote := false
	inDoubleQuote := false
	inLineComment := false
	inBlockComment := false
	escaped := false

	for i := 0; i < len(sql); i++ {
		ch := sql[i]

		if escaped {
			current.WriteByte(ch)
			escaped = false
			continue
		}

		if ch == '\\' && (inSingleQuote || inDoubleQuote) {
			current.WriteByte(ch)
			escaped = true
			continue
		}

		if inLineComment {
			if ch == '\n' {
				inLineComment = false
				current.WriteByte(ch)
			}
			continue
		}

		if inBlockComment {
			if ch == '*' && i+1 < len(sql) && sql[i+1] == '/' {
				inBlockComment = false
				current.WriteString("*/")
				i++
			}
			continue
		}

		if inSingleQuote {
			current.WriteByte(ch)
			if ch == '\'' {
				// Cek escaped quote ('')
				if i+1 < len(sql) && sql[i+1] == '\'' {
					current.WriteByte('\'')
					i++
				} else {
					inSingleQuote = false
				}
			}
			continue
		}

		if inDoubleQuote {
			current.WriteByte(ch)
			if ch == '"' {
				inDoubleQuote = false
			}
			continue
		}

		// Di luar string literal
		if ch == '\'' {
			inSingleQuote = true
			current.WriteByte(ch)
			continue
		}
		if ch == '"' {
			inDoubleQuote = true
			current.WriteByte(ch)
			continue
		}

		// Comment
		if ch == '-' && i+1 < len(sql) && sql[i+1] == '-' {
			inLineComment = true
			current.WriteString("--")
			i++
			continue
		}
		if ch == '/' && i+1 < len(sql) && sql[i+1] == '*' {
			inBlockComment = true
			current.WriteString("/*")
			i++
			continue
		}

		// Statement separator
		if ch == ';' {
			s := strings.TrimSpace(current.String())
			if s != "" {
				statements = append(statements, s)
			}
			current.Reset()
			continue
		}

		current.WriteByte(ch)
	}

	// Sisa
	s := strings.TrimSpace(current.String())
	if s != "" {
		statements = append(statements, s)
	}

	return statements
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
