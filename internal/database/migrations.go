package database

import (
	"database/sql"
	"fmt"
)

// migrations è l'elenco ordinato delle migrazioni: l'indice i porta lo schema
// dalla versione i alla i+1. Mai modificare una migrazione già rilasciata:
// aggiungerne una nuova in coda.
var migrations = []func(*sql.Tx) error{
	migrateV1Schema,
	migrateV2Seed,
	migrateV3CalendarAndSource,
}

func (db *DB) migrate() error {
	version, err := db.SchemaVersion()
	if err != nil {
		return err
	}
	for i := version; i < len(migrations); i++ {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if err := migrations[i](tx); err != nil {
			tx.Rollback()
			return fmt.Errorf("migrazione %d: %w", i+1, err)
		}
		// user_version è transazionale: se il commit fallisce resta la versione precedente.
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func migrateV1Schema(tx *sql.Tx) error {
	_, err := tx.Exec(`
CREATE TABLE categories (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	name       TEXT    NOT NULL UNIQUE COLLATE NOCASE,
	sort_order INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE apps (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	category_id INTEGER NOT NULL REFERENCES categories(id) ON DELETE RESTRICT,
	title       TEXT    NOT NULL,
	description TEXT    NOT NULL DEFAULT '',
	url         TEXT    NOT NULL DEFAULT '',
	icon_kind   TEXT    NOT NULL DEFAULT '',
	icon_value  TEXT    NOT NULL DEFAULT '',
	icon_color  TEXT    NOT NULL DEFAULT '#475569',
	sort_order  INTEGER NOT NULL DEFAULT 0,
	enabled     INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1))
);

CREATE TABLE guides (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	app_id     INTEGER REFERENCES apps(id) ON DELETE SET NULL,
	title      TEXT    NOT NULL,
	kind       TEXT    NOT NULL DEFAULT 'link',
	url        TEXT    NOT NULL DEFAULT '',
	body       TEXT    NOT NULL DEFAULT '',
	sort_order INTEGER NOT NULL DEFAULT 0,
	enabled    INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1))
);

CREATE TABLE alerts (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	title      TEXT    NOT NULL,
	body       TEXT    NOT NULL DEFAULT '',
	level      TEXT    NOT NULL CHECK (level IN ('urgent', 'maintenance', 'news')),
	starts_at  TEXT    NOT NULL,
	ends_at    TEXT,
	notify     INTEGER NOT NULL DEFAULT 0 CHECK (notify IN (0, 1)),
	created_at TEXT    NOT NULL,
	created_by TEXT    NOT NULL DEFAULT ''
);

CREATE INDEX idx_apps_category ON apps(category_id);
CREATE INDEX idx_guides_app    ON guides(app_id);
CREATE INDEX idx_alerts_window ON alerts(starts_at, ends_at);
`)
	return err
}

func migrateV2Seed(tx *sql.Tx) error {
	res, err := tx.Exec(`INSERT INTO categories (name, sort_order) VALUES ('Applicativi', 0)`)
	if err != nil {
		return err
	}
	catID, err := res.LastInsertId()
	if err != nil {
		return err
	}
	_, err = tx.Exec(`
INSERT INTO apps (category_id, title, description, icon_kind, icon_value, icon_color, sort_order) VALUES
	(?, 'Rubrica', 'Contatti e interni', 'pack', 'contacts', '#2563eb', 0),
	(?, 'Webmail', 'Posta elettronica',  'pack', 'mail',     '#0891b2', 1)`,
		catID, catID)
	return err
}

func migrateV3CalendarAndSource(tx *sql.Tx) error {
	_, err := tx.Exec(`
CREATE TABLE calendar_events (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	title       TEXT    NOT NULL,
	kind        TEXT    NOT NULL CHECK (kind IN ('closure', 'event')),
	starts_on   TEXT    NOT NULL,
	ends_on     TEXT    NOT NULL,
	description TEXT    NOT NULL DEFAULT '',
	yearly      INTEGER NOT NULL DEFAULT 0 CHECK (yearly IN (0, 1))
);
CREATE INDEX idx_calendar_range ON calendar_events(starts_on, ends_on);

ALTER TABLE alerts ADD COLUMN source TEXT NOT NULL DEFAULT '';
`)
	return err
}
