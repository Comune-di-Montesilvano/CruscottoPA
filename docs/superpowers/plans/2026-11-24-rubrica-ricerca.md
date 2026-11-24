# Contatti della Rubrica nella ricerca a tendina — piano di implementazione

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** gruppo «Persone» nella ricerca a tendina della plancia, con nome, reparto, interno e mail dalla Rubrica e link alla scheda.

**Architecture:** la Rubrica espone `GET /api/cerca`; il Cruscotto la interroga dal server (`internal/rubrica`, cache 2 min) dietro `GET /rubrica/cerca` (sempre 200 + JSON); `dashboard.js` chiama quella route con debounce e aggiunge il gruppo.

**Tech Stack:** Go (gorilla/mux + `mattn/go-sqlite3` nella Rubrica; stdlib + `modernc.org/sqlite` nel Cruscotto), JS vanilla.

**Spec:** `docs/superpowers/specs/2026-11-24-rubrica-ricerca-design.md`

## Global Constraints

- Due repo: Rubrica in `C:\Users\mirko.daddiego\Documents\rubrica` (branch nuovo `feat/api-cerca`, test in container `golang:1.25-alpine` con gcc come da suo CLAUDE.md), Cruscotto qui (branch `feat/rubrica-ricerca`, test `internal/web` in container).
- Rubrica: `q` < 2 caratteri → `[]`; al massimo **8** risultati; campi JSON `uid`, `nome`, `reparto`, `interno`, `mail`; `interno` = `LDAPExt` con `;` → `", "` (come `extList`), altrimenti `PrimaryNumber`.
- Cruscotto: env `RUBRICA_URL` (vuota = spenta, solo `http`/`https`, niente `/` finale) in `docker-compose.yml`, `.env.example`, `internal/config`.
- `GET /rubrica/cerca` sempre **200**: `{"ok":true,"persone":[…]}` oppure `{"ok":false,"persone":[]}`; ogni voce ha `url` = `RUBRICA_URL + "/contacts/" + url.PathEscape(uid)`.
- Client: timeout **2 s**, nessun redirect, corpo max **256 KB**, max 8 voci; cache 2 minuti, max 500 chiavi, chiave = testo minuscolo con spazi compattati.
- Mai il testo cercato nei log; Rubrica giù → `slog.Warn("rubrica non raggiungibile", "err", …)` al massimo una volta al minuto, mai `slog.Error`.
- JS: dopo 2 caratteri e **250 ms**; `AbortController`; risposta di un testo non più attuale scartata; gruppo «Persone» dopo i risultati locali e prima di «Web»; icona `person`; nuova scheda.
- CSP stretta (niente inline), colori solo nei token.

## Review Focus

1. Rubrica che risponde lentamente mentre l'utente continua a scrivere: compaiono solo i risultati dell'ultimo testo, mai quelli di un testo precedente. Test in Task 3 (controllo del codice: confronto col testo attuale) e prova Playwright con Rubrica finta lenta.
2. Utente che naviga con le frecce mentre arrivano le persone: la voce attiva non salta a zero. Task 3: `render` conserva l'indice attivo se ancora valido; prova Playwright.
3. Nome con apostrofo o accenti (`D'Angelo`, `Niccolò`): la query arriva intatta alla Rubrica e il nome si vede giusto. Test in Task 2 (`TestRubricaQueryEncoding`).
4. Rubrica che risponde con HTML (pagina di cortesia del proxy) o 302 al login: nessun gruppo, nessun errore in pagina. Test in Task 2 (JSON rotto, redirect).
5. Uid con `/` o spazi: il link alla scheda resta dentro `/contacts/`. Test in Task 2.

---

### Task 1: Rubrica — `GET /api/cerca`

**Files (repo Rubrica):**
- Create: `cmd/server/api_search.go`
- Modify: `cmd/server/main.go` (route accanto a `/search`), `VERSION` (0.13.0), CLAUDE.md (riga sull'endpoint)
- Test: `cmd/server/api_search_test.go`

**Interfaces:**
- Produces: `GET /api/cerca?q=` → `200 application/json`, array di `apiContact{UID string "uid"; Nome string "nome"; Reparto string "reparto"; Interno string "interno"; Mail string "mail"}`; errore DB → 500 `{"errore":"ricerca non riuscita"}`.

- [ ] **Step 1: test che falliscono** (`setupHandlerEnv` + `pbService = phonebook.NewService(db)`; contatti creati con `db.UpsertContact`):
  - `TestAPISearchShortQuery`: `q=a` → 200, corpo `[]`.
  - `TestAPISearchFields`: contatto `Mario Rossi`, `LDAPExt "123;124"`, mail `mrossi@example.it`, reparto `Tributi`; altro con solo `PrimaryNumber "0850000000"`; `q=rossi` → una voce con `interno "123, 124"`; `q=` del secondo → `interno "0850000000"`; `Content-Type` = `application/json`.
  - `TestAPISearchLimit`: 10 contatti `Prova N` → 8 voci.
  - `TestAPISearchDisabled`: contatto con `Disabled: true` non compare.
- [ ] **Step 2:** test in container → FAIL.
- [ ] **Step 3:** `handleAPISearch` in `api_search.go`, route `r.HandleFunc("/api/cerca", handleAPISearch).Methods("GET")`. Interno con la stessa sostituzione di `extList` (funzione condivisa `formatExt(s string) string` usata anche dalla FuncMap).
- [ ] **Step 4:** test in container → PASS; `go vet ./...`.
- [ ] **Step 5:** commit, PR, merge, tag `v0.13.0`, release (procedura del repo Rubrica).

### Task 2: Cruscotto — config, client e route `/rubrica/cerca`

**Files:**
- Create: `internal/rubrica/client.go`, `internal/rubrica/client_test.go`, `internal/web/rubrica.go`, `internal/web/rubrica_test.go`
- Modify: `internal/config/config.go` (+ test), `docker-compose.yml`, `.env.example`, `internal/web/server.go` (campo, `Options.Rubrica`, route), `cmd/server/main.go` (wiring)

**Interfaces:**
- Consumes: API del Task 1.
- Produces:
  ```go
  // internal/config
  Config.RubricaURL string // validata: "" oppure http(s)://host[...], senza "/" finale
  // internal/rubrica
  type Person struct{ UID, Nome, Reparto, Interno, Mail, URL string } // tag json: uid,nome,reparto,interno,mail,url
  type Client struct{ /* base, http, cache */ }
  func New(base string) *Client                // nil se base == ""
  func (c *Client) Search(ctx context.Context, q string) ([]Person, error)
  // internal/web
  Options.Rubrica *rubrica.Client
  // GET /rubrica/cerca?q=
  ```

- [ ] **Step 1: test che falliscono**:
  - `internal/config`: `RUBRICA_URL=https://rubrica.example.it/` → `RubricaURL` senza `/`; `ftp://x` e `rubrica.example.it` → errore di `Load`; vuota → `""`.
  - `internal/rubrica` con `httptest.Server`: `TestRubricaSearchOK` (campi e `URL` = base + `/contacts/` + uid); `TestRubricaQueryEncoding` (`D'Angelo Niccolò` arriva identico nel `q` letto dal server finto); `TestRubricaUIDEscaped` (uid `a/b c` → URL `/contacts/a%2Fb%20c`); `TestRubricaErrors` (500, JSON rotto/HTML, 302 verso `/login` non seguito, corpo > 256 KB, risposta dopo 3 s → errore entro ~2 s); `TestRubricaMaxResults` (12 voci → 8); `TestRubricaCache` (stessa query con maiuscole e spazi diversi → una sola richiesta al server; dopo 2 minuti di orologio finto → nuova richiesta); `TestRubricaShortQuery` (1 carattere → nessuna richiesta, lista vuota).
  - `internal/web`: `TestRubricaRouteOK` (server finto, `{"ok":true,"persone":[…]}`); `TestRubricaRouteDown` (server finto 500 → 200 `{"ok":false,"persone":[]}`); `TestRubricaRouteOff` (senza `RUBRICA_URL` → `{"ok":false,"persone":[]}`); `TestRubricaDataAttr` (con `RUBRICA_URL` la plancia ha `data-rubrica` sull'input di ricerca, senza no).
- [ ] **Step 2:** test → FAIL (`go test ./internal/config/ ./internal/rubrica/` in locale, `internal/web` in container).
- [ ] **Step 3:** implementazione. Client: `http.Client{Timeout: 2s, CheckRedirect: ErrUseLastResponse}` e status ≠ 200 = errore; `io.LimitReader` a 256 KB + 1 per riconoscere il troppo grande; orologio iniettabile (`now func() time.Time`) per la cache; cache `map` con mutex, oltre 500 chiavi si svuota. Route in `rubrica.go`: `Warn` limitato con un `time.Time` dell'ultimo avviso sotto mutex. `data-rubrica="1"` sull'input in `dashboard.html` quando `s.rubrica != nil` (funzione di template o campo della vista).
- [ ] **Step 4:** test → PASS (incluso tutto `internal/web` in container); `go vet ./...`.
- [ ] **Step 5:** commit `Rubrica: ricerca dei contatti dal server`.

### Task 3: Cruscotto — gruppo «Persone» in `dashboard.js`, documentazione

**Files:**
- Modify: `web/static/js/dashboard.js`, `web/static/css/plancia.css` (solo se serve una regola per la riga a tre parti, con token esistenti), `CLAUDE.md`
- Test: `internal/web/admin_branding_test.go` o nuovo `internal/web/rubrica_test.go` (test che legge il JS)

**Interfaces:**
- Consumes: `GET /rubrica/cerca` (Task 2), `data-rubrica` sull'input.

- [ ] **Step 1: test che fallisce** `TestDashboardJSRubrica`: `dashboard.js` contiene `"/rubrica/cerca?q="`, `AbortController`, `"Persone"`, `"person"`.
- [ ] **Step 2:** test → FAIL.
- [ ] **Step 3:** in `dashboard.js`: stato `people = {q: "", list: []}`; sull'`input`, se `input.dataset.rubrica` e testo ≥ 2 caratteri, timer 250 ms → abort della richiesta precedente → `fetch("/rubrica/cerca?q=" + encodeURIComponent(typed))`; all'arrivo, se `typed` è ancora `input.value.trim()` e `ok`, salva e chiama `render()`. In `render`, dopo i gruppi locali e prima di «Web», gruppo «Persone» se `people.q === typed`: titolo = nome, sotto = reparto · `int. ` interno · mail (parti vuote omesse), `href` da `safeHref(p.url)`, nuova scheda. `render` conserva l'indice attivo se la voce attiva esiste ancora (altrimenti 0). Il testo «Nessun risultato in plancia.» resta legato ai soli risultati locali.
- [ ] **Step 4:** `node -e "new Function(require('fs').readFileSync('web/static/js/dashboard.js','utf8'))"`; test → PASS.
- [ ] **Step 5:** prova Playwright: server mock con `RUBRICA_URL` verso un server finto (piccolo programma Go temporaneo nello scratchpad che risponde a `/api/cerca` con 2 persone e ritardo di 800 ms per `q=lento`) → digitando «ross» compare «Persone» con interno e mail; Invio apre `…/contacts/<uid>`; scrivendo «lento» e poi subito «rossi» non compaiono i risultati di «lento»; tema scuro leggibile; nessun errore in console.
- [ ] **Step 6:** CLAUDE.md: riga in Architettura (Plancia → Ricerca a tendina: gruppo «Persone» da `RUBRICA_URL`, `internal/rubrica`, route pubblica `/rubrica/cerca`) e `/rubrica/cerca` nell'elenco delle route pubbliche.
- [ ] **Step 7:** commit `Rubrica: gruppo Persone nella ricerca a tendina`.
