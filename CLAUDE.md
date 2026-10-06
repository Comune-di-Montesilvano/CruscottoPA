# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Progetto

**CruscottoPA** (module `github.com/Comune-di-Montesilvano/CruscottoPA`): portale Intranet / homepage dei dipendenti del Comune di Montesilvano. "Plancia": striscia avvisi, card degli applicativi per categoria con guide agganciate, guide generali; pannello `/admin` HTMX (login LDAP) per gestirli. CI/CD ereditata da GoPulley. Lavoro diviso in sotto-progetti (vedi `docs/superpowers/specs/`): 1 plancia+admin (fatto), backup e ripristino (fatto), 2 guide ricche (Markdown/GitHub/PDF), 3 PWA + notifiche (SSE + Web Push), 4 riconoscimento utente dal reverse proxy e filtri per ufficio. Futuro (non ancora progettato): modulo di invio ticket di assistenza — da decidere se integrarlo con un gestionale esterno o realizzarlo interno. Probabile: avvisi pubblicati dagli uffici (es. Ufficio Stipendi per i cedolini) con redattori per ufficio — gli avvisi hanno già il campo `source` (fonte).

Immagine: `ghcr.io/comune-di-montesilvano/cruscottopa` (minuscolo: GHCR/OCI lo richiedono). Binario/container/utente di sistema: `cruscottopa` (uid/gid 1001).

## Comandi

```bash
LDAP_HOST=mock go run ./cmd/server  # PORT=8080, DB_PATH=cruscotto.db; LDAP_HOST obbligatorio (nessun default)
# admin: http://localhost:8080/admin (mock: qualsiasi credenziale è admin — solo sviluppo)
go build ./...
go vet ./...
go test ./...
go test -run TestName ./internal/database/
docker compose up -d --build        # build locale, versione "dev-DEV"
```

Git Bash su Windows: prefissare `MSYS_NO_PATHCONV=1` ai comandi `docker run/exec` con path assoluti (es. `/app/cruscottopa -healthcheck`), altrimenti diventano `C:/Program Files/Git/...`.
Verifica immagine: `docker build --build-arg VERSION=x -t cp:check .` → `docker run -d -p 18090:8080 -e LDAP_HOST=mock cp:check` → `curl localhost:18090/health` (versione) + `docker inspect --format '{{.State.Health.Status}}'`.

## Test

- `internal/web`: `newTestServer(t, auth)` (DB temporaneo, `WebDir: "../../web"`, `fakeAuth`), `do(t, s, method, url, form, cookie, headers)` passa per tutta la catena middleware, `login(t, s)` restituisce il cookie admin, `hx` = header `HX-Request`. `fixedNow` = 08:00Z = 10:00 Europe/Rome.
- Asserzioni sull'HTML: `html/template` scrive `'` come `&#39;` (es. `l&#39;indirizzo`).
- `internal/database`: `newTestDB(t)` parte già con il seed (categoria "Applicativi" + Rubrica e Webmail senza URL): i conteggi nei test lo includono.

Driver SQLite `modernc.org/sqlite` (pure-Go, nome driver `"sqlite"`): build con `CGO_ENABLED=0`, nessun gcc su Windows. CGO serve solo per `go test -race` in CI. **Non passare a `mattn/go-sqlite3`** senza aggiornare Dockerfile (gcc/musl-dev, CGO_ENABLED=1) e sintassi DSN.

## Versione

`main.AppVersion` (default `dev`) è iniettata via `-ldflags "-X main.AppVersion=..."`. In `release.yml` vale il tag git (`github.ref_name`) o l'input manuale del `workflow_dispatch`; build locali via compose aggiungono `VERSION_SUFFIX=-DEV`. Esposta nel footer e in `GET /health` (`{"status","version"}`), anche come label OCI `org.opencontainers.image.version`. Tenere `softwareVersion`/`releaseDate` di `publiccode.yml` allineati al tag a ogni release.

## CI/CD

- `test.yml` (push/PR su `main`): build → vet → `go test -race` → Trivy fs **bloccante** (CRITICAL/HIGH). Il job si chiama `test`: è il required status check della branch protection — non rinominarlo.
- `release.yml` (tag `*` o manuale): build+push GHCR (`:<tag>` + `:latest`), Trivy image report-only → tab Security.
- Dependabot settimanale (gomod, docker, github-actions) con cooldown. Action pinnate per SHA.
- `.trivyignore`: solo falsi positivi verificati.
- `main` protetto (check `test` obbligatorio e strict, niente force push né delete): lavorare su branch e aprire PR. Spec e piani in `docs/superpowers/{specs,plans}/`.

## Vincoli di deploy (Portainer git-stack su Podman rootless)

- **Nome file `docker-compose.yml`**, non `compose.yml`: Portainer git-stack lo richiede.
- **Nessun `env_file`**: il clone Portainer non ha `.env` → il pull fallirebbe. Ogni variabile va elencata in `environment:` come `${VAR}`. Una nuova env var va aggiunta in tre posti: `docker-compose.yml`, `.env.example`, codice.
- **`/data` = volume nominato `cruscottopa-data`, mai bind mount**: Portainer clona in un path per-commit che non possiede ("permission denied"); in rootless un bind mount richiederebbe anche `:Z`/`:U` e uid mapping.
- **`pull_policy: always`** insieme a `build:`: senza, il redeploy Portainer può riusare una build locale e mostrare `dev` in prod.
- **Container non root (uid 1001), porte > 1024**: Podman rootless non apre porte privilegiate. Niente socket Docker montato (rootless non lo espone).
- **Healthcheck senza wget/curl**: `HEALTHCHECK` esegue `/app/cruscottopa -healthcheck` (GET `/health` su 127.0.0.1).
- **Mai `docker cp` sul DB live**: file root-owned + `-wal`/`-shm` disallineati → "attempt to write a readonly database". Usare `VACUUM INTO` per snapshot.
- Rete: rete di default del progetto compose; nessuna rete esterna dichiarata (una rete `external` inesistente fa fallire lo stack).
- **Ripristino = uscita del processo**: dopo lo swap il container deve ripartire da solo (`restart: unless-stopped`); senza restart policy resta fermo con i dati già ripristinati.

## Identificazione utente — attenzione all'IP

La dashboard deve riconoscere l'utente (IP, header del proxy o cookie). L'IP sorgente **non è affidabile** in container:
- **Podman rootless** con port forwarder `rootlessport`/`slirp4netns` (default fino a Podman 4) **nasconde l'IP client**: tutte le richieste arrivano dal gateway interno. Con `pasta` (default Podman 5) l'IP è preservato. Verificare con `podman info --format '{{.Host.NetworkBackend}}'` / `podman info | grep -i pasta` prima di basarsi sull'IP.
- Dietro reverse proxy l'IP è quello del proxy: leggere `X-Forwarded-For`/header utente **solo** se `RemoteAddr` appartiene a una lista di proxy fidati (CIDR configurabile), altrimenti l'header è falsificabile da chiunque.

## Architettura

- `cmd/server/main.go`: solo wiring — `config.Load()`, `database.Open`, `web.New`, graceful shutdown, flag `-healthcheck` (legge solo `PORT`, non valida il resto della config).
- `internal/config`: env → `Config`; validazione all'avvio (`SESSION_SECRET` ≥ 32 caratteri obbligatorio se `LDAP_HOST` ≠ `mock`; in mock, se assente, segreto casuale → le sessioni non sopravvivono al riavvio).
- `internal/database`: SQL raw su `modernc.org/sqlite`. Migrazioni in `migrations.go` con `PRAGMA user_version`: **mai modificare una migrazione rilasciata, aggiungerne una in coda**. Tabelle `categories`, `apps`, `guides`, `alerts`. Date come testo UTC `2006-01-02T15:04:05Z` (confrontabili come stringhe). `apps.icon_kind` e `guides.kind` validati in Go, non con CHECK. Ordinamento manuale via `sort_order` + `moveRow` (rinumera 0..n-1 nell'ambito: categoria per le app, app o "generale" per le guide — `app_id IS ?` gestisce NULL).
- Visibilità in plancia: app `enabled=1 AND url<>''`; guida `enabled=1` e generale oppure di un'app visibile; avviso `starts_at <= now < ends_at` (o senza fine). Plancia = testata, carosello avvisi (`alerts_carousel`, refresh HTMX 5 min), tile pastello (`tint` dell'`icon_color`, scheda `.tile-fly` via CSS hover/focus + badge su touch), widget `widget_oggi`/`widget_calendario`/`widget_guide` (nuovi widget = nuovo template nella colonna `.widgets`). Urgenti: `dialog.urgent` una volta per `alertVersion` (localStorage). CSS: `app.css` = base condivisa con l'admin, `plancia.css` = solo plancia.
- `internal/auth`: porting di GoPulley con differenze volute — StartTLS fallito = login fallito (nessun ripiego in chiaro), username solo `[A-Za-z0-9._@-]`, in mock admin = `ADMIN_USERS` (o tutti se vuoto). `LDAP_HOST` non ha default: mock va scelto esplicitamente (fail-closed) e all'avvio produce un warning. `RateLimiter` in memoria: 5 fallimenti/15 min per username e per IP → blocco 30s raddoppiato fino a 15 min.
- `internal/icons`: catalogo Material Icons da `codepoints.txt` (embedded). Font self-hosted in `web/static/fonts` (Apache 2.0, stesso v145 di UtenzePA).
- `internal/calendar`: logica pura del widget calendario — festività nazionali (Pasqua con algoritmo di Meeus), espansione degli eventi (annuali, 29/02 → 28/02 negli anni non bisestili), griglia del mese da lunedì, prossimi appuntamenti. Le date di calendario sono giorni interi a mezzanotte UTC (`calendar.Day`, `database.DayLayout`).
- `internal/backup`: archivi `tar.gz` (manifest + `VACUUM INTO` del DB + `uploads/`) in `<dir DB_PATH>/backups`, scritti su `.tmp` e rinominati. Scheduler ogni `BACKUP_INTERVAL_HOURS` (0 = off) con retention GFS solo sugli `auto`. Ripristino: `extract` in `restore-tmp` con validazione (percorsi, link, manifest, schema non futuro, `integrity_check`), backup `pre-ripristino`, swap di DB e uploads, poi `exit(0)` → riavvio dalla restart policy. Un'operazione alla volta (`ErrBusy`). Upload a pezzi da 512 KB per stare sotto il limite di 1 MB dei proxy.
- `internal/web`: `Server` con dipendenze esplicite (testabile con `httptest`, vedi `newTestServer` in `server_test.go`). Catena: `securityHeaders` → `http.CrossOriginProtection` (CSRF, nessun token nei form) → `ServeMux`.
- **CSP stretta**: niente `<script>`/`<style>` inline né `on*=`; unica eccezione `style-src-attr 'unsafe-inline'` per il colore delle icone. Nuovo JS → file in `web/static/js`. HTMX configurato via `<meta name="htmx-config">` (`includeIndicatorStyles:false`).
- **Admin**: pagine separate con shell `admin_top`/`admin_bottom` (dati `pageView{adminPage, Body}`). Ogni sezione è un template `<nome>_section` dentro `<div id="section">`; ogni azione HTMX (`hx-post`, `hx-target="#section"`, `hx-swap="outerHTML"`) restituisce l'intera sezione: 200 se ok, **422 con errori** (htmx configurato per fare swap anche sui 422). Route: `GET /admin/<s>`, `GET /admin/<s>/{id}/modifica`, `POST /admin/<s>`, `POST /admin/<s>/{id}`, `POST …/elimina`, `POST …/sposta` (`dir=up|down`).
- Sessione: cookie `cruscotto_admin` cifrato (gorilla/sessions), `Path=/admin`, 8 ore, `Secure` da `X-Forwarded-Proto` o `SECURE_COOKIES`. Senza sessione: 303 al login, oppure 401 + `HX-Redirect` per HTMX. Login su HTTP in chiaro con cookie Secure → 400 con spiegazione (`cookieWouldBeDropped`; localhost escluso): accesso diretto via `http://host:porta` richiede `SECURE_COOKIES=false`.
- **Upload icone**: tipo dal contenuto (PNG/WebP/SVG), max 512 KB, nome casuale in `UPLOAD_DIR/icons`, servite da `/uploads/icons/{file}` con `Content-Security-Policy: sandbox` (uno script in un SVG non gira mai). Il file vecchio si cancella quando l'icona cambia o l'app viene eliminata.
- `web/templates` (parse all'avvio, relativo alla cwd: avviare dalla root del repo), `web/static` (`htmx.min.js` 2.0.4, `dashboard.js`, `admin.js`).

## Debito noto (revisione sotto-progetto 1)

- Rate limiter login: la chiave `ip:` dietro proxy/rootlessport è condivisa → 5 tentativi sbagliati bloccano tutti gli admin. Da rivedere con i proxy fidati (sotto-progetto 4).
- `ADMIN_USERS` confronta lo username intero: `mrossi@dominio` ≠ `mrossi`.
- Icone da URL `http://` accettate ma bloccate da `img-src https:`; icone `upload` senza ripiego sulle iniziali.
- Admin HTMX: nessun messaggio a schermo su 500/404/403. Il refresh avvisi ogni 5 min chiude un dialog aperto.
- `moveRow` senza `_txlock=immediate`: raro `SQLITE_BUSY_SNAPSHOT`. `/static/` elenca le directory. Manca la favicon.
