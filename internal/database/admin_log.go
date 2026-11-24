package database

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// AdminLogEntry è una riga del registro delle modifiche dell'admin.
type AdminLogEntry struct {
	ID       int64
	At       time.Time
	Username string
	Section  string // etichetta del menu admin, es. "Applicativi"
	Action   string // es. "eliminato"
	ObjectID string // "" se assente
	Title    string // "" se assente
}

// AdminLogFilter: campi vuoti = nessun filtro.
type AdminLogFilter struct {
	Username, Section string
	Limit, Offset     int
}

func (db *DB) AddAdminLog(e AdminLogEntry) error {
	_, err := db.Exec(`INSERT INTO admin_log (at, username, section, action, object_id, title) VALUES (?, ?, ?, ?, ?, ?)`,
		formatTime(e.At), e.Username, e.Section, e.Action, e.ObjectID, e.Title)
	return err
}

// ListAdminLog restituisce le righe dalla più recente; il bool dice se ce ne
// sono altre dopo questa pagina.
func (db *DB) ListAdminLog(f AdminLogFilter) ([]AdminLogEntry, bool, error) {
	var where []string
	var args []any
	if f.Username != "" {
		where = append(where, "username = ?")
		args = append(args, f.Username)
	}
	if f.Section != "" {
		where = append(where, "section = ?")
		args = append(args, f.Section)
	}
	q := `SELECT id, at, username, section, action, object_id, title FROM admin_log`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY at DESC, id DESC LIMIT ? OFFSET ?"
	args = append(args, f.Limit+1, f.Offset)
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := []AdminLogEntry{}
	for rows.Next() {
		var e AdminLogEntry
		var at string
		if err := rows.Scan(&e.ID, &at, &e.Username, &e.Section, &e.Action, &e.ObjectID, &e.Title); err != nil {
			return nil, false, err
		}
		if e.At, err = parseTime(at); err != nil {
			return nil, false, err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	more := len(out) > f.Limit
	if more {
		out = out[:f.Limit]
	}
	return out, more, nil
}

// AdminLogFacets: utenti e sezioni presenti nel registro, ordinati (per i filtri).
func (db *DB) AdminLogFacets() (users, sections []string, err error) {
	if users, err = db.distinctAdminLog("username"); err != nil {
		return nil, nil, err
	}
	sections, err = db.distinctAdminLog("section")
	return users, sections, err
}

func (db *DB) distinctAdminLog(col string) ([]string, error) {
	rows, err := db.Query(`SELECT DISTINCT ` + col + ` FROM admin_log ORDER BY ` + col)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// CleanupAdminLog cancella le righe più vecchie di before.
func (db *DB) CleanupAdminLog(before time.Time) error {
	_, err := db.Exec(`DELETE FROM admin_log WHERE at < ?`, formatTime(before))
	return err
}

// WritePendingAdminLog salva una riga in un file accanto al DB: il ripristino
// di un backup sostituisce il DB e con lui il registro, la riga del ripristino
// si reinserisce all'avvio con ImportPendingAdminLog.
func WritePendingAdminLog(path string, e AdminLogEntry) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ImportPendingAdminLog inserisce la riga scritta da WritePendingAdminLog e
// cancella il file. File assente = nulla da fare; file illeggibile = rinominato
// in .bad (così non si riprova a ogni avvio) ed errore.
func (db *DB) ImportPendingAdminLog(path string) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	var e AdminLogEntry
	if err := json.Unmarshal(b, &e); err != nil {
		os.Rename(path, path+".bad")
		return fmt.Errorf("riga del registro in sospeso illeggibile: %w", err)
	}
	if err := db.AddAdminLog(e); err != nil {
		return err
	}
	return os.Remove(path)
}
