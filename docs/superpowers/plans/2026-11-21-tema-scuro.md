# Tema scuro automatico Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** plancia, pagine secondarie, admin, login ed editor seguono `prefers-color-scheme` del sistema.

**Architecture:** tutti i colori diventano token CSS dentro `:root`, ridefiniti in un blocco `@media (prefers-color-scheme: dark) { :root { … } }`. Le tile ricevono il colore dell'app come `--c` e il pastello lo calcola `color-mix()`. Un test Go legge i CSS e rifiuta i colori fissi fuori dai token.

**Tech Stack:** CSS (custom properties, `color-mix`, `color-scheme`), template Go `html/template`, test Go, Playwright in container per gli screenshot.

**Spec:** `docs/superpowers/specs/2026-11-21-tema-scuro-design.md`

## Global Constraints

- Nessun interruttore, nessuna preferenza salvata: solo `prefers-color-scheme`.
- CSP invariata: niente `<style>` inline. Lo `style=""` sulle tile resta ammesso (`style-src-attr 'unsafe-inline'`).
- Contrasto testo WCAG AA (4,5:1 normale, 3:1 per testo grande e icone) in entrambi i temi.
- `icon_bg` vuoto = riquadro icona bianco anche al buio. Logo dell'ente sempre su fondo chiaro. Immagini Markdown e SVG delle guide invariati.
- `:root { color-scheme: light dark; }` in `app.css`.
- Test di `internal/web` solo in container (vedi CLAUDE.md, sezione Test).
- Ordine dei colori nel tema chiaro: **invariato**. Il refactor in token non deve cambiare l'aspetto chiaro (confronto con screenshot "prima").

## Review Focus

1. Logo dell'ente caricato con testo scuro: dove appare su un fondo che al buio diventa scuro, deve stare su un riquadro chiaro (`--logo-bg`). Va visto negli screenshot di plancia, admin Ente e login.
2. Markdown al buio (tabelle con bordi, `code`/`pre`, citazioni, link) in `/guide/{id}`, `/avvisi` e nell'anteprima dell'admin: leggibile, nessun fondo bianco residuo. Nello screenshot va usato un avviso con tabella e codice.
3. `dialog.urgent`, `dialog.ticket`, `dialog.notify-ask`/`notify-blocked`, il loro `::backdrop` e i campi dei form dentro i dialog: fondo scuro, campi scuri (`color-scheme`), testo leggibile.
4. Focus visibile e stati di `body.personalizza` (contorni, puntina, `[role=status]`) al buio: l'anello di focus non deve sparire sul fondo scuro.
5. Messaggi di errore e successo dell'admin (`.err`, flash, `X-Esito: non-valido`) e pillole urgente/manutenzione/novità: le varianti scure mantengono il significato (il rosso resta rosso).

Ognuna ha una voce nello script Playwright del Task 6.

---

### Task 1: Tile pastello con `color-mix`

**Files:**
- Modify: `internal/web/render.go:50,187-199` (`tint` → `tileColor`)
- Modify: `web/templates/partials_dashboard.html:77`
- Modify: `web/static/css/plancia.css` (regola `.tile`)
- Test: `internal/web/render_test.go` (`TestTint` → `TestTileColor`), `internal/web/dashboard_test.go:54`

**Interfaces:**
- Produces: funzione di template `tileColor(hex string) string` = l'hex se `colorRe` lo accetta, altrimenti `"#64748b"`. Token CSS `--tile-base` (definito nel Task 3).

- [ ] **Step 1: Test falliti.** `TestTileColor`: `"#2563eb"→"#2563eb"`, `"#FFFFFF"→"#FFFFFF"`, `"red"→"#64748b"`, `""→"#64748b"`, `"#12345"→"#64748b"`. In `dashboard_test.go:54` l'atteso diventa `style="--c:#2563eb"`. Nuovo `TestTileTintCSS`: `plancia.css` contiene `color-mix(in srgb, var(--c) 15%, var(--tile-base))`.
- [ ] **Step 2: Esecuzione dei test in container.** Atteso: FAIL (`tileColor` non definita).
- [ ] **Step 3: Implementazione.** `tileColor` sostituisce `tint` (rimossa, con l'import `math` se resta inutilizzato). Il template scrive `style="--c:{{tileColor .IconColor}}"`. `.tile { background: color-mix(in srgb, var(--c) 15%, var(--tile-base)); }`. Finché il Task 3 non lo definisce, mettere `--tile-base: #fff` nel `:root` di `plancia.css`.
- [ ] **Step 4: Nuova esecuzione dei test in container.** Atteso: PASS.
- [ ] **Step 5: Commit** `Tile: pastello calcolato in CSS con color-mix`.

### Task 2: Guardia sui colori fissi + `app.css` e `admin.css` in token con blocco scuro

**Files:**
- Create: `internal/web/theme_test.go`
- Modify: `web/static/css/app.css`, `web/static/css/admin.css`

**Interfaces:**
- Produces: helper di test `cssStrayColors(css string) []string`, che restituisce le dichiarazioni con colori fuori dai blocchi `:root` (anche quello dentro `@media (prefers-color-scheme: dark)`). Marcatore di eccezione: dichiarazione seguita da `/* fisso: <motivo> */` sulla stessa riga. Token condivisi (in `app.css`, usati da admin e plancia): quelli esistenti più `--surface-2` (fondo campi e righe alternate), `--on-color: #fff` (testo su fondo colorato), `--focus`, `--shadow`, `--logo-bg`.

- [ ] **Step 1: Test falliti** in `theme_test.go`:
  - `TestCSSStrayColorsHelper`: `":root{--a:#fff} .x{color:#000}"` → 1 risultato; `".x{color:#000} /* fisso: logo */"` → 0; `"@media (prefers-color-scheme: dark){:root{--a:#111}}"` → 0; `".x{background:rgba(0,0,0,.2)}"` → 1; `".x{color:white}"` → 1; `".x{color:transparent}"` e `currentColor` → 0. Colori cercati: `#[0-9a-fA-F]{3,8}\b`, `rgba?(`, `hsla?(`, parole `white`/`black`.
  - `TestThemeCSS` (tabella su `app.css`, `admin.css`): `cssStrayColors` vuoto; il file contiene `@media (prefers-color-scheme: dark)`. Solo `app.css` contiene anche `color-scheme: light dark`.
- [ ] **Step 2: Esecuzione dei test in container.** Atteso: FAIL con l'elenco dei colori fissi di `app.css`/`admin.css`.
- [ ] **Step 3: Implementazione.** Colori fissi di `app.css` (es. `.app-icon` `#fff`, `#475569`) e dei 14 di `admin.css` → token. Blocco scuro in `app.css` che ridefinisce tutti i token condivisi (palette: fondo ~`#0f1420`, card ~`#171e2c`, linee ~`#2a3446`, testo ~`#e6eaf2`, muted ~`#9aa6ba`; stati con fondo scuro tinto e testo chiaro tinto). Rail admin `--navy*`: più scuro di poco, resta distinguibile dal fondo. Contrasti verificati con un calcolatore (annotare nel commit i rapporti di testo/fondo, muted/fondo, accent/card).
- [ ] **Step 4: Nuova esecuzione dei test in container.** Atteso: PASS. `go vet ./...` pulito.
- [ ] **Step 5: Commit** `Tema scuro: token e blocco scuro per app.css e admin.css`.

### Task 3: `plancia.css` in token con blocco scuro

**Files:**
- Modify: `web/static/css/plancia.css`
- Test: `internal/web/theme_test.go` (aggiungere `plancia.css` alla tabella di `TestThemeCSS`)

**Interfaces:**
- Consumes: `cssStrayColors`, token condivisi del Task 2, `--tile-base` del Task 1.
- Produces: token `--p-*` completi (esistenti più quelli nuovi per i circa 104 colori fissi: superficie card, superficie in rilievo, bordi, ombre, banda blu, testata, carosello, dialog, `--tile-base`), tutti ridefiniti in `@media (prefers-color-scheme: dark)`.

- [ ] **Step 1: Test fallito.** `plancia.css` aggiunto alla tabella di `TestThemeCSS`.
- [ ] **Step 2: Esecuzione dei test in container.** Atteso: FAIL con circa 104 dichiarazioni.
- [ ] **Step 3: Implementazione.** Ogni colore fisso diventa un token: valori chiari identici a oggi, colori ripetuti raggruppati in un solo token. Blocco scuro: `--tile-base` = superficie scura, così le tile al buio diventano fondi scuri tinti dal colore dell'app. Banda blu e testata con blu più profondi (~`#0b2a5c`/`#103a7a`). Ombre più leggere o un bordo `--p-line`. `::backdrop` dei dialog più scuro. Unica eccezione ammessa: `.tile-icon` senza `icon_bg` resta `#fff /* fisso: loghi pensati per il bianco */`. Avviso «Notifiche disattivate»: rosso ben visibile sul fondo scuro.
- [ ] **Step 4: Nuova esecuzione dei test in container.** Atteso: PASS, anche i test CSS esistenti (`TestCarouselStableHeightCSS`, `personalizza_test`, `plancia_layout_test`).
- [ ] **Step 5: Commit** `Tema scuro: plancia.css in token con blocco scuro`.

### Task 4: Template: `theme-color`, logo CruscottoPA, logo dell'ente

**Files:**
- Modify: `web/templates/dashboard.html:7` (e ogni altro template con `theme-color`: cercare con grep)
- Modify: footer in `dashboard.html:109`, `avvisi.html:15`, `avviso.html:15`, `guida.html:23` e le altre pagine con `logo-on-light.svg` (grep); `admin_login.html:13`
- Modify: `web/templates/partials_brand.html` (`ente_logo`) se serve un contenitore per `--logo-bg`
- Test: `internal/web/theme_test.go`

**Interfaces:**
- Produces: template `logo_light_dark` in `partials_brand.html` (argomento: classe CSS dell'`<img>`), che scrive `<picture><source srcset="/static/img/logo-on-dark.svg" media="(prefers-color-scheme: dark)"><img class="…" src="/static/img/logo-on-light.svg" alt=""></picture>`.

- [ ] **Step 1: Test falliti.** `TestThemeTemplates`: `GET /` contiene `<meta name="theme-color" content="#1565c0" media="(prefers-color-scheme: light)">` e un secondo `theme-color` con `media="(prefers-color-scheme: dark)"`. `GET /`, `/avvisi` e `/admin/login` contengono `srcset="/static/img/logo-on-dark.svg" media="(prefers-color-scheme: dark)"`. Nessun template sotto `web/templates` contiene `logo-on-light.svg` fuori da `partials_brand.html` (test che legge i file).
- [ ] **Step 2: Esecuzione dei test in container.** Atteso: FAIL.
- [ ] **Step 3: Implementazione.** `theme-color` scuro uguale al blu della testata scura del Task 3. I footer e il login usano `logo_light_dark`. Logo dell'ente: dove appare su un fondo che al buio diventa scuro (login, admin Ente, footer se presente), riquadro con `background: var(--logo-bg)`, padding e raggio piccoli. Sulla testata blu resta com'è oggi.
- [ ] **Step 4: Nuova esecuzione dei test in container.** Atteso: PASS.
- [ ] **Step 5: Commit** `Tema scuro: theme-color, logo e logo dell'ente`.

### Task 5: Editor TipTap

**Files:**
- Modify: `web/editor/src/editor.css`
- Test: `internal/web/theme_test.go`

**Interfaces:**
- Consumes: token di `app.css` (l'editor è caricato solo nell'admin, dopo `app.css`/`admin.css`).

- [ ] **Step 1: Test fallito.** Aggiungere `../../web/editor/src/editor.css` alla tabella di `TestThemeCSS`, senza il requisito del blocco `@media` per questo file (usa solo i token).
- [ ] **Step 2: Esecuzione dei test in container.** Atteso: FAIL con circa 11 colori.
- [ ] **Step 3: Implementazione.** Colori → `var(--card)`, `var(--line)`, `var(--ink)`, `var(--surface-2)`, `var(--accent)`… Ricostruire il bundle: `MSYS_NO_PATHCONV=1 sh scripts/editor.sh`.
- [ ] **Step 4: Nuova esecuzione dei test in container.** Atteso: PASS. Verificare anche `docker build --target editor .`.
- [ ] **Step 5: Commit** `Tema scuro: editor dell'admin sui token`.

### Task 6: Verifica visiva, documentazione, versione

**Files:**
- Create: `C:\Users\MIRKO~1.DAD\AppData\Local\Temp\claude\236248dc89a1ebfe\ed1ae9ed-362d-4d1f-a0d3-dbf908570b55\scratchpad\e2e\tema.mjs` (non va nel repo)
- Modify: `CLAUDE.md` (Architettura, riga CSS della plancia: token e blocco scuro, `cssStrayColors`, marcatore `/* fisso: … */`, `tileColor` al posto di `tint`), `publiccode.yml` (versione 0.21.0, `releaseDate`)

- [ ] **Step 1: Server mock con dati.** Server mock con dati di prova (CLAUDE.md, Test: sonda `cmd/cookieprobe` per l'utente riconosciuto, app con URL e colori diversi, una con `icon_bg`, un avviso urgente, un avviso news con tabella e blocco di codice, una guida Markdown, ticket da `OTRS_URL=mock`).
- [ ] **Step 2: Screenshot.** Script Playwright con `colorScheme: 'dark'` e poi `'light'`, a 1440×900 e 390×844, su: `/`, `/` con `body.personalizza`, `dialog.urgent` aperto, `dialog.ticket` aperto, `/avvisi`, `/guide/{id}`, `/ticket`, `/ticket/{id}`, `/admin/login`, `/admin` (avvisi, app, ente, gruppi), editor con anteprima. Un'altra passata con il focus da tastiera su una tile (Review Focus 4).
- [ ] **Step 3: Revisione degli screenshot.** Controllare a vista i punti della Review Focus 1–5 e confrontare il tema chiaro con quello di `main` (nessuna differenza attesa). Correggere i token e ripetere.
- [ ] **Step 4: Test completi in container, `go vet ./...`, `go build ./...`.** Atteso: tutto PASS.
- [ ] **Step 5: Commit** `Tema scuro: documentazione e versione 0.21.0`, poi push del branch e PR (rilascio secondo CLAUDE.md).
