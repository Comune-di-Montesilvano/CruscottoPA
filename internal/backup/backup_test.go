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

// Crash tra "uploads → uploads.old" e "restore-tmp/uploads → uploads":
// all'avvio la sostituzione va completata, non vanno cancellate entrambe le copie.
func TestCleanupCompletesInterruptedSwap(t *testing.T) {
	e := newEnv(t)
	dir := filepath.Dir(e.dbPath)
	os.Rename(e.uploads, e.uploads+".old") // vecchi uploads (a.png) spostati
	os.MkdirAll(filepath.Join(dir, "restore-tmp", "uploads", "icons"), 0o750)
	os.WriteFile(filepath.Join(dir, "restore-tmp", "uploads", "icons", "nuovo.png"), []byte("NEW"), 0o640)

	if _, err := New(e.s.o); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(e.uploads, "icons", "nuovo.png")); err != nil || string(b) != "NEW" {
		t.Fatalf("la sostituzione interrotta va completata con gli uploads ripristinati: %v", err)
	}
	for _, p := range []string{e.uploads + ".old", filepath.Join(dir, "restore-tmp")} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("residuo non rimosso: %s", p)
		}
	}
}

// Se manca anche la copia ripristinata, si torna agli uploads precedenti.
func TestCleanupRecoversOldUploads(t *testing.T) {
	e := newEnv(t)
	os.Rename(e.uploads, e.uploads+".old")

	if _, err := New(e.s.o); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(e.uploads, "icons", "a.png")); err != nil || string(b) != "PNG-A" {
		t.Fatalf("gli uploads precedenti vanno rimessi al loro posto: %v", err)
	}
}
