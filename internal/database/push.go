package database

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// PushSubscription è l'iscrizione Web Push di un browser.
type PushSubscription struct {
	ID        int64
	Endpoint  string
	P256dh    string
	Auth      string
	Username  string // dal cookie utente; "" = anonimo
	CreatedAt time.Time
}

const pushCols = `id, endpoint, p256dh, auth, username, created_at`

func queryPush(db *DB, q string, args ...any) ([]PushSubscription, error) {
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PushSubscription{}
	for rows.Next() {
		var p PushSubscription
		var created string
		if err := rows.Scan(&p.ID, &p.Endpoint, &p.P256dh, &p.Auth, &p.Username, &created); err != nil {
			return nil, err
		}
		if p.CreatedAt, err = parseTime(created); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SavePushSubscription inserisce o aggiorna (stesso endpoint = stesso browser).
func (db *DB) SavePushSubscription(ps PushSubscription) error {
	_, err := db.Exec(`
INSERT INTO push_subscriptions (endpoint, p256dh, auth, username, created_at) VALUES (?, ?, ?, ?, ?)
ON CONFLICT(endpoint) DO UPDATE SET p256dh = excluded.p256dh, auth = excluded.auth, username = excluded.username`,
		ps.Endpoint, ps.P256dh, ps.Auth, strings.ToLower(ps.Username), formatTime(ps.CreatedAt))
	return err
}

func (db *DB) DeletePushSubscription(endpoint string) error {
	_, err := db.Exec(`DELETE FROM push_subscriptions WHERE endpoint = ?`, endpoint)
	return err
}

func (db *DB) ListPushSubscriptions() ([]PushSubscription, error) {
	return queryPush(db, `SELECT `+pushCols+` FROM push_subscriptions ORDER BY id`)
}

func (db *DB) ListPushSubscriptionsFor(username string) ([]PushSubscription, error) {
	return queryPush(db, `SELECT `+pushCols+` FROM push_subscriptions WHERE username = ? ORDER BY id`, strings.ToLower(username))
}

func (db *DB) TouchPushSubscription(endpoint string, at time.Time) error {
	_, err := db.Exec(`UPDATE push_subscriptions SET last_ok_at = ? WHERE endpoint = ?`, formatTime(at), endpoint)
	return err
}

// EnsureVAPIDKeys restituisce le chiavi VAPID, generandole al primo uso.
func (db *DB) EnsureVAPIDKeys(gen func() (privateKey, publicKey string, err error)) (string, string, error) {
	var pub, priv string
	err := db.QueryRow(`SELECT public_key, private_key FROM vapid_keys WHERE id = 1`).Scan(&pub, &priv)
	if err == nil {
		return pub, priv, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", "", err
	}
	if priv, pub, err = gen(); err != nil {
		return "", "", err
	}
	if _, err := db.Exec(`INSERT OR IGNORE INTO vapid_keys (id, public_key, private_key) VALUES (1, ?, ?)`, pub, priv); err != nil {
		return "", "", err
	}
	// Rilettura: se due processi generassero insieme, vince la prima riga salvata.
	err = db.QueryRow(`SELECT public_key, private_key FROM vapid_keys WHERE id = 1`).Scan(&pub, &priv)
	return pub, priv, err
}
