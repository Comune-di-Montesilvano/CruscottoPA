# CruscottoPA

Portale Intranet per i dipendenti dell'ente: homepage e plancia di comando che centralizza i link agli applicativi web, ai portali e alle utility, affiancandoli a guide tecniche e FAQ contestuali. Ogni dipendente vede solo le card del proprio ufficio.

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

Al primo avvio esistono solo la categoria "Applicativi" con Rubrica e Webmail **senza indirizzo**: compaiono in plancia dopo averlo inserito.

`LDAP_HOST` è obbligatorio. Solo in sviluppo si può usare `LDAP_HOST=mock`, che accetta qualsiasi credenziale come amministratore: mai in produzione.

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
