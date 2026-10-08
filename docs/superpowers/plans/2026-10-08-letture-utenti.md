# Letture, consegne e pannello Utenti — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** per ogni avviso chi l'ha letto e a chi la notifica è arrivata; pannello Utenti con ultimo accesso, ultima apertura come app, notifiche e stato attivo (0.10.0).

**Architecture:** migrazione v9 (`alert_reads`, `alert_deliveries`, `user_presence`). La plancia e `/avvisi/{id}` scrivono presenza e letture lato server; il browser completa con `sendBeacon` (`/presenza`, `/avvisi/{id}/letto`); il dispatcher registra ogni invio; il service worker conferma la ricevuta (`/push/ricevuta`). Due pagine admin leggono i dati.

**Tech Stack:** Go 1.27, `modernc.org/sqlite`, `html/template`, HTMX 2.0.4, JS vanilla.

**Spec:** `docs/superpowers/specs/2026-10-08-letture-utenti-design.md`

## Global Constraints

- Migrazioni: mai modificare una rilasciata; **v9** in coda a `migrations` (v8 è rilasciata in 0.9.x).
- Date come testo UTC `2006-01-02T15:04:05Z` (`database.timeLayout`/helper esistenti: verificare con `grep -n "func fmtTime\|timeLayout\|func parseTime" internal/database/*.go` e riusarli).
- Endpoint pubblici (`/presenza`, `/avvisi/{id}/letto`, `/push/ricevuta`): **sempre 200**, nessun 4xx (il reverse proxy li sostituirebbe); anonimi e input non validi ignorati senza errore.
- CSP invariata: nessuno script inline; `sendBeacon`/`fetch` verso lo stesso sito.
- Utente attivo = ultimo accesso negli ultimi **15 giorni** (`database.ActiveWindow = 15 * 24 * time.Hour`).
- Presenza: al massimo una scrittura ogni **5 minuti** per utente (`database.PresenceEvery`).
- Test in container (Windows non affidabile, CLAUDE.md): `bash .superpowers/sdd/gotest.sh <pkg> [-run X]` (script locale già presente; equivale a `docker run … golang:1.27-alpine go test -count=1 <pkg>`).
- Modifiche multi-riga via script Python su file nello scratchpad (`python -X utf8 <file>`).
- Branch `letture-utenti` (contiene la spec). Commit in italiano con `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. **Due schede o due PC che segnano la lettura insieme** → una sola riga, la prima (PRIMARY KEY + `INSERT OR IGNORE`). Test in Task 2.
2. **Ricevuta per un endpoint che non ha ricevuto quell'avviso, o ricevuta ripetuta** → ignorata / prima data conservata. Test in Task 2.
3. **Utente che apre la pagina di un avviso riservato ad altri** → nessuna lettura registrata. Test in Task 3.
4. **Admin che visita la plancia senza cookie utente (solo sessione admin)** → nessuna presenza registrata (la presenza segue il cookie utente, non la sessione admin). Test in Task 3.
5. **Utente con permesso concesso ma senza iscrizione** → pannello mostra «senza iscrizione», non «attive». Test in Task 6.

---

### Task 1: DB — migrazione v9 e presenza

**Files:**
- Modify: `internal/database/migrations.go`
- Create: `internal/database/presence.go`, `internal/database/presence_test.go`

**Interfaces:**
- Produces:
  - `const PresenceEvery = 5 * time.Minute`, `const ActiveWindow = 15 * 24 * time.Hour`
  - `type Presence struct { Username, Name string; LastSeen time.Time; LastApp *time.Time; Permission string; PermissionAt *time.Time }`
  - `func (db *DB) TouchPresence(username, name string, now time.Time) error` — crea o aggiorna `last_seen_at` (e `name` se non vuoto) solo se l'ultimo è più vecchio di `PresenceEvery`; username minuscolo; `""` ignorato
  - `func (db *DB) SetPresenceClient(username string, app bool, permission string, now time.Time) error` — solo su righe esistenti; `app` → `last_app_at = now`; `permission` ∈ {granted, denied, default} altrimenti ignorato
  - `func (db *DB) ListPresence() ([]Presence, error)` — ordine `last_seen_at DESC`
  - `func (db *DB) ActiveUsernames(now time.Time) ([]string, error)` — `last_seen_at >= now-ActiveWindow`

- [ ] **Step 1: Test** — `internal/database/presence_test.go`:

```go
package database

import (
	"testing"
	"time"
)

func TestPresence(t *testing.T) {
	db := newTestDB(t)
	t0 := time.Date(2026, 10, 8, 8, 0, 0, 0, time.UTC)
	if err := db.TouchPresence("Mario.Rossi", "Mario Rossi", t0); err != nil {
		t.Fatal(err)
	}
	db.TouchPresence("mario.rossi", "", t0.Add(2*time.Minute)) // entro 5 minuti: niente scrittura
	db.TouchPresence("", "x", t0)                              // anonimo: ignorato
	ps, _ := db.ListPresence()
	if len(ps) != 1 || ps[0].Username != "mario.rossi" || ps[0].Name != "Mario Rossi" || !ps[0].LastSeen.Equal(t0) {
		t.Fatalf("presenza: %+v", ps)
	}
	db.TouchPresence("mario.rossi", "", t0.Add(6*time.Minute))
	ps, _ = db.ListPresence()
	if !ps[0].LastSeen.Equal(t0.Add(6*time.Minute)) || ps[0].Name != "Mario Rossi" {
		t.Fatalf("dopo 6 minuti: %+v", ps[0])
	}
	db.SetPresenceClient("mario.rossi", true, "granted", t0.Add(7*time.Minute))
	db.SetPresenceClient("mario.rossi", false, "boh", t0.Add(8*time.Minute)) // permesso non valido: resta granted
	db.SetPresenceClient("sconosciuto", true, "granted", t0)                 // senza riga: ignorato
	ps, _ = db.ListPresence()
	if len(ps) != 1 || ps[0].LastApp == nil || !ps[0].LastApp.Equal(t0.Add(7*time.Minute)) || ps[0].Permission != "granted" {
		t.Fatalf("client: %+v", ps[0])
	}
	db.TouchPresence("anna.bianchi", "Anna", t0.Add(-20*24*time.Hour))
	act, _ := db.ActiveUsernames(t0.Add(10 * time.Minute))
	if len(act) != 1 || act[0] != "mario.rossi" {
		t.Fatalf("attivi: %v", act)
	}
}
```

- [ ] **Step 2: FAIL** — `bash .superpowers/sdd/gotest.sh ./internal/database/ -run TestPresence` → `undefined: (*DB).TouchPresence`.

- [ ] **Step 3: Migrazione v9** — in coda a `migrations` (`migrateV9ReadsAndPresence`):

```go
// migrateV9ReadsAndPresence: letture degli avvisi, consegna delle notifiche,
// presenza degli utenti (solo l'ultimo accesso, nessuna cronologia).
func migrateV9ReadsAndPresence(tx *sql.Tx) error {
	_, err := tx.Exec(`
CREATE TABLE alert_reads (
	alert_id INTEGER NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
	username TEXT    NOT NULL,
	read_at  TEXT    NOT NULL,
	how      TEXT    NOT NULL,
	PRIMARY KEY (alert_id, username)
);
CREATE TABLE alert_deliveries (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	alert_id    INTEGER NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
	username    TEXT    NOT NULL DEFAULT '',
	endpoint    TEXT    NOT NULL,
	service     TEXT    NOT NULL,
	sent_at     TEXT    NOT NULL,
	status      TEXT    NOT NULL,
	received_at TEXT,
	UNIQUE (alert_id, endpoint)
);
CREATE INDEX idx_alert_deliveries_alert ON alert_deliveries(alert_id);
CREATE TABLE user_presence (
	username      TEXT PRIMARY KEY,
	name          TEXT NOT NULL DEFAULT '',
	last_seen_at  TEXT NOT NULL,
	last_app_at   TEXT,
	permission    TEXT NOT NULL DEFAULT '',
	permission_at TEXT
);
`)
	return err
}
```

- [ ] **Step 4: `presence.go`** — prima `grep -n "Format(\|time.Parse(" internal/database/push.go internal/database/alerts.go | head` per riusare il layout esistente (es. `const timeLayout = "2006-01-02T15:04:05Z"` e helper `fmtTime`/`parseTime`); se non esistono, definirli qui:

```go
package database

import (
	"database/sql"
	"strings"
	"time"
)

const (
	PresenceEvery = 5 * time.Minute     // al massimo una scrittura per utente
	ActiveWindow  = 15 * 24 * time.Hour // utente attivo: accesso negli ultimi 15 giorni
)

// Presence: ultimo stato noto di un utente in plancia (nessuna cronologia).
type Presence struct {
	Username, Name string
	LastSeen       time.Time
	LastApp        *time.Time
	Permission     string // granted | denied | default | ""
	PermissionAt   *time.Time
}

func (db *DB) TouchPresence(username, name string, now time.Time) error {
	u := strings.ToLower(strings.TrimSpace(username))
	if u == "" {
		return nil
	}
	ts := now.UTC().Format(timeLayout)
	limit := now.Add(-PresenceEvery).UTC().Format(timeLayout)
	_, err := db.Exec(`INSERT INTO user_presence (username, name, last_seen_at) VALUES (?, ?, ?)
ON CONFLICT(username) DO UPDATE SET
	last_seen_at = excluded.last_seen_at,
	name = CASE WHEN excluded.name <> '' THEN excluded.name ELSE user_presence.name END
WHERE user_presence.last_seen_at < ?`, u, strings.TrimSpace(name), ts, limit)
	return err
}

func (db *DB) SetPresenceClient(username string, app bool, permission string, now time.Time) error {
	u := strings.ToLower(strings.TrimSpace(username))
	if u == "" {
		return nil
	}
	ts := now.UTC().Format(timeLayout)
	if app {
		if _, err := db.Exec(`UPDATE user_presence SET last_app_at = ? WHERE username = ?`, ts, u); err != nil {
			return err
		}
	}
	switch permission {
	case "granted", "denied", "default":
		_, err := db.Exec(`UPDATE user_presence SET permission = ?, permission_at = ? WHERE username = ?`, permission, ts, u)
		return err
	}
	return nil
}

func (db *DB) ListPresence() ([]Presence, error) {
	rows, err := db.Query(`SELECT username, name, last_seen_at, last_app_at, permission, permission_at
FROM user_presence ORDER BY last_seen_at DESC, username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Presence{}
	for rows.Next() {
		var p Presence
		var seen string
		var app, permAt sql.NullString
		if err := rows.Scan(&p.Username, &p.Name, &seen, &app, &p.Permission, &permAt); err != nil {
			return nil, err
		}
		p.LastSeen, _ = time.Parse(timeLayout, seen)
		p.LastApp = parseNullTime(app)
		p.PermissionAt = parseNullTime(permAt)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (db *DB) ActiveUsernames(now time.Time) ([]string, error) {
	rows, err := db.Query(`SELECT username FROM user_presence WHERE last_seen_at >= ? ORDER BY username`,
		now.Add(-ActiveWindow).UTC().Format(timeLayout))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func parseNullTime(s sql.NullString) *time.Time {
	if !s.Valid || s.String == "" {
		return nil
	}
	t, err := time.Parse(timeLayout, s.String)
	if err != nil {
		return nil
	}
	return &t
}
```

Se nel package esiste già una costante/funzione con lo stesso compito (es. `timeLayout` con altro nome, `parseNullTime`), usare quella e non duplicare.

- [ ] **Step 5: PASS** — `bash .superpowers/sdd/gotest.sh ./internal/database/`.
- [ ] **Step 6: Commit** — "Migrazione v9 e presenza degli utenti in plancia".

---

### Task 2: DB — letture e consegne

**Files:**
- Create: `internal/database/reads.go`, `internal/database/reads_test.go`

**Interfaces:**
- Consumes: tabelle v9 (Task 1).
- Produces:
  - `const ReadConfirm, ReadOpen = "conferma", "apertura"`
  - `const DeliverySent, DeliveryFailed, DeliveryGone = "inviata", "non_riuscita", "scaduta"`
  - `type AlertRead struct { Username string; ReadAt time.Time; How string }`
  - `type Delivery struct { Username, Endpoint, Service, Status string; SentAt time.Time; ReceivedAt *time.Time }`
  - `func (db *DB) MarkRead(alertID int64, username, how string, now time.Time) error` — `INSERT OR IGNORE`; `how` non valido o username vuoto → nil senza scrivere
  - `func (db *DB) ReadsFor(alertID int64) ([]AlertRead, error)` — ordine `read_at`
  - `func (db *DB) ReadCounts() (map[int64]int, error)`
  - `func (db *DB) RecordDelivery(alertID int64, username, endpoint, service, status string, now time.Time) error` — upsert su (alert, endpoint): aggiorna stato e `sent_at`, azzera `received_at`
  - `func (db *DB) MarkReceived(alertID int64, endpoint string, now time.Time) error` — solo se esiste e `received_at IS NULL`
  - `func (db *DB) DeliveriesFor(alertID int64) ([]Delivery, error)` — ordine username, service
  - `type DeliveryCount struct{ Sent, Received int }`; `func (db *DB) DeliveryCounts() (map[int64]DeliveryCount, error)` — Sent = status inviata
  - `func (db *DB) LastReceivedByUser() (map[string]time.Time, error)`

- [ ] **Step 1: Test** — `internal/database/reads_test.go`:

```go
package database

import (
	"testing"
	"time"
)

func TestReadsAndDeliveries(t *testing.T) {
	db := newTestDB(t)
	t0 := time.Date(2026, 10, 8, 8, 0, 0, 0, time.UTC)
	id, _ := db.CreateAlert(Alert{Title: "A", Level: LevelNews, StartsAt: t0})
	// Prima lettura vince (due schede insieme).
	db.MarkRead(id, "Mario.Rossi", ReadOpen, t0)
	db.MarkRead(id, "mario.rossi", ReadConfirm, t0.Add(time.Minute))
	db.MarkRead(id, "anna", "boh", t0) // modo non valido: ignorato
	db.MarkRead(id, "", ReadOpen, t0)  // anonimo: ignorato
	rs, _ := db.ReadsFor(id)
	if len(rs) != 1 || rs[0].Username != "mario.rossi" || rs[0].How != ReadOpen || !rs[0].ReadAt.Equal(t0) {
		t.Fatalf("letture: %+v", rs)
	}
	if c, _ := db.ReadCounts(); c[id] != 1 {
		t.Fatalf("conteggio letture: %v", c)
	}

	db.RecordDelivery(id, "mario.rossi", "https://fcm.googleapis.com/a", "Chrome/Edge", DeliverySent, t0)
	db.RecordDelivery(id, "anna", "https://updates.push.services.mozilla.com/b", "Firefox", DeliveryFailed, t0)
	db.MarkReceived(id, "https://fcm.googleapis.com/a", t0.Add(time.Second))
	db.MarkReceived(id, "https://fcm.googleapis.com/a", t0.Add(time.Hour)) // ripetuta: resta la prima
	db.MarkReceived(id, "https://altro.example/x", t0)                     // endpoint sconosciuto: ignorata
	ds, _ := db.DeliveriesFor(id)
	if len(ds) != 2 || ds[1].Username != "mario.rossi" || ds[1].ReceivedAt == nil || !ds[1].ReceivedAt.Equal(t0.Add(time.Second)) || ds[0].Status != DeliveryFailed {
		t.Fatalf("consegne: %+v", ds)
	}
	if c, _ := db.DeliveryCounts(); c[id] != (DeliveryCount{Sent: 1, Received: 1}) {
		t.Fatalf("conteggi consegne: %+v", c)
	}
	if last, _ := db.LastReceivedByUser(); !last["mario.rossi"].Equal(t0.Add(time.Second)) {
		t.Fatalf("ultima ricevuta: %v", last)
	}
	// Reinvio: stato aggiornato, ricevuta azzerata, nessuna riga in più.
	db.RecordDelivery(id, "mario.rossi", "https://fcm.googleapis.com/a", "Chrome/Edge", DeliverySent, t0.Add(2*time.Hour))
	if ds, _ = db.DeliveriesFor(id); len(ds) != 2 || ds[1].ReceivedAt != nil {
		t.Fatalf("reinvio: %+v", ds)
	}
	// Avviso eliminato: tutto sparisce.
	if err := db.DeleteAlert(id); err != nil {
		t.Fatal(err)
	}
	if rs, _ := db.ReadsFor(id); len(rs) != 0 {
		t.Fatalf("letture dopo eliminazione: %+v", rs)
	}
	if ds, _ := db.DeliveriesFor(id); len(ds) != 0 {
		t.Fatalf("consegne dopo eliminazione: %+v", ds)
	}
}
```

(`CreateAlert`/`DeleteAlert`/`LevelNews` esistono: verificare i nomi con `grep -n "func (db \*DB) CreateAlert\|func (db \*DB) DeleteAlert" internal/database/alerts.go`.)

- [ ] **Step 2: FAIL** — `bash .superpowers/sdd/gotest.sh ./internal/database/ -run TestReadsAndDeliveries`.

- [ ] **Step 3: `reads.go`**

```go
package database

import (
	"database/sql"
	"strings"
	"time"
)

const (
	ReadConfirm = "conferma" // «Ho letto» sul popup urgente
	ReadOpen    = "apertura" // testo completo aperto (carosello o pagina dell'avviso)

	DeliverySent   = "inviata"
	DeliveryFailed = "non_riuscita"
	DeliveryGone   = "scaduta" // iscrizione non più valida (404/410)
)

type AlertRead struct {
	Username string
	ReadAt   time.Time
	How      string
}

type Delivery struct {
	Username, Endpoint, Service, Status string
	SentAt                              time.Time
	ReceivedAt                          *time.Time
}

type DeliveryCount struct{ Sent, Received int }

func (db *DB) MarkRead(alertID int64, username, how string, now time.Time) error {
	u := strings.ToLower(strings.TrimSpace(username))
	if u == "" || (how != ReadConfirm && how != ReadOpen) {
		return nil
	}
	_, err := db.Exec(`INSERT OR IGNORE INTO alert_reads (alert_id, username, read_at, how) VALUES (?, ?, ?, ?)`,
		alertID, u, now.UTC().Format(timeLayout), how)
	return err
}

func (db *DB) ReadsFor(alertID int64) ([]AlertRead, error) {
	rows, err := db.Query(`SELECT username, read_at, how FROM alert_reads WHERE alert_id = ? ORDER BY read_at, username`, alertID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AlertRead{}
	for rows.Next() {
		var r AlertRead
		var at string
		if err := rows.Scan(&r.Username, &at, &r.How); err != nil {
			return nil, err
		}
		r.ReadAt, _ = time.Parse(timeLayout, at)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (db *DB) ReadCounts() (map[int64]int, error) {
	rows, err := db.Query(`SELECT alert_id, COUNT(*) FROM alert_reads GROUP BY alert_id`)
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

func (db *DB) RecordDelivery(alertID int64, username, endpoint, service, status string, now time.Time) error {
	_, err := db.Exec(`INSERT INTO alert_deliveries (alert_id, username, endpoint, service, sent_at, status) VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(alert_id, endpoint) DO UPDATE SET username = excluded.username, service = excluded.service,
	sent_at = excluded.sent_at, status = excluded.status, received_at = NULL`,
		alertID, strings.ToLower(username), endpoint, service, now.UTC().Format(timeLayout), status)
	return err
}

func (db *DB) MarkReceived(alertID int64, endpoint string, now time.Time) error {
	_, err := db.Exec(`UPDATE alert_deliveries SET received_at = ? WHERE alert_id = ? AND endpoint = ? AND received_at IS NULL`,
		now.UTC().Format(timeLayout), alertID, endpoint)
	return err
}

func (db *DB) DeliveriesFor(alertID int64) ([]Delivery, error) {
	rows, err := db.Query(`SELECT username, endpoint, service, status, sent_at, received_at FROM alert_deliveries
WHERE alert_id = ? ORDER BY username, service, id`, alertID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Delivery{}
	for rows.Next() {
		var d Delivery
		var sent string
		var rec sql.NullString
		if err := rows.Scan(&d.Username, &d.Endpoint, &d.Service, &d.Status, &sent, &rec); err != nil {
			return nil, err
		}
		d.SentAt, _ = time.Parse(timeLayout, sent)
		d.ReceivedAt = parseNullTime(rec)
		out = append(out, d)
	}
	return out, rows.Err()
}

func (db *DB) DeliveryCounts() (map[int64]DeliveryCount, error) {
	rows, err := db.Query(`SELECT alert_id,
	SUM(CASE WHEN status = 'inviata' THEN 1 ELSE 0 END),
	SUM(CASE WHEN received_at IS NOT NULL THEN 1 ELSE 0 END)
FROM alert_deliveries GROUP BY alert_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]DeliveryCount{}
	for rows.Next() {
		var id int64
		var c DeliveryCount
		if err := rows.Scan(&id, &c.Sent, &c.Received); err != nil {
			return nil, err
		}
		out[id] = c
	}
	return out, rows.Err()
}

func (db *DB) LastReceivedByUser() (map[string]time.Time, error) {
	rows, err := db.Query(`SELECT username, MAX(received_at) FROM alert_deliveries
WHERE received_at IS NOT NULL AND username <> '' GROUP BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var u, at string
		if err := rows.Scan(&u, &at); err != nil {
			return nil, err
		}
		if t, err := time.Parse(timeLayout, at); err == nil {
			out[u] = t
		}
	}
	return out, rows.Err()
}
```

Nel test l'ordine `ds[0]` = anna (non_riuscita), `ds[1]` = mario.rossi (ordine per username).

- [ ] **Step 4: PASS** — `bash .superpowers/sdd/gotest.sh ./internal/database/`.
- [ ] **Step 5: Commit** — "Letture degli avvisi e consegne delle notifiche nel DB".

---

### Task 3: Web — presenza e letture dalla plancia

**Files:**
- Create: `internal/web/presence.go`, `internal/web/presence_test.go`
- Modify: `internal/web/dashboard.go` (`handleDashboard`), `internal/web/avviso.go` (`handleAvviso`), `internal/web/server.go` (route)

**Interfaces:**
- Consumes: `TouchPresence`, `SetPresenceClient`, `MarkRead` (Task 1–2); `s.viewer(r)`, `s.alertVisibleTo(username, id)`.
- Produces: `POST /presenza` (form `app=1|0`, `permesso=granted|denied|default`), `POST /avvisi/{id}/letto` (form `come=conferma|apertura`), entrambi sempre **200** con corpo vuoto (`w.WriteHeader(http.StatusOK)`).

- [ ] **Step 1: Test** — `internal/web/presence_test.go`:

```go
package web

import (
	"net/url"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
)

func TestPresenceFromDashboard(t *testing.T) {
	s, db := newTestServer(t, nil)
	mario := viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi"})
	do(t, s, "GET", "/", nil, mario, nil)
	if rec := do(t, s, "POST", "/presenza", url.Values{"app": {"1"}, "permesso": {"granted"}}, mario, nil); rec.Code != 200 {
		t.Fatalf("/presenza: %d", rec.Code)
	}
	ps, _ := db.ListPresence()
	if len(ps) != 1 || ps[0].Username != "mrossi" || ps[0].Name != "Mario Rossi" || ps[0].LastApp == nil || ps[0].Permission != "granted" {
		t.Fatalf("presenza: %+v", ps)
	}
	// Anonimo e admin senza cookie utente: nessuna presenza.
	do(t, s, "GET", "/", nil, viewerCookie(t, s, identity.User{Anonymous: true}), nil)
	do(t, s, "GET", "/", nil, login(t, s), nil)
	if rec := do(t, s, "POST", "/presenza", url.Values{"app": {"1"}}, nil, nil); rec.Code != 200 {
		t.Fatalf("/presenza anonima: %d", rec.Code)
	}
	if ps, _ = db.ListPresence(); len(ps) != 1 {
		t.Fatalf("presenze in più: %+v", ps)
	}
}

func TestReadFromPlancia(t *testing.T) {
	s, db := newTestServer(t, nil)
	seedVisibility(t, db) // "Urgente riservato" solo al gruppo Nessuno; "Novità per tutti" pubblica
	alerts, _ := db.ListActiveAlerts(fixedNow)
	var public, reserved int64
	for _, a := range alerts {
		switch a.Title {
		case "Novità per tutti":
			public = a.ID
		case "Urgente riservato":
			reserved = a.ID
		}
	}
	mario := viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi"})
	if rec := do(t, s, "POST", "/avvisi/"+itoa(public)+"/letto", url.Values{"come": {"conferma"}}, mario, nil); rec.Code != 200 {
		t.Fatalf("letto: %d", rec.Code)
	}
	do(t, s, "POST", "/avvisi/"+itoa(reserved)+"/letto", url.Values{"come": {"conferma"}}, mario, nil) // non visibile a lui
	do(t, s, "GET", "/avvisi/"+itoa(reserved), nil, mario, nil)                                       // idem, dalla pagina
	do(t, s, "POST", "/avvisi/abc/letto", nil, mario, nil)                                             // id non valido: 200, nulla
	if rs, _ := db.ReadsFor(public); len(rs) != 1 || rs[0].How != database.ReadConfirm {
		t.Fatalf("letture pubblico: %+v", rs)
	}
	if rs, _ := db.ReadsFor(reserved); len(rs) != 0 {
		t.Fatalf("lettura di un avviso non visibile: %+v", rs)
	}
	// La pagina dell'avviso conta come apertura.
	anna := viewerCookie(t, s, identity.User{Username: "senzanome"})
	do(t, s, "GET", "/avvisi/"+itoa(public), nil, anna, nil)
	if rs, _ := db.ReadsFor(public); len(rs) != 2 || rs[1].Username != "senzanome" || rs[1].How != database.ReadOpen {
		t.Fatalf("apertura dalla pagina: %+v", rs)
	}
}
```

(`senzanome` è nel profilo di prova di `testDirectory`; `mrossi` non è nel gruppo «Nessuno».)

- [ ] **Step 2: FAIL** — `bash .superpowers/sdd/gotest.sh ./internal/web/ -run 'TestPresenceFromDashboard|TestReadFromPlancia'`.

- [ ] **Step 3: `internal/web/presence.go`**

```go
package web

import (
	"log/slog"
	"net/http"
	"strconv"
)

// Presenza e letture dalla plancia. Rispondono sempre 200: il reverse proxy
// sostituirebbe i 4xx. Contano solo gli utenti riconosciuti (identità
// dichiarata: dati indicativi, non una prova).

// recognized: username dell'utente riconosciuto dal cookie, "" altrimenti.
func (s *Server) recognized(r *http.Request) (username, name string) {
	u, ok := s.viewer(r)
	if !ok || u.Anonymous || u.Username == "" {
		return "", ""
	}
	return u.Username, u.Name
}

func (s *Server) handlePresence(w http.ResponseWriter, r *http.Request) {
	if user, _ := s.recognized(r); user != "" {
		if err := s.db.SetPresenceClient(user, r.FormValue("app") == "1", r.FormValue("permesso"), s.now()); err != nil {
			slog.Warn("presenza", "err", err)
		}
	}
	w.WriteHeader(http.StatusOK)
}

// markRead registra la lettura se l'avviso è visibile all'utente.
func (s *Server) markRead(username string, alertID int64, how string) {
	if visible, _ := s.alertVisibleTo(username, alertID); !visible {
		return
	}
	if err := s.db.MarkRead(alertID, username, how, s.now()); err != nil {
		slog.Warn("lettura avviso", "err", err)
	}
}

func (s *Server) handleAlertRead(w http.ResponseWriter, r *http.Request) {
	if user, _ := s.recognized(r); user != "" {
		if id, err := strconv.ParseInt(r.PathValue("id"), 10, 64); err == nil {
			s.markRead(user, id, r.FormValue("come"))
		}
	}
	w.WriteHeader(http.StatusOK)
}
```

`alertVisibleTo` restituisce `true` per un avviso pubblico anche se l'ID non esiste? Verificare: con ID inesistente `GetContentAudience` restituisce audience vuota → «pubblico» → `MarkRead` fallisce sulla FOREIGN KEY → `slog.Warn`. Per evitare il warning, in `markRead` aggiungere prima `if _, err := s.db.GetAlert(alertID); err != nil { return }` (verificare il nome con `grep -n "func (db \*DB) GetAlert" internal/database/alerts.go`).

- [ ] **Step 4: Aggancio** — in `handleDashboard`, dopo `u, known := s.viewer(r)`:

```go
	if known && !u.Anonymous && u.Username != "" {
		if err := s.db.TouchPresence(u.Username, u.Name, s.now()); err != nil {
			slog.Warn("presenza", "err", err)
		}
	}
```

In `handleAvviso`, dopo aver trovato `view.Alert` (`view.Alert = &a`):

```go
				if user, _ := s.recognized(r); user != "" {
					s.markRead(user, a.ID, database.ReadOpen)
				}
```

(la lista è già filtrata con `f.alertList`, quindi l'avviso è visibile; `markRead` ricontrolla.) Route in `server.go` accanto a `GET /avvisi/{id}`:

```go
	s.mux.HandleFunc("POST /presenza", s.handlePresence)
	s.mux.HandleFunc("POST /avvisi/{id}/letto", s.handleAlertRead)
```

- [ ] **Step 5: PASS** — `bash .superpowers/sdd/gotest.sh ./internal/web/`.
- [ ] **Step 6: Commit** — "Plancia: presenza degli utenti e letture degli avvisi".

---

### Task 4: Consegne — dispatcher e ricevuta del service worker

**Files:**
- Modify: `internal/notify/dispatcher.go`, `internal/notify/push.go` (`ServiceName`), `internal/web/push.go` (`/push/ricevuta`), `internal/web/server.go`
- Test: `internal/notify/notify_test.go` (append), `internal/web/push_test.go` (append)

**Interfaces:**
- Consumes: `RecordDelivery`, `MarkReceived` (Task 2).
- Produces: `func ServiceName(endpoint string) string` in `notify`; `Store` esteso con `RecordDelivery(alertID int64, username, endpoint, service, status string, now time.Time) error`; route `POST /push/ricevuta` (JSON `{"tag":"avviso-12","endpoint":"https://…"}`, risposta `{"ok":…}`).

- [ ] **Step 1: Test** — in `internal/notify/notify_test.go` leggere prima il fake store (`grep -n "type fakeStore\|func (f \*fakeStore)" internal/notify/notify_test.go`) e aggiungervi il metodo che registra, poi:

```go
func TestServiceName(t *testing.T) {
	for in, want := range map[string]string{
		"https://fcm.googleapis.com/fcm/send/x":           "Chrome/Edge",
		"https://wns2-par02p.notify.windows.com/w/?t=1":   "Edge (Windows)",
		"https://updates.push.services.mozilla.com/wpush": "Firefox",
		"https://web.push.apple.com/abc":                  "Safari",
		"https://altro.example/x":                         "altro",
	} {
		if got := ServiceName(in); got != want {
			t.Errorf("ServiceName(%q) = %q, atteso %q", in, got, want)
		}
	}
}
```

e un test sul dispatcher che, dopo `RunOnce` con un pusher finto che riesce per un'iscrizione e fallisce per l'altra, trova nel fake store due consegne con stato `inviata` e `non_riuscita` (modellarlo sul test esistente del dispatcher: `grep -n "func TestDispatcher" internal/notify/notify_test.go`). In `internal/web/push_test.go`:

```go
func TestPushReceipt(t *testing.T) {
	s, db := newTestServer(t, nil)
	id, _ := db.CreateAlert(database.Alert{Title: "A", Level: database.LevelNews, StartsAt: fixedNow.Add(-time.Hour), CreatedAt: fixedNow})
	ep := "https://fcm.googleapis.com/fcm/send/abc"
	db.RecordDelivery(id, "mrossi", ep, "Chrome/Edge", database.DeliverySent, fixedNow)
	body := `{"tag":"avviso-` + itoa(id) + `","endpoint":"` + ep + `"}`
	rec := doJSON(t, s, "/push/ricevuta", body)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("ricevuta: %d %s", rec.Code, rec.Body)
	}
	if ds, _ := db.DeliveriesFor(id); ds[0].ReceivedAt == nil {
		t.Fatal("ricevuta non registrata")
	}
	for _, bad := range []string{`{"tag":"prova-1","endpoint":"` + ep + `"}`, `{"tag":"avviso-x"}`, `nonjson`} {
		if rec := doJSON(t, s, "/push/ricevuta", bad); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"ok":false`) {
			t.Errorf("%s: %d %s", bad, rec.Code, rec.Body)
		}
	}
}
```

`doJSON` esiste già nei test push? `grep -n "func doJSON\|application/json" internal/web/push_test.go`; se no, usare l'helper con cui i test esistenti fanno `POST /push/iscrizioni` e riusarlo.

- [ ] **Step 2: FAIL** — `bash .superpowers/sdd/gotest.sh ./internal/notify/` e `./internal/web/ -run TestPushReceipt`.

- [ ] **Step 3: `ServiceName`** in `internal/notify/push.go`:

```go
// ServiceName: browser o servizio dell'iscrizione, dall'host dell'endpoint.
func ServiceName(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "altro"
	}
	h := strings.ToLower(u.Hostname())
	switch {
	case h == "fcm.googleapis.com":
		return "Chrome/Edge"
	case strings.HasSuffix(h, ".notify.windows.com"):
		return "Edge (Windows)"
	case h == "updates.push.services.mozilla.com":
		return "Firefox"
	case strings.HasSuffix(h, ".push.apple.com"):
		return "Safari"
	}
	return "altro"
}
```

- [ ] **Step 4: Dispatcher** — `Store` riceve `RecordDelivery(...)`; in `push()`:

```go
	status := database.DeliverySent
	switch {
	case gone:
		status = database.DeliveryGone
		if err := d.Store.DeletePushSubscription(j.sub.Endpoint); err != nil {
			slog.Warn("rimozione iscrizione push", "err", err)
		}
	case err != nil:
		status = database.DeliveryFailed
		slog.Warn("invio push", "err", err)
	default:
		d.Store.TouchPushSubscription(j.sub.Endpoint, now)
	}
	if err := d.Store.RecordDelivery(j.alert.ID, j.sub.Username, j.sub.Endpoint, ServiceName(j.sub.Endpoint), status, now); err != nil {
		slog.Warn("consegna push", "err", err)
	}
```

`*database.DB` soddisfa già l'interfaccia (Task 2).

- [ ] **Step 5: `/push/ricevuta`** in `internal/web/push.go`:

```go
// handlePushReceipt: il service worker conferma di aver mostrato la notifica
// di un avviso (tag "avviso-<id>"). Solo per consegne già registrate.
func (s *Server) handlePushReceipt(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Tag      string `json:"tag"`
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 4<<10)).Decode(&b); err != nil || !notify.AllowedEndpoint(b.Endpoint) {
		pushReply(w, false)
		return
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(b.Tag, "avviso-"), 10, 64)
	if err != nil || !strings.HasPrefix(b.Tag, "avviso-") {
		pushReply(w, false)
		return
	}
	if err := s.db.MarkReceived(id, b.Endpoint, s.now()); err != nil {
		slog.Warn("ricevuta push", "err", err)
		pushReply(w, false)
		return
	}
	pushReply(w, true)
}
```

Route: `s.mux.HandleFunc("POST /push/ricevuta", s.handlePushReceipt)` accanto alle altre `/push/*`. Import `strconv`, `strings`, `log/slog` se mancanti.

- [ ] **Step 6: PASS** — `bash .superpowers/sdd/gotest.sh ./...`.
- [ ] **Step 7: Commit** — "Notifiche: consegne registrate e ricevuta dal service worker".

---

### Task 5: JS — beacon di presenza, letture, ricevuta

**Files:**
- Modify: `web/static/js/dashboard.js`, `web/static/sw.js`
- Test: `internal/web/plancia_layout_test.go` (append)

- [ ] **Step 1: Test**

```go
func TestPresenceAndReadJS(t *testing.T) {
	js, _ := os.ReadFile("../../web/static/js/dashboard.js")
	for _, want := range []string{`sendBeacon("/presenza"`, `matchMedia("(display-mode: standalone)")`, "function sendRead(", `sendRead(next.dataset.urgent, "conferma")`, `sendRead(card.dataset.alert, "apertura")`} {
		if !strings.Contains(string(js), want) {
			t.Errorf("dashboard.js: manca %q", want)
		}
	}
	sw, _ := os.ReadFile("../../web/static/sw.js")
	for _, want := range []string{`"/push/ricevuta"`, "pushManager.getSubscription()", `startsWith("avviso-")`} {
		if !strings.Contains(string(sw), want) {
			t.Errorf("sw.js: manca %q", want)
		}
	}
}
```

- [ ] **Step 2: FAIL** — `bash .superpowers/sdd/gotest.sh ./internal/web/ -run TestPresenceAndReadJS`.

- [ ] **Step 3: `dashboard.js`**
  - In testa (dopo `"use strict";`):

```js
	// Presenza e letture: contano solo per gli utenti riconosciuti (il server
	// ignora gli altri). sendBeacon non blocca la pagina e sopravvive alla chiusura.
	const beacon = (url, data) => {
		try { navigator.sendBeacon(url, new URLSearchParams(data)); } catch (_) { /* best effort */ }
	};
	const sentReads = new Set();
	function sendRead(id, how) {
		if (!id || sentReads.has(id)) return;
		sentReads.add(id);
		beacon(`/avvisi/${id}/letto`, { come: how });
	}
	beacon("/presenza", {
		app: window.matchMedia("(display-mode: standalone)").matches ? "1" : "0",
		permesso: "Notification" in window ? Notification.permission : "",
	});
```

  - Popup urgente, nel listener `close` (dove c'è `markRead(next);`): aggiungere `sendRead(next.dataset.urgent, "conferma");`.
  - Carosello, in `openNews(more, via)` dopo `more.open = true;`:

```js
			const card = more.closest(".news");
			if (card) sendRead(card.dataset.alert, "apertura");
```

  Nota: `sendBeacon` usa `Content-Type` form-urlencoded e la stessa origine: passa `CrossOriginProtection` (`Sec-Fetch-Site: same-origin`).

- [ ] **Step 4: `sw.js`**, nel listener `push`, sostituire `e.waitUntil(self.registration.showNotification(...))` con:

```js
	const tag = d.tag || "cruscottopa";
	e.waitUntil(self.registration.showNotification(d.title || "CruscottoPA", {
		body: d.body || "",
		icon: "/static/img/icon-192.png",
		badge: "/static/img/icon-192.png",
		tag,
		renotify: true, // stesso tag di una notifica presente: torna a farsi notare
		data: { url: d.url || "/" },
	}).then(() => (tag.startsWith("avviso-")
		// Ricevuta: la notifica dell'avviso è comparsa su questo browser.
		? self.registration.pushManager.getSubscription().then((sub) => sub && fetch("/push/ricevuta", {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({ tag, endpoint: sub.endpoint }),
		})).catch(() => {})
		: undefined)));
```

- [ ] **Step 5: Verifiche** — `node -e "new Function(require('fs').readFileSync('web/static/js/dashboard.js','utf8')); new Function(require('fs').readFileSync('web/static/sw.js','utf8'))"`; `bash .superpowers/sdd/gotest.sh ./internal/web/` PASS (anche `TestServiceWorkerRenotify`).
- [ ] **Step 6: Commit** — "Plancia: beacon di presenza, letture dal browser, ricevuta delle notifiche".

---

### Task 6: Admin — letture degli avvisi e pannello Utenti

**Files:**
- Modify: `internal/web/admin_alerts.go` (conteggi nella riga), `web/templates/admin_avvisi.html`, `internal/web/server.go`, `web/templates/admin_base.html`
- Create: `internal/web/admin_readers.go`, `web/templates/admin_letture.html`, `web/templates/admin_utenti.html`, `internal/web/admin_readers_test.go`

**Interfaces:**
- Consumes: Task 1–2 (`ReadsFor`, `DeliveriesFor`, `ReadCounts`, `DeliveryCounts`, `ListPresence`, `ActiveUsernames`, `LastReceivedByUser`, `ListPushSubscriptions`), `s.alertVisibleTo`, `notify.ServiceName`.
- Produces: `GET /admin/avvisi/{id}/letture`, `GET /admin/utenti` (`?attivi=1`, `?q=`).

- [ ] **Step 1: Test** — `internal/web/admin_readers_test.go`:

```go
package web

import (
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

func TestAlertReadersPage(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	id, _ := db.CreateAlert(database.Alert{Title: "Sciopero", Level: database.LevelNews, StartsAt: fixedNow.Add(-time.Hour), CreatedAt: fixedNow})
	db.TouchPresence("mrossi", "Mario Rossi", fixedNow)
	db.TouchPresence("senzanome", "", fixedNow)
	db.MarkRead(id, "mrossi", database.ReadConfirm, fixedNow)
	db.RecordDelivery(id, "mrossi", "https://fcm.googleapis.com/a", "Chrome/Edge", database.DeliverySent, fixedNow)
	db.MarkReceived(id, "https://fcm.googleapis.com/a", fixedNow)

	list := do(t, s, "GET", "/admin/avvisi", nil, c, nil).Body.String()
	if !strings.Contains(list, "Letto da 1") || !strings.Contains(list, "ricevute 1/1") || !strings.Contains(list, `href="/admin/avvisi/`+itoa(id)+`/letture"`) {
		t.Fatalf("lista avvisi:\n%s", list)
	}
	page := do(t, s, "GET", "/admin/avvisi/"+itoa(id)+"/letture", nil, c, nil).Body.String()
	for _, want := range []string{"Sciopero", "Mario Rossi", "conferma", "Chrome/Edge", "Non ancora letto da", "senzanome", "non una prova"} {
		if !strings.Contains(page, want) {
			t.Errorf("letture: manca %q", want)
		}
	}
	if rec := do(t, s, "GET", "/admin/avvisi/999/letture", nil, c, nil); rec.Code != 404 {
		t.Fatalf("avviso inesistente: %d", rec.Code)
	}
}

func TestUsersPanel(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	db.TouchPresence("mrossi", "Mario Rossi", fixedNow)
	db.SetPresenceClient("mrossi", true, "granted", fixedNow)
	db.TouchPresence("anna", "Anna Verdi", fixedNow.Add(-20*24*time.Hour))
	db.SetPresenceClient("anna", false, "granted", fixedNow.Add(-20*24*time.Hour)) // concesso ma senza iscrizione
	db.TouchPresence("luca", "Luca Neri", fixedNow)
	db.SetPresenceClient("luca", false, "denied", fixedNow)
	db.SavePushSubscription(database.PushSubscription{Endpoint: "https://fcm.googleapis.com/m", P256dh: "k", Auth: "a", Username: "mrossi"}, fixedNow)

	page := do(t, s, "GET", "/admin/utenti", nil, c, nil).Body.String()
	for _, want := range []string{"Mario Rossi", "attivo", "Chrome/Edge", "Anna Verdi", "inattivo", "senza iscrizione", "Luca Neri", "bloccate", "informativa"} {
		if !strings.Contains(page, want) {
			t.Errorf("utenti: manca %q", want)
		}
	}
	if only := do(t, s, "GET", "/admin/utenti?attivi=1", nil, c, nil).Body.String(); strings.Contains(only, "Anna Verdi") {
		t.Error("filtro attivi")
	}
	if q := do(t, s, "GET", "/admin/utenti?q=luca", nil, c, nil).Body.String(); strings.Contains(q, "Mario Rossi") || !strings.Contains(q, "Luca Neri") {
		t.Error("ricerca")
	}
	if body := do(t, s, "GET", "/admin", nil, c, nil).Body.String(); !strings.Contains(body, `href="/admin/utenti"`) {
		t.Error("voce Utenti nel menu")
	}
}
```

Verificare il nome della funzione che salva un'iscrizione: `grep -n "func (db \*DB) .*PushSubscription" internal/database/push.go` e adeguare la chiamata (`SavePushSubscription` o equivalente, con la sua firma).

- [ ] **Step 2: FAIL** — `bash .superpowers/sdd/gotest.sh ./internal/web/ -run 'TestAlertReadersPage|TestUsersPanel'`.

- [ ] **Step 3: Conteggi nella lista avvisi** — `alertRow` riceve `Reads int` e `Deliveries database.DeliveryCount`; in `alertsData` caricarli una volta (`ReadCounts`, `DeliveryCounts`) e assegnarli per ID. In `alert_rows` (admin_avvisi.html), nella `<small>` sotto il titolo, aggiungere:

```html
 · <a href="/admin/avvisi/{{.ID}}/letture">Letto da {{.Reads}}{{if .Deliveries.Sent}} · notifiche ricevute {{.Deliveries.Received}}/{{.Deliveries.Sent}}{{end}}</a>
```

- [ ] **Step 4: `internal/web/admin_readers.go`**

```go
package web

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/notify"
)

// Letture degli avvisi e pannello Utenti: dati indicativi (identità
// dichiarata), soggetti all'informativa dell'ente.

type readerRow struct {
	Name, Username, How string
	At                  time.Time
}

type lettureView struct {
	Alert        database.Alert
	Reads        []readerRow
	Deliveries   []database.Delivery
	Unread       []string // nomi degli utenti attivi che possono vederlo e non l'hanno letto
	UnreadUnsure bool     // AD non disponibile per un avviso riservato
	Names        map[string]string
}

func (s *Server) handleAlertReaders(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a, err := s.db.GetAlert(id)
	if errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	pres, err := s.db.ListPresence()
	if err != nil {
		s.serverError(w, err)
		return
	}
	names := map[string]string{}
	for _, p := range pres {
		names[p.Username] = p.Name
	}
	display := func(u string) string {
		if n := names[u]; n != "" {
			return n
		}
		return u
	}
	reads, err := s.db.ReadsFor(id)
	if err != nil {
		s.serverError(w, err)
		return
	}
	v := lettureView{Alert: a, Names: names}
	read := map[string]bool{}
	for _, rd := range reads {
		read[rd.Username] = true
		v.Reads = append(v.Reads, readerRow{Name: display(rd.Username), Username: rd.Username, How: rd.How, At: rd.ReadAt})
	}
	if v.Deliveries, err = s.db.DeliveriesFor(id); err != nil {
		s.serverError(w, err)
		return
	}
	active, err := s.db.ActiveUsernames(s.now())
	if err != nil {
		s.serverError(w, err)
		return
	}
	for _, u := range active {
		if read[u] {
			continue
		}
		visible, unsure := s.alertVisibleTo(u, id)
		if unsure {
			v.UnreadUnsure = true
		}
		if visible {
			v.Unread = append(v.Unread, display(u))
		}
	}
	slices.Sort(v.Unread)
	s.renderPage(w, r, "admin_letture.html", "avvisi", v)
}

type userRow struct {
	database.Presence
	Active       bool
	Notify       string   // attive | bloccate | da decidere | senza iscrizione
	Services     []string // browser iscritti
	LastReceived *time.Time
}

type utentiView struct {
	Users      []userRow
	OnlyActive bool
	Query      string
}

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	pres, err := s.db.ListPresence()
	if err != nil {
		s.serverError(w, err)
		return
	}
	subs, err := s.db.ListPushSubscriptions()
	if err != nil {
		s.serverError(w, err)
		return
	}
	last, err := s.db.LastReceivedByUser()
	if err != nil {
		s.serverError(w, err)
		return
	}
	services := map[string][]string{}
	for _, sub := range subs {
		u := strings.ToLower(sub.Username)
		if name := notify.ServiceName(sub.Endpoint); !slices.Contains(services[u], name) {
			services[u] = append(services[u], name)
		}
	}
	v := utentiView{OnlyActive: r.FormValue("attivi") == "1", Query: strings.TrimSpace(r.FormValue("q"))}
	q := strings.ToLower(v.Query)
	cutoff := s.now().Add(-database.ActiveWindow)
	for _, p := range pres {
		row := userRow{Presence: p, Active: !p.LastSeen.Before(cutoff), Services: services[p.Username]}
		if v.OnlyActive && !row.Active {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(p.Username+" "+p.Name), q) {
			continue
		}
		switch {
		case len(row.Services) > 0:
			row.Notify = "attive"
		case p.Permission == "denied":
			row.Notify = "bloccate"
		case p.Permission == "granted":
			row.Notify = "senza iscrizione"
		default:
			row.Notify = "da decidere"
		}
		if t, ok := last[p.Username]; ok {
			row.LastReceived = &t
		}
		v.Users = append(v.Users, row)
	}
	s.renderPage(w, r, "admin_utenti.html", "utenti", v)
}
```

Verificare `GetAlert` e `ListPushSubscriptions` (`grep -n "func (db \*DB) GetAlert\|func (db \*DB) ListPushSubscriptions" internal/database/*.go`).

- [ ] **Step 5: Template** — `web/templates/admin_letture.html`:

```html
{{define "admin_letture.html"}}{{template "admin_top" .}}
{{with .Body}}
<p><a href="/admin/avvisi">← Avvisi</a></p>
<h1>Letture di «{{.Alert.Title}}»</h1>
<p class="hint">L'identità in plancia è dichiarata (NTLM non verificato): questi dati sono indicativi, non una prova di lettura. Dati personali dei dipendenti: uso secondo l'informativa dell'ente.</p>

<div class="card">
	<h2>Letto da ({{len .Reads}})</h2>
	<table class="list"><thead><tr><th>Utente</th><th>Quando</th><th>Come</th></tr></thead><tbody>
	{{range .Reads}}<tr><td>{{.Name}}{{if ne .Name .Username}} <small class="muted">{{.Username}}</small>{{end}}</td><td>{{fmtDate .At}}</td><td>{{.How}}</td></tr>
	{{else}}<tr><td colspan="3" class="muted">Nessuna lettura registrata.</td></tr>{{end}}
	</tbody></table>
</div>

<div class="card">
	<h2>Notifiche</h2>
	<table class="list"><thead><tr><th>Utente</th><th>Browser</th><th>Inviata</th><th>Esito</th><th>Ricevuta</th></tr></thead><tbody>
	{{$names := .Names}}{{range .Deliveries}}<tr><td>{{with index $names .Username}}{{.}}{{else}}{{if .Username}}{{.Username}}{{else}}<span class="muted">anonimo</span>{{end}}{{end}}</td><td>{{.Service}}</td><td>{{fmtDate .SentAt}}</td><td>{{.Status}}</td><td>{{with .ReceivedAt}}{{fmtDatePtr .}}{{else}}<span class="muted">non confermata</span>{{end}}</td></tr>
	{{else}}<tr><td colspan="5" class="muted">Nessuna notifica inviata per questo avviso.</td></tr>{{end}}
	</tbody></table>
</div>

<div class="card">
	<h2>Non ancora letto da ({{len .Unread}})</h2>
	<p class="hint">Utenti attivi (accesso negli ultimi 15 giorni) che possono vedere l'avviso.</p>
	{{if .UnreadUnsure}}<p class="flash error">Elenco incompleto: AD non raggiungibile per verificare chi può vedere l'avviso.</p>{{end}}
	{{range .Unread}}<span class="chip chip-plain">{{.}}</span> {{else}}<p class="muted">Nessuno.</p>{{end}}
</div>
{{end}}
{{template "admin_bottom" .}}{{end}}
```

Attenzione: `index $names .Username` restituisce `""` per chiavi assenti → ramo `else` corretto. `fmtDate` accetta `time.Time`; per i `*time.Time` (`ReceivedAt`, `LastApp`, `LastReceived`) si usa `fmtDatePtr`, già esistente (usata per `EndsAt`): `{{with}}` su un puntatore non nil passa il puntatore.

`web/templates/admin_utenti.html`:

```html
{{define "admin_utenti.html"}}{{template "admin_top" .}}
<h1>Utenti</h1>
{{with .Body}}
<form class="inline-form" method="get" action="/admin/utenti">
	<label>Cerca<input type="search" name="q" value="{{.Query}}" placeholder="Nome o username"></label>
	<label class="inline"><input type="checkbox" name="attivi" value="1"{{if .OnlyActive}} checked{{end}}>Solo attivi</label>
	<button type="submit">Filtra</button>
</form>
<table class="list">
	<thead><tr><th>Utente</th><th>Stato</th><th>Ultimo accesso</th><th>App</th><th>Notifiche</th><th>Ultima notifica ricevuta</th></tr></thead>
	<tbody>
	{{range .Users}}<tr>
		<td>{{if .Name}}{{.Name}}<br><small class="muted">{{.Username}}</small>{{else}}{{.Username}}{{end}}</td>
		<td><span class="tag{{if not .Active}} warn{{end}}">{{if .Active}}attivo{{else}}inattivo{{end}}</span></td>
		<td>{{fmtDate .LastSeen}}</td>
		<td>{{with .LastApp}}{{fmtDatePtr .}}{{else}}<span class="muted">mai</span>{{end}}</td>
		<td>{{.Notify}}{{with .Services}}<br><small class="muted">{{range $i, $s := .}}{{if $i}}, {{end}}{{$s}}{{end}}</small>{{end}}</td>
		<td>{{with .LastReceived}}{{fmtDatePtr .}}{{else}}—{{end}}</td>
	</tr>{{else}}<tr><td colspan="6" class="muted">Nessun utente.</td></tr>{{end}}
	</tbody>
</table>
<p class="hint">Attivo = accesso negli ultimi 15 giorni. «App» = ultima apertura come app installata. Dati personali dei dipendenti: uso secondo l'informativa dell'ente. Nessuna cronologia: si conserva solo l'ultimo accesso. L'identità in plancia è dichiarata.</p>
{{end}}
{{template "admin_bottom" .}}{{end}}
```

Menu (`admin_base.html`, dopo Gruppi): `<a href="/admin/utenti"{{if eq .Section "utenti"}} aria-current="page"{{end}}><span class="material-icons" aria-hidden="true">badge</span>Utenti</a>`. Route: `GET /admin/utenti` e `GET /admin/avvisi/{id}/letture` con `requireAdmin`.

- [ ] **Step 6: PASS** — `bash .superpowers/sdd/gotest.sh ./internal/web/`.
- [ ] **Step 7: Commit** — "Admin: letture degli avvisi e pannello Utenti".

---

### Task 7: Documentazione, versione, prova nel browser

**Files:** `CLAUDE.md`, `publiccode.yml`

- [ ] **Step 1: CLAUDE.md** — tabelle v9 (`alert_reads`, `alert_deliveries`, `user_presence`); route pubbliche nuove (`/presenza`, `/avvisi/{id}/letto`, `/push/ricevuta`); admin `/admin/utenti`, `/admin/avvisi/{id}/letture`; regole: utente attivo 15 giorni, presenza ogni 5 minuti, letture prima-vince, ricevuta dal service worker solo per tag `avviso-<id>`.
- [ ] **Step 2: publiccode.yml** — `softwareVersion: 0.10.0`, `releaseDate` di oggi.
- [ ] **Step 3: Prova nel browser** — istanza mock con `NTLM_DOMAIN=MOCK` e cookie utente firmato (come per la testata della 0.9.1: sonda temporanea `cmd/cookieprobe` con `identity.NewCookieCodec(secret).Encode`, cancellata dopo): aprire la plancia → `/admin/utenti` mostra l'utente attivo; espandere un avviso → `/admin/avvisi/{id}/letture` mostra «apertura». Spegnere il mock.
- [ ] **Step 4: Verifica finale** — `bash .superpowers/sdd/gotest.sh ./...`, `go vet ./...` in container, sintassi JS.
- [ ] **Step 5: Commit** — "Documentazione e versione 0.10.0".
