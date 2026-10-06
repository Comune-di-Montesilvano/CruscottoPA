// Package database gestisce la persistenza SQLite del portale Intranet:
// gruppi, profili utente, link (App e Guide) e la loro associazione.
package database

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	_ "modernc.org/sqlite" // driver pure-Go (nessun CGO), registra il nome "sqlite"
)

// ErrProfileNotFound viene restituito quando l'identificativo utente
// non corrisponde ad alcun profilo registrato.
var ErrProfileNotFound = errors.New("database: profilo non trovato")

// Group rappresenta un ufficio/area dell'ente (es. "Anagrafe", "Tributi").
type Group struct {
	ID   int64
	Name string
}

// Profile rappresenta un dipendente identificato da username, IP o header del proxy.
// GroupID è nil se il profilo non è ancora stato assegnato ad alcun gruppo.
type Profile struct {
	ID       int64
	Username string
	GroupID  *int64
}

// Link è una card della dashboard: un applicativo (IsGuide=false)
// oppure una guida/FAQ (IsGuide=true).
type Link struct {
	ID          int64
	Title       string
	URL         string
	Icon        string
	Description string
	IsGuide     bool
}

// DashboardData contiene tutto ciò che serve a renderizzare la homepage utente.
// Group è nil se il profilo esiste ma non appartiene ad alcun gruppo.
type DashboardData struct {
	Profile Profile
	Group   *Group
	Apps    []Link
	Guides  []Link
}

// DB incapsula la connessione SQLite; *sql.DB è embedded così che
// il chiamante possa usare direttamente Close(), Ping(), ecc.
type DB struct {
	*sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS groups (
	id   INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT    NOT NULL UNIQUE COLLATE NOCASE
);

CREATE TABLE IF NOT EXISTS profiles (
	id       INTEGER PRIMARY KEY AUTOINCREMENT,
	username TEXT    NOT NULL UNIQUE COLLATE NOCASE,
	group_id INTEGER REFERENCES groups(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS links (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	title       TEXT    NOT NULL,
	url         TEXT    NOT NULL,
	icon        TEXT    NOT NULL DEFAULT '',
	description TEXT    NOT NULL DEFAULT '',
	is_guide    INTEGER NOT NULL DEFAULT 0 CHECK (is_guide IN (0, 1))
);

CREATE TABLE IF NOT EXISTS group_links (
	group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
	link_id  INTEGER NOT NULL REFERENCES links(id)  ON DELETE CASCADE,
	PRIMARY KEY (group_id, link_id)
);

CREATE INDEX IF NOT EXISTS idx_profiles_group_id   ON profiles(group_id);
CREATE INDEX IF NOT EXISTS idx_group_links_link_id ON group_links(link_id);
`

// InitDB apre (o crea) il database SQLite in path e applica lo schema.
// Le PRAGMA sono passate nel DSN così valgono per ogni connessione del pool:
//   - foreign_keys: abilita i vincoli FK (in SQLite sono OFF di default)
//   - journal_mode=WAL: letture concorrenti alle scritture
//   - busy_timeout: attende fino a 5s invece di fallire con SQLITE_BUSY
func InitDB(path string) (*DB, error) {
	dsn := fmt.Sprintf(
		"file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)",
		path,
	)

	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("apertura database: %w", err)
	}

	if err := sqlDB.Ping(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("connessione database: %w", err)
	}

	if _, err := sqlDB.Exec(schema); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("creazione schema: %w", err)
	}

	return &DB{sqlDB}, nil
}

// dashboardQuery parte dal profilo e usa LEFT JOIN a cascata, così da
// ottenere sempre almeno una riga se il profilo esiste, anche quando
// non ha un gruppo o il gruppo non ha link (colonne link a NULL).
const dashboardQuery = `
SELECT
	p.id, p.username, p.group_id,
	g.id, g.name,
	l.id, l.title, l.url, l.icon, l.description, l.is_guide
FROM profiles p
LEFT JOIN groups      g  ON g.id = p.group_id
LEFT JOIN group_links gl ON gl.group_id = g.id
LEFT JOIN links       l  ON l.id = gl.link_id
WHERE p.username = ?
ORDER BY l.is_guide, l.title COLLATE NOCASE
`

// GetDashboardForUser recupera con un'unica query il profilo, il suo gruppo
// e tutti i link visibili a quel gruppo, separandoli tra App e Guide.
// Restituisce ErrProfileNotFound se l'utente non è registrato.
func (db *DB) GetDashboardForUser(username string) (DashboardData, error) {
	data := DashboardData{
		Apps:   []Link{},
		Guides: []Link{},
	}

	rows, err := db.Query(dashboardQuery, strings.TrimSpace(username))
	if err != nil {
		return data, fmt.Errorf("query dashboard: %w", err)
	}
	defer rows.Close()

	found := false
	for rows.Next() {
		var (
			profileGroupID sql.NullInt64
			groupID        sql.NullInt64
			groupName      sql.NullString
			linkID         sql.NullInt64
			title          sql.NullString
			url            sql.NullString
			icon           sql.NullString
			description    sql.NullString
			isGuide        sql.NullBool
		)

		if err := rows.Scan(
			&data.Profile.ID, &data.Profile.Username, &profileGroupID,
			&groupID, &groupName,
			&linkID, &title, &url, &icon, &description, &isGuide,
		); err != nil {
			return data, fmt.Errorf("scan dashboard: %w", err)
		}

		// Profilo e gruppo sono identici su ogni riga: li valorizziamo una volta sola.
		if !found {
			found = true
			if profileGroupID.Valid {
				gid := profileGroupID.Int64
				data.Profile.GroupID = &gid
			}
			if groupID.Valid {
				data.Group = &Group{ID: groupID.Int64, Name: groupName.String}
			}
		}

		// Nessun link associato: la riga contiene solo profilo/gruppo.
		if !linkID.Valid {
			continue
		}

		link := Link{
			ID:          linkID.Int64,
			Title:       title.String,
			URL:         url.String,
			Icon:        icon.String,
			Description: description.String,
			IsGuide:     isGuide.Bool,
		}
		if link.IsGuide {
			data.Guides = append(data.Guides, link)
		} else {
			data.Apps = append(data.Apps, link)
		}
	}

	if err := rows.Err(); err != nil {
		return data, fmt.Errorf("iterazione dashboard: %w", err)
	}
	if !found {
		return data, ErrProfileNotFound
	}

	return data, nil
}
