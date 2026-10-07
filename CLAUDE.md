# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Progetto

**CruscottoPA** (module `github.com/Comune-di-Montesilvano/CruscottoPA`): portale Intranet / homepage dei dipendenti del Comune di Montesilvano. "Plancia": striscia avvisi, card degli applicativi per categoria con guide agganciate, guide generali; pannello `/admin` HTMX (login LDAP) per gestirli. CI/CD ereditata da GoPulley. Lavoro diviso in sotto-progetti (vedi `docs/superpowers/specs/`): 1 plancia+admin (fatto), backup e ripristino (fatto), 2 guide ricche (Markdown/GitHub/PDF), 3 PWA + notifiche (fatto), 4 riconoscimento utente dal reverse proxy e filtri per ufficio. Futuro (non ancora progettato): modulo di invio ticket di assistenza — da decidere se integrarlo con un gestionale esterno o realizzarlo interno. Probabile: avvisi pubblicati dagli uffici (es. Ufficio Stipendi per i cedolini) con redattori per ufficio — gli avvisi hanno già il campo `source` (fonte).

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
- Dati di prova via `curl` da Git Bash: accenti corrotti (codifica del terminale, non dell'app). Inserirli dall'admin o con `--data-urlencode @file` UTF-8.

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

- **Il reverse proxy (nginx `revprx01`) sostituisce le risposte 4xx/5xx** con una pagina di cortesia e **toglie gli header** (`HX-Redirect`, `Set-Cookie`; resta solo `WWW-Authenticate`). Quindi: niente 4xx per flussi che l'utente deve vedere — login admin con errori in pagina e **200**, sessione scaduta HTMX con **200 + `HX-Redirect`**, stato del riconoscimento ricordato nel browser (`localStorage`), errori HTMX mostrati con un messaggio generico (`admin.js`, mai il corpo della risposta). Le validazioni admin usano ancora 422: da verificare che il proxy non lo intercetti. Gli endpoint `/push/*` rispondono sempre **200** con `{"ok":true|false}`.

- **Nome file `docker-compose.yml`**, non `compose.yml`: Portainer git-stack lo richiede.
- **Nessun `env_file`**: il clone Portainer non ha `.env` → il pull fallirebbe. Ogni variabile va elencata in `environment:` come `${VAR}`. Una nuova env var va aggiunta in tre posti: `docker-compose.yml`, `.env.example`, codice.
- **`/data` = volume nominato `cruscottopa-data`, mai bind mount**: Portainer clona in un path per-commit che non possiede ("permission denied"); in rootless un bind mount richiederebbe anche `:Z`/`:U` e uid mapping.
- **`pull_policy: always`** insieme a `build:`: senza, il redeploy Portainer può riusare una build locale e mostrare `dev` in prod.
- **Container non root (uid 1001), porte > 1024**: Podman rootless non apre porte privilegiate. Niente socket Docker montato (rootless non lo espone).
- **Healthcheck senza wget/curl**: `HEALTHCHECK` esegue `/app/cruscottopa -healthcheck` (GET `/health` su 127.0.0.1).
- **Mai `docker cp` sul DB live**: file root-owned + `-wal`/`-shm` disallineati → "attempt to write a readonly database". Usare `VACUUM INTO` per snapshot.
- Rete: rete di default del progetto compose; nessuna rete esterna dichiarata (una rete `external` inesistente fa fallire lo stack).
- **Ripristino = uscita del processo**: dopo lo swap il container deve ripartire da solo (`restart: unless-stopped`); senza restart policy resta fermo con i dati già ripristinati.

## Identificazione utente — NTLM, non IP

Risultati della sonda in produzione (2026-10-06, spec `2026-10-06-riconoscimento-utente-design.md`):
- Davanti al container c'è nginx (`revprx01`, il nome del sito è un CNAME); il container vede sempre `RemoteAddr` = `10.89.11.8` (rootlessport di Podman): **l'IP non identifica nessuno** e `X-Forwarded-For` non si distingue da una chiamata diretta alla porta. Niente proxy fidati.
- Edge sui PC del dominio invia **NTLM** in automatico (sito già in zona intranet): da lì si legge `DOMINIO\utente`. **Kerberos no** (manca l'SPN, e il CNAME porta a `HTTP/revprx01…`).
- Il nome NTLM **non è verificato** (servirebbe NETLOGON verso il DC): identità **dichiarata**, solo per personalizzare, **mai per autorizzare**. L'admin resta con login LDAP.
- Filtri sui contenuti per gruppi della plancia (attributi AD, gruppi AD, utenti, esclusioni): implementati, vedi `internal/audience` e spec `2026-10-06-filtri-contenuti-design.md`.

## Architettura

- `cmd/server/main.go`: solo wiring — `config.Load()`, `database.Open`, `web.New`, graceful shutdown, flag `-healthcheck` (legge solo `PORT`, non valida il resto della config).
- `internal/config`: env → `Config`; validazione all'avvio (`SESSION_SECRET` ≥ 32 caratteri obbligatorio se `LDAP_HOST` ≠ `mock`; in mock, se assente, segreto casuale → le sessioni non sopravvivono al riavvio).
- `internal/database`: SQL raw su `modernc.org/sqlite`. Migrazioni in `migrations.go` con `PRAGMA user_version`: **mai modificare una migrazione rilasciata, aggiungerne una in coda**. Tabelle `categories`, `apps`, `guides`, `alerts` (con `source`), `calendar_events` (v3), `branding` (v4, riga singola `id = 1`), `audience_attributes`, `audience_groups`, `audience_rules`, `content_audience` (v5), `alerts.notified_at`, `push_subscriptions`, `vapid_keys` (v6; la migrazione marca come notificati gli avvisi già iniziati). Date come testo UTC `2006-01-02T15:04:05Z` (confrontabili come stringhe). `apps.icon_kind` e `guides.kind` validati in Go, non con CHECK. Ordinamento manuale via `sort_order` + `moveRow` (rinumera 0..n-1 nell'ambito: categoria per le app, app o "generale" per le guide — `app_id IS ?` gestisce NULL).
- Visibilità in plancia: app `enabled=1 AND url<>''`; guida `enabled=1` e generale oppure di un'app visibile; avviso `starts_at <= now < ends_at` (o senza fine). Plancia = testata, carosello avvisi (`alerts_carousel`, refresh HTMX 5 min), tile pastello (`tint` dell'`icon_color`, scheda `.tile-fly` via CSS hover/focus + badge su touch), widget `widget_oggi`/`widget_calendario`/`widget_guide` (nuovi widget = nuovo template nella colonna `.widgets`). Urgenti: `dialog.urgent` una volta per `alertVersion` (localStorage). CSS: `app.css` = base condivisa con l'admin, `plancia.css` = solo plancia.
- `internal/identity`: riconoscimento in plancia. `ntlm.go` (sfida e lettura del tipo 3, nessuna verifica; fuzz test), `user.go` (cookie `cruscotto_utente` firmato con chiavi derivate da `SESSION_SECRET`, 30 giorni / 24 ore se anonimo), `directory.go` (nome da AD con l'account di servizio via `auth.Dial`; mock). Flusso: senza cookie la plancia porta `data-riconosci`, `dashboard.js` chiama `/io` (handshake NTLM, poi cookie e una ricarica). Attivo solo con `NTLM_DOMAIN` e una directory (`LDAP_BIND_DN` o mock). `identity/ntlmtest` costruisce messaggi per i test.
- `internal/audience`: logica pura dei **gruppi della plancia**. `Member` (almeno una regola attr/adgroup/user e nessuna exclude; gruppo senza regole positive = nessun membro), `Visible` (pubblico sempre; utente non noto = solo pubblici; `only`/`hide` sui gruppi). Filtro di **presentazione, non protezione** (identità dichiarata + "Mostra tutto").
- Gruppi e visibilità: pagina `/admin/gruppi` (attributi AD utilizzabili, gruppi, regole con suggerimenti da `/admin/ad/*`, anteprima membri); gruppo o attributo in uso non eliminabili (`ErrInUse`); fieldset "Visibilità" nei form di app, guide, avvisi (`content_audience`, nessuna riga = pubblico; righe cancellate in `Delete*`). Profilo utente da AD (`Directory.Profile`: attributi configurati + gruppi annidati via `member:1.2.840.113556.1.4.1941:`) in cache nel server 15 min, 1 min se AD giù (`profileCache`, svuotata quando cambiano gli attributi). Plancia, `/avvisi`, `/partials/alerts` filtrati lato server (`contentFilter`); "Mostra tutto" = cookie `cruscotto_tutto=1` impostato da `dashboard.js`; popup urgenti solo per avvisi destinati all'utente (`Alert.NotForViewer`).
- `internal/notify`: notifiche degli avvisi con "Invia notifica" (`alerts.notify`; `admin.js` la spunta per gli urgenti finché l'admin non la tocca). `Dispatcher` ogni 30 s (`StartNotifications`): avvisi `notify`, attivi, con `notified_at` NULL → `MarkNotified` **prima** dell'invio (`UPDATE … WHERE notified_at IS NULL`: una sola notifica, mai ripetuta, anche dopo una modifica), poi `Hub` SSE e `WebPusher`, solo a chi può vedere l'avviso (`alertVisibleTo`: anonimo/AD giù → solo pubblici). `WebPusher` usa `webpush-go`: **la libreria antepone `mailto:` da sola** al subject, quindi si passa senza prefisso; 404/410 → iscrizione cancellata, altri errori solo log. Chiavi VAPID generate al primo avvio in `vapid_keys` (finiscono nel backup). `VAPID_SUBJECT` vuota = Web Push spento (SSE attivo).
- PWA e notifiche lato browser: `GET /eventi` (SSE, `X-Accel-Buffering: no`, heartbeat 25 s per nginx, max 2000 connessioni; `Server.Close()` prima di `Shutdown` chiude i flussi), `GET /push/chiave`, `POST /push/iscrizioni`, `POST /push/iscrizioni/rimuovi` (solo endpoint `https://` ≤ 1024), `GET /sw.js` (scope `/`, nessuna cache offline), `GET /manifest.webmanifest` (icone `icon-192/512.png` da `logo.svg`), `POST /admin/notifiche/prova`. `notifiche.js`: popup di primo accesso (`dialog.notify-ask`, "Non ora" per 30 giorni), iscrizione automatica se il permesso è già concesso (policy Edge `NotificationsAllowedForUrls` per pre-autorizzare il sito sui PC del dominio), link nel footer con disattivazione ricordata in `localStorage`, evento SSE → refresh del carosello e notifica se la scheda non è visibile (stesso `tag` del push: niente doppioni). **Una sola connessione `/eventi` per browser** (il proxy parla HTTP/1.1, max 6 connessioni per sito): la scheda che ottiene il Web Lock `cruscotto-eventi` tiene il flusso e inoltra gli eventi alle altre con `BroadcastChannel`. Se le chiavi VAPID cambiano (es. ripristino), il browser rifà l'iscrizione. Push solo verso gli host dei servizi push (`notify.AllowedEndpoint`, niente redirect), max 20000 iscrizioni, invii in parallelo (8, timeout 5 s) dopo gli eventi SSE; avviso riservato con AD giù → rimandato fino a 10 minuti.
- `internal/auth`: porting di GoPulley con differenze volute — StartTLS fallito = login fallito (nessun ripiego in chiaro), username solo `[A-Za-z0-9._@-]`, in mock admin = `ADMIN_USERS` (o tutti se vuoto). `LDAP_HOST` non ha default: mock va scelto esplicitamente (fail-closed) e all'avvio produce un warning. `RateLimiter` in memoria: 5 fallimenti/15 min per username (niente chiave per IP: dietro rootlessport è uguale per tutti) → blocco 30s raddoppiato fino a 15 min.
- `internal/icons`: catalogo Material Icons da `codepoints.txt` (embedded). Font self-hosted in `web/static/fonts` (Apache 2.0, stesso v145 di UtenzePA).
- `internal/calendar`: logica pura del widget calendario — festività nazionali (Pasqua con algoritmo di Meeus), espansione degli eventi (annuali, 29/02 → 28/02 negli anni non bisestili), griglia del mese da lunedì, prossimi appuntamenti. Le date di calendario sono giorni interi a mezzanotte UTC (`calendar.Day`, `database.DayLayout`).
- `internal/backup`: archivi `tar.gz` (manifest + `VACUUM INTO` del DB + `uploads/`) in `<dir DB_PATH>/backups`, scritti su `.tmp` e rinominati. Scheduler ogni `BACKUP_INTERVAL_HOURS` (0 = off) con retention GFS solo sugli `auto`. Ripristino: `extract` in `restore-tmp` con validazione (percorsi, link, manifest, schema non futuro, `integrity_check`), backup `pre-ripristino`, swap di DB e uploads, poi `exit(0)` → riavvio dalla restart policy. Un'operazione alla volta (`ErrBusy`). Upload a pezzi da 512 KB per stare sotto il limite di 1 MB dei proxy.
- `internal/web`: `Server` con dipendenze esplicite (testabile con `httptest`, vedi `newTestServer` in `server_test.go`). Catena: `securityHeaders` → `http.CrossOriginProtection` (CSRF, nessun token nei form) → `ServeMux`.
- **CSP stretta**: niente `<script>`/`<style>` inline né `on*=`; unica eccezione `style-src-attr 'unsafe-inline'` per il colore delle icone. Nuovo JS → file in `web/static/js`. HTMX configurato via `<meta name="htmx-config">` (`includeIndicatorStyles:false`).
- **Admin**: pagine separate con shell `admin_top`/`admin_bottom` (dati `pageView{adminPage, Body}`). Ogni sezione è un template `<nome>_section` dentro `<div id="section">`; ogni azione HTMX (`hx-post`, `hx-target="#section"`, `hx-swap="outerHTML"`) restituisce l'intera sezione: 200 se ok, **422 con errori** (htmx configurato per fare swap anche sui 422). Route: `GET /admin/<s>`, `GET /admin/<s>/{id}/modifica`, `POST /admin/<s>`, `POST /admin/<s>/{id}`, `POST …/elimina`, `POST …/sposta` (`dir=up|down`).
- Sessione: cookie `cruscotto_admin` cifrato (gorilla/sessions), `Path=/admin`, 8 ore, `Secure` da `X-Forwarded-Proto` o `SECURE_COOKIES`. Senza sessione: 303 al login, oppure 401 + `HX-Redirect` per HTMX. Login su HTTP in chiaro con cookie Secure → 400 con spiegazione (`cookieWouldBeDropped`; localhost escluso): accesso diretto via `http://host:porta` richiede `SECURE_COOKIES=false`.
- **Upload icone**: tipo dal contenuto (PNG/WebP/SVG), max 512 KB, nome casuale in `UPLOAD_DIR/icons`, servite da `/uploads/icons/{file}` con `Content-Security-Policy: sandbox` (uno script in un SVG non gira mai). Il file vecchio si cancella quando l'icona cambia o l'app viene eliminata. Codice generico per directory: `saveUpload`/`removeUpload`/`handleUploadFile` (`icons`, `branding`).
- **Branding**: nome e logo dell'ente in `branding`, in cache nel `Server` (`atomic.Pointer`, funzione di template `ente`; template `favicon`, `title_ente`, `ente_logo` in `partials_brand.html`), modificabili da `/admin/ente`. Logo in `UPLOAD_DIR/branding`, servito da `/uploads/branding/{file}` in sandbox. Nessun ente scritto nei template (lo verifica un test). Logo e favicon di CruscottoPA fissi in `web/static/img/`: `logo.svg` (con riquadro), `logo-on-dark.svg` (testata, rail admin), `logo-on-light.svg` (login, footer), `logo-16.svg`, `favicon.ico` (servita anche da `/favicon.ico`). Rigenerare l'ICO: Chrome headless a sfondo trasparente su `logo-16.svg` a 16 px e `logo.svg` a 24–256 px, poi Pillow.
- Route pubbliche: `/`, `/io`, `/eventi`, `/push/*`, `/sw.js`, `/manifest.webmanifest`, `/avvisi`, `/partials/alerts`, `/partials/calendario`, `/health`, `/uploads/icons/{file}`, `/uploads/branding/{file}`, `/favicon.ico`. `/static/` passa da `revalidate` (`Cache-Control: no-cache`): non toglierlo, altrimenti i browser tengono il JS vecchio dopo un aggiornamento.
- `web/templates` (parse all'avvio, relativo alla cwd: avviare dalla root del repo), `web/static` (`htmx.min.js` 2.0.4, `dashboard.js`, `notifiche.js`, `admin.js`, `sw.js`, `manifest.webmanifest`).

## Debito noto (revisione sotto-progetto 1)

- Icone `upload` senza ripiego sulle iniziali.
- Il refresh avvisi ogni 5 min chiude un dialog aperto e non aggiorna il contatore di "Mostra tutto".
- `moveRow` senza `_txlock=immediate`: raro `SQLITE_BUSY_SNAPSHOT`.
- Riconoscimento: accessi con UPN (dominio vuoto, `utente@dominio`) restano anonimi. Salvataggi concorrenti in `/admin/ente` possono disallinearsi.
