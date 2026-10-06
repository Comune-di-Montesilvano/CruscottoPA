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
