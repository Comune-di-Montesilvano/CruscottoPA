# Notifiche degli avvisi e plancia installabile (PWA)

Data: 2026-10-07 · Stato: in revisione · Sotto-progetto 3

## Contesto e obiettivo

Oggi un avviso urgente compare solo quando si apre la plancia (popup) o al refresh del carosello ogni 5 minuti. Chi ha la plancia in una scheda in secondo piano, o il browser chiuso, non se ne accorge.

**Obiettivo:**
- notifiche di sistema (Windows) per gli avvisi che lo richiedono, sia **con la plancia aperta** (anche in secondo piano) sia **con il browser chiuso**;
- plancia **installabile come app** (finestra propria, icona di CruscottoPA).

Le notifiche raggiungono solo chi può vedere l'avviso (gruppi della plancia, sotto-progetto 4).

## Decisioni

- **Due canali:** SSE + Notification API a plancia aperta (nessun servizio esterno); **Web Push** a browser chiuso (il server esce su Internet verso i servizi push dei browser).
- **Quali avvisi:** casella **"Invia notifica"** nel form (campo `notify` esistente), **spuntata automaticamente quando il livello è Urgente**, disattivabile; per gli altri livelli non spuntata, attivabile.
- **Una sola notifica per avviso**, quando diventa attivo (subito o all'orario di inizio se programmato), anche attraverso i riavvii.
- **Destinatari = chi può vedere l'avviso** (stessa regola della plancia: anonimo o profilo non disponibile → solo avvisi pubblici).
- **Permesso:** al primo accesso un **popup della plancia** invita ad attivare le notifiche; il clic su "Attiva" fa partire la richiesta del browser (che i browser mostrano solo dopo un gesto dell'utente). Se il permesso è già concesso (anche via policy di dominio), nessun popup e iscrizione automatica.

## 1. Dati (migrazione v6)

```sql
ALTER TABLE alerts ADD COLUMN notified_at TEXT; -- NULL = non ancora notificato

CREATE TABLE push_subscriptions (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	endpoint   TEXT    NOT NULL UNIQUE,
	p256dh     TEXT    NOT NULL,
	auth       TEXT    NOT NULL,
	username   TEXT    NOT NULL DEFAULT '', -- dal cookie utente; '' = anonimo
	created_at TEXT    NOT NULL,
	last_ok_at TEXT                         -- ultimo invio riuscito
);

CREATE TABLE vapid_keys (
	id          INTEGER PRIMARY KEY CHECK (id = 1),
	public_key  TEXT NOT NULL,
	private_key TEXT NOT NULL
);
```

- Gli avvisi già attivi al momento della migrazione vengono marcati come notificati (`notified_at = now`), per non inviare notifiche arretrate al primo avvio.
- Le chiavi VAPID si generano al primo avvio se la tabella è vuota; stanno nel DB, quindi nel backup (un ripristino conserva le iscrizioni valide).

## 2. Configurazione

| Variabile | Esempio | Significato |
|---|---|---|
| `VAPID_SUBJECT` | `mailto:supporto@comune.montesilvano.pe.it` | Contatto richiesto dallo standard VAPID (mailto: o https:). **Vuota = Web Push spento** (SSE e notifiche a plancia aperta funzionano comunque). |

In `docker-compose.yml`, `.env.example`, `internal/config`. Avviso all'avvio se il valore non inizia con `mailto:` o `https:`.

## 3. Componenti

**`internal/notify`** (nuovo pacchetto):
- `Dispatcher`: goroutine con un ciclo ogni **30 secondi**. Cerca gli avvisi `notify = 1`, attivi (`starts_at <= now < ends_at` o senza fine) e con `notified_at IS NULL`; per ciascuno imposta `notified_at` **prima** dell'invio (al massimo una notifica, mai due anche con un errore a metà) e lo passa a SSE e Web Push.
- `Hub` SSE: registro delle connessioni aperte (`chan`, username, cookie); `Broadcast(alert)` invia l'evento solo alle connessioni per cui l'avviso è visibile. Visibilità calcolata con la stessa logica della plancia (profilo AD in cache + `content_audience`).
- `Pusher`: invio Web Push con `github.com/SherClockHolmes/webpush-go` (cifratura RFC 8291, VAPID). Per ogni iscrizione a cui l'avviso è visibile: payload JSON `{"title","body","url","tag"}`, TTL 1 ora, urgenza `high` per gli urgenti. Risposte 404/410 → iscrizione cancellata; altri errori → log `Warn`, nessun nuovo tentativo.

**Endpoint** (`internal/web`):
- `GET /eventi` — SSE pubblico: `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `X-Accel-Buffering: no`; commento di heartbeat ogni **25 secondi** (nginx chiude dopo 60 s senza traffico); evento `avviso` con `{"id","title","level"}`. Massimo 2000 connessioni contemporanee (oltre: 503, il client riprova). Chiusura pulita allo shutdown del server.
- `GET /push/chiave` — chiave pubblica VAPID (404 se push spento).
- `POST /push/iscrizioni` — salva l'iscrizione (JSON del browser: endpoint, keys); username dal cookie utente se riconosciuto. `POST /push/iscrizioni/rimuovi` — rimuove per endpoint. Endpoint validato (solo `https://`, max 1024 caratteri). Risposta sempre 200/204 (il proxy riscrive i 4xx): errori come JSON `{"ok":false}`.
- `GET /sw.js` — service worker servito dalla root (scope `/`), `Cache-Control: no-cache`.
- `GET /manifest.webmanifest` — nome "CruscottoPA", `start_url: "/"`, `display: "standalone"`, colori del logo, icone 192 e 512 px.
- Admin: `POST /admin/notifiche/prova` — invia una notifica di prova alle iscrizioni dell'admin corrente (username della sessione) e mostra l'esito (iscrizioni trovate, inviate, errori).

**Front-end:**
- `web/static/js/notifiche.js` (incluso dalla plancia):
  - si collega a `/eventi` (`EventSource`); su `avviso`: notifica di sistema (`new Notification` se la pagina non ha il focus, altrimenti solo aggiornamento) e refresh immediato del carosello (`htmx.ajax` su `/partials/alerts`);
  - registra `/sw.js`;
  - **popup di primo accesso** (`<dialog class="notify-ask">`): mostrato se `Notification` e `serviceWorker` sono supportati, `Notification.permission === "default"`, nessun popup urgente aperto, e non è stato scelto "Non ora" negli ultimi 30 giorni (`localStorage`). "Attiva" → `Notification.requestPermission()` → se concesso, iscrizione push (`pushManager.subscribe` con la chiave VAPID) e invio a `/push/iscrizioni`;
  - se il permesso è già `granted` (anche da policy): iscrizione automatica e silenziosa;
  - link nel footer "Notifiche: attiva" / "Notifiche: disattiva".
- `web/static/sw.js` (servito come `/sw.js`): `push` → `showNotification(title, {body, icon, tag, data:{url}})`; `notificationclick` → porta in primo piano una plancia aperta o ne apre una su `url`.
- Icone PNG 192 e 512 px generate da `logo.svg` (come per la favicon), in `web/static/img/`.

**Admin, form avvisi:** casella "Invia notifica" (`notify`); `admin.js` la spunta quando si sceglie "Urgente" (solo se l'admin non l'ha toccata). Negli elenchi: "notificato il …" se `notified_at` è impostato. Modificare un avviso già notificato non rinvia la notifica.

**CSP:** invariata (`default-src 'self'` copre `connect-src` per SSE e `worker-src` per il service worker; nessuno script inline).

## 4. Errori

| Caso | Comportamento |
|---|---|
| SSE bloccato o interrotto dal proxy | `EventSource` riprova da solo; resta il refresh di 5 minuti |
| Permesso negato dall'utente | niente popup in futuro, nessuna notifica; link nel footer spiega come riattivarle dal browser |
| Browser senza `Notification`/`serviceWorker` | nessun popup, nessun link |
| `VAPID_SUBJECT` vuoto | Web Push spento: niente iscrizione push, SSE attivo |
| Servizio push risponde 404/410 | iscrizione cancellata |
| Servizio push irraggiungibile o 5xx | log `Warn`, avviso comunque marcato come notificato (nessuna raffica di ripetizioni) |
| AD non disponibile al momento dell'invio | per gli utenti senza profilo: solo avvisi pubblici (come in plancia) |
| Riavvio durante un invio | l'avviso è già marcato: nessun doppione (alcune notifiche possono andare perse, accettato) |
| Iscrizione con endpoint non https o troppo lungo | rifiutata (`{"ok":false}`) |

## 5. Test

- `notify`: il dispatcher seleziona solo avvisi `notify`, attivi e non notificati; marca prima di inviare; avviso programmato notificato all'orario di inizio (orologio finto); nessun doppione su più cicli.
- Visibilità dei destinatari: SSE e push rispettano Riservato/Nascosto e l'anonimo (solo pubblici).
- Hub SSE: registrazione/rimozione connessioni, broadcast filtrato, heartbeat, limite connessioni.
- Pusher con un servizio push finto (`httptest`): payload cifrato inviato, 410 → iscrizione cancellata, 500 → log e nessun ritento.
- Database: migrazione v6 (avvisi attivi esistenti marcati), iscrizioni (unicità endpoint, rimozione), chiavi VAPID generate una sola volta.
- Web: `/eventi` (header, primo heartbeat, evento ricevuto), `/push/*` (validazione, risposte 200 con JSON), `/sw.js` e manifest (tipo di contenuto, scope), form avvisi (casella, "notificato il"), notifica di prova admin.
- JS (popup, iscrizione, service worker): verificati nel browser (Playwright/Edge headless) e su Edge reale in produzione.

## 6. Verifica in produzione (prima del rilascio)

Lo streaming SSE attraverso nginx (`revprx01`) va provato sull'istanza di test: se il proxy bufferizza la risposta nonostante `X-Accel-Buffering: no`, gli eventi arrivano in ritardo; in quel caso il proxy va configurato (`proxy_buffering off` per `/eventi`) oppure ci si affida a Web Push + refresh.

## 7. Documentazione

`CLAUDE.md`: pacchetto `notify`, endpoint `/eventi`, `/push/*`, `/sw.js`, manifest, tabelle v6, `VAPID_SUBJECT`, policy Edge `NotificationsAllowedForUrls` per pre-autorizzare il sito sui PC del dominio.
