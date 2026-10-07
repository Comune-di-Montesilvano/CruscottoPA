# Guide ricche e avvisi in Markdown con editor visuale

Data: 2026-10-07 · Stato: approvata · Sotto-progetto 2

## Contesto e obiettivo

Oggi le guide sono solo link (`kind = 'link'`, il campo `body` esiste ma non è usato) e il corpo degli avvisi è testo semplice reso con `paragraphs`. Chi scrive (redattori non tecnici) non può formattare, inserire immagini o allegare un manuale.

**Obiettivo:**
- guide di quattro tipi: **link** (come oggi), **Markdown interno**, **PDF caricato**, **file Markdown da GitHub**;
- corpo degli **avvisi in Markdown**;
- un **editor visuale** nell'admin (come un word processor: l'utente non vede mai la sintassi Markdown) per avvisi e guide, con immagini caricate dall'editor.

## Decisioni

- **Formato salvato: Markdown.** Un solo renderer lato server (goldmark) per anteprima admin, plancia, guide GitHub e avvisi: quello che si vede in anteprima è quello che si vede in plancia.
- **Editor: TipTap** (ProseMirror) + `tiptap-markdown`, impacchettato con esbuild in un unico file. Scartati: Toast UI Editor (repository archiviato, ultima release 2023-02), Quill (salva HTML, non Markdown), textarea con toolbar (l'utente vede i simboli).
- **Node solo in container:** il bundle dell'editor si costruisce in uno stage del Dockerfile (`node:24-alpine`, pnpm 12 via corepack). Non è versionato; in sviluppo lo produce `scripts/editor.sh` con lo stesso container. Senza bundle l'admin ripiega sulla textarea.
- **Immagini** caricate dall'editor (bottone, incolla, trascina), salvate in `UPLOAD_DIR/guide/`.
- **GitHub:** URL di un singolo file `.md` di un repository **pubblico**; copia in cache nel DB, aggiornata periodicamente e a mano.
- **Avvisi in plancia:** il carosello mostra un estratto in testo semplice (~200 caratteri) con "Leggi tutto" → `/avvisi/{id}`; il Markdown completo è reso nella pagina dell'avviso, in `/avvisi` e nel popup urgente.
- **Versione:** v0.7.0.

## 1. Renderer — `internal/markdown`

Pacchetto puro, senza dipendenze dal web.

- `goldmark` con estensioni GFM (tabelle, barrato, autolink, elenchi di attività) e **hard wraps** (un a capo singolo = `<br>`: gli avvisi esistenti in testo semplice si vedono come oggi, nessuna migrazione dei dati).
- **HTML grezzo disattivato** (default di goldmark, nessun `WithUnsafe`): tag scritti nel testo non passano.
- **URL ammessi** in link e immagini: `http`, `https`, `mailto` (solo link), relativi. Tutto il resto (`javascript:`, `data:`, `vbscript:`, …) viene scartato: link resi come testo, immagini omesse. Controllo in un AST transformer nostro, non affidato al solo default.
- Link esterni (host diverso dal sito) con `target="_blank" rel="noopener noreferrer"`.
- Titoli: `#` del Markdown diventa `<h2>` (il livello 1 è il titolo della pagina); `##` → `<h3>` e così via, massimo `<h6>`.
- API:
  - `Render(src string, opt Options) template.HTML` — `Options{BaseURL, ImageBaseURL string}` per riscrivere link e immagini relativi (GitHub); vuote = relativi lasciati come sono (contenuti interni puntano a `/uploads/guide/...`).
  - `Plain(src string, n int) string` — testo semplice senza sintassi Markdown (titoli, enfasi, link → testo del link, immagini → testo alternativo), spazi compattati, troncato a `n` rune senza spezzare le parole, con `…`.
- Nessuna cache: i contenuti sono piccoli e il rendering è veloce. Se servisse, si aggiunge dopo.

## 2. Dati (migrazione v7)

```sql
ALTER TABLE guides ADD COLUMN file        TEXT NOT NULL DEFAULT ''; -- nome del PDF in UPLOAD_DIR/guide
ALTER TABLE guides ADD COLUMN source_url  TEXT NOT NULL DEFAULT ''; -- URL GitHub inserito dall'admin
ALTER TABLE guides ADD COLUMN fetched_at  TEXT;                     -- ultimo download riuscito
ALTER TABLE guides ADD COLUMN fetch_error TEXT NOT NULL DEFAULT ''; -- errore dell'ultimo tentativo, '' = ok
```

- `guides.kind` (validato in Go): `link`, `markdown`, `pdf`, `github`.
  - `link`: `url` obbligatorio (come oggi).
  - `markdown`: `body` obbligatorio.
  - `pdf`: `file` obbligatorio.
  - `github`: `source_url` obbligatorio; `body` = ultima copia scaricata.
- `alerts.body`: invariato, interpretato come Markdown.
- Nuove query: `GuidesToRefresh(olderThan)` (guide `github` abilitate con `fetched_at` NULL o più vecchio), `SetGuideFetched(id, body, at)`, `SetGuideFetchError(id, msg)`.

## 3. Editor nell'admin

### Sorgenti e build

```
web/editor/
  package.json        # packageManager: pnpm@12.x (con hash), engines.node >=24, script "build"
  pnpm-lock.yaml
  build.mjs           # esbuild: bundle iife minificato
  src/editor.js       # TipTap: StarterKit, Link, Image, Table*, Placeholder, tiptap-markdown
  src/editor.css
```

- Output: `web/static/vendor/editor.js` e `editor.css`, in `.gitignore`.
- `Dockerfile`: stage `FROM node:24-alpine@sha256:… AS editor` → `corepack enable && pnpm install --frozen-lockfile && pnpm build`; lo stage finale copia i due file in `web/static/vendor/`.
- `scripts/editor.sh`: `docker run --rm -v "$PWD/web/editor":/w -v "$PWD/web/static/vendor":/out node:24-alpine …` per lo sviluppo locale (nessun Node sul PC).
- Nessun `<style>` inline né `eval`: il CSS è nel file; ProseMirror usa solo attributi `style` (ammessi da `style-src-attr 'unsafe-inline'`). Verificato nei test browser (console senza errori CSP).

### Aggancio ai form

- Ogni `<textarea data-editor>` (corpo di avvisi e guide `markdown`) viene nascosta e sostituita dall'editor; il Markdown viene riscritto nella textarea a ogni modifica, quindi i form HTMX restano identici. Dopo uno swap HTMX della sezione (`htmx:afterSwap`) le nuove textarea vengono agganciate.
- Bundle assente o errore di caricamento → resta la textarea normale (degradazione voluta).
- `admin_base.html` carica `vendor/editor.js` e `editor.css` solo nell'admin.

### Toolbar (etichette in italiano)

Grassetto, corsivo, titolo (H2/H3), elenco puntato, elenco numerato, link (prompt con URL), immagine, tabella (inserisci/aggiungi riga/colonna/elimina), annulla, ripeti. Scorciatoie standard (Ctrl+B/I/Z/Y).

### Immagini

- Bottone, incolla o trascina → upload a pezzi da 512 KB su `POST /admin/media` (protocollo uguale al caricamento dei backup: id upload, offset, ultimo pezzo) → risposta `{"ok":true,"url":"/uploads/guide/<nome>"}` → l'immagine compare nell'editor.
- Tipo dal contenuto: PNG, JPEG, WebP (niente SVG nelle guide); max **2 MB**. Errore → messaggio generico nell'editor (mai il corpo della risposta, che il proxy può sostituire): gli endpoint rispondono sempre 200 con `{"ok":false,"error":"…"}`.

### Anteprima

Pulsante "Anteprima" → `POST /admin/anteprima` (Markdown nel form) → HTML di `markdown.Render` in un pannello sotto l'editor. Serve a vedere esattamente il risultato della plancia (stesso renderer).

### Manutenzione

- `dependabot.yml`: ecosistema `npm` su `/web/editor` (Dependabot legge `pnpm-lock.yaml`), settimanale con cooldown come gli altri; `docker` copre già l'immagine `node`.
- `test.yml`: nuovo step `docker build --target editor .` — una PR di Dependabot che rompe la build dell'editor diventa rossa. Il job resta `test` (required check).
- Trivy fs (già bloccante) legge `pnpm-lock.yaml`.

## 4. Guide in plancia e nell'admin

### Admin (`/admin/guide`)

- Il form sceglie il tipo (radio: Link, Testo, PDF, GitHub); i campi degli altri tipi sono nascosti (`admin.js`, nessun inline).
- **Testo:** editor (`textarea data-editor`).
- **PDF:** input file con upload a pezzi su `POST /admin/media?tipo=pdf` (stesso endpoint, max **20 MB**, contenuto che inizia con `%PDF-`), il nome restituito va in un campo nascosto `file`. In modifica mostra il PDF attuale con link.
- **GitHub:** campo URL; al salvataggio il server scarica subito il file (errore di download = errore di validazione, la guida non si salva); pulsante "Aggiorna ora" (`POST /admin/guide/{id}/aggiorna`); in elenco data dell'ultimo aggiornamento e, se presente, "Ultimo aggiornamento fallito: …".

### Plancia

- `link` → come oggi (nuova scheda).
- `markdown`, `github` → `/guide/{id}`: testata della plancia, titolo, contenuto reso, per le GitHub "Fonte: <link al file su GitHub> · aggiornata il …".
- `pdf` → `/guide/{id}/pdf` in una nuova scheda (viewer del browser).
- `/guide/{id}` e `/guide/{id}/pdf` applicano la stessa visibilità della plancia (`contentFilter`: guida abilitata, generale o di un'app visibile, gruppi della plancia). Guida non visibile o inesistente → pagina "Guida non disponibile" con **200** (il proxy sostituisce i 404) e link alla plancia.

## 5. GitHub — `internal/guidesrc`

- `ParseGitHubURL(s) (raw, blobDir, rawDir string, err)`: ammesso solo `https://github.com/{owner}/{repo}/blob/{ref}/{path}` con path che termina in `.md` (case-insensitive), senza userinfo né porta né query/fragment significativi; `owner`, `repo` `[A-Za-z0-9._-]+`; `ref` e `path` senza `..`. Restituisce l'URL raw (`https://raw.githubusercontent.com/{owner}/{repo}/{ref}/{path}`) e le basi per riscrivere link (`https://github.com/{owner}/{repo}/blob/{ref}/{dir}/`) e immagini (`https://raw.githubusercontent.com/{owner}/{repo}/{ref}/{dir}/`).
- `Fetcher`: `http.Client` dedicato — timeout 15 s, `Proxy: http.ProxyFromEnvironment` (`HTTPS_PROXY` se il server esce tramite proxy), redirect seguiti solo verso `raw.githubusercontent.com` (max 3), nessun cookie, risposta letta fino a **1 MB** (oltre = errore), solo 200 e testo valido UTF-8.
- Le basi per link e immagini non si salvano: si ricalcolano da `source_url` a ogni rendering.
- **Aggiornamento:** al salvataggio, con "Aggiorna ora" e con un job ogni `GUIDE_REFRESH_HOURS` (predefinito **6**, `0` = solo manuale; nuova variabile in `docker-compose.yml`, `.env.example`, `internal/config`). Il job aggiorna le guide con `fetched_at` più vecchio dell'intervallo, una alla volta.
- **Errore** (rete, 404, troppo grande, redirect esterno): resta l'ultima copia buona, `fetch_error` valorizzato e mostrato nell'admin, log `slog.Warn`. Successo → `fetch_error = ''`.
- Immagini e link relativi riscritti dal renderer con le basi calcolate; le immagini sono caricate dal browser direttamente da GitHub (`img-src https:` già nella CSP), nessuna copia locale.

## 6. PDF e immagini caricate

- Directory `UPLOAD_DIR/guide/`, nomi casuali (come icone e branding, codice generico `saveUpload`/`removeUpload`).
- Immagini servite da `/uploads/guide/{file}` con `Content-Security-Policy: sandbox` come le icone.
- PDF: `/guide/{id}/pdf` → `X-Content-Type-Options: nosniff`, `Content-Type: application/pdf`, `Content-Disposition: inline; filename="<titolo ripulito>.pdf"`, CSP `default-src 'none'; object-src 'self'; frame-ancestors 'none'`. Niente `sandbox`: il viewer PDF di Chrome/Edge è un plugin (governato da `object-src`) e un documento in sandbox rischia di essere scaricato invece che mostrato. La prova automatica non è stata possibile (i browser pilotati da Playwright/DevTools scaricano i PDF anche senza CSP): verifica a mano in Chrome ed Edge nei test browser. Il rischio residuo è basso: i PDF li carica solo l'admin e il viewer non esegue script della pagina.
- **Pulizia:** il PDF vecchio si cancella quando viene sostituito o la guida eliminata. Le immagini senza riferimenti (nome assente da ogni `alerts.body` e `guides.body` di tipo `markdown`) si cancellano al salvataggio e all'eliminazione di avvisi e guide; per evitare di cancellare un'immagine appena caricata in un form non ancora salvato, si eliminano solo i file più vecchi di 24 ore.
- Backup: `uploads/guide/` è già incluso (il backup prende tutto `uploads/`); il contenuto GitHub è nel DB.

## 7. Avvisi

- Admin: corpo con l'editor (`textarea data-editor`).
- Carosello: `markdown.Plain(body, 200)` + "Leggi tutto" → `/avvisi/{id}` se il testo è stato troncato o il corpo contiene immagini/tabelle.
- `/avvisi/{id}`: nuova pagina con titolo, livello, fonte, date e corpo reso; stessa visibilità di `/avvisi` (avviso attivo e destinato all'utente o "Mostra tutto"); altrimenti "Avviso non disponibile" con 200.
- `/avvisi` e popup urgente: corpo reso con `markdown.Render` (sostituisce `paragraphs`).
- Notifiche (push e SSE): testo `markdown.Plain(body, 120)`; il clic apre `/avvisi/{id}` (oggi `/`).

## 8. Test

- `internal/markdown`: HTML grezzo rimosso; `javascript:`/`data:`/`vbscript:` scartati in link e immagini (anche con maiuscole, spazi, entità); `mailto:` ammesso solo nei link; hard wraps; tabelle; titoli spostati di un livello; link esterni con `rel`; riscrittura relativi con `BaseURL`/`ImageBaseURL` (e assoluti lasciati); `Plain` (sintassi tolta, troncamento su parola, rune multibyte); **fuzz test** (`Render` non va in panic e non produce `<script`, `javascript:`).
- `internal/guidesrc`: `ParseGitHubURL` validi e rifiutati (host, userinfo, porta, `..`, non `.md`, `raw`/`tree`); `Fetcher` contro `httptest` (200, 404, oltre 1 MB, redirect verso host esterno rifiutato, UTF-8 non valido).
- `internal/database`: migrazione v7, validazioni dei tipi, query di refresh.
- `internal/web`: `/admin/media` (tipi dal contenuto, limiti, pezzi, sempre 200+JSON); salvataggio delle guide per tipo; GitHub che fallisce al salvataggio = 422 con errore; refresh che fallisce mantiene il body; `/guide/{id}`, `/guide/{id}/pdf`, `/avvisi/{id}` filtrati per visibilità (200 "non disponibile"); pulizia orfani (vecchi > 24 h cancellati, recenti e referenziati no); carosello con estratto e "Leggi tutto"; anteprima admin.
- Browser (Playwright, a mano): editor nell'admin (formattazione, immagine incollata, tabella), salvataggio e rendering in plancia, console senza errori CSP, textarea di ripiego senza bundle, PDF aperto in Chrome/Edge.

## Fuori perimetro

Repository GitHub privati (token), guide multi-pagina o cartelle, ricerca full-text nelle guide, versioni/storico dei contenuti, conversione automatica dei link esistenti in guide interne, immagini delle guide GitHub copiate in locale.

## Documentazione

`CLAUDE.md` (pacchetti `markdown` e `guidesrc`, editor e build in container, nuove route, `GUIDE_REFRESH_HOURS`), README (`scripts/editor.sh`), `publiccode.yml` alla release.
