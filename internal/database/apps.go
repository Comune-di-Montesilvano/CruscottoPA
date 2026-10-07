package database

import (
	"database/sql"
	"errors"
)

// Valori di apps.icon_kind (validati in Go, non con CHECK: i sotto-progetti
// successivi possono aggiungerne senza ricreare la tabella).
const (
	IconMonogram = ""
	IconPack     = "pack"
	IconUpload   = "upload"
	IconURL      = "url"
)

// App è una card della plancia che apre un applicativo.
type App struct {
	ID          int64
	CategoryID  int64
	Title       string
	Description string
	URL         string
	IconKind    string
	IconValue   string
	IconColor   string
	SortOrder   int
	Enabled     bool
}

const appCols = `a.id, a.category_id, a.title, a.description, a.url,
	a.icon_kind, a.icon_value, a.icon_color, a.sort_order, a.enabled`

const appOrder = `a.sort_order, a.title COLLATE NOCASE, a.id`

type scanner interface{ Scan(dest ...any) error }

// scanApp legge le colonne appCols; extra sono destinazioni che le precedono nella SELECT.
func scanApp(s scanner, extra ...any) (App, error) {
	var a App
	dest := append(extra, &a.ID, &a.CategoryID, &a.Title, &a.Description, &a.URL,
		&a.IconKind, &a.IconValue, &a.IconColor, &a.SortOrder, &a.Enabled)
	err := s.Scan(dest...)
	return a, err
}

func (db *DB) ListApps() ([]App, error) {
	rows, err := db.Query(`SELECT ` + appCols + ` FROM apps a
JOIN categories c ON c.id = a.category_id
ORDER BY c.sort_order, c.name COLLATE NOCASE, ` + appOrder)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []App{}
	for rows.Next() {
		a, err := scanApp(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (db *DB) GetApp(id int64) (App, error) {
	a, err := scanApp(db.QueryRow(`SELECT `+appCols+` FROM apps a WHERE a.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

func (db *DB) CreateApp(a App) (int64, error) {
	res, err := db.Exec(`
INSERT INTO apps (category_id, title, description, url, icon_kind, icon_value, icon_color, enabled, sort_order)
VALUES (?, ?, ?, ?, ?, ?, ?, ?,
	(SELECT COALESCE(MAX(sort_order), -1) + 1 FROM apps WHERE category_id = ?))`,
		a.CategoryID, a.Title, a.Description, a.URL, a.IconKind, a.IconValue, a.IconColor, a.Enabled, a.CategoryID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateApp aggiorna tutti i campi tranne sort_order; se cambia categoria
// l'app finisce in coda alla nuova (in UPDATE le espressioni vedono i valori vecchi).
func (db *DB) UpdateApp(a App) error {
	return checkAffected(db.Exec(`
UPDATE apps SET
	sort_order  = CASE WHEN category_id = ? THEN sort_order
	                   ELSE (SELECT COALESCE(MAX(sort_order), -1) + 1 FROM apps WHERE category_id = ?) END,
	category_id = ?, title = ?, description = ?, url = ?,
	icon_kind = ?, icon_value = ?, icon_color = ?, enabled = ?
WHERE id = ?`,
		a.CategoryID, a.CategoryID,
		a.CategoryID, a.Title, a.Description, a.URL,
		a.IconKind, a.IconValue, a.IconColor, a.Enabled, a.ID))
}

func (db *DB) DeleteApp(id int64) error {
	return db.deleteContent(ContentApp, "apps", id)
}

func (db *DB) MoveApp(id int64, dir int) error {
	a, err := db.GetApp(id)
	if err != nil {
		return err
	}
	return db.moveRow("apps", "category_id = ?", []any{a.CategoryID},
		`sort_order, title COLLATE NOCASE, id`, id, dir)
}
