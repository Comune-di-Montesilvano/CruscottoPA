package database

import (
	"database/sql"
	"strings"
	"time"
)

const (
	PresenceEvery = 5 * time.Minute     // al massimo una scrittura per utente
	ActiveWindow  = 15 * 24 * time.Hour // utente attivo: accesso negli ultimi 15 giorni
)

// Presence: ultimo stato noto di un utente in plancia (nessuna cronologia).
type Presence struct {
	Username, Name string
	LastSeen       time.Time
	LastApp        *time.Time // ultima apertura come app installata
	Permission     string     // granted | denied | default | ""
	PermissionAt   *time.Time
}

// TouchPresence segna l'accesso; scrive al massimo una volta ogni
// PresenceEvery per utente. Username vuoto (anonimo) ignorato.
func (db *DB) TouchPresence(username, name string, now time.Time) error {
	u := strings.ToLower(strings.TrimSpace(username))
	if u == "" {
		return nil
	}
	_, err := db.Exec(`INSERT INTO user_presence (username, name, last_seen_at) VALUES (?, ?, ?)
ON CONFLICT(username) DO UPDATE SET
	last_seen_at = excluded.last_seen_at,
	name = CASE WHEN excluded.name <> '' THEN excluded.name ELSE user_presence.name END
WHERE user_presence.last_seen_at < ?`, u, strings.TrimSpace(name), formatTime(now), formatTime(now.Add(-PresenceEvery)))
	return err
}

// SetPresenceClient registra ciò che dice il browser (aperta come app,
// permesso delle notifiche). Solo per utenti già presenti.
func (db *DB) SetPresenceClient(username string, app bool, permission string, now time.Time) error {
	u := strings.ToLower(strings.TrimSpace(username))
	if u == "" {
		return nil
	}
	// Il browser lo manda a ogni caricamento: si scrive solo se serve.
	if app {
		if _, err := db.Exec(`UPDATE user_presence SET last_app_at = ?
WHERE username = ? AND (last_app_at IS NULL OR last_app_at < ?)`, formatTime(now), u, formatTime(now.Add(-PresenceEvery))); err != nil {
			return err
		}
	}
	switch permission {
	case "granted", "denied", "default":
		_, err := db.Exec(`UPDATE user_presence SET permission = ?, permission_at = ? WHERE username = ? AND permission <> ?`,
			permission, formatTime(now), u, permission)
		return err
	}
	return nil
}

// DeletePresence toglie un utente (es. non più attivo nel dominio).
func (db *DB) DeletePresence(username string) error {
	_, err := db.Exec(`DELETE FROM user_presence WHERE username = ?`, strings.ToLower(strings.TrimSpace(username)))
	return err
}

func (db *DB) ListPresence() ([]Presence, error) {
	rows, err := db.Query(`SELECT username, name, last_seen_at, last_app_at, permission, permission_at
FROM user_presence ORDER BY last_seen_at DESC, username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Presence{}
	for rows.Next() {
		var p Presence
		var seen string
		var app, permAt sql.NullString
		if err := rows.Scan(&p.Username, &p.Name, &seen, &app, &p.Permission, &permAt); err != nil {
			return nil, err
		}
		p.LastSeen, _ = parseTime(seen)
		p.LastApp = parseNullTime(app)
		p.PermissionAt = parseNullTime(permAt)
		out = append(out, p)
	}
	return out, rows.Err()
}

// ActiveUsernames: utenti con un accesso negli ultimi ActiveWindow.
func (db *DB) ActiveUsernames(now time.Time) ([]string, error) {
	rows, err := db.Query(`SELECT username FROM user_presence WHERE last_seen_at >= ? ORDER BY username`,
		formatTime(now.Add(-ActiveWindow)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func parseNullTime(s sql.NullString) *time.Time {
	if !s.Valid || s.String == "" {
		return nil
	}
	t, err := parseTime(s.String)
	if err != nil {
		return nil
	}
	return &t
}
