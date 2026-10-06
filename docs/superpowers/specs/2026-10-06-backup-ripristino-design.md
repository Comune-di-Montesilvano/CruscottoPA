# Backup e ripristino

Data: 2026-10-06 · Stato: in revisione · Priorità: prima dei sotto-progetti 2–4

## Contesto e obiettivo

CruscottoPA conserva tutto nel volume `/data`: il DB SQLite (`cruscotto.db`) e i file caricati (`/data/uploads`: icone oggi, PDF e immagini delle guide col sotto-progetto 2). Serve poter tornare a uno stato precedente dopo un errore umano (cancellazioni, modifiche sbagliate) o un aggiornamento andato male, e poter spostare i dati su un'altra istanza.

**Obiettivo:** backup automatici e manuali di DB + uploads in un unico archivio, scaricabili dal pannello admin, e ripristino completo dal pannello (da un backup in elenco o da un archivio caricato), con validazione prima di toccare i dati.

Riferimento: `internal/backup` e il flusso di ripristino di Rubrica (VACUUM INTO, retention GFS, upload a chunk, `os.Exit(0)` dopo lo swap, riavvio via `restart: unless-stopped` — verificato funzionante in produzione su Portainer/Podman rootless).

## Decisioni

- Copie **solo nel volume** (`/data/backups`) + download manuale. Nessuna copia automatica fuori dal server: la protezione dalla perdita del server/volume è a carico di chi scarica le copie (avviso fisso nel pannello).
- Ripristino **completo** (DB + uploads), nessun ripristino parziale.
- Dopo lo swap il processo esce e il container viene riavviato dalla restart policy, come Rubrica.

## 1. Formato e contenuto

Un archivio `tar.gz` per backup, nome:

```
cruscotto-AAAAMMGG-HHMMSS-<tipo>.tar.gz      tipo ∈ auto | manuale | pre-ripristino
```

(data/ora nel fuso `TZ`). Contenuto:

| Percorso nell'archivio | Contenuto |
|---|---|
| `manifest.json` | `{"format":1,"app_version":"…","schema_version":N,"created_at":"RFC3339 UTC","kind":"auto","files":N}` — primo file dell'archivio |
| `cruscotto.db` | snapshot coerente via `VACUUM INTO` su un file temporaneo, mai copia del file a caldo |
| `uploads/…` | copia ricorsiva di `UPLOAD_DIR`, solo file regolari |

- `schema_version` = `PRAGMA user_version` del DB al momento del backup.
- Il file viene scritto come `…tar.gz.tmp` e rinominato solo a creazione completata: un file `.tmp` non compare mai in elenco e viene rimosso all'avvio.
- Limite accettato: un file caricato in `uploads` durante la creazione può non essere incluso (lo prende il backup successivo). Il DB è sempre coerente.

## 2. Pianificazione e retention

- Nuova env var `BACKUP_INTERVAL_HOURS` (default `24`, `0` = backup automatici disattivati). Da aggiungere a `docker-compose.yml`, `.env.example`, `internal/config`.
- Una goroutine controlla ogni minuto: se l'ultimo backup `auto` è più vecchio di `BACKUP_INTERVAL_HOURS`, ne crea uno. Effetto: all'avvio, se l'ultimo automatico è scaduto, il backup parte subito; un riavvio non fa saltare un giro. Si ferma con lo shutdown del server.
- Dopo ogni backup `auto` si applica la retention GFS **solo ai backup `auto`**:
  - ultimi 7 giorni: tutti;
  - 7–35 giorni: il più recente per settimana ISO;
  - 35–365 giorni: il più recente per mese;
  - oltre 365 giorni: eliminati.
- `manuale` e `pre-ripristino` restano finché un admin non li elimina.

## 3. Ripristino

### Origine

1. **Da elenco:** pulsante "Ripristina" su un backup esistente.
2. **Da archivio caricato:** upload a chunk da 512 KB (sotto il limite di default di 1 MB dei reverse proxy nginx):
   - `POST /admin/backup/upload` → crea una sessione, restituisce `id`;
   - `POST /admin/backup/upload/{id}/chunk?n=<indice>` → corpo binario, chunk in ordine (indice atteso, altrimenti 409);
   - `POST /admin/backup/upload/{id}/fine` → chiude il file e avvia la validazione.
   - Sessioni non completate entro 1 ora: file temporaneo rimosso.
   - Dimensione massima dell'archivio caricato: 2 GB.

### Validazione (prima di toccare qualsiasi dato)

L'archivio viene estratto in `/data/restore-tmp/` (svuotata prima e dopo). Rifiutato con messaggio chiaro se:

- non è un gzip/tar leggibile;
- manca `manifest.json` o `format` ≠ 1;
- una voce ha percorso assoluto, contiene `..`, è un link (simbolico o fisico) o non è un file/directory regolare, oppure non sta sotto `cruscotto.db` / `uploads/`;
- la somma delle dimensioni estratte supera 2 GB;
- manca `cruscotto.db`, l'header non è `SQLite format 3\0`, o `PRAGMA integrity_check` ≠ `ok`;
- `schema_version` del manifest **maggiore** di quella supportata dall'app (backup di una versione futura). Uno schema più vecchio è accettato: le migrazioni lo aggiornano all'avvio.

### Swap

1. Backup `pre-ripristino` dello stato attuale (come la creazione manuale).
2. Risposta HTTP: pagina "Ripristino in corso…" che interroga `/health` e ricarica `/admin/backup` quando il server risponde di nuovo (logica in `admin.js`, nessuno script inline).
3. In background, dopo l'invio della risposta:
   - chiusura del DB;
   - rimozione di `cruscotto.db-wal` e `cruscotto.db-shm`;
   - rename `restore-tmp/cruscotto.db` → `DB_PATH`;
   - rename `UPLOAD_DIR` → `UPLOAD_DIR.old`, rename `restore-tmp/uploads` → `UPLOAD_DIR`, rimozione di `UPLOAD_DIR.old`;
   - `exit(0)` (funzione iniettabile nei test).
4. Al riavvio: migrazioni normali; pulizia di `restore-tmp`, `uploads.old` e `*.tmp` residui.

La sessione admin sopravvive al riavvio (cookie cifrato con `SESSION_SECRET`, indipendente dal DB) — con `LDAP_HOST=mock` senza `SESSION_SECRET` no, perché il segreto è casuale a ogni avvio: accettato, è solo sviluppo.

## 4. Pannello `/admin/backup`

Nuova voce "Backup" nella sidebar admin.

- Banner fisso: "Le copie sono sullo stesso server: scaricane una periodicamente e conservala altrove."
- Pulsante **Crea backup ora** (tipo `manuale`).
- Elenco, più recenti prima: data, tipo, dimensione, versione app; azioni:
  - **Scarica** (`GET /admin/backup/{nome}`, `Content-Disposition: attachment`);
  - **Ripristina** (`POST /admin/backup/{nome}/ripristina`);
  - **Elimina** (`POST /admin/backup/{nome}/elimina`) — non per i backup `auto`, gestiti dalla retention.
- Sezione **Ripristina da file**: input file + barra di avanzamento (upload a chunk in `admin.js`).
- Conferma del ripristino: campo in cui digitare `RIPRISTINA`; senza, il server rifiuta (422).
- Nome file nelle route validato con regex `^cruscotto-\d{8}-\d{6}-(auto|manuale|pre-ripristino)\.tar\.gz$` (niente path traversal).

**Panoramica `/admin`:** avviso se l'ultimo backup riuscito ha più di 48 ore, oppure se l'ultimo tentativo automatico è fallito (con il messaggio d'errore, tenuto in memoria).

## 5. Errori e concorrenza

- Un solo backup/ripristino alla volta (mutex nel servizio): un'operazione concorrente riceve "Operazione già in corso" (409).
- Errore durante la creazione (disco pieno, I/O): file `.tmp` rimosso, errore loggato e mostrato in panoramica; nessun backup parziale in elenco.
- Errore durante lo swap dopo la chiusura del DB: log a livello error e `exit(1)`; il backup `pre-ripristino` resta disponibile per il ripristino manuale.

## 6. Struttura del codice

```
internal/backup        Service: Create(kind), List(), Open(name), Delete(name),
                       Prune(now), Validate(archivePath) (estrae in restore-tmp),
                       Restore(...) (swap + exit), Scheduler(ctx), stato ultimo esito
internal/backup/upload sessioni di upload a chunk (file temporanei + scadenza)
internal/web           handler /admin/backup*, template admin_backup.html
```

`database.DB` espone `Snapshot(path string) error` (`VACUUM INTO ?`) e `SchemaVersion()` (già presente); `database` esporta anche `CurrentSchemaVersion = len(migrations)`.

## 7. Test

- Creazione + lettura: archivio contiene manifest corretto, DB apribile con gli stessi dati, uploads identici byte per byte.
- Retention GFS con date simulate (manuali e pre-ripristino mai toccati).
- Validazione, un caso per regola: voce `../x`, percorso assoluto, symlink, manifest mancante, `format` 2, schema futuro, DB corrotto, file non gzip, archivio oltre il limite (limite ridotto nel test).
- Ripristino end-to-end con `exit` finto: DB e uploads sostituiti, `pre-ripristino` presente, `-wal`/`-shm` rimossi.
- Upload a chunk: ordine, chunk fuori sequenza (409), sessione scaduta, `fine` senza chunk.
- Handler: solo admin, CSRF cross-origin 403, nome file non conforme 404, conferma `RIPRISTINA` mancante 422, operazione concorrente 409.
- Scheduler: con intervallo scaduto crea un backup `auto`; con `0` non ne crea.

## 8. Fuori scope

- Copia automatica fuori dal server (SMB/SFTP/S3).
- Ripristino parziale (solo avvisi, solo guide…).
- Cifratura degli archivi. Oggi contengono solo contenuti del portale; col sotto-progetto 4 conterranno anche username e uffici (dati personali): per questo download e ripristino sono solo admin, i file sono scritti con permessi `0640` e il banner invita a conservarli in un luogo protetto. La cifratura sarà rivalutata con il sotto-progetto 4.
