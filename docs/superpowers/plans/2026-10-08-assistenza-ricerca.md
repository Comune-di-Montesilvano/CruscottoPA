# Assistenza, ricerca a tendina e contatti nella testata — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** canali di assistenza collegati a più applicativi e ben visibili in plancia, ricerca a tendina con risultati raggruppati, riga dei contatti sotto il saluto configurata dall'admin (0.9.0).

**Architecture:** migrazione v8 (tabelle `support_channels`, `app_support`; colonne `hero`, `hero_kind` su `audience_attributes`). I canali entrano in `database.Dashboard` accanto alle guide di ogni app, così il filtro per gruppi esistente (`contentFilter.dashboard`) li tiene o li toglie insieme all'app. La ricerca usa un indice JSON scritto nella pagina (`<script type="application/json">`, non eseguito: CSP invariata) e un combobox in `dashboard.js`. La testata legge gli attributi marcati dal profilo AD già in cache.

**Tech Stack:** Go 1.27, `modernc.org/sqlite`, `html/template`, HTMX 2.0.4, JS vanilla, CSS.

**Spec:** `docs/superpowers/specs/2026-10-08-assistenza-ricerca-design.md`

## Global Constraints

- Migrazioni: **mai modificare una rilasciata**; v8 in coda a `migrations` in `internal/database/migrations.go`.
- CSP stretta: niente `<script>`/`<style>` inline eseguibili né `on*=`; JSON in `<script type="application/json">` è ammesso (non eseguito).
- Admin: ogni azione HTMX restituisce l'intera sezione `<div id="section">`; 200 se ok, **422 con errori**.
- Link dei canali: solo `http`/`https` assoluti (`validURL` in `internal/web/validate.go`).
- Testi UI in italiano; date/ID come nel resto del codice.
- **Test su Windows**: `go test ./internal/web/` locale esce con 0 a metà (vedi CLAUDE.md). Lanciare SEMPRE i test in container:
  `MSYS_NO_PATHCONV=1 docker run --rm -v "$(pwd -W)":/src -v "$(pwd -W)/.devcache":/cache -e GOPATH=/cache/gopath -e GOCACHE=/cache/build -e CGO_ENABLED=0 -w /src golang:1.27-alpine go test ./...`
  (nei passi sotto: **`GOTEST <pacchetto> [-run X]`** = questo comando con pacchetto e filtro al posto di `./...`).
- JS: sintassi con `node -e "new Function(require('fs').readFileSync('web/static/js/dashboard.js','utf8'))"`; comportamento fissato da test Go che leggono il file.
- Modifiche multi-riga via script: scriverlo su file nello scratchpad e lanciarlo con `python -X utf8 <file>` (gli heredoc di Git Bash alterano apici e `\n`).
- Commit su branch `assistenza-ricerca` (esiste, con la spec); messaggi in italiano, chiusi da `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. **Canale collegato solo ad app nascoste al visitatore** → non deve comparire né sulla tile né nella ricerca (tranne con "Mostra tutto"). Test in Task 7.
2. **Titoli con caratteri speciali o `</script>` in un nome** → l'indice JSON non deve rompere la pagina (html/template escapa nel contesto JS). Test in Task 7.
3. **Attributo AD della testata a più valori o con soli spazi** → si mostra il primo valore non vuoto, gli spazi non producono una voce vuota. Test in Task 6.
4. **Eliminazione di un applicativo collegato** → collegamenti tolti, canale resta e in admin risulta "Non collegato a nessun applicativo". Test in Task 1 (DB) e Task 3 (admin).
5. **Ricerca con accenti/maiuscole ("perche" trova "Perché") e tasti a tendina chiusa** → nessun errore JS, `/` e Esc continuano a funzionare. Test in Task 8 (file JS) + prova Playwright.

---

### Task 1: DB — migrazione v8 e canali di assistenza

**Files:**
- Modify: `internal/database/migrations.go` (aggiungere `migrateV8SupportAndHero` in coda)
- Create: `internal/database/support.go`
- Modify: `internal/database/dashboard.go` (canali sulla tile)
- Test: `internal/database/support_test.go`

**Interfaces:**
- Produces:
  - `type SupportChannel struct { ID int64; Title, URL, Note string; SortOrder int; Enabled bool; AppIDs []int64 }`
  - `func (db *DB) ListSupportChannels() ([]SupportChannel, error)` — tutti, ordine `sort_order, title, id`, con `AppIDs` (mai nil)
  - `func (db *DB) GetSupportChannel(id int64) (SupportChannel, error)` — `ErrNotFound`
  - `func (db *DB) CreateSupportChannel(c SupportChannel) (int64, error)` — `sort_order` in coda, scrive anche i collegamenti
  - `func (db *DB) UpdateSupportChannel(c SupportChannel) error` — sostituisce i collegamenti; `ErrNotFound`
  - `func (db *DB) DeleteSupportChannel(id int64) error`
  - `func (db *DB) MoveSupportChannel(id int64, dir int) error`
  - `func (db *DB) SupportByApp() (map[int64][]SupportChannel, error)` — solo canali attivi, per app
  - `AppWithGuides.Support []SupportChannel` (mai nil) in `GetDashboard`

- [ ] **Step 1: Test che fallisce** — `internal/database/support_test.go`:

```go
package database

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestSupportChannels(t *testing.T) {
	db := newTestDB(t)
	apps, _ := db.ListApps() // seed: Rubrica, Webmail
	rub, mail := apps[0].ID, apps[1].ID
	id, err := db.CreateSupportChannel(SupportChannel{Title: "Portale Maggioli", URL: "https://assistenza.example", Note: "serve l'utenza", Enabled: true, AppIDs: []int64{rub, mail}})
	if err != nil {
		t.Fatal(err)
	}
	ced, _ := db.CreateSupportChannel(SupportChannel{Title: "CED", URL: "https://ced.example", Enabled: true, AppIDs: []int64{rub}})
	c, err := db.GetSupportChannel(id)
	if err != nil || c.Title != "Portale Maggioli" || !reflect.DeepEqual(c.AppIDs, []int64{rub, mail}) {
		t.Fatalf("Get: %+v %v", c, err)
	}
	c.AppIDs = []int64{mail}
	c.Note = ""
	if err := db.UpdateSupportChannel(c); err != nil {
		t.Fatal(err)
	}
	by, _ := db.SupportByApp()
	if len(by[rub]) != 1 || by[rub][0].ID != ced || len(by[mail]) != 1 || by[mail][0].ID != id {
		t.Fatalf("SupportByApp: %+v", by)
	}
	if err := db.MoveSupportChannel(ced, -1); err != nil {
		t.Fatal(err)
	}
	all, _ := db.ListSupportChannels()
	if len(all) != 2 || all[0].ID != ced {
		t.Fatalf("ordine: %+v", all)
	}
	// Review focus 4: app eliminata → collegamento tolto, canale resta.
	if err := db.DeleteApp(mail); err != nil {
		t.Fatal(err)
	}
	c, _ = db.GetSupportChannel(id)
	if len(c.AppIDs) != 0 {
		t.Fatalf("collegamento all'app eliminata: %+v", c.AppIDs)
	}
	if err := db.DeleteSupportChannel(id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetSupportChannel(id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("dopo Delete: %v", err)
	}
	if err := db.UpdateSupportChannel(SupportChannel{ID: 999, Title: "x", URL: "https://x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Update inesistente: %v", err)
	}
}

func TestDashboardSupport(t *testing.T) {
	db := newTestDB(t)
	apps, _ := db.ListApps()
	a := apps[0]
	a.URL = "https://rubrica.local"
	db.UpdateApp(a)
	db.CreateSupportChannel(SupportChannel{Title: "Attivo", URL: "https://a", Enabled: true, AppIDs: []int64{a.ID}})
	db.CreateSupportChannel(SupportChannel{Title: "Spento", URL: "https://b", Enabled: false, AppIDs: []int64{a.ID}})
	d, err := db.GetDashboard(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	got := d.Categories[0].Apps[0].Support
	if len(got) != 1 || got[0].Title != "Attivo" {
		t.Fatalf("canali sulla tile: %+v", got)
	}
}
```

Verifica nomi: `DeleteApp` esiste in `internal/database/apps.go` (`grep -n "func (db \*DB) DeleteApp" internal/database/apps.go`); se ha un altro nome usare quello.

- [ ] **Step 2: Verifica che fallisca** — `GOTEST ./internal/database/ -run 'TestSupport|TestDashboardSupport'` → FAIL `undefined: SupportChannel`.

- [ ] **Step 3: Migrazione** — in `migrations.go` aggiungere `migrateV8SupportAndHero` all'elenco `migrations` dopo `migrateV7Guides`, e in fondo al file:

```go
// migrateV8SupportAndHero: canali di assistenza collegati agli applicativi e
// attributi AD mostrati sotto il saluto.
func migrateV8SupportAndHero(tx *sql.Tx) error {
	_, err := tx.Exec(`
CREATE TABLE support_channels (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	title      TEXT    NOT NULL,
	url        TEXT    NOT NULL,
	note       TEXT    NOT NULL DEFAULT '',
	sort_order INTEGER NOT NULL DEFAULT 0,
	enabled    INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE app_support (
	app_id     INTEGER NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
	channel_id INTEGER NOT NULL REFERENCES support_channels(id) ON DELETE CASCADE,
	PRIMARY KEY (app_id, channel_id)
);
CREATE INDEX idx_app_support_channel ON app_support(channel_id);
ALTER TABLE audience_attributes ADD COLUMN hero INTEGER NOT NULL DEFAULT 0;
ALTER TABLE audience_attributes ADD COLUMN hero_kind TEXT NOT NULL DEFAULT 'text';
`)
	return err
}
```

(`PRAGMA foreign_keys` è già attivo: le guide usano `ON DELETE SET NULL`.)

- [ ] **Step 4: `internal/database/support.go`**

```go
package database

import (
	"database/sql"
	"strings"
)

// SupportChannel: dove chiedere aiuto per uno o più applicativi (es. portale
// di assistenza del fornitore). AppIDs mai nil.
type SupportChannel struct {
	ID        int64
	Title     string
	URL       string
	Note      string
	SortOrder int
	Enabled   bool
	AppIDs    []int64
}

const supportOrder = `sort_order, title COLLATE NOCASE, id`

func (db *DB) ListSupportChannels() ([]SupportChannel, error) {
	rows, err := db.Query(`SELECT id, title, url, note, sort_order, enabled FROM support_channels ORDER BY ` + supportOrder)
	if err != nil {
		return nil, err
	}
	out := []SupportChannel{}
	idx := map[int64]int{}
	for rows.Next() {
		c := SupportChannel{AppIDs: []int64{}}
		if err := rows.Scan(&c.ID, &c.Title, &c.URL, &c.Note, &c.SortOrder, &c.Enabled); err != nil {
			rows.Close()
			return nil, err
		}
		idx[c.ID] = len(out)
		out = append(out, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	links, err := db.Query(`SELECT s.channel_id, s.app_id FROM app_support s
JOIN apps a ON a.id = s.app_id JOIN categories c ON c.id = a.category_id
ORDER BY c.sort_order, c.name COLLATE NOCASE, a.sort_order, a.title COLLATE NOCASE, a.id`)
	if err != nil {
		return nil, err
	}
	defer links.Close()
	for links.Next() {
		var ch, app int64
		if err := links.Scan(&ch, &app); err != nil {
			return nil, err
		}
		if i, ok := idx[ch]; ok {
			out[i].AppIDs = append(out[i].AppIDs, app)
		}
	}
	return out, links.Err()
}

func (db *DB) GetSupportChannel(id int64) (SupportChannel, error) {
	all, err := db.ListSupportChannels()
	if err != nil {
		return SupportChannel{}, err
	}
	for _, c := range all {
		if c.ID == id {
			return c, nil
		}
	}
	return SupportChannel{}, ErrNotFound
}

func (db *DB) CreateSupportChannel(c SupportChannel) (id int64, err error) {
	err = db.inTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`INSERT INTO support_channels (title, url, note, enabled, sort_order)
VALUES (?, ?, ?, ?, (SELECT COALESCE(MAX(sort_order), -1) + 1 FROM support_channels))`,
			strings.TrimSpace(c.Title), strings.TrimSpace(c.URL), strings.TrimSpace(c.Note), c.Enabled)
		if err != nil {
			return err
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
		return setSupportApps(tx, id, c.AppIDs)
	})
	return id, err
}

func (db *DB) UpdateSupportChannel(c SupportChannel) error {
	return db.inTx(func(tx *sql.Tx) error {
		if err := checkAffected(tx.Exec(`UPDATE support_channels SET title = ?, url = ?, note = ?, enabled = ? WHERE id = ?`,
			strings.TrimSpace(c.Title), strings.TrimSpace(c.URL), strings.TrimSpace(c.Note), c.Enabled, c.ID)); err != nil {
			return err
		}
		return setSupportApps(tx, c.ID, c.AppIDs)
	})
}

func setSupportApps(tx *sql.Tx, channel int64, apps []int64) error {
	if _, err := tx.Exec(`DELETE FROM app_support WHERE channel_id = ?`, channel); err != nil {
		return err
	}
	for _, a := range apps {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO app_support (app_id, channel_id) VALUES (?, ?)`, a, channel); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) DeleteSupportChannel(id int64) error {
	return checkAffected(db.Exec(`DELETE FROM support_channels WHERE id = ?`, id))
}

func (db *DB) MoveSupportChannel(id int64, dir int) error {
	return db.moveRow("support_channels", "1 = 1", nil, supportOrder, id, dir)
}

// SupportByApp: canali attivi per applicativo, nell'ordine dell'admin.
func (db *DB) SupportByApp() (map[int64][]SupportChannel, error) {
	all, err := db.ListSupportChannels()
	if err != nil {
		return nil, err
	}
	out := map[int64][]SupportChannel{}
	for _, c := range all {
		if !c.Enabled {
			continue
		}
		for _, a := range c.AppIDs {
			out[a] = append(out[a], c)
		}
	}
	return out, nil
}
```

Prima di scrivere: `grep -n "func (db \*DB) inTx\|func checkAffected" internal/database/*.go` per confermare firme (`inTx(func(*sql.Tx) error) error`, `checkAffected(sql.Result, error) error`).

- [ ] **Step 5: canali nella plancia** — in `dashboard.go`:

```go
type AppWithGuides struct {
	App
	Guides  []Guide
	Support []SupportChannel // canali di assistenza attivi
}
```

In `GetDashboard`, nella creazione della tile: `AppWithGuides{App: a, Guides: []Guide{}, Support: []SupportChannel{}}`; dopo `attachGuides(&d, index, guides)`:

```go
	support, err := db.SupportByApp()
	if err != nil {
		return d, err
	}
	for id, p := range index {
		if cs := support[id]; len(cs) > 0 {
			d.Categories[p.cat].Apps[p.app].Support = cs
		}
	}
```

- [ ] **Step 6: Verifica** — `GOTEST ./internal/database/` → PASS (anche `TestOpenAppliesMigrationsAndSeed`, che usa `len(migrations)`).

- [ ] **Step 7: Commit**

```bash
git add internal/database/migrations.go internal/database/support.go internal/database/support_test.go internal/database/dashboard.go
git commit -m "Migrazione v8: canali di assistenza collegati agli applicativi

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: DB — attributi AD sotto il saluto

**Files:**
- Modify: `internal/database/audience.go` (`AudienceAttribute`, `ListAudienceAttributes`, nuove funzioni)
- Test: `internal/database/audience_test.go`

**Interfaces:**
- Consumes: colonne `hero`, `hero_kind` (Task 1)
- Produces:
  - `AudienceAttribute{ID int64; Name, Label string; Hero int; HeroKind string}` (`Hero` 0 = non mostrato, altrimenti posizione 1..n)
  - `const HeroText, HeroPhone, HeroMail = "text", "phone", "mail"`; `func ValidHeroKind(k string) bool`
  - `func (db *DB) SetAttributeHero(id int64, kind string) error` — `kind == ""` toglie (e rinumera), altrimenti mostra con quel formato (in coda se nuovo); `ErrNotFound`
  - `func (db *DB) MoveAttributeHero(id int64, dir int) error` — solo tra quelli mostrati
  - `func (db *DB) HeroAttributes() ([]AudienceAttribute, error)` — mostrati, in ordine di `hero`

- [ ] **Step 1: Test** — in coda a `internal/database/audience_test.go`:

```go
func TestAttributeHero(t *testing.T) {
	db := newTestDB(t)
	off, _ := db.CreateAudienceAttribute("physicalDeliveryOfficeName", "Ufficio")
	tel, _ := db.CreateAudienceAttribute("telephoneNumber", "Interno")
	mail, _ := db.CreateAudienceAttribute("mail", "Email")
	if err := db.SetAttributeHero(tel, HeroPhone); err != nil {
		t.Fatal(err)
	}
	db.SetAttributeHero(off, HeroText)
	db.SetAttributeHero(mail, HeroMail)
	if err := db.SetAttributeHero(off, "boh"); err == nil {
		t.Fatal("formato non valido accettato")
	}
	db.MoveAttributeHero(off, -1)
	h, _ := db.HeroAttributes()
	if len(h) != 3 || h[0].ID != off || h[1].ID != tel || h[2].HeroKind != HeroMail {
		t.Fatalf("ordine: %+v", h)
	}
	db.SetAttributeHero(off, "")
	h, _ = db.HeroAttributes()
	if len(h) != 2 || h[0].ID != tel || h[0].Hero != 1 || h[1].Hero != 2 {
		t.Fatalf("dopo averne tolto uno: %+v", h)
	}
	if err := db.SetAttributeHero(999, HeroText); !errors.Is(err, ErrNotFound) {
		t.Fatalf("inesistente: %v", err)
	}
}
```

- [ ] **Step 2: FAIL** — `GOTEST ./internal/database/ -run TestAttributeHero` → `undefined: HeroPhone`.

- [ ] **Step 3: Implementazione** in `audience.go`:

```go
type AudienceAttribute struct {
	ID          int64
	Name, Label string
	Hero        int    // 0 = non sotto il saluto, altrimenti posizione 1..n
	HeroKind    string // HeroText | HeroPhone | HeroMail
}

const (
	HeroText  = "text"
	HeroPhone = "phone"
	HeroMail  = "mail"
)

func ValidHeroKind(k string) bool { return k == HeroText || k == HeroPhone || k == HeroMail }
```

In `ListAudienceAttributes` selezionare anche `hero, hero_kind` e fare `Scan(&a.ID, &a.Name, &a.Label, &a.Hero, &a.HeroKind)`. Aggiungere:

```go
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
			hero = 0
		case hero == 0:
			if err := tx.QueryRow(`SELECT COALESCE(MAX(hero), 0) + 1 FROM audience_attributes`).Scan(&hero); err != nil {
				return err
			}
		}
		if kind == "" {
			kind = HeroText
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
```

Import: aggiungere `"slices"` (già presenti `database/sql`, `errors`, `fmt`, `strings`).

- [ ] **Step 4: PASS** — `GOTEST ./internal/database/`.

- [ ] **Step 5: Commit** — `git add internal/database/audience.go internal/database/audience_test.go && git commit -m "Attributi AD da mostrare sotto il saluto (formato e ordine)" ` + riga Co-Authored-By.

---

### Task 3: Admin — sezione Assistenza

**Files:**
- Create: `internal/web/admin_support.go`, `web/templates/admin_assistenza.html`, `internal/web/admin_support_test.go`
- Modify: `internal/web/server.go` (route), `web/templates/admin_base.html` (voce nel rail), `internal/web/admin.go` + `web/templates/admin_overview.html` (conteggio)

**Interfaces:**
- Consumes: Task 1 (`SupportChannel`, CRUD, `ListApps`, `ListCategories`)
- Produces: route `/admin/assistenza…`; template `support_section`

- [ ] **Step 1: Test** — `internal/web/admin_support_test.go`:

```go
package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestSupportAdmin(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	apps, _ := db.ListApps()
	a, b := itoa(apps[0].ID), itoa(apps[1].ID)

	page := do(t, s, "GET", "/admin/assistenza", nil, c, nil).Body.String()
	if !strings.Contains(page, `name="app" value="`+a+`"`) || !strings.Contains(page, "Applicativi") {
		t.Fatalf("form con le caselle degli applicativi:\n%s", page)
	}
	rec := do(t, s, "POST", "/admin/assistenza", url.Values{"title": {"Portale Maggioli"}, "url": {"https://assistenza.example"},
		"note": {"serve l'utenza"}, "enabled": {"1"}, "app": {a, b}}, c, hx)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Portale Maggioli") {
		t.Fatalf("crea: %d\n%s", rec.Code, rec.Body)
	}
	all, _ := db.ListSupportChannels()
	if len(all) != 1 || len(all[0].AppIDs) != 2 || !all[0].Enabled {
		t.Fatalf("salvato: %+v", all)
	}
	id := itoa(all[0].ID)
	for _, bad := range []url.Values{
		{"title": {""}, "url": {"https://x"}},
		{"title": {"X"}, "url": {"javascript:alert(1)"}},
		{"title": {"X"}, "url": {"https://x"}, "app": {"999999"}},
	} {
		if rec := do(t, s, "POST", "/admin/assistenza/"+id, bad, c, hx); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%v: atteso 422, %d", bad, rec.Code)
		}
	}
	if body := do(t, s, "GET", "/admin/assistenza/"+id+"/modifica", nil, c, hx).Body.String(); !strings.Contains(body, `value="`+a+`" checked`) {
		t.Fatalf("modifica: caselle spuntate\n%s", body)
	}
	// Review focus 4: senza applicativi resta, con l'avviso.
	do(t, s, "POST", "/admin/assistenza/"+id, url.Values{"title": {"Portale Maggioli"}, "url": {"https://assistenza.example"}}, c, hx)
	if body := do(t, s, "GET", "/admin/assistenza", nil, c, nil).Body.String(); !strings.Contains(body, "Non collegato a nessun applicativo") {
		t.Fatal("manca l'avviso per il canale senza applicativi")
	}
	if rec := do(t, s, "POST", "/admin/assistenza/"+id+"/elimina", nil, c, hx); rec.Code != http.StatusOK {
		t.Fatalf("elimina: %d", rec.Code)
	}
	if rec := do(t, s, "GET", "/admin/assistenza/999/modifica", nil, c, hx); rec.Code != http.StatusNotFound {
		t.Fatalf("inesistente: %d", rec.Code)
	}
	if body := do(t, s, "GET", "/admin", nil, c, nil).Body.String(); !strings.Contains(body, `href="/admin/assistenza"`) {
		t.Fatal("voce Assistenza nel menu")
	}
}
```

- [ ] **Step 2: FAIL** — `GOTEST ./internal/web/ -run TestSupportAdmin` → 404 su `/admin/assistenza`.

- [ ] **Step 3: `internal/web/admin_support.go`**

```go
package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

// Sezione "Assistenza": dove chiedere aiuto per uno o più applicativi.

type supportRow struct {
	database.SupportChannel
	AppTitles []string
}

type supportAppChoice struct {
	ID      int64
	Title   string
	Note    string // "senza indirizzo", "nascosto"
	Checked bool
}

type supportCategory struct {
	Name string
	Apps []supportAppChoice
}

type supportSection struct {
	Channels   []supportRow
	Form       database.SupportChannel
	Categories []supportCategory
	Errors     formErrors
}

func (s *Server) supportData(form database.SupportChannel, errs formErrors) (supportSection, error) {
	sec := supportSection{Form: form, Errors: errs}
	chans, err := s.db.ListSupportChannels()
	if err != nil {
		return sec, err
	}
	apps, err := s.db.ListApps()
	if err != nil {
		return sec, err
	}
	cats, err := s.db.ListCategories()
	if err != nil {
		return sec, err
	}
	titles := map[int64]string{}
	for _, a := range apps {
		titles[a.ID] = a.Title
	}
	for _, c := range chans {
		r := supportRow{SupportChannel: c}
		for _, id := range c.AppIDs {
			r.AppTitles = append(r.AppTitles, titles[id])
		}
		sec.Channels = append(sec.Channels, r)
	}
	checked := map[int64]bool{}
	for _, id := range form.AppIDs {
		checked[id] = true
	}
	for _, c := range cats {
		sc := supportCategory{Name: c.Name}
		for _, a := range apps {
			if a.CategoryID != c.ID {
				continue
			}
			note := ""
			switch {
			case a.URL == "":
				note = "senza indirizzo"
			case !a.Enabled:
				note = "nascosto"
			}
			sc.Apps = append(sc.Apps, supportAppChoice{ID: a.ID, Title: a.Title, Note: note, Checked: checked[a.ID]})
		}
		if len(sc.Apps) > 0 {
			sec.Categories = append(sec.Categories, sc)
		}
	}
	return sec, nil
}

func (s *Server) renderSupport(w http.ResponseWriter, status int, form database.SupportChannel, errs formErrors) {
	sec, err := s.supportData(form, errs)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.render(w, status, "support_section", sec)
}

func newSupportForm() database.SupportChannel {
	return database.SupportChannel{Enabled: true, AppIDs: []int64{}}
}

func (s *Server) handleSupportPage(w http.ResponseWriter, r *http.Request) {
	sec, err := s.supportData(newSupportForm(), nil)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderPage(w, r, "admin_assistenza.html", "assistenza", sec)
}

func (s *Server) handleSupportEdit(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	c, err := s.db.GetSupportChannel(id)
	if errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderSupport(w, http.StatusOK, c, nil)
}

func (s *Server) handleSupportSave(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Richiesta non valida", http.StatusBadRequest)
		return
	}
	form := database.SupportChannel{ID: id, AppIDs: []int64{},
		Title:   strings.TrimSpace(r.FormValue("title")),
		URL:     strings.TrimSpace(r.FormValue("url")),
		Note:    strings.TrimSpace(r.FormValue("note")),
		Enabled: r.FormValue("enabled") == "1"}
	errs := formErrors{}
	checkText(errs, "title", form.Title, 80, true)
	checkURL(errs, "url", form.URL, true)
	checkText(errs, "note", form.Note, 200, false)
	apps, err := s.db.ListApps()
	if err != nil {
		s.serverError(w, err)
		return
	}
	known := map[int64]bool{}
	for _, a := range apps {
		known[a.ID] = true
	}
	for _, v := range r.Form["app"] {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || !known[n] {
			errs.add("app", "Applicativo non valido.")
			continue
		}
		form.AppIDs = append(form.AppIDs, n)
	}
	if len(errs) > 0 {
		s.renderSupport(w, http.StatusUnprocessableEntity, form, errs)
		return
	}
	if id == 0 {
		_, err = s.db.CreateSupportChannel(form)
	} else {
		err = s.db.UpdateSupportChannel(form)
	}
	switch {
	case errors.Is(err, database.ErrNotFound):
		http.NotFound(w, r)
	case err != nil:
		s.serverError(w, err)
	default:
		s.renderSupport(w, http.StatusOK, newSupportForm(), nil)
	}
}

func (s *Server) handleSupportDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.db.DeleteSupportChannel(id); errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderSupport(w, http.StatusOK, newSupportForm(), nil)
}

func (s *Server) handleSupportMove(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.db.MoveSupportChannel(id, moveDir(r)); errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderSupport(w, http.StatusOK, newSupportForm(), nil)
}
```

Nota: `pathID` su route senza `{id}` (`POST /admin/assistenza`) restituisce 0 come per le categorie (verificare `pathID` in `validate.go`).

- [ ] **Step 4: Route** in `server.go`, accanto a quelle delle categorie:

```go
	s.mux.HandleFunc("GET /admin/assistenza", s.requireAdmin(s.handleSupportPage))
	s.mux.HandleFunc("GET /admin/assistenza/{id}/modifica", s.requireAdmin(s.handleSupportEdit))
	s.mux.HandleFunc("POST /admin/assistenza", s.requireAdmin(s.handleSupportSave))
	s.mux.HandleFunc("POST /admin/assistenza/{id}", s.requireAdmin(s.handleSupportSave))
	s.mux.HandleFunc("POST /admin/assistenza/{id}/elimina", s.requireAdmin(s.handleSupportDelete))
	s.mux.HandleFunc("POST /admin/assistenza/{id}/sposta", s.requireAdmin(s.handleSupportMove))
```

- [ ] **Step 5: Template** `web/templates/admin_assistenza.html`:

```html
{{define "admin_assistenza.html"}}{{template "admin_top" .}}
<h1>Assistenza</h1>
{{template "support_section" .Body}}
{{template "admin_bottom" .}}{{end}}

{{define "support_section"}}
<div id="section">
	{{with .Errors.general}}<p class="flash error" role="alert">{{.}}</p>{{end}}
	<p class="hint">Dove chiedere aiuto quando un applicativo non funziona (es. il portale di assistenza del fornitore). Un canale può valere per più applicativi e compare sulla loro scheda in plancia.</p>

	<form class="card form" hx-post="{{if .Form.ID}}/admin/assistenza/{{.Form.ID}}{{else}}/admin/assistenza{{end}}" hx-target="#section" hx-swap="outerHTML">
		<h2>{{if .Form.ID}}Modifica canale{{else}}Nuovo canale{{end}}</h2>
		<label>Nome<input name="title" value="{{.Form.Title}}" maxlength="80" required placeholder="Portale assistenza Maggioli"></label>
		{{with .Errors.title}}<p class="field-error">{{.}}</p>{{end}}
		<label>Link<input name="url" type="url" value="{{.Form.URL}}" maxlength="2048" required placeholder="https://…"></label>
		{{with .Errors.url}}<p class="field-error">{{.}}</p>{{end}}
		<label>Nota <small class="muted">(facoltativa)</small><input name="note" value="{{.Form.Note}}" maxlength="200" placeholder="Serve l'utenza del portale: chiedila al CED"></label>
		{{with .Errors.note}}<p class="field-error">{{.}}</p>{{end}}
		<fieldset class="support-apps">
			<legend>Applicativi</legend>
			{{range .Categories}}<div class="support-cat"><strong>{{.Name}}</strong>
				{{range .Apps}}<label class="inline"><input type="checkbox" name="app" value="{{.ID}}"{{if .Checked}} checked{{end}}>{{.Title}}{{with .Note}} <small class="muted">({{.}})</small>{{end}}</label>{{end}}
			</div>{{else}}<p class="muted">Nessun applicativo.</p>{{end}}
		</fieldset>
		{{with .Errors.app}}<p class="field-error">{{.}}</p>{{end}}
		<label class="inline"><input type="checkbox" name="enabled" value="1"{{if .Form.Enabled}} checked{{end}}>Attivo</label>
		<div class="actions">
			<button class="primary" type="submit">Salva</button>
			{{if .Form.ID}}<a class="button" href="/admin/assistenza">Annulla</a>{{end}}
		</div>
	</form>

	<table class="list">
		<thead><tr><th>Canale</th><th>Applicativi</th><th></th></tr></thead>
		<tbody>
		{{range .Channels}}<tr>
			<td>{{.Title}}{{if not .Enabled}} <span class="pill">disattivato</span>{{end}}<br><small class="muted">{{.URL}}</small></td>
			<td>{{range .AppTitles}}<span class="chip">{{.}}</span> {{else}}<span class="muted">Non collegato a nessun applicativo</span>{{end}}</td>
			<td class="actions">
				<button class="icon" type="button" title="Su" hx-post="/admin/assistenza/{{.ID}}/sposta" hx-vals='{"dir":"up"}' hx-target="#section" hx-swap="outerHTML"><span class="material-icons" aria-hidden="true">arrow_upward</span></button>
				<button class="icon" type="button" title="Giù" hx-post="/admin/assistenza/{{.ID}}/sposta" hx-vals='{"dir":"down"}' hx-target="#section" hx-swap="outerHTML"><span class="material-icons" aria-hidden="true">arrow_downward</span></button>
				<button type="button" hx-get="/admin/assistenza/{{.ID}}/modifica" hx-target="#section" hx-swap="outerHTML">Modifica</button>
				<button class="danger" type="button" hx-post="/admin/assistenza/{{.ID}}/elimina" hx-confirm="Eliminare il canale «{{.Title}}»?" hx-target="#section" hx-swap="outerHTML">Elimina</button>
			</td></tr>
		{{else}}<tr><td colspan="3" class="muted">Nessun canale di assistenza.</td></tr>{{end}}
		</tbody>
	</table>
</div>
{{end}}
```

CSS in `web/static/css/admin.css` (in coda):

```css
.support-apps { border: 1px solid var(--line); border-radius: 8px; padding: .6rem .8rem; display: grid; gap: .5rem; }
.support-cat { display: flex; flex-wrap: wrap; gap: .2rem 1rem; align-items: center; }
.support-cat strong { flex-basis: 100%; font-size: .85rem; color: var(--muted); }
```

(`.chip` esiste già in `admin.css` dalla 0.8.0; la checkbox come `name="app" value="ID"` deve combaciare col test: in HTML l'ordine attributi è `type name value`, quindi il test cerca `name="app" value="`.)

- [ ] **Step 6: Menu e panoramica** — in `admin_base.html`, dopo la voce Guide (o Categorie):

```html
		<a href="/admin/assistenza"{{if eq .Section "assistenza"}} aria-current="page"{{end}}><span class="material-icons" aria-hidden="true">support_agent</span>Assistenza</a>
```

In `admin.go` aggiungere `Support int` a `overviewView`, caricarlo con `chans, err := s.db.ListSupportChannels()` (gestire `err` come gli altri) e `Support: len(chans)`; in `admin_overview.html` dopo le guide: `<a class="stat" href="/admin/assistenza"><b>{{.Support}}</b>canali di assistenza</a>`.

- [ ] **Step 7: PASS** — `GOTEST ./internal/web/ -run 'TestSupportAdmin|TestOverview'`, poi tutto `GOTEST ./internal/web/`.

- [ ] **Step 8: Commit** — file sopra; messaggio "Admin: sezione Assistenza con canali collegati a più applicativi".

---

### Task 4: Admin — assistenza nella scheda dell'app e attributi della testata

**Files:**
- Modify: `internal/web/admin_apps.go`, `web/templates/admin_app.html`
- Modify: `internal/web/admin_audience.go`, `internal/web/server.go`, `web/templates/admin_gruppi.html`
- Test: `internal/web/admin_support_test.go` (append), `internal/web/admin_audience_requirements_test.go` (append)

**Interfaces:**
- Consumes: Task 1 `SupportByApp`; Task 2 `SetAttributeHero`, `MoveAttributeHero`, `AudienceAttribute.Hero/HeroKind`
- Produces: route `POST /admin/gruppi/attributi/{id}/testata` (campo `hero_kind`: `""|text|phone|mail`), `POST /admin/gruppi/attributi/{id}/sposta` (`dir`)

- [ ] **Step 1: Test** — in `admin_support_test.go`:

```go
func TestAppFormShowsSupport(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	apps, _ := db.ListApps()
	db.CreateSupportChannel(database.SupportChannel{Title: "Portale Maggioli", URL: "https://x", Enabled: true, AppIDs: []int64{apps[0].ID}})
	body := do(t, s, "GET", "/admin/app/"+itoa(apps[0].ID)+"/modifica", nil, c, hx).Body.String()
	if !strings.Contains(body, "Assistenza:") || !strings.Contains(body, "Portale Maggioli") || !strings.Contains(body, `href="/admin/assistenza"`) {
		t.Fatalf("scheda app senza assistenza:\n%s", body)
	}
}
```

(aggiungere l'import di `internal/database`). In `admin_audience_requirements_test.go`:

```go
func TestAttributeHeroAdmin(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	tel, _ := db.CreateAudienceAttribute("telephoneNumber", "Interno")
	off, _ := db.CreateAudienceAttribute("physicalDeliveryOfficeName", "Ufficio")
	if rec := do(t, s, "POST", "/admin/gruppi/attributi/"+itoa(off)+"/testata", url.Values{"hero_kind": {"text"}}, c, hx); rec.Code != 200 {
		t.Fatalf("testata: %d", rec.Code)
	}
	do(t, s, "POST", "/admin/gruppi/attributi/"+itoa(tel)+"/testata", url.Values{"hero_kind": {"phone"}}, c, hx)
	if rec := do(t, s, "POST", "/admin/gruppi/attributi/"+itoa(tel)+"/testata", url.Values{"hero_kind": {"boh"}}, c, hx); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("formato non valido: %d", rec.Code)
	}
	do(t, s, "POST", "/admin/gruppi/attributi/"+itoa(tel)+"/sposta", url.Values{"dir": {"up"}}, c, hx)
	h, _ := db.HeroAttributes()
	if len(h) != 2 || h[0].ID != tel || h[0].HeroKind != "phone" {
		t.Fatalf("dopo lo spostamento: %+v", h)
	}
	page := do(t, s, "GET", "/admin/gruppi", nil, c, nil).Body.String()
	if !strings.Contains(page, `<option value="phone" selected>`) || !strings.Contains(page, "Sotto il saluto") {
		t.Fatalf("pagina gruppi:\n%s", page)
	}
}
```

- [ ] **Step 2: FAIL** — `GOTEST ./internal/web/ -run 'TestAppFormShowsSupport|TestAttributeHeroAdmin'`.

- [ ] **Step 3: Scheda app** — in `admin_apps.go`, `appsSection` riceve `Support []database.SupportChannel`; in `appsData`, se `form.ID != 0`:

```go
	if form.ID != 0 {
		by, err := s.db.SupportByApp()
		if err != nil {
			return appsSection{}, err
		}
		sec.Support = by[form.ID]
	}
```

(posizionare dopo la costruzione di `sec`). In `admin_app.html`, prima di `<div class="actions">` del form:

```html
		{{if .Form.ID}}<p class="hint">Assistenza: {{range $i, $c := .Support}}{{if $i}}, {{end}}{{$c.Title}}{{else}}nessun canale{{end}} · <a href="/admin/assistenza">gestisci</a></p>{{end}}
```

Nota: `SupportByApp` restituisce solo canali attivi; per la scheda va bene (i disattivati non compaiono in plancia).

- [ ] **Step 4: Handler testata** — in `admin_audience.go`:

```go
func (s *Server) handleAttributeHero(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	kind := r.FormValue("hero_kind")
	errs := formErrors{}
	if kind != "" && !database.ValidHeroKind(kind) {
		errs.add("attr", "Formato non valido.")
	} else if err := s.db.SetAttributeHero(id, kind); errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderAudience(w, statusFor(errs), 0, errs, nil)
}

func (s *Server) handleAttributeHeroMove(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.db.MoveAttributeHero(id, moveDir(r)); errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderAudience(w, http.StatusOK, 0, nil, nil)
}
```

Route in `server.go` accanto a `attributi/{id}/elimina`:

```go
	s.mux.HandleFunc("POST /admin/gruppi/attributi/{id}/testata", s.requireAdmin(s.handleAttributeHero))
	s.mux.HandleFunc("POST /admin/gruppi/attributi/{id}/sposta", s.requireAdmin(s.handleAttributeHeroMove))
```

- [ ] **Step 5: Template** — in `admin_gruppi.html`, la riga dell'attributo diventa:

```html
			{{range .Attributes}}<tr><td>{{.Label}}</td><td class="muted"><code>{{.Name}}</code></td>
				<td><form class="inline-form hero-form" hx-post="/admin/gruppi/attributi/{{.ID}}/testata" hx-trigger="change" hx-target="#section" hx-swap="outerHTML">
					<label>Sotto il saluto<select name="hero_kind">
						<option value=""{{if eq .Hero 0}} selected{{end}}>—</option>
						<option value="text"{{if and .Hero (eq .HeroKind "text")}} selected{{end}}>Testo</option>
						<option value="phone"{{if and .Hero (eq .HeroKind "phone")}} selected{{end}}>Interno</option>
						<option value="mail"{{if and .Hero (eq .HeroKind "mail")}} selected{{end}}>Email</option>
					</select></label></form></td>
				<td class="actions">
				{{if .Hero}}<button class="icon" type="button" title="Prima nella testata" hx-post="/admin/gruppi/attributi/{{.ID}}/sposta" hx-vals='{"dir":"up"}' hx-target="#section" hx-swap="outerHTML"><span class="material-icons" aria-hidden="true">arrow_upward</span></button>
				<button class="icon" type="button" title="Dopo nella testata" hx-post="/admin/gruppi/attributi/{{.ID}}/sposta" hx-vals='{"dir":"down"}' hx-target="#section" hx-swap="outerHTML"><span class="material-icons" aria-hidden="true">arrow_downward</span></button>{{end}}
				<button class="danger" type="button" hx-post="/admin/gruppi/attributi/{{.ID}}/elimina" hx-confirm="Togliere l'attributo «{{.Label}}»?" hx-target="#section" hx-swap="outerHTML">Togli</button></td></tr>
```

(sostituisce la riga esistente con `<td class="actions">…Togli…</td>`; mantenere `{{else}}` invariato). Sotto la tabella aggiungere: `<p class="hint">«Sotto il saluto»: il valore compare in plancia sotto «Buongiorno, …» (es. Ufficio come Testo, <code>telephoneNumber</code> come Interno, <code>mail</code> come Email).</p>`. CSS admin: `.hero-form { margin: 0; } .hero-form label { flex-direction: row; align-items: center; gap: .4rem; font-weight: 400; }`.

Attenzione: in Go template `eq .Hero 0` confronta `int` con costante intera: ok.

- [ ] **Step 6: PASS** — `GOTEST ./internal/web/`.

- [ ] **Step 7: Commit** — "Admin: assistenza nella scheda dell'app, attributi della testata nella pagina Gruppi".

---

### Task 5: Plancia — assistenza sulla tile

**Files:**
- Modify: `web/templates/partials_dashboard.html` (`app_tile`), `web/static/css/plancia.css`
- Test: `internal/web/plancia_layout_test.go` (append)

**Interfaces:**
- Consumes: `AppWithGuides.Support` (Task 1); il filtro `contentFilter.dashboard` tiene/toglie l'app intera, quindi i canali seguono l'app senza codice nuovo.

- [ ] **Step 1: Test**

```go
func TestTileSupport(t *testing.T) {
	s, db := newTestServer(t, nil)
	apps, _ := db.ListApps()
	a := apps[0]
	a.URL = "https://rubrica.local"
	db.UpdateApp(a)
	db.CreateSupportChannel(database.SupportChannel{Title: "Portale Maggioli", URL: "https://assistenza.example", Note: "serve l'utenza", Enabled: true, AppIDs: []int64{a.ID}})
	body := do(t, s, "GET", "/", nil, nil, nil).Body.String()
	for _, want := range []string{
		`class="tile-support-flag`,
		`Problemi con ` + a.Title + `?`,
		`<a href="https://assistenza.example" target="_blank" rel="noopener">Portale Maggioli ↗</a>`,
		`serve l&#39;utenza`,
		`aria-controls="guide-app-` + itoa(a.ID) + `"`, // si apre anche senza guide
	} {
		if !strings.Contains(body, want) {
			t.Errorf("manca %q", want)
		}
	}
}

// Review focus 1: canale di un'app nascosta al visitatore → non compare.
func TestTileSupportFollowsAppVisibility(t *testing.T) {
	s, db := newTestServer(t, nil)
	seedVisibility(t, db) // in audience_filter_test.go: app riservate al gruppo Tributi
	apps, _ := db.ListApps()
	var hidden database.App
	for _, a := range apps {
		if ca, _ := db.GetContentAudience(database.ContentApp, a.ID); len(ca.Groups) > 0 {
			hidden = a
		}
	}
	if hidden.ID == 0 {
		t.Skip("seedVisibility senza app riservate")
	}
	db.CreateSupportChannel(database.SupportChannel{Title: "Canale riservato", URL: "https://r.example", Enabled: true, AppIDs: []int64{hidden.ID}})
	body := do(t, s, "GET", "/", nil, viewerCookie(t, s, identity.User{Anonymous: true}), nil).Body.String()
	if strings.Contains(body, "Canale riservato") {
		t.Fatal("canale di un'app nascosta visibile")
	}
}
```

Prima di scrivere il secondo test leggere `seedVisibility` in `internal/web/audience_filter_test.go`: se nessuna app è riservata, aggiungere nel test stesso `db.SetContentAudience(database.ContentApp, id, database.ContentAudience{Mode: "only", Groups: []int64{gruppo}})` su un'app con URL, creando il gruppo con `db.CreateAudienceGroup("Riservato")`, e togliere lo `Skip`.

- [ ] **Step 2: FAIL** — `GOTEST ./internal/web/ -run TestTileSupport`.

- [ ] **Step 3: Template** — in `app_tile` (partials_dashboard.html) sostituire il blocco badge + `.tile-more`:

```html
	{{if or .Guides .Support}}<button type="button" class="tile-badge" data-tile-toggle aria-expanded="false" aria-controls="guide-app-{{.ID}}">{{if .Support}}<span class="tile-support-flag material-icons" title="Assistenza disponibile" aria-label="Assistenza disponibile">support_agent</span>{{end}}{{with .Guides}}{{len .}} {{if eq (len .) 1}}guida{{else}}guide{{end}}{{end}}<span class="material-icons" aria-hidden="true">expand_more</span></button>
	<div class="tile-more" id="guide-app-{{.ID}}">
		{{with .Guides}}<ul>{{range .}}<li>{{template "guide_link" .}}</li>{{end}}</ul>{{end}}
		{{$app := .Title}}{{range .Support}}<div class="tile-support">
			<span class="material-icons" aria-hidden="true">support_agent</span>
			<div><div class="tile-support-q">Problemi con {{$app}}?</div>
			<a href="{{.URL}}" target="_blank" rel="noopener">{{.Title}} ↗</a>
			{{with .Note}}<small>{{.}}</small>{{end}}</div>
		</div>{{end}}
	</div>{{end}}
```

Il test cerca `class="tile-support-flag` (senza virgolette di chiusura): la classe va messa per prima.

`data-search` sulla tile: aggiungere i nomi dei canali (`{{range .Support}} {{.Title}}{{end}}`) — sarà comunque sostituito dall'indice nel Task 7.

- [ ] **Step 4: CSS** (plancia.css, dopo `.tile-more li a:hover`):

```css
.tile-support-flag { font-size: 16px !important; margin-right: .2rem; color: #b54708; }
.tile-support { display: flex; gap: .55rem; align-items: flex-start; margin-top: .6rem; padding: .55rem .7rem; border-radius: 10px; background: rgba(255, 255, 255, .75); border-left: 4px solid #f79009; }
.tile-support > .material-icons { color: #b54708; font-size: 20px; }
.tile-support-q { font-size: .8rem; color: var(--p-text-2); }
.tile-support a { font-weight: 700; color: var(--p-blue-2); text-decoration: none; }
.tile-support a:hover { text-decoration: underline; }
.tile-support small { display: block; color: var(--p-text-3); font-size: .76rem; margin-top: .1rem; }
.tile-more > ul + .tile-support, .tile-more > .tile-support:first-child { margin-top: .55rem; }
```

- [ ] **Step 5: PASS** — `GOTEST ./internal/web/`.

- [ ] **Step 6: Commit** — "Plancia: assistenza sulla tile (icona e riquadro)".

---

### Task 6: Plancia — contatti sotto il saluto

**Files:**
- Modify: `internal/web/dashboard.go` (togliere `heroAttrs`/`officeLine`/`Office`, aggiungere `heroItems`), `internal/web/profiles.go` (togliere l'aggiunta di `heroAttrs`), `web/templates/dashboard.html`, `web/static/css/plancia.css`
- Test: `internal/web/plancia_layout_test.go` (sostituire `TestHeroShowsOffice`)

**Interfaces:**
- Consumes: `db.HeroAttributes()` (Task 2); `s.viewerProfile(r)` (esistente)
- Produces: `type heroItem struct{ Kind, Text string }`; `func heroItems(p audience.Profile, attrs []database.AudienceAttribute) []heroItem`; `dashboardView.Hero []heroItem`

- [ ] **Step 1: Test** — sostituire `TestHeroShowsOffice` con:

```go
func TestHeroContacts(t *testing.T) {
	s, db := newTestServer(t, nil)
	off, _ := db.CreateAudienceAttribute("physicalDeliveryOfficeName", "Ufficio")
	tit, _ := db.CreateAudienceAttribute("title", "Qualifica")
	db.CreateAudienceAttribute("department", "Settore") // non in testata
	db.SetAttributeHero(off, database.HeroText)
	db.SetAttributeHero(tit, database.HeroText)
	body := do(t, s, "GET", "/", nil, viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi"}), nil).Body.String()
	header := body[:strings.Index(body, "</header>")]
	if !strings.Contains(header, `<span class="hero-item">Tributi</span>`) || !strings.Contains(header, "Istruttore amministrativo") {
		t.Fatalf("contatti nella testata:\n%s", header)
	}
	if strings.Index(header, "Tributi") > strings.Index(header, "Istruttore") {
		t.Error("ordine della testata non rispettato")
	}
	// Anonimo: nessuna riga.
	anon := do(t, s, "GET", "/", nil, nil, nil).Body.String()
	if strings.Contains(anon[:strings.Index(anon, "</header>")], "hero-info") {
		t.Error("riga dei contatti per un anonimo")
	}
}

func TestHeroItems(t *testing.T) {
	attrs := []database.AudienceAttribute{
		{Name: "description", Hero: 1, HeroKind: database.HeroText},
		{Name: "telephoneNumber", Hero: 2, HeroKind: database.HeroPhone},
		{Name: "mail", Hero: 3, HeroKind: database.HeroMail},
		{Name: "pager", Hero: 4, HeroKind: database.HeroText},
	}
	// Review focus 3: più valori → il primo non vuoto; soli spazi → saltato.
	p := audience.Profile{Attrs: map[string][]string{
		"description":     {" ", "CED"},
		"telephonenumber": {"731"},
		"mail":            {"m.rossi@example.it"},
		"pager":           {"  "},
	}}
	got := heroItems(p, attrs)
	want := []heroItem{{"text", "Ced"}, {"phone", "Int. 731"}, {"mail", "m.rossi@example.it"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("heroItems = %+v", got)
	}
}
```

(import `reflect`; il profilo di prova `mrossi` in `server_test.go` ha già `physicaldeliveryofficename: TRIBUTI` e `title: Istruttore amministrativo`.)

- [ ] **Step 2: FAIL** — `GOTEST ./internal/web/ -run 'TestHero'`.

- [ ] **Step 3: Codice** — in `dashboard.go` togliere `heroAttrs`, `officeLine` e il campo `Office`; tenere `readable`. Aggiungere:

```go
// heroItem: una voce della riga sotto il saluto.
type heroItem struct{ Kind, Text string }

// heroItems: valori degli attributi marcati per la testata, nell'ordine
// scelto; primo valore non vuoto, valori vuoti saltati.
func heroItems(p audience.Profile, attrs []database.AudienceAttribute) []heroItem {
	out := []heroItem{}
	for _, a := range attrs {
		v := ""
		for _, x := range p.Attrs[strings.ToLower(a.Name)] {
			if x = strings.TrimSpace(x); x != "" {
				v = x
				break
			}
		}
		if v == "" {
			continue
		}
		switch a.HeroKind {
		case database.HeroPhone:
			v = "Int. " + v
		case database.HeroMail:
		default:
			v = readable(v)
		}
		out = append(out, heroItem{Kind: a.HeroKind, Text: v})
	}
	return out
}
```

Nota: `readable("CED")` → "Ced". In `handleDashboard` sostituire il blocco `office` con:

```go
	var hero []heroItem
	if _, p, ok := s.viewerProfile(r); ok {
		attrs, err := s.db.HeroAttributes()
		if err != nil {
			s.serverError(w, err)
			return
		}
		hero = heroItems(p, attrs)
	}
```

e `Hero: hero,` nella view (campo `Hero []heroItem // contatti da AD sotto il saluto`). In `profiles.go` ripristinare la costruzione semplice dei nomi (gli attributi della testata sono attributi configurati, quindi già richiesti):

```go
	names := make([]string, len(attrs))
	for i, a := range attrs {
		names[i] = a.Name
	}
```

e togliere l'import `slices` se non più usato.

- [ ] **Step 4: Template** — in `dashboard.html` sostituire `{{with .Office}}<p class="hero-info">{{.}}</p>{{end}}` con:

```html
				{{with .Hero}}<p class="hero-info">{{range .}}<span class="hero-item">{{if eq .Kind "phone"}}<span class="material-icons" aria-hidden="true">call</span>{{else if eq .Kind "mail"}}<span class="material-icons" aria-hidden="true">mail</span>{{end}}{{.Text}}</span>{{end}}</p>{{end}}
```

Attenzione al test: per il formato testo l'HTML è `<span class="hero-item">Tributi</span>` (nessuna icona): combacia.

CSS (plancia.css, sostituisce la regola `.hero-info`):

```css
.hero-info { display: flex; flex-wrap: wrap; gap: .2rem 1.1rem; margin: .3rem 0 0; opacity: .88; font-size: .92rem; }
.hero-item { display: inline-flex; align-items: center; gap: .3rem; }
.hero-item .material-icons { font-size: 16px; opacity: .8; }
```

- [ ] **Step 5: PASS** — `GOTEST ./internal/web/` (rimuovere eventuali riferimenti residui a `officeLine` nei test).

- [ ] **Step 6: Commit** — "Testata: contatti scelti dall'admin al posto dell'ufficio fisso".

---

### Task 7: Ricerca — indice JSON lato server

**Files:**
- Create: `internal/web/search_index.go`, `internal/web/search_index_test.go`
- Modify: `internal/web/dashboard.go` (`SearchIndex` nella view), `web/templates/dashboard.html`

**Interfaces:**
- Consumes: `database.Dashboard` già filtrato (`f.dashboard(&d)`), `guideHref`, `guideNewTab`
- Produces:

```go
type searchItem struct {
	Kind   string `json:"k"`           // app | guide | support
	Title  string `json:"t"`
	Sub    string `json:"s,omitempty"` // categoria, app della guida, app coperte
	Text   string `json:"x,omitempty"` // altro testo cercabile (descrizione, nota)
	URL    string `json:"u"`
	NewTab bool   `json:"n,omitempty"`
	Icon   string `json:"i,omitempty"` // nome Material Icon (app con icona del catalogo)
	Color  string `json:"c,omitempty"`
}
func buildSearchIndex(d database.Dashboard) []searchItem
```

- [ ] **Step 1: Test** — `internal/web/search_index_test.go`:

```go
package web

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
)

func searchIndexOf(t *testing.T, body string) []searchItem {
	t.Helper()
	const open = `<script type="application/json" id="search-index">`
	i := strings.Index(body, open)
	if i < 0 {
		t.Fatal("indice di ricerca assente")
	}
	raw := body[i+len(open):]
	raw = raw[:strings.Index(raw, "</script>")]
	var items []searchItem
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		t.Fatalf("JSON non valido: %v\n%s", err, raw)
	}
	return items
}

func TestSearchIndex(t *testing.T) {
	s, db := newTestServer(t, nil)
	apps, _ := db.ListApps()
	a := apps[0]
	a.URL = "https://rubrica.local"
	a.Title = `Rubrica </script><b>"x"`
	db.UpdateApp(a)
	db.CreateGuide(database.Guide{AppID: &a.ID, Title: "Cercare un interno", Kind: "link", URL: "https://wiki/1", Enabled: true})
	db.CreateGuide(database.Guide{Title: "VPN da casa", Kind: "markdown", Body: "x", Enabled: true})
	db.CreateSupportChannel(database.SupportChannel{Title: "Portale Maggioli", URL: "https://assistenza.example", Note: "utenza", Enabled: true, AppIDs: []int64{a.ID}})

	body := do(t, s, "GET", "/", nil, nil, nil).Body.String()
	// Review focus 2: un titolo con </script> non chiude il blocco.
	if strings.Contains(body, `Rubrica </script>`) {
		t.Fatal("</script> non escapato nell'indice")
	}
	items := searchIndexOf(t, body)
	kinds := map[string]searchItem{}
	for _, it := range items {
		kinds[it.Kind+":"+it.Title] = it
	}
	app, ok := kinds["app:"+a.Title]
	if !ok || app.URL != "https://rubrica.local" || !app.NewTab || app.Sub == "" {
		t.Fatalf("app nell'indice: %+v", items)
	}
	if g := kinds["guide:Cercare un interno"]; g.Sub != a.Title || g.URL != "https://wiki/1" {
		t.Errorf("guida dell'app: %+v", g)
	}
	if g := kinds["guide:VPN da casa"]; g.Sub != "Guida generale" || g.NewTab || !strings.HasPrefix(g.URL, "/guide/") {
		t.Errorf("guida generale: %+v", g)
	}
	sup := kinds["support:Portale Maggioli"]
	if sup.URL != "https://assistenza.example" || !strings.Contains(sup.Sub, a.Title) || sup.Text != "utenza" {
		t.Errorf("canale: %+v", sup)
	}
}

// Review focus 1: contenuti nascosti dai gruppi fuori dall'indice.
func TestSearchIndexHidesFiltered(t *testing.T) {
	s, db := newTestServer(t, nil)
	seedVisibility(t, db)
	anon := searchIndexOf(t, do(t, s, "GET", "/", nil, viewerCookie(t, s, identity.User{Anonymous: true}), nil).Body.String())
	for _, it := range anon {
		if it.Title == "Guida non per Tributi" || it.Title == "Urgente riservato" {
			t.Errorf("nascosto nell'indice: %+v", it)
		}
	}
}
```

Prima di scrivere `TestSearchIndexHidesFiltered` leggere `seedVisibility` e usare titoli che per l'anonimo sono davvero nascosti (contenuti riservati a un gruppo); se serve, controllare `TestDashboardAnonymousOnlyPublic` che elenca cosa l'anonimo non vede.

- [ ] **Step 2: FAIL** — `GOTEST ./internal/web/ -run TestSearchIndex`.

- [ ] **Step 3: `internal/web/search_index.go`**

```go
package web

import (
	"strings"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

// searchItem: una voce della ricerca a tendina della plancia. L'indice è
// scritto nella pagina (JSON, non eseguito) dopo il filtro per gruppi.
type searchItem struct {
	Kind   string `json:"k"`
	Title  string `json:"t"`
	Sub    string `json:"s,omitempty"`
	Text   string `json:"x,omitempty"`
	URL    string `json:"u"`
	NewTab bool   `json:"n,omitempty"`
	Icon   string `json:"i,omitempty"`
	Color  string `json:"c,omitempty"`
}

func buildSearchIndex(d database.Dashboard) []searchItem {
	out := []searchItem{}
	type chanApps struct {
		c    database.SupportChannel
		apps []string
	}
	var chans []*chanApps
	seen := map[int64]*chanApps{}
	for _, c := range d.Categories {
		for _, a := range c.Apps {
			it := searchItem{Kind: "app", Title: a.Title, Sub: c.Name, Text: a.Description, URL: a.URL, NewTab: true, Color: a.IconColor}
			if a.IconKind == database.IconPack {
				it.Icon = a.IconValue
			}
			out = append(out, it)
			for _, g := range a.Guides {
				out = append(out, searchItem{Kind: "guide", Title: g.Title, Sub: a.Title, URL: guideHref(g), NewTab: guideNewTab(g)})
			}
			for _, sc := range a.Support {
				ca, ok := seen[sc.ID]
				if !ok {
					ca = &chanApps{c: sc}
					seen[sc.ID] = ca
					chans = append(chans, ca)
				}
				ca.apps = append(ca.apps, a.Title)
			}
		}
	}
	for _, g := range d.GeneralGuides {
		out = append(out, searchItem{Kind: "guide", Title: g.Title, Sub: "Guida generale", URL: guideHref(g), NewTab: guideNewTab(g)})
	}
	for _, ca := range chans {
		out = append(out, searchItem{Kind: "support", Title: ca.c.Title, Sub: strings.Join(ca.apps, ", "), Text: ca.c.Note, URL: ca.c.URL, NewTab: true})
	}
	return out
}
```

Verificare il nome della costante dell'icona da catalogo: `grep -n "IconPack" internal/database/apps.go` (usata in `admin_apps.go` come `database.IconPack`).

- [ ] **Step 4: Pagina** — `dashboardView` riceve `SearchIndex []searchItem`; in `handleDashboard`, dopo `f.dashboard(&d)`: `SearchIndex: buildSearchIndex(d),`. In `dashboard.html`, subito dopo la `<label class="search">…</label>`:

```html
	<script type="application/json" id="search-index">{{.SearchIndex}}</script>
```

`html/template` riconosce `application/json` come contesto JS e serializza la slice in JSON con escape di `<`, `>` e `&` (`<`): il test del Review focus 2 lo verifica.

- [ ] **Step 5: PASS** — `GOTEST ./internal/web/`.

- [ ] **Step 6: Commit** — "Plancia: indice di ricerca (applicativi, guide, assistenza)".

---

### Task 8: Ricerca — tendina (JS + CSS)

**Files:**
- Modify: `web/templates/dashboard.html` (combobox), `web/static/js/dashboard.js` (sostituire `filter()`), `web/static/css/plancia.css`, `web/templates/partials_dashboard.html` (togliere `data-search-item`/`data-search`), `web/templates/dashboard.html` (togliere `#no-results`)
- Test: `internal/web/plancia_layout_test.go` (append)

**Interfaces:**
- Consumes: `#search-index` (Task 7)

- [ ] **Step 1: Test**

```go
func TestSearchDropdownMarkup(t *testing.T) {
	s, _ := newTestServer(t, nil)
	body := do(t, s, "GET", "/", nil, nil, nil).Body.String()
	for _, want := range []string{`role="combobox"`, `aria-controls="search-results"`, `aria-expanded="false"`, `id="search-results" role="listbox"`} {
		if !strings.Contains(body, want) {
			t.Errorf("manca %q", want)
		}
	}
	if strings.Contains(body, "data-search-item") || strings.Contains(body, `id="no-results"`) {
		t.Error("il vecchio filtro delle tile va tolto")
	}
	js, _ := os.ReadFile("../../web/static/js/dashboard.js")
	for _, want := range []string{`getElementById("search-index")`, `"ArrowDown"`, `"ArrowUp"`, `"Enter"`, "aria-activedescendant", `normalize("NFD")`} {
		if !strings.Contains(string(js), want) {
			t.Errorf("dashboard.js: manca %q", want)
		}
	}
	if strings.Contains(string(js), "data-search-item") {
		t.Error("dashboard.js filtra ancora le tile")
	}
}
```

- [ ] **Step 2: FAIL** — `GOTEST ./internal/web/ -run TestSearchDropdownMarkup`.

- [ ] **Step 3: Markup** — in `dashboard.html` la ricerca diventa:

```html
	<div class="search-wrap">
		<label class="search">
			<span class="material-icons" aria-hidden="true">search</span>
			<input type="search" id="search" placeholder="Cerca un applicativo, una guida o l'assistenza…" autocomplete="off" aria-label="Cerca un applicativo, una guida o l'assistenza"
				role="combobox" aria-autocomplete="list" aria-controls="search-results" aria-expanded="false">
			<kbd>/</kbd>
		</label>
		<div class="search-results" id="search-results" role="listbox" aria-label="Risultati" hidden></div>
	</div>
	<script type="application/json" id="search-index">{{.SearchIndex}}</script>
```

Togliere `<p class="empty" id="no-results" hidden>Nessun risultato.</p>`. In `partials_dashboard.html` togliere `data-search-item data-search="…"` dalla tile e dalle `<li>` di `widget_guide`.

- [ ] **Step 4: JS** — in `dashboard.js` sostituire l'intero blocco `// ── Ricerca ──` (da `const input = …` fino a `if (input) input.addEventListener("input", filter);`) con:

```js
	// ── Ricerca a tendina: indice JSON scritto dal server (già filtrato) ──
	const input = document.getElementById("search");
	const list = document.getElementById("search-results");
	const norm = (s) => (s || "").normalize("NFD").replace(/[̀-ͯ]/g, "").toLowerCase();
	let index = [];
	try { index = JSON.parse(document.getElementById("search-index")?.textContent || "[]") || []; } catch (_) { index = []; }
	index.forEach((it) => { it.nt = norm(it.t); it.nx = norm(`${it.s || ""} ${it.x || ""}`); });
	const GROUPS = [["app", "Applicativi", "apps"], ["guide", "Guide", "menu_book"], ["support", "Assistenza", "support_agent"]];
	const MAX_PER_GROUP = 8;
	let options = [];
	let active = -1;

	function score(it, q) {
		if (it.nt.startsWith(q)) return 0;
		if (it.nt.split(/\s+/).some((w) => w.startsWith(q))) return 1;
		if (it.nt.includes(q)) return 2;
		if (it.nx.includes(q)) return 3;
		return -1;
	}
	function el(tag, cls, text) {
		const e = document.createElement(tag);
		if (cls) e.className = cls;
		if (text) e.textContent = text;
		return e;
	}
	function closeResults() {
		if (!list) return;
		list.hidden = true;
		list.replaceChildren();
		input.setAttribute("aria-expanded", "false");
		input.removeAttribute("aria-activedescendant");
		options = [];
		active = -1;
	}
	function setActive(i) {
		options.forEach((o, k) => o.classList.toggle("active", k === i));
		active = i;
		if (i >= 0) {
			input.setAttribute("aria-activedescendant", options[i].id);
			options[i].scrollIntoView({ block: "nearest" });
		} else {
			input.removeAttribute("aria-activedescendant");
		}
	}
	function render() {
		const q = norm(input.value.trim());
		if (!q || !list) { closeResults(); return; }
		list.replaceChildren();
		options = [];
		GROUPS.forEach(([kind, label, icon]) => {
			const hits = index.map((it) => [score(it, q), it]).filter(([sc, it]) => sc >= 0 && it.k === kind)
				.sort((a, b) => a[0] - b[0] || a[1].t.localeCompare(b[1].t, "it")).slice(0, MAX_PER_GROUP);
			if (!hits.length) return;
			list.append(el("div", "search-group", label));
			hits.forEach(([, it]) => {
				const a = el("a", "search-option");
				a.id = `search-opt-${options.length}`;
				a.href = it.u;
				a.setAttribute("role", "option");
				if (it.n) { a.target = "_blank"; a.rel = "noopener"; }
				const ic = el("span", "material-icons", it.k === "app" && it.i ? it.i : icon);
				ic.setAttribute("aria-hidden", "true");
				if (it.k === "app" && it.c) ic.style.color = it.c;
				const txt = el("span", "search-text");
				txt.append(el("span", "search-title", it.t));
				if (it.s) txt.append(el("span", "search-sub", it.k === "support" ? `Assistenza per ${it.s}` : it.s));
				a.append(ic, txt);
				a.addEventListener("mousemove", () => setActive(options.indexOf(a)));
				options.push(a);
				list.append(a);
			});
		});
		if (!options.length) list.append(el("div", "search-empty", `Nessun risultato per «${input.value.trim()}»`));
		list.hidden = false;
		input.setAttribute("aria-expanded", "true");
		setActive(options.length ? 0 : -1);
	}
	if (input && list) {
		input.addEventListener("input", render);
		input.addEventListener("focus", () => { if (input.value.trim()) render(); });
		input.addEventListener("keydown", (e) => {
			if (list.hidden) return;
			if (e.key === "ArrowDown" && options.length) { e.preventDefault(); setActive((active + 1) % options.length); }
			else if (e.key === "ArrowUp" && options.length) { e.preventDefault(); setActive((active - 1 + options.length) % options.length); }
			else if (e.key === "Enter" && active >= 0) { e.preventDefault(); options[active].click(); }
		});
		document.addEventListener("click", (e) => { if (!e.target.closest(".search-wrap")) closeResults(); });
	}
```

Nel gestore `keydown` globale (Esc): sostituire `input.value = ""; filter(); input.blur();` con `input.value = ""; closeResults(); input.blur();`. Il gestore `/` resta. Nota CSP: `ic.style.color = …` è uno stile impostato da JS (CSSOM), non un attributo inline nel markup: consentito da `style-src-attr 'unsafe-inline'` già presente.

- [ ] **Step 5: CSS** (plancia.css, dopo le regole `.search`):

```css
.search-wrap { position: relative; z-index: 25; margin: -1.6rem var(--p-gutter) 0; }
.search-wrap .search { margin: 0; }
.search-results { position: absolute; left: 0; right: 0; top: calc(100% + 6px); background: #fff; border-radius: 14px; box-shadow: 0 22px 48px rgba(16, 24, 40, .25); padding: .4rem; max-height: min(70vh, 520px); overflow-y: auto; }
.search-group { font-size: .68rem; font-weight: 700; letter-spacing: .08em; text-transform: uppercase; color: var(--p-text-3); padding: .55rem .7rem .25rem; }
.search-option { display: flex; align-items: center; gap: .75rem; padding: .5rem .7rem; border-radius: 10px; color: var(--p-text); text-decoration: none; }
.search-option.active { background: #eff4ff; }
.search-option .material-icons { font-size: 22px; color: var(--p-blue-2); width: 28px; text-align: center; }
.search-text { display: flex; flex-direction: column; min-width: 0; }
.search-title { font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.search-sub { font-size: .8rem; color: var(--p-text-3); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.search-empty { padding: .8rem .9rem; color: var(--p-text-3); }
```

La regola esistente `.search { … margin: -1.6rem var(--p-gutter) 0; … }` passa al wrapper: togliere `margin` da `.search` (ora `margin: 0` dal wrapper) e tenere `position: relative; z-index: 2`. Ricontrollare a 640 px che il wrapper usi `--p-gutter` (già ridefinito a 1rem).

- [ ] **Step 6: Verifiche** — sintassi JS (`node -e …`), `GOTEST ./internal/web/` → PASS.

- [ ] **Step 7: Prova nel browser** — istanza mock come nella 0.8.0:
  `MSYS_NO_PATHCONV=1 docker run -d --name cp-mock -p 18090:8080 -v "$(pwd -W)":/src -v "$(pwd -W)/.devcache":/cache -e GOPATH=/cache/gopath -e GOCACHE=/cache/build -e CGO_ENABLED=0 -e LDAP_HOST=mock -e SECURE_COOKIES=false -e DB_PATH=/tmp/c.db -e UPLOAD_DIR=/tmp/up -w /src golang:1.27-alpine go run ./cmd/server`
  Login admin con `fetch('/admin/login', {method:'POST', body: new URLSearchParams({username:'admin',password:'x'})})`, creare dall'admin (o con `curl --data-urlencode @file` UTF-8) due app con URL, un canale "Portale Maggioli" collegato a entrambe. In plancia: digitare "magg" → gruppo Assistenza con il canale; "perche" trova un titolo con "Perché"; ↓/↑ evidenziano, Invio apre, Esc chiude; `/` mette il focus; la griglia non cambia. Spegnere il mock (`docker rm -f cp-mock`).

- [ ] **Step 8: Commit** — "Plancia: ricerca a tendina con risultati raggruppati".

---

### Task 9: Documentazione e versione

**Files:**
- Modify: `CLAUDE.md`, `publiccode.yml`

- [ ] **Step 1: CLAUDE.md** — aggiornare:
  - elenco tabelle in `internal/database`: "`support_channels`, `app_support` (v8, molti a molti, `ON DELETE CASCADE`), `audience_attributes.hero/hero_kind` (v8)";
  - Plancia: "tile con `support_agent` e riquadri «Problemi con …?» dai canali di assistenza (`AppWithGuides.Support`, seguono la visibilità dell'app); ricerca a tendina da `#search-index` (JSON scritto dal server dopo il filtro per gruppi, `buildSearchIndex`), combobox in `dashboard.js`";
  - Testata: sostituire la frase su `heroAttrs`/`officeLine` con "contatti sotto il saluto = attributi AD marcati «Sotto il saluto» (pagina Gruppi, formato testo/interno/email, ordine), `heroItems`";
  - Admin: "sezione `/admin/assistenza`".
- [ ] **Step 2: publiccode.yml** — `softwareVersion: 0.9.0`, `releaseDate:` data del giorno.
- [ ] **Step 3: Verifica finale** — `GOTEST ./...` tutto PASS; `go vet ./...` (nel container); sintassi JS.
- [ ] **Step 4: Commit** — "Documentazione e versione 0.9.0".
