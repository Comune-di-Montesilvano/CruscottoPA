package database

import (
	"database/sql"
	"strings"
)

// Preferiti e ordine personale della plancia, per utente. Delle aperture si
// salva il numero per giorno: mai l'orario.

func layoutUser(username string) string { return strings.ToLower(strings.TrimSpace(username)) }

// AddAppOpen conta un'apertura dell'app nel giorno day (YYYY-MM-DD). App
// inesistente = errore FK.
func (db *DB) AddAppOpen(username string, appID int64, day string) error {
	_, err := db.Exec(`INSERT INTO user_app_opens (username, app_id, day, opens) VALUES (?, ?, ?, 1)
ON CONFLICT(username, app_id, day) DO UPDATE SET opens = opens + 1`, layoutUser(username), appID, day)
	return err
}

// AppOpens: aperture per app dell'utente da fromDay in poi.
func (db *DB) AppOpens(username, fromDay string) (map[int64]int, error) {
	rows, err := db.Query(`SELECT app_id, SUM(opens) FROM user_app_opens WHERE username = ? AND day >= ? GROUP BY app_id`,
		layoutUser(username), fromDay)
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

// Layout: fissati in ordine; posizione personale di categorie e app (id → posizione).
type Layout struct {
	Favorites  []int64
	Categories map[int64]int
	Apps       map[int64]int
}

func (db *DB) UserLayout(username string) (Layout, error) {
	u := layoutUser(username)
	l := Layout{Favorites: []int64{}, Categories: map[int64]int{}, Apps: map[int64]int{}}
	rows, err := db.Query(`SELECT app_id FROM user_favorites WHERE username = ? ORDER BY position`, u)
	if err != nil {
		return l, err
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return l, err
		}
		l.Favorites = append(l.Favorites, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return l, err
	}
	for _, q := range []struct {
		sql string
		m   map[int64]int
	}{
		{`SELECT category_id, position FROM user_category_order WHERE username = ?`, l.Categories},
		{`SELECT app_id, position FROM user_app_order WHERE username = ?`, l.Apps},
	} {
		if err := scanPositions(db, q.sql, u, q.m); err != nil {
			return l, err
		}
	}
	return l, nil
}

func scanPositions(db *DB, query, u string, m map[int64]int) error {
	rows, err := db.Query(query, u)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var pos int
		if err := rows.Scan(&id, &pos); err != nil {
			return err
		}
		m[id] = pos
	}
	return rows.Err()
}

// insertPositions scrive ids con posizione 0..n-1.
func insertPositions(tx *sql.Tx, query, u string, ids []int64) error {
	for i, id := range ids {
		if _, err := tx.Exec(query, u, id, i); err != nil {
			return err
		}
	}
	return nil
}

// SaveFavorites sostituisce i fissati dell'utente.
func (db *DB) SaveFavorites(username string, ids []int64) error {
	u := layoutUser(username)
	return db.inTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM user_favorites WHERE username = ?`, u); err != nil {
			return err
		}
		return insertPositions(tx, `INSERT INTO user_favorites (username, app_id, position) VALUES (?, ?, ?)`, u, ids)
	})
}

// SaveCategoryOrder sostituisce l'ordine personale delle categorie.
func (db *DB) SaveCategoryOrder(username string, ids []int64) error {
	u := layoutUser(username)
	return db.inTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM user_category_order WHERE username = ?`, u); err != nil {
			return err
		}
		return insertPositions(tx, `INSERT INTO user_category_order (username, category_id, position) VALUES (?, ?, ?)`, u, ids)
	})
}

// SaveAppOrder sostituisce l'ordine personale delle app che ora stanno in categoryID.
func (db *DB) SaveAppOrder(username string, categoryID int64, ids []int64) error {
	u := layoutUser(username)
	return db.inTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM user_app_order WHERE username = ? AND app_id IN (SELECT id FROM apps WHERE category_id = ?)`, u, categoryID); err != nil {
			return err
		}
		return insertPositions(tx, `INSERT INTO user_app_order (username, app_id, position) VALUES (?, ?, ?)`, u, ids)
	})
}

// CleanupAppOpens: aperture di tutti prima di day.
func (db *DB) CleanupAppOpens(day string) error {
	_, err := db.Exec(`DELETE FROM user_app_opens WHERE day < ?`, day)
	return err
}

// ResetLayout toglie fissati e ordine personale; le aperture restano.
func (db *DB) ResetLayout(username string) error {
	return db.deleteLayout(layoutUser(username), "user_favorites", "user_category_order", "user_app_order")
}

// DeleteUserLayout toglie tutto, aperture comprese (utente uscito da AD).
func (db *DB) DeleteUserLayout(username string) error {
	return db.deleteLayout(layoutUser(username), "user_favorites", "user_category_order", "user_app_order", "user_app_opens")
}

func (db *DB) deleteLayout(u string, tables ...string) error {
	return db.inTx(func(tx *sql.Tx) error {
		for _, t := range tables {
			if _, err := tx.Exec(`DELETE FROM `+t+` WHERE username = ?`, u); err != nil {
				return err
			}
		}
		return nil
	})
}
