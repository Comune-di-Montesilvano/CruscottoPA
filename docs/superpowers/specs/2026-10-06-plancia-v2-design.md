# Plancia v2 — grafica, carosello avvisi, widget calendario

Data: 2026-10-06 · Stato: in revisione · Base: branch `feat/backup` (PR #4)

## Contesto e obiettivo

La plancia del sotto-progetto 1 funziona ma è poco informativa e poco interattiva. Obiettivo: trasformarla in una **dashboard** — testata più curata, avvisi leggibili per intero in un carosello, urgenti impossibili da perdere, tile degli applicativi più grandi con dettagli al passaggio del mouse, colonna di widget (oggi, calendario, guide generali) estendibile in futuro.

Mockup di riferimento (visual companion): `.superpowers/brainstorm/*/content/stile-v2.html` (direzione finale), `avvisi-carousel.html` (variante A), `tile-grandi.html` (variante A), `dashboard-widget.html`.

## Decisioni prese

- Direzione grafica **1 "chiaro istituzionale"** (fascia blu, ricerca galleggiante, card bianche) con le **tile colorate pastello** della direzione 3.
- Avvisi: **carosello a card grande**, un avviso alla volta, testo completo leggibile; **urgenti a tutto schermo** alla prima visita.
- Tile: **variante A** (click apre l'app, scheda con descrizione e guide al passaggio del mouse/focus).
- Widget: **Oggi**, **Calendario** (festività nazionali + chiusure dell'ente/patrono + scadenze/eventi), **Guide generali**.
- Nome utente nel saluto: **non in questa fase** (arriva col sotto-progetto 4); il layout ne prevede il posto.
- Niente santo del giorno, niente riga di riepilogo separata (le informazioni sono nel widget Oggi).

## 1. Dati

### Migrazione 3

```sql
CREATE TABLE calendar_events (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    title       TEXT    NOT NULL,
    kind        TEXT    NOT NULL CHECK (kind IN ('closure', 'event')),
    starts_on   TEXT    NOT NULL,              -- AAAA-MM-GG
    ends_on     TEXT    NOT NULL,              -- AAAA-MM-GG, >= starts_on
    description TEXT    NOT NULL DEFAULT '',
    yearly      INTEGER NOT NULL DEFAULT 0 CHECK (yearly IN (0, 1))
);
CREATE INDEX idx_calendar_range ON calendar_events(starts_on, ends_on);

ALTER TABLE alerts ADD COLUMN source TEXT NOT NULL DEFAULT '';
```

- Date di calendario come **giorni interi** (`AAAA-MM-GG`), senza ora né fuso.
- `closure` = chiusura dell'ente (ponti, patrono, chiusure straordinarie); `event` = scadenza o evento.
- `yearly = 1`: l'evento si ripete ogni anno nelle stesse date (es. patrono). Durata massima 31 giorni. Un annuale del 29 febbraio negli anni non bisestili cade il 28.
- `alerts.source`: fonte mostrata nel carosello; vuota → "Servizio informatico". Prepara gli avvisi pubblicati dagli uffici (feature futura).

### Festività nazionali (calcolate, nessuna tabella)

1 gennaio (Capodanno), 6 gennaio (Epifania), Pasqua e Lunedì dell'Angelo (algoritmo di Gauss/Meeus per la Pasqua gregoriana), 25 aprile, 1 maggio, 2 giugno, 15 agosto, 1 novembre, 8 dicembre, 25 e 26 dicembre.

### Tinta delle tile

Colore di sfondo della tile = `icon_color` mescolato con il bianco all'85% (`tint(#rrggbb, 0.85)`), calcolato lato server. Colore non valido → grigio chiaro `#eef1f5`.

## 2. Layout e comportamento della plancia

### Testata

- Fascia con gradiente blu (`#0b3a7e → #1565c0 → #1e88e5`): nome portale + ente a sinistra, orologio `HH:MM` a destra; saluto grande per fascia oraria ("Buongiorno", con posto per ", Nome" vuoto finché non c'è il riconoscimento); data estesa.
- Ricerca galleggiante a cavallo della fascia, scorciatoia `/`, filtro client-side come oggi (app e guide, accent-insensitive).

### Carosello avvisi

- Card grande bianca con barra laterale colorata per livello (urgente rosso, manutenzione ambra, novità blu).
- Contenuto: badge livello, fonte, "pubblicato …" e scadenza "fino a …"; titolo grande; testo completo (a capo preservati, URL cliccabili via `linkify`).
- Un avviso alla volta, ordine per gravità e data. Rotazione ogni **12 s**; pausa al passaggio del mouse, con focus da tastiera e con il pulsante ❚❚; frecce ‹ › e tasti ← → quando il carosello ha il focus; indicatore a pallini con avanzamento.
- Un solo avviso → niente controlli né rotazione. `prefers-reduced-motion: reduce` → niente rotazione automatica.
- Nessun avviso attivo → carosello assente.
- Aggiornamento HTMX ogni 5 minuti (`/partials/alerts`): dopo lo swap il carosello riparte dall'avviso che era visibile (stesso id), se esiste ancora.
- Senza JavaScript: tutte le card impilate, leggibili.
- Link "Tutti gli avvisi" → nuova pagina pubblica `GET /avvisi` con l'elenco degli avvisi attivi (stesse card, impilate).

### Urgenti a tutto schermo

- Ogni avviso `urgent` attivo non ancora letto si apre in un `<dialog>` modale rosso sopra la plancia, con pulsante **Ho letto**; più urgenti → in sequenza.
- "Letto" salvato in `localStorage` con chiave `cruscotto:letto:<id>:<versione>`, dove `versione` = hash breve (8 caratteri esadecimali di SHA-256) di titolo, testo, livello e date, calcolato lato server (`alertVersion`) e messo in `data-version`. Nessuna colonna nuova. Se l'avviso viene modificato la versione cambia e il dialog si ripresenta.
- `localStorage` non disponibile → il dialog si mostra a ogni caricamento, senza errori.

### Tile (variante A)

- Card arrotondata (raggio 18 px) con sfondo pastello (`tint`), icona 48 px su riquadro bianco semitrasparente, titolo, descrizione su 2 righe (troncata), badge "N guide" se l'app ha guide.
- Click sulla tile → apre l'app in nuova scheda.
- Passaggio del mouse (dopo 300 ms) o focus da tastiera → scheda sotto la tile con descrizione completa, guide dell'app e pulsante "Apri ↗"; si chiude uscendo con il mouse, con Esc o perdendo il focus.
- Touch: il tocco sul badge "N guide" apre la scheda; il tocco sul resto della tile apre l'app. App senza guide: nessun badge, la scheda si apre solo con mouse/focus (descrizione completa).
- Senza JavaScript: la scheda si apre con `:hover`/`:focus-within` in CSS (senza ritardo).

### Colonna widget

Ogni widget è un template separato (`widget_oggi`, `widget_calendario`, `widget_guide`), per poterne aggiungere in futuro (ticket, appunti).

1. **Oggi**: numero del giorno in grande, giorno della settimana, "settimana N · giorno X di 365/366".
2. **Calendario**:
   - griglia del mese, settimane da lunedì, giorni dei mesi adiacenti in grigio;
   - oggi evidenziato; festivi nazionali e domeniche in rosso; chiusure dell'ente su fondo rosa; eventi con pallino ambra;
   - frecce mese precedente/successivo via HTMX: `GET /partials/calendario?mese=AAAA-MM` (mese non valido → mese corrente);
   - legenda (Festivo, Chiusura ente, Scadenza/evento);
   - "Prossimi": fino a 5 tra festività, chiusure ed eventi da oggi in avanti (anche nei mesi successivi), con data breve e titolo;
   - click su un giorno con eventi → popover con l'elenco di quel giorno.
3. **Guide generali**: come oggi.

### Responsive

- ≥ 1100 px: app 3 colonne + colonna widget 280 px.
- 900–1100 px: app 2 colonne + widget.
- < 900 px: widget sotto le app; < 640 px: tile su una colonna, carosello con frecce sotto il testo.

## 3. Admin

- **`/admin/calendario`** (convenzione sezioni HTMX): form con titolo (max 120), tipo (Chiusura ente / Evento-scadenza), dal, al (facoltativo, default = dal), descrizione (max 500), "si ripete ogni anno". Elenco "Prossimi" e "Passati (ultimi 30)". Voce "Calendario" nella sidebar.
- **Avvisi**: campo "Fonte" (facoltativo, max 60), colonna nell'elenco.
- **Panoramica**: chiusure dell'ente nei prossimi 14 giorni.

Validazioni: date `AAAA-MM-GG` valide; `al ≥ dal`; durata ≤ 31 giorni; titolo obbligatorio.

## 4. Struttura del codice

```
internal/calendar         Holidays(year) []Day; Easter(year) time.Time; Month(year, month, events, today) MonthView;
                          Upcoming(from, events, n) []Entry — logica pura, senza DB
internal/database         calendar.go: CRUD calendar_events, ListCalendarEventsBetween(from, to)
                          migrazione 3; alerts.source in Alert e nelle query
internal/web              dashboard.go (view model nuovo), calendar handlers (partial + admin),
                          render.go: tint, alertVersion
web/templates             dashboard.html riscritto, partials: alerts_carousel, urgent_dialogs, app_tile,
                          widget_oggi, widget_calendario, widget_guide; avvisi.html; admin_calendario.html
web/static/css/app.css    riscritto con i token della direzione 1
web/static/js/dashboard.js carosello, urgenti, scheda tile, popover giorno, cambio mese
```

## 5. Errori e casi limite

- Mese non valido nella query → mese corrente.
- Evento annuale del 29/02 in anno non bisestile → 28/02.
- Evento a cavallo di due mesi → mostrato in entrambi.
- `localStorage` assente → urgenti a ogni caricamento.
- JS disattivato → pagina pienamente leggibile (card impilate, schede tile via CSS).
- CSP invariata: nessuno script/stile inline; colori dinamici solo in attributi `style` (ammessi da `style-src-attr`).

## 6. Test

- `internal/calendar`: Pasqua per 2025 (20/04), 2026 (05/04), 2027 (28/03), 2038 (25/04); festività fisse; Pasquetta; griglia del mese (inizio lunedì, riempimento, 4/5/6 settimane); evento su più giorni a cavallo di mesi; annuale e 29/02; `Upcoming` con ordine e limite.
- `internal/database`: migrazione 3 (tabella e colonna con default su dati esistenti); CRUD eventi; query per intervallo con annuali.
- `internal/web`: `tint` (valori attesi e fallback); plancia (carosello con fonte e data-version, urgenti marcati, badge guide, widget presenti); `/partials/calendario` con mese valido/non valido; `/avvisi`; CRUD `/admin/calendario` con validazioni, solo admin, CSRF; campo fonte negli avvisi.
- Prova manuale Playwright: rotazione/pausa/frecce, urgente e "Ho letto" (non riappare al ricaricamento), scheda tile con mouse e Tab, cambio mese e popover giorno, layout desktop/tablet/telefono, nessuna violazione CSP.

## 7. Fuori scope

- Nome nel saluto e "letto" per persona lato server → sotto-progetto 4.
- Avvisi pubblicati dagli uffici, destinatari mirati → feature probabile.
- Widget ticket e appunti → futuri.
- Import/export iCal, notifiche sugli eventi del calendario.
