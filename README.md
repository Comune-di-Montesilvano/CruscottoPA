# CruscottoPA

Portale Intranet per i dipendenti dell'ente: una plancia con gli avvisi del giorno (gli urgenti si aprono a tutto schermo), gli applicativi con le loro guide, un calendario con festività, chiusure dell'ente e scadenze, e le guide generali. Ogni contenuto si gestisce dal pannello `/admin`.

- Backend: Go (`net/http`, `html/template`)
- Database: SQLite embedded (`modernc.org/sqlite`, pure-Go, nessun CGO)
- Frontend: HTMX

## Avvio rapido (container)

```bash
cp .env.example .env
docker compose up -d          # oppure: podman compose up -d
```

L'immagine `ghcr.io/comune-di-montesilvano/cruscottopa:latest` viene scaricata da GitHub Container Registry. Per compilare dal sorgente: `docker compose up -d --build` (la versione mostrata sarà `dev-DEV`).

I dati (database SQLite) vivono nel volume nominato `cruscottopa-data`, montato su `/data`.

### Portainer (git-stack)

Puntare lo stack a questo repository, file `docker-compose.yml`, e impostare le variabili di `.env.example` nella sezione *Environment variables* dello stack.

## Amministrazione

`/admin` richiede il login con le credenziali di dominio (LDAP/Active Directory). Sono amministratori gli utenti del gruppo `LDAP_ADMIN_GROUP` o elencati in `ADMIN_USERS`. Da lì si gestiscono:

- **Avvisi**: urgente, manutenzione o novità, con periodo di visibilità;
- **Applicativi**: titolo, indirizzo, categoria e icona (catalogo Material Icons, file caricato o URL);
- **Guide**: link a guide e FAQ, generali oppure agganciate a un applicativo (pulsante "?" sulla sua card);
- **Categorie**: raggruppamento delle card in plancia.
- **Calendario**: chiusure dell'ente (anche ricorrenti, come il patrono) ed eventi/scadenze; le festività nazionali sono già incluse.

Al primo avvio esistono solo la categoria "Applicativi" con Rubrica e Webmail **senza indirizzo**: compaiono in plancia dopo averlo inserito.

`LDAP_HOST` è obbligatorio. Solo in sviluppo si può usare `LDAP_HOST=mock`, che accetta qualsiasi credenziale come amministratore: mai in produzione.

## Backup e ripristino

Da `/admin/backup`:

- backup automatici ogni `BACKUP_INTERVAL_HOURS` ore (default 24, `0` = disattivati), conservati a scalare: tutti gli ultimi 7 giorni, poi uno a settimana fino a 35 giorni, uno al mese fino a un anno;
- backup manuali con "Crea backup ora" (restano finché non li elimini);
- download di ogni backup (`.tar.gz` con database e file caricati);
- ripristino da un backup in elenco o da un file caricato, con conferma `RIPRISTINA`: prima viene salvato lo stato attuale come backup "pre-ripristino", poi il servizio si riavvia con i dati ripristinati.

Le copie stanno nel volume `/data`, cioè sullo stesso server: scaricane una periodicamente e conservala altrove.

## Sviluppo

```bash
LDAP_HOST=mock go run ./cmd/server   # http://localhost:8080
go test ./...
go vet ./...
```

Non serve un compilatore C: il driver SQLite è pure-Go.

## Versioni e release

Il tag git è la versione: `git tag 0.1.0 && git push --tags` avvia `release.yml`, che compila l'immagine iniettando il tag in `main.AppVersion` (visibile nel footer e in `GET /health`) e la pubblica su GHCR come `:<tag>` e `:latest`.

## Licenza

[EUPL-1.2](LICENSE) — © Comune di Montesilvano
