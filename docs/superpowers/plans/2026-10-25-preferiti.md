# Preferiti e ordine personale — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Sezione «Preferiti» (6 posti: fissati + più aperti da 10 aperture) in cima alla plancia e modalità «Personalizza» per riordinare preferiti, applicativi nelle categorie e categorie, tutto salvato nel DB per utente.

**Architecture:** Migrazione v15 con quattro tabelle per utente; logica pura in `internal/web/layout.go` (fusione degli ordini, ordinamento, calcolo dei preferiti) applicata in `handleDashboard` dopo il filtro per gruppi; endpoint POST sempre 200 + `{"ok"}`; nuovo `web/static/js/personalizza.js` con trascinamento a Pointer Events (mouse e touch) e tastiera.

**Tech Stack:** Go, `modernc.org/sqlite`, `html/template`, HTMX 2, JS vanilla, CSS in `plancia.css`.

**Spec:** `docs/superpowers/specs/2026-10-25-preferiti-design.md`

## Global Constraints

- Preferiti al massimo **6**; automatici solo con **almeno 10 aperture**; due aperture dello stesso applicativo a **meno di 30 secondi** contano come una.
- Conteggio: solo `username`, `app_id`, `opens`. **Nessuna data o orario**; mai visibile all'admin.
- Solo utenti riconosciuti (`viewerName` non vuoto); anonimi: niente sezione, niente scritture.
- Applicativi considerati: solo quelli **destinati all'utente** (`contentFilter.visible`), mai quelli mostrati da «Mostra tutto».
- Endpoint **sempre 200** con `{"ok":true|false}` (`pushReply`); corpo form `id=…&id=…` come i beacon esistenti.
- CSP stretta: niente JS/CSS inline; stile coerente con la plancia (`.tile`, `tint`, icone Material self-hosted).
- Tremolio in modifica; con `prefers-reduced-motion: reduce` solo bordo.
- Migrazioni solo in coda (`migrateV15Layout`); username minuscolo (`strings.ToLower(strings.TrimSpace)`).
- Test di `internal/web` solo in container (vedi CLAUDE.md); JS: controllo sintassi con `node -e "new Function(...)"` e test Go che leggono il file.

## Review Focus

1. **Fissato che sparisce e ritorna** (app disattivata poi riattivata, o non più destinata): dopo un salvataggio fatto mentre era nascosto deve restare salvato e ricomparire in fondo, e con 7 fissati visibili se ne mostrano 6. Test in Task 2 (`TestMergeOrderKeepsHidden`) e Task 4 (`TestFavoritesCapAfterReturn`).
2. **«Mostra tutto» attivo**: un'app non destinata non entra nei preferiti, non si conta, non si salva negli ordini. Test in Task 3 (`TestAppOpenNotTargeted`) e Task 5 (`TestSaveDropsNotTargeted`).
3. **Doppio clic / clic ravvicinati**: entro 30 s un solo incremento, dopo 30 s un altro. Test in Task 3 (`TestAppOpenThrottle`).
4. **Refresh HTMX del carosello** (`/partials/alerts`): non tocca la sezione Preferiti né l'ordine (il carosello è un nodo separato). Test in Task 4 (`TestAlertsPartialNoFavorites`).
5. **App spostata dall'admin in un'altra categoria** dopo un riordino: compare in fondo alla nuova. Test in Task 1 (`TestUpdateAppCategoryClearsUserOrder`).

---

### Task 1: Migrazione v15 e funzioni del DB

**Files:**
- Modify: `internal/database/migrations.go` (aggiungere `migrateV15Layout` in coda)
- Create: `internal/database/layout.go`
- Modify: `internal/database/apps.go` (`updateApp`)
- Test: `internal/database/layout_test.go`

**Interfaces:**
- Produces:
  - `func (db *DB) AddAppOpen(username string, appID int64) error` (FK: app inesistente = errore)
  - `func (db *DB) AppOpens(username string) (map[int64]int, error)`
  - `type Layout struct { Favorites []int64; Categories map[int64]int; Apps map[int64]int }` — Favorites in ordine; mappe id → posizione
  - `func (db *DB) UserLayout(username string) (Layout, error)` (mappe mai nil)
  - `func (db *DB) SaveFavorites(username string, ids []int64) error` — sostituisce tutte le righe dell'utente
  - `func (db *DB) SaveCategoryOrder(username string, ids []int64) error` — sostituisce tutte le righe dell'utente
  - `func (db *DB) SaveAppOrder(username string, categoryID int64, ids []int64) error` — cancella le righe dell'utente per le app che **ora** stanno in `categoryID`, poi inserisce `ids` con posizione 0..n-1
  - `func (db *DB) ResetLayout(username string) error` — fissati e ordini, **non** i conteggi
  - `func (db *DB) DeleteUserLayout(username string) error` — tutto, conteggi compresi

- [ ] **Step 1: Write the failing tests** in `internal/database/layout_test.go`:
  - `TestAppOpens`: due `AddAppOpen("MRossi", app)` → `AppOpens("mrossi")[app] == 2`; `AddAppOpen` su id 9999 → errore.
  - `TestUserLayoutRoundTrip`: `SaveFavorites(u, [b, a])` → `Favorites == [b, a]`; `SaveCategoryOrder(u, [c2, c1])` → `Categories[c2]==0, [c1]==1`; `SaveAppOrder(u, c1, [a2, a1])` → `Apps[a2]==0, Apps[a1]==1`; nuovo `SaveFavorites(u, [a])` → `[a]`.
  - `TestSaveAppOrderOnlyThatCategory`: righe di app di un'altra categoria restano dopo `SaveAppOrder` sulla prima.
  - `TestResetLayoutKeepsOpens`: dopo `ResetLayout`, layout vuoto e `AppOpens` invariato; `DeleteUserLayout` → anche `AppOpens` vuoto.
  - `TestLayoutCascade`: `DeleteApp(a)` → sparisce da fissati, ordine e conteggi.
  - `TestUpdateAppCategoryClearsUserOrder`: con riga in `user_app_order` per `a`, `UpdateApp` di `a` con `CategoryID` diverso → `Apps` senza `a`; `UpdateApp` con la stessa categoria → riga invariata.

- [ ] **Step 2: Run** `go test ./internal/database/ -run 'AppOpens|UserLayout|SaveAppOrder|ResetLayout|LayoutCascade|UpdateAppCategory'` — Expected: FAIL (funzioni non definite).

- [ ] **Step 3: Implement.** `migrateV15Layout` crea le tabelle esattamente come nella spec (sezione «Dati»): `user_app_opens`, `user_favorites`, `user_category_order`, `user_app_order`, tutte con FK `ON DELETE CASCADE`, chiave `(username, id)`. Le funzioni `Save*` in una transazione (DSN già `_txlock=immediate`). In `updateApp`, prima dell'UPDATE: `DELETE FROM user_app_order WHERE app_id = ? AND (SELECT category_id FROM apps WHERE id = ?) <> ?` (stesso `execer`).

- [ ] **Step 4: Run** `go test ./internal/database/` — Expected: PASS (compresi i test esistenti: il seed non cambia).

- [ ] **Step 5: Commit** `git commit -m "Preferiti: tabelle v15 e funzioni del DB"`

---

### Task 2: Logica pura del layout

**Files:**
- Create: `internal/web/layout.go`
- Test: `internal/web/layout_test.go`

**Interfaces:**
- Consumes: `database.Layout`, `database.AppWithGuides`, `database.CategoryWithApps`
- Produces:
  - `const favMax = 6`, `const favMinOpens = 10`
  - `func mergeOrder(saved, sent []int64, visible map[int64]bool) []int64` — `sent` filtrato (solo `visible`, senza ripetizioni), poi gli elementi di `saved` **non** visibili, nel loro ordine
  - `func applyLayout(d *database.Dashboard, l database.Layout)` — riordina `d.Categories` per `l.Categories` e le app di ogni categoria per `l.Apps`: prima gli elementi con posizione (crescente), poi gli altri nell'ordine attuale (admin)
  - `type tileView struct { database.AppWithGuides; Prefix string; Fav, Pinned, Pinnable bool }`
  - `func favorites(d database.Dashboard, pinned []int64, opens map[int64]int) []tileView` — candidati = app di `d` con `!NotForViewer`, nell'ordine di pagina; prima i fissati visibili nell'ordine di `pinned` (al massimo 6), poi automatici con `opens >= 10` non fissati, per aperture decrescenti e a parità ordine di pagina, fino a 6. `Prefix = "fav-"`, `Fav = true`, `Pinned` per i fissati.

- [ ] **Step 1: Write the failing tests:**
  - `TestMergeOrderKeepsHidden`: `mergeOrder([1,2,3], [3,1,9,3], {1,3})` == `[3,1,2]` (9 non visibile scartato, 3 ripetuto una volta, 2 nascosto in fondo).
  - `TestApplyLayout`: categorie A,B (admin) con `Categories{B:0}` → B,A; app a1,a2,a3 in A con `Apps{a3:0,a1:1}` → a3,a1,a2.
  - `TestFavorites`: pinned `[x]`, opens `{y:12, z:10, w:9, x:50}` → `[x(pinned), y, z]` (w sotto soglia, x non ripetuto); parità `opens` → ordine di pagina.
  - `TestFavoritesCapAfterReturn`: 7 fissati visibili → 6, nell'ordine di `pinned`; app con `NotForViewer` mai presente anche se fissata o con 99 aperture.

- [ ] **Step 2: Run** (container) `go test ./internal/web/ -run 'MergeOrder|ApplyLayout|Favorites'` — Expected: FAIL (non definite).

- [ ] **Step 3: Implement** in `internal/web/layout.go` (ordinamento stabile con `sort.SliceStable`).

- [ ] **Step 4: Run** lo stesso comando — Expected: PASS.

- [ ] **Step 5: Commit** `git commit -m "Preferiti: logica di ordine e preferiti"`

---

### Task 3: Conteggio delle aperture (tile e ricerca)

**Files:**
- Modify: `internal/web/stats.go` (`handleAppClick`, nuova cache delle aperture)
- Modify: `internal/web/search_index.go` (`searchItem.ID`)
- Modify: `web/static/js/dashboard.js` (beacon anche dalla ricerca)
- Test: `internal/web/layout_opens_test.go`

**Interfaces:**
- Consumes: `db.AddAppOpen`, `appVisibleTo`
- Produces: `searchItem.ID int64 \`json:"id,omitempty"\`` valorizzato per `Kind == "app"`; in pagina le opzioni `app` della ricerca hanno `data-app`.

- [ ] **Step 1: Write the failing tests** (riusare `webApp`, `click`, helper di `hot_test.go`):
  - `TestAppOpenCounted`: `click(/app/{id}/clic, "mrossi")` → `AppOpens("mrossi")[id] == 1`; anonimo (`user == ""`) → nessuna riga.
  - `TestAppOpenThrottle`: due clic con lo stesso `fixedNow` → 1; poi `s.now = func() time.Time { return fixedNow.Add(31 * time.Second) }` e un altro clic → 2 (`s.now` è il campo orologio del `Server`, iniettato da `Options.Now`).
  - `TestAppOpenWithoutStats`: con `StatsEnabled = false` il conteggio personale funziona (1) e `app_clicks` resta 0.
  - `TestAppOpenNotTargeted`: app riservata a un gruppo di cui mrossi non fa parte, con cookie `cruscotto_tutto=1` → 0.
  - `TestSearchIndexAppID`: in `buildSearchIndex` la voce `app` ha `ID` = id dell'app; le voci guida/assistenza no.
  - `TestSearchBeaconJS`: `dashboard.js` contiene `a[data-app]` nel selettore del beacon e `dataset.app = ` nella costruzione delle opzioni.

- [ ] **Step 2: Run** (container) `go test ./internal/web/ -run 'AppOpen|SearchIndexAppID|SearchBeaconJS'` — Expected: FAIL.

- [ ] **Step 3: Implement.** `handleAppClick`: risponde 200; esce se utente vuoto o id non valido; calcola `openDue` (ultima apertura contata per `user|id` più vecchia di 30 s, mappa in memoria con mutex; potatura quando supera 10000 voci) e `clickDue` (logica attuale, solo con `StatsEnabled`); se nessuno dei due serve, esce **prima** di `appVisibleTo`; poi visibilità; poi `AddAppOpen` e/o `AddAppClick`. Ricerca: `searchItem.ID`; in `dashboard.js` il selettore diventa `a[data-app]` e le opzioni `app` ricevono `a.dataset.app = it.id`.

- [ ] **Step 4: Run** lo stesso comando, poi `go test ./internal/web/ -run 'Click|Hot|Search'` — Expected: PASS (i test di fiammella e ricerca esistenti restano verdi). `node -e "new Function(require('fs').readFileSync('web/static/js/dashboard.js','utf8'))"` — Expected: nessun output.

- [ ] **Step 5: Commit** `git commit -m "Preferiti: conteggio delle aperture per utente"`

---

### Task 4: Plancia — ordine personale e sezione Preferiti

**Files:**
- Modify: `internal/database/dashboard.go` (`AppWithGuides.NotForViewer bool` — calcolato dal web)
- Modify: `internal/web/audience_filter.go` (`dashboard()` imposta `a.NotForViewer = !visible`)
- Modify: `internal/web/dashboard.go` (`dashboardView.Layout *layoutView`; in `handleDashboard`, dopo `markHot` e dopo `buildSearchIndex`: `UserLayout`, `applyLayout`, `AppOpens`, `favorites`)
- Modify: `internal/web/render.go` (func `tile(a AppWithGuides, pinnable bool) tileView`)
- Modify: `web/templates/partials_dashboard.html` (`app_tile` riceve `tileView`; nuovo `favorites`)
- Modify: `web/templates/dashboard.html` (sezione prima di `#alerts`; `data-` per categorie e griglie)
- Modify: `internal/web/server.go` (`GET /partials/preferiti`)
- Modify: `internal/web/presence.go` (`CleanupPresence` chiama `DeleteUserLayout`)
- Test: `internal/web/layout_page_test.go`

**Interfaces:**
- Consumes: Task 1 (`UserLayout`, `AppOpens`, `DeleteUserLayout`), Task 2 (`applyLayout`, `favorites`, `tileView`)
- Produces (markup che Task 6 usa):
  - `type layoutView struct { Favorites []tileView; Full bool }` — `Full` = 6 fissati visibili; `nil` per chi non è riconosciuto
  - `<section class="favorites" data-personalizza>` con titolo «Preferiti», pulsante `button[data-personalizza-toggle]` («Personalizza», icona `tune`), `button[data-personalizza-reset] hidden` («Ripristina», icona `restart_alt`), riga `p.personalizza-hint`, griglia `div.grid[data-sortable="preferiti"]`; la sezione ha classe `favorites-empty` se `Favorites` è vuoto (solo pulsante e, in modifica, la spiegazione)
  - riquadro: `div.tile[data-tile-id="{id}"]` con `data-pinned` sui fissati e `data-altri` sulle app `NotForViewer`; `id` interni con `Prefix` (`{{.Prefix}}guide-app-{{.ID}}`); puntina fuori modifica `span.tile-pin-mark` sui fissati dei preferiti; bottone `button.tile-pin[data-pin]` (`aria-pressed` = fissato) solo se `Pinnable` (utente riconosciuto e app destinata)
  - categorie: `section.apps[data-sortable="categorie"]`, `div.category[data-category][data-id="{id}"]`, `h2.section-title` con maniglia `span.drag-handle.material-icons` (`drag_indicator`), griglia `div.grid[data-sortable="categoria"][data-id="{id}"]`
  - `GET /partials/preferiti`: 200 con il solo template `favorites` (vuoto per chi non è riconosciuto)

- [ ] **Step 1: Write the failing tests:**
  - `TestFavoritesSection`: mrossi con 10 aperture su A (via `AddAppOpen`) e B fissata → la pagina ha `data-personalizza`, B prima di A dentro `data-sortable="preferiti"`, `data-pinned` su B, `id="fav-guide-app-` se A ha una guida; anonimo e senza cookie → nessun `data-personalizza`.
  - `TestFavoritesHiddenForNewUser`: mrossi senza aperture né fissati → `favorites-empty` presente (pulsante «Personalizza» visibile), nessun riquadro nei preferiti.
  - `TestUserOrderApplied`: `SaveCategoryOrder` e `SaveAppOrder` → l'ordine delle categorie e delle app in pagina segue l'utente; un altro utente vede l'ordine admin; l'indice di ricerca `#search-index` resta nell'ordine admin.
  - `TestAlertsPartialNoFavorites`: `/partials/alerts` non contiene `data-personalizza`.
  - `TestFavoritesNotTargetedWithShowAll`: app riservata ad altri, fissata in DB (insert diretto) e cookie `cruscotto_tutto=1` → non nei preferiti; nella categoria il riquadro ha `data-altri` e nessun `data-pin`.
  - `TestCleanupRemovesLayout`: utente sconosciuto ad AD dopo `CleanupPresence` → `AppOpens` e `UserLayout` vuoti (come `TestCleanupRemovesUserDays`).
  - `TestFavoritesPartial`: `GET /partials/preferiti` per mrossi contiene la griglia dei preferiti; per anonimo corpo senza `data-personalizza`, status 200.

- [ ] **Step 2: Run** (container) `go test ./internal/web/ -run 'Favorites|UserOrderApplied|AlertsPartialNoFavorites|CleanupRemovesLayout'` — Expected: FAIL.

- [ ] **Step 3: Implement** come da Interfaces. `handleFavoritesPartial` ricostruisce dashboard + filtro + layout come `handleDashboard` (estrarre un helper `layoutFor(r, d *database.Dashboard, f *contentFilter) *layoutView` usato da entrambi). I template esistenti che chiamano `app_tile` passano `(tile . $pinnable)`.

- [ ] **Step 4: Run** il comando dello Step 2 e poi tutta la suite in container — Expected: PASS (adattare solo i test che verificano markup ora cambiato, con un ledger `Ruling:`).

- [ ] **Step 5: Commit** `git commit -m "Preferiti: sezione in plancia e ordine personale"`

---

### Task 5: Endpoint di salvataggio

**Files:**
- Create: `internal/web/layout_handlers.go`
- Modify: `internal/web/server.go` (route)
- Test: `internal/web/layout_handlers_test.go`

**Interfaces:**
- Consumes: Task 1 (`Save*`, `ResetLayout`, `UserLayout`), Task 2 (`mergeOrder`, `favMax`)
- Produces:
  - `POST /preferiti` — `id` ripetuti, ordinati; salva `mergeOrder(saved, sent, visibili)` dove `sent` è troncato a 6 **dopo** il filtro
  - `POST /ordine/categorie` — visibili = categorie che hanno almeno un'app destinata all'utente
  - `POST /ordine/categorie/{id}` — visibili = app destinate all'utente che stanno in quella categoria; saved = ordine attuale dell'utente per quelle app
  - `POST /ordine/ripristina`
  - tutti: `pushReply(w, ok)`; utente non riconosciuto → `{"ok":false}` senza scritture
  - helper `func (s *Server) targetedApps(r *http.Request) (map[int64]int64, error)` — app destinate all'utente (indipendente da «Mostra tutto») → categoria

- [ ] **Step 1: Write the failing tests:**
  - `TestSaveFavorites`: POST `id=B&id=A` → `UserLayout.Favorites == [B, A]`, risposta `{"ok":true}`; 8 id → 6 salvati.
  - `TestSaveKeepsHiddenPin`: B fissata, poi B disattivata, POST `id=A` → `Favorites == [A, B]`.
  - `TestSaveDropsNotTargeted`: app riservata ad altri inviata (anche con `cruscotto_tutto=1`) → non salvata.
  - `TestSaveOrders`: `POST /ordine/categorie` e `/ordine/categorie/{id}` salvano; id di un'altra categoria nella seconda → scartato.
  - `TestResetLayoutEndpoint`: dopo `POST /ordine/ripristina` layout vuoto, aperture invariate.
  - `TestLayoutEndpointsAnonymous`: anonimo → 200 `{"ok":false}`, nessuna riga.
  - `TestLayoutEndpointsCSRF`: POST con `Sec-Fetch-Site: cross-site` respinto dalla catena esistente (nessuna scrittura).

- [ ] **Step 2: Run** (container) `go test ./internal/web/ -run 'SaveFavorites|SaveKeepsHiddenPin|SaveDropsNotTargeted|SaveOrders|ResetLayoutEndpoint|LayoutEndpoints'` — Expected: FAIL.

- [ ] **Step 3: Implement** in `layout_handlers.go`; id non numerici ignorati; massimo 200 id letti per richiesta.

- [ ] **Step 4: Run** lo stesso comando — Expected: PASS.

- [ ] **Step 5: Commit** `git commit -m "Preferiti: endpoint di salvataggio"`

---

### Task 6: Modalità «Personalizza» (JS e CSS)

**Files:**
- Create: `web/static/js/personalizza.js`
- Modify: `web/templates/dashboard.html` (script `defer`)
- Modify: `web/static/js/dashboard.js` (rotazione e apertura tile ferme con `body.personalizza`)
- Modify: `web/static/css/plancia.css`
- Test: `internal/web/personalizza_test.go`

**Interfaces:**
- Consumes: markup di Task 4, endpoint di Task 5, `GET /partials/preferiti`
- Produces: comportamento della spec, sezione «Modalità Personalizza»

- [ ] **Step 1: Write the failing tests** (leggono i file, come `TestPresenceAndReadJS`):
  - `TestPersonalizzaJS`: `personalizza.js` contiene `pointerdown`, `setPointerCapture`, `300` (pressione lunga), `sessionStorage`, `"/preferiti"`, `"/ordine/categorie"`, `"/ordine/ripristina"`, `"/partials/preferiti"`, `ArrowLeft`, `aria-live` o `role="status"`, `Escape`; e non contiene `innerHTML =` con dati del server se non dalla risposta di `/partials/preferiti`.
  - `TestPersonalizzaCSS`: `plancia.css` contiene `body.personalizza`, un `@keyframes` del tremolio e, dentro `@media (prefers-reduced-motion: reduce)`, `animation: none` per i riquadri in modifica.
  - `TestDashboardJSRespectsEdit`: `dashboard.js` contiene `classList.contains("personalizza")`.
  - `TestPersonalizzaScriptIncluded`: la plancia di mrossi include `/static/js/personalizza.js`.

- [ ] **Step 2: Run** (container) `go test ./internal/web/ -run 'Personalizza|DashboardJSRespectsEdit'` — Expected: FAIL.

- [ ] **Step 3: Implement** `personalizza.js`:
  - toggle `body.personalizza`; «Fatto»/«Personalizza» e `hidden` su «Ripristina» (conferma con `confirm`); Esc esce; all'avvio, se `sessionStorage` ha il segno, rientra in modifica e lo toglie.
  - in modifica: listener di `click` in **cattura su `window`** che blocca l'apertura dei riquadri e dei badge (niente beacon); `[data-altri]` nascosti; titoli delle categorie `tabindex=0`.
  - trascinamento a Pointer Events dentro lo stesso `[data-sortable]` (mai tra contenitori); mouse: parte subito; touch/pen: dopo 300 ms fermi (movimento > 8 px prima = scorrimento, si annulla), poi `touchmove` `{passive:false}` con `preventDefault` finché si trascina; segnaposto mentre si trascina; rilascio → salvataggio del contenitore.
  - tastiera: con il focus su un riquadro o titolo di categoria, frecce = posizione precedente/successiva, poi salvataggio e annuncio («Spostato in posizione 3 di 8») in un `div[role=status]`.
  - preferiti: salvataggio = elenco dei fissati nell'ordine del DOM, più l'automatico appena trascinato (diventa fissato); puntina `[data-pin]` (anche sulle categorie): aggiunge in fondo ai fissati o toglie; con 6 fissati la puntina «Fissa» è disattivata con `title="Sblocca prima un preferito"`; dopo ogni salvataggio dei preferiti, sostituzione della sezione con `GET /partials/preferiti` restando in modifica.
  - salvataggio: `fetch` POST `application/x-www-form-urlencoded`, `credentials: "same-origin"`; risposta non ok o non `{"ok":true}` → messaggio generico, segno in `sessionStorage`, `location.reload()`.
  - `dashboard.js`: `tick()` del carosello e apertura delle tile escono se `document.body.classList.contains("personalizza")`.
  - CSS: in modifica bordo azzurro (`box-shadow`), maniglie visibili, tremolio leggero (es. rotazione ±0,4° in 0,3 s alternato), segnaposto; `@media (prefers-reduced-motion: reduce)` senza animazione; colori e raggi dei riquadri esistenti.

- [ ] **Step 4: Run** il comando dello Step 2 — Expected: PASS. `node -e "new Function(require('fs').readFileSync('web/static/js/personalizza.js','utf8'))"` e lo stesso per `dashboard.js` — Expected: nessun output.

- [ ] **Step 5: Commit** `git commit -m "Preferiti: modalità Personalizza"`

---

### Task 7: Documentazione e prova nel browser

**Files:**
- Modify: `CLAUDE.md` (tabelle v15, bullet «Preferiti e ordine personale», route pubbliche `/preferiti`, `/ordine/*`, `/partials/preferiti`)
- Modify: `README.md` (paragrafo «Preferiti»)

- [ ] **Step 1: Docs** come sopra, stile dei bullet esistenti.
- [ ] **Step 2: Suite completa** in container `go test ./...` — Expected: tutti `ok`.
- [ ] **Step 3: Prova nel browser** (Playwright in container contro mock con `NTLM_DOMAIN=MOCK`, cookie di mrossi come da CLAUDE.md): «Personalizza» → tremolio visibile; trascinamento di un'app in una categoria e di una categoria, ricarica → ordine mantenuto; puntina su un'app di categoria → compare nei preferiti; con viewport touch (`hasTouch`, `isMobile`) pressione lunga e trascinamento; `emulateMedia({ reducedMotion: "reduce" })` → nessuna animazione sui riquadri; clic su un riquadro fuori modifica apre e conta. Nessun errore in console oltre al 401 atteso di `/io`.
- [ ] **Step 4: Commit** `git commit -m "Preferiti: documentazione"`
