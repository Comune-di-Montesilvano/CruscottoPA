package database

import (
	"database/sql"
	"errors"
)

// GuideKindLink è l'unico tipo gestito nel sotto-progetto 1;
// 'markdown' e 'github' arrivano col sotto-progetto 2.
const GuideKindLink = "link"

// Guide è una guida o FAQ. AppID nil = guida generale.
type Guide struct {
	ID        int64
	AppID     *int64
	Title     string
	Kind      string
	URL       string
	Body      string
	SortOrder int
	Enabled   bool
}

const guideCols = `g.id, g.app_id, g.title, g.kind, g.url, g.body, g.sort_order, g.enabled`

const guideOrder = `g.sort_order, g.title COLLATE NOCASE, g.id`

func scanGuide(s scanner) (Guide, error) {
	var g Guide
	var appID sql.NullInt64
	err := s.Scan(&g.ID, &appID, &g.Title, &g.Kind, &g.URL, &g.Body, &g.SortOrder, &g.Enabled)
	if appID.Valid {
		g.AppID = &appID.Int64
	}
	return g, err
}

func queryGuides(db *DB, query string, args ...any) ([]Guide, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Guide{}
	for rows.Next() {
		g, err := scanGuide(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (db *DB) ListGuides() ([]Guide, error) {
	return queryGuides(db, `SELECT `+guideCols+` FROM guides g
ORDER BY g.app_id IS NOT NULL, g.app_id, `+guideOrder)
}

func (db *DB) GetGuide(id int64) (Guide, error) {
	g, err := scanGuide(db.QueryRow(`SELECT `+guideCols+` FROM guides g WHERE g.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return g, ErrNotFound
	}
	return g, err
}

// "app_id IS ?" confronta correttamente anche NULL (guide generali).
func (db *DB) CreateGuide(g Guide) (int64, error) {
	res, err := db.Exec(`
INSERT INTO guides (app_id, title, kind, url, body, enabled, sort_order)
VALUES (?, ?, ?, ?, ?, ?,
	(SELECT COALESCE(MAX(sort_order), -1) + 1 FROM guides WHERE app_id IS ?))`,
		g.AppID, g.Title, g.Kind, g.URL, g.Body, g.Enabled, g.AppID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateGuide: se cambia app, la guida finisce in coda alla nuova.
func (db *DB) UpdateGuide(g Guide) error {
	return checkAffected(db.Exec(`
UPDATE guides SET
	sort_order = CASE WHEN app_id IS ? THEN sort_order
	                  ELSE (SELECT COALESCE(MAX(sort_order), -1) + 1 FROM guides WHERE app_id IS ?) END,
	app_id = ?, title = ?, kind = ?, url = ?, body = ?, enabled = ?
WHERE id = ?`,
		g.AppID, g.AppID,
		g.AppID, g.Title, g.Kind, g.URL, g.Body, g.Enabled, g.ID))
}

func (db *DB) DeleteGuide(id int64) error {
	return db.deleteContent(ContentGuide, "guides", id)
}

func (db *DB) MoveGuide(id int64, dir int) error {
	g, err := db.GetGuide(id)
	if err != nil {
		return err
	}
	return db.moveRow("guides", "app_id IS ?", []any{g.AppID},
		`sort_order, title COLLATE NOCASE, id`, id, dir)
}

// GuideCountsByApp restituisce quante guide (anche disabilitate) ha ogni app.
func (db *DB) GuideCountsByApp() (map[int64]int, error) {
	rows, err := db.Query(`SELECT app_id, COUNT(*) FROM guides WHERE app_id IS NOT NULL GROUP BY app_id`)
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
