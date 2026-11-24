# Contatti della Rubrica nella ricerca a tendina — design

Data: 2026-11-24. Dimensione: piccola, su due repository (Rubrica e CruscottoPA).

## Perché

Una sola ricerca in plancia: scrivendo un nome nella tendina del Cruscotto il
collega trova anche le persone della Rubrica (applicazione interna, repo
`Comune-di-Montesilvano/Rubrica`), ne **vede interno e mail** e con un clic
**apre la scheda in Rubrica**.

## Obiettivi

- Gruppo «Persone» nella tendina: nome, reparto, interno, mail.
- Clic sulla riga → `RUBRICA_URL/contacts/{uid}` in una nuova scheda.
- Rubrica irraggiungibile o lenta: la tendina funziona come oggi, senza il gruppo.

## Non obiettivi

- Nessuna copia dei contatti nel Cruscotto (né DB né indice nella pagina).
- Nessun numero di gruppo (Centralino, uffici): solo i contatti.
- Nessuna chiamata/mail diretta dalla riga: si apre la scheda.
- Nessun filtro per gruppi della plancia: la Rubrica è già pubblica in intranet.

## Anti-obiettivi

- La tendina che si blocca o rallenta perché la Rubrica non risponde.
- Testo cercato (nomi di persone) nei log o su GlitchTip.
- Un 4xx/5xx verso il browser (il reverse proxy lo sostituirebbe).

## Approcci considerati

- **A. Il server del Cruscotto fa da tramite — scelto.** CSP e proxy invariati,
  sempre 200 + JSON, il browser non deve raggiungere la Rubrica.
- B. Il browser chiama la Rubrica (CORS + `connect-src`): fragile per nomi/protocolli
  diversi, CSP da allargare.
- C. Copia periodica di tutta la rubrica nel Cruscotto: dati duplicati e in
  ritardo, più codice.

## Come

### Rubrica

- `GET /api/cerca?q=` pubblica come le pagine. `q` ripulito; meno di 2 caratteri
  → `[]`. Usa `SearchContactsWithGroups(q, 8)` (stessa ricerca della pagina:
  nome, mail, reparto, ruolo; disattivati già esclusi).
- Risposta `200 application/json`: `[{"uid","nome","reparto","interno","mail"}]`;
  `interno` = `LDAPExt` formattato come nelle pagine (`extList`), altrimenti
  `PrimaryNumber`, altrimenti vuoto. Errore interno → `500` (lo gestisce il
  Cruscotto, non arriva a un browser).
- Test dell'handler, versione della Rubrica aggiornata, release.

### Cruscotto

- Env `RUBRICA_URL` (es. `https://rubrica.example.it`, senza `/` finale; vuota =
  funzione spenta). Aggiunta in `docker-compose.yml`, `.env.example`, `config`.
  Solo `http`/`https`, validata all'avvio (non valida = errore di config).
- `GET /rubrica/cerca?q=` (pubblica, come la plancia): sempre `200` +
  `{"ok":true,"persone":[…]}`; spenta o errore → `{"ok":false,"persone":[]}`.
  Ogni voce porta anche `url` = `RUBRICA_URL/contacts/{uid}` (uid con
  `url.PathEscape`).
- Client in `internal/rubrica`: timeout 2 s, nessun redirect, risposta max 256 KB,
  massimo 8 voci accettate. Cache in memoria per testo normalizzato
  (minuscolo, spazi compattati): 2 minuti, al massimo 500 chiavi.
- Log: solo `slog.Warn("rubrica non raggiungibile", "err", …)` senza il testo
  cercato, al massimo una volta al minuto. Mai `slog.Error` (GlitchTip) per una
  Rubrica giù.
- `dashboard.js`: dopo 2 caratteri e 250 ms di pausa chiama `/rubrica/cerca`
  (annulla la richiesta precedente con `AbortController`); risposta arrivata per
  un testo non più attuale → scartata. Gruppo «Persone» dopo i risultati locali,
  prima di «Web»; riga = nome (titolo), reparto · interno · mail (sotto), icona
  `person`; Invio/clic apre la scheda in nuova scheda. Attivo solo se la plancia
  porta `data-rubrica` sull'input (presente quando `RUBRICA_URL` è impostata).
- Testo cercato verso la Rubrica solo dopo che l'utente ha scritto (come oggi per
  il gruppo «Web» al clic): accettato, è una ricerca interna.

## Test

- Rubrica: `/api/cerca` con `q` corto, con risultati (interno da `LDAPExt` e da
  `PrimaryNumber`), disattivati esclusi.
- Cruscotto: client con server HTTP finto (risposta ok, 500, timeout, corpo troppo
  grande, JSON rotto, redirect rifiutato); handler sempre 200; cache (seconda
  chiamata non raggiunge il server finto); spento senza `RUBRICA_URL`; `url`
  della scheda costruito e uid con caratteri speciali.
- `dashboard.js`: controllo di sintassi + test Go che legge il file (come da
  CLAUDE.md); prova nel browser con Playwright e Rubrica finta.

## Lasciato al builder

- Stile della riga «Persone» (riusare quello delle voci esistenti, colori solo nei
  token).
- Nome del package e dei tipi in Rubrica (seguire le convenzioni del repo).
