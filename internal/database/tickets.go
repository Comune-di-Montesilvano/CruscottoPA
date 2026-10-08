package database

import (
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
	CreatedAt                      time.Time
}

func (db *DB) RecordTicket(t TicketSent) error {
	_, err := db.Exec(`INSERT INTO tickets_sent
(username, name, email, subject, ticket_id, ticket_number, customer_set, attachments, pc, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		strings.ToLower(strings.TrimSpace(t.Username)), t.Name, t.Email, t.Subject, t.TicketID, t.TicketNumber,
		t.CustomerSet, t.Attachments, t.PC, formatTime(t.CreatedAt))
	return err
}

// CountTicketsSince: ticket aperti da username da since in poi (limite di frequenza).
func (db *DB) CountTicketsSince(username string, since time.Time) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM tickets_sent WHERE username = ? AND created_at >= ?`,
		strings.ToLower(strings.TrimSpace(username)), formatTime(since)).Scan(&n)
	return n, err
}

func (db *DB) ListTickets(limit int) ([]TicketSent, error) {
	rows, err := db.Query(`SELECT id, username, name, email, subject, ticket_id, ticket_number, customer_set, attachments, pc, created_at
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
			&t.CustomerSet, &t.Attachments, &t.PC, &at); err != nil {
			return nil, err
		}
		if t.CreatedAt, err = parseTime(at); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
