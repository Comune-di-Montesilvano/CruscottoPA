package database

import (
	"database/sql"
	"errors"
	"strings"
)

// «Noi del Comune»: numeri di gruppo e traguardi personali. Dati indicativi
// (identità dichiarata). Dei giorni di accesso si salva solo il giorno
// (YYYY-MM-DD, calendario di Europe/Rome), mai l'ora.

// RecordDay segna l'accesso del giorno; added = riga nuova. Il contatore dei
// traguardi cresce nella stessa transazione e non cala con la pulizia.
func (db *DB) RecordDay(username, day string) (added bool, err error) {
	u := strings.ToLower(strings.TrimSpace(username))
	if u == "" {
		return false, nil
	}
	tx, err := db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT OR IGNORE INTO user_days (username, day) VALUES (?, ?)`, u, day)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, nil
	}
	if _, err := tx.Exec(`UPDATE counters SET value = value + 1 WHERE name = 'accessi'`); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (db *DB) UsersOnDay(day string) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM user_days WHERE day = ?`, day).Scan(&n)
	return n, err
}

// UserDaysBetween: giorni di accesso dell'utente tra fromDay e toDay inclusi.
func (db *DB) UserDaysBetween(username, fromDay, toDay string) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM user_days WHERE username = ? AND day >= ? AND day <= ?`,
		strings.ToLower(strings.TrimSpace(username)), fromDay, toDay).Scan(&n)
	return n, err
}

// AccessTotal: giornate-utente dall'inizio (non cala con la pulizia).
func (db *DB) AccessTotal() (int64, error) {
	var n int64
	err := db.QueryRow(`SELECT value FROM counters WHERE name = 'accessi'`).Scan(&n)
	return n, err
}

// Milestones: soglie dei traguardi collettivi fino a upTo.
func Milestones(upTo int64) []int64 {
	out := []int64{}
	for _, t := range []int64{1000, 2500, 5000, 10000, 25000, 50000, 100000} {
		if t <= upTo {
			out = append(out, t)
		}
	}
	for t := int64(200000); t <= upTo; t += 100000 {
		out = append(out, t)
	}
	return out
}

// ReachMilestones scrive le soglie superate e non ancora registrate.
func (db *DB) ReachMilestones(total int64, day string) error {
	for _, t := range Milestones(total) {
		if _, err := db.Exec(`INSERT OR IGNORE INTO milestones (threshold, reached_on) VALUES (?, ?)`, t, day); err != nil {
			return err
		}
	}
	return nil
}

// MilestoneOn: la soglia più alta raggiunta in quel giorno.
func (db *DB) MilestoneOn(day string) (threshold int64, ok bool, err error) {
	err = db.QueryRow(`SELECT MAX(threshold) FROM milestones WHERE reached_on = ? HAVING COUNT(*) > 0`, day).Scan(&threshold)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return threshold, err == nil, err
}

func (db *DB) DeleteUserDays(username string) error {
	_, err := db.Exec(`DELETE FROM user_days WHERE username = ?`, strings.ToLower(strings.TrimSpace(username)))
	return err
}

// CleanupStats: giorni di accesso e dati dei ticket prima di daysBefore, clic
// prima di clicksBefore. Il contatore dei traguardi resta com'è.
func (db *DB) CleanupStats(daysBefore, clicksBefore string) error {
	for _, q := range []struct {
		sql string
		arg string
	}{
		{`DELETE FROM user_days WHERE day < ?`, daysBefore},
		{`DELETE FROM app_clicks WHERE day < ?`, clicksBefore},
		{`DELETE FROM ticket_facts WHERE created_at < ?`, daysBefore + "T00:00:00Z"},
	} {
		if _, err := db.Exec(q.sql, q.arg); err != nil {
			return err
		}
	}
	return nil
}
