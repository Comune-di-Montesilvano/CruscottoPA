# Branding dell'ente

Data: 2026-10-06 · Stato: in revisione · Priorità: prima dei sotto-progetti 2–4

## Contesto e obiettivo

Oggi il nome dell'ente è scritto nel codice (`dashboard.html`: "CruscottoPA · Comune di Montesilvano"), non c'è un logo e manca la favicon (debito noto). CruscottoPA ha un `publiccode.yml`: è pensato per il riuso da altri enti, quindi nessun riferimento a un ente specifico deve restare nei template.

**Obiettivo:** nome dell'ente, logo e favicon configurabili dal pannello admin, mostrati in plancia, pagina avvisi, login e admin.

## Decisioni

- Configurazione **dal pannello admin** (`/admin/ente`), non da variabili d'ambiente: si cambia senza ridistribuire lo stack, e logo/favicon finiscono nel backup insieme al resto.
- **Logo e favicon separati**: gli stemmi comunali dettagliati o i logotipi orizzontali sono illeggibili a 16 px; l'ente carica una favicon apposita (es. solo lo scudo).
- **Solo il nome dell'ente** come testo. Il nome del prodotto resta "CruscottoPA"; niente sottotitolo né nome del portale personalizzabile (YAGNI).
- Fuori ambito: apple-touch-icon, manifest e icone PWA (sotto-progetto 3), colori del tema.

## 1. Dati

Migrazione **v4** (in coda, come da regola):

```sql
CREATE TABLE branding (
	id           INTEGER PRIMARY KEY CHECK (id = 1),
	ente_name    TEXT NOT NULL DEFAULT '',
	logo_file    TEXT NOT NULL DEFAULT '',
	favicon_file TEXT NOT NULL DEFAULT '',
	updated_at   TEXT NOT NULL DEFAULT '',
	updated_by   TEXT NOT NULL DEFAULT ''
);
INSERT INTO branding (id) VALUES (1);
```

- Una sola riga, sempre presente. Stringa vuota = non impostato.
- Campi futuri: `ALTER TABLE branding ADD COLUMN …` in una nuova migrazione.
- `internal/database`: tipo `Branding{EnteName, LogoFile, FaviconFile, UpdatedAt, UpdatedBy}`, `GetBranding(ctx)` e `UpdateBranding(ctx, Branding)`.
- Un DB ripristinato da un backup precedente alla v4 viene migrato all'avvio come gli altri: riga vuota, nessun branding.

## 2. File

- Directory `UPLOAD_DIR/branding/`, nome casuale (16 byte hex + estensione) come per le icone. Inclusa nel backup e nel ripristino senza modifiche a `internal/backup` (copia già tutto `uploads/`).
- Tipo riconosciuto dal contenuto, max 512 KB:
  - **logo**: PNG, WebP, SVG (riuso di `detectIconExt`);
  - **favicon**: PNG, WebP, SVG oppure **ICO** (firma `00 00 01 00`).
- Servizio da `GET /uploads/branding/{file}`: nome validato con regex (`^[0-9a-f]{32}\.(png|webp|svg|ico)$`), stessi header delle icone (`Content-Security-Policy: sandbox; …`, `nosniff`, `Cache-Control: public, max-age=86400`). Il nome cambia a ogni caricamento, quindi la cache lunga non mostra mai un file vecchio.
- Il codice di salvataggio, rimozione e servizio delle icone va generalizzato per directory e formati ammessi, non duplicato.

## 3. Cache del branding

- `Server` tiene il branding corrente in un `atomic.Pointer[database.Branding]`: caricato in `web.New`, sostituito dopo ogni salvataggio riuscito.
- Nessuna query per pagina. Il ripristino termina il processo (`exit(0)`), quindi al riavvio la cache si ricarica dal DB ripristinato.

## 4. Rendering

Funzioni di template (registrate al parse, leggono la cache):

- `ente` → `database.Branding` corrente;
- `faviconURL` → `/uploads/branding/{favicon_file}` se impostata, altrimenti `/static/img/favicon.svg`.

Uso nei template:

| Pagina | Testata / brand | `<title>` |
|---|---|---|
| Plancia (`dashboard.html`) | logo (altezza ~40 px, se presente) + nome ente; senza nome: "CruscottoPA" | "CruscottoPA · {ente}" o "CruscottoPA" |
| Avvisi (`avvisi.html`) | "← {ente}" o "← CruscottoPA" | "Avvisi · CruscottoPA[ · {ente}]" |
| Login (`admin_login.html`) | logo sopra il titolo + nome ente sotto "CruscottoPA" | "Accesso · CruscottoPA[ · {ente}]" |
| Admin (`admin_base.html`) | logo piccolo nel rail + "CruscottoPA / amministrazione" + nome ente | "Amministrazione · CruscottoPA[ · {ente}]" |

- Footer invariato: "CruscottoPA v{{.Version}}".
- `<link rel="icon" href="{{faviconURL}}">` in tutti gli head (plancia, avvisi, login, admin).
- `alt` del logo = nome ente, oppure "Logo dell'ente" se il nome è vuoto.
- Favicon predefinita: nuovo file `web/static/img/favicon.svg` (glifo "dashboard" bianco su quadrato arrotondato blu, colore primario dell'app).
- `GET /favicon.ico` → `302` verso `faviconURL`, per browser e strumenti che la richiedono senza leggere l'HTML. `Cache-Control: no-cache` sul redirect, così un cambio di favicon si vede subito.
- CSP invariata: tutte le immagini arrivano da `'self'`.

## 5. Pannello admin

- Voce nel rail "Ente" (icona `account_balance`), dopo "Panoramica".
- `GET /admin/ente` → sezione `branding_section`; `POST /admin/ente` → salva e restituisce la sezione (200 ok, 422 con errori), come le altre sezioni.
- Form `hx-post` multipart (`hx-encoding="multipart/form-data"`), come quello dell'icona delle app:
  - **Nome ente** (testo, max 120 caratteri, spazi ai bordi rimossi; vuoto ammesso);
  - **Logo**: anteprima attuale, campo file, checkbox "Rimuovi il logo";
  - **Favicon**: anteprima attuale (o quella predefinita), campo file, checkbox "Rimuovi la favicon";
  - suggerimenti brevi: logo orizzontale o quadrato, sfondo trasparente; favicon quadrata, semplice, leggibile a 16 px.
- File e checkbox insieme: vince il file nuovo.
- Dopo il salvataggio la sezione mostra "Salvato" e le nuove anteprime. La testata del rail si aggiorna al prossimo caricamento di pagina (accettato).
- `updated_by` = utente della sessione.

## 6. Errori e coerenza

- Validazione completa prima di scrivere qualunque cosa: nome troppo lungo, file troppo grande, formato non ammesso (ICO come logo → "Formato non ammesso: usa PNG, WebP o SVG."). Con un errore non si salva nulla, nemmeno il nome; risposta 422 con l'errore accanto al campo.
- Ordine: scrittura dei nuovi file su disco → `UpdateBranding` → aggiornamento cache → cancellazione dei file vecchi sostituiti o rimossi.
- Se `UpdateBranding` fallisce: i file appena scritti vengono rimossi, i vecchi restano, cache invariata, 500.
- Cancellazione di un file vecchio fallita: solo log (`slog.Warn`), il salvataggio resta valido.

## 7. Test

`internal/database`:
- dopo la migrazione esiste esattamente una riga vuota (`id = 1`);
- `UpdateBranding` + `GetBranding` andata e ritorno.

`internal/web`:
- `/admin/ente` richiede la sessione (303 / 401 + `HX-Redirect`);
- salvataggio del nome → la plancia mostra il nome e il `<title>` lo contiene;
- upload logo PNG → file in `uploads/branding`, la plancia mostra `<img>` col nuovo URL;
- sostituzione del logo → il file vecchio viene cancellato; "Rimuovi" → file cancellato e campo vuoto;
- ICO accettato come favicon, rifiutato come logo (422, nome non salvato);
- file oltre 512 KB → 422;
- `GET /favicon.ico` → 302 a `/static/img/favicon.svg`; dopo l'upload → 302 a `/uploads/branding/…`;
- `GET /uploads/branding/{file}` con CSP sandbox; nome non valido → 404;
- nessun "Montesilvano" nei template (`grep` in un test sui file di `web/templates`).

## 8. Documentazione

- `CLAUDE.md`: tabella `branding` (v4), route `/admin/ente`, `/favicon.ico`, `/uploads/branding/{file}` tra le pubbliche; rimuovere "Manca la favicon" dal debito noto.
- Nessuna nuova variabile d'ambiente.
