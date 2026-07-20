package main

import (
	"database/sql"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// GoDump adalah pure-Go MySQL dumper. Tidak membutuhkan mysqldump binary.
// Menghasilkan SQL valid yang bisa direstore dengan `mysql` client atau
// dengan GoRestore.
func GoDump(c Connection, w io.Writer) error {
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

	// Header
	fmt.Fprintf(w, "-- DB Backup Scheduler (pure-Go dumper)\n")
	fmt.Fprintf(w, "-- Server: %s:%s\n", c.Host, c.Port)
	fmt.Fprintf(w, "-- Waktu : %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(w, "--\n\n")
	fmt.Fprintln(w, "SET NAMES utf8mb4;")
	fmt.Fprintln(w, "SET FOREIGN_KEY_CHECKS = 0;")
	fmt.Fprintln(w, "SET UNIQUE_CHECKS = 0;\n")

	databases := []string{c.Database}
	if strings.TrimSpace(c.Database) == "" {
		databases, err = listDatabases(db)
		if err != nil {
			return fmt.Errorf("gagal daftar database: %w", err)
		}
	}

	for _, dbName := range databases {
		if err := dumpDatabase(db, dbName, w); err != nil {
			log.Printf("[WARN] Gagal dump database %s: %s", dbName, err)
			fmt.Fprintf(w, "-- ERROR dumping %s: %s\n\n", dbName, err)
		}
	}

	fmt.Fprintln(w, "SET FOREIGN_KEY_CHECKS = 1;")
	fmt.Fprintln(w, "SET UNIQUE_CHECKS = 1;")
	return nil
}

func listDatabases(db *sql.DB) ([]string, error) {
	rows, err := db.Query("SHOW DATABASES")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		// Lewati database sistem
		switch name {
		case "information_schema", "performance_schema", "mysql", "sys":
			continue
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

func dumpDatabase(db *sql.DB, dbName string, w io.Writer) error {
	fmt.Fprintf(w, "-- ============================\n")
	fmt.Fprintf(w, "-- Database: %s\n", dbName)
	fmt.Fprintf(w, "-- ============================\n\n")
	fmt.Fprintf(w, "CREATE DATABASE IF NOT EXISTS `%s`;\n", dbName)
	fmt.Fprintf(w, "USE `%s`;\n\n", dbName)

	tables, err := listTables(db, dbName)
	if err != nil {
		return fmt.Errorf("gagal daftar tabel: %w", err)
	}

	// Disable FK checks per-DB supaya restore tidak error urutan
	fmt.Fprintln(w, "SET FOREIGN_KEY_CHECKS = 0;")

	for _, tbl := range tables {
		if err := dumpTable(db, dbName, tbl, w); err != nil {
			log.Printf("[WARN] Gagal dump tabel %s.%s: %s", dbName, tbl, err)
			fmt.Fprintf(w, "-- ERROR dumping %s: %s\n\n", tbl, err)
		}
	}

	// Dump views
	views, err := listViews(db, dbName)
	if err == nil {
		for _, v := range views {
			if err := dumpView(db, dbName, v, w); err != nil {
				log.Printf("[WARN] Gagal dump view %s.%s: %s", dbName, v, err)
			}
		}
	}

	fmt.Fprintln(w, "SET FOREIGN_KEY_CHECKS = 1;")
	fmt.Fprintln(w, "")
	return nil
}

func listTables(db *sql.DB, dbName string) ([]string, error) {
	rows, err := db.Query("SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_TYPE = 'BASE TABLE' ORDER BY TABLE_NAME", dbName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

func listViews(db *sql.DB, dbName string) ([]string, error) {
	rows, err := db.Query("SELECT TABLE_NAME FROM information_schema.VIEWS WHERE TABLE_SCHEMA = ?", dbName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

func dumpTable(db *sql.DB, dbName, tblName string, w io.Writer) error {
	// 1. CREATE TABLE
	createSQL, err := getCreateTable(db, dbName, tblName)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "DROP TABLE IF EXISTS `%s`;\n", tblName)
	fmt.Fprintf(w, "%s;\n\n", createSQL)

	// 2. Data — stream per batch
	if err := dumpData(db, dbName, tblName, w); err != nil {
		return err
	}

	return nil
}

func getCreateTable(db *sql.DB, dbName, tblName string) (string, error) {
	var tbl, createSQL string
	err := db.QueryRow(fmt.Sprintf("SHOW CREATE TABLE `%s`.`%s`", dbName, tblName)).Scan(&tbl, &createSQL)
	if err != nil {
		return "", err
	}
	return createSQL, nil
}

const insertBatchSize = 200

func dumpData(db *sql.DB, dbName, tblName string, w io.Writer) error {
	rows, err := db.Query(fmt.Sprintf("SELECT * FROM `%s`.`%s`", dbName, tblName))
	if err != nil {
		return err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	if len(cols) == 0 {
		return nil
	}

	colTypes, err := rows.ColumnTypes()
	if err != nil {
		return err
	}

	// Build column list
	colList := "`" + strings.Join(cols, "`, `") + "`"

	rowCount := 0
	batchCount := 0
	buf := make([]interface{}, len(cols))
	ptrs := make([]interface{}, len(cols))
	for i := range ptrs {
		ptrs[i] = &buf[i]
	}

	var values strings.Builder

	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}

		if batchCount > 0 {
			values.WriteString(",\n")
		}
		values.WriteString("(")
		for i, val := range buf {
			if i > 0 {
				values.WriteString(", ")
			}
			values.WriteString(formatValue(val, colTypes[i]))
		}
		values.WriteString(")")

		rowCount++
		batchCount++

		if batchCount >= insertBatchSize {
			fmt.Fprintf(w, "INSERT INTO `%s` (%s) VALUES\n%s;\n\n", tblName, colList, values.String())
			values.Reset()
			batchCount = 0
		}
	}

	if batchCount > 0 {
		fmt.Fprintf(w, "INSERT INTO `%s` (%s) VALUES\n%s;\n\n", tblName, colList, values.String())
	}

	return rows.Err()
}

func dumpView(db *sql.DB, dbName, viewName string, w io.Writer) error {
	var view, createSQL, charSet, collation string
	err := db.QueryRow(fmt.Sprintf("SHOW CREATE VIEW `%s`.`%s`", dbName, viewName)).Scan(&view, &createSQL, &charSet, &collation)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "DROP VIEW IF EXISTS `%s`;\n", viewName)
	fmt.Fprintf(w, "%s;\n\n", createSQL)
	return nil
}

// formatValue mengubah nilai dari database menjadi string SQL yang valid.
func formatValue(val interface{}, colType *sql.ColumnType) string {
	if val == nil {
		return "NULL"
	}

	switch v := val.(type) {
	case []byte:
		return quoteString(string(v))
	case string:
		return quoteString(v)
	case int64:
		return fmt.Sprintf("%d", v)
	case float64:
		// Gunakan format asli untuk menghindari presisi hilang
		s := fmt.Sprintf("%v", v)
		if !strings.Contains(s, ".") && !strings.Contains(s, "e") && !strings.Contains(s, "E") {
			s += ".0"
		}
		return s
	case bool:
		if v {
			return "1"
		}
		return "0"
	case time.Time:
		return quoteString(v.Format("2006-01-02 15:04:05"))
	default:
		return quoteString(fmt.Sprintf("%v", v))
	}
}

func quoteString(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "'", "''")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\x00", "")
	return "'" + s + "'"
}
