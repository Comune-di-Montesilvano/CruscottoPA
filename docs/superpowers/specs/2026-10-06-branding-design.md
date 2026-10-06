# Branding dell'ente

Data: 2026-10-06 · Stato: implementata · Priorità: prima dei sotto-progetti 2–4

## Contesto e obiettivo

Oggi il nome dell'ente è scritto nel codice (`dashboard.html`: "CruscottoPA · Comune di Montesilvano"), non c'è un logo e manca la favicon (debito noto). CruscottoPA ha un `publiccode.yml`: è pensato per il riuso da altri enti, quindi nessun riferimento a un ente specifico deve restare nei template.

**Obiettivo:** nome e logo dell'ente configurabili dal pannello admin, mostrati in plancia, pagina avvisi, login e admin; logo e favicon propri di CruscottoPA, fissi.

## Decisioni

- Configurazione **dal pannello admin** (`/admin/ente`), non da variabili d'ambiente: si cambia senza ridistribuire lo stack, e il logo finisce nel backup insieme al resto.
- **Favicon fissa** = logo di CruscottoPA, non configurabile: identifica l'applicazione (scheda del browser, collegamento sul desktop), non l'ente.
- **Logo di CruscottoPA** già disegnato in `web/static/img/` (palazzo pubblico: frontone e colonne, con le tile degli applicativi al posto del portone):

  | File | Uso |
  |---|---|
  | `logo.svg` | icona con riquadro blu (sorgente delle taglie 24–256 della favicon) |
  | `logo-on-dark.svg` | senza riquadro, per la testata blu della plancia e il rail admin |
  | `logo-on-light.svg` | senza riquadro, per la card di login e il footer |
  | `logo-16.svg` | versione allineata ai pixel, sorgente della taglia 16 |
  | `favicon.ico` | 16, 24, 32, 48, 64, 128, 256 px; anche per i collegamenti sul desktop |

  Rigenerare `favicon.ico`: rasterizzare `logo-16.svg` a 16 px e `logo.svg` alle altre taglie (Chrome headless, sfondo trasparente) e unirle con Pillow.
- **Solo il nome dell'ente** come testo. Il nome del prodotto resta "CruscottoPA"; niente sottotitolo né nome del portale personalizzabile (YAGNI).
- Fuori ambito: apple-touch-icon, manifest e icone PWA (sotto-progetto 3), colori del tema.

## 1. Dati

Migrazione **v4** (in coda, come da regola):

```sql
CREATE TABLE branding (
	id         INTEGER PRIMARY KEY CHECK (id = 1),
	ente_name  TEXT NOT NULL DEFAULT '',
	logo_file  TEXT NOT NULL DEFAULT '',
	updated_at TEXT NOT NULL DEFAULT '',
	updated_by TEXT NOT NULL DEFAULT ''
);
INSERT INTO branding (id) VALUES (1);
```

- Una sola riga, sempre presente. Stringa vuota = non impostato.
- Campi futuri: `ALTER TABLE branding ADD COLUMN …` in una nuova migrazione.
- `internal/database`: tipo `Branding{EnteName, LogoFile, UpdatedAt, UpdatedBy}`, `GetBranding()` e `UpdateBranding(Branding)` (senza `ctx`, come gli altri metodi del package).
- Un DB ripristinato da un backup precedente alla v4 viene migrato all'avvio come gli altri: riga vuota, nessun branding.

## 2. File

- Directory `UPLOAD_DIR/branding/`, nome casuale (16 byte hex + estensione) come per le icone. Inclusa nel backup e nel ripristino senza modifiche a `internal/backup` (copia già tutto `uploads/`).
- Logo: tipo riconosciuto dal contenuto (PNG, WebP, SVG, riuso di `detectIconExt`), max 512 KB.
- Servizio da `GET /uploads/branding/{file}`: nome validato con regex (`^[0-9a-f]{32}\.(png|webp|svg)$`), stessi header delle icone (`Content-Security-Policy: sandbox; …`, `nosniff`, `Cache-Control: public, max-age=86400`). Il nome cambia a ogni caricamento, quindi la cache lunga non mostra mai un file vecchio.
- Il codice di salvataggio, rimozione e servizio delle icone va generalizzato per directory, non duplicato.

## 3. Cache del branding

- `Server` tiene il branding corrente in un `atomic.Pointer[database.Branding]`: caricato in `web.New`, sostituito dopo ogni salvataggio riuscito.
- Nessuna query per pagina. Il ripristino termina il processo (`exit(0)`), quindi al riavvio la cache si ricarica dal DB ripristinato.

## 4. Rendering

Funzione di template `ente` (registrata al parse, legge la cache) → `database.Branding` corrente.

Uso nei template:

| Pagina | Testata / brand | `<title>` |
|---|---|---|
| Plancia (`dashboard.html`) | sempre `logo-on-dark.svg` + "CruscottoPA"; accanto, dopo un separatore, logo ente (~40 px, 30 px su mobile) + nome ente se impostati | "CruscottoPA · {ente}" o "CruscottoPA" |
| Avvisi (`avvisi.html`) | "← CruscottoPA · {ente}" o "← CruscottoPA" | "Avvisi · CruscottoPA[ · {ente}]" |
| Login (`admin_login.html`) | `logo-on-light.svg` (~56 px) + "CruscottoPA"; sotto, logo ente piccolo + nome ente | "Accesso · CruscottoPA[ · {ente}]" |
| Admin (`admin_base.html`) | `logo-on-dark.svg` (~30 px, il rail è scuro) + "CruscottoPA / amministrazione" nel rail; sotto, nome ente | "Amministrazione · CruscottoPA[ · {ente}]" |

- Footer: `logo-on-light.svg` (~14 px) + "CruscottoPA v{{.Version}}".
- `<link rel="icon" href="/static/img/favicon.ico">` in tutti gli head (plancia, avvisi, login, admin). Solo ICO, niente favicon SVG: a 16 px i browser rasterizzerebbero l'SVG impastando le tile, mentre l'ICO contiene la versione allineata ai pixel.
- `alt` del logo vuoto quando il nome dell'ente è scritto accanto (non va letto due volte dagli screen reader), "Logo dell'ente" se il nome è vuoto.
- `GET /favicon.ico` serve `web/static/img/favicon.ico` (`image/x-icon`, `Cache-Control: no-cache` come gli statici), per browser e strumenti che la richiedono senza leggere l'HTML.
- CSP invariata: tutte le immagini arrivano da `'self'`.

## 5. Pannello admin

- Voce nel rail "Ente" (icona `account_balance`), dopo "Panoramica".
- `GET /admin/ente` → sezione `branding_section`; `POST /admin/ente` → salva e restituisce la sezione (200 ok, 422 con errori), come le altre sezioni.
- Form `hx-post` multipart (`hx-encoding="multipart/form-data"`), come quello dell'icona delle app:
  - **Nome ente** (testo, max 120 caratteri, spazi ai bordi rimossi; vuoto ammesso);
  - **Logo**: anteprima attuale, campo file, checkbox "Rimuovi il logo";
  - suggerimento breve: logo orizzontale o quadrato, sfondo trasparente.
- File e checkbox "Rimuovi" insieme: vince il file nuovo.
- Dopo il salvataggio la sezione mostra "Salvato" e le nuove anteprime. La testata del rail si aggiorna al prossimo caricamento di pagina (accettato).
- `updated_by` = utente della sessione.

## 6. Errori e coerenza

- Validazione completa prima di scrivere qualunque cosa: nome troppo lungo, file troppo grande, formato non ammesso ("Formato non ammesso: usa PNG, WebP o SVG."). Con un errore non si salva nulla, nemmeno il nome; risposta 422 con l'errore accanto al campo.
- Ordine: scrittura del nuovo logo su disco → `UpdateBranding` → aggiornamento cache → cancellazione del logo vecchio sostituito o rimosso.
- Se `UpdateBranding` fallisce: il file appena scritto viene rimosso, il vecchio resta, cache invariata, 500.
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
- formato non ammesso (es. GIF) → 422, nome non salvato;
- file oltre 512 KB → 422;
- `GET /favicon.ico` → 200 `image/x-icon`; tutte le pagine hanno `<link rel="icon" href="/static/img/favicon.ico">`;
- `GET /uploads/branding/{file}` con CSP sandbox; nome non valido → 404;
- nessun "Montesilvano" nei template (`grep` in un test sui file di `web/templates`).

## 8. Documentazione

- `CLAUDE.md`: tabella `branding` (v4), route `/admin/ente`, `/favicon.ico`, `/uploads/branding/{file}` tra le pubbliche, file del logo in `web/static/img` e come rigenerare l'ICO; rimuovere "Manca la favicon" dal debito noto.
- Nessuna nuova variabile d'ambiente.
