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
	} {
		if _, err := db.Exec(q.sql, q.arg); err != nil {
			return err
		}
	}
	// I segnaposto (creazione non ancora letta) si tengono finché sono tra i
	// chiusi degli ultimi 30 giorni: il job li reinserirebbe a ogni giro.
	_, err := db.Exec(`DELETE FROM ticket_facts WHERE created_at < ? AND (created_at <> ? OR closed_month IS NULL)`,
		daysBefore+"T00:00:00Z", noCreated)
	return err
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

// Dati dei ticket della coda per le statistiche dell'assistenza: solo ID e
// date, nessun contenuto. noCreated = creazione non ancora letta da OTRS.
const noCreated = "1970-01-01T00:00:00Z"

type TicketFact struct {
	TicketID     string
	CreatedAt    time.Time
	FirstReplyAt *time.Time
	ClosedMonth  string
}

// UpsertTicketCreated registra la creazione; non tocca la prima risposta.
func (db *DB) UpsertTicketCreated(id string, created time.Time) error {
	_, err := db.Exec(`INSERT INTO ticket_facts (ticket_id, created_at) VALUES (?, ?)
ON CONFLICT(ticket_id) DO UPDATE SET created_at = excluded.created_at`, id, formatTime(created))
	return err
}

// MissingTicketFacts: ID senza la data di creazione (da leggere in OTRS).
func (db *DB) MissingTicketFacts(ids []string) ([]string, error) {
	known := map[string]bool{}
	rows, err := db.Query(`SELECT ticket_id FROM ticket_facts WHERE created_at <> ?`, noCreated)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		known[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []string{}
	for _, id := range ids {
		if !known[id] {
			out = append(out, id)
		}
	}
	return out, nil
}

// TicketsWithoutReply: ticket aperti, creati da createdFrom, senza prima
// risposta; prima i più recenti (con più di limit in attesa i nuovi non
// restano mai fuori). Un ticket chiuso senza risposta non si rilegge.
func (db *DB) TicketsWithoutReply(createdFrom time.Time, limit int) ([]TicketFact, error) {
	rows, err := db.Query(`SELECT ticket_id, created_at, COALESCE(closed_month, '') FROM ticket_facts
WHERE created_at >= ? AND first_reply_at IS NULL AND closed_month IS NULL ORDER BY created_at DESC, ticket_id LIMIT ?`, formatTime(createdFrom), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TicketFact{}
	for rows.Next() {
		var f TicketFact
		var created string
		if err := rows.Scan(&f.TicketID, &created, &f.ClosedMonth); err != nil {
			return nil, err
		}
		f.CreatedAt, _ = parseTime(created)
		out = append(out, f)
	}
	return out, rows.Err()
}

func (db *DB) SetFirstReply(id string, at time.Time) error {
	_, err := db.Exec(`UPDATE ticket_facts SET first_reply_at = ? WHERE ticket_id = ?`, formatTime(at), id)
	return err
}

// closedMark: valore di closed_month per i ticket chiusi negli ultimi 30 giorni
// (la colonna nella 0.14.0 teneva il mese di chiusura).
const closedMark = "30g"

// SetClosed: i ticket chiusi negli ultimi 30 giorni sono esattamente closedIDs; quelli
// segnati prima e non più in elenco (riaperti) tornano aperti.
func (db *DB) SetClosed(closedIDs []string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE ticket_facts SET closed_month = NULL WHERE closed_month IS NOT NULL`); err != nil {
		return err
	}
	for _, id := range closedIDs {
		if _, err := tx.Exec(`INSERT INTO ticket_facts (ticket_id, created_at, closed_month) VALUES (?, ?, ?)
ON CONFLICT(ticket_id) DO UPDATE SET closed_month = excluded.closed_month`, id, noCreated, closedMark); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ClosedAmong: quanti degli ids risultano chiusi negli ultimi 30 giorni.
func (db *DB) ClosedAmong(ids []string) (int, error) {
	n := 0
	for _, id := range ids {
		var c int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ticket_facts WHERE ticket_id = ? AND closed_month = ?`, id, closedMark).Scan(&c); err != nil {
			return 0, err
		}
		n += c
	}
	return n, nil
}

// FirstReplyDurations: tempi di prima risposta dei ticket creati in [from, to).
func (db *DB) FirstReplyDurations(createdFrom, createdTo time.Time) ([]time.Duration, error) {
	rows, err := db.Query(`SELECT created_at, first_reply_at FROM ticket_facts
WHERE created_at >= ? AND created_at < ? AND first_reply_at IS NOT NULL`, formatTime(createdFrom), formatTime(createdTo))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []time.Duration{}
	for rows.Next() {
		var c, r string
		if err := rows.Scan(&c, &r); err != nil {
			return nil, err
		}
		ct, err1 := parseTime(c)
		rt, err2 := parseTime(r)
		if err1 == nil && err2 == nil && !rt.Before(ct) {
			out = append(out, rt.Sub(ct))
		}
	}
	return out, rows.Err()
}

// TicketStats: numeri degli ultimi 30 giorni dell'assistenza (coda
// configurata), dal job OTRS. Una sola riga (month = closedMark).
type TicketStats struct {
	Requests, Resolved int
	MedianFirst        *time.Duration // nil = nessun ticket con risposta
	UpdatedAt          time.Time
}

func (db *DB) SaveTicketStats(st TicketStats) error {
	var med sql.NullInt64
	if st.MedianFirst != nil {
		med = sql.NullInt64{Int64: int64(*st.MedianFirst / time.Second), Valid: true}
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM ticket_stats WHERE month <> ?`, closedMark); err != nil { // righe per mese della 0.14.0
		return err
	}
	if _, err := tx.Exec(`INSERT INTO ticket_stats (month, requests, resolved, median_first_sec, updated_at) VALUES (?, ?, ?, ?, ?)
ON CONFLICT(month) DO UPDATE SET requests = excluded.requests, resolved = excluded.resolved,
	median_first_sec = excluded.median_first_sec, updated_at = excluded.updated_at`,
		closedMark, st.Requests, st.Resolved, med, formatTime(st.UpdatedAt)); err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) GetTicketStats() (TicketStats, bool, error) {
	var st TicketStats
	var med sql.NullInt64
	var updated string
	err := db.QueryRow(`SELECT requests, resolved, median_first_sec, updated_at FROM ticket_stats WHERE month = ?`, closedMark).
		Scan(&st.Requests, &st.Resolved, &med, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return TicketStats{}, false, nil
	}
	if err != nil {
		return TicketStats{}, false, err
	}
	if med.Valid {
		d := time.Duration(med.Int64) * time.Second
		st.MedianFirst = &d
	}
	st.UpdatedAt, _ = parseTime(updated)
	return st, true, nil
}
