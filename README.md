<p align="center">
  <img src="web/static/img/logo.svg" alt="" width="112" height="112">
</p>

<h1 align="center">CruscottoPA</h1>

<p align="center">La homepage dei dipendenti dell'ente: avvisi, applicativi, guide e assistenza in un'unica plancia.</p>

---

CruscottoPA è il portale Intranet che i dipendenti trovano all'apertura del browser. La **plancia** riunisce:

- gli **avvisi** del giorno in un carosello (gli urgenti si aprono a tutto schermo), con notifiche nel browser;
- gli **applicativi** per categoria, ciascuno con le sue guide e i canali di assistenza;
- una **ricerca a tendina** su applicativi e guide, con ricerca web e sul sito dell'ente;
- il **calendario** con festività, chiusure dell'ente e scadenze;
- le **guide generali** e il widget **Assistenza** per aprire un ticket e seguire «I miei ticket».

Ogni contenuto si gestisce dal pannello `/admin`.

- Backend: Go (`net/http`, `html/template`)
- Database: SQLite embedded (`modernc.org/sqlite`, pure-Go, nessun CGO)
- Frontend: HTMX, installabile come PWA

## Avvio rapido (container)

```bash
cp .env.example .env
docker compose up -d          # oppure: podman compose up -d
```

L'immagine `ghcr.io/comune-di-montesilvano/cruscottopa:latest` viene scaricata da GitHub Container Registry. Per compilare dal sorgente: `docker compose up -d --build` (la versione mostrata sarà `dev-DEV`).

I dati (database SQLite e file caricati) vivono nel volume nominato `cruscottopa-data`, montato su `/data`.

### Portainer (git-stack)

Puntare lo stack a questo repository, file `docker-compose.yml`, e impostare le variabili di `.env.example` nella sezione *Environment variables* dello stack. Ogni variabile è commentata in `.env.example`.

## Riconoscimento dell'utente

Sui PC del dominio il browser si presenta da solo (NTLM, `NTLM_DOMAIN`): la plancia saluta per nome, mostra i contatti da Active Directory, il nome e l'indirizzo IP del PC e i contenuti destinati all'ufficio. Senza riconoscimento la plancia funziona lo stesso, con i soli contenuti pubblici.

L'identità così ottenuta è **dichiarata, non verificata**: serve a personalizzare la pagina, mai ad autorizzare. Il pannello `/admin` richiede sempre il login con le credenziali di dominio.

## Amministrazione

`/admin` richiede il login LDAP/Active Directory. Sono amministratori gli utenti del gruppo `LDAP_ADMIN_GROUP` o elencati in `ADMIN_USERS`. Da lì si gestiscono:

- **Avvisi**: urgente, manutenzione o novità, con periodo di visibilità, fonte e notifica facoltativa; il testo si scrive con un editor visuale (grassetto, elenchi, link, tabelle, immagini incollate). Per ogni avviso: chi l'ha letto e a chi è arrivata la notifica;
- **Applicativi**: titolo, indirizzo, categoria e icona (catalogo Material Icons, file caricato o URL);
- **Guide**: generali oppure agganciate a un applicativo; link esterno, testo scritto con l'editor, PDF caricato o file Markdown di un repository GitHub pubblico (riscaricato ogni `GUIDE_REFRESH_HOURS` ore, default 6, o con «Aggiorna ora»);
- **Categorie** e **Calendario** (chiusure dell'ente, anche ricorrenti, ed eventi; le festività nazionali sono già incluse);
- **Assistenza**: i canali di supporto («Problemi con …?») collegati agli applicativi;
- **Gruppi**: destinatari dei contenuti per attributi o gruppi di Active Directory, con anteprima dei membri;
- **Utenti** e **Ticket**: ultimi accessi alla plancia e ticket aperti da lì;
- **Ente**: nome, logo e indirizzi di ricerca.

Al primo avvio esistono solo la categoria «Applicativi» con Rubrica e Webmail **senza indirizzo**: compaiono in plancia dopo averlo inserito.

`LDAP_HOST` è obbligatorio. Solo in sviluppo si può usare `LDAP_HOST=mock`, che accetta qualsiasi credenziale come amministratore: mai in produzione.

## Notifiche

Gli avvisi con «Invia notifica» arrivano ai browser con la plancia aperta (eventi in tempo reale) e, con `VAPID_SUBJECT` impostata, anche a quelli chiusi tramite Web Push. Lo stesso canale avvisa delle risposte ai ticket.

## Ticket (OTRS)

Con `OTRS_URL` e le altre variabili `OTRS_*` la plancia apre i ticket nella coda configurata di OTRS, con allegati e i dati del PC, e mostra «I miei ticket» con la conversazione e la possibilità di rispondere. Senza `OTRS_URL` il modulo è spento e il resto funziona normalmente.

## Backup e ripristino

Da `/admin/backup`:

- backup automatici ogni `BACKUP_INTERVAL_HOURS` ore (default 24, `0` = disattivati), conservati a scalare: tutti gli ultimi 7 giorni, poi uno a settimana fino a 35 giorni, uno al mese fino a un anno;
- backup manuali con «Crea backup ora» (restano finché non li elimini);
- download di ogni backup (`.tar.gz` con database e file caricati);
- ripristino da un backup in elenco o da un file caricato, con conferma `RIPRISTINA`: prima viene salvato lo stato attuale come backup «pre-ripristino», poi il servizio si riavvia con i dati ripristinati.

Le copie stanno nel volume `/data`, cioè sullo stesso server: scaricane una periodicamente e conservala altrove.

## Sviluppo

```bash
LDAP_HOST=mock go run ./cmd/server   # http://localhost:8080
go test ./...
go vet ./...
```

Non serve un compilatore C: il driver SQLite è pure-Go. Con `OTRS_URL=mock` (solo insieme a `LDAP_HOST=mock`) la plancia mostra ticket di esempio.

L'editor visuale dell'admin (TipTap) si costruisce con Node **in container**: `sh scripts/editor.sh` (da Git Bash anteporre `MSYS_NO_PATHCONV=1`) scrive `web/static/vendor/`. Senza, l'admin usa una semplice casella di testo. L'immagine Docker lo costruisce da sola (stage `editor`).

## Versioni e release

Il tag git è la versione: `git tag -a v0.13.0 -m … && git push origin v0.13.0` avvia `release.yml`, che compila l'immagine iniettando il tag in `main.AppVersion` (visibile nel footer e in `GET /health`) e la pubblica su GHCR come `:<tag>` e `:latest`.

## Licenza

[EUPL-1.2](LICENSE) — © Comune di Montesilvano
