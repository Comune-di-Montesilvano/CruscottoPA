# Notifiche degli avvisi e PWA — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** notifiche di sistema per gli avvisi con "Invia notifica" (SSE a plancia aperta, Web Push a browser chiuso), solo a chi può vederli, una volta per avviso; plancia installabile (manifest + service worker).

**Architecture:** nuovo pacchetto `internal/notify` (hub SSE, invio Web Push con `webpush-go`, dispatcher che ogni 30 s prende gli avvisi da notificare, li marca e li distribuisce). `internal/database` aggiunge `notified_at`, iscrizioni push e chiavi VAPID (v6). `internal/web` espone `/eventi`, `/push/*`, `/sw.js`, `/manifest.webmanifest`, la casella "Invia notifica" e la prova admin; il front-end (`notifiche.js`, `sw.js`) gestisce popup di primo accesso, iscrizione e notifiche.

**Tech Stack:** Go 1.26, `github.com/SherClockHolmes/webpush-go` v1.4.0, SQLite, HTMX, Notification API, Push API, Service Worker.

**Spec:** `docs/superpowers/specs/2026-10-07-notifiche-pwa-design.md`

## Global Constraints

- Una sola notifica per avviso: `notified_at` impostato **prima** dell'invio, con `UPDATE … WHERE notified_at IS NULL` (vince un solo ciclo).
- Destinatari = chi può vedere l'avviso; anonimo o profilo non disponibile → solo avvisi pubblici.
- `VAPID_SUBJECT` vuota = Web Push spento (SSE attivo). **La libreria antepone da sola `mailto:`** a ogni subject che non inizia con `https:`: passarle il valore **senza** il prefisso `mailto:`.
- Il reverse proxy riscrive i 4xx/5xx: gli endpoint `/push/*` rispondono **200** con JSON `{"ok":true|false}`.
- SSE: `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `X-Accel-Buffering: no`, heartbeat ogni 25 s, max 2000 connessioni.
- CSP invariata: nessuno script inline; `sw.js` servito da `/sw.js` (scope `/`).
- Migrazione v6 in coda. Nuova env var in compose, `.env.example`, codice.
- `go test ./...`, `go vet ./...`, `gofmt -l internal/ cmd/` vuoto. Testi in italiano.

## Review Focus

- Shutdown del server con connessioni SSE aperte: deve chiudersi in pochi secondi, non restare appeso 10 s per ogni client → test in Task 4 (hub `Close` sblocca gli handler).
- Avviso con `notify` modificato dopo l'invio (titolo cambiato, ancora attivo): nessuna seconda notifica → test in Task 1 (`MarkNotified` su avviso già marcato = false) e Task 3.
- Iscrizione push inviata due volte dallo stesso browser (stesso endpoint, chiavi nuove): una sola riga aggiornata → test in Task 1.
- Iscrizione con endpoint `http://` o lunghissimo: rifiutata senza 4xx → test in Task 5.
- Servizio push che risponde 410: iscrizione cancellata, l'avviso resta notificato per gli altri → test in Task 2/3.

## Prima di iniziare

Branch `feat/notifiche-pwa` da `spec/notifiche-pwa`.

---

### Task 1: database (migrazione v6)

**Files:**
- Modify: `internal/database/migrations.go`, `internal/database/alerts.go`
- Create: `internal/database/push.go`, `internal/database/push_test.go`

**Interfaces:**
- Produces:
  - `Alert.NotifiedAt *time.Time` (letto in `alertCols`)
  - `func (db *DB) PendingNotifications(now time.Time) ([]Alert, error)` — `notify = 1`, attivi, `notified_at IS NULL`
  - `func (db *DB) MarkNotified(id int64, at time.Time) (bool, error)` — `true` solo se era ancora da notificare
  - `type PushSubscription struct { ID int64; Endpoint, P256dh, Auth, Username string; CreatedAt time.Time }`
  - `func (db *DB) SavePushSubscription(ps PushSubscription) error` (upsert per endpoint)
  - `func (db *DB) DeletePushSubscription(endpoint string) error`
  - `func (db *DB) ListPushSubscriptions() ([]PushSubscription, error)`
  - `func (db *DB) ListPushSubscriptionsFor(username string) ([]PushSubscription, error)`
  - `func (db *DB) TouchPushSubscription(endpoint string, at time.Time) error`
  - `func (db *DB) EnsureVAPIDKeys(gen func() (privateKey, publicKey string, err error)) (publicKey, privateKey string, err error)`

- [ ] **Step 1: test che falliscono**

`internal/database/push_test.go`:

```go
package database

import (
	"testing"
	"time"
)

func TestPendingAndMarkNotified(t *testing.T) {
	db := newTestDB(t)
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)
	on, _ := db.CreateAlert(Alert{Title: "attivo", Level: LevelUrgent, Notify: true, StartsAt: now.Add(-time.Minute)})
	db.CreateAlert(Alert{Title: "senza notifica", Level: LevelNews, StartsAt: now.Add(-time.Minute)})
	later, _ := db.CreateAlert(Alert{Title: "programmato", Level: LevelNews, Notify: true, StartsAt: future})

	p, err := db.PendingNotifications(now)
	if err != nil || len(p) != 1 || p[0].ID != on {
		t.Fatalf("pending: %+v %v", p, err)
	}
	if ok, err := db.MarkNotified(on, now); !ok || err != nil {
		t.Fatalf("prima marcatura: %v %v", ok, err)
	}
	if ok, _ := db.MarkNotified(on, now); ok {
		t.Fatal("seconda marcatura: l'avviso era già notificato")
	}
	if p, _ := db.PendingNotifications(now); len(p) != 0 {
		t.Fatalf("dopo la marcatura: %+v", p)
	}
	if p, _ := db.PendingNotifications(future.Add(time.Second)); len(p) != 1 || p[0].ID != later {
		t.Fatalf("avviso programmato all'orario di inizio: %+v", p)
	}
	a, _ := db.GetAlert(on)
	if a.NotifiedAt == nil || !a.NotifiedAt.Equal(now) {
		t.Fatalf("NotifiedAt: %v", a.NotifiedAt)
	}
}

func TestPushSubscriptions(t *testing.T) {
	db := newTestDB(t)
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	ps := PushSubscription{Endpoint: "https://push.example/a", P256dh: "k1", Auth: "a1", Username: "mrossi", CreatedAt: now}
	if err := db.SavePushSubscription(ps); err != nil {
		t.Fatal(err)
	}
	ps.P256dh, ps.Username = "k2", ""
	if err := db.SavePushSubscription(ps); err != nil {
		t.Fatal(err)
	}
	all, _ := db.ListPushSubscriptions()
	if len(all) != 1 || all[0].P256dh != "k2" || all[0].Username != "" {
		t.Fatalf("upsert per endpoint: %+v", all)
	}
	db.SavePushSubscription(PushSubscription{Endpoint: "https://push.example/b", P256dh: "k", Auth: "a", Username: "mrossi", CreatedAt: now})
	if mine, _ := db.ListPushSubscriptionsFor("MRossi"); len(mine) != 1 || mine[0].Endpoint != "https://push.example/b" {
		t.Fatalf("per utente: %+v", mine)
	}
	if err := db.TouchPushSubscription("https://push.example/b", now); err != nil {
		t.Fatal(err)
	}
	if err := db.DeletePushSubscription("https://push.example/a"); err != nil {
		t.Fatal(err)
	}
	if all, _ := db.ListPushSubscriptions(); len(all) != 1 {
		t.Fatalf("dopo la cancellazione: %+v", all)
	}
}

func TestEnsureVAPIDKeys(t *testing.T) {
	db := newTestDB(t)
	calls := 0
	gen := func() (string, string, error) { calls++; return "priv", "pub", nil }
	pub, priv, err := db.EnsureVAPIDKeys(gen)
	if err != nil || pub != "pub" || priv != "priv" {
		t.Fatalf("prima generazione: %q %q %v", pub, priv, err)
	}
	if pub, _, _ := db.EnsureVAPIDKeys(gen); pub != "pub" || calls != 1 {
		t.Fatalf("le chiavi si generano una volta sola: calls=%d", calls)
	}
}

// Gli avvisi già attivi al momento della migrazione non vanno notificati in ritardo.
func TestMigrationMarksActiveAlerts(t *testing.T) {
	db := newTestDB(t)
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('alerts') WHERE name = 'notified_at'`).Scan(&n)
	if n != 1 {
		t.Fatal("colonna notified_at mancante")
	}
}
```

- [ ] **Step 2: verifica che falliscano**

Run: `go test ./internal/database/ -run 'Pending|PushSub|VAPID|MigrationMarks'`
Expected: errori di compilazione.

- [ ] **Step 3: migrazione**

`migrations.go`, in coda all'elenco `migrateV6Notifications`:

```go
func migrateV6Notifications(tx *sql.Tx) error {
	_, err := tx.Exec(`
ALTER TABLE alerts ADD COLUMN notified_at TEXT;
-- Avvisi già attivi: niente notifiche arretrate al primo avvio.
UPDATE alerts SET notified_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
WHERE starts_at <= strftime('%Y-%m-%dT%H:%M:%SZ', 'now');

CREATE TABLE push_subscriptions (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	endpoint   TEXT    NOT NULL UNIQUE,
	p256dh     TEXT    NOT NULL,
	auth       TEXT    NOT NULL,
	username   TEXT    NOT NULL DEFAULT '',
	created_at TEXT    NOT NULL,
	last_ok_at TEXT
);
CREATE INDEX idx_push_username ON push_subscriptions(username);

CREATE TABLE vapid_keys (
	id          INTEGER PRIMARY KEY CHECK (id = 1),
	public_key  TEXT NOT NULL,
	private_key TEXT NOT NULL
);
`)
	return err
}
```

(Marca anche gli avvisi già scaduti: innocuo, non sarebbero comunque selezionati.)

- [ ] **Step 4: avvisi**

`alerts.go`:
- struct `Alert`: `NotifiedAt *time.Time // quando è partita la notifica; nil = non ancora`
- `alertCols`: aggiungere `, notified_at` in fondo; `scanAlert`: leggere in un `sql.NullString` e, se valido, `parseTime` in `NotifiedAt`.
- in coda al file:

```go
// PendingNotifications: avvisi con notifica richiesta, attivi e non ancora notificati.
func (db *DB) PendingNotifications(now time.Time) ([]Alert, error) {
	n := formatTime(now)
	return queryAlerts(db, `SELECT `+alertCols+` FROM alerts
WHERE notify = 1 AND notified_at IS NULL AND starts_at <= ? AND (ends_at IS NULL OR ends_at > ?)
ORDER BY starts_at, id`, n, n)
}

// MarkNotified marca l'avviso come notificato; false se lo era già (un solo invio).
func (db *DB) MarkNotified(id int64, at time.Time) (bool, error) {
	res, err := db.Exec(`UPDATE alerts SET notified_at = ? WHERE id = ? AND notified_at IS NULL`, formatTime(at), id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}
```

- [ ] **Step 5: iscrizioni e chiavi**

`internal/database/push.go`:

```go
package database

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// PushSubscription è l'iscrizione Web Push di un browser.
type PushSubscription struct {
	ID        int64
	Endpoint  string
	P256dh    string
	Auth      string
	Username  string // dal cookie utente; "" = anonimo
	CreatedAt time.Time
}

const pushCols = `id, endpoint, p256dh, auth, username, created_at`

func queryPush(db *DB, q string, args ...any) ([]PushSubscription, error) {
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PushSubscription{}
	for rows.Next() {
		var p PushSubscription
		var created string
		if err := rows.Scan(&p.ID, &p.Endpoint, &p.P256dh, &p.Auth, &p.Username, &created); err != nil {
			return nil, err
		}
		if p.CreatedAt, err = parseTime(created); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SavePushSubscription inserisce o aggiorna (stesso endpoint = stesso browser).
func (db *DB) SavePushSubscription(ps PushSubscription) error {
	_, err := db.Exec(`
INSERT INTO push_subscriptions (endpoint, p256dh, auth, username, created_at) VALUES (?, ?, ?, ?, ?)
ON CONFLICT(endpoint) DO UPDATE SET p256dh = excluded.p256dh, auth = excluded.auth, username = excluded.username`,
		ps.Endpoint, ps.P256dh, ps.Auth, strings.ToLower(ps.Username), formatTime(ps.CreatedAt))
	return err
}

func (db *DB) DeletePushSubscription(endpoint string) error {
	_, err := db.Exec(`DELETE FROM push_subscriptions WHERE endpoint = ?`, endpoint)
	return err
}

func (db *DB) ListPushSubscriptions() ([]PushSubscription, error) {
	return queryPush(db, `SELECT `+pushCols+` FROM push_subscriptions ORDER BY id`)
}

func (db *DB) ListPushSubscriptionsFor(username string) ([]PushSubscription, error) {
	return queryPush(db, `SELECT `+pushCols+` FROM push_subscriptions WHERE username = ? ORDER BY id`, strings.ToLower(username))
}

func (db *DB) TouchPushSubscription(endpoint string, at time.Time) error {
	_, err := db.Exec(`UPDATE push_subscriptions SET last_ok_at = ? WHERE endpoint = ?`, formatTime(at), endpoint)
	return err
}

// EnsureVAPIDKeys restituisce le chiavi VAPID, generandole al primo uso.
func (db *DB) EnsureVAPIDKeys(gen func() (privateKey, publicKey string, err error)) (string, string, error) {
	var pub, priv string
	err := db.QueryRow(`SELECT public_key, private_key FROM vapid_keys WHERE id = 1`).Scan(&pub, &priv)
	if err == nil {
		return pub, priv, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", "", err
	}
	if priv, pub, err = gen(); err != nil {
		return "", "", err
	}
	if _, err := db.Exec(`INSERT OR IGNORE INTO vapid_keys (id, public_key, private_key) VALUES (1, ?, ?)`, pub, priv); err != nil {
		return "", "", err
	}
	// Rilettura: se due processi generassero insieme, vince la prima riga salvata.
	err = db.QueryRow(`SELECT public_key, private_key FROM vapid_keys WHERE id = 1`).Scan(&pub, &priv)
	return pub, priv, err
}
```

- [ ] **Step 6: verifica**

Run: `go test ./internal/database/ ./internal/backup/ ./internal/web/ && go vet ./...`
Expected: PASS.

- [ ] **Step 7: commit**

```bash
git add internal/database
git commit -m "feat(database): migrazione 6 per notifiche, iscrizioni push e chiavi VAPID"
```

---

### Task 2: configurazione e invio Web Push

**Files:**
- Modify: `internal/config/config.go`, `internal/config/config_test.go`, `docker-compose.yml`, `.env.example`, `go.mod`, `go.sum`
- Create: `internal/notify/push.go`, `internal/notify/push_test.go`

**Interfaces:**
- Consumes: `database.PushSubscription`, `database.Alert` (Task 1).
- Produces:
  - `config.Config.VAPIDSubject string`; avviso in `Warnings()` se non inizia con `mailto:` o `https:`
  - `type notify.Pusher interface { Send(ctx context.Context, sub database.PushSubscription, payload []byte, urgent bool) (gone bool, err error) }`
  - `type notify.WebPusher struct { Subject, PublicKey, PrivateKey string; Client webpush.HTTPClient }` (implementa `Pusher`)
  - `func notify.Payload(a database.Alert) []byte` — JSON `{"title","body","url","tag"}`

- [ ] **Step 1: test che falliscono**

In `internal/config/config_test.go` aggiungere `"VAPID_SUBJECT"` alla lista delle variabili azzerate e:

```go
func TestVAPIDSubject(t *testing.T) {
	t.Setenv("LDAP_HOST", "mock")
	t.Setenv("VAPID_SUBJECT", " mailto:supporto@example.it ")
	cfg, err := Load()
	if err != nil || cfg.VAPIDSubject != "mailto:supporto@example.it" || len(cfg.Warnings()) != 0 {
		t.Fatalf("VAPIDSubject = %q, warnings %v (%v)", cfg.VAPIDSubject, cfg.Warnings(), err)
	}
	t.Setenv("VAPID_SUBJECT", "supporto@example.it")
	cfg, _ = Load()
	if w := strings.Join(cfg.Warnings(), " "); !strings.Contains(w, "VAPID_SUBJECT") {
		t.Fatalf("atteso avviso su VAPID_SUBJECT senza mailto:/https:, ottenuto %q", w)
	}
}
```

`internal/notify/push_test.go`:

```go
package notify

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/SherClockHolmes/webpush-go"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

func testSubscription(t *testing.T, endpoint string) database.PushSubscription {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	rand.Read(auth)
	return database.PushSubscription{
		Endpoint: endpoint,
		P256dh:   base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()),
		Auth:     base64.RawURLEncoding.EncodeToString(auth),
	}
}

func testPusher(t *testing.T) *WebPusher {
	t.Helper()
	priv, pub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	return &WebPusher{Subject: "mailto:supporto@example.it", PublicKey: pub, PrivateKey: priv}
}

func TestWebPusherSends(t *testing.T) {
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	p := testPusher(t)
	gone, err := p.Send(context.Background(), testSubscription(t, srv.URL+"/push/1"), []byte(`{"title":"x"}`), true)
	if err != nil || gone {
		t.Fatalf("invio: gone=%v err=%v", gone, err)
	}
	if got.Header.Get("Urgency") != "high" || got.Header.Get("TTL") != "3600" || got.Header.Get("Content-Encoding") != "aes128gcm" {
		t.Fatalf("header: %v", got.Header)
	}
	if a := got.Header.Get("Authorization"); !strings.HasPrefix(a, "vapid t=") {
		t.Fatalf("Authorization VAPID mancante: %q", a)
	}
}

func TestWebPusherGone(t *testing.T) {
	for _, code := range []int{http.StatusGone, http.StatusNotFound} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) }))
		gone, err := testPusher(t).Send(context.Background(), testSubscription(t, srv.URL), []byte(`{}`), false)
		srv.Close()
		if !gone || err != nil {
			t.Errorf("%d: atteso gone senza errore, ottenuto %v %v", code, gone, err)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) }))
	defer srv.Close()
	if gone, err := testPusher(t).Send(context.Background(), testSubscription(t, srv.URL), []byte(`{}`), false); gone || err == nil {
		t.Fatalf("500: atteso errore e iscrizione da tenere, ottenuto %v %v", gone, err)
	}
}

func TestPayload(t *testing.T) {
	a := database.Alert{ID: 7, Title: "Sciopero", Body: strings.Repeat("parola ", 60), Level: database.LevelUrgent, StartsAt: time.Now()}
	var m map[string]string
	if err := json.Unmarshal(Payload(a), &m); err != nil {
		t.Fatal(err)
	}
	if m["title"] != "Sciopero" || m["url"] != "/" || m["tag"] != "avviso-7" || len([]rune(m["body"])) > 160 {
		t.Fatalf("payload: %+v", m)
	}
}
```

- [ ] **Step 2: verifica che falliscano**

Run: `go get github.com/SherClockHolmes/webpush-go@v1.4.0 && go test ./internal/config/ ./internal/notify/`
Expected: errori di compilazione (`VAPIDSubject`, `WebPusher`, `Payload` non definiti).

- [ ] **Step 3: configurazione**

`config.go`: campo `VAPIDSubject string` (commento: "contatto VAPID (mailto: o https:); vuoto = Web Push spento"), in `Load` `VAPIDSubject: strings.TrimSpace(os.Getenv("VAPID_SUBJECT")),` e in `Warnings()`:

```go
	if s := c.VAPIDSubject; s != "" && !strings.HasPrefix(s, "mailto:") && !strings.HasPrefix(s, "https:") {
		w = append(w, "VAPID_SUBJECT deve iniziare con mailto: o https:")
	}
```

`docker-compose.yml`: `- VAPID_SUBJECT=${VAPID_SUBJECT}` sotto `NTLM_DOMAIN`. `.env.example`, in coda:

```
# ── Notifiche push (browser chiuso) ────────────────────────────────────────
# Contatto richiesto dai servizi push (mailto: o https:). Vuoto = Web Push spento;
# le notifiche a plancia aperta funzionano comunque. Il server deve uscire su Internet.
VAPID_SUBJECT=
```

- [ ] **Step 4: invio**

`internal/notify/push.go`:

```go
// Package notify invia le notifiche degli avvisi: in tempo reale alle plance
// aperte (SSE) e via Web Push ai browser iscritti.
package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/SherClockHolmes/webpush-go"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

// Pusher consegna un messaggio a un'iscrizione Web Push. gone = iscrizione
// non più valida (da cancellare).
type Pusher interface {
	Send(ctx context.Context, sub database.PushSubscription, payload []byte, urgent bool) (gone bool, err error)
}

// WebPusher usa webpush-go (cifratura RFC 8291, firma VAPID).
type WebPusher struct {
	Subject    string // VAPID_SUBJECT così come configurato (mailto:… o https:…)
	PublicKey  string
	PrivateKey string
	Client     webpush.HTTPClient // nil = http.Client predefinito
}

func (p *WebPusher) Send(ctx context.Context, sub database.PushSubscription, payload []byte, urgent bool) (bool, error) {
	urgency := webpush.UrgencyNormal
	if urgent {
		urgency = webpush.UrgencyHigh
	}
	resp, err := webpush.SendNotificationWithContext(ctx, payload,
		&webpush.Subscription{Endpoint: sub.Endpoint, Keys: webpush.Keys{P256dh: sub.P256dh, Auth: sub.Auth}},
		&webpush.Options{
			HTTPClient: p.Client,
			// La libreria antepone "mailto:" a tutto ciò che non inizia con
			// "https:": va tolto qui, altrimenti diventerebbe "mailto:mailto:…".
			Subscriber:      strings.TrimPrefix(p.Subject, "mailto:"),
			VAPIDPublicKey:  p.PublicKey,
			VAPIDPrivateKey: p.PrivateKey,
			TTL:             3600,
			Urgency:         urgency,
		})
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return true, nil
	case resp.StatusCode >= 300:
		return false, fmt.Errorf("servizio push: %s", resp.Status)
	}
	return false, nil
}

// Payload: contenuto della notifica (letto dal service worker).
func Payload(a database.Alert) []byte {
	body := strings.Join(strings.Fields(a.Body), " ")
	if utf8.RuneCountInString(body) > 160 {
		body = string([]rune(body)[:157]) + "…"
	}
	b, _ := json.Marshal(map[string]string{
		"title": a.Title,
		"body":  body,
		"url":   "/",
		"tag":   fmt.Sprintf("avviso-%d", a.ID),
	})
	return b
}
```

Poi `go mod tidy`.

- [ ] **Step 5: verifica**

Run: `go test ./internal/config/ ./internal/notify/ && go vet ./...`
Expected: PASS.

- [ ] **Step 6: commit**

```bash
git add internal/config internal/notify docker-compose.yml .env.example go.mod go.sum
git commit -m "feat(notify): invio Web Push con VAPID e configurazione VAPID_SUBJECT"
```

---

### Task 3: hub SSE e dispatcher

**Files:**
- Create: `internal/notify/hub.go`, `internal/notify/dispatcher.go`, `internal/notify/notify_test.go`

**Interfaces:**
- Consumes: `Pusher`, `Payload` (Task 2); metodi DB del Task 1.
- Produces:
  - `type Event struct { ID int64; Title, Level string }`
  - `func NewHub(max int) *Hub`; `(*Hub).Subscribe(username string) (*Client, error)` (`ErrTooMany`); `(*Hub).Unsubscribe(*Client)`; `(*Hub).Broadcast(e Event, visible func(username string) bool)`; `(*Hub).Close()`; `(*Hub).Done() <-chan struct{}`
  - `type Client struct { Username string; Events <-chan Event }`
  - `type Store interface { PendingNotifications(time.Time) ([]database.Alert, error); MarkNotified(int64, time.Time) (bool, error); ListPushSubscriptions() ([]database.PushSubscription, error); DeletePushSubscription(string) error; TouchPushSubscription(string, time.Time) error }`
  - `type Dispatcher struct { Store Store; Hub *Hub; Pusher Pusher; Visible func(username string, alertID int64) bool; Now func() time.Time }`
  - `(*Dispatcher).RunOnce(ctx context.Context) error`; `(*Dispatcher).Run(ctx context.Context, every time.Duration)`

- [ ] **Step 1: test che falliscono**

`internal/notify/notify_test.go`:

```go
package notify

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

func TestHubBroadcastFiltered(t *testing.T) {
	h := NewHub(10)
	a, _ := h.Subscribe("mrossi")
	b, _ := h.Subscribe("")
	h.Broadcast(Event{ID: 1, Title: "riservato"}, func(u string) bool { return u == "mrossi" })
	select {
	case e := <-a.Events:
		if e.ID != 1 {
			t.Fatalf("evento: %+v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("l'utente destinatario non ha ricevuto l'evento")
	}
	select {
	case e := <-b.Events:
		t.Fatalf("l'anonimo non doveva ricevere %+v", e)
	default:
	}
	h.Unsubscribe(a)
	h.Unsubscribe(b)
}

func TestHubLimitAndClose(t *testing.T) {
	h := NewHub(1)
	if _, err := h.Subscribe("a"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Subscribe("b"); !errors.Is(err, ErrTooMany) {
		t.Fatalf("oltre il limite: %v", err)
	}
	h.Close()
	select {
	case <-h.Done():
	case <-time.After(time.Second):
		t.Fatal("Close deve chiudere Done()")
	}
	if _, err := h.Subscribe("c"); err == nil {
		t.Fatal("dopo Close nessuna nuova connessione")
	}
}

type fakeStore struct {
	mu       sync.Mutex
	pending  []database.Alert
	marked   map[int64]bool
	subs     []database.PushSubscription
	deleted  []string
	touched  []string
}

func (f *fakeStore) PendingNotifications(time.Time) ([]database.Alert, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []database.Alert{}
	for _, a := range f.pending {
		if !f.marked[a.ID] {
			out = append(out, a)
		}
	}
	return out, nil
}
func (f *fakeStore) MarkNotified(id int64, _ time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.marked[id] {
		return false, nil
	}
	f.marked[id] = true
	return true, nil
}
func (f *fakeStore) ListPushSubscriptions() ([]database.PushSubscription, error) { return f.subs, nil }
func (f *fakeStore) DeletePushSubscription(e string) error                     { f.deleted = append(f.deleted, e); return nil }
func (f *fakeStore) TouchPushSubscription(e string, _ time.Time) error         { f.touched = append(f.touched, e); return nil }

type fakePusher struct {
	sent []string
	gone map[string]bool
}

func (p *fakePusher) Send(_ context.Context, s database.PushSubscription, _ []byte, _ bool) (bool, error) {
	p.sent = append(p.sent, s.Endpoint)
	return p.gone[s.Endpoint], nil
}

func TestDispatcherNotifiesOnceAndOnlyVisible(t *testing.T) {
	st := &fakeStore{
		pending: []database.Alert{{ID: 1, Title: "urgente", Level: database.LevelUrgent, Notify: true}},
		marked:  map[int64]bool{},
		subs: []database.PushSubscription{
			{Endpoint: "https://p/mrossi", Username: "mrossi"},
			{Endpoint: "https://p/anon"},
			{Endpoint: "https://p/vecchio", Username: "mrossi"},
		},
	}
	pu := &fakePusher{gone: map[string]bool{"https://p/vecchio": true}}
	hub := NewHub(10)
	c, _ := hub.Subscribe("mrossi")
	d := &Dispatcher{Store: st, Hub: hub, Pusher: pu, Now: time.Now,
		Visible: func(u string, id int64) bool { return u == "mrossi" }}

	if err := d.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(pu.sent) != 2 || pu.sent[0] != "https://p/mrossi" {
		t.Fatalf("push solo ai destinatari: %v", pu.sent)
	}
	if len(st.deleted) != 1 || st.deleted[0] != "https://p/vecchio" {
		t.Fatalf("iscrizione scaduta da cancellare: %v", st.deleted)
	}
	select {
	case <-c.Events:
	case <-time.After(time.Second):
		t.Fatal("evento SSE mancante")
	}
	if err := d.RunOnce(context.Background()); err != nil || len(pu.sent) != 2 {
		t.Fatalf("secondo ciclo: nessuna nuova notifica, inviati %v", pu.sent)
	}
}

func TestDispatcherWithoutPush(t *testing.T) {
	st := &fakeStore{pending: []database.Alert{{ID: 2, Title: "x", Notify: true}}, marked: map[int64]bool{}}
	d := &Dispatcher{Store: st, Hub: NewHub(1), Now: time.Now, Visible: func(string, int64) bool { return true }}
	if err := d.RunOnce(context.Background()); err != nil || !st.marked[2] {
		t.Fatalf("senza Pusher: avviso comunque marcato, err=%v", err)
	}
}
```

- [ ] **Step 2: verifica che falliscano**

Run: `go test ./internal/notify/`
Expected: errori di compilazione.

- [ ] **Step 3: hub**

`internal/notify/hub.go`:

```go
package notify

import (
	"errors"
	"sync"
)

// ErrTooMany: limite di connessioni SSE raggiunto (o hub chiuso).
var ErrTooMany = errors.New("notify: troppe connessioni")

// Event è ciò che una plancia aperta riceve quando parte una notifica.
type Event struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Level string `json:"level"`
}

// Client è una plancia collegata a /eventi.
type Client struct {
	Username string
	Events   <-chan Event
	ch       chan Event
}

// Hub tiene le connessioni SSE aperte.
type Hub struct {
	mu      sync.Mutex
	clients map[*Client]bool
	max     int
	done    chan struct{}
	closed  bool
}

func NewHub(max int) *Hub {
	return &Hub{clients: map[*Client]bool{}, max: max, done: make(chan struct{})}
}

func (h *Hub) Subscribe(username string) (*Client, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || len(h.clients) >= h.max {
		return nil, ErrTooMany
	}
	ch := make(chan Event, 8)
	c := &Client{Username: username, Events: ch, ch: ch}
	h.clients[c] = true
	return c, nil
}

func (h *Hub) Unsubscribe(c *Client) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
}

// Broadcast invia e alle plance per cui visible(username) è vero. Un client
// lento perde l'evento invece di bloccare gli altri.
func (h *Hub) Broadcast(e Event, visible func(username string) bool) {
	h.mu.Lock()
	targets := make([]*Client, 0, len(h.clients))
	for c := range h.clients {
		targets = append(targets, c)
	}
	h.mu.Unlock()
	for _, c := range targets {
		if !visible(c.Username) {
			continue
		}
		select {
		case c.ch <- e:
		default:
		}
	}
}

// Done si chiude con Close: gli handler SSE terminano (shutdown rapido).
func (h *Hub) Done() <-chan struct{} { return h.done }

func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.closed {
		h.closed = true
		close(h.done)
	}
}
```

- [ ] **Step 4: dispatcher**

`internal/notify/dispatcher.go`:

```go
package notify

import (
	"context"
	"log/slog"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

// Store: ciò che il dispatcher legge e scrive nel database.
type Store interface {
	PendingNotifications(now time.Time) ([]database.Alert, error)
	MarkNotified(id int64, at time.Time) (bool, error)
	ListPushSubscriptions() ([]database.PushSubscription, error)
	DeletePushSubscription(endpoint string) error
	TouchPushSubscription(endpoint string, at time.Time) error
}

// Dispatcher manda una sola volta la notifica degli avvisi che la richiedono,
// alle plance aperte (Hub) e ai browser iscritti (Pusher, nil = push spento).
type Dispatcher struct {
	Store   Store
	Hub     *Hub
	Pusher  Pusher
	Visible func(username string, alertID int64) bool // chi può vedere l'avviso
	Now     func() time.Time
}

func (d *Dispatcher) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if err := d.RunOnce(ctx); err != nil {
			slog.Warn("notifiche", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (d *Dispatcher) RunOnce(ctx context.Context) error {
	now := d.Now()
	alerts, err := d.Store.PendingNotifications(now)
	if err != nil {
		return err
	}
	for _, a := range alerts {
		// Marcato prima dell'invio: al massimo una notifica, anche se il
		// processo si ferma a metà (qualche invio può andare perso: accettato).
		ok, err := d.Store.MarkNotified(a.ID, now)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		visible := func(u string) bool { return d.Visible(u, a.ID) }
		d.Hub.Broadcast(Event{ID: a.ID, Title: a.Title, Level: a.Level}, visible)
		if d.Pusher != nil {
			d.push(ctx, a, visible, now)
		}
		slog.Info("notifica inviata", "alert", a.ID)
	}
	return nil
}

func (d *Dispatcher) push(ctx context.Context, a database.Alert, visible func(string) bool, now time.Time) {
	subs, err := d.Store.ListPushSubscriptions()
	if err != nil {
		slog.Warn("iscrizioni push", "err", err)
		return
	}
	payload := Payload(a)
	for _, s := range subs {
		if !visible(s.Username) {
			continue
		}
		sctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		gone, err := d.Pusher.Send(sctx, s, payload, a.Level == database.LevelUrgent)
		cancel()
		switch {
		case gone:
			if err := d.Store.DeletePushSubscription(s.Endpoint); err != nil {
				slog.Warn("rimozione iscrizione push", "err", err)
			}
		case err != nil:
			slog.Warn("invio push", "err", err)
		default:
			d.Store.TouchPushSubscription(s.Endpoint, now)
		}
	}
}
```

- [ ] **Step 5: verifica**

Run: `go test ./internal/notify/ && go vet ./...`
Expected: PASS.

- [ ] **Step 6: commit**

```bash
git add internal/notify
git commit -m "feat(notify): hub SSE e dispatcher (una notifica per avviso, solo ai destinatari)"
```

---

### Task 4: `/eventi`, visibilità per utente e ciclo di vita

**Files:**
- Modify: `internal/web/profiles.go` (profilo per username), `internal/web/server.go`, `cmd/server/main.go`
- Create: `internal/web/events.go`, `internal/web/events_test.go`

**Interfaces:**
- Consumes: `notify.Hub`, `notify.Dispatcher`, `notify.WebPusher` (Task 2–3), `db.EnsureVAPIDKeys` (Task 1).
- Produces:
  - `func (s *Server) alertVisibleTo(username string, alertID int64) bool`
  - campi `Server.hub *notify.Hub`, `Server.vapidPublic string`, `Server.pusher notify.Pusher` (nil = push spento)
  - `func (s *Server) StartNotifications(ctx context.Context)` (avvia il dispatcher ogni 30 s); `func (s *Server) Close()` (chiude l'hub)
  - variabile di pacchetto `sseHeartbeat = 25 * time.Second` (i test la riducono)
  - route `GET /eventi`

- [ ] **Step 1: test che falliscono**

`internal/web/events_test.go`:

```go
package web

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/audience"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/notify"
)

func TestEventsStream(t *testing.T) {
	sseHeartbeat = 50 * time.Millisecond
	defer func() { sseHeartbeat = 25 * time.Second }()
	s, _ := newTestServer(t, nil)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/eventi", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Content-Type") != "text/event-stream" || resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("header: %v", resp.Header)
	}
	r := bufio.NewReader(resp.Body)
	if line, _ := r.ReadString('\n'); !strings.HasPrefix(line, ":") {
		t.Fatalf("primo heartbeat atteso, ottenuto %q", line)
	}
	s.hub.Broadcast(notify.Event{ID: 9, Title: "Sciopero", Level: "urgent"}, func(string) bool { return true })
	deadline := time.After(2 * time.Second)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(line, "data: ") && strings.Contains(line, `"Sciopero"`) {
			break
		}
		select {
		case <-deadline:
			t.Fatal("evento non ricevuto")
		default:
		}
	}
}

func TestEventsCloseUnblocks(t *testing.T) {
	s, _ := newTestServer(t, nil)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/eventi")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	done := make(chan struct{})
	go func() { bufio.NewReader(resp.Body).ReadString(0); close(done) }()
	s.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close deve chiudere i flussi SSE (shutdown rapido)")
	}
}

func TestAlertVisibleTo(t *testing.T) {
	s, db := newTestServer(t, nil)
	db.CreateAudienceAttribute("physicalDeliveryOfficeName", "Ufficio")
	trib, _ := db.CreateAudienceGroup("Tributi")
	db.AddAudienceRule(database.AudienceRule{GroupID: trib, Kind: audience.KindAttr, Attr: "physicalDeliveryOfficeName", Value: "TRIBUTI"})
	pub, _ := db.CreateAlert(database.Alert{Title: "per tutti", Level: database.LevelNews, StartsAt: fixedNow})
	ris, _ := db.CreateAlert(database.Alert{Title: "solo tributi", Level: database.LevelNews, StartsAt: fixedNow})
	db.SetContentAudience(database.ContentAlert, ris, database.ContentAudience{Mode: audience.ModeOnly, Groups: []int64{trib}})
	if !s.alertVisibleTo("", pub) || s.alertVisibleTo("", ris) {
		t.Fatal("anonimo: solo avvisi pubblici")
	}
	if !s.alertVisibleTo("mrossi", ris) || s.alertVisibleTo("senzanome", ris) {
		t.Fatal("riservato: visibile al membro, non agli altri")
	}
}
```

- [ ] **Step 2: verifica che falliscano**

Run: `go test ./internal/web/ -run 'Events|AlertVisibleTo'`
Expected: errori di compilazione (`sseHeartbeat`, `s.hub`, `s.Close`, `alertVisibleTo`).

- [ ] **Step 3: profilo per username**

In `internal/web/profiles.go` estrarre da `viewerProfile` la parte che lavora sul solo username:

```go
// profileFor: profilo AD di username (dalla cache). ok=false se username vuoto,
// directory assente o AD non disponibile.
func (s *Server) profileFor(username string) (audience.Profile, bool) {
	if username == "" || s.directory == nil {
		return audience.Profile{}, false
	}
	attrs, err := s.db.ListAudienceAttributes()
	if err != nil {
		slog.Warn("attributi dei gruppi", "err", err)
		return audience.Profile{}, false
	}
	names := make([]string, len(attrs))
	for i, a := range attrs {
		names[i] = a.Name
	}
	return s.profiles.get(username, func() (audience.Profile, error) { return s.directory.Profile(username, names) })
}

// groupsFor: gruppi della plancia di username. known=false → solo pubblici.
func (s *Server) groupsFor(username string) (map[int64]bool, bool) {
	p, ok := s.profileFor(username)
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

// alertVisibleTo: stessa regola della plancia, per un utente (o "" = anonimo).
func (s *Server) alertVisibleTo(username string, alertID int64) bool {
	ca, err := s.db.GetContentAudience(database.ContentAlert, alertID)
	if err != nil {
		return false
	}
	memberOf, known := s.groupsFor(username)
	return audience.Visible(ca.Mode, ca.Groups, memberOf, known)
}
```

e riscrivere `viewerProfile` e `viewerGroups` usandoli:

```go
func (s *Server) viewerProfile(r *http.Request) (identity.User, audience.Profile, bool) {
	u, ok := s.viewer(r)
	if !ok || u.Anonymous {
		return u, audience.Profile{}, false
	}
	p, ok := s.profileFor(u.Username)
	return u, p, ok
}

func (s *Server) viewerGroups(r *http.Request) (map[int64]bool, bool) {
	u, ok := s.viewer(r)
	if !ok || u.Anonymous {
		return nil, false
	}
	return s.groupsFor(u.Username)
}
```

(import di `database` in `profiles.go`).

- [ ] **Step 4: hub, push e ciclo di vita nel server**

`internal/web/server.go`:
- campi: `hub *notify.Hub`, `pusher notify.Pusher`, `vapidPublic string`
- in `New`, dopo `s.cookies = …`:

```go
	s.hub = notify.NewHub(2000)
	if o.Config.VAPIDSubject != "" {
		pub, priv, err := o.DB.EnsureVAPIDKeys(webpush.GenerateVAPIDKeys)
		if err != nil {
			return nil, fmt.Errorf("chiavi VAPID: %w", err)
		}
		s.vapidPublic = pub
		s.pusher = &notify.WebPusher{Subject: o.Config.VAPIDSubject, PublicKey: pub, PrivateKey: priv}
	}
```

- route dopo `/io`: `s.mux.HandleFunc("GET /eventi", s.handleEvents)`
- metodi:

```go
// StartNotifications avvia il dispatcher delle notifiche (ogni 30 s).
func (s *Server) StartNotifications(ctx context.Context) {
	d := &notify.Dispatcher{Store: s.db, Hub: s.hub, Pusher: s.pusher, Visible: s.alertVisibleTo, Now: s.now}
	go d.Run(ctx, 30*time.Second)
}

// Close chiude i flussi SSE aperti: senza, lo shutdown attenderebbe ogni client.
func (s *Server) Close() { s.hub.Close() }
```

`internal/web/events.go`:

```go
package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// sseHeartbeat: commento periodico, perché nginx chiude dopo 60 s di silenzio.
var sseHeartbeat = 25 * time.Second

// handleEvents è il flusso SSE delle plance aperte: arriva un evento quando
// parte la notifica di un avviso visibile a chi guarda.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming non supportato", http.StatusInternalServerError)
		return
	}
	username := ""
	if u, ok := s.viewer(r); ok && !u.Anonymous {
		username = u.Username
	}
	c, err := s.hub.Subscribe(username)
	if err != nil {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "troppe connessioni", http.StatusServiceUnavailable)
		return
	}
	defer s.hub.Unsubscribe(c)

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no") // nginx: niente buffer sulla risposta
	fmt.Fprint(w, ": ok\n\n")
	flusher.Flush()

	tick := time.NewTicker(sseHeartbeat)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.hub.Done():
			return
		case <-tick.C:
			fmt.Fprint(w, ": ping\n\n")
		case e := <-c.Events:
			b, _ := json.Marshal(e)
			fmt.Fprintf(w, "event: avviso\ndata: %s\n\n", b)
		}
		flusher.Flush()
	}
}
```

`cmd/server/main.go`: dopo la creazione di `srv`, `srv.StartNotifications(ctx)`; prima di `httpSrv.Shutdown(...)`, `srv.Close()`.

Nota: `securityHeaders` o altri wrapper devono lasciar passare `http.Flusher`. Se `w.(http.Flusher)` fallisce nel test, il wrapper del middleware va esteso con un metodo `Flush()` (o `Unwrap()`), senza cambiarne il comportamento.

- [ ] **Step 5: verifica**

Run: `go test ./... && go vet ./... && gofmt -l internal/ cmd/`
Expected: PASS.

- [ ] **Step 6: commit**

```bash
git add internal/web cmd/server/main.go
git commit -m "feat(web): flusso SSE /eventi, dispatcher delle notifiche e shutdown pulito"
```

---

### Task 5: iscrizioni push, service worker, manifest e admin

**Files:**
- Create: `internal/web/push.go`, `internal/web/push_test.go`, `web/static/sw.js`, `web/static/manifest.webmanifest`, `web/static/img/icon-192.png`, `web/static/img/icon-512.png`
- Modify: `internal/web/server.go` (route), `internal/web/admin_alerts.go`, `web/templates/admin_avvisi.html`, `web/static/js/admin.js`

**Interfaces:**
- Consumes: `db.SavePushSubscription`, `db.DeletePushSubscription`, `db.ListPushSubscriptionsFor` (Task 1); `s.pusher`, `s.vapidPublic` (Task 4); `notify.Payload`.
- Produces: route `GET /push/chiave`, `POST /push/iscrizioni`, `POST /push/iscrizioni/rimuovi`, `GET /sw.js`, `GET /manifest.webmanifest`, `POST /admin/notifiche/prova`; campo form avvisi `notify=1`.

- [ ] **Step 1: test che falliscono**

`internal/web/push_test.go`:

```go
package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/identity"
)

func postJSON(t *testing.T, s *Server, path, body string, c *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if c != nil {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestPushKeyDisabledWithoutSubject(t *testing.T) {
	s, _ := newTestServer(t, nil)
	if rec := do(t, s, "GET", "/push/chiave", nil, nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("push spento: atteso 404, ottenuto %d", rec.Code)
	}
}

func TestPushSubscribeAndRemove(t *testing.T) {
	s, db := newTestServerWith(t, nil, func(o *Options) { o.Config.VAPIDSubject = "mailto:supporto@example.it" })
	if rec := do(t, s, "GET", "/push/chiave", nil, nil, nil); rec.Code != 200 || len(strings.TrimSpace(rec.Body.String())) < 40 {
		t.Fatalf("chiave pubblica: %d %q", rec.Code, rec.Body)
	}
	c := viewerCookie(t, s, identity.User{Username: "mrossi", Name: "Mario Rossi"})
	body := `{"endpoint":"https://push.example/x","keys":{"p256dh":"BAAA","auth":"AAAA"}}`
	rec := postJSON(t, s, "/push/iscrizioni", body, c)
	var res map[string]bool
	json.Unmarshal(rec.Body.Bytes(), &res)
	if rec.Code != 200 || !res["ok"] {
		t.Fatalf("iscrizione: %d %s", rec.Code, rec.Body)
	}
	if mine, _ := db.ListPushSubscriptionsFor("mrossi"); len(mine) != 1 {
		t.Fatalf("iscrizione non associata all'utente: %+v", mine)
	}
	for _, bad := range []string{
		`{"endpoint":"http://push.example/x","keys":{"p256dh":"BAAA","auth":"AAAA"}}`,
		`{"endpoint":"https://push.example/` + strings.Repeat("a", 1100) + `","keys":{"p256dh":"BAAA","auth":"AAAA"}}`,
		`{"endpoint":"https://push.example/y","keys":{"p256dh":"","auth":"AAAA"}}`,
		`non json`,
	} {
		rec := postJSON(t, s, "/push/iscrizioni", bad, nil)
		json.Unmarshal(rec.Body.Bytes(), &res)
		if rec.Code != 200 || res["ok"] {
			t.Errorf("iscrizione non valida accettata: %s", bad)
		}
	}
	postJSON(t, s, "/push/iscrizioni/rimuovi", `{"endpoint":"https://push.example/x"}`, nil)
	if all, _ := db.ListPushSubscriptions(); len(all) != 0 {
		t.Fatalf("rimozione: %+v", all)
	}
}

func TestServiceWorkerAndManifest(t *testing.T) {
	s, _ := newTestServer(t, nil)
	sw := do(t, s, "GET", "/sw.js", nil, nil, nil)
	if sw.Code != 200 || !strings.Contains(sw.Header().Get("Content-Type"), "javascript") || sw.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("sw.js: %d %v", sw.Code, sw.Header())
	}
	m := do(t, s, "GET", "/manifest.webmanifest", nil, nil, nil)
	if m.Code != 200 || m.Header().Get("Content-Type") != "application/manifest+json" || !strings.Contains(m.Body.String(), `"start_url"`) {
		t.Fatalf("manifest: %d %v", m.Code, m.Header())
	}
	if body := do(t, s, "GET", "/", nil, nil, nil).Body.String(); !strings.Contains(body, `<link rel="manifest" href="/manifest.webmanifest">`) {
		t.Fatal("plancia senza link al manifest")
	}
}

func TestAlertFormNotifyCheckbox(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	page := do(t, s, "GET", "/admin/avvisi", nil, c, nil).Body.String()
	if !strings.Contains(page, `name="notify" value="1"`) {
		t.Fatal("casella Invia notifica mancante")
	}
	form := map[string][]string{"title": {"Sciopero"}, "level": {"urgent"}, "starts_at": {"2026-10-06T09:00"}, "notify": {"1"}}
	rec := do(t, s, "POST", "/admin/avvisi", form, c, hx)
	if rec.Code != 200 {
		t.Fatalf("salvataggio: %d", rec.Code)
	}
	all, _ := db.ListActiveAlerts(fixedNow)
	if !all[0].Notify {
		t.Fatal("notify non salvato")
	}
}
```

(`do` accetta `url.Values`: convertire le mappe dei test in `url.Values{...}` se il compilatore lo richiede.)

- [ ] **Step 2: verifica che falliscano**

Run: `go test ./internal/web/ -run 'Push|ServiceWorker|NotifyCheckbox'`
Expected: FAIL / 404.

- [ ] **Step 3: endpoint push**

`internal/web/push.go`:

```go
package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/notify"
)

// Il reverse proxy riscrive i 4xx: questi endpoint rispondono sempre 200
// con {"ok":true|false}.
func pushReply(w http.ResponseWriter, ok bool) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string]bool{"ok": ok})
}

func (s *Server) handlePushKey(w http.ResponseWriter, r *http.Request) {
	if s.pusher == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	fmt.Fprint(w, s.vapidPublic)
}

type pushBody struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

func readPushBody(r *http.Request) (pushBody, bool) {
	var b pushBody
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 8<<10)).Decode(&b); err != nil {
		return b, false
	}
	return b, strings.HasPrefix(b.Endpoint, "https://") && len(b.Endpoint) <= 1024
}

func (s *Server) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	b, ok := readPushBody(r)
	if !ok || s.pusher == nil || b.Keys.P256dh == "" || b.Keys.Auth == "" || len(b.Keys.P256dh) > 200 || len(b.Keys.Auth) > 100 {
		pushReply(w, false)
		return
	}
	username := ""
	if u, ok := s.viewer(r); ok && !u.Anonymous {
		username = u.Username
	}
	err := s.db.SavePushSubscription(database.PushSubscription{Endpoint: b.Endpoint, P256dh: b.Keys.P256dh, Auth: b.Keys.Auth, Username: username, CreatedAt: s.now()})
	pushReply(w, err == nil)
}

func (s *Server) handlePushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	b, ok := readPushBody(r)
	if !ok {
		pushReply(w, false)
		return
	}
	pushReply(w, s.db.DeletePushSubscription(b.Endpoint) == nil)
}

// handlePushTest invia una notifica di prova alle iscrizioni dell'admin.
func (s *Server) handlePushTest(w http.ResponseWriter, r *http.Request) {
	msg := ""
	switch subs, err := s.db.ListPushSubscriptionsFor(s.currentAdmin(r)); {
	case s.pusher == nil:
		msg = "Web Push spento: imposta VAPID_SUBJECT."
	case err != nil:
		s.serverError(w, err)
		return
	case len(subs) == 0:
		msg = "Nessuna iscrizione per il tuo utente: apri la plancia da questo PC e attiva le notifiche."
	default:
		payload := notify.Payload(database.Alert{ID: 0, Title: "Notifica di prova", Body: "Le notifiche di CruscottoPA funzionano."})
		sent, failed := 0, 0
		for _, sub := range subs {
			gone, err := s.pusher.Send(r.Context(), sub, payload, false)
			switch {
			case gone:
				s.db.DeletePushSubscription(sub.Endpoint)
				failed++
			case err != nil:
				failed++
			default:
				sent++
			}
		}
		msg = fmt.Sprintf("Iscrizioni: %d · inviate: %d · non riuscite: %d.", len(subs), sent, failed)
	}
	s.render(w, http.StatusOK, "push_test_result", msg)
}

func (s *Server) handleServiceWorker(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, s.webDir+"/static/sw.js")
}

func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/manifest+json")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, s.webDir+"/static/manifest.webmanifest")
}
```

Route in `server.go`: `GET /push/chiave`, `POST /push/iscrizioni`, `POST /push/iscrizioni/rimuovi`, `GET /sw.js`, `GET /manifest.webmanifest` (pubbliche) e `POST /admin/notifiche/prova` con `requireAdmin`. Le POST pubbliche passano dal controllo CSRF esistente (`CrossOriginProtection`): `fetch` same-origin è ammesso.

`web/static/manifest.webmanifest`:

```json
{
  "name": "CruscottoPA",
  "short_name": "CruscottoPA",
  "start_url": "/",
  "scope": "/",
  "display": "standalone",
  "background_color": "#f4f6fa",
  "theme_color": "#1565c0",
  "icons": [
    { "src": "/static/img/icon-192.png", "sizes": "192x192", "type": "image/png" },
    { "src": "/static/img/icon-512.png", "sizes": "512x512", "type": "image/png", "purpose": "any maskable" }
  ]
}
```

Icone: rasterizzare `web/static/img/logo.svg` con Chrome headless (sfondo trasparente) a 192 e 512 px, come per `favicon.ico`, salvando `icon-192.png` e `icon-512.png`.

`web/static/sw.js`:

```js
// Service worker di CruscottoPA: notifiche push e installazione come app.
// Nessuna cache offline: la plancia ha senso solo online.
self.addEventListener("install", () => self.skipWaiting());
self.addEventListener("activate", (e) => e.waitUntil(self.clients.claim()));

self.addEventListener("push", (e) => {
	let d = {};
	try { d = e.data ? e.data.json() : {}; } catch (_) { d = { title: e.data && e.data.text() }; }
	e.waitUntil(self.registration.showNotification(d.title || "CruscottoPA", {
		body: d.body || "",
		icon: "/static/img/icon-192.png",
		badge: "/static/img/icon-192.png",
		tag: d.tag || "cruscottopa",
		data: { url: d.url || "/" },
	}));
});

self.addEventListener("notificationclick", (e) => {
	e.notification.close();
	const url = (e.notification.data && e.notification.data.url) || "/";
	e.waitUntil(self.clients.matchAll({ type: "window", includeUncontrolled: true }).then((list) => {
		for (const c of list) {
			if (new URL(c.url).origin === self.location.origin) {
				c.navigate(url);
				return c.focus();
			}
		}
		return self.clients.openWindow(url);
	}));
});
```

- [ ] **Step 4: form avvisi**

`admin_alerts.go`:
- `alertForm`: campo `Notify bool`; `newAlertForm`: `Notify: false`; `formFromAlert`: `Notify: a.Notify`; `alertRow` riceve già `Alert` (con `NotifiedAt`).
- salvataggio: `form.Notify = r.FormValue("notify") == "1"`; costruzione dell'avviso con `Notify: form.Notify`; togliere la riga `a.Notify = old.Notify` (resta la lettura di `old` solo se serve ad altro, altrimenti va tolta del tutto insieme alla variabile).

`admin_avvisi.html`:
- nel form, dopo la fonte: `<label class="inline"><input type="checkbox" name="notify" value="1" data-notify{{if .Form.Notify}} checked{{end}}>Invia notifica (quando l'avviso diventa attivo, a chi può vederlo)</label>`
- nel template `alert_rows`, nella colonna del titolo: `{{with .NotifiedAt}}<br><small class="muted">notificato il {{fmtDate .}}</small>{{end}}` (adattare se `fmtDate` vuole `time.Time`: usare `fmtDatePtr .NotifiedAt` sull'elemento)
- in fondo alla sezione: `<div class="card"><h2>Notifiche</h2><button type="button" hx-post="/admin/notifiche/prova" hx-target="#push-test" hx-swap="innerHTML">Invia una notifica di prova a me</button><div id="push-test"></div></div>` e il template `{{define "push_test_result"}}<p class="flash">{{.}}</p>{{end}}`.

`admin.js`: spuntare la casella quando si sceglie "Urgente", finché l'admin non l'ha toccata:

```js
	// "Invia notifica": proposta per gli urgenti, finché l'admin non la cambia.
	document.addEventListener("change", (e) => {
		const box = e.target.closest("[data-notify]");
		if (box) { box.dataset.touched = "1"; return; }
		if (e.target.name !== "level") return;
		const cb = e.target.form && e.target.form.querySelector("[data-notify]");
		if (cb && !cb.dataset.touched) cb.checked = e.target.value === "urgent";
	});
```

- [ ] **Step 5: verifica**

Run: `go test ./... && go vet ./... && gofmt -l internal/ && node --check web/static/sw.js web/static/js/admin.js`
Expected: PASS.

- [ ] **Step 6: commit**

```bash
git add internal/web web/static web/templates
git commit -m "feat: iscrizioni push, service worker, manifest e casella Invia notifica"
```

---

### Task 6: plancia (popup di primo accesso, notifiche, footer)

**Files:**
- Create: `web/static/js/notifiche.js`
- Modify: `web/templates/dashboard.html`, `web/templates/avvisi.html`, `web/static/css/plancia.css`
- Test: `internal/web/push_test.go` (markup)

**Interfaces:**
- Consumes: `/eventi`, `/push/chiave`, `/push/iscrizioni`, `/push/iscrizioni/rimuovi`, `/sw.js` (Task 4–5).
- Produces: `<dialog class="notify-ask">`, link `[data-notifiche]` nel footer, `<link rel="manifest">`, `<meta name="theme-color">`.

- [ ] **Step 1: test che fallisce**

In `internal/web/push_test.go`:

```go
func TestDashboardNotifyMarkup(t *testing.T) {
	s, _ := newTestServer(t, nil)
	body := do(t, s, "GET", "/", nil, nil, nil).Body.String()
	for _, want := range []string{`<script src="/static/js/notifiche.js" defer></script>`, `<dialog class="notify-ask"`, `data-notifiche`, `<meta name="theme-color" content="#1565c0">`} {
		if !strings.Contains(body, want) {
			t.Errorf("plancia: manca %q", want)
		}
	}
}
```

- [ ] **Step 2: verifica che fallisca**

Run: `go test ./internal/web/ -run DashboardNotifyMarkup`
Expected: FAIL.

- [ ] **Step 3: markup e stili**

`dashboard.html`, in `plancia_head`: `<link rel="manifest" href="/manifest.webmanifest">`, `<meta name="theme-color" content="#1565c0">`, `<script src="/static/js/notifiche.js" defer></script>`.

Prima di `</body>` della plancia:

```html
	<dialog class="notify-ask" aria-labelledby="notify-ask-title">
		<h2 id="notify-ask-title">Ricevi le notifiche degli avvisi?</h2>
		<p>Ti avvisiamo subito di comunicazioni urgenti e manutenzioni, anche quando la plancia non è aperta.</p>
		<div class="notify-ask-actions">
			<button type="button" class="primary" data-notify-yes>Attiva le notifiche</button>
			<button type="button" data-notify-no>Non ora</button>
		</div>
	</dialog>
```

Footer di plancia e avvisi: dopo il link al repository ` · <a class="foot-link" href="#" data-notifiche hidden>Notifiche</a>` (testo impostato dal JS: "Notifiche: attiva" / "Notifiche: disattiva").

`plancia.css`:

```css
dialog.notify-ask { border: 0; border-radius: 16px; padding: 1.3rem 1.4rem; width: min(440px, 92vw); box-shadow: 0 30px 80px rgba(0, 0, 0, .35); color: var(--p-text); }
dialog.notify-ask h2 { margin: 0 0 .5rem; font-size: 1.15rem; }
dialog.notify-ask p { margin: 0 0 1rem; color: var(--p-text-2); line-height: 1.5; }
.notify-ask-actions { display: flex; gap: .6rem; justify-content: flex-end; }
.notify-ask-actions button { border: 1px solid var(--p-line); background: #fff; border-radius: 10px; padding: .55rem 1rem; font: inherit; cursor: pointer; }
.notify-ask-actions .primary { background: var(--p-blue-2); border-color: var(--p-blue-2); color: #fff; font-weight: 600; }
```

- [ ] **Step 4: `notifiche.js`**

```js
// Notifiche degli avvisi: flusso SSE a plancia aperta, iscrizione Web Push e
// popup di primo accesso. Nessun handler inline (CSP).
(() => {
	"use strict";
	const supported = "Notification" in window && "serviceWorker" in navigator;
	const ASK_KEY = "cruscotto-notifiche-non-ora";
	const ASK_DAYS = 30;

	function b64ToBytes(s) {
		const pad = "=".repeat((4 - (s.length % 4)) % 4);
		const raw = atob((s + pad).replace(/-/g, "+").replace(/_/g, "/"));
		return Uint8Array.from(raw, (c) => c.charCodeAt(0));
	}

	async function registration() {
		return navigator.serviceWorker.register("/sw.js");
	}

	async function subscribe() {
		const reg = await registration();
		const key = await fetch("/push/chiave", { credentials: "same-origin" });
		if (!key.ok) return; // Web Push spento: restano le notifiche a plancia aperta
		let sub = await reg.pushManager.getSubscription();
		if (!sub) {
			sub = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: b64ToBytes((await key.text()).trim()) });
		}
		await fetch("/push/iscrizioni", {
			method: "POST", credentials: "same-origin",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify(sub.toJSON()),
		});
	}

	async function unsubscribe() {
		const reg = await navigator.serviceWorker.getRegistration();
		const sub = reg && await reg.pushManager.getSubscription();
		if (!sub) return;
		await fetch("/push/iscrizioni/rimuovi", {
			method: "POST", credentials: "same-origin",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({ endpoint: sub.endpoint }),
		});
		await sub.unsubscribe();
	}

	function askedRecently() {
		try { return Date.now() - (Number(localStorage.getItem(ASK_KEY)) || 0) < ASK_DAYS * 864e5; } catch (_) { return true; }
	}

	async function enable() {
		const p = await Notification.requestPermission();
		if (p === "granted") await subscribe().catch(() => {});
		updateLink();
	}

	const link = document.querySelector("[data-notifiche]");
	function updateLink() {
		if (!link || !supported) return;
		link.hidden = false;
		link.textContent = Notification.permission === "granted" ? "Notifiche: disattiva" : "Notifiche: attiva";
		link.title = Notification.permission === "denied" ? "Bloccate dal browser: riattivale dalle impostazioni del sito" : "";
	}
	if (link) {
		link.addEventListener("click", async (e) => {
			e.preventDefault();
			if (Notification.permission === "granted") {
				await unsubscribe().catch(() => {});
				link.textContent = "Notifiche: attiva (riattiva anche dalle impostazioni del browser)";
			} else if (Notification.permission === "default") {
				await enable();
			}
		});
	}

	// Popup di primo accesso: la richiesta del browser parte dal clic.
	const ask = document.querySelector("dialog.notify-ask");
	function maybeAsk() {
		if (!supported || !ask || Notification.permission !== "default" || askedRecently()) return;
		if (document.querySelector("dialog.urgent[open]")) { setTimeout(maybeAsk, 3000); return; }
		ask.showModal();
	}
	if (ask) {
		ask.querySelector("[data-notify-yes]").addEventListener("click", async () => { ask.close(); await enable(); });
		ask.querySelector("[data-notify-no]").addEventListener("click", () => {
			try { localStorage.setItem(ASK_KEY, String(Date.now())); } catch (_) { /* ripresentato al prossimo accesso */ }
			ask.close();
		});
	}

	// Plancia aperta: eventi in tempo reale.
	if ("EventSource" in window && document.body.classList.contains("plancia")) {
		const es = new EventSource("/eventi");
		es.addEventListener("avviso", (e) => {
			let a = {};
			try { a = JSON.parse(e.data); } catch (_) { return; }
			if (window.htmx && document.getElementById("alerts")) {
				htmx.ajax("GET", "/partials/alerts", { target: "#alerts", swap: "innerHTML" });
			}
			if (supported && Notification.permission === "granted" && document.visibilityState !== "visible") {
				navigator.serviceWorker.getRegistration().then((reg) => {
					const opts = { body: "", icon: "/static/img/icon-192.png", tag: "avviso-" + a.id, data: { url: "/" } };
					if (reg) reg.showNotification(a.title, opts); else new Notification(a.title, opts);
				});
			}
		});
	}

	if (supported) {
		updateLink();
		if (Notification.permission === "granted") subscribe().catch(() => {}); // anche se concesso da policy
		setTimeout(maybeAsk, 1500);
	}
})();
```

- [ ] **Step 5: verifica**

Run: `go test ./... && node --check web/static/js/notifiche.js`
Expected: PASS.

Verifica nel browser (Edge headless / Playwright, `LDAP_HOST=mock`, `VAPID_SUBJECT=mailto:test@example.it`): popup al primo accesso; "Non ora" non lo ripresenta al ricaricamento; con permesso concesso (Playwright `context.grantPermissions(["notifications"])`) la plancia si iscrive (`/push/iscrizioni` 200, riga in DB); creando in admin un avviso urgente con "Invia notifica", entro 30 s il carosello si aggiorna da solo.

- [ ] **Step 6: commit**

```bash
git add web internal/web
git commit -m "feat(plancia): popup di primo accesso, notifiche in tempo reale e iscrizione push"
```

---

### Task 7: verifica in produzione e documentazione

**Files:**
- Modify: `CLAUDE.md`, `docs/superpowers/specs/2026-10-07-notifiche-pwa-design.md` (stato)

- [ ] **Step 1: CLAUDE.md**

- Architettura: punto **`internal/notify`** (hub SSE, dispatcher ogni 30 s con `notified_at` impostato prima dell'invio, `WebPusher` con `webpush-go`; la libreria antepone `mailto:` da sola); endpoint `/eventi` (heartbeat 25 s, `X-Accel-Buffering: no`, `Server.Close()` prima dello shutdown), `/push/*` (sempre 200 + JSON per il proxy), `/sw.js`, `/manifest.webmanifest`; tabelle v6; `VAPID_SUBJECT`; policy Edge `NotificationsAllowedForUrls` per pre-autorizzare il sito.
- Route pubbliche: aggiungere i nuovi endpoint.

- [ ] **Step 2: spec**

`Stato: in revisione` → `Stato: implementata`.

- [ ] **Step 3: verifica finale**

Run: `go vet ./... && go test ./... && gofmt -l internal/ cmd/`
Expected: tutto PASS.

- [ ] **Step 4: commit**

```bash
git add CLAUDE.md docs/superpowers/specs/2026-10-07-notifiche-pwa-design.md
git commit -m "docs: notifiche e PWA in CLAUDE.md"
```

- [ ] **Step 5 (dopo il deploy, con l'utente): verifica SSE dietro nginx**

Sull'istanza di test: `curl -N https://…/eventi` deve mostrare `: ok` subito e `: ping` ogni 25 s. Se i messaggi arrivano tutti insieme o con ritardo, il proxy bufferizza: chiedere `proxy_buffering off;` per `location /eventi` su `revprx01`.
