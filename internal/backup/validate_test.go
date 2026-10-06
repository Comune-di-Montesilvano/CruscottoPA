package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
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
		"traversal":          {buildArchive(t, []entry{{name: "manifest.json", body: ok}, {name: "../evil.txt", body: []byte("x")}}), "Percorso non ammesso"},
		"traversal annidato": {buildArchive(t, []entry{{name: "manifest.json", body: ok}, {name: "uploads/../../evil.txt", body: []byte("x")}}), "Percorso non ammesso"},
		"assoluto":           {buildArchive(t, []entry{{name: "manifest.json", body: ok}, {name: "/tmp/evil.txt", body: []byte("x")}}), "Percorso non ammesso"},
		"symlink":            {buildArchive(t, []entry{{name: "manifest.json", body: ok}, {name: "uploads/link", typ: tar.TypeSymlink, link: "/etc/passwd"}}), "Tipo di voce non ammesso"},
		"voce inattesa":      {buildArchive(t, []entry{{name: "manifest.json", body: ok}, {name: "altro.txt", body: []byte("x")}}), "Voce inattesa"},
		"senza manifest":     {buildArchive(t, []entry{{name: "cruscotto.db", body: db}}), "senza manifest.json"},
		"formato 2":          {buildArchive(t, []entry{{name: "manifest.json", body: manifestFor(1, 2)}, {name: "cruscotto.db", body: db}}), "Formato di backup 2"},
		"schema futuro":      {buildArchive(t, []entry{{name: "manifest.json", body: manifestFor(e.s.o.MaxSchema+1, 1)}, {name: "cruscotto.db", body: db}}), "versione più recente"},
		"senza db":           {buildArchive(t, []entry{{name: "manifest.json", body: ok}}), "senza cruscotto.db"},
		"db corrotto":        {buildArchive(t, []entry{{name: "manifest.json", body: ok}, {name: "cruscotto.db", body: append([]byte("SQLite format 3\x00"), bytes.Repeat([]byte{0xff}, 4096)...)}}), "danneggiato"},
		"non sqlite":         {buildArchive(t, []entry{{name: "manifest.json", body: ok}, {name: "cruscotto.db", body: []byte("ciao")}}), "danneggiato"},
		"non gzip":           {notGzip, "non è un archivio di backup"},
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

// Difesa in profondità: anche se entryName lasciasse passare un percorso,
// safeJoin non deve mai restituire un file fuori dalla cartella di destinazione.
func TestSafeJoin(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "restore-tmp")
	if p, err := safeJoin(dest, "uploads/icons/a.png"); err != nil || !strings.HasPrefix(p, dest) {
		t.Fatalf("percorso valido rifiutato: %q %v", p, err)
	}
	for _, bad := range []string{"../evil", "uploads/../../evil", "/etc/passwd", ".."} {
		if _, err := safeJoin(dest, bad); err == nil {
			t.Errorf("safeJoin(%q): atteso errore", bad)
		}
	}
}

// entryName usa filepath.IsLocal: su Windows rifiuta anche i nomi di
// dispositivo riservati, che aprirebbero un device invece di un file.
func TestEntryNameRejectsReservedNamesOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("nomi di dispositivo riservati solo su Windows")
	}
	for _, bad := range []string{"uploads/NUL", "uploads/icons/COM1"} {
		if _, err := entryName(&tar.Header{Name: bad, Typeflag: tar.TypeReg}); err == nil {
			t.Errorf("entryName(%q): atteso errore", bad)
		}
	}
}
