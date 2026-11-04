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
	ID         int64
	Title      string
	Body       string
	Level      string
	Source     string // fonte mostrata in plancia; "" = Servizio informatico
	StartsAt   time.Time
	EndsAt     *time.Time
	Notify     bool
	CreatedAt  time.Time
	CreatedBy  string
	NotifiedAt *time.Time // quando è partita la notifica; nil = non ancora
	ArchivedAt *time.Time // archiviato a mano: fuori dalla plancia; nil = no

	NotForViewer bool // calcolato dal web: avviso non destinato a chi guarda (niente popup)
	ReadByViewer bool // calcolato dal web: già letto da chi guarda (dal DB, non dal browser)
	CanRead      bool // calcolato dal web: chi guarda è riconosciuto, «Ho letto» viene registrato
	ReadPct      int  // calcolato dal web: «Letto dal X% dei colleghi» (0 = non mostrato)
}

const alertCols = `id, title, body, level, source, starts_at, ends_at, notify, created_at, created_by, notified_at, archived_at`

const levelOrder = `CASE level WHEN 'urgent' THEN 0 WHEN 'maintenance' THEN 1 ELSE 2 END`

func scanAlert(s scanner) (Alert, error) {
	var a Alert
	var starts, created string
	var ends, notified, archived sql.NullString
	if err := s.Scan(&a.ID, &a.Title, &a.Body, &a.Level, &a.Source, &starts, &ends, &a.Notify, &created, &a.CreatedBy, &notified, &archived); err != nil {
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
	if notified.Valid {
		n, err := parseTime(notified.String)
		if err != nil {
			return a, err
		}
		a.NotifiedAt = &n
	}
	if archived.Valid {
		v, err := parseTime(archived.String)
		if err != nil {
			return a, err
		}
		a.ArchivedAt = &v
	}
	return a, nil
}

// ActiveAt: avviso in plancia in quell'istante (iniziato, non scaduto, non archiviato).
func (a Alert) ActiveAt(now time.Time) bool {
	return a.ArchivedAt == nil && !a.StartsAt.After(now) && (a.EndsAt == nil || now.Before(*a.EndsAt))
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

// ListActiveAlerts: starts_at <= now < ends_at (o senza fine), non archiviati,
// ordinati per gravità.
func (db *DB) ListActiveAlerts(now time.Time) ([]Alert, error) {
	n := formatTime(now)
	return queryAlerts(db, `SELECT `+alertCols+` FROM alerts
WHERE archived_at IS NULL AND starts_at <= ? AND (ends_at IS NULL OR ends_at > ?)
ORDER BY `+levelOrder+`, starts_at DESC, id DESC`, n, n)
}

// ListAlertsForAdmin separa avvisi correnti/programmati da quelli archiviati o
// scaduti (ultimi 30, i più recenti prima).
func (db *DB) ListAlertsForAdmin(now time.Time) (current, expired []Alert, err error) {
	n := formatTime(now)
	current, err = queryAlerts(db, `SELECT `+alertCols+` FROM alerts
WHERE archived_at IS NULL AND (ends_at IS NULL OR ends_at > ?) ORDER BY starts_at DESC, id DESC`, n)
	if err != nil {
		return nil, nil, err
	}
	expired, err = queryAlerts(db, `SELECT `+alertCols+` FROM alerts
WHERE archived_at IS NOT NULL OR ends_at <= ?
ORDER BY COALESCE(archived_at, ends_at) DESC, id DESC LIMIT 30`, n)
	return current, expired, err
}

func (db *DB) GetAlert(id int64) (Alert, error) {
	a, err := scanAlert(db.QueryRow(`SELECT `+alertCols+` FROM alerts WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

func (db *DB) CreateAlert(a Alert) (int64, error) { return createAlert(db, a) }

func createAlert(q execer, a Alert) (int64, error) {
	res, err := q.Exec(`
INSERT INTO alerts (title, body, level, source, starts_at, ends_at, notify, created_at, created_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.Title, a.Body, a.Level, a.Source, formatTime(a.StartsAt), nullableTime(a.EndsAt), a.Notify, formatTime(a.CreatedAt), a.CreatedBy)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (db *DB) UpdateAlert(a Alert) error { return updateAlert(db, a) }

func updateAlert(q execer, a Alert) error {
	return checkAffected(q.Exec(`
UPDATE alerts SET title = ?, body = ?, level = ?, source = ?, starts_at = ?, ends_at = ?, notify = ?
WHERE id = ?`,
		a.Title, a.Body, a.Level, a.Source, formatTime(a.StartsAt), nullableTime(a.EndsAt), a.Notify, a.ID))
}

// ArchiveAlert toglie l'avviso dalla plancia senza toccarne le date; già
// archiviato = resta la prima data di archiviazione.
func (db *DB) ArchiveAlert(id int64, now time.Time) error {
	if _, err := db.GetAlert(id); err != nil {
		return err
	}
	_, err := db.Exec(`UPDATE alerts SET archived_at = ? WHERE id = ? AND archived_at IS NULL`, formatTime(now), id)
	return err
}

// RestoreAlert toglie l'archiviazione: l'avviso torna a seguire le sue date.
func (db *DB) RestoreAlert(id int64) error {
	return checkAffected(db.Exec(`UPDATE alerts SET archived_at = NULL WHERE id = ?`, id))
}

func (db *DB) DeleteAlert(id int64) error {
	return db.deleteContent(ContentAlert, "alerts", id)
}

// PendingNotifications: avvisi con notifica richiesta, attivi e non ancora notificati.
func (db *DB) PendingNotifications(now time.Time) ([]Alert, error) {
	n := formatTime(now)
	return queryAlerts(db, `SELECT `+alertCols+` FROM alerts
WHERE notify = 1 AND notified_at IS NULL AND archived_at IS NULL AND starts_at <= ? AND (ends_at IS NULL OR ends_at > ?)
ORDER BY starts_at, id`, n, n)
}

// MarkNotified marca l'avviso come notificato; false se lo era già (un solo invio).
func (db *DB) MarkNotified(id int64, at time.Time) (bool, error) {
	res, err := db.Exec(`UPDATE alerts SET notified_at = ? WHERE id = ? AND notified_at IS NULL`, formatTime(at), id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}
