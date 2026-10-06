package database

import (
	"database/sql"
	"errors"
	"time"
)

const (
	LevelUrgent      = "urgent"
	LevelMaintenance = "maintenance"
	LevelNews        = "news"
)

// Alert è un avviso della striscia in plancia. EndsAt nil = senza scadenza.
type Alert struct {
	ID        int64
	Title     string
	Body      string
	Level     string
	StartsAt  time.Time
	EndsAt    *time.Time
	Notify    bool
	CreatedAt time.Time
	CreatedBy string
}

const alertCols = `id, title, body, level, starts_at, ends_at, notify, created_at, created_by`

const levelOrder = `CASE level WHEN 'urgent' THEN 0 WHEN 'maintenance' THEN 1 ELSE 2 END`

func scanAlert(s scanner) (Alert, error) {
	var a Alert
	var starts, created string
	var ends sql.NullString
	if err := s.Scan(&a.ID, &a.Title, &a.Body, &a.Level, &starts, &ends, &a.Notify, &created, &a.CreatedBy); err != nil {
		return a, err
	}
	var err error
	if a.StartsAt, err = parseTime(starts); err != nil {
		return a, err
	}
	if a.CreatedAt, err = parseTime(created); err != nil {
		return a, err
	}
	if ends.Valid {
		e, err := parseTime(ends.String)
		if err != nil {
			return a, err
		}
		a.EndsAt = &e
	}
	return a, nil
}

func queryAlerts(db *DB, query string, args ...any) ([]Alert, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Alert{}
	for rows.Next() {
		a, err := scanAlert(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return formatTime(*t)
}

// ListActiveAlerts: starts_at <= now < ends_at (o senza fine), ordinati per gravità.
func (db *DB) ListActiveAlerts(now time.Time) ([]Alert, error) {
	n := formatTime(now)
	return queryAlerts(db, `SELECT `+alertCols+` FROM alerts
WHERE starts_at <= ? AND (ends_at IS NULL OR ends_at > ?)
ORDER BY `+levelOrder+`, starts_at DESC, id DESC`, n, n)
}

// ListAlertsForAdmin separa avvisi correnti/programmati da quelli scaduti (ultimi 30).
func (db *DB) ListAlertsForAdmin(now time.Time) (current, expired []Alert, err error) {
	n := formatTime(now)
	current, err = queryAlerts(db, `SELECT `+alertCols+` FROM alerts
WHERE ends_at IS NULL OR ends_at > ? ORDER BY starts_at DESC, id DESC`, n)
	if err != nil {
		return nil, nil, err
	}
	expired, err = queryAlerts(db, `SELECT `+alertCols+` FROM alerts
WHERE ends_at <= ? ORDER BY ends_at DESC, id DESC LIMIT 30`, n)
	return current, expired, err
}

func (db *DB) GetAlert(id int64) (Alert, error) {
	a, err := scanAlert(db.QueryRow(`SELECT `+alertCols+` FROM alerts WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

func (db *DB) CreateAlert(a Alert) (int64, error) {
	res, err := db.Exec(`
INSERT INTO alerts (title, body, level, starts_at, ends_at, notify, created_at, created_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.Title, a.Body, a.Level, formatTime(a.StartsAt), nullableTime(a.EndsAt), a.Notify, formatTime(a.CreatedAt), a.CreatedBy)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (db *DB) UpdateAlert(a Alert) error {
	return checkAffected(db.Exec(`
UPDATE alerts SET title = ?, body = ?, level = ?, starts_at = ?, ends_at = ?, notify = ?
WHERE id = ?`,
		a.Title, a.Body, a.Level, formatTime(a.StartsAt), nullableTime(a.EndsAt), a.Notify, a.ID))
}

func (db *DB) DeleteAlert(id int64) error {
	return checkAffected(db.Exec(`DELETE FROM alerts WHERE id = ?`, id))
}
