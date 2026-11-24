# Registro delle modifiche dell'admin — design

Data: 2026-11-24. Dimensione: piccola modifica (design breve).

## Perché

Sapere **chi ha toccato cosa e quando** nel pannello `/admin`. Diventa necessario
con più persone che gestiscono i contenuti (redattori per ufficio, in roadmap).
Non serve vedere com'era prima né tornare indietro.

## Obiettivi

- Una riga per ogni azione **riuscita** che cambia qualcosa: quando, chi
  (username LDAP dell'admin), sezione, azione, titolo dell'oggetto.
- Pagina `/admin/registro` in sola lettura, visibile a tutti gli admin: dal più
  recente, filtro per persona e per sezione.
- Conservazione 12 mesi.

## Non obiettivi

- Nessuna versione precedente, nessun diff, nessun «annulla».
- Nessun login (né riuscito né fallito).
- Nessuna azione che non cambia nulla: anteprima Markdown (`/admin/anteprima`),
  anteprima membri (`/admin/gruppi/{id}/anteprima`), pezzi degli upload (media e
  backup: conta il salvataggio dell'avviso/guida o il ripristino), notifica di
  prova (`/admin/notifiche/prova`).
- Nessuna azione di sistema (refresh automatico delle guide, scheduler di backup,
  notifiche).

## Anti-obiettivi

- Una route `POST /admin/*` nuova che sfugge al registro senza che nessuno se ne
  accorga.
- Una riga illeggibile dopo l'eliminazione dell'oggetto (il titolo si salva come
  testo, non solo l'ID).
- Una riga per un tentativo rifiutato (422 / `X-Esito: non-valido`) o fallito
  (5xx).

## Approcci considerati

- **A. Punto unico in `requireAdmin` + titolo dall'handler — scelto.** Dopo
  l'handler, se il POST è riuscito, si scrive la riga; sezione e azione dal
  pattern della route (`r.Pattern`) con una tabella; il titolo lo passa l'handler
  nel contesto. Un test sulle route garantisce che ogni `POST /admin/*` sia
  mappato o escluso.
- B. Chiamata esplicita in ogni handler: precisa ma ~40 punti, una route nuova
  senza chiamata passa inosservata.
- C. Trigger SQLite: scartato, SQLite non conosce l'admin e registrerebbe anche le
  scritture di sistema.

## Come

- **Migrazione v18** `admin_log`: `id`, `at` (testo UTC, formato del package),
  `username`, `section`, `action`, `object_id` (testo, può essere vuoto o il nome
  di un backup), `title`. Indici su `at`, `username`, `section`.
- **Tabella delle route** (`internal/web/audit.go`): pattern → `{sezione, azione}`
  (es. `POST /admin/app/{id}/elimina` → `App`, `eliminato`; `POST /admin/app` →
  `creato`; `POST /admin/app/{id}` → `modificato`), più l'insieme dei pattern
  esclusi. Azioni: creato, modificato, eliminato, spostato, archiviato,
  ripristinato, aggiornato (guida GitHub «Aggiorna ora»), backup creato, backup
  eliminato, backup ripristinato, testata (attributi «Sotto il saluto»).
- **Esito**: `requireAdmin` passa già un `validationWriter`; si aggiunge il codice
  di stato e se è stato trasformato un 422. Riuscita = metodo POST, non 422,
  status < 400 (redirect 303 inclusi).
- **Titolo**: l'handler chiama `auditTitle(r, titolo)` (valore nel contesto
  scrivibile dall'handler). Per le eliminazioni l'handler legge il titolo prima di
  cancellare. Senza titolo la riga mostra «<Sezione> #<id>». ID e titolo per le
  creazioni: l'handler passa anche l'ID nuovo (`auditObject(r, id, titolo)`).
- **Errore di scrittura del registro**: `slog.Error` (va su GlitchTip), l'azione
  dell'admin resta riuscita.
- **Ripristino**: il DB viene sostituito e con lui il registro. Prima dello swap si
  scrive la riga in un file JSON accanto al DB (`<dir DB_PATH>/admin_log_pending.json`);
  all'avvio, dopo le migrazioni, se il file c'è la riga si inserisce e il file si
  cancella. Le righe scritte dopo la data del backup ripristinato si perdono:
  accettato.
- **Pagina** `/admin/registro` (shell `admin_top`/`admin_bottom`, voce nel menu):
  tabella Quando (data e ora di Roma) · Chi · Sezione · Azione · Oggetto; filtri
  `?chi=` e `?sezione=` (select con i valori presenti); paginazione a 100 righe.
  Nessun link all'oggetto (potrebbe non esistere più).
- **Pulizia**: nel giro giornaliero di `StartPresenceCleanup`, righe con `at` più
  vecchio di 12 mesi.
- **Backup**: la tabella fa parte del DB, quindi finisce nei backup come il resto.

## Test

- Test che scorre i pattern registrati sul mux (`POST /admin/*`, escluso login e
  logout) e fallisce se uno non è né mappato né escluso.
- Per ogni sezione: un'azione riuscita scrive una riga con sezione, azione, utente
  e titolo giusti; un'azione rifiutata (422) e una esclusa non scrivono.
- Eliminazione: il titolo è quello dell'oggetto prima della cancellazione.
- Ripristino: il file pending viene reinserito all'avvio e cancellato.
- Pulizia: righe oltre 12 mesi cancellate, le altre restano.
- Pagina: filtri e paginazione.

## Lasciato al builder

- Nomi esatti delle sezioni in italiano nella pagina (seguire le etichette del
  menu admin).
- Ordine delle colonne e stile della tabella (riusare quello di `/admin/ticket`).
