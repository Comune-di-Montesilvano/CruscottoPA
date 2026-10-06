// Package database gestisce la persistenza SQLite di CruscottoPA.
package database

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite" // driver pure-Go, registra "sqlite"
)

var (
	ErrNotFound         = errors.New("database: record non trovato")
	ErrDuplicate        = errors.New("database: valore duplicato")
	ErrCategoryNotEmpty = errors.New("database: la categoria contiene ancora delle app")
)

// timeLayout è RFC 3339 in UTC a lunghezza fissa: le stringhe si confrontano
// correttamente anche in SQL (ordine lessicografico = ordine temporale).
const timeLayout = "2006-01-02T15:04:05Z"

func formatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

func parseTime(s string) (time.Time, error) { return time.Parse(timeLayout, s) }

// DB incapsula *sql.DB; i metodi di dominio sono nei file per entità.
type DB struct {
	*sql.DB
}

// Open apre (o crea) il database e applica le migrazioni mancanti.
// Le PRAGMA sono nel DSN così valgono per ogni connessione del pool.
func Open(path string) (*DB, error) {
	dsn := fmt.Sprintf(
		"file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)",
		path,
	)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("apertura database: %w", err)
	}
	if err := sqlDB.Ping(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("connessione database: %w", err)
	}
	db := &DB{sqlDB}
	if err := db.migrate(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("migrazioni: %w", err)
	}
	return db, nil
}

// SchemaVersion restituisce PRAGMA user_version.
func (db *DB) SchemaVersion() (int, error) {
	var v int
	err := db.QueryRow(`PRAGMA user_version`).Scan(&v)
	return v, err
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// checkAffected converte "nessuna riga toccata" in ErrNotFound.
func checkAffected(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
