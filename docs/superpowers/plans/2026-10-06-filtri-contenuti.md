# Filtri sui contenuti per gruppi della plancia — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** gruppi della plancia definiti in admin con regole su AD (attributo, gruppo AD, utente, escludi utente) e visibilità di applicativi, guide e avvisi (Pubblico / Riservato a / Nascosto a), applicata in plancia con "Mostra tutto".

**Architecture:** logica pura in `internal/audience` (appartenenza e visibilità); tabelle v5 in `internal/database`; `identity.Directory` esteso con profilo utente (attributi + gruppi annidati), ricerche e anteprima; cache dei profili nel server web; pagina admin "Gruppi"; fieldset "Visibilità" nei tre form; filtro lato server in plancia, `/avvisi` e `/partials/alerts`.

**Tech Stack:** Go 1.26, `github.com/go-ldap/ldap/v3`, SQLite `modernc.org/sqlite`, HTMX 2, `html/template`.

**Spec:** `docs/superpowers/specs/2026-10-06-filtri-contenuti-design.md`

## Global Constraints

- **Filtro di presentazione, non protezione**: scriverlo nei form ("Filtro di visualizzazione, non una protezione: chi conosce l'indirizzo può comunque aprire l'applicativo e chiunque può scegliere «Mostra tutto»").
- **Utente anonimo o profilo non disponibile: solo i contenuti pubblici.**
- Nulla di fisso nel codice: gli attributi utilizzabili si configurano in admin.
- Tipi di regola: `attr`, `adgroup`, `user`, `exclude`; appartenenza = almeno una tra attr/adgroup/user **e** nessuna exclude; confronti senza maiuscole/minuscole e spazi ai bordi.
- Modalità contenuto: `""` pubblico (nessuna riga), `only`, `hide`.
- Gruppo in uso non eliminabile; attributo in uso non eliminabile.
- Nome attributo LDAP valido: `^[A-Za-z][A-Za-z0-9-]{0,63}$`; valori e query LDAP sempre con `ldap.EscapeFilter`.
- Cache profili 15 minuti, cache negativa 1 minuto; valori attributo 6 ore.
- `cruscotto_tutto=1`: `Path=/`, 1 anno, `SameSite=Lax`, non `HttpOnly`.
- Migrazione v5 in coda. CSP invariata (niente JS/stili inline, niente `on*=`). Pattern admin: sezione intera, 200/422.
- `go test ./...`, `go vet ./...`, `gofmt -l internal/ cmd/` vuoto. Testi in italiano.

## Review Focus

- Contenuto *Riservato a* un gruppo poi svuotato di regole (nessun membro): il contenuto non deve diventare visibile a tutti → test in Task 1 (`Member` con regole vuote = false).
- Regola `adgroup` con DN in maiuscole/minuscole diverse da quelle restituite da AD: deve valere comunque → test in Task 1.
- Richiesta di suggerimenti con caratteri speciali LDAP (`*`, `(`, `)`, `\`) nella query: nessuna iniezione nel filtro → test in Task 3.
- Applicativo con visibilità impostata e poi eliminato: nessuna riga orfana in `content_audience` che blocchi l'eliminazione del gruppo → test in Task 2.
- AD giù a ogni pagina: la plancia non deve interrogare AD a ogni richiesta (cache negativa 1 minuto) → test in Task 4.

## Prima di iniziare

Branch `feat/filtri-contenuti` da `spec/filtri-contenuti`. Leggere `CLAUDE.md` (pattern admin, test helper `newTestServer`/`newTestServerWith`/`do`/`login`/`postMultipart`, `fakeDirectory`).

---

### Task 1: logica pura `internal/audience`

**Files:**
- Create: `internal/audience/audience.go`, `internal/audience/audience_test.go`

**Interfaces:**
- Produces:
  - `audience.Profile{Username string; Attrs map[string]string; Groups []string}` (chiavi di `Attrs` minuscole)
  - `audience.Rule{Kind, Attr, Value string}`; costanti `KindAttr="attr"`, `KindADGroup="adgroup"`, `KindUser="user"`, `KindExclude="exclude"`; `ValidKind(string) bool`
  - `audience.Member(p Profile, rules []Rule) bool`
  - `audience.Mode` con `ModePublic Mode = ""`, `ModeOnly = "only"`, `ModeHide = "hide"`; `ValidMode(string) bool`
  - `audience.Visible(mode Mode, groups []int64, memberOf map[int64]bool, known bool) bool`

- [ ] **Step 1: test che falliscono**

`internal/audience/audience_test.go`:

```go
package audience

import "testing"

var mario = Profile{
	Username: "mario.rossi",
	Attrs:    map[string]string{"physicaldeliveryofficename": "INFORMATIZZAZIONE"},
	Groups:   []string{"CN=SHARE_PNRR_RW,OU=Gruppi,DC=intranet,DC=local"},
}

func TestMember(t *testing.T) {
	cases := []struct {
		name  string
		rules []Rule
		want  bool
	}{
		{"nessuna regola", nil, false},
		{"attributo", []Rule{{Kind: KindAttr, Attr: "physicalDeliveryOfficeName", Value: " informatizzazione "}}, true},
		{"attributo diverso", []Rule{{Kind: KindAttr, Attr: "physicalDeliveryOfficeName", Value: "TRIBUTI"}}, false},
		{"attributo assente nel profilo", []Rule{{Kind: KindAttr, Attr: "department", Value: "X"}}, false},
		{"gruppo AD, DN con maiuscole diverse", []Rule{{Kind: KindADGroup, Value: "cn=share_pnrr_rw,ou=gruppi,dc=intranet,dc=local"}}, true},
		{"utente", []Rule{{Kind: KindUser, Value: "Mario.Rossi"}}, true},
		{"oppure tra regole", []Rule{{Kind: KindUser, Value: "altro"}, {Kind: KindAttr, Attr: "physicalDeliveryOfficeName", Value: "INFORMATIZZAZIONE"}}, true},
		{"esclusione vince", []Rule{{Kind: KindAttr, Attr: "physicalDeliveryOfficeName", Value: "INFORMATIZZAZIONE"}, {Kind: KindExclude, Value: "mario.rossi"}}, false},
		{"solo esclusioni", []Rule{{Kind: KindExclude, Value: "altro"}}, false},
	}
	for _, c := range cases {
		if got := Member(mario, c.rules); got != c.want {
			t.Errorf("%s: Member = %v, atteso %v", c.name, got, c.want)
		}
	}
}

func TestVisible(t *testing.T) {
	in := map[int64]bool{1: true}
	cases := []struct {
		name   string
		mode   Mode
		groups []int64
		known  bool
		want   bool
	}{
		{"pubblico, anonimo", ModePublic, nil, false, true},
		{"riservato a un mio gruppo", ModeOnly, []int64{2, 1}, true, true},
		{"riservato ad altri", ModeOnly, []int64{2}, true, false},
		{"nascosto a un mio gruppo", ModeHide, []int64{1}, true, false},
		{"nascosto ad altri", ModeHide, []int64{2}, true, true},
		{"nascosto ad altri, anonimo", ModeHide, []int64{2}, false, false},
		{"riservato, anonimo", ModeOnly, []int64{1}, false, false},
	}
	for _, c := range cases {
		if got := Visible(c.mode, c.groups, in, c.known); got != c.want {
			t.Errorf("%s: Visible = %v, atteso %v", c.name, got, c.want)
		}
	}
}

func TestValidKindAndMode(t *testing.T) {
	if !ValidKind("attr") || !ValidKind("exclude") || ValidKind("altro") {
		t.Fatal("ValidKind")
	}
	if !ValidMode("") || !ValidMode("only") || !ValidMode("hide") || ValidMode("x") {
		t.Fatal("ValidMode")
	}
}
```

- [ ] **Step 2: verifica che falliscano**

Run: `go test ./internal/audience/`
Expected: errori di compilazione.

- [ ] **Step 3: implementazione**

`internal/audience/audience.go`:

```go
// Package audience decide chi vede cosa in plancia: appartenenza ai gruppi
// della plancia e visibilità dei contenuti. È un filtro di PRESENTAZIONE, non
// una protezione: l'identità dell'utente è dichiarata (NTLM non verificato).
package audience

import "strings"

const (
	KindAttr    = "attr"    // valore di un attributo AD
	KindADGroup = "adgroup" // gruppo AD (DN), annidati compresi
	KindUser    = "user"    // utente incluso
	KindExclude = "exclude" // utente escluso
)

func ValidKind(k string) bool {
	switch k {
	case KindAttr, KindADGroup, KindUser, KindExclude:
		return true
	}
	return false
}

// Profile è ciò che serve sapere di un utente per i gruppi della plancia.
type Profile struct {
	Username string            // minuscolo
	Attrs    map[string]string // nome attributo minuscolo → valore
	Groups   []string          // DN dei gruppi AD, annidati compresi
}

type Rule struct{ Kind, Attr, Value string }

func norm(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// Member: almeno una regola attr/adgroup/user soddisfatta e nessuna exclude.
func Member(p Profile, rules []Rule) bool {
	user := norm(p.Username)
	in := false
	for _, r := range rules {
		switch r.Kind {
		case KindExclude:
			if norm(r.Value) == user {
				return false
			}
		case KindUser:
			in = in || norm(r.Value) == user
		case KindAttr:
			v, ok := p.Attrs[norm(r.Attr)]
			in = in || (ok && norm(v) == norm(r.Value))
		case KindADGroup:
			for _, g := range p.Groups {
				if norm(g) == norm(r.Value) {
					in = true
				}
			}
		}
	}
	return in
}

type Mode string

const (
	ModePublic Mode = ""
	ModeOnly   Mode = "only"
	ModeHide   Mode = "hide"
)

func ValidMode(m string) bool {
	switch Mode(m) {
	case ModePublic, ModeOnly, ModeHide:
		return true
	}
	return false
}

// Visible: pubblico → sempre; utente non noto (anonimo o profilo non
// disponibile) → solo i pubblici; only → in almeno uno dei gruppi;
// hide → in nessuno.
func Visible(mode Mode, groups []int64, memberOf map[int64]bool, known bool) bool {
	if mode == ModePublic {
		return true
	}
	if !known {
		return false
	}
	any := false
	for _, g := range groups {
		if memberOf[g] {
			any = true
			break
		}
	}
	if mode == ModeOnly {
		return any
	}
	return !any
}
```

- [ ] **Step 4: verifica**

Run: `go test ./internal/audience/ && go vet ./...`
Expected: PASS.

- [ ] **Step 5: commit**

```bash
git add internal/audience
git commit -m "feat(audience): appartenenza ai gruppi della plancia e visibilità dei contenuti"
```

---

### Task 2: database (migrazione v5)

**Files:**
- Modify: `internal/database/migrations.go`, `internal/database/apps.go`, `internal/database/guides.go`, `internal/database/alerts.go` (`Delete*`)
- Create: `internal/database/audience.go`, `internal/database/audience_test.go`

**Interfaces:**
- Consumes: `audience.Rule`, `audience.Mode`, `audience.ValidKind`, `audience.ValidMode` (Task 1).
- Produces:
  - `database.AudienceAttribute{ID int64; Name, Label string}`; `ListAudienceAttributes() ([]AudienceAttribute, error)`; `CreateAudienceAttribute(name, label string) (int64, error)` (`ErrDuplicate`); `DeleteAudienceAttribute(id int64) error` (`ErrInUse` se usato da regole)
  - `database.AudienceGroup{ID int64; Name string; SortOrder, Rules, Uses int}`; `ListAudienceGroups() ([]AudienceGroup, error)`; `GetAudienceGroup(id) (AudienceGroup, error)`; `CreateAudienceGroup(name) (int64, error)`; `RenameAudienceGroup(id, name) error`; `MoveAudienceGroup(id int64, dir int) error`; `DeleteAudienceGroup(id int64) error` (`ErrInUse`)
  - `database.AudienceRule{ID, GroupID int64; Kind, Attr, Value, Label string}`; `ListAudienceRules(groupID int64) ([]AudienceRule, error)`; `AllAudienceRules() (map[int64][]audience.Rule, error)`; `AddAudienceRule(r AudienceRule) (int64, error)` (`ErrDuplicate`); `DeleteAudienceRule(groupID, ruleID int64) error`
  - `database.ContentKind` con `ContentApp`, `ContentGuide`, `ContentAlert`; `database.ContentAudience{Mode audience.Mode; Groups []int64}`; `GetContentAudience(k, id) (ContentAudience, error)`; `SetContentAudience(k, id, ca ContentAudience) error`; `AllContentAudience(k) (map[int64]ContentAudience, error)`; `GroupUses(groupID int64) ([]string, error)` (titoli dei contenuti che lo usano)
  - `database.ErrInUse`

- [ ] **Step 1: test che falliscono**

`internal/database/audience_test.go`:

```go
package database

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
)

func TestAudienceAttributes(t *testing.T) {
	db := newTestDB(t)
	id, err := db.CreateAudienceAttribute("physicalDeliveryOfficeName", "Ufficio")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateAudienceAttribute("PHYSICALDELIVERYOFFICENAME", "Doppio"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("attributo doppio: %v", err)
	}
	g, _ := db.CreateAudienceGroup("CED")
	db.AddAudienceRule(AudienceRule{GroupID: g, Kind: audience.KindAttr, Attr: "physicalDeliveryOfficeName", Value: "INFORMATIZZAZIONE"})
	if err := db.DeleteAudienceAttribute(id); !errors.Is(err, ErrInUse) {
		t.Fatalf("attributo in uso eliminato: %v", err)
	}
}

func TestAudienceGroupsAndRules(t *testing.T) {
	db := newTestDB(t)
	ced, _ := db.CreateAudienceGroup("CED")
	rag, _ := db.CreateAudienceGroup("Ragioneria")
	if _, err := db.CreateAudienceGroup("ced"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("nome doppio: %v", err)
	}
	r1, err := db.AddAudienceRule(AudienceRule{GroupID: ced, Kind: audience.KindUser, Value: "Mario.Rossi"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddAudienceRule(AudienceRule{GroupID: ced, Kind: audience.KindUser, Value: "mario.rossi"}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("regola doppia: %v", err)
	}
	if _, err := db.AddAudienceRule(AudienceRule{GroupID: ced, Kind: "boh", Value: "x"}); err == nil {
		t.Fatal("tipo di regola non valido accettato")
	}
	all, _ := db.AllAudienceRules()
	if !reflect.DeepEqual(all[ced], []audience.Rule{{Kind: audience.KindUser, Value: "mario.rossi"}}) || len(all[rag]) != 0 {
		t.Fatalf("AllAudienceRules: %v", all)
	}
	groups, _ := db.ListAudienceGroups()
	if len(groups) != 2 || groups[0].Name != "CED" || groups[0].Rules != 1 {
		t.Fatalf("ListAudienceGroups: %+v", groups)
	}
	if err := db.MoveAudienceGroup(rag, -1); err != nil {
		t.Fatal(err)
	}
	if groups, _ = db.ListAudienceGroups(); groups[0].Name != "Ragioneria" {
		t.Fatalf("spostamento: %+v", groups)
	}
	if err := db.DeleteAudienceRule(ced, r1); err != nil {
		t.Fatal(err)
	}
	if rules, _ := db.ListAudienceRules(ced); len(rules) != 0 {
		t.Fatalf("regola non tolta: %v", rules)
	}
}

func TestContentAudience(t *testing.T) {
	db := newTestDB(t)
	ced, _ := db.CreateAudienceGroup("CED")
	apps, _ := db.ListApps()
	app := apps[0].ID

	if ca, err := db.GetContentAudience(ContentApp, app); err != nil || ca.Mode != audience.ModePublic || len(ca.Groups) != 0 {
		t.Fatalf("iniziale: %+v %v", ca, err)
	}
	want := ContentAudience{Mode: audience.ModeOnly, Groups: []int64{ced}}
	if err := db.SetContentAudience(ContentApp, app, want); err != nil {
		t.Fatal(err)
	}
	if ca, _ := db.GetContentAudience(ContentApp, app); !reflect.DeepEqual(ca, want) {
		t.Fatalf("Get: %+v", ca)
	}
	if m, _ := db.AllContentAudience(ContentApp); !reflect.DeepEqual(m[app], want) {
		t.Fatalf("All: %+v", m)
	}
	if uses, _ := db.GroupUses(ced); len(uses) != 1 {
		t.Fatalf("GroupUses: %v", uses)
	}
	if err := db.DeleteAudienceGroup(ced); !errors.Is(err, ErrInUse) {
		t.Fatalf("gruppo in uso eliminato: %v", err)
	}
	// Eliminare il contenuto toglie le sue righe: il gruppo torna eliminabile.
	if err := db.DeleteApp(app); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteAudienceGroup(ced); err != nil {
		t.Fatalf("gruppo non più in uso: %v", err)
	}

	id, _ := db.CreateAlert(Alert{Title: "x", Level: LevelNews, StartsAt: time.Now()})
	g2, _ := db.CreateAudienceGroup("G2")
	db.SetContentAudience(ContentAlert, id, ContentAudience{Mode: audience.ModeHide, Groups: []int64{g2}})
	db.SetContentAudience(ContentAlert, id, ContentAudience{Mode: audience.ModePublic})
	if ca, _ := db.GetContentAudience(ContentAlert, id); ca.Mode != audience.ModePublic {
		t.Fatalf("ritorno a pubblico: %+v", ca)
	}
}
```

- [ ] **Step 2: verifica che falliscano**

Run: `go test ./internal/database/ -run Audience`
Expected: errori di compilazione.

- [ ] **Step 3: migrazione**

In `migrations.go` aggiungere `migrateV5Audience` in coda all'elenco e la funzione con lo SQL della spec §1 (quattro `CREATE TABLE`, identici a quelli della spec, più `CREATE INDEX idx_content_audience_group ON content_audience(group_id);`).

- [ ] **Step 4: implementazione**

`internal/database/audience.go`:

```go
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
```

Poi sostituire i corpi di `DeleteApp`, `DeleteGuide`, `DeleteAlert` con `return db.deleteContent(ContentApp, "apps", id)` (e `ContentGuide`/`"guides"`, `ContentAlert`/`"alerts"`).

Verificare la firma reale di `moveRow` in `order.go`: se lo scope richiede una colonna, usare l'equivalente per "nessun ambito" (es. `"1 = ?", []any{1}`), adeguando senza cambiare `moveRow`.

- [ ] **Step 5: verifica**

Run: `go test ./internal/database/ ./internal/backup/ ./internal/web/ && go vet ./...`
Expected: PASS.

- [ ] **Step 6: commit**

```bash
git add internal/database
git commit -m "feat(database): migrazione 5 con gruppi della plancia e visibilità dei contenuti"
```

---

### Task 3: directory AD estesa (profilo, ricerche, anteprima)

**Files:**
- Modify: `internal/identity/directory.go`, `internal/identity/directory_test.go`
- Modify: `internal/web/server_test.go` (`fakeDirectory` implementa i nuovi metodi)

**Interfaces:**
- Consumes: `audience.Profile`, `audience.Rule` (Task 1).
- Produces:
  - `identity.ADGroup{DN, Name string}`
  - `identity.Directory` con in più: `Profile(username string, attrs []string) (audience.Profile, error)`; `SearchGroups(q string) ([]ADGroup, error)`; `SearchUsers(q string) ([]Person, error)`; `AttributeValues(attr string) ([]string, error)`; `Members(rules []audience.Rule) (int, []Person, error)`
  - `identity.ValidAttrName(string) bool`
  - funzioni pure testate: `membersFilter(rules []audience.Rule) (string, bool)` (false = nessuna regola positiva), `searchFilter(q string, attrs ...string) string`
  - nei test web: `fakeDirectory` con campi `profiles map[string]audience.Profile`, `groups []identity.ADGroup`, `values map[string][]string`, `members []identity.Person`

- [ ] **Step 1: test che falliscono**

In `internal/identity/directory_test.go`:

```go
func TestValidAttrName(t *testing.T) {
	for _, ok := range []string{"physicalDeliveryOfficeName", "department", "extensionAttribute1", "x-y"} {
		if !ValidAttrName(ok) {
			t.Errorf("%q rifiutato", ok)
		}
	}
	for _, bad := range []string{"", "1abc", "a b", "cn=*", "a)(b", strings.Repeat("a", 65)} {
		if ValidAttrName(bad) {
			t.Errorf("%q accettato", bad)
		}
	}
}

func TestSearchFilterEscapes(t *testing.T) {
	f := searchFilter(`a*)(cn=\`, "cn")
	if strings.Contains(f, "a*)(cn=") || !strings.Contains(f, `a\2a\29\28cn=\5c`) {
		t.Fatalf("query non escapata: %q", f)
	}
}

func TestMembersFilter(t *testing.T) {
	if _, ok := membersFilter([]audience.Rule{{Kind: audience.KindExclude, Value: "x"}}); ok {
		t.Fatal("senza regole positive non deve esserci un filtro")
	}
	f, ok := membersFilter([]audience.Rule{
		{Kind: audience.KindAttr, Attr: "physicalDeliveryOfficeName", Value: "INFO*"},
		{Kind: audience.KindADGroup, Value: "CN=G,DC=x"},
		{Kind: audience.KindUser, Value: "mrossi"},
		{Kind: audience.KindExclude, Value: "stagista1"},
	})
	for _, want := range []string{
		`(physicalDeliveryOfficeName=INFO\2a)`,
		`(memberOf:1.2.840.113556.1.4.1941:=CN=G,DC=x)`,
		`(sAMAccountName=mrossi)`,
		`(!(sAMAccountName=stagista1))`,
	} {
		if !ok || !strings.Contains(f, want) {
			t.Errorf("manca %q in %q", want, f)
		}
	}
}

func TestMockDirectoryProfile(t *testing.T) {
	p, err := MockDirectory{}.Profile("MRossi", []string{"physicalDeliveryOfficeName"})
	if err != nil || p.Username != "mrossi" || p.Attrs["physicaldeliveryofficename"] == "" || len(p.Groups) == 0 {
		t.Fatalf("Profile: %+v %v", p, err)
	}
}
```

(aggiungere l'import di `internal/audience`).

- [ ] **Step 2: verifica che falliscano**

Run: `go test ./internal/identity/`
Expected: errori di compilazione.

- [ ] **Step 3: implementazione**

In `internal/identity/directory.go`:
- estendere l'interfaccia `Directory` con i cinque metodi;
- aggiungere:

```go
type ADGroup struct{ DN, Name string }

var attrNameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]{0,63}$`)

// ValidAttrName: nome di attributo LDAP accettabile (finisce nei filtri).
func ValidAttrName(s string) bool { return attrNameRe.MatchString(s) }

const activeUsersFilter = `(objectCategory=person)(objectClass=user)(!(userAccountControl:1.2.840.113556.1.4.803:=2))`

// searchFilter: sottostringa q (escapata) in uno degli attributi indicati.
func searchFilter(q string, attrs ...string) string {
	esc := ldap.EscapeFilter(strings.TrimSpace(q))
	var b strings.Builder
	b.WriteString("(|")
	for _, a := range attrs {
		fmt.Fprintf(&b, "(%s=*%s*)", a, esc)
	}
	b.WriteString(")")
	return b.String()
}

// membersFilter traduce le regole in un filtro sugli utenti attivi; false se
// non c'è nessuna regola positiva (gruppo senza membri).
func membersFilter(rules []audience.Rule) (string, bool) {
	var or, not strings.Builder
	for _, r := range rules {
		v := ldap.EscapeFilter(strings.TrimSpace(r.Value))
		switch r.Kind {
		case audience.KindAttr:
			if ValidAttrName(r.Attr) {
				fmt.Fprintf(&or, "(%s=%s)", r.Attr, v)
			}
		case audience.KindADGroup:
			fmt.Fprintf(&or, "(memberOf:1.2.840.113556.1.4.1941:=%s)", v)
		case audience.KindUser:
			fmt.Fprintf(&or, "(sAMAccountName=%s)", v)
		case audience.KindExclude:
			fmt.Fprintf(&not, "(!(sAMAccountName=%s))", v)
		}
	}
	if or.Len() == 0 {
		return "", false
	}
	return "(&" + activeUsersFilter + "(|" + or.String() + ")" + not.String() + ")", true
}
```

- sostituire la costante del filtro in `userFilter` con `activeUsersFilter` (stesso valore);
- metodi di `LDAPDirectory` (tutti aprono la connessione con un helper `conn()` che fa `auth.Dial` + bind di servizio, come `Lookup`):
  - `Profile`: cerca l'utente (`userFilter`) chiedendo `distinguishedName`, `sAMAccountName` e gli attributi validi tra `attrs`; poi cerca i gruppi con filtro `(&(objectClass=group)(member:1.2.840.113556.1.4.1941:=<DN escapato>))` e attributo `dn` (paging 500). Restituisce `audience.Profile` con `Attrs` a chiavi minuscole. Utente non trovato → `ErrUnknownUser`.
  - `SearchGroups(q)`: `(&(objectClass=group)` + `searchFilter(q, "cn")` + `)`, size limit 20, attributi `cn`; risultato ordinato per nome. `q` vuota → slice vuota senza interrogare AD.
  - `SearchUsers(q)`: `(&` + `activeUsersFilter` + `searchFilter(q, "sAMAccountName", "displayName")` + `)`, size limit 20, attributi `sAMAccountName`, `displayName`, `givenName`; usa `personFromEntry`.
  - `AttributeValues(attr)`: `attr` non valido → errore; altrimenti valori distinti (trim, senza maiuscole doppie) tra gli utenti attivi con `(attr=*)`, paging 500, ordinati; **cache 6 ore** per attributo (mappa + `sync.Mutex` nel `LDAPDirectory`; in errore restituisce l'ultimo valore valido se c'è).
  - `Members(rules)`: `membersFilter`; se false → `0, nil, nil`; altrimenti ricerca paginata (500) con attributi `sAMAccountName`, `displayName`, `givenName`; conteggio totale e primi 30 `personFromEntry` ordinati per nome.
- `MockDirectory`:
  - `Profile` → `audience.Profile{Username: lower, Attrs: {"physicaldeliveryofficename": "INFORMATIZZAZIONE"}, Groups: ["CN=Utenti Mock,OU=Mock,DC=mock"]}` (solo gli attributi richiesti che coincidono);
  - `SearchGroups` → filtra per sottostringa su `[{"CN=Utenti Mock,OU=Mock,DC=mock", "Utenti Mock"}, {"CN=Amministrativi Mock,OU=Mock,DC=mock", "Amministrativi Mock"}]`;
  - `SearchUsers` → `[{q, q, q}]` se `q` è uno username valido;
  - `AttributeValues` → `["AMMINISTRATIVO", "INFORMATIZZAZIONE", "TRIBUTI"]` per qualsiasi attributo valido;
  - `Members` → `1, [{mock, Utente Mock}]` se c'è almeno una regola positiva.

In `internal/web/server_test.go` estendere `fakeDirectory`:

```go
type fakeDirectory struct {
	people   map[string]identity.Person
	profiles map[string]audience.Profile
	groups   []identity.ADGroup
	values   map[string][]string
	members  []identity.Person
	err      error
	calls    *int // conta le chiamate a Profile (test della cache)
}

func (f fakeDirectory) Profile(u string, _ []string) (audience.Profile, error) {
	if f.calls != nil {
		*f.calls++
	}
	if f.err != nil {
		return audience.Profile{}, f.err
	}
	p, ok := f.profiles[strings.ToLower(u)]
	if !ok {
		return audience.Profile{}, identity.ErrUnknownUser
	}
	return p, nil
}

func (f fakeDirectory) SearchGroups(string) ([]identity.ADGroup, error)  { return f.groups, f.err }
func (f fakeDirectory) SearchUsers(string) ([]identity.Person, error)    { return f.members, f.err }
func (f fakeDirectory) AttributeValues(a string) ([]string, error)       { return f.values[a], f.err }
func (f fakeDirectory) Members([]audience.Rule) (int, []identity.Person, error) {
	return len(f.members), f.members, f.err
}
```

e in `testDirectory` aggiungere:

```go
	profiles: map[string]audience.Profile{
		"mrossi":    {Username: "mrossi", Attrs: map[string]string{"physicaldeliveryofficename": "TRIBUTI"}, Groups: []string{"CN=SHARE_TRIBUTI_RW,DC=test"}},
		"senzanome": {Username: "senzanome", Attrs: map[string]string{}},
	},
	groups:  []identity.ADGroup{{DN: "CN=SHARE_TRIBUTI_RW,DC=test", Name: "SHARE_TRIBUTI_RW"}},
	values:  map[string][]string{"physicalDeliveryOfficeName": {"LLPP", "TRIBUTI"}},
	members: []identity.Person{{Username: "mrossi", Name: "Mario Rossi", GivenName: "Mario"}},
```

- [ ] **Step 4: verifica**

Run: `go test ./... && go vet ./... && gofmt -l internal/`
Expected: PASS.

- [ ] **Step 5: commit**

```bash
git add internal/identity internal/web/server_test.go
git commit -m "feat(identity): profilo utente con attributi e gruppi AD annidati, ricerche e anteprima"
```

---

### Task 4: cache dei profili nel server

**Files:**
- Create: `internal/web/profiles.go`, `internal/web/profiles_test.go`
- Modify: `internal/web/server.go` (campo `profiles`)

**Interfaces:**
- Consumes: `identity.Directory.Profile` (Task 3), `db.ListAudienceAttributes`, `db.AllAudienceRules` (Task 2), `audience.Member` (Task 1), `s.viewer` (esistente).
- Produces:
  - `type profileCache struct` con `get(username string, load func() (audience.Profile, error)) (audience.Profile, bool)`; `newProfileCache(now func() time.Time) *profileCache`; TTL 15 minuti, errore 1 minuto
  - `func (s *Server) viewerGroups(r *http.Request) (memberOf map[int64]bool, known bool)`: `known=false` se anonimo, senza cookie, riconoscimento spento o profilo non disponibile

- [ ] **Step 1: test che falliscono**

`internal/web/profiles_test.go`:

```go
package web

import (
	"errors"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
)

func TestProfileCache(t *testing.T) {
	now := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
	c := newProfileCache(func() time.Time { return now })
	calls := 0
	ok := func() (audience.Profile, error) { calls++; return audience.Profile{Username: "mrossi"}, nil }
	fail := func() (audience.Profile, error) { calls++; return audience.Profile{}, errors.New("AD giù") }

	if p, found := c.get("mrossi", ok); !found || p.Username != "mrossi" || calls != 1 {
		t.Fatal("primo caricamento")
	}
	now = now.Add(14 * time.Minute)
	c.get("mrossi", ok)
	if calls != 1 {
		t.Fatal("entro 15 minuti non si ricarica")
	}
	now = now.Add(2 * time.Minute)
	c.get("mrossi", ok)
	if calls != 2 {
		t.Fatal("dopo 15 minuti si ricarica")
	}

	if _, found := c.get("giu", fail); found || calls != 3 {
		t.Fatal("errore: nessun profilo")
	}
	now = now.Add(30 * time.Second)
	c.get("giu", fail)
	if calls != 3 {
		t.Fatal("cache negativa: entro 1 minuto non si riprova")
	}
	now = now.Add(31 * time.Second)
	c.get("giu", fail)
	if calls != 4 {
		t.Fatal("dopo 1 minuto si riprova")
	}
}
```

- [ ] **Step 2: verifica che fallisca**

Run: `go test ./internal/web/ -run ProfileCache`
Expected: errori di compilazione.

- [ ] **Step 3: implementazione**

`internal/web/profiles.go`:

```go
package web

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
)

const (
	profileTTL      = 15 * time.Minute
	profileErrorTTL = time.Minute // AD giù: non interrogarlo a ogni pagina
)

type cachedProfile struct {
	p       audience.Profile
	ok      bool
	expires time.Time
}

// profileCache tiene i profili AD per username (in memoria, per processo).
type profileCache struct {
	mu  sync.Mutex
	m   map[string]cachedProfile
	now func() time.Time
}

func newProfileCache(now func() time.Time) *profileCache {
	return &profileCache{m: map[string]cachedProfile{}, now: now}
}

func (c *profileCache) get(username string, load func() (audience.Profile, error)) (audience.Profile, bool) {
	key := strings.ToLower(username)
	c.mu.Lock()
	e, hit := c.m[key]
	c.mu.Unlock()
	if hit && c.now().Before(e.expires) {
		return e.p, e.ok
	}
	p, err := load()
	e = cachedProfile{p: p, ok: err == nil, expires: c.now().Add(profileTTL)}
	if err != nil {
		e.expires = c.now().Add(profileErrorTTL)
		if !errors.Is(err, identity.ErrUnknownUser) {
			slog.Warn("profilo utente da AD", "err", err)
		}
	}
	c.mu.Lock()
	c.m[key] = e
	c.mu.Unlock()
	return e.p, e.ok
}

// viewerGroups: gruppi della plancia di chi guarda. known=false → solo pubblici.
func (s *Server) viewerGroups(r *http.Request) (map[int64]bool, bool) {
	u, ok := s.viewer(r)
	if !ok || u.Anonymous || u.Username == "" || s.directory == nil {
		return nil, false
	}
	attrs, err := s.db.ListAudienceAttributes()
	if err != nil {
		slog.Warn("attributi dei gruppi", "err", err)
		return nil, false
	}
	names := make([]string, len(attrs))
	for i, a := range attrs {
		names[i] = a.Name
	}
	p, ok := s.profiles.get(u.Username, func() (audience.Profile, error) { return s.directory.Profile(u.Username, names) })
	if !ok {
		return nil, false
	}
	rules, err := s.db.AllAudienceRules()
	if err != nil {
		slog.Warn("regole dei gruppi", "err", err)
		return nil, false
	}
	in := map[int64]bool{}
	for g, rs := range rules {
		if audience.Member(p, rs) {
			in[g] = true
		}
	}
	return in, true
}
```

In `server.go`: campo `profiles *profileCache` e in `New` `profiles: newProfileCache(o.Now),` (dopo che `o.Now` ha il default).

Nota: con attributi aggiunti in admin dopo il caricamento, il profilo in cache non li contiene fino alla scadenza (massimo 15 minuti). Accettato; nel gestore di aggiunta/rimozione degli attributi (Task 5) svuotare la cache con `s.profiles.reset()`:

```go
func (c *profileCache) reset() {
	c.mu.Lock()
	c.m = map[string]cachedProfile{}
	c.mu.Unlock()
}
```

- [ ] **Step 4: verifica**

Run: `go test ./internal/web/ && go vet ./...`
Expected: PASS.

- [ ] **Step 5: commit**

```bash
git add internal/web
git commit -m "feat(web): cache dei profili AD e gruppi della plancia di chi guarda"
```

---

### Task 5: pagina admin "Gruppi"

**Files:**
- Create: `internal/web/admin_audience.go`, `internal/web/admin_audience_test.go`, `web/templates/admin_gruppi.html`
- Modify: `internal/web/server.go` (route), `web/templates/admin_base.html` (voce nel rail), `web/static/css/admin.css`

**Interfaces:**
- Consumes: metodi `database` del Task 2, `identity.Directory` (Task 3), `identity.ValidAttrName`, `s.profiles.reset()` (Task 4).
- Produces: route
  - `GET /admin/ad/suggerimenti` (dal form delle regole)
  - `GET /admin/gruppi` (pagina), `POST /admin/gruppi/attributi` (aggiungi: `name`, `label`), `POST /admin/gruppi/attributi/{id}/elimina`
  - `POST /admin/gruppi` (nuovo: `name`), `POST /admin/gruppi/{id}` (rinomina), `POST /admin/gruppi/{id}/elimina`, `POST /admin/gruppi/{id}/sposta` (`dir=up|down`)
  - `GET /admin/gruppi/{id}/modifica` (sezione con le regole), `POST /admin/gruppi/{id}/regole` (`kind`, `attr`, `value`, `label`), `POST /admin/gruppi/{id}/regole/{rid}/elimina`, `POST /admin/gruppi/{id}/anteprima`
  - suggerimenti (frammenti HTML): `GET /admin/ad/valori?attr=&q=`, `GET /admin/ad/gruppi?q=`, `GET /admin/ad/utenti?q=`
  - template `audience_section` (dentro `<div id="section">`), `ad_suggestions`

- [ ] **Step 1: test che falliscono**

`internal/web/admin_audience_test.go`:

```go
package web

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

var hx = map[string]string{"HX-Request": "true"}

func TestAudienceAttributesAdmin(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	rec := do(t, s, "POST", "/admin/gruppi/attributi", url.Values{"name": {"physicalDeliveryOfficeName"}, "label": {"Ufficio"}}, c, hx)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Ufficio") {
		t.Fatalf("aggiungi attributo: %d\n%s", rec.Code, rec.Body)
	}
	if rec := do(t, s, "POST", "/admin/gruppi/attributi", url.Values{"name": {"a)(b"}, "label": {"X"}}, c, hx); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Nome di attributo LDAP non valido") {
		t.Fatalf("nome non valido: %d", rec.Code)
	}
	attrs, _ := db.ListAudienceAttributes()
	if len(attrs) != 1 {
		t.Fatalf("attributi: %v", attrs)
	}
}

func TestAudienceGroupLifecycle(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	db.CreateAudienceAttribute("physicalDeliveryOfficeName", "Ufficio")
	if rec := do(t, s, "POST", "/admin/gruppi", url.Values{"name": {"CED"}}, c, hx); rec.Code != 200 {
		t.Fatalf("nuovo gruppo: %d", rec.Code)
	}
	groups, _ := db.ListAudienceGroups()
	g := itoa(groups[0].ID)

	for _, rule := range []url.Values{
		{"kind": {"attr"}, "attr": {"physicalDeliveryOfficeName"}, "value": {"TRIBUTI"}},
		{"kind": {"adgroup"}, "value": {"CN=SHARE_TRIBUTI_RW,DC=test"}, "label": {"SHARE_TRIBUTI_RW"}},
		{"kind": {"user"}, "value": {"Mario.Rossi"}},
		{"kind": {"exclude"}, "value": {"stagista1"}},
	} {
		if rec := do(t, s, "POST", "/admin/gruppi/"+g+"/regole", rule, c, hx); rec.Code != 200 {
			t.Fatalf("regola %v: %d\n%s", rule, rec.Code, rec.Body)
		}
	}
	page := do(t, s, "GET", "/admin/gruppi/"+g+"/modifica", nil, c, hx).Body.String()
	for _, want := range []string{"Ufficio = TRIBUTI", "Gruppo AD SHARE_TRIBUTI_RW", "Utente mario.rossi", "Escludi stagista1"} {
		if !strings.Contains(page, want) {
			t.Errorf("manca %q", want)
		}
	}
	if rec := do(t, s, "POST", "/admin/gruppi/"+g+"/regole", url.Values{"kind": {"attr"}, "attr": {"department"}, "value": {"X"}}, c, hx); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("attributo non configurato accettato: %d", rec.Code)
	}
	prev := do(t, s, "POST", "/admin/gruppi/"+g+"/anteprima", nil, c, hx).Body.String()
	if !strings.Contains(prev, "1 utente") || !strings.Contains(prev, "Mario Rossi") {
		t.Fatalf("anteprima:\n%s", prev)
	}

	apps, _ := db.ListApps()
	db.SetContentAudience(database.ContentApp, apps[0].ID, database.ContentAudience{Mode: "only", Groups: []int64{groups[0].ID}})
	rec := do(t, s, "POST", "/admin/gruppi/"+g+"/elimina", nil, c, hx)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), apps[0].Title) {
		t.Fatalf("gruppo in uso eliminato: %d\n%s", rec.Code, rec.Body)
	}
}

func TestADSuggestions(t *testing.T) {
	s, _ := newTestServer(t, nil)
	c := login(t, s)
	if body := do(t, s, "GET", "/admin/ad/gruppi?q=TRIB", nil, c, hx).Body.String(); !strings.Contains(body, "SHARE_TRIBUTI_RW") {
		t.Fatalf("suggerimenti gruppi:\n%s", body)
	}
	if body := do(t, s, "GET", "/admin/ad/valori?attr=physicalDeliveryOfficeName&q=tri", nil, c, hx).Body.String(); !strings.Contains(body, "TRIBUTI") || strings.Contains(body, "LLPP") {
		t.Fatalf("suggerimenti valori (filtrati per q):\n%s", body)
	}
	if rec := do(t, s, "GET", "/admin/ad/utenti?q=x", nil, nil, hx); rec.Code != http.StatusUnauthorized {
		t.Fatalf("suggerimenti senza sessione: %d", rec.Code)
	}
}

func TestADUnavailableInAdmin(t *testing.T) {
	s, _ := newTestServerWith(t, nil, func(o *Options) { o.Directory = fakeDirectory{err: errors.New("giù")} })
	c := login(t, s)
	if body := do(t, s, "GET", "/admin/ad/gruppi?q=a", nil, c, hx).Body.String(); !strings.Contains(body, "AD non disponibile") {
		t.Fatalf("AD giù:\n%s", body)
	}
}
```

(se `hx` esiste già nei test, non ridefinirlo: usare quello esistente.)

- [ ] **Step 2: verifica che falliscano**

Run: `go test ./internal/web/ -run 'Audience|ADSuggestions|ADUnavailable'`
Expected: 404 / FAIL.

- [ ] **Step 3: handler**

`internal/web/admin_audience.go`, sul modello di `admin_categories.go`:

- `type audienceSection struct { Attributes []database.AudienceAttribute; Groups []database.AudienceGroup; Edit *groupEdit; Errors formErrors; NewName string }`
- `type groupEdit struct { Group database.AudienceGroup; Rules []ruleView; Attributes []database.AudienceAttribute; Preview *previewView; Errors formErrors }` (gli errori delle regole vanno in `Edit.Errors["rule"]`)
- `type ruleView struct { database.AudienceRule; Text string }` con `Text` = etichetta leggibile:
  - `attr` → `<label dell'attributo, o il nome se non configurato> = <value>`
  - `adgroup` → `Gruppo AD <label, o il DN>`
  - `user` → `Utente <value>`; `exclude` → `Escludi <value>`
- `type previewView struct { Count int; People []identity.Person; Err string }`
- `func (s *Server) audienceData(edit int64, errs formErrors) (audienceSection, error)` carica attributi, gruppi e, se `edit != 0`, il gruppo con le regole.
- `renderAudience(w, status, edit, errs)` → `s.render(w, status, "audience_section", sec)`; pagina → `s.renderPage(w, r, "admin_gruppi.html", "gruppi", sec)`.
- Attributi: nome validato con `identity.ValidAttrName` (errore "Nome di attributo LDAP non valido."), etichetta obbligatoria max 60 (`checkText`); `ErrDuplicate` → "Attributo già presente."; eliminazione `ErrInUse` → "Attributo usato da qualche regola: toglile prima." Dopo aggiunta o eliminazione riuscita `s.profiles.reset()`.
- Gruppi: nome obbligatorio max 60; `ErrDuplicate` → "Esiste già un gruppo con questo nome."; eliminazione `ErrInUse` → messaggio "Gruppo usato da: " + `strings.Join(db.GroupUses(id), ", ")` (422, in `Errors["general"]`).
- Regole: `kind` con `audience.ValidKind`; per `attr` l'attributo deve essere tra quelli configurati (confronto senza maiuscole) altrimenti 422 "Scegli un attributo configurato."; `value` obbligatorio max 512; `user`/`exclude` con la regola username `[A-Za-z0-9._@-]{1,128}` (422 "Username non valido."); `ErrDuplicate` → "Regola già presente.". Dopo ogni modifica alle regole la sezione resta in modalità modifica del gruppo.
- Anteprima: `s.directory.Members(rules)`; `nil` directory o errore → `Preview.Err = "AD non disponibile."`.
- Suggerimenti: `q` max 64 caratteri; directory `nil` o errore → frammento con "AD non disponibile."; valori: `AttributeValues(attr)` filtrati per sottostringa di `q` (senza maiuscole), max 20. Il frammento `ad_suggestions` è una lista di `<button type="button" class="suggestion" data-fill="<valore>" data-label="<etichetta>">…</button>`; `admin.js` al click copia `data-fill` nel campo `value` del form e `data-label` nel campo nascosto `label` (aggiungere questo handler a `admin.js`, delegato su `document`, senza JS inline).

Route in `server.go` (tutte con `s.requireAdmin`), come elencate negli Interfaces.

- [ ] **Step 4: template**

`web/templates/admin_gruppi.html`:

```html
{{define "admin_gruppi.html"}}{{template "admin_top" .}}
<h1>Gruppi della plancia</h1>
{{template "audience_section" .Body}}
{{template "admin_bottom" .}}{{end}}

{{define "audience_section"}}
<div id="section">
	{{with .Errors.general}}<p class="flash error" role="alert">{{.}}</p>{{end}}
	<p class="hint">I gruppi decidono chi vede applicativi, guide e avvisi. È un filtro di visualizzazione, non una protezione.</p>

	<div class="card">
		<h2>Attributi AD utilizzabili</h2>
		<table class="list"><tbody>
			{{range .Attributes}}<tr><td>{{.Label}}</td><td class="muted"><code>{{.Name}}</code></td><td class="actions">
				<button class="danger" type="button" hx-post="/admin/gruppi/attributi/{{.ID}}/elimina" hx-confirm="Togliere l'attributo «{{.Label}}»?" hx-target="#section" hx-swap="outerHTML">Togli</button></td></tr>
			{{else}}<tr><td class="muted">Nessun attributo: aggiungine uno (es. physicalDeliveryOfficeName → Ufficio).</td></tr>{{end}}
		</tbody></table>
		<form class="form inline-form" hx-post="/admin/gruppi/attributi" hx-target="#section" hx-swap="outerHTML">
			<label>Nome LDAP<input name="name" maxlength="64" placeholder="physicalDeliveryOfficeName" required></label>
			<label>Etichetta<input name="label" maxlength="60" placeholder="Ufficio" required></label>
			<button class="primary" type="submit">Aggiungi</button>
		</form>
		{{with .Errors.attr}}<p class="field-error">{{.}}</p>{{end}}
	</div>

	{{with .Edit}}{{template "group_edit" .}}{{end}}

	<div class="card">
		<h2>Gruppi</h2>
		<form class="form inline-form" hx-post="/admin/gruppi" hx-target="#section" hx-swap="outerHTML">
			<label>Nuovo gruppo<input name="name" maxlength="60" required></label>
			<button class="primary" type="submit">Crea</button>
		</form>
		{{with .Errors.name}}<p class="field-error">{{.}}</p>{{end}}
		<table class="list">
			<thead><tr><th>Nome</th><th>Regole</th><th>Usato da</th><th></th></tr></thead>
			<tbody>
			{{range .Groups}}<tr>
				<td>{{.Name}}</td><td>{{.Rules}}</td><td>{{.Uses}} contenuti</td>
				<td class="actions">
					<button class="icon" type="button" title="Su" hx-post="/admin/gruppi/{{.ID}}/sposta" hx-vals='{"dir":"up"}' hx-target="#section" hx-swap="outerHTML"><span class="material-icons" aria-hidden="true">arrow_upward</span></button>
					<button class="icon" type="button" title="Giù" hx-post="/admin/gruppi/{{.ID}}/sposta" hx-vals='{"dir":"down"}' hx-target="#section" hx-swap="outerHTML"><span class="material-icons" aria-hidden="true">arrow_downward</span></button>
					<button type="button" hx-get="/admin/gruppi/{{.ID}}/modifica" hx-target="#section" hx-swap="outerHTML">Regole</button>
					<button class="danger" type="button" hx-post="/admin/gruppi/{{.ID}}/elimina" hx-confirm="Eliminare il gruppo «{{.Name}}»?" hx-target="#section" hx-swap="outerHTML">Elimina</button>
				</td></tr>
			{{else}}<tr><td colspan="4" class="muted">Nessun gruppo.</td></tr>{{end}}
			</tbody>
		</table>
	</div>
</div>
{{end}}

{{define "group_edit"}}
<div class="card">
	<h2>Regole di «{{.Group.Name}}»</h2>
	<form class="form inline-form" hx-post="/admin/gruppi/{{.Group.ID}}" hx-target="#section" hx-swap="outerHTML">
		<label>Nome<input name="name" value="{{.Group.Name}}" maxlength="60" required></label>
		<button type="submit">Rinomina</button>
	</form>
	<p class="hint">Un utente è nel gruppo se soddisfa almeno una regola e non è escluso.</p>
	<ul class="rules">
		{{range .Rules}}<li>{{.Text}} <button class="danger small" type="button" hx-post="/admin/gruppi/{{.GroupID}}/regole/{{.ID}}/elimina" hx-target="#section" hx-swap="outerHTML">Togli</button></li>
		{{else}}<li class="muted">Nessuna regola: il gruppo non ha membri.</li>{{end}}
	</ul>
	<form class="form rule-form" hx-post="/admin/gruppi/{{.Group.ID}}/regole" hx-target="#section" hx-swap="outerHTML">
		<label>Tipo<select name="kind" data-rule-kind>
			<option value="attr">Attributo</option><option value="adgroup">Gruppo AD</option>
			<option value="user">Utente</option><option value="exclude">Escludi utente</option>
		</select></label>
		<label data-for-kind="attr">Attributo<select name="attr">{{range .Attributes}}<option value="{{.Name}}">{{.Label}}</option>{{end}}</select></label>
		<label>Valore<input name="value" autocomplete="off" required
			hx-get="/admin/ad/suggerimenti" hx-trigger="input changed delay:300ms" hx-include="closest form" hx-target="next .suggestions" hx-swap="innerHTML"></label>
		<div class="suggestions"></div>
		<input type="hidden" name="label">
		<button class="primary" type="submit">Aggiungi regola</button>
	</form>
	{{with .Errors.rule}}<p class="field-error">{{.}}</p>{{end}}
	<button type="button" hx-post="/admin/gruppi/{{.Group.ID}}/anteprima" hx-target="#preview" hx-swap="innerHTML">Anteprima membri</button>
	<div id="preview">{{with .Preview}}{{template "group_preview" .}}{{end}}</div>
</div>
{{end}}

{{define "group_preview"}}{{if .Err}}<p class="flash error">{{.Err}}</p>{{else if eq .Count 0}}<p class="muted">Nessun utente corrisponde.</p>{{else}}<p>{{.Count}} {{if eq .Count 1}}utente{{else}}utenti{{end}}{{if gt .Count (len .People)}} (primi {{len .People}}){{end}}:</p><ul class="preview">{{range .People}}<li>{{if .Name}}{{.Name}}{{else}}{{.Username}}{{end}} <span class="muted">{{.Username}}</span></li>{{end}}</ul>{{end}}{{end}}

{{define "ad_suggestions"}}{{if .Err}}<p class="muted">{{.Err}}</p>{{else}}{{range .Items}}<button type="button" class="suggestion" data-fill="{{.Value}}" data-label="{{.Label}}">{{.Text}}</button>{{else}}<p class="muted">Nessun risultato.</p>{{end}}{{end}}{{end}}
```

**Suggerimenti:** il campo valore del form delle regole usa `GET /admin/ad/suggerimenti`, che legge `kind`, `attr` e `value` dal form (`hx-include="closest form"`) e smista su valori dell'attributo, gruppi AD o utenti (per `user`/`exclude`). Le tre route `/admin/ad/valori?attr=&q=`, `/admin/ad/gruppi?q=`, `/admin/ad/utenti?q=` restano come accessi diretti con `q` (usate dai test). Tutte rispondono con il template `ad_suggestions`, dati `struct{ Err string; Items []suggestion }` con `suggestion{Value, Label, Text string}`: per i gruppi AD `Value` = DN, `Label` = nome, `Text` = nome; per valori e utenti `Label` vuoto. L'anteprima risponde con il template `group_preview` (dati `previewView`).

`admin.js`: handler delegato

```js
	// Suggerimenti AD: un click riempie il valore (e l'etichetta del gruppo AD).
	document.addEventListener("click", (e) => {
		const b = e.target.closest(".suggestion");
		if (!b) return;
		const form = b.closest("form");
		if (!form) return;
		form.querySelector('[name="value"]').value = b.dataset.fill || "";
		const label = form.querySelector('[name="label"]');
		if (label) label.value = b.dataset.label || "";
		b.parentElement.innerHTML = "";
	});
	// Il campo "Attributo" serve solo con il tipo "Attributo".
	document.addEventListener("change", (e) => {
		const sel = e.target.closest("[data-rule-kind]");
		if (!sel) return;
		sel.form.querySelectorAll("[data-for-kind]").forEach((el) => { el.hidden = el.dataset.forKind !== sel.value; });
	});
```

(inserito dentro l'IIFE esistente di `admin.js`).

`admin_base.html`: voce nel rail dopo "Categorie": `<a href="/admin/gruppi"{{if eq .Section "gruppi"}} aria-current="page"{{end}}><span class="material-icons" aria-hidden="true">groups</span>Gruppi</a>`.

`admin.css`, in coda:

```css
.inline-form { display: flex; flex-wrap: wrap; gap: .6rem; align-items: flex-end; margin-top: .8rem; }
.rules { list-style: none; padding: 0; margin: .5rem 0 1rem; display: grid; gap: .35rem; }
.rules li { display: flex; justify-content: space-between; align-items: center; gap: .6rem; padding: .35rem .6rem; border: 1px solid var(--line); border-radius: 6px; }
.suggestions { display: flex; flex-wrap: wrap; gap: .3rem; }
.suggestion { border: 1px solid var(--line); background: var(--card); border-radius: 999px; padding: .2rem .7rem; font: inherit; font-size: .85rem; cursor: pointer; }
.suggestion:hover { border-color: var(--accent); }
ul.preview { columns: 2; padding-left: 1.1rem; }
```

- [ ] **Step 5: verifica**

Run: `go test ./... && go vet ./... && gofmt -l internal/`
Expected: PASS.

- [ ] **Step 6: commit**

```bash
git add internal/web web/templates web/static
git commit -m "feat(admin): gruppi della plancia con regole da AD, suggerimenti e anteprima"
```

---

### Task 6: visibilità nei form di applicativi, guide e avvisi

**Files:**
- Create: `web/templates/partials_audience.html`, `internal/web/audience_form.go`, `internal/web/admin_visibility_test.go`
- Modify: `internal/web/admin_apps.go`, `internal/web/admin_guides.go`, `internal/web/admin_alerts.go`
- Modify: `web/templates/admin_app.html`, `web/templates/admin_guide.html`, `web/templates/admin_avvisi.html`

**Interfaces:**
- Consumes: `db.ListAudienceGroups`, `db.GetContentAudience`, `db.SetContentAudience`, `db.AllContentAudience` (Task 2).
- Produces:
  - `type visibilityField struct { Mode string; Selected map[int64]bool; Groups []database.AudienceGroup }`
  - `func (s *Server) visibilityField(ca database.ContentAudience) (visibilityField, error)`
  - `func parseVisibility(r *http.Request, errs formErrors) database.ContentAudience` (campi `visibilita` = `""|only|hide`, `gruppi` ripetuto)
  - `func (s *Server) visibilityLabels(k database.ContentKind) (map[int64]string, error)` (testo per gli elenchi: "Riservato: CED, Ragioneria")
  - template `visibility_fieldset` (dati `visibilityField`) e `visibility_label` (dati `string`)
  - nei tre form: campo `Visibility database.ContentAudience`; nelle tre sezioni: `VisibilityField visibilityField`, `VisibilityLabels map[int64]string`

- [ ] **Step 1: test che falliscono**

`internal/web/admin_visibility_test.go`:

```go
package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

func TestAlertVisibilitySaved(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	ced, _ := db.CreateAudienceGroup("CED")
	page := do(t, s, "GET", "/admin/avvisi", nil, c, nil).Body.String()
	if !strings.Contains(page, `name="visibilita"`) || !strings.Contains(page, "CED") || !strings.Contains(page, "non una protezione") {
		t.Fatal("fieldset Visibilità mancante")
	}
	form := url.Values{"title": {"Solo CED"}, "level": {"news"}, "starts_at": {"2026-10-06T09:00"}, "visibilita": {"only"}, "gruppi": {itoa(ced)}}
	rec := do(t, s, "POST", "/admin/avvisi", form, c, map[string]string{"HX-Request": "true"})
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Riservato: CED") {
		t.Fatalf("salvataggio: %d\n%s", rec.Code, rec.Body)
	}
	all, _ := db.ListActiveAlerts(fixedNow)
	if ca, _ := db.GetContentAudience(database.ContentAlert, all[0].ID); ca.Mode != audience.ModeOnly || len(ca.Groups) != 1 {
		t.Fatalf("visibilità salvata: %+v", ca)
	}
}

func TestVisibilityNeedsGroup(t *testing.T) {
	s, _ := newTestServer(t, nil)
	c := login(t, s)
	form := url.Values{"title": {"x"}, "level": {"news"}, "starts_at": {"2026-10-06T09:00"}, "visibilita": {"hide"}}
	if rec := do(t, s, "POST", "/admin/avvisi", form, c, map[string]string{"HX-Request": "true"}); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Scegli almeno un gruppo") {
		t.Fatalf("Nascosto a senza gruppi: %d", rec.Code)
	}
}

func TestAppVisibilityEditAndList(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	g, _ := db.CreateAudienceGroup("Polizia Locale")
	apps, _ := db.ListApps()
	id := apps[0].ID
	db.SetContentAudience(database.ContentApp, id, database.ContentAudience{Mode: audience.ModeHide, Groups: []int64{g}})
	edit := do(t, s, "GET", "/admin/app/"+itoa(id)+"/modifica", nil, c, map[string]string{"HX-Request": "true"}).Body.String()
	if !strings.Contains(edit, `value="hide" checked`) {
		t.Fatal("la modalità salvata deve risultare selezionata")
	}
	if list := do(t, s, "GET", "/admin/app", nil, c, nil).Body.String(); !strings.Contains(list, "Nascosto a: Polizia Locale") {
		t.Fatal("etichetta negli elenchi mancante")
	}
	rec := postMultipart(t, s, "/admin/app/"+itoa(id), appFields(db, map[string]string{"visibilita": ""}), nil, c)
	if ca, _ := db.GetContentAudience(database.ContentApp, id); rec.Code != 200 || ca.Mode != audience.ModePublic {
		t.Fatalf("ritorno a pubblico: %d %+v", rec.Code, ca)
	}
}
```

Verificare i nomi reali dei campi dei form (`title`, `level`, `starts_at`) e dell'helper `appFields`; adeguare il test, non l'implementazione.

- [ ] **Step 2: verifica che falliscano**

Run: `go test ./internal/web/ -run 'Visibility'`
Expected: FAIL.

- [ ] **Step 3: helper**

`internal/web/audience_form.go`:

```go
package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

type visibilityField struct {
	Mode     string
	Selected map[int64]bool
	Groups   []database.AudienceGroup
}

func (s *Server) visibilityField(ca database.ContentAudience) (visibilityField, error) {
	groups, err := s.db.ListAudienceGroups()
	f := visibilityField{Mode: string(ca.Mode), Selected: map[int64]bool{}, Groups: groups}
	for _, g := range ca.Groups {
		f.Selected[g] = true
	}
	return f, err
}

// parseVisibility legge "visibilita" e "gruppi"; con Pubblico i gruppi sono ignorati.
func parseVisibility(r *http.Request, errs formErrors) database.ContentAudience {
	mode := r.FormValue("visibilita")
	if !audience.ValidMode(mode) {
		errs.add("visibilita", "Visibilità non valida.")
		return database.ContentAudience{Groups: []int64{}}
	}
	ca := database.ContentAudience{Mode: audience.Mode(mode), Groups: []int64{}}
	if ca.Mode == audience.ModePublic {
		return ca
	}
	for _, v := range r.Form["gruppi"] {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil && id > 0 {
			ca.Groups = append(ca.Groups, id)
		}
	}
	if len(ca.Groups) == 0 {
		errs.add("visibilita", "Scegli almeno un gruppo.")
	}
	return ca
}

// visibilityLabels: testo per gli elenchi admin ("" = pubblico).
func (s *Server) visibilityLabels(k database.ContentKind) (map[int64]string, error) {
	all, err := s.db.AllContentAudience(k)
	if err != nil {
		return nil, err
	}
	groups, err := s.db.ListAudienceGroups()
	if err != nil {
		return nil, err
	}
	names := map[int64]string{}
	for _, g := range groups {
		names[g.ID] = g.Name
	}
	out := map[int64]string{}
	for id, ca := range all {
		var ns []string
		for _, g := range ca.Groups {
			ns = append(ns, names[g])
		}
		prefix := "Riservato: "
		if ca.Mode == audience.ModeHide {
			prefix = "Nascosto a: "
		}
		out[id] = prefix + strings.Join(ns, ", ")
	}
	return out, nil
}
```

Gli id dei gruppi vanno verificati nel salvataggio (un id inesistente farebbe fallire l'insert per la chiave esterna). Aggiungere in `audience_form.go`:

```go
// checkGroups: ogni gruppo scelto deve esistere.
func (s *Server) checkGroups(ca database.ContentAudience, errs formErrors) error {
	groups, err := s.db.ListAudienceGroups()
	if err != nil {
		return err
	}
	exists := map[int64]bool{}
	for _, g := range groups {
		exists[g.ID] = true
	}
	for _, id := range ca.Groups {
		if !exists[id] {
			errs.add("visibilita", "Gruppo non valido.")
			break
		}
	}
	return nil
}
```

e chiamarlo negli handler di salvataggio subito dopo `parseVisibility` (errore → `s.serverError`).

`web/templates/partials_audience.html`:

```html
{{define "visibility_fieldset"}}
<fieldset class="visibility">
	<legend>Visibilità</legend>
	<label class="inline"><input type="radio" name="visibilita" value=""{{if eq .Mode ""}} checked{{end}}>Pubblico</label>
	<label class="inline"><input type="radio" name="visibilita" value="only"{{if eq .Mode "only"}} checked{{end}}>Riservato a</label>
	<label class="inline"><input type="radio" name="visibilita" value="hide"{{if eq .Mode "hide"}} checked{{end}}>Nascosto a</label>
	<div class="offices-grid">
		{{$sel := .Selected}}{{range .Groups}}<label class="inline"><input type="checkbox" name="gruppi" value="{{.ID}}"{{if index $sel .ID}} checked{{end}}>{{.Name}}</label>{{else}}<p class="muted">Nessun gruppo: creali in <a href="/admin/gruppi">Gruppi</a>.</p>{{end}}
	</div>
	<p class="hint">Filtro di visualizzazione, non una protezione: chi conosce l'indirizzo può comunque aprire l'applicativo e chiunque può scegliere «Mostra tutto». Con «Pubblico» i gruppi sono ignorati.</p>
</fieldset>
{{end}}

{{define "visibility_label"}}{{if .}}{{.}}{{else}}Pubblico{{end}}{{end}}
```

`admin.css`: `.offices-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(190px, 1fr)); gap: .25rem .9rem; margin: .3rem 0; }`.

- [ ] **Step 4: i tre form**

Per ognuno (`admin_apps.go` → `database.ContentApp`, `admin_guides.go` → `database.ContentGuide`, `admin_alerts.go` → `database.ContentAlert`):

1. form struct: `Visibility database.ContentAudience`; costruttori del form nuovo con `Visibility: database.ContentAudience{Groups: []int64{}}`.
2. section struct: `VisibilityField visibilityField` e `VisibilityLabels map[int64]string`; in `xxxData`: `sec.VisibilityField, err = s.visibilityField(form.Visibility)` e `sec.VisibilityLabels, err = s.visibilityLabels(<kind>)`.
3. handler `…Edit`: dopo il form dall'elemento, `form.Visibility, err = s.db.GetContentAudience(<kind>, id)`.
4. handler `…Save`: prima del controllo errori, `form.Visibility = parseVisibility(r, errs)` (guide e avvisi: assicurarsi che `r.ParseForm()` sia stato chiamato; per le app `ParseMultipartForm` popola già `r.Form`), poi `s.checkGroups(form.Visibility, errs)`. Dopo `Create`/`Update` riusciti, con l'id salvato: `s.db.SetContentAudience(<kind>, savedID, form.Visibility)`; errore → `s.serverError`.
5. template del form: prima di `<div class="actions">` `{{template "visibility_fieldset" .VisibilityField}}{{with .Errors.visibilita}}<p class="field-error">{{.}}</p>{{end}}`.
6. template dell'elenco, nella cella del titolo: `<br><small class="muted">{{template "visibility_label" (index $.VisibilityLabels .ID)}}</small>`. Negli avvisi (`alert_rows` riceve una lista) aggiungere `Visibility string` ad `alertRow`, popolato in `alertsData` dalla mappa, e usare `{{template "visibility_label" .Visibility}}`.

- [ ] **Step 5: verifica**

Run: `go test ./... && go vet ./... && gofmt -l internal/`
Expected: PASS.

- [ ] **Step 6: commit**

```bash
git add internal/web web/templates web/static/css/admin.css
git commit -m "feat(admin): visibilità di applicativi, guide e avvisi per gruppi della plancia"
```

---

### Task 7: plancia filtrata e "Mostra tutto"

**Files:**
- Create: `internal/web/audience_filter.go`, `internal/web/audience_filter_test.go`
- Modify: `internal/web/dashboard.go`, `internal/database/alerts.go` (campo `NotForViewer`), `web/templates/dashboard.html`, `web/templates/partials_dashboard.html`, `web/static/js/dashboard.js`, `web/static/css/plancia.css`

**Interfaces:**
- Consumes: `s.viewerGroups` (Task 4), `db.AllContentAudience` (Task 2), `audience.Visible` (Task 1).
- Produces:
  - `database.Alert.NotForViewer bool` (calcolato dal web, non salvato)
  - `const showAllCookie = "cruscotto_tutto"`
  - `type contentFilter struct { memberOf map[int64]bool; known, ShowAll bool; Hidden int; apps, guides, alerts map[int64]database.ContentAudience }`
  - `func (s *Server) contentFilterFor(r *http.Request) (*contentFilter, error)`
  - `func (f *contentFilter) dashboard(d *database.Dashboard)`; `func (f *contentFilter) alertList(list []database.Alert) []database.Alert`
  - in `dashboardView`: `Filter *contentFilter`

- [ ] **Step 1: test che falliscono**

`internal/web/audience_filter_test.go`:

```go
package web

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
)

// seedVisibility: gruppo "Tributi" (ufficio TRIBUTI), Rubrica pubblica,
// Webmail riservata a Tributi, guida generale nascosta a Tributi, urgente
// riservato a un gruppo senza membri, novità pubblica.
func seedVisibility(t *testing.T, db *database.DB) {
	t.Helper()
	db.CreateAudienceAttribute("physicalDeliveryOfficeName", "Ufficio")
	trib, _ := db.CreateAudienceGroup("Tributi")
	db.AddAudienceRule(database.AudienceRule{GroupID: trib, Kind: audience.KindAttr, Attr: "physicalDeliveryOfficeName", Value: "TRIBUTI"})
	nessuno, _ := db.CreateAudienceGroup("Nessuno")

	apps, _ := db.ListApps()
	for _, a := range apps {
		a.URL = "https://example.it/" + strings.ToLower(a.Title)
		if err := db.UpdateApp(a); err != nil {
			t.Fatal(err)
		}
		if a.Title == "Webmail" {
			db.SetContentAudience(database.ContentApp, a.ID, database.ContentAudience{Mode: audience.ModeOnly, Groups: []int64{trib}})
		}
	}
	gid, _ := db.CreateGuide(database.Guide{Title: "Guida non per Tributi", URL: "https://example.it/g", Kind: "link", Enabled: true})
	db.SetContentAudience(database.ContentGuide, gid, database.ContentAudience{Mode: audience.ModeHide, Groups: []int64{trib}})
	start := fixedNow.Add(-time.Hour)
	uid, _ := db.CreateAlert(database.Alert{Title: "Urgente riservato", Level: database.LevelUrgent, StartsAt: start})
	db.SetContentAudience(database.ContentAlert, uid, database.ContentAudience{Mode: audience.ModeOnly, Groups: []int64{nessuno}})
	db.CreateAlert(database.Alert{Title: "Novità per tutti", Level: database.LevelNews, StartsAt: start})
}

func viewerCookie(t *testing.T, s *Server, u identity.User) *http.Cookie {
	t.Helper()
	v, err := s.cookies.Encode(u)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: identity.CookieName, Value: v}
}

func TestDashboardFilteredForMember(t *testing.T) {
	s, db := newTestServer(t, nil)
	seedVisibility(t, db)
	body := do(t, s, "GET", "/", nil, viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi"}), nil).Body.String()
	for _, want := range []string{"Rubrica", "Webmail", "Novità per tutti", "Mostra anche i contenuti non destinati a te (2)"} {
		if !strings.Contains(body, want) {
			t.Errorf("manca %q", want)
		}
	}
	for _, no := range []string{"Guida non per Tributi", "Urgente riservato"} {
		if strings.Contains(body, no) {
			t.Errorf("non doveva esserci %q", no)
		}
	}
}

func TestDashboardAnonymousOnlyPublic(t *testing.T) {
	s, db := newTestServer(t, nil)
	seedVisibility(t, db)
	body := do(t, s, "GET", "/", nil, viewerCookie(t, s, identity.User{Anonymous: true}), nil).Body.String()
	if !strings.Contains(body, "Rubrica") || strings.Contains(body, "Webmail") || strings.Contains(body, "Guida non per Tributi") {
		t.Fatal("anonimo: solo pubblici (anche i «Nascosto a» restano nascosti)")
	}
}

func TestDashboardShowAllNoUrgentPopup(t *testing.T) {
	s, db := newTestServer(t, nil)
	seedVisibility(t, db)
	h := map[string]string{"Cookie": viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi"}).String() + "; " + showAllCookie + "=1"}
	body := do(t, s, "GET", "/", nil, nil, h).Body.String()
	if !strings.Contains(body, "Urgente riservato") || !strings.Contains(body, "Mostra solo i miei contenuti") {
		t.Fatal("con Mostra tutto l'urgente compare nel carosello")
	}
	if strings.Contains(body, `<dialog class="urgent"`) {
		t.Fatal("niente popup per un urgente non destinato all'utente")
	}
}

func TestAvvisiAndPartialFilteredByAudience(t *testing.T) {
	s, db := newTestServer(t, nil)
	seedVisibility(t, db)
	c := viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi"})
	for _, path := range []string{"/avvisi", "/partials/alerts"} {
		if body := do(t, s, "GET", path, nil, c, nil).Body.String(); strings.Contains(body, "Urgente riservato") || !strings.Contains(body, "Novità per tutti") {
			t.Errorf("%s: filtro avvisi mancante", path)
		}
	}
}

func TestProfileCachedAcrossPages(t *testing.T) {
	calls := 0
	dir := testDirectory
	dir.calls = &calls
	s, db := newTestServerWith(t, nil, func(o *Options) { o.Directory = dir })
	seedVisibility(t, db)
	c := viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi"})
	for i := 0; i < 3; i++ {
		do(t, s, "GET", "/", nil, c, nil)
	}
	if calls != 1 {
		t.Fatalf("profilo letto da AD %d volte invece di 1", calls)
	}
}
```

Verificare i nomi reali (`ListApps` restituisce `[]App`, `UpdateApp(App)`, campi di `Guide`) e adeguare il test.

- [ ] **Step 2: verifica che falliscano**

Run: `go test ./internal/web/ -run 'FilteredForMember|AnonymousOnlyPublic|ShowAllNoUrgent|FilteredByAudience|ProfileCached'`
Expected: FAIL.

- [ ] **Step 3: filtro**

`internal/database/alerts.go`, struct `Alert`: `NotForViewer bool // calcolato dal web: avviso non destinato a chi guarda (niente popup)`.

`internal/web/audience_filter.go`:

```go
package web

import (
	"net/http"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

// showAllCookie: preferenza "Mostra tutto", impostata da dashboard.js.
const showAllCookie = "cruscotto_tutto"

// contentFilter applica la visibilità dei contenuti a chi guarda. È
// presentazione, non sicurezza: con "Mostra tutto" si vede ogni contenuto.
type contentFilter struct {
	memberOf             map[int64]bool
	known, ShowAll       bool
	Hidden               int
	apps, guides, alerts map[int64]database.ContentAudience
}

func (s *Server) contentFilterFor(r *http.Request) (*contentFilter, error) {
	f := &contentFilter{}
	f.memberOf, f.known = s.viewerGroups(r)
	if c, err := r.Cookie(showAllCookie); err == nil && c.Value == "1" {
		f.ShowAll = true
	}
	var err error
	if f.apps, err = s.db.AllContentAudience(database.ContentApp); err != nil {
		return nil, err
	}
	if f.guides, err = s.db.AllContentAudience(database.ContentGuide); err != nil {
		return nil, err
	}
	if f.alerts, err = s.db.AllContentAudience(database.ContentAlert); err != nil {
		return nil, err
	}
	return f, nil
}

func (f *contentFilter) visible(ca database.ContentAudience) bool {
	return audience.Visible(ca.Mode, ca.Groups, f.memberOf, f.known)
}

// keep conta ciò che esclude e dice se l'elemento resta in pagina.
func (f *contentFilter) keep(visible bool) bool {
	if visible || f.ShowAll {
		return true
	}
	f.Hidden++
	return false
}

func (f *contentFilter) dashboard(d *database.Dashboard) {
	d.Alerts = f.alertList(d.Alerts)
	cats := d.Categories[:0]
	for _, c := range d.Categories {
		kept := c.Apps[:0]
		for _, a := range c.Apps {
			if !f.keep(f.visible(f.apps[a.ID])) {
				continue
			}
			gs := a.Guides[:0]
			for _, g := range a.Guides {
				if f.keep(f.visible(f.guides[g.ID])) {
					gs = append(gs, g)
				}
			}
			a.Guides = gs
			kept = append(kept, a)
		}
		if len(kept) > 0 {
			c.Apps = kept
			cats = append(cats, c)
		}
	}
	d.Categories = cats
	gen := d.GeneralGuides[:0]
	for _, g := range d.GeneralGuides {
		if f.keep(f.visible(f.guides[g.ID])) {
			gen = append(gen, g)
		}
	}
	d.GeneralGuides = gen
}

// alertList: con "Mostra tutto" gli avvisi non destinati restano, marcati,
// così non aprono il popup degli urgenti.
func (f *contentFilter) alertList(list []database.Alert) []database.Alert {
	out := list[:0]
	for _, a := range list {
		v := f.visible(f.alerts[a.ID])
		if !f.keep(v) {
			continue
		}
		a.NotForViewer = !v
		out = append(out, a)
	}
	return out
}
```

`internal/web/dashboard.go`:
- `dashboardView` riceve `Filter *contentFilter`;
- `handleDashboard`, dopo `GetDashboard`: `f, err := s.contentFilterFor(r)` (errore → `s.serverError`), `f.dashboard(&d)` e `Filter: f` nel letterale;
- `handleAlertsPartial` e `handleAvvisi`: dopo `ListActiveAlerts`, `f, err := s.contentFilterFor(r)` → `alerts = f.alertList(alerts)`.

- [ ] **Step 4: template, JS, CSS**

`dashboard.html`, dopo la `</label>` della ricerca:

```html
	{{with .Filter}}{{if or .Hidden .ShowAll}}<div class="audience-toggle"><button type="button" data-mostra-tutto="{{if .ShowAll}}0{{else}}1{{end}}" hidden>{{if .ShowAll}}Mostra solo i miei contenuti{{else}}Mostra anche i contenuti non destinati a te ({{.Hidden}}){{end}}</button></div>{{end}}{{end}}
```

`partials_dashboard.html`, ciclo degli urgenti: `{{range .}}{{if eq .Level "urgent"}}` → `{{range .}}{{if and (eq .Level "urgent") (not .NotForViewer)}}`.

`dashboard.js`, prima di `initCarousel();` in fondo:

```js
	// "Mostra tutto": preferenza in un cookie letto dal server, poi ricarica.
	const audienceToggle = document.querySelector("[data-mostra-tutto]");
	if (audienceToggle) {
		audienceToggle.hidden = false;
		audienceToggle.addEventListener("click", () => {
			document.cookie = audienceToggle.dataset.mostraTutto === "1"
				? "cruscotto_tutto=1; Path=/; Max-Age=31536000; SameSite=Lax"
				: "cruscotto_tutto=; Path=/; Max-Age=0; SameSite=Lax";
			location.reload();
		});
	}
```

`plancia.css`, dopo `.hero-compact`/regole della testata:

```css
.audience-toggle { margin: .7rem var(--p-gutter) 0; text-align: right; }
.audience-toggle button { border: 0; background: none; color: var(--p-blue-2); font: inherit; font-size: .82rem; font-weight: 600; cursor: pointer; padding: .2rem 0; }
.audience-toggle button:hover { text-decoration: underline; }
```

- [ ] **Step 5: verifica**

Run: `go test ./... && go vet ./... && gofmt -l internal/`
Expected: PASS.

- [ ] **Step 6: commit**

```bash
git add internal web
git commit -m "feat(plancia): contenuti filtrati per gruppi della plancia con Mostra tutto"
```

---

### Task 8: verifica manuale e documentazione

**Files:**
- Modify: `CLAUDE.md`, `docs/superpowers/specs/2026-10-06-filtri-contenuti-design.md` (stato)

- [ ] **Step 1: prova manuale (mock)**

Avviare con `LDAP_HOST=mock NTLM_DOMAIN=<dominio del PC> SECURE_COOKIES=false PORT=18091 DB_PATH=<scratch>/f.db UPLOAD_DIR=<scratch>/up`:
- in `/admin/gruppi` aggiungere l'attributo `physicalDeliveryOfficeName` → "Ufficio", creare "CED" con regola Ufficio = INFORMATIZZAZIONE (scegliendola dai suggerimenti) e "Altri" con Ufficio = TRIBUTI; anteprima di CED;
- "Webmail" (con URL) *Riservato a* Altri, un avviso *Riservato a* CED;
- in Edge headless (`--dump-dom`, profilo nella scratchpad) aprire `/`: dopo il riconoscimento Webmail assente, avviso presente, pulsante "Mostra anche… (1)".

- [ ] **Step 2: CLAUDE.md**

- Architettura: punto **`internal/audience`** (Member, Visible; filtro di presentazione); tabelle `audience_attributes`, `audience_groups`, `audience_rules`, `content_audience` (v5); profilo AD con gruppi annidati e cache 15 min / 1 min; pagina `/admin/gruppi` e suggerimenti `/admin/ad/*`; anonimo = solo pubblici; gruppo/attributo in uso non eliminabili; cookie `cruscotto_tutto`.
- Sezione "Identificazione utente": togliere il rimando ai filtri "fase successiva" e indicare che sono implementati.

- [ ] **Step 3: spec**

`Stato: in revisione` → `Stato: implementata`.

- [ ] **Step 4: verifica finale**

Run: `go vet ./... && go test ./... && gofmt -l internal/ cmd/`
Expected: tutto PASS.

- [ ] **Step 5: commit**

```bash
git add CLAUDE.md docs/superpowers/specs/2026-10-06-filtri-contenuti-design.md
git commit -m "docs: filtri per gruppi della plancia in CLAUDE.md"
```
