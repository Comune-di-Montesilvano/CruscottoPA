package database

import (
	"database/sql"
	"errors"
	"strings"
	"time"
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

// AddAppClick conta un clic aggregato (nessun utente). App inesistente = errore FK.
func (db *DB) AddAppClick(appID int64, day string) error {
	_, err := db.Exec(`INSERT INTO app_clicks (app_id, day, clicks) VALUES (?, ?, 1)
ON CONFLICT(app_id, day) DO UPDATE SET clicks = clicks + 1`, appID, day)
	return err
}

// AppClicksSince: clic per app da fromDay in poi.
func (db *DB) AppClicksSince(fromDay string) (map[int64]int, error) {
	rows, err := db.Query(`SELECT app_id, SUM(clicks) FROM app_clicks WHERE day >= ? GROUP BY app_id`, fromDay)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

func (db *DB) HasPushSubscription(username string) (bool, error) {
	u := strings.ToLower(strings.TrimSpace(username))
	if u == "" {
		return false, nil
	}
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM push_subscriptions WHERE username = ?`, u).Scan(&n)
	return n > 0, err
}

// TicketsOpenedBetween: ticket aperti dall'utente dalla plancia in [from, to).
func (db *DB) TicketsOpenedBetween(username string, from, to time.Time) ([]string, error) {
	rows, err := db.Query(`SELECT ticket_id FROM tickets_sent WHERE username = ? AND kind = ? AND created_at >= ? AND created_at < ?
ORDER BY created_at`, strings.ToLower(strings.TrimSpace(username)), TicketOpen, formatTime(from), formatTime(to))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (db *DB) HasOpenedTicket(username string) (bool, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM tickets_sent WHERE username = ? AND kind = ?`,
		strings.ToLower(strings.TrimSpace(username)), TicketOpen).Scan(&n)
	return n > 0, err
}
