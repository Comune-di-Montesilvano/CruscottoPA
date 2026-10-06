# Backup e ripristino — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Backup automatici e manuali di DB + uploads in archivi `tar.gz` nel volume, scaricabili dal pannello admin, e ripristino completo (da elenco o da file caricato a pezzi) con validazione, backup pre-ripristino e riavvio del container.

**Architecture:** Nuovo package `internal/backup` con un `Service` (creazione, elenco, retention GFS, scheduler, estrazione/validazione, upload a chunk, swap + `exit`). `internal/database` espone `Snapshot` (`VACUUM INTO`) e `CurrentSchemaVersion`. `internal/web` aggiunge la pagina `/admin/backup` con la convenzione delle sezioni HTMX; l'upload a chunk e l'attesa del riavvio sono in `admin.js`.

**Tech Stack:** Go 1.26 stdlib (`archive/tar`, `compress/gzip`), `modernc.org/sqlite`, HTMX 2.

**Spec:** `docs/superpowers/specs/2026-10-06-backup-ripristino-design.md`

**Branch:** `feat/backup` creato da `spec/backup`; a fine piano PR verso `main`.

## Global Constraints

- Archivi in `<dir di DB_PATH>/backups`, nome `cruscotto-AAAAMMGG-HHMMSS-<tipo>.tar.gz`, tipo ∈ `auto | manuale | pre-ripristino`, ora nel fuso `TZ`.
- Validazione nome nelle route: `^cruscotto-(\d{8}-\d{6})-(auto|manuale|pre-ripristino)\.tar\.gz$`.
- Archivio: `manifest.json` (primo), `cruscotto.db` (da `VACUUM INTO`), `uploads/…`; file scritti con permessi `0640`; scrittura su `.tmp` + rename.
- `BACKUP_INTERVAL_HOURS` default `24`, `0` disattiva; nei tre posti (compose, `.env.example`, config).
- Retention GFS solo sui `auto`: ≤7 g tutti, ≤35 g uno per settimana ISO, ≤365 g uno per mese, oltre eliminati.
- Chunk upload 512 KB; archivio e contenuto estratto max 2 GB; sessioni upload scadono dopo 1 ora.
- Conferma ripristino: campo `conferma` = `RIPRISTINA`, altrimenti 422.
- Un'operazione alla volta (`ErrBusy`).
- Nessuno script/stile inline (CSP); testi in italiano; commit con trailer `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`; `go vet ./... && go test ./...` verdi prima di ogni commit.

## Review Focus

1. **Archivio malevolo** (voce `../../etc/x`, link simbolico, percorso assoluto) caricato da un admin → rifiutato prima di scrivere fuori da `restore-tmp`, dati intatti. Test in Task 4.
2. **Ripristino fallito a metà validazione** → i dati attuali restano identici e il lock viene rilasciato (si può fare un nuovo backup subito dopo). Test in Task 5.
3. **Backup di una versione futura** (schema più alto) → rifiutato con messaggio che dice di aggiornare l'app. Test in Task 4.
4. **Riavvio subito dopo un backup automatico** → nessun secondo backup finché l'intervallo non è scaduto (niente archivi duplicati a ogni restart). Test in Task 3.
5. **Due creazioni nello stesso secondo** (doppio click su "Crea backup ora") → la seconda non sovrascrive la prima. Test in Task 2.

---

### Task 1: Configurazione e snapshot del DB

**Files:**
- Modify: `internal/config/config.go`, `internal/config/config_test.go`, `docker-compose.yml`, `.env.example`, `.gitignore`
- Create: `internal/database/snapshot.go`, `internal/database/snapshot_test.go`

**Interfaces:**
- Produces:
  ```go
  // config.Config
  BackupIntervalHours int
  // database
  func (db *DB) Snapshot(path string) error
  func CurrentSchemaVersion() int
  ```

- [ ] **Step 1: Branch**

```bash
git checkout spec/backup && git checkout -b feat/backup
```

- [ ] **Step 2: Test config** — in `internal/config/config_test.go` aggiungi `"BACKUP_INTERVAL_HOURS"` all'elenco `allVars` e in coda:

```go
func TestLoadBackupInterval(t *testing.T) {
	clearEnv(t)
	t.Setenv("LDAP_HOST", "mock")
	cfg, err := Load()
	if err != nil || cfg.BackupIntervalHours != 24 {
		t.Fatalf("default: atteso 24, ottenuto %d (%v)", cfg.BackupIntervalHours, err)
	}
	t.Setenv("BACKUP_INTERVAL_HOURS", "0")
	if cfg, err := Load(); err != nil || cfg.BackupIntervalHours != 0 {
		t.Fatalf("0 deve disattivare: %d %v", cfg.BackupIntervalHours, err)
	}
	for _, bad := range []string{"-1", "abc", "1.5"} {
		t.Setenv("BACKUP_INTERVAL_HOURS", bad)
		if _, err := Load(); err == nil {
			t.Errorf("BACKUP_INTERVAL_HOURS=%q: atteso errore", bad)
		}
	}
}
```

- [ ] **Step 3: Test snapshot** — `internal/database/snapshot_test.go`

```go
package database

import (
	"path/filepath"
	"testing"
)

func TestSnapshot(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.CreateCategory("Esterni"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "copia.db")
	if err := db.Snapshot(path); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if err := db.Snapshot(path); err == nil {
		t.Fatal("VACUUM INTO su file esistente deve fallire: mai sovrascrivere in silenzio")
	}

	cp, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer cp.Close()
	cats, _ := cp.ListCategories()
	if len(cats) != 2 || cats[1].Name != "Esterni" {
		t.Fatalf("copia senza i dati: %+v", cats)
	}
	if v, _ := cp.SchemaVersion(); v != CurrentSchemaVersion() {
		t.Fatalf("schema copia %d, atteso %d", v, CurrentSchemaVersion())
	}
}
```

Run: `go test ./internal/config/ ./internal/database/`
Expected: FAIL — `cfg.BackupIntervalHours undefined`, `db.Snapshot undefined`

- [ ] **Step 4: Implementa**

`internal/config/config.go`: aggiungi il campo `BackupIntervalHours int` a `Config` (dopo `Location`), poi in `Load()` dopo il parsing di `LOG_LEVEL`:

```go
	if cfg.BackupIntervalHours, err = getEnvInt("BACKUP_INTERVAL_HOURS", 24); err != nil {
		return Config{}, err
	}
```

e in fondo al file:

```go
func getEnvInt(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s: atteso un intero >= 0, ottenuto %q", key, v)
	}
	return n, nil
}
```

`internal/database/snapshot.go`:

```go
package database

// Snapshot scrive in path una copia coerente del DB con VACUUM INTO (mai copiare
// il file a caldo: WAL e pagine in uso la renderebbero incoerente). Fallisce se
// path esiste già.
func (db *DB) Snapshot(path string) error {
	_, err := db.Exec(`VACUUM INTO ?`, path)
	return err
}

// CurrentSchemaVersion è la versione di schema prodotta dalle migrazioni di questo binario.
func CurrentSchemaVersion() int { return len(migrations) }
```

- [ ] **Step 5: File di deploy**

`docker-compose.yml`, sotto `environment:` dopo `- LOG_LEVEL=…`: `      - BACKUP_INTERVAL_HOURS=${BACKUP_INTERVAL_HOURS:-24}`

`.env.example`, dopo il blocco `LOG_LEVEL`:

```bash

# ── Backup ─────────────────────────────────────────────────────────────────
# Ogni quante ore creare un backup automatico (DB + uploads) in /data/backups.
# 0 = disattivati. Le copie stanno nello stesso volume: scaricane una
# periodicamente da /admin/backup e conservala fuori dal server.
BACKUP_INTERVAL_HOURS=24
```

`.gitignore`, sezione "Database locali": aggiungi `/backups/` e `/restore-tmp/`.

- [ ] **Step 6: Verifica e commit**

Run: `go vet ./... && go test ./...`
Expected: PASS

```bash
git add internal/config internal/database docker-compose.yml .env.example .gitignore
git commit -m "feat(backup): BACKUP_INTERVAL_HOURS e snapshot del DB con VACUUM INTO

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Servizio backup — creazione, elenco, eliminazione, stato

**Files:**
- Create: `internal/backup/backup.go`, `internal/backup/archive.go`
- Test: `internal/backup/backup_test.go`

**Interfaces:**
- Consumes: `database.DB` (`Snapshot`, `SchemaVersion`, `Close`), `database.CurrentSchemaVersion`, `database.Open` (Task 1)
- Produces:
  ```go
  const KindAuto, KindManual, KindPreRestore = "auto", "manuale", "pre-ripristino"
  var ErrBusy, ErrNotFound, ErrProtected error
  type Store interface { Snapshot(path string) error; SchemaVersion() (int, error); Close() error }
  type Manifest struct { Format int; AppVersion string; SchemaVersion int; CreatedAt time.Time; Kind string; Files int } // tag json snake_case
  type Info struct { Name, Kind string; CreatedAt time.Time; Size int64 }
  type Status struct { LastSuccess time.Time; LastAutoError string }
  type Options struct { Store Store; DBPath, UploadDir, AppVersion string; MaxSchema int; Location *time.Location; Now func() time.Time; Exit func(int); MaxExtract int64 }
  func New(o Options) (*Service, error)
  func (s *Service) Create(kind string) (Info, error)
  func (s *Service) List() ([]Info, error)          // più recenti prima
  func (s *Service) Path(name string) (string, error)
  func (s *Service) Delete(name string) error        // ErrProtected per gli auto
  func (s *Service) Status() Status
  // interni usati dai task successivi:
  func (s *Service) create(kind string) (Info, error) // senza lock
  func (s *Service) record(kind string, err error)
  func (s *Service) restoreDir() string
  ```

- [ ] **Step 1: Test che falliscono** — `internal/backup/backup_test.go`

```go
package backup

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

type env struct {
	s       *Service
	db      *database.DB
	dbPath  string
	uploads string
	clock   time.Time
	exited  chan int
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	e := &env{
		dbPath:  filepath.Join(dir, "cruscotto.db"),
		uploads: filepath.Join(dir, "uploads"),
		clock:   time.Date(2026, 10, 6, 8, 0, 0, 0, time.UTC),
		exited:  make(chan int, 4),
	}
	db, err := database.Open(e.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	e.db = db
	os.MkdirAll(filepath.Join(e.uploads, "icons"), 0o750)
	os.WriteFile(filepath.Join(e.uploads, "icons", "a.png"), []byte("PNG-A"), 0o640)

	e.s, err = New(Options{
		Store: db, DBPath: e.dbPath, UploadDir: e.uploads, AppVersion: "test",
		MaxSchema: database.CurrentSchemaVersion(), Location: time.UTC,
		Now:  func() time.Time { return e.clock },
		Exit: func(code int) { e.exited <- code },
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *env) advance(d time.Duration) { e.clock = e.clock.Add(d) }

// readArchive restituisce i nomi delle voci in ordine e il loro contenuto.
func readArchive(t *testing.T, path string) ([]string, map[string][]byte) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	var order []string
	content := map[string][]byte{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(tr)
		order = append(order, h.Name)
		content[h.Name] = b
	}
	return order, content
}

func TestCreateAndList(t *testing.T) {
	e := newEnv(t)
	e.db.CreateCategory("Esterni")

	info, err := e.s.Create(KindManual)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if info.Name != "cruscotto-20261006-080000-manuale.tar.gz" {
		t.Fatalf("nome: %s", info.Name)
	}
	path, err := e.s.Path(info.Name)
	if err != nil {
		t.Fatal(err)
	}
	order, content := readArchive(t, path)
	if order[0] != "manifest.json" {
		t.Fatalf("manifest.json deve essere la prima voce: %v", order)
	}
	var m Manifest
	if err := json.Unmarshal(content["manifest.json"], &m); err != nil {
		t.Fatal(err)
	}
	if m.Format != 1 || m.Kind != KindManual || m.AppVersion != "test" || m.Files != 1 || m.SchemaVersion != database.CurrentSchemaVersion() {
		t.Fatalf("manifest: %+v", m)
	}
	if string(content["uploads/icons/a.png"]) != "PNG-A" {
		t.Fatalf("upload mancante o diverso: %v", order)
	}

	restored := filepath.Join(t.TempDir(), "r.db")
	os.WriteFile(restored, content["cruscotto.db"], 0o640)
	rdb, err := database.Open(restored)
	if err != nil {
		t.Fatal(err)
	}
	defer rdb.Close()
	if cats, _ := rdb.ListCategories(); len(cats) != 2 {
		t.Fatalf("DB nell'archivio senza i dati: %+v", cats)
	}

	list, _ := e.s.List()
	if len(list) != 1 || list[0].Kind != KindManual || list[0].Size == 0 || !list[0].CreatedAt.Equal(e.clock) {
		t.Fatalf("List: %+v", list)
	}
	if e.s.Status().LastSuccess.IsZero() {
		t.Fatal("Status deve riportare l'ultimo backup riuscito")
	}
}

func TestCreateSameSecondDoesNotOverwrite(t *testing.T) { // Review Focus #5
	e := newEnv(t)
	first, _ := e.s.Create(KindManual)
	p, _ := e.s.Path(first.Name)
	before, _ := os.Stat(p)
	if _, err := e.s.Create(KindManual); err == nil {
		t.Fatal("seconda creazione nello stesso secondo: atteso errore")
	}
	after, _ := os.Stat(p)
	if !after.ModTime().Equal(before.ModTime()) || after.Size() != before.Size() {
		t.Fatal("il primo backup è stato sovrascritto")
	}
	if list, _ := e.s.List(); len(list) != 1 {
		t.Fatalf("attesi 1 backup, ottenuti %d", len(list))
	}
}

func TestCreateBusy(t *testing.T) {
	e := newEnv(t)
	e.s.mu.Lock()
	defer e.s.mu.Unlock()
	if _, err := e.s.Create(KindManual); !errors.Is(err, ErrBusy) {
		t.Fatalf("atteso ErrBusy, ottenuto %v", err)
	}
}

func TestPathAndDelete(t *testing.T) {
	e := newEnv(t)
	auto, _ := e.s.Create(KindAuto)
	e.advance(time.Second)
	man, _ := e.s.Create(KindManual)

	if err := e.s.Delete(auto.Name); !errors.Is(err, ErrProtected) {
		t.Fatalf("gli auto non si eliminano a mano: %v", err)
	}
	if err := e.s.Delete(man.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.Path(man.Name); !errors.Is(err, ErrNotFound) {
		t.Fatalf("dopo Delete: %v", err)
	}
	for _, bad := range []string{"../cruscotto.db", "cruscotto-x.tar.gz", "cruscotto-20261006-080000-auto.tar.gz.tmp", ""} {
		if _, err := e.s.Path(bad); !errors.Is(err, ErrNotFound) {
			t.Errorf("Path(%q): atteso ErrNotFound, ottenuto %v", bad, err)
		}
	}
}

func TestCleanupOnStart(t *testing.T) {
	e := newEnv(t)
	dir := filepath.Dir(e.dbPath)
	os.WriteFile(filepath.Join(dir, "backups", "x.tar.gz.tmp"), []byte("x"), 0o640)
	os.WriteFile(filepath.Join(dir, "backups", "upload-1.part"), []byte("x"), 0o640)
	os.MkdirAll(filepath.Join(dir, "restore-tmp", "uploads"), 0o750)
	os.MkdirAll(e.uploads+".old", 0o750)

	if _, err := New(e.s.o); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		filepath.Join(dir, "backups", "x.tar.gz.tmp"),
		filepath.Join(dir, "backups", "upload-1.part"),
		filepath.Join(dir, "restore-tmp"),
		e.uploads + ".old",
	} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("residuo non rimosso all'avvio: %s", p)
		}
	}
}

type failingStore struct{ Store }

func (failingStore) Snapshot(string) error { return errors.New("disco pieno") }

func TestAutoFailureStatus(t *testing.T) {
	e := newEnv(t)
	good := e.s.o.Store
	e.s.o.Store = failingStore{good}
	if _, err := e.s.Create(KindAuto); err == nil {
		t.Fatal("atteso errore")
	}
	if st := e.s.Status(); st.LastAutoError == "" {
		t.Fatal("l'errore dell'ultimo automatico va riportato")
	}
	entries, _ := os.ReadDir(e.s.dir)
	for _, en := range entries {
		if matched, _ := regexp.MatchString(`\.tmp$`, en.Name()); matched {
			t.Fatalf("file temporaneo lasciato: %s", en.Name())
		}
	}
	e.s.o.Store = good
	if _, err := e.s.Create(KindAuto); err != nil {
		t.Fatal(err)
	}
	if st := e.s.Status(); st.LastAutoError != "" {
		t.Fatalf("dopo un automatico riuscito l'errore va azzerato: %q", st.LastAutoError)
	}
}
```

Run: `go test ./internal/backup/`
Expected: FAIL — package senza sorgenti non di test / `undefined: New`

- [ ] **Step 2: Implementa** — `internal/backup/backup.go`

```go
// Package backup crea, elenca, conserva e ripristina archivi tar.gz con lo
// snapshot del DB SQLite e la cartella degli upload.
package backup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	KindAuto       = "auto"
	KindManual     = "manuale"
	KindPreRestore = "pre-ripristino"

	formatVersion  = 1
	nameTimeLayout = "20060102-150405"
	manifestEntry  = "manifest.json"
	dbEntry        = "cruscotto.db"
	uploadsEntry   = "uploads"
)

var (
	ErrBusy      = errors.New("Operazione di backup o ripristino già in corso: riprova tra poco.")
	ErrNotFound  = errors.New("Backup non trovato.")
	ErrProtected = errors.New("I backup automatici sono gestiti dalla conservazione automatica e non si eliminano a mano.")

	nameRe = regexp.MustCompile(`^cruscotto-(\d{8}-\d{6})-(auto|manuale|pre-ripristino)\.tar\.gz$`)
)

// Store è ciò che il servizio usa del DB (implementato da *database.DB).
type Store interface {
	Snapshot(path string) error
	SchemaVersion() (int, error)
	Close() error
}

// Manifest descrive un archivio; è la prima voce del tar.
type Manifest struct {
	Format        int       `json:"format"`
	AppVersion    string    `json:"app_version"`
	SchemaVersion int       `json:"schema_version"`
	CreatedAt     time.Time `json:"created_at"`
	Kind          string    `json:"kind"`
	Files         int       `json:"files"`
}

// Info è un archivio presente nella cartella dei backup.
type Info struct {
	Name      string
	Kind      string
	CreatedAt time.Time
	Size      int64
}

// Status riassume l'esito dei backup per la panoramica admin.
type Status struct {
	LastSuccess   time.Time // zero se non c'è nessun backup
	LastAutoError string    // "" se l'ultimo automatico è riuscito
}

type Options struct {
	Store      Store
	DBPath     string
	UploadDir  string
	AppVersion string
	MaxSchema  int
	Location   *time.Location
	Now        func() time.Time
	Exit       func(code int)
	MaxExtract int64 // limite archivio caricato e contenuto estratto
}

type Service struct {
	o   Options
	dir string

	mu sync.Mutex // un backup/ripristino alla volta

	stMu        sync.Mutex
	lastOK      time.Time
	lastAutoErr string

	upMu    sync.Mutex
	uploads map[string]*upload
}

func New(o Options) (*Service, error) {
	if o.Location == nil {
		o.Location = time.UTC
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Exit == nil {
		o.Exit = os.Exit
	}
	if o.MaxExtract == 0 {
		o.MaxExtract = 2 << 30
	}
	s := &Service{o: o, dir: filepath.Join(filepath.Dir(o.DBPath), "backups"), uploads: map[string]*upload{}}
	if err := os.MkdirAll(s.dir, 0o750); err != nil {
		return nil, fmt.Errorf("cartella backup: %w", err)
	}
	s.cleanup()
	return s, nil
}

func (s *Service) restoreDir() string { return filepath.Join(filepath.Dir(s.o.DBPath), "restore-tmp") }

// cleanup rimuove i residui di operazioni interrotte da un crash o da un riavvio.
func (s *Service) cleanup() {
	entries, _ := os.ReadDir(s.dir)
	for _, e := range entries {
		if n := e.Name(); strings.HasSuffix(n, ".tmp") || strings.HasSuffix(n, ".part") {
			os.Remove(filepath.Join(s.dir, n))
		}
	}
	os.RemoveAll(s.restoreDir())
	os.RemoveAll(s.o.UploadDir + ".old")
}

// Create crea un backup del tipo indicato (KindAuto o KindManual).
func (s *Service) Create(kind string) (Info, error) {
	if !s.mu.TryLock() {
		return Info{}, ErrBusy
	}
	defer s.mu.Unlock()
	info, err := s.create(kind)
	s.record(kind, err)
	return info, err
}

func (s *Service) record(kind string, err error) {
	s.stMu.Lock()
	defer s.stMu.Unlock()
	if err == nil {
		s.lastOK = s.o.Now()
		if kind == KindAuto {
			s.lastAutoErr = ""
		}
		return
	}
	if kind == KindAuto {
		s.lastAutoErr = err.Error()
	}
}

func (s *Service) create(kind string) (Info, error) {
	now := s.o.Now()
	name := fmt.Sprintf("cruscotto-%s-%s.tar.gz", now.In(s.o.Location).Format(nameTimeLayout), kind)
	final := filepath.Join(s.dir, name)
	if _, err := os.Stat(final); err == nil {
		return Info{}, fmt.Errorf("esiste già un backup con nome %s: riprova tra un secondo", name)
	}

	snap := final + ".db.tmp"
	defer os.Remove(snap)
	if err := s.o.Store.Snapshot(snap); err != nil {
		return Info{}, fmt.Errorf("copia del database: %w", err)
	}
	schema, err := s.o.Store.SchemaVersion()
	if err != nil {
		return Info{}, err
	}
	files, err := listFiles(s.o.UploadDir)
	if err != nil {
		return Info{}, fmt.Errorf("elenco upload: %w", err)
	}

	tmp := final + ".tmp"
	m := Manifest{Format: formatVersion, AppVersion: s.o.AppVersion, SchemaVersion: schema,
		CreatedAt: now.UTC(), Kind: kind, Files: len(files)}
	if err := writeArchive(tmp, m, snap, s.o.UploadDir, files); err != nil {
		os.Remove(tmp)
		return Info{}, fmt.Errorf("scrittura archivio: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp)
		return Info{}, err
	}
	st, err := os.Stat(final)
	if err != nil {
		return Info{}, err
	}
	return Info{Name: name, Kind: kind, CreatedAt: now, Size: st.Size()}, nil
}

// List restituisce gli archivi presenti, più recenti prima.
func (s *Service) List() ([]Info, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	out := []Info{}
	for _, e := range entries {
		m := nameRe.FindStringSubmatch(e.Name())
		if m == nil || !e.Type().IsRegular() {
			continue
		}
		t, err := time.ParseInLocation(nameTimeLayout, m[1], s.o.Location)
		if err != nil {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Info{Name: e.Name(), Kind: m[2], CreatedAt: t, Size: fi.Size()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// Path restituisce il percorso di un archivio esistente; il nome è validato (niente traversal).
func (s *Service) Path(name string) (string, error) {
	if !nameRe.MatchString(name) {
		return "", ErrNotFound
	}
	p := filepath.Join(s.dir, name)
	if fi, err := os.Stat(p); err != nil || !fi.Mode().IsRegular() {
		return "", ErrNotFound
	}
	return p, nil
}

func (s *Service) Delete(name string) error {
	m := nameRe.FindStringSubmatch(name)
	if m == nil {
		return ErrNotFound
	}
	if m[2] == KindAuto {
		return ErrProtected
	}
	p, err := s.Path(name)
	if err != nil {
		return err
	}
	return os.Remove(p)
}

func (s *Service) Status() Status {
	s.stMu.Lock()
	st := Status{LastSuccess: s.lastOK, LastAutoError: s.lastAutoErr}
	s.stMu.Unlock()
	if list, err := s.List(); err == nil && len(list) > 0 && list[0].CreatedAt.After(st.LastSuccess) {
		st.LastSuccess = list[0].CreatedAt
	}
	return st
}
```

`upload` è definito nel Task 5: per compilare ora aggiungi in fondo a `backup.go` un segnaposto che il Task 5 sostituisce con la definizione completa nello stesso file `upload.go`:

```go
// upload è una sessione di caricamento a pezzi (vedi upload.go).
type upload struct{}
```

`internal/backup/archive.go`:

```go
package backup

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// listFiles restituisce i file regolari sotto root, relativi e con '/'. root assente = nessun file.
func listFiles(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) && p == root {
				return filepath.SkipDir
			}
			return err
		}
		if d.Type().IsRegular() {
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	return out, err
}

func writeArchive(path string, m Manifest, dbFile, uploadRoot string, files []string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)

	err = func() error {
		mj, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			return err
		}
		if err := tw.WriteHeader(&tar.Header{Name: manifestEntry, Mode: 0o640, Size: int64(len(mj)),
			ModTime: m.CreatedAt, Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		if _, err := tw.Write(mj); err != nil {
			return err
		}
		if err := addFile(tw, dbEntry, dbFile); err != nil {
			return err
		}
		for _, rel := range files {
			err := addFile(tw, uploadsEntry+"/"+rel, filepath.Join(uploadRoot, filepath.FromSlash(rel)))
			if err != nil && !os.IsNotExist(err) { // file rimosso nel frattempo: lo salta
				return err
			}
		}
		if err := tw.Close(); err != nil {
			return err
		}
		return gz.Close()
	}()
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

func addFile(tw *tar.Writer, name, src string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o640, Size: fi.Size(),
		ModTime: fi.ModTime(), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	_, err = io.CopyN(tw, f, fi.Size())
	return err
}
```

- [ ] **Step 3: Verifica e commit**

Run: `go vet ./... && go test ./internal/backup/ -v`
Expected: PASS (6 test)

```bash
git add internal/backup
git commit -m "feat(backup): creazione, elenco ed eliminazione degli archivi

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Retention GFS e scheduler

**Files:**
- Create: `internal/backup/schedule.go`
- Test: `internal/backup/schedule_test.go`

**Interfaces:**
- Consumes: `Service`, `List`, `Create`, `KindAuto` (Task 2)
- Produces:
  ```go
  func (s *Service) Prune(now time.Time) ([]string, error) // nomi eliminati
  func (s *Service) Scheduler(ctx context.Context, interval time.Duration)
  func (s *Service) tick(interval time.Duration)
  ```

- [ ] **Step 1: Test che falliscono** — `internal/backup/schedule_test.go`

```go
package backup

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// touch crea un archivio finto (Prune guarda solo i nomi).
func touch(t *testing.T, e *env, at time.Time, kind string) string {
	t.Helper()
	name := "cruscotto-" + at.Format(nameTimeLayout) + "-" + kind + ".tar.gz"
	if err := os.WriteFile(filepath.Join(e.s.dir, name), []byte("x"), 0o640); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestPruneGFS(t *testing.T) {
	e := newEnv(t) // ora: 2026-10-06 08:00 UTC
	d := func(y int, m time.Month, day int) time.Time { return time.Date(y, m, day, 3, 0, 0, 0, time.UTC) }

	keep := []string{
		touch(t, e, d(2026, 10, 5), KindAuto),        // giornaliero
		touch(t, e, d(2026, 10, 1), KindAuto),        // giornaliero (5 giorni)
		touch(t, e, d(2026, 9, 23), KindAuto),        // settimana W39: il più recente
		touch(t, e, d(2026, 7, 20), KindAuto),        // luglio: il più recente
		touch(t, e, d(2025, 9, 1), KindManual),       // manuale: mai toccato
		touch(t, e, d(2025, 9, 1), KindPreRestore),   // pre-ripristino: mai toccato
	}
	drop := []string{
		touch(t, e, d(2026, 9, 21), KindAuto), // stessa W39, più vecchio
		touch(t, e, d(2026, 7, 10), KindAuto), // stesso mese, più vecchio
		touch(t, e, d(2025, 9, 1), KindAuto),  // oltre un anno
	}

	deleted, err := e.s.Prune(e.clock)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(deleted)
	sort.Strings(drop)
	if strings.Join(deleted, ",") != strings.Join(drop, ",") {
		t.Fatalf("eliminati %v, attesi %v", deleted, drop)
	}
	for _, n := range keep {
		if _, err := e.s.Path(n); err != nil {
			t.Errorf("%s doveva restare", n)
		}
	}
}

func TestSchedulerTick(t *testing.T) { // Review Focus #4
	e := newEnv(t)
	countAuto := func() int {
		n := 0
		list, _ := e.s.List()
		for _, b := range list {
			if b.Kind == KindAuto {
				n++
			}
		}
		return n
	}

	e.s.tick(24 * time.Hour)
	if countAuto() != 1 {
		t.Fatal("primo giro: atteso un backup automatico")
	}
	e.advance(time.Hour)
	e.s.tick(24 * time.Hour) // come un riavvio un'ora dopo
	if countAuto() != 1 {
		t.Fatal("intervallo non scaduto: nessun nuovo backup")
	}
	e.advance(24 * time.Hour)
	e.s.tick(24 * time.Hour)
	if countAuto() != 2 {
		t.Fatal("intervallo scaduto: atteso un secondo backup")
	}
}

func TestSchedulerDisabled(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e.s.Scheduler(ctx, 0)
	if list, _ := e.s.List(); len(list) != 0 {
		t.Fatal("intervallo 0: nessun backup automatico")
	}
}
```

Run: `go test ./internal/backup/ -run 'Prune|Scheduler'`
Expected: FAIL — `e.s.Prune undefined`

- [ ] **Step 2: Implementa** — `internal/backup/schedule.go`

```go
package backup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// Prune applica la retention GFS ai soli backup automatici (manuali e
// pre-ripristino restano finché un admin non li elimina):
// ≤7 giorni tutti, ≤35 uno per settimana ISO, ≤365 uno per mese, oltre eliminati.
func (s *Service) Prune(now time.Time) ([]string, error) {
	list, err := s.List() // più recenti prima: in ogni bucket resta il primo visto
	if err != nil {
		return nil, err
	}
	const day = 24 * time.Hour
	seenWeek, seenMonth := map[string]bool{}, map[string]bool{}
	var deleted []string
	for _, b := range list {
		if b.Kind != KindAuto {
			continue
		}
		age := now.Sub(b.CreatedAt)
		var key string
		var seen map[string]bool
		switch {
		case age <= 7*day:
			continue
		case age <= 35*day:
			y, w := b.CreatedAt.ISOWeek()
			key, seen = fmt.Sprintf("%d-W%02d", y, w), seenWeek
		case age <= 365*day:
			key, seen = b.CreatedAt.Format("2006-01"), seenMonth
		}
		if seen != nil && !seen[key] {
			seen[key] = true
			continue
		}
		if err := os.Remove(filepath.Join(s.dir, b.Name)); err != nil {
			return deleted, err
		}
		deleted = append(deleted, b.Name)
	}
	return deleted, nil
}

// Scheduler crea un backup automatico quando l'ultimo è più vecchio di interval
// (controllo subito e poi ogni minuto). interval <= 0 disattiva.
func (s *Service) Scheduler(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	s.tick(interval)
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.tick(interval)
		}
	}
}

func (s *Service) tick(interval time.Duration) {
	list, err := s.List()
	if err != nil {
		slog.Error("backup: elenco", "err", err)
		return
	}
	for _, b := range list {
		if b.Kind == KindAuto {
			if s.o.Now().Sub(b.CreatedAt) < interval {
				return
			}
			break
		}
	}
	info, err := s.Create(KindAuto)
	if err != nil {
		if !errors.Is(err, ErrBusy) {
			slog.Error("backup automatico non riuscito", "err", err)
		}
		return
	}
	slog.Info("backup automatico creato", "file", info.Name, "bytes", info.Size)
	if deleted, err := s.Prune(s.o.Now()); err != nil {
		slog.Error("backup: conservazione", "err", err)
	} else if len(deleted) > 0 {
		slog.Info("backup: eliminati dalla conservazione", "file", deleted)
	}
}
```

- [ ] **Step 3: Verifica e commit**

Run: `go vet ./... && go test ./internal/backup/ -v`
Expected: PASS

```bash
git add internal/backup
git commit -m "feat(backup): conservazione GFS e backup automatici pianificati

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: Estrazione e validazione dell'archivio

**Files:**
- Create: `internal/backup/restore.go`
- Test: `internal/backup/validate_test.go`

**Interfaces:**
- Consumes: `Service`, `restoreDir`, `Manifest`, costanti `manifestEntry`/`dbEntry`/`uploadsEntry`/`formatVersion` (Task 2)
- Produces:
  ```go
  type ValidationError struct{ Msg string } // Error() = Msg, mostrabile all'utente
  func (s *Service) extract(archive string) (Manifest, error) // estrae in restoreDir; su errore la rimuove
  ```

- [ ] **Step 1: Test che falliscono** — `internal/backup/validate_test.go`

```go
package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type entry struct {
	name string
	body []byte
	typ  byte
	link string
}

func buildArchive(t *testing.T, entries []entry) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, en := range entries {
		typ := en.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		h := &tar.Header{Name: en.name, Mode: 0o640, Size: int64(len(en.body)), Typeflag: typ, Linkname: en.link, ModTime: time.Now()}
		if typ != tar.TypeReg {
			h.Size = 0
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if typ == tar.TypeReg {
			tw.Write(en.body)
		}
	}
	tw.Close()
	gz.Close()
	p := filepath.Join(t.TempDir(), "in.tar.gz")
	os.WriteFile(p, buf.Bytes(), 0o640)
	return p
}

func manifestFor(schema, format int) []byte {
	b, _ := json.Marshal(Manifest{Format: format, AppVersion: "x", SchemaVersion: schema, Kind: KindManual, CreatedAt: time.Now().UTC()})
	return b
}

func snapshotBytes(t *testing.T, e *env) []byte {
	t.Helper()
	p := filepath.Join(t.TempDir(), "s.db")
	if err := e.db.Snapshot(p); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	return b
}

func TestExtractValidArchive(t *testing.T) {
	e := newEnv(t)
	info, _ := e.s.Create(KindManual)
	p, _ := e.s.Path(info.Name)

	m, err := e.s.extract(p)
	if err != nil {
		t.Fatalf("archivio valido rifiutato: %v", err)
	}
	if m.Kind != KindManual {
		t.Fatalf("manifest: %+v", m)
	}
	if b, err := os.ReadFile(filepath.Join(e.s.restoreDir(), "uploads", "icons", "a.png")); err != nil || string(b) != "PNG-A" {
		t.Fatalf("upload non estratto: %v", err)
	}
	if _, err := os.Stat(filepath.Join(e.s.restoreDir(), "cruscotto.db")); err != nil {
		t.Fatal("DB non estratto")
	}
}

func TestExtractRejects(t *testing.T) { // Review Focus #1 e #3
	e := newEnv(t)
	db := snapshotBytes(t, e)
	ok := manifestFor(e.s.o.MaxSchema, 1)
	notGzip := filepath.Join(t.TempDir(), "x.tar.gz")
	os.WriteFile(notGzip, []byte("non sono un archivio"), 0o640)

	cases := map[string]struct {
		archive string
		msg     string
	}{
		"traversal":         {buildArchive(t, []entry{{name: "manifest.json", body: ok}, {name: "../evil.txt", body: []byte("x")}}), "Percorso non ammesso"},
		"traversal annidato": {buildArchive(t, []entry{{name: "manifest.json", body: ok}, {name: "uploads/../../evil.txt", body: []byte("x")}}), "Percorso non ammesso"},
		"assoluto":          {buildArchive(t, []entry{{name: "manifest.json", body: ok}, {name: "/tmp/evil.txt", body: []byte("x")}}), "Percorso non ammesso"},
		"symlink":           {buildArchive(t, []entry{{name: "manifest.json", body: ok}, {name: "uploads/link", typ: tar.TypeSymlink, link: "/etc/passwd"}}), "Tipo di voce non ammesso"},
		"voce inattesa":     {buildArchive(t, []entry{{name: "manifest.json", body: ok}, {name: "altro.txt", body: []byte("x")}}), "Voce inattesa"},
		"senza manifest":    {buildArchive(t, []entry{{name: "cruscotto.db", body: db}}), "senza manifest.json"},
		"formato 2":         {buildArchive(t, []entry{{name: "manifest.json", body: manifestFor(1, 2)}, {name: "cruscotto.db", body: db}}), "Formato di backup 2"},
		"schema futuro":     {buildArchive(t, []entry{{name: "manifest.json", body: manifestFor(e.s.o.MaxSchema + 1, 1)}, {name: "cruscotto.db", body: db}}), "versione più recente"},
		"senza db":          {buildArchive(t, []entry{{name: "manifest.json", body: ok}}), "senza cruscotto.db"},
		"db corrotto":       {buildArchive(t, []entry{{name: "manifest.json", body: ok}, {name: "cruscotto.db", body: append([]byte("SQLite format 3\x00"), bytes.Repeat([]byte{0xff}, 4096)...)}}), "danneggiato"},
		"non sqlite":        {buildArchive(t, []entry{{name: "manifest.json", body: ok}, {name: "cruscotto.db", body: []byte("ciao")}}), "danneggiato"},
		"non gzip":          {notGzip, "non è un archivio di backup"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := e.s.extract(tc.archive)
			var ve *ValidationError
			if !errors.As(err, &ve) || !strings.Contains(ve.Msg, tc.msg) {
				t.Fatalf("atteso ValidationError con %q, ottenuto %v", tc.msg, err)
			}
			if _, err := os.Stat(e.s.restoreDir()); !os.IsNotExist(err) {
				t.Fatal("restore-tmp non ripulita dopo il rifiuto")
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(e.dbPath), "evil.txt")); !os.IsNotExist(err) {
				t.Fatal("file scritto fuori da restore-tmp")
			}
		})
	}
}

func TestExtractSizeLimit(t *testing.T) {
	e := newEnv(t)
	info, _ := e.s.Create(KindManual)
	p, _ := e.s.Path(info.Name)
	e.s.o.MaxExtract = 16
	_, err := e.s.extract(p)
	var ve *ValidationError
	if !errors.As(err, &ve) || !strings.Contains(ve.Msg, "troppo grande") {
		t.Fatalf("atteso rifiuto per dimensione, ottenuto %v", err)
	}
}
```

Run: `go test ./internal/backup/ -run Extract`
Expected: FAIL — `e.s.extract undefined`

- [ ] **Step 2: Implementa** — `internal/backup/restore.go`

```go
package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// ValidationError indica un archivio rifiutato; Msg è pensato per l'utente.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func invalid(format string, a ...any) error { return &ValidationError{Msg: fmt.Sprintf(format, a...)} }

var sqliteMagic = []byte("SQLite format 3\x00")

// extract valida l'archivio e lo estrae in restoreDir. Nessun file viene
// scritto fuori da restoreDir; in caso di errore restoreDir viene rimossa.
func (s *Service) extract(archive string) (m Manifest, err error) {
	dest := s.restoreDir()
	os.RemoveAll(dest)
	if err := os.MkdirAll(dest, 0o750); err != nil {
		return m, err
	}
	defer func() {
		if err != nil {
			os.RemoveAll(dest)
		}
	}()

	f, err := os.Open(archive)
	if err != nil {
		return m, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return m, invalid("Il file non è un archivio di backup valido (.tar.gz).")
	}
	tr := tar.NewReader(gz)

	var manifest *Manifest
	var total int64
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return m, invalid("Il file non è un archivio di backup valido (.tar.gz).")
		}
		name, err := entryName(hdr)
		if err != nil {
			return m, err
		}
		target := filepath.Join(dest, filepath.FromSlash(name))
		switch {
		case hdr.Typeflag == tar.TypeDir:
			if err := os.MkdirAll(target, 0o750); err != nil {
				return m, err
			}
		case name == manifestEntry:
			var mm Manifest
			if err := json.NewDecoder(io.LimitReader(tr, 64<<10)).Decode(&mm); err != nil {
				return m, invalid("manifest.json non leggibile.")
			}
			manifest = &mm
		default:
			total += hdr.Size
			if total > s.o.MaxExtract {
				return m, invalid("Archivio troppo grande una volta estratto (oltre %d MB).", s.o.MaxExtract>>20)
			}
			if err := writeEntry(target, tr, hdr.Size); err != nil {
				return m, err
			}
		}
	}

	if manifest == nil {
		return m, invalid("Archivio senza manifest.json: non è un backup di CruscottoPA.")
	}
	if manifest.Format != formatVersion {
		return m, invalid("Formato di backup %d non supportato.", manifest.Format)
	}
	if manifest.SchemaVersion > s.o.MaxSchema {
		return m, invalid("Backup creato da una versione più recente di CruscottoPA (schema %d, supportato fino a %d): aggiorna prima l'applicazione.",
			manifest.SchemaVersion, s.o.MaxSchema)
	}
	dbPath := filepath.Join(dest, dbEntry)
	if _, err := os.Stat(dbPath); err != nil {
		return m, invalid("Archivio senza cruscotto.db.")
	}
	if err := checkDB(dbPath); err != nil {
		return m, err
	}
	return *manifest, nil
}

// entryName ammette solo file/cartelle regolari sotto manifest.json, cruscotto.db, uploads/.
func entryName(hdr *tar.Header) (string, error) {
	name := hdr.Name
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, `\`) || filepath.IsAbs(name) || strings.Contains(name, ":") {
		return "", invalid("Percorso non ammesso nell'archivio: %s", name)
	}
	clean := strings.TrimSuffix(name, "/")
	for _, part := range strings.Split(clean, "/") {
		if part == "" || part == "." || part == ".." {
			return "", invalid("Percorso non ammesso nell'archivio: %s", name)
		}
	}
	if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeDir {
		return "", invalid("Tipo di voce non ammesso nell'archivio: %s", name)
	}
	if clean != manifestEntry && clean != dbEntry && clean != uploadsEntry && !strings.HasPrefix(clean, uploadsEntry+"/") {
		return "", invalid("Voce inattesa nell'archivio: %s", name)
	}
	return clean, nil
}

func writeEntry(target string, r io.Reader, size int64) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return err
	}
	f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return invalid("Voce duplicata nell'archivio: %s", filepath.Base(target))
	}
	_, err = io.CopyN(f, r, size)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return invalid("Archivio troncato.")
	}
	return err
}

// checkDB verifica header SQLite e integrità del file estratto.
func checkDB(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	head := make([]byte, len(sqliteMagic))
	_, err = io.ReadFull(f, head)
	f.Close()
	if err != nil || !bytes.Equal(head, sqliteMagic) {
		return invalid("Il database nell'archivio è danneggiato.")
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return invalid("Il database nell'archivio è danneggiato.")
	}
	defer db.Close()
	var res string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&res); err != nil || res != "ok" {
		return invalid("Il database nell'archivio è danneggiato.")
	}
	return nil
}
```

- [ ] **Step 3: Verifica e commit**

Run: `go vet ./... && go test ./internal/backup/ -v -run Extract`
Expected: PASS (tutti i sotto-casi)

```bash
git add internal/backup
git commit -m "feat(backup): estrazione sicura e validazione dell'archivio

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: Upload a pezzi e ripristino

**Files:**
- Create: `internal/backup/upload.go`
- Modify: `internal/backup/backup.go` (rimuovi il segnaposto `type upload struct{}`), `internal/backup/restore.go` (aggiungi `PrepareRestore`, `swap`), `internal/backup/schedule.go` (`tick` scade gli upload)
- Test: `internal/backup/restore_test.go`

**Interfaces:**
- Consumes: `extract`, `create`, `record`, `restoreDir` (Task 2–4)
- Produces:
  ```go
  const ChunkSize = 512 << 10
  var ErrUploadNotFound, ErrChunkOrder, ErrChunkTooBig error
  func (s *Service) StartUpload() (string, error)
  func (s *Service) WriteChunk(id string, n int, r io.Reader) error
  func (s *Service) FinishUpload(id string) (string, error) // percorso del file caricato
  func (s *Service) expireUploads()
  // PrepareRestore valida e crea il pre-ripristino; restituisce la funzione di swap
  // (chiude il DB, sostituisce i dati, chiama Exit(0)). Il lock resta preso.
  func (s *Service) PrepareRestore(archive string, removeArchive bool) (func(), error)
  ```

- [ ] **Step 1: Test che falliscono** — `internal/backup/restore_test.go`

```go
package backup

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

func TestUploadChunks(t *testing.T) {
	e := newEnv(t)
	id, err := e.s.StartUpload()
	if err != nil {
		t.Fatal(err)
	}
	if err := e.s.WriteChunk(id, 1, strings.NewReader("x")); !errors.Is(err, ErrChunkOrder) {
		t.Fatalf("pezzo fuori sequenza: %v", err)
	}
	e.s.WriteChunk(id, 0, strings.NewReader("abc"))
	e.s.WriteChunk(id, 1, strings.NewReader("def"))
	if err := e.s.WriteChunk(id, 2, bytes.NewReader(make([]byte, ChunkSize+1))); !errors.Is(err, ErrChunkTooBig) {
		t.Fatalf("pezzo troppo grande: %v", err)
	}
	if err := e.s.WriteChunk("inesistente", 0, strings.NewReader("x")); !errors.Is(err, ErrUploadNotFound) {
		t.Fatalf("id sconosciuto: %v", err)
	}
	path, err := e.s.FinishUpload(id)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "abcdef" {
		t.Fatalf("contenuto caricato: %q", b)
	}
	if _, err := e.s.FinishUpload(id); !errors.Is(err, ErrUploadNotFound) {
		t.Fatal("una sessione chiusa non si riusa")
	}
}

func TestUploadExpiry(t *testing.T) {
	e := newEnv(t)
	id, _ := e.s.StartUpload()
	e.s.WriteChunk(id, 0, strings.NewReader("abc"))
	e.advance(61 * time.Minute)
	e.s.expireUploads()
	if err := e.s.WriteChunk(id, 1, strings.NewReader("x")); !errors.Is(err, ErrUploadNotFound) {
		t.Fatalf("sessione scaduta: %v", err)
	}
	if parts, _ := filepath.Glob(filepath.Join(e.s.dir, "*.part")); len(parts) != 0 {
		t.Fatalf("file parziali non rimossi: %v", parts)
	}
}

func TestRestoreEndToEnd(t *testing.T) {
	e := newEnv(t)
	e.db.CreateCategory("Prima")
	info, _ := e.s.Create(KindManual)
	archive, _ := e.s.Path(info.Name)

	e.db.CreateCategory("Dopo")
	os.WriteFile(filepath.Join(e.uploads, "icons", "b.png"), []byte("PNG-B"), 0o640)
	e.advance(time.Minute)

	swap, err := e.s.PrepareRestore(archive, false)
	if err != nil {
		t.Fatalf("PrepareRestore: %v", err)
	}
	swap()
	select {
	case code := <-e.exited:
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("exit non chiamato")
	}

	db, err := database.Open(e.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cats, _ := db.ListCategories()
	names := ""
	for _, c := range cats {
		names += c.Name + ","
	}
	if !strings.Contains(names, "Prima") || strings.Contains(names, "Dopo") {
		t.Fatalf("DB non ripristinato: %s", names)
	}
	if _, err := os.Stat(filepath.Join(e.uploads, "icons", "b.png")); !os.IsNotExist(err) {
		t.Fatal("uploads non sostituiti: b.png presente")
	}
	if b, _ := os.ReadFile(filepath.Join(e.uploads, "icons", "a.png")); string(b) != "PNG-A" {
		t.Fatal("upload dell'archivio mancante")
	}
	for _, p := range []string{e.s.restoreDir(), e.uploads + ".old"} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("residuo dopo lo swap: %s", p)
		}
	}
	list, _ := e.s.List()
	found := false
	for _, b := range list {
		found = found || b.Kind == KindPreRestore
	}
	if !found {
		t.Fatal("manca il backup pre-ripristino")
	}
}

func TestRestoreInvalidLeavesDataUntouched(t *testing.T) { // Review Focus #2
	e := newEnv(t)
	e.db.CreateCategory("Attuale")
	bad := filepath.Join(t.TempDir(), "x.tar.gz")
	os.WriteFile(bad, []byte("non valido"), 0o640)

	if _, err := e.s.PrepareRestore(bad, true); err == nil {
		t.Fatal("atteso errore di validazione")
	}
	if _, err := os.Stat(bad); !os.IsNotExist(err) {
		t.Fatal("con removeArchive l'upload rifiutato va cancellato")
	}
	if cats, _ := e.db.ListCategories(); len(cats) != 2 {
		t.Fatal("dati modificati da un ripristino fallito")
	}
	if list, _ := e.s.List(); len(list) != 0 {
		t.Fatalf("nessun pre-ripristino se la validazione fallisce: %+v", list)
	}
	if _, err := e.s.Create(KindManual); err != nil {
		t.Fatalf("il lock deve essere rilasciato dopo il rifiuto: %v", err)
	}
	select {
	case <-e.exited:
		t.Fatal("exit chiamato per un ripristino rifiutato")
	default:
	}
}

func TestRestoreBusy(t *testing.T) {
	e := newEnv(t)
	e.s.mu.Lock()
	defer e.s.mu.Unlock()
	if _, err := e.s.PrepareRestore("qualsiasi", false); !errors.Is(err, ErrBusy) {
		t.Fatalf("atteso ErrBusy, ottenuto %v", err)
	}
}
```

Run: `go test ./internal/backup/ -run 'Upload|Restore'`
Expected: FAIL — `e.s.StartUpload undefined`

- [ ] **Step 2: Implementa**

Rimuovi da `internal/backup/backup.go` le righe:

```go
// upload è una sessione di caricamento a pezzi (vedi upload.go).
type upload struct{}
```

`internal/backup/upload.go`:

```go
package backup

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"
)

// ChunkSize resta sotto il limite di default di 1 MB dei reverse proxy nginx.
const (
	ChunkSize = 512 << 10
	uploadTTL = time.Hour
)

var (
	ErrUploadNotFound = errors.New("Caricamento non trovato o scaduto: riprova.")
	ErrChunkOrder     = errors.New("Pezzo del file fuori sequenza.")
	ErrChunkTooBig    = errors.New("Pezzo del file troppo grande.")
)

type upload struct {
	f       *os.File
	path    string
	next    int
	size    int64
	updated time.Time
}

func (s *Service) StartUpload() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	id := hex.EncodeToString(b)
	path := filepath.Join(s.dir, "upload-"+id+".part")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return "", err
	}
	s.upMu.Lock()
	s.uploads[id] = &upload{f: f, path: path, updated: s.o.Now()}
	s.upMu.Unlock()
	return id, nil
}

func (s *Service) WriteChunk(id string, n int, r io.Reader) error {
	s.upMu.Lock()
	defer s.upMu.Unlock()
	u := s.uploads[id]
	if u == nil {
		return ErrUploadNotFound
	}
	if n != u.next {
		return ErrChunkOrder
	}
	data, err := io.ReadAll(io.LimitReader(r, ChunkSize+1))
	if err != nil {
		return err
	}
	if len(data) > ChunkSize {
		return ErrChunkTooBig
	}
	if u.size+int64(len(data)) > s.o.MaxExtract {
		return invalid("Archivio troppo grande (oltre %d MB).", s.o.MaxExtract>>20)
	}
	if _, err := u.f.Write(data); err != nil {
		return err
	}
	u.next++
	u.size += int64(len(data))
	u.updated = s.o.Now()
	return nil
}

// FinishUpload chiude la sessione e restituisce il percorso del file caricato.
func (s *Service) FinishUpload(id string) (string, error) {
	s.upMu.Lock()
	u := s.uploads[id]
	delete(s.uploads, id)
	s.upMu.Unlock()
	if u == nil {
		return "", ErrUploadNotFound
	}
	if err := u.f.Close(); err != nil {
		os.Remove(u.path)
		return "", err
	}
	return u.path, nil
}

func (s *Service) expireUploads() {
	s.upMu.Lock()
	defer s.upMu.Unlock()
	for id, u := range s.uploads {
		if s.o.Now().Sub(u.updated) > uploadTTL {
			u.f.Close()
			os.Remove(u.path)
			delete(s.uploads, id)
		}
	}
}
```

In `internal/backup/schedule.go`, prima riga del corpo di `tick`: `s.expireUploads()`. (Così gli upload abbandonati scadono anche con i backup automatici attivi; con `BACKUP_INTERVAL_HOURS=0` vengono rimossi al riavvio da `cleanup`.)

In coda a `internal/backup/restore.go` (aggiungi `"log/slog"` agli import):

```go
// PrepareRestore valida l'archivio e crea il backup pre-ripristino. Se tutto va
// a buon fine restituisce la funzione di swap, da eseguire dopo aver risposto al
// client: sostituisce DB e uploads e termina il processo (il container viene
// riavviato dalla restart policy). Il lock resta preso fino all'uscita.
func (s *Service) PrepareRestore(archive string, removeArchive bool) (func(), error) {
	if !s.mu.TryLock() {
		return nil, ErrBusy
	}
	_, err := s.extract(archive)
	if removeArchive {
		os.Remove(archive)
	}
	if err == nil {
		_, err = s.create(KindPreRestore)
		s.record(KindPreRestore, err)
		if err != nil {
			err = fmt.Errorf("backup pre-ripristino non riuscito: %w", err)
		}
	}
	if err != nil {
		os.RemoveAll(s.restoreDir())
		s.mu.Unlock()
		return nil, err
	}
	return s.swap, nil
}

func (s *Service) swap() {
	src := s.restoreDir()
	if err := s.o.Store.Close(); err != nil {
		slog.Error("ripristino: chiusura DB", "err", err)
	}
	os.Remove(s.o.DBPath + "-wal")
	os.Remove(s.o.DBPath + "-shm")
	if err := os.Rename(filepath.Join(src, dbEntry), s.o.DBPath); err != nil {
		slog.Error("ripristino: sostituzione DB non riuscita, usare il backup pre-ripristino", "err", err)
		s.o.Exit(1)
		return
	}
	old := s.o.UploadDir + ".old"
	os.RemoveAll(old)
	if _, err := os.Stat(s.o.UploadDir); err == nil {
		if err := os.Rename(s.o.UploadDir, old); err != nil {
			slog.Error("ripristino: spostamento uploads", "err", err)
			s.o.Exit(1)
			return
		}
	}
	if _, err := os.Stat(filepath.Join(src, uploadsEntry)); err == nil {
		if err := os.Rename(filepath.Join(src, uploadsEntry), s.o.UploadDir); err != nil {
			slog.Error("ripristino: sostituzione uploads", "err", err)
			s.o.Exit(1)
			return
		}
	} else {
		os.MkdirAll(s.o.UploadDir, 0o750)
	}
	os.RemoveAll(old)
	os.RemoveAll(src)
	slog.Info("ripristino completato: riavvio")
	s.o.Exit(0)
}
```

- [ ] **Step 3: Verifica e commit**

Run: `go vet ./... && go test ./internal/backup/ -v`
Expected: PASS

```bash
git add internal/backup
git commit -m "feat(backup): upload a pezzi e ripristino con backup pre-ripristino

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: Pagina admin dei backup, ripristino da elenco, avviso in panoramica

**Files:**
- Create: `internal/web/admin_backup.go`, `web/templates/admin_backup.html`
- Modify: `internal/web/server.go` (Options, Server, route), `internal/web/render.go` (funcs), `internal/web/render_test.go`, `internal/web/server_test.go` (`newTestServer`), `internal/web/admin.go` (panoramica), `web/templates/admin_overview.html`, `web/templates/admin_base.html` (voce menu), `web/static/css/admin.css`, `cmd/server/main.go`
- Test: `internal/web/admin_backup_test.go`

**Interfaces:**
- Consumes: `backup.Service` (`Create`, `List`, `Path`, `Delete`, `Status`, `PrepareRestore`), `backup.ErrBusy`, `backup.ErrNotFound`, `backup.ErrProtected`, `backup.ValidationError`, `backup.Kind*` (Task 2–5); convenzione sezioni admin (sotto-progetto 1)
- Produces:
  ```go
  // web.Options
  Backup       *backup.Service
  RestoreDelay time.Duration // attesa prima dello swap; 0 → 500ms
  func (s *Server) startRestore(w http.ResponseWriter, r *http.Request, archive string, removeArchive bool)
  func (s *Server) renderBackups(w http.ResponseWriter, status int, errs formErrors, notice string)
  func humanSize(n int64) string
  func kindLabel(kind string) string
  func backupWarning(st backup.Status, now time.Time) string
  // test helper
  var serverExits map[*Server]chan int
  ```
  Template: `admin_backup.html`, `backup_section` (dati `backupSection`), `backup_restarting`.

- [ ] **Step 1: `newTestServer` con servizio backup** — in `internal/web/server_test.go` sostituisci il corpo di `newTestServer` con:

```go
// serverExits raccoglie le chiamate a exit del servizio backup di ogni server di test.
var serverExits = map[*Server]chan int{}

func newTestServer(t *testing.T, a auth.Authenticator) (*Server, *database.DB) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	uploadDir := filepath.Join(dir, "uploads")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	rome, _ := time.LoadLocation("Europe/Rome")
	if a == nil {
		a = fakeAuth{ok: true, admin: true}
	}
	exits := make(chan int, 4)
	bk, err := backup.New(backup.Options{
		Store: db, DBPath: dbPath, UploadDir: uploadDir, AppVersion: "test",
		MaxSchema: database.CurrentSchemaVersion(), Location: rome,
		Now:  func() time.Time { return fixedNow },
		Exit: func(code int) { exits <- code },
	})
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(Options{
		DB:           db,
		Config:       config.Config{SessionSecret: strings.Repeat("s", 32), DBPath: dbPath, UploadDir: uploadDir, Location: rome},
		Auth:         a,
		Backup:       bk,
		RestoreDelay: time.Nanosecond,
		Version:      "test",
		WebDir:       "../../web",
		Now:          func() time.Time { return fixedNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	serverExits[s] = exits
	return s, db
}
```

con import `"github.com/Comune-di-Montesilvano/CruscottoPA/internal/backup"`.

- [ ] **Step 2: Test funzioni di template** — in `internal/web/render_test.go`:

```go
func TestHumanSizeAndKindLabel(t *testing.T) {
	for n, want := range map[int64]string{0: "0 B", 512: "512 B", 1536: "1,5 KB", 5 << 20: "5,0 MB", 3 << 30: "3,0 GB"} {
		if got := humanSize(n); got != want {
			t.Errorf("humanSize(%d) = %q, atteso %q", n, got, want)
		}
	}
	if kindLabel("auto") != "Automatico" || kindLabel("manuale") != "Manuale" || kindLabel("pre-ripristino") != "Pre-ripristino" {
		t.Fatal("etichette tipo backup errate")
	}
}
```

- [ ] **Step 3: Test handler** — `internal/web/admin_backup_test.go`

```go
package web

import (
	"bytes"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/backup"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

func waitExit(t *testing.T, s *Server) {
	t.Helper()
	select {
	case code := <-serverExits[s]:
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("il ripristino non ha chiamato exit")
	}
}

func categoryNames(t *testing.T, dbPath string) string {
	t.Helper()
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cats, _ := db.ListCategories()
	var out []string
	for _, c := range cats {
		out = append(out, c.Name)
	}
	return strings.Join(out, ",")
}

func TestBackupPageAndCreate(t *testing.T) {
	s, _ := newTestServer(t, nil)
	c := login(t, s)

	page := do(t, s, "GET", "/admin/backup", nil, c, nil).Body.String()
	if !strings.Contains(page, "stesso server") || !strings.Contains(page, "Nessun backup") {
		t.Fatalf("pagina backup:\n%s", page)
	}
	rec := do(t, s, "POST", "/admin/backup", nil, c, hx)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Backup creato") || !strings.Contains(rec.Body.String(), "Manuale") {
		t.Fatalf("crea: %d\n%s", rec.Code, rec.Body)
	}
	rec = do(t, s, "POST", "/admin/backup", nil, c, hx) // stesso secondo (orologio fisso)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "esiste già") {
		t.Fatalf("doppio click: %d\n%s", rec.Code, rec.Body)
	}
}

func TestBackupDownloadAndDelete(t *testing.T) {
	s, _ := newTestServer(t, nil)
	c := login(t, s)
	info, _ := s.backup.Create(backup.KindManual)

	rec := do(t, s, "GET", "/admin/backup/"+info.Name, nil, c, nil)
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Content-Disposition"), "attachment") || !bytes.HasPrefix(rec.Body.Bytes(), []byte{0x1f, 0x8b}) {
		t.Fatalf("download: %d %v", rec.Code, rec.Header())
	}
	if rec := do(t, s, "GET", "/admin/backup/cruscotto-x.tar.gz", nil, c, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("nome non valido: %d", rec.Code)
	}
	if rec := do(t, s, "POST", "/admin/backup/"+info.Name+"/elimina", nil, c, hx); rec.Code != 200 {
		t.Fatalf("elimina: %d", rec.Code)
	}
	if list, _ := s.backup.List(); len(list) != 0 {
		t.Fatal("backup non eliminato")
	}

	auto, _ := s.backup.Create(backup.KindAuto)
	rec = do(t, s, "POST", "/admin/backup/"+auto.Name+"/elimina", nil, c, hx)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "conservazione automatica") {
		t.Fatalf("auto non eliminabile: %d\n%s", rec.Code, rec.Body)
	}
}

func TestRestoreFromList(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	db.CreateCategory("Prima")
	info, _ := s.backup.Create(backup.KindManual)
	db.CreateCategory("Dopo")
	path := "/admin/backup/" + info.Name + "/ripristina"

	rec := do(t, s, "POST", path, url.Values{"conferma": {"ripristina"}}, c, hx)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "digita RIPRISTINA") {
		t.Fatalf("conferma errata: %d\n%s", rec.Code, rec.Body)
	}
	if list, _ := s.backup.List(); len(list) != 1 {
		t.Fatal("senza conferma non deve partire nulla (niente pre-ripristino)")
	}

	rec = do(t, s, "POST", path, url.Values{"conferma": {"RIPRISTINA"}}, c, hx)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "data-restarting") {
		t.Fatalf("ripristino: %d\n%s", rec.Code, rec.Body)
	}
	waitExit(t, s)
	if names := categoryNames(t, s.cfg.DBPath); !strings.Contains(names, "Prima") || strings.Contains(names, "Dopo") {
		t.Fatalf("dati non ripristinati: %s", names)
	}
}

func TestBackupRequiresAdminAndSameOrigin(t *testing.T) {
	s, _ := newTestServer(t, nil)
	info, _ := s.backup.Create(backup.KindManual)
	for _, p := range []string{"/admin/backup", "/admin/backup/" + info.Name} {
		if rec := do(t, s, "GET", p, nil, nil, nil); rec.Code != http.StatusSeeOther {
			t.Errorf("%s senza sessione: %d", p, rec.Code)
		}
	}
	c := login(t, s)
	if rec := do(t, s, "POST", "/admin/backup", nil, c, map[string]string{"Sec-Fetch-Site": "cross-site"}); rec.Code != http.StatusForbidden {
		t.Fatalf("POST cross-origin: %d", rec.Code)
	}
}

func TestOverviewBackupWarning(t *testing.T) {
	s, _ := newTestServer(t, nil)
	c := login(t, s)
	if body := do(t, s, "GET", "/admin", nil, c, nil).Body.String(); !strings.Contains(body, "Non è ancora stato fatto nessun backup") {
		t.Fatal("manca l'avviso senza backup")
	}
	s.backup.Create(backup.KindManual)
	if body := do(t, s, "GET", "/admin", nil, c, nil).Body.String(); strings.Contains(body, "nessun backup") {
		t.Fatal("avviso presente nonostante il backup")
	}
}

func TestBackupWarning(t *testing.T) {
	now := time.Date(2026, 10, 6, 8, 0, 0, 0, time.UTC)
	cases := map[string]struct {
		st   backup.Status
		want string
	}{
		"nessuno":  {backup.Status{}, "Non è ancora stato fatto nessun backup."},
		"recente":  {backup.Status{LastSuccess: now.Add(-47 * time.Hour)}, ""},
		"vecchio":  {backup.Status{LastSuccess: now.Add(-49 * time.Hour)}, "più di 48 ore"},
		"fallito":  {backup.Status{LastSuccess: now, LastAutoError: "disco pieno"}, "disco pieno"},
	}
	for name, tc := range cases {
		got := backupWarning(tc.st, now)
		if (tc.want == "" && got != "") || !strings.Contains(got, tc.want) {
			t.Errorf("%s: %q", name, got)
		}
	}
}
```

Run: `go test ./internal/web/`
Expected: FAIL — `undefined: backup` in server.go / `Options.Backup` inesistente

- [ ] **Step 4: Server** — in `internal/web/server.go`:
  - import `"github.com/Comune-di-Montesilvano/CruscottoPA/internal/backup"`;
  - a `Options` aggiungi `Backup *backup.Service` e `RestoreDelay time.Duration`;
  - a `Server` aggiungi `backup *backup.Service` e `restoreDelay time.Duration`;
  - in `New`, nella costruzione di `s`: `backup: o.Backup, restoreDelay: o.RestoreDelay,` e prima: `if o.RestoreDelay == 0 { o.RestoreDelay = 500 * time.Millisecond }`;
  - in `routes()`, dopo le route delle categorie:

```go
	s.mux.HandleFunc("GET /admin/backup", s.requireAdmin(s.handleBackupPage))
	s.mux.HandleFunc("POST /admin/backup", s.requireAdmin(s.handleBackupCreate))
	s.mux.HandleFunc("GET /admin/backup/{name}", s.requireAdmin(s.handleBackupDownload))
	s.mux.HandleFunc("POST /admin/backup/{name}/elimina", s.requireAdmin(s.handleBackupDelete))
	s.mux.HandleFunc("POST /admin/backup/{name}/ripristina", s.requireAdmin(s.handleBackupRestore))
```

- [ ] **Step 5: Funzioni di template** — in `internal/web/render.go`, nella `FuncMap` aggiungi `"humanSize": humanSize, "kindLabel": kindLabel,` e in fondo al file:

```go
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 2; m /= unit {
		div *= unit
		exp++
	}
	s := fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMG"[exp])
	return strings.Replace(s, ".", ",", 1)
}

func kindLabel(kind string) string {
	switch kind {
	case "auto":
		return "Automatico"
	case "manuale":
		return "Manuale"
	default:
		return "Pre-ripristino"
	}
}
```

- [ ] **Step 6: Handler** — `internal/web/admin_backup.go`

```go
package web

import (
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"
	"time"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/auth"
	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/backup"
)

const restoreConfirm = "RIPRISTINA"

type backupSection struct {
	Backups []backup.Info
	Status  backup.Status
	Errors  formErrors
	Notice  string
}

func (s *Server) renderBackups(w http.ResponseWriter, status int, errs formErrors, notice string) {
	list, err := s.backup.List()
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.render(w, status, "backup_section", backupSection{Backups: list, Status: s.backup.Status(), Errors: errs, Notice: notice})
}

func (s *Server) handleBackupPage(w http.ResponseWriter, r *http.Request) {
	list, err := s.backup.List()
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderPage(w, r, "admin_backup.html", "backup", backupSection{Backups: list, Status: s.backup.Status()})
}

func (s *Server) handleBackupCreate(w http.ResponseWriter, r *http.Request) {
	info, err := s.backup.Create(backup.KindManual)
	if err != nil {
		if !errors.Is(err, backup.ErrBusy) {
			slog.Error("backup manuale non riuscito", "err", err)
		}
		s.renderBackups(w, http.StatusUnprocessableEntity, formErrors{"general": "Backup non riuscito: " + err.Error()}, "")
		return
	}
	slog.Info("backup manuale creato", "user", auth.SafeLog(s.currentAdmin(r)), "file", info.Name)
	s.renderBackups(w, http.StatusOK, nil, "Backup creato: "+info.Name)
}

func (s *Server) handleBackupDownload(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	p, err := s.backup.Path(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeFile(w, r, p)
}

func (s *Server) handleBackupDelete(w http.ResponseWriter, r *http.Request) {
	err := s.backup.Delete(r.PathValue("name"))
	switch {
	case errors.Is(err, backup.ErrNotFound):
		http.NotFound(w, r)
	case errors.Is(err, backup.ErrProtected):
		s.renderBackups(w, http.StatusUnprocessableEntity, formErrors{"general": err.Error()}, "")
	case err != nil:
		s.serverError(w, err)
	default:
		s.renderBackups(w, http.StatusOK, nil, "Backup eliminato.")
	}
}

func (s *Server) handleBackupRestore(w http.ResponseWriter, r *http.Request) {
	if r.FormValue("conferma") != restoreConfirm {
		s.renderBackups(w, http.StatusUnprocessableEntity, formErrors{"general": "Per confermare il ripristino digita RIPRISTINA."}, "")
		return
	}
	p, err := s.backup.Path(r.PathValue("name"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.startRestore(w, r, p, false)
}

// startRestore valida l'archivio, crea il pre-ripristino, risponde con la pagina
// di attesa e solo dopo esegue lo swap (che termina il processo).
func (s *Server) startRestore(w http.ResponseWriter, r *http.Request, archive string, removeArchive bool) {
	swap, err := s.backup.PrepareRestore(archive, removeArchive)
	if err != nil {
		var ve *backup.ValidationError
		msg := "Ripristino non riuscito: " + err.Error()
		switch {
		case errors.As(err, &ve):
			msg = "Archivio rifiutato: " + ve.Msg
		case errors.Is(err, backup.ErrBusy):
			msg = err.Error()
		default:
			slog.Error("ripristino non riuscito", "err", err)
		}
		s.renderBackups(w, http.StatusUnprocessableEntity, formErrors{"general": msg}, "")
		return
	}
	slog.Warn("ripristino avviato", "user", auth.SafeLog(s.currentAdmin(r)), "archivio", filepath.Base(archive))
	s.render(w, http.StatusOK, "backup_restarting", nil)
	go func() {
		time.Sleep(s.restoreDelay)
		swap()
	}()
}

// backupWarning è il messaggio per la panoramica admin ("" se tutto ok).
func backupWarning(st backup.Status, now time.Time) string {
	switch {
	case st.LastAutoError != "":
		return "L'ultimo backup automatico non è riuscito: " + st.LastAutoError
	case st.LastSuccess.IsZero():
		return "Non è ancora stato fatto nessun backup."
	case now.Sub(st.LastSuccess) > 48*time.Hour:
		return "L'ultimo backup riuscito ha più di 48 ore."
	}
	return ""
}
```

Nota sul `filepath.Base(archive)` nel log: per i caricamenti è il nome generato `upload-<id>.part`, mai input utente.

- [ ] **Step 7: Panoramica** — in `internal/web/admin.go` aggiungi a `overviewView` il campo `BackupWarning string` e, in `handleOverview` prima di `s.renderPage`, `v.BackupWarning = backupWarning(s.backup.Status(), s.now())`. In `web/templates/admin_overview.html` subito dopo `{{with .Body}}`:

```html
{{with .BackupWarning}}<p class="flash error" role="alert">{{.}} <a href="/admin/backup">Vai ai backup</a></p>{{end}}
```

- [ ] **Step 8: Template** — `web/templates/admin_backup.html`

```html
{{define "admin_backup.html"}}{{template "admin_top" .}}
<h1>Backup</h1>
{{template "backup_section" .Body}}
{{template "admin_bottom" .}}{{end}}

{{define "backup_restarting"}}
<div id="section" data-restarting>
	<div class="card">
		<h2>Ripristino in corso</h2>
		<p>I dati vengono sostituiti e il servizio si riavvia. La pagina si ricaricherà da sola appena il servizio torna disponibile.</p>
	</div>
</div>
{{end}}

{{define "backup_section"}}
<div id="section">
	<p class="flash">Le copie sono sullo stesso server: scaricane una periodicamente e conservala in un luogo protetto, fuori dal server.</p>
	{{with .Errors.general}}<p class="flash error" role="alert">{{.}}</p>{{end}}
	{{with .Notice}}<p class="flash" role="status">{{.}}</p>{{end}}

	<div class="card form">
		<h2>Nuovo backup</h2>
		<p class="hint">{{if .Status.LastSuccess.IsZero}}Nessun backup ancora eseguito.{{else}}Ultimo backup riuscito: {{fmtDate .Status.LastSuccess}}.{{end}} Il backup comprende database e file caricati.</p>
		<div class="actions">
			<button class="primary" type="button" hx-post="/admin/backup" hx-target="#section" hx-swap="outerHTML">Crea backup ora</button>
		</div>
	</div>

	<table class="list">
		<thead><tr><th>Data</th><th>Tipo</th><th>Dimensione</th><th></th></tr></thead>
		<tbody>
			{{range .Backups}}
			<tr>
				<td>{{fmtDate .CreatedAt}}</td>
				<td><span class="tag{{if eq .Kind "pre-ripristino"}} warn{{end}}">{{kindLabel .Kind}}</span></td>
				<td class="muted">{{humanSize .Size}}</td>
				<td class="actions">
					<a class="button" href="/admin/backup/{{.Name}}" download>Scarica</a>
					<form class="inline-confirm" hx-post="/admin/backup/{{.Name}}/ripristina" hx-target="#section" hx-swap="outerHTML">
						<input name="conferma" placeholder="digita RIPRISTINA" aria-label="Conferma ripristino" autocomplete="off">
						<button type="submit">Ripristina</button>
					</form>
					{{if ne .Kind "auto"}}<button class="danger" type="button" hx-post="/admin/backup/{{.Name}}/elimina" hx-confirm="Eliminare il backup del {{fmtDate .CreatedAt}}?" hx-target="#section" hx-swap="outerHTML">Elimina</button>{{end}}
				</td>
			</tr>
			{{else}}
			<tr><td colspan="4" class="muted">Nessun backup.</td></tr>
			{{end}}
		</tbody>
	</table>
</div>
{{end}}
```

In `web/templates/admin_base.html`, dopo la voce Categorie:

```html
		<a href="/admin/backup"{{if eq .Section "backup"}} aria-current="page"{{end}}><span class="material-icons" aria-hidden="true">backup</span>Backup</a>
```

In coda a `web/static/css/admin.css`:

```css
.inline-confirm { display: inline-flex; gap: .3rem; margin: 0; }
.inline-confirm input { width: 10rem; padding: .35rem .5rem; }
#section > .card + .list, #section > .list + .card { margin-top: 1.25rem; }
progress { width: 100%; }
```

- [ ] **Step 9: Wiring in `main.go`**

Sposta le due righe

```go
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
```

subito dopo il blocco `os.MkdirAll(cfg.UploadDir, …)`, e aggiungi lì sotto:

```go
	bk, err := backup.New(backup.Options{
		Store:      db,
		DBPath:     cfg.DBPath,
		UploadDir:  cfg.UploadDir,
		AppVersion: AppVersion,
		MaxSchema:  database.CurrentSchemaVersion(),
		Location:   cfg.Location,
	})
	if err != nil {
		slog.Error("inizializzazione backup", "err", err)
		os.Exit(1)
	}
	go bk.Scheduler(ctx, time.Duration(cfg.BackupIntervalHours)*time.Hour)
```

In `web.Options{…}` aggiungi `Backup: bk,`; importa `"github.com/Comune-di-Montesilvano/CruscottoPA/internal/backup"`.

- [ ] **Step 10: Verifica e commit**

Run: `go vet ./... && go test ./...`
Expected: PASS

```bash
git add internal/web web cmd/server/main.go
git commit -m "feat(admin): pagina backup con creazione, download, eliminazione e ripristino

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: Ripristino da file caricato (upload a pezzi + attesa riavvio)

**Files:**
- Modify: `internal/web/admin_backup.go`, `internal/web/server.go` (route), `web/templates/admin_backup.html`, `web/static/js/admin.js`
- Test: `internal/web/admin_backup_upload_test.go`

**Interfaces:**
- Consumes: `backup.StartUpload`, `WriteChunk`, `FinishUpload`, `ChunkSize`, `ErrUploadNotFound`, `ErrChunkOrder`, `ErrChunkTooBig`, `ValidationError` (Task 5); `startRestore`, `renderBackups` (Task 6)
- Produces: route `POST /admin/backup/upload` (JSON `{"id","chunk"}`), `POST /admin/backup/upload/{id}/chunk?n=` (204/400/404/409/413), `POST /admin/backup/upload/{id}/fine` (sezione o pagina di attesa)

- [ ] **Step 1: Test che falliscono** — `internal/web/admin_backup_upload_test.go`

```go
package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/backup"
)

func postRaw(t *testing.T, s *Server, target string, body []byte, c *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", target, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.AddCookie(c)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func startUpload(t *testing.T, s *Server, c *http.Cookie) (string, int) {
	t.Helper()
	rec := postRaw(t, s, "/admin/backup/upload", nil, c)
	var out struct {
		ID    string `json:"id"`
		Chunk int    `json:"chunk"`
	}
	if rec.Code != 200 || json.NewDecoder(rec.Body).Decode(&out) != nil || out.ID == "" {
		t.Fatalf("avvio upload: %d %s", rec.Code, rec.Body)
	}
	return out.ID, out.Chunk
}

func TestUploadRestoreFlow(t *testing.T) {
	s, db := newTestServer(t, nil)
	c := login(t, s)
	db.CreateCategory("Prima")
	info, _ := s.backup.Create(backup.KindManual)
	p, _ := s.backup.Path(info.Name)
	archive, _ := os.ReadFile(p)
	db.CreateCategory("Dopo")

	// conferma sbagliata: upload scartato, nessun ripristino
	id, chunk := startUpload(t, s, c)
	if chunk != backup.ChunkSize {
		t.Fatalf("chunk annunciato %d", chunk)
	}
	if rec := postRaw(t, s, "/admin/backup/upload/"+id+"/chunk?n=1", archive, c); rec.Code != http.StatusConflict {
		t.Fatalf("pezzo fuori sequenza: %d", rec.Code)
	}
	if rec := postRaw(t, s, "/admin/backup/upload/"+id+"/chunk?n=0", archive, c); rec.Code != http.StatusNoContent {
		t.Fatalf("pezzo 0: %d %s", rec.Code, rec.Body)
	}
	rec := do(t, s, "POST", "/admin/backup/upload/"+id+"/fine", url.Values{"conferma": {"no"}}, c, nil)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "digita RIPRISTINA") {
		t.Fatalf("conferma errata: %d\n%s", rec.Code, rec.Body)
	}

	// flusso completo
	id, _ = startUpload(t, s, c)
	for n := 0; n*backup.ChunkSize < len(archive); n++ {
		end := min((n+1)*backup.ChunkSize, len(archive))
		if rec := postRaw(t, s, "/admin/backup/upload/"+id+"/chunk?n="+strconv.Itoa(n), archive[n*backup.ChunkSize:end], c); rec.Code != http.StatusNoContent {
			t.Fatalf("pezzo %d: %d", n, rec.Code)
		}
	}
	rec = do(t, s, "POST", "/admin/backup/upload/"+id+"/fine", url.Values{"conferma": {"RIPRISTINA"}}, c, nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "data-restarting") {
		t.Fatalf("fine: %d\n%s", rec.Code, rec.Body)
	}
	waitExit(t, s)
	if names := categoryNames(t, s.cfg.DBPath); !strings.Contains(names, "Prima") || strings.Contains(names, "Dopo") {
		t.Fatalf("dati non ripristinati: %s", names)
	}
}

func TestUploadRejectsInvalidArchive(t *testing.T) {
	s, _ := newTestServer(t, nil)
	c := login(t, s)
	id, _ := startUpload(t, s, c)
	postRaw(t, s, "/admin/backup/upload/"+id+"/chunk?n=0", []byte("non è un backup"), c)
	rec := do(t, s, "POST", "/admin/backup/upload/"+id+"/fine", url.Values{"conferma": {"RIPRISTINA"}}, c, nil)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Archivio rifiutato") {
		t.Fatalf("archivio non valido: %d\n%s", rec.Code, rec.Body)
	}
	select {
	case <-serverExits[s]:
		t.Fatal("exit chiamato per un archivio rifiutato")
	default:
	}
}

func TestUploadChunkErrors(t *testing.T) {
	s, _ := newTestServer(t, nil)
	c := login(t, s)
	if rec := postRaw(t, s, "/admin/backup/upload/sconosciuto/chunk?n=0", []byte("x"), c); rec.Code != http.StatusNotFound {
		t.Fatalf("id sconosciuto: %d", rec.Code)
	}
	id, _ := startUpload(t, s, c)
	if rec := postRaw(t, s, "/admin/backup/upload/"+id+"/chunk?n=zero", []byte("x"), c); rec.Code != http.StatusBadRequest {
		t.Fatalf("n non numerico: %d", rec.Code)
	}
	if rec := postRaw(t, s, "/admin/backup/upload/"+id+"/chunk?n=0", make([]byte, backup.ChunkSize+1), c); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("pezzo troppo grande: %d", rec.Code)
	}
}
```

Run: `go test ./internal/web/ -run Upload`
Expected: FAIL — 404/405 sulle route `/admin/backup/upload`

- [ ] **Step 2: Handler** — in coda a `internal/web/admin_backup.go` (aggiungi gli import `"encoding/json"`, `"os"`, `"strconv"`):

```go
func (s *Server) handleBackupUploadStart(w http.ResponseWriter, r *http.Request) {
	id, err := s.backup.StartUpload()
	if err != nil {
		s.serverError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"id": id, "chunk": backup.ChunkSize})
}

func (s *Server) handleBackupUploadChunk(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(r.URL.Query().Get("n"))
	if err != nil || n < 0 {
		http.Error(w, "parametro n non valido", http.StatusBadRequest)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, backup.ChunkSize+1)
	err = s.backup.WriteChunk(r.PathValue("id"), n, r.Body)
	var ve *backup.ValidationError
	var tooBig *http.MaxBytesError
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, backup.ErrUploadNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, backup.ErrChunkOrder):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, backup.ErrChunkTooBig), errors.As(err, &ve), errors.As(err, &tooBig):
		http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
	default:
		s.serverError(w, err)
	}
}

func (s *Server) handleBackupUploadFinish(w http.ResponseWriter, r *http.Request) {
	path, err := s.backup.FinishUpload(r.PathValue("id"))
	if err != nil {
		s.renderBackups(w, http.StatusUnprocessableEntity, formErrors{"general": err.Error()}, "")
		return
	}
	if r.FormValue("conferma") != restoreConfirm {
		os.Remove(path)
		s.renderBackups(w, http.StatusUnprocessableEntity, formErrors{"general": "Per confermare il ripristino digita RIPRISTINA."}, "")
		return
	}
	s.startRestore(w, r, path, true)
}
```

Nota: `http.MaxBytesReader` blocca a `ChunkSize+1` byte, `WriteChunk` legge fino a `ChunkSize+1` e rifiuta oltre `ChunkSize`: un pezzo troppo grande dà sempre 413.

In `routes()`, dopo le route di backup del Task 6:

```go
	s.mux.HandleFunc("POST /admin/backup/upload", s.requireAdmin(s.handleBackupUploadStart))
	s.mux.HandleFunc("POST /admin/backup/upload/{id}/chunk", s.requireAdmin(s.handleBackupUploadChunk))
	s.mux.HandleFunc("POST /admin/backup/upload/{id}/fine", s.requireAdmin(s.handleBackupUploadFinish))
```

- [ ] **Step 3: Form** — in `web/templates/admin_backup.html`, dentro `backup_section` subito prima della `</div>` di chiusura di `#section`:

```html
	<form class="card form" data-restore-upload>
		<h2>Ripristina da file</h2>
		<p class="hint">Carica un archivio <code>.tar.gz</code> scaricato in precedenza. Tutti i dati attuali (database e file caricati) verranno sostituiti; prima viene creato in automatico un backup "pre-ripristino".</p>
		<input type="file" name="archivio" accept=".gz,application/gzip">
		<label>Per confermare digita RIPRISTINA<input name="conferma" autocomplete="off"></label>
		<progress value="0" max="1"></progress>
		<p class="field-error" data-upload-msg></p>
		<div class="actions"><button class="primary" type="submit">Carica e ripristina</button></div>
	</form>
```

- [ ] **Step 4: JS** — in `web/static/js/admin.js`, dentro l'IIFE dopo i listener esistenti:

```js
	// ── Ripristino da file: upload a pezzi (sotto il limite del reverse proxy) ──
	async function uploadRestore(form) {
		const file = form.querySelector("[name=archivio]").files[0];
		const conferma = form.querySelector("[name=conferma]").value;
		const bar = form.querySelector("progress");
		const msg = form.querySelector("[data-upload-msg]");
		const button = form.querySelector("button[type=submit]");
		msg.textContent = "";
		if (!file) { msg.textContent = "Scegli un file .tar.gz."; return; }
		if (conferma !== "RIPRISTINA") { msg.textContent = "Per confermare digita RIPRISTINA."; return; }
		button.disabled = true;
		try {
			const start = await fetch("/admin/backup/upload", { method: "POST" });
			if (!start.ok) throw new Error("Impossibile avviare il caricamento.");
			const { id, chunk } = await start.json();
			const total = Math.max(1, Math.ceil(file.size / chunk));
			for (let n = 0; n < total; n++) {
				const res = await fetch(`/admin/backup/upload/${id}/chunk?n=${n}`, {
					method: "POST",
					body: file.slice(n * chunk, (n + 1) * chunk),
				});
				if (!res.ok) throw new Error(`Caricamento interrotto (pezzo ${n + 1} di ${total}): ${(await res.text()).trim()}`);
				bar.value = (n + 1) / total;
			}
			const fin = await fetch(`/admin/backup/upload/${id}/fine`, {
				method: "POST",
				body: new URLSearchParams({ conferma }),
			});
			document.getElementById("section").outerHTML = await fin.text();
			watchRestart();
		} catch (err) {
			msg.textContent = err.message;
			button.disabled = false;
		}
	}

	document.addEventListener("submit", (e) => {
		if (!e.target.matches("[data-restore-upload]")) return;
		e.preventDefault();
		uploadRestore(e.target);
	});

	// ── Dopo un ripristino: attende che il servizio riparta e ricarica ──
	function watchRestart() {
		if (!document.querySelector("#section[data-restarting]")) return;
		const started = Date.now();
		const poll = async () => {
			try {
				const res = await fetch("/health", { cache: "no-store" });
				if (res.ok && Date.now() - started > 3000) {
					location.href = "/admin/backup";
					return;
				}
			} catch (_) {
				// servizio in riavvio
			}
			setTimeout(poll, 2000);
		};
		setTimeout(poll, 2000);
	}
	document.addEventListener("htmx:afterSwap", watchRestart);
```

- [ ] **Step 5: Verifica e commit**

Run: `go vet ./... && go test ./...`
Expected: PASS

```bash
git add internal/web web
git commit -m "feat(admin): ripristino da file caricato a pezzi con attesa del riavvio

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 8: Documentazione, prova reale nel container, PR

**Files:**
- Modify: `CLAUDE.md`, `README.md`

- [ ] **Step 1: `CLAUDE.md`** — nella sezione `## Architettura`, dopo la riga di `internal/icons`:

```markdown
- `internal/backup`: archivi `tar.gz` (manifest + `VACUUM INTO` del DB + `uploads/`) in `<dir DB_PATH>/backups`, scritti su `.tmp` e rinominati. Scheduler ogni `BACKUP_INTERVAL_HOURS` (0 = off) con retention GFS solo sugli `auto`. Ripristino: `extract` in `restore-tmp` con validazione (percorsi, link, manifest, schema non futuro, `integrity_check`), backup `pre-ripristino`, swap di DB e uploads, poi `exit(0)` → riavvio dalla restart policy. Un'operazione alla volta (`ErrBusy`). Upload a pezzi da 512 KB per stare sotto il limite di 1 MB dei proxy.
```

Nella sezione `## Vincoli di deploy` aggiungi:

```markdown
- **Ripristino = uscita del processo**: dopo lo swap il container deve ripartire da solo (`restart: unless-stopped`); senza restart policy resta fermo con i dati già ripristinati.
```

- [ ] **Step 2: `README.md`** — dopo la sezione "Amministrazione":

```markdown
## Backup e ripristino

Da `/admin/backup`:

- backup automatici ogni `BACKUP_INTERVAL_HOURS` ore (default 24, `0` = disattivati), conservati a scalare: tutti gli ultimi 7 giorni, poi uno a settimana fino a 35 giorni, uno al mese fino a un anno;
- backup manuali con "Crea backup ora" (restano finché non li elimini);
- download di ogni backup (`.tar.gz` con database e file caricati);
- ripristino da un backup in elenco o da un file caricato, con conferma `RIPRISTINA`: prima viene salvato lo stato attuale come backup "pre-ripristino", poi il servizio si riavvia con i dati ripristinati.

Le copie stanno nel volume `/data`, cioè sullo stesso server: scaricane una periodicamente e conservala altrove.
```

- [ ] **Step 3: Prova reale nel container**

```bash
go vet ./... && go test ./...
docker compose up -d --build
```

Nel browser (`http://localhost:<PORT>/admin/backup`, login):
1. "Crea backup ora" → compare in elenco; "Scarica" scarica il `.tar.gz`.
2. Crea una categoria "Prova ripristino".
3. Sul backup del punto 1: digita `RIPRISTINA` → "Ripristina" → pagina "Ripristino in corso" → dopo qualche secondo la pagina si ricarica da sola.
4. La categoria "Prova ripristino" non c'è più; in elenco c'è un backup "Pre-ripristino".
5. "Ripristina da file" con il file scaricato al punto 1: stesso esito.

```bash
docker inspect cruscottopa --format '{{.RestartCount}} {{.State.Health.Status}}'   # RestartCount aumentato, healthy
docker logs cruscottopa 2>&1 | grep -E "ripristino|backup"
```

Expected: due riavvii registrati, container healthy, log "ripristino completato: riavvio".

- [ ] **Step 4: Commit, push, PR**

```bash
git add CLAUDE.md README.md
git commit -m "docs: backup e ripristino

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
git push -u origin feat/backup
gh pr create --base main --title "Backup e ripristino" --body "$(cat <<'EOF'
Implementa `docs/superpowers/specs/2026-10-06-backup-ripristino-design.md`.

- Backup automatici (`BACKUP_INTERVAL_HOURS`, retention GFS) e manuali: `tar.gz` con manifest, snapshot `VACUUM INTO` e uploads
- Pagina `/admin/backup`: crea, scarica, elimina, ripristina (da elenco o da file caricato a pezzi), conferma `RIPRISTINA`
- Ripristino validato (percorsi, link, schema, integrità) con backup pre-ripristino e riavvio del container
- Avviso in panoramica se l'ultimo backup è vecchio o fallito

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```
