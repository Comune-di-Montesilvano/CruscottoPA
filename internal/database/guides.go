package database

import (
	"database/sql"
	"errors"
	"time"
)

// Tipi di guida (validati in Go, non con CHECK).
const (
	GuideKindLink     = "link"     // link esterno (URL)
	GuideKindMarkdown = "markdown" // testo scritto nell'admin (Body)
	GuideKindPDF      = "pdf"      // PDF caricato (File)
	GuideKindGitHub   = "github"   // file .md di GitHub (SourceURL), copia in Body
)

func ValidGuideKind(k string) bool {
	switch k {
	case GuideKindLink, GuideKindMarkdown, GuideKindPDF, GuideKindGitHub:
		return true
	}
	return false
}

// Guide è una guida o FAQ. AppID nil = guida generale.
type Guide struct {
	ID        int64
	AppID     *int64
	Title     string
	Kind      string
	URL       string
	Body      string
	File      string     // PDF in UPLOAD_DIR/guide
	SourceURL string     // URL GitHub inserito dall'admin
	FetchedAt *time.Time // ultimo download riuscito (GitHub)
	// FetchError: errore dell'ultimo download, "" = ok (resta l'ultima copia buona).
	FetchError string
	SortOrder  int
	Enabled    bool
}

const guideCols = `g.id, g.app_id, g.title, g.kind, g.url, g.body, g.file, g.source_url, g.fetched_at, g.fetch_error, g.sort_order, g.enabled`

const guideOrder = `g.sort_order, g.title COLLATE NOCASE, g.id`

func scanGuide(s scanner) (Guide, error) {
	var g Guide
	var appID sql.NullInt64
	var fetched sql.NullString
	err := s.Scan(&g.ID, &appID, &g.Title, &g.Kind, &g.URL, &g.Body, &g.File, &g.SourceURL, &fetched, &g.FetchError, &g.SortOrder, &g.Enabled)
	if err != nil {
		return g, err
	}
	if appID.Valid {
		g.AppID = &appID.Int64
	}
	if fetched.Valid {
		t, err := parseTime(fetched.String)
		if err != nil {
			return g, err
		}
		g.FetchedAt = &t
	}
	return g, nil
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
func (db *DB) CreateGuide(g Guide) (int64, error) { return createGuide(db, g) }

func createGuide(q execer, g Guide) (int64, error) {
	res, err := q.Exec(`
INSERT INTO guides (app_id, title, kind, url, body, file, source_url, enabled, sort_order)
VALUES (?, ?, ?, ?, ?, ?, ?, ?,
	(SELECT COALESCE(MAX(sort_order), -1) + 1 FROM guides WHERE app_id IS ?))`,
		g.AppID, g.Title, g.Kind, g.URL, g.Body, g.File, g.SourceURL, g.Enabled, g.AppID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateGuide: se cambia app, la guida finisce in coda alla nuova.
func (db *DB) UpdateGuide(g Guide) error { return updateGuide(db, g) }

func updateGuide(q execer, g Guide) error {
	return checkAffected(q.Exec(`
UPDATE guides SET
	sort_order = CASE WHEN app_id IS ? THEN sort_order
	                  ELSE (SELECT COALESCE(MAX(sort_order), -1) + 1 FROM guides WHERE app_id IS ?) END,
	app_id = ?, title = ?, kind = ?, url = ?, body = ?, file = ?, source_url = ?, enabled = ?
WHERE id = ?`,
		g.AppID, g.AppID,
		g.AppID, g.Title, g.Kind, g.URL, g.Body, g.File, g.SourceURL, g.Enabled, g.ID))
}

// GetPlanciaGuide: guida abilitata, generale o di un'app visibile in plancia
// (stessa regola di GetDashboard); altrimenti ErrNotFound.
func (db *DB) GetPlanciaGuide(id int64) (Guide, error) {
	g, err := scanGuide(db.QueryRow(`SELECT `+guideCols+` FROM guides g
LEFT JOIN apps a ON a.id = g.app_id
WHERE g.id = ? AND g.enabled = 1 AND (g.app_id IS NULL OR (`+visibleApp+`))`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return g, ErrNotFound
	}
	return g, err
}

// GuidesToRefresh: guide GitHub abilitate mai scaricate o scaricate prima di before.
func (db *DB) GuidesToRefresh(before time.Time) ([]Guide, error) {
	return queryGuides(db, `SELECT `+guideCols+` FROM guides g
WHERE g.kind = 'github' AND g.enabled = 1 AND (g.fetched_at IS NULL OR g.fetched_at < ?)
ORDER BY g.fetched_at IS NOT NULL, g.fetched_at, g.id`, formatTime(before))
}

// SetGuideFetched salva la copia scaricata da sourceURL e azzera l'errore.
// Solo se la guida è ancora GitHub con lo stesso URL (un download in corso
// non sovrascrive una modifica dell'admin); altrimenti ErrNotFound.
func (db *DB) SetGuideFetched(id int64, sourceURL, body string, at time.Time) error {
	return checkAffected(db.Exec(`UPDATE guides SET body = ?, fetched_at = ?, fetch_error = ''
WHERE id = ? AND kind = 'github' AND source_url = ?`, body, formatTime(at), id, sourceURL))
}

// SetGuideFetchError registra un download fallito (stessa condizione di
// SetGuideFetched); il body resta quello di prima.
func (db *DB) SetGuideFetchError(id int64, sourceURL, msg string) error {
	return checkAffected(db.Exec(`UPDATE guides SET fetch_error = ?
WHERE id = ? AND kind = 'github' AND source_url = ?`, msg, id, sourceURL))
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
