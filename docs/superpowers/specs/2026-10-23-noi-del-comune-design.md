# «Noi del Comune»: numeri di gruppo e traguardi personali in plancia

Data: 2026-10-23. Stato: bozza da approvare.

## Intento

Rendere l'uso del cruscotto un po' più coinvolgente **senza gara**: l'obiettivo è il senso di far parte di un gruppo («oggi siamo in 87»), non la competizione. Siamo un ente pubblico: niente classifiche tra persone, niente orari, niente che sembri controllo sui dipendenti. Deve essere carino e discreto, non invasivo.

Un effetto collaterale voluto: i traguardi personali spingono i comportamenti che servono all'ente (notifiche attive, app installata, avvisi letti).

## Obiettivi

1. **Pastiglia in testata** «Oggi siamo in N», accanto al saluto, visibile a tutti (anche agli anonimi).
2. **Traguardi collettivi**: quando si supera una soglia di accessi totali, per quel giorno la pastiglia diventa «Abbiamo superato 10.000 accessi».
3. **«Letto dal X% dei colleghi»** sotto gli avvisi del carosello.
4. **Fiammella sulle tile** dei 3 applicativi più aperti negli ultimi 7 giorni.
5. **Widget «Il tuo mese»** (solo per gli utenti riconosciuti): giorni di cruscotto nel mese, le tue richieste di assistenza (inviate e risolte), spille, e in fondo la riga di gruppo dell'assistenza (richieste del mese, risolte, prima risposta tipica) calcolata da OTRS con un job pianificato e salvata nel DB.

## Non obiettivi

- Preferiti e ordinamento personale degli applicativi: progetto separato, si progetta dopo questo.
- Pagine admin con statistiche per persona.
- Aggiornamento in tempo reale dei numeri: valgono quelli del caricamento della pagina.

## Anti-obiettivi (se succede, abbiamo sbagliato anche se funziona)

- **Qualsiasi classifica o confronto tra persone**, anche anonimo («sei il 2°», «sei tra i primi 10», «sei sopra la media»).
- **Dati derivati dall'orario di accesso.** Si salva solo il *giorno*, mai l'ora: avvicinarsi al controllo a distanza dei lavoratori (art. 4 L. 300/1970) e al GDPR è il rischio principale.
- **Serie di giorni consecutivi** (streak): chi è in ferie la perde e la vive come un rimprovero.
- Numeri negativi o che mettono a disagio (ultimi posti, «non hai letto 5 avvisi»).
- Popup, suoni, animazioni insistenti. Niente dialog per i traguardi.
- Ticket «per persona» visibili ad altri: «chi apre più ticket» si legge come «chi crea più problemi».
- Endpoint o pagine che espongono dati di altri utenti.

## Decisioni prese con l'utente

- Tono: gruppo, non gara. Classifiche tra persone tolte.
- Layout **B**: gruppo nella testata (pastiglia), widget tutto personale nella colonna destra, con in fondo una riga sull'assistenza.
- Il widget **non si chiude** (sempre visibile).
- Statistiche OTRS: **job pianificato** con risultato salvato nel DB (la plancia non chiama OTRS per questi numeri).
- Fiammella sulle tile e «letto dal X%» nel carosello: approvati.
- Anonimi: solo la pastiglia.
- Letture: un avviso corto è letto all'apertura della plancia; un avviso lungo solo con «Ho letto», che si abilita dopo 5–7 secondi (qui 6); lo stato sta nel DB, non nel browser.
- Interruttore `STATS_ENABLED`, default acceso, e conservazione configurabile.

## Dati

Migrazione **v14** in coda (`migrations.go`, mai modificare le precedenti). Date come nel resto del DB; i giorni come `YYYY-MM-DD` del calendario di Europe/Rome (`s.loc()`).

```sql
-- Un accesso al giorno per utente riconosciuto. Solo il giorno, mai l'ora.
CREATE TABLE user_days (
	username TEXT NOT NULL,
	day      TEXT NOT NULL, -- YYYY-MM-DD, Europe/Rome
	PRIMARY KEY (username, day)
);
CREATE INDEX idx_user_days_day ON user_days(day);

-- Clic sulle tile, aggregati: nessun utente.
CREATE TABLE app_clicks (
	app_id INTEGER NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
	day    TEXT    NOT NULL,
	clicks INTEGER NOT NULL,
	PRIMARY KEY (app_id, day)
);

-- Traguardi collettivi raggiunti (una riga per soglia, scritta una volta).
CREATE TABLE milestones (
	threshold  INTEGER PRIMARY KEY,
	reached_on TEXT NOT NULL -- YYYY-MM-DD
);

-- Statistiche dell'assistenza per mese, dal job OTRS.
CREATE TABLE ticket_stats (
	month             TEXT PRIMARY KEY, -- YYYY-MM
	requests          INTEGER NOT NULL,
	resolved          INTEGER NOT NULL,
	median_first_sec  INTEGER,          -- NULL = nessun ticket con risposta
	updated_at        TEXT NOT NULL
);

-- Per ogni ticket della coda creato nel mese: prima risposta e chiusura.
-- Evita di rileggere da OTRS i ticket già risolti a ogni giro.
CREATE TABLE ticket_facts (
	ticket_id      TEXT PRIMARY KEY,
	created_at     TEXT NOT NULL,
	first_reply_at TEXT,
	closed_month   TEXT -- YYYY-MM della chiusura, NULL se aperto
);
```

**Pulizia** (nel giro giornaliero di `StartPresenceCleanup`): `user_days` e `ticket_facts` oltre `STATS_RETENTION_MONTHS` (default 13), `app_clicks` oltre 90 giorni. Gli utenti che AD non conosce più escono anche da `user_days`, come già succede per `user_presence`. Le nuove tabelle finiscono nel backup senza modifiche (`VACUUM INTO`).

## Comportamento

### Accesso del giorno

Dove oggi `handleDashboard` chiama `TouchPresence`, si aggiunge `INSERT OR IGNORE INTO user_days` (oggi in Europe/Rome). È valido solo per gli utenti riconosciuti e non anonimi. «Accessi» nei traguardi significa **giornate-utente**: la somma delle righe di `user_days`.

### Pastiglia «Oggi siamo in N»

- `N = COUNT(*) FROM user_days WHERE day = oggi`, in cache nel `Server` per 60 s.
- Si mostra nella testata della plancia, accanto al saluto, con l'icona Material `groups`. Il testo è «Oggi siamo in N». Con N < 2 la pastiglia non si mostra: niente «sei il primo», che sarebbe un dato sull'ordine di arrivo.
- La vedono tutti, anche gli anonimi e i non riconosciuti.
- Se oggi è stato raggiunto un traguardo (`milestones.reached_on = oggi`), al posto del conteggio compare «Abbiamo superato 10.000 accessi» con l'icona `celebration`. Se ce ne sono più d'uno, vale il più alto.

### Traguardi collettivi

- Soglie: 1.000, 2.500, 5.000, 10.000, 25.000, 50.000, 100.000, poi ogni 100.000.
- Il controllo si fa insieme all'inserimento in `user_days`, al massimo una volta al minuto: totale delle righe ≥ soglia e soglia non ancora in `milestones` → `INSERT OR IGNORE` con `reached_on` = oggi. Il contatore parte da 0 con la v14 (nessuno storico da importare), quindi ogni soglia si festeggia nel giorno in cui viene superata davvero. *Mia scelta.*
- La pulizia a 13 mesi abbasserebbe `COUNT(*)`: il totale è invece un **contatore che non cala**, nella tabella `counters (name TEXT PRIMARY KEY, value INTEGER NOT NULL)` con riga `accessi`. Cresce di 1 quando l'`INSERT OR IGNORE` in `user_days` aggiunge davvero una riga, nella stessa transazione.

### Letture degli avvisi: regole nuove (decise con l'utente)

Oggi un avviso risulta «letto» anche solo passandoci sopra col mouse per 500 ms («Continua a leggere» aperto in hover): quasi mai è vero. Le regole nuove servono sia alla percentuale sia alla spilla.

- **Avviso corto** (`needsMore` falso: tutto il testo è già nella card): per gli utenti riconosciuti, è letto quando la plancia viene caricata con quell'avviso nel carosello. La riga si scrive lato server in `handleDashboard`, per gli avvisi attivi e visibili a chi guarda, con `how = 'plancia'`. È un nuovo valore validato in Go, mostrato nella pagina Letture dell'admin come «In plancia».
- **Avviso lungo**: aprirlo («Continua a leggere», clic o hover) **non** segna più la lettura. Nella card espansa compare un bottone **«Ho letto»**, disabilitato per i primi **6 secondi** dall'apertura (si vede un breve conto alla rovescia o un riempimento, niente testo insistente). Il clic → `/avvisi/{id}/letto` con `come=conferma`. Se la card si chiude prima, il conto riparte alla prossima apertura.
- **Popup urgente**: anche il suo «Ho letto» si abilita dopo 6 secondi.
- **Pagina `/avvisi/{id}`**: resta lettura `apertura`, come oggi (ci si arriva dalla notifica, quindi l'apertura è voluta).
- **Stato salvato nel DB, non nel browser**: per gli utenti riconosciuti, un urgente con una riga in `alert_reads` non si ripresenta più, anche con un altro browser o con i dati del sito cancellati. Il server marca l'avviso già letto (per esempio con `data-letto`) e `dashboard.js` non apre il popup. Allo stesso modo, nella card espansa di un avviso già letto «Ho letto» non c'è più e compare «Letto» con la spunta. `localStorage` resta solo per gli anonimi. *Mia scelta:* per i riconosciuti un avviso modificato dopo la lettura non ripropone il popup (vale la prima lettura, come già oggi per `alert_reads`).

### «Letto dal X% dei colleghi»

- Nella card del carosello (`alert_card`), in basso, in piccolo e grigio: «Letto dal 78% dei colleghi».
- `X = letture dell'avviso / utenti attivi` (`ActiveUsernames`, ultimi 15 giorni), arrotondato all'intero.
- Si mostra solo per gli avvisi **pubblici** (senza righe in `content_audience`). Per quelli riservati il denominatore giusto richiederebbe AD per ogni utente.
- Si mostra solo con almeno 10 utenti attivi e con l'avviso iniziato da almeno 1 ora. Sotto il 10% non si mostra (anti-obiettivo: numeri che mettono a disagio).
- In cache per 5 minuti. Vale anche per il refresh HTMX del carosello.

### Fiammella sulle tile

- Nuovo endpoint pubblico `POST /app/{id}/clic`, che risponde sempre 200 (vincolo del proxy). `dashboard.js` lo chiama con `sendBeacon` al clic su `.tile-main` e sui link della tile verso l'app (non sulle guide).
- Si conta solo per gli utenti riconosciuti, al massimo 1 clic per utente, per app, per giorno. La deduplicazione avviene in memoria (insieme `username|app|giorno`, svuotato a cambio giorno): nel DB finisce solo `app_clicks.clicks += 1`, senza utente. Un riavvio può contare un secondo clic: va bene, è indicativo.
- Le app inesistenti o non visibili non si contano (stesso filtro della plancia).
- In plancia, le 3 app con più clic negli ultimi 7 giorni, **tra quelle visibili a chi guarda** e con almeno 5 clic, mostrano in alto a destra della tile l'icona `local_fire_department` (arancio tenue) con `title` e testo accessibile «Tra i più aperti questa settimana». In cache per 10 minuti.

### Widget «Il tuo mese»

Template `widget_mese`, nella colonna `.widgets` subito dopo `widget_assist` (o per primo se l'assistenza è spenta). C'è solo per gli utenti riconosciuti, non anonimi. Non si chiude. Il titolo è «Il tuo ottobre» (il mese in italiano, minuscolo).

Contenuto, dall'alto:

1. **Numero grande**: giorni con una riga in `user_days` nel mese corrente, con l'etichetta «giorni di cruscotto» (singolare con 1).
2. **Le tue richieste** (solo con `OTRS_URL`): «3 richieste inviate, 2 risolte questo mese». Le inviate sono le righe di `tickets_sent` con `kind = 'apertura'` del mese, dell'utente. Le risolte sono le stesse unite a `ticket_facts.closed_month = mese corrente`. Contano solo le richieste partite dal cruscotto. Se sono 0, la riga diventa «Hai bisogno di aiuto? Apri un ticket» con il link che apre il dialog.
3. **Spille** (icone Material in cerchio; quella presa è colorata, quella da prendere è grigia con il suggerimento):
   - `notifications_active` «Notifiche attive»: l'utente ha almeno una riga in `push_subscriptions` **oppure** `user_presence.permission = 'granted'` (copre le iscrizioni fatte prima del riconoscimento, con username vuoto). Con il Web Push spento (`VAPID_SUBJECT` vuota) vale solo il permesso. *Mia scelta.* Se non l'ha presa, il clic apre il popup delle notifiche di `notifiche.js` (`dialog.notify-ask` o `notify-blocked`).
   - `install_desktop` «App installata»: `user_presence.last_app_at` non NULL. Se non l'ha presa, il suggerimento spiega come installarla.
   - `task_alt` «Tutti gli avvisi del mese letti»: almeno un avviso iniziato nel mese, **ancora attivo** e visibile all'utente (`contentFilter`), e tutti hanno una riga in `alert_reads` dell'utente. Gli avvisi già scaduti non contano: chi era in ferie non perde la spilla per sempre. *Mia scelta.* Se non ci sono avvisi che contano, la spilla non si mostra.
   - `confirmation_number` «Prima richiesta di assistenza»: almeno una riga in `tickets_sent` (solo con `OTRS_URL`).
   - Il suggerimento sta in `title` e in un testo accessibile. Su touch serve un tocco che mostra il testo sotto le spille: niente hover obbligatorio.
4. **Riga dell'assistenza** (gruppo, separata da un filetto; solo con `OTRS_URL` e con una riga in `ticket_stats` per il mese): «Assistenza a ottobre: 64 richieste, 58 risolte · prima risposta di solito entro 2 ore». La prima risposta è la **mediana**, in tempo di calendario, arrotondata: «entro N minuti» sotto l'ora, «entro N ore» fino a 48, poi «entro N giorni». Se la mediana è NULL, quella parte non compare.

### Job statistiche OTRS

- `StartTicketStats(ctx)` in `main.go`, insieme agli altri. Parte all'avvio e poi ogni ora. Solo con `OTRS_URL` (non con il mock della demo, salvo dati finti di `NewDemoMock` per vedere la UI).
- Ambito: la coda configurata, mese corrente in Europe/Rome.
- Passi:
  1. `TicketSearch` dei ticket con `TicketCreateTimeNewerDate` = inizio mese → `requests`.
  2. `TicketSearch` con `StateType closed` e `TicketCloseTimeNewerDate` = inizio mese → `resolved`. Contano tutti gli stati di tipo closed, compreso `closed unsuccessful` e le chiusure fatte dall'utente dal cruscotto. Ogni ID trovato riceve `ticket_facts.closed_month` = mese. I ticket con `closed_month` = mese che non compaiono più nella ricerca (riaperti) tornano a NULL. *Mia scelta.*
  3. Per ogni ticket creato nel mese e **senza** `first_reply_at` in `ticket_facts`: `TicketGet` senza allegati, primo articolo **visibile** (`visibleTypes`) con `SenderType agent` e non `ToThirdParty`, **escluso il primo articolo del ticket** (l'apertura, che può essere dell'operatore) → `first_reply_at`. *Mia scelta.* I ticket che hanno già una risposta non si rileggono.
  4. Mediana di `first_reply_at - created_at` sui ticket del mese con risposta → `ticket_stats`.
- Errori: si logga e si tiene l'ultima riga salvata. Un ticket illeggibile non blocca gli altri (come `Changed`).
- Nuovo metodo nell'interfaccia `otrs.Client` (per esempio `Stats(ctx, from time.Time) (…)` oppure le primitive di ricerca e lettura): la forma è scelta del costruttore, ma la logica OTRS resta in `internal/otrs`. Nessun dato del contenuto dei ticket salvato: solo ID, date e mese di chiusura.
- Limiti: `TicketSearch` con `Limit` alto (10.000); al massimo 300 `TicketGet` per giro (il resto al giro dopo), così da non caricare OTRS.

## Interruttore e conservazione (deciso con l'utente)

- `STATS_ENABLED` (default **true**). Con `false`:
  - nessuna scrittura in `user_days` e `app_clicks`;
  - `/app/{id}/clic` risponde 200 senza fare nulla;
  - niente pastiglia, widget «Il tuo mese», fiammella, «Letto dal X%»;
  - nessun job delle statistiche OTRS.

  Le letture degli avvisi restano: c'erano già. I dati raccolti prima dello spegnimento restano fino alla pulizia.
- `STATS_RETENTION_MONTHS` (default **13**): conservazione di `user_days` e `ticket_facts`. `app_clicks` resta a 90 giorni fissi.
- Le due variabili vanno aggiunte nei tre posti soliti: `docker-compose.yml`, `.env.example` e codice (`internal/config`).

## Vincoli dal progetto

- Sempre 200 (proxy `revprx01`), `sendBeacon` best effort, CSP stretta (niente inline: nuovo JS in `dashboard.js`, stili in `plancia.css`).
- Identità dichiarata: questi dati sono **indicativi e solo per personalizzare**, come le letture. Nessuna autorizzazione dipende da loro.
- I nuovi dati raccolti (giorni di accesso, clic aggregati) rientrano nell'informativa dell'ente. **Prima del rilascio va informato il DPO** e va aggiornata l'informativa, se serve. Va scritto nel PR e nel README.
- Test: `internal/database` (conteggi, deduplicazione, pulizia, traguardi senza festeggiare dati vecchi), `internal/web` (pastiglia per anonimi e riconosciuti, widget assente per anonimi, `/app/{id}/clic` sempre 200 e dedup, percentuale nascosta per gli avvisi riservati o sotto soglia, fiammella solo su app visibili), `internal/otrs` (stats con il server finto). Test di `internal/web` in container (vedi CLAUDE.md).
- `CLAUDE.md`: aggiornare Architettura (tabelle v14, route pubbliche `/app/{id}/clic`, job).

## Lasciato al costruttore

- La forma esatta dell'API in `internal/otrs`.
- Le chiavi di cache e i tempi minori.
- I dettagli visivi (colori delle spille, posizione della fiammella), restando nello stile pastello della plancia e nella bozza B.
