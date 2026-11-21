# Errori su GlitchTip — design

Data: 2026-11-21. Stato: da approvare.

## Perché

Oggi gli errori del Cruscotto restano nello stdout del container (Portainer) e quelli del browser non si vedono affatto. Lo scopo è **raccogliere i bug eventuali**, anche quelli che nessun collega segnala, sul GlitchTip già usato dall'ente per gli altri applicativi (`bugtracker.provincia.pescara.it`, API compatibile Sentry). Prior art: ComunicaPA (`apps/backend/src/common/sentry.util.ts`, `all-exceptions.filter.ts`, contesto della richiesta).

## Obiettivi

- Ogni `slog.Error` del server diventa un evento su GlitchTip.
- Ogni panic in un handler HTTP diventa un evento (uno solo) con lo stack.
- Gli errori JavaScript non gestiti di plancia, pagine secondarie e admin (`error`, `unhandledrejection`) arrivano su GlitchTip passando dal server.
- GlitchTip conta gli «utenti colpiti» senza sapere chi sono.

## Non obiettivi

- Performance/tracing, session replay, breadcrumb, widget di feedback utente, source map (il JS non è minificato; il bundle `vendor/editor.js` sì, ma va bene così).
- `slog.Warn` e livelli inferiori: restano solo nei log (sono quasi tutti attesi: AD giù, OTRS lento, download guide).
- Regole di allerta in GlitchTip: si configurano lì, non nel codice.

## Anti-obiettivi (fallimento anche se "funziona")

- **Un dato identificante sul GlitchTip della Provincia**: username, nome, mail, telefono, nome del PC, IP, testo dei ticket, cookie, header, corpo delle richieste. Scelta del responsabile (2026-11-21): nulla di identificante.
- Il Cruscotto che rallenta, si blocca o non parte perché GlitchTip è irraggiungibile o il DSN è sbagliato.
- Un ciclo o una pioggia di eventi che esaurisce la quota (stesso errore JS su 300 PC ogni 5 minuti di refresh).
- Script di terzi in pagina o CSP allargata.

## Vincoli

- CSP stretta invariata: niente inline, nessun `connect-src` esterno. Il nuovo JS è un file in `web/static/js`.
- Reverse proxy: il nuovo endpoint risponde **sempre 200** con `{"ok":…}`.
- Uscita verso internet dal container solo via `HTTPS_PROXY` (già in `docker-compose.yml`).
- Nuove env in tre posti: `docker-compose.yml` (`${VAR}`), `.env.example`, codice (`internal/config`).
- Mai dati reali nei test né nel repo (niente DSN vero: solo nel `.env`/Portainer).
- Su Windows i test di `internal/web` vanno lanciati nel container.

## Approcci considerati

- **A. Solo server** (`sentry-go` + handler `slog` + recover): poco codice, ma gli errori JS restano invisibili. Scartato: lo scopo è raccogliere tutto.
- **B. Server + errori del browser inoltrati dal Cruscotto** — **scelto**. `errori.js` manda gli errori a `POST /errori` sullo stesso sito; il server li ripulisce e li inoltra. CSP invariata, i PC non devono raggiungere la Provincia, filtro dei dati personali e limiti in un solo punto. Costo: stack JS meno ricchi dell'SDK ufficiale (niente breadcrumb).
- **C. Server + SDK Sentry browser**: dati più ricchi, ma ~70 KB in `vendor/`, `connect-src` verso la Provincia, ogni PC deve raggiungerla e lo scrub avverrebbe nel browser. Scartato.

## Design

### Configurazione

- `SENTRY_DSN`: vuota = integrazione spenta (nessun client, `/errori` risponde `{"ok":true}` senza fare nulla, `errori.js` non incluso o inattivo). DSN non valida → **warning all'avvio** (`cfg.Warnings()` o al init del client) e integrazione spenta; mai uscita del processo.
- `SENTRY_ENVIRONMENT`: default `production`; in sviluppo `.env` con `sviluppo`.
- `release` = `main.AppVersion` (passata a `web`/inizializzazione).
- Trasporto HTTP di `sentry-go` con proxy da ambiente (`http.ProxyFromEnvironment`, cioè `HTTPS_PROXY`), invio asincrono, `sentry.Flush(2s)` nel graceful shutdown.
- `SendDefaultPII: false`, `ServerName` vuoto o fisso `cruscottopa` (l'hostname del container è un ID casuale: inutile, ma non personale — scelta del builder), nessuna integrazione automatica che legga richieste o variabili d'ambiente.

### Server: errori da `slog`

- `cmd/server/main.go` avvolge l'handler testuale esistente in un handler "tee": tutto va allo stdout come oggi; i record di livello `Error` producono anche un evento.
- Evento: `message` = messaggio di `slog` (es. `"ticket: registro"`), **fingerprint = messaggio + funzione chiamante** (dal PC del record `slog`) così lo stesso punto del codice fa un'unica issue anche se `err` cambia, e punti diversi non si fondono.
- `serverError` (`internal/web/render.go`, ~118 chiamanti, sempre `"errore interno"`) va cambiato perché il PC del record sia quello di **chi chiama** `serverError`, non `serverError` stesso (record costruito a mano con `runtime.Callers` saltando un frame e `Handler().Handle`). La firma resta uguale: i chiamanti non cambiano. Senza questo tutti gli errori interni finirebbero in un'unica issue. (Scelta di Claude.) Gli attributi (`err`, `ticket`, …) vanno in `extra`/contesto dopo lo scrub. Livello `error`, tag `origine=server`.
- Implementazione libera: handler scritto a mano su `sentry-go` (preferito: controllo totale su cosa esce) o `github.com/getsentry/sentry-go/slog` se si riesce a imporre fingerprint e scrub. Scelta del builder.
- Se la richiesta HTTP è nel contesto (`slog.ErrorContext`/hub nel contesto), si aggiungono metodo e percorso normalizzato; non serve convertire tutte le chiamate esistenti a `ErrorContext`.

### Server: panic

- Middleware di recover nella catena di `internal/web` (dopo `securityHeaders`, prima del mux): cattura il panic, manda **un** evento con lo stack (eccezione, non messaggio), scrive il log su stdout **senza** produrre un secondo evento via `slog`, risponde 500 (il proxy mostra la pagina di cortesia; ok).
- `http.ErrAbortHandler` non è un errore: si rilancia/ignora come fa `net/http`.
- Panic nelle goroutine di fondo (dispatcher, watcher, backup): fuori ambito; restano come oggi.

### Browser: `errori.js`

- File `web/static/js/errori.js`, incluso **per primo** (prima degli altri script, `defer` o no a scelta del builder purché catturi gli errori degli altri) in `plancia_head`, `admin_base.html` e ogni altra pagina con script propri (login compresa se ne ha).
- Ascolta `window` `error` e `unhandledrejection`. Raccoglie: messaggio (≤ 500 caratteri), stack (≤ 8 KB), file, riga, colonna, percorso della pagina (`location.pathname`, mai la query né l'hash), `navigator.userAgent` troncato.
- Scarta: `"Script error."` senza file (script di altri domini), stack o file con `chrome-extension://`, `moz-extension://`, `safari-extension://`, file non del sito.
- Limiti per caricamento di pagina: stessa firma (messaggio+file+riga) una sola volta, al massimo 10 invii.
- Invio con `navigator.sendBeacon` (fallback `fetch` `keepalive`) di JSON a `/errori`; nessun retry, nessun errore mostrato all'utente, mai un errore lanciato da `errori.js` stesso (tutto in `try/catch`).
- Attivo solo se l'integrazione è accesa: il template lo dichiara (es. attributo `data-errori` su `<html>`/`<body>` o inclusione condizionata con una funzione di template come `ente`; `plancia_head` è chiamato senza dati). Scelta del builder.

### Server: `POST /errori`

- Route pubblica, sempre **200** `{"ok":true|false}`. Passa da `http.CrossOriginProtection` come le altre (sendBeacon dallo stesso sito è ammesso; verificarlo in test).
- Corpo max 16 KB (`http.MaxBytesReader`), JSON con i campi sopra; campi mancanti o fuori misura → `ok:false` senza evento.
- Anti-pioggia: quello comune (sezione sotto). Scartato: `ok:true` in silenzio.
- Evento: livello `error`, tag `origine=browser`, `browser` (nome e versione ricavati dallo user agent se semplice, altrimenti UA troncato in `extra`), `pagina` = percorso normalizzato, stack nel campo `extra` o come stacktrace Sentry (frame `filename`/`lineno`/`colno`) a scelta del builder. Fingerprint = messaggio+file+riga.
- Utente: hash dello username del cookie `cruscotto_utente` se riconosciuto (vedi sotto); anonimo → nessun utente.

### Anti-pioggia comune (scelta di Claude)

Un solo componente davanti all'invio, per **tutti** i percorsi (`slog`, panic, browser): un errore del server che si ripete a ogni refresh di 300 PC deve pesare quanto uno del browser.

- Dedup in memoria per **fingerprint + hash utente** (anonimo = un solo "utente"): la stessa coppia al massimo una volta ogni 10 minuti. Così GlitchTip vede ogni utente colpito almeno una volta e il conteggio «utenti colpiti» resta sensato.
- Tetto globale (ordine: 60 eventi/minuto, token bucket) sopra il dedup.
- Memoria limitata: mappa con scadenza, al massimo ~5000 chiavi; oltre, le più vecchie escono (o si rifiuta l'evento: scelta del builder, purché la memoria resti limitata).
- Orologio iniettabile per i test.

### Errori all'avvio (scelta di Claude)

Il client si inizializza subito dopo `config.Load()` e il logger, **prima** di `database.Open`. Ogni `os.Exit(1)` dopo un `slog.Error` passa da un'uscita che fa prima `Flush` (2 s): DB, `UPLOAD_DIR`, backup, web e server HTTP che non partono arrivano su GlitchTip. Un loop di riavvii (`restart: unless-stopped`) manda un evento a ogni avvio (il dedup è in memoria e non sopravvive): va bene, GlitchTip li raggruppa e il conteggio stesso segnala il loop.

### Dati personali: scrub (unico punto, server)

Vale per eventi `slog`, panic e browser, applicato in `BeforeSend` (o equivalente) così niente sfugge:

- **Utente**: `user.id` = primi 16 caratteri esadecimali di HMAC-SHA256(chiave derivata da `SESSION_SECRET` con un'etichetta dedicata, username in minuscolo). Nessun altro campo `user` (niente `username`, `email`, `ip_address`; impostare `ip_address` vuoto/`{{auto}}` disattivato — verificare che GlitchTip non ricavi l'IP del container o del proxy: deve risultare assente o un valore fisso). Admin: stesso hash sullo username LDAP della sessione.
- **Richiesta**: solo metodo e percorso normalizzato. Segmenti numerici e ID esadecimali/casuali (≥ 8 caratteri) → `{id}`; `/uploads/<dir>/<file>` → `/uploads/<dir>/{file}`. Niente query, header, cookie, corpo, IP.
- **Testo libero** (messaggi, attributi, stack, messaggi d'errore di LDAP/OTRS/SQLite): sostituzione con regex di indirizzi mail → `<mail>`, IPv4/IPv6 → `<ip>`, `DOMINIO\utente` e `CN=…`/DN LDAP → `<utente>`/`<dn>`. Attributi di `slog` con chiave nota come personale (`user`, `username`, `utente`, `mail`, `email`, `pc`, `ip`, `name`, `nome`, `remote`) → scartati del tutto.
- Nessun tag con valori liberi provenienti dall'utente.

### Documentazione e rilascio

- `.env.example` e `docker-compose.yml` con `SENTRY_DSN`, `SENTRY_ENVIRONMENT`.
- `CLAUDE.md`: sezione breve (cosa va su GlitchTip, scrub, `/errori`, anti-pioggia), route pubbliche aggiornate.
- Informativa: con lo scrub non si inviano dati personali; nessuna modifica richiesta (da confermare col responsabile, non blocca il codice).
- Versione minore (0.20.0), `publiccode.yml` allineato.
- Prima dell'uso in produzione: creare il progetto `cruscottopa` in GlitchTip (lo fa l'utente: serve un permesso che il token di Claude non ha) e mettere la DSN in Portainer.

## Test

- Trasporto finto di `sentry-go` (`ClientOptions.Transport` che registra gli eventi) per:
  - `slog.Error` → un evento con fingerprint = messaggio + chiamante; `slog.Warn` → nessuno; due chiamanti diversi di `serverError` → fingerprint diversi.
  - anti-pioggia: stesso errore ripetuto → un evento per utente ogni 10 minuti, tetto globale, memoria limitata; vale anche per `slog` e panic.
  - panic in un handler → un solo evento con stack, risposta 500; `http.ErrAbortHandler` → nessun evento.
  - `/errori`: evento corretto; sempre 200; corpo > 16 KB, JSON rotto, extension → nessun evento; dedup 10 min e tetto per minuto (orologio iniettabile).
  - scrub: mail, IP, `DOMINIO\utente`, DN, query string, attributi personali spariscono; `user.id` = hash stabile, nessun `username`/`ip_address`; anonimo senza utente.
  - DSN vuota → nessun client, `/errori` 200 no-op; DSN non valida → warning e avvio regolare.
- `errori.js`: controllo di sintassi con Node e test Go che legge il file (presenza dei filtri sulle extension, dei limiti, di `sendBeacon`), come per gli altri JS.
- Template: `errori.js` incluso prima degli altri script in plancia e admin quando attivo.
- Prova manuale: DSN di un progetto di test su GlitchTip, errore JS provocato da console, `slog.Error` provocato (es. OTRS mock rotto), verifica sulla UI di GlitchTip che non compaiano IP/username.

## Lasciato al builder

- Struttura dei file (es. `internal/errtrack` per client, scrub, handler `slog`; handler HTTP e middleware in `internal/web`).
- Handler `slog` a mano o `sentry-go/slog`; stack browser come `extra` o stacktrace Sentry.
- Come `errori.js` sa se è attivo.
- Numeri esatti del tetto globale (ordine di grandezza: decine al minuto).
