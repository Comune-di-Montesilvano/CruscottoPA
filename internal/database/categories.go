package database

import (
	"database/sql"
	"errors"
)

// Category raggruppa le app in plancia (es. "Applicativi", "Gestionali esterni").
type Category struct {
	ID        int64
	Name      string
	SortOrder int
}

const categoryOrder = `sort_order, name COLLATE NOCASE, id`

func (db *DB) ListCategories() ([]Category, error) {
	rows, err := db.Query(`SELECT id, name, sort_order FROM categories ORDER BY ` + categoryOrder)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Category{}
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.ID, &c.Name, &c.SortOrder); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (db *DB) GetCategory(id int64) (Category, error) {
	var c Category
	err := db.QueryRow(`SELECT id, name, sort_order FROM categories WHERE id = ?`, id).
		Scan(&c.ID, &c.Name, &c.SortOrder)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

func (db *DB) CreateCategory(name string) (int64, error) {
	res, err := db.Exec(`
INSERT INTO categories (name, sort_order)
VALUES (?, (SELECT COALESCE(MAX(sort_order), -1) + 1 FROM categories))`, name)
	if isUniqueViolation(err) {
		return 0, ErrDuplicate
	}
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (db *DB) UpdateCategory(id int64, name string) error {
	res, err := db.Exec(`UPDATE categories SET name = ? WHERE id = ?`, name, id)
	if isUniqueViolation(err) {
		return ErrDuplicate
	}
	return checkAffected(res, err)
}

func (db *DB) DeleteCategory(id int64) error {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM apps WHERE category_id = ?`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrCategoryNotEmpty
	}
	return checkAffected(db.Exec(`DELETE FROM categories WHERE id = ?`, id))
}

func (db *DB) MoveCategory(id int64, dir int) error {
	return db.moveRow("categories", "1 = 1", nil, categoryOrder, id, dir)
}
