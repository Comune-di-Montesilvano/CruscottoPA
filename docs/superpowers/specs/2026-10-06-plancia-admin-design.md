# Sotto-progetto 1 — Plancia + admin base

Data: 2026-10-06 · Stato: in revisione

## Contesto e obiettivo

CruscottoPA è la homepage dei dipendenti del Comune di Montesilvano: un unico punto da cui aprire gli applicativi (Rubrica, Webmail, Sito comunale, Albo pretorio, ComunicaPA, PagoPA, UtenzePA, gestionali esterni come TINN, Sicraweb, SUE, Sportello del cittadino), leggere gli avvisi del servizio informatico e trovare guide/FAQ, con l'obiettivo di ridurre i ticket di assistenza.

Il lavoro complessivo è diviso in quattro sotto-progetti, ciascuno con spec, piano e implementazione propri:

| # | Sotto-progetto | Dipende da |
|---|---|---|
| **1** | **Plancia + admin base** (questo documento) | — |
| 2 | Guide ricche: editor Markdown, upload immagini/PDF, guide da GitHub, conversione PDF esistenti | 1 |
| 3 | PWA installabile + notifiche desktop (SSE + Web Push VAPID) | 1 |
| 4 | Riconoscimento utente dal reverse proxy, gruppi/uffici, dashboard e avvisi filtrati | test header del proxy |

**Obiettivo del sotto-progetto 1:** dashboard "Plancia" uguale per tutti, completa e usabile in produzione, gestita interamente da un pannello admin protetto da login LDAP. Nessun contenuto hardcoded oltre a un seed minimo.

## Decisioni prese in brainstorming

- Direzione visiva **A "Plancia di comando"**: testata scura con saluto, data, orologio e ricerca; avvisi come striscia di pillole colorate; tile compatte; colonna laterale per le guide generali. CSS custom, nessun framework.
- Guide **agganciate alle app**: una guida può riferirsi a un'app (pulsante dedicato sulla tile) o essere **generale** (colonna laterale).
- Pulsante guide a **segmento laterale** (split-button): click su icona/nome apre l'applicativo, click sul segmento "? N" apre l'elenco guide dell'app.
- Icone: pacchetto **Material Icons classico** (stesso di UtenzePA, Apache 2.0, self-hosted), oppure icona caricata, oppure URL esterno; monogramma come fallback.
- Admin: login **LDAP/AD** come GoPulley/Rubrica.
- Seed al primo avvio: solo categoria "Applicativi" con **Rubrica** e **Webmail** (URL vuoto).

Mockup di riferimento: `.superpowers/brainstorm/*/content/guide-collegate.html` (variante 2) e `tile-split.html` (variante A).

## 1. Modello dati

Il DB non è ancora in produzione: lo schema viene riscritto da zero. Le tabelle `groups`/`profiles`/`group_links` e la colonna `links.is_guide` dello scaffold iniziale vengono rimosse; la visibilità per gruppo torna nel sotto-progetto 4 come tabelle dedicate.

```sql
CREATE TABLE categories (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    sort_order INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE apps (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    category_id INTEGER NOT NULL REFERENCES categories(id) ON DELETE RESTRICT,
    title       TEXT    NOT NULL,
    description TEXT    NOT NULL DEFAULT '',
    url         TEXT    NOT NULL DEFAULT '',
    icon_kind   TEXT    NOT NULL DEFAULT '',   -- '' (monogramma) | 'pack' | 'upload' | 'url'
    icon_value  TEXT    NOT NULL DEFAULT '',   -- nome Material Icon | nome file in /data/uploads/icons | URL
    icon_color  TEXT    NOT NULL DEFAULT '#475569',
    sort_order  INTEGER NOT NULL DEFAULT 0,
    enabled     INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1))
);

CREATE TABLE guides (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    app_id     INTEGER REFERENCES apps(id) ON DELETE SET NULL,  -- NULL = guida generale
    title      TEXT    NOT NULL,
    kind       TEXT    NOT NULL DEFAULT 'link',  -- 'link' | 'markdown' | 'github'
    url        TEXT    NOT NULL DEFAULT '',
    body       TEXT    NOT NULL DEFAULT '',
    sort_order INTEGER NOT NULL DEFAULT 0,
    enabled    INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1))
);

CREATE TABLE alerts (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    title      TEXT    NOT NULL,
    body       TEXT    NOT NULL DEFAULT '',
    level      TEXT    NOT NULL CHECK (level IN ('urgent', 'maintenance', 'news')),
    starts_at  TEXT    NOT NULL,                -- RFC 3339, UTC
    ends_at    TEXT,                            -- NULL = senza scadenza
    notify     INTEGER NOT NULL DEFAULT 0 CHECK (notify IN (0, 1)),  -- usato dal sotto-progetto 3
    created_at TEXT    NOT NULL,
    created_by TEXT    NOT NULL DEFAULT ''
);

CREATE INDEX idx_apps_category  ON apps(category_id);
CREATE INDEX idx_guides_app     ON guides(app_id);
CREATE INDEX idx_alerts_window  ON alerts(starts_at, ends_at);
```

Regole:

- **`guides.kind` e `apps.icon_kind` sono validati in Go, non con CHECK**: i sotto-progetti successivi aggiungono valori senza dover ricreare la tabella. Nel sotto-progetto 1 l'admin può creare solo guide `kind='link'`.
- **Migrazioni versionate** con `PRAGMA user_version`: un elenco ordinato di funzioni Go, ciascuna eseguita in transazione e seguita dall'aggiornamento di `user_version`. La migrazione 1 crea lo schema sopra; il seed è una migrazione separata (la 2), così non si ripete.
- **Seed (migrazione 2)**: categoria "Applicativi" (`sort_order=0`), app "Rubrica" (`icon_kind='pack'`, `icon_value='contacts'`) e "Webmail" (`icon_kind='pack'`, `icon_value='mail'`), entrambe con `url=''`.
- **Avviso attivo**: `starts_at <= now AND (ends_at IS NULL OR ends_at > now)`. Ordine: livello (`urgent`, `maintenance`, `news`), poi `starts_at` decrescente.
- **App visibile in dashboard**: `enabled=1 AND url<>''`. Le app senza URL compaiono solo in admin, segnalate come "da completare".
- **Guida visibile**: `enabled=1` e, se agganciata, app visibile. Una guida agganciata a un'app disabilitata o senza URL non appare (né sulla tile né tra le generali).
- **Ordinamento**: `sort_order`, poi `title COLLATE NOCASE`.
- Date memorizzate in UTC (RFC 3339); visualizzate e inserite nel fuso `TZ` del container (default `Europe/Rome`).

## 2. Dashboard (`GET /`)

### Dati

Il handler costruisce un `Dashboard` con query semplici e separate:

```go
type Dashboard struct {
    Alerts        []Alert
    Categories    []CategoryWithApps // solo categorie con almeno un'app visibile
    GeneralGuides []Guide
    Version       string
}
type CategoryWithApps struct {
    Category
    Apps []AppWithGuides
}
type AppWithGuides struct {
    App
    Guides []Guide
}
```

### Layout (Plancia)

- **Testata scura**: saluto per fascia oraria (Buongiorno < 13:00 ≤ Buon pomeriggio < 18:00 ≤ Buonasera), data in italiano, orologio `HH:MM`. Saluto, data e orologio sono calcolati e aggiornati via JS sull'ora del client; il server rende un valore iniziale.
- **Ricerca**: campo nella testata, scorciatoia `/` per il focus, `Esc` per svuotare. Filtra lato client (JS vanilla, nessuna chiamata al server) tile e guide per titolo e descrizione, case- e accent-insensitive. Le categorie rimaste vuote si nascondono; se non resta nulla compare "Nessun risultato".
- **Striscia avvisi**: pillole colorate per livello (urgente rosso, manutenzione ambra, novità blu). Click su una pillola apre un `<dialog>` con titolo, periodo e testo (testo semplice: a capo preservati, URL resi link). La striscia è un frammento `GET /partials/alerts` ricaricato da HTMX ogni 5 minuti (`hx-trigger="every 300s"`). Nessun avviso attivo → striscia assente.
- **Tile**: icona + nome + descrizione; l'area principale è un `<a target="_blank" rel="noopener">` verso l'app. Se l'app ha guide, a destra un **segmento** separato da un bordo verticale con "?" e il conteggio, che apre un pannello tramite attributo HTML `popover` (`popovertarget`): elenco guide dell'app, ciascuna link in nuova scheda. Nessun JS per il popover; si chiude con Esc o click esterno.
- **Icone** nella tile, in ordine di fallback:
  1. `pack` → `<span class="material-icons">` su sfondo `icon_color`;
  2. `upload` → `<img src="/uploads/icons/<file>">`;
  3. `url` → `<img src="<url>">`; se il caricamento fallisce, `dashboard.js` (listener `error` in fase di capture, nessun `onerror` inline) lo sostituisce con il monogramma;
  4. `''` → monogramma (prime due lettere significative del titolo) su `icon_color`.
- **Colonna "Guide generali"**: a destra; assente se vuota.
- **Responsive**: griglia tile 4 colonne ≥ 1100px, 2 colonne ≥ 640px, 1 colonna sotto; sotto 900px la colonna guide scende sotto la griglia.
- **Footer**: versione (`AppVersion`).
- Nel sotto-progetto 1 **non** compaiono i pulsanti Installa e campanella (sotto-progetto 3).

### Asset statici

Tutto self-hosted in `web/static`, nessuna CDN:

- `htmx.min.js` (già presente);
- font Material Icons (`MaterialIcons-Regular.woff2`) + `LICENSE` Apache 2.0;
- `css/app.css`, `css/admin.css`;
- `js/dashboard.js` (orologio, saluto, ricerca, dialog avvisi), `js/admin.js` (anteprima icona).

L'elenco dei nomi Material Icons per la validazione e il picker è un file Go generato (`internal/icons/names.go`) a partire dal file `codepoints` del font, riusando l'approccio di UtenzePA (`frontend/src/app/core/helpers/material-icons.ts`).

### Header di sicurezza

Middleware applicato a tutte le risposte:

- `Content-Security-Policy: default-src 'self'; img-src 'self' https: data:; style-src 'self'; style-src-attr 'unsafe-inline'; script-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'`
- `X-Content-Type-Options: nosniff`
- `Referrer-Policy: same-origin`

Script e fogli di stile sono tutti in file esterni: nessun `<script>`/`<style>` inline né handler `on*=`. L'unica concessione è `style-src-attr 'unsafe-inline'`, necessaria per l'attributo `style="background:…"` con il colore dell'icona (valore validato `#rrggbb` e comunque escapato da `html/template` nel contesto CSS). HTMX è configurato con `htmx.config.includeIndicatorStyles = false` tramite `<meta name="htmx-config">` per evitare lo `<style>` inline che inietta di default.

## 3. Admin e autenticazione

### Login

- `internal/auth` portato da GoPulley: `Authenticate(username, password string, cfg) (ok, isAdmin bool, err error)` con StartTLS, `LDAP_TLS_SKIP_VERIFY`, bind di servizio opzionale, `LDAP_REQUIRED_GROUP`, `LDAP_ADMIN_GROUP`, `ADMIN_USERS`. `LDAP_HOST=mock` accetta qualsiasi credenziale (sviluppo); in mock è admin solo chi è in `ADMIN_USERS`, oppure chiunque se `ADMIN_USERS` è vuoto.
- Solo gli admin accedono: login valido ma non admin → pagina "Accesso non autorizzato" (403), nessuna sessione creata.
- `GET/POST /admin/login`, `POST /admin/logout`.

### Sessione

- `gorilla/sessions` CookieStore firmato e cifrato da `SESSION_SECRET` (obbligatorio fuori da `LDAP_HOST=mock`: l'avvio fallisce se assente o < 32 caratteri).
- Cookie `HttpOnly`, `SameSite=Lax`, `Path=/admin`, durata 8 ore. `Secure` se `X-Forwarded-Proto: https` oppure `SECURE_COOKIES=true` (come GoPulley).

### Protezioni

- **CSRF**: `http.NewCrossOriginProtection()` della standard library (Go ≥ 1.25) su tutte le route `/admin` non-GET; basato su `Sec-Fetch-Site`/`Origin`.
- **Rate limit login**: in memoria, chiave per username e per IP client; dopo 5 fallimenti in 15 minuti risposta 429 con attesa crescente (30s, 60s, 120s… max 15 min). Si azzera al riavvio. Finché il sotto-progetto 4 non definisce i proxy fidati, l'IP usato è `RemoteAddr` (eventualmente quello del proxy: il limite per username resta comunque efficace).
- **Sessione scaduta su richiesta HTMX** (`HX-Request: true`): risposta 401 con `HX-Redirect: /admin/login`. Senza HTMX: 303 verso `/admin/login`.

### Struttura delle pagine

Pagine separate, ciascuna con shell HTML completa e sidebar condivisa (`admin_rail.html`), come Rubrica. Ogni sezione ha un contenitore che fa da target HTMX: create/update/delete restituiscono solo il frammento della lista aggiornata.

| Route | Contenuto |
|---|---|
| `GET /admin` | Panoramica: avvisi attivi, app "da completare" (senza URL), conteggi |
| `/admin/avvisi` | CRUD avvisi: titolo, testo, livello, inizio, fine (opzionale) |
| `/admin/app` | CRUD app: categoria, titolo, descrizione, URL, icona, colore, abilitata; ordinamento ↑↓ |
| `/admin/guide` | CRUD guide `link`: titolo, URL, app collegata o "Generale", abilitata; ordinamento ↑↓ |
| `/admin/categorie` | CRUD categorie; ordinamento ↑↓ |
| `GET /admin/icone?q=` | Frammento HTMX: max 60 icone Material che contengono `q`, cliccabili per selezionarle |

- Eliminazioni con `hx-confirm`.
- Il campo icona dell'app ha tre schede: **Pacchetto** (ricerca + griglia via `/admin/icone`), **Carica file**, **URL**; anteprima live della tile.
- Gli avvisi scaduti restano in elenco admin (sezione "Scaduti", ultimi 30) per poterli riattivare modificando le date; nessuna pagina storico dedicata.

### Upload icone

- `POST` multipart, max 512 KB (`http.MaxBytesReader`).
- Tipo determinato dal contenuto: `http.DetectContentType` per PNG/WebP; per SVG controllo che il contenuto sia XML con root `<svg`. Altri tipi rifiutati.
- Salvataggio in `/data/uploads/icons/<16 byte random hex>.<ext>`; la directory è creata all'avvio. Il file precedente viene eliminato quando l'icona è sostituita o l'app eliminata.
- Servite da `GET /uploads/icons/{file}` (nome validato con regex `^[0-9a-f]{32}\.(png|webp|svg)$`) con header aggiuntivi `Content-Security-Policy: sandbox; default-src 'none'; style-src 'unsafe-inline'` e `X-Content-Type-Options: nosniff`, così uno script in un SVG non viene mai eseguito anche aprendo il file direttamente.
- Nuova env var `UPLOAD_DIR` (default `/data/uploads`).

### Variabili d'ambiente nuove

Da aggiungere in `docker-compose.yml`, `.env.example` e `internal/config`:

`SESSION_SECRET`, `SECURE_COOKIES`, `LDAP_HOST`, `LDAP_BASE_DN`, `LDAP_USER_DN_TEMPLATE`, `LDAP_STARTTLS`, `LDAP_TLS_SKIP_VERIFY`, `LDAP_BIND_DN`, `LDAP_BIND_PASSWORD`, `LDAP_REQUIRED_GROUP`, `LDAP_ADMIN_GROUP`, `ADMIN_USERS`, `UPLOAD_DIR`, `LOG_LEVEL`.

## 4. Struttura del codice

```
cmd/server/main.go          wiring: config, DB, auth, router, server, shutdown
internal/config             env → Config, validazione all'avvio
internal/database           apertura DB, migrazioni, query per categories/apps/guides/alerts
internal/auth               LDAP (da GoPulley), rate limiter
internal/icons              nomi Material Icons generati + Search(q)
internal/web                handler dashboard e admin, middleware (security headers,
                            requireAdmin, CSRF), render template, validazione form
web/templates               dashboard.html, partials, admin_*.html
web/static                  css, js, fonts, htmx
```

`main.go` resta sottile: gli handler vivono in `internal/web` come metodi di un `Server` che riceve dipendenze esplicite (DB, config, template, auth), invece delle variabili di package usate in Rubrica, così sono testabili con `httptest`.

## 5. Gestione errori

- **Validazione server-side** con messaggi per campo restituiti nel frammento del form (status 422):
  - URL solo `http`/`https` (rifiutati `javascript:`, `data:` ecc.);
  - titolo obbligatorio (max 120 caratteri), descrizione max 200;
  - `ends_at` > `starts_at`;
  - `icon_color` formato `#rrggbb`;
  - `icon_value` per `pack` presente nel catalogo Material Icons; per `url` valida come URL https/http.
- **Categoria con app**: eliminazione rifiutata con messaggio "Sposta o elimina prima le app di questa categoria".
- **Errori DB** nella dashboard: pagina di errore minimale 500, dettaglio solo nei log `slog`. `/health` restituisce 503 se il DB non risponde (già implementato).
- **Upload non valido**: 422 con messaggio (tipo non ammesso / file troppo grande).

## 6. Test

- `internal/database`:
  - migrazioni da DB vuoto all'ultima versione; idempotenza al riavvio; seed applicato una sola volta;
  - avvisi attivi: bordi `starts_at == now`, `ends_at == now`, `ends_at` NULL, ordinamento per livello;
  - app nascoste se `enabled=0` o `url=''`; guide di app nascoste escluse;
  - `RESTRICT` sulla categoria e `SET NULL` sulla guida all'eliminazione dell'app.
- `internal/auth`: modalità mock, `ADMIN_USERS`, rate limiter (soglia, backoff, reset finestra).
- `internal/icons`: `Search` (match parziale, limite risultati), `Valid`.
- `internal/web` con `httptest`:
  - dashboard: presenza avvisi, tile, segmento guide solo se l'app ha guide, colonna generali assente se vuota;
  - `/admin/*` senza sessione → 303; con `HX-Request` → 401 + `HX-Redirect`;
  - POST cross-origin → 403;
  - login non admin → 403 senza cookie;
  - upload: SVG con `<script>` accettato ma servito con CSP `sandbox`; PNG valido accettato; tipo non ammesso e file > 512 KB rifiutati; nome file non conforme → 404;
  - header di sicurezza presenti.
- Validazione: test a tabella su URL, colori, date.
- CI invariata (`test.yml`: build, vet, `-race`, Trivy).

## 7. Fuori scope

- Guide `markdown`/`github`, allegati PDF, editor, conversione PDF esistenti → sotto-progetto 2.
- Manifest PWA, service worker, pulsanti Installa e campanella, SSE, Web Push → sotto-progetto 3 (la colonna `alerts.notify` è già presente).
- Riconoscimento utente, proxy fidati, gruppi, filtri per ufficio → sotto-progetto 4.
- Tema scuro, storico avvisi dedicato, audit log delle modifiche admin, drag & drop per l'ordinamento.
