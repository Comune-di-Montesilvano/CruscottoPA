package database

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
)

// ErrInUse: elemento referenziato altrove (gruppo usato da contenuti,
// attributo usato da regole).
var ErrInUse = errors.New("database: elemento in uso")

// ErrInvalidDN: regola "gruppo AD" con un valore che non è un DN.
var ErrInvalidDN = audience.ErrInvalidDN

type AudienceAttribute struct {
	ID          int64
	Name, Label string
	Hero        int    // 0 = non sotto il saluto, altrimenti posizione 1..n
	HeroKind    string // HeroText | HeroPhone | HeroMail
}

// Formati degli attributi mostrati sotto il saluto.
const (
	HeroText  = "text"
	HeroPhone = "phone"
	HeroMail  = "mail"
)

func ValidHeroKind(k string) bool { return k == HeroText || k == HeroPhone || k == HeroMail }

func (db *DB) ListAudienceAttributes() ([]AudienceAttribute, error) {
	rows, err := db.Query(`SELECT id, name, label, hero, hero_kind FROM audience_attributes ORDER BY label COLLATE NOCASE, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AudienceAttribute{}
	for rows.Next() {
		var a AudienceAttribute
		if err := rows.Scan(&a.ID, &a.Name, &a.Label, &a.Hero, &a.HeroKind); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// HeroAttributes: attributi mostrati sotto il saluto, nell'ordine scelto.
func (db *DB) HeroAttributes() ([]AudienceAttribute, error) {
	all, err := db.ListAudienceAttributes()
	if err != nil {
		return nil, err
	}
	out := []AudienceAttribute{}
	for _, a := range all {
		if a.Hero > 0 {
			out = append(out, a)
		}
	}
	slices.SortFunc(out, func(a, b AudienceAttribute) int { return a.Hero - b.Hero })
	return out, nil
}

// SetAttributeHero: kind "" toglie l'attributo dalla testata, altrimenti lo
// mostra con quel formato (in coda se non c'era).
func (db *DB) SetAttributeHero(id int64, kind string) error {
	if kind != "" && !ValidHeroKind(kind) {
		return fmt.Errorf("formato della testata non valido: %q", kind)
	}
	return db.inTx(func(tx *sql.Tx) error {
		var hero int
		if err := tx.QueryRow(`SELECT hero FROM audience_attributes WHERE id = ?`, id).Scan(&hero); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		switch {
		case kind == "":
			hero, kind = 0, HeroText
		case hero == 0:
			if err := tx.QueryRow(`SELECT COALESCE(MAX(hero), 0) + 1 FROM audience_attributes`).Scan(&hero); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`UPDATE audience_attributes SET hero = ?, hero_kind = ? WHERE id = ?`, hero, kind, id); err != nil {
			return err
		}
		return renumberHero(tx, 0, 0)
	})
}

// MoveAttributeHero sposta un attributo mostrato di una posizione (dir ±1).
func (db *DB) MoveAttributeHero(id int64, dir int) error {
	return db.inTx(func(tx *sql.Tx) error { return renumberHero(tx, id, dir) })
}

// renumberHero rinumera 1..n gli attributi mostrati, spostando id di dir.
func renumberHero(tx *sql.Tx, id int64, dir int) error {
	rows, err := tx.Query(`SELECT id FROM audience_attributes WHERE hero > 0 ORDER BY hero, id`)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var r int64
		if err := rows.Scan(&r); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if id != 0 {
		i := slices.Index(ids, id)
		if i < 0 {
			return ErrNotFound
		}
		if j := i + dir; j >= 0 && j < len(ids) {
			ids[i], ids[j] = ids[j], ids[i]
		}
	}
	for pos, r := range ids {
		if _, err := tx.Exec(`UPDATE audience_attributes SET hero = ? WHERE id = ?`, pos+1, r); err != nil {
			return err
		}
	}
	return nil
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
WHERE a.id = ? AND r.attr <> ''`, id).Scan(&n); err != nil {
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
	if r.Kind != audience.KindAttr && !audience.IsRequirement(r.Kind) {
		r.Attr = ""
	}
	if audience.IsRequirement(r.Kind) {
		r.Value = ""
	}
	if r.Kind == audience.KindADGroup {
		dn, err := audience.NormalizeDN(r.Value)
		if err != nil {
			return 0, err
		}
		r.Value = dn
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
	return db.inTx(func(tx *sql.Tx) error { return setContentAudience(tx, k, id, ca) })
}

func setContentAudience(q execer, k ContentKind, id int64, ca ContentAudience) error {
	if !audience.ValidMode(string(ca.Mode)) {
		return fmt.Errorf("modalità non valida: %q", ca.Mode)
	}
	if _, err := q.Exec(`DELETE FROM content_audience WHERE kind = ? AND content_id = ?`, string(k), id); err != nil {
		return err
	}
	if ca.Mode != audience.ModePublic {
		for _, g := range ca.Groups {
			if _, err := q.Exec(`INSERT OR IGNORE INTO content_audience (kind, content_id, mode, group_id) VALUES (?, ?, ?, ?)`,
				string(k), id, string(ca.Mode), g); err != nil {
				return err
			}
		}
	}
	return nil
}

// execer: *DB e *sql.Tx, per usare le stesse query dentro e fuori da una transazione.
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func (db *DB) inTx(fn func(tx *sql.Tx) error) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// Varianti "con visibilità": contenuto e visibilità nella stessa transazione,
// così un errore sulla visibilità non lascia un contenuto pubblico a metà.

func (db *DB) CreateAppWithAudience(a App, ca ContentAudience) (id int64, err error) {
	err = db.inTx(func(tx *sql.Tx) error {
		if id, err = createApp(tx, a); err != nil {
			return err
		}
		return setContentAudience(tx, ContentApp, id, ca)
	})
	return id, err
}

func (db *DB) UpdateAppWithAudience(a App, ca ContentAudience) error {
	return db.inTx(func(tx *sql.Tx) error {
		if err := updateApp(tx, a); err != nil {
			return err
		}
		return setContentAudience(tx, ContentApp, a.ID, ca)
	})
}

func (db *DB) CreateGuideWithAudience(g Guide, ca ContentAudience) (id int64, err error) {
	err = db.inTx(func(tx *sql.Tx) error {
		if id, err = createGuide(tx, g); err != nil {
			return err
		}
		return setContentAudience(tx, ContentGuide, id, ca)
	})
	return id, err
}

func (db *DB) UpdateGuideWithAudience(g Guide, ca ContentAudience) error {
	return db.inTx(func(tx *sql.Tx) error {
		if err := updateGuide(tx, g); err != nil {
			return err
		}
		return setContentAudience(tx, ContentGuide, g.ID, ca)
	})
}

func (db *DB) CreateAlertWithAudience(a Alert, ca ContentAudience) (id int64, err error) {
	err = db.inTx(func(tx *sql.Tx) error {
		if id, err = createAlert(tx, a); err != nil {
			return err
		}
		return setContentAudience(tx, ContentAlert, id, ca)
	})
	return id, err
}

func (db *DB) UpdateAlertWithAudience(a Alert, ca ContentAudience) error {
	return db.inTx(func(tx *sql.Tx) error {
		if err := updateAlert(tx, a); err != nil {
			return err
		}
		return setContentAudience(tx, ContentAlert, a.ID, ca)
	})
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
	if k == ContentApp {
		// Le guide dell'app diventano generali (ON DELETE SET NULL): quelle
		// pubbliche prendono la visibilità dell'app, per non diventare di tutti.
		if _, err := tx.Exec(`
INSERT INTO content_audience (kind, content_id, mode, group_id)
SELECT 'guide', g.id, c.mode, c.group_id
FROM guides g JOIN content_audience c ON c.kind = 'app' AND c.content_id = g.app_id
WHERE g.app_id = ? AND NOT EXISTS (
	SELECT 1 FROM content_audience x WHERE x.kind = 'guide' AND x.content_id = g.id)`, id); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DELETE FROM content_audience WHERE kind = ? AND content_id = ?`, string(k), id); err != nil {
		return err
	}
	if err := checkAffected(tx.Exec(`DELETE FROM `+table+` WHERE id = ?`, id)); err != nil {
		return err
	}
	return tx.Commit()
}
