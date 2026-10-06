# Plancia + admin base — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Dashboard "Plancia" uguale per tutti (avvisi, applicativi con guide agganciate, guide generali) gestita da un pannello admin HTMX protetto da login LDAP.

**Architecture:** Binario Go unico. `internal/database` (SQLite modernc, migrazioni con `PRAGMA user_version`), `internal/config` (env), `internal/auth` (LDAP + rate limiter), `internal/icons` (catalogo Material Icons embedded), `internal/web` (handler come metodi di `Server` con dipendenze esplicite, template `html/template`, middleware). Frontend server-rendered + HTMX 2 + JS vanilla minimo, tutto self-hosted.

**Tech Stack:** Go 1.26, `modernc.org/sqlite`, `github.com/gorilla/sessions`, `github.com/go-ldap/ldap/v3`, HTMX 2.0.4, Material Icons (Apache 2.0).

**Spec:** `docs/superpowers/specs/2026-10-06-plancia-admin-design.md`

**Branch:** lavorare su `feat/plancia-admin` creato da `spec/plancia-admin`; a fine piano aprire una PR verso `main` (il check `test` è obbligatorio).

## Global Constraints

- Nessun contenuto hardcoded oltre al seed: categoria "Applicativi" + app "Rubrica" (`pack`/`contacts`) e "Webmail" (`pack`/`mail`), entrambe con `url=''`.
- Nessuna CDN: font, JS, CSS serviti da `web/static`.
- Nessuno `<script>`/`<style>` inline né attributi `on*=`; unica eccezione CSP `style-src-attr 'unsafe-inline'`.
- CSP globale esatta: `default-src 'self'; img-src 'self' https: data:; style-src 'self'; style-src-attr 'unsafe-inline'; script-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'`.
- Date in DB: stringhe UTC formato `2006-01-02T15:04:05Z`; visualizzate/inserite nel fuso `TZ` (default `Europe/Rome`).
- Nuova env var → tre posti: `docker-compose.yml` (`- VAR=${VAR}`), `.env.example`, `internal/config`. Niente `env_file`.
- Build `CGO_ENABLED=0`; nessuna dipendenza CGO.
- Testi UI in italiano.
- Commit in Conventional Commits, con trailer `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Prima di ogni commit: `go vet ./... && go test ./...` verdi.

## Review Focus

1. **URL senza schema** (`www.comune.it`) inserito da un admin → messaggio "Inserisci l'indirizzo completo, es. https://…", non salvataggio silenzioso né link rotto. Test in Task 10 (`validate_test.go`).
2. **Prima installazione**: le app del seed non hanno URL → la plancia deve mostrare "Nessun applicativo configurato" invece di una pagina vuota. Test in Task 8.
3. **Fuso orario/ora legale** negli avvisi: `2026-03-29T10:00` inserito in `Europe/Rome` deve diventare `08:00Z` e tornare `10:00` nel form. Test in Task 13.
4. **Eliminazione di un'app con guide**: le guide diventano generali (`SET NULL`) e compaiono nella colonna; la conferma deve avvisarlo. Test in Task 11.
5. **Maiuscole in `ADMIN_USERS`** (`MRossi` vs `mrossi`) → stesso utente. Test in Task 6.

---

### Task 1: Configurazione da env

**Files:**
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`
- Modify: `cmd/server/main.go`, `docker-compose.yml`, `.env.example`, `Dockerfile`, `.gitignore`

**Interfaces:**
- Produces:
  ```go
  type LDAP struct {
      Host, BaseDN, UserDNTemplate string
      StartTLS, TLSSkipVerify      bool
      BindDN, BindPassword         string
      RequiredGroup, AdminGroup    string
      AdminUsers                   []string
  }
  type Config struct {
      Port, DBPath, UploadDir string
      SessionSecret           string
      SecureCookies           bool
      LogLevel                slog.Level
      Location                *time.Location
      LDAP                    LDAP
  }
  func Load() (Config, error)
  ```

- [ ] **Step 1: Crea il branch**

```bash
git checkout spec/plancia-admin && git checkout -b feat/plancia-admin
```

- [ ] **Step 2: Scrivi i test che falliscono** — `internal/config/config_test.go`

```go
package config

import (
	"log/slog"
	"strings"
	"testing"
)

var allVars = []string{
	"PORT", "DB_PATH", "UPLOAD_DIR", "SESSION_SECRET", "SECURE_COOKIES", "LOG_LEVEL", "TZ",
	"LDAP_HOST", "LDAP_BASE_DN", "LDAP_USER_DN_TEMPLATE", "LDAP_STARTTLS", "LDAP_TLS_SKIP_VERIFY",
	"LDAP_BIND_DN", "LDAP_BIND_PASSWORD", "LDAP_REQUIRED_GROUP", "LDAP_ADMIN_GROUP", "ADMIN_USERS",
}

func clearEnv(t *testing.T) {
	t.Helper()
	for _, v := range allVars {
		t.Setenv(v, "")
	}
}

func TestLoadDefaults(t *testing.T) {
	clearEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != "8080" || cfg.DBPath != "cruscotto.db" || cfg.UploadDir != "uploads" {
		t.Fatalf("default inattesi: %+v", cfg)
	}
	if cfg.LDAP.Host != "mock" || !cfg.LDAP.StartTLS || !cfg.SecureCookies {
		t.Fatalf("default LDAP/cookie inattesi: %+v", cfg)
	}
	if cfg.Location.String() != "Europe/Rome" || cfg.LogLevel != slog.LevelInfo {
		t.Fatalf("default TZ/log inattesi: %v %v", cfg.Location, cfg.LogLevel)
	}
	if len(cfg.SessionSecret) != 64 {
		t.Fatalf("in mock senza SESSION_SECRET serve un segreto casuale, ottenuto %q", cfg.SessionSecret)
	}
}

func TestLoadRealLDAPRequiresSecret(t *testing.T) {
	clearEnv(t)
	t.Setenv("LDAP_HOST", "ldaps://dc.example.local:636")
	t.Setenv("SESSION_SECRET", "troppo-corto")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "SESSION_SECRET") {
		t.Fatalf("atteso errore su SESSION_SECRET, ottenuto %v", err)
	}
	t.Setenv("SESSION_SECRET", strings.Repeat("a", 32))
	if _, err := Load(); err != nil {
		t.Fatalf("con segreto valido: %v", err)
	}
}

func TestLoadAdminUsers(t *testing.T) {
	clearEnv(t)
	t.Setenv("ADMIN_USERS", " mrossi ; gbianchi,, lverdi ")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"mrossi", "gbianchi", "lverdi"}
	if strings.Join(cfg.LDAP.AdminUsers, "|") != strings.Join(want, "|") {
		t.Fatalf("ADMIN_USERS: atteso %v, ottenuto %v", want, cfg.LDAP.AdminUsers)
	}
}

func TestLoadInvalidValues(t *testing.T) {
	for _, tc := range []struct{ key, val string }{
		{"SECURE_COOKIES", "forse"},
		{"LDAP_STARTTLS", "boh"},
		{"TZ", "Marte/Olympus"},
		{"LOG_LEVEL", "verbose"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			clearEnv(t)
			t.Setenv(tc.key, tc.val)
			if _, err := Load(); err == nil {
				t.Fatalf("%s=%q: atteso errore", tc.key, tc.val)
			}
		})
	}
}
```

- [ ] **Step 3: Verifica che fallisca**

Run: `go test ./internal/config/`
Expected: FAIL — `undefined: Load`

- [ ] **Step 4: Implementa** — `internal/config/config.go`

```go
// Package config legge la configurazione del server dalle variabili d'ambiente.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// LDAP raccoglie i parametri di connessione ad Active Directory.
type LDAP struct {
	Host           string // "mock" in sviluppo, altrimenti ldap://host:389 o ldaps://host:636
	BaseDN         string
	UserDNTemplate string // %s = username, es. "%s@comune.local"
	StartTLS       bool
	TLSSkipVerify  bool
	BindDN         string
	BindPassword   string
	RequiredGroup  string
	AdminGroup     string
	AdminUsers     []string
}

// Config è la configurazione completa del server.
type Config struct {
	Port          string
	DBPath        string
	UploadDir     string
	SessionSecret string
	SecureCookies bool
	LogLevel      slog.Level
	Location      *time.Location
	LDAP          LDAP
}

// Load legge le variabili d'ambiente, applica i default e valida i valori.
func Load() (Config, error) {
	cfg := Config{
		Port:          getEnv("PORT", "8080"),
		DBPath:        getEnv("DB_PATH", "cruscotto.db"),
		UploadDir:     getEnv("UPLOAD_DIR", "uploads"),
		SessionSecret: os.Getenv("SESSION_SECRET"),
		LDAP: LDAP{
			Host:           getEnv("LDAP_HOST", "mock"),
			BaseDN:         os.Getenv("LDAP_BASE_DN"),
			UserDNTemplate: getEnv("LDAP_USER_DN_TEMPLATE", "%s"),
			BindDN:         os.Getenv("LDAP_BIND_DN"),
			BindPassword:   os.Getenv("LDAP_BIND_PASSWORD"),
			RequiredGroup:  os.Getenv("LDAP_REQUIRED_GROUP"),
			AdminGroup:     os.Getenv("LDAP_ADMIN_GROUP"),
			AdminUsers:     splitList(os.Getenv("ADMIN_USERS")),
		},
	}

	var err error
	if cfg.SecureCookies, err = getEnvBool("SECURE_COOKIES", true); err != nil {
		return Config{}, err
	}
	if cfg.LDAP.StartTLS, err = getEnvBool("LDAP_STARTTLS", true); err != nil {
		return Config{}, err
	}
	if cfg.LDAP.TLSSkipVerify, err = getEnvBool("LDAP_TLS_SKIP_VERIFY", false); err != nil {
		return Config{}, err
	}
	if cfg.Location, err = time.LoadLocation(getEnv("TZ", "Europe/Rome")); err != nil {
		return Config{}, fmt.Errorf("TZ: %w", err)
	}
	if err = cfg.LogLevel.UnmarshalText([]byte(getEnv("LOG_LEVEL", "info"))); err != nil {
		return Config{}, fmt.Errorf("LOG_LEVEL: %w", err)
	}

	if cfg.LDAP.Host != "mock" && len(cfg.SessionSecret) < 32 {
		return Config{}, errors.New("SESSION_SECRET obbligatorio (almeno 32 caratteri) quando LDAP_HOST non è mock")
	}
	if cfg.SessionSecret == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return Config{}, fmt.Errorf("generazione SESSION_SECRET: %w", err)
		}
		cfg.SessionSecret = hex.EncodeToString(b)
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvBool(key string, fallback bool) (bool, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s: valore booleano non valido %q", key, v)
	}
	return b, nil
}

// splitList divide una lista separata da ';' o ',' scartando spazi e voci vuote.
func splitList(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ';' || r == ',' }) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
```

- [ ] **Step 5: Verifica che passi**

Run: `go test ./internal/config/ -v`
Expected: PASS (4 test)

- [ ] **Step 6: Usa `config.Load` in `main.go`**

In `cmd/server/main.go`: rimuovi il tipo `config`, `loadConfig` e `getEnv`; aggiungi gli import `"time"` (già presente), `_ "time/tzdata"` e `"github.com/Comune-di-Montesilvano/CruscottoPA/internal/config"`. Sostituisci l'inizio di `main()` con:

```go
func main() {
	healthcheck := flag.Bool("healthcheck", false, "esegue GET /health su localhost ed esce (usato dal HEALTHCHECK del container)")
	flag.Parse()

	// Il healthcheck non deve dipendere dalla validità del resto della configurazione.
	if *healthcheck {
		port := os.Getenv("PORT")
		if port == "" {
			port = "8080"
		}
		os.Exit(runHealthcheck(port))
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "configurazione:", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)
```

Il resto di `main()` resta invariato (usa ancora `cfg.Port` e `cfg.DBPath`).

- [ ] **Step 7: Variabili nei file di deploy**

`docker-compose.yml`, sotto `environment:` sostituisci il blocco esistente con:

```yaml
    environment:
      - PORT=${PORT:-8080}
      - DB_PATH=/data/cruscotto.db
      - UPLOAD_DIR=/data/uploads
      - TZ=${TZ:-Europe/Rome}
      - LOG_LEVEL=${LOG_LEVEL:-info}
      - SESSION_SECRET=${SESSION_SECRET}
      - SECURE_COOKIES=${SECURE_COOKIES:-true}
      - LDAP_HOST=${LDAP_HOST:-mock}
      - LDAP_BASE_DN=${LDAP_BASE_DN}
      - LDAP_USER_DN_TEMPLATE=${LDAP_USER_DN_TEMPLATE}
      - LDAP_STARTTLS=${LDAP_STARTTLS:-true}
      - LDAP_TLS_SKIP_VERIFY=${LDAP_TLS_SKIP_VERIFY:-false}
      - LDAP_BIND_DN=${LDAP_BIND_DN}
      - LDAP_BIND_PASSWORD=${LDAP_BIND_PASSWORD}
      - LDAP_REQUIRED_GROUP=${LDAP_REQUIRED_GROUP}
      - LDAP_ADMIN_GROUP=${LDAP_ADMIN_GROUP}
      - ADMIN_USERS=${ADMIN_USERS}
```

`.env.example`, aggiungi in coda:

```bash
# Livello di log: debug, info, warn, error
LOG_LEVEL=info

# ── Sessione admin ─────────────────────────────────────────────────────────
# Obbligatorio (min 32 caratteri) con LDAP reale. Genera con: openssl rand -hex 32
SESSION_SECRET=
# true = cookie solo su HTTPS. Dietro reverse proxy HTTPS viene dedotto da X-Forwarded-Proto.
SECURE_COOKIES=true

# ── LDAP / Active Directory (login /admin) ─────────────────────────────────
# mock = accetta qualsiasi credenziale (solo sviluppo)
LDAP_HOST=mock
#LDAP_HOST=ldaps://dc.example.local:636
LDAP_BASE_DN=dc=example,dc=local
# %s = username digitato. UPN: %s@example.local — DN classico: uid=%s,ou=Users,dc=example,dc=local
LDAP_USER_DN_TEMPLATE=%s@example.local
# Su ldap:// esegue StartTLS; se fallisce il login fallisce (nessun ripiego in chiaro)
LDAP_STARTTLS=true
LDAP_TLS_SKIP_VERIFY=false
# Account di servizio per la ricerca gruppi (vuoto = sessione dell'utente)
LDAP_BIND_DN=
LDAP_BIND_PASSWORD=
# CN del gruppo richiesto per il login (vuoto = tutti)
LDAP_REQUIRED_GROUP=
# CN del gruppo amministratori
LDAP_ADMIN_GROUP=
# Admin espliciti separati da ';'. In mock: vuoto = tutti admin.
ADMIN_USERS=
```

`Dockerfile`, nel blocco `ENV` aggiungi `UPLOAD_DIR=/data/uploads \` dopo `DB_PATH=/data/cruscotto.db \`.

`.gitignore`, nella sezione "Database locali" aggiungi la riga `/uploads/`.

- [ ] **Step 8: Verifica e commit**

```bash
go vet ./... && go test ./...
git add internal/config cmd/server/main.go docker-compose.yml .env.example Dockerfile .gitignore
git commit -m "feat(config): configurazione da env con validazione

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Database — apertura, migrazioni, seed

**Files:**
- Delete: `internal/database/sqlite.go`, `internal/database/sqlite_test.go`
- Create: `internal/database/db.go`, `internal/database/migrations.go`
- Test: `internal/database/db_test.go`
- Modify: `cmd/server/main.go`

**Interfaces:**
- Produces:
  ```go
  var ErrNotFound, ErrDuplicate, ErrCategoryNotEmpty error
  type DB struct{ *sql.DB }
  func Open(path string) (*DB, error)
  func (db *DB) SchemaVersion() (int, error)
  func formatTime(t time.Time) string        // UTC "2006-01-02T15:04:05Z"
  func parseTime(s string) (time.Time, error)
  func isUniqueViolation(err error) bool
  func checkAffected(res sql.Result, err error) error // ErrNotFound se 0 righe
  ```

- [ ] **Step 1: Rimuovi lo scaffold**

```bash
git rm internal/database/sqlite.go internal/database/sqlite_test.go
```

- [ ] **Step 2: Scrivi i test che falliscono** — `internal/database/db_test.go`

```go
package database

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestOpenAppliesMigrationsAndSeed(t *testing.T) {
	db := newTestDB(t)

	v, err := db.SchemaVersion()
	if err != nil || v != len(migrations) {
		t.Fatalf("versione schema: attesa %d, ottenuta %d (%v)", len(migrations), v, err)
	}

	var cats, apps int
	db.QueryRow(`SELECT COUNT(*) FROM categories`).Scan(&cats)
	db.QueryRow(`SELECT COUNT(*) FROM apps WHERE url = ''`).Scan(&apps)
	if cats != 1 || apps != 2 {
		t.Fatalf("seed: attese 1 categoria e 2 app senza URL, ottenute %d e %d", cats, apps)
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	for i := 0; i < 2; i++ {
		db, err := Open(path)
		if err != nil {
			t.Fatalf("Open #%d: %v", i+1, err)
		}
		db.Close()
	}
	db, _ := Open(path)
	defer db.Close()
	var apps int
	db.QueryRow(`SELECT COUNT(*) FROM apps`).Scan(&apps)
	if apps != 2 {
		t.Fatalf("seed ripetuto: attese 2 app, ottenute %d", apps)
	}
}

func TestForeignKeysEnforced(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.Exec(`INSERT INTO apps (category_id, title) VALUES (999, 'x')`); err == nil {
		t.Fatal("foreign_keys non attive: insert con category_id inesistente accettato")
	}
}

func TestTimeRoundTrip(t *testing.T) {
	in := time.Date(2026, 3, 29, 10, 0, 0, 0, time.FixedZone("CEST", 2*3600))
	s := formatTime(in)
	if s != "2026-03-29T08:00:00Z" {
		t.Fatalf("formatTime: %s", s)
	}
	out, err := parseTime(s)
	if err != nil || !out.Equal(in) {
		t.Fatalf("parseTime: %v %v", out, err)
	}
}
```

- [ ] **Step 3: Verifica che fallisca**

Run: `go test ./internal/database/`
Expected: FAIL — `undefined: Open`

- [ ] **Step 4: Implementa** — `internal/database/db.go`

```go
// Package database gestisce la persistenza SQLite di CruscottoPA.
package database

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite" // driver pure-Go, registra "sqlite"
)

var (
	ErrNotFound         = errors.New("database: record non trovato")
	ErrDuplicate        = errors.New("database: valore duplicato")
	ErrCategoryNotEmpty = errors.New("database: la categoria contiene ancora delle app")
)

// timeLayout è RFC 3339 in UTC a lunghezza fissa: le stringhe si confrontano
// correttamente anche in SQL (ordine lessicografico = ordine temporale).
const timeLayout = "2006-01-02T15:04:05Z"

func formatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

func parseTime(s string) (time.Time, error) { return time.Parse(timeLayout, s) }

// DB incapsula *sql.DB; i metodi di dominio sono nei file per entità.
type DB struct {
	*sql.DB
}

// Open apre (o crea) il database e applica le migrazioni mancanti.
// Le PRAGMA sono nel DSN così valgono per ogni connessione del pool.
func Open(path string) (*DB, error) {
	dsn := fmt.Sprintf(
		"file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)",
		path,
	)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("apertura database: %w", err)
	}
	if err := sqlDB.Ping(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("connessione database: %w", err)
	}
	db := &DB{sqlDB}
	if err := db.migrate(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("migrazioni: %w", err)
	}
	return db, nil
}

// SchemaVersion restituisce PRAGMA user_version.
func (db *DB) SchemaVersion() (int, error) {
	var v int
	err := db.QueryRow(`PRAGMA user_version`).Scan(&v)
	return v, err
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// checkAffected converte "nessuna riga toccata" in ErrNotFound.
func checkAffected(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
```

`internal/database/migrations.go`:

```go
package database

import (
	"database/sql"
	"fmt"
)

// migrations è l'elenco ordinato delle migrazioni: l'indice i porta lo schema
// dalla versione i alla i+1. Mai modificare una migrazione già rilasciata:
// aggiungerne una nuova in coda.
var migrations = []func(*sql.Tx) error{
	migrateV1Schema,
	migrateV2Seed,
}

func (db *DB) migrate() error {
	version, err := db.SchemaVersion()
	if err != nil {
		return err
	}
	for i := version; i < len(migrations); i++ {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if err := migrations[i](tx); err != nil {
			tx.Rollback()
			return fmt.Errorf("migrazione %d: %w", i+1, err)
		}
		// user_version è transazionale: se il commit fallisce resta la versione precedente.
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func migrateV1Schema(tx *sql.Tx) error {
	_, err := tx.Exec(`
CREATE TABLE categories (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	name       TEXT    NOT NULL UNIQUE COLLATE NOCASE,
	sort_order INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE apps (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	category_id INTEGER NOT NULL REFERENCES categories(id) ON DELETE RESTRICT,
	title       TEXT    NOT NULL,
	description TEXT    NOT NULL DEFAULT '',
	url         TEXT    NOT NULL DEFAULT '',
	icon_kind   TEXT    NOT NULL DEFAULT '',
	icon_value  TEXT    NOT NULL DEFAULT '',
	icon_color  TEXT    NOT NULL DEFAULT '#475569',
	sort_order  INTEGER NOT NULL DEFAULT 0,
	enabled     INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1))
);

CREATE TABLE guides (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	app_id     INTEGER REFERENCES apps(id) ON DELETE SET NULL,
	title      TEXT    NOT NULL,
	kind       TEXT    NOT NULL DEFAULT 'link',
	url        TEXT    NOT NULL DEFAULT '',
	body       TEXT    NOT NULL DEFAULT '',
	sort_order INTEGER NOT NULL DEFAULT 0,
	enabled    INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1))
);

CREATE TABLE alerts (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	title      TEXT    NOT NULL,
	body       TEXT    NOT NULL DEFAULT '',
	level      TEXT    NOT NULL CHECK (level IN ('urgent', 'maintenance', 'news')),
	starts_at  TEXT    NOT NULL,
	ends_at    TEXT,
	notify     INTEGER NOT NULL DEFAULT 0 CHECK (notify IN (0, 1)),
	created_at TEXT    NOT NULL,
	created_by TEXT    NOT NULL DEFAULT ''
);

CREATE INDEX idx_apps_category ON apps(category_id);
CREATE INDEX idx_guides_app    ON guides(app_id);
CREATE INDEX idx_alerts_window ON alerts(starts_at, ends_at);
`)
	return err
}

func migrateV2Seed(tx *sql.Tx) error {
	res, err := tx.Exec(`INSERT INTO categories (name, sort_order) VALUES ('Applicativi', 0)`)
	if err != nil {
		return err
	}
	catID, err := res.LastInsertId()
	if err != nil {
		return err
	}
	_, err = tx.Exec(`
INSERT INTO apps (category_id, title, description, icon_kind, icon_value, icon_color, sort_order) VALUES
	(?, 'Rubrica', 'Contatti e interni', 'pack', 'contacts', '#2563eb', 0),
	(?, 'Webmail', 'Posta elettronica',  'pack', 'mail',     '#0891b2', 1)`,
		catID, catID)
	return err
}
```

- [ ] **Step 5: Aggiorna `main.go`**

In `cmd/server/main.go` sostituisci `database.InitDB(cfg.DBPath)` con `database.Open(cfg.DBPath)`.

- [ ] **Step 6: Verifica e commit**

Run: `go vet ./... && go test ./internal/database/ -v`
Expected: PASS (4 test)

```bash
git add -A internal/database cmd/server/main.go
git commit -m "feat(database): schema v1 con migrazioni versionate e seed minimo

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Database — categorie e applicativi

**Files:**
- Create: `internal/database/categories.go`, `internal/database/apps.go`, `internal/database/order.go`
- Test: `internal/database/categories_test.go`, `internal/database/apps_test.go`

**Interfaces:**
- Consumes: `DB`, `ErrNotFound`, `ErrDuplicate`, `ErrCategoryNotEmpty`, `isUniqueViolation`, `checkAffected` (Task 2)
- Produces:
  ```go
  type Category struct { ID int64; Name string; SortOrder int }
  func (db *DB) ListCategories() ([]Category, error)
  func (db *DB) GetCategory(id int64) (Category, error)
  func (db *DB) CreateCategory(name string) (int64, error)
  func (db *DB) UpdateCategory(id int64, name string) error
  func (db *DB) DeleteCategory(id int64) error
  func (db *DB) MoveCategory(id int64, dir int) error // dir -1 su, +1 giù

  const (IconMonogram = ""; IconPack = "pack"; IconUpload = "upload"; IconURL = "url")
  type App struct {
      ID, CategoryID                          int64
      Title, Description, URL                 string
      IconKind, IconValue, IconColor          string
      SortOrder                               int
      Enabled                                 bool
  }
  func (db *DB) ListApps() ([]App, error) // ordine: categoria, sort_order, titolo
  func (db *DB) GetApp(id int64) (App, error)
  func (db *DB) CreateApp(a App) (int64, error)
  func (db *DB) UpdateApp(a App) error
  func (db *DB) DeleteApp(id int64) error
  func (db *DB) MoveApp(id int64, dir int) error
  ```

- [ ] **Step 1: Test categorie** — `internal/database/categories_test.go`

```go
package database

import (
	"errors"
	"testing"
)

func names(cs []Category) string {
	s := ""
	for _, c := range cs {
		s += c.Name + ","
	}
	return s
}

func TestCategoryCRUD(t *testing.T) {
	db := newTestDB(t)

	id, err := db.CreateCategory("Gestionali esterni")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateCategory("gestionali ESTERNI"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("nome duplicato (case-insensitive): atteso ErrDuplicate, ottenuto %v", err)
	}
	if err := db.UpdateCategory(id, "Esterni"); err != nil {
		t.Fatal(err)
	}
	c, err := db.GetCategory(id)
	if err != nil || c.Name != "Esterni" || c.SortOrder != 1 {
		t.Fatalf("GetCategory: %+v %v", c, err)
	}
	if err := db.UpdateCategory(9999, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update inesistente: %v", err)
	}
	if err := db.DeleteCategory(id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetCategory(id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("dopo delete: %v", err)
	}
}

func TestDeleteCategoryWithAppsRefused(t *testing.T) {
	db := newTestDB(t)
	cats, _ := db.ListCategories() // seed: "Applicativi" con 2 app
	if err := db.DeleteCategory(cats[0].ID); !errors.Is(err, ErrCategoryNotEmpty) {
		t.Fatalf("atteso ErrCategoryNotEmpty, ottenuto %v", err)
	}
}

func TestMoveCategory(t *testing.T) {
	db := newTestDB(t)
	b, _ := db.CreateCategory("B")
	db.CreateCategory("C")

	if err := db.MoveCategory(b, -1); err != nil {
		t.Fatal(err)
	}
	cs, _ := db.ListCategories()
	if got := names(cs); got != "B,Applicativi,C," {
		t.Fatalf("dopo su: %s", got)
	}
	// Già in cima: nessun effetto, nessun errore.
	if err := db.MoveCategory(b, -1); err != nil {
		t.Fatal(err)
	}
	if err := db.MoveCategory(9999, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("move inesistente: %v", err)
	}
}
```

- [ ] **Step 2: Test app** — `internal/database/apps_test.go`

```go
package database

import (
	"errors"
	"testing"
)

func TestAppCRUD(t *testing.T) {
	db := newTestDB(t)
	cats, _ := db.ListCategories()
	cat := cats[0].ID

	id, err := db.CreateApp(App{
		CategoryID: cat, Title: "Sicraweb", URL: "https://sicraweb.local",
		IconKind: IconPack, IconValue: "description", IconColor: "#475569", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	a, err := db.GetApp(id)
	if err != nil || a.Title != "Sicraweb" || !a.Enabled || a.SortOrder != 2 {
		t.Fatalf("GetApp: %+v %v (sort_order atteso 2, dopo le 2 app del seed)", a, err)
	}

	a.Title, a.Enabled = "Sicraweb EVO", false
	if err := db.UpdateApp(a); err != nil {
		t.Fatal(err)
	}
	a, _ = db.GetApp(id)
	if a.Title != "Sicraweb EVO" || a.Enabled {
		t.Fatalf("UpdateApp: %+v", a)
	}

	if err := db.DeleteApp(id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetApp(id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("dopo delete: %v", err)
	}
}

func TestUpdateAppCategoryMovesToEnd(t *testing.T) {
	db := newTestDB(t)
	other, _ := db.CreateCategory("Gestionali esterni")
	tinn, _ := db.CreateApp(App{CategoryID: other, Title: "TINN", Enabled: true})

	apps, _ := db.ListApps()
	rubrica := apps[0] // seed, categoria "Applicativi"
	rubrica.CategoryID = other
	if err := db.UpdateApp(rubrica); err != nil {
		t.Fatal(err)
	}
	r, _ := db.GetApp(rubrica.ID)
	t2, _ := db.GetApp(tinn)
	if r.SortOrder <= t2.SortOrder {
		t.Fatalf("cambio categoria: attesa in coda (sort %d > %d)", r.SortOrder, t2.SortOrder)
	}
}

func TestMoveAppStaysInCategory(t *testing.T) {
	db := newTestDB(t)
	other, _ := db.CreateCategory("Altro")
	db.CreateApp(App{CategoryID: other, Title: "Zeta", Enabled: true})

	apps, _ := db.ListApps()
	webmail := apps[1]
	if err := db.MoveApp(webmail.ID, -1); err != nil {
		t.Fatal(err)
	}
	apps, _ = db.ListApps()
	if apps[0].Title != "Webmail" || apps[1].Title != "Rubrica" || apps[2].Title != "Zeta" {
		t.Fatalf("ordine dopo move: %s %s %s", apps[0].Title, apps[1].Title, apps[2].Title)
	}
	// In fondo alla propria categoria: non scavalca in un'altra categoria.
	if err := db.MoveApp(apps[1].ID, 1); err != nil {
		t.Fatal(err)
	}
	apps, _ = db.ListApps()
	if apps[2].Title != "Zeta" {
		t.Fatalf("move ha attraversato la categoria: %s", apps[2].Title)
	}
}
```

- [ ] **Step 3: Verifica che falliscano**

Run: `go test ./internal/database/`
Expected: FAIL — `undefined: Category`, `undefined: App`…

- [ ] **Step 4: Implementa** — `internal/database/order.go`

```go
package database

import (
	"fmt"
	"slices"
)

// moveRow sposta la riga id di una posizione (dir -1 su, +1 giù) all'interno
// dell'insieme selezionato da where, poi rinumera sort_order 0..n-1.
// table/where/orderBy sono costanti del package, mai input utente.
func (db *DB) moveRow(table, where string, args []any, orderBy string, id int64, dir int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.Query(fmt.Sprintf(`SELECT id FROM %s WHERE %s ORDER BY %s`, table, where, orderBy), args...)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var rid int64
		if err := rows.Scan(&rid); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, rid)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	i := slices.Index(ids, id)
	if i < 0 {
		return ErrNotFound
	}
	j := i + dir
	if j < 0 || j >= len(ids) {
		return nil // già al bordo
	}
	ids[i], ids[j] = ids[j], ids[i]
	for pos, rid := range ids {
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s SET sort_order = ? WHERE id = ?`, table), pos, rid); err != nil {
			return err
		}
	}
	return tx.Commit()
}
```

`internal/database/categories.go`:

```go
package database

import (
	"database/sql"
	"errors"
)

// Category raggruppa le app in plancia (es. "Applicativi", "Gestionali esterni").
type Category struct {
	ID        int64
	Name      string
	SortOrder int
}

const categoryOrder = `sort_order, name COLLATE NOCASE, id`

func (db *DB) ListCategories() ([]Category, error) {
	rows, err := db.Query(`SELECT id, name, sort_order FROM categories ORDER BY ` + categoryOrder)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Category{}
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.ID, &c.Name, &c.SortOrder); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (db *DB) GetCategory(id int64) (Category, error) {
	var c Category
	err := db.QueryRow(`SELECT id, name, sort_order FROM categories WHERE id = ?`, id).
		Scan(&c.ID, &c.Name, &c.SortOrder)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

func (db *DB) CreateCategory(name string) (int64, error) {
	res, err := db.Exec(`
INSERT INTO categories (name, sort_order)
VALUES (?, (SELECT COALESCE(MAX(sort_order), -1) + 1 FROM categories))`, name)
	if isUniqueViolation(err) {
		return 0, ErrDuplicate
	}
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (db *DB) UpdateCategory(id int64, name string) error {
	res, err := db.Exec(`UPDATE categories SET name = ? WHERE id = ?`, name, id)
	if isUniqueViolation(err) {
		return ErrDuplicate
	}
	return checkAffected(res, err)
}

func (db *DB) DeleteCategory(id int64) error {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM apps WHERE category_id = ?`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrCategoryNotEmpty
	}
	return checkAffected(db.Exec(`DELETE FROM categories WHERE id = ?`, id))
}

func (db *DB) MoveCategory(id int64, dir int) error {
	return db.moveRow("categories", "1 = 1", nil, categoryOrder, id, dir)
}
```

`internal/database/apps.go`:

```go
package database

import (
	"database/sql"
	"errors"
)

// Valori di apps.icon_kind (validati in Go, non con CHECK: i sotto-progetti
// successivi possono aggiungerne senza ricreare la tabella).
const (
	IconMonogram = ""
	IconPack     = "pack"
	IconUpload   = "upload"
	IconURL      = "url"
)

// App è una card della plancia che apre un applicativo.
type App struct {
	ID          int64
	CategoryID  int64
	Title       string
	Description string
	URL         string
	IconKind    string
	IconValue   string
	IconColor   string
	SortOrder   int
	Enabled     bool
}

const appCols = `a.id, a.category_id, a.title, a.description, a.url,
	a.icon_kind, a.icon_value, a.icon_color, a.sort_order, a.enabled`

const appOrder = `a.sort_order, a.title COLLATE NOCASE, a.id`

type scanner interface{ Scan(dest ...any) error }

func scanApp(s scanner, extra ...any) (App, error) {
	var a App
	dest := append(extra, &a.ID, &a.CategoryID, &a.Title, &a.Description, &a.URL,
		&a.IconKind, &a.IconValue, &a.IconColor, &a.SortOrder, &a.Enabled)
	err := s.Scan(dest...)
	return a, err
}

func (db *DB) ListApps() ([]App, error) {
	rows, err := db.Query(`SELECT ` + appCols + ` FROM apps a
JOIN categories c ON c.id = a.category_id
ORDER BY c.sort_order, c.name COLLATE NOCASE, ` + appOrder)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []App{}
	for rows.Next() {
		a, err := scanApp(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (db *DB) GetApp(id int64) (App, error) {
	a, err := scanApp(db.QueryRow(`SELECT `+appCols+` FROM apps a WHERE a.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

func (db *DB) CreateApp(a App) (int64, error) {
	res, err := db.Exec(`
INSERT INTO apps (category_id, title, description, url, icon_kind, icon_value, icon_color, enabled, sort_order)
VALUES (?, ?, ?, ?, ?, ?, ?, ?,
	(SELECT COALESCE(MAX(sort_order), -1) + 1 FROM apps WHERE category_id = ?))`,
		a.CategoryID, a.Title, a.Description, a.URL, a.IconKind, a.IconValue, a.IconColor, a.Enabled, a.CategoryID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateApp aggiorna tutti i campi tranne sort_order; se cambia categoria
// l'app finisce in coda alla nuova (in UPDATE le espressioni vedono i valori vecchi).
func (db *DB) UpdateApp(a App) error {
	return checkAffected(db.Exec(`
UPDATE apps SET
	sort_order  = CASE WHEN category_id = ? THEN sort_order
	                   ELSE (SELECT COALESCE(MAX(sort_order), -1) + 1 FROM apps WHERE category_id = ?) END,
	category_id = ?, title = ?, description = ?, url = ?,
	icon_kind = ?, icon_value = ?, icon_color = ?, enabled = ?
WHERE id = ?`,
		a.CategoryID, a.CategoryID,
		a.CategoryID, a.Title, a.Description, a.URL,
		a.IconKind, a.IconValue, a.IconColor, a.Enabled, a.ID))
}

func (db *DB) DeleteApp(id int64) error {
	return checkAffected(db.Exec(`DELETE FROM apps WHERE id = ?`, id))
}

func (db *DB) MoveApp(id int64, dir int) error {
	a, err := db.GetApp(id)
	if err != nil {
		return err
	}
	return db.moveRow("apps", "category_id = ?", []any{a.CategoryID},
		`sort_order, title COLLATE NOCASE, id`, id, dir)
}
```

- [ ] **Step 5: Verifica e commit**

Run: `go vet ./... && go test ./internal/database/ -v`
Expected: PASS

```bash
git add internal/database
git commit -m "feat(database): CRUD e ordinamento di categorie e applicativi

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: Database — guide, avvisi, dati della plancia

**Files:**
- Create: `internal/database/guides.go`, `internal/database/alerts.go`, `internal/database/dashboard.go`
- Test: `internal/database/guides_test.go`, `internal/database/alerts_test.go`, `internal/database/dashboard_test.go`

**Interfaces:**
- Consumes: `App`, `scanApp`, `scanner`, `appCols`, `appOrder`, `Category`, `moveRow`, `formatTime`, `parseTime`, `checkAffected` (Task 2–3)
- Produces:
  ```go
  const GuideKindLink = "link"
  type Guide struct { ID int64; AppID *int64; Title, Kind, URL, Body string; SortOrder int; Enabled bool }
  func (db *DB) ListGuides() ([]Guide, error)          // generali prima, poi per app
  func (db *DB) GetGuide(id int64) (Guide, error)
  func (db *DB) CreateGuide(g Guide) (int64, error)
  func (db *DB) UpdateGuide(g Guide) error
  func (db *DB) DeleteGuide(id int64) error
  func (db *DB) MoveGuide(id int64, dir int) error
  func (db *DB) GuideCountsByApp() (map[int64]int, error)

  const (LevelUrgent = "urgent"; LevelMaintenance = "maintenance"; LevelNews = "news")
  type Alert struct {
      ID int64; Title, Body, Level string
      StartsAt time.Time; EndsAt *time.Time
      Notify bool; CreatedAt time.Time; CreatedBy string
  }
  func (db *DB) ListActiveAlerts(now time.Time) ([]Alert, error)
  func (db *DB) ListAlertsForAdmin(now time.Time) (current, expired []Alert, err error)
  func (db *DB) GetAlert(id int64) (Alert, error)
  func (db *DB) CreateAlert(a Alert) (int64, error)
  func (db *DB) UpdateAlert(a Alert) error // non tocca created_at/created_by
  func (db *DB) DeleteAlert(id int64) error

  type AppWithGuides struct { App; Guides []Guide }
  type CategoryWithApps struct { Category; Apps []AppWithGuides }
  type Dashboard struct { Alerts []Alert; Categories []CategoryWithApps; GeneralGuides []Guide }
  func (db *DB) GetDashboard(now time.Time) (Dashboard, error)
  ```

- [ ] **Step 1: Test guide** — `internal/database/guides_test.go`

```go
package database

import (
	"errors"
	"testing"
)

func ptr(v int64) *int64 { return &v }

func TestGuideCRUDAndOrdering(t *testing.T) {
	db := newTestDB(t)
	apps, _ := db.ListApps()
	webmail := apps[1].ID

	gen, err := db.CreateGuide(Guide{Title: "VPN da casa", Kind: GuideKindLink, URL: "https://wiki/vpn", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	firma, _ := db.CreateGuide(Guide{AppID: ptr(webmail), Title: "Firma email", Kind: GuideKindLink, URL: "https://wiki/firma", Enabled: true})
	db.CreateGuide(Guide{AppID: ptr(webmail), Title: "Archiviazione", Kind: GuideKindLink, URL: "https://wiki/arch", Enabled: true})

	gs, _ := db.ListGuides()
	if len(gs) != 3 || gs[0].ID != gen || gs[0].AppID != nil {
		t.Fatalf("ListGuides: le generali vanno per prime, ottenuto %+v", gs)
	}

	g, _ := db.GetGuide(firma)
	g.Title = "Firma email aziendale"
	if err := db.UpdateGuide(g); err != nil {
		t.Fatal(err)
	}
	if err := db.MoveGuide(firma, 1); err != nil {
		t.Fatal(err)
	}
	gs, _ = db.ListGuides()
	if gs[2].Title != "Firma email aziendale" {
		t.Fatalf("MoveGuide entro l'app: ottenuto ordine %s, %s", gs[1].Title, gs[2].Title)
	}

	counts, _ := db.GuideCountsByApp()
	if counts[webmail] != 2 || len(counts) != 1 {
		t.Fatalf("GuideCountsByApp: %v", counts)
	}

	if err := db.DeleteGuide(gen); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetGuide(gen); !errors.Is(err, ErrNotFound) {
		t.Fatalf("dopo delete: %v", err)
	}
}

func TestDeleteAppMakesGuidesGeneral(t *testing.T) {
	db := newTestDB(t)
	apps, _ := db.ListApps()
	id, _ := db.CreateGuide(Guide{AppID: ptr(apps[0].ID), Title: "Cercare un interno", Kind: GuideKindLink, URL: "https://wiki/x", Enabled: true})

	if err := db.DeleteApp(apps[0].ID); err != nil {
		t.Fatal(err)
	}
	g, _ := db.GetGuide(id)
	if g.AppID != nil {
		t.Fatalf("ON DELETE SET NULL: attesa guida generale, app_id=%v", *g.AppID)
	}
}
```

- [ ] **Step 2: Test avvisi** — `internal/database/alerts_test.go`

```go
package database

import (
	"testing"
	"time"
)

var now = time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)

func mkAlert(t *testing.T, db *DB, title, level string, start time.Time, end *time.Time) int64 {
	t.Helper()
	id, err := db.CreateAlert(Alert{Title: title, Level: level, StartsAt: start, EndsAt: end, CreatedAt: now, CreatedBy: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func tp(t time.Time) *time.Time { return &t }

func TestListActiveAlerts(t *testing.T) {
	db := newTestDB(t)
	mkAlert(t, db, "novità", LevelNews, now.Add(-time.Hour), nil)
	mkAlert(t, db, "urgente", LevelUrgent, now.Add(-2*time.Hour), tp(now.Add(time.Hour)))
	mkAlert(t, db, "inizia ora", LevelMaintenance, now, nil)                 // starts_at == now → attivo
	mkAlert(t, db, "finisce ora", LevelUrgent, now.Add(-time.Hour), tp(now)) // ends_at == now → scaduto
	mkAlert(t, db, "futuro", LevelUrgent, now.Add(time.Hour), nil)

	got, err := db.ListActiveAlerts(now)
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, a := range got {
		titles = append(titles, a.Title)
	}
	want := []string{"urgente", "inizia ora", "novità"} // ordine per livello
	if len(titles) != len(want) {
		t.Fatalf("attivi: atteso %v, ottenuto %v", want, titles)
	}
	for i := range want {
		if titles[i] != want[i] {
			t.Fatalf("attivi: atteso %v, ottenuto %v", want, titles)
		}
	}
	if got[0].EndsAt == nil || !got[0].EndsAt.Equal(now.Add(time.Hour)) || got[2].EndsAt != nil {
		t.Fatalf("EndsAt non letto correttamente: %+v", got)
	}
}

func TestAlertsForAdminAndUpdate(t *testing.T) {
	db := newTestDB(t)
	id := mkAlert(t, db, "vecchio", LevelNews, now.Add(-48*time.Hour), tp(now.Add(-24*time.Hour)))
	mkAlert(t, db, "programmato", LevelNews, now.Add(time.Hour), nil)

	current, expired, err := db.ListAlertsForAdmin(now)
	if err != nil || len(current) != 1 || len(expired) != 1 || expired[0].ID != id {
		t.Fatalf("ListAlertsForAdmin: %v %v %v", current, expired, err)
	}

	a, _ := db.GetAlert(id)
	a.EndsAt = nil // riattivato senza scadenza
	a.CreatedBy = "altro"
	if err := db.UpdateAlert(a); err != nil {
		t.Fatal(err)
	}
	a, _ = db.GetAlert(id)
	if a.EndsAt != nil || a.CreatedBy != "admin" {
		t.Fatalf("UpdateAlert: %+v (created_by non deve cambiare)", a)
	}
	if err := db.DeleteAlert(id); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 3: Test plancia** — `internal/database/dashboard_test.go`

```go
package database

import (
	"testing"
	"time"
)

func TestGetDashboard(t *testing.T) {
	db := newTestDB(t)
	apps, _ := db.ListApps()
	rubrica, webmail := apps[0], apps[1]

	// Solo Rubrica ha un URL; Webmail resta "da completare".
	rubrica.URL = "https://rubrica.local"
	db.UpdateApp(rubrica)

	ext, _ := db.CreateCategory("Gestionali esterni")
	off, _ := db.CreateApp(App{CategoryID: ext, Title: "Spenta", URL: "https://x", Enabled: false})
	db.CreateCategory("Vuota")

	db.CreateGuide(Guide{AppID: ptr(rubrica.ID), Title: "Cercare un interno", Kind: GuideKindLink, URL: "https://g/1", Enabled: true})
	db.CreateGuide(Guide{AppID: ptr(rubrica.ID), Title: "Disabilitata", Kind: GuideKindLink, URL: "https://g/2", Enabled: false})
	db.CreateGuide(Guide{AppID: ptr(webmail.ID), Title: "Di app senza URL", Kind: GuideKindLink, URL: "https://g/3", Enabled: true})
	db.CreateGuide(Guide{AppID: ptr(off), Title: "Di app spenta", Kind: GuideKindLink, URL: "https://g/4", Enabled: true})
	db.CreateGuide(Guide{Title: "VPN", Kind: GuideKindLink, URL: "https://g/5", Enabled: true})
	mkAlert(t, db, "attivo", LevelNews, now.Add(-time.Hour), nil)

	d, err := db.GetDashboard(now)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Alerts) != 1 {
		t.Fatalf("avvisi: %+v", d.Alerts)
	}
	if len(d.Categories) != 1 || d.Categories[0].Name != "Applicativi" {
		t.Fatalf("categorie visibili: solo quelle con app visibili, ottenuto %+v", d.Categories)
	}
	cat := d.Categories[0]
	if len(cat.Apps) != 1 || cat.Apps[0].Title != "Rubrica" {
		t.Fatalf("app visibili: solo abilitate con URL, ottenuto %+v", cat.Apps)
	}
	if g := cat.Apps[0].Guides; len(g) != 1 || g[0].Title != "Cercare un interno" {
		t.Fatalf("guide di Rubrica: %+v", g)
	}
	if len(d.GeneralGuides) != 1 || d.GeneralGuides[0].Title != "VPN" {
		t.Fatalf("guide generali: le guide di app nascoste non devono comparire, ottenuto %+v", d.GeneralGuides)
	}
}

func TestGetDashboardFreshInstall(t *testing.T) {
	db := newTestDB(t)
	d, err := db.GetDashboard(now)
	if err != nil {
		t.Fatal(err)
	}
	if d.Categories == nil || d.Alerts == nil || d.GeneralGuides == nil || len(d.Categories) != 0 {
		t.Fatalf("installazione nuova: attese slice vuote non nil, ottenuto %+v", d)
	}
}
```

- [ ] **Step 4: Verifica che falliscano**

Run: `go test ./internal/database/`
Expected: FAIL — `undefined: Guide`, `undefined: Alert`…

- [ ] **Step 5: Implementa** — `internal/database/guides.go`

```go
package database

import (
	"database/sql"
	"errors"
)

// GuideKindLink è l'unico tipo gestito nel sotto-progetto 1;
// 'markdown' e 'github' arrivano col sotto-progetto 2.
const GuideKindLink = "link"

// Guide è una guida o FAQ. AppID nil = guida generale.
type Guide struct {
	ID        int64
	AppID     *int64
	Title     string
	Kind      string
	URL       string
	Body      string
	SortOrder int
	Enabled   bool
}

const guideCols = `g.id, g.app_id, g.title, g.kind, g.url, g.body, g.sort_order, g.enabled`

const guideOrder = `g.sort_order, g.title COLLATE NOCASE, g.id`

func scanGuide(s scanner) (Guide, error) {
	var g Guide
	var appID sql.NullInt64
	err := s.Scan(&g.ID, &appID, &g.Title, &g.Kind, &g.URL, &g.Body, &g.SortOrder, &g.Enabled)
	if appID.Valid {
		g.AppID = &appID.Int64
	}
	return g, err
}

func queryGuides(db *DB, query string, args ...any) ([]Guide, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Guide{}
	for rows.Next() {
		g, err := scanGuide(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (db *DB) ListGuides() ([]Guide, error) {
	return queryGuides(db, `SELECT `+guideCols+` FROM guides g
ORDER BY g.app_id IS NOT NULL, g.app_id, `+guideOrder)
}

func (db *DB) GetGuide(id int64) (Guide, error) {
	g, err := scanGuide(db.QueryRow(`SELECT `+guideCols+` FROM guides g WHERE g.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return g, ErrNotFound
	}
	return g, err
}

// "app_id IS ?" confronta correttamente anche NULL (guide generali).
func (db *DB) CreateGuide(g Guide) (int64, error) {
	res, err := db.Exec(`
INSERT INTO guides (app_id, title, kind, url, body, enabled, sort_order)
VALUES (?, ?, ?, ?, ?, ?,
	(SELECT COALESCE(MAX(sort_order), -1) + 1 FROM guides WHERE app_id IS ?))`,
		g.AppID, g.Title, g.Kind, g.URL, g.Body, g.Enabled, g.AppID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateGuide: se cambia app, la guida finisce in coda alla nuova.
func (db *DB) UpdateGuide(g Guide) error {
	return checkAffected(db.Exec(`
UPDATE guides SET
	sort_order = CASE WHEN app_id IS ? THEN sort_order
	                  ELSE (SELECT COALESCE(MAX(sort_order), -1) + 1 FROM guides WHERE app_id IS ?) END,
	app_id = ?, title = ?, kind = ?, url = ?, body = ?, enabled = ?
WHERE id = ?`,
		g.AppID, g.AppID,
		g.AppID, g.Title, g.Kind, g.URL, g.Body, g.Enabled, g.ID))
}

func (db *DB) DeleteGuide(id int64) error {
	return checkAffected(db.Exec(`DELETE FROM guides WHERE id = ?`, id))
}

func (db *DB) MoveGuide(id int64, dir int) error {
	g, err := db.GetGuide(id)
	if err != nil {
		return err
	}
	return db.moveRow("guides", "app_id IS ?", []any{g.AppID},
		`sort_order, title COLLATE NOCASE, id`, id, dir)
}

// GuideCountsByApp restituisce quante guide (anche disabilitate) ha ogni app.
func (db *DB) GuideCountsByApp() (map[int64]int, error) {
	rows, err := db.Query(`SELECT app_id, COUNT(*) FROM guides WHERE app_id IS NOT NULL GROUP BY app_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}
```

`internal/database/alerts.go`:

```go
package database

import (
	"database/sql"
	"errors"
	"time"
)

const (
	LevelUrgent      = "urgent"
	LevelMaintenance = "maintenance"
	LevelNews        = "news"
)

// Alert è un avviso della striscia in plancia. EndsAt nil = senza scadenza.
type Alert struct {
	ID        int64
	Title     string
	Body      string
	Level     string
	StartsAt  time.Time
	EndsAt    *time.Time
	Notify    bool
	CreatedAt time.Time
	CreatedBy string
}

const alertCols = `id, title, body, level, starts_at, ends_at, notify, created_at, created_by`

const levelOrder = `CASE level WHEN 'urgent' THEN 0 WHEN 'maintenance' THEN 1 ELSE 2 END`

func scanAlert(s scanner) (Alert, error) {
	var a Alert
	var starts, created string
	var ends sql.NullString
	if err := s.Scan(&a.ID, &a.Title, &a.Body, &a.Level, &starts, &ends, &a.Notify, &created, &a.CreatedBy); err != nil {
		return a, err
	}
	var err error
	if a.StartsAt, err = parseTime(starts); err != nil {
		return a, err
	}
	if a.CreatedAt, err = parseTime(created); err != nil {
		return a, err
	}
	if ends.Valid {
		e, err := parseTime(ends.String)
		if err != nil {
			return a, err
		}
		a.EndsAt = &e
	}
	return a, nil
}

func queryAlerts(db *DB, query string, args ...any) ([]Alert, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Alert{}
	for rows.Next() {
		a, err := scanAlert(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return formatTime(*t)
}

// ListActiveAlerts: starts_at <= now < ends_at (o senza fine), ordinati per gravità.
func (db *DB) ListActiveAlerts(now time.Time) ([]Alert, error) {
	n := formatTime(now)
	return queryAlerts(db, `SELECT `+alertCols+` FROM alerts
WHERE starts_at <= ? AND (ends_at IS NULL OR ends_at > ?)
ORDER BY `+levelOrder+`, starts_at DESC, id DESC`, n, n)
}

// ListAlertsForAdmin separa avvisi correnti/programmati da quelli scaduti (ultimi 30).
func (db *DB) ListAlertsForAdmin(now time.Time) (current, expired []Alert, err error) {
	n := formatTime(now)
	current, err = queryAlerts(db, `SELECT `+alertCols+` FROM alerts
WHERE ends_at IS NULL OR ends_at > ? ORDER BY starts_at DESC, id DESC`, n)
	if err != nil {
		return nil, nil, err
	}
	expired, err = queryAlerts(db, `SELECT `+alertCols+` FROM alerts
WHERE ends_at <= ? ORDER BY ends_at DESC, id DESC LIMIT 30`, n)
	return current, expired, err
}

func (db *DB) GetAlert(id int64) (Alert, error) {
	a, err := scanAlert(db.QueryRow(`SELECT `+alertCols+` FROM alerts WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

func (db *DB) CreateAlert(a Alert) (int64, error) {
	res, err := db.Exec(`
INSERT INTO alerts (title, body, level, starts_at, ends_at, notify, created_at, created_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.Title, a.Body, a.Level, formatTime(a.StartsAt), nullableTime(a.EndsAt), a.Notify, formatTime(a.CreatedAt), a.CreatedBy)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (db *DB) UpdateAlert(a Alert) error {
	return checkAffected(db.Exec(`
UPDATE alerts SET title = ?, body = ?, level = ?, starts_at = ?, ends_at = ?, notify = ?
WHERE id = ?`,
		a.Title, a.Body, a.Level, formatTime(a.StartsAt), nullableTime(a.EndsAt), a.Notify, a.ID))
}

func (db *DB) DeleteAlert(id int64) error {
	return checkAffected(db.Exec(`DELETE FROM alerts WHERE id = ?`, id))
}
```

`internal/database/dashboard.go`:

```go
package database

import "time"

type AppWithGuides struct {
	App
	Guides []Guide
}

type CategoryWithApps struct {
	Category
	Apps []AppWithGuides
}

// Dashboard contiene tutto ciò che serve alla plancia. Slice mai nil.
type Dashboard struct {
	Alerts        []Alert
	Categories    []CategoryWithApps
	GeneralGuides []Guide
}

// Un'app è visibile se abilitata e con URL; una guida se abilitata e generale
// oppure agganciata a un'app visibile.
const visibleApp = `a.enabled = 1 AND a.url <> ''`

func (db *DB) GetDashboard(now time.Time) (Dashboard, error) {
	d := Dashboard{Categories: []CategoryWithApps{}, GeneralGuides: []Guide{}}

	var err error
	if d.Alerts, err = db.ListActiveAlerts(now); err != nil {
		return d, err
	}

	rows, err := db.Query(`SELECT c.id, c.name, c.sort_order, ` + appCols + `
FROM apps a JOIN categories c ON c.id = a.category_id
WHERE ` + visibleApp + `
ORDER BY c.sort_order, c.name COLLATE NOCASE, ` + appOrder)
	if err != nil {
		return d, err
	}
	type pos struct{ cat, app int }
	index := map[int64]pos{}
	for rows.Next() {
		var c Category
		a, err := scanApp(rows, &c.ID, &c.Name, &c.SortOrder)
		if err != nil {
			rows.Close()
			return d, err
		}
		last := len(d.Categories) - 1
		if last < 0 || d.Categories[last].ID != c.ID {
			d.Categories = append(d.Categories, CategoryWithApps{Category: c})
			last++
		}
		d.Categories[last].Apps = append(d.Categories[last].Apps, AppWithGuides{App: a, Guides: []Guide{}})
		index[a.ID] = pos{last, len(d.Categories[last].Apps) - 1}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return d, err
	}

	guides, err := queryGuides(db, `SELECT `+guideCols+` FROM guides g
LEFT JOIN apps a ON a.id = g.app_id
WHERE g.enabled = 1 AND (g.app_id IS NULL OR (`+visibleApp+`))
ORDER BY `+guideOrder)
	if err != nil {
		return d, err
	}
	for _, g := range guides {
		if g.AppID == nil {
			d.GeneralGuides = append(d.GeneralGuides, g)
			continue
		}
		p := index[*g.AppID]
		d.Categories[p.cat].Apps[p.app].Guides = append(d.Categories[p.cat].Apps[p.app].Guides, g)
	}
	return d, nil
}
```

- [ ] **Step 6: Verifica e commit**

Run: `go vet ./... && go test ./internal/database/ -v`
Expected: PASS

```bash
git add internal/database
git commit -m "feat(database): guide, avvisi e query della plancia

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: Catalogo Material Icons

**Files:**
- Create: `internal/icons/icons.go`, `internal/icons/codepoints.txt` (scaricato)
- Create: `web/static/fonts/MaterialIcons-Regular.woff2`, `web/static/fonts/LICENSE-MaterialIcons.txt` (scaricati), `web/static/fonts/material-icons.css`
- Test: `internal/icons/icons_test.go`

**Interfaces:**
- Produces:
  ```go
  func Valid(name string) bool
  func Search(q string, limit int) []string // prima i prefissi, poi i "contiene"
  func Count() int
  ```

- [ ] **Step 1: Scarica font, licenza e catalogo**

```bash
mkdir -p web/static/fonts
curl -sSfL -o internal/icons/codepoints.txt https://raw.githubusercontent.com/google/material-design-icons/master/font/MaterialIcons-Regular.codepoints
curl -sSfL -o web/static/fonts/MaterialIcons-Regular.woff2 https://fonts.gstatic.com/s/materialicons/v145/flUhRq6tzZclQEJ-Vdg-IuiaDsNc.woff2
curl -sSfL -o web/static/fonts/LICENSE-MaterialIcons.txt https://raw.githubusercontent.com/google/material-design-icons/master/LICENSE
wc -l internal/icons/codepoints.txt   # atteso ~2235
```

(Stesso file woff2 v145 usato da UtenzePA. Aggiungi `*.woff2 binary` a `.gitattributes`.)

- [ ] **Step 2: Foglio di stile del font** — `web/static/fonts/material-icons.css`

```css
/* Material Icons (Apache 2.0, vedi LICENSE-MaterialIcons.txt) — self-hosted, nessuna CDN. */
@font-face {
	font-family: "Material Icons";
	font-style: normal;
	font-weight: 400;
	font-display: block;
	src: url("MaterialIcons-Regular.woff2") format("woff2");
}

.material-icons {
	font-family: "Material Icons";
	font-weight: normal;
	font-style: normal;
	font-size: 20px;
	line-height: 1;
	letter-spacing: normal;
	text-transform: none;
	display: inline-block;
	white-space: nowrap;
	word-wrap: normal;
	direction: ltr;
	font-feature-settings: "liga";
	-webkit-font-smoothing: antialiased;
}
```

- [ ] **Step 3: Test che fallisce** — `internal/icons/icons_test.go`

```go
package icons

import (
	"slices"
	"testing"
)

func TestCatalog(t *testing.T) {
	if Count() < 2000 {
		t.Fatalf("catalogo troppo piccolo: %d", Count())
	}
	for _, n := range []string{"mail", "contacts", "account_balance", "gavel", "payments"} {
		if !Valid(n) {
			t.Errorf("%q dovrebbe essere valida", n)
		}
	}
	for _, n := range []string{"", "MAIL", "mail ", "<script>", "non_esiste_xyz"} {
		if Valid(n) {
			t.Errorf("%q non dovrebbe essere valida", n)
		}
	}
}

func TestSearch(t *testing.T) {
	got := Search("mail", 10)
	if len(got) == 0 || got[0] != "mail" {
		t.Fatalf("Search(mail): il match esatto/prefisso va per primo, ottenuto %v", got)
	}
	if !slices.Contains(Search("account bal", 10), "account_balance") {
		t.Fatal("Search: gli spazi devono valere come underscore")
	}
	if !slices.Contains(Search("balance", 60), "account_balance") {
		t.Fatal("Search: deve trovare anche le sottostringhe")
	}
	if n := len(Search("", 7)); n != 7 {
		t.Fatalf("Search vuota: attesi 7 risultati, ottenuti %d", n)
	}
	if n := len(Search("a", 60)); n != 60 {
		t.Fatalf("limite non rispettato: %d", n)
	}
}
```

Run: `go test ./internal/icons/`
Expected: FAIL — `undefined: Count`

- [ ] **Step 4: Implementa** — `internal/icons/icons.go`

```go
// Package icons espone il catalogo dei nomi Material Icons (ligature del font
// in web/static/fonts), usato per validare le icone delle app e per il picker admin.
package icons

import (
	_ "embed"
	"sort"
	"strings"
)

//go:embed codepoints.txt
var codepoints string

var (
	names []string
	set   = map[string]struct{}{}
)

func init() {
	for _, line := range strings.Split(codepoints, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if _, dup := set[f[0]]; !dup {
			set[f[0]] = struct{}{}
			names = append(names, f[0])
		}
	}
	sort.Strings(names)
}

// Valid indica se name è un'icona del catalogo.
func Valid(name string) bool {
	_, ok := set[name]
	return ok
}

// Count restituisce il numero di icone nel catalogo.
func Count() int { return len(names) }

// Search restituisce al massimo limit nomi: prima quelli che iniziano con q,
// poi quelli che lo contengono. Gli spazi in q valgono come underscore.
func Search(q string, limit int) []string {
	q = strings.ReplaceAll(strings.ToLower(strings.TrimSpace(q)), " ", "_")
	var prefix, contains []string
	for _, n := range names {
		switch {
		case strings.HasPrefix(n, q):
			prefix = append(prefix, n)
		case strings.Contains(n, q):
			contains = append(contains, n)
		}
		if len(prefix) >= limit {
			break
		}
	}
	out := append(prefix, contains...)
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
```

- [ ] **Step 5: Verifica e commit**

Run: `go vet ./... && go test ./internal/icons/ -v`
Expected: PASS

```bash
git add internal/icons web/static/fonts .gitattributes
git commit -m "feat(icons): catalogo Material Icons self-hosted

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: Autenticazione LDAP e rate limiter

**Files:**
- Create: `internal/auth/ldap.go`, `internal/auth/ratelimit.go`
- Test: `internal/auth/ldap_test.go`, `internal/auth/ratelimit_test.go`
- Modify: `go.mod`/`go.sum` (`go get github.com/go-ldap/ldap/v3`)

**Interfaces:**
- Consumes: `config.LDAP` (Task 1)
- Produces:
  ```go
  type Authenticator interface {
      Authenticate(username, password string) (ok, admin bool, err error)
  }
  func NewLDAP(cfg config.LDAP) *LDAP // implementa Authenticator
  func IsAdminUser(list []string, username string) bool

  type RateLimiter struct{ /* ... */ }
  func NewRateLimiter(max int, window time.Duration) *RateLimiter
  func (r *RateLimiter) Allow(keys ...string) (wait time.Duration, ok bool)
  func (r *RateLimiter) Fail(keys ...string)
  func (r *RateLimiter) Success(keys ...string)
  ```

Differenze volute rispetto a GoPulley: (1) se StartTLS fallisce il login fallisce, **nessun ripiego in chiaro** (la password viaggerebbe in chiaro); (2) username ammessi solo `[A-Za-z0-9._@-]` (evita iniezioni nel DN del bind); (3) utente fuori dal gruppo richiesto → `ok=false` senza errore; (4) in mock è admin chi è in `ADMIN_USERS`, oppure chiunque se la lista è vuota.

- [ ] **Step 1: Dipendenza**

```bash
go get github.com/go-ldap/ldap/v3@latest
```

- [ ] **Step 2: Test che falliscono** — `internal/auth/ldap_test.go`

```go
package auth

import (
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/config"
)

func TestMockAuthentication(t *testing.T) {
	open := NewLDAP(config.LDAP{Host: "mock"})
	if ok, admin, err := open.Authenticate("chiunque", "x"); !ok || !admin || err != nil {
		t.Fatalf("mock senza ADMIN_USERS: tutti admin, ottenuto %v %v %v", ok, admin, err)
	}

	restricted := NewLDAP(config.LDAP{Host: "mock", AdminUsers: []string{"mrossi"}})
	if ok, admin, _ := restricted.Authenticate("MRossi", "x"); !ok || !admin {
		t.Fatal("ADMIN_USERS deve ignorare maiuscole/minuscole")
	}
	if ok, admin, _ := restricted.Authenticate("gbianchi", "x"); !ok || admin {
		t.Fatal("utente non in ADMIN_USERS: autenticato ma non admin")
	}
}

func TestRejectsInvalidInput(t *testing.T) {
	a := NewLDAP(config.LDAP{Host: "mock"})
	for _, u := range []string{"", "  ", "mario rossi", "admin)(uid=*", "cn=x,dc=y", "ü"} {
		if ok, _, _ := a.Authenticate(u, "pw"); ok {
			t.Errorf("username %q dovrebbe essere rifiutato", u)
		}
	}
	if ok, _, _ := a.Authenticate("mrossi", ""); ok {
		t.Error("password vuota dovrebbe essere rifiutata")
	}
}

func TestInGroup(t *testing.T) {
	memberOf := []string{
		"CN=CED Admin,OU=Gruppi,DC=comune,DC=local",
		"CN=Tutti,OU=Gruppi,DC=comune,DC=local",
	}
	if !inGroup(memberOf, "ced admin") {
		t.Error("CN case-insensitive non riconosciuto")
	}
	if !inGroup(memberOf, "CN=Tutti,OU=Gruppi,DC=comune,DC=local") {
		t.Error("DN completo non riconosciuto")
	}
	if inGroup(memberOf, "Gruppi") || inGroup(memberOf, "CED") {
		t.Error("match parziale su OU o prefisso del CN non ammesso")
	}
}

func TestLDAPHostname(t *testing.T) {
	for in, want := range map[string]string{
		"ldaps://dc01.comune.local:636": "dc01.comune.local",
		"ldap://10.0.0.5":               "10.0.0.5",
	} {
		if got := ldapHostname(in); got != want {
			t.Errorf("ldapHostname(%q) = %q, atteso %q", in, got, want)
		}
	}
}
```

`internal/auth/ratelimit_test.go`:

```go
package auth

import (
	"testing"
	"time"
)

func newTestLimiter() (*RateLimiter, *time.Time) {
	clock := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
	r := NewRateLimiter(5, 15*time.Minute)
	r.now = func() time.Time { return clock }
	return r, &clock
}

func TestRateLimiterBlocksWithBackoff(t *testing.T) {
	r, clock := newTestLimiter()
	for i := 0; i < 4; i++ {
		r.Fail("u:mrossi")
	}
	if _, ok := r.Allow("u:mrossi"); !ok {
		t.Fatal("4 fallimenti: ancora consentito")
	}
	r.Fail("u:mrossi")
	if wait, ok := r.Allow("u:mrossi"); ok || wait != 30*time.Second {
		t.Fatalf("5° fallimento: atteso blocco 30s, ottenuto %v %v", wait, ok)
	}

	*clock = clock.Add(31 * time.Second)
	if _, ok := r.Allow("u:mrossi"); !ok {
		t.Fatal("dopo 30s: deve essere di nuovo consentito")
	}
	for i := 0; i < 5; i++ {
		r.Fail("u:mrossi")
	}
	if wait, _ := r.Allow("u:mrossi"); wait != 60*time.Second {
		t.Fatalf("secondo blocco: attesi 60s, ottenuti %v", wait)
	}
}

func TestRateLimiterWindowAndSuccess(t *testing.T) {
	r, clock := newTestLimiter()
	for i := 0; i < 4; i++ {
		r.Fail("ip:10.0.0.1")
	}
	*clock = clock.Add(16 * time.Minute) // fuori finestra
	r.Fail("ip:10.0.0.1")
	if _, ok := r.Allow("ip:10.0.0.1"); !ok {
		t.Fatal("i fallimenti fuori dalla finestra non devono contare")
	}

	for i := 0; i < 4; i++ {
		r.Fail("u:x")
	}
	r.Success("u:x")
	r.Fail("u:x")
	if _, ok := r.Allow("u:x"); !ok {
		t.Fatal("Success deve azzerare il contatore")
	}
}

func TestRateLimiterMultipleKeys(t *testing.T) {
	r, _ := newTestLimiter()
	for i := 0; i < 5; i++ {
		r.Fail("ip:10.0.0.9")
	}
	if _, ok := r.Allow("u:nuovo", "ip:10.0.0.9"); ok {
		t.Fatal("basta una chiave bloccata per bloccare la richiesta")
	}
}

func TestRateLimiterMaxBlock(t *testing.T) {
	r, clock := newTestLimiter()
	var wait time.Duration
	for round := 0; round < 12; round++ {
		for i := 0; i < 5; i++ {
			r.Fail("u:x")
		}
		wait, _ = r.Allow("u:x")
		*clock = clock.Add(wait + time.Second)
	}
	if wait != 15*time.Minute {
		t.Fatalf("blocco massimo: attesi 15m, ottenuti %v", wait)
	}
}
```

Run: `go test ./internal/auth/`
Expected: FAIL — `undefined: NewLDAP`, `undefined: NewRateLimiter`

- [ ] **Step 3: Implementa** — `internal/auth/ldap.go`

```go
// Package auth verifica le credenziali admin su Active Directory (porting
// di internal/auth di GoPulley) e limita i tentativi di login.
package auth

import (
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/config"
)

// Authenticator verifica username/password e dice se l'utente è admin.
type Authenticator interface {
	Authenticate(username, password string) (ok, admin bool, err error)
}

// LDAP implementa Authenticator su Active Directory / OpenLDAP.
type LDAP struct {
	cfg config.LDAP
}

func NewLDAP(cfg config.LDAP) *LDAP { return &LDAP{cfg: cfg} }

// usernameRe limita i caratteri ammessi: lo username finisce nel DN del bind.
var usernameRe = regexp.MustCompile(`^[A-Za-z0-9._@-]{1,128}$`)

// IsAdminUser indica se username è nella lista esplicita (case-insensitive).
func IsAdminUser(list []string, username string) bool {
	for _, u := range list {
		if strings.EqualFold(u, username) {
			return true
		}
	}
	return false
}

func (l *LDAP) Authenticate(username, password string) (bool, bool, error) {
	username = strings.TrimSpace(username)
	if !usernameRe.MatchString(username) || password == "" {
		return false, false, nil
	}
	isAdmin := IsAdminUser(l.cfg.AdminUsers, username)

	if l.cfg.Host == "mock" {
		return true, isAdmin || len(l.cfg.AdminUsers) == 0, nil
	}

	conn, err := l.dial()
	if err != nil {
		return false, false, err
	}
	defer conn.Close()

	userDN := fmt.Sprintf(l.cfg.UserDNTemplate, username)
	if err := conn.Bind(userDN, password); err != nil {
		if ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials) {
			return false, false, nil
		}
		return false, false, fmt.Errorf("ldap bind: %w", err)
	}

	if l.cfg.RequiredGroup == "" && l.cfg.AdminGroup == "" {
		return true, isAdmin, nil
	}

	if l.cfg.BindDN != "" {
		if err := conn.Bind(l.cfg.BindDN, l.cfg.BindPassword); err != nil {
			return false, false, fmt.Errorf("ldap bind di servizio: %w", err)
		}
	}

	entry, err := l.findUser(conn, username)
	if err != nil {
		return false, false, err
	}
	memberOf := entry.GetAttributeValues("memberOf")

	hasRequired := l.cfg.RequiredGroup == "" ||
		inGroup(memberOf, l.cfg.RequiredGroup) ||
		l.nestedMember(conn, l.cfg.RequiredGroup, entry.DN)
	if !isAdmin && l.cfg.AdminGroup != "" {
		isAdmin = inGroup(memberOf, l.cfg.AdminGroup) || l.nestedMember(conn, l.cfg.AdminGroup, entry.DN)
	}
	if !hasRequired && !isAdmin {
		slog.Info("ldap: utente fuori dal gruppo richiesto", "user", username, "group", l.cfg.RequiredGroup)
		return false, false, nil
	}
	return true, isAdmin, nil
}

func (l *LDAP) dial() (*ldap.Conn, error) {
	tlsCfg := &tls.Config{
		InsecureSkipVerify: l.cfg.TLSSkipVerify, //nolint:gosec // opzione esplicita per CA interne
		ServerName:         ldapHostname(l.cfg.Host),
	}
	conn, err := ldap.DialURL(l.cfg.Host, ldap.DialWithTLSConfig(tlsCfg))
	if err != nil {
		return nil, fmt.Errorf("ldap dial: %w", err)
	}
	conn.SetTimeout(5 * time.Second)

	if l.cfg.StartTLS && strings.HasPrefix(l.cfg.Host, "ldap://") {
		if err := conn.StartTLS(tlsCfg); err != nil {
			conn.Close()
			return nil, fmt.Errorf("ldap StartTLS: %w (LDAP_STARTTLS=false solo su rete fidata)", err)
		}
	}
	return conn, nil
}

func (l *LDAP) findUser(conn *ldap.Conn, username string) (*ldap.Entry, error) {
	plain := strings.SplitN(username, "@", 2)[0]
	filter := fmt.Sprintf("(|(sAMAccountName=%s)(userPrincipalName=%s)(uid=%s))",
		ldap.EscapeFilter(plain), ldap.EscapeFilter(username), ldap.EscapeFilter(plain))
	res, err := conn.Search(ldap.NewSearchRequest(
		l.cfg.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
		filter, []string{"dn", "memberOf"}, nil,
	))
	if err != nil {
		return nil, fmt.Errorf("ldap search: %w", err)
	}
	if len(res.Entries) == 0 {
		return nil, errors.New("ldap: utente non trovato sotto LDAP_BASE_DN")
	}
	return res.Entries[0], nil
}

// nestedMember usa LDAP_MATCHING_RULE_IN_CHAIN (solo AD) per i gruppi annidati.
func (l *LDAP) nestedMember(conn *ldap.Conn, group, userDN string) bool {
	attr := "cn"
	if strings.Contains(group, "=") {
		attr = "distinguishedName"
	}
	filter := fmt.Sprintf("(&(objectClass=group)(%s=%s)(member:1.2.840.113556.1.4.1941:=%s))",
		attr, ldap.EscapeFilter(group), ldap.EscapeFilter(userDN))
	res, err := conn.Search(ldap.NewSearchRequest(
		l.cfg.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
		filter, []string{"cn"}, nil,
	))
	if err != nil {
		slog.Warn("ldap: ricerca gruppi annidati fallita", "group", group, "err", err)
		return false
	}
	return len(res.Entries) > 0
}

// inGroup confronta memberOf con un CN ("CED Admin") o un DN completo.
func inGroup(memberOf []string, group string) bool {
	g := strings.ToLower(group)
	for _, dn := range memberOf {
		d := strings.ToLower(dn)
		if d == g || strings.HasPrefix(d, "cn="+g+",") {
			return true
		}
	}
	return false
}

func ldapHostname(ldapURL string) string {
	u, err := url.Parse(ldapURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
```

`internal/auth/ratelimit.go`:

```go
package auth

import (
	"sync"
	"time"
)

const (
	baseBlock  = 30 * time.Second
	maxBlock   = 15 * time.Minute
	maxEntries = 10000
)

// RateLimiter blocca una chiave (es. "u:mrossi", "ip:10.0.0.1") dopo max
// fallimenti entro window, con attesa che raddoppia a ogni blocco (max 15 min).
// Stato solo in memoria: si azzera al riavvio.
type RateLimiter struct {
	mu      sync.Mutex
	max     int
	window  time.Duration
	now     func() time.Time
	entries map[string]*limitEntry
}

type limitEntry struct {
	failures     []time.Time
	strikes      int
	blockedUntil time.Time
}

func NewRateLimiter(max int, window time.Duration) *RateLimiter {
	return &RateLimiter{max: max, window: window, now: time.Now, entries: map[string]*limitEntry{}}
}

// Allow restituisce l'attesa più lunga tra le chiavi bloccate; ok=false se c'è.
func (r *RateLimiter) Allow(keys ...string) (time.Duration, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	var wait time.Duration
	for _, k := range keys {
		if e := r.entries[k]; e != nil && now.Before(e.blockedUntil) {
			if d := e.blockedUntil.Sub(now); d > wait {
				wait = d
			}
		}
	}
	return wait, wait == 0
}

func (r *RateLimiter) Fail(keys ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	if len(r.entries) > maxEntries {
		r.prune(now)
	}
	for _, k := range keys {
		e := r.entries[k]
		if e == nil {
			e = &limitEntry{}
			r.entries[k] = e
		}
		recent := e.failures[:0]
		for _, f := range e.failures {
			if now.Sub(f) < r.window {
				recent = append(recent, f)
			}
		}
		e.failures = append(recent, now)
		if len(e.failures) >= r.max {
			if e.strikes < 10 {
				e.strikes++
			}
			block := baseBlock << (e.strikes - 1)
			if block > maxBlock {
				block = maxBlock
			}
			e.blockedUntil = now.Add(block)
			e.failures = nil
		}
	}
}

func (r *RateLimiter) Success(keys ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, k := range keys {
		delete(r.entries, k)
	}
}

// prune elimina le chiavi non bloccate e senza fallimenti recenti.
func (r *RateLimiter) prune(now time.Time) {
	for k, e := range r.entries {
		if now.After(e.blockedUntil) && (len(e.failures) == 0 || now.Sub(e.failures[len(e.failures)-1]) > r.window) {
			delete(r.entries, k)
		}
	}
}
```

Nota per il test del blocco massimo: `strikes` sale fino a 10 (30s·2⁹ > 15 min), quindi l'attesa si stabilizza a 15 minuti.

- [ ] **Step 4: Verifica e commit**

Run: `go vet ./... && go test ./internal/auth/ -v`
Expected: PASS

```bash
git add internal/auth go.mod go.sum
git commit -m "feat(auth): login LDAP (porting GoPulley) e rate limiter

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: Fondamenta web — Server, render, header di sicurezza, wiring

**Files:**
- Create: `internal/web/server.go`, `internal/web/render.go`, `internal/web/middleware.go`
- Test: `internal/web/server_test.go`, `internal/web/render_test.go`
- Modify: `cmd/server/main.go`

**Interfaces:**
- Consumes: `config.Config` (Task 1), `database.DB`, `database.LevelUrgent/...` (Task 2–4), `auth.Authenticator`, `auth.NewRateLimiter` (Task 6)
- Produces:
  ```go
  type Options struct {
      DB      *database.DB
      Config  config.Config
      Auth    auth.Authenticator
      Version string
      WebDir  string           // default "web"; nei test "../../web"
      Now     func() time.Time // default time.Now
  }
  type Server struct { db; cfg; auth; limiter; tmpl; store; version; webDir; now; mux }
  func New(o Options) (*Server, error)
  func (s *Server) Handler() http.Handler
  func (s *Server) render(w http.ResponseWriter, status int, name string, data any)
  func (s *Server) serverError(w http.ResponseWriter, err error)
  func (s *Server) loc() *time.Location
  // template funcs: monogram, levelLabel, linkify, fmtDate, fmtDatePtr, inputTime, inputTimePtr, derefID
  func monogram(title string) string
  func levelLabel(level string) string
  func linkify(text string) template.HTML
  const cspPolicy = "default-src 'self'; ..."
  ```
  Le route vengono registrate in `func (s *Server) routes()`; i task successivi aggiungono righe lì.

- [ ] **Step 1: Test funzioni di template** — `internal/web/render_test.go`

```go
package web

import (
	"strings"
	"testing"
)

func TestMonogram(t *testing.T) {
	for in, want := range map[string]string{
		"Rubrica":             "Ru",
		"Sito comunale":       "SC",
		"albo pretorio":       "AP",
		"  ":                  "?",
		"Èlite":               "Èl",
		"Sportello del cittadino": "SD",
	} {
		if got := monogram(in); got != want {
			t.Errorf("monogram(%q) = %q, atteso %q", in, got, want)
		}
	}
}

func TestLevelLabel(t *testing.T) {
	if levelLabel("urgent") != "Urgente" || levelLabel("maintenance") != "Manutenzione" || levelLabel("news") != "Novità" {
		t.Fatal("etichette livello errate")
	}
}

func TestLinkify(t *testing.T) {
	got := string(linkify("Vedi https://wiki.local/a?b=1&c=2 <b>ora</b>\nriga 2"))
	if !strings.Contains(got, `<a href="https://wiki.local/a?b=1&amp;c=2" target="_blank" rel="noopener">`) {
		t.Fatalf("link mancante o non escapato: %s", got)
	}
	if strings.Contains(got, "<b>") || !strings.Contains(got, "&lt;b&gt;ora&lt;/b&gt;") {
		t.Fatalf("HTML del testo non escapato: %s", got)
	}
	if strings.Contains(linkify(`javascript:alert(1)`), "<a") {
		t.Fatal("solo http/https diventano link")
	}
}
```

- [ ] **Step 2: Test server** — `internal/web/server_test.go`

```go
package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/auth"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/config"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

var fixedNow = time.Date(2026, 10, 6, 8, 0, 0, 0, time.UTC) // 10:00 a Roma

type fakeAuth struct {
	ok, admin bool
	err       error
}

func (f fakeAuth) Authenticate(_, _ string) (bool, bool, error) { return f.ok, f.admin, f.err }

func newTestServer(t *testing.T, a auth.Authenticator) (*Server, *database.DB) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	rome, _ := time.LoadLocation("Europe/Rome")
	if a == nil {
		a = fakeAuth{ok: true, admin: true}
	}
	s, err := New(Options{
		DB:      db,
		Config:  config.Config{SessionSecret: strings.Repeat("s", 32), UploadDir: t.TempDir(), Location: rome},
		Auth:    a,
		Version: "test",
		WebDir:  "../../web",
		Now:     func() time.Time { return fixedNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, db
}

// do esegue una richiesta sul handler completo (middleware inclusi).
func do(t *testing.T, s *Server, method, target string, form url.Values, cookie *http.Cookie, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	req := httptest.NewRequest(method, target, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestHealth(t *testing.T) {
	s, _ := newTestServer(t, nil)
	rec := do(t, s, "GET", "/health", nil, nil, nil)
	var body map[string]string
	json.NewDecoder(rec.Body).Decode(&body)
	if rec.Code != 200 || body["status"] != "ok" || body["version"] != "test" {
		t.Fatalf("health: %d %v", rec.Code, body)
	}
}

func TestSecurityHeaders(t *testing.T) {
	s, _ := newTestServer(t, nil)
	rec := do(t, s, "GET", "/", nil, nil, nil)
	if got := rec.Header().Get("Content-Security-Policy"); got != cspPolicy {
		t.Fatalf("CSP: %q", got)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("Referrer-Policy") != "same-origin" {
		t.Fatalf("header mancanti: %v", rec.Header())
	}
}

func TestCrossOriginPostRejected(t *testing.T) {
	s, _ := newTestServer(t, nil)
	rec := do(t, s, "POST", "/admin/login", url.Values{"username": {"x"}}, nil,
		map[string]string{"Sec-Fetch-Site": "cross-site"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST cross-origin: atteso 403, ottenuto %d", rec.Code)
	}
}

func TestStaticServed(t *testing.T) {
	s, _ := newTestServer(t, nil)
	if rec := do(t, s, "GET", "/static/js/htmx.min.js", nil, nil, nil); rec.Code != 200 {
		t.Fatalf("static: %d", rec.Code)
	}
}
```

Run: `go test ./internal/web/`
Expected: FAIL — package senza sorgenti / `undefined: New`

- [ ] **Step 3: Dipendenza sessioni**

```bash
go get github.com/gorilla/sessions@latest
```

- [ ] **Step 4: Implementa** — `internal/web/middleware.go`

```go
package web

import "net/http"

// cspPolicy: tutto self-hosted; img https per le icone da URL esterno;
// style-src-attr per lo style="background:#rrggbb" delle icone.
const cspPolicy = "default-src 'self'; img-src 'self' https: data:; style-src 'self'; " +
	"style-src-attr 'unsafe-inline'; script-src 'self'; frame-ancestors 'none'; " +
	"base-uri 'self'; form-action 'self'"

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", cspPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}
```

`internal/web/render.go`:

```go
package web

import (
	"bytes"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

const inputTimeLayout = "2006-01-02T15:04" // <input type="datetime-local">

func (s *Server) funcs() template.FuncMap {
	return template.FuncMap{
		"monogram":   monogram,
		"levelLabel": levelLabel,
		"linkify":    linkify,
		"fmtDate":    func(t time.Time) string { return t.In(s.loc()).Format("02/01/2006 15:04") },
		"fmtDatePtr": func(t *time.Time) string {
			if t == nil {
				return ""
			}
			return t.In(s.loc()).Format("02/01/2006 15:04")
		},
		"inputTime": func(t time.Time) string { return t.In(s.loc()).Format(inputTimeLayout) },
		"derefID": func(p *int64) int64 {
			if p == nil {
				return 0
			}
			return *p
		},
	}
}

// render esegue il template in un buffer: un errore a metà non produce mai
// una pagina troncata con status 200.
func (s *Server) render(w http.ResponseWriter, status int, name string, data any) {
	var buf bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		s.serverError(w, fmt.Errorf("template %s: %w", name, err))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	buf.WriteTo(w)
}

func (s *Server) serverError(w http.ResponseWriter, err error) {
	slog.Error("errore interno", "err", err)
	http.Error(w, "Si è verificato un errore. Riprova più tardi.", http.StatusInternalServerError)
}

// monogram: iniziali delle prime due parole, oppure prime due lettere se una sola parola.
func monogram(title string) string {
	words := strings.FieldsFunc(title, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	var significant []string
	for _, w := range words {
		if len([]rune(w)) > 2 || len(words) == 1 {
			significant = append(significant, w)
		}
	}
	switch {
	case len(significant) == 0:
		return "?"
	case len(significant) == 1:
		r := []rune(significant[0])
		if len(r) == 1 {
			return strings.ToUpper(string(r))
		}
		return strings.ToUpper(string(r[0])) + strings.ToLower(string(r[1]))
	default:
		a, b := []rune(significant[0]), []rune(significant[1])
		return strings.ToUpper(string(a[0]) + string(b[0]))
	}
}

func levelLabel(level string) string {
	switch level {
	case database.LevelUrgent:
		return "Urgente"
	case database.LevelMaintenance:
		return "Manutenzione"
	default:
		return "Novità"
	}
}

var urlRe = regexp.MustCompile(`https?://[^\s<>"']+`)

// linkify escapa il testo e rende cliccabili solo gli URL http/https.
func linkify(text string) template.HTML {
	var b strings.Builder
	last := 0
	for _, m := range urlRe.FindAllStringIndex(text, -1) {
		b.WriteString(template.HTMLEscapeString(text[last:m[0]]))
		u := template.HTMLEscapeString(text[m[0]:m[1]])
		fmt.Fprintf(&b, `<a href="%s" target="_blank" rel="noopener">%s</a>`, u, u)
		last = m[1]
	}
	b.WriteString(template.HTMLEscapeString(text[last:]))
	return template.HTML(b.String()) //nolint:gosec // testo escapato sopra
}
```

Nota su `monogram`: le parole di ≤2 lettere ("di", "del" no — "del" ha 3 lettere) vengono saltate solo se ci sono più parole: "Sportello del cittadino" → "SD".

`internal/web/server.go`:

```go
// Package web contiene handler HTTP, template e middleware di CruscottoPA.
package web

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"
	"time"

	"github.com/gorilla/sessions"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/auth"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/config"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

type Options struct {
	DB      *database.DB
	Config  config.Config
	Auth    auth.Authenticator
	Version string
	WebDir  string
	Now     func() time.Time
}

type Server struct {
	db      *database.DB
	cfg     config.Config
	auth    auth.Authenticator
	limiter *auth.RateLimiter
	tmpl    *template.Template
	store   *sessions.CookieStore
	version string
	webDir  string
	now     func() time.Time
	mux     *http.ServeMux
}

func New(o Options) (*Server, error) {
	if o.WebDir == "" {
		o.WebDir = "web"
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Config.Location == nil {
		o.Config.Location = time.UTC
	}
	s := &Server{
		db:      o.DB,
		cfg:     o.Config,
		auth:    o.Auth,
		limiter: auth.NewRateLimiter(5, 15*time.Minute),
		version: o.Version,
		webDir:  o.WebDir,
		now:     o.Now,
		mux:     http.NewServeMux(),
	}
	tmpl, err := template.New("").Funcs(s.funcs()).ParseGlob(filepath.Join(o.WebDir, "templates", "*.html"))
	if err != nil {
		return nil, fmt.Errorf("template: %w", err)
	}
	s.tmpl = tmpl
	s.routes()
	return s, nil
}

func (s *Server) loc() *time.Location { return s.cfg.Location }

// Handler applica, dall'esterno: header di sicurezza, protezione CSRF
// (Sec-Fetch-Site/Origin, solo metodi non sicuri), routing.
func (s *Server) Handler() http.Handler {
	return securityHeaders(http.NewCrossOriginProtection().Handler(s.mux))
}

func (s *Server) routes() {
	static := http.FileServer(http.Dir(filepath.Join(s.webDir, "static")))
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", static))
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /{$}", s.handleIndex)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	status, code := "ok", http.StatusOK
	if err := s.db.PingContext(r.Context()); err != nil {
		status, code = "db unavailable", http.StatusServiceUnavailable
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"status": status, "version": s.version})
}

// handleIndex è temporaneo: il Task 8 lo sostituisce con la plancia.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	s.render(w, http.StatusOK, "index.html", map[string]any{"Version": s.version})
}
```

`store` resta nil fino al Task 9; il campo esiste già perché `Server` non cambi forma.

- [ ] **Step 5: Wiring in `main.go`**

Sostituisci in `cmd/server/main.go`: rimuovi `tmpl`, `mux`, `handleHealth` e la registrazione delle route; importa `internal/auth` e `internal/web`. Dopo l'apertura del DB:

```go
	if err := os.MkdirAll(cfg.UploadDir, 0o750); err != nil {
		slog.Error("creazione UPLOAD_DIR", "err", err)
		os.Exit(1)
	}

	srv, err := web.New(web.Options{
		DB:      db,
		Config:  cfg,
		Auth:    auth.NewLDAP(cfg.LDAP),
		Version: AppVersion,
	})
	if err != nil {
		slog.Error("inizializzazione web", "err", err)
		os.Exit(1)
	}

	httpSrv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
```

e rinomina `srv` in `httpSrv` nel resto di `main()` (`ListenAndServe`, `Shutdown`). Rimuovi gli import non più usati (`encoding/json`, `html/template`). Nel log di avvio aggiungi `"ldap", cfg.LDAP.Host`.

- [ ] **Step 6: Verifica e commit**

Run: `go vet ./... && go test ./... && go build ./cmd/server`
Expected: PASS, build ok

```bash
git add internal/web cmd/server/main.go go.mod go.sum
git commit -m "feat(web): server HTTP con header di sicurezza e protezione CSRF

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 8: Plancia (`GET /`)

**Files:**
- Delete: `web/templates/index.html`
- Create: `internal/web/dashboard.go`, `web/templates/dashboard.html`, `web/templates/partials_dashboard.html`, `web/static/js/dashboard.js`
- Modify: `web/static/css/app.css` (riscrittura), `internal/web/server.go` (route)
- Test: `internal/web/dashboard_test.go`

**Interfaces:**
- Consumes: `database.GetDashboard`, `database.ListActiveAlerts`, funcs di template (Task 7)
- Produces:
  - template `dashboard.html`, `alerts_strip` (dati: `[]database.Alert`), `app_tile` (dati: `database.AppWithGuides`), `app_icon` (dati: `database.App`) — `app_icon` è riusato dall'admin nei Task 9–11
  - `func greeting(hour int) string`, `func italianDate(t time.Time) string`
  - route `GET /{$}`, `GET /partials/alerts`

- [ ] **Step 1: Test che falliscono** — `internal/web/dashboard_test.go`

```go
package web

import (
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

func id64(v int64) *int64 { return &v }

func TestDashboardFreshInstall(t *testing.T) {
	s, _ := newTestServer(t, nil)
	rec := do(t, s, "GET", "/", nil, nil, nil)
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, "Nessun applicativo configurato") {
		t.Fatalf("installazione nuova: atteso messaggio vuoto, ottenuto %d\n%s", rec.Code, body)
	}
	if strings.Contains(body, `class="guides"`) || strings.Contains(body, `class="ticker"`) {
		t.Fatal("colonna guide e striscia avvisi non devono esserci se vuote")
	}
	if !strings.Contains(body, "Buongiorno") || !strings.Contains(body, "martedì 6 ottobre 2026") {
		t.Fatal("saluto/data iniziali mancanti (10:00 Europe/Rome)")
	}
	if !strings.Contains(body, "vtest") {
		t.Fatal("versione nel footer mancante")
	}
}

func TestDashboardTilesGuidesAlerts(t *testing.T) {
	s, db := newTestServer(t, nil)
	apps, _ := db.ListApps()
	rubrica, webmail := apps[0], apps[1]
	rubrica.URL, webmail.URL = "https://rubrica.local", "https://mail.local"
	db.UpdateApp(rubrica)
	db.UpdateApp(webmail)
	db.CreateGuide(database.Guide{AppID: id64(rubrica.ID), Title: "Cercare un interno", Kind: "link", URL: "https://wiki/interno", Enabled: true})
	db.CreateGuide(database.Guide{Title: "VPN da casa", Kind: "link", URL: "https://wiki/vpn", Enabled: true})
	db.CreateAlert(database.Alert{Title: "Phishing via PEC", Body: "Dettagli su https://cert.local", Level: "urgent",
		StartsAt: fixedNow.Add(-time.Hour), CreatedAt: fixedNow})

	rec := do(t, s, "GET", "/", nil, nil, nil)
	body := rec.Body.String()

	for _, want := range []string{
		`href="https://rubrica.local"`, `href="https://mail.local"`,
		`popovertarget="guides-` + itoa(rubrica.ID) + `"`, "Cercare un interno",
		`class="guides"`, "VPN da casa",
		`class="pill pill-urgent"`, "Phishing via PEC",
		`<a href="https://cert.local" target="_blank" rel="noopener">`,
		`<span class="material-icons" aria-hidden="true">contacts</span>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("manca %q", want)
		}
	}
	if strings.Contains(body, `popovertarget="guides-`+itoa(webmail.ID)+`"`) {
		t.Error("Webmail non ha guide: niente segmento")
	}
}

func TestDashboardUnsafeURLNeutralized(t *testing.T) {
	s, db := newTestServer(t, nil)
	apps, _ := db.ListApps()
	a := apps[0]
	a.URL = "javascript:alert(1)" // scritto direttamente in DB, aggirando la validazione admin
	db.UpdateApp(a)
	body := do(t, s, "GET", "/", nil, nil, nil).Body.String()
	if strings.Contains(body, "javascript:alert") {
		t.Fatal("URL javascript: deve essere neutralizzato da html/template")
	}
}

func TestAlertsPartial(t *testing.T) {
	s, db := newTestServer(t, nil)
	db.CreateAlert(database.Alert{Title: "Manutenzione Sicraweb", Level: "maintenance", StartsAt: fixedNow.Add(-time.Minute), CreatedAt: fixedNow})
	rec := do(t, s, "GET", "/partials/alerts", nil, nil, nil)
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, "Manutenzione Sicraweb") || strings.Contains(body, "<html") {
		t.Fatalf("partial avvisi: %d\n%s", rec.Code, body)
	}
}

func TestGreetingAndDate(t *testing.T) {
	for h, want := range map[int]string{0: "Buonasera", 6: "Buongiorno", 12: "Buongiorno", 13: "Buon pomeriggio", 17: "Buon pomeriggio", 18: "Buonasera"} {
		if got := greeting(h); got != want {
			t.Errorf("greeting(%d) = %q, atteso %q", h, got, want)
		}
	}
	if got := italianDate(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)); got != "domenica 1 marzo 2026" {
		t.Errorf("italianDate: %q", got)
	}
}
```

Aggiungi in `server_test.go` l'helper:

```go
func itoa(v int64) string { return strconv.FormatInt(v, 10) }
```

(con import `"strconv"`).

Nota: "Buonasera" anche per le ore 0–5 (prima delle 6 non è ancora "giorno").

Run: `go test ./internal/web/`
Expected: FAIL — `undefined: greeting`, template `dashboard.html` inesistente

- [ ] **Step 2: Handler** — `internal/web/dashboard.go`

```go
package web

import (
	"fmt"
	"net/http"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

type dashboardView struct {
	database.Dashboard
	Greeting  string
	DateLabel string
	Clock     string
	Version   string
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	d, err := s.db.GetDashboard(s.now())
	if err != nil {
		s.serverError(w, err)
		return
	}
	now := s.now().In(s.loc())
	s.render(w, http.StatusOK, "dashboard.html", dashboardView{
		Dashboard: d,
		Greeting:  greeting(now.Hour()),
		DateLabel: italianDate(now),
		Clock:     now.Format("15:04"),
		Version:   s.version,
	})
}

func (s *Server) handleAlertsPartial(w http.ResponseWriter, r *http.Request) {
	alerts, err := s.db.ListActiveAlerts(s.now())
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.render(w, http.StatusOK, "alerts_strip", alerts)
}

// greeting: stesse soglie di dashboard.js (che lo aggiorna lato client).
func greeting(hour int) string {
	switch {
	case hour >= 6 && hour < 13:
		return "Buongiorno"
	case hour >= 13 && hour < 18:
		return "Buon pomeriggio"
	default:
		return "Buonasera"
	}
}

var (
	weekdays = [...]string{"domenica", "lunedì", "martedì", "mercoledì", "giovedì", "venerdì", "sabato"}
	months   = [...]string{"gennaio", "febbraio", "marzo", "aprile", "maggio", "giugno",
		"luglio", "agosto", "settembre", "ottobre", "novembre", "dicembre"}
)

func italianDate(t time.Time) string {
	return fmt.Sprintf("%s %d %s %d", weekdays[t.Weekday()], t.Day(), months[t.Month()-1], t.Year())
}
```

In `routes()` sostituisci la riga di `handleIndex` con:

```go
	s.mux.HandleFunc("GET /{$}", s.handleDashboard)
	s.mux.HandleFunc("GET /partials/alerts", s.handleAlertsPartial)
```

ed elimina `handleIndex` da `server.go`.

- [ ] **Step 3: Template** — elimina `web/templates/index.html` (`git rm`), crea `web/templates/dashboard.html`

```html
{{define "dashboard.html"}}<!doctype html>
<html lang="it">
<head>
	<meta charset="utf-8">
	<meta name="viewport" content="width=device-width, initial-scale=1">
	<meta name="htmx-config" content='{"includeIndicatorStyles":false}'>
	<title>CruscottoPA</title>
	<link rel="stylesheet" href="/static/fonts/material-icons.css">
	<link rel="stylesheet" href="/static/css/app.css">
	<script src="/static/js/htmx.min.js" defer></script>
	<script src="/static/js/dashboard.js" defer></script>
</head>
<body>
	<header class="top">
		<div>
			<div class="hello" data-greeting>{{.Greeting}}</div>
			<div class="date" data-date>{{.DateLabel}}</div>
		</div>
		<div class="clock" data-clock>{{.Clock}}</div>
		<label class="search">
			<span class="material-icons" aria-hidden="true">search</span>
			<input type="search" id="search" placeholder="Cerca applicativo o guida…" autocomplete="off" aria-label="Cerca applicativo o guida">
			<kbd>/</kbd>
		</label>
	</header>

	<div id="alerts" hx-get="/partials/alerts" hx-trigger="every 300s">{{template "alerts_strip" .Alerts}}</div>

	<main class="body">
		<section class="apps">
			{{range .Categories}}
			<div class="category" data-category>
				<h2>{{.Name}}</h2>
				<div class="grid">
					{{range .Apps}}{{template "app_tile" .}}{{end}}
				</div>
			</div>
			{{else}}
			<p class="empty">Nessun applicativo configurato. Gli amministratori possono aggiungerli dal <a href="/admin">pannello di amministrazione</a>.</p>
			{{end}}
			<p class="empty" id="no-results" hidden>Nessun risultato.</p>
		</section>

		{{if .GeneralGuides}}
		<aside class="guides">
			<h2>Guide generali</h2>
			<ul>
				{{range .GeneralGuides}}
				<li data-search-item data-search="{{.Title}}"><a href="{{.URL}}" target="_blank" rel="noopener">{{.Title}}</a></li>
				{{end}}
			</ul>
		</aside>
		{{end}}
	</main>

	<footer>CruscottoPA v{{.Version}}</footer>
</body>
</html>{{end}}
```

`web/templates/partials_dashboard.html`:

```html
{{define "alerts_strip"}}{{if .}}
<div class="ticker">
	{{range .}}
	<button type="button" class="pill pill-{{.Level}}" data-dialog="alert-{{.ID}}"><b>{{levelLabel .Level}}</b> {{.Title}}</button>
	{{end}}
</div>
{{range .}}
<dialog id="alert-{{.ID}}" class="alert-dialog alert-{{.Level}}">
	<h3><span class="badge">{{levelLabel .Level}}</span> {{.Title}}</h3>
	<p class="when">Dal {{fmtDate .StartsAt}}{{if .EndsAt}} al {{fmtDatePtr .EndsAt}}{{end}}</p>
	{{if .Body}}<div class="alert-body">{{linkify .Body}}</div>{{end}}
	<form method="dialog"><button>Chiudi</button></form>
</dialog>
{{end}}
{{end}}{{end}}

{{define "app_icon"}}{{if eq .IconKind "pack"}}<span class="app-icon" style="background:{{.IconColor}}"><span class="material-icons" aria-hidden="true">{{.IconValue}}</span></span>{{else if eq .IconKind "upload"}}<img class="app-icon" src="/uploads/icons/{{.IconValue}}" alt="">{{else if eq .IconKind "url"}}<img class="app-icon" src="{{.IconValue}}" alt="" data-fallback="{{monogram .Title}}" data-color="{{.IconColor}}">{{else}}<span class="app-icon" style="background:{{.IconColor}}">{{monogram .Title}}</span>{{end}}{{end}}

{{define "app_tile"}}
<div class="tile" data-search-item data-search="{{.Title}} {{.Description}}{{range .Guides}} {{.Title}}{{end}}">
	<a class="tile-main" href="{{.URL}}" target="_blank" rel="noopener">
		{{template "app_icon" .App}}
		<span class="tile-text">
			<span class="tile-title">{{.Title}}</span>
			{{with .Description}}<small>{{.}}</small>{{end}}
		</span>
	</a>
	{{if .Guides}}
	<button type="button" class="tile-guides" popovertarget="guides-{{.ID}}" aria-label="Guide per {{.Title}}"><b>?</b>{{len .Guides}}</button>
	<div class="guides-pop" id="guides-{{.ID}}" popover>
		<div class="pop-title">Guide {{.Title}}</div>
		<ul>
			{{range .Guides}}<li><a href="{{.URL}}" target="_blank" rel="noopener">{{.Title}}</a></li>{{end}}
		</ul>
	</div>
	{{end}}
</div>
{{end}}
```

- [ ] **Step 4: CSS** — sostituisci `web/static/css/app.css`

```css
:root {
	--navy: #0f1d33; --navy-2: #1b2c49; --navy-line: #2c4166; --navy-text: #8fa3c0;
	--ink: #14213d; --muted: #5b6b85; --soft: #6b7a92;
	--bg: #eef1f5; --card: #fff; --line: #dde3ec;
	--accent: #1c4f9b; --accent-bg: #e3eefc;
	--red: #9b1c1c; --red-bg: #fde8e8;
	--amb: #8a5300; --amb-bg: #fff4dc;
	--blu: #1c4f9b; --blu-bg: #e3eefc;
}

* { box-sizing: border-box; }
[hidden] { display: none !important; }
body { margin: 0; font-family: "Segoe UI", system-ui, sans-serif; background: var(--bg); color: var(--ink); }
a { color: inherit; }

/* ── Testata ─────────────────────────────────────────────── */
.top { background: var(--navy); color: #e6ecf5; padding: 1rem 1.5rem; display: grid; grid-template-columns: 1fr auto; gap: .9rem 1rem; align-items: center; }
.hello { font-size: 1.35rem; font-weight: 600; }
.date { font-size: .8rem; color: var(--navy-text); }
.clock { font-variant-numeric: tabular-nums; font-size: 1.9rem; font-weight: 300; }
.search { grid-column: 1 / -1; display: flex; align-items: center; gap: .5rem; background: var(--navy-2); border: 1px solid var(--navy-line); border-radius: 8px; padding: .5rem .8rem; color: var(--navy-text); }
.search:focus-within { border-color: #5b84c4; }
.search input { flex: 1; min-width: 0; background: none; border: 0; outline: none; color: #e6ecf5; font: inherit; }
.search input::placeholder { color: var(--navy-text); }
.search kbd { background: var(--navy-line); border-radius: 4px; padding: 0 .4rem; font-size: .75rem; color: #c9d6ea; }

/* ── Avvisi ──────────────────────────────────────────────── */
#alerts:empty { display: none; }
.ticker { display: flex; flex-wrap: wrap; gap: .5rem; padding: .6rem 1.5rem; background: var(--card); border-bottom: 1px solid var(--line); }
.pill { border: 0; cursor: pointer; font: inherit; font-size: .82rem; padding: .35rem .8rem; border-radius: 999px; }
.pill-urgent, .alert-urgent .badge { background: var(--red-bg); color: var(--red); }
.pill-maintenance, .alert-maintenance .badge { background: var(--amb-bg); color: var(--amb); }
.pill-news, .alert-news .badge { background: var(--blu-bg); color: var(--blu); }
.alert-dialog { border: 0; border-radius: 10px; width: min(560px, 92vw); padding: 1.25rem 1.4rem; box-shadow: 0 20px 60px rgba(15, 29, 51, .3); color: var(--ink); }
.alert-dialog::backdrop { background: rgba(15, 29, 51, .45); }
.alert-dialog h3 { margin: 0 0 .3rem; font-size: 1.1rem; }
.badge { font-size: .7rem; text-transform: uppercase; letter-spacing: .06em; padding: .15rem .45rem; border-radius: 4px; vertical-align: middle; }
.when { margin: 0 0 .8rem; font-size: .8rem; color: var(--soft); }
.alert-body { white-space: pre-line; line-height: 1.5; }
.alert-body a { color: var(--accent); }
.alert-dialog form { text-align: right; margin-top: 1rem; }
.alert-dialog button { font: inherit; border: 1px solid var(--line); background: var(--card); border-radius: 6px; padding: .4rem .9rem; cursor: pointer; }

/* ── Corpo ───────────────────────────────────────────────── */
.body { display: grid; grid-template-columns: minmax(0, 1fr) 260px; gap: 1.25rem; padding: 1.25rem 1.5rem 1.5rem; max-width: 1400px; margin: 0 auto; }
.body:not(:has(.guides)) { grid-template-columns: minmax(0, 1fr); }
h2 { font-size: .7rem; text-transform: uppercase; letter-spacing: .1em; color: var(--muted); margin: 0 0 .6rem; font-weight: 600; }
.category { margin-bottom: 1.1rem; }
.grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: .6rem; }
.empty { color: var(--soft); font-size: .9rem; }
.empty a { color: var(--accent); }

/* ── Tile ────────────────────────────────────────────────── */
.tile { display: flex; background: var(--card); border: 1px solid var(--line); border-radius: 8px; transition: border-color .15s, box-shadow .15s; }
.tile:hover { border-color: #b9c7dc; box-shadow: 0 2px 10px rgba(15, 29, 51, .06); }
.tile-main { flex: 1; min-width: 0; display: flex; gap: .6rem; align-items: center; padding: .7rem; text-decoration: none; border-radius: 8px; }
.tile-main:focus-visible { outline: 2px solid var(--accent); outline-offset: -2px; }
.tile-text { min-width: 0; }
.tile-title { display: block; font-size: .88rem; font-weight: 600; line-height: 1.2; }
.tile-text small { display: block; color: var(--soft); font-size: .72rem; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.app-icon { width: 34px; height: 34px; border-radius: 7px; flex-shrink: 0; display: grid; place-items: center; color: #fff; font-weight: 700; font-size: .8rem; object-fit: cover; background: #475569; }
img.app-icon { background: none; }
.tile-guides { width: 46px; flex-shrink: 0; border: 0; border-left: 1px solid var(--line); background: none; border-radius: 0 8px 8px 0; color: var(--accent); font: inherit; font-size: .65rem; font-weight: 700; cursor: pointer; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: .1rem; }
.tile-guides b { font-size: 1rem; }
.tile-guides:hover, .tile-guides:focus-visible { background: var(--accent-bg); }

/* ── Popover guide di un'app (posizionato da dashboard.js) ── */
.guides-pop { position: fixed; inset: auto; margin: 0; width: 260px; background: var(--card); color: var(--ink); border: 1px solid #c7d3e6; border-radius: 8px; box-shadow: 0 10px 30px rgba(15, 29, 51, .18); padding: .6rem .75rem; }
.pop-title { font-size: .65rem; text-transform: uppercase; letter-spacing: .08em; color: var(--muted); margin-bottom: .3rem; }

/* ── Liste guide ─────────────────────────────────────────── */
.guides-pop ul, .guides ul { list-style: none; margin: 0; padding: 0; }
.guides-pop li a, .guides li a { display: block; font-size: .84rem; padding: .42rem 0; text-decoration: none; }
.guides-pop li + li, .guides li + li { border-top: 1px dashed var(--line); }
.guides-pop a:hover, .guides a:hover { color: var(--accent); }
.guides li a::before { content: "›"; color: var(--accent); font-weight: 700; margin-right: .5rem; }
.guides { align-self: start; background: var(--card); border: 1px solid var(--line); border-radius: 8px; padding: .9rem; }

footer { text-align: center; padding: 1rem; font-size: .75rem; color: var(--soft); }

/* ── Responsive ──────────────────────────────────────────── */
@media (max-width: 1100px) { .grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@media (max-width: 900px) { .body { grid-template-columns: minmax(0, 1fr); } }
@media (max-width: 640px) {
	.grid { grid-template-columns: minmax(0, 1fr); }
	.top, .body { padding: 1rem; }
	.ticker { padding: .6rem 1rem; }
	.clock { font-size: 1.5rem; }
	.search kbd { display: none; }
}
```

- [ ] **Step 5: JS** — `web/static/js/dashboard.js`

```js
// Plancia: orologio/saluto, ricerca, dialog avvisi, popover guide, fallback icone.
// Nessun handler inline: la CSP consente solo script da 'self'.
(() => {
	"use strict";

	const DAYS = ["domenica", "lunedì", "martedì", "mercoledì", "giovedì", "venerdì", "sabato"];
	const MONTHS = ["gennaio", "febbraio", "marzo", "aprile", "maggio", "giugno",
		"luglio", "agosto", "settembre", "ottobre", "novembre", "dicembre"];

	// Stesse soglie di greeting() in internal/web/dashboard.go.
	const greeting = (h) => (h >= 6 && h < 13) ? "Buongiorno" : (h >= 13 && h < 18) ? "Buon pomeriggio" : "Buonasera";

	function tick() {
		const now = new Date();
		const set = (sel, text) => { const el = document.querySelector(sel); if (el) el.textContent = text; };
		set("[data-clock]", now.toLocaleTimeString("it-IT", { hour: "2-digit", minute: "2-digit" }));
		set("[data-greeting]", greeting(now.getHours()));
		set("[data-date]", `${DAYS[now.getDay()]} ${now.getDate()} ${MONTHS[now.getMonth()]} ${now.getFullYear()}`);
	}
	tick();
	setInterval(tick, 15000);

	// ── Ricerca ──────────────────────────────────────────────
	const input = document.getElementById("search");
	const norm = (s) => s.normalize("NFD").replace(/[̀-ͯ]/g, "").toLowerCase();

	function filter() {
		const q = norm(input.value.trim());
		let any = false;
		document.querySelectorAll("[data-search-item]").forEach((el) => {
			const hit = q === "" || norm(el.dataset.search || "").includes(q);
			el.hidden = !hit;
			any = any || hit;
		});
		document.querySelectorAll("[data-category]").forEach((c) => {
			c.hidden = !c.querySelector("[data-search-item]:not([hidden])");
		});
		const guides = document.querySelector("aside.guides");
		if (guides) guides.hidden = !guides.querySelector("[data-search-item]:not([hidden])");
		const none = document.getElementById("no-results");
		if (none) none.hidden = any || document.querySelectorAll("[data-search-item]").length === 0;
	}

	if (input) {
		input.addEventListener("input", filter);
		document.addEventListener("keydown", (e) => {
			const typing = e.target.closest("input, textarea, select, [contenteditable]");
			if (e.key === "/" && !typing) {
				e.preventDefault();
				input.focus();
			} else if (e.key === "Escape" && e.target === input) {
				input.value = "";
				filter();
				input.blur();
			}
		});
	}

	// ── Dialog avvisi (delega: la striscia è ricaricata da HTMX) ──
	document.addEventListener("click", (e) => {
		const btn = e.target.closest("[data-dialog]");
		if (btn) document.getElementById(btn.dataset.dialog)?.showModal();
	});

	// ── Posizione popover guide sotto il segmento "?" ──
	document.addEventListener("toggle", (e) => {
		const pop = e.target;
		if (!(pop instanceof HTMLElement) || !pop.matches(".guides-pop") || e.newState !== "open") return;
		const btn = document.querySelector(`[popovertarget="${pop.id}"]`);
		if (!btn) return;
		const r = btn.getBoundingClientRect();
		const left = Math.max(8, Math.min(r.right - pop.offsetWidth, window.innerWidth - pop.offsetWidth - 8));
		const below = r.bottom + 6;
		const top = below + pop.offsetHeight > window.innerHeight - 8 ? Math.max(8, r.top - pop.offsetHeight - 6) : below;
		pop.style.left = `${left}px`;
		pop.style.top = `${top}px`;
	}, true);

	// ── Icona da URL esterno non raggiungibile → monogramma ──
	document.addEventListener("error", (e) => {
		const img = e.target;
		if (!(img instanceof HTMLImageElement) || !img.dataset.fallback) return;
		const span = document.createElement("span");
		span.className = "app-icon";
		span.textContent = img.dataset.fallback;
		span.style.background = img.dataset.color || "";
		img.replaceWith(span);
	}, true);
})();
```

- [ ] **Step 6: Verifica automatica**

Run: `go vet ./... && go test ./internal/web/ -v`
Expected: PASS

- [ ] **Step 7: Verifica manuale nel browser**

```bash
go run ./cmd/server
```

Apri `http://localhost:8080`: messaggio "Nessun applicativo configurato". Poi da sqlite (`sqlite3 cruscotto.db "UPDATE apps SET url='https://example.org'"`) ricarica: due tile con icona Material, ricerca filtra digitando, `/` porta il focus sulla ricerca, nessun errore CSP nella console del browser. Elimina `cruscotto.db*` alla fine.

- [ ] **Step 8: Commit**

```bash
git add -A web internal/web
git commit -m "feat(web): plancia con avvisi, tile, guide agganciate e ricerca

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 9: Login admin, sessione, shell admin e panoramica

**Files:**
- Create: `internal/web/session.go`, `internal/web/admin_auth.go`, `internal/web/admin.go`
- Create: `web/templates/admin_base.html`, `web/templates/admin_login.html`, `web/templates/admin_overview.html`, `web/static/css/admin.css`
- Modify: `internal/web/server.go` (store + route)
- Test: `internal/web/admin_auth_test.go`

**Interfaces:**
- Consumes: `Server`, `render`, `do`, `newTestServer`, `fakeAuth` (Task 7), `app_icon` (Task 8), `auth.RateLimiter` (Task 6)
- Produces:
  ```go
  const sessionName = "cruscotto_admin"
  func (s *Server) currentAdmin(r *http.Request) string
  func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc
  type adminPage struct { User, Section, Version string }
  type pageView struct { adminPage; Body any }
  func (s *Server) renderPage(w http.ResponseWriter, r *http.Request, page, section string, body any)
  // test helper (admin_auth_test.go):
  func login(t *testing.T, s *Server) *http.Cookie
  ```
  Template `admin_top` / `admin_bottom` (dati: `pageView`): ogni pagina admin è `{{template "admin_top" .}} … {{template "admin_bottom" .}}` e legge i propri dati da `.Body`. `.Section` vale `overview|avvisi|app|guide|categorie`.

- [ ] **Step 1: Test che falliscono** — `internal/web/admin_auth_test.go`

```go
package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func login(t *testing.T, s *Server) *http.Cookie {
	t.Helper()
	rec := do(t, s, "POST", "/admin/login", url.Values{"username": {"mrossi"}, "password": {"pw"}}, nil, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login: %d\n%s", rec.Code, rec.Body)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionName {
			return c
		}
	}
	t.Fatal("cookie di sessione mancante")
	return nil
}

func sessionCookie(rec interface{ Result() *http.Response }) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionName {
			return c
		}
	}
	return nil
}

func TestAdminRequiresLogin(t *testing.T) {
	s, _ := newTestServer(t, nil)

	rec := do(t, s, "GET", "/admin", nil, nil, nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/login" {
		t.Fatalf("senza sessione: atteso 303 → /admin/login, ottenuto %d %q", rec.Code, rec.Header().Get("Location"))
	}

	rec = do(t, s, "GET", "/admin", nil, nil, map[string]string{"HX-Request": "true"})
	if rec.Code != http.StatusUnauthorized || rec.Header().Get("HX-Redirect") != "/admin/login" {
		t.Fatalf("HTMX senza sessione: atteso 401 + HX-Redirect, ottenuto %d %v", rec.Code, rec.Header())
	}
}

func TestLoginAndOverview(t *testing.T) {
	s, _ := newTestServer(t, nil)
	c := login(t, s)
	if !c.HttpOnly || c.Path != "/admin" || c.SameSite != http.SameSiteLaxMode {
		t.Fatalf("attributi cookie: %+v", c)
	}

	rec := do(t, s, "GET", "/admin", nil, c, nil)
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, "Panoramica") || !strings.Contains(body, "mrossi") {
		t.Fatalf("panoramica: %d\n%s", rec.Code, body)
	}
	if !strings.Contains(body, "Applicativi da completare") || !strings.Contains(body, "Rubrica") {
		t.Fatal("le app del seed senza URL devono comparire come da completare")
	}

	// Già autenticato: la pagina di login rimanda al pannello.
	if rec := do(t, s, "GET", "/admin/login", nil, c, nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("login con sessione attiva: %d", rec.Code)
	}
}

func TestSecureCookieBehindHTTPSProxy(t *testing.T) {
	s, _ := newTestServer(t, nil)
	rec := do(t, s, "POST", "/admin/login", url.Values{"username": {"mrossi"}, "password": {"pw"}}, nil,
		map[string]string{"X-Forwarded-Proto": "https"})
	if c := sessionCookie(rec); c == nil || !c.Secure {
		t.Fatalf("dietro proxy HTTPS il cookie deve essere Secure: %+v", c)
	}
}

func TestLoginNonAdminForbidden(t *testing.T) {
	s, _ := newTestServer(t, fakeAuth{ok: true, admin: false})
	rec := do(t, s, "POST", "/admin/login", url.Values{"username": {"gbianchi"}, "password": {"pw"}}, nil, nil)
	if rec.Code != http.StatusForbidden || sessionCookie(rec) != nil {
		t.Fatalf("non admin: atteso 403 senza cookie, ottenuto %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "non è amministratore") {
		t.Fatal("messaggio di accesso non autorizzato mancante")
	}
}

func TestLoginRateLimited(t *testing.T) {
	s, _ := newTestServer(t, fakeAuth{ok: false})
	form := url.Values{"username": {"mrossi"}, "password": {"sbagliata"}}
	for i := 0; i < 5; i++ {
		if rec := do(t, s, "POST", "/admin/login", form, nil, nil); rec.Code != http.StatusUnauthorized {
			t.Fatalf("tentativo %d: atteso 401, ottenuto %d", i+1, rec.Code)
		}
	}
	rec := do(t, s, "POST", "/admin/login", form, nil, nil)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("6° tentativo: atteso 429 con Retry-After, ottenuto %d", rec.Code)
	}
}

func TestLogout(t *testing.T) {
	s, _ := newTestServer(t, nil)
	c := login(t, s)
	rec := do(t, s, "POST", "/admin/logout", nil, c, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("logout: %d", rec.Code)
	}
	if out := sessionCookie(rec); out == nil || out.MaxAge >= 0 {
		t.Fatalf("logout deve scadere il cookie: %+v", out)
	}
}
```

Run: `go test ./internal/web/ -run 'Admin|Login|Logout|Secure'`
Expected: FAIL — `undefined: sessionName`

- [ ] **Step 2: Sessione** — `internal/web/session.go`

```go
package web

import (
	"crypto/sha256"
	"net/http"
	"strings"

	"github.com/gorilla/sessions"
)

const (
	sessionName   = "cruscotto_admin"
	sessionMaxAge = 8 * 60 * 60
)

// newSessionStore deriva da SESSION_SECRET una chiave di firma e una di cifratura (AES-256).
func newSessionStore(secret string) *sessions.CookieStore {
	hashKey := sha256.Sum256([]byte("auth:" + secret))
	encKey := sha256.Sum256([]byte("enc:" + secret))
	st := sessions.NewCookieStore(hashKey[:], encKey[:])
	st.Options = &sessions.Options{
		Path:     "/admin",
		MaxAge:   sessionMaxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
	st.MaxAge(sessionMaxAge)
	return st
}

// secureRequest: dietro reverse proxy decide X-Forwarded-Proto; altrimenti TLS diretto o SECURE_COOKIES.
func (s *Server) secureRequest(r *http.Request) bool {
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		return strings.EqualFold(p, "https")
	}
	return r.TLS != nil || s.cfg.SecureCookies
}

func (s *Server) sessionOptions(r *http.Request, maxAge int) *sessions.Options {
	opts := *s.store.Options
	opts.Secure = s.secureRequest(r)
	opts.MaxAge = maxAge
	return &opts
}

// currentAdmin restituisce lo username in sessione, "" se assente o non valida.
func (s *Server) currentAdmin(r *http.Request) string {
	sess, err := s.store.Get(r, sessionName)
	if err != nil {
		return ""
	}
	user, _ := sess.Values["user"].(string)
	return user
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, user string) error {
	sess, _ := s.store.New(r, sessionName) // un cookie vecchio/illeggibile viene sostituito
	sess.Values["user"] = user
	sess.Options = s.sessionOptions(r, sessionMaxAge)
	return sess.Save(r, w)
}

func (s *Server) endSession(w http.ResponseWriter, r *http.Request) {
	sess, _ := s.store.Get(r, sessionName)
	sess.Options = s.sessionOptions(r, -1)
	sess.Save(r, w)
}

// requireAdmin: senza sessione 303 al login; per HTMX 401 + HX-Redirect,
// così il login non finisce dentro un frammento della pagina.
func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.currentAdmin(r) == "" {
			if r.Header.Get("HX-Request") == "true" {
				w.Header().Set("HX-Redirect", "/admin/login")
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}
```

In `New()` (server.go), prima di `s.routes()`: `s.store = newSessionStore(o.Config.SessionSecret)`.

- [ ] **Step 3: Login/logout** — `internal/web/admin_auth.go`

```go
package web

import (
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
)

type loginView struct {
	Username string
	Error    string
	Version  string
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	if s.currentAdmin(r) != "" {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	s.render(w, http.StatusOK, "admin_login.html", loginView{Version: s.version})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	user := strings.TrimSpace(r.FormValue("username"))
	pass := r.FormValue("password")
	view := loginView{Username: user, Version: s.version}
	// Finché il sotto-progetto 4 non definisce i proxy fidati, l'IP può essere
	// quello del reverse proxy: il limite per username resta comunque efficace.
	keys := []string{"u:" + strings.ToLower(user), "ip:" + clientIP(r)}

	if wait, ok := s.limiter.Allow(keys...); !ok {
		secs := int(math.Ceil(wait.Seconds()))
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		view.Error = fmt.Sprintf("Troppi tentativi falliti. Riprova tra %d secondi.", secs)
		s.render(w, http.StatusTooManyRequests, "admin_login.html", view)
		return
	}

	ok, admin, err := s.auth.Authenticate(user, pass)
	if err != nil {
		slog.Warn("login: errore di autenticazione", "user", user, "err", err)
	}
	if err != nil || !ok {
		s.limiter.Fail(keys...)
		view.Error = "Credenziali non valide."
		if err != nil {
			view.Error = "Accesso non riuscito. Verifica le credenziali o riprova più tardi."
		}
		s.render(w, http.StatusUnauthorized, "admin_login.html", view)
		return
	}
	s.limiter.Success(keys...)

	if !admin {
		slog.Info("login: utente non amministratore", "user", user)
		view.Error = "Accesso non autorizzato: il tuo account non è amministratore di CruscottoPA."
		s.render(w, http.StatusForbidden, "admin_login.html", view)
		return
	}
	if err := s.startSession(w, r, user); err != nil {
		s.serverError(w, err)
		return
	}
	slog.Info("login amministratore", "user", user)
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.endSession(w, r)
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}
```

- [ ] **Step 4: Pagine admin** — `internal/web/admin.go`

```go
package web

import (
	"net/http"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

type adminPage struct {
	User    string
	Section string
	Version string
}

// pageView è il dato di ogni pagina admin: shell (adminPage) + contenuto (Body).
type pageView struct {
	adminPage
	Body any
}

func (s *Server) renderPage(w http.ResponseWriter, r *http.Request, page, section string, body any) {
	s.render(w, http.StatusOK, page, pageView{
		adminPage: adminPage{User: s.currentAdmin(r), Section: section, Version: s.version},
		Body:      body,
	})
}

type overviewView struct {
	ActiveAlerts []database.Alert
	Incomplete   []database.App
	Apps         int
	Guides       int
	Categories   int
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	alerts, err := s.db.ListActiveAlerts(s.now())
	if err != nil {
		s.serverError(w, err)
		return
	}
	apps, err := s.db.ListApps()
	if err != nil {
		s.serverError(w, err)
		return
	}
	guides, err := s.db.ListGuides()
	if err != nil {
		s.serverError(w, err)
		return
	}
	cats, err := s.db.ListCategories()
	if err != nil {
		s.serverError(w, err)
		return
	}
	v := overviewView{ActiveAlerts: alerts, Incomplete: []database.App{},
		Apps: len(apps), Guides: len(guides), Categories: len(cats)}
	for _, a := range apps {
		if a.URL == "" {
			v.Incomplete = append(v.Incomplete, a)
		}
	}
	s.renderPage(w, r, "admin_overview.html", "overview", v)
}
```

In `routes()` aggiungi:

```go
	s.mux.HandleFunc("GET /admin/login", s.handleLoginForm)
	s.mux.HandleFunc("POST /admin/login", s.handleLogin)
	s.mux.HandleFunc("POST /admin/logout", s.handleLogout)
	s.mux.HandleFunc("GET /admin", s.requireAdmin(s.handleOverview))
```

- [ ] **Step 5: Template** — `web/templates/admin_base.html`

```html
{{define "admin_top"}}<!doctype html>
<html lang="it">
<head>
	<meta charset="utf-8">
	<meta name="viewport" content="width=device-width, initial-scale=1">
	<meta name="htmx-config" content='{"includeIndicatorStyles":false,"responseHandling":[{"code":"204","swap":false},{"code":"[23]..","swap":true},{"code":"422","swap":true},{"code":"[45]..","swap":false,"error":true}]}'>
	<title>Amministrazione · CruscottoPA</title>
	<link rel="stylesheet" href="/static/fonts/material-icons.css">
	<link rel="stylesheet" href="/static/css/app.css">
	<link rel="stylesheet" href="/static/css/admin.css">
	<script src="/static/js/htmx.min.js" defer></script>
</head>
<body class="admin">
	<nav class="rail">
		<div class="brand">CruscottoPA<small>amministrazione</small></div>
		<a href="/admin"{{if eq .Section "overview"}} aria-current="page"{{end}}><span class="material-icons" aria-hidden="true">dashboard</span>Panoramica</a>
		<a href="/admin/avvisi"{{if eq .Section "avvisi"}} aria-current="page"{{end}}><span class="material-icons" aria-hidden="true">campaign</span>Avvisi</a>
		<a href="/admin/app"{{if eq .Section "app"}} aria-current="page"{{end}}><span class="material-icons" aria-hidden="true">apps</span>Applicativi</a>
		<a href="/admin/guide"{{if eq .Section "guide"}} aria-current="page"{{end}}><span class="material-icons" aria-hidden="true">menu_book</span>Guide</a>
		<a href="/admin/categorie"{{if eq .Section "categorie"}} aria-current="page"{{end}}><span class="material-icons" aria-hidden="true">category</span>Categorie</a>
		<div class="rail-foot">
			<a href="/" target="_blank" rel="noopener"><span class="material-icons" aria-hidden="true">open_in_new</span>Apri la plancia</a>
			<form method="post" action="/admin/logout">
				<button type="submit"><span class="material-icons" aria-hidden="true">logout</span>Esci ({{.User}})</button>
			</form>
			<small>v{{.Version}}</small>
		</div>
	</nav>
	<main class="admin-main">
{{end}}

{{define "admin_bottom"}}
	</main>
</body>
</html>{{end}}
```

`web/templates/admin_login.html`:

```html
{{define "admin_login.html"}}<!doctype html>
<html lang="it">
<head>
	<meta charset="utf-8">
	<meta name="viewport" content="width=device-width, initial-scale=1">
	<title>Accesso · CruscottoPA</title>
	<link rel="stylesheet" href="/static/css/app.css">
	<link rel="stylesheet" href="/static/css/admin.css">
</head>
<body class="login">
	<form class="login-card" method="post" action="/admin/login">
		<h1>CruscottoPA</h1>
		<p class="hint">Accesso amministratori con le credenziali di dominio.</p>
		{{with .Error}}<p class="flash error" role="alert">{{.}}</p>{{end}}
		<label>Utente<input name="username" value="{{.Username}}" autocomplete="username" required autofocus></label>
		<label>Password<input type="password" name="password" autocomplete="current-password" required></label>
		<button class="primary" type="submit">Accedi</button>
		<small>v{{.Version}}</small>
	</form>
</body>
</html>{{end}}
```

`web/templates/admin_overview.html`:

```html
{{define "admin_overview.html"}}{{template "admin_top" .}}
<h1>Panoramica</h1>
{{with .Body}}
<div class="stats">
	<a class="stat" href="/admin/app"><b>{{.Apps}}</b>applicativi</a>
	<a class="stat" href="/admin/guide"><b>{{.Guides}}</b>guide</a>
	<a class="stat" href="/admin/categorie"><b>{{.Categories}}</b>categorie</a>
	<a class="stat" href="/admin/avvisi"><b>{{len .ActiveAlerts}}</b>avvisi attivi</a>
</div>

{{if .Incomplete}}
<section class="card">
	<h2>Applicativi da completare</h2>
	<p class="hint">Non compaiono in plancia finché non hanno un indirizzo.</p>
	<ul class="plain">
		{{range .Incomplete}}
		<li>{{template "app_icon" .}}<span>{{.Title}}</span><a href="/admin/app?modifica={{.ID}}">Aggiungi indirizzo</a></li>
		{{end}}
	</ul>
</section>
{{end}}

<section class="card">
	<h2>Avvisi attivi</h2>
	{{range .ActiveAlerts}}
	<p><span class="pill pill-{{.Level}}">{{levelLabel .Level}}</span> {{.Title}}</p>
	{{else}}
	<p class="hint">Nessun avviso attivo.</p>
	{{end}}
</section>
{{end}}
{{template "admin_bottom" .}}{{end}}
```

- [ ] **Step 6: CSS admin** — `web/static/css/admin.css`

```css
/* Pannello admin: riusa variabili e componenti di app.css. */
body.admin { display: grid; grid-template-columns: 220px minmax(0, 1fr); min-height: 100vh; }
.rail { background: var(--navy); color: #c9d6ea; display: flex; flex-direction: column; padding: 1rem .75rem; gap: .15rem; position: sticky; top: 0; height: 100vh; }
.rail .brand { color: #fff; font-weight: 700; font-size: 1.05rem; padding: .25rem .5rem 1rem; }
.rail .brand small { display: block; font-weight: 400; font-size: .7rem; color: var(--navy-text); text-transform: uppercase; letter-spacing: .08em; }
.rail a, .rail button { display: flex; align-items: center; gap: .6rem; padding: .5rem; border-radius: 6px; color: inherit; text-decoration: none; font: inherit; font-size: .9rem; background: none; border: 0; cursor: pointer; width: 100%; text-align: left; }
.rail a:hover, .rail button:hover { background: var(--navy-2); color: #fff; }
.rail a[aria-current="page"] { background: var(--navy-2); color: #fff; box-shadow: inset 3px 0 0 #5b84c4; }
.rail-foot { margin-top: auto; display: flex; flex-direction: column; gap: .15rem; }
.rail-foot form { margin: 0; }
.rail-foot small { color: var(--navy-text); padding: .5rem; font-size: .7rem; }

.admin-main { padding: 1.5rem 2rem 3rem; max-width: 1100px; }
.admin-main h1 { font-size: 1.4rem; margin: 0 0 1.2rem; }
.card { background: var(--card); border: 1px solid var(--line); border-radius: 10px; padding: 1.1rem 1.25rem; margin-bottom: 1.25rem; }
.hint { color: var(--soft); font-size: .85rem; margin: .2rem 0 .8rem; }

.stats { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: .75rem; margin-bottom: 1.25rem; }
.stat { background: var(--card); border: 1px solid var(--line); border-radius: 10px; padding: .9rem 1rem; text-decoration: none; color: var(--muted); font-size: .85rem; }
.stat b { display: block; font-size: 1.6rem; color: var(--ink); }
.stat:hover { border-color: #b9c7dc; }

ul.plain { list-style: none; margin: 0; padding: 0; }
ul.plain li { display: flex; align-items: center; gap: .7rem; padding: .45rem 0; border-top: 1px solid var(--line); }
ul.plain li:first-child { border-top: 0; }
ul.plain li a { margin-left: auto; color: var(--accent); font-size: .85rem; }

/* ── Form ─────────────────────────────────────────────── */
.form { display: grid; gap: .8rem; }
.form h2 { margin: 0; }
.form .row { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: .8rem; }
label { display: grid; gap: .3rem; font-size: .85rem; font-weight: 600; color: var(--muted); }
label.inline { display: flex; align-items: center; gap: .5rem; font-weight: 400; color: var(--ink); }
input, select, textarea { font: inherit; font-weight: 400; color: var(--ink); border: 1px solid #c7d3e6; border-radius: 6px; padding: .45rem .6rem; background: #fff; }
input:focus, select:focus, textarea:focus { outline: 2px solid #9fb9e3; border-color: var(--accent); }
input[type="color"] { padding: 0; width: 3rem; height: 2rem; }
textarea { resize: vertical; min-height: 6rem; }
fieldset { border: 1px solid var(--line); border-radius: 8px; padding: .8rem; display: grid; gap: .7rem; margin: 0; }
legend { font-size: .85rem; font-weight: 600; color: var(--muted); padding: 0 .3rem; }
.field-error { color: var(--red); font-size: .8rem; margin: -.4rem 0 0; }
.actions { display: flex; gap: .5rem; align-items: center; }

button, .button { font: inherit; font-size: .88rem; border: 1px solid #c7d3e6; background: #fff; color: var(--ink); border-radius: 6px; padding: .45rem .9rem; cursor: pointer; text-decoration: none; display: inline-flex; align-items: center; gap: .3rem; }
button.primary { background: var(--accent); border-color: var(--accent); color: #fff; }
button.danger { color: var(--red); }
button.icon { padding: .3rem .45rem; }
button:hover, .button:hover { border-color: var(--accent); }

.flash { background: var(--blu-bg); color: var(--blu); border-radius: 6px; padding: .6rem .8rem; font-size: .88rem; }
.flash.error { background: var(--red-bg); color: var(--red); }

/* ── Tabelle elenco ───────────────────────────────────── */
table.list { width: 100%; border-collapse: collapse; background: var(--card); border: 1px solid var(--line); border-radius: 10px; overflow: hidden; font-size: .88rem; }
table.list th { text-align: left; font-size: .7rem; text-transform: uppercase; letter-spacing: .08em; color: var(--muted); background: #f6f8fb; padding: .55rem .75rem; }
table.list td { padding: .55rem .75rem; border-top: 1px solid var(--line); vertical-align: middle; }
table.list td.actions { justify-content: flex-end; white-space: nowrap; }
table.list .muted { color: var(--soft); }
.tag { font-size: .7rem; border-radius: 4px; padding: .1rem .4rem; background: #f1f4f8; color: var(--muted); }
.tag.warn { background: var(--amb-bg); color: var(--amb); }
table.list .app-icon { width: 28px; height: 28px; font-size: .7rem; }
table.list .app-icon .material-icons { font-size: 17px; }

/* ── Login ────────────────────────────────────────────── */
body.login { min-height: 100vh; display: grid; place-items: center; background: var(--navy); }
.login-card { background: var(--card); border-radius: 12px; padding: 2rem; width: min(360px, 92vw); display: grid; gap: .9rem; box-shadow: 0 20px 60px rgba(0, 0, 0, .3); }
.login-card h1 { margin: 0; font-size: 1.4rem; }
.login-card small { color: var(--soft); text-align: center; }

@media (max-width: 800px) {
	body.admin { grid-template-columns: minmax(0, 1fr); }
	.rail { position: static; height: auto; flex-direction: row; flex-wrap: wrap; }
	.rail-foot { margin: 0; flex-direction: row; }
	.stats { grid-template-columns: repeat(2, minmax(0, 1fr)); }
	.form .row { grid-template-columns: minmax(0, 1fr); }
	.admin-main { padding: 1rem; }
}
```

- [ ] **Step 7: Verifica e commit**

Run: `go vet ./... && go test ./internal/web/ -v`
Expected: PASS

```bash
git add internal/web web
git commit -m "feat(admin): login LDAP, sessione cifrata, shell e panoramica

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 10: Validazione form e gestione categorie

**Files:**
- Create: `internal/web/validate.go`, `internal/web/admin_categories.go`, `web/templates/admin_categorie.html`
- Modify: `internal/web/server.go` (route)
- Test: `internal/web/validate_test.go`, `internal/web/admin_categories_test.go`

**Interfaces:**
- Consumes: `requireAdmin`, `renderPage`, `render`, `login`, `do` (Task 7–9), `database.Category*` (Task 3)
- Produces:
  ```go
  type formErrors map[string]string
  func (e formErrors) add(field, msg string)          // conserva il primo errore per campo
  func checkText(e formErrors, field, value string, max int, required bool)
  func checkURL(e formErrors, field, value string, required bool)
  func checkColor(e formErrors, field, value string)
  func validURL(s string) bool
  func pathID(r *http.Request) (int64, error)          // 0,nil se la route non ha {id}
  func moveDir(r *http.Request) int                    // "up" → -1, altrimenti +1
  ```
  Convenzione per ogni sezione admin (usata anche nei Task 11–13): un template `<sezione>_section` racchiuso in `<div id="section">`; form e pulsanti fanno `hx-post` con `hx-target="#section" hx-swap="outerHTML"`; il server risponde sempre con l'intera sezione (200 se ok, 422 con errori). Route: `GET /admin/<s>` pagina, `GET /admin/<s>/{id}/modifica` sezione con form precompilato, `POST /admin/<s>` crea, `POST /admin/<s>/{id}` aggiorna, `POST /admin/<s>/{id}/elimina`, `POST /admin/<s>/{id}/sposta` (campo `dir=up|down`).

- [ ] **Step 1: Test validazione** — `internal/web/validate_test.go`

```go
package web

import "testing"

func TestValidURL(t *testing.T) {
	for in, want := range map[string]bool{
		"https://sicraweb.comune.local/login": true,
		"http://10.0.0.5:8080":                true,
		"HTTPS://Esempio.it":                  true,
		"www.comune.it":                       false, // Review Focus #1
		"comune.it/albo":                      false,
		"javascript:alert(1)":                 false,
		"ftp://files.local":                   false,
		"https://":                            false,
		"https://a b.it":                      false,
		"":                                    false,
	} {
		if got := validURL(in); got != want {
			t.Errorf("validURL(%q) = %v, atteso %v", in, got, want)
		}
	}
}

func TestCheckURLMessage(t *testing.T) {
	e := formErrors{}
	checkURL(e, "url", "www.comune.it", false)
	if e["url"] != "Inserisci l'indirizzo completo, es. https://…" {
		t.Fatalf("messaggio: %q", e["url"])
	}
	e = formErrors{}
	checkURL(e, "url", "", false)
	if len(e) != 0 {
		t.Fatal("URL facoltativo vuoto: nessun errore")
	}
}

func TestCheckTextAndColor(t *testing.T) {
	e := formErrors{}
	checkText(e, "title", "  ", 10, true)
	checkText(e, "desc", "àèìòùàèìòù", 10, false) // 10 rune, 20 byte: ok
	checkText(e, "long", "12345678901", 10, false)
	checkColor(e, "c1", "#1C4F9b")
	checkColor(e, "c2", "red")
	if e["title"] != "Campo obbligatorio." || e["long"] == "" || e["c2"] == "" {
		t.Fatalf("errori attesi mancanti: %v", e)
	}
	if _, ok := e["desc"]; ok {
		t.Fatal("la lunghezza va contata in caratteri, non in byte")
	}
	if _, ok := e["c1"]; ok {
		t.Fatal("#1C4F9b è un colore valido")
	}
}
```

Nota: `checkText` riceve valori già passati da `strings.TrimSpace` negli handler; il test usa `"  "` per verificare che anche il solo spazio sia "obbligatorio" (il controllo usa `strings.TrimSpace`).

- [ ] **Step 2: Test categorie** — `internal/web/admin_categories_test.go`

```go
package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

var hx = map[string]string{"HX-Request": "true"}

func TestCategoriesPageAndCreate(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)

	rec := do(t, s, "GET", "/admin/categorie", nil, c, nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `id="section"`) || !strings.Contains(rec.Body.String(), "Applicativi") {
		t.Fatalf("pagina categorie: %d\n%s", rec.Code, rec.Body)
	}

	rec = do(t, s, "POST", "/admin/categorie", url.Values{"name": {"  Gestionali esterni "}}, c, hx)
	if rec.Code != 200 || strings.Contains(rec.Body.String(), "<html") || !strings.Contains(rec.Body.String(), "Gestionali esterni") {
		t.Fatalf("create: atteso frammento con la nuova categoria, %d\n%s", rec.Code, rec.Body)
	}
	cats, _ := db.ListCategories()
	if len(cats) != 2 || cats[1].Name != "Gestionali esterni" {
		t.Fatalf("DB: %+v", cats)
	}
}

func TestCategoryValidationAndDuplicates(t *testing.T) {
	s, _ := newTestServer(t, nil)
	c := login(t, s)

	rec := do(t, s, "POST", "/admin/categorie", url.Values{"name": {"   "}}, c, hx)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Campo obbligatorio.") {
		t.Fatalf("nome vuoto: %d\n%s", rec.Code, rec.Body)
	}
	rec = do(t, s, "POST", "/admin/categorie", url.Values{"name": {"applicativi"}}, c, hx)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Esiste già") {
		t.Fatalf("duplicato: %d\n%s", rec.Code, rec.Body)
	}
}

func TestCategoryEditUpdateMoveDelete(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	id, _ := db.CreateCategory("Esterni")
	path := "/admin/categorie/" + itoa(id)

	rec := do(t, s, "GET", path+"/modifica", nil, c, hx)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `value="Esterni"`) || !strings.Contains(rec.Body.String(), `hx-post="`+path+`"`) {
		t.Fatalf("modifica: %d\n%s", rec.Code, rec.Body)
	}
	if rec := do(t, s, "POST", path, url.Values{"name": {"Gestionali esterni"}}, c, hx); rec.Code != 200 {
		t.Fatalf("update: %d", rec.Code)
	}
	if rec := do(t, s, "POST", path+"/sposta", url.Values{"dir": {"up"}}, c, hx); rec.Code != 200 {
		t.Fatalf("sposta: %d", rec.Code)
	}
	cats, _ := db.ListCategories()
	if cats[0].Name != "Gestionali esterni" {
		t.Fatalf("dopo update+sposta: %+v", cats)
	}
	if rec := do(t, s, "POST", path+"/elimina", nil, c, hx); rec.Code != 200 {
		t.Fatalf("elimina: %d", rec.Code)
	}
	if rec := do(t, s, "POST", "/admin/categorie/999/elimina", nil, c, hx); rec.Code != http.StatusNotFound {
		t.Fatalf("elimina inesistente: %d", rec.Code)
	}
	if rec := do(t, s, "GET", "/admin/categorie/abc/modifica", nil, c, hx); rec.Code != http.StatusNotFound {
		t.Fatalf("id non numerico: %d", rec.Code)
	}
}

func TestDeleteCategoryWithAppsShowsMessage(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	cats, _ := db.ListCategories()
	rec := do(t, s, "POST", "/admin/categorie/"+itoa(cats[0].ID)+"/elimina", nil, c, hx)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Sposta o elimina prima le app") {
		t.Fatalf("categoria con app: %d\n%s", rec.Code, rec.Body)
	}
}

func TestCategoriesRequireLogin(t *testing.T) {
	s, _ := newTestServer(t, nil)
	if rec := do(t, s, "POST", "/admin/categorie", url.Values{"name": {"x"}}, nil, hx); rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST senza sessione: %d", rec.Code)
	}
}
```

Run: `go test ./internal/web/ -run 'Valid|Check|Categor'`
Expected: FAIL — `undefined: validURL`…

- [ ] **Step 3: Implementa** — `internal/web/validate.go`

```go
package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// formErrors: campo → messaggio. "general" per errori non legati a un campo.
type formErrors map[string]string

func (e formErrors) add(field, msg string) {
	if _, ok := e[field]; !ok {
		e[field] = msg
	}
}

func checkText(e formErrors, field, value string, max int, required bool) {
	switch {
	case required && strings.TrimSpace(value) == "":
		e.add(field, "Campo obbligatorio.")
	case utf8.RuneCountInString(value) > max:
		e.add(field, fmt.Sprintf("Massimo %d caratteri.", max))
	}
}

func checkURL(e formErrors, field, value string, required bool) {
	if value == "" {
		if required {
			e.add(field, "Campo obbligatorio.")
		}
		return
	}
	if !validURL(value) {
		e.add(field, "Inserisci l'indirizzo completo, es. https://…")
	}
}

// validURL accetta solo URL assoluti http/https con host, senza spazi.
func validURL(s string) bool {
	if s == "" || len(s) > 2000 || strings.ContainsAny(s, " \t\r\n") {
		return false
	}
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

var colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func checkColor(e formErrors, field, value string) {
	if !colorRe.MatchString(value) {
		e.add(field, "Colore non valido (formato #rrggbb).")
	}
}

var errBadID = errors.New("id non valido")

// pathID legge {id} dalla route; 0,nil se la route non lo prevede.
func pathID(r *http.Request) (int64, error) {
	v := r.PathValue("id")
	if v == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(v, 10, 64)
	if err != nil || id <= 0 {
		return 0, errBadID
	}
	return id, nil
}

func moveDir(r *http.Request) int {
	if r.FormValue("dir") == "up" {
		return -1
	}
	return 1
}
```

`internal/web/admin_categories.go`:

```go
package web

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

type categoryForm struct {
	ID   int64
	Name string
}

type categoriesSection struct {
	Categories []database.Category
	Form       categoryForm
	Errors     formErrors
}

func (s *Server) categoriesData(form categoryForm, errs formErrors) (categoriesSection, error) {
	cats, err := s.db.ListCategories()
	return categoriesSection{Categories: cats, Form: form, Errors: errs}, err
}

func (s *Server) renderCategories(w http.ResponseWriter, status int, form categoryForm, errs formErrors) {
	sec, err := s.categoriesData(form, errs)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.render(w, status, "categories_section", sec)
}

func (s *Server) handleCategoriesPage(w http.ResponseWriter, r *http.Request) {
	sec, err := s.categoriesData(categoryForm{}, nil)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderPage(w, r, "admin_categorie.html", "categorie", sec)
}

func (s *Server) handleCategoryEdit(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	c, err := s.db.GetCategory(id)
	if errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderCategories(w, http.StatusOK, categoryForm{ID: c.ID, Name: c.Name}, nil)
}

func (s *Server) handleCategorySave(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	form := categoryForm{ID: id, Name: strings.TrimSpace(r.FormValue("name"))}
	errs := formErrors{}
	checkText(errs, "name", form.Name, 60, true)
	if len(errs) > 0 {
		s.renderCategories(w, http.StatusUnprocessableEntity, form, errs)
		return
	}
	if id == 0 {
		_, err = s.db.CreateCategory(form.Name)
	} else {
		err = s.db.UpdateCategory(id, form.Name)
	}
	switch {
	case errors.Is(err, database.ErrDuplicate):
		errs.add("name", "Esiste già una categoria con questo nome.")
		s.renderCategories(w, http.StatusUnprocessableEntity, form, errs)
	case errors.Is(err, database.ErrNotFound):
		http.NotFound(w, r)
	case err != nil:
		s.serverError(w, err)
	default:
		s.renderCategories(w, http.StatusOK, categoryForm{}, nil)
	}
}

func (s *Server) handleCategoryDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	err = s.db.DeleteCategory(id)
	switch {
	case errors.Is(err, database.ErrCategoryNotEmpty):
		s.renderCategories(w, http.StatusUnprocessableEntity, categoryForm{},
			formErrors{"general": "Sposta o elimina prima le app di questa categoria."})
	case errors.Is(err, database.ErrNotFound):
		http.NotFound(w, r)
	case err != nil:
		s.serverError(w, err)
	default:
		s.renderCategories(w, http.StatusOK, categoryForm{}, nil)
	}
}

func (s *Server) handleCategoryMove(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.db.MoveCategory(id, moveDir(r)); errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderCategories(w, http.StatusOK, categoryForm{}, nil)
}
```

In `routes()` aggiungi:

```go
	s.mux.HandleFunc("GET /admin/categorie", s.requireAdmin(s.handleCategoriesPage))
	s.mux.HandleFunc("GET /admin/categorie/{id}/modifica", s.requireAdmin(s.handleCategoryEdit))
	s.mux.HandleFunc("POST /admin/categorie", s.requireAdmin(s.handleCategorySave))
	s.mux.HandleFunc("POST /admin/categorie/{id}", s.requireAdmin(s.handleCategorySave))
	s.mux.HandleFunc("POST /admin/categorie/{id}/elimina", s.requireAdmin(s.handleCategoryDelete))
	s.mux.HandleFunc("POST /admin/categorie/{id}/sposta", s.requireAdmin(s.handleCategoryMove))
```

- [ ] **Step 4: Template** — `web/templates/admin_categorie.html`

```html
{{define "admin_categorie.html"}}{{template "admin_top" .}}
<h1>Categorie</h1>
{{template "categories_section" .Body}}
{{template "admin_bottom" .}}{{end}}

{{define "categories_section"}}
<div id="section">
	{{with .Errors.general}}<p class="flash error" role="alert">{{.}}</p>{{end}}

	<form class="card form" hx-post="{{if .Form.ID}}/admin/categorie/{{.Form.ID}}{{else}}/admin/categorie{{end}}" hx-target="#section" hx-swap="outerHTML">
		<h2>{{if .Form.ID}}Modifica categoria{{else}}Nuova categoria{{end}}</h2>
		<label>Nome<input name="name" value="{{.Form.Name}}" maxlength="60" required></label>
		{{with .Errors.name}}<p class="field-error">{{.}}</p>{{end}}
		<div class="actions">
			<button class="primary" type="submit">Salva</button>
			{{if .Form.ID}}<a class="button" href="/admin/categorie">Annulla</a>{{end}}
		</div>
	</form>

	<table class="list">
		<thead><tr><th>Nome</th><th></th></tr></thead>
		<tbody>
			{{range .Categories}}
			<tr>
				<td>{{.Name}}</td>
				<td class="actions">
					<button class="icon" type="button" title="Su" hx-post="/admin/categorie/{{.ID}}/sposta" hx-vals='{"dir":"up"}' hx-target="#section" hx-swap="outerHTML"><span class="material-icons" aria-hidden="true">arrow_upward</span></button>
					<button class="icon" type="button" title="Giù" hx-post="/admin/categorie/{{.ID}}/sposta" hx-vals='{"dir":"down"}' hx-target="#section" hx-swap="outerHTML"><span class="material-icons" aria-hidden="true">arrow_downward</span></button>
					<button type="button" hx-get="/admin/categorie/{{.ID}}/modifica" hx-target="#section" hx-swap="outerHTML">Modifica</button>
					<button class="danger" type="button" hx-post="/admin/categorie/{{.ID}}/elimina" hx-confirm="Eliminare la categoria «{{.Name}}»?" hx-target="#section" hx-swap="outerHTML">Elimina</button>
				</td>
			</tr>
			{{else}}
			<tr><td colspan="2" class="muted">Nessuna categoria.</td></tr>
			{{end}}
		</tbody>
	</table>
</div>
{{end}}
```

- [ ] **Step 5: Verifica e commit**

Run: `go vet ./... && go test ./internal/web/ -v`
Expected: PASS

```bash
git add internal/web web/templates
git commit -m "feat(admin): validazione form e gestione categorie

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 11: Gestione applicativi, icone caricate e picker

**Files:**
- Create: `internal/web/uploads.go`, `internal/web/admin_apps.go`, `web/templates/admin_app.html`, `web/static/js/admin.js`
- Modify: `internal/web/server.go` (route), `web/templates/admin_base.html` (script admin.js), `web/static/css/admin.css` (pannelli icona)
- Test: `internal/web/uploads_test.go`, `internal/web/admin_apps_test.go`

**Interfaces:**
- Consumes: convenzione sezioni admin, `formErrors`, `checkText`, `checkURL`, `checkColor`, `validURL`, `pathID`, `moveDir` (Task 10), `icons.Valid`, `icons.Search` (Task 5), `database.App*`, `database.GuideCountsByApp` (Task 3–4), `app_icon` (Task 8)
- Produces:
  ```go
  const maxIconBytes = 512 << 10
  var errIconType, errIconSize error
  func (s *Server) iconDir() string
  func detectIconExt(data []byte) (string, error) // "png" | "webp" | "svg"
  func (s *Server) saveIcon(r io.Reader) (string, error)
  func (s *Server) removeIcon(name string)
  // route: GET /uploads/icons/{file}, GET /admin/icone?q=, /admin/app[...]
  // GET /admin/app?modifica={id} apre la pagina con il form precompilato (link dalla panoramica)
  ```

- [ ] **Step 1: Test upload** — `internal/web/uploads_test.go`

```go
package web

import (
	"bytes"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var (
	pngBytes  = append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)
	svgScript = []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
)

func TestDetectIconExt(t *testing.T) {
	for _, tc := range []struct {
		data []byte
		ext  string
		err  error
	}{
		{pngBytes, "png", nil},
		{append([]byte("RIFF\x00\x00\x00\x00WEBPVP8 "), make([]byte, 32)...), "webp", nil},
		{svgScript, "svg", nil},
		{[]byte(`<!DOCTYPE svg><svg></svg>`), "svg", nil},
		{[]byte(`<html><body>ciao</body></html>`), "", errIconType},
		{[]byte("GIF89a......"), "", errIconType},
		{[]byte("testo qualsiasi"), "", errIconType},
	} {
		ext, err := detectIconExt(tc.data)
		if ext != tc.ext || !errors.Is(err, tc.err) {
			t.Errorf("detectIconExt(%.20q) = %q, %v; atteso %q, %v", tc.data, ext, err, tc.ext, tc.err)
		}
	}
}

func TestSaveIconLimits(t *testing.T) {
	s, _ := newTestServer(t, nil)
	if _, err := s.saveIcon(bytes.NewReader(make([]byte, maxIconBytes+1))); !errors.Is(err, errIconSize) {
		t.Fatalf("file > 512 KB: atteso errIconSize, ottenuto %v", err)
	}
	name, err := s.saveIcon(bytes.NewReader(pngBytes))
	if err != nil || !iconFileRe.MatchString(name) || !strings.HasSuffix(name, ".png") {
		t.Fatalf("saveIcon: %q %v", name, err)
	}
	if _, err := os.Stat(filepath.Join(s.iconDir(), name)); err != nil {
		t.Fatal("file non scritto")
	}
	s.removeIcon(name)
	if _, err := os.Stat(filepath.Join(s.iconDir(), name)); !os.IsNotExist(err) {
		t.Fatal("removeIcon non ha cancellato il file")
	}
	s.removeIcon("../../etc/passwd") // nome non conforme: ignorato senza panic
}

func TestServeIconSandboxed(t *testing.T) {
	s, _ := newTestServer(t, nil)
	name, _ := s.saveIcon(bytes.NewReader(svgScript))

	rec := do(t, s, "GET", "/uploads/icons/"+name, nil, nil, nil)
	if rec.Code != 200 {
		t.Fatalf("icona: %d", rec.Code)
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.HasPrefix(csp, "sandbox") || !strings.Contains(csp, "default-src 'none'") {
		t.Fatalf("SVG servito senza sandbox: %q", csp)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/svg+xml" {
		t.Fatalf("Content-Type: %q", ct)
	}
	for _, bad := range []string{"/uploads/icons/abc.png", "/uploads/icons/" + strings.Repeat("a", 32) + ".html", "/uploads/icons/..%2f..%2fgo.mod"} {
		if rec := do(t, s, "GET", bad, nil, nil, nil); rec.Code != http.StatusNotFound {
			t.Errorf("%s: atteso 404, ottenuto %d", bad, rec.Code)
		}
	}
}
```

- [ ] **Step 2: Test gestione app** — `internal/web/admin_apps_test.go`

```go
package web

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

func postMultipart(t *testing.T, s *Server, target string, fields map[string]string, file []byte, c *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		mw.WriteField(k, v)
	}
	if file != nil {
		fw, _ := mw.CreateFormFile("icon_file", "logo.bin")
		fw.Write(file)
	}
	mw.Close()
	req := httptest.NewRequest("POST", target, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("HX-Request", "true")
	req.AddCookie(c)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func appFields(db *database.DB, overrides map[string]string) map[string]string {
	cats, _ := db.ListCategories()
	f := map[string]string{
		"title": "Sicraweb", "description": "Protocollo e atti", "url": "https://sicraweb.local",
		"category_id": itoa(cats[0].ID), "icon_kind": "pack", "icon_value_pack": "description",
		"icon_color": "#475569", "enabled": "1",
	}
	for k, v := range overrides {
		f[k] = v
	}
	return f
}

func TestCreateAppWithPackIcon(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	rec := postMultipart(t, s, "/admin/app", appFields(db, nil), nil, c)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Sicraweb") {
		t.Fatalf("create: %d\n%s", rec.Code, rec.Body)
	}
	apps, _ := db.ListApps()
	a := apps[len(apps)-1]
	if a.Title != "Sicraweb" || a.IconKind != "pack" || a.IconValue != "description" || !a.Enabled {
		t.Fatalf("app salvata: %+v", a)
	}
}

func TestCreateAppValidation(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	rec := postMultipart(t, s, "/admin/app", appFields(db, map[string]string{
		"title": "", "url": "www.comune.it", "icon_value_pack": "non_esiste_xyz", "category_id": "999",
	}), nil, c)
	body := rec.Body.String()
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("atteso 422, ottenuto %d", rec.Code)
	}
	for _, want := range []string{"Campo obbligatorio.", "Inserisci l&#39;indirizzo completo", "Scegli un&#39;icona dal catalogo.", "Scegli una categoria."} {
		if !strings.Contains(body, want) {
			t.Errorf("manca il messaggio %q", want)
		}
	}
	if !strings.Contains(body, `value="www.comune.it"`) {
		t.Error("il form deve conservare i valori inseriti")
	}
}

func TestAppUploadLifecycle(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	apps, _ := db.ListApps()
	id := apps[0].ID
	path := "/admin/app/" + itoa(id)

	// 1) upload senza file → errore
	rec := postMultipart(t, s, path, appFields(db, map[string]string{"icon_kind": "upload"}), nil, c)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Carica un file") {
		t.Fatalf("upload senza file: %d", rec.Code)
	}
	// 2) tipo non ammesso
	rec = postMultipart(t, s, path, appFields(db, map[string]string{"icon_kind": "upload"}), []byte("ciao"), c)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Formato non ammesso") {
		t.Fatalf("tipo non ammesso: %d", rec.Code)
	}
	// 3) troppo grande
	big := append(append([]byte{}, pngBytes...), make([]byte, maxIconBytes)...)
	rec = postMultipart(t, s, path, appFields(db, map[string]string{"icon_kind": "upload"}), big, c)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "troppo grande") {
		t.Fatalf("file grande: %d", rec.Code)
	}
	// 4) PNG valido
	rec = postMultipart(t, s, path, appFields(db, map[string]string{"icon_kind": "upload"}), pngBytes, c)
	if rec.Code != 200 {
		t.Fatalf("upload PNG: %d\n%s", rec.Code, rec.Body)
	}
	a, _ := db.GetApp(id)
	file := filepath.Join(s.iconDir(), a.IconValue)
	if a.IconKind != "upload" || !iconFileRe.MatchString(a.IconValue) {
		t.Fatalf("icona caricata: %+v", a)
	}
	// 5) salvataggio senza nuovo file → mantiene l'icona
	postMultipart(t, s, path, appFields(db, map[string]string{"icon_kind": "upload", "title": "Rubrica 2"}), nil, c)
	if b, _ := db.GetApp(id); b.IconValue != a.IconValue {
		t.Fatal("senza nuovo file l'icona caricata va mantenuta")
	}
	// 6) passaggio a icona del pacchetto → file cancellato
	postMultipart(t, s, path, appFields(db, nil), nil, c)
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("il file della vecchia icona va cancellato")
	}
}

func TestDeleteAppWarnsAboutGuides(t *testing.T) { // Review Focus #4
	s, db := newTestServer(t, nil)
	c := login(t, s)
	apps, _ := db.ListApps()
	rubrica := apps[0]
	rubrica.URL = "https://rubrica.local"
	db.UpdateApp(rubrica)
	db.CreateGuide(database.Guide{AppID: id64(rubrica.ID), Title: "Cercare un interno", Kind: "link", URL: "https://wiki/x", Enabled: true})

	page := do(t, s, "GET", "/admin/app", nil, c, nil).Body.String()
	if !strings.Contains(page, "La sua guida diventerà generale.") {
		t.Fatalf("la conferma di eliminazione deve avvisare delle guide collegate\n%s", page)
	}
	if rec := do(t, s, "POST", "/admin/app/"+itoa(rubrica.ID)+"/elimina", nil, c, hx); rec.Code != 200 {
		t.Fatalf("elimina: %d", rec.Code)
	}
	dash := do(t, s, "GET", "/", nil, nil, nil).Body.String()
	if !strings.Contains(dash, `class="guides"`) || !strings.Contains(dash, "Cercare un interno") {
		t.Fatal("dopo l'eliminazione la guida deve comparire tra le generali")
	}
}

func TestAppEditMoveAndIconSearch(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	apps, _ := db.ListApps()
	webmail := apps[1]

	rec := do(t, s, "GET", "/admin/app?modifica="+itoa(webmail.ID), nil, c, nil)
	if !strings.Contains(rec.Body.String(), `value="Webmail"`) {
		t.Fatal("?modifica= deve aprire il form precompilato")
	}
	if rec := do(t, s, "GET", "/admin/app/"+itoa(webmail.ID)+"/modifica", nil, c, hx); !strings.Contains(rec.Body.String(), `hx-post="/admin/app/`+itoa(webmail.ID)+`"`) {
		t.Fatal("modifica: form non puntato all'app")
	}
	do(t, s, "POST", "/admin/app/"+itoa(webmail.ID)+"/sposta", url_("dir", "up"), c, hx)
	if apps, _ := db.ListApps(); apps[0].ID != webmail.ID {
		t.Fatal("sposta su non applicato")
	}

	rec = do(t, s, "GET", "/admin/icone?q=mail", nil, c, hx)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `data-icon="mail"`) {
		t.Fatalf("ricerca icone: %d\n%s", rec.Code, rec.Body)
	}
}
```

Aggiungi in `server_test.go`:

```go
func url_(k, v string) url.Values { return url.Values{k: {v}} }
```

Run: `go test ./internal/web/ -run 'Icon|App'`
Expected: FAIL — `undefined: detectIconExt`…

- [ ] **Step 3: Implementa** — `internal/web/uploads.go`

```go
package web

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
)

const maxIconBytes = 512 << 10

var (
	errIconType = errors.New("Formato non ammesso: usa PNG, WebP o SVG.")
	errIconSize = errors.New("File troppo grande (massimo 512 KB).")
	// Nome generato da saveIcon: 16 byte casuali in hex + estensione.
	iconFileRe = regexp.MustCompile(`^[0-9a-f]{32}\.(png|webp|svg)$`)
)

func (s *Server) iconDir() string { return filepath.Join(s.cfg.UploadDir, "icons") }

// detectIconExt riconosce il tipo dal contenuto, mai dall'estensione dichiarata.
func detectIconExt(data []byte) (string, error) {
	switch http.DetectContentType(data) {
	case "image/png":
		return "png", nil
	case "image/webp":
		return "webp", nil
	}
	if isSVG(data) {
		return "svg", nil
	}
	return "", errIconType
}

// isSVG: documento XML il cui primo elemento è <svg>.
func isSVG(data []byte) bool {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			return false
		}
		if se, ok := tok.(xml.StartElement); ok {
			return se.Name.Local == "svg"
		}
	}
}

func (s *Server) saveIcon(r io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxIconBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxIconBytes {
		return "", errIconSize
	}
	ext, err := detectIconExt(data)
	if err != nil {
		return "", err
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	name := hex.EncodeToString(b) + "." + ext
	if err := os.MkdirAll(s.iconDir(), 0o750); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(s.iconDir(), name), data, 0o640); err != nil {
		return "", err
	}
	return name, nil
}

// removeIcon cancella un file caricato; ignora nomi non generati da saveIcon.
func (s *Server) removeIcon(name string) {
	if !iconFileRe.MatchString(name) {
		return
	}
	if err := os.Remove(filepath.Join(s.iconDir(), name)); err != nil && !os.IsNotExist(err) {
		slog.Warn("rimozione icona", "file", name, "err", err)
	}
}

// handleIconFile serve le icone caricate in sandbox: uno script dentro un SVG
// non viene eseguito nemmeno aprendo direttamente l'URL del file.
func (s *Server) handleIconFile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	if !iconFileRe.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'; style-src 'unsafe-inline'")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, filepath.Join(s.iconDir(), name))
}
```

`internal/web/admin_apps.go`:

```go
package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/icons"
)

const defaultIconColor = "#475569"

type appForm struct {
	ID          int64
	CategoryID  int64
	Title       string
	Description string
	URL         string
	IconKind    string
	IconValue   string
	IconColor   string
	Enabled     bool
}

type appRow struct {
	database.App
	CategoryName string
	GuideCount   int
}

type appsSection struct {
	Apps       []appRow
	Categories []database.Category
	Form       appForm
	Errors     formErrors
}

func newAppForm() appForm {
	return appForm{IconKind: database.IconPack, IconColor: defaultIconColor, Enabled: true}
}

func formFromApp(a database.App) appForm {
	return appForm{ID: a.ID, CategoryID: a.CategoryID, Title: a.Title, Description: a.Description,
		URL: a.URL, IconKind: a.IconKind, IconValue: a.IconValue, IconColor: a.IconColor, Enabled: a.Enabled}
}

func (s *Server) appsData(form appForm, errs formErrors) (appsSection, error) {
	apps, err := s.db.ListApps()
	if err != nil {
		return appsSection{}, err
	}
	cats, err := s.db.ListCategories()
	if err != nil {
		return appsSection{}, err
	}
	counts, err := s.db.GuideCountsByApp()
	if err != nil {
		return appsSection{}, err
	}
	catNames := map[int64]string{}
	for _, c := range cats {
		catNames[c.ID] = c.Name
	}
	rows := make([]appRow, 0, len(apps))
	for _, a := range apps {
		rows = append(rows, appRow{App: a, CategoryName: catNames[a.CategoryID], GuideCount: counts[a.ID]})
	}
	if form.CategoryID == 0 && len(cats) > 0 {
		form.CategoryID = cats[0].ID
	}
	return appsSection{Apps: rows, Categories: cats, Form: form, Errors: errs}, nil
}

func (s *Server) renderApps(w http.ResponseWriter, status int, form appForm, errs formErrors) {
	sec, err := s.appsData(form, errs)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.render(w, status, "apps_section", sec)
}

func (s *Server) handleAppsPage(w http.ResponseWriter, r *http.Request) {
	form := newAppForm()
	if v := r.URL.Query().Get("modifica"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			if a, err := s.db.GetApp(id); err == nil {
				form = formFromApp(a)
			}
		}
	}
	sec, err := s.appsData(form, nil)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderPage(w, r, "admin_app.html", "app", sec)
}

func (s *Server) handleAppEdit(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a, err := s.db.GetApp(id)
	if errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderApps(w, http.StatusOK, formFromApp(a), nil)
}

func (s *Server) handleAppSave(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var current database.App
	if id != 0 {
		if current, err = s.db.GetApp(id); errors.Is(err, database.ErrNotFound) {
			http.NotFound(w, r)
			return
		} else if err != nil {
			s.serverError(w, err)
			return
		}
	}

	// Limite sul corpo intero: icona (512 KB) + margine per gli altri campi.
	r.Body = http.MaxBytesReader(w, r.Body, maxIconBytes+64<<10)
	if err := r.ParseMultipartForm(maxIconBytes + 64<<10); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			form := newAppForm()
			if id != 0 {
				form = formFromApp(current)
			}
			s.renderApps(w, http.StatusUnprocessableEntity, form, formErrors{"icon": errIconSize.Error()})
			return
		}
		http.Error(w, "Richiesta non valida", http.StatusBadRequest)
		return
	}

	form := appForm{
		ID:          id,
		Title:       strings.TrimSpace(r.FormValue("title")),
		Description: strings.TrimSpace(r.FormValue("description")),
		URL:         strings.TrimSpace(r.FormValue("url")),
		IconKind:    r.FormValue("icon_kind"),
		IconColor:   strings.TrimSpace(r.FormValue("icon_color")),
		Enabled:     r.FormValue("enabled") == "1",
	}
	form.CategoryID, _ = strconv.ParseInt(r.FormValue("category_id"), 10, 64)
	if form.IconColor == "" {
		form.IconColor = defaultIconColor
	}

	errs := formErrors{}
	checkText(errs, "title", form.Title, 120, true)
	checkText(errs, "description", form.Description, 200, false)
	checkURL(errs, "url", form.URL, false)
	checkColor(errs, "icon", form.IconColor)
	if _, err := s.db.GetCategory(form.CategoryID); errors.Is(err, database.ErrNotFound) {
		errs.add("category", "Scegli una categoria.")
	} else if err != nil {
		s.serverError(w, err)
		return
	}

	switch form.IconKind {
	case database.IconMonogram:
		form.IconValue = ""
	case database.IconPack:
		form.IconValue = strings.TrimSpace(r.FormValue("icon_value_pack"))
		if !icons.Valid(form.IconValue) {
			errs.add("icon", "Scegli un'icona dal catalogo.")
		}
	case database.IconURL:
		form.IconValue = strings.TrimSpace(r.FormValue("icon_value_url"))
		if !validURL(form.IconValue) {
			errs.add("icon", "Indirizzo dell'icona non valido (https://…).")
		}
	case database.IconUpload:
		if current.IconKind == database.IconUpload {
			form.IconValue = current.IconValue // mantenuta se non arriva un nuovo file
		}
	default:
		errs.add("icon", "Tipo di icona non valido.")
	}

	// Il file si salva solo se il resto del form è valido: niente file orfani.
	var uploaded string
	if form.IconKind == database.IconUpload && len(errs) == 0 {
		file, _, err := r.FormFile("icon_file")
		switch {
		case err == nil:
			name, saveErr := s.saveIcon(file)
			file.Close()
			switch {
			case errors.Is(saveErr, errIconType), errors.Is(saveErr, errIconSize):
				errs.add("icon", saveErr.Error())
			case saveErr != nil:
				s.serverError(w, saveErr)
				return
			default:
				uploaded, form.IconValue = name, name
			}
		case errors.Is(err, http.ErrMissingFile), errors.Is(err, http.ErrNotMultipart):
			if form.IconValue == "" {
				errs.add("icon", "Carica un file per l'icona.")
			}
		default:
			s.serverError(w, err)
			return
		}
	}

	if len(errs) > 0 {
		s.renderApps(w, http.StatusUnprocessableEntity, form, errs)
		return
	}

	a := database.App{ID: id, CategoryID: form.CategoryID, Title: form.Title, Description: form.Description,
		URL: form.URL, IconKind: form.IconKind, IconValue: form.IconValue, IconColor: form.IconColor, Enabled: form.Enabled}
	if id == 0 {
		_, err = s.db.CreateApp(a)
	} else {
		err = s.db.UpdateApp(a)
	}
	if err != nil {
		s.removeIcon(uploaded)
		s.serverError(w, err)
		return
	}
	if current.IconKind == database.IconUpload && current.IconValue != a.IconValue {
		s.removeIcon(current.IconValue)
	}
	s.renderApps(w, http.StatusOK, newAppForm(), nil)
}

func (s *Server) handleAppDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a, err := s.db.GetApp(id)
	if errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	if err := s.db.DeleteApp(id); err != nil {
		s.serverError(w, err)
		return
	}
	if a.IconKind == database.IconUpload {
		s.removeIcon(a.IconValue)
	}
	s.renderApps(w, http.StatusOK, newAppForm(), nil)
}

func (s *Server) handleAppMove(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.db.MoveApp(id, moveDir(r)); errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderApps(w, http.StatusOK, newAppForm(), nil)
}

func (s *Server) handleIconSearch(w http.ResponseWriter, r *http.Request) {
	s.render(w, http.StatusOK, "icon_results", icons.Search(r.URL.Query().Get("q"), 60))
}
```

In `routes()` aggiungi:

```go
	s.mux.HandleFunc("GET /uploads/icons/{file}", s.handleIconFile)
	s.mux.HandleFunc("GET /admin/icone", s.requireAdmin(s.handleIconSearch))
	s.mux.HandleFunc("GET /admin/app", s.requireAdmin(s.handleAppsPage))
	s.mux.HandleFunc("GET /admin/app/{id}/modifica", s.requireAdmin(s.handleAppEdit))
	s.mux.HandleFunc("POST /admin/app", s.requireAdmin(s.handleAppSave))
	s.mux.HandleFunc("POST /admin/app/{id}", s.requireAdmin(s.handleAppSave))
	s.mux.HandleFunc("POST /admin/app/{id}/elimina", s.requireAdmin(s.handleAppDelete))
	s.mux.HandleFunc("POST /admin/app/{id}/sposta", s.requireAdmin(s.handleAppMove))
```

- [ ] **Step 4: Template** — `web/templates/admin_app.html`

```html
{{define "admin_app.html"}}{{template "admin_top" .}}
<h1>Applicativi</h1>
{{template "apps_section" .Body}}
{{template "admin_bottom" .}}{{end}}

{{define "icon_results"}}{{range .}}<button type="button" class="icon-choice" data-icon="{{.}}" title="{{.}}"><span class="material-icons" aria-hidden="true">{{.}}</span></button>{{else}}<p class="hint">Nessuna icona trovata.</p>{{end}}{{end}}

{{define "apps_section"}}
<div id="section">
	{{with .Errors.general}}<p class="flash error" role="alert">{{.}}</p>{{end}}
	{{if not .Categories}}
	<p class="flash">Crea prima una <a href="/admin/categorie">categoria</a>.</p>
	{{else}}
	<form class="card form" hx-post="{{if .Form.ID}}/admin/app/{{.Form.ID}}{{else}}/admin/app{{end}}" hx-encoding="multipart/form-data" hx-target="#section" hx-swap="outerHTML">
		<h2>{{if .Form.ID}}Modifica applicativo{{else}}Nuovo applicativo{{end}}</h2>
		<div class="row">
			<label>Titolo<input name="title" value="{{.Form.Title}}" maxlength="120" required></label>
			<label>Categoria
				<select name="category_id">
					{{$sel := .Form.CategoryID}}
					{{range .Categories}}<option value="{{.ID}}"{{if eq .ID $sel}} selected{{end}}>{{.Name}}</option>{{end}}
				</select>
			</label>
		</div>
		{{with .Errors.title}}<p class="field-error">{{.}}</p>{{end}}
		{{with .Errors.category}}<p class="field-error">{{.}}</p>{{end}}
		<label>Descrizione breve<input name="description" value="{{.Form.Description}}" maxlength="200"></label>
		{{with .Errors.description}}<p class="field-error">{{.}}</p>{{end}}
		<label>Indirizzo (URL)<input name="url" value="{{.Form.URL}}" placeholder="https://…"></label>
		{{with .Errors.url}}<p class="field-error">{{.}}</p>{{else}}<p class="hint">Senza indirizzo l'applicativo non compare in plancia.</p>{{end}}

		<fieldset class="icon-field">
			<legend>Icona</legend>
			<div class="icon-tabs">
				<label class="inline"><input type="radio" name="icon_kind" value="pack" id="ik-pack"{{if eq .Form.IconKind "pack"}} checked{{end}}>Pacchetto</label>
				<label class="inline"><input type="radio" name="icon_kind" value="upload" id="ik-upload"{{if eq .Form.IconKind "upload"}} checked{{end}}>Carica file</label>
				<label class="inline"><input type="radio" name="icon_kind" value="url" id="ik-url"{{if eq .Form.IconKind "url"}} checked{{end}}>Da URL</label>
				<label class="inline"><input type="radio" name="icon_kind" value="" id="ik-none"{{if eq .Form.IconKind ""}} checked{{end}}>Iniziali</label>
			</div>
			<div class="icon-panel" data-kind="pack">
				<div class="icon-pick">
					<span class="app-icon" data-preview style="background:{{.Form.IconColor}}"><span class="material-icons" aria-hidden="true" data-preview-icon>{{if eq .Form.IconKind "pack"}}{{.Form.IconValue}}{{else}}apps{{end}}</span></span>
					<label>Icona scelta<input name="icon_value_pack" value="{{if eq .Form.IconKind "pack"}}{{.Form.IconValue}}{{end}}" readonly></label>
				</div>
				<input type="search" name="q" placeholder="Cerca nel catalogo: mail, account, receipt…" autocomplete="off" hx-get="/admin/icone" hx-trigger="input changed delay:300ms, focus once" hx-target="#icon-results" hx-swap="innerHTML">
				<div id="icon-results" class="icon-results"></div>
			</div>
			<div class="icon-panel" data-kind="upload">
				<input type="file" name="icon_file" accept="image/png,image/webp,image/svg+xml">
				<p class="hint">PNG, WebP o SVG, massimo 512 KB.{{if eq .Form.IconKind "upload"}} Lascia vuoto per mantenere l'icona attuale.{{end}}</p>
			</div>
			<div class="icon-panel" data-kind="url">
				<input name="icon_value_url" value="{{if eq .Form.IconKind "url"}}{{.Form.IconValue}}{{end}}" placeholder="https://…/logo.png">
				<p class="hint">Se l'immagine non è raggiungibile, in plancia compaiono le iniziali.</p>
			</div>
			<label class="inline">Colore di sfondo<input type="color" name="icon_color" value="{{.Form.IconColor}}"></label>
			{{with .Errors.icon}}<p class="field-error">{{.}}</p>{{end}}
		</fieldset>

		<label class="inline"><input type="checkbox" name="enabled" value="1"{{if .Form.Enabled}} checked{{end}}>Visibile in plancia</label>
		<div class="actions">
			<button class="primary" type="submit">Salva</button>
			{{if .Form.ID}}<a class="button" href="/admin/app">Annulla</a>{{end}}
		</div>
	</form>
	{{end}}

	<table class="list">
		<thead><tr><th></th><th>Applicativo</th><th>Categoria</th><th>Indirizzo</th><th></th></tr></thead>
		<tbody>
			{{range .Apps}}
			<tr>
				<td>{{template "app_icon" .App}}</td>
				<td>{{.Title}}{{if not .Enabled}} <span class="tag">nascosto</span>{{end}}{{if .GuideCount}} <span class="tag">{{.GuideCount}} guide</span>{{end}}</td>
				<td class="muted">{{.CategoryName}}</td>
				<td>{{if .URL}}<a href="{{.URL}}" target="_blank" rel="noopener" class="muted">{{.URL}}</a>{{else}}<span class="tag warn">da completare</span>{{end}}</td>
				<td class="actions">
					<button class="icon" type="button" title="Su" hx-post="/admin/app/{{.ID}}/sposta" hx-vals='{"dir":"up"}' hx-target="#section" hx-swap="outerHTML"><span class="material-icons" aria-hidden="true">arrow_upward</span></button>
					<button class="icon" type="button" title="Giù" hx-post="/admin/app/{{.ID}}/sposta" hx-vals='{"dir":"down"}' hx-target="#section" hx-swap="outerHTML"><span class="material-icons" aria-hidden="true">arrow_downward</span></button>
					<button type="button" hx-get="/admin/app/{{.ID}}/modifica" hx-target="#section" hx-swap="outerHTML">Modifica</button>
					<button class="danger" type="button" hx-post="/admin/app/{{.ID}}/elimina" hx-target="#section" hx-swap="outerHTML"
						hx-confirm="Eliminare «{{.Title}}»?{{if eq .GuideCount 1}} La sua guida diventerà generale.{{else if .GuideCount}} Le sue {{.GuideCount}} guide diventeranno generali.{{end}}">Elimina</button>
				</td>
			</tr>
			{{else}}
			<tr><td colspan="5" class="muted">Nessun applicativo.</td></tr>
			{{end}}
		</tbody>
	</table>
</div>
{{end}}
```

- [ ] **Step 5: JS e CSS**

`web/static/js/admin.js`:

```js
// Pannello admin: picker icone e anteprima colore. Nessun handler inline (CSP).
(() => {
	"use strict";

	document.addEventListener("click", (e) => {
		const btn = e.target.closest("[data-icon]");
		if (!btn) return;
		const form = btn.closest("form");
		form.querySelector("[name=icon_value_pack]").value = btn.dataset.icon;
		const icon = form.querySelector("[data-preview-icon]");
		if (icon) icon.textContent = btn.dataset.icon;
		form.querySelectorAll("[data-icon][aria-pressed]").forEach((b) => b.removeAttribute("aria-pressed"));
		btn.setAttribute("aria-pressed", "true");
	});

	document.addEventListener("input", (e) => {
		if (e.target.name !== "icon_color") return;
		const preview = e.target.form?.querySelector("[data-preview]");
		if (preview) preview.style.background = e.target.value;
	});
})();
```

In `web/templates/admin_base.html`, dopo lo script di htmx: `<script src="/static/js/admin.js" defer></script>`.

In coda a `web/static/css/admin.css`:

```css
/* ── Campo icona: un pannello per tipo, scelto dal radio (senza JS) ── */
.icon-tabs { display: flex; flex-wrap: wrap; gap: 1rem; }
.icon-panel { display: none; gap: .6rem; }
.icon-field:has(#ik-pack:checked) .icon-panel[data-kind="pack"],
.icon-field:has(#ik-upload:checked) .icon-panel[data-kind="upload"],
.icon-field:has(#ik-url:checked) .icon-panel[data-kind="url"] { display: grid; }
.icon-pick { display: flex; align-items: end; gap: .7rem; }
.icon-pick .app-icon { width: 42px; height: 42px; }
.icon-results { display: grid; grid-template-columns: repeat(auto-fill, minmax(42px, 1fr)); gap: .3rem; max-height: 220px; overflow-y: auto; }
.icon-choice { justify-content: center; padding: .35rem; }
.icon-choice[aria-pressed="true"] { background: var(--accent-bg); border-color: var(--accent); }
```

- [ ] **Step 6: Verifica**

Run: `go vet ./... && go test ./internal/web/ -v`
Expected: PASS

Manuale: `go run ./cmd/server`, login su `/admin/login` (mock), crea un'app con icona del pacchetto (ricerca "receipt"), poi una con PNG caricato; controlla in plancia le icone e nella console del browser nessun errore CSP.

- [ ] **Step 7: Commit**

```bash
git add internal/web web
git commit -m "feat(admin): gestione applicativi con icone da catalogo, upload o URL

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 12: Gestione guide

**Files:**
- Create: `internal/web/admin_guides.go`, `web/templates/admin_guide.html`
- Modify: `internal/web/server.go` (route)
- Test: `internal/web/admin_guides_test.go`

**Interfaces:**
- Consumes: convenzione sezioni admin e helper di validazione (Task 10), `database.Guide*`, `database.ListApps`, `database.GuideKindLink` (Task 3–4)
- Produces: route `/admin/guide[...]`, template `admin_guide.html`, `guides_section`

- [ ] **Step 1: Test che falliscono** — `internal/web/admin_guides_test.go`

```go
package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestGuidesCRUD(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	apps, _ := db.ListApps()
	webmail := apps[1]

	rec := do(t, s, "POST", "/admin/guide", url.Values{
		"title": {"VPN da casa"}, "url": {"https://wiki.local/vpn"}, "app_id": {"0"}, "enabled": {"1"},
	}, c, hx)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "VPN da casa") || !strings.Contains(rec.Body.String(), "Generale") {
		t.Fatalf("crea generale: %d\n%s", rec.Code, rec.Body)
	}
	rec = do(t, s, "POST", "/admin/guide", url.Values{
		"title": {"Firma email"}, "url": {"https://wiki.local/firma"}, "app_id": {itoa(webmail.ID)}, "enabled": {"1"},
	}, c, hx)
	if rec.Code != 200 {
		t.Fatalf("crea agganciata: %d", rec.Code)
	}

	gs, _ := db.ListGuides()
	if len(gs) != 2 || gs[0].AppID != nil || gs[1].AppID == nil || *gs[1].AppID != webmail.ID || gs[1].Kind != "link" {
		t.Fatalf("DB: %+v", gs)
	}

	firma := gs[1].ID
	rec = do(t, s, "GET", "/admin/guide/"+itoa(firma)+"/modifica", nil, c, hx)
	if !strings.Contains(rec.Body.String(), `value="Firma email"`) {
		t.Fatal("modifica: form non precompilato")
	}
	do(t, s, "POST", "/admin/guide/"+itoa(firma), url.Values{
		"title": {"Firma email"}, "url": {"https://wiki.local/firma"}, "app_id": {"0"},
	}, c, hx)
	g, _ := db.GetGuide(firma)
	if g.AppID != nil || g.Enabled {
		t.Fatalf("update: attesa generale e nascosta, ottenuto %+v", g)
	}

	if rec := do(t, s, "POST", "/admin/guide/"+itoa(firma)+"/sposta", url.Values{"dir": {"up"}}, c, hx); rec.Code != 200 {
		t.Fatalf("sposta: %d", rec.Code)
	}
	if rec := do(t, s, "POST", "/admin/guide/"+itoa(firma)+"/elimina", nil, c, hx); rec.Code != 200 {
		t.Fatalf("elimina: %d", rec.Code)
	}
	if _, err := db.GetGuide(firma); err == nil {
		t.Fatal("guida non eliminata")
	}
}

func TestGuideValidation(t *testing.T) {
	s, _ := newTestServer(t, nil)
	c := login(t, s)
	rec := do(t, s, "POST", "/admin/guide", url.Values{"title": {""}, "url": {"wiki/vpn"}, "app_id": {"999"}}, c, hx)
	body := rec.Body.String()
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("atteso 422, ottenuto %d", rec.Code)
	}
	for _, want := range []string{"Campo obbligatorio.", "Inserisci l&#39;indirizzo completo", "Applicativo non trovato."} {
		if !strings.Contains(body, want) {
			t.Errorf("manca %q", want)
		}
	}
	rec = do(t, s, "POST", "/admin/guide", url.Values{"title": {"x"}, "url": {""}, "app_id": {"0"}}, c, hx)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatal("per le guide l'URL è obbligatorio")
	}
}
```

Run: `go test ./internal/web/ -run Guide`
Expected: FAIL — 404/405 sulle route `/admin/guide`

- [ ] **Step 2: Implementa** — `internal/web/admin_guides.go`

```go
package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

type guideForm struct {
	ID      int64
	AppID   int64 // 0 = generale
	Title   string
	URL     string
	Enabled bool
}

type guideRow struct {
	database.Guide
	AppTitle string // "" = generale
}

type guidesSection struct {
	Guides []guideRow
	Apps   []database.App
	Form   guideForm
	Errors formErrors
}

func newGuideForm() guideForm { return guideForm{Enabled: true} }

func formFromGuide(g database.Guide) guideForm {
	f := guideForm{ID: g.ID, Title: g.Title, URL: g.URL, Enabled: g.Enabled}
	if g.AppID != nil {
		f.AppID = *g.AppID
	}
	return f
}

func (s *Server) guidesData(form guideForm, errs formErrors) (guidesSection, error) {
	guides, err := s.db.ListGuides()
	if err != nil {
		return guidesSection{}, err
	}
	apps, err := s.db.ListApps()
	if err != nil {
		return guidesSection{}, err
	}
	titles := map[int64]string{}
	for _, a := range apps {
		titles[a.ID] = a.Title
	}
	rows := make([]guideRow, 0, len(guides))
	for _, g := range guides {
		row := guideRow{Guide: g}
		if g.AppID != nil {
			row.AppTitle = titles[*g.AppID]
		}
		rows = append(rows, row)
	}
	return guidesSection{Guides: rows, Apps: apps, Form: form, Errors: errs}, nil
}

func (s *Server) renderGuides(w http.ResponseWriter, status int, form guideForm, errs formErrors) {
	sec, err := s.guidesData(form, errs)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.render(w, status, "guides_section", sec)
}

func (s *Server) handleGuidesPage(w http.ResponseWriter, r *http.Request) {
	sec, err := s.guidesData(newGuideForm(), nil)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderPage(w, r, "admin_guide.html", "guide", sec)
}

func (s *Server) handleGuideEdit(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	g, err := s.db.GetGuide(id)
	if errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderGuides(w, http.StatusOK, formFromGuide(g), nil)
}

func (s *Server) handleGuideSave(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	form := guideForm{
		ID:      id,
		Title:   strings.TrimSpace(r.FormValue("title")),
		URL:     strings.TrimSpace(r.FormValue("url")),
		Enabled: r.FormValue("enabled") == "1",
	}
	form.AppID, _ = strconv.ParseInt(r.FormValue("app_id"), 10, 64)

	errs := formErrors{}
	checkText(errs, "title", form.Title, 120, true)
	checkURL(errs, "url", form.URL, true)
	if form.AppID != 0 {
		if _, err := s.db.GetApp(form.AppID); errors.Is(err, database.ErrNotFound) {
			errs.add("app", "Applicativo non trovato.")
		} else if err != nil {
			s.serverError(w, err)
			return
		}
	}
	if len(errs) > 0 {
		s.renderGuides(w, http.StatusUnprocessableEntity, form, errs)
		return
	}

	g := database.Guide{ID: id, Title: form.Title, Kind: database.GuideKindLink, URL: form.URL, Enabled: form.Enabled}
	if form.AppID != 0 {
		g.AppID = &form.AppID
	}
	if id == 0 {
		_, err = s.db.CreateGuide(g)
	} else {
		err = s.db.UpdateGuide(g)
	}
	switch {
	case errors.Is(err, database.ErrNotFound):
		http.NotFound(w, r)
	case err != nil:
		s.serverError(w, err)
	default:
		s.renderGuides(w, http.StatusOK, newGuideForm(), nil)
	}
}

func (s *Server) handleGuideDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.db.DeleteGuide(id); errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderGuides(w, http.StatusOK, newGuideForm(), nil)
}

func (s *Server) handleGuideMove(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.db.MoveGuide(id, moveDir(r)); errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderGuides(w, http.StatusOK, newGuideForm(), nil)
}
```

In `routes()` aggiungi:

```go
	s.mux.HandleFunc("GET /admin/guide", s.requireAdmin(s.handleGuidesPage))
	s.mux.HandleFunc("GET /admin/guide/{id}/modifica", s.requireAdmin(s.handleGuideEdit))
	s.mux.HandleFunc("POST /admin/guide", s.requireAdmin(s.handleGuideSave))
	s.mux.HandleFunc("POST /admin/guide/{id}", s.requireAdmin(s.handleGuideSave))
	s.mux.HandleFunc("POST /admin/guide/{id}/elimina", s.requireAdmin(s.handleGuideDelete))
	s.mux.HandleFunc("POST /admin/guide/{id}/sposta", s.requireAdmin(s.handleGuideMove))
```

- [ ] **Step 3: Template** — `web/templates/admin_guide.html`

```html
{{define "admin_guide.html"}}{{template "admin_top" .}}
<h1>Guide</h1>
{{template "guides_section" .Body}}
{{template "admin_bottom" .}}{{end}}

{{define "guides_section"}}
<div id="section">
	<form class="card form" hx-post="{{if .Form.ID}}/admin/guide/{{.Form.ID}}{{else}}/admin/guide{{end}}" hx-target="#section" hx-swap="outerHTML">
		<h2>{{if .Form.ID}}Modifica guida{{else}}Nuova guida{{end}}</h2>
		<div class="row">
			<label>Titolo<input name="title" value="{{.Form.Title}}" maxlength="120" required></label>
			<label>Applicativo
				<select name="app_id">
					<option value="0"{{if eq .Form.AppID 0}} selected{{end}}>Generale (colonna laterale)</option>
					{{$sel := .Form.AppID}}
					{{range .Apps}}<option value="{{.ID}}"{{if eq .ID $sel}} selected{{end}}>{{.Title}}</option>{{end}}
				</select>
			</label>
		</div>
		{{with .Errors.title}}<p class="field-error">{{.}}</p>{{end}}
		{{with .Errors.app}}<p class="field-error">{{.}}</p>{{end}}
		<label>Indirizzo (URL)<input name="url" value="{{.Form.URL}}" placeholder="https://…" required></label>
		{{with .Errors.url}}<p class="field-error">{{.}}</p>{{end}}
		<label class="inline"><input type="checkbox" name="enabled" value="1"{{if .Form.Enabled}} checked{{end}}>Visibile in plancia</label>
		<div class="actions">
			<button class="primary" type="submit">Salva</button>
			{{if .Form.ID}}<a class="button" href="/admin/guide">Annulla</a>{{end}}
		</div>
	</form>

	<table class="list">
		<thead><tr><th>Guida</th><th>Applicativo</th><th></th></tr></thead>
		<tbody>
			{{range .Guides}}
			<tr>
				<td><a href="{{.URL}}" target="_blank" rel="noopener">{{.Title}}</a>{{if not .Enabled}} <span class="tag">nascosta</span>{{end}}</td>
				<td class="muted">{{if .AppTitle}}{{.AppTitle}}{{else}}Generale{{end}}</td>
				<td class="actions">
					<button class="icon" type="button" title="Su" hx-post="/admin/guide/{{.ID}}/sposta" hx-vals='{"dir":"up"}' hx-target="#section" hx-swap="outerHTML"><span class="material-icons" aria-hidden="true">arrow_upward</span></button>
					<button class="icon" type="button" title="Giù" hx-post="/admin/guide/{{.ID}}/sposta" hx-vals='{"dir":"down"}' hx-target="#section" hx-swap="outerHTML"><span class="material-icons" aria-hidden="true">arrow_downward</span></button>
					<button type="button" hx-get="/admin/guide/{{.ID}}/modifica" hx-target="#section" hx-swap="outerHTML">Modifica</button>
					<button class="danger" type="button" hx-post="/admin/guide/{{.ID}}/elimina" hx-confirm="Eliminare la guida «{{.Title}}»?" hx-target="#section" hx-swap="outerHTML">Elimina</button>
				</td>
			</tr>
			{{else}}
			<tr><td colspan="3" class="muted">Nessuna guida.</td></tr>
			{{end}}
		</tbody>
	</table>
</div>
{{end}}
```

Lo spostamento ↑↓ agisce entro lo stesso gruppo (stessa app, oppure tra le generali), coerente con l'ordine in plancia.

- [ ] **Step 4: Verifica e commit**

Run: `go vet ./... && go test ./internal/web/ -v`
Expected: PASS

```bash
git add internal/web web/templates
git commit -m "feat(admin): gestione guide di tipo link, generali o agganciate

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 13: Gestione avvisi

**Files:**
- Create: `internal/web/admin_alerts.go`, `web/templates/admin_avvisi.html`
- Modify: `internal/web/server.go` (route)
- Test: `internal/web/admin_alerts_test.go`

**Interfaces:**
- Consumes: convenzione sezioni admin e helper di validazione (Task 10), `currentAdmin` (Task 9), `inputTimeLayout`, func `inputTime`, `fmtDate`, `fmtDatePtr`, `levelLabel` (Task 7), `database.Alert*`, `database.Level*` (Task 4)
- Produces: route `/admin/avvisi[...]`, template `admin_avvisi.html`, `alerts_section`; `func (s *Server) parseInputTime(v string) (time.Time, error)`

- [ ] **Step 1: Test che falliscono** — `internal/web/admin_alerts_test.go`

```go
package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

func alertValues(overrides map[string]string) url.Values {
	v := url.Values{
		"title": {"Manutenzione Sicraweb"}, "body": {"Dalle 13 alle 15.\nSalvare il lavoro."},
		"level": {"maintenance"}, "starts_at": {"2026-10-06T09:00"}, "ends_at": {"2026-10-09T15:00"},
	}
	for k, val := range overrides {
		v.Set(k, val)
	}
	return v
}

func TestCreateAlertShowsInDashboard(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)

	page := do(t, s, "GET", "/admin/avvisi", nil, c, nil).Body.String()
	if !strings.Contains(page, `value="2026-10-06T10:00"`) {
		t.Fatal("nuovo avviso: inizio precompilato con l'ora corrente in Europe/Rome")
	}

	rec := do(t, s, "POST", "/admin/avvisi", alertValues(nil), c, hx)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Manutenzione Sicraweb") || !strings.Contains(rec.Body.String(), "Attivo") {
		t.Fatalf("crea: %d\n%s", rec.Code, rec.Body)
	}
	current, _, _ := db.ListAlertsForAdmin(fixedNow)
	if len(current) != 1 || current[0].CreatedBy != "mrossi" || current[0].Body != "Dalle 13 alle 15.\nSalvare il lavoro." {
		t.Fatalf("DB: %+v", current)
	}
	if !current[0].StartsAt.Equal(time.Date(2026, 10, 6, 7, 0, 0, 0, time.UTC)) {
		t.Fatalf("09:00 a Roma (CEST) = 07:00Z, ottenuto %v", current[0].StartsAt)
	}
	if dash := do(t, s, "GET", "/", nil, nil, nil).Body.String(); !strings.Contains(dash, "pill-maintenance") {
		t.Fatal("l'avviso attivo deve comparire in plancia")
	}
}

func TestAlertValidation(t *testing.T) {
	s, _ := newTestServer(t, nil)
	c := login(t, s)
	for name, tc := range map[string]struct {
		overrides map[string]string
		msg       string
	}{
		"titolo vuoto":     {map[string]string{"title": " "}, "Campo obbligatorio."},
		"livello":          {map[string]string{"level": "critico"}, "Livello non valido."},
		"inizio non data":  {map[string]string{"starts_at": "domani"}, "Data e ora non valide."},
		"fine prima":       {map[string]string{"ends_at": "2026-10-06T08:00"}, "La fine deve essere successiva all&#39;inizio."},
		"fine uguale":      {map[string]string{"ends_at": "2026-10-06T09:00"}, "La fine deve essere successiva all&#39;inizio."},
		"testo troppo lungo": {map[string]string{"body": strings.Repeat("a", 2001)}, "Massimo 2000 caratteri."},
	} {
		t.Run(name, func(t *testing.T) {
			rec := do(t, s, "POST", "/admin/avvisi", alertValues(tc.overrides), c, hx)
			if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), tc.msg) {
				t.Fatalf("atteso 422 con %q, ottenuto %d\n%s", tc.msg, rec.Code, rec.Body)
			}
		})
	}
}

func TestAlertDSTRoundTrip(t *testing.T) { // Review Focus #3
	s, db := newTestServer(t, nil)
	c := login(t, s)
	do(t, s, "POST", "/admin/avvisi", alertValues(map[string]string{"starts_at": "2026-03-29T10:00", "ends_at": ""}), c, hx)

	_, expired, _ := db.ListAlertsForAdmin(fixedNow)
	current, _, _ := db.ListAlertsForAdmin(fixedNow)
	all := append(current, expired...)
	if len(all) != 1 || !all[0].StartsAt.Equal(time.Date(2026, 3, 29, 8, 0, 0, 0, time.UTC)) || all[0].EndsAt != nil {
		t.Fatalf("29/03 10:00 Roma (già CEST) = 08:00Z senza fine, ottenuto %+v", all)
	}
	rec := do(t, s, "GET", "/admin/avvisi/"+itoa(all[0].ID)+"/modifica", nil, c, hx)
	if !strings.Contains(rec.Body.String(), `value="2026-03-29T10:00"`) {
		t.Fatalf("il form deve mostrare di nuovo 10:00\n%s", rec.Body)
	}
}

func TestExpiredAlertReactivation(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	end := fixedNow.Add(-time.Hour)
	id, _ := db.CreateAlert(database.Alert{Title: "Vecchio avviso", Level: "news",
		StartsAt: fixedNow.Add(-48 * time.Hour), EndsAt: &end, CreatedAt: fixedNow, CreatedBy: "gbianchi"})

	page := do(t, s, "GET", "/admin/avvisi", nil, c, nil).Body.String()
	if !strings.Contains(page, "Scaduti") || !strings.Contains(page, "Vecchio avviso") {
		t.Fatal("gli avvisi scaduti vanno elencati a parte")
	}
	rec := do(t, s, "POST", "/admin/avvisi/"+itoa(id), alertValues(map[string]string{"title": "Vecchio avviso", "level": "news", "starts_at": "2026-10-04T10:00", "ends_at": ""}), c, hx)
	if rec.Code != 200 {
		t.Fatalf("riattivazione: %d\n%s", rec.Code, rec.Body)
	}
	a, _ := db.GetAlert(id)
	if a.EndsAt != nil || a.CreatedBy != "gbianchi" {
		t.Fatalf("riattivato senza fine, autore invariato: %+v", a)
	}
	if rec := do(t, s, "POST", "/admin/avvisi/"+itoa(id)+"/elimina", nil, c, hx); rec.Code != 200 {
		t.Fatalf("elimina: %d", rec.Code)
	}
	if rec := do(t, s, "POST", "/admin/avvisi/"+itoa(id)+"/elimina", nil, c, hx); rec.Code != http.StatusNotFound {
		t.Fatalf("elimina due volte: %d", rec.Code)
	}
}
```

Run: `go test ./internal/web/ -run Alert`
Expected: FAIL — route `/admin/avvisi` assenti

- [ ] **Step 2: Implementa** — `internal/web/admin_alerts.go`

```go
package web

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

type alertForm struct {
	ID       int64
	Title    string
	Body     string
	Level    string
	StartsAt string // formato inputTimeLayout, fuso s.loc()
	EndsAt   string // "" = senza scadenza
}

type alertRow struct {
	database.Alert
	Status string // Attivo | Programmato | Scaduto
}

type alertsSection struct {
	Current []alertRow
	Expired []alertRow
	Form    alertForm
	Errors  formErrors
}

var alertLevels = []string{database.LevelUrgent, database.LevelMaintenance, database.LevelNews}

func (s *Server) parseInputTime(v string) (time.Time, error) {
	return time.ParseInLocation(inputTimeLayout, v, s.loc())
}

func (s *Server) newAlertForm() alertForm {
	return alertForm{Level: database.LevelNews, StartsAt: s.now().In(s.loc()).Format(inputTimeLayout)}
}

func (s *Server) formFromAlert(a database.Alert) alertForm {
	f := alertForm{ID: a.ID, Title: a.Title, Body: a.Body, Level: a.Level,
		StartsAt: a.StartsAt.In(s.loc()).Format(inputTimeLayout)}
	if a.EndsAt != nil {
		f.EndsAt = a.EndsAt.In(s.loc()).Format(inputTimeLayout)
	}
	return f
}

func (s *Server) alertStatus(a database.Alert) string {
	now := s.now()
	switch {
	case a.EndsAt != nil && !now.Before(*a.EndsAt):
		return "Scaduto"
	case a.StartsAt.After(now):
		return "Programmato"
	default:
		return "Attivo"
	}
}

func (s *Server) alertsData(form alertForm, errs formErrors) (alertsSection, error) {
	current, expired, err := s.db.ListAlertsForAdmin(s.now())
	if err != nil {
		return alertsSection{}, err
	}
	sec := alertsSection{Form: form, Errors: errs}
	for _, a := range current {
		sec.Current = append(sec.Current, alertRow{Alert: a, Status: s.alertStatus(a)})
	}
	for _, a := range expired {
		sec.Expired = append(sec.Expired, alertRow{Alert: a, Status: "Scaduto"})
	}
	return sec, nil
}

func (s *Server) renderAlerts(w http.ResponseWriter, status int, form alertForm, errs formErrors) {
	sec, err := s.alertsData(form, errs)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.render(w, status, "alerts_section", sec)
}

func (s *Server) handleAlertsPage(w http.ResponseWriter, r *http.Request) {
	sec, err := s.alertsData(s.newAlertForm(), nil)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderPage(w, r, "admin_avvisi.html", "avvisi", sec)
}

func (s *Server) handleAlertEdit(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a, err := s.db.GetAlert(id)
	if errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderAlerts(w, http.StatusOK, s.formFromAlert(a), nil)
}

func (s *Server) handleAlertSave(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	form := alertForm{
		ID:       id,
		Title:    strings.TrimSpace(r.FormValue("title")),
		Body:     strings.TrimSpace(strings.ReplaceAll(r.FormValue("body"), "\r\n", "\n")),
		Level:    r.FormValue("level"),
		StartsAt: strings.TrimSpace(r.FormValue("starts_at")),
		EndsAt:   strings.TrimSpace(r.FormValue("ends_at")),
	}

	errs := formErrors{}
	checkText(errs, "title", form.Title, 120, true)
	checkText(errs, "body", form.Body, 2000, false)
	validLevel := false
	for _, l := range alertLevels {
		validLevel = validLevel || form.Level == l
	}
	if !validLevel {
		errs.add("level", "Livello non valido.")
	}
	starts, err := s.parseInputTime(form.StartsAt)
	if err != nil {
		errs.add("starts_at", "Data e ora non valide.")
	}
	var ends *time.Time
	if form.EndsAt != "" {
		e, err := s.parseInputTime(form.EndsAt)
		switch {
		case err != nil:
			errs.add("ends_at", "Data e ora non valide.")
		case !e.After(starts):
			errs.add("ends_at", "La fine deve essere successiva all'inizio.")
		default:
			ends = &e
		}
	}
	if len(errs) > 0 {
		s.renderAlerts(w, http.StatusUnprocessableEntity, form, errs)
		return
	}

	a := database.Alert{ID: id, Title: form.Title, Body: form.Body, Level: form.Level, StartsAt: starts, EndsAt: ends}
	if id == 0 {
		a.CreatedAt = s.now()
		a.CreatedBy = s.currentAdmin(r)
		_, err = s.db.CreateAlert(a)
	} else {
		var old database.Alert
		if old, err = s.db.GetAlert(id); err == nil {
			a.Notify = old.Notify // gestito dal sotto-progetto 3
			err = s.db.UpdateAlert(a)
		}
	}
	switch {
	case errors.Is(err, database.ErrNotFound):
		http.NotFound(w, r)
	case err != nil:
		s.serverError(w, err)
	default:
		s.renderAlerts(w, http.StatusOK, s.newAlertForm(), nil)
	}
}

func (s *Server) handleAlertDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.db.DeleteAlert(id); errors.Is(err, database.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderAlerts(w, http.StatusOK, s.newAlertForm(), nil)
}
```

In `routes()` aggiungi:

```go
	s.mux.HandleFunc("GET /admin/avvisi", s.requireAdmin(s.handleAlertsPage))
	s.mux.HandleFunc("GET /admin/avvisi/{id}/modifica", s.requireAdmin(s.handleAlertEdit))
	s.mux.HandleFunc("POST /admin/avvisi", s.requireAdmin(s.handleAlertSave))
	s.mux.HandleFunc("POST /admin/avvisi/{id}", s.requireAdmin(s.handleAlertSave))
	s.mux.HandleFunc("POST /admin/avvisi/{id}/elimina", s.requireAdmin(s.handleAlertDelete))
```

- [ ] **Step 3: Template** — `web/templates/admin_avvisi.html`

```html
{{define "admin_avvisi.html"}}{{template "admin_top" .}}
<h1>Avvisi</h1>
{{template "alerts_section" .Body}}
{{template "admin_bottom" .}}{{end}}

{{define "alert_rows"}}
{{range .}}
<tr>
	<td><span class="pill pill-{{.Level}}">{{levelLabel .Level}}</span></td>
	<td>{{.Title}}<br><small class="muted">di {{.CreatedBy}}</small></td>
	<td class="muted">{{fmtDate .StartsAt}}{{if .EndsAt}} → {{fmtDatePtr .EndsAt}}{{else}} → senza scadenza{{end}}</td>
	<td><span class="tag{{if eq .Status "Scaduto"}}{{else if eq .Status "Programmato"}} warn{{end}}">{{.Status}}</span></td>
	<td class="actions">
		<button type="button" hx-get="/admin/avvisi/{{.ID}}/modifica" hx-target="#section" hx-swap="outerHTML">Modifica</button>
		<button class="danger" type="button" hx-post="/admin/avvisi/{{.ID}}/elimina" hx-confirm="Eliminare l'avviso «{{.Title}}»?" hx-target="#section" hx-swap="outerHTML">Elimina</button>
	</td>
</tr>
{{end}}
{{end}}

{{define "alerts_section"}}
<div id="section">
	<form class="card form" hx-post="{{if .Form.ID}}/admin/avvisi/{{.Form.ID}}{{else}}/admin/avvisi{{end}}" hx-target="#section" hx-swap="outerHTML">
		<h2>{{if .Form.ID}}Modifica avviso{{else}}Nuovo avviso{{end}}</h2>
		<div class="row">
			<label>Titolo<input name="title" value="{{.Form.Title}}" maxlength="120" required></label>
			<label>Livello
				<select name="level">
					<option value="urgent"{{if eq .Form.Level "urgent"}} selected{{end}}>Urgente</option>
					<option value="maintenance"{{if eq .Form.Level "maintenance"}} selected{{end}}>Manutenzione</option>
					<option value="news"{{if eq .Form.Level "news"}} selected{{end}}>Novità</option>
				</select>
			</label>
		</div>
		{{with .Errors.title}}<p class="field-error">{{.}}</p>{{end}}
		{{with .Errors.level}}<p class="field-error">{{.}}</p>{{end}}
		<label>Testo<textarea name="body" maxlength="2000" rows="5">{{.Form.Body}}</textarea></label>
		{{with .Errors.body}}<p class="field-error">{{.}}</p>{{else}}<p class="hint">Testo semplice: gli a capo vengono mantenuti, gli indirizzi https://… diventano link.</p>{{end}}
		<div class="row">
			<label>Visibile dal<input type="datetime-local" name="starts_at" value="{{.Form.StartsAt}}" required></label>
			<label>Fino al (facoltativo)<input type="datetime-local" name="ends_at" value="{{.Form.EndsAt}}"></label>
		</div>
		{{with .Errors.starts_at}}<p class="field-error">{{.}}</p>{{end}}
		{{with .Errors.ends_at}}<p class="field-error">{{.}}</p>{{end}}
		<div class="actions">
			<button class="primary" type="submit">Salva</button>
			{{if .Form.ID}}<a class="button" href="/admin/avvisi">Annulla</a>{{end}}
		</div>
	</form>

	<h2>Attivi e programmati</h2>
	<table class="list">
		<thead><tr><th>Livello</th><th>Avviso</th><th>Periodo</th><th>Stato</th><th></th></tr></thead>
		<tbody>
			{{template "alert_rows" .Current}}
			{{if not .Current}}<tr><td colspan="5" class="muted">Nessun avviso attivo o programmato.</td></tr>{{end}}
		</tbody>
	</table>

	{{if .Expired}}
	<h2 class="spaced">Scaduti (ultimi 30)</h2>
	<p class="hint">Per riattivarne uno, modificalo e sposta o togli la data di fine.</p>
	<table class="list">
		<thead><tr><th>Livello</th><th>Avviso</th><th>Periodo</th><th>Stato</th><th></th></tr></thead>
		<tbody>{{template "alert_rows" .Expired}}</tbody>
	</table>
	{{end}}
</div>
{{end}}
```

In coda a `web/static/css/admin.css`: `h2.spaced { margin-top: 1.5rem; }`

- [ ] **Step 4: Verifica e commit**

Run: `go vet ./... && go test ./internal/web/ -v`
Expected: PASS

```bash
git add internal/web web
git commit -m "feat(admin): gestione avvisi con periodo di visibilità

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 14: Documentazione, verifica end-to-end, PR

**Files:**
- Modify: `CLAUDE.md`, `README.md`

**Interfaces:**
- Consumes: tutto quanto sopra.

- [ ] **Step 1: Aggiorna `CLAUDE.md`**

Sostituisci la sezione `## Architettura` con:

```markdown
## Architettura

- `cmd/server/main.go`: solo wiring — `config.Load()`, `database.Open`, `web.New`, graceful shutdown, flag `-healthcheck` (legge solo `PORT`, non valida il resto della config).
- `internal/config`: env → `Config`; validazione all'avvio (`SESSION_SECRET` ≥ 32 caratteri obbligatorio se `LDAP_HOST` ≠ `mock`; in mock, se assente, segreto casuale → le sessioni non sopravvivono al riavvio).
- `internal/database`: SQL raw su `modernc.org/sqlite`. Migrazioni in `migrations.go` con `PRAGMA user_version`: **mai modificare una migrazione rilasciata, aggiungerne una in coda**. Tabelle `categories`, `apps`, `guides`, `alerts`. Date come testo UTC `2006-01-02T15:04:05Z` (confrontabili come stringhe). `apps.icon_kind` e `guides.kind` validati in Go, non con CHECK. Ordinamento manuale via `sort_order` + `moveRow` (rinumera 0..n-1 nell'ambito: categoria per le app, app o "generale" per le guide — `app_id IS ?` gestisce NULL).
- Visibilità in plancia: app `enabled=1 AND url<>''`; guida `enabled=1` e generale oppure di un'app visibile; avviso `starts_at <= now < ends_at` (o senza fine).
- `internal/auth`: porting di GoPulley con differenze volute — StartTLS fallito = login fallito (nessun ripiego in chiaro), username solo `[A-Za-z0-9._@-]`, in mock admin = `ADMIN_USERS` (o tutti se vuoto). `RateLimiter` in memoria: 5 fallimenti/15 min per username e per IP → blocco 30s raddoppiato fino a 15 min.
- `internal/icons`: catalogo Material Icons da `codepoints.txt` (embedded). Font self-hosted in `web/static/fonts` (Apache 2.0, stesso v145 di UtenzePA).
- `internal/web`: `Server` con dipendenze esplicite (testabile con `httptest`, vedi `newTestServer` in `server_test.go`). Catena: `securityHeaders` → `http.CrossOriginProtection` (CSRF, nessun token nei form) → `ServeMux`.
- **CSP stretta**: niente `<script>`/`<style>` inline né `on*=`; unica eccezione `style-src-attr 'unsafe-inline'` per il colore delle icone. Nuovo JS → file in `web/static/js`. HTMX configurato via `<meta name="htmx-config">` (`includeIndicatorStyles:false`).
- **Admin**: pagine separate con shell `admin_top`/`admin_bottom` (dati `pageView{adminPage, Body}`). Ogni sezione è un template `<nome>_section` dentro `<div id="section">`; ogni azione HTMX (`hx-post`, `hx-target="#section"`, `hx-swap="outerHTML"`) restituisce l'intera sezione: 200 se ok, **422 con errori** (htmx configurato per fare swap anche sui 422). Route: `GET /admin/<s>`, `GET /admin/<s>/{id}/modifica`, `POST /admin/<s>`, `POST /admin/<s>/{id}`, `POST …/elimina`, `POST …/sposta` (`dir=up|down`).
- Sessione: cookie `cruscotto_admin` cifrato (gorilla/sessions), `Path=/admin`, 8 ore, `Secure` da `X-Forwarded-Proto` o `SECURE_COOKIES`. Senza sessione: 303 al login, oppure 401 + `HX-Redirect` per HTMX.
- **Upload icone**: tipo dal contenuto (PNG/WebP/SVG), max 512 KB, nome casuale in `UPLOAD_DIR/icons`, servite da `/uploads/icons/{file}` con `Content-Security-Policy: sandbox` (uno script in un SVG non gira mai). Il file vecchio si cancella quando l'icona cambia o l'app viene eliminata.
- `web/templates` (parse all'avvio, relativo alla cwd: avviare dalla root del repo), `web/static` (`htmx.min.js` 2.0.4, `dashboard.js`, `admin.js`).
```

E nella sezione `## Comandi`, sotto `go run ./cmd/server`, aggiungi: `# admin: http://localhost:8080/admin (LDAP_HOST=mock: qualsiasi credenziale)`.

- [ ] **Step 2: Aggiorna `README.md`**

Dopo "Avvio rapido" aggiungi:

```markdown
## Amministrazione

`/admin` richiede il login con le credenziali di dominio (LDAP/Active Directory). Sono amministratori gli utenti del gruppo `LDAP_ADMIN_GROUP` o elencati in `ADMIN_USERS`. Da lì si gestiscono:

- **Avvisi**: urgente, manutenzione o novità, con periodo di visibilità;
- **Applicativi**: titolo, indirizzo, categoria e icona (catalogo Material Icons, file caricato o URL);
- **Guide**: link a guide e FAQ, generali oppure agganciate a un applicativo (pulsante "?" sulla sua card);
- **Categorie**: raggruppamento delle card in plancia.

Al primo avvio esistono solo la categoria "Applicativi" con Rubrica e Webmail **senza indirizzo**: compaiono in plancia dopo averlo inserito.

In sviluppo `LDAP_HOST=mock` accetta qualsiasi credenziale.
```

- [ ] **Step 3: Verifica completa**

```bash
go vet ./... && go test ./... && go build ./...
MSYS_NO_PATHCONV=1 docker build -q --build-arg VERSION=0.1.0-rc -t cruscottopa:check .
MSYS_NO_PATHCONV=1 docker run -d --rm --name cp-check -p 18090:8080 cruscottopa:check
sleep 8
curl -s localhost:18090/health                     # {"status":"ok","version":"0.1.0-rc"}
curl -s -o /dev/null -w "%{http_code}\n" localhost:18090/admin   # 303
MSYS_NO_PATHCONV=1 docker exec cp-check ls -la /data /data/uploads   # cruscotto.db + uploads di proprietà cruscottopa
docker stop cp-check && docker rmi cruscottopa:check
```

Expected: test verdi, health ok con la versione iniettata, `/admin` → 303, file in `/data` di proprietà dell'utente 1001.

- [ ] **Step 4: Prova manuale guidata** (`go run ./cmd/server`, browser)

1. `/` → "Nessun applicativo configurato".
2. `/admin/login` → login (mock) → panoramica con Rubrica e Webmail "da completare".
3. Applicativi: imposta l'URL di Rubrica; crea "Sicraweb" in una nuova categoria "Gestionali esterni" con icona `description`.
4. Guide: una generale ("VPN da casa") e una agganciata a Sicraweb.
5. Avvisi: uno urgente attivo.
6. `/` → testata, pillola rossa (click → dialog), tile con segmento "? 1" su Sicraweb (popover sotto il pulsante), colonna "Guide generali"; digitando "vpn" nella ricerca resta solo la guida; `/` porta il focus sulla ricerca.
7. Console del browser: **nessuna violazione CSP**.
8. Riduci la finestra a larghezza telefono: una colonna, guide sotto.

- [ ] **Step 5: Commit, push, PR**

```bash
git add CLAUDE.md README.md
git commit -m "docs: architettura e uso del pannello admin

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
git push -u origin feat/plancia-admin
gh pr create --base main --title "Plancia + admin base (sotto-progetto 1)" --body "$(cat <<'EOF'
Implementa il sotto-progetto 1 secondo `docs/superpowers/specs/2026-10-06-plancia-admin-design.md`.

- Plancia: avvisi, applicativi per categoria con guide agganciate (segmento "?"), guide generali, ricerca
- Admin con login LDAP: avvisi, applicativi (icone Material, upload sandboxato, URL), guide, categorie
- Migrazioni versionate, CSP stretta, protezione CSRF, rate limit sul login

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

Expected: PR aperta, check `test` verde.
