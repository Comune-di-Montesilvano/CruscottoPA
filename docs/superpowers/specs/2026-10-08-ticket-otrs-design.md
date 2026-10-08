# Apertura ticket verso OTRS dalla plancia

Data: 2026-10-08 · Stato: da approvare · Versione prevista: 0.12.0

## Contesto e obiettivo

Il CED gestisce l'assistenza con **OTRS 5s**. Oggi i dipendenti aprono un ticket scrivendo alla casella dell'assistenza: il PostMaster crea il ticket nella coda unica e le risposte tornano per mail. I canali di assistenza (0.9) dicono già *dove* chiedere aiuto; manca il modo di chiederlo dalla plancia.

**Obiettivo:** una tile «Apri un ticket» che apre un dialog con un modulo guidato; il ticket nasce in OTRS identico a quelli arrivati via mail (stesso cliente, stessa coda), con eventuali screenshot o PDF allegati, e l'utente vede subito il numero.

**Fuori ambito (fase 2):** elenco «I miei ticket» e conversazione in plancia, risposta dalla plancia, notifiche delle risposte, scelta dell'applicativo nel modulo, ripiego via SMTP.

## Verifiche fatte su OTRS (2026-10-08)

Web service GenericInterface dedicato, provider HTTP::REST, provato a mano. Nome del web service e route sono decisi dall'admin di OTRS e arrivano a CruscottoPA come parametri (vedi Configurazione); quelle provate:

| Operazione | Route di prova | Esito |
|---|---|---|
| `Ticket::TicketCreate` | `POST /TicketCreate` | ticket di prova creati |
| `Ticket::TicketUpdate` | `PATCH /TicketUpdate` | articolo aggiunto, cliente impostato |
| `Ticket::TicketSearch` | `POST /TicketSearch` | ok (fase 2) |
| `Ticket::TicketGet` | `GET /Ticket/:TicketID` | ok, credenziali nel corpo JSON anche in GET (fase 2) |

Fatti emersi:

- Coda unica, configurata con `OTRS_QUEUE`: il nome distingue maiuscole e minuscole; una coda inesistente fa fallire la richiesta con 500.
- I dipendenti **non sono customer user** in OTRS: il PostMaster scrive la mail del mittente in `CustomerUserID` e `CustomerID`.
- **`TicketCreate` con `CustomerUser` = mail salva il cliente vuoto** (cerca il login in anagrafica e non lo trova). Un `TicketUpdate` successivo con `Ticket.CustomerUser` e `Ticket.CustomerID` = mail lo imposta: il ticket diventa identico a quelli via mail.
- L'agente che chiama il web service diventa **Owner** del ticket: serve un agente dedicato.
- `From` va mandato come `Nome Cognome <mail>`, altrimenti `FromRealname` è la sola mail.
- Davanti a OTRS c'è revprx01: route mancante, coda inesistente o corpo vuoto arrivano come **pagina HTML di cortesia 500/502**; gli errori applicativi (`AuthFail`, `MissingParameter`) come **200 + `{"Error":{"ErrorCode","ErrorMessage"}}`**.
- Il debugger del web service a livello `debug` registra il corpo delle richieste, password compresa: in produzione `DebugThreshold: error`.

Da fare lato OTRS prima del rilascio: web service definitivo e agente dedicato con permessi `create` e `rw` sulla coda (`TicketUpdate` richiede `rw` per impostare il cliente); chiudere i ticket di prova; verificare se il cliente riceve una mail di conferma (oggi non verificato: il messaggio della plancia non la promette).

## Configurazione

Nuove variabili (in `docker-compose.yml`, `.env.example` e `internal/config`):

| Variabile | Default | Note |
|---|---|---|
| `OTRS_URL` | vuota | URL base del web service, nella forma `https://<host>/otrs/nph-genericinterface.pl/Webservice/<nome>`. Vuota = modulo spento, tile assente. `mock` = client finto (solo con `LDAP_HOST=mock`) |
| `OTRS_ROUTE_CREATE` | `/TicketCreate` | route `POST` dell'operazione `TicketCreate` |
| `OTRS_ROUTE_UPDATE` | `/TicketUpdate` | route `PATCH` dell'operazione `TicketUpdate` |
| `OTRS_USER` | — | agente dedicato, obbligatorio se `OTRS_URL` è valorizzata e non `mock` |
| `OTRS_PASSWORD` | — | come sopra; mai nei log |
| `OTRS_QUEUE` | — | coda di destinazione, obbligatoria se il modulo è attivo (nome esatto: maiuscole e minuscole contano) |
| `OTRS_FALLBACK_EMAIL` | vuota | casella mostrata se OTRS non risponde o l'utente non può usare il modulo |

Validazione all'avvio: `OTRS_URL` diversa da `mock` deve essere `https://`, le route devono iniziare con `/`; utente, password e coda obbligatori; `OTRS_URL=mock` con `LDAP_HOST` diverso da `mock` = errore. Il riconoscimento utente (NTLM + directory) è un prerequisito: senza, la tile mostra solo il messaggio per gli anonimi.

## Client OTRS — `internal/otrs`

Unico pacchetto che conosce OTRS.

```go
type Client interface {
	Create(ctx context.Context, t NewTicket) (Created, error)
}

type NewTicket struct {
	Name, Email, Phone string
	Subject, Body      string
	Attachments        []Attachment
}

type Attachment struct {
	Filename, ContentType string
	Content               []byte
}

type Created struct {
	TicketID, TicketNumber string
	CustomerSet            bool // false se il TicketUpdate del cliente è fallito
}
```

`Create`:

1. `POST {OTRS_URL}{OTRS_ROUTE_CREATE}` con `UserLogin`, `Password`, `Ticket{Title, Queue, State:"new", Priority:"3 normal", CustomerUser: mail}`, `Article{Subject, Body, ContentType:"text/plain; charset=utf8", ArticleType:"webrequest", SenderType:"customer", From:"Nome <mail>"}`, `Attachment[]{Content (base64), ContentType, Filename}`. Il corpo dell'articolo è la descrizione seguita da una riga vuota e `Telefono / interno: …` (se presente).
2. `PATCH {OTRS_URL}{OTRS_ROUTE_UPDATE}` con `TicketID` e `Ticket{CustomerUser: mail, CustomerID: mail}`. Se fallisce: `CustomerSet=false`, errore nel log, `Create` riesce comunque (il ticket esiste).

Regole:

- `net/http` con timeout 30 s per chiamata, proxy da `HTTPS_PROXY` (come `guidesrc`), **nessun redirect seguito**.
- Risposta valida = 200 con `Content-Type` JSON e corpo che si decodifica. **Qualsiasi altra cosa è un errore** (la pagina HTML del proxy compresa). `{"Error":…}` = errore con `ErrorCode` nel log.
- Il nome del file allegato viene ripulito (solo il nome base, niente percorsi, massimo 100 caratteri).
- `UserLogin`/`Password` non compaiono mai nei log né negli errori restituiti.
- `otrs.Mock` per sviluppo e test: registra i ticket in memoria e restituisce numeri finti.

## Dati — migrazione v11

```sql
CREATE TABLE tickets_sent (
	id INTEGER PRIMARY KEY,
	username TEXT NOT NULL,
	name TEXT NOT NULL,
	email TEXT NOT NULL,
	subject TEXT NOT NULL,
	ticket_id TEXT NOT NULL,
	ticket_number TEXT NOT NULL,
	customer_set INTEGER NOT NULL,
	attachments INTEGER NOT NULL,
	created_at TEXT NOT NULL
);
CREATE INDEX tickets_sent_user ON tickets_sent(username, created_at);
```

- Registro dei ticket **inviati con successo**: chi (identità dichiarata), quando, numero. Il testo **non** si salva (sta in OTRS); gli allegati solo come numero.
- **Limite**: massimo 5 ticket per utente nell'ultima ora, contati su `tickets_sent`.
- Finisce nel backup con il resto del DB. Nessuna pulizia automatica: poche righe all'anno.

## Allegati

- Upload a pezzi da 512 KB come i media dell'admin, su route pubbliche per utenti riconosciuti: `POST /ticket/allegati` (avvio: nome, dimensione dichiarata) → `POST /ticket/allegati/{id}/pezzo?n=` → `POST /ticket/allegati/{id}/fine`. Sempre **200 + `{"ok":…}`**.
- Tipi ammessi, riconosciuti dal contenuto: **PNG, JPEG, WebP, PDF** (`%PDF-`). Limiti: **5 MB per file, 3 file, 10 MB in totale** per ticket.
- Ogni upload è legato allo username del cookie `cruscotto_utente`: un id caricato da un altro utente non vale. Un id si usa una sola volta.
- Temporanei in `UPLOAD_DIR/.tmp/ticket` (il backup salta le cartelle nascoste); cancellati dopo l'invio (riuscito o no) e, se abbandonati, da una pulizia che rimuove quelli più vecchi di 1 ora (all'avvio e a ogni nuovo upload).
- Nel dialog: bottone «Allega» e **incolla dagli appunti** (Ctrl+V di uno screenshot).

## Plancia

**Tile «Apri un ticket»**: presente solo con il modulo attivo; stile proprio (icona `confirmation_number`, testo «Apri un ticket al CED»). Posizione iniziale: prima tile della colonna degli applicativi; quella definitiva si decide in implementazione con gli screenshot. Visibile anche agli anonimi.

**Dialog** `dialog.ticket` (nuovo `web/static/js/ticket.js`, nessuno script inline):

- **Anonimo** o utente senza `mail` in AD: spiegazione (riconoscimento necessario, rimando alla guida per Firefox già esistente; oppure mail mancante in AD) e casella `OTRS_FALLBACK_EMAIL` se configurata. Niente modulo.
- **Riconosciuto**:
  - Richiedente: nome e mail da AD in **sola lettura** (una mail modificabile manderebbe le risposte di OTRS a un indirizzo qualsiasi).
  - Telefono / interno: precompilato da AD (`telephoneNumber`), modificabile, facoltativo, max 40 caratteri.
  - Oggetto: obbligatorio, max 120 caratteri.
  - Descrizione: obbligatoria, testo semplice, max 10.000 caratteri.
  - Allegati: elenco con «×» per togliere, barra di avanzamento.
  - Bozza in `sessionStorage` (oggetto, descrizione, telefono) finché la scheda resta aperta; cancellata dopo l'invio riuscito. Accesso a `sessionStorage` sempre in try/catch.
- Bottone «Invia» disattivato durante l'invio.

**Invio** `POST /ticket` (form: oggetto, descrizione, telefono, id degli allegati) → sempre **200 + JSON**:

| Risposta | Dialog |
|---|---|
| `{"ok":true,"numero":"…"}` | «Ticket **…** aperto. Le risposte arriveranno per mail a {mail}.» + «Chiudi» |
| `{"ok":false,"campi":{"oggetto":"…"}}` | errori sotto i campi |
| `{"ok":false,"errore":"anonimo"}` | messaggio per gli anonimi |
| `{"ok":false,"errore":"limite"}` | «Hai aperto molti ticket nell'ultima ora: riprova più tardi o scrivi a …» |
| `{"ok":false,"errore":"ad"}` | «Non riesco a leggere i tuoi dati dalla rete del Comune: riprova tra poco.» |
| `{"ok":false,"errore":"otrs"}` | «Il sistema di assistenza non risponde: riprova o scrivi a {casella}.» Il testo resta nel dialog |

Lato server: utente dal cookie `cruscotto_utente` (anonimo → `errore:"anonimo"`); **nome e mail dal profilo AD in cache** (`profileCache`), mai dal form; telefono dal form se presente, altrimenti da AD. Profilo non disponibile (AD giù) → `errore:"ad"`. CSRF coperta da `CrossOriginProtection`. Dopo un invio riuscito: riga in `tickets_sent`, temporanei cancellati.

## Admin

Pagina `/admin/ticket` (voce nel menu dell'admin), sola lettura:

- In cima lo stato: modulo attivo/spento, coda configurata, URL di OTRS (senza credenziali).
- Ultimi 200 ticket aperti dalla plancia: data, utente (nome e username), oggetto, numero con link allo zoom in OTRS (`{base OTRS}/index.pl?Action=AgentTicketZoom;TicketID={id}`, base ricavata da `OTRS_URL` togliendo `nph-genericinterface.pl/…`), numero di allegati. Righe con `customer_set = 0` evidenziate («Cliente non impostato in OTRS»).
- Nessun bottone di prova (creerebbe ticket veri).

## Errori e casi limite

- OTRS giù, risposta HTML, `{"Error"}` su `TicketCreate`: `errore:"otrs"`, `ErrorCode` nel log, nessuna riga in `tickets_sent`, temporanei cancellati.
- `TicketUpdate` del cliente fallito: invio riuscito, `customer_set = 0`, log; visibile nell'admin.
- Allegato non ammesso, troppo grande, oltre il totale o oltre il numero: rifiutato all'upload con messaggio accanto al file.
- Allegato di un altro utente o già usato: `ok:false` all'invio, campo allegati in errore.
- Doppio clic: bottone disattivato; gli id degli allegati sono monouso.
- Rischio accettato: con l'identità dichiarata (NTLM) si può aprire un ticket a nome di un collega; le risposte vanno comunque alla sua mail vera e il registro tiene traccia dello username.

## Test

- `internal/otrs` (`httptest`): richiesta di create attesa (campi, `From` «Nome <mail>», riga del telefono, allegati in base64, nome file ripulito); risposta ok; `{"Error"}`; pagina HTML 500; non JSON; timeout; redirect non seguito; update fallito → `CustomerSet=false` e create riuscita; password assente da log ed errori.
- `internal/config`: validazione delle variabili OTRS (https, credenziali, `mock` solo con LDAP mock).
- `internal/database`: migrazione v11 su un DB v10 con dati; inserimento e conteggio per il limite.
- `internal/web` (client finto): tile assente con modulo spento; anonimo → `errore:"anonimo"`; campi obbligatori e lunghezze; nome e mail dal profilo anche se il form li manda diversi; AD giù → `errore:"ad"`; limite oltre 5 in un'ora; upload a pezzi con tipi e limiti; allegato di un altro utente rifiutato; id monouso; temporanei cancellati dopo l'invio; OTRS in errore → nessuna riga; pagina admin con link allo zoom e righe evidenziate.
- JS: controllo di sintassi di `ticket.js` e prova Playwright del dialog contro il server mock (apertura, errori, allegato incollato, conferma col numero).
