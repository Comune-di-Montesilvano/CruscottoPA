# Ticket fase 2: i miei ticket, conversazione, risposta e notifiche

Data: 2026-10-08 · Stato: da approvare · Versione prevista: 0.12.0 (stesso rilascio della fase 1, PR #30)

## Contesto e obiettivo

La fase 1 (`2026-10-08-ticket-otrs-design.md`) apre i ticket in OTRS dalla plancia. Le risposte degli operatori arrivano solo per mail. Questa fase porta in plancia:

1. **I miei ticket**: i ticket dell'utente aperti e quelli chiusi negli ultimi 7 giorni, con stato e segnale di risposta non vista.
2. **Conversazione**: i messaggi visibili al cliente, con gli allegati.
3. **Risposta**: nuovo messaggio dalla plancia, con allegati; su un ticket chiuso lo riapre.
4. **Notifica**: SSE e Web Push quando un operatore risponde.

**Fuori ambito:** ticket chiusi da più di 7 giorni (restano nella mail), ricerca fra i propri ticket, valutazione, modifica di oggetto o priorità, webhook da OTRS.

## Rischio accettato: identità dichiarata

Il riconoscimento NTLM è **dichiarato**: il server non verifica la password e chiunque in rete, con uno script, può presentarsi con lo username di un collega. In fase 1 il rischio era basso (le risposte vanno comunque alla mail vera). Qui chi si spaccia per un collega **può leggere i suoi ticket** (testo, allegati, eventuali dati personali) e rispondere a suo nome. Proposte alternative (link via mail, password di dominio, solo elenco senza testo) sono state scartate dal responsabile del progetto, che **accetta il rischio** (2026-10-08). Mitigazioni comunque presenti: controllo di proprietà su ogni lettura e risposta, registro delle risposte con username, limiti di frequenza.

## Lettura da OTRS (`internal/otrs`)

Approccio scelto: **lettura diretta da OTRS con cache breve**; nessuna copia locale del contenuto dei ticket.

**Configurazione nuova** (in `docker-compose.yml`, `.env.example`, `internal/config`):

| Variabile | Default | Note |
|---|---|---|
| `OTRS_ROUTE_SEARCH` | `/TicketSearch` | route POST di `TicketSearch` |
| `OTRS_ROUTE_GET` | `/Ticket/:TicketID` | route GET di `TicketGet`; `:TicketID` sostituito dal client; credenziali nel corpo JSON (verificato: OTRS 5 lo legge anche in GET) |

Validazione: route che iniziano con `/`; `OTRS_ROUTE_GET` deve contenere `:TicketID`.

**Metodi nuovi del `Client`** (oltre a `Create`):

```go
Mine(ctx, email string) ([]Summary, error)
Get(ctx, email, ticketID string) (Ticket, error)
Reply(ctx, email, ticketID string, r NewReply) error
Changed(ctx, since time.Time) ([]Ticket, error)
Attachment(ctx, email, ticketID, articleID, fileID string) (Attachment, error)
```

- `Summary`: `TicketID, TicketNumber, Title, State, StateType string; Changed, Created time.Time; Closed bool; LastAgentArticle time.Time`.
- `Ticket`: i campi di `Summary`, `CustomerUserID string` e `Articles []Article`.
- `Article`: `ArticleID string; FromAgent bool; From, Subject, Body string; Created time.Time; Attachments []AttachmentInfo` (`FileID, Filename, ContentType string; Size int64`, senza contenuto).
- `NewReply`: `Name, Email, Body string; Attachments []Attachment`.

**Mine**: due `TicketSearch` sulla coda configurata con `CustomerUserLogin` = mail: stati aperti (`StateType`: `new`, `open`, `pending reminder`, `pending auto`) e chiusi con `TicketCloseTimeNewerDate` = ora − 7 giorni. Poi `TicketGet` (con articoli, senza contenuto degli allegati) per ciascuno, al massimo 20 ticket in tutto, ordinati per ultima modifica. `LastAgentArticle` = data dell'ultimo articolo visibile scritto da un operatore.

**Filtro degli articoli** (sempre, dentro `internal/otrs`): passano solo `ArticleType` ∈ {`email-external`, `phone`, `fax`, `webrequest`, `note-external`}. Note interne, `*-internal`, `note-report` e articoli di sistema **non escono mai** dal pacchetto. `FromAgent` = `SenderType` `agent`.

**Proprietà**: `Get`, `Reply`, `Attachment` restituiscono `ErrNotYours` se `CustomerUserID` del ticket ≠ mail del richiedente (confronto senza distinzione di maiuscole), oppure se il ticket non è nella coda configurata. Controllo a ogni chiamata.

**Reply**: `TicketUpdate` con articolo cliente (`webrequest`, `customer`, `From` «Nome <mail>», `text/plain; charset=utf8`) e allegati; se il ticket ha `StateType` `closed`, nello stesso `TicketUpdate` `Ticket.State` = `open`.

**Changed**: `TicketSearch` sulla coda con `TicketChangeTimeNewerDate` = `since` (qualsiasi stato: OTRS 5 non combina «aperti oppure chiusi da poco» in una ricerca, e una risposta su un ticket chiuso va notificata), poi `TicketGet` con articoli. Al massimo 50 ticket per giro; un ticket illeggibile non blocca gli altri.

**Attachment**: `TicketGet` con `AllArticles` e `Attachments`, scelta per `ArticleID` + `FileID` fra gli articoli visibili.

**Tempi**: ogni lettura 15 s; la risposta usa i tempi di `Create` (25 + 10 s), sempre sotto i 60 s del proxy.

`otrs.Mock` implementa i nuovi metodi in memoria (ticket con articoli interni ed esterni) per sviluppo e test.

## Dati (migrazione v13)

```sql
ALTER TABLE tickets_sent ADD COLUMN kind TEXT NOT NULL DEFAULT 'apertura'; -- apertura | risposta
CREATE TABLE ticket_seen (
	username  TEXT NOT NULL,
	ticket_id TEXT NOT NULL,
	seen_at   TEXT NOT NULL,
	PRIMARY KEY (username, ticket_id)
);
CREATE TABLE ticket_notify_state (
	ticket_id       TEXT PRIMARY KEY,
	last_article_id INTEGER NOT NULL,
	updated_at      TEXT NOT NULL
);
CREATE TABLE ticket_users (
	email      TEXT PRIMARY KEY, -- minuscolo
	username   TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
```

Pulizia giornaliera (la stessa di `StartPresenceCleanup`): righe di `ticket_seen`, `ticket_notify_state`, `ticket_users` non aggiornate da più di 30 giorni.

## Plancia

**Widget «I miei ticket»** nella colonna `.widgets`, prima delle guide; solo con modulo attivo e utente riconosciuto con mail in AD.

- Caricato a parte: `GET /partials/ticket` (HTMX, `hx-trigger="load, every 120s"` e refresh su evento SSE `ticket`). La plancia non aspetta OTRS.
- Riga: oggetto, numero, stato in italiano (`new` → «Nuovo», `open` → «In lavorazione», `pending*` → «In attesa», `closed` → «Chiuso»), data dell'ultima modifica, **pallino** se `LastAgentArticle` > `seen_at` (o nessun `seen_at` con almeno una risposta di operatore).
- Aperti in alto; «Chiusi di recente (n)» in un `<details>` chiuso.
- Nessun ticket: «Nessun ticket aperto» + link che apre il dialog «Apri un ticket». OTRS giù: «Assistenza non raggiungibile, riprova più tardi». Sempre 200.
- Il caricamento del widget aggiorna `ticket_users` (mail → username).

**Pagina `/ticket/{id}`** (id = TicketID):

- Testata: oggetto, numero, stato, data di apertura; link «← Plancia».
- Conversazione in ordine cronologico: bolle «Tu» / «Assistenza» con data; testo **semplice** con gli a capo (nessun HTML interpretato); allegati come link a `/ticket/{id}/allegati/{articolo}/{file}`.
- Allegati: immagini con `Content-Security-Policy: sandbox`, PDF con la CSP delle guide, altri tipi scaricati come `application/octet-stream` con `Content-Disposition: attachment`; nome ripulito.
- Modulo di risposta: testo (max 10.000), allegati con l'upload a pezzi esistente (stessi limiti, tetto di 6 per utente), blocco «Informazioni sul PC» in fondo come per l'apertura; con ticket chiuso la nota «Rispondendo il ticket verrà riaperto». JS in `ticket.js` (stesso codice di upload del dialog).
- Aprire la pagina aggiorna `ticket_seen`.
- Ticket di altri, inesistente, fuori coda, OTRS giù, anonimo: 200 con «Ticket non disponibile» (nessun dettaglio).

**Cache**: elenco (`Mine`) e ticket (`Get`) in memoria 60 s per utente/ticket; una risposta inviata svuota la cache dell'utente.

## Risposta: `POST /ticket/{id}/risposta`

Sempre 200 + JSON. Richiedente da AD come per l'apertura; controllo di proprietà; un invio alla volta per utente (lo stesso blocco dell'apertura); **10 risposte/ora** per utente (contate su `tickets_sent` con `kind = 'risposta'`). Esiti: `{"ok":true}`, `{"ok":false,"campi":{…}}`, `{"ok":false,"errore":"anonimo|ad|mail|limite|in_corso|otrs|non_tuo|spento"}` con `casella` se configurata. OTRS fallito: allegati restituiti, testo nel modulo. Riuscita: riga in `tickets_sent` (`kind = 'risposta'`, numero del ticket, PC), cache svuotata, la pagina si ricarica.

`/admin/ticket` mostra anche le risposte (colonna «Tipo»).

## Notifiche di risposta

`StartTicketWatch` (ogni 2 minuti, solo con il modulo attivo):

1. `Changed(since)`; `since` = fine del giro precedente meno 1 minuto (tolleranza sugli orologi).
2. Per ogni ticket, articoli visibili con `FromAgent` e `ArticleID` > `ticket_notify_state.last_article_id`.
3. Destinatario: `ticket_users` per la mail del cliente; senza corrispondenza, solo lo stato si aggiorna.
4. `ticket_notify_state` aggiornato **prima** dell'invio (una risposta si notifica una volta sola).
5. Evento SSE `ticket` alle plance di quell'utente (aggiorna il widget; avviso del browser se la scheda non è visibile) e Web Push alle sue iscrizioni: titolo «Risposta al ticket {numero}», testo = prime 120 lettere della risposta, URL `/ticket/{id}`, tag `ticket-{id}`.

**Primo giro dopo l'avvio**: registra `last_article_id` correnti **senza notificare**.
**OTRS giù**: giro saltato (log); il successivo riparte dallo stesso `since`.
`sw.js`: le notifiche con tag `ticket-*` aprono il loro URL; la ricevuta `/push/ricevuta` resta solo per gli avvisi.

## Configurazione in OTRS (prima dell'uso)

- Agente dedicato con permessi `ro` (ricerca e lettura) e `rw` (risposta e riapertura) sulla coda, oltre a `create`.
- Web service con `TicketSearch` (POST) e `TicketGet` (GET `/Ticket/:TicketID`), oltre a `TicketCreate` e `TicketUpdate`.

## Test

- `internal/config`: route nuove, `:TicketID` obbligatorio.
- `internal/otrs` (`httptest`): richieste di `Mine` (coda, mail, stati, chiusi da 7 giorni); filtro degli articoli interni; `ErrNotYours` con mail diversa o coda diversa; `Reply` con riapertura dei chiusi; sostituzione di `:TicketID`; errori HTML e `{"Error"}`; `Changed`; `Attachment` solo da articoli visibili.
- `internal/database`: migrazione v13; `seen`; `notify_state`; `ticket_users`; conteggio delle risposte; pulizia.
- `internal/web` (client finto): widget presente/assente; pallino; `ticket_users` aggiornato; pagina di un ticket altrui → «non disponibile»; nessuna nota interna nell'HTML; risposta riuscita, limite, `in_corso`, riapertura; allegato di un ticket altrui rifiutato; header degli allegati.
- Notifiche: primo giro muto; una sola notifica per articolo di operatore; solo all'utente giusto; nessuna notifica per le risposte del cliente; OTRS giù non perde risposte.
- JS: sintassi; prova Playwright della pagina contro il mock.
