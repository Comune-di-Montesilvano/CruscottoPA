package database

import (
	"database/sql"
	"strings"
	"time"
)

// Letture degli avvisi e consegne delle notifiche. Dati indicativi
// (identità dichiarata); spariscono con l'avviso (ON DELETE CASCADE).

const (
	ReadConfirm = "conferma" // «Ho letto» sul popup urgente
	ReadOpen    = "apertura" // testo completo aperto (carosello o pagina dell'avviso)

	DeliverySent   = "inviata"
	DeliveryFailed = "non_riuscita"
	DeliveryGone   = "scaduta" // iscrizione non più valida (404/410)
)

type AlertRead struct {
	Username string
	ReadAt   time.Time
	How      string
}

type Delivery struct {
	Username, Endpoint, Service, Status string
	SentAt                              time.Time
	ReceivedAt                          *time.Time
}

type DeliveryCount struct{ Sent, Received int }

// MarkRead registra la prima lettura; le successive sono ignorate.
func (db *DB) MarkRead(alertID int64, username, how string, now time.Time) error {
	u := strings.ToLower(strings.TrimSpace(username))
	if u == "" || (how != ReadConfirm && how != ReadOpen) {
		return nil
	}
	_, err := db.Exec(`INSERT OR IGNORE INTO alert_reads (alert_id, username, read_at, how) VALUES (?, ?, ?, ?)`,
		alertID, u, formatTime(now), how)
	return err
}

func (db *DB) ReadsFor(alertID int64) ([]AlertRead, error) {
	rows, err := db.Query(`SELECT username, read_at, how FROM alert_reads WHERE alert_id = ? ORDER BY read_at, username`, alertID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AlertRead{}
	for rows.Next() {
		var r AlertRead
		var at string
		if err := rows.Scan(&r.Username, &at, &r.How); err != nil {
			return nil, err
		}
		r.ReadAt, _ = parseTime(at)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (db *DB) ReadCounts() (map[int64]int, error) {
	rows, err := db.Query(`SELECT alert_id, COUNT(*) FROM alert_reads GROUP BY alert_id`)
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

// RecordDelivery: esito dell'invio per (avviso, iscrizione). Un nuovo invio
// aggiorna la riga e azzera la ricevuta.
func (db *DB) RecordDelivery(alertID int64, username, endpoint, service, status string, now time.Time) error {
	_, err := db.Exec(`INSERT INTO alert_deliveries (alert_id, username, endpoint, service, sent_at, status) VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(alert_id, endpoint) DO UPDATE SET username = excluded.username, service = excluded.service,
	sent_at = excluded.sent_at, status = excluded.status, received_at = NULL`,
		alertID, strings.ToLower(username), endpoint, service, formatTime(now), status)
	return err
}

// MarkReceived: il service worker ha mostrato la notifica. Solo per consegne
// registrate; resta la prima ricevuta.
func (db *DB) MarkReceived(alertID int64, endpoint string, now time.Time) error {
	_, err := db.Exec(`UPDATE alert_deliveries SET received_at = ? WHERE alert_id = ? AND endpoint = ? AND received_at IS NULL`,
		formatTime(now), alertID, endpoint)
	return err
}

func (db *DB) DeliveriesFor(alertID int64) ([]Delivery, error) {
	rows, err := db.Query(`SELECT username, endpoint, service, status, sent_at, received_at FROM alert_deliveries
WHERE alert_id = ? ORDER BY username, service, id`, alertID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Delivery{}
	for rows.Next() {
		var d Delivery
		var sent string
		var rec sql.NullString
		if err := rows.Scan(&d.Username, &d.Endpoint, &d.Service, &d.Status, &sent, &rec); err != nil {
			return nil, err
		}
		d.SentAt, _ = parseTime(sent)
		d.ReceivedAt = parseNullTime(rec)
		out = append(out, d)
	}
	return out, rows.Err()
}

func (db *DB) DeliveryCounts() (map[int64]DeliveryCount, error) {
	rows, err := db.Query(`SELECT alert_id,
	SUM(CASE WHEN status = 'inviata' THEN 1 ELSE 0 END),
	SUM(CASE WHEN received_at IS NOT NULL THEN 1 ELSE 0 END)
FROM alert_deliveries GROUP BY alert_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]DeliveryCount{}
	for rows.Next() {
		var id int64
		var c DeliveryCount
		if err := rows.Scan(&id, &c.Sent, &c.Received); err != nil {
			return nil, err
		}
		out[id] = c
	}
	return out, rows.Err()
}

func (db *DB) LastReceivedByUser() (map[string]time.Time, error) {
	rows, err := db.Query(`SELECT username, MAX(received_at) FROM alert_deliveries
WHERE received_at IS NOT NULL AND username <> '' GROUP BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var u, at string
		if err := rows.Scan(&u, &at); err != nil {
			return nil, err
		}
		if t, err := parseTime(at); err == nil {
			out[u] = t
		}
	}
	return out, rows.Err()
}
