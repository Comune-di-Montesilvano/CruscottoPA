package database

import (
	"fmt"
	"slices"
)

// moveRow sposta la riga id di una posizione (dir -1 su, +1 giù) all'interno
// dell'insieme selezionato da where, poi rinumera sort_order 0..n-1.
// table/where/orderBy sono costanti del package, mai input utente.
func (db *DB) moveRow(table, where string, args []any, orderBy string, id int64, dir int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.Query(fmt.Sprintf(`SELECT id FROM %s WHERE %s ORDER BY %s`, table, where, orderBy), args...)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var rid int64
		if err := rows.Scan(&rid); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, rid)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	i := slices.Index(ids, id)
	if i < 0 {
		return ErrNotFound
	}
	j := i + dir
	if j < 0 || j >= len(ids) {
		return nil // già al bordo
	}
	ids[i], ids[j] = ids[j], ids[i]
	for pos, rid := range ids {
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s SET sort_order = ? WHERE id = ?`, table), pos, rid); err != nil {
			return err
		}
	}
	return tx.Commit()
}
