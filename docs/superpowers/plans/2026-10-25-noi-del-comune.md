# «Noi del Comune» Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** In plancia compaiono numeri di gruppo («Oggi siamo in N», traguardi, «Letto dal X%», fiammella sulle tile più aperte) e il widget personale «Il tuo mese» con spille e statistiche dell'assistenza. Cambiano anche le regole di lettura degli avvisi.

**Architecture:** I dati nuovi stanno in SQLite (migrazione v14) dietro metodi di `internal/database`. Le statistiche OTRS le calcola un job orario, che salva il risultato nel DB. In `internal/web` un nuovo file `stats.go` mette insieme cache e vista. I template e `dashboard.js` cambiano senza toccare la CSP. Con `STATS_ENABLED=false` sparisce tutto tranne le letture.

**Tech Stack:** Go, `modernc.org/sqlite`, `html/template`, HTMX 2, JS vanilla, CSS.

**Spec:** `docs/superpowers/specs/2026-10-23-noi-del-comune-design.md`

## Global Constraints

- **Sempre 200** dagli endpoint della plancia, compreso `/app/{id}/clic` (il proxy `revprx01` sostituisce le risposte 4xx/5xx).
- **CSP stretta**: niente `<script>`/`<style>` inline né `on*=`. Il JS va in `web/static/js/dashboard.js` e `notifiche.js`, gli stili in `web/static/css/plancia.css`.
- **Solo il giorno, mai l'ora**: in `user_days` e `app_clicks` si salva un `YYYY-MM-DD` del calendario Europe/Rome (`s.loc()`).
- **Nessuna classifica e nessun confronto tra persone**, nessuna streak, nessun endpoint con i dati di altri utenti.
- **Migrazioni**: si aggiunge `migrateV14Stats` in coda a `migrations.go`. Le migrazioni esistenti non si toccano mai.
- **Nuove variabili d'ambiente in tre posti**: `docker-compose.yml` (`${VAR}`), `.env.example`, `internal/config`. Le variabili sono `STATS_ENABLED` (default `true`) e `STATS_RETENTION_MONTHS` (default `13`).
- **Soglie esatte dalla spec**:
  - pastiglia nascosta con N < 2; cache 60 s;
  - traguardi a 1000, 2500, 5000, 10000, 25000, 50000, 100000, poi ogni 100000;
  - «Letto dal X%» solo su avvisi pubblici, con ≥ 10 utenti attivi, avviso iniziato da ≥ 1 h e X ≥ 10; cache 5 min;
  - fiammella sulle prime 3 app degli ultimi 7 giorni con ≥ 5 clic, tra quelle visibili; cache 10 min;
  - «Ho letto» abilitato dopo **6 s**;
  - job OTRS orario, `Limit` 10000, al massimo 300 `TicketGet` per giro;
  - `app_clicks` conservati 90 giorni.
- **Identità dichiarata**: i dati sono indicativi e nessuna autorizzazione dipende da loro.
- **Test**: `go test ./internal/database/ ./internal/otrs/ ./internal/config/` si possono lanciare su Windows. `internal/web` va lanciato **solo in container**:
  `MSYS_NO_PATHCONV=1 docker run --rm -v "$(pwd -W)":/src -v "$(pwd -W)/.devcache":/cache -e GOPATH=/cache/gopath -e GOCACHE=/cache/build -e CGO_ENABLED=0 -w /src golang:1.27-alpine go test ./internal/web/ -run '<Nome>'`
- **Sintassi JS**: `node -e "new Function(require('fs').readFileSync('web/static/js/dashboard.js','utf8'))"`.
- **Branch**: si lavora su `feat/noi-del-comune`, creato da `main` aggiornato (dopo il merge della PR #40, oppure da `spec/noi-del-comune`).

## Review Focus

1. **Utente anonimo o non riconosciuto** (cookie assente, `Anonymous`): vede la pastiglia, non vede il widget, non scrive `user_days`, non scrive letture né clic. Test in Task 7 (`TestPillAnonymous`) e Task 10 (`TestAppClickAnonymousIgnored`).
2. **`STATS_ENABLED=false`**: niente pastiglia, widget, fiammella né percentuale; nessuna scrittura in `user_days`/`app_clicks`; job OTRS non avviato. Le letture però restano. Test in Task 7 (`TestStatsDisabled`).
3. **Cambio di giorno a mezzanotte di Roma**, non UTC. Esempio: alle 00:30 di Roma (22:30Z del giorno prima) l'accesso va nel giorno nuovo. Test in Task 7 (`TestRecordDayUsesRomeCalendar`).
4. **Refresh HTMX del carosello** (`/partials/alerts`): deve portare gli stessi attributi `data-letto` e la stessa percentuale della plancia, altrimenti il popup urgente si riapre dopo 5 minuti. Test in Task 8 (`TestAlertsPartialCarriesReadState`).
5. **App eliminata o disabilitata** dopo i clic: la fiammella non punta ad app sparite, e `/app/{id}/clic` su un'app inesistente risponde 200 senza scrivere nulla. Test in Task 10 (`TestAppClickUnknownApp`).

---

### Task 1: Configurazione `STATS_ENABLED` e `STATS_RETENTION_MONTHS`

**Files:**
- Modify: `internal/config/config.go` (struct `Config`, `Load`)
- Modify: `docker-compose.yml` (`environment:`), `.env.example`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces: `Config.StatsEnabled bool`, `Config.StatsRetentionMonths int`

- [ ] **Step 1: Write the failing test** `TestStatsConfig`:
  - default → `StatsEnabled == true`, `StatsRetentionMonths == 13`;
  - `STATS_ENABLED=false` → `false`;
  - `STATS_RETENTION_MONTHS=6` → `6`;
  - `STATS_RETENTION_MONTHS=0` → errore. Serve almeno 1 mese: `getEnvInt` accetta 0, quindi va aggiunto un controllo `< 1` con messaggio `STATS_RETENTION_MONTHS: almeno 1`.
- [ ] **Step 2:** `go test ./internal/config/ -run TestStatsConfig` → FAIL (campo inesistente).
- [ ] **Step 3:** Leggere le due variabili con `getEnvBool`/`getEnvInt` in `Load`. Aggiungere `STATS_ENABLED: ${STATS_ENABLED}` e `STATS_RETENTION_MONTHS: ${STATS_RETENTION_MONTHS}` al compose, e le due righe commentate in `.env.example`.
- [ ] **Step 4:** `go test ./internal/config/` → PASS.
- [ ] **Step 5:** Commit `Config: STATS_ENABLED e STATS_RETENTION_MONTHS`.

### Task 2: Migrazione v14 e giorni di accesso

**Files:**
- Modify: `internal/database/migrations.go` (aggiungere `migrateV14Stats` all'elenco)
- Create: `internal/database/stats.go`
- Test: `internal/database/stats_test.go`

**Interfaces:**
- Produces:
  - I giorni usano `database.DayLayout` (`"2006-01-02"`), che esiste già.
  - `func (db *DB) RecordDay(username, day string) (added bool, err error)`: `INSERT OR IGNORE` in `user_days`. Se la riga è nuova, nella **stessa transazione** fa `counters.accessi += 1`. Con uno username vuoto non fa nulla. Lo username va in minuscolo, come in `TouchPresence`.
  - `func (db *DB) UsersOnDay(day string) (int, error)`
  - `func (db *DB) UserDaysBetween(username, fromDay, toDay string) (int, error)`: estremi inclusi.
  - `func (db *DB) AccessTotal() (int64, error)`: valore di `counters` con `name = 'accessi'`.
  - `func (db *DB) ReachMilestones(total int64, day string) error`: scrive in `milestones` le soglie ≤ total non ancora presenti, con `reached_on = day`.
  - `func (db *DB) MilestoneOn(day string) (threshold int64, ok bool, err error)`: la soglia più alta con `reached_on = day`.
  - `func Milestones(upTo int64) []int64`: funzione pura con l'elenco delle soglie definite nella spec.
  - `func (db *DB) DeleteUserDays(username string) error`
  - `func (db *DB) CleanupStats(daysBefore, clicksBefore string) error`: cancella `user_days.day < daysBefore`, `app_clicks.day < clicksBefore` e `ticket_facts.created_at < daysBefore + "T00:00:00Z"`. **Non** tocca `counters`.

La migrazione crea `user_days`, `app_clicks`, `milestones`, `ticket_stats` e `ticket_facts` esattamente come nella spec (sezione Dati). In più crea `counters (name TEXT PRIMARY KEY, value INTEGER NOT NULL)` con la riga `('accessi', 0)`.

- [ ] **Step 1: Write the failing tests**:
  - `TestRecordDayOncePerDay`: due `RecordDay("MRossi","2026-10-23")`, la prima con `added == true` e la seconda con `false`; `UsersOnDay == 1`; `AccessTotal == 1`.
  - `TestAccessTotalSurvivesCleanup`: tre giorni registrati, poi `CleanupStats("2026-10-23", …)` cancella i primi due. `UsersOnDay` dei giorni vecchi vale 0, ma `AccessTotal` resta 3.
  - `TestMilestones`:
    - `Milestones(250000)` dà `[1000 2500 5000 10000 25000 50000 100000 200000]`;
    - `ReachMilestones(1200, "2026-10-23")` → `MilestoneOn("2026-10-23") == 1000, true`;
    - un secondo `ReachMilestones(1300, "2026-10-24")` non scrive nulla di nuovo e `MilestoneOn("2026-10-24")` dà `ok == false`.
  - `TestUserDaysBetween`: 3 giorni in ottobre e 1 a novembre; `("2026-10-01","2026-10-31")` → 3.
- [ ] **Step 2:** `go test ./internal/database/ -run 'RecordDay|AccessTotal|Milestones|UserDays'` → FAIL.
- [ ] **Step 3:** Implementare migrazione e metodi in `stats.go`.
- [ ] **Step 4:** `go test ./internal/database/` → PASS, compresi i test esistenti sulle migrazioni e sul backup.
- [ ] **Step 5:** Commit `Database: v14, giorni di accesso e traguardi`.

### Task 3: Clic aggregati, letture per utente e conteggi

**Files:**
- Modify: `internal/database/stats.go`, `internal/database/reads.go`
- Test: `internal/database/stats_test.go`, `internal/database/reads_test.go`

**Interfaces:**
- Produces:
  - `func (db *DB) AddAppClick(appID int64, day string) error`: upsert con `clicks = clicks + 1`. Un `appID` inesistente produce un errore di FK, che il chiamante ignora.
  - `func (db *DB) AppClicksSince(fromDay string) (map[int64]int, error)`
  - `const ReadPlancia = "plancia"`. `MarkRead` lo accetta insieme a `ReadConfirm`/`ReadOpen`.
  - `func (db *DB) ReadAlertIDs(username string) (map[int64]bool, error)`: gli avvisi letti dall'utente.
  - `func (db *DB) ReadCounts() (map[int64]int, error)`: numero di letture per avviso.
  - `func (db *DB) MarkReadMany(alertIDs []int64, username, how string, now time.Time) error`: una sola transazione con `INSERT OR IGNORE`.
  - `func (db *DB) HasPushSubscription(username string) (bool, error)`
  - `func (db *DB) TicketsOpenedBetween(username string, from, to time.Time) ([]string, error)`: i `ticket_id` di `tickets_sent` con `kind = 'apertura'`.
  - `func (db *DB) HasOpenedTicket(username string) (bool, error)`

- [ ] **Step 1: Write the failing tests**:
  - `TestAppClicks`: 2 clic sull'app A oggi, 1 su B oggi, 1 su A 10 giorni fa. `AppClicksSince(7 giorni fa)` dà `{A:2, B:1}`. Dopo `DeleteApp(A)` la mappa non contiene più A (cascade).
  - `TestMarkReadPlancia`: `MarkRead(id,"mrossi",ReadPlancia,…)` scrive; `ReadsFor(id)[0].How == "plancia"`. Un `how` sconosciuto (`"boh"`) viene ignorato.
  - `TestMarkReadManyFirstWins`: un `ReadConfirm` già presente non viene sostituito da `MarkReadMany(…, ReadPlancia)`.
  - `TestReadAlertIDsAndCounts`: 2 utenti leggono A e 1 utente legge B → `ReadCounts == {A:2, B:1}`, `ReadAlertIDs("mrossi")` contiene solo i suoi.
  - `TestTicketsOpenedBetween`: ignora le righe con `kind = 'risposta'`/`'chiusura'` e quelle di altri utenti.
- [ ] **Step 2:** `go test ./internal/database/ -run 'AppClicks|MarkRead|ReadAlertIDs|TicketsOpened'` → FAIL.
- [ ] **Step 3:** Implementare.
- [ ] **Step 4:** `go test ./internal/database/` → PASS.
- [ ] **Step 5:** Commit `Database: clic aggregati, letture in plancia e conteggi`.

### Task 4: Fatti dei ticket e statistiche mensili nel DB

**Files:**
- Modify: `internal/database/stats.go`
- Test: `internal/database/stats_test.go`

**Interfaces:**
- Produces:
  - `type TicketFact struct { TicketID string; CreatedAt time.Time; FirstReplyAt *time.Time; ClosedMonth string }`
  - `func (db *DB) UpsertTicketCreated(id string, created time.Time) error`: inserisce se manca. Non cancella mai `first_reply_at`.
  - `func (db *DB) TicketsWithoutReply(createdFrom time.Time, limit int) ([]TicketFact, error)`: in ordine di `created_at`.
  - `func (db *DB) SetFirstReply(id string, at time.Time) error`
  - `func (db *DB) SetClosedMonth(month string, closedIDs []string) error`: in una transazione mette `closed_month = month` sugli ID dati (inserendo la riga se manca, con `created_at` = epoch) e `NULL` sulle righe con `closed_month = month` che non sono in elenco.
  - `func (db *DB) ClosedInMonth(month string, ids []string) (int, error)`: quanti degli `ids` hanno `closed_month = month`.
  - `type TicketStats struct { Month string; Requests, Resolved int; MedianFirst *time.Duration; UpdatedAt time.Time }`
  - `func (db *DB) SaveTicketStats(st TicketStats) error`
  - `func (db *DB) GetTicketStats(month string) (TicketStats, bool, error)`
  - `func (db *DB) FirstReplyDurations(createdFrom, createdTo time.Time) ([]time.Duration, error)`

- [ ] **Step 1: Write the failing tests**:
  - `TestTicketFactsReopen`: `SetClosedMonth("2026-10", ["1","2"])`, poi `SetClosedMonth("2026-10", ["1"])` → `ClosedInMonth("2026-10", ["1","2"]) == 1`.
  - `TestTicketsWithoutReply`: dopo `SetFirstReply("1", …)` il ticket 1 non compare più; viene rispettato `limit`.
  - `TestTicketStatsRoundTrip`: con `MedianFirst` nil e non nil.
- [ ] **Step 2:** FAIL → **Step 3:** implementare → **Step 4:** `go test ./internal/database/` PASS.
- [ ] **Step 5:** Commit `Database: fatti e statistiche mensili dei ticket`.

### Task 5: OTRS, ticket del mese e prima risposta

**Files:**
- Modify: `internal/otrs/otrs.go` (interfaccia `Client`), `internal/otrs/read.go`, `internal/otrs/mock.go`
- Test: `internal/otrs/read_test.go`

**Interfaces:**
- Produces, nell'interfaccia `Client`:
  - `MonthTicketIDs(ctx context.Context, from time.Time) (createdIDs, closedIDs []string, err error)`. Fa due `search`:
    - `TicketCreateTimeNewerDate` = `from`, `Limit: 10000`;
    - `StateType: ["closed"]`, `TicketCloseTimeNewerDate` = `from`, `Limit: 10000`.

    `search` aggiunge già `Queues`.
  - `FirstReply(ctx context.Context, id string) (created time.Time, reply *time.Time, err error)`: un solo `getRaw(id, false)` dà la data di creazione e il primo articolo visibile (`visibleTypes`, `SenderType agent`, non `toThirdParty`) **escluso il primo articolo del ticket**. Si restituisce `ErrNotYours` se il ticket è di un'altra coda.
- Mock:
  - `MonthTicketIDs`: dai `Tickets`, quelli con `Created >= from` e quelli con `Closed && Changed >= from`;
  - `FirstReply`: il primo `Articles[i]` con `i > 0 && FromAgent && !ToThirdParty`.

- [ ] **Step 1: Write the failing tests** (con l'helper `otrsRead` esistente):
  - `TestMonthTicketIDs`: verifica che il corpo della ricerca contenga `TicketCreateTimeNewerDate` nel formato `2006-01-02 15:04:05` (ora locale), `Queues` e `Limit` 10000, e la seconda chiamata `StateType ["closed"]` e `TicketCloseTimeNewerDate`.
  - `TestFirstReplySkipsOpeningArticle`: ticket con articoli [agent phone (apertura), customer, agent email-external] → `reply` = data del terzo.
  - `TestFirstReplyIgnoresInternalAndThirdParty`: una nota interna dell'agente e una mail a terzi vengono saltate; senza risposte valide `reply == nil`.
  - `TestFirstReplyOtherQueue` → `ErrNotYours`.
  - `TestMockStats`: il mock rispetta le stesse regole.
- [ ] **Step 2:** `go test ./internal/otrs/` → FAIL.
- [ ] **Step 3:** Implementare. Aggiornare anche gli eventuali fake di `Client` nei test di `internal/web` (cercare `otrs.Client` nei file `_test.go`).
- [ ] **Step 4:** `go test ./internal/otrs/` e `go build ./...` → PASS.
- [ ] **Step 5:** Commit `OTRS: ID dei ticket del mese e prima risposta`.

### Task 6: Job statistiche OTRS

**Files:**
- Create: `internal/web/ticket_stats.go`
- Modify: `cmd/server/main.go` (`srv.StartTicketStats(ctx)` dopo `StartTicketWatch`)
- Test: `internal/web/ticket_stats_test.go`

**Interfaces:**
- Consumes: Task 4 (`UpsertTicketCreated`, `TicketsWithoutReply`, `SetFirstReply`, `SetClosedMonth`, `FirstReplyDurations`, `SaveTicketStats`), Task 5 (`MonthTicketIDs`, `FirstReply`).
- Produces:
  - `func (s *Server) ticketStatsOnce(ctx context.Context) error`
  - `func (s *Server) StartTicketStats(ctx context.Context)`: non fa nulla senza `ticketsEnabled()` o con `!cfg.StatsEnabled`; altrimenti gira subito e poi ogni ora (`ticketStatsEvery = time.Hour`).

Algoritmo di `ticketStatsOnce` (mese = mese corrente in `s.loc()`, `from` = il primo del mese alle 00:00 locali):
1. `MonthTicketIDs(from)`.
2. Per ogni ID creato non ancora in `ticket_facts`, `FirstReply(id)` dà la data di creazione (e magari già la risposta). Si salva con `UpsertTicketCreated` (+ `SetFirstReply`). Al massimo 300 `FirstReply` per giro, contando anche il passo 3.
3. Per i `TicketsWithoutReply(from, rimanenti)`, `FirstReply`; se c'è una risposta, `SetFirstReply`. Un ticket in errore si salta: si registra il primo errore e si va avanti.
4. `SetClosedMonth(mese, closedIDs)`.
5. Si calcola la mediana di `FirstReplyDurations(from, from+1 mese)` (con numero pari di valori, la media dei due centrali, troncata al secondo). Poi `SaveTicketStats{Requests: len(createdIDs), Resolved: len(closedIDs), MedianFirst}`.

- [ ] **Step 1: Write the failing tests** (`newTestServerWith` + `otrs.NewMock()` con ticket costruiti a mano, `fixedNow`):
  - `TestTicketStatsOnce`: 3 ticket creati in ottobre (risposte dopo 1 h, 3 h, nessuna), 1 chiuso → `GetTicketStats("2026-10")` dà `Requests 3, Resolved 1, MedianFirst 2h`.
  - `TestTicketStatsDoesNotRereadAnswered`: un mock che conta le chiamate a `FirstReply`; il secondo giro non rilegge i ticket con risposta.
  - `TestTicketStatsDisabled`: con `StatsEnabled=false`, `StartTicketStats` non scrive nulla.
- [ ] **Step 2:** In container `-run TicketStats` → FAIL.
- [ ] **Step 3:** Implementare e collegarlo in `main.go`.
- [ ] **Step 4:** In container `-run TicketStats` → PASS.
- [ ] **Step 5:** Commit `Assistenza: statistiche mensili da OTRS ogni ora`.

### Task 7: Accesso del giorno, pastiglia e traguardi

**Files:**
- Create: `internal/web/stats.go`
- Modify: `internal/web/server.go` (campo `stats *statsCache` in `Server`, creato in `New`), `internal/web/dashboard.go` (`dashboardView.Pill`), `web/templates/dashboard.html`, `web/static/css/plancia.css`
- Test: `internal/web/stats_test.go`

**Interfaces:**
- Consumes: Task 1 (`StatsEnabled`), Task 2.
- Produces:
  - `type pillView struct { Count int; Milestone int64 }` (`Count` 0 = nascosta)
  - `func (s *Server) dayOf(t time.Time) string`: `t.In(s.loc()).Format("2006-01-02")`
  - `func (s *Server) recordAccess(username string)`: chiama `RecordDay(username, oggi)`; se la riga è nuova e dall'ultimo controllo è passato almeno un minuto, `ReachMilestones(AccessTotal, oggi)`. Gli errori vanno solo nel log (`slog.Warn("statistiche", …)`).
  - `func (s *Server) pill() pillView`: cache di 60 s su `UsersOnDay(oggi)` e `MilestoneOn(oggi)`. Con `Count < 2` e senza traguardo il risultato è `pillView{}`. Con un traguardo, `Milestone` ha la precedenza.
  - Funzione di template `thousands` (`10000` → `10.000`), registrata in `render.go`.

`handleDashboard` chiama `recordAccess` accanto a `TouchPresence`, solo se `cfg.StatsEnabled`, e mette `Pill: s.pill()` nella vista (pillView vuota se spento).

Template, dentro `.hero-main` subito dopo `<h1 class="hello">`:
- con un traguardo: `<span class="hero-together"><span class="material-icons" aria-hidden="true">celebration</span>Abbiamo superato {{thousands .Pill.Milestone}} accessi</span>`;
- altrimenti, con `Count`: `groups` + `Oggi siamo in {{.Pill.Count}}`.

Stile: la pastiglia della testata segue lo stile di `.chip` della bozza B (sfondo `--navy-2`, bordo `--navy-line`).

- [ ] **Step 1: Write the failing tests**:
  - `TestPillCountsRecognizedUsers`: tre utenti diversi fanno `GET /` → il corpo contiene `Oggi siamo in 3`. Lo stesso utente due volte conta 1.
  - `TestPillHiddenUnderTwo`: un solo utente → nessun `hero-together`.
  - `TestPillAnonymous`: dopo 2 riconosciuti, un `GET /` anonimo mostra la pastiglia e `UsersOnDay` resta 2.
  - `TestRecordDayUsesRomeCalendar`: `Now` = `2026-10-24T22:30:00Z` → la riga è del `2026-10-25`.
  - `TestMilestonePill`: `counters.accessi` portato a 999 con un `UPDATE` diretto, poi un nuovo accesso → `Abbiamo superato 1.000 accessi`.
  - `TestStatsDisabled`: con `StatsEnabled=false` non c'è `hero-together` e `UsersOnDay == 0`.
- [ ] **Step 2:** In container `-run 'Pill|RecordDay|Milestone|StatsDisabled'` → FAIL.
- [ ] **Step 3:** Implementare.
- [ ] **Step 4:** Stesso comando → PASS.
- [ ] **Step 5:** Commit `Plancia: «Oggi siamo in N» e traguardi collettivi`.

### Task 8: Nuove regole di lettura degli avvisi

**Files:**
- Modify:
  - `internal/database/alerts.go` (campo calcolato `ReadByViewer bool` in `Alert`, accanto a `NotForViewer`)
  - `internal/web/dashboard.go` (`handleDashboard`, `handleAlertsPartial`)
  - `internal/web/presence.go`
  - `internal/web/admin_readers.go` (`readTrackable`)
  - `internal/web/render.go` (`readLabel`)
  - `web/templates/partials_dashboard.html` (`alert_card`, dialog urgente)
  - `web/static/js/dashboard.js`, `web/static/css/plancia.css`
- Test: `internal/web/presence_test.go`, `internal/web/dashboard_test.go`, `internal/web/admin_readers_test.go`

**Interfaces:**
- Consumes: Task 3 (`ReadPlancia`, `MarkReadMany`, `ReadAlertIDs`).
- Produces: `func (s *Server) markViewerReads(username string, alerts []database.Alert) []database.Alert`. Per un utente riconosciuto:
  - scrive `ReadPlancia` sugli avvisi **non urgenti** con `!needsMore(Body)` e `!NotForViewer`;
  - poi imposta `ReadByViewer` su tutti gli avvisi da `ReadAlertIDs`.

  Va chiamato **solo** da `handleDashboard`. `handleAlertsPartial` invece imposta soltanto `ReadByViewer` (`func (s *Server) withReadState(username string, alerts []database.Alert) []database.Alert`) senza scrivere, perché il refresh non è una nuova apertura.

Le letture **non** dipendono da `STATS_ENABLED`.

Template:
- In `alert_card`: `data-letto` su `<article>` se `.ReadByViewer`. Dentro `.news-expand`, dopo il corpo:
  - se l'avviso non è letto: `<button type="button" class="news-read" data-read disabled>Ho letto</button>`;
  - se è letto: `<span class="news-read-done"><span class="material-icons" aria-hidden="true">check</span>Letto</span>`.
- Nel dialog urgente: `data-letto` se `.ReadByViewer`. Il bottone diventa `<button value="letto" data-read disabled>Ho letto</button>`.

JS (`dashboard.js`):
- `openNews` **non** chiama più `sendRead`. All'apertura avvia un timer di 6000 ms (`READ_DELAY_MS = 6000`) che abilita `[data-read]` nella card. Alla chiusura il timer si annulla e il bottone torna disabilitato.
- Il clic su `.news-read` chiama `sendRead(id, "conferma")` e sostituisce il bottone con «Letto».
- Mentre si aspetta, il bottone mostra un riempimento CSS: `.news-read[disabled]` con `background-size` animato per 6 s (`@keyframes`). Con `prefers-reduced-motion` il riempimento non si anima e compare solo il testo «Ho letto (fra poco)».
- Popup urgente: `isRead(d)` restituisce true anche se c'è `d.hasAttribute("data-letto")`. Dopo `showModal()` il bottone `[data-read]` si abilita dopo 6000 ms. `localStorage` resta per chi non ha `data-letto`.

Admin: `readTrackable` restituisce sempre `true` (anche gli avvisi corti ora vengono rilevati); il ramo `Trackable == false` del template si toglie. `readLabel(ReadPlancia)` restituisce `"in plancia"`.

- [ ] **Step 1: Write the failing tests**:
  - `TestShortAlertReadOnDashboard`: un avviso corto pubblico e uno lungo (`strings.Repeat("parola ", 60)`). `GET /` di mrossi → `ReadsFor(corto)` vale `[{how: plancia}]` e `ReadsFor(lungo)` è vuoto.
  - `TestShortUrgentNotAutoRead`: un urgente corto non viene letto al caricamento.
  - `TestReservedShortAlertNotRead`: con «Mostra tutto», un avviso corto non destinato a mrossi non viene letto.
  - `TestAlertsPartialCarriesReadState`: dopo `POST /avvisi/{urgente}/letto come=conferma`, sia `GET /` sia `GET /partials/alerts` contengono `data-urgent="{id}"` insieme a `data-letto`. Il partial non scrive nuove letture: un avviso corto creato dopo non risulta letto dopo `GET /partials/alerts`.
  - `TestReadButtonDisabled`: in `GET /`, la card lunga non letta contiene `data-read disabled`.
  - Adattare `TestReadFromPlancia` e i test dell'admin che si aspettano `Trackable == false`.
- [ ] **Step 2:** In container `-run 'Read|Alert|Readers'` → FAIL.
- [ ] **Step 3:** Implementare (Go, template, JS, CSS).
- [ ] **Step 4:** In container `-run 'Read|Alert|Readers|Dashboard'` → PASS. Controllo di sintassi JS (vedi Global Constraints) → nessun errore.
- [ ] **Step 5:** Commit `Avvisi: corti letti in plancia, «Ho letto» dopo 6 secondi, stato dal DB`.

### Task 9: «Letto dal X% dei colleghi»

**Files:**
- Modify: `internal/database/alerts.go` (campo calcolato `ReadPct int`), `internal/web/stats.go`, `internal/web/dashboard.go` (entrambi gli handler), `web/templates/partials_dashboard.html` (`alert_card`), `web/static/css/plancia.css`
- Test: `internal/web/stats_test.go`

**Interfaces:**
- Consumes: Task 3 (`ReadCounts`), `ActiveUsernames`, `contentFilter.alerts`.
- Produces: `func (s *Server) withReadPct(alerts []database.Alert, f *contentFilter) []database.Alert`. Per ogni avviso calcola `ReadPct` se valgono tutte queste condizioni:
  - `STATS_ENABLED`;
  - avviso pubblico (`f.alerts[a.ID].Mode` vuoto / nessuna riga);
  - `now - StartsAt >= 1h`;
  - utenti attivi ≥ 10.

  `ReadPct = min(100, round(letture*100/attivi))`, e `0` se sotto 10. I conteggi stanno in cache 5 minuti.

Template, in fondo ad `alert_card`: `{{if .ReadPct}}<p class="news-readers">Letto dal {{.ReadPct}}% dei colleghi</p>{{end}}`. Stile piccolo, colore `--muted`.

- [ ] **Step 1: Write the failing tests**:
  - `TestReadPct`: 20 utenti attivi (`TouchPresence`), 15 letture di un avviso pubblico iniziato 2 h fa → `Letto dal 75% dei colleghi`.
  - `TestReadPctHidden`: assente con un avviso riservato, con 9 utenti attivi, con un avviso iniziato 30 min fa, o con 1 lettura su 20 (5%).
- [ ] **Step 2:** FAIL → **Step 3:** implementare → **Step 4:** in container `-run ReadPct` PASS.
- [ ] **Step 5:** Commit `Avvisi: «Letto dal X% dei colleghi»`.

### Task 10: Clic sulle tile e fiammella

**Files:**
- Modify: `internal/web/stats.go`, `internal/web/server.go` (route `POST /app/{id}/clic`), `internal/database/dashboard.go` (campo calcolato `Hot bool` in `AppWithGuides`), `internal/web/dashboard.go`, `web/templates/partials_dashboard.html` (`app_tile`), `web/static/js/dashboard.js`, `web/static/css/plancia.css`
- Test: `internal/web/stats_test.go`

**Interfaces:**
- Consumes: Task 3 (`AddAppClick`, `AppClicksSince`).
- Produces:
  - `func (s *Server) handleAppClick(w, r)`: sempre 200. Conta solo se valgono tutte queste condizioni:
    - `StatsEnabled`;
    - l'utente è riconosciuto (`viewerName`);
    - l'`id` è un intero;
    - l'app è visibile in plancia per l'utente: è nel `GetDashboard` filtrato da `contentFilterFor` **senza** «Mostra tutto», quindi conta il `visible` vero e non `keep`;
    - la chiave `username|id|giorno` non è ancora nell'insieme in memoria `clickSeen` (mutex). L'insieme si svuota quando cambia il giorno.
  - `func (s *Server) markHot(d *database.Dashboard)`: con la cache di 10 min su `AppClicksSince(oggi-6 giorni)`, le prime 3 app **presenti in `d`** con ≥ 5 clic prendono `Hot = true`. A parità di clic vince l'ID più basso.

Template `app_tile`: con `.Hot`, dentro `.tile-card` va `<span class="tile-hot" title="Tra i più aperti questa settimana"><span class="material-icons" aria-hidden="true">local_fire_department</span><span class="visually-hidden">Tra i più aperti questa settimana</span></span>`. Va posizionata in alto a destra, in arancio tenue, senza coprire il badge delle guide. `data-app="{{.ID}}"` va solo su `a.tile-main`: i link delle guide e dell'assistenza non contano.

JS: nel listener dei clic, `e.target.closest("a.tile-main[data-app]")` → `beacon("/app/" + id + "/clic", {})`. Il clic con il tasto centrale (`auxclick`) conta allo stesso modo.

- [ ] **Step 1: Write the failing tests**:
  - `TestAppClickCountsOncePerDay`: mrossi clicca 3 volte → `clicks == 1`; anna clicca 1 volta → `2`.
  - `TestAppClickAnonymousIgnored`: anonimo o senza cookie → 200 e nessuna riga.
  - `TestAppClickUnknownApp`: `/app/99999/clic` e `/app/abc/clic` → 200, nessuna riga, nessun panic.
  - `TestAppClickReservedApp`: un'app riservata a un gruppo di cui mrossi non fa parte → non contata.
  - `TestHotTiles`: 4 app con 9, 7, 5 e 4 clic negli ultimi 7 giorni → `tile-hot` sulle prime tre, non sulla quarta. Un'app disabilitata con 20 clic non compare.
- [ ] **Step 2:** FAIL → **Step 3:** implementare → **Step 4:** in container `-run 'AppClick|HotTiles'` PASS, controllo di sintassi JS ok.
- [ ] **Step 5:** Commit `Plancia: fiammella sugli applicativi più aperti`.

### Task 11: Widget «Il tuo mese»

**Files:**
- Modify: `internal/web/stats.go`, `internal/web/dashboard.go` (`dashboardView.Month *monthView`), `web/templates/partials_dashboard.html` (nuovo `widget_mese`), `web/templates/dashboard.html` (subito dopo `widget_assist`), `web/static/js/notifiche.js`, `web/static/js/dashboard.js`, `web/static/css/plancia.css`
- Test: `internal/web/stats_test.go`

**Interfaces:**
- Consumes: Task 2 (`UserDaysBetween`), Task 3 (`HasPushSubscription`, `TicketsOpenedBetween`, `HasOpenedTicket`, `ReadAlertIDs`), Task 4 (`ClosedInMonth`, `GetTicketStats`), `ListPresence`/presenza dell'utente, `ListActiveAlerts`.
- Produces:
  - `type badgeView struct { Key, Icon, Label, Hint string; Earned bool }`. Key è uno tra `notifiche`, `app`, `avvisi`, `ticket`.
  - `type monthView struct { MonthName string; Days int; Sent, Resolved int; Tickets bool; Badges []badgeView; Assist *database.TicketStats }`
  - `func (s *Server) monthFor(username string, f *contentFilter) (*monthView, error)`: restituisce nil se anonimo o con `!StatsEnabled`.
- Regole delle spille (dalla spec):
  - **notifiche**: guadagnata se c'è una `HasPushSubscription` oppure se la presenza ha `Permission == "granted"`. Hint: «Attiva le notifiche per ricevere gli avvisi urgenti».
  - **app**: guadagnata con `LastApp != nil`. Hint: «Installa il cruscotto come app dal menu del browser (⋯ → App → Installa)».
  - **avvisi**: si considerano gli avvisi attivi, con `StartsAt` nel mese corrente e visibili all'utente (`f.visible`, non `keep`). Senza avvisi la spilla non si mostra; altrimenti è guadagnata se sono tutti in `ReadAlertIDs`. Hint: «Leggi tutti gli avvisi del mese».
  - **ticket**: solo se `ticketsEnabled()`; guadagnata con `HasOpenedTicket`. Hint: «Quando ti serve aiuto, apri una richiesta dal riquadro Assistenza».
- Nella riga «Le tue richieste»:
  - `Sent = len(TicketsOpenedBetween(mese))`;
  - `Resolved = ClosedInMonth(mese, quegli ID)`.
- `Assist` = `GetTicketStats(mese)`, solo con `ticketsEnabled()`.

Copy del template (testi esatti):
- titolo `Il tuo {{.MonthName}}` (ad esempio «Il tuo ottobre»; mese minuscolo da `months` in `dashboard.go`);
- numero grande `.Days` con `giorno di cruscotto` o `giorni di cruscotto`;
- con `Tickets` e `Sent > 0`: `{{.Sent}} richieste inviate, {{.Resolved}} risolte questo mese` (con 1: `1 richiesta inviata, … risolta/risolte`);
- con `Tickets` e `Sent == 0`: `Hai bisogno di aiuto?` + `<button type="button" class="link-btn" data-ticket-open>Apri un ticket</button>` (`ticket.js` ha già un listener delegato su `[data-ticket-open]`);
- spille: `<ul class="badges">`, un `<li>` per spilla con `<button type="button" class="badge{{if .Earned}} earned{{end}}" data-badge="{{.Key}}" aria-describedby="badge-hint">` e l'icona Material:
  - notifiche: `notifications_active`;
  - app: `install_desktop`;
  - avvisi: `task_alt`;
  - ticket: `confirmation_number`.

  Il testo accessibile è `Label` (+ « (da ottenere)» se non è stata presa). Sotto le spille va `<p id="badge-hint" class="badge-hint" aria-live="polite"></p>`.
- riga assistenza, con `Assist`: `Assistenza a {{mese}}: {{Requests}} richieste, {{Resolved}} risolte`; con `MedianFirst`: ` · prima risposta di solito entro {{durata}}`. Funzione di template `roughDuration(d)`:
  - sotto 1 h → «N minuti» (minimo 1; con 1, «1 minuto»);
  - fino a 48 h → «N ore» (con 1, «1 ora»), arrotondato;
  - oltre → «N giorni».

JS:
- `dashboard.js`: clic o focus su `.badge` → mette `Hint` (da `data-hint`) in `#badge-hint`.
- `notifiche.js`: il listener del bottone `offBtn` vale anche per `[data-badge="notifiche"]:not(.earned)`. Si passa a un listener delegato su `document` per i selettori `[data-notify-off], [data-badge="notifiche"]:not(.earned)`.

- [ ] **Step 1: Write the failing tests**:
  - `TestMonthWidget`: mrossi con 3 giorni in ottobre e 1 a settembre (`RecordDay` diretti) → `giorni di cruscotto` con `3`, titolo `Il tuo ottobre`.
  - `TestMonthWidgetAnonymous`: nessun `widget-mese` per anonimi né senza cookie.
  - `TestBadges`:
    - con permesso `granted` e nessuna iscrizione → notifiche `earned`;
    - con `SetPresenceClient(app=true)` → app `earned`;
    - nessun avviso del mese → nessun `data-badge="avvisi"`;
    - un avviso del mese non letto → presente e non `earned`;
    - dopo la lettura → `earned`;
    - senza OTRS → nessun `data-badge="ticket"`.
  - `TestMonthTickets`: con un mock OTRS, 2 `tickets_sent` di mrossi in ottobre, 1 in `SetClosedMonth("2026-10")` e 1 `kind = 'risposta'` → `2 richieste inviate, 1 risolta questo mese`.
  - `TestAssistLine`: con `SaveTicketStats{Requests 64, Resolved 58, MedianFirst 2h}` → `Assistenza a ottobre: 64 richieste, 58 risolte · prima risposta di solito entro 2 ore`.
  - `TestRoughDuration`, tabella: 30s→`1 minuto`, 45m→`45 minuti`, 1h→`1 ora`, 90m→`2 ore`, 47h→`47 ore`, 72h→`3 giorni`.
  - `TestStatsDisabledNoWidget`.
- [ ] **Step 2:** FAIL → **Step 3:** implementare (Go, template, CSS nello stile pastello della plancia, JS) → **Step 4:** in container `-run 'Month|Badges|Assist|RoughDuration|StatsDisabled'` PASS, controllo di sintassi JS ok su entrambi i file.
- [ ] **Step 5:** Commit `Plancia: widget «Il tuo mese» con spille e assistenza`.

### Task 12: Pulizia, documentazione e verifica finale

**Files:**
- Modify: `internal/web/presence.go` (`StartPresenceCleanup`, `CleanupPresence`), `CLAUDE.md`, `README.md` (sezione funzionalità e variabili d'ambiente). `publiccode.yml` non si tocca: si aggiorna al rilascio.
- Test: `internal/web/presence_test.go`

**Interfaces:**
- Consumes: Task 2 (`CleanupStats`, `DeleteUserDays`).

Comportamento:
- Nel giro giornaliero, `CleanupStats(dayOf(now - StatsRetentionMonths mesi), dayOf(now - 90 giorni))`.
- In `CleanupPresence`, quando AD non conosce più un utente, si chiama anche `DeleteUserDays`.

- [ ] **Step 1: Write the failing test** `TestCleanupRemovesUserDays`: con la directory mock che dà `ErrUnknownUser` per «ex», i suoi `user_days` spariscono. Con `StatsRetentionMonths=1`, un giorno di 2 mesi fa sparisce e uno di ieri resta.
- [ ] **Step 2:** FAIL → **Step 3:** implementare → **Step 4:** PASS.
- [ ] **Step 5:** Aggiornare `CLAUDE.md`:
  - Architettura: tabelle v14, `internal/web/stats.go`, `ticket_stats.go` e job orario;
  - route pubbliche: `/app/{id}/clic`;
  - nuove regole di lettura (`plancia`, «Ho letto» dopo 6 s, `data-letto`);
  - env `STATS_*`.

  Aggiornare `README.md` con la funzionalità e le variabili.
- [ ] **Step 6: Verifica completa**:
  - `go build ./... && go vet ./...` → nessun output;
  - suite intera in container (`go test ./...`) → tutti `ok`;
  - `docker compose up -d --build` e `docker compose restart cruscottopa` con l'override di sviluppo, poi controllo a mano nel browser (utente mock riconosciuto, vedi CLAUDE.md, sezione Test): pastiglia, widget, «Ho letto» dopo 6 s, fiammella dopo i clic.
- [ ] **Step 7:** Commit `Statistiche: pulizia, documentazione`. Poi push del branch e PR, senza merge finché l'utente non lo chiede.
