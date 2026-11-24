# Registro delle modifiche dell'admin — piano di implementazione

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** registrare chi ha toccato cosa e quando nell'admin, con pagina `/admin/registro` in sola lettura.

**Architecture:** `requireAdmin` crea un `auditRecord` nel contesto e, dopo l'handler, se il POST è riuscito scrive una riga in `admin_log`; sezione e azione arrivano da una tabella indicizzata da `r.Pattern`, ID e titolo dall'handler (`auditObject`). Il ripristino scrive la sua riga in un file accanto al DB, reinserito da `web.New`.

**Tech Stack:** Go, `modernc.org/sqlite`, `html/template`, HTMX (già presenti). Nessuna dipendenza nuova.

**Spec:** `docs/superpowers/specs/2026-11-24-registro-admin-design.md`

## Global Constraints

- Mai modificare una migrazione rilasciata: aggiungere `migrateV18AdminLog` in coda a `migrations`.
- Date in DB come testo UTC `2006-01-02T15:04:05Z` (`formatTime`/`parseTime` del package `database`); in pagina ora di Roma (`s.cfg.Location`).
- Conservazione 12 mesi.
- Niente login/logout nel registro. Escluse: `POST /admin/anteprima`, `POST /admin/gruppi/{id}/anteprima`, `POST /admin/notifiche/prova`, `POST /admin/media`, `POST /admin/media/{id}/pezzo`, `POST /admin/media/{id}/fine`, `POST /admin/backup/upload`, `POST /admin/backup/upload/{id}/chunk`.
- Riuscita = POST, nessun 422 (anche trasformato in 200 + `X-Esito: non-valido`), status finale < 400 (status 0 = 200).
- Errore di scrittura del registro: `slog.Error("registro admin", "err", err)`, la risposta all'admin non cambia.
- CSP stretta: niente script/stili inline. Pagina admin con shell `admin_top`/`admin_bottom` e `renderPage`.
- Test `internal/web` in container (vedi CLAUDE.md), non su Windows.
- Mai dati reali nei test: admin `mrossi`, titoli inventati.

## Review Focus

1. Handler che mostra un errore con **200** invece di 422 (es. eliminazione di un gruppo/attributo in uso, `ErrInUse`): la riga non deve comparire. Test in Task 3 (`TestAuditInUseNotLogged`); se fallisce, l'handler va corretto a 422.
2. Eliminazione di un oggetto inesistente (404) e ID non numerico: nessuna riga. Test in Task 2.
3. Admin che modifica un'app cambiando il titolo: la riga riporta il **titolo nuovo** (dopo il salvataggio). Test in Task 3.
4. File pending illeggibile o JSON rotto all'avvio: il server parte comunque, file rinominato `.bad`, `slog.Error`. Test in Task 4.
5. Filtri della pagina con valori non presenti o pagina oltre l'ultima: tabella vuota con messaggio, 200, nessun errore. Test in Task 5.

---

### Task 1: tabella `admin_log` e funzioni del DB

**Files:**
- Modify: `internal/database/migrations.go` (coda di `migrations` + funzione)
- Create: `internal/database/admin_log.go`
- Test: `internal/database/admin_log_test.go`

**Interfaces:**
- Produces:
  ```go
  type AdminLogEntry struct {
      ID       int64
      At       time.Time
      Username string
      Section  string // etichetta del menu, es. "Applicativi"
      Action   string // es. "eliminato"
      ObjectID string // "" se assente
      Title    string // "" se assente
  }
  type AdminLogFilter struct{ Username, Section string; Limit, Offset int }
  func (db *DB) AddAdminLog(e AdminLogEntry) error
  func (db *DB) ListAdminLog(f AdminLogFilter) ([]AdminLogEntry, bool, error) // bool = ci sono altre righe dopo
  func (db *DB) AdminLogFacets() (users, sections []string, err error)          // valori distinti, ordinati
  func (db *DB) CleanupAdminLog(before time.Time) error
  func WritePendingAdminLog(path string, e AdminLogEntry) error
  func (db *DB) ImportPendingAdminLog(path string) error // file assente = nil
  ```

- [ ] **Step 1: test che falliscono** in `admin_log_test.go`:
  - `TestAdminLogAddList`: 3 righe (`mrossi`/`Applicativi`/`creato`/`12`/`Webmail`, `mrossi`/`Avvisi`, `lbianchi`/`Avvisi`) con `At` crescenti; `ListAdminLog({Limit:2})` → 2 righe, la più recente prima, `more == true`; `{Section:"Avvisi", Limit:10}` → 2; `{Username:"lbianchi", Limit:10}` → 1; `At` riletto uguale (UTC, secondi).
  - `TestAdminLogFacets`: users `[lbianchi mrossi]`, sections `[Applicativi Avvisi]`.
  - `TestAdminLogCleanup`: righe a −13 mesi e −1 mese; `CleanupAdminLog(now.AddDate(0,-12,0))` → resta solo la seconda.
  - `TestPendingAdminLog`: `WritePendingAdminLog(p, e)` poi `ImportPendingAdminLog(p)` → riga presente, file cancellato; seconda `Import` con file assente → nil, nessuna riga nuova.
  - `TestPendingAdminLogBroken`: file con `{rotto` → `ImportPendingAdminLog` restituisce errore, il file diventa `p+".bad"`, nessuna riga.
- [ ] **Step 2:** `go test ./internal/database/ -run 'AdminLog|Pending'` → FAIL (simboli non definiti).
- [ ] **Step 3: migrazione v18** `migrateV18AdminLog`: tabella `admin_log(id INTEGER PRIMARY KEY AUTOINCREMENT, at TEXT NOT NULL, username TEXT NOT NULL, section TEXT NOT NULL, action TEXT NOT NULL, object_id TEXT NOT NULL DEFAULT '', title TEXT NOT NULL DEFAULT '')`, indici `idx_admin_log_at(at)`, `idx_admin_log_user(username)`, `idx_admin_log_section(section)`. Commento sopra la funzione nello stile delle altre.
- [ ] **Step 4: funzioni** in `admin_log.go`. `ListAdminLog`: `ORDER BY at DESC, id DESC`, `LIMIT f.Limit+1` per calcolare `more`; filtri vuoti = nessun filtro. Pending: JSON di `AdminLogEntry` (con `At` RFC3339), scrittura su `path+".tmp"` e rename; import: lettura, `AddAdminLog`, `os.Remove`; JSON rotto → `os.Rename(path, path+".bad")` ed errore.
- [ ] **Step 5:** `go test ./internal/database/` → PASS (anche i test esistenti sulla versione dello schema: aggiornare il numero atteso se un test lo fissa).
- [ ] **Step 6: commit** `Registro admin: tabella admin_log (migrazione v18)`.

### Task 2: registrazione in `requireAdmin` e tabella delle route

**Files:**
- Create: `internal/web/audit.go`
- Modify: `internal/web/session.go` (`requireAdmin`, `validationWriter`)
- Test: `internal/web/audit_test.go`

**Interfaces:**
- Consumes: `AddAdminLog`, `AdminLogEntry` (Task 1).
- Produces:
  ```go
  type auditAction struct{ Section, Action string }
  var auditRoutes map[string]auditAction // chiave = r.Pattern
  var auditSkip map[string]bool          // pattern esclusi (Global Constraints) + i due ripristini
  func auditObject(r *http.Request, id any, title string) // id: int64 o string; no-op senza record nel contesto
  ```
  Tabella (sezione = etichetta del menu admin):
  | Pattern | Sezione | Azione |
  |---|---|---|
  | `POST /admin/ente` | Ente | modificato |
  | `POST /admin/avvisi` / `{id}` / `{id}/elimina` / `{id}/archivia` / `{id}/ripristina` | Avvisi | creato / modificato / eliminato / archiviato / ripristinato |
  | `POST /admin/calendario` / `{id}` / `{id}/elimina` | Calendario | creato / modificato / eliminato |
  | `POST /admin/app` / `{id}` / `{id}/elimina` / `{id}/sposta` | Applicativi | creato / modificato / eliminato / spostato |
  | `POST /admin/guide` / `{id}` / `{id}/elimina` / `{id}/sposta` / `{id}/aggiorna` | Guide | creato / modificato / eliminato / spostato / aggiornato |
  | `POST /admin/assistenza` / `{id}` / `{id}/elimina` / `{id}/sposta` | Assistenza | creato / modificato / eliminato / spostato |
  | `POST /admin/categorie` / `{id}` / `{id}/elimina` / `{id}/sposta` | Categorie | creato / modificato / eliminato / spostato |
  | `POST /admin/gruppi` / `{id}` / `{id}/elimina` / `{id}/sposta` | Gruppi | creato / rinominato / eliminato / spostato |
  | `POST /admin/gruppi/{id}/regole` / `{id}/regole/{rid}/elimina` | Gruppi | regola aggiunta / regola eliminata |
  | `POST /admin/gruppi/attributi` / `attributi/{id}/elimina` | Gruppi | attributo aggiunto / attributo eliminato |
  | `POST /admin/gruppi/attributi/{id}/testata` / `attributi/{id}/sposta` | Gruppi | testata modificata / testata spostata |
  | `POST /admin/backup` / `{name}/elimina` | Backup | backup creato / backup eliminato |
  `POST /admin/backup/{name}/ripristina` e `POST /admin/backup/upload/{id}/fine` stanno in `auditSkip`: la riga del ripristino la scrive il Task 4.

- [ ] **Step 1: test che falliscono** in `audit_test.go`:
  - `TestAuditRoutesCover`: legge `server.go`, estrae con regexp `"POST (/admin[^"]*)"` tutti i pattern; escluso login e logout, ognuno deve stare in `auditRoutes` o `auditSkip`, e nessuna chiave delle due mappe deve mancare dal file (niente voci morte).
  - `TestAuditLogsSuccess`: login, `POST /admin/categorie` con nome valido → una riga con `Username` = utente del login, `Section` "Categorie", `Action` "creato".
  - `TestAuditSkipsInvalid`: `POST /admin/categorie` con nome vuoto (422 → `X-Esito: non-valido`) → nessuna riga.
  - `TestAuditSkipsExcluded`: `POST /admin/anteprima` → nessuna riga.
  - `TestAuditSkipsNotFound`: `POST /admin/app/999/elimina` e `POST /admin/app/abc/elimina` → nessuna riga.
  - `TestAuditObjectNoRecord`: `auditObject` su una richiesta senza record nel contesto non va in panic.
- [ ] **Step 2:** test in container (comando di CLAUDE.md con `-run Audit ./internal/web/`) → FAIL.
- [ ] **Step 3: `validationWriter`** prende `status int` e `invalid bool` (impostati in `WriteHeader`; `Write` senza `WriteHeader` → status 200). **`requireAdmin`**: crea `&auditRecord{}` nel contesto (chiave non esportata), chiama l'handler, poi `s.audit(r, rec, vw)`: se `r.Method == POST`, pattern in `auditRoutes`, `!vw.invalid` e status < 400 → `AddAdminLog` con `At: s.now()`, `Username: s.currentAdmin(r)`. Pattern né mappato né escluso a runtime: `slog.Warn` (il test lo impedisce, è una rete in più). `auditObject` converte l'ID con `fmt.Sprint`.
- [ ] **Step 4:** test in container → PASS (tutto `./internal/web/`).
- [ ] **Step 5: commit** `Registro admin: scrittura in requireAdmin`.

### Task 3: titoli e ID negli handler

**Files:**
- Modify: `internal/web/admin_alerts.go`, `admin_apps.go`, `admin_guides.go`, `admin_calendar.go`, `admin_support.go`, `admin_categories.go`, `admin_audience.go`, `admin_branding.go`, `admin_backup.go`, `guide_refresh.go` (se «Aggiorna ora» è lì)
- Test: `internal/web/audit_test.go`

**Interfaces:**
- Consumes: `auditObject` (Task 2).

Regole: creazioni e modifiche chiamano `auditObject` **dopo** la scrittura riuscita, con l'ID (nuovo per le creazioni) e il titolo salvato; eliminazioni lo chiamano col titolo letto **prima** di cancellare (es. `handleAppDelete` ha già `a` da `GetApp`); spostamenti, archivia/ripristina, «Aggiorna ora» col titolo dell'oggetto. Titoli: app/guida/avviso/canale `Title`, categoria/gruppo `Name`, evento del calendario titolo dell'evento, attributo il suo nome AD, regola «<gruppo>: <tipo> <valore>», Ente il nome dell'ente, backup il nome del file (come `ObjectID` e titolo vuoto).

- [ ] **Step 1: test che falliscono**, tabellare `TestAuditTitles`: per ciascuna sezione (Avvisi, Calendario, Applicativi, Guide link, Assistenza, Categorie, Gruppi, Ente, Backup creato) crea l'oggetto via POST e verifica l'ultima riga: `ObjectID` = ID creato, `Title` = titolo inviato; poi modifica con titolo nuovo → `Title` = titolo nuovo; poi elimina → `Title` = ultimo titolo, `Action` "eliminato".
  - `TestAuditInUseNotLogged`: attributo usato da una regola, `POST /admin/gruppi/attributi/{id}/elimina` → nessuna riga «attributo eliminato» (stesso per un gruppo usato da un contenuto). Se l'handler risponde 200, correggerlo a 422 (deve mostrare ancora l'errore: verificare il testo nella risposta).
- [ ] **Step 2:** test in container → FAIL.
- [ ] **Step 3:** aggiungere le chiamate `auditObject` negli handler secondo le regole sopra.
- [ ] **Step 4:** test in container → PASS (tutto `./internal/web/`).
- [ ] **Step 5: commit** `Registro admin: titoli e ID degli oggetti`.

### Task 4: riga del ripristino sopravvive allo swap

**Files:**
- Modify: `internal/web/admin_backup.go` (`startRestore`), `internal/web/server.go` (`New`)
- Create (o in `audit.go`): `func pendingAuditPath(dbPath string) string` → `filepath.Join(filepath.Dir(dbPath), "admin_log_pending.json")`
- Test: `internal/web/audit_test.go`

**Interfaces:**
- Consumes: `WritePendingAdminLog`, `ImportPendingAdminLog` (Task 1).

- [ ] **Step 1: test che falliscono**:
  - `TestAuditRestorePending`: crea backup, `POST /admin/backup/{name}/ripristina` con `conferma=RIPRISTINA` → il file pending esiste con `Section` "Backup", `Action` "backup ripristinato", `ObjectID` = nome dell'archivio, `Username` dell'admin (attendere l'exit come negli altri test di ripristino).
  - `TestAuditPendingImportedAtStart`: scrive un pending nel dir del DB, crea il server con `newTestServerWith` sullo stesso DB → la riga c'è, il file no.
  - `TestAuditPendingBrokenAtStart`: pending rotto → `New` non fallisce, esiste `.bad`.
  - Validazione fallita (archivio rifiutato) → nessun file pending.
- [ ] **Step 2:** test in container → FAIL.
- [ ] **Step 3:** in `startRestore`, dopo `PrepareRestore` riuscito e prima della risposta, `WritePendingAdminLog` (errore → `slog.Error`, il ripristino prosegue). Per l'upload `ObjectID` = nome originale se noto, altrimenti "caricato". In `New`, dopo la costruzione di `s`, `ImportPendingAdminLog(pendingAuditPath(o.Config.DBPath))`; errore → `slog.Error`, nessun fallimento.
- [ ] **Step 4:** test in container → PASS.
- [ ] **Step 5: commit** `Registro admin: ripristino nel registro`.

### Task 5: pagina `/admin/registro`, menu, pulizia, documentazione

**Files:**
- Create: `internal/web/admin_log.go`, `web/templates/admin_registro.html`
- Modify: `internal/web/server.go` (route `GET /admin/registro` dietro `requireAdmin`), `web/templates/admin_base.html` (voce dopo «Backup», icona `history`, sezione `registro`), `internal/web/presence.go` (`StartPresenceCleanup`: `CleanupAdminLog(s.now().AddDate(0,-12,0))`, errore → `slog.Warn`), `CLAUDE.md` (v18 nell'elenco tabelle + riga su `internal/web/audit.go` e la regola «nuova route POST admin = voce in `auditRoutes` o `auditSkip`»)
- Test: `internal/web/audit_test.go`

**Interfaces:**
- Consumes: `ListAdminLog`, `AdminLogFacets`, `CleanupAdminLog` (Task 1).
- Produces: `GET /admin/registro?chi=&sezione=&pagina=` (pagina da 1, 100 righe).

- [ ] **Step 1: test che falliscono**:
  - `TestAdminRegistroPage`: 2 righe inserite col DB; pagina 200 con entrambi i titoli, ora in Europe/Rome (`fixedNow` → «10:00»), utente e sezione; `?chi=lbianchi` mostra solo la sua; `?sezione=Inesistente` → 200 con «Nessuna modifica registrata.»; `?pagina=99` → 200, stesso messaggio; `?pagina=abc` → pagina 1.
  - `TestAdminRegistroRequiresLogin`: senza cookie → 303 a `/admin/login`.
  - Oggetto senza titolo: cella «<Sezione> #<id>»; senza ID né titolo: cella vuota.
  - Cleanup: test sulla funzione chiamata dal giro (riga a −13 mesi sparisce).
- [ ] **Step 2:** test in container → FAIL.
- [ ] **Step 3:** handler `handleAdminRegistro` con `renderPage(w, r, "admin_registro.html", "registro", view)`; view con righe, facets per due `<select>` (form GET, bottone «Filtra», nessun JS), link «Più vecchie»/«Più recenti» che mantengono i filtri. Tabella con lo stile di `admin_ticket.html`. Colonne: Quando · Chi · Sezione · Azione · Oggetto.
- [ ] **Step 4:** test in container → PASS; `go vet ./...`; `go build ./...`.
- [ ] **Step 5:** prova nel browser (server mock, Playwright in container come da CLAUDE.md): crea un'app, modificala, eliminala, apri `/admin/registro` → tre righe; filtri funzionanti; tema scuro leggibile.
- [ ] **Step 6: commit** `Registro admin: pagina, menu e pulizia`.
