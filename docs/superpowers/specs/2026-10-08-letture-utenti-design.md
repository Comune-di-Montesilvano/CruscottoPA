# Letture degli avvisi, consegna delle notifiche e pannello Utenti

Data: 2026-10-08 · Stato: da approvare · Versione prevista: 0.10.0 (dopo la 0.9.2 con l'avviso «Notifiche non attive»)

## Contesto e obiettivo

Gli avvisi arrivano in plancia e, se richiesto, come notifica. Oggi l'admin non sa **chi li ha letti**, **a chi la notifica è arrivata davvero** (il 2026-10-08 una notifica di prova risultava «inviata» ma non compariva) e **quali utenti usano la plancia**, l'hanno installata come app o hanno le notifiche attive.

**Obiettivi**

1. Per ogni avviso: chi l'ha letto (quando, come), a chi è stata inviata la notifica e se il browser l'ha ricevuta, chi tra gli utenti attivi non l'ha ancora letto.
2. Pannello «Utenti»: ultimo accesso, ultima apertura come app, notifiche (permesso e iscrizioni), utente attivo (accesso negli ultimi 15 giorni).

**Fuori ambito:** cronologia degli accessi, tracciamento della navigazione, statistiche aggregate, conferma di lettura con valore legale.

## Vincoli dichiarati nell'interfaccia

- **Identità dichiarata** (NTLM non verificato): «letto da» è un'indicazione, **non una prova**.
- **Dati personali dei dipendenti** (art. 4 L. 300/1970, GDPR): l'ente decide uso e informativa; il pannello lo ricorda. Minimizzazione: nessuna cronologia (ultimo accesso sovrascritto), letture e consegne esistono solo finché esiste l'avviso.
- «Aperta come app» = la plancia è stata aperta almeno una volta in modalità app installata (`display-mode: standalone`): il browser non dice se l'app è ancora installata.

## Decisioni

- **Letto** = «Ho letto» sul popup urgente, «Continua a leggere» nel carosello, oppure pagina `/avvisi/{id}` aperta da un utente riconosciuto. Conta solo la prima lettura (con il modo: `conferma` o `apertura`).
- **Notificato** = esito di ogni invio push per iscrizione (`inviata`, `non riuscita`, `scaduta` per 404/410) **più** la ricevuta del service worker (`ricevuta il`) quando mostra la notifica.
- **Utente attivo** = ultimo accesso negli ultimi **15 giorni** (costante nel codice).
- Conservazione: letture e consegne con `ON DELETE CASCADE` sull'avviso; presenza una riga per utente, sovrascritta.

## 1. Dati (migrazione v9)

```sql
CREATE TABLE alert_reads (
	alert_id INTEGER NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
	username TEXT    NOT NULL,           -- minuscolo
	read_at  TEXT    NOT NULL,           -- UTC 2006-01-02T15:04:05Z
	how      TEXT    NOT NULL,           -- conferma | apertura (validato in Go)
	PRIMARY KEY (alert_id, username)
);

CREATE TABLE alert_deliveries (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	alert_id    INTEGER NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
	username    TEXT    NOT NULL DEFAULT '',
	endpoint    TEXT    NOT NULL,       -- iscrizione (per collegare la ricevuta)
	service     TEXT    NOT NULL,       -- chrome-edge | firefox | edge-windows | safari | altro (dall'host)
	sent_at     TEXT    NOT NULL,
	status      TEXT    NOT NULL,       -- inviata | non_riuscita | scaduta
	received_at TEXT,
	UNIQUE (alert_id, endpoint)
);
CREATE INDEX idx_alert_deliveries_alert ON alert_deliveries(alert_id);

CREATE TABLE user_presence (
	username        TEXT PRIMARY KEY,   -- minuscolo
	name            TEXT NOT NULL DEFAULT '',
	last_seen_at    TEXT NOT NULL,
	last_app_at     TEXT,               -- ultima apertura come app installata
	permission      TEXT NOT NULL DEFAULT '', -- granted | denied | default | '' (non dichiarato)
	permission_at   TEXT
);
```

- `service` si ricava dall'host dell'endpoint: `fcm.googleapis.com` → Chrome/Edge, `*.notify.windows.com` → Edge (Windows), `updates.push.services.mozilla.com` → Firefox, `*.push.apple.com` → Safari.
- Un nuovo invio per lo stesso avviso e iscrizione aggiorna la riga (`UNIQUE`), non ne crea un'altra.

## 2. Raccolta

### 2.1 Presenza

- **Server**, in `handleDashboard`: utente riconosciuto (non anonimo) → `TouchPresence(username, name, now)`; scrive al massimo una volta ogni 5 minuti per utente (confronto con `last_seen_at`, una sola `UPDATE … WHERE last_seen_at < ?`).
- **Browser**, `dashboard.js` al caricamento: `navigator.sendBeacon("/presenza", form)` con `app=1|0` (`matchMedia("(display-mode: standalone)")`) e `permesso=granted|denied|default` (`Notification.permission`, se supportato). Il server aggiorna `last_app_at` (se `app=1`) e `permission`. Sempre **200**; anonimi e valori non validi ignorati. Protetto dalla stessa CSRF (`CrossOriginProtection`) delle altre POST.

### 2.2 Letture

- `POST /avvisi/{id}/letto` con `come=conferma|apertura`, chiamato da `dashboard.js` su «Ho letto» (popup urgente) e su «Continua a leggere» / apertura col mouse del carosello (prima apertura per avviso nella pagina). Sempre 200.
- `GET /avvisi/{id}` (pagina dell'avviso) registra `apertura` lato server per un utente riconosciuto, se l'avviso è visibile a lui.
- `INSERT OR IGNORE`: resta la prima lettura. Solo avvisi esistenti e visibili all'utente (stessa regola di plancia); altrimenti ignorato.

### 2.3 Consegne

- `Dispatcher` (e «Invia notifica di prova» non registra: la prova non è un avviso): dopo ogni `Send` registra la riga (`inviata` / `non_riuscita` / `scaduta`).
- `sw.js`, evento `push`: dopo `showNotification`, se il `tag` è `avviso-<id>`, `fetch("/push/ricevuta", {method: "POST", body: {tag, endpoint}})` con l'endpoint di `self.registration.pushManager.getSubscription()`. Il server imposta `received_at` sulla riga (alert, endpoint) se esiste e non ha già una ricevuta. Sempre 200 `{"ok":…}` come gli altri `/push/*`.

## 3. Admin

### 3.1 Avvisi

- Nella lista: «Letto da N · notifiche ricevute R/I» (I = inviate) con link a `/admin/avvisi/{id}/letture`.
- Pagina **Letture** (`GET /admin/avvisi/{id}/letture`, shell admin):
  - **Letto da**: nome (da `user_presence.name`, altrimenti username), data, modo.
  - **Notifiche**: utente, browser, inviata il, esito, ricevuta il (o «non confermata»).
  - **Non ancora letto da**: utenti attivi (accesso ≤ 15 giorni) che possono vedere l'avviso (`alertVisibleTo` con il profilo AD in cache) e non l'hanno letto. Calcolato su richiesta; con AD non disponibile, per gli avvisi riservati: «Elenco non disponibile: AD non raggiungibile».
  - Nota: «L'identità in plancia è dichiarata: questi dati sono indicativi, non una prova di lettura».

### 3.2 Utenti (`GET /admin/utenti`, voce nel menu con icona `badge`)

Tabella ordinata per ultimo accesso (più recenti prima), ricerca per nome/username, filtro «Solo attivi»:

| Colonna | Contenuto |
|---|---|
| Utente | nome e username |
| Stato | **attivo** (accesso ≤ 15 giorni) / inattivo |
| Ultimo accesso | data e ora relativa («oggi 10:12», «3 giorni fa») |
| App | ultima apertura come app, oppure «mai» |
| Notifiche | **attive** (permesso concesso e almeno un'iscrizione: browser elencati), **bloccate**, **da decidere**, **senza iscrizione** (permesso concesso ma nessuna iscrizione: da riaprire la plancia) |
| Ultima notifica ricevuta | data dell'ultima ricevuta registrata, oppure «—» |

In fondo: «Dati personali dei dipendenti: uso secondo l'informativa dell'ente. Nessuna cronologia: si conserva solo l'ultimo accesso.»

## 4. Errori e casi limite

- Utente anonimo o non riconosciuto: nessuna presenza, nessuna lettura (le chiamate rispondono 200 e non scrivono).
- Avviso eliminato: letture e consegne spariscono (cascade).
- Ricevuta per un avviso o un endpoint sconosciuto: ignorata.
- Più schede o più PC: la lettura resta la prima; la presenza prende l'ultimo accesso; più iscrizioni per utente sono elencate per browser.
- AD giù: pannello Utenti invariato (non usa AD); «Non ancora letto da» limitato come al §3.1.

## 5. Test

- `internal/database`: migrazione v9 su DB v8 con dati; `TouchPresence` (limite 5 minuti), `SetPresenceClient`, `MarkRead` (prima lettura, how valido), consegne (`RecordDelivery` upsert, `MarkReceived` una volta), cascade su avviso eliminato, `ActiveUsers(now)` (15 giorni).
- `internal/web`: `/presenza`, `/avvisi/{id}/letto`, `/push/ricevuta` (200 sempre, anonimi ignorati, avviso non visibile ignorato); pagina avviso registra apertura; pagina Letture (letto, notifiche, non letto da con gruppi); pannello Utenti (stati notifiche, attivo/inattivo, filtro).
- `internal/notify`: il dispatcher registra le consegne.
- JS (`dashboard.js`, `sw.js`): test Go sul file (sendBeacon, letto su «Ho letto»/«Continua a leggere», ricevuta nel push) + prova nel browser.
