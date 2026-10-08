package database

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// TicketSent: un ticket aperto dalla plancia. Identità dichiarata (NTLM).
type TicketSent struct {
	ID                             int64
	Username, Name, Email, Subject string
	TicketID, TicketNumber         string
	CustomerSet                    bool
	Attachments                    int
	PC                             string
	Kind                           string // TicketOpen | TicketReply
	CreatedAt                      time.Time
}

func (db *DB) RecordTicket(t TicketSent) error {
	kind := t.Kind
	if kind == "" {
		kind = TicketOpen
	}
	_, err := db.Exec(`INSERT INTO tickets_sent
(username, name, email, subject, ticket_id, ticket_number, customer_set, attachments, pc, kind, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		strings.ToLower(strings.TrimSpace(t.Username)), t.Name, t.Email, t.Subject, t.TicketID, t.TicketNumber,
		t.CustomerSet, t.Attachments, t.PC, kind, formatTime(t.CreatedAt))
	return err
}

// CountTicketsSince: aperture o risposte di username da since in poi (limite di frequenza).
func (db *DB) CountTicketsSince(username, kind string, since time.Time) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM tickets_sent WHERE username = ? AND kind = ? AND created_at >= ?`,
		strings.ToLower(strings.TrimSpace(username)), kind, formatTime(since)).Scan(&n)
	return n, err
}

func (db *DB) ListTickets(limit int) ([]TicketSent, error) {
	rows, err := db.Query(`SELECT id, username, name, email, subject, ticket_id, ticket_number, customer_set, attachments, pc, kind, created_at
FROM tickets_sent ORDER BY created_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TicketSent{}
	for rows.Next() {
		var t TicketSent
		var at string
		if err := rows.Scan(&t.ID, &t.Username, &t.Name, &t.Email, &t.Subject, &t.TicketID, &t.TicketNumber,
			&t.CustomerSet, &t.Attachments, &t.PC, &t.Kind, &at); err != nil {
			return nil, err
		}
		if t.CreatedAt, err = parseTime(at); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

const (
	TicketOpen           = "apertura"
	TicketReply          = "risposta"
	ticketStateRetention = 30 * 24 * time.Hour
)

func (db *DB) MarkTicketSeen(username, ticketID string, now time.Time) error {
	_, err := db.Exec(`INSERT INTO ticket_seen (username, ticket_id, seen_at) VALUES (?, ?, ?)
ON CONFLICT(username, ticket_id) DO UPDATE SET seen_at = excluded.seen_at`,
		strings.ToLower(strings.TrimSpace(username)), ticketID, formatTime(now))
	return err
}

func (db *DB) TicketSeen(username string) (map[string]time.Time, error) {
	rows, err := db.Query(`SELECT ticket_id, seen_at FROM ticket_seen WHERE username = ?`, strings.ToLower(strings.TrimSpace(username)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var id, at string
		if err := rows.Scan(&id, &at); err != nil {
			return nil, err
		}
		if out[id], err = parseTime(at); err != nil {
			return nil, err
		}
	}
	return out, rows.Err()
}

func (db *DB) TicketNotifyState(ticketID string) (int64, bool, error) {
	var last int64
	err := db.QueryRow(`SELECT last_article_id FROM ticket_notify_state WHERE ticket_id = ?`, ticketID).Scan(&last)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return last, err == nil, err
}

func (db *DB) SetTicketNotifyState(ticketID string, last int64, now time.Time) error {
	_, err := db.Exec(`INSERT INTO ticket_notify_state (ticket_id, last_article_id, updated_at) VALUES (?, ?, ?)
ON CONFLICT(ticket_id) DO UPDATE SET last_article_id = excluded.last_article_id, updated_at = excluded.updated_at`,
		ticketID, last, formatTime(now))
	return err
}

func (db *DB) UpsertTicketUser(email, username string, now time.Time) error {
	_, err := db.Exec(`INSERT INTO ticket_users (email, username, updated_at) VALUES (?, ?, ?)
ON CONFLICT(email) DO UPDATE SET username = excluded.username, updated_at = excluded.updated_at`,
		strings.ToLower(strings.TrimSpace(email)), strings.ToLower(strings.TrimSpace(username)), formatTime(now))
	return err
}

func (db *DB) TicketUserFor(email string) (string, error) {
	var u string
	err := db.QueryRow(`SELECT username FROM ticket_users WHERE email = ?`, strings.ToLower(strings.TrimSpace(email))).Scan(&u)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return u, err
}

// CleanupTicketState: via lo stato locale fermo da più di 30 giorni.
func (db *DB) CleanupTicketState(now time.Time) error {
	old := formatTime(now.Add(-ticketStateRetention))
	for _, q := range []string{
		`DELETE FROM ticket_seen WHERE seen_at < ?`,
		`DELETE FROM ticket_notify_state WHERE updated_at < ?`,
		`DELETE FROM ticket_users WHERE updated_at < ?`,
	} {
		if _, err := db.Exec(q, old); err != nil {
			return err
		}
	}
	return nil
}
