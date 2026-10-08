# Assistenza per applicativo, ricerca a tendina e contatti nella testata

Data: 2026-10-08 · Stato: da approvare · Versione prevista: 0.9.0

## Contesto e obiettivo

Le guide agganciate agli applicativi spiegano come usarli, ma non dicono **a chi rivolgersi quando qualcosa non funziona**. Per alcuni software l'assistenza è esterna e condivisa: il portale di assistenza Maggioli vale sia per TINN sia per Sicraweb. Oggi l'unico modo è una guida "link" ripetuta su ogni applicativo, indistinguibile dalle altre.

Nella stessa versione:

- la **ricerca** passa da filtro delle tile a **menu a tendina con i risultati** raggruppati;
- la riga sotto il saluto mostra i **dati dell'utente da AD** (ufficio, interno, mail…), scelti dall'admin invece che fissi nel codice.

**Obiettivi**

1. Canali di assistenza gestiti a parte, collegabili a **più applicativi** (molti a molti), ben visibili in plancia.
2. Ricerca moderna: tendina con applicativi, guide e canali di assistenza, navigabile da tastiera.
3. Riga dei contatti nella testata configurabile dall'admin.

**Fuori ambito:** modulo di invio ticket (resta in roadmap), chat interna (roadmap, da studiare), visibilità per gruppi dei singoli canali.

## Decisioni

- **Sezione admin "Assistenza"** separata dalle guide: un canale non è documentazione, è "dove chiedere aiuto".
- Un canale: **nome**, **link** (`http`/`https`), **nota breve** facoltativa, **attivo**, **applicativi collegati** (uno o più). Un applicativo può avere **più canali** (es. TINN: portale Maggioli per gli errori del software + CED per utenze e permessi).
- **Nessuna visibilità propria**: un canale si vede insieme a ogni applicativo visibile a cui è collegato. Un canale senza applicativi visibili non compare.
- In plancia: **icona cuffie sempre visibile** sulla tile chiusa; a tile aperta, dopo le guide, **un riquadro per canale** "Problemi con TINN? → Portale assistenza Maggioli ↗" con la nota.
- **Ricerca lato client** sui dati già in pagina (niente chiamate al server), senza maiuscole né accenti; la griglia non si filtra più.
- **Contatti nella testata**: ogni "Attributo AD utilizzabile" (pagina Gruppi) può essere marcato "Mostra sotto il saluto" con un formato (testo, interno, email). Nessun attributo fisso nel codice: sostituisce `heroAttrs`/`officeLine` della 0.8.0.

## 1. Dati (migrazione v8)

```sql
CREATE TABLE support_channels (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	title      TEXT    NOT NULL,
	url        TEXT    NOT NULL,
	note       TEXT    NOT NULL DEFAULT '',
	sort_order INTEGER NOT NULL DEFAULT 0,
	enabled    INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE app_support (
	app_id     INTEGER NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
	channel_id INTEGER NOT NULL REFERENCES support_channels(id) ON DELETE CASCADE,
	PRIMARY KEY (app_id, channel_id)
);
CREATE INDEX idx_app_support_channel ON app_support(channel_id);

ALTER TABLE audience_attributes ADD COLUMN hero       INTEGER NOT NULL DEFAULT 0; -- 0 = no, altrimenti posizione
ALTER TABLE audience_attributes ADD COLUMN hero_kind  TEXT    NOT NULL DEFAULT 'text'; -- text | phone | mail (validato in Go)
```

- Ordinamento dei canali con `sort_order` + `moveRow` come le altre tabelle.
- Nella plancia, per ogni app i canali attivi in ordine di `sort_order`.
- Backup: nulla da fare, sono nel DB.

## 2. Admin

### 2.1 Sezione "Assistenza" (`/admin/assistenza`)

Stesso schema delle altre sezioni (`assistenza_section`, azioni HTMX che restituiscono la sezione, 422 con errori).

- **Elenco**: nome, link (dominio), applicativi collegati come etichette, attivo; ↑ ↓, Modifica, Elimina (con conferma).
- **Form**: nome (max 80), link (obbligatorio, `http`/`https`, max 2048), nota (max 200, testo semplice), attivo, **applicativi**: caselle raggruppate per categoria (tutti gli applicativi, anche quelli senza URL o disattivati, con l'indicazione).
- Route: `GET /admin/assistenza`, `GET /admin/assistenza/{id}/modifica`, `POST /admin/assistenza`, `POST /admin/assistenza/{id}`, `POST …/elimina`, `POST …/sposta`.
- Voce nel rail dell'admin con icona `support_agent`; conteggio nella panoramica.

### 2.2 Scheda dell'applicativo

Sola lettura sotto il form: "Assistenza: Portale Maggioli, CED" con link alla sezione. Il collegamento si fa dal canale (un solo posto dove modificarlo).

### 2.3 Attributi AD utilizzabili (pagina Gruppi)

Ogni riga ha in più: casella **Sotto il saluto** e formato (**Testo**, **Interno**, **Email**), con ↑ ↓ per l'ordine nella riga. Suggerimento nel riquadro: "Es. Ufficio (`physicalDeliveryOfficeName`), Interno (`telephoneNumber`), Email (`mail`)". Un attributo usato solo nella testata non è "in uso" per i gruppi.

## 3. Plancia

### 3.1 Tile

- Tile chiusa: icona `support_agent` (cuffie) accanto al badge delle guide se l'app ha almeno un canale attivo; `title="Assistenza disponibile"`. Una tile con soli canali e senza guide ha l'icona e si apre come le altre (mouse, focus, tocco sull'icona).
- Tile aperta (`.tile-more`), dopo le guide: per ogni canale un riquadro con bordo a sinistra colorato:
  - riga 1: icona + "Problemi con {{app}}?"
  - riga 2: link "{{nome canale}} ↗" (nuova scheda, `rel="noopener"`)
  - riga 3: nota, se c'è.

### 3.2 Ricerca a tendina

Sostituisce il filtro attuale (`filter()` in `dashboard.js`, `data-search-item` sulle tile e `#no-results`).

- Dati: il server scrive nella pagina un indice JSON in `<script type="application/json" id="search-index">` (consentito dalla CSP: non viene eseguito) con applicativi visibili (titolo, descrizione, categoria, URL, icona), guide visibili (titolo, app o "generale", link), canali (nome, nota, app collegate, URL). Contenuti nascosti dai gruppi esclusi, salvo "Mostra tutto".
- Confronto senza maiuscole né accenti su titolo, descrizione, nomi delle app collegate; ordine: inizio del titolo, poi parola del titolo, poi resto.
- Tendina sotto la barra, max 8 risultati per gruppo, gruppi **Applicativi**, **Guide**, **Assistenza**, con icona e riga secondaria (categoria, app della guida, app coperte dal canale). Nessun risultato: "Nessun risultato per «…»".
- Tastiera: ↓ ↑ scorrono, Invio apre (stesse regole di nuova scheda dei link in pagina), Esc chiude e svuota, `/` mette il focus. ARIA: `role="combobox"` sull'input, `role="listbox"`/`option`, `aria-activedescendant`.
- Clic fuori o blur chiude la tendina; la griglia resta ferma.

### 3.3 Testata

Sotto il saluto, gli attributi marcati "Sotto il saluto" nell'ordine scelto, dal profilo AD già in cache (gli attributi della testata si aggiungono a quelli dei gruppi nella stessa richiesta). Formati: **Testo** (valori tutti maiuscoli resi leggibili, come oggi), **Interno** (icona `call`, "Int. 731"), **Email** (icona `mail`, testo semplice). Valori vuoti saltati; nessun attributo o utente non riconosciuto → riga assente.

## 4. Errori e casi limite

- Link del canale non valido → 422 con errore sul campo.
- Canale disattivato → sparisce da plancia e ricerca, resta in admin.
- Applicativo eliminato → collegamenti rimossi dal `CASCADE`; canale senza applicativi resta in admin con avviso "Non collegato a nessun applicativo".
- AD non disponibile → riga della testata assente, il resto invariato.

## 5. Test

- `internal/database`: migrazione v8 su DB v7 con dati; CRUD canali; collegamento a più app; cascate (app e canale); ordine; colonne `hero`.
- `internal/web`: CRUD admin con 422; caselle degli applicativi; plancia con icona e riquadro solo per app visibili e canali attivi; indice JSON senza contenuti nascosti; riga della testata con formati e ordine; anonimo senza riga.
- JS (`dashboard.js`): test Go che legge il file (combobox, tasti, nessun `filter()` sulle tile) + prova Playwright in container della tendina (digitare "magg", frecce, Invio).
- CSS: test sulle regole chiave come per le altre parti.

## 6. Roadmap (fuori da questa versione)

- **Chat interna** tra dipendenti: da studiare (serve un'identità verificata: NTLM è solo dichiarato).
- **Modulo ticket**: i canali di assistenza ne sono il primo passo (dove chiedere aiuto); l'invio dalla plancia resta da progettare.
