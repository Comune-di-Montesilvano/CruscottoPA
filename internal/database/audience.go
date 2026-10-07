package database

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
)

// ErrInUse: elemento referenziato altrove (gruppo usato da contenuti,
// attributo usato da regole).
var ErrInUse = errors.New("database: elemento in uso")

type AudienceAttribute struct {
	ID          int64
	Name, Label string
}

func (db *DB) ListAudienceAttributes() ([]AudienceAttribute, error) {
	rows, err := db.Query(`SELECT id, name, label FROM audience_attributes ORDER BY label COLLATE NOCASE, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AudienceAttribute{}
	for rows.Next() {
		var a AudienceAttribute
		if err := rows.Scan(&a.ID, &a.Name, &a.Label); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (db *DB) CreateAudienceAttribute(name, label string) (int64, error) {
	res, err := db.Exec(`INSERT INTO audience_attributes (name, label) VALUES (?, ?)`, strings.TrimSpace(name), strings.TrimSpace(label))
	if isUniqueViolation(err) {
		return 0, ErrDuplicate
	}
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (db *DB) DeleteAudienceAttribute(id int64) error {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audience_rules r JOIN audience_attributes a ON a.name = r.attr COLLATE NOCASE
WHERE a.id = ? AND r.kind = 'attr'`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrInUse
	}
	return checkAffected(db.Exec(`DELETE FROM audience_attributes WHERE id = ?`, id))
}

// AudienceGroup è un gruppo della plancia; Rules e Uses sono conteggi.
type AudienceGroup struct {
	ID        int64
	Name      string
	SortOrder int
	Rules     int // regole del gruppo
	Uses      int // contenuti che lo usano
}

const audienceGroupCols = `g.id, g.name, g.sort_order,
	(SELECT COUNT(*) FROM audience_rules r WHERE r.group_id = g.id),
	(SELECT COUNT(DISTINCT kind || ':' || content_id) FROM content_audience c WHERE c.group_id = g.id)`

func scanAudienceGroup(s scanner) (AudienceGroup, error) {
	var g AudienceGroup
	err := s.Scan(&g.ID, &g.Name, &g.SortOrder, &g.Rules, &g.Uses)
	return g, err
}

func (db *DB) ListAudienceGroups() ([]AudienceGroup, error) {
	rows, err := db.Query(`SELECT ` + audienceGroupCols + ` FROM audience_groups g ORDER BY g.sort_order, g.name COLLATE NOCASE, g.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AudienceGroup{}
	for rows.Next() {
		g, err := scanAudienceGroup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (db *DB) GetAudienceGroup(id int64) (AudienceGroup, error) {
	g, err := scanAudienceGroup(db.QueryRow(`SELECT `+audienceGroupCols+` FROM audience_groups g WHERE g.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return g, ErrNotFound
	}
	return g, err
}

func (db *DB) CreateAudienceGroup(name string) (int64, error) {
	res, err := db.Exec(`INSERT INTO audience_groups (name, sort_order)
VALUES (?, (SELECT COALESCE(MAX(sort_order), -1) + 1 FROM audience_groups))`, strings.TrimSpace(name))
	if isUniqueViolation(err) {
		return 0, ErrDuplicate
	}
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (db *DB) RenameAudienceGroup(id int64, name string) error {
	err := checkAffected(db.Exec(`UPDATE audience_groups SET name = ? WHERE id = ?`, strings.TrimSpace(name), id))
	if isUniqueViolation(err) {
		return ErrDuplicate
	}
	return err
}

func (db *DB) MoveAudienceGroup(id int64, dir int) error {
	return db.moveRow("audience_groups", "1 = ?", []any{1}, `sort_order, name COLLATE NOCASE, id`, id, dir)
}

// DeleteAudienceGroup: vietato se il gruppo è usato da qualche contenuto,
// altrimenti un contenuto "Riservato a" diventerebbe pubblico.
func (db *DB) DeleteAudienceGroup(id int64) error {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM content_audience WHERE group_id = ?`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrInUse
	}
	return checkAffected(db.Exec(`DELETE FROM audience_groups WHERE id = ?`, id))
}

type AudienceRule struct {
	ID, GroupID              int64
	Kind, Attr, Value, Label string
}

func (db *DB) ListAudienceRules(groupID int64) ([]AudienceRule, error) {
	rows, err := db.Query(`SELECT id, group_id, kind, attr, value, label FROM audience_rules WHERE group_id = ?
ORDER BY CASE kind WHEN 'exclude' THEN 1 ELSE 0 END, kind, attr, value COLLATE NOCASE`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AudienceRule{}
	for rows.Next() {
		var r AudienceRule
		if err := rows.Scan(&r.ID, &r.GroupID, &r.Kind, &r.Attr, &r.Value, &r.Label); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AllAudienceRules: regole di tutti i gruppi, per la valutazione in plancia.
func (db *DB) AllAudienceRules() (map[int64][]audience.Rule, error) {
	rows, err := db.Query(`SELECT group_id, kind, attr, value FROM audience_rules ORDER BY group_id, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]audience.Rule{}
	for rows.Next() {
		var g int64
		var r audience.Rule
		if err := rows.Scan(&g, &r.Kind, &r.Attr, &r.Value); err != nil {
			return nil, err
		}
		out[g] = append(out[g], r)
	}
	return out, rows.Err()
}

func (db *DB) AddAudienceRule(r AudienceRule) (int64, error) {
	if !audience.ValidKind(r.Kind) {
		return 0, fmt.Errorf("tipo di regola non valido: %q", r.Kind)
	}
	r.Value = strings.TrimSpace(r.Value)
	if r.Kind == audience.KindUser || r.Kind == audience.KindExclude {
		r.Value = strings.ToLower(r.Value)
	}
	if r.Kind != audience.KindAttr {
		r.Attr = ""
	}
	res, err := db.Exec(`INSERT INTO audience_rules (group_id, kind, attr, value, label) VALUES (?, ?, ?, ?, ?)`,
		r.GroupID, r.Kind, strings.TrimSpace(r.Attr), r.Value, strings.TrimSpace(r.Label))
	if isUniqueViolation(err) {
		return 0, ErrDuplicate
	}
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (db *DB) DeleteAudienceRule(groupID, ruleID int64) error {
	return checkAffected(db.Exec(`DELETE FROM audience_rules WHERE id = ? AND group_id = ?`, ruleID, groupID))
}

// ContentKind identifica il tipo di contenuto in content_audience.
type ContentKind string

const (
	ContentApp   ContentKind = "app"
	ContentGuide ContentKind = "guide"
	ContentAlert ContentKind = "alert"
)

// ContentAudience: visibilità di un contenuto. Mode "" = pubblico.
type ContentAudience struct {
	Mode   audience.Mode
	Groups []int64
}

func (db *DB) GetContentAudience(k ContentKind, id int64) (ContentAudience, error) {
	m, err := db.contentAudience(`WHERE kind = ? AND content_id = ?`, string(k), id)
	if err != nil {
		return ContentAudience{Groups: []int64{}}, err
	}
	if ca, ok := m[id]; ok {
		return ca, nil
	}
	return ContentAudience{Groups: []int64{}}, nil
}

func (db *DB) AllContentAudience(k ContentKind) (map[int64]ContentAudience, error) {
	return db.contentAudience(`WHERE kind = ?`, string(k))
}

func (db *DB) contentAudience(where string, args ...any) (map[int64]ContentAudience, error) {
	rows, err := db.Query(`SELECT content_id, mode, group_id FROM content_audience `+where+` ORDER BY content_id, group_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]ContentAudience{}
	for rows.Next() {
		var id, g int64
		var mode string
		if err := rows.Scan(&id, &mode, &g); err != nil {
			return nil, err
		}
		ca := out[id]
		ca.Mode = audience.Mode(mode)
		ca.Groups = append(ca.Groups, g)
		out[id] = ca
	}
	return out, rows.Err()
}

// SetContentAudience sostituisce la visibilità; Mode pubblico = nessuna riga.
func (db *DB) SetContentAudience(k ContentKind, id int64, ca ContentAudience) error {
	if !audience.ValidMode(string(ca.Mode)) {
		return fmt.Errorf("modalità non valida: %q", ca.Mode)
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM content_audience WHERE kind = ? AND content_id = ?`, string(k), id); err != nil {
		return err
	}
	if ca.Mode != audience.ModePublic {
		for _, g := range ca.Groups {
			if _, err := tx.Exec(`INSERT OR IGNORE INTO content_audience (kind, content_id, mode, group_id) VALUES (?, ?, ?, ?)`,
				string(k), id, string(ca.Mode), g); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// GroupUses: titoli dei contenuti che usano il gruppo (per il messaggio di errore).
func (db *DB) GroupUses(groupID int64) ([]string, error) {
	rows, err := db.Query(`
SELECT a.title FROM content_audience c JOIN apps a ON c.kind = 'app' AND a.id = c.content_id WHERE c.group_id = ?
UNION ALL
SELECT g.title FROM content_audience c JOIN guides g ON c.kind = 'guide' AND g.id = c.content_id WHERE c.group_id = ?
UNION ALL
SELECT al.title FROM content_audience c JOIN alerts al ON c.kind = 'alert' AND al.id = c.content_id WHERE c.group_id = ?`,
		groupID, groupID, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// deleteContent cancella un contenuto e la sua visibilità nella stessa transazione.
func (db *DB) deleteContent(k ContentKind, table string, id int64) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM content_audience WHERE kind = ? AND content_id = ?`, string(k), id); err != nil {
		return err
	}
	if err := checkAffected(tx.Exec(`DELETE FROM `+table+` WHERE id = ?`, id)); err != nil {
		return err
	}
	return tx.Commit()
}
