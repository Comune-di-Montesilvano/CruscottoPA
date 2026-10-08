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
	migrateV4Branding,
	migrateV5Audience,
	migrateV6Notifications,
	migrateV7Guides,
	migrateV8SupportAndHero,
	migrateV9ReadsAndPresence,
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

func migrateV4Branding(tx *sql.Tx) error {
	_, err := tx.Exec(`
CREATE TABLE branding (
	id         INTEGER PRIMARY KEY CHECK (id = 1),
	ente_name  TEXT NOT NULL DEFAULT '',
	logo_file  TEXT NOT NULL DEFAULT '',
	updated_at TEXT NOT NULL DEFAULT '',
	updated_by TEXT NOT NULL DEFAULT ''
);
INSERT INTO branding (id) VALUES (1);
`)
	return err
}

func migrateV5Audience(tx *sql.Tx) error {
	_, err := tx.Exec(`
CREATE TABLE audience_attributes (
	id       INTEGER PRIMARY KEY AUTOINCREMENT,
	name     TEXT NOT NULL UNIQUE COLLATE NOCASE, -- nome LDAP, es. physicalDeliveryOfficeName
	label    TEXT NOT NULL                        -- es. "Ufficio"
);

CREATE TABLE audience_groups (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	name       TEXT    NOT NULL UNIQUE COLLATE NOCASE,
	sort_order INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE audience_rules (
	id       INTEGER PRIMARY KEY AUTOINCREMENT,
	group_id INTEGER NOT NULL REFERENCES audience_groups(id) ON DELETE CASCADE,
	kind     TEXT    NOT NULL,          -- attr | adgroup | user | exclude (validato in Go)
	attr     TEXT    NOT NULL DEFAULT '', -- solo per kind=attr: nome LDAP
	value    TEXT    NOT NULL,          -- valore dell'attributo, DN del gruppo AD o username
	label    TEXT    NOT NULL DEFAULT '', -- per kind=adgroup: CN da mostrare
	UNIQUE (group_id, kind, attr, value)
);

CREATE TABLE content_audience (
	kind       TEXT    NOT NULL,        -- app | guide | alert
	content_id INTEGER NOT NULL,
	mode       TEXT    NOT NULL,        -- only | hide (uguale per tutte le righe del contenuto)
	group_id   INTEGER NOT NULL REFERENCES audience_groups(id),
	PRIMARY KEY (kind, content_id, group_id)
);
CREATE INDEX idx_content_audience_group ON content_audience(group_id);
`)
	return err
}

func migrateV6Notifications(tx *sql.Tx) error {
	_, err := tx.Exec(`
ALTER TABLE alerts ADD COLUMN notified_at TEXT;
-- Avvisi già attivi: niente notifiche arretrate al primo avvio.
UPDATE alerts SET notified_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
WHERE starts_at <= strftime('%Y-%m-%dT%H:%M:%SZ', 'now');

CREATE TABLE push_subscriptions (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	endpoint   TEXT    NOT NULL UNIQUE,
	p256dh     TEXT    NOT NULL,
	auth       TEXT    NOT NULL,
	username   TEXT    NOT NULL DEFAULT '',
	created_at TEXT    NOT NULL,
	last_ok_at TEXT
);
CREATE INDEX idx_push_username ON push_subscriptions(username);

CREATE TABLE vapid_keys (
	id          INTEGER PRIMARY KEY CHECK (id = 1),
	public_key  TEXT NOT NULL,
	private_key TEXT NOT NULL
);
`)
	return err
}

// migrateV7Guides: guide Markdown, PDF e GitHub (sotto-progetto 2).
func migrateV7Guides(tx *sql.Tx) error {
	for _, q := range []string{
		`ALTER TABLE guides ADD COLUMN file TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE guides ADD COLUMN source_url TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE guides ADD COLUMN fetched_at TEXT`,
		`ALTER TABLE guides ADD COLUMN fetch_error TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := tx.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

// migrateV8SupportAndHero: canali di assistenza collegati agli applicativi e
// attributi AD mostrati sotto il saluto, sfondo del riquadro dell'icona.
func migrateV8SupportAndHero(tx *sql.Tx) error {
	_, err := tx.Exec(`
CREATE TABLE support_channels (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	title      TEXT    NOT NULL,
	url        TEXT    NOT NULL,
	note       TEXT    NOT NULL DEFAULT '',
	sort_order INTEGER NOT NULL DEFAULT 0,
	enabled    INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE app_support (
	app_id     INTEGER NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
	channel_id INTEGER NOT NULL REFERENCES support_channels(id) ON DELETE CASCADE,
	PRIMARY KEY (app_id, channel_id)
);
CREATE INDEX idx_app_support_channel ON app_support(channel_id);
ALTER TABLE audience_attributes ADD COLUMN hero INTEGER NOT NULL DEFAULT 0;
ALTER TABLE audience_attributes ADD COLUMN hero_kind TEXT NOT NULL DEFAULT 'text';
ALTER TABLE apps ADD COLUMN icon_bg TEXT NOT NULL DEFAULT ''; -- sfondo del riquadro icona, '' = bianco
`)
	return err
}

// migrateV9ReadsAndPresence: letture degli avvisi, consegna delle notifiche,
// presenza degli utenti (solo l'ultimo accesso, nessuna cronologia).
func migrateV9ReadsAndPresence(tx *sql.Tx) error {
	_, err := tx.Exec(`
CREATE TABLE alert_reads (
	alert_id INTEGER NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
	username TEXT    NOT NULL,
	read_at  TEXT    NOT NULL,
	how      TEXT    NOT NULL, -- conferma | apertura (validato in Go)
	PRIMARY KEY (alert_id, username)
);
CREATE TABLE alert_deliveries (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	alert_id    INTEGER NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
	username    TEXT    NOT NULL DEFAULT '',
	endpoint    TEXT    NOT NULL,
	service     TEXT    NOT NULL,
	sent_at     TEXT    NOT NULL,
	status      TEXT    NOT NULL, -- inviata | non_riuscita | scaduta
	received_at TEXT,
	UNIQUE (alert_id, endpoint)
);
CREATE INDEX idx_alert_deliveries_alert ON alert_deliveries(alert_id);
CREATE TABLE user_presence (
	username      TEXT PRIMARY KEY,
	name          TEXT NOT NULL DEFAULT '',
	last_seen_at  TEXT NOT NULL,
	last_app_at   TEXT,
	permission    TEXT NOT NULL DEFAULT '',
	permission_at TEXT
);
`)
	return err
}
