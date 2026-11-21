# Errori su GlitchTip — piano di implementazione

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** errori del server (`slog.Error`, panic, avvio) e del browser (via `POST /errori`) su GlitchTip, senza dati identificanti.

**Architecture:** nuovo package `internal/errtrack` (client `sentry-go` proprio, non globale; scrub, anti-pioggia, handler `slog` "tee"). `internal/web` riceve il `*errtrack.Tracker` dalle `Options`: middleware di recover, `POST /errori`, `serverError` che conserva il chiamante, `errori.js` incluso se attivo. `cmd/server` inizializza il tracker subito dopo il logger e fa `Flush` prima di ogni uscita.

**Tech Stack:** Go, `github.com/getsentry/sentry-go` (ultima versione stabile), `log/slog`, JS vanilla.

**Spec:** `docs/superpowers/specs/2026-11-21-glitchtip-design.md`

## Global Constraints

- `SENTRY_DSN` vuota = tutto spento; DSN non valida = warning, avvio regolare, spento.
- `SENTRY_ENVIRONMENT` default `production`; `release` = `AppVersion`.
- Nuove env in `docker-compose.yml` (`${VAR}`/`${VAR:-}`), `.env.example`, `internal/config`.
- Nessun dato identificante: niente username/mail/IP/PC/query/header/cookie/corpo. `user.id` = primi 16 hex di HMAC-SHA256(`sha256("errori-utente:"+SESSION_SECRET)`, `strings.ToLower(username)`), nessun altro campo `user`.
- Anti-pioggia unico per tutti i percorsi: chiave = fingerprint + `user.id` (vuoto se anonimo), 1 invio ogni 10 min per chiave, tetto globale 60/min, max 5000 chiavi.
- `/errori` sempre 200 `{"ok":bool}`, corpo ≤ 16 KB. CSP invariata. Nessun DSN vero nel repo.
- Test di `internal/web` nel container (`MSYS_NO_PATHCONV=1 docker run … golang:1.27-alpine go test ./...`); `internal/errtrack` e `internal/config` anche su Windows.

## Review Focus

- GlitchTip irraggiungibile o lento: nessuna richiesta HTTP del Cruscotto deve attendere l'invio → test che `CaptureLog` con trasporto bloccante ritorni subito (trasporto asincrono di sentry-go; nel test, trasporto finto che registra senza bloccare e `Flush`). (Task 1)
- Messaggio d'errore di LDAP con DN e mail dentro `err` (es. `LDAP Result Code 49 "Invalid Credentials": CN=Mario Rossi,OU=…`): deve uscire `<dn>`. → test in `TestScrub`. (Task 1)
- Panic con `http.ErrAbortHandler` (client che chiude a metà SSE/download): nessun evento, comportamento di `net/http` invariato. (Task 3)
- `/errori` chiamato da un'estensione o con `"Script error."`: nessun evento. (Task 4)
- Pagine con `errori.js` spento (DSN vuota): nessun `<script src="/static/js/errori.js">` nell'HTML. (Task 4)

---

### Task 1: package `internal/errtrack` (client, scrub, anti-pioggia)

**Files:**
- Create: `internal/errtrack/errtrack.go`, `internal/errtrack/scrub.go`, `internal/errtrack/throttle.go`
- Test: `internal/errtrack/errtrack_test.go`, `internal/errtrack/scrub_test.go`, `internal/errtrack/throttle_test.go`
- Modify: `go.mod`, `go.sum` (`go get github.com/getsentry/sentry-go@latest`)

**Interfaces:**
- Produces:
  - `type Options struct { DSN, Environment, Release, Secret string; Transport sentry.Transport; Now func() time.Time }`
  - `func New(o Options) (*Tracker, error)` — DSN vuota → `(nil, nil)`; DSN non valida → `(nil, err)`. Tutti i metodi sono sicuri su `*Tracker` nil (no-op, `Enabled()` false).
  - `func (t *Tracker) Enabled() bool`
  - `func (t *Tracker) UserID(username string) string` — `""` se username vuoto.
  - `type Request struct { Method, Path string }` (Path già grezzo: normalizza il tracker)
  - `func (t *Tracker) CaptureMessage(msg, fingerprint string, attrs map[string]string, req *Request, userID string)`
  - `func (t *Tracker) CapturePanic(v any, stack []byte, req Request, userID string)`
  - `type BrowserError struct { Message, Stack, File string; Line, Col int; Page, UserAgent string }`
  - `func (t *Tracker) CaptureBrowser(e BrowserError, userID string)`
  - `func (t *Tracker) Flush(d time.Duration)`
  - `func Scrub(s string) string`, `func NormalizePath(p string) string`, `func PersonalKey(k string) bool`

- [ ] **Step 1: test di scrub e percorsi** (`scrub_test.go`, tabellari)
  - `TestScrub`: `"mario.rossi@example.it"`→`"<mail>"`; `"dial 192.0.2.15:389"`→`"dial <ip>:389"`; `"2001:db8::1"`→`"<ip>"`; `"MOCK\\mrossi"`→`"<utente>"`; `"CN=Mario Rossi,OU=Uffici,DC=example,DC=it: invalid"`→`"<dn>: invalid"`; `"ticket: registro"` invariato; `"template plancia: line 3"` invariato.
  - `TestNormalizePath`: `/ticket/123`→`/ticket/{id}`; `/ticket/123/allegati/4/foto.png`→`/ticket/{id}/allegati/{id}/{file}`; `/uploads/icons/a1b2c3d4e5.png`→`/uploads/icons/{file}`; `/guide/7/pdf`→`/guide/{id}/pdf`; `/admin/media/9f8e7d6c5b4a/pezzo?n=2`→`/admin/media/{id}/pezzo`; `/`→`/`; `/avvisi`→`/avvisi`.
  - `TestPersonalKey`: true per `user username utente mail email pc ip name nome remote` (anche maiuscole), false per `err ticket da`.
- [ ] **Step 2:** `go test ./internal/errtrack/` → FAIL (package inesistente).
- [ ] **Step 3: implementa `scrub.go`.** Regex compilate una volta; ordine: DN (`(?i)\b(CN|OU|DC)=[^:;"]*` fino al primo `:`/fine) → mail → `DOMINIO\utente` (`\b[A-Za-z0-9-]+\\[A-Za-z0-9._-]+`) → IPv4 → IPv6 (almeno due `:` con gruppi hex, non orari tipo `10:00`: richiedere `::` o ≥ 3 gruppi hex). `NormalizePath`: tolta la query; segmenti tutti cifre o ≥ 8 caratteri `[0-9a-fA-F-]` → `{id}`; ultimo segmento con `.` → `{file}`.
- [ ] **Step 4: test anti-pioggia** (`throttle_test.go`, orologio finto): `TestThrottleSameKey` (stessa chiave: true, false, dopo 10m01s true); `TestThrottleDifferentUsers` (`fp|u1`, `fp|u2` entrambi true); `TestThrottleGlobalCap` (61 chiavi diverse nello stesso minuto: la 61ª false; dopo 1 min true); `TestThrottleBounded` (6000 chiavi in 6000 minuti finti → `len(entries) <= 5000`).
- [ ] **Step 5: implementa `throttle.go`**: `type throttle struct{ mu sync.Mutex; seen map[string]time.Time; tokens float64; last time.Time; now func() time.Time }`, `func (t *throttle) allow(key string) bool`. Token bucket 60/min (capacità 60, ricarica 1/s). Su inserimento con `len(seen) >= 5000`: elimina le chiavi scadute; se ancora piene, elimina la più vecchia.
- [ ] **Step 6: test del tracker** (`errtrack_test.go`) con trasporto finto `type fakeTransport struct{ mu; events []*sentry.Event }` che implementa `sentry.Transport` (Configure, SendEvent, Flush, FlushWithContext, Close) registrando gli eventi.
  - `TestNewEmptyDSN`: `New(Options{})` → nil, nil; metodi su nil non vanno in panic, `Enabled()` false.
  - `TestNewInvalidDSN`: `DSN: "non-una-dsn"` → errore.
  - `TestCaptureMessage`: un evento, `Level error`, `Message "ticket: registro"`, `Fingerprint == []string{"ticket: registro|web.(*Server).handleTicket"}` (passato dal chiamante), `Tags["origine"]=="server"`, `Environment`, `Release`; attrs `{"err":"bind CN=X,DC=it: fail","mail":"a@example.it"}` → `Extra["err"]=="bind <dn>: fail"`, nessuna chiave `mail`.
  - `TestUserID`: stabile, 16 hex, `UserID("MRossi")==UserID("mrossi")`, segreti diversi → id diversi, `UserID("")==""`.
  - `TestNoPII`: evento con `req` e `userID`: `User.ID` = id, `User.Username/Email/IPAddress` vuoti; `Request.URL` = percorso normalizzato senza query; `Request.Headers`, `Cookies`, `Data`, `QueryString`, `Env` vuoti; `ServerName == "cruscottopa"`.
  - `TestCapturePanic`: un evento con `Exception[0].Value` scrubbed, stacktrace presente, `Tags["origine"]=="server"`, `Level fatal` o `error`.
  - `TestCaptureBrowser`: `Tags["origine"]=="browser"`, fingerprint = `Message|File|Line`, `Extra["stack"]` scrubbed e ≤ 8 KB, `Tags["pagina"]` normalizzata.
  - `TestThrottleApplied`: stesso messaggio 3 volte stesso utente → 1 evento; utente diverso → 2.
- [ ] **Step 7: implementa `errtrack.go`.** `sentry.NewClient(ClientOptions{Dsn, Environment, Release, ServerName:"cruscottopa", SendDefaultPII:false, Transport, BeforeSend: scrubEvent, Integrations: func(i []sentry.Integration) []sentry.Integration { return nil }})` (nessuna integrazione automatica: niente modules/environment/request) e `sentry.NewHub(client, sentry.NewScope())`. Ogni cattura: calcola la chiave, `throttle.allow`, costruisce l'`*sentry.Event` e chiama `client.CaptureEvent(ev, nil, nil)`. `scrubEvent` riapplica `Scrub` a Message, Exception values, Extra stringa, azzera `User` tranne `ID`, `Request` ridotta a `Method`+`URL` normalizzato. Secret per HMAC: `sha256("errori-utente:"+Secret)`.
- [ ] **Step 8:** `go test ./internal/errtrack/` → PASS; `go vet ./...` pulito.
- [ ] **Step 9: commit** `feat(errtrack): client GlitchTip con scrub e anti-pioggia`.

### Task 2: handler `slog`, configurazione e avvio

**Files:**
- Create: `internal/errtrack/slog.go`, test `internal/errtrack/slog_test.go`
- Modify: `internal/config/config.go` (+test), `cmd/server/main.go`, `docker-compose.yml`, `.env.example`

**Interfaces:**
- Consumes: Task 1.
- Produces:
  - `func (t *Tracker) Handler(next slog.Handler) slog.Handler` (nil tracker → `next`)
  - `const SkipKey = "errtrack.skip"` — un record con `slog.Bool(SkipKey, true)` non diventa evento (l'attributo non compare nel log testuale... accettabile anche se compare).
  - `config.Config.SentryDSN`, `config.Config.SentryEnvironment string`

- [ ] **Step 1: test** `slog_test.go`: `TestHandlerError` (record Error → un evento, fingerprint = `msg + "|" + funzione del PC del record`, es. contiene `errtrack.TestHandlerError`; il testo va comunque al `next`); `TestHandlerWarn` (Warn → nessun evento); `TestHandlerSkip` (Error con `SkipKey` → nessun evento); `TestHandlerWithAttrs` (`logger.With("err", x).Error(...)` → attrs di `With` presenti in Extra, `WithGroup` prefissa le chiavi con `gruppo.`); `TestHandlerPCZero` (record senza PC → fingerprint = solo messaggio).
- [ ] **Step 2:** FAIL. **Step 3:** implementa `teeHandler{next slog.Handler; t *Tracker; attrs []slog.Attr; group string}`: `Enabled` delega a `next` (ma Error sempre abilitato), `Handle` chiama `next.Handle` e, se `r.Level >= slog.LevelError` e nessun `SkipKey`, raccoglie attrs (`a.Value.String()`, chiavi personali scartate da `CaptureMessage`) e chiama `CaptureMessage(r.Message, fp, attrs, nil, "")`. Funzione da `runtime.CallersFrames([]uintptr{r.PC}).Next().Function`. **Step 4:** PASS.
- [ ] **Step 5: config.** Test in `config_test.go`: `SENTRY_ENVIRONMENT` assente → `"production"`; `SENTRY_DSN` letta con `TrimSpace`. Implementa in `Load()`. Aggiungi a `docker-compose.yml` `- SENTRY_DSN=${SENTRY_DSN:-}` e `- SENTRY_ENVIRONMENT=${SENTRY_ENVIRONMENT:-production}`; a `.env.example` le due righe commentate in italiano (DSN del progetto su GlitchTip, vuota = spento; ambiente, es. `sviluppo`).
- [ ] **Step 6: `cmd/server/main.go`.** Dopo `config.Load()` e prima di creare il logger: `tracker, terr := errtrack.New(errtrack.Options{DSN: cfg.SentryDSN, Environment: cfg.SentryEnvironment, Release: AppVersion, Secret: cfg.SessionSecret})`; logger = `slog.New(tracker.Handler(slog.NewTextHandler(...)))`; se `terr != nil` → `slog.Warn("SENTRY_DSN non valida: errori non inviati a GlitchTip", "err", terr)`. Helper `fatal := func(msg string, err error) { slog.Error(msg, "err", err); tracker.Flush(2*time.Second); os.Exit(1) }` al posto delle coppie `slog.Error`+`os.Exit(1)` (database, UPLOAD_DIR, backup, web). Passa `Errors: tracker` a `web.Options`. Dopo `httpSrv.Shutdown`: `tracker.Flush(2*time.Second)`. Se `cfg.SessionSecret` è vuoto in mock, il segreto casuale va comunque bene (usa quello effettivo che `config` ha generato: verificare che `cfg.SessionSecret` sia già valorizzato dopo `Load`, altrimenti usare il campo dove `config` lo mette).
- [ ] **Step 7:** `go build ./... && go vet ./... && go test ./internal/errtrack/ ./internal/config/` → PASS. `LDAP_HOST=mock SENTRY_DSN=x go run ./cmd/server` → log con warning e server avviato (fermarlo).
- [ ] **Step 8: commit** `feat: errori di slog e dell'avvio su GlitchTip`.

### Task 3: `internal/web` — panic e `serverError`

**Files:**
- Modify: `internal/web/server.go` (Options `Errors *errtrack.Tracker`, campo `errs`, `Handler()`), `internal/web/middleware.go` (`recoverPanic`), `internal/web/render.go` (`serverError`)
- Test: `internal/web/errtrack_test.go` (nuovo); helper per un tracker con trasporto finto: esporre in `errtrack` `func NewForTest(t testing.TB) (*Tracker, func() []*sentry.Event)` in un file **non** `_test.go` (`errtrack/testing.go`) così `internal/web` può usarlo.

**Interfaces:**
- Consumes: Task 1 (`CapturePanic`, `UserID`, `Request`), Task 2 (`SkipKey`, `Handler`).
- Produces: `web.Options.Errors`; `(*Server).reportUser(r *http.Request) string` = `UserID` dello username del cookie `cruscotto_utente` se riconosciuto, altrimenti dello username della sessione admin se il percorso è `/admin…`, altrimenti `""`.

- [ ] **Step 1: test.** `TestPanicReported`: server di prova con tracker finto, route di prova registrata su `s.mux` (`GET /panico-test` che fa `panic("boom mario@example.it")`); `do(...)` → status 500, un evento con valore `boom <mail>`, URL `/panico-test`. `TestPanicAbortHandler`: `panic(http.ErrAbortHandler)` → nessun evento e il panic si propaga (recuperarlo nel test con `defer recover`). `TestServerErrorFingerprint`: due handler di prova diversi chiamano `s.serverError` → due eventi con fingerprint diversi che contengono il nome della funzione chiamante, non `serverError`. (Il logger di default va sostituito nel test con `tracker.Handler(...)` e ripristinato.)
- [ ] **Step 2:** FAIL (nel container). **Step 3:** implementa. `recoverPanic(next) http.Handler` come metodo di `Server`: `defer func(){ if v := recover(); v != nil { if v == http.ErrAbortHandler { panic(v) }; stack := debug.Stack(); s.errs.CapturePanic(v, stack, errtrack.Request{r.Method, r.URL.Path}, s.reportUser(r)); slog.Error("panic", "valore", fmt.Sprint(v), "stack", string(stack), errtrack.SkipKey, true); http.Error(w, "Si è verificato un errore. Riprova più tardi.", 500) } }()`. `Handler()` = `securityHeaders(s.recoverPanic(http.NewCrossOriginProtection().Handler(s.mux)))`. `serverError`: `var pcs [1]uintptr; runtime.Callers(2, pcs[:]); rec := slog.NewRecord(time.Now(), slog.LevelError, "errore interno", pcs[0]); rec.AddAttrs(slog.Any("err", err)); _ = slog.Default().Handler().Handle(context.Background(), rec)` poi la risposta come oggi. **Step 4:** PASS (container).
- [ ] **Step 5: commit** `feat(web): panic ed errori interni su GlitchTip`.

### Task 4: `POST /errori` ed `errori.js`

**Files:**
- Create: `internal/web/errori.go`, `web/static/js/errori.js`, test `internal/web/errori_test.go`
- Modify: `internal/web/server.go` (route, funzione di template `errori`), `web/templates/dashboard.html` (`plancia_head`), `web/templates/admin_base.html`

**Interfaces:**
- Consumes: Task 1 (`CaptureBrowser`, `BrowserError`), Task 3 (`reportUser`).
- Produces: route `POST /errori`; funzione di template `errori` → `bool` (`s.errs.Enabled()`).

- [ ] **Step 1: test** (`errori_test.go`), JSON `{"message","stack","file","line","col","page","ua"}`:
  - `TestErroriReported`: messaggio valido, `file` = `/static/js/dashboard.js` → 200 `{"ok":true}`, un evento `origine=browser`; cookie `cruscotto_utente` di `mrossi` → `User.ID == tracker.UserID("mrossi")`.
  - `TestErroriAlways200`: JSON rotto, corpo 17 KB, `message` vuoto → 200 `{"ok":false}`, nessun evento.
  - `TestErroriFiltered`: `"Script error."` senza file; `file` `chrome-extension://abc/x.js`; stack con `moz-extension://` → 200, nessun evento.
  - `TestErroriDisabled`: server senza tracker → 200 `{"ok":true}`, nessun panic.
  - `TestErroriCrossOrigin`: header `Sec-Fetch-Site: cross-site` → rifiutata da `CrossOriginProtection` (comportamento attuale delle POST pubbliche; verificare cosa risponde e fissarlo).
  - `TestErroriScriptIncluded`: plancia e `/admin` (login) con tracker → `<script src="/static/js/errori.js">` presente **prima** di `htmx.min.js`; senza tracker → assente.
  - `TestErroriJS`: legge `web/static/js/errori.js` e verifica la presenza di `unhandledrejection`, `sendBeacon`, `chrome-extension://`, `moz-extension://`, `Script error.`, il limite `10`.
- [ ] **Step 2:** FAIL. **Step 3: `errori.go`** — `handleErrori`: `http.MaxBytesReader(w, r.Body, 16<<10)`, decode, scarti come da test (file deve iniziare con `location.origin` del sito o con `/`; accettare URL assoluti dello stesso host di `r.Host`), troncamenti (message 500, stack 8 KB, ua 300), `s.errs.CaptureBrowser(...)`, risposta JSON 200. Registra `POST /errori` in `server.go` e la funzione di template `errori`.
- [ ] **Step 4: `errori.js`** — IIFE, tutto in `try/catch`; listener `error` (usa `e.message`, `e.filename`, `e.lineno`, `e.colno`, `e.error && e.error.stack`) e `unhandledrejection` (`e.reason` → messaggio e stack); firma `message|file|line` in un `Set`, max 10 invii per pagina; payload con `page: location.pathname`, `ua: navigator.userAgent.slice(0,300)`; `navigator.sendBeacon("/errori", new Blob([json], {type:"application/json"}))`, fallback `fetch("/errori", {method:"POST", body: json, headers:{"Content-Type":"application/json"}, keepalive:true, credentials:"same-origin"}).catch(()=>{})`. Scarti lato browser come lato server.
- [ ] **Step 5: template** — `{{if errori}}<script src="/static/js/errori.js"></script>{{end}}` (senza `defer`, prima di `htmx.min.js`) in `plancia_head` e `admin_top`.
- [ ] **Step 6:** `node -e "new Function(require('fs').readFileSync('web/static/js/errori.js','utf8'))"` ok; test nel container → PASS.
- [ ] **Step 7: commit** `feat(web): errori del browser su GlitchTip via /errori`.

### Task 5: documentazione, verifica e rilascio

**Files:** `CLAUDE.md`, `publiccode.yml`

- [ ] **Step 1:** `CLAUDE.md`: voce `internal/errtrack` in Architettura (cosa va su GlitchTip, scrub, anti-pioggia comune, `serverError` col chiamante, `SkipKey`, `/errori` sempre 200, `errori.js` solo con DSN), `/errori` nelle route pubbliche, comandi: `SENTRY_DSN` vuota in sviluppo.
- [ ] **Step 2:** test completi nel container `go test ./...` → `ok` per tutti; `go vet ./...`; `docker build --build-arg VERSION=x -t cp:check .` ok.
- [ ] **Step 3:** prova reale: server locale con `SENTRY_DSN` di un progetto di test (se l'utente la fornisce) altrimenti con un ricevitore finto (`httptest`-like: piccolo server Go che stampa le richieste a `/api/<id>/envelope/`, DSN `http://chiave@host.docker.internal:<porta>/1`): provocare un errore JS da Playwright (`page.evaluate(() => setTimeout(() => { throw new Error("prova mario@example.it") }))`) e verificare nel payload `<mail>`, nessun IP, `user.id` hash.
- [ ] **Step 4:** `publiccode.yml` `softwareVersion: 0.20.0`, `releaseDate: 2026-11-21`; commit; push; PR (spec + piano + codice); `gh pr checks --watch`; merge squash; tag `v0.20.0`; `gh run watch` del release.
