# Chat interna tra colleghi — design

Data: 2026-11-24. Stato: da approvare.

## Intento

Una chat **assolutamente informale** dentro CruscottoPA: un dipendente scrive a un collega invece di telefonargli. Non è un canale di lavoro: non vale come comunicazione ufficiale dell'ente né come documento amministrativo. Fa parte della plancia come «punto unico della giornata lavorativa».

Vincoli decisi dal responsabile:
- Teams bocciato, **nessun applicativo esterno** (niente Matrix, Rocket.Chat…): si costruisce dentro CruscottoPA.
- **Conservazione 30 giorni**: poi i messaggi spariscono.
- **Nessuna barriera all'inizio**: si scrive il primo messaggio senza login.

## Obiettivi

- Messaggi diretti **1:1** tra dipendenti noti ad AD.
- Avvio da ricerca a tendina (gruppo «Persone») e da un widget «Messaggi» in plancia.
- Arrivo in tempo reale (SSE esistente) e notifica push.
- Identità del mittente **dichiarata o verificata, e sempre mostrata come tale** al destinatario.
- Lettura protetta da login LDAP (una volta per browser).

## Non obiettivi (per ora)

- Stanze, gruppi, piazza generale (possibili dopo, sullo stesso motore).
- Allegati, immagini, file.
- Kerberos (resta la strada futura per togliere il login: vedi «Approcci»).
- Messaggi a chi non è in AD o non è dipendente.
- Ricerca nel testo dei messaggi.

## Anti-obiettivi (fallimento anche se funziona)

- **Controllo dei dipendenti**: niente stato «online», niente «letto alle…», niente statistiche per persona, l'admin **non legge** i messaggi (art. 4 Statuto dei lavoratori). Coerente con «Noi del Comune»: mai orari, mai confronti.
- **Impersonazione silenziosa**: un messaggio a nome di un collega che il destinatario non può distinguere da uno vero.
- **Chat che diventa canale di lavoro**: file, dati di cittadini, comunicazioni ufficiali.
- **Testo dei messaggi fuori dal DB**: mai nei log, in GlitchTip, nelle notifiche push.

## Approcci considerati

1. **Kerberos (SPNEGO)**: identità verificata e trasparente. Richiede SPN `HTTP/<nome del sito>` sull'account di servizio, keytab, policy Edge `DisableAuthNegotiateCnameLookup` e forse `large_client_header_buffers` su revprx01. Zero attrito, ma interventi su AD, GPO e proxy. **Rimandato**: lo strato «cookie verificato» di questo design è lo stesso che Kerberos produrrebbe, quindi si potrà aggiungere dopo senza toccare la chat.
2. **Login LDAP una volta per browser** — **scelto**, nella variante sotto (scrittura senza login, lettura con login).
3. **NTLM verificato via NETLOGON**: scartato (account macchina, Secure Channel, nessuna libreria Go matura).

Variante scelta dal responsabile: **scrivere non richiede login; leggere sì**. Le tre contromisure all'impersonazione (segno «non verificato», visibilità alla vittima, tracciabilità) sono la condizione per accettarla.

## Identità

Due livelli, sopra il cookie esistente `cruscotto_utente` (identità NTLM dichiarata):

- **Dichiarato**: utente riconosciuto via NTLM, senza login chat. Può **scrivere** (nuove conversazioni e risposte), ma non può **leggere** nulla: né cronologia, né risposte, né i messaggi ricevuti.
- **Verificato**: ha fatto il login LDAP della chat in quel browser. Cookie separato `cruscotto_chat` (firmato e cifrato con chiavi derivate da `SESSION_SECRET`, prefissi diversi da utente e admin), contenuto `{Username, VerifiedAt, Expires}`.
  - Durata **180 giorni, che si rinnova a ogni uso** (riscritto al massimo una volta al giorno).
  - Valido solo se `Username` coincide con quello di `cruscotto_utente`: se sul PC entra un altro utente NTLM, il cookie chat viene ignorato e il login richiesto di nuovo.
  - Login: nuova funzione in `internal/auth` (bind LDAP come `Authenticate`, ma **senza** i controlli `LDAP_REQUIRED_GROUP`/`LDAP_ADMIN_GROUP`, che riguardano l'accesso all'admin), stesso `RateLimiter` dell'admin con chiavi separate (`chat:<username>`). Si chiede **solo la password**: lo username è quello NTLM del cookie (mostrato nel riquadro: «Accedi come Mario Rossi»), mai scritto dall'utente, quindi niente login incrociati. Il bind con quella password è ciò che verifica l'identità dichiarata. In mock: qualsiasi password non vuota accettata.
  - Errori in pagina, **sempre 200** (il proxy sostituisce i 4xx).
  - Logout chat: cancella il cookie (link nel pannello chat).
- **Anonimi**: niente chat, nessun elemento visibile.

Il livello è **per messaggio**: `chat_messages.verified` = 1 se al momento dell'invio il mittente aveva un cookie chat valido.

## Contromisure all'impersonazione

1. **Segno «non verificato»**: i messaggi con `verified = 0` si vedono nel pannello del destinatario con un'etichetta discreta («⚠ non verificato») e un tooltip: «Scritto prima dell'accesso alla chat: non possiamo garantire che l'abbia scritto davvero lui o lei». Nella notifica push la stessa etichetta.
2. **Visibilità alla vittima**: ogni messaggio sta nella conversazione tra mittente e destinatario. Il vero titolare dell'account, al login, vede anche i messaggi «suoi» che non ha scritto e può aprire un ticket all'assistenza ICT (nessuna funzione di segnalazione nella chat: scelta del responsabile). Nel pannello i messaggi non verificati inviati da te portano la stessa etichetta, così l'anomalia salta all'occhio.
3. **Tracciabilità**: ogni messaggio conserva **IP reale** (`X-Real-Ip`, come `pcAddress`) e **nome PC** del cookie. Mai mostrati agli utenti né nell'admin (nessuna pagina li espone), cancellati col messaggio a 30 giorni. Si consultano solo sul DB, a seguito di un ticket per abuso e con le procedure dell'ente. L'avviso fisso della chat lo dice esplicitamente. (Conservarli senza interfaccia è una scelta del builder: senza, l'impersonazione non lascerebbe traccia.)

## Funzionamento

### Dove si vede
- **Ricerca a tendina**, gruppo «Persone»: accanto a ogni collega un'azione «Scrivi» (icona `chat`), solo se il collega può usare la chat. L'`uid` della Rubrica coincide con il `sAMAccountName` (confermato dal responsabile). Apre il pannello chat sulla conversazione con lui o lei.
- **Widget «Messaggi»** (`widget_chat`) nella colonna destra, sotto «Assistenza»: badge non letti, ultime conversazioni (nome e «hai X messaggi nuovi», **mai il testo** finché non si è verificati), bottone «Nuovo messaggio» con scelta del collega per nome e cognome in AD via `Directory.SearchUsers` (solo chi può usare la chat; funziona anche senza `RUBRICA_URL`). Il destinatario si valida sempre con `Directory.Lookup` sul `sAMAccountName`.
- **Pannello chat**: `dialog.chat` laterale (stile `dialog.ticket`), con elenco delle conversazioni a sinistra e il thread a destra; su mobile una vista alla volta. Pagina `/chat` come fallback e destinazione delle notifiche.

Senza login chat il pannello mostra: campo «Scrivi a …» funzionante, nessun messaggio precedente, e un riquadro «Per leggere i messaggi accedi con la password di Windows (una volta sola su questo PC)».

### Avviso fisso
In cima al pannello, sempre: «Chat informale tra colleghi. Non usarla per comunicazioni ufficiali né per dati personali di cittadini. I messaggi si cancellano dopo 30 giorni. Per i casi di abuso restano registrati PC e indirizzo di rete di chi scrive: apri un ticket all'assistenza.»

### Chi può usare la chat
Definito nell'admin **come la visibilità degli applicativi**: nuova pagina `/admin/chat` con solo il fieldset «Visibilità» (gruppi della plancia multiregola, `only`/`hide`, stessa UI e stessa logica `audience.Visible` usata da app, guide e avvisi; nessuna riga = tutti gli utenti AD). Salvataggio nel registro dell'admin (`auditRoutes`).
- Vale **in entrambe le direzioni**: può scrivere e ricevere solo chi è nel pubblico della chat. Chi è fuori non vede widget, «Scrivi» né pannello, e non compare nelle ricerche di destinatari.
- Mittente e destinatario si controllano con il profilo AD (`profileFor`, cache esistente) a ogni invio. Con AD giù: invio rifiutato (`errore: ad`), lettura dei messaggi esistenti consentita ai verificati.
- Il destinatario deve esistere in AD (`Directory.Lookup` sul `sAMAccountName`) anche se non ha mai aperto il Cruscotto: il messaggio aspetta. Non a se stessi.
- Se un utente esce dal pubblico, le sue conversazioni restano fino ai 30 giorni ma non può più scrivere né leggere.

### Messaggi
- Solo testo, **max 2000 caratteri**, emoji incluse. Niente Markdown: testo semplice, a capo conservati, URL `http(s)` resi cliccabili (nuova scheda, `rel=noopener noreferrer`).
- Niente modifica né eliminazione (scelta del builder: un'eliminazione permetterebbe a chi scrive a nome di un collega di cancellare le tracce; i 30 giorni bastano).
- Niente «letto». Lo stato «non letto» esiste solo **per il destinatario** (badge), mai mostrato al mittente.
- Niente stato online.

### Blocca
Ogni utente verificato può bloccare un collega dalla conversazione. Il bloccato può ancora scrivere (risposta normale, nessun errore: non sa di essere bloccato). I suoi messaggi si salvano con `hidden = 1`: li vede solo il mittente (il thread resta coerente, niente messaggi che spariscono), mai il destinatario; non contano nei non letti, non generano eventi né push. Sblocco dallo stesso menu. Tabella `chat_blocks`. Il blocco non ha scadenza (non è un contenuto della chat).

### Abusi
Nessuna funzione «Segnala» (scelta del responsabile: chi ha un problema apre un ticket). Restano il blocco e la tracciabilità di IP e PC.

### Arrivo e notifiche
- Invio: `POST /chat/messaggi` (sempre 200 + JSON `{ok, errore?}`). Errori: `anonimo`, `destinatario`, `lungo`, `limite`, `ad`.
- Evento SSE `chat` sull'hub esistente (`notify.Event{Kind: "chat"}`), solo alle connessioni del destinatario: contiene ID conversazione e nome del mittente, **mai il testo**. Il client verificato poi scarica i messaggi con `GET`.
- Push (se la scheda non è visibile, stesso meccanismo di ticket): titolo «Nuovo messaggio da Mario Rossi» (+ «non verificato»), corpo vuoto, tag `chat-<conversazione>` (le notifiche della stessa conversazione si sostituiscono). URL `/chat?c=<id>`.
- Anche lo stesso utente su un altro browser riceve l'evento (per aggiornare il proprio thread).

### Limiti
- 30 messaggi/minuto e 300/ora per mittente (in memoria).
- Nuove conversazioni: 50/giorno per mittente (frena lo spam a tappeto).
- Lettura: `GET /chat/conversazioni`, `GET /chat/conversazioni/{id}?prima=<id>` (paginazione a 50), sempre 200.

## Dati (nuova migrazione in coda)

```
chat_conversations(id, user_a, user_b, created_at, last_at)   -- user_a < user_b, UNIQUE(user_a,user_b)
chat_messages(id, conversation_id → cascade, sender, body, verified, hidden, ip, pc, created_at)
chat_unread(username, conversation_id → cascade, count)        -- solo per il destinatario
chat_blocks(blocker, blocked, created_at)                      -- PK(blocker,blocked)
```

Il pubblico della chat riusa `content_audience` con un tipo di contenuto nuovo (es. `chat`, ID fisso 1), senza tabelle proprie.

- Username in minuscolo (`sAMAccountName`).
- Pulizia nel giro giornaliero: messaggi con `created_at` più vecchio di 30 giorni; conversazioni senza messaggi. Nessun limite alla pulizia di `chat_blocks`.
- I dati della chat finiscono nel backup (stesso DB): accettato, i backup ruotano.
- Utenti che AD non conosce più (pulizia giornaliera esistente): via conversazioni, messaggi e blocchi che li riguardano.

## Privacy e log

- Testo dei messaggi **mai** in log, GlitchTip, push, risposta SSE. `chat` e `body` tra le chiavi personali di `errtrack`.
- L'admin non ha una vista delle conversazioni. Nessuna esportazione.
- Nota per l'informativa dell'ente (non è codice): chat informale, 30 giorni, IP e PC conservati per gli abusi, nessuna lettura delle conversazioni dall'admin.

## Feature flag

`CHAT_ENABLED` (default `false`, scelta del builder: interruttore di deploy, il pubblico si gestisce dall'admin): spento = nessuna route, nessun widget, nessuna azione «Scrivi». Da aggiungere in `docker-compose.yml`, `.env.example` e codice. Richiede la directory (`LDAP_BIND_DN` o mock) e il riconoscimento NTLM attivo.

## Test (indicazioni)

- Dichiarato può scrivere, non può leggere (nessun testo in nessuna risposta senza cookie chat).
- Cookie chat con username diverso da quello NTLM → ignorato.
- `verified` registrato correttamente e mostrato come etichetta.
- Blocco: messaggio accettato al mittente, non salvato per il destinatario, nessun evento.
- Evento SSE e push senza testo.
- Pulizia a 30 giorni.
- Pubblico della chat: chi è fuori non scrive, non riceve, non compare nelle ricerche; IP e PC mai in nessuna risposta HTTP.
- Nessuna risposta 4xx dalle route chat.

## Lasciato al builder

- Layout esatto di pannello e widget (rispettando token e tema scuro, CSP stretta, niente inline).
- Struttura dei file JS (`chat.js` nuovo, agganciato da `plancia_head` come gli altri).
- Nome e posizione esatta delle route oltre a quelle indicate.
