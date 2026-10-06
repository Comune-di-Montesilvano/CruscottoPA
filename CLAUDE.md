# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Progetto

**CruscottoPA** (module `github.com/Comune-di-Montesilvano/CruscottoPA`): portale Intranet / homepage dei dipendenti del Comune di Montesilvano. Card di applicativi (`links.is_guide=0`) e guide/FAQ (`is_guide=1`) visibili per gruppo (ufficio); pannello `/admin` HTMX per CRUD link/gruppi e spostamento profili tra gruppi. CI/CD ereditata da GoPulley.

Immagine: `ghcr.io/comune-di-montesilvano/cruscottopa` (minuscolo: GHCR/OCI lo richiedono). Binario/container/utente di sistema: `cruscottopa` (uid/gid 1001).

## Comandi

```bash
go run ./cmd/server                 # PORT=8080, DB_PATH=cruscotto.db
go build ./...
go vet ./...
go test ./...
go test -run TestName ./internal/database/
docker compose up -d --build        # build locale, versione "dev-DEV"
```

Driver SQLite `modernc.org/sqlite` (pure-Go, nome driver `"sqlite"`): build con `CGO_ENABLED=0`, nessun gcc su Windows. CGO serve solo per `go test -race` in CI. **Non passare a `mattn/go-sqlite3`** senza aggiornare Dockerfile (gcc/musl-dev, CGO_ENABLED=1) e sintassi DSN.

## Versione

`main.AppVersion` (default `dev`) è iniettata via `-ldflags "-X main.AppVersion=..."`. In `release.yml` vale il tag git (`github.ref_name`) o l'input manuale del `workflow_dispatch`; build locali via compose aggiungono `VERSION_SUFFIX=-DEV`. Esposta nel footer e in `GET /health` (`{"status","version"}`), anche come label OCI `org.opencontainers.image.version`. Tenere `softwareVersion`/`releaseDate` di `publiccode.yml` allineati al tag a ogni release.

## CI/CD

- `test.yml` (push/PR su `main`): build → vet → `go test -race` → Trivy fs **bloccante** (CRITICAL/HIGH). Il job si chiama `test`: è il required status check della branch protection — non rinominarlo.
- `release.yml` (tag `*` o manuale): build+push GHCR (`:<tag>` + `:latest`), Trivy image report-only → tab Security.
- Dependabot settimanale (gomod, docker, github-actions) con cooldown. Action pinnate per SHA.
- `.trivyignore`: solo falsi positivi verificati.

## Vincoli di deploy (Portainer git-stack su Podman rootless)

- **Nome file `docker-compose.yml`**, non `compose.yml`: Portainer git-stack lo richiede.
- **Nessun `env_file`**: il clone Portainer non ha `.env` → il pull fallirebbe. Ogni variabile va elencata in `environment:` come `${VAR}`. Una nuova env var va aggiunta in tre posti: `docker-compose.yml`, `.env.example`, codice.
- **`/data` = volume nominato `cruscottopa-data`, mai bind mount**: Portainer clona in un path per-commit che non possiede ("permission denied"); in rootless un bind mount richiederebbe anche `:Z`/`:U` e uid mapping.
- **`pull_policy: always`** insieme a `build:`: senza, il redeploy Portainer può riusare una build locale e mostrare `dev` in prod.
- **Container non root (uid 1001), porte > 1024**: Podman rootless non apre porte privilegiate. Niente socket Docker montato (rootless non lo espone).
- **Healthcheck senza wget/curl**: `HEALTHCHECK` esegue `/app/cruscottopa -healthcheck` (GET `/health` su 127.0.0.1).
- **Mai `docker cp` sul DB live**: file root-owned + `-wal`/`-shm` disallineati → "attempt to write a readonly database". Usare `VACUUM INTO` per snapshot.
- Rete: rete di default del progetto compose; nessuna rete esterna dichiarata (una rete `external` inesistente fa fallire lo stack).

## Identificazione utente — attenzione all'IP

La dashboard deve riconoscere l'utente (IP, header del proxy o cookie). L'IP sorgente **non è affidabile** in container:
- **Podman rootless** con port forwarder `rootlessport`/`slirp4netns` (default fino a Podman 4) **nasconde l'IP client**: tutte le richieste arrivano dal gateway interno. Con `pasta` (default Podman 5) l'IP è preservato. Verificare con `podman info --format '{{.Host.NetworkBackend}}'` / `podman info | grep -i pasta` prima di basarsi sull'IP.
- Dietro reverse proxy l'IP è quello del proxy: leggere `X-Forwarded-For`/header utente **solo** se `RemoteAddr` appartiene a una lista di proxy fidati (CIDR configurabile), altrimenti l'header è falsificabile da chiunque.

## Architettura

- `cmd/server/main.go`: config da env, routing `net/http` (pattern Go 1.22+ `"GET /path"`), graceful shutdown, flag `-healthcheck`.
- `internal/database`: SQL raw su `database/sql`, schema `CREATE TABLE IF NOT EXISTS` in `InitDB`. PRAGMA nel DSN (`foreign_keys`, WAL, `busy_timeout`) così valgono per ogni connessione del pool. Nuove colonne TEXT via `ALTER TABLE` → sempre `NOT NULL DEFAULT ''`.
- `web/templates` (parse all'avvio, relativo alla cwd: avviare dalla root del repo), `web/static` (`htmx.min.js` 2.0.4).
