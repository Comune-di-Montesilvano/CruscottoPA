package database

import (
	"database/sql"
	"errors"
	"time"
)

// DayLayout è il formato delle date di calendario (giorni interi, senza ora).
const DayLayout = "2006-01-02"

// CalendarEvent è una chiusura dell'ente o un evento/scadenza.
type CalendarEvent struct {
	ID          int64
	Title       string
	Kind        string // "closure" | "event"
	StartsOn    time.Time
	EndsOn      time.Time
	Description string
	Yearly      bool
}

const calendarCols = `id, title, kind, starts_on, ends_on, description, yearly`

func scanCalendarEvent(s scanner) (CalendarEvent, error) {
	var e CalendarEvent
	var start, end string
	if err := s.Scan(&e.ID, &e.Title, &e.Kind, &start, &end, &e.Description, &e.Yearly); err != nil {
		return e, err
	}
	var err error
	if e.StartsOn, err = time.Parse(DayLayout, start); err != nil {
		return e, err
	}
	e.EndsOn, err = time.Parse(DayLayout, end)
	return e, err
}

// ListCalendarEvents restituisce tutti gli eventi (la tabella è piccola: il
// filtro per periodo, annuali compresi, lo fa internal/calendar).
func (db *DB) ListCalendarEvents() ([]CalendarEvent, error) {
	rows, err := db.Query(`SELECT ` + calendarCols + ` FROM calendar_events ORDER BY starts_on, title COLLATE NOCASE, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CalendarEvent{}
	for rows.Next() {
		e, err := scanCalendarEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (db *DB) GetCalendarEvent(id int64) (CalendarEvent, error) {
	e, err := scanCalendarEvent(db.QueryRow(`SELECT `+calendarCols+` FROM calendar_events WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrNotFound
	}
	return e, err
}

func (db *DB) CreateCalendarEvent(e CalendarEvent) (int64, error) {
	res, err := db.Exec(`
INSERT INTO calendar_events (title, kind, starts_on, ends_on, description, yearly)
VALUES (?, ?, ?, ?, ?, ?)`,
		e.Title, e.Kind, e.StartsOn.Format(DayLayout), e.EndsOn.Format(DayLayout), e.Description, e.Yearly)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (db *DB) UpdateCalendarEvent(e CalendarEvent) error {
	return checkAffected(db.Exec(`
UPDATE calendar_events SET title = ?, kind = ?, starts_on = ?, ends_on = ?, description = ?, yearly = ?
WHERE id = ?`,
		e.Title, e.Kind, e.StartsOn.Format(DayLayout), e.EndsOn.Format(DayLayout), e.Description, e.Yearly, e.ID))
}

func (db *DB) DeleteCalendarEvent(id int64) error {
	return checkAffected(db.Exec(`DELETE FROM calendar_events WHERE id = ?`, id))
}
